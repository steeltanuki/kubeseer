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
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertBudgetRejectionStatus(t *testing.T) {
	t.Helper()
	key := types.NamespacedName{Namespace: "team-a", Name: "budget-status-owner"}
	result := runtimeStatusResult()

	t.Run("terminal rejection replaces prior observation with fixed conditions", func(t *testing.T) {
		current := runtimeStatusObject(key, "budget-status-uid", 7, &result)
		composeStatusFixture(t, current, publisherEvaluation(result))
		current.Status.Conditions = append(current.Status.Conditions, metav1.Condition{
			Type:               "ExternalController",
			Status:             metav1.ConditionTrue,
			Reason:             "Preserved",
			Message:            "foreign condition remains owned by another controller",
			ObservedGeneration: 3,
		})

		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		writer := &runtimeStatusWriter{}
		rejection := statuscontract.Evaluation{ConfigurationBudgetExceeded: true}
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(context.Background(), lease, rejection); err != nil {
			t.Fatalf("publish budget rejection: %v", err)
		}
		published := writer.Last()
		if published == nil || writer.Calls() != 1 {
			t.Fatalf("budget rejection writes = %d object=%#v", writer.Calls(), published)
		}
		if published.Status.Result != nil || published.Status.Summary != nil || published.Status.ResultHash != "" {
			t.Fatalf("budget rejection retained observation data: %#v", published.Status)
		}
		if published.Status.ObservedGeneration != current.Generation {
			t.Fatalf("observed generation = %d, want %d", published.Status.ObservedGeneration, current.Generation)
		}
		assertPublisherCondition(t, published.Status, statuscontract.ConditionAccepted, metav1.ConditionFalse, statuscontract.ReasonConfigurationBudgetExceeded)
		assertPublisherCondition(t, published.Status, statuscontract.ConditionAuthorized, metav1.ConditionUnknown, statuscontract.ReasonAuthorizationNotEvaluated)
		assertPublisherCondition(t, published.Status, statuscontract.ConditionSourcesResolved, metav1.ConditionUnknown, statuscontract.ReasonResolutionNotEvaluated)
		assertPublisherCondition(t, published.Status, statuscontract.ConditionReady, metav1.ConditionFalse, statuscontract.ReasonConfigurationBudgetExceeded)
		assertPublisherCondition(t, published.Status, statuscontract.ConditionDegraded, metav1.ConditionTrue, statuscontract.ReasonEvaluationUnavailable)
		if len(published.Status.Conditions) != 7 || published.Status.Conditions[6].Type != "ExternalController" {
			t.Fatalf("foreign condition was not preserved: %#v", published.Status.Conditions)
		}
		for _, condition := range published.Status.Conditions[:5] {
			if strings.Contains(condition.Message, "secret") || strings.Contains(condition.Message, "selector") || strings.Contains(condition.Message, "jsonpath") {
				t.Fatalf("rejection condition leaked diagnostic payload: %#v", condition)
			}
		}
	})

	t.Run("equivalent rejection suppresses a second status write", func(t *testing.T) {
		current := runtimeStatusObject(key, "budget-idempotent-uid", 8, &result)
		composeStatusFixture(t, current, publisherEvaluation(result))
		rejection := statuscontract.Evaluation{ConfigurationBudgetExceeded: true}
		firstWriter := &runtimeStatusWriter{}
		firstTracker := reconciliation.NewFreshnessTracker()
		firstLease, firstRelease := runtimeStatusLease(t, firstTracker, current)
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), firstWriter, firstTracker).Publish(context.Background(), firstLease, rejection); err != nil {
			t.Fatalf("first rejection publish: %v", err)
		}
		firstRelease()
		published := firstWriter.Last()
		secondWriter := &runtimeStatusWriter{}
		secondTracker := reconciliation.NewFreshnessTracker()
		secondLease, secondRelease := runtimeStatusLease(t, secondTracker, published)
		defer secondRelease()
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(published), secondWriter, secondTracker).Publish(context.Background(), secondLease, rejection); err != nil {
			t.Fatalf("equivalent rejection publish: %v", err)
		}
		if secondWriter.Calls() != 0 {
			t.Fatalf("equivalent rejection issued %d writes", secondWriter.Calls())
		}
	})

	t.Run("configuration rejection cannot be combined with observation assessments", func(t *testing.T) {
		if _, err := statuscontract.Compose(1, nil, statuscontract.Evaluation{
			Result:                      &v1alpha1.KubeseerResult{},
			ConfigurationBudgetExceeded: true,
		}); err == nil {
			t.Fatal("configuration rejection with a result was accepted")
		}
		if _, err := statuscontract.Compose(1, nil, statuscontract.Evaluation{
			Sources:                     []statuscontract.SourceAssessment{{Index: 0}},
			ConfigurationBudgetExceeded: true,
		}); err == nil {
			t.Fatal("configuration rejection with source assessments was accepted")
		}
	})

	t.Run("an uncontainable rejection reports StatusLimitInvalid without writing", func(t *testing.T) {
		current := runtimeStatusObject(key, "budget-limit-uid", 9, &result)
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		writer := &runtimeStatusWriter{}
		err := reconciliation.NewStatusPublisher(
			newRuntimeStatusReader(current), writer, tracker,
			reconciliation.WithMaxStatusBytes(64),
		).Publish(context.Background(), lease, statuscontract.Evaluation{ConfigurationBudgetExceeded: true})
		var runtimeErr *reconciliation.RuntimeError
		if !errors.As(err, &runtimeErr) || runtimeErr.Reason != reconciliation.ReasonStatusLimitInvalid {
			t.Fatalf("status limit error = %v, want StatusLimitInvalid", err)
		}
		if writer.Calls() != 0 {
			t.Fatalf("uncontainable rejection issued %d writes", writer.Calls())
		}
	})
}

