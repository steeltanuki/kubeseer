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

if (($# != 2)); then
	printf 'usage: %s <package-pattern> <anchored-test-regexp>\n' "$0" >&2
	exit 64
fi

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly TEST_LAYER_RUNNER="$ROOT_DIR/hack/test-layer-runner.sh"
readonly PACKAGE_PATTERN="$1"
readonly TEST_REGEX="$2"
readonly CLUSTER_NAME="kubeseer-e2e"
readonly KIND_PROVIDER="podman"

run_kind() {
	env -u KUBECONFIG KIND_EXPERIMENTAL_PROVIDER="$KIND_PROVIDER" kind "$@"
}

run_podman() {
	env -u KUBECONFIG podman "$@"
}

run_kubectl() {
	env -u KUBECONFIG kubectl "$@"
}

require_prerequisite() {
	local command_name="$1"
	if ! command -v "$command_name" >/dev/null 2>&1; then
		printf 'E2E prerequisite missing: %s\n' "$command_name" >&2
		return 1
	fi
}

preflight() {
	local command_name
	for command_name in kind podman kubectl; do
		require_prerequisite "$command_name"
	done

	local output
	if ! output="$(run_podman info 2>&1)"; then
		printf 'E2E prerequisite failed: podman info\n%s\n' "$output" >&2
		return 1
	fi
	if ! output="$(run_kind version 2>&1)"; then
		printf 'E2E prerequisite failed: kind version\n%s\n' "$output" >&2
		return 1
	fi
	if ! output="$(run_kubectl version --client=true 2>&1)"; then
		printf 'E2E prerequisite failed: kubectl version --client\n%s\n' "$output" >&2
		return 1
	fi
}

owned_dir=''
kubeconfig_path=''
cluster_owned=0

cleanup() {
	local exit_status=$?
	local cleanup_status=0
	local command_status
	trap - EXIT

	if ((cluster_owned)); then
		command_status=0
		run_kind delete cluster --name "$CLUSTER_NAME" --kubeconfig "$kubeconfig_path" || command_status=$?
		if ((command_status != 0)); then
			cleanup_status=$command_status
			printf 'E2E cleanup failed for owned cluster %s; kubeconfig retained at %s\n' \
				"$CLUSTER_NAME" "$kubeconfig_path" >&2
		fi
	fi

	if ((cleanup_status == 0)) && [[ -n "$owned_dir" ]]; then
		command_status=0
		rm -rf -- "$owned_dir" || command_status=$?
		if ((command_status != 0)); then
			cleanup_status=$command_status
			printf 'E2E cleanup failed for owned temporary path %s\n' "$owned_dir" >&2
		fi
	fi

	if ((exit_status == 0 && cleanup_status != 0)); then
		exit_status=$cleanup_status
	fi
	exit "$exit_status"
}
trap cleanup EXIT

set +e
probe_output="$(env -u KUBECONFIG -u USE_EXISTING_CLUSTER \
	GO_TEST_FLAGS="${GO_TEST_FLAGS:-}" \
	"$TEST_LAYER_RUNNER" end-to-end "$PACKAGE_PATTERN" "$TEST_REGEX" probe 2>&1)"
probe_status=$?
set -e
printf '%s\n' "$probe_output"
if ((probe_status != 0)); then
	exit "$probe_status"
fi

preflight

owned_dir="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-e2e.XXXXXXXX")"
kubeconfig_path="$owned_dir/kubeconfig"

# Mark the named cluster as owned before creation so a partially-created kind
# cluster is still removed by the exact, idempotent delete operation.
cluster_owned=1
if ! run_kind create cluster --name "$CLUSTER_NAME" --kubeconfig "$kubeconfig_path"; then
	printf 'E2E cluster setup failed for owned cluster %s\n' "$CLUSTER_NAME" >&2
	exit 1
fi
if [[ ! -s "$kubeconfig_path" ]]; then
	printf 'E2E cluster setup failed: kind did not write %s\n' "$kubeconfig_path" >&2
	exit 1
fi

env -u USE_EXISTING_CLUSTER -u KIND_EXPERIMENTAL_PROVIDER \
	KUBECONFIG="$kubeconfig_path" \
	KUBESEER_E2E_KUBECONFIG="$kubeconfig_path" \
	GO_TEST_FLAGS="${GO_TEST_FLAGS:-}" \
	"$TEST_LAYER_RUNNER" end-to-end "$PACKAGE_PATTERN" "$TEST_REGEX" required
