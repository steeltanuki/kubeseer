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
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/observability"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
)

func assertObservabilityStatusScenarios(t *testing.T, ctx context.Context) {
	t.Helper()
	result := runtimeStatusResult()

	t.Run("writes one normal success Event after the semantic status write", func(t *testing.T) {
		current := runtimeStatusObject(types.NamespacedName{Namespace: "team-a", Name: "event-success"}, "event-success-uid", 1, nil)
		writer := &runtimeStatusWriter{}
		recorder := record.NewFakeRecorder(4)
		registry := newObservabilityRegistry(t)
		observer, err := observability.New(observability.Options{Registerer: registry, EventRecorder: recorder})
		if err != nil {
			t.Fatalf("construct status observer: %v", err)
		}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker, reconciliation.WithStatusObserver(observer)).Publish(ctx, lease, publisherEvaluation(result)); err != nil {
			t.Fatalf("publish successful status: %v", err)
		}
		if writer.Calls() != 1 {
			t.Fatalf("status writes = %d, want one", writer.Calls())
		}
		event := requireFakeEvent(t, recorder)
		if !strings.HasPrefix(event, corev1.EventTypeNormal+" "+string(observability.ReasonEvaluationSucceeded)+" ") || !strings.Contains(event, "evaluation succeeded") {
			t.Fatalf("success Event = %q", event)
		}
		assertObservabilityMetricValue(t, registry, "kubeseer_status_updates_total", `outcome="written",reason="EvaluationSucceeded"`, 1)
	})

	priorityTests := []struct {
		name       string
		evaluation statuscontract.Evaluation
		wantReason string
	}{
		{
			name:       "Accepted wins",
			evaluation: statuscontract.Evaluation{Result: result.DeepCopy(), Sources: []statuscontract.SourceAssessment{{Index: 0, Configuration: statuscontract.ConfigurationInvalidOutcome, Authorization: statuscontract.AuthorizationNotEvaluatedOutcome, Resolution: statuscontract.ResolutionNotEvaluatedOutcome}}},
			wantReason: statuscontract.ReasonInvalidConfiguration,
		},
		{
			name:       "Authorized wins after Accepted",
			evaluation: statuscontract.Evaluation{Result: result.DeepCopy(), Sources: []statuscontract.SourceAssessment{{Index: 0, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationDeniedOutcome, Resolution: statuscontract.ResolutionResolvedOutcome}}},
			wantReason: statuscontract.ReasonAuthorizationDenied,
		},
		{
			name:       "SourcesResolved wins after earlier success",
			evaluation: statuscontract.Evaluation{Result: result.DeepCopy(), Sources: []statuscontract.SourceAssessment{{Index: 0, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationAllowedOutcome, Resolution: statuscontract.ResolutionFailedOutcome}}},
			wantReason: statuscontract.ReasonResolutionFailed,
		},
		{
			name:       "Ready wins after earlier success",
			evaluation: statuscontract.Evaluation{Result: func() *v1alpha1.KubeseerResult { value := degradedStatusResult(); return &value }(), Sources: []statuscontract.SourceAssessment{{Index: 0, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationAllowedOutcome, Resolution: statuscontract.ResolutionResolvedOutcome}}},
			wantReason: statuscontract.ReasonEvaluationDegraded,
		},
	}
	for _, test := range priorityTests {
		t.Run(test.name, func(t *testing.T) {
			current := runtimeStatusObject(types.NamespacedName{Namespace: "team-a", Name: "event-priority"}, types.UID("event-"+strings.ToLower(strings.ReplaceAll(test.name, " ", "-"))), 1, nil)
			writer := &runtimeStatusWriter{}
			recorder := record.NewFakeRecorder(2)
			observer, err := observability.New(observability.Options{Registerer: newObservabilityRegistry(t), EventRecorder: recorder})
			if err != nil {
				t.Fatalf("construct priority observer: %v", err)
			}
			tracker := reconciliation.NewFreshnessTracker()
			lease, release := runtimeStatusLease(t, tracker, current)
			defer release()
			if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker, reconciliation.WithStatusObserver(observer)).Publish(ctx, lease, test.evaluation); err != nil {
				t.Fatalf("publish %s: %v", test.name, err)
			}
			event := requireFakeEvent(t, recorder)
			if !strings.HasPrefix(event, corev1.EventTypeWarning+" "+test.wantReason+" ") {
				t.Fatalf("priority Event = %q, want Warning %s", event, test.wantReason)
			}
		})
	}

	t.Run("skips, conflicts, failures, and Event delivery errors stay passive", func(t *testing.T) {
		current := runtimeStatusObject(types.NamespacedName{Namespace: "team-a", Name: "event-suppressed"}, "event-suppressed-uid", 1, result.DeepCopy())
		evaluation := publisherEvaluation(result)
		composeStatusFixture(t, current, evaluation)
		recorder := record.NewFakeRecorder(8)
		observer, err := observability.New(observability.Options{Registerer: newObservabilityRegistry(t), EventRecorder: recorder})
		if err != nil {
			t.Fatalf("construct suppression observer: %v", err)
		}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		writer := &runtimeStatusWriter{}
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker, reconciliation.WithStatusObserver(observer)).Publish(ctx, lease, evaluation); err != nil {
			t.Fatalf("semantic skip: %v", err)
		}
		if writer.Calls() != 0 || len(recorder.Events) != 0 {
			t.Fatalf("semantic skip writes/events = %d/%d", writer.Calls(), len(recorder.Events))
		}

		conflictRecorder := record.NewFakeRecorder(2)
		conflictObserver, err := observability.New(observability.Options{Registerer: newObservabilityRegistry(t), EventRecorder: conflictRecorder})
		if err != nil {
			t.Fatalf("construct conflict observer: %v", err)
		}
		conflictWriter := &runtimeStatusWriter{err: apierrors.NewConflict(schema.GroupResource{Group: "kubeseer.io", Resource: "kubeseers"}, "event-suppressed", errors.New("raw conflict body"))}
		conflictTracker := reconciliation.NewFreshnessTracker()
		conflictLease, conflictRelease := runtimeStatusLease(t, conflictTracker, current)
		defer conflictRelease()
		conflictErr := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), conflictWriter, conflictTracker, reconciliation.WithStatusObserver(conflictObserver)).Publish(ctx, conflictLease, publisherEvaluation(v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{{ID: "different"}}}))
		if conflictErr == nil || !reconciliation.IsRetryable(conflictErr) || len(conflictRecorder.Events) != 0 {
			t.Fatalf("conflict result = err=%v events=%d", conflictErr, len(conflictRecorder.Events))
		}

		failingRecorder := &panicEventRecorder{}
		failingObserver, err := observability.New(observability.Options{Registerer: newObservabilityRegistry(t), EventRecorder: failingRecorder})
		if err != nil {
			t.Fatalf("construct failing-recorder observer: %v", err)
		}
		written := &runtimeStatusWriter{}
		writeTracker := reconciliation.NewFreshnessTracker()
		writeLease, writeRelease := runtimeStatusLease(t, writeTracker, runtimeStatusObject(types.NamespacedName{Namespace: "team-a", Name: "event-failing-recorder"}, "event-failing-uid", 1, nil))
		defer writeRelease()
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(runtimeStatusObject(types.NamespacedName{Namespace: "team-a", Name: "event-failing-recorder"}, "event-failing-uid", 1, nil)), written, writeTracker, reconciliation.WithStatusObserver(failingObserver)).Publish(ctx, writeLease, evaluation); err != nil {
			t.Fatalf("failing Event recorder changed status result: %v", err)
		}
		if written.Calls() != 1 {
			t.Fatalf("failing Event recorder status writes = %d", written.Calls())
		}
	})

	t.Run("malformed canonical status falls back without leaking the message", func(t *testing.T) {
		recorder := record.NewFakeRecorder(2)
		observer, err := observability.New(observability.Options{Registerer: newObservabilityRegistry(t), EventRecorder: recorder})
		if err != nil {
			t.Fatalf("construct malformed observer: %v", err)
		}
		owner := &v1alpha1.Kubeseer{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "malformed-event", UID: "malformed-event-uid"}}
		observer.RecordStatusEvent(ctx, owner, []metav1.Condition{{Type: statuscontract.ConditionAccepted, Status: metav1.ConditionFalse, Reason: "user-controlled", Message: "secret-data"}})
		event := requireFakeEvent(t, recorder)
		if !strings.HasPrefix(event, corev1.EventTypeWarning+" "+string(observability.ReasonInternalError)+" ") || strings.Contains(event, "secret-data") || strings.Contains(event, "user-controlled") {
			t.Fatalf("malformed Event = %q", event)
		}
	})
}

func newObservabilityRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	return prometheus.NewRegistry()
}

func requireFakeEvent(t *testing.T, recorder *record.FakeRecorder) string {
	t.Helper()
	select {
	case event := <-recorder.Events:
		return event
	default:
		t.Fatal("expected one Kubernetes Event")
		return ""
	}
}

func degradedStatusResult() v1alpha1.KubeseerResult {
	return v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{{ID: "degraded-source", State: v1alpha1.SourceStateError, Error: &v1alpha1.KubeseerResultError{Reason: "ReadUnavailable", Message: "sanitized"}}}}
}

type panicEventRecorder struct{}

func (*panicEventRecorder) Event(runtime.Object, string, string, string) {
	panic("event recorder unavailable")
}

func (*panicEventRecorder) Eventf(runtime.Object, string, string, string, ...interface{}) {
	panic("event recorder unavailable")
}

func (*panicEventRecorder) AnnotatedEventf(runtime.Object, map[string]string, string, string, string, ...interface{}) {
	panic("event recorder unavailable")
}
