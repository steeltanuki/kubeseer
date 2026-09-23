#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

if (($# < 1 || $# > 2)); then
	printf 'usage: %s <apply|inspect|verify|down> [catalog-name]\n' "$0" >&2
	exit 64
fi

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly EXAMPLES_DIR="$ROOT_DIR/examples"
readonly CATALOG="$EXAMPLES_DIR/catalog.txt"
readonly COMMAND="$1"
readonly REQUESTED="${2:-}"
readonly STATE_DIR="${KUBESEER_LOCAL_STATE_DIR:-${XDG_STATE_HOME:-${HOME:?HOME is required}/.local/state}/kubeseer/local}"
readonly KUBECONFIG_PATH="$STATE_DIR/kubeconfig"
readonly CLUSTER_NAME="${LOCAL_CLUSTER_NAME:-kubeseer-local}"
readonly KUBE_CONTEXT="${LOCAL_KUBE_CONTEXT:-kind-kubeseer-local}"
readonly FIELD_MANAGER="kubeseer-local-examples"
readonly NAMESPACE_LABEL="kubeseer.io/example"

# shellcheck source=hack/kind-podman-common.sh
source "$ROOT_DIR/hack/kind-podman-common.sh"

fail() { printf 'LOCAL_EXAMPLES=%s STATUS=failed: %s\n' "$COMMAND" "$1" >&2; exit 1; }

[[ -f "$CATALOG" ]] || fail 'examples/catalog.txt is missing'
[[ -s "$KUBECONFIG_PATH" ]] || fail "owned kubeconfig is missing: $KUBECONFIG_PATH"

mapfile -t CATALOG_ENTRIES < <(sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$CATALOG")
[[ "${#CATALOG_ENTRIES[@]}" == 7 ]] || fail "catalog must contain exactly seven entries (got ${#CATALOG_ENTRIES[@]})"
declare -A seen=()
for name in "${CATALOG_ENTRIES[@]}"; do
	[[ "$name" =~ ^[a-z0-9-]+$ ]] || fail "invalid catalog name: $name"
	[[ -z "${seen[$name]+set}" ]] || fail "duplicate catalog entry: $name"
	seen[$name]=1
	[[ -d "$EXAMPLES_DIR/$name" ]] || fail "catalog entry has no directory: $name"
	[[ -f "$EXAMPLES_DIR/$name/README.md" && -f "$EXAMPLES_DIR/$name/kubeseer.yaml" && -f "$EXAMPLES_DIR/$name/workload.yaml" ]] || fail "example is incomplete: $name"
	namespace_lines="$(rg -o 'kubeseer-example-[a-z0-9-]+' "$EXAMPLES_DIR/$name" | sort -u || true)"
	[[ -n "$namespace_lines" ]] || fail "example has no stable namespace: $name"
done

for name in "${CATALOG_ENTRIES[@]}"; do
	case "$name" in
		custom-resource|partial-degradation)
			[[ -f "$EXAMPLES_DIR/$name/fixture-crd.yaml" ]] || fail "fixture CRD missing: $name"
			;;
	esac
done

kubectl_local() {
	kp_run_kubectl "$KUBECONFIG_PATH" "$KUBE_CONTEXT" "$@"
}

namespace_for() {
	local name="$1"
	case "$name" in
		builtin-resource) printf '%s\n' kubeseer-example-builtin ;;
		typed-extraction) printf '%s\n' kubeseer-example-typed ;;
		value-operator) printf '%s\n' kubeseer-example-operator ;;
		cross-namespace-aggregation) printf '%s\n' kubeseer-example-aggregation-a ;;
		custom-resource) printf '%s\n' kubeseer-example-custom ;;
		authorization-denial) printf '%s\n' kubeseer-example-denial ;;
		partial-degradation) printf '%s\n' kubeseer-example-degraded ;;
		*) return 1 ;;
	esac
}

# Some examples intentionally span more than one namespace.  Keep the
# namespace set explicit so apply and cleanup cannot accidentally broaden
# their scope or leave a sibling namespace behind.
namespaces_for() {
	local name="$1"
	case "$name" in
		cross-namespace-aggregation)
			printf '%s\n' kubeseer-example-aggregation-a kubeseer-example-aggregation-b
			;;
		*) namespace_for "$name" ;;
	esac
}

