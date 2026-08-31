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
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

// CompileSource validates every operator declaration before any typed
// resource is evaluated. Field declarations are copied and sorted; the API
// object and its operand pointers are never retained by the plan.
func CompileSource(source v1alpha1.KubeseerSource) PlanOutcome {
	fields := append([]v1alpha1.KubeseerField(nil), source.Fields...)
	sort.SliceStable(fields, func(left, right int) bool {
		return fields[left].Name < fields[right].Name
	})

	failures := make([]*OperatorError, 0)
	if source.ID == "" {
		failures = append(failures, planningError(source.ID, "", -1, "", ReasonInvalidInput, "source identity is invalid", nil))
	}

	plans := make([]FieldPlan, 0, len(fields))
	seenNames := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, found := seenNames[field.Name]; found {
			failures = append(failures, planningError(source.ID, field.Name, -1, "", ReasonInvalidInput, "field identity is duplicated", nil))
			continue
		}
		seenNames[field.Name] = struct{}{}

		fieldFailures := make([]*OperatorError, 0)
		if field.Name == "" {
			fieldFailures = append(fieldFailures, planningError(source.ID, field.Name, -1, "", ReasonInvalidInput, "field identity is invalid", nil))
		}
		if field.Type == "" && len(field.Operators) == 0 {
			fieldFailures = append(fieldFailures, planningError(source.ID, field.Name, -1, "", ReasonInvalidOperand, "field type is required", nil))
		}
		if field.Type != "" && !typedoutput.IsSupportedType(field.Type) {
			fieldFailures = append(fieldFailures, planningError(source.ID, field.Name, -1, "", ReasonIncompatibleOperator, "field type is unsupported", nil))
		}
		if field.Type != "" && !typedoutput.IsSupportedType(field.Type) {
			failures = append(failures, fieldFailures...)
			continue
		}

		operators := make([]OperatorPlan, 0, len(field.Operators))
		for index, declaration := range field.Operators {
			planned, err := planOperator(source.ID, field, index, declaration)
			if err != nil {
				fieldFailures = append(fieldFailures, err)
				continue
			}
			operators = append(operators, planned)
		}
		failures = append(failures, fieldFailures...)
		if len(fieldFailures) != 0 {
			continue
		}
		plans = append(plans, FieldPlan{
			sourceID:  source.ID,
			name:      field.Name,
			typeName:  field.Type,
			operators: operators,
		})
	}

	return PlanOutcome{
		sourceID: source.ID,
		plan: SourcePlan{
			sourceID: source.ID,
			fields:   plans,
		},
		failures: failures,
	}
}

// CompileBatch compiles sources independently and preserves declaration
// order. There is intentionally no plan cache: each call observes current
// declarations and recompiles deterministically.
func CompileBatch(sources []v1alpha1.KubeseerSource) []PlanOutcome {
	if sources == nil {
		return nil
	}
	outcomes := make([]PlanOutcome, len(sources))
	for index, source := range sources {
		outcomes[index] = CompileSource(source)
	}
	return outcomes
}

func planOperator(sourceID string, field v1alpha1.KubeseerField, index int, declaration v1alpha1.KubeseerOperator) (OperatorPlan, *OperatorError) {
	name := declaration.Operator
	if !supportedOperator(name) {
		return OperatorPlan{}, planningError(sourceID, field.Name, index, string(name), ReasonUnsupportedOperator, "operator is not supported", nil)
	}
	if field.Type == "" {
		return OperatorPlan{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidOperand, "field type is required before operand preparation", nil)
	}
	if !compatibleOperator(name, field.Type) {
		return OperatorPlan{}, planningError(sourceID, field.Name, index, string(name), ReasonIncompatibleOperator, "operator is incompatible with field type", nil)
	}

	if requiresSingleValue(name) {
		if declaration.Value == nil || len(declaration.Values) != 0 {
			return OperatorPlan{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidArity, "operator requires exactly one value operand", nil)
		}
		operand, err := decodeOperand(sourceID, field, index, name, *declaration.Value)
		if err != nil {
			return OperatorPlan{}, err
		}
		planned := OperatorPlan{index: index, kind: name, hasOperand: true, operand: operand}
		if name == v1alpha1.OperatorMatches {
			pattern, ok := operand.StringValue()
			if !ok {
				return OperatorPlan{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidOperand, "operator operand does not match the field type", nil)
			}
			compiled, compileErr := regexp.Compile(pattern)
			if compileErr != nil {
				return OperatorPlan{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidPattern, "regular expression is invalid", compileErr)
			}
			planned.pattern = compiled
		}
		return planned, nil
	}
	if requiresManyValues(name) {
		if declaration.Value != nil || len(declaration.Values) == 0 {
			return OperatorPlan{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidArity, "operator requires a non-empty values operand list", nil)
		}
		operands := make([]typedoutput.Match, 0, len(declaration.Values))
		for _, configured := range declaration.Values {
			operand, err := decodeOperand(sourceID, field, index, name, configured)
			if err != nil {
				return OperatorPlan{}, err
			}
			operands = append(operands, operand)
		}
		return OperatorPlan{index: index, kind: name, operands: operands}, nil
	}
	if declaration.Value != nil || len(declaration.Values) != 0 {
		return OperatorPlan{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidArity, "operator does not accept operands", nil)
	}
	return OperatorPlan{index: index, kind: name}, nil
}

