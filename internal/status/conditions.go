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

package status

import (
	"errors"
	"fmt"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionAccepted               = "Accepted"
	ConditionAuthorized             = "Authorized"
	ConditionSourcesResolved        = "SourcesResolved"
	ConditionReady                  = "Ready"
	ConditionDegraded               = "Degraded"
	ReasonConfigurationAccepted     = "ConfigurationAccepted"
	ReasonInvalidConfiguration      = "InvalidConfiguration"
	ReasonAuthorizationSucceeded    = "AuthorizationSucceeded"
	ReasonAuthorizationDenied       = "AuthorizationDenied"
	ReasonAuthorizationNotEvaluated = "AuthorizationNotEvaluated"
	ReasonPolicyMissing             = "PolicyMissing"
	ReasonPolicyInvalid             = "PolicyInvalid"
	ReasonReadForbidden             = "ReadForbidden"
	ReasonAuthorizationUnavailable  = "AuthorizationUnavailable"
	ReasonResolutionSucceeded       = "ResolutionSucceeded"
	ReasonResolutionFailed          = "ResolutionFailed"
	ReasonResolutionNotEvaluated    = "ResolutionNotEvaluated"
	ReasonResolutionUnavailable     = "ResolutionUnavailable"
	ReasonEvaluationSucceeded       = "EvaluationSucceeded"
	ReasonEvaluationDegraded        = "EvaluationDegraded"
	ReasonEvaluationUnavailable     = "EvaluationUnavailable"
)

// ConfigurationOutcome is the internal configuration assessment for one
// declared source.
type ConfigurationOutcome string

const (
	ConfigurationAcceptedOutcome ConfigurationOutcome = "accepted"
	ConfigurationInvalidOutcome  ConfigurationOutcome = "invalid"
)

// AuthorizationOutcome is the internal authorization assessment for one
// source or for an installation-wide policy state.
type AuthorizationOutcome string

const (
	AuthorizationAllowedOutcome       AuthorizationOutcome = "allowed"
	AuthorizationDeniedOutcome        AuthorizationOutcome = "denied"
	AuthorizationPolicyMissingOutcome AuthorizationOutcome = "policy-missing"
	AuthorizationPolicyInvalidOutcome AuthorizationOutcome = "policy-invalid"
	AuthorizationReadForbiddenOutcome AuthorizationOutcome = "read-forbidden"
	AuthorizationUnavailableOutcome   AuthorizationOutcome = "unavailable"
	AuthorizationNotEvaluatedOutcome  AuthorizationOutcome = "not-evaluated"
)

// ResolutionOutcome is the internal resource-resolution assessment for one
// declared source.
type ResolutionOutcome string

const (
	ResolutionResolvedOutcome     ResolutionOutcome = "resolved"
	ResolutionFailedOutcome       ResolutionOutcome = "failed"
	ResolutionUnavailableOutcome  ResolutionOutcome = "unavailable"
	ResolutionNotEvaluatedOutcome ResolutionOutcome = "not-evaluated"
)

// SourceAssessment carries one source's terminal dimension outcomes. Index is
// the zero-based declaration order and is used only to make safe messages
// deterministic.
type SourceAssessment struct {
	Index         int
	Configuration ConfigurationOutcome
	Authorization AuthorizationOutcome
	Resolution    ResolutionOutcome
}

// Evaluation is the status-relevant terminal result of one reconciliation.
// A non-nil Result and ResultUnavailable are mutually exclusive.
type Evaluation struct {
	Result              *v1alpha1.KubeseerResult
	Sources             []SourceAssessment
	GlobalAuthorization *AuthorizationOutcome
	ResultUnavailable   bool
}

