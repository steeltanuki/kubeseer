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

package admission

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
)

var kindIdentifierPattern = regexp.MustCompile("^[A-Z][A-Za-z0-9]*$")

// ValidateKubeseerSemantics runs all deterministic, dependency-free
// validation stages. It never calls discovery, policy sources, or resource
// clients and never mutates the proposed object.
func ValidateKubeseerSemantics(object *v1alpha1.Kubeseer) Result {
	if object == nil {
		return result([]Issue{invalidIssue("spec", "MissingObject", "object must be provided")})
	}

	issues := make([]Issue, 0)
	seenSources := make(map[string]int, len(object.Spec.Sources))
	for sourceIndex, source := range object.Spec.Sources {
		prefix := fmt.Sprintf("spec.sources[%d]", sourceIndex)
		if previous, found := seenSources[source.ID]; found {
			issues = append(issues, invalidIssue(prefix+".id", "DuplicateSourceID", fmt.Sprintf("source identifier must be unique; first declared at index %d", previous)))
		} else if source.ID != "" {
			seenSources[source.ID] = sourceIndex
		}
		issues = append(issues, validateSourceIdentity(prefix, source)...)
		issues = append(issues, validateNamespaces(prefix, source)...)
		issues = append(issues, validateSelector(prefix, source)...)
		issues = append(issues, validateSourceDeclarations(prefix, source)...)
	}
	return result(issues)
}

// ValidateAccessPolicySemantics validates a proposed policy through the
// shared policy compiler's complete declaration validator.
func ValidateAccessPolicySemantics(object *v1alpha1.KubeseerAccessPolicy) Result {
	issues := make([]Issue, 0)
	for _, failure := range accesspolicy.Validate(object) {
		if failure == nil {
			continue
		}
		issues = append(issues, invalidIssue(failure.Field, string(failure.Reason), failure.Message))
	}
	return result(issues)
}

func validateSourceIdentity(prefix string, source v1alpha1.KubeseerSource) []Issue {
	issues := make([]Issue, 0)
	if source.ID == "" || len(source.ID) > 63 || len(validation.IsDNS1123Label(source.ID)) != 0 {
		issues = append(issues, invalidIssue(prefix+".id", "InvalidSourceID", "source id must be a valid DNS-1123 label"))
	}
	if strings.TrimSpace(source.Resource.APIVersion) == "" {
		issues = append(issues, invalidIssue(prefix+".resource.apiVersion", "InvalidAPIVersion", "apiVersion must be a non-empty Kubernetes group-version"))
	} else if parsed, err := schema.ParseGroupVersion(source.Resource.APIVersion); err != nil || parsed.Version == "" {
		issues = append(issues, invalidIssue(prefix+".resource.apiVersion", "InvalidAPIVersion", "apiVersion must be a valid Kubernetes group-version"))
	}
	if source.Resource.Kind == "" || !kindIdentifierPattern.MatchString(source.Resource.Kind) {
		issues = append(issues, invalidIssue(prefix+".resource.kind", "InvalidKind", "Kind must be a valid CamelCase Kubernetes identifier"))
	}
	return issues
}

func validateNamespaces(prefix string, source v1alpha1.KubeseerSource) []Issue {
	if source.Namespaces == nil {
		return nil
	}
	issues := make([]Issue, 0)
	seen := make(map[string]int, len(source.Namespaces.Names))
	for index, namespace := range source.Namespaces.Names {
		path := fmt.Sprintf("%s.namespaces.names[%d]", prefix, index)
		if previous, found := seen[namespace]; found {
			issues = append(issues, invalidIssue(path, "DuplicateNamespace", fmt.Sprintf("namespace must be unique; first declared at index %d", previous)))
		} else {
			seen[namespace] = index
		}
		if len(validation.IsDNS1123Label(namespace)) != 0 {
			issues = append(issues, invalidIssue(path, "InvalidNamespace", "namespace must be a valid DNS-1123 label"))
		}
	}
	return issues
}

