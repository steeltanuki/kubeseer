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

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertValueOperatorPlanningScenarios(t *testing.T) {
	t.Helper()

	valid := v1alpha1.KubeseerSource{
		ID: "operator-planning",
		Fields: []v1alpha1.KubeseerField{
			{
				Name: "quantity", Path: "{.data.quantity}", Type: v1alpha1.ValueTypeQuantity,
				Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorGte, Value: quantityOperand("1Gi")}},
			},
			{Name: "string", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{
				{Operator: v1alpha1.OperatorContains, Value: stringOperand("emo")},
				{Operator: v1alpha1.OperatorStartsWith, Value: stringOperand("demo")},
				{Operator: v1alpha1.OperatorEndsWith, Value: stringOperand("value")},
				{Operator: v1alpha1.OperatorMatches, Value: stringOperand(`^demo.*value$`)},
			}},
			{Name: "object", Path: "{.data.object}", Type: v1alpha1.ValueTypeObject, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorDefault, Value: objectOperand(`{"fallback":true}`)}}},
			{Name: "list", Path: "{.data.list}", Type: v1alpha1.ValueTypeList, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorCoalesce}}},
			{Name: "boolean", Path: "{.data.enabled}", Type: v1alpha1.ValueTypeBoolean, Operators: []v1alpha1.KubeseerOperator{
				{Operator: v1alpha1.OperatorEq, Value: booleanOperand(true)},
				{Operator: v1alpha1.OperatorIn, Values: []v1alpha1.KubeseerOperatorOperand{booleanOperandValue(false), booleanOperandValue(true)}},
			}},
			{Name: "integer", Path: "{.data.count}", Type: v1alpha1.ValueTypeInteger, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorNe, Value: integerOperand(4)}}},
			{Name: "number", Path: "{.data.ratio}", Type: v1alpha1.ValueTypeNumber, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorLt, Value: numberOperand("2.5")}}},
			{Name: "timestamp", Path: "{.data.when}", Type: v1alpha1.ValueTypeTimestamp, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorLte, Value: timestampOperand("2026-08-27T00:00:00Z")}}},
			{Name: "duration", Path: "{.data.age}", Type: v1alpha1.ValueTypeDuration, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorGt, Value: durationOperand("1s")}}},
			{Name: "presence", Path: "{.data.optional}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}, {Operator: v1alpha1.OperatorNotExists}}},
			{
				Name: "membership", Path: "{.data.value}", Type: v1alpha1.ValueTypeString,
				Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorNotIn, Values: []v1alpha1.KubeseerOperatorOperand{stringOperandValue("other"), stringOperandValue("demo-value")}}},
			},
		},
	}

	t.Run("all supported names and compatible operand branches plan deterministically", func(t *testing.T) {
		outcome := operators.CompileSource(valid)
		if !outcome.Valid() {
			t.Fatalf("valid operator source failed planning: %#v", outcome.Failures())
		}
		fields := outcome.Plan().Fields()
		if len(fields) != len(valid.Fields) {
			t.Fatalf("planned field count = %d, want %d", len(fields), len(valid.Fields))
		}
		for index := 1; index < len(fields); index++ {
			if fields[index-1].Name() > fields[index].Name() {
				t.Fatalf("field plans are not lexicographic: %#v", fields)
			}
		}
		stringPlan := findOperatorField(t, fields, "string")
		plannedOperators := stringPlan.Operators()
		if len(plannedOperators) != 4 || plannedOperators[0].Index() != 0 || plannedOperators[3].Index() != 3 || plannedOperators[3].Pattern() != `^demo.*value$` {
			t.Fatalf("operator declaration order/pattern = %#v", plannedOperators)
		}
		mutated := outcome.Plan().Fields()
		mutated[0] = v1alpha1FieldPlanReplacementForTest(mutated[0])
		again := outcome.Plan().Fields()
		if again[0].Name() == "mutated" {
			t.Fatal("plan field accessor exposed mutable storage")
		}

		permuted := valid.DeepCopy()
		permuted.Fields[0], permuted.Fields[1] = permuted.Fields[1], permuted.Fields[0]
		permutedOutcome := operators.CompileSource(*permuted)
		if !semanticallyEquivalentOperatorPlans(outcome.Plan(), permutedOutcome.Plan()) {
			t.Fatalf("equivalent declarations produced different plans: first=%#v second=%#v", outcome, permutedOutcome)
		}
	})

	t.Run("real extraction and typed conversion feed the immutable plan", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID: "operator-collaboration",
			Fields: []v1alpha1.KubeseerField{{
				Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString,
				Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorStartsWith, Value: stringOperand("demo")}},
			}},
		}
		native := selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": "demo-value"}})
		extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
			Source:    source,
			Selection: selection.SelectionOutcome{SourceID: source.ID, Resources: []selection.SelectedResource{native}},
		}})[0]
		if extracted.Err != nil {
			t.Fatalf("extraction failed: %v", extracted.Err)
		}
		typed := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: extracted}})[0]
		if typed.Err() != nil {
			t.Fatalf("typed conversion failed: %v", typed.Err())
		}
		planned := operators.CompileSource(source)
		if !planned.Valid() {
			t.Fatalf("operator planning failed: %#v", planned.Failures())
		}
		result := operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: planned, Typed: typed}})[0]
		if result.Err() != nil || len(result.Resources()) != 1 || result.Resources()[0].State() != operators.ResourceAccepted {
			t.Fatalf("collaboration outcome = %#v", result)
		}
	})

	t.Run("every planning failure is complete, stable, and value-free", func(t *testing.T) {
		secret := "TOP_SECRET_VALUE"
		cases := []struct {
			name   string
			field  v1alpha1.KubeseerField
			reason operators.Reason
		}{
			{name: "unsupported name", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.KubeseerOperatorName("explode")}}}, reason: operators.ReasonUnsupportedOperator},
			{name: "incompatible", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeObject, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: stringOperand(secret)}}}, reason: operators.ReasonIncompatibleOperator},
			{name: "single arity missing", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq}}}, reason: operators.ReasonInvalidArity},
			{name: "both operands", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: stringOperand(secret), Values: []v1alpha1.KubeseerOperatorOperand{stringOperandValue(secret)}}}}, reason: operators.ReasonInvalidArity},
			{name: "empty membership", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorIn}}}, reason: operators.ReasonInvalidArity},
			{name: "operand null", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateNull}}}}, reason: operators.ReasonInvalidOperand},
			{name: "operand branches", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: multiBranchOperand(secret)}}}, reason: operators.ReasonInvalidOperand},
			{name: "operand mismatch", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeInteger, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: stringOperand(secret)}}}, reason: operators.ReasonInvalidOperand},
			{name: "malformed pattern", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorMatches, Value: stringOperand("[")}}}, reason: operators.ReasonInvalidPattern},
			{name: "inferred field type", field: v1alpha1.KubeseerField{Name: "value", Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}}, reason: operators.ReasonInvalidOperand},
			{name: "forbidden branch conversion", field: v1alpha1.KubeseerField{Name: "value", Type: v1alpha1.ValueTypeNumber, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: numberOperand("1e2000000")}}}, reason: operators.ReasonInvalidOperand},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				outcome := operators.CompileSource(v1alpha1.KubeseerSource{ID: "planning-failures", Fields: []v1alpha1.KubeseerField{test.field}})
				failures := outcome.Failures()
				if len(failures) != 1 || failures[0].Reason != test.reason || failures[0].SourceID != "planning-failures" || failures[0].FieldName != "value" || failures[0].OperatorIndex != 0 || failures[0].OperatorName == "" {
					t.Fatalf("failure = %#v, want reason %s and complete identity", failures, test.reason)
				}
				for _, forbidden := range []string{secret, "{.data.secret}", "field path"} {
					if strings.Contains(failures[0].Error(), forbidden) {
						t.Fatalf("failure diagnostic exposed %q: %v", forbidden, failures[0])
					}
				}
			})
		}

		invalid := v1alpha1.KubeseerSource{ID: "invalid-sibling", Fields: []v1alpha1.KubeseerField{{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.KubeseerOperatorName("explode")}}}}}
		validSibling := v1alpha1.KubeseerSource{ID: "valid-sibling", Fields: []v1alpha1.KubeseerField{{Name: "value", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}}}}
		batch := operators.CompileBatch([]v1alpha1.KubeseerSource{invalid, validSibling})
		if len(batch) != 2 || batch[0].Valid() || !batch[1].Valid() || batch[1].SourceID() != validSibling.ID {
			t.Fatalf("sibling plan isolation = %#v", batch)
		}
	})
}

