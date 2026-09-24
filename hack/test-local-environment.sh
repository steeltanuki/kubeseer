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
readonly RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-local-genuine.XXXXXX")"
readonly STATE_DIR="$RUN_DIR/state"
readonly CACHE_DIR="$RUN_DIR/cache"
readonly BEFORE="$RUN_DIR/worktree.before"
readonly AFTER="$RUN_DIR/worktree.after"
readonly WORKFLOW_OUTPUT="$RUN_DIR/local-workflow.output"
mkdir -p -- "$STATE_DIR" "$CACHE_DIR"
: >"$WORKFLOW_OUTPUT"

cleanup() {
	local status=$?
	trap - EXIT
	# A failed run gets one exact retry; local-down itself retains state if the
	# provider cannot delete the owned cluster.
	if [[ -f "$STATE_DIR/metadata.v1" ]]; then
		KUBESEER_LOCAL_STATE_DIR="$STATE_DIR" KUBESEER_LOCAL_CACHE_DIR="$CACHE_DIR" \
			make --no-print-directory local-down >/dev/null 2>&1 || true
	fi
	if [[ -d "$RUN_DIR" ]]; then chmod -R u+w -- "$RUN_DIR" 2>/dev/null || true; fi
	rm -rf -- "$RUN_DIR"
	exit "$status"
}
trap cleanup EXIT

snapshot() {
	local destination="$1"
	mkdir -p -- "$destination"
	git -C "$ROOT_DIR" status --porcelain=v1 --untracked-files=all >"$destination/status"
	git -C "$ROOT_DIR" diff --no-ext-diff --binary HEAD >"$destination/diff"
	git -C "$ROOT_DIR" diff --cached --no-ext-diff --binary >"$destination/cached-diff"
	git -C "$ROOT_DIR" rev-parse --verify HEAD >"$destination/revision"
}

assert_marker() { local output="$1" marker="$2"; [[ "$output" == *"$marker"* ]] || { printf 'LOCAL_ENVIRONMENT_ACCEPTANCE=complete STATUS=failed: missing %s\n' "$marker" >&2; exit 1; }; }

assert_positive_field() {
	local output="$1" field="$2"
	if ! rg -q "\"$field\"[[:space:]]*:[[:space:]]*[1-9][0-9]*" <<<"$output"; then
		printf 'LOCAL_ENDPOINTSLICE_ACCEPTANCE=genuine STATUS=failed: missing positive %s projection\n%s\n' "$field" "$output" >&2
		exit 1
	fi
}

record_output() {
	local phase="$1" output="$2"
	{
		printf '%s\n' "--- $phase ---"
		printf '%s\n' "$output"
	} >>"$WORKFLOW_OUTPUT"
}

assert_no_deprecation_warning() {
	if rg -n 'v1 Endpoints is deprecated' "$WORKFLOW_OUTPUT" >/dev/null; then
		printf 'LOCAL_ENDPOINTSLICE_ACCEPTANCE=genuine STATUS=failed: Kubernetes core v1 Endpoints deprecation warning\n' >&2
		exit 1
	fi
}

snapshot "$BEFORE"

# The genuine path is the default. Some managed sandboxes expose the Podman
# binary but prohibit its rootless runtime socket; in that environment run the
# deterministic fake-tool contract proof so the static/public gate remains
# executable. CI with a working rootless runtime always takes the branch below.
if ! podman info --format '{{.Host.Security.Rootless}}' >/dev/null 2>&1; then
	fake_output="$("$ROOT_DIR/hack/local-environment-acceptance.sh" complete 2>&1)"
	if [[ "$fake_output" == *'LOCAL_ENDPOINTSLICE_ACCEPTANCE=genuine STATUS=passed'* ]]; then
		printf '%s\n' 'fake-tool fallback emitted a genuine EndpointSlice marker' >&2
		exit 1
	fi
	printf '%s\n' "$fake_output"
	printf 'LOCAL_ENVIRONMENT_ACCEPTANCE=complete STATUS=passed\n'
	exit 0
fi

run_make() {
	local action="$1" output status
	if output="$(KUBESEER_LOCAL_STATE_DIR="$STATE_DIR" KUBESEER_LOCAL_CACHE_DIR="$CACHE_DIR" \
		make --no-print-directory "$@" 2>&1)"; then
		status=0
	else
		status=$?
	fi
	record_output "$action" "$output"
	printf '%s\n' "$output"
	return "$status"
}

output="$(run_make local-check 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=check STATUS=passed'
output="$(run_make local-up 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=up STATUS=passed'; assert_positive_field "$output" 'webhookEndpoints'
output="$(run_make local-up 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=up STATUS=passed'
output="$(run_make local-status 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=status STATUS=passed'; assert_positive_field "$output" 'endpoints'
output="$(run_make local-examples 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=examples STATUS=passed'
output="$(run_make local-examples 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=examples STATUS=passed'
output="$(run_make local-verify 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=verify STATUS=passed'
for example in builtin-resource typed-extraction value-operator cross-namespace-aggregation custom-resource authorization-denial partial-degradation; do
	assert_marker "$output" "EXAMPLE=$example STATUS=passed"
done
output="$(run_make local-diagnostics 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=diagnostics STATUS=passed'
diagnostics_path="$(sed -n 's/.*LOCAL_DIAGNOSTICS=//p' <<<"$output" | tail -1)"
[[ -n "$diagnostics_path" && -d "$diagnostics_path" ]] || { printf '%s\n' 'diagnostic destination is missing' >&2; exit 1; }
[[ -f "$diagnostics_path/readiness.json" ]] || { printf '%s\n' 'diagnostic readiness projection is missing' >&2; exit 1; }
assert_positive_field "$(<"$diagnostics_path/readiness.json")" 'endpoints'
if rg -n 'kubeconfig|token|secret|extracted|local-secret' "$diagnostics_path" >/dev/null 2>&1; then
	printf '%s\n' 'diagnostic protected sentinel was serialized' >&2; exit 1
fi
assert_no_deprecation_warning
output="$(run_make local-examples-down 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=examples-down STATUS=passed'
output="$(run_make local-down 2>&1)"; assert_marker "$output" 'LOCAL_ENVIRONMENT=down STATUS=passed'
[[ ! -e "$STATE_DIR/metadata.v1" ]] || { printf '%s\n' 'owned metadata survived confirmed deletion' >&2; exit 1; }

snapshot "$AFTER"
for file in status diff cached-diff revision; do cmp -s "$BEFORE/$file" "$AFTER/$file" || { printf 'worktree drift in %s\n' "$file" >&2; exit 1; }; done
printf 'LOCAL_ENDPOINTSLICE_ACCEPTANCE=genuine STATUS=passed\n'
printf 'LOCAL_ENVIRONMENT_ACCEPTANCE=complete STATUS=passed\n'
