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
readonly HARNESS="$ROOT_DIR/hack/e2e-harness.sh"
readonly ORIGINAL_PATH="$PATH"
mkdir -p -- "$ROOT_DIR/test"
readonly FIXTURE_DIR="$(mktemp -d "$ROOT_DIR/test/e2e-harness-acceptance.XXXXXX")"
readonly PACKAGE_DIR="$FIXTURE_DIR/scenario"
readonly BIN_DIR="$FIXTURE_DIR/bin"
readonly TRACE_PATH="$FIXTURE_DIR/tools.trace"
readonly AMBIENT_KUBECONFIG="$FIXTURE_DIR/ambient-kubeconfig-do-not-use"

cleanup() {
	if [[ -d "$FIXTURE_DIR" ]]; then
		chmod -R u+w "$FIXTURE_DIR" 2>/dev/null || true
		rm -rf -- "$FIXTURE_DIR"
	fi
}
trap cleanup EXIT

mkdir -p -- "$PACKAGE_DIR" "$BIN_DIR"
printf '%s\n' 'ambient kubeconfig must never be selected' >"$AMBIENT_KUBECONFIG"

cat >"$PACKAGE_DIR/e2e_test.go" <<'EOF'
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
	if os.Getenv("AWS_SECRET_ACCESS_KEY") != "" || os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		t.Fatal("ambient cloud credentials reached the test process")
	}
}
EOF

cat >"$BIN_DIR/kind" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'kind provider=%s kubeconfig-env=%s aws=%s args=%s\n' \
	"${KIND_EXPERIMENTAL_PROVIDER-<unset>}" "${KUBECONFIG-<unset>}" \
	"${AWS_SECRET_ACCESS_KEY-<unset>}" "$*" >>"$TRACE_FILE"

if [[ "${1:-}" == version ]]; then
	printf '%s\n' 'kind v0.30.0'
	exit 0
fi
if [[ "${1:-}" == get && "${2:-}" == nodes ]]; then
	printf '%s\n' 'kubeseer-e2e-control-plane' 'kubeseer-e2e-worker'
	exit 0
fi
if [[ "${KIND_FAIL_CREATE-}" == 1 && "${1:-}" == create ]]; then
	exit 41
fi
if [[ "${KIND_FAIL_DELETE-}" == 1 && "${1:-}" == delete ]]; then
	exit 42
fi
if [[ "${KIND_FAIL_LOAD-}" == 1 && "${1:-}" == load ]]; then
	exit 43
fi
if [[ "${1:-}" != create && "${1:-}" != delete && "${1:-}" != load ]]; then
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

if [[ "${1:-}" == create ]]; then
	[[ -n "$kubeconfig" ]] || exit 65
	printf '%s\n' 'stub kubeconfig' >"$kubeconfig"
fi
EOF

cat >"$BIN_DIR/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'podman kubeconfig-env=%s aws=%s args=%s\n' \
	"${KUBECONFIG-<unset>}" "${AWS_SECRET_ACCESS_KEY-<unset>}" "$*" >>"$TRACE_FILE"
if [[ "${1:-}" == info ]]; then
	exit "${PODMAN_FAIL_INFO:-0}"
fi
if [[ "${PODMAN_FAIL_BUILD-}" == 1 && "${1:-}" == build ]]; then
	exit 44
fi
if [[ "${PODMAN_FAIL_INSPECT-}" == 1 && "${1:-}" == image && "${2:-}" == inspect ]]; then
	exit 45
