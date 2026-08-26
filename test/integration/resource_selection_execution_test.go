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
	"sync"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// assertResourceSelectionExecutionBoundaryScenarios exercises the complete
// planner/binder/executor path while replacing only the outbound list port.
// In particular, rejected authorization inputs must not reach that port.
func assertResourceSelectionExecutionBoundaryScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	planner := selection.NewPlanner(resolver)
	source := v1alpha1.KubeseerSource{
		ID:       "execution-boundary-source",
		Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Selector: &v1alpha1.ResourceSelector{Name: "authorized-pod"},
	}
	plan, err := planner.Plan(ctx, "team-a", source)
	if err != nil {
		t.Fatalf("plan execution boundary fixture: %v", err)
	}
	if len(plan.Targets()) != 1 {
		t.Fatalf("execution boundary plan targets = %#v", plan.Targets())
	}

	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "authorized-pod",
			"namespace": "team-a",
			"uid":       "uid-authorized-pod",
		},
	}}
	lister := &countingResourceLister{response: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*object}}}
	authorized, err := selection.Bind(plan, []selection.Authorization{{Request: selection.RequestForTarget(plan.Targets()[0]), Decision: allowedDecision()}})
	if err != nil {
		t.Fatalf("bind allowed execution boundary fixture: %v", err)
	}
	outcome := selection.NewExecutor(lister).Execute(ctx, authorized)
	if outcome.Err != nil || len(outcome.Resources) != 1 {
		t.Fatalf("authorized execution outcome = %#v", outcome)
	}
	if got := outcome.Resources[0].Provenance; got.APIVersion != "v1" || got.Kind != "Pod" || got.Namespace != "team-a" || got.Name != "authorized-pod" || got.UID != types.UID("uid-authorized-pod") {
		t.Fatalf("authorized execution provenance = %#v", got)
	}

	calls := lister.Calls()
	if len(calls) != 1 {
		t.Fatalf("authorized execution issued %d LIST calls, want 1", len(calls))
	}
	wantTarget := plan.Targets()[0]
	if !reflect.DeepEqual(calls[0].target, wantTarget) {
		t.Fatalf("LIST target = %#v, want %#v", calls[0].target, wantTarget)
	}
	wantOptions := metav1.ListOptions{FieldSelector: "metadata.name=authorized-pod", LabelSelector: "", Limit: 500}
	if calls[0].options != wantOptions {
		t.Fatalf("LIST options = %#v, want %#v", calls[0].options, wantOptions)
	}

	t.Run("denied authorization does not issue a list", func(t *testing.T) {
		denied := []selection.Authorization{{Request: selection.RequestForTarget(plan.Targets()[0]), Decision: deniedDecision()}}
		if _, err := selection.Bind(plan, denied); !selection.HasReason(err, selection.ReasonAuthorizationDenied) {
			t.Fatalf("denied bind error = %v", err)
		}
		if got := len(lister.Calls()); got != 1 {
			t.Fatalf("denied bind changed LIST call count to %d", got)
		}
	})

	t.Run("mismatched authorization does not issue a list", func(t *testing.T) {
		mismatched := []selection.Authorization{{
			Request:  selection.RequestForTarget(selection.ReadTarget{SourceID: "other-source", GVR: wantTarget.GVR, Kind: wantTarget.Kind, Scope: wantTarget.Scope, Namespace: wantTarget.Namespace}),
			Decision: allowedDecision(),
		}}
		if _, err := selection.Bind(plan, mismatched); !selection.HasReason(err, selection.ReasonAuthorizationMismatch) {
			t.Fatalf("mismatched bind error = %v", err)
		}
		if got := len(lister.Calls()); got != 1 {
			t.Fatalf("mismatched bind changed LIST call count to %d", got)
		}
	})

	t.Run("explicit empty namespace plan completes without a list", func(t *testing.T) {
		emptySource := v1alpha1.KubeseerSource{
			ID:         "execution-empty-source",
			Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{}},
		}
		emptyPlan, err := planner.Plan(ctx, "team-a", emptySource)
		if err != nil {
			t.Fatalf("plan empty execution fixture: %v", err)
		}
		emptyAuthorized, err := selection.Bind(emptyPlan, nil)
		if err != nil {
			t.Fatalf("bind empty execution fixture: %v", err)
		}
		emptyOutcome := selection.NewExecutor(lister).Execute(ctx, emptyAuthorized)
		if emptyOutcome.Err != nil || len(emptyOutcome.Resources) != 0 {
			t.Fatalf("empty execution outcome = %#v", emptyOutcome)
		}
		if got := len(lister.Calls()); got != 1 {
			t.Fatalf("empty execution changed LIST call count to %d", got)
		}
	})
}

type recordedListCall struct {
	target  selection.ReadTarget
	options metav1.ListOptions
	ctx     context.Context
}

type countingResourceLister struct {
	mu       sync.Mutex
	calls    []recordedListCall
	response *unstructured.UnstructuredList
	err      error
}

func (l *countingResourceLister) List(ctx context.Context, target selection.ReadTarget, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	l.mu.Lock()
	l.calls = append(l.calls, recordedListCall{target: target, options: options, ctx: ctx})
	var response *unstructured.UnstructuredList
	if l.response != nil {
		response = l.response.DeepCopy()
	}
	err := l.err
	l.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if response == nil {
		response = &unstructured.UnstructuredList{}
	}
	if ctx == nil {
		return response, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return response, nil
}

func (l *countingResourceLister) Calls() []recordedListCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]recordedListCall(nil), l.calls...)
}

func deniedDecision() accesspolicy.Decision {
	return accesspolicy.Decision{Reason: accesspolicy.ReasonResourceDenied, Message: "resource is denied by the installation policy"}
}

var _ selection.ResourceLister = (*countingResourceLister)(nil)
