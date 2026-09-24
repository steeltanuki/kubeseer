#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

if (($# != 1)); then
	printf 'usage: %s <preflight|ownership|resume-ownership|resume|package|examples|verification|diagnostics|complete>\n' "$0" >&2
	exit 64
fi
readonly MODE="$1"
readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly ORIGINAL_PATH="$PATH"
readonly FIXTURE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-local-acceptance.XXXXXX")"
readonly BIN_DIR="$FIXTURE_DIR/bin"
readonly STATE_DIR="$FIXTURE_DIR/state"
readonly CACHE_DIR="$FIXTURE_DIR/cache"
readonly TRACE_FILE="$FIXTURE_DIR/tools.trace"
readonly CLUSTER_MARKER="$FIXTURE_DIR/cluster.present"
readonly CONTAINER_ID_FILE="$FIXTURE_DIR/container.id"
readonly CONTAINER_NAME_FILE="$FIXTURE_DIR/container.name"
readonly CONTAINER_STATE_FILE="$FIXTURE_DIR/container.state"
readonly CLUSTER_LABEL_FILE="$FIXTURE_DIR/container.cluster-label"
readonly ROLE_LABEL_FILE="$FIXTURE_DIR/container.role-label"
readonly API_BINDING_FILE="$FIXTURE_DIR/container.api-binding"
readonly API_AVAILABLE_FILE="$FIXTURE_DIR/api.available"
readonly API_NODES_FILE="$FIXTURE_DIR/api.nodes"
readonly API_ATTEMPTS_FILE="$FIXTURE_DIR/api.attempts"
readonly CONTAINER_STARTS_FILE="$FIXTURE_DIR/container.starts"

cleanup() { chmod -R u+w "$FIXTURE_DIR" 2>/dev/null || true; rm -rf -- "$FIXTURE_DIR"; }
trap cleanup EXIT
mkdir -p -- "$BIN_DIR" "$STATE_DIR" "$CACHE_DIR"
: >"$TRACE_FILE"

cat >"$BIN_DIR/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == version ]]; then printf '%s\n' 'go version go1.26.7 linux/amd64'; exit 0; fi
printf 'go args=%s\n' "$*" >>"${TRACE_FILE:?}"
if [[ "$*" == *'./cmd/kubeseer-local readiness'* ]]; then printf '%s\n' '{"phase":"readiness","status":"passed"}'; exit 0; fi
if [[ "$*" == *'./cmd/kubeseer-local status'* ]]; then printf '%s\n' '{"cluster":{"clusterName":"kubeseer-local","context":"kind-kubeseer-local"}}'; exit 0; fi
if [[ "$*" == *'./cmd/kubeseer-local diagnostics'* ]]; then
  destination=''; previous=''
  for argument in "$@"; do if [[ "$previous" == --destination ]]; then destination="$argument"; fi; previous="$argument"; done
  [[ -n "$destination" ]] || exit 71
  mkdir -p "$destination"; printf '%s\n' '{"schemaVersion":1,"completedSections":["versions","ownership"]}' >"$destination/manifest.json"
  printf 'LOCAL_DIAGNOSTICS=%s\n' "$destination"; exit 0
fi
if [[ "$*" == *'./cmd/kubeseer-local verify'* ]]; then
  example=''; previous=''
  for argument in "$@"; do if [[ "$previous" == --example ]]; then example="$argument"; fi; previous="$argument"; done
  if [[ -n "$example" ]]; then printf 'EXAMPLE=%s STATUS=passed\n' "$example"; else for example in builtin-resource typed-extraction value-operator cross-namespace-aggregation custom-resource authorization-denial partial-degradation; do printf 'EXAMPLE=%s STATUS=passed\n' "$example"; done; fi
  exit 0
fi
exec /usr/bin/go "$@"
EOF

cat >"$BIN_DIR/git" <<'EOF'
#!/usr/bin/env bash
exec /usr/bin/git "$@"
EOF

cat >"$BIN_DIR/make" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == --version ]]; then printf '%s\n' 'GNU Make 4.4'; else exec /usr/bin/make "$@"; fi
EOF

