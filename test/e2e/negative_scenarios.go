// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func scenarioMissingTypeDegradation(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-007")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-007 namespace: %v", err)
	}
	if _, err := fixtures.InstallFixtureCRD(ctx); err != nil {
		t.Fatalf("E2E-007 fixture CRD: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{"", fixtureAPIGroup}, Kinds: []string{"Pod", fixtureKind}}})
	if _, err := fixtures.CreatePod(ctx, fixtures.scopedName("sibling"), namespace, nil, map[string]interface{}{"value": "sibling-value"}); err != nil {
		t.Fatalf("E2E-007 successful sibling: %v", err)
	}
	if _, err := fixtures.CreateWidget(ctx, fixtures.scopedName("missing"), namespace, map[string]interface{}{"name": "temporary"}, nil); err != nil {
		t.Fatalf("E2E-007 temporary Widget: %v", err)
	}
	object, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{
		sourceSpec("sibling", "v1", "Pod", []string{namespace}, nil, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil),
		sourceSpec("missing-type", fixtureAPIGroup+"/"+fixtureAPIVersion, fixtureKind, []string{namespace}, nil, nil, []map[string]interface{}{{"name": "name", "path": "{.spec.name}", "type": "string"}}, nil),
	}})
	if err != nil {
		t.Fatalf("E2E-007 Kubeseer: %v", err)
	}
	if err := session.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, "widgets."+fixtureAPIGroup, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("E2E-007 remove served type: %v", err)
	}
	snapshot := mustObservedSnapshot(t, session, ctx, object, "E2E-007", func(snapshot StatusSnapshot) bool {
		if snapshot.Result == nil || len(snapshot.Result.Sources) != 2 {
			return false
		}
		return snapshot.Result.Sources[0].State == v1alpha1.SourceStateValues && snapshot.Result.Sources[1].State == v1alpha1.SourceStateError && conditionStatus(snapshot.Conditions, "Degraded", metav1.ConditionTrue)
	})
	if snapshot.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue == nil || *snapshot.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue != "sibling-value" {
		t.Fatalf("E2E-007 sibling preservation = %#v", snapshot.Result.Sources)
	}
	if snapshot.Result.Sources[1].Error != nil && strings.Contains(snapshot.Result.Sources[1].Error.Message, "temporary") {
		t.Fatal("E2E-007 missing-type status leaked fixture value")
	}
}

func scenarioAuthorizationPolicyDrift(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-008")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-008 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	pod, err := fixtures.CreatePod(ctx, fixtures.scopedName("protected"), namespace, nil, map[string]interface{}{"value": "protected-value"})
	if err != nil {
		t.Fatalf("E2E-008 protected Pod: %v", err)
	}
	object, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("protected", "v1", "Pod", []string{namespace}, map[string]interface{}{"name": pod.GetName()}, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)}})
	if err != nil {
		t.Fatalf("E2E-008 Kubeseer: %v", err)
	}
	_ = mustReadySnapshot(t, session, ctx, object, "E2E-008")
	setPolicy(t, session, ctx, []string{"kube-system"}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	snapshot := mustObservedSnapshot(t, session, ctx, object, "E2E-008", func(snapshot StatusSnapshot) bool {
		return conditionStatus(snapshot.Conditions, "Authorized", metav1.ConditionFalse) && snapshot.Result != nil && (len(snapshot.Result.Sources) == 0 || len(snapshot.Result.Sources[0].Resources) == 0)
	})
	if conditionStatus(snapshot.Conditions, "Authorized", metav1.ConditionTrue) {
		t.Fatal("E2E-008 authorization remained allowed after policy narrowing")
	}
	forbidden := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kubeseer.io/v1alpha1", "kind": "Kubeseer",
		"metadata": map[string]interface{}{"name": fixtures.scopedName("forbidden"), "namespace": namespace},
		"spec":     map[string]interface{}{"sources": []interface{}{sourceSpec("forbidden", "v1", "Pod", []string{namespace}, nil, nil, nil, nil)}},
	}}
	_, err = session.Dynamic.Resource(KubeseerResource).Namespace(namespace).Create(ctx, forbidden, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if !apierrors.IsForbidden(err) {
		t.Fatalf("E2E-008 forbidden dry-run error = %v", err)
	}
}

func scenarioResourceStatusLimits(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-009")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-009 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	for index := 0; index < 260; index++ {
		if _, err := fixtures.CreatePod(ctx, fixtures.scopedName("matched-"+strconv.Itoa(index)), namespace, map[string]string{"limit": "matched"}, map[string]interface{}{"value": "bounded"}); err != nil {
			t.Fatalf("E2E-009 matched resource %d: %v", index, err)
		}
	}
	matched, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("matched-owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("matched", "v1", "Pod", []string{namespace}, nil, map[string]interface{}{"matchLabels": map[string]interface{}{"limit": "matched"}}, nil, nil)}})
	if err != nil {
		t.Fatalf("E2E-009 matched-limit Kubeseer: %v", err)
	}
	matchedSnapshot := mustObservedSnapshot(t, session, ctx, matched, "E2E-009", func(snapshot StatusSnapshot) bool {
		if snapshot.Result == nil || len(snapshot.Result.Sources) != 1 || snapshot.Result.Sources[0].Error == nil {
			return false
		}
		return snapshot.Result.Sources[0].Error.Reason == "SelectionLimitExceeded"
	})
	if matchedSnapshot.Result == nil || len(matchedSnapshot.Result.Sources) != 1 || matchedSnapshot.Result.Sources[0].State != v1alpha1.SourceStateError || matchedSnapshot.Result.Sources[0].Resources != nil {
		t.Fatal("E2E-009 matched-resource ceiling was not source-atomic")
	}
	largeValue := strings.Repeat("x", 70000)
	if _, err := fixtures.CreatePod(ctx, fixtures.scopedName("oversized"), namespace, map[string]string{"limit": "status"}, map[string]interface{}{"value": largeValue}); err != nil {
		t.Fatalf("E2E-009 oversized fixture: %v", err)
	}
	oversized, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("status-owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("oversized", "v1", "Pod", []string{namespace}, nil, map[string]interface{}{"matchLabels": map[string]interface{}{"limit": "status"}}, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)}})
	if err != nil {
		t.Fatalf("E2E-009 status-limit Kubeseer: %v", err)
	}
	snapshot := mustObservedSnapshot(t, session, ctx, oversized, "E2E-009", func(snapshot StatusSnapshot) bool {
		return conditionReason(snapshot.Conditions, "Ready", "ResultLimitExceeded")
	})
	if snapshot.Result != nil || snapshot.Summary != nil || snapshot.ResultHash != "" {
		t.Fatal("E2E-009 oversized status retained a truncated result")
	}
}

func conditionReason(conditions []metav1.Condition, kind, reason string) bool {
	for _, condition := range conditions {
		if string(condition.Type) == kind && condition.Reason == reason {
			return true
		}
	}
	return false
}
