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
	"fmt"
	"testing"
	"time"
)

// TestEndToEnd is the one non-vacuous release entry point. The harness probe
// selects this exact function and the registry executes all stable IDs in
// manifest order.
func TestEndToEnd(t *testing.T) {
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("certification manifest: %v", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("certification registry: %v", err)
	}
	metadata, err := LoadRunMetadata()
	if err != nil {
		t.Fatalf("run metadata: %v", err)
	}
	session, err := NewClusterSession(context.Background(), metadata)
	if err != nil {
		t.Fatalf("cluster session: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Minute)
	defer cancel()
	for _, scenario := range scenarioRegistry {
		scenario := scenario
		t.Run(scenario.ID, func(t *testing.T) {
			t.Helper()
			defer func() {
				if t.Failed() {
					recordScenarioDiagnostic(t, session, scenario.ID, nil, fmt.Errorf("scenario assertion failed"))
				}
			}()
			scenario.Run(ctx, t, session)
			if !t.Failed() {
				fmt.Printf("SCENARIO=%s STATUS=passed\n", scenario.ID)
			}
		})
	}
	if t.Failed() {
		return
	}
	fmt.Println("E2E_CERTIFICATION=complete STATUS=passed")
}
