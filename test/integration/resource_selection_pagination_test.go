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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type paginationContextKey struct{}

type scriptedListResponse struct {
	list      *unstructured.UnstructuredList
	err       error
	returnNil bool
}

type scriptedResourceLister struct {
	responses  []scriptedListResponse
	calls      []recordedListCall
	contextKey interface{}
	contextVal interface{}
	onCall     func(int, context.Context)
}

func (l *scriptedResourceLister) List(ctx context.Context, read selection.AuthorizedRead, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	target := read.Target()
	if l.contextKey != nil && (ctx == nil || ctx.Value(l.contextKey) != l.contextVal) {
		return nil, errors.New("selection context was not propagated")
	}
	callIndex := len(l.calls)
	l.calls = append(l.calls, recordedListCall{target: target, options: options, ctx: ctx})
	if l.onCall != nil {
		l.onCall(callIndex, ctx)
	}
	if len(l.responses) == 0 {
		return nil, errors.New("scripted LIST response exhausted")
	}
	response := l.responses[0]
	l.responses = l.responses[1:]
	if response.err != nil {
		return nil, response.err
	}
	if response.returnNil {
		return nil, nil
	}
	if response.list == nil {
		return &unstructured.UnstructuredList{}, nil
	}
	return response.list.DeepCopy(), nil
}

func assertResourceSelectionPaginationScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	ownerNamespace := "team-a"
	source := v1alpha1.KubeseerSource{
		ID:         "pagination-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{ownerNamespace}},
		Selector:   &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"app": "demo"}, FieldSelector: "metadata.namespace=team-a"},
	}
	authorized := planAndAuthorizeSelection(t, ctx, resolver, ownerNamespace, source)
	target := authorized.Plan().Targets()[0]
	markerCtx := context.WithValue(ctx, paginationContextKey{}, "pagination-marker")

	t.Run("continued pages preserve target and selectors and include empty pages", func(t *testing.T) {
		alpha := paginationObject(ownerNamespace, "alpha", "uid-alpha")
		beta := paginationObject(ownerNamespace, "beta", "uid-beta")
		lister := &scriptedResourceLister{
			contextKey: paginationContextKey{},
			contextVal: "pagination-marker",
			responses: []scriptedListResponse{
				{list: listWithContinue(beta, "token-1")},
				{list: listWithContinue(nil, "token-2")},
				{list: listWithContinue(alpha, "")},
			},
		}
		outcome := newSelectionExecutor(lister, selection.WithPageLimit(2)).Execute(markerCtx, authorized)
		if outcome.Err != nil || len(outcome.Resources) != 2 {
			t.Fatalf("paginated outcome = %#v", outcome)
		}
		if got := []string{outcome.Resources[0].Provenance.Name, outcome.Resources[1].Provenance.Name}; !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
			t.Fatalf("paginated result order = %#v", got)
		}
		if got := len(lister.calls); got != 3 {
			t.Fatalf("paginated LIST calls = %d, want 3", got)
		}
		for index, call := range lister.calls {
			if !reflect.DeepEqual(call.target, target) {
				t.Fatalf("page %d target = %#v, want %#v", index, call.target, target)
			}
			if call.options.LabelSelector != "app=demo" || call.options.FieldSelector != "metadata.namespace=team-a" || call.options.Limit != 2 {
				t.Fatalf("page %d options = %#v", index, call.options)
			}
			if call.ctx == nil || call.ctx.Value(paginationContextKey{}) != "pagination-marker" {
				t.Fatalf("page %d lost context value", index)
			}
		}
		if got := []string{lister.calls[0].options.Continue, lister.calls[1].options.Continue, lister.calls[2].options.Continue}; !reflect.DeepEqual(got, []string{"", "token-1", "token-2"}) {
			t.Fatalf("continuation tokens = %#v", got)
		}

		original := beta.DeepCopy()
		for _, resource := range outcome.Resources {
			if resource.Provenance.Name == "beta" {
				resource.Object.SetLabels(map[string]string{"mutated": "true"})
			}
		}
		if !reflect.DeepEqual(beta.GetLabels(), original.GetLabels()) {
			t.Fatal("returned object mutation changed scripted source object")
		}
	})

	t.Run("equivalent page boundaries produce the same UID-deduplicated result", func(t *testing.T) {
		alpha := paginationObject(ownerNamespace, "alpha", "uid-alpha")
		beta := paginationObject(ownerNamespace, "beta", "uid-beta")
		first := &scriptedResourceLister{responses: []scriptedListResponse{
			{list: listWithContinue(beta, "next")},
			{list: listWithContinue(alpha, "")},
		}}
		second := &scriptedResourceLister{responses: []scriptedListResponse{
			{list: listWithContinue(alpha, beta, "")},
		}}
		firstOutcome := newSelectionExecutor(first, selection.WithPageLimit(1)).Execute(ctx, authorized)
		secondOutcome := newSelectionExecutor(second, selection.WithPageLimit(1)).Execute(ctx, authorized)
		if firstOutcome.Err != nil || secondOutcome.Err != nil {
			t.Fatalf("equivalent boundary errors = %#v / %#v", firstOutcome.Err, secondOutcome.Err)
		}
		if got, want := selectionOutcomeSignature(firstOutcome), selectionOutcomeSignature(secondOutcome); !reflect.DeepEqual(got, want) {
			t.Fatalf("equivalent boundary signatures = %#v, want %#v", got, want)
		}

		duplicate := paginationObject(ownerNamespace, "alpha", "uid-alpha")
		dedup := &scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(alpha, "next")}, {list: listWithContinue(duplicate, "")}}}
		dedupOutcome := newSelectionExecutor(dedup).Execute(ctx, authorized)
		if dedupOutcome.Err != nil || len(dedupOutcome.Resources) != 1 || dedupOutcome.Resources[0].Provenance.UID != "uid-alpha" {
			t.Fatalf("UID deduplication outcome = %#v", dedupOutcome)
		}
	})

	t.Run("one expiration restarts the target and discards partial pages", func(t *testing.T) {
		alpha := paginationObject(ownerNamespace, "alpha", "uid-alpha")
		expired := &scriptedResourceLister{responses: []scriptedListResponse{
			{err: apierrors.NewResourceExpired("expired continuation")},
			{list: listWithContinue(alpha, "")},
		}}
		outcome := newSelectionExecutor(expired).Execute(ctx, authorized)
		if outcome.Err != nil || len(outcome.Resources) != 1 || len(expired.calls) != 2 {
			t.Fatalf("single-restart outcome = %#v calls=%d", outcome, len(expired.calls))
		}
		if expired.calls[0].options.Continue != "" || expired.calls[1].options.Continue != "" {
			t.Fatalf("single-restart continuation tokens = %#v", expired.calls)
		}
	})

	t.Run("second expiration fails atomically with a stable reason", func(t *testing.T) {
		alpha := paginationObject(ownerNamespace, "alpha", "uid-alpha")
		expiredError := apierrors.NewResourceExpired("expired continuation")
		secondExpired := &scriptedResourceLister{responses: []scriptedListResponse{
			{list: listWithContinue(alpha, "next")},
			{err: expiredError},
			{list: listWithContinue(alpha, "next")},
			{err: expiredError},
		}}
		outcome := newSelectionExecutor(secondExpired).Execute(ctx, authorized)
		if !selection.HasReason(outcome.Err, selection.ReasonListExpired) || !errors.Is(outcome.Err, expiredError) || len(outcome.Resources) != 0 {
			t.Fatalf("second-expiration outcome = %#v", outcome)
		}
		if strings.Contains(outcome.Err.Error(), "expired continuation") || strings.Contains(outcome.Err.Error(), "uid-alpha") {
			t.Fatalf("second-expiration diagnostic leaked upstream details: %v", outcome.Err)
		}
	})
}

