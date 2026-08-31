// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package integration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/observability"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertObservabilityRuntimeScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	registry := prometheus.NewRegistry()
	capture := &observabilityCapture{err: errors.New("runtime capture unavailable")}
	observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture, TracerProvider: provider, AttemptID: opaqueTestAttemptID()})
	if err != nil {
		t.Fatalf("construct runtime observer: %v", err)
	}

	source := runtimePipelineValuesSource("observability-runtime-source")
	key := types.NamespacedName{Namespace: "team-a", Name: "observability-runtime-owner"}
	object := runtimePipelineKubeseer(key, "observability-runtime-uid", 1, source)
	reader := newRuntimePipelineReader(object)
	lister := newRuntimePipelineLister()
	lister.SetResponse(source.ID, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*runtimePipelineResource("runtime-resource", "runtime-resource-uid", "runtime-value", "7")}})
	publisher := &runtimePipelinePublisher{}
	runtime := mustObservedRuntimePipeline(t, resolver, reader, lister, basePolicy(), &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker(), observer)
	if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatalf("successful observed reconciliation: %v", err)
	}
	records := capture.snapshot()
	if len(records) < 2 || records[0].Event != observability.EventReconciliationStarted {
		t.Fatalf("runtime capture start/terminal = %#v", records)
	}
	terminal := records[len(records)-1]
	if terminal.Event != observability.EventReconciliationCompleted || terminal.Outcome != observability.OutcomeCompleted || terminal.Reason != observability.ReasonEvaluationSucceeded || terminal.AttemptID == "" || terminal.TraceID == "" || terminal.SpanID == "" {
		t.Fatalf("runtime terminal envelope = %#v", terminal)
	}
	assertObservabilityMetricValue(t, registry, "kubeseer_reconciliations_total", `outcome="completed",reason="EvaluationSucceeded"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_reconciliation_duration_seconds", `outcome="completed"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_results_produced_total", `outcome="completed"`, 1)
	spans := exporter.GetSpans()
	if len(spans) != 10 {
		t.Fatalf("trace span count = %d, want one root plus nine fixed stages", len(spans))
	}
	rootIndex := -1
	for index, span := range spans {
		if span.Name == "kubeseer.reconciliation" && !span.Parent.SpanID().IsValid() {
			rootIndex = index
			break
		}
	}
	if rootIndex < 0 {
		t.Fatalf("root span not found in %#v", spans)
	}
	rootTrace := spans[rootIndex].SpanContext.TraceID()
	for index, span := range spans {
		if index == rootIndex {
			continue
		}
		if span.Parent.TraceID() != rootTrace || !span.Parent.SpanID().IsValid() || !strings.HasPrefix(span.Name, "kubeseer.reconciliation.") {
			t.Fatalf("stage span is not rooted at reconciliation: %#v", span)
		}
	}

	absentCapture := &observabilityCapture{}
	absentObserver, err := observability.New(observability.Options{Registerer: prometheus.NewRegistry(), LogSink: absentCapture})
	if err != nil {
		t.Fatalf("construct absent observer: %v", err)
	}
	absentRuntime := mustObservedRuntimePipeline(t, resolver, newRuntimePipelineReader(), newRuntimePipelineLister(), basePolicy(), &runtimePipelineRoutes{}, &runtimePipelinePublisher{}, reconciliation.NewFreshnessTracker(), absentObserver)
	if _, err := absentRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "missing-runtime"}}); err != nil {
		t.Fatalf("absent observed reconciliation: %v", err)
	}
	absentRecords := absentCapture.snapshot()
	if len(absentRecords) != 2 || absentRecords[1].Event != observability.EventReconciliationSkipped || absentRecords[1].Reason != observability.ReasonObjectAbsent || absentRecords[1].UID != "" {
		t.Fatalf("absent runtime records = %#v", absentRecords)
	}

	retryCapture := &observabilityCapture{}
	retryRegistry := prometheus.NewRegistry()
	retryObserver, err := observability.New(observability.Options{Registerer: retryRegistry, LogSink: retryCapture})
	if err != nil {
		t.Fatalf("construct retry observer: %v", err)
	}
	retryLister := newRuntimePipelineLister()
	retryLister.SetResponse(source.ID, nil, apierrors.NewServiceUnavailable("raw-retry-body-sentinel"))
	retryPublisher := &runtimePipelinePublisher{}
	retryRuntime := mustObservedRuntimePipeline(t, resolver, newRuntimePipelineReader(object), retryLister, basePolicy(), &runtimePipelineRoutes{}, retryPublisher, reconciliation.NewFreshnessTracker(), retryObserver)
	if _, err := retryRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err == nil || !reconciliation.IsRetryable(err) {
		t.Fatalf("retry observed reconciliation error = %v", err)
	}
	retryRecords := retryCapture.snapshot()
	if len(retryRecords) < 3 || retryRecords[len(retryRecords)-1].Event != observability.EventReconciliationRetryScheduled || retryRecords[len(retryRecords)-1].Reason != observability.ReasonReadUnavailable || retryRecords[len(retryRecords)-1].Retry != observability.RetryScheduled {
		t.Fatalf("retry runtime records = %#v", retryRecords)
	}
	assertObservabilityMetricValue(t, retryRegistry, "kubeseer_source_failures_total", `reason="ReadUnavailable",stage="read"`, 1)
	for _, record := range retryRecords {
		if strings.Contains(record.AttemptID, "raw-retry-body-sentinel") || strings.Contains(record.SourceID, "raw-retry-body-sentinel") {
			t.Fatalf("retry error crossed structured observation: %#v", record)
		}
	}

	concurrentRegistry := prometheus.NewRegistry()
	concurrentCapture := &observabilityCapture{}
	concurrentObserver, err := observability.New(observability.Options{Registerer: concurrentRegistry, LogSink: concurrentCapture})
	if err != nil {
		t.Fatalf("construct concurrent observer: %v", err)
	}
	concurrentReader := newRuntimePipelineReader(
		runtimePipelineKubeseer(types.NamespacedName{Namespace: "team-a", Name: "concurrent-a"}, "concurrent-a-uid", 1),
		runtimePipelineKubeseer(types.NamespacedName{Namespace: "team-a", Name: "concurrent-b"}, "concurrent-b-uid", 1),
	)
	concurrentRuntime := mustObservedRuntimePipeline(t, resolver, concurrentReader, newRuntimePipelineLister(), basePolicy(), &runtimePipelineRoutes{}, &runtimePipelinePublisher{}, reconciliation.NewFreshnessTracker(), concurrentObserver)
	var wait sync.WaitGroup
	wait.Add(2)
	for _, name := range []string{"concurrent-a", "concurrent-b"} {
		go func(name string) {
			defer wait.Done()
			if _, err := concurrentRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: name}}); err != nil {
				t.Errorf("concurrent %s reconciliation: %v", name, err)
			}
		}(name)
	}
	wait.Wait()
	assertObservabilityMetricValue(t, concurrentRegistry, "kubeseer_reconciliations_total", `outcome="completed",reason="EvaluationSucceeded"`, 2)
}

