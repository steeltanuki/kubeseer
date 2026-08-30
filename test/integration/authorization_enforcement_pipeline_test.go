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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertAuthorizationEnforcementPipelineScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	t.Run("one fresh ordered decision batch drives routes LIST and public status", func(t *testing.T) {
		policy := basePolicy()
		policy.UID = types.UID("authorization-policy-uid")
		policy.Generation = 7
		allowedSource := authorizationPipelineSource("allowed-source", "team-a", "team-c")
		allowedSource.Selector = &v1alpha1.ResourceSelector{
			MatchLabels:   map[string]string{"authorization-label": "allowed"},
			FieldSelector: "metadata.name=selector-sentinel",
		}
		deniedSource := authorizationPipelineSource("denied-source", "team-b")
		deniedSource.Fields[0].Path = "{.data.secret}"
		deniedSource.Selector = &v1alpha1.ResourceSelector{FieldSelector: "metadata.name=denied-selector-sentinel"}
		key := types.NamespacedName{Namespace: "team-a", Name: "authorization-pipeline-owner"}
		object := runtimePipelineKubeseer(key, types.UID("authorization-owner-uid"), 3, allowedSource, deniedSource)
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetHook(func(ctx context.Context, target selection.ReadTarget, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*authorizationPipelineResource(target)}}, nil
		})
		routes := &runtimePipelineRoutes{}
		publisher := &runtimePipelinePublisher{}
		tracker := reconciliation.NewFreshnessTracker()
		recorder := &authorizationPipelineRecorder{}
		policySource := &authorizationPipelinePolicySource{policy: policy}
		runtime := mustAuthorizationPipelineRuntime(t, resolver, reader, lister, policySource, routes, publisher, tracker, authorization.NewEnforcer(recorder))

		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("ordered authorization reconciliation: %v", err)
		}
		if policySource.Calls() != 1 {
			t.Fatalf("fresh policy loads = %d, want one", policySource.Calls())
		}
		publications := publisher.Publications()
		if len(publications) != 1 {
			t.Fatalf("publications = %d, want one", len(publications))
		}
		evaluation := publications[0].Evaluation
		if len(evaluation.Sources) != 2 {
			t.Fatalf("source assessments = %#v, want two", evaluation.Sources)
		}
		assertPipelineAssessment(t, evaluation.Sources[0], 0, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationAllowedOutcome, statuscontract.ResolutionResolvedOutcome)
		assertPipelineAssessment(t, evaluation.Sources[1], 1, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationDeniedOutcome, statuscontract.ResolutionResolvedOutcome)
		if evaluation.Result == nil || len(evaluation.Result.Sources) != 2 || evaluation.Result.Sources[0].State != v1alpha1.SourceStateValues || len(evaluation.Result.Sources[0].Resources) != 2 {
			t.Fatalf("ordered result = %#v", evaluation.Result)
		}
		if evaluation.Result.Sources[1].State != v1alpha1.SourceStateError || evaluation.Result.Sources[1].Error == nil || evaluation.Result.Sources[1].Error.Reason != string(selection.ReasonAuthorizationDenied) {
			t.Fatalf("denied sibling result = %#v", evaluation.Result.Sources[1])
		}
		if routes.LastRouteCount() != 2 {
			t.Fatalf("authorized route count = %d, want two exact current targets", routes.LastRouteCount())
		}
		calls := lister.Calls()
		if len(calls) != 2 || calls[0].Target.SourceID != allowedSource.ID || calls[1].Target.SourceID != allowedSource.ID || calls[0].Target.Namespace != "team-a" || calls[1].Target.Namespace != "team-c" {
			t.Fatalf("authorized LIST calls = %#v, want ordered allowed namespaces only", calls)
		}
		if calls[0].Options.FieldSelector != "metadata.name=selector-sentinel" || calls[0].Options.LabelSelector != "authorization-label=allowed" {
			t.Fatalf("authorized selectors = %#v, want source selectors unchanged", calls[0].Options)
		}

		records := recorder.Records()
		if len(records) != 3 {
			t.Fatalf("captured decision records = %d, want one per exact target", len(records))
		}
		wantRecords := []struct {
			source    string
			namespace string
			outcome   authorization.Outcome
			reason    string
		}{
			{source: allowedSource.ID, namespace: "team-a", outcome: authorization.OutcomeAllowed, reason: string(accesspolicy.ReasonAllowed)},
			{source: allowedSource.ID, namespace: "team-c", outcome: authorization.OutcomeAllowed, reason: string(accesspolicy.ReasonAllowed)},
			{source: deniedSource.ID, namespace: "team-b", outcome: authorization.OutcomeDenied, reason: string(accesspolicy.ReasonNamespaceDenied)},
		}
		for index, want := range wantRecords {
			got := records[index]
			if got.SourceID != want.source || got.Namespace != want.namespace || got.Outcome != want.outcome || got.Reason != want.reason {
				t.Fatalf("record %d = %#v, want source=%q namespace=%q outcome=%q reason=%q", index, got, want.source, want.namespace, want.outcome, want.reason)
			}
			if got.Subject.Key != key || got.Subject.UID != object.UID || got.Subject.Generation != object.Generation || got.Subject.PolicyEpoch != 0 {
				t.Fatalf("record %d subject = %#v, want live identity and epoch zero", index, got.Subject)
			}
			if got.PolicyIdentity == nil || !reflect.DeepEqual(*got.PolicyIdentity, accesspolicy.PolicyIdentity{Name: v1alpha1.InstallationAccessCeilingName, UID: policy.UID, Generation: policy.Generation}) {
				t.Fatalf("record %d policy identity = %#v", index, got.PolicyIdentity)
			}
		}
		diagnostics := fmt.Sprintf("%#v", evaluation.Result.Sources[1].Error)
		recordDiagnostics := fmt.Sprintf("%#v", records)
		for _, sentinel := range []string{"denied-secret-sentinel", "denied-selector-sentinel", "selector-sentinel", "authorization-label=allowed"} {
			if strings.Contains(diagnostics, sentinel) || strings.Contains(recordDiagnostics, sentinel) {
				t.Fatalf("authorization diagnostics leaked %q: result=%s records=%s", sentinel, diagnostics, recordDiagnostics)
			}
		}
		candidate, err := statuscontract.Compose(object.Generation, nil, evaluation)
		if err != nil {
			t.Fatalf("compose ordered authorization status: %v", err)
		}
		assertPipelineCondition(t, candidate, statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonAuthorizationDenied)
	})

	t.Run("narrowing removes old values and broadening reconstructs fresh access", func(t *testing.T) {
		policy := basePolicy()
		policy.UID = types.UID("authorization-transition-policy")
		policy.Generation = 2
		source := authorizationPipelineSource("transition-source", "team-a")
		key := types.NamespacedName{Namespace: "team-a", Name: "authorization-transition-owner"}
		object := runtimePipelineKubeseer(key, types.UID("authorization-transition-owner-uid"), 1, source)
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		lister.SetHook(func(ctx context.Context, target selection.ReadTarget, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*authorizationPipelineResource(target)}}, nil
		})
		policySource := &authorizationPipelinePolicySource{policy: policy}
		publisher := &runtimePipelinePublisher{}
		tracker := reconciliation.NewFreshnessTracker()
		routes := &runtimePipelineRoutes{}
		runtime := mustAuthorizationPipelineRuntime(t, resolver, reader, lister, policySource, routes, publisher, tracker, authorization.NewEnforcer(nil))

		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("initial transition reconciliation: %v", err)
		}
		initialCalls := len(lister.Calls())
		if initialCalls != 1 || len(publisher.Publications()) != 1 {
			t.Fatalf("initial transition state: calls=%d publications=%d", initialCalls, len(publisher.Publications()))
		}

		narrowed := mutatePolicy(policy, func(next *v1alpha1.KubeseerAccessPolicy) {
			next.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{""}, Kinds: []string{"Node"}}}
		})
		tracker.InvalidateAll()
		policySource.SetPolicy(narrowed)
		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("narrowed transition reconciliation: %v", err)
		}
		publications := publisher.Publications()
		if len(publications) != 2 || publications[1].Result.Sources[0].Resources != nil || publications[1].Result.Sources[0].Error == nil {
			t.Fatalf("narrowed result retained old value: %#v", publications)
		}
		assertPipelineAssessment(t, publications[1].Evaluation.Sources[0], 0, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationDeniedOutcome, statuscontract.ResolutionResolvedOutcome)
		if len(lister.Calls()) != initialCalls || routes.LastRouteCount() != 0 {
			t.Fatalf("narrowed policy retained executable access: calls=%d routes=%d", len(lister.Calls()), routes.LastRouteCount())
		}

		tracker.InvalidateAll()
		policySource.SetPolicy(policy)
		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("broadened transition reconciliation: %v", err)
		}
		publications = publisher.Publications()
		if len(publications) != 3 || publications[2].Result.Sources[0].State != v1alpha1.SourceStateValues || len(publications[2].Result.Sources[0].Resources) != 1 {
			t.Fatalf("broadened result did not reconstruct access: %#v", publications)
		}
		if len(lister.Calls()) != initialCalls+1 || policySource.Calls() != 3 {
			t.Fatalf("fresh broadening calls: LIST=%d policy=%d", len(lister.Calls()), policySource.Calls())
		}

		restartedPublisher := &runtimePipelinePublisher{}
		restartedTracker := reconciliation.NewFreshnessTracker()
		restartedLister := newRuntimePipelineLister()
		restartedLister.SetHook(func(ctx context.Context, target selection.ReadTarget, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*authorizationPipelineResource(target)}}, nil
		})
		restarted := mustAuthorizationPipelineRuntime(t, resolver, reader, restartedLister, policySource, &runtimePipelineRoutes{}, restartedPublisher, restartedTracker, authorization.NewEnforcer(nil))
		if _, err := restarted.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("restart reconstruction reconciliation: %v", err)
		}
		if len(restartedLister.Calls()) != 1 || len(restartedPublisher.Publications()) != 1 || policySource.Calls() != 4 {
			t.Fatalf("restart did not reconstruct one fresh authorization: calls=%d publications=%d policy=%d", len(restartedLister.Calls()), len(restartedPublisher.Publications()), policySource.Calls())
		}
	})

	t.Run("zero sources and terminal policy states remain fail closed and deterministic", func(t *testing.T) {
		key := types.NamespacedName{Namespace: "team-a", Name: "authorization-empty-owner"}
		object := runtimePipelineKubeseer(key, types.UID("authorization-empty-uid"), 1)
		policySource := &authorizationPipelinePolicySource{err: errors.New("raw policy payload sentinel")}
		recorder := &authorizationPipelineRecorder{}
		publisher := &runtimePipelinePublisher{}
		lister := newRuntimePipelineLister()
		emptyRuntime := mustAuthorizationPipelineRuntime(t, resolver, newRuntimePipelineReader(object), lister, policySource, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker(), authorization.NewEnforcer(recorder))
		if _, err := emptyRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("zero-source reconciliation: %v", err)
		}
		if policySource.Calls() != 0 || len(recorder.Records()) != 0 || len(lister.Calls()) != 0 {
			t.Fatalf("zero-source path performed enforcement work: policy=%d records=%d lists=%d", policySource.Calls(), len(recorder.Records()), len(lister.Calls()))
		}
		if publications := publisher.Publications(); len(publications) != 1 || len(publications[0].Result.Sources) != 0 {
			t.Fatalf("zero-source publication = %#v", publications)
		}

		invalid := v1alpha1.KubeseerSource{ID: "invalid-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}, Selector: &v1alpha1.ResourceSelector{FieldSelector: "metadata.name in ("}, Fields: []v1alpha1.KubeseerField{{Name: "not-evaluated-secret", Path: "{.data.not-evaluated-secret}", Type: v1alpha1.ValueTypeString}}}
		unknown := v1alpha1.KubeseerSource{ID: "unknown-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "UnknownKind"}, Fields: []v1alpha1.KubeseerField{{Name: "unknown-secret", Path: "{.data.unknown-secret}", Type: v1alpha1.ValueTypeString}}}
		deterministicKey := types.NamespacedName{Namespace: "team-a", Name: "authorization-not-evaluated-owner"}
		deterministicObject := runtimePipelineKubeseer(deterministicKey, types.UID("authorization-not-evaluated-uid"), 1, invalid, unknown)
		deterministicPublisher := &runtimePipelinePublisher{}
		deterministicSource := &authorizationPipelinePolicySource{policy: basePolicy()}
		deterministicRuntime := mustAuthorizationPipelineRuntime(t, resolver, newRuntimePipelineReader(deterministicObject), newRuntimePipelineLister(), deterministicSource, &runtimePipelineRoutes{}, deterministicPublisher, reconciliation.NewFreshnessTracker(), authorization.NewEnforcer(&authorizationPipelineRecorder{}))
		if _, err := deterministicRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: deterministicKey}); err != nil {
			t.Fatalf("deterministic not-evaluated reconciliation: %v", err)
		}
		publications := deterministicPublisher.Publications()
		if len(publications) != 1 || len(publications[0].Evaluation.Sources) != 2 {
			t.Fatalf("deterministic publication = %#v", publications)
		}
		for index, assessment := range publications[0].Evaluation.Sources {
			if assessment.Authorization != statuscontract.AuthorizationNotEvaluatedOutcome {
				t.Fatalf("source %d authorization = %q, want not evaluated", index, assessment.Authorization)
			}
		}
		candidate, err := statuscontract.Compose(deterministicObject.Generation, nil, publications[0].Evaluation)
		if err != nil {
			t.Fatalf("compose not-evaluated status: %v", err)
		}
		assertPipelineCondition(t, candidate, statuscontract.ConditionAuthorized, metav1.ConditionUnknown, statuscontract.ReasonAuthorizationNotEvaluated)
	})

	t.Run("unavailable policy retries without using cached authorization", func(t *testing.T) {
		key := types.NamespacedName{Namespace: "team-a", Name: "authorization-unavailable-owner"}
		source := authorizationPipelineSource("unavailable-source", "team-a")
		object := runtimePipelineKubeseer(key, types.UID("authorization-unavailable-uid"), 1, source)
		policySource := &authorizationPipelinePolicySource{err: errors.New("raw unavailable policy sentinel")}
		publisher := &runtimePipelinePublisher{}
		lister := newRuntimePipelineLister()
		runtime := mustAuthorizationPipelineRuntime(t, resolver, newRuntimePipelineReader(object), lister, policySource, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker(), authorization.NewEnforcer(nil))
		result, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		if result != (reconcile.Result{}) || err == nil || !reconciliation.IsRetryable(err) {
			t.Fatalf("unavailable policy result=%#v error=%v, want retryable error", result, err)
		}
		publications := publisher.Publications()
		if len(publications) != 1 || publications[0].Evaluation.GlobalAuthorization == nil || *publications[0].Evaluation.GlobalAuthorization != statuscontract.AuthorizationUnavailableOutcome || len(lister.Calls()) != 0 {
			t.Fatalf("unavailable policy evaluation = %#v calls=%d", publications, len(lister.Calls()))
		}
		candidate, composeErr := statuscontract.Compose(object.Generation, nil, publications[0].Evaluation)
		if composeErr != nil {
			t.Fatalf("compose unavailable policy status: %v", composeErr)
		}
		assertPipelineCondition(t, candidate, statuscontract.ConditionAuthorized, metav1.ConditionUnknown, statuscontract.ReasonAuthorizationUnavailable)
		if strings.Contains(fmt.Sprintf("%#v", publications[0].Result), "raw unavailable policy sentinel") {
			t.Fatal("unavailable policy diagnostic leaked the raw policy error")
		}
	})

	t.Run("policy invalidation cancels in-flight work before publication", func(t *testing.T) {
		key := types.NamespacedName{Namespace: "team-a", Name: "authorization-stale-owner"}
		source := authorizationPipelineSource("stale-source", "team-a")
		object := runtimePipelineKubeseer(key, types.UID("authorization-stale-uid"), 1, source)
		started := make(chan struct{})
		var startedOnce sync.Once
		lister := newRuntimePipelineLister()
		lister.SetHook(func(ctx context.Context, target selection.ReadTarget, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			startedOnce.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		})
		tracker := reconciliation.NewFreshnessTracker()
		publisher := &runtimePipelinePublisher{}
		runtime := mustAuthorizationPipelineRuntime(t, resolver, newRuntimePipelineReader(object), lister, &authorizationPipelinePolicySource{policy: basePolicy()}, &runtimePipelineRoutes{}, publisher, tracker, authorization.NewEnforcer(nil))
		done := make(chan error, 1)
		go func() {
			_, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			done <- err
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("stale policy fixture did not start LIST")
		}
		tracker.InvalidateAll()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("stale reconciliation returned error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("stale reconciliation did not stop after policy invalidation")
		}
		if len(publisher.Publications()) != 0 || len(lister.Calls()) != 1 {
			t.Fatalf("stale candidate escaped publication or issued later work: publications=%#v calls=%#v", publisher.Publications(), lister.Calls())
		}
	})
}

