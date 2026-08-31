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

package envtest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/admission"
	discoveryruntime "github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	controllerenvtest "sigs.k8s.io/controller-runtime/pkg/envtest"
	crwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

const admissionValidationEnvtestTimeout = 90 * time.Second

func TestEnvtestAdmissionValidation(t *testing.T) {
	crdPath, err := filepath.Abs(filepath.Join("..", "..", "config", "crd", "bases"))
	if err != nil {
		t.Fatalf("resolve generated CRD path: %v", err)
	}
	harness, err := New(t, Options{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
		CRDMaxWait:            20 * time.Second,
		CRDPollInterval:       100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("create envtest harness: %v", err)
	}

	registration := admission.WebhookConfiguration(admissionregistrationv1.WebhookClientConfig{
		Service: &admissionregistrationv1.ServiceReference{Namespace: "kubeseer-test", Name: "admission-webhook"},
	})
	webhookOptions := controllerenvtest.WebhookInstallOptions{
		ValidatingWebhooks: []*admissionregistrationv1.ValidatingWebhookConfiguration{registration},
		MaxTime:            20 * time.Second,
		PollInterval:       100 * time.Millisecond,
	}
	if err := webhookOptions.PrepWithoutInstalling(); err != nil {
		t.Fatalf("prepare envtest webhook serving material: %v", err)
	}
	t.Cleanup(func() {
		if err := webhookOptions.Cleanup(); err != nil {
			t.Errorf("cleanup envtest webhook serving material: %v", err)
		}
	})

	config, err := harness.Start()
	if err != nil {
		t.Fatalf("start Kubernetes API server: %v", err)
	}
	clients, err := harness.Clients()
	if err != nil {
		t.Fatalf("create envtest clients: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), admissionValidationEnvtestTimeout)
	defer cancel()

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("register Kubeseer scheme: %v", err)
	}
	apiClient, err := crclient.New(config, crclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create controller-runtime client: %v", err)
	}

	var discoveryUnavailable atomic.Bool
	resolver := &envtestAdmissionResolver{
		delegate:    discoveryruntime.NewResolver(clients.Discovery),
		unavailable: &discoveryUnavailable,
	}
	var policyMode atomic.Int32
	policySource := accesspolicy.PolicySourceFunc(func(ctx context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
		switch policyMode.Load() {
		case admissionPolicyMissing:
			return nil, apierrors.NewNotFound(schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: "kubeseeraccesspolicies"}, v1alpha1.InstallationAccessCeilingName)
		case admissionPolicyInvalid:
			invalid := envtestAdmissionPolicy(harness.Scope().Namespace)
			invalid.Name = "default"
			return invalid, nil
		case admissionPolicyUnavailable:
			return nil, errors.New("envtest-policy-secret")
		default:
			current := new(v1alpha1.KubeseerAccessPolicy)
			if err := apiClient.Get(ctx, crclient.ObjectKey{Name: v1alpha1.InstallationAccessCeilingName}, current); err != nil {
				return nil, err
			}
			return current, nil
		}
	})
	validator := admission.NewValidator(resolver, policySource)
	webhookServer := crwebhook.NewServer(crwebhook.Options{
		Host:       webhookOptions.LocalServingHost,
		Port:       webhookOptions.LocalServingPort,
		CertDir:    webhookOptions.LocalServingCertDir,
		WebhookMux: nil,
	})
	admission.Register(webhookServer, scheme, validator)
	serverContext, stopServer := context.WithCancel(context.Background())
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- webhookServer.Start(serverContext) }()
	stopWebhookServer := func() {
		stopServer()
		select {
		case err := <-serverErrors:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("stop admission webhook server: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("admission webhook server did not stop")
		}
	}
	t.Cleanup(stopWebhookServer)
	if err := WaitFor(ctx, 10*time.Second, func(context.Context) (bool, error) {
		select {
		case err := <-serverErrors:
			if err != nil {
				return false, err
			}
			return false, errors.New("admission webhook server stopped before readiness")
		default:
		}
		if err := webhookServer.StartedChecker()(nil); err != nil {
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatalf("wait admission webhook readiness: %v", err)
	}
	if err := webhookOptions.Install(config); err != nil {
		t.Fatalf("install validating webhook configuration: %v", err)
	}
	harness.AddCleanup("delete admission validation webhook configuration", func(ctx context.Context) error {
		err := clients.AdmissionRegistration.ValidatingWebhookConfigurations().Delete(ctx, registration.Name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})

	kubeseerGVR := schema.GroupVersionResource{Group: v1alpha1.GroupVersion.Group, Version: v1alpha1.GroupVersion.Version, Resource: "kubeseers"}
	policy := envtestAdmissionPolicy(harness.Scope().Namespace)
	if err := apiClient.Create(ctx, policy); err != nil {
		t.Fatalf("create valid installation policy: %v", err)
	}
	harness.AddCleanup("delete admission validation Kubeseer fixtures", func(ctx context.Context) error {
		err := clients.Dynamic.Resource(kubeseerGVR).Namespace(harness.Scope().Namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	harness.AddCleanup("delete admission validation policy", func(ctx context.Context) error {
		err := apiClient.Delete(ctx, &v1alpha1.KubeseerAccessPolicy{ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.InstallationAccessCeilingName}})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})

	persisted := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-persisted", "pods", "v1", "Pod")
	if err := apiClient.Create(ctx, persisted); err != nil {
		t.Fatalf("create valid built-in Kubeseer: %v", err)
	}
	if err := apiClient.Get(ctx, crclient.ObjectKeyFromObject(persisted), new(v1alpha1.Kubeseer)); err != nil {
		t.Fatalf("read persisted valid Kubeseer: %v", err)
	}

	dryRun := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-dry-run", "pods", "v1", "Pod")
	if err := apiClient.Create(ctx, dryRun, crclient.DryRunAll); err != nil {
		t.Fatalf("dry-run valid Kubeseer was rejected: %v", err)
	}
	if err := apiClient.Get(ctx, crclient.ObjectKeyFromObject(dryRun), new(v1alpha1.Kubeseer)); !apierrors.IsNotFound(err) {
		t.Fatalf("dry-run Kubeseer persistence error = %v, want NotFound", err)
	}

	unknown := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-unknown", "unknown", "v1", "Ghost")
	if err := apiClient.Create(ctx, unknown); err == nil {
		t.Fatal("unknown discovered resource was persisted")
	} else {
		assertEnvtestAdmissionStatus(t, err, http.StatusUnprocessableEntity, metav1.StatusReasonInvalid, "spec.sources[0]")
	}
	if err := apiClient.Get(ctx, crclient.ObjectKeyFromObject(unknown), new(v1alpha1.Kubeseer)); !apierrors.IsNotFound(err) {
		t.Fatalf("unknown-resource persistence error = %v, want NotFound", err)
	}

	updateTarget := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-update", "update", "v1", "Pod")
	if err := apiClient.Create(ctx, updateTarget); err != nil {
		t.Fatalf("create update target: %v", err)
	}
	updateTarget.Spec.Sources[0].Resource = v1alpha1.ResourceReference{APIVersion: "bad/version/extra", Kind: "Pod"}
	if err := apiClient.Update(ctx, updateTarget); err == nil {
		t.Fatal("invalid Kubeseer update was persisted")
	} else {
		assertEnvtestAdmissionStatus(t, err, http.StatusUnprocessableEntity, metav1.StatusReasonInvalid, "spec.sources[0].resource.apiVersion")
	}
	currentUpdate := new(v1alpha1.Kubeseer)
	if err := apiClient.Get(ctx, crclient.ObjectKeyFromObject(updateTarget), currentUpdate); err != nil {
		t.Fatalf("read update target after rejection: %v", err)
	}
	if currentUpdate.Spec.Sources[0].Resource.APIVersion != "v1" {
		t.Fatalf("rejected update changed persisted API version to %q", currentUpdate.Spec.Sources[0].Resource.APIVersion)
	}

	structural := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": v1alpha1.GroupVersion.String(),
		"kind":       "Kubeseer",
		"metadata": map[string]interface{}{
			"name":      "admission-structural",
			"namespace": harness.Scope().Namespace,
		},
		"spec": map[string]interface{}{
			"sources": []interface{}{map[string]interface{}{
				"resource": map[string]interface{}{"apiVersion": "v1", "kind": "Pod"},
			}},
		},
	}}
	if _, err := clients.Dynamic.Resource(kubeseerGVR).Namespace(harness.Scope().Namespace).Create(ctx, structural, metav1.CreateOptions{}); err == nil {
		t.Fatal("structurally invalid Kubeseer was persisted")
	}
	if _, err := clients.Dynamic.Resource(kubeseerGVR).Namespace(harness.Scope().Namespace).Get(ctx, "admission-structural", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("structural-invalid persistence error = %v, want NotFound", err)
	}

	temporaryCRD := envtestAdmissionTemporaryCRD()
	if _, err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, temporaryCRD, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create temporary discovery CRD: %v", err)
	}
	harness.AddCleanup("delete admission validation temporary CRD", func(ctx context.Context) error {
		err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, temporaryCRD.Name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	if err := WaitFor(ctx, 20*time.Second, func(ctx context.Context) (bool, error) {
		current, err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, temporaryCRD.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait temporary discovery CRD establishment: %v", err)
	}

	policy.Spec.Namespaces.Exclude = []string{"blocked"}
	if err := apiClient.Update(ctx, policy); err != nil {
		t.Fatalf("update valid policy before dynamic matrix: %v", err)
	}
	gadget := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-gadget", "gadgets", "admission.example.test/v1", "Gadget")
	if err := apiClient.Create(ctx, gadget); err != nil {
		t.Fatalf("create Kubeseer for temporary discovered CR: %v", err)
	}

	exactNamespace := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-exact-namespace", "exact", "v1", "Pod")
	exactNamespace.Spec.Sources[0].Namespaces = &v1alpha1.NamespaceSelection{Names: []string{"blocked"}}
	if err := apiClient.Create(ctx, exactNamespace); err == nil {
		t.Fatal("policy-denied exact namespace was persisted")
	} else {
		assertEnvtestAdmissionStatus(t, err, http.StatusForbidden, metav1.StatusReasonForbidden, "spec.sources[0].namespaces.names[0]")
	}

	cluster := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-cluster", "nodes", "v1", "Node")
	if err := apiClient.Create(ctx, cluster, crclient.DryRunAll); err == nil {
		t.Fatal("cluster-scoped target was allowed while policy flag was false")
	} else {
		assertEnvtestAdmissionStatus(t, err, http.StatusForbidden, metav1.StatusReasonForbidden, "spec.sources[0].resource")
	}
	policy.Spec.AllowClusterScoped = true
	if err := apiClient.Update(ctx, policy); err != nil {
		t.Fatalf("valid policy update enabling cluster scope was rejected: %v", err)
	}
	if err := apiClient.Create(ctx, cluster, crclient.DryRunAll); err != nil {
		t.Fatalf("cluster-scoped target was rejected after policy allow: %v", err)
	}

	narrowed := policy.DeepCopy()
	narrowed.Spec.Resources = []v1alpha1.ResourceRule{
		{APIGroups: []string{""}, Kinds: []string{"Node"}},
		{APIGroups: []string{"admission.example.test"}, Kinds: []string{"Gadget"}},
	}
	if err := apiClient.Update(ctx, narrowed); err != nil {
		t.Fatalf("valid policy narrowing was rejected because of existing Kubeseers: %v", err)
	}
	if err := apiClient.Get(ctx, crclient.ObjectKeyFromObject(persisted), new(v1alpha1.Kubeseer)); err != nil {
		t.Fatalf("existing admitted Kubeseer disappeared after policy narrowing: %v", err)
	}
	narrowedPod := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-narrowed-pod", "narrowed", "v1", "Pod")
	if err := apiClient.Create(ctx, narrowedPod, crclient.DryRunAll); err == nil {
		t.Fatal("new Pod was allowed after policy narrowed away Pod")
	} else {
		assertEnvtestAdmissionStatus(t, err, http.StatusForbidden, metav1.StatusReasonForbidden, "spec.sources[0].resource")
	}

	invalidPolicyUpdate := narrowed.DeepCopy()
	invalidPolicyUpdate.Spec.Namespaces.Mode = "invalid"
	if err := apiClient.Update(ctx, invalidPolicyUpdate); err == nil {
		t.Fatal("invalid policy update was persisted")
	} else {
		assertEnvtestAdmissionStatus(t, err, http.StatusUnprocessableEntity, metav1.StatusReasonInvalid, "spec.namespaces.mode")
	}
	currentPolicy := new(v1alpha1.KubeseerAccessPolicy)
	if err := apiClient.Get(ctx, crclient.ObjectKey{Name: v1alpha1.InstallationAccessCeilingName}, currentPolicy); err != nil {
		t.Fatalf("read policy after invalid update: %v", err)
	}
	if currentPolicy.Spec.Namespaces.Mode == "invalid" {
		t.Fatal("invalid policy update changed persisted namespace mode")
	}

	if err := apiClient.Delete(ctx, currentPolicy); err != nil {
		t.Fatalf("delete canonical policy: %v", err)
	}
	policyMissingObject := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-policy-missing", "missing", "admission.example.test/v1", "Gadget")
	if err := apiClient.Create(ctx, policyMissingObject, crclient.DryRunAll); err == nil {
		t.Fatal("Kubeseer was allowed without the canonical policy")
	} else {
		assertEnvtestAdmissionStatus(t, err, http.StatusForbidden, metav1.StatusReasonForbidden, "spec")
	}
	policy = envtestAdmissionPolicy(harness.Scope().Namespace)
	policy.Spec.AllowClusterScoped = true
	policy.Spec.Resources = narrowed.Spec.Resources
	if err := apiClient.Create(ctx, policy); err != nil {
		t.Fatalf("recreate canonical policy after missing-policy scenario: %v", err)
	}

	policyMode.Store(admissionPolicyInvalid)
	policyInvalidObject := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-policy-invalid", "invalid-policy", "admission.example.test/v1", "Gadget")
	if err := apiClient.Create(ctx, policyInvalidObject, crclient.DryRunAll); err == nil {
		t.Fatal("Kubeseer was allowed with an invalid canonical policy")
	} else {
		assertEnvtestAdmissionStatus(t, err, http.StatusForbidden, metav1.StatusReasonForbidden, "spec")
	}
	policyMode.Store(admissionPolicyUnavailable)
	policyUnavailableObject := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-policy-unavailable", "unavailable-policy", "admission.example.test/v1", "Gadget")
	if err := apiClient.Create(ctx, policyUnavailableObject, crclient.DryRunAll); err == nil {
		t.Fatal("Kubeseer was allowed while policy loading was unavailable")
	} else {
		assertEnvtestAdmissionUnavailable(t, err, "envtest-policy-secret")
	}
	policyMode.Store(admissionPolicyNormal)

	discoveryUnavailable.Store(true)
	discoveryUnavailableObject := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-discovery-unavailable", "unavailable-discovery", "admission.example.test/v1", "Gadget")
	if err := apiClient.Create(ctx, discoveryUnavailableObject, crclient.DryRunAll); err == nil {
		t.Fatal("Kubeseer was allowed while discovery was unavailable")
	} else {
		assertEnvtestAdmissionUnavailable(t, err, "envtest-discovery-secret")
	}
	discoveryUnavailable.Store(false)

	registered, err := clients.AdmissionRegistration.ValidatingWebhookConfigurations().Get(ctx, registration.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read installed validating webhook configuration: %v", err)
	}
	originalRegistration := registered.DeepCopy()
	for index := range registered.Webhooks {
		badURL := fmt.Sprintf("https://127.0.0.1:1%s", []string{admission.KubeseerWebhookPath, admission.KubeseerAccessPolicyWebhookPath}[index])
		registered.Webhooks[index].ClientConfig.URL = &badURL
		registered.Webhooks[index].ClientConfig.Service = nil
	}
	if _, err := clients.AdmissionRegistration.ValidatingWebhookConfigurations().Update(ctx, registered, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("point validating webhook configuration at unavailable endpoint: %v", err)
	}
	endpointUnavailableObject := envtestAdmissionKubeseer(harness.Scope().Namespace, "admission-endpoint-unavailable", "endpoint-unavailable", "admission.example.test/v1", "Gadget")
	if err := apiClient.Create(ctx, endpointUnavailableObject, crclient.DryRunAll); err == nil {
		t.Fatal("covered write succeeded with unavailable webhook endpoint")
	} else if strings.Contains(err.Error(), "endpoint-unavailable") {
		t.Fatalf("endpoint failure echoed submitted object name: %v", err)
	}
	statusObject := new(v1alpha1.Kubeseer)
	if err := apiClient.Get(ctx, crclient.ObjectKeyFromObject(persisted), statusObject); err != nil {
		t.Fatalf("read object for unmatched status update: %v", err)
	}
	statusObject.Status.ObservedGeneration = statusObject.Generation
	if err := apiClient.Status().Update(ctx, statusObject); err != nil {
		t.Fatalf("status update was blocked while webhook endpoint was unavailable: %v", err)
	}
	if err := apiClient.Delete(ctx, statusObject); err != nil {
		t.Fatalf("DELETE was blocked while webhook endpoint was unavailable: %v", err)
	}

	restored, err := clients.AdmissionRegistration.ValidatingWebhookConfigurations().Get(ctx, registration.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read unavailable webhook configuration before restore: %v", err)
	}
	restored.Webhooks = originalRegistration.Webhooks
	if _, err := clients.AdmissionRegistration.ValidatingWebhookConfigurations().Update(ctx, restored, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("restore validating webhook configuration: %v", err)
	}

	t.Log("API_CONTRACT=admission-validation STATUS=passed")
}

const (
	admissionPolicyNormal int32 = iota
	admissionPolicyMissing
	admissionPolicyInvalid
	admissionPolicyUnavailable
)

type envtestAdmissionResolver struct {
	delegate    selection.DiscoveryResolver
	unavailable *atomic.Bool
}

func (r *envtestAdmissionResolver) Resolve(ctx context.Context, descriptor discoveryruntime.SourceDescriptor) (discoveryruntime.Resolution, error) {
	if r == nil || r.unavailable != nil && r.unavailable.Load() {
		return discoveryruntime.Resolution{}, discoveryruntime.NewResolutionError(descriptor.SourceID, discoveryruntime.ReasonDiscoveryUnavailable, "envtest-discovery-secret")
	}
	if r.delegate == nil {
		return discoveryruntime.Resolution{}, discoveryruntime.NewResolutionError(descriptor.SourceID, discoveryruntime.ReasonDiscoveryUnavailable, "discovery resolver is unavailable")
	}
	return r.delegate.Resolve(ctx, descriptor)
}

func envtestAdmissionPolicy(namespace string) *v1alpha1.KubeseerAccessPolicy {
	return &v1alpha1.KubeseerAccessPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "KubeseerAccessPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name: v1alpha1.InstallationAccessCeilingName,
		},
		Spec: v1alpha1.KubeseerAccessPolicySpec{
			Namespaces: v1alpha1.NamespacePolicy{
				Mode:             v1alpha1.NamespaceModeExplicit,
				Include:          []string{namespace},
				SystemNamespaces: []string{"kube-system", "kube-public", "kube-node-lease"},
			},
			Resources: []v1alpha1.ResourceRule{
				{APIGroups: []string{""}, Kinds: []string{"Pod", "Node"}},
				{APIGroups: []string{"admission.example.test"}, Kinds: []string{"Gadget"}},
			},
		},
	}
}

func envtestAdmissionKubeseer(namespace, name, sourceID, apiVersion, kind string) *v1alpha1.Kubeseer {
	return &v1alpha1.Kubeseer{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "Kubeseer"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{{
			ID:       sourceID,
			Resource: v1alpha1.ResourceReference{APIVersion: apiVersion, Kind: kind},
		}}},
	}
}

func envtestAdmissionTemporaryCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "gadgets.admission.example.test"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "admission.example.test",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     "gadgets",
				Singular:   "gadget",
				Kind:       "Gadget",
				ListKind:   "GadgetList",
				ShortNames: []string{"gad"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {Type: "object"},
					},
				}},
			}},
		},
	}
}