selected_entries() {
	if [[ -z "$REQUESTED" ]]; then printf '%s\n' "${CATALOG_ENTRIES[@]}"; return; fi
	[[ -n "${seen[$REQUESTED]+set}" ]] || fail "unknown catalog entry: $REQUESTED"
	printf '%s\n' "$REQUESTED"
}

apply_namespace() {
	local namespace="$1" example="$2"
	kubectl_local apply --server-side --field-manager="$FIELD_MANAGER" -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $namespace
  labels:
    $NAMESPACE_LABEL: $example
EOF
}

apply_one() {
	local name="$1" namespace
	while IFS= read -r namespace; do
		apply_namespace "$namespace" "$name"
	done < <(namespaces_for "$name")
	# CRDs are always established before their Custom Resources.
	if [[ "$name" == custom-resource || "$name" == partial-degradation ]]; then
		kubectl_local apply --server-side --field-manager="$FIELD_MANAGER" -f "$EXAMPLES_DIR/$name/fixture-crd.yaml"
		kubectl_local wait --for=condition=Established --timeout=2m crd/widgets.fixtures.kubeseer.io
	fi
	kubectl_local apply --server-side --field-manager="$FIELD_MANAGER" -f "$EXAMPLES_DIR/$name/workload.yaml"
	kubectl_local apply --server-side --field-manager="$FIELD_MANAGER" -f "$EXAMPLES_DIR/$name/kubeseer.yaml"
	printf 'EXAMPLE=%s STATUS=applied\n' "$name"
}

# The authorization-denial object must pass admission before it can exercise
# runtime policy revalidation.  Keep the baseline resource set explicit so a
# local verification run can remove Service from the active policy and restore
# the exact managed profile afterward.
narrow_authorization_denial_policy() {
	kubectl_local patch kubeseeraccesspolicy installation-access-ceiling --type=merge \
		-p='{"spec":{"resources":[{"apiGroups":[""],"kinds":["Pod"]},{"apiGroups":["apps"],"kinds":["Deployment"]},{"apiGroups":["fixtures.kubeseer.io"],"kinds":["Widget"]}]}}'
}

restore_authorization_denial_policy() {
	kubectl_local patch kubeseeraccesspolicy installation-access-ceiling --type=merge \
		-p='{"spec":{"resources":[{"apiGroups":[""],"kinds":["Pod"]},{"apiGroups":["apps"],"kinds":["Deployment"]},{"apiGroups":[""],"kinds":["Service"]},{"apiGroups":["fixtures.kubeseer.io"],"kinds":["Widget"]}]}}'
}

inspect_one() {
	local name="$1" namespace
	namespace="$(namespace_for "$name")"
	kubectl_local --namespace "$namespace" get kubeseer -l "$NAMESPACE_LABEL=$name" -o json
}

