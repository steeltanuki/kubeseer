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
readonly PROFILE="$ROOT_DIR/internal/limits/profile.go"
readonly LIMITS_DIR="$ROOT_DIR/internal/limits"
readonly OBS_DIR="$ROOT_DIR/internal/observability"
readonly RECONCILIATION_DIR="$ROOT_DIR/internal/reconciliation"
readonly API_DIR="$ROOT_DIR/api"
readonly CONFIG_DIR="$ROOT_DIR/config"
readonly MAKEFILE="$ROOT_DIR/Makefile"
readonly TEST_LAYER_POLICY="$ROOT_DIR/hack/verify-test-layer-policy.sh"
readonly INTEGRATION_TEST="$ROOT_DIR/test/integration/installation_access_policy_test.go"
readonly PERFORMANCE_TEST="$ROOT_DIR/test/integration/performance_limits_profile_test.go"
readonly ENVTEST="$ROOT_DIR/test/envtest/reconciliation_runtime_envtest_test.go"

failures=0

violation() {
	printf 'performance and limits boundary violation: %s\n' "$1" >&2
	failures=$((failures + 1))
}

check_absent() {
	local label="$1"
	local pattern="$2"
	shift 2
	local matches
	local status
	set +e
	matches="$(rg -n --no-heading --glob '*.go' --glob '*.yaml' --glob '*.yml' --glob '!**/*_test.go' "$pattern" "$@" 2>&1)"
	status=$?
	set -e
	if [[ "$status" -eq 0 ]]; then
		violation "$label:\n$matches"
	elif [[ "$status" -gt 1 ]]; then
		violation "$label check failed:\n$matches"
	fi
}

require_text() {
	local file="$1"
	local expected="$2"
	if [[ ! -f "$file" ]]; then
		violation "required file is missing: ${file#"$ROOT_DIR"/}"
		return
	fi
	if ! rg -F -q -- "$expected" "$file"; then
		violation "${file#"$ROOT_DIR"/} does not contain $expected"
	fi
}

check_header() {
	local file="$1"
	local first_line
	local second_line
	first_line="$(sed -n '1p' "$file")"
	second_line="$(sed -n '2p' "$file")"
	if [[ "$first_line" != '// Copyright 2026 Alessandro Rontani' && "$first_line" != '# Copyright 2026 Alessandro Rontani' && "$second_line" != '// Copyright 2026 Alessandro Rontani' && "$second_line" != '# Copyright 2026 Alessandro Rontani' ]]; then
		violation "${file#"$ROOT_DIR"/} is missing the approved Apache header"
	fi
}

check_header "$PROFILE"
check_header "$ROOT_DIR/hack/verify-performance-and-limits-boundaries.sh"
check_header "$PERFORMANCE_TEST"

check_absent 'public limit or performance fields' 'Max(PageSize|MatchedResources|SelectedInputBytes|ProducedValueBytes|StatusBytes|ConcurrentReconciles|ActiveWatches|PendingTriggers)|DiscoveryCache|EvaluationTimeout|[Pp]erformance|[Ll]imits' "$API_DIR" "$CONFIG_DIR"
check_absent 'mutable global profile state' '^[[:space:]]*var[[:space:]].*[Pp]rofile' "$LIMITS_DIR"
check_absent 'payload, result, or value cache' '[Cc]ache[^[:alnum:]]*(body|result|value)| (body|result|value)[^[:alnum:]]*[Cc]ache|map\[[^]]+\][[:space:]]*(\*?unstructured|v1alpha1\.KubeseerResult)' "$LIMITS_DIR" "$RECONCILIATION_DIR" "$OBS_DIR"
check_absent 'custom worker queue' 'New(Delaying|RateLimiting|Named).*Queue|RateLimitingQueue|DelayingQueue' "$RECONCILIATION_DIR"
check_absent 'unbounded limit metric labels' 'WithLabelValues\([^)]*(dimension|ceiling|source|namespace|name|uid|selector|path|value|message|body)' "$OBS_DIR"
check_absent 'forbidden diagnostic fields' '^[[:space:]]*(Body|Selector|FieldPath|ExtractedValue|TypedValue|AggregateValue|Secret|Cause|ObservedObject|ObservedUID)[[:space:]]' "$OBS_DIR"
if find "$LIMITS_DIR" -type f -name '*_test.go' -print -quit | rg -q .; then
	violation 'internal/limits must remain covered through module integration rather than package-local test substitutions'
fi

# These values are the approved default profile and the admission structural
# ceilings. Keeping the table in this verifier makes an accidental relaxation
# visible before a generated/API review.
require_text "$PROFILE" 'DefaultPageSize                int64 = 500'
require_text "$PROFILE" 'DefaultMaxMatchedResources     int   = 1000'
require_text "$PROFILE" 'DefaultSelectedInputBytes      int64 = 8 * 1024 * 1024'
require_text "$PROFILE" 'DefaultProducedValueBytes      int64 = 8 * 1024 * 1024'
require_text "$PROFILE" 'DefaultMaxStatusBytes          int64 = 512 * 1024'
require_text "$PROFILE" 'DefaultMaxConcurrentReconciles int   = 4'
require_text "$PROFILE" 'DefaultDiscoveryCacheEntries   int   = 1024'
require_text "$PROFILE" 'DefaultMaxActiveWatches        int   = 1024'
require_text "$PROFILE" 'DefaultMaxPendingTriggers      int   = 4096'
require_text "$PROFILE" 'ReasonInvalidLimitConfiguration = "InvalidLimitConfiguration"'
require_text "$PROFILE" 'override exceeds generated structural ceiling'
require_text "$MAKEFILE" './hack/verify-generated.sh'
require_text "$MAKEFILE" './hack/verify-performance-and-limits-boundaries.sh'
require_text "$TEST_LAYER_POLICY" 'check_envtest_suite test/envtest/reconciliation_runtime_envtest_test.go TestEnvtestReconciliationRuntime'
require_text "$INTEGRATION_TEST" 'MODULE_INTEGRATION=performance-and-limits STATUS=passed'
require_text "$PERFORMANCE_TEST" 'MODULE_INTEGRATION=performance-and-limits STATUS=passed'
require_text "$ENVTEST" 'API_CONTRACT=performance-and-limits STATUS=passed'

if ((failures > 0)); then
	printf 'Performance and limits boundary verification failed (%d violation(s))\n' "$failures" >&2
	exit 1
fi

printf '%s\n' 'Performance and limits boundary verification passed'