func authorizationPipelineSource(id string, namespaces ...string) v1alpha1.KubeseerSource {
	return v1alpha1.KubeseerSource{
		ID:         id,
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: append([]string(nil), namespaces...)},
		Fields:     []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}},
	}
}

func authorizationPipelineResource(target selection.ReadTarget) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      target.Namespace + "-authorization-resource",
			"namespace": target.Namespace,
			"uid":       target.Namespace + "-authorization-resource-uid",
		},
		"data": map[string]interface{}{"value": "observed-value-sentinel-" + target.Namespace},
	}}
}

func mustAuthorizationPipelineRuntime(t *testing.T, resolver *discovery.Resolver, reader *runtimePipelineReader, lister *runtimePipelineLister, policy accesspolicy.PolicySource, routes *runtimePipelineRoutes, publisher *runtimePipelinePublisher, tracker *reconciliation.FreshnessTracker, enforcer *authorization.Enforcer) *reconciliation.Runtime {
	t.Helper()
	runtime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour}, reconciliation.Dependencies{
		Reader:       reader,
		Lister:       reader,
		PolicySource: policy,
		Enforcer:     enforcer,
		Planner:      selection.NewPlanner(resolver),
		Executor:     selection.NewExecutor(lister, selection.WithVerifier(tracker)),
		Routes:       routes,
		Publisher:    publisher,
		Tracker:      tracker,
	})
	if err != nil {
		t.Fatalf("construct authorization pipeline runtime: %v", err)
	}
	return runtime
}

type authorizationPipelineRecorder struct {
	mu      sync.Mutex
	batches [][]authorization.Record
}

func (r *authorizationPipelineRecorder) Record(_ context.Context, records []authorization.Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	batch := make([]authorization.Record, len(records))
	for index := range records {
		batch[index] = records[index].DeepCopy()
	}
	r.batches = append(r.batches, batch)
}

func (r *authorizationPipelineRecorder) Records() []authorization.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var records []authorization.Record
	for _, batch := range r.batches {
		for _, record := range batch {
			records = append(records, record.DeepCopy())
		}
	}
	return records
}

type authorizationPipelinePolicySource struct {
	mu     sync.Mutex
	policy *v1alpha1.KubeseerAccessPolicy
	err    error
	gets   int
}

func (s *authorizationPipelinePolicySource) Get(ctx context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if s.err != nil {
		return nil, s.err
	}
	if s.policy == nil {
		return nil, nil
	}
	return s.policy.DeepCopy(), nil
}

func (s *authorizationPipelinePolicySource) SetPolicy(policy *v1alpha1.KubeseerAccessPolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = policy.DeepCopy()
	s.err = nil
}

func (s *authorizationPipelinePolicySource) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets
}
