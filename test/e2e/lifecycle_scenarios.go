// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

func scenarioSourceUpdateNoop(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-010")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-010 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	pod, err := fixtures.CreatePod(ctx, fixtures.scopedName("mutable"), namespace, nil, map[string]interface{}{"value": "before"})
	if err != nil {
		t.Fatalf("E2E-010 source Pod: %v", err)
	}
	owner, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("mutable", "v1", "Pod", []string{namespace}, map[string]interface{}{"name": pod.GetName()}, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)}})
	if err != nil {
		t.Fatalf("E2E-010 Kubeseer: %v", err)
	}
	initial := mustReadySnapshot(t, session, ctx, owner, "E2E-010")
	if initial.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue == nil || *initial.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue != "before" {
		t.Fatalf("E2E-010 initial result = %#v", initial.Result)
	}
	currentPod, err := session.Core.CoreV1().Pods(namespace).Get(ctx, pod.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("E2E-010 read mutable Pod: %v", err)
	}
	currentPod.Annotations["value"] = "after"
	if _, err := session.Core.CoreV1().Pods(namespace).Update(ctx, currentPod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("E2E-010 update source: %v", err)
	}
	updated := mustObservedSnapshot(t, session, ctx, owner, "E2E-010", func(snapshot StatusSnapshot) bool {
		return snapshot.Result != nil && len(snapshot.Result.Sources) == 1 && len(snapshot.Result.Sources[0].Resources) == 1 && len(snapshot.Result.Sources[0].Resources[0].Fields) == 1 && len(snapshot.Result.Sources[0].Resources[0].Fields[0].Matches) == 1 && snapshot.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue != nil && *snapshot.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue == "after"
	})
	if updated.ResourceVersion == initial.ResourceVersion {
		t.Fatalf("E2E-010 changed source did not publish a new status resourceVersion")
	}
	beforeEvents, err := session.ListEvents(ctx, namespace, "involvedObject.name="+owner.GetName())
	if err != nil {
		t.Fatalf("E2E-010 list status Events: %v", err)
	}
	beforeMetric := metricCounter(t, session, "kubeseer_status_updates_total")
	currentPod, err = session.Core.CoreV1().Pods(namespace).Get(ctx, pod.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("E2E-010 reread source: %v", err)
	}
	if currentPod.Annotations == nil {
		currentPod.Annotations = map[string]string{}
	}
	currentPod.Annotations["unrelated"] = "no-op"
	if _, err := session.Core.CoreV1().Pods(namespace).Update(ctx, currentPod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("E2E-010 semantic no-op update: %v", err)
	}
	if err := QuietWindow(ctx, 5*time.Second, func(observeCtx context.Context) (string, error) {
		current, readErr := session.GetKubeseer(observeCtx, owner.GetNamespace(), owner.GetName())
		if readErr != nil {
			return "", readErr
		}
		return current.ResourceVersion, nil
	}, updated.ResourceVersion); err != nil {
		t.Fatalf("E2E-010 no-op status write: %v", err)
	}
	afterEvents, err := session.ListEvents(ctx, namespace, "involvedObject.name="+owner.GetName())
	if err != nil {
		t.Fatalf("E2E-010 reread status Events: %v", err)
	}
	afterMetric := metricCounter(t, session, "kubeseer_status_updates_total")
	if len(afterEvents) != len(beforeEvents) && len(afterEvents) != len(beforeEvents)+1 {
		t.Fatalf("E2E-010 no-op Event cardinality changed unexpectedly: before=%d after=%d", len(beforeEvents), len(afterEvents))
	}
	if beforeMetric >= 0 && afterMetric != beforeMetric {
		t.Fatalf("E2E-010 no-op status metric changed: before=%v after=%v", beforeMetric, afterMetric)
	}
}

func scenarioSourceDeletionPolicy(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-011")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-011 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	pod, err := fixtures.CreatePod(ctx, fixtures.scopedName("deletable"), namespace, nil, map[string]interface{}{"value": "contribution"})
	if err != nil {
		t.Fatalf("E2E-011 source Pod: %v", err)
	}
	owner, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("deletable", "v1", "Pod", []string{namespace}, map[string]interface{}{"name": pod.GetName()}, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)}})
	if err != nil {
		t.Fatalf("E2E-011 Kubeseer: %v", err)
	}
	_ = mustReadySnapshot(t, session, ctx, owner, "E2E-011")
	if err := session.Core.CoreV1().Pods(namespace).Delete(ctx, pod.GetName(), metav1.DeleteOptions{}); err != nil {
		t.Fatalf("E2E-011 delete source: %v", err)
	}
	removed := mustObservedSnapshot(t, session, ctx, owner, "E2E-011", func(snapshot StatusSnapshot) bool {
		return snapshot.Result != nil && len(snapshot.Result.Sources) == 1 && len(snapshot.Result.Sources[0].Resources) == 0
	})
	if removed.Summary == nil || removed.Summary.MatchedResources != 0 {
		t.Fatalf("E2E-011 deleted contribution summary = %#v", removed.Summary)
	}
	setPolicy(t, session, ctx, []string{"kube-system"}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	denied := mustObservedSnapshot(t, session, ctx, owner, "E2E-011", func(snapshot StatusSnapshot) bool {
		return conditionStatus(snapshot.Conditions, "Authorized", metav1.ConditionFalse)
	})
	if conditionStatus(denied.Conditions, "Authorized", metav1.ConditionTrue) || (denied.Result != nil && len(denied.Result.Sources) > 0 && len(denied.Result.Sources[0].Resources) > 0) {
		t.Fatalf("E2E-011 policy restriction retained protected contribution: %#v", denied)
	}
}

