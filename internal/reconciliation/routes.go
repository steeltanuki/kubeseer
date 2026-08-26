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

	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// WatchAddress identifies one shared metadata-watch transport. Source IDs are
// intentionally absent: equivalent sources share this network endpoint.
type WatchAddress struct {
	GVR       schema.GroupVersionResource
	Scope     discovery.Scope
	Namespace string
}

// Validate checks the exact dimensions that distinguish cluster and
// namespaced Kubernetes endpoints.
func (a WatchAddress) Validate() error {
	if a.GVR.Version == "" || a.GVR.Resource == "" {
		return errors.New("watch address requires a group version resource")
	}
	if !a.Scope.Valid() {
		return errors.New("watch address scope is invalid")
	}
	if a.Scope == discovery.ScopeCluster {
		if a.Namespace != "" {
			return errors.New("cluster watch address cannot contain a namespace")
		}
		return nil
	}
	if len(validation.IsDNS1123Label(a.Namespace)) != 0 {
		return errors.New("namespaced watch address requires a valid namespace")
	}
	return nil
}

// AddressForTarget converts a discovered selection target into the exact
// address used by the watcher and policy dimensions.
func AddressForTarget(target selection.ReadTarget) (WatchAddress, error) {
	address := WatchAddress{GVR: target.GVR, Scope: target.Scope, Namespace: target.Namespace}
	if err := address.Validate(); err != nil {
		return WatchAddress{}, err
	}
	return address, nil
}

// RouteBinding retains the source identity and owner generation while the
// address remains shareable.
type RouteBinding struct {
	Address    WatchAddress
	SourceID   string
	Owner      types.NamespacedName
	OwnerUID   types.UID
	Generation int64
}

// AuthorizedRoute is an opaque capability created only from an exact allowed
// selection target and policy decision.
type AuthorizedRoute struct {
	binding RouteBinding
}

// NewAuthorizedRoute constructs a route only when the exact target has an
// allow decision. The returned value contains no selector or resource data.
func NewAuthorizedRoute(owner types.NamespacedName, ownerUID types.UID, generation int64, target selection.ReadTarget, decision accesspolicy.Decision) (AuthorizedRoute, error) {
	if owner.Name == "" || owner.Namespace == "" {
		return AuthorizedRoute{}, errors.New("authorized route owner is incomplete")
	}
	if target.SourceID == "" {
		return AuthorizedRoute{}, errors.New("authorized route source identity is empty")
	}
	if !decision.Allowed || decision.Reason != accesspolicy.ReasonAllowed {
		return AuthorizedRoute{}, errors.New("authorized route requires an allowed policy decision")
	}
	address, err := AddressForTarget(target)
	if err != nil {
		return AuthorizedRoute{}, err
	}
	return AuthorizedRoute{binding: RouteBinding{
		Address:    address,
		SourceID:   target.SourceID,
		Owner:      owner,
		OwnerUID:   ownerUID,
		Generation: generation,
	}}, nil
}

// NewAuthorizedRouteForLease is the lease-oriented form used by the pipeline.
func NewAuthorizedRouteForLease(lease Lease, target selection.ReadTarget, decision accesspolicy.Decision) (AuthorizedRoute, error) {
	return NewAuthorizedRoute(lease.Key, lease.UID, lease.Generation, target, decision)
}

// Binding returns an immutable copy of the route identity.
func (r AuthorizedRoute) Binding() RouteBinding { return r.binding }

// Address returns the shared watch address.
func (r AuthorizedRoute) Address() WatchAddress { return r.binding.Address }

// MetadataWatcher is the only source-watch I/O port. It never performs a
// resource-instance LIST.
type MetadataWatcher interface {
	Watch(context.Context, WatchAddress, string) (watch.Interface, error)
}

// ClientMetadataWatcher adapts client-go's metadata client to MetadataWatcher.
type ClientMetadataWatcher struct {
	client metadata.Interface
}

// NewClientMetadataWatcher constructs a metadata-only watch adapter.
func NewClientMetadataWatcher(client metadata.Interface) *ClientMetadataWatcher {
	return &ClientMetadataWatcher{client: client}
}

// Watch addresses exactly the discovered GVR and namespace. It deliberately
// supplies no label or field selector; selector membership is re-evaluated by
// the normal selection LIST path.
func (w *ClientMetadataWatcher) Watch(ctx context.Context, address WatchAddress, resourceVersion string) (watch.Interface, error) {
	if w == nil || w.client == nil {
		return nil, errors.New("metadata watcher client is not configured")
	}
	if err := address.Validate(); err != nil {
		return nil, err
	}
	options := metav1.ListOptions{
		ResourceVersion:     resourceVersion,
		AllowWatchBookmarks: true,
	}
	resource := w.client.Resource(address.GVR)
	if address.Scope == discovery.ScopeNamespaced {
		return resource.Namespace(address.Namespace).Watch(ctx, options)
	}
	return resource.Watch(ctx, options)
}

