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
	"reflect"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func assertStatusAndConditionsPublisherScenarios(t *testing.T, ctx context.Context) {
	t.Helper()
	evaluation := publisherEvaluation(runtimeStatusResult())
	key := statusTestKey()

	t.Run("equivalent complete status suppresses writes across ordering and nil-empty drift", func(t *testing.T) {
		current := runtimeStatusObject(key, "publisher-equivalent-uid", 3, evaluation.Result)
		composeStatusFixture(t, current, evaluation)
		current.Status.Conditions = append(current.Status.Conditions[2:], current.Status.Conditions[:2]...)
		current.Status.Result.Sources[0].FieldErrors = []v1alpha1.KubeseerFieldError{}
		current.Status.Result.Sources[0].Resources[0].Fields[1].Matches = []v1alpha1.KubeseerTypedMatch{}
		writer := &runtimeStatusWriter{}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, evaluation); err != nil {
			t.Fatalf("equivalent complete publish: %v", err)
		}
		if writer.Calls() != 0 {
			t.Fatalf("equivalent complete status issued %d writes", writer.Calls())
		}
	})

	t.Run("summary hash result and canonical condition differences each repair once", func(t *testing.T) {
		mutations := []struct {
			name   string
			mutate func(*v1alpha1.Kubeseer)
		}{
			{name: "summary count", mutate: func(object *v1alpha1.Kubeseer) { object.Status.Summary.SuccessfulSources++ }},
			{name: "result hash", mutate: func(object *v1alpha1.Kubeseer) {
				object.Status.ResultHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
			}},
			{name: "result value", mutate: func(object *v1alpha1.Kubeseer) {
				value := "publisher-changed-value"
				object.Status.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue = &value
			}},
			{name: "canonical message", mutate: func(object *v1alpha1.Kubeseer) { object.Status.Conditions[0].Message = "stale canonical message" }},
		}
		for _, test := range mutations {
			t.Run(test.name, func(t *testing.T) {
				current := runtimeStatusObject(key, types.UID("publisher-difference-"+test.name), 3, evaluation.Result)
				composeStatusFixture(t, current, evaluation)
				test.mutate(current)
				writer := &runtimeStatusWriter{}
				tracker := reconciliation.NewFreshnessTracker()
				lease, release := runtimeStatusLease(t, tracker, current)
				defer release()
				if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, evaluation); err != nil {
					t.Fatalf("repair %s: %v", test.name, err)
				}
				if writer.Calls() != 1 || writer.Last() == nil {
					t.Fatalf("repair %s writes = %d object=%#v", test.name, writer.Calls(), writer.Last())
				}
				last := writer.Last()
				if !reflect.DeepEqual(last.Status.Summary, evaluationSummary(t, evaluation)) {
					t.Fatalf("repair %s summary = %#v", test.name, last.Status.Summary)
				}
				if last.Status.ResultHash == "" || last.Status.ResultHash == "sha256:0000000000000000000000000000000000000000000000000000000000000000" {
					t.Fatalf("repair %s hash = %q", test.name, last.Status.ResultHash)
				}
				if len(last.Status.Conditions) != 6 || last.Status.Conditions[5].Type != "Observed" {
					t.Fatalf("repair %s conditions = %#v", test.name, last.Status.Conditions)
				}
			})
		}
	})

	t.Run("transition times remain stable until a condition status changes", func(t *testing.T) {
		current := runtimeStatusObject(key, "publisher-transition-uid", 3, evaluation.Result)
		composeStatusFixture(t, current, evaluation)
		before := make(map[string]metav1.Time, len(current.Status.Conditions))
		for _, condition := range current.Status.Conditions {
			before[condition.Type] = condition.LastTransitionTime
		}
		writer := &runtimeStatusWriter{}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, evaluation); err != nil {
			t.Fatalf("stable transition publish: %v", err)
		}
		if writer.Calls() != 0 {
			t.Fatalf("stable transition issued %d writes", writer.Calls())
		}

		changed := evaluation
		changed.Result = evaluation.Result.DeepCopy()
		changed.Result.Sources[0].State = v1alpha1.SourceStateError
		changed.Result.Sources[0].Error = &v1alpha1.KubeseerResultError{Reason: "ReadUnavailable", Message: "sanitized"}
		writer = &runtimeStatusWriter{}
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, changed); err != nil {
			t.Fatalf("changed transition publish: %v", err)
		}
		if writer.Calls() != 1 {
			t.Fatalf("changed transition writes = %d", writer.Calls())
		}
		last := writer.Last()
		if !last.Status.Conditions[3].LastTransitionTime.After(before[statuscontract.ConditionReady].Time) || !last.Status.Conditions[4].LastTransitionTime.After(before[statuscontract.ConditionDegraded].Time) {
			t.Fatalf("status transition did not advance Ready/Degraded times: before=%#v after=%#v", before, last.Status.Conditions)
		}
		for _, condition := range last.Status.Conditions {
			if condition.Type != statuscontract.ConditionReady && condition.Type != statuscontract.ConditionDegraded {
				wantTime := before[condition.Type]
				if !condition.LastTransitionTime.Equal(&wantTime) {
					t.Fatalf("unrelated condition %s changed transition time", condition.Type)
				}
			}
		}
	})

	t.Run("unavailable evaluation clears authoritative result and derived fields", func(t *testing.T) {
		current := runtimeStatusObject(key, "publisher-unavailable-uid", 3, evaluation.Result)
		composeStatusFixture(t, current, evaluation)
		writer := &runtimeStatusWriter{}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		unavailable := statuscontract.Evaluation{Sources: evaluation.Sources, ResultUnavailable: true}
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, unavailable); err != nil {
			t.Fatalf("unavailable publish: %v", err)
		}
		if writer.Calls() != 1 || writer.Last().Status.Result != nil || writer.Last().Status.Summary != nil || writer.Last().Status.ResultHash != "" {
			t.Fatalf("unavailable status = writes=%d status=%#v", writer.Calls(), writer.Last().Status)
		}
		assertPublisherCondition(t, writer.Last().Status, statuscontract.ConditionReady, metav1.ConditionFalse, statuscontract.ReasonEvaluationUnavailable)
		assertPublisherCondition(t, writer.Last().Status, statuscontract.ConditionDegraded, metav1.ConditionTrue, statuscontract.ReasonEvaluationUnavailable)
	})

	t.Run("spec and non-canonical condition fields survive a repaired status", func(t *testing.T) {
		current := runtimeStatusObject(key, "publisher-preservation-uid", 3, evaluation.Result)
		composeStatusFixture(t, current, evaluation)
		current.Spec.Sources[0].ID = "spec-must-survive"
		current.Status.Conditions = append(current.Status.Conditions, metav1.Condition{Type: "OtherController", Status: metav1.ConditionTrue, Reason: "External", Message: "preserve this field", ObservedGeneration: 99})
		changed := evaluation
		changed.Result = evaluation.Result.DeepCopy()
		changed.Result.Sources[0].Resources[0].Name = "changed-resource"
		writer := &runtimeStatusWriter{}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, changed); err != nil {
			t.Fatalf("preservation publish: %v", err)
		}
		last := writer.Last()
		if writer.Calls() != 1 || !reflect.DeepEqual(last.Spec, current.Spec) {
			t.Fatalf("preservation spec = %#v, want %#v", last.Spec, current.Spec)
		}
		if got := last.Status.Conditions[len(last.Status.Conditions)-1]; !reflect.DeepEqual(got, current.Status.Conditions[len(current.Status.Conditions)-1]) {
			t.Fatalf("non-canonical condition = %#v, want %#v", got, current.Status.Conditions[len(current.Status.Conditions)-1])
		}
	})
}

