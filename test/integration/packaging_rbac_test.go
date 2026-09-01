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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func assertPackagingRBACScenarios(t *testing.T) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	chartDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "charts", "kubeseer")

	render := func(extra ...string) (string, error) {
		args := []string{"template", "kubeseer", chartDir, "--namespace", "team-a", "--kube-version", "1.35.6"}
		args = append(args, extra...)
		return func() (string, error) {
			output, err := exec.Command("helm", args...).CombinedOutput()
			return string(output), err
		}()
	}

	defaultRender, err := render()
	if err != nil {
		t.Fatalf("render default RBAC: %v\n%s", err, defaultRender)
	}
	for _, fragment := range []string{
		"name: kubeseer-operator",
		"resources: [\"kubeseers\"]",
		"resources: [\"leases\"]",
		"resources: [\"events\"]",
	} {
		if !strings.Contains(defaultRender, fragment) {
			t.Fatalf("default RBAC lacks %q", fragment)
		}
	}
	if strings.Contains(defaultRender, "resources: [\"secrets\"]") || strings.Contains(defaultRender, "resources: [\"*\"]") {
		t.Fatal("default RBAC grants Secret or wildcard resource access")
	}
	if strings.Contains(defaultRender, "verbs: [\"*\"]") {
		t.Fatal("default RBAC grants wildcard verbs")
	}

	namespacedRender, err := render("--set-json", `rbac.observed.namespaced=[{"namespace":"team-a","apiGroups":["apps"],"resources":["deployments"],"verbs":["get","list","watch"]}]`)
	if err != nil {
		t.Fatalf("render namespaced observed RBAC: %v\n%s", err, namespacedRender)
	}
	if !strings.Contains(namespacedRender, "kind: Role\nmetadata:\n  name: kubeseer-observed-0\n  namespace: team-a") || !strings.Contains(namespacedRender, "resources:\n      - deployments") {
		t.Fatal("namespaced observed RBAC was not scoped to the declared namespace/resource")
	}

	clusterArgs := `rbac.observed.clusterScoped=[{"apiGroups":[""],"resources":["nodes"],"verbs":["get"]}]`
	clusterRender, err := render("--set-json", clusterArgs)
	if err == nil || !strings.Contains(clusterRender, "allowClusterScoped") {
		t.Fatalf("cluster-scoped RBAC without logical opt-in was accepted: err=%v output=%s", err, clusterRender)
	}
	clusterRender, err = render("--set-json", clusterArgs, "--set", "accessPolicy.allowClusterScoped=true")
	if err != nil {
		t.Fatalf("render explicitly allowed cluster-scoped RBAC: %v\n%s", err, clusterRender)
	}
	if !strings.Contains(clusterRender, "name: kubeseer-observed-cluster") || !strings.Contains(clusterRender, "resources:\n      - nodes") {
		t.Fatal("explicit cluster-scoped RBAC was not rendered")
	}

	authorRender, err := render("--set-json", `rbac.authors.namespaces=["team-a"]`, "--set-json", `rbac.authors.subjects=[{"kind":"User","name":"alice"}]`)
	if err != nil {
		t.Fatalf("render author RBAC: %v\n%s", err, authorRender)
	}
	if !strings.Contains(authorRender, "name: kubeseer-author") || !strings.Contains(authorRender, "name: alice") {
		t.Fatal("author RBAC was not rendered independently")
	}
	authorSection := authorRender
	if start := strings.Index(authorSection, "# Source: kubeseer/templates/rbac-authors.yaml"); start >= 0 {
		authorSection = authorSection[start:]
		if end := strings.Index(authorSection, "\n---\n"); end >= 0 {
			authorSection = authorSection[:end]
		}
	}
	if strings.Contains(authorSection, "resources: [\"secrets\"]") || strings.Contains(authorSection, "resources: [\"deployments\"]") {
		t.Fatal("author RBAC includes observed-resource or Secret access")
	}

	t.Log("MODULE_INTEGRATION=packaging-rbac STATUS=passed")
}
