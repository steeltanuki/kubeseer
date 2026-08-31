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
readonly ADMISSION_DIR="$ROOT_DIR/internal/admission"
readonly API_DIR="$ROOT_DIR/api"
readonly CRD_DIR="$ROOT_DIR/config/crd/bases"
readonly MAKEFILE="$ROOT_DIR/Makefile"

failures=0

violation() {
	printf 'admission boundary violation: %s\n' "$1" >&2
	failures=$((failures + 1))
}

check_absent() {
	local label="$1"
	local pattern="$2"
	shift 2
	local matches
	local status

	set +e
	matches="$(rg -n --no-heading --glob '*.go' --glob '*.yaml' --glob '!**/*_test.go' "$pattern" "$@" 2>&1)"
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
		violation "required registration file is missing: ${file#"$ROOT_DIR"/}"
		return
	fi
	if ! rg -F -q -- "$text" "$file"; then
		violation "${file#"$ROOT_DIR"/} does not register or prove $text"
	fi
}

if [[ ! -d "$ADMISSION_DIR" ]]; then
	violation 'internal/admission package is missing'
else
	if find "$ADMISSION_DIR" -type f -name '*_test.go' -print -quit | rg -q .; then
		violation 'internal/admission must not add a package-local unit-test suite'
	fi
fi

if [[ -d "$API_DIR" ]]; then
	while IFS= read -r version_dir; do
		if [[ "$(basename -- "$version_dir")" != 'v1alpha1' ]]; then
			violation "second public API version directory exists: ${version_dir#"$ROOT_DIR"/}"
		fi
	done < <(find "$API_DIR" -mindepth 1 -maxdepth 1 -type d -name 'v*' -print | sort)
fi

check_absent 'second Kubeseer API version' 'kubeseer\.io/v(?:[2-9][0-9]*|1(?:alpha[2-9][0-9]*|beta[0-9]+))' "$API_DIR" "$CRD_DIR" "$ADMISSION_DIR"
check_absent 'opaque public schema' 'x-kubernetes-preserve-unknown-fields|preserveUnknownFields|preserve-unknown-fields' "$API_DIR" "$CRD_DIR"
check_absent 'SubjectAccessReview or user impersonation' 'SubjectAccessReview|subjectaccessreviews|Impersonat|Impersonation|as-user|as-group' "$ADMISSION_DIR"
check_absent 'admission-created authorization capability' 'internal/authorization|authorization\.(New|Capability|Record)|\bCapability\b|\bAuthorizedRoute\b|\bAuthorizedRead\b|BindCapabilities' "$ADMISSION_DIR"
check_absent 'admission resource-instance I/O' 'k8s\.io/client-go/(dynamic|kubernetes|metadata)|dynamic\.Interface|ResourceLister|NewDynamicResourceLister|selection\.Executor' "$ADMISSION_DIR"
check_absent 'admission Service or certificate lifecycle' 'cert-manager|certwatcher|Certificate(Request)?|corev1|NewService|Service[[:space:]]*\{|Secret[[:space:]]*\{' "$ADMISSION_DIR"

require_text "$MAKEFILE" './hack/verify-generated.sh'
require_text "$MAKEFILE" 'TestEnvtestAdmissionValidation'
require_text "$ROOT_DIR/hack/verify-test-layer-policy.sh" 'check_envtest_suite test/envtest/admission_validation_envtest_test.go TestEnvtestAdmissionValidation'
require_text "$ROOT_DIR/test/integration/installation_access_policy_test.go" 'MODULE_INTEGRATION=admission-validation STATUS=passed'
require_text "$ROOT_DIR/test/envtest/reconciliation_runtime_envtest_test.go" 'API_CONTRACT=admission-validation-runtime STATUS=passed'

if ((failures > 0)); then
	printf 'Admission boundary verification failed (%d violation(s))\n' "$failures" >&2
	exit 1
fi

printf '%s\n' 'Admission boundary verification passed'
