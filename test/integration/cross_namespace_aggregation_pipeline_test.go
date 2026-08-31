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
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertCrossNamespaceAggregationPipelineScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	key := types.NamespacedName{Namespace: "team-a", Name: "aggregation-pipeline-owner"}
	aggregationSource := v1alpha1.KubeseerSource{
		ID:         "aggregation-pipeline-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a", "team-b"}},
		Fields: []v1alpha1.KubeseerField{
			{Name: "group", Path: "{.data.group}", Type: v1alpha1.ValueTypeString},
			{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger},
		},
		Aggregations: []v1alpha1.KubeseerAggregation{
			{Name: "sum-by-group", Function: v1alpha1.AggregationSum, Field: "value", GroupBy: []string{"group"}, IncludeProvenance: true},
			{Name: "average-default", Function: v1alpha1.AggregationAverage, Field: "value", IncludeProvenance: true},
			{Name: "invalid-sibling", Function: v1alpha1.AggregationSum, Field: "not-declared"},
		},
	}
	plainSource := v1alpha1.KubeseerSource{
		ID:         "plain-pipeline-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
		Fields:     []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger}},
	}
	object := runtimePipelineKubeseer(key, "aggregation-pipeline-uid", 1, aggregationSource, plainSource)
	policy := mutatePolicy(basePolicy(), func(next *v1alpha1.KubeseerAccessPolicy) {
		next.Spec.Namespaces.Include = []string{"team-a", "team-b"}
		next.Spec.Namespaces.Exclude = nil
	})
	reader := newRuntimePipelineReader(object)
	lister := newRuntimePipelineLister()
	teamA := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		*runtimePipelineAggregationResource("team-a", "aggregate-a", "aggregate-a-uid", "blue", "1"),
	}}
	teamB := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		*runtimePipelineAggregationResource("team-b", "aggregate-b", "aggregate-b-uid", "red", "3"),
		*runtimePipelineAggregationResource("team-b", "aggregate-c", "aggregate-c-uid", "blue", "2"),
	}}
	lister.SetHook(func(_ context.Context, target selection.ReadTarget, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		switch target.SourceID {
		case aggregationSource.ID:
			if target.Namespace == "team-b" {
				return teamB.DeepCopy(), nil
			}
			return teamA.DeepCopy(), nil
		case plainSource.ID:
			return teamA.DeepCopy(), nil
		default:
			return &unstructured.UnstructuredList{}, nil
		}
	})
	publisher := &runtimePipelinePublisher{}
	runtime := mustRuntimePipeline(t, resolver, reader, lister, policy, &runtimePipelineRoutes{}, publisher, nil)
	if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatalf("cross-namespace aggregation runtime reconciliation: %v", err)
	}
	publications := publisher.Publications()
	if len(publications) != 1 || len(publications[0].Result.Sources) != 2 {
		t.Fatalf("pipeline publication = %#v", publications)
	}
	result := publications[0].Result
	if result.Sources[0].ID != aggregationSource.ID || result.Sources[1].ID != plainSource.ID {
		t.Fatalf("pipeline source order = %#v", result.Sources)
	}
	if len(result.Sources[0].Resources) != 3 || len(result.Sources[1].Resources) != 1 {
		t.Fatalf("pipeline raw resource preservation = aggregate=%d plain=%d", len(result.Sources[0].Resources), len(result.Sources[1].Resources))
	}
	if len(result.Sources[1].Aggregates) != 0 {
		t.Fatalf("plain sibling unexpectedly received aggregate output: %#v", result.Sources[1].Aggregates)
	}
	aggregates := result.Sources[0].Aggregates
	if len(aggregates) != 3 || aggregates[0].Name != "average-default" || aggregates[1].Name != "invalid-sibling" || aggregates[2].Name != "sum-by-group" {
		t.Fatalf("pipeline aggregate order = %#v", aggregates)
	}
	average := aggregates[0]
	if average.State != v1alpha1.AggregateStateValues || len(average.Groups) != 1 || len(average.Groups[0].Value.Matches) != 1 || average.Groups[0].Value.Matches[0].Value.NumberValue == nil || *average.Groups[0].Value.Matches[0].Value.NumberValue != "2" || len(average.Groups[0].Contributors) != 3 {
		t.Fatalf("pipeline average default/provenance = %#v", average)
	}
	invalid := aggregates[1]
	if invalid.State != v1alpha1.AggregateStateError || invalid.Error == nil || invalid.Error.Reason != "unknown-field" {
		t.Fatalf("pipeline invalid aggregate status = %#v", invalid)
	}
	sum := aggregates[2]
	if sum.State != v1alpha1.AggregateStateValues || len(sum.Groups) != 2 || sum.Groups[0].Keys[0].Value.StringValue == nil || *sum.Groups[0].Keys[0].Value.StringValue != "blue" {
		t.Fatalf("pipeline grouped aggregate = %#v", sum)
	}
	if strings.Contains(invalid.Error.Message, "not-declared") {
		t.Fatal("invalid aggregate diagnostic leaked the target field name")
	}
}

func runtimePipelineAggregationResource(namespace, name string, uid types.UID, group, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"uid":       string(uid),
		},
		"data": map[string]interface{}{"group": group, "value": value},
	}}
}
