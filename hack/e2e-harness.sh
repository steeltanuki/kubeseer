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
readonly KIND_PROVIDER="podman"
readonly E2E_NAMESPACE="${KUBESEER_E2E_NAMESPACE:-kubeseer-system}"
readonly E2E_RELEASE="${KUBESEER_E2E_RELEASE:-kubeseer}"
readonly KUBERNETES_VERSION="${KUBERNETES_VERSION:-1.35.6}"
readonly CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.18.2}"
readonly E2E_LIMIT_PROFILE="${KUBESEER_E2E_LIMIT_PROFILE:-$ROOT_DIR/test/e2e/values.yaml}"

# Keep E2E's run-unique state machine independent while sharing only the
# stateless, credential-sanitizing kind/Podman process primitives.
# shellcheck source=hack/kind-podman-common.sh
source "$ROOT_DIR/hack/kind-podman-common.sh"

# These names are deliberately explicit. The E2E process must not inherit a
# user's ambient cluster or cloud credentials, even when a provider CLI would
# otherwise discover them automatically.
readonly SANITIZED_VARIABLES=(
	KUBECONFIG USE_EXISTING_CLUSTER KIND_EXPERIMENTAL_PROVIDER
	AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE AWS_DEFAULT_PROFILE
	AZURE_CLIENT_ID AZURE_CLIENT_SECRET AZURE_TENANT_ID AZURE_SUBSCRIPTION_ID AZURE_AUTH_LOCATION
	GOOGLE_APPLICATION_CREDENTIALS GOOGLE_CREDENTIALS CLOUDSDK_CONFIG CLOUDSDK_AUTH_ACCESS_TOKEN
	CLOUDSDK_CORE_PROJECT GOOGLE_CLOUD_PROJECT
)

owned_dir=''
kubeconfig_path=''
cluster_name=''
kube_context=''
cluster_owned=0
diagnostic_phase='startup'
port_forward_pids=()
e2e_go_proxy=''

prepare_go_environment() {
	local source_module_cache source_proxy inherited_proxy
	inherited_proxy="${GOPROXY:-}"
	source_module_cache="$(env -u GOMODCACHE go env GOMODCACHE 2>/dev/null || true)"
	source_proxy="$source_module_cache/cache/download"
	if [[ -d "$source_proxy" ]]; then
		# Keep the run's module extraction cache owned by the harness while
		# allowing an offline invocation to reuse the contributor's immutable
		# module download cache through Go's file proxy.
		e2e_go_proxy="file://$source_proxy"
		if [[ -n "$inherited_proxy" && "$inherited_proxy" != off ]]; then
			e2e_go_proxy="$e2e_go_proxy,$inherited_proxy"
		fi
	else
		e2e_go_proxy="$inherited_proxy"
	fi
}

sanitized_env() {
	kp_sanitized_env "$@"
}

run_kind() {
	kp_run_kind "$KIND_PROVIDER" "$@"
}

run_podman() {
	kp_run_podman "$@"
}

run_kubectl() {
	[[ -n "$kubeconfig_path" ]] || { printf '%s\n' 'E2E kubectl invoked before kubeconfig creation' >&2; return 70; }
	kp_run_kubectl "$kubeconfig_path" "$kube_context" "$@"
}

run_helm() {
	[[ -n "$kubeconfig_path" ]] || { printf '%s\n' 'E2E Helm invoked before kubeconfig creation' >&2; return 70; }
	kp_run_helm "$kubeconfig_path" "$kube_context" "$owned_dir/helm/config" \
		"$owned_dir/helm/cache" "$owned_dir/helm/data" "$@"
}

run_curl() {
	kp_run_curl "$@"
}

require_prerequisite() {
	local command_name="$1"
	if ! command -v "$command_name" >/dev/null 2>&1; then
		printf 'E2E prerequisite missing: %s\n' "$command_name" >&2
		return 1
	fi
}

fail_setup() {
	printf 'E2E setup failed during %s: %s\n' "$diagnostic_phase" "$1" >&2
	return 1
}

