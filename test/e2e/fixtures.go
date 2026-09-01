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

package e2e

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/yaml"
)

//go:embed fixtures/*.yaml
var fixtureFiles embed.FS

const (
	fixtureAPIGroup   = "fixtures.kubeseer.io"
	fixtureAPIVersion = "v1alpha1"
	fixtureResource   = "widgets"
	fixtureKind       = "Widget"
)

var fixtureGVR = schema.GroupVersionResource{Group: fixtureAPIGroup, Version: fixtureAPIVersion, Resource: fixtureResource}

// FixtureSet tracks only objects created by the current run and scenario.
// Cleanup is reverse ordered and treats NotFound as an already-completed
// idempotent deletion.
type FixtureSet struct {
	Session    *ClusterSession
	ScenarioID string
	Namespace  string
	Labels     map[string]string
	cleanups   []fixtureCleanup
}

type fixtureCleanup struct {
	name string
	fn   func(context.Context) error
}

// NewFixtureSet returns a run-scoped fixture owner.
func NewFixtureSet(session *ClusterSession, scenarioID string) (*FixtureSet, error) {
	if session == nil {
		return nil, errors.New("fixture set requires a cluster session")
	}
	if strings.TrimSpace(scenarioID) == "" {
		return nil, errors.New("fixture set requires a scenario ID")
	}
	labels := map[string]string{
		"kubeseer.io/e2e-cluster":  session.Metadata.ClusterName,
		"kubeseer.io/e2e-scenario": scenarioID,
	}
	return &FixtureSet{Session: session, ScenarioID: scenarioID, Namespace: session.Metadata.Namespace, Labels: labels}, nil
}

func (f *FixtureSet) scopedName(prefix string) string {
	identity := strings.ToLower(strings.NewReplacer("/", "-", "_", "-", ".", "-").Replace(f.Session.Metadata.ClusterName))
	name := strings.Trim(strings.Join([]string{"e2e", prefix, identity, strings.ToLower(f.ScenarioID)}, "-"), "-")
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.Trim(name, "-")
}

func (f *FixtureSet) register(name string, fn func(context.Context) error) {
	f.cleanups = append(f.cleanups, fixtureCleanup{name: name, fn: fn})
}