func opaqueTestAttemptID() observability.AttemptIDGenerator {
	var mu sync.Mutex
	count := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		count++
		return "observability-runtime-attempt-" + string(rune('a'+count-1))
	}
}

func mustObservedRuntimePipeline(t *testing.T, resolver *discovery.Resolver, reader *runtimePipelineReader, lister *runtimePipelineLister, policy *v1alpha1.KubeseerAccessPolicy, routes *runtimePipelineRoutes, publisher *runtimePipelinePublisher, tracker *reconciliation.FreshnessTracker, observer *observability.Observer) *reconciliation.Runtime {
	t.Helper()
	runtime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour}, reconciliation.Dependencies{
		Reader: reader,
		Lister: reader,
		PolicySource: accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			return policy.DeepCopy(), nil
		}),
		Enforcer: authorization.NewEnforcer(observability.NewAuthorizationRecorder(observer)),
		Planner:  selection.NewPlanner(resolver),
		Executor: selection.NewExecutor(lister,
			selection.WithVerifier(authorization.VerifierFunc(func(subject authorization.Subject) bool { return subject.Validate() == nil })),
			selection.WithPageObserver(observability.NewPageObserver(observer)),
		),
		Routes:    routes,
		Publisher: publisher,
		Tracker:   tracker,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("construct observed runtime: %v", err)
	}
	return runtime
}
