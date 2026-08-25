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

mkdir -p -- "$TEST_DIR"
readonly FIXTURE_DIR="$(mktemp -d "$TEST_DIR/test-layer-runner.XXXXXX")"

cleanup() {
	rm -rf -- "$FIXTURE_DIR"
	rmdir --ignore-fail-on-non-empty "$TEST_DIR" 2>/dev/null || true
}
trap cleanup EXIT

readonly RUNNER="$ROOT_DIR/hack/test-layer-runner.sh"
readonly VALID_PACKAGE="./${FIXTURE_DIR#"$ROOT_DIR"/}/valid"
readonly BROKEN_PACKAGE="./${FIXTURE_DIR#"$ROOT_DIR"/}/broken"

mkdir -p -- "$FIXTURE_DIR/valid" "$FIXTURE_DIR/broken"

cat > "$FIXTURE_DIR/valid/runner_test.go" <<'EOF'
package runneracceptance

import "testing"

func TestModuleIntegration(t *testing.T) {
	t.Log("selected integration scenario")
}

func TestModuleIntegrationExtra(t *testing.T) {
	t.Fatal("anchored selection ran the extra scenario")
}
EOF

cat > "$FIXTURE_DIR/broken/runner_test.go" <<'EOF'
package runneracceptance

import "testing"

func TestModuleIntegration(t *testing.T) {
	t.Log("this package must not compile")
// Missing closing brace deliberately exercises compile-error propagation.
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

probe_output="$("$RUNNER" module-integration "$VALID_PACKAGE" '^TestModuleIntegration$' probe)"
assert_contains "$probe_output" 'TEST_LAYER=module-integration STATUS=available'

run_output="$("$RUNNER" module-integration "$VALID_PACKAGE" '^TestModuleIntegration$' required)"
assert_contains "$run_output" 'TEST_LAYER=module-integration STATUS=passed'
assert_contains "$run_output" '--- PASS: TestModuleIntegration'
assert_not_contains "$run_output" 'TestModuleIntegrationExtra'

missing_test_output="$(run_with_status 2 "$RUNNER" module-integration "$VALID_PACKAGE" '^TestMissing$' required)"
assert_contains "$missing_test_output" 'TEST_LAYER=module-integration STATUS=not-applicable'

missing_package_output="$(run_with_status 2 "$RUNNER" module-integration "$FIXTURE_DIR/missing" '^TestModuleIntegration$' required)"
assert_contains "$missing_package_output" 'TEST_LAYER=module-integration STATUS=not-applicable'

broken_output="$(run_with_status 1 "$RUNNER" module-integration "$BROKEN_PACKAGE" '^TestModuleIntegration$' probe)"
assert_contains "$broken_output" 'TEST_LAYER=module-integration STATUS=failed'
assert_not_contains "$broken_output" 'STATUS=not-applicable'
assert_contains "$broken_output" "expected '}'"

printf '%s\n' 'Test-layer runner acceptance passed'
