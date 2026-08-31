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

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

// EvaluateBatch evaluates source aggregations sequentially and preserves
// caller source order. It consumes completed operator outcomes and performs
// no Kubernetes I/O.
func EvaluateBatch(ctx context.Context, inputs []SourceInput) []SourceOutcome {
	if ctx == nil {
		ctx = context.Background()
	}
	if inputs == nil {
		return nil
	}
	outcomes := make([]SourceOutcome, len(inputs))
	interrupted := false
	for index, input := range inputs {
		source := inputOperatorOutcome(input)
		sourceID := input.Plan.SourceID()
		if sourceID == "" {
			sourceID = source.SourceID()
		}
		if interrupted {
			outcomes[index] = interruptedSourceOutcome(sourceID, context.Canceled)
			continue
		}
		if cause := ctx.Err(); cause != nil {
			interrupted = true
			outcomes[index] = interruptedSourceOutcome(sourceID, cause)
			continue
		}
		if input.Plan.SourceID() == "" || source.SourceID() != input.Plan.SourceID() {
			outcomes[index] = SourceOutcome{
				sourceID: sourceID,
				operator: source,
				err:      NewAggregateError(sourceID, "", "", "", nil, ReasonInvalidInput, "operator source identity does not match aggregation plan"),
			}
			continue
		}
		if source.Err() != nil {
			outcomes[index] = SourceOutcome{sourceID: sourceID, operator: source}
			continue
		}

		outcome := SourceOutcome{sourceID: sourceID, operator: source}
		for _, entry := range input.Plan.entries {
			if cause := ctx.Err(); cause != nil {
				interrupted = true
				outcomes[index] = interruptedSourceOutcome(sourceID, cause)
				break
			}
			if entry.failure != nil {
				outcome.aggregates = append(outcome.aggregates, AggregateOutcome{
					name:     entry.failure.AggregateName,
					function: entry.failure.Function,
					field:    entry.failure.FieldName,
					state:    v1alpha1.AggregateStateError,
					err:      cloneAggregateError(entry.failure),
				})
				continue
			}
			if entry.plan == nil {
				outcome.aggregates = append(outcome.aggregates, AggregateOutcome{
					state: v1alpha1.AggregateStateError,
					err:   NewAggregateError(sourceID, "", "", "", nil, ReasonInvalidInput, "aggregation plan entry is empty"),
				})
				continue
			}
			plan := cloneAggregatePlan(*entry.plan)
			grouped, interruptedErr := formGroups(ctx, plan, source)
			if interruptedErr != nil {
				interrupted = true
				outcomes[index] = interruptedSourceOutcome(sourceID, interruptedErr)
				break
			}
			if grouped.limitErr != nil {
				outcome.aggregates = append(outcome.aggregates, AggregateOutcome{
					name:     plan.name,
					function: plan.function,
					field:    plan.field,
					state:    v1alpha1.AggregateStateError,
					err:      cloneAggregateError(grouped.limitErr),
				})
				continue
			}
			if cause := ctx.Err(); cause != nil {
				interrupted = true
				outcomes[index] = interruptedSourceOutcome(sourceID, cause)
				break
			}
			if limitErr := aggregateLimitFailure(plan, grouped); limitErr != nil {
				outcome.aggregates = append(outcome.aggregates, AggregateOutcome{
					name:     plan.name,
					function: plan.function,
					field:    plan.field,
					state:    v1alpha1.AggregateStateError,
					err:      limitErr,
				})
				continue
			}
			reduced := reduceAggregate(ctx, plan, grouped.groups, grouped.failures)
			if reduced.err != nil && HasReason(reduced.err, ReasonAggregationInterrupted) {
				interrupted = true
				outcomes[index] = interruptedSourceOutcome(sourceID, reduced.err)
				break
			}
			outcome.aggregates = append(outcome.aggregates, reduced)
		}
		if outcomes[index].sourceID != "" {
			continue
		}
		outcomes[index] = outcome
	}
	return outcomes
}

func inputOperatorOutcome(input SourceInput) operators.SourceOutcome {
	if input.Operators.SourceID() != "" || input.Operators.Err() != nil || input.Operators.Resources() != nil || input.Operators.FieldErrors() != nil {
		return input.Operators
	}
	return input.Operator
}

func interruptedSourceOutcome(sourceID string, cause error) SourceOutcome {
	if cause == nil {
		cause = context.Canceled
	}
	return SourceOutcome{
		sourceID: sourceID,
		err:      aggregateErrorWithCause(sourceID, "", "", "", nil, ReasonAggregationInterrupted, "aggregation interrupted", cause),
	}
}

func aggregateLimitFailure(plan AggregatePlan, grouped groupingResult) *AggregateError {
	if len(grouped.groups) > plan.limits.MaxGroups {
		return NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonCardinalityExceeded, "aggregate group limit exceeded")
	}
	contributions := 0
	collected := 0
	distinct := 0
	provenanceEntries := 0
	for _, group := range grouped.groups {
		contributions += len(group.contributions)
		if plan.includeProvenance {
			provenanceEntries += len(group.contributors)
		}
		if plan.function == v1alpha1.AggregationCollect {
			collected += len(group.contributions)
		}
		if plan.function == v1alpha1.AggregationDistinct {
			seen := make(map[string]struct{}, len(group.contributions))
			for _, contribution := range group.contributions {
				key, err := typedoutput.CanonicalKey(plan.fieldType, contribution.match)
				if err != nil {
					continue
				}
				seen[string(key)] = struct{}{}
			}
			distinct += len(seen)
		}
		if plan.includeProvenance && (plan.function == v1alpha1.AggregationCollect || plan.function == v1alpha1.AggregationDistinct) {
			for range group.contributions {
				if plan.function == v1alpha1.AggregationCollect {
					provenanceEntries++
					continue
				}
				// Distinct value provenance is counted once per distinct
				// value/resource pair, matching the public projection.
			}
			if plan.function == v1alpha1.AggregationDistinct {
				for _, count := range distinctProvenanceCounts(group, plan) {
					provenanceEntries += count
				}
			}
		}
	}
	if contributions > plan.limits.MaxContributions {
		return NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonCardinalityExceeded, "aggregate contribution limit exceeded")
	}
	if collected > plan.limits.MaxCollectedValues {
		return NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonCardinalityExceeded, "aggregate collection limit exceeded")
	}
	if distinct > plan.limits.MaxDistinctValues {
		return NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonCardinalityExceeded, "aggregate distinct-value limit exceeded")
	}
	if provenanceEntries > plan.limits.MaxProvenanceEntries {
		return NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonCardinalityExceeded, "aggregate provenance limit exceeded")
	}
	return nil
}

func distinctProvenanceCounts(group *contributionGroup, plan AggregatePlan) map[string]int {
	counts := make(map[string]map[string]struct{})
	for _, contribution := range group.contributions {
		key, err := typedoutput.CanonicalKey(plan.fieldType, contribution.match)
		if err != nil {
			continue
		}
		valueKey := string(key)
		if _, found := counts[valueKey]; !found {
			counts[valueKey] = make(map[string]struct{})
		}
		counts[valueKey][string(contribution.provenance.UID)] = struct{}{}
	}
	output := make(map[string]int, len(counts))
	for key, values := range counts {
		output[key] = len(values)
	}
	return output
}
