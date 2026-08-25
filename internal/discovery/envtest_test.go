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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	harness "github.com/steeltanuki/kubeseer/test/envtest"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

const (
	envtestRequestTimeout = 30 * time.Second
	envtestCRDTimeout     = 20 * time.Second
)

func TestEnvtestDiscovery(t *testing.T) {
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
	config := environment.Config()
	config.Timeout = envtestRequestTimeout
	discoveryClient, transport, err := newCountingDiscovery(config)
	if err != nil {
		t.Fatalf("create counting discovery client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), envtestCRDTimeout)
	defer cancel()

	crd := discoveryTestCRD(environment.Scope())
	extensionsClient := clients.APIExtensions
	environment.AddCleanup("delete discovery contract CRD", func(ctx context.Context) error {
		err := extensionsClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crd.Name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	if _, err := extensionsClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("install discovery test CRD: %v", err)
	}
	waitForCRDEstablished(t, ctx, extensionsClient, crd.Name)
	waitForDiscoveryResource(t, ctx, discoveryClient, crdGroupVersion(crd), crd.Spec.Names.Plural)

	scenario := newDiscoveryScenario(discoveryClient)
	transport.Reset()

	t.Run("descriptor and error contracts", func(t *testing.T) {
		assertEnvtestDiscoveryContracts(t)

		resolver := NewResolver(scenario)
		unknown, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "unknown-source", APIVersion: "v1", Kind: "Missing"})
		if !HasReason(err, ReasonUnknownType) || unknown != (Resolution{}) {
			t.Fatalf("unknown real discovery type was not source-scoped: resolution=%#v err=%v", unknown, err)
		}

		discoveryFailure := errors.New("local discovery request failed")
		scenario.SetError("apps/v1", discoveryFailure)
		_, err = resolver.Resolve(ctx, SourceDescriptor{SourceID: "unavailable-source", APIVersion: "apps/v1", Kind: "Deployment"})
		if !HasReason(err, ReasonDiscoveryUnavailable) || !errors.Is(err, discoveryFailure) {
			t.Fatalf("discovery failure did not preserve source-scoped cause: %v", err)
		}
		scenario.Reset()

		scenario.SetOverride("discovery.kubeseer.io/v1", &metav1.APIResourceList{
			GroupVersion: "discovery.kubeseer.io/v1",
			APIResources: []metav1.APIResource{
				{Name: crd.Spec.Names.Plural, Kind: "Widget", Namespaced: true},
				{Name: "clusterwidgets", Kind: "Widget", Namespaced: false},
				{Name: crd.Spec.Names.Plural + "/status", Kind: "Widget", Namespaced: true},
			},
		})
		_, err = NewResolver(scenario).Resolve(ctx, SourceDescriptor{SourceID: "ambiguous-source", APIVersion: "discovery.kubeseer.io/v1", Kind: "Widget"})
		if !HasReason(err, ReasonAmbiguousResource) || err.Error() != `source "ambiguous-source": AmbiguousResource: multiple resources match kind "Widget" in "discovery.kubeseer.io/v1"` {
			t.Fatalf("ambiguous real discovery response was not deterministic: %v", err)
		}
		scenario.Reset()
	})

	t.Run("real built-in and CRD resolution", func(t *testing.T) {
		resolver := NewResolver(scenario)
		namespaced, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "builtin-pod", APIVersion: "v1", Kind: "Pod"})
		if err != nil || namespaced.Resource != (schema.GroupVersionResource{Version: "v1", Resource: "pods"}) || namespaced.Scope != ScopeNamespaced {
			t.Fatalf("unexpected built-in namespaced resolution: %#v err=%v", namespaced, err)
		}
		clusterScoped, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "builtin-node", APIVersion: "v1", Kind: "Node"})
		if err != nil || clusterScoped.Resource != (schema.GroupVersionResource{Version: "v1", Resource: "nodes"}) || clusterScoped.Scope != ScopeCluster {
			t.Fatalf("unexpected built-in cluster resolution: %#v err=%v", clusterScoped, err)
		}
		deployment, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "builtin-deployment", APIVersion: "apps/v1", Kind: "Deployment"})
		if err != nil || deployment.Resource != (schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}) || deployment.Scope != ScopeNamespaced {
			t.Fatalf("unexpected grouped resolution: %#v err=%v", deployment, err)
		}
		widget, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "custom-widget", APIVersion: "discovery.kubeseer.io/v1", Kind: "Widget"})
		if err != nil || widget.Resource != (schema.GroupVersionResource{Group: "discovery.kubeseer.io", Version: "v1", Resource: crd.Spec.Names.Plural}) || widget.Scope != ScopeNamespaced {
			t.Fatalf("unexpected CRD-backed resolution: %#v err=%v", widget, err)
		}
		if err := RequireNamespaced(clusterScoped); !HasReason(err, ReasonInvalidScope) || !strings.Contains(err.Error(), "cluster") {
			t.Fatalf("cluster scope guard did not reject namespaced use: %v", err)
		}
	})

	t.Run("batch isolation", func(t *testing.T) {
		scenario.Reset()
		discoveryFailure := errors.New("batch discovery request failed")
		scenario.SetError("apps/v1", discoveryFailure)
		outcomes := NewResolver(scenario).ResolveBatch(ctx, []SourceDescriptor{
			{SourceID: "pod-source", APIVersion: "v1", Kind: "Pod"},
			{SourceID: "deployment-source", APIVersion: "apps/v1", Kind: "Deployment"},
		})
		if len(outcomes) != 2 || outcomes[0].Resolution == nil || outcomes[0].Err != nil || outcomes[1].Resolution != nil || !HasReason(outcomes[1].Err, ReasonDiscoveryUnavailable) || !errors.Is(outcomes[1].Err, discoveryFailure) {
			t.Fatalf("batch outcomes were not independently source-scoped: %#v", outcomes)
		}
		canceledContext, cancel := context.WithCancel(context.Background())
		cancel()
		canceled := NewResolver(scenario).ResolveBatch(canceledContext, []SourceDescriptor{
			{SourceID: "first-source", APIVersion: "v1", Kind: "Pod"},
			{SourceID: "second-source", APIVersion: "v1", Kind: "Pod"},
		})
		if len(canceled) != 2 || !HasReason(canceled[0].Err, ReasonDiscoveryUnavailable) || !HasReason(canceled[1].Err, ReasonDiscoveryUnavailable) || !errors.Is(canceled[0].Err, context.Canceled) || !errors.Is(canceled[1].Err, context.Canceled) {
			t.Fatalf("canceled batch did not preserve source-scoped context failures: %#v", canceled)
		}
		scenario.Reset()
	})

	t.Run("cache freshness and invalidation", func(t *testing.T) {
		assertEnvtestCacheFreshHit(t, ctx, scenario, transport)
		assertEnvtestCacheExpiry(t, ctx, scenario, transport)
		assertEnvtestCacheInvalidation(t, ctx, scenario, transport)
	})

	t.Run("concurrent refresh", func(t *testing.T) {
		assertEnvtestConcurrentRefresh(t, ctx, scenario, transport, crd)
	})

	t.Logf("discovery contract passed with Kubernetes assets %s", environment.AssetsDirectory())
	t.Log("API_CONTRACT=resource-discovery STATUS=passed")
}