func validateSelector(prefix string, source v1alpha1.KubeseerSource) []Issue {
	selector := source.Selector
	if selector == nil {
		return nil
	}
	issues := make([]Issue, 0)
	if selector.Name != "" && len(validation.IsDNS1123Subdomain(selector.Name)) != 0 {
		issues = append(issues, invalidIssue(prefix+".selector.name", "InvalidResourceName", "name must be a valid Kubernetes resource-name path segment"))
	}
	if _, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels:      selector.MatchLabels,
		MatchExpressions: selector.MatchExpressions,
	}); err != nil {
		issues = append(issues, invalidIssue(prefix+".selector", "InvalidLabelSelector", "label selector is invalid under Kubernetes selector rules"))
	}
	if _, err := fields.ParseSelector(selector.FieldSelector); err != nil {
		issues = append(issues, invalidIssue(prefix+".selector.fieldSelector", "InvalidFieldSelector", "field selector is invalid under Kubernetes selector rules"))
	}
	return issues
}

func validateSourceDeclarations(prefix string, source v1alpha1.KubeseerSource) []Issue {
	issues := make([]Issue, 0)
	fieldIndexes := make(map[string]int, len(source.Fields))
	for index, field := range source.Fields {
		if previous, found := fieldIndexes[field.Name]; found {
			issues = append(issues, invalidIssue(fmt.Sprintf("%s.fields[%d].name", prefix, index), "DuplicateFieldName", fmt.Sprintf("field name must be unique; first declared at index %d", previous)))
		} else {
			fieldIndexes[field.Name] = index
		}
	}

	for _, failure := range extraction.ValidateSource(source) {
		if failure == nil {
			continue
		}
		fieldIndex := failure.FieldIndex
		if fieldIndex < 0 || fieldIndex >= len(source.Fields) {
			fieldIndex = firstFieldIndex(source.Fields, failure.FieldName)
		}
		fieldPath := fmt.Sprintf("%s.fields[%d]", prefix, fieldIndex)
		path := fieldPath + ".path"
		message := "field path is invalid"
		if strings.Contains(failure.Message, "field name") {
			path = fieldPath + ".name"
			message = "field name must be a valid unique identifier"
		}
		issues = append(issues, invalidIssue(path, string(failure.Reason), message))
	}

	typed := typedoutput.CompileSource(source)
	for _, failure := range typed.Failures() {
		if failure == nil {
			continue
		}
		fieldIndex := failure.FieldIndex
		if fieldIndex < 0 || fieldIndex >= len(source.Fields) {
			fieldIndex = firstFieldIndex(source.Fields, failure.FieldName)
		}
		issues = append(issues, invalidIssue(
			fmt.Sprintf("%s.fields[%d].type", prefix, fieldIndex),
			string(failure.Reason),
			typedFieldMessage(failure.Reason),
		))
	}

	operatorOutcome := operators.CompileSource(source)
	for _, failure := range operatorOutcome.Failures() {
		if failure == nil || failure.OperatorIndex < 0 {
			continue
		}
		fieldIndex := failure.FieldIndex
		if fieldIndex < 0 || fieldIndex >= len(source.Fields) {
			fieldIndex = firstFieldIndex(source.Fields, failure.FieldName)
		}
		if fieldIndex < 0 || fieldIndex >= len(source.Fields) || source.Fields[fieldIndex].Type == "" {
			continue
		}
		operatorPath := fmt.Sprintf("%s.fields[%d].operators[%d]", prefix, fieldIndex, failure.OperatorIndex)
		if failure.ValueIndex >= 0 {
			operatorPath += fmt.Sprintf(".values[%d]", failure.ValueIndex)
		} else if failure.Reason == operators.ReasonInvalidOperand {
			operatorPath += ".value"
		} else {
			operatorPath += ".operator"
		}
		issues = append(issues, invalidIssue(operatorPath, string(failure.Reason), operatorMessage(failure.Reason)))
	}

	aggregationOutcome := aggregation.PlanSource(source, aggregation.DefaultLimits())
	for _, failure := range aggregationOutcome.Failures() {
		if failure == nil || failure.GroupByIndex >= 0 {
			continue
		}
		aggregationIndex := failure.AggregateIndex
		if aggregationIndex < 0 || aggregationIndex >= len(source.Aggregations) {
			aggregationIndex = firstAggregationIndex(source.Aggregations, failure.AggregateName)
		}
		path := fmt.Sprintf("%s.aggregations[%d].name", prefix, aggregationIndex)
		switch failure.Reason {
		case aggregation.ReasonUnknownField, aggregation.ReasonTargetFieldError:
			path = fmt.Sprintf("%s.aggregations[%d].field", prefix, aggregationIndex)
		case aggregation.ReasonIncompatibleFunction:
			path = fmt.Sprintf("%s.aggregations[%d].function", prefix, aggregationIndex)
		case aggregation.ReasonInvalidAverageOptions:
			path = fmt.Sprintf("%s.aggregations[%d]", prefix, aggregationIndex)
		}
		issues = append(issues, invalidIssue(path, string(failure.Reason), aggregationMessage(failure.Reason)))
	}
	issues = append(issues, validateGroupingFields(prefix, source)...)
	return issues
}

