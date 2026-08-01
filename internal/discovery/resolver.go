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
	"fmt"
	"sort"
	"strings"

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
	client DiscoveryClient
}

// NewResolver creates a resolver backed by the supplied discovery client.
func NewResolver(client DiscoveryClient) *Resolver {
	return &Resolver{client: client}
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
	if r == nil || r.client == nil {
		return Resolution{}, NewResolutionError(descriptor.SourceID, ReasonDiscoveryUnavailable, "discovery client is not configured")
	}

	groupVersion, err := schema.ParseGroupVersion(descriptor.APIVersion)
	if err != nil || groupVersion.Version == "" {
		return Resolution{}, NewResolutionError(descriptor.SourceID, ReasonInvalidDescriptor, fmt.Sprintf("apiVersion %q is invalid", descriptor.APIVersion))
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
