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
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	harness "github.com/steeltanuki/kubeseer/test/envtest"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

const (
	kubeseerResource    = "kubeseers"
	kubeseerCRDName     = "kubeseers.kubeseer.io"
	unservedAPIVersion  = "kubeseer.io/v1beta1"
	apiRequestTimeout   = 30 * time.Second
	crdInstallMaxWait   = 20 * time.Second
	crdInstallPollDelay = 100 * time.Millisecond
)

var kubeseerResourceGVR = schema.GroupVersionResource{
	Group:    GroupVersion.Group,
	Version:  GroupVersion.Version,
	Resource: kubeseerResource,
}

func TestAPIContract(t *testing.T) {
	crdPath, err := filepath.Abs(filepath.Join("..", "..", "config", "crd", "bases"))
	if err != nil {
		t.Fatalf("resolve generated CRD path: %v", err)
	}

	environment, err := harness.New(t, harness.Options{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
		CRDMaxWait:            crdInstallMaxWait,
		CRDPollInterval:       crdInstallPollDelay,
	})
	if err != nil {
		t.Fatalf("create envtest harness: %v", err)
	}
	if _, err := environment.Start(); err != nil {
		t.Fatalf("start Kubernetes API server: %v", err)
	}

	clients, err := environment.Clients()
	if err != nil {
		t.Fatalf("create envtest clients: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()

	assertCRDEstablished(t, ctx, clients.APIExtensions)
	assertInstalledCRDContract(t, ctx, clients.APIExtensions)
	assertResourceRegistered(t, clients.Discovery)
	namespace := environment.Scope().Namespace
	resources := clients.Dynamic.Resource(kubeseerResourceGVR).Namespace(namespace)
	environment.AddCleanup("delete Kubeseer API contract fixtures", func(ctx context.Context) error {
		for _, name := range []string{"minimal", "valid-source", "negative-generation", "status-isolation", "typed-persistence"} {
			err := resources.Delete(ctx, name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
		return nil
	})

	assertTypedSchemeAndClient(t, ctx, resources, namespace)
	createMinimalResource(t, ctx, resources, namespace)
	createValidSourceResource(t, ctx, resources, namespace)
	assertMissingSpecRejected(t, ctx, resources, namespace)
	assertInvalidSourceRejected(t, ctx, resources, namespace)
	assertNonListSourcesRejected(t, ctx, resources, namespace)
	assertNegativeObservedGenerationRejected(t, ctx, resources, namespace)
	assertStatusUpdatePreservesSpec(t, ctx, resources, namespace)
	assertUnservedVersionRejected(t, ctx, clients.Dynamic, namespace)

	t.Logf("API contract passed with Kubernetes assets %s", environment.AssetsDirectory())
	t.Log("API_CONTRACT=kubeseer-v1alpha1 STATUS=passed")
}

func assertTypedSchemeAndClient(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	typeScheme := runtime.NewScheme()
	if err := AddToScheme(typeScheme); err != nil {
		t.Fatalf("register Kubeseer typed scheme: %v", err)
	}
	for _, object := range []runtime.Object{&Kubeseer{}, &KubeseerList{}} {
		gvks, _, err := typeScheme.ObjectKinds(object)
		if err != nil {
			t.Fatalf("resolve typed GVK for %T: %v", object, err)
		}
		if len(gvks) != 1 || gvks[0].GroupVersion() != GroupVersion {
			t.Fatalf("unexpected typed GVK for %T: %v", object, gvks)
		}
	}
	if object, err := typeScheme.New(GroupVersion.WithKind("Kubeseer")); err != nil || object == nil {
		t.Fatalf("construct Kubeseer from the registered scheme: object=%T err=%v", object, err)
	}

	original := &Kubeseer{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "Kubeseer"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "typed-persistence",
			Namespace: namespace,
			Labels:    map[string]string{"contract": "typed"},
		},
		Spec: KubeseerSpec{Sources: []KubeseerSource{{ID: "typed-source"}}},
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
		t.Fatalf("serialize typed Kubeseer: %v", err)
	}
	var decoded Kubeseer
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("deserialize typed Kubeseer: %v", err)
	}
	if decoded.TypeMeta != original.TypeMeta || decoded.Name != original.Name || decoded.Namespace != original.Namespace || !reflect.DeepEqual(original.Labels, decoded.Labels) || !reflect.DeepEqual(original.Spec, decoded.Spec) {
		t.Fatalf("typed JSON round-trip changed identity, metadata, or spec: original=%#v decoded=%#v", original, decoded)
	}
	if decoded.Status.ObservedGeneration != original.Status.ObservedGeneration || decoded.Status.Result == nil || len(decoded.Status.Conditions) != len(original.Status.Conditions) {
		t.Fatalf("typed JSON round-trip changed the status envelope: original=%#v decoded=%#v", original.Status, decoded.Status)
	}
	for index, condition := range original.Status.Conditions {
		decodedCondition := decoded.Status.Conditions[index]
		if decodedCondition.Type != condition.Type || decodedCondition.Status != condition.Status || decodedCondition.ObservedGeneration != condition.ObservedGeneration || decodedCondition.Reason != condition.Reason || decodedCondition.Message != condition.Message || !decodedCondition.LastTransitionTime.Time.Equal(condition.LastTransitionTime.Time) {
			t.Fatalf("typed JSON round-trip changed condition %d: original=%#v decoded=%#v", index, condition, decodedCondition)
		}
	}

	withoutSources := &Kubeseer{TypeMeta: original.TypeMeta, Spec: KubeseerSpec{}}
	withoutSourcesJSON, err := json.Marshal(withoutSources)
	if err != nil {
		t.Fatalf("serialize typed Kubeseer without sources: %v", err)
	}
	var serializedFields map[string]json.RawMessage
	if err := json.Unmarshal(withoutSourcesJSON, &serializedFields); err != nil {
		t.Fatalf("inspect typed empty spec: %v", err)
	}
	var specFields map[string]json.RawMessage
	if err := json.Unmarshal(serializedFields["spec"], &specFields); err != nil {
		t.Fatalf("decode typed empty spec: %v", err)
	}
	if _, found := specFields["sources"]; found {
		t.Fatalf("typed empty sources field was synthesized: %s", withoutSourcesJSON)
	}

	apiObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&Kubeseer{
		TypeMeta: original.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      original.Name,
			Namespace: original.Namespace,
			Labels:    original.Labels,
		},
		Spec: original.Spec,
	})
	if err != nil {
		t.Fatalf("convert typed Kubeseer for API persistence: %v", err)
	}
	apiObject["apiVersion"] = GroupVersion.String()
	apiObject["kind"] = "Kubeseer"
	created, err := resources.Create(ctx, &unstructured.Unstructured{Object: apiObject}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("persist typed Kubeseer through the API server: %v", err)
	}
	var persisted Kubeseer
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(created.Object, &persisted); err != nil {
		t.Fatalf("decode persisted Kubeseer into typed object: %v", err)
	}
	if persisted.Name != original.Name || persisted.Namespace != namespace || !reflect.DeepEqual(persisted.Spec, original.Spec) {
		t.Fatalf("typed API persistence changed identity or spec: original=%#v persisted=%#v", original, persisted)
	}

	persisted.Status = original.Status
	copy := persisted.DeepCopy()
	if copy == &persisted || copy.Status.Result == nil {
		t.Fatal("generated typed DeepCopy did not return an isolated result envelope")
	}
	copy.Labels["contract"] = "changed"
	copy.Spec.Sources[0].ID = "changed"
	copy.Status.Conditions[0].Reason = "Changed"
	if persisted.Labels["contract"] != "typed" || persisted.Spec.Sources[0].ID != "typed-source" || persisted.Status.Conditions[0].Reason != "Available" {
		t.Fatalf("generated typed DeepCopy aliases the API-derived object: %#v", persisted)
	}
}

