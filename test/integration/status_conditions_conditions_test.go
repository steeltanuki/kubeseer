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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func assertStatusAndConditionsConditionScenarios(t *testing.T) {
	t.Helper()

	t.Run("complete result emits the canonical condition set", func(t *testing.T) {
		evaluation := statuscontract.Evaluation{
			Result: &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{
				{ID: "source-a", State: v1alpha1.SourceStateValues},
				{ID: "source-b", State: v1alpha1.SourceStateValues},
			}},
			Sources: []statuscontract.SourceAssessment{
				{Index: 0, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationAllowedOutcome, Resolution: statuscontract.ResolutionResolvedOutcome},
				{Index: 1, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationAllowedOutcome, Resolution: statuscontract.ResolutionResolvedOutcome},
			},
		}
		candidate, err := statuscontract.Compose(7, nil, evaluation)
		if err != nil {
			t.Fatalf("compose complete status: %v", err)
		}
		assertCanonicalConditionSet(t, candidate, 7)
		assertCondition(t, candidate.Conditions[0], statuscontract.ConditionAccepted, metav1.ConditionTrue, statuscontract.ReasonConfigurationAccepted)
		assertCondition(t, candidate.Conditions[1], statuscontract.ConditionAuthorized, metav1.ConditionTrue, statuscontract.ReasonAuthorizationSucceeded)
		assertCondition(t, candidate.Conditions[2], statuscontract.ConditionSourcesResolved, metav1.ConditionTrue, statuscontract.ReasonResolutionSucceeded)
		assertCondition(t, candidate.Conditions[3], statuscontract.ConditionReady, metav1.ConditionTrue, statuscontract.ReasonEvaluationSucceeded)
		assertCondition(t, candidate.Conditions[4], statuscontract.ConditionDegraded, metav1.ConditionFalse, statuscontract.ReasonEvaluationSucceeded)
		for _, condition := range candidate.Conditions {
			if !strings.Contains(condition.Message, "generation 7") || condition.Reason == "" || condition.Message == "" {
				t.Fatalf("condition lacks stable generation/message metadata: %#v", condition)
			}
		}
	})

	t.Run("zero sources are a successful present empty evaluation", func(t *testing.T) {
		candidate, err := statuscontract.Compose(3, nil, statuscontract.Evaluation{Result: &v1alpha1.KubeseerResult{}})
		if err != nil {
			t.Fatalf("compose zero-source status: %v", err)
		}
		assertCanonicalConditionSet(t, candidate, 3)
		assertCondition(t, candidate.Conditions[0], statuscontract.ConditionAccepted, metav1.ConditionTrue, statuscontract.ReasonConfigurationAccepted)
		assertCondition(t, candidate.Conditions[1], statuscontract.ConditionAuthorized, metav1.ConditionTrue, statuscontract.ReasonAuthorizationSucceeded)
		assertCondition(t, candidate.Conditions[2], statuscontract.ConditionSourcesResolved, metav1.ConditionTrue, statuscontract.ReasonResolutionSucceeded)
		assertCondition(t, candidate.Conditions[3], statuscontract.ConditionReady, metav1.ConditionTrue, statuscontract.ReasonEvaluationSucceeded)
		assertCondition(t, candidate.Conditions[4], statuscontract.ConditionDegraded, metav1.ConditionFalse, statuscontract.ReasonEvaluationSucceeded)
		if candidate.Result == nil || candidate.Summary == nil || *candidate.Summary != (v1alpha1.KubeseerSummary{}) || candidate.ResultHash == "" {
			t.Fatalf("zero-source result was not present with derived zero values: %#v", candidate)
		}
	})

	t.Run("first source controls differing failures", func(t *testing.T) {
		evaluation := statuscontract.Evaluation{
			Result: &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{
				{ID: "first", State: v1alpha1.SourceStateError},
				{ID: "second", State: v1alpha1.SourceStateError},
			}},
			Sources: []statuscontract.SourceAssessment{
				{Index: 0, Configuration: statuscontract.ConfigurationInvalidOutcome, Authorization: statuscontract.AuthorizationNotEvaluatedOutcome, Resolution: statuscontract.ResolutionFailedOutcome},
				{Index: 1, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationDeniedOutcome, Resolution: statuscontract.ResolutionUnavailableOutcome},
			},
		}
		candidate, err := statuscontract.Compose(9, nil, evaluation)
		if err != nil {
			t.Fatalf("compose first-failure status: %v", err)
		}
		assertCondition(t, candidate.Conditions[0], statuscontract.ConditionAccepted, metav1.ConditionFalse, statuscontract.ReasonInvalidConfiguration)
		assertCondition(t, candidate.Conditions[1], statuscontract.ConditionAuthorized, metav1.ConditionUnknown, statuscontract.ReasonAuthorizationNotEvaluated)
		assertCondition(t, candidate.Conditions[2], statuscontract.ConditionSourcesResolved, metav1.ConditionFalse, statuscontract.ReasonResolutionFailed)
		assertCondition(t, candidate.Conditions[3], statuscontract.ConditionReady, metav1.ConditionFalse, statuscontract.ReasonEvaluationDegraded)
		assertCondition(t, candidate.Conditions[4], statuscontract.ConditionDegraded, metav1.ConditionTrue, statuscontract.ReasonEvaluationDegraded)
		if !strings.Contains(candidate.Conditions[0].Message, "source 1") || !strings.Contains(candidate.Conditions[1].Message, "source 1") || !strings.Contains(candidate.Conditions[2].Message, "source 1") {
			t.Fatalf("condition messages did not identify the first affected ordinal: %#v", candidate.Conditions)
		}
	})

	t.Run("policy-wide and unavailable outcomes retain stable reasons", func(t *testing.T) {
		missing := statuscontract.AuthorizationPolicyMissingOutcome
		candidate, err := statuscontract.Compose(4, nil, statuscontract.Evaluation{
			Result:              &v1alpha1.KubeseerResult{},
			GlobalAuthorization: &missing,
		})
		if err != nil {
			t.Fatalf("compose missing-policy status: %v", err)
		}
		assertCondition(t, candidate.Conditions[1], statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonPolicyMissing)

		unavailable, err := statuscontract.Compose(4, nil, statuscontract.Evaluation{
			Sources:           []statuscontract.SourceAssessment{{Index: 0, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationUnavailableOutcome, Resolution: statuscontract.ResolutionUnavailableOutcome}},
			ResultUnavailable: true,
		})
		if err != nil {
			t.Fatalf("compose unavailable status: %v", err)
		}
		assertCondition(t, unavailable.Conditions[1], statuscontract.ConditionAuthorized, metav1.ConditionUnknown, statuscontract.ReasonAuthorizationUnavailable)
		assertCondition(t, unavailable.Conditions[2], statuscontract.ConditionSourcesResolved, metav1.ConditionUnknown, statuscontract.ReasonResolutionUnavailable)
		assertCondition(t, unavailable.Conditions[3], statuscontract.ConditionReady, metav1.ConditionFalse, statuscontract.ReasonEvaluationUnavailable)
		assertCondition(t, unavailable.Conditions[4], statuscontract.ConditionDegraded, metav1.ConditionTrue, statuscontract.ReasonEvaluationUnavailable)
		if unavailable.Result != nil || unavailable.Summary != nil || unavailable.ResultHash != "" {
			t.Fatalf("unavailable evaluation retained derived result fields: %#v", unavailable)
		}
	})

	t.Run("transition times and non-canonical conditions are preserved", func(t *testing.T) {
		persistedTime := metav1.NewTime(time.Date(2026, time.August, 27, 8, 0, 0, 0, time.UTC))
		persisted := []metav1.Condition{
			{Type: "OtherControllerCondition", Status: metav1.ConditionTrue, ObservedGeneration: 1, LastTransitionTime: persistedTime, Reason: "External", Message: "external metadata"},
		}
		evaluation := statuscontract.Evaluation{Result: &v1alpha1.KubeseerResult{}, Sources: nil}
		first, err := statuscontract.Compose(7, persisted, evaluation)
		if err != nil {
			t.Fatalf("compose initial transition status: %v", err)
		}
		second, err := statuscontract.Compose(7, first.Conditions, evaluation)
		if err != nil {
			t.Fatalf("compose stable transition status: %v", err)
		}
		for index := range first.Conditions {
			if !first.Conditions[index].LastTransitionTime.Time.Equal(second.Conditions[index].LastTransitionTime.Time) {
				t.Fatalf("condition %q transition time changed without status transition: first=%v second=%v", first.Conditions[index].Type, first.Conditions[index].LastTransitionTime, second.Conditions[index].LastTransitionTime)
			}
		}
		if len(second.Conditions) != 6 || !reflect.DeepEqual(second.Conditions[5], persisted[0]) {
			t.Fatalf("non-canonical condition was not preserved after canonical conditions: %#v", second.Conditions)
		}

		changed := statuscontract.Evaluation{Result: &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{{ID: "failed", State: v1alpha1.SourceStateError}}}, Sources: []statuscontract.SourceAssessment{{Index: 0, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationAllowedOutcome, Resolution: statuscontract.ResolutionResolvedOutcome}}}
		third, err := statuscontract.Compose(7, second.Conditions, changed)
		if err != nil {
			t.Fatalf("compose changed transition status: %v", err)
		}
		if third.Conditions[3].Status != metav1.ConditionFalse || third.Conditions[3].LastTransitionTime.Time.Equal(second.Conditions[3].LastTransitionTime.Time) {
			t.Fatalf("Ready transition did not receive a new transition time: before=%#v after=%#v", second.Conditions[3], third.Conditions[3])
		}
	})

	t.Run("invalid assessment combinations fail before publication", func(t *testing.T) {
		_, err := statuscontract.Compose(1, nil, statuscontract.Evaluation{Result: &v1alpha1.KubeseerResult{}, Sources: []statuscontract.SourceAssessment{{Index: 2, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationAllowedOutcome, Resolution: statuscontract.ResolutionResolvedOutcome}}})
		if err == nil {
			t.Fatal("non-contiguous assessment index was accepted")
		}
		_, err = statuscontract.Compose(1, nil, statuscontract.Evaluation{Result: &v1alpha1.KubeseerResult{}, ResultUnavailable: true})
		if err == nil {
			t.Fatal("result and unavailable state were accepted together")
		}
	})
}

