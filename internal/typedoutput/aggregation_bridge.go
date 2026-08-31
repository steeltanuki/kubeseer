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
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
)

// CanonicalKey returns a type-tagged, length-delimited key for one non-null
// normalized match. The representation is suitable for equality maps and
// cannot collide when a payload contains separator bytes.
func CanonicalKey(valueType v1alpha1.KubeseerValueType, match Match) ([]byte, error) {
	if match.IsNull() || match.Type() != valueType || !IsSupportedType(valueType) {
		return nil, errors.New("typed key requires one supported non-null match")
	}
	payload, err := canonicalMatchPayload(match)
	if err != nil {
		return nil, err
	}
	var key bytes.Buffer
	if err := writeKeyPart(&key, string(valueType)); err != nil {
		return nil, err
	}
	if err := writeKeyPart(&key, payload); err != nil {
		return nil, err
	}
	return key.Bytes(), nil
}

func writeKeyPart(buffer *bytes.Buffer, value string) error {
	if uint64(len(value)) > uint64(^uint32(0)) {
		return errors.New("typed key payload is too large")
	}
	if err := binary.Write(buffer, binary.BigEndian, uint32(len(value))); err != nil {
		return err
	}
	_, err := buffer.WriteString(value)
	return err
}

func canonicalMatchPayload(match Match) (string, error) {
	switch match.value.kind {
	case valueKindString:
		return match.value.stringValue, nil
	case valueKindInteger:
		return strconv.FormatInt(match.value.integerValue, 10), nil
	case valueKindNumber:
		return match.value.numberValue, nil
	case valueKindBoolean:
		return strconv.FormatBool(match.value.booleanValue), nil
	case valueKindTimestamp:
		return match.value.timestampValue.UTC().Format(time.RFC3339Nano), nil
	case valueKindDuration:
		return strconv.FormatInt(int64(match.value.durationValue), 10), nil
	case valueKindQuantity:
		return match.value.baseUnits, nil
	case valueKindObject:
		return canonicalJSON(match.value.objectValue, '{')
	case valueKindList:
		return canonicalJSON(match.value.listValue, '[')
	default:
		return "", errors.New("typed key payload is not recognized")
	}
}

// ExactRational returns the exact numeric magnitude of an integer, number,
// duration, or quantity match. It deliberately rejects non-numeric logical
// types and never converts through floating point.
func ExactRational(valueType v1alpha1.KubeseerValueType, match Match) (*big.Rat, error) {
	if match.IsNull() || match.Type() != valueType {
		return nil, errors.New("exact rational requires one non-null match of the declared type")
	}
	switch valueType {
	case v1alpha1.ValueTypeInteger:
		return new(big.Rat).SetInt64(match.value.integerValue), nil
	case v1alpha1.ValueTypeNumber:
		value, ok := new(big.Rat).SetString(match.value.numberValue)
		if !ok {
			return nil, errors.New("number match is not canonical")
		}
		return value, nil
	case v1alpha1.ValueTypeDuration:
		return new(big.Rat).SetInt64(int64(match.value.durationValue)), nil
	case v1alpha1.ValueTypeQuantity:
		value, ok := new(big.Rat).SetString(match.value.baseUnits)
		if !ok {
			return nil, errors.New("quantity match has invalid base units")
		}
		return value, nil
	default:
		return nil, errors.New("logical type is not exactly numeric")
	}
}

// MatchFromExactNumber rounds an exact rational once to the requested number
// of fractional decimal places and returns a normalized number match.
func MatchFromExactNumber(value *big.Rat, scale int32, mode v1alpha1.KubeseerRoundingMode) (Match, error) {
	if value == nil {
		return Match{}, errors.New("exact number is nil")
	}
	text, err := roundRationalDecimal(value, scale, mode)
	if err != nil {
		return Match{}, err
	}
	return ConvertConfiguredMatch("", "", v1alpha1.ValueTypeNumber, text)
}

