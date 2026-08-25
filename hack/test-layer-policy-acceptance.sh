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
readonly VERIFIER="$ROOT_DIR/hack/verify-test-layer-policy.sh"

mkdir -p -- "$TEST_DIR"
readonly FIXTURE_DIR="$(mktemp -d "$TEST_DIR/test-layer-policy-acceptance.XXXXXX")"

cleanup() {
	rm -rf -- "$FIXTURE_DIR"
	rmdir --ignore-fail-on-non-empty "$TEST_DIR" 2>/dev/null || true
}
trap cleanup EXIT

mkdir -p -- "$FIXTURE_DIR/api/v1alpha1" "$FIXTURE_DIR/internal/discovery"

cat > "$FIXTURE_DIR/Makefile" <<'EOF'
.PHONY: verify

verify:
	@true
EOF

cat > "$FIXTURE_DIR/api/v1alpha1/kubeseer_envtest_test.go" <<'EOF'
package v1alpha1

import "testing"

func TestAPIContract(t *testing.T) {}
EOF

cat > "$FIXTURE_DIR/internal/discovery/envtest_test.go" <<'EOF'
package discovery

import "testing"

func TestEnvtestDiscovery(t *testing.T) {}
EOF

assert_contains() {
	local output="$1"
	local expected="$2"
	if [[ "$output" != *"$expected"* ]]; then
		printf 'expected output to contain %q, got:\n%s\n' "$expected" "$output" >&2
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

baseline_output="$(env KUBESEER_VERIFY_ROOT="$FIXTURE_DIR" "$VERIFIER")"
assert_contains "$baseline_output" 'Test layer policy passed'

cat > "$FIXTURE_DIR/api/v1alpha1/unit_test.go" <<'EOF'
package v1alpha1

import "testing"

func TestUnit(t *testing.T) {}
EOF
forbidden_output="$(run_with_status 1 env KUBESEER_VERIFY_ROOT="$FIXTURE_DIR" "$VERIFIER")"
assert_contains "$forbidden_output" 'api/v1alpha1/unit_test.go is a package-local test file outside the approved envtest suites'
rm -f -- "$FIXTURE_DIR/api/v1alpha1/unit_test.go"

cat >> "$FIXTURE_DIR/Makefile" <<'EOF'

test-unit:
	@true
EOF
target_output="$(run_with_status 1 env KUBESEER_VERIFY_ROOT="$FIXTURE_DIR" "$VERIFIER")"
assert_contains "$target_output" 'dedicated test-unit target is forbidden'
rm -f -- "$FIXTURE_DIR/Makefile"

cat > "$FIXTURE_DIR/Makefile" <<'EOF'
.PHONY: verify

verify:
	@true
EOF
cat > "$FIXTURE_DIR/api/v1alpha1/kubeseer_envtest_test.go" <<'EOF'
package v1alpha1

import "testing"

func TestAPIContract(t *testing.T) {}
func TestUnexpected(t *testing.T) {}
EOF
suite_output="$(run_with_status 1 env KUBESEER_VERIFY_ROOT="$FIXTURE_DIR" "$VERIFIER")"
assert_contains "$suite_output" 'api/v1alpha1/kubeseer_envtest_test.go must contain only the named envtest suite TestAPIContract'

printf '%s\n' 'Test-layer policy acceptance passed'
