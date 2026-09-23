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

if (($# != 1)); then
	printf 'usage: %s <check|up|status|examples|verify|diagnostics|examples-down|down|example|test>\n' "$0" >&2
	exit 64
fi

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly ACTION="$1"
readonly LOCAL_CLUSTER_NAME="${LOCAL_CLUSTER_NAME:-kubeseer-local}"
readonly LOCAL_KUBE_CONTEXT="${LOCAL_KUBE_CONTEXT:-kind-kubeseer-local}"
readonly LOCAL_NAMESPACE="${LOCAL_NAMESPACE:-kubeseer-system}"
readonly LOCAL_RELEASE="${LOCAL_RELEASE:-kubeseer}"
readonly LOCAL_PROVIDER="${LOCAL_PROVIDER:-podman}"
readonly CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.18.2}"
readonly KUBERNETES_VERSION="${KUBERNETES_VERSION:-1.35.6}"
readonly LOCAL_READYZ_PORT="${KUBESEER_LOCAL_READYZ_PORT:-18081}"
readonly LOCAL_METRICS_PORT="${KUBESEER_LOCAL_METRICS_PORT:-18080}"
readonly LOCAL_FIELD_MANAGER="kubeseer-local-environment"
readonly LOCAL_EXAMPLE_NAMESPACES=(
	kubeseer-example-builtin
	kubeseer-example-typed
	kubeseer-example-operator
	kubeseer-example-aggregation-a
	kubeseer-example-aggregation-b
	kubeseer-example-custom
	kubeseer-example-denial
	kubeseer-example-degraded
)
readonly SANITIZED_VARIABLES=(
	KUBECONFIG USE_EXISTING_CLUSTER KIND_EXPERIMENTAL_PROVIDER
	AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE AWS_DEFAULT_PROFILE
	AZURE_CLIENT_ID AZURE_CLIENT_SECRET AZURE_TENANT_ID AZURE_SUBSCRIPTION_ID AZURE_AUTH_LOCATION
	GOOGLE_APPLICATION_CREDENTIALS GOOGLE_CREDENTIALS CLOUDSDK_CONFIG CLOUDSDK_AUTH_ACCESS_TOKEN
	CLOUDSDK_CORE_PROJECT GOOGLE_CLOUD_PROJECT
)
# shellcheck source=hack/kind-podman-common.sh
source "$ROOT_DIR/hack/kind-podman-common.sh"

	case "$ACTION" in
	check|preflight|up|status|examples|verify|diagnostics|examples-down|down|example|test) ;;
	*)
		printf 'unsupported local action %q; choose check, up, status, examples, verify, diagnostics, examples-down, down, example, or test\n' "$ACTION" >&2
		exit 64
		;;
esac

state_dir=''
cache_dir=''
metadata_path=''
kubeconfig_path=''
lock_dir=''
worktree_dir=''
kind_node_image=''
source_revision=''
source_identity=''
image_ref=''
image_id=''
cluster_created=0
ownership_promoted=0
lock_held=0
owned_container_id=''
owned_container_state=''

fail() {
	printf 'LOCAL_ENVIRONMENT=%s STATUS=failed: %s\n' "$ACTION" "$1" >&2
	return 1
}

