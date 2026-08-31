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
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/observability"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultWatchBackoffBase   = 100 * time.Millisecond
	defaultWatchBackoffMax    = 5 * time.Second
	defaultEnqueueBackoffBase = 100 * time.Millisecond
	defaultEnqueueBackoffMax  = 5 * time.Second
)

// Options controls internal runtime timing. SafetyInterval is intentionally
// not a public Kubeseer API field; it belongs to manager configuration.
type Options struct {
	SafetyInterval time.Duration

	// TraceProvider is the optional manager-wide OpenTelemetry provider. All
	// other observability facilities come from the owning manager at setup
	// time; a nil provider keeps direct construction tracing-free.
	TraceProvider trace.TracerProvider

	// The backoff knobs are useful for deterministic integration fixtures and
	// retain bounded production defaults. They do not affect reconciliation
	// result semantics.
	WatchBackoffBase   time.Duration
	WatchBackoffMax    time.Duration
	EnqueueBackoffBase time.Duration
	EnqueueBackoffMax  time.Duration
}

func (o Options) normalized() (Options, error) {
	if o.SafetyInterval <= 0 {
		return Options{}, errors.New("reconciliation safety interval must be positive")
	}
	if o.WatchBackoffBase <= 0 {
		o.WatchBackoffBase = defaultWatchBackoffBase
	}
	if o.WatchBackoffMax <= 0 {
		o.WatchBackoffMax = defaultWatchBackoffMax
	}
	if o.WatchBackoffMax < o.WatchBackoffBase {
		return Options{}, errors.New("watch backoff maximum must be at least its base")
	}
	if o.EnqueueBackoffBase <= 0 {
		o.EnqueueBackoffBase = defaultEnqueueBackoffBase
	}
	if o.EnqueueBackoffMax <= 0 {
		o.EnqueueBackoffMax = defaultEnqueueBackoffMax
	}
	if o.EnqueueBackoffMax < o.EnqueueBackoffBase {
		return Options{}, errors.New("enqueue backoff maximum must be at least its base")
	}
	return o, nil
}

// KubeseerReader is the direct current-object read port used by reconciliation
// and status publication. It cannot read arbitrary observed resources.
type KubeseerReader interface {
	Get(context.Context, types.NamespacedName, *v1alpha1.Kubeseer) error
}

// KubeseerLister is the narrow list port used only by enqueue-all scheduling.
type KubeseerLister interface {
	List(context.Context, *v1alpha1.KubeseerList) error
}

// KubeseerStore is the combined port normally supplied by a controller-runtime
// client adapter.
type KubeseerStore interface {
	KubeseerReader
	KubeseerLister
}

// StatusWriter is limited to the Kubeseer status subresource.
type StatusWriter interface {
	Update(context.Context, *v1alpha1.Kubeseer) error
}

// StatusPublisherPort publishes a candidate for one current lease.
type StatusPublisherPort interface {
	Publish(context.Context, Lease, statuscontract.Evaluation) error
}

// RouteManager owns only authorized source-event routing state.
type RouteManager interface {
	Replace(Lease, []AuthorizedRoute) error
	RemoveOwner(types.NamespacedName)
	RemoveAll()
}

// Dependencies are the production-module and Kubernetes ports required by a
// Runtime. Every field is deliberately explicit so tests can replace only
// outbound I/O while retaining the real discovery, policy, selection,
// extraction, and typed-output implementations.
type Dependencies struct {
	Reader       KubeseerReader
	Lister       KubeseerLister
	PolicySource accesspolicy.PolicySource
	Enforcer     *authorization.Enforcer
	Planner      *selection.Planner
	Executor     *selection.Executor
	Routes       RouteManager
	Publisher    StatusPublisherPort
	Tracker      *FreshnessTracker
	Trigger      *TriggerSource
	Observer     *observability.Observer
}

// Lease is a process-local freshness capability. It is valid only for the
// exact Kubeseer identity, generation, and policy epoch captured at acquire
// time.
type Lease struct {
	Key         types.NamespacedName
	UID         types.UID
	Generation  int64
	PolicyEpoch uint64
}

// Candidate is the internal result handed to publication. Retryable is kept
// separate from the structural result so a sanitized partial result can be
// published before the queue receives one rate-limited retry error.
type Candidate struct {
	Lease     Lease
	Result    v1alpha1.KubeseerResult
	Retryable error
}

// Runtime is the controller-runtime reconciler and its event-source helpers.
type Runtime struct {
	options Options
	deps    Dependencies
	trigger *TriggerSource
	gate    *executionGate
}

