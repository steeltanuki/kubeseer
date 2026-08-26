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

package extraction

import "github.com/steeltanuki/kubeseer/internal/selection"

// MatchSet preserves the distinction between no match and one match whose
// native value is nil. Values are stored privately and copied on access.
type MatchSet struct {
	values []any
}

// Len returns the number of native matches.
func (m MatchSet) Len() int { return len(m.values) }

// Values returns independent deep copies of all native matches.
func (m MatchSet) Values() []any {
	if m.values == nil {
		return nil
	}
	values := make([]any, len(m.values))
	for index, value := range m.values {
		clone, err := cloneJSONValue(value)
		if err != nil {
			return nil
		}
		values[index] = clone
	}
	return values
}

// FieldOutcome contains one field's ordered native matches for a resource.
type FieldOutcome struct {
	FieldName string
	Matches   MatchSet
}

// ResourceOutcome contains field outcomes and complete selection provenance
// for one selected object.
type ResourceOutcome struct {
	Provenance selection.Provenance
	Fields     []FieldOutcome
}
