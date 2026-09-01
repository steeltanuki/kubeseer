#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

readonly source_dir="config/crd/bases"
readonly destination_dir="charts/kubeseer/crds"

mkdir -p "$destination_dir"
for name in kubeseer.io_kubeseers.yaml kubeseer.io_kubeseeraccesspolicies.yaml; do
	source="$source_dir/$name"
	destination="$destination_dir/$name"
	[[ -f "$source" ]] || { printf 'missing generated CRD: %s\n' "$source" >&2; exit 1; }
	cp "$source" "$destination"
	cmp -s "$source" "$destination" || { printf 'CRD copy is not byte-for-byte identical: %s\n' "$name" >&2; exit 1; }
done

printf 'PACKAGE_CRD_SYNC=passed\n'