func assertEnvtestAdmissionStatus(t *testing.T, err error, wantCode int, wantReason metav1.StatusReason, wantCause string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected API error with code %d reason %q", wantCode, wantReason)
	}
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		t.Fatalf("error %T does not expose Kubernetes status: %v", err, err)
	}
	observed := status.Status()
	if observed.Code != int32(wantCode) || observed.Reason != wantReason {
		t.Fatalf("API status = %#v, want code=%d reason=%q", observed, wantCode, wantReason)
	}
	if wantCause == "" {
		return
	}
	if observed.Details == nil {
		t.Fatalf("API status has no causes: %#v", observed)
	}
	for _, cause := range observed.Details.Causes {
		if cause.Field == wantCause {
			return
		}
	}
	t.Fatalf("API status causes = %#v, want field %q", observed.Details.Causes, wantCause)
}

func assertEnvtestAdmissionUnavailable(t *testing.T, err error, secret string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected unavailable admission error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("unavailable admission error leaked upstream payload %q: %v", secret, err)
	}
	var status apierrors.APIStatus
	if errors.As(err, &status) && status.Status().Code == http.StatusServiceUnavailable {
		return
	}
	if !apierrors.IsInternalError(err) {
		t.Fatalf("unavailable admission error = %T %v, want service-unavailable or API internal webhook failure", err, err)
	}
}