func assertBudgetRejectionRuntime(t *testing.T, resolver *discovery.Resolver) {
	t.Helper()
	key := types.NamespacedName{Namespace: "team-a", Name: "budget-runtime-owner"}
	sources := make([]v1alpha1.KubeseerSource, limits.DefaultMaxKubeseerSources+1)
	for index := range sources {
		sources[index] = v1alpha1.KubeseerSource{
			ID:         fmt.Sprintf("budget-source-%d", index),
			Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
		}
	}
	object := runtimePipelineKubeseer(key, "budget-runtime-uid", 4, sources...)
	lister := newRuntimePipelineLister()
	policySource := &runtimePipelinePolicySource{err: errors.New("policy must not be read for a budget rejection")}
	routes := &runtimePipelineRoutes{}
	publisher := &runtimePipelinePublisher{}
	runtime := mustRuntimePipelineWithPolicy(t, resolver, newRuntimePipelineReader(object), lister, policySource, routes, publisher, reconciliation.NewFreshnessTracker())

	_, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
	var runtimeErr *reconciliation.RuntimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != reconciliation.ReasonConfigurationBudgetExceeded {
		t.Fatalf("budget reconciliation error = %v, want ConfigurationBudgetExceeded", err)
	}
	if reconciliation.IsRetryable(err) {
		t.Fatalf("budget reconciliation error unexpectedly retryable: %v", err)
	}
	publications := publisher.Publications()
	if len(publications) != 1 || !publications[0].Evaluation.ConfigurationBudgetExceeded || publications[0].Evaluation.Result != nil {
		t.Fatalf("budget rejection publication = %#v", publications)
	}
	if len(lister.Calls()) != 0 {
		t.Fatalf("budget rejection issued observed-resource calls: %#v", lister.Calls())
	}
	if policySource.Calls() != 0 {
		t.Fatalf("budget rejection issued %d policy reads", policySource.Calls())
	}
	if !routes.Removed(key) {
		t.Fatalf("budget rejection did not remove routes for %s", key)
	}

	t.Run("route promotion does not wait for a stalled watch", func(t *testing.T) {
		assertBudgetRoutePromotionNonBlocking(t, resolver)
	})
}