// Compose builds a complete status candidate for one generation. Persisted
// conditions are used only to preserve native transition timestamps and
// non-canonical condition metadata.
func Compose(generation int64, persisted []metav1.Condition, evaluation Evaluation) (v1alpha1.KubeseerStatus, error) {
	if generation < 0 {
		return v1alpha1.KubeseerStatus{}, errors.New("status generation must be non-negative")
	}
	if err := validateEvaluation(evaluation); err != nil {
		return v1alpha1.KubeseerStatus{}, err
	}

	derived, err := DeriveResult(evaluation.Result)
	if err != nil {
		return v1alpha1.KubeseerStatus{}, err
	}
	candidate := v1alpha1.KubeseerStatus{
		ObservedGeneration: generation,
		Summary:            derived.Summary,
		ResultHash:         derived.ResultHash,
		Result:             derived.Result,
	}
	desired := desiredConditions(generation, evaluation, derived)
	candidate.Conditions = mergeConditions(persisted, desired)
	return candidate, nil
}

func validateEvaluation(evaluation Evaluation) error {
	if evaluation.Result != nil && evaluation.ResultUnavailable {
		return errors.New("status evaluation cannot contain both a result and unavailable state")
	}
	if evaluation.Result == nil && !evaluation.ResultUnavailable {
		return errors.New("status evaluation without a result must be unavailable")
	}
	for index, assessment := range evaluation.Sources {
		if assessment.Index != index {
			return fmt.Errorf("status source assessment index %d is not declaration index %d", assessment.Index, index)
		}
		if !validConfigurationOutcome(assessment.Configuration) {
			return fmt.Errorf("invalid configuration outcome %q for source %d", assessment.Configuration, index+1)
		}
		if !validAuthorizationOutcome(assessment.Authorization) {
			return fmt.Errorf("invalid authorization outcome %q for source %d", assessment.Authorization, index+1)
		}
		if !validResolutionOutcome(assessment.Resolution) {
			return fmt.Errorf("invalid resolution outcome %q for source %d", assessment.Resolution, index+1)
		}
	}
	if evaluation.GlobalAuthorization != nil && !validAuthorizationOutcome(*evaluation.GlobalAuthorization) {
		return fmt.Errorf("invalid global authorization outcome %q", *evaluation.GlobalAuthorization)
	}
	return nil
}

func validConfigurationOutcome(outcome ConfigurationOutcome) bool {
	return outcome == ConfigurationAcceptedOutcome || outcome == ConfigurationInvalidOutcome
}

func validAuthorizationOutcome(outcome AuthorizationOutcome) bool {
	switch outcome {
	case AuthorizationAllowedOutcome, AuthorizationDeniedOutcome,
		AuthorizationPolicyMissingOutcome, AuthorizationPolicyInvalidOutcome,
		AuthorizationReadForbiddenOutcome, AuthorizationUnavailableOutcome,
		AuthorizationNotEvaluatedOutcome:
		return true
	default:
		return false
	}
}

func validResolutionOutcome(outcome ResolutionOutcome) bool {
	switch outcome {
	case ResolutionResolvedOutcome, ResolutionFailedOutcome,
		ResolutionUnavailableOutcome, ResolutionNotEvaluatedOutcome:
		return true
	default:
		return false
	}
}

func desiredConditions(generation int64, evaluation Evaluation, derived DerivedResult) []metav1.Condition {
	return []metav1.Condition{
		acceptedCondition(generation, evaluation.Sources),
		authorizedCondition(generation, evaluation),
		resolutionCondition(generation, evaluation.Sources),
		evaluationCondition(generation, evaluation, derived),
		degradedCondition(generation, evaluation, derived),
	}
}

func acceptedCondition(generation int64, sources []SourceAssessment) metav1.Condition {
	for _, source := range sources {
		if source.Configuration == ConfigurationInvalidOutcome {
			return condition(ConditionAccepted, metav1.ConditionFalse, ReasonInvalidConfiguration,
				fmt.Sprintf("generation %d contains invalid configuration at source %d", generation, source.Index+1), generation)
		}
	}
	return condition(ConditionAccepted, metav1.ConditionTrue, ReasonConfigurationAccepted,
		fmt.Sprintf("generation %d configuration accepted", generation), generation)
}

func authorizedCondition(generation int64, evaluation Evaluation) metav1.Condition {
	if evaluation.GlobalAuthorization != nil {
		return authorizationConditionForOutcome(generation, *evaluation.GlobalAuthorization, 0, false)
	}
	for _, source := range evaluation.Sources {
		if source.Authorization != AuthorizationAllowedOutcome {
			return authorizationConditionForOutcome(generation, source.Authorization, source.Index+1, true)
		}
	}
	return condition(ConditionAuthorized, metav1.ConditionTrue, ReasonAuthorizationSucceeded,
		fmt.Sprintf("generation %d authorization succeeded", generation), generation)
}

