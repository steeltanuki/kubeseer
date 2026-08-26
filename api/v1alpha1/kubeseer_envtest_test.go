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

package v1alpha1_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	. "github.com/steeltanuki/kubeseer/api/v1alpha1"
	accesspolicy "github.com/steeltanuki/kubeseer/internal/accesspolicy"
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
	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kubeseerResource     = "kubeseers"
	kubeseerCRDName      = "kubeseers.kubeseer.io"
	accessPolicyResource = "kubeseeraccesspolicies"
	accessPolicyCRDName  = "kubeseeraccesspolicies.kubeseer.io"
	unservedAPIVersion   = "kubeseer.io/v1beta1"
	apiRequestTimeout    = 30 * time.Second
	crdInstallMaxWait    = 20 * time.Second
	crdInstallPollDelay  = 100 * time.Millisecond
)

var kubeseerResourceGVR = schema.GroupVersionResource{
	Group:    GroupVersion.Group,
	Version:  GroupVersion.Version,
	Resource: kubeseerResource,
}

var accessPolicyResourceGVR = schema.GroupVersionResource{
	Group:    GroupVersion.Group,
	Version:  GroupVersion.Version,
	Resource: accessPolicyResource,
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

	assertCRDEstablished(t, ctx, clients.APIExtensions, kubeseerCRDName)
	assertCRDEstablished(t, ctx, clients.APIExtensions, accessPolicyCRDName)
	assertInstalledCRDContract(t, ctx, clients.APIExtensions)
	assertInstalledAccessPolicyCRDContract(t, ctx, clients.APIExtensions)
	assertResourceRegistered(t, clients.Discovery, kubeseerResource, true)
	assertResourceRegistered(t, clients.Discovery, accessPolicyResource, false)
	namespace := environment.Scope().Namespace
	resources := clients.Dynamic.Resource(kubeseerResourceGVR).Namespace(namespace)
	accessPolicies := clients.Dynamic.Resource(accessPolicyResourceGVR)
	environment.AddCleanup("delete Kubeseer API contract fixtures", func(ctx context.Context) error {
		for _, name := range []string{"minimal", "valid-source", "negative-generation", "status-isolation", "typed-persistence", "duplicate-source-ids", "missing-resource", "invalid-namespace", "duplicate-namespaces"} {
			err := resources.Delete(ctx, name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
		return nil
	})
	environment.AddCleanup("delete KubeseerAccessPolicy API contract fixture", func(ctx context.Context) error {
		err := accessPolicies.Delete(ctx, InstallationAccessCeilingName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	})

	assertTypedSchemeAndClient(t, ctx, resources, namespace, environment.Config())
	createMinimalResource(t, ctx, resources, namespace)
	createValidSourceResource(t, ctx, resources, namespace)
	assertMissingSpecRejected(t, ctx, resources, namespace)
	assertInvalidSourceRejected(t, ctx, resources, namespace)
	assertNonListSourcesRejected(t, ctx, resources, namespace)
	assertSelectionSourceAdmissionRejected(t, ctx, resources, namespace)
	assertNegativeObservedGenerationRejected(t, ctx, resources, namespace)
	assertStatusUpdatePreservesSpec(t, ctx, resources, namespace)
	assertUnservedVersionRejected(t, ctx, clients.Dynamic, namespace)
	assertAccessPolicyDefaultingAndEmptyOverride(t, ctx, accessPolicies)
	assertAccessPolicyValidation(t, ctx, accessPolicies)
	assertAccessPolicyClientSource(t, ctx, environment.Config())

	t.Logf("API contract passed with Kubernetes assets %s", environment.AssetsDirectory())
	t.Log("API_CONTRACT=kubeseer-v1alpha1 STATUS=passed")
	t.Log("API_CONTRACT=resource-selection-types STATUS=passed")
	t.Log("API_CONTRACT=kubeseer-access-policy STATUS=passed")
	t.Log("API_CONTRACT=kubeseer-access-policy-admission STATUS=passed")
}

func assertTypedSchemeAndClient(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string, config *rest.Config) {
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
		Spec: KubeseerSpec{Sources: []KubeseerSource{{
			ID:         "typed-source",
			Resource:   ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces: &NamespaceSelection{Names: []string{"team-a"}},
			Selector: &ResourceSelector{
				Name:        "demo",
				MatchLabels: map[string]string{"app": "demo"},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "tier",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"backend"},
				}},
				FieldSelector: "metadata.namespace=team-a",
			},
		}}},
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
	if decoded.Spec.Sources[0].Resource != original.Spec.Sources[0].Resource || decoded.Spec.Sources[0].Namespaces == nil || !reflect.DeepEqual(decoded.Spec.Sources[0].Namespaces.Names, original.Spec.Sources[0].Namespaces.Names) || decoded.Spec.Sources[0].Selector == nil || !reflect.DeepEqual(decoded.Spec.Sources[0].Selector, original.Spec.Sources[0].Selector) {
		t.Fatalf("typed JSON round-trip changed source selection fields: original=%#v decoded=%#v", original.Spec.Sources[0], decoded.Spec.Sources[0])
	}

	emptyNamespaces := &Kubeseer{Spec: KubeseerSpec{Sources: []KubeseerSource{{ID: "empty-namespaces", Resource: ResourceReference{APIVersion: "v1", Kind: "Pod"}, Namespaces: &NamespaceSelection{Names: []string{}}}}}}
	emptyJSON, err := json.Marshal(emptyNamespaces)
	if err != nil {
		t.Fatalf("serialize explicit empty namespaces: %v", err)
	}
	var decodedEmpty Kubeseer
	if err := json.Unmarshal(emptyJSON, &decodedEmpty); err != nil {
		t.Fatalf("deserialize explicit empty namespaces: %v", err)
	}
	if decodedEmpty.Spec.Sources[0].Namespaces == nil || decodedEmpty.Spec.Sources[0].Namespaces.Names == nil || len(decodedEmpty.Spec.Sources[0].Namespaces.Names) != 0 {
		t.Fatalf("explicit empty namespaces lost pointer/list semantics: %#v", decodedEmpty.Spec.Sources[0])
	}
	omittedNamespaces := &Kubeseer{Spec: KubeseerSpec{Sources: []KubeseerSource{{ID: "omitted-namespaces", Resource: ResourceReference{APIVersion: "v1", Kind: "Pod"}}}}}
	omittedJSON, err := json.Marshal(omittedNamespaces)
	if err != nil {
		t.Fatalf("serialize omitted namespaces: %v", err)
	}
	var decodedOmitted Kubeseer
	if err := json.Unmarshal(omittedJSON, &decodedOmitted); err != nil {
		t.Fatalf("deserialize omitted namespaces: %v", err)
	}
	if decodedOmitted.Spec.Sources[0].Namespaces != nil {
		t.Fatalf("omitted namespaces synthesized a selection block: %#v", decodedOmitted.Spec.Sources[0])
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
	copy.Spec.Sources[0].Namespaces.Names[0] = "changed"
	copy.Spec.Sources[0].Selector.MatchLabels["app"] = "changed"
	copy.Spec.Sources[0].Selector.MatchExpressions[0].Values[0] = "changed"
	copy.Spec.Sources[0].Selector.FieldSelector = "changed"
	copy.Status.Conditions[0].Reason = "Changed"
	if persisted.Labels["contract"] != "typed" || persisted.Spec.Sources[0].ID != "typed-source" || persisted.Spec.Sources[0].Namespaces.Names[0] != "team-a" || persisted.Spec.Sources[0].Selector.MatchLabels["app"] != "demo" || persisted.Spec.Sources[0].Selector.MatchExpressions[0].Values[0] != "backend" || persisted.Spec.Sources[0].Selector.FieldSelector != "metadata.namespace=team-a" || persisted.Status.Conditions[0].Reason != "Available" {
		t.Fatalf("generated typed DeepCopy aliases the API-derived object: %#v", persisted)
	}

	assertAccessPolicyTypedContract(t, ctx, config)
}

func assertAccessPolicyTypedContract(t *testing.T, ctx context.Context, config *rest.Config) {
	t.Helper()
	if config == nil {
		t.Fatal("envtest returned a nil REST config for the typed policy contract")
	}

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("register KubeseerAccessPolicy scheme: %v", err)
	}
	for _, object := range []runtime.Object{&KubeseerAccessPolicy{}, &KubeseerAccessPolicyList{}} {
		gvks, _, err := scheme.ObjectKinds(object)
		if err != nil {
			t.Fatalf("resolve policy GVK for %T: %v", object, err)
		}
		if len(gvks) != 1 || gvks[0].GroupVersion() != GroupVersion {
			t.Fatalf("unexpected policy GVKs for %T: %v", object, gvks)
		}
	}
	for _, kind := range []string{"KubeseerAccessPolicy", "KubeseerAccessPolicyList"} {
		if object, err := scheme.New(GroupVersion.WithKind(kind)); err != nil || object == nil {
			t.Fatalf("construct %s from policy scheme: object=%T err=%v", kind, object, err)
		}
	}
	if InstallationAccessCeilingName != "installation-access-ceiling" {
		t.Fatalf("unexpected active policy name: %q", InstallationAccessCeilingName)
	}

	original := &KubeseerAccessPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "KubeseerAccessPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   InstallationAccessCeilingName,
			Labels: map[string]string{"contract": "typed-policy"},
		},
		Spec: KubeseerAccessPolicySpec{
			Namespaces: NamespacePolicy{
				Mode:             NamespaceModeAllNonSystem,
				Include:          []string{"observability"},
				Exclude:          []string{"restricted"},
				SystemNamespaces: []string{},
			},
			Resources: []ResourceRule{{
				APIGroups: []string{""},
				Kinds:     []string{"Pod", "Service"},
			}},
		},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("serialize typed access policy: %v", err)
	}
	var decoded KubeseerAccessPolicy
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("deserialize typed access policy: %v", err)
	}
	if !reflect.DeepEqual(*original, decoded) {
		t.Fatalf("typed policy JSON round-trip changed the contract: original=%#v decoded=%#v", *original, decoded)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("inspect typed access policy JSON: %v", err)
	}
	var specFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["spec"], &specFields); err != nil {
		t.Fatalf("inspect typed access policy spec JSON: %v", err)
	}
	var namespaceFields map[string]json.RawMessage
	if err := json.Unmarshal(specFields["namespaces"], &namespaceFields); err != nil {
		t.Fatalf("inspect typed namespace policy JSON: %v", err)
	}
	if string(namespaceFields["systemNamespaces"]) != "[]" {
		t.Fatalf("explicit empty systemNamespaces was not preserved as []: %s", namespaceFields["systemNamespaces"])
	}

	withoutOverride := &KubeseerAccessPolicy{Spec: KubeseerAccessPolicySpec{Namespaces: NamespacePolicy{Mode: NamespaceModeAllNonSystem}}}
	withoutOverrideJSON, err := json.Marshal(withoutOverride)
	if err != nil {
		t.Fatalf("serialize policy without system namespace override: %v", err)
	}
	var withoutOverrideFields map[string]json.RawMessage
	if err := json.Unmarshal(withoutOverrideJSON, &withoutOverrideFields); err != nil {
		t.Fatalf("inspect policy without system namespace override: %v", err)
	}
	var withoutOverrideSpec map[string]json.RawMessage
	if err := json.Unmarshal(withoutOverrideFields["spec"], &withoutOverrideSpec); err != nil {
		t.Fatalf("inspect spec without system namespace override: %v", err)
	}
	var withoutOverrideNamespaces map[string]json.RawMessage
	if err := json.Unmarshal(withoutOverrideSpec["namespaces"], &withoutOverrideNamespaces); err != nil {
		t.Fatalf("inspect namespaces without system namespace override: %v", err)
	}
	if string(withoutOverrideNamespaces["systemNamespaces"]) != "null" {
		t.Fatalf("omitted systemNamespaces was not preserved as null: %s", withoutOverrideNamespaces["systemNamespaces"])
	}

	typedClient, err := crclient.New(config, crclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create controller-runtime policy client: %v", err)
	}
	if err := typedClient.Create(ctx, original.DeepCopy()); err != nil {
		t.Fatalf("persist typed access policy through controller-runtime: %v", err)
	}
	var persisted KubeseerAccessPolicy
	if err := typedClient.Get(ctx, crclient.ObjectKey{Name: InstallationAccessCeilingName}, &persisted); err != nil {
		t.Fatalf("get typed access policy through controller-runtime: %v", err)
	}
	if persisted.Name != InstallationAccessCeilingName || !reflect.DeepEqual(persisted.Spec.Namespaces.Include, original.Spec.Namespaces.Include) || !reflect.DeepEqual(persisted.Spec.Namespaces.SystemNamespaces, original.Spec.Namespaces.SystemNamespaces) || !reflect.DeepEqual(persisted.Spec.Resources, original.Spec.Resources) {
		t.Fatalf("typed controller-runtime persistence changed the policy contract: original=%#v persisted=%#v", original, persisted)
	}

	copy := original.DeepCopy()
	if copy == original {
		t.Fatal("policy DeepCopy returned the original pointer")
	}
	copy.Labels["contract"] = "changed"
	copy.Spec.Namespaces.Include[0] = "changed"
	copy.Spec.Namespaces.Exclude[0] = "changed"
	copy.Spec.Resources[0].APIGroups[0] = "apps"
	copy.Spec.Resources[0].Kinds[0] = "Deployment"
	if original.Labels["contract"] != "typed-policy" || original.Spec.Namespaces.Include[0] != "observability" || original.Spec.Namespaces.Exclude[0] != "restricted" || original.Spec.Resources[0].APIGroups[0] != "" || original.Spec.Resources[0].Kinds[0] != "Pod" {
		t.Fatalf("policy DeepCopy aliases mutable fields: %#v", original)
	}
	if err := typedClient.Delete(ctx, &persisted); err != nil {
		t.Fatalf("delete typed access policy fixture: %v", err)
	}

	t.Log("API_CONTRACT=kubeseer-access-policy-types STATUS=passed")
}

