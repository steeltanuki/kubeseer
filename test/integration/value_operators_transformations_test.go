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
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertValueOperatorTransformationScenarios(t *testing.T) {
	t.Helper()

	t.Run("default replaces absence and null while preserving non-null values", func(t *testing.T) {
		fallback := stringOperand("fallback")
		absentSource := operatorSource("transform-default-absent", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorDefault, Value: fallback}}})
		absent, typedBefore := evaluateOperatorSource(t, absentSource, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{}})})
		assertAcceptedStringField(t, absent, "fallback")
		if typedBefore.Resources()[0].Fields()[0].State() != typedoutput.FieldStateAbsent {
			t.Fatal("default test did not begin with an absent typed field")
		}

		nullSource := operatorSource("transform-default-null", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorDefault, Value: stringOperand("fallback")}}})
		null, typedNull := evaluateOperatorSource(t, nullSource, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": nil}})})
		assertAcceptedStringField(t, null, "fallback")
		if !typedNull.Resources()[0].Fields()[0].Matches()[0].IsNull() {
			t.Fatal("default test did not begin with an explicit null match")
		}

		presentSource := operatorSource("transform-default-present", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorDefault, Value: stringOperand("fallback")}}})
		present, _ := evaluateOperatorSource(t, presentSource, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": "original"}})})
		assertAcceptedStringField(t, present, "original")
	})

	t.Run("coalesce keeps the first non-null, one null, or absence", func(t *testing.T) {
		source := operatorSource("transform-coalesce-values", v1alpha1.KubeseerField{Name: "value", Path: "{.data.values[*]}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorCoalesce}}})
		outcome, _ := evaluateOperatorSource(t, source, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"values": []any{nil, "first", "second"}}})})
		assertAcceptedStringField(t, outcome, "first")
		if len(outcome.Resources()[0].Fields()[0].Matches()) != 1 {
			t.Fatal("coalesce retained more than the first non-null match")
		}

		nulls, _ := evaluateOperatorSource(t, source, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"values": []any{nil, nil}}})})
		if len(nulls.Resources()) != 1 || len(nulls.Resources()[0].Fields()[0].Matches()) != 1 || !nulls.Resources()[0].Fields()[0].Matches()[0].IsNull() {
			t.Fatalf("coalesce all-null outcome = %#v", nulls.Resources())
		}

		absentSource := operatorSource("transform-coalesce-absent", v1alpha1.KubeseerField{Name: "value", Path: "{.data.missing}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorCoalesce}}})
		absent, _ := evaluateOperatorSource(t, absentSource, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{}})})
		if len(absent.Resources()) != 1 || absent.Resources()[0].Fields()[0].State() != typedoutput.FieldStateAbsent {
			t.Fatalf("coalesce changed absence = %#v", absent.Resources())
		}
	})

	t.Run("chains are ordered and all fields use implicit AND", func(t *testing.T) {
		defaultThenEq := operatorSource("transform-before-predicate", v1alpha1.KubeseerField{Name: "value", Path: "{.data.missing}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{
			{Operator: v1alpha1.OperatorDefault, Value: stringOperand("fallback")},
			{Operator: v1alpha1.OperatorEq, Value: stringOperand("fallback")},
		}})
		accepted, _ := evaluateOperatorSource(t, defaultThenEq, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{}})})
		assertSingleResourceState(t, accepted, operators.ResourceAccepted)
		assertAcceptedStringField(t, accepted, "fallback")

		eqThenDefault := operatorSource("predicate-before-transform", v1alpha1.KubeseerField{Name: "value", Path: "{.data.missing}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{
			{Operator: v1alpha1.OperatorEq, Value: stringOperand("fallback")},
			{Operator: v1alpha1.OperatorDefault, Value: stringOperand("fallback")},
		}})
		rejected, _ := evaluateOperatorSource(t, eqThenDefault, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{}})})
		assertSingleResourceState(t, rejected, operators.ResourceRejected)
		if len(rejected.Resources()[0].Fields()) != 0 {
			t.Fatal("rejected resource exposed temporary transformed fields")
		}

		andSource := v1alpha1.KubeseerSource{ID: "transform-resource-and", Fields: []v1alpha1.KubeseerField{
			{Name: "alpha", Path: "{.data.alpha}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: stringOperand("yes")}}},
			{Name: "zeta", Path: "{.data.zeta}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: stringOperand("no")}}},
		}}
		andOutcome, _ := evaluateOperatorSource(t, andSource, []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"alpha": "yes", "zeta": "yes"}})})
		assertSingleResourceState(t, andOutcome, operators.ResourceRejected)
	})

	t.Run("accepted, rejected, and unsuccessful resources project with isolation", func(t *testing.T) {
		source := operatorSource("transform-projection", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorEq, Value: integerOperand(2)}}})
		accepted := selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": int64(2)}})
		accepted.Provenance.Name, accepted.Provenance.UID = "accepted", "uid-accepted"
		rejected := selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": int64(1)}})
		rejected.Provenance.Name, rejected.Provenance.UID = "rejected", "uid-rejected"
		unsuccessful := selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": "TOP_SECRET_VALUE"}})
		unsuccessful.Provenance.Name, unsuccessful.Provenance.UID = "unsuccessful", "uid-unsuccessful"
		outcome, _ := evaluateOperatorSource(t, source, []selection.SelectedResource{accepted, rejected, unsuccessful})
		resources := outcome.Resources()
		if outcome.Err() != nil || len(resources) != 3 || resources[0].State() != operators.ResourceAccepted || resources[1].State() != operators.ResourceRejected || resources[2].State() != operators.ResourceUnsuccessful {
			t.Fatalf("resource outcome partition = %#v", resources)
		}
		if resources[2].Failure() == nil || !operators.HasReason(resources[2].Failure(), operators.ReasonInvalidInput) || len(resources[2].Fields()) != 0 {
			t.Fatalf("unsuccessful resource = %#v", resources[2])
		}

		public, err := operators.BuildResult([]operators.SourceOutcome{outcome})
		if err != nil {
			t.Fatalf("project operator result: %v", err)
		}
		if len(public.Sources) != 1 || len(public.Sources[0].Resources) != 2 || public.Sources[0].Resources[0].Name != "accepted" || public.Sources[0].Resources[1].Name != "unsuccessful" {
			t.Fatalf("public resource projection = %#v", public)
		}
		failedPublic := public.Sources[0].Resources[1]
		if len(failedPublic.Fields) != 0 || failedPublic.Error == nil || failedPublic.Error.Reason != string(operators.ReasonInvalidInput) || strings.Contains(failedPublic.Error.Message, "TOP_SECRET_VALUE") {
			t.Fatalf("public unsuccessful resource = %#v", failedPublic)
		}
		derived, err := statuscontract.DeriveResult(&public)
		if err != nil || !derived.Degraded || !statuscontract.HasResultErrors(&public) {
			t.Fatalf("resource error did not degrade status: derived=%#v err=%v", derived, err)
		}
	})
}