func authorizationConditionForOutcome(generation int64, outcome AuthorizationOutcome, ordinal int, sourceScoped bool) metav1.Condition {
	status := metav1.ConditionUnknown
	reason := ReasonAuthorizationNotEvaluated
	subject := ""
	if sourceScoped {
		subject = fmt.Sprintf(" at source %d", ordinal)
	}
	switch outcome {
	case AuthorizationAllowedOutcome:
		status = metav1.ConditionTrue
		reason = ReasonAuthorizationSucceeded
	case AuthorizationDeniedOutcome:
		status = metav1.ConditionFalse
		reason = ReasonAuthorizationDenied
	case AuthorizationPolicyMissingOutcome:
		status = metav1.ConditionFalse
		reason = ReasonPolicyMissing
	case AuthorizationPolicyInvalidOutcome:
		status = metav1.ConditionFalse
		reason = ReasonPolicyInvalid
	case AuthorizationReadForbiddenOutcome:
		status = metav1.ConditionFalse
		reason = ReasonReadForbidden
	case AuthorizationUnavailableOutcome:
		status = metav1.ConditionUnknown
		reason = ReasonAuthorizationUnavailable
	case AuthorizationNotEvaluatedOutcome:
		status = metav1.ConditionUnknown
		reason = ReasonAuthorizationNotEvaluated
	}
	return condition(ConditionAuthorized, status, reason,
		fmt.Sprintf("generation %d authorization %s%s", generation, authorizationMessage(reason), subject), generation)
}

func resolutionCondition(generation int64, sources []SourceAssessment) metav1.Condition {
	for _, source := range sources {
		if source.Resolution != ResolutionResolvedOutcome {
			status := metav1.ConditionUnknown
			reason := ReasonResolutionNotEvaluated
			message := "resolution was not evaluated"
			switch source.Resolution {
			case ResolutionFailedOutcome:
				status = metav1.ConditionFalse
				reason = ReasonResolutionFailed
				message = "resolution failed"
			case ResolutionUnavailableOutcome:
				status = metav1.ConditionUnknown
				reason = ReasonResolutionUnavailable
				message = "resolution is unavailable"
			case ResolutionNotEvaluatedOutcome:
				status = metav1.ConditionUnknown
				reason = ReasonResolutionNotEvaluated
				message = "resolution was not evaluated"
			}
			return condition(ConditionSourcesResolved, status, reason,
				fmt.Sprintf("generation %d source %d %s", generation, source.Index+1, message), generation)
		}
	}
	return condition(ConditionSourcesResolved, metav1.ConditionTrue, ReasonResolutionSucceeded,
		fmt.Sprintf("generation %d source resolution succeeded", generation), generation)
}

func evaluationCondition(generation int64, evaluation Evaluation, derived DerivedResult) metav1.Condition {
	if evaluation.ResultUnavailable {
		return condition(ConditionReady, metav1.ConditionFalse, ReasonEvaluationUnavailable,
			fmt.Sprintf("generation %d evaluation is unavailable", generation), generation)
	}
	if derived.Degraded {
		return condition(ConditionReady, metav1.ConditionFalse, ReasonEvaluationDegraded,
			fmt.Sprintf("generation %d evaluation is degraded: %s", generation, resultCountsMessage(derived.Summary)), generation)
	}
	return condition(ConditionReady, metav1.ConditionTrue, ReasonEvaluationSucceeded,
		fmt.Sprintf("generation %d evaluation succeeded: %s", generation, resultCountsMessage(derived.Summary)), generation)
}

