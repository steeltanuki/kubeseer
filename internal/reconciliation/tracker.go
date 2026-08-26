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
	"errors"
	"sync"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

type activeLease struct {
	lease  Lease
	cancel context.CancelFunc
}

type trackedState struct {
	uid         types.UID
	generation  int64
	deleting    bool
	absent      bool
	policyEpoch uint64
	active      *activeLease
}

// FreshnessTracker owns process-local lifecycle and policy invalidation state.
// It never stores resource contents or extracted values.
type FreshnessTracker struct {
	mu     sync.RWMutex
	epoch  uint64
	states map[types.NamespacedName]*trackedState
}

// NewFreshnessTracker creates an empty tracker.
func NewFreshnessTracker() *FreshnessTracker {
	return &FreshnessTracker{states: make(map[types.NamespacedName]*trackedState)}
}

func (t *FreshnessTracker) ensureState(key types.NamespacedName) *trackedState {
	if t.states == nil {
		t.states = make(map[types.NamespacedName]*trackedState)
	}
	state := t.states[key]
	if state == nil {
		state = &trackedState{policyEpoch: t.epoch}
		t.states[key] = state
	}
	return state
}

func changedIdentity(state *trackedState, uid types.UID, generation int64, deleting, absent bool) bool {
	if state == nil {
		return true
	}
	return state.uid != uid || state.generation != generation || state.deleting != deleting || state.absent != absent
}

func cancelActive(state *trackedState) {
	if state != nil && state.active != nil && state.active.cancel != nil {
		state.active.cancel()
		state.active = nil
	}
}

// Observe records the current lifecycle identity and cancels an active lease
// when a UID, generation, or deletion state changes.
func (t *FreshnessTracker) Observe(object *v1alpha1.Kubeseer) {
	if t == nil || object == nil {
		return
	}
	key := types.NamespacedName{Namespace: object.Namespace, Name: object.Name}
	deleting := object.DeletionTimestamp != nil
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.ensureState(key)
	if changedIdentity(state, object.UID, object.Generation, deleting, false) {
		cancelActive(state)
	}
	state.uid = object.UID
	state.generation = object.Generation
	state.deleting = deleting
	state.absent = false
	state.policyEpoch = t.epoch
}

// ObserveDelete records an observed deletion and cancels active work for the
// key. A UID supplied by a tombstone is retained only as identity metadata.
func (t *FreshnessTracker) ObserveDelete(key types.NamespacedName, uid types.UID) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.ensureState(key)
	cancelActive(state)
	if uid != "" {
		state.uid = uid
	}
	state.deleting = true
	state.absent = true
	state.policyEpoch = t.epoch
}

// MarkAbsent records a NotFound reconciliation. It is idempotent and removes
// any active lease without persisting a finalizer or object data.
func (t *FreshnessTracker) MarkAbsent(key types.NamespacedName) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.ensureState(key)
	cancelActive(state)
	state.absent = true
	state.deleting = true
	state.policyEpoch = t.epoch
}

// InvalidateAll advances the process-local policy epoch and cancels all
// active leases. Every following reconciliation still loads a fresh policy
// through accesspolicy.Load.
func (t *FreshnessTracker) InvalidateAll() uint64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.epoch++
	for _, state := range t.states {
		cancelActive(state)
		state.policyEpoch = t.epoch
	}
	return t.epoch
}

// PolicyEpoch returns the current process-local policy invalidation epoch.
func (t *FreshnessTracker) PolicyEpoch() uint64 {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.epoch
}

// Acquire creates a cancellable child context and an identity-bound lease.
// Callers must invoke the returned release function when the reconciliation
// finishes. A second same-key caller is rejected; controller-runtime normally
// prevents this through its work queue, while the guard also protects direct
// composition tests.
func (t *FreshnessTracker) Acquire(ctx context.Context, key types.NamespacedName, uid types.UID, generation int64) (Lease, context.Context, func(), error) {
	if t == nil {
		return Lease{}, nil, nil, errors.New("freshness tracker is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.ensureState(key)
	if state.active != nil {
		return Lease{}, nil, nil, errors.New("Kubeseer key already has active reconciliation")
	}
	state.uid = uid
	state.generation = generation
	state.deleting = false
	state.absent = false
	state.policyEpoch = t.epoch
	lease := Lease{Key: key, UID: uid, Generation: generation, PolicyEpoch: t.epoch}
	child, cancel := context.WithCancel(ctx)
	state.active = &activeLease{lease: lease, cancel: cancel}
	released := false
	release := func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if released {
			return
		}
		released = true
		current := t.states[key]
		if current != nil && current.active != nil && current.active.lease == lease {
			current.active = nil
		}
		cancel()
	}
	return lease, child, release, nil
}

// IsCurrent reports whether a lease still owns the tracked identity and policy
// epoch. It is safe to call from concurrent event handlers and publishers.
func (t *FreshnessTracker) IsCurrent(lease Lease) bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	state := t.states[lease.Key]
	return state != nil && state.uid == lease.UID && state.generation == lease.Generation && !state.deleting && !state.absent && state.policyEpoch == lease.PolicyEpoch && t.epoch == lease.PolicyEpoch
}

// TrackedState is a read-only lifecycle snapshot useful for diagnostics and
// deterministic higher-layer tests.
type TrackedState struct {
	UID         types.UID
	Generation  int64
	Deleting    bool
	Absent      bool
	PolicyEpoch uint64
	Active      bool
}

// State returns a copy of the identity-only tracker state.
func (t *FreshnessTracker) State(key types.NamespacedName) (TrackedState, bool) {
	if t == nil {
		return TrackedState{}, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	state := t.states[key]
	if state == nil {
		return TrackedState{}, false
	}
	return TrackedState{
		UID:         state.uid,
		Generation:  state.generation,
		Deleting:    state.deleting,
		Absent:      state.absent,
		PolicyEpoch: state.policyEpoch,
		Active:      state.active != nil,
	}, true
}

type executionGate struct {
	mu    sync.Mutex
	locks map[types.NamespacedName]*sync.Mutex
}

func newExecutionGate() *executionGate {
	return &executionGate{locks: make(map[types.NamespacedName]*sync.Mutex)}
}

func (g *executionGate) lock(key types.NamespacedName) func() {
	g.mu.Lock()
	lock := g.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		g.locks[key] = lock
	}
	g.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}
