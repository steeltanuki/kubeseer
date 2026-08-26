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
	"regexp"
	"sort"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
)

var fieldNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]*(?:-[a-z0-9]+)*$`)

// CompileSource validates and compiles every field declaration before a plan
// is returned. It performs no resource evaluation and does not use a cache.
func CompileSource(source v1alpha1.KubeseerSource) (Plan, *ExtractionError) {
	fields := append([]v1alpha1.KubeseerField(nil), source.Fields...)
	compiled := make([]FieldPlan, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !fieldNamePattern.MatchString(field.Name) || len(field.Name) > 63 {
			return Plan{}, &ExtractionError{SourceID: source.ID, FieldName: field.Name, Reason: ReasonInvalidInput, Message: "field name is invalid"}
		}
		if _, found := seen[field.Name]; found {
			return Plan{}, &ExtractionError{SourceID: source.ID, FieldName: field.Name, Reason: ReasonInvalidInput, Message: "field name is duplicated"}
		}
		seen[field.Name] = struct{}{}
		if len(field.Path) == 0 || len(field.Path) > 1024 {
			return Plan{}, &ExtractionError{SourceID: source.ID, FieldName: field.Name, Reason: ReasonInvalidInput, Message: "field path length is invalid"}
		}
		operations, err := parsePath(source.ID, field.Name, field.Path)
		if err != nil {
			return Plan{}, err
		}
		compiled = append(compiled, FieldPlan{name: field.Name, path: field.Path, ops: operations})
	}
	sort.SliceStable(compiled, func(left, right int) bool {
		return compiled[left].name < compiled[right].name
	})
	return Plan{sourceID: source.ID, fields: compiled}, nil
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