// NewRuntime validates and constructs the internal composition root. It does
// not register a controller or start a watch; SetupWithManager performs those
// lifecycle actions after all dependencies have been validated.
func NewRuntime(options Options, dependencies Dependencies) (*Runtime, error) {
	normalized, err := options.normalized()
	if err != nil {
		return nil, err
	}
	if dependencies.Reader == nil {
		return nil, errors.New("reconciliation Kubeseer reader is required")
	}
	if dependencies.Lister == nil {
		return nil, errors.New("reconciliation Kubeseer lister is required")
	}
	if dependencies.PolicySource == nil {
		return nil, errors.New("reconciliation policy source is required")
	}
	if dependencies.Enforcer == nil {
		return nil, errors.New("reconciliation authorization enforcer is required")
	}
	if dependencies.Planner == nil {
		return nil, errors.New("reconciliation selection planner is required")
	}
	if dependencies.Executor == nil {
		return nil, errors.New("reconciliation selection executor is required")
	}
	if dependencies.Routes == nil {
		return nil, errors.New("reconciliation route manager is required")
	}
	if dependencies.Publisher == nil {
		return nil, errors.New("reconciliation status publisher is required")
	}
	if dependencies.Tracker == nil {
		dependencies.Tracker = NewFreshnessTracker()
	}
	if dependencies.Observer == nil {
		dependencies.Observer = observability.NewNoop()
	}
	trigger := dependencies.Trigger
	if trigger == nil {
		trigger = NewTriggerSource(dependencies.Lister, dependencies.Routes, normalized)
	}
	return &Runtime{
		options: normalized,
		deps:    dependencies,
		trigger: trigger,
		gate:    newExecutionGate(),
	}, nil
}

// NewRuntimeWithDependencies is an argument-order-friendly alias for callers
// that group ports before timing options.
func NewRuntimeWithDependencies(dependencies Dependencies, options Options) (*Runtime, error) {
	return NewRuntime(options, dependencies)
}

// TriggerSource returns the custom source owned by this runtime.
func (r *Runtime) TriggerSource() *TriggerSource {
	if r == nil {
		return nil
	}
	return r.trigger
}

// LifecycleHandler returns the typed Kubeseer event handler for registration.
func (r *Runtime) LifecycleHandler() *LifecycleHandler {
	if r == nil {
		return nil
	}
	return NewLifecycleHandler(r.deps.Tracker, r.deps.Routes)
}

// PolicyHandler returns the typed installation-policy event handler.
func (r *Runtime) PolicyHandler() *PolicyHandler {
	if r == nil {
		return nil
	}
	return NewPolicyHandler(r.deps.Tracker, r.deps.Routes, r.trigger)
}

// KubeseerPredicate returns the lifecycle predicate used by SetupWithManager.
func (r *Runtime) KubeseerPredicate() KubeseerPredicate { return KubeseerPredicate{} }

// PolicyPredicate returns the singleton policy predicate used by setup.
func (r *Runtime) PolicyPredicate() PolicyPredicate { return PolicyPredicate{} }

// ClientKubeseerStore adapts a controller-runtime reader to the narrow
// Kubeseer ports. It is safe to use with APIReader for fresh security-sensitive
// reads and enqueue-all listing.
type ClientKubeseerStore struct {
	reader client.Reader
}

// NewClientKubeseerStore constructs a narrow adapter around client.Reader.
func NewClientKubeseerStore(reader client.Reader) *ClientKubeseerStore {
	return &ClientKubeseerStore{reader: reader}
}

// Get retrieves one current Kubeseer by namespace/name.
func (s *ClientKubeseerStore) Get(ctx context.Context, key types.NamespacedName, object *v1alpha1.Kubeseer) error {
	if s == nil || s.reader == nil {
		return errors.New("Kubeseer reader is not configured")
	}
	if object == nil {
		return errors.New("Kubeseer read destination is nil")
	}
	return s.reader.Get(ctx, client.ObjectKey(key), object)
}

// List lists all Kubeseers through the supplied reader. The caller consumes
// only identity metadata and drops the returned objects after enqueueing.
func (s *ClientKubeseerStore) List(ctx context.Context, list *v1alpha1.KubeseerList) error {
	if s == nil || s.reader == nil {
		return errors.New("Kubeseer lister is not configured")
	}
	if list == nil {
		return errors.New("Kubeseer list destination is nil")
	}
	return s.reader.List(ctx, list)
}

// ClientStatusWriter adapts a controller-runtime status subresource writer.
type ClientStatusWriter struct {
	writer client.SubResourceWriter
}

// NewClientStatusWriter constructs a status-only adapter.
func NewClientStatusWriter(writer client.SubResourceWriter) *ClientStatusWriter {
	return &ClientStatusWriter{writer: writer}
}

// Update delegates only to the status subresource.
func (w *ClientStatusWriter) Update(ctx context.Context, object *v1alpha1.Kubeseer) error {
	if w == nil || w.writer == nil {
		return errors.New("Kubeseer status writer is not configured")
	}
	if object == nil {
		return errors.New("Kubeseer status destination is nil")
	}
	return w.writer.Update(ctx, object)
}

// StatusPublisherFunc adapts a function to StatusPublisherPort.
type StatusPublisherFunc func(context.Context, Lease, statuscontract.Evaluation) error

// Publish implements StatusPublisherPort.
func (f StatusPublisherFunc) Publish(ctx context.Context, lease Lease, evaluation statuscontract.Evaluation) error {
	if f == nil {
		return fmt.Errorf("status publisher function is nil")
	}
	return f(ctx, lease, evaluation)
}
