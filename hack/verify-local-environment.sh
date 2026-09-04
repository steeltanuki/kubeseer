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
	printf 'usage: %s <probe|examples|verification|diagnostics|complete>\n' "$0" >&2
	exit 64
fi
readonly MODE="$1"
readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

fail() { printf 'LOCAL_ENVIRONMENT_VERIFY=%s STATUS=failed: %s\n' "$MODE" "$1" >&2; exit 1; }
case "$MODE" in probe|examples|verification|diagnostics|complete) ;; *) fail "unknown verification phase: $MODE" ;; esac

[[ -x "$ROOT_DIR/hack/local-environment.sh" ]] || fail 'local environment router is missing or not executable'
[[ -x "$ROOT_DIR/hack/local-examples.sh" ]] || fail 'example executor is missing or not executable'
[[ -x "$ROOT_DIR/hack/kind-podman-common.sh" ]] || fail 'provider helper is missing or not executable'
[[ -x "$ROOT_DIR/hack/test-local-environment.sh" ]] || fail 'genuine local lifecycle harness is missing or not executable'
[[ -f "$ROOT_DIR/hack/toolchain.mk" ]] || fail 'central toolchain file is missing'
[[ -f "$ROOT_DIR/config/local/values.yaml" ]] || fail 'local values profile is missing'
[[ -f "$ROOT_DIR/examples/catalog.txt" ]] || fail 'example catalog is missing'

for target in local-check local-up local-status local-examples local-verify local-diagnostics local-examples-down local-down local-example verify-local-environment test-local-environment; do
	rg -q "^${target}:" "$ROOT_DIR/Makefile" || fail "Make target ${target} is missing"
done
rg -q 'include hack/toolchain.mk' "$ROOT_DIR/Makefile" || fail 'Makefile does not include central toolchain'
rg -q 'KIND_EXPERIMENTAL_PROVIDER.*podman|kp_run_kind.*provider' "$ROOT_DIR/hack/local-environment.sh" "$ROOT_DIR/hack/kind-podman-common.sh" || fail 'provider selection is not explicit'
rg -q -- '--kubeconfig' "$ROOT_DIR/hack/local-environment.sh" "$ROOT_DIR/hack/kind-podman-common.sh" || fail 'explicit kubeconfig boundary is missing'
rg -q -- '--context|--kube-context' "$ROOT_DIR/hack/local-environment.sh" "$ROOT_DIR/hack/kind-podman-common.sh" || fail 'explicit context boundary is missing'
rg -q 'WithTimeout|PollUntilContextTimeout|--timeout' "$ROOT_DIR/internal/localprobe/probe.go" "$ROOT_DIR/hack/local-environment.sh" || fail 'bounded context/deadline is missing'
rg -q 'atomic|mktemp|mv -f' "$ROOT_DIR/hack/local-environment.sh" || fail 'atomic metadata write is missing'
rg -q 'schema_version=1|metadata.v1' "$ROOT_DIR/hack/local-environment.sh" "$ROOT_DIR/internal/localprobe/probe.go" || fail 'metadata schema is missing'
rg -q -- '--atomic' "$ROOT_DIR/hack/local-environment.sh" || fail 'Helm atomic upgrade boundary is missing'
rg -q -- '--server-side' "$ROOT_DIR/hack/local-environment.sh" "$ROOT_DIR/hack/local-examples.sh" || fail 'server-side apply boundary is missing'
if rg -n 'USE_EXISTING_CLUSTER=true|kind[[:space:]]+delete[[:space:]]+clusters|podman[[:space:]]+system[[:space:]]+prune|podman[[:space:]]+image[[:space:]]+prune' "$ROOT_DIR/hack/local-environment.sh" "$ROOT_DIR/hack/local-examples.sh" >/dev/null; then
	fail 'ambient kubeconfig or global cleanup path detected'
fi
if rg -n 'internal/(discovery|selection|extraction|operators|aggregation|authorization|reconciliation|status)' "$ROOT_DIR/internal/localprobe" "$ROOT_DIR/cmd/kubeseer-local" >/dev/null; then
	fail 'local probe imports production algorithms'
fi
if rg -n 'CoreV1\(\)[[:space:]]*\.Endpoints' "$ROOT_DIR/internal/localprobe" "$ROOT_DIR/cmd/kubeseer-local" >/dev/null; then
	fail 'local probe requests deprecated core v1 Endpoints'
fi
rg -q 'discoveryv1\.LabelServiceName|EndpointSlices' "$ROOT_DIR/internal/localprobe/probe.go" || fail 'local probe EndpointSlice observer is missing'

