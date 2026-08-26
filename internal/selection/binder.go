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

package selection

import "github.com/steeltanuki/kubeseer/internal/accesspolicy"

// RequestForTarget converts one exact target into the policy request consumed
// by the existing installation-policy evaluator.
func RequestForTarget(target ReadTarget) accesspolicy.Request {
	return accesspolicy.Request{
		SourceID:  target.SourceID,
		APIGroup:  target.GVR.Group,
		Kind:      target.Kind,
		Namespace: target.Namespace,
		Scope:     target.Scope,
	}
}

// Bind validates complete, exact authorization coverage for a plan. No
// executable capability is returned unless every target has exactly one
// allowed matching decision.
func Bind(plan SelectionPlan, authorizations []Authorization) (AuthorizedPlan, error) {
	targets := plan.Targets()
	targetKeys := make(map[accesspolicy.Request]struct{}, len(targets))
	for _, target := range targets {
		targetKeys[RequestForTarget(target)] = struct{}{}
	}
	authorizationIndexes := make(map[accesspolicy.Request][]int, len(authorizations))
	for index, authorization := range authorizations {
		if _, exists := targetKeys[authorization.Request]; !exists {
			return AuthorizedPlan{}, NewSelectionError(plan.SourceID(), ReasonAuthorizationMismatch, "authorization contains a target that does not match the selection plan")
		}
		authorizationIndexes[authorization.Request] = append(authorizationIndexes[authorization.Request], index)
	}

	for _, target := range targets {
		want := RequestForTarget(target)
		indexes := authorizationIndexes[want]
		if len(indexes) == 0 {
			return AuthorizedPlan{}, NewSelectionError(plan.SourceID(), ReasonAuthorizationMissing, "authorization is missing for an exact read target")
		}
		if len(indexes) > 1 {
			return AuthorizedPlan{}, NewSelectionError(plan.SourceID(), ReasonAuthorizationMismatch, "multiple authorization decisions match one exact read target")
		}
		decision := authorizations[indexes[0]].Decision
		if !decision.Allowed || decision.Reason != accesspolicy.ReasonAllowed {
			return AuthorizedPlan{}, NewSelectionError(plan.SourceID(), ReasonAuthorizationDenied, "exact read target is not allowed by the installation policy")
		}
	}

	return AuthorizedPlan{plan: clonePlan(plan)}, nil
}
