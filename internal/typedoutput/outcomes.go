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
	"context"
	"errors"
	"sort"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/selection"
)

// SourceState identifies whether a typed source has values or a source-level
// failure.
type SourceState string

const (
	SourceStateValues SourceState = "values"
	SourceStateError  SourceState = "error"
)

// FieldState identifies absence, values, or a field-scoped failure.
type FieldState string

const (
	FieldStateAbsent FieldState = "absent"
	FieldStateValues FieldState = "values"
	FieldStateError  FieldState = "error"
)

// FieldOutcome is one immutable typed result for a declared field. An absent
// field has no matches, a values field has one match for every native match,
// and an error field has no matches.
type FieldOutcome struct {
	name     string
	typeName v1alpha1.KubeseerValueType
	state    FieldState
	matches  []Match
	err      *ConversionError
}

// Name returns the declared field identity.
func (o FieldOutcome) Name() string { return o.name }

// Type returns the declared logical type. It is retained on null matches.
func (o FieldOutcome) Type() v1alpha1.KubeseerValueType { return o.typeName }

// State returns the field cardinality or error branch.
func (o FieldOutcome) State() FieldState { return o.state }

// Matches returns defensive copies in extraction order.
func (o FieldOutcome) Matches() []Match {
	if o.matches == nil {
		return nil
	}
	matches := make([]Match, len(o.matches))
	for index, match := range o.matches {
		matches[index] = match.clone()
	}
	return matches
}

// Err returns a defensive copy of the field conversion failure, if any.
func (o FieldOutcome) Err() *ConversionError { return cloneErrors([]*ConversionError{o.err})[0] }

// Failure is an explicit alias for Err for callers reading the outcome as a
// union of successful and unsuccessful field branches.
func (o FieldOutcome) Failure() *ConversionError { return o.Err() }

// ResourceOutcome is one immutable typed result for a selected resource.
type ResourceOutcome struct {
	provenance selection.Provenance
	fields     []FieldOutcome
}

// Provenance returns the complete selected-resource identity.
func (o ResourceOutcome) Provenance() selection.Provenance { return o.provenance }

// Fields returns defensive copies in lexicographic field-name order.
func (o ResourceOutcome) Fields() []FieldOutcome {
	if o.fields == nil {
		return nil
	}
	fields := make([]FieldOutcome, len(o.fields))
	for index, field := range o.fields {
		fields[index] = field.clone()
	}
	return fields
}

// SourceOutcome is one immutable typed result for a source. Source-level
// planning failures remain separate from resource-local field outcomes. Err
// preserves an upstream extraction failure or a source-level interruption.
type SourceOutcome struct {
	sourceID    string
	resources   []ResourceOutcome
	fieldErrors []*ConversionError
	err         error
}

// SourceID returns the source identity captured by conversion.
func (o SourceOutcome) SourceID() string { return o.sourceID }

// State returns the source success or source-error branch.
func (o SourceOutcome) State() SourceState {
	if o.err != nil {
		return SourceStateError
	}
	return SourceStateValues
}

// Resources returns defensive copies in extraction order.
func (o SourceOutcome) Resources() []ResourceOutcome {
	if o.resources == nil {
		return nil
	}
	resources := make([]ResourceOutcome, len(o.resources))
	for index, resource := range o.resources {
		resources[index] = resource.clone()
	}
	return resources
}

// FieldErrors returns defensive copies of source-level planning failures in
// deterministic field order.
func (o SourceOutcome) FieldErrors() []*ConversionError { return cloneErrors(o.fieldErrors) }

// Err returns the source-level failure. An unsuccessful extraction error is
// returned unchanged so errors.Is/errors.As retain the upstream identity.
func (o SourceOutcome) Err() error { return o.err }

// ConvertField converts one extraction field outcome. A failed match discards
// every temporary match for that field; absent and explicit-null outcomes stay
// distinct.
func ConvertField(plan FieldPlan, input extraction.FieldOutcome, provenance selection.Provenance) FieldOutcome {
	field, _ := convertFieldContext(context.Background(), plan, input, provenance)
	return field
}

