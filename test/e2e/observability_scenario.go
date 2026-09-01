// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func scenarioStatusObservability(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-015")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-015 namespace: %v", err)
	}
	if _, err := fixtures.CreateSentinelSecret(ctx, fixtures.scopedName("diagnostic-sentinel"), namespace); err != nil {
		t.Fatalf("E2E-015 diagnostic sentinel: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	pod, err := fixtures.CreatePod(ctx, fixtures.scopedName("observable"), namespace, nil, map[string]interface{}{"value": "observable"})
	if err != nil {
		t.Fatalf("E2E-015 source Pod: %v", err)
	}
	beforeMetric := metricCounter(t, session, "kubeseer_status_updates_total")
	owner, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("observable", "v1", "Pod", []string{namespace}, map[string]interface{}{"name": pod.GetName()}, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)}})
	if err != nil {
		t.Fatalf("E2E-015 Kubeseer: %v", err)
	}
	snapshot := mustReadySnapshot(t, session, ctx, owner, "E2E-015")
	assertReadyStatus(t, snapshot, []string{"observable"})
	if snapshot.ResultHash == "" || len(snapshot.ResultHash) != len("sha256:")+64 {
		t.Fatalf("E2E-015 deterministic result hash = %q", snapshot.ResultHash)
	}
	repeated, err := session.GetKubeseer(ctx, namespace, owner.GetName())
	if err != nil {
		t.Fatalf("E2E-015 reread status: %v", err)
	}
	if repeated.Status.ResultHash != snapshot.ResultHash {
		t.Fatalf("E2E-015 result hash changed without a generation transition")
	}
	afterMetric := metricCounter(t, session, "kubeseer_status_updates_total")
	if afterMetric < 0 || beforeMetric >= 0 && afterMetric <= beforeMetric {
		t.Fatalf("E2E-015 status metric did not increase: before=%v after=%v", beforeMetric, afterMetric)
	}
	for _, expected := range []struct {
		typeName string
		status   metav1.ConditionStatus
		reason   string
	}{
		{"Accepted", metav1.ConditionTrue, "ConfigurationAccepted"},
		{"Authorized", metav1.ConditionTrue, "AuthorizationSucceeded"},
		{"SourcesResolved", metav1.ConditionTrue, "ResolutionSucceeded"},
		{"Ready", metav1.ConditionTrue, "EvaluationSucceeded"},
		{"Degraded", metav1.ConditionFalse, "EvaluationSucceeded"},
	} {
		found := false
		for _, condition := range snapshot.Conditions {
			if string(condition.Type) == expected.typeName && condition.Status == expected.status && condition.Reason == expected.reason {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("E2E-015 canonical condition missing: %#v", expected)
		}
	}
	metric := metricCounter(t, session, "kubeseer_status_updates_total")
	if metric < 0 {
		t.Fatalf("E2E-015 status metric endpoint was not reachable")
	}
	if err := session.WaitForEvent(ctx, namespace, "involvedObject.name="+owner.GetName(), "EvaluationSucceeded"); err != nil {
		t.Fatalf("E2E-015 status Event: %v", err)
	}
	var records []RecordIdentity
	if err := session.WaitFor(ctx, "E2E-015", "reconciliation attempt correlation", func(waitCtx context.Context) (bool, error) {
		var readErr error
		records, readErr = session.ReadManagerRecordIdentities(waitCtx, namespace, owner.GetName())
		if readErr != nil {
			return false, readErr
		}
		return hasAttemptCorrelation(records), nil
	}); err != nil {
		t.Fatalf("E2E-015 reconciliation records: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("E2E-015 no allowlisted reconciliation records")
	}
	snapshot.MetricFamilies = []MetricSample{{Family: "kubeseer_status_updates_total", Value: afterMetric}}
	snapshot.RecordCodes = make([]string, 0, len(records))
	for _, record := range records {
		snapshot.RecordCodes = append(snapshot.RecordCodes, record.Code())
	}
	snapshot.EventIdentities, err = session.ListEvents(ctx, namespace, "involvedObject.name="+owner.GetName())
	if err != nil {
		t.Fatalf("E2E-015 status Event identities: %v", err)
	}
	if err := ValidateDiagnosticSentinels(session.Metadata.Diagnostics); err != nil {
		t.Fatalf("E2E-015 diagnostic sentinel validation: %v", err)
	}
}

func hasAttemptCorrelation(records []RecordIdentity) bool {
	started := make(map[string]struct{})
	authorized := make(map[string]struct{})
	terminal := make(map[string]struct{})
	for _, record := range records {
		switch record.Event {
		case "ReconciliationStarted":
			started[record.AttemptID] = struct{}{}
		case "AuthorizationDecision":
			authorized[record.AttemptID] = struct{}{}
		case "ReconciliationCompleted", "ReconciliationFailed", "ReconciliationSkipped", "ReconciliationRetryScheduled":
			terminal[record.AttemptID] = struct{}{}
		}
	}
	for attempt := range started {
		if _, ok := authorized[attempt]; ok {
			if _, ok := terminal[attempt]; ok {
				return true
			}
		}
	}
	return false
}
