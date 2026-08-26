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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	Fields []KubeseerField `json:"fields,omitempty"`
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
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// +optional
	// +listType=atomic
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
	Result *KubeseerResult `json:"result,omitempty"`
}

// KubeseerResult reserves a typed result envelope for later features.
type KubeseerResult struct{}
