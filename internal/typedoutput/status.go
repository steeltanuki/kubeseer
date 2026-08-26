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

package typedoutput

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildResult maps immutable internal typed outcomes into the structural API
// status representation. It performs no Kubernetes I/O.
func BuildResult(outcomes []SourceOutcome) (v1alpha1.KubeseerResult, error) {
	result := v1alpha1.KubeseerResult{}
	if outcomes == nil {
		return result, nil
	}
	result.Sources = make([]v1alpha1.KubeseerSourceResult, len(outcomes))
	for index, source := range outcomes {
		converted, err := buildSourceResult(source)
		if err != nil {
			return v1alpha1.KubeseerResult{}, err
		}
		result.Sources[index] = converted
	}
	return result, nil
}

func buildSourceResult(source SourceOutcome) (v1alpha1.KubeseerSourceResult, error) {
	if source.sourceID == "" {
		return v1alpha1.KubeseerSourceResult{}, invalidResultError("source identity is empty")
	}
	if source.err != nil {
		if len(source.resources) != 0 || len(source.fieldErrors) != 0 {
			return v1alpha1.KubeseerSourceResult{}, invalidResultError("source error contains successful payloads")
		}
		return v1alpha1.KubeseerSourceResult{
			ID:    source.sourceID,
			State: v1alpha1.SourceStateError,
			Error: publicResultError(source.err),
		}, nil
	}

	result := v1alpha1.KubeseerSourceResult{
		ID:    source.sourceID,
		State: v1alpha1.SourceStateValues,
	}
	if len(source.fieldErrors) != 0 {
		result.FieldErrors = make([]v1alpha1.KubeseerFieldError, len(source.fieldErrors))
		for index, fieldErr := range source.fieldErrors {
			if fieldErr == nil || fieldErr.FieldName == "" {
				return v1alpha1.KubeseerSourceResult{}, invalidResultError("source field error is incomplete")
			}
			result.FieldErrors[index] = v1alpha1.KubeseerFieldError{
				Name:    fieldErr.FieldName,
				Reason:  string(fieldErr.Reason),
				Message: fieldErr.Message,
			}
		}
	}
	if len(source.resources) != 0 {
		result.Resources = make([]v1alpha1.KubeseerResourceResult, len(source.resources))
		for index, resource := range source.resources {
			converted, err := buildResourceResult(resource)
			if err != nil {
				return v1alpha1.KubeseerSourceResult{}, err
			}
			result.Resources[index] = converted
		}
	}
	return result, nil
}

func buildResourceResult(resource ResourceOutcome) (v1alpha1.KubeseerResourceResult, error) {
	if resource.provenance.APIVersion == "" || resource.provenance.Kind == "" || resource.provenance.Name == "" || resource.provenance.UID == "" {
		return v1alpha1.KubeseerResourceResult{}, invalidResultError("resource provenance is incomplete")
	}
	result := v1alpha1.KubeseerResourceResult{
		APIVersion: resource.provenance.APIVersion,
		Kind:       resource.provenance.Kind,
		Namespace:  resource.provenance.Namespace,
		Name:       resource.provenance.Name,
		UID:        resource.provenance.UID,
	}
	if len(resource.fields) != 0 {
		result.Fields = make([]v1alpha1.KubeseerFieldResult, len(resource.fields))
		for index, field := range resource.fields {
			converted, err := buildFieldResult(field)
			if err != nil {
				return v1alpha1.KubeseerResourceResult{}, err
			}
			result.Fields[index] = converted
		}
	}
	return result, nil
}

