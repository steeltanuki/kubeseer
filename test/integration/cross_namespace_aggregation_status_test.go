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
	"reflect"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
)

func assertCrossNamespaceAggregationStatusScenarios(t *testing.T) {
	t.Helper()

	t.Run("aggregate projection preserves raw results and declaration order", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID: "aggregate-source",
			Fields: []v1alpha1.KubeseerField{
				{Name: "group", Path: "{.data.group}", Type: v1alpha1.ValueTypeString},
				{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger},
			},
			Aggregations: []v1alpha1.KubeseerAggregation{
				{Name: "sum-by-group", Function: v1alpha1.AggregationSum, Field: "value", GroupBy: []string{"group"}, IncludeProvenance: true},
				{Name: "average", Function: v1alpha1.AggregationAverage, Field: "value"},
				{Name: "invalid", Function: v1alpha1.AggregationSum, Field: "missing"},
			},
		}
		resources := []selection.SelectedResource{
			aggregationSelectedResource("team-b", "second", "status-second", map[string]any{"group": "b", "value": int64(2)}),
			aggregationSelectedResource("team-a", "first", "status-first", map[string]any{"group": "a", "value": int64(4)}),
			aggregationSelectedResource("team-a", "bad", "status-bad", map[string]any{"group": "a", "value": "not-an-integer"}),
		}
		op, _ := evaluateOperatorSource(t, source, resources)
		plan := aggregation.PlanSource(source, aggregation.Limits{})
		if plan.Valid() {
			t.Fatal("invalid aggregate declaration unexpectedly produced a valid complete plan")
		}
		outcome := aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: plan, Operators: op}})[0]
		result := aggregation.BuildResult([]aggregation.SourceOutcome{outcome})
		if len(result.Sources) != 1 || result.Sources[0].ID != source.ID {
			t.Fatalf("projected source identity = %#v", result.Sources)
		}
		projectedSource := result.Sources[0]
		if len(projectedSource.Resources) != len(op.Resources()) {
			t.Fatalf("raw resource count = %d, want %d", len(projectedSource.Resources), len(op.Resources()))
		}
		if len(projectedSource.Aggregates) != 3 {
			t.Fatalf("aggregate count = %d, want three independent declarations", len(projectedSource.Aggregates))
		}
		wantOrder := []string{"average", "invalid", "sum-by-group"}
		for index, want := range wantOrder {
			if projectedSource.Aggregates[index].Name != want {
				t.Fatalf("aggregate %d name = %q, want %q", index, projectedSource.Aggregates[index].Name, want)
			}
		}
		average := projectedSource.Aggregates[0]
		if average.State != v1alpha1.AggregateStateDegraded || len(average.Groups) != 1 || len(average.Groups[0].Value.Matches) != 1 || average.Groups[0].Value.Matches[0].Value.NumberValue == nil || *average.Groups[0].Value.Matches[0].Value.NumberValue != "3" || len(average.Failures) != 1 {
			t.Fatalf("projected average = %#v", average)
		}
		invalid := projectedSource.Aggregates[1]
		if invalid.State != v1alpha1.AggregateStateError || invalid.Error == nil || invalid.Error.Reason != string(aggregation.ReasonUnknownField) {
			t.Fatalf("projected invalid aggregate = %#v", invalid)
		}
		sum := projectedSource.Aggregates[2]
		if sum.State != v1alpha1.AggregateStateDegraded || len(sum.Groups) != 2 || len(sum.Failures) != 1 || len(sum.Groups[0].Contributors) != 1 {
			t.Fatalf("projected degraded aggregate = %#v", sum)
		}
		if strings.Contains(sum.Failures[0].Error.Message, "not-an-integer") {
			t.Fatal("aggregate failure leaked the invalid input value")
		}
		if sum.Groups[0].Keys[0].Value.StringValue == nil || *sum.Groups[0].Keys[0].Value.StringValue != "a" {
			t.Fatalf("canonical projected group order = %#v", sum.Groups)
		}

		plainSource := v1alpha1.KubeseerSource{ID: "plain-source", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger}}}
		emptySource := plainSource
		emptySource.ID = "explicit-empty-source"
		emptySource.Aggregations = []v1alpha1.KubeseerAggregation{}
		plainOperator, _ := evaluateOperatorSource(t, plainSource, resources[:1])
		emptyOperator, _ := evaluateOperatorSource(t, emptySource, resources[:1])
		plainOutcome := aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: aggregation.PlanSource(plainSource, aggregation.Limits{}), Operators: plainOperator}})[0]
		emptyOutcome := aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: aggregation.PlanSource(emptySource, aggregation.Limits{}), Operators: emptyOperator}})[0]
		plainResult := aggregation.BuildResult([]aggregation.SourceOutcome{plainOutcome, emptyOutcome})
		if plainResult.Sources[0].Aggregates != nil || plainResult.Sources[1].Aggregates != nil {
			t.Fatalf("omitted/explicit-empty aggregate fields were not omitted: %#v", plainResult.Sources)
		}
		if len(plainResult.Sources[0].Resources) != 1 || len(plainResult.Sources[1].Resources) != 1 {
			t.Fatalf("raw result was not retained for no-aggregate sources: %#v", plainResult.Sources)
		}
	})

	t.Run("aggregate semantics participate in status derivation without changing raw counts", func(t *testing.T) {
		value := int64(1)
		base := &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{{
			ID:    "source",
			State: v1alpha1.SourceStateValues,
			Resources: []v1alpha1.KubeseerResourceResult{{
				APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "pod", UID: "pod-uid",
				Fields: []v1alpha1.KubeseerFieldResult{{Name: "value", Type: v1alpha1.ValueTypeInteger, State: v1alpha1.FieldStateValues, Matches: []v1alpha1.KubeseerTypedMatch{{State: v1alpha1.MatchStateValue, IntegerValue: &value}}}},
			}},
		}}}
		withoutAggregate, err := statuscontract.DeriveResult(base)
		if err != nil {
			t.Fatalf("derive raw result: %v", err)
		}
		withAggregate := base.DeepCopy()
		withAggregate.Sources[0].Aggregates = []v1alpha1.KubeseerAggregateResult{{
			Name: "sum", Function: v1alpha1.AggregationSum, Field: "value", State: v1alpha1.AggregateStateValues,
			Groups: []v1alpha1.KubeseerAggregateGroup{{Value: v1alpha1.KubeseerAggregateValue{Type: v1alpha1.ValueTypeInteger, State: v1alpha1.AggregateValueValues, Matches: []v1alpha1.KubeseerAggregateMatch{{Value: v1alpha1.KubeseerTypedMatch{State: v1alpha1.MatchStateValue, IntegerValue: &value}}}}}},
		}}
		withAggregateDerived, err := statuscontract.DeriveResult(withAggregate)
		if err != nil {
			t.Fatalf("derive aggregate result: %v", err)
		}
		if withAggregateDerived.ResultHash == withoutAggregate.ResultHash || withAggregateDerived.Summary.MatchedResources != 1 || withAggregateDerived.Degraded {
			t.Fatalf("successful aggregate derivation = %#v, raw=%#v", withAggregateDerived, withoutAggregate)
		}
		degraded := withAggregate.DeepCopy()
		degraded.Sources[0].Aggregates[0].State = v1alpha1.AggregateStateDegraded
		degradedDerived, err := statuscontract.DeriveResult(degraded)
		if err != nil {
			t.Fatalf("derive degraded aggregate result: %v", err)
		}
		if !degradedDerived.Degraded || !statuscontract.HasResultErrors(degradedDerived.Result) || degradedDerived.Summary.MatchedResources != 1 {
			t.Fatalf("degraded aggregate was not status-visible without inflating summary: %#v", degradedDerived)
		}

		nilCollections := withAggregate.DeepCopy()
		nilCollections.Sources[0].Aggregates[0].Groups = nil
		emptyCollections := withAggregate.DeepCopy()
		emptyCollections.Sources[0].Aggregates[0].Groups = []v1alpha1.KubeseerAggregateGroup{}
		if !statuscontract.SemanticResultEqual(nilCollections, emptyCollections) {
			t.Fatal("nil and empty aggregate collections were not normalized equivalently")
		}
		equivalent, err := statuscontract.DeriveResult(emptyCollections)
		if err != nil {
			t.Fatalf("derive equivalent empty aggregate result: %v", err)
		}
		nilDerived, err := statuscontract.DeriveResult(nilCollections)
		if err != nil {
			t.Fatalf("derive nil aggregate result: %v", err)
		}
		if equivalent.ResultHash != nilDerived.ResultHash || !reflect.DeepEqual(equivalent.Summary, nilDerived.Summary) {
			t.Fatalf("equivalent aggregate collections changed status semantics: equivalent=%#v nil=%#v", equivalent, nilDerived)
		}
	})
}
