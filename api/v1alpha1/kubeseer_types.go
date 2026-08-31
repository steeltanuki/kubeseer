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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Kubeseer is the root custom resource for declarative Kubernetes observation.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=kubeseers,singular=kubeseer,scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
type Kubeseer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   KubeseerSpec   `json:"spec"`
	Status KubeseerStatus `json:"status,omitempty"`
}

// KubeseerList contains a list of Kubeseer resources.
//
// +kubebuilder:object:root=true
type KubeseerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Kubeseer `json:"items"`
}

// KubeseerSpec contains the stable foundation envelope for future sources.
type KubeseerSpec struct {
	// +optional
	// +listType=map
	// +listMapKey=id
	// +kubebuilder:validation:MaxItems=32
	Sources []KubeseerSource `json:"sources,omitempty"`
}

// KubeseerSource identifies one Kubernetes resource selection without granting
// permissions or embedding installation-policy configuration.
type KubeseerSource struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ID string `json:"id"`

	// +kubebuilder:validation:Required
	Resource ResourceReference `json:"resource"`

	// A nil pointer means the containing Kubeseer namespace for namespaced
	// resources; a non-nil empty Names list intentionally selects no namespace.
	// +optional
	Namespaces *NamespaceSelection `json:"namespaces,omitempty"`

	// +optional
	Selector *ResourceSelector `json:"selector,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=64
	Fields []KubeseerField `json:"fields,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Aggregations []KubeseerAggregation `json:"aggregations,omitempty"`
}

// KubeseerAggregationFunction identifies one supported source-wide reducer.
// +kubebuilder:validation:Enum=collect;count;sum;min;max;average;first;last;distinct
type KubeseerAggregationFunction string

const (
	AggregationCollect  KubeseerAggregationFunction = "collect"
	AggregationCount    KubeseerAggregationFunction = "count"
	AggregationSum      KubeseerAggregationFunction = "sum"
	AggregationMin      KubeseerAggregationFunction = "min"
	AggregationMax      KubeseerAggregationFunction = "max"
	AggregationAverage  KubeseerAggregationFunction = "average"
	AggregationFirst    KubeseerAggregationFunction = "first"
	AggregationLast     KubeseerAggregationFunction = "last"
	AggregationDistinct KubeseerAggregationFunction = "distinct"
)

// KubeseerRoundingMode identifies the closed set of average rounding rules.
// +kubebuilder:validation:Enum=halfEven;halfAwayFromZero;towardZero;awayFromZero
type KubeseerRoundingMode string

const (
	RoundingHalfEven         KubeseerRoundingMode = "halfEven"
	RoundingHalfAwayFromZero KubeseerRoundingMode = "halfAwayFromZero"
	RoundingTowardZero       KubeseerRoundingMode = "towardZero"
	RoundingAwayFromZero     KubeseerRoundingMode = "awayFromZero"
)

// KubeseerAggregation declares one typed reduction over the containing
// source's extracted field outcomes. Average options are resolved at runtime
// so omission remains distinguishable from invalid options on other functions.
type KubeseerAggregation struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z][A-Za-z0-9]*(?:-[a-z0-9]+)*$`
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	Function KubeseerAggregationFunction `json:"function"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z][A-Za-z0-9]*(?:-[a-z0-9]+)*$`
	Field string `json:"field"`

	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=16
	GroupBy []string `json:"groupBy,omitempty"`

	// +optional
	IncludeProvenance bool `json:"includeProvenance,omitempty"`

	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=18
	Precision *int32 `json:"precision,omitempty"`

	// +optional
	RoundingMode KubeseerRoundingMode `json:"roundingMode,omitempty"`
}

// KubeseerField declares one named native-value extraction from a selected
// resource.
type KubeseerField struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z][A-Za-z0-9]*(?:-[a-z0-9]+)*$`
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	Path string `json:"path"`

	// Type is optional for API compatibility with untyped field declarations.
	// Typed conversion reports a field-scoped missing-type failure when it is
	// omitted.
	// +optional
	Type KubeseerValueType `json:"type,omitempty"`

	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=16
	Operators []KubeseerOperator `json:"operators,omitempty"`
}

// KubeseerValueType identifies the explicit logical type requested for one
// extracted field.
// +kubebuilder:validation:Enum=string;integer;number;boolean;timestamp;duration;quantity;object;list
type KubeseerValueType string