cat >"$BIN_DIR/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'podman provider=%s kubeconfig=%s args=%s\n' "${KIND_EXPERIMENTAL_PROVIDER-<unset>}" "${KUBECONFIG-<unset>}" "$*" >>"${TRACE_FILE:?}"
if [[ "${1:-}" == --version ]]; then printf '%s\n' 'podman version 5.8.4'; exit 0; fi
if [[ "${PODMAN_FAIL_INFO-}" == 1 && "${1:-}" == info ]]; then exit 41; fi
if [[ "${1:-}" == info ]]; then printf '%s\n' 'true'; exit 0; fi
if [[ "${1:-}" == container && "${2:-}" == inspect ]]; then
  if [[ "${PODMAN_INSPECT_FAIL-}" == 1 || ! -s "${CLUSTER_MARKER:?}" || ! -s "${CONTAINER_ID_FILE:?}" ]]; then
    printf '%s\n' 'fixture container inspection unavailable' >&2
    exit 45
  fi
  target="${@: -1}"
  expected_target="$(<"$CLUSTER_MARKER")-control-plane"
  if [[ "$target" != "$expected_target" && "$target" != "$(<"$CONTAINER_ID_FILE")" ]]; then
    printf 'fixture container name mismatch: target=%s expected=%s\n' "$target" "$expected_target" >&2
    exit 46
  fi
  printf '%s|/%s|%s|%s|%s|%s\n' "$(<"$CONTAINER_ID_FILE")" "$(<"$CONTAINER_NAME_FILE")" \
    "$(<"$CONTAINER_STATE_FILE")" "$(<"$CLUSTER_LABEL_FILE")" "$(<"$ROLE_LABEL_FILE")" "$(<"$API_BINDING_FILE")"
  exit 0
fi
if [[ "${1:-}" == container && "${2:-}" == start ]]; then
  target="${@: -1}"
  if [[ "${PODMAN_FAIL_START-}" == 1 ]]; then printf '%s\n' 'fixture Podman start failure' >&2; exit 50; fi
  [[ "$target" == "$(<"$CONTAINER_ID_FILE")" ]] || { printf 'fixture start target mismatch: %s\n' "$target" >&2; exit 51; }
  starts=$(( $(<"$CONTAINER_STARTS_FILE") + 1 ))
  printf '%s\n' "$starts" >"$CONTAINER_STARTS_FILE"
  printf '%s\n' running >"$CONTAINER_STATE_FILE"
  if [[ -n "${PODMAN_START_BINDING-}" ]]; then printf '%s\n' "$PODMAN_START_BINDING" >"$API_BINDING_FILE"; fi
  exit 0
fi
if [[ "${PODMAN_FAIL_BUILD-}" == 1 && "${1:-}" == build ]]; then exit 44; fi
if [[ "${1:-}" == build ]]; then printf '%s\n' 'built'; exit 0; fi
if [[ "${1:-}" == image && "${2:-}" == inspect ]]; then printf '%s\n' 'sha256:local-immutable-image'; exit 0; fi
if [[ "${1:-}" == save ]]; then output=''; previous=''; for argument in "$@"; do if [[ "$previous" == --output ]]; then output="$argument"; fi; previous="$argument"; done; printf '%s\n' 'oci archive' >"$output"; exit 0; fi
exit 0
EOF

cat >"$BIN_DIR/kind" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'kind provider=%s kubeconfig=%s args=%s\n' "${KIND_EXPERIMENTAL_PROVIDER-<unset>}" "${KUBECONFIG-<unset>}" "$*" >>"${TRACE_FILE:?}"
if [[ "${1:-}" == version ]]; then printf '%s\n' 'kind v0.27.0'; exit 0; fi
if [[ "${1:-}" == get && "${2:-}" == clusters ]]; then [[ -s "${CLUSTER_MARKER:?}" ]] && cat "$CLUSTER_MARKER"; exit 0; fi
if [[ "${1:-}" == get && "${2:-}" == nodes ]]; then
  if [[ -s "${CLUSTER_MARKER:?}" && "${KIND_FIXTURE_NODE_MISSING-}" != 1 ]]; then printf '%s-control-plane\n' "$(<"$CLUSTER_MARKER")"; fi
  exit 0
fi
if [[ "${KIND_FAIL_CREATE-}" == 1 && "${1:-}" == create ]]; then exit 42; fi
if [[ "${1:-}" == create ]]; then
  kubeconfig=''; cluster=''; previous=''
  for argument in "$@"; do
    if [[ "$previous" == --kubeconfig ]]; then kubeconfig="$argument"; fi
    if [[ "$previous" == --name ]]; then cluster="$argument"; fi
    previous="$argument"
  done
  [[ -n "$cluster" && -n "$kubeconfig" ]] || exit 48
  printf '%s\n' "$cluster" >"$CLUSTER_MARKER"
  cat >"$kubeconfig" <<KIND_KUBECONFIG
apiVersion: v1
clusters:
- cluster:
    server: https://127.0.0.1:35679
  name: kind-$cluster
contexts:
- context:
    cluster: kind-$cluster
    user: kind-$cluster
  name: kind-$cluster
