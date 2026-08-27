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

package integration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertReconciliationRuntimePipelineScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	policy := basePolicy()
	valuesSource := runtimePipelineValuesSource("values-source")
	deniedSource := v1alpha1.KubeseerSource{
		ID:         "denied-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-b"}},
		Fields:     []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}},
	}
	readSource := v1alpha1.KubeseerSource{
		ID:         "read-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
		Fields:     []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}},
	}
	emptySource := v1alpha1.KubeseerSource{
		ID:         "empty-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{}},
		Fields:     []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}},
	}
	key := types.NamespacedName{Namespace: "team-a", Name: "pipeline-owner"}
	object := runtimePipelineKubeseer(key, "pipeline-uid", 1, valuesSource, deniedSource, readSource, emptySource)

	t.Run("mixed outcomes preserve order, siblings, and sanitized failures", func(t *testing.T) {
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetResponse(valuesSource.ID, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			*runtimePipelineResource("first-resource", "uid-first", "kept-value-sentinel", "not-an-integer"),
			*runtimePipelineResource("second-resource", "uid-second", "second-value", "2"),
		}})
		lister.SetResponse(readSource.ID, nil, apierrors.NewServiceUnavailable("resource body must not enter diagnostics"))
		routes := &runtimePipelineRoutes{}
		publisher := &runtimePipelinePublisher{}
		tracker := reconciliation.NewFreshnessTracker()
		runtime := mustRuntimePipeline(t, resolver, reader, lister, policy, routes, publisher, tracker)

		result, reconcileErr := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		if result != (reconcile.Result{}) {
			t.Fatalf("mixed reconciliation result = %#v", result)
		}
		if reconcileErr == nil || !reconciliation.IsRetryable(reconcileErr) {
			t.Fatalf("mixed reconciliation error = %v, want one retryable read failure", reconcileErr)
		}
		publications := publisher.Publications()
		if len(publications) != 1 {
			t.Fatalf("mixed publications = %d, want one partial candidate", len(publications))
		}
		got := publications[0].Result
		if len(got.Sources) != 4 {
			t.Fatalf("mixed source cardinality = %d, want four", len(got.Sources))
		}
		wantIDs := []string{"values-source", "denied-source", "read-source", "empty-source"}
		for index, wantID := range wantIDs {
			if got.Sources[index].ID != wantID {
				t.Fatalf("source %d ID = %q, want declaration order %q", index, got.Sources[index].ID, wantID)
			}
		}
		if got.Sources[0].State != v1alpha1.SourceStateValues || len(got.Sources[0].Resources) != 2 {
			t.Fatalf("successful source result = %#v", got.Sources[0])
		}
		firstResource := got.Sources[0].Resources[0]
		if len(firstResource.Fields) != 2 {
			t.Fatalf("field-local result = %#v", firstResource.Fields)
		}
		if firstResource.Fields[0].Name != "bad-number" || firstResource.Fields[0].State != v1alpha1.FieldStateError {
			t.Fatalf("bad field result = %#v", firstResource.Fields[0])
		}
		if firstResource.Fields[1].Name != "good" || firstResource.Fields[1].State != v1alpha1.FieldStateValues || len(firstResource.Fields[1].Matches) != 1 || firstResource.Fields[1].Matches[0].StringValue == nil || *firstResource.Fields[1].Matches[0].StringValue != "kept-value-sentinel" {
			t.Fatalf("successful sibling field result = %#v", firstResource.Fields[1])
		}
		for _, index := range []int{1, 2} {
			if got.Sources[index].State != v1alpha1.SourceStateError || got.Sources[index].Error == nil {
				t.Fatalf("failed source %d result = %#v", index, got.Sources[index])
			}
		}
		if got.Sources[3].State != v1alpha1.SourceStateValues || len(got.Sources[3].Resources) != 0 {
			t.Fatalf("empty source result = %#v", got.Sources[3])
		}
		if got.Sources[1].Error.Reason != string(selection.ReasonAuthorizationDenied) || got.Sources[2].Error.Reason != string(selection.ReasonReadUnavailable) {
			t.Fatalf("stable source failure reasons = denied=%#v read=%#v", got.Sources[1].Error, got.Sources[2].Error)
		}
		for _, text := range []string{"resource body must not enter diagnostics", "kept-value-sentinel", "{.data.value}"} {
			if strings.Contains(got.Sources[2].Error.Message, text) || strings.Contains(got.Sources[1].Error.Message, text) {
				t.Fatalf("diagnostic leaked %q: result=%#v", text, got)
			}
		}
		calls := lister.Calls()
		if len(calls) != 2 || calls[0].Target.SourceID != valuesSource.ID || calls[1].Target.SourceID != readSource.ID {
			t.Fatalf("resource LIST calls = %#v, want only allowed executable sources in order", calls)
		}
		if routes.LastRouteCount() != 2 {
			t.Fatalf("authorized route count = %d, want values and read targets", routes.LastRouteCount())
		}
		if !reflect.DeepEqual(publisher.Order(), []string{"publish"}) {
			t.Fatalf("publisher order = %#v", publisher.Order())
		}
	})

	t.Run("all deterministic failures and an empty object remain publishable", func(t *testing.T) {
		invalidSource := v1alpha1.KubeseerSource{
			ID:       "invalid-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Selector: &v1alpha1.ResourceSelector{FieldSelector: "metadata.name in ("},
		}
		allFailedObject := runtimePipelineKubeseer(key, "pipeline-uid-failed", 1, deniedSource, invalidSource)
		reader := newRuntimePipelineReader(allFailedObject)
		lister := newRuntimePipelineLister()
		publisher := &runtimePipelinePublisher{}
		routes := &runtimePipelineRoutes{}
		runtime := mustRuntimePipeline(t, resolver, reader, lister, policy, routes, publisher, reconciliation.NewFreshnessTracker())
		_, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		if err != nil {
			t.Fatalf("all deterministic failures returned error = %v", err)
		}
		publications := publisher.Publications()
		if len(publications) != 1 || len(publications[0].Result.Sources) != 2 {
			t.Fatalf("all-failed publication = %#v", publications)
		}
		for index, source := range publications[0].Result.Sources {
			if source.State != v1alpha1.SourceStateError || source.Error == nil || len(source.Resources) != 0 {
				t.Fatalf("all-failed source %d = %#v", index, source)
			}
		}
		if len(lister.Calls()) != 0 || routes.LastRouteCount() != 0 {
			t.Fatalf("deterministic failures reached LIST/routes: calls=%#v routes=%d", lister.Calls(), routes.LastRouteCount())
		}

		emptyKey := types.NamespacedName{Namespace: "team-a", Name: "empty-pipeline"}
		emptyObject := runtimePipelineKubeseer(emptyKey, "pipeline-uid-empty", 1)
		emptyPolicy := &runtimePipelinePolicySource{err: errors.New("policy unavailable must not block present empty result")}
		emptyPublisher := &runtimePipelinePublisher{}
		emptyRuntime := mustRuntimePipelineWithPolicy(t, resolver, newRuntimePipelineReader(emptyObject), newRuntimePipelineLister(), emptyPolicy, &runtimePipelineRoutes{}, emptyPublisher, reconciliation.NewFreshnessTracker())
		_, err = emptyRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: emptyKey})
		if err != nil {
			t.Fatalf("empty object with unavailable policy returned error = %v", err)
		}
		publications = emptyPublisher.Publications()
		if len(publications) != 1 || len(publications[0].Result.Sources) != 0 {
			t.Fatalf("present empty result = %#v", publications)
		}
	})

	t.Run("fresh policy invalidation removes old values and broadening requires a fresh run", func(t *testing.T) {
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetResponse(valuesSource.ID, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*runtimePipelineResource("fresh-resource", "uid-fresh", "fresh-value", "7")}})
		policySource := &runtimePipelinePolicySource{policy: policy}
		publisher := &runtimePipelinePublisher{}
		tracker := reconciliation.NewFreshnessTracker()
		routes := &runtimePipelineRoutes{}
		runtime := mustRuntimePipelineWithPolicy(t, resolver, reader, lister, policySource, routes, publisher, tracker)
		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("initial fresh-policy reconciliation: %v", err)
		}
		initialListCalls := len(lister.Calls())
		tracker.InvalidateAll()
		policySource.SetPolicy(mutatePolicy(policy, func(next *v1alpha1.KubeseerAccessPolicy) {
			next.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{""}, Kinds: []string{"Node"}}}
		}))
		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("narrowed fresh-policy reconciliation: %v", err)
		}
		publications := publisher.Publications()
		if len(publications) != 2 || publications[1].Result.Sources[0].State != v1alpha1.SourceStateError || len(publications[1].Result.Sources[0].Resources) != 0 {
			t.Fatalf("narrowed result retained old value: %#v", publications)
		}
		if got := len(lister.Calls()); got != initialListCalls {
			t.Fatalf("narrowed policy issued %d LIST calls, want unchanged count %d", got, initialListCalls)
		}

		tracker.InvalidateAll()
		policySource.SetPolicy(policy)
		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("broadened fresh-policy reconciliation: %v", err)
		}
		publications = publisher.Publications()
		if len(publications) != 3 || publications[2].Result.Sources[0].State != v1alpha1.SourceStateValues || len(publications[2].Result.Sources[0].Resources) != 1 {
			t.Fatalf("broadened result did not require fresh reconciliation: %#v", publications)
		}
	})

	t.Run("discovery and read availability are retryable but invalid configuration is not", func(t *testing.T) {
		discoveryFailure := errors.New("discovery response must stay private")
		failureResolver := discovery.NewResolver(&selectionDiscoveryFailureClient{delegate: newPolicyDiscoveryClient(), cause: discoveryFailure})
		discoverySource := v1alpha1.KubeseerSource{ID: "discovery-source", Resource: v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"}}
		discoveryKey := types.NamespacedName{Namespace: "team-a", Name: "discovery-owner"}
		discoveryObject := runtimePipelineKubeseer(discoveryKey, "discovery-uid", 1, discoverySource)
		discoveryPublisher := &runtimePipelinePublisher{}
		discoveryLister := newRuntimePipelineLister()
		discoveryRuntime := mustRuntimePipeline(t, failureResolver, newRuntimePipelineReader(discoveryObject), discoveryLister, policy, &runtimePipelineRoutes{}, discoveryPublisher, reconciliation.NewFreshnessTracker())
		_, err := discoveryRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: discoveryKey})
		if err == nil || !reconciliation.IsRetryable(err) || len(discoveryLister.Calls()) != 0 {
			t.Fatalf("discovery availability result: err=%v calls=%#v", err, discoveryLister.Calls())
		}
		if got := discoveryPublisher.Publications()[0].Result.Sources[0].Error; got == nil || got.Reason != string(selection.ReasonInvalidSource) || strings.Contains(got.Message, discoveryFailure.Error()) {
			t.Fatalf("discovery diagnostic = %#v", got)
		}

		invalidKey := types.NamespacedName{Namespace: "team-a", Name: "invalid-owner"}
		invalidObject := runtimePipelineKubeseer(invalidKey, "invalid-uid", 1, v1alpha1.KubeseerSource{ID: "invalid-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}, Selector: &v1alpha1.ResourceSelector{FieldSelector: "metadata.name in ("}})
		invalidPublisher := &runtimePipelinePublisher{}
		invalidLister := newRuntimePipelineLister()
		invalidRuntime := mustRuntimePipeline(t, resolver, newRuntimePipelineReader(invalidObject), invalidLister, policy, &runtimePipelineRoutes{}, invalidPublisher, reconciliation.NewFreshnessTracker())
		_, err = invalidRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: invalidKey})
		if err != nil || len(invalidLister.Calls()) != 0 {
			t.Fatalf("invalid configuration result: err=%v calls=%#v", err, invalidLister.Calls())
		}
	})

	t.Run("cancellation withholds the candidate and starts no later source read", func(t *testing.T) {
		first := runtimePipelineValuesSource("cancel-first")
		second := runtimePipelineValuesSource("cancel-second")
		cancelKey := types.NamespacedName{Namespace: "team-a", Name: "cancel-owner"}
		cancelObject := runtimePipelineKubeseer(cancelKey, "cancel-uid", 1, first, second)
		lister := newRuntimePipelineLister()
		started := make(chan struct{})
		var startedOnce sync.Once
		lister.SetHook(func(ctx context.Context, target selection.ReadTarget, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			if target.SourceID == first.ID {
				startedOnce.Do(func() { close(started) })
				<-ctx.Done()
			}
			return nil, ctx.Err()
		})
		publisher := &runtimePipelinePublisher{}
		cancelRuntime := mustRuntimePipeline(t, resolver, newRuntimePipelineReader(cancelObject), lister, policy, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
		cancelContext, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			_, reconcileErr := cancelRuntime.Reconcile(cancelContext, reconcile.Request{NamespacedName: cancelKey})
			done <- reconcileErr
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("cancellation fixture did not start the first LIST")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("canceled reconciliation returned error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled reconciliation did not finish")
		}
		if len(publisher.Publications()) != 0 || len(lister.Calls()) != 1 || lister.Calls()[0].Target.SourceID != first.ID {
			t.Fatalf("canceled pipeline started later work or published: calls=%#v publications=%#v", lister.Calls(), publisher.Publications())
		}
	})

	t.Run("same-key work is excluded while another key progresses", func(t *testing.T) {
		firstKey := types.NamespacedName{Namespace: "team-a", Name: "busy-owner"}
		secondKey := types.NamespacedName{Namespace: "team-a", Name: "free-owner"}
		empty := func(id string) v1alpha1.KubeseerSource {
			return v1alpha1.KubeseerSource{ID: id, Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}, Namespaces: &v1alpha1.NamespaceSelection{Names: []string{}}}
		}
		reader := newRuntimePipelineReader(runtimePipelineKubeseer(firstKey, "busy-uid", 1, empty("busy-source")), runtimePipelineKubeseer(secondKey, "free-uid", 1, empty("free-source")))
		publisher := &runtimePipelinePublisher{blockKey: firstKey, blockStarted: make(chan struct{}), blockRelease: make(chan struct{})}
		runtime := mustRuntimePipeline(t, resolver, reader, newRuntimePipelineLister(), policy, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
		firstDone := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: firstKey})
			firstDone <- err
		}()
		select {
		case <-publisher.blockStarted:
		case <-time.After(time.Second):
			t.Fatal("busy key did not reach publication")
		}
		sameDone := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: firstKey})
			sameDone <- err
		}()
		select {
		case err := <-sameDone:
			t.Fatalf("same-key reconciliation completed while first was active: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
		differentDone := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: secondKey})
			differentDone <- err
		}()
		select {
		case err := <-differentDone:
			if err != nil {
				t.Fatalf("independent key returned error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("independent key was blocked by busy key")
		}
		close(publisher.blockRelease)
		select {
		case err := <-firstDone:
			if err != nil {
				t.Fatalf("busy key returned error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("busy key did not finish after release")
		}
		select {
		case err := <-sameDone:
			if err != nil {
				t.Fatalf("queued same-key reconciliation returned error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("queued same-key reconciliation did not run")
		}
		if got := len(publisher.Publications()); got != 3 {
			t.Fatalf("publication count = %d, want busy, independent, and follow-up", got)
		}
	})
}

func runtimePipelineValuesSource(id string) v1alpha1.KubeseerSource {
	return v1alpha1.KubeseerSource{
		ID:         id,
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
		Fields: []v1alpha1.KubeseerField{
			{Name: "bad-number", Path: "{.data.bad}", Type: v1alpha1.ValueTypeInteger},
			{Name: "good", Path: "{.data.good}", Type: v1alpha1.ValueTypeString},
		},
	}
}

func runtimePipelineKubeseer(key types.NamespacedName, uid types.UID, generation int64, sources ...v1alpha1.KubeseerSource) *v1alpha1.Kubeseer {
	return &v1alpha1.Kubeseer{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name, UID: uid, Generation: generation}, Spec: v1alpha1.KubeseerSpec{Sources: append([]v1alpha1.KubeseerSource(nil), sources...)}}
}

func runtimePipelineResource(name string, uid types.UID, good, bad string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": "team-a",
			"uid":       string(uid),
		},
		"data": map[string]interface{}{"good": good, "bad": bad},
	}}
}