const (
	ValueTypeString    KubeseerValueType = "string"
	ValueTypeInteger   KubeseerValueType = "integer"
	ValueTypeNumber    KubeseerValueType = "number"
	ValueTypeBoolean   KubeseerValueType = "boolean"
	ValueTypeTimestamp KubeseerValueType = "timestamp"
	ValueTypeDuration  KubeseerValueType = "duration"
	ValueTypeQuantity  KubeseerValueType = "quantity"
	ValueTypeObject    KubeseerValueType = "object"
	ValueTypeList      KubeseerValueType = "list"
)

// KubeseerOperatorName identifies one supported field operator.
// +kubebuilder:validation:Enum=eq;ne;gt;gte;lt;lte;contains;startsWith;endsWith;matches;exists;notExists;in;notIn;default;coalesce
type KubeseerOperatorName string

const (
	OperatorEq         KubeseerOperatorName = "eq"
	OperatorNe         KubeseerOperatorName = "ne"
	OperatorGt         KubeseerOperatorName = "gt"
	OperatorGte        KubeseerOperatorName = "gte"
	OperatorLt         KubeseerOperatorName = "lt"
	OperatorLte        KubeseerOperatorName = "lte"
	OperatorContains   KubeseerOperatorName = "contains"
	OperatorStartsWith KubeseerOperatorName = "startsWith"
	OperatorEndsWith   KubeseerOperatorName = "endsWith"
	OperatorMatches    KubeseerOperatorName = "matches"
	OperatorExists     KubeseerOperatorName = "exists"
	OperatorNotExists  KubeseerOperatorName = "notExists"
	OperatorIn         KubeseerOperatorName = "in"
	OperatorNotIn      KubeseerOperatorName = "notIn"
	OperatorDefault    KubeseerOperatorName = "default"
	OperatorCoalesce   KubeseerOperatorName = "coalesce"
)

// KubeseerOperator declares one ordered operation over a typed field.
type KubeseerOperator struct {
	// +kubebuilder:validation:Required
	Operator KubeseerOperatorName `json:"operator"`

	// +optional
	Value *KubeseerOperatorOperand `json:"value,omitempty"`

	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=128
	Values []KubeseerOperatorOperand `json:"values,omitempty"`
}

// KubeseerOperatorOperand is a structural typed operand. Exactly one payload
// branch is valid when State is MatchStateValue; MatchStateNull is rejected by
// runtime operator planning and exists to keep an explicit null distinct from
// an omitted operand.
type KubeseerOperatorOperand struct {
	// +kubebuilder:validation:Required
	State KubeseerMatchState `json:"state"`

	// +optional
	StringValue *string `json:"stringValue,omitempty"`

	// +optional
	IntegerValue *int64 `json:"integerValue,omitempty"`

	// +optional
	NumberValue *string `json:"numberValue,omitempty"`

	// +optional
	BooleanValue *bool `json:"booleanValue,omitempty"`

	// +optional
	TimestampValue *string `json:"timestampValue,omitempty"`

	// +optional
	DurationValue *string `json:"durationValue,omitempty"`

	// +optional
	QuantityValue *string `json:"quantityValue,omitempty"`

	// +optional
	ObjectValue *string `json:"objectValue,omitempty"`

	// +optional
	ListValue *string `json:"listValue,omitempty"`
}

// ResourceReference identifies the API version and Kind resolved through
// Kubernetes discovery.
type ResourceReference struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	APIVersion string `json:"apiVersion"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`
}

// NamespaceSelection narrows a namespaced source to an explicit namespace set.
// Names intentionally omits omitempty so an explicit empty list remains
// distinguishable from an omitted namespace block after serialization.
type NamespaceSelection struct {
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Names []string `json:"names"`
}

// ResourceSelector expresses Kubernetes-native name, label, and field
// selection constraints. All populated mechanisms are combined by the
// selection planner.
type ResourceSelector struct {
	// +optional
	Name string `json:"name,omitempty"`

	// +optional
	// +kubebuilder:validation:MaxProperties=64
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	MatchExpressions []metav1.LabelSelectorRequirement `json:"matchExpressions,omitempty"`

	// +optional
	FieldSelector string `json:"fieldSelector,omitempty"`
}

// KubeseerStatus contains controller-observed state for a Kubeseer resource.
type KubeseerStatus struct {
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	Summary *KubeseerSummary `json:"summary,omitempty"`

	// +optional
	// +kubebuilder:validation:MaxLength=71
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	ResultHash string `json:"resultHash,omitempty"`

	// +optional
	Result *KubeseerResult `json:"result,omitempty"`
}

