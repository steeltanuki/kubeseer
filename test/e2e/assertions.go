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
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AssertConditions compares the complete canonical condition set. Tests pass
// literal expected values and therefore do not reproduce status algorithms.
func AssertConditions(t testing.TB, got []metav1.Condition, expected []metav1.Condition) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("conditions length: got %d, want %d", len(got), len(expected))
	}
	for index := range expected {
		if got[index].Type != expected[index].Type || got[index].Status != expected[index].Status || got[index].Reason != expected[index].Reason || got[index].Message != expected[index].Message {
			t.Fatalf("condition %d differs: got %#v, want %#v", index, got[index], expected[index])
		}
	}
}

func AssertSummary(t testing.TB, got *v1alpha1.KubeseerSummary, expected v1alpha1.KubeseerSummary) {
	t.Helper()
	if got == nil || *got != expected {
		t.Fatalf("summary differs: got %#v, want %#v", got, expected)
	}
}

func AssertResultHash(t testing.TB, status v1alpha1.KubeseerStatus, expected string) {
	t.Helper()
	if status.ResultHash != expected || !strings.HasPrefix(status.ResultHash, "sha256:") {
		t.Fatalf("result hash: got %q, want %q", status.ResultHash, expected)
	}
}

func AssertSourceOrder(t testing.TB, result *v1alpha1.KubeseerResult, expectedIDs []string) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Sources) != len(expectedIDs) {
		t.Fatalf("source count: got %d, want %d", len(result.Sources), len(expectedIDs))
	}
	for index, expected := range expectedIDs {
		if result.Sources[index].ID != expected {
			t.Fatalf("source order at %d: got %q, want %q", index, result.Sources[index].ID, expected)
		}
	}
}

func AssertProvenance(t testing.TB, got []v1alpha1.KubeseerResourceProvenance, expected []v1alpha1.KubeseerResourceProvenance) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("provenance length: got %d, want %d", len(got), len(expected))
	}
	for index := range expected {
		if got[index] != expected[index] {
			t.Fatalf("provenance %d differs: got %#v, want %#v", index, got[index], expected[index])
		}
	}
}

// AssertPublicStatus verifies only API-visible semantic fields. It is safe to
// use in diagnostics because the result body is never formatted there.
func AssertPublicStatus(t testing.TB, status v1alpha1.KubeseerStatus, expectedSummary *v1alpha1.KubeseerSummary, expectedHash string) {
	t.Helper()
	if expectedSummary != nil {
		AssertSummary(t, status.Summary, *expectedSummary)
	}
	if expectedHash != "" {
		AssertResultHash(t, status, expectedHash)
	}
	if status.ObservedGeneration == 0 {
		t.Fatal("status observedGeneration is not populated")
	}
}

// SanitizeError maps an arbitrary client error to a stable reason and never
// includes the downstream body, selector operands, or observed values.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "forbidden"):
		return "Forbidden"
	case strings.Contains(message, "not found"):
		return "NotFound"
	case strings.Contains(message, "timeout") || strings.Contains(message, "deadline"):
		return "Timeout"
	default:
		return "RequestFailed"
	}
}

// DiagnosticProjection is the allowlisted failure projection. Result bodies,
// extracted values, Secret payloads, kubeconfig bytes, tokens, and arbitrary
// log fields have no representation in this type.
type DiagnosticProjection struct {
	Phase             string                    `json:"phase"`
	Scenario          string                    `json:"scenario,omitempty"`
	Awaited           string                    `json:"awaited,omitempty"`
	SourceRevision    string                    `json:"sourceRevision"`
	KubernetesVersion string                    `json:"kubernetesVersion"`
	ClusterName       string                    `json:"clusterName"`
	Namespace         string                    `json:"namespace"`
	Release           string                    `json:"release"`
	Generation        int64                     `json:"generation,omitempty"`
	Conditions        []metav1.Condition        `json:"conditions,omitempty"`
	Summary           *v1alpha1.KubeseerSummary `json:"summary,omitempty"`
	ResultHash        string                    `json:"resultHash,omitempty"`
	StableError       string                    `json:"error,omitempty"`
	MetricFamilies    []MetricSample            `json:"metrics,omitempty"`
	RecordCodes       []string                  `json:"records,omitempty"`
	EventIdentities   []string                  `json:"events,omitempty"`
}

func (p DiagnosticProjection) Validate() error {
	encoded, err := json.Marshal(p)
	if err != nil {
		return err
	}
	for _, forbidden := range []string{"stringValue", "integerValue", "numberValue", "booleanValue", "objectValue", "listValue", "secret", "token", "kubeconfig", "password"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			return fmt.Errorf("diagnostic projection contains forbidden sentinel %q", forbidden)
		}
	}
	return nil
}