mapfile -t catalog < <(sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$ROOT_DIR/examples/catalog.txt")
[[ "${#catalog[@]}" == 7 ]] || fail 'catalog does not contain exactly seven entries'
declare -A catalog_seen=()
for name in "${catalog[@]}"; do
	[[ -z "${catalog_seen[$name]+set}" ]] || fail "catalog entry is duplicated: $name"
	catalog_seen[$name]=1
	directory="$ROOT_DIR/examples/$name"
	[[ -d "$directory" && -f "$directory/README.md" && -f "$directory/workload.yaml" && -f "$directory/kubeseer.yaml" ]] || fail "example files are incomplete: $name"
	first_line="$(sed -n '1p' "$directory/README.md")"
	[[ "$first_line" == '# Copyright 2026 Alessandro Rontani' ]] || fail "Apache header missing: examples/$name/README.md"
done

for file in "$ROOT_DIR/hack/local-environment.sh" "$ROOT_DIR/hack/local-examples.sh" "$ROOT_DIR/hack/kind-podman-common.sh" "$ROOT_DIR/hack/local-environment-acceptance.sh" "$ROOT_DIR/hack/verify-local-environment.sh"; do
	bash -n "$file" || fail "shell syntax failed: ${file#"$ROOT_DIR/"}"
done

source_module_cache="$(env -u GOMODCACHE go env GOMODCACHE)"
build_cache="${GOCACHE:-/tmp/kubeseer-local-go-build}"
module_cache="${GOMODCACHE:-/tmp/kubeseer-local-go-mod}"
mkdir -p -- "$build_cache" "$module_cache"
local_proxy="file://$source_module_cache/cache/download"
if [[ "$MODE" == probe || "$MODE" == verification || "$MODE" == complete ]]; then
	GOCACHE="$build_cache" GOMODCACHE="$module_cache" GOSUMDB=off GOPROXY="$local_proxy" \
		go build -trimpath -o /tmp/kubeseer-local-probe-verify ./cmd/kubeseer-local >/dev/null || fail 'typed local probe does not build'
	[[ -x /tmp/kubeseer-local-probe-verify ]] || fail 'typed local probe binary was not produced'
	if /tmp/kubeseer-local-probe-verify >/dev/null 2>&1; then fail 'probe accepted missing command'; fi
	rm -f -- /tmp/kubeseer-local-probe-verify
	printf 'LOCAL_ENVIRONMENT_VERIFY=probe STATUS=passed\n'
fi

if [[ "$MODE" == examples || "$MODE" == complete ]]; then
	rg -q 'CRDs are always established before their Custom Resources' "$ROOT_DIR/hack/local-examples.sh" || fail 'example CRD ordering is undocumented in executor'
	rg -q 'kubeseer.io/example' "$ROOT_DIR/hack/local-examples.sh" "$ROOT_DIR/examples/catalog.txt" || fail 'example ownership label is missing'
	rg -q 'partial-degradation|authorization-denial|cross-namespace-aggregation' "$ROOT_DIR/internal/localprobe/probe.go" "$ROOT_DIR/examples/catalog.txt" || fail 'special example assertions are missing'
	printf 'LOCAL_ENVIRONMENT_VERIFY=examples STATUS=passed\n'
fi

if [[ "$MODE" == verification || "$MODE" == complete ]]; then
	rg -q 'builtin-resource' "$ROOT_DIR/internal/localprobe/probe.go" || fail 'probe verifier registry is missing'
	rg -q 'EXAMPLE=%s STATUS=passed' "$ROOT_DIR/internal/localprobe/probe.go" || fail 'non-vacuous example reporting is missing'
	[[ "$(rg -o '(builtin-resource|typed-extraction|value-operator|cross-namespace-aggregation|custom-resource|authorization-denial|partial-degradation)' "$ROOT_DIR/internal/localprobe/probe.go" | sort -u | wc -l)" == 7 ]] || fail 'probe registry and catalog do not cover seven stable names'
	if rg -n 'time\.Sleep|testing\.(T|TB)\.Skip|os\.Exit\(0\)' "$ROOT_DIR/internal/localprobe" "$ROOT_DIR/hack/local-examples.sh" >/dev/null; then fail 'verification contains skip, sleep, or vacuous success'; fi
	rg -q 'kubeseer-e2e-' "$ROOT_DIR/hack/e2e-harness.sh" || fail 'E2E identity separation is missing'
	printf 'LOCAL_ENVIRONMENT_VERIFY=verification STATUS=passed\n'
fi

if [[ "$MODE" == diagnostics || "$MODE" == complete ]]; then
	rg -q 'completedSections|failedSections|unavailable' "$ROOT_DIR/internal/localprobe/probe.go" || fail 'diagnostic manifest/unavailable projection is missing'
	rg -q 'maxDiagLogLines|maxDiagLogBytes|eventsProjection|logsProjection' "$ROOT_DIR/internal/localprobe/probe.go" || fail 'diagnostic bounds/allowlist is missing'
	printf 'LOCAL_ENVIRONMENT_VERIFY=diagnostics STATUS=passed\n'
fi

if [[ "$MODE" == complete ]]; then
	[[ -f "$ROOT_DIR/docs/local-development.md" ]] || fail 'local development guide is missing'
	rg -q 'local-development.md' "$ROOT_DIR/README.md" || fail 'README does not link local guide'
	if rg -n 'CoreV1\(\)[[:space:]]*\.Endpoints' \
		"$ROOT_DIR/internal" "$ROOT_DIR/cmd" "$ROOT_DIR/hack/e2e-harness.sh" >/dev/null; then
		fail 'supported production or E2E path requests deprecated core v1 Endpoints'
	fi
	if rg -n '(^|[[:space:]])get[[:space:]]+[^#]*([,[:space:]])endpoints([[:space:]]|$)' \
		--glob '*.sh' --glob '!verify-local-environment.sh' --glob '!e2e-harness-acceptance.sh' \
		"$ROOT_DIR/hack" >/dev/null; then
		fail 'supported harness path uses deprecated kubectl get endpoints'
	fi
	if rg -n '(^|[[:space:]])get[[:space:]]+[^#]*([,[:space:]])endpoints([[:space:]]|$)' \
		--glob '*.md' "$ROOT_DIR/docs" >/dev/null; then
		fail 'documented workflow uses deprecated kubectl get endpoints'
	fi
	for inspected in "$ROOT_DIR/hack/e2e-harness.sh" "$ROOT_DIR/docs/operations.md" "$ROOT_DIR/docs/local-development.md"; do
		rg -q 'get endpointslice' "$inspected" || fail "EndpointSlice inspection is missing: ${inspected#"$ROOT_DIR/"}"
		rg -q 'kubernetes.io/service-name' "$inspected" || fail "Service-name selector is missing: ${inspected#"$ROOT_DIR/"}"
	done
	rg -q 'items\[\*\]\.endpoints\[\*\]' "$ROOT_DIR/hack/e2e-harness.sh" || fail 'E2E EndpointSlice aggregation is missing'
	rg -q 'LOCAL_ENDPOINTSLICE_ACCEPTANCE=genuine STATUS=passed' "$ROOT_DIR/hack/test-local-environment.sh" || fail 'genuine EndpointSlice acceptance marker is missing'
	rg -q 'v1 Endpoints is deprecated' "$ROOT_DIR/hack/test-local-environment.sh" || fail 'genuine deprecation-warning gate is missing'
	for documented in 'Linux/amd64' 'macOS' 'Windows' 'WSL' 'non-amd64' 'kubeseer-local' 'kind-kubeseer-local' 'kubeseer-system' 'installation-access-ceiling' 'local-check' 'local-up' 'local-status' 'local-examples' 'local-verify' 'local-diagnostics' 'local-examples-down' 'local-down' '/readyz' '/metrics' 'Events' 'logs' 'rootless Podman' 'cert-manager' 'run-unique' 'external registry' 'external clusters' 'externalSecret' 'EndpointSlice' 'kubernetes.io/service-name'; do
		rg -F -q -- "$documented" "$ROOT_DIR/docs/local-development.md" || fail "local guide lacks ${documented}"
	done
	for name in "${catalog[@]}"; do
		rg -q -- "make local-example" "$ROOT_DIR/examples/$name/README.md" || fail "example README lacks constrained command: $name"
		rg -q -- 'ACTION=apply' "$ROOT_DIR/examples/$name/README.md" || fail "example README lacks apply action: $name"
		rg -q -- 'ACTION=verify' "$ROOT_DIR/examples/$name/README.md" || fail "example README lacks verify action: $name"
		rg -q -- 'ACTION=down' "$ROOT_DIR/examples/$name/README.md" || fail "example README lacks cleanup action: $name"
	done
	printf 'LOCAL_ENVIRONMENT_VERIFY=complete STATUS=passed\n'
fi
