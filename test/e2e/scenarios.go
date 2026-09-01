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
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
)

//go:embed certification.json
var certificationManifest []byte

type Manifest struct {
	Features         []FeatureMapping   `json:"features"`
	MinimumScenarios []MinimumScenario  `json:"minimumScenarios"`
	Prerequisites    []Prerequisite     `json:"prerequisites"`
	Scenarios        []ManifestScenario `json:"scenarios"`
}

type FeatureMapping struct {
	Name          string   `json:"name"`
	Scenarios     []string `json:"scenarios"`
	Prerequisites []string `json:"prerequisites,omitempty"`
}

type MinimumScenario struct {
	Key       string   `json:"key"`
	Scenarios []string `json:"scenarios"`
}

type Prerequisite struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Expect  string `json:"expect"`
}

type ManifestScenario struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RegistryKey string `json:"registryKey"`
}

// ScenarioFunc is the only executable boundary. Every scenario receives the
// public clients and a bounded context, never a production implementation.
type ScenarioFunc func(context.Context, *testing.T, *ClusterSession)

type Scenario struct {
	ID          string
	Name        string
	RegistryKey string
	Run         ScenarioFunc
}

var scenarioRegistry = []Scenario{
	{ID: "E2E-001", Name: "Package bootstrap and admission", RegistryKey: "package-bootstrap", Run: scenarioPackageBootstrap},
	{ID: "E2E-002", Name: "Same-namespace Deployment", RegistryKey: "same-namespace-deployment", Run: scenarioSameNamespaceDeployment},
	{ID: "E2E-003", Name: "Multi-namespace Pods", RegistryKey: "multi-namespace-pods", Run: scenarioMultiNamespacePods},
	{ID: "E2E-004", Name: "Custom Resource and labels", RegistryKey: "custom-resource-labels", Run: scenarioCustomResourceLabels},
	{ID: "E2E-005", Name: "Extraction, typing, and operators", RegistryKey: "extraction-typing-operators", Run: scenarioExtractionTypingOperators},
	{ID: "E2E-006", Name: "Numeric aggregation", RegistryKey: "numeric-aggregation", Run: scenarioNumericAggregation},
	{ID: "E2E-007", Name: "Missing type and partial degradation", RegistryKey: "missing-type-degradation", Run: scenarioMissingTypeDegradation},
	{ID: "E2E-008", Name: "Authorization denial after policy drift", RegistryKey: "authorization-policy-drift", Run: scenarioAuthorizationPolicyDrift},
	{ID: "E2E-009", Name: "Resource and status limits", RegistryKey: "resource-status-limits", Run: scenarioResourceStatusLimits},
	{ID: "E2E-010", Name: "Source update and semantic no-op", RegistryKey: "source-update-noop", Run: scenarioSourceUpdateNoop},
	{ID: "E2E-011", Name: "Source deletion and policy restriction", RegistryKey: "source-deletion-policy", Run: scenarioSourceDeletionPolicy},
	{ID: "E2E-012", Name: "Manager restart", RegistryKey: "manager-restart", Run: scenarioManagerRestart},
	{ID: "E2E-013", Name: "Kubeseer deletion", RegistryKey: "kubeseer-deletion", Run: scenarioKubeseerDeletion},
	{ID: "E2E-014", Name: "Overlapping instances", RegistryKey: "overlapping-instances", Run: scenarioOverlappingInstances},
	{ID: "E2E-015", Name: "Status and observability correlation", RegistryKey: "status-observability", Run: scenarioStatusObservability},
}

var expectedFeatureNames = []string{
	"kubeseer-api-foundation", "integration-testing-foundation", "resource-discovery", "installation-access-policy",
	"resource-selection", "field-extraction", "typed-output-model", "reconciliation-runtime", "status-and-conditions",
	"authorization-enforcement", "value-operators", "cross-namespace-aggregation", "admission-validation", "observability",
	"performance-and-limits", "packaging-and-installation",
}

var expectedMinimumScenarioKeys = []string{
	"deployment-in-kubeseer-namespace", "pods-across-multiple-namespaces", "custom-resource", "label-selection",
	"scalar-jsonpath", "list-jsonpath", "typed-values", "numeric-aggregation", "partial-degraded-result",
	"forbidden-namespace", "forbidden-kind", "missing-resource-type-or-crd", "source-update", "semantic-no-op",
	"source-deletion", "policy-restriction", "operator-restart", "oversized-output", "kubeseer-deletion", "overlapping-instances",
}

