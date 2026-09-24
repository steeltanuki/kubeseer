package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func assertNativeScalarPipelineScenarios(t *testing.T) {
	t.Helper()

	t.Run("extraction conversion and public projection preserve isolation", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID: "native-scalar-pipeline",
			Fields: []v1alpha1.KubeseerField{
				{Name: "quantity-nano", Path: "{.data.quantityNano}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "quantity-micro", Path: "{.data.quantityMicro}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "duration-zero", Path: "{.data.durationZero}", Type: v1alpha1.ValueTypeDuration},
				{Name: "duration-micro", Path: "{.data.durationMicro}", Type: v1alpha1.ValueTypeDuration},
				{Name: "quantity-mixed", Path: "{.data.quantityMixed[*]}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "duration-mixed", Path: "{.data.durationMixed[*]}", Type: v1alpha1.ValueTypeDuration},
				{Name: "quantity-decimal", Path: "{.data.quantityDecimal}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "quantity-exponent", Path: "{.data.quantityExponent}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "quantity-binary", Path: "{.data.quantityBinary}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "duration-compound", Path: "{.data.durationCompound}", Type: v1alpha1.ValueTypeDuration},
				{Name: "quantity-absent", Path: "{.data.quantityAbsent}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "quantity-null", Path: "{.data.quantityNull}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "duration-absent", Path: "{.data.durationAbsent}", Type: v1alpha1.ValueTypeDuration},
				{Name: "duration-null", Path: "{.data.durationNull}", Type: v1alpha1.ValueTypeDuration},
			},
		}

		first := selectedExtractionResourceWithObject(map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"data": map[string]any{
				"quantityNano":     "100n",
				"quantityMicro":    "100u",
				"durationZero":     "-0",
				"durationMicro":    "1μs",
				"quantityMixed":    []any{"100n", "TOP_SECRET_NATIVE", "100u"},
				"durationMixed":    []any{"0", "0.1ns", "1μs"},
				"quantityDecimal":  "1.5",
				"quantityExponent": "1e3",
				"quantityBinary":   "1Gi",
				"durationCompound": "1h2m3.004s",
				"quantityNull":     nil,
				"durationNull":     nil,
				"resourceSecret":   "RESOURCE_BODY_SECRET",
			},
		})
		first.Provenance.Name, first.Provenance.UID = "native-first", "native-first-uid"
		second := selectedExtractionResourceWithObject(map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"data": map[string]any{
				"quantityNano":     "-100n",
				"quantityMicro":    "-100u",
				"durationZero":     "+0",
				"durationMicro":    "1µs",
				"quantityMixed":    []any{"100n", "100u"},
				"durationMixed":    []any{"0", "1μs"},
				"quantityDecimal":  "1.5",
				"quantityExponent": "1e3",
				"quantityBinary":   "1Gi",
				"durationCompound": "1h2m3.004s",
				"quantityNull":     nil,
				"durationNull":     nil,
			},
		})
		second.Provenance.Name, second.Provenance.UID = "native-second", "native-second-uid"

		extracted := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{
			Source: source,
			Selection: selection.SelectionOutcome{
				SourceID:  source.ID,
				Resources: []selection.SelectedResource{first, second},
			},
		}})[0]
		if extracted.Err != nil || len(extracted.Resources) != 2 {
			t.Fatalf("extract mixed native scalar resources: %#v", extracted)
		}
		converted := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: extracted}})
		if len(converted) != 1 || converted[0].Err() != nil || len(converted[0].Resources()) != 2 {
			t.Fatalf("convert mixed native scalar resources: %#v", converted)
		}

		firstFields := nativeScalarFieldsByName(converted[0].Resources()[0].Fields())
		secondFields := nativeScalarFieldsByName(converted[0].Resources()[1].Fields())
		for _, name := range []string{"quantity-nano", "quantity-micro", "duration-zero", "duration-micro", "quantity-mixed", "duration-mixed", "quantity-decimal", "quantity-exponent", "quantity-binary", "duration-compound", "quantity-absent", "quantity-null", "duration-absent", "duration-null"} {
			if _, ok := firstFields[name]; !ok {
				t.Fatalf("first resource lost declared field %q: %#v", name, firstFields)
			}
		}
		for name, field := range firstFields {
			if field.Type() != sourceFieldType(source, name) {
				t.Fatalf("field %q type tag = %q, want %q", name, field.Type(), sourceFieldType(source, name))
			}
		}

		assertNativeScalarQuantity(t, firstFields["quantity-nano"], "0.0000001")
		assertNativeScalarQuantity(t, firstFields["quantity-micro"], "0.0001")
		assertNativeScalarDuration(t, firstFields["duration-zero"], 0)
		assertNativeScalarDuration(t, firstFields["duration-micro"], time.Microsecond)
		if firstFields["quantity-mixed"].State() != typedoutput.FieldStateError || len(firstFields["quantity-mixed"].Matches()) != 0 || !typedoutput.HasReason(firstFields["quantity-mixed"].Err(), typedoutput.ReasonInvalidValue) {
			t.Fatalf("failed quantity field retained matches or changed reason: %#v", firstFields["quantity-mixed"])
		}
		if firstFields["duration-mixed"].State() != typedoutput.FieldStateError || len(firstFields["duration-mixed"].Matches()) != 0 || !typedoutput.HasReason(firstFields["duration-mixed"].Err(), typedoutput.ReasonForbiddenConversion) {
			t.Fatalf("failed duration field retained matches or changed reason: %#v", firstFields["duration-mixed"])
		}
		assertNativeScalarQuantity(t, firstFields["quantity-decimal"], "1.5")
		assertNativeScalarQuantity(t, firstFields["quantity-exponent"], "1000")
		assertNativeScalarQuantity(t, firstFields["quantity-binary"], "1073741824")
		assertNativeScalarDuration(t, firstFields["duration-compound"], time.Hour+2*time.Minute+3*time.Second+4*time.Millisecond)
		if firstFields["quantity-absent"].State() != typedoutput.FieldStateAbsent || len(firstFields["quantity-absent"].Matches()) != 0 || firstFields["quantity-absent"].Err() != nil {
			t.Fatalf("quantity absence semantics changed: %#v", firstFields["quantity-absent"])
		}
		if firstFields["quantity-null"].State() != typedoutput.FieldStateValues || len(firstFields["quantity-null"].Matches()) != 1 || !firstFields["quantity-null"].Matches()[0].IsNull() {
			t.Fatalf("quantity null semantics changed: %#v", firstFields["quantity-null"])
		}
		if firstFields["duration-absent"].State() != typedoutput.FieldStateAbsent || firstFields["duration-null"].State() != typedoutput.FieldStateValues || len(firstFields["duration-null"].Matches()) != 1 || !firstFields["duration-null"].Matches()[0].IsNull() {
			t.Fatalf("duration absence/null semantics changed: absent=%#v null=%#v", firstFields["duration-absent"], firstFields["duration-null"])
		}

		assertNativeScalarQuantity(t, secondFields["quantity-nano"], "-0.0000001")
		assertNativeScalarQuantity(t, secondFields["quantity-micro"], "-0.0001")
		assertNativeScalarDuration(t, secondFields["duration-zero"], 0)
		assertNativeScalarDuration(t, secondFields["duration-micro"], time.Microsecond)
		if secondFields["quantity-mixed"].State() != typedoutput.FieldStateValues || len(secondFields["quantity-mixed"].Matches()) != 2 || secondFields["duration-mixed"].State() != typedoutput.FieldStateValues || len(secondFields["duration-mixed"].Matches()) != 2 {
			t.Fatalf("successful sibling resource fields changed: quantity=%#v duration=%#v", secondFields["quantity-mixed"], secondFields["duration-mixed"])
		}

		public, err := typedoutput.BuildResult(converted)
		if err != nil || len(public.Sources) != 1 || len(public.Sources[0].Resources) != 2 {
			t.Fatalf("build public native scalar result: result=%#v err=%v", public, err)
		}
		publicFields := make(map[string]v1alpha1.KubeseerFieldResult, len(public.Sources[0].Resources[0].Fields))
		for _, field := range public.Sources[0].Resources[0].Fields {
			publicFields[field.Name] = field
			if field.Type != sourceFieldType(source, field.Name) {
				t.Fatalf("public field %q type tag = %q, want %q", field.Name, field.Type, sourceFieldType(source, field.Name))
			}
		}
		if publicFields["quantity-mixed"].State != v1alpha1.FieldStateError || len(publicFields["quantity-mixed"].Matches) != 0 || publicFields["quantity-mixed"].Error == nil || publicFields["quantity-mixed"].Error.Reason != string(typedoutput.ReasonInvalidValue) {
			t.Fatalf("public failed quantity field = %#v", publicFields["quantity-mixed"])
		}
		if publicFields["duration-mixed"].State != v1alpha1.FieldStateError || len(publicFields["duration-mixed"].Matches) != 0 || publicFields["duration-mixed"].Error == nil || publicFields["duration-mixed"].Error.Reason != string(typedoutput.ReasonForbiddenConversion) {
			t.Fatalf("public failed duration field = %#v", publicFields["duration-mixed"])
		}
		if publicFields["quantity-absent"].State != v1alpha1.FieldStateAbsent || publicFields["quantity-absent"].Type != v1alpha1.ValueTypeQuantity {
			t.Fatalf("public absent quantity field = %#v", publicFields["quantity-absent"])
		}
		if publicFields["quantity-null"].State != v1alpha1.FieldStateValues || len(publicFields["quantity-null"].Matches) != 1 || publicFields["quantity-null"].Matches[0].State != v1alpha1.MatchStateNull || publicFields["quantity-null"].Matches[0].QuantityValue != nil {
			t.Fatalf("public null quantity field = %#v", publicFields["quantity-null"])
		}
		if publicFields["quantity-nano"].Matches[0].QuantityValue == nil || publicFields["quantity-nano"].Matches[0].QuantityValue.BaseUnits != "0.0000001" || publicFields["duration-micro"].Matches[0].DurationValue == nil || publicFields["duration-micro"].Matches[0].DurationValue.Nanoseconds != int64(time.Microsecond) {
			t.Fatalf("public corrected scalar payloads = quantity=%#v duration=%#v", publicFields["quantity-nano"], publicFields["duration-micro"])
		}

		encoded, err := json.Marshal(public)
		if err != nil {
			t.Fatalf("marshal public native scalar result: %v", err)
		}
		for _, forbidden := range []string{"TOP_SECRET_NATIVE", "RESOURCE_BODY_SECRET", "{.data.quantityMixed[*]}", "quantities must match", "fractional nanoseconds"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("public scalar diagnostics exposed %q: %s", forbidden, encoded)
			}
		}
	})

	t.Run("operators and aggregations consume corrected scalars", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID: "native-scalar-consumers",
			Fields: []v1alpha1.KubeseerField{
				{Name: "quantity", Path: "{.data.quantity}", Type: v1alpha1.ValueTypeQuantity, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorGte, Value: quantityOperand("100n")}}},
				{Name: "duration", Path: "{.data.duration}", Type: v1alpha1.ValueTypeDuration, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorGte, Value: durationOperand("1μs")}}},
				{Name: "quantity-decimal", Path: "{.data.quantityDecimal}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "quantity-exponent", Path: "{.data.quantityExponent}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "quantity-binary", Path: "{.data.quantityBinary}", Type: v1alpha1.ValueTypeQuantity},
				{Name: "duration-compound", Path: "{.data.durationCompound}", Type: v1alpha1.ValueTypeDuration},
			},
			Aggregations: []v1alpha1.KubeseerAggregation{
				{Name: "quantity-sum", Function: v1alpha1.AggregationSum, Field: "quantity"},
				{Name: "duration-sum", Function: v1alpha1.AggregationSum, Field: "duration"},
			},
		}
		resources := []selection.SelectedResource{
			aggregationSelectedResource("team-a", "consumer-one", "consumer-one-uid", map[string]any{"quantity": "100u", "duration": "1μs", "quantityDecimal": "1.5", "quantityExponent": "1e3", "quantityBinary": "1Gi", "durationCompound": "1h2m3.004s"}),
			aggregationSelectedResource("team-a", "consumer-two", "consumer-two-uid", map[string]any{"quantity": "100n", "duration": "1µs", "quantityDecimal": "1.5", "quantityExponent": "1e3", "quantityBinary": "1Gi", "durationCompound": "1h2m3.004s"}),
		}
		operatorOutcome, _ := evaluateOperatorSource(t, source, resources)
		if operatorOutcome.Err() != nil || len(operatorOutcome.Resources()) != 2 || operatorOutcome.Resources()[0].State() != operators.ResourceAccepted || operatorOutcome.Resources()[1].State() != operators.ResourceAccepted {
			t.Fatalf("corrected scalar operator outcome = %#v", operatorOutcome)
		}
		operatorPublic, err := operators.BuildResult([]operators.SourceOutcome{operatorOutcome})
		if err != nil || len(operatorPublic.Sources) != 1 || len(operatorPublic.Sources[0].Resources) != 2 {
			t.Fatalf("project corrected scalar operator result: result=%#v err=%v", operatorPublic, err)
		}
		if operatorPublic.Sources[0].Resources[0].Fields[0].Type != v1alpha1.ValueTypeDuration || operatorPublic.Sources[0].Resources[1].Fields[0].Type != v1alpha1.ValueTypeDuration {
			t.Fatalf("operator field type tags changed: %#v", operatorPublic.Sources[0].Resources)
		}

		aggregateOutcome := evaluateAggregationSource(t, source, resources, aggregation.Limits{})
		if aggregateOutcome.Err() != nil || len(aggregateOutcome.Aggregates()) != 2 {
			t.Fatalf("corrected scalar aggregation outcome = %#v", aggregateOutcome)
		}
		aggregatePublic, err := aggregation.BuildResultChecked([]aggregation.SourceOutcome{aggregateOutcome})
		if err != nil || len(aggregatePublic.Sources) != 1 || len(aggregatePublic.Sources[0].Aggregates) != 2 {
			t.Fatalf("project corrected scalar aggregate result: result=%#v err=%v", aggregatePublic, err)
		}
		byName := make(map[string]v1alpha1.KubeseerAggregateResult, len(aggregatePublic.Sources[0].Aggregates))
		for _, aggregate := range aggregatePublic.Sources[0].Aggregates {
			byName[aggregate.Name] = aggregate
			if aggregate.State != v1alpha1.AggregateStateValues || len(aggregate.Groups) != 1 || len(aggregate.Groups[0].Value.Matches) != 1 {
				t.Fatalf("aggregate %q did not remain successful: %#v", aggregate.Name, aggregate)
			}
		}
		quantitySum := byName["quantity-sum"].Groups[0].Value.Matches[0].Value.QuantityValue
		if quantitySum == nil || quantitySum.BaseUnits != "0.0001001" {
			t.Fatalf("corrected quantity aggregate = %#v", byName["quantity-sum"])
		}
		durationSum := byName["duration-sum"].Groups[0].Value.Matches[0].Value.DurationValue
		if durationSum == nil || durationSum.Nanoseconds != int64(2*time.Microsecond) {
			t.Fatalf("corrected duration aggregate = %#v", byName["duration-sum"])
		}

		invalidSource := v1alpha1.KubeseerSource{
			ID: "native-scalar-invalid-consumer",
			Fields: []v1alpha1.KubeseerField{
				{Name: "quantity", Path: "{.data.quantity}", Type: v1alpha1.ValueTypeQuantity, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}},
				{Name: "duration", Path: "{.data.duration}", Type: v1alpha1.ValueTypeDuration, Operators: []v1alpha1.KubeseerOperator{{Operator: v1alpha1.OperatorExists}}},
			},
		}
		invalid := aggregationSelectedResource("team-a", "invalid-consumer", "invalid-consumer-uid", map[string]any{"quantity": "0.0000000001", "duration": "0.1ns"})
		invalidOutcome, _ := evaluateOperatorSource(t, invalidSource, []selection.SelectedResource{invalid})
		if invalidOutcome.Err() != nil || len(invalidOutcome.Resources()) != 1 || invalidOutcome.Resources()[0].State() != operators.ResourceUnsuccessful || !operators.HasReason(invalidOutcome.Resources()[0].Failure(), operators.ReasonInvalidInput) || strings.Contains(invalidOutcome.Resources()[0].Failure().Error(), "0.0000000001") || strings.Contains(invalidOutcome.Resources()[0].Failure().Error(), "0.1ns") {
			t.Fatalf("invalid scalar controls changed operator failure contract: %#v", invalidOutcome)
		}
	})
}

