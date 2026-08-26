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
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func assertFieldExtractionNativeValueScenarios(t *testing.T) {
	t.Helper()
	resource := selectedExtractionResource()

	t.Run("scalar object array null and absent cardinality remain distinct", func(t *testing.T) {
		tests := []struct {
			name  string
			path  string
			want  []any
			count int
		}{
			{name: "scalar", path: "{.data.scalar}", want: []any{"text"}, count: 1},
			{name: "object", path: "{.data.object}", want: []any{map[string]any{"enabled": true, "name": "nested"}}, count: 1},
			{name: "array", path: "{.data.array}", want: []any{[]any{"first", float64(2)}}, count: 1},
			{name: "explicit null", path: "{.data.null}", want: []any{nil}, count: 1},
			{name: "missing", path: "{.data.missing}", want: nil, count: 0},
			{name: "out of range", path: "{.data.array[99]}", want: nil, count: 0},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				outcome := evaluateFieldPath(t, test.path, resource)
				field := outcome.Fields[0]
				if field.Matches.Len() != test.count || !reflect.DeepEqual(field.Matches.Values(), test.want) {
					t.Fatalf("path %q matches = %#v (count %d), want %#v (count %d)", test.path, field.Matches.Values(), field.Matches.Len(), test.want, test.count)
				}
			})
		}
	})

	t.Run("wildcards preserve branch order and local absence", func(t *testing.T) {
		resource := selectedExtractionResourceWithObject(map[string]any{
			"items": []any{
				map[string]any{"name": "first", "groups": []any{map[string]any{"name": "first-a"}, map[string]any{"name": "first-b"}}},
				map[string]any{"ignored": true, "groups": []any{map[string]any{"name": "second-a"}}},
				map[string]any{"name": "third", "groups": []any{}},
			},
		})
		wildcard := evaluateFieldPath(t, "{.items[*].name}", resource)
		if got := wildcard.Fields[0].Matches.Values(); !reflect.DeepEqual(got, []any{"first", "third"}) {
			t.Fatalf("wildcard values = %#v, want branch-local values", got)
		}
		nested := evaluateFieldPath(t, "{.items[*].groups[*].name}", resource)
		if got := nested.Fields[0].Matches.Values(); !reflect.DeepEqual(got, []any{"first-a", "first-b", "second-a"}) {
			t.Fatalf("nested wildcard values = %#v, want depth-first order", got)
		}
	})

	t.Run("native numbers and booleans are not rendered", func(t *testing.T) {
		resource := selectedExtractionResourceWithObject(map[string]any{
			"values": map[string]any{
				"boolean": true,
				"number":  float64(3.5),
				"exact":   json.Number("7.25"),
			},
		})
		for _, test := range []struct {
			path string
			want any
		}{
			{path: "{.values.boolean}", want: true},
			{path: "{.values.number}", want: float64(3.5)},
			{path: "{.values.exact}", want: json.Number("7.25")},
		} {
			outcome := evaluateFieldPath(t, test.path, resource)
			got := outcome.Fields[0].Matches.Values()
			if len(got) != 1 || reflect.TypeOf(got[0]) != reflect.TypeOf(test.want) || !reflect.DeepEqual(got[0], test.want) {
				t.Fatalf("path %q value = %#v (%T), want %#v (%T)", test.path, got, got[0], test.want, test.want)
			}
		}
	})

	t.Run("fields are sorted and provenance is complete", func(t *testing.T) {
		plan, err := extraction.CompileSource(v1alpha1.KubeseerSource{
			ID: "source-one",
			Fields: []v1alpha1.KubeseerField{
				{Name: "zeta", Path: "{.data.scalar}"},
				{Name: "alpha", Path: "{.data.object}"},
			},
		})
		if err != nil {
			t.Fatalf("compile fields: %v", err)
		}
		outcome, err := extraction.EvaluateResource(context.Background(), plan, resource)
		if err != nil {
			t.Fatalf("evaluate fields: %v", err)
		}
		if !reflect.DeepEqual(outcome.Provenance, resource.Provenance) {
			t.Fatalf("provenance = %#v, want %#v", outcome.Provenance, resource.Provenance)
		}
		if len(outcome.Fields) != 2 || outcome.Fields[0].FieldName != "alpha" || outcome.Fields[1].FieldName != "zeta" {
			t.Fatalf("field order = %#v, want alpha then zeta", outcome.Fields)
		}
	})

	t.Run("returned objects and arrays cannot mutate selected resources", func(t *testing.T) {
		resource := selectedExtractionResourceWithObject(map[string]any{
			"nested": map[string]any{
				"object": map[string]any{"name": "original"},
				"array":  []any{map[string]any{"name": "array-original"}},
			},
		})
		outcome := evaluateFieldPath(t, "{.nested.object}", resource)
		values := outcome.Fields[0].Matches.Values()
		values[0].(map[string]any)["name"] = "changed"
		if got := outcome.Fields[0].Matches.Values()[0].(map[string]any)["name"]; got != "original" {
			t.Fatalf("object match retained caller mutation: %#v", got)
		}
		arrayOutcome := evaluateFieldPath(t, "{.nested.array}", resource)
		arrayValues := arrayOutcome.Fields[0].Matches.Values()
		arrayValues[0].([]any)[0].(map[string]any)["name"] = "changed"
		if got := resource.Object.Object["nested"].(map[string]any)["array"].([]any)[0].(map[string]any)["name"]; got != "array-original" {
			t.Fatalf("array match mutated selected resource: %#v", got)
		}
	})

	t.Run("evaluation errors are typed scoped and sanitized", func(t *testing.T) {
		sanitizedResource := selectedExtractionResourceWithObject(map[string]any{
			"data": map[string]any{"scalar": "observed-payload-123"},
		})
		tests := []struct {
			name string
			path string
		}{
			{name: "property type mismatch", path: "{.data.scalar.name}"},
			{name: "array type mismatch", path: "{.data.scalar[0]}"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				plan, compileErr := extraction.CompileSource(v1alpha1.KubeseerSource{ID: "sensitive-source", Fields: []v1alpha1.KubeseerField{{Name: "secret-value", Path: test.path}}})
				if compileErr != nil {
					t.Fatalf("compile evaluation failure path: %v", compileErr)
				}
				_, err := extraction.EvaluateResource(context.Background(), plan, sanitizedResource)
				if !extraction.HasReason(err, extraction.ReasonEvaluationTypeMismatch) || err.SourceID != "sensitive-source" || err.FieldName != "secret-value" || err.Provenance == nil || err.Provenance.Name != "resource-a" {
					t.Fatalf("evaluation error = %#v, want scoped type mismatch", err)
				}
				if strings.Contains(err.Error(), "observed-payload-123") {
					t.Fatalf("evaluation error exposed an observed value: %v", err)
				}
			})
		}

		invalidPlan, compileErr := extraction.CompileSource(v1alpha1.KubeseerSource{ID: "invalid-resource-source", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.scalar}"}}})
		if compileErr != nil {
			t.Fatalf("compile invalid-resource plan: %v", compileErr)
		}
		_, err := extraction.EvaluateResource(context.Background(), invalidPlan, selection.SelectedResource{Provenance: sanitizedResource.Provenance})
		if !extraction.HasReason(err, extraction.ReasonInvalidResource) || err.Provenance == nil {
			t.Fatalf("nil selected object error = %#v, want invalid resource", err)
		}

		invalidNative := selectedExtractionResourceWithObject(map[string]any{"bad": complex(1, 2)})
		_, err = extraction.EvaluateResource(context.Background(), invalidPlan, invalidNative)
		if !extraction.HasReason(err, extraction.ReasonInvalidResource) || strings.Contains(err.Error(), "complex") {
			t.Fatalf("invalid native value error = %v, want sanitized invalid resource", err)
		}
	})
}

