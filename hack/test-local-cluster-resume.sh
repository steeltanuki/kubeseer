#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly ORIGINAL_PATH="$PATH"
readonly RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-local-resume.XXXXXX")"
readonly RUN_ID="$(printf '%s' "${RUN_DIR##*.}" | tr '[:upper:]' '[:lower:]')"
readonly FIXTURE_CLUSTER_NAME="kubeseer-resume-${RUN_ID}"
readonly FIXTURE_CONTEXT="kind-${FIXTURE_CLUSTER_NAME}"
readonly LOCAL_NAMESPACE="kubeseer-resume-proof"
readonly CONFIGMAP_NAME="resume-persistence-proof"
readonly STATE_DIR="$RUN_DIR/state"
readonly CACHE_DIR="$RUN_DIR/cache"
readonly BIN_DIR="$RUN_DIR/bin"
readonly PODMAN_TRACE="$RUN_DIR/podman-starts.trace"
readonly REAL_PODMAN_BINARY="$(command -v podman || true)"
cleanup_required=0
preserve_run_dir=0
readyz_port=''
metrics_port=''

mkdir -p -- "$STATE_DIR" "$CACHE_DIR" "$BIN_DIR"
: >"$PODMAN_TRACE"
[[ -n "$REAL_PODMAN_BINARY" && "$REAL_PODMAN_BINARY" == /* ]] || {
	printf '%s\n' 'LOCAL_CLUSTER_RESUME_ACCEPTANCE=genuine STATUS=failed: Podman is unavailable' >&2
	rm -rf -- "$RUN_DIR"
	exit 1
}

# Count only Podman CLI starts made by local-up; delegate every operation to
# the real host binary without changing its arguments.
cat >"$BIN_DIR/podman" <<'PODMAN_WRAPPER'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == container && "${2:-}" == start ]]; then
	printf '%s\n' "$*" >>"${PODMAN_CALL_TRACE_FILE:?}"
fi
exec "${PODMAN_REAL_BINARY:?}" "$@"
PODMAN_WRAPPER
chmod 700 -- "$BIN_DIR/podman"

port_is_free() {
	local candidate="$1"
	if (exec 3<>"/dev/tcp/127.0.0.1/$candidate") 2>/dev/null; then
		exec 3>&-
		return 1
	fi
	return 0
}
for ((attempt = 0; attempt < 100; attempt++)); do
	base_port=$((20000 + RANDOM % 35000))
	if ((base_port < 65534)) && port_is_free "$base_port" && port_is_free "$((base_port + 1))"; then
		readyz_port="$base_port"
		metrics_port="$((base_port + 1))"
		break
	fi
done
[[ -n "$readyz_port" && -n "$metrics_port" ]] || {
	printf '%s\n' 'LOCAL_CLUSTER_RESUME_ACCEPTANCE=genuine STATUS=failed: no free loopback ports for readiness probes' >&2
	rm -rf -- "$RUN_DIR"
	exit 1
}

run_make() {
	PATH="$BIN_DIR:$ORIGINAL_PATH" \
	PODMAN_REAL_BINARY="$REAL_PODMAN_BINARY" PODMAN_CALL_TRACE_FILE="$PODMAN_TRACE" \
	LOCAL_CLUSTER_NAME="$FIXTURE_CLUSTER_NAME" LOCAL_KUBE_CONTEXT="$FIXTURE_CONTEXT" \
	KUBESEER_LOCAL_STATE_DIR="$STATE_DIR" KUBESEER_LOCAL_CACHE_DIR="$CACHE_DIR" \
	KUBESEER_LOCAL_READYZ_PORT="$readyz_port" KUBESEER_LOCAL_METRICS_PORT="$metrics_port" \
	make --no-print-directory "$@"
}

metadata_value() {
	local key="$1"
	sed -n "s/^${key}=//p" "$STATE_DIR/metadata.v1"
}

prove_owned_container() {
	local expected_node="$FIXTURE_CLUSTER_NAME-control-plane"
	local nodes inspect_output container_id container_name container_state cluster_label role_label api_bindings
	local api_server api_port
	local -a inspection=()
	[[ -s "$STATE_DIR/metadata.v1" && -s "$STATE_DIR/kubeconfig" ]] || {
		printf '%s\n' 'owned state metadata or kubeconfig is missing' >&2
		return 1
	}
	[[ "$(metadata_value state)" == owned && "$(metadata_value cluster_name)" == "$FIXTURE_CLUSTER_NAME" &&
		"$(metadata_value context)" == "$FIXTURE_CONTEXT" && "$(metadata_value kubeconfig)" == "$STATE_DIR/kubeconfig" ]] || {
		printf '%s\n' 'run metadata does not prove ownership of the disposable cluster' >&2
		return 1
	}
	nodes="$(kp_kind_nodes podman "$FIXTURE_CLUSTER_NAME")"
	if ! printf '%s\n' "$nodes" | rg -F -x -q -- "$expected_node"; then
		printf 'kind does not report expected control-plane node %s\n' "$expected_node" >&2
		return 1
	fi
	inspect_output="$(kp_run_podman container inspect --format '{{.Id}}|{{.Name}}|{{.State.Status}}|{{index .Config.Labels "io.x-k8s.kind.cluster"}}|{{index .Config.Labels "io.x-k8s.kind.role"}}|{{range $binding := (index .HostConfig.PortBindings "6443/tcp")}}{{.HostIP}},{{.HostPort}};{{end}}' "$expected_node")"
	IFS='|' read -r -a inspection <<<"$inspect_output"
	[[ "${#inspection[@]}" == 6 ]] || { printf '%s\n' 'Podman returned incomplete control-plane identity' >&2; return 1; }
	container_id="${inspection[0]}"
	container_name="${inspection[1]#/}"
	container_state="${inspection[2]}"
	cluster_label="${inspection[3]}"
	role_label="${inspection[4]}"
	api_bindings="${inspection[5]}"
	[[ "$container_id" =~ ^[0-9a-f]{12,64}$ && "$container_name" == "$expected_node" &&
		"$container_state" == running && "$cluster_label" == "$FIXTURE_CLUSTER_NAME" && "$role_label" == control-plane ]] || {
		printf 'Podman identity does not prove the expected running node %s\n' "$expected_node" >&2
		return 1
	}
	api_server="$(kp_run_kubectl "$STATE_DIR/kubeconfig" "$FIXTURE_CONTEXT" config view --minify -o 'jsonpath={.clusters[0].cluster.server}')"
	[[ "$api_server" =~ ^https://127\.([0-9]{1,3}\.){2}[0-9]{1,3}:([0-9]{1,5})$ ]] || {
		printf '%s\n' 'owned kubeconfig API server is not an explicit IPv4 loopback endpoint' >&2
		return 1
	}
	api_port="${BASH_REMATCH[2]}"
	[[ "$api_bindings" == "127.0.0.1,${api_port};" ]] || {
		printf '%s\n' 'owned kubeconfig endpoint does not match the control-plane API port binding' >&2
		return 1
	}
	printf '%s\n' "$container_id"
}

configmap_identity() {
	kp_run_kubectl "$STATE_DIR/kubeconfig" "$FIXTURE_CONTEXT" --namespace "$LOCAL_NAMESPACE" \
		get configmap "$CONFIGMAP_NAME" -o 'jsonpath={.metadata.uid}|{.data.marker}'
}

cleanup() {
	local status=$? cleanup_output metadata_cluster metadata_context
	trap - EXIT
	if ((cleanup_required)) && [[ -s "$STATE_DIR/metadata.v1" ]]; then
		metadata_cluster="$(metadata_value cluster_name)"
		metadata_context="$(metadata_value context)"
		if [[ "$metadata_cluster" == "$FIXTURE_CLUSTER_NAME" && "$metadata_context" == "$FIXTURE_CONTEXT" ]]; then
			if cleanup_output="$(run_make local-down 2>&1)"; then
				printf '%s\n' "$cleanup_output"
				cleanup_required=0
			else
				printf '%s\n' "$cleanup_output" >&2
				preserve_run_dir=1
			fi
		else
			printf '%s\n' 'refusing cleanup because saved metadata does not match the run-unique cluster' >&2
			preserve_run_dir=1
		fi
	fi
	if ((preserve_run_dir)); then
		printf 'LOCAL_CLUSTER_RESUME_CLEANUP=retained state=%s cluster=%s context=%s command="LOCAL_CLUSTER_NAME=%s LOCAL_KUBE_CONTEXT=%s KUBESEER_LOCAL_STATE_DIR=%s KUBESEER_LOCAL_CACHE_DIR=%s make local-down"\n' \
			"$STATE_DIR" "$FIXTURE_CLUSTER_NAME" "$FIXTURE_CONTEXT" "$FIXTURE_CLUSTER_NAME" "$FIXTURE_CONTEXT" "$STATE_DIR" "$CACHE_DIR" >&2
		((status != 0)) || status=1
	else
		chmod -R u+w -- "$RUN_DIR" 2>/dev/null || true
		rm -rf -- "$RUN_DIR"
	fi
	exit "$status"
}
trap cleanup EXIT

if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 ]]; then
	printf '%s\n' 'LOCAL_CLUSTER_RESUME_ACCEPTANCE=genuine STATUS=failed: proof requires Linux/amd64' >&2
	exit 1
fi
if [[ "$("$REAL_PODMAN_BINARY" info --format '{{.Host.Security.Rootless}}' 2>/dev/null || true)" != true ]]; then
	printf '%s\n' 'LOCAL_CLUSTER_RESUME_ACCEPTANCE=genuine STATUS=failed: rootless Podman is unavailable' >&2
	exit 1
fi

source "$ROOT_DIR/hack/kind-podman-common.sh"
existing_clusters="$(kp_run_kind podman get clusters)"
if printf '%s\n' "$existing_clusters" | rg -F -x -q -- "$FIXTURE_CLUSTER_NAME"; then
	printf 'LOCAL_CLUSTER_RESUME_ACCEPTANCE=genuine STATUS=failed: run-unique cluster already exists: %s\n' "$FIXTURE_CLUSTER_NAME" >&2
	exit 1
fi
readonly BASELINE_CLUSTERS="$(printf '%s\n' "$existing_clusters" | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)"
readonly BASELINE_CONTAINERS="$(kp_run_podman ps --all --no-trunc --format '{{.ID}}' | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)"
readonly BASELINE_NETWORKS="$(kp_run_podman network ls --format '{{.ID}}|{{.Name}}' | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)"
readonly BASELINE_VOLUMES="$(kp_run_podman volume ls --format '{{.Name}}' | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)"

cleanup_required=1
if ! output="$(run_make local-up 2>&1)"; then
	printf '%s\n' "$output" >&2
	exit 1
fi
[[ "$output" == *'LOCAL_ENVIRONMENT=up STATUS=passed'* ]] || {
	printf 'initial local-up did not pass:\n%s\n' "$output" >&2
	exit 1
}
container_id="$(prove_owned_container)"

run_make local-check >/dev/null
run_make local-status >/dev/null
kp_run_kubectl "$STATE_DIR/kubeconfig" "$FIXTURE_CONTEXT" create namespace "$LOCAL_NAMESPACE" >/dev/null
kp_run_kubectl "$STATE_DIR/kubeconfig" "$FIXTURE_CONTEXT" --namespace "$LOCAL_NAMESPACE" \
	create configmap "$CONFIGMAP_NAME" --from-literal=marker='owned-resume-workload-data' >/dev/null
before_workload="$(configmap_identity)"
[[ "$before_workload" == *'|owned-resume-workload-data' ]] || {
	printf 'workload fixture did not retain its expected data: %s\n' "$before_workload" >&2
	exit 1
}
cp -- "$STATE_DIR/kubeconfig" "$RUN_DIR/kubeconfig.before"

# Stop only the immutable container ID whose metadata, kind label, node role,
# inventory entry, and published API endpoint have been checked above.
[[ "$container_id" == "$(prove_owned_container)" ]] || {
	printf '%s\n' 'control-plane container ID changed before stop' >&2
	exit 1
}
kp_run_podman container stop "$container_id" >/dev/null
stopped_inspection="$(kp_run_podman container inspect --format '{{.Id}}|{{.State.Status}}' "$container_id")"
[[ "$stopped_inspection" == "$container_id|exited" ]] || {
	printf 'owned control-plane did not reach exited state: %s\n' "$stopped_inspection" >&2
	exit 1
}
if [[ "$(prove_owned_container 2>/dev/null || true)" == "$container_id" ]]; then
	printf '%s\n' 'ownership proof unexpectedly accepted an exited node as running' >&2
	exit 1
fi

: >"$PODMAN_TRACE"
if ! output="$(run_make local-up 2>&1)"; then
	printf '%s\n' "$output" >&2
	exit 1
fi
[[ "$output" == *'LOCAL_ENVIRONMENT=up STATUS=passed'* ]] || {
	printf 'resumed local-up did not pass:\n%s\n' "$output" >&2
	exit 1
}
[[ "$(wc -l <"$PODMAN_TRACE" | tr -d '[:space:]')" == 1 && "$(<"$PODMAN_TRACE")" == "container start $container_id" ]] || {
	printf 'resume did not issue exactly one start for owned container %s\n' "$container_id" >&2
	exit 1
}
[[ "$(prove_owned_container)" == "$container_id" ]] || {
	printf '%s\n' 'resumption replaced the owned control-plane container' >&2
	exit 1
}
cmp -s "$RUN_DIR/kubeconfig.before" "$STATE_DIR/kubeconfig" || {
	printf '%s\n' 'resume changed the saved kubeconfig' >&2
	exit 1
}
kp_run_kubectl "$STATE_DIR/kubeconfig" "$FIXTURE_CONTEXT" wait --for=condition=Ready \
	"node/$FIXTURE_CLUSTER_NAME-control-plane" --timeout=5m >/dev/null
after_workload="$(configmap_identity)"
[[ "$after_workload" == "$before_workload" ]] || {
	printf 'workload ConfigMap identity/data changed across resume: before=%s after=%s\n' "$before_workload" "$after_workload" >&2
	exit 1
}

# The healthy repeat path must not issue a second Podman start.
: >"$PODMAN_TRACE"
if ! output="$(run_make local-up 2>&1)"; then
	printf '%s\n' "$output" >&2
	exit 1
fi
[[ "$output" == *'LOCAL_ENVIRONMENT=up STATUS=passed'* && ! -s "$PODMAN_TRACE" ]] || {
	printf '%s\n' 'repeat local-up did not converge without a second container start' >&2
	exit 1
}
[[ "$(prove_owned_container)" == "$container_id" ]] || {
	printf '%s\n' 'repeat local-up changed the owned control-plane identity' >&2
	exit 1
}

if ! output="$(run_make local-down 2>&1)"; then
	printf '%s\n' "$output" >&2
	exit 1
fi
printf '%s\n' "$output"
cleanup_required=0
remaining_clusters="$(kp_run_kind podman get clusters | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)"
remaining_containers="$(kp_run_podman ps --all --no-trunc --format '{{.ID}}' | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)"
remaining_networks="$(kp_run_podman network ls --format '{{.ID}}|{{.Name}}' | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)"
remaining_volumes="$(kp_run_podman volume ls --format '{{.Name}}' | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)"
[[ "$remaining_clusters" == "$BASELINE_CLUSTERS" ]] || {
	printf 'kind cluster inventory changed after cleanup: before=%s after=%s\n' "$BASELINE_CLUSTERS" "$remaining_clusters" >&2
	exit 1
}
[[ "$remaining_containers" == "$BASELINE_CONTAINERS" ]] || {
	printf '%s\n' 'Podman container inventory changed after the isolated resume proof' >&2
	exit 1
}
[[ "$remaining_networks" == "$BASELINE_NETWORKS" ]] || {
	printf '%s\n' 'Podman network inventory changed after the isolated resume proof' >&2
	exit 1
}
[[ "$remaining_volumes" == "$BASELINE_VOLUMES" ]] || {
	printf '%s\n' 'Podman volume inventory changed after the isolated resume proof' >&2
	exit 1
}
printf 'LOCAL_CLUSTER_RESUME_ACCEPTANCE=genuine STATUS=passed\n'
