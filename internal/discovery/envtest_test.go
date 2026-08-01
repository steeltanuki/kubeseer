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

package discovery

import (
	"context"
	"os"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	envtestRequestTimeout = 30 * time.Second
	envtestPollInterval   = 100 * time.Millisecond
	envtestCRDTimeout     = 20 * time.Second
)

func TestEnvtestDiscovery(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Fatal("KUBEBUILDER_ASSETS is required; run this test through the Walden envtest proof")
	}

	environment := &envtest.Environment{}
	config, err := environment.Start()
	if err != nil {
		t.Fatalf("start Kubernetes API server with assets %s: %v", assets, err)
	}
	defer func() {
		if err := environment.Stop(); err != nil {
			t.Errorf("stop Kubernetes API server: %v", err)
		}
	}()

	config.Timeout = envtestRequestTimeout
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		t.Fatalf("create discovery client: %v", err)
	}
	extensionsClient, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		t.Fatalf("create API extensions client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), envtestCRDTimeout)
	defer cancel()
	resolver := NewResolver(discoveryClient)

	namespaced, err := resolver.Resolve(ctx, SourceDescriptor{
		SourceID:   "builtin-pod",
		APIVersion: "v1",
		Kind:       "Pod",
	})
	if err != nil {
		t.Fatalf("resolve built-in namespaced resource: %v", err)
	}
	if namespaced.Resource != (schema.GroupVersionResource{Version: "v1", Resource: "pods"}) || namespaced.Scope != ScopeNamespaced {
		t.Fatalf("unexpected built-in namespaced resolution: %#v", namespaced)
	}

	clusterScoped, err := resolver.Resolve(ctx, SourceDescriptor{
		SourceID:   "builtin-node",
		APIVersion: "v1",
		Kind:       "Node",
	})
	if err != nil {
		t.Fatalf("resolve built-in cluster resource: %v", err)
	}
	if clusterScoped.Resource != (schema.GroupVersionResource{Version: "v1", Resource: "nodes"}) || clusterScoped.Scope != ScopeCluster {
		t.Fatalf("unexpected built-in cluster resolution: %#v", clusterScoped)
	}

	crd := discoveryTestCRD()
	if _, err := extensionsClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("install discovery test CRD: %v", err)
	}
	defer func() {
		deleteCtx, deleteCancel := context.WithTimeout(context.Background(), envtestRequestTimeout)
		defer deleteCancel()
		_ = extensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(deleteCtx, crd.Name, metav1.DeleteOptions{})
	}()
	waitForCRDEstablished(t, ctx, extensionsClient, crd.Name)

	customResource, err := resolver.Resolve(ctx, SourceDescriptor{
		SourceID:   "custom-widget",
		APIVersion: "discovery.kubeseer.io/v1",
		Kind:       "Widget",
	})
	if err != nil {
		t.Fatalf("resolve CRD-backed resource: %v", err)
	}
	if customResource.Resource != (schema.GroupVersionResource{Group: "discovery.kubeseer.io", Version: "v1", Resource: "widgets"}) || customResource.Scope != ScopeNamespaced {
		t.Fatalf("unexpected CRD-backed resolution: %#v", customResource)
	}

	t.Logf("discovery smoke test passed with Kubernetes assets %s", assets)
}

func discoveryTestCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.discovery.kubeseer.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "discovery.kubeseer.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     "widgets",
				Singular:   "widget",
				Kind:       "Widget",
				ListKind:   "WidgetList",
				ShortNames: []string{"wdg"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				},
			}},
		},
	}
}

func waitForCRDEstablished(t *testing.T, ctx context.Context, client *apiextensionsclient.Clientset, name string) {
	t.Helper()
	err := wait.PollUntilContextTimeout(ctx, envtestPollInterval, envtestCRDTimeout, true, func(ctx context.Context) (bool, error) {
		crd, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextensionsv1.Established {
				return condition.Status == apiextensionsv1.ConditionTrue, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("wait for discovery test CRD %s to become Established: %v", name, err)
	}
}