func evaluateFieldPath(t *testing.T, path string, resource selection.SelectedResource) extraction.ResourceOutcome {
	t.Helper()
	plan, err := extraction.CompileSource(v1alpha1.KubeseerSource{
		ID:     "source-one",
		Fields: []v1alpha1.KubeseerField{{Name: "value", Path: path}},
	})
	if err != nil {
		t.Fatalf("compile path %q: %v", path, err)
	}
	outcome, err := extraction.EvaluateResource(context.Background(), plan, resource)
	if err != nil {
		t.Fatalf("evaluate path %q: %v", path, err)
	}
	return outcome
}

func selectedExtractionResource() selection.SelectedResource {
	return selectedExtractionResourceWithObject(map[string]any{
		"metadata": map[string]any{
			"name":      "resource-a",
			"namespace": "team-a",
		},
		"data": map[string]any{
			"scalar": "text",
			"object": map[string]any{"enabled": true, "name": "nested"},
			"array":  []any{"first", float64(2)},
			"null":   nil,
		},
	})
}

func selectedExtractionResourceWithObject(object map[string]any) selection.SelectedResource {
	return selection.SelectedResource{
		Object: &unstructured.Unstructured{Object: object},
		Provenance: selection.Provenance{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Namespace:  "team-a",
			Name:       "resource-a",
			UID:        "uid-a",
		},
	}
}
