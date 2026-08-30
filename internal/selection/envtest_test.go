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

package selection_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	harness "github.com/steeltanuki/kubeseer/test/envtest"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const selectionRequestTimeout = 30 * time.Second

func TestEnvtestSelection(t *testing.T) {
	environment, err := harness.New(t, harness.Options{})
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
	ctx, cancel := context.WithTimeout(context.Background(), selectionRequestTimeout)
	defer cancel()

	ownerNamespace := environment.Scope().Namespace
	otherNamespace := environment.Scope().Prefix + "-other"
	if _, err := clients.Core.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: otherNamespace, Labels: map[string]string{"kubeseer.io/test-scope": environment.Scope().Prefix}}}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create cross-namespace fixture: %v", err)
	}
	environment.AddCleanup("delete selection fixture namespace", func(ctx context.Context) error {
		err := clients.Core.CoreV1().Namespaces().Delete(ctx, otherNamespace, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})

	createConfigMaps(t, ctx, clients, ownerNamespace, otherNamespace)
	createNode(t, ctx, clients, environment.Scope().Prefix)
	crd, widgetGVR := createWidgetCRD(t, ctx, environment, clients)
	createWidgets(t, ctx, clients.Dynamic, widgetGVR, ownerNamespace)
	environment.AddCleanup("delete selection fixture resources", func(ctx context.Context) error {
		if err := clients.Dynamic.Resource(widgetGVR).Namespace(ownerNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if err := clients.Core.CoreV1().Nodes().Delete(ctx, "selection-node-"+environment.Scope().Prefix, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	})
	environment.AddCleanup("delete selection fixture CRD", func(ctx context.Context) error {
		err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crd.Name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})

	resolver := discovery.NewResolver(clients.Discovery)
	verifier := authorization.VerifierFunc(func(subject authorization.Subject) bool {
		return subject.Validate() == nil
	})
	lister := selection.NewDynamicResourceLister(clients.Dynamic, verifier)
	executor := selection.NewExecutor(lister, selection.WithVerifier(verifier))

	t.Run("exact name and labels use server-side AND selection", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID:       "exact-label-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "ConfigMap"},
			Selector: &v1alpha1.ResourceSelector{
				Name:        "alpha",
				MatchLabels: map[string]string{"app": "demo"},
			},
		}
		outcome := executeSource(t, ctx, resolver, executor, ownerNamespace, source)
		if outcome.Err != nil || len(outcome.Resources) != 1 || outcome.Resources[0].Provenance.Name != "alpha" {
			t.Fatalf("exact and label selection = %#v", outcome)
		}
		if outcome.Resources[0].Provenance.APIVersion != "v1" || outcome.Resources[0].Provenance.Kind != "ConfigMap" || outcome.Resources[0].Provenance.Namespace != ownerNamespace || outcome.Resources[0].Provenance.UID == "" {
			t.Fatalf("selection provenance = %#v", outcome.Resources[0].Provenance)
		}
	})

	t.Run("match expressions and supported field selector", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID:       "expression-field-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "ConfigMap"},
			Selector: &v1alpha1.ResourceSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "tier", Operator: metav1.LabelSelectorOpIn, Values: []string{"backend"}}},
				FieldSelector:    "metadata.name=beta",
			},
		}
		outcome := executeSource(t, ctx, resolver, executor, ownerNamespace, source)
		if outcome.Err != nil || len(outcome.Resources) != 1 || outcome.Resources[0].Provenance.Name != "beta" {
			t.Fatalf("expression and field selection = %#v", outcome)
		}
	})

	t.Run("match all and no match are successful outcomes", func(t *testing.T) {
		all := v1alpha1.KubeseerSource{ID: "all-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "ConfigMap"}}
		allOutcome := executeSource(t, ctx, resolver, executor, ownerNamespace, all)
		if allOutcome.Err != nil || len(allOutcome.Resources) != 3 {
			t.Fatalf("match-all outcome = %#v", allOutcome)
		}
		noMatch := all
		noMatch.ID = "no-match-source"
		noMatch.Selector = &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"app": "missing"}}
		noMatchOutcome := executeSource(t, ctx, resolver, executor, ownerNamespace, noMatch)
		if noMatchOutcome.Err != nil || len(noMatchOutcome.Resources) != 0 {
			t.Fatalf("zero-match outcome = %#v", noMatchOutcome)
		}
	})

	t.Run("server pagination completes with a bounded page limit", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{ID: "pagination-api-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "ConfigMap"}}
		paginatedOutcome := executeSource(t, ctx, resolver, selection.NewExecutor(lister, selection.WithPageLimit(1), selection.WithVerifier(verifier)), ownerNamespace, source)
		if paginatedOutcome.Err != nil || len(paginatedOutcome.Resources) != 3 {
			t.Fatalf("server-paginated outcome = %#v", paginatedOutcome)
		}
		for _, resource := range paginatedOutcome.Resources {
			if resource.Provenance.APIVersion != "v1" || resource.Provenance.Kind != "ConfigMap" || resource.Provenance.Namespace != ownerNamespace || resource.Provenance.Name == "" || resource.Provenance.UID == "" {
				t.Fatalf("server-paginated provenance = %#v", resource.Provenance)
			}
		}
	})

	t.Run("cross namespace and cluster scope preserve provenance", func(t *testing.T) {
		crossNamespace := v1alpha1.KubeseerSource{
			ID:         "cross-namespace-source",
			Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "ConfigMap"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{otherNamespace, ownerNamespace}},
			Selector:   &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"app": "shared"}},
		}
		crossOutcome := executeSource(t, ctx, resolver, executor, ownerNamespace, crossNamespace)
		if crossOutcome.Err != nil || len(crossOutcome.Resources) != 2 || crossOutcome.Resources[0].Provenance.Namespace != ownerNamespace || crossOutcome.Resources[1].Provenance.Namespace != otherNamespace {
			t.Fatalf("cross namespace outcome = %#v", crossOutcome)
		}

		node := v1alpha1.KubeseerSource{
			ID:       "cluster-node-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Node"},
			Selector: &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"role": "worker"}},
		}
		nodeOutcome := executeSource(t, ctx, resolver, executor, ownerNamespace, node)
		if nodeOutcome.Err != nil || len(nodeOutcome.Resources) != 1 || nodeOutcome.Resources[0].Provenance.Namespace != "" || nodeOutcome.Resources[0].Provenance.Name == "" {
			t.Fatalf("cluster selection outcome = %#v", nodeOutcome)
		}
	})

	t.Run("CRD-backed resources use their discovered identity", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID:       "widget-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "selection.kubeseer.io/v1", Kind: "Widget"},
			Selector: &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"app": "custom"}},
		}
		outcome := executeSource(t, ctx, resolver, executor, ownerNamespace, source)
		if outcome.Err != nil || len(outcome.Resources) != 1 || outcome.Resources[0].Provenance.APIVersion != "selection.kubeseer.io/v1" || outcome.Resources[0].Provenance.Kind != "Widget" {
			t.Fatalf("CRD-backed outcome = %#v", outcome)
		}
	})

	t.Run("unsupported field selector is source scoped and sanitized", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID:       "unsupported-field-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "ConfigMap"},
			Selector: &v1alpha1.ResourceSelector{FieldSelector: "spec.unsupported=value"},
		}
		outcome := executeSource(t, ctx, resolver, executor, ownerNamespace, source)
		if !selection.HasReason(outcome.Err, selection.ReasonUnsupportedSelector) || strings.Contains(outcome.Err.Error(), "value") {
			t.Fatalf("unsupported field selector outcome = %#v", outcome)
		}
	})

	t.Log("API_CONTRACT=resource-selection STATUS=passed")
	t.Log("API_CONTRACT=resource-selection-pagination STATUS=passed")
}