func scenarioManagerRestart(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-012")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-012 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	pod, err := fixtures.CreatePod(ctx, fixtures.scopedName("restart-source"), namespace, nil, map[string]interface{}{"value": "survives-restart"})
	if err != nil {
		t.Fatalf("E2E-012 source Pod: %v", err)
	}
	owner, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("restart", "v1", "Pod", []string{namespace}, map[string]interface{}{"name": pod.GetName()}, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)}})
	if err != nil {
		t.Fatalf("E2E-012 Kubeseer: %v", err)
	}
	_ = mustReadySnapshot(t, session, ctx, owner, "E2E-012")
	before := managerPod(t, session, ctx)
	if err := session.Core.CoreV1().Pods(session.Metadata.Namespace).Delete(ctx, before.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("E2E-012 delete manager Pod: %v", err)
	}
	if err := session.WaitFor(ctx, "E2E-012", "manager Pod replacement", func(waitCtx context.Context) (bool, error) {
		current, listErr := managerPodMaybe(session, waitCtx)
		if listErr != nil {
			return false, listErr
		}
		if current == nil {
			// A ReplicaSet replacement has a short interval with no active Pod;
			// that is an expected intermediate observation, not a polling error.
			return false, nil
		}
		return current.UID != before.UID && current.Status.Conditions != nil && podReady(current), nil
	}); err != nil {
		t.Fatalf("E2E-012 manager replacement: %v", err)
	}
	recovered := mustReadySnapshot(t, session, ctx, owner, "E2E-012")
	if recovered.Result == nil || len(recovered.Result.Sources) != 1 || len(recovered.Result.Sources[0].Resources) != 1 {
		t.Fatalf("E2E-012 recovered public result = %#v", recovered.Result)
	}
}

func scenarioKubeseerDeletion(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-013")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-013 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	pod, err := fixtures.CreatePod(ctx, fixtures.scopedName("preserved"), namespace, nil, map[string]interface{}{"value": "preserve"})
	if err != nil {
		t.Fatalf("E2E-013 source Pod: %v", err)
	}
	owner, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("preserved", "v1", "Pod", []string{namespace}, map[string]interface{}{"name": pod.GetName()}, nil, nil, nil)}})
	if err != nil {
		t.Fatalf("E2E-013 Kubeseer: %v", err)
	}
	_ = mustReadySnapshot(t, session, ctx, owner, "E2E-013")
	if err := session.Dynamic.Resource(KubeseerResource).Namespace(namespace).Delete(ctx, owner.GetName(), metav1.DeleteOptions{}); err != nil {
		t.Fatalf("E2E-013 delete Kubeseer: %v", err)
	}
	if err := session.WaitFor(ctx, "E2E-013", "Kubeseer deletion", func(waitCtx context.Context) (bool, error) {
		_, getErr := session.Dynamic.Resource(KubeseerResource).Namespace(namespace).Get(waitCtx, owner.GetName(), metav1.GetOptions{})
		return apierrors.IsNotFound(getErr), nil
	}); err != nil {
		t.Fatalf("E2E-013 deletion completion: %v", err)
	}
	if _, err := session.Core.CoreV1().Pods(namespace).Get(ctx, pod.GetName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("E2E-013 observed resource was deleted with Kubeseer: %v", err)
	}
	if current := managerPod(t, session, ctx); !podReady(current) {
		t.Fatalf("E2E-013 manager health after deletion = %#v", current.Status.Conditions)
	}
}

