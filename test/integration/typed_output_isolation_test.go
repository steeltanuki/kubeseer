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
	"reflect"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertTypedOutputIsolationScenarios(t *testing.T) {
	t.Helper()

	first := v1alpha1.KubeseerSource{
		ID: "isolation-first",
		Fields: []v1alpha1.KubeseerField{
			{Name: "good", Path: "{.data.good}", Type: v1alpha1.ValueTypeString},
			{Name: "bad", Path: "{.data.bad}", Type: v1alpha1.ValueTypeInteger},
		},
	}
	second := v1alpha1.KubeseerSource{ID: "isolation-second", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}}}
	firstExtraction := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
		Source: first,
		Selection: selection.SelectionOutcome{
			SourceID: first.ID,
			Resources: []selection.SelectedResource{
				selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"bad": "not-an-integer", "good": "kept-first"}}),
				selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"bad": int64(7), "good": "kept-second"}}),
			},
		},
	}})[0]
	secondExtraction := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
		Source: second,
		Selection: selection.SelectionOutcome{
			SourceID:  second.ID,
			Resources: []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": "independent"}})},
		},
	}})[0]

	inputs := []typedoutput.SourceInput{{Source: first, Extraction: firstExtraction}, {Source: second, Extraction: secondExtraction}}
	outcomes := typedoutput.ConvertBatch(context.Background(), inputs)
	if len(outcomes) != 2 || outcomes[0].Err() != nil || outcomes[1].Err() != nil || outcomes[0].SourceID() != first.ID || outcomes[1].SourceID() != second.ID {
		t.Fatalf("independent batch outcomes = %#v", outcomes)
	}
	firstResources := outcomes[0].Resources()
	if len(firstResources) != 2 || firstResources[0].Provenance().Name != "resource-a" || firstResources[1].Provenance().Name != "resource-a" {
		t.Fatalf("resource order/provenance = %#v", firstResources)
	}
	firstFields := firstResources[0].Fields()
	if len(firstFields) != 2 || firstFields[0].Name() != "bad" || firstFields[1].Name() != "good" {
		t.Fatalf("lexicographic field order = %#v", firstFields)
	}
	if firstFields[0].State() != typedoutput.FieldStateError || len(firstFields[0].Matches()) != 0 || !typedoutput.HasReason(firstFields[0].Err(), typedoutput.ReasonInvalidValue) {
		t.Fatalf("failed field outcome = %#v", firstFields[0])
	}
	if firstFields[1].State() != typedoutput.FieldStateValues || len(firstFields[1].Matches()) != 1 {
		t.Fatalf("sibling field was discarded = %#v", firstFields[1])
	}
	secondFields := firstResources[1].Fields()
	if secondFields[0].State() != typedoutput.FieldStateValues || len(secondFields[0].Matches()) != 1 {
		t.Fatalf("successful sibling resource was discarded = %#v", secondFields[0])
	}
	if len(outcomes[1].Resources()) != 1 || outcomes[1].Resources()[0].Fields()[0].Name() != "value" {
		t.Fatalf("independent source was discarded = %#v", outcomes[1])
	}

	t.Run("planning failures are recorded once and empty outcomes remain successful", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{ID: "planning-isolation", Fields: []v1alpha1.KubeseerField{
			{Name: "valid", Path: "{.data.value}", Type: v1alpha1.ValueTypeString},
			{Name: "missing", Path: "{.data.missing}"},
			{Name: "unsupported", Path: "{.data.unsupported}", Type: v1alpha1.KubeseerValueType("decimal")},
		}}
		extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
			Source:    source,
			Selection: selection.SelectionOutcome{SourceID: source.ID},
		}})[0]
		outcome := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: extracted}})[0]
		if outcome.Err() != nil || outcome.State() != typedoutput.SourceStateValues || len(outcome.Resources()) != 0 {
			t.Fatalf("empty planning outcome = %#v", outcome)
		}
		failures := outcome.FieldErrors()
		if len(failures) != 2 || failures[0].FieldName != "missing" || failures[1].FieldName != "unsupported" || failures[0].Reason != typedoutput.ReasonMissingType || failures[1].Reason != typedoutput.ReasonUnsupportedType {
			t.Fatalf("planning failures = %#v", failures)
		}

		emptyResourceSource := v1alpha1.KubeseerSource{ID: "empty-resource", Fields: []v1alpha1.KubeseerField{}}
		emptyResource := extraction.SourceOutcome{
			SourceID: emptyResourceSource.ID,
			Resources: []extraction.ResourceOutcome{{
				Provenance: selection.Provenance{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "empty", UID: "uid-empty"},
			}},
		}
		emptyOutcome := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: emptyResourceSource, Extraction: emptyResource}})[0]
		if emptyOutcome.Err() != nil || len(emptyOutcome.Resources()) != 1 || len(emptyOutcome.Resources()[0].Fields()) != 0 {
			t.Fatalf("successful empty resource outcome = %#v", emptyOutcome)
		}
	})

	t.Run("upstream errors and identity mismatches remain scoped", func(t *testing.T) {
		upstream := selection.NewSelectionError("upstream", selection.ReasonReadUnavailable, "resource read unavailable")
		upstreamInput := typedoutput.SourceInput{
			Source:     v1alpha1.KubeseerSource{ID: "upstream", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}}},
			Extraction: extraction.SourceOutcome{SourceID: "upstream", Err: upstream},
		}
		upstreamOutcome := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{upstreamInput})[0]
		if upstreamOutcome.Err() != upstream || !errors.Is(upstreamOutcome.Err(), upstream) || len(upstreamOutcome.Resources()) != 0 {
			t.Fatalf("upstream error was not preserved exactly = %#v", upstreamOutcome)
		}

		declaration := v1alpha1.KubeseerSource{ID: "declared", Fields: []v1alpha1.KubeseerField{{Name: "declared-field", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}}}
		mismatch := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{
			Source:     declaration,
			Extraction: extraction.SourceOutcome{SourceID: "different"},
		}})[0]
		if !typedoutput.HasReason(mismatch.Err(), typedoutput.ReasonInvalidInput) || mismatch.SourceID() != declaration.ID {
			t.Fatalf("source identity mismatch = %#v", mismatch)
		}

		fieldMismatch := extraction.SourceOutcome{
			SourceID: declaration.ID,
			Resources: []extraction.ResourceOutcome{{
				Provenance: selection.Provenance{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "mismatch", UID: "uid-mismatch"},
				Fields:     []extraction.FieldOutcome{{FieldName: "other-field"}},
			}},
		}
		fieldOutcome := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: declaration, Extraction: fieldMismatch}})[0]
		fields := fieldOutcome.Resources()[0].Fields()
		if len(fields) != 2 || fields[1].Name() != "other-field" || !typedoutput.HasReason(fields[1].Err(), typedoutput.ReasonInvalidInput) {
			t.Fatalf("field identity mismatch = %#v", fields)
		}
	})

	t.Run("equivalent inputs are deterministic and cancellation partitions sources", func(t *testing.T) {
		firstSource := v1alpha1.KubeseerSource{ID: "cancel-completed", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}}}
		laterSource := v1alpha1.KubeseerSource{ID: "cancel-unstarted", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}}}
		thirdSource := v1alpha1.KubeseerSource{ID: "cancel-also-unstarted", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}}}
		makeExtraction := func(source v1alpha1.KubeseerSource) extraction.SourceOutcome {
			return extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
				Source:    source,
				Selection: selection.SelectionOutcome{SourceID: source.ID, Resources: []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{"data": map[string]any{"value": source.ID}})}},
			}})[0]
		}
		batch := []typedoutput.SourceInput{{Source: firstSource, Extraction: makeExtraction(firstSource)}, {Source: laterSource, Extraction: makeExtraction(laterSource)}, {Source: thirdSource, Extraction: makeExtraction(thirdSource)}}
		left := typedoutput.ConvertBatch(context.Background(), batch)
		right := typedoutput.ConvertBatch(context.Background(), batch)
		leftResult, err := typedoutput.BuildResult(left)
		if err != nil {
			t.Fatalf("build deterministic left result: %v", err)
		}
		rightResult, err := typedoutput.BuildResult(right)
		if err != nil {
			t.Fatalf("build deterministic right result: %v", err)
		}
		if !reflect.DeepEqual(leftResult, rightResult) {
			t.Fatalf("equivalent batch inputs diverged: left=%#v right=%#v", leftResult, rightResult)
		}

		canceled := typedoutput.ConvertBatch(&cancelAfterContext{cancelAfter: 5}, batch)
		if len(canceled) != 3 || canceled[0].Err() != nil || len(canceled[0].Resources()) != 1 || !typedoutput.HasReason(canceled[1].Err(), typedoutput.ReasonConversionInterrupted) || !typedoutput.HasReason(canceled[2].Err(), typedoutput.ReasonConversionInterrupted) || len(canceled[1].Resources()) != 0 || len(canceled[2].Resources()) != 0 {
			t.Fatalf("cancellation partition = %#v", canceled)
		}
		preCanceled := typedoutput.ConvertBatch(contextCanceled(), []typedoutput.SourceInput{{Source: firstSource, Extraction: batch[0].Extraction}})[0]
		if !typedoutput.HasReason(preCanceled.Err(), typedoutput.ReasonConversionInterrupted) || len(preCanceled.Resources()) != 0 {
			t.Fatalf("pre-canceled conversion = %#v", preCanceled)
		}
	})
}
