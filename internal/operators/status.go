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

package operators

import (
	"errors"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

// BuildResult projects operator outcomes into the existing structural typed
// result. Rejected resources are intentionally omitted; unsuccessful ones
// retain provenance and a sanitized resource error without fields.
func BuildResult(outcomes []SourceOutcome) (v1alpha1.KubeseerResult, error) {
	result := v1alpha1.KubeseerResult{}
	if outcomes == nil {
		return result, nil
	}
	result.Sources = make([]v1alpha1.KubeseerSourceResult, len(outcomes))
	for sourceIndex, source := range outcomes {
		if source.sourceID == "" {
			return v1alpha1.KubeseerResult{}, errors.New("operator result source identity is empty")
		}
		if source.err != nil {
			if len(source.resources) != 0 || len(source.fieldErrors) != 0 {
				return v1alpha1.KubeseerResult{}, errors.New("operator source error contains successful payloads")
			}
			result.Sources[sourceIndex] = v1alpha1.KubeseerSourceResult{
				ID:    source.sourceID,
				State: v1alpha1.SourceStateError,
				Error: publicOperatorResultError(source.err),
			}
			continue
		}

		projected := v1alpha1.KubeseerSourceResult{ID: source.sourceID, State: v1alpha1.SourceStateValues}
		if len(source.fieldErrors) != 0 {
			projected.FieldErrors = make([]v1alpha1.KubeseerFieldError, len(source.fieldErrors))
			for index, failure := range source.fieldErrors {
				if failure == nil || failure.FieldName == "" {
					return v1alpha1.KubeseerResult{}, errors.New("operator planning failure is incomplete")
				}
				projected.FieldErrors[index] = v1alpha1.KubeseerFieldError{
					Name:    failure.FieldName,
					Reason:  string(failure.Reason),
					Message: failure.Message,
				}
			}
		}

		for _, resource := range source.resources {
			switch resource.state {
			case ResourceRejected:
				continue
			case ResourceAccepted:
				converted, err := typedoutput.ProjectResourceResult(resource.provenance, resource.fields)
				if err != nil {
					return v1alpha1.KubeseerResult{}, err
				}
				projected.Resources = append(projected.Resources, converted)
			case ResourceUnsuccessful:
				if resource.failure == nil {
					return v1alpha1.KubeseerResult{}, errors.New("unsuccessful operator resource has no failure")
				}
				converted, err := typedoutput.ProjectResourceResult(resource.provenance, nil)
				if err != nil {
					return v1alpha1.KubeseerResult{}, err
				}
				converted.Error = publicOperatorResultError(resource.failure)
				projected.Resources = append(projected.Resources, converted)
			default:
				return v1alpha1.KubeseerResult{}, errors.New("operator resource state is unsupported")
			}
		}
		result.Sources[sourceIndex] = projected
	}
	return result, nil
}

func publicOperatorResultError(err error) *v1alpha1.KubeseerResultError {
	if err == nil {
		return nil
	}
	var operatorErr *OperatorError
	if errors.As(err, &operatorErr) {
		return &v1alpha1.KubeseerResultError{Reason: string(operatorErr.Reason), Message: operatorErr.Message}
	}
	return typedoutput.ProjectResultError(err)
}