func assertInstalledCRDContract(t *testing.T, ctx context.Context, client apiextensionsclient.Interface) {
	t.Helper()

	crd, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, kubeseerCRDName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get installed CRD contract: %v", err)
	}
	if crd.Spec.Group != GroupVersion.Group || crd.Spec.Scope != apiextensionsv1.NamespaceScoped {
		t.Fatalf("installed CRD identity or scope is incorrect: %#v", crd.Spec)
	}
	if crd.Spec.Names.Kind != "Kubeseer" || crd.Spec.Names.ListKind != "KubeseerList" || crd.Spec.Names.Plural != kubeseerResource || crd.Spec.Names.Singular != "kubeseer" {
		t.Fatalf("installed CRD names are incorrect: %#v", crd.Spec.Names)
	}
	if len(crd.Spec.Versions) != 1 {
		t.Fatalf("installed CRD has %d versions, expected one", len(crd.Spec.Versions))
	}
	version := crd.Spec.Versions[0]
	if version.Name != GroupVersion.Version || !version.Served || !version.Storage || version.Subresources == nil || version.Subresources.Status == nil {
		t.Fatalf("installed CRD version or status subresource is incorrect: %#v", version)
	}
	if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
		t.Fatal("installed CRD has no structural OpenAPI schema")
	}

	root := version.Schema.OpenAPIV3Schema
	if root.Type != "object" {
		t.Fatalf("installed CRD root schema is not an object: %q", root.Type)
	}
	for _, property := range []string{"apiVersion", "kind", "metadata", "spec", "status"} {
		if _, found := root.Properties[property]; !found {
			t.Fatalf("installed CRD schema does not expose %q", property)
		}
	}
	spec, found := root.Properties["spec"]
	if !found || spec.Type != "object" || len(spec.Properties) != 1 {
		t.Fatalf("installed CRD spec schema is incorrect: %#v", spec)
	}
	sources, found := spec.Properties["sources"]
	if !found || sources.Type != "array" || sources.Items == nil || sources.Items.Schema == nil || sources.XListType == nil || *sources.XListType != "map" || len(sources.XListMapKeys) != 1 || sources.XListMapKeys[0] != "id" {
		t.Fatalf("installed CRD sources schema is incorrect: %#v", sources)
	}
	source := sources.Items.Schema
	idSchema, found := source.Properties["id"]
	if !found || source.Type != "object" || !apiContractContains(source.Required, "id") || idSchema.Type != "string" || idSchema.MaxLength == nil || *idSchema.MaxLength != 63 || idSchema.Pattern != `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` {
		t.Fatalf("installed CRD source ID schema is incorrect: %#v", source)
	}
	status, found := root.Properties["status"]
	if !found || status.Type != "object" {
		t.Fatalf("installed CRD status schema is incorrect: %#v", status)
	}
	observedGeneration, found := status.Properties["observedGeneration"]
	if !found || observedGeneration.Type != "integer" || observedGeneration.Format != "int64" || observedGeneration.Minimum == nil || *observedGeneration.Minimum != 0 {
		t.Fatalf("installed CRD observedGeneration schema is incorrect: %#v", observedGeneration)
	}
	if result, found := status.Properties["result"]; !found || result.Type != "object" || len(result.Properties) != 0 || result.AdditionalProperties != nil {
		t.Fatalf("installed CRD result schema is incorrect: %#v", result)
	}
	apiContractAssertNoDefaults(t, root)
}

func apiContractContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func apiContractAssertNoDefaults(t *testing.T, schema *apiextensionsv1.JSONSchemaProps) {
	t.Helper()
	if schema.Default != nil {
		t.Fatalf("installed CRD schema contains an implicit default: %#v", schema.Default)
	}
	for _, property := range schema.Properties {
		property := property
		apiContractAssertNoDefaults(t, &property)
	}
	if schema.Items != nil && schema.Items.Schema != nil {
		apiContractAssertNoDefaults(t, schema.Items.Schema)
	}
}

func assertCRDEstablished(t *testing.T, ctx context.Context, client apiextensionsclient.Interface) {
	t.Helper()

	crd, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, kubeseerCRDName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get installed CRD: %v", err)
	}
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
			return
		}
	}
	t.Fatalf("CRD %s did not become Established: %#v", kubeseerCRDName, crd.Status.Conditions)
}

func assertResourceRegistered(t *testing.T, client discovery.DiscoveryInterface) {
	t.Helper()

	resources, err := client.ServerResourcesForGroupVersion(GroupVersion.String())
	if err != nil {
		t.Fatalf("discover %s: %v", GroupVersion.String(), err)
	}
	for _, resource := range resources.APIResources {
		if resource.Name == kubeseerResource {
			return
		}
	}
	t.Fatalf("%s is not registered in API discovery: %#v", kubeseerResource, resources.APIResources)
}

func createMinimalResource(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	created, err := resources.Create(ctx, newKubeseer("minimal", namespace, map[string]interface{}{}), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create minimal Kubeseer: %v", err)
	}
	if created.GetName() != "minimal" || created.GetNamespace() != namespace {
		t.Fatalf("minimal Kubeseer was not persisted as requested: %#v", created.Object)
	}
	if _, err := resources.Get(ctx, "minimal", metav1.GetOptions{}); err != nil {
		t.Fatalf("get persisted minimal Kubeseer: %v", err)
	}
}

func createValidSourceResource(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	created, err := resources.Create(ctx, newKubeseer("valid-source", namespace, map[string]interface{}{
		"sources": []interface{}{map[string]interface{}{"id": "source-one"}},
	}), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create Kubeseer with valid source: %v", err)
	}
	if _, found, err := unstructured.NestedSlice(created.Object, "spec", "sources"); err != nil || !found {
		t.Fatalf("persisted valid source was not returned: found=%t err=%v object=%#v", found, err, created.Object)
	}
}

func assertMissingSpecRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	assertInvalidCreate(t, ctx, resources, newKubeseer("missing-spec", namespace, nil), "missing spec")
	assertNotPersisted(t, ctx, resources, "missing-spec")
}

func assertInvalidSourceRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	assertInvalidCreate(t, ctx, resources, newKubeseer("invalid-source", namespace, map[string]interface{}{
		"sources": []interface{}{map[string]interface{}{"id": "Invalid_Source"}},
	}), "invalid source ID")
	assertNotPersisted(t, ctx, resources, "invalid-source")
}

func assertNonListSourcesRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	assertInvalidCreate(t, ctx, resources, newKubeseer("non-list-sources", namespace, map[string]interface{}{
		"sources": "not-a-list",
	}), "non-list sources")
	assertNotPersisted(t, ctx, resources, "non-list-sources")
}

func assertNegativeObservedGenerationRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	created, err := resources.Create(ctx, newKubeseer("negative-generation", namespace, map[string]interface{}{}), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create negative-generation fixture: %v", err)
	}
	statusUpdate := created.DeepCopy()
	if err := unstructured.SetNestedField(statusUpdate.Object, int64(-1), "status", "observedGeneration"); err != nil {
		t.Fatalf("set negative observedGeneration: %v", err)
	}
	_, err = resources.UpdateStatus(ctx, statusUpdate, metav1.UpdateOptions{})
	if !apierrors.IsInvalid(err) {
		t.Fatalf("negative observedGeneration was not rejected as invalid: %v", err)
	}
}

func assertStatusUpdatePreservesSpec(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	created, err := resources.Create(ctx, newKubeseer("status-isolation", namespace, map[string]interface{}{
		"sources": []interface{}{map[string]interface{}{"id": "stable-source"}},
	}), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create status-isolation fixture: %v", err)
	}
	expectedSpec, found, err := unstructured.NestedFieldCopy(created.Object, "spec")
	if err != nil || !found {
		t.Fatalf("read status-isolation spec: found=%t err=%v", found, err)
	}

	statusUpdate := created.DeepCopy()
	if err := unstructured.SetNestedField(statusUpdate.Object, int64(1), "status", "observedGeneration"); err != nil {
		t.Fatalf("set status observedGeneration: %v", err)
	}
	if _, err := resources.UpdateStatus(ctx, statusUpdate, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update status subresource: %v", err)
	}

	stored, err := resources.Get(ctx, "status-isolation", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get status-isolation after status update: %v", err)
	}
	actualSpec, found, err := unstructured.NestedFieldCopy(stored.Object, "spec")
	if err != nil || !found || !reflect.DeepEqual(expectedSpec, actualSpec) {
		t.Fatalf("status update changed spec: expected=%#v actual=%#v found=%t err=%v", expectedSpec, actualSpec, found, err)
	}
	observedGeneration, found, err := unstructured.NestedInt64(stored.Object, "status", "observedGeneration")
	if err != nil || !found || observedGeneration != 1 {
		t.Fatalf("status update was not persisted: generation=%d found=%t err=%v", observedGeneration, found, err)
	}
}

func assertUnservedVersionRejected(t *testing.T, ctx context.Context, client dynamic.Interface, namespace string) {
	t.Helper()

	unservedResources := client.Resource(schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  "v1beta1",
		Resource: kubeseerResource,
	}).Namespace(namespace)
	_, err := unservedResources.Create(ctx, &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": unservedAPIVersion,
		"kind":       "Kubeseer",
		"metadata":   map[string]interface{}{"name": "unserved-version"},
		"spec":       map[string]interface{}{},
	}}, metav1.CreateOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("unserved API version was not rejected with NotFound: %v", err)
	}
}

func assertInvalidCreate(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, object *unstructured.Unstructured, description string) {
	t.Helper()

	_, err := resources.Create(ctx, object, metav1.CreateOptions{})
	if !apierrors.IsInvalid(err) && !apierrors.IsBadRequest(err) {
		t.Fatalf("%s was not rejected as an invalid request: %v", description, err)
	}
}

func assertNotPersisted(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, name string) {
	t.Helper()

	_, err := resources.Get(ctx, name, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("invalid resource %q was persisted: %v", name, err)
	}
}

func newKubeseer(name, namespace string, spec map[string]interface{}) *unstructured.Unstructured {
	object := map[string]interface{}{
		"apiVersion": GroupVersion.String(),
		"kind":       "Kubeseer",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
	}
	if spec != nil {
		object["spec"] = spec
	}
	return &unstructured.Unstructured{Object: object}
}
