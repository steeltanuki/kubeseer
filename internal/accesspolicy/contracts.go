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
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

// PolicyIdentity identifies the policy object that produced an evaluation
// snapshot. It is diagnostic context only and never participates in policy
// matching.
type PolicyIdentity struct {
	Name       string
	UID        types.UID
	Generation int64
}

// PolicyReason is the stable reason category used by policy validation and
// later policy loading/evaluation boundaries.
type PolicyReason string

const (
	ReasonAllowed            PolicyReason = "Allowed"
	ReasonPolicyMissing      PolicyReason = "PolicyMissing"
	ReasonPolicyInvalid      PolicyReason = "PolicyInvalid"
	ReasonPolicyUnavailable  PolicyReason = "PolicyUnavailable"
	ReasonInvalidRequest     PolicyReason = "InvalidRequest"
	ReasonResourceDenied     PolicyReason = "ResourceDenied"
	ReasonClusterScopeDenied PolicyReason = "ClusterScopeDenied"
	ReasonNamespaceDenied    PolicyReason = "NamespaceDenied"
)

// PolicyError is safe to expose to status-producing callers. Field identifies
// the stable policy path that failed, while Message remains free of raw
// upstream payloads.
type PolicyError struct {
	Reason  PolicyReason
	Field   string
	Message string
}

// Error implements error with a deterministic reason, field, and message.
func (e *PolicyError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field == "" {
		return fmt.Sprintf("policy: %s: %s", e.Reason, e.Message)
	}
	return fmt.Sprintf("policy %s at %s: %s", e.Reason, e.Field, e.Message)
}

// HasReason reports whether err is a PolicyError with the requested reason.
func HasReason(err error, reason PolicyReason) bool {
	var policyErr *PolicyError
	return errors.As(err, &policyErr) && policyErr.Reason == reason
}
