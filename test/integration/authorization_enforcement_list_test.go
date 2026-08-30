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
	"fmt"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func assertAuthorizationEnforcementListScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	planner := selection.NewPlanner(resolver)
	source := v1alpha1.KubeseerSource{
		ID:         "authorization-list-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a", "team-c"}},
		Selector:   &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"app": "demo"}, FieldSelector: "metadata.namespace=team-a"},
	}
	plan, err := planner.Plan(ctx, "team-a", source)
	if err != nil {
		t.Fatalf("plan LIST enforcement fixture: %v", err)
	}
	oneTargetSource := source
	oneTargetSource.Namespaces = &v1alpha1.NamespaceSelection{Names: []string{"team-a"}}
	oneTargetPlan, err := planner.Plan(ctx, "team-a", oneTargetSource)
	if err != nil {
		t.Fatalf("plan single-target LIST enforcement fixture: %v", err)
	}
	snapshot := mustSnapshot(t, basePolicy())
	requests := make([]accesspolicy.Request, 0, len(plan.Targets()))
	for _, target := range plan.Targets() {
		requests = append(requests, selection.RequestForTarget(target))
	}
	subject := authorization.Subject{Key: types.NamespacedName{Namespace: "team-a", Name: "authorization-list-owner"}, UID: types.UID("authorization-list-owner-uid"), Generation: 1, PolicyEpoch: 4}

	t.Run("authorized read keeps exact target and selectors", func(t *testing.T) {
		batch, err := authorization.NewEnforcer(nil).EvaluateBatch(ctx, subject, snapshot, requests)
		if err != nil {
			t.Fatalf("evaluate LIST fixture: %v", err)
		}
		authorized, err := selection.BindCapabilities(plan, batch.Outcomes())
		if err != nil {
			t.Fatalf("bind LIST fixture: %v", err)
		}
		lister := &scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(nil, "")}, {list: listWithContinue(nil, "")}}}
		outcome := selection.NewExecutor(lister, selection.WithVerifier(authorization.VerifierFunc(func(candidate authorization.Subject) bool {
			return candidate == subject
		}))).Execute(ctx, authorized)
		if outcome.Err != nil {
			t.Fatalf("authorized LIST outcome: %v", outcome.Err)
		}
		if len(lister.calls) != 2 {
			t.Fatalf("authorized LIST calls = %d, want one exact call per namespace target", len(lister.calls))
		}
		for index, call := range lister.calls {
			if call.target != plan.Targets()[index] {
				t.Fatalf("call %d target = %#v, want %#v", index, call.target, plan.Targets()[index])
			}
			if call.options.LabelSelector != "app=demo" || call.options.FieldSelector != "metadata.namespace=team-a" {
				t.Fatalf("call %d selectors = %#v, want exact plan selectors", index, call.options)
			}
		}
	})

	t.Run("stale capability stops continuation and expired restart before I/O", func(t *testing.T) {
		current := true
		verifier := authorization.VerifierFunc(func(candidate authorization.Subject) bool {
			return current && candidate == subject
		})
		batch, err := authorization.NewEnforcer(nil).EvaluateBatch(ctx, subject, snapshot, []accesspolicy.Request{requests[0]})
		if err != nil {
			t.Fatalf("evaluate stale fixture: %v", err)
		}
		authorized, err := selection.BindCapabilities(oneTargetPlan, batch.Outcomes())
		if err != nil {
			t.Fatalf("bind stale fixture: %v", err)
		}
		lister := &scriptedResourceLister{
			responses: []scriptedListResponse{{err: apierrors.NewResourceExpired("continuation-expired-sentinel")}, {list: listWithContinue(nil, "")}},
			onCall: func(index int, _ context.Context) {
				if index == 0 {
					current = false
				}
			},
		}
		outcome := selection.NewExecutor(lister, selection.WithVerifier(verifier)).Execute(ctx, authorized)
		if !selection.HasReason(outcome.Err, selection.ReasonAuthorizationStale) || len(lister.calls) != 1 {
			t.Fatalf("expired continuation stale outcome = %#v calls=%d", outcome, len(lister.calls))
		}
	})

	t.Run("cancellation prevents later pages and source values", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		batch, err := authorization.NewEnforcer(nil).EvaluateBatch(ctx, subject, snapshot, []accesspolicy.Request{requests[0]})
		if err != nil {
			t.Fatalf("evaluate cancellation fixture: %v", err)
		}
		authorized, err := selection.BindCapabilities(oneTargetPlan, batch.Outcomes())
		if err != nil {
			t.Fatalf("bind cancellation fixture: %v", err)
		}
		lister := &scriptedResourceLister{
			responses: []scriptedListResponse{{list: listWithContinue(nil, "next")}, {list: listWithContinue(nil, "")}},
			onCall: func(index int, _ context.Context) {
				if index == 0 {
					cancel()
				}
			},
		}
		outcome := selection.NewExecutor(lister, selection.WithVerifier(authorization.VerifierFunc(func(candidate authorization.Subject) bool {
			return candidate == subject
		}))).Execute(canceled, authorized)
		if !selection.HasReason(outcome.Err, selection.ReasonReadInterrupted) || len(outcome.Resources) != 0 || len(lister.calls) != 1 {
			t.Fatalf("canceled continuation outcome = %#v calls=%d", outcome, len(lister.calls))
		}
	})

	t.Run("RBAC Forbidden emits linked sanitized evidence", func(t *testing.T) {
		var records []authorization.Record
		recorder := authorization.RecorderFunc(func(_ context.Context, observed []authorization.Record) {
			records = append(records, observed...)
		})
		batch, err := authorization.NewEnforcer(recorder).EvaluateBatch(ctx, subject, snapshot, []accesspolicy.Request{requests[0]})
		if err != nil {
			t.Fatalf("evaluate forbidden fixture: %v", err)
		}
		authorized, err := selection.BindCapabilities(oneTargetPlan, batch.Outcomes())
		if err != nil {
			t.Fatalf("bind forbidden fixture: %v", err)
		}
		lister := &scriptedResourceLister{responses: []scriptedListResponse{{err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "forbidden-object-sentinel", errors.New("raw-rbac-body-sentinel"))}}}
		outcome := selection.NewExecutor(lister, selection.WithVerifier(authorization.VerifierFunc(func(candidate authorization.Subject) bool {
			return candidate == subject
		}))).Execute(ctx, authorized)
		if !selection.HasReason(outcome.Err, selection.ReasonReadForbidden) || len(outcome.Resources) != 0 {
			t.Fatalf("forbidden LIST outcome = %#v", outcome)
		}
		if len(records) != 2 || records[0].Kind != authorization.RecordPolicyDecision || records[1].Kind != authorization.RecordReadForbidden || records[1].Outcome != authorization.OutcomeForbidden {
			t.Fatalf("forbidden evidence = %#v, want logical and linked enforcement records", records)
		}
		if records[1].SourceID != requests[0].SourceID || records[1].Subject != subject {
			t.Fatalf("forbidden evidence linkage = %#v", records[1])
		}
		for _, record := range records {
			if strings.Contains(fmt.Sprintf("%#v", record), "forbidden-object-sentinel") || strings.Contains(fmt.Sprintf("%#v", record), "raw-rbac-body-sentinel") {
				t.Fatalf("forbidden evidence leaked API details: %#v", record)
			}
		}
	})

	t.Run("denied binding issues no LIST and sibling source remains independent", func(t *testing.T) {
		deniedPolicy := basePolicy()
		deniedPolicy.Spec.Resources = nil
		deniedPlan := oneTargetPlan
		deniedBatch, err := authorization.NewEnforcer(nil).EvaluateBatch(ctx, subject, mustSnapshot(t, deniedPolicy), []accesspolicy.Request{requests[0]})
		if err != nil {
			t.Fatalf("evaluate denied fixture: %v", err)
		}
		if _, err := selection.BindCapabilities(deniedPlan, deniedBatch.Outcomes()); !selection.HasReason(err, selection.ReasonAuthorizationDenied) {
			t.Fatalf("denied binding error = %v", err)
		}
		allowedBatch, err := authorization.NewEnforcer(nil).EvaluateBatch(ctx, subject, snapshot, []accesspolicy.Request{requests[0]})
		if err != nil {
			t.Fatalf("evaluate sibling fixture: %v", err)
		}
		allowed, err := selection.BindCapabilities(deniedPlan, allowedBatch.Outcomes())
		if err != nil {
			t.Fatalf("bind sibling fixture: %v", err)
		}
		lister := &scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(nil, "")}}}
		outcomes := selection.NewExecutor(lister, selection.WithVerifier(authorization.VerifierFunc(func(candidate authorization.Subject) bool {
			return candidate == subject
		}))).SelectBatch(ctx, []selection.AuthorizedPlan{allowed})
		if len(outcomes) != 1 || outcomes[0].Err != nil || len(lister.calls) != 1 {
			t.Fatalf("authorized sibling outcome = %#v calls=%d", outcomes, len(lister.calls))
		}
	})
}