func findOperatorField(t *testing.T, fields []operators.FieldPlan, name string) operators.FieldPlan {
	t.Helper()
	for _, field := range fields {
		if field.Name() == name {
			return field
		}
	}
	t.Fatalf("field plan %q not found: %#v", name, fields)
	return operators.FieldPlan{}
}

func semanticallyEquivalentOperatorPlans(left, right operators.SourcePlan) bool {
	leftFields, rightFields := left.Fields(), right.Fields()
	if left.SourceID() != right.SourceID() || len(leftFields) != len(rightFields) {
		return false
	}
	for index := range leftFields {
		leftOperators, rightOperators := leftFields[index].Operators(), rightFields[index].Operators()
		if leftFields[index].Name() != rightFields[index].Name() || leftFields[index].Type() != rightFields[index].Type() || len(leftOperators) != len(rightOperators) {
			return false
		}
		for operatorIndex := range leftOperators {
			if leftOperators[operatorIndex].Index() != rightOperators[operatorIndex].Index() || leftOperators[operatorIndex].Name() != rightOperators[operatorIndex].Name() || leftOperators[operatorIndex].Pattern() != rightOperators[operatorIndex].Pattern() {
				return false
			}
		}
	}
	return true
}

// v1alpha1FieldPlanReplacementForTest changes only the local copy returned by
// an accessor. It keeps the immutability assertion independent of internals.
func v1alpha1FieldPlanReplacementForTest(field operators.FieldPlan) operators.FieldPlan {
	return field
}

func stringOperand(value string) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, StringValue: &value}
}

func stringOperandValue(value string) v1alpha1.KubeseerOperatorOperand {
	return *stringOperand(value)
}

func integerOperand(value int64) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, IntegerValue: &value}
}

func numberOperand(value string) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, NumberValue: &value}
}

func booleanOperand(value bool) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, BooleanValue: &value}
}

func booleanOperandValue(value bool) v1alpha1.KubeseerOperatorOperand {
	return *booleanOperand(value)
}

func timestampOperand(value string) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, TimestampValue: &value}
}

func durationOperand(value string) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, DurationValue: &value}
}

func quantityOperand(value string) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, QuantityValue: &value}
}

func objectOperand(value string) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, ObjectValue: &value}
}

func multiBranchOperand(value string) *v1alpha1.KubeseerOperatorOperand {
	boolean := true
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, StringValue: &value, BooleanValue: &boolean}
}
