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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func assertBudgetRejectionDocumentation(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..")
	paths := map[string]string{
		"api":        filepath.Join(root, "docs", "api-reference.md"),
		"operations": filepath.Join(root, "docs", "operations.md"),
		"security":   filepath.Join(root, "docs", "security.md"),
	}
	docs := make(map[string]string, len(paths))
	for name, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s guide: %v", name, err)
		}
		docs[name] = string(contents)
	}

	apiSection := documentationSection(docs["api"], "### Configuration-budget rejection", "### Summary and result")
	operationsSection := documentationSection(docs["operations"], "## Diagnose configuration-budget rejection", "## Kubernetes Events")
	securitySection := documentationSection(docs["security"], "## Budget rejection and data removal", "## Package and runtime hardening")
	for name, section := range map[string]string{"api": apiSection, "operations": operationsSection, "security": securitySection} {
		if strings.TrimSpace(section) == "" {
			t.Fatalf("%s budget-rejection section is missing", name)
		}
	}

	composed, err := status.Compose(8, nil, status.Evaluation{ConfigurationBudgetExceeded: true})
	if err != nil {
		t.Fatalf("compose documented budget rejection: %v", err)
	}
	if composed.Result != nil || composed.Summary != nil || composed.ResultHash != "" || composed.ObservedGeneration != 8 {
		t.Fatalf("documented budget rejection is not status-only: %#v", composed)
	}
	for _, condition := range composed.Conditions {
		row := "| `" + condition.Type + "` | `" + string(condition.Status) + "` | `" + condition.Reason + "` |"
		if !strings.Contains(apiSection, row) {
			// The Markdown example quotes YAML booleans; validate the semantic
			// values separately while keeping the table readable.
			if !strings.Contains(apiSection, "`"+condition.Type+"`") || !strings.Contains(apiSection, "`"+condition.Reason+"`") || !strings.Contains(apiSection, string(condition.Status)) {
				t.Fatalf("API guide omitted composed condition %s=%s/%s", condition.Type, condition.Status, condition.Reason)
			}
		}
	}
	for _, field := range []string{"status.result", "status.summary", "status.resultHash"} {
		if !strings.Contains(apiSection, field) || !strings.Contains(securitySection, field) {
			t.Fatalf("documentation omitted status field-presence rule for %s", field)
		}
	}
	for _, reason := range []string{
		status.ReasonConfigurationBudgetExceeded,
		status.ReasonAuthorizationNotEvaluated,
		status.ReasonResolutionNotEvaluated,
		status.ReasonEvaluationUnavailable,
	} {
		if !strings.Contains(apiSection, reason) || !strings.Contains(operationsSection, reason) {
			t.Fatalf("documentation omitted production reason %q", reason)
		}
	}
	if !strings.Contains(operationsSection, "resultPresent") || !strings.Contains(operationsSection, "summaryPresent") || !strings.Contains(operationsSection, "resultHashPresent") || !strings.Contains(operationsSection, "jq") {
		t.Fatal("operations guide omitted the safe status-only field projection")
	}
	for _, phrase := range []string{
		"generation",
		"observedGeneration",
		"policy denial",
		"compatible profile",
		"StatusUnavailable",
		"successful status write",
	} {
		if !strings.Contains(operationsSection+securitySection, phrase) {
			t.Fatalf("documentation omitted operational guidance %q", phrase)
		}
	}

	for name, section := range map[string]string{"api": apiSection, "operations": operationsSection, "security": securitySection} {
		for _, block := range documentationCodeBlocks(section) {
			lower := strings.ToLower(block)
			for _, forbidden := range []string{"source payload", "selector operand", "jsonpath", "secret-sentinel", "observed-initial-value"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("%s budget example contains forbidden diagnostic material %q", name, forbidden)
				}
			}
		}
	}

	for _, link := range []struct {
		section string
		text    string
	}{
		{apiSection, "(operations.md#diagnose-configuration-budget-rejection)"},
		{apiSection, "(security.md#budget-rejection-and-data-removal)"},
		{operationsSection, "(api-reference.md#configuration-budget-rejection)"},
		{operationsSection, "(security.md#budget-rejection-and-data-removal)"},
		{securitySection, "(api-reference.md#configuration-budget-rejection)"},
		{securitySection, "(operations.md#diagnose-configuration-budget-rejection)"},
	} {
		if !strings.Contains(link.section, link.text) {
			t.Fatalf("budget documentation link %q is missing", link.text)
		}
	}
	for _, target := range []string{"docs/api-reference.md", "docs/operations.md", "docs/security.md"} {
		if _, err := os.Stat(filepath.Join(root, target)); err != nil {
			t.Fatalf("documentation target %s is not available: %v", target, err)
		}
	}

	// Keep the public status assertion tied to the same condition constants as
	// the production composer, rather than duplicating a second reason list.
	if runtimeConditionStatus(composed, status.ConditionAccepted) != metav1.ConditionFalse || runtimeConditionReason(composed, status.ConditionReady) != status.ReasonConfigurationBudgetExceeded {
		t.Fatal("composed budget rejection no longer matches the documented terminal status")
	}
	t.Log("MODULE_INTEGRATION=configuration-budget-documentation STATUS=passed")
	t.Log("API_CONTRACT=configuration-budget-documentation STATUS=passed")
}

func documentationSection(document, startHeading, endHeading string) string {
	start := strings.Index(document, startHeading)
	if start < 0 {
		return ""
	}
	section := document[start:]
	if end := strings.Index(section[len(startHeading):], endHeading); end >= 0 {
		section = section[:len(startHeading)+end]
	}
	return section
}

func documentationCodeBlocks(section string) []string {
	parts := strings.Split(section, "```")
	blocks := make([]string, 0, len(parts)/2)
	for index := 1; index < len(parts); index += 2 {
		blocks = append(blocks, parts[index])
	}
	return blocks
}

func runtimeConditionStatus(candidate v1alpha1.KubeseerStatus, conditionType string) metav1.ConditionStatus {
	for _, condition := range candidate.Conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return metav1.ConditionUnknown
}

func runtimeConditionReason(candidate v1alpha1.KubeseerStatus, conditionType string) string {
	for _, condition := range candidate.Conditions {
		if condition.Type == conditionType {
			return condition.Reason
		}
	}
	return ""
}
