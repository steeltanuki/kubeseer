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
	client     DiscoveryClient
	cacheTTL   time.Duration
	clock      Clock
	cacheMu    sync.RWMutex
	cache      map[cacheKey]cacheEntry
	refreshing map[cacheKey]*refreshState
}

type cacheKey struct {
	groupVersion string
	kind         string
}

type cacheEntry struct {
	resource  schema.GroupVersionResource
	scope     Scope
	expiresAt time.Time
}

func (e cacheEntry) resolution(sourceID string) Resolution {
	return Resolution{
		SourceID: sourceID,
		Resource: e.resource,
		Scope:    e.scope,
	}
}

type refreshState struct {
	done  chan struct{}
	entry cacheEntry
	err   error
}

// DefaultCacheTTL is the freshness interval used by a resolver unless an
// option supplies a different duration.
const DefaultCacheTTL = 5 * time.Minute

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
	if r.clock == nil {
		r.clock = Clock(time.Now)
	}
	if r.cache == nil {
		r.cache = make(map[cacheKey]cacheEntry)
	}
	if r.refreshing == nil {
		r.refreshing = make(map[cacheKey]*refreshState)
	}
}

func (r *Resolver) freshCacheEntry(key cacheKey) (cacheEntry, bool) {
	now := r.clock()
	r.cacheMu.RLock()
	entry, ok := r.cache[key]
	r.cacheMu.RUnlock()
	return entry, ok && now.Before(entry.expiresAt)
}

func (r *Resolver) beginRefresh(key cacheKey) (*refreshState, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if state, ok := r.refreshing[key]; ok {
		return state, false
	}
	state := &refreshState{done: make(chan struct{})}
	r.refreshing[key] = state
	return state, true
}

func (r *Resolver) finishRefresh(key cacheKey, state *refreshState, entry cacheEntry, err error) {
	r.cacheMu.Lock()
	state.entry = entry
	state.err = err
	if err == nil {
		r.cache[key] = entry
	}
	delete(r.refreshing, key)
	close(state.done)
	r.cacheMu.Unlock()
}

// NewResolver creates a resolver backed by the supplied discovery client.
// Options are intentionally variadic so existing callers retain the original
// constructor while tests and deployments can configure cache behavior.
func NewResolver(client DiscoveryClient, options ...ResolverOption) *Resolver {
	resolver := &Resolver{
		client:     client,
		cacheTTL:   DefaultCacheTTL,
		clock:      Clock(time.Now),
		cache:      make(map[cacheKey]cacheEntry),
		refreshing: make(map[cacheKey]*refreshState),
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
	state, owner := r.beginRefresh(key)
	if !owner {
		select {
		case <-state.done:
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
	entry := cacheEntry{
		resource:  resolution.Resource,
		scope:     resolution.Scope,
		expiresAt: r.clock().Add(r.cacheTTL),
	}
	r.finishRefresh(key, state, entry, nil)
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
