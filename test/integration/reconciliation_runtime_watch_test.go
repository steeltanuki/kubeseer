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
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertReconciliationRuntimeWatchRoutingScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	planner := selection.NewPlanner(resolver)
	snapshot := mustSnapshot(t, basePolicy())
	ownerA := types.NamespacedName{Namespace: "team-a", Name: "route-owner-a"}
	ownerB := types.NamespacedName{Namespace: "team-a", Name: "route-owner-b"}

	namespacedSourceA := v1alpha1.KubeseerSource{
		ID:         "watch-source-a",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
		Selector: &v1alpha1.ResourceSelector{
			Name:          "route-value-sentinel",
			MatchLabels:   map[string]string{"route-label-sentinel": "route-value-sentinel"},
			FieldSelector: "metadata.name=route-value-sentinel",
		},
		Fields: []v1alpha1.KubeseerField{{Name: "route-field", Path: "spec.route.path.sentinel", Type: v1alpha1.ValueTypeString}},
	}
	namespacedSourceB := namespacedSourceA
	namespacedSourceB.ID = "watch-source-b"
	namespacedTargetA := mustRuntimeTarget(t, ctx, planner, ownerA.Namespace, namespacedSourceA)
	namespacedTargetB := mustRuntimeTarget(t, ctx, planner, ownerB.Namespace, namespacedSourceB)
	if !reflect.DeepEqual(namespacedTargetA.GVR, namespacedTargetB.GVR) || namespacedTargetA.Scope != namespacedTargetB.Scope || namespacedTargetA.Namespace != namespacedTargetB.Namespace {
		t.Fatalf("equivalent route targets differ: A=%#v B=%#v", namespacedTargetA, namespacedTargetB)
	}
	if decision := snapshot.Evaluate(selection.RequestForTarget(namespacedTargetA)); !decision.Allowed || decision.Reason != accesspolicy.ReasonAllowed {
		t.Fatalf("real policy denied namespaced route target: %#v", decision)
	}

	t.Run("deny before watch and share exact target transport", func(t *testing.T) {
		watcher := newRuntimeScriptedMetadataWatcher()
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond))
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := registry.Start(watchContext, queue); err != nil {
			t.Fatalf("start route registry: %v", err)
		}

		deniedSource := v1alpha1.KubeseerSource{
			ID:         "watch-denied-source",
			Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-b"}},
		}
		deniedTarget := mustRuntimeTarget(t, ctx, planner, ownerA.Namespace, deniedSource)
		deniedDecision := snapshot.Evaluate(selection.RequestForTarget(deniedTarget))
		if deniedDecision.Allowed || deniedDecision.Reason != accesspolicy.ReasonNamespaceDenied {
			t.Fatalf("real policy denied-target decision = %#v", deniedDecision)
		}
		if _, err := reconciliation.NewAuthorizedRoute(deniedTarget, authorization.Capability{}); err == nil {
			t.Fatal("denied target produced an authorized route")
		}
		if got := len(watcher.Calls()); got != 0 {
			t.Fatalf("denied target started %d WATCH calls", got)
		}

		objectA := newRuntimeKubeseer(ownerA, "uid-a", 1)
		tracker.Observe(objectA)
		leaseA, _, releaseA, err := tracker.Acquire(ctx, ownerA, objectA.UID, objectA.Generation)
		if err != nil {
			t.Fatalf("acquire owner A lease: %v", err)
		}
		defer releaseA()
		routeA, err := mustRuntimeAuthorizedRoute(t, leaseA, namespacedTargetA, snapshot)
		if err != nil {
			t.Fatalf("authorize owner A route: %v", err)
		}
		if got := routeA.Binding(); got.SourceID != namespacedSourceA.ID || got.Address.Namespace != "team-a" || got.Address.Scope != discovery.ScopeNamespaced {
			t.Fatalf("owner A route binding = %#v", got)
		}
		if err := registry.Replace(leaseA, []reconciliation.AuthorizedRoute{routeA}); err != nil {
			t.Fatalf("replace owner A routes: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		calls := watcher.Calls()
		wantAddress := routeA.Address()
		if calls[0].Address != wantAddress || calls[0].ResourceVersion != "" {
			t.Fatalf("first WATCH call = %#v, want exact address %#v and empty resourceVersion", calls[0], wantAddress)
		}
		if registry.WatchCount() != 1 || registry.BindingCount(ownerA) != 1 {
			t.Fatalf("initial route indexes: watches=%d bindings=%d", registry.WatchCount(), registry.BindingCount(ownerA))
		}

		objectB := newRuntimeKubeseer(ownerB, "uid-b", 1)
		tracker.Observe(objectB)
		leaseB, _, releaseB, err := tracker.Acquire(ctx, ownerB, objectB.UID, objectB.Generation)
		if err != nil {
			t.Fatalf("acquire owner B lease: %v", err)
		}
		defer releaseB()
		routeB, err := mustRuntimeAuthorizedRoute(t, leaseB, namespacedTargetB, snapshot)
		if err != nil {
			t.Fatalf("authorize owner B route: %v", err)
		}
		if err := registry.Replace(leaseB, []reconciliation.AuthorizedRoute{routeB}); err != nil {
			t.Fatalf("replace owner B routes: %v", err)
		}
		if registry.WatchCount() != 1 || registry.BindingCount(ownerB) != 1 {
			t.Fatalf("shared route indexes: watches=%d bindings B=%d", registry.WatchCount(), registry.BindingCount(ownerB))
		}
		if got := len(watcher.Calls()); got != 1 {
			t.Fatalf("equivalent owner route opened %d transports, want one", got)
		}
		owners := registry.Owners(wantAddress)
		if !reflect.DeepEqual(owners, []types.NamespacedName{ownerA, ownerB}) {
			t.Fatalf("route owners = %#v, want A and B", owners)
		}

		stream := watcher.Stream(0)
		if stream == nil {
			t.Fatal("shared route has no scripted stream")
		}
		sentinelObject := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":            "nonmatching-object",
				"namespace":       "team-a",
				"resourceVersion": "rv-route-1",
				"labels": map[string]interface{}{
					"other-label": "not-the-selector",
				},
			},
			"spec": map[string]interface{}{
				"route": map[string]interface{}{"path": map[string]interface{}{"sentinel": "secret-route-value-sentinel"}},
			},
		}}
		for _, eventType := range []watch.EventType{watch.Added, watch.Modified, watch.Deleted} {
			stream.Action(eventType, sentinelObject)
			got := collectRuntimeQueueKeys(t, queue, 2)
			if !got[ownerA] || !got[ownerB] || len(got) != 2 {
				t.Fatalf("%s routed keys = %#v, want both owners", eventType, got)
			}
		}
		registryDump := fmt.Sprintf("%#v", registry)
		for _, forbidden := range []string{"secret-route-value-sentinel", "spec.route.path.sentinel", "route-value-sentinel"} {
			if strings.Contains(registryDump, forbidden) {
				t.Fatalf("route registry retained forbidden %q: %s", forbidden, registryDump)
			}
		}

		clusterPolicy := mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
			policy.Spec.AllowClusterScoped = true
		})
		clusterSnapshot := mustSnapshot(t, clusterPolicy)
		clusterSource := v1alpha1.KubeseerSource{ID: "watch-cluster-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Node"}}
		clusterTarget := mustRuntimeTarget(t, ctx, planner, ownerA.Namespace, clusterSource)
		clusterDecision := clusterSnapshot.Evaluate(selection.RequestForTarget(clusterTarget))
		if !clusterDecision.Allowed {
			t.Fatalf("cluster route policy decision = %#v", clusterDecision)
		}
		updated := objectA.DeepCopy()
		updated.Generation = 2
		tracker.Observe(updated)
		releaseA()
		leaseA2, _, releaseA2, err := tracker.Acquire(ctx, ownerA, updated.UID, updated.Generation)
		if err != nil {
			t.Fatalf("acquire updated owner A lease: %v", err)
		}
		defer releaseA2()
		clusterRoute, err := mustRuntimeAuthorizedRoute(t, leaseA2, clusterTarget, clusterSnapshot)
		if err != nil {
			t.Fatalf("authorize updated cluster route: %v", err)
		}
		if err := registry.Replace(leaseA2, []reconciliation.AuthorizedRoute{clusterRoute}); err != nil {
			t.Fatalf("replace stale owner A route: %v", err)
		}
		if registry.BindingCount(ownerA) != 1 || len(registry.Owners(wantAddress)) != 1 || registry.Owners(wantAddress)[0] != ownerB {
			t.Fatalf("current-generation replacement left stale owner route: bindings=%d owners=%#v", registry.BindingCount(ownerA), registry.Owners(wantAddress))
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 2 })
		if got := watcher.Calls()[1].Address; got.Scope != discovery.ScopeCluster || got.Namespace != "" || got.GVR.Resource != "nodes" {
			t.Fatalf("cluster WATCH address = %#v", got)
		}

		registry.RemoveOwner(ownerB)
		waitForRuntimeWatchCondition(t, func() bool { return stream.IsStopped() && len(registry.Owners(wantAddress)) == 0 })
		registry.RemoveOwner(ownerA)
		waitForRuntimeWatchCondition(t, func() bool { return registry.WatchCount() == 0 })
		registry.RemoveAll()
	})

	t.Run("restart errors and closed streams with bounded resume state", func(t *testing.T) {
		watcher := newRuntimeScriptedMetadataWatcher(
			runtimeWatchResponse{stream: watch.NewRaceFreeFake()},
			runtimeWatchResponse{err: apierrors.NewGone("watch resource version expired")},
			runtimeWatchResponse{stream: watch.NewRaceFreeFake()},
		)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond))
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := registry.Start(watchContext, queue); err != nil {
			t.Fatalf("start restart registry: %v", err)
		}
		object := newRuntimeKubeseer(ownerA, "uid-restart", 1)
		tracker.Observe(object)
		lease, _, release, err := tracker.Acquire(ctx, ownerA, object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire restart lease: %v", err)
		}
		defer release()
		route, err := mustRuntimeAuthorizedRoute(t, lease, namespacedTargetA, snapshot)
		if err != nil {
			t.Fatalf("authorize restart route: %v", err)
		}
		if err := registry.Replace(lease, []reconciliation.AuthorizedRoute{route}); err != nil {
			t.Fatalf("replace restart route: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		first := watcher.Stream(0)
		if first == nil {
			t.Fatal("restart fixture did not create first stream")
		}
		first.Action(watch.Added, runtimeMetadataObject("rv-route-1", "secret-watch-event-value"))
		_ = collectRuntimeQueueKeys(t, queue, 1)
		first.Error(&metav1.Status{Reason: metav1.StatusReasonInternalError})
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 2 })
		calls := watcher.Calls()
		if calls[1].ResourceVersion != "rv-route-1" {
			t.Fatalf("restart after stream error used resourceVersion %q, want rv-route-1", calls[1].ResourceVersion)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 3 })
		calls = watcher.Calls()
		if calls[2].ResourceVersion != "" {
			t.Fatalf("expired WATCH restart used stale resourceVersion %q", calls[2].ResourceVersion)
		}
		registry.RemoveOwner(ownerA)
		waitForRuntimeWatchCondition(t, func() bool {
			return registry.WatchCount() == 0 && watcher.Stream(1) != nil && watcher.Stream(1).IsStopped()
		})
	})

	t.Run("metadata adapter addresses exact scope without selectors", func(t *testing.T) {
		client := &runtimeRecordingMetadataClient{}
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(reconciliation.NewClientMetadataWatcher(client), tracker, reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond))
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := registry.Start(watchContext, queue); err != nil {
			t.Fatalf("start metadata adapter registry: %v", err)
		}
		addresses := []struct {
			address   reconciliation.WatchAddress
			gvr       schema.GroupVersionResource
			kind      string
			namespace string
		}{
			{
				address:   reconciliation.WatchAddress{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Scope: discovery.ScopeNamespaced, Namespace: "team-a"},
				gvr:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
				kind:      "Deployment",
				namespace: "team-a",
			},
			{
				address: reconciliation.WatchAddress{GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, Scope: discovery.ScopeCluster},
				gvr:     schema.GroupVersionResource{Version: "v1", Resource: "nodes"},
				kind:    "Node",
			},
		}
		for index, test := range addresses {
			owner := types.NamespacedName{Namespace: "team-a", Name: fmt.Sprintf("adapter-owner-%d", index)}
			object := newRuntimeKubeseer(owner, types.UID(fmt.Sprintf("adapter-owner-uid-%d", index)), 1)
			tracker.Observe(object)
			lease, _, release, err := tracker.Acquire(ctx, owner, object.UID, object.Generation)
			if err != nil {
				t.Fatalf("acquire metadata adapter lease: %v", err)
			}
			target := selection.ReadTarget{SourceID: fmt.Sprintf("adapter-source-%d", index), GVR: test.gvr, Kind: test.kind, Scope: test.address.Scope, Namespace: test.namespace}
			policy := basePolicy()
			if test.address.Scope == discovery.ScopeCluster {
				policy.Spec.AllowClusterScoped = true
			}
			route, err := mustRuntimeAuthorizedRoute(t, lease, target, mustSnapshot(t, policy))
			if err != nil {
				release()
				t.Fatalf("authorize metadata adapter route: %v", err)
			}
			if err := registry.Replace(lease, []reconciliation.AuthorizedRoute{route}); err != nil {
				release()
				t.Fatalf("replace metadata adapter route: %v", err)
			}
			waitForRuntimeWatchCondition(t, func() bool { return len(client.Calls()) >= index+1 })
			registry.RemoveOwner(owner)
			release()
		}
		gotRequests := client.Calls()
		if len(gotRequests) != len(addresses) {
			t.Fatalf("metadata adapter requests = %#v", gotRequests)
		}
		for index, request := range gotRequests {
			if request.GVR != addresses[index].gvr || request.Namespace != addresses[index].namespace {
				t.Fatalf("metadata request = %#v, want GVR=%#v namespace=%q", request, addresses[index].gvr, addresses[index].namespace)
			}
			if request.Options.ResourceVersion != "" || !request.Options.AllowWatchBookmarks || request.Options.LabelSelector != "" || request.Options.FieldSelector != "" {
				t.Fatalf("metadata watch options = %#v, want only resume bookmark options", request.Options)
			}
		}
	})
}

