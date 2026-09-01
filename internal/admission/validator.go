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

package admission

import (
	"context"
	"errors"
	"fmt"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/selection"
)

// Validator composes bounded pure validation, current discovery, and one
// fresh canonical installation-policy snapshot. It retains no result, plan,
// snapshot, or authorization capability after a call returns.
type Validator struct {
	resolver     selection.DiscoveryResolver
	policySource accesspolicy.PolicySource
	limits       Limits
	budgets      *BudgetValidator
}

// NewValidator constructs a validator with the approved default budgets.
func NewValidator(resolver selection.DiscoveryResolver, policySource accesspolicy.PolicySource) *Validator {
	return NewValidatorWithLimits(resolver, policySource, DefaultLimits())
}

// NewValidatorWithLimits constructs a validator with a defensive copy of the
// supplied budgets. Non-positive fields continue to use approved defaults.
func NewValidatorWithLimits(resolver selection.DiscoveryResolver, policySource accesspolicy.PolicySource, limits Limits) *Validator {
	budgetValidator := NewBudgetValidator(limits)
	return &Validator{resolver: resolver, policySource: policySource, limits: budgetValidator.Limits(), budgets: budgetValidator}
}

// NewValidatorWithProfile constructs admission using the same immutable
// manager profile that reconciliation will use.
func NewValidatorWithProfile(resolver selection.DiscoveryResolver, policySource accesspolicy.PolicySource, profile limits.Profile) *Validator {
	return NewValidatorWithLimits(resolver, policySource, LimitsFromProfile(profile))
}

// NewValidatorWithBudgetValidator composes a pre-resolved common budget
// boundary without exposing mutable validator state.
func NewValidatorWithBudgetValidator(resolver selection.DiscoveryResolver, policySource accesspolicy.PolicySource, budgetValidator *BudgetValidator) *Validator {
	if budgetValidator == nil {
		budgetValidator = NewBudgetValidator(DefaultLimits())
	}
	return &Validator{resolver: resolver, policySource: policySource, limits: budgetValidator.Limits(), budgets: budgetValidator}
}

// ValidateKubeseer validates one proposed Kubeseer through the staged
// admission pipeline. Pure budget or semantic issues prevent all dynamic
// discovery and policy work.
func (v *Validator) ValidateKubeseer(ctx context.Context, object *v1alpha1.Kubeseer) Result {
	if ctx == nil {
		ctx = context.Background()
	}
	issues := budgetIssues(v.budgetValidator().ValidateKubeseer(object))
	if len(issues) != 0 {
		return result(issues)
	}
	semantic := ValidateKubeseerSemantics(object)
	if len(semantic.Issues) != 0 {
		return semantic
	}
	if object == nil {
		return semantic
	}
	if v == nil || v.resolver == nil {
		return result([]Issue{{
			Path:    "spec",
			Reason:  "ValidationUnavailable",
			Message: "current Kubernetes discovery is unavailable",
			Class:   Unavailable,
		}})
	}

	planner := selection.NewPlanner(v.resolver)
	plans := make([]selection.SelectionPlan, len(object.Spec.Sources))
	for sourceIndex, source := range object.Spec.Sources {
		plan, err := planner.Plan(ctx, object.Namespace, source)
		if err != nil {
			issues = append(issues, selectionIssue(sourceIndex, err))
			continue
		}
		plans[sourceIndex] = plan
	}
	if len(issues) != 0 {
		return result(issues)
	}

	snapshot := accesspolicy.Load(ctx, v.policySource)
	if snapshot.IsTerminal() {
		return result([]Issue{policyTerminalIssue(snapshot.TerminalReason())})
	}
	for sourceIndex, plan := range plans {
		for _, target := range plan.Targets() {
			decision := snapshot.Evaluate(accesspolicy.Request{
				SourceID:  target.SourceID,
				APIGroup:  target.GVR.Group,
				Kind:      target.Kind,
				Namespace: target.Namespace,
				Scope:     target.Scope,
			})
			if decision.Allowed {
				continue
			}
			issues = append(issues, policyDecisionIssue(sourceIndex, object.Spec.Sources[sourceIndex], target, decision))
		}
	}
	return result(issues)
}

