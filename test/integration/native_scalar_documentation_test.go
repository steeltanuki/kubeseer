package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertNativeScalarDocumentation(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..")
	apiPath := filepath.Join(root, "docs", "api-reference.md")
	architecturePath := filepath.Join(root, "docs", "concepts-and-architecture.md")
	apiBytes, err := os.ReadFile(apiPath)
	if err != nil {
		t.Fatalf("read API reference: %v", err)
	}
	architectureBytes, err := os.ReadFile(architecturePath)
	if err != nil {
		t.Fatalf("read architecture guide: %v", err)
	}
	apiText, architectureText := string(apiBytes), string(architectureBytes)

	for _, snippet := range []string{
		"### Native duration and quantity spellings",
		"`100n`",
		"`100u`",
		"`-100u`",
		"`\"0\"`",
		"`\"+0\"`",
		"`\"-0\"`",
		"`\"1us\"`",
		"`\"1µs\"`",
		"`\"1μs\"`",
		"U+00B5 MICRO SIGN",
		"U+03BC GREEK SMALL LETTER MU",
		"quantityValue.canonical",
		"quantityValue.baseUnits",
		"durationValue",
		"nanoseconds",
		"0.0000000001",
		"0.1ns",
	} {
		if !strings.Contains(apiText, snippet) {
			t.Fatalf("API reference omitted documented scalar snippet %q", snippet)
		}
	}
	for _, snippet := range []string{
		"### Exact native scalar conversion",
		"quantity `100n`",
		"`baseUnits: \"0.0000001\"`",
		"quantity `100u`",
		"duration strings `\"0\"`, `\"+0\"`, and `\"-0\"`",
		"ASCII `\"1us\"`",
		"U+00B5 MICRO SIGN",
		"U+03BC GREEK SMALL LETTER MU",
		"decimal (`1.5`)",
		"exponent (`1e3`)",
		"binary quantity (`1Gi`)",
		"compound duration (`1h2m3.004s`)",
		"0.0000000001",
		"0.1ns",
	} {
		if !strings.Contains(architectureText, snippet) {
			t.Fatalf("architecture guide omitted documented scalar snippet %q", snippet)
		}
	}
	if !strings.Contains(apiText, "(concepts-and-architecture.md#exact-native-scalar-conversion)") || !strings.Contains(architectureText, "(api-reference.md#native-duration-and-quantity-spellings)") {
		t.Fatal("cross-guide scalar fragment links are missing")
	}
	if !strings.Contains(architectureText, "## Exact native scalar conversion") || !strings.Contains(apiText, "## Operators") {
		t.Fatal("cross-guide fragment targets are missing")
	}
	if []rune("µs")[0] != '\u00B5' || []rune("μs")[0] != '\u03BC' || strings.Contains("µs", "μ") || strings.Contains("μs", "µ") {
		t.Fatal("documented microsecond spellings lost their distinct Unicode code points")
	}

	tests := []struct {
		name         string
		input        string
		typeName     v1alpha1.KubeseerValueType
		wantQuantity string
		wantDuration time.Duration
	}{
		{name: "quantity-nano", input: "100n", typeName: v1alpha1.ValueTypeQuantity, wantQuantity: "0.0000001"},
		{name: "quantity-micro", input: "100u", typeName: v1alpha1.ValueTypeQuantity, wantQuantity: "0.0001"},
		{name: "quantity-negative-micro", input: "-100u", typeName: v1alpha1.ValueTypeQuantity, wantQuantity: "-0.0001"},
		{name: "duration-zero", input: "0", typeName: v1alpha1.ValueTypeDuration, wantDuration: 0},
		{name: "duration-positive-zero", input: "+0", typeName: v1alpha1.ValueTypeDuration, wantDuration: 0},
		{name: "duration-negative-zero", input: "-0", typeName: v1alpha1.ValueTypeDuration, wantDuration: 0},
		{name: "duration-ascii-micro", input: "1us", typeName: v1alpha1.ValueTypeDuration, wantDuration: time.Microsecond},
		{name: "duration-micro-sign", input: "1µs", typeName: v1alpha1.ValueTypeDuration, wantDuration: time.Microsecond},
		{name: "duration-greek-mu", input: "1μs", typeName: v1alpha1.ValueTypeDuration, wantDuration: time.Microsecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			match, err := convertExtractedNativeScalar(t, test.input, test.typeName)
			if err != nil {
				t.Fatalf("convert documented scalar %q: %v", test.input, err)
			}
			if test.typeName == v1alpha1.ValueTypeQuantity {
				_, baseUnits, ok := match.QuantityValue()
				if !ok || baseUnits != test.wantQuantity {
					t.Fatalf("documented quantity %q normalized to %q (ok=%t), want %q", test.input, baseUnits, ok, test.wantQuantity)
				}
				return
			}
			got, ok := match.DurationValue()
			if !ok || got != test.wantDuration || got.String() != durationCanonical(test.wantDuration) {
				t.Fatalf("documented duration %q = %v (ok=%t), want %v/%q", test.input, got, ok, test.wantDuration, durationCanonical(test.wantDuration))
			}
		})
	}

	for _, test := range []struct {
		name     string
		input    string
		typeName v1alpha1.KubeseerValueType
	}{
		{name: "quantity-rounding", input: "0.0000000001", typeName: v1alpha1.ValueTypeQuantity},
		{name: "duration-rounding", input: "0.1ns", typeName: v1alpha1.ValueTypeDuration},
	} {
		t.Run(test.name, func(t *testing.T) {
			match, err := convertExtractedNativeScalar(t, test.input, test.typeName)
			if err == nil || !typedoutput.HasReason(err, typedoutput.ReasonForbiddenConversion) || !strings.Contains(err.Error(), "conversion would lose information") {
				t.Fatalf("documented rejected scalar %q = match=%#v err=%v", test.input, match, err)
			}
		})
	}

	source := v1alpha1.KubeseerSource{
		ID: "native-scalar-documentation",
		Fields: []v1alpha1.KubeseerField{
			{Name: "quantityNano", Path: "{.data.quantityNano}", Type: v1alpha1.ValueTypeQuantity},
			{Name: "quantityMicro", Path: "{.data.quantityMicro}", Type: v1alpha1.ValueTypeQuantity},
			{Name: "durationZero", Path: "{.data.durationZero}", Type: v1alpha1.ValueTypeDuration},
			{Name: "durationMicro", Path: "{.data.durationMicro}", Type: v1alpha1.ValueTypeDuration},
		},
	}
	extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
		Source: source,
		Selection: selection.SelectionOutcome{
			SourceID: source.ID,
			Resources: []selection.SelectedResource{selectedExtractionResourceWithObject(map[string]any{
				"data": map[string]any{"quantityNano": "100n", "quantityMicro": "100u", "durationZero": "-0", "durationMicro": "1μs"},
			})},
		},
	}})[0]
	if extracted.Err != nil {
		t.Fatalf("extract documented scalar payload: %v", extracted.Err)
	}
	typed := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: extracted}})
	public, err := typedoutput.BuildResult(typed)
	if err != nil {
		t.Fatalf("serialize documented scalar payload: %v", err)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal documented scalar payload: %v", err)
	}
	for _, field := range []string{"quantityNano", "quantityMicro", "durationZero", "durationMicro"} {
		if !strings.Contains(string(encoded), `"name":"`+field+`"`) {
			t.Fatalf("public documented payload lost field %q: %s", field, encoded)
		}
	}
	for _, snippet := range []string{`"type":"quantity"`, `"type":"duration"`, `"quantityValue"`, `"baseUnits":"0.0000001"`, `"baseUnits":"0.0001"`, `"canonical":"0s"`, `"nanoseconds":1000`} {
		if !strings.Contains(string(encoded), snippet) {
			t.Fatalf("public documented payload omitted %q: %s", snippet, encoded)
		}
	}
}

func durationCanonical(value time.Duration) string {
	if value == 0 {
		return "0s"
	}
	return value.String()
}