// MatchFromExact converts an exact rational to a supported numeric output
// type without changing its magnitude. Number values must have a terminating
// decimal representation, as required by the structural typed-output model.
func MatchFromExact(valueType v1alpha1.KubeseerValueType, value *big.Rat) (Match, error) {
	if value == nil {
		return Match{}, errors.New("exact value is nil")
	}
	switch valueType {
	case v1alpha1.ValueTypeInteger:
		if !value.IsInt() || !value.Num().IsInt64() {
			return Match{}, errors.New("exact integer is outside the signed 64-bit range")
		}
		return ConvertConfiguredMatch("", "", valueType, value.Num().Int64())
	case v1alpha1.ValueTypeNumber, v1alpha1.ValueTypeQuantity:
		text, err := canonicalRationalDecimal(value)
		if err != nil {
			return Match{}, err
		}
		return ConvertConfiguredMatch("", "", valueType, text)
	case v1alpha1.ValueTypeDuration:
		if !value.IsInt() || !value.Num().IsInt64() {
			return Match{}, errors.New("exact duration is outside the signed 64-bit range")
		}
		return ConvertConfiguredMatch("", "", valueType, time.Duration(value.Num().Int64()).String())
	default:
		return Match{}, errors.New("logical type is not an exact numeric output")
	}
}

// ProjectMatch serializes one immutable normalized match through the existing
// structural typed-output contract.
func ProjectMatch(match Match) (v1alpha1.KubeseerTypedMatch, error) {
	return buildMatch(match, match.Type())
}

func roundRationalDecimal(value *big.Rat, scale int32, mode v1alpha1.KubeseerRoundingMode) (string, error) {
	if scale < 0 || scale > 18 {
		return "", errors.New("decimal precision is outside the supported range")
	}
	if !supportedRoundingMode(mode) {
		return "", fmt.Errorf("rounding mode %q is unsupported", mode)
	}
	absolute := new(big.Rat).Set(value)
	negative := absolute.Sign() < 0
	if negative {
		absolute.Abs(absolute)
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	numerator := new(big.Int).Mul(absolute.Num(), factor)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, absolute.Denom(), remainder)
	if shouldRoundUp(quotient, remainder, absolute.Denom(), mode) {
		quotient.Add(quotient, big.NewInt(1))
	}
	if negative && quotient.Sign() != 0 {
		quotient.Neg(quotient)
	}
	return formatScaledInteger(quotient, scale), nil
}

func shouldRoundUp(quotient, remainder, denominator *big.Int, mode v1alpha1.KubeseerRoundingMode) bool {
	if remainder.Sign() == 0 {
		return false
	}
	switch mode {
	case v1alpha1.RoundingTowardZero:
		return false
	case v1alpha1.RoundingAwayFromZero:
		return true
	case v1alpha1.RoundingHalfAwayFromZero:
		return new(big.Int).Lsh(new(big.Int).Set(remainder), 1).Cmp(denominator) >= 0
	case v1alpha1.RoundingHalfEven:
		doubled := new(big.Int).Lsh(new(big.Int).Set(remainder), 1)
		comparison := doubled.Cmp(denominator)
		return comparison > 0 || (comparison == 0 && quotient.Bit(0) == 1)
	default:
		return false
	}
}

func formatScaledInteger(value *big.Int, scale int32) string {
	negative := value.Sign() < 0
	abs := new(big.Int).Abs(value).String()
	if scale == 0 {
		if negative && abs != "0" {
			return "-" + abs
		}
		return abs
	}
	width := int(scale) + 1
	if len(abs) < width {
		abs = strings.Repeat("0", width-len(abs)) + abs
	}
	point := len(abs) - int(scale)
	text := abs[:point] + "." + abs[point:]
	text = trimDecimalZeros(text)
	if negative && text != "0" {
		return "-" + text
	}
	return text
}

func supportedRoundingMode(mode v1alpha1.KubeseerRoundingMode) bool {
	switch mode {
	case v1alpha1.RoundingHalfEven, v1alpha1.RoundingHalfAwayFromZero,
		v1alpha1.RoundingTowardZero, v1alpha1.RoundingAwayFromZero:
		return true
	default:
		return false
	}
}