func mustRuntimePipeline(t *testing.T, resolver *discovery.Resolver, reader *runtimePipelineReader, lister *runtimePipelineLister, policy *v1alpha1.KubeseerAccessPolicy, routes *runtimePipelineRoutes, publisher *runtimePipelinePublisher, tracker *reconciliation.FreshnessTracker) *reconciliation.Runtime {
	t.Helper()
	return mustRuntimePipelineWithPolicy(t, resolver, reader, lister, &runtimePipelinePolicySource{policy: policy}, routes, publisher, tracker)
}

func mustRuntimePipelineWithPolicy(t *testing.T, resolver *discovery.Resolver, reader *runtimePipelineReader, lister *runtimePipelineLister, policy accesspolicy.PolicySource, routes *runtimePipelineRoutes, publisher *runtimePipelinePublisher, tracker *reconciliation.FreshnessTracker) *reconciliation.Runtime {
	t.Helper()
	runtime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour}, reconciliation.Dependencies{
		Reader:       reader,
		Lister:       reader,
		PolicySource: policy,
		Planner:      selection.NewPlanner(resolver),
		Executor:     selection.NewExecutor(lister),
		Routes:       routes,
		Publisher:    publisher,
		Tracker:      tracker,
	})
	if err != nil {
		t.Fatalf("construct reconciliation runtime: %v", err)
	}
	return runtime
}

