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

package envtest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/admission"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	discoveryruntime "github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const reconciliationRuntimeEnvtestTimeout = 45 * time.Second

func TestEnvtestReconciliationRuntime(t *testing.T) {
	crdPath, err := filepath.Abs(filepath.Join("..", "..", "config", "crd", "bases"))
	if err != nil {
		t.Fatalf("resolve generated CRD path: %v", err)
	}
	environment, err := New(t, Options{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
		CRDMaxWait:            20 * time.Second,
		CRDPollInterval:       100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("create envtest harness: %v", err)
	}
	config, err := environment.Start()
	if err != nil {
		t.Fatalf("start Kubernetes API server: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reconciliationRuntimeEnvtestTimeout)
	defer cancel()
	clients, err := environment.Clients()
	if err != nil {
		t.Fatalf("create envtest clients: %v", err)
	}
	namespace := environment.Scope().Namespace
	observedCRD, observedGVR := runtimeObservedResourceCRD()
	if _, err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, observedCRD, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create disposable observed-resource CRD: %v", err)
	}
	environment.AddCleanup("delete observed-resource CRD", func(ctx context.Context) error {
		err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, observedCRD.Name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	})
	environment.AddCleanup("delete observed-resource fixtures", func(ctx context.Context) error {
		err := clients.Dynamic.Resource(observedGVR).Namespace(namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	})
	if err := WaitFor(ctx, 20*time.Second, func(ctx context.Context) (bool, error) {
		current, err := clients.APIExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, observedCRD.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait observed-resource CRD establishment: %v", err)
	}

	requestRecorder := newRuntimeObservedRequestRecorder(observedGVR, namespace)
	config.WrapTransport = func(base http.RoundTripper) http.RoundTripper {
		return &runtimeObservedRecordingTransport{base: base, recorder: requestRecorder}
	}

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("register Kubeseer scheme: %v", err)
	}
	managerInstance, err := ctrlmanager.New(config, ctrlmanager.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		t.Fatalf("create controller-runtime manager: %v", err)
	}
	if err := reconciliation.SetupWithManager(managerInstance, reconciliation.Options{
		SafetyInterval:     25 * time.Millisecond,
		WatchBackoffBase:   5 * time.Millisecond,
		WatchBackoffMax:    40 * time.Millisecond,
		EnqueueBackoffBase: 5 * time.Millisecond,
		EnqueueBackoffMax:  40 * time.Millisecond,
	}); err != nil {
		t.Fatalf("setup reconciliation runtime: %v", err)
	}

	apiClient, err := crclient.New(config, crclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create direct controller-runtime client: %v", err)
	}
	initialSource := runtimeEnvtestPodSource("runtime-source", false)
	existingKey := types.NamespacedName{Namespace: namespace, Name: "runtime-existing"}
	fanoutKey := types.NamespacedName{Namespace: namespace, Name: "runtime-fanout"}
	newKey := types.NamespacedName{Namespace: namespace, Name: "runtime-new"}
	watchKey := types.NamespacedName{Namespace: namespace, Name: "runtime-watch"}
	watchPeerKey := types.NamespacedName{Namespace: namespace, Name: "runtime-watch-peer"}
	statusKey := types.NamespacedName{Namespace: namespace, Name: "runtime-status"}
	observabilityKey := types.NamespacedName{Namespace: namespace, Name: "runtime-observability-event"}
	allFailedKey := types.NamespacedName{Namespace: namespace, Name: "runtime-all-failed"}
	emptyKey := types.NamespacedName{Namespace: namespace, Name: "runtime-empty"}
	adapterKey := types.NamespacedName{Namespace: namespace, Name: "runtime-adapter"}
	busyKey := types.NamespacedName{Namespace: namespace, Name: "runtime-busy"}
	freeKey := types.NamespacedName{Namespace: namespace, Name: "runtime-free"}
	deterministicKey := types.NamespacedName{Namespace: namespace, Name: "runtime-deterministic"}
	operatorKey := types.NamespacedName{Namespace: namespace, Name: "runtime-operators"}
	operatorInvalidKey := types.NamespacedName{Namespace: namespace, Name: "runtime-operators-invalid"}
	operatorFailureKey := types.NamespacedName{Namespace: namespace, Name: "runtime-operators-failure"}
	operatorSiblingKey := types.NamespacedName{Namespace: namespace, Name: "runtime-operators-sibling"}
	aggregationKey := types.NamespacedName{Namespace: namespace, Name: "runtime-aggregation"}
	aggregationNamespace := "runtime-aggregation-peer"
	if _, err := clients.Core.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: aggregationNamespace}}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create aggregation peer namespace: %v", err)
	}
	for _, object := range []*v1alpha1.Kubeseer{
		runtimeEnvtestKubeseer(existingKey, initialSource),
		runtimeEnvtestKubeseer(fanoutKey, initialSource),
	} {
		if err := apiClient.Create(ctx, object); err != nil {
			t.Fatalf("create pre-start Kubeseer %s/%s: %v", object.Namespace, object.Name, err)
		}
	}
	policy := runtimeEnvtestPolicy(namespace)
	environment.AddCleanup("delete reconciliation runtime fixtures", func(ctx context.Context) error {
		var cleanupErr error
		for _, key := range []types.NamespacedName{existingKey, fanoutKey, newKey, watchKey, watchPeerKey, statusKey, observabilityKey, allFailedKey, emptyKey, adapterKey, busyKey, freeKey, deterministicKey, operatorKey, operatorInvalidKey, operatorFailureKey, operatorSiblingKey, aggregationKey} {
			object := &v1alpha1.Kubeseer{}
			err := apiClient.Get(ctx, key, object)
			if apierrors.IsNotFound(err) {
				continue
			}
			if err == nil {
				if deleteErr := apiClient.Delete(ctx, object); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					cleanupErr = errors.Join(cleanupErr, deleteErr)
				}
			} else {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
		if deleteErr := apiClient.Delete(ctx, policy); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			cleanupErr = errors.Join(cleanupErr, deleteErr)
		}
		if deleteErr := clients.Core.CoreV1().Pods(namespace).Delete(ctx, "runtime-status-pod", metav1.DeleteOptions{}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			cleanupErr = errors.Join(cleanupErr, deleteErr)
		}
		if deleteErr := clients.Core.CoreV1().Namespaces().Delete(ctx, aggregationNamespace, metav1.DeleteOptions{}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			cleanupErr = errors.Join(cleanupErr, deleteErr)
		}
		return cleanupErr
	})

	managerContext, stopManager := context.WithCancel(context.Background())
	managerErr := make(chan error, 1)
	go func() { managerErr <- managerInstance.Start(managerContext) }()
	if !managerInstance.GetCache().WaitForCacheSync(ctx) {
		stopManager()
		t.Fatalf("controller-runtime cache did not synchronize")
	}
	managerStopped := false
	stopAndWaitManager := func() {
		if managerStopped {
			return
		}
		stopManager()
		select {
		case err := <-managerErr:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("stop controller-runtime manager: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("controller-runtime manager did not stop")
		}
		managerStopped = true
	}
	defer stopAndWaitManager()

	waitRuntimeSourceState(t, ctx, apiClient, existingKey, v1alpha1.SourceStateError)
	waitRuntimeSourceState(t, ctx, apiClient, fanoutKey, v1alpha1.SourceStateError)
	missingStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, existingKey, missingStatus); err != nil {
		t.Fatalf("read missing-policy status: %v", err)
	}
	assertRuntimeStatusSnapshot(t, missingStatus, missingStatus.Generation, true)
	assertRuntimeCondition(t, missingStatus.Status, statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonPolicyMissing)
	if err := apiClient.Create(ctx, policy); err != nil {
		t.Fatalf("create installation access policy: %v", err)
	}
	waitRuntimeSourceState(t, ctx, apiClient, existingKey, v1alpha1.SourceStateValues)
	waitRuntimeSourceState(t, ctx, apiClient, fanoutKey, v1alpha1.SourceStateValues)
	successStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, existingKey, successStatus); err != nil {
		t.Fatalf("read successful status snapshot: %v", err)
	}
	assertRuntimeStatusSnapshot(t, successStatus, successStatus.Generation, true)
	assertRuntimeCondition(t, successStatus.Status, statuscontract.ConditionReady, metav1.ConditionTrue, statuscontract.ReasonEvaluationSucceeded)
	assertRuntimeCondition(t, successStatus.Status, statuscontract.ConditionDegraded, metav1.ConditionFalse, statuscontract.ReasonEvaluationSucceeded)

	eventObject := runtimeEnvtestKubeseer(observabilityKey, runtimeEnvtestInvalidSource("observability-event-source"))
	if err := apiClient.Create(ctx, eventObject); err != nil {
		t.Fatalf("create observability Event Kubeseer: %v", err)
	}
	waitRuntimeSourceState(t, ctx, apiClient, observabilityKey, v1alpha1.SourceStateError)
	observabilityEvents := waitRuntimeKubeseerEvents(t, ctx, clients, observabilityKey, 1)
	if len(observabilityEvents) != 1 || observabilityEvents[0].Type != corev1.EventTypeWarning || observabilityEvents[0].Reason != statuscontract.ReasonAuthorizationNotEvaluated || strings.Contains(observabilityEvents[0].Message, "Missing") {
		t.Fatalf("persisted observability Event = %#v", observabilityEvents)
	}
	if err := WaitFor(ctx, 300*time.Millisecond, func(ctx context.Context) (bool, error) {
		events, err := listRuntimeKubeseerEvents(ctx, clients, observabilityKey)
		return len(events) == 1, err
	}); err != nil {
		t.Fatalf("unchanged reconcile emitted duplicate observability Event: %v", err)
	}

	aggregationPolicy := &v1alpha1.KubeseerAccessPolicy{}
	if err := apiClient.Get(ctx, types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}, aggregationPolicy); err != nil {
		t.Fatalf("read policy before cross-namespace aggregation: %v", err)
	}
	aggregationPolicy.Spec.Namespaces.Include = []string{namespace, aggregationNamespace}
	aggregationPolicy.Spec.Namespaces.Exclude = nil
	if err := apiClient.Update(ctx, aggregationPolicy); err != nil {
		t.Fatalf("authorize cross-namespace aggregation namespaces: %v", err)
	}
	aggregationPods := []*corev1.Pod{
		runtimeEnvtestAggregationPod("runtime-aggregation-a", namespace, "blue", "1", "1"),
		runtimeEnvtestAggregationPod("runtime-aggregation-b", aggregationNamespace, "red", "3", "1"),
		runtimeEnvtestAggregationPod("runtime-aggregation-c", aggregationNamespace, "blue", "2", "not-an-integer"),
	}
	for _, pod := range aggregationPods {
		if _, err := clients.Core.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create cross-namespace aggregation Pod %s/%s: %v", pod.Namespace, pod.Name, err)
		}
	}
	aggregationSource := runtimeEnvtestAggregationSource("aggregation-source", namespace, aggregationNamespace)
	aggregationSibling := runtimeEnvtestAggregationSiblingSource("aggregation-sibling", namespace)
	aggregationObject := runtimeEnvtestKubeseer(aggregationKey, aggregationSource)
	aggregationObject.Spec.Sources = []v1alpha1.KubeseerSource{aggregationSource, aggregationSibling}
	if err := apiClient.Create(ctx, aggregationObject); err != nil {
		t.Fatalf("create cross-namespace aggregation Kubeseer: %v", err)
	}
	waitRuntimeAggregationState(t, ctx, apiClient, aggregationKey, aggregationSource.ID, v1alpha1.SourceStateValues, 3)
	aggregationStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, aggregationKey, aggregationStatus); err != nil {
		t.Fatalf("read cross-namespace aggregation status: %v", err)
	}
	assertRuntimeStatusSnapshot(t, aggregationStatus, aggregationStatus.Generation, true)
	assertRuntimeCondition(t, aggregationStatus.Status, statuscontract.ConditionAccepted, metav1.ConditionFalse, statuscontract.ReasonInvalidConfiguration)
	assertRuntimeCondition(t, aggregationStatus.Status, statuscontract.ConditionDegraded, metav1.ConditionTrue, statuscontract.ReasonEvaluationDegraded)
	if len(aggregationStatus.Status.Result.Sources) != 2 || len(aggregationStatus.Status.Result.Sources[1].Aggregates) != 0 || aggregationStatus.Status.Result.Sources[1].State != v1alpha1.SourceStateValues {
		t.Fatalf("aggregate sibling preservation = %#v", aggregationStatus.Status.Result.Sources)
	}
	aggregationResult := aggregationStatus.Status.Result.Sources[0]
	if len(aggregationResult.Aggregates) != 3 || aggregationResult.Aggregates[0].Name != "average-default" || aggregationResult.Aggregates[1].Name != "invalid-plan" || aggregationResult.Aggregates[2].Name != "sum-bad" {
		t.Fatalf("aggregate declaration order = %#v", aggregationResult.Aggregates)
	}
	averageAggregate := aggregationResult.Aggregates[0]
	if averageAggregate.State != v1alpha1.AggregateStateValues || len(averageAggregate.Groups) != 1 || len(averageAggregate.Groups[0].Value.Matches) != 1 || averageAggregate.Groups[0].Value.Matches[0].Value.NumberValue == nil || *averageAggregate.Groups[0].Value.Matches[0].Value.NumberValue != "2" || len(averageAggregate.Groups[0].Contributors) != 3 {
		t.Fatalf("runtime average default/provenance = %#v", averageAggregate)
	}
	if invalid := aggregationResult.Aggregates[1]; invalid.State != v1alpha1.AggregateStateError || invalid.Error == nil || invalid.Error.Reason != "unknown-field" {
		t.Fatalf("runtime invalid aggregation plan = %#v", invalid)
	}
	badAggregate := aggregationResult.Aggregates[2]
	if badAggregate.State != v1alpha1.AggregateStateDegraded || len(badAggregate.Groups) != 1 || len(badAggregate.Failures) != 1 || strings.Contains(badAggregate.Failures[0].Error.Message, "not-an-integer") {
		t.Fatalf("runtime sanitized aggregate failure = %#v", badAggregate)
	}
	statusWritesBeforeNoop := requestRecorder.StatusWrites(aggregationKey.Name)
	noopPod, err := clients.Core.CoreV1().Pods(namespace).Get(ctx, "runtime-aggregation-a", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read aggregation Pod before no-op event: %v", err)
	}
	noopPod.Annotations = map[string]string{"unrelated": "status-no-op"}
	if _, err := clients.Core.CoreV1().Pods(namespace).Update(ctx, noopPod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update unrelated aggregation Pod metadata: %v", err)
	}
	assertRuntimeStatusWritesStable(t, ctx, requestRecorder, aggregationKey.Name, statusWritesBeforeNoop, 250*time.Millisecond)

	aggregationPolicy = &v1alpha1.KubeseerAccessPolicy{}
	if err := apiClient.Get(ctx, types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}, aggregationPolicy); err != nil {
		t.Fatalf("read policy before unavailable namespace proof: %v", err)
	}
	aggregationPolicy.Spec.Namespaces.Include = []string{namespace}
	if err := apiClient.Update(ctx, aggregationPolicy); err != nil {
		t.Fatalf("remove aggregation peer authorization: %v", err)
	}
	waitRuntimeAggregationDenied(t, ctx, apiClient, aggregationKey, aggregationSource.ID)

	operatorPods := []*corev1.Pod{
		runtimeEnvtestOperatorPod("runtime-operator-kept", namespace, "yes", "keep-me"),
		runtimeEnvtestOperatorPod("runtime-operator-dropped", namespace, "yes", "drop-me"),
		runtimeEnvtestOperatorPod("runtime-operator-good", namespace, "failure", "1"),
		runtimeEnvtestOperatorPod("runtime-operator-bad", namespace, "failure", "bad"),
	}
	for _, pod := range operatorPods {
		if _, err := clients.Core.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create value-operator Pod %s: %v", pod.Name, err)
		}
	}
	operatorValid := runtimeEnvtestValueOperatorSource("operator-filter", "yes", v1alpha1.ValueTypeString, v1alpha1.OperatorContains, stringOperatorOperand("keep"))
	operatorObject := runtimeEnvtestKubeseer(operatorKey, operatorValid)
	if err := apiClient.Create(ctx, operatorObject); err != nil {
		t.Fatalf("create value-operator Kubeseer: %v", err)
	}
	waitRuntimeValueOperatorState(t, ctx, apiClient, operatorKey, "operator-filter", v1alpha1.SourceStateValues, 1, "keep-me")
	operatorStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, operatorKey, operatorStatus); err != nil {
		t.Fatalf("read value-operator status: %v", err)
	}
	assertRuntimeStatusSnapshot(t, operatorStatus, operatorStatus.Generation, true)
	assertRuntimeCondition(t, operatorStatus.Status, statuscontract.ConditionReady, metav1.ConditionTrue, statuscontract.ReasonEvaluationSucceeded)

	operatorInvalid := runtimeEnvtestValueOperatorSource("operator-invalid", "yes", v1alpha1.ValueTypeString, v1alpha1.OperatorMatches, stringOperatorOperand("["))
	invalidObject := runtimeEnvtestKubeseer(operatorInvalidKey, operatorInvalid)
	if err := apiClient.Create(ctx, invalidObject); err != nil {
		t.Fatalf("create invalid value-operator Kubeseer: %v", err)
	}
	waitRuntimeValueOperatorState(t, ctx, apiClient, operatorInvalidKey, "operator-invalid", v1alpha1.SourceStateValues, 0, "")
	invalidStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, operatorInvalidKey, invalidStatus); err != nil {
		t.Fatalf("read invalid value-operator status: %v", err)
	}
	if len(invalidStatus.Status.Result.Sources[0].FieldErrors) != 1 || invalidStatus.Status.Result.Sources[0].FieldErrors[0].Reason != string(operators.ReasonInvalidPattern) {
		t.Fatalf("invalid value-operator field errors = %#v", invalidStatus.Status.Result.Sources[0].FieldErrors)
	}
	assertRuntimeStatusSnapshot(t, invalidStatus, invalidStatus.Generation, true)
	assertRuntimeCondition(t, invalidStatus.Status, statuscontract.ConditionAccepted, metav1.ConditionFalse, statuscontract.ReasonInvalidConfiguration)

	operatorFailure := runtimeEnvtestValueOperatorSource("operator-failure", "failure", v1alpha1.ValueTypeInteger, v1alpha1.OperatorEq, integerOperatorOperand(1))
	failureObject := runtimeEnvtestKubeseer(operatorFailureKey, operatorFailure)
	if err := apiClient.Create(ctx, failureObject); err != nil {
		t.Fatalf("create failing value-operator Kubeseer: %v", err)
	}
	waitRuntimeValueOperatorState(t, ctx, apiClient, operatorFailureKey, "operator-failure", v1alpha1.SourceStateValues, 2, "")
	failureStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, operatorFailureKey, failureStatus); err != nil {
		t.Fatalf("read failing value-operator status: %v", err)
	}
	var failureResource *v1alpha1.KubeseerResourceResult
	for index := range failureStatus.Status.Result.Sources[0].Resources {
		resource := &failureStatus.Status.Result.Sources[0].Resources[index]
		if resource.Name == "runtime-operator-bad" {
			failureResource = resource
			break
		}
	}
	if failureResource == nil || len(failureResource.Fields) != 0 || failureResource.Error == nil || failureResource.Error.Reason != string(operators.ReasonInvalidInput) || strings.Contains(failureResource.Error.Message, "bad") {
		t.Fatalf("failing value-operator resource = %#v", failureResource)
	}
	assertRuntimeStatusSnapshot(t, failureStatus, failureStatus.Generation, true)
	assertRuntimeCondition(t, failureStatus.Status, statuscontract.ConditionDegraded, metav1.ConditionTrue, statuscontract.ReasonEvaluationDegraded)

	siblingObject := runtimeEnvtestKubeseer(operatorSiblingKey, operatorValid)
	siblingObject.Spec.Sources = []v1alpha1.KubeseerSource{operatorValid, runtimeEnvtestValueOperatorSource("operator-plain", "yes", v1alpha1.ValueTypeString, "", nil)}
	if err := apiClient.Create(ctx, siblingObject); err != nil {
		t.Fatalf("create sibling value-operator Kubeseer: %v", err)
	}
	waitRuntimeValueOperatorState(t, ctx, apiClient, operatorSiblingKey, "operator-filter", v1alpha1.SourceStateValues, 1, "keep-me")
	waitRuntimeValueOperatorState(t, ctx, apiClient, operatorSiblingKey, "operator-plain", v1alpha1.SourceStateValues, 2, "")

	newObject := runtimeEnvtestKubeseer(newKey, runtimeEnvtestPodSource("runtime-new-source", false))
	if err := apiClient.Create(ctx, newObject); err != nil {
		t.Fatalf("create post-start Kubeseer: %v", err)
	}
	waitRuntimeSourceState(t, ctx, apiClient, newKey, v1alpha1.SourceStateValues)

	observedResources := clients.Dynamic.Resource(observedGVR).Namespace(namespace)
	matchingName := "observed-matching"
	enterLeaveName := "observed-enter-leave"
	for _, fixture := range []*unstructured.Unstructured{
		runtimeEnvtestObservation(matchingName, namespace, "yes", "observed-initial-value"),
		runtimeEnvtestObservation(enterLeaveName, namespace, "no", "observed-enter-leave-value"),
	} {
		if _, err := observedResources.Create(ctx, fixture, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create observed-resource fixture %s: %v", fixture.GetName(), err)
		}
	}

	watchOwner := runtimeEnvtestKubeseer(watchKey, runtimeEnvtestObservedSource("observed-source-a"))
	watchOwner.Spec.Sources = []v1alpha1.KubeseerSource{
		runtimeEnvtestObservedSource("observed-source-a"),
		runtimeEnvtestObservedSource("observed-source-b"),
	}
	watchPeer := runtimeEnvtestKubeseer(watchPeerKey, runtimeEnvtestObservedSource("observed-peer-source"))
	requestRecorder.Reset()
	if err := apiClient.Create(ctx, watchOwner); err != nil {
		t.Fatalf("create observed-resource Kubeseer: %v", err)
	}
	if err := apiClient.Create(ctx, watchPeer); err != nil {
		t.Fatalf("create observed-resource peer Kubeseer: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 1, "observed-initial-value")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 1, "observed-initial-value")
	watchStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, watchKey, watchStatus); err != nil {
		t.Fatalf("read successful observed status snapshot: %v", err)
	}
	assertRuntimeStatusSnapshot(t, watchStatus, watchStatus.Generation, true)
	observedRequests := requestRecorder.Requests()
	firstWatch, firstList, watchCount := runtimeObservedRequestIndexes(observedRequests)
	if watchCount != 1 {
		t.Fatalf("expected one shared observed-resource WATCH for three source bindings, got %d", watchCount)
	}
	if firstWatch < 0 || firstList < 0 || firstWatch > firstList {
		t.Fatalf("expected observed-resource WATCH before selection LIST, requests=%v", observedRequests)
	}
	for _, request := range observedRequests {
		if request.Watch {
			if request.HasLabelSelector || request.HasFieldSelector {
				t.Fatalf("metadata WATCH unexpectedly carried a selector: %+v", request)
			}
			continue
		}
		if !request.HasLabelSelector {
			t.Fatalf("selection LIST did not carry the source selector: %+v", request)
		}
	}
	if requestRecorder.ActiveWatches() == 0 {
		t.Fatalf("expected one active metadata WATCH")
	}
	for _, request := range observedRequests {
		if strings.Contains(request.Path, "observed-initial-value") || strings.Contains(request.Path, "{.spec.value}") {
			t.Fatalf("routing request retained fixture value or field path: %+v", request)
		}
	}

	matching, err := observedResources.Get(ctx, matchingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read matching observed-resource fixture: %v", err)
	}
	if err := unstructured.SetNestedField(matching.Object, "observed-updated-value", "spec", "value"); err != nil {
		t.Fatalf("update matching observed-resource value: %v", err)
	}
	if _, err := observedResources.Update(ctx, matching, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("persist matching observed-resource update: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 1, "observed-updated-value")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 1, "observed-updated-value")

	policyCurrent := &v1alpha1.KubeseerAccessPolicy{}
	if err := apiClient.Get(ctx, types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}, policyCurrent); err != nil {
		t.Fatalf("read policy before narrowing observed-resource access: %v", err)
	}
	policyCurrent.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{""}, Kinds: []string{"Pod"}}}
	requestRecorder.Reset()
	if err := apiClient.Update(ctx, policyCurrent); err != nil {
		t.Fatalf("narrow installation policy: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateError, -1, "")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateError, -1, "")
	if err := WaitFor(ctx, 10*time.Second, func(context.Context) (bool, error) {
		return requestRecorder.ActiveWatches() == 0, nil
	}); err != nil {
		t.Fatalf("narrowed policy did not stop observed-resource WATCH: %v", err)
	}
	requestRecorder.Reset()
	assertRuntimeObservedRequestsAbsent(t, ctx, requestRecorder, 150*time.Millisecond)

	if err := apiClient.Get(ctx, types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}, policyCurrent); err != nil {
		t.Fatalf("read policy before broadening observed-resource access: %v", err)
	}
	policyCurrent.Spec.Resources = []v1alpha1.ResourceRule{
		{APIGroups: []string{""}, Kinds: []string{"Pod"}},
		{APIGroups: []string{"runtime.kubeseer.io"}, Kinds: []string{"Observation"}},
	}
	requestRecorder.Reset()
	if err := apiClient.Update(ctx, policyCurrent); err != nil {
		t.Fatalf("broaden installation policy: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 1, "observed-updated-value")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 1, "observed-updated-value")
	if requestRecorder.ActiveWatches() == 0 {
		t.Fatalf("broadened policy did not require a fresh observed-resource WATCH")
	}

	requestRecorder.Reset()
	requestRecorder.SetPolicyUnavailable(true)
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateError, -1, "")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateError, -1, "")
	policyUnavailableStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, watchKey, policyUnavailableStatus); err != nil {
		t.Fatalf("read unavailable-policy status snapshot: %v", err)
	}
	assertRuntimeStatusSnapshot(t, policyUnavailableStatus, policyUnavailableStatus.Generation, true)
	assertRuntimeCondition(t, policyUnavailableStatus.Status, statuscontract.ConditionAuthorized, metav1.ConditionUnknown, statuscontract.ReasonAuthorizationUnavailable)
	if err := WaitFor(ctx, 10*time.Second, func(context.Context) (bool, error) {
		return requestRecorder.ActiveWatches() == 0, nil
	}); err != nil {
		t.Fatalf("unavailable policy did not stop observed-resource WATCH: %v", err)
	}
	requestRecorder.Reset()
	assertRuntimeObservedRequestsAbsent(t, ctx, requestRecorder, 150*time.Millisecond)
	requestRecorder.SetPolicyUnavailable(false)
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 1, "observed-updated-value")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 1, "observed-updated-value")

	policyCurrent = &v1alpha1.KubeseerAccessPolicy{}
	if err := apiClient.Get(ctx, types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}, policyCurrent); err != nil {
		t.Fatalf("read policy before deletion state proof: %v", err)
	}
	requestRecorder.Reset()
	if err := apiClient.Delete(ctx, policyCurrent); err != nil {
		t.Fatalf("delete installation policy: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateError, -1, "")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateError, -1, "")
	missingObservedStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, watchKey, missingObservedStatus); err != nil {
		t.Fatalf("read missing-policy observed status: %v", err)
	}
	assertRuntimeCondition(t, missingObservedStatus.Status, statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonPolicyMissing)
	if err := WaitFor(ctx, 10*time.Second, func(context.Context) (bool, error) {
		return requestRecorder.ActiveWatches() == 0, nil
	}); err != nil {
		t.Fatalf("deleted policy did not stop observed-resource WATCH: %v", err)
	}
	requestRecorder.Reset()
	assertRuntimeObservedRequestsAbsent(t, ctx, requestRecorder, 150*time.Millisecond)

	policy = runtimeEnvtestPolicy(namespace)
	if err := apiClient.Create(ctx, policy); err != nil {
		t.Fatalf("re-create installation policy: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 1, "observed-updated-value")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 1, "observed-updated-value")
	if err := WaitFor(ctx, 10*time.Second, func(context.Context) (bool, error) {
		return requestRecorder.ActiveWatches() > 0, nil
	}); err != nil {
		t.Fatalf("re-created policy did not reconstruct observed-resource WATCH: %v", err)
	}

	watchCountBeforeClose := requestRecorder.WatchRequestCount()
	if !requestRecorder.CloseOneWatch() {
		t.Fatalf("expected an active observed-resource WATCH to close")
	}
	matching, err = observedResources.Get(ctx, matchingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read matching observed-resource fixture before watch-gap update: %v", err)
	}
	if err := unstructured.SetNestedField(matching.Object, "observed-after-watch-close", "spec", "value"); err != nil {
		t.Fatalf("prepare watch-gap observed-resource update: %v", err)
	}
	if _, err := observedResources.Update(ctx, matching, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("persist watch-gap observed-resource update: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 1, "observed-after-watch-close")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 1, "observed-after-watch-close")
	if err := WaitFor(ctx, 10*time.Second, func(context.Context) (bool, error) {
		return requestRecorder.WatchRequestCount() >= watchCountBeforeClose+1 && requestRecorder.ActiveWatches() > 0, nil
	}); err != nil {
		t.Fatalf("closed observed-resource WATCH did not restart: %v", err)
	}

	enterLeave, err := observedResources.Get(ctx, enterLeaveName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read selector enter/leave fixture: %v", err)
	}
	enterLeave.SetLabels(map[string]string{"watch": "yes"})
	if enterLeave, err = observedResources.Update(ctx, enterLeave, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("enter selector with observed-resource fixture: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 2, "")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 2, "")
	enterLeave.SetLabels(map[string]string{"watch": "no"})
	if _, err := observedResources.Update(ctx, enterLeave, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("leave selector with observed-resource fixture: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 1, "observed-after-watch-close")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 1, "observed-after-watch-close")
	if err := observedResources.Delete(ctx, matchingName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete matching observed-resource fixture: %v", err)
	}
	waitRuntimeObservedState(t, ctx, apiClient, watchKey, 2, v1alpha1.SourceStateValues, 0, "")
	waitRuntimeObservedState(t, ctx, apiClient, watchPeerKey, 1, v1alpha1.SourceStateValues, 0, "")

	watchCurrent := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, watchKey, watchCurrent); err != nil {
		t.Fatalf("read watched Kubeseer before source cleanup: %v", err)
	}
	watchCurrent.Spec.Sources = nil
	if err := apiClient.Update(ctx, watchCurrent); err != nil {
		t.Fatalf("remove observed-resource sources: %v", err)
	}
	waitRuntimeEmptyResult(t, ctx, apiClient, watchKey)
	if err := apiClient.Delete(ctx, watchPeer); err != nil {
		t.Fatalf("delete watched peer Kubeseer: %v", err)
	}
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		current := &v1alpha1.Kubeseer{}
		err := apiClient.Get(ctx, watchPeerKey, current)
		if apierrors.IsNotFound(err) {
			return requestRecorder.ActiveWatches() == 0, nil
		}
		return false, err
	}); err != nil {
		t.Fatalf("source and owner deletion did not stop observed-resource WATCH: %v", err)
	}

	statusPodName := "runtime-status-pod"
	statusPod, err := clients.Core.CoreV1().Pods(namespace).Create(ctx, runtimeEnvtestStatusPod(statusPodName, namespace), metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create status publication Pod: %v", err)
	}
	statusSource := runtimeEnvtestPodFieldSource("status-success")
	statusKeyObject := &v1alpha1.Kubeseer{
		ObjectMeta: metav1.ObjectMeta{Namespace: statusKey.Namespace, Name: statusKey.Name},
		Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{
			statusSource,
			runtimeEnvtestInvalidSource("status-invalid"),
		}},
	}
	if err := apiClient.Create(ctx, statusKeyObject); err != nil {
		t.Fatalf("create mixed status Kubeseer: %v", err)
	}
	waitRuntimeMixedStatus(t, ctx, apiClient, statusKey, statusKeyObject.Generation, 1)
	statusBeforeCondition := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, statusKey, statusBeforeCondition); err != nil {
		t.Fatalf("read mixed status before condition preservation: %v", err)
	}
	statusBeforeCondition.Status.Conditions = []metav1.Condition{{Type: "External", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now(), Reason: "Fixture", Message: "must survive runtime publication"}}
	if err := apiClient.Status().Update(ctx, statusBeforeCondition); err != nil {
		t.Fatalf("persist condition before generation-only publication: %v", err)
	}
	statusBeforeGeneration := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, statusKey, statusBeforeGeneration); err != nil {
		t.Fatalf("read mixed status before generation-only update: %v", err)
	}
	statusBeforeResult := statusBeforeGeneration.Status.Result.DeepCopy()
	statusWritesBeforeGeneration := requestRecorder.StatusWrites(statusKey.Name)
	statusBeforeGeneration.Spec.Sources[0].Selector = &v1alpha1.ResourceSelector{
		Name:        statusPodName,
		MatchLabels: map[string]string{"runtime-status": "yes"},
	}
	if err := apiClient.Update(ctx, statusBeforeGeneration); err != nil {
		t.Fatalf("change status selector without changing selected result: %v", err)
	}
	waitRuntimeMixedStatus(t, ctx, apiClient, statusKey, statusBeforeGeneration.Generation, 1)
	statusAfterGeneration := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, statusKey, statusAfterGeneration); err != nil {
		t.Fatalf("read mixed status after generation-only publication: %v", err)
	}
	if !reflect.DeepEqual(statusBeforeResult, statusAfterGeneration.Status.Result) {
		t.Fatalf("generation-only publication changed the semantic result: before=%#v after=%#v", statusBeforeResult, statusAfterGeneration.Status.Result)
	}
	if len(statusAfterGeneration.Status.Conditions) != 6 || statusAfterGeneration.Status.Conditions[5].Type != "External" || statusAfterGeneration.Status.Conditions[5].Message != "must survive runtime publication" {
		t.Fatalf("generation-only publication did not preserve conditions: %#v", statusAfterGeneration.Status.Conditions)
	}
	assertRuntimeStatusSnapshot(t, statusAfterGeneration, statusAfterGeneration.Generation, true)
	if statusAfterGeneration.Spec.Sources[0].Selector == nil || statusAfterGeneration.Spec.Sources[0].Selector.Name != statusPodName {
		t.Fatalf("generation-only publication did not preserve the latest spec: %#v", statusAfterGeneration.Spec.Sources[0].Selector)
	}
	if requestRecorder.StatusWrites(statusKey.Name) != statusWritesBeforeGeneration+1 {
		t.Fatalf("generation-only publication status writes=%d, want %d", requestRecorder.StatusWrites(statusKey.Name), statusWritesBeforeGeneration+1)
	}

	statusPod, err = clients.Core.CoreV1().Pods(namespace).Get(ctx, statusPodName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read status Pod before unchanged source event: %v", err)
	}
	statusPod.Annotations = map[string]string{"runtime-event": "unchanged-result"}
	if _, err := clients.Core.CoreV1().Pods(namespace).Update(ctx, statusPod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update status Pod without changing selection: %v", err)
	}
	statusWritesBeforeSourceEvent := requestRecorder.StatusWrites(statusKey.Name)
	assertRuntimeStatusWritesStable(t, ctx, requestRecorder, statusKey.Name, statusWritesBeforeSourceEvent, 150*time.Millisecond)

	if err := clients.Core.CoreV1().Pods(namespace).Delete(ctx, statusPodName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete selected status Pod: %v", err)
	}
	waitRuntimeMixedStatus(t, ctx, apiClient, statusKey, statusAfterGeneration.Generation, 0)
	if _, err := clients.Core.CoreV1().Pods(namespace).Create(ctx, runtimeEnvtestStatusPod(statusPodName, namespace), metav1.CreateOptions{}); err != nil {
		t.Fatalf("recreate selected status Pod: %v", err)
	}
	waitRuntimeMixedStatus(t, ctx, apiClient, statusKey, statusAfterGeneration.Generation, 1)

	allFailedObject := &v1alpha1.Kubeseer{
		ObjectMeta: metav1.ObjectMeta{Namespace: allFailedKey.Namespace, Name: allFailedKey.Name},
		Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{
			runtimeEnvtestInvalidSource("all-failed-discovery"),
			runtimeEnvtestInvalidSelectorSource("all-failed-selector"),
		}},
	}
	if err := apiClient.Create(ctx, allFailedObject); err != nil {
		t.Fatalf("create all-failed Kubeseer: %v", err)
	}
	waitRuntimeSourceStates(t, ctx, apiClient, allFailedKey, 2, v1alpha1.SourceStateError, 0, "")
	allFailedStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, allFailedKey, allFailedStatus); err != nil {
		t.Fatalf("read all-failed status snapshot: %v", err)
	}
	assertRuntimeStatusSnapshot(t, allFailedStatus, allFailedStatus.Generation, true)
	assertRuntimeCondition(t, allFailedStatus.Status, statuscontract.ConditionAccepted, metav1.ConditionFalse, statuscontract.ReasonInvalidConfiguration)
	assertRuntimeCondition(t, allFailedStatus.Status, statuscontract.ConditionReady, metav1.ConditionFalse, statuscontract.ReasonEvaluationDegraded)
	allFailedWrites := requestRecorder.StatusWrites(allFailedKey.Name)
	assertRuntimeStatusWritesStable(t, ctx, requestRecorder, allFailedKey.Name, allFailedWrites, 150*time.Millisecond)

	emptyObject := &v1alpha1.Kubeseer{ObjectMeta: metav1.ObjectMeta{Namespace: emptyKey.Namespace, Name: emptyKey.Name}}
	if err := apiClient.Create(ctx, emptyObject); err != nil {
		t.Fatalf("create zero-source Kubeseer: %v", err)
	}
	waitRuntimeEmptyResult(t, ctx, apiClient, emptyKey)
	emptyStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, emptyKey, emptyStatus); err != nil {
		t.Fatalf("read zero-source status snapshot: %v", err)
	}
	assertRuntimeStatusSnapshot(t, emptyStatus, emptyStatus.Generation, true)
	if emptyStatus.Status.Summary == nil || emptyStatus.Status.Summary.SuccessfulSources != 0 || emptyStatus.Status.Summary.FailedSources != 0 || emptyStatus.Status.Summary.MatchedResources != 0 {
		t.Fatalf("zero-source summary = %#v", emptyStatus.Status.Summary)
	}

	updated := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, existingKey, updated); err != nil {
		t.Fatalf("read existing Kubeseer before generation burst: %v", err)
	}
	updated.Spec.Sources = []v1alpha1.KubeseerSource{runtimeEnvtestPodSource("runtime-follow-up-a", true)}
	if err := apiClient.Update(ctx, updated); err != nil {
		t.Fatalf("update first generation: %v", err)
	}
	updated.Spec.Sources = []v1alpha1.KubeseerSource{runtimeEnvtestPodSource("runtime-follow-up-b", true)}
	if err := apiClient.Update(ctx, updated); err != nil {
		t.Fatalf("update second generation: %v", err)
	}
	waitRuntimeObservedGeneration(t, ctx, apiClient, existingKey, updated.Generation)

	statusOnly := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, existingKey, statusOnly); err != nil {
		t.Fatalf("read existing Kubeseer before status-only update: %v", err)
	}
	statusOnly.Status.Conditions = append(statusOnly.Status.Conditions, metav1.Condition{Type: "External", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now(), Reason: "Fixture", Message: "status-only event"})
	if err := apiClient.Status().Update(ctx, statusOnly); err != nil {
		t.Fatalf("update status-only condition: %v", err)
	}
	stableVersion := statusOnly.ResourceVersion
	observations := 0
	if err := WaitFor(ctx, 3*time.Second, func(ctx context.Context) (bool, error) {
		current := &v1alpha1.Kubeseer{}
		if err := apiClient.Get(ctx, existingKey, current); err != nil {
			return false, err
		}
		if current.ResourceVersion != stableVersion {
			return false, errors.New("status-only event caused an unexpected runtime status write")
		}
		observations++
		return observations >= 3, nil
	}); err != nil {
		t.Fatalf("status-only predicate suppression: %v", err)
	}

	if err := apiClient.Delete(ctx, newObject); err != nil {
		t.Fatalf("delete post-start Kubeseer: %v", err)
	}
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		current := &v1alpha1.Kubeseer{}
		err := apiClient.Get(ctx, newKey, current)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}); err != nil {
		t.Fatalf("Kubeseer deletion handling: %v", err)
	}

	stopAndWaitManager()
	runRuntimeEnvtestAdapterScenarios(t, ctx, apiClient, clients, namespace, adapterKey, busyKey, freeKey, deterministicKey, emptyKey)

	t.Log("API_CONTRACT=authorization-enforcement STATUS=passed")
	t.Log("API_CONTRACT=reconciliation-runtime-lifecycle STATUS=passed")
	t.Log("API_CONTRACT=reconciliation-runtime-watch-routing STATUS=passed")
	t.Log("API_CONTRACT=reconciliation-runtime-status STATUS=passed")
	t.Log("API_CONTRACT=status-and-conditions-snapshots STATUS=passed")
	t.Log("API_CONTRACT=status-and-conditions-transitions STATUS=passed")
	t.Log("API_CONTRACT=value-operators-pipeline STATUS=passed")
	t.Log("API_CONTRACT=cross-namespace-aggregation-pipeline STATUS=passed")
	t.Log("API_CONTRACT=admission-validation-runtime STATUS=passed")
	t.Log("API_CONTRACT=observability-events STATUS=passed")
}