func executeSource(t *testing.T, ctx context.Context, resolver *discovery.Resolver, executor *selection.Executor, ownerNamespace string, source v1alpha1.KubeseerSource) selection.SelectionOutcome {
	t.Helper()
	planner := selection.NewPlanner(resolver)
	plan, err := planner.Plan(ctx, ownerNamespace, source)
	if err != nil {
		t.Fatalf("plan %q: %v", source.ID, err)
	}
	requests := make([]accesspolicy.Request, 0, len(plan.Targets()))
	for _, target := range plan.Targets() {
		requests = append(requests, selection.RequestForTarget(target))
	}
	subject := authorization.Subject{Key: types.NamespacedName{Namespace: ownerNamespace, Name: "selection-envtest"}, UID: types.UID("selection-envtest-uid"), Generation: 1}
	snapshot := selectionFixtureSnapshot(t)
	batch, err := authorization.NewEnforcer(nil).EvaluateBatch(ctx, subject, snapshot, requests)
	if err != nil {
		t.Fatalf("evaluate authorization fixture %q: %v", source.ID, err)
	}
	authorized, err := selection.BindCapabilities(plan, batch.Outcomes())
	if err != nil {
		t.Fatalf("bind %q: %v", source.ID, err)
	}
	return executor.Execute(ctx, authorized)
}

