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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/observability"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

func assertObservabilityWatchScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	source := v1alpha1.KubeseerSource{ID: "observability-watch-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}, Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}}}
	planner := selection.NewPlanner(resolver)
	target := mustRuntimeTarget(t, ctx, planner, "team-a", source)
	snapshot := mustSnapshot(t, basePolicy())

	t.Run("unexpected close emits bounded stop and restart signals", func(t *testing.T) {
		first := watch.NewRaceFreeFake()
		second := watch.NewRaceFreeFake()
		watcher := newRuntimeScriptedMetadataWatcher(runtimeWatchResponse{stream: first}, runtimeWatchResponse{stream: second})
		registry := prometheus.NewRegistry()
		capture := &observabilityCapture{err: errors.New("watch capture unavailable")}
		observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture})
		if err != nil {
			t.Fatalf("construct watch observer: %v", err)
		}
		tracker := reconciliation.NewFreshnessTracker()
		ownerKey := types.NamespacedName{Namespace: "team-a", Name: "observability-watch-owner"}
		owner := newRuntimeKubeseer(ownerKey, "observability-watch-uid", 1)
		tracker.Observe(owner)
		lease, _, release, err := tracker.Acquire(ctx, ownerKey, owner.UID, owner.Generation)
		if err != nil {
			t.Fatalf("acquire watch lease: %v", err)
		}
		defer release()
		route, err := mustRuntimeAuthorizedRoute(t, lease, target, snapshot)
		if err != nil {
			t.Fatalf("authorize watch route: %v", err)
		}
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		watchRegistry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond), reconciliation.WithRouteWatchObserver(observer))
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := watchRegistry.Start(watchContext, queue); err != nil {
			t.Fatalf("start observed watch registry: %v", err)
		}
		if err := watchRegistry.Replace(context.Background(), lease, []reconciliation.AuthorizedRoute{route}); err != nil {
			t.Fatalf("replace observed watch route: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		first.Stop()
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 2 })
		records := capture.snapshot()
		if len(records) < 2 || records[len(records)-2].Event != observability.EventSourceWatchStopped || records[len(records)-1].Event != observability.EventSourceWatchRestarted {
			t.Fatalf("watch lifecycle records = %#v", records)
		}
		for _, record := range records {
			if record.Target.Resource != "pods" || record.Target.Scope != observability.ScopeNamespaced || record.Target.Namespace != "team-a" || record.Reason != observability.ReasonReadUnavailable {
				t.Fatalf("watch target/classification = %#v", record)
			}
			if strings.Contains(record.SourceID, "secret-watch") || strings.Contains(record.Name, "raw-watch") {
				t.Fatalf("watch record retained forbidden data = %#v", record)
			}
		}
		assertObservabilityMetricValue(t, registry, "kubeseer_source_watch_restarts_total", `reason="ReadUnavailable"`, 1)
		watchRegistry.RemoveOwner(ownerKey)
	})

	t.Run("forbidden watch errors remain distinct and sanitized", func(t *testing.T) {
		watcher := newRuntimeScriptedMetadataWatcher(runtimeWatchResponse{err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "secret-watch-object-sentinel", errors.New("raw-watch-body-sentinel"))})
		capture := &observabilityCapture{}
		registry := prometheus.NewRegistry()
		observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture})
		if err != nil {
			t.Fatalf("construct forbidden-watch observer: %v", err)
		}
		tracker := reconciliation.NewFreshnessTracker()
		ownerKey := types.NamespacedName{Namespace: "team-a", Name: "observability-forbidden-watch"}
		owner := newRuntimeKubeseer(ownerKey, "observability-forbidden-uid", 1)
		tracker.Observe(owner)
		lease, _, release, err := tracker.Acquire(ctx, ownerKey, owner.UID, owner.Generation)
		if err != nil {
			t.Fatalf("acquire forbidden-watch lease: %v", err)
		}
		route, err := runtimeAuthorizedRouteWithRecorder(t, lease, target, snapshot, observability.NewAuthorizationRecorder(observer))
		if err != nil {
			release()
			t.Fatalf("authorize forbidden-watch route: %v", err)
		}
		watchContext, cancel := context.WithCancel(ctx)
		watchRegistry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithRouteWatchBackoff(20*time.Millisecond, 20*time.Millisecond), reconciliation.WithRouteWatchObserver(observer))
		queue := newRuntimeQueue()
		if err := watchRegistry.Start(watchContext, queue); err != nil {
			queue.ShutDown()
			release()
			t.Fatalf("start forbidden-watch registry: %v", err)
		}
		if err := watchRegistry.Replace(context.Background(), lease, []reconciliation.AuthorizedRoute{route}); err != nil {
			cancel()
			queue.ShutDown()
			release()
			t.Fatalf("replace forbidden-watch route: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool {
			for _, record := range capture.snapshot() {
				if record.Event == observability.EventSourceWatchStopped && record.Reason == observability.ReasonReadForbidden {
					return true
				}
			}
			return false
		})
		for _, record := range capture.snapshot() {
			if strings.Contains(record.AttemptID, "secret-watch-object-sentinel") || strings.Contains(record.Name, "raw-watch-body-sentinel") || strings.Contains(record.SourceID, "raw-watch-body-sentinel") {
				t.Fatalf("forbidden watch record retained raw data = %#v", record)
			}
		}
		cancel()
		watchRegistry.RemoveOwner(ownerKey)
		queue.ShutDown()
		release()
	})

	t.Run("owning cancellation emits no watch lifecycle signal", func(t *testing.T) {
		stream := watch.NewRaceFreeFake()
		watcher := newRuntimeScriptedMetadataWatcher(runtimeWatchResponse{stream: stream})
		capture := &observabilityCapture{}
		observer, err := observability.New(observability.Options{Registerer: prometheus.NewRegistry(), LogSink: capture})
		if err != nil {
			t.Fatalf("construct cancellation observer: %v", err)
		}
		tracker := reconciliation.NewFreshnessTracker()
		ownerKey := types.NamespacedName{Namespace: "team-a", Name: "observability-cancel-watch"}
		owner := newRuntimeKubeseer(ownerKey, "observability-cancel-uid", 1)
		tracker.Observe(owner)
		lease, _, release, err := tracker.Acquire(ctx, ownerKey, owner.UID, owner.Generation)
		if err != nil {
			t.Fatalf("acquire cancellation lease: %v", err)
		}
		defer release()
		route, err := mustRuntimeAuthorizedRoute(t, lease, target, snapshot)
		if err != nil {
			t.Fatalf("authorize cancellation route: %v", err)
		}
		watchContext, cancel := context.WithCancel(ctx)
		watchRegistry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithRouteWatchObserver(observer))
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := watchRegistry.Start(watchContext, queue); err != nil {
			t.Fatalf("start cancellation registry: %v", err)
		}
		if err := watchRegistry.Replace(context.Background(), lease, []reconciliation.AuthorizedRoute{route}); err != nil {
			t.Fatalf("replace cancellation route: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		cancel()
		waitForRuntimeWatchCondition(t, stream.IsStopped)
		if len(capture.snapshot()) != 0 {
			t.Fatalf("canceled watch emitted lifecycle records: %#v", capture.snapshot())
		}
		watchRegistry.RemoveOwner(ownerKey)
	})
}
