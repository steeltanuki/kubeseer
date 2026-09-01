#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

if [[ "$#" -ne 2 || -z "$1" || -z "$2" ]]; then
	printf 'usage: %s RELEASE NAMESPACE\n' "$0" >&2
	exit 2
fi

release="$1"
namespace="$2"
helm uninstall "$release" --namespace "$namespace" --ignore-not-found --wait

printf 'UNINSTALL release=%s namespace=%s STATUS=passed\n' "$release" "$namespace"

for target in kubeseers.kubeseer.io kubeseeraccesspolicies.kubeseer.io; do
	if kubectl get crd "$target" -o name --ignore-not-found 2>/dev/null | rg -q .; then
		printf 'UNINSTALL_RETAINED target=%s outcome=retained\n' "$target"
	else
		printf 'UNINSTALL_RETAINED target=%s outcome=already-absent\n' "$target"
	fi
done

if kubectl get kubeseeraccesspolicy installation-access-ceiling -o name --ignore-not-found 2>/dev/null | rg -q .; then
	printf 'UNINSTALL_RETAINED target=installation-access-ceiling outcome=retained\n'
else
	printf 'UNINSTALL_RETAINED target=installation-access-ceiling outcome=already-absent\n'
fi