func discoveryTestCRD(scope harness.Scope) *apiextensionsv1.CustomResourceDefinition {
	suffix := strings.TrimPrefix(scope.Prefix, "kubeseer-")
	plural := "widgets" + suffix
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: plural + ".discovery.kubeseer.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "discovery.kubeseer.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     plural,
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

func waitForCRDEstablished(t *testing.T, ctx context.Context, client apiextensionsclient.Interface, name string) {
	t.Helper()
	err := harness.WaitFor(ctx, envtestCRDTimeout, func(ctx context.Context) (bool, error) {
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

func waitForDiscoveryResource(t *testing.T, ctx context.Context, client DiscoveryClient, groupVersion, resourceName string) {
	t.Helper()
	err := harness.WaitFor(ctx, envtestCRDTimeout, func(context.Context) (bool, error) {
		resources, err := client.ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			return false, nil
		}
		for _, resource := range resources.APIResources {
			if resource.Name == resourceName {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("wait for discovery resource %s in %s: %v", resourceName, groupVersion, err)
	}
}

func assertEnvtestDiscoveryContracts(t *testing.T) {
	t.Helper()
	for _, descriptor := range []SourceDescriptor{
		{SourceID: "pods", APIVersion: "v1", Kind: "Pod"},
		{SourceID: "deployments", APIVersion: "apps/v1", Kind: "Deployment"},
	} {
		if err := descriptor.Validate(); err != nil {
			t.Fatalf("valid descriptor %q rejected: %v", descriptor.SourceID, err)
		}
	}

	for _, testCase := range []struct {
		name       string
		descriptor SourceDescriptor
		message    string
	}{
		{name: "missing api version", descriptor: SourceDescriptor{SourceID: "missing-version", Kind: "Pod"}, message: "apiVersion must not be empty"},
		{name: "malformed api version", descriptor: SourceDescriptor{SourceID: "malformed-version", APIVersion: "apps/", Kind: "Deployment"}, message: `apiVersion "apps/" is invalid`},
		{name: "missing kind", descriptor: SourceDescriptor{SourceID: "missing-kind", APIVersion: "v1"}, message: "kind must not be empty"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.descriptor.Validate()
			if !HasReason(err, ReasonInvalidDescriptor) || !strings.Contains(err.Error(), testCase.descriptor.SourceID) || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("invalid descriptor contract failed: %v", err)
			}
		})
	}

	namespaced := Resolution{SourceID: "pods", Resource: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Scope: ScopeNamespaced}
	cluster := Resolution{SourceID: "nodes", Resource: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, Scope: ScopeCluster}
	if !namespaced.Scope.Valid() || namespaced.Scope.String() != "namespaced" || !cluster.Scope.Valid() || cluster.Scope.String() != "cluster" {
		t.Fatalf("scope values are not stable: namespaced=%#v cluster=%#v", namespaced.Scope, cluster.Scope)
	}
	if namespaced.Resource.String() != "/v1, Resource=pods" || cluster.Resource.String() != "/v1, Resource=nodes" {
		t.Fatalf("GVR values are not stable: namespaced=%s cluster=%s", namespaced.Resource, cluster.Resource)
	}

	cause := errors.New("discovery request failed")
	err := &ResolutionError{SourceID: "unavailable", Reason: ReasonDiscoveryUnavailable, Message: "discovery request failed", Cause: cause}
	if !HasReason(err, ReasonDiscoveryUnavailable) || !errors.Is(err, cause) || err.Error() != `source "unavailable": DiscoveryUnavailable: discovery request failed` {
		t.Fatalf("resolution error contract is unstable: %v", err)
	}
	for _, reason := range []ResolutionErrorReason{ReasonUnknownType, ReasonDiscoveryUnavailable, ReasonAmbiguousResource} {
		err := NewResolutionError("source-one", reason, "resolution failed")
		if !HasReason(err, reason) || !strings.Contains(err.Error(), `source "source-one"`) {
			t.Fatalf("source-scoped reason contract failed for %q: %v", reason, err)
		}
	}
}

func newCountingDiscovery(config *rest.Config) (DiscoveryClient, *countingTransport, error) {
	var transport *countingTransport
	config.WrapTransport = func(delegate http.RoundTripper) http.RoundTripper {
		transport = &countingTransport{delegate: delegate}
		return transport
	}
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	if transport == nil {
		return nil, nil, errors.New("client-go did not install the counting transport")
	}
	return client, transport, nil
}

type countingTransport struct {
	delegate http.RoundTripper
	mu       sync.Mutex
	requests []string
}

func (t *countingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, request.URL.Path)
	t.mu.Unlock()
	return t.delegate.RoundTrip(request)
}

func (t *countingTransport) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests = nil
}

func (t *countingTransport) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

type discoveryScenario struct {
	delegate DiscoveryClient
	mu       sync.RWMutex
	override map[string]*metav1.APIResourceList
	errors   map[string]error
	hook     func()
}

func newDiscoveryScenario(delegate DiscoveryClient) *discoveryScenario {
	return &discoveryScenario{
		delegate: delegate,
		override: make(map[string]*metav1.APIResourceList),
		errors:   make(map[string]error),
	}
}

func (s *discoveryScenario) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	resources, delegateErr := s.delegate.ServerResourcesForGroupVersion(groupVersion)
	s.mu.RLock()
	hook := s.hook
	s.mu.RUnlock()
	if hook != nil {
		hook()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.errors[groupVersion]; err != nil {
		return nil, err
	}
	if override := s.override[groupVersion]; override != nil {
		return override.DeepCopy(), nil
	}
	return resources, delegateErr
}

func (s *discoveryScenario) SetOverride(groupVersion string, resources *metav1.APIResourceList) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if resources == nil {
		delete(s.override, groupVersion)
		return
	}
	s.override[groupVersion] = resources.DeepCopy()
}

func (s *discoveryScenario) SetError(groupVersion string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.errors, groupVersion)
		return
	}
	s.errors[groupVersion] = err
}

