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
	"sync/atomic"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
)

func assertFieldExtractionBatchScenarios(t *testing.T) {
	t.Helper()

	t.Run("omitted and explicit empty fields produce successful empty outcomes", func(t *testing.T) {
		resource := selectedExtractionResource()
		inputs := []extraction.SourceInput{
			{Source: v1alpha1.KubeseerSource{ID: "omitted-fields"}, Selection: selection.SelectionOutcome{SourceID: "omitted-fields", Resources: []selection.SelectedResource{resource}}},
			{Source: v1alpha1.KubeseerSource{ID: "empty-fields", Fields: []v1alpha1.KubeseerField{}}, Selection: selection.SelectionOutcome{SourceID: "empty-fields", Resources: []selection.SelectedResource{resource}}},
			{Source: v1alpha1.KubeseerSource{ID: "empty-selection", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.scalar}"}}}, Selection: selection.SelectionOutcome{SourceID: "empty-selection"}},
		}
		outcomes := extraction.ExtractBatch(context.Background(), inputs)
		for index, outcome := range outcomes {
			if outcome.SourceID != inputs[index].Source.ID || outcome.Err != nil || len(outcome.Resources) != 0 {
				t.Fatalf("outcome %d = %#v, want successful empty extraction", index, outcome)
			}
		}
	})

	t.Run("selection failures pass through without field evaluation", func(t *testing.T) {
		selectionErr := selection.NewSelectionError("selection-failed", selection.ReasonReadUnavailable, "resource read unavailable")
		input := extraction.SourceInput{
			Source: v1alpha1.KubeseerSource{ID: "selection-failed", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.scalar}"}}},
			Selection: selection.SelectionOutcome{
				SourceID:  "selection-failed",
				Resources: []selection.SelectedResource{{}},
				Err:       selectionErr,
			},
		}
		outcome := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{input})[0]
		if outcome.Err != selectionErr || !errors.Is(outcome.Err, selectionErr) || len(outcome.Resources) != 0 {
			t.Fatalf("selection failure was not preserved exactly: %#v", outcome)
		}
	})

	t.Run("planning and evaluation failures are source atomic", func(t *testing.T) {
		first := v1alpha1.KubeseerSource{
			ID: "atomic-failure",
			Fields: []v1alpha1.KubeseerField{
				{Name: "alpha", Path: "{.data.scalar}"},
				{Name: "zeta", Path: "{.data.scalar.name}"},
			},
		}
		second := v1alpha1.KubeseerSource{ID: "independent-success", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.scalar}"}}}
		outcomes := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{
			{Source: first, Selection: selection.SelectionOutcome{SourceID: first.ID, Resources: []selection.SelectedResource{selectedExtractionResource()}}},
			{Source: second, Selection: selection.SelectionOutcome{SourceID: second.ID, Resources: []selection.SelectedResource{selectedExtractionResource()}}},
		})
		if !extraction.HasReason(outcomes[0].Err, extraction.ReasonEvaluationTypeMismatch) || len(outcomes[0].Resources) != 0 {
			t.Fatalf("failed source retained partial extraction: %#v", outcomes[0])
		}
		if outcomes[1].Err != nil || len(outcomes[1].Resources) != 1 || len(outcomes[1].Resources[0].Fields) != 1 {
			t.Fatalf("independent source was not preserved: %#v", outcomes[1])
		}

		invalid := v1alpha1.KubeseerSource{ID: "invalid-plan", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data[?(@.name)]}"}}}
		valid := v1alpha1.KubeseerSource{ID: "valid-after-invalid", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.scalar}"}}}
		invalidResource := selectedExtractionResourceWithObject(map[string]any{"bad": complex(1, 2)})
		outcomes = extraction.ExtractBatch(context.Background(), []extraction.SourceInput{
			{Source: invalid, Selection: selection.SelectionOutcome{SourceID: invalid.ID, Resources: []selection.SelectedResource{invalidResource}}},
			{Source: valid, Selection: selection.SelectionOutcome{SourceID: valid.ID, Resources: []selection.SelectedResource{selectedExtractionResource()}}},
		})
		if !extraction.HasReason(outcomes[0].Err, extraction.ReasonUnsupportedExpression) || outcomes[0].Err.(*extraction.ExtractionError).FieldName != "value" {
			t.Fatalf("planning failure was not reported before evaluation: %#v", outcomes[0])
		}
		if outcomes[1].Err != nil {
			t.Fatalf("valid sibling failed after invalid plan: %v", outcomes[1].Err)
		}
	})

	t.Run("source identity and source order remain stable", func(t *testing.T) {
		first := v1alpha1.KubeseerSource{ID: "first", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.metadata.name}"}}}
		second := v1alpha1.KubeseerSource{ID: "second", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.metadata.namespace}"}}}
		outcomes := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{
			{Source: first, Selection: selection.SelectionOutcome{SourceID: first.ID, Resources: []selection.SelectedResource{selectedExtractionResource()}}},
			{Source: second, Selection: selection.SelectionOutcome{SourceID: second.ID, Resources: []selection.SelectedResource{selectedExtractionResource()}}},
		})
		got := []string{outcomes[0].SourceID, outcomes[1].SourceID}
		if !reflect.DeepEqual(got, []string{"first", "second"}) || outcomes[0].Err != nil || outcomes[1].Err != nil {
			t.Fatalf("batch source order = %#v, want input order", outcomes)
		}
		mismatch := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
			Source:    first,
			Selection: selection.SelectionOutcome{SourceID: "other-source"},
		}})[0]
		if !extraction.HasReason(mismatch.Err, extraction.ReasonInvalidInput) || mismatch.SourceID != "first" {
			t.Fatalf("source mismatch outcome = %#v, want invalid input for declaration", mismatch)
		}
	})

	t.Run("completed outcomes survive cancellation and later sources interrupt", func(t *testing.T) {
		first := v1alpha1.KubeseerSource{ID: "completed", Fields: []v1alpha1.KubeseerField{{Name: "root", Path: "{.}"}}}
		second := v1alpha1.KubeseerSource{ID: "unstarted", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.scalar}"}}}
		third := v1alpha1.KubeseerSource{ID: "also-unstarted", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.scalar}"}}}
		ctx := &cancelAfterContext{cancelAfter: 7}
		outcomes := extraction.ExtractBatch(ctx, []extraction.SourceInput{
			{Source: first, Selection: selection.SelectionOutcome{SourceID: first.ID, Resources: []selection.SelectedResource{selectedExtractionResource()}}},
			{Source: second, Selection: selection.SelectionOutcome{SourceID: second.ID, Resources: []selection.SelectedResource{selectedExtractionResource()}}},
			{Source: third, Selection: selection.SelectionOutcome{SourceID: third.ID, Resources: []selection.SelectedResource{selectedExtractionResource()}}},
		})
		if outcomes[0].Err != nil || len(outcomes[0].Resources) != 1 {
			t.Fatalf("completed source was not preserved: %#v", outcomes[0])
		}
		for index := 1; index < len(outcomes); index++ {
			if !extraction.HasReason(outcomes[index].Err, extraction.ReasonExtractionInterrupted) || len(outcomes[index].Resources) != 0 {
				t.Fatalf("source %d after cancellation = %#v, want interrupted empty outcome", index, outcomes[index])
			}
		}

		canceled := extraction.ExtractBatch(contextCanceled(), []extraction.SourceInput{{
			Source:    first,
			Selection: selection.SelectionOutcome{SourceID: first.ID},
		}})[0]
		if !extraction.HasReason(canceled.Err, extraction.ReasonExtractionInterrupted) {
			t.Fatalf("pre-canceled extraction = %#v, want interrupted outcome", canceled)
		}
	})
}

type cancelAfterContext struct {
	calls       atomic.Int32
	cancelAfter int32
}

func (c *cancelAfterContext) Deadline() (deadline time.Time, ok bool) { return time.Time{}, false }

func (c *cancelAfterContext) Done() <-chan struct{} { return nil }

func (c *cancelAfterContext) Err() error {
	if c.calls.Add(1) > c.cancelAfter {
		return context.Canceled
	}
	return nil
}

func (c *cancelAfterContext) Value(key any) any { return nil }

func contextCanceled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
