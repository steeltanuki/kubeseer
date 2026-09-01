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
	"encoding/json"
	"errors"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var forbiddenDiagnosticSentinels = []string{
	"e2e-secret-sentinel",
	"e2e-extracted-value-sentinel",
	"e2e-ambient-token-sentinel",
}

// CollectDiagnostic writes an allowlisted projection beneath the harness-owned
// directory. It does not collect raw Kubernetes, log, Secret, or kubeconfig
// bodies.
func CollectDiagnostic(metadata RunMetadata, phase, scenario, awaited string, snapshot *StatusSnapshot, err error) error {
	if metadata.Diagnostics == "" || !filepath.IsAbs(metadata.Diagnostics) {
		return errors.New("diagnostics directory must be an absolute path")
	}
	if err := os.MkdirAll(metadata.Diagnostics, 0o700); err != nil {
		return err
	}
	projection := DiagnosticProjection{
		Phase:             phase,
		Scenario:          scenario,
		Awaited:           awaited,
		SourceRevision:    metadata.SourceRevision,
		KubernetesVersion: metadata.KubernetesVersion,
		ClusterName:       metadata.ClusterName,
		Namespace:         metadata.Namespace,
		Release:           metadata.Release,
	}
	if snapshot != nil {
		projection.Generation = snapshot.Generation
		projection.Conditions = append([]metav1.Condition(nil), snapshot.Conditions...)
		projection.Summary = snapshot.Summary
		projection.ResultHash = snapshot.ResultHash
		projection.MetricFamilies = append([]MetricSample(nil), snapshot.MetricFamilies...)
		projection.RecordCodes = append([]string(nil), snapshot.RecordCodes...)
		projection.EventIdentities = append([]string(nil), snapshot.EventIdentities...)
	}
	projection.StableError = SanitizeError(err)
	if validationErr := projection.Validate(); validationErr != nil {
		return validationErr
	}
	encoded, marshalErr := json.MarshalIndent(projection, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	filename := filepath.Join(metadata.Diagnostics, "scenario-"+safeDiagnosticName(scenario)+".json")
	if writeErr := os.WriteFile(filename, encoded, 0o600); writeErr != nil {
		return writeErr
	}
	return ValidateDiagnosticSentinels(metadata.Diagnostics)
}

// ValidateDiagnosticSentinels fails closed if a protected fixture value ever
// reaches the collected diagnostic directory.
func ValidateDiagnosticSentinels(directory string) error {
	if directory == "" {
		return errors.New("diagnostics directory is required")
	}
	var files []string
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}
	for _, file := range files {
		contents, readErr := os.ReadFile(file)
		if readErr != nil {
			return readErr
		}
		for _, sentinel := range forbiddenDiagnosticSentinels {
			if strings.Contains(string(contents), sentinel) {
				return fmt.Errorf("diagnostics contain forbidden sentinel %q", sentinel)
			}
		}
	}
	return nil
}

func safeDiagnosticName(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			builder.WriteRune(character)
		} else {
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func recordScenarioDiagnostic(t *testing.T, session *ClusterSession, scenario string, snapshot *StatusSnapshot, err error) {
	t.Helper()
	if diagnosticErr := CollectDiagnostic(session.Metadata, "scenario", scenario, "public status", snapshot, err); diagnosticErr != nil {
		t.Logf("SCENARIO=%s diagnostic collection failed: %v", scenario, diagnosticErr)
	}
}
