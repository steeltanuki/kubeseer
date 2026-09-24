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
	"sync"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertReconciliationRuntimeSchedulingScenarios(t *testing.T) {
	t.Helper()
	keyA := types.NamespacedName{Namespace: "team-a", Name: "runtime-a"}
	keyB := types.NamespacedName{Namespace: "team-b", Name: "runtime-b"}
	store := newRuntimeTestStore(
		newRuntimeKubeseer(keyA, "uid-a", 1),
		newRuntimeKubeseer(keyB, "uid-b", 1),
	)
	routes := &runtimeTestRoutes{}
	tracker := reconciliation.NewFreshnessTracker()

	t.Run("invalid safety interval fails before source startup", func(t *testing.T) {
		queue := newRuntimeQueue()
		source := reconciliation.NewTriggerSource(store, routes, reconciliation.Options{})
		if err := source.Start(context.Background(), queue); err == nil {
			t.Fatal("zero safety interval unexpectedly started")
		}
		if routes.Started() {
			t.Fatal("route manager started after invalid trigger setup")
		}
		if store.ListCalls() != 0 {
			t.Fatalf("invalid setup issued %d Kubeseer lists", store.ListCalls())
		}
	})

	t.Run("lifecycle events update identity before enqueue and suppress status-only updates", func(t *testing.T) {
		queue := newRuntimeQueue()
		handler := reconciliation.NewLifecycleHandler(tracker, routes)
		object := newRuntimeKubeseer(keyA, "uid-a", 1)
		handler.Create(context.Background(), event.TypedCreateEvent[*v1alpha1.Kubeseer]{Object: object}, queue)
		if got := queue.Len(); got != 1 {
			t.Fatalf("create queue length = %d, want 1", got)
		}
		lease, child, release, err := tracker.Acquire(context.Background(), keyA, object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire lifecycle lease: %v", err)
		}
		defer release()
		newObject := object.DeepCopy()
		newObject.Generation = 2
		newObject.ResourceVersion = "2"
		if !(reconciliation.KubeseerPredicate{}).Update(event.TypedUpdateEvent[*v1alpha1.Kubeseer]{ObjectOld: object, ObjectNew: newObject}) {
			t.Fatal("generation update was suppressed")
		}
		handler.Update(context.Background(), event.TypedUpdateEvent[*v1alpha1.Kubeseer]{ObjectOld: object, ObjectNew: newObject}, queue)
		select {
		case <-child.Done():
		case <-time.After(time.Second):
			t.Fatal("generation update did not cancel the old lease")
		}
		if lease.Generation != 1 {
			t.Fatalf("lease was mutated in place: %#v", lease)
		}

		statusOnlyOld := newObject.DeepCopy()
		statusOnlyNew := newObject.DeepCopy()
		statusOnlyNew.Status.ObservedGeneration = 2
		if (reconciliation.KubeseerPredicate{}).Update(event.TypedUpdateEvent[*v1alpha1.Kubeseer]{ObjectOld: statusOnlyOld, ObjectNew: statusOnlyNew}) {
			t.Fatal("status-only update was not suppressed")
		}

		deleting := newObject.DeepCopy()
		deleting.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		if !(reconciliation.KubeseerPredicate{}).Update(event.TypedUpdateEvent[*v1alpha1.Kubeseer]{ObjectOld: newObject, ObjectNew: deleting}) {
			t.Fatal("deletion timestamp update was suppressed")
		}
		handler.Update(context.Background(), event.TypedUpdateEvent[*v1alpha1.Kubeseer]{ObjectOld: newObject, ObjectNew: deleting}, queue)
		state, ok := tracker.State(keyA)
		if !ok || !state.Deleting {
			t.Fatalf("deletion state = %#v, present=%t", state, ok)
		}
		if got := routes.Removed(); got != 1 {
			t.Fatalf("deletion removed routes %d times, want 1", got)
		}
	})

	t.Run("queue coalesces bursts and preserves active follow-up", func(t *testing.T) {
		queue := newRuntimeQueue()
		request := reconcile.Request{NamespacedName: keyA}
		queue.Add(request)
		queue.Add(request)
		queue.Add(request)
		if queue.Len() != 1 {
			t.Fatalf("burst queue length = %d, want 1", queue.Len())
		}
		item, shutdown := queue.Get()
		if shutdown || item != request {
			t.Fatalf("queue Get = item=%#v shutdown=%t", item, shutdown)
		}
		queue.Add(request)
		queue.Done(item)
		if queue.Len() != 1 {
			t.Fatalf("active follow-up queue length = %d, want 1", queue.Len())
		}
		followUp, shutdown := queue.Get()
		if shutdown || followUp != request {
			t.Fatalf("follow-up Get = item=%#v shutdown=%t", followUp, shutdown)
		}
		queue.Done(followUp)
		queue.Forget(followUp)
	})

	t.Run("periodic and policy fan-out enqueue every identity", func(t *testing.T) {
		queue := newRuntimeQueue()
		source := reconciliation.NewTriggerSource(store, routes, reconciliation.Options{
			SafetyInterval:     10 * time.Millisecond,
			EnqueueBackoffBase: time.Millisecond,
			EnqueueBackoffMax:  4 * time.Millisecond,
		})
		if err := source.Start(context.Background(), queue); err != nil {
			t.Fatalf("start periodic trigger source: %v", err)
		}
		defer queue.ShutDown()
		waitForRuntimeQueue(t, queue, 2)
		if got := store.ListCalls(); got == 0 {
			t.Fatal("periodic source did not list Kubeseers")
		}
		drainRuntimeQueue(queue)
		policyHandler := reconciliation.NewPolicyHandler(tracker, routes, source)
		policy := &v1alpha1.KubeseerAccessPolicy{ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.InstallationAccessCeilingName, UID: "policy-uid"}}
		policyHandler.Create(context.Background(), event.TypedCreateEvent[*v1alpha1.KubeseerAccessPolicy]{Object: policy}, queue)
		waitForRuntimeQueue(t, queue, 2)
		if tracker.PolicyEpoch() == 0 {
			t.Fatal("policy fan-out did not advance policy epoch")
		}
	})

	t.Run("different keys retain independent freshness state", func(t *testing.T) {
		first, firstContext, firstRelease, err := tracker.Acquire(context.Background(), keyA, "uid-a", 7)
		if err != nil {
			t.Fatalf("acquire first key: %v", err)
		}
		second, secondContext, secondRelease, err := tracker.Acquire(context.Background(), keyB, "uid-b", 3)
		if err != nil {
			t.Fatalf("acquire second key: %v", err)
		}
		defer firstRelease()
		defer secondRelease()
		if !tracker.IsLeaseCurrent(first) || !tracker.IsLeaseCurrent(second) {
			t.Fatal("independent leases were not current")
		}
		if firstContext.Err() != nil || secondContext.Err() != nil {
			t.Fatal("independent lease contexts were canceled")
		}
	})

}

