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
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
	"sigs.k8s.io/yaml"
)

func assertTypedOutputSerializationScenarios(t *testing.T) {
	t.Helper()

	source := v1alpha1.KubeseerSource{
		ID: "serialization-source",
		Fields: []v1alpha1.KubeseerField{
			{Name: "empty", Path: "{.data.empty}", Type: v1alpha1.ValueTypeString},
			{Name: "integer-zero", Path: "{.data.integerZero}", Type: v1alpha1.ValueTypeInteger},
			{Name: "number", Path: "{.data.number}", Type: v1alpha1.ValueTypeNumber},
			{Name: "boolean-false", Path: "{.data.booleanFalse}", Type: v1alpha1.ValueTypeBoolean},
			{Name: "timestamp", Path: "{.data.timestamp}", Type: v1alpha1.ValueTypeTimestamp},
			{Name: "duration", Path: "{.data.duration}", Type: v1alpha1.ValueTypeDuration},
			{Name: "quantity", Path: "{.data.quantity}", Type: v1alpha1.ValueTypeQuantity},
			{Name: "object", Path: "{.data.object}", Type: v1alpha1.ValueTypeObject},
			{Name: "list", Path: "{.data.list}", Type: v1alpha1.ValueTypeList},
			{Name: "null", Path: "{.data.null}", Type: v1alpha1.ValueTypeString},
			{Name: "absent", Path: "{.data.absent}", Type: v1alpha1.ValueTypeString},
		},
	}
	plan := typedoutput.CompileSource(source)
	if len(plan.Failures()) != 0 {
		t.Fatalf("compile serialization source: %#v", plan.Failures())
	}
	extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
		Source: source,
		Selection: selection.SelectionOutcome{
			SourceID: source.ID,
			Resources: []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{
				"data": map[string]any{
					"empty":        "",
					"integerZero":  int64(0),
					"number":       json.Number("7.2500"),
					"booleanFalse": false,
					"timestamp":    "2026-08-26T16:30:00+02:00",
					"duration":     "1.5s",
					"quantity":     "1.5Gi",
					"object": map[string]any{
						"z":      json.Number("7.2500"),
						"a":      int64(1),
						"nested": map[string]any{"enabled": true},
					},
					"list": []any{
						map[string]any{"b": "two", "a": "one"},
						json.Number("2.00"),
					},
					"null": nil,
				},
			})},
		},
	}})[0]
	if extracted.Err != nil {
		t.Fatalf("extract serialization fixture: %v", extracted.Err)
	}

	typed := typedoutput.ConvertSource(plan, extracted)
	result, err := typedoutput.BuildResult([]typedoutput.SourceOutcome{typed})
	if err != nil {
		t.Fatalf("build structural typed result: %v", err)
	}
	if len(result.Sources) != 1 || result.Sources[0].ID != source.ID || result.Sources[0].State != v1alpha1.SourceStateValues || len(result.Sources[0].Resources) != 1 {
		t.Fatalf("structural result envelope = %#v", result)
	}
	resource := result.Sources[0].Resources[0]
	if resource.APIVersion != "v1" || resource.Kind != "ConfigMap" || resource.Namespace != "team-a" || resource.Name != "resource-a" || resource.UID != "uid-a" {
		t.Fatalf("structural provenance = %#v", resource)
	}

	byName := make(map[string]v1alpha1.KubeseerFieldResult, len(resource.Fields))
	for _, field := range resource.Fields {
		byName[field.Name] = field
	}
	if field := byName["absent"]; field.State != v1alpha1.FieldStateAbsent || len(field.Matches) != 0 || field.Type != v1alpha1.ValueTypeString {
		t.Fatalf("public absent field = %#v", field)
	}
	if field := byName["null"]; field.State != v1alpha1.FieldStateValues || len(field.Matches) != 1 || field.Matches[0].State != v1alpha1.MatchStateNull || hasTypedPayload(field.Matches[0]) {
		t.Fatalf("public null field = %#v", field)
	}
	if field := byName["empty"]; field.Matches[0].StringValue == nil || *field.Matches[0].StringValue != "" {
		t.Fatalf("empty string pointer was lost: %#v", field)
	}
	if field := byName["integer-zero"]; field.Matches[0].IntegerValue == nil || *field.Matches[0].IntegerValue != 0 {
		t.Fatalf("zero integer pointer was lost: %#v", field)
	}
	if field := byName["boolean-false"]; field.Matches[0].BooleanValue == nil || *field.Matches[0].BooleanValue {
		t.Fatalf("false boolean pointer was lost: %#v", field)
	}
	if field := byName["number"]; field.Matches[0].NumberValue == nil || *field.Matches[0].NumberValue != "7.25" {
		t.Fatalf("canonical number = %#v, want 7.25", field)
	}
	if field := byName["timestamp"]; field.Matches[0].TimestampValue == nil || !field.Matches[0].TimestampValue.Time.Equal(time.Date(2026, time.August, 26, 14, 30, 0, 0, time.UTC)) || field.Matches[0].TimestampValue.Location() != time.UTC {
		t.Fatalf("normalized timestamp = %#v", field)
	}
	if field := byName["duration"]; field.Matches[0].DurationValue == nil || field.Matches[0].DurationValue.Canonical != "1.5s" || field.Matches[0].DurationValue.Nanoseconds != 1500000000 {
		t.Fatalf("duration payload = %#v", field)
	}
	if field := byName["quantity"]; field.Matches[0].QuantityValue == nil || field.Matches[0].QuantityValue.Canonical != "1536Mi" || field.Matches[0].QuantityValue.BaseUnits != "1610612736" {
		t.Fatalf("quantity payload = %#v", field)
	}
	if field := byName["object"]; field.Matches[0].ObjectValue == nil || *field.Matches[0].ObjectValue != `{"a":1,"nested":{"enabled":true},"z":7.25}` {
		t.Fatalf("canonical object = %#v", field)
	}
	if field := byName["list"]; field.Matches[0].ListValue == nil || *field.Matches[0].ListValue != `[{"a":"one","b":"two"},2]` {
		t.Fatalf("canonical list = %#v", field)
	}
	for _, field := range resource.Fields {
		for _, match := range field.Matches {
			if match.State == v1alpha1.MatchStateValue && countTypedPayloads(match) != 1 {
				t.Fatalf("value match does not have exactly one payload: %#v", match)
			}
		}
	}

	encodedJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal typed result JSON: %v", err)
	}
	if strings.Contains(string(encodedJSON), `"value":`) || strings.Contains(string(encodedJSON), `"originalValue":`) {
		t.Fatalf("typed result contains an undeclared original-value payload: %s", encodedJSON)
	}
	var decodedJSON v1alpha1.KubeseerResult
	if err := json.Unmarshal(encodedJSON, &decodedJSON); err != nil {
		t.Fatalf("unmarshal typed result JSON: %v", err)
	}
	if !typedResultSemanticallyEqualForIntegration(result, decodedJSON) {
		t.Fatalf("typed result JSON round-trip changed semantics: original=%#v decoded=%#v", result, decodedJSON)
	}

	encodedYAML, err := yaml.Marshal(result)
	if err != nil {
		t.Fatalf("marshal typed result YAML: %v", err)
	}
	var decodedYAML v1alpha1.KubeseerResult
	if err := yaml.Unmarshal(encodedYAML, &decodedYAML); err != nil {
		t.Fatalf("unmarshal typed result YAML: %v", err)
	}
	if !typedResultSemanticallyEqualForIntegration(result, decodedYAML) {
		t.Fatalf("typed result YAML round-trip changed semantics: original=%#v decoded=%#v", result, decodedYAML)
	}

	if _, err := typedoutput.BuildResult([]typedoutput.SourceOutcome{{}}); err == nil {
		t.Fatal("invalid empty source union was accepted")
	}

	planningSource := v1alpha1.KubeseerSource{ID: "planning-failure-source", Fields: []v1alpha1.KubeseerField{{Name: "missing", Path: "{.data.value}"}}}
	planningPlan := typedoutput.CompileSource(planningSource)
	planningExtracted := extraction.SourceOutcome{SourceID: planningSource.ID}
	planningResult, err := typedoutput.BuildResult([]typedoutput.SourceOutcome{typedoutput.ConvertSource(planningPlan, planningExtracted)})
	if err != nil || len(planningResult.Sources) != 1 || len(planningResult.Sources[0].FieldErrors) != 1 || planningResult.Sources[0].FieldErrors[0].Reason != string(typedoutput.ReasonMissingType) {
		t.Fatalf("planning error status = result=%#v err=%v", planningResult, err)
	}

	failingSource := v1alpha1.KubeseerSource{ID: "runtime-failure-source", Fields: []v1alpha1.KubeseerField{{Name: "bad", Path: "{.data.bad}", Type: v1alpha1.ValueTypeInteger}, {Name: "good", Path: "{.data.good}", Type: v1alpha1.ValueTypeString}}}
	failingPlan := typedoutput.CompileSource(failingSource)
	failingExtracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
		Source: failingSource,
		Selection: selection.SelectionOutcome{
			SourceID: failingSource.ID,
			Resources: []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{
				"data": map[string]any{"bad": "not-an-integer", "good": "preserved"},
			})},
		},
	}})[0]
	failedResult, err := typedoutput.BuildResult([]typedoutput.SourceOutcome{typedoutput.ConvertSource(failingPlan, failingExtracted)})
	if err != nil || len(failedResult.Sources[0].Resources) != 1 || len(failedResult.Sources[0].Resources[0].Fields) != 2 {
		t.Fatalf("runtime failure status = result=%#v err=%v", failedResult, err)
	}
}

