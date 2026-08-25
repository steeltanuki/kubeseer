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
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLoadPolicy(t *testing.T) {
	ctx := context.Background()
	request := Request{
		APIGroup:  "",
		Kind:      "Pod",
		Namespace: "team-a",
		Scope:     discovery.ScopeNamespaced,
	}

	valid := validPolicy()
	invalid := validPolicy()
	invalid.Name = "default"

	tests := []struct {
		name       string
		source     PolicySource
		wantReason PolicyReason
		wantAllow  bool
		wantText   string
	}{
		{
			name:       "valid policy produces a logical allow",
			source:     PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) { return valid.DeepCopy(), nil }),
			wantReason: ReasonAllowed,
			wantAllow:  true,
			wantText:   "request is allowed by the installation policy",
		},
		{
			name: "missing policy produces deny all",
			source: PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
				return nil, apierrors.NewNotFound(schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: "kubeseeraccesspolicies"}, v1alpha1.InstallationAccessCeilingName)
			}),
			wantReason: ReasonPolicyMissing,
			wantText:   "policy is missing; observation denied",
		},
		{
			name:       "invalid policy produces deny all",
			source:     PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) { return invalid.DeepCopy(), nil }),
			wantReason: ReasonPolicyInvalid,
			wantText:   "policy PolicyInvalid at metadata.name: must be installation-access-ceiling",
		},
		{
			name: "unavailable policy produces deny all without raw error",
			source: PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
				return nil, errors.New("raw-secret-upstream-payload")
			}),
			wantReason: ReasonPolicyUnavailable,
			wantText:   "policy is unavailable; observation denied",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Load(ctx, test.source).Evaluate(request)
			if got.Allowed != test.wantAllow || got.Reason != test.wantReason || got.Message != test.wantText {
				t.Fatalf("Load().Evaluate() = %#v, want allowed=%t reason=%q message=%q", got, test.wantAllow, test.wantReason, test.wantText)
			}
			if got.Message == "" || containsSecret(got.Message, "raw-secret-upstream-payload") {
				t.Fatalf("load diagnostic is not sanitized: %#v", got)
			}
		})
	}

	t.Run("a later failure cannot reuse a prior valid snapshot", func(t *testing.T) {
		calls := 0
		source := PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			calls++
			if calls == 1 {
				return valid.DeepCopy(), nil
			}
			return nil, apierrors.NewNotFound(schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: "kubeseeraccesspolicies"}, v1alpha1.InstallationAccessCeilingName)
		})

		first := Load(ctx, source)
		second := Load(ctx, source)
		firstDecision := first.Evaluate(request)
		secondDecision := second.Evaluate(request)
		if !firstDecision.Allowed || firstDecision.Reason != ReasonAllowed {
			t.Fatalf("first valid load was not allowed: %#v", firstDecision)
		}
		if secondDecision.Allowed || secondDecision.Reason != ReasonPolicyMissing {
			t.Fatalf("second missing load reused valid state: %#v", secondDecision)
		}
		if calls != 2 {
			t.Fatalf("source calls = %d, want 2 loads", calls)
		}
	})

	t.Run("evaluation does not call the source", func(t *testing.T) {
		calls := 0
		source := PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			calls++
			return valid.DeepCopy(), nil
		})
		snapshot := Load(ctx, source)
		before := calls
		_ = snapshot.Evaluate(request)
		_ = snapshot.Evaluate(Request{APIGroup: "batch", Kind: "Job", Namespace: "team-a", Scope: discovery.ScopeNamespaced})
		if calls != before {
			t.Fatalf("evaluation called source: before=%d after=%d", before, calls)
		}
	})

	t.Run("controller-runtime adapter reads only the active singleton", func(t *testing.T) {
		scheme := runtime.NewScheme()
		if err := v1alpha1.AddToScheme(scheme); err != nil {
			t.Fatalf("add API types to fake client scheme: %v", err)
		}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(valid.DeepCopy()).Build()
		source := NewClientPolicySource(client)
		got := Load(ctx, source).Evaluate(request)
		want := Decision{Allowed: true, Reason: ReasonAllowed, Message: "request is allowed by the installation policy"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("client source decision = %#v, want %#v", got, want)
		}

		missingClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		missing := Load(ctx, NewClientPolicySource(missingClient)).Evaluate(request)
		if missing.Allowed || missing.Reason != ReasonPolicyMissing {
			t.Fatalf("client source missing decision = %#v", missing)
		}
	})

	t.Run("nil source fails closed", func(t *testing.T) {
		decision := Load(ctx, nil).Evaluate(request)
		if decision.Allowed || decision.Reason != ReasonPolicyUnavailable {
			t.Fatalf("nil source decision = %#v", decision)
		}
	})
}
