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
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func assertAuthorizationEnforcementWatchScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	planner := selection.NewPlanner(resolver)
	source := v1alpha1.KubeseerSource{
		ID:         "authorization-watch-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
	}
	target := mustRuntimeTarget(t, ctx, planner, "team-a", source)
	snapshot := mustSnapshot(t, basePolicy())

	t.Run("stale shared owner is pruned before restart and last current owner stops transport", func(t *testing.T) {
		watcher := newRuntimeScriptedMetadataWatcher()
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond))
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		currentOwner := types.NamespacedName{Namespace: "team-a", Name: "watch-current-owner"}
		staleOwner := types.NamespacedName{Namespace: "team-a", Name: "watch-stale-owner"}
		currentObject := newRuntimeKubeseer(currentOwner, "watch-current-uid", 1)
		staleObject := newRuntimeKubeseer(staleOwner, "watch-stale-uid", 1)
		tracker.Observe(currentObject)
		tracker.Observe(staleObject)
		currentLease, _, releaseCurrent, err := tracker.Acquire(ctx, currentOwner, currentObject.UID, currentObject.Generation)
		if err != nil {
			t.Fatalf("acquire current owner: %v", err)
		}
		defer releaseCurrent()
		staleLease, _, releaseStale, err := tracker.Acquire(ctx, staleOwner, staleObject.UID, staleObject.Generation)
		if err != nil {
			t.Fatalf("acquire stale owner: %v", err)
		}
		defer releaseStale()
		currentRoute, err := mustRuntimeAuthorizedRoute(t, currentLease, target, snapshot)
		if err != nil {
			t.Fatalf("authorize current owner: %v", err)
		}
		staleRoute, err := mustRuntimeAuthorizedRoute(t, staleLease, target, snapshot)
		if err != nil {
			t.Fatalf("authorize stale owner: %v", err)
		}
		if err := registry.Replace(currentLease, []reconciliation.AuthorizedRoute{currentRoute}); err != nil {
			t.Fatalf("replace current route: %v", err)
		}
		if err := registry.Replace(staleLease, []reconciliation.AuthorizedRoute{staleRoute}); err != nil {
			t.Fatalf("replace stale route: %v", err)
		}
		if err := registry.Start(watchContext, queue); err != nil {
			t.Fatalf("start shared watch registry: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		first := watcher.Calls()[0]
		if len(first.PermitSubjects) != 2 {
			t.Fatalf("initial watch permit subjects = %#v, want both owners", first.PermitSubjects)
		}

		updatedStale := staleObject.DeepCopy()
		updatedStale.Generation = 2
		tracker.Observe(updatedStale)
		if stream := watcher.Stream(0); stream != nil {
			stream.Stop()
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 2 })
		second := watcher.Calls()[1]
		if len(second.PermitSubjects) != 1 || second.PermitSubjects[0].Key != currentOwner {
			t.Fatalf("restart watch permit subjects = %#v, want only current owner", second.PermitSubjects)
		}
		registry.RemoveOwner(currentOwner)
		waitForRuntimeWatchCondition(t, func() bool { return registry.WatchCount() == 0 })
		if stream := watcher.Stream(0); stream != nil && !stream.IsStopped() {
			t.Fatal("last current owner removal left shared watch running")
		}
	})

	t.Run("policy epoch invalidation removes routes before a watch restart", func(t *testing.T) {
		watcher := newRuntimeScriptedMetadataWatcher()
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond))
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := registry.Start(watchContext, queue); err != nil {
			t.Fatalf("start epoch registry: %v", err)
		}
		owner := types.NamespacedName{Namespace: "team-a", Name: "watch-epoch-owner"}
		object := newRuntimeKubeseer(owner, "watch-epoch-uid", 1)
		tracker.Observe(object)
		lease, _, release, err := tracker.Acquire(ctx, owner, object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire epoch owner: %v", err)
		}
		defer release()
		route, err := mustRuntimeAuthorizedRoute(t, lease, target, snapshot)
		if err != nil {
			t.Fatalf("authorize epoch route: %v", err)
		}
		if err := registry.Replace(lease, []reconciliation.AuthorizedRoute{route}); err != nil {
			t.Fatalf("replace epoch route: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		tracker.InvalidateAll()
		registry.RemoveAll()
		stream := watcher.Stream(0)
		waitForRuntimeWatchCondition(t, func() bool { return registry.WatchCount() == 0 && stream != nil && stream.IsStopped() })
		calls := len(watcher.Calls())
		if calls != 1 {
			t.Fatalf("epoch invalidation opened %d watch attempts, want no restart", calls)
		}
	})

	t.Run("WATCH Forbidden emits one linked sanitized record", func(t *testing.T) {
		var records []authorization.Record
		var recordsMu sync.Mutex
		recorder := authorization.RecorderFunc(func(_ context.Context, observed []authorization.Record) {
			recordsMu.Lock()
			defer recordsMu.Unlock()
			records = append(records, observed...)
		})
		snapshotRecords := func() []authorization.Record {
			recordsMu.Lock()
			defer recordsMu.Unlock()
			return append([]authorization.Record(nil), records...)
		}
		watcher := newRuntimeScriptedMetadataWatcher(runtimeWatchResponse{err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "secret-watch-object-sentinel", errors.New("raw-watch-body-sentinel"))})
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond))
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := registry.Start(watchContext, queue); err != nil {
			t.Fatalf("start forbidden registry: %v", err)
		}
		owner := types.NamespacedName{Namespace: "team-a", Name: "watch-forbidden-owner"}
		object := newRuntimeKubeseer(owner, "watch-forbidden-uid", 1)
		tracker.Observe(object)
		lease, _, release, err := tracker.Acquire(ctx, owner, object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire forbidden owner: %v", err)
		}
		defer release()
		route, err := runtimeAuthorizedRouteWithRecorder(t, lease, target, snapshot, recorder)
		if err != nil {
			t.Fatalf("authorize forbidden route: %v", err)
		}
		if err := registry.Replace(lease, []reconciliation.AuthorizedRoute{route}); err != nil {
			t.Fatalf("replace forbidden route: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(snapshotRecords()) >= 2 })
		registry.RemoveOwner(owner)
		observedRecords := snapshotRecords()
		if len(observedRecords) < 2 || observedRecords[0].Kind != authorization.RecordPolicyDecision || observedRecords[1].Kind != authorization.RecordReadForbidden || observedRecords[1].Outcome != authorization.OutcomeForbidden {
			t.Fatalf("WATCH forbidden evidence = %#v", observedRecords)
		}
		for _, record := range observedRecords {
			if strings.Contains(fmt.Sprintf("%#v", record), "secret-watch-object-sentinel") || strings.Contains(fmt.Sprintf("%#v", record), "raw-watch-body-sentinel") {
				t.Fatalf("WATCH forbidden evidence leaked API details: %#v", record)
			}
		}
	})
}
