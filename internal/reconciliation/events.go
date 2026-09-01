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

package reconciliation

import (
	"context"
	"sync"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func requestForKey(key types.NamespacedName) reconcile.Request {
	return reconcile.Request{NamespacedName: key}
}

func enqueueObject(queue workqueue.TypedRateLimitingInterface[reconcile.Request], namespace, name string) {
	if queue != nil && name != "" {
		queue.Add(requestForKey(types.NamespacedName{Namespace: namespace, Name: name}))
	}
}

// KubeseerPredicate accepts lifecycle events that can change work safety and
// suppresses status-only updates.
type KubeseerPredicate struct{}

func (KubeseerPredicate) Create(e event.TypedCreateEvent[*v1alpha1.Kubeseer]) bool {
	return e.Object != nil
}

func (KubeseerPredicate) Delete(e event.TypedDeleteEvent[*v1alpha1.Kubeseer]) bool {
	return e.Object != nil
}

func (KubeseerPredicate) Generic(e event.TypedGenericEvent[*v1alpha1.Kubeseer]) bool {
	return e.Object != nil
}

func (KubeseerPredicate) Update(e event.TypedUpdateEvent[*v1alpha1.Kubeseer]) bool {
	return kubeseerLifecycleChanged(e.ObjectOld, e.ObjectNew)
}

func kubeseerLifecycleChanged(oldObject, newObject *v1alpha1.Kubeseer) bool {
	if oldObject == nil || newObject == nil {
		return oldObject != newObject
	}
	if oldObject.UID != newObject.UID || oldObject.Generation != newObject.Generation {
		return true
	}
	return oldObject.DeletionTimestamp == nil && newObject.DeletionTimestamp != nil
}

// LifecycleHandler updates freshness before putting a key on the shared
// queue. It removes routes immediately on deletion safety transitions.
type LifecycleHandler struct {
	tracker *FreshnessTracker
	routes  RouteManager
	trigger *TriggerSource
}

var _ handler.TypedEventHandler[*v1alpha1.Kubeseer, reconcile.Request] = (*LifecycleHandler)(nil)

// NewLifecycleHandler creates the owned-resource event handler.
func NewLifecycleHandler(tracker *FreshnessTracker, routes RouteManager, trigger ...*TriggerSource) *LifecycleHandler {
	var ingress *TriggerSource
	if len(trigger) != 0 {
		ingress = trigger[0]
	}
	return &LifecycleHandler{tracker: tracker, routes: routes, trigger: ingress}
}

func (h *LifecycleHandler) enqueue(key types.NamespacedName, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if h != nil && h.trigger != nil {
		h.trigger.Enqueue(key)
		return
	}
	enqueueObject(queue, key.Namespace, key.Name)
}

func (h *LifecycleHandler) Create(_ context.Context, e event.TypedCreateEvent[*v1alpha1.Kubeseer], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if h == nil || e.Object == nil {
		return
	}
	if h.tracker != nil {
		h.tracker.Observe(e.Object)
	}
	if e.Object.DeletionTimestamp != nil && h.routes != nil {
		h.routes.RemoveOwner(types.NamespacedName{Namespace: e.Object.Namespace, Name: e.Object.Name})
	}
	h.enqueue(types.NamespacedName{Namespace: e.Object.Namespace, Name: e.Object.Name}, queue)
}

func (h *LifecycleHandler) Update(_ context.Context, e event.TypedUpdateEvent[*v1alpha1.Kubeseer], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if h == nil || !kubeseerLifecycleChanged(e.ObjectOld, e.ObjectNew) || e.ObjectNew == nil {
		return
	}
	key := types.NamespacedName{Namespace: e.ObjectNew.Namespace, Name: e.ObjectNew.Name}
	if h.tracker != nil {
		h.tracker.Observe(e.ObjectNew)
	}
	if h.routes != nil && (e.ObjectNew.DeletionTimestamp != nil || (e.ObjectOld != nil && e.ObjectOld.UID != e.ObjectNew.UID)) {
		h.routes.RemoveOwner(key)
	}
	h.enqueue(key, queue)
}

func (h *LifecycleHandler) Delete(_ context.Context, e event.TypedDeleteEvent[*v1alpha1.Kubeseer], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if h == nil || e.Object == nil {
		return
	}
	key := types.NamespacedName{Namespace: e.Object.Namespace, Name: e.Object.Name}
	if h.tracker != nil {
		h.tracker.ObserveDelete(key, e.Object.UID)
	}
	if h.routes != nil {
		h.routes.RemoveOwner(key)
	}
	h.enqueue(key, queue)
}

func (h *LifecycleHandler) Generic(_ context.Context, e event.TypedGenericEvent[*v1alpha1.Kubeseer], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if h == nil || e.Object == nil {
		return
	}
	if h.tracker != nil {
		h.tracker.Observe(e.Object)
	}
	h.enqueue(types.NamespacedName{Namespace: e.Object.Namespace, Name: e.Object.Name}, queue)
}

// PolicyPredicate selects only the administrative singleton and ignores
// status-like or metadata-only updates without a new generation.
type PolicyPredicate struct{}

func (PolicyPredicate) Create(e event.TypedCreateEvent[*v1alpha1.KubeseerAccessPolicy]) bool {
	return e.Object != nil && e.Object.Name == v1alpha1.InstallationAccessCeilingName
}

func (PolicyPredicate) Delete(e event.TypedDeleteEvent[*v1alpha1.KubeseerAccessPolicy]) bool {
	return e.Object != nil && e.Object.Name == v1alpha1.InstallationAccessCeilingName
}

func (PolicyPredicate) Generic(e event.TypedGenericEvent[*v1alpha1.KubeseerAccessPolicy]) bool {
	return e.Object != nil && e.Object.Name == v1alpha1.InstallationAccessCeilingName
}

func (PolicyPredicate) Update(e event.TypedUpdateEvent[*v1alpha1.KubeseerAccessPolicy]) bool {
	if e.ObjectNew == nil || e.ObjectNew.Name != v1alpha1.InstallationAccessCeilingName {
		return false
	}
	if e.ObjectOld == nil {
		return true
	}
	return e.ObjectOld.UID != e.ObjectNew.UID || e.ObjectOld.Generation != e.ObjectNew.Generation || e.ObjectOld.DeletionTimestamp == nil && e.ObjectNew.DeletionTimestamp != nil
}

// PolicyHandler invalidates active leases, drops old routing, and asks the
// trigger source to enqueue every existing Kubeseer.
type PolicyHandler struct {
	tracker *FreshnessTracker
	routes  RouteManager
	trigger *TriggerSource
	mu      sync.Mutex
}

var _ handler.TypedEventHandler[*v1alpha1.KubeseerAccessPolicy, reconcile.Request] = (*PolicyHandler)(nil)

// NewPolicyHandler creates the singleton policy event handler.
func NewPolicyHandler(tracker *FreshnessTracker, routes RouteManager, trigger *TriggerSource) *PolicyHandler {
	return &PolicyHandler{tracker: tracker, routes: routes, trigger: trigger}
}

func (h *PolicyHandler) changed(ctx context.Context, object *v1alpha1.KubeseerAccessPolicy) {
	if h == nil || object == nil || object.Name != v1alpha1.InstallationAccessCeilingName {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tracker != nil {
		h.tracker.InvalidateAll()
	}
	if h.routes != nil {
		h.routes.RemoveAll()
	}
	if h.trigger != nil {
		h.trigger.RequestEnqueueAll(ctx)
	}
}

func (h *PolicyHandler) Create(ctx context.Context, e event.TypedCreateEvent[*v1alpha1.KubeseerAccessPolicy], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.changed(ctx, e.Object)
}

func (h *PolicyHandler) Update(ctx context.Context, e event.TypedUpdateEvent[*v1alpha1.KubeseerAccessPolicy], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if !(PolicyPredicate{}).Update(e) {
		return
	}
	h.changed(ctx, e.ObjectNew)
}

func (h *PolicyHandler) Delete(ctx context.Context, e event.TypedDeleteEvent[*v1alpha1.KubeseerAccessPolicy], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.changed(ctx, e.Object)
}

func (h *PolicyHandler) Generic(ctx context.Context, e event.TypedGenericEvent[*v1alpha1.KubeseerAccessPolicy], _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.changed(ctx, e.Object)
}

var _ predicate.TypedPredicate[*v1alpha1.Kubeseer] = KubeseerPredicate{}
var _ predicate.TypedPredicate[*v1alpha1.KubeseerAccessPolicy] = PolicyPredicate{}
