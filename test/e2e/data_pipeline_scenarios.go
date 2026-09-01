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

package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

func scenarioSameNamespaceDeployment(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-002")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-002 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{"apps"}, Kinds: []string{"Deployment"}}})

	deploymentName := fixtures.scopedName("deployment")
	deployment, err := fixtures.CreateDeployment(ctx, deploymentName, map[string]string{"fixture": "deployment"}, 2)
	if err != nil {
		t.Fatalf("E2E-002 Deployment: %v", err)
	}
	object, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{
		"sources": []interface{}{sourceSpec("deployment", "apps/v1", "Deployment", []string{namespace}, map[string]interface{}{"name": deploymentName}, nil, []map[string]interface{}{{"name": "replicas", "path": "{.spec.replicas}", "type": "integer"}}, nil)},
	})
	if err != nil {
		t.Fatalf("E2E-002 Kubeseer: %v", err)
	}
	snapshot := mustReadySnapshot(t, session, ctx, object, "E2E-002")
	assertReadyStatus(t, snapshot, []string{"deployment"})
	source := snapshot.Result.Sources[0]
	if source.State != v1alpha1.SourceStateValues || len(source.Resources) != 1 {
		t.Fatalf("E2E-002 public result = %#v", source)
	}
	resource := source.Resources[0]
	if resource.APIVersion != "apps/v1" || resource.Kind != "Deployment" || resource.Namespace != namespace || resource.Name != deployment.Name || resource.UID != deployment.UID {
		t.Fatalf("E2E-002 provenance = %#v", resource)
	}
	if len(resource.Fields) != 1 || len(resource.Fields[0].Matches) != 1 || resource.Fields[0].Matches[0].IntegerValue == nil || *resource.Fields[0].Matches[0].IntegerValue != 2 {
		t.Fatalf("E2E-002 typed replicas = %#v", resource.Fields)
	}
}

func scenarioMultiNamespacePods(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-003")
	defer cleanupFixtures(t, fixtures)
	firstNamespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-003 namespace: %v", err)
	}
	secondNamespace := fixtures.scopedName("peer")
	createNamespace(t, session, ctx, secondNamespace, fixtures.Labels)
	fixtures.register("namespace/"+secondNamespace, func(cleanupCtx context.Context) error {
		err := session.Core.CoreV1().Namespaces().Delete(cleanupCtx, secondNamespace, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{firstNamespace, secondNamespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})

	first, err := fixtures.CreatePod(ctx, "same-name", firstNamespace, map[string]string{"tier": "blue"}, map[string]interface{}{"value": "first"})
	if err != nil {
		t.Fatalf("E2E-003 first Pod: %v", err)
	}
	second, err := fixtures.CreatePod(ctx, "same-name", secondNamespace, map[string]string{"tier": "blue"}, map[string]interface{}{"value": "second"})
	if err != nil {
		t.Fatalf("E2E-003 second Pod: %v", err)
	}
	object, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{
		"sources": []interface{}{sourceSpec("pods", "v1", "Pod", []string{secondNamespace, firstNamespace}, nil, nil, []map[string]interface{}{{"name": "value", "path": "{.metadata.annotations.value}", "type": "string"}}, nil)},
	})
	if err != nil {
		t.Fatalf("E2E-003 Kubeseer: %v", err)
	}
	snapshot := mustReadySnapshot(t, session, ctx, object, "E2E-003")
	assertReadyStatus(t, snapshot, []string{"pods"})
	resources := snapshot.Result.Sources[0].Resources
	if len(resources) != 2 {
		t.Fatalf("E2E-003 resources = %#v", resources)
	}
	if resources[0].Name != first.Name || resources[0].Namespace != firstNamespace || resources[0].UID != first.UID || resources[1].Name != second.Name || resources[1].Namespace != secondNamespace || resources[1].UID != second.UID {
		t.Fatalf("E2E-003 deterministic provenance order = %#v", resources)
	}
}

