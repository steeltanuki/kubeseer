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
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"k8s.io/apimachinery/pkg/types"
)

func assertCrossNamespaceAggregationGroupingScenarios(t *testing.T) {
	t.Helper()
	source := v1alpha1.KubeseerSource{
		ID: "grouping-source",
		Fields: []v1alpha1.KubeseerField{
			{Name: "keep", Path: "{.data.keep}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: stringOperand("yes")}}},
			{Name: "team", Path: "{.data.team}", Type: v1alpha1.ValueTypeString},
			{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger},
			{Name: "values", Path: "{.data.values[*]}", Type: v1alpha1.ValueTypeInteger},
		},
		Aggregations: []v1alpha1.KubeseerAggregation{
			{Name: "sum-by-team", Function: v1alpha1.AggregationSum, Field: "value", GroupBy: []string{"team"}, IncludeProvenance: true},
			{Name: "collect-values", Function: v1alpha1.AggregationCollect, Field: "values", IncludeProvenance: true},
			{Name: "count-values", Function: v1alpha1.AggregationCount, Field: "values"},
			{Name: "distinct-values", Function: v1alpha1.AggregationDistinct, Field: "values", IncludeProvenance: true},
		},
	}
	resources := []selection.SelectedResource{
		aggregationSelectedResource("team-a", "same-name", "uid-a", map[string]any{"keep": "yes", "team": "team-a", "value": int64(4), "values": []any{int64(2), int64(1)}}),
		aggregationSelectedResource("team-b", "same-name", "uid-b", map[string]any{"keep": "yes", "team": "team-b", "value": int64(9), "values": []any{int64(1), int64(3)}}),
		aggregationSelectedResource("team-a", "other", "uid-c", map[string]any{"keep": "yes", "team": "team-a", "value": int64(3), "values": []any{int64(2)}}),
		// The later object has the same UID and must not amplify the first
		// occurrence, even though its target value differs.
		aggregationSelectedResource("team-a", "duplicate", "uid-a", map[string]any{"keep": "yes", "team": "team-a", "value": int64(100), "values": []any{int64(100)}}),
		aggregationSelectedResource("team-a", "rejected", "uid-rejected", map[string]any{"keep": "no", "team": "team-a", "value": int64(1000), "values": []any{int64(1000)}}),
	}
	op, _ := evaluateOperatorSource(t, source, resources)
	if op.Err() != nil || len(op.Resources()) != len(resources) {
		t.Fatalf("operator collaboration outcome = err=%v resources=%d", op.Err(), len(op.Resources()))
	}
	plan := aggregation.PlanSource(source, aggregation.Limits{})
	if !plan.Valid() {
		t.Fatalf("grouping plan failed: %#v", plan.Failures())
	}
	first := aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: plan, Operators: op}})[0]
	second := aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: plan, Operators: op}})[0]
	if first.Err() != nil || !reflect.DeepEqual(first.Aggregates(), second.Aggregates()) {
		t.Fatalf("grouping evaluation is not deterministic: first=%#v second=%#v err=%v", first.Aggregates(), second.Aggregates(), first.Err())
	}
	aggregates := first.Aggregates()
	if len(aggregates) != 4 || aggregates[3].Name() != "sum-by-team" {
		t.Fatalf("aggregate order = %#v", aggregates)
	}
	sum := aggregates[3]
	if sum.State() != v1alpha1.AggregateStateValues || len(sum.Groups()) != 2 || len(sum.Failures()) != 0 {
		t.Fatalf("grouped sum state/groups/failures = %s/%d/%d", sum.State(), len(sum.Groups()), len(sum.Failures()))
	}
	groups := sum.Groups()
	firstTeam, firstTeamOK := groups[0].Keys()[0].Value().StringValue()
	secondTeam, secondTeamOK := groups[1].Keys()[0].Value().StringValue()
	if !firstTeamOK || !secondTeamOK || firstTeam != "team-a" || secondTeam != "team-b" {
		t.Fatalf("canonical group ordering = %#v", groups)
	}
	teamASum, teamASumOK := groups[0].Value().Matches()[0].Value().IntegerValue()
	if !teamASumOK || teamASum != 7 {
		t.Fatalf("team-a sum = %d, want 7", teamASum)
	}
	teamBSum, teamBSumOK := groups[1].Value().Matches()[0].Value().IntegerValue()
	if !teamBSumOK || teamBSum != 9 {
		t.Fatalf("team-b sum = %d, want 9", teamBSum)
	}
	if len(groups[0].Contributors()) != 2 || len(groups[1].Contributors()) != 1 || groups[0].Contributors()[0].Namespace != "team-a" {
		t.Fatalf("complete group provenance = %#v / %#v", groups[0].Contributors(), groups[1].Contributors())
	}

	collect := aggregates[0]
	if len(collect.Groups()) != 1 || len(collect.Groups()[0].Value().Matches()) != 5 {
		t.Fatalf("collect values = %#v", collect.Groups())
	}
	firstCollected, firstCollectedOK := collect.Groups()[0].Value().Matches()[0].Value().IntegerValue()
	secondCollected, secondCollectedOK := collect.Groups()[0].Value().Matches()[1].Value().IntegerValue()
	if !firstCollectedOK || !secondCollectedOK || firstCollected != 2 || secondCollected != 2 {
		t.Fatalf("collect preserves match order = %#v", collect.Groups()[0].Value().Matches())
	}
	if len(collect.Groups()[0].Value().Matches()[0].Contributors()) != 1 {
		t.Fatalf("collect value provenance = %#v", collect.Groups()[0].Value().Matches()[0].Contributors())
	}

	distinct := aggregates[2]
	if len(distinct.Groups()) != 1 || len(distinct.Groups()[0].Value().Matches()) != 3 {
		t.Fatalf("distinct values = %#v", distinct.Groups())
	}
	if len(distinct.Groups()[0].Value().Matches()[0].Contributors()) != 2 {
		t.Fatalf("distinct all-contributor provenance = %#v", distinct.Groups()[0].Value().Matches()[0].Contributors())
	}
	countValue, countValueOK := aggregates[1].Groups()[0].Value().Matches()[0].Value().IntegerValue()
	if len(aggregates[1].Groups()[0].Value().Matches()) != 1 || !countValueOK || countValue != 5 {
		t.Fatalf("count excludes rejected and duplicate resources = %#v", aggregates[1].Groups()[0].Value())
	}

	t.Run("key cardinality and target failures remain aggregate-local", func(t *testing.T) {
		failureSource := v1alpha1.KubeseerSource{
			ID: "group-failure-source",
			Fields: []v1alpha1.KubeseerField{
				{Name: "missingKey", Path: "{.data.missingKey}", Type: v1alpha1.ValueTypeString},
				{Name: "nullKey", Path: "{.data.nullKey}", Type: v1alpha1.ValueTypeString},
				{Name: "manyKeys", Path: "{.data.manyKeys[*]}", Type: v1alpha1.ValueTypeString},
				{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger},
			},
			Aggregations: []v1alpha1.KubeseerAggregation{
				{Name: "missing", Function: v1alpha1.AggregationSum, Field: "value", GroupBy: []string{"missingKey"}},
				{Name: "null", Function: v1alpha1.AggregationSum, Field: "value", GroupBy: []string{"nullKey"}},
				{Name: "many", Function: v1alpha1.AggregationSum, Field: "value", GroupBy: []string{"manyKeys"}},
			},
		}
		failureResources := []selection.SelectedResource{
			aggregationSelectedResource("team-a", "missing", "failure-a", map[string]any{"value": int64(1)}),
			aggregationSelectedResource("team-a", "null", "failure-b", map[string]any{"nullKey": nil, "value": int64(2)}),
			aggregationSelectedResource("team-a", "many", "failure-c", map[string]any{"manyKeys": []any{"a", "b"}, "value": int64(3)}),
		}
		failureOperator, _ := evaluateOperatorSource(t, failureSource, failureResources)
		failurePlan := aggregation.PlanSource(failureSource, aggregation.Limits{})
		failureOutcome := aggregation.EvaluateBatch(context.Background(), []aggregation.SourceInput{{Plan: failurePlan, Operators: failureOperator}})[0]
		for _, aggregate := range failureOutcome.Aggregates() {
			if aggregate.State() != v1alpha1.AggregateStateDegraded || len(aggregate.Failures()) != 3 || len(aggregate.Groups()) != 0 {
				t.Fatalf("isolated grouping failure %q = state=%s failures=%d groups=%d", aggregate.Name(), aggregate.State(), len(aggregate.Failures()), len(aggregate.Groups()))
			}
			if !aggregation.HasReason(aggregate.Failures()[0].Error(), aggregation.ReasonInvalidGroupKey) {
				t.Fatalf("grouping reason = %v", aggregate.Failures()[0].Error())
			}
		}
	})
}

func aggregationSelectedResource(namespace, name, uid string, data map[string]any) selection.SelectedResource {
	resource := selectedExtractionResourceWithObject(map[string]any{"data": data})
	resource.Provenance.Namespace = namespace
	resource.Provenance.Name = name
	resource.Provenance.UID = types.UID(uid)
	return resource
}
