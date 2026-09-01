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

package operators

import (
	"context"
	"strings"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

// SourceInput joins one complete operator plan outcome with the corresponding
// completed typed-output source outcome.
type SourceInput struct {
	Plan  PlanOutcome
	Typed typedoutput.SourceOutcome
}

// EvaluateBatch evaluates sources sequentially and preserves source order.
// A planning failure is source-scoped; an upstream typed source error is
// passed through unchanged. Cancellation turns the active and later sources
// into deterministic operator-interrupted outcomes while completed sources
// remain intact.
func EvaluateBatch(ctx context.Context, inputs []SourceInput) []SourceOutcome {
	return EvaluateBatchWithLimit(ctx, inputs, 0)
}

// EvaluateBatchWithLimit evaluates sources with a fresh canonical produced
// value accountant per source. A zero ceiling preserves direct-call behavior.
func EvaluateBatchWithLimit(ctx context.Context, inputs []SourceInput, maxBytes int64) []SourceOutcome {
	if ctx == nil {
		ctx = context.Background()
	}
	if inputs == nil {
		return nil
	}
	outcomes := make([]SourceOutcome, len(inputs))
	interrupted := false
	for index, input := range inputs {
		sourceID := input.Plan.SourceID()
		if sourceID == "" {
			sourceID = input.Typed.SourceID()
		}
		if interrupted {
			outcomes[index] = interruptedSourceOutcome(sourceID, context.Canceled)
			continue
		}
		if cause := ctx.Err(); cause != nil {
			interrupted = true
			outcomes[index] = interruptedSourceOutcome(sourceID, cause)
			continue
		}
		if input.Typed.SourceID() != input.Plan.SourceID() {
			outcomes[index] = SourceOutcome{
				sourceID: sourceID,
				err:      NewOperatorError(sourceID, "", -1, "", nil, ReasonInvalidInput, "typed source identity does not match the operator plan"),
			}
			continue
		}
		if input.Typed.Err() != nil {
			outcomes[index] = SourceOutcome{sourceID: sourceID, err: input.Typed.Err()}
			continue
		}
		if !input.Plan.Valid() {
			failures := input.Plan.Failures()
			if len(failures) == 0 {
				failures = []*OperatorError{NewOperatorError(sourceID, "", -1, "", nil, ReasonInvalidInput, "operator plan is invalid")}
			}
			outcomes[index] = SourceOutcome{sourceID: sourceID, fieldErrors: failures}
			continue
		}

		plan := input.Plan.Plan()
		resources := input.Typed.Resources()
		var accountant *limits.Accountant
		if maxBytes > 0 {
			var err error
			accountant, err = limits.NewAccountant(maxBytes, "produced-values")
			if err != nil {
				outcomes[index] = NewSourceError(sourceID, ReasonValueLimitExceeded, "produced value ceiling exceeded")
				continue
			}
		}
		processed := make([]ResourceOutcome, 0, len(resources))
		for _, typedResource := range resources {
			if cause := ctx.Err(); cause != nil {
				interrupted = true
				outcomes[index] = interruptedSourceOutcome(sourceID, cause)
				break
			}
			resource, failure := evaluateResource(ctx, plan, typedResource)
			if failure != nil {
				if HasReason(failure, ReasonOperatorInterrupted) {
					interrupted = true
					outcomes[index] = interruptedSourceOutcome(sourceID, failure)
					break
				}
				if err := accountOperatorResource(accountant, resource); err != nil {
					outcomes[index] = NewSourceError(sourceID, ReasonValueLimitExceeded, "produced value ceiling exceeded")
					break
				}
				processed = append(processed, resource)
				continue
			}
			if err := accountOperatorResource(accountant, resource); err != nil {
				outcomes[index] = NewSourceError(sourceID, ReasonValueLimitExceeded, "produced value ceiling exceeded")
				break
			}
			processed = append(processed, resource)
		}
		if outcomes[index].sourceID != "" {
			continue
		}
		outcomes[index] = SourceOutcome{sourceID: sourceID, resources: processed}
	}
	return outcomes
}

func accountOperatorResource(accountant *limits.Accountant, resource ResourceOutcome) error {
	if accountant == nil || resource.state == ResourceRejected {
		return nil
	}
	var projected v1alpha1.KubeseerResourceResult
	var err error
	if resource.state == ResourceAccepted {
		projected, err = typedoutput.ProjectResourceResult(resource.provenance, resource.fields)
	} else if resource.state == ResourceUnsuccessful {
		projected, err = typedoutput.ProjectResourceResult(resource.provenance, nil)
		if err == nil {
			projected.Error = publicOperatorResultError(resource.failure)
		}
	}
	if err != nil {
		return err
	}
	return accountant.Add(projected)
}

func evaluateResource(ctx context.Context, plan SourcePlan, input typedoutput.ResourceOutcome) (ResourceOutcome, *OperatorError) {
	provenance := input.Provenance()
	if provenance.APIVersion == "" || provenance.Kind == "" || provenance.Name == "" || provenance.UID == "" {
		failure := NewOperatorError(plan.sourceID, "", -1, "", &provenance, ReasonInvalidInput, "resource provenance is invalid")
		return unsuccessfulResource(provenance, failure), failure
	}

	working := input.Fields()
	byName := make(map[string]int, len(working))
	for index, field := range working {
		if _, found := byName[field.Name()]; found {
			failure := NewOperatorError(plan.sourceID, field.Name(), -1, "", &provenance, ReasonInvalidInput, "typed field identity is duplicated")
			return unsuccessfulResource(provenance, failure), failure
		}
		byName[field.Name()] = index
	}

	for _, fieldPlan := range plan.fields {
		if cause := ctx.Err(); cause != nil {
			failure := interruptedOperatorError(plan.sourceID, fieldPlan.name, firstOperator(fieldPlan), firstOperatorName(fieldPlan), &provenance, cause)
			return ResourceOutcome{}, failure
		}
		fieldIndex, found := byName[fieldPlan.name]
		if !found {
			failure := NewOperatorError(plan.sourceID, fieldPlan.name, firstOperator(fieldPlan), firstOperatorName(fieldPlan), &provenance, ReasonInvalidInput, "typed field is missing from the resource")
			return unsuccessfulResource(provenance, failure), failure
		}
		field := working[fieldIndex]
		if field.Type() != fieldPlan.typeName {
			failure := NewOperatorError(plan.sourceID, fieldPlan.name, firstOperator(fieldPlan), firstOperatorName(fieldPlan), &provenance, ReasonInvalidInput, "typed field type does not match the operator plan")
			return unsuccessfulResource(provenance, failure), failure
		}
		if field.State() == typedoutput.FieldStateError && len(fieldPlan.operators) != 0 {
			failure := NewOperatorError(plan.sourceID, fieldPlan.name, firstOperator(fieldPlan), firstOperatorName(fieldPlan), &provenance, ReasonInvalidInput, "operator field outcome is unsuccessful")
			return unsuccessfulResource(provenance, failure), failure
		}

		for _, operator := range fieldPlan.operators {
			if cause := ctx.Err(); cause != nil {
				failure := interruptedOperatorError(plan.sourceID, fieldPlan.name, operator.index, string(operator.kind), &provenance, cause)
				return ResourceOutcome{}, failure
			}
			updated, accepted, failure := applyOperator(field, fieldPlan, operator, provenance)
			if failure != nil {
				return unsuccessfulResource(provenance, failure), failure
			}
			if !accepted {
				return ResourceOutcome{state: ResourceRejected, provenance: provenance}, nil
			}
			field = updated
			working[fieldIndex] = field
		}
	}

	return ResourceOutcome{state: ResourceAccepted, provenance: provenance, fields: working}, nil
}

func applyOperator(field typedoutput.FieldOutcome, fieldPlan FieldPlan, operator OperatorPlan, provenance selection.Provenance) (typedoutput.FieldOutcome, bool, *OperatorError) {
	switch operator.kind {
	case v1alpha1.OperatorDefault:
		fallback, ok := operator.Operand()
		if !ok {
			return field, false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "operator plan has no configured operand", nil)
		}
		matches := field.Matches()
		if field.State() == typedoutput.FieldStateAbsent {
			matches = []typedoutput.Match{fallback}
		} else {
			for index, match := range matches {
				if match.IsNull() {
					matches[index] = fallback
				}
			}
		}
		updated, err := typedoutput.ReplaceFieldMatches(field, matches)
		if err != nil {
			return field, false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "operator transformation could not replace the field", err)
		}
		return updated, true, nil
	case v1alpha1.OperatorCoalesce:
		if field.State() == typedoutput.FieldStateAbsent {
			return field, true, nil
		}
		matches := field.Matches()
		for _, match := range matches {
			if !match.IsNull() {
				updated, err := typedoutput.ReplaceFieldMatches(field, []typedoutput.Match{match})
				if err != nil {
					return field, false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "operator transformation could not replace the field", err)
				}
				return updated, true, nil
			}
		}
		null, err := typedoutput.NullMatch(fieldPlan.sourceID, fieldPlan.name, fieldPlan.typeName)
		if err != nil {
			return field, false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "operator transformation could not create a null match", err)
		}
		updated, replaceErr := typedoutput.ReplaceFieldMatches(field, []typedoutput.Match{null})
		if replaceErr != nil {
			return field, false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "operator transformation could not replace the field", replaceErr)
		}
		return updated, true, nil
	case v1alpha1.OperatorExists:
		return field, len(field.Matches()) > 0, nil
	case v1alpha1.OperatorNotExists:
		return field, len(field.Matches()) == 0, nil
	case v1alpha1.OperatorEq, v1alpha1.OperatorNe, v1alpha1.OperatorGt, v1alpha1.OperatorGte, v1alpha1.OperatorLt, v1alpha1.OperatorLte:
		operand, ok := operator.Operand()
		if !ok {
			return field, false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "operator plan has no configured operand", nil)
		}
		decision, failure := comparePredicate(field, operator.kind, operand, fieldPlan, operator, provenance)
		return field, decision, failure
	case v1alpha1.OperatorContains, v1alpha1.OperatorStartsWith, v1alpha1.OperatorEndsWith, v1alpha1.OperatorMatches:
		operand, ok := operator.Operand()
		if !ok {
			return field, false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "operator plan has no configured operand", nil)
		}
		text, _ := operand.StringValue()
		for _, match := range field.Matches() {
			value, ok := match.StringValue()
			if !ok {
				continue
			}
			switch operator.kind {
			case v1alpha1.OperatorContains:
				if strings.Contains(value, text) {
					return field, true, nil
				}
			case v1alpha1.OperatorStartsWith:
				if strings.HasPrefix(value, text) {
					return field, true, nil
				}
			case v1alpha1.OperatorEndsWith:
				if strings.HasSuffix(value, text) {
					return field, true, nil
				}
			case v1alpha1.OperatorMatches:
				if operator.pattern != nil && operator.pattern.MatchString(value) {
					return field, true, nil
				}
			}
		}
		return field, false, nil
	case v1alpha1.OperatorIn, v1alpha1.OperatorNotIn:
		operands := operator.Operands()
		decision, failure := membershipPredicate(field, operator.kind, operands, fieldPlan, operator, provenance)
		return field, decision, failure
	default:
		return field, false, evaluationError(fieldPlan, operator, provenance, ReasonUnsupportedOperator, "operator is not supported", nil)
	}
}

