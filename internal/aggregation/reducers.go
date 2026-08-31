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
	"context"
	"math/big"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func reduceAggregate(ctx context.Context, plan AggregatePlan, groups []*contributionGroup, failures []ResourceFailure) AggregateOutcome {
	outcome := AggregateOutcome{
		name:     plan.name,
		function: plan.function,
		field:    plan.field,
		state:    v1alpha1.AggregateStateValues,
		failures: cloneResourceFailures(failures),
	}
	if len(failures) != 0 {
		outcome.state = v1alpha1.AggregateStateDegraded
	}
	if len(groups) == 0 && len(plan.groupBy) == 0 {
		groups = []*contributionGroup{{}}
	}
	if len(groups) != 0 {
		outcome.groups = make([]AggregateGroup, 0, len(groups))
	}
	for _, group := range groups {
		if cause := ctx.Err(); cause != nil {
			outcome.state = v1alpha1.AggregateStateError
			outcome.groups = nil
			outcome.failures = nil
			outcome.err = interruptedReducerError(plan, cause)
			return outcome
		}
		value, err := reduceGroup(ctx, plan, group)
		if err != nil {
			outcome.state = v1alpha1.AggregateStateError
			outcome.groups = nil
			outcome.failures = nil
			outcome.err = err
			return outcome
		}
		var publicContributors []selection.Provenance
		if plan.includeProvenance {
			publicContributors = append([]selection.Provenance(nil), group.contributors...)
		}
		outcome.groups = append(outcome.groups, AggregateGroup{
			keys:         cloneAggregateKeys(group.keys),
			value:        value,
			contributors: publicContributors,
		})
	}
	return outcome
}

func reduceGroup(ctx context.Context, plan AggregatePlan, group *contributionGroup) (AggregateValue, *AggregateError) {
	value := AggregateValue{typeName: plan.fieldType, state: v1alpha1.AggregateValueValues}
	contributions := group.contributions
	switch plan.function {
	case v1alpha1.AggregationCollect:
		value.matches = make([]AggregateValueMatch, 0, len(contributions))
		for _, contribution := range contributions {
			if cause := ctx.Err(); cause != nil {
				return AggregateValue{}, interruptedReducerError(plan, cause)
			}
			value.matches = append(value.matches, AggregateValueMatch{
				match:        contribution.match,
				contributors: provenanceForValue(plan, contribution),
			})
		}
		return value, nil
	case v1alpha1.AggregationCount:
		match, err := typedoutput.ConvertConfiguredMatch(plan.sourceID, plan.name, v1alpha1.ValueTypeInteger, int64(len(contributions)))
		if err != nil {
			return AggregateValue{}, reducerError(plan, ReasonInvalidInput, "count result could not be constructed", err)
		}
		value.typeName = v1alpha1.ValueTypeInteger
		value.matches = []AggregateValueMatch{{match: match}}
		return value, nil
	case v1alpha1.AggregationSum:
		match, err := sumMatch(ctx, plan, contributions)
		if err != nil {
			return AggregateValue{}, err
		}
		value.matches = []AggregateValueMatch{{match: match}}
		return value, nil
	case v1alpha1.AggregationAverage:
		if len(contributions) == 0 {
			value.state = v1alpha1.AggregateValueAbsent
			return value, nil
		}
		total, err := exactContributionSum(ctx, plan, contributions)
		if err != nil {
			return AggregateValue{}, err
		}
		mean := new(big.Rat).Quo(total, new(big.Rat).SetInt64(int64(len(contributions))))
		match, matchErr := typedoutput.MatchFromExactNumber(mean, plan.precision, plan.roundingMode)
		if matchErr != nil {
			return AggregateValue{}, reducerError(plan, ReasonOverflow, "average result could not be represented", matchErr)
		}
		value.typeName = v1alpha1.ValueTypeNumber
		value.matches = []AggregateValueMatch{{match: match}}
		return value, nil
	case v1alpha1.AggregationMin, v1alpha1.AggregationMax:
		if len(contributions) == 0 {
			value.state = v1alpha1.AggregateValueAbsent
			return value, nil
		}
		selected := contributions[0].match
		for _, contribution := range contributions[1:] {
			if cause := ctx.Err(); cause != nil {
				return AggregateValue{}, interruptedReducerError(plan, cause)
			}
			comparison, err := typedoutput.CompareMatches(contribution.match, selected)
			if err != nil {
				return AggregateValue{}, reducerError(plan, ReasonInvalidInput, "extrema comparison is invalid", err)
			}
			if (plan.function == v1alpha1.AggregationMin && comparison < 0) || (plan.function == v1alpha1.AggregationMax && comparison > 0) {
				selected = contribution.match
			}
		}
		value.matches = []AggregateValueMatch{{match: selected}}
		return value, nil
	case v1alpha1.AggregationFirst:
		if len(contributions) == 0 {
			value.state = v1alpha1.AggregateValueAbsent
			return value, nil
		}
		value.matches = []AggregateValueMatch{{match: contributions[0].match}}
		return value, nil
	case v1alpha1.AggregationLast:
		if len(contributions) == 0 {
			value.state = v1alpha1.AggregateValueAbsent
			return value, nil
		}
		value.matches = []AggregateValueMatch{{match: contributions[len(contributions)-1].match}}
		return value, nil
	case v1alpha1.AggregationDistinct:
		seen := make(map[string]int, len(contributions))
		for _, contribution := range contributions {
			if cause := ctx.Err(); cause != nil {
				return AggregateValue{}, interruptedReducerError(plan, cause)
			}
			key, err := typedoutput.CanonicalKey(plan.fieldType, contribution.match)
			if err != nil {
				return AggregateValue{}, reducerError(plan, ReasonInvalidInput, "distinct value key is invalid", err)
			}
			keyText := string(key)
			index, found := seen[keyText]
			if !found {
				seen[keyText] = len(value.matches)
				value.matches = append(value.matches, AggregateValueMatch{match: contribution.match, contributors: provenanceForValue(plan, contribution)})
				continue
			}
			if plan.includeProvenance {
				value.matches[index].contributors = appendUniqueProvenance(value.matches[index].contributors, contribution.provenance)
			}
		}
		return value, nil
	default:
		return AggregateValue{}, reducerError(plan, ReasonInvalidInput, "aggregate function is not supported", nil)
	}
}