func scenarioCustomResourceLabels(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-004")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-004 namespace: %v", err)
	}
	if _, err := fixtures.InstallFixtureCRD(ctx); err != nil {
		t.Fatalf("E2E-004 fixture CRD: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{namespace}, []resourceRule{{APIGroups: []string{"fixtures.kubeseer.io"}, Kinds: []string{"Widget"}}})
	gold, err := fixtures.CreateWidget(ctx, fixtures.scopedName("gold"), namespace, map[string]interface{}{"name": "gold", "replicas": int64(3)}, map[string]string{"tier": "gold"})
	if err != nil {
		t.Fatalf("E2E-004 gold Widget: %v", err)
	}
	if _, err := fixtures.CreateWidget(ctx, fixtures.scopedName("silver"), namespace, map[string]interface{}{"name": "silver", "replicas": int64(1)}, map[string]string{"tier": "silver"}); err != nil {
		t.Fatalf("E2E-004 silver Widget: %v", err)
	}
	object, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{
		"sources": []interface{}{sourceSpec("widgets", "fixtures.kubeseer.io/v1alpha1", "Widget", []string{namespace}, nil, map[string]interface{}{"matchLabels": map[string]interface{}{"tier": "gold"}}, []map[string]interface{}{{"name": "replicas", "path": "{.spec.replicas}", "type": "integer"}}, nil)},
	})
	if err != nil {
		t.Fatalf("E2E-004 Kubeseer: %v", err)
	}
	snapshot := mustReadySnapshot(t, session, ctx, object, "E2E-004")
	assertReadyStatus(t, snapshot, []string{"widgets"})
	resources := snapshot.Result.Sources[0].Resources
	if len(resources) != 1 || resources[0].Name != gold.GetName() || resources[0].Kind != "Widget" || resources[0].UID != gold.GetUID() {
		t.Fatalf("E2E-004 label selection/provenance = %#v", resources)
	}
}

func scenarioExtractionTypingOperators(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-005")
	defer cleanupFixtures(t, fixtures)
	namespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-005 namespace: %v", err)
	}
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}}, namespace)
	keep, err := fixtures.CreatePod(ctx, fixtures.scopedName("keep"), namespace, map[string]string{"operator": "candidate"}, map[string]interface{}{"value": "keep-me"})
	if err != nil {
		t.Fatalf("E2E-005 keep Pod: %v", err)
	}
	if _, err := fixtures.CreatePod(ctx, fixtures.scopedName("drop"), namespace, map[string]string{"operator": "candidate"}, map[string]interface{}{"value": "drop-me"}); err != nil {
		t.Fatalf("E2E-005 drop Pod: %v", err)
	}
	fields := []map[string]interface{}{
		{"name": "scalar", "path": "{.metadata.annotations.value}", "type": "string", "operators": []interface{}{operator("contains", stringOperand("keep"))}},
		{"name": "list", "path": "{.spec.containers}", "type": "list"},
		{"name": "converted", "path": "{.metadata.annotations.value}", "type": "integer"},
	}
	object, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("pods", "v1", "Pod", []string{namespace}, nil, nil, fields, nil)}})
	if err != nil {
		t.Fatalf("E2E-005 Kubeseer: %v", err)
	}
	snapshot := mustObservedSnapshot(t, session, ctx, object, "E2E-005", func(snapshot StatusSnapshot) bool {
		return snapshot.Result != nil && len(snapshot.Result.Sources) == 1 && len(snapshot.Result.Sources[0].Resources) == 1 && conditionStatus(snapshot.Conditions, "Degraded", metav1.ConditionTrue)
	})
	if !conditionStatus(snapshot.Conditions, "Accepted", metav1.ConditionTrue) || !conditionStatus(snapshot.Conditions, "Authorized", metav1.ConditionTrue) || !conditionStatus(snapshot.Conditions, "SourcesResolved", metav1.ConditionTrue) || conditionStatus(snapshot.Conditions, "Ready", metav1.ConditionTrue) {
		t.Fatalf("E2E-005 canonical degraded conditions = %#v", snapshot.Conditions)
	}
	AssertSourceOrder(t, snapshot.Result, []string{"pods"})
	resources := snapshot.Result.Sources[0].Resources
	if len(resources) != 1 || resources[0].Name != keep.Name {
		t.Fatalf("E2E-005 operator selection = %#v", resources)
	}
	if len(resources[0].Fields) != 3 {
		t.Fatalf("E2E-005 field results = %#v", resources[0].Fields)
	}
	fieldsByName := make(map[string]v1alpha1.KubeseerFieldResult, len(resources[0].Fields))
	for _, field := range resources[0].Fields {
		fieldsByName[field.Name] = field
	}
	scalar := fieldsByName["scalar"]
	if scalar.State != v1alpha1.FieldStateValues || len(scalar.Matches) != 1 || scalar.Matches[0].StringValue == nil || *scalar.Matches[0].StringValue != "keep-me" {
		t.Fatalf("E2E-005 scalar operator result = %#v", scalar)
	}
	list := fieldsByName["list"]
	if list.State != v1alpha1.FieldStateValues || len(list.Matches) != 1 || list.Matches[0].ListValue == nil {
		t.Fatalf("E2E-005 list result = %#v", list)
	}
	var listValues []map[string]interface{}
	if err := json.Unmarshal([]byte(*list.Matches[0].ListValue), &listValues); err != nil || len(listValues) != 1 || listValues[0]["name"] != "fixture" {
		t.Fatalf("E2E-005 list result = %q (err=%v)", *list.Matches[0].ListValue, err)
	}
	converted := fieldsByName["converted"]
	if converted.State != v1alpha1.FieldStateError || scalar.State != v1alpha1.FieldStateValues || list.State != v1alpha1.FieldStateValues {
		t.Fatalf("E2E-005 conversion/sibling preservation = %#v", resources)
	}
}

