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

readonly ROOT_DIR="${KUBESEER_VERIFY_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)}"
readonly MAKEFILE="$ROOT_DIR/Makefile"

failures=0

violation() {
	printf 'test-layer policy violation: %s\n' "$1" >&2
	failures=$((failures + 1))
}

if [[ ! -f "$MAKEFILE" ]]; then
	violation 'Makefile is missing'
elif grep -E -q '^[[:space:]]*test-unit[[:space:]]*:' "$MAKEFILE"; then
	violation 'dedicated test-unit target is forbidden'
fi

check_envtest_suite() {
	local relative_path="$1"
	local expected_test="$2"
	local file="$ROOT_DIR/$relative_path"
	local actual_tests

	if [[ ! -f "$file" ]]; then
		return
	fi

	actual_tests="$(awk '/^func Test[A-Za-z0-9_]+\(/ { sub(/^func /, ""); sub(/\(.*/, ""); print }' "$file" | sort)"
	if [[ "$actual_tests" != "$expected_test" ]]; then
		violation "$relative_path must contain only the named envtest suite $expected_test (found: ${actual_tests:-none})"
	fi
}

for production_root in api internal; do
	root="$ROOT_DIR/$production_root"
	if [[ ! -d "$root" ]]; then
		continue
	fi

	while IFS= read -r file; do
		relative_path="${file#"$ROOT_DIR"/}"
		case "$relative_path" in
		api/v1alpha1/kubeseer_envtest_test.go|internal/discovery/envtest_test.go|internal/selection/envtest_test.go)
			;;
		*)
			violation "$relative_path is a package-local test file outside the approved envtest suites"
			;;
		esac
	done < <(find "$root" -type f -name '*_test.go' -print | sort)
done

check_envtest_suite api/v1alpha1/kubeseer_envtest_test.go TestAPIContract
check_envtest_suite internal/discovery/envtest_test.go TestEnvtestDiscovery
check_envtest_suite internal/selection/envtest_test.go TestEnvtestSelection

if ((failures > 0)); then
	printf 'Test layer policy failed (%d violation(s))\n' "$failures" >&2
	exit 1
fi

printf '%s\n' 'Test layer policy passed'