type runtimePipelineReader struct {
	mu      sync.Mutex
	objects map[types.NamespacedName]*v1alpha1.Kubeseer
}

func newRuntimePipelineReader(objects ...*v1alpha1.Kubeseer) *runtimePipelineReader {
	reader := &runtimePipelineReader{objects: make(map[types.NamespacedName]*v1alpha1.Kubeseer)}
	for _, object := range objects {
		reader.objects[types.NamespacedName{Namespace: object.Namespace, Name: object.Name}] = object.DeepCopy()
	}
	return reader
}

func (r *runtimePipelineReader) Get(_ context.Context, key types.NamespacedName, object *v1alpha1.Kubeseer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.objects[key]
	if current == nil {
		return apierrors.NewNotFound(schemaGroupResourceKubeseer(), key.Name)
	}
	*object = *current.DeepCopy()
	return nil
}

func (r *runtimePipelineReader) List(_ context.Context, list *v1alpha1.KubeseerList) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list.Items = list.Items[:0]
	for _, object := range r.objects {
		list.Items = append(list.Items, *object.DeepCopy())
	}
	return nil
}

func schemaGroupResourceKubeseer() schema.GroupResource {
	return schema.GroupResource{Group: "kubeseer.io", Resource: "kubeseers"}
}

type runtimePipelinePolicySource struct {
	mu     sync.Mutex
	policy *v1alpha1.KubeseerAccessPolicy
	err    error
}

