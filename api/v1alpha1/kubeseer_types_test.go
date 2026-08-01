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

package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("add Kubeseer types to scheme: %v", err)
	}

	for _, object := range []runtime.Object{&Kubeseer{}, &KubeseerList{}} {
		gvks, _, err := scheme.ObjectKinds(object)
		if err != nil {
			t.Fatalf("resolve GVK for %T: %v", object, err)
		}
		if len(gvks) != 1 || gvks[0].GroupVersion() != GroupVersion {
			t.Fatalf("unexpected GVKs for %T: %v", object, gvks)
		}
	}

	if _, err := scheme.New(GroupVersion.WithKind("Kubeseer")); err != nil {
		t.Fatalf("construct Kubeseer from scheme: %v", err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	original := Kubeseer{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "Kubeseer"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
		},
		Spec: KubeseerSpec{
			Sources: []KubeseerSource{{ID: "source-one"}},
		},
		Status: KubeseerStatus{
			ObservedGeneration: 7,
			Conditions: []metav1.Condition{{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 7,
				LastTransitionTime: metav1.Time{Time: time.Date(2026, time.August, 1, 11, 30, 0, 0, time.UTC)},
				Reason:             "Available",
				Message:            "API contract is available",
			}},
			Result: &KubeseerResult{},
		},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal Kubeseer: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("inspect Kubeseer JSON: %v", err)
	}
	for _, field := range []string{"apiVersion", "kind", "metadata", "spec", "status"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("serialized Kubeseer is missing %q: %s", field, encoded)
		}
	}

	var decoded Kubeseer
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal Kubeseer: %v", err)
	}
	if decoded.TypeMeta != original.TypeMeta || decoded.Name != original.Name || decoded.Namespace != original.Namespace {
		t.Fatalf("round-trip changed object identity: original=%#v decoded=%#v", original, decoded)
	}
	if !reflect.DeepEqual(original.Spec, decoded.Spec) {
		t.Fatalf("round-trip changed spec: original=%#v decoded=%#v", original.Spec, decoded.Spec)
	}
	if decoded.Status.ObservedGeneration != original.Status.ObservedGeneration || decoded.Status.Result == nil {
		t.Fatalf("round-trip changed status envelope: original=%#v decoded=%#v", original.Status, decoded.Status)
	}
	if len(decoded.Status.Conditions) != 1 {
		t.Fatalf("round-trip changed conditions: %#v", decoded.Status.Conditions)
	}
	originalCondition := original.Status.Conditions[0]
	decodedCondition := decoded.Status.Conditions[0]
	if decodedCondition.Type != originalCondition.Type || decodedCondition.Status != originalCondition.Status ||
		decodedCondition.ObservedGeneration != originalCondition.ObservedGeneration ||
		decodedCondition.Reason != originalCondition.Reason || decodedCondition.Message != originalCondition.Message ||
		!decodedCondition.LastTransitionTime.Time.Equal(originalCondition.LastTransitionTime.Time) {
		t.Fatalf("round-trip changed condition: original=%#v decoded=%#v", originalCondition, decodedCondition)
	}

	withoutSources := Kubeseer{TypeMeta: original.TypeMeta, Spec: KubeseerSpec{}}
	withoutSourcesJSON, err := json.Marshal(withoutSources)
	if err != nil {
		t.Fatalf("marshal Kubeseer without sources: %v", err)
	}
	var omitted map[string]json.RawMessage
	if err := json.Unmarshal(withoutSourcesJSON, &omitted); err != nil {
		t.Fatalf("inspect Kubeseer without sources: %v", err)
	}
	var specFields map[string]json.RawMessage
	if err := json.Unmarshal(omitted["spec"], &specFields); err != nil {
		t.Fatalf("inspect empty spec: %v", err)
	}
	if _, ok := specFields["sources"]; ok {
		t.Fatalf("omitted sources field was synthesized: %s", withoutSourcesJSON)
	}
}

func TestDeepCopyIsolation(t *testing.T) {
	original := &Kubeseer{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"environment": "test"},
		},
		Spec: KubeseerSpec{
			Sources: []KubeseerSource{{ID: "source-one"}},
		},
		Status: KubeseerStatus{
			Conditions: []metav1.Condition{{Type: "Ready", Reason: "Available"}},
			Result:     &KubeseerResult{},
		},
	}

	copy := original.DeepCopy()
	if copy == original {
		t.Fatal("DeepCopy returned the original pointer")
	}
	if copy.Status.Result == nil {
		t.Fatal("DeepCopy dropped the result envelope")
	}

	copy.Labels["environment"] = "production"
	copy.Spec.Sources[0].ID = "changed"
	copy.Status.Conditions[0].Reason = "Changed"

	if original.Labels["environment"] != "test" {
		t.Fatalf("metadata labels alias original: %#v", original.Labels)
	}
	if original.Spec.Sources[0].ID != "source-one" {
		t.Fatalf("spec sources alias original: %#v", original.Spec.Sources)
	}
	if original.Status.Conditions[0].Reason != "Available" {
		t.Fatalf("status conditions alias original: %#v", original.Status.Conditions)
	}
}