func buildFieldResult(field FieldOutcome) (v1alpha1.KubeseerFieldResult, error) {
	if field.name == "" {
		return v1alpha1.KubeseerFieldResult{}, invalidResultError("field identity is empty")
	}
	if !IsSupportedType(field.typeName) {
		return v1alpha1.KubeseerFieldResult{}, invalidResultError("field type is unsupported")
	}
	result := v1alpha1.KubeseerFieldResult{Name: field.name, Type: field.typeName, State: v1alpha1.KubeseerFieldState(field.state)}
	switch field.state {
	case FieldStateAbsent:
		if len(field.matches) != 0 || field.err != nil {
			return v1alpha1.KubeseerFieldResult{}, invalidResultError("absent field contains matches or an error")
		}
	case FieldStateValues:
		if len(field.matches) == 0 || field.err != nil {
			return v1alpha1.KubeseerFieldResult{}, invalidResultError("values field has no matches or contains an error")
		}
		result.Matches = make([]v1alpha1.KubeseerTypedMatch, len(field.matches))
		for index, match := range field.matches {
			converted, err := buildMatch(match, field.typeName)
			if err != nil {
				return v1alpha1.KubeseerFieldResult{}, err
			}
			result.Matches[index] = converted
		}
	case FieldStateError:
		if len(field.matches) != 0 || field.err == nil {
			return v1alpha1.KubeseerFieldResult{}, invalidResultError("error field contains matches or no error")
		}
		result.Error = publicResultError(field.err)
	default:
		return v1alpha1.KubeseerFieldResult{}, invalidResultError("field state is unsupported")
	}
	return result, nil
}

func buildMatch(match Match, typeName v1alpha1.KubeseerValueType) (v1alpha1.KubeseerTypedMatch, error) {
	if match.value.typeName != typeName {
		return v1alpha1.KubeseerTypedMatch{}, invalidResultError("match type does not match field type")
	}
	switch match.state {
	case matchStateNull:
		if match.value.kind != 0 {
			return v1alpha1.KubeseerTypedMatch{}, invalidResultError("null match contains a payload")
		}
		return v1alpha1.KubeseerTypedMatch{State: v1alpha1.MatchStateNull}, nil
	case matchStateValue:
		if !IsSupportedType(typeName) {
			return v1alpha1.KubeseerTypedMatch{}, invalidResultError("value match type is unsupported")
		}
		result := v1alpha1.KubeseerTypedMatch{State: v1alpha1.MatchStateValue}
		switch match.value.kind {
		case valueKindString:
			if typeName != v1alpha1.ValueTypeString {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("string payload does not match field type")
			}
			value := match.value.stringValue
			result.StringValue = &value
		case valueKindInteger:
			if typeName != v1alpha1.ValueTypeInteger {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("integer payload does not match field type")
			}
			value := match.value.integerValue
			result.IntegerValue = &value
		case valueKindNumber:
			if typeName != v1alpha1.ValueTypeNumber {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("number payload does not match field type")
			}
			if _, err := canonicalDecimal(match.value.numberValue); err != nil {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("number payload is not canonical")
			}
			value := match.value.numberValue
			result.NumberValue = &value
		case valueKindBoolean:
			if typeName != v1alpha1.ValueTypeBoolean {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("boolean payload does not match field type")
			}
			value := match.value.booleanValue
			result.BooleanValue = &value
		case valueKindTimestamp:
			if typeName != v1alpha1.ValueTypeTimestamp {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("timestamp payload does not match field type")
			}
			value := metav1.Time{Time: match.value.timestampValue.UTC()}
			result.TimestampValue = &value
		case valueKindDuration:
			if typeName != v1alpha1.ValueTypeDuration {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("duration payload does not match field type")
			}
			result.DurationValue = &v1alpha1.KubeseerDurationValue{
				Canonical:   match.value.durationValue.String(),
				Nanoseconds: int64(match.value.durationValue),
			}
		case valueKindQuantity:
			if typeName != v1alpha1.ValueTypeQuantity || match.value.baseUnits == "" {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("quantity payload is incomplete")
			}
			if _, err := canonicalDecimal(match.value.baseUnits); err != nil {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("quantity base-unit payload is not canonical")
			}
			result.QuantityValue = &v1alpha1.KubeseerQuantityValue{
				Canonical: match.value.quantityValue.String(),
				BaseUnits: match.value.baseUnits,
			}
		case valueKindObject:
			if typeName != v1alpha1.ValueTypeObject {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("object payload does not match field type")
			}
			encoded, err := canonicalJSON(match.value.objectValue, '{')
			if err != nil {
				return v1alpha1.KubeseerTypedMatch{}, err
			}
			result.ObjectValue = &encoded
		case valueKindList:
			if typeName != v1alpha1.ValueTypeList {
				return v1alpha1.KubeseerTypedMatch{}, invalidResultError("list payload does not match field type")
			}
			encoded, err := canonicalJSON(match.value.listValue, '[')
			if err != nil {
				return v1alpha1.KubeseerTypedMatch{}, err
			}
			result.ListValue = &encoded
		default:
			return v1alpha1.KubeseerTypedMatch{}, invalidResultError("value match has no recognized payload")
		}
		return result, nil
	default:
		return v1alpha1.KubeseerTypedMatch{}, invalidResultError("match state is unsupported")
	}
}

