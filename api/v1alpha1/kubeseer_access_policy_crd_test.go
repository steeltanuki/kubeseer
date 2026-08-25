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
	"os"
	"path/filepath"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestKubeseerAccessPolicyGeneratedCRDContract(t *testing.T) {
	crdPath := filepath.Join("..", "..", "config", "crd", "bases", "kubeseer.io_kubeseeraccesspolicies.yaml")
	file, err := os.Open(crdPath)
	if err != nil {
		t.Fatalf("open generated access policy CRD %q: %v", crdPath, err)
	}
	defer file.Close()

	var crd apiextensionsv1.CustomResourceDefinition
	if err := utilyaml.NewYAMLOrJSONDecoder(file, 4096).Decode(&crd); err != nil {
		t.Fatalf("decode generated access policy CRD: %v", err)
	}

	if crd.Name != "kubeseeraccesspolicies.kubeseer.io" {
		t.Fatalf("unexpected access policy CRD name: %q", crd.Name)
	}
	if crd.Spec.Group != GroupVersion.Group || crd.Spec.Scope != apiextensionsv1.ClusterScoped {
		t.Fatalf("unexpected access policy CRD identity: group=%q scope=%q", crd.Spec.Group, crd.Spec.Scope)
	}
	if crd.Spec.Names.Kind != "KubeseerAccessPolicy" ||
		crd.Spec.Names.ListKind != "KubeseerAccessPolicyList" ||
		crd.Spec.Names.Plural != "kubeseeraccesspolicies" ||
		crd.Spec.Names.Singular != "kubeseeraccesspolicy" {
		t.Fatalf("unexpected access policy CRD names: %#v", crd.Spec.Names)
	}
	if len(crd.Spec.Versions) != 1 {
		t.Fatalf("expected exactly one served access policy version, got %d", len(crd.Spec.Versions))
	}

	version := crd.Spec.Versions[0]
	if version.Name != GroupVersion.Version || !version.Served || !version.Storage {
		t.Fatalf("unexpected access policy version contract: %#v", version)
	}
	if version.Subresources != nil && version.Subresources.Status != nil {
		t.Fatal("access policy CRD unexpectedly exposes a status subresource")
	}
	if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
		t.Fatal("generated access policy CRD has no OpenAPI schema")
	}

	schema := version.Schema.OpenAPIV3Schema
	if schema.Type != "object" || !contains(schema.Required, "spec") {
		t.Fatalf("access policy root schema is not a required structural object: %#v", schema)
	}
	if len(schema.XValidations) != 1 || schema.XValidations[0].Rule != "self.metadata.name == 'installation-access-ceiling'" ||
		schema.XValidations[0].Message != "metadata.name must be installation-access-ceiling" {
		t.Fatalf("singleton-name validation contract is incorrect: %#v", schema.XValidations)
	}

	spec, ok := schema.Properties["spec"]
	if !ok || spec.Type != "object" || !contains(spec.Required, "namespaces") {
		t.Fatalf("access policy spec schema is missing or not required: %#v", spec)
	}
	if len(spec.Properties) != 3 || sortedSchemaKeys(spec.Properties)[0] != "allowClusterScoped" {
		t.Fatalf("access policy spec exposes an unexpected field surface: %v", sortedSchemaKeys(spec.Properties))
	}

	namespaces, ok := spec.Properties["namespaces"]
	if !ok || namespaces.Type != "object" || !contains(namespaces.Required, "mode") {
		t.Fatalf("namespace policy schema is missing or not required: %#v", namespaces)
	}
	mode, ok := namespaces.Properties["mode"]
	if !ok || mode.Type != "string" || !equalJSONValues(mode.Enum, []string{"Explicit", "All", "AllNonSystem"}) {
		t.Fatalf("namespace mode enum is incorrect: %#v", mode)
	}
	for _, field := range []string{"include", "exclude", "systemNamespaces"} {
		list, ok := namespaces.Properties[field]
		if !ok || list.Type != "array" || list.Items == nil || list.Items.Schema == nil ||
			list.XListType == nil || *list.XListType != "set" {
			t.Fatalf("namespace list %q is not a set of strings: %#v", field, list)
		}
		if list.Items.Schema.Type != "string" || list.Items.Schema.MaxLength == nil || *list.Items.Schema.MaxLength != 63 ||
			list.Items.Schema.Pattern != `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` {
			t.Fatalf("namespace list %q item validation is incorrect: %#v", field, list.Items.Schema)
		}
	}
	systemNamespaces := namespaces.Properties["systemNamespaces"]
	if string(systemNamespaces.Default.Raw) != `["kube-system","kube-public","kube-node-lease"]` {
		t.Fatalf("system namespace default is incorrect: %s", systemNamespaces.Default.Raw)
	}

	resources, ok := spec.Properties["resources"]
	if !ok || resources.Type != "array" || resources.Items == nil || resources.Items.Schema == nil ||
		resources.XListType == nil || *resources.XListType != "atomic" {
		t.Fatalf("resource rules are not an atomic list: %#v", resources)
	}
	resourceRule := resources.Items.Schema
	if resourceRule.Type != "object" || !contains(resourceRule.Required, "apiGroups") || !contains(resourceRule.Required, "kinds") {
		t.Fatalf("resource rule required fields are incorrect: %#v", resourceRule)
	}
	apiGroups := resourceRule.Properties["apiGroups"]
	kinds := resourceRule.Properties["kinds"]
	if apiGroups.Type != "array" || apiGroups.MinItems == nil || *apiGroups.MinItems != 1 || apiGroups.XListType == nil || *apiGroups.XListType != "set" ||
		apiGroups.Items == nil || apiGroups.Items.Schema == nil || apiGroups.Items.Schema.Pattern != `^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` {
		t.Fatalf("API group validation contract is incorrect: %#v", apiGroups)
	}
	if kinds.Type != "array" || kinds.MinItems == nil || *kinds.MinItems != 1 || kinds.XListType == nil || *kinds.XListType != "set" ||
		kinds.Items == nil || kinds.Items.Schema == nil || kinds.Items.Schema.Pattern != `^[A-Z][A-Za-z0-9]*$` {
		t.Fatalf("Kind validation contract is incorrect: %#v", kinds)
	}

	allowClusterScoped, ok := spec.Properties["allowClusterScoped"]
	if !ok || allowClusterScoped.Type != "boolean" || allowClusterScoped.Default == nil || string(allowClusterScoped.Default.Raw) != "false" {
		t.Fatalf("cluster-scoped access default is incorrect: %#v", allowClusterScoped)
	}
	if schema.XPreserveUnknownFields != nil || spec.XPreserveUnknownFields != nil || namespaces.XPreserveUnknownFields != nil || resourceRule.XPreserveUnknownFields != nil {
		t.Fatal("access policy schema permits preserved unknown fields")
	}
}

func equalJSONValues(values []apiextensionsv1.JSON, expected []string) bool {
	if len(values) != len(expected) {
		return false
	}
	for index, value := range values {
		if string(value.Raw) != `"`+expected[index]+`"` {
			return false
		}
	}
	return true
}
