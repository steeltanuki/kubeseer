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

if (($# != 4)); then
	printf 'usage: %s <layer> <package-pattern> <anchored-test-regexp> <probe|required>\n' "$0" >&2
	exit 64
fi

readonly layer="$1"
readonly package_pattern="$2"
readonly test_regex="$3"
readonly mode="$4"

if [[ -z "$layer" || -z "$package_pattern" || -z "$test_regex" ]]; then
	printf 'layer, package pattern, and test regexp must not be empty\n' >&2
	exit 64
fi

if [[ "${test_regex:0:1}" != '^' || "${test_regex: -1}" != '$' ]]; then
	printf 'test regexp must be anchored at both ends: %s\n' "$test_regex" >&2
	exit 64
fi

case "$mode" in
	probe|required)
		;;
	*)
		printf 'mode must be probe or required: %s\n' "$mode" >&2
		exit 64
		;;
esac

go_test_flags=()
if [[ -n "${GO_TEST_FLAGS:-}" ]]; then
	read -r -a go_test_flags <<< "$GO_TEST_FLAGS"
fi

not_applicable() {
	printf 'TEST_LAYER=%s STATUS=not-applicable\n' "$layer"
	exit 2
}

is_missing_package() {
	local output="$1"
	grep -E -q 'pattern .*: directory not found|stat .*: directory not found|stat .*: no such file or directory|lstat .*: no such file or directory|matched no packages|no packages to test' <<< "$output"
}

set +e
list_output="$(go test "${go_test_flags[@]}" -list "$test_regex" "$package_pattern" 2>&1)"
list_status=$?
set -e

if ((list_status != 0)); then
	if is_missing_package "$list_output"; then
		not_applicable
	fi

	printf 'TEST_LAYER=%s STATUS=failed\n' "$layer" >&2
	printf '%s\n' "$list_output" >&2
	exit "$list_status"
fi

if ! printf '%s\n' "$list_output" | grep -E -q -x "$test_regex"; then
	not_applicable
fi

if [[ "$mode" == 'probe' ]]; then
	printf 'TEST_LAYER=%s STATUS=available\n' "$layer"
	exit 0
fi

printf 'TEST_LAYER=%s STATUS=running\n' "$layer"
if go test -v "${go_test_flags[@]}" -run "$test_regex" "$package_pattern"; then
	printf 'TEST_LAYER=%s STATUS=passed\n' "$layer"
	exit 0
else
	test_status=$?
fi

printf 'TEST_LAYER=%s STATUS=failed\n' "$layer" >&2
exit "$test_status"