func (s *runtimePipelinePolicySource) Get(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.policy == nil {
		return nil, nil
	}
	return s.policy.DeepCopy(), nil
}

func (s *runtimePipelinePolicySource) SetPolicy(policy *v1alpha1.KubeseerAccessPolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = policy.DeepCopy()
	s.err = nil
}

type runtimePipelineListCall struct {
	Target  selection.ReadTarget
	Options metav1.ListOptions
}

type runtimePipelineListResponse struct {
	list *unstructured.UnstructuredList
	err  error
}

type runtimePipelineLister struct {
	mu        sync.Mutex
	responses map[string]runtimePipelineListResponse
	calls     []runtimePipelineListCall
	hook      func(context.Context, selection.ReadTarget, metav1.ListOptions) (*unstructured.UnstructuredList, error)
}

func newRuntimePipelineLister() *runtimePipelineLister {
	return &runtimePipelineLister{responses: make(map[string]runtimePipelineListResponse)}
}

func (l *runtimePipelineLister) SetResponse(sourceID string, list *unstructured.UnstructuredList, errs ...error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var err error
	if len(errs) != 0 {
		err = errs[0]
	}
	l.responses[sourceID] = runtimePipelineListResponse{list: list, err: err}
}

func (l *runtimePipelineLister) SetHook(hook func(context.Context, selection.ReadTarget, metav1.ListOptions) (*unstructured.UnstructuredList, error)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hook = hook
}

