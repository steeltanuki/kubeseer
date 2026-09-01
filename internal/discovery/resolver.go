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

package discovery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DiscoveryClient is the narrow client-go boundary required by the resolver.
// The real client is configured with a REST timeout by its caller.
type DiscoveryClient interface {
	ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error)
}

// Resolver maps source descriptors to Kubernetes resource identities.
type Resolver struct {
	client            DiscoveryClient
	cacheTTL          time.Duration
	cacheCapacity     int
	clock             Clock
	cacheMu           sync.RWMutex
	cache             map[cacheKey]cacheEntry
	refreshing        map[cacheKey]*refreshState
	accessSequence    uint64
	invalidationEpoch uint64
	keyEpoch          map[cacheKey]uint64
}

type cacheKey struct {
	groupVersion string
	kind         string
}

type cacheEntry struct {
	resource       schema.GroupVersionResource
	scope          Scope
	expiresAt      time.Time
	accessSequence uint64
	version        cacheVersion
}

type cacheVersion struct {
	all uint64
	key uint64
}

func (e cacheEntry) resolution(sourceID string) Resolution {
	return Resolution{
		SourceID: sourceID,
		Resource: e.resource,
		Scope:    e.scope,
	}
}

type refreshState struct {
	done    chan struct{}
	entry   cacheEntry
	err     error
	version cacheVersion
}

// DefaultCacheTTL is the freshness interval used by a resolver unless an
// option supplies a different duration.
const DefaultCacheTTL = 5 * time.Minute

// DefaultCacheCapacity is the metadata-entry bound used by a resolver unless
// the manager composition root supplies a different positive capacity.
const DefaultCacheCapacity = 1024

// Clock supplies the current time used for cache expiry decisions.
type Clock func() time.Time

// ResolverOption customizes resolver behavior at construction time.
type ResolverOption func(*Resolver)

// WithCacheTTL configures the resolver's per-entry freshness interval.
func WithCacheTTL(ttl time.Duration) ResolverOption {
	return func(r *Resolver) {
		if ttl > 0 {
			r.cacheTTL = ttl
		}
	}
}

// WithCacheCapacity configures the maximum number of discovery metadata
// entries retained by the resolver. Non-positive values leave the default in
// place so an incomplete option cannot make the cache unbounded or unusable.
func WithCacheCapacity(capacity int) ResolverOption {
	return func(r *Resolver) {
		if capacity > 0 {
			r.cacheCapacity = capacity
		}
	}
}

// WithClock injects the clock used for cache insertion and expiry checks.
func WithClock(clock Clock) ResolverOption {
	return func(r *Resolver) {
		if clock != nil {
			r.clock = clock
		}
	}
}

func (r *Resolver) ensureCache() {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.cacheTTL <= 0 {
		r.cacheTTL = DefaultCacheTTL
	}
	if r.cacheCapacity <= 0 {
		r.cacheCapacity = DefaultCacheCapacity
	}
	if r.clock == nil {
		r.clock = Clock(time.Now)
	}
	if r.cache == nil {
		r.cache = make(map[cacheKey]cacheEntry)
	}
	if r.refreshing == nil {
		r.refreshing = make(map[cacheKey]*refreshState)
	}
	if r.keyEpoch == nil {
		r.keyEpoch = make(map[cacheKey]uint64)
	}
}

func (r *Resolver) freshCacheEntry(key cacheKey) (cacheEntry, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	entry, ok := r.cache[key]
	if !ok {
		return cacheEntry{}, false
	}
	if entry.version != r.versionLocked(key) {
		delete(r.cache, key)
		return cacheEntry{}, false
	}
	if !r.clock().Before(entry.expiresAt) {
		return cacheEntry{}, false
	}
	entry.accessSequence = r.nextAccessLocked()
	r.cache[key] = entry
	return entry, true
}

func (r *Resolver) beginRefresh(key cacheKey) (*refreshState, bool, cacheEntry, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	version := r.versionLocked(key)
	if entry, ok := r.cache[key]; ok {
		if entry.version != version {
			delete(r.cache, key)
		} else if r.clock().Before(entry.expiresAt) {
			entry.accessSequence = r.nextAccessLocked()
			r.cache[key] = entry
			return nil, false, entry, true
		}
	}
	if state, ok := r.refreshing[key]; ok {
		if state.version == version {
			return state, false, cacheEntry{}, false
		}
		// An invalidation advances the version. The old request is allowed to
		// finish, but a new caller must not wait for or reuse its result.
		delete(r.refreshing, key)
	}
	state := &refreshState{done: make(chan struct{}), version: version}
	r.refreshing[key] = state
	return state, true, cacheEntry{}, false
}

