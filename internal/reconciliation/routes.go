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
	"sort"
	"sync"
	"time"

	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/observability"
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

// RouteBinding retains the source identity, full authorization subject, and
// owner generation while the address remains shareable. The capability is
// private and can only be installed through NewAuthorizedRoute.
type RouteBinding struct {
	Address    WatchAddress
	SourceID   string
	Owner      types.NamespacedName
	OwnerUID   types.UID
	Generation int64
	Subject    authorization.Subject
	capability authorization.Capability
}

type routeBindingKey struct {
	Address     WatchAddress
	SourceID    string
	Owner       types.NamespacedName
	OwnerUID    types.UID
	Generation  int64
	PolicyEpoch uint64
}

// AuthorizedRoute is an opaque capability created only from an exact allowed
// selection target and authorization capability.
type AuthorizedRoute struct {
	binding RouteBinding
}

// NewAuthorizedRoute constructs a route only from an exact target and opaque
// allowed capability. The returned value contains no selector or resource
// data.
func NewAuthorizedRoute(target selection.ReadTarget, capability authorization.Capability) (AuthorizedRoute, error) {
	subject := capability.Subject()
	if err := subject.Validate(); err != nil {
		return AuthorizedRoute{}, errors.New("authorized route subject is incomplete")
	}
	if target.SourceID == "" {
		return AuthorizedRoute{}, errors.New("authorized route source identity is empty")
	}
	if !capability.Valid() || capability.Request() != selection.RequestForTarget(target) {
		return AuthorizedRoute{}, errors.New("authorized route requires a matching allowed capability")
	}
	address, err := AddressForTarget(target)
	if err != nil {
		return AuthorizedRoute{}, err
	}
	return AuthorizedRoute{binding: RouteBinding{
		Address:    address,
		SourceID:   target.SourceID,
		Owner:      subject.Key,
		OwnerUID:   subject.UID,
		Generation: subject.Generation,
		Subject:    subject,
		capability: capability,
	}}, nil
}

// Binding returns an immutable copy of the route identity.
func (r AuthorizedRoute) Binding() RouteBinding { return r.binding }

// Address returns the shared watch address.
func (r AuthorizedRoute) Address() WatchAddress { return r.binding.Address }

// WatchPermit is the private exact-address authorization permit passed to a
// metadata watcher. It contains only current capabilities for the shared
// address and has no public constructor.
type WatchPermit struct {
	address      WatchAddress
	capabilities []authorization.Capability
}

// Address returns the exact shared watch address.
func (p WatchPermit) Address() WatchAddress { return p.address }

// Capabilities returns defensive copies of the capabilities attached to the
// permit. It is useful to adapters that emit linked enforcement evidence.
func (p WatchPermit) Capabilities() []authorization.Capability {
	return append([]authorization.Capability(nil), p.capabilities...)
}

func (p WatchPermit) valid() bool {
	if p.address.Validate() != nil || len(p.capabilities) == 0 {
		return false
	}
	for _, capability := range p.capabilities {
		request := capability.Request()
		if !capability.Valid() || request.APIGroup != p.address.GVR.Group || request.Scope != p.address.Scope || request.Namespace != p.address.Namespace {
			return false
		}
	}
	return true
}

