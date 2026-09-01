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

package typedoutput

import (
	"errors"
	"fmt"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/selection"
)

// ConversionErrorReason is the stable category of a typed-output planning or
// conversion failure.
type ConversionErrorReason string

const (
	ReasonMissingType            ConversionErrorReason = "MissingType"
	ReasonUnsupportedType        ConversionErrorReason = "UnsupportedType"
	ReasonConversionTypeMismatch ConversionErrorReason = "ConversionTypeMismatch"
	ReasonInvalidValue           ConversionErrorReason = "InvalidValue"
	ReasonConversionOverflow     ConversionErrorReason = "ConversionOverflow"
	ReasonForbiddenConversion    ConversionErrorReason = "ForbiddenConversion"
	ReasonInvalidInput           ConversionErrorReason = "InvalidInput"
	ReasonConversionInterrupted  ConversionErrorReason = "ConversionInterrupted"
	ReasonValueLimitExceeded     ConversionErrorReason = "ValueLimitExceeded"
)

// ConversionError is a sanitized typed-output failure. Its private cause is
// available through errors.Is/errors.As without being exposed in Error().
type ConversionError struct {
	SourceID   string
	FieldName  string
	FieldIndex int
	Provenance *selection.Provenance
	Reason     ConversionErrorReason
	Message    string

	cause error
}

// NewConversionError creates a stable, sanitized conversion error. The
// provenance pointer is copied so callers cannot mutate the error identity.
func NewConversionError(sourceID, fieldName string, provenance *selection.Provenance, reason ConversionErrorReason, message string) *ConversionError {
	var copied *selection.Provenance
	if provenance != nil {
		value := *provenance
		copied = &value
	}
	return &ConversionError{
		SourceID:   sourceID,
		FieldName:  fieldName,
		FieldIndex: -1,
		Provenance: copied,
		Reason:     reason,
		Message:    message,
	}
}

func conversionErrorWithCause(sourceID, fieldName string, provenance *selection.Provenance, reason ConversionErrorReason, message string, cause error) *ConversionError {
	err := NewConversionError(sourceID, fieldName, provenance, reason, message)
	err.cause = cause
	return err
}

// Error implements error with deterministic configuration and provenance
// identity. It never includes paths, native values, or resource contents.
func (e *ConversionError) Error() string {
	if e == nil {
		return ""
	}
	identity := fmt.Sprintf("source %q", e.SourceID)
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

// Unwrap exposes an underlying cause to programmatic callers only.
func (e *ConversionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// HasReason reports whether err contains the requested stable reason.
func HasReason(err error, reason ConversionErrorReason) bool {
	var conversionErr *ConversionError
	return errors.As(err, &conversionErr) && conversionErr.Reason == reason
}

// FieldPlan is one immutable typed field declaration.
type FieldPlan struct {
	sourceID string
	name     string
	typeName v1alpha1.KubeseerValueType
}

// SourceID returns the source identifier captured in the plan.
func (p FieldPlan) SourceID() string { return p.sourceID }

// Name returns the field identifier captured in the plan.
func (p FieldPlan) Name() string { return p.name }

// Type returns the explicit logical type captured in the plan.
func (p FieldPlan) Type() v1alpha1.KubeseerValueType { return p.typeName }

// Plan is an immutable valid subset of one source's typed declarations.
type Plan struct {
	sourceID string
	fields   []FieldPlan
	failures []*ConversionError
}

// SourceID returns the source identifier captured during planning.
func (p Plan) SourceID() string { return p.sourceID }

// Fields returns field plans in lexicographic field-name order.
func (p Plan) Fields() []FieldPlan {
	fields := make([]FieldPlan, len(p.fields))
	copy(fields, p.fields)
	return fields
}

// Failures returns independent copies of field-local planning failures.
func (p Plan) Failures() []*ConversionError { return cloneErrors(p.failures) }

// PlanOutcome preserves valid field plans and field-local planning failures
// for one source.
type PlanOutcome struct {
	sourceID string
	plan     Plan
	failures []*ConversionError
}

// SourceID returns the source identifier associated with the outcome.
func (o PlanOutcome) SourceID() string { return o.sourceID }

// Plan returns the immutable valid plan subset.
func (o PlanOutcome) Plan() Plan { return clonePlan(o.plan) }

// Failures returns independent copies of all field-local planning failures.
func (o PlanOutcome) Failures() []*ConversionError { return cloneErrors(o.failures) }

func clonePlan(plan Plan) Plan {
	plan.fields = append([]FieldPlan(nil), plan.fields...)
	plan.failures = cloneErrors(plan.failures)
	return plan
}

func cloneErrors(errorsIn []*ConversionError) []*ConversionError {
	if errorsIn == nil {
		return nil
	}
	errorsOut := make([]*ConversionError, len(errorsIn))
	for index, err := range errorsIn {
		if err == nil {
			continue
		}
		copy := NewConversionError(err.SourceID, err.FieldName, err.Provenance, err.Reason, err.Message)
		copy.FieldIndex = err.FieldIndex
		copy.cause = err.cause
		errorsOut[index] = copy
	}
	return errorsOut
}

func invalidPlanError(sourceID, fieldName string, reason ConversionErrorReason, message string) *ConversionError {
	return NewConversionError(sourceID, fieldName, nil, reason, message)
}

var _ error = (*ConversionError)(nil)
