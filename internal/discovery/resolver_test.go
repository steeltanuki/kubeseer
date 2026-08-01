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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type scriptedDiscovery struct {
	resources map[string]*metav1.APIResourceList
	errors    map[string]error
	calls     []string
}

func (s *scriptedDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	s.calls = append(s.calls, groupVersion)
	if err := s.errors[groupVersion]; err != nil {
		return nil, err
	}
	return s.resources[groupVersion], nil
}

func TestResolveSingle(t *testing.T) {
	client := &scriptedDiscovery{
		resources: map[string]*metav1.APIResourceList{
			"v1": {
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true}},
			},
		},
		errors: map[string]error{},
	}

	resolution, err := NewResolver(client).Resolve(context.Background(), SourceDescriptor{
		SourceID:   "pod-source",
		APIVersion: "v1",
		Kind:       "Pod",
	})
	if err != nil {
		t.Fatalf("resolve source: %v", err)
	}
	expected := Resolution{
		SourceID: "pod-source",
		Resource: schema.GroupVersionResource{Version: "v1", Resource: "pods"},
		Scope:    ScopeNamespaced,
	}
	if resolution != expected {
		t.Fatalf("unexpected resolution: got %#v, want %#v", resolution, expected)
	}
}

func TestResolveCore(t *testing.T) {
	client := discoveryWith("v1", metav1.APIResource{Name: "nodes", Kind: "Node", Namespaced: false})
	resolution, err := NewResolver(client).Resolve(context.Background(), SourceDescriptor{
		SourceID:   "node-source",
		APIVersion: "v1",
		Kind:       "Node",
	})
	if err != nil {
		t.Fatalf("resolve core resource: %v", err)
	}
	if resolution.Resource != (schema.GroupVersionResource{Version: "v1", Resource: "nodes"}) {
		t.Fatalf("unexpected core GVR: %#v", resolution.Resource)
	}
	if resolution.Scope != ScopeCluster {
		t.Fatalf("unexpected core scope: %s", resolution.Scope)
	}
}

func TestResolveGrouped(t *testing.T) {
	client := discoveryWith("apps/v1", metav1.APIResource{Name: "deployments", Kind: "Deployment", Namespaced: true})
	resolution, err := NewResolver(client).Resolve(context.Background(), SourceDescriptor{
		SourceID:   "deployment-source",
		APIVersion: "apps/v1",
		Kind:       "Deployment",
	})
	if err != nil {
		t.Fatalf("resolve grouped resource: %v", err)
	}
	want := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	if resolution.Resource != want || resolution.Scope != ScopeNamespaced {
		t.Fatalf("unexpected grouped resolution: got %#v", resolution)
	}
}

func TestResolveCRD(t *testing.T) {
	client := discoveryWith("example.io/v1", metav1.APIResource{Name: "widgets", Kind: "Widget", Namespaced: true})
	resolution, err := NewResolver(client).Resolve(context.Background(), SourceDescriptor{
		SourceID:   "widget-source",
		APIVersion: "example.io/v1",
		Kind:       "Widget",
	})
	if err != nil {
		t.Fatalf("resolve CRD resource: %v", err)
	}
	want := schema.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "widgets"}
	if resolution.Resource != want || resolution.Scope != ScopeNamespaced {
		t.Fatalf("unexpected CRD resolution: got %#v", resolution)
	}
}

func TestResolveScope(t *testing.T) {
	client := discoveryWith("v1", metav1.APIResource{Name: "nodes", Kind: "Node", Namespaced: false})
	resolution, err := NewResolver(client).Resolve(context.Background(), SourceDescriptor{
		SourceID:   "cluster-source",
		APIVersion: "v1",
		Kind:       "Node",
	})
	if err != nil {
		t.Fatalf("resolve cluster resource: %v", err)
	}
	scopeErr := RequireNamespaced(resolution)
	if !HasReason(scopeErr, ReasonInvalidScope) {
		t.Fatalf("expected invalid scope for namespaced representation, got %v", scopeErr)
	}
	if !strings.Contains(scopeErr.Error(), "cluster") {
		t.Fatalf("scope error does not explain cluster scope: %v", scopeErr)
	}
}

func TestResolveAmbiguous(t *testing.T) {
	client := &scriptedDiscovery{
		resources: map[string]*metav1.APIResourceList{
			"example.io/v1": {
				GroupVersion: "example.io/v1",
				APIResources: []metav1.APIResource{
					{Name: "widgets", Kind: "Widget", Namespaced: true},
					{Name: "clusterwidgets", Kind: "Widget", Namespaced: false},
					{Name: "widgets/status", Kind: "Widget", Namespaced: true},
				},
			},
		},
		errors: map[string]error{},
	}

	_, err := NewResolver(client).Resolve(context.Background(), SourceDescriptor{
		SourceID:   "ambiguous-source",
		APIVersion: "example.io/v1",
		Kind:       "Widget",
	})
	if !HasReason(err, ReasonAmbiguousResource) {
		t.Fatalf("expected ambiguous resource error, got %v", err)
	}
	if err.Error() != `source "ambiguous-source": AmbiguousResource: multiple resources match kind "Widget" in "example.io/v1"` {
		t.Fatalf("unexpected ambiguous error: %v", err)
	}
}

func TestResolveErrors(t *testing.T) {
	client := &scriptedDiscovery{
		resources: map[string]*metav1.APIResourceList{
			"v1": {GroupVersion: "v1"},
		},
		errors: map[string]error{
			"apps/v1": errors.New("connection refused"),
		},
	}
	resolver := NewResolver(client)

	_, err := resolver.Resolve(context.Background(), SourceDescriptor{SourceID: "unknown-source", APIVersion: "v1", Kind: "Missing"})
	if !HasReason(err, ReasonUnknownType) {
		t.Fatalf("expected unknown type error, got %v", err)
	}

	_, err = resolver.Resolve(context.Background(), SourceDescriptor{SourceID: "unavailable-source", APIVersion: "apps/v1", Kind: "Deployment"})
	if !HasReason(err, ReasonDiscoveryUnavailable) {
		t.Fatalf("expected discovery unavailable error, got %v", err)
	}
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || !errors.Is(err, client.errors["apps/v1"]) {
		t.Fatalf("discovery error did not preserve its cause: %v", err)
	}
}

func discoveryWith(groupVersion string, resources ...metav1.APIResource) *scriptedDiscovery {
	return &scriptedDiscovery{
		resources: map[string]*metav1.APIResourceList{
			groupVersion: {GroupVersion: groupVersion, APIResources: resources},
		},
		errors: map[string]error{},
	}
}