func (r *Resolver) finishRefresh(key cacheKey, state *refreshState, entry cacheEntry, err error) {
	r.cacheMu.Lock()
	state.entry = entry
	state.err = err
	if current, ok := r.refreshing[key]; ok && current == state {
		if err == nil && state.version == r.versionLocked(key) {
			entry.version = state.version
			r.retainEntryLocked(key, entry, state)
		}
		delete(r.refreshing, key)
	}
	close(state.done)
	r.cacheMu.Unlock()
}

func (r *Resolver) versionLocked(key cacheKey) cacheVersion {
	return cacheVersion{all: r.invalidationEpoch, key: r.keyEpoch[key]}
}

func (r *Resolver) nextAccessLocked() uint64 {
	r.accessSequence++
	if r.accessSequence == 0 {
		// Sequence overflow is practically unreachable, but renumbering keeps
		// eviction deterministic even for a long-lived process.
		var sequence uint64
		for key, entry := range r.cache {
			sequence++
			entry.accessSequence = sequence
			r.cache[key] = entry
		}
		r.accessSequence = sequence + 1
	}
	return r.accessSequence
}

func (r *Resolver) retainEntryLocked(key cacheKey, entry cacheEntry, state *refreshState) bool {
	if r.cacheCapacity <= 0 || state == nil || state.version != r.versionLocked(key) {
		return false
	}

	// Replacing a stale entry for the same key never needs to evict another
	// key. A refresh state is deliberately kept separate from this metadata.
	delete(r.cache, key)
	if len(r.cache) >= r.cacheCapacity {
		victim, found := r.lruVictimLocked()
		if !found {
			// Every retained entry is refreshing. The caller still receives the
			// fresh result; retention is an optimization, not a correctness
			// dependency.
			return false
		}
		delete(r.cache, victim)
	}
	entry.accessSequence = r.nextAccessLocked()
	r.cache[key] = entry
	return true
}

func (r *Resolver) lruVictimLocked() (cacheKey, bool) {
	var victim cacheKey
	found := false
	for key, entry := range r.cache {
		if _, refreshing := r.refreshing[key]; refreshing {
			continue
		}
		if !found || entry.accessSequence < r.cache[victim].accessSequence ||
			(entry.accessSequence == r.cache[victim].accessSequence && cacheKeyLess(key, victim)) {
			victim = key
			found = true
		}
	}
	return victim, found
}

func cacheKeyLess(left, right cacheKey) bool {
	if left.groupVersion != right.groupVersion {
		return left.groupVersion < right.groupVersion
	}
	return left.kind < right.kind
}

func (r *Resolver) refreshVersionCurrent(key cacheKey, version cacheVersion) bool {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	return r.versionLocked(key) == version
}

// InvalidateGroupVersion removes every cached kind under a canonical API
// group/version. Invalid input is ignored because there is no cache key to
// invalidate.
func (r *Resolver) InvalidateGroupVersion(groupVersion string) {
	canonical, ok := canonicalGroupVersion(groupVersion)
	if r == nil || !ok {
		return
	}
	r.ensureCache()
	r.cacheMu.Lock()
	keys := make(map[cacheKey]struct{})
	for key := range r.cache {
		if key.groupVersion == canonical {
			keys[key] = struct{}{}
			delete(r.cache, key)
		}
	}
	for key := range r.refreshing {
		if key.groupVersion == canonical {
			keys[key] = struct{}{}
		}
	}
	for key := range keys {
		r.keyEpoch[key]++
	}
	r.cacheMu.Unlock()
}

// InvalidateResource removes one cached kind under a canonical API
// group/version. The next resolution performs one bounded discovery request.
func (r *Resolver) InvalidateResource(groupVersion, kind string) {
	canonical, ok := canonicalGroupVersion(groupVersion)
	if r == nil || !ok || kind == "" {
		return
	}
	r.ensureCache()
	r.cacheMu.Lock()
	key := cacheKey{groupVersion: canonical, kind: kind}
	delete(r.cache, key)
	r.keyEpoch[key]++
	r.cacheMu.Unlock()
}

// InvalidateAll removes every cached resolution without starting discovery.
func (r *Resolver) InvalidateAll() {
	if r == nil {
		return
	}
	r.ensureCache()
	r.cacheMu.Lock()
	r.invalidationEpoch++
	r.cache = make(map[cacheKey]cacheEntry)
	r.cacheMu.Unlock()
}

