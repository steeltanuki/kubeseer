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

const (
	// InstallationAccessCeilingName is the only active installation policy name.
	InstallationAccessCeilingName = "installation-access-ceiling"
)

// NamespaceMode controls how a policy selects namespaces.
type NamespaceMode string

const (
	// NamespaceModeExplicit allows only namespaces in Include.
	NamespaceModeExplicit NamespaceMode = "Explicit"
	// NamespaceModeAll allows every namespace except those in Exclude.
	NamespaceModeAll NamespaceMode = "All"
	// NamespaceModeAllNonSystem allows non-system namespaces and explicit Include entries.
	NamespaceModeAllNonSystem NamespaceMode = "AllNonSystem"
)

// KubeseerAccessPolicy is the administrator-owned installation observation ceiling.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=kubeseeraccesspolicies,singular=kubeseeraccesspolicy,scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'installation-access-ceiling'",message="metadata.name must be installation-access-ceiling"
type KubeseerAccessPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KubeseerAccessPolicySpec `json:"spec"`
}

// KubeseerAccessPolicyList contains a list of KubeseerAccessPolicy resources.
//
// +kubebuilder:object:root=true
type KubeseerAccessPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeseerAccessPolicy `json:"items"`
}

// KubeseerAccessPolicySpec defines the maximum observation scope available to Kubeseer.
type KubeseerAccessPolicySpec struct {
	Namespaces NamespacePolicy `json:"namespaces"`
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=128
	Resources []ResourceRule `json:"resources,omitempty"`
	// +optional
	// +kubebuilder:default:=false
	AllowClusterScoped bool `json:"allowClusterScoped,omitempty"`
}

// NamespacePolicy defines the namespace boundary of an installation policy.
type NamespacePolicy struct {
	// +kubebuilder:validation:Enum=Explicit;All;AllNonSystem
	Mode NamespaceMode `json:"mode"`

	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=256
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Include []string `json:"include,omitempty"`

	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=256
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Exclude []string `json:"exclude,omitempty"`

	// This field intentionally omits omitempty: nil and an explicit empty list
	// have different defaulting semantics for system namespace classification.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=256
	// +kubebuilder:default:={"kube-system", "kube-public", "kube-node-lease"}
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	SystemNamespaces []string `json:"systemNamespaces"`
}

// ResourceRule allows exact Kubernetes API group and Kind combinations.
type ResourceRule struct {
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=253
	// +kubebuilder:validation:items:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	APIGroups []string `json:"apiGroups"`
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[A-Z][A-Za-z0-9]*$`
	Kinds []string `json:"kinds"`
}
