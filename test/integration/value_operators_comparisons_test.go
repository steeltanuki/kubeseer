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

func assertValueOperatorComparisonScenarios(t *testing.T) {
	t.Helper()

	t.Run("logical scalar equality is exact across normalized representations", func(t *testing.T) {
		cases := []struct {
			name      string
			typeName  v1alpha1.KubeseerValueType
			observed  any
			operand   *v1alpha1.KubeseerOperatorOperand
			wantState operators.ResourceState
		}{
			{name: "string case-sensitive", typeName: v1alpha1.ValueTypeString, observed: "Demo", operand: stringOperand("Demo"), wantState: operators.ResourceAccepted},
			{name: "integer signed", typeName: v1alpha1.ValueTypeInteger, observed: int64(-7), operand: integerOperand(-7), wantState: operators.ResourceAccepted},
			{name: "number normalized decimal", typeName: v1alpha1.ValueTypeNumber, observed: "2.5000", operand: numberOperand("2.5"), wantState: operators.ResourceAccepted},
			{name: "boolean logical value", typeName: v1alpha1.ValueTypeBoolean, observed: true, operand: booleanOperand(true), wantState: operators.ResourceAccepted},
			{name: "timestamp instant", typeName: v1alpha1.ValueTypeTimestamp, observed: "2026-08-26T14:30:00+02:00", operand: timestampOperand("2026-08-26T12:30:00Z"), wantState: operators.ResourceAccepted},
			{name: "duration nanoseconds", typeName: v1alpha1.ValueTypeDuration, observed: "1.5s", operand: durationOperand("1500ms"), wantState: operators.ResourceAccepted},
			{name: "quantity magnitude", typeName: v1alpha1.ValueTypeQuantity, observed: "1Gi", operand: quantityOperand("1024Mi"), wantState: operators.ResourceAccepted},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				outcome := evaluateValueOperator(t, "eq", test.typeName, test.observed, "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorEq, Value: test.operand})
				assertSingleResourceState(t, outcome, test.wantState)
			})
		}

		unequal := evaluateValueOperator(t, "ne", v1alpha1.ValueTypeString, "Demo", "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNe, Value: stringOperand("demo")})
		assertSingleResourceState(t, unequal, operators.ResourceAccepted)
		equal := evaluateValueOperator(t, "ne-equal", v1alpha1.ValueTypeString, "Demo", "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNe, Value: stringOperand("Demo")})
		assertSingleResourceState(t, equal, operators.ResourceRejected)
	})

	t.Run("ordered predicates cover inclusive and exclusive boundaries", func(t *testing.T) {
		cases := []struct {
			name string
			op   v1alpha1.KubeseerOperatorName
			got  any
			arg  *v1alpha1.KubeseerOperatorOperand
		}{
			{name: "greater", op: v1alpha1.OperatorGt, got: int64(3), arg: integerOperand(2)},
			{name: "greater equal", op: v1alpha1.OperatorGte, got: int64(2), arg: integerOperand(2)},
			{name: "less", op: v1alpha1.OperatorLt, got: "1.25", arg: numberOperand("2.5")},
			{name: "less equal", op: v1alpha1.OperatorLte, got: "2026-08-26T12:30:00Z", arg: timestampOperand("2026-08-26T12:30:00Z")},
		}
		types := []v1alpha1.KubeseerValueType{v1alpha1.ValueTypeInteger, v1alpha1.ValueTypeInteger, v1alpha1.ValueTypeNumber, v1alpha1.ValueTypeTimestamp}
		for index, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				outcome := evaluateValueOperator(t, string(test.op), types[index], test.got, "{.data.value}", &v1alpha1.KubeseerOperator{Operator: test.op, Value: test.arg})
				assertSingleResourceState(t, outcome, operators.ResourceAccepted)
			})
		}
		boundary := evaluateValueOperator(t, "not-less", v1alpha1.ValueTypeDuration, "1s", "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorLt, Value: durationOperand("1s")})
		assertSingleResourceState(t, boundary, operators.ResourceRejected)
	})

	t.Run("multi-match and null cardinality use existential positive and non-empty negative semantics", func(t *testing.T) {
		positive := evaluateValueOperator(t, "multi-eq", v1alpha1.ValueTypeString, []any{"other", nil, "target"}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorEq, Value: stringOperand("target")})
		assertSingleResourceState(t, positive, operators.ResourceAccepted)
		resources := positive.Resources()
		if len(resources) != 1 || len(resources[0].Fields()) != 1 || len(resources[0].Fields()[0].Matches()) != 3 {
			t.Fatalf("positive comparison changed field cardinality/order: %#v", resources)
		}
		if value, ok := resources[0].Fields()[0].Matches()[0].StringValue(); !ok || value != "other" {
			t.Fatalf("positive comparison changed first match: %q (ok=%t)", value, ok)
		}
		negative := evaluateValueOperator(t, "multi-ne", v1alpha1.ValueTypeString, []any{"other", nil, "target"}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNe, Value: stringOperand("target")})
		assertSingleResourceState(t, negative, operators.ResourceRejected)
		noEqual := evaluateValueOperator(t, "multi-ne-no-equal", v1alpha1.ValueTypeString, []any{"other", nil}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNe, Value: stringOperand("target")})
		assertSingleResourceState(t, noEqual, operators.ResourceAccepted)
		absent := evaluateValueOperator(t, "absent-eq", v1alpha1.ValueTypeString, map[string]any{}, "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorEq, Value: stringOperand("target")})
		assertSingleResourceState(t, absent, operators.ResourceRejected)
		allNull := evaluateValueOperator(t, "null-eq", v1alpha1.ValueTypeString, []any{nil, nil}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorEq, Value: stringOperand("target")})
		assertSingleResourceState(t, allNull, operators.ResourceRejected)
		nullNe := evaluateValueOperator(t, "null-ne", v1alpha1.ValueTypeString, []any{nil}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNe, Value: stringOperand("target")})
		assertSingleResourceState(t, nullNe, operators.ResourceRejected)
	})
}

