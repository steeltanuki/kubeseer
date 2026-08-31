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

package status

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"reflect"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
)

// DerivedResult is the immutable semantic projection derived from one
// structural result. Result remains the authoritative value; Summary,
// ResultHash, and Degraded are recomputed from it.
type DerivedResult struct {
	Result     *v1alpha1.KubeseerResult
	Summary    *v1alpha1.KubeseerSummary
	ResultHash string
	Degraded   bool
}

// DeriveResult deep-copies and normalizes result, then derives all status
// values that are functions of that result. A nil result represents an
// unavailable result and therefore has no summary, hash, or degradation.
func DeriveResult(result *v1alpha1.KubeseerResult) (DerivedResult, error) {
	normalized := NormalizeResult(result)
	if normalized == nil {
		return DerivedResult{}, nil
	}

	summary, err := summaryForResult(normalized)
	if err != nil {
		return DerivedResult{}, err
	}
	hash, err := hashNormalizedResult(normalized)
	if err != nil {
		return DerivedResult{}, err
	}
	return DerivedResult{
		Result:     normalized,
		Summary:    summary,
		ResultHash: hash,
		Degraded:   HasResultErrors(normalized),
	}, nil
}

// NormalizeResult returns a deep copy with nil and empty result collections
// represented identically. Ordered result collections are never sorted.
func NormalizeResult(result *v1alpha1.KubeseerResult) *v1alpha1.KubeseerResult {
	if result == nil {
		return nil
	}

	copy := result.DeepCopy()
	if len(copy.Sources) == 0 {
		copy.Sources = nil
	}
	for sourceIndex := range copy.Sources {
		source := &copy.Sources[sourceIndex]
		if len(source.FieldErrors) == 0 {
			source.FieldErrors = nil
		}
		if len(source.Resources) == 0 {
			source.Resources = nil
		}
		for resourceIndex := range source.Resources {
			resource := &source.Resources[resourceIndex]
			if len(resource.Fields) == 0 {
				resource.Fields = nil
			}
			for fieldIndex := range resource.Fields {
				if len(resource.Fields[fieldIndex].Matches) == 0 {
					resource.Fields[fieldIndex].Matches = nil
				}
			}
		}
	}
	return copy
}

// SemanticResultEqual compares normalized structural results, retaining the
// distinction between an absent result and a present empty result.
func SemanticResultEqual(left, right *v1alpha1.KubeseerResult) bool {
	return reflect.DeepEqual(NormalizeResult(left), NormalizeResult(right))
}

// HasResultErrors reports source-scoped and field-scoped errors in a result.
func HasResultErrors(result *v1alpha1.KubeseerResult) bool {
	if result == nil {
		return false
	}
	for _, source := range result.Sources {
		if source.State == v1alpha1.SourceStateError || source.Error != nil || len(source.FieldErrors) != 0 {
			return true
		}
		for _, resource := range source.Resources {
			if resource.Error != nil {
				return true
			}
			for _, field := range resource.Fields {
				if field.State == v1alpha1.FieldStateError || field.Error != nil {
					return true
				}
			}
		}
	}
	return false
}

func summaryForResult(result *v1alpha1.KubeseerResult) (*v1alpha1.KubeseerSummary, error) {
	summary := &v1alpha1.KubeseerSummary{}
	for _, source := range result.Sources {
		switch source.State {
		case v1alpha1.SourceStateValues:
			if err := increment(&summary.SuccessfulSources); err != nil {
				return nil, fmt.Errorf("count successful sources: %w", err)
			}
		case v1alpha1.SourceStateError:
			if err := increment(&summary.FailedSources); err != nil {
				return nil, fmt.Errorf("count failed sources: %w", err)
			}
		}
		if err := addCount(&summary.MatchedResources, len(source.Resources)); err != nil {
			return nil, fmt.Errorf("count matched resources: %w", err)
		}
	}
	return summary, nil
}

func increment(value *int64) error {
	return addCount(value, 1)
}

func addCount(value *int64, count int) error {
	if count < 0 || uint64(count) > uint64(math.MaxInt64-*value) {
		return fmt.Errorf("count %d overflows int64", count)
	}
	*value += int64(count)
	return nil
}

func hashNormalizedResult(result *v1alpha1.KubeseerResult) (string, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal normalized result: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest), nil
}