preflight() {
	local command_name output
	for command_name in go git kind podman kubectl helm curl; do
		require_prerequisite "$command_name" || return 1
	done

	case "$KUBERNETES_VERSION" in
	1.35.6) kind_node_image="${KIND_NODE_IMAGE_1_35_6:-kindest/node:v1.35.5}" ;;
	1.36.2) kind_node_image="${KIND_NODE_IMAGE_1_36_2:-kindest/node:v1.36.1}" ;;
	*)
		printf 'E2E unsupported Kubernetes version: %s (supported: 1.35.6 1.36.2)\n' \
			"$KUBERNETES_VERSION" >&2
		return 1
		;;
	esac

	if ! output="$(sanitized_env go version 2>&1)"; then
		printf 'E2E prerequisite failed: go version\n%s\n' "$output" >&2
		return 1
	fi
	if ! output="$(sanitized_env git --version 2>&1)"; then
		printf 'E2E prerequisite failed: git --version\n%s\n' "$output" >&2
		return 1
	fi
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
	if ! output="$(run_helm version --short 2>&1)"; then
		printf 'E2E prerequisite failed: helm version\n%s\n' "$output" >&2
		return 1
	fi
	if ! output="$(sanitized_env curl --version 2>&1)"; then
		printf 'E2E prerequisite failed: curl --version\n%s\n' "$output" >&2
		return 1
	fi
}

capture_worktree_snapshot() {
	local destination="$1"
	mkdir -p -- "$destination"
	git -C "$ROOT_DIR" status --porcelain=v1 --untracked-files=all >"$destination/status"
	git -C "$ROOT_DIR" diff --no-ext-diff --binary HEAD >"$destination/diff"
	git -C "$ROOT_DIR" diff --cached --no-ext-diff --binary >"$destination/cached-diff"
	git -C "$ROOT_DIR" rev-parse --verify HEAD >"$destination/revision"
	{
		cat "$destination/status" "$destination/diff" "$destination/cached-diff"
		cat "$destination/revision"
	} | sha256sum | awk '{print $1}' >"$destination/identity"
}

worktree_unchanged() {
	local before="$owned_dir/worktree.before" after="$owned_dir/worktree.after"
	capture_worktree_snapshot "$after"
	cmp -s "$before/status" "$after/status" &&
		cmp -s "$before/diff" "$after/diff" &&
		cmp -s "$before/cached-diff" "$after/cached-diff" &&
		cmp -s "$before/revision" "$after/revision" &&
		cmp -s "$before/identity" "$after/identity"
}

record_metadata() {
	{
		printf 'cluster_name=%s\n' "$cluster_name"
		printf 'kube_context=%s\n' "$kube_context"
		printf 'kubernetes_version=%s\n' "$KUBERNETES_VERSION"
		printf 'kind_node_image=%s\n' "${kind_node_image:-<unmapped>}"
		printf 'cert_manager_version=%s\n' "$CERT_MANAGER_VERSION"
		printf 'namespace=%s\n' "$E2E_NAMESPACE"
		printf 'release=%s\n' "$E2E_RELEASE"
		printf 'source_revision=%s\n' "$(<"$owned_dir/worktree.before/revision")"
		printf 'source_identity=%s\n' "$(<"$owned_dir/worktree.before/identity")"
	} >"$owned_dir/run-metadata"
}

write_admission_fixtures() {
	cat >"$owned_dir/admission-valid.yaml" <<EOF
apiVersion: kubeseer.io/v1alpha1
kind: Kubeseer
metadata:
  name: e2e-admission-probe
  namespace: $E2E_NAMESPACE
spec:
  sources: []
EOF
	cat >"$owned_dir/admission-invalid.yaml" <<EOF
apiVersion: kubeseer.io/v1alpha1
kind: Kubeseer
metadata:
  name: e2e-admission-invalid
  namespace: $E2E_NAMESPACE
spec:
  sources:
    - name: invalid
      kind: Unsupported
EOF
}

build_and_load_image() {
	diagnostic_phase='image-build'
	local revision image_tag image_ref image_id nodes node archive node_archive
	revision="$(<"$owned_dir/worktree.before/revision")"
	image_tag="${revision:0:12}-${cluster_name#kubeseer-e2e-}"
	image_tag="$(printf '%s' "$image_tag" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9_.-' '-')"
	image_ref="localhost/kubeseer-e2e:$image_tag"
	run_podman build --file "$ROOT_DIR/Dockerfile" \
		--tag "$image_ref" \
		--build-arg "VERSION=$revision" \
		--build-arg "COMMIT=$revision" \
		--build-arg "BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		"$ROOT_DIR" || return 1
	image_id="$(run_podman image inspect --format '{{.Id}}' "$image_ref")" || return 1
	[[ -n "$image_id" ]] || { printf '%s\n' 'Podman returned an empty image identity' >&2; return 1; }
	printf 'image_ref=%s\nimage_id=%s\n' "$image_ref" "$image_id" >>"$owned_dir/run-metadata"

	# kind's Podman loader in older kind releases cannot inspect images from
	# the rootless Podman store and its archive path can reject newer
	# containerd config versions. Import the exact OCI archive directly into
	# each node's k8s.io containerd namespace instead.
	archive="$owned_dir/kubeseer-image.oci.tar"
	run_podman save --format oci-archive --output "$archive" "$image_ref" || return 1
	[[ -s "$archive" ]] || { printf '%s\n' 'Podman produced an empty image archive' >&2; return 1; }

	# kind prints one node name per line for `get nodes`; unlike kubectl its
	# command does not accept the shorthand output flag on all supported kind
	# releases.
	nodes="$(run_kind get nodes --name "$cluster_name")" || return 1
	[[ -n "$nodes" ]] || { printf 'kind returned no nodes for %s\n' "$cluster_name" >&2; return 1; }
	while IFS= read -r node; do
		[[ -n "$node" ]] || continue
		node_archive="/tmp/kubeseer-e2e-${cluster_name}.oci.tar"
		run_podman cp "$archive" "$node:$node_archive" || return 1
		run_podman exec "$node" ctr --namespace k8s.io images import "$node_archive" || return 1
		run_podman exec "$node" rm -f "$node_archive" || return 1
	done <<<"$nodes"
}