func selectionFixtureSnapshot(t *testing.T) accesspolicy.Snapshot {
	t.Helper()
	policy := &v1alpha1.KubeseerAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.InstallationAccessCeilingName, UID: types.UID("selection-policy-uid"), Generation: 1},
		Spec: v1alpha1.KubeseerAccessPolicySpec{
			Namespaces:         v1alpha1.NamespacePolicy{Mode: v1alpha1.NamespaceModeAllNonSystem},
			Resources:          []v1alpha1.ResourceRule{{APIGroups: []string{"", "selection.kubeseer.io"}, Kinds: []string{"ConfigMap", "Node", "Widget"}}},
			AllowClusterScoped: true,
		},
	}
	compiled, err := accesspolicy.Compile(policy)
	if err != nil {
		t.Fatalf("compile envtest selection policy: %v", err)
	}
	return compiled.Snapshot()
}

func createConfigMaps(t *testing.T, ctx context.Context, clients harness.Clients, ownerNamespace, otherNamespace string) {
	t.Helper()
	fixtures := []struct {
		namespace string
		name      string
		labels    map[string]string
	}{
		{namespace: ownerNamespace, name: "alpha", labels: map[string]string{"app": "demo", "tier": "frontend"}},
		{namespace: ownerNamespace, name: "beta", labels: map[string]string{"app": "demo", "tier": "backend"}},
		{namespace: ownerNamespace, name: "shared-owner", labels: map[string]string{"app": "shared"}},
		{namespace: otherNamespace, name: "shared-other", labels: map[string]string{"app": "shared"}},
	}
	for _, fixture := range fixtures {
		_, err := clients.Core.CoreV1().ConfigMaps(fixture.namespace).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: fixture.name, Namespace: fixture.namespace, Labels: fixture.labels}, Data: map[string]string{"observed": fixture.name}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("create ConfigMap %s/%s: %v", fixture.namespace, fixture.name, err)
		}
	}
}

func createNode(t *testing.T, ctx context.Context, clients harness.Clients, prefix string) {
	t.Helper()
	name := "selection-node-" + prefix
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"role": "worker", "kubeseer.io/selection-fixture": "true"}}}
	if _, err := clients.Core.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create cluster-scoped Node: %v", err)
	}
}

func createWidgetCRD(t *testing.T, ctx context.Context, environment *harness.Harness, clients harness.Clients) (*apiextensionsv1.CustomResourceDefinition, schema.GroupVersionResource) {
	t.Helper()
	group, version, plural := "selection.kubeseer.io", "v1", "widgets-"+strings.TrimPrefix(environment.Scope().Prefix, "kubeseer-")
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: plural + "." + group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: plural, Singular: "widget", Kind: "Widget", ListKind: "WidgetList"},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: version, Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{"spec": {Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{"value": {Type: "string"}}}}}},
			}},
		},
	}
	if _, err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create selection fixture CRD: %v", err)
	}
	if err := harness.WaitFor(ctx, 20*time.Second, func(ctx context.Context) (bool, error) {
		current, err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crd.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established {
				return condition.Status == apiextensionsv1.ConditionTrue, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait for selection fixture CRD: %v", err)
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: plural}
	if err := harness.WaitFor(ctx, 20*time.Second, func(ctx context.Context) (bool, error) {
		resources, err := clients.Discovery.ServerResourcesForGroupVersion(group + "/" + version)
		if err != nil {
			return false, nil
		}
		for _, resource := range resources.APIResources {
			if resource.Name == plural {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait for selection fixture discovery: %v", err)
	}
	return crd, gvr
}

func createWidgets(t *testing.T, ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string) {
	t.Helper()
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": gvr.Group + "/" + gvr.Version,
		"kind":       "Widget",
		"metadata": map[string]interface{}{
			"name":      "custom-alpha",
			"namespace": namespace,
			"labels":    map[string]interface{}{"app": "custom"},
		},
		"spec": map[string]interface{}{"value": "fixture"},
	}}
	if _, err := client.Resource(gvr).Namespace(namespace).Create(ctx, object, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create CRD-backed selection fixture: %v", err)
	}
}
