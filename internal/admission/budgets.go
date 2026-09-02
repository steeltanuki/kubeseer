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

// Package admission owns bounded, side-effect-free validation of proposed
// Kubeseer configuration. Dynamic validation is layered on these guards so an
// over-budget request cannot reach discovery or policy evaluation.
//
// Responsibility: validate bounded, structural, semantic, and dynamic
// admission inputs while returning deterministic, sanitized diagnostics.
//
// Boundary: Kubernetes discovery and policy access arrive through consumer-
// owned ports; admission never reads observed resource instances directly.
package admission

import (
	"encoding/json"
	"fmt"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/limits"
)

const (
	DefaultMaxKubeseerSources          = 32
	DefaultMaxSourceNamespaces         = 64
	DefaultMaxSourceFields             = 64
	DefaultMaxFieldOperators           = 16
	DefaultMaxOperatorValues           = 128
	DefaultMaxSourceAggregations       = 32
	DefaultMaxAggregationGroupBy       = 16
	DefaultMaxSelectorMatchExpressions = 64
	DefaultMaxSelectorExpressionValues = 64
	DefaultMaxSelectorMatchLabels      = 64
	DefaultMaxPolicyNamespaceNames     = 256
	DefaultMaxPolicyResourceRules      = 128
	DefaultMaxPolicyAPIGroups          = 64
	DefaultMaxPolicyKinds              = 64
	DefaultMaxCanonicalSpecBytes       = 262144
)

// Limits is the admission budget set. A validator retains a copy so callers
// cannot mutate the active limits through the value passed to its constructor.
type Limits struct {
	MaxKubeseerSources          int
	MaxSourceNamespaces         int
	MaxSourceFields             int
	MaxFieldOperators           int
	MaxOperatorValues           int
	MaxSourceAggregations       int
	MaxAggregationGroupBy       int
	MaxSelectorMatchExpressions int
	MaxSelectorExpressionValues int
	MaxSelectorMatchLabels      int
	MaxPolicyNamespaceNames     int
	MaxPolicyResourceRules      int
	MaxPolicyAPIGroups          int
	MaxPolicyKinds              int
	MaxKubeseerSpecBytes        int
	MaxAccessPolicySpecBytes    int
}

// DefaultLimits returns the approved positive admission budgets.
func DefaultLimits() Limits {
	return LimitsFromProfile(limits.DefaultProfile())
}

// LimitsFromProfile adapts the immutable manager profile to the existing
// admission contract without exposing the profile's private state.
func LimitsFromProfile(profile limits.Profile) Limits {
	values := profile.Admission()
	return Limits{
		MaxKubeseerSources:          values.MaxKubeseerSources,
		MaxSourceNamespaces:         values.MaxSourceNamespaces,
		MaxSourceFields:             values.MaxSourceFields,
		MaxFieldOperators:           values.MaxFieldOperators,
		MaxOperatorValues:           values.MaxOperatorValues,
		MaxSourceAggregations:       values.MaxSourceAggregations,
		MaxAggregationGroupBy:       values.MaxAggregationGroupBy,
		MaxSelectorMatchExpressions: values.MaxSelectorMatchExpressions,
		MaxSelectorExpressionValues: values.MaxSelectorExpressionValues,
		MaxSelectorMatchLabels:      values.MaxSelectorMatchLabels,
		MaxPolicyNamespaceNames:     values.MaxPolicyNamespaceNames,
		MaxPolicyResourceRules:      values.MaxPolicyResourceRules,
		MaxPolicyAPIGroups:          values.MaxPolicyAPIGroups,
		MaxPolicyKinds:              values.MaxPolicyKinds,
		MaxKubeseerSpecBytes:        values.MaxKubeseerSpecBytes,
		MaxAccessPolicySpecBytes:    values.MaxAccessPolicySpecBytes,
	}
}

// BudgetIssue is the sanitized, coordinate-preserving result of a budget
// check. The admission issue contract wraps this shape in the next task.
type BudgetIssue struct {
	Path    string
	Reason  string
	Message string
}

// BudgetValidator is the reusable pure budget boundary shared by admission
// and reconciliation. It owns a defensive value copy and never performs
// discovery, policy, or resource I/O.
type BudgetValidator struct {
	limits Limits
}

// NewBudgetValidator constructs a reusable validator from one budget view.
// Non-positive fields retain the historical direct-constructor defaults; the
// manager composition root rejects such values before constructing it.
func NewBudgetValidator(limits Limits) *BudgetValidator {
	return &BudgetValidator{limits: normalizeLimits(limits)}
}

// NewBudgetValidatorFromProfile adapts a resolved manager profile to the
// common admission/runtime budget boundary.
func NewBudgetValidatorFromProfile(profile limits.Profile) *BudgetValidator {
	return NewBudgetValidator(LimitsFromProfile(profile))
}