func mustRuntimeTarget(t *testing.T, ctx context.Context, planner *selection.Planner, ownerNamespace string, source v1alpha1.KubeseerSource) selection.ReadTarget {
	t.Helper()
	plan, err := planner.Plan(ctx, ownerNamespace, source)
	if err != nil {
		t.Fatalf("plan runtime route source %q: %v", source.ID, err)
	}
	targets := plan.Targets()
	if len(targets) != 1 {
		t.Fatalf("runtime route source %q targets = %#v, want one", source.ID, targets)
	}
	return targets[0]
}

func mustRuntimeAuthorizedRoute(t *testing.T, lease reconciliation.Lease, target selection.ReadTarget, snapshot accesspolicy.Snapshot) (reconciliation.AuthorizedRoute, error) {
	return runtimeAuthorizedRouteWithRecorder(t, lease, target, snapshot, nil)
}

func runtimeAuthorizedRouteWithRecorder(t *testing.T, lease reconciliation.Lease, target selection.ReadTarget, snapshot accesspolicy.Snapshot, recorder authorization.Recorder) (reconciliation.AuthorizedRoute, error) {
	t.Helper()
	subject := authorization.Subject{Key: lease.Key, UID: lease.UID, Generation: lease.Generation, PolicyEpoch: lease.PolicyEpoch}
	request := selection.RequestForTarget(target)
	batch, err := authorization.NewEnforcer(recorder).EvaluateBatch(context.Background(), subject, snapshot, []accesspolicy.Request{request})
	if err != nil {
		return reconciliation.AuthorizedRoute{}, err
	}
	outcomes := batch.Outcomes()
	if len(outcomes) != 1 {
		return reconciliation.AuthorizedRoute{}, errors.New("route authorization produced no exact outcome")
	}
	capability, ok := outcomes[0].Capability()
	if !ok {
		return reconciliation.AuthorizedRoute{}, errors.New("route authorization was denied")
	}
	return reconciliation.NewAuthorizedRoute(target, capability)
}

