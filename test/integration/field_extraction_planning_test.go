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
	"reflect"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
)

func assertFieldExtractionPlanningScenarios(t *testing.T) {
	t.Helper()

	t.Run("supported grammar compiles without broad JSONPath behavior", func(t *testing.T) {
		tests := []struct {
			name string
			path string
		}{
			{name: "root", path: "{.}"},
			{name: "dot property", path: "{.metadata.name}"},
			{name: "underscore property", path: "{._private.value_2}"},
			{name: "quoted key", path: "{.data['display-name']}"},
			{name: "escaped quoted key", path: "{.data['display\\'name']}"},
			{name: "escaped backslash key", path: "{.data['display\\\\name']}"},
			{name: "array index", path: "{.items[0].name}"},
			{name: "array wildcard", path: "{.items[*].name}"},
			{name: "nested wildcard", path: "{.items[*].values[*]}"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				source := v1alpha1.KubeseerSource{
					ID:     "supported-source",
					Fields: []v1alpha1.KubeseerField{{Name: "value", Path: test.path}},
				}
				plan, err := extraction.CompileSource(source)
				if err != nil {
					t.Fatalf("compile supported path %q: %v", test.path, err)
				}
				fields := plan.Fields()
				if plan.SourceID() != source.ID || len(fields) != 1 || fields[0].Name() != "value" || fields[0].Path() != test.path {
					t.Fatalf("compiled plan = source %q fields %#v, want source %q path %q", plan.SourceID(), fields, source.ID, test.path)
				}
			})
		}
	})

	t.Run("unsupported expressions have a stable distinct reason", func(t *testing.T) {
		tests := []struct {
			name string
			path string
		}{
			{name: "filter", path: "{.items[?(@.name=='demo')].name}"},
			{name: "union", path: "{.items[0,1].name}"},
			{name: "slice", path: "{.items[0:2].name}"},
			{name: "negative index", path: "{.items[-1].name}"},
			{name: "recursive descent", path: "{..metadata.name}"},
			{name: "map wildcard", path: "{.*}"},
			{name: "double quoted key", path: "{.data[\"display-name\"]}"},
			{name: "function", path: "{.items|length}"},
			{name: "template text", path: "{range .items[*]}{.name}{end}"},
			{name: "outside suffix", path: "{.metadata.name} trailing"},
			{name: "outside prefix", path: "prefix {.metadata.name}"},
			{name: "multiple paths", path: "{.metadata.name}{.metadata.namespace}"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				source := v1alpha1.KubeseerSource{
					ID:     "unsupported-source",
					Fields: []v1alpha1.KubeseerField{{Name: "value", Path: test.path}},
				}
				_, first := extraction.CompileSource(source)
				if !extraction.HasReason(first, extraction.ReasonUnsupportedExpression) {
					t.Fatalf("compile %q reason = %v, want %s", test.path, first, extraction.ReasonUnsupportedExpression)
				}
				if first.SourceID != source.ID || first.FieldName != "value" || first.Offset < 0 {
					t.Fatalf("unsupported error lost source, field, or offset: %#v", first)
				}
				_, second := extraction.CompileSource(source)
				if second.Reason != first.Reason || second.Error() != first.Error() {
					t.Fatalf("unsupported error is not stable: first=%v second=%v", first, second)
				}
			})
		}
	})

	t.Run("malformed expressions have a stable invalid reason", func(t *testing.T) {
		tests := []struct {
			name string
			path string
		}{
			{name: "missing envelope", path: "metadata.name"},
			{name: "missing root dot", path: "{metadata.name}"},
			{name: "unclosed envelope", path: "{.metadata.name"},
			{name: "missing property after dot", path: "{.metadata.}"},
			{name: "unclosed index", path: "{.items[0}"},
			{name: "empty index", path: "{.items[]}"},
			{name: "invalid quoted escape", path: "{.data['display\\nname']}"},
			{name: "unclosed quoted key", path: "{.data['display-name]}"},
			{name: "overflowed index", path: "{.items[18446744073709551616]}"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				source := v1alpha1.KubeseerSource{
					ID:     "invalid-source",
					Fields: []v1alpha1.KubeseerField{{Name: "value", Path: test.path}},
				}
				_, err := extraction.CompileSource(source)
				if !extraction.HasReason(err, extraction.ReasonInvalidExpression) {
					t.Fatalf("compile %q reason = %v, want %s", test.path, err, extraction.ReasonInvalidExpression)
				}
				if err.SourceID != source.ID || err.FieldName != "value" {
					t.Fatalf("invalid error lost source or field identity: %#v", err)
				}
			})
		}
	})

	t.Run("batch compilation isolates sources and sorts fields", func(t *testing.T) {
		sources := []v1alpha1.KubeseerSource{
			{
				ID: "first-source",
				Fields: []v1alpha1.KubeseerField{
					{Name: "zeta", Path: "{.metadata.name}"},
					{Name: "alpha", Path: "{.metadata.namespace}"},
				},
			},
			{ID: "broken-source", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.items[?(@.name)]}"}}},
			{ID: "last-source", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: "{.metadata.uid}"}}},
		}
		outcomes := extraction.CompileBatch(sources)
		if len(outcomes) != len(sources) {
			t.Fatalf("compiled outcomes = %d, want %d", len(outcomes), len(sources))
		}
		if outcomes[0].Err != nil || outcomes[0].SourceID != "first-source" || outcomes[2].Err != nil || outcomes[2].SourceID != "last-source" {
			t.Fatalf("independent valid sources were not preserved: %#v", outcomes)
		}
		if !extraction.HasReason(outcomes[1].Err, extraction.ReasonUnsupportedExpression) || outcomes[1].SourceID != "broken-source" || outcomes[1].Err.FieldName != "value" {
			t.Fatalf("invalid source result lost its scoped failure: %#v", outcomes[1])
		}
		fields := outcomes[0].Plan.Fields()
		if got := []string{fields[0].Name(), fields[1].Name()}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
			t.Fatalf("fields are not lexicographically ordered: %#v", got)
		}

		permuted := []v1alpha1.KubeseerSource{{
			ID: "first-source",
			Fields: []v1alpha1.KubeseerField{
				{Name: "alpha", Path: "{.metadata.namespace}"},
				{Name: "zeta", Path: "{.metadata.name}"},
			},
		}}
		permutedOutcomes := extraction.CompileBatch(permuted)
		permutedFields := permutedOutcomes[0].Plan.Fields()
		if len(permutedFields) != len(fields) {
			t.Fatalf("equivalent plan field count changed: %#v vs %#v", fields, permutedFields)
		}
		for index := range fields {
			if fields[index].Name() != permutedFields[index].Name() || fields[index].Path() != permutedFields[index].Path() {
				t.Fatalf("equivalent declarations produced different plans: %#v vs %#v", fields, permutedFields)
			}
		}

		sources[0].Fields[0].Path = "{.changed}"
		if outcomes[0].Plan.Fields()[1].Path() != "{.metadata.name}" {
			t.Fatal("compiled plan retained mutable source declaration state")
		}
		firstAccess := outcomes[0].Plan.Fields()
		secondAccess := outcomes[0].Plan.Fields()
		if &firstAccess[0] == &secondAccess[0] {
			t.Fatal("plan accessor returned the same mutable field slice")
		}
	})
}