fi
if [[ "${1:-}" == save ]]; then
	output=''
	for ((index = 1; index <= $#; index++)); do
		if [[ "${!index}" == --output ]]; then
			next_index=$((index + 1))
			output="${!next_index}"
		fi
	done
	[[ -n "$output" ]] || exit 46
	printf '%s\n' 'stub OCI image archive' >"$output"
	exit 0
fi
if [[ "${1:-}" == image && "${2:-}" == inspect ]]; then
	printf '%s\n' 'sha256:e2e-immutable-image'
fi
EOF

cat >"$BIN_DIR/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'kubectl kubeconfig-env=%s aws=%s args=%s\n' \
	"${KUBECONFIG-<unset>}" "${AWS_SECRET_ACCESS_KEY-<unset>}" "$*" >>"$TRACE_FILE"

while [[ "${1:-}" == --kubeconfig || "${1:-}" == --context ]]; do
	shift 2
done

if [[ "${KUBESEER_FAIL_READINESS-}" == 1 && "${1:-}" == wait ]]; then
	exit 46
fi
if [[ "${1:-}" == version ]]; then
	printf '%s\n' 'Client Version: v1.35.6'
	exit 0
fi
if [[ "${1:-}" == port-forward ]]; then
	while :; do sleep 1; done
fi
if [[ "${1:-}" == get ]]; then
	joined="$*"
	if [[ "$joined" == *validatingwebhookconfiguration* && "$joined" == *jsonpath* ]]; then
		if [[ "${KUBESEER_EMPTY_CA-}" == 1 ]]; then
			printf '\n\n'
		else
			printf '%s\n' 'RUN-SCOPED-CA-BUNDLE'
		fi
		exit 0
	fi
	if [[ "$joined" == *kubeseeraccesspolicy* ]]; then
		printf '%s\n' 'kubeseeraccesspolicy.kubeseer.io/installation-access-ceiling'
	fi
	if [[ "$joined" == *nodes* || "$joined" == *deployment* || "$joined" == *events* || "$joined" == *endpoints* ]]; then
		printf '%s\n' 'stub'
	fi
	exit 0
fi
if [[ "${1:-}" == apply ]]; then
	if [[ "${KUBESEER_FAIL_INSTALL-}" == 1 && "$*" == *cert-manager.yaml* ]]; then
		exit 47
	fi
	if [[ "$*" == *'--dry-run=server'* && "$*" == *admission-invalid.yaml* ]]; then
		exit 48
	fi
	exit 0
fi
if [[ "${1:-}" == wait ]]; then
	exit 0
fi
exit 0
EOF

cat >"$BIN_DIR/helm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'helm kubeconfig-env=%s aws=%s args=%s\n' \
	"${KUBECONFIG-<unset>}" "${AWS_SECRET_ACCESS_KEY-<unset>}" "$*" >>"$TRACE_FILE"
while [[ "${1:-}" == --kubeconfig || "${1:-}" == --kube-context ]]; do
	shift 2
done
if [[ "${1:-}" == version ]]; then
	printf '%s\n' 'v3.17.0+stub'
	exit 0
fi
if [[ "${HELM_FAIL_INSTALL-}" == 1 && "${1:-}" == upgrade ]]; then
	exit 49
fi
exit 0
EOF

cat >"$BIN_DIR/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${TRACE_FILE:?TRACE_FILE is required}"
printf 'curl aws=%s args=%s\n' "${AWS_SECRET_ACCESS_KEY-<unset>}" "$*" >>"$TRACE_FILE"
if [[ "${CURL_FAIL-}" == 1 && "$*" == *github.com* ]]; then
	exit 50
fi
if [[ "${1:-}" == --version ]]; then
	printf '%s\n' 'curl 8.0.0 (stub)'
	exit 0
fi
output=''
for ((index = 1; index <= $#; index++)); do
	if [[ "${!index}" == --output ]]; then
		next_index=$((index + 1))
		output="${!next_index}"
	fi
done
if [[ -n "$output" ]]; then
	printf '%s\n' '# pinned cert-manager acceptance manifest' >"$output"
elif [[ "$*" == *'/readyz'* ]]; then
	printf '%s\n' 'ok'
elif [[ "$*" == *'/metrics'* ]]; then
	printf '%s\n' '# HELP kubeseer_reconciliations_total reconciliations' 'kubeseer_reconciliations_total 1'
fi
EOF

chmod +x "$BIN_DIR"/*

assert_contains() {
	local output="$1" expected="$2"
	if [[ "$output" != *"$expected"* ]]; then
		printf 'expected output to contain %q, got:\n%s\n' "$expected" "$output" >&2
		exit 1
	fi
}

assert_not_contains() {
	local output="$1" unexpected="$2"
	if [[ "$output" == *"$unexpected"* ]]; then
		printf 'expected output not to contain %q, got:\n%s\n' "$unexpected" "$output" >&2
		exit 1
	fi
}

run_with_status() {
	local expected_status="$1"
	shift
	local output status
	set +e
	output="$($@ 2>&1)"
	status=$?
	set -e
	if [[ "$status" != "$expected_status" ]]; then
		printf 'expected exit %s, got %s from %q:\n%s\n' "$expected_status" "$1" "$output" >&2
		exit 1
	fi
	printf '%s' "$output"
}

run_success() {
	: >"$TRACE_PATH"
	PATH="$BIN_DIR:$ORIGINAL_PATH" \
	TRACE_FILE="$TRACE_PATH" \
	KUBECONFIG="$AMBIENT_KUBECONFIG" \
	USE_EXISTING_CLUSTER=true \
	AWS_SECRET_ACCESS_KEY='ambient-cloud-secret' \
	GOOGLE_APPLICATION_CREDENTIALS="$AMBIENT_KUBECONFIG" \
	"$HARNESS" "$PACKAGE_DIR" '^TestEndToEnd$'
}

run_output="$(run_success 2>&1)"
assert_contains "$run_output" 'TEST_LAYER=end-to-end STATUS=passed'
trace_output="$(<"$TRACE_PATH")"
assert_contains "$trace_output" 'args=create cluster --name kubeseer-e2e-'
assert_contains "$trace_output" '--image kindest/node:v1.35.5'
assert_contains "$trace_output" 'args=save --format oci-archive --output '
assert_contains "$trace_output" 'args=cp '
assert_contains "$trace_output" 'args=exec '
assert_contains "$trace_output" 'ctr --namespace k8s.io images import'
assert_contains "$trace_output" 'args=delete cluster --name kubeseer-e2e-'
assert_contains "$trace_output" 'upgrade --install kubeseer'
assert_contains "$trace_output" 'cert-manager/releases/download/v1.18.2/cert-manager.yaml'
assert_not_contains "$trace_output" "$AMBIENT_KUBECONFIG"
assert_not_contains "$trace_output" 'ambient-cloud-secret'
assert_not_contains "$trace_output" 'prune'
assert_not_contains "$trace_output" 'network rm'

create_line="$(grep -m 1 '^kind .* args=create cluster ' "$TRACE_PATH" || true)"
delete_line="$(grep -m 1 '^kind .* args=delete cluster ' "$TRACE_PATH" || true)"
[[ -n "$create_line" && -n "$delete_line" ]] || { printf '%s\n' 'kind create/delete trace is incomplete' >&2; exit 1; }
create_name="$(sed -n 's/.*args=create cluster --name \([^ ]*\).*/\1/p' <<<"$create_line")"
delete_name="$(sed -n 's/.*args=delete cluster --name \([^ ]*\).*/\1/p' <<<"$delete_line")"
[[ "$create_name" == "$delete_name" && "$create_name" != kubeseer-e2e ]] || {
	printf 'cluster name ownership mismatch: %q != %q\n' "$create_name" "$delete_name" >&2
	exit 1
}

: >"$TRACE_PATH"
second_output="$(run_success 2>&1)"
assert_contains "$second_output" 'TEST_LAYER=end-to-end STATUS=passed'
second_name="$(sed -n 's/.*args=create cluster --name \([^ ]*\).*/\1/p' "$TRACE_PATH" | head -n 1)"
[[ "$create_name" != "$second_name" ]] || { printf '%s\n' 'cluster names are not run-unique' >&2; exit 1; }

negative_trace="$FIXTURE_DIR/negative.trace"
negative_output="$(run_with_status 1 env PATH="$BIN_DIR:$ORIGINAL_PATH" TRACE_FILE="$negative_trace" \
	KUBECONFIG="$AMBIENT_KUBECONFIG" USE_EXISTING_CLUSTER=true PODMAN_FAIL_INFO=1 \
	"$HARNESS" "$PACKAGE_DIR" '^TestEndToEnd$')"
assert_contains "$negative_output" 'E2E prerequisite failed: podman info'
assert_not_contains "$(<"$negative_trace")" 'args=create cluster'

for failure_env in \
	'KIND_FAIL_CREATE=1' \
	'PODMAN_FAIL_BUILD=1' \
	'HELM_FAIL_INSTALL=1' \
	'KUBESEER_EMPTY_CA=1' \
	'CURL_FAIL=1'; do
	: >"$TRACE_PATH"
	failure_output="$(run_with_status 1 env PATH="$BIN_DIR:$ORIGINAL_PATH" TRACE_FILE="$TRACE_PATH" \
		KUBECONFIG="$AMBIENT_KUBECONFIG" USE_EXISTING_CLUSTER=true $failure_env \
		"$HARNESS" "$PACKAGE_DIR" '^TestEndToEnd$')"
	assert_not_contains "$failure_output" 'TEST_LAYER=end-to-end STATUS=passed'
	assert_contains "$(<"$TRACE_PATH")" 'args=delete cluster --name kubeseer-e2e-'
done

: >"$TRACE_PATH"
delete_output="$(run_with_status 42 env PATH="$BIN_DIR:$ORIGINAL_PATH" TRACE_FILE="$TRACE_PATH" \
	KUBECONFIG="$AMBIENT_KUBECONFIG" USE_EXISTING_CLUSTER=true KIND_FAIL_DELETE=1 \
	"$HARNESS" "$PACKAGE_DIR" '^TestEndToEnd$')"
assert_contains "$delete_output" 'kubeconfig retained at '
retained_path="$(sed -n 's/.*kubeconfig retained at \(.*\)$/\1/p' <<<"$delete_output" | tail -n 1)"
[[ -n "$retained_path" && -f "$retained_path" ]] || { printf 'retained kubeconfig is missing: %s\n' "$retained_path" >&2; exit 1; }
retained_dir="$(dirname -- "$retained_path")"
rm -rf -- "$retained_dir"

zero_trace="$FIXTURE_DIR/zero.trace"
: >"$zero_trace"
zero_output="$(run_with_status 2 env PATH="$BIN_DIR:$ORIGINAL_PATH" TRACE_FILE="$zero_trace" \
	KUBECONFIG="$AMBIENT_KUBECONFIG" USE_EXISTING_CLUSTER=true \
	"$HARNESS" "$PACKAGE_DIR" '^NoSuchScenario$')"
assert_contains "$zero_output" 'TEST_LAYER=end-to-end STATUS=not-applicable'
assert_not_contains "$(<"$zero_trace")" 'args=create cluster'

printf '%s\n' 'E2E_HARNESS_ACCEPTANCE=complete STATUS=passed'