func canonicalGroupVersion(value string) (string, bool) {
	groupVersion, err := schema.ParseGroupVersion(strings.TrimSpace(value))
	if err != nil || groupVersion.Version == "" {
		return "", false
	}
	return groupVersion.String(), true
}

// NewResolver creates a resolver backed by the supplied discovery client.
// Options are intentionally variadic so existing callers retain the original
// constructor while tests and deployments can configure cache behavior.
func NewResolver(client DiscoveryClient, options ...ResolverOption) *Resolver {
	resolver := &Resolver{
		client:        client,
		cacheTTL:      DefaultCacheTTL,
		cacheCapacity: DefaultCacheCapacity,
		clock:         Clock(time.Now),
		cache:         make(map[cacheKey]cacheEntry),
		refreshing:    make(map[cacheKey]*refreshState),
		keyEpoch:      make(map[cacheKey]uint64),
	}
	for _, option := range options {
		if option != nil {
			option(resolver)
		}
	}
	return resolver
}

// ResolveBatch resolves each descriptor independently and preserves one
// source-scoped outcome for every input descriptor. A canceled context stops
// new discovery requests while still returning an outcome for every input.
func (r *Resolver) ResolveBatch(ctx context.Context, descriptors []SourceDescriptor) []Outcome {
	if ctx == nil {
		ctx = context.Background()
	}

	outcomes := make([]Outcome, len(descriptors))
	for i, descriptor := range descriptors {
		outcomes[i].SourceID = descriptor.SourceID
		if cause := ctx.Err(); cause != nil {
			outcomes[i].Err = &ResolutionError{
				SourceID: descriptor.SourceID,
				Reason:   ReasonDiscoveryUnavailable,
				Message:  "batch resolution context is unavailable",
				Cause:    cause,
			}
			continue
		}

		resolution, err := r.Resolve(ctx, descriptor)
		if err != nil {
			outcomes[i].Err = batchResolutionError(descriptor.SourceID, err)
			continue
		}
		outcomes[i].Resolution = &resolution
	}

	return outcomes
}

func batchResolutionError(sourceID string, err error) *ResolutionError {
	return sourceScopedResolutionError(sourceID, err, "batch resolution failed")
}

