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
	"sync"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
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

	mu        sync.Mutex
	queue     workqueue.TypedRateLimitingInterface[reconcile.Request]
	ctx       context.Context
	started   bool
	running   bool
	requested bool
}

var _ interface {
	Start(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error
} = (*TriggerSource)(nil)

// NewTriggerSource creates a non-started periodic trigger source.
func NewTriggerSource(reader KubeseerLister, routes RouteManager, options Options) *TriggerSource {
	return &TriggerSource{reader: reader, routes: routes, options: options}
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
	if _, err := s.options.normalized(); err != nil {
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
	s.queue = queue
	s.ctx = ctx
	requested := s.requested
	s.mu.Unlock()

	if starter, ok := s.routes.(interface {
		Start(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error
	}); ok {
		if err := starter.Start(ctx, queue); err != nil {
			return fmt.Errorf("start authorized route registry: %w", err)
		}
	}

	go s.schedule(ctx)
	// A startup enqueue closes the window between cache startup and the first
	// periodic tick while remaining safe when cache add events also enqueue the
	// same key.
	s.RequestEnqueueAll(ctx)
	if requested {
		s.startEnqueueWorker()
	}
	return nil
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
		queue.Add(requestForKey(typesNamespacedName(object.Namespace, object.Name)))
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
