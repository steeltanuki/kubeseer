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
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertObservabilityManagerScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	defer func() { _ = provider.Shutdown(context.Background()) }()

	registry := prometheus.NewRegistry()
	capture := &observabilityCapture{}
	loggerLines := newObservabilityLoggerCapture()
	eventRecorder := record.NewFakeRecorder(8)
	observer, err := observability.New(observability.Options{
		Logger:         loggerLines.Logger(),
		Registerer:     registry,
		EventRecorder:  eventRecorder,
		TracerProvider: provider,
		LogSink:        capture,
		AttemptID:      func() string { return "observability-manager-attempt" },
	})
	if err != nil {
		t.Fatalf("construct manager-wide observer: %v", err)
	}

	key := types.NamespacedName{Namespace: "team-a", Name: "observability-manager-owner"}
	source := runtimePipelineValuesSource("observability-manager-source")
	owner := runtimePipelineKubeseer(key, "observability-manager-uid", 1, source)
	ownerReader := &countingRuntimeReader{delegate: newRuntimePipelineReader(owner)}
	resourceLister := newRuntimePipelineLister()
	resourceLister.SetResponse(source.ID, unstructuredListForPipeline(runtimePipelineResource("manager-resource", "manager-resource-uid", "manager-value", "7")))

	tracker := reconciliation.NewFreshnessTracker()
	watchStream := watch.NewRaceFreeFake()
	watcher := newRuntimeScriptedMetadataWatcher(runtimeWatchResponse{stream: watchStream})
	routes := reconciliation.NewRouteRegistry(
		watcher,
		tracker,
		reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
		reconciliation.WithRouteWatchObserver(observer),
	)
	queue := newRuntimeQueue()
	if err := routes.Start(ctx, queue); err != nil {
		queue.ShutDown()
		t.Fatalf("start manager-wide route registry: %v", err)
	}
	defer func() {
		routes.RemoveAll()
		queue.ShutDown()
	}()

	statusReader := &countingStatusReader{delegate: newRuntimeStatusReader(owner)}
	statusWriter := &runtimeStatusWriter{}
	publisher := reconciliation.NewStatusPublisher(statusReader, statusWriter, tracker, reconciliation.WithStatusObserver(observer))
	runtimeInstance, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour}, reconciliation.Dependencies{
		Reader:       ownerReader,
		Lister:       ownerReader,
		PolicySource: accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) { return basePolicy().DeepCopy(), nil }),
		Enforcer:     authorization.NewEnforcer(observability.NewAuthorizationRecorder(observer)),
		Planner:      selection.NewPlanner(resolver),
		Executor: selection.NewExecutor(resourceLister,
			selection.WithVerifier(authorization.VerifierFunc(func(subject authorization.Subject) bool { return subject.Validate() == nil })),
			selection.WithPageObserver(observability.NewPageObserver(observer)),
		),
		Routes:    routes,
		Publisher: publisher,
		Tracker:   tracker,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("construct manager-wide runtime: %v", err)
	}
	if _, err := runtimeInstance.Reconcile(ctx, observabilityReconcileRequest(key)); err != nil {
		t.Fatalf("manager-wide production reconciliation: %v", err)
	}
	if ownerReader.Gets() != 1 || ownerReader.Lists() != 0 {
		t.Fatalf("owner reads = get:%d list:%d, want one GET and no telemetry LIST", ownerReader.Gets(), ownerReader.Lists())
	}
	if statusReader.Gets() != 1 || statusWriter.Calls() != 1 {
		t.Fatalf("status reads/writes = %d/%d, want one each", statusReader.Gets(), statusWriter.Calls())
	}
	if len(resourceLister.Calls()) != 1 {
		t.Fatalf("resource LIST calls = %d, want one authorized production read", len(resourceLister.Calls()))
	}
	event := requireFakeEvent(t, eventRecorder)
	if !strings.HasPrefix(event, "Normal "+string(observability.ReasonEvaluationSucceeded)+" ") {
		t.Fatalf("manager-wide status Event = %q", event)
	}

	waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
	watchStream.Stop()
	waitForRuntimeWatchCondition(t, func() bool {
		for _, item := range capture.snapshot() {
			if item.Event == observability.EventSourceWatchRestarted {
				return true
			}
		}
		return false
	})
	// The production run above exercises the runtime's successful source path;
	// retain the complete manager-wide matrix by feeding the two remaining
	// finalized failure facts through the same observer instance.
	observer.ObserveSourceFailure(ctx, observability.SourceFailure{Stage: observability.StageRead, Reason: observability.ReasonReadUnavailable})
	observer.ObserveJSONPathFailure(ctx, observability.ReasonInvalidExtraction)

	records := capture.snapshot()
	seen := make(map[observability.EventCode]int)
	attemptID := ""
	traceID := ""
	for _, item := range records {
		seen[item.Event]++
		if item.Event == observability.EventReconciliationStarted {
			if attemptID != "" || item.AttemptID == "" {
				t.Fatalf("manager-wide start correlation = %#v", item)
			}
			attemptID = item.AttemptID
		}
		if item.AttemptID != "" {
			if item.AttemptID != attemptID || item.TraceID == "" || item.SpanID == "" {
				t.Fatalf("manager-wide correlation drift = %#v", item)
			}
			if traceID == "" {
				traceID = item.TraceID
			} else if item.TraceID != traceID {
				t.Fatalf("manager-wide trace correlation drift = %#v", item)
			}
		}
		encoded := string(mustJSON(t, item))
		for _, forbidden := range []string{"manager-resource-uid", "manager-value", "selector-value", "field-path", "raw-kubernetes-error", "secret-data"} {
			if strings.Contains(encoded, forbidden) {
				t.Fatalf("manager-wide signal retained forbidden sentinel %q: %s", forbidden, encoded)
			}
		}
	}
	for _, eventCode := range []observability.EventCode{
		observability.EventReconciliationStarted,
		observability.EventAuthorizationDecision,
		observability.EventStatusUpdateWritten,
		observability.EventSourceWatchStopped,
		observability.EventSourceWatchRestarted,
		observability.EventReconciliationCompleted,
	} {
		if seen[eventCode] != 1 {
			t.Fatalf("manager-wide event %s count = %d, want one", eventCode, seen[eventCode])
		}
	}
	if loggerLines.Len() < len(records) {
		t.Fatalf("replaceable logger entries = %d, want at least %d", loggerLines.Len(), len(records))
	}
	assertObservabilityMetricFamilies(t, registry)
	assertObservabilityMetricValue(t, registry, "kubeseer_reconciliations_total", `outcome="completed",reason="EvaluationSucceeded"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_reconciliation_duration_seconds", `outcome="completed"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_resources_read_total", `scope="namespaced"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_authorization_decisions_total", `kind="PolicyDecision",outcome="allowed",reason="Allowed"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_results_produced_total", `outcome="completed"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_status_updates_total", `outcome="written",reason="EvaluationSucceeded"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_source_watch_restarts_total", `reason="ReadUnavailable"`, 1)

	spans := exporter.GetSpans()
	if len(spans) != 10 {
		t.Fatalf("manager-wide spans = %d, want one root plus nine stages", len(spans))
	}
	if !exporter.GetSpans()[0].SpanContext.TraceID().IsValid() {
		t.Fatal("manager-wide trace exporter returned an invalid trace")
	}

	failingLogs := newObservabilityLoggerCapture()
	failingObserver, err := observability.New(observability.Options{
		Logger:     failingLogs.Logger(),
		Registerer: prometheus.NewRegistry(),
		LogSink:    &observabilityCapture{err: errors.New("capture exporter unavailable")},
	})
	if err != nil {
		t.Fatalf("construct failing-capture observer: %v", err)
	}
	failingRuntime := mustObservedRuntimePipeline(t, resolver, newRuntimePipelineReader(), newRuntimePipelineLister(), basePolicy(), &runtimePipelineRoutes{}, &runtimePipelinePublisher{}, reconciliation.NewFreshnessTracker(), failingObserver)
	if _, err := failingRuntime.Reconcile(ctx, observabilityReconcileRequest(types.NamespacedName{Namespace: "team-a", Name: "failing-capture-owner"})); err != nil {
		t.Fatalf("failing capture changed domain result: %v", err)
	}
	if failingLogs.Len() < 2 {
		t.Fatalf("remaining logger sink entries after capture failure = %d, want start and skip", failingLogs.Len())
	}
}

