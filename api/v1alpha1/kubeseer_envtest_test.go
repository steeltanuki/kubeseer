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
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	testNamespace       = "default"
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
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Fatal("KUBEBUILDER_ASSETS is required; run this suite through make test-api")
	}

	crdPath, err := filepath.Abs(filepath.Join("..", "..", "config", "crd", "bases"))
	if err != nil {
		t.Fatalf("resolve generated CRD path: %v", err)
	}

	environment := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
		CRDInstallOptions: envtest.CRDInstallOptions{
			CleanUpAfterUse: true,
			MaxTime:         crdInstallMaxWait,
			PollInterval:    crdInstallPollDelay,
		},
	}

	config, err := environment.Start()
	if err != nil {
		t.Fatalf("start Kubernetes API server with assets %s: %v", assets, err)
	}
	defer func() {
		if err := environment.Stop(); err != nil {
			t.Errorf("stop Kubernetes API server: %v", err)
		}
	}()

	apiExtensions, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		t.Fatalf("create API extensions client: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		t.Fatalf("create discovery client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiRequestTimeout)
	defer cancel()

	assertCRDEstablished(t, ctx, apiExtensions)
	assertResourceRegistered(t, discoveryClient)
	resources := dynamicClient.Resource(kubeseerResourceGVR).Namespace(testNamespace)

	createMinimalResource(t, ctx, resources)
	createValidSourceResource(t, ctx, resources)
	assertMissingSpecRejected(t, ctx, resources)
	assertInvalidSourceRejected(t, ctx, resources)
	assertNonListSourcesRejected(t, ctx, resources)
	assertNegativeObservedGenerationRejected(t, ctx, resources)
	assertStatusUpdatePreservesSpec(t, ctx, resources)
	assertUnservedVersionRejected(t, ctx, dynamicClient)

	t.Logf("API contract passed with Kubernetes assets %s", assets)
}

func assertCRDEstablished(t *testing.T, ctx context.Context, client *apiextensionsclient.Clientset) {
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

func assertResourceRegistered(t *testing.T, client *discovery.DiscoveryClient) {
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

func createMinimalResource(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	created, err := resources.Create(ctx, newKubeseer("minimal", map[string]interface{}{}), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create minimal Kubeseer: %v", err)
	}
	if created.GetName() != "minimal" || created.GetNamespace() != testNamespace {
		t.Fatalf("minimal Kubeseer was not persisted as requested: %#v", created.Object)
	}
	if _, err := resources.Get(ctx, "minimal", metav1.GetOptions{}); err != nil {
		t.Fatalf("get persisted minimal Kubeseer: %v", err)
	}
}

func createValidSourceResource(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	created, err := resources.Create(ctx, newKubeseer("valid-source", map[string]interface{}{
		"sources": []interface{}{map[string]interface{}{"id": "source-one"}},
	}), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create Kubeseer with valid source: %v", err)
	}
	if _, found, err := unstructured.NestedSlice(created.Object, "spec", "sources"); err != nil || !found {
		t.Fatalf("persisted valid source was not returned: found=%t err=%v object=%#v", found, err, created.Object)
	}
}

func assertMissingSpecRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	assertInvalidCreate(t, ctx, resources, newKubeseer("missing-spec", nil), "missing spec")
	assertNotPersisted(t, ctx, resources, "missing-spec")
}

func assertInvalidSourceRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	assertInvalidCreate(t, ctx, resources, newKubeseer("invalid-source", map[string]interface{}{
		"sources": []interface{}{map[string]interface{}{"id": "Invalid_Source"}},
	}), "invalid source ID")
	assertNotPersisted(t, ctx, resources, "invalid-source")
}

func assertNonListSourcesRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	assertInvalidCreate(t, ctx, resources, newKubeseer("non-list-sources", map[string]interface{}{
		"sources": "not-a-list",
	}), "non-list sources")
	assertNotPersisted(t, ctx, resources, "non-list-sources")
}

func assertNegativeObservedGenerationRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	created, err := resources.Create(ctx, newKubeseer("negative-generation", map[string]interface{}{}), metav1.CreateOptions{})
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

func assertStatusUpdatePreservesSpec(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	created, err := resources.Create(ctx, newKubeseer("status-isolation", map[string]interface{}{
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

func assertUnservedVersionRejected(t *testing.T, ctx context.Context, client dynamic.Interface) {
	t.Helper()

	unservedResources := client.Resource(schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  "v1beta1",
		Resource: kubeseerResource,
	}).Namespace(testNamespace)
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

func newKubeseer(name string, spec map[string]interface{}) *unstructured.Unstructured {
	object := map[string]interface{}{
		"apiVersion": GroupVersion.String(),
		"kind":       "Kubeseer",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": testNamespace,
		},
	}
	if spec != nil {
		object["spec"] = spec
	}
	return &unstructured.Unstructured{Object: object}
}
