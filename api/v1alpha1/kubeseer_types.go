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

// KubeseerSource identifies a future observation source without granting permissions.
type KubeseerSource struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ID string `json:"id"`
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