install_cert_manager_and_chart() {
	diagnostic_phase='package-install'
	local cert_manifest="$owned_dir/cert-manager.yaml"
	local cert_url="https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
	local image_ref image_tag
	image_ref="$(sed -n 's/^image_ref=//p' "$owned_dir/run-metadata")"
	image_tag="${image_ref##*:}"

	run_curl --fail --silent --show-error --location --retry 3 --output "$cert_manifest" "$cert_url" || return 1
	[[ -s "$cert_manifest" ]] || { printf '%s\n' 'cert-manager manifest is empty' >&2; return 1; }
	run_kubectl apply --server-side --field-manager=kubeseer-e2e-cert-manager -f "$cert_manifest" || return 1
	run_kubectl wait --for=condition=Established --timeout=5m \
		crd/certificates.cert-manager.io crd/issuers.cert-manager.io crd/clusterissuers.cert-manager.io || return 1
	run_kubectl --namespace cert-manager wait --for=condition=Available --timeout=10m deployment --all || return 1

	[[ -f "$E2E_LIMIT_PROFILE" ]] || { printf 'E2E limit profile is missing: %s\n' "$E2E_LIMIT_PROFILE" >&2; return 1; }
	run_helm upgrade --install "$E2E_RELEASE" "$ROOT_DIR/charts/kubeseer" \
		--namespace "$E2E_NAMESPACE" --create-namespace \
		--values "$E2E_LIMIT_PROFILE" \
		--set "image.repository=${image_ref%:*}" \
		--set "image.tag=$image_tag" \
		--set image.pullPolicy=IfNotPresent \
		--wait --timeout 10m || return 1
}

port_forward_loop() {
	local service="$1" local_port="$2" remote_port="$3" forward_log="$4"
	local child_pid='' child_status=0
	trap 'if [[ -n "${child_pid:-}" ]]; then kill "$child_pid" 2>/dev/null || true; fi; exit 143' TERM INT
	while [[ ! -e "$owned_dir/port-forward.stop" ]]; do
		run_kubectl --namespace "$E2E_NAMESPACE" port-forward "$service" "$local_port:$remote_port" >>"$forward_log" 2>&1 &
		child_pid=$!
		set +e
		wait "$child_pid"
		child_status=$?
		set -e
		child_pid=''
		[[ -e "$owned_dir/port-forward.stop" ]] && break
		# A service port-forward is tied to the selected Pod. Reconnect after a
		# manager replacement while keeping the retry bounded by harness teardown.
		sleep 1
	done
	return "$child_status"
}

wait_for_http() {
	local service="$1" local_port="$2" remote_port="$3" path="$4" expected="$5" reconnect="${6:-}"
	local forward_pid='' body='' forward_log="$owned_dir/diagnostics/port-forward-${local_port}.log"
	mkdir -p -- "$owned_dir/diagnostics"
	if [[ "$reconnect" == reconnect ]]; then
		port_forward_loop "$service" "$local_port" "$remote_port" "$forward_log" &
	else
		(run_kubectl --namespace "$E2E_NAMESPACE" port-forward "$service" "$local_port:$remote_port" >"$forward_log" 2>&1) &
	fi
	forward_pid=$!
	port_forward_pids+=("$forward_pid")
	local attempt
	for attempt in {1..30}; do
		if body="$(run_curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:${local_port}${path}" 2>/dev/null)"; then
			if [[ -z "$expected" || "$body" == *"$expected"* ]]; then
				return 0
			fi
		fi
		run_curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:${local_port}${path}" >/dev/null 2>&1 || true
	done
	printf 'HTTP readiness failed for %s%s\n' "$service" "$path" >&2
	return 1
}