type routeRegistryOptions struct {
	watchBackoffBase time.Duration
	watchBackoffMax  time.Duration
}

// RouteRegistryOption configures watch restart timing.
type RouteRegistryOption func(*routeRegistryOptions)

// WithRouteWatchBackoff configures positive bounded watch backoff values.
func WithRouteWatchBackoff(base, maximum time.Duration) RouteRegistryOption {
	return func(options *routeRegistryOptions) {
		if base > 0 {
			options.watchBackoffBase = base
		}
		if maximum > 0 {
			options.watchBackoffMax = maximum
		}
	}
}

// RouteRegistry maintains synchronized owner and target indexes and one
// supervisor per exact authorized address.
type RouteRegistry struct {
	watcher MetadataWatcher
	tracker *FreshnessTracker
	options routeRegistryOptions

	mu       sync.RWMutex
	byOwner  map[types.NamespacedName]map[RouteBinding]struct{}
	byTarget map[WatchAddress]map[types.NamespacedName]int
	watches  map[WatchAddress]*watchSupervisor
	started  bool
	ctx      context.Context
	queue    workqueue.TypedRateLimitingInterface[reconcile.Request]
}

// NewRouteRegistry creates an identity-only route registry.
func NewRouteRegistry(watcher MetadataWatcher, tracker *FreshnessTracker, options ...RouteRegistryOption) *RouteRegistry {
	settings := routeRegistryOptions{
		watchBackoffBase: defaultWatchBackoffBase,
		watchBackoffMax:  defaultWatchBackoffMax,
	}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	if settings.watchBackoffMax < settings.watchBackoffBase {
		settings.watchBackoffMax = settings.watchBackoffBase
	}
	return &RouteRegistry{
		watcher:  watcher,
		tracker:  tracker,
		options:  settings,
		byOwner:  make(map[types.NamespacedName]map[RouteBinding]struct{}),
		byTarget: make(map[WatchAddress]map[types.NamespacedName]int),
		watches:  make(map[WatchAddress]*watchSupervisor),
	}
}

