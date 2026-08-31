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
readonly OBS_DIR="$ROOT_DIR/internal/observability"
readonly API_DIR="$ROOT_DIR/api"
readonly CONFIG_DIR="$ROOT_DIR/config"
readonly MAKEFILE="$ROOT_DIR/Makefile"
readonly TEST_LAYER_POLICY="$ROOT_DIR/hack/verify-test-layer-policy.sh"
readonly INTEGRATION_TEST="$ROOT_DIR/test/integration/installation_access_policy_test.go"
readonly ENVTEST="$ROOT_DIR/test/envtest/reconciliation_runtime_envtest_test.go"

failures=0

violation() {
	printf 'observability boundary violation: %s\n' "$1" >&2
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
	local text="$2"
	if [[ ! -f "$file" ]]; then
		violation "required file is missing: ${file#"$ROOT_DIR"/}"
		return
	fi
	if ! rg -F -q -- "$text" "$file"; then
		violation "${file#"$ROOT_DIR"/} does not contain $text"
	fi
}

if [[ ! -d "$OBS_DIR" ]]; then
	violation 'internal/observability package is missing'
else
	if find "$OBS_DIR" -type f -name '*_test.go' -print -quit | rg -q .; then
		violation 'internal/observability must not contain package-local tests'
	fi
	while IFS= read -r file; do
		if [[ "$(sed -n '1p' "$file")" != '// Copyright 2026 Alessandro Rontani' ]]; then
			violation "${file#"$ROOT_DIR"/} is missing the approved Apache header"
		fi
	done < <(find "$OBS_DIR" -type f -name '*.go' -print | sort)
fi

check_absent 'asynchronous telemetry queue or goroutine' 'go[[:space:]]+func|make\([[:space:]]*chan|workqueue|chan[[:space:]]+|chan\)' "$OBS_DIR"
check_absent 'telemetry enrichment I/O' 'k8s\.io/client-go/(dynamic|kubernetes|metadata)|controller-runtime/pkg/client|dynamic\.Interface|metadata\.Interface|\.Get\(|\.List\(|\.Watch\(' "$OBS_DIR"
check_absent 'custom metrics endpoint or server' 'metrics/server|ListenAndServe|prometheus\.Handler|http\.' "$OBS_DIR"
check_absent 'durable audit storage or packaging resource' 'database/sql|gorm|os\.(Create|Open|OpenFile|WriteFile)|WriteFile|ConfigMap|Secret|Service[[:space:]]*\{' "$OBS_DIR"
check_absent 'free-form or high-cardinality metric labels' 'WithLabelValues\([^)]*(SourceID|Namespace|Name|UID|Generation|Selector|Path|Value|Message|PolicyIdentity)' "$OBS_DIR"
check_absent 'observability public API or CRD addition' 'observability' "$API_DIR" "$CONFIG_DIR"

if find "$CONFIG_DIR" -type f \( -iname '*observability*' -o -iname '*telemetry*' -o -iname '*dashboard*' -o -iname '*servicemonitor*' \) -print -quit | rg -q .; then
	violation 'observability packaging, dashboard, or ServiceMonitor resources are forbidden'
fi

require_text "$OBS_DIR/metrics.go" 'metricReconciliations        = "kubeseer_reconciliations_total"'
require_text "$OBS_DIR/metrics.go" 'metricReconciliationDuration = "kubeseer_reconciliation_duration_seconds"'
require_text "$OBS_DIR/metrics.go" 'metricResourcesRead          = "kubeseer_resources_read_total"'
require_text "$OBS_DIR/metrics.go" 'metricSourceFailures         = "kubeseer_source_failures_total"'
require_text "$OBS_DIR/metrics.go" 'metricResultsProduced        = "kubeseer_results_produced_total"'
require_text "$OBS_DIR/metrics.go" 'metricStatusUpdates          = "kubeseer_status_updates_total"'
require_text "$OBS_DIR/metrics.go" 'metricAuthorization          = "kubeseer_authorization_decisions_total"'
require_text "$OBS_DIR/metrics.go" 'metricJSONPathFailures       = "kubeseer_jsonpath_failures_total"'
require_text "$OBS_DIR/metrics.go" 'metricWatchRestarts          = "kubeseer_source_watch_restarts_total"'
require_text "$OBS_DIR/metrics.go" '}, []string{"outcome", "reason"})'
require_text "$OBS_DIR/metrics.go" '}, []string{"outcome"})'
require_text "$OBS_DIR/metrics.go" '}, []string{"scope"})'
require_text "$OBS_DIR/metrics.go" '}, []string{"stage", "reason"})'
require_text "$OBS_DIR/metrics.go" '}, []string{"kind", "outcome", "reason"})'

require_text "$MAKEFILE" './hack/verify-observability-boundaries.sh'
require_text "$TEST_LAYER_POLICY" 'check_envtest_suite test/envtest/reconciliation_runtime_envtest_test.go TestEnvtestReconciliationRuntime'
require_text "$INTEGRATION_TEST" 'MODULE_INTEGRATION=observability STATUS=passed'
require_text "$ENVTEST" 'API_CONTRACT=observability-events STATUS=passed'

if ((failures > 0)); then
	printf 'Observability boundary verification failed (%d violation(s))\n' "$failures" >&2
	exit 1
fi

printf '%s\n' 'Observability boundary verification passed'