package_readiness() {
	diagnostic_phase='readiness'
	run_kubectl wait --for=condition=Established --timeout=5m \
		crd/kubeseers.kubeseer.io crd/kubeseeraccesspolicies.kubeseer.io || return 1
	run_kubectl --namespace "$E2E_NAMESPACE" wait --for=condition=Available \
		"deployment/$E2E_RELEASE" --timeout=10m || return 1
	run_kubectl wait --for=condition=Ready --timeout=10m \
		--namespace "$E2E_NAMESPACE" "certificate/${E2E_RELEASE}-webhook-serving" || return 1

	local ca_bundles
	ca_bundles="$(run_kubectl get validatingwebhookconfiguration kubeseer-validating-webhook \
		-o jsonpath='{range .webhooks[*]}{.clientConfig.caBundle}{"\n"}{end}')" || return 1
	if [[ -z "$ca_bundles" ]]; then
		printf '%s\n' 'validating webhook CA bundle is empty' >&2
		return 1
	fi
	while IFS= read -r ca_bundle; do
		if [[ -z "${ca_bundle//[[:space:]]/}" ]]; then
			printf '%s\n' 'validating webhook CA bundle is empty' >&2
			return 1
		fi
	done <<<"$ca_bundles"

	run_kubectl get endpoints "${E2E_RELEASE}-webhook" -n "$E2E_NAMESPACE" -o wide >/dev/null || return 1
	run_kubectl get kubeseeraccesspolicy installation-access-ceiling -o name >/dev/null || return 1
	write_admission_fixtures
	run_kubectl apply --dry-run=server -f "$owned_dir/admission-valid.yaml" >/dev/null || return 1
	if run_kubectl apply --dry-run=server -f "$owned_dir/admission-invalid.yaml" >/dev/null 2>&1; then
		printf '%s\n' 'forbidden admission probe was unexpectedly accepted' >&2
		return 1
	fi
	wait_for_http "deployment/$E2E_RELEASE" 18081 8081 /readyz 'ok' || return 1
	# The endpoint must be reachable before any reconciliation has emitted
	# Kubeseer-labelled counters; assert the Prometheus exposition header rather
	# than a product metric that is legitimately absent on a fresh install.
	wait_for_http "service/${E2E_RELEASE}-metrics" 18080 8080 /metrics '# HELP' reconnect || return 1
}

collect_diagnostics() {
	((cluster_owned)) || return 0
	[[ -n "$kubeconfig_path" && -s "$kubeconfig_path" ]] || return 0
	local destination="$owned_dir/diagnostics/failure.txt"
	mkdir -p -- "$owned_dir/diagnostics"
	{
		printf 'phase=%s\n' "$diagnostic_phase"
		printf 'cluster=%s\n' "$cluster_name"
		printf 'context=%s\n' "$kube_context"
		printf 'kubernetes_version=%s\n' "$KUBERNETES_VERSION"
		printf 'source_revision=%s\n' "$(<"$owned_dir/worktree.before/revision")"
		run_kubectl get nodes -o custom-columns='NAME:.metadata.name,STATUS:.status.conditions[-1].type' --no-headers 2>/dev/null || true
		run_kubectl --namespace "$E2E_NAMESPACE" get deployment,pods -o custom-columns='KIND:.kind,NAME:.metadata.name,READY:.status.conditions[-1].type' --no-headers 2>/dev/null || true
		run_kubectl get events --all-namespaces --sort-by=.lastTimestamp \
		-o custom-columns='NAMESPACE:.metadata.namespace,REASON:.reason,TYPE:.type,LAST:.lastTimestamp' --no-headers 2>/dev/null || true
	} >"$destination"
	# Diagnostics are an allowlisted projection; never print their file contents
	# here because the kubeconfig and provider logs may contain credentials.
	printf 'E2E diagnostics retained at %s\n' "$destination" >&2
}

stop_port_forwards() {
	local pid
	if [[ -n "$owned_dir" ]]; then
		: >"$owned_dir/port-forward.stop"
	fi
	for pid in "${port_forward_pids[@]}"; do
		kill "$pid" 2>/dev/null || true
	done
	port_forward_pids=()
}

