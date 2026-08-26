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
	"errors"
	"fmt"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	discoveryruntime "github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/selection"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/metadata"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const controllerName = "kubeseer-reconciliation-runtime"

// SetupWithManager validates options and registers one low-level controller
// with typed lifecycle/policy sources and the custom trigger source.
func SetupWithManager(mgr manager.Manager, options Options) error {
	if mgr == nil {
		return errors.New("reconciliation manager is required")
	}
	normalized, err := options.normalized()
	if err != nil {
		return err
	}
	apiReader := mgr.GetAPIReader()
	managerClient := mgr.GetClient()
	if apiReader == nil || managerClient == nil {
		return errors.New("manager must provide API reader and client")
	}
	config := mgr.GetConfig()
	if config == nil {
		return errors.New("manager must provide a REST config")
	}

	discoveryClient, err := k8sdiscovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return fmt.Errorf("create discovery client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create dynamic client: %w", err)
	}
	metadataClient, err := metadata.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create metadata client: %w", err)
	}

	tracker := NewFreshnessTracker()
	store := NewClientKubeseerStore(apiReader)
	routes := NewRouteRegistry(
		NewClientMetadataWatcher(metadataClient),
		tracker,
		WithRouteWatchBackoff(normalized.WatchBackoffBase, normalized.WatchBackoffMax),
	)
	dependencies := Dependencies{
		Reader:       store,
		Lister:       store,
		PolicySource: accesspolicy.NewClientPolicySource(apiReader),
		Planner:      selection.NewPlanner(discoveryruntime.NewResolver(discoveryClient)),
		Executor:     selection.NewExecutor(selection.NewDynamicResourceLister(dynamicClient)),
		Routes:       routes,
		Publisher:    NewStatusPublisher(store, NewClientStatusWriter(managerClient.Status()), tracker),
		Tracker:      tracker,
	}
	runtime, err := NewRuntime(normalized, dependencies)
	if err != nil {
		return err
	}

	controllerInstance, err := controller.New(controllerName, mgr, controller.Options{Reconciler: runtime})
	if err != nil {
		return fmt.Errorf("create reconciliation controller: %w", err)
	}
	if err := controllerInstance.Watch(source.TypedKind(
		mgr.GetCache(),
		&v1alpha1.Kubeseer{},
		runtime.LifecycleHandler(),
		runtime.KubeseerPredicate(),
	)); err != nil {
		return fmt.Errorf("watch Kubeseer lifecycle: %w", err)
	}
	if err := controllerInstance.Watch(source.TypedKind(
		mgr.GetCache(),
		&v1alpha1.KubeseerAccessPolicy{},
		runtime.PolicyHandler(),
		runtime.PolicyPredicate(),
	)); err != nil {
		return fmt.Errorf("watch installation policy: %w", err)
	}
	if err := controllerInstance.Watch(runtime.TriggerSource()); err != nil {
		return fmt.Errorf("watch reconciliation triggers: %w", err)
	}
	return nil
}