func assertAccessPolicyClientSource(t *testing.T, ctx context.Context, config *rest.Config) {
	t.Helper()
	if config == nil {
		t.Fatal("envtest returned a nil REST config for the policy source adapter")
	}

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("register policy source scheme: %v", err)
	}
	typedClient, err := crclient.New(config, crclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create controller-runtime policy source client: %v", err)
	}
	policy := &KubeseerAccessPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "KubeseerAccessPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name: InstallationAccessCeilingName,
		},
		Spec: KubeseerAccessPolicySpec{
			Namespaces: NamespacePolicy{
				Mode:             NamespaceModeExplicit,
				Include:          []string{"team-a"},
				SystemNamespaces: []string{},
			},
			Resources: []ResourceRule{{
				APIGroups: []string{""},
				Kinds:     []string{"Pod"},
			}},
		},
	}
	if err := typedClient.Create(ctx, policy); err != nil {
		t.Fatalf("create policy source fixture: %v", err)
	}
	defer func() {
		if err := typedClient.Delete(ctx, policy); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete policy source fixture: %v", err)
		}
	}()

	source := accesspolicy.NewClientPolicySource(typedClient)
	loaded, err := source.Get(ctx)
	if err != nil {
		t.Fatalf("load active policy through ClientPolicySource: %v", err)
	}
	if loaded.Name != InstallationAccessCeilingName || loaded.Namespace != "" || !reflect.DeepEqual(loaded.Spec.Namespaces.Include, policy.Spec.Namespaces.Include) || !reflect.DeepEqual(loaded.Spec.Resources, policy.Spec.Resources) {
		t.Fatalf("ClientPolicySource returned the wrong active policy: expected=%#v actual=%#v", policy, loaded)
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
	if !found || source.Type != "object" || !apiContractContains(source.Required, "id") || !apiContractContains(source.Required, "resource") || idSchema.Type != "string" || idSchema.MaxLength == nil || *idSchema.MaxLength != 63 || idSchema.Pattern != `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` {
		t.Fatalf("installed CRD source ID schema is incorrect: %#v", source)
	}
	resource, found := source.Properties["resource"]
	if !found || resource.Type != "object" || !apiContractContains(resource.Required, "apiVersion") || !apiContractContains(resource.Required, "kind") {
		t.Fatalf("installed CRD resource reference schema is incorrect: %#v", resource)
	}
	for _, field := range []string{"apiVersion", "kind"} {
		value, found := resource.Properties[field]
		if !found || value.Type != "string" || value.MinLength == nil || *value.MinLength != 1 {
			t.Fatalf("installed CRD resource reference field %q is incorrect: %#v", field, value)
		}
	}
	namespaces, found := source.Properties["namespaces"]
	if !found || namespaces.Type != "object" || !apiContractContains(namespaces.Required, "names") {
		t.Fatalf("installed CRD namespace selection schema is incorrect: %#v", namespaces)
	}
	names, found := namespaces.Properties["names"]
	if !found || names.Type != "array" || names.XListType == nil || *names.XListType != "set" || names.Items == nil || names.Items.Schema == nil || names.Items.Schema.MaxLength == nil || *names.Items.Schema.MaxLength != 63 || names.Items.Schema.Pattern != `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` {
		t.Fatalf("installed CRD namespace names schema is incorrect: %#v", names)
	}
	selector, found := source.Properties["selector"]
	if !found || selector.Type != "object" {
		t.Fatalf("installed CRD selector schema is incorrect: %#v", selector)
	}
	for _, field := range []string{"name", "fieldSelector"} {
		value, found := selector.Properties[field]
		if !found || value.Type != "string" {
			t.Fatalf("installed CRD selector field %q is incorrect: %#v", field, value)
		}
	}
	if labels, found := selector.Properties["matchLabels"]; !found || labels.Type != "object" || labels.AdditionalProperties == nil || labels.AdditionalProperties.Schema == nil || labels.AdditionalProperties.Schema.Type != "string" {
		t.Fatalf("installed CRD matchLabels schema is incorrect: %#v", selector.Properties["matchLabels"])
	}
	if expressions, found := selector.Properties["matchExpressions"]; !found || expressions.Type != "array" || expressions.Items == nil || expressions.Items.Schema == nil || expressions.XListType == nil || *expressions.XListType != "atomic" {
		t.Fatalf("installed CRD matchExpressions schema is incorrect: %#v", selector.Properties["matchExpressions"])
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

func assertInstalledAccessPolicyCRDContract(t *testing.T, ctx context.Context, client apiextensionsclient.Interface) {
	t.Helper()

	crd, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, accessPolicyCRDName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get installed access policy CRD contract: %v", err)
	}
	if crd.Spec.Group != GroupVersion.Group || crd.Spec.Scope != apiextensionsv1.ClusterScoped {
		t.Fatalf("installed access policy CRD identity or scope is incorrect: %#v", crd.Spec)
	}
	if crd.Spec.Names.Kind != "KubeseerAccessPolicy" || crd.Spec.Names.ListKind != "KubeseerAccessPolicyList" || crd.Spec.Names.Plural != accessPolicyResource || crd.Spec.Names.Singular != "kubeseeraccesspolicy" {
		t.Fatalf("installed access policy CRD names are incorrect: %#v", crd.Spec.Names)
	}
	if len(crd.Spec.Versions) != 1 {
		t.Fatalf("installed access policy CRD has %d versions, expected one", len(crd.Spec.Versions))
	}
	version := crd.Spec.Versions[0]
	if version.Name != GroupVersion.Version || !version.Served || !version.Storage {
		t.Fatalf("installed access policy version contract is incorrect: %#v", version)
	}
	if version.Subresources != nil && version.Subresources.Status != nil {
		t.Fatal("installed access policy CRD unexpectedly exposes a status subresource")
	}
	if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
		t.Fatal("installed access policy CRD has no structural OpenAPI schema")
	}

	root := version.Schema.OpenAPIV3Schema
	if root.Type != "object" || !apiContractContains(root.Required, "spec") {
		t.Fatalf("installed access policy root schema is incorrect: %#v", root)
	}
	if len(root.XValidations) != 1 || root.XValidations[0].Rule != "self.metadata.name == 'installation-access-ceiling'" || root.XValidations[0].Message != "metadata.name must be installation-access-ceiling" {
		t.Fatalf("installed access policy singleton validation is incorrect: %#v", root.XValidations)
	}

	spec, found := root.Properties["spec"]
	if !found || spec.Type != "object" || !apiContractContains(spec.Required, "namespaces") || len(spec.Properties) != 3 {
		t.Fatalf("installed access policy spec schema is incorrect: %#v", spec)
	}
	namespaces, found := spec.Properties["namespaces"]
	if !found || namespaces.Type != "object" || !apiContractContains(namespaces.Required, "mode") {
		t.Fatalf("installed namespace policy schema is incorrect: %#v", namespaces)
	}
	mode, found := namespaces.Properties["mode"]
	if !found || mode.Type != "string" || !equalJSONValues(mode.Enum, []string{"Explicit", "All", "AllNonSystem"}) {
		t.Fatalf("installed namespace mode enum is incorrect: %#v", mode)
	}
	for _, field := range []string{"include", "exclude", "systemNamespaces"} {
		list, found := namespaces.Properties[field]
		if !found || list.Type != "array" || list.Items == nil || list.Items.Schema == nil || list.XListType == nil || *list.XListType != "set" {
			t.Fatalf("installed namespace list %q is not a set: %#v", field, list)
		}
		item := list.Items.Schema
		if item.Type != "string" || item.MaxLength == nil || *item.MaxLength != 63 || item.Pattern != `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` {
			t.Fatalf("installed namespace list %q item validation is incorrect: %#v", field, item)
		}
	}
	systemNamespaces := namespaces.Properties["systemNamespaces"]
	if systemNamespaces.Default == nil || string(systemNamespaces.Default.Raw) != `["kube-system","kube-public","kube-node-lease"]` {
		t.Fatalf("installed system namespace default is incorrect: %#v", systemNamespaces.Default)
	}

	resources, found := spec.Properties["resources"]
	if !found || resources.Type != "array" || resources.Items == nil || resources.Items.Schema == nil || resources.XListType == nil || *resources.XListType != "atomic" {
		t.Fatalf("installed resource rules are not atomic: %#v", resources)
	}
	resourceRule := resources.Items.Schema
	if resourceRule.Type != "object" || !apiContractContains(resourceRule.Required, "apiGroups") || !apiContractContains(resourceRule.Required, "kinds") {
		t.Fatalf("installed resource rule required fields are incorrect: %#v", resourceRule)
	}
	apiGroups := resourceRule.Properties["apiGroups"]
	if apiGroups.Type != "array" || apiGroups.MinItems == nil || *apiGroups.MinItems != 1 || apiGroups.XListType == nil || *apiGroups.XListType != "set" || apiGroups.Items == nil || apiGroups.Items.Schema == nil || apiGroups.Items.Schema.MaxLength == nil || *apiGroups.Items.Schema.MaxLength != 253 || apiGroups.Items.Schema.Pattern != `^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$` {
		t.Fatalf("installed API group validation contract is incorrect: %#v", apiGroups)
	}
	kinds := resourceRule.Properties["kinds"]
	if kinds.Type != "array" || kinds.MinItems == nil || *kinds.MinItems != 1 || kinds.XListType == nil || *kinds.XListType != "set" || kinds.Items == nil || kinds.Items.Schema == nil || kinds.Items.Schema.MaxLength == nil || *kinds.Items.Schema.MaxLength != 63 || kinds.Items.Schema.Pattern != `^[A-Z][A-Za-z0-9]*$` {
		t.Fatalf("installed Kind validation contract is incorrect: %#v", kinds)
	}

	allowClusterScoped, found := spec.Properties["allowClusterScoped"]
	if !found || allowClusterScoped.Type != "boolean" || allowClusterScoped.Default == nil || string(allowClusterScoped.Default.Raw) != "false" {
		t.Fatalf("installed cluster-scope default is incorrect: %#v", allowClusterScoped)
	}
	if root.XPreserveUnknownFields != nil || spec.XPreserveUnknownFields != nil || namespaces.XPreserveUnknownFields != nil || resourceRule.XPreserveUnknownFields != nil {
		t.Fatal("installed access policy schema permits preserved unknown fields")
	}
}

func apiContractContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
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

func assertCRDEstablished(t *testing.T, ctx context.Context, client apiextensionsclient.Interface, crdName string) {
	t.Helper()

	crd, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crdName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get installed CRD: %v", err)
	}
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
			return
		}
	}
	t.Fatalf("CRD %s did not become Established: %#v", crdName, crd.Status.Conditions)
}

func assertResourceRegistered(t *testing.T, client discovery.DiscoveryInterface, resourceName string, namespaced bool) {
	t.Helper()

	resources, err := client.ServerResourcesForGroupVersion(GroupVersion.String())
	if err != nil {
		t.Fatalf("discover %s: %v", GroupVersion.String(), err)
	}
	for _, resource := range resources.APIResources {
		if resource.Name == resourceName {
			if resource.Namespaced != namespaced {
				t.Fatalf("%s discovery scope = %t, want %t", resourceName, resource.Namespaced, namespaced)
			}
			return
		}
	}
	t.Fatalf("%s is not registered in API discovery: %#v", resourceName, resources.APIResources)
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
		"sources": []interface{}{map[string]interface{}{
			"id":       "source-one",
			"resource": map[string]interface{}{"apiVersion": "v1", "kind": "Pod"},
		}},
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

func assertSelectionSourceAdmissionRejected(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface, namespace string) {
	t.Helper()

	validSource := func(id string) map[string]interface{} {
		return map[string]interface{}{
			"id": id,
			"resource": map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
			},
		}
	}
	tests := []struct {
		name   string
		object *unstructured.Unstructured
	}{
		{
			name: "duplicate source IDs",
			object: newKubeseer("duplicate-source-ids", namespace, map[string]interface{}{
				"sources": []interface{}{validSource("duplicate"), validSource("duplicate")},
			}),
		},
		{
			name: "missing resource coordinates",
			object: newKubeseer("missing-resource", namespace, map[string]interface{}{
				"sources": []interface{}{map[string]interface{}{"id": "missing-resource"}},
			}),
		},
		{
			name: "invalid namespace name",
			object: newKubeseer("invalid-namespace", namespace, map[string]interface{}{
				"sources": []interface{}{func() map[string]interface{} {
					source := validSource("invalid-namespace")
					source["namespaces"] = map[string]interface{}{"names": []interface{}{"Invalid_Namespace"}}
					return source
				}()},
			}),
		},
		{
			name: "duplicate namespace names",
			object: newKubeseer("duplicate-namespaces", namespace, map[string]interface{}{
				"sources": []interface{}{func() map[string]interface{} {
					source := validSource("duplicate-namespaces")
					source["namespaces"] = map[string]interface{}{"names": []interface{}{"team-a", "team-a"}}
					return source
				}()},
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertInvalidCreate(t, ctx, resources, test.object, test.name)
			assertNotPersisted(t, ctx, resources, test.object.GetName())
		})
	}
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
		"sources": []interface{}{map[string]interface{}{
			"id":       "stable-source",
			"resource": map[string]interface{}{"apiVersion": "v1", "kind": "Pod"},
		}},
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

func assertAccessPolicyDefaultingAndEmptyOverride(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	created, err := resources.Create(ctx, newAccessPolicy(InstallationAccessCeilingName, validAccessPolicySpec()), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create valid access policy: %v", err)
	}
	if created.GetName() != InstallationAccessCeilingName || created.GetNamespace() != "" {
		t.Fatalf("access policy was not persisted cluster-scoped: %#v", created.Object)
	}

	systemNamespaces, found, err := unstructured.NestedSlice(created.Object, "spec", "namespaces", "systemNamespaces")
	if err != nil || !found || !reflect.DeepEqual(systemNamespaces, []interface{}{"kube-system", "kube-public", "kube-node-lease"}) {
		t.Fatalf("omitted systemNamespaces did not receive the API default: value=%#v found=%t err=%v", systemNamespaces, found, err)
	}
	allowClusterScoped, found, err := unstructured.NestedBool(created.Object, "spec", "allowClusterScoped")
	if err != nil || !found || allowClusterScoped {
		t.Fatalf("omitted allowClusterScoped did not default to false: value=%t found=%t err=%v", allowClusterScoped, found, err)
	}

	current, err := resources.Get(ctx, InstallationAccessCeilingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get persisted access policy: %v", err)
	}
	if err := unstructured.SetNestedSlice(current.Object, []interface{}{}, "spec", "namespaces", "systemNamespaces"); err != nil {
		t.Fatalf("set explicit empty systemNamespaces: %v", err)
	}
	updated, err := resources.Update(ctx, current, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("update access policy with explicit empty systemNamespaces: %v", err)
	}
	systemNamespaces, found, err = unstructured.NestedSlice(updated.Object, "spec", "namespaces", "systemNamespaces")
	if err != nil || !found || !reflect.DeepEqual(systemNamespaces, []interface{}{}) {
		t.Fatalf("explicit empty systemNamespaces was not preserved: value=%#v found=%t err=%v", systemNamespaces, found, err)
	}

	if err := unstructured.SetNestedField(updated.Object, "Explicit", "spec", "namespaces", "mode"); err != nil {
		t.Fatalf("set empty deny-all namespace mode: %v", err)
	}
	for _, field := range []string{"include", "exclude", "systemNamespaces"} {
		if err := unstructured.SetNestedSlice(updated.Object, []interface{}{}, "spec", "namespaces", field); err != nil {
			t.Fatalf("set empty namespace list %q: %v", field, err)
		}
	}
	if err := unstructured.SetNestedSlice(updated.Object, []interface{}{}, "spec", "resources"); err != nil {
		t.Fatalf("set empty resource allowlist: %v", err)
	}
	updated, err = resources.Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("update access policy with empty deny-all boundaries: %v", err)
	}
	for _, field := range []string{"include", "exclude", "systemNamespaces"} {
		values, found, err := unstructured.NestedSlice(updated.Object, "spec", "namespaces", field)
		if err != nil || !found || !reflect.DeepEqual(values, []interface{}{}) {
			t.Fatalf("empty deny-all namespace list %q was not preserved: value=%#v found=%t err=%v", field, values, found, err)
		}
	}
	resourcesList, found, err := unstructured.NestedSlice(updated.Object, "spec", "resources")
	if err != nil || !found || !reflect.DeepEqual(resourcesList, []interface{}{}) {
		t.Fatalf("empty deny-all resource list was not preserved: value=%#v found=%t err=%v", resourcesList, found, err)
	}

	if err := resources.Delete(ctx, InstallationAccessCeilingName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete access policy before invalid cases: %v", err)
	}
}

func assertAccessPolicyValidation(t *testing.T, ctx context.Context, resources dynamic.ResourceInterface) {
	t.Helper()

	tests := []struct {
		name   string
		object *unstructured.Unstructured
	}{
		{
			name:   "non-singleton name",
			object: newAccessPolicy("default", validAccessPolicySpec()),
		},
		{
			name: "invalid mode",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["namespaces"].(map[string]interface{})["mode"] = "Unknown"
				return spec
			}()),
		},
		{
			name: "invalid namespace",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["namespaces"].(map[string]interface{})["include"] = []interface{}{"Invalid_Namespace"}
				return spec
			}()),
		},
		{
			name: "duplicate namespace set value",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["namespaces"].(map[string]interface{})["include"] = []interface{}{"team-a", "team-a"}
				return spec
			}()),
		},
		{
			name: "empty API group rule member",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["resources"] = []interface{}{map[string]interface{}{
					"apiGroups": []interface{}{},
					"kinds":     []interface{}{"Pod"},
				}}
				return spec
			}()),
		},
		{
			name: "empty Kind rule member",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["resources"] = []interface{}{map[string]interface{}{
					"apiGroups": []interface{}{[]interface{}{}},
					"kinds":     []interface{}{},
				}}
				return spec
			}()),
		},
		{
			name: "duplicate resource set value",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["resources"] = []interface{}{map[string]interface{}{
					"apiGroups": []interface{}{"apps", "apps"},
					"kinds":     []interface{}{"Deployment"},
				}}
				return spec
			}()),
		},
		{
			name: "malformed API group",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["resources"].([]interface{})[0].(map[string]interface{})["apiGroups"] = []interface{}{"Apps"}
				return spec
			}()),
		},
		{
			name: "wildcard API group",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["resources"].([]interface{})[0].(map[string]interface{})["apiGroups"] = []interface{}{"*"}
				return spec
			}()),
		},
		{
			name: "malformed Kind",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["resources"].([]interface{})[0].(map[string]interface{})["kinds"] = []interface{}{"deployment"}
				return spec
			}()),
		},
		{
			name: "wildcard Kind",
			object: newAccessPolicy(InstallationAccessCeilingName, func() map[string]interface{} {
				spec := validAccessPolicySpec()
				spec["resources"].([]interface{})[0].(map[string]interface{})["kinds"] = []interface{}{"*"}
				return spec
			}()),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertInvalidCreate(t, ctx, resources, test.object, test.name)
			assertNotPersisted(t, ctx, resources, test.object.GetName())
		})
	}
}

func validAccessPolicySpec() map[string]interface{} {
	return map[string]interface{}{
		"namespaces": map[string]interface{}{
			"mode":    "AllNonSystem",
			"include": []interface{}{"team-a"},
			"exclude": []interface{}{"team-b"},
		},
		"resources": []interface{}{map[string]interface{}{
			"apiGroups": []interface{}{""},
			"kinds":     []interface{}{"Pod"},
		}},
	}
}

func newAccessPolicy(name string, spec map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": GroupVersion.String(),
		"kind":       "KubeseerAccessPolicy",
		"metadata": map[string]interface{}{
			"name": name,
		},
		"spec": spec,
	}}
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