current-context: kind-$cluster
users:
- name: kind-$cluster
  user: {}
KIND_KUBECONFIG
  printf '%s\n' '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef' >"$CONTAINER_ID_FILE"
  printf '%s-control-plane\n' "$cluster" >"$CONTAINER_NAME_FILE"
  printf '%s\n' running >"$CONTAINER_STATE_FILE"
  printf '%s\n' "$cluster" >"$CLUSTER_LABEL_FILE"
  printf '%s\n' control-plane >"$ROLE_LABEL_FILE"
  printf '%s\n' '127.0.0.1,35679;' >"$API_BINDING_FILE"
  printf '%s\n' 1 >"$API_AVAILABLE_FILE"
  printf '%s-control-plane\n' "$cluster" >"$API_NODES_FILE"
  printf '%s\n' 0 >"$API_ATTEMPTS_FILE"
  printf '%s\n' 0 >"$CONTAINER_STARTS_FILE"
  exit 0
fi
if [[ "${KIND_FAIL_DELETE-}" == 1 && "${1:-}" == delete ]]; then exit 43; fi
if [[ "${1:-}" == delete ]]; then rm -f "$CLUSTER_MARKER"; exit 0; fi
exit 0
EOF

cat >"$BIN_DIR/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl kubeconfig=%s args=%s\n' "${KUBECONFIG-<unset>}" "$*" >>"${TRACE_FILE:?}"
arguments=("$@")
kubeconfig=''
command=''
index=0
while ((index < ${#arguments[@]})); do
  case "${arguments[index]}" in
    --kubeconfig) kubeconfig="${arguments[index+1]:-}"; index=$((index + 2));;
    --context) index=$((index + 2));;
    --*) index=$((index + 1));;
    *) command="${arguments[index]}"; break;;
  esac
done
if [[ "$command" == version ]]; then
  if [[ "$*" == *--client=true* ]]; then printf '%s\n' '{"clientVersion":{"gitVersion":"v1.35.6"}}'; exit 0; fi
  if [[ -f "${API_AVAILABLE_FILE-}" && "$(<"$API_AVAILABLE_FILE")" != 1 ]]; then
    attempts=$(( $(<"$API_ATTEMPTS_FILE") + 1 ))
    printf '%s\n' "$attempts" >"$API_ATTEMPTS_FILE"
    if [[ "${API_APPEAR_AFTER:-0}" =~ ^[0-9]+$ ]] && ((API_APPEAR_AFTER > 0 && attempts >= API_APPEAR_AFTER)); then printf '%s\n' 1 >"$API_AVAILABLE_FILE"; else exit 45; fi
  fi
  printf '%s\n' '{"clientVersion":{"gitVersion":"v1.35.6"},"serverVersion":{"gitVersion":"v1.35.6"}}'; exit 0
fi
if [[ "$command" == config && "${arguments[index+1]:-}" == view ]]; then
  [[ -n "$kubeconfig" && -s "$kubeconfig" ]] || exit 48
  sed -n -E 's/^[[:space:]]*server:[[:space:]]*([^[:space:]]+).*$/\1/p' "$kubeconfig" | head -n 1
  exit 0
fi
if [[ "${KUBESEER_FAIL_CERT-}" == 1 && "$command" == apply && "$*" == *cert-manager* ]]; then exit 47; fi
if [[ "$command" == get && "${arguments[index+1]:-}" == nodes ]]; then cat "${API_NODES_FILE:?}"; exit 0; fi
if [[ "$command" == apply || "$command" == wait || "$command" == delete || "$command" == get || "$command" == create || "$command" == patch || "$command" == replace ]]; then exit 0; fi
if [[ "$command" == version ]]; then printf '%s\n' 'Client Version: v1.35.6'; exit 0; fi
exit 0
EOF

cat >"$BIN_DIR/helm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'helm kubeconfig=%s args=%s\n' "${KUBECONFIG-<unset>}" "$*" >>"${TRACE_FILE:?}"
if [[ "${1:-}" == version ]]; then printf '%s\n' 'v3.17.0+stub'; exit 0; fi
if [[ "${HELM_FAIL_INSTALL-}" == 1 && "${1:-}" == upgrade ]]; then exit 49; fi
exit 0
EOF

cat >"$BIN_DIR/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'curl args=%s\n' "$*" >>"${TRACE_FILE:?}"
if [[ "${1:-}" == --version ]]; then printf '%s\n' 'curl 8.18.0 (stub)'; exit 0; fi
output=''; previous=''; for argument in "$@"; do if [[ "$previous" == --output ]]; then output="$argument"; fi; previous="$argument"; done
if [[ -n "$output" ]]; then printf '%s\n' '# pinned cert-manager manifest' >"$output"; fi
url="${*: -1}"
if [[ "$url" == *'/readyz' ]]; then printf '%s\n' 'ok'; fi
if [[ "$url" == *'/metrics' ]]; then printf '%s\n' '# HELP kubeseer_reconciliations_total'; fi
EOF

