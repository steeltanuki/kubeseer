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

package selection

import "context"

// SelectBatch executes authorized source plans sequentially and preserves the
// input order. A cancellation prevents new reads while completed outcomes stay
// intact; each source that was not started receives its own interrupted error.
func (e *Executor) SelectBatch(ctx context.Context, plans []AuthorizedPlan) []SelectionOutcome {
	if ctx == nil {
		ctx = context.Background()
	}
	outcomes := make([]SelectionOutcome, len(plans))
	for index, plan := range plans {
		sourceID := plan.Plan().SourceID()
		if cause := ctx.Err(); cause != nil {
			outcomes[index] = SelectionOutcome{
				SourceID: sourceID,
				Err:      interruptedError(sourceID, cause),
			}
			continue
		}
		outcomes[index] = e.Execute(ctx, plan)
	}
	return outcomes
}
