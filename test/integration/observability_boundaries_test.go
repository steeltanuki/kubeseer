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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/observability"
	"github.com/steeltanuki/kubeseer/internal/selection"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func assertObservabilityBoundaryScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	registry := prometheus.NewRegistry()
	capture := &observabilityCapture{err: errors.New("audit capture unavailable")}
	observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture})
	if err != nil {
		t.Fatalf("construct boundary observer: %v", err)
	}

	planSource := v1alpha1.KubeseerSource{
		ID:         "observability-page-source",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
	}
	plan, err := selection.NewPlanner(resolver).Plan(ctx, "team-a", planSource)
	if err != nil {
		t.Fatalf("plan observable page source: %v", err)
	}
	clusterRequest := requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "observability-cluster-source", APIVersion: "v1", Kind: "Node"}, "")
	requests := []accesspolicy.Request{selection.RequestForTarget(plan.Targets()[0]), clusterRequest}
	subject := authorization.Subject{Key: types.NamespacedName{Namespace: "team-a", Name: "observability-owner"}, UID: "observability-owner-uid", Generation: 1, PolicyEpoch: 1}
	clusterPolicy := basePolicy()
	clusterPolicy.Spec.AllowClusterScoped = true
	snapshot := mustSnapshot(t, clusterPolicy)
	enforcer := authorization.NewEnforcer(observability.NewAuthorizationRecorder(observer))
	batch, err := enforcer.EvaluateBatch(ctx, subject, snapshot, requests)
	if err != nil {
		t.Fatalf("evaluate observable authorization batch: %v", err)
	}
	if len(capture.snapshot()) != 2 {
		t.Fatalf("authorization records = %d, want ordered batch of 2", len(capture.snapshot()))
	}
	for index, record := range capture.snapshot() {
		if record.Event != observability.EventAuthorizationDecision || record.Authorization == nil {
			t.Fatalf("authorization capture %d = %#v", index, record)
		}
		if record.Authorization.SourceID != requests[index].SourceID {
			t.Fatalf("authorization capture %d source = %q, want %q", index, record.Authorization.SourceID, requests[index].SourceID)
		}
	}
	authorized, err := selection.BindCapabilities(plan, batch.Outcomes()[:1])
	if err != nil {
		t.Fatalf("bind observable page capability: %v", err)
	}
	pageLister := &scriptedResourceLister{responses: []scriptedListResponse{
		{list: listWithContinue(paginationObject("team-a", "safe-pod", "safe-pod-uid"), "next")},
		{list: listWithContinue(nil, "")},
	}}
	executor := selection.NewExecutor(pageLister,
		selection.WithVerifier(authorization.VerifierFunc(func(candidate authorization.Subject) bool { return candidate == subject })),
		selection.WithPageObserver(observability.NewPageObserver(observer)),
	)
	outcome := executor.Execute(ctx, authorized)
	if outcome.Err != nil || len(outcome.Resources) != 1 || len(pageLister.calls) != 2 {
		t.Fatalf("observable page outcome = %#v calls=%d", outcome, len(pageLister.calls))
	}
	assertObservabilityMetricValue(t, registry, "kubeseer_resources_read_total", `scope="namespaced"`, 1)
	assertObservabilityMetricValue(t, registry, "kubeseer_authorization_decisions_total", `kind="PolicyDecision",outcome="allowed",reason="Allowed"`, 2)

	forbiddenCapture := &observabilityCapture{err: errors.New("forbidden capture unavailable")}
	forbiddenObserver, err := observability.New(observability.Options{Registerer: prometheus.NewRegistry(), LogSink: forbiddenCapture})
	if err != nil {
		t.Fatalf("construct forbidden observer: %v", err)
	}
	forbiddenBatch, err := authorization.NewEnforcer(observability.NewAuthorizationRecorder(forbiddenObserver)).EvaluateBatch(ctx, subject, snapshot, []accesspolicy.Request{selection.RequestForTarget(plan.Targets()[0])})
	if err != nil {
		t.Fatalf("evaluate forbidden authorization: %v", err)
	}
	forbiddenPlan, err := selection.BindCapabilities(plan, forbiddenBatch.Outcomes())
	if err != nil {
		t.Fatalf("bind forbidden authorization: %v", err)
	}
	forbiddenLister := &scriptedResourceLister{responses: []scriptedListResponse{{err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "observed-object-sentinel", errors.New("raw-rbac-body-sentinel"))}}}
	forbiddenOutcome := selection.NewExecutor(forbiddenLister,
		selection.WithVerifier(authorization.VerifierFunc(func(candidate authorization.Subject) bool { return candidate == subject })),
		selection.WithPageObserver(observability.NewPageObserver(forbiddenObserver)),
	).Execute(ctx, forbiddenPlan)
	if !selection.HasReason(forbiddenOutcome.Err, selection.ReasonReadForbidden) || len(forbiddenLister.calls) != 1 {
		t.Fatalf("forbidden boundary outcome = %#v calls=%d", forbiddenOutcome, len(forbiddenLister.calls))
	}
	for _, record := range forbiddenCapture.snapshot() {
		if record.Event != observability.EventAuthorizationDecision && record.Event != observability.EventAuthorizationReadForbidden {
			t.Fatalf("unexpected forbidden event = %#v", record.Event)
		}
		encoded, marshalErr := json.Marshal(record)
		if marshalErr != nil {
			t.Fatalf("marshal forbidden capture: %v", marshalErr)
		}
		if strings.Contains(string(encoded), "observed-object-sentinel") || strings.Contains(string(encoded), "raw-rbac-body-sentinel") {
			t.Fatalf("forbidden data crossed audit boundary: %s", encoded)
		}
	}
}
