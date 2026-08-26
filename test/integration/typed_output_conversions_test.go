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
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertTypedOutputConversionScenarios(t *testing.T) {
	t.Helper()

	t.Run("planning validates every type before conversion and keeps valid fields", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID: "planning-source",
			Fields: []v1alpha1.KubeseerField{
				{Name: "zeta", Path: "{.data.zeta}", Type: v1alpha1.ValueTypeString},
				{Name: "missing", Path: "{.data.missing}"},
				{Name: "unsupported", Path: "{.data.unsupported}", Type: v1alpha1.KubeseerValueType("decimal")},
			},
		}
		outcome := typedoutput.CompileSource(source)
		if outcome.SourceID() != source.ID {
			t.Fatalf("planning source ID = %q, want %q", outcome.SourceID(), source.ID)
		}
		fields := outcome.Plan().Fields()
		if len(fields) != 1 || fields[0].Name() != "zeta" || fields[0].Type() != v1alpha1.ValueTypeString {
			t.Fatalf("valid plan fields = %#v, want only sorted valid field", fields)
		}
		failures := outcome.Failures()
		if len(failures) != 2 {
			t.Fatalf("planning failures = %#v, want one per invalid field", failures)
		}
		if failures[0].FieldName != "missing" || failures[0].Reason != typedoutput.ReasonMissingType || failures[1].FieldName != "unsupported" || failures[1].Reason != typedoutput.ReasonUnsupportedType {
			t.Fatalf("planning failures lost field order or stable reasons: %#v", failures)
		}
		for _, failure := range failures {
			if failure.SourceID != source.ID || failure.Provenance != nil {
				t.Fatalf("planning failure identity = %#v, want source only", failure)
			}
		}
		if !strings.Contains(failures[0].Error(), source.ID) || !strings.Contains(failures[0].Error(), failures[0].FieldName) {
			t.Fatalf("planning diagnostic lost identity: %v", failures[0])
		}

		permuted := source.DeepCopy()
		permuted.Fields[0], permuted.Fields[1], permuted.Fields[2] = permuted.Fields[2], permuted.Fields[0], permuted.Fields[1]
		permutedOutcome := typedoutput.CompileSource(*permuted)
		if !reflect.DeepEqual(outcome.Plan().Fields(), permutedOutcome.Plan().Fields()) || len(outcome.Failures()) != len(permutedOutcome.Failures()) {
			t.Fatalf("equivalent declarations produced different plans: first=%#v second=%#v", outcome, permutedOutcome)
		}
		permuted.Fields[0].Name = "mutated"
		if outcome.Plan().Fields()[0].Name() != "zeta" {
			t.Fatal("plan retained mutable source declaration state")
		}
	})

	t.Run("selection and extraction feed the approved scalar conversion matrix", func(t *testing.T) {
		resourceObject := selectedExtractionResourceWithObject(map[string]any{
			"data": map[string]any{
				"string":         "hello",
				"integer-native": int64(-42),
				"integer-text":   "17",
				"number-native":  float64(3.5),
				"number-text":    "1.25e2",
				"boolean-native": true,
				"boolean-text":   "false",
				"timestamp":      "2026-08-26T16:30:00+02:00",
				"duration":       "1.5s",
				"quantity":       "1.5Gi",
			},
		})
		tests := []struct {
			name      string
			path      string
			typeName  v1alpha1.KubeseerValueType
			wantValue any
		}{
			{name: "string", path: "{.data.string}", typeName: v1alpha1.ValueTypeString, wantValue: "hello"},
			{name: "native integer", path: "{.data['integer-native']}", typeName: v1alpha1.ValueTypeInteger, wantValue: int64(-42)},
			{name: "text integer", path: "{.data['integer-text']}", typeName: v1alpha1.ValueTypeInteger, wantValue: int64(17)},
			{name: "native number", path: "{.data['number-native']}", typeName: v1alpha1.ValueTypeNumber, wantValue: "3.5"},
			{name: "text number", path: "{.data['number-text']}", typeName: v1alpha1.ValueTypeNumber, wantValue: "125"},
			{name: "native boolean", path: "{.data['boolean-native']}", typeName: v1alpha1.ValueTypeBoolean, wantValue: true},
			{name: "text boolean", path: "{.data['boolean-text']}", typeName: v1alpha1.ValueTypeBoolean, wantValue: false},
			{name: "timestamp", path: "{.data.timestamp}", typeName: v1alpha1.ValueTypeTimestamp, wantValue: time.Date(2026, time.August, 26, 14, 30, 0, 0, time.UTC)},
			{name: "duration", path: "{.data.duration}", typeName: v1alpha1.ValueTypeDuration, wantValue: 1500000000 * time.Nanosecond},
			{name: "quantity", path: "{.data.quantity}", typeName: v1alpha1.ValueTypeQuantity, wantValue: "1610612736"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				source := v1alpha1.KubeseerSource{ID: "conversion-source", Fields: []v1alpha1.KubeseerField{{Name: "value", Path: test.path, Type: test.typeName}}}
				planOutcome := typedoutput.CompileSource(source)
				if len(planOutcome.Failures()) != 0 {
					t.Fatalf("compile %q failures = %#v", test.name, planOutcome.Failures())
				}
				extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
					Source:    source,
					Selection: selection.SelectionOutcome{SourceID: source.ID, Resources: []selection.SelectedResource{resourceObject}},
				}})[0]
				if extracted.Err != nil || len(extracted.Resources) != 1 || len(extracted.Resources[0].Fields) != 1 || extracted.Resources[0].Fields[0].Matches.Len() != 1 {
					t.Fatalf("selection-to-extraction outcome = %#v", extracted)
				}
				native := extracted.Resources[0].Fields[0].Matches.Values()[0]
				match, err := typedoutput.ConvertMatch(planOutcome.Plan().Fields()[0], native, &extracted.Resources[0].Provenance)
				if err != nil {
					t.Fatalf("convert %q native value %#v: %v", test.name, native, err)
				}
				if match.Type() != test.typeName || match.IsNull() {
					t.Fatalf("converted %q match = type %q null=%t", test.name, match.Type(), match.IsNull())
				}
				assertTypedMatchValue(t, match, test.typeName, test.wantValue)
			})
		}

		jsonNumber := json.Number("7.2500")
		plan := mustTypedFieldPlan(t, "json-number", v1alpha1.ValueTypeNumber)
		match, err := typedoutput.ConvertMatch(plan, jsonNumber, nil)
		if err != nil {
			t.Fatalf("convert json.Number: %v", err)
		}
		if got, ok := match.NumberValue(); !ok || got != "7.25" {
			t.Fatalf("json.Number conversion = %q (ok=%t), want 7.25", got, ok)
		}
	})

	t.Run("forbidden conversions and malformed values have stable sanitized reasons", func(t *testing.T) {
		tests := []struct {
			name     string
			typeName v1alpha1.KubeseerValueType
			native   any
			reason   typedoutput.ConversionErrorReason
		}{
			{name: "integer type mismatch", typeName: v1alpha1.ValueTypeInteger, native: true, reason: typedoutput.ReasonConversionTypeMismatch},
			{name: "integer malformed text", typeName: v1alpha1.ValueTypeInteger, native: "01", reason: typedoutput.ReasonInvalidValue},
			{name: "integer text overflow", typeName: v1alpha1.ValueTypeInteger, native: "9223372036854775808", reason: typedoutput.ReasonConversionOverflow},
			{name: "integer fractional float", typeName: v1alpha1.ValueTypeInteger, native: 1.5, reason: typedoutput.ReasonForbiddenConversion},
			{name: "integer NaN", typeName: v1alpha1.ValueTypeInteger, native: math.NaN(), reason: typedoutput.ReasonInvalidValue},
			{name: "number type mismatch", typeName: v1alpha1.ValueTypeNumber, native: true, reason: typedoutput.ReasonConversionTypeMismatch},
			{name: "number malformed text", typeName: v1alpha1.ValueTypeNumber, native: "1.", reason: typedoutput.ReasonInvalidValue},
			{name: "number infinity", typeName: v1alpha1.ValueTypeNumber, native: math.Inf(1), reason: typedoutput.ReasonInvalidValue},
			{name: "number decimal overflow", typeName: v1alpha1.ValueTypeNumber, native: "1e2000000", reason: typedoutput.ReasonConversionOverflow},
			{name: "boolean malformed text", typeName: v1alpha1.ValueTypeBoolean, native: "TRUE", reason: typedoutput.ReasonInvalidValue},
			{name: "boolean type mismatch", typeName: v1alpha1.ValueTypeBoolean, native: int64(1), reason: typedoutput.ReasonConversionTypeMismatch},
			{name: "timestamp malformed", typeName: v1alpha1.ValueTypeTimestamp, native: "2026-08-26T16:30:00", reason: typedoutput.ReasonInvalidValue},
			{name: "timestamp type mismatch", typeName: v1alpha1.ValueTypeTimestamp, native: true, reason: typedoutput.ReasonConversionTypeMismatch},
			{name: "duration malformed", typeName: v1alpha1.ValueTypeDuration, native: "forever", reason: typedoutput.ReasonInvalidValue},
			{name: "duration subnanosecond", typeName: v1alpha1.ValueTypeDuration, native: "0.0000000001s", reason: typedoutput.ReasonForbiddenConversion},
			{name: "duration overflow", typeName: v1alpha1.ValueTypeDuration, native: "2562047h47m16.854775808s", reason: typedoutput.ReasonConversionOverflow},
			{name: "quantity malformed", typeName: v1alpha1.ValueTypeQuantity, native: "not-a-quantity", reason: typedoutput.ReasonInvalidValue},
			{name: "quantity precision loss", typeName: v1alpha1.ValueTypeQuantity, native: "0.0000000001", reason: typedoutput.ReasonForbiddenConversion},
			{name: "quantity overflow", typeName: v1alpha1.ValueTypeQuantity, native: "9223372036854775808", reason: typedoutput.ReasonConversionOverflow},
			{name: "object type mismatch", typeName: v1alpha1.ValueTypeObject, native: "object", reason: typedoutput.ReasonConversionTypeMismatch},
			{name: "list type mismatch", typeName: v1alpha1.ValueTypeList, native: map[string]any{}, reason: typedoutput.ReasonConversionTypeMismatch},
			{name: "implicit stringification", typeName: v1alpha1.ValueTypeString, native: int64(1), reason: typedoutput.ReasonConversionTypeMismatch},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				plan := mustTypedFieldPlan(t, "sensitive-field", test.typeName)
				first, err := typedoutput.ConvertMatch(plan, test.native, &selection.Provenance{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "sensitive-resource", UID: "uid-sensitive"})
				if err == nil || first.Type() != "" || !typedoutput.HasReason(err, test.reason) {
					t.Fatalf("conversion error = match %#v err %v, want reason %s", first, err, test.reason)
				}
				var conversionErr *typedoutput.ConversionError
				if !errors.As(err, &conversionErr) || conversionErr.SourceID != "conversion-source" || conversionErr.FieldName != "sensitive-field" || conversionErr.Provenance == nil {
					t.Fatalf("conversion error lost scoped identity: %#v", err)
				}
				diagnostic := err.Error()
				for _, secret := range []string{"sensitive-field", "TOP_SECRET_VALUE", "not-a-quantity", "{.data.secret}"} {
					if secret != "sensitive-field" && strings.Contains(diagnostic, secret) {
						t.Fatalf("diagnostic exposed sensitive input %q: %v", secret, err)
					}
				}
				_, secondErr := typedoutput.ConvertMatch(plan, test.native, &selection.Provenance{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team-a", Name: "sensitive-resource", UID: "uid-sensitive"})
				if secondErr == nil || secondErr.Error() != err.Error() || !typedoutput.HasReason(secondErr, test.reason) {
					t.Fatalf("equivalent conversion error is unstable: first=%v second=%v", err, secondErr)
				}
			})
		}
	})
}