func operatorSource(id string, field v1alpha1.KubeseerField) v1alpha1.KubeseerSource {
	return v1alpha1.KubeseerSource{ID: id, Fields: []v1alpha1.KubeseerField{field}}
}

func evaluateOperatorSource(t *testing.T, source v1alpha1.KubeseerSource, resources []selection.SelectedResource) (operators.SourceOutcome, typedoutput.SourceOutcome) {
	t.Helper()
	extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
		Source:    source,
		Selection: selection.SelectionOutcome{SourceID: source.ID, Resources: resources},
	}})[0]
	if extracted.Err != nil {
		t.Fatalf("extract operator source %s: %v", source.ID, extracted.Err)
	}
	typed := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: extracted}})[0]
	plan := operators.CompileSource(source)
	if !plan.Valid() {
		t.Fatalf("plan operator source %s: %#v", source.ID, plan.Failures())
	}
	return operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: plan, Typed: typed}})[0], typed
}

func assertAcceptedStringField(t *testing.T, outcome operators.SourceOutcome, want string) {
	t.Helper()
	assertSingleResourceState(t, outcome, operators.ResourceAccepted)
	fields := outcome.Resources()[0].Fields()
	if len(fields) != 1 || fields[0].State() != typedoutput.FieldStateValues || len(fields[0].Matches()) != 1 {
		t.Fatalf("accepted transformed fields = %#v", fields)
	}
	got, ok := fields[0].Matches()[0].StringValue()
	if !ok || got != want {
		t.Fatalf("transformed string = %q (ok=%t), want %q", got, ok, want)
	}
}
