#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
root_dir="$(dirname -- "$script_dir")"
cd "$root_dir"
"$script_dir/check-crd-compatibility.sh" "$@"

kubeconfig=""
kube_context=""
while (($# > 0)); do
	case "$1" in
		--kubeconfig) kubeconfig="$2"; shift 2 ;;
		--context) kube_context="$2"; shift 2 ;;
		*) shift ;;
	esac
done

readonly kubectl_args=(--kubeconfig "$kubeconfig" --context "$kube_context")
kubectl "${kubectl_args[@]}" apply --server-side --field-manager=kubeseer-crd-upgrade -f config/crd/bases
kubectl "${kubectl_args[@]}" wait --for=condition=Established --timeout=120s crd/kubeseers.kubeseer.io crd/kubeseeraccesspolicies.kubeseer.io
printf 'PACKAGE_CRD_APPLY=passed\n'
