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
	"sort"

	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

type contributionGroup struct {
	keys          []AggregateKey
	contributions []Contribution
	contributors  []selection.Provenance
}

type groupingResult struct {
	groups        []*contributionGroup
	failures      []ResourceFailure
	contributions int
	limitErr      *AggregateError
}

// formGroups consumes only the immutable accepted/rejected/unsuccessful
// operator outcomes. It does not read Kubernetes objects or inspect public
// status values.
func formGroups(ctx context.Context, plan AggregatePlan, source operators.SourceOutcome) (groupingResult, *AggregateError) {
	result := groupingResult{}
	resources := source.Resources()
	seenUIDs := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		if cause := ctx.Err(); cause != nil {
			return groupingResult{}, aggregateErrorWithCause(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonAggregationInterrupted, "aggregation interrupted", cause)
		}
		switch resource.State() {
		case operators.ResourceRejected:
			continue
		case operators.ResourceUnsuccessful:
			provenance := resource.Provenance()
			uid := string(provenance.UID)
			if uid != "" {
				if _, seen := seenUIDs[uid]; seen {
					continue
				}
				seenUIDs[uid] = struct{}{}
			}
			result.failures = append(result.failures, operatorResourceFailure(plan, resource))
		case operators.ResourceAccepted:
			provenance := resource.Provenance()
			if failure := validateProvenance(plan, provenance); failure != nil {
				result.failures = append(result.failures, ResourceFailure{provenance: provenance, err: failure})
				continue
			}
			uid := string(provenance.UID)
			if _, seen := seenUIDs[uid]; seen {
				continue
			}
			seenUIDs[uid] = struct{}{}
			fields := resource.Fields()
			keys, failure := groupingKeys(plan, fields, provenance)
			if failure != nil {
				result.failures = append(result.failures, ResourceFailure{provenance: provenance, err: failure})
				continue
			}
			target, targetFailure := targetField(plan, fields, provenance)
			if targetFailure != nil {
				result.failures = append(result.failures, ResourceFailure{provenance: provenance, err: targetFailure})
				continue
			}
			if target.State() == typedoutput.FieldStateAbsent {
				ensureGlobalGroup(plan, &result)
				continue
			}
			matches := target.Matches()
			if len(matches) == 0 {
				ensureGlobalGroup(plan, &result)
				continue
			}
			for matchIndex, match := range matches {
				if cause := ctx.Err(); cause != nil {
					return groupingResult{}, aggregateErrorWithCause(plan.sourceID, plan.name, plan.function, plan.field, &provenance, ReasonAggregationInterrupted, "aggregation interrupted", cause)
				}
				if match.IsNull() {
					continue
				}
				if result.contributions >= plan.limits.MaxContributions {
					result.limitErr = NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonCardinalityExceeded, "aggregate contribution limit exceeded")
					return result, nil
				}
				group := findGroup(result.groups, keys)
				if group == nil && len(result.groups) >= plan.limits.MaxGroups {
					result.limitErr = NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, nil, ReasonCardinalityExceeded, "aggregate group limit exceeded")
					return result, nil
				}
				if group == nil {
					group = findOrCreateGroup(&result, keys)
				}
				group.contributions = append(group.contributions, Contribution{match: match, provenance: provenance, matchIndex: matchIndex})
				group.contributors = appendUniqueProvenance(group.contributors, provenance)
				result.contributions++
			}
			if len(plan.groupBy) == 0 && len(result.groups) == 0 {
				ensureGlobalGroup(plan, &result)
			}
		default:
			provenance := resource.Provenance()
			result.failures = append(result.failures, ResourceFailure{provenance: provenance, err: NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, &provenance, ReasonInvalidInput, "operator resource state is unsupported")})
		}
	}
	for _, group := range result.groups {
		sort.SliceStable(group.contributions, func(left, right int) bool {
			return compareContributions(group.contributions[left], group.contributions[right]) < 0
		})
		sort.SliceStable(group.contributors, func(left, right int) bool {
			return compareProvenance(group.contributors[left], group.contributors[right]) < 0
		})
	}
	sort.SliceStable(result.groups, func(left, right int) bool {
		return compareKeyTuples(result.groups[left].keys, result.groups[right].keys) < 0
	})
	sort.SliceStable(result.failures, func(left, right int) bool {
		return compareProvenance(result.failures[left].provenance, result.failures[right].provenance) < 0
	})
	return result, nil
}

func validateProvenance(plan AggregatePlan, provenance selection.Provenance) *AggregateError {
	if provenance.APIVersion == "" || provenance.Kind == "" || provenance.Name == "" || provenance.UID == "" {
		return NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, &provenance, ReasonInvalidInput, "resource provenance is incomplete")
	}
	return nil
}

