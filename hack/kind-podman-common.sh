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

# Stateless process primitives shared by the persistent local workflow and
# disposable E2E. This file is intentionally source-only: it owns no state,
# identity, lifecycle decision, or cleanup policy.

readonly KP_SANITIZED_VARIABLES=(
	KUBECONFIG USE_EXISTING_CLUSTER KIND_EXPERIMENTAL_PROVIDER
	AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE AWS_DEFAULT_PROFILE
	AZURE_CLIENT_ID AZURE_CLIENT_SECRET AZURE_TENANT_ID AZURE_SUBSCRIPTION_ID AZURE_AUTH_LOCATION
	GOOGLE_APPLICATION_CREDENTIALS GOOGLE_CREDENTIALS CLOUDSDK_CONFIG CLOUDSDK_AUTH_ACCESS_TOKEN
	CLOUDSDK_CORE_PROJECT GOOGLE_CLOUD_PROJECT
)

kp_sanitized_env() {
	local variable
	local env_args=()
	for variable in "${KP_SANITIZED_VARIABLES[@]}"; do
		env_args+=(-u "$variable")
	done
	env "${env_args[@]}" "$@"
}

kp_run_kind() {
	local provider="$1"
	shift
	[[ -n "$provider" ]] || { printf '%s\n' 'kind provider must be explicit' >&2; return 64; }
	kp_sanitized_env KIND_EXPERIMENTAL_PROVIDER="$provider" kind "$@"
}

kp_run_podman() {
	kp_sanitized_env podman "$@"
}

kp_run_kubectl() {
	local kubeconfig="$1" context="$2"
	shift 2
	[[ "$kubeconfig" == /* && -n "$context" ]] || {
		printf '%s\n' 'kubectl requires an absolute kubeconfig and explicit context' >&2
		return 64
	}
	kp_sanitized_env kubectl --kubeconfig "$kubeconfig" --context "$context" "$@"
}

kp_run_helm() {
	local kubeconfig="$1" context="$2" config_home="$3" cache_home="$4" data_home="$5"
	shift 5
	[[ "$kubeconfig" == /* && -n "$context" ]] || {
		printf '%s\n' 'Helm requires an absolute kubeconfig and explicit context' >&2
		return 64
	}
	kp_sanitized_env HELM_CONFIG_HOME="$config_home" HELM_CACHE_HOME="$cache_home" \
		HELM_DATA_HOME="$data_home" helm --kubeconfig "$kubeconfig" --kube-context "$context" "$@"
}

kp_run_curl() {
	kp_sanitized_env curl "$@"
}

# Run one bounded port-forward through the same explicit kubeconfig/context
# boundary as every other Kubernetes operation. The caller owns the child PID
# and is responsible for terminating it after its deadline.
kp_run_port_forward() {
	local kubeconfig="$1" context="$2" namespace="$3" target="$4" mapping="$5"
	[[ "$kubeconfig" == /* && -n "$context" && -n "$namespace" && -n "$target" && "$mapping" == *:* ]] || {
		printf '%s\n' 'port-forward requires an absolute kubeconfig, context, namespace, target, and port mapping' >&2
		return 64
	}
	kp_run_kubectl "$kubeconfig" "$context" --namespace "$namespace" port-forward "$target" "$mapping"
}

kp_kind_nodes() {
	local provider="$1" cluster="$2"
	shift 2
	kp_run_kind "$provider" get nodes --name "$cluster" "$@"
}
