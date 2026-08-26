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
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func assertReconciliationRuntimeStatusScenarios(t *testing.T, ctx context.Context) {
	t.Helper()
	key := types.NamespacedName{Namespace: "team-a", Name: "status-owner"}
	result := runtimeStatusResult()

	t.Run("normalization distinguishes pointer presence and every meaningful value", func(t *testing.T) {
		emptyCollections := result.DeepCopy()
		normalizedCollections := result.DeepCopy()
		normalizedCollections.Sources[0].FieldErrors = []v1alpha1.KubeseerFieldError{}
		normalizedCollections.Sources[0].Resources[0].Fields[1].Matches = []v1alpha1.KubeseerTypedMatch{}
		if !reconciliation.SemanticallyEqualResult(emptyCollections, normalizedCollections) {
			t.Fatal("nil and empty nested result collections were not normalized")
		}
		if reconciliation.SemanticallyEqualStatus(v1alpha1.KubeseerStatus{}, v1alpha1.KubeseerStatus{Result: &v1alpha1.KubeseerResult{}}) {
			t.Fatal("nil result pointer was collapsed into a present empty result")
		}

		mutations := []struct {
			name   string
			mutate func(*v1alpha1.KubeseerResult)
		}{
			{name: "source identity", mutate: func(value *v1alpha1.KubeseerResult) { value.Sources[0].ID = "other-source" }},
			{name: "source state", mutate: func(value *v1alpha1.KubeseerResult) { value.Sources[0].State = v1alpha1.SourceStateError }},
			{name: "source diagnostic", mutate: func(value *v1alpha1.KubeseerResult) {
				value.Sources[0].Error = &v1alpha1.KubeseerResultError{Reason: "new-reason"}
			}},
			{name: "resource provenance", mutate: func(value *v1alpha1.KubeseerResult) { value.Sources[0].Resources[0].Name = "other-resource" }},
			{name: "field identity", mutate: func(value *v1alpha1.KubeseerResult) { value.Sources[0].Resources[0].Fields[0].Name = "other-field" }},
			{name: "field type", mutate: func(value *v1alpha1.KubeseerResult) {
				value.Sources[0].Resources[0].Fields[0].Type = v1alpha1.ValueTypeNumber
			}},
			{name: "field state", mutate: func(value *v1alpha1.KubeseerResult) {
				value.Sources[0].Resources[0].Fields[0].State = v1alpha1.FieldStateAbsent
			}},
			{name: "match cardinality", mutate: func(value *v1alpha1.KubeseerResult) {
				value.Sources[0].Resources[0].Fields[0].Matches = append(value.Sources[0].Resources[0].Fields[0].Matches, v1alpha1.KubeseerTypedMatch{State: v1alpha1.MatchStateNull})
			}},
			{name: "typed payload", mutate: func(value *v1alpha1.KubeseerResult) {
				next := "different-value"
				value.Sources[0].Resources[0].Fields[0].Matches[0].StringValue = &next
			}},
		}
		for _, test := range mutations {
			t.Run(test.name, func(t *testing.T) {
				changed := result.DeepCopy()
				test.mutate(changed)
				if reconciliation.SemanticallyEqualResult(&result, changed) {
					t.Fatalf("meaningful mutation %q was treated as equivalent", test.name)
				}
			})
		}
		left := runtimeStatusObject(key, "status-uid", 1, &result)
		right := left.DeepCopy()
		right.Status.Conditions[0].Message = "condition changed outside this feature"
		if !reconciliation.SemanticallyEqualStatus(left.Status, right.Status) {
			t.Fatal("condition-only change affected the feature status projection")
		}
	})

	t.Run("equivalent status performs no write and meaningful status preserves spec and conditions", func(t *testing.T) {
		current := runtimeStatusObject(key, "status-uid", 1, &result)
		current.Status.Result.Sources[0].FieldErrors = nil
		writer := &runtimeStatusWriter{}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		publisher := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker)
		if err := publisher.Publish(ctx, lease, *result.DeepCopy()); err != nil {
			t.Fatalf("equivalent status publish: %v", err)
		}
		if writer.Calls() != 0 {
			t.Fatalf("equivalent status issued %d writes", writer.Calls())
		}

		changed := current.DeepCopy()
		changed.Status.Result = nil
		changed.Status.ObservedGeneration = 0
		reader := newRuntimeStatusReader(changed)
		writer = &runtimeStatusWriter{}
		tracker = reconciliation.NewFreshnessTracker()
		lease, release = runtimeStatusLease(t, tracker, changed)
		defer release()
		publisher = reconciliation.NewStatusPublisher(reader, writer, tracker)
		if err := publisher.Publish(ctx, lease, *result.DeepCopy()); err != nil {
			t.Fatalf("meaningful status publish: %v", err)
		}
		if writer.Calls() != 1 || writer.Last() == nil {
			t.Fatalf("meaningful status writes = %d last=%#v", writer.Calls(), writer.Last())
		}
		last := writer.Last()
		if last.Status.ObservedGeneration != 1 || !reconciliation.SemanticallyEqualResult(last.Status.Result, &result) || !reflect.DeepEqual(last.Spec, changed.Spec) || !reflect.DeepEqual(last.Status.Conditions, changed.Status.Conditions) || last.ResourceVersion != changed.ResourceVersion {
			t.Fatalf("status update did not preserve current object fields: %#v", last)
		}
	})

	t.Run("generation-only change publishes observed generation", func(t *testing.T) {
		current := runtimeStatusObject(key, "generation-uid", 2, &result)
		current.Status.ObservedGeneration = 1
		writer := &runtimeStatusWriter{}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, *result.DeepCopy()); err != nil {
			t.Fatalf("generation-only publish: %v", err)
		}
		if writer.Calls() != 1 || writer.Last().Status.ObservedGeneration != 2 {
			t.Fatalf("generation-only update = %#v", writer.Last())
		}
	})

	t.Run("conflict and transient read failures are retryable with one write attempt", func(t *testing.T) {
		current := runtimeStatusObject(key, "conflict-uid", 1, nil)
		writer := &runtimeStatusWriter{err: apierrors.NewConflict(schema.GroupResource{Group: "kubeseer.io", Resource: "kubeseers"}, key.Name, errors.New("newer status must win"))}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, result)
		if err == nil || !reconciliation.IsRetryable(err) || writer.Calls() != 1 {
			t.Fatalf("conflict result = err=%v writes=%d", err, writer.Calls())
		}
		var runtimeErr *reconciliation.RuntimeError
		if !errors.As(err, &runtimeErr) || runtimeErr.Reason != reconciliation.ReasonStatusConflict {
			t.Fatalf("conflict classification = %#v", err)
		}

		readWriter := &runtimeStatusWriter{}
		readTracker := reconciliation.NewFreshnessTracker()
		readLease, readRelease := runtimeStatusLease(t, readTracker, current)
		defer readRelease()
		readErr := reconciliation.NewStatusPublisher(&runtimeStatusReader{err: apierrors.NewServiceUnavailable("status body must remain private")}, readWriter, readTracker).Publish(ctx, readLease, result)
		if readErr == nil || !reconciliation.IsRetryable(readErr) || readWriter.Calls() != 0 {
			t.Fatalf("transient status read result = err=%v writes=%d", readErr, readWriter.Calls())
		}
	})

	t.Run("UID, generation, policy, deletion, and cancellation guards withhold writes", func(t *testing.T) {
		base := runtimeStatusObject(key, "guard-uid", 1, nil)
		tests := []struct {
			name string
			run  func(*testing.T, *v1alpha1.Kubeseer, *reconciliation.FreshnessTracker, reconciliation.Lease)
		}{
			{
				name: "UID replacement",
				run: func(t *testing.T, current *v1alpha1.Kubeseer, tracker *reconciliation.FreshnessTracker, lease reconciliation.Lease) {
					replaced := current.DeepCopy()
					replaced.UID = "new-uid"
					writer := &runtimeStatusWriter{}
					err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(replaced), writer, tracker).Publish(ctx, lease, result)
					if err == nil || writer.Calls() != 0 {
						t.Fatalf("UID replacement result = err=%v writes=%d", err, writer.Calls())
					}
				},
			},
			{
				name: "deletion",
				run: func(t *testing.T, current *v1alpha1.Kubeseer, tracker *reconciliation.FreshnessTracker, lease reconciliation.Lease) {
					deleting := current.DeepCopy()
					deleting.DeletionTimestamp = &metav1.Time{Time: time.Now()}
					writer := &runtimeStatusWriter{}
					err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(deleting), writer, tracker).Publish(ctx, lease, result)
					if err != nil || writer.Calls() != 0 {
						t.Fatalf("deletion result = err=%v writes=%d", err, writer.Calls())
					}
				},
			},
			{
				name: "stale generation",
				run: func(t *testing.T, current *v1alpha1.Kubeseer, tracker *reconciliation.FreshnessTracker, lease reconciliation.Lease) {
					updated := current.DeepCopy()
					updated.Generation = 2
					tracker.Observe(updated)
					writer := &runtimeStatusWriter{}
					err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, result)
					if err == nil || writer.Calls() != 0 {
						t.Fatalf("stale generation result = err=%v writes=%d", err, writer.Calls())
					}
				},
			},
			{
				name: "stale policy epoch",
				run: func(t *testing.T, current *v1alpha1.Kubeseer, tracker *reconciliation.FreshnessTracker, lease reconciliation.Lease) {
					tracker.InvalidateAll()
					writer := &runtimeStatusWriter{}
					err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(ctx, lease, result)
					if err == nil || writer.Calls() != 0 {
						t.Fatalf("stale policy result = err=%v writes=%d", err, writer.Calls())
					}
				},
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				current := base.DeepCopy()
				tracker := reconciliation.NewFreshnessTracker()
				lease, release := runtimeStatusLease(t, tracker, current)
				defer release()
				test.run(t, current, tracker, lease)
			})
		}

		current := base.DeepCopy()
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		writer := &runtimeStatusWriter{}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker).Publish(canceled, lease, result)
		if !errors.Is(err, context.Canceled) || writer.Calls() != 0 {
			t.Fatalf("canceled status result = err=%v writes=%d", err, writer.Calls())
		}
	})
}