// reconcileRequest keeps this composition test independent from controller-runtime
// request construction details while still invoking the production Reconcile port.
func observabilityReconcileRequest(key types.NamespacedName) reconcile.Request {
	return reconcile.Request{NamespacedName: key}
}

type observabilityLoggerCapture struct {
	mu    sync.Mutex
	lines []string
}

func newObservabilityLoggerCapture() *observabilityLoggerCapture {
	return &observabilityLoggerCapture{}
}

func (c *observabilityLoggerCapture) Logger() logr.Logger {
	return funcr.New(func(_, args string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.lines = append(c.lines, args)
	}, funcr.Options{})
}

func (c *observabilityLoggerCapture) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.lines)
}

type countingRuntimeReader struct {
	delegate *runtimePipelineReader
	mu       sync.Mutex
	gets     int
	lists    int
}

func (r *countingRuntimeReader) Get(ctx context.Context, key types.NamespacedName, object *v1alpha1.Kubeseer) error {
	r.mu.Lock()
	r.gets++
	r.mu.Unlock()
	return r.delegate.Get(ctx, key, object)
}

func (r *countingRuntimeReader) List(ctx context.Context, list *v1alpha1.KubeseerList) error {
	r.mu.Lock()
	r.lists++
	r.mu.Unlock()
	return r.delegate.List(ctx, list)
}

func (r *countingRuntimeReader) Gets() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gets
}

func (r *countingRuntimeReader) Lists() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lists
}

type countingStatusReader struct {
	delegate *runtimeStatusReader
	mu       sync.Mutex
	gets     int
}

func (r *countingStatusReader) Get(ctx context.Context, key types.NamespacedName, object *v1alpha1.Kubeseer) error {
	r.mu.Lock()
	r.gets++
	r.mu.Unlock()
	return r.delegate.Get(ctx, key, object)
}

func (r *countingStatusReader) Gets() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gets
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal observability signal: %v", err)
	}
	return encoded
}