func scenarioNumericAggregation(ctx context.Context, t *testing.T, session *ClusterSession) {
	fixtures := mustFixtures(t, session, "E2E-006")
	defer cleanupFixtures(t, fixtures)
	firstNamespace, err := fixtures.EnsureNamespace(ctx)
	if err != nil {
		t.Fatalf("E2E-006 namespace: %v", err)
	}
	secondNamespace := fixtures.scopedName("peer")
	createNamespace(t, session, ctx, secondNamespace, fixtures.Labels)
	fixtures.register("namespace/"+secondNamespace, func(cleanupCtx context.Context) error {
		err := session.Core.CoreV1().Namespaces().Delete(cleanupCtx, secondNamespace, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	previous := mustPolicy(t, session, ctx)
	defer restorePolicy(t, session, ctx, previous)
	setPolicy(t, session, ctx, []string{firstNamespace, secondNamespace}, []resourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}})
	for _, fixture := range []struct {
		name, namespace, group, value string
	}{
		{"aggregate-a", firstNamespace, "blue", "1"},
		{"aggregate-b", secondNamespace, "red", "3"},
		{"aggregate-c", secondNamespace, "blue", "2"},
		{"aggregate-invalid", secondNamespace, "blue", "not-an-integer"},
	} {
		if _, err := fixtures.CreatePod(ctx, fixture.name, fixture.namespace, nil, map[string]interface{}{"group": fixture.group, "value": fixture.value}); err != nil {
			t.Fatalf("E2E-006 contributor %s: %v", fixture.name, err)
		}
	}
	fields := []map[string]interface{}{{"name": "group", "path": "{.metadata.annotations.group}", "type": "string"}, {"name": "value", "path": "{.metadata.annotations.value}", "type": "integer"}}
	aggregations := []map[string]interface{}{{"name": "sum-by-group", "function": "sum", "field": "value", "groupBy": []interface{}{"group"}, "includeProvenance": true}, {"name": "average", "function": "average", "field": "value", "includeProvenance": true}}
	object, err := fixtures.CreateKubeseer(ctx, fixtures.scopedName("owner"), map[string]interface{}{"sources": []interface{}{sourceSpec("pods", "v1", "Pod", []string{firstNamespace, secondNamespace}, nil, nil, fields, aggregations)}})
	if err != nil {
		t.Fatalf("E2E-006 Kubeseer: %v", err)
	}
	snapshot := mustObservedSnapshot(t, session, ctx, object, "E2E-006", func(snapshot StatusSnapshot) bool {
		return snapshot.Result != nil && len(snapshot.Result.Sources) == 1 && len(snapshot.Result.Sources[0].Aggregates) == 2
	})
	if snapshot.Result.Sources[0].State != v1alpha1.SourceStateValues || len(snapshot.Result.Sources[0].Resources) != 4 {
		t.Fatalf("E2E-006 source result = %#v", snapshot.Result.Sources[0])
	}
	aggregatesByName := make(map[string]v1alpha1.KubeseerAggregateResult, len(snapshot.Result.Sources[0].Aggregates))
	for _, aggregate := range snapshot.Result.Sources[0].Aggregates {
		aggregatesByName[aggregate.Name] = aggregate
	}
	aggregate := aggregatesByName["sum-by-group"]
	if aggregate.Name != "sum-by-group" || aggregate.State != v1alpha1.AggregateStateDegraded || len(aggregate.Groups) != 2 || len(aggregate.Failures) != 1 {
		t.Fatalf("E2E-006 degraded grouped aggregate = %#v", aggregate)
	}
	if aggregate.Groups[0].Value.Matches[0].Value.IntegerValue == nil || *aggregate.Groups[0].Value.Matches[0].Value.IntegerValue != 3 || aggregate.Groups[1].Value.Matches[0].Value.IntegerValue == nil || *aggregate.Groups[1].Value.Matches[0].Value.IntegerValue != 3 {
		t.Fatalf("E2E-006 grouped totals = %#v", aggregate.Groups)
	}
	if len(aggregate.Groups[0].Contributors) == 0 || len(aggregate.Groups[1].Contributors) == 0 {
		t.Fatalf("E2E-006 contributor provenance = %#v", aggregate.Groups)
	}
	average := aggregatesByName["average"]
	if average.Name != "average" || average.State != v1alpha1.AggregateStateDegraded || len(average.Groups) != 1 || len(average.Groups[0].Value.Matches) != 1 || average.Groups[0].Value.Matches[0].Value.NumberValue == nil || *average.Groups[0].Value.Matches[0].Value.NumberValue != "2" || len(average.Groups[0].Contributors) != 3 {
		t.Fatalf("E2E-006 average aggregate = %#v", average)
	}
}

type resourceRule struct {
	APIGroups []string
	Kinds     []string
}

func sourceSpec(id, apiVersion, kind string, namespaces []string, selector, fieldSelector map[string]interface{}, fields, aggregations []map[string]interface{}) map[string]interface{} {
	source := map[string]interface{}{"id": id, "resource": map[string]interface{}{"apiVersion": apiVersion, "kind": kind}}
	if namespaces != nil {
		values := make([]interface{}, len(namespaces))
		for index := range namespaces {
			values[index] = namespaces[index]
		}
		source["namespaces"] = map[string]interface{}{"names": values}
	}
	if selector != nil {
		source["selector"] = selector
	}
	if fieldSelector != nil {
		source["selector"] = fieldSelector
	}
	if fields != nil {
		values := make([]interface{}, len(fields))
		for index := range fields {
			values[index] = fields[index]
		}
		source["fields"] = values
	}
	if aggregations != nil {
		values := make([]interface{}, len(aggregations))
		for index := range aggregations {
			values[index] = aggregations[index]
		}
		source["aggregations"] = values
	}
	return source
}

func operator(name string, operand map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"operator": name, "value": operand}
}

