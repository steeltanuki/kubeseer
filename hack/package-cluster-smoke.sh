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
cd -- "$ROOT_DIR"
version=""
kubeconfig=""
kube_context=""

while (($# > 0)); do
	case "$1" in
	--kubernetes-version)
		[[ $# -ge 2 ]] || { printf '%s\n' '--kubernetes-version requires a value' >&2; exit 64; }
		version="$2"
		shift 2
		;;
	--kubeconfig)
		[[ $# -ge 2 ]] || { printf '%s\n' '--kubeconfig requires a value' >&2; exit 64; }
		kubeconfig="$2"
		shift 2
		;;
	--context)
		[[ $# -ge 2 ]] || { printf '%s\n' '--context requires a value' >&2; exit 64; }
		kube_context="$2"
		shift 2
		;;
	*)
		printf 'unknown argument: %s\n' "$1" >&2
		exit 64
		;;
	esac
done

[[ "$version" == 1.35.6 || "$version" == 1.36.2 ]] || { printf 'unsupported Kubernetes version: %s\n' "$version" >&2; exit 64; }
[[ "$kubeconfig" == /* && -f "$kubeconfig" ]] || { printf '%s\n' 'an existing absolute --kubeconfig is required' >&2; exit 64; }
[[ -n "$kube_context" ]] || { printf '%s\n' 'an explicit --context is required' >&2; exit 64; }

for command_name in helm kubectl jq; do
	command -v "$command_name" >/dev/null 2>&1 || { printf 'required command is missing: %s\n' "$command_name" >&2; exit 69; }
done

readonly kubectl_args=(--kubeconfig "$kubeconfig" --context "$kube_context")
readonly helm_args=(--kubeconfig "$kubeconfig" --kube-context "$kube_context")
readonly namespace="${KUBESEER_PACKAGE_NAMESPACE:-kubeseer-system}"
readonly image_repository="${KUBESEER_PACKAGE_IMAGE_REPOSITORY:-}"
readonly image_tag="${KUBESEER_PACKAGE_IMAGE_TAG:-}"
readonly external_secret_name="${KUBESEER_PACKAGE_EXTERNAL_SECRET_NAME:-administrator-webhook-tls}"
readonly external_ca_bundle="${KUBESEER_PACKAGE_EXTERNAL_CA_BUNDLE:-}"
readonly rotated_secret_manifest="${KUBESEER_PACKAGE_EXTERNAL_ROTATED_SECRET:-}"

[[ -n "$image_repository" && -n "$image_tag" && "$image_tag" != latest ]] || {
	printf '%s\n' 'live smoke requires KUBESEER_PACKAGE_IMAGE_REPOSITORY and a non-latest KUBESEER_PACKAGE_IMAGE_TAG' >&2
	exit 64
}

server_version="$(env -u KUBECONFIG kubectl "${kubectl_args[@]}" version -o json | jq -r '.serverVersion.gitVersion')"
[[ "$server_version" == "v${version}"* ]] || {
	printf 'server version %s does not match requested Kubernetes %s\n' "$server_version" "$version" >&2
	exit 1
}

cluster_server="$(env -u KUBECONFIG kubectl config view --raw "${kubectl_args[@]}" -o json | jq -r --arg context "$kube_context" '(.contexts[] | select(.name == $context) | .context.cluster) as $cluster | .clusters[] | select(.name == $cluster) | .cluster.server')"
[[ -n "$cluster_server" && "$cluster_server" != null ]] || { printf '%s\n' 'selected context has no API server' >&2; exit 1; }

apply_crds() {
	"$ROOT_DIR/hack/apply-compatible-crds.sh" "${kubectl_args[@]}"
}

kubectl_get() {
	env -u KUBECONFIG kubectl "${kubectl_args[@]}" "$@"
}

helm_release() {
	env -u KUBECONFIG helm "${helm_args[@]}" "$@"
}

run_profile() {
	local mode="$1"
	local values=(--set "image.repository=${image_repository}" --set "image.tag=${image_tag}")
	if [[ "$mode" == externalSecret ]]; then
		[[ -n "$external_ca_bundle" && "$external_ca_bundle" == /* && -f "$external_ca_bundle" ]] || {
			printf '%s\n' 'externalSecret smoke requires an existing absolute KUBESEER_PACKAGE_EXTERNAL_CA_BUNDLE' >&2
			return 64
		}
		values+=(--set certificate.mode=externalSecret)
		values+=(--set "certificate.externalSecret.secretName=${external_secret_name}")
		values+=(--set-file "certificate.externalSecret.caBundle=${external_ca_bundle}")
	fi

	apply_crds
	helm_release upgrade --install kubeseer "$ROOT_DIR/charts/kubeseer" \
		--namespace "$namespace" --create-namespace \
		--wait --timeout 10m "${values[@]}"
	kubectl_get wait --for=condition=Established --timeout=120s \
		crd/kubeseers.kubeseer.io crd/kubeseeraccesspolicies.kubeseer.io
	kubectl_get -n "$namespace" wait --for=condition=Available deployment/kubeseer --timeout=5m
	kubectl_get get validatingwebhookconfiguration kubeseer-validating-webhook -o json \
		| jq -e '(.webhooks | length == 2) and all(.[]; .failurePolicy == "Fail" and (.clientConfig.caBundle | length > 0))' >/dev/null

	if [[ "$mode" == externalSecret && -n "$rotated_secret_manifest" ]]; then
		[[ "$rotated_secret_manifest" == /* && -f "$rotated_secret_manifest" ]] || {
			printf '%s\n' 'KUBESEER_PACKAGE_EXTERNAL_ROTATED_SECRET must be an existing absolute manifest path' >&2
			return 64
		}
		before_generation="$(kubectl_get -n "$namespace" get deployment kubeseer -o jsonpath='{.metadata.generation}')"
		kubectl_get apply --server-side --field-manager=kubeseer-external-secret-smoke -f "$rotated_secret_manifest"
		kubectl_get -n "$namespace" wait --for=condition=Available deployment/kubeseer --timeout=5m
		after_generation="$(kubectl_get -n "$namespace" get deployment kubeseer -o jsonpath='{.metadata.generation}')"
		[[ "$before_generation" == "$after_generation" ]] || {
			printf '%s\n' 'external Secret rotation unexpectedly edited the manager Deployment' >&2
			return 1
		}
	fi

	helm_release uninstall kubeseer --namespace "$namespace" --ignore-not-found --wait --timeout 10m
	helm_release uninstall kubeseer --namespace "$namespace" --ignore-not-found --wait --timeout 10m
	kubectl_get get crd kubeseers.kubeseer.io kubeseeraccesspolicies.kubeseer.io >/dev/null
	kubectl_get get kubeseeraccesspolicy installation-access-ceiling >/dev/null
	if [[ "$mode" == externalSecret ]]; then
		kubectl_get -n "$namespace" get secret "$external_secret_name" >/dev/null
	fi

	set +e
	GOCACHE="${GOCACHE:-/tmp/kubeseer-packaging-go-build}" \
	GOMODCACHE="${GOMODCACHE:-/tmp/kubeseer-packaging-go-mod}" \
		env -u KUBECONFIG go run ./cmd/kubeseer-purge \
			--kubeconfig "$kubeconfig" --context "$kube_context" \
			--confirm-context "$kube_context" --confirm-server "$cluster_server" \
			>/dev/null 2>&1
	purge_status=$?
	set -e
	((purge_status != 0)) || { printf '%s\n' 'purge without the exact token unexpectedly succeeded' >&2; return 1; }
	kubectl_get get crd kubeseers.kubeseer.io kubeseeraccesspolicies.kubeseer.io >/dev/null
	kubectl_get get kubeseeraccesspolicy installation-access-ceiling >/dev/null
	printf 'PACKAGE_CLUSTER_PROFILE version=%s mode=%s STATUS=passed\n' "$version" "$mode"
}

run_profile certManager
run_profile externalSecret