// Limits returns a defensive copy of the validator's budget view.
func (v *BudgetValidator) Limits() Limits {
	if v == nil {
		return DefaultLimits()
	}
	return v.limits
}

// ValidateKubeseer applies the common Kubeseer budget boundary.
func (v *BudgetValidator) ValidateKubeseer(object *v1alpha1.Kubeseer) []BudgetIssue {
	if v == nil {
		return ValidateKubeseerBudgets(object)
	}
	return ValidateKubeseerBudgetsWithLimits(object, v.limits)
}

// ValidateAccessPolicy applies the common installation-policy budget
// boundary before policy compilation.
func (v *BudgetValidator) ValidateAccessPolicy(object *v1alpha1.KubeseerAccessPolicy) []BudgetIssue {
	if v == nil {
		return ValidateAccessPolicyBudgets(object)
	}
	return ValidateAccessPolicyBudgetsWithLimits(object, v.limits)
}

// ValidateKubeseerBudgets applies every Kubeseer cardinality and serialized
// Spec limit without contacting Kubernetes or another dependency.
func ValidateKubeseerBudgets(object *v1alpha1.Kubeseer) []BudgetIssue {
	return ValidateKubeseerBudgetsWithLimits(object, DefaultLimits())
}

// ValidateKubeseerBudgetsWithLimits applies an explicit budget set. Non-
// positive values are replaced with the approved defaults to prevent a
// caller from accidentally disabling a guard.
func ValidateKubeseerBudgetsWithLimits(object *v1alpha1.Kubeseer, limits Limits) []BudgetIssue {
	if object == nil {
		return nil
	}
	limits = normalizeLimits(limits)
	issues := make([]BudgetIssue, 0)
	issues = appendCardinalityIssue(issues, "spec.sources", len(object.Spec.Sources), limits.MaxKubeseerSources)
	for sourceIndex, source := range object.Spec.Sources {
		prefix := fmt.Sprintf("spec.sources[%d]", sourceIndex)
		if source.Namespaces != nil {
			issues = appendCardinalityIssue(issues, prefix+".namespaces.names", len(source.Namespaces.Names), limits.MaxSourceNamespaces)
		}
		issues = appendCardinalityIssue(issues, prefix+".fields", len(source.Fields), limits.MaxSourceFields)
		for fieldIndex, field := range source.Fields {
			fieldPrefix := fmt.Sprintf("%s.fields[%d]", prefix, fieldIndex)
			issues = appendCardinalityIssue(issues, fieldPrefix+".operators", len(field.Operators), limits.MaxFieldOperators)
			for operatorIndex, operator := range field.Operators {
				issues = appendCardinalityIssue(issues, fmt.Sprintf("%s.operators[%d].values", fieldPrefix, operatorIndex), len(operator.Values), limits.MaxOperatorValues)
			}
		}
		issues = appendCardinalityIssue(issues, prefix+".aggregations", len(source.Aggregations), limits.MaxSourceAggregations)
		for aggregationIndex, aggregation := range source.Aggregations {
			issues = appendCardinalityIssue(issues, fmt.Sprintf("%s.aggregations[%d].groupBy", prefix, aggregationIndex), len(aggregation.GroupBy), limits.MaxAggregationGroupBy)
		}
		if source.Selector != nil {
			selectorPrefix := prefix + ".selector"
			issues = appendCardinalityIssue(issues, selectorPrefix+".matchExpressions", len(source.Selector.MatchExpressions), limits.MaxSelectorMatchExpressions)
			issues = appendCardinalityIssue(issues, selectorPrefix+".matchLabels", len(source.Selector.MatchLabels), limits.MaxSelectorMatchLabels)
			for expressionIndex, expression := range source.Selector.MatchExpressions {
				issues = appendCardinalityIssue(issues, fmt.Sprintf("%s.matchExpressions[%d].values", selectorPrefix, expressionIndex), len(expression.Values), limits.MaxSelectorExpressionValues)
			}
		}
	}
	return appendSpecSizeIssue(issues, "spec", object.Spec, limits.MaxKubeseerSpecBytes)
}

// ValidateAccessPolicyBudgets applies every policy cardinality and serialized
// Spec limit without compiling the policy or contacting Kubernetes.
func ValidateAccessPolicyBudgets(object *v1alpha1.KubeseerAccessPolicy) []BudgetIssue {
	return ValidateAccessPolicyBudgetsWithLimits(object, DefaultLimits())
}