func stringOperand(value string) map[string]interface{} {
	return map[string]interface{}{"state": "value", "stringValue": value}
}

func mustFixtures(t *testing.T, session *ClusterSession, id string) *FixtureSet {
	t.Helper()
	fixtures, err := NewFixtureSet(session, id)
	if err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func cleanupFixtures(t *testing.T, fixtures *FixtureSet) {
	t.Helper()
	if err := fixtures.Cleanup(context.Background()); err != nil {
		t.Fatalf("%s fixture cleanup: %v", fixtures.ScenarioID, err)
	}
}

func createNamespace(t *testing.T, session *ClusterSession, ctx context.Context, name string, labels map[string]string) {
	t.Helper()
	namespace := coreNamespace(name, labels)
	if _, err := session.Core.CoreV1().Namespaces().Create(ctx, &namespace, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace %s: %v", name, err)
	}
}

func coreNamespace(name string, labels map[string]string) v1Namespace {
	return v1Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

// v1Namespace aliases the public Kubernetes Namespace shape without leaking
// fixture payloads into status assertions.
type v1Namespace = corev1.Namespace

func mustPolicy(t *testing.T, session *ClusterSession, ctx context.Context) *unstructured.Unstructured {
	t.Helper()
	policy, err := session.Dynamic.Resource(PolicyResource).Get(ctx, v1alpha1.InstallationAccessCeilingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read installation policy: %v", err)
	}
	return policy.DeepCopy()
}

func setPolicy(t *testing.T, session *ClusterSession, ctx context.Context, args ...interface{}) {
	t.Helper()
	namespaces := []string{}
	var rules []resourceRule
	for _, arg := range args {
		switch value := arg.(type) {
		case string:
			namespaces = append(namespaces, value)
		case []string:
			namespaces = append(namespaces, value...)
		case []resourceRule:
			rules = value
		}
	}
	policy := mustPolicy(t, session, ctx)
	namespaceValues := make([]interface{}, len(namespaces))
	for index := range namespaces {
		namespaceValues[index] = namespaces[index]
	}
	resourceValues := make([]interface{}, len(rules))
	for index, rule := range rules {
		groups := make([]interface{}, len(rule.APIGroups))
		for groupIndex := range rule.APIGroups {
			groups[groupIndex] = rule.APIGroups[groupIndex]
		}
		kinds := make([]interface{}, len(rule.Kinds))
		for kindIndex := range rule.Kinds {
			kinds[kindIndex] = rule.Kinds[kindIndex]
		}
		resourceValues[index] = map[string]interface{}{"apiGroups": groups, "kinds": kinds}
	}
	if err := unstructured.SetNestedField(policy.Object, map[string]interface{}{"mode": "Explicit", "include": namespaceValues, "exclude": []interface{}{}, "systemNamespaces": []interface{}{"kube-system", "kube-public", "kube-node-lease"}}, "spec", "namespaces"); err != nil {
		t.Fatalf("prepare policy namespaces: %v", err)
	}
	if err := unstructured.SetNestedField(policy.Object, resourceValues, "spec", "resources"); err != nil {
		t.Fatalf("prepare policy resources: %v", err)
	}
	if _, err := session.Dynamic.Resource(PolicyResource).Update(ctx, policy, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update installation policy: %v", err)
	}
}

func restorePolicy(t *testing.T, session *ClusterSession, ctx context.Context, policy *unstructured.Unstructured) {
	t.Helper()
	if policy == nil {
		return
	}
	resource := session.Dynamic.Resource(PolicyResource)
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, 60*time.Second, true, func(waitCtx context.Context) (bool, error) {
		current, getErr := resource.Get(waitCtx, v1alpha1.InstallationAccessCeilingName, metav1.GetOptions{})
		if getErr != nil {
			if retryablePolicyError(getErr) {
				lastErr = getErr
				return false, nil
			}
			return false, getErr
		}
		restored := policy.DeepCopy()
		restored.SetResourceVersion(current.GetResourceVersion())
		if _, updateErr := resource.Update(waitCtx, restored, metav1.UpdateOptions{}); updateErr == nil {
			return true, nil
		} else if retryablePolicyError(updateErr) {
			lastErr = updateErr
			return false, nil
		} else {
			return false, updateErr
		}
	})
	if err != nil {
		if lastErr != nil {
			t.Fatalf("restore installation policy: %v", lastErr)
		}
		t.Fatalf("restore installation policy: %v", err)
	}
}

func retryablePolicyError(err error) bool {
	if err == nil {
		return false
	}
	return apierrors.IsConflict(err) || apierrors.IsInternalError(err) || apierrors.IsServiceUnavailable(err) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err)
}