func comparePredicate(field typedoutput.FieldOutcome, kind v1alpha1.KubeseerOperatorName, operand typedoutput.Match, fieldPlan FieldPlan, operator OperatorPlan, provenance selection.Provenance) (bool, *OperatorError) {
	values := field.Matches()
	nonNull := 0
	for _, match := range values {
		if match.IsNull() {
			continue
		}
		nonNull++
		var decision bool
		var err error
		if kind == v1alpha1.OperatorEq || kind == v1alpha1.OperatorNe {
			decision, err = typedoutput.EqualMatches(match, operand)
			if err != nil {
				return false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "typed comparison is invalid", err)
			}
			if kind == v1alpha1.OperatorNe {
				if decision {
					return false, nil
				}
				continue
			}
			if decision {
				return true, nil
			}
			continue
		}
		comparison, compareErr := typedoutput.CompareMatches(match, operand)
		if compareErr != nil {
			return false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "typed comparison is invalid", compareErr)
		}
		switch kind {
		case v1alpha1.OperatorGt:
			decision = comparison > 0
		case v1alpha1.OperatorGte:
			decision = comparison >= 0
		case v1alpha1.OperatorLt:
			decision = comparison < 0
		case v1alpha1.OperatorLte:
			decision = comparison <= 0
		}
		if decision {
			return true, nil
		}
	}
	if kind == v1alpha1.OperatorNe {
		return nonNull > 0, nil
	}
	return false, nil
}

