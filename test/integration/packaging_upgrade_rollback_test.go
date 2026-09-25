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
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/internal/managerapp"
)

func assertPackagingUpgradeRollbackScenarios(t *testing.T) {
	t.Helper()
	chartDir := packagingChartDir(t)
	defaultRender := renderPackagingChart(t, chartDir)
	appVersion := packagingAppVersion(t, chartDir)

	for _, fragment := range []string{
		"progressDeadlineSeconds: 600",
		"maxUnavailable: 0",
		"maxSurge: 1",
		"revisionHistoryLimit: 5",
		"helm.sh/hook: pre-install,pre-upgrade,pre-rollback",
		"helm.sh/hook-weight: \"-35\"",
		"helm.sh/hook-weight: \"-10\"",
		"helm.sh/hook: post-install,post-upgrade,post-rollback",
		"helm.sh/hook-weight: \"5\"",
		"helm.sh/hook-weight: \"10\"",
		"--expected-version=" + appVersion,
		"--expected-image=ghcr.io/steeltanuki/kubeseer:" + appVersion,
		"--expected-kubeseer-crd-storage-version=v1alpha1",
		"--expected-access-policy-crd-storage-version=v1alpha1",
		"--timeout=180s",
	} {
		if !strings.Contains(defaultRender, fragment) {
			t.Fatalf("upgrade/rollback render lacks %q", fragment)
		}
	}

	if strings.Count(defaultRender, "kind: Deployment") != 1 {
		t.Fatalf("rendered %d manager Deployments, want one", strings.Count(defaultRender, "kind: Deployment"))
	}
	if strings.Contains(defaultRender, "helm.sh/hook: post-install,post-upgrade,post-rollback\n    helm.sh/hook-weight: \"-") {
		t.Fatal("readiness hook is ordered before the normal release resources")
	}

	changedImage := renderPackagingChart(t, chartDir, "--set", "image.tag=0.2.0")
	if !strings.Contains(changedImage, "image: \"ghcr.io/steeltanuki/kubeseer:0.2.0\"") || !strings.Contains(changedImage, "--expected-image=ghcr.io/steeltanuki/kubeseer:0.2.0") {
		t.Fatal("image upgrade did not propagate the immutable image identity to rollout verification")
	}

	externalRender := renderPackagingChart(t, chartDir,
		"--set", "certificate.mode=externalSecret",
		"--set", "certificate.externalSecret.secretName=administrator-webhook-tls",
		"--set-string", "certificate.externalSecret.caBundle=PUBLIC-CA",
	)
	if !strings.Contains(externalRender, "secretName: \"administrator-webhook-tls\"") {
		t.Fatal("external Secret identity was not retained in the verification hook")
	}
	if strings.Contains(externalRender, "kind: Secret") {
		t.Fatal("external Secret mode rendered an owned TLS Secret during upgrade")
	}

	for _, action := range []string{"preflight", "policy", "verify", "pre-delete", "post-delete"} {
		if _, err := managerapp.ParsePackageAction(action); err != nil {
			t.Fatalf("lifecycle action %q is not accepted: %v", action, err)
		}
	}

	t.Log("MODULE_INTEGRATION=packaging-upgrade-rollback STATUS=passed")
}
