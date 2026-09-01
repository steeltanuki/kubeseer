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

package extraction

import (
	"errors"
	"fmt"

	"github.com/steeltanuki/kubeseer/internal/selection"
)

// ExtractionErrorReason is the stable category of a planning or extraction
// failure. The values are part of the internal composition contract consumed by
// later typed-output and reconciliation features.
type ExtractionErrorReason string

const (
	ReasonInvalidExpression      ExtractionErrorReason = "InvalidExpression"
	ReasonUnsupportedExpression  ExtractionErrorReason = "UnsupportedExpression"
	ReasonEvaluationTypeMismatch ExtractionErrorReason = "EvaluationTypeMismatch"
	ReasonInvalidResource        ExtractionErrorReason = "InvalidResource"
	ReasonInvalidInput           ExtractionErrorReason = "InvalidInput"
	ReasonExtractionInterrupted  ExtractionErrorReason = "ExtractionInterrupted"
	ReasonValueLimitExceeded     ExtractionErrorReason = "ValueLimitExceeded"
)

// ExtractionError is a sanitized source- or resource-scoped extraction
// failure. It never includes an observed object, field path, or extracted
// value. Cause is deliberately private and is available only through
// errors.Is/errors.As.
type ExtractionError struct {
	SourceID   string
	FieldName  string
	FieldIndex int
	Provenance *selection.Provenance
	Reason     ExtractionErrorReason
	Message    string
	Offset     int

	cause error
}

func (e *ExtractionError) Error() string {
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

func (e *ExtractionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// HasReason reports whether err carries the requested extraction reason.
func HasReason(err error, reason ExtractionErrorReason) bool {
	var extractionErr *ExtractionError
	return errors.As(err, &extractionErr) && extractionErr.Reason == reason
}

// Plan is an immutable compiled set of field expressions for one source.
// Its operation representation is private; accessors return copies of the
// field-plan values and never expose mutable operation state.
type Plan struct {
	sourceID string
	fields   []FieldPlan
}

// SourceID returns the source identifier captured during compilation.
func (p Plan) SourceID() string { return p.sourceID }

// Fields returns a defensive copy of the fields sorted by field name.
func (p Plan) Fields() []FieldPlan {
	fields := make([]FieldPlan, len(p.fields))
	for index, field := range p.fields {
		fields[index] = field.clone()
	}
	return fields
}

// FieldPlan is one immutable compiled field declaration.
type FieldPlan struct {
	name string
	path string
	ops  []operation
}

// Name returns the declared field identifier.
func (p FieldPlan) Name() string { return p.name }

// Path returns the declared expression. It is configuration, not observed
// resource data, and is useful to composition code that needs a stable plan
// fingerprint.
func (p FieldPlan) Path() string { return p.path }

func (p FieldPlan) clone() FieldPlan {
	p.ops = append([]operation(nil), p.ops...)
	return p
}

// CompileOutcome preserves one source's independent planning result when a
// batch contains both valid and invalid declarations.
type CompileOutcome struct {
	SourceID string
	Plan     Plan
	Err      *ExtractionError
}
