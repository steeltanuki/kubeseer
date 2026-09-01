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

package extraction

import (
	"context"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/selection"
)

// SourceInput joins one public source declaration with the result of the
// already-authorized resource-selection boundary.
type SourceInput struct {
	Source    v1alpha1.KubeseerSource
	Selection selection.SelectionOutcome
}

// SourceOutcome is one source-scoped extraction result. Resources are
// populated only on success; Err preserves either the exact selection failure
// or a sanitized extraction failure.
type SourceOutcome struct {
	SourceID  string
	Resources []ResourceOutcome
	Err       error
}

// ExtractBatch compiles all source declarations before evaluating any selected
// object, then evaluates sources sequentially in input order. A source failure
// discards its temporary results while completed sibling outcomes remain
// available.
func ExtractBatch(ctx context.Context, inputs []SourceInput) []SourceOutcome {
	return ExtractBatchWithLimit(ctx, inputs, 0)
}

// ExtractBatchWithLimit is ExtractBatch with a source-local canonical output
// ceiling. A zero ceiling preserves the historical unbounded direct-call
// behavior; the manager always supplies the resolved positive ceiling.
func ExtractBatchWithLimit(ctx context.Context, inputs []SourceInput, maxBytes int64) []SourceOutcome {
	if ctx == nil {
		ctx = context.Background()
	}
	sources := make([]v1alpha1.KubeseerSource, len(inputs))
	for index, input := range inputs {
		sources[index] = input.Source
	}
	compiled := CompileBatch(sources)
	outcomes := make([]SourceOutcome, len(inputs))

	for index, input := range inputs {
		sourceID := input.Source.ID
		if cause := ctx.Err(); cause != nil {
			outcomes[index] = SourceOutcome{SourceID: sourceID, Err: interruptedSourceError(sourceID, cause)}
			continue
		}
		if input.Selection.SourceID != sourceID {
			outcomes[index] = SourceOutcome{
				SourceID: sourceID,
				Err:      sourceError(sourceID, ReasonInvalidInput, "selection source identity does not match declaration", nil),
			}
			continue
		}
		if compiled[index].Err != nil {
			outcomes[index] = SourceOutcome{SourceID: sourceID, Err: compiled[index].Err}
			continue
		}
		if input.Selection.Err != nil {
			outcomes[index] = SourceOutcome{SourceID: sourceID, Err: input.Selection.Err}
			continue
		}
		if len(compiled[index].Plan.fields) == 0 || len(input.Selection.Resources) == 0 {
			outcomes[index] = SourceOutcome{SourceID: sourceID}
			continue
		}
		var accountant *limits.Accountant
		if maxBytes > 0 {
			var err error
			accountant, err = limits.NewAccountant(maxBytes, "produced-values")
			if err != nil {
				outcomes[index] = SourceOutcome{SourceID: sourceID, Err: valueLimitError(sourceID)}
				continue
			}
		}

		temporary := make([]ResourceOutcome, 0, len(input.Selection.Resources))
		failed := false
		for _, resource := range input.Selection.Resources {
			if cause := ctx.Err(); cause != nil {
				outcomes[index] = SourceOutcome{SourceID: sourceID, Err: interruptedSourceError(sourceID, cause)}
				failed = true
				break
			}
			resourceOutcome, err := EvaluateResource(ctx, compiled[index].Plan, resource)
			if err != nil {
				outcomes[index] = SourceOutcome{SourceID: sourceID, Err: err}
				failed = true
				break
			}
			if accountant != nil {
				if err := accountant.Add(canonicalResource(resourceOutcome)); err != nil {
					outcomes[index] = SourceOutcome{SourceID: sourceID, Err: valueLimitError(sourceID)}
					failed = true
					break
				}
			}
			temporary = append(temporary, resourceOutcome)
		}
		if !failed {
			outcomes[index] = SourceOutcome{SourceID: sourceID, Resources: temporary}
		}
	}
	return outcomes
}

type canonicalExtractionResource struct {
	Provenance selection.Provenance       `json:"provenance"`
	Fields     []canonicalExtractionField `json:"fields"`
}

type canonicalExtractionField struct {
	Name   string `json:"name"`
	Values []any  `json:"values"`
}

func canonicalResource(resource ResourceOutcome) canonicalExtractionResource {
	canonical := canonicalExtractionResource{Provenance: resource.Provenance, Fields: make([]canonicalExtractionField, len(resource.Fields))}
	for index, field := range resource.Fields {
		canonical.Fields[index] = canonicalExtractionField{Name: field.FieldName, Values: field.Matches.Values()}
	}
	return canonical
}

func valueLimitError(sourceID string) *ExtractionError {
	return sourceError(sourceID, ReasonValueLimitExceeded, "produced value ceiling exceeded", nil)
}

func sourceError(sourceID string, reason ExtractionErrorReason, message string, cause error) *ExtractionError {
	return &ExtractionError{SourceID: sourceID, Reason: reason, Message: message, cause: cause}
}

func interruptedSourceError(sourceID string, cause error) *ExtractionError {
	return sourceError(sourceID, ReasonExtractionInterrupted, "extraction interrupted", cause)
}