func degradedCondition(generation int64, evaluation Evaluation, derived DerivedResult) metav1.Condition {
	if evaluation.ResultUnavailable {
		return condition(ConditionDegraded, metav1.ConditionTrue, ReasonEvaluationUnavailable,
			fmt.Sprintf("generation %d evaluation is unavailable", generation), generation)
	}
	if derived.Degraded {
		return condition(ConditionDegraded, metav1.ConditionTrue, ReasonEvaluationDegraded,
			fmt.Sprintf("generation %d evaluation is degraded: %s", generation, resultCountsMessage(derived.Summary)), generation)
	}
	return condition(ConditionDegraded, metav1.ConditionFalse, ReasonEvaluationSucceeded,
		fmt.Sprintf("generation %d evaluation is not degraded: %s", generation, resultCountsMessage(derived.Summary)), generation)
}

func resultCountsMessage(summary *v1alpha1.KubeseerSummary) string {
	if summary == nil {
		return "result unavailable"
	}
	return fmt.Sprintf("%d successful sources, %d failed sources, %d matched resources", summary.SuccessfulSources, summary.FailedSources, summary.MatchedResources)
}

func authorizationMessage(reason string) string {
	switch reason {
	case ReasonAuthorizationSucceeded:
		return "succeeded"
	case ReasonAuthorizationDenied:
		return "denied"
	case ReasonPolicyMissing:
		return "blocked because policy is missing"
	case ReasonPolicyInvalid:
		return "blocked because policy is invalid"
	case ReasonReadForbidden:
		return "forbidden by Kubernetes access"
	case ReasonAuthorizationUnavailable:
		return "unavailable"
	default:
		return "not evaluated"
	}
}

func condition(conditionType string, conditionStatus metav1.ConditionStatus, reason, message string, generation int64) metav1.Condition {
	return metav1.Condition{
		Type:               conditionType,
		Status:             conditionStatus,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	}
}

var canonicalConditionOrder = []string{
	ConditionAccepted,
	ConditionAuthorized,
	ConditionSourcesResolved,
	ConditionReady,
	ConditionDegraded,
}

// NormalizeConditions returns a copy ordered by the canonical condition
// types, followed by every non-canonical condition in its persisted order.
// Duplicate canonical entries are retained so a malformed persisted status
// remains observably different from a repaired candidate.
func NormalizeConditions(conditions []metav1.Condition) []metav1.Condition {
	if conditions == nil {
		return nil
	}
	canonical := make(map[string][]metav1.Condition, len(canonicalConditionOrder))
	nonCanonical := make([]metav1.Condition, 0, len(conditions))
	for _, item := range conditions {
		copy := item
		if isCanonicalCondition(item.Type) {
			canonical[item.Type] = append(canonical[item.Type], copy)
			continue
		}
		nonCanonical = append(nonCanonical, copy)
	}

	normalized := make([]metav1.Condition, 0, len(conditions))
	for _, conditionType := range canonicalConditionOrder {
		normalized = append(normalized, canonical[conditionType]...)
	}
	normalized = append(normalized, nonCanonical...)
	return normalized
}

func mergeConditions(persisted, desired []metav1.Condition) []metav1.Condition {
	working := make([]metav1.Condition, 0, len(persisted))
	seenCanonical := make(map[string]struct{}, len(canonicalConditionOrder))
	for _, persistedCondition := range persisted {
		if isCanonicalCondition(persistedCondition.Type) {
			if _, seen := seenCanonical[persistedCondition.Type]; seen {
				continue
			}
			seenCanonical[persistedCondition.Type] = struct{}{}
		}
		working = append(working, persistedCondition)
	}
	for _, desiredCondition := range desired {
		apiMeta.SetStatusCondition(&working, desiredCondition)
	}

	byType := make(map[string]metav1.Condition, len(desired))
	for _, item := range working {
		if isCanonicalCondition(item.Type) {
			byType[item.Type] = item
		}
	}
	merged := make([]metav1.Condition, 0, len(working))
	for _, conditionType := range canonicalConditionOrder {
		if item, found := byType[conditionType]; found {
			merged = append(merged, item)
		}
	}
	for _, item := range working {
		if !isCanonicalCondition(item.Type) {
			merged = append(merged, item)
		}
	}
	return merged
}

func isCanonicalCondition(conditionType string) bool {
	for _, canonicalType := range canonicalConditionOrder {
		if conditionType == canonicalType {
			return true
		}
	}
	return false
}