func decodeOperand(sourceID string, field v1alpha1.KubeseerField, index int, name v1alpha1.KubeseerOperatorName, operand v1alpha1.KubeseerOperatorOperand) (typedoutput.Match, *OperatorError) {
	if operand.State != v1alpha1.MatchStateValue {
		return typedoutput.Match{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidOperand, "operand state is invalid", nil)
	}

	branches := []struct {
		name  string
		value any
		match func() bool
	}{
		{name: "stringValue", value: pointerValue(operand.StringValue), match: func() bool { return operand.StringValue != nil }},
		{name: "integerValue", value: pointerValue(operand.IntegerValue), match: func() bool { return operand.IntegerValue != nil }},
		{name: "numberValue", value: pointerValue(operand.NumberValue), match: func() bool { return operand.NumberValue != nil }},
		{name: "booleanValue", value: pointerValue(operand.BooleanValue), match: func() bool { return operand.BooleanValue != nil }},
		{name: "timestampValue", value: pointerValue(operand.TimestampValue), match: func() bool { return operand.TimestampValue != nil }},
		{name: "durationValue", value: pointerValue(operand.DurationValue), match: func() bool { return operand.DurationValue != nil }},
		{name: "quantityValue", value: pointerValue(operand.QuantityValue), match: func() bool { return operand.QuantityValue != nil }},
		{name: "objectValue", value: pointerValue(operand.ObjectValue), match: func() bool { return operand.ObjectValue != nil }},
		{name: "listValue", value: pointerValue(operand.ListValue), match: func() bool { return operand.ListValue != nil }},
	}
	selected := -1
	for branchIndex, branch := range branches {
		if !branch.match() {
			continue
		}
		if selected >= 0 {
			return typedoutput.Match{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidOperand, "operand must contain exactly one typed branch", nil)
		}
		selected = branchIndex
	}
	if selected < 0 {
		return typedoutput.Match{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidOperand, "operand must contain one typed branch", nil)
	}

	branch := branches[selected]
	if operandBranchType(branch.name) != field.Type {
		return typedoutput.Match{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidOperand, "operand branch does not match the field type", nil)
	}
	native := branch.value
	if branch.name == "objectValue" || branch.name == "listValue" {
		decoded, ok := decodeJSONOperand(native.(string), branch.name == "objectValue")
		if !ok {
			return typedoutput.Match{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidOperand, "composite operand is invalid", nil)
		}
		native = decoded
	}
	converted, conversionErr := typedoutput.ConvertConfiguredMatch(sourceID, field.Name, field.Type, native)
	if conversionErr != nil {
		return typedoutput.Match{}, planningError(sourceID, field.Name, index, string(name), ReasonInvalidOperand, "operand cannot be converted to the field type", conversionErr)
	}
	return converted, nil
}

func pointerValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func operandBranchType(branch string) v1alpha1.KubeseerValueType {
	switch branch {
	case "stringValue":
		return v1alpha1.ValueTypeString
	case "integerValue":
		return v1alpha1.ValueTypeInteger
	case "numberValue":
		return v1alpha1.ValueTypeNumber
	case "booleanValue":
		return v1alpha1.ValueTypeBoolean
	case "timestampValue":
		return v1alpha1.ValueTypeTimestamp
	case "durationValue":
		return v1alpha1.ValueTypeDuration
	case "quantityValue":
		return v1alpha1.ValueTypeQuantity
	case "objectValue":
		return v1alpha1.ValueTypeObject
	case "listValue":
		return v1alpha1.ValueTypeList
	default:
		return ""
	}
}

func decodeJSONOperand(text string, object bool) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, false
	}
	if object {
		_, ok := value.(map[string]any)
		return value, ok
	}
	_, ok := value.([]any)
	return value, ok
}

func supportedOperator(name v1alpha1.KubeseerOperatorName) bool {
	switch name {
	case v1alpha1.OperatorEq, v1alpha1.OperatorNe,
		v1alpha1.OperatorGt, v1alpha1.OperatorGte,
		v1alpha1.OperatorLt, v1alpha1.OperatorLte,
		v1alpha1.OperatorContains, v1alpha1.OperatorStartsWith,
		v1alpha1.OperatorEndsWith, v1alpha1.OperatorMatches,
		v1alpha1.OperatorExists, v1alpha1.OperatorNotExists,
		v1alpha1.OperatorIn, v1alpha1.OperatorNotIn,
		v1alpha1.OperatorDefault, v1alpha1.OperatorCoalesce:
		return true
	default:
		return false
	}
}

func compatibleOperator(name v1alpha1.KubeseerOperatorName, typeName v1alpha1.KubeseerValueType) bool {
	switch name {
	case v1alpha1.OperatorEq, v1alpha1.OperatorNe, v1alpha1.OperatorIn, v1alpha1.OperatorNotIn:
		return scalarEqualityType(typeName)
	case v1alpha1.OperatorGt, v1alpha1.OperatorGte, v1alpha1.OperatorLt, v1alpha1.OperatorLte:
		return orderableType(typeName)
	case v1alpha1.OperatorContains, v1alpha1.OperatorStartsWith, v1alpha1.OperatorEndsWith, v1alpha1.OperatorMatches:
		return typeName == v1alpha1.ValueTypeString
	case v1alpha1.OperatorExists, v1alpha1.OperatorNotExists, v1alpha1.OperatorDefault, v1alpha1.OperatorCoalesce:
		return typedoutput.IsSupportedType(typeName)
	default:
		return false
	}
}

func scalarEqualityType(typeName v1alpha1.KubeseerValueType) bool {
	switch typeName {
	case v1alpha1.ValueTypeString, v1alpha1.ValueTypeInteger, v1alpha1.ValueTypeNumber,
		v1alpha1.ValueTypeBoolean, v1alpha1.ValueTypeTimestamp, v1alpha1.ValueTypeDuration,
		v1alpha1.ValueTypeQuantity:
		return true
	default:
		return false
	}
}

func orderableType(typeName v1alpha1.KubeseerValueType) bool {
	switch typeName {
	case v1alpha1.ValueTypeInteger, v1alpha1.ValueTypeNumber, v1alpha1.ValueTypeTimestamp,
		v1alpha1.ValueTypeDuration, v1alpha1.ValueTypeQuantity:
		return true
	default:
		return false
	}
}

func requiresSingleValue(name v1alpha1.KubeseerOperatorName) bool {
	switch name {
	case v1alpha1.OperatorEq, v1alpha1.OperatorNe,
		v1alpha1.OperatorGt, v1alpha1.OperatorGte,
		v1alpha1.OperatorLt, v1alpha1.OperatorLte,
		v1alpha1.OperatorContains, v1alpha1.OperatorStartsWith,
		v1alpha1.OperatorEndsWith, v1alpha1.OperatorMatches,
		v1alpha1.OperatorDefault:
		return true
	default:
		return false
	}
}

func requiresManyValues(name v1alpha1.KubeseerOperatorName) bool {
	return name == v1alpha1.OperatorIn || name == v1alpha1.OperatorNotIn
}

func planningError(sourceID, fieldName string, index int, name string, reason Reason, message string, cause error) *OperatorError {
	return operatorErrorWithCause(sourceID, fieldName, index, name, nil, reason, message, cause)
}