func (l *runtimePipelineLister) List(ctx context.Context, target selection.ReadTarget, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	l.mu.Lock()
	l.calls = append(l.calls, runtimePipelineListCall{Target: target, Options: options})
	hook := l.hook
	response := l.responses[target.SourceID]
	l.mu.Unlock()
	if hook != nil {
		return hook(ctx, target, options)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response.err != nil {
		return nil, response.err
	}
	if response.list == nil {
		return &unstructured.UnstructuredList{}, nil
	}
	return response.list.DeepCopy(), nil
}

func (l *runtimePipelineLister) Calls() []runtimePipelineListCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]runtimePipelineListCall(nil), l.calls...)
}

type runtimePipelineRouteReplacement struct {
	Lease      reconciliation.Lease
	RouteCount int
}

type runtimePipelineRoutes struct {
	mu           sync.Mutex
	replacements []runtimePipelineRouteReplacement
}

func (r *runtimePipelineRoutes) Replace(lease reconciliation.Lease, routes []reconciliation.AuthorizedRoute) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replacements = append(r.replacements, runtimePipelineRouteReplacement{Lease: lease, RouteCount: len(routes)})
	return nil
}

func (r *runtimePipelineRoutes) RemoveOwner(types.NamespacedName) {}

func (r *runtimePipelineRoutes) RemoveAll() {}

