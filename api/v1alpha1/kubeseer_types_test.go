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
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("add Kubeseer types to scheme: %v", err)
	}

	for _, object := range []runtime.Object{&Kubeseer{}, &KubeseerList{}} {
		gvks, _, err := scheme.ObjectKinds(object)
		if err != nil {
			t.Fatalf("resolve GVK for %T: %v", object, err)
		}
		if len(gvks) != 1 || gvks[0].GroupVersion() != GroupVersion {
			t.Fatalf("unexpected GVKs for %T: %v", object, gvks)
		}
	}

	if _, err := scheme.New(GroupVersion.WithKind("Kubeseer")); err != nil {
		t.Fatalf("construct Kubeseer from scheme: %v", err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	original := Kubeseer{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "Kubeseer"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
		},
		Spec: KubeseerSpec{
			Sources: []KubeseerSource{{ID: "source-one"}},
		},
		Status: KubeseerStatus{
			ObservedGeneration: 7,
			Conditions: []metav1.Condition{{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 7,
				LastTransitionTime: metav1.Time{Time: time.Date(2026, time.August, 1, 11, 30, 0, 0, time.UTC)},
				Reason:             "Available",
				Message:            "API contract is available",
			}},
			Result: &KubeseerResult{},
		},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal Kubeseer: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("inspect Kubeseer JSON: %v", err)
	}
	for _, field := range []string{"apiVersion", "kind", "metadata", "spec", "status"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("serialized Kubeseer is missing %q: %s", field, encoded)
		}
	}

	var decoded Kubeseer
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal Kubeseer: %v", err)
	}
	if decoded.TypeMeta != original.TypeMeta || decoded.Name != original.Name || decoded.Namespace != original.Namespace {
		t.Fatalf("round-trip changed object identity: original=%#v decoded=%#v", original, decoded)
	}
	if !reflect.DeepEqual(original.Spec, decoded.Spec) {
		t.Fatalf("round-trip changed spec: original=%#v decoded=%#v", original.Spec, decoded.Spec)
	}
	if decoded.Status.ObservedGeneration != original.Status.ObservedGeneration || decoded.Status.Result == nil {
		t.Fatalf("round-trip changed status envelope: original=%#v decoded=%#v", original.Status, decoded.Status)
	}
	if len(decoded.Status.Conditions) != 1 {
		t.Fatalf("round-trip changed conditions: %#v", decoded.Status.Conditions)
	}
	originalCondition := original.Status.Conditions[0]
	decodedCondition := decoded.Status.Conditions[0]
	if decodedCondition.Type != originalCondition.Type || decodedCondition.Status != originalCondition.Status ||
		decodedCondition.ObservedGeneration != originalCondition.ObservedGeneration ||
		decodedCondition.Reason != originalCondition.Reason || decodedCondition.Message != originalCondition.Message ||
		!decodedCondition.LastTransitionTime.Time.Equal(originalCondition.LastTransitionTime.Time) {
		t.Fatalf("round-trip changed condition: original=%#v decoded=%#v", originalCondition, decodedCondition)
	}

	withoutSources := Kubeseer{TypeMeta: original.TypeMeta, Spec: KubeseerSpec{}}
	withoutSourcesJSON, err := json.Marshal(withoutSources)
	if err != nil {
		t.Fatalf("marshal Kubeseer without sources: %v", err)
	}
	var omitted map[string]json.RawMessage
	if err := json.Unmarshal(withoutSourcesJSON, &omitted); err != nil {
		t.Fatalf("inspect Kubeseer without sources: %v", err)
	}
	var specFields map[string]json.RawMessage
	if err := json.Unmarshal(omitted["spec"], &specFields); err != nil {
		t.Fatalf("inspect empty spec: %v", err)
	}
	if _, ok := specFields["sources"]; ok {
		t.Fatalf("omitted sources field was synthesized: %s", withoutSourcesJSON)
	}
}

func TestDeepCopyIsolation(t *testing.T) {
	original := &Kubeseer{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"environment": "test"},
		},
		Spec: KubeseerSpec{
			Sources: []KubeseerSource{{ID: "source-one"}},
		},
		Status: KubeseerStatus{
			Conditions: []metav1.Condition{{Type: "Ready", Reason: "Available"}},
			Result:     &KubeseerResult{},
		},
	}

	copy := original.DeepCopy()
	if copy == original {
		t.Fatal("DeepCopy returned the original pointer")
	}
	if copy.Status.Result == nil {
		t.Fatal("DeepCopy dropped the result envelope")
	}

	copy.Labels["environment"] = "production"
	copy.Spec.Sources[0].ID = "changed"
	copy.Status.Conditions[0].Reason = "Changed"

	if original.Labels["environment"] != "test" {
		t.Fatalf("metadata labels alias original: %#v", original.Labels)
	}
	if original.Spec.Sources[0].ID != "source-one" {
		t.Fatalf("spec sources alias original: %#v", original.Spec.Sources)
	}
	if original.Status.Conditions[0].Reason != "Available" {
		t.Fatalf("status conditions alias original: %#v", original.Status.Conditions)
	}
}