func countTypedPayloads(match v1alpha1.KubeseerTypedMatch) int {
	count := 0
	if match.StringValue != nil {
		count++
	}
	if match.IntegerValue != nil {
		count++
	}
	if match.NumberValue != nil {
		count++
	}
	if match.BooleanValue != nil {
		count++
	}
	if match.TimestampValue != nil {
		count++
	}
	if match.DurationValue != nil {
		count++
	}
	if match.QuantityValue != nil {
		count++
	}
	if match.ObjectValue != nil {
		count++
	}
	if match.ListValue != nil {
		count++
	}
	return count
}

func hasTypedPayload(match v1alpha1.KubeseerTypedMatch) bool {
	return countTypedPayloads(match) != 0
}

func typedResultSemanticallyEqualForIntegration(left, right v1alpha1.KubeseerResult) bool {
	leftCopy := *left.DeepCopy()
	rightCopy := *right.DeepCopy()
	for _, result := range []*v1alpha1.KubeseerResult{&leftCopy, &rightCopy} {
		for sourceIndex := range result.Sources {
			for resourceIndex := range result.Sources[sourceIndex].Resources {
				for fieldIndex := range result.Sources[sourceIndex].Resources[resourceIndex].Fields {
					for matchIndex := range result.Sources[sourceIndex].Resources[resourceIndex].Fields[fieldIndex].Matches {
						timestamp := result.Sources[sourceIndex].Resources[resourceIndex].Fields[fieldIndex].Matches[matchIndex].TimestampValue
						if timestamp != nil {
							timestamp.Time = timestamp.Time.UTC()
						}
					}
				}
			}
		}
	}
	return reflect.DeepEqual(leftCopy, rightCopy)
}
