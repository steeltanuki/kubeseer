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
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	integerTextPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	jsonNumberPattern  = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
	quantityPattern    = regexp.MustCompile(`^([+-]?)([0-9]+(?:\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+|Ki|Mi|Gi|Ti|Pi|Ei|m|k|M|G|T|P|E)?$`)

	errInvalidDecimal   = errors.New("invalid decimal")
	errDecimalOverflow  = errors.New("decimal exceeds supported representation")
	errDurationInexact  = errors.New("duration is not an exact nanosecond value")
	errQuantityInvalid  = errors.New("invalid quantity")
	errQuantityOverflow = errors.New("quantity exceeds supported representation")
)

const maxCanonicalDecimalDigits = 1 << 20

// ConvertMatch converts one native extraction match according to an approved
// field plan. A nil native value becomes an explicit null for every supported
// logical type.
func ConvertMatch(plan FieldPlan, native any, provenance *selection.Provenance) (Match, error) {
	if plan.typeName == "" {
		return Match{}, conversionErrorWithCause(plan.sourceID, plan.name, provenance, ReasonMissingType, "field type is required", nil)
	}
	if !IsSupportedType(plan.typeName) {
		return Match{}, conversionErrorWithCause(plan.sourceID, plan.name, provenance, ReasonUnsupportedType, "field type is unsupported", nil)
	}
	if native == nil {
		return newNullMatch(plan.typeName), nil
	}

	converted, reason, cause := convertNonNull(plan.typeName, native)
	if reason != "" {
		return Match{}, conversionErrorWithCause(plan.sourceID, plan.name, provenance, reason, conversionMessage(reason), cause)
	}
	return newValueMatch(converted), nil
}

func conversionMessage(reason ConversionErrorReason) string {
	switch reason {
	case ReasonMissingType:
		return "field type is required"
	case ReasonUnsupportedType:
		return "field type is unsupported"
	case ReasonConversionTypeMismatch:
		return "value kind is not compatible with the declared type"
	case ReasonInvalidValue:
		return "value has invalid lexical form"
	case ReasonConversionOverflow:
		return "value exceeds the supported representation"
	case ReasonForbiddenConversion:
		return "conversion would lose information"
	case ReasonInvalidInput:
		return "typed input identity is invalid"
	case ReasonConversionInterrupted:
		return "conversion interrupted"
	default:
		return "typed conversion failed"
	}
}

func convertNonNull(typeName v1alpha1.KubeseerValueType, native any) (value, ConversionErrorReason, error) {
	switch typeName {
	case v1alpha1.ValueTypeString:
		text, ok := native.(string)
		if !ok {
			return value{}, ReasonConversionTypeMismatch, nil
		}
		return value{typeName: typeName, kind: valueKindString, stringValue: text}, "", nil
	case v1alpha1.ValueTypeInteger:
		return convertInteger(typeName, native)
	case v1alpha1.ValueTypeNumber:
		return convertNumber(typeName, native)
	case v1alpha1.ValueTypeBoolean:
		switch typed := native.(type) {
		case bool:
			return value{typeName: typeName, kind: valueKindBoolean, booleanValue: typed}, "", nil
		case string:
			if typed != "true" && typed != "false" {
				return value{}, ReasonInvalidValue, nil
			}
			return value{typeName: typeName, kind: valueKindBoolean, booleanValue: typed == "true"}, "", nil
		default:
			return value{}, ReasonConversionTypeMismatch, nil
		}
	case v1alpha1.ValueTypeTimestamp:
		text, ok := native.(string)
		if !ok {
			return value{}, ReasonConversionTypeMismatch, nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return value{}, ReasonInvalidValue, err
		}
		return value{typeName: typeName, kind: valueKindTimestamp, timestampValue: parsed.UTC()}, "", nil
	case v1alpha1.ValueTypeDuration:
		return convertDuration(typeName, native)
	case v1alpha1.ValueTypeQuantity:
		return convertQuantity(typeName, native)
	case v1alpha1.ValueTypeObject:
		object, ok := native.(map[string]any)
		if !ok {
			return value{}, ReasonConversionTypeMismatch, nil
		}
		clone, err := cloneJSONValue(object)
		if err != nil {
			return value{}, ReasonConversionTypeMismatch, err
		}
		return value{typeName: typeName, kind: valueKindObject, objectValue: clone.(map[string]any)}, "", nil
	case v1alpha1.ValueTypeList:
		list, ok := native.([]any)
		if !ok {
			return value{}, ReasonConversionTypeMismatch, nil
		}
		clone, err := cloneJSONValue(list)
		if err != nil {
			return value{}, ReasonConversionTypeMismatch, err
		}
		return value{typeName: typeName, kind: valueKindList, listValue: clone.([]any)}, "", nil
	default:
		return value{}, ReasonUnsupportedType, nil
	}
}