// ValidateAccessPolicyBudgetsWithLimits applies an explicit budget set while
// retaining the submitted rule and member indexes in every list issue.
func ValidateAccessPolicyBudgetsWithLimits(object *v1alpha1.KubeseerAccessPolicy, limits Limits) []BudgetIssue {
	if object == nil {
		return nil
	}
	limits = normalizeLimits(limits)
	issues := make([]BudgetIssue, 0)
	issues = appendCardinalityIssue(issues, "spec.namespaces.include", len(object.Spec.Namespaces.Include), limits.MaxPolicyNamespaceNames)
	issues = appendCardinalityIssue(issues, "spec.namespaces.exclude", len(object.Spec.Namespaces.Exclude), limits.MaxPolicyNamespaceNames)
	issues = appendCardinalityIssue(issues, "spec.namespaces.systemNamespaces", len(object.Spec.Namespaces.SystemNamespaces), limits.MaxPolicyNamespaceNames)
	issues = appendCardinalityIssue(issues, "spec.resources", len(object.Spec.Resources), limits.MaxPolicyResourceRules)
	for ruleIndex, rule := range object.Spec.Resources {
		prefix := fmt.Sprintf("spec.resources[%d]", ruleIndex)
		issues = appendCardinalityIssue(issues, prefix+".apiGroups", len(rule.APIGroups), limits.MaxPolicyAPIGroups)
		issues = appendCardinalityIssue(issues, prefix+".kinds", len(rule.Kinds), limits.MaxPolicyKinds)
	}
	return appendSpecSizeIssue(issues, "spec", object.Spec, limits.MaxAccessPolicySpecBytes)
}

func normalizeLimits(limits Limits) Limits {
	defaults := DefaultLimits()
	if limits.MaxKubeseerSources <= 0 {
		limits.MaxKubeseerSources = defaults.MaxKubeseerSources
	}
	if limits.MaxSourceNamespaces <= 0 {
		limits.MaxSourceNamespaces = defaults.MaxSourceNamespaces
	}
	if limits.MaxSourceFields <= 0 {
		limits.MaxSourceFields = defaults.MaxSourceFields
	}
	if limits.MaxFieldOperators <= 0 {
		limits.MaxFieldOperators = defaults.MaxFieldOperators
	}
	if limits.MaxOperatorValues <= 0 {
		limits.MaxOperatorValues = defaults.MaxOperatorValues
	}
	if limits.MaxSourceAggregations <= 0 {
		limits.MaxSourceAggregations = defaults.MaxSourceAggregations
	}
	if limits.MaxAggregationGroupBy <= 0 {
		limits.MaxAggregationGroupBy = defaults.MaxAggregationGroupBy
	}
	if limits.MaxSelectorMatchExpressions <= 0 {
		limits.MaxSelectorMatchExpressions = defaults.MaxSelectorMatchExpressions
	}
	if limits.MaxSelectorExpressionValues <= 0 {
		limits.MaxSelectorExpressionValues = defaults.MaxSelectorExpressionValues
	}
	if limits.MaxSelectorMatchLabels <= 0 {
		limits.MaxSelectorMatchLabels = defaults.MaxSelectorMatchLabels
	}
	if limits.MaxPolicyNamespaceNames <= 0 {
		limits.MaxPolicyNamespaceNames = defaults.MaxPolicyNamespaceNames
	}
	if limits.MaxPolicyResourceRules <= 0 {
		limits.MaxPolicyResourceRules = defaults.MaxPolicyResourceRules
	}
	if limits.MaxPolicyAPIGroups <= 0 {
		limits.MaxPolicyAPIGroups = defaults.MaxPolicyAPIGroups
	}
	if limits.MaxPolicyKinds <= 0 {
		limits.MaxPolicyKinds = defaults.MaxPolicyKinds
	}
	if limits.MaxKubeseerSpecBytes <= 0 {
		limits.MaxKubeseerSpecBytes = defaults.MaxKubeseerSpecBytes
	}
	if limits.MaxAccessPolicySpecBytes <= 0 {
		limits.MaxAccessPolicySpecBytes = defaults.MaxAccessPolicySpecBytes
	}
	return limits
}

func appendCardinalityIssue(issues []BudgetIssue, path string, size, limit int) []BudgetIssue {
	if size <= limit {
		return issues
	}
	return append(issues, BudgetIssue{
		Path:    fmt.Sprintf("%s[%d]", path, limit),
		Reason:  "ConfigurationBudgetExceeded",
		Message: fmt.Sprintf("must contain at most %d entries", limit),
	})
}

func appendSpecSizeIssue(issues []BudgetIssue, path string, spec any, limit int) []BudgetIssue {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return append(issues, BudgetIssue{Path: path, Reason: "ConfigurationBudgetExceeded", Message: "cannot be encoded within the configuration budget"})
	}
	if len(encoded) <= limit {
		return issues
	}
	return append(issues, BudgetIssue{
		Path:    path,
		Reason:  "ConfigurationBudgetExceeded",
		Message: fmt.Sprintf("canonical JSON encoding must be at most %d bytes", limit),
	})
}
