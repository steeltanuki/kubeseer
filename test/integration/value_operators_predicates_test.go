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
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/operators"
)

func assertValueOperatorPredicateScenarios(t *testing.T) {
	t.Helper()

	t.Run("string predicates are case-sensitive and existential", func(t *testing.T) {
		cases := []struct {
			name string
			op   v1alpha1.KubeseerOperatorName
			arg  string
			want operators.ResourceState
		}{
			{name: "contains", op: v1alpha1.OperatorContains, arg: "World", want: operators.ResourceAccepted},
			{name: "startsWith", op: v1alpha1.OperatorStartsWith, arg: "Hello", want: operators.ResourceAccepted},
			{name: "endsWith", op: v1alpha1.OperatorEndsWith, arg: "World", want: operators.ResourceAccepted},
			{name: "matches", op: v1alpha1.OperatorMatches, arg: `^Hello(World)?$`, want: operators.ResourceAccepted},
			{name: "case mismatch", op: v1alpha1.OperatorContains, arg: "world", want: operators.ResourceRejected},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				outcome := evaluateValueOperator(t, "predicate-"+test.name, v1alpha1.ValueTypeString, "HelloWorld", "{.data.value}", &v1alpha1.KubeseerOperator{Operator: test.op, Value: stringOperand(test.arg)})
				assertSingleResourceState(t, outcome, test.want)
			})
		}
		multi := evaluateValueOperator(t, "predicate-multi", v1alpha1.ValueTypeString, []any{"other", nil, "HelloWorld"}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorContains, Value: stringOperand("World")})
		assertSingleResourceState(t, multi, operators.ResourceAccepted)
		if len(multi.Resources()[0].Fields()[0].Matches()) != 3 {
			t.Fatalf("string predicate changed multi-match cardinality: %#v", multi.Resources()[0].Fields())
		}
	})

	t.Run("presence counts null and empty values but distinguishes absence", func(t *testing.T) {
		presentCases := []struct {
			name     string
			typeName v1alpha1.KubeseerValueType
			value    any
		}{
			{name: "explicit null", typeName: v1alpha1.ValueTypeString, value: nil},
			{name: "empty string", typeName: v1alpha1.ValueTypeString, value: ""},
			{name: "empty object", typeName: v1alpha1.ValueTypeObject, value: map[string]any{}},
			{name: "empty list", typeName: v1alpha1.ValueTypeList, value: []any{}},
		}
		for _, test := range presentCases {
			t.Run(test.name, func(t *testing.T) {
				outcome := evaluateValueOperator(t, "present-"+test.name, test.typeName, test.value, "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorExists})
				assertSingleResourceState(t, outcome, operators.ResourceAccepted)
			})
		}
		absentExists := evaluateValueOperator(t, "absent-exists", v1alpha1.ValueTypeString, nil, "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorExists})
		assertSingleResourceState(t, absentExists, operators.ResourceRejected)
		absentNotExists := evaluateValueOperator(t, "absent-not-exists", v1alpha1.ValueTypeString, nil, "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNotExists})
		assertSingleResourceState(t, absentNotExists, operators.ResourceAccepted)
		presentNotExists := evaluateValueOperator(t, "present-not-exists", v1alpha1.ValueTypeString, "", "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNotExists})
		assertSingleResourceState(t, presentNotExists, operators.ResourceRejected)
	})

	t.Run("membership uses typed equality, duplicate members, and non-empty negative sets", func(t *testing.T) {
		in := evaluateValueOperator(t, "membership-in", v1alpha1.ValueTypeString, []any{"other", nil, "target"}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorIn, Values: []v1alpha1.KubeseerOperatorOperand{stringOperandValue("target"), stringOperandValue("target")}})
		assertSingleResourceState(t, in, operators.ResourceAccepted)
		notIn := evaluateValueOperator(t, "membership-not-in", v1alpha1.ValueTypeInteger, []any{int64(1), int64(2)}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNotIn, Values: []v1alpha1.KubeseerOperatorOperand{*integerOperand(3), *integerOperand(3)}})
		assertSingleResourceState(t, notIn, operators.ResourceAccepted)
		notInEqual := evaluateValueOperator(t, "membership-not-in-equal", v1alpha1.ValueTypeInteger, []any{int64(1), int64(2)}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNotIn, Values: []v1alpha1.KubeseerOperatorOperand{*integerOperand(2)}})
		assertSingleResourceState(t, notInEqual, operators.ResourceRejected)
		noMatch := evaluateValueOperator(t, "membership-no-match", v1alpha1.ValueTypeString, []any{"other", nil}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorIn, Values: []v1alpha1.KubeseerOperatorOperand{stringOperandValue("target")}})
		assertSingleResourceState(t, noMatch, operators.ResourceRejected)
		allNull := evaluateValueOperator(t, "membership-all-null", v1alpha1.ValueTypeString, []any{nil, nil}, "{.data.values[*]}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNotIn, Values: []v1alpha1.KubeseerOperatorOperand{stringOperandValue("target")}})
		assertSingleResourceState(t, allNull, operators.ResourceRejected)
		absent := evaluateValueOperator(t, "membership-absent", v1alpha1.ValueTypeString, nil, "{.data.value}", &v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorNotIn, Values: []v1alpha1.KubeseerOperatorOperand{stringOperandValue("target")}})
		assertSingleResourceState(t, absent, operators.ResourceRejected)
	})
}
