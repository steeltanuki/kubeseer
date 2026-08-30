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
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"k8s.io/apimachinery/pkg/types"
)

func assertResourceSelectionAuthorizationScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	planner := selection.NewPlanner(resolver)
	source := v1alpha1.KubeseerSource{
		ID:         "authorized-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a", "team-c"}},
	}
	plan, err := planner.Plan(ctx, "owner", source)
	if err != nil {
		t.Fatalf("plan authorization fixture: %v", err)
	}
	snapshot := mustSnapshot(t, basePolicy())
	authorizations := authorizationOutcomesForPlan(t, plan, snapshot)

	t.Run("all exact allowed targets produce an opaque capability", func(t *testing.T) {
		authorized, err := selection.BindCapabilities(plan, authorizations)
		if err != nil {
			t.Fatalf("bind allowed plan: %v", err)
		}
		if got := authorized.Plan().Targets(); len(got) != 2 || got[0].Namespace != "team-a" || got[1].Namespace != "team-c" {
			t.Fatalf("authorized plan targets = %#v", got)
		}
	})

	t.Run("missing decision is rejected before capability creation", func(t *testing.T) {
		_, err := selection.BindCapabilities(plan, authorizations[:1])
		if !selection.HasReason(err, selection.ReasonAuthorizationMissing) || !strings.Contains(err.Error(), "authorization is missing") {
			t.Fatalf("missing authorization error = %v", err)
		}
	})

	tests := []struct {
		name       string
		outcomes   func() []authorization.DecisionOutcome
		wantReason selection.SelectionErrorReason
		wantText   string
	}{
		{
			name: "denied decision",
			outcomes: func() []authorization.DecisionOutcome {
				denyingPolicy := basePolicy()
				denyingPolicy.Spec.Resources = nil
				return authorizationOutcomesForPlan(t, plan, mustSnapshot(t, denyingPolicy))
			},
			wantReason: selection.ReasonAuthorizationDenied,
			wantText:   "exact read target is not allowed",
		},
		{
			name: "mismatched source identity",
			outcomes: func() []authorization.DecisionOutcome {
				request := selection.RequestForTarget(plan.Targets()[0])
				request.SourceID = "other-source"
				return authorizationOutcomesForRequests(t, snapshot, []accesspolicy.Request{request})
			},
			wantReason: selection.ReasonAuthorizationMismatch,
			wantText:   "does not match the selection plan",
		},
		{
			name: "duplicate target decisions",
			outcomes: func() []authorization.DecisionOutcome {
				request := selection.RequestForTarget(plan.Targets()[0])
				return authorizationOutcomesForRequests(t, snapshot, []accesspolicy.Request{request, request})
			},
			wantReason: selection.ReasonAuthorizationMismatch,
			wantText:   "multiple authorization decisions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := selection.BindCapabilities(plan, test.outcomes())
			if !selection.HasReason(err, test.wantReason) || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("BindCapabilities error = %v, want reason=%q containing %q", err, test.wantReason, test.wantText)
			}
		})
	}

	t.Run("extra decisions are rejected without leaking object data", func(t *testing.T) {
		extraRequest := selection.RequestForTarget(plan.Targets()[0])
		extraRequest.SourceID = "extra-source"
		extra := append([]authorization.DecisionOutcome(nil), authorizations...)
		extra = append(extra, authorizationOutcomesForRequests(t, snapshot, []accesspolicy.Request{extraRequest})...)
		_, err := selection.BindCapabilities(plan, extra)
		if !selection.HasReason(err, selection.ReasonAuthorizationMismatch) || strings.Contains(err.Error(), "secret-object-value") {
			t.Fatalf("extra authorization error = %v", err)
		}
	})
}

func authorizationOutcomesForPlan(t *testing.T, plan selection.SelectionPlan, snapshot accesspolicy.Snapshot) []authorization.DecisionOutcome {
	t.Helper()
	targets := plan.Targets()
	requests := make([]accesspolicy.Request, 0, len(targets))
	for _, target := range targets {
		requests = append(requests, selection.RequestForTarget(target))
	}
	return authorizationOutcomesForRequests(t, snapshot, requests)
}

func authorizationOutcomesForRequests(t *testing.T, snapshot accesspolicy.Snapshot, requests []accesspolicy.Request) []authorization.DecisionOutcome {
	t.Helper()
	subject := authorization.Subject{
		Key:        types.NamespacedName{Namespace: "selection-test", Name: "selection-owner"},
		UID:        types.UID("selection-owner-uid"),
		Generation: 1,
	}
	batch, err := authorization.NewEnforcer(nil).EvaluateBatch(context.Background(), subject, snapshot, requests)
	if err != nil {
		t.Fatalf("evaluate authorization fixture: %v", err)
	}
	return batch.Outcomes()
}

func newSelectionExecutor(lister selection.ResourceLister, options ...selection.ExecutorOption) *selection.Executor {
	configured := append([]selection.ExecutorOption(nil), options...)
	configured = append(configured, selection.WithVerifier(authorization.VerifierFunc(func(subject authorization.Subject) bool {
		return subject.Validate() == nil
	})))
	return selection.NewExecutor(lister, configured...)
}
