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
	"math"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/selection"
)

func assertCrossNamespaceAggregationArithmeticScenarios(t *testing.T) {
	t.Helper()
	t.Run("sums stay exact for decimal, duration, quantity, and integer values", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID: "exact-sums",
			Fields: []v1alpha1.KubeseerField{
				{Name: "integer", Path: "{.data.integer}", Type: v1alpha1.ValueTypeInteger},
				{Name: "number", Path: "{.data.number}", Type: v1alpha1.ValueTypeNumber},
				{Name: "duration", Path: "{.data.duration}", Type: v1alpha1.ValueTypeDuration},
				{Name: "quantity", Path: "{.data.quantity}", Type: v1alpha1.ValueTypeQuantity},
			},
			Aggregations: []v1alpha1.KubeseerAggregation{
				{Name: "sum-integer", Function: v1alpha1.AggregationSum, Field: "integer"},
				{Name: "sum-number", Function: v1alpha1.AggregationSum, Field: "number"},
				{Name: "sum-duration", Function: v1alpha1.AggregationSum, Field: "duration"},
				{Name: "sum-quantity", Function: v1alpha1.AggregationSum, Field: "quantity"},
			},
		}
		resources := []selection.SelectedResource{
			aggregationSelectedResource("team-a", "one", "exact-one", map[string]any{"integer": int64(1), "number": "0.1", "duration": "1.5s", "quantity": "1Gi"}),
			aggregationSelectedResource("team-a", "two", "exact-two", map[string]any{"integer": int64(2), "number": "0.2", "duration": "500ms", "quantity": "512Mi"}),
		}
		outcome := evaluateAggregationSource(t, source, resources, aggregation.Limits{})
		for _, aggregate := range outcome.Aggregates() {
			if aggregate.State() != v1alpha1.AggregateStateValues || len(aggregate.Groups()) != 1 || len(aggregate.Groups()[0].Value().Matches()) != 1 {
				t.Fatalf("exact sum %q = state=%s groups=%d", aggregate.Name(), aggregate.State(), len(aggregate.Groups()))
			}
			match := aggregate.Groups()[0].Value().Matches()[0].Value()
			switch aggregate.Name() {
			case "sum-integer":
				got, ok := match.IntegerValue()
				if !ok || got != 3 {
					t.Fatalf("integer sum = %d (ok=%t)", got, ok)
				}
			case "sum-number":
				got, ok := match.NumberValue()
				if !ok || got != "0.3" {
					t.Fatalf("number sum = %q (ok=%t)", got, ok)
				}
			case "sum-duration":
				got, ok := match.DurationValue()
				if !ok || got != time.Second*2 {
					t.Fatalf("duration sum = %#v (ok=%t)", got, ok)
				}
			case "sum-quantity":
				_, baseUnits, ok := match.QuantityValue()
				if !ok || baseUnits != "1610612736" {
					t.Fatalf("quantity sum base units = %q (ok=%t)", baseUnits, ok)
				}
			}
		}
	})

	t.Run("averages round exact rational results once in every mode", func(t *testing.T) {
		precisionZero := int32(0)
		precision18 := int32(18)
		source := v1alpha1.KubeseerSource{
			ID: "average-rounding",
			Fields: []v1alpha1.KubeseerField{
				{Name: "positive", Path: "{.data.positive[*]}", Type: v1alpha1.ValueTypeNumber},
				{Name: "negative", Path: "{.data.negative[*]}", Type: v1alpha1.ValueTypeNumber},
				{Name: "third", Path: "{.data.third[*]}", Type: v1alpha1.ValueTypeNumber},
				{Name: "zero", Path: "{.data.zero[*]}", Type: v1alpha1.ValueTypeNumber},
			},
			Aggregations: []v1alpha1.KubeseerAggregation{
				{Name: "positive-half-even", Function: v1alpha1.AggregationAverage, Field: "positive", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingHalfEven},
				{Name: "positive-half-away", Function: v1alpha1.AggregationAverage, Field: "positive", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingHalfAwayFromZero},
				{Name: "positive-toward-zero", Function: v1alpha1.AggregationAverage, Field: "positive", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingTowardZero},
				{Name: "positive-away-zero", Function: v1alpha1.AggregationAverage, Field: "positive", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingAwayFromZero},
				{Name: "negative-half-even", Function: v1alpha1.AggregationAverage, Field: "negative", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingHalfEven},
				{Name: "negative-half-away", Function: v1alpha1.AggregationAverage, Field: "negative", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingHalfAwayFromZero},
				{Name: "negative-toward-zero", Function: v1alpha1.AggregationAverage, Field: "negative", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingTowardZero},
				{Name: "negative-away-zero", Function: v1alpha1.AggregationAverage, Field: "negative", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingAwayFromZero},
				{Name: "third-default", Function: v1alpha1.AggregationAverage, Field: "third"},
				{Name: "third-precision18", Function: v1alpha1.AggregationAverage, Field: "third", Precision: &precision18, RoundingMode: v1alpha1.RoundingTowardZero},
				{Name: "negative-zero", Function: v1alpha1.AggregationAverage, Field: "zero", Precision: &precisionZero, RoundingMode: v1alpha1.RoundingTowardZero},
			},
		}
		resource := aggregationSelectedResource("team-a", "averages", "average-one", map[string]any{
			"positive": []any{"1", "2"}, "negative": []any{"-1", "-2"}, "third": []any{"1", "0", "0"}, "zero": []any{"-1", "0"},
		})
		outcome := evaluateAggregationSource(t, source, []selection.SelectedResource{resource}, aggregation.Limits{})
		want := map[string]string{
			"positive-half-even": "2", "positive-half-away": "2", "positive-toward-zero": "1", "positive-away-zero": "2",
			"negative-half-even": "-2", "negative-half-away": "-2", "negative-toward-zero": "-1", "negative-away-zero": "-2",
			"third-default": "0.333333", "third-precision18": "0.333333333333333333", "negative-zero": "0",
		}
		for _, aggregate := range outcome.Aggregates() {
			if aggregate.State() != v1alpha1.AggregateStateValues || len(aggregate.Groups()) != 1 {
				t.Fatalf("average %q = state=%s groups=%d err=%v", aggregate.Name(), aggregate.State(), len(aggregate.Groups()), aggregate.Err())
			}
			got, ok := aggregate.Groups()[0].Value().Matches()[0].Value().NumberValue()
			if !ok || got != want[aggregate.Name()] {
				t.Fatalf("average %q = %q (ok=%t), want %q", aggregate.Name(), got, ok, want[aggregate.Name()])
			}
		}
	})

	t.Run("integer duration and quantity overflow fail only their aggregate", func(t *testing.T) {
		maxDuration := time.Duration(math.MaxInt64).String()
		source := v1alpha1.KubeseerSource{
			ID: "overflow-isolation",
			Fields: []v1alpha1.KubeseerField{
				{Name: "integer", Path: "{.data.integer}", Type: v1alpha1.ValueTypeInteger},
				{Name: "duration", Path: "{.data.duration}", Type: v1alpha1.ValueTypeDuration},
				{Name: "quantity", Path: "{.data.quantity}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "safe", Path: "{.data.safe}", Type: v1alpha1.ValueTypeInteger},
			},
			Aggregations: []v1alpha1.KubeseerAggregation{
				{Name: "integer-overflow", Function: v1alpha1.AggregationSum, Field: "integer"},
				{Name: "duration-overflow", Function: v1alpha1.AggregationSum, Field: "duration"},
				{Name: "quantity-overflow", Function: v1alpha1.AggregationSum, Field: "quantity"},
				{Name: "safe-sum", Function: v1alpha1.AggregationSum, Field: "safe"},
			},
		}
		resources := []selection.SelectedResource{
			aggregationSelectedResource("team-a", "maximum", "overflow-one", map[string]any{"integer": int64(math.MaxInt64), "duration": maxDuration, "quantity": "9223372036854775807", "safe": int64(1)}),
			aggregationSelectedResource("team-a", "increment", "overflow-two", map[string]any{"integer": int64(1), "duration": "1ns", "quantity": "1", "safe": int64(2)}),
		}
		outcome := evaluateAggregationSource(t, source, resources, aggregation.Limits{})
		for _, aggregate := range outcome.Aggregates() {
			if aggregate.Name() == "safe-sum" {
				if aggregate.State() != v1alpha1.AggregateStateValues || len(aggregate.Groups()) != 1 {
					t.Fatalf("safe sibling = %#v", aggregate)
				}
				continue
			}
			if aggregate.State() != v1alpha1.AggregateStateError || len(aggregate.Groups()) != 0 || !aggregation.HasReason(aggregate.Err(), aggregation.ReasonOverflow) {
				t.Fatalf("overflow aggregate %q = state=%s groups=%d err=%v", aggregate.Name(), aggregate.State(), len(aggregate.Groups()), aggregate.Err())
			}
		}
	})
}