chmod +x "$BIN_DIR"/*

run_local() {
	env PATH="$BIN_DIR:$ORIGINAL_PATH" TRACE_FILE="$TRACE_FILE" CLUSTER_MARKER="$CLUSTER_MARKER" \
		CONTAINER_ID_FILE="$CONTAINER_ID_FILE" CONTAINER_NAME_FILE="$CONTAINER_NAME_FILE" \
		CONTAINER_STATE_FILE="$CONTAINER_STATE_FILE" CLUSTER_LABEL_FILE="$CLUSTER_LABEL_FILE" \
		ROLE_LABEL_FILE="$ROLE_LABEL_FILE" API_BINDING_FILE="$API_BINDING_FILE" \
		API_AVAILABLE_FILE="$API_AVAILABLE_FILE" API_NODES_FILE="$API_NODES_FILE" \
		API_ATTEMPTS_FILE="$API_ATTEMPTS_FILE" CONTAINER_STARTS_FILE="$CONTAINER_STARTS_FILE" \
		KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS="${KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS:-}" \
		API_APPEAR_AFTER="${API_APPEAR_AFTER:-0}" PODMAN_FAIL_START="${PODMAN_FAIL_START:-0}" \
		PODMAN_START_BINDING="${PODMAN_START_BINDING:-}" PODMAN_INSPECT_FAIL="${PODMAN_INSPECT_FAIL:-0}" \
		KIND_FIXTURE_NODE_MISSING="${KIND_FIXTURE_NODE_MISSING:-0}" \
		KUBESEER_LOCAL_STATE_DIR="$STATE_DIR" KUBESEER_LOCAL_CACHE_DIR="$CACHE_DIR" \
		KUBECONFIG=/tmp/ambient-kubeconfig USE_EXISTING_CLUSTER=true AWS_SECRET_ACCESS_KEY=local-secret \
		"$ROOT_DIR/hack/local-environment.sh" "$@"
}

expect_failure() {
	local expected_pattern="$1"; shift
	local output status
	set +e
	output="$($@ 2>&1)"; status=$?
	set -e
	[[ "$status" != 0 ]] || { printf 'acceptance expected failure from %q\n' "$1" >&2; exit 1; }
	[[ -z "$expected_pattern" || "$output" == *"$expected_pattern"* ]] || { printf 'acceptance expected %q in failure:\n%s\n' "$expected_pattern" "$output" >&2; exit 1; }
	printf '%s' "$output"
}

assert_contains() { [[ "$1" == *"$2"* ]] || { printf 'acceptance expected %q in:\n%s\n' "$2" "$1" >&2; exit 1; }; }
assert_not_contains() { [[ "$1" != *"$2"* ]] || { printf 'acceptance did not expect %q in:\n%s\n' "$2" "$1" >&2; exit 1; }; }

preflight_checks() {
	local output
	output="$(run_local check 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=check STATUS=passed'; assert_not_contains "$(<"$TRACE_FILE")" 'create cluster'
	if run_local unsupported >/dev/null 2>&1; then printf '%s\n' 'unsupported action unexpectedly succeeded' >&2; exit 1; fi
}

workflow_checks() {
	local output
	: >"$TRACE_FILE"
	output="$(run_local up 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=up STATUS=passed'
	[[ -f "$STATE_DIR/metadata.v1" && -f "$STATE_DIR/kubeconfig" ]] || { printf '%s\n' 'ownership state was not written' >&2; exit 1; }
	output="$(run_local up 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=up STATUS=passed'
	output="$(run_local status 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=status STATUS=passed'
	[[ "$(grep -c 'args=create cluster' "$TRACE_FILE" || true)" == 1 ]] || { printf '%s\n' 'local-up did not converge to one cluster' >&2; exit 1; }
	# A held mkdir lock fails concurrent mutation while read-only status remains
	# usable; the lock file is never treated as ownership evidence by itself.
	mkdir -p "$STATE_DIR/action.lock"; printf '%s\n' 'action=other' >"$STATE_DIR/action.lock/owner"
	expect_failure 'action lock is held' run_local up
	rm -rf "$STATE_DIR/action.lock"
	# Malformed metadata is rejected before any cluster read or write.
	cp "$STATE_DIR/metadata.v1" "$STATE_DIR/metadata.good"
	printf '%s\n' 'unknown=field' >>"$STATE_DIR/metadata.v1"
	expect_failure 'ownership metadata' run_local up
	mv "$STATE_DIR/metadata.good" "$STATE_DIR/metadata.v1"
	# A supported-version mismatch names the explicit recovery command.
	sed -i 's/^kubernetes_version=.*/kubernetes_version=1.36.2/' "$STATE_DIR/metadata.v1"
	expect_failure 'run make local-down' run_local up
	sed -i 's/^kubernetes_version=.*/kubernetes_version=1.35.6/' "$STATE_DIR/metadata.v1"
}

