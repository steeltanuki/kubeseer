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
	"errors"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertCrossNamespaceAggregationIsolationScenarios(t *testing.T) {
	t.Helper()
	resources := []selection.SelectedResource{
		aggregationSelectedResource("team-a", "one", "isolation-one", map[string]any{"value": int64(1), "missing": nil}),
		aggregationSelectedResource("team-a", "two", "isolation-two", map[string]any{"value": int64(2), "missing": nil}),
	}

	t.Run("each cardinality ceiling fails closed and preserves independent aggregate work", func(t *testing.T) {
		cases := []struct {
			name     string
			limits   aggregation.Limits
			function v1alpha1.KubeseerAggregationFunction
			field    string
		}{
			{name: "groups", limits: isolationLimits(func(l *aggregation.Limits) { l.MaxGroups = 1 }), function: v1alpha1.AggregationSum, field: "value"},
			{name: "contributions", limits: isolationLimits(func(l *aggregation.Limits) { l.MaxContributions = 1 }), function: v1alpha1.AggregationCollect, field: "value"},
			{name: "collected-values", limits: isolationLimits(func(l *aggregation.Limits) { l.MaxCollectedValues = 1 }), function: v1alpha1.AggregationCollect, field: "value"},
			{name: "distinct-values", limits: isolationLimits(func(l *aggregation.Limits) { l.MaxDistinctValues = 1 }), function: v1alpha1.AggregationDistinct, field: "value"},
			{name: "provenance", limits: isolationLimits(func(l *aggregation.Limits) { l.MaxProvenanceEntries = 1 }), function: v1alpha1.AggregationSum, field: "value"},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				source := v1alpha1.KubeseerSource{
					ID: "limit-" + test.name,
					Fields: []v1alpha1.KubeseerField{
						{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger},
						{Name: "missing", Path: "{.data.missing}", Type: v1alpha1.ValueTypeInteger},
					},
					Aggregations: []v1alpha1.KubeseerAggregation{
						{Name: "limited", Function: test.function, Field: test.field, IncludeProvenance: test.name == "provenance", GroupBy: groupByForLimit(test.name)},
						{Name: "sibling", Function: v1alpha1.AggregationCount, Field: "missing"},
					},
				}
				outcome := evaluateAggregationSourceWithID(t, source, resources, test.limits)
				limited := findAggregate(outcome, "limited")
				sibling := findAggregate(outcome, "sibling")
				if limited.State() != v1alpha1.AggregateStateError || len(limited.Groups()) != 0 || !aggregation.HasReason(limited.Err(), aggregation.ReasonCardinalityExceeded) {
					t.Fatalf("limited aggregate = state=%s groups=%d err=%v", limited.State(), len(limited.Groups()), limited.Err())
				}
				if sibling.State() != v1alpha1.AggregateStateValues {
					t.Fatalf("sibling aggregate was not preserved = %#v", sibling)
				}
			})
		}
	})

	t.Run("upstream source and resource failures retain the narrowest boundary", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{ID: "upstream-source", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger}}, Aggregations: []v1alpha1.KubeseerAggregation{{Name: "sum", Function: v1alpha1.AggregationSum, Field: "value"}}}
		upstream := selection.NewSelectionError(source.ID, selection.ReasonReadUnavailable, "resource read unavailable")
		operatorFailure := operatorSourceFailure(t, source, upstream)
		plan := aggregation.PlanSource(source, aggregation.Limits{})
		outcome := aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: plan, Operators: operatorFailure}})[0]
		if outcome.OperatorOutcome().Err() != upstream || !errors.Is(outcome.OperatorOutcome().Err(), upstream) || len(outcome.Aggregates()) != 0 {
			t.Fatalf("upstream source failure was not preserved: operator=%v aggregates=%#v", outcome.OperatorOutcome().Err(), outcome.Aggregates())
		}

		resourceSource := source
		resourceSource.ID = "resource-failure-source"
		resourceSource.Fields[0].Operators = []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: integerOperand(1)}}
		resourceOutcome := evaluateAggregationSource(t, resourceSource, []selection.SelectedResource{
			aggregationSelectedResource("team-a", "bad", "resource-bad", map[string]any{"value": "not-an-integer"}),
			aggregationSelectedResource("team-a", "good", "resource-good", map[string]any{"value": int64(1)}),
		}, aggregation.Limits{})
		aggregate := resourceOutcome.Aggregates()[0]
		if aggregate.State() != v1alpha1.AggregateStateDegraded || len(aggregate.Groups()) != 1 || len(aggregate.Failures()) != 1 {
			t.Fatalf("resource-local failure = state=%s groups=%d failures=%d", aggregate.State(), len(aggregate.Groups()), len(aggregate.Failures()))
		}
		if strings.Contains(aggregate.Failures()[0].Error().Error(), "not-an-integer") {
			t.Fatal("resource failure leaked the invalid value")
		}
	})

	t.Run("source identity and cancellation failures are sanitized and partitioned", func(t *testing.T) {
		first := v1alpha1.KubeseerSource{ID: "cancel-first", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger}}, Aggregations: []v1alpha1.KubeseerAggregation{{Name: "sum", Function: v1alpha1.AggregationSum, Field: "value"}}}
		second := v1alpha1.KubeseerSource{ID: "cancel-second", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger}}, Aggregations: []v1alpha1.KubeseerAggregation{{Name: "sum", Function: v1alpha1.AggregationSum, Field: "value"}}}
		firstOperator, _ := evaluateOperatorSource(t, first, []selection.SelectedResource{aggregationSelectedResource("team-a", "first", "cancel-first-uid", map[string]any{"value": int64(1)})})
		secondOperator, _ := evaluateOperatorSource(t, second, []selection.SelectedResource{aggregationSelectedResource("team-a", "second", "cancel-second-uid", map[string]any{"value": int64(2)})})
		inputs := []aggregation.SourceInput{
			{Plan: aggregation.PlanSource(first, aggregation.Limits{}), Operators: firstOperator},
			{Plan: aggregation.PlanSource(second, aggregation.Limits{}), Operators: secondOperator},
		}
		preCanceled := aggregation.EvaluateBatch(contextCanceled(), inputs)
		if len(preCanceled) != 2 || !aggregation.HasReason(preCanceled[0].Err(), aggregation.ReasonAggregationInterrupted) || !aggregation.HasReason(preCanceled[1].Err(), aggregation.ReasonAggregationInterrupted) {
			t.Fatalf("pre-cancellation partition = %#v", preCanceled)
		}
		active := aggregation.EvaluateBatch(&cancelAfterContext{cancelAfter: 3}, inputs[:1])
		if len(active) != 1 || !aggregation.HasReason(active[0].Err(), aggregation.ReasonAggregationInterrupted) || len(active[0].Aggregates()) != 0 {
			t.Fatalf("active cancellation partition = %#v", active)
		}
		completedThenCanceled := aggregation.EvaluateBatch(&cancelAfterContext{cancelAfter: 1}, []aggregation.SourceInput{
			{Plan: aggregation.PlanSource(v1alpha1.KubeseerSource{ID: "empty-before-cancel"}, aggregation.Limits{}), Operators: mustEmptyOperatorOutcome(t, "empty-before-cancel")},
			inputs[1],
		})
		if len(completedThenCanceled) != 2 || completedThenCanceled[0].Err() != nil || !aggregation.HasReason(completedThenCanceled[1].Err(), aggregation.ReasonAggregationInterrupted) {
			t.Fatalf("later-source cancellation partition = %#v", completedThenCanceled)
		}

		mismatchedPlan := aggregation.PlanSource(first, aggregation.Limits{})
		mismatchOperator, _ := evaluateOperatorSource(t, second, []selection.SelectedResource{aggregationSelectedResource("team-a", "second", "mismatch", map[string]any{"value": int64(2)})})
		mismatch := aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: mismatchedPlan, Operators: mismatchOperator}})[0]
		if !aggregation.HasReason(mismatch.Err(), aggregation.ReasonInvalidInput) {
			t.Fatalf("source mismatch reason = %v", mismatch.Err())
		}
	})
}

