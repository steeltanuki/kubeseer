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
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TriggerSource is the single custom controller-runtime source for periodic
// safety scheduling and policy-wide enqueue intents.
type TriggerSource struct {
	reader  KubeseerLister
	routes  RouteManager
	options Options

	mu              sync.Mutex
	queue           workqueue.TypedRateLimitingInterface[reconcile.Request]
	ctx             context.Context
	started         bool
	running         bool
	requested       bool
	pendingCapacity int
	pending         map[types.NamespacedName]struct{}
	wake            chan struct{}
	overloaded      bool
}

// TriggerIngress is the identity-only handoff between source events and the
// controller queue. Implementations must return without waiting on queue or
// Kubernetes I/O.
type TriggerIngress interface {
	Enqueue(types.NamespacedName)
}

var _ interface {
	Start(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error
} = (*TriggerSource)(nil)

// NewTriggerSource creates a non-started periodic trigger source.
func NewTriggerSource(reader KubeseerLister, routes RouteManager, options Options) *TriggerSource {
	profile := limits.DefaultProfile()
	if options.LimitProfile != nil && options.LimitProfile.Valid() {
		profile = *options.LimitProfile
	}
	return &TriggerSource{
		reader:          reader,
		routes:          routes,
		options:         options,
		pendingCapacity: profile.MaxPendingTriggers(),
		pending:         make(map[types.NamespacedName]struct{}),
		wake:            make(chan struct{}, 1),
	}
}

// String provides a stable controller-runtime source description.
func (s *TriggerSource) String() string {
	return fmt.Sprintf("reconciliation trigger source: %p", s)
}

// Start binds the source to one controller queue and returns immediately. The
// scheduler and enqueue-all worker are canceled by the controller context.
func (s *TriggerSource) Start(ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	if s == nil {
		return errors.New("reconciliation trigger source is nil")
	}
	normalized, err := s.options.normalized()
	if err != nil {
		return err
	}
	if s.reader == nil {
		return errors.New("reconciliation trigger source requires a Kubeseer lister")
	}
	if s.routes == nil {
		return errors.New("reconciliation trigger source requires a route manager")
	}
	if queue == nil {
		return errors.New("reconciliation trigger source requires a work queue")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("reconciliation trigger source cannot be started twice")
	}
	s.started = true
	s.options = normalized
	if s.pendingCapacity <= 0 {
		s.pendingCapacity = limits.DefaultMaxPendingTriggers
	}
	if s.pending == nil {
		s.pending = make(map[types.NamespacedName]struct{})
	}
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	s.queue = queue
	s.ctx = ctx
	requested := s.requested
	s.mu.Unlock()

	if setter, ok := s.routes.(interface{ SetTriggerIngress(TriggerIngress) }); ok {
		setter.SetTriggerIngress(s)
	}
	if starter, ok := s.routes.(interface {
		Start(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error
	}); ok {
		if err := starter.Start(ctx, queue); err != nil {
			return fmt.Errorf("start authorized route registry: %w", err)
		}
	}

	go s.schedule(ctx)
	go s.drainPending(ctx)
	// A startup enqueue closes the window between cache startup and the first
	// periodic tick while remaining safe when cache add events also enqueue the
	// same key.
	s.RequestEnqueueAll(ctx)
	if requested {
		s.startEnqueueWorker()
	}
	return nil
}

// Enqueue accepts one identity-only trigger. Duplicate identities coalesce;
// once the bounded pending set is full, the producer records one recovery
// intent and signals the non-blocking drain path.
func (s *TriggerSource) Enqueue(key types.NamespacedName) {
	if s == nil || key.Name == "" {
		return
	}
	s.mu.Lock()
	if s.pendingCapacity <= 0 {
		s.pendingCapacity = limits.DefaultMaxPendingTriggers
	}
	if s.pending == nil {
		s.pending = make(map[types.NamespacedName]struct{})
	}
	if _, exists := s.pending[key]; exists {
		s.mu.Unlock()
		return
	}
	if len(s.pending) >= s.pendingCapacity {
		s.overloaded = true
		s.signalWakeLocked()
		s.mu.Unlock()
		return
	}
	s.pending[key] = struct{}{}
	s.signalWakeLocked()
	s.mu.Unlock()
}

func (s *TriggerSource) signalWakeLocked() {
	if s.wake == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *TriggerSource) drainPending(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}

		s.mu.Lock()
		keys := make([]types.NamespacedName, 0, len(s.pending))
		for key := range s.pending {
			keys = append(keys, key)
			delete(s.pending, key)
		}
		overloaded := s.overloaded
		s.overloaded = false
		queue := s.queue
		s.mu.Unlock()

		sort.Slice(keys, func(i, j int) bool {
			if keys[i].Namespace != keys[j].Namespace {
				return keys[i].Namespace < keys[j].Namespace
			}
			return keys[i].Name < keys[j].Name
		})
		for _, key := range keys {
			if queue != nil {
				queue.Add(requestForKey(key))
			}
		}
		if overloaded {
			s.RequestEnqueueAll(ctx)
		}
	}
}