// KubeseerSummary contains compact counts derived from the structural result.
type KubeseerSummary struct {
	// +kubebuilder:validation:Minimum=0
	SuccessfulSources int64 `json:"successfulSources"`

	// +kubebuilder:validation:Minimum=0
	FailedSources int64 `json:"failedSources"`

	// +kubebuilder:validation:Minimum=0
	MatchedResources int64 `json:"matchedResources"`
}

// KubeseerResult is the structural, ordered typed-output snapshot published
// in status.
type KubeseerResult struct {
	// +optional
	// +listType=atomic
	Sources []KubeseerSourceResult `json:"sources,omitempty"`
}

// KubeseerSourceState identifies whether a source produced values or failed.
// +kubebuilder:validation:Enum=values;error
type KubeseerSourceState string

const (
	SourceStateValues KubeseerSourceState = "values"
	SourceStateError  KubeseerSourceState = "error"
)

// KubeseerFieldState identifies absence, values, or a field-scoped failure.
// +kubebuilder:validation:Enum=absent;values;error
type KubeseerFieldState string

const (
	FieldStateAbsent KubeseerFieldState = "absent"
	FieldStateValues KubeseerFieldState = "values"
	FieldStateError  KubeseerFieldState = "error"
)

// KubeseerMatchState distinguishes a non-null value from an explicit null.
// +kubebuilder:validation:Enum=value;null
type KubeseerMatchState string

const (
	MatchStateValue KubeseerMatchState = "value"
	MatchStateNull  KubeseerMatchState = "null"
)

// KubeseerSourceResult is one source-scoped typed-output result.
type KubeseerSourceResult struct {
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// +kubebuilder:validation:Required
	State KubeseerSourceState `json:"state"`

	// +optional
	// +listType=atomic
	FieldErrors []KubeseerFieldError `json:"fieldErrors,omitempty"`

	// +optional
	// +listType=atomic
	Resources []KubeseerResourceResult `json:"resources,omitempty"`

	// +optional
	// +listType=atomic
	Aggregates []KubeseerAggregateResult `json:"aggregates,omitempty"`

	// +optional
	Error *KubeseerResultError `json:"error,omitempty"`
}

// KubeseerAggregateState identifies the terminal state of one aggregate.
// +kubebuilder:validation:Enum=values;degraded;error
type KubeseerAggregateState string

const (
	AggregateStateValues   KubeseerAggregateState = "values"
	AggregateStateDegraded KubeseerAggregateState = "degraded"
	AggregateStateError    KubeseerAggregateState = "error"
)

// KubeseerAggregateValueState distinguishes an absent scalar from a value
// collection, including an intentionally empty collection.
// +kubebuilder:validation:Enum=absent;values
type KubeseerAggregateValueState string

const (
	AggregateValueAbsent KubeseerAggregateValueState = "absent"
	AggregateValueValues KubeseerAggregateValueState = "values"
)

// KubeseerAggregateResult is one source-scoped aggregate outcome.
type KubeseerAggregateResult struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	Function KubeseerAggregationFunction `json:"function"`

	// +kubebuilder:validation:Required
	Field string `json:"field"`

	// +kubebuilder:validation:Required
	State KubeseerAggregateState `json:"state"`

	// +optional
	// +listType=atomic
	Groups []KubeseerAggregateGroup `json:"groups,omitempty"`

	// +optional
	// +listType=atomic
	Failures []KubeseerAggregateResourceFailure `json:"failures,omitempty"`

	// +optional
	Error *KubeseerResultError `json:"error,omitempty"`
}

// KubeseerAggregateKey is one ordered typed group-key component.
type KubeseerAggregateKey struct {
	// +kubebuilder:validation:Required
	Field string `json:"field"`

	// +kubebuilder:validation:Required
	Type KubeseerValueType `json:"type"`

	// +kubebuilder:validation:Required
	Value KubeseerTypedMatch `json:"value"`
}

// KubeseerAggregateGroup is one ordered key and reduced value.
type KubeseerAggregateGroup struct {
	// +optional
	// +listType=atomic
	Keys []KubeseerAggregateKey `json:"keys,omitempty"`

	// +kubebuilder:validation:Required
	Value KubeseerAggregateValue `json:"value"`

	// +optional
	// +listType=atomic
	Contributors []KubeseerResourceProvenance `json:"contributors,omitempty"`
}

