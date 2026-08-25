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
readonly DISCOVERY_DIR="$ROOT_DIR/internal/discovery"

cd -- "$ROOT_DIR"

failures=0

violation() {
	printf 'module boundary violation: %s\n' "$1" >&2
	failures=$((failures + 1))
}

mapfile -t production_dirs < <(
	find api internal \
		-type f \
		-name '*.go' \
		-not -name '*_test.go' \
		-exec dirname {} \; \
		| sort -u
)

if ((${#production_dirs[@]} == 0)); then
	violation 'no production Go packages were found under api/ or internal/'
fi

for package_dir in "${production_dirs[@]}"; do
	package_name="$(awk '$1 == "package" { print $2; exit }' "$package_dir"/*.go)"
	if [[ -z "$package_name" ]]; then
		violation "could not determine the package name for $package_dir"
		continue
	fi

	if ! grep -R -E -q \
		--include='*.go' \
		--exclude='*_test.go' \
		"^[[:space:]]*//[[:space:]]*Package[[:space:]]+${package_name}([[:space:]]|$)" \
		"$package_dir"; then
		violation "$package_dir lacks a Go package responsibility comment"
	fi

	if ! grep -R -E -q \
		--include='*.go' \
		--exclude='*_test.go' \
		'^[[:space:]]*//[[:space:]]*Responsibility:' \
		"$package_dir"; then
		violation "$package_dir lacks an explicit Responsibility: package boundary"
	fi

	if ! grep -R -E -q \
		--include='*.go' \
		--exclude='*_test.go' \
		'^[[:space:]]*//[[:space:]]*Boundary:' \
		"$package_dir"; then
		violation "$package_dir lacks an explicit Boundary: package contract"
	fi
done

if [[ -d "$DISCOVERY_DIR" ]]; then
	if ! grep -R -E -q \
		--include='*.go' \
		--exclude='*_test.go' \
		'^[[:space:]]*type[[:space:]]+DiscoveryClient[[:space:]]+interface[[:space:]]*\{' \
		"$DISCOVERY_DIR"; then
		violation 'DiscoveryClient must be declared by the discovery consumer'
	fi
	if ! grep -R -E -q \
		--include='*.go' \
		--exclude='*_test.go' \
		'^[[:space:]]*func[[:space:]]+NewResolver[[:space:]]*\([^)]*DiscoveryClient' \
		"$DISCOVERY_DIR"; then
		violation 'NewResolver must consume the discovery-owned DiscoveryClient port'
	fi
	if ! grep -R -E -q \
		--include='*.go' \
		--exclude='*_test.go' \
		'^[[:space:]]*type[[:space:]]+Clock[[:space:]]+func\(\)[[:space:]]+time\.Time' \
		"$DISCOVERY_DIR"; then
		violation 'Clock must be declared as a discovery-owned injectable function port'
	fi
	if ! grep -R -E -q \
		--include='*.go' \
		--exclude='*_test.go' \
		'^[[:space:]]*func[[:space:]]+WithClock[[:space:]]*\([^)]*Clock' \
		"$DISCOVERY_DIR"; then
		violation 'WithClock must expose the discovery-owned clock port'
	fi
else
	violation 'internal/discovery package is missing'
fi

production_imports_from_test="$(grep -R -n -E \
	--include='*.go' \
	--exclude='*_test.go' \
	'^[[:space:]]*([[:alnum:]_.-]+[[:space:]]+)?["]([^\"]*/)?test(/|["])' \
	api internal || true)"
if [[ -n "$production_imports_from_test" ]]; then
	violation "production code imports test code:\n$production_imports_from_test"
fi

production_packages=()
for package_dir in "${production_dirs[@]}"; do
	production_packages+=("./${package_dir#./}")
done

if ((${#production_packages[@]} > 0)); then
	if ! go list -deps "${production_packages[@]}" >/dev/null; then
		violation 'the production Go import graph is not loadable; check for an import cycle or an unavailable dependency'
	fi
fi

if ((failures > 0)); then
	printf 'Module boundary verification failed (%d violation(s))\n' "$failures" >&2
	exit 1
fi

printf '%s\n' 'Module boundary verification passed'