func evaluateValueOperator(t *testing.T, id string, typeName v1alpha1.KubeseerValueType, observed any, path string, operator *v1alpha1.KubeseerOperator) operators.SourceOutcome {
	t.Helper()
	fieldValue := observed
	if path == "{.data.value}" {
		if _, isObject := observed.(map[string]any); isObject {
			fieldValue = observed
		}
	}
	source := v1alpha1.KubeseerSource{ID: id, Fields: []v1alpha1.KubeseerField{{Name: "value", Path: path, Type: typeName, Operators: []v1alpha1.KubeseerOperator{*operator}}}}
	object := map[string]any{"data": map[string]any{"value": fieldValue}}
	if path != "{.data.value}" {
		object["data"].(map[string]any)["values"] = observed
	}
	if strings.HasPrefix(id, "absent-") {
		delete(object["data"].(map[string]any), "value")
	}
	selected := selectedExtractionResourceWithObject(object)
	extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{Source: source, Selection: selection.SelectionOutcome{SourceID: id, Resources: []selection.SelectedResource{selected}}}})[0]
	if extracted.Err != nil {
		t.Fatalf("extract %s: %v", id, extracted.Err)
	}
	typed := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: extracted}})[0]
	if typed.Err() != nil {
		t.Fatalf("convert %s: %v", id, typed.Err())
	}
	plan := operators.CompileSource(source)
	if !plan.Valid() {
		t.Fatalf("plan %s: %#v", id, plan.Failures())
	}
	return operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: plan, Typed: typed}})[0]
}

func assertSingleResourceState(t *testing.T, outcome operators.SourceOutcome, want operators.ResourceState) {
	t.Helper()
	if outcome.Err() != nil || len(outcome.Resources()) != 1 || outcome.Resources()[0].State() != want {
		t.Fatalf("operator outcome = source error %v resources %#v, want %s", outcome.Err(), outcome.Resources(), want)
	}
}
