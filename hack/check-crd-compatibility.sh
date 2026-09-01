#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

kubeconfig=""
kube_context=""
while (($# > 0)); do
	case "$1" in
		--kubeconfig)
			(($# >= 2)) || { printf '%s\n' '--kubeconfig requires a value' >&2; exit 64; }
			kubeconfig="$2"
			shift 2
			;;
		--context)
			(($# >= 2)) || { printf '%s\n' '--context requires a value' >&2; exit 64; }
			kube_context="$2"
			shift 2
			;;
		*)
			printf 'unknown argument: %s\n' "$1" >&2
			exit 64
			;;
	esac
done

[[ -n "$kubeconfig" && "$kubeconfig" == /* && -f "$kubeconfig" ]] || {
	printf '%s\n' 'an existing absolute --kubeconfig is required; ambient credentials are not used' >&2
	exit 64
}
[[ -n "$kube_context" ]] || { printf '%s\n' 'an explicit --context is required' >&2; exit 64; }
command -v kubectl >/dev/null 2>&1 || { printf '%s\n' 'kubectl is required' >&2; exit 69; }
command -v jq >/dev/null 2>&1 || { printf '%s\n' 'jq is required' >&2; exit 69; }

readonly kubectl_args=(--kubeconfig "$kubeconfig" --context "$kube_context")
readonly source_dir="config/crd/bases"

desired_value() {
	local file="$1"
	local expression="$2"
	kubectl apply --dry-run=client -f "$file" -o json | jq -r "$expression"
}

check_one() {
	local file="$1"
	local name="$2"
	local live
	if ! live="$(kubectl "${kubectl_args[@]}" get crd "$name" -o json 2>/dev/null)"; then
		printf 'CRD %s is absent; creation is compatible\n' "$name"
		return 0
	fi

	local live_scope live_storage live_conversion desired_scope desired_storage desired_conversion
	live_scope="$(jq -r '.spec.scope' <<<"$live")"
	live_storage="$(jq -r '[.spec.versions[] | select(.storage == true) | .name] | join(",")' <<<"$live")"
	live_conversion="$(jq -r '.spec.conversion.strategy // "None"' <<<"$live")"
	desired_scope="$(desired_value "$file" '.spec.scope')"
	desired_storage="$(desired_value "$file" '[.spec.versions[] | select(.storage == true) | .name] | join(",")')"
	desired_conversion="$(desired_value "$file" '.spec.conversion.strategy // "None"')"

	[[ "$live_scope" == "$desired_scope" ]] || { printf 'CRD %s scope change is incompatible\n' "$name" >&2; return 1; }
	[[ "$live_storage" == "$desired_storage" ]] || { printf 'CRD %s storage-version change is incompatible\n' "$name" >&2; return 1; }
	[[ "$live_conversion" == "$desired_conversion" ]] || { printf 'CRD %s conversion strategy change is incompatible\n' "$name" >&2; return 1; }
	printf 'CRD %s has a compatible scope/storage/conversion identity\n' "$name"
}

check_one "$source_dir/kubeseer.io_kubeseers.yaml" kubeseers.kubeseer.io
check_one "$source_dir/kubeseer.io_kubeseeraccesspolicies.yaml" kubeseeraccesspolicies.kubeseer.io
printf 'PACKAGE_CRD_COMPATIBILITY=passed\n'
