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
	"sort"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

const defaultAveragePrecision int32 = 6

// PlanSource validates every aggregate declaration before evaluation. Valid
// declarations and independent failures are retained in lexical name order;
// no declaration cache is consulted or populated.
func PlanSource(source v1alpha1.KubeseerSource, limits Limits) PlanOutcome {
	limits = normalizeLimits(limits)
	declarations := append([]v1alpha1.KubeseerAggregation(nil), source.Aggregations...)
	sort.SliceStable(declarations, func(left, right int) bool {
		return declarations[left].Name < declarations[right].Name
	})

	outcome := PlanOutcome{sourceID: source.ID, limits: limits}
	if source.ID == "" {
		outcome.entries = append(outcome.entries, planEntry{failure: planningFailure(source.ID, "", "", "", ReasonInvalidInput, "source identity is invalid")})
		return outcome
	}

	fieldTypes := make(map[string]v1alpha1.KubeseerValueType, len(source.Fields))
	for _, field := range source.Fields {
		if _, exists := fieldTypes[field.Name]; !exists {
			fieldTypes[field.Name] = field.Type
		}
	}
	nameCounts := make(map[string]int, len(declarations))
	for _, declaration := range declarations {
		nameCounts[declaration.Name]++
	}

	for _, declaration := range declarations {
		if nameCounts[declaration.Name] > 1 {
			outcome.entries = append(outcome.entries, planEntry{failure: planningFailure(source.ID, declaration.Name, declaration.Function, declaration.Field, ReasonDuplicateAggregate, "aggregate name is duplicated")})
			continue
		}
		plan, failure := planDeclaration(source.ID, declaration, fieldTypes, limits)
		if failure != nil {
			outcome.entries = append(outcome.entries, planEntry{failure: failure})
			continue
		}
		outcome.entries = append(outcome.entries, planEntry{plan: &plan})
	}
	return outcome
}

// PlanBatch plans all sources independently and retains caller declaration
// order. It is a convenience for the production pipeline.
func PlanBatch(sources []v1alpha1.KubeseerSource, limits Limits) []PlanOutcome {
	if sources == nil {
		return nil
	}
	outcomes := make([]PlanOutcome, len(sources))
	for index, source := range sources {
		outcomes[index] = PlanSource(source, limits)
	}
	return outcomes
}

func planDeclaration(sourceID string, declaration v1alpha1.KubeseerAggregation, fieldTypes map[string]v1alpha1.KubeseerValueType, limits Limits) (AggregatePlan, *AggregateError) {
	if declaration.Name == "" {
		return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, declaration.Field, ReasonInvalidInput, "aggregate name is invalid")
	}
	if declaration.Field == "" {
		return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, declaration.Field, ReasonUnknownField, "aggregate target field is not declared")
	}
	fieldType, found := fieldTypes[declaration.Field]
	if !found {
		return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, declaration.Field, ReasonUnknownField, "aggregate target field is not declared")
	}
	if !typedoutput.IsSupportedType(fieldType) {
		return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, declaration.Field, ReasonUnknownField, "aggregate target field type is unsupported")
	}
	if !compatibleFunction(declaration.Function, fieldType) {
		return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, declaration.Field, ReasonIncompatibleFunction, "aggregate function is incompatible with the target field type")
	}

	groupBy := make([]GroupFieldPlan, 0, len(declaration.GroupBy))
	seenGroupFields := make(map[string]struct{}, len(declaration.GroupBy))
	for _, groupField := range declaration.GroupBy {
		if _, duplicate := seenGroupFields[groupField]; duplicate {
			return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, groupField, ReasonDuplicateGroupField, "grouping field is duplicated")
		}
		seenGroupFields[groupField] = struct{}{}
		groupType, found := fieldTypes[groupField]
		if !found {
			return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, groupField, ReasonUnknownField, "grouping field is not declared")
		}
		if !supportedGroupType(groupType) {
			return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, groupField, ReasonUnsupportedGroupType, "grouping field type is not orderable")
		}
		groupBy = append(groupBy, GroupFieldPlan{name: groupField, typeName: groupType})
	}

	precision := defaultAveragePrecision
	roundingMode := v1alpha1.RoundingHalfEven
	if declaration.Function != v1alpha1.AggregationAverage {
		if declaration.Precision != nil || declaration.RoundingMode != "" {
			return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, declaration.Field, ReasonInvalidAverageOptions, "precision and roundingMode are valid only for average")
		}
		precision = 0
		roundingMode = ""
	} else {
		if declaration.Precision != nil {
			precision = *declaration.Precision
		}
		if precision < 0 || precision > 18 {
			return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, declaration.Field, ReasonInvalidAverageOptions, "average precision is outside the supported range")
		}
		if declaration.RoundingMode != "" {
			roundingMode = declaration.RoundingMode
		}
		if !supportedRoundingMode(roundingMode) {
			return AggregatePlan{}, planningFailure(sourceID, declaration.Name, declaration.Function, declaration.Field, ReasonInvalidAverageOptions, "average roundingMode is unsupported")
		}
	}

	return AggregatePlan{
		sourceID:          sourceID,
		name:              declaration.Name,
		function:          declaration.Function,
		field:             declaration.Field,
		fieldType:         fieldType,
		groupBy:           groupBy,
		includeProvenance: declaration.IncludeProvenance,
		precision:         precision,
		roundingMode:      roundingMode,
		limits:            limits,
	}, nil
}

func compatibleFunction(function v1alpha1.KubeseerAggregationFunction, fieldType v1alpha1.KubeseerValueType) bool {
	switch function {
	case v1alpha1.AggregationCollect, v1alpha1.AggregationCount,
		v1alpha1.AggregationFirst, v1alpha1.AggregationLast,
		v1alpha1.AggregationDistinct:
		return typedoutput.IsSupportedType(fieldType)
	case v1alpha1.AggregationSum:
		return fieldType == v1alpha1.ValueTypeInteger || fieldType == v1alpha1.ValueTypeNumber || fieldType == v1alpha1.ValueTypeDuration || fieldType == v1alpha1.ValueTypeQuantity
	case v1alpha1.AggregationAverage:
		return fieldType == v1alpha1.ValueTypeInteger || fieldType == v1alpha1.ValueTypeNumber
	case v1alpha1.AggregationMin, v1alpha1.AggregationMax:
		return supportedGroupType(fieldType)
	default:
		return false
	}
}

func supportedGroupType(fieldType v1alpha1.KubeseerValueType) bool {
	switch fieldType {
	case v1alpha1.ValueTypeString, v1alpha1.ValueTypeInteger,
		v1alpha1.ValueTypeNumber, v1alpha1.ValueTypeBoolean,
		v1alpha1.ValueTypeTimestamp, v1alpha1.ValueTypeDuration,
		v1alpha1.ValueTypeQuantity:
		return true
	default:
		return false
	}
}

func supportedRoundingMode(mode v1alpha1.KubeseerRoundingMode) bool {
	switch mode {
	case v1alpha1.RoundingHalfEven, v1alpha1.RoundingHalfAwayFromZero,
		v1alpha1.RoundingTowardZero, v1alpha1.RoundingAwayFromZero:
		return true
	default:
		return false
	}
}

func planningFailure(sourceID, aggregateName string, function v1alpha1.KubeseerAggregationFunction, fieldName string, reason Reason, message string) *AggregateError {
	return NewAggregateError(sourceID, aggregateName, function, fieldName, nil, reason, message)
}