func nativeScalarFieldsByName(fields []typedoutput.FieldOutcome) map[string]typedoutput.FieldOutcome {
	byName := make(map[string]typedoutput.FieldOutcome, len(fields))
	for _, field := range fields {
		byName[field.Name()] = field
	}
	return byName
}

func sourceFieldType(source v1alpha1.KubeseerSource, name string) v1alpha1.KubeseerValueType {
	for _, field := range source.Fields {
		if field.Name == name {
			return field.Type
		}
	}
	return ""
}

func assertNativeScalarQuantity(t *testing.T, field typedoutput.FieldOutcome, wantBaseUnits string) {
	t.Helper()
	if field.State() != typedoutput.FieldStateValues || len(field.Matches()) != 1 {
		t.Fatalf("quantity field %q = state=%s matches=%d", field.Name(), field.State(), len(field.Matches()))
	}
	_, baseUnits, ok := field.Matches()[0].QuantityValue()
	if !ok || baseUnits != wantBaseUnits {
		t.Fatalf("quantity field %q normalized base units = %q (ok=%t), want %q", field.Name(), baseUnits, ok, wantBaseUnits)
	}
}

func assertNativeScalarDuration(t *testing.T, field typedoutput.FieldOutcome, want time.Duration) {
	t.Helper()
	if field.State() != typedoutput.FieldStateValues || len(field.Matches()) != 1 {
		t.Fatalf("duration field %q = state=%s matches=%d", field.Name(), field.State(), len(field.Matches()))
	}
	got, ok := field.Matches()[0].DurationValue()
	if !ok || got != want {
		t.Fatalf("duration field %q = %v (ok=%t), want %v", field.Name(), got, ok, want)
	}
}
