#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly TEMP_DIR="$(mktemp -d)"

cleanup() {
	rm -rf -- "$TEMP_DIR"
}
trap cleanup EXIT

mkdir -p -- "$TEMP_DIR/config/crd/bases" "$TEMP_DIR/hack"
cp -R -- "$ROOT_DIR/api" "$TEMP_DIR/api"
cp -R -- "$ROOT_DIR/hack/boilerplate.go.txt" "$TEMP_DIR/hack/boilerplate.go.txt"
cp -- "$ROOT_DIR/Makefile" "$ROOT_DIR/go.mod" "$ROOT_DIR/go.sum" "$TEMP_DIR/"

(
	cd -- "$TEMP_DIR"
	make generate
	make manifests
)

compare_generated() {
	local expected="$1"
	local generated="$2"

	if cmp -s -- "$expected" "$generated"; then
		return 0
	fi

	echo "generated artifact drift: ${expected#"$ROOT_DIR"/}" >&2
	diff -u -- "$expected" "$generated" || true
	return 1
}

compare_generated \
	"$ROOT_DIR/api/v1alpha1/zz_generated.deepcopy.go" \
	"$TEMP_DIR/api/v1alpha1/zz_generated.deepcopy.go"
compare_generated \
	"$ROOT_DIR/config/crd/bases/kubeseer.io_kubeseers.yaml" \
	"$TEMP_DIR/config/crd/bases/kubeseer.io_kubeseers.yaml"

echo "generated artifacts are current"