// EnsureNamespace creates or verifies the scenario namespace. The harness's
// release namespace remains separate from fixture state.
func (f *FixtureSet) EnsureNamespace(ctx context.Context) (string, error) {
	name := f.scopedName("fixtures")
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: f.Labels}}
	created, err := f.Session.Core.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		current, getErr := f.Session.Core.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return "", getErr
		}
		if current.Labels["kubeseer.io/e2e-cluster"] != f.Session.Metadata.ClusterName {
			return "", fmt.Errorf("namespace %q is not owned by this E2E run", name)
		}
		f.Namespace = name
		return name, nil
	}
	if err != nil {
		return "", fmt.Errorf("create fixture namespace: %w", err)
	}
	f.Namespace = created.Name
	f.register("namespace/"+name, func(cleanupCtx context.Context) error {
		err := f.Session.Core.CoreV1().Namespaces().Delete(cleanupCtx, name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	return created.Name, nil
}

// CreateDeployment creates a minimal deterministic built-in fixture.
func (f *FixtureSet) CreateDeployment(ctx context.Context, name string, labels map[string]string, replicas int32) (*appsv1.Deployment, error) {
	if name == "" {
		name = f.scopedName("deployment")
	}
	merged := mergeLabels(f.Labels, labels)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: f.Namespace, Labels: merged},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": name}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/name": name}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "fixture", Image: "registry.k8s.io/pause:3.10"}}}},
		},
	}
	created, err := f.Session.Core.AppsV1().Deployments(f.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	f.register("deployment/"+name, func(cleanupCtx context.Context) error {
		err := f.Session.Core.AppsV1().Deployments(f.Namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	return created, nil
}

// CreatePod creates a deterministic Pod fixture, including explicit run labels.
func (f *FixtureSet) CreatePod(ctx context.Context, name, namespace string, labels map[string]string, data map[string]interface{}) (*corev1.Pod, error) {
	if name == "" {
		name = f.scopedName("pod")
	}
	if namespace == "" {
		namespace = f.Namespace
	}
	merged := mergeLabels(f.Labels, labels)
	annotations := map[string]string{}
	for key, value := range data {
		annotations[key] = fmt.Sprint(value)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: merged, Annotations: annotations}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "fixture", Image: "registry.k8s.io/pause:3.10"}}}}
	created, err := f.Session.Core.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	f.register("pod/"+namespace+"/"+name, func(cleanupCtx context.Context) error {
		err := f.Session.Core.CoreV1().Pods(namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	return created, nil
}

// CreateSentinelSecret creates an unrelated Secret used only to prove that
// diagnostics never serialize protected payloads. No scenario selects it.
func (f *FixtureSet) CreateSentinelSecret(ctx context.Context, name, namespace string) (*corev1.Secret, error) {
	if name == "" {
		name = f.scopedName("sentinel")
	}
	if namespace == "" {
		namespace = f.Namespace
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: f.Labels},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"payload": []byte("e2e-secret-sentinel")},
	}
	created, err := f.Session.Core.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	f.register("secret/"+namespace+"/"+name, func(cleanupCtx context.Context) error {
		err := f.Session.Core.CoreV1().Secrets(namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	return created, nil
}

// InstallFixtureCRD installs the committed structural CRD and waits for
// discovery to publish it.
func (f *FixtureSet) InstallFixtureCRD(ctx context.Context) (*apiextensionsv1.CustomResourceDefinition, error) {
	contents, err := fixtureFiles.ReadFile("fixtures/widget-crd.yaml")
	if err != nil {
		return nil, err
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal(contents, crd); err != nil {
		return nil, fmt.Errorf("decode fixture CRD: %w", err)
	}
	created, err := f.Session.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = f.Session.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crd.Name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("install fixture CRD: %w", err)
	}
	if err := wait.PollUntilContextCancel(ctx, 100*time.Millisecond, true, func(pollCtx context.Context) (bool, error) {
		current, getErr := f.Session.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(pollCtx, crd.Name, metav1.GetOptions{})
		if getErr != nil {
			return false, getErr
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("wait fixture CRD establishment: %w", err)
	}
	f.register("crd/"+crd.Name, func(cleanupCtx context.Context) error {
		err := f.Session.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Delete(cleanupCtx, crd.Name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	return created, nil
}

// CreateWidget creates a structural custom resource through the dynamic API.
func (f *FixtureSet) CreateWidget(ctx context.Context, name, namespace string, spec map[string]interface{}, labels map[string]string) (*unstructured.Unstructured, error) {
	if name == "" {
		name = f.scopedName("widget")
	}
	if namespace == "" {
		namespace = f.Namespace
	}
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": fixtureAPIGroup + "/" + fixtureAPIVersion,
		"kind":       fixtureKind,
		"metadata": map[string]interface{}{
			"name": name, "namespace": namespace, "labels": mergeLabels(f.Labels, labels),
		},
		"spec": spec,
	}}
	created, err := f.Session.Dynamic.Resource(fixtureGVR).Namespace(namespace).Create(ctx, object, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	f.register("widget/"+namespace+"/"+name, func(cleanupCtx context.Context) error {
		err := f.Session.Dynamic.Resource(fixtureGVR).Namespace(namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	return created, nil
}

// CreateKubeseer writes a public API object and registers status-safe cleanup.
func (f *FixtureSet) CreateKubeseer(ctx context.Context, name string, spec map[string]interface{}) (*unstructured.Unstructured, error) {
	if name == "" {
		name = f.scopedName("kubeseer")
	}
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kubeseer.io/v1alpha1", "kind": "Kubeseer",
		"metadata": map[string]interface{}{"name": name, "namespace": f.Namespace, "labels": f.Labels},
		"spec":     spec,
	}}
	created, err := f.Session.Dynamic.Resource(KubeseerResource).Namespace(f.Namespace).Create(ctx, object, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	f.register("kubeseer/"+f.Namespace+"/"+name, func(cleanupCtx context.Context) error {
		err := f.Session.Dynamic.Resource(KubeseerResource).Namespace(f.Namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	return created, nil
}

// Cleanup removes only resources registered by this run and scenario.
func (f *FixtureSet) Cleanup(ctx context.Context) error {
	var failures []string
	for index := len(f.cleanups) - 1; index >= 0; index-- {
		entry := f.cleanups[index]
		if err := entry.fn(ctx); err != nil {
			failures = append(failures, entry.name+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("fixture cleanup failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func mergeLabels(base, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}
