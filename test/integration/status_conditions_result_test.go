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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
)

func assertStatusAndConditionsResultScenarios(t *testing.T) {
	t.Helper()

	t.Run("summary and degradation come from the structural result", func(t *testing.T) {
		value := "observed-value"
		result := &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{
			{
				ID:    "successful-source",
				State: v1alpha1.SourceStateValues,
				FieldErrors: []v1alpha1.KubeseerFieldError{{
					Name:   "declared-field",
					Reason: "InvalidExpression",
				}},
				Resources: []v1alpha1.KubeseerResourceResult{
					{
						APIVersion: "v1",
						Kind:       "Pod",
						Name:       "pod-a",
						UID:        "pod-a-uid",
						Fields: []v1alpha1.KubeseerFieldResult{{
							Name:  "value",
							Type:  v1alpha1.ValueTypeString,
							State: v1alpha1.FieldStateValues,
							Matches: []v1alpha1.KubeseerTypedMatch{{
								State:       v1alpha1.MatchStateValue,
								StringValue: &value,
							}},
						}},
					},
					{
						APIVersion: "v1",
						Kind:       "Pod",
						Name:       "pod-b",
						UID:        "pod-b-uid",
					},
				},
			},
			{
				ID:    "failed-source",
				State: v1alpha1.SourceStateError,
				Error: &v1alpha1.KubeseerResultError{Reason: "ReadUnavailable", Message: "source unavailable"},
			},
		}}

		derived, err := statuscontract.DeriveResult(result)
		if err != nil {
			t.Fatalf("derive result: %v", err)
		}
		wantSummary := &v1alpha1.KubeseerSummary{SuccessfulSources: 1, FailedSources: 1, MatchedResources: 2}
		if !reflect.DeepEqual(derived.Summary, wantSummary) {
			t.Fatalf("summary = %#v, want %#v", derived.Summary, wantSummary)
		}
		if derived.Result == nil || !statuscontract.HasResultErrors(derived.Result) || !derived.Degraded {
			t.Fatalf("derived result did not retain structural errors: %#v", derived)
		}
		if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(derived.ResultHash) {
			t.Fatalf("result hash has invalid format: %q", derived.ResultHash)
		}
		encoded, err := json.Marshal(derived.Result)
		if err != nil {
			t.Fatalf("marshal normalized result independently: %v", err)
		}
		digest := sha256.Sum256(encoded)
		if wantHash := fmt.Sprintf("sha256:%x", digest); derived.ResultHash != wantHash {
			t.Fatalf("result hash = %q, want independent digest %q", derived.ResultHash, wantHash)
		}

		copy := derived.Result.DeepCopy()
		copy.Sources[0].Resources[0].Name = "changed"
		if result.Sources[0].Resources[0].Name != "pod-a" {
			t.Fatalf("normalized result aliases source result: %#v", result)
		}
	})

	t.Run("nil and empty collections are one semantic result", func(t *testing.T) {
		nilCollections := &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{{
			ID:    "source",
			State: v1alpha1.SourceStateValues,
			Resources: []v1alpha1.KubeseerResourceResult{{
				APIVersion: "v1",
				Kind:       "Pod",
				Name:       "pod",
				UID:        "pod-uid",
				Fields: []v1alpha1.KubeseerFieldResult{{
					Name:    "field",
					State:   v1alpha1.FieldStateValues,
					Matches: nil,
				}},
			}},
		}}}
		emptyCollections := nilCollections.DeepCopy()
		emptyCollections.Sources = append([]v1alpha1.KubeseerSourceResult(nil), emptyCollections.Sources...)
		emptyCollections.Sources[0].FieldErrors = []v1alpha1.KubeseerFieldError{}
		emptyCollections.Sources[0].Resources = append([]v1alpha1.KubeseerResourceResult(nil), emptyCollections.Sources[0].Resources...)
		emptyCollections.Sources[0].Resources[0].Fields = []v1alpha1.KubeseerFieldResult{}
		emptyCollections.Sources[0].Resources[0].Fields = append(emptyCollections.Sources[0].Resources[0].Fields, v1alpha1.KubeseerFieldResult{
			Name:    "field",
			State:   v1alpha1.FieldStateValues,
			Matches: []v1alpha1.KubeseerTypedMatch{},
		})

		if !statuscontract.SemanticResultEqual(nilCollections, emptyCollections) {
			t.Fatalf("nil and empty collection results are not semantically equal: nil=%#v empty=%#v", nilCollections, emptyCollections)
		}
		left, err := statuscontract.DeriveResult(nilCollections)
		if err != nil {
			t.Fatalf("derive nil-collection result: %v", err)
		}
		right, err := statuscontract.DeriveResult(emptyCollections)
		if err != nil {
			t.Fatalf("derive empty-collection result: %v", err)
		}
		if left.ResultHash != right.ResultHash || !reflect.DeepEqual(left.Summary, right.Summary) {
			t.Fatalf("nil/empty derivations differ: left=%#v right=%#v", left, right)
		}

		emptyResult := &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{}}
		nilResult := &v1alpha1.KubeseerResult{}
		if !statuscontract.SemanticResultEqual(emptyResult, nilResult) {
			t.Fatalf("nil and empty top-level source collections are not equivalent")
		}
	})

	t.Run("result order is significant and status metadata is excluded", func(t *testing.T) {
		first := &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{
			{ID: "first", State: v1alpha1.SourceStateValues},
			{ID: "second", State: v1alpha1.SourceStateError},
		}}
		second := first.DeepCopy()
		second.Sources[0], second.Sources[1] = second.Sources[1], second.Sources[0]
		left, err := statuscontract.DeriveResult(first)
		if err != nil {
			t.Fatalf("derive ordered result: %v", err)
		}
		right, err := statuscontract.DeriveResult(second)
		if err != nil {
			t.Fatalf("derive reordered result: %v", err)
		}
		if left.ResultHash == right.ResultHash {
			t.Fatalf("reordering result sources did not change hash: %q", left.ResultHash)
		}
	})

	t.Run("absent result omits derived values", func(t *testing.T) {
		derived, err := statuscontract.DeriveResult(nil)
		if err != nil {
			t.Fatalf("derive absent result: %v", err)
		}
		if derived.Result != nil || derived.Summary != nil || derived.ResultHash != "" || derived.Degraded {
			t.Fatalf("absent result produced derived values: %#v", derived)
		}
	})
}
