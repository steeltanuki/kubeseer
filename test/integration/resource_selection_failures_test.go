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
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func assertResourceSelectionFailureScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	t.Run("a target failure discards all prior source matches", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID:         "atomic-source",
			Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a", "team-c"}},
		}
		authorized := planAndAuthorizeSelection(t, ctx, resolver, "team-a", source)
		backendFailure := errors.New("backend payload contains secret-value")
		lister := &scriptedResourceLister{responses: []scriptedListResponse{
			{list: listWithContinue(paginationObject("team-a", "partial-pod", "uid-partial"), "")},
			{err: backendFailure},
		}}
		outcome := newSelectionExecutor(lister).Execute(ctx, authorized)
		if !selection.HasReason(outcome.Err, selection.ReasonReadUnavailable) || !errors.Is(outcome.Err, backendFailure) || len(outcome.Resources) != 0 {
			t.Fatalf("atomic failure outcome = %#v", outcome)
		}
		if strings.Contains(outcome.Err.Error(), "secret-value") || strings.Contains(outcome.Err.Error(), "partial-pod") {
			t.Fatalf("atomic failure diagnostic leaked data: %v", outcome.Err)
		}
	})

	t.Run("forbidden runtime reads stay distinct from policy denial", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{ID: "rbac-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		plan := planSelection(t, ctx, resolver, "team-a", source)
		runtimeFailure := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "forbidden-pod", errors.New("raw-rbac-detail"))
		lister := &scriptedResourceLister{responses: []scriptedListResponse{{err: runtimeFailure}}}
		authorized, err := selection.BindCapabilities(plan, authorizationOutcomesForPlan(t, plan, mustSnapshot(t, basePolicy())))
		if err != nil {
			t.Fatalf("bind runtime-forbidden fixture: %v", err)
		}
		outcome := newSelectionExecutor(lister).Execute(ctx, authorized)
		if !selection.HasReason(outcome.Err, selection.ReasonReadForbidden) || !errors.Is(outcome.Err, runtimeFailure) {
			t.Fatalf("runtime forbidden outcome = %#v", outcome)
		}
		if strings.Contains(outcome.Err.Error(), "forbidden-pod") || strings.Contains(outcome.Err.Error(), "raw-rbac-detail") {
			t.Fatalf("runtime forbidden diagnostic leaked API details: %v", outcome.Err)
		}

		deniedLister := &countingResourceLister{}
		denyingPolicy := basePolicy()
		denyingPolicy.Spec.Resources = nil
		if _, err := selection.BindCapabilities(plan, authorizationOutcomesForPlan(t, plan, mustSnapshot(t, denyingPolicy))); !selection.HasReason(err, selection.ReasonAuthorizationDenied) {
			t.Fatalf("installation-policy denial = %v", err)
		}
		if got := len(deniedLister.Calls()); got != 0 {
			t.Fatalf("policy denial unexpectedly issued %d LIST calls", got)
		}
	})

	t.Run("unsupported, interrupted, unavailable, and invalid-object failures are stable", func(t *testing.T) {
		unsupportedSource := v1alpha1.KubeseerSource{
			ID:       "unsupported-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Selector: &v1alpha1.ResourceSelector{FieldSelector: "spec.unsupported=secret-value"},
		}
		unsupportedPlan := planSelection(t, ctx, resolver, "team-a", unsupportedSource)
		failure := apierrors.NewBadRequest("field spec.unsupported is not supported: secret-value")
		first := newSelectionExecutor(&scriptedResourceLister{responses: []scriptedListResponse{{err: failure}}}).Execute(ctx, mustBindSelection(t, unsupportedPlan))
		second := newSelectionExecutor(&scriptedResourceLister{responses: []scriptedListResponse{{err: failure}}}).Execute(ctx, mustBindSelection(t, unsupportedPlan))
		if !selection.HasReason(first.Err, selection.ReasonUnsupportedSelector) || !selection.HasReason(second.Err, selection.ReasonUnsupportedSelector) || first.Err.Reason != second.Err.Reason {
			t.Fatalf("unsupported selector stability = %#v / %#v", first.Err, second.Err)
		}
		if strings.Contains(first.Err.Error(), "secret-value") || strings.Contains(first.Err.Error(), "spec.unsupported") {
			t.Fatalf("unsupported selector diagnostic leaked selector value: %v", first.Err)
		}

		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()
		canceledSource := v1alpha1.KubeseerSource{ID: "canceled-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		canceledPlan := planSelection(t, ctx, resolver, "team-a", canceledSource)
		canceledLister := &countingResourceLister{}
		canceledOutcome := newSelectionExecutor(canceledLister).Execute(canceledCtx, mustBindSelection(t, canceledPlan))
		if !selection.HasReason(canceledOutcome.Err, selection.ReasonReadInterrupted) || len(canceledLister.Calls()) != 0 {
			t.Fatalf("canceled outcome = %#v calls=%d", canceledOutcome, len(canceledLister.Calls()))
		}

		unavailableSource := v1alpha1.KubeseerSource{ID: "unavailable-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		unavailablePlan := planSelection(t, ctx, resolver, "team-a", unavailableSource)
		unavailable := newSelectionExecutor(&scriptedResourceLister{responses: []scriptedListResponse{{returnNil: true}}}).Execute(ctx, mustBindSelection(t, unavailablePlan))
		if !selection.HasReason(unavailable.Err, selection.ReasonReadUnavailable) {
			t.Fatalf("nil response outcome = %#v", unavailable)
		}

		invalidSource := v1alpha1.KubeseerSource{ID: "invalid-object-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		invalidPlan := planSelection(t, ctx, resolver, "team-a", invalidSource)
		invalidObject := newSelectionExecutor(&scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(&unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "missing-uid"}}}, "")}}}).Execute(ctx, mustBindSelection(t, invalidPlan))
		if !selection.HasReason(invalidObject.Err, selection.ReasonInvalidObject) || len(invalidObject.Resources) != 0 {
			t.Fatalf("invalid object outcome = %#v", invalidObject)
		}
	})

	t.Run("batch keeps completed siblings and interrupts only unstarted sources", func(t *testing.T) {
		firstSource := v1alpha1.KubeseerSource{ID: "batch-first", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		secondSource := v1alpha1.KubeseerSource{ID: "batch-second", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		thirdSource := v1alpha1.KubeseerSource{ID: "batch-third", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		firstPlan := planSelection(t, ctx, resolver, "team-a", firstSource)
		secondPlan := planSelection(t, ctx, resolver, "team-a", secondSource)
		thirdPlan := planSelection(t, ctx, resolver, "team-a", thirdSource)
		firstAuthorized := mustBindSelection(t, firstPlan)
		secondAuthorized := mustBindSelection(t, secondPlan)
		thirdAuthorized := mustBindSelection(t, thirdPlan)

		lister := &scriptedResourceLister{responses: []scriptedListResponse{
			{list: listWithContinue(paginationObject("team-a", "batch-first-pod", "uid-batch-first"), "")},
			{err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "batch-second-pod", nil)},
		}}
		outcomes := newSelectionExecutor(lister).SelectBatch(ctx, []selection.AuthorizedPlan{firstAuthorized, secondAuthorized, thirdAuthorized})
		if len(outcomes) != 3 || outcomes[0].Err != nil || len(outcomes[0].Resources) != 1 || !selection.HasReason(outcomes[1].Err, selection.ReasonReadForbidden) || !selection.HasReason(outcomes[2].Err, selection.ReasonReadUnavailable) {
			t.Fatalf("batch outcomes = %#v", outcomes)
		}

		cancelCtx, cancel := context.WithCancel(ctx)
		cancelLister := &scriptedResourceLister{
			responses: []scriptedListResponse{{list: listWithContinue(paginationObject("team-a", "cancel-first-pod", "uid-cancel-first"), "")}},
			onCall: func(index int, _ context.Context) {
				if index == 0 {
					cancel()
				}
			},
		}
		canceledOutcomes := newSelectionExecutor(cancelLister).SelectBatch(cancelCtx, []selection.AuthorizedPlan{firstAuthorized, secondAuthorized, thirdAuthorized})
		if len(canceledOutcomes) != 3 || canceledOutcomes[0].Err != nil || !selection.HasReason(canceledOutcomes[1].Err, selection.ReasonReadInterrupted) || !selection.HasReason(canceledOutcomes[2].Err, selection.ReasonReadInterrupted) || len(cancelLister.calls) != 1 {
			t.Fatalf("canceled batch outcomes = %#v calls=%d", canceledOutcomes, len(cancelLister.calls))
		}
	})
}

func planSelection(t *testing.T, ctx context.Context, resolver *discovery.Resolver, ownerNamespace string, source v1alpha1.KubeseerSource) selection.SelectionPlan {
	t.Helper()
	plan, err := selection.NewPlanner(resolver).Plan(ctx, ownerNamespace, source)
	if err != nil {
		t.Fatalf("plan %q: %v", source.ID, err)
	}
	return plan
}

func mustBindSelection(t *testing.T, plan selection.SelectionPlan) selection.AuthorizedPlan {
	t.Helper()
	authorized, err := selection.BindCapabilities(plan, authorizationOutcomesForPlan(t, plan, mustSnapshot(t, basePolicy())))
	if err != nil {
		t.Fatalf("bind %q: %v", plan.SourceID(), err)
	}
	return authorized
}