func TestGeneratedCRDContract(t *testing.T) {
	crdPath := filepath.Join("..", "..", "config", "crd", "bases", "kubeseer.io_kubeseers.yaml")
	file, err := os.Open(crdPath)
	if err != nil {
		t.Fatalf("open generated CRD %q: %v", crdPath, err)
	}
	defer file.Close()

	var crd apiextensionsv1.CustomResourceDefinition
	if err := utilyaml.NewYAMLOrJSONDecoder(file, 4096).Decode(&crd); err != nil {
		t.Fatalf("decode generated CRD: %v", err)
	}

	if crd.Name != "kubeseers.kubeseer.io" {
		t.Fatalf("unexpected CRD name: %q", crd.Name)
	}
	if crd.Spec.Group != GroupVersion.Group {
		t.Fatalf("unexpected CRD group: %q", crd.Spec.Group)
	}
	if crd.Spec.Scope != apiextensionsv1.NamespaceScoped {
		t.Fatalf("unexpected CRD scope: %q", crd.Spec.Scope)
	}
	if crd.Spec.Names.Kind != "Kubeseer" || crd.Spec.Names.ListKind != "KubeseerList" ||
		crd.Spec.Names.Plural != "kubeseers" || crd.Spec.Names.Singular != "kubeseer" {
		t.Fatalf("unexpected CRD names: %#v", crd.Spec.Names)
	}
	if len(crd.Spec.Versions) != 1 {
		t.Fatalf("expected exactly one served API version, got %d", len(crd.Spec.Versions))
	}

	version := crd.Spec.Versions[0]
	if version.Name != GroupVersion.Version || !version.Served || !version.Storage {
		t.Fatalf("unexpected version contract: %#v", version)
	}
	if version.Subresources == nil || version.Subresources.Status == nil {
		t.Fatal("status subresource is not generated")
	}
	if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
		t.Fatal("generated CRD has no OpenAPI schema")
	}

	schema := version.Schema.OpenAPIV3Schema
	if schema.Type != "object" {
		t.Fatalf("root schema is not structural object: %q", schema.Type)
	}
	for _, required := range []string{"apiVersion", "kind", "metadata", "spec"} {
		if !contains(schema.Required, required) && required == "spec" {
			t.Fatalf("root schema does not require %q: %v", required, schema.Required)
		}
		if _, ok := schema.Properties[required]; !ok {
			t.Fatalf("root schema does not expose %q", required)
		}
	}

	spec, ok := schema.Properties["spec"]
	if !ok || spec.Type != "object" {
		t.Fatalf("spec schema is missing or not an object: %#v", spec)
	}
	if len(spec.Properties) != 1 {
		t.Fatalf("spec exposes fields outside the API foundation: %v", sortedSchemaKeys(spec.Properties))
	}
	sources, ok := spec.Properties["sources"]
	if !ok || sources.Type != "array" || sources.Items == nil || sources.Items.Schema == nil {
		t.Fatalf("sources schema is missing or not an array of objects: %#v", sources)
	}
	if sources.XListType == nil || *sources.XListType != "map" || len(sources.XListMapKeys) != 1 || sources.XListMapKeys[0] != "id" {
		t.Fatalf("sources list-map contract is incorrect: %#v", sources)
	}
	source := sources.Items.Schema
	if source.Type != "object" || len(source.Properties) != 1 || !contains(source.Required, "id") {
		t.Fatalf("source envelope contract is incorrect: %#v", source)
	}
	idSchema, ok := source.Properties["id"]
	if !ok || idSchema.Type != "string" || idSchema.MaxLength == nil || *idSchema.MaxLength != 63 ||
		idSchema.Pattern != `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` {
		t.Fatalf("source ID validation contract is incorrect: %#v", idSchema)
	}

	status, ok := schema.Properties["status"]
	if !ok || status.Type != "object" {
		t.Fatalf("status schema is missing or not an object: %#v", status)
	}
	observedGeneration, ok := status.Properties["observedGeneration"]
	if !ok || observedGeneration.Type != "integer" || observedGeneration.Format != "int64" ||
		observedGeneration.Minimum == nil || *observedGeneration.Minimum != 0 {
		t.Fatalf("observedGeneration validation contract is incorrect: %#v", observedGeneration)
	}
	conditions, ok := status.Properties["conditions"]
	if !ok || conditions.Type != "array" || conditions.Items == nil || conditions.Items.Schema == nil ||
		conditions.XListType == nil || *conditions.XListType != "map" || len(conditions.XListMapKeys) != 1 || conditions.XListMapKeys[0] != "type" {
		t.Fatalf("conditions schema is incorrect: %#v", conditions)
	}
	result, ok := status.Properties["result"]
	if !ok || result.Type != "object" {
		t.Fatalf("result schema is missing or not a typed object: %#v", result)
	}

	if status.XPreserveUnknownFields != nil || spec.XPreserveUnknownFields != nil || result.XPreserveUnknownFields != nil {
		t.Fatal("API foundation permits preserved unknown fields")
	}
	if len(result.Properties) != 0 || result.AdditionalProperties != nil {
		t.Fatalf("result exposes an unreviewed payload surface: %#v", result)
	}
	assertNoDefaults(t, schema)
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func sortedSchemaKeys(values map[string]apiextensionsv1.JSONSchemaProps) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertNoDefaults(t *testing.T, schema *apiextensionsv1.JSONSchemaProps) {
	t.Helper()
	if schema.Default != nil {
		t.Fatalf("generated schema contains an implicit default: %#v", schema.Default)
	}
	for _, property := range schema.Properties {
		property := property
		assertNoDefaults(t, &property)
	}
	if schema.Items != nil && schema.Items.Schema != nil {
		assertNoDefaults(t, schema.Items.Schema)
	}
}