func runtimeEnvtestKubeseer(key types.NamespacedName, source v1alpha1.KubeseerSource) *v1alpha1.Kubeseer {
	return &v1alpha1.Kubeseer{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Spec:       v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{source}},
	}
}

func runtimeEnvtestPodSource(id string, explicitEmpty bool) v1alpha1.KubeseerSource {
	source := v1alpha1.KubeseerSource{ID: id, Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}
	if explicitEmpty {
		source.Namespaces = &v1alpha1.NamespaceSelection{Names: []string{}}
	}
	return source
}

func runtimeEnvtestPodFieldSource(id string) v1alpha1.KubeseerSource {
	source := runtimeEnvtestPodSource(id, false)
	source.Selector = &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"runtime-status": "yes"}}
	source.Fields = []v1alpha1.KubeseerField{{Name: "name", Path: "{.metadata.name}", Type: v1alpha1.ValueTypeString}}
	return source
}

func runtimeEnvtestInvalidSource(id string) v1alpha1.KubeseerSource {
	return v1alpha1.KubeseerSource{
		ID:       id,
		Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Missing"},
	}
}

func runtimeEnvtestInvalidSelectorSource(id string) v1alpha1.KubeseerSource {
	source := runtimeEnvtestPodSource(id, false)
	source.Selector = &v1alpha1.ResourceSelector{FieldSelector: "metadata.name in ("}
	return source
}