assert_read_only_resume_trace() {
	local trace="$1"
	assert_not_contains "$trace" 'args=start'
	assert_not_contains "$trace" 'args=create cluster'
	assert_not_contains "$trace" 'args=delete cluster'
	assert_not_contains "$trace" 'args=build --file '
	assert_not_contains "$trace" 'args=save --format '
	assert_not_contains "$trace" 'args=cp '
	assert_not_contains "$trace" 'args=exec '
	assert_not_contains "$trace" 'upgrade --install kubeseer'
	assert_not_contains "$trace" ' apply '
	assert_not_contains "$trace" ' create '
	assert_not_contains "$trace" ' patch '
	assert_not_contains "$trace" ' replace '
	assert_not_contains "$trace" ' delete '
	assert_not_contains "$trace" ' wait '
	assert_not_contains "$trace" ' port-forward '
}

resume_ownership_checks() {
	local output trace saved
	output="$(run_local up 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=up STATUS=passed'
	trace="$(<"$TRACE_FILE")"
	assert_contains "$trace" 'args=container inspect'
	assert_contains "$trace" 'kubectl kubeconfig=<unset> args=--kubeconfig '
	assert_not_contains "$trace" 'args=start'

	# Cluster without project ownership metadata is not inspected or started.
	mv "$STATE_DIR/metadata.v1" "$STATE_DIR/metadata.saved"
	: >"$TRACE_FILE"
	output="$(expect_failure 'exists without matching ownership metadata' run_local up)"
	trace="$(<"$TRACE_FILE")"
	assert_read_only_resume_trace "$trace"
	assert_not_contains "$trace" 'args=container inspect'
	mv "$STATE_DIR/metadata.saved" "$STATE_DIR/metadata.v1"

	# A changed container name, cluster label, role, missing kind node, absent
	# container, or mismatched API binding must fail before lifecycle mutation.
	saved="$(<"$CONTAINER_NAME_FILE")"
	printf '%s\n' unrelated-control-plane >"$CONTAINER_NAME_FILE"
	: >"$TRACE_FILE"; expect_failure 'Podman identity does not match' run_local up >/dev/null
	assert_read_only_resume_trace "$(<"$TRACE_FILE")"; printf '%s\n' "$saved" >"$CONTAINER_NAME_FILE"

	saved="$(<"$CLUSTER_LABEL_FILE")"
	printf '%s\n' unrelated-cluster >"$CLUSTER_LABEL_FILE"
	: >"$TRACE_FILE"; expect_failure 'Podman identity does not match' run_local up >/dev/null
	assert_read_only_resume_trace "$(<"$TRACE_FILE")"; printf '%s\n' "$saved" >"$CLUSTER_LABEL_FILE"

	saved="$(<"$ROLE_LABEL_FILE")"
	printf '%s\n' worker >"$ROLE_LABEL_FILE"
	: >"$TRACE_FILE"; expect_failure 'Podman identity does not match' run_local up >/dev/null
	assert_read_only_resume_trace "$(<"$TRACE_FILE")"; printf '%s\n' "$saved" >"$ROLE_LABEL_FILE"

	: >"$TRACE_FILE"
	output="$(KIND_FIXTURE_NODE_MISSING=1 expect_failure 'expected kind node' run_local up)"
	assert_read_only_resume_trace "$(<"$TRACE_FILE")"
	assert_contains "$output" 'ownership conflict'

	: >"$TRACE_FILE"
	output="$(PODMAN_INSPECT_FAIL=1 expect_failure 'cannot inspect the expected control-plane container' run_local up)"
	assert_read_only_resume_trace "$(<"$TRACE_FILE")"

	saved="$(<"$API_BINDING_FILE")"
	printf '%s\n' '127.0.0.1,35680;' >"$API_BINDING_FILE"
	: >"$TRACE_FILE"; output="$(expect_failure 'does not uniquely match' run_local up)"
	assert_contains "$output" 'ownership conflict'; assert_read_only_resume_trace "$(<"$TRACE_FILE")"
	printf '%s\n' "$saved" >"$API_BINDING_FILE"

	saved="$(sed -n -E 's/^[[:space:]]*server:[[:space:]]*([^[:space:]]+).*$/\1/p' "$STATE_DIR/kubeconfig" | head -n 1)"
	sed -i 's#https://127.0.0.1:35679#https://192.0.2.15:35679#' "$STATE_DIR/kubeconfig"
	: >"$TRACE_FILE"; output="$(expect_failure 'not a loopback address' run_local up)"
	assert_contains "$output" 'ownership conflict'; assert_read_only_resume_trace "$(<"$TRACE_FILE")"
	sed -i "s#https://192.0.2.15:35679#$saved#" "$STATE_DIR/kubeconfig"

	saved="$(<"$API_AVAILABLE_FILE")"
	printf '%s\n' 0 >"$API_AVAILABLE_FILE"
	: >"$TRACE_FILE"
	output="$(expect_failure 'Kubernetes API identity is unreachable' run_local up)"
	assert_contains "$output" 'Kubernetes API identity is unreachable'
	assert_read_only_resume_trace "$(<"$TRACE_FILE")"
	assert_contains "$(<"$TRACE_FILE")" 'args=container inspect'
	printf '%s\n' "$saved" >"$API_AVAILABLE_FILE"
}