// PendingCount returns the number of distinct identity triggers waiting in
// the bounded ingress. It is intended for diagnostics and integration checks.
func (s *TriggerSource) PendingCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// RecoveryPending reports whether ingress overflow or policy-wide recovery is
// waiting for the enqueue-all path.
func (s *TriggerSource) RecoveryPending() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overloaded || s.requested
}

func (s *TriggerSource) schedule(ctx context.Context) {
	ticker := time.NewTicker(s.options.SafetyInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RequestEnqueueAll(ctx)
		}
	}
}

// RequestEnqueueAll records one coalesced enqueue-all intent. It is safe to
// call from policy handlers and source supervisors while another list is in
// flight.
func (s *TriggerSource) RequestEnqueueAll(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.requested = true
	started := s.started
	s.mu.Unlock()
	if started {
		s.startEnqueueWorker()
	}
	_ = ctx // the source lifecycle context owns cancellation and retry scope.
}

func (s *TriggerSource) startEnqueueWorker() {
	s.mu.Lock()
	if !s.started || s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	ctx := s.ctx
	s.mu.Unlock()
	go s.drainEnqueueIntents(ctx)
}

func (s *TriggerSource) drainEnqueueIntents(ctx context.Context) {
	backoff := s.options.EnqueueBackoffBase
	for {
		s.mu.Lock()
		if !s.requested {
			s.running = false
			s.mu.Unlock()
			return
		}
		s.requested = false
		queue := s.queue
		s.mu.Unlock()

		err := s.enqueueAll(ctx, queue)
		if err != nil {
			if ctx == nil || ctx.Err() != nil {
				s.mu.Lock()
				s.running = false
				s.mu.Unlock()
				return
			}
			if !waitFor(ctx, backoff) {
				s.mu.Lock()
				s.running = false
				s.mu.Unlock()
				return
			}
			backoff = nextBackoff(backoff, s.options.EnqueueBackoffMax)
			s.mu.Lock()
			s.requested = true
			s.mu.Unlock()
			continue
		}
		backoff = s.options.EnqueueBackoffBase
	}
}

func (s *TriggerSource) enqueueAll(ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	if queue == nil {
		return errors.New("reconciliation trigger queue is unavailable")
	}
	list := newKubeseerList()
	if err := s.reader.List(ctx, list); err != nil {
		return transientRuntimeError("enqueue-all", "", ReasonReadUnavailable, "Kubeseer listing is unavailable", err)
	}
	seen := make(map[string]struct{}, len(list.Items))
	keys := make([]types.NamespacedName, 0, len(list.Items))
	for index := range list.Items {
		object := &list.Items[index]
		key := object.Namespace + "\x00" + object.Name
		if object.Name == "" || object.Namespace == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, typesNamespacedName(object.Namespace, object.Name))
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Namespace != keys[j].Namespace {
			return keys[i].Namespace < keys[j].Namespace
		}
		return keys[i].Name < keys[j].Name
	})
	for _, key := range keys {
		queue.Add(requestForKey(key))
	}
	return nil
}

// newKubeseerList is kept in this file to make it clear that enqueue-all
// consumes identity metadata only; the list is dropped after the loop.
func newKubeseerList() *v1alpha1.KubeseerList { return &v1alpha1.KubeseerList{} }

func waitFor(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return true
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	if current <= 0 {
		return maximum
	}
	if current >= maximum/2 || current > maximum-current {
		return maximum
	}
	return current * 2
}

// typesNamespacedName avoids exposing a mutable list item beyond the enqueue
// operation and keeps all queue values identity-only.
func typesNamespacedName(namespace, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: namespace, Name: name}
}