func runtimeEnvtestOperatorPod(name, namespace, selector, value string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"value-operator":  selector,
				"operator-result": value,
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "busybox"}}},
	}
}

func runtimeEnvtestValueOperatorSource(id, selector string, typeName v1alpha1.KubeseerValueType, operator v1alpha1.KubeseerOperatorName, operand *v1alpha1.KubeseerOperatorOperand) v1alpha1.KubeseerSource {
	source := runtimeEnvtestPodSource(id, false)
	source.Selector = &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"value-operator": selector}}
	field := v1alpha1.KubeseerField{Name: "value", Path: "{.metadata.labels['operator-result']}", Type: typeName}
	if operator != "" {
		field.Operators = []v1alpha1.KubeseerOperator{{Operator: operator, Value: operand}}
	}
	source.Fields = []v1alpha1.KubeseerField{field}
	return source
}

func runtimeEnvtestAggregationSource(id, firstNamespace, secondNamespace string) v1alpha1.KubeseerSource {
	return v1alpha1.KubeseerSource{
		ID:         id,
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{firstNamespace, secondNamespace}},
		Selector:   &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"runtime-aggregation": "yes"}},
		Fields: []v1alpha1.KubeseerField{
			{Name: "group", Path: "{.metadata.labels['aggregation-group']}", Type: v1alpha1.ValueTypeString},
			{Name: "value", Path: "{.metadata.labels['aggregation-value']}", Type: v1alpha1.ValueTypeNumber},
			{Name: "bad", Path: "{.metadata.labels['aggregation-bad']}", Type: v1alpha1.ValueTypeInteger},
		},
		Aggregations: []v1alpha1.KubeseerAggregation{
			{Name: "average-default", Function: v1alpha1.AggregationAverage, Field: "value", IncludeProvenance: true},
			{Name: "invalid-plan", Function: v1alpha1.AggregationSum, Field: "not-declared"},
			{Name: "sum-bad", Function: v1alpha1.AggregationSum, Field: "bad"},
		},
	}
}

