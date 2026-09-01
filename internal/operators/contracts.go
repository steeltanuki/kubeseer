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

// Package operators plans and evaluates the closed, field-local operator
// vocabulary over immutable typed-output outcomes. It performs no Kubernetes
// I/O, discovery, extraction, authorization, or status writes.
package operators

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

// Reason is the stable category of an operator planning or evaluation
// failure. Values are intentionally independent of implementation errors.
type Reason string

const (
	ReasonUnsupportedOperator  Reason = "unsupported-operator"
	ReasonIncompatibleOperator Reason = "incompatible-operator"
	ReasonInvalidArity         Reason = "invalid-arity"
	ReasonInvalidOperand       Reason = "invalid-operand"
	ReasonInvalidPattern       Reason = "invalid-pattern"
	ReasonInvalidInput         Reason = "invalid-input"
	ReasonOperatorInterrupted  Reason = "operator-interrupted"
	ReasonValueLimitExceeded   Reason = "ValueLimitExceeded"
)

// OperatorError contains only stable declaration identity, optional resource
// provenance, and sanitized metadata. Its cause is private and available
// through errors.Is/errors.As only.
type OperatorError struct {
	SourceID      string
	FieldName     string
	FieldIndex    int
	OperatorIndex int
	ValueIndex    int
	OperatorName  string
	Provenance    *selection.Provenance
	Reason        Reason
	Message       string

	cause error
}

// NewOperatorError creates a sanitized operator error. The provenance value
// is copied so the error does not retain a mutable caller-owned pointer.
func NewOperatorError(sourceID, fieldName string, operatorIndex int, operatorName string, provenance *selection.Provenance, reason Reason, message string) *OperatorError {
	var copied *selection.Provenance
	if provenance != nil {
		value := *provenance
		copied = &value
	}
	return &OperatorError{
		SourceID:      sourceID,
		FieldName:     fieldName,
		FieldIndex:    -1,
		OperatorIndex: operatorIndex,
		ValueIndex:    -1,
		OperatorName:  operatorName,
		Provenance:    copied,
		Reason:        reason,
		Message:       message,
	}
}

func operatorErrorWithCause(sourceID, fieldName string, operatorIndex int, operatorName string, provenance *selection.Provenance, reason Reason, message string, cause error) *OperatorError {
	err := NewOperatorError(sourceID, fieldName, operatorIndex, operatorName, provenance, reason, message)
	err.cause = cause
	return err
}

// Error implements a deterministic diagnostic that never contains operands,
// paths, extracted values, or resource contents.
func (e *OperatorError) Error() string {
	if e == nil {
		return ""
	}
	identity := fmt.Sprintf("source %q", e.SourceID)
	if e.FieldName != "" {
		identity += fmt.Sprintf(", field %q", e.FieldName)
	}
	if e.OperatorIndex >= 0 {
		identity += fmt.Sprintf(", operator %d %q", e.OperatorIndex, e.OperatorName)
	}
	if e.Provenance != nil {
		identity += fmt.Sprintf(", resource %q %q %q/%q", e.Provenance.APIVersion, e.Provenance.Kind, e.Provenance.Namespace, e.Provenance.Name)
	}
	if e.Message == "" {
		return fmt.Sprintf("%s: %s", identity, e.Reason)
	}
	return fmt.Sprintf("%s: %s: %s", identity, e.Reason, e.Message)
}

