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
	"sort"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
)

// CompileSource validates every field type before any native match is
// converted. Valid plans are sorted by field name; invalid declarations remain
// field-local failures so independent plans survive.
func CompileSource(source v1alpha1.KubeseerSource) PlanOutcome {
	type indexedField struct {
		field v1alpha1.KubeseerField
		index int
	}
	fields := make([]indexedField, len(source.Fields))
	for index, field := range source.Fields {
		fields[index] = indexedField{field: field, index: index}
	}
	sort.SliceStable(fields, func(left, right int) bool {
		return fields[left].field.Name < fields[right].field.Name
	})

	valid := make([]FieldPlan, 0, len(fields))
	failures := make([]*ConversionError, 0)
	for _, indexed := range fields {
		field := indexed.field
		switch {
		case field.Type == "":
			err := invalidPlanError(source.ID, field.Name, ReasonMissingType, "field type is required")
			err.FieldIndex = indexed.index
			failures = append(failures, err)
		case !IsSupportedType(field.Type):
			err := invalidPlanError(source.ID, field.Name, ReasonUnsupportedType, "field type is unsupported")
			err.FieldIndex = indexed.index
			failures = append(failures, err)
		default:
			valid = append(valid, FieldPlan{sourceID: source.ID, name: field.Name, typeName: field.Type})
		}
	}

	plan := Plan{sourceID: source.ID, fields: valid, failures: failures}
	return PlanOutcome{sourceID: source.ID, plan: plan, failures: failures}
}

// CompileBatch compiles all source declarations independently and preserves
// source input order.
func CompileBatch(sources []v1alpha1.KubeseerSource) []PlanOutcome {
	outcomes := make([]PlanOutcome, len(sources))
	for index, source := range sources {
		outcomes[index] = CompileSource(source)
	}
	return outcomes
}

// IsSupportedType reports whether a field type belongs to the closed logical
// type set approved by the public API.
func IsSupportedType(valueType v1alpha1.KubeseerValueType) bool {
	switch valueType {
	case v1alpha1.ValueTypeString,
		v1alpha1.ValueTypeInteger,
		v1alpha1.ValueTypeNumber,
		v1alpha1.ValueTypeBoolean,
		v1alpha1.ValueTypeTimestamp,
		v1alpha1.ValueTypeDuration,
		v1alpha1.ValueTypeQuantity,
		v1alpha1.ValueTypeObject,
		v1alpha1.ValueTypeList:
		return true
	default:
		return false
	}
}