toolchain_value() {
	local key="$1" fallback="$2" value
	value="$(sed -n -E "s/^[[:space:]]*${key}[[:space:]]*(\?[:?]?=|:=|=)[[:space:]]*([^ #]+).*$/\2/p" "$ROOT_DIR/hack/toolchain.mk" | head -n 1)"
	printf '%s\n' "${value:-$fallback}"
}

version_number() {
	local text="$1"
	if [[ "$text" =~ ([0-9]+)\.([0-9]+)(\.([0-9]+))? ]]; then
		printf '%d.%d.%d\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[4]:-0}"
	else
		printf '0.0.0\n'
	fi
}

version_at_least() {
	local actual required a b c x y z
	actual="$(version_number "$1")"; required="$(version_number "$2")"
	IFS=. read -r a b c <<<"$actual"; IFS=. read -r x y z <<<"$required"
	((a > x || (a == x && b > y) || (a == x && b == y && c >= z)))
}

print_tool_version() {
	local name="$1"; shift
	local output
	if ! output="$(kp_sanitized_env "$@" 2>&1)"; then
		printf 'local prerequisite failed: %s\n%s\n' "$name" "$output" >&2
		return 1
	fi
	# Version output is diagnostic, but never include inherited environment data.
	printf 'LOCAL_TOOL=%s VERSION=%s\n' "$name" "$(printf '%s' "$output" | tr '\n' ' ' | cut -c1-240)"
}

resolve_path() {
	local candidate="$1" label="$2" normalized repo_prefix home_dir
	[[ "$candidate" == /* ]] || { printf '%s must be an absolute path: %s\n' "$label" "$candidate" >&2; return 1; }
	normalized="$(readlink -m -- "$candidate")"
	repo_prefix="$ROOT_DIR/"
home_dir="$(readlink -m -- "${HOME:?HOME is required}")"
[[ "$normalized" != "$ROOT_DIR" && "$normalized" != "$repo_prefix"* ]] || {
		printf '%s must be outside the repository: %s\n' "$label" "$normalized" >&2; return 1;
	}
	[[ "$normalized" != / && "$normalized" != "$home_dir" ]] || {
		printf '%s must not be a filesystem or home root: %s\n' "$label" "$normalized" >&2; return 1;
	}
	printf '%s\n' "$normalized"
}

resolve_paths() {
	local state_default cache_default
	state_default="${XDG_STATE_HOME:-${HOME:?HOME is required}/.local/state}/kubeseer/local"
	cache_default="${XDG_CACHE_HOME:-${HOME:?HOME is required}/.cache}/kubeseer/local"
	state_dir="$(resolve_path "${KUBESEER_LOCAL_STATE_DIR:-$state_default}" state-dir)" || return 1
	cache_dir="$(resolve_path "${KUBESEER_LOCAL_CACHE_DIR:-$cache_default}" cache-dir)" || return 1
	if [[ "$state_dir" == "$cache_dir" || "$state_dir" == "$cache_dir/"* || "$cache_dir" == "$state_dir/"* ]]; then
		printf '%s\n' 'state-dir and cache-dir must be distinct non-nested roots' >&2
		return 1
	fi
	metadata_path="$state_dir/metadata.v1"
	kubeconfig_path="$state_dir/kubeconfig"
	lock_dir="$state_dir/action.lock"
	worktree_dir="$state_dir/worktree"
}

preflight() {
	local required command_name output min
	required=(go git make podman kind kubectl helm curl)
	case "$(uname -s):$(uname -m)" in
		Linux:x86_64|Linux:amd64) ;;
		*) printf 'unsupported local host: %s %s; supported profile is Linux/amd64\n' "$(uname -s)" "$(uname -m)" >&2; return 1 ;;
	esac
	for command_name in "${required[@]}"; do
		command -v "$command_name" >/dev/null 2>&1 || { printf 'local prerequisite missing: %s\n' "$command_name" >&2; return 1; }
	done
	case "$KUBERNETES_VERSION" in
		1.35.6) kind_node_image="$(toolchain_value KIND_NODE_IMAGE_1_35_6 kindest/node:v1.35.5)" ;;
		1.36.2) kind_node_image="$(toolchain_value KIND_NODE_IMAGE_1_36_2 kindest/node:v1.36.1)" ;;
		*) printf 'unsupported Kubernetes version %s (supported: 1.35.6 1.36.2)\n' "$KUBERNETES_VERSION" >&2; return 1 ;;
	esac
	print_tool_version go go version
	print_tool_version git git --version
	print_tool_version make make --version
	print_tool_version podman podman --version
	print_tool_version kind kind version
	print_tool_version kubectl kubectl version --client=true
	print_tool_version helm helm version --short
	print_tool_version curl curl --version
	min="$(toolchain_value GO_MIN_VERSION 1.26)"; version_at_least "$(go version)" "$min" || { printf 'Go %s or newer is required\n' "$min" >&2; return 1; }
	min="$(toolchain_value GIT_MIN_VERSION 2.30)"; version_at_least "$(git --version)" "$min" || { printf 'Git %s or newer is required\n' "$min" >&2; return 1; }
	min="$(toolchain_value MAKE_MIN_VERSION 4.3)"; version_at_least "$(make --version | head -1)" "$min" || { printf 'Make %s or newer is required\n' "$min" >&2; return 1; }
	min="$(toolchain_value PODMAN_MIN_VERSION 4.9)"; version_at_least "$(podman --version)" "$min" || { printf 'Podman %s or newer is required\n' "$min" >&2; return 1; }
	min="$(toolchain_value KIND_MIN_VERSION 0.23)"; version_at_least "$(kind version)" "$min" || { printf 'kind %s or newer is required\n' "$min" >&2; return 1; }
	min="$(toolchain_value KUBECTL_MIN_VERSION 1.27)"; version_at_least "$(kubectl version --client=true 2>&1)" "$min" || { printf 'kubectl %s or newer is required\n' "$min" >&2; return 1; }
	min="$(toolchain_value HELM_MIN_VERSION 3.12)"; version_at_least "$(helm version --short)" "$min" || { printf 'Helm %s or newer is required\n' "$min" >&2; return 1; }
	min="$(toolchain_value CURL_MIN_VERSION 7.81)"; version_at_least "$(curl --version | head -1)" "$min" || { printf 'curl %s or newer is required\n' "$min" >&2; return 1; }
	if ! output="$(kp_run_podman info --format '{{.Host.Security.Rootless}}' 2>&1)"; then
		printf 'rootless Podman is unavailable: %s\n' "$(printf '%s' "$output" | tr '\n' ' ' | cut -c1-240)" >&2; return 1
	fi
	if [[ -n "$output" && "$output" != *true* && "$output" != *True* ]]; then
		printf 'Podman is not operating rootlessly\n' >&2; return 1
	fi
	if ! output="$(kp_run_kind "$LOCAL_PROVIDER" version 2>&1)"; then
		printf 'kind cannot select provider %s: %s\n' "$LOCAL_PROVIDER" "$output" >&2; return 1
	fi
}

capture_snapshot() {
	local destination="$1" untracked_file
	mkdir -p -- "$destination"
	git -C "$ROOT_DIR" status --porcelain=v1 --untracked-files=all >"$destination/status"
	git -C "$ROOT_DIR" diff --no-ext-diff --binary HEAD >"$destination/diff"
	git -C "$ROOT_DIR" diff --cached --no-ext-diff --binary >"$destination/cached-diff"
	git -C "$ROOT_DIR" rev-parse --verify HEAD >"$destination/revision"
	: >"$destination/untracked"
	while IFS= read -r untracked_file; do
		[[ -f "$ROOT_DIR/$untracked_file" ]] || continue
		printf '%s ' "$untracked_file" >>"$destination/untracked"
		sha256sum -- "$ROOT_DIR/$untracked_file" >>"$destination/untracked"
	done < <(git -C "$ROOT_DIR" ls-files --others --exclude-standard)
	{
		cat "$destination/status" "$destination/diff" "$destination/cached-diff" "$destination/untracked" "$destination/revision"
		for untracked_file in Dockerfile hack/toolchain.mk config charts examples; do
			[[ -e "$ROOT_DIR/$untracked_file" ]] && find "$ROOT_DIR/$untracked_file" -type f -print0 | sort -z | xargs -0 sha256sum 2>/dev/null || true
		done
	} | sha256sum | awk '{print $1}' >"$destination/identity"
}

worktree_unchanged() {
	capture_snapshot "$worktree_dir/after"
	for file in status diff cached-diff untracked revision identity; do
		cmp -s "$worktree_dir/before/$file" "$worktree_dir/after/$file" || return 1
	done
}

write_metadata() {
	local state="$1" temporary
	mkdir -p -- "$state_dir"
	chmod 700 -- "$state_dir"
	temporary="$(mktemp "$state_dir/metadata.v1.XXXXXX")"
	chmod 600 -- "$temporary"
	{
		printf 'schema_version=1\nstate=%s\ncluster_name=%s\ncontext=%s\n' "$state" "$LOCAL_CLUSTER_NAME" "$LOCAL_KUBE_CONTEXT"
		printf 'kubernetes_version=%s\nkind_node_image=%s\nkubeconfig=%s\nnamespace=%s\nrelease=%s\n' "$KUBERNETES_VERSION" "$kind_node_image" "$kubeconfig_path" "$LOCAL_NAMESPACE" "$LOCAL_RELEASE"
		printf 'source_revision=%s\nsource_identity=sha256:%s\nimage_reference=%s\nimage_id=%s\n' "$source_revision" "$source_identity" "$image_ref" "$image_id"
	} >"$temporary"
	mv -f -- "$temporary" "$metadata_path"
}

read_metadata() {
	local key value line
	local -A seen=()
	[[ -f "$metadata_path" ]] || return 1
	while IFS= read -r line || [[ -n "$line" ]]; do
		[[ "$line" =~ ^([a-z_]+)=(.*)$ ]] || { printf 'malformed ownership metadata\n' >&2; return 1; }
		key="${BASH_REMATCH[1]}"; value="${BASH_REMATCH[2]}"
		[[ -z "${seen[$key]+set}" ]] || { printf 'duplicate ownership metadata field: %s\n' "$key" >&2; return 1; }
		seen[$key]=1
		case "$key" in
			schema_version|state|cluster_name|context|kubernetes_version|kind_node_image|kubeconfig|namespace|release|source_revision|source_identity|image_reference|image_id) ;;
			*) printf 'unknown ownership metadata field: %s\n' "$key" >&2; return 1 ;;
		esac
		done <"$metadata_path"
	for key in schema_version state cluster_name context kubernetes_version kind_node_image kubeconfig namespace release source_revision source_identity image_reference image_id; do
		[[ -n "${seen[$key]+set}" ]] || { printf 'missing ownership metadata field: %s\n' "$key" >&2; return 1; }
	done
	[[ "$(sed -n 's/^schema_version=//p' "$metadata_path")" == 1 ]] || { printf 'unsupported ownership metadata schema\n' >&2; return 1; }
	[[ "$(sed -n 's/^cluster_name=//p' "$metadata_path")" == "$LOCAL_CLUSTER_NAME" ]] || return 1
	[[ "$(sed -n 's/^context=//p' "$metadata_path")" == "$LOCAL_KUBE_CONTEXT" ]] || return 1
	[[ "$(sed -n 's/^kubeconfig=//p' "$metadata_path")" == "$kubeconfig_path" ]] || return 1
	[[ "$(sed -n 's/^namespace=//p' "$metadata_path")" == "$LOCAL_NAMESPACE" ]] || return 1
	[[ "$(sed -n 's/^release=//p' "$metadata_path")" == "$LOCAL_RELEASE" ]] || return 1
	local state source_revision source_identity image_reference image_id
	state="$(sed -n 's/^state=//p' "$metadata_path")"
	source_revision="$(sed -n 's/^source_revision=//p' "$metadata_path")"
	source_identity="$(sed -n 's/^source_identity=//p' "$metadata_path")"
	image_reference="$(sed -n 's/^image_reference=//p' "$metadata_path")"
	image_id="$(sed -n 's/^image_id=//p' "$metadata_path")"
	case "$state" in creating|owned) ;; *) printf 'invalid ownership state\n' >&2; return 1 ;; esac
	[[ "$source_revision" =~ ^[0-9a-fA-F]{40,64}$ ]] || { printf 'invalid source revision in ownership metadata\n' >&2; return 1; }
	[[ "$source_identity" =~ ^sha256:[0-9a-f]{64}$ ]] || { printf 'invalid source identity in ownership metadata\n' >&2; return 1; }
	if [[ -n "$image_reference" ]]; then
		[[ "$image_reference" =~ ^localhost/kubeseer-local:[0-9a-f]{32}$ ]] || { printf 'invalid image reference in ownership metadata\n' >&2; return 1; }
	fi
	# A cluster is promoted to owned before image/package convergence so a
	# failed build preserves the exact target for diagnosis. Empty image fields
	# are therefore valid only during that retryable convergence window.
	if [[ -n "$image_id" && -z "$image_reference" ]]; then
		printf 'owned metadata has an image ID without an image reference\n' >&2
		return 1
	fi
}

cluster_exists() {
	local clusters
	clusters="$(kp_run_kind "$LOCAL_PROVIDER" get clusters 2>/dev/null || true)"
	printf '%s\n' "$clusters" | awk -v wanted="$LOCAL_CLUSTER_NAME" '$0 == wanted {found=1} END {exit(found ? 0 : 1)}'
}

cluster_nodes() {
	kp_kind_nodes "$LOCAL_PROVIDER" "$LOCAL_CLUSTER_NAME"
}

loopback_ip() {
	local address="$1" octet
	local -a octets=()
	if [[ "$address" == ::1 ]]; then return 0; fi
	[[ "$address" =~ ^127\.([0-9]{1,3}\.){2}[0-9]{1,3}$ ]] || return 1
	IFS=. read -r -a octets <<<"$address"
	for octet in "${octets[@]}"; do
		((10#$octet <= 255)) || return 1
	done
}

inspect_owned_control_plane() {
	local inspect_target="${1:-}" expected_container_id=''
	local nodes expected_node api_server api_host api_port inspect_output
	local container_id container_name container_state cluster_label role_label published_api extra
	local binding binding_host binding_port binding_count=0 matched_binding=0
	local -a bindings=()

	expected_node="${LOCAL_CLUSTER_NAME}-control-plane"
	if [[ -z "$inspect_target" ]]; then inspect_target="$expected_node"; else expected_container_id="$inspect_target"; fi
	nodes="$(cluster_nodes)" || {
		printf 'ownership conflict: kind node inventory is unavailable for %s\n' "$LOCAL_CLUSTER_NAME" >&2
		return 1
	}
	if ! printf '%s\n' "$nodes" | rg -F -x -q -- "$expected_node"; then
		printf 'ownership conflict: expected kind node %s is absent from cluster %s\n' "$expected_node" "$LOCAL_CLUSTER_NAME" >&2
		return 1
	fi
	[[ -s "$kubeconfig_path" ]] || {
		printf 'ownership conflict: saved kubeconfig is missing for %s\n' "$LOCAL_CLUSTER_NAME" >&2
		return 1
	}
	api_server="$(kp_run_kubectl "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" config view --minify -o 'jsonpath={.clusters[0].cluster.server}' 2>&1)" || {
		printf 'ownership conflict: cannot read the API endpoint from the owned kubeconfig for context %s\n' "$LOCAL_KUBE_CONTEXT" >&2
		return 1
	}
	if [[ "$api_server" =~ ^https://(\[[^]]+\]|[^/:]+):([0-9]+)$ ]]; then
		api_host="${BASH_REMATCH[1]}"
		api_port="${BASH_REMATCH[2]}"
	else
		printf 'ownership conflict: saved API endpoint is not an explicit HTTPS loopback address\n' >&2
		return 1
	fi
	api_host="${api_host#[}"
	api_host="${api_host%]}"
	if [[ "$api_host" != localhost ]] && ! loopback_ip "$api_host"; then
		printf 'ownership conflict: saved API endpoint is not a loopback address\n' >&2
		return 1
	fi
	[[ "$api_port" =~ ^[0-9]{1,5}$ ]] && ((10#$api_port >= 1 && 10#$api_port <= 65535)) || {
		printf 'ownership conflict: saved API endpoint has an invalid port\n' >&2
		return 1
	}

	if ! inspect_output="$(kp_run_podman container inspect --format '{{.Id}}|{{.Name}}|{{.State.Status}}|{{index .Config.Labels "io.x-k8s.kind.cluster"}}|{{index .Config.Labels "io.x-k8s.kind.role"}}|{{range $binding := (index .HostConfig.PortBindings "6443/tcp")}}{{.HostIP}},{{.HostPort}};{{end}}' "$inspect_target" 2>&1)"; then
		printf 'ownership conflict: cannot inspect the expected control-plane container %s: %s\n' "$expected_node" "${inspect_output:-Podman inspect failed}" >&2
		return 1
	fi
	[[ "$inspect_output" != *$'\n'* ]] || {
		printf 'ownership conflict: Podman returned ambiguous control-plane inspection for %s\n' "$expected_node" >&2
		return 1
	}
	IFS='|' read -r container_id container_name container_state cluster_label role_label published_api extra <<<"$inspect_output"
	container_name="${container_name#/}"
	if [[ -n "$extra" || ! "$container_id" =~ ^[0-9a-f]{12,64}$ || "$container_name" != "$expected_node" ||
		"$cluster_label" != "$LOCAL_CLUSTER_NAME" || "$role_label" != control-plane ]]; then
		printf 'ownership conflict: Podman identity does not match control-plane node %s in cluster %s\n' "$expected_node" "$LOCAL_CLUSTER_NAME" >&2
		return 1
	fi
	if [[ -n "$expected_container_id" && "$container_id" != "$expected_container_id" ]]; then
		printf 'ownership conflict: inspected control-plane container ID changed for %s\n' "$expected_node" >&2
		return 1
	fi
	case "$container_state" in
	running|exited) ;;
	*) printf 'ownership conflict: control-plane container %s has unsupported state %s\n' "$expected_node" "${container_state:-unknown}" >&2; return 1 ;;
	esac

	IFS=';' read -r -a bindings <<<"$published_api"
	for binding in "${bindings[@]}"; do
		[[ -n "$binding" ]] || continue
		((binding_count += 1))
		if [[ "$binding" =~ ^([^,]+),([0-9]+)$ ]]; then
			binding_host="${BASH_REMATCH[1]}"
			binding_port="${BASH_REMATCH[2]}"
			binding_host="${binding_host#[}"
			binding_host="${binding_host%]}"
			if [[ "$binding_host" != localhost ]] && ! loopback_ip "$binding_host"; then
				printf 'ownership conflict: published Kubernetes API binding is not loopback-only for %s\n' "$expected_node" >&2
				return 1
			fi
			if ((10#$binding_port < 1 || 10#$binding_port > 65535)); then
				printf 'ownership conflict: published Kubernetes API binding has an invalid port for %s\n' "$expected_node" >&2
				return 1
			fi
			if [[ "${binding_host,,}" == "${api_host,,}" ]] && ((10#$binding_port == 10#$api_port)); then
				matched_binding=1
			fi
		else
			printf 'ownership conflict: Podman returned a malformed Kubernetes API binding for %s\n' "$expected_node" >&2
			return 1
		fi
	done
	if ((binding_count != 1 || matched_binding != 1)); then
		printf 'ownership conflict: saved API endpoint does not uniquely match the published Kubernetes API port for %s\n' "$expected_node" >&2
		return 1
	fi

	owned_container_id="$container_id"
	owned_container_state="$container_state"
}

acquire_lock() {
	if ! mkdir -- "$lock_dir" 2>/dev/null; then
		printf 'local action lock is held at %s; retry after the owner exits\n' "$lock_dir" >&2
		return 1
	fi
	lock_held=1
	chmod 700 -- "$lock_dir"
	{
		printf 'action=%s\npid=%s\n' "$ACTION" "$$"
	} >"$lock_dir/owner"
}

release_lock() {
	if ((lock_held)); then
		chmod -R u+w -- "$lock_dir" 2>/dev/null || true
		rm -f -- "$lock_dir/owner" 2>/dev/null || true
		rmdir -- "$lock_dir" 2>/dev/null || true
		lock_held=0
	fi
}

cleanup_partial_cluster() {
	if ((cluster_created)) && ((ownership_promoted == 0)); then
		kp_run_kind "$LOCAL_PROVIDER" delete cluster --name "$LOCAL_CLUSTER_NAME" --kubeconfig "$kubeconfig_path" >/dev/null 2>&1 || true
	fi
}

validate_identity() {
	local deadline="${1:-}" server_output kind_nodes api_nodes request_timeout
	[[ -s "$kubeconfig_path" ]] || { printf 'owned kubeconfig is missing: %s\n' "$kubeconfig_path" >&2; return 1; }
	if [[ -n "$deadline" ]]; then
		request_timeout="$(request_timeout_for_deadline "$deadline")" || {
			printf 'Kubernetes API readiness wait expired for context %s\n' "$LOCAL_KUBE_CONTEXT" >&2
			return 1
		}
	else
		request_timeout=5s
	fi
	server_output="$(kp_run_kubectl "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" version --request-timeout="$request_timeout" --output=json 2>&1)" || {
		printf 'Kubernetes API identity is unreachable for context %s: %s\n' "$LOCAL_KUBE_CONTEXT" "$server_output" >&2
		return 1
	}
	if [[ "$server_output" != *'"serverVersion"'* || "$server_output" != *'"gitVersion"'* ]]; then
		printf 'Kubernetes API identity conflicts with saved context %s\n' "$LOCAL_KUBE_CONTEXT" >&2
		return 2
	fi
	kind_nodes="$(cluster_nodes)" || { printf 'kind node identity is unavailable for %s\n' "$LOCAL_CLUSTER_NAME" >&2; return 2; }
	[[ -n "$kind_nodes" ]] || { printf 'kind returned no nodes for %s\n' "$LOCAL_CLUSTER_NAME" >&2; return 2; }
	if [[ -n "$deadline" ]]; then
		request_timeout="$(request_timeout_for_deadline "$deadline")" || {
			printf 'Kubernetes API readiness wait expired for context %s\n' "$LOCAL_KUBE_CONTEXT" >&2
			return 1
		}
	fi
	api_nodes="$(kp_run_kubectl "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" get nodes --request-timeout="$request_timeout" -o 'jsonpath={range .items[*]}{.metadata.name}{"\n"}{end}' 2>&1)" || {
		printf 'Kubernetes API node identity is unavailable for context %s: %s\n' "$LOCAL_KUBE_CONTEXT" "$api_nodes" >&2
		return 1
	}
	kind_nodes="$(printf '%s\n' "$kind_nodes" | LC_ALL=C sort -u)"
	api_nodes="$(printf '%s\n' "$api_nodes" | LC_ALL=C sort -u)"
	if [[ -z "$api_nodes" || "$api_nodes" != "$kind_nodes" ]]; then
		printf 'Kubernetes API node identity conflicts with kind cluster %s (kind=%s api=%s)\n' \
			"$LOCAL_CLUSTER_NAME" "${kind_nodes//$'\n'/,}" "${api_nodes//$'\n'/,}" >&2
		return 2
	fi
}

request_timeout_for_deadline() {
	local deadline="$1" remaining
	remaining=$((deadline - SECONDS))
	((remaining > 0)) || return 1
	((remaining <= 5)) || remaining=5
	printf '%ss\n' "$remaining"
}

resume_owned_control_plane() {
	local resume_timeout="${KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS:-120}"
	local timeout_number deadline remaining start_output last_error='' validation_output validation_status
	local node_name="${LOCAL_CLUSTER_NAME}-control-plane"
	local resume_container_id="$owned_container_id"

	if [[ "$owned_container_state" == running ]]; then
		validate_identity
		return
	fi
	if [[ ! "$resume_timeout" =~ ^[0-9]{1,3}$ ]]; then
		printf 'KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS must be an integer from 1 through 600\n' >&2
		return 1
	fi
	timeout_number=$((10#$resume_timeout))
	if ((timeout_number < 1 || timeout_number > 600)); then
		printf 'KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS must be an integer from 1 through 600\n' >&2
		return 1
	fi
	if ! start_output="$(kp_run_podman container start "$resume_container_id" 2>&1)"; then
		printf 'failed to start owned node %s with Podman: %s\n' "$node_name" "${start_output:-Podman container start failed}" >&2
		return 1
	fi
	inspect_owned_control_plane "$resume_container_id" || return 1
	if [[ "$owned_container_id" != "$resume_container_id" || "$owned_container_state" != running ]]; then
		printf 'ownership conflict: resumed node %s did not return as the same running container\n' "$node_name" >&2
		return 1
	fi
	deadline=$((SECONDS + timeout_number))
	while :; do
		remaining=$((deadline - SECONDS))
		if ((remaining <= 0)); then
			printf 'Kubernetes API readiness wait expired after %s seconds for node %s (context %s)\n' \
				"$timeout_number" "$node_name" "$LOCAL_KUBE_CONTEXT" >&2
			[[ -z "$last_error" ]] || printf 'last API check: %s\n' "$last_error" >&2
			return 1
		fi
		if validation_output="$(validate_identity "$deadline" 2>&1)"; then
			return 0
		else
			validation_status=$?
		fi
		if ((validation_status == 2)); then
			printf 'resumed API identity conflicts for node %s: %s\n' "$node_name" "$validation_output" >&2
			return 1
		fi
		last_error="$validation_output"
		remaining=$((deadline - SECONDS))
		if ((remaining > 1)); then sleep 1; elif ((remaining > 0)); then sleep "$remaining"; fi
	done
}

compute_source_identity() {
	source_revision="$(git -C "$ROOT_DIR" rev-parse --verify HEAD)"
	source_identity="$(<"$worktree_dir/before/identity")"
}

build_and_load_image() {
	local archive nodes node node_archive
	image_ref="localhost/kubeseer-local:${source_identity:0:32}"
	image_ref="$(printf '%s' "$image_ref" | tr '[:upper:]' '[:lower:]')"
	if [[ -s "$metadata_path" && "$(sed -n 's/^source_identity=sha256://p' "$metadata_path" 2>/dev/null || true)" == "$source_identity" && -n "$(sed -n 's/^image_id=//p' "$metadata_path" 2>/dev/null || true)" ]]; then
		local recorded_image_id existing_image_id
		recorded_image_id="$(sed -n 's/^image_id=//p' "$metadata_path")"
		existing_image_id="$(kp_run_podman image inspect --format '{{.Id}}' "$image_ref" 2>/dev/null || true)"
		if [[ -n "$existing_image_id" && "$existing_image_id" == "$recorded_image_id" ]]; then
			image_id="$recorded_image_id"
			printf 'LOCAL_IMAGE_REUSE source_identity=sha256:%s image=%s\n' "$source_identity" "$image_ref"
		else
			image_id=''
		fi
	fi
	if [[ -z "$image_id" ]]; then
		kp_run_podman build --file "$ROOT_DIR/Dockerfile" --tag "$image_ref" \
			--build-arg "VERSION=$source_revision" --build-arg "COMMIT=$source_revision" \
			--build-arg "BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$ROOT_DIR" || {
			printf 'image build failed before Helm release mutation\n' >&2; return 1;
		}
		image_id="$(kp_run_podman image inspect --format '{{.Id}}' "$image_ref")" || return 1
		[[ -n "$image_id" ]] || { printf 'Podman returned an empty image identity\n' >&2; return 1; }
	fi
	archive="$cache_dir/images/${source_identity}.oci.tar"
	mkdir -p -- "$(dirname -- "$archive")"; chmod 700 -- "$cache_dir" "$(dirname -- "$archive")"
	if [[ ! -s "$archive" ]]; then
		kp_run_podman save --format oci-archive --output "$archive" "$image_ref" || { printf 'image archive export failed\n' >&2; return 1; }
	fi
	nodes="$(cluster_nodes)" || return 1
	while IFS= read -r node; do
		[[ -n "$node" ]] || continue
		node_archive="/tmp/kubeseer-local-${LOCAL_CLUSTER_NAME}.oci.tar"
		if ! kp_run_podman cp "$archive" "$node:$node_archive"; then printf 'image import failed for node %s\n' "$node" >&2; return 1; fi
		if ! kp_run_podman exec "$node" ctr --namespace k8s.io images import "$node_archive"; then printf 'image import failed for node %s\n' "$node" >&2; return 1; fi
		kp_run_podman exec "$node" rm -f "$node_archive" >/dev/null 2>&1 || true
	done <<<"$nodes"
}

ensure_local_example_namespaces() {
	local namespace
	for namespace in "${LOCAL_EXAMPLE_NAMESPACES[@]}"; do
		kp_run_kubectl "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" apply --server-side \
			--field-manager="$LOCAL_FIELD_MANAGER" -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $namespace
EOF
	done
}

install_package() {
	local cert_manifest="$cache_dir/downloads/cert-manager-${CERT_MANAGER_VERSION}.yaml"
	local cert_url="https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
	local image_tag="${image_ref##*:}"
	mkdir -p -- "$(dirname -- "$cert_manifest")"; chmod 700 -- "$cache_dir/downloads"
	if [[ ! -s "$cert_manifest" ]]; then
		kp_run_curl --fail --silent --show-error --location --retry 3 --output "$cert_manifest" "$cert_url" || { printf 'pinned cert-manager %s could not be downloaded\n' "$CERT_MANAGER_VERSION" >&2; return 1; }
	fi
	[[ -s "$cert_manifest" ]] || { printf 'pinned cert-manager manifest is empty\n' >&2; return 1; }
	kp_run_kubectl "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" apply --server-side --field-manager=kubeseer-local-cert-manager -f "$cert_manifest" || return 1
	kp_run_kubectl "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" wait --for=condition=Established --timeout=5m \
		crd/certificates.cert-manager.io crd/issuers.cert-manager.io crd/clusterissuers.cert-manager.io || return 1
	kp_run_kubectl "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" --namespace cert-manager wait --for=condition=Available --timeout=10m deployment --all || return 1
	[[ -f "$ROOT_DIR/config/local/values.yaml" ]] || { printf 'local Helm values profile is missing\n' >&2; return 1; }
	ensure_local_example_namespaces || return 1
	kp_run_helm "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" "$state_dir/helm/config" "$cache_dir/helm" "$state_dir/helm/data" \
		upgrade --install "$LOCAL_RELEASE" "$ROOT_DIR/charts/kubeseer" --namespace "$LOCAL_NAMESPACE" --create-namespace \
		--values "$ROOT_DIR/config/local/values.yaml" --set "image.repository=${image_ref%:*}" --set "image.tag=$image_tag" \
		--set image.pullPolicy=IfNotPresent --atomic --cleanup-on-fail --wait --timeout 10m || {
		printf 'canonical Helm upgrade failed; prior release revision is preserved\n' >&2; return 1;
	}
}

wait_for_local_http() {
	local target="$1" local_port="$2" remote_port="$3" path="$4" expected="$5"
	local forward_log="$state_dir/port-forward-${local_port}.log" forward_pid='' body='' attempt
	mkdir -p -- "$state_dir"
	# The port-forward process is deliberately scoped to this one bounded
	# observation. It never becomes part of persistent cluster state.
	kp_run_port_forward "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" "$LOCAL_NAMESPACE" "$target" "$local_port:$remote_port" >"$forward_log" 2>&1 &
	forward_pid=$!
	for attempt in {1..30}; do
		if body="$(kp_run_curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:${local_port}${path}" 2>/dev/null)" && [[ "$body" == *"$expected"* ]]; then
			kill "$forward_pid" 2>/dev/null || true
			wait "$forward_pid" 2>/dev/null || true
			return 0
		fi
		if ! kill -0 "$forward_pid" 2>/dev/null; then
			break
		fi
		sleep 1
	done
	kill "$forward_pid" 2>/dev/null || true
	wait "$forward_pid" 2>/dev/null || true
	printf 'awaited %s%s on loopback port %s\n' "$target" "$path" "$local_port" >&2
	return 1
}

probe() {
	local command=(go run ./cmd/kubeseer-local)
	if [[ -n "${KUBESEER_LOCAL_PROBE_BIN:-}" ]]; then command=("$KUBESEER_LOCAL_PROBE_BIN"); fi
	"${command[@]}" "$1" --state-dir "$state_dir" --metadata "$metadata_path" --kubeconfig "$kubeconfig_path" --cluster-name "$LOCAL_CLUSTER_NAME" --context "$LOCAL_KUBE_CONTEXT" --timeout "${KUBESEER_LOCAL_TIMEOUT:-2m}" "${@:2}"
}

create_or_validate_cluster() {
	local existing
	if [[ -f "$metadata_path" ]]; then
		read_metadata || { printf 'ownership conflict: metadata is malformed or mismatched\n' >&2; return 1; }
		existing=0; cluster_exists && existing=1 || true
		if [[ "$(sed -n 's/^kubernetes_version=//p' "$metadata_path")" != "$KUBERNETES_VERSION" ]]; then
			printf 'owned cluster Kubernetes version mismatch: found %s, requested %s; run make local-down\n' \
				"$(sed -n 's/^kubernetes_version=//p' "$metadata_path")" "$KUBERNETES_VERSION" >&2
			return 1
		fi
		if [[ "$(sed -n 's/^state=//p' "$metadata_path")" == creating ]]; then
			if ((existing)); then
				kp_run_kind "$LOCAL_PROVIDER" delete cluster --name "$LOCAL_CLUSTER_NAME" --kubeconfig "$kubeconfig_path" || return 1
			fi
			rm -f -- "$metadata_path" "$kubeconfig_path"
			existing=0
		fi
		if ((existing == 0)) && [[ -f "$metadata_path" ]]; then
			printf 'ownership metadata claims an owned target but %s is absent; refusing adoption\n' "$LOCAL_CLUSTER_NAME" >&2
			return 1
		fi
	else
		existing=0; cluster_exists && existing=1 || true
	fi
	if ((existing)); then
		[[ -f "$metadata_path" ]] || { printf 'cluster %s exists without matching ownership metadata\n' "$LOCAL_CLUSTER_NAME" >&2; return 1; }
		inspect_owned_control_plane || return 1
		resume_owned_control_plane || return 1
		return 0
	fi
	write_metadata creating
	cluster_created=1
	if ! kp_run_kind "$LOCAL_PROVIDER" create cluster --name "$LOCAL_CLUSTER_NAME" --image "$kind_node_image" --wait 5m --kubeconfig "$kubeconfig_path"; then
		cleanup_partial_cluster; rm -f -- "$metadata_path" "$kubeconfig_path"; printf 'kind creation failed; partial owned target was cleaned\n' >&2; return 1
	fi
	cluster_created=1
	validate_identity || { cleanup_partial_cluster; rm -f -- "$metadata_path" "$kubeconfig_path"; return 1; }
	ownership_promoted=1
	write_metadata owned
}

run_up() {
	preflight || return 1
	resolve_paths || return 1
	mkdir -p -- "$state_dir" "$cache_dir" "$state_dir/helm/config" "$state_dir/helm/data" "$cache_dir/helm"; chmod 700 -- "$state_dir" "$cache_dir"
	capture_snapshot "$worktree_dir/before"
	acquire_lock || return 1
	trap 'cleanup_partial_cluster; release_lock' RETURN
	compute_source_identity
	create_or_validate_cluster || return 1
	build_and_load_image || return 1
	write_metadata owned
	install_package || return 1
	write_metadata owned
	wait_for_local_http "deployment/$LOCAL_RELEASE" "$LOCAL_READYZ_PORT" 8081 /readyz ok || return 1
	wait_for_local_http "service/${LOCAL_RELEASE}-metrics" "$LOCAL_METRICS_PORT" 8080 /metrics '# HELP' || return 1
	probe readiness || return 1
	write_metadata owned
	worktree_unchanged || { printf 'worktree drift detected; no files were changed by local-up\n' >&2; return 1; }
	ownership_promoted=1
	release_lock
	trap - RETURN
	printf 'LOCAL_ENVIRONMENT=up STATUS=passed\n'
}

run_status() {
	resolve_paths || return 1
	if [[ ! -f "$metadata_path" ]]; then
		if cluster_exists; then
			printf 'LOCAL_STATUS=conflict cluster=%s metadata=%s\n' "$LOCAL_CLUSTER_NAME" "$metadata_path" >&2
			return 1
		fi
		printf 'LOCAL_STATUS=absent cluster=%s context=%s state=%s\n' "$LOCAL_CLUSTER_NAME" "$LOCAL_KUBE_CONTEXT" "$state_dir"
		printf 'LOCAL_ENVIRONMENT=status STATUS=passed\n'; return 0
	fi
	read_metadata || { printf 'LOCAL_STATUS=conflict metadata=%s\n' "$metadata_path" >&2; return 1; }
	capture_snapshot "$worktree_dir/before"
	printf 'LOCAL_STATUS_IDENTITY state=%s kubeconfig=%s context=%s cluster=%s kubernetes_version=%s source_identity=sha256:%s\n' \
		"$(sed -n 's/^state=//p' "$metadata_path")" "$kubeconfig_path" "$LOCAL_KUBE_CONTEXT" "$LOCAL_CLUSTER_NAME" \
		"$(sed -n 's/^kubernetes_version=//p' "$metadata_path")" "$(sed -n 's/^source_identity=sha256://p' "$metadata_path")"
	probe status
	worktree_unchanged || return 1
	printf 'LOCAL_ENVIRONMENT=status STATUS=passed\n'
}

run_examples() {
	resolve_paths || return 1
	[[ -f "$metadata_path" ]] || { printf 'local cluster is absent; run make local-up first\n' >&2; return 1; }
	read_metadata || return 1
	[[ "$(sed -n 's/^state=//p' "$metadata_path")" == owned ]] || { printf 'local cluster is still being created; retry after make local-up\n' >&2; return 1; }
	probe status >/dev/null || return 1
	capture_snapshot "$worktree_dir/before"
	acquire_lock || return 1
	trap 'release_lock' RETURN
	KUBESEER_LOCAL_STATE_DIR="$state_dir" KUBESEER_LOCAL_CACHE_DIR="$cache_dir" \
		"$ROOT_DIR/hack/local-examples.sh" apply "${EXAMPLE:-}" || return 1
	worktree_unchanged || return 1
	release_lock; trap - RETURN
	printf 'LOCAL_ENVIRONMENT=examples STATUS=passed\n'
}

run_verify() {
	resolve_paths || return 1
	read_metadata || { printf 'local cluster ownership is unavailable\n' >&2; return 1; }
	[[ "$(sed -n 's/^state=//p' "$metadata_path")" == owned ]] || { printf 'local cluster is still being created; retry after make local-up\n' >&2; return 1; }
	capture_snapshot "$worktree_dir/before"
	wait_for_local_http "deployment/$LOCAL_RELEASE" "$LOCAL_READYZ_PORT" 8081 /readyz ok || return 1
	wait_for_local_http "service/${LOCAL_RELEASE}-metrics" "$LOCAL_METRICS_PORT" 8080 /metrics '# HELP' || return 1
	probe readiness >/dev/null || return 1
	# The catalog executor owns the authorization-denial policy-drift fixture;
	# it narrows and restores the live policy around that one probe assertion.
	# Keep the public assertions in the typed local probe while letting the
	# workflow layer control this intentional runtime transition.
	KUBESEER_LOCAL_STATE_DIR="$state_dir" KUBESEER_LOCAL_CACHE_DIR="$cache_dir" \
		"$ROOT_DIR/hack/local-examples.sh" verify
	worktree_unchanged || return 1
	printf 'LOCAL_ENVIRONMENT=verify STATUS=passed\n'
}

run_diagnostics() {
	resolve_paths || return 1
	read_metadata || { printf 'local cluster ownership is unavailable\n' >&2; return 1; }
	[[ "$(sed -n 's/^state=//p' "$metadata_path")" == owned ]] || { printf 'local cluster is still being created; retry after make local-up\n' >&2; return 1; }
	capture_snapshot "$worktree_dir/before"
	local kind_version podman_version kubectl_version helm_version curl_version destination
	destination="${KUBESEER_LOCAL_DIAGNOSTICS_DIR:-}"
	if [[ -z "$destination" ]]; then
		mkdir -p -- "$state_dir/diagnostics"
		destination="$(mktemp -d "$state_dir/diagnostics/run.XXXXXX")" || return 1
		chmod 700 -- "$destination"
	fi
	kind_version="$(kp_run_kind "$LOCAL_PROVIDER" version 2>/dev/null || true)"
	podman_version="$(kp_run_podman --version 2>/dev/null || true)"
	kubectl_version="$(kp_sanitized_env kubectl version --client=true 2>/dev/null || true)"
	helm_version="$(kp_sanitized_env helm version --short 2>/dev/null || true)"
	curl_version="$(kp_run_curl --version 2>/dev/null || true)"
	KUBESEER_LOCAL_KIND_VERSION="$kind_version" KUBESEER_LOCAL_PODMAN_VERSION="$podman_version" \
	KUBESEER_LOCAL_KUBECTL_VERSION="$kubectl_version" KUBESEER_LOCAL_HELM_VERSION="$helm_version" \
	KUBESEER_LOCAL_CURL_VERSION="$curl_version" probe diagnostics --destination "$destination"
	worktree_unchanged || return 1
	printf 'LOCAL_ENVIRONMENT=diagnostics STATUS=passed\n'
}

run_examples_down() {
	resolve_paths || return 1
	read_metadata || { printf 'local cluster ownership is unavailable\n' >&2; return 1; }
	[[ "$(sed -n 's/^state=//p' "$metadata_path")" == owned ]] || { printf 'local cluster is still being created; retry after make local-up\n' >&2; return 1; }
	probe status >/dev/null || return 1
	capture_snapshot "$worktree_dir/before"
	acquire_lock || return 1
	trap 'release_lock' RETURN
	KUBESEER_LOCAL_STATE_DIR="$state_dir" KUBESEER_LOCAL_CACHE_DIR="$cache_dir" \
		"$ROOT_DIR/hack/local-examples.sh" down "${EXAMPLE:-}" || return 1
	worktree_unchanged || return 1
	release_lock; trap - RETURN
	printf 'LOCAL_ENVIRONMENT=examples-down STATUS=passed\n'
}

run_down() {
	resolve_paths || return 1
	if [[ ! -f "$metadata_path" ]]; then
		if cluster_exists; then
			printf 'ownership metadata is absent; refusing to delete cluster=%s\n' "$LOCAL_CLUSTER_NAME" >&2
			return 1
		fi
		printf 'LOCAL_STATUS=absent cluster=%s\n' "$LOCAL_CLUSTER_NAME"
		printf 'LOCAL_ENVIRONMENT=down STATUS=passed\n'; return 0
	fi
	preflight || return 1
	read_metadata || { printf 'ownership conflict; refusing to delete %s\n' "$LOCAL_CLUSTER_NAME" >&2; return 1; }
	[[ "$(sed -n 's/^state=//p' "$metadata_path")" == owned ]] || { printf 'cluster is not in owned state; refusing deletion\n' >&2; return 1; }
	capture_snapshot "$worktree_dir/before"
	acquire_lock || return 1
	trap 'release_lock' RETURN
	if ! kp_run_kind "$LOCAL_PROVIDER" delete cluster --name "$LOCAL_CLUSTER_NAME" --kubeconfig "$kubeconfig_path"; then
		printf 'local cluster deletion failed; retained cluster=%s kubeconfig=%s metadata=%s\n' "$LOCAL_CLUSTER_NAME" "$kubeconfig_path" "$metadata_path" >&2; return 1
	fi
	rm -f -- "$metadata_path" "$kubeconfig_path"
	printf 'LOCAL_CLEANUP=cluster=%s image-cache=%s tool-cache=%s\n' "$LOCAL_CLUSTER_NAME" "$cache_dir/images" "$cache_dir"
	worktree_unchanged || return 1
	release_lock; trap - RETURN
	printf 'LOCAL_ENVIRONMENT=down STATUS=passed\n'
}

run_test() {
	local test_dir state cache output status
	test_dir="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-local-test.XXXXXX")"
	state="$test_dir/state"; cache="$test_dir/cache"
	set +e
	output="$(KUBESEER_LOCAL_STATE_DIR="$state" KUBESEER_LOCAL_CACHE_DIR="$cache" "$ROOT_DIR/hack/local-environment-acceptance.sh" complete 2>&1)"
	status=$?
	set -e
	printf '%s\n' "$output"
	rm -rf -- "$test_dir"
	return "$status"
}

case "$ACTION" in
	check|preflight)
		resolve_paths; preflight
		printf 'LOCAL_ENVIRONMENT=%s STATUS=passed\n' "$ACTION"
		;;
	up) run_up ;;
	status) run_status ;;
	examples) run_examples ;;
	verify) run_verify ;;
	diagnostics) run_diagnostics ;;
	examples-down) run_examples_down ;;
	down) run_down ;;
	example)
		local_example_action="${LOCAL_EXAMPLE_ACTION:-}"
		[[ -n "${EXAMPLE:-}" && -n "$local_example_action" ]] || { printf 'local-example requires EXAMPLE and ACTION=apply|inspect|verify|down\n' >&2; exit 64; }
	case "$local_example_action" in apply|inspect|verify|down) ;; *) printf 'unsupported local-example ACTION %s\n' "$local_example_action" >&2; exit 64 ;; esac
		resolve_paths; read_metadata || { printf 'local cluster ownership is unavailable\n' >&2; exit 1; }
		[[ "$(sed -n 's/^state=//p' "$metadata_path")" == owned ]] || { printf 'local cluster is still being created; retry after make local-up\n' >&2; exit 1; }
		probe status >/dev/null || exit 1
		capture_snapshot "$worktree_dir/before"
		if [[ "$local_example_action" == apply || "$local_example_action" == down ]]; then
			acquire_lock || exit 1
			trap 'release_lock' EXIT
		fi
		KUBESEER_LOCAL_STATE_DIR="$state_dir" KUBESEER_LOCAL_CACHE_DIR="$cache_dir" \
			"$ROOT_DIR/hack/local-examples.sh" "$local_example_action" "$EXAMPLE"
		if [[ "$local_example_action" == apply || "$local_example_action" == down ]]; then
			release_lock
			trap - EXIT
		fi
		worktree_unchanged || exit 1
		;;
	test) run_test ;;
esac