func mustReadySnapshot(t *testing.T, session *ClusterSession, ctx context.Context, object *unstructured.Unstructured, id string) StatusSnapshot {
	t.Helper()
	return mustObservedSnapshot(t, session, ctx, object, id, func(snapshot StatusSnapshot) bool {
		return conditionStatus(snapshot.Conditions, "Ready", metav1.ConditionTrue) && snapshot.Result != nil
	})
}

func mustObservedSnapshot(t *testing.T, session *ClusterSession, ctx context.Context, object *unstructured.Unstructured, id string, predicate func(StatusSnapshot) bool) StatusSnapshot {
	t.Helper()
	snapshot, err := session.WaitForStatus(ctx, id, object.GetNamespace(), object.GetName(), "public status", predicate)
	if err != nil {
		t.Fatalf("%s status: %v", id, err)
	}
	return snapshot
}

func assertReadyStatus(t *testing.T, snapshot StatusSnapshot, sourceIDs []string) {
	t.Helper()
	if !conditionStatus(snapshot.Conditions, "Accepted", metav1.ConditionTrue) || !conditionStatus(snapshot.Conditions, "Authorized", metav1.ConditionTrue) || !conditionStatus(snapshot.Conditions, "SourcesResolved", metav1.ConditionTrue) || !conditionStatus(snapshot.Conditions, "Ready", metav1.ConditionTrue) || conditionStatus(snapshot.Conditions, "Degraded", metav1.ConditionTrue) {
		t.Fatalf("canonical conditions = %#v", snapshot.Conditions)
	}
	if snapshot.Summary == nil || snapshot.Summary.MatchedResources < 1 {
		t.Fatalf("canonical summary = %#v", snapshot.Summary)
	}
	AssertSourceOrder(t, snapshot.Result, sourceIDs)
}

func conditionStatus(conditions []metav1.Condition, kind string, expected metav1.ConditionStatus) bool {
	for _, condition := range conditions {
		if string(condition.Type) == kind && condition.Status == expected {
			return true
		}
	}
	return false
}