func (r *runtimePipelineRoutes) LastRouteCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.replacements) == 0 {
		return 0
	}
	return r.replacements[len(r.replacements)-1].RouteCount
}

type runtimePipelinePublication struct {
	Lease      reconciliation.Lease
	Evaluation statuscontract.Evaluation
	Result     v1alpha1.KubeseerResult
}

type runtimePipelinePublisher struct {
	mu           sync.Mutex
	publications []runtimePipelinePublication
	order        []string
	blockKey     types.NamespacedName
	blockStarted chan struct{}
	blockRelease chan struct{}
	blockOnce    sync.Once
}

func (p *runtimePipelinePublisher) Publish(ctx context.Context, lease reconciliation.Lease, evaluation statuscontract.Evaluation) error {
	if lease.Key == p.blockKey && p.blockStarted != nil {
		p.blockOnce.Do(func() { close(p.blockStarted) })
		select {
		case <-p.blockRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.mu.Lock()
	publication := runtimePipelinePublication{Lease: lease, Evaluation: cloneStatusEvaluation(evaluation)}
	if evaluation.Result != nil {
		publication.Result = *evaluation.Result.DeepCopy()
	}
	p.publications = append(p.publications, publication)
	p.order = append(p.order, "publish")
	p.mu.Unlock()
	return nil
}

func cloneStatusEvaluation(evaluation statuscontract.Evaluation) statuscontract.Evaluation {
	copy := evaluation
	if evaluation.Result != nil {
		copy.Result = evaluation.Result.DeepCopy()
	}
	copy.Sources = append([]statuscontract.SourceAssessment(nil), evaluation.Sources...)
	if evaluation.GlobalAuthorization != nil {
		value := *evaluation.GlobalAuthorization
		copy.GlobalAuthorization = &value
	}
	return copy
}

func (p *runtimePipelinePublisher) Publications() []runtimePipelinePublication {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]runtimePipelinePublication(nil), p.publications...)
}

func (p *runtimePipelinePublisher) Order() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.order...)
}