func convertFieldContext(ctx context.Context, plan FieldPlan, input extraction.FieldOutcome, provenance selection.Provenance) (FieldOutcome, *ConversionError) {
	if input.FieldName != plan.name {
		name := input.FieldName
		return newFieldErrorOutcome(name, plan.typeName, NewConversionError(plan.sourceID, name, &provenance, ReasonInvalidInput, "extraction field identity does not match declaration")), nil
	}

	count := input.Matches.Len()
	if count == 0 {
		return newAbsentFieldOutcome(plan.name, plan.typeName), nil
	}
	nativeValues := input.Matches.Values()
	if len(nativeValues) != count {
		return newFieldErrorOutcome(plan.name, plan.typeName, conversionErrorWithCause(plan.sourceID, plan.name, &provenance, ReasonInvalidInput, "extraction matches are not JSON-compatible", errors.New("invalid extraction match set"))), nil
	}

	matches := make([]Match, 0, count)
	for _, native := range nativeValues {
		if cause := ctx.Err(); cause != nil {
			return FieldOutcome{}, interruptedConversionError(plan.sourceID, plan.name, &provenance, cause)
		}
		match, err := ConvertMatch(plan, native, &provenance)
		if err != nil {
			var conversionErr *ConversionError
			if !errors.As(err, &conversionErr) {
				conversionErr = NewConversionError(plan.sourceID, plan.name, &provenance, ReasonInvalidInput, "typed conversion returned an invalid error")
			}
			return newFieldErrorOutcome(plan.name, plan.typeName, conversionErr), nil
		}
		matches = append(matches, match)
	}
	if cause := ctx.Err(); cause != nil {
		return FieldOutcome{}, interruptedConversionError(plan.sourceID, plan.name, &provenance, cause)
	}
	return newValuesFieldOutcome(plan.name, plan.typeName, matches), nil
}

// ConvertResource joins a valid source plan to one extraction resource. It
// preserves resource provenance, normalizes field order, and turns identity
// mismatches into field-local invalid-input outcomes.
func ConvertResource(plan Plan, input extraction.ResourceOutcome) ResourceOutcome {
	resource, _ := convertResourceContext(context.Background(), plan, input)
	return resource
}

func convertResourceContext(ctx context.Context, plan Plan, input extraction.ResourceOutcome) (ResourceOutcome, *ConversionError) {
	byName := make(map[string][]extraction.FieldOutcome, len(input.Fields))
	for _, field := range input.Fields {
		byName[field.FieldName] = append(byName[field.FieldName], field)
	}

	names := make([]string, 0, len(byName)+len(plan.fields))
	seen := make(map[string]struct{}, len(byName)+len(plan.fields))
	for _, field := range plan.fields {
		if _, found := seen[field.name]; !found {
			names = append(names, field.name)
			seen[field.name] = struct{}{}
		}
	}
	for name := range byName {
		if _, found := seen[name]; !found {
			names = append(names, name)
			seen[name] = struct{}{}
		}
	}
	sort.Strings(names)

	declared := make(map[string]FieldPlan, len(plan.fields))
	for _, field := range plan.fields {
		declared[field.name] = field
	}
	fields := make([]FieldOutcome, 0, len(names))
	for _, name := range names {
		if cause := ctx.Err(); cause != nil {
			return ResourceOutcome{}, interruptedConversionError(plan.sourceID, "", &input.Provenance, cause)
		}
		fieldPlan, isDeclared := declared[name]
		inputs := byName[name]
		if !isDeclared {
			fields = append(fields, newFieldErrorOutcome(name, "", NewConversionError(plan.sourceID, name, &input.Provenance, ReasonInvalidInput, "extraction field is not declared in the typed plan")))
			continue
		}
		if len(inputs) != 1 {
			fields = append(fields, newFieldErrorOutcome(name, fieldPlan.typeName, NewConversionError(plan.sourceID, name, &input.Provenance, ReasonInvalidInput, "extraction field identity is duplicated")))
			continue
		}
		field, interrupted := convertFieldContext(ctx, fieldPlan, inputs[0], input.Provenance)
		if interrupted != nil {
			return ResourceOutcome{}, interrupted
		}
		fields = append(fields, field)
	}

	return ResourceOutcome{provenance: input.Provenance, fields: fields}, nil
}

// ConvertSource joins one immutable typed plan outcome to one extraction
// source outcome. It is the source-local composition primitive used by the
// ordered batch converter.
func ConvertSource(plan PlanOutcome, input extraction.SourceOutcome) SourceOutcome {
	return convertSourceContext(context.Background(), plan, input)
}