func (s *discoveryScenario) SetHook(hook func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hook = hook
}

func (s *discoveryScenario) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.override = make(map[string]*metav1.APIResourceList)
	s.errors = make(map[string]error)
	s.hook = nil
}

type testClock struct {
	mu      sync.RWMutex
	current time.Time
}

func newTestClock(current time.Time) *testClock {
	return &testClock{current: current}
}

func (c *testClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

func (c *testClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(duration)
}

func assertEnvtestCacheFreshHit(t *testing.T, ctx context.Context, scenario *discoveryScenario, transport *countingTransport) {
	t.Helper()
	scenario.Reset()
	transport.Reset()
	clock := newTestClock(time.Unix(100, 0))
	resolver := NewResolver(scenario, WithCacheTTL(time.Minute), WithClock(clock.Now))
	first, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "first-source", APIVersion: "v1", Kind: "Pod"})
	if err != nil {
		t.Fatalf("resolve first fresh-cache source: %v", err)
	}
	second, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "second-source", APIVersion: "v1", Kind: "Pod"})
	if err != nil {
		t.Fatalf("resolve second fresh-cache source: %v", err)
	}
	if transport.Count() != 1 || second.SourceID != "second-source" || second.Resource != first.Resource || second.Scope != first.Scope {
		t.Fatalf("fresh cache did not preserve caller metadata or request count: first=%#v second=%#v requests=%d", first, second, transport.Count())
	}
}

