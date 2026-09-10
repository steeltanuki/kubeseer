package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/internal/observability"
)

func assertWatchStartupDocumentation(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..")
	paths := map[string]string{
		"architecture": filepath.Join(root, "docs", "concepts-and-architecture.md"),
		"operations":   filepath.Join(root, "docs", "operations.md"),
		"security":     filepath.Join(root, "docs", "security.md"),
	}
	docs := make(map[string]string, len(paths))
	for name, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s guide: %v", name, err)
		}
		docs[name] = string(contents)
	}

	architecture := documentationSection(docs["architecture"], "## Watch lifecycle and observation gaps", "## Failure isolation")
	operations := documentationSection(docs["operations"], "## Diagnose stalled WATCH startup and recovery", "## Kubernetes Events")
	security := documentationSection(docs["security"], "## Watch authority and transport lifetime", "## Admission and runtime revalidation")
	sections := map[string]string{
		"architecture": architecture,
		"operations":   operations,
		"security":     security,
	}
	for name, section := range sections {
		if strings.TrimSpace(section) == "" {
			t.Fatalf("%s watch-lifecycle section is missing", name)
		}
	}

	for _, phrase := range []string{
		"EvaluationTimeout",
		"RouteRegistry",
		"WatchAddress",
		"coalescing ingress",
		"fresh `LIST`",
		"periodic safety interval",
		"context-ignoring",
	} {
		if !strings.Contains(architecture, phrase) {
			t.Fatalf("architecture guide omitted startup contract %q", phrase)
		}
	}
	for _, phrase := range []string{
		"jsonpath",
		"metadata.generation",
		"status.observedGeneration",
		"kubeseer_source_watch_restarts_total",
		"auth can-i",
		"installation-access-ceiling",
		"policy or identity revocation",
		"Kubernetes RBAC",
	} {
		if !strings.Contains(operations, phrase) {
			t.Fatalf("operations guide omitted safe diagnosis guidance %q", phrase)
		}
	}
	for _, phrase := range []string{
		"exact-target only",
		"Every WATCH attempt",
		"current owners",
		"serial with bounded backoff",
		"last current owner",
		"Late responses",
		"cannot be forcibly terminated",
	} {
		if !strings.Contains(security, phrase) {
			t.Fatalf("security guide omitted watch authority rule %q", phrase)
		}
	}

	for _, reason := range []observability.Reason{
		observability.ReasonEvaluationTimedOut,
		observability.ReasonReadInterrupted,
		observability.ReasonReadUnavailable,
		observability.ReasonListExpired,
		observability.ReasonAuthorizationDenied,
		observability.ReasonAuthorizationStale,
		observability.ReasonStaleLease,
		observability.ReasonReadForbidden,
	} {
		if !strings.Contains(operations, string(reason)) {
			t.Fatalf("operations guide omitted production reason %q", reason)
		}
	}
	for _, event := range []observability.EventCode{
		observability.EventSourceWatchStopped,
		observability.EventSourceWatchRestarted,
	} {
		if !strings.Contains(operations, string(event)) {
			t.Fatalf("operations guide omitted production event %q", event)
		}
	}

	blocks := documentationCodeBlocks(operations)
	if len(blocks) == 0 {
		t.Fatal("operations guide omitted a safe diagnostic command projection")
	}
	joinedBlocks := strings.Join(blocks, "\n")
	for _, phrase := range []string{"metadata.generation", "status.observedGeneration", "status.conditions", "reason", "kubeseer_source_watch_restarts_total"} {
		if !strings.Contains(joinedBlocks, phrase) {
			t.Fatalf("safe diagnostic commands omitted %q", phrase)
		}
	}
	for _, block := range blocks {
		lower := strings.ToLower(block)
		for _, forbidden := range []string{"payload", "selector", "field value", "capability", "{.spec", "secret-sentinel"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("safe diagnostic command contains forbidden material %q", forbidden)
			}
		}
	}

	for _, link := range []struct {
		section string
		text    string
	}{
		{architecture, "(operations.md#diagnose-stalled-watch-startup-and-recovery)"},
		{architecture, "(security.md#watch-authority-and-transport-lifetime)"},
		{operations, "(concepts-and-architecture.md#watch-lifecycle-and-observation-gaps)"},
		{operations, "(security.md#watch-authority-and-transport-lifetime)"},
		{security, "(operations.md#diagnose-stalled-watch-startup-and-recovery)"},
		{security, "(concepts-and-architecture.md#watch-lifecycle-and-observation-gaps)"},
	} {
		if !strings.Contains(link.section, link.text) {
			t.Fatalf("watch documentation link %q is missing", link.text)
		}
	}
	for _, target := range paths {
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("documentation target %s is not available: %v", target, err)
		}
	}

	t.Log("MODULE_INTEGRATION=watch-startup-documentation STATUS=passed")
	t.Log("API_CONTRACT=watch-startup-documentation STATUS=passed")
}