func publisherEvaluation(result v1alpha1.KubeseerResult) statuscontract.Evaluation {
	return statuscontract.Evaluation{
		Result: result.DeepCopy(),
		Sources: []statuscontract.SourceAssessment{{
			Index:         0,
			Configuration: statuscontract.ConfigurationAcceptedOutcome,
			Authorization: statuscontract.AuthorizationAllowedOutcome,
			Resolution:    statuscontract.ResolutionResolvedOutcome,
		}},
	}
}

func composeStatusFixture(t *testing.T, object *v1alpha1.Kubeseer, evaluation statuscontract.Evaluation) {
	t.Helper()
	composed, err := statuscontract.Compose(object.Generation, object.Status.Conditions, evaluation)
	if err != nil {
		t.Fatalf("compose publisher fixture: %v", err)
	}
	object.Status = composed
}

func evaluationSummary(t *testing.T, evaluation statuscontract.Evaluation) *v1alpha1.KubeseerSummary {
	t.Helper()
	derived, err := statuscontract.DeriveResult(evaluation.Result)
	if err != nil {
		t.Fatalf("derive publisher summary: %v", err)
	}
	return derived.Summary
}

func statusTestKey() types.NamespacedName {
	return types.NamespacedName{Namespace: "team-a", Name: "status-publisher-owner"}
}

func assertPublisherCondition(t *testing.T, candidate v1alpha1.KubeseerStatus, conditionType string, conditionStatus metav1.ConditionStatus, reason string) {
	t.Helper()
	for _, condition := range candidate.Conditions {
		if condition.Type == conditionType {
			if condition.Status != conditionStatus || condition.Reason != reason {
				t.Fatalf("condition %s = %#v, want status=%s reason=%s", conditionType, condition, conditionStatus, reason)
			}
			return
		}
	}
	t.Fatalf("condition %s is missing from %#v", conditionType, candidate.Conditions)
}
