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

package aggregation

import (
	"fmt"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

// BuildResult projects aggregation outcomes into the existing structural
// result contract. Raw source and resource results are delegated to the
// operator adapter and aggregate outcomes are appended beside them.
//
// The public adapter is intentionally infallible for valid immutable
// outcomes. Callers that need to classify malformed internal outcomes can use
// BuildResultChecked.
func BuildResult(outcomes []SourceOutcome) v1alpha1.KubeseerResult {
	result, _ := BuildResultChecked(outcomes)
	return result
}

// BuildResultChecked is the checked form used by the reconciliation runtime.
// It preserves the existing operator projection and fails before publication
// if an internal outcome cannot be represented by the structural API.
func BuildResultChecked(outcomes []SourceOutcome) (v1alpha1.KubeseerResult, error) {
	if outcomes == nil {
		return v1alpha1.KubeseerResult{}, nil
	}

	operatorsOutcomes := make([]operators.SourceOutcome, len(outcomes))
	for index, outcome := range outcomes {
		operatorsOutcomes[index] = outcome.OperatorOutcome()
	}
	result, err := operators.BuildResult(operatorsOutcomes)
	if err != nil {
		return v1alpha1.KubeseerResult{}, fmt.Errorf("project operator result: %w", err)
	}
	if len(result.Sources) != len(outcomes) {
		return v1alpha1.KubeseerResult{}, fmt.Errorf("operator result source count %d does not match aggregation outcome count %d", len(result.Sources), len(outcomes))
	}

	for index, outcome := range outcomes {
		if len(outcome.Aggregates()) == 0 {
			continue
		}
		projected, err := projectAggregates(outcome.Aggregates())
		if err != nil {
			return v1alpha1.KubeseerResult{}, fmt.Errorf("project aggregates for source %q: %w", outcome.SourceID(), err)
		}
		result.Sources[index].Aggregates = projected
	}
	return result, nil
}

func projectAggregates(outcomes []AggregateOutcome) ([]v1alpha1.KubeseerAggregateResult, error) {
	projected := make([]v1alpha1.KubeseerAggregateResult, len(outcomes))
	for index, outcome := range outcomes {
		converted, err := projectAggregate(outcome)
		if err != nil {
			return nil, err
		}
		projected[index] = converted
	}
	return projected, nil
}

func projectAggregate(outcome AggregateOutcome) (v1alpha1.KubeseerAggregateResult, error) {
	projected := v1alpha1.KubeseerAggregateResult{
		Name:     outcome.Name(),
		Function: outcome.Function(),
		Field:    outcome.Field(),
		State:    outcome.State(),
	}
	if err := outcome.Err(); err != nil {
		projected.State = v1alpha1.AggregateStateError
		projected.Error = publicAggregateError(err)
		return projected, nil
	}

	groups := outcome.Groups()
	if len(groups) != 0 {
		projected.Groups = make([]v1alpha1.KubeseerAggregateGroup, len(groups))
		for index, group := range groups {
			converted, err := projectGroup(group)
			if err != nil {
				return v1alpha1.KubeseerAggregateResult{}, err
			}
			projected.Groups[index] = converted
		}
	}
	failures := outcome.Failures()
	if len(failures) != 0 {
		projected.Failures = make([]v1alpha1.KubeseerAggregateResourceFailure, len(failures))
		for index, failure := range failures {
			converted, err := projectResourceFailure(failure)
			if err != nil {
				return v1alpha1.KubeseerAggregateResult{}, err
			}
			projected.Failures[index] = converted
		}
	}
	return projected, nil
}

func projectGroup(group AggregateGroup) (v1alpha1.KubeseerAggregateGroup, error) {
	projected := v1alpha1.KubeseerAggregateGroup{}
	keys := group.Keys()
	if len(keys) != 0 {
		projected.Keys = make([]v1alpha1.KubeseerAggregateKey, len(keys))
		for index, key := range keys {
			value, err := typedoutput.ProjectMatch(key.Value())
			if err != nil {
				return v1alpha1.KubeseerAggregateGroup{}, fmt.Errorf("project group key %q: %w", key.Field(), err)
			}
			projected.Keys[index] = v1alpha1.KubeseerAggregateKey{
				Field: key.Field(),
				Type:  key.Type(),
				Value: value,
			}
		}
	}

	value, err := projectAggregateValue(group.Value())
	if err != nil {
		return v1alpha1.KubeseerAggregateGroup{}, err
	}
	projected.Value = value
	projected.Contributors, err = projectProvenances(group.Contributors())
	if err != nil {
		return v1alpha1.KubeseerAggregateGroup{}, err
	}
	return projected, nil
}

func projectAggregateValue(value AggregateValue) (v1alpha1.KubeseerAggregateValue, error) {
	projected := v1alpha1.KubeseerAggregateValue{
		Type:  value.Type(),
		State: value.State(),
	}
	matches := value.Matches()
	if len(matches) == 0 {
		return projected, nil
	}
	projected.Matches = make([]v1alpha1.KubeseerAggregateMatch, len(matches))
	for index, match := range matches {
		converted, err := typedoutput.ProjectMatch(match.Value())
		if err != nil {
			return v1alpha1.KubeseerAggregateValue{}, fmt.Errorf("project aggregate value match: %w", err)
		}
		contributors, err := projectProvenances(match.Contributors())
		if err != nil {
			return v1alpha1.KubeseerAggregateValue{}, err
		}
		projected.Matches[index] = v1alpha1.KubeseerAggregateMatch{Value: converted, Contributors: contributors}
	}
	return projected, nil
}

func projectResourceFailure(failure ResourceFailure) (v1alpha1.KubeseerAggregateResourceFailure, error) {
	provenance, err := projectProvenance(failure.Provenance())
	if err != nil {
		return v1alpha1.KubeseerAggregateResourceFailure{}, err
	}
	failureErr := failure.Error()
	if failureErr == nil {
		return v1alpha1.KubeseerAggregateResourceFailure{}, fmt.Errorf("aggregate resource failure for %q has no error", provenance.Name)
	}
	return v1alpha1.KubeseerAggregateResourceFailure{
		Provenance: provenance,
		Error:      *publicAggregateError(failureErr),
	}, nil
}

func projectProvenances(provenances []selection.Provenance) ([]v1alpha1.KubeseerResourceProvenance, error) {
	if len(provenances) == 0 {
		return nil, nil
	}
	projected := make([]v1alpha1.KubeseerResourceProvenance, len(provenances))
	for index, provenance := range provenances {
		converted, err := projectProvenance(provenance)
		if err != nil {
			return nil, err
		}
		projected[index] = converted
	}
	return projected, nil
}

func projectProvenance(provenance selection.Provenance) (v1alpha1.KubeseerResourceProvenance, error) {
	if provenance.APIVersion == "" || provenance.Kind == "" || provenance.Name == "" || provenance.UID == "" {
		return v1alpha1.KubeseerResourceProvenance{}, fmt.Errorf("aggregate provenance is incomplete")
	}
	return v1alpha1.KubeseerResourceProvenance{
		APIVersion: provenance.APIVersion,
		Kind:       provenance.Kind,
		Namespace:  provenance.Namespace,
		Name:       provenance.Name,
		UID:        provenance.UID,
	}, nil
}

func publicAggregateError(err *AggregateError) *v1alpha1.KubeseerResultError {
	if err == nil {
		return nil
	}
	return &v1alpha1.KubeseerResultError{Reason: string(err.Reason), Message: err.Message}
}