// Unwrap exposes the private cause for programmatic inspection only.
func (e *OperatorError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// HasReason reports whether err contains the requested stable reason.
func HasReason(err error, reason Reason) bool {
	var operatorErr *OperatorError
	return errors.As(err, &operatorErr) && operatorErr.Reason == reason
}

// ResourceState identifies the terminal operator decision for one resource.
type ResourceState string

const (
	ResourceAccepted     ResourceState = "accepted"
	ResourceRejected     ResourceState = "rejected"
	ResourceUnsuccessful ResourceState = "unsuccessful"
)

// OperatorPlan is one immutable planned operator. Values are exposed through
// defensive accessors; the regular expression is retained only internally.
type OperatorPlan struct {
	index      int
	kind       v1alpha1.KubeseerOperatorName
	hasOperand bool
	operand    typedoutput.Match
	operands   []typedoutput.Match
	pattern    *regexp.Regexp
}

// Index returns the zero-based declaration index.
func (p OperatorPlan) Index() int { return p.index }

// Name returns the normalized supported operator name.
func (p OperatorPlan) Name() v1alpha1.KubeseerOperatorName { return p.kind }

// Operand returns the single configured operand when the operator has one.
func (p OperatorPlan) Operand() (typedoutput.Match, bool) {
	if !p.hasOperand {
		return typedoutput.Match{}, false
	}
	return p.operand, true
}

// Operands returns configured membership operands in declaration order.
func (p OperatorPlan) Operands() []typedoutput.Match {
	if p.operands == nil {
		return nil
	}
	return append([]typedoutput.Match(nil), p.operands...)
}

// Pattern returns the source regular-expression text. The compiled expression
// is immutable and intentionally not exposed as a mutable implementation
// object.
func (p OperatorPlan) Pattern() string {
	if p.pattern == nil {
		return ""
	}
	return p.pattern.String()
}

func (p OperatorPlan) regexp() *regexp.Regexp { return p.pattern }

// FieldPlan is one immutable field declaration with operators in original
// declaration order.
type FieldPlan struct {
	sourceID  string
	name      string
	typeName  v1alpha1.KubeseerValueType
	operators []OperatorPlan
}

// SourceID returns the source identifier captured by the plan.
func (p FieldPlan) SourceID() string { return p.sourceID }

// Name returns the field identifier captured by the plan.
func (p FieldPlan) Name() string { return p.name }

// Type returns the field's explicit logical type.
func (p FieldPlan) Type() v1alpha1.KubeseerValueType { return p.typeName }

// Operators returns the ordered operator plans with copied slices.
func (p FieldPlan) Operators() []OperatorPlan {
	if p.operators == nil {
		return nil
	}
	operators := make([]OperatorPlan, len(p.operators))
	for index, operator := range p.operators {
		operators[index] = cloneOperatorPlan(operator)
	}
	return operators
}

// SourcePlan is one immutable source plan. Its fields are sorted
// lexicographically by field name.
type SourcePlan struct {
	sourceID string
	fields   []FieldPlan
}

// SourceID returns the source identifier captured by the plan.
func (p SourcePlan) SourceID() string { return p.sourceID }

// Fields returns field plans in lexicographic order.
func (p SourcePlan) Fields() []FieldPlan {
	if p.fields == nil {
		return nil
	}
	fields := make([]FieldPlan, len(p.fields))
	for index, field := range p.fields {
		fields[index] = cloneFieldPlan(field)
	}
	return fields
}

// PlanOutcome contains a complete valid source plan or all deterministic
// declaration failures. A source with any failure is not evaluable.
type PlanOutcome struct {
	sourceID string
	plan     SourcePlan
	failures []*OperatorError
}

// SourceID returns the source identifier associated with this outcome.
func (o PlanOutcome) SourceID() string { return o.sourceID }

// Valid reports whether every declaration was planned successfully.
func (o PlanOutcome) Valid() bool { return o.sourceID != "" && len(o.failures) == 0 }

// Plan returns a defensive copy of the source plan. Callers must check Valid
// before evaluating it.
func (o PlanOutcome) Plan() SourcePlan { return cloneSourcePlan(o.plan) }

// Failures returns defensive copies in deterministic field/operator order.
func (o PlanOutcome) Failures() []*OperatorError { return cloneOperatorErrors(o.failures) }

func cloneOperatorPlan(plan OperatorPlan) OperatorPlan {
	copy := plan
	copy.operands = append([]typedoutput.Match(nil), plan.operands...)
	return copy
}

func cloneFieldPlan(plan FieldPlan) FieldPlan {
	copy := plan
	if plan.operators != nil {
		copy.operators = make([]OperatorPlan, len(plan.operators))
		for index, operator := range plan.operators {
			copy.operators[index] = cloneOperatorPlan(operator)
		}
	}
	return copy
}

func cloneSourcePlan(plan SourcePlan) SourcePlan {
	copy := plan
	if plan.fields != nil {
		copy.fields = make([]FieldPlan, len(plan.fields))
		for index, field := range plan.fields {
			copy.fields[index] = cloneFieldPlan(field)
		}
	}
	return copy
}

func cloneOperatorErrors(input []*OperatorError) []*OperatorError {
	if input == nil {
		return nil
	}
	output := make([]*OperatorError, len(input))
	for index, err := range input {
		if err == nil {
			continue
		}
		copy := NewOperatorError(err.SourceID, err.FieldName, err.OperatorIndex, err.OperatorName, err.Provenance, err.Reason, err.Message)
		copy.FieldIndex = err.FieldIndex
		copy.ValueIndex = err.ValueIndex
		copy.cause = err.cause
		output[index] = copy
	}
	return output
}

// ResourceOutcome is one immutable operator decision. Rejected and
// unsuccessful outcomes intentionally expose no typed fields through Fields.
type ResourceOutcome struct {
	state      ResourceState
	provenance selection.Provenance
	fields     []typedoutput.FieldOutcome
	failure    *OperatorError
}

// State returns accepted, rejected, or unsuccessful.
func (o ResourceOutcome) State() ResourceState { return o.state }

// Provenance returns the selected resource identity.
func (o ResourceOutcome) Provenance() selection.Provenance { return o.provenance }

// Fields returns defensive typed fields. Rejected and unsuccessful outcomes
// return nil.
func (o ResourceOutcome) Fields() []typedoutput.FieldOutcome {
	if o.state != ResourceAccepted || o.fields == nil {
		return nil
	}
	fields := make([]typedoutput.FieldOutcome, len(o.fields))
	copy(fields, o.fields)
	return fields
}

// Failure returns a defensive unsuccessful-resource failure, if present.
func (o ResourceOutcome) Failure() *OperatorError {
	return cloneOperatorErrors([]*OperatorError{o.failure})[0]
}

// SourceOutcome is one immutable operator result for a source.
type SourceOutcome struct {
	sourceID    string
	resources   []ResourceOutcome
	fieldErrors []*OperatorError
	err         error
}

// NewSourceError creates a source-scoped operator outcome without retaining
// any resource or value payload. It is used by downstream atomic stages when
// a source must be discarded after a bounded-output failure.
func NewSourceError(sourceID string, reason Reason, message string) SourceOutcome {
	return SourceOutcome{sourceID: sourceID, err: NewOperatorError(sourceID, "", -1, "", nil, reason, message)}
}

// SourceID returns the source identity.
func (o SourceOutcome) SourceID() string { return o.sourceID }

// Resources returns all internal resource decisions in input order. The
// public result adapter omits rejected resources but retains them here as a
// successful decision for diagnostics and tests.
func (o SourceOutcome) Resources() []ResourceOutcome {
	if o.resources == nil {
		return nil
	}
	resources := make([]ResourceOutcome, len(o.resources))
	for index, resource := range o.resources {
		resources[index] = cloneResourceOutcome(resource)
	}
	return resources
}

// FieldErrors returns source-level planning failures.
func (o SourceOutcome) FieldErrors() []*OperatorError { return cloneOperatorErrors(o.fieldErrors) }

// Err returns an upstream source failure or source-level operator failure.
func (o SourceOutcome) Err() error { return o.err }

// State reports whether the source outcome is successful or failed.
func (o SourceOutcome) State() ResourceState {
	if o.err != nil {
		return ResourceUnsuccessful
	}
	return ResourceAccepted
}

func cloneResourceOutcome(input ResourceOutcome) ResourceOutcome {
	copy := input
	copy.failure = cloneOperatorErrors([]*OperatorError{input.failure})[0]
	if input.fields != nil {
		copy.fields = input.fields
	}
	return copy
}
