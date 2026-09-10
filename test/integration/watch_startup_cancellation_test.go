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
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertWatchStartupCancellation(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	t.Run("evaluation deadline releases readiness and publishes timeout", func(t *testing.T) {
		profile := watchStartupProfile(t, 35*time.Millisecond)
		watcher := newStartupBlockingWatcher(false)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start deadline registry: %v", err)
		}
		key := types.NamespacedName{Namespace: "team-a", Name: "watch-deadline-owner"}
		source := runtimePipelineValuesSource("watch-deadline-source")
		reader := newRuntimePipelineReader(runtimePipelineKubeseer(key, "watch-deadline-uid", 1, source))
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, nil, nil)
		publisher := &runtimePipelinePublisher{}
		runtime := mustWatchStartupRuntime(t, resolver, reader, lister, basePolicy(), registry, publisher, tracker, profile)

		done := make(chan error, 1)
		startedAt := time.Now()
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			done <- err
		}()
		watcher.WaitCalls(t, 1)
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), string(reconciliation.ReasonEvaluationTimedOut)) {
				t.Fatalf("deadline reconciliation error = %v, want EvaluationTimedOut", err)
			}
		case <-time.After(time.Second):
			t.Fatal("deadline reconciliation remained blocked behind WATCH startup")
		}
		if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
			t.Fatalf("deadline release took %s", elapsed)
		}
		publications := publisher.Publications()
		if len(publications) != 1 || publications[0].Evaluation.Result == nil || publications[0].Result.Sources[0].Error == nil || publications[0].Result.Sources[0].Error.Reason != string(reconciliation.ReasonEvaluationTimedOut) {
			t.Fatalf("deadline publication = %#v", publications)
		}
		if got := len(watcher.Calls()); got != 1 {
			t.Fatalf("deadline watcher calls = %d, want one while manager remains active", got)
		}
		close(watcher.release)
		cancelManager()
		watcher.WaitStopped(t, 0)
	})

	t.Run("external cancellation suppresses publication while manager stays active", func(t *testing.T) {
		profile := watchStartupProfile(t, time.Second)
		watcher := newStartupBlockingWatcher(false)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start external-cancel registry: %v", err)
		}
		key := types.NamespacedName{Namespace: "team-a", Name: "watch-cancel-owner"}
		source := runtimePipelineValuesSource("watch-cancel-source")
		reader := newRuntimePipelineReader(runtimePipelineKubeseer(key, "watch-cancel-uid", 1, source))
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, nil, nil)
		publisher := &runtimePipelinePublisher{}
		runtime := mustWatchStartupRuntime(t, resolver, reader, lister, basePolicy(), registry, publisher, tracker, profile)
		attemptContext, cancelAttempt := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(attemptContext, reconcile.Request{NamespacedName: key})
			done <- err
		}()
		watcher.WaitCalls(t, 1)
		cancelAttempt()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("external cancellation returned error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("external cancellation remained blocked behind WATCH startup")
		}
		if got := len(publisher.Publications()); got != 0 {
			t.Fatalf("external cancellation published %d candidates", got)
		}
		if got := len(watcher.Calls()); got != 1 {
			t.Fatalf("external cancellation watcher calls = %d, want one with manager active", got)
		}
		close(watcher.release)
		cancelManager()
		watcher.WaitStopped(t, 0)
	})

	for _, invalidation := range []struct {
		name   string
		mutate func(*reconciliation.FreshnessTracker, *v1alpha1.Kubeseer)
	}{
		{name: "uid", mutate: func(tracker *reconciliation.FreshnessTracker, object *v1alpha1.Kubeseer) {
			updated := object.DeepCopy()
			updated.UID = types.UID("watch-invalidated-new-uid")
			tracker.Observe(updated)
		}},
		{name: "generation", mutate: func(tracker *reconciliation.FreshnessTracker, object *v1alpha1.Kubeseer) {
			updated := object.DeepCopy()
			updated.Generation++
			tracker.Observe(updated)
		}},
		{name: "deletion", mutate: func(tracker *reconciliation.FreshnessTracker, object *v1alpha1.Kubeseer) {
			updated := object.DeepCopy()
			deleting := metav1.Now()
			updated.DeletionTimestamp = &deleting
			tracker.Observe(updated)
		}},
		{name: "policy epoch", mutate: func(tracker *reconciliation.FreshnessTracker, _ *v1alpha1.Kubeseer) {
			tracker.InvalidateAll()
		}},
	} {
		invalidation := invalidation
		t.Run("lease invalidation by "+invalidation.name+" suppresses stale publication", func(t *testing.T) {
			profile := watchStartupProfile(t, time.Second)
			watcher := newStartupBlockingWatcher(true)
			tracker := reconciliation.NewFreshnessTracker()
			registry := reconciliation.NewRouteRegistry(
				watcher,
				tracker,
				reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
				reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
			)
			managerContext, cancelManager := context.WithCancel(ctx)
			queue := newRuntimeQueue()
			defer queue.ShutDown()
			if err := registry.Start(managerContext, queue); err != nil {
				t.Fatalf("start %s invalidation registry: %v", invalidation.name, err)
			}
			key := types.NamespacedName{Namespace: "team-a", Name: "watch-invalidated-" + strings.ReplaceAll(invalidation.name, " ", "-")}
			source := runtimePipelineValuesSource("watch-invalidated-source")
			object := runtimePipelineKubeseer(key, "watch-invalidated-uid", 1, source)
			reader := newRuntimePipelineReader(object)
			lister := newRuntimePipelineLister()
			lister.SetResponse(source.ID, nil, nil)
			publisher := &runtimePipelinePublisher{}
			runtime := mustWatchStartupRuntime(t, resolver, reader, lister, basePolicy(), registry, publisher, tracker, profile)
			done := make(chan error, 1)
			go func() {
				_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
				done <- err
			}()
			watcher.WaitCalls(t, 1)
			invalidation.mutate(tracker, object)
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("%s invalidation returned error = %v", invalidation.name, err)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s invalidation remained blocked behind WATCH startup", invalidation.name)
			}
			if got := len(publisher.Publications()); got != 0 {
				t.Fatalf("%s invalidation published %d stale candidates", invalidation.name, got)
			}
			close(watcher.release)
			cancelManager()
			watcher.WaitStopped(t, 0)
		})
	}

	t.Run("newer generation progresses after the obsolete startup is canceled", func(t *testing.T) {
		profile := watchStartupProfile(t, time.Second)
		watcher := newStartupBlockingWatcher(true)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start generation registry: %v", err)
		}
		key := types.NamespacedName{Namespace: "team-a", Name: "watch-generation-owner"}
		source := runtimePipelineValuesSource("watch-generation-source")
		object := runtimePipelineKubeseer(key, "watch-generation-uid", 1, source)
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, nil, nil)
		publisher := &runtimePipelinePublisher{}
		runtime := mustWatchStartupRuntime(t, resolver, reader, lister, basePolicy(), registry, publisher, tracker, profile)
		oldDone := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			oldDone <- err
		}()
		watcher.WaitCalls(t, 1)
		updated := object.DeepCopy()
		updated.Generation = 2
		setRuntimePipelineObject(reader, updated)
		tracker.Observe(updated)
		select {
		case err := <-oldDone:
			if err != nil {
				t.Fatalf("obsolete generation returned error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("obsolete generation did not release after lease cancellation")
		}
		newDone := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			newDone <- err
		}()
		watcher.WaitCalls(t, 2)
		close(watcher.release)
		select {
		case err := <-newDone:
			if err != nil {
				t.Fatalf("new generation returned error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("new generation waited for obsolete WATCH response")
		}
		if got := len(publisher.Publications()); got != 1 {
			t.Fatalf("new generation publications = %d, want one", got)
		}
		cancelManager()
		watcher.WaitStopped(t, 1)
	})

	t.Run("shared pending startup reuses one readiness signal and preserves owners", func(t *testing.T) {
		planner := selection.NewPlanner(resolver)
		snapshot := mustSnapshot(t, basePolicy())
		sourceA := runtimePipelineValuesSource("shared-startup-a")
		sourceB := runtimePipelineValuesSource("shared-startup-b")
		targetA := mustRuntimeTarget(t, ctx, planner, "team-a", sourceA)
		targetB := mustRuntimeTarget(t, ctx, planner, "team-a", sourceB)
		ownerA := types.NamespacedName{Namespace: "team-a", Name: "shared-startup-a"}
		ownerB := types.NamespacedName{Namespace: "team-a", Name: "shared-startup-b"}
		tracker := reconciliation.NewFreshnessTracker()
		watcher := newStartupBlockingWatcher(false)
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start shared startup registry: %v", err)
		}
		objectA := newRuntimeKubeseer(ownerA, "shared-startup-uid-a", 1)
		objectB := newRuntimeKubeseer(ownerB, "shared-startup-uid-b", 1)
		tracker.Observe(objectA)
		tracker.Observe(objectB)
		leaseA, _, releaseA, err := tracker.Acquire(context.Background(), ownerA, objectA.UID, objectA.Generation)
		if err != nil {
			t.Fatalf("acquire shared owner A: %v", err)
		}
		defer releaseA()
		leaseB, _, releaseB, err := tracker.Acquire(context.Background(), ownerB, objectB.UID, objectB.Generation)
		if err != nil {
			t.Fatalf("acquire shared owner B: %v", err)
		}
		defer releaseB()
		routeA, err := mustRuntimeAuthorizedRoute(t, leaseA, targetA, snapshot)
		if err != nil {
			t.Fatalf("authorize shared owner A: %v", err)
		}
		routeB, err := mustRuntimeAuthorizedRoute(t, leaseB, targetB, snapshot)
		if err != nil {
			t.Fatalf("authorize shared owner B: %v", err)
		}
		ownerADone := make(chan error, 1)
		go func() {
			ownerADone <- registry.Replace(context.Background(), leaseA, []reconciliation.AuthorizedRoute{routeA})
		}()
		watcher.WaitCalls(t, 1)
		ownerBContext, cancelOwnerB := context.WithCancel(context.Background())
		ownerBDone := make(chan error, 1)
		go func() {
			ownerBDone <- registry.Replace(ownerBContext, leaseB, []reconciliation.AuthorizedRoute{routeB})
		}()
		cancelOwnerB()
		select {
		case err := <-ownerBDone:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled shared owner B error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled shared owner B remained blocked")
		}
		if got := len(watcher.Calls()); got != 1 {
			t.Fatalf("shared owners opened %d WATCH transports", got)
		}
		close(watcher.release)
		select {
		case err := <-ownerADone:
			if err != nil {
				t.Fatalf("owner A replacement error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("owner A did not observe the shared readiness signal")
		}
		if got := registry.WatchCount(); got != 1 {
			t.Fatalf("shared watch count = %d, want one", got)
		}
		owners := registry.Owners(routeA.Address())
		if len(owners) != 2 || owners[0] != ownerA || owners[1] != ownerB {
			t.Fatalf("shared owners = %#v", owners)
		}
		stream := watcher.Stream(0)
		if stream == nil || stream.IsStopped() {
			t.Fatal("shared stream did not survive caller completion")
		}
		stream.Action(watch.Modified, runtimeMetadataObject("shared-rv", "watch-value-must-not-enter-routing"))
		got := collectRuntimeQueueKeys(t, queue, 2)
		if !got[ownerA] || !got[ownerB] || len(got) != 2 {
			t.Fatalf("shared event owners = %#v", got)
		}
		registry.RemoveOwner(ownerA)
		registry.RemoveOwner(ownerB)
		releaseA()
		releaseB()
		cancelManager()
		waitForRuntimeWatchCondition(t, func() bool { return stream.IsStopped() && registry.WatchCount() == 0 })
	})

	t.Run("all blocked workers release by their own evaluation deadlines", func(t *testing.T) {
		profile := watchStartupProfile(t, 35*time.Millisecond)
		watcher := newStartupBlockingWatcher(false)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start saturation registry: %v", err)
		}
		const workers = 4
		objects := make([]*v1alpha1.Kubeseer, 0, workers)
		for index := 0; index < workers; index++ {
			key := types.NamespacedName{Namespace: "team-a", Name: fmt.Sprintf("watch-saturation-%d", index)}
			source := runtimePipelineValuesSource(fmt.Sprintf("watch-saturation-source-%d", index))
			switch index {
			case 1:
				source.Resource = v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"}
			case 2:
				source.Namespaces = &v1alpha1.NamespaceSelection{Names: []string{"team-d"}}
			case 3:
				source.Resource = v1alpha1.ResourceReference{APIVersion: "widgets.kubeseer.io/v1", Kind: "Widget"}
				source.Namespaces = &v1alpha1.NamespaceSelection{Names: []string{"team-e"}}
			}
			objects = append(objects, runtimePipelineKubeseer(key, types.UID(fmt.Sprintf("watch-saturation-uid-%d", index)), 1, source))
		}
		reader := newRuntimePipelineReader(objects...)
		lister := newRuntimePipelineLister()
		for _, object := range objects {
			lister.SetResponse(object.Spec.Sources[0].ID, nil, nil)
		}
		publisher := &runtimePipelinePublisher{}
		policy := basePolicy()
		policy.Spec.Namespaces.Include = []string{"team-a", "team-c", "team-d", "team-e"}
		runtime := mustWatchStartupRuntime(t, resolver, reader, lister, policy, registry, publisher, tracker, profile)
		done := make(chan error, workers)
		startedAt := time.Now()
		for _, object := range objects {
			key := types.NamespacedName{Namespace: object.Namespace, Name: object.Name}
			go func() {
				_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
				done <- err
			}()
		}
		watcher.WaitCalls(t, workers)
		for index := 0; index < workers; index++ {
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), string(reconciliation.ReasonEvaluationTimedOut)) {
					t.Fatalf("saturated worker error = %v, want EvaluationTimedOut", err)
				}
			case <-time.After(time.Second):
				t.Fatal("a saturated worker remained blocked behind WATCH startup")
			}
		}
		if elapsed := time.Since(startedAt); elapsed > 700*time.Millisecond {
			t.Fatalf("saturated workers released after %s", elapsed)
		}
		if got := len(publisher.Publications()); got != workers {
			t.Fatalf("saturated timeout publications = %d, want %d", got, workers)
		}
		close(watcher.release)
		cancelManager()
		watcher.WaitStopped(t, workers-1)
	})
}

