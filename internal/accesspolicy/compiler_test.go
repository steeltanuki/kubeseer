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
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompilePolicy(t *testing.T) {
	tests := []struct {
		name          string
		policy        *v1alpha1.KubeseerAccessPolicy
		wantField     string
		wantSubstring string
		check         func(t *testing.T, compiled *CompiledPolicy)
	}{
		{
			name:   "valid policy uses restrictive defaults",
			policy: validPolicy(),
			check: func(t *testing.T, compiled *CompiledPolicy) {
				if compiled.namespaceMode != v1alpha1.NamespaceModeAllNonSystem {
					t.Fatalf("unexpected namespace mode: %q", compiled.namespaceMode)
				}
				if compiled.allowClusterScoped {
					t.Fatal("cluster-scoped access is enabled by the zero value")
				}
				for _, namespace := range []string{"kube-system", "kube-public", "kube-node-lease"} {
					if _, ok := compiled.systemNamespaces[namespace]; !ok {
						t.Fatalf("default system namespace %q is missing: %#v", namespace, compiled.systemNamespaces)
					}
				}
				for _, key := range []resourceKey{{apiGroup: "", kind: "Pod"}, {apiGroup: "", kind: "Service"}, {apiGroup: "apps", kind: "Pod"}, {apiGroup: "apps", kind: "Service"}} {
					if _, ok := compiled.allowedResources[key]; !ok {
						t.Fatalf("compiled resource cross-product is missing %#v: %#v", key, compiled.allowedResources)
					}
				}
			},
		},
		{
			name: "explicit empty system namespace override remains empty",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Spec.Namespaces.SystemNamespaces = []string{}
				return policy
			}(),
			check: func(t *testing.T, compiled *CompiledPolicy) {
				if len(compiled.systemNamespaces) != 0 {
					t.Fatalf("explicit empty system namespace override became non-empty: %#v", compiled.systemNamespaces)
				}
			},
		},
		{
			name: "empty namespace and resource boundaries are valid",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				return &v1alpha1.KubeseerAccessPolicy{
					ObjectMeta: objectMeta(),
					Spec: v1alpha1.KubeseerAccessPolicySpec{
						Namespaces: v1alpha1.NamespacePolicy{Mode: v1alpha1.NamespaceModeExplicit},
					},
				}
			}(),
			check: func(t *testing.T, compiled *CompiledPolicy) {
				if len(compiled.includedNamespaces) != 0 || len(compiled.allowedResources) != 0 {
					t.Fatalf("empty deny-all boundaries were not preserved: include=%#v resources=%#v", compiled.includedNamespaces, compiled.allowedResources)
				}
			},
		},
		{
			name: "invalid active name",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Name = "default"
				return policy
			}(),
			wantField:     "metadata.name",
			wantSubstring: "installation-access-ceiling",
		},
		{
			name: "invalid namespace mode",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Spec.Namespaces.Mode = "Unknown"
				return policy
			}(),
			wantField:     "spec.namespaces.mode",
			wantSubstring: "Explicit, All, or AllNonSystem",
		},
		{
			name: "duplicate namespace entries",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Spec.Namespaces.Include = []string{"team-a", "team-a"}
				return policy
			}(),
			wantField:     "spec.namespaces.include[1]",
			wantSubstring: "duplicates entry",
		},
		{
			name: "invalid namespace entry",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Spec.Namespaces.Include = []string{"Team_A"}
				return policy
			}(),
			wantField:     "spec.namespaces.include[0]",
			wantSubstring: "valid DNS-1123 namespace name",
		},
		{
			name: "empty resource rule members",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Spec.Resources = []v1alpha1.ResourceRule{{}}
				return policy
			}(),
			wantField:     "spec.resources[0].apiGroups",
			wantSubstring: "at least one API group",
		},
		{
			name: "duplicate resource set entries",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Spec.Resources = []v1alpha1.ResourceRule{{
					APIGroups: []string{"apps", "apps"},
					Kinds:     []string{"Deployment"},
				}}
				return policy
			}(),
			wantField:     "spec.resources[0].apiGroups[1]",
			wantSubstring: "duplicates entry",
		},
		{
			name: "invalid api group and wildcard kind",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Spec.Resources = []v1alpha1.ResourceRule{{
					APIGroups: []string{"Apps"},
					Kinds:     []string{"*"},
				}}
				return policy
			}(),
			wantField:     "spec.resources[0].apiGroups[0]",
			wantSubstring: "valid DNS-1123 subdomain",
		},
		{
			name: "invalid kind syntax",
			policy: func() *v1alpha1.KubeseerAccessPolicy {
				policy := validPolicy()
				policy.Spec.Resources = []v1alpha1.ResourceRule{{
					APIGroups: []string{""},
					Kinds:     []string{"deployment"},
				}}
				return policy
			}(),
			wantField:     "spec.resources[0].kinds[0]",
			wantSubstring: "CamelCase Kubernetes Kind",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := Compile(test.policy)
			if test.wantField == "" {
				if err != nil {
					t.Fatalf("Compile() unexpected error: %v", err)
				}
				test.check(t, compiled)
				return
			}

			if compiled != nil {
				t.Fatalf("Compile() returned compiled policy for invalid input: %#v", compiled)
			}
			policyErr, ok := err.(*PolicyError)
			if !ok {
				t.Fatalf("Compile() error type = %T, want *PolicyError: %v", err, err)
			}
			if policyErr.Reason != ReasonPolicyInvalid || policyErr.Field != test.wantField {
				t.Fatalf("Compile() diagnostic = %#v, want reason=%q field=%q", policyErr, ReasonPolicyInvalid, test.wantField)
			}
			if !containsSubstring(policyErr.Message, test.wantSubstring) {
				t.Fatalf("Compile() message = %q, want substring %q", policyErr.Message, test.wantSubstring)
			}
		})
	}

	t.Run("diagnostic ordering is stable", func(t *testing.T) {
		policy := validPolicy()
		policy.Name = "default"
		policy.Spec.Namespaces.Mode = "invalid"
		policy.Spec.Resources = []v1alpha1.ResourceRule{{}}

		_, firstErr := Compile(policy)
		_, secondErr := Compile(policy.DeepCopy())
		if firstErr.Error() != secondErr.Error() {
			t.Fatalf("diagnostic changed across equivalent inputs: %q != %q", firstErr, secondErr)
		}
		if !HasReason(firstErr, ReasonPolicyInvalid) {
			t.Fatalf("diagnostic reason was not stable: %v", firstErr)
		}
	})

	t.Run("compiled policy does not alias source slices", func(t *testing.T) {
		policy := validPolicy()
		policy.Spec.Namespaces.SystemNamespaces = []string{"kube-system"}
		compiled, err := Compile(policy)
		if err != nil {
			t.Fatalf("Compile() unexpected error: %v", err)
		}
		policy.Spec.Namespaces.Include[0] = "changed"
		policy.Spec.Namespaces.SystemNamespaces[0] = "changed"
		policy.Spec.Resources[0].APIGroups[0] = "changed"
		if _, ok := compiled.includedNamespaces["team-a"]; !ok {
			t.Fatalf("compiled include set aliases source: %#v", compiled.includedNamespaces)
		}
		if _, ok := compiled.systemNamespaces["kube-system"]; !ok {
			t.Fatalf("compiled system namespace set aliases source: %#v", compiled.systemNamespaces)
		}
		if _, ok := compiled.allowedResources[resourceKey{apiGroup: "", kind: "Pod"}]; !ok {
			t.Fatalf("compiled resource set aliases source: %#v", compiled.allowedResources)
		}
	})
}

func validPolicy() *v1alpha1.KubeseerAccessPolicy {
	return &v1alpha1.KubeseerAccessPolicy{
		ObjectMeta: objectMeta(),
		Spec: v1alpha1.KubeseerAccessPolicySpec{
			Namespaces: v1alpha1.NamespacePolicy{
				Mode:             v1alpha1.NamespaceModeAllNonSystem,
				Include:          []string{"team-a"},
				Exclude:          []string{"team-b"},
				SystemNamespaces: nil,
			},
			Resources: []v1alpha1.ResourceRule{{
				APIGroups: []string{"", "apps"},
				Kinds:     []string{"Pod", "Service"},
			}},
		},
	}
}

func objectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: v1alpha1.InstallationAccessCeilingName}
}

func containsSubstring(value, substring string) bool {
	return strings.Contains(value, substring)
}
