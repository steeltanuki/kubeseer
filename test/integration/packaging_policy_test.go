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

func assertPackagingPolicyScenarios(t *testing.T) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	chartDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "charts", "kubeseer")
	managed := exec.Command("helm", "template", "kubeseer", chartDir,
		"--namespace", "team-a", "--kube-version", "1.35.6",
		"--set", "accessPolicy.explicitNamespaces[0]=team-a",
		"--set", "accessPolicy.allowedResources[0].apiGroups[0]=apps",
		"--set", "accessPolicy.allowedResources[0].kinds[0]=Deployment")
	managedOutput, err := managed.CombinedOutput()
	if err != nil {
		t.Fatalf("render managed policy hooks: %v\n%s", err, managedOutput)
	}
	managedText := string(managedOutput)
	for _, fragment := range []string{
		"name: kubeseer-policy-bootstrap",
		"name: kubeseer-policy",
		"helm.sh/hook: pre-install,pre-upgrade,pre-rollback",
		"helm.sh/hook-weight: \"-20\"",
		"helm.sh/hook-weight: \"-10\"",
		"installation-access-ceiling",
		`"mode":"Explicit"`,
		"team-a",
		"Deployment",
	} {
		if !strings.Contains(managedText, fragment) {
			t.Fatalf("managed policy render lacks %q", fragment)
		}
	}
	if strings.Contains(managedText, `"allowClusterScoped":true`) {
		t.Fatal("managed policy unexpectedly enables cluster-scoped observation")
	}

	external := exec.Command("helm", "template", "kubeseer", chartDir,
		"--namespace", "team-a", "--kube-version", "1.35.6",
		"--set", "accessPolicy.mode=external")
	externalOutput, err := external.CombinedOutput()
	if err != nil {
		t.Fatalf("render external policy mode: %v\n%s", err, externalOutput)
	}
	externalText := string(externalOutput)
	if strings.Contains(externalText, "policy-bootstrap") || strings.Contains(externalText, "package\n            - policy") {
		t.Fatal("external policy mode rendered a policy payload or write hook")
	}

	t.Log("MODULE_INTEGRATION=packaging-policy-bootstrap STATUS=passed")
}