func mustTypedFieldPlan(t *testing.T, name string, typeName v1alpha1.KubeseerValueType) typedoutput.FieldPlan {
	t.Helper()
	outcome := typedoutput.CompileSource(v1alpha1.KubeseerSource{ID: "conversion-source", Fields: []v1alpha1.KubeseerField{{Name: name, Path: "{.data.value}", Type: typeName}}})
	if len(outcome.Failures()) != 0 || len(outcome.Plan().Fields()) != 1 {
		t.Fatalf("compile typed field plan %q: failures=%#v fields=%#v", name, outcome.Failures(), outcome.Plan().Fields())
	}
	return outcome.Plan().Fields()[0]
}

func assertTypedMatchValue(t *testing.T, match typedoutput.Match, typeName v1alpha1.KubeseerValueType, want any) {
	t.Helper()
	switch typeName {
	case v1alpha1.ValueTypeString:
		got, ok := match.StringValue()
		if !ok || got != want.(string) {
			t.Fatalf("string match = %q (ok=%t), want %q", got, ok, want)
		}
	case v1alpha1.ValueTypeInteger:
		got, ok := match.IntegerValue()
		if !ok || got != want.(int64) {
			t.Fatalf("integer match = %d (ok=%t), want %d", got, ok, want)
		}
	case v1alpha1.ValueTypeNumber:
		got, ok := match.NumberValue()
		if !ok || got != want.(string) {
			t.Fatalf("number match = %q (ok=%t), want %q", got, ok, want)
		}
	case v1alpha1.ValueTypeBoolean:
		got, ok := match.BooleanValue()
		if !ok || got != want.(bool) {
			t.Fatalf("boolean match = %t (ok=%t), want %t", got, ok, want)
		}
	case v1alpha1.ValueTypeTimestamp:
		got, ok := match.TimestampValue()
		if !ok || !got.Equal(want.(time.Time)) || got.Location() != time.UTC {
			t.Fatalf("timestamp match = %v (location %v, ok=%t), want UTC %v", got, got.Location(), ok, want)
		}
	case v1alpha1.ValueTypeDuration:
		got, ok := match.DurationValue()
		if !ok || got != want.(time.Duration) {
			t.Fatalf("duration match = %v (ok=%t), want %v", got, ok, want)
		}
	case v1alpha1.ValueTypeQuantity:
		got, baseUnits, ok := match.QuantityValue()
		if !ok || got.String() != "1536Mi" || baseUnits != want.(string) {
			t.Fatalf("quantity match = %q baseUnits=%q (ok=%t), want 1536Mi/%q", got.String(), baseUnits, ok, want)
		}
	default:
		t.Fatalf("assertTypedMatchValue called for unsupported type %q", typeName)
	}
}
