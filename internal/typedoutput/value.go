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
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type valueKind uint8

const (
	valueKindString valueKind = iota + 1
	valueKindInteger
	valueKindNumber
	valueKindBoolean
	valueKindTimestamp
	valueKindDuration
	valueKindQuantity
	valueKindObject
	valueKindList
)

type value struct {
	typeName v1alpha1.KubeseerValueType
	kind     valueKind

	stringValue    string
	integerValue   int64
	numberValue    string
	booleanValue   bool
	timestampValue time.Time
	durationValue  time.Duration
	quantityValue  resource.Quantity
	baseUnits      string
	objectValue    map[string]any
	listValue      []any
}

type matchState uint8

const (
	matchStateValue matchState = iota + 1
	matchStateNull
)

// Match is one immutable typed match. Composite accessors return defensive
// copies; scalar accessors return values.
type Match struct {
	state matchState
	value value
}

func newNullMatch(typeName v1alpha1.KubeseerValueType) Match {
	return Match{state: matchStateNull, value: value{typeName: typeName}}
}

func newValueMatch(converted value) Match {
	return Match{state: matchStateValue, value: converted}
}

// Type returns the declared logical type of the match, including null matches.
func (m Match) Type() v1alpha1.KubeseerValueType { return m.value.typeName }

// IsNull reports whether the match represents an explicit null.
func (m Match) IsNull() bool { return m.state == matchStateNull }

// StringValue returns a string payload when this match has the string type.
func (m Match) StringValue() (string, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindString {
		return "", false
	}
	return m.value.stringValue, true
}

// IntegerValue returns an integer payload when this match has the integer
// type.
func (m Match) IntegerValue() (int64, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindInteger {
		return 0, false
	}
	return m.value.integerValue, true
}

// NumberValue returns canonical decimal text when this match has the number
// type.
func (m Match) NumberValue() (string, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindNumber {
		return "", false
	}
	return m.value.numberValue, true
}

// BooleanValue returns a boolean payload when this match has the boolean
// type.
func (m Match) BooleanValue() (bool, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindBoolean {
		return false, false
	}
	return m.value.booleanValue, true
}

// TimestampValue returns the normalized UTC timestamp payload.
func (m Match) TimestampValue() (time.Time, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindTimestamp {
		return time.Time{}, false
	}
	return m.value.timestampValue, true
}

// DurationValue returns the exact duration payload.
func (m Match) DurationValue() (time.Duration, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindDuration {
		return 0, false
	}
	return m.value.durationValue, true
}

// QuantityValue returns the canonical Kubernetes quantity and exact base-unit
// decimal text.
func (m Match) QuantityValue() (resource.Quantity, string, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindQuantity {
		return resource.Quantity{}, "", false
	}
	return m.value.quantityValue.DeepCopy(), m.value.baseUnits, true
}

// ObjectValue returns an independent native object copy.
func (m Match) ObjectValue() (map[string]any, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindObject {
		return nil, false
	}
	clone, err := cloneJSONValue(m.value.objectValue)
	if err != nil {
		return nil, false
	}
	object, ok := clone.(map[string]any)
	return object, ok
}

// ListValue returns an independent native list copy.
func (m Match) ListValue() ([]any, bool) {
	if m.state != matchStateValue || m.value.kind != valueKindList {
		return nil, false
	}
	clone, err := cloneJSONValue(m.value.listValue)
	if err != nil {
		return nil, false
	}
	list, ok := clone.([]any)
	return list, ok
}

func (m Match) clone() Match {
	copy := m
	if m.state != matchStateValue {
		return copy
	}
	switch m.value.kind {
	case valueKindQuantity:
		copy.value.quantityValue = m.value.quantityValue.DeepCopy()
	case valueKindObject:
		if cloned, err := cloneJSONValue(m.value.objectValue); err == nil {
			copy.value.objectValue, _ = cloned.(map[string]any)
		}
	case valueKindList:
		if cloned, err := cloneJSONValue(m.value.listValue); err == nil {
			copy.value.listValue, _ = cloned.([]any)
		}
	}
	return copy
}