assert_resume_state_preserved() {
	cmp -s "$FIXTURE_DIR/metadata.before" "$STATE_DIR/metadata.v1" || { printf '%s\n' 'failed resume changed owned metadata' >&2; exit 1; }
	cmp -s "$FIXTURE_DIR/kubeconfig.before" "$STATE_DIR/kubeconfig" || { printf '%s\n' 'failed resume changed owned kubeconfig' >&2; exit 1; }
}

assert_no_convergence_trace() {
	local trace="$1"
	assert_not_contains "$trace" 'args=create cluster'
	assert_not_contains "$trace" 'args=delete cluster'
	assert_not_contains "$trace" 'args=build --file '
	assert_not_contains "$trace" 'args=save --format '
	assert_not_contains "$trace" 'args=cp '
	assert_not_contains "$trace" 'args=exec '
	assert_not_contains "$trace" 'upgrade --install kubeseer'
	assert_not_contains "$trace" ' apply '
	assert_not_contains "$trace" ' wait '
	assert_not_contains "$trace" ' port-forward '
}

resume_state_checks() {
	local output trace saved_state start_count start_line api_line
	local workload="$FIXTURE_DIR/workload.sentinel"
	printf '%s\n' 'persistent test workload data' >"$workload"
	cp "$STATE_DIR/metadata.v1" "$FIXTURE_DIR/metadata.before"
	cp "$STATE_DIR/kubeconfig" "$FIXTURE_DIR/kubeconfig.before"
	printf '%s\n' 0 >"$CONTAINER_STARTS_FILE"
	printf '%s\n' 0 >"$API_ATTEMPTS_FILE"
	printf '%s\n' 0 >"$API_AVAILABLE_FILE"
	printf '%s\n' exited >"$CONTAINER_STATE_FILE"
	: >"$TRACE_FILE"
	output="$(API_APPEAR_AFTER=2 KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS=5 run_local up 2>&1)"
	assert_contains "$output" 'LOCAL_ENVIRONMENT=up STATUS=passed'
	assert_contains "$(<"$TRACE_FILE")" 'args=container start '
	[[ "$(<"$CONTAINER_STARTS_FILE")" == 1 ]] || { printf '%s\n' 'resume did not start exactly one container' >&2; exit 1; }
	[[ "$(<"$CONTAINER_STATE_FILE")" == running ]] || { printf '%s\n' 'resumed container is not running' >&2; exit 1; }
	[[ "$(<"$workload")" == 'persistent test workload data' ]] || { printf '%s\n' 'resume changed existing workload data' >&2; exit 1; }
	trace="$(<"$TRACE_FILE")"
	start_line="$(rg -n 'args=container start ' "$TRACE_FILE" | head -n 1 | cut -d: -f1)"
	api_line="$(rg -n 'version --request-timeout=' "$TRACE_FILE" | head -n 1 | cut -d: -f1)"
	[[ -n "$start_line" && -n "$api_line" && "$start_line" -lt "$api_line" ]] || { printf '%s\n' 'API identity was not checked after the owned container start' >&2; exit 1; }
	assert_not_contains "$trace" 'args=create cluster'
	assert_not_contains "$trace" 'args=delete cluster'
	local helm_line
	helm_line="$(rg -n 'upgrade --install kubeseer' "$TRACE_FILE" | head -n 1 | cut -d: -f1)"
	[[ -n "$helm_line" && "$api_line" -lt "$helm_line" ]] || { printf '%s\n' 'normal convergence began before API identity validation' >&2; exit 1; }

	# A subsequent up validates and converges without another container start.
	: >"$TRACE_FILE"
	output="$(run_local up 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=up STATUS=passed'
	[[ "$(<"$CONTAINER_STARTS_FILE")" == 1 ]] || { printf '%s\n' 'healthy repeat local-up restarted the node' >&2; exit 1; }
	assert_not_contains "$(<"$TRACE_FILE")" 'args=container start '

	# Read-only entry points leave an exited control plane stopped.
	printf '%s\n' exited >"$CONTAINER_STATE_FILE"
	start_count="$(<"$CONTAINER_STARTS_FILE")"
	: >"$TRACE_FILE"
	output="$(run_local check 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=check STATUS=passed'
	output="$(run_local status 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=status STATUS=passed'
	[[ "$(<"$CONTAINER_STATE_FILE")" == exited && "$(<"$CONTAINER_STARTS_FILE")" == "$start_count" ]] || { printf '%s\n' 'read-only local command resumed the exited node' >&2; exit 1; }
	assert_not_contains "$(<"$TRACE_FILE")" 'args=container start '

	# Bad timeout is rejected before Podman mutation; start failures retain state.
	: >"$TRACE_FILE"
	output="$(KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS=0 expect_failure 'integer from 1 through 600' run_local up)"
	assert_contains "$output" 'integer from 1 through 600'
	assert_not_contains "$(<"$TRACE_FILE")" 'args=container start '
	assert_resume_state_preserved

	printf '%s\n' 0 >"$API_AVAILABLE_FILE"
	: >"$TRACE_FILE"
	output="$(PODMAN_FAIL_START=1 expect_failure 'failed to start owned node kubeseer-local-control-plane with Podman: fixture Podman start failure' run_local up)"
	assert_contains "$output" 'fixture Podman start failure'
	[[ "$(<"$CONTAINER_STATE_FILE")" == exited ]] || { printf '%s\n' 'failed Podman start changed container state' >&2; exit 1; }
	assert_not_contains "$(<"$TRACE_FILE")" 'version --request-timeout='
	assert_no_convergence_trace "$(<"$TRACE_FILE")"
	assert_resume_state_preserved

	# A stopped node that never serves its API expires within the configured budget.
	printf '%s\n' 0 >"$API_AVAILABLE_FILE"
	printf '%s\n' 0 >"$API_ATTEMPTS_FILE"
	: >"$TRACE_FILE"
	output="$(KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS=2 expect_failure 'readiness wait expired after 2 seconds' run_local up)"
	assert_contains "$output" 'readiness wait expired after 2 seconds'
	[[ "$(<"$CONTAINER_STATE_FILE")" == running ]] || { printf '%s\n' 'timed-out started node should remain available for diagnosis' >&2; exit 1; }
	assert_no_convergence_trace "$(<"$TRACE_FILE")"
	assert_resume_state_preserved

	# An API that answers for a different kind node identity stops before writes.
	printf '%s\n' 1 >"$API_AVAILABLE_FILE"
	printf '%s\n' 'unrelated-control-plane' >"$API_NODES_FILE"
	printf '%s\n' exited >"$CONTAINER_STATE_FILE"
	: >"$TRACE_FILE"
	output="$(expect_failure 'resumed API identity conflicts' run_local up)"
	assert_contains "$output" 'Kubernetes API node identity conflicts'
	assert_contains "$(<"$TRACE_FILE")" 'args=container start '
	assert_no_convergence_trace "$(<"$TRACE_FILE")"
	assert_resume_state_preserved
	printf '%s-control-plane\n' "$(<"$CLUSTER_MARKER")" >"$API_NODES_FILE"

	# Re-inspection after start catches a changed published endpoint before API calls.
	printf '%s\n' exited >"$CONTAINER_STATE_FILE"
	: >"$TRACE_FILE"
	output="$(PODMAN_START_BINDING='127.0.0.1,35680;' expect_failure 'does not uniquely match' run_local up)"
	assert_contains "$output" 'does not uniquely match the published Kubernetes API port'
	assert_not_contains "$(<"$TRACE_FILE")" 'version --request-timeout='
	assert_no_convergence_trace "$(<"$TRACE_FILE")"
	assert_resume_state_preserved
	printf '%s\n' '127.0.0.1,35679;' >"$API_BINDING_FILE"

	# Unsupported states fail closed without a start attempt.
	saved_state="$(<"$CONTAINER_STATE_FILE")"
	printf '%s\n' created >"$CONTAINER_STATE_FILE"
	: >"$TRACE_FILE"
	output="$(expect_failure 'unsupported state created' run_local up)"
	assert_contains "$output" 'unsupported state created'
	assert_not_contains "$(<"$TRACE_FILE")" 'args=container start '
	printf '%s\n' "$saved_state" >"$CONTAINER_STATE_FILE"
}