func runtimeEnvtestAggregationSiblingSource(id, namespace string) v1alpha1.KubeseerSource {
	return v1alpha1.KubeseerSource{
		ID:         id,
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{namespace}},
		Selector:   &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"runtime-aggregation": "yes"}},
		Fields:     []v1alpha1.KubeseerField{{Name: "value", Path: "{.metadata.labels['aggregation-value']}", Type: v1alpha1.ValueTypeNumber}},
	}
}

func runtimeEnvtestAggregationPod(name, namespace, group, value, bad string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"runtime-aggregation": "yes",
				"aggregation-group":   group,
				"aggregation-value":   value,
				"aggregation-bad":     bad,
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "busybox"}}},
	}
}

func stringOperatorOperand(value string) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, StringValue: &value}
}

func integerOperatorOperand(value int64) *v1alpha1.KubeseerOperatorOperand {
	return &v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, IntegerValue: &value}
}

func waitRuntimeValueOperatorState(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName, sourceID string, want v1alpha1.KubeseerSourceState, resourceCount int, value string) {
	t.Helper()
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		object := &v1alpha1.Kubeseer{}
		if err := client.Get(ctx, key, object); err != nil {
			return false, err
		}
		if object.Status.Result == nil {
			return false, nil
		}
		for _, source := range object.Status.Result.Sources {
			if source.ID != sourceID {
				continue
			}
			if source.State != want || want == v1alpha1.SourceStateValues && source.Error != nil || want == v1alpha1.SourceStateError && source.Error == nil {
				return false, nil
			}
			if resourceCount >= 0 && len(source.Resources) != resourceCount {
				return false, nil
			}
			if value != "" && !runtimeSourceHasFieldStringValue(source, "value", value) {
				return false, nil
			}
			return true, nil
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait %s/%s value-operator source %q: %v", key.Namespace, key.Name, sourceID, err)
	}
}

func waitRuntimeAggregationState(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName, sourceID string, want v1alpha1.KubeseerSourceState, resourceCount int) {
	t.Helper()
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		object := &v1alpha1.Kubeseer{}
		if err := client.Get(ctx, key, object); err != nil {
			return false, err
		}
		if object.Status.Result == nil {
			return false, nil
		}
		for _, source := range object.Status.Result.Sources {
			if source.ID != sourceID {
				continue
			}
			return source.State == want && len(source.Resources) == resourceCount && len(source.Aggregates) == 3, nil
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait %s/%s aggregation source %q: %v", key.Namespace, key.Name, sourceID, err)
	}
}

func waitRuntimeAggregationDenied(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName, sourceID string) {
	t.Helper()
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		object := &v1alpha1.Kubeseer{}
		if err := client.Get(ctx, key, object); err != nil {
			return false, err
		}
		if object.Status.Result == nil {
			return false, nil
		}
		for _, source := range object.Status.Result.Sources {
			if source.ID != sourceID {
				continue
			}
			return source.State == v1alpha1.SourceStateError && source.Error != nil && len(source.Resources) == 0 && len(source.Aggregates) == 0, nil
		}
		return false, nil
	}); err != nil {
		t.Fatalf("wait %s/%s denied aggregation source %q: %v", key.Namespace, key.Name, sourceID, err)
	}
}

func runtimeEnvtestStatusPod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"runtime-status": "yes"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "busybox"}}},
	}
}

func runtimeEnvtestPolicy(namespace string) *v1alpha1.KubeseerAccessPolicy {
	return &v1alpha1.KubeseerAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.InstallationAccessCeilingName},
		Spec: v1alpha1.KubeseerAccessPolicySpec{
			Namespaces: v1alpha1.NamespacePolicy{Mode: v1alpha1.NamespaceModeExplicit, Include: []string{namespace}},
			Resources: []v1alpha1.ResourceRule{
				{APIGroups: []string{""}, Kinds: []string{"Pod"}},
				{APIGroups: []string{"runtime.kubeseer.io"}, Kinds: []string{"Observation"}},
			},
		},
	}
}

func runtimeObservedResourceCRD() (*apiextensionsv1.CustomResourceDefinition, schema.GroupVersionResource) {
	const group = "runtime.kubeseer.io"
	const version = "v1"
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "observations." + group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     "observations",
				Singular:   "observation",
				Kind:       "Observation",
				ListKind:   "ObservationList",
				ShortNames: []string{"obs"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    version,
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"value": {Type: "string"},
							},
						},
					},
				}},
			}},
		},
	}, schema.GroupVersionResource{Group: group, Version: version, Resource: "observations"}
}

func runtimeEnvtestObservation(name, namespace, watchLabel, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "runtime.kubeseer.io/v1",
		"kind":       "Observation",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"labels":    map[string]interface{}{"watch": watchLabel},
		},
		"spec": map[string]interface{}{"value": value},
	}}
}