func assertWatchStartupRecovery(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	t.Run("failed establishment enqueues a current owner for a fresh LIST", func(t *testing.T) {
		gate := make(chan struct{})
		watcher := newStartupRecoveryWatcher(
			startupRecoveryWatchResponse{err: errors.New("simulated startup failure")},
			startupRecoveryWatchResponse{gate: gate},
		)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start recovery registry: %v", err)
		}
		key := types.NamespacedName{Namespace: "team-a", Name: "recovery-startup-owner"}
		source := runtimePipelineValuesSource("recovery-startup-source")
		object := runtimePipelineKubeseer(key, "recovery-startup-uid", 1, source)
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, runtimePipelineValueList("startup-resource", "startup-resource-uid", "before-recovery", "7"))
		publisher := &runtimePipelinePublisher{}
		runtime := mustWatchStartupRuntime(t, resolver, reader, lister, basePolicy(), registry, publisher, tracker, watchStartupProfile(t, time.Second))
		done := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			done <- err
		}()
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("initial failed-startup reconciliation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("initial failed-startup reconciliation remained blocked")
		}
		_ = collectRuntimeQueueKeys(t, queue, 1)
		lister.SetResponse(source.ID, runtimePipelineValueList("startup-resource", "startup-resource-uid", "after-recovery", "8"))
		close(gate)
		waitForRuntimeWatchCondition(t, func() bool { return watcher.Stream(0) != nil })
		if got := collectRuntimeQueueKeys(t, queue, 1); !got[key] {
			t.Fatalf("failed-startup recovery queue = %#v", got)
		}
		secondDone := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			secondDone <- err
		}()
		select {
		case err := <-secondDone:
			if err != nil {
				t.Fatalf("recovery LIST reconciliation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("recovery LIST reconciliation remained blocked")
		}
		assertRuntimePipelinePublishedValue(t, publisher.Publications(), key, "after-recovery")
		registry.RemoveOwner(key)
		cancelManager()
		waitForRuntimeWatchCondition(t, func() bool { return registry.WatchCount() == 0 })
	})

	t.Run("reconnect enqueues owners after the stream gap", func(t *testing.T) {
		first := watch.NewRaceFreeFake()
		gate := make(chan struct{})
		watcher := newStartupRecoveryWatcher(
			startupRecoveryWatchResponse{stream: first},
			startupRecoveryWatchResponse{gate: gate},
		)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start reconnect registry: %v", err)
		}
		key := types.NamespacedName{Namespace: "team-a", Name: "recovery-reconnect-owner"}
		source := runtimePipelineValuesSource("recovery-reconnect-source")
		object := runtimePipelineKubeseer(key, "recovery-reconnect-uid", 1, source)
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, runtimePipelineValueList("reconnect-resource", "reconnect-resource-uid", "before-reconnect", "9"))
		publisher := &runtimePipelinePublisher{}
		runtime := mustWatchStartupRuntime(t, resolver, reader, lister, basePolicy(), registry, publisher, tracker, watchStartupProfile(t, time.Second))
		done := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			done <- err
		}()
		waitForRuntimeWatchCondition(t, func() bool { return watcher.Stream(0) != nil })
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("initial reconnect reconciliation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("initial reconnect reconciliation remained blocked")
		}
		first.Stop()
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 2 })
		lister.SetResponse(source.ID, runtimePipelineValueList("reconnect-resource", "reconnect-resource-uid", "after-reconnect", "10"))
		close(gate)
		waitForRuntimeWatchCondition(t, func() bool { return watcher.Stream(1) != nil })
		if got := collectRuntimeQueueKeys(t, queue, 1); !got[key] {
			t.Fatalf("reconnect recovery queue = %#v", got)
		}
		secondDone := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			secondDone <- err
		}()
		select {
		case err := <-secondDone:
			if err != nil {
				t.Fatalf("reconnect LIST reconciliation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("reconnect LIST reconciliation remained blocked")
		}
		assertRuntimePipelinePublishedValue(t, publisher.Publications(), key, "after-reconnect")
		registry.RemoveOwner(key)
		cancelManager()
		waitForRuntimeWatchCondition(t, func() bool { return registry.WatchCount() == 0 })
	})

	t.Run("capacity promotion enqueues the deferred current owner", func(t *testing.T) {
		gate := make(chan struct{})
		first := watch.NewRaceFreeFake()
		watcher := newStartupRecoveryWatcher(
			startupRecoveryWatchResponse{stream: first},
			startupRecoveryWatchResponse{gate: gate},
		)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
			reconciliation.WithMaxActiveWatches(1),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start promotion registry: %v", err)
		}
		ownerA := types.NamespacedName{Namespace: "team-a", Name: "recovery-promotion-a"}
		ownerB := types.NamespacedName{Namespace: "team-a", Name: "recovery-promotion-b"}
		sourceA := runtimePipelineValuesSource("recovery-promotion-a-source")
		sourceB := runtimePipelineValuesSource("recovery-promotion-b-source")
		sourceB.Resource = v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"}
		objectA := runtimePipelineKubeseer(ownerA, "recovery-promotion-uid-a", 1, sourceA)
		objectB := runtimePipelineKubeseer(ownerB, "recovery-promotion-uid-b", 1, sourceB)
		reader := newRuntimePipelineReader(objectA, objectB)
		lister := newRuntimePipelineLister()
		lister.SetResponse(sourceA.ID, runtimePipelineValueList("promotion-a-resource", "promotion-a-resource-uid", "a-before", "1"))
		lister.SetResponse(sourceB.ID, runtimePipelineValueList("promotion-b-resource", "promotion-b-resource-uid", "b-before", "2"))
		publisher := &runtimePipelinePublisher{}
		profile := watchStartupProfileWithMaxWatches(t, time.Second, 1)
		runtime := mustWatchStartupRuntime(t, resolver, reader, lister, basePolicy(), registry, publisher, tracker, profile)
		for _, key := range []types.NamespacedName{ownerA, ownerB} {
			done := make(chan error, 1)
			go func(key types.NamespacedName) {
				_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
				done <- err
			}(key)
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("initial promotion reconciliation for %s = %v", key.Name, err)
				}
			case <-time.After(time.Second):
				t.Fatalf("initial promotion reconciliation for %s remained blocked", key.Name)
			}
		}
		if got := len(watcher.Calls()); got != 1 {
			t.Fatalf("capacity-deferred startup opened %d WATCH calls", got)
		}
		lister.SetResponse(sourceB.ID, runtimePipelineValueList("promotion-b-resource", "promotion-b-resource-uid", "b-after-promotion", "3"))
		registry.RemoveOwner(ownerA)
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 2 })
		close(gate)
		waitForRuntimeWatchCondition(t, func() bool { return watcher.Stream(1) != nil })
		if got := collectRuntimeQueueKeys(t, queue, 1); !got[ownerB] {
			t.Fatalf("promotion recovery queue = %#v", got)
		}
		done := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: ownerB})
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("promotion recovery reconciliation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("promotion recovery LIST remained blocked")
		}
		assertRuntimePipelinePublishedValue(t, publisher.Publications(), ownerB, "b-after-promotion")
		registry.RemoveOwner(ownerB)
		cancelManager()
		waitForRuntimeWatchCondition(t, func() bool { return registry.WatchCount() == 0 })
	})

	t.Run("periodic safety enqueue remains available without a stream", func(t *testing.T) {
		gate := make(chan struct{})
		watcher := newStartupRecoveryWatcher(
			startupRecoveryWatchResponse{err: errors.New("periodic startup failure")},
			startupRecoveryWatchResponse{gate: gate},
		)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		key := types.NamespacedName{Namespace: "team-a", Name: "recovery-periodic-owner"}
		source := runtimePipelineValuesSource("recovery-periodic-source")
		object := runtimePipelineKubeseer(key, "recovery-periodic-uid", 1, source)
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, runtimePipelineValueList("periodic-resource", "periodic-resource-uid", "periodic", "4"))
		publisher := &runtimePipelinePublisher{}
		profile := watchStartupProfile(t, time.Second)
		runtime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: 10 * time.Millisecond, LimitProfile: &profile}, reconciliation.Dependencies{
			Reader:       reader,
			Lister:       reader,
			PolicySource: &runtimePipelinePolicySource{policy: basePolicy()},
			Enforcer:     authorization.NewEnforcer(nil),
			Planner:      selection.NewPlanner(resolver),
			Executor:     selection.NewExecutor(lister, selection.WithVerifier(authorization.VerifierFunc(func(subject authorization.Subject) bool { return subject.Validate() == nil }))),
			Routes:       registry,
			Publisher:    publisher,
			Tracker:      tracker,
		})
		if err != nil {
			t.Fatalf("construct periodic runtime: %v", err)
		}
		if err := runtime.TriggerSource().Start(managerContext, queue); err != nil {
			t.Fatalf("start periodic trigger: %v", err)
		}
		_ = collectRuntimeQueueKeys(t, queue, 1)
		done := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			done <- err
		}()
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("periodic startup reconciliation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("periodic startup reconciliation remained blocked")
		}
		_ = collectRuntimeQueueKeys(t, queue, 1)
		waitForRuntimeWatchCondition(t, func() bool { return queue.Len() > 0 })
		if got := collectRuntimeQueueKeys(t, queue, 1); !got[key] {
			t.Fatalf("periodic recovery queue = %#v", got)
		}
		if watcher.Stream(0) != nil {
			t.Fatal("periodic safety test unexpectedly established a stream")
		}
		close(gate)
		cancelManager()
	})

	t.Run("revoked owners are not re-enqueued on recovery", func(t *testing.T) {
		gate := make(chan struct{})
		watcher := newStartupRecoveryWatcher(
			startupRecoveryWatchResponse{err: errors.New("revoked startup failure")},
			startupRecoveryWatchResponse{gate: gate},
		)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start revoked-owner registry: %v", err)
		}
		key := types.NamespacedName{Namespace: "team-a", Name: "recovery-revoked-owner"}
		source := runtimePipelineValuesSource("recovery-revoked-source")
		object := runtimePipelineKubeseer(key, "recovery-revoked-uid", 1, source)
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, runtimePipelineValueList("revoked-resource", "revoked-resource-uid", "revoked", "5"))
		publisher := &runtimePipelinePublisher{}
		runtime := mustWatchStartupRuntime(t, resolver, reader, lister, basePolicy(), registry, publisher, tracker, watchStartupProfile(t, time.Second))
		done := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			done <- err
		}()
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("revoked-owner startup reconciliation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("revoked-owner startup reconciliation remained blocked")
		}
		_ = collectRuntimeQueueKeys(t, queue, 1)
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 2 })
		tracker.InvalidateAll()
		close(gate)
		waitForRuntimeWatchCondition(t, func() bool { return watcher.Stream(0) != nil })
		watcher.Stream(0).Stop()
		cancelManager()
		registry.RemoveOwner(key)
		waitForRuntimeWatchCondition(t, func() bool { return registry.WatchCount() == 0 })
		if queue.Len() != 0 {
			t.Fatalf("revoked-owner recovery queued %d stale identities", queue.Len())
		}
	})
}