func isolationLimits(change func(*aggregation.Limits)) aggregation.Limits {
	limits := aggregation.Limits{MaxGroups: 100, MaxContributions: 100, MaxCollectedValues: 100, MaxDistinctValues: 100, MaxProvenanceEntries: 100}
	change(&limits)
	return limits
}

func groupByForLimit(name string) []string {
	if name == "groups" {
		return []string{"value"}
	}
	return nil
}

func findAggregate(outcome aggregation.SourceOutcome, name string) aggregation.AggregateOutcome {
	for _, aggregate := range outcome.Aggregates() {
		if aggregate.Name() == name {
			return aggregate
		}
	}
	return aggregation.AggregateOutcome{}
}

func evaluateAggregationSourceWithID(t *testing.T, source v1alpha1.KubeseerSource, resources []selection.SelectedResource, limits aggregation.Limits) aggregation.SourceOutcome {
	t.Helper()
	operatorOutcome, _ := evaluateOperatorSource(t, source, resources)
	plan := aggregation.PlanSource(source, limits)
	if !plan.Valid() {
		t.Fatalf("aggregation plan %q failed: %#v", source.ID, plan.Failures())
	}
	return aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: plan, Operators: operatorOutcome}})[0]
}

func operatorSourceFailure(t *testing.T, source v1alpha1.KubeseerSource, upstream error) operators.SourceOutcome {
	t.Helper()
	typed := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: extraction.SourceOutcome{SourceID: source.ID, Err: upstream}}})[0]
	plan := operators.CompileSource(source)
	return operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: plan, Typed: typed}})[0]
}

func mustEmptyOperatorOutcome(t *testing.T, sourceID string) operators.SourceOutcome {
	t.Helper()
	source := v1alpha1.KubeseerSource{ID: sourceID}
	return operatorSourceFailureOrEmpty(t, source)
}

func operatorSourceFailureOrEmpty(t *testing.T, source v1alpha1.KubeseerSource) operators.SourceOutcome {
	t.Helper()
	typed := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: extraction.SourceOutcome{SourceID: source.ID}}})[0]
	plan := operators.CompileSource(source)
	return operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: plan, Typed: typed}})[0]
}
