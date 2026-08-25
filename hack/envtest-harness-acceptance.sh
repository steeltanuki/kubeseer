#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly TEST_DIR="$ROOT_DIR/test"

if [[ -z "${KUBEBUILDER_ASSETS:-}" ]]; then
	echo "KUBEBUILDER_ASSETS is required; run KUBEBUILDER_ASSETS=\$(./hack/envtest-assets.sh 1.35.6) ./hack/envtest-harness-acceptance.sh" >&2
	exit 1
fi

mkdir -p -- "$TEST_DIR"
readonly FIXTURE_DIR="$(mktemp -d "$TEST_DIR/envtest-harness-acceptance.XXXXXX")"

cleanup() {
	rm -rf -- "$FIXTURE_DIR"
	rmdir --ignore-fail-on-non-empty "$TEST_DIR" 2>/dev/null || true
}
trap cleanup EXIT

cat > "$FIXTURE_DIR/harness_test.go" <<'EOF'
package harnessacceptance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harness "github.com/steeltanuki/kubeseer/test/envtest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHarnessLifecycle(t *testing.T) {
	t.Setenv("USE_EXISTING_CLUSTER", "true")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "ambient-kubeconfig-do-not-use"))

	testHarness, err := harness.New(t, harness.Options{})
	if err != nil {
		t.Fatalf("create harness: %v", err)
	}
	if _, err := testHarness.Start(); err != nil {
		t.Fatalf("start harness: %v", err)
	}

	scope := testHarness.Scope()
	if scope.Prefix == "" || scope.Namespace == "" || scope.TempDir == "" {
		t.Fatalf("incomplete ownership scope: %#v", scope)
	}
	if !strings.HasPrefix(scope.Namespace, scope.Prefix+"-") {
		t.Fatalf("namespace is not owned by the scope: %#v", scope)
	}
	if _, err := os.Stat(scope.TempDir); err != nil {
		t.Fatalf("scope temp directory is unavailable: %v", err)
	}

	clients, err := testHarness.Clients()
	if err != nil {
		t.Fatalf("construct loopback clients: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), harness.DefaultRequestTimeout)
	defer cancel()
	if _, err := clients.Core.CoreV1().Namespaces().Get(ctx, scope.Namespace, metav1.GetOptions{}); err != nil {
		t.Fatalf("get owned namespace: %v", err)
	}

	cleanupCalled := false
	testHarness.AddCleanup("acceptance marker", func(context.Context) error {
		cleanupCalled = true
		return nil
	})
	if err := testHarness.Close(); err != nil {
		t.Fatalf("close harness: %v", err)
	}
	if !cleanupCalled {
		t.Fatal("explicit cleanup callback was not called")
	}
}
EOF

package_path="./${FIXTURE_DIR#"$ROOT_DIR"/}"
GOCACHE="${GOCACHE:-/tmp/kubeseer-gocache}" go test -count=1 "$package_path"
printf '%s\n' 'Envtest harness acceptance passed'