type runtimeTestStore struct {
	mu      sync.Mutex
	objects map[types.NamespacedName]*v1alpha1.Kubeseer
	listErr error
	gets    int
	lists   int
}

func newRuntimeTestStore(objects ...*v1alpha1.Kubeseer) *runtimeTestStore {
	store := &runtimeTestStore{objects: make(map[types.NamespacedName]*v1alpha1.Kubeseer)}
	for _, object := range objects {
		store.objects[types.NamespacedName{Namespace: object.Namespace, Name: object.Name}] = object.DeepCopy()
	}
	return store
}

func (s *runtimeTestStore) Get(_ context.Context, key types.NamespacedName, object *v1alpha1.Kubeseer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	current := s.objects[key]
	if current == nil {
		return apierrors.NewNotFound(schema.GroupResource{Group: "kubeseer.io", Resource: "kubeseers"}, key.Name)
	}
	*object = *current.DeepCopy()
	return nil
}

func (s *runtimeTestStore) List(_ context.Context, list *v1alpha1.KubeseerList) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists++
	if s.listErr != nil {
		return s.listErr
	}
	list.Items = list.Items[:0]
	for _, object := range s.objects {
		list.Items = append(list.Items, *object.DeepCopy())
	}
	return nil
}

func (s *runtimeTestStore) ListCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lists
}

func newRuntimeKubeseer(key types.NamespacedName, uid types.UID, generation int64) *v1alpha1.Kubeseer {
	return &v1alpha1.Kubeseer{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name, UID: uid, Generation: generation}}
}

type runtimeTestRoutes struct {
	mu      sync.Mutex
	started bool
	removed int
}

func (r *runtimeTestRoutes) Start(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = true
	return nil
}

func (r *runtimeTestRoutes) Replace(context.Context, reconciliation.Lease, []reconciliation.AuthorizedRoute) error {
	return nil
}

func (r *runtimeTestRoutes) RemoveOwner(types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed++
}

func (r *runtimeTestRoutes) RemoveAll() {}

func (r *runtimeTestRoutes) Started() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}

func (r *runtimeTestRoutes) Removed() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.removed
}

func newRuntimeQueue() workqueue.TypedRateLimitingInterface[reconcile.Request] {
	return workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
}

func drainRuntimeQueue(queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	for queue.Len() > 0 {
		item, shutdown := queue.Get()
		if shutdown {
			return
		}
		queue.Done(item)
		queue.Forget(item)
	}
}

func waitForRuntimeQueue(t *testing.T, queue workqueue.TypedRateLimitingInterface[reconcile.Request], want int) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if queue.Len() >= want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("queue length = %d, want at least %d", queue.Len(), want)
		case <-ticker.C:
		}
	}
}