// Resolve returns one resource identity and its discovery-reported scope.
func (r *Resolver) Resolve(ctx context.Context, descriptor SourceDescriptor) (Resolution, error) {
	if err := descriptor.Validate(); err != nil {
		return Resolution{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Resolution{}, &ResolutionError{
			SourceID: descriptor.SourceID,
			Reason:   ReasonDiscoveryUnavailable,
			Message:  "discovery context is not available",
			Cause:    err,
		}
	}
	if r == nil {
		return Resolution{}, NewResolutionError(descriptor.SourceID, ReasonDiscoveryUnavailable, "discovery client is not configured")
	}
	r.ensureCache()

	groupVersion, err := schema.ParseGroupVersion(descriptor.APIVersion)
	if err != nil || groupVersion.Version == "" {
		return Resolution{}, NewResolutionError(descriptor.SourceID, ReasonInvalidDescriptor, fmt.Sprintf("apiVersion %q is invalid", descriptor.APIVersion))
	}

	key := cacheKey{groupVersion: groupVersion.String(), kind: descriptor.Kind}
	if entry, ok := r.freshCacheEntry(key); ok {
		return entry.resolution(descriptor.SourceID), nil
	}
	return r.resolveWithRefresh(ctx, descriptor, groupVersion, key)
}

func (r *Resolver) resolveWithRefresh(ctx context.Context, descriptor SourceDescriptor, groupVersion schema.GroupVersion, key cacheKey) (Resolution, error) {
	state, owner, entry, cached := r.beginRefresh(key)
	if cached {
		return entry.resolution(descriptor.SourceID), nil
	}
	if !owner {
		select {
		case <-state.done:
			if !r.refreshVersionCurrent(key, state.version) {
				return r.resolveWithRefresh(ctx, descriptor, groupVersion, key)
			}
			if state.err != nil {
				return Resolution{}, sourceScopedResolutionError(descriptor.SourceID, state.err, "discovery refresh failed")
			}
			return state.entry.resolution(descriptor.SourceID), nil
		case <-ctx.Done():
			return Resolution{}, &ResolutionError{
				SourceID: descriptor.SourceID,
				Reason:   ReasonDiscoveryUnavailable,
				Message:  "discovery context expired while waiting for refresh",
				Cause:    ctx.Err(),
			}
		}
	}

	resolution, err := r.resolveFromDiscovery(ctx, descriptor, groupVersion)
	if err != nil {
		r.finishRefresh(key, state, cacheEntry{}, err)
		return Resolution{}, err
	}
	refreshedEntry := cacheEntry{
		resource: resolution.Resource,
		scope:    resolution.Scope,
		version:  state.version,
	}
	r.cacheMu.RLock()
	refreshedEntry.expiresAt = r.clock().Add(r.cacheTTL)
	r.cacheMu.RUnlock()
	r.finishRefresh(key, state, refreshedEntry, nil)
	if !r.refreshVersionCurrent(key, state.version) {
		return r.resolveWithRefresh(ctx, descriptor, groupVersion, key)
	}
	return resolution, nil
}

func (r *Resolver) resolveFromDiscovery(ctx context.Context, descriptor SourceDescriptor, groupVersion schema.GroupVersion) (Resolution, error) {
	if err := ctx.Err(); err != nil {
		return Resolution{}, &ResolutionError{
			SourceID: descriptor.SourceID,
			Reason:   ReasonDiscoveryUnavailable,
			Message:  "discovery context is not available",
			Cause:    err,
		}
	}
	if r.client == nil {
		return Resolution{}, NewResolutionError(descriptor.SourceID, ReasonDiscoveryUnavailable, "discovery client is not configured")
	}

	resourceList, err := r.client.ServerResourcesForGroupVersion(groupVersion.String())
	if err != nil {
		return Resolution{}, &ResolutionError{
			SourceID: descriptor.SourceID,
			Reason:   ReasonDiscoveryUnavailable,
			Message:  fmt.Sprintf("unable to discover %s", groupVersion.String()),
			Cause:    err,
		}
	}
	if err := ctx.Err(); err != nil {
		return Resolution{}, &ResolutionError{
			SourceID: descriptor.SourceID,
			Reason:   ReasonDiscoveryUnavailable,
			Message:  "discovery context expired",
			Cause:    err,
		}
	}

	candidates := matchingResources(resourceList, descriptor.Kind)
	switch len(candidates) {
	case 0:
		return Resolution{}, NewResolutionError(
			descriptor.SourceID,
			ReasonUnknownType,
			fmt.Sprintf("no resource matches kind %q in %q", descriptor.Kind, groupVersion.String()),
		)
	case 1:
		resource := candidates[0]
		scope := ScopeCluster
		if resource.Namespaced {
			scope = ScopeNamespaced
		}
		return Resolution{
			SourceID: descriptor.SourceID,
			Resource: groupVersion.WithResource(resource.Name),
			Scope:    scope,
		}, nil
	default:
		return Resolution{}, NewResolutionError(
			descriptor.SourceID,
			ReasonAmbiguousResource,
			fmt.Sprintf("multiple resources match kind %q in %q", descriptor.Kind, groupVersion.String()),
		)
	}
}

func sourceScopedResolutionError(sourceID string, err error, fallbackMessage string) *ResolutionError {
	var resolutionErr *ResolutionError
	if errors.As(err, &resolutionErr) {
		copy := *resolutionErr
		copy.SourceID = sourceID
		return &copy
	}
	return &ResolutionError{
		SourceID: sourceID,
		Reason:   ReasonDiscoveryUnavailable,
		Message:  fallbackMessage,
		Cause:    err,
	}
}

// RequireNamespaced rejects a resolution that cannot be addressed through a
// namespace. Callers should apply this guard before constructing a namespaced
// dynamic client.
func RequireNamespaced(resolution Resolution) error {
	if resolution.Scope == ScopeNamespaced {
		return nil
	}
	return NewResolutionError(
		resolution.SourceID,
		ReasonInvalidScope,
		fmt.Sprintf("resource %s is %s", resolution.Resource.String(), resolution.Scope.String()),
	)
}

func matchingResources(resourceList *metav1.APIResourceList, kind string) []metav1.APIResource {
	if resourceList == nil {
		return nil
	}

	candidates := make([]metav1.APIResource, 0, len(resourceList.APIResources))
	for _, resource := range resourceList.APIResources {
		if resource.Kind != kind || resource.Name == "" || strings.Contains(resource.Name, "/") {
			continue
		}
		candidates = append(candidates, resource)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Name != candidates[j].Name {
			return candidates[i].Name < candidates[j].Name
		}
		return candidates[i].Namespaced && !candidates[j].Namespaced
	})
	return candidates
}