func membershipPredicate(field typedoutput.FieldOutcome, kind v1alpha1.KubeseerOperatorName, operands []typedoutput.Match, fieldPlan FieldPlan, operator OperatorPlan, provenance selection.Provenance) (bool, *OperatorError) {
	nonNull := 0
	for _, match := range field.Matches() {
		if match.IsNull() {
			continue
		}
		nonNull++
		equal := false
		for _, operand := range operands {
			var err error
			equal, err = typedoutput.EqualMatches(match, operand)
			if err != nil {
				return false, evaluationError(fieldPlan, operator, provenance, ReasonInvalidInput, "typed membership comparison is invalid", err)
			}
			if equal {
				break
			}
		}
		if kind == v1alpha1.OperatorIn && equal {
			return true, nil
		}
		if kind == v1alpha1.OperatorNotIn && equal {
			return false, nil
		}
	}
	if kind == v1alpha1.OperatorNotIn {
		return nonNull > 0, nil
	}
	return false, nil
}

func firstOperator(field FieldPlan) int {
	if len(field.operators) == 0 {
		return -1
	}
	return field.operators[0].index
}

func firstOperatorName(field FieldPlan) string {
	if len(field.operators) == 0 {
		return ""
	}
	return string(field.operators[0].kind)
}

func unsuccessfulResource(provenance selection.Provenance, failure *OperatorError) ResourceOutcome {
	return ResourceOutcome{state: ResourceUnsuccessful, provenance: provenance, failure: failure}
}

func evaluationError(field FieldPlan, operator OperatorPlan, provenance selection.Provenance, reason Reason, message string, cause error) *OperatorError {
	return operatorErrorWithCause(field.sourceID, field.name, operator.index, string(operator.kind), &provenance, reason, message, cause)
}

func interruptedOperatorError(sourceID, fieldName string, operatorIndex int, operatorName string, provenance *selection.Provenance, cause error) *OperatorError {
	return operatorErrorWithCause(sourceID, fieldName, operatorIndex, operatorName, provenance, ReasonOperatorInterrupted, "operator evaluation interrupted", cause)
}

func interruptedSourceOutcome(sourceID string, cause error) SourceOutcome {
	if cause == nil {
		cause = context.Canceled
	}
	return SourceOutcome{
		sourceID: sourceID,
		err:      operatorErrorWithCause(sourceID, "", -1, "", nil, ReasonOperatorInterrupted, "operator evaluation interrupted", cause),
	}
}

var _ error = (*OperatorError)(nil)
