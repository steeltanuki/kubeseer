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

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/internal/localprobe"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func assertLocalEndpointSliceObserverScenarios(t *testing.T, ctx context.Context) {
	t.Helper()
	const (
		namespace   = "kubeseer-system"
		serviceName = "kubeseer-webhook"
	)

	t.Run("selects the Service label and aggregates every matching slice", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset(
			endpointSliceFixture(namespace, "webhook-a", serviceName,
				endpointFixture("10.0.0.1", nil),
				endpointFixture("10.0.0.2", boolPointer(false))),
			endpointSliceFixture(namespace, "webhook-b", serviceName,
				endpointFixture("10.0.0.3", boolPointer(true)),
				endpointFixture("10.0.0.4", boolPointer(false))),
			endpointSliceFixture(namespace, "other-service", "other-service",
				endpointFixture("10.0.0.5", boolPointer(true))),
		)
		var listNamespace, listSelector string
		client.PrependReactor("list", "endpointslices", func(action ktesting.Action) (bool, runtime.Object, error) {
			listAction, ok := action.(ktesting.ListAction)
			if !ok {
				t.Fatalf("EndpointSlice action = %T, want ListAction", action)
			}
			listNamespace = action.GetNamespace()
			listSelector = listAction.GetListRestrictions().Labels.String()
			return false, nil, nil
		})

		observer, err := localprobe.NewEndpointSliceObserver(client.DiscoveryV1(), namespace, serviceName)
		if err != nil {
			t.Fatalf("construct EndpointSlice observer: %v", err)
		}
		got, err := observer.ReadyEndpointCount(ctx)
		if err != nil {
			t.Fatalf("observe ready EndpointSlice endpoints: %v", err)
		}
		if got != 2 {
			t.Fatalf("ready EndpointSlice endpoints = %d, want 2", got)
		}
		if listNamespace != namespace {
			t.Fatalf("EndpointSlice LIST namespace = %q, want %q", listNamespace, namespace)
		}
		if listSelector != "kubernetes.io/service-name=kubeseer-webhook" {
			t.Fatalf("EndpointSlice LIST selector = %q, want exact Service equality selector", listSelector)
		}
	})

	t.Run("reports a Service-level zero-ready failure", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset(endpointSliceFixture(namespace, "webhook-not-ready", serviceName, endpointFixture("10.0.0.6", boolPointer(false))))
		observer, err := localprobe.NewEndpointSliceObserver(client.DiscoveryV1(), namespace, serviceName)
		if err != nil {
			t.Fatalf("construct zero-ready observer: %v", err)
		}
		got, err := observer.ReadyEndpointCount(ctx)
		if got != 0 {
			t.Fatalf("zero-ready count = %d, want 0", got)
		}
		if err == nil || !strings.Contains(err.Error(), "webhook Service kubeseer-system/kubeseer-webhook has no ready EndpointSlice endpoints") {
			t.Fatalf("zero-ready error = %v", err)
		}
	})

	t.Run("reports a Service-level list failure", func(t *testing.T) {
		client := kubernetesfake.NewSimpleClientset()
		client.PrependReactor("list", "endpointslices", func(ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("forbidden by EndpointSlice fixture")
		})
		observer, err := localprobe.NewEndpointSliceObserver(client.DiscoveryV1(), namespace, serviceName)
		if err != nil {
			t.Fatalf("construct list-error observer: %v", err)
		}
		got, err := observer.ReadyEndpointCount(ctx)
		if got != 0 {
			t.Fatalf("list-error count = %d, want 0", got)
		}
		if err == nil || !strings.Contains(err.Error(), "webhook Service kubeseer-system/kubeseer-webhook EndpointSlice list unavailable") || !strings.Contains(err.Error(), "forbidden by EndpointSlice fixture") {
			t.Fatalf("list-error = %v", err)
		}
	})
}

func endpointSliceFixture(namespace, name, serviceName string, endpoints ...discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{discoveryv1.LabelServiceName: serviceName},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   endpoints,
	}
}

func endpointFixture(address string, ready *bool) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses:  []string{address},
		Conditions: discoveryv1.EndpointConditions{Ready: ready},
	}
}

func boolPointer(value bool) *bool { return &value }
