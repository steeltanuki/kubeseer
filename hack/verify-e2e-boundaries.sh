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

if (($# != 1)); then
	printf 'usage: %s <foundation|data-pipeline|negative-boundaries|lifecycle|complete>\n' "$0" >&2
	exit 64
fi

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly E2E_DIR="$ROOT_DIR/test/e2e"
readonly MANIFEST="$E2E_DIR/certification.json"
readonly MODE="$1"

fail() {
	printf 'E2E_VERIFY=%s STATUS=failed: %s\n' "$MODE" "$1" >&2
	exit 1
}

command -v go >/dev/null 2>&1 || fail 'go is required'
command -v jq >/dev/null 2>&1 || fail 'jq is required for manifest verification'
[[ -d "$E2E_DIR" ]] || fail 'test/e2e directory is missing'
[[ -f "$MANIFEST" ]] || fail 'certification manifest is missing'
for required_file in e2e_test.go scenarios.go session.go fixtures.go observer.go assertions.go fixtures/widget-crd.yaml; do
	[[ -f "$E2E_DIR/$required_file" ]] || fail "required E2E file is missing: $required_file"
done

feature_count="$(jq '(.features | length)' "$MANIFEST")"
minimum_count="$(jq '(.minimumScenarios | length)' "$MANIFEST")"
scenario_count="$(jq '(.scenarios | length)' "$MANIFEST")"
prerequisite_count="$(jq '(.prerequisites | length)' "$MANIFEST")"
[[ "$feature_count" == 16 ]] || fail "manifest must declare exactly sixteen features (got $feature_count)"
[[ "$minimum_count" == 20 ]] || fail "manifest must declare exactly twenty minimum scenarios (got $minimum_count)"
[[ "$scenario_count" == 15 ]] || fail "manifest must declare exactly fifteen stable IDs (got $scenario_count)"
[[ "$prerequisite_count" -ge 1 ]] || fail 'manifest has no prerequisite certifications'
[[ "$(jq '[.features[].name] | length == (unique | length)' "$MANIFEST")" == true ]] || fail 'feature names are duplicated'
[[ "$(jq '[.minimumScenarios[].key] | length == (unique | length)' "$MANIFEST")" == true ]] || fail 'minimum scenario keys are duplicated'
[[ "$(jq '[.scenarios[].id] | length == (unique | length)' "$MANIFEST")" == true ]] || fail 'stable scenario IDs are duplicated'
[[ "$(jq -r '[.scenarios[].id] | sort | join(" ")' "$MANIFEST")" == 'E2E-001 E2E-002 E2E-003 E2E-004 E2E-005 E2E-006 E2E-007 E2E-008 E2E-009 E2E-010 E2E-011 E2E-012 E2E-013 E2E-014 E2E-015' ]] || fail 'stable IDs do not match the approved set'

for file in "$E2E_DIR"/*.go "$E2E_DIR"/fixtures/*.yaml; do
	[[ -f "$file" ]] || continue
	first_line="$(sed -n '1p' "$file")"
	[[ "$first_line" == '// Copyright 2026 Alessandro Rontani' || "$first_line" == '# Copyright 2026 Alessandro Rontani' ]] || fail "missing Apache header: ${file#"$ROOT_DIR"/}"
done

if rg -n 'github\.com/steeltanuki/kubeseer/internal/|testing\.T\.Skip|testing\.TB\.Skip|time\.Sleep|(^|[[:space:]])sleep([[:space:]]|\(|$)' "$E2E_DIR" >/dev/null; then
	fail 'public-boundary suite contains a production import, skip, or sleep'
fi
if rg -n 'fake|envtest|test-seam|controller-runtime/pkg/client' "$E2E_DIR" --glob '*.go' >/dev/null; then
	fail 'public-boundary suite contains a fake, envtest, or test-seam dependency'
fi

while IFS= read -r prerequisite; do
	command_name="${prerequisite%% *}"
	case "$command_name" in
	make)
		[[ -f "$ROOT_DIR/Makefile" ]] || fail "missing prerequisite entry point: $prerequisite"
		;;
	\./*)
		[[ -x "$ROOT_DIR/${command_name#./}" ]] || fail "missing prerequisite entry point: $prerequisite"
		;;
	*)
		fail "unsupported prerequisite command: $prerequisite"
		;;
	esac
done < <(jq -r '.prerequisites[].command' "$MANIFEST")

module_cache="${GOMODCACHE:-}"
if [[ -z "$module_cache" || ! -d "$module_cache/k8s.io/apimachinery@v0.36.0" ]]; then
	module_cache="$(env -u GOMODCACHE go env GOMODCACHE)"
fi
list_output="$(GOCACHE="${GOCACHE:-/tmp/kubeseer-e2e-go-build}" GOMODCACHE="$module_cache" GOPROXY=off go test -list '^TestEndToEnd$' ./test/e2e/... 2>&1)" || fail "suite listing failed: $list_output"
printf '%s\n' "$list_output" | rg -q '^TestEndToEnd$' || fail 'exact TestEndToEnd selection is not listed'
GOCACHE="${GOCACHE:-/tmp/kubeseer-e2e-go-build}" GOMODCACHE="$module_cache" GOPROXY=off go test -run '^$' ./test/e2e/... >/dev/null || fail 'suite compilation failed'

case "$MODE" in
	foundation)
		;;
	data-pipeline)
		rg -q 'scenarioNotImplemented\("E2E-002"\)' "$E2E_DIR/scenarios.go" && fail 'data-pipeline scenarios remain unimplemented'
		rg -q 'scenarioNotImplemented\("E2E-006"\)' "$E2E_DIR/scenarios.go" && fail 'aggregation scenario remains unimplemented'
		;;
	negative-boundaries)
		rg -q 'scenarioNotImplemented\("E2E-007"\)' "$E2E_DIR/scenarios.go" && fail 'negative boundary scenarios remain unimplemented'
		rg -q 'scenarioNotImplemented\("E2E-009"\)' "$E2E_DIR/scenarios.go" && fail 'limit scenario remains unimplemented'
		;;
	lifecycle)
		for id in E2E-010 E2E-011 E2E-012 E2E-013 E2E-014; do
			rg -q "scenarioNotImplemented(\"$id\")" "$E2E_DIR/scenarios.go" && fail "$id remains unimplemented"
		done
		;;
	complete)
		rg -q 'scenarioNotImplemented' "$E2E_DIR/scenarios.go" && fail 'scenario registry still contains an unimplemented entry'
		;;
	*)
		fail "unknown verification mode: $MODE"
		;;
esac

printf 'E2E_VERIFY=%s STATUS=passed\n' "$MODE"
