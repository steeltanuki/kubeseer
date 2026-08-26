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
	"encoding/json"
	"fmt"

	"github.com/steeltanuki/kubeseer/internal/selection"
)

// EvaluateResource evaluates all fields in one immutable plan against one
// selected resource. It performs no Kubernetes I/O and never returns aliases
// into the selected object's native JSON tree.
func EvaluateResource(ctx context.Context, plan Plan, resource selection.SelectedResource) (ResourceOutcome, *ExtractionError) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ResourceOutcome{}, interruptedError(plan.SourceID(), "", resource.Provenance, err)
	}
	if resource.Object == nil || resource.Object.Object == nil {
		return ResourceOutcome{}, resourceError(plan.SourceID(), "", resource.Provenance, ReasonInvalidResource, "selected resource is not a valid object", nil)
	}
	if _, err := cloneJSONValue(resource.Object.Object); err != nil {
		return ResourceOutcome{}, resourceError(plan.SourceID(), "", resource.Provenance, ReasonInvalidResource, "selected resource contains an invalid native value", err)
	}

	outcome := ResourceOutcome{
		Provenance: resource.Provenance,
		Fields:     make([]FieldOutcome, 0, len(plan.fields)),
	}
	for _, field := range plan.fields {
		if err := ctx.Err(); err != nil {
			return ResourceOutcome{}, interruptedError(plan.SourceID(), field.name, resource.Provenance, err)
		}
		values, err := evaluateField(ctx, plan.sourceID, field, resource.Object.Object, resource.Provenance)
		if err != nil {
			return ResourceOutcome{}, err
		}
		outcome.Fields = append(outcome.Fields, FieldOutcome{
			FieldName: field.name,
			Matches:   MatchSet{values: values},
		})
	}
	return outcome, nil
}

func evaluateField(ctx context.Context, sourceID string, field FieldPlan, root map[string]any, provenance selection.Provenance) ([]any, *ExtractionError) {
	branches := []any{root}
	for _, operation := range field.ops {
		if err := ctx.Err(); err != nil {
			return nil, interruptedError(sourceID, field.name, provenance, err)
		}
		next := make([]any, 0, len(branches))
		for _, branch := range branches {
			if err := ctx.Err(); err != nil {
				return nil, interruptedError(sourceID, field.name, provenance, err)
			}
			switch operation.kind {
			case operationProperty:
				object, ok := branch.(map[string]any)
				if !ok {
					return nil, resourceError(sourceID, field.name, provenance, ReasonEvaluationTypeMismatch, "property access requires an object", nil)
				}
				value, found := object[operation.key]
				if found {
					next = append(next, value)
				}
			case operationIndex:
				array, ok := branch.([]any)
				if !ok {
					return nil, resourceError(sourceID, field.name, provenance, ReasonEvaluationTypeMismatch, "array index requires an array", nil)
				}
				if operation.index < len(array) {
					next = append(next, array[operation.index])
				}
			case operationWildcard:
				array, ok := branch.([]any)
				if !ok {
					return nil, resourceError(sourceID, field.name, provenance, ReasonEvaluationTypeMismatch, "array wildcard requires an array", nil)
				}
				for _, value := range array {
					if err := ctx.Err(); err != nil {
						return nil, interruptedError(sourceID, field.name, provenance, err)
					}
					next = append(next, value)
				}
			}
		}
		branches = next
	}

	if len(branches) == 0 {
		return nil, nil
	}
	values := make([]any, len(branches))
	for index, branch := range branches {
		if err := ctx.Err(); err != nil {
			return nil, interruptedError(sourceID, field.name, provenance, err)
		}
		clone, err := cloneJSONValue(branch)
		if err != nil {
			return nil, resourceError(sourceID, field.name, provenance, ReasonInvalidResource, "selected resource contains an invalid native value", err)
		}
		values[index] = clone
	}
	return values, nil
}

func cloneJSONValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil, bool, string, int64, float64, json.Number:
		return typed, nil
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, nested := range typed {
			value, err := cloneJSONValue(nested)
			if err != nil {
				return nil, err
			}
			clone[key] = value
		}
		return clone, nil
	case []any:
		clone := make([]any, len(typed))
		for index, nested := range typed {
			value, err := cloneJSONValue(nested)
			if err != nil {
				return nil, err
			}
			clone[index] = value
		}
		return clone, nil
	default:
		return nil, fmt.Errorf("unsupported native JSON value type %T", value)
	}
}

func resourceError(sourceID, fieldName string, provenance selection.Provenance, reason ExtractionErrorReason, message string, cause error) *ExtractionError {
	copy := provenance
	return &ExtractionError{
		SourceID:   sourceID,
		FieldName:  fieldName,
		Provenance: &copy,
		Reason:     reason,
		Message:    message,
		cause:      cause,
	}
}

func interruptedError(sourceID, fieldName string, provenance selection.Provenance, cause error) *ExtractionError {
	return resourceError(sourceID, fieldName, provenance, ReasonExtractionInterrupted, "extraction interrupted", cause)
}