// KubeseerAggregateValue is a typed scalar or collection result.
type KubeseerAggregateValue struct {
	// +kubebuilder:validation:Required
	Type KubeseerValueType `json:"type"`

	// +kubebuilder:validation:Required
	State KubeseerAggregateValueState `json:"state"`

	// +optional
	// +listType=atomic
	Matches []KubeseerAggregateMatch `json:"matches,omitempty"`
}

// KubeseerAggregateMatch is one typed aggregate value and optional
// value-specific provenance.
type KubeseerAggregateMatch struct {
	// +kubebuilder:validation:Required
	Value KubeseerTypedMatch `json:"value"`

	// +optional
	// +listType=atomic
	Contributors []KubeseerResourceProvenance `json:"contributors,omitempty"`
}

// KubeseerResourceProvenance identifies one contributing Kubernetes resource.
type KubeseerResourceProvenance struct {
	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion"`

	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// +optional
	Namespace string `json:"namespace,omitempty"`

	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	UID types.UID `json:"uid"`
}

// KubeseerAggregateResourceFailure associates a sanitized failure with the
// resource whose contribution could not be evaluated.
type KubeseerAggregateResourceFailure struct {
	// +kubebuilder:validation:Required
	Provenance KubeseerResourceProvenance `json:"provenance"`

	// +kubebuilder:validation:Required
	Error KubeseerResultError `json:"error"`
}

// KubeseerResourceResult identifies one contributing selected resource and
// its ordered typed fields.
type KubeseerResourceResult struct {
	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion"`

	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// +optional
	Namespace string `json:"namespace,omitempty"`

	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	UID types.UID `json:"uid"`

	// +optional
	// +listType=atomic
	Fields []KubeseerFieldResult `json:"fields,omitempty"`

	// +optional
	Error *KubeseerResultError `json:"error,omitempty"`
}

// KubeseerFieldResult is one field-scoped typed outcome.
type KubeseerFieldResult struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +optional
	Type KubeseerValueType `json:"type,omitempty"`

	// +kubebuilder:validation:Required
	State KubeseerFieldState `json:"state"`

	// +optional
	// +listType=atomic
	Matches []KubeseerTypedMatch `json:"matches,omitempty"`

	// +optional
	Error *KubeseerResultError `json:"error,omitempty"`
}

// KubeseerTypedMatch is one ordered typed match. Exactly one typed payload is
// populated when State is value; all payloads are omitted for null.
type KubeseerTypedMatch struct {
	// +kubebuilder:validation:Required
	State KubeseerMatchState `json:"state"`

	// +optional
	StringValue *string `json:"stringValue,omitempty"`

	// +optional
	IntegerValue *int64 `json:"integerValue,omitempty"`

	// +optional
	NumberValue *string `json:"numberValue,omitempty"`

	// +optional
	BooleanValue *bool `json:"booleanValue,omitempty"`

	// +optional
	TimestampValue *metav1.Time `json:"timestampValue,omitempty"`

	// +optional
	DurationValue *KubeseerDurationValue `json:"durationValue,omitempty"`

	// +optional
	QuantityValue *KubeseerQuantityValue `json:"quantityValue,omitempty"`

	// +optional
	ObjectValue *string `json:"objectValue,omitempty"`

	// +optional
	ListValue *string `json:"listValue,omitempty"`
}

// KubeseerDurationValue is the canonical duration text and exact nanosecond
// magnitude.
type KubeseerDurationValue struct {
	// +kubebuilder:validation:Required
	Canonical string `json:"canonical"`

	// +kubebuilder:validation:Required
	Nanoseconds int64 `json:"nanoseconds"`
}

// KubeseerQuantityValue is the canonical Kubernetes quantity text and exact
// normalized base-unit decimal magnitude.
type KubeseerQuantityValue struct {
	// +kubebuilder:validation:Required
	Canonical string `json:"canonical"`

	// +kubebuilder:validation:Required
	BaseUnits string `json:"baseUnits"`
}

// KubeseerFieldError is a sanitized field planning failure.
type KubeseerFieldError struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	Reason string `json:"reason"`

	// +optional
	Message string `json:"message,omitempty"`
}

// KubeseerResultError is a sanitized runtime or source-level failure.
type KubeseerResultError struct {
	// +kubebuilder:validation:Required
	Reason string `json:"reason"`

	// +optional
	Message string `json:"message,omitempty"`
}