func assertCanonicalConditionSet(t *testing.T, candidate v1alpha1.KubeseerStatus, generation int64) {
	t.Helper()
	wantTypes := []string{statuscontract.ConditionAccepted, statuscontract.ConditionAuthorized, statuscontract.ConditionSourcesResolved, statuscontract.ConditionReady, statuscontract.ConditionDegraded}
	if len(candidate.Conditions) != len(wantTypes) {
		t.Fatalf("canonical condition count = %d, want %d: %#v", len(candidate.Conditions), len(wantTypes), candidate.Conditions)
	}
	for index, wantType := range wantTypes {
		if candidate.Conditions[index].Type != wantType || candidate.Conditions[index].ObservedGeneration != generation || candidate.Conditions[index].LastTransitionTime.IsZero() {
			t.Fatalf("condition %d = %#v, want type=%q generation=%d with transition time", index, candidate.Conditions[index], wantType, generation)
		}
	}
}

func assertCondition(t *testing.T, condition metav1.Condition, wantType string, wantStatus metav1.ConditionStatus, wantReason string) {
	t.Helper()
	if condition.Type != wantType || condition.Status != wantStatus || condition.Reason != wantReason {
		t.Fatalf("condition = %#v, want type=%q status=%q reason=%q", condition, wantType, wantStatus, wantReason)
	}
}