func runtimeEnvtestObservedSource(id string) v1alpha1.KubeseerSource {
	return v1alpha1.KubeseerSource{
		ID:       id,
		Resource: v1alpha1.ResourceReference{APIVersion: "runtime.kubeseer.io/v1", Kind: "Observation"},
		Selector: &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"watch": "yes"}},
		Fields: []v1alpha1.KubeseerField{{
			Name: "value",
			Path: "{.spec.value}",
			Type: v1alpha1.ValueTypeString,
		}},
	}
}

type runtimeObservedRequest struct {
	Method           string
	Path             string
	Watch            bool
	HasLabelSelector bool
	HasFieldSelector bool
	ResourceVersion  string
}

type runtimeObservedRequestRecorder struct {
	mu                sync.Mutex
	gvr               schema.GroupVersionResource
	namespace         string
	requests          []runtimeObservedRequest
	active            []*runtimeObservedWatchBody
	statusWrites      map[string]int
	policyUnavailable bool
}

func newRuntimeObservedRequestRecorder(gvr schema.GroupVersionResource, namespace string) *runtimeObservedRequestRecorder {
	return &runtimeObservedRequestRecorder{gvr: gvr, namespace: namespace, statusWrites: make(map[string]int)}
}

func (r *runtimeObservedRequestRecorder) observedPath() string {
	if r == nil {
		return ""
	}
	return "/apis/" + r.gvr.Group + "/" + r.gvr.Version + "/namespaces/" + r.namespace + "/" + r.gvr.Resource
}

func (r *runtimeObservedRequestRecorder) policyPath() string {
	return "/apis/kubeseer.io/v1alpha1/kubeseeraccesspolicies/" + v1alpha1.InstallationAccessCeilingName
}

func (r *runtimeObservedRequestRecorder) record(request *http.Request) {
	if r == nil || request == nil || request.URL == nil || request.URL.Path != r.observedPath() {
		return
	}
	query := request.URL.Query()
	entry := runtimeObservedRequest{
		Method:           request.Method,
		Path:             request.URL.Path,
		Watch:            query.Get("watch") == "true",
		HasLabelSelector: query.Get("labelSelector") != "",
		HasFieldSelector: query.Get("fieldSelector") != "",
		ResourceVersion:  query.Get("resourceVersion"),
	}
	r.mu.Lock()
	r.requests = append(r.requests, entry)
	r.mu.Unlock()
}

func (r *runtimeObservedRequestRecorder) addBody(body *runtimeObservedWatchBody) {
	if r == nil || body == nil {
		return
	}
	body.mu.Lock()
	defer body.mu.Unlock()
	if body.closed {
		return
	}
	r.mu.Lock()
	r.active = append(r.active, body)
	r.mu.Unlock()
}

func (r *runtimeObservedRequestRecorder) removeBody(body *runtimeObservedWatchBody) {
	if r == nil || body == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, active := range r.active {
		if active == body {
			r.active = append(r.active[:index], r.active[index+1:]...)
			return
		}
	}
}

func (r *runtimeObservedRequestRecorder) Requests() []runtimeObservedRequest {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runtimeObservedRequest(nil), r.requests...)
}

func (r *runtimeObservedRequestRecorder) recordStatusWrite(request *http.Request) {
	if r == nil || request == nil || request.URL == nil || request.Method != http.MethodPut {
		return
	}
	prefix := "/apis/kubeseer.io/v1alpha1/namespaces/" + r.namespace + "/kubeseers/"
	if !strings.HasPrefix(request.URL.Path, prefix) || !strings.HasSuffix(request.URL.Path, "/status") {
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, prefix), "/status")
	if name == "" || strings.Contains(name, "/") {
		return
	}
	r.mu.Lock()
	if r.statusWrites == nil {
		r.statusWrites = make(map[string]int)
	}
	r.statusWrites[name]++
	r.mu.Unlock()
}

func (r *runtimeObservedRequestRecorder) StatusWrites(name string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusWrites[name]
}

func (r *runtimeObservedRequestRecorder) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.requests = nil
	r.mu.Unlock()
}

func (r *runtimeObservedRequestRecorder) ActiveWatches() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}

func (r *runtimeObservedRequestRecorder) WatchRequestCount() int {
	count := 0
	for _, request := range r.Requests() {
		if request.Watch {
			count++
		}
	}
	return count
}

func (r *runtimeObservedRequestRecorder) SetPolicyUnavailable(unavailable bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.policyUnavailable = unavailable
	r.mu.Unlock()
}

func (r *runtimeObservedRequestRecorder) isPolicyUnavailable() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.policyUnavailable
}

func (r *runtimeObservedRequestRecorder) CloseOneWatch() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	active := append([]*runtimeObservedWatchBody(nil), r.active...)
	r.mu.Unlock()
	for index := len(active) - 1; index >= 0; index-- {
		if closed, _ := active[index].closeOnce(); closed {
			return true
		}
	}
	return false
}

type runtimeObservedRecordingTransport struct {
	base     http.RoundTripper
	recorder *runtimeObservedRequestRecorder
}

func (t *runtimeObservedRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t == nil || t.base == nil {
		return nil, errors.New("recording transport has no base transport")
	}
	if t.recorder != nil && request != nil && request.URL != nil {
		t.recorder.recordStatusWrite(request)
		if request.Method == http.MethodGet && request.URL.Path == t.recorder.policyPath() && t.recorder.isPolicyUnavailable() {
			return nil, errors.New("simulated policy API unavailability")
		}
		t.recorder.record(request)
	}
	response, err := t.base.RoundTrip(request)
	if err == nil && response != nil && response.Body != nil && t.recorder != nil && request != nil && request.URL != nil && request.URL.Path == t.recorder.observedPath() && request.URL.Query().Get("watch") == "true" {
		body := &runtimeObservedWatchBody{ReadCloser: response.Body, recorder: t.recorder}
		response.Body = body
		t.recorder.addBody(body)
	}
	return response, err
}

type runtimeObservedWatchBody struct {
	io.ReadCloser
	recorder *runtimeObservedRequestRecorder
	mu       sync.Mutex
	closed   bool
}

func (b *runtimeObservedWatchBody) isClosed() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *runtimeObservedWatchBody) closeOnce() (bool, error) {
	if b == nil {
		return false, nil
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return false, nil
	}
	b.closed = true
	reader := b.ReadCloser
	recorder := b.recorder
	b.mu.Unlock()
	if recorder != nil {
		recorder.removeBody(b)
	}
	if reader == nil {
		return true, nil
	}
	return true, reader.Close()
}

func (b *runtimeObservedWatchBody) Close() error {
	_, err := b.closeOnce()
	return err
}

func runtimeObservedRequestIndexes(requests []runtimeObservedRequest) (firstWatch, firstList, watchCount int) {
	firstWatch, firstList = -1, -1
	for index, request := range requests {
		if request.Watch {
			watchCount++
			if firstWatch == -1 {
				firstWatch = index
			}
			continue
		}
		if firstList == -1 {
			firstList = index
		}
	}
	return firstWatch, firstList, watchCount
}

func assertRuntimeObservedRequestsAbsent(t *testing.T, ctx context.Context, recorder *runtimeObservedRequestRecorder, duration time.Duration) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatalf("context ended while checking denied observed-resource requests: %v", ctx.Err())
	case <-timer.C:
	}
	if requests := recorder.Requests(); len(requests) != 0 {
		t.Fatalf("denied observed-resource target issued requests: %+v", requests)
	}
}

func assertRuntimeStatusWritesStable(t *testing.T, ctx context.Context, recorder *runtimeObservedRequestRecorder, name string, want int, duration time.Duration) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatalf("context ended while checking unchanged status writes: %v", ctx.Err())
	case <-timer.C:
	}
	if got := recorder.StatusWrites(name); got != want {
		t.Fatalf("unchanged source event status writes=%d, want %d", got, want)
	}
}

func waitRuntimeSourceState(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName, want v1alpha1.KubeseerSourceState) {
	t.Helper()
	waitRuntimeSourceStates(t, ctx, client, key, 1, want, -1, "")
}

func waitRuntimeObservedState(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName, sourceCount int, want v1alpha1.KubeseerSourceState, resourceCount int, value string) {
	t.Helper()
	waitRuntimeSourceStates(t, ctx, client, key, sourceCount, want, resourceCount, value)
}

func waitRuntimeMixedStatus(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName, generation int64, resourceCount int) {
	t.Helper()
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		object := &v1alpha1.Kubeseer{}
		if err := client.Get(ctx, key, object); err != nil {
			return false, err
		}
		if object.Status.Result == nil || object.Status.ObservedGeneration != generation || len(object.Status.Result.Sources) != 2 {
			return false, nil
		}
		success, failed := object.Status.Result.Sources[0], object.Status.Result.Sources[1]
		if success.ID != "status-success" || success.State != v1alpha1.SourceStateValues || failed.ID != "status-invalid" || failed.State != v1alpha1.SourceStateError || failed.Error == nil {
			return false, nil
		}
		if len(success.Resources) != resourceCount {
			return false, nil
		}
		if resourceCount > 0 && !runtimeSourceHasFieldStringValue(success, "name", "runtime-status-pod") {
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatalf("wait %s/%s mixed ordered status generation %d resources %d: %v", key.Namespace, key.Name, generation, resourceCount, err)
	}
}

func waitRuntimeSourceStates(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName, sourceCount int, want v1alpha1.KubeseerSourceState, resourceCount int, value string) {
	t.Helper()
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		object := &v1alpha1.Kubeseer{}
		if err := client.Get(ctx, key, object); err != nil {
			return false, err
		}
		if object.Status.Result == nil || len(object.Status.Result.Sources) != sourceCount {
			return false, nil
		}
		for _, source := range object.Status.Result.Sources {
			if source.State != want || want == v1alpha1.SourceStateValues && source.Error != nil || want == v1alpha1.SourceStateError && source.Error == nil {
				return false, nil
			}
			if resourceCount >= 0 && len(source.Resources) != resourceCount {
				return false, nil
			}
			if value != "" && !runtimeSourceHasStringValue(source, value) {
				return false, nil
			}
		}
		return true, nil
	}); err != nil {
		t.Fatalf("wait %s/%s source state %q: %v", key.Namespace, key.Name, want, err)
	}
}

func runtimeSourceHasStringValue(source v1alpha1.KubeseerSourceResult, want string) bool {
	return runtimeSourceHasFieldStringValue(source, "value", want)
}

func runtimeSourceHasFieldStringValue(source v1alpha1.KubeseerSourceResult, fieldName, want string) bool {
	for _, resource := range source.Resources {
		for _, field := range resource.Fields {
			if field.Name != fieldName {
				continue
			}
			for _, match := range field.Matches {
				if match.StringValue != nil && *match.StringValue == want {
					return true
				}
			}
		}
	}
	return false
}

func waitRuntimeEmptyResult(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName) {
	t.Helper()
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		object := &v1alpha1.Kubeseer{}
		if err := client.Get(ctx, key, object); err != nil {
			return false, err
		}
		return object.Status.Result != nil && len(object.Status.Result.Sources) == 0, nil
	}); err != nil {
		t.Fatalf("wait %s/%s empty result: %v", key.Namespace, key.Name, err)
	}
}

func waitRuntimeObservedGeneration(t *testing.T, ctx context.Context, client crclient.Client, key types.NamespacedName, want int64) {
	t.Helper()
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		object := &v1alpha1.Kubeseer{}
		if err := client.Get(ctx, key, object); err != nil {
			return false, err
		}
		return object.Status.Result != nil && object.Status.ObservedGeneration == want && len(object.Status.Result.Sources) == 1, nil
	}); err != nil {
		t.Fatalf("wait %s/%s observed generation %d: %v", key.Namespace, key.Name, want, err)
	}
}

func waitRuntimeKubeseerEvents(t *testing.T, ctx context.Context, clients Clients, key types.NamespacedName, want int) []corev1.Event {
	t.Helper()
	if err := WaitFor(ctx, 10*time.Second, func(ctx context.Context) (bool, error) {
		events, err := listRuntimeKubeseerEvents(ctx, clients, key)
		return len(events) >= want, err
	}); err != nil {
		t.Fatalf("wait %d Events for %s/%s: %v", want, key.Namespace, key.Name, err)
	}
	events, err := listRuntimeKubeseerEvents(ctx, clients, key)
	if err != nil {
		t.Fatalf("list Events for %s/%s: %v", key.Namespace, key.Name, err)
	}
	return events
}