type runtimeWatchCall struct {
	Address         reconciliation.WatchAddress
	ResourceVersion string
	PermitSubjects  []authorization.Subject
}

type runtimeWatchResponse struct {
	stream *watch.RaceFreeFakeWatcher
	err    error
}

type runtimeScriptedMetadataWatcher struct {
	mu        sync.Mutex
	responses []runtimeWatchResponse
	calls     []runtimeWatchCall
	streams   []*watch.RaceFreeFakeWatcher
}

func newRuntimeScriptedMetadataWatcher(responses ...runtimeWatchResponse) *runtimeScriptedMetadataWatcher {
	return &runtimeScriptedMetadataWatcher{responses: append([]runtimeWatchResponse(nil), responses...)}
}

func (w *runtimeScriptedMetadataWatcher) Watch(_ context.Context, permit reconciliation.WatchPermit, resourceVersion string) (watch.Interface, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	capabilities := permit.Capabilities()
	subjects := make([]authorization.Subject, 0, len(capabilities))
	for _, capability := range capabilities {
		subjects = append(subjects, capability.Subject())
	}
	w.calls = append(w.calls, runtimeWatchCall{Address: permit.Address(), ResourceVersion: resourceVersion, PermitSubjects: subjects})
	response := runtimeWatchResponse{}
	if len(w.responses) != 0 {
		response = w.responses[0]
		w.responses = w.responses[1:]
	}
	if response.err != nil {
		return nil, response.err
	}
	if response.stream == nil {
		response.stream = watch.NewRaceFreeFake()
	}
	w.streams = append(w.streams, response.stream)
	return response.stream, nil
}