func runtimeStatusResult() v1alpha1.KubeseerResult {
	value := "status-value"
	valueField := v1alpha1.KubeseerFieldResult{
		Name:  "value",
		Type:  v1alpha1.ValueTypeString,
		State: v1alpha1.FieldStateValues,
		Matches: []v1alpha1.KubeseerTypedMatch{{
			State:       v1alpha1.MatchStateValue,
			StringValue: &value,
		}},
	}
	absentField := v1alpha1.KubeseerFieldResult{Name: "absent", Type: v1alpha1.ValueTypeString, State: v1alpha1.FieldStateAbsent}
	resource := v1alpha1.KubeseerResourceResult{
		APIVersion: "v1",
		Kind:       "Pod",
		Namespace:  "team-a",
		Name:       "status-resource",
		UID:        "status-resource-uid",
		Fields:     []v1alpha1.KubeseerFieldResult{valueField, absentField},
	}
	source := v1alpha1.KubeseerSourceResult{ID: "status-source", State: v1alpha1.SourceStateValues, Resources: []v1alpha1.KubeseerResourceResult{resource}}
	return v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{source}}
}

func runtimeStatusObject(key types.NamespacedName, uid types.UID, generation int64, result *v1alpha1.KubeseerResult) *v1alpha1.Kubeseer {
	conditions := []metav1.Condition{{Type: "Observed", Status: metav1.ConditionTrue, Reason: "Kept", Message: "condition survives status publication", ObservedGeneration: generation}}
	return &v1alpha1.Kubeseer{
		ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name, UID: uid, Generation: generation, ResourceVersion: "rv-status"},
		Spec:       v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{{ID: "configured-source", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}}}},
		Status:     v1alpha1.KubeseerStatus{ObservedGeneration: generation, Conditions: conditions, Result: result.DeepCopy()},
	}
}

