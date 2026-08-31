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

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
)

var fieldNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]*(?:-[a-z0-9]+)*$`)

// CompileSource validates and compiles every field declaration before a plan
// is returned. It performs no resource evaluation and does not use a cache.
// This compatibility wrapper returns the first sorted failure.
func CompileSource(source v1alpha1.KubeseerSource) (Plan, *ExtractionError) {
	plan, failures := CompileSourceAll(source)
	if len(failures) != 0 {
		return Plan{}, failures[0]
	}
	return plan, nil
}

// CompileSourceAll compiles every independently valid field and returns all
// deterministic declaration failures with their submitted field indexes.
func CompileSourceAll(source v1alpha1.KubeseerSource) (Plan, []*ExtractionError) {
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

	compiled := make([]FieldPlan, 0, len(fields))
	failures := make([]*ExtractionError, 0)
	seen := make(map[string]int, len(fields))
	for _, indexed := range fields {
		field := indexed.field
		if !fieldNamePattern.MatchString(field.Name) || len(field.Name) > 63 {
			failures = append(failures, &ExtractionError{SourceID: source.ID, FieldName: field.Name, FieldIndex: indexed.index, Reason: ReasonInvalidInput, Message: "field name is invalid"})
			continue
		}
		if previous, found := seen[field.Name]; found {
			failures = append(failures, &ExtractionError{SourceID: source.ID, FieldName: field.Name, FieldIndex: indexed.index, Reason: ReasonInvalidInput, Message: fmt.Sprintf("field name is duplicated at index %d", previous)})
			continue
		}
		seen[field.Name] = indexed.index
		if len(field.Path) == 0 || len(field.Path) > 1024 {
			failures = append(failures, &ExtractionError{SourceID: source.ID, FieldName: field.Name, FieldIndex: indexed.index, Reason: ReasonInvalidInput, Message: "field path length is invalid"})
			continue
		}
		operations, err := parsePath(source.ID, field.Name, field.Path)
		if err != nil {
			err.FieldIndex = indexed.index
			failures = append(failures, err)
			continue
		}
		compiled = append(compiled, FieldPlan{name: field.Name, path: field.Path, ops: operations})
	}
	sort.SliceStable(compiled, func(left, right int) bool {
		return compiled[left].name < compiled[right].name
	})
	sort.SliceStable(failures, func(left, right int) bool {
		if failures[left].FieldIndex != failures[right].FieldIndex {
			return failures[left].FieldIndex < failures[right].FieldIndex
		}
		if failures[left].Reason != failures[right].Reason {
			return failures[left].Reason < failures[right].Reason
		}
		return failures[left].Message < failures[right].Message
	})
	return Plan{sourceID: source.ID, fields: compiled}, failures
}

// ValidateSource returns all static extraction failures without retaining a
// compiled plan.
func ValidateSource(source v1alpha1.KubeseerSource) []*ExtractionError {
	_, failures := CompileSourceAll(source)
	return failures
}

// CompileBatch compiles all sources independently, preserving input order and
// retaining a source-scoped error for every invalid declaration.
func CompileBatch(sources []v1alpha1.KubeseerSource) []CompileOutcome {
	outcomes := make([]CompileOutcome, len(sources))
	for index, source := range sources {
		plan, err := CompileSource(source)
		outcomes[index] = CompileOutcome{SourceID: source.ID, Plan: plan, Err: err}
	}
	return outcomes
}