func planAndAuthorizeSelection(t *testing.T, ctx context.Context, resolver *discovery.Resolver, ownerNamespace string, source v1alpha1.KubeseerSource) selection.AuthorizedPlan {
	t.Helper()
	plan, err := selection.NewPlanner(resolver).Plan(ctx, ownerNamespace, source)
	if err != nil {
		t.Fatalf("plan %q: %v", source.ID, err)
	}
	authorized, err := selection.BindCapabilities(plan, authorizationOutcomesForPlan(t, plan, mustSnapshot(t, basePolicy())))
	if err != nil {
		t.Fatalf("bind %q: %v", source.ID, err)
	}
	return authorized
}

func paginationObject(namespace, name, uid string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"uid":       uid,
		},
	}}
}

func listWithContinue(objects ...interface{}) *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{}
	for _, object := range objects {
		switch value := object.(type) {
		case *unstructured.Unstructured:
			list.Items = append(list.Items, *value)
		case string:
			list.SetContinue(value)
		case nil:
		}
	}
	return list
}

func selectionOutcomeSignature(outcome selection.SelectionOutcome) []string {
	if outcome.Err != nil {
		return []string{string(outcome.Err.Reason)}
	}
	signature := make([]string, 0, len(outcome.Resources))
	for _, resource := range outcome.Resources {
		signature = append(signature, resource.Provenance.Namespace+"/"+resource.Provenance.Name+"/"+string(resource.Provenance.UID))
	}
	return signature
}
