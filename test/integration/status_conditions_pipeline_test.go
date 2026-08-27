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

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertStatusAndConditionsPipelineScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	policy := basePolicy()
	key := types.NamespacedName{Namespace: "team-a", Name: "status-pipeline-owner"}

	t.Run("success carries complete terminal assessments and result", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{
			ID:         "pipeline-success",
			Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
			Fields:     []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.good}", Type: v1alpha1.ValueTypeString}},
		}
		object := runtimePipelineKubeseer(key, "status-pipeline-success-uid", 7, source)
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, unstructuredListForPipeline(runtimePipelineResource("success-resource", "success-resource-uid", "success", "1")))
		publisher := &runtimePipelinePublisher{}
		runtime := mustRuntimePipeline(t, resolver, newRuntimePipelineReader(object), lister, policy, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
		if _, err := runtime.Reconcile(ctx, reconcileRequest(key)); err != nil {
			t.Fatalf("successful pipeline reconciliation: %v", err)
		}
		evaluation := requirePipelineEvaluation(t, publisher)
		if evaluation.Result == nil || evaluation.Result.Sources[0].ID != source.ID {
			t.Fatalf("successful evaluation result = %#v", evaluation.Result)
		}
		assertPipelineAssessment(t, evaluation.Sources[0], 0, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationAllowedOutcome, statuscontract.ResolutionResolvedOutcome)
		if evaluation.GlobalAuthorization != nil || statuscontract.HasResultErrors(evaluation.Result) {
			t.Fatalf("successful evaluation = %#v", evaluation)
		}
		candidate, err := statuscontract.Compose(object.Generation, nil, evaluation)
		if err != nil {
			t.Fatalf("compose successful pipeline evaluation: %v", err)
		}
		assertPipelineCondition(t, candidate, statuscontract.ConditionReady, metav1.ConditionTrue, statuscontract.ReasonEvaluationSucceeded)
	})

	t.Run("partial result retains siblings and separates denial from read availability", func(t *testing.T) {
		values := runtimePipelineValuesSource("pipeline-partial-values")
		denied := v1alpha1.KubeseerSource{
			ID:         "pipeline-partial-denied",
			Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-b"}},
		}
		readUnavailable := v1alpha1.KubeseerSource{
			ID:         "pipeline-partial-read",
			Resource:   v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
		}
		object := runtimePipelineKubeseer(key, "status-pipeline-partial-uid", 1, values, denied, readUnavailable)
		lister := newRuntimePipelineLister()
		lister.SetResponse(values.ID, unstructuredListForPipeline(runtimePipelineResource("partial-resource", "partial-resource-uid", "kept-sibling", "1")))
		lister.SetResponse(readUnavailable.ID, nil, apierrors.NewServiceUnavailable("observed body must stay private"))
		publisher := &runtimePipelinePublisher{}
		runtime := mustRuntimePipeline(t, resolver, newRuntimePipelineReader(object), lister, policy, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
		_, err := runtime.Reconcile(ctx, reconcileRequest(key))
		if err == nil || !reconciliation.IsRetryable(err) {
			t.Fatalf("partial pipeline error = %v, want retryable read availability", err)
		}
		evaluation := requirePipelineEvaluation(t, publisher)
		if evaluation.Result == nil || len(evaluation.Result.Sources) != 3 || len(evaluation.Result.Sources[0].Resources) != 1 {
			t.Fatalf("partial result did not retain successful sibling: %#v", evaluation.Result)
		}
		assertPipelineAssessment(t, evaluation.Sources[0], 0, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationAllowedOutcome, statuscontract.ResolutionResolvedOutcome)
		assertPipelineAssessment(t, evaluation.Sources[1], 1, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationDeniedOutcome, statuscontract.ResolutionResolvedOutcome)
		assertPipelineAssessment(t, evaluation.Sources[2], 2, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationAllowedOutcome, statuscontract.ResolutionResolvedOutcome)
		if evaluation.Result.Sources[1].Error == nil || evaluation.Result.Sources[2].Error == nil {
			t.Fatalf("partial source failures were not retained: %#v", evaluation.Result.Sources)
		}
		if strings.Contains(evaluation.Result.Sources[2].Error.Message, "observed body") {
			t.Fatal("read failure leaked the upstream response body")
		}
		candidate, composeErr := statuscontract.Compose(object.Generation, nil, evaluation)
		if composeErr != nil {
			t.Fatalf("compose partial pipeline evaluation: %v", composeErr)
		}
		assertPipelineCondition(t, candidate, statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonAuthorizationDenied)
		assertPipelineCondition(t, candidate, statuscontract.ConditionReady, metav1.ConditionFalse, statuscontract.ReasonEvaluationDegraded)
		assertPipelineCondition(t, candidate, statuscontract.ConditionDegraded, metav1.ConditionTrue, statuscontract.ReasonEvaluationDegraded)
	})

	t.Run("deterministic configuration and resolution failures preserve first-source precedence", func(t *testing.T) {
		invalid := v1alpha1.KubeseerSource{
			ID:         "pipeline-invalid",
			Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
			Selector:   &v1alpha1.ResourceSelector{FieldSelector: "metadata.name in ("},
		}
		unknown := v1alpha1.KubeseerSource{
			ID:       "pipeline-unknown",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "NoSuchKind"},
		}
		object := runtimePipelineKubeseer(key, "status-pipeline-invalid-uid", 1, invalid, unknown)
		publisher := &runtimePipelinePublisher{}
		runtime := mustRuntimePipeline(t, resolver, newRuntimePipelineReader(object), newRuntimePipelineLister(), policy, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
		if _, err := runtime.Reconcile(ctx, reconcileRequest(key)); err != nil {
			t.Fatalf("deterministic failure pipeline: %v", err)
		}
		evaluation := requirePipelineEvaluation(t, publisher)
		assertPipelineAssessment(t, evaluation.Sources[0], 0, statuscontract.ConfigurationInvalidOutcome, statuscontract.AuthorizationNotEvaluatedOutcome, statuscontract.ResolutionResolvedOutcome)
		assertPipelineAssessment(t, evaluation.Sources[1], 1, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationNotEvaluatedOutcome, statuscontract.ResolutionFailedOutcome)
		candidate, err := statuscontract.Compose(object.Generation, nil, evaluation)
		if err != nil {
			t.Fatalf("compose deterministic failure evaluation: %v", err)
		}
		assertPipelineCondition(t, candidate, statuscontract.ConditionAccepted, metav1.ConditionFalse, statuscontract.ReasonInvalidConfiguration)
		assertPipelineCondition(t, candidate, statuscontract.ConditionAuthorized, metav1.ConditionUnknown, statuscontract.ReasonAuthorizationNotEvaluated)
		assertPipelineCondition(t, candidate, statuscontract.ConditionSourcesResolved, metav1.ConditionFalse, statuscontract.ReasonResolutionFailed)
		if evaluation.Result == nil || len(evaluation.Result.Sources) != 2 {
			t.Fatalf("deterministic failure result = %#v", evaluation.Result)
		}
	})

	t.Run("policy-wide missing invalid and unavailable states take authorization precedence", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{ID: "pipeline-policy-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		cases := []struct {
			name          string
			policy        accesspolicy.PolicySource
			wantOutcome   statuscontract.AuthorizationOutcome
			wantReason    string
			wantCondition metav1.ConditionStatus
			wantRetry     bool
		}{
			{name: "missing", policy: &runtimePipelinePolicySource{err: apierrors.NewNotFound(schema.GroupResource{Group: "kubeseer.io", Resource: "kubeseeraccesspolicies"}, v1alpha1.InstallationAccessCeilingName)}, wantOutcome: statuscontract.AuthorizationPolicyMissingOutcome, wantReason: statuscontract.ReasonPolicyMissing, wantCondition: metav1.ConditionFalse},
			{name: "invalid", policy: &runtimePipelinePolicySource{policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) { policy.Spec.Namespaces.Mode = "invalid" })}, wantOutcome: statuscontract.AuthorizationPolicyInvalidOutcome, wantReason: statuscontract.ReasonPolicyInvalid, wantCondition: metav1.ConditionFalse},
			{name: "unavailable", policy: &runtimePipelinePolicySource{err: errors.New("policy payload must stay private")}, wantOutcome: statuscontract.AuthorizationUnavailableOutcome, wantReason: statuscontract.ReasonAuthorizationUnavailable, wantCondition: metav1.ConditionUnknown, wantRetry: true},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				object := runtimePipelineKubeseer(key, types.UID("status-pipeline-policy-"+test.name), 1, source)
				publisher := &runtimePipelinePublisher{}
				runtime := mustRuntimePipelineWithPolicy(t, resolver, newRuntimePipelineReader(object), newRuntimePipelineLister(), test.policy, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
				_, err := runtime.Reconcile(ctx, reconcileRequest(key))
				if test.wantRetry != (err != nil && reconciliation.IsRetryable(err)) {
					t.Fatalf("policy %s retry result = %v", test.name, err)
				}
				evaluation := requirePipelineEvaluation(t, publisher)
				if evaluation.GlobalAuthorization == nil || *evaluation.GlobalAuthorization != test.wantOutcome {
					t.Fatalf("policy %s global authorization = %#v", test.name, evaluation.GlobalAuthorization)
				}
				candidate, composeErr := statuscontract.Compose(object.Generation, nil, evaluation)
				if composeErr != nil {
					t.Fatalf("compose policy %s evaluation: %v", test.name, composeErr)
				}
				assertPipelineCondition(t, candidate, statuscontract.ConditionAuthorized, test.wantCondition, test.wantReason)
			})
		}
	})

	t.Run("forbidden reads replace allowed authorization and do not retry", func(t *testing.T) {
		source := v1alpha1.KubeseerSource{ID: "pipeline-forbidden", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
		object := runtimePipelineKubeseer(key, "status-pipeline-forbidden-uid", 1, source)
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, nil, apierrors.NewForbidden(schema.GroupResource{Group: "", Resource: "pods"}, "forbidden", errors.New("RBAC body must stay private")))
		publisher := &runtimePipelinePublisher{}
		runtime := mustRuntimePipeline(t, resolver, newRuntimePipelineReader(object), lister, policy, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
		if _, err := runtime.Reconcile(ctx, reconcileRequest(key)); err != nil {
			t.Fatalf("forbidden pipeline returned error: %v", err)
		}
		evaluation := requirePipelineEvaluation(t, publisher)
		assertPipelineAssessment(t, evaluation.Sources[0], 0, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationReadForbiddenOutcome, statuscontract.ResolutionResolvedOutcome)
		if evaluation.Result == nil || evaluation.Result.Sources[0].Error == nil || evaluation.Result.Sources[0].Error.Reason != string(selection.ReasonReadForbidden) {
			t.Fatalf("forbidden result = %#v", evaluation.Result)
		}
	})

	t.Run("zero sources publish a present empty result without policy", func(t *testing.T) {
		object := runtimePipelineKubeseer(key, "status-pipeline-empty-uid", 1)
		publisher := &runtimePipelinePublisher{}
		policySource := &runtimePipelinePolicySource{err: apierrors.NewNotFound(schema.GroupResource{Group: "kubeseer.io", Resource: "kubeseeraccesspolicies"}, v1alpha1.InstallationAccessCeilingName)}
		runtime := mustRuntimePipelineWithPolicy(t, resolver, newRuntimePipelineReader(object), newRuntimePipelineLister(), policySource, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
		if _, err := runtime.Reconcile(ctx, reconcileRequest(key)); err != nil {
			t.Fatalf("zero-source pipeline returned error: %v", err)
		}
		evaluation := requirePipelineEvaluation(t, publisher)
		if evaluation.Result == nil || len(evaluation.Result.Sources) != 0 || evaluation.GlobalAuthorization != nil || len(evaluation.Sources) != 0 {
			t.Fatalf("zero-source evaluation = %#v", evaluation)
		}
		candidate, err := statuscontract.Compose(object.Generation, nil, evaluation)
		if err != nil {
			t.Fatalf("compose zero-source evaluation: %v", err)
		}
		assertPipelineCondition(t, candidate, statuscontract.ConditionAuthorized, metav1.ConditionTrue, statuscontract.ReasonAuthorizationSucceeded)
		assertPipelineCondition(t, candidate, statuscontract.ConditionReady, metav1.ConditionTrue, statuscontract.ReasonEvaluationSucceeded)
	})

	t.Run("discovery unavailability is retained as unknown and retryable", func(t *testing.T) {
		failureResolver := discovery.NewResolver(&selectionDiscoveryFailureClient{delegate: newPolicyDiscoveryClient(), cause: errors.New("discovery payload must stay private")})
		source := v1alpha1.KubeseerSource{ID: "pipeline-discovery-unavailable", Resource: v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"}}
		object := runtimePipelineKubeseer(key, "status-pipeline-discovery-uid", 1, source)
		publisher := &runtimePipelinePublisher{}
		runtime := mustRuntimePipeline(t, failureResolver, newRuntimePipelineReader(object), newRuntimePipelineLister(), policy, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker())
		_, err := runtime.Reconcile(ctx, reconcileRequest(key))
		if err == nil || !reconciliation.IsRetryable(err) {
			t.Fatalf("discovery unavailable error = %v", err)
		}
		evaluation := requirePipelineEvaluation(t, publisher)
		assertPipelineAssessment(t, evaluation.Sources[0], 0, statuscontract.ConfigurationAcceptedOutcome, statuscontract.AuthorizationUnavailableOutcome, statuscontract.ResolutionUnavailableOutcome)
		candidate, composeErr := statuscontract.Compose(object.Generation, nil, evaluation)
		if composeErr != nil {
			t.Fatalf("compose discovery unavailable evaluation: %v", composeErr)
		}
		assertPipelineCondition(t, candidate, statuscontract.ConditionAuthorized, metav1.ConditionUnknown, statuscontract.ReasonAuthorizationUnavailable)
		assertPipelineCondition(t, candidate, statuscontract.ConditionSourcesResolved, metav1.ConditionUnknown, statuscontract.ReasonResolutionUnavailable)
	})
}

func assertPipelineAssessment(t *testing.T, got statuscontract.SourceAssessment, index int, configuration statuscontract.ConfigurationOutcome, authorization statuscontract.AuthorizationOutcome, resolution statuscontract.ResolutionOutcome) {
	t.Helper()
	if got.Index != index || got.Configuration != configuration || got.Authorization != authorization || got.Resolution != resolution {
		t.Fatalf("source assessment = %#v, want index=%d configuration=%q authorization=%q resolution=%q", got, index, configuration, authorization, resolution)
	}
}

func assertPipelineCondition(t *testing.T, candidate v1alpha1.KubeseerStatus, conditionType string, conditionStatus metav1.ConditionStatus, reason string) {
	t.Helper()
	for _, condition := range candidate.Conditions {
		if condition.Type == conditionType {
			if condition.Status != conditionStatus || condition.Reason != reason {
				t.Fatalf("condition %s = %#v, want status=%s reason=%s", conditionType, condition, conditionStatus, reason)
			}
			return
		}
	}
	t.Fatalf("condition %s missing from %#v", conditionType, candidate.Conditions)
}

func requirePipelineEvaluation(t *testing.T, publisher *runtimePipelinePublisher) statuscontract.Evaluation {
	t.Helper()
	publications := publisher.Publications()
	if len(publications) != 1 {
		t.Fatalf("pipeline publications = %d, want one", len(publications))
	}
	return publications[0].Evaluation
}

func unstructuredListForPipeline(resource *unstructured.Unstructured) *unstructured.UnstructuredList {
	return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*resource}}
}

func reconcileRequest(key types.NamespacedName) reconcile.Request {
	return reconcile.Request{NamespacedName: key}
}
