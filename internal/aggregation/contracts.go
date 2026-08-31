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

// Package aggregation reduces accepted typed operator outcomes without
// performing Kubernetes I/O. It is deliberately downstream of selection,
// extraction, typed conversion, and value-operator evaluation.
package aggregation

import (
	"errors"
	"fmt"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

// Limits bounds every output-amplifying dimension of one aggregate.
type Limits struct {
	MaxGroups            int
	MaxContributions     int
	MaxCollectedValues   int
	MaxDistinctValues    int
	MaxProvenanceEntries int
}


// DefaultLimits returns the positive implementation-owned ceilings used when
// a caller does not inject a test or deployment-specific limit.
func DefaultLimits() Limits {
	return Limits{
		MaxGroups:            1000,
		MaxContributions:     10000,
		MaxCollectedValues:   10000,
		MaxDistinctValues:    10000,
		MaxProvenanceEntries: 10000,
	}
}

func normalizeLimits(input Limits) Limits {
	defaults := DefaultLimits()
	if input.MaxGroups <= 0 {
		input.MaxGroups = defaults.MaxGroups
	}
	if input.MaxContributions <= 0 {
		input.MaxContributions = defaults.MaxContributions
	}
	if input.MaxCollectedValues <= 0 {
		input.MaxCollectedValues = defaults.MaxCollectedValues
	}
	if input.MaxDistinctValues <= 0 {
		input.MaxDistinctValues = defaults.MaxDistinctValues
	}
	if input.MaxProvenanceEntries <= 0 {
		input.MaxProvenanceEntries = defaults.MaxProvenanceEntries
	}
	return input
}

// AggregateError is a sanitized planning or evaluation failure. Its wrapped
// cause is intentionally private and never appears in the diagnostic text.
type AggregateError struct {
	SourceID      string
	AggregateName string
	Function      v1alpha1.KubeseerAggregationFunction
	FieldName     string
	Provenance    *selection.Provenance
	Reason        Reason
	Message       string

	cause error
}

// Reason is the stable category of an aggregation failure.
type Reason string

const (
	ReasonDuplicateAggregate     Reason = "duplicate-aggregate"
	ReasonUnknownField           Reason = "unknown-field"
	ReasonDuplicateGroupField    Reason = "duplicate-group-field"
	ReasonUnsupportedGroupType   Reason = "unsupported-group-type"
	ReasonIncompatibleFunction   Reason = "incompatible-function"
	ReasonInvalidAverageOptions  Reason = "invalid-average-options"
	ReasonInvalidInput           Reason = "invalid-input"
	ReasonInvalidGroupKey        Reason = "invalid-group-key"
	ReasonTargetFieldError       Reason = "target-field-error"
	ReasonOverflow               Reason = "overflow"
	ReasonCardinalityExceeded    Reason = "cardinality-exceeded"
	ReasonAggregationInterrupted Reason = "aggregation-interrupted"
)

// NewAggregateError creates a stable sanitized error and copies provenance.
func NewAggregateError(sourceID string, aggregateName string, function v1alpha1.KubeseerAggregationFunction, fieldName string, provenance *selection.Provenance, reason Reason, message string) *AggregateError {
	var copied *selection.Provenance
	if provenance != nil {
		value := *provenance
		copied = &value
	}
	return &AggregateError{
		SourceID:      sourceID,
		AggregateName: aggregateName,
		Function:      function,
		FieldName:     fieldName,
		Provenance:    copied,
		Reason:        reason,
		Message:       message,
	}
}

func aggregateErrorWithCause(sourceID string, aggregateName string, function v1alpha1.KubeseerAggregationFunction, fieldName string, provenance *selection.Provenance, reason Reason, message string, cause error) *AggregateError {
	err := NewAggregateError(sourceID, aggregateName, function, fieldName, provenance, reason, message)
	err.cause = cause
	return err
}

// Error implements a value-free diagnostic containing only declaration and
// resource identity plus a stable reason.
func (e *AggregateError) Error() string {
	if e == nil {
		return ""
	}
	identity := fmt.Sprintf("source %q", e.SourceID)
	if e.AggregateName != "" {
		identity += fmt.Sprintf(", aggregate %q", e.AggregateName)
	}
	if e.Function != "" {
		identity += fmt.Sprintf(", function %q", e.Function)
	}
	if e.FieldName != "" {
		identity += fmt.Sprintf(", field %q", e.FieldName)
	}
	if e.Provenance != nil {
		identity += fmt.Sprintf(", resource %q %q %q/%q", e.Provenance.APIVersion, e.Provenance.Kind, e.Provenance.Namespace, e.Provenance.Name)
	}
	if e.Message == "" {
		return fmt.Sprintf("%s: %s", identity, e.Reason)
	}
	return fmt.Sprintf("%s: %s: %s", identity, e.Reason, e.Message)
}

// Unwrap exposes the internal cause to programmatic callers only.
func (e *AggregateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// HasReason reports whether err contains the requested aggregation reason.
func HasReason(err error, reason Reason) bool {
	var aggregateErr *AggregateError
	return errors.As(err, &aggregateErr) && aggregateErr.Reason == reason
}

// GroupFieldPlan captures one ordered grouping field and its logical type.
type GroupFieldPlan struct {
	name     string
	typeName v1alpha1.KubeseerValueType
}

// Name returns the declared grouping field name.
func (p GroupFieldPlan) Name() string { return p.name }

// Type returns the declared grouping field type.
func (p GroupFieldPlan) Type() v1alpha1.KubeseerValueType { return p.typeName }

// AggregatePlan is one immutable valid aggregate declaration.
type AggregatePlan struct {
	sourceID          string
	name              string
	function          v1alpha1.KubeseerAggregationFunction
	field             string
	fieldType         v1alpha1.KubeseerValueType
	groupBy           []GroupFieldPlan
	includeProvenance bool
	precision         int32
	roundingMode      v1alpha1.KubeseerRoundingMode
	limits            Limits
}

// SourceID returns the source identity captured by the plan.
func (p AggregatePlan) SourceID() string { return p.sourceID }

// Name returns the aggregate declaration name.
func (p AggregatePlan) Name() string { return p.name }

// Function returns the closed reducer function.
func (p AggregatePlan) Function() v1alpha1.KubeseerAggregationFunction { return p.function }

// Field returns the target field name.
func (p AggregatePlan) Field() string { return p.field }

// FieldType returns the target logical type.
func (p AggregatePlan) FieldType() v1alpha1.KubeseerValueType { return p.fieldType }

// GroupBy returns ordered grouping field plans.
func (p AggregatePlan) GroupBy() []GroupFieldPlan {
	return append([]GroupFieldPlan(nil), p.groupBy...)
}

// IncludeProvenance reports whether contributor provenance is public.
func (p AggregatePlan) IncludeProvenance() bool { return p.includeProvenance }

// Precision returns the effective average precision. It is meaningful for
// average plans and is still stable on other plans for defensive inspection.
func (p AggregatePlan) Precision() int32 { return p.precision }

// RoundingMode returns the effective average rounding mode.
func (p AggregatePlan) RoundingMode() v1alpha1.KubeseerRoundingMode { return p.roundingMode }

// Limits returns the copied per-plan cardinality ceilings.
func (p AggregatePlan) Limits() Limits { return p.limits }

func cloneAggregatePlan(plan AggregatePlan) AggregatePlan {
	plan.groupBy = append([]GroupFieldPlan(nil), plan.groupBy...)
	return plan
}

type planEntry struct {
	plan    *AggregatePlan
	failure *AggregateError
}

// PlanOutcome contains all valid declaration plans and independent planning
// failures in lexical declaration order.
type PlanOutcome struct {
	sourceID string
	limits   Limits
	entries  []planEntry
}

// SourceID returns the source identity associated with the outcome.
func (o PlanOutcome) SourceID() string { return o.sourceID }

// Valid reports whether every declaration produced a valid plan.
func (o PlanOutcome) Valid() bool {
	return o.sourceID != "" && len(o.entries) >= 0 && len(o.Failures()) == 0
}

// Plans returns valid aggregate plans in lexical aggregate-name order.
func (o PlanOutcome) Plans() []AggregatePlan {
	if o.entries == nil {
		return nil
	}
	plans := make([]AggregatePlan, 0, len(o.entries))
	for _, entry := range o.entries {
		if entry.plan != nil {
			plans = append(plans, cloneAggregatePlan(*entry.plan))
		}
	}
	return plans
}

// Plan is an alias for Plans for callers that treat one source plan as a
// collection of named aggregate declarations.
func (o PlanOutcome) Plan() []AggregatePlan { return o.Plans() }

// Failures returns independent copies of declaration planning failures.
func (o PlanOutcome) Failures() []*AggregateError {
	if o.entries == nil {
		return nil
	}
	failures := make([]*AggregateError, 0)
	for _, entry := range o.entries {
		if entry.failure == nil {
			continue
		}
		failures = append(failures, cloneAggregateError(entry.failure))
	}
	return failures
}

func cloneAggregateError(input *AggregateError) *AggregateError {
	if input == nil {
		return nil
	}
	copy := NewAggregateError(input.SourceID, input.AggregateName, input.Function, input.FieldName, input.Provenance, input.Reason, input.Message)
	copy.cause = input.cause
	return copy
}

// SourceInput joins one source's aggregation plan outcome to the completed
// value-operator outcome. The singular Operator field is retained as a
// convenient compatibility alias; EvaluateBatch prefers Operators when set.
type SourceInput struct {
	Plan      PlanOutcome
	Operators operators.SourceOutcome
	Operator  operators.SourceOutcome
}

// Contribution is one immutable non-null target match with complete resource
// provenance and its original typed-match index.
type Contribution struct {
	match      typedoutput.Match
	provenance selection.Provenance
	matchIndex int
}

// Match returns a defensive immutable typed match.
func (c Contribution) Match() typedoutput.Match { return c.match }

// Provenance returns complete resource identity.
func (c Contribution) Provenance() selection.Provenance { return c.provenance }

// MatchIndex returns the target field's original match index.
func (c Contribution) MatchIndex() int { return c.matchIndex }

// AggregateKey is one ordered, typed group-key component.
type AggregateKey struct {
	field     string
	typeName  v1alpha1.KubeseerValueType
	match     typedoutput.Match
	canonical []byte
}

// Field returns the grouping field name.
func (k AggregateKey) Field() string { return k.field }

// Type returns the grouping field type.
func (k AggregateKey) Type() v1alpha1.KubeseerValueType { return k.typeName }

// Value returns the immutable typed key match.
func (k AggregateKey) Value() typedoutput.Match { return k.match }

func (k AggregateKey) canonicalKey() []byte { return append([]byte(nil), k.canonical...) }

// AggregateValueMatch is one reducer output value and optional value-level
// provenance for collect/distinct.
type AggregateValueMatch struct {
	match        typedoutput.Match
	contributors []selection.Provenance
}

// Value returns the immutable typed reducer value.
func (m AggregateValueMatch) Value() typedoutput.Match { return m.match }

// Contributors returns defensive provenance copies.
func (m AggregateValueMatch) Contributors() []selection.Provenance {
	return append([]selection.Provenance(nil), m.contributors...)
}

// AggregateValue is one explicit typed reducer result.
type AggregateValue struct {
	typeName v1alpha1.KubeseerValueType
	state    v1alpha1.KubeseerAggregateValueState
	matches  []AggregateValueMatch
}

// Type returns the output logical type.
func (v AggregateValue) Type() v1alpha1.KubeseerValueType { return v.typeName }

// State returns absent or values, including an intentionally empty collection.
func (v AggregateValue) State() v1alpha1.KubeseerAggregateValueState { return v.state }

// Matches returns defensive copies of reducer output values.
func (v AggregateValue) Matches() []AggregateValueMatch {
	if v.matches == nil {
		return nil
	}
	output := make([]AggregateValueMatch, len(v.matches))
	for index, match := range v.matches {
		output[index] = AggregateValueMatch{match: match.match, contributors: append([]selection.Provenance(nil), match.contributors...)}
	}
	return output
}

// AggregateGroup is one ordered typed key tuple and reducer output.
type AggregateGroup struct {
	keys         []AggregateKey
	value        AggregateValue
	contributors []selection.Provenance
}

// Keys returns ordered defensive key copies.
func (g AggregateGroup) Keys() []AggregateKey {
	if g.keys == nil {
		return nil
	}
	keys := make([]AggregateKey, len(g.keys))
	for index, key := range g.keys {
		keys[index] = AggregateKey{field: key.field, typeName: key.typeName, match: key.match, canonical: append([]byte(nil), key.canonical...)}
	}
	return keys
}

// Value returns the reducer output.
func (g AggregateGroup) Value() AggregateValue { return g.value }

// Contributors returns group-level defensive provenance copies.
func (g AggregateGroup) Contributors() []selection.Provenance {
	return append([]selection.Provenance(nil), g.contributors...)
}

// ResourceFailure associates a sanitized resource-scoped error with a
// resource that could not contribute to one aggregate.
type ResourceFailure struct {
	provenance selection.Provenance
	err        *AggregateError
}

// Provenance returns the affected resource identity.
func (f ResourceFailure) Provenance() selection.Provenance { return f.provenance }

// Error returns a defensive failure copy.
func (f ResourceFailure) Error() *AggregateError { return cloneAggregateError(f.err) }

// AggregateOutcome is exactly one evaluated declaration, including its
// terminal state, groups, resource failures, or aggregate-scoped error.
type AggregateOutcome struct {
	name     string
	function v1alpha1.KubeseerAggregationFunction
	field    string
	state    v1alpha1.KubeseerAggregateState
	groups   []AggregateGroup
	failures []ResourceFailure
	err      *AggregateError
}

// Name returns the declaration name.
func (o AggregateOutcome) Name() string { return o.name }

// Function returns the reducer function.
func (o AggregateOutcome) Function() v1alpha1.KubeseerAggregationFunction { return o.function }

// Field returns the target field.
func (o AggregateOutcome) Field() string { return o.field }

// State returns values, degraded, or error.
func (o AggregateOutcome) State() v1alpha1.KubeseerAggregateState { return o.state }

// Groups returns defensive groups in canonical tuple order.
func (o AggregateOutcome) Groups() []AggregateGroup {
	return append([]AggregateGroup(nil), o.groups...)
}

// Failures returns defensive resource-scoped failures.
func (o AggregateOutcome) Failures() []ResourceFailure {
	return append([]ResourceFailure(nil), o.failures...)
}

// Err returns a defensive aggregate-scoped failure.
func (o AggregateOutcome) Err() *AggregateError { return cloneAggregateError(o.err) }

// SourceOutcome carries the unchanged operator source result alongside
// ordered aggregate outcomes, or a source-scoped aggregation error.
type SourceOutcome struct {
	sourceID   string
	operator   operators.SourceOutcome
	aggregates []AggregateOutcome
	err        *AggregateError
}

// SourceID returns the source identity.
func (o SourceOutcome) SourceID() string { return o.sourceID }

// OperatorOutcome returns the unchanged value-operator outcome.
func (o SourceOutcome) OperatorOutcome() operators.SourceOutcome { return o.operator }

// Aggregates returns defensive aggregate outcomes in declaration order.
func (o SourceOutcome) Aggregates() []AggregateOutcome {
	return append([]AggregateOutcome(nil), o.aggregates...)
}

// Err returns a source-scoped aggregation failure, if present.
func (o SourceOutcome) Err() *AggregateError { return cloneAggregateError(o.err) }

func cloneProvenance(input selection.Provenance) selection.Provenance { return input }
