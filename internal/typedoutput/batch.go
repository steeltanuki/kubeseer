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

package typedoutput

import (
	"context"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
)

// SourceInput joins one public source declaration with its completed native
// extraction outcome.
type SourceInput struct {
	Source     v1alpha1.KubeseerSource
	Extraction extraction.SourceOutcome
}

// ConvertBatch compiles every source declaration before converting any
// extracted value, then processes sources sequentially in input order.
// Completed source outcomes remain available when cancellation interrupts a
// later source.
func ConvertBatch(ctx context.Context, inputs []SourceInput) []SourceOutcome {
	if ctx == nil {
		ctx = context.Background()
	}
	sources := make([]v1alpha1.KubeseerSource, len(inputs))
	for index, input := range inputs {
		sources[index] = input.Source
	}
	plans := CompileBatch(sources)
	outcomes := make([]SourceOutcome, len(inputs))

	interrupted := false
	for index, input := range inputs {
		sourceID := input.Source.ID
		if interrupted {
			outcomes[index] = SourceOutcome{sourceID: sourceID, err: interruptedConversionError(sourceID, "", nil, context.Canceled)}
			continue
		}
		if cause := ctx.Err(); cause != nil {
			interrupted = true
			outcomes[index] = SourceOutcome{sourceID: sourceID, err: interruptedConversionError(sourceID, "", nil, cause)}
			continue
		}
		if input.Extraction.SourceID != sourceID {
			outcomes[index] = SourceOutcome{
				sourceID: sourceID,
				err:      NewConversionError(sourceID, "", nil, ReasonInvalidInput, "extraction source identity does not match declaration"),
			}
			continue
		}
		converted := convertSourceContext(ctx, plans[index], input.Extraction)
		outcomes[index] = converted
		if HasReason(converted.err, ReasonConversionInterrupted) {
			interrupted = true
		}
	}
	return outcomes
}