func assertEnvtestCacheExpiry(t *testing.T, ctx context.Context, scenario *discoveryScenario, transport *countingTransport) {
	t.Helper()
	scenario.Reset()
	transport.Reset()
	clock := newTestClock(time.Unix(200, 0))
	resolver := NewResolver(scenario, WithCacheTTL(time.Minute), WithClock(clock.Now))
	first, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "source", APIVersion: "v1", Kind: "Pod"})
	if err != nil {
		t.Fatalf("resolve before expiry: %v", err)
	}
	scenario.SetOverride("v1", &metav1.APIResourceList{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{{Name: "pods-v2", Kind: "Pod", Namespaced: true}},
	})
	clock.Advance(time.Minute)
	refreshed, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "source", APIVersion: "v1", Kind: "Pod"})
	if err != nil {
		t.Fatalf("resolve after expiry: %v", err)
	}
	if transport.Count() != 2 || first.Resource == refreshed.Resource || refreshed.Resource.Resource != "pods-v2" {
		t.Fatalf("expired cache did not refresh against local discovery: first=%#v refreshed=%#v requests=%d", first, refreshed, transport.Count())
	}
	scenario.Reset()
}

func assertEnvtestCacheInvalidation(t *testing.T, ctx context.Context, scenario *discoveryScenario, transport *countingTransport) {
	t.Helper()
	scenario.Reset()
	transport.Reset()
	clock := newTestClock(time.Unix(400, 0))
	resolver := NewResolver(scenario, WithCacheTTL(time.Hour), WithClock(clock.Now))
	if _, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "pod-source", APIVersion: "v1", Kind: "Pod"}); err != nil {
		t.Fatalf("seed pod cache: %v", err)
	}
	if _, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "node-source", APIVersion: "v1", Kind: "Node"}); err != nil {
		t.Fatalf("seed node cache: %v", err)
	}
	if transport.Count() != 2 {
		t.Fatalf("unexpected local discovery seed count: %d", transport.Count())
	}
	scenario.SetOverride("v1", &metav1.APIResourceList{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			{Name: "pods-v2", Kind: "Pod", Namespaced: true},
			{Name: "nodes-v2", Kind: "Node", Namespaced: false},
		},
	})
	resolver.InvalidateResource("/v1", "Pod")
	refreshedPod, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "pod-source", APIVersion: "v1", Kind: "Pod"})
	if err != nil || refreshedPod.Resource.Resource != "pods-v2" || transport.Count() != 3 {
		t.Fatalf("resource invalidation did not refresh one local discovery entry: resolution=%#v requests=%d err=%v", refreshedPod, transport.Count(), err)
	}
	if _, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "node-source", APIVersion: "v1", Kind: "Node"}); err != nil {
		t.Fatalf("resolve unaffected cached resource: %v", err)
	}
	if transport.Count() != 3 {
		t.Fatalf("resource invalidation evicted an unrelated kind: %d", transport.Count())
	}
	resolver.InvalidateGroupVersion("v1")
	if _, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "pod-source", APIVersion: "v1", Kind: "Pod"}); err != nil {
		t.Fatalf("resolve pod after group/version invalidation: %v", err)
	}
	if _, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "node-source", APIVersion: "v1", Kind: "Node"}); err != nil {
		t.Fatalf("resolve node after group/version invalidation: %v", err)
	}
	if transport.Count() != 5 {
		t.Fatalf("group/version invalidation did not refresh both local entries: %d", transport.Count())
	}
	resolver.InvalidateAll()
	if _, err := resolver.Resolve(ctx, SourceDescriptor{SourceID: "pod-source", APIVersion: "v1", Kind: "Pod"}); err != nil {
		t.Fatalf("resolve after full invalidation: %v", err)
	}
	if transport.Count() != 6 {
		t.Fatalf("full invalidation did not evict local cache: %d", transport.Count())
	}
	scenario.Reset()
}