func validateGroupingFields(prefix string, source v1alpha1.KubeseerSource) []Issue {
	fieldTypes := make(map[string]v1alpha1.KubeseerValueType, len(source.Fields))
	for _, field := range source.Fields {
		if _, found := fieldTypes[field.Name]; !found {
			fieldTypes[field.Name] = field.Type
		}
	}
	issues := make([]Issue, 0)
	for aggregationIndex, declaration := range source.Aggregations {
		seen := make(map[string]int, len(declaration.GroupBy))
		for groupIndex, groupField := range declaration.GroupBy {
			path := fmt.Sprintf("%s.aggregations[%d].groupBy[%d]", prefix, aggregationIndex, groupIndex)
			if previous, found := seen[groupField]; found {
				issues = append(issues, invalidIssue(path, string(aggregation.ReasonDuplicateGroupField), fmt.Sprintf("grouping field must be unique; first declared at index %d", previous)))
			} else {
				seen[groupField] = groupIndex
			}
			fieldType, found := fieldTypes[groupField]
			if !found {
				issues = append(issues, invalidIssue(path, string(aggregation.ReasonUnknownField), "grouping field must reference a declared field"))
				continue
			}
			if !supportedGroupingType(fieldType) {
				issues = append(issues, invalidIssue(path, string(aggregation.ReasonUnsupportedGroupType), "grouping field type is not supported as a key"))
			}
		}
	}
	return issues
}

func firstFieldIndex(fields []v1alpha1.KubeseerField, name string) int {
	for index, field := range fields {
		if field.Name == name {
			return index
		}
	}
	return 0
}

func firstAggregationIndex(aggregations []v1alpha1.KubeseerAggregation, name string) int {
	for index, aggregation := range aggregations {
		if aggregation.Name == name {
			return index
		}
	}
	return 0
}

func supportedGroupingType(fieldType v1alpha1.KubeseerValueType) bool {
	switch fieldType {
	case v1alpha1.ValueTypeString, v1alpha1.ValueTypeInteger, v1alpha1.ValueTypeNumber,
		v1alpha1.ValueTypeBoolean, v1alpha1.ValueTypeTimestamp, v1alpha1.ValueTypeDuration,
		v1alpha1.ValueTypeQuantity:
		return true
	default:
		return false
	}
}

func typedFieldMessage(reason typedoutput.ConversionErrorReason) string {
	switch reason {
	case typedoutput.ReasonMissingType:
		return "field type is required"
	case typedoutput.ReasonUnsupportedType:
		return "field type is unsupported"
	default:
		return "field type is invalid"
	}
}

func operatorMessage(reason operators.Reason) string {
	switch reason {
	case operators.ReasonUnsupportedOperator:
		return "operator is not supported"
	case operators.ReasonIncompatibleOperator:
		return "operator is incompatible with field type"
	case operators.ReasonInvalidArity:
		return "operator operand arity is invalid"
	case operators.ReasonInvalidPattern:
		return "regular expression is invalid"
	default:
		return "operator operand is invalid"
	}
}

func aggregationMessage(reason aggregation.Reason) string {
	switch reason {
	case aggregation.ReasonDuplicateAggregate:
		return "aggregate name must be unique"
	case aggregation.ReasonUnknownField:
		return "aggregate must reference a declared field"
	case aggregation.ReasonIncompatibleFunction:
		return "aggregate function is incompatible with the target field type"
	case aggregation.ReasonInvalidAverageOptions:
		return "average options are invalid for the aggregate function"
	default:
		return "aggregate declaration is invalid"
	}
}