verify_one() {
	local name="$1" command=(go run ./cmd/kubeseer-local) namespace
	namespace="$(namespace_for "$name")"
	if [[ -n "${KUBESEER_LOCAL_PROBE_BIN:-}" ]]; then command=("$KUBESEER_LOCAL_PROBE_BIN"); fi
	if [[ "$name" == partial-degradation ]]; then
		# First let the initial successful reconciliation publish Ready.  This
		# prevents the fixture removal from racing the first evaluation when the
		# complete catalog was just (re)applied.
		kubectl_local --namespace "$namespace" wait --for=condition=Ready --timeout="${KUBESEER_LOCAL_TIMEOUT:-2m}" kubeseer/degraded
		# Leave the successful Deployment in place, then remove only the
		# degradable Widget and its fixture type before observing status.
		kubectl_local --namespace "$namespace" delete widget degraded-widget --ignore-not-found=true >/dev/null || true
		kubectl_local delete crd widgets.fixtures.kubeseer.io --ignore-not-found=true >/dev/null || true
	fi
	if [[ "$name" == authorization-denial ]]; then
		# Admission already accepted the object while Service was in the local
		# profile.  Narrowing only the persisted policy now exercises the runtime
		# AuthorizationDenied path required by this example.
		local verify_status
		narrow_authorization_denial_policy
		if "${command[@]}" verify --state-dir "$STATE_DIR" --metadata "$STATE_DIR/metadata.v1" --kubeconfig "$KUBECONFIG_PATH" --cluster-name "$CLUSTER_NAME" --context "$KUBE_CONTEXT" --timeout "${KUBESEER_LOCAL_TIMEOUT:-2m}" --example "$name" --namespace "$namespace"; then
			verify_status=0
		else
			verify_status=$?
		fi
		if ! restore_authorization_denial_policy; then
			printf '%s\n' 'failed to restore installation-access-ceiling after authorization-denial verification' >&2
			return 1
		fi
		((verify_status == 0)) || return "$verify_status"
	else
		"${command[@]}" verify --state-dir "$STATE_DIR" --metadata "$STATE_DIR/metadata.v1" --kubeconfig "$KUBECONFIG_PATH" --cluster-name "$CLUSTER_NAME" --context "$KUBE_CONTEXT" --timeout "${KUBESEER_LOCAL_TIMEOUT:-2m}" --example "$name" --namespace "$namespace"
	fi
	printf 'EXAMPLE=%s STATUS=passed\n' "$name"
}

down_one() {
	local name="$1" namespace
	namespace="$(namespace_for "$name")"
	kubectl_local --namespace "$namespace" delete -f "$EXAMPLES_DIR/$name/kubeseer.yaml" --ignore-not-found=true >/dev/null
	if [[ "$name" == partial-degradation ]]; then
		kubectl_local --namespace "$namespace" delete deployment degraded --ignore-not-found=true >/dev/null || true
		kubectl_local --namespace "$namespace" delete widget degraded-widget --ignore-not-found=true >/dev/null || true
	elif [[ "$name" == custom-resource ]]; then
		# The partial-degradation example may already have removed the shared
		# fixture CRD. Avoid asking kubectl to resolve a type that no longer
		# exists while still deleting the CR before the CRD when present.
		if kubectl_local get crd widgets.fixtures.kubeseer.io >/dev/null 2>&1; then
			kubectl_local --namespace "$namespace" delete -f "$EXAMPLES_DIR/$name/workload.yaml" --ignore-not-found=true >/dev/null
		fi
	elif [[ "$name" == cross-namespace-aggregation ]]; then
		# The workload manifest deliberately contains objects in both exact
		# aggregation namespaces; let each object namespace from the manifest
		# drive deletion instead of imposing namespace A on namespace B.
		kubectl_local delete -f "$EXAMPLES_DIR/$name/workload.yaml" --ignore-not-found=true >/dev/null
	else
		kubectl_local --namespace "$namespace" delete -f "$EXAMPLES_DIR/$name/workload.yaml" --ignore-not-found=true >/dev/null
	fi
	if [[ "$name" == custom-resource || "$name" == partial-degradation ]]; then
		kubectl_local delete -f "$EXAMPLES_DIR/$name/fixture-crd.yaml" --ignore-not-found=true >/dev/null
	fi
	while IFS= read -r namespace; do
		kubectl_local delete namespace "$namespace" --ignore-not-found=true --wait=false >/dev/null
	done < <(namespaces_for "$name")
	printf 'EXAMPLE=%s STATUS=removed\n' "$name"
}

case "$COMMAND" in
	apply)
		while IFS= read -r name; do apply_one "$name"; done < <(selected_entries)
		;;
	inspect)
		[[ -n "$REQUESTED" ]] || fail 'inspect requires one catalog name'
		inspect_one "$REQUESTED"
		;;
	verify)
		while IFS= read -r name; do verify_one "$name"; done < <(selected_entries)
		;;
	down)
		mapfile -t selected < <(selected_entries)
		for ((index=${#selected[@]}-1; index>=0; index--)); do down_one "${selected[$index]}"; done
		;;
	*) fail "unsupported command: $COMMAND" ;;
esac