// LoadManifest rejects unknown fields so a new release-scope entry cannot be
// silently ignored by the registry.
func LoadManifest() (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(certificationManifest))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode certification manifest: %w", err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Manifest{}, errors.New("certification manifest contains trailing JSON")
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest) error {
	if len(manifest.Features) != len(expectedFeatureNames) {
		return fmt.Errorf("manifest features: got %d, want %d", len(manifest.Features), len(expectedFeatureNames))
	}
	if len(manifest.MinimumScenarios) != len(expectedMinimumScenarioKeys) {
		return fmt.Errorf("manifest minimum scenarios: got %d, want %d", len(manifest.MinimumScenarios), len(expectedMinimumScenarioKeys))
	}
	if len(manifest.Scenarios) != len(scenarioRegistry) {
		return fmt.Errorf("manifest scenarios: got %d, want %d", len(manifest.Scenarios), len(scenarioRegistry))
	}
	featureSet := make(map[string]struct{}, len(manifest.Features))
	for index, feature := range manifest.Features {
		if feature.Name != expectedFeatureNames[index] {
			return fmt.Errorf("feature %d: got %q, want %q", index, feature.Name, expectedFeatureNames[index])
		}
		if _, duplicate := featureSet[feature.Name]; duplicate {
			return fmt.Errorf("duplicate feature %q", feature.Name)
		}
		featureSet[feature.Name] = struct{}{}
	}
	minimumSet := make(map[string]struct{}, len(manifest.MinimumScenarios))
	for index, minimum := range manifest.MinimumScenarios {
		if minimum.Key != expectedMinimumScenarioKeys[index] {
			return fmt.Errorf("minimum scenario %d: got %q, want %q", index, minimum.Key, expectedMinimumScenarioKeys[index])
		}
		if len(minimum.Scenarios) == 0 {
			return fmt.Errorf("minimum scenario %q has no executable mapping", minimum.Key)
		}
		if _, duplicate := minimumSet[minimum.Key]; duplicate {
			return fmt.Errorf("duplicate minimum scenario %q", minimum.Key)
		}
		minimumSet[minimum.Key] = struct{}{}
	}
	prerequisiteSet := make(map[string]struct{}, len(manifest.Prerequisites))
	for _, prerequisite := range manifest.Prerequisites {
		if prerequisite.Name == "" || prerequisite.Command == "" || prerequisite.Expect == "" {
			return errors.New("prerequisite entries require name, command, and expected marker")
		}
		if _, duplicate := prerequisiteSet[prerequisite.Name]; duplicate {
			return fmt.Errorf("duplicate prerequisite %q", prerequisite.Name)
		}
		prerequisiteSet[prerequisite.Name] = struct{}{}
	}
	registryByID := make(map[string]Scenario, len(scenarioRegistry))
	registryByKey := make(map[string]Scenario, len(scenarioRegistry))
	for _, scenario := range scenarioRegistry {
		if scenario.Run == nil {
			return fmt.Errorf("scenario %s has no executable function", scenario.ID)
		}
		if _, duplicate := registryByID[scenario.ID]; duplicate {
			return fmt.Errorf("duplicate registry scenario %s", scenario.ID)
		}
		if _, duplicate := registryByKey[scenario.RegistryKey]; duplicate {
			return fmt.Errorf("duplicate registry key %q", scenario.RegistryKey)
		}
		registryByID[scenario.ID] = scenario
		registryByKey[scenario.RegistryKey] = scenario
	}
	manifestIDs := make(map[string]struct{}, len(manifest.Scenarios))
	for _, scenario := range manifest.Scenarios {
		registered, ok := registryByID[scenario.ID]
		if !ok {
			return fmt.Errorf("manifest scenario %s is not registered", scenario.ID)
		}
		if _, duplicate := manifestIDs[scenario.ID]; duplicate {
			return fmt.Errorf("duplicate manifest scenario %s", scenario.ID)
		}
		manifestIDs[scenario.ID] = struct{}{}
		if registered.RegistryKey != scenario.RegistryKey || registered.Name != scenario.Name {
			return fmt.Errorf("manifest scenario %s does not match registry", scenario.ID)
		}
		if _, known := registryByKey[scenario.RegistryKey]; !known {
			return fmt.Errorf("manifest scenario %s has an unknown registry key", scenario.ID)
		}
	}
	if len(manifestIDs) != len(registryByID) {
		return errors.New("scenario registry contains an entry absent from the manifest")
	}
	for _, feature := range manifest.Features {
		for _, id := range feature.Scenarios {
			if _, known := manifestIDs[id]; !known {
				return fmt.Errorf("feature %q references unknown scenario %q", feature.Name, id)
			}
		}
		for _, name := range feature.Prerequisites {
			if _, known := prerequisiteSet[name]; !known {
				return fmt.Errorf("feature %q references unknown prerequisite %q", feature.Name, name)
			}
		}
	}
	for _, minimum := range manifest.MinimumScenarios {
		for _, id := range minimum.Scenarios {
			if _, known := manifestIDs[id]; !known {
				return fmt.Errorf("minimum scenario %q references unknown scenario %q", minimum.Key, id)
			}
		}
	}
	return nil
}

func scenarioPackageBootstrap(ctx context.Context, t *testing.T, session *ClusterSession) {
	if err := session.ValidatePackage(ctx); err != nil {
		t.Fatalf("E2E-001 package bootstrap: %v", err)
	}
}

// Registry returns a copy so callers cannot mutate the immutable execution order.
func Registry() []Scenario {
	return append([]Scenario(nil), scenarioRegistry...)
}