func (w *runtimeScriptedMetadataWatcher) Calls() []runtimeWatchCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]runtimeWatchCall(nil), w.calls...)
}

func (w *runtimeScriptedMetadataWatcher) Stream(index int) *watch.RaceFreeFakeWatcher {
	w.mu.Lock()
	defer w.mu.Unlock()
	if index < 0 || index >= len(w.streams) {
		return nil
	}
	return w.streams[index]
}

func collectRuntimeQueueKeys(t *testing.T, queue workqueue.TypedRateLimitingInterface[reconcile.Request], want int) map[types.NamespacedName]bool {
	t.Helper()
	waitForRuntimeQueue(t, queue, want)
	keys := make(map[types.NamespacedName]bool, want)
	for index := 0; index < want; index++ {
		item, shutdown := queue.Get()
		if shutdown {
			t.Fatal("runtime queue shut down while collecting route events")
		}
		keys[item.NamespacedName] = true
		queue.Done(item)
		queue.Forget(item)
	}
	return keys
}

func waitForRuntimeWatchCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("timed out waiting for runtime watch condition")
		case <-ticker.C:
		}
	}
}

func runtimeMetadataObject(resourceVersion, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":            "restart-object",
			"namespace":       "team-a",
			"resourceVersion": resourceVersion,
		},
		"value": value,
	}}
}