// MetadataWatcher is the only source-watch I/O port. It never performs a
// resource-instance LIST and receives only an opaque private permit.
type MetadataWatcher interface {
	Watch(context.Context, WatchPermit, string) (watch.Interface, error)
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
func (w *ClientMetadataWatcher) Watch(ctx context.Context, permit WatchPermit, resourceVersion string) (watch.Interface, error) {
	if w == nil || w.client == nil {
		return nil, errors.New("metadata watcher client is not configured")
	}
	if !permit.valid() {
		return nil, errors.New("metadata watch permit is invalid")
	}
	address := permit.address
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
	maxActiveWatches int
	watchObserver    *observability.Observer
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

// WithMaxActiveWatches configures the maximum number of exact metadata watch
// supervisors retained by the registry. Bound routes beyond this ceiling stay
// registered for periodic safety reconciliation.
func WithMaxActiveWatches(capacity int) RouteRegistryOption {
	return func(options *routeRegistryOptions) {
		if capacity > 0 {
			options.maxActiveWatches = capacity
		}
	}
}

// WithRouteWatchObserver reports unexpected source-watch stops and scheduled
// restarts without changing route ownership or retry semantics.
func WithRouteWatchObserver(observer *observability.Observer) RouteRegistryOption {
	return func(options *routeRegistryOptions) {
		options.watchObserver = observer
	}
}

// RouteRegistry maintains synchronized owner and target indexes and one
// supervisor per exact authorized address.
type RouteRegistry struct {
	watcher MetadataWatcher
	tracker *FreshnessTracker
	options routeRegistryOptions

	mu       sync.RWMutex
	byOwner  map[types.NamespacedName]map[routeBindingKey]RouteBinding
	byTarget map[WatchAddress]map[routeBindingKey]RouteBinding
	watches  map[WatchAddress]*watchSupervisor
	started  bool
	ctx      context.Context
	queue    workqueue.TypedRateLimitingInterface[reconcile.Request]
	ingress  TriggerIngress
}

// NewRouteRegistry creates an identity-only route registry.
func NewRouteRegistry(watcher MetadataWatcher, tracker *FreshnessTracker, options ...RouteRegistryOption) *RouteRegistry {
	settings := routeRegistryOptions{
		watchBackoffBase: defaultWatchBackoffBase,
		watchBackoffMax:  defaultWatchBackoffMax,
		maxActiveWatches: limits.DefaultMaxActiveWatches,
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
		byOwner:  make(map[types.NamespacedName]map[routeBindingKey]RouteBinding),
		byTarget: make(map[WatchAddress]map[routeBindingKey]RouteBinding),
		watches:  make(map[WatchAddress]*watchSupervisor),
	}
}

// SetTriggerIngress routes source-watch identities through the manager-owned
// bounded trigger ingress. A nil value restores standalone queue behavior.
func (r *RouteRegistry) SetTriggerIngress(ingress TriggerIngress) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.ingress = ingress
	r.mu.Unlock()
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
	if r.tracker == nil {
		return errors.New("route registry freshness tracker is required")
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
	if r.tracker != nil && !r.tracker.IsLeaseCurrent(lease) {
		return staleRuntimeError("route-replace")
	}
	newBindings := make(map[routeBindingKey]RouteBinding, len(routes))
	for _, route := range routes {
		binding := route.binding
		expectedSubject := authorization.Subject{Key: lease.Key, UID: lease.UID, Generation: lease.Generation, PolicyEpoch: lease.PolicyEpoch}
		if binding.Owner != lease.Key || binding.OwnerUID != lease.UID || binding.Generation != lease.Generation || binding.Subject != expectedSubject {
			return errors.New("authorized route does not match freshness lease")
		}
		if !binding.capability.Valid() || binding.capability.Subject() != binding.Subject || binding.capability.Request().SourceID != binding.SourceID || binding.capability.Request().Scope != binding.Address.Scope || binding.capability.Request().Namespace != binding.Address.Namespace || binding.capability.Request().APIGroup != binding.Address.GVR.Group {
			return errors.New("authorized route does not contain a matching capability")
		}
		if err := binding.Address.Validate(); err != nil {
			return err
		}
		if binding.SourceID == "" {
			return errors.New("authorized route source identity is empty")
		}
		key := keyForBinding(binding)
		if _, exists := newBindings[key]; exists {
			return errors.New("duplicate authorized route binding")
		}
		newBindings[key] = binding
	}

	r.mu.Lock()
	if r.tracker != nil && !r.tracker.IsLeaseCurrent(lease) {
		r.mu.Unlock()
		return staleRuntimeError("route-replace")
	}
	oldBindings := r.byOwner[lease.Key]
	toStop := make([]*watchSupervisor, 0)
	for key, binding := range oldBindings {
		if _, keep := newBindings[key]; keep {
			continue
		}
		if supervisor := r.removeBindingLocked(binding); supervisor != nil {
			toStop = append(toStop, supervisor)
		}
	}
	for key, binding := range newBindings {
		if _, exists := oldBindings[key]; exists {
			continue
		}
		r.addBindingLocked(binding)
	}
	if len(newBindings) == 0 {
		delete(r.byOwner, lease.Key)
	}
	started := r.started
	ctx := r.ctx
	toStart := r.ensureWatchCapacityLocked()
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

// currentPermit prunes every stale exact binding for an address and returns a
// fresh private permit only when at least one current capability remains.
func (r *RouteRegistry) currentPermit(address WatchAddress) (WatchPermit, bool) {
	if r == nil {
		return WatchPermit{}, false
	}
	r.mu.Lock()
	toStop := r.pruneStaleLocked(address)
	started := r.started
	ctx := r.ctx
	toStart := r.ensureWatchCapacityLocked()
	bindings := r.byTarget[address]
	ordered := make([]RouteBinding, 0, len(bindings))
	for _, binding := range bindings {
		ordered = append(ordered, binding)
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
	if len(ordered) == 0 {
		return WatchPermit{}, false
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Owner.Namespace != right.Owner.Namespace {
			return left.Owner.Namespace < right.Owner.Namespace
		}
		if left.Owner.Name != right.Owner.Name {
			return left.Owner.Name < right.Owner.Name
		}
		return left.SourceID < right.SourceID
	})
	capabilities := make([]authorization.Capability, 0, len(ordered))
	for _, binding := range ordered {
		capabilities = append(capabilities, binding.capability)
	}
	permit := WatchPermit{address: address, capabilities: capabilities}
	if !permit.valid() {
		return WatchPermit{}, false
	}
	return permit, true
}

func (r *RouteRegistry) pruneStaleLocked(address WatchAddress) []*watchSupervisor {
	if r == nil {
		return nil
	}
	bindings := r.byTarget[address]
	toStop := make([]*watchSupervisor, 0)
	for _, binding := range bindings {
		if r.tracker == nil || !r.tracker.IsCurrent(binding.Subject) {
			if supervisor := r.removeBindingLocked(binding); supervisor != nil {
				toStop = append(toStop, supervisor)
			}
		}
	}
	return toStop
}

func (r *RouteRegistry) addBindingLocked(binding RouteBinding) {
	key := keyForBinding(binding)
	ownerBindings := r.byOwner[binding.Owner]
	if ownerBindings == nil {
		ownerBindings = make(map[routeBindingKey]RouteBinding)
		r.byOwner[binding.Owner] = ownerBindings
	}
	ownerBindings[key] = binding
	targetBindings := r.byTarget[binding.Address]
	if targetBindings == nil {
		targetBindings = make(map[routeBindingKey]RouteBinding)
		r.byTarget[binding.Address] = targetBindings
	}
	targetBindings[key] = binding
}

func (r *RouteRegistry) removeBindingLocked(binding RouteBinding) *watchSupervisor {
	key := keyForBinding(binding)
	ownerBindings := r.byOwner[binding.Owner]
	if ownerBindings == nil {
		return nil
	}
	delete(ownerBindings, key)
	if len(ownerBindings) == 0 {
		delete(r.byOwner, binding.Owner)
	}
	targetBindings := r.byTarget[binding.Address]
	if targetBindings == nil {
		return nil
	}
	delete(targetBindings, key)
	if len(targetBindings) != 0 {
		return nil
	}
	delete(r.byTarget, binding.Address)
	supervisor := r.watches[binding.Address]
	delete(r.watches, binding.Address)
	return supervisor
}

func (r *RouteRegistry) ensureWatchCapacityLocked() []*watchSupervisor {
	if r == nil {
		return nil
	}
	capacity := r.options.maxActiveWatches
	if capacity <= 0 {
		capacity = limits.DefaultMaxActiveWatches
	}
	addresses := make([]WatchAddress, 0, len(r.byTarget))
	for address := range r.byTarget {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return watchAddressLess(addresses[i], addresses[j]) })
	toStart := make([]*watchSupervisor, 0)
	for _, address := range addresses {
		if len(r.watches) >= capacity {
			break
		}
		if _, exists := r.watches[address]; exists {
			continue
		}
		supervisor := &watchSupervisor{
			registry: r,
			address:  address,
			watcher:  r.watcher,
			base:     r.options.watchBackoffBase,
			maximum:  r.options.watchBackoffMax,
			observer: r.options.watchObserver,
		}
		r.watches[address] = supervisor
		toStart = append(toStart, supervisor)
	}
	return toStart
}

func watchAddressLess(left, right WatchAddress) bool {
	if left.GVR.Group != right.GVR.Group {
		return left.GVR.Group < right.GVR.Group
	}
	if left.GVR.Version != right.GVR.Version {
		return left.GVR.Version < right.GVR.Version
	}
	if left.GVR.Resource != right.GVR.Resource {
		return left.GVR.Resource < right.GVR.Resource
	}
	if left.Scope != right.Scope {
		return left.Scope < right.Scope
	}
	return left.Namespace < right.Namespace
}

func keyForBinding(binding RouteBinding) routeBindingKey {
	return routeBindingKey{
		Address:     binding.Address,
		SourceID:    binding.SourceID,
		Owner:       binding.Owner,
		OwnerUID:    binding.OwnerUID,
		Generation:  binding.Generation,
		PolicyEpoch: binding.Subject.PolicyEpoch,
	}
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
	for _, binding := range bindings {
		if supervisor := r.removeBindingLocked(binding); supervisor != nil {
			toStop = append(toStop, supervisor)
		}
	}
	started := r.started
	ctx := r.ctx
	toStart := r.ensureWatchCapacityLocked()
	r.mu.Unlock()
	for _, supervisor := range toStop {
		supervisor.stop()
	}
	if started {
		for _, supervisor := range toStart {
			r.startSupervisorAndWait(supervisor, ctx)
		}
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
	r.byOwner = make(map[types.NamespacedName]map[routeBindingKey]RouteBinding)
	r.byTarget = make(map[WatchAddress]map[routeBindingKey]RouteBinding)
	r.watches = make(map[WatchAddress]*watchSupervisor)
	r.mu.Unlock()
	for _, supervisor := range supervisors {
		supervisor.stop()
	}
}

// Owners returns sorted distinct owners currently bound to an address.
func (r *RouteRegistry) Owners(address WatchAddress) []types.NamespacedName {
	return r.currentOwners(address)
}

func (r *RouteRegistry) currentOwners(address WatchAddress) []types.NamespacedName {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	toStop := r.pruneStaleLocked(address)
	started := r.started
	ctx := r.ctx
	toStart := r.ensureWatchCapacityLocked()
	owners := make([]types.NamespacedName, 0, len(r.byTarget[address]))
	seen := make(map[types.NamespacedName]struct{}, len(r.byTarget[address]))
	for _, binding := range r.byTarget[address] {
		if _, exists := seen[binding.Owner]; exists {
			continue
		}
		seen[binding.Owner] = struct{}{}
		owners = append(owners, binding.Owner)
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
	owners := r.currentOwners(address)
	r.mu.RLock()
	queue := r.queue
	ingress := r.ingress
	r.mu.RUnlock()
	for _, owner := range owners {
		if ingress != nil {
			ingress.Enqueue(owner)
		} else if queue != nil {
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
	observer *observability.Observer

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
		permit, current := s.registry.currentPermit(s.address)
		if !current {
			signalWatchReady(s, &firstAttempt)
			return
		}
		stream, err := s.watcher.Watch(ctx, permit, resourceVersion)
		if firstAttempt {
			signalWatchReady(s, &firstAttempt)
		}
		if err == nil && stream != nil {
			err = s.consume(ctx, stream, &resourceVersion)
			stream.Stop()
		} else if err == nil {
			err = errors.New("metadata watcher returned no stream")
		}
		if apierrors.IsForbidden(err) {
			recordWatchForbidden(ctx, permit)
		}
		if ctx.Err() != nil {
			return
		}
		if isExpiredWatchError(err) {
			resourceVersion = ""
		}
		if _, current = s.registry.currentPermit(s.address); !current {
			return
		}
		watchReason := classifyWatchReason(err)
		if s.observer != nil {
			s.observer.ObserveWatchStopped(ctx, observability.WatchObservation{
				Target: watchObservationTarget(s.address),
				Reason: watchReason,
				Retry:  observability.RetryNone,
			})
			s.observer.ObserveWatchRestart(ctx, observability.WatchObservation{
				Target: watchObservationTarget(s.address),
				Reason: watchReason,
				Retry:  observability.RetryScheduled,
			})
		}
		if !waitFor(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, s.maximum)
	}
}

func watchObservationTarget(address WatchAddress) observability.WatchTarget {
	return observability.WatchTarget{
		APIGroup:  address.GVR.Group,
		Resource:  address.GVR.Resource,
		Scope:     observability.ScopeFromDiscovery(address.Scope),
		Namespace: address.Namespace,
	}
}

func classifyWatchReason(err error) observability.Reason {
	if err == nil {
		return observability.ReasonReadUnavailable
	}
	if apierrors.IsForbidden(err) {
		return observability.ReasonReadForbidden
	}
	if isExpiredWatchError(err) {
		return observability.ReasonListExpired
	}
	if apierrors.IsTimeout(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return observability.ReasonReadInterrupted
	}
	return observability.ReasonReadUnavailable
}

func signalWatchReady(s *watchSupervisor, firstAttempt *bool) {
	if s == nil || firstAttempt == nil || !*firstAttempt {
		return
	}
	s.mu.Lock()
	ready := s.ready
	s.ready = nil
	s.mu.Unlock()
	if ready != nil {
		close(ready)
	}
	*firstAttempt = false
}

func recordWatchForbidden(ctx context.Context, permit WatchPermit) {
	for _, capability := range permit.capabilities {
		record := capability.Record()
		record.Kind = authorization.RecordReadForbidden
		record.Outcome = authorization.OutcomeForbidden
		record.Reason = string(selection.ReasonReadForbidden)
		if recorder := capability.Recorder(); recorder != nil {
			recorder.Record(ctx, []authorization.Record{record})
		}
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
		switch status.Reason {
		case metav1.StatusReasonForbidden:
			return apierrors.NewForbidden(schema.GroupResource{Resource: "metadata"}, "watch", errors.New("metadata watch is forbidden"))
		case metav1.StatusReasonExpired:
			return apierrors.NewResourceExpired("metadata watch resource version expired")
		case metav1.StatusReasonTimeout:
			return apierrors.NewTimeoutError("metadata watch timed out", 0)
		}
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