package_checks() {
	local trace
	trace="$(<"$TRACE_FILE")"
	assert_contains "$trace" 'args=build --file '
	assert_contains "$trace" 'args=save --format oci-archive --output '
	assert_contains "$trace" 'args=cp '
	assert_contains "$trace" 'args=exec '
	assert_contains "$trace" 'ctr --namespace k8s.io images import'
	assert_contains "$trace" 'cert-manager-v1.18.2.yaml'
	assert_contains "$trace" 'upgrade --install kubeseer'
	assert_contains "$trace" '--atomic --cleanup-on-fail --wait'
	assert_not_contains "$trace" 'docker.io'
	assert_not_contains "$trace" 'podman image prune'
	assert_not_contains "$trace" 'prune'
}

case "$MODE" in
	preflight)
		preflight_checks
		;;
	ownership|resume-ownership|resume|package|examples|verification|diagnostics|complete)
		preflight_checks; workflow_checks
		if [[ "$MODE" == package || "$MODE" == complete ]]; then package_checks; fi
		if [[ "$MODE" == resume-ownership || "$MODE" == resume || "$MODE" == complete ]]; then resume_ownership_checks; fi
		if [[ "$MODE" == resume || "$MODE" == complete ]]; then resume_state_checks; fi
		if [[ "$MODE" == ownership || "$MODE" == complete ]]; then
			# Creation failure cleans only the current partial exact target.
			output="$(KIND_FAIL_CREATE=1 run_local down 2>&1)" || true
			KIND_FAIL_CREATE=1 expect_failure 'kind creation failed' run_local up
			[[ ! -f "$STATE_DIR/metadata.v1" && ! -f "$CLUSTER_MARKER" ]] || { printf '%s\n' 'partial creation state survived' >&2; exit 1; }
			workflow_checks
			# Deletion failure retains both metadata and the owned kubeconfig for retry.
			KIND_FAIL_DELETE=1 expect_failure 'retained cluster=kubeseer-local' run_local down
			[[ -f "$STATE_DIR/metadata.v1" && -f "$STATE_DIR/kubeconfig" ]] || { printf '%s\n' 'failed deletion lost retry state' >&2; exit 1; }
		fi
		case "$MODE" in
			examples|verification|complete)
				output="$(run_local examples 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=examples STATUS=passed'
				output="$(run_local examples 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=examples STATUS=passed'
				;;
		esac
		case "$MODE" in
			verification|complete)
				output="$(run_local verify 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=verify STATUS=passed'
				for example in builtin-resource typed-extraction value-operator cross-namespace-aggregation custom-resource authorization-denial partial-degradation; do assert_contains "$output" "EXAMPLE=$example STATUS=passed"; done
				;;
		esac
		case "$MODE" in
			diagnostics|complete)
				output="$(run_local diagnostics 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=diagnostics STATUS=passed'; assert_not_contains "$(find "$STATE_DIR/diagnostics" -type f -exec strings {} + 2>/dev/null || true)" 'local-secret'
				;;
		esac
		case "$MODE" in
			ownership|resume-ownership|resume|package|examples|verification|diagnostics|complete)
				output="$(run_local examples-down 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=examples-down STATUS=passed'
				output="$(run_local down 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=down STATUS=passed'; [[ ! -f "$CLUSTER_MARKER" ]] || exit 1
				;;
		esac
		;;
	*) printf 'unknown acceptance mode: %s\n' "$MODE" >&2; exit 64 ;;
esac

if [[ "$MODE" == resume-ownership ]]; then
	printf 'LOCAL_CLUSTER_RESUME_ACCEPTANCE=ownership STATUS=passed\n'
elif [[ "$MODE" == resume || "$MODE" == complete ]]; then
	printf 'LOCAL_CLUSTER_RESUME_ACCEPTANCE=deterministic STATUS=passed\n'
fi
if [[ "$MODE" != resume-ownership && "$MODE" != resume && "$MODE" != complete ]]; then
	printf 'LOCAL_ENVIRONMENT_ACCEPTANCE=%s STATUS=passed\n' "$MODE"
elif [[ "$MODE" == complete ]]; then
	printf 'LOCAL_ENVIRONMENT_ACCEPTANCE=%s STATUS=passed\n' "$MODE"
fi