func convertInteger(typeName v1alpha1.KubeseerValueType, native any) (value, ConversionErrorReason, error) {
	switch typed := native.(type) {
	case int64:
		return value{typeName: typeName, kind: valueKindInteger, integerValue: typed}, "", nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return value{}, ReasonInvalidValue, nil
		}
		if math.Trunc(typed) != typed {
			return value{}, ReasonForbiddenConversion, nil
		}
		if typed < -math.Exp2(63) || typed >= math.Exp2(63) {
			return value{}, ReasonConversionOverflow, nil
		}
		converted := int64(typed)
		if float64(converted) != typed {
			return value{}, ReasonForbiddenConversion, nil
		}
		return value{typeName: typeName, kind: valueKindInteger, integerValue: converted}, "", nil
	case string:
		if !integerTextPattern.MatchString(typed) {
			return value{}, ReasonInvalidValue, nil
		}
		converted, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return value{}, ReasonConversionOverflow, err
			}
			return value{}, ReasonInvalidValue, err
		}
		return value{typeName: typeName, kind: valueKindInteger, integerValue: converted}, "", nil
	case json.Number:
		canonical, err := canonicalDecimal(typed.String())
		if err != nil {
			return value{}, decimalReason(err), err
		}
		if strings.Contains(canonical, ".") {
			return value{}, ReasonForbiddenConversion, nil
		}
		converted, err := strconv.ParseInt(canonical, 10, 64)
		if err != nil {
			if errors.Is(err, strconv.ErrRange) {
				return value{}, ReasonConversionOverflow, err
			}
			return value{}, ReasonInvalidValue, err
		}
		return value{typeName: typeName, kind: valueKindInteger, integerValue: converted}, "", nil
	default:
		return value{}, ReasonConversionTypeMismatch, nil
	}
}

func convertNumber(typeName v1alpha1.KubeseerValueType, native any) (value, ConversionErrorReason, error) {
	var text string
	switch typed := native.(type) {
	case int64:
		text = strconv.FormatInt(typed, 10)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return value{}, ReasonInvalidValue, nil
		}
		text = strconv.FormatFloat(typed, 'g', -1, 64)
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	default:
		return value{}, ReasonConversionTypeMismatch, nil
	}
	canonical, err := canonicalDecimal(text)
	if err != nil {
		return value{}, decimalReason(err), err
	}
	return value{typeName: typeName, kind: valueKindNumber, numberValue: canonical}, "", nil
}

func decimalReason(err error) ConversionErrorReason {
	if errors.Is(err, errDecimalOverflow) {
		return ReasonConversionOverflow
	}
	return ReasonInvalidValue
}

func canonicalDecimal(text string) (string, error) {
	if !jsonNumberPattern.MatchString(text) {
		return "", errInvalidDecimal
	}

	sign := ""
	if strings.HasPrefix(text, "-") {
		sign = "-"
		text = text[1:]
	}
	exponent := int64(0)
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		parsed, err := strconv.ParseInt(text[index+1:], 10, 64)
		if err != nil {
			return "", errDecimalOverflow
		}
		exponent = parsed
		text = text[:index]
	}
	integerPart, fractionPart := text, ""
	if index := strings.IndexByte(text, '.'); index >= 0 {
		integerPart, fractionPart = text[:index], text[index+1:]
	}
	digits := integerPart + fractionPart
	firstNonZero := strings.IndexFunc(digits, func(r rune) bool { return r != '0' })
	if firstNonZero < 0 {
		return "0", nil
	}
	digits = digits[firstNonZero:]
	decimalPosition := int64(len(integerPart)-firstNonZero) + exponent
	if decimalPosition > maxCanonicalDecimalDigits || decimalPosition < -maxCanonicalDecimalDigits {
		return "", errDecimalOverflow
	}
	for len(digits) > 0 && digits[len(digits)-1] == '0' && int64(len(digits)) > decimalPosition {
		digits = digits[:len(digits)-1]
	}
	if len(digits) > maxCanonicalDecimalDigits {
		return "", errDecimalOverflow
	}

	var canonical string
	switch {
	case decimalPosition <= 0:
		zeros := -decimalPosition
		if zeros > maxCanonicalDecimalDigits {
			return "", errDecimalOverflow
		}
		canonical = "0." + strings.Repeat("0", int(zeros)) + digits
	case decimalPosition >= int64(len(digits)):
		zeros := decimalPosition - int64(len(digits))
		if zeros > maxCanonicalDecimalDigits {
			return "", errDecimalOverflow
		}
		canonical = digits + strings.Repeat("0", int(zeros))
	default:
		canonical = digits[:decimalPosition] + "." + digits[decimalPosition:]
	}
	if canonical == "0" || strings.Trim(canonical, "0.") == "" {
		return "0", nil
	}
	return sign + canonical, nil
}

