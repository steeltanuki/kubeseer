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
readonly HARNESS="$ROOT_DIR/hack/e2e-harness.sh"
readonly ORIGINAL_PATH="$PATH"

mkdir -p -- "$TEST_DIR"
readonly FIXTURE_DIR="$(mktemp -d "$TEST_DIR/e2e-harness-acceptance.XXXXXX")"

cleanup() {
	rm -rf -- "$FIXTURE_DIR"
	rmdir --ignore-fail-on-non-empty "$TEST_DIR" 2>/dev/null || true
}
trap cleanup EXIT

mkdir -p -- "$FIXTURE_DIR/bin" "$FIXTURE_DIR/scenario" "$FIXTURE_DIR/missing-bin"
readonly TRACE_PATH="$FIXTURE_DIR/tools.trace"
readonly NEGATIVE_TRACE_PATH="$FIXTURE_DIR/negative-tools.trace"

cat > "$FIXTURE_DIR/bin/kind" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'kind provider=%s kubeconfig-env=%s args=%s\n' \
	"${KIND_EXPERIMENTAL_PROVIDER-<unset>}" \
	"${KUBECONFIG-<unset>}" "$*" >> "$TRACE_FILE"

if [[ "${1:-}" == version ]]; then
	exit 0
fi
if [[ "${1:-}" != create && "${1:-}" != delete ]] || [[ "${2:-}" != cluster ]]; then
	exit 64
fi

kubeconfig=''
for ((index = 1; index <= $#; index++)); do
	argument="${!index}"
	if [[ "$argument" == --kubeconfig ]]; then
		next_index=$((index + 1))
		kubeconfig="${!next_index}"
	fi
done
if [[ -z "$kubeconfig" ]]; then
	exit 65
fi

if [[ "${1:-}" == create ]]; then
	printf '%s\n' 'stub kubeconfig' > "$kubeconfig"
fi
EOF

cat > "$FIXTURE_DIR/bin/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'podman kubeconfig-env=%s args=%s\n' \
	"${KUBECONFIG-<unset>}" "$*" >> "$TRACE_FILE"
[[ "${1:-}" == info ]]
EOF

cat > "$FIXTURE_DIR/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'kubectl kubeconfig-env=%s args=%s\n' \
	"${KUBECONFIG-<unset>}" "$*" >> "$TRACE_FILE"
[[ "${1:-}" == version ]]
EOF

cat > "$FIXTURE_DIR/missing-bin/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'podman unavailable args=%s\n' "$*" >> "$TRACE_FILE"
exit 42
EOF

chmod +x \
	"$FIXTURE_DIR/bin/kind" \
	"$FIXTURE_DIR/bin/podman" \
	"$FIXTURE_DIR/bin/kubectl" \
	"$FIXTURE_DIR/missing-bin/podman"
ln -s "$FIXTURE_DIR/bin/kind" "$FIXTURE_DIR/missing-bin/kind"
ln -s "$FIXTURE_DIR/bin/kubectl" "$FIXTURE_DIR/missing-bin/kubectl"

cat > "$FIXTURE_DIR/scenario/e2e_test.go" <<'EOF'
package e2eacceptance

import (
	"os"
	"testing"
)

func TestEndToEnd(t *testing.T) {
	expected := os.Getenv("KUBESEER_E2E_KUBECONFIG")
	if expected == "" || os.Getenv("KUBECONFIG") != expected {
		t.Fatalf("harness did not pass its owned kubeconfig explicitly: got %q, want %q", os.Getenv("KUBECONFIG"), expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("owned kubeconfig is unavailable: %v", err)
	}
}
EOF

assert_contains() {
	local output="$1"
	local expected="$2"
	if [[ "$output" != *"$expected"* ]]; then
		printf 'expected output to contain %q, got:\n%s\n' "$expected" "$output" >&2
		exit 1
	fi
}

assert_not_contains() {
	local output="$1"
	local unexpected="$2"
	if [[ "$output" == *"$unexpected"* ]]; then
		printf 'expected output not to contain %q, got:\n%s\n' "$unexpected" "$output" >&2
		exit 1
	fi
}

run_with_status() {
	local expected_status="$1"
	shift
	local output
	local status
	set +e
	output="$("$@" 2>&1)"
	status=$?
	set -e
	if [[ "$status" != "$expected_status" ]]; then
		printf 'expected exit %s, got %s from %q:\n%s\n' "$expected_status" "$status" "$1" "$output" >&2
		exit 1
	fi
	printf '%s' "$output"
}

readonly PACKAGE_PATH="./${FIXTURE_DIR#"$ROOT_DIR"}/scenario"
readonly AMBIENT_KUBECONFIG="$FIXTURE_DIR/ambient-kubeconfig-do-not-use"
printf '%s\n' 'ambient kubeconfig must not be selected' > "$AMBIENT_KUBECONFIG"

run_output="$(
	PATH="$FIXTURE_DIR/bin:$ORIGINAL_PATH" \
	TRACE_FILE="$TRACE_PATH" \
	KUBECONFIG="$AMBIENT_KUBECONFIG" \
	USE_EXISTING_CLUSTER=true \
	EXPECTED_KUBECONFIG='' \
	"$HARNESS" "$PACKAGE_PATH" '^TestEndToEnd$' 2>&1
)"
assert_contains "$run_output" 'TEST_LAYER=end-to-end STATUS=passed'

trace_output="$(<"$TRACE_PATH")"
assert_contains "$trace_output" 'kind provider=podman kubeconfig-env=<unset> args=create cluster --name kubeseer-e2e --kubeconfig /tmp/kubeseer-e2e.'
assert_contains "$trace_output" 'kind provider=podman kubeconfig-env=<unset> args=delete cluster --name kubeseer-e2e --kubeconfig /tmp/kubeseer-e2e.'
assert_not_contains "$trace_output" "$AMBIENT_KUBECONFIG"
assert_not_contains "$trace_output" 'prune'
assert_not_contains "$trace_output" 'network rm'

create_line="$(grep -m 1 '^kind .* args=create cluster ' "$TRACE_PATH" || true)"
delete_line="$(grep -m 1 '^kind .* args=delete cluster ' "$TRACE_PATH" || true)"
if [[ -z "$create_line" || -z "$delete_line" ]]; then
	printf 'kind create/delete trace is incomplete:\n%s\n' "$trace_output" >&2
	exit 1
fi
create_kubeconfig="${create_line##*--kubeconfig }"
delete_kubeconfig="${delete_line##*--kubeconfig }"
if [[ "$create_kubeconfig" != "$delete_kubeconfig" ]]; then
	printf 'kind create/delete kubeconfig targets differ: %q != %q\n' \
		"$create_kubeconfig" "$delete_kubeconfig" >&2
	exit 1
fi

negative_output="$(run_with_status 1 \
	env PATH="$FIXTURE_DIR/missing-bin:$ORIGINAL_PATH" \
	TRACE_FILE="$NEGATIVE_TRACE_PATH" \
	KUBECONFIG="$AMBIENT_KUBECONFIG" \
	USE_EXISTING_CLUSTER=true \
	EXPECTED_KUBECONFIG='' \
	"$HARNESS" "$PACKAGE_PATH" '^TestEndToEnd$')"
assert_contains "$negative_output" 'E2E prerequisite failed: podman info'
negative_trace_output="$(<"$NEGATIVE_TRACE_PATH")"
assert_not_contains "$negative_trace_output" 'create cluster'
assert_not_contains "$negative_trace_output" 'delete cluster'

printf '%s\n' 'E2E harness acceptance passed'
