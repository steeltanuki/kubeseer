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
	"reflect"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func assertResourceSelectionPlanningScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	planner := selection.NewPlanner(resolver)

	t.Run("source identity and canonical selectors survive planning", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID:       "pod-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Selector: &v1alpha1.ResourceSelector{
				Name:        "pod-a",
				MatchLabels: map[string]string{"app": "demo"},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "tier",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"backend"},
				}},
				FieldSelector: "metadata.namespace=team-a",
			},
		}
		plan, err := planner.Plan(ctx, "team-a", source)
		if err != nil {
			t.Fatalf("plan source: %v", err)
		}
		if plan.SourceID() != source.ID || plan.APIVersion() != source.Resource.APIVersion || plan.Kind() != source.Resource.Kind {
			t.Fatalf("plan identity changed: %#v", plan)
		}
		if plan.LabelSelector() != "app=demo,tier in (backend)" {
			t.Fatalf("canonical label selector = %q", plan.LabelSelector())
		}
		if plan.FieldSelector() != "metadata.name=pod-a,metadata.namespace=team-a" {
			t.Fatalf("canonical field selector = %q", plan.FieldSelector())
		}
		want := []selection.ReadTarget{{
			SourceID:  "pod-source",
			GVR:       discoveryResolution(t, ctx, resolver, source).Resource,
			Kind:      "Pod",
			Scope:     discovery.ScopeNamespaced,
			Namespace: "team-a",
		}}
		if got := plan.Targets(); !reflect.DeepEqual(got, want) {
			t.Fatalf("plan targets = %#v, want %#v", got, want)
		}
		mutated := plan.Targets()
		mutated[0].Namespace = "changed"
		if plan.Targets()[0].Namespace != "team-a" {
			t.Fatal("plan target accessor exposed mutable internal state")
		}
	})

	t.Run("namespace targets derive from scope and preserve empty intent", func(t *testing.T) {
		base := func(namespaces *v1alpha1.NamespaceSelection) v1alpha1.KubeseerSource {
			return v1alpha1.KubeseerSource{ID: "namespace-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}, Namespaces: namespaces}
		}
		defaultPlan, err := planner.Plan(ctx, "team-owner", base(nil))
		if err != nil || len(defaultPlan.Targets()) != 1 || defaultPlan.Targets()[0].Namespace != "team-owner" {
			t.Fatalf("omitted namespaces plan = %#v err=%v", defaultPlan.Targets(), err)
		}
		explicitPlan, err := planner.Plan(ctx, "team-owner", base(&v1alpha1.NamespaceSelection{Names: []string{"team-b", "team-a"}}))
		if err != nil {
			t.Fatalf("explicit namespace plan: %v", err)
		}
		if got := explicitPlan.Targets(); len(got) != 2 || got[0].Namespace != "team-a" || got[1].Namespace != "team-b" {
			t.Fatalf("explicit namespace targets are not deterministic: %#v", got)
		}
		emptyPlan, err := planner.Plan(ctx, "team-owner", base(&v1alpha1.NamespaceSelection{Names: []string{}}))
		if err != nil || len(emptyPlan.Targets()) != 0 {
			t.Fatalf("explicit empty namespace plan = %#v err=%v", emptyPlan.Targets(), err)
		}

		cluster := v1alpha1.KubeseerSource{ID: "node-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Node"}}
		clusterPlan, err := planner.Plan(ctx, "team-owner", cluster)
		if err != nil || len(clusterPlan.Targets()) != 1 || clusterPlan.Targets()[0].Scope != discovery.ScopeCluster || clusterPlan.Targets()[0].Namespace != "" {
			t.Fatalf("cluster plan = %#v err=%v", clusterPlan.Targets(), err)
		}
		cluster.Namespaces = &v1alpha1.NamespaceSelection{Names: []string{}}
		if _, err := planner.Plan(ctx, "team-owner", cluster); !selection.HasReason(err, selection.ReasonInvalidNamespaceScope) {
			t.Fatalf("cluster namespace declaration error = %v", err)
		}
	})

	t.Run("defensive namespace and selector validation is source scoped", func(t *testing.T) {
		valid := func() v1alpha1.KubeseerSource {
			return v1alpha1.KubeseerSource{ID: "invalid-input", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		}
		invalidNamespaces := valid()
		invalidNamespaces.Namespaces = &v1alpha1.NamespaceSelection{Names: []string{"Invalid_Namespace"}}
		if _, err := planner.Plan(ctx, "team-owner", invalidNamespaces); !selection.HasReason(err, selection.ReasonInvalidNamespaceScope) {
			t.Fatalf("invalid namespace error = %v", err)
		}
		duplicateNamespaces := valid()
		duplicateNamespaces.Namespaces = &v1alpha1.NamespaceSelection{Names: []string{"team-a", "team-a"}}
		if _, err := planner.Plan(ctx, "team-owner", duplicateNamespaces); !selection.HasReason(err, selection.ReasonInvalidNamespaceScope) {
			t.Fatalf("duplicate namespace error = %v", err)
		}
		invalidLabels := valid()
		invalidLabels.Selector = &v1alpha1.ResourceSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "Unknown", Values: []string{"demo"}}}}
		if _, err := planner.Plan(ctx, "team-owner", invalidLabels); !selection.HasReason(err, selection.ReasonInvalidSelector) {
			t.Fatalf("invalid label selector error = %v", err)
		}
		invalidFields := valid()
		invalidFields.Selector = &v1alpha1.ResourceSelector{FieldSelector: "metadata.name in ("}
		if _, err := planner.Plan(ctx, "team-owner", invalidFields); !selection.HasReason(err, selection.ReasonInvalidSelector) {
			t.Fatalf("invalid field selector error = %v", err)
		}
	})

	t.Run("discovery failure stops planning before targets exist", func(t *testing.T) {
		discoveryFailure := errors.New("discovery payload should stay wrapped")
		failureResolver := discovery.NewResolver(&selectionDiscoveryFailureClient{delegate: newPolicyDiscoveryClient(), cause: discoveryFailure})
		source := v1alpha1.KubeseerSource{ID: "unavailable-source", Resource: v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"}}
		_, err := selection.NewPlanner(failureResolver).Plan(ctx, "team-a", source)
		if err == nil || !errors.Is(err, discoveryFailure) || !strings.Contains(err.Error(), `source "unavailable-source"`) {
			t.Fatalf("planning did not preserve source-scoped discovery failure: %v", err)
		}
	})
}

type selectionDiscoveryFailureClient struct {
	delegate *policyDiscoveryClient
	cause    error
}

func (c *selectionDiscoveryFailureClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if groupVersion == "apps/v1" {
		return nil, c.cause
	}
	return c.delegate.ServerResourcesForGroupVersion(groupVersion)
}

func discoveryResolution(t *testing.T, ctx context.Context, resolver *discovery.Resolver, source v1alpha1.KubeseerSource) discovery.Resolution {
	t.Helper()
	resolution, err := resolver.Resolve(ctx, discovery.SourceDescriptor{SourceID: source.ID, APIVersion: source.Resource.APIVersion, Kind: source.Resource.Kind})
	if err != nil {
		t.Fatalf("resolve %q: %v", source.ID, err)
	}
	return resolution
}
