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
	"fmt"

	"github.com/steeltanuki/kubeseer/internal/discovery"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Request is the normalized identity and scope of one resource authorization
// decision. It contains no user, ServiceAccount, or resource-instance data.
type Request struct {
	SourceID  string
	APIGroup  string
	Kind      string
	Namespace string
	Scope     discovery.Scope
}

// Decision is the stable logical policy result. A decision never represents a
// Kubernetes RBAC result; a later enforcement layer keeps that result separate.
type Decision struct {
	Allowed bool
	Reason  PolicyReason
	Message string
}

// Snapshot holds either one immutable compiled policy or one terminal deny-all
// state. Evaluation never loads or queries Kubernetes state.
type Snapshot struct {
	policy          *CompiledPolicy
	terminalReason  PolicyReason
	terminalMessage string
}

// NewSnapshot wraps a compiled policy for pure evaluation. A nil policy is
// converted into a fail-closed invalid snapshot.
func NewSnapshot(policy *CompiledPolicy) Snapshot {
	if policy == nil {
		return NewDenyAllSnapshot(ReasonPolicyInvalid, "compiled policy is unavailable")
	}
	return Snapshot{policy: policy}
}

// NewDenyAllSnapshot constructs a terminal snapshot that cannot produce an
// allow decision. Unknown terminal reasons degrade to PolicyUnavailable.
func NewDenyAllSnapshot(reason PolicyReason, message string) Snapshot {
	switch reason {
	case ReasonPolicyMissing, ReasonPolicyInvalid, ReasonPolicyUnavailable:
	default:
		reason = ReasonPolicyUnavailable
	}
	return Snapshot{terminalReason: reason, terminalMessage: message}
}

// Snapshot returns an evaluation snapshot for this compiled policy.
func (p *CompiledPolicy) Snapshot() Snapshot {
	return NewSnapshot(p)
}

// Evaluate runs the pure policy decision algorithm against one request.
func (s Snapshot) Evaluate(request Request) Decision {
	if s.terminalReason != "" {
		message := s.terminalMessage
		if message == "" {
			message = fmt.Sprintf("observation denied because policy state is %s", s.terminalReason)
		}
		return Decision{Reason: s.terminalReason, Message: message}
	}
	if s.policy == nil {
		return Decision{Reason: ReasonPolicyInvalid, Message: "observation denied because policy state is PolicyInvalid"}
	}

	if message := validateRequest(request); message != "" {
		return Decision{Reason: ReasonInvalidRequest, Message: message}
	}

	if _, allowed := s.policy.allowedResources[resourceKey{apiGroup: request.APIGroup, kind: request.Kind}]; !allowed {
		return Decision{
			Reason:  ReasonResourceDenied,
			Message: fmt.Sprintf("resource type %q/%q is outside the installation policy", request.APIGroup, request.Kind),
		}
	}

	if request.Scope == discovery.ScopeCluster {
		if !s.policy.allowClusterScoped {
			return Decision{Reason: ReasonClusterScopeDenied, Message: "cluster-scoped access is disabled by the installation policy"}
		}
		return allowedDecision()
	}

	if _, excluded := s.policy.excludedNamespaces[request.Namespace]; excluded {
		return Decision{Reason: ReasonNamespaceDenied, Message: fmt.Sprintf("namespace %q is excluded by the installation policy", request.Namespace)}
	}

	allowedNamespace := false
	switch s.policy.namespaceMode {
	case "Explicit":
		_, allowedNamespace = s.policy.includedNamespaces[request.Namespace]
	case "All":
		allowedNamespace = true
	case "AllNonSystem":
		if _, explicitlyIncluded := s.policy.includedNamespaces[request.Namespace]; explicitlyIncluded {
			allowedNamespace = true
		} else {
			_, system := s.policy.systemNamespaces[request.Namespace]
			allowedNamespace = !system
		}
	}
	if !allowedNamespace {
		return Decision{Reason: ReasonNamespaceDenied, Message: fmt.Sprintf("namespace %q is outside the installation policy", request.Namespace)}
	}

	return allowedDecision()
}

// Evaluate is the function form of Snapshot.Evaluate for callers that prefer
// an explicit evaluator boundary.
func Evaluate(snapshot Snapshot, request Request) Decision {
	return snapshot.Evaluate(request)
}

func allowedDecision() Decision {
	return Decision{Allowed: true, Reason: ReasonAllowed, Message: "request is allowed by the installation policy"}
}

func validateRequest(request Request) string {
	if !request.Scope.Valid() {
		return "request scope must be namespaced or cluster"
	}
	if request.Kind == "" || !kindPattern.MatchString(request.Kind) || len(request.Kind) > 63 {
		return "request Kind is missing or not normalized"
	}
	if request.APIGroup != "" && len(validation.IsDNS1123Subdomain(request.APIGroup)) != 0 {
		return "request API group is not normalized"
	}
	if request.Scope == discovery.ScopeNamespaced {
		if request.Namespace == "" {
			return "request namespace is required for a namespaced resource"
		}
		if len(validation.IsDNS1123Label(request.Namespace)) != 0 {
			return "request namespace is not normalized"
		}
	}
	return ""
}