func sumMatch(ctx context.Context, plan AggregatePlan, contributions []Contribution) (typedoutput.Match, *AggregateError) {
	total, err := exactContributionSum(ctx, plan, contributions)
	if err != nil {
		return typedoutput.Match{}, err
	}
	match, matchErr := typedoutput.MatchFromExact(plan.fieldType, total)
	if matchErr != nil {
		return typedoutput.Match{}, reducerError(plan, ReasonOverflow, "sum result could not be represented", matchErr)
	}
	return match, nil
}

func exactContributionSum(ctx context.Context, plan AggregatePlan, contributions []Contribution) (*big.Rat, *AggregateError) {
	total := new(big.Rat)
	for _, contribution := range contributions {
		if cause := ctx.Err(); cause != nil {
			return nil, interruptedReducerError(plan, cause)
		}
		value, err := typedoutput.ExactRational(plan.fieldType, contribution.match)
		if err != nil {
			return nil, reducerError(plan, ReasonInvalidInput, "numeric contribution is invalid", err)
		}
		total.Add(total, value)
	}
	return total, nil
}

func interruptedReducerError(plan AggregatePlan, cause error) *AggregateError {
	return aggregateErrorWithCause(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonAggregationInterrupted, "aggregation interrupted", cause)
}

func provenanceForValue(plan AggregatePlan, contribution Contribution) []selection.Provenance {
	if !plan.includeProvenance {
		return nil
	}
	return []selection.Provenance{contribution.provenance}
}

func reducerError(plan AggregatePlan, reason Reason, message string, cause error) *AggregateError {
	return aggregateErrorWithCause(plan.sourceID, plan.name, plan.function, plan.field, nil, reason, message, cause)
}

func cloneAggregateKeys(input []AggregateKey) []AggregateKey {
	if input == nil {
		return nil
	}
	output := make([]AggregateKey, len(input))
	for index, key := range input {
		output[index] = AggregateKey{field: key.field, typeName: key.typeName, match: key.match, canonical: append([]byte(nil), key.canonical...)}
	}
	return output
}

func cloneResourceFailures(input []ResourceFailure) []ResourceFailure {
	if input == nil {
		return nil
	}
	output := make([]ResourceFailure, len(input))
	for index, failure := range input {
		output[index] = ResourceFailure{provenance: cloneProvenance(failure.provenance), err: cloneAggregateError(failure.err)}
	}
	return output
}
