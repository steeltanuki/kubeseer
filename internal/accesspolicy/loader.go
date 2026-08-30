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

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PolicySource is the only loading boundary needed by this package. It never
// accepts a caller-selected name: implementations retrieve the active
// installation singleton only.
type PolicySource interface {
	Get(ctx context.Context) (*v1alpha1.KubeseerAccessPolicy, error)
}

// PolicySourceFunc adapts a function into a narrow PolicySource for tests and
// other in-process adapters.
type PolicySourceFunc func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error)

func (f PolicySourceFunc) Get(ctx context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
	return f(ctx)
}

// ClientPolicySource adapts a controller-runtime client to the active policy
// source. Its Get operation always addresses the cluster-scoped singleton.
type ClientPolicySource struct {
	client client.Reader
}

// NewClientPolicySource constructs the controller-runtime policy adapter.
func NewClientPolicySource(c client.Reader) *ClientPolicySource {
	return &ClientPolicySource{client: c}
}

// Get retrieves only KubeseerAccessPolicy/installation-access-ceiling.
func (s *ClientPolicySource) Get(ctx context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
	if s == nil || s.client == nil {
		return nil, context.Canceled
	}

	policy := new(v1alpha1.KubeseerAccessPolicy)
	err := s.client.Get(ctx, client.ObjectKey{Name: v1alpha1.InstallationAccessCeilingName}, policy)
	if err != nil {
		return nil, err
	}
	return policy, nil
}

// Load reads and compiles one fresh policy snapshot. Every outcome is
// represented by a new valid or terminal deny-all snapshot; no previous
// snapshot is accepted as an input or reused after failure.
func Load(ctx context.Context, source PolicySource) Snapshot {
	if source == nil {
		return NewDenyAllSnapshot(ReasonPolicyUnavailable, "policy is unavailable; observation denied")
	}

	policy, err := source.Get(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return NewDenyAllSnapshot(ReasonPolicyMissing, "policy is missing; observation denied")
		}
		return NewDenyAllSnapshot(ReasonPolicyUnavailable, "policy is unavailable; observation denied")
	}
	if policy == nil {
		return NewDenyAllSnapshot(ReasonPolicyInvalid, "policy is invalid; observation denied")
	}

	identity := PolicyIdentity{
		Name:       policy.Name,
		UID:        policy.UID,
		Generation: policy.Generation,
	}
	compiled, err := Compile(policy)
	if err != nil {
		message := "policy is invalid; observation denied"
		var policyErr *PolicyError
		if errors.As(err, &policyErr) {
			message = policyErr.Error()
		}
		return NewDenyAllSnapshotWithIdentity(ReasonPolicyInvalid, message, identity)
	}
	return NewSnapshotWithIdentity(compiled, identity)
}

// LoadPolicy is an explicit name for callers that prefer the feature's domain
// terminology over the shorter Load function.
func LoadPolicy(ctx context.Context, source PolicySource) Snapshot {
	return Load(ctx, source)
}
