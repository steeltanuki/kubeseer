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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func assertPackagingWorkloadScenarios(t *testing.T) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	chartDir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "charts", "kubeseer")
	command := exec.Command("helm", "template", "kubeseer", chartDir,
		"--namespace", "team-a", "--kube-version", "1.35.6", "--include-crds")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("render workload chart: %v\n%s", err, output)
	}
	rendered := string(output)
	for _, fragment := range []string{
		"kind: Deployment",
		"namespace: team-a",
		"image: \"ghcr.io/steeltanuki/kubeseer:" + packagingAppVersion(t, chartDir) + "\"",
		"maxUnavailable: 0",
		"maxSurge: 1",
		"readOnlyRootFilesystem: true",
		"allowPrivilegeEscalation: false",
		"hostNetwork: false",
		"hostPID: false",
		"hostIPC: false",
		"type: RuntimeDefault",
		"- ALL",
		"path: /readyz",
		"path: /healthz",
		"kind: Service",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("rendered workload lacks %q", fragment)
		}
	}
	if strings.Contains(rendered, "hostNetwork: true") || strings.Contains(rendered, "privileged: true") {
		t.Fatal("rendered workload enables host networking or privileged mode")
	}

	disabledMetrics := exec.Command("helm", "template", "kubeseer", chartDir,
		"--namespace", "team-a", "--kube-version", "1.35.6", "--set", "metrics.service.enabled=false")
	disabledOutput, err := disabledMetrics.CombinedOutput()
	if err != nil {
		t.Fatalf("render workload with metrics disabled: %v\n%s", err, disabledOutput)
	}
	if strings.Contains(string(disabledOutput), "name: kubeseer-metrics") {
		t.Fatal("metrics Service was rendered while disabled")
	}

	t.Log("MODULE_INTEGRATION=packaging-workload STATUS=passed")
}

// Read the expected identity from the chart source, independently of rendering.
func packagingAppVersion(t *testing.T, chartDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		t.Fatalf("read chart metadata: %v", err)
	}
	var metadata struct {
		Version    string `json:"version"`
		AppVersion string `json:"appVersion"`
	}
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("decode chart metadata: %v", err)
	}
	if metadata.AppVersion == "" || metadata.Version != metadata.AppVersion {
		t.Fatalf("chart version %q and appVersion %q must be nonempty and equal", metadata.Version, metadata.AppVersion)
	}
	return metadata.AppVersion
}