// Start binds the registry to the controller queue and starts all existing
// supervisors. It is called by TriggerSource and is intentionally separate
// from construction so invalid setup cannot start a WATCH.
func (r *RouteRegistry) Start(ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	if r == nil {
		return errors.New("route registry is nil")
	}
	if r.watcher == nil {
		return errors.New("route registry metadata watcher is required")
	}
	if queue == nil {
		return errors.New("route registry queue is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("route registry cannot be started twice")
	}
	r.started = true
	r.ctx = ctx
	r.queue = queue
	supervisors := make([]*watchSupervisor, 0, len(r.watches))
	for _, supervisor := range r.watches {
		supervisors = append(supervisors, supervisor)
	}
	r.mu.Unlock()
	for _, supervisor := range supervisors {
		supervisor.start(ctx)
	}
	return nil
}

// Replace atomically replaces all routes owned by the lease. Denied,
// obsolete, or stale bindings cannot reach the registry.
func (r *RouteRegistry) Replace(lease Lease, routes []AuthorizedRoute) error {
	if r == nil {
		return errors.New("route registry is nil")
	}
	if r.tracker != nil && !r.tracker.IsCurrent(lease) {
		return staleRuntimeError("route-replace")
	}
	newBindings := make(map[RouteBinding]struct{}, len(routes))
	for _, route := range routes {
		binding := route.binding
		if binding.Owner != lease.Key || binding.OwnerUID != lease.UID || binding.Generation != lease.Generation {
			return errors.New("authorized route does not match freshness lease")
		}
		if err := binding.Address.Validate(); err != nil {
			return err
		}
		if binding.SourceID == "" {
			return errors.New("authorized route source identity is empty")
		}
		newBindings[binding] = struct{}{}
	}

	r.mu.Lock()
	if r.tracker != nil && !r.tracker.IsCurrent(lease) {
		r.mu.Unlock()
		return staleRuntimeError("route-replace")
	}
	oldBindings := r.byOwner[lease.Key]
	toStop := make([]*watchSupervisor, 0)
	for binding := range oldBindings {
		if _, keep := newBindings[binding]; keep {
			continue
		}
		if supervisor := r.removeBindingLocked(binding); supervisor != nil {
			toStop = append(toStop, supervisor)
		}
	}
	for binding := range newBindings {
		if _, exists := oldBindings[binding]; exists {
			continue
		}
		r.addBindingLocked(binding)
	}
	if len(newBindings) == 0 {
		delete(r.byOwner, lease.Key)
	}
	started := r.started
	ctx := r.ctx
	for address := range r.byTarget {
		if _, exists := r.watches[address]; !exists {
			r.watches[address] = &watchSupervisor{registry: r, address: address, watcher: r.watcher, base: r.options.watchBackoffBase, maximum: r.options.watchBackoffMax}
		}
	}
	toStart := make([]*watchSupervisor, 0)
	if started {
		for _, supervisor := range r.watches {
			if !supervisor.isStarted() {
				toStart = append(toStart, supervisor)
			}
		}
	}
	r.mu.Unlock()
	for _, supervisor := range toStop {
		supervisor.stop()
	}
	if started {
		for _, supervisor := range toStart {
			r.startSupervisorAndWait(supervisor, ctx)
		}
	}
	return nil
}

// startSupervisorAndWait closes the small check/start race between a route
// replacement and a concurrent owner removal. The read lock keeps the
// supervisor registered while its first WATCH attempt is launched; an owner
// removal that wins first makes the check fail, while one that follows waits
// and then stops the supervisor.
func (r *RouteRegistry) startSupervisorAndWait(supervisor *watchSupervisor, parent context.Context) {
	if r == nil || supervisor == nil || parent == nil {
		return
	}
	ready := make(chan struct{})
	r.mu.RLock()
	current := r.started && r.watches[supervisor.address] == supervisor && len(r.byTarget[supervisor.address]) != 0
	if current {
		started := supervisor.startWithReady(parent, ready)
		r.mu.RUnlock()
		if !started {
			return
		}
		select {
		case <-ready:
		case <-parent.Done():
		}
		return
	}
	r.mu.RUnlock()
}

func (r *RouteRegistry) addBindingLocked(binding RouteBinding) {
	ownerBindings := r.byOwner[binding.Owner]
	if ownerBindings == nil {
		ownerBindings = make(map[RouteBinding]struct{})
		r.byOwner[binding.Owner] = ownerBindings
	}
	ownerBindings[binding] = struct{}{}
	targetOwners := r.byTarget[binding.Address]
	if targetOwners == nil {
		targetOwners = make(map[types.NamespacedName]int)
		r.byTarget[binding.Address] = targetOwners
	}
	targetOwners[binding.Owner]++
}

func (r *RouteRegistry) removeBindingLocked(binding RouteBinding) *watchSupervisor {
	ownerBindings := r.byOwner[binding.Owner]
	if ownerBindings == nil {
		return nil
	}
	delete(ownerBindings, binding)
	if len(ownerBindings) == 0 {
		delete(r.byOwner, binding.Owner)
	}
	targetOwners := r.byTarget[binding.Address]
	if targetOwners == nil {
		return nil
	}
	targetOwners[binding.Owner]--
	if targetOwners[binding.Owner] <= 0 {
		delete(targetOwners, binding.Owner)
	}
	if len(targetOwners) != 0 {
		return nil
	}
	delete(r.byTarget, binding.Address)
	supervisor := r.watches[binding.Address]
	delete(r.watches, binding.Address)
	return supervisor
}

// RemoveOwner removes all routes for one key and stops transports that no
// longer have any binding.
func (r *RouteRegistry) RemoveOwner(owner types.NamespacedName) {
	if r == nil {
		return
	}
	r.mu.Lock()
	bindings := r.byOwner[owner]
	toStop := make([]*watchSupervisor, 0)
	for binding := range bindings {
		if supervisor := r.removeBindingLocked(binding); supervisor != nil {
			toStop = append(toStop, supervisor)
		}
	}
	r.mu.Unlock()
	for _, supervisor := range toStop {
		supervisor.stop()
	}
}

// RemoveAll drops all owner and target indexes and stops every supervisor.
func (r *RouteRegistry) RemoveAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	supervisors := make([]*watchSupervisor, 0, len(r.watches))
	for _, supervisor := range r.watches {
		supervisors = append(supervisors, supervisor)
	}
	r.byOwner = make(map[types.NamespacedName]map[RouteBinding]struct{})
	r.byTarget = make(map[WatchAddress]map[types.NamespacedName]int)
	r.watches = make(map[WatchAddress]*watchSupervisor)
	r.mu.Unlock()
	for _, supervisor := range supervisors {
		supervisor.stop()
	}
}

