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
	"reflect"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertTypedOutputCardinalityScenarios(t *testing.T) {
	t.Helper()

	source := v1alpha1.KubeseerSource{
		ID: "cardinality-source",
		Fields: []v1alpha1.KubeseerField{
			{Name: "wildcard-failure", Path: "{.data.items[*].name}", Type: v1alpha1.ValueTypeInteger},
			{Name: "list", Path: "{.data.list}", Type: v1alpha1.ValueTypeList},
			{Name: "empty-object", Path: "{.data.emptyObject}", Type: v1alpha1.ValueTypeObject},
			{Name: "empty-list", Path: "{.data.emptyList}", Type: v1alpha1.ValueTypeList},
			{Name: "empty-string", Path: "{.data.emptyString}", Type: v1alpha1.ValueTypeString},
			{Name: "null", Path: "{.data.null}", Type: v1alpha1.ValueTypeString},
			{Name: "absent", Path: "{.data.missing}", Type: v1alpha1.ValueTypeString},
			{Name: "wildcard-values", Path: "{.data.items[*].name}", Type: v1alpha1.ValueTypeString},
		},
	}
	planOutcome := typedoutput.CompileSource(source)
	if len(planOutcome.Failures()) != 0 {
		t.Fatalf("compile cardinality source: %#v", planOutcome.Failures())
	}

	first := selectedExtractionResourceWithObject(map[string]any{
		"data": map[string]any{
			"emptyString": "",
			"emptyObject": map[string]any{},
			"emptyList":   []any{},
			"list": []any{
				map[string]any{"name": "first"},
				"second",
			},
			"null": nil,
			"items": []any{
				map[string]any{"name": "1"},
				map[string]any{"name": "bad"},
				map[string]any{"name": "3"},
			},
		},
	})
	second := selectedExtractionResourceWithObject(map[string]any{
		"data": map[string]any{
			"emptyString": "second",
			"items":       []any{map[string]any{"name": "later"}},
		},
	})
	second.Provenance.Name = "resource-b"

	extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
		Source: source,
		Selection: selection.SelectionOutcome{
			SourceID:  source.ID,
			Resources: []selection.SelectedResource{first, second},
		},
	}})[0]
	if extracted.Err != nil || len(extracted.Resources) != 2 {
		t.Fatalf("extract cardinality fixtures: %#v", extracted)
	}

	typed := typedoutput.ConvertSource(planOutcome, extracted)
	if typed.Err() != nil || typed.State() != typedoutput.SourceStateValues {
		t.Fatalf("typed source outcome = state %q err %v", typed.State(), typed.Err())
	}
	resources := typed.Resources()
	if len(resources) != 2 || resources[0].Provenance().Name != "resource-a" || resources[1].Provenance().Name != "resource-b" {
		t.Fatalf("resource order/provenance = %#v, want extraction order", resources)
	}

	fields := resources[0].Fields()
	wantNames := []string{"absent", "empty-list", "empty-object", "empty-string", "list", "null", "wildcard-failure", "wildcard-values"}
	gotNames := make([]string, len(fields))
	byName := make(map[string]typedoutput.FieldOutcome, len(fields))
	for index, field := range fields {
		gotNames[index] = field.Name()
		byName[field.Name()] = field
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("field order = %#v, want %#v", gotNames, wantNames)
	}

	if field := byName["absent"]; field.State() != typedoutput.FieldStateAbsent || field.Type() != v1alpha1.ValueTypeString || len(field.Matches()) != 0 || field.Err() != nil {
		t.Fatalf("absent outcome = %#v, want absent typed field", field)
	}
	if field := byName["null"]; field.State() != typedoutput.FieldStateValues || field.Type() != v1alpha1.ValueTypeString || len(field.Matches()) != 1 || !field.Matches()[0].IsNull() {
		t.Fatalf("null outcome = %#v, want one typed null match", field)
	}
	if value, ok := byName["null"].Matches()[0].StringValue(); ok || value != "" {
		t.Fatalf("null exposed a scalar payload %q (ok=%t)", value, ok)
	}

	emptyString := byName["empty-string"]
	if emptyString.State() != typedoutput.FieldStateValues || len(emptyString.Matches()) != 1 {
		t.Fatalf("empty string outcome = %#v, want one value", emptyString)
	}
	if value, ok := emptyString.Matches()[0].StringValue(); !ok || value != "" {
		t.Fatalf("empty string match = %q (ok=%t)", value, ok)
	}

	emptyObject := byName["empty-object"]
	object, ok := emptyObject.Matches()[0].ObjectValue()
	if !ok || len(object) != 0 {
		t.Fatalf("empty object match = %#v (ok=%t)", object, ok)
	}
	object["added"] = true
	if next, _ := emptyObject.Matches()[0].ObjectValue(); len(next) != 0 {
		t.Fatal("object accessor exposed mutable outcome state")
	}

	emptyList := byName["empty-list"]
	list, ok := emptyList.Matches()[0].ListValue()
	if !ok || len(list) != 0 {
		t.Fatalf("empty list match = %#v (ok=%t)", list, ok)
	}

	directList := byName["list"]
	if directList.State() != typedoutput.FieldStateValues || len(directList.Matches()) != 1 {
		t.Fatalf("direct list cardinality = state %q matches %d, want one match", directList.State(), len(directList.Matches()))
	}
	direct, ok := directList.Matches()[0].ListValue()
	if !ok || !reflect.DeepEqual(direct, []any{map[string]any{"name": "first"}, "second"}) {
		t.Fatalf("direct list value = %#v (ok=%t)", direct, ok)
	}
	direct[0].(map[string]any)["name"] = "mutated"
	if next, _ := directList.Matches()[0].ListValue(); next[0].(map[string]any)["name"] != "first" {
		t.Fatal("list accessor exposed mutable outcome state")
	}

	wildcard := byName["wildcard-values"]
	if wildcard.State() != typedoutput.FieldStateValues || len(wildcard.Matches()) != 3 {
		t.Fatalf("wildcard cardinality = state %q matches %d, want three matches", wildcard.State(), len(wildcard.Matches()))
	}
	for index, want := range []string{"1", "bad", "3"} {
		value, ok := wildcard.Matches()[index].StringValue()
		if !ok || value != want {
			t.Fatalf("wildcard match %d = %q (ok=%t), want %q", index, value, ok, want)
		}
	}

	failure := byName["wildcard-failure"]
	if failure.State() != typedoutput.FieldStateError || len(failure.Matches()) != 0 || !typedoutput.HasReason(failure.Err(), typedoutput.ReasonInvalidValue) {
		t.Fatalf("multi-match failure = state %q matches %d err %v, want atomic field error", failure.State(), len(failure.Matches()), failure.Err())
	}
	if byName["empty-string"].State() != typedoutput.FieldStateValues || resources[1].Fields()[0].State() == typedoutput.FieldStateError {
		t.Fatal("field-local conversion failure discarded independent outcomes")
	}
}