func convertDuration(typeName v1alpha1.KubeseerValueType, native any) (value, ConversionErrorReason, error) {
	text, ok := native.(string)
	if !ok {
		return value{}, ReasonConversionTypeMismatch, nil
	}
	exact, err := exactDurationNanoseconds(text)
	if err != nil {
		if errors.Is(err, errDurationInexact) {
			return value{}, ReasonForbiddenConversion, err
		}
		return value{}, ReasonInvalidValue, err
	}
	if !exact.IsInt64() {
		return value{}, ReasonConversionOverflow, nil
	}
	duration, err := time.ParseDuration(text)
	if err != nil {
		return value{}, ReasonInvalidValue, err
	}
	if exact.Int64() != int64(duration) {
		return value{}, ReasonForbiddenConversion, nil
	}
	return value{typeName: typeName, kind: valueKindDuration, durationValue: duration}, "", nil
}

func exactDurationNanoseconds(text string) (*big.Int, error) {
	negative := false
	position := 0
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		negative = text[0] == '-'
		position++
	}
	total := new(big.Rat)
	for position < len(text) {
		start := position
		digitCount := 0
		for position < len(text) && text[position] >= '0' && text[position] <= '9' {
			position++
			digitCount++
		}
		if position < len(text) && text[position] == '.' {
			position++
			for position < len(text) && text[position] >= '0' && text[position] <= '9' {
				position++
				digitCount++
			}
		}
		if digitCount == 0 {
			return nil, errInvalidDecimal
		}
		numberText := text[start:position]
		if strings.HasPrefix(numberText, ".") {
			numberText = "0" + numberText
		}
		if strings.HasSuffix(numberText, ".") {
			numberText += "0"
		}
		number, ok := new(big.Rat).SetString(numberText)
		if !ok {
			return nil, errInvalidDecimal
		}
		unit, factor, ok := durationUnit(text[position:])
		if !ok {
			return nil, errInvalidDecimal
		}
		position += len(unit)
		number.Mul(number, new(big.Rat).SetInt64(factor))
		total.Add(total, number)
	}
	if negative {
		total.Neg(total)
	}
	if total.Denom().Cmp(big.NewInt(1)) != 0 {
		return nil, errDurationInexact
	}
	return new(big.Int).Set(total.Num()), nil
}

func durationUnit(remaining string) (string, int64, bool) {
	for _, candidate := range []struct {
		name   string
		factor int64
	}{
		{name: "ns", factor: 1},
		{name: "us", factor: 1000},
		{name: "µs", factor: 1000},
		{name: "ms", factor: 1000000},
		{name: "s", factor: 1000000000},
		{name: "m", factor: 60000000000},
		{name: "h", factor: 3600000000000},
	} {
		if strings.HasPrefix(remaining, candidate.name) {
			return candidate.name, candidate.factor, true
		}
	}
	return "", 0, false
}

func convertQuantity(typeName v1alpha1.KubeseerValueType, native any) (value, ConversionErrorReason, error) {
	text, ok := native.(string)
	if !ok {
		return value{}, ReasonConversionTypeMismatch, nil
	}
	quantity, err := resource.ParseQuantity(text)
	if err != nil {
		if exact, exactErr := exactQuantityMagnitude(text); errors.Is(exactErr, errQuantityOverflow) || (exactErr == nil && quantityMagnitudeExceedsInt64(exact)) {
			return value{}, ReasonConversionOverflow, err
		}
		return value{}, ReasonInvalidValue, err
	}
	exact, err := exactQuantityMagnitude(text)
	if err != nil {
		if errors.Is(err, errQuantityOverflow) {
			return value{}, ReasonConversionOverflow, err
		}
		return value{}, ReasonInvalidValue, err
	}
	if quantityMagnitudeExceedsInt64(exact) {
		return value{}, ReasonConversionOverflow, nil
	}
	actual, ok := new(big.Rat).SetString(quantity.AsDec().String())
	if !ok {
		return value{}, ReasonInvalidValue, nil
	}
	if exact.Cmp(actual) != 0 {
		return value{}, ReasonForbiddenConversion, nil
	}
	baseUnits, err := canonicalRationalDecimal(exact)
	if err != nil {
		return value{}, ReasonConversionOverflow, err
	}
	return value{typeName: typeName, kind: valueKindQuantity, quantityValue: quantity.DeepCopy(), baseUnits: baseUnits}, "", nil
}

