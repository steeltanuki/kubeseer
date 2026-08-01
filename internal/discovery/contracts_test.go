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
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestDiscoveryContracts(t *testing.T) {
	t.Run("valid descriptors accept core and grouped API versions", func(t *testing.T) {
		for _, descriptor := range []SourceDescriptor{
			{SourceID: "pods", APIVersion: "v1", Kind: "Pod"},
			{SourceID: "deployments", APIVersion: "apps/v1", Kind: "Deployment"},
		} {
			if err := descriptor.Validate(); err != nil {
				t.Fatalf("valid descriptor %q rejected: %v", descriptor.SourceID, err)
			}
		}
	})

	t.Run("invalid descriptors return source-scoped typed errors", func(t *testing.T) {
		cases := []struct {
			name       string
			descriptor SourceDescriptor
			message    string
		}{
			{
				name:       "missing api version",
				descriptor: SourceDescriptor{SourceID: "missing-version", Kind: "Pod"},
				message:    "apiVersion must not be empty",
			},
			{
				name:       "malformed api version",
				descriptor: SourceDescriptor{SourceID: "malformed-version", APIVersion: "apps/", Kind: "Deployment"},
				message:    `apiVersion "apps/" is invalid`,
			},
			{
				name:       "missing kind",
				descriptor: SourceDescriptor{SourceID: "missing-kind", APIVersion: "v1"},
				message:    "kind must not be empty",
			},
		}

		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				err := testCase.descriptor.Validate()
				if !HasReason(err, ReasonInvalidDescriptor) {
					t.Fatalf("expected invalid descriptor reason, got %v", err)
				}
				if !strings.Contains(err.Error(), testCase.descriptor.SourceID) {
					t.Fatalf("error %q does not identify source %q", err, testCase.descriptor.SourceID)
				}
				if !strings.Contains(err.Error(), testCase.message) {
					t.Fatalf("error %q does not contain %q", err, testCase.message)
				}
			})
		}
	})

	t.Run("scope and resolution values preserve metadata", func(t *testing.T) {
		namespaced := Resolution{
			SourceID: "pods",
			Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			Scope:    ScopeNamespaced,
		}
		cluster := Resolution{
			SourceID: "nodes",
			Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"},
			Scope:    ScopeCluster,
		}
		if !namespaced.Scope.Valid() || namespaced.Scope.String() != "namespaced" {
			t.Fatalf("invalid namespaced scope: %#v", namespaced.Scope)
		}
		if !cluster.Scope.Valid() || cluster.Scope.String() != "cluster" {
			t.Fatalf("invalid cluster scope: %#v", cluster.Scope)
		}
		if namespaced.Resource.String() != "/v1, Resource=pods" {
			t.Fatalf("unexpected namespaced GVR: %s", namespaced.Resource)
		}
		if cluster.Resource.String() != "/v1, Resource=nodes" {
			t.Fatalf("unexpected cluster GVR: %s", cluster.Resource)
		}
	})

	t.Run("errors remain deterministic and unwrap causes", func(t *testing.T) {
		cause := errors.New("discovery request failed")
		err := &ResolutionError{
			SourceID: "unavailable",
			Reason:   ReasonDiscoveryUnavailable,
			Message:  "discovery request failed",
			Cause:    cause,
		}
		if !HasReason(err, ReasonDiscoveryUnavailable) {
			t.Fatalf("expected discovery unavailable reason, got %v", err)
		}
		if !errors.Is(err, cause) {
			t.Fatalf("resolution error did not unwrap cause")
		}
		const expected = `source "unavailable": DiscoveryUnavailable: discovery request failed`
		if err.Error() != expected || err.Error() != expected {
			t.Fatalf("unstable resolution error: %q", err)
		}
	})

	t.Run("status-ready reasons remain source scoped", func(t *testing.T) {
		for _, reason := range []ResolutionErrorReason{
			ReasonUnknownType,
			ReasonDiscoveryUnavailable,
			ReasonAmbiguousResource,
		} {
			err := NewResolutionError("source-one", reason, "resolution failed")
			if !HasReason(err, reason) {
				t.Fatalf("reason %q was not retained: %v", reason, err)
			}
			if !strings.Contains(err.Error(), `source "source-one"`) {
				t.Fatalf("error %q does not identify the source", err)
			}
		}
	})
}