func scenarioOverlappingInstances(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-014")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-014 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	pod, err := fixtures.CreatePod(ctx, fixtures.scopedName("shared"), namespace, nil, map[string]interface{}{"value": "shared-result"})
	if err != nil {
		t.Fatalf("E2E-014 shared Pod: %v", err)
	}
	first, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("first"), map[string]interface{}{"sources": []interface{}{sourceSpec("first", "v1", "Pod", []string{namespace}, map[string]interface{}{"name": pod.GetName()}, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)}})
	if err != nil {
		t.Fatalf("E2E-014 first Kubeseer: %v", err)
	}
	second, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("second"), map[string]interface{}{"sources": []interface{}{sourceSpec("second", "v1", "Pod", []string{namespace}, map[string]interface{}{"name": pod.GetName()}, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)}})
	if err != nil {
		t.Fatalf("E2E-014 second Kubeseer: %v", err)
	}
	_ = mustReadySnapshot(t, session, ctx, first, "E2E-014")
	_ = mustReadySnapshot(t, session, ctx, second, "E2E-014")
	currentSecond, err := session.Dynamic.Resource(KubeseerResource).Namespace(namespace).Get(ctx, second.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("E2E-014 reread second Kubeseer: %v", err)
	}
	// Use a schema-valid type conversion failure while retaining the shared
	// source and the first instance's independent status.
	sources, found, err := unstructured.NestedSlice(currentSecond.Object, "spec", "sources")
	if err != nil || !found || len(sources) != 1 {
		t.Fatalf("E2E-014 source shape: found=%v err=%v", found, err)
	}
	sourceMap, ok := sources[0].(map[string]interface{})
	if !ok {
		t.Fatalf("E2E-014 source is not an object: %#v", sources[0])
	}
	fields, found, err := unstructured.NestedSlice(sourceMap, "fields")
	if err != nil || !found || len(fields) != 1 {
		t.Fatalf("E2E-014 field shape: found=%v err=%v", found, err)
	}
	fieldMap, ok := fields[0].(map[string]interface{})
	if !ok {
		t.Fatalf("E2E-014 field is not an object: %#v", fields[0])
	}
	fieldMap["type"] = "integer"
	fields[0] = fieldMap
	sourceMap["fields"] = fields
	sources[0] = sourceMap
	if err := unstructured.SetNestedSlice(currentSecond.Object, sources, "spec", "sources"); err != nil {
		t.Fatalf("E2E-014 prepare isolated failure: %v", err)
	}
	if _, err := session.Dynamic.Resource(KubeseerResource).Namespace(namespace).Update(ctx, currentSecond, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("E2E-014 mutate second instance: %v", err)
	}
	failed := mustObservedSnapshot(t, session, ctx, currentSecond, "E2E-014", func(snapshot StatusSnapshot) bool {
		return conditionStatus(snapshot.Conditions, "Degraded", metav1.ConditionTrue)
	})
	if failed.Result == nil {
		t.Fatalf("E2E-014 failed overlapping result was discarded")
	}
	firstCurrent := mustObservedSnapshot(t, session, ctx, first, "E2E-014", func(snapshot StatusSnapshot) bool {
		return conditionStatus(snapshot.Conditions, "Ready", metav1.ConditionTrue) && snapshot.Result != nil
	})
	if firstCurrent.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue == nil || *firstCurrent.Result.Sources[0].Resources[0].Fields[0].Matches[0].StringValue != "shared-result" {
		t.Fatalf("E2E-014 unaffected overlapping instance = %#v", firstCurrent.Result)
	}
}

func metricCounter(t *testing.T, session *ClusterSession, family string) float64 {
	t.Helper()
	endpoint := strings.TrimSpace(getenv("KUBESEER_E2E_METRICS_URL"))
	if endpoint != "" {
		value := float64(-1)
		pollCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		_ = wait.PollUntilContextTimeout(pollCtx, 200*time.Millisecond, 12*time.Second, true, func(ctx context.Context) (bool, error) {
			requestCtx, requestCancel := context.WithTimeout(ctx, 2*time.Second)
			defer requestCancel()
			request, requestErr := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/metrics", nil)
			if requestErr != nil {
				return false, nil
			}
			response, requestErr := session.HTTP.Do(request)
			if requestErr != nil {
				return false, nil
			}
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
				return false, nil
			}
			if parsed, ok := findMetricValue(string(body), family); ok {
				value = parsed
				return true, nil
			}
			return false, nil
		})
		return value
	}
	body, err := session.REST.Get().Namespace(session.Metadata.Namespace).Resource("services").Name(session.Metadata.Release+"-metrics").Suffix("proxy", "metrics").Do(context.Background()).Raw()
	if err == nil {
		if value, ok := findMetricValue(string(body), family); ok {
			return value
		}
	}
	return -1
}

func findMetricValue(body, family string) (float64, bool) {
	var total float64
	found := false
	for _, line := range strings.Split(body, "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || strings.SplitN(parts[0], "{", 2)[0] != family {
			continue
		}
		if family == "kubeseer_status_updates_total" && (!strings.Contains(parts[0], `outcome="written"`) || !strings.Contains(parts[0], `reason="EvaluationSucceeded"`)) {
			continue
		}
		value, err := strconv.ParseFloat(parts[1], 64)
		if err == nil {
			total += value
			found = true
		}
	}
	return total, found
}

func getenv(name string) string { return strings.TrimSpace(os.Getenv(name)) }

func managerPod(t *testing.T, session *ClusterSession, ctx context.Context) *corev1.Pod {
	t.Helper()
	pod, err := managerPodMaybe(session, ctx)
	if err != nil {
		t.Fatalf("read manager Pod: %v", err)
	}
	if pod == nil {
		t.Fatalf("read manager Pod: manager Pod is not present")
	}
	return pod
}

func managerPodMaybe(session *ClusterSession, ctx context.Context) (*corev1.Pod, error) {
	pods, err := session.Core.CoreV1().Pods(session.Metadata.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=kubeseer,app.kubernetes.io/instance=" + session.Metadata.Release})
	if err != nil {
		return nil, err
	}
	for index := range pods.Items {
		if pods.Items[index].DeletionTimestamp == nil {
			return &pods.Items[index], nil
		}
	}
	return nil, nil
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