func assertWatchSupervisorEstablishment(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	t.Run("cooperative establishment timeout cancels transport and retries serially", func(t *testing.T) {
		watcher := newStartupBlockingWatcher(true)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(25*time.Millisecond),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start establishment registry: %v", err)
		}
		planner := selection.NewPlanner(resolver)
		source := runtimePipelineValuesSource("establishment-timeout-source")
		target := mustRuntimeTarget(t, ctx, planner, "team-a", source)
		object := newRuntimeKubeseer(types.NamespacedName{Namespace: "team-a", Name: "establishment-timeout-owner"}, "establishment-timeout-uid", 1)
		tracker.Observe(object)
		lease, _, release, err := tracker.Acquire(context.Background(), objectKey(object), object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire establishment lease: %v", err)
		}
		defer release()
		route, err := mustRuntimeAuthorizedRoute(t, lease, target, mustSnapshot(t, basePolicy()))
		if err != nil {
			t.Fatalf("authorize establishment route: %v", err)
		}
		replaceDone := make(chan error, 1)
		go func() {
			replaceDone <- registry.Replace(context.Background(), lease, []reconciliation.AuthorizedRoute{route})
		}()
		watcher.WaitCalls(t, 1)
		watcher.WaitCanceled(t)
		select {
		case err := <-replaceDone:
			if err != nil {
				t.Fatalf("establishment replacement error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("establishment replacement did not release after startup timeout")
		}
		watcher.WaitCalls(t, 2)
		if got := watcher.MaxActive(); got != 1 {
			t.Fatalf("concurrent WATCH establishments = %d, want serial attempts", got)
		}
		registry.RemoveOwner(objectKey(object))
		cancelManager()
	})

	t.Run("late streams are stopped and cannot resurrect a replacement supervisor", func(t *testing.T) {
		watcher := newStartupBlockingWatcher(false)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start late-stream registry: %v", err)
		}
		planner := selection.NewPlanner(resolver)
		snapshot := mustSnapshot(t, basePolicy())
		source := runtimePipelineValuesSource("late-stream-source")
		target := mustRuntimeTarget(t, ctx, planner, "team-a", source)
		owner := types.NamespacedName{Namespace: "team-a", Name: "late-stream-owner"}
		object := newRuntimeKubeseer(owner, "late-stream-uid", 1)
		tracker.Observe(object)
		lease, _, release, err := tracker.Acquire(context.Background(), owner, object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire late-stream lease: %v", err)
		}
		route, err := mustRuntimeAuthorizedRoute(t, lease, target, snapshot)
		if err != nil {
			release()
			t.Fatalf("authorize late-stream route: %v", err)
		}
		replaceDone := make(chan error, 1)
		go func() {
			replaceDone <- registry.Replace(context.Background(), lease, []reconciliation.AuthorizedRoute{route})
		}()
		watcher.WaitCalls(t, 1)
		registry.RemoveOwner(owner)
		select {
		case err := <-replaceDone:
			if err != nil {
				t.Fatalf("removed owner replacement error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("removed owner replacement remained blocked")
		}
		if registry.WatchCount() != 0 {
			t.Fatalf("removed owner left %d supervisors", registry.WatchCount())
		}
		close(watcher.release)
		waitForRuntimeWatchCondition(t, func() bool {
			stream := watcher.Stream(0)
			return stream != nil && stream.IsStopped()
		})
		if queue.Len() != 0 || len(registry.Owners(route.Address())) != 0 {
			t.Fatalf("late stream restored routing: queue=%d owners=%#v", queue.Len(), registry.Owners(route.Address()))
		}
		release()

		updated := object.DeepCopy()
		updated.Generation = 2
		tracker.Observe(updated)
		lease2, _, release2, err := tracker.Acquire(context.Background(), owner, updated.UID, updated.Generation)
		if err != nil {
			t.Fatalf("acquire replacement lease: %v", err)
		}
		route2, err := mustRuntimeAuthorizedRoute(t, lease2, target, snapshot)
		if err != nil {
			release2()
			t.Fatalf("authorize replacement route: %v", err)
		}
		replacementDone := make(chan error, 1)
		go func() {
			replacementDone <- registry.Replace(context.Background(), lease2, []reconciliation.AuthorizedRoute{route2})
		}()
		watcher.WaitCalls(t, 2)
		select {
		case err := <-replacementDone:
			if err != nil {
				t.Fatalf("replacement supervisor error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("replacement supervisor did not establish")
		}
		stream := watcher.Stream(1)
		if stream == nil || stream.IsStopped() {
			t.Fatal("replacement stream is not active")
		}
		if old := watcher.Stream(0); old == nil || !old.IsStopped() {
			t.Fatal("late stream was not disposed before replacement became active")
		}
		stream.Action(watch.Modified, runtimeMetadataObject("late-stream-replacement-rv", "late-stream-value"))
		got := collectRuntimeQueueKeys(t, queue, 1)
		if !got[owner] || len(got) != 1 {
			t.Fatalf("replacement event owners = %#v", got)
		}
		registry.RemoveOwner(owner)
		release2()
		cancelManager()
		waitForRuntimeWatchCondition(t, func() bool { return stream.IsStopped() && registry.WatchCount() == 0 })
	})

	t.Run("reauthorization removes a retrying supervisor", func(t *testing.T) {
		watcher := newStartupBlockingWatcher(true)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond),
			reconciliation.WithRouteWatchEstablishmentTimeout(25*time.Millisecond),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start reauthorization registry: %v", err)
		}
		planner := selection.NewPlanner(resolver)
		source := runtimePipelineValuesSource("reauthorization-source")
		target := mustRuntimeTarget(t, ctx, planner, "team-a", source)
		owner := types.NamespacedName{Namespace: "team-a", Name: "reauthorization-owner"}
		object := newRuntimeKubeseer(owner, "reauthorization-uid", 1)
		tracker.Observe(object)
		lease, _, release, err := tracker.Acquire(context.Background(), owner, object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire reauthorization lease: %v", err)
		}
		defer release()
		route, err := mustRuntimeAuthorizedRoute(t, lease, target, mustSnapshot(t, basePolicy()))
		if err != nil {
			t.Fatalf("authorize reauthorization route: %v", err)
		}
		replaceDone := make(chan error, 1)
		go func() {
			replaceDone <- registry.Replace(context.Background(), lease, []reconciliation.AuthorizedRoute{route})
		}()
		watcher.WaitCalls(t, 1)
		tracker.InvalidateAll()
		_ = registry.Owners(route.Address())
		watcher.WaitCanceled(t)
		select {
		case err := <-replaceDone:
			if err != nil {
				t.Fatalf("reauthorization replacement error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("reauthorization replacement remained blocked")
		}
		time.Sleep(5 * time.Millisecond)
		if got := len(watcher.Calls()); got != 1 {
			t.Fatalf("reauthorization opened %d attempts after revocation", got)
		}
		cancelManager()
	})

	t.Run("manager shutdown cancels transport and readiness", func(t *testing.T) {
		watcher := newStartupBlockingWatcher(true)
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			watcher,
			tracker,
			reconciliation.WithRouteWatchEstablishmentTimeout(5*time.Second),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start shutdown registry: %v", err)
		}
		planner := selection.NewPlanner(resolver)
		source := runtimePipelineValuesSource("shutdown-source")
		target := mustRuntimeTarget(t, ctx, planner, "team-a", source)
		owner := types.NamespacedName{Namespace: "team-a", Name: "shutdown-owner"}
		object := newRuntimeKubeseer(owner, "shutdown-uid", 1)
		tracker.Observe(object)
		lease, _, release, err := tracker.Acquire(context.Background(), owner, object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire shutdown lease: %v", err)
		}
		defer release()
		route, err := mustRuntimeAuthorizedRoute(t, lease, target, mustSnapshot(t, basePolicy()))
		if err != nil {
			t.Fatalf("authorize shutdown route: %v", err)
		}
		replaceDone := make(chan error, 1)
		go func() {
			replaceDone <- registry.Replace(context.Background(), lease, []reconciliation.AuthorizedRoute{route})
		}()
		watcher.WaitCalls(t, 1)
		cancelManager()
		watcher.WaitCanceled(t)
		select {
		case <-replaceDone:
		case <-time.After(time.Second):
			t.Fatal("manager shutdown did not release readiness")
		}
	})

	t.Run("client metadata transport cancels a blocked response header", func(t *testing.T) {
		requestStarted := make(chan struct{})
		requestCanceled := make(chan struct{})
		transport := &startupBlockingRoundTripper{
			started:  requestStarted,
			canceled: requestCanceled,
		}
		config := &rest.Config{
			Host:      "https://metadata.invalid",
			APIPath:   "/api",
			Transport: transport,
			ContentConfig: rest.ContentConfig{
				GroupVersion:         &schema.GroupVersion{Version: "v1"},
				NegotiatedSerializer: clientscheme.Codecs.WithoutConversion(),
			},
		}
		metadataClient, err := metadata.NewForConfig(config)
		if err != nil {
			t.Fatalf("construct metadata client: %v", err)
		}
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(
			reconciliation.NewClientMetadataWatcher(metadataClient),
			tracker,
			reconciliation.WithRouteWatchEstablishmentTimeout(25*time.Millisecond),
		)
		managerContext, cancelManager := context.WithCancel(ctx)
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		if err := registry.Start(managerContext, queue); err != nil {
			t.Fatalf("start HTTP registry: %v", err)
		}
		planner := selection.NewPlanner(resolver)
		source := runtimePipelineValuesSource("http-startup-source")
		target := mustRuntimeTarget(t, ctx, planner, "team-a", source)
		owner := types.NamespacedName{Namespace: "team-a", Name: "http-startup-owner"}
		object := newRuntimeKubeseer(owner, "http-startup-uid", 1)
		tracker.Observe(object)
		lease, _, release, err := tracker.Acquire(context.Background(), owner, object.UID, object.Generation)
		if err != nil {
			t.Fatalf("acquire HTTP lease: %v", err)
		}
		defer release()
		route, err := mustRuntimeAuthorizedRoute(t, lease, target, mustSnapshot(t, basePolicy()))
		if err != nil {
			t.Fatalf("authorize HTTP route: %v", err)
		}
		replaceDone := make(chan error, 1)
		go func() {
			replaceDone <- registry.Replace(context.Background(), lease, []reconciliation.AuthorizedRoute{route})
		}()
		select {
		case <-requestStarted:
		case <-time.After(time.Second):
			t.Fatal("HTTP WATCH request did not reach the blocked-header fixture")
		}
		select {
		case err := <-replaceDone:
			if err != nil {
				t.Fatalf("HTTP replacement error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("HTTP replacement did not release after establishment timeout")
		}
		select {
		case <-requestCanceled:
		case <-time.After(time.Second):
			t.Fatal("HTTP WATCH transport ignored establishment cancellation")
		}
		registry.RemoveOwner(owner)
		cancelManager()
	})
}

func runtimePipelineValueList(name string, uid types.UID, good, bad string) *unstructured.UnstructuredList {
	return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*runtimePipelineResource(name, uid, good, bad)}}
}

func assertRuntimePipelinePublishedValue(t *testing.T, publications []runtimePipelinePublication, key types.NamespacedName, want string) {
	t.Helper()
	publication, ok := latestRuntimePipelinePublication(publications, key)
	if !ok || len(publication.Result.Sources) != 1 || len(publication.Result.Sources[0].Resources) != 1 {
		t.Fatalf("publication for %s/%s = %#v", key.Namespace, key.Name, publication)
	}
	resource := publication.Result.Sources[0].Resources[0]
	for _, field := range resource.Fields {
		if field.Name == "good" && len(field.Matches) == 1 && field.Matches[0].StringValue != nil {
			if *field.Matches[0].StringValue != want {
				t.Fatalf("published value = %q, want %q", *field.Matches[0].StringValue, want)
			}
			return
		}
	}
	t.Fatalf("publication has no good field value %q: %#v", want, resource)
}

func watchStartupProfileWithMaxWatches(t *testing.T, timeout time.Duration, maxWatches int) limits.Profile {
	t.Helper()
	profile, err := limits.Resolve(limits.Overrides{EvaluationTimeout: &timeout, MaxActiveWatches: &maxWatches})
	if err != nil {
		t.Fatalf("resolve watch startup profile: %v", err)
	}
	return profile
}

type startupRecoveryWatchResponse struct {
	gate   <-chan struct{}
	stream *watch.RaceFreeFakeWatcher
	err    error
}

type startupRecoveryWatcher struct {
	mu        sync.Mutex
	responses []startupRecoveryWatchResponse
	calls     []runtimeWatchCall
	streams   []*watch.RaceFreeFakeWatcher
}

func newStartupRecoveryWatcher(responses ...startupRecoveryWatchResponse) *startupRecoveryWatcher {
	return &startupRecoveryWatcher{responses: append([]startupRecoveryWatchResponse(nil), responses...)}
}

func (w *startupRecoveryWatcher) Watch(ctx context.Context, permit reconciliation.WatchPermit, resourceVersion string) (watch.Interface, error) {
	w.mu.Lock()
	capabilities := permit.Capabilities()
	subjects := make([]authorization.Subject, 0, len(capabilities))
	for _, capability := range capabilities {
		subjects = append(subjects, capability.Subject())
	}
	w.calls = append(w.calls, runtimeWatchCall{Address: permit.Address(), ResourceVersion: resourceVersion, PermitSubjects: subjects})
	response := startupRecoveryWatchResponse{}
	if len(w.responses) != 0 {
		response = w.responses[0]
		w.responses = w.responses[1:]
	}
	w.mu.Unlock()
	if response.gate != nil {
		select {
		case <-response.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if response.err != nil {
		return nil, response.err
	}
	if response.stream == nil {
		response.stream = watch.NewRaceFreeFake()
	}
	w.mu.Lock()
	w.streams = append(w.streams, response.stream)
	w.mu.Unlock()
	return response.stream, nil
}

func (w *startupRecoveryWatcher) Calls() []runtimeWatchCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]runtimeWatchCall(nil), w.calls...)
}

func (w *startupRecoveryWatcher) Stream(index int) *watch.RaceFreeFakeWatcher {
	w.mu.Lock()
	defer w.mu.Unlock()
	if index < 0 || index >= len(w.streams) {
		return nil
	}
	return w.streams[index]
}

type startupBlockingRoundTripper struct {
	started      chan struct{}
	canceled     chan struct{}
	startedOnce  sync.Once
	canceledOnce sync.Once
}

func (t *startupBlockingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.startedOnce.Do(func() { close(t.started) })
	<-request.Context().Done()
	t.canceledOnce.Do(func() { close(t.canceled) })
	return nil, request.Context().Err()
}

func objectKey(object *v1alpha1.Kubeseer) types.NamespacedName {
	return types.NamespacedName{Namespace: object.Namespace, Name: object.Name}
}

func watchStartupProfile(t *testing.T, timeout time.Duration) limits.Profile {
	t.Helper()
	profile, err := limits.Resolve(limits.Overrides{EvaluationTimeout: &timeout})
	if err != nil {
		t.Fatalf("resolve watch startup profile: %v", err)
	}
	return profile
}

func mustWatchStartupRuntime(t *testing.T, resolver *discovery.Resolver, reader *runtimePipelineReader, lister *runtimePipelineLister, policy *v1alpha1.KubeseerAccessPolicy, routes *reconciliation.RouteRegistry, publisher *runtimePipelinePublisher, tracker *reconciliation.FreshnessTracker, profile limits.Profile) *reconciliation.Runtime {
	t.Helper()
	runtime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour, LimitProfile: &profile}, reconciliation.Dependencies{
		Reader:       reader,
		Lister:       reader,
		PolicySource: &runtimePipelinePolicySource{policy: policy},
		Enforcer:     authorization.NewEnforcer(nil),
		Planner:      selection.NewPlanner(resolver),
		Executor: selection.NewExecutor(lister, selection.WithVerifier(authorization.VerifierFunc(func(subject authorization.Subject) bool {
			return subject.Validate() == nil
		}))),
		Routes:    routes,
		Publisher: publisher,
		Tracker:   tracker,
	})
	if err != nil {
		t.Fatalf("construct watch startup runtime: %v", err)
	}
	return runtime
}

