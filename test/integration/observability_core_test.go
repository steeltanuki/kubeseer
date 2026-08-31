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

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/observability"
)

type observabilityCapture struct {
	mu      sync.Mutex
	records []observability.LogRecord
	err     error
}

func (c *observabilityCapture) Emit(record observability.LogRecord) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, record)
	return c.err
}

func (c *observabilityCapture) snapshot() []observability.LogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]observability.LogRecord(nil), c.records...)
}

func assertObservabilityCoreScenarios(t *testing.T) {
	t.Helper()
	registry := prometheus.NewRegistry()
	capture := &observabilityCapture{err: errors.New("capture unavailable")}
	now := time.Unix(100, 0)
	observer, err := observability.New(observability.Options{
		Registerer: registry,
		Clock: func() time.Time {
			return now
		},
		AttemptID: func() string { return "attempt-core" },
		LogSink:   capture,
	})
	if err != nil {
		t.Fatalf("construct observer: %v", err)
	}
	ctx, attempt := observer.StartAttempt(context.Background(), observability.AttemptStart{Namespace: "team-a", Name: "demo"})
	ctx = attempt.BindObject(ctx, observability.ObjectIdentity{UID: "kubeseer-uid", Generation: 4})
	stageCtx, stage := attempt.StartStage(ctx, observability.StageLoad)
	stage.End(observability.StageObservation{Stage: observability.StageLoad, Outcome: observability.OutcomeCompleted, Reason: observability.ReasonReconciliationStarted})
	_ = stageCtx
	attempt.Complete(ctx, observability.Terminal{Outcome: observability.OutcomeCompleted, Reason: observability.ReasonEvaluationSucceeded})
	attempt.Complete(ctx, observability.Terminal{Outcome: observability.OutcomeFailed, Reason: observability.ReasonBuildFailure})
	observer.ObserveSourceFailure(ctx, observability.SourceFailure{SourceID: "source-a", Stage: observability.StageRead, Reason: "untrusted-reason", Retry: observability.RetryScheduled})
	observer.ObserveJSONPathFailure(ctx, observability.ReasonInvalidExtraction)
	observer.ObserveResult(ctx, observability.OutcomeCompleted)
	observer.ObserveResult(ctx, observability.OutcomeDegraded)
	observer.ObserveResourcesRead(ctx, observability.ScopeNamespaced, 2)
	observer.ObserveResourcesRead(ctx, observability.ScopeCluster, 3)
	observer.ObserveStatus(ctx, observability.StatusWritten, observability.ReasonEvaluationSucceeded)
	observer.ObserveStatus(ctx, observability.StatusSkipped, observability.ReasonEvaluationSucceeded)
	observer.ObserveWatchRestart(ctx, observability.WatchObservation{Reason: observability.ReasonReadUnavailable, Retry: observability.RetryScheduled})
	observer.ObserveAuthorization(ctx, authorizationRecordForObservabilityCore())

	records := capture.snapshot()
	if len(records) < 7 {
		t.Fatalf("expected captured records despite sink errors, got %d", len(records))
	}
	if records[0].Event != observability.EventReconciliationStarted || records[0].AttemptID != "attempt-core" || records[0].Namespace != "team-a" || records[0].Name != "demo" {
		t.Fatalf("unexpected start envelope: %#v", records[0])
	}
	if records[1].UID != "kubeseer-uid" || records[1].Generation != 4 || records[1].TraceID != "" || records[1].SpanID != "" {
		t.Fatalf("unexpected bound/no-trace correlation: %#v", records[1])
	}
	foundInternal := false
	for _, record := range records {
		if record.Reason == observability.ReasonInternalError {
			foundInternal = true
		}
		encoded, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			t.Fatalf("marshal capture record: %v", marshalErr)
		}
		for _, forbidden := range []string{"selector-value", "field-path", "secret-data", "observed-name", "raw-kubernetes-error"} {
			if stringContains(string(encoded), forbidden) {
				t.Fatalf("forbidden sentinel %q crossed observability boundary: %s", forbidden, encoded)
			}
		}
	}
	if !foundInternal {
		t.Fatal("unknown reason did not normalize to InternalError")
	}

	assertObservabilityMetricFamilies(t, registry)
	if _, err := observability.New(observability.Options{Registerer: registry}); !errors.Is(err, observability.ErrObservabilitySetupInvalid) {
		t.Fatalf("duplicate collector registration error = %v", err)
	}

	const concurrent = 32
	var wait sync.WaitGroup
	wait.Add(concurrent)
	for index := 0; index < concurrent; index++ {
		go func() {
			defer wait.Done()
			observer.ObserveResourcesRead(context.Background(), observability.ScopeNamespaced, 1)
		}()
	}
	wait.Wait()
	assertObservabilityMetricValue(t, registry, "kubeseer_resources_read_total", `scope="namespaced"`, float64(2+concurrent))

	noOp := observability.NewNoop()
	noOpCtx, noOpAttempt := noOp.StartAttempt(context.Background(), observability.AttemptStart{Namespace: "team-a", Name: "noop"})
	noOpAttempt.Complete(noOpCtx, observability.Terminal{Outcome: observability.OutcomeCompleted, Reason: observability.ReasonEvaluationSucceeded})
}

func authorizationRecordForObservabilityCore() authorization.Record {
	return authorization.Record{}
}

func assertObservabilityMetricFamilies(t *testing.T, registry *prometheus.Registry) {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	want := map[string]map[string]bool{
		"kubeseer_reconciliations_total":           {"outcome": true, "reason": true},
		"kubeseer_reconciliation_duration_seconds": {"outcome": true},
		"kubeseer_resources_read_total":            {"scope": true},
		"kubeseer_source_failures_total":           {"stage": true, "reason": true},
		"kubeseer_results_produced_total":          {"outcome": true},
		"kubeseer_status_updates_total":            {"outcome": true, "reason": true},
		"kubeseer_authorization_decisions_total":   {"kind": true, "outcome": true, "reason": true},
		"kubeseer_jsonpath_failures_total":         {"reason": true},
		"kubeseer_source_watch_restarts_total":     {"reason": true},
	}
	if len(families) != len(want) {
		t.Fatalf("metric family count = %d, want %d", len(families), len(want))
	}
	for _, family := range families {
		labels := map[string]bool{}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				labels[label.GetName()] = true
			}
		}
		if expected, ok := want[family.GetName()]; !ok {
			t.Fatalf("unexpected metric family %q", family.GetName())
		} else if len(labels) != len(expected) {
			t.Fatalf("metric %q labels = %#v, want %#v", family.GetName(), labels, expected)
		}
	}
}

func assertObservabilityMetricValue(t *testing.T, registry *prometheus.Registry, family, label string, want float64) {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, gathered := range families {
		if gathered.GetName() != family {
			continue
		}
		for _, metric := range gathered.Metric {
			if formatLabels(metric.Label) != label {
				continue
			}
			value := metric.GetCounter().GetValue()
			if gathered.GetType().String() == "HISTOGRAM" {
				value = float64(metric.GetHistogram().GetSampleCount())
			}
			if value != want {
				t.Fatalf("metric %s{%s} = %v, want %v", family, label, value, want)
			}
			return
		}
	}
	t.Fatalf("metric %s{%s} not found", family, label)
}

func formatLabels(labels []*dto.LabelPair) string {
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, label.GetName()+"=\""+label.GetValue()+"\"")
	}
	return strings.Join(parts, ",")
}

func stringContains(value, part string) bool {
	return len(part) > 0 && len(value) >= len(part) && contains(value, part)
}

func contains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