func exactQuantityMagnitude(text string) (*big.Rat, error) {
	matches := quantityPattern.FindStringSubmatch(text)
	if matches == nil {
		return nil, errQuantityInvalid
	}
	numberText := matches[2]
	if strings.HasPrefix(numberText, ".") {
		numberText = "0" + numberText
	}
	if strings.HasSuffix(numberText, ".") {
		numberText += "0"
	}
	number, ok := new(big.Rat).SetString(numberText)
	if !ok {
		return nil, errQuantityInvalid
	}
	suffix := matches[3]
	factor, err := quantitySuffixFactor(suffix)
	if err != nil {
		return nil, err
	}
	number.Mul(number, factor)
	if matches[1] == "-" {
		number.Neg(number)
	}
	return number, nil
}

func quantitySuffixFactor(suffix string) (*big.Rat, error) {
	if suffix == "" {
		return new(big.Rat).SetInt64(1), nil
	}
	if len(suffix) > 1 && (suffix[0] == 'e' || suffix[0] == 'E') {
		exponent, err := strconv.ParseInt(suffix[1:], 10, 64)
		if err != nil || exponent > maxCanonicalDecimalDigits || exponent < -maxCanonicalDecimalDigits {
			return nil, errQuantityOverflow
		}
		if exponent >= 0 {
			return new(big.Rat).SetInt(pow10BigInt(int(exponent))), nil
		}
		return new(big.Rat).SetFrac(big.NewInt(1), pow10BigInt(int(-exponent))), nil
	}
	decimalExponents := map[string]int{"k": 3, "M": 6, "G": 9, "T": 12, "P": 15, "E": 18}
	if exponent, found := decimalExponents[suffix]; found {
		return new(big.Rat).SetInt(pow10BigInt(exponent)), nil
	}
	if suffix == "m" {
		return new(big.Rat).SetFrac(big.NewInt(1), big.NewInt(1000)), nil
	}
	for index, candidate := range []string{"Ki", "Mi", "Gi", "Ti", "Pi", "Ei"} {
		if suffix == candidate {
			return new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), uint(10*(index+1)))), nil
		}
	}
	return nil, errQuantityInvalid
}

func pow10BigInt(exponent int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
}

func quantityMagnitudeExceedsInt64(value *big.Rat) bool {
	maximum := big.NewRat(1<<63-1, 1)
	absolute := new(big.Rat).Set(value)
	if absolute.Sign() < 0 {
		absolute.Neg(absolute)
	}
	return absolute.Cmp(maximum) > 0
}

func canonicalRationalDecimal(value *big.Rat) (string, error) {
	denominator := new(big.Int).Set(value.Denom())
	scale := 0
	two := big.NewInt(2)
	five := big.NewInt(5)
	for new(big.Int).Mod(denominator, two).Sign() == 0 {
		denominator.Div(denominator, two)
		if scale < 1 {
			scale = 1
		}
	}
	for new(big.Int).Mod(denominator, five).Sign() == 0 {
		denominator.Div(denominator, five)
		if scale < 1 {
			scale = 1
		}
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return "", errDecimalOverflow
	}
	for scale < maxCanonicalDecimalDigits {
		candidate := value.FloatString(scale)
		if parsed, ok := new(big.Rat).SetString(candidate); ok && parsed.Cmp(value) == 0 {
			return trimDecimalZeros(candidate), nil
		}
		scale++
	}
	return "", errDecimalOverflow
}

func trimDecimalZeros(text string) string {
	if strings.Contains(text, ".") {
		text = strings.TrimRight(text, "0")
		text = strings.TrimRight(text, ".")
	}
	if text == "-0" || text == "" {
		return "0"
	}
	return text
}

func cloneJSONValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil, bool, string, int64, float64, json.Number:
		return typed, nil
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, nested := range typed {
			copied, err := cloneJSONValue(nested)
			if err != nil {
				return nil, err
			}
			clone[key] = copied
		}
		return clone, nil
	case []any:
		clone := make([]any, len(typed))
		for index, nested := range typed {
			copied, err := cloneJSONValue(nested)
			if err != nil {
				return nil, err
			}
			clone[index] = copied
		}
		return clone, nil
	default:
		return nil, errors.New("native value is not JSON-compatible")
	}
}