// ConvertSourceWithLimit converts one source with a source-local canonical
// output ceiling. The partial typed result is discarded on overflow.
func ConvertSourceWithLimit(ctx context.Context, plan PlanOutcome, input extraction.SourceOutcome, maxBytes int64) SourceOutcome {
	return convertSourceContextWithLimit(ctx, plan, input, maxBytes)
}

func convertSourceContext(ctx context.Context, plan PlanOutcome, input extraction.SourceOutcome) SourceOutcome {
	return convertSourceContextWithLimit(ctx, plan, input, 0)
}

func convertSourceContextWithLimit(ctx context.Context, plan PlanOutcome, input extraction.SourceOutcome, maxBytes int64) SourceOutcome {
	sourceID := plan.sourceID
	if input.SourceID != sourceID {
		return SourceOutcome{
			sourceID: sourceID,
			err:      NewConversionError(sourceID, "", nil, ReasonInvalidInput, "extraction source identity does not match declaration"),
		}
	}
	if input.Err != nil {
		return SourceOutcome{sourceID: sourceID, err: input.Err}
	}

	resources := make([]ResourceOutcome, 0, len(input.Resources))
	var accountant *limits.Accountant
	if maxBytes > 0 {
		var err error
		accountant, err = limits.NewAccountant(maxBytes, "produced-values")
		if err != nil {
			return SourceOutcome{sourceID: sourceID, err: valueLimitConversionError(sourceID)}
		}
	}
	for _, resource := range input.Resources {
		if cause := ctx.Err(); cause != nil {
			return SourceOutcome{sourceID: sourceID, err: interruptedConversionError(sourceID, "", nil, cause)}
		}
		converted, interrupted := convertResourceContext(ctx, plan.plan, resource)
		if interrupted != nil {
			return SourceOutcome{sourceID: sourceID, err: interrupted}
		}
		if accountant != nil {
			projected, err := ProjectResourceResult(converted.provenance, converted.fields)
			if err != nil {
				return SourceOutcome{sourceID: sourceID, err: valueLimitConversionError(sourceID)}
			}
			if err := accountant.Add(projected); err != nil {
				return SourceOutcome{sourceID: sourceID, err: valueLimitConversionError(sourceID)}
			}
		}
		resources = append(resources, converted)
	}
	return SourceOutcome{
		sourceID:    sourceID,
		resources:   resources,
		fieldErrors: cloneErrors(plan.failures),
	}
}

func valueLimitConversionError(sourceID string) *ConversionError {
	return NewConversionError(sourceID, "", nil, ReasonValueLimitExceeded, "produced value ceiling exceeded")
}

func interruptedConversionError(sourceID, fieldName string, provenance *selection.Provenance, cause error) *ConversionError {
	return conversionErrorWithCause(sourceID, fieldName, provenance, ReasonConversionInterrupted, "conversion interrupted", cause)
}

func newAbsentFieldOutcome(name string, typeName v1alpha1.KubeseerValueType) FieldOutcome {
	return FieldOutcome{name: name, typeName: typeName, state: FieldStateAbsent}
}

func newValuesFieldOutcome(name string, typeName v1alpha1.KubeseerValueType, matches []Match) FieldOutcome {
	cloned := make([]Match, len(matches))
	for index, match := range matches {
		cloned[index] = match.clone()
	}
	return FieldOutcome{name: name, typeName: typeName, state: FieldStateValues, matches: cloned}
}

func newFieldErrorOutcome(name string, typeName v1alpha1.KubeseerValueType, err *ConversionError) FieldOutcome {
	return FieldOutcome{name: name, typeName: typeName, state: FieldStateError, err: cloneErrors([]*ConversionError{err})[0]}
}

func (o FieldOutcome) clone() FieldOutcome {
	copy := o
	copy.err = cloneErrors([]*ConversionError{o.err})[0]
	if o.matches != nil {
		copy.matches = make([]Match, len(o.matches))
		for index, match := range o.matches {
			copy.matches[index] = match.clone()
		}
	}
	return copy
}

func (o ResourceOutcome) clone() ResourceOutcome {
	copy := o
	if o.fields != nil {
		copy.fields = make([]FieldOutcome, len(o.fields))
		for index, field := range o.fields {
			copy.fields[index] = field.clone()
		}
	}
	return copy
}
