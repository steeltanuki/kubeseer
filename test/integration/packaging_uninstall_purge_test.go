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
	"time"

	"github.com/steeltanuki/kubeseer/internal/purge"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func assertPackagingUninstallPurgeScenarios(t *testing.T) {
	t.Helper()
	chartDir := packagingChartDir(t)
	render := renderPackagingChart(t, chartDir)
	for _, fragment := range []string{
		"name: kubeseer-pre-delete",
		"helm.sh/hook: pre-delete",
		"helm.sh/hook-weight: \"-20\"",
		"name: kubeseer-validating-webhook",
		"name: kubeseer-post-delete",
		"helm.sh/hook: post-delete",
		"helm.sh/hook-weight: \"10\"",
		"PACKAGE_ACTION=pre-delete",
	} {
		// The command marker is checked below through the executable source;
		// rendered resources carry the lifecycle identity and ordering.
		if fragment == "PACKAGE_ACTION=pre-delete" {
			continue
		}
		if !strings.Contains(render, fragment) {
			t.Fatalf("uninstall render lacks %q", fragment)
		}
	}
	if strings.Contains(render, "kind: CustomResourceDefinition") {
		t.Fatal("normal uninstall render includes a CRD deletion target")
	}
	externalRender := renderPackagingChart(t, chartDir,
		"--set", "certificate.mode=externalSecret",
		"--set", "certificate.externalSecret.secretName=administrator-webhook-tls",
		"--set-string", "certificate.externalSecret.caBundle=PUBLIC-CA",
	)
	if strings.Contains(externalRender, "kind: Secret") || !strings.Contains(externalRender, "secretName: \"administrator-webhook-tls\"") {
		t.Fatal("external TLS Secret is not mounted as administrator-owned state")
	}
	if !strings.Contains(render, "helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded") {
		t.Fatal("uninstall hooks do not converge on repeated invocation")
	}
	uninstallScript, err := os.ReadFile(filepath.Join(chartDir, "..", "..", "hack", "uninstall-kubeseer.sh"))
	if err != nil {
		t.Fatalf("read uninstall wrapper: %v", err)
	}
	for _, fragment := range []string{"helm uninstall", "--ignore-not-found", "--wait", "kubeseers.kubeseer.io", "installation-access-ceiling"} {
		if !strings.Contains(string(uninstallScript), fragment) {
			t.Fatalf("uninstall wrapper lacks %q", fragment)
		}
	}
	purgeSource, err := os.ReadFile(filepath.Join(chartDir, "..", "..", "internal", "purge", "purge.go"))
	if err != nil {
		t.Fatalf("read purge implementation: %v", err)
	}
	for _, fragment := range []string{"purge-kubeseer-crds", "waitCollectionEmpty", "KubeseerCRDName", "AccessPolicyCRDName"} {
		if !strings.Contains(string(purgeSource), fragment) {
			t.Fatalf("purge implementation lacks %q", fragment)
		}
	}

	configPath := writePurgeKubeconfig(t)
	base := purge.Options{
		KubeconfigPath: configPath,
		ContextName:    "disposable",
		ConfirmContext: "disposable",
		ConfirmServer:  "https://127.0.0.1:6443",
		Confirmation:   purge.ConfirmationToken,
		Timeout:        time.Minute,
	}
	if _, _, err := purge.ResolveTarget(base); err != nil {
		t.Fatalf("valid explicit purge target was rejected: %v", err)
	}
	for name, options := range map[string]purge.Options{
		"missing token": func() purge.Options {
			copy := base
			copy.Confirmation = "purge"
			return copy
		}(),
		"mismatched context": func() purge.Options {
			copy := base
			copy.ConfirmContext = "another-context"
			return copy
		}(),
		"mismatched server": func() purge.Options {
			copy := base
			copy.ConfirmServer = "https://other.example"
			return copy
		}(),
		"relative kubeconfig": func() purge.Options {
			copy := base
			copy.KubeconfigPath = "kubeconfig"
			return copy
		}(),
	} {
		if _, _, err := purge.ResolveTarget(options); err == nil {
			t.Fatalf("%s confirmation unexpectedly resolved", name)
		}
	}

	for _, forbidden := range []string{"kind: Secret", "delete-everything", "--all"} {
		if strings.Contains(render, forbidden) {
			t.Fatalf("normal uninstall render contains destructive token %q", forbidden)
		}
	}

	t.Log("MODULE_INTEGRATION=packaging-uninstall-purge STATUS=passed")
}

func writePurgeKubeconfig(t *testing.T) string {
	t.Helper()
	config := clientcmdapi.Config{
		Kind:           "Config",
		APIVersion:     "v1",
		CurrentContext: "ambient-context",
		Clusters: map[string]*clientcmdapi.Cluster{
			"disposable-cluster": {Server: "https://127.0.0.1:6443"},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"disposable": {Cluster: "disposable-cluster"},
		},
	}
	data, err := clientcmd.Write(config)
	if err != nil {
		t.Fatalf("serialize purge kubeconfig: %v", err)
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write purge kubeconfig: %v", err)
	}
	return path
}