func setRuntimePipelineObject(reader *runtimePipelineReader, object *v1alpha1.Kubeseer) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.objects[types.NamespacedName{Namespace: object.Namespace, Name: object.Name}] = object.DeepCopy()
}

type startupBlockingWatcher struct {
	mu           sync.Mutex
	cooperative  bool
	release      chan struct{}
	started      chan struct{}
	canceled     chan struct{}
	startedOnce  sync.Once
	canceledOnce sync.Once
	calls        []runtimeWatchCall
	streams      []*watch.RaceFreeFakeWatcher
	active       int
	maxActive    int
}

func newStartupBlockingWatcher(cooperative bool) *startupBlockingWatcher {
	return &startupBlockingWatcher{
		cooperative: cooperative,
		release:     make(chan struct{}),
		started:     make(chan struct{}),
		canceled:    make(chan struct{}),
	}
}

func (w *startupBlockingWatcher) Watch(ctx context.Context, permit reconciliation.WatchPermit, resourceVersion string) (watch.Interface, error) {
	w.mu.Lock()
	w.active++
	if w.active > w.maxActive {
		w.maxActive = w.active
	}
	capabilities := permit.Capabilities()
	subjects := make([]authorization.Subject, 0, len(capabilities))
	for _, capability := range capabilities {
		subjects = append(subjects, capability.Subject())
	}
	w.calls = append(w.calls, runtimeWatchCall{Address: permit.Address(), ResourceVersion: resourceVersion, PermitSubjects: subjects})
	w.startedOnce.Do(func() { close(w.started) })
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.active--
		w.mu.Unlock()
	}()

	if w.cooperative {
		select {
		case <-w.release:
		case <-ctx.Done():
			w.canceledOnce.Do(func() { close(w.canceled) })
			return nil, ctx.Err()
		}
	} else {
		<-w.release
	}
	stream := watch.NewRaceFreeFake()
	w.mu.Lock()
	w.streams = append(w.streams, stream)
	w.mu.Unlock()
	return stream, nil
}

func (w *startupBlockingWatcher) Calls() []runtimeWatchCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]runtimeWatchCall(nil), w.calls...)
}

func (w *startupBlockingWatcher) Stream(index int) *watch.RaceFreeFakeWatcher {
	w.mu.Lock()
	defer w.mu.Unlock()
	if index < 0 || index >= len(w.streams) {
		return nil
	}
	return w.streams[index]
}

func (w *startupBlockingWatcher) WaitCalls(t *testing.T, want int) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if len(w.Calls()) >= want {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("timed out waiting for %d WATCH calls, got %#v", want, w.Calls())
		}
	}
}

func (w *startupBlockingWatcher) WaitCanceled(t *testing.T) {
	t.Helper()
	select {
	case <-w.canceled:
	case <-time.After(time.Second):
		t.Fatal("WATCH request was not canceled")
	}
}

func (w *startupBlockingWatcher) MaxActive() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.maxActive
}

func (w *startupBlockingWatcher) WaitStopped(t *testing.T, want int) {
	t.Helper()
	waitForRuntimeWatchCondition(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		stopped := 0
		for _, stream := range w.streams {
			if stream.IsStopped() {
				stopped++
			}
		}
		return stopped >= want
	})
}