func listRuntimeKubeseerEvents(ctx context.Context, clients Clients, key types.NamespacedName) ([]corev1.Event, error) {
	list, err := clients.Core.CoreV1().Events(key.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	filtered := make([]corev1.Event, 0, len(list.Items))
	for _, event := range list.Items {
		if event.InvolvedObject.Name == key.Name {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

func runRuntimeEnvtestAdapterScenarios(t *testing.T, ctx context.Context, apiClient crclient.Client, clients Clients, namespace string, adapterKey, busyKey, freeKey, deterministicKey, emptyKey types.NamespacedName) {
	t.Helper()
	store := reconciliation.NewClientKubeseerStore(apiClient)
	discoveryClient := &runtimeEnvtestDiscoveryAdapter{delegate: clients.Discovery}
	resolver := discoveryruntime.NewResolver(discoveryClient)
	verifier := authorization.VerifierFunc(func(subject authorization.Subject) bool {
		return subject.Validate() == nil
	})
	resourceLister := &runtimeEnvtestResourceListerAdapter{delegate: selection.NewDynamicResourceLister(clients.Dynamic, verifier)}
	tracker := reconciliation.NewFreshnessTracker()
	routes := &runtimeEnvtestRouteManager{}
	statusWriter := &runtimeEnvtestCountingStatusWriter{delegate: reconciliation.NewClientStatusWriter(apiClient.Status())}
	runtimeInstance, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour}, reconciliation.Dependencies{
		Reader:       store,
		Lister:       store,
		PolicySource: accesspolicy.NewClientPolicySource(apiClient),
		Enforcer:     authorization.NewEnforcer(nil),
		Planner:      selection.NewPlanner(resolver),
		Executor:     selection.NewExecutor(resourceLister, selection.WithVerifier(verifier)),
		Routes:       routes,
		Publisher:    reconciliation.NewStatusPublisher(store, statusWriter, tracker),
		Tracker:      tracker,
	})
	if err != nil {
		t.Fatalf("construct real-client adapter runtime: %v", err)
	}

	adapterObject := &v1alpha1.Kubeseer{
		ObjectMeta: metav1.ObjectMeta{Namespace: adapterKey.Namespace, Name: adapterKey.Name},
		Spec:       v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{runtimeEnvtestPodFieldSource("adapter-source")}},
	}
	if err := apiClient.Create(ctx, adapterObject); err != nil {
		t.Fatalf("create adapter runtime Kubeseer: %v", err)
	}
	request := reconcile.Request{NamespacedName: adapterKey}
	discoveryClient.FailNext()
	rateQueue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer rateQueue.ShutDown()
	rateQueue.Add(request)
	item, shutdown := rateQueue.Get()
	if shutdown {
		t.Fatalf("rate-limited queue shut down before discovery failure")
	}
	_, firstErr := runtimeInstance.Reconcile(ctx, item)
	rateQueue.Done(item)
	if !reconciliation.IsRetryable(firstErr) {
		t.Fatalf("discovery availability error was not retryable: %v", firstErr)
	}
	waitRuntimeSourceState(t, ctx, apiClient, adapterKey, v1alpha1.SourceStateError)
	rateQueue.AddRateLimited(item)
	retryItem, retryShutdown := runtimeEnvtestQueueGet(t, ctx, rateQueue)
	if retryShutdown {
		t.Fatalf("rate-limited queue shut down before recovery")
	}
	_, retryErr := runtimeInstance.Reconcile(ctx, retryItem)
	rateQueue.Done(retryItem)
	rateQueue.Forget(retryItem)
	if retryErr != nil {
		t.Fatalf("discovery availability retry did not converge: %v", retryErr)
	}
	waitRuntimeSourceStates(t, ctx, apiClient, adapterKey, 1, v1alpha1.SourceStateValues, 1, "")

	listWritesBefore := statusWriter.Calls()
	resourceLister.FailNext()
	_, listErr := runtimeInstance.Reconcile(ctx, request)
	if !reconciliation.IsRetryable(listErr) {
		t.Fatalf("LIST availability error was not retryable: %v", listErr)
	}
	waitRuntimeSourceState(t, ctx, apiClient, adapterKey, v1alpha1.SourceStateError)
	if statusWriter.Calls() <= listWritesBefore {
		t.Fatalf("LIST availability failure did not publish its sanitized partial status")
	}
	if _, err := runtimeInstance.Reconcile(ctx, request); err != nil {
		t.Fatalf("LIST availability retry did not converge: %v", err)
	}
	waitRuntimeSourceStates(t, ctx, apiClient, adapterKey, 1, v1alpha1.SourceStateValues, 1, "")

	deterministicObject := &v1alpha1.Kubeseer{
		ObjectMeta: metav1.ObjectMeta{Namespace: deterministicKey.Namespace, Name: deterministicKey.Name},
		Spec:       v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{runtimeEnvtestInvalidSource("deterministic-invalid")}},
	}
	if err := apiClient.Create(ctx, deterministicObject); err != nil {
		t.Fatalf("create deterministic-failure Kubeseer: %v", err)
	}
	if _, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: deterministicKey}); err != nil {
		t.Fatalf("deterministic source failure unexpectedly requested retry: %v", err)
	}
	waitRuntimeSourceState(t, ctx, apiClient, deterministicKey, v1alpha1.SourceStateError)
	deterministicWrites := statusWriter.Calls()
	if _, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: deterministicKey}); err != nil {
		t.Fatalf("repeat deterministic source failure unexpectedly requested retry: %v", err)
	}
	if statusWriter.Calls() != deterministicWrites {
		t.Fatalf("deterministic failure hot-loop wrote status again: before=%d after=%d", deterministicWrites, statusWriter.Calls())
	}

	invalidPolicy := runtimeEnvtestPolicy(namespace)
	invalidPolicy.Spec.Namespaces.Mode = v1alpha1.NamespaceMode("invalid")
	invalidTracker := reconciliation.NewFreshnessTracker()
	invalidPolicyRuntime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour}, reconciliation.Dependencies{
		Reader:       store,
		Lister:       store,
		PolicySource: accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) { return invalidPolicy.DeepCopy(), nil }),
		Enforcer:     authorization.NewEnforcer(nil),
		Planner:      selection.NewPlanner(resolver),
		Executor:     selection.NewExecutor(resourceLister, selection.WithVerifier(verifier)),
		Routes:       routes,
		Publisher:    reconciliation.NewStatusPublisher(store, statusWriter, invalidTracker),
		Tracker:      invalidTracker,
	})
	if err != nil {
		t.Fatalf("construct invalid-policy runtime: %v", err)
	}
	if _, err := invalidPolicyRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: deterministicKey}); err != nil {
		t.Fatalf("invalid-policy reconciliation: %v", err)
	}
	invalidPolicyStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, deterministicKey, invalidPolicyStatus); err != nil {
		t.Fatalf("read invalid-policy status: %v", err)
	}
	assertRuntimeStatusSnapshot(t, invalidPolicyStatus, invalidPolicyStatus.Generation, true)
	assertRuntimeCondition(t, invalidPolicyStatus.Status, statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonPolicyInvalid)

	denyingPolicy := runtimeEnvtestPolicy(namespace)
	denyingPolicy.Spec.Namespaces.Include = nil
	denialTracker := reconciliation.NewFreshnessTracker()
	denialRuntime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour}, reconciliation.Dependencies{
		Reader:       store,
		Lister:       store,
		PolicySource: accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) { return denyingPolicy.DeepCopy(), nil }),
		Enforcer:     authorization.NewEnforcer(nil),
		Planner:      selection.NewPlanner(resolver),
		Executor:     selection.NewExecutor(resourceLister, selection.WithVerifier(verifier)),
		Routes:       routes,
		Publisher:    reconciliation.NewStatusPublisher(store, statusWriter, denialTracker),
		Tracker:      denialTracker,
	})
	if err != nil {
		t.Fatalf("construct denying-policy runtime: %v", err)
	}
	if _, err := denialRuntime.Reconcile(ctx, reconcile.Request{NamespacedName: adapterKey}); err != nil {
		t.Fatalf("denying-policy reconciliation: %v", err)
	}
	deniedPolicyStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, adapterKey, deniedPolicyStatus); err != nil {
		t.Fatalf("read denied-policy status: %v", err)
	}
	assertRuntimeStatusSnapshot(t, deniedPolicyStatus, deniedPolicyStatus.Generation, true)
	assertRuntimeCondition(t, deniedPolicyStatus.Status, statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonAuthorizationDenied)

	resourceLister.ForbidNext()
	if _, err := runtimeInstance.Reconcile(ctx, request); err != nil {
		t.Fatalf("forbidden-read reconciliation: %v", err)
	}
	waitRuntimeSourceState(t, ctx, apiClient, adapterKey, v1alpha1.SourceStateError)
	forbiddenStatus := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, adapterKey, forbiddenStatus); err != nil {
		t.Fatalf("read forbidden status: %v", err)
	}
	assertRuntimeStatusSnapshot(t, forbiddenStatus, forbiddenStatus.Generation, true)
	assertRuntimeCondition(t, forbiddenStatus.Status, statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonReadForbidden)

	busyObject := &v1alpha1.Kubeseer{
		ObjectMeta: metav1.ObjectMeta{Namespace: busyKey.Namespace, Name: busyKey.Name},
		Spec:       v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{runtimeEnvtestPodFieldSource("busy-source")}},
	}
	freeObject := &v1alpha1.Kubeseer{ObjectMeta: metav1.ObjectMeta{Namespace: freeKey.Namespace, Name: freeKey.Name}}
	if err := apiClient.Create(ctx, busyObject); err != nil {
		t.Fatalf("create busy Kubeseer: %v", err)
	}
	if err := apiClient.Create(ctx, freeObject); err != nil {
		t.Fatalf("create independent free Kubeseer: %v", err)
	}
	blockRelease := make(chan struct{})
	blockStarted := make(chan struct{})
	resourceLister.BlockNext(blockRelease, blockStarted)
	busyContext, cancelBusy := context.WithCancel(ctx)
	defer cancelBusy()
	busyDone := make(chan error, 1)
	go func() {
		_, err := runtimeInstance.Reconcile(busyContext, reconcile.Request{NamespacedName: busyKey})
		busyDone <- err
	}()
	select {
	case <-blockStarted:
	case <-ctx.Done():
		t.Fatalf("busy reconciliation did not reach the real LIST adapter: %v", ctx.Err())
	}
	freeDone := make(chan error, 1)
	go func() {
		_, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: freeKey})
		freeDone <- err
	}()
	select {
	case err := <-freeDone:
		if err != nil {
			t.Fatalf("independent free key was blocked or failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("independent free key remained blocked by busy key")
	}
	waitRuntimeEmptyResult(t, ctx, apiClient, freeKey)
	busyWrites := statusWriter.Calls()
	cancelBusy()
	select {
	case err := <-busyDone:
		if err != nil {
			t.Fatalf("canceled busy reconciliation returned an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("canceled busy reconciliation did not finish")
	}
	close(blockRelease)
	if statusWriter.Calls() != busyWrites {
		t.Fatalf("canceled reconciliation published status: before=%d after=%d", busyWrites, statusWriter.Calls())
	}

	adapterCurrent := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, adapterKey, adapterCurrent); err != nil {
		t.Fatalf("read adapter Kubeseer before conflict proof: %v", err)
	}
	conflictTracker := reconciliation.NewFreshnessTracker()
	conflictTracker.Observe(adapterCurrent)
	conflictLease, conflictContext, releaseConflict, err := conflictTracker.Acquire(ctx, adapterKey, adapterCurrent.UID, adapterCurrent.Generation)
	if err != nil {
		t.Fatalf("acquire conflict proof lease: %v", err)
	}
	conflictResult := v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{{ID: "conflict-result", State: v1alpha1.SourceStateValues}}}
	conflictWriter := &runtimeEnvtestConflictStatusWriter{
		delegate: reconciliation.NewClientStatusWriter(apiClient.Status()),
		client:   apiClient,
		key:      adapterKey,
	}
	conflictPublisher := reconciliation.NewStatusPublisher(store, conflictWriter, conflictTracker)
	conflictErr := conflictPublisher.Publish(conflictContext, conflictLease, envtestStatusEvaluation(conflictResult))
	releaseConflict()
	if !reconciliation.IsRetryable(conflictErr) || conflictWriter.Calls() != 1 {
		t.Fatalf("real status conflict = err=%v calls=%d, want one retryable attempt", conflictErr, conflictWriter.Calls())
	}
	conflicted := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, adapterKey, conflicted); err != nil {
		t.Fatalf("read adapter Kubeseer after conflict: %v", err)
	}
	if len(conflicted.Status.Conditions) != 1 || conflicted.Status.Conditions[0].Type != "ConflictFixture" {
		t.Fatalf("concurrent real API status write was overwritten: %#v", conflicted.Status.Conditions)
	}
	if conflicted.Status.Result != nil && len(conflicted.Status.Result.Sources) == 1 && conflicted.Status.Result.Sources[0].ID == "conflict-result" {
		t.Fatalf("conflicting status candidate overwrote the newer API state")
	}
	conflictTracker.Observe(conflicted)
	convergeLease, convergeContext, releaseConverge, err := conflictTracker.Acquire(ctx, adapterKey, conflicted.UID, conflicted.Generation)
	if err != nil {
		t.Fatalf("acquire convergence lease after conflict: %v", err)
	}
	if err := reconciliation.NewStatusPublisher(store, reconciliation.NewClientStatusWriter(apiClient.Status()), conflictTracker).Publish(convergeContext, convergeLease, envtestStatusEvaluation(conflictResult)); err != nil {
		releaseConverge()
		t.Fatalf("status did not converge after conflict retry: %v", err)
	}
	releaseConverge()
	converged := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, adapterKey, converged); err != nil {
		t.Fatalf("read converged adapter status: %v", err)
	}
	if converged.Status.Result == nil || len(converged.Status.Result.Sources) != 1 || converged.Status.Result.Sources[0].ID != "conflict-result" {
		t.Fatalf("status conflict did not converge to the fresh candidate: %#v", converged.Status.Result)
	}

	emptyCurrent := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, emptyKey, emptyCurrent); err != nil {
		t.Fatalf("read zero-source Kubeseer for nil/empty suppression: %v", err)
	}
	emptyTracker := reconciliation.NewFreshnessTracker()
	emptyTracker.Observe(emptyCurrent)
	emptyLease, emptyContext, releaseEmpty, err := emptyTracker.Acquire(ctx, emptyKey, emptyCurrent.UID, emptyCurrent.Generation)
	if err != nil {
		t.Fatalf("acquire zero-source semantic lease: %v", err)
	}
	emptyWriter := &runtimeEnvtestCountingStatusWriter{delegate: reconciliation.NewClientStatusWriter(apiClient.Status())}
	if err := reconciliation.NewStatusPublisher(store, emptyWriter, emptyTracker).Publish(emptyContext, emptyLease, envtestStatusEvaluation(v1alpha1.KubeseerResult{})); err != nil {
		releaseEmpty()
		t.Fatalf("nil/empty semantic status suppression failed: %v", err)
	}
	releaseEmpty()
	if emptyWriter.Calls() != 0 {
		t.Fatalf("nil/empty semantic equivalent caused %d status writes", emptyWriter.Calls())
	}

	staleTracker := reconciliation.NewFreshnessTracker()
	staleTracker.Observe(converged)
	staleLease, staleContext, releaseStale, err := staleTracker.Acquire(ctx, adapterKey, converged.UID, converged.Generation)
	if err != nil {
		t.Fatalf("acquire stale-lease proof: %v", err)
	}
	newer := converged.DeepCopy()
	newer.Generation++
	staleTracker.Observe(newer)
	staleWriter := &runtimeEnvtestCountingStatusWriter{delegate: reconciliation.NewClientStatusWriter(apiClient.Status())}
	if err := reconciliation.NewStatusPublisher(store, staleWriter, staleTracker).Publish(staleContext, staleLease, envtestStatusEvaluation(conflictResult)); err == nil {
		releaseStale()
		t.Fatalf("stale lease unexpectedly published status")
	}
	releaseStale()
	if staleWriter.Calls() != 0 {
		t.Fatalf("stale lease attempted %d status writes", staleWriter.Calls())
	}

	canceledTracker := reconciliation.NewFreshnessTracker()
	canceledTracker.Observe(converged)
	canceledParent, cancelCanceled := context.WithCancel(ctx)
	canceledLease, canceledContext, releaseCanceled, err := canceledTracker.Acquire(canceledParent, adapterKey, converged.UID, converged.Generation)
	cancelCanceled()
	if err != nil {
		t.Fatalf("acquire canceled proof lease: %v", err)
	}
	canceledWriter := &runtimeEnvtestCountingStatusWriter{delegate: reconciliation.NewClientStatusWriter(apiClient.Status())}
	if err := reconciliation.NewStatusPublisher(store, canceledWriter, canceledTracker).Publish(canceledContext, canceledLease, envtestStatusEvaluation(conflictResult)); err == nil {
		releaseCanceled()
		t.Fatalf("canceled status publication unexpectedly succeeded")
	}
	releaseCanceled()
	if canceledWriter.Calls() != 0 {
		t.Fatalf("canceled publication attempted %d status writes", canceledWriter.Calls())
	}

	runRuntimeEnvtestAuthorizationPaginationScenario(t, ctx, apiClient, clients, resolver, namespace, adapterKey)
	runAdmissionValidationRuntimeEnvtestScenario(t, ctx, apiClient, clients, resolver, discoveryClient, namespace, runtimeInstance, resourceLister, tracker)
}