func groupingKeys(plan AggregatePlan, fields []typedoutput.FieldOutcome, provenance selection.Provenance) ([]AggregateKey, *AggregateError) {
	if len(plan.groupBy) == 0 {
		return nil, nil
	}
	keys := make([]AggregateKey, 0, len(plan.groupBy))
	for _, groupField := range plan.groupBy {
		field, found := findTypedField(fields, groupField.name)
		if !found || field.State() == typedoutput.FieldStateAbsent || field.State() == typedoutput.FieldStateError {
			return nil, NewAggregateError(plan.sourceID, plan.name, plan.function, groupField.name, &provenance, ReasonInvalidGroupKey, "grouping field does not contain exactly one non-null match")
		}
		matches := field.Matches()
		if len(matches) != 1 || matches[0].IsNull() {
			return nil, NewAggregateError(plan.sourceID, plan.name, plan.function, groupField.name, &provenance, ReasonInvalidGroupKey, "grouping field does not contain exactly one non-null match")
		}
		key, err := makeAggregateKey(groupField, matches[0])
		if err != nil {
			return nil, NewAggregateError(plan.sourceID, plan.name, plan.function, groupField.name, &provenance, ReasonInvalidGroupKey, "grouping field match is invalid")
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func targetField(plan AggregatePlan, fields []typedoutput.FieldOutcome, provenance selection.Provenance) (typedoutput.FieldOutcome, *AggregateError) {
	field, found := findTypedField(fields, plan.field)
	if !found {
		return typedoutput.FieldOutcome{}, NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, &provenance, ReasonTargetFieldError, "target field outcome is missing")
	}
	if field.State() == typedoutput.FieldStateError {
		return typedoutput.FieldOutcome{}, NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, &provenance, ReasonTargetFieldError, "target field outcome is unsuccessful")
	}
	return field, nil
}

func findTypedField(fields []typedoutput.FieldOutcome, name string) (typedoutput.FieldOutcome, bool) {
	for _, field := range fields {
		if field.Name() == name {
			return field, true
		}
	}
	return typedoutput.FieldOutcome{}, false
}

func ensureGlobalGroup(plan AggregatePlan, result *groupingResult) {
	if len(plan.groupBy) == 0 && len(result.groups) == 0 {
		result.groups = append(result.groups, &contributionGroup{})
	}
}

func findOrCreateGroup(result *groupingResult, keys []AggregateKey) *contributionGroup {
	if group := findGroup(result.groups, keys); group != nil {
		return group
	}
	copyKeys := make([]AggregateKey, len(keys))
	for index, key := range keys {
		copyKeys[index] = AggregateKey{field: key.field, typeName: key.typeName, match: key.match, canonical: append([]byte(nil), key.canonical...)}
	}
	group := &contributionGroup{keys: copyKeys}
	result.groups = append(result.groups, group)
	return group
}

func findGroup(groups []*contributionGroup, keys []AggregateKey) *contributionGroup {
	for _, group := range groups {
		if equalKeyTuples(group.keys, keys) {
			return group
		}
	}
	return nil
}

func appendUniqueProvenance(values []selection.Provenance, candidate selection.Provenance) []selection.Provenance {
	for _, value := range values {
		if compareProvenance(value, candidate) == 0 {
			return values
		}
	}
	return append(values, candidate)
}

func compareContributions(left, right Contribution) int {
	if comparison := compareProvenance(left.provenance, right.provenance); comparison != 0 {
		return comparison
	}
	if left.matchIndex < right.matchIndex {
		return -1
	}
	if left.matchIndex > right.matchIndex {
		return 1
	}
	return 0
}

func compareProvenance(left, right selection.Provenance) int {
	for _, pair := range [][2]string{
		{left.Namespace, right.Namespace},
		{left.Name, right.Name},
		{string(left.UID), string(right.UID)},
		{left.APIVersion, right.APIVersion},
		{left.Kind, right.Kind},
	} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

func operatorResourceFailure(plan AggregatePlan, resource operators.ResourceOutcome) ResourceFailure {
	provenance := resource.Provenance()
	operatorFailure := resource.Failure()
	if operatorFailure == nil {
		return ResourceFailure{provenance: provenance, err: NewAggregateError(plan.sourceID, plan.name, plan.function, plan.field, &provenance, ReasonInvalidInput, "operator resource outcome is unsuccessful")}
	}
	return ResourceFailure{
		provenance: provenance,
		err:        aggregateErrorWithCause(plan.sourceID, plan.name, plan.function, plan.field, &provenance, Reason(operatorFailure.Reason), operatorFailure.Message, operatorFailure),
	}
}
