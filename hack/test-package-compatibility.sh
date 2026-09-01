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
readonly CHART_DIR="$ROOT_DIR/charts/kubeseer"
readonly TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-package-compatibility.XXXXXX")"
readonly VERSIONS=(1.35.6 1.36.2)
readonly MODES=(certManager externalSecret)
trap 'rm -rf -- "$TEMP_DIR"' EXIT

fail() {
	printf 'PACKAGE_COMPATIBILITY=complete STATUS=failed: %s\n' "$1" >&2
	exit 1
}

command -v helm >/dev/null 2>&1 || fail 'helm is required'

profile_count=0
for version in "${VERSIONS[@]}"; do
	for mode in "${MODES[@]}"; do
		profile_count=$((profile_count + 1))
		output="$TEMP_DIR/${version//./-}-${mode}.yaml"
		args=(template kubeseer "$CHART_DIR" --namespace kubeseer-system --kube-version "$version" --include-crds)
		if [[ "$mode" == externalSecret ]]; then
			args+=(--set certificate.mode=externalSecret)
			args+=(--set certificate.externalSecret.secretName=administrator-webhook-tls)
			args+=(--set-string certificate.externalSecret.caBundle="PUBLIC-CA-${version}")
		fi
		helm "${args[@]}" >"$output" || fail "render failed for Kubernetes ${version} ${mode}"
		[[ -s "$output" ]] || fail "empty render for Kubernetes ${version} ${mode}"
		cmp -s "$output" <(helm "${args[@]}") || fail "non-deterministic render for Kubernetes ${version} ${mode}"
		if [[ "$mode" == certManager ]]; then
			rg -q 'apiVersion: cert-manager.io/v1' "$output" || fail "cert-manager profile missing for Kubernetes ${version}"
		else
			if rg -q 'cert-manager.io/|kind: Secret' "$output"; then
				fail "externalSecret profile owns cert-manager or TLS Secret state"
			fi
			rg -q 'secretName: "administrator-webhook-tls"' "$output" || fail "external Secret mount missing"
		fi
		rg -q 'failurePolicy: Fail' "$output" || fail "fail-closed webhook missing"
		rg -q 'maxUnavailable: 0' "$output" || fail "rolling availability gate missing"
	done
done
((profile_count == 4)) || fail "compatibility matrix did not execute all profiles"

if [[ "${KUBESEER_PACKAGE_RUN_CLUSTER:-0}" == 1 ]]; then
	for version in "${VERSIONS[@]}"; do
		config_var="KUBESEER_PACKAGE_KUBECONFIG_${version//./_}"
		context_var="KUBESEER_PACKAGE_CONTEXT_${version//./_}"
		kubeconfig="${!config_var-}"
		kube_context="${!context_var-}"
		[[ "$kubeconfig" == /* && -f "$kubeconfig" ]] || fail "${config_var} must be an existing absolute path"
		[[ -n "$kube_context" ]] || fail "${context_var} is required"
		"$ROOT_DIR/hack/package-cluster-smoke.sh" \
			--kubernetes-version "$version" \
			--kubeconfig "$kubeconfig" \
			--context "$kube_context"
	done
elif [[ "${KUBESEER_PACKAGE_RUN_CLUSTER:-0}" != 0 ]]; then
	fail 'KUBESEER_PACKAGE_RUN_CLUSTER must be 0 or 1'
fi

printf 'PACKAGE_COMPATIBILITY=complete STATUS=passed\n'