func canonicalJSON(value any, topLevel byte) (string, error) {
	encoded, err := canonicalJSONBytes(value)
	if err != nil {
		return "", err
	}
	if len(encoded) == 0 || encoded[0] != topLevel || !json.Valid(encoded) {
		return "", invalidResultError("composite payload has the wrong JSON kind")
	}
	return string(encoded), nil
}

func canonicalJSONBytes(value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return []byte("null"), nil
	case bool:
		return []byte(strconv.FormatBool(typed)), nil
	case string:
		return json.Marshal(typed)
	case int64:
		return []byte(strconv.FormatInt(typed, 10)), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return nil, invalidResultError("composite payload contains a non-finite number")
		}
		canonical, err := canonicalDecimal(strconv.FormatFloat(typed, 'g', -1, 64))
		if err != nil {
			return nil, invalidResultError("composite number payload is not representable")
		}
		return []byte(canonical), nil
	case json.Number:
		canonical, err := canonicalDecimal(typed.String())
		if err != nil {
			return nil, invalidResultError("composite number payload is not canonical")
		}
		return []byte(canonical), nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var buffer bytes.Buffer
		buffer.WriteByte('{')
		for index, key := range keys {
			if index != 0 {
				buffer.WriteByte(',')
			}
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return nil, invalidResultError("composite object key cannot be encoded")
			}
			buffer.Write(encodedKey)
			buffer.WriteByte(':')
			encodedValue, err := canonicalJSONBytes(typed[key])
			if err != nil {
				return nil, err
			}
			buffer.Write(encodedValue)
		}
		buffer.WriteByte('}')
		return buffer.Bytes(), nil
	case []any:
		var buffer bytes.Buffer
		buffer.WriteByte('[')
		for index, element := range typed {
			if index != 0 {
				buffer.WriteByte(',')
			}
			encoded, err := canonicalJSONBytes(element)
			if err != nil {
				return nil, err
			}
			buffer.Write(encoded)
		}
		buffer.WriteByte(']')
		return buffer.Bytes(), nil
	default:
		return nil, invalidResultError("composite payload contains an unsupported native value")
	}
}

func publicResultError(err error) *v1alpha1.KubeseerResultError {
	if err == nil {
		return nil
	}
	var conversionErr *ConversionError
	if errors.As(err, &conversionErr) {
		return &v1alpha1.KubeseerResultError{Reason: string(conversionErr.Reason), Message: conversionErr.Message}
	}
	var extractionErr *extraction.ExtractionError
	if errors.As(err, &extractionErr) {
		return &v1alpha1.KubeseerResultError{Reason: string(extractionErr.Reason), Message: extractionErr.Message}
	}
	var selectionErr *selection.SelectionError
	if errors.As(err, &selectionErr) {
		return &v1alpha1.KubeseerResultError{Reason: string(selectionErr.Reason), Message: selectionErr.Message}
	}
	return &v1alpha1.KubeseerResultError{Reason: "TypedOutputError", Message: "typed output conversion failed"}
}

func invalidResultError(message string) error {
	return fmt.Errorf("typed output result is invalid: %s", message)
}