func runAdmissionValidationRuntimeEnvtestScenario(t *testing.T, ctx context.Context, apiClient crclient.Client, clients Clients, resolver *discoveryruntime.Resolver, discoveryClient *runtimeEnvtestDiscoveryAdapter, namespace string, runtimeInstance *reconciliation.Runtime, resourceLister *runtimeEnvtestResourceListerAdapter, tracker *reconciliation.FreshnessTracker) {
	t.Helper()
	legacyKey := types.NamespacedName{Namespace: namespace, Name: "runtime-admission-legacy"}
	admittedKey := types.NamespacedName{Namespace: namespace, Name: "runtime-admission-admitted"}
	source := runtimeEnvtestObservedSource("runtime-admission-source")
	source.Selector = &v1alpha1.ResourceSelector{MatchLabels: map[string]string{"admission-runtime": "yes"}}
	observedResources := clients.Dynamic.Resource(schema.GroupVersionResource{Group: "runtime.kubeseer.io", Version: "v1", Resource: "observations"}).Namespace(namespace)
	fixture := runtimeEnvtestObservation("runtime-admission-observation", namespace, "yes", "runtime-admission-value")
	fixture.SetLabels(map[string]string{"admission-runtime": "yes"})
	if _, err := observedResources.Create(ctx, fixture, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create admission-runtime observed fixture: %v", err)
	}
	defer func() {
		for _, key := range []types.NamespacedName{legacyKey, admittedKey} {
			object := &v1alpha1.Kubeseer{}
			if err := apiClient.Get(ctx, key, object); err == nil {
				if err := apiClient.Delete(ctx, object); err != nil && !apierrors.IsNotFound(err) {
					t.Errorf("delete admission-runtime Kubeseer %s/%s: %v", key.Namespace, key.Name, err)
				}
			}
		}
		if err := observedResources.Delete(ctx, fixture.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete admission-runtime observed fixture: %v", err)
		}
	}()

	legacy := runtimeEnvtestKubeseer(legacyKey, source)
	if err := apiClient.Create(ctx, legacy); err != nil {
		t.Fatalf("create pre-webhook legacy Kubeseer: %v", err)
	}
	admitted := runtimeEnvtestKubeseer(admittedKey, source)
	validator := admission.NewValidator(discoveryruntime.NewResolver(discoveryClient), accesspolicy.NewClientPolicySource(apiClient))
	if result := validator.ValidateKubeseer(ctx, admitted); !result.Valid() {
		t.Fatalf("admitted runtime fixture was rejected: %#v", result.IssuesCopy())
	}
	if err := apiClient.Create(ctx, admitted); err != nil {
		t.Fatalf("create previously admitted Kubeseer: %v", err)
	}

	resolver.InvalidateAll()
	for _, key := range []types.NamespacedName{legacyKey, admittedKey} {
		if _, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("initial runtime revalidation for %s/%s: %v", key.Namespace, key.Name, err)
		}
		waitRuntimeSourceStates(t, ctx, apiClient, key, 1, v1alpha1.SourceStateValues, 1, "")
	}

	listCallsBeforeDiscoveryDrift := len(resourceLister.Calls())
	for _, key := range []types.NamespacedName{legacyKey, admittedKey} {
		discoveryClient.FailNext()
		resolver.InvalidateAll()
		_, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		if !reconciliation.IsRetryable(err) {
			t.Fatalf("runtime discovery drift for %s/%s = %v, want retryable error", key.Namespace, key.Name, err)
		}
		waitRuntimeSourceStates(t, ctx, apiClient, key, 1, v1alpha1.SourceStateError, 0, "")
	}
	if len(resourceLister.Calls()) != listCallsBeforeDiscoveryDrift {
		t.Fatalf("discovery drift issued resource LIST: before=%d after=%d", listCallsBeforeDiscoveryDrift, len(resourceLister.Calls()))
	}

	resolver.InvalidateAll()
	for _, key := range []types.NamespacedName{legacyKey, admittedKey} {
		if _, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("runtime discovery recovery for %s/%s: %v", key.Namespace, key.Name, err)
		}
		waitRuntimeSourceStates(t, ctx, apiClient, key, 1, v1alpha1.SourceStateValues, 1, "")
	}

	policy := &v1alpha1.KubeseerAccessPolicy{}
	if err := apiClient.Get(ctx, types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}, policy); err != nil {
		t.Fatalf("read policy before runtime narrowing: %v", err)
	}
	policy.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{""}, Kinds: []string{"Node"}}}
	if err := apiClient.Update(ctx, policy); err != nil {
		t.Fatalf("narrow runtime policy after admission: %v", err)
	}
	tracker.InvalidateAll()
	listCallsBeforeNarrowing := len(resourceLister.Calls())
	for _, key := range []types.NamespacedName{legacyKey, admittedKey} {
		if _, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("runtime policy narrowing for %s/%s: %v", key.Namespace, key.Name, err)
		}
		waitRuntimeSourceStates(t, ctx, apiClient, key, 1, v1alpha1.SourceStateError, 0, "")
	}
	if len(resourceLister.Calls()) != listCallsBeforeNarrowing {
		t.Fatalf("narrowed policy issued resource LIST: before=%d after=%d", listCallsBeforeNarrowing, len(resourceLister.Calls()))
	}

	if err := apiClient.Delete(ctx, policy); err != nil {
		t.Fatalf("delete runtime policy after admission: %v", err)
	}
	tracker.InvalidateAll()
	listCallsBeforeDeletion := len(resourceLister.Calls())
	for _, key := range []types.NamespacedName{legacyKey, admittedKey} {
		if _, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("runtime policy deletion for %s/%s: %v", key.Namespace, key.Name, err)
		}
		waitRuntimeSourceStates(t, ctx, apiClient, key, 1, v1alpha1.SourceStateError, 0, "")
		status := &v1alpha1.Kubeseer{}
		if err := apiClient.Get(ctx, key, status); err != nil {
			t.Fatalf("read policy-deletion status for %s/%s: %v", key.Namespace, key.Name, err)
		}
		assertRuntimeCondition(t, status.Status, statuscontract.ConditionAuthorized, metav1.ConditionFalse, statuscontract.ReasonPolicyMissing)
	}
	if len(resourceLister.Calls()) != listCallsBeforeDeletion {
		t.Fatalf("missing policy issued resource LIST: before=%d after=%d", listCallsBeforeDeletion, len(resourceLister.Calls()))
	}

	if err := apiClient.Create(ctx, runtimeEnvtestPolicy(namespace)); err != nil {
		t.Fatalf("recreate runtime policy after admission drift: %v", err)
	}
	tracker.InvalidateAll()
	listCallsBeforeRecovery := len(resourceLister.Calls())
	for _, key := range []types.NamespacedName{legacyKey, admittedKey} {
		if _, err := runtimeInstance.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("runtime policy recovery for %s/%s: %v", key.Namespace, key.Name, err)
		}
		waitRuntimeSourceStates(t, ctx, apiClient, key, 1, v1alpha1.SourceStateValues, 1, "")
	}
	if len(resourceLister.Calls()) != listCallsBeforeRecovery+2 {
		t.Fatalf("runtime policy recovery LIST calls = %d, want %d", len(resourceLister.Calls()), listCallsBeforeRecovery+2)
	}
}