cleanup() {
	local exit_status=$?
	local cleanup_status=0
	local command_status
	trap - EXIT
	stop_port_forwards

	if ((exit_status != 0)); then
		collect_diagnostics || true
	fi

	if ((cluster_owned)); then
		command_status=0
		run_kind delete cluster --name "$cluster_name" --kubeconfig "$kubeconfig_path" || command_status=$?
		if ((command_status != 0)); then
			cleanup_status=$command_status
			printf 'E2E cleanup failed for owned cluster %s; kubeconfig retained at %s\n' \
				"$cluster_name" "$kubeconfig_path" >&2
		fi
	fi

	if [[ -n "$owned_dir" && $cleanup_status -eq 0 ]]; then
		if ! worktree_unchanged; then
			cleanup_status=1
			printf 'E2E worktree changed during run; temporary path retained at %s\n' "$owned_dir" >&2
		fi
	fi

	if ((cleanup_status == 0)) && [[ -n "$owned_dir" ]]; then
		command_status=0
		# Go's module proxy preserves read-only source permissions. Restore
		# ownership permissions before removing the run-owned cache.
		chmod -R u+w -- "$owned_dir" 2>/dev/null || true
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

before_parent="${TMPDIR:-/tmp}"
owned_dir="$(mktemp -d "$before_parent/kubeseer-e2e.XXXXXXXX")"
mkdir -p -- "$owned_dir"/{worktree.before,diagnostics,helm/{config,cache,data},go-cache,go-mod-cache,go-path}
capture_worktree_snapshot "$owned_dir/worktree.before"
prepare_go_environment

run_identity="$(date -u +%Y%m%d%H%M%S)-$$"
cluster_name="kubeseer-e2e-${run_identity}"
cluster_name="${cluster_name:0:63}"
cluster_name="${cluster_name%-}"
kube_context="kind-${cluster_name}"
kubeconfig_path="$owned_dir/kubeconfig"
diagnostic_phase='probe'
record_metadata

set +e
probe_output="$(sanitized_env \
	KUBECONFIG= USE_EXISTING_CLUSTER= \
	GOCACHE="$owned_dir/go-cache" GOMODCACHE="$owned_dir/go-mod-cache" GOPATH="$owned_dir/go-path" \
	GOPROXY="$e2e_go_proxy" \
	GO_TEST_FLAGS="${GO_TEST_FLAGS:-}" \
		"$TEST_LAYER_RUNNER" end-to-end "$PACKAGE_PATTERN" "$TEST_REGEX" probe 2>&1)"
probe_status=$?
set -e
printf '%s\n' "$probe_output"
if ((probe_status != 0)); then
	exit "$probe_status"
fi

diagnostic_phase='preflight'
preflight || exit 1
record_metadata

diagnostic_phase='cluster-create'
cluster_owned=1
run_kind create cluster --name "$cluster_name" --image "$kind_node_image" \
	--wait 5m --kubeconfig "$kubeconfig_path" || fail_setup 'kind create failed'
[[ -s "$kubeconfig_path" ]] || fail_setup "kind did not write $kubeconfig_path"

build_and_load_image || fail_setup 'production image build/load failed'
install_cert_manager_and_chart || fail_setup 'cert-manager or chart installation failed'
package_readiness || fail_setup 'package readiness gate failed'

diagnostic_phase='scenario-run'
sanitized_env \
	KUBECONFIG="$kubeconfig_path" \
	KUBESEER_E2E_KUBECONFIG="$kubeconfig_path" \
	KUBESEER_E2E_CLUSTER_NAME="$cluster_name" \
	KUBESEER_E2E_CONTEXT="$kube_context" \
	KUBESEER_E2E_KUBERNETES_VERSION="$KUBERNETES_VERSION" \
	KUBESEER_E2E_CERT_MANAGER_VERSION="$CERT_MANAGER_VERSION" \
	KUBESEER_E2E_NAMESPACE="$E2E_NAMESPACE" \
	KUBESEER_E2E_RELEASE="$E2E_RELEASE" \
	KUBESEER_E2E_METRICS_URL="http://127.0.0.1:18080" \
	KUBESEER_E2E_IMAGE_REF="$(sed -n 's/^image_ref=//p' "$owned_dir/run-metadata")" \
	KUBESEER_E2E_SOURCE_REVISION="$(<"$owned_dir/worktree.before/revision")" \
	KUBESEER_E2E_SOURCE_IDENTITY="$(<"$owned_dir/worktree.before/identity")" \
	KUBESEER_E2E_DIAGNOSTICS="$owned_dir/diagnostics" \
	GOCACHE="$owned_dir/go-cache" GOMODCACHE="$owned_dir/go-mod-cache" GOPATH="$owned_dir/go-path" \
	GOPROXY="$e2e_go_proxy" \
	GO_TEST_FLAGS="${GO_TEST_FLAGS:-}" \
		"$TEST_LAYER_RUNNER" end-to-end "$PACKAGE_PATTERN" "$TEST_REGEX" required
