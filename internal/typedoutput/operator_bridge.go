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
	"errors"
	"math/big"
	"reflect"
	"strings"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/selection"
)

// ConvertConfiguredMatch converts one already-decoded operator operand with
// the exact conversion core used for observed extraction matches. The
// operator layer supplies the branch-specific native value; this function
// does not infer a field type or add coercions.
func ConvertConfiguredMatch(sourceID, fieldName string, typeName v1alpha1.KubeseerValueType, native any) (Match, error) {
	return ConvertMatch(FieldPlan{sourceID: sourceID, name: fieldName, typeName: typeName}, native, nil)
}

// NullMatch returns the explicit null representation for one supported field
// type through the same configured-value conversion path.
func NullMatch(sourceID, fieldName string, typeName v1alpha1.KubeseerValueType) (Match, error) {
	return ConvertConfiguredMatch(sourceID, fieldName, typeName, nil)
}

// EqualMatches compares two normalized typed matches without using display
// formatting or floating-point conversion. Null matches compare false and
// are expected to be filtered by predicate callers.
func EqualMatches(left, right Match) (bool, error) {
	if left.IsNull() || right.IsNull() {
		return false, nil
	}
	if err := validateComparableMatches(left, right); err != nil {
		return false, err
	}
	switch left.value.kind {
	case valueKindString:
		return left.value.stringValue == right.value.stringValue, nil
	case valueKindInteger:
		return left.value.integerValue == right.value.integerValue, nil
	case valueKindNumber:
		leftNumber, ok := decimalRat(left.value.numberValue)
		if !ok {
			return false, errors.New("left number is not normalized")
		}
		rightNumber, ok := decimalRat(right.value.numberValue)
		if !ok {
			return false, errors.New("right number is not normalized")
		}
		return leftNumber.Cmp(rightNumber) == 0, nil
	case valueKindBoolean:
		return left.value.booleanValue == right.value.booleanValue, nil
	case valueKindTimestamp:
		return left.value.timestampValue.Equal(right.value.timestampValue), nil
	case valueKindDuration:
		return left.value.durationValue == right.value.durationValue, nil
	case valueKindQuantity:
		return left.value.quantityValue.Cmp(right.value.quantityValue) == 0, nil
	case valueKindObject:
		return reflect.DeepEqual(left.value.objectValue, right.value.objectValue), nil
	case valueKindList:
		return reflect.DeepEqual(left.value.listValue, right.value.listValue), nil
	default:
		return false, errors.New("match payload is not recognized")
	}
}

// CompareMatches returns -1, 0, or 1 for two normalized ordered values. It
// deliberately refuses object and list values, which have no ordered logical
// representation in the approved operator matrix.
func CompareMatches(left, right Match) (int, error) {
	if left.IsNull() || right.IsNull() {
		return 0, errors.New("null matches are not orderable")
	}
	if err := validateComparableMatches(left, right); err != nil {
		return 0, err
	}
	switch left.value.kind {
	case valueKindString:
		return strings.Compare(left.value.stringValue, right.value.stringValue), nil
	case valueKindInteger:
		if left.value.integerValue < right.value.integerValue {
			return -1, nil
		}
		if left.value.integerValue > right.value.integerValue {
			return 1, nil
		}
		return 0, nil
	case valueKindNumber:
		leftNumber, ok := decimalRat(left.value.numberValue)
		if !ok {
			return 0, errors.New("left number is not normalized")
		}
		rightNumber, ok := decimalRat(right.value.numberValue)
		if !ok {
			return 0, errors.New("right number is not normalized")
		}
		return leftNumber.Cmp(rightNumber), nil
	case valueKindTimestamp:
		if left.value.timestampValue.Before(right.value.timestampValue) {
			return -1, nil
		}
		if left.value.timestampValue.After(right.value.timestampValue) {
			return 1, nil
		}
		return 0, nil
	case valueKindDuration:
		if left.value.durationValue < right.value.durationValue {
			return -1, nil
		}
		if left.value.durationValue > right.value.durationValue {
			return 1, nil
		}
		return 0, nil
	case valueKindQuantity:
		return left.value.quantityValue.Cmp(right.value.quantityValue), nil
	default:
		return 0, errors.New("match type is not orderable")
	}
}

func decimalRat(text string) (*big.Rat, bool) {
	value, ok := new(big.Rat).SetString(text)
	return value, ok
}

func validateComparableMatches(left, right Match) error {
	if !IsSupportedType(left.Type()) || !IsSupportedType(right.Type()) {
		return errors.New("match type is unsupported")
	}
	if left.Type() != right.Type() || left.value.kind != right.value.kind {
		return errors.New("match types do not agree")
	}
	return nil
}

// ReplaceFieldMatches returns a new immutable field outcome with the supplied
// ordered matches. It never mutates the input field and preserves its name and
// logical type. An empty replacement is the explicit absent state.
func ReplaceFieldMatches(field FieldOutcome, matches []Match) (FieldOutcome, error) {
	if field.name == "" || !IsSupportedType(field.typeName) {
		return FieldOutcome{}, NewConversionError("", field.name, nil, ReasonInvalidInput, "typed field identity is invalid")
	}
	if field.state == FieldStateError {
		return FieldOutcome{}, NewConversionError("", field.name, nil, ReasonInvalidInput, "cannot replace an unsuccessful typed field")
	}
	for _, match := range matches {
		if match.Type() != field.typeName || (!match.IsNull() && match.value.kind == 0) {
			return FieldOutcome{}, NewConversionError("", field.name, nil, ReasonInvalidInput, "replacement match type is invalid")
		}
	}
	if len(matches) == 0 {
		return newAbsentFieldOutcome(field.name, field.typeName), nil
	}
	return newValuesFieldOutcome(field.name, field.typeName, matches), nil
}

// ProjectFieldResult serializes one immutable typed field through the
// existing structural result contract without status writes.
func ProjectFieldResult(field FieldOutcome) (v1alpha1.KubeseerFieldResult, error) {
	return buildFieldResult(field)
}

// ProjectResourceResult serializes one resource provenance and its immutable
// typed fields through the existing structural result contract.
func ProjectResourceResult(provenance selection.Provenance, fields []FieldOutcome) (v1alpha1.KubeseerResourceResult, error) {
	return buildResourceResult(ResourceOutcome{provenance: provenance, fields: fields})
}

// ProjectResultError maps an internal source or typed-output error to the
// existing sanitized public result error contract.
func ProjectResultError(err error) *v1alpha1.KubeseerResultError {
	return publicResultError(err)
}