// ValidateAccessPolicy validates a proposed policy without reading the
// currently persisted policy or any Kubeseer object.
func (v *Validator) ValidateAccessPolicy(_ context.Context, object *v1alpha1.KubeseerAccessPolicy) Result {
	issues := budgetIssues(v.budgetValidator().ValidateAccessPolicy(object))
	if len(issues) != 0 {
		return result(issues)
	}
	return ValidateAccessPolicySemantics(object)
}

// ValidatePolicy is an explicit policy-named compatibility alias.
func (v *Validator) ValidatePolicy(ctx context.Context, object *v1alpha1.KubeseerAccessPolicy) Result {
	return v.ValidateAccessPolicy(ctx, object)
}

func (v *Validator) validationLimits() Limits {
	if v == nil {
		return DefaultLimits()
	}
	return normalizeLimits(v.limits)
}

func (v *Validator) budgetValidator() *BudgetValidator {
	if v == nil || v.budgets == nil {
		return NewBudgetValidator(DefaultLimits())
	}
	return v.budgets
}

func budgetIssues(input []BudgetIssue) []Issue {
	issues := make([]Issue, 0, len(input))
	for _, issue := range input {
		issues = append(issues, invalidIssue(issue.Path, issue.Reason, issue.Message))
	}
	return issues
}

func selectionIssue(sourceIndex int, err error) Issue {
	path := fmt.Sprintf("spec.sources[%d]", sourceIndex)
	reason := "InvalidSelection"
	message := "source selection is invalid"
	class := Invalid

	var resolutionErr *discovery.ResolutionError
	if errors.As(err, &resolutionErr) {
		switch resolutionErr.Reason {
		case discovery.ReasonUnknownType:
			reason = "UnknownResource"
			message = "resource apiVersion and Kind are not served by current discovery"
		case discovery.ReasonDiscoveryUnavailable:
			reason = "ValidationUnavailable"
			message = "current Kubernetes discovery is unavailable"
			class = Unavailable
		case discovery.ReasonAmbiguousResource:
			reason = "AmbiguousResource"
			message = "resource identity resolves to more than one served resource"
		}
	}
	var selectionErr *selection.SelectionError
	if errors.As(err, &selectionErr) {
		switch selectionErr.Reason {
		case selection.ReasonInvalidNamespaceScope:
			reason = "InvalidNamespaceScope"
			message = "source namespace scope is incompatible with the discovered resource"
		case selection.ReasonInvalidSelector:
			reason = "InvalidSelector"
			message = "source selector is invalid"
		case selection.ReasonInvalidSource:
			reason = "InvalidSource"
			message = "source declaration is invalid"
		}
	}
	return Issue{Path: path, Reason: reason, Message: message, Class: class}
}

func policyTerminalIssue(reason accesspolicy.PolicyReason) Issue {
	switch reason {
	case accesspolicy.ReasonPolicyMissing:
		return Issue{Path: "spec", Reason: "PolicyMissing", Message: "installation access policy is missing", Class: Forbidden}
	case accesspolicy.ReasonPolicyInvalid:
		return Issue{Path: "spec", Reason: "PolicyInvalid", Message: "installation access policy is invalid", Class: Forbidden}
	default:
		return Issue{Path: "spec", Reason: "ValidationUnavailable", Message: "installation access policy is unavailable", Class: Unavailable}
	}
}

func policyDecisionIssue(sourceIndex int, source v1alpha1.KubeseerSource, target selection.ReadTarget, decision accesspolicy.Decision) Issue {
	path := fmt.Sprintf("spec.sources[%d].resource", sourceIndex)
	if decision.Reason == accesspolicy.ReasonNamespaceDenied && source.Namespaces != nil {
		for namespaceIndex, namespace := range source.Namespaces.Names {
			if namespace == target.Namespace {
				path = fmt.Sprintf("spec.sources[%d].namespaces.names[%d]", sourceIndex, namespaceIndex)
				break
			}
		}
	}
	reason := "PolicyDenied"
	message := "requested resource target is outside the installation access policy"
	if decision.Reason == accesspolicy.ReasonClusterScopeDenied {
		reason = "ClusterScopeDenied"
		message = "cluster-scoped target is disabled by the installation access policy"
	} else if decision.Reason == accesspolicy.ReasonNamespaceDenied {
		reason = "NamespaceDenied"
		message = "requested namespace is outside the installation access policy"
	}
	return Issue{Path: path, Reason: reason, Message: message, Class: Forbidden}
}
