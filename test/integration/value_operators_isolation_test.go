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
	"errors"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertValueOperatorIsolationScenarios(t *testing.T) {
	t.Helper()

	t.Run("source and resource failures remain isolated", func(t *testing.T) {
		firstSource := operatorSource("isolation-first", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}})
		secondSource := operatorSource("isolation-second", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}})
		firstResource := selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": "first"}})
		firstResource.Provenance.Name, firstResource.Provenance.UID = "first-resource", "uid-first-resource"
		secondResource := selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": "second"}})
		secondResource.Provenance.Name, secondResource.Provenance.UID = "second-resource", "uid-second-resource"
		firstOutcome, firstTyped := evaluateOperatorSource(t, firstSource, []selection.SelectedResource{firstResource})
		secondOutcome, secondTyped := evaluateOperatorSource(t, secondSource, []selection.SelectedResource{secondResource})
		plans := operators.CompileBatch([]v1alpha1.KubeseerSource{firstSource, secondSource})
		batch := operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: plans[0], Typed: firstTyped}, {Plan: plans[1], Typed: secondTyped}})
		if len(batch) != 2 || batch[0].Err() != nil || batch[1].Err() != nil || batch[0].Resources()[0].Provenance().Name != "first-resource" || batch[1].Resources()[0].Provenance().Name != "second-resource" {
			t.Fatalf("independent source outcomes = %#v first=%#v second=%#v", batch, firstOutcome, secondOutcome)
		}

		upstreamErr := selection.NewSelectionError("upstream", selection.ReasonReadUnavailable, "source unavailable")
		upstreamSource := operatorSource("upstream", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}})
		upstreamTyped := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: upstreamSource, Extraction: extraction.SourceOutcome{SourceID: upstreamSource.ID, Err: upstreamErr}}})[0]
		upstream := operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: operators.CompileSource(upstreamSource), Typed: upstreamTyped}})[0]
		if upstream.Err() != upstreamErr || !errors.Is(upstream.Err(), upstreamErr) || len(upstream.Resources()) != 0 {
			t.Fatalf("upstream source error was not preserved: %#v", upstream)
		}

		invalidSource := operatorSource("invalid-plan-isolated", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.KubeseerOperatorName("explode"), Value: stringOperand("TOP_SECRET_VALUE")}}})
		invalidTyped := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: invalidSource, Extraction: extraction.SourceOutcome{SourceID: invalidSource.ID}}})[0]
		invalid := operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: operators.CompileSource(invalidSource), Typed: invalidTyped}})[0]
		if invalid.Err() != nil || len(invalid.FieldErrors()) != 1 || invalid.FieldErrors()[0].Reason != operators.ReasonUnsupportedOperator || strings.Contains(invalid.FieldErrors()[0].Error(), "TOP_SECRET_VALUE") {
			t.Fatalf("invalid plan outcome = %#v", invalid)
		}
	})

	t.Run("field and type mismatches fail one resource without discarding siblings", func(t *testing.T) {
		declaration := operatorSource("field-mismatch", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}})
		integerDeclaration := operatorSource("field-mismatch", v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger})
		resource := selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": int64(1)}})
		extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{Source: integerDeclaration, Selection: selection.SelectionOutcome{SourceID: integerDeclaration.ID, Resources: []selection.SelectedResource{resource}}}})[0]
		typed := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: integerDeclaration, Extraction: extracted}})[0]
		result := operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: operators.CompileSource(declaration), Typed: typed}})[0]
		if len(result.Resources()) != 1 || result.Resources()[0].State() != operators.ResourceUnsuccessful || !operators.HasReason(result.Resources()[0].Failure(), operators.ReasonInvalidInput) || len(result.Resources()[0].Fields()) != 0 {
			t.Fatalf("type mismatch outcome = %#v", result.Resources())
		}
	})

	t.Run("completed sources survive cancellation while active and later sources interrupt", func(t *testing.T) {
		sources := make([]v1alpha1.KubeseerSource, 3)
		typed := make([]typedoutput.SourceOutcome, 3)
		plans := make([]operators.PlanOutcome, 3)
		for index := range sources {
			sources[index] = operatorSource("cancel-"+string(rune('a'+index)), v1alpha1.KubeseerField{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}})
			_, typed[index] = evaluateOperatorSource(t, sources[index], []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": "value"}})})
			plans[index] = operators.CompileSource(sources[index])
		}
		ctx := &cancelAfterContext{cancelAfter: 4}
		outcomes := operators.EvaluateBatch(ctx, []operators.SourceInput{{Plan: plans[0], Typed: typed[0]}, {Plan: plans[1], Typed: typed[1]}, {Plan: plans[2], Typed: typed[2]}})
		if len(outcomes) != 3 || outcomes[0].Err() != nil || len(outcomes[0].Resources()) != 1 || !operators.HasReason(outcomes[1].Err(), operators.ReasonOperatorInterrupted) || !operators.HasReason(outcomes[2].Err(), operators.ReasonOperatorInterrupted) {
			t.Fatalf("cancellation partition = %#v", outcomes)
		}
	})
}
