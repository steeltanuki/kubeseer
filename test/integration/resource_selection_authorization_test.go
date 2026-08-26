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
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
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
	authorizations := authorizationsForPlan(plan, snapshot)

	t.Run("all exact allowed targets produce an opaque capability", func(t *testing.T) {
		authorized, err := selection.Bind(plan, authorizations)
		if err != nil {
			t.Fatalf("bind allowed plan: %v", err)
		}
		if got := authorized.Plan().Targets(); len(got) != 2 || got[0].Namespace != "team-a" || got[1].Namespace != "team-c" {
			t.Fatalf("authorized plan targets = %#v", got)
		}
	})

	t.Run("missing decision is rejected before capability creation", func(t *testing.T) {
		_, err := selection.Bind(plan, authorizations[:1])
		if !selection.HasReason(err, selection.ReasonAuthorizationMissing) || !strings.Contains(err.Error(), "authorization is missing") {
			t.Fatalf("missing authorization error = %v", err)
		}
	})

	tests := []struct {
		name       string
		mutate     func([]selection.Authorization)
		wantReason selection.SelectionErrorReason
		wantText   string
	}{
		{
			name: "denied decision",
			mutate: func(values []selection.Authorization) {
				values[1].Decision = accesspolicy.Decision{Reason: accesspolicy.ReasonNamespaceDenied, Message: "namespace is denied"}
			},
			wantReason: selection.ReasonAuthorizationDenied,
			wantText:   "exact read target is not allowed",
		},
		{
			name: "mismatched source identity",
			mutate: func(values []selection.Authorization) {
				values[0].Request.SourceID = "other-source"
			},
			wantReason: selection.ReasonAuthorizationMismatch,
			wantText:   "does not match the selection plan",
		},
		{
			name: "duplicate target decisions",
			mutate: func(values []selection.Authorization) {
				values[1].Request = values[0].Request
			},
			wantReason: selection.ReasonAuthorizationMismatch,
			wantText:   "multiple authorization decisions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := append([]selection.Authorization(nil), authorizations...)
			test.mutate(values)
			_, err := selection.Bind(plan, values)
			if !selection.HasReason(err, test.wantReason) || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("Bind error = %v, want reason=%q containing %q", err, test.wantReason, test.wantText)
			}
		})
	}

	t.Run("extra decisions are rejected without leaking object data", func(t *testing.T) {
		extra := append([]selection.Authorization(nil), authorizations...)
		extra = append(extra, selection.Authorization{Request: selection.RequestForTarget(plan.Targets()[0]), Decision: accesspolicy.Decision{Allowed: true, Reason: accesspolicy.ReasonAllowed}})
		_, err := selection.Bind(plan, extra)
		if !selection.HasReason(err, selection.ReasonAuthorizationMismatch) || strings.Contains(err.Error(), "secret-object-value") {
			t.Fatalf("extra authorization error = %v", err)
		}
	})
}

func authorizationsForPlan(plan selection.SelectionPlan, snapshot accesspolicy.Snapshot) []selection.Authorization {
	targets := plan.Targets()
	values := make([]selection.Authorization, 0, len(targets))
	for _, target := range targets {
		request := selection.RequestForTarget(target)
		values = append(values, selection.Authorization{Request: request, Decision: snapshot.Evaluate(request)})
	}
	return values
}