type runtimeMetadataCall struct {
	GVR       schema.GroupVersionResource
	Namespace string
	Options   metav1.ListOptions
}

type runtimeRecordingMetadataClient struct {
	mu    sync.Mutex
	calls []runtimeMetadataCall
}

func (c *runtimeRecordingMetadataClient) Resource(gvr schema.GroupVersionResource) metadata.Getter {
	return &runtimeRecordingMetadataResource{client: c, gvr: gvr}
}

func (c *runtimeRecordingMetadataClient) Calls() []runtimeMetadataCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]runtimeMetadataCall(nil), c.calls...)
}

type runtimeRecordingMetadataResource struct {
	client    *runtimeRecordingMetadataClient
	gvr       schema.GroupVersionResource
	namespace string
}

func (r *runtimeRecordingMetadataResource) Namespace(namespace string) metadata.ResourceInterface {
	copy := *r
	copy.namespace = namespace
	return &copy
}

func (r *runtimeRecordingMetadataResource) Watch(_ context.Context, options metav1.ListOptions) (watch.Interface, error) {
	r.client.mu.Lock()
	r.client.calls = append(r.client.calls, runtimeMetadataCall{GVR: r.gvr, Namespace: r.namespace, Options: options})
	r.client.mu.Unlock()
	return watch.NewRaceFreeFake(), nil
}

func (r *runtimeRecordingMetadataResource) Delete(context.Context, string, metav1.DeleteOptions, ...string) error {
	return nil
}

func (r *runtimeRecordingMetadataResource) DeleteCollection(context.Context, metav1.DeleteOptions, metav1.ListOptions) error {
	return nil
}

func (r *runtimeRecordingMetadataResource) Get(context.Context, string, metav1.GetOptions, ...string) (*metav1.PartialObjectMetadata, error) {
	return nil, nil
}

func (r *runtimeRecordingMetadataResource) List(context.Context, metav1.ListOptions) (*metav1.PartialObjectMetadataList, error) {
	return nil, nil
}

func (r *runtimeRecordingMetadataResource) Patch(context.Context, string, types.PatchType, []byte, metav1.PatchOptions, ...string) (*metav1.PartialObjectMetadata, error) {
	return nil, nil
}