func runRuntimeEnvtestAuthorizationPaginationScenario(t *testing.T, ctx context.Context, apiClient crclient.Client, clients Clients, resolver *discoveryruntime.Resolver, namespace string, key types.NamespacedName) {
	t.Helper()
	source := runtimeEnvtestObservedSource("authorization-pagination")
	plan, err := selection.NewPlanner(resolver).Plan(ctx, namespace, source)
	if err != nil {
		t.Fatalf("plan real pagination source: %v", err)
	}
	owner := &v1alpha1.Kubeseer{}
	if err := apiClient.Get(ctx, key, owner); err != nil {
		t.Fatalf("read pagination owner: %v", err)
	}
	tracker := reconciliation.NewFreshnessTracker()
	tracker.Observe(owner)
	lease, child, release, err := tracker.Acquire(ctx, key, owner.UID, owner.Generation)
	if err != nil {
		t.Fatalf("acquire pagination lease: %v", err)
	}
	defer release()
	subject, err := authorization.NewSubject(key, lease.UID, lease.Generation, lease.PolicyEpoch)
	if err != nil {
		t.Fatalf("construct pagination subject: %v", err)
	}
	snapshot := accesspolicy.Load(ctx, accesspolicy.NewClientPolicySource(apiClient))
	if snapshot.IsTerminal() {
		t.Fatalf("pagination policy snapshot is terminal: %s", snapshot.TerminalReason())
	}
	requests := make([]accesspolicy.Request, 0, len(plan.Targets()))
	for _, target := range plan.Targets() {
		requests = append(requests, selection.RequestForTarget(target))
	}
	batch, err := authorization.NewEnforcer(nil).EvaluateBatch(child, subject, snapshot, requests)
	if err != nil {
		t.Fatalf("evaluate real pagination source: %v", err)
	}
	authorized, err := selection.BindCapabilities(plan, batch.Outcomes())
	if err != nil {
		t.Fatalf("bind real pagination source: %v", err)
	}
	observedResources := clients.Dynamic.Resource(schema.GroupVersionResource{Group: "runtime.kubeseer.io", Version: "v1", Resource: "observations"}).Namespace(namespace)
	for index := 0; index < 3; index++ {
		fixture := runtimeEnvtestObservation("authorization-pagination-"+string(rune('a'+index)), namespace, "yes", "pagination-value-"+string(rune('a'+index)))
		if _, err := observedResources.Create(ctx, fixture, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create pagination fixture %s: %v", fixture.GetName(), err)
		}
	}
	dynamicLister := selection.NewDynamicResourceLister(clients.Dynamic, tracker)
	outcome := selection.NewExecutor(dynamicLister, selection.WithPageLimit(1), selection.WithVerifier(tracker)).Execute(child, authorized)
	if outcome.Err != nil || len(outcome.Resources) != 3 {
		t.Fatalf("real paginated LIST outcome = %#v, want three resources", outcome)
	}

	current := true
	verifier := authorization.VerifierFunc(func(candidate authorization.Subject) bool {
		return current && tracker.IsCurrent(candidate)
	})
	staleDynamicLister := selection.NewDynamicResourceLister(clients.Dynamic, verifier)
	calls := 0
	staleLister := runtimeEnvtestResourceListerFunc(func(ctx context.Context, read selection.AuthorizedRead, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		calls++
		response, err := staleDynamicLister.List(ctx, read, options)
		if calls == 1 {
			if err != nil {
				t.Fatalf("first stale-pagination page: %v", err)
			}
			if response == nil || response.GetContinue() == "" {
				t.Fatalf("real API did not return a continuation page for the stale barrier")
			}
			current = false
		}
		return response, err
	})
	staleOutcome := selection.NewExecutor(staleLister, selection.WithPageLimit(1), selection.WithVerifier(verifier)).Execute(child, authorized)
	if !selection.HasReason(staleOutcome.Err, selection.ReasonAuthorizationStale) || calls != 1 || len(staleOutcome.Resources) != 0 {
		t.Fatalf("stale real pagination outcome = %#v calls=%d, want one page and no continuation request", staleOutcome, calls)
	}
}

type runtimeEnvtestResourceListerFunc func(context.Context, selection.AuthorizedRead, metav1.ListOptions) (*unstructured.UnstructuredList, error)

func (f runtimeEnvtestResourceListerFunc) List(ctx context.Context, read selection.AuthorizedRead, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	if f == nil {
		return nil, errors.New("resource lister function is not configured")
	}
	return f(ctx, read, options)
}

func envtestStatusEvaluation(result v1alpha1.KubeseerResult) statuscontract.Evaluation {
	return statuscontract.Evaluation{Result: result.DeepCopy()}
}

func assertRuntimeStatusSnapshot(t *testing.T, object *v1alpha1.Kubeseer, generation int64, wantResult bool) {
	t.Helper()
	if object.Status.ObservedGeneration != generation {
		t.Fatalf("status observed generation = %d, want %d", object.Status.ObservedGeneration, generation)
	}
	wantTypes := []string{
		statuscontract.ConditionAccepted,
		statuscontract.ConditionAuthorized,
		statuscontract.ConditionSourcesResolved,
		statuscontract.ConditionReady,
		statuscontract.ConditionDegraded,
	}
	if len(object.Status.Conditions) < len(wantTypes) {
		t.Fatalf("status condition count = %d, want at least %d: %#v", len(object.Status.Conditions), len(wantTypes), object.Status.Conditions)
	}
	seen := make(map[string]int, len(object.Status.Conditions))
	for index, condition := range object.Status.Conditions {
		seen[condition.Type]++
		if index < len(wantTypes) && condition.Type != wantTypes[index] {
			t.Fatalf("canonical condition %d = %q, want %q: %#v", index, condition.Type, wantTypes[index], object.Status.Conditions)
		}
		if condition.Type == "" || condition.Reason == "" || condition.Message == "" || condition.LastTransitionTime.IsZero() || index < len(wantTypes) && condition.ObservedGeneration != generation {
			t.Fatalf("incomplete condition = %#v", condition)
		}
	}
	for _, conditionType := range wantTypes {
		if seen[conditionType] != 1 {
			t.Fatalf("canonical condition %s count = %d: %#v", conditionType, seen[conditionType], object.Status.Conditions)
		}
	}
	ready := runtimeCondition(object.Status, statuscontract.ConditionReady)
	degraded := runtimeCondition(object.Status, statuscontract.ConditionDegraded)
	if ready.Status == metav1.ConditionTrue && degraded.Status == metav1.ConditionTrue {
		t.Fatalf("Ready and Degraded are both true: %#v", object.Status.Conditions)
	}
	if wantResult {
		if object.Status.Result == nil || object.Status.Summary == nil || object.Status.ResultHash == "" {
			t.Fatalf("complete status snapshot omitted result-derived fields: %#v", object.Status)
		}
		derived, err := statuscontract.DeriveResult(object.Status.Result)
		if err != nil {
			t.Fatalf("derive persisted status result: %v", err)
		}
		if !reflect.DeepEqual(derived.Summary, object.Status.Summary) || derived.ResultHash != object.Status.ResultHash {
			t.Fatalf("persisted derived fields drifted: derived=%#v status=%#v", derived, object.Status)
		}
	} else if object.Status.Result != nil || object.Status.Summary != nil || object.Status.ResultHash != "" {
		t.Fatalf("status snapshot unexpectedly contains result-derived fields: %#v", object.Status)
	}
	for _, condition := range object.Status.Conditions {
		for _, forbidden := range []string{"observed-initial-value", "observed-updated-value", "runtime-status-pod", "{.spec.value}", "{.metadata.name}", "secret-sentinel"} {
			if strings.Contains(condition.Message, forbidden) {
				t.Fatalf("condition message leaked %q: %#v", forbidden, condition)
			}
		}
	}
}

func assertRuntimeCondition(t *testing.T, candidate v1alpha1.KubeseerStatus, conditionType string, conditionStatus metav1.ConditionStatus, reason string) {
	t.Helper()
	condition := runtimeCondition(candidate, conditionType)
	if condition.Type == "" || condition.Status != conditionStatus || condition.Reason != reason {
		t.Fatalf("condition %s = %#v, want status=%s reason=%s", conditionType, condition, conditionStatus, reason)
	}
}

func runtimeCondition(candidate v1alpha1.KubeseerStatus, conditionType string) metav1.Condition {
	for _, condition := range candidate.Conditions {
		if condition.Type == conditionType {
			return condition
		}
	}
	return metav1.Condition{}
}

func runtimeEnvtestQueueGet(t *testing.T, ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) (reconcile.Request, bool) {
	t.Helper()
	itemCh := make(chan reconcile.Request, 1)
	shutdownCh := make(chan bool, 1)
	go func() {
		item, shutdown := queue.Get()
		itemCh <- item
		shutdownCh <- shutdown
	}()
	select {
	case item := <-itemCh:
		return item, <-shutdownCh
	case <-ctx.Done():
		t.Fatalf("context ended while waiting for rate-limited retry: %v", ctx.Err())
		return reconcile.Request{}, true
	case <-time.After(3 * time.Second):
		t.Fatalf("rate-limited retry was not requeued")
		return reconcile.Request{}, true
	}
}

type runtimeEnvtestDiscoveryAdapter struct {
	delegate interface {
		ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error)
	}
	mu       sync.Mutex
	failNext bool
}

func (a *runtimeEnvtestDiscoveryAdapter) FailNext() {
	a.mu.Lock()
	a.failNext = true
	a.mu.Unlock()
}

func (a *runtimeEnvtestDiscoveryAdapter) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	a.mu.Lock()
	fail := a.failNext
	a.failNext = false
	a.mu.Unlock()
	if fail {
		return nil, errors.New("simulated discovery API unavailability")
	}
	return a.delegate.ServerResourcesForGroupVersion(groupVersion)
}

type runtimeEnvtestResourceListerAdapter struct {
	delegate   selection.ResourceLister
	mu         sync.Mutex
	failNext   bool
	forbidNext bool
	block      *runtimeEnvtestListBlock
	calls      []selection.ReadTarget
}

type runtimeEnvtestListBlock struct {
	release <-chan struct{}
	started chan<- struct{}
}

func (a *runtimeEnvtestResourceListerAdapter) FailNext() {
	a.mu.Lock()
	a.failNext = true
	a.mu.Unlock()
}

func (a *runtimeEnvtestResourceListerAdapter) ForbidNext() {
	a.mu.Lock()
	a.forbidNext = true
	a.mu.Unlock()
}

func (a *runtimeEnvtestResourceListerAdapter) BlockNext(release <-chan struct{}, started chan<- struct{}) {
	a.mu.Lock()
	a.block = &runtimeEnvtestListBlock{release: release, started: started}
	a.mu.Unlock()
}

func (a *runtimeEnvtestResourceListerAdapter) Calls() []selection.ReadTarget {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]selection.ReadTarget(nil), a.calls...)
}

func (a *runtimeEnvtestResourceListerAdapter) List(ctx context.Context, read selection.AuthorizedRead, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	target := read.Target()
	a.mu.Lock()
	a.calls = append(a.calls, target)
	fail := a.failNext
	a.failNext = false
	forbid := a.forbidNext
	a.forbidNext = false
	block := a.block
	a.block = nil
	a.mu.Unlock()
	if fail {
		return nil, errors.New("simulated resource LIST unavailability")
	}
	if forbid {
		return nil, apierrors.NewForbidden(schema.GroupResource{Group: "", Resource: "pods"}, target.SourceID, errors.New("simulated RBAC denial"))
	}
	if block != nil {
		if block.started != nil {
			close(block.started)
		}
		select {
		case <-block.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return a.delegate.List(ctx, read, options)
}

type runtimeEnvtestRouteManager struct{}

func (*runtimeEnvtestRouteManager) Replace(reconciliation.Lease, []reconciliation.AuthorizedRoute) error {
	return nil
}

func (*runtimeEnvtestRouteManager) RemoveOwner(types.NamespacedName) {}

func (*runtimeEnvtestRouteManager) RemoveAll() {}

type runtimeEnvtestCountingStatusWriter struct {
	delegate reconciliation.StatusWriter
	mu       sync.Mutex
	calls    int
}

func (w *runtimeEnvtestCountingStatusWriter) Update(ctx context.Context, object *v1alpha1.Kubeseer) error {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	return w.delegate.Update(ctx, object)
}

func (w *runtimeEnvtestCountingStatusWriter) Calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

type runtimeEnvtestConflictStatusWriter struct {
	delegate reconciliation.StatusWriter
	client   crclient.Client
	key      types.NamespacedName
	mu       sync.Mutex
	calls    int
}

func (w *runtimeEnvtestConflictStatusWriter) Update(ctx context.Context, object *v1alpha1.Kubeseer) error {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	current := &v1alpha1.Kubeseer{}
	if err := w.client.Get(ctx, w.key, current); err != nil {
		return err
	}
	current.Status.Conditions = []metav1.Condition{{Type: "ConflictFixture", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now(), Reason: "ConcurrentWrite", Message: "newer API state"}}
	if err := w.client.Status().Update(ctx, current); err != nil {
		return err
	}
	return w.delegate.Update(ctx, object)
}

func (w *runtimeEnvtestConflictStatusWriter) Calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}