func assertBudgetRoutePromotionNonBlocking(t *testing.T, resolver *discovery.Resolver) {
	t.Helper()
	planner := selection.NewPlanner(resolver)
	snapshot := mustSnapshot(t, basePolicy())
	ownerA := types.NamespacedName{Namespace: "team-a", Name: "budget-route-a"}
	ownerB := types.NamespacedName{Namespace: "team-a", Name: "budget-route-b"}
	sourceA := v1alpha1.KubeseerSource{ID: "budget-route-source-a", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}, Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}}}
	sourceB := v1alpha1.KubeseerSource{ID: "budget-route-source-b", Resource: v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"}, Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}}}
	targetA := mustRuntimeTarget(t, context.Background(), planner, ownerA.Namespace, sourceA)
	targetB := mustRuntimeTarget(t, context.Background(), planner, ownerB.Namespace, sourceB)
	objectA := newRuntimeKubeseer(ownerA, "budget-route-uid-a", 1)
	objectB := newRuntimeKubeseer(ownerB, "budget-route-uid-b", 1)
	tracker := reconciliation.NewFreshnessTracker()
	tracker.Observe(objectA)
	tracker.Observe(objectB)
	leaseA, _, releaseA, err := tracker.Acquire(context.Background(), ownerA, objectA.UID, objectA.Generation)
	if err != nil {
		t.Fatalf("acquire first route lease: %v", err)
	}
	defer releaseA()
	leaseB, _, releaseB, err := tracker.Acquire(context.Background(), ownerB, objectB.UID, objectB.Generation)
	if err != nil {
		t.Fatalf("acquire second route lease: %v", err)
	}
	defer releaseB()
	routeA, err := mustRuntimeAuthorizedRoute(t, leaseA, targetA, snapshot)
	if err != nil {
		t.Fatalf("authorize first route: %v", err)
	}
	routeB, err := mustRuntimeAuthorizedRoute(t, leaseB, targetB, snapshot)
	if err != nil {
		t.Fatalf("authorize second route: %v", err)
	}
	watcher := &budgetBlockingWatcher{
		firstStarted:  make(chan struct{}),
		firstRelease:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		secondRelease: make(chan struct{}),
	}
	registry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithMaxActiveWatches(1), reconciliation.WithRouteWatchBackoff(time.Millisecond, 2*time.Millisecond))
	queue := newRuntimeQueue()
	defer queue.ShutDown()
	watchContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := registry.Start(watchContext, queue); err != nil {
		t.Fatalf("start budget route registry: %v", err)
	}
	replaceAErr := make(chan error, 1)
	go func() { replaceAErr <- registry.Replace(context.Background(), leaseA, []reconciliation.AuthorizedRoute{routeA}) }()
	select {
	case <-watcher.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first budget route did not start watching")
	}
	close(watcher.firstRelease)
	select {
	case err := <-replaceAErr:
		if err != nil {
			t.Fatalf("replace first budget route: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first budget route replacement did not finish")
	}
	if err := registry.Replace(context.Background(), leaseB, []reconciliation.AuthorizedRoute{routeB}); err != nil {
		t.Fatalf("replace second budget route: %v", err)
	}
	started := time.Now()
	registry.RemoveOwner(ownerA)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("route removal waited %s for promoted watch", elapsed)
	}
	select {
	case <-watcher.secondStarted:
	case <-time.After(time.Second):
		t.Fatal("promoted budget route did not start")
	}
	if registry.BindingCount(ownerA) != 0 || registry.BindingCount(ownerB) != 1 || len(registry.Owners(routeB.Address())) != 1 || registry.Owners(routeB.Address())[0] != ownerB {
		t.Fatalf("route ownership after rejection promotion: A=%d B=%d owners=%#v", registry.BindingCount(ownerA), registry.BindingCount(ownerB), registry.Owners(routeB.Address()))
	}
	close(watcher.secondRelease)
	registry.RemoveOwner(ownerB)
}

type budgetBlockingWatcher struct {
	mu            sync.Mutex
	calls         int
	firstStarted  chan struct{}
	firstRelease  chan struct{}
	secondStarted chan struct{}
	secondRelease chan struct{}
}

func (w *budgetBlockingWatcher) Watch(ctx context.Context, _ reconciliation.WatchPermit, _ string) (watch.Interface, error) {
	w.mu.Lock()
	w.calls++
	call := w.calls
	w.mu.Unlock()
	switch call {
	case 1:
		close(w.firstStarted)
		select {
		case <-w.firstRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	case 2:
		close(w.secondStarted)
		select {
		case <-w.secondRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return watch.NewRaceFreeFake(), nil
}
