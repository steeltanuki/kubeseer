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

package accesspolicy

import (
	"reflect"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/discovery"
)

func TestEvaluatePolicy(t *testing.T) {
	compiled, err := Compile(validPolicy())
	if err != nil {
		t.Fatalf("Compile() unexpected error: %v", err)
	}
	validSnapshot := NewSnapshot(compiled)
	clusterPolicy := validPolicy()
	clusterPolicy.Spec.Resources[0].Kinds = append(clusterPolicy.Spec.Resources[0].Kinds, "Node")
	clusterCompiled, err := Compile(clusterPolicy)
	if err != nil {
		t.Fatalf("Compile(cluster policy) unexpected error: %v", err)
	}
	clusterDisabledSnapshot := NewSnapshot(clusterCompiled)
	systemIncludedPolicy := validPolicy()
	systemIncludedPolicy.Spec.Namespaces.Include = append(systemIncludedPolicy.Spec.Namespaces.Include, "kube-system")
	systemIncludedCompiled, err := Compile(systemIncludedPolicy)
	if err != nil {
		t.Fatalf("Compile(system-included policy) unexpected error: %v", err)
	}
	systemIncludedSnapshot := NewSnapshot(systemIncludedCompiled)

	tests := []struct {
		name     string
		snapshot Snapshot
		request  Request
		want     Decision
	}{
		{
			name: "core namespaced resource is allowed",
			request: Request{
				SourceID:  "pods",
				APIGroup:  "",
				Kind:      "Pod",
				Namespace: "team-a",
				Scope:     discovery.ScopeNamespaced,
			},
			want: Decision{Allowed: true, Reason: ReasonAllowed, Message: "request is allowed by the installation policy"},
		},
		{
			name: "resource group and kind are matched exactly",
			request: Request{
				APIGroup:  "apps",
				Kind:      "Service",
				Namespace: "team-a",
				Scope:     discovery.ScopeNamespaced,
			},
			want: Decision{Allowed: true, Reason: ReasonAllowed, Message: "request is allowed by the installation policy"},
		},
		{
			name: "resource denial precedes namespace denial",
			request: Request{
				APIGroup:  "batch",
				Kind:      "Job",
				Namespace: "forbidden",
				Scope:     discovery.ScopeNamespaced,
			},
			want: Decision{Reason: ReasonResourceDenied, Message: `resource type "batch"/"Job" is outside the installation policy`},
		},
		{
			name: "excluded namespace wins over explicit inclusion",
			request: Request{
				APIGroup:  "",
				Kind:      "Pod",
				Namespace: "team-b",
				Scope:     discovery.ScopeNamespaced,
			},
			want: Decision{Reason: ReasonNamespaceDenied, Message: `namespace "team-b" is excluded by the installation policy`},
		},
		{
			name: "system namespace is denied by default in all non-system mode",
			request: Request{
				APIGroup:  "",
				Kind:      "Pod",
				Namespace: "kube-system",
				Scope:     discovery.ScopeNamespaced,
			},
			want: Decision{Reason: ReasonNamespaceDenied, Message: `namespace "kube-system" is outside the installation policy`},
		},
		{
			name:     "explicit inclusion can add a system namespace",
			snapshot: systemIncludedSnapshot,
			request: Request{
				APIGroup:  "",
				Kind:      "Pod",
				Namespace: "kube-system",
				Scope:     discovery.ScopeNamespaced,
			},
			want: Decision{Allowed: true, Reason: ReasonAllowed, Message: "request is allowed by the installation policy"},
		},
		{
			name:     "cluster scope is denied by the default flag",
			snapshot: clusterDisabledSnapshot,
			request: Request{
				APIGroup: "",
				Kind:     "Node",
				Scope:    discovery.ScopeCluster,
			},
			want: Decision{Reason: ReasonClusterScopeDenied, Message: "cluster-scoped access is disabled by the installation policy"},
		},
		{
			name: "invalid scope is rejected before resource lookup",
			request: Request{
				APIGroup:  "",
				Kind:      "Pod",
				Namespace: "team-a",
				Scope:     discovery.Scope(99),
			},
			want: Decision{Reason: ReasonInvalidRequest, Message: "request scope must be namespaced or cluster"},
		},
		{
			name: "missing kind is invalid",
			request: Request{
				APIGroup:  "",
				Namespace: "team-a",
				Scope:     discovery.ScopeNamespaced,
			},
			want: Decision{Reason: ReasonInvalidRequest, Message: "request Kind is missing or not normalized"},
		},
		{
			name: "missing namespace is invalid for namespaced requests",
			request: Request{
				APIGroup: "",
				Kind:     "Pod",
				Scope:    discovery.ScopeNamespaced,
			},
			want: Decision{Reason: ReasonInvalidRequest, Message: "request namespace is required for a namespaced resource"},
		},
		{
			name:     "terminal policy state is fail closed",
			snapshot: NewDenyAllSnapshot(ReasonPolicyMissing, "policy is missing; observation denied"),
			request: Request{
				APIGroup:  "",
				Kind:      "Pod",
				Namespace: "team-a",
				Scope:     discovery.ScopeNamespaced,
			},
			want: Decision{Reason: ReasonPolicyMissing, Message: "policy is missing; observation denied"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := test.snapshot
			if snapshot.policy == nil && snapshot.terminalReason == "" {
				snapshot = validSnapshot
			}
			if got := snapshot.Evaluate(test.request); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Evaluate() = %#v, want %#v", got, test.want)
			}
		})
	}

	t.Run("cluster scope ignores namespace policy when enabled", func(t *testing.T) {
		policy := validPolicy()
		policy.Spec.AllowClusterScoped = true
		policy.Spec.Namespaces.Exclude = []string{"team-b"}
		policy.Spec.Resources[0].Kinds = append(policy.Spec.Resources[0].Kinds, "Node")
		compiled, err := Compile(policy)
		if err != nil {
			t.Fatalf("Compile() unexpected error: %v", err)
		}
		got := NewSnapshot(compiled).Evaluate(Request{
			APIGroup:  "",
			Kind:      "Node",
			Namespace: "team-b",
			Scope:     discovery.ScopeCluster,
		})
		want := Decision{Allowed: true, Reason: ReasonAllowed, Message: "request is allowed by the installation policy"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("cluster evaluation = %#v, want %#v", got, want)
		}
	})

	t.Run("all mode and explicit mode have distinct namespace semantics", func(t *testing.T) {
		allPolicy := validPolicy()
		allPolicy.Spec.Namespaces.Mode = v1alpha1.NamespaceModeAll
		allPolicy.Spec.Namespaces.Include = nil
		allPolicy.Spec.Namespaces.Exclude = nil
		allCompiled, err := Compile(allPolicy)
		if err != nil {
			t.Fatalf("Compile(all) unexpected error: %v", err)
		}
		if !NewSnapshot(allCompiled).Evaluate(Request{APIGroup: "", Kind: "Pod", Namespace: "kube-system", Scope: discovery.ScopeNamespaced}).Allowed {
			t.Fatal("All mode unexpectedly denied a non-excluded system namespace")
		}

		explicitPolicy := validPolicy()
		explicitPolicy.Spec.Namespaces.Mode = v1alpha1.NamespaceModeExplicit
		explicitPolicy.Spec.Namespaces.Include = []string{"kube-system"}
		explicitPolicy.Spec.Namespaces.Exclude = nil
		explicitCompiled, err := Compile(explicitPolicy)
		if err != nil {
			t.Fatalf("Compile(explicit) unexpected error: %v", err)
		}
		if !NewSnapshot(explicitCompiled).Evaluate(Request{APIGroup: "", Kind: "Pod", Namespace: "kube-system", Scope: discovery.ScopeNamespaced}).Allowed {
			t.Fatal("Explicit mode did not allow its exact inclusion")
		}
		if NewSnapshot(explicitCompiled).Evaluate(Request{APIGroup: "", Kind: "Pod", Namespace: "team-a", Scope: discovery.ScopeNamespaced}).Allowed {
			t.Fatal("Explicit mode allowed a namespace outside Include")
		}
	})

	t.Run("equivalent rule order produces byte-for-byte stable decisions", func(t *testing.T) {
		first := validPolicy()
		second := validPolicy()
		second.Spec.Resources[0].APIGroups = []string{"apps", ""}
		second.Spec.Resources[0].Kinds = []string{"Service", "Pod"}
		compiledFirst, err := Compile(first)
		if err != nil {
			t.Fatalf("Compile(first) unexpected error: %v", err)
		}
		compiledSecond, err := Compile(second)
		if err != nil {
			t.Fatalf("Compile(second) unexpected error: %v", err)
		}
		requests := []Request{
			{APIGroup: "", Kind: "Pod", Namespace: "team-a", Scope: discovery.ScopeNamespaced},
			{APIGroup: "apps", Kind: "Service", Namespace: "team-a", Scope: discovery.ScopeNamespaced},
			{APIGroup: "batch", Kind: "Job", Namespace: "team-a", Scope: discovery.ScopeNamespaced},
		}
		for _, request := range requests {
			if firstDecision, secondDecision := NewSnapshot(compiledFirst).Evaluate(request), NewSnapshot(compiledSecond).Evaluate(request); firstDecision != secondDecision {
				t.Fatalf("rule order changed decision for %#v: %#v != %#v", request, firstDecision, secondDecision)
			}
		}
	})

	t.Run("decisions do not expose resource contents or RBAC state", func(t *testing.T) {
		request := Request{SourceID: "secret-value", APIGroup: "", Kind: "Pod", Namespace: "team-b", Scope: discovery.ScopeNamespaced}
		decision := validSnapshot.Evaluate(request)
		if decision.Allowed || decision.Reason != ReasonNamespaceDenied {
			t.Fatalf("unexpected logical decision: %#v", decision)
		}
		if containsSecret(decision.Message, request.SourceID) {
			t.Fatalf("decision leaked source payload: %#v", decision)
		}
	})
}

func containsSecret(message, secret string) bool {
	return secret != "" && len(message) >= len(secret) && stringContains(message, secret)
}

func stringContains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
