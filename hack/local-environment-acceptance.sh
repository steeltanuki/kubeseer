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
	printf 'usage: %s <preflight|ownership|package|examples|verification|diagnostics|complete>\n' "$0" >&2
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
if [[ "${1:-}" == get && "${2:-}" == clusters ]]; then [[ -f "${CLUSTER_MARKER:?}" ]] && printf '%s\n' kubeseer-local; exit 0; fi
if [[ "${1:-}" == get && "${2:-}" == nodes ]]; then [[ -f "${CLUSTER_MARKER:?}" ]] && printf '%s\n' kubeseer-local-control-plane kubeseer-local-worker; exit 0; fi
if [[ "${KIND_FAIL_CREATE-}" == 1 && "${1:-}" == create ]]; then exit 42; fi
if [[ "${1:-}" == create ]]; then kubeconfig=''; previous=''; for argument in "$@"; do if [[ "$previous" == --kubeconfig ]]; then kubeconfig="$argument"; fi; previous="$argument"; done; printf '%s\n' 'apiVersion: v1' >"$kubeconfig"; : >"$CLUSTER_MARKER"; exit 0; fi
if [[ "${KIND_FAIL_DELETE-}" == 1 && "${1:-}" == delete ]]; then exit 43; fi
if [[ "${1:-}" == delete ]]; then rm -f "$CLUSTER_MARKER"; exit 0; fi
exit 0
EOF

cat >"$BIN_DIR/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'kubectl kubeconfig=%s args=%s\n' "${KUBECONFIG-<unset>}" "$*" >>"${TRACE_FILE:?}"
if [[ "${1:-}" == version ]]; then printf '%s\n' 'Client Version: v1.35.6'; exit 0; fi
if [[ "${KUBESEER_FAIL_CERT-}" == 1 && "${1:-}" == apply && "$*" == *cert-manager* ]]; then exit 47; fi
if [[ "${1:-}" == apply || "${1:-}" == wait || "${1:-}" == delete || "${1:-}" == get ]]; then exit 0; fi
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
	ownership|package|examples|verification|diagnostics|complete)
		preflight_checks; workflow_checks
		if [[ "$MODE" == package || "$MODE" == complete ]]; then package_checks; fi
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
			ownership|package|examples|verification|diagnostics|complete)
				output="$(run_local examples-down 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=examples-down STATUS=passed'
				output="$(run_local down 2>&1)"; assert_contains "$output" 'LOCAL_ENVIRONMENT=down STATUS=passed'; [[ ! -f "$CLUSTER_MARKER" ]] || exit 1
				;;
		esac
		;;
	*) printf 'unknown acceptance mode: %s\n' "$MODE" >&2; exit 64 ;;
esac

printf 'LOCAL_ENVIRONMENT_ACCEPTANCE=%s STATUS=passed\n' "$MODE"
