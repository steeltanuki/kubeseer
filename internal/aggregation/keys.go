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

package aggregation

import (
	"bytes"
	"errors"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
)

func makeAggregateKey(field GroupFieldPlan, match typedoutput.Match) (AggregateKey, error) {
	if match.IsNull() || match.Type() != field.typeName {
		return AggregateKey{}, errors.New("grouping match is absent, null, or incorrectly typed")
	}
	canonical, err := typedoutput.CanonicalKey(field.typeName, match)
	if err != nil {
		return AggregateKey{}, err
	}
	return AggregateKey{field: field.name, typeName: field.typeName, match: match, canonical: canonical}, nil
}

func compareKeyTuples(left, right []AggregateKey) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		comparison := compareKeyValues(left[index], right[index])
		if comparison != 0 {
			return comparison
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

func compareKeyValues(left, right AggregateKey) int {
	if left.typeName != right.typeName {
		if left.typeName < right.typeName {
			return -1
		}
		return 1
	}
	if left.typeName == v1alpha1.ValueTypeBoolean {
		leftValue, _ := left.match.BooleanValue()
		rightValue, _ := right.match.BooleanValue()
		if leftValue == rightValue {
			return 0
		}
		if !leftValue {
			return -1
		}
		return 1
	}
	comparison, err := typedoutput.CompareMatches(left.match, right.match)
	if err == nil {
		return comparison
	}
	return bytes.Compare(left.canonical, right.canonical)
}

func equalKeyTuples(left, right []AggregateKey) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].typeName != right[index].typeName || !bytes.Equal(left[index].canonical, right[index].canonical) {
			return false
		}
	}
	return true
}

func tupleCanonicalKey(keys []AggregateKey) string {
	var buffer bytes.Buffer
	for _, key := range keys {
		buffer.Write(key.canonical)
	}
	return buffer.String()
}
