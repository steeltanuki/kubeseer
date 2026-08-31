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

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertCrossNamespaceAggregationPlanningScenarios(t *testing.T) {
	t.Helper()
	fields := []v1alpha1.KubeseerField{
		{Name: "team", Path: "{.data.team}", Type: v1alpha1.ValueTypeString},
		{Name: "replicas", Path: "{.data.replicas}", Type: v1alpha1.ValueTypeInteger},
		{Name: "ratio", Path: "{.data.ratio}", Type: v1alpha1.ValueTypeNumber},
		{Name: "enabled", Path: "{.data.enabled}", Type: v1alpha1.ValueTypeBoolean},
		{Name: "when", Path: "{.data.when}", Type: v1alpha1.ValueTypeTimestamp},
		{Name: "age", Path: "{.data.age}", Type: v1alpha1.ValueTypeDuration},
		{Name: "capacity", Path: "{.data.capacity}", Type: v1alpha1.ValueTypeQuantity},
		{Name: "payload", Path: "{.data.payload}", Type: v1alpha1.ValueTypeObject},
		{Name: "items", Path: "{.data.items}", Type: v1alpha1.ValueTypeList},
	}
	valid := v1alpha1.KubeseerSource{
		ID:     "aggregation-source",
		Fields: fields,
		Aggregations: []v1alpha1.KubeseerAggregation{
			{Name: "sum-replicas", Function: v1alpha1.AggregationSum, Field: "replicas", GroupBy: []string{"team"}},
			{Name: "average-ratio", Function: v1alpha1.AggregationAverage, Field: "ratio"},
			{Name: "count-payload", Function: v1alpha1.AggregationCount, Field: "payload"},
			{Name: "collect-items", Function: v1alpha1.AggregationCollect, Field: "items"},
			{Name: "min-time", Function: v1alpha1.AggregationMin, Field: "when"},
			{Name: "max-capacity", Function: v1alpha1.AggregationMax, Field: "capacity"},
			{Name: "first-enabled", Function: v1alpha1.AggregationFirst, Field: "enabled"},
			{Name: "last-age", Function: v1alpha1.AggregationLast, Field: "age"},
			{Name: "distinct-team", Function: v1alpha1.AggregationDistinct, Field: "team"},
		},
	}

	if _, extractionErr := extraction.CompileSource(valid); extractionErr != nil {
		t.Fatalf("real extraction plan rejected aggregation fixture: %v", extractionErr)
	}
	if typedPlan := typedoutput.CompileSource(valid); len(typedPlan.Failures()) != 0 {
		t.Fatalf("real typed-output plan rejected aggregation fixture: %#v", typedPlan.Failures())
	}
	if operatorPlan := operators.CompileSource(valid); !operatorPlan.Valid() {
		t.Fatalf("real operator plan rejected aggregation fixture: %#v", operatorPlan.Failures())
	}

	planned := aggregation.PlanSource(valid, aggregation.Limits{})
	if !planned.Valid() {
		t.Fatalf("valid aggregation plan = %#v", planned.Failures())
	}
	plans := planned.Plans()
	if len(plans) != len(valid.Aggregations) {
		t.Fatalf("valid plan count = %d, want %d", len(plans), len(valid.Aggregations))
	}
	for index := 1; index < len(plans); index++ {
		if plans[index-1].Name() >= plans[index].Name() {
			t.Fatalf("plans are not lexical: %q before %q", plans[index-1].Name(), plans[index].Name())
		}
	}
	average := plans[0]
	if average.Name() != "average-ratio" || average.Precision() != 6 || average.RoundingMode() != v1alpha1.RoundingHalfEven {
		t.Fatalf("average defaults = name=%q precision=%d rounding=%q", average.Name(), average.Precision(), average.RoundingMode())
	}
	for _, plan := range plans {
		if plan.Name() == "sum-replicas" {
			groupBy := plan.GroupBy()
			if len(groupBy) != 1 || groupBy[0].Name() != "team" || groupBy[0].Type() != v1alpha1.ValueTypeString {
				t.Fatalf("ordered group plan = %#v", groupBy)
			}
		}
	}

	explicit := valid
	precision := int32(3)
	explicit.Aggregations[1].Precision = &precision
	explicit.Aggregations[1].RoundingMode = v1alpha1.RoundingAwayFromZero
	reordered := explicit
	reordered.Aggregations = append([]v1alpha1.KubeseerAggregation(nil), explicit.Aggregations...)
	for left, right := 0, len(reordered.Aggregations)-1; left < right; left, right = left+1, right-1 {
		reordered.Aggregations[left], reordered.Aggregations[right] = reordered.Aggregations[right], reordered.Aggregations[left]
	}
	firstPlan := aggregation.PlanSource(explicit, aggregation.Limits{})
	secondPlan := aggregation.PlanSource(reordered, aggregation.Limits{})
	if !reflect.DeepEqual(firstPlan.Plans(), secondPlan.Plans()) {
		t.Fatalf("equivalent declarations produced different plans: %#v vs %#v", firstPlan.Plans(), secondPlan.Plans())
	}
	for _, plan := range firstPlan.Plans() {
		if plan.Name() == "average-ratio" && (plan.Precision() != 3 || plan.RoundingMode() != v1alpha1.RoundingAwayFromZero) {
			t.Fatalf("explicit average options were not retained: %#v", plan)
		}
	}
	mutable := firstPlan.Plans()
	for index := range mutable {
		groupBy := mutable[index].GroupBy()
		if len(groupBy) != 0 {
			groupBy[0] = aggregation.GroupFieldPlan{}
		}
	}
	if got := firstPlan.Plans(); got[0].Name() == "" {
		t.Fatal("mutating plan accessors changed the immutable plan")
	}

	t.Run("independent invalid declarations retain valid siblings", func(t *testing.T) {
		invalid := valid
		invalid.Aggregations = []v1alpha1.KubeseerAggregation{
			{Name: "good", Function: v1alpha1.AggregationCount, Field: "payload"},
			{Name: "bad-target", Function: v1alpha1.AggregationCount, Field: "missing"},
			{Name: "bad-group", Function: v1alpha1.AggregationCount, Field: "payload", GroupBy: []string{"payload"}},
			{Name: "bad-duplicate-group", Function: v1alpha1.AggregationCount, Field: "payload", GroupBy: []string{"team", "team"}},
			{Name: "bad-compatibility", Function: v1alpha1.AggregationSum, Field: "team"},
			{Name: "bad-options", Function: v1alpha1.AggregationCount, Field: "payload", Precision: ptrInt32(2)},
			{Name: "bad-function", Function: v1alpha1.KubeseerAggregationFunction("unknown"), Field: "payload"},
		}
		outcome := aggregation.PlanSource(invalid, aggregation.Limits{})
		if len(outcome.Plans()) != 1 || outcome.Plans()[0].Name() != "good" || len(outcome.Failures()) != 6 || outcome.Valid() {
			t.Fatalf("independent plan outcome = plans=%#v failures=%#v valid=%v", outcome.Plans(), outcome.Failures(), outcome.Valid())
		}
		for _, failure := range outcome.Failures() {
			if failure.SourceID != invalid.ID || failure.AggregateName == "" || failure.Function == "" || failure.Reason == "" {
				t.Fatalf("incomplete planning failure = %#v", failure)
			}
			if strings.Contains(failure.Error(), "SENTINEL_VALUE") || strings.Contains(failure.Error(), "{.secret") {
				t.Fatalf("planning diagnostic leaked data: %s", failure.Error())
			}
		}
		if outcome.Failures()[0].Reason != aggregation.ReasonIncompatibleFunction && outcome.Failures()[0].Reason != aggregation.ReasonInvalidAverageOptions {
			// Lexical ordering is part of the contract; this assertion still
			// permits the first invalid branch if the fixture is expanded.
			t.Fatalf("unexpected first stable planning reason: %s", outcome.Failures()[0].Reason)
		}
	})

	t.Run("canonical keys reuse typed normalization", func(t *testing.T) {
		cases := []struct {
			name     string
			typeName v1alpha1.KubeseerValueType
			left     any
			right    any
			equal    bool
		}{
			{name: "string", typeName: v1alpha1.ValueTypeString, left: "Team-A", right: "Team-A", equal: true},
			{name: "integer", typeName: v1alpha1.ValueTypeInteger, left: int64(7), right: int64(8), equal: false},
			{name: "number", typeName: v1alpha1.ValueTypeNumber, left: "1.0", right: "1", equal: true},
			{name: "boolean", typeName: v1alpha1.ValueTypeBoolean, left: true, right: true, equal: true},
			{name: "timestamp", typeName: v1alpha1.ValueTypeTimestamp, left: "2026-01-01T01:00:00+01:00", right: "2026-01-01T00:00:00Z", equal: true},
			{name: "duration", typeName: v1alpha1.ValueTypeDuration, left: "1s", right: "1000ms", equal: true},
			{name: "quantity", typeName: v1alpha1.ValueTypeQuantity, left: "1", right: "1000m", equal: true},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				left, err := typedoutput.ConvertConfiguredMatch("aggregation-source", test.name, test.typeName, test.left)
				if err != nil {
					t.Fatalf("left conversion: %v", err)
				}
				right, err := typedoutput.ConvertConfiguredMatch("aggregation-source", test.name, test.typeName, test.right)
				if err != nil {
					t.Fatalf("right conversion: %v", err)
				}
				leftKey, err := typedoutput.CanonicalKey(test.typeName, left)
				if err != nil {
					t.Fatalf("left key: %v", err)
				}
				rightKey, err := typedoutput.CanonicalKey(test.typeName, right)
				if err != nil {
					t.Fatalf("right key: %v", err)
				}
				if reflect.DeepEqual(leftKey, rightKey) != test.equal {
					t.Fatalf("canonical keys equal=%v, want %v", reflect.DeepEqual(leftKey, rightKey), test.equal)
				}
			})
		}
	})
}

func ptrInt32(value int32) *int32 { return &value }

var _ = aggregation.DefaultLimits