func runtimeStatusLease(t *testing.T, tracker *reconciliation.FreshnessTracker, object *v1alpha1.Kubeseer) (reconciliation.Lease, func()) {
	t.Helper()
	tracker.Observe(object)
	lease, _, release, err := tracker.Acquire(context.Background(), types.NamespacedName{Namespace: object.Namespace, Name: object.Name}, object.UID, object.Generation)
	if err != nil {
		t.Fatalf("acquire status lease: %v", err)
	}
	return lease, release
}

type runtimeStatusReader struct {
	mu     sync.Mutex
	object *v1alpha1.Kubeseer
	err    error
}

func newRuntimeStatusReader(object *v1alpha1.Kubeseer) *runtimeStatusReader {
	return &runtimeStatusReader{object: object.DeepCopy()}
}

func (r *runtimeStatusReader) Get(ctx context.Context, _ types.NamespacedName, object *v1alpha1.Kubeseer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.object == nil {
		return apierrors.NewNotFound(schema.GroupResource{Group: "kubeseer.io", Resource: "kubeseers"}, "status-owner")
	}
	*object = *r.object.DeepCopy()
	return nil
}

type runtimeStatusWriter struct {
	mu    sync.Mutex
	calls int
	last  *v1alpha1.Kubeseer
	err   error
}

func (w *runtimeStatusWriter) Update(_ context.Context, object *v1alpha1.Kubeseer) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	w.last = object.DeepCopy()
	return w.err
}

func (w *runtimeStatusWriter) Calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func (w *runtimeStatusWriter) Last() *v1alpha1.Kubeseer {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.last.DeepCopy()
}