func assertEnvtestConcurrentRefresh(t *testing.T, ctx context.Context, scenario *discoveryScenario, transport *countingTransport, crd *apiextensionsv1.CustomResourceDefinition) {
	t.Helper()
	scenario.Reset()
	transport.Reset()
	clock := newTestClock(time.Unix(300, 0))
	resolver := NewResolver(scenario, WithCacheTTL(time.Minute), WithClock(clock.Now))
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	var releaseOnce sync.Once
	scenario.SetHook(func() {
		startOnce.Do(func() { close(started) })
		<-release
	})
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		releaseAll()
		scenario.SetHook(nil)
	}()

	const workers = 8
	results := make(chan Resolution, workers)
	errorsSeen := make(chan error, workers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for index := 0; index < workers; index++ {
		go func(index int) {
			defer waitGroup.Done()
			resolution, err := resolver.Resolve(ctx, SourceDescriptor{
				SourceID:   fmt.Sprintf("source-%d", index),
				APIVersion: crdGroupVersion(crd),
				Kind:       "Widget",
			})
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- resolution
		}(index)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent envtest refresh did not reach the local discovery endpoint")
	}
	releaseAll()
	done := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent envtest refresh did not complete")
	}
	if transport.Count() != 1 {
		t.Fatalf("concurrent refresh stampeded the local discovery endpoint: %d requests", transport.Count())
	}
	for index := 0; index < workers; index++ {
		select {
		case err := <-errorsSeen:
			t.Fatalf("concurrent local discovery refresh failed: %v", err)
		case resolution := <-results:
			if resolution.Resource.Resource != crd.Spec.Names.Plural || resolution.Scope != ScopeNamespaced {
				t.Fatalf("unexpected concurrent CRD resolution: %#v", resolution)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("missing concurrent local discovery resolution outcome")
		}
	}
}

func crdGroupVersion(crd *apiextensionsv1.CustomResourceDefinition) string {
	return crd.Spec.Group + "/" + crd.Spec.Versions[0].Name
}
