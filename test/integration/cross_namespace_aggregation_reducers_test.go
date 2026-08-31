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
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/selection"
)

func assertCrossNamespaceAggregationReducerScenarios(t *testing.T) {
	t.Helper()
	t.Run("closed reducers cover every compatible logical type", func(t *testing.T) {
		type fieldCase struct {
			name      string
			typeName  v1alpha1.KubeseerValueType
			first     any
			second    any
			wantCount int
		}
		cases := []fieldCase{
			{name: "string", typeName: v1alpha1.ValueTypeString, first: "b", second: "a", wantCount: 2},
			{name: "integer", typeName: v1alpha1.ValueTypeInteger, first: int64(2), second: int64(1), wantCount: 2},
			{name: "number", typeName: v1alpha1.ValueTypeNumber, first: "1.0", second: "1", wantCount: 2},
			{name: "boolean", typeName: v1alpha1.ValueTypeBoolean, first: true, second: false, wantCount: 2},
			{name: "timestamp", typeName: v1alpha1.ValueTypeTimestamp, first: "2026-08-31T01:00:00+01:00", second: "2026-08-31T00:00:00Z", wantCount: 2},
			{name: "duration", typeName: v1alpha1.ValueTypeDuration, first: "2s", second: "1s", wantCount: 2},
			{name: "quantity", typeName: v1alpha1.ValueTypeQuantity, first: "2Gi", second: "2048Mi", wantCount: 2},
			{name: "object", typeName: v1alpha1.ValueTypeObject, first: map[string]any{"b": int64(2), "a": true}, second: map[string]any{"a": true, "b": int64(2)}, wantCount: 2},
			{name: "list", typeName: v1alpha1.ValueTypeList, first: []any{"b", int64(2)}, second: []any{"a", int64(1)}, wantCount: 2},
		}
		source := v1alpha1.KubeseerSource{ID: "reducer-types"}
		for _, test := range cases {
			source.Fields = append(source.Fields, v1alpha1.KubeseerField{Name: test.name, Path: "{.data." + test.name + "}", Type: test.typeName})
			for _, function := range []v1alpha1.KubeseerAggregationFunction{
				v1alpha1.AggregationCollect, v1alpha1.AggregationCount,
				v1alpha1.AggregationFirst, v1alpha1.AggregationLast,
				v1alpha1.AggregationDistinct,
			} {
				source.Aggregations = append(source.Aggregations, v1alpha1.KubeseerAggregation{
					Name: test.name + "-" + string(function), Function: function, Field: test.name, IncludeProvenance: true,
				})
			}
			if test.typeName == v1alpha1.ValueTypeString || test.typeName == v1alpha1.ValueTypeInteger || test.typeName == v1alpha1.ValueTypeNumber || test.typeName == v1alpha1.ValueTypeTimestamp || test.typeName == v1alpha1.ValueTypeDuration || test.typeName == v1alpha1.ValueTypeQuantity {
				for _, function := range []v1alpha1.KubeseerAggregationFunction{v1alpha1.AggregationMin, v1alpha1.AggregationMax} {
					source.Aggregations = append(source.Aggregations, v1alpha1.KubeseerAggregation{Name: test.name + "-" + string(function), Function: function, Field: test.name})
				}
			}
		}
		firstData := make(map[string]any, len(cases))
		secondData := make(map[string]any, len(cases))
		for _, test := range cases {
			firstData[test.name] = test.first
			secondData[test.name] = test.second
		}
		resources := []selection.SelectedResource{
			aggregationSelectedResource("team-b", "second", "reducer-b", secondData),
			aggregationSelectedResource("team-a", "first", "reducer-a", firstData),
		}
		outcome := evaluateAggregationSource(t, source, resources, aggregation.Limits{})
		if outcome.Err() != nil || len(outcome.Aggregates()) != len(source.Aggregations) {
			t.Fatalf("reducer outcome = err=%v aggregates=%d", outcome.Err(), len(outcome.Aggregates()))
		}
		for _, aggregate := range outcome.Aggregates() {
			if aggregate.State() != v1alpha1.AggregateStateValues || len(aggregate.Groups()) != 1 {
				t.Fatalf("reducer %q state/groups = %s/%d", aggregate.Name(), aggregate.State(), len(aggregate.Groups()))
			}
			value := aggregate.Groups()[0].Value()
			if aggregate.Function() == v1alpha1.AggregationCount {
				if value.Type() != v1alpha1.ValueTypeInteger || len(value.Matches()) != 1 {
					t.Fatalf("count result %q = %#v", aggregate.Name(), value)
				}
				count, ok := value.Matches()[0].Value().IntegerValue()
				if !ok || count != int64(testCountForAggregateName(aggregate.Name())) {
					t.Fatalf("count result %q = %d (ok=%t)", aggregate.Name(), count, ok)
				}
			}
			if aggregate.Function() == v1alpha1.AggregationCollect && len(value.Matches()) != 2 {
				t.Fatalf("collect result %q cardinality = %d", aggregate.Name(), len(value.Matches()))
			}
			if aggregate.Function() == v1alpha1.AggregationDistinct && len(value.Matches()) == 0 {
				t.Fatalf("distinct result %q is empty", aggregate.Name())
			}
			if aggregate.Function() == v1alpha1.AggregationCollect || aggregate.Function() == v1alpha1.AggregationDistinct {
				if len(value.Matches()[0].Contributors()) == 0 || len(aggregate.Groups()[0].Contributors()) == 0 {
					t.Fatalf("value/group provenance missing for %q", aggregate.Name())
				}
			}
		}
	})

	t.Run("empty groups expose exact reducer states and provenance is opt-in", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID:     "empty-reducers",
			Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger}},
			Aggregations: []v1alpha1.KubeseerAggregation{
				{Name: "collect", Function: v1alpha1.AggregationCollect, Field: "value"},
				{Name: "count", Function: v1alpha1.AggregationCount, Field: "value"},
				{Name: "distinct", Function: v1alpha1.AggregationDistinct, Field: "value"},
				{Name: "first", Function: v1alpha1.AggregationFirst, Field: "value"},
				{Name: "last", Function: v1alpha1.AggregationLast, Field: "value"},
				{Name: "max", Function: v1alpha1.AggregationMax, Field: "value"},
				{Name: "min", Function: v1alpha1.AggregationMin, Field: "value"},
				{Name: "sum", Function: v1alpha1.AggregationSum, Field: "value"},
				{Name: "average", Function: v1alpha1.AggregationAverage, Field: "value"},
			},
		}
		outcome := evaluateAggregationSource(t, source, nil, aggregation.Limits{})
		if outcome.Err() != nil || len(outcome.Aggregates()) != len(source.Aggregations) {
			t.Fatalf("empty reducer outcome = err=%v aggregates=%d", outcome.Err(), len(outcome.Aggregates()))
		}
		for _, aggregate := range outcome.Aggregates() {
			value := aggregate.Groups()[0].Value()
			switch aggregate.Function() {
			case v1alpha1.AggregationCollect, v1alpha1.AggregationDistinct:
				if value.State() != v1alpha1.AggregateValueValues || len(value.Matches()) != 0 {
					t.Fatalf("empty collection %q = state=%s matches=%d", aggregate.Name(), value.State(), len(value.Matches()))
				}
			case v1alpha1.AggregationCount:
				count, ok := value.Matches()[0].Value().IntegerValue()
				if !ok || count != 0 {
					t.Fatalf("empty count = %d (ok=%t)", count, ok)
				}
			case v1alpha1.AggregationSum:
				sum, ok := value.Matches()[0].Value().IntegerValue()
				if !ok || sum != 0 {
					t.Fatalf("empty sum = %d (ok=%t)", sum, ok)
				}
			default:
				if value.State() != v1alpha1.AggregateValueAbsent || len(value.Matches()) != 0 {
					t.Fatalf("empty selected reducer %q = state=%s matches=%d", aggregate.Name(), value.State(), len(value.Matches()))
				}
			}
		}

		provenanceSource := source
		provenanceSource.Aggregations = []v1alpha1.KubeseerAggregation{{Name: "collect", Function: v1alpha1.AggregationCollect, Field: "value", IncludeProvenance: false}}
		resource := aggregationSelectedResource("team-a", "one", "one", map[string]any{"value": int64(1)})
		provenanceOutcome := evaluateAggregationSource(t, provenanceSource, []selection.SelectedResource{resource}, aggregation.Limits{})
		group := provenanceOutcome.Aggregates()[0].Groups()[0]
		if group.Contributors() != nil || group.Value().Matches()[0].Contributors() != nil {
			t.Fatalf("disabled provenance was projected: group=%#v value=%#v", group.Contributors(), group.Value().Matches()[0].Contributors())
		}
	})
}

func evaluateAggregationSource(t *testing.T, source v1alpha1.KubeseerSource, resources []selection.SelectedResource, limits aggregation.Limits) aggregation.SourceOutcome {
	t.Helper()
	operatorOutcome, _ := evaluateOperatorSource(t, source, resources)
	plan := aggregation.PlanSource(source, limits)
	if !plan.Valid() {
		t.Fatalf("aggregation plan %q failed: %#v", source.ID, plan.Failures())
	}
	return aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: plan, Operators: operatorOutcome}})[0]
}

func testCountForAggregateName(name string) int {
	// Every non-empty reducer fixture has exactly two accepted resources.
	if name == "" {
		return 0
	}
	return 2
}