// Owners returns sorted distinct owners currently bound to an address.
func (r *RouteRegistry) Owners(address WatchAddress) []types.NamespacedName {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	owners := make([]types.NamespacedName, 0, len(r.byTarget[address]))
	for owner := range r.byTarget[address] {
		owners = append(owners, owner)
	}
	r.mu.RUnlock()
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].Namespace != owners[j].Namespace {
			return owners[i].Namespace < owners[j].Namespace
		}
		return owners[i].Name < owners[j].Name
	})
	return owners
}

// BindingCount returns the number of source bindings for an owner.
func (r *RouteRegistry) BindingCount(owner types.NamespacedName) int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byOwner[owner])
}

// WatchCount returns the number of exact addresses with active registry state.
func (r *RouteRegistry) WatchCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.watches)
}

func (r *RouteRegistry) routeEvent(address WatchAddress) {
	if r == nil {
		return
	}
	owners := r.Owners(address)
	r.mu.RLock()
	queue := r.queue
	r.mu.RUnlock()
	for _, owner := range owners {
		if queue != nil {
			queue.Add(requestForKey(owner))
		}
	}
}

type watchSupervisor struct {
	registry *RouteRegistry
	address  WatchAddress
	watcher  MetadataWatcher
	base     time.Duration
	maximum  time.Duration

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	ready   chan struct{}
}

func (s *watchSupervisor) isStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *watchSupervisor) start(parent context.Context) {
	_ = s.startWithReady(parent, nil)
}

func (s *watchSupervisor) startAndWait(parent context.Context) {
	if s == nil || parent == nil {
		return
	}
	ready := make(chan struct{})
	if !s.startWithReady(parent, ready) {
		return
	}
	select {
	case <-ready:
	case <-parent.Done():
	}
}

func (s *watchSupervisor) startWithReady(parent context.Context, ready chan struct{}) bool {
	if s == nil || s.watcher == nil || parent == nil {
		return false
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(parent)
	s.started = true
	s.cancel = cancel
	s.ready = ready
	s.mu.Unlock()
	go s.run(ctx)
	return true
}

func (s *watchSupervisor) stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.started = false
	ready := s.ready
	s.ready = nil
	s.mu.Unlock()
	if ready != nil {
		close(ready)
	}
	if cancel != nil {
		cancel()
	}
}

func (s *watchSupervisor) run(ctx context.Context) {
	resourceVersion := ""
	backoff := s.base
	firstAttempt := true
	for {
		if ctx.Err() != nil {
			return
		}
		stream, err := s.watcher.Watch(ctx, s.address, resourceVersion)
		if firstAttempt {
			s.mu.Lock()
			ready := s.ready
			s.ready = nil
			s.mu.Unlock()
			if ready != nil {
				close(ready)
			}
			firstAttempt = false
		}
		if err == nil && stream != nil {
			err = s.consume(ctx, stream, &resourceVersion)
			stream.Stop()
		} else if err == nil {
			err = errors.New("metadata watcher returned no stream")
		}
		if ctx.Err() != nil {
			return
		}
		if isExpiredWatchError(err) {
			resourceVersion = ""
		}
		if !waitFor(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, s.maximum)
	}
}

func (s *watchSupervisor) consume(ctx context.Context, stream watch.Interface, resourceVersion *string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, open := <-stream.ResultChan():
			if !open {
				return nil
			}
			if version := objectResourceVersion(event.Object); version != "" {
				*resourceVersion = version
			}
			switch event.Type {
			case watch.Added, watch.Modified, watch.Deleted:
				// The event object is intentionally not inspected or retained.
				s.registry.routeEvent(s.address)
			case watch.Bookmark:
				// A bookmark advances the resume token only.
			case watch.Error:
				return watchEventError(event.Object)
			}
		}
	}
}

func objectResourceVersion(object runtime.Object) string {
	if object == nil {
		return ""
	}
	accessor, err := meta.Accessor(object)
	if err != nil {
		return ""
	}
	return accessor.GetResourceVersion()
}

func watchEventError(object runtime.Object) error {
	if object == nil {
		return errors.New("metadata watch returned an unspecified error")
	}
	if status, ok := object.(*metav1.Status); ok {
		if status.Reason != "" {
			return fmt.Errorf("metadata watch returned %s", status.Reason)
		}
		return errors.New("metadata watch returned an API error")
	}
	return errors.New("metadata watch returned an API error")
}

func isExpiredWatchError(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
		return true
	}
	status, ok := err.(apierrors.APIStatus)
	return ok && status.Status().Reason == metav1.StatusReasonExpired
}
