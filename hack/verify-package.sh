#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly chart_dir="$ROOT_DIR/charts/kubeseer"
readonly temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-package-verify.XXXXXX")"
cleanup() {
	chmod -R u+w "$temp_dir" 2>/dev/null || true
	rm -rf "$temp_dir"
}
trap cleanup EXIT
cd -- "$ROOT_DIR"

fail() {
	printf 'PACKAGE_VERIFY=chart-core STATUS=failed: %s\n' "$1" >&2
	exit 1
}

fail_crd() {
	printf 'PACKAGE_VERIFY=crd-lifecycle STATUS=failed: %s\n' "$1" >&2
	exit 1
}

fail_complete() {
	printf 'PACKAGE_VERIFY=complete STATUS=failed: %s\n' "$1" >&2
	exit 1
}

command -v helm >/dev/null 2>&1 || fail "helm is required"
[[ -f "$chart_dir/Chart.yaml" ]] || fail "Chart.yaml is missing"
[[ -f "$chart_dir/values.yaml" ]] || fail "values.yaml is missing"
[[ -f "$chart_dir/values.schema.json" ]] || fail "values.schema.json is missing"
[[ -f "$chart_dir/templates/_helpers.tpl" ]] || fail "canonical helpers are missing"
[[ -f "$chart_dir/crds/kubeseer.io_kubeseers.yaml" ]] || fail "Kubeseer CRD is missing"
[[ -f "$chart_dir/crds/kubeseer.io_kubeseeraccesspolicies.yaml" ]] || fail "access policy CRD is missing"

# Helm 3.12 has no lint-specific kube-version flag and assumes v1.27. The
# actual chart gate is checked by helm template below; lint a disposable copy
# with only that local capability gate widened so the verification remains
# usable with the pinned minimum Helm CLI.
lint_chart="$temp_dir/lint-chart"
cp -R "$chart_dir" "$lint_chart"
sed -i 's/^kubeVersion: .*/kubeVersion: ">=1.27.0-0 <1.37.0-0"/' "$lint_chart/Chart.yaml"
helm lint "$lint_chart" >/dev/null || fail "helm lint failed"

helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 --include-crds >"$temp_dir/render-a.yaml" || fail "default render failed"
helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 --include-crds >"$temp_dir/render-b.yaml" || fail "repeat render failed"
cmp -s "$temp_dir/render-a.yaml" "$temp_dir/render-b.yaml" || fail "repeated render is not byte-for-byte deterministic"

for profile in \
	'default' \
	'external --set certificate.mode=externalSecret --set certificate.externalSecret.secretName=administrator-webhook-tls --set-string certificate.externalSecret.caBundle=PUBLIC-CA' \
	'namespace --set accessPolicy.explicitNamespaces[0]=team-a' \
	'observed --set-json rbac.observed.namespaced=[{"namespace":"team-a","apiGroups":["apps"],"resources":["deployments"],"verbs":["get","list","watch"]}]' \
	'cluster --set-json rbac.observed.clusterScoped=[{"apiGroups":[""],"resources":["nodes"],"verbs":["get"]}] --set accessPolicy.allowClusterScoped=true' \
	'tracing --set tracing.enabled=true --set tracing.endpoint=http://otel-collector:4317' \
	'limits --set limits.pageSize=100 --set limits.maxConcurrentReconciles=2'; do
	read -r -a profile_args <<<"$profile"
	profile_name="${profile_args[0]}"
	profile_output="$temp_dir/profile-${profile_name}.yaml"
	helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 --include-crds "${profile_args[@]:1}" >"$profile_output" || fail "values profile ${profile_name} failed"
	[[ -s "$profile_output" ]] || fail "values profile ${profile_name} rendered no objects"
done

helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 \
	--set certificate.mode=externalSecret \
	--set certificate.externalSecret.secretName=administrator-webhook-tls \
	--set-string certificate.externalSecret.caBundle=PUBLIC-CA >"$temp_dir/external.yaml" || fail "externalSecret render failed"
if rg -q 'cert-manager.io/|kind: Secret' "$temp_dir/external.yaml"; then
	fail "externalSecret render owns certificate provider or TLS Secret state"
fi
rg -q 'secretName: "administrator-webhook-tls"' "$temp_dir/external.yaml" || fail "externalSecret mount is missing"

for invalid in \
	'--set-string image.tag=latest' \
	'--set certificate.mode=unsupported' \
	'--set certificate.mode=externalSecret' \
	'--set tracing.enabled=true --set tracing.endpoint=' \
	'--set replicaCount=0' \
	'--set-json rbac.observed.namespaced=[{"namespace":"team-a","apiGroups":["*"],"resources":["deployments"],"verbs":["get"]}]' \
	'--set-json rbac.observed.clusterScoped=[{"apiGroups":[""],"resources":["nodes"],"verbs":["get"]}]'; do
	read -r -a invalid_args <<<"$invalid"
	if helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 "${invalid_args[@]}" > /dev/null 2>&1; then
		fail "invalid values were accepted: ${invalid}"
	fi
done

rg -q 'runAsNonRoot: true' "$temp_dir/render-a.yaml" || fail "non-root workload security is missing"
rg -q 'runAsUser: 65532' "$temp_dir/render-a.yaml" || fail "numeric non-root workload identity is missing"
rg -q 'readOnlyRootFilesystem: true' "$temp_dir/render-a.yaml" || fail "read-only root filesystem is missing"
rg -q 'type: RuntimeDefault' "$temp_dir/render-a.yaml" || fail "runtime-default seccomp is missing"
rg -q 'maxUnavailable: 0' "$temp_dir/render-a.yaml" || fail "rolling availability boundary is missing"
rg -q 'failurePolicy: Fail' "$temp_dir/render-a.yaml" || fail "fail-closed webhook policy is missing"
if rg -n 'resources: \["(\*|secrets)"\]|verbs: \["\*"\]|hostNetwork: true|hostPID: true|hostIPC: true' "$temp_dir/render-a.yaml" >/dev/null; then
	fail "default package contains over-broad RBAC or host access"
fi

for required_script in \
	verify-package.sh package-sync-crds.sh check-crd-compatibility.sh apply-compatible-crds.sh \
	uninstall-kubeseer.sh test-package-compatibility.sh package-cluster-smoke.sh; do
	[[ -x "$ROOT_DIR/hack/$required_script" ]] || fail "package script is missing or not executable: ${required_script}"
done
for required_dockerfile in \
	'^FROM scratch' \
	'USER 65532:65532' \
	'ENTRYPOINT \["/kubeseer", "manager"\]' \
	'COPY --from=build /out/kubeseer /kubeseer'; do
	rg -q "$required_dockerfile" "$ROOT_DIR/Dockerfile" || fail "Dockerfile constraint is missing: ${required_dockerfile}"
done

cmp -s config/crd/bases/kubeseer.io_kubeseers.yaml "$chart_dir/crds/kubeseer.io_kubeseers.yaml" || fail "Kubeseer CRD drifted from generated source"
cmp -s config/crd/bases/kubeseer.io_kubeseeraccesspolicies.yaml "$chart_dir/crds/kubeseer.io_kubeseeraccesspolicies.yaml" || fail "access policy CRD drifted from generated source"

if rg -n '(^|[[:space:]])lookup[[:space:](]' "$chart_dir" >/dev/null; then
	fail "chart templates must not use lookup"
fi
if helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 --set-string image.tag=latest >/dev/null 2>&1; then
	fail "mutable latest tag was accepted"
fi
if helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 --set certificate.mode=unsupported >/dev/null 2>&1; then
	fail "unsupported certificate mode was accepted"
fi
if helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 --set certificate.mode=externalSecret >/dev/null 2>&1; then
	fail "incomplete externalSecret values were accepted"
fi

helm template kubeseer "$chart_dir" --namespace team-a --kube-version 1.35.6 \
	--set certificate.mode=externalSecret \
	--set certificate.externalSecret.secretName=webhook-tls \
	--set-string certificate.externalSecret.caBundle=PUBLIC-CA >/dev/null || fail "valid externalSecret values failed"
helm package "$chart_dir" --destination "$temp_dir" >/dev/null || fail "chart packaging failed"
archive="$(find "$temp_dir" -maxdepth 1 -type f -name 'kubeseer-*.tgz' -print -quit)"
[[ -n "$archive" ]] || fail "versioned chart archive was not produced"
tar -tzf "$archive" >"$temp_dir/archive-files" || fail "archive listing failed"
rg -q '^kubeseer/Chart.yaml$' "$temp_dir/archive-files" || fail "archive lacks Chart.yaml"
if rg -i '(^|/)(id_rsa|.*\.key|.*\.pem|credentials|secret)' "$temp_dir/archive-files" >/dev/null; then
	fail "archive contains credential-like content"
fi

for required_text in image replicaCount resources manager metrics tracing certificate accessPolicy rbac limits; do
	rg -q "$required_text" "$chart_dir/README.md" "$chart_dir/values.yaml" || fail "public value $required_text is undocumented"
done
rg -q 'app.kubernetes.io/name:' "$temp_dir/render-a.yaml" || fail "standard identity labels are missing"
rg -q 'meta.helm.sh/release-name:' "$temp_dir/render-a.yaml" || fail "release ownership is missing"
rg -q 'appVersion: "0\.1\.0"' "$chart_dir/Chart.yaml" || fail "appVersion is not immutable"
rg -q 'version: 0\.1\.0' "$chart_dir/Chart.yaml" || fail "chart version is not SemVer"
rg -q 'kubeVersion:' "$chart_dir/Chart.yaml" || fail "Kubernetes compatibility range is missing"

printf 'PACKAGE_VERIFY=chart-core STATUS=passed\n'

# The persistent local profile composes the same chart and is checked here so
# a package change cannot silently broaden the example policy or certificate
# mode. The catalog is deliberately parsed as data (comments and blank lines
# are ignored), never sourced as shell.
local_values="$ROOT_DIR/config/local/values.yaml"
local_catalog="$ROOT_DIR/examples/catalog.txt"
[[ -f "$local_values" ]] || fail_complete "local values profile is missing"
[[ -f "$local_catalog" ]] || fail_complete "local example catalog is missing"
local_render="$temp_dir/local-render.yaml"
helm template kubeseer "$chart_dir" --namespace kubeseer-system --kube-version 1.35.6 \
	--values "$local_values" >"$local_render" || fail_complete "local values profile rejected by chart schema"
rg -q '^  mode: certManager$' "$local_values" || fail_complete "local profile must use certManager mode"
rg -q 'pullPolicy: IfNotPresent' "$local_values" || fail_complete "local profile must use IfNotPresent"
rg -q 'installation-access-ceiling' "$local_render" || fail_complete "local profile policy singleton is missing"
if rg -n "resources:[[:space:]]*\\[\\\"(\\*|secrets?)\\\"\\]|verbs:[[:space:]]*\\[\\\"\\*\\\"\\]" "$local_values" >/dev/null; then
	fail_complete "local profile contains wildcard or Secret access"
fi
mapfile -t local_examples < <(sed -e 's/[[:space:]]*#.*$//' -e '/^[[:space:]]*$/d' "$local_catalog")
[[ "${#local_examples[@]}" == 7 ]] || fail_complete "local catalog must contain seven examples"
for local_example in "${local_examples[@]}"; do
	[[ -d "$ROOT_DIR/examples/$local_example" ]] || fail_complete "catalog directory is missing: $local_example"
	rg -q -- "$local_example" "$local_values" || fail_complete "local policy does not document catalog example: $local_example"
done
printf 'PACKAGE_VERIFY=local-profile STATUS=passed\n'

generated_dir="$temp_dir/generated-crds"
mkdir -p "$generated_dir"
tool_module_cache="$(env -u GOMODCACHE go env GOMODCACHE)"
local_go_proxy="file://$tool_module_cache/cache/download"
build_module_cache="$temp_dir/go-mod-cache"
GOCACHE="$temp_dir/go-cache" GOMODCACHE="$build_module_cache" GOSUMDB=off GOPROXY="$local_go_proxy" \
	go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.20.1 \
		crd:crdVersions=v1 paths=./api/v1alpha1 output:crd:artifacts:config="$generated_dir" >/dev/null || fail_crd "fresh CRD generation failed"
for name in kubeseer.io_kubeseers.yaml kubeseer.io_kubeseeraccesspolicies.yaml; do
	cmp -s "config/crd/bases/$name" "$generated_dir/$name" || fail_crd "generated source drifted for $name"
	cmp -s "config/crd/bases/$name" "$chart_dir/crds/$name" || fail_crd "chart CRD drifted for $name"
done

kubeseer_digest="$(sha256sum config/crd/bases/kubeseer.io_kubeseers.yaml | awk '{print $1}')"
policy_digest="$(sha256sum config/crd/bases/kubeseer.io_kubeseeraccesspolicies.yaml | awk '{print $1}')"
rg -q "generated-crd-kubeseer-sha256: $kubeseer_digest" "$chart_dir/Chart.yaml" || fail_crd "Kubeseer CRD digest metadata is stale"
rg -q "generated-crd-access-policy-sha256: $policy_digest" "$chart_dir/Chart.yaml" || fail_crd "access policy CRD digest metadata is stale"
if rg -n 'kind:[[:space:]]+CustomResourceDefinition' "$chart_dir/templates" >/dev/null; then
	fail_crd "CRDs must remain under chart crds/"
fi
rg -q 'make package-sync-crds' "$chart_dir/README.md" || fail_crd "CRD synchronization command is undocumented"
rg -q 'does not upgrade CRDs' "$chart_dir/README.md" || fail_crd "Helm CRD upgrade behavior is undocumented"
rg -q -- '--server-side' hack/apply-compatible-crds.sh || fail_crd "CRD apply is not server-side"
rg -q -- '--field-manager=kubeseer-crd-upgrade' hack/apply-compatible-crds.sh || fail_crd "CRD field manager is not fixed"
rg -q -- '--for=condition=Established' hack/apply-compatible-crds.sh || fail_crd "CRD establishment wait is missing"
rg -q 'scope: Namespaced' config/crd/bases/kubeseer.io_kubeseers.yaml || fail_crd "Kubeseer scope changed"
rg -q 'scope: Cluster' config/crd/bases/kubeseer.io_kubeseeraccesspolicies.yaml || fail_crd "access policy scope changed"
rg -q 'subresources:' config/crd/bases/kubeseer.io_kubeseers.yaml || fail_crd "Kubeseer status subresource is missing"
rg -q 'storage: true' config/crd/bases/kubeseer.io_kubeseers.yaml || fail_crd "Kubeseer storage version is missing"
rg -q 'storage: true' config/crd/bases/kubeseer.io_kubeseeraccesspolicies.yaml || fail_crd "access policy storage version is missing"

printf 'PACKAGE_VERIFY=crd-lifecycle STATUS=passed\n'

[[ -f "$ROOT_DIR/docs/installation.md" ]] || fail_complete "installation guide is missing"
for documented in \
	'helm template' \
	'helm lint' \
	'helm package' \
	'helm upgrade --install' \
	'helm rollback' \
	'helm uninstall' \
	'certManager' \
	'externalSecret' \
	'--reuse-values' \
	'make package-crd-check' \
	'make package-apply-crds' \
	'purge-kubeseer-crds' \
	'--confirm-server' \
	'kubeseer-validating-webhook' \
	'KUBESEER_PACKAGE_KUBECONFIG_1_35_6' \
	'KUBESEER_PACKAGE_KUBECONFIG_1_36_2'; do
	rg -F -q -- "$documented" "$ROOT_DIR/docs/installation.md" || fail_complete "installation guide lacks ${documented}"
done

GOCACHE="$temp_dir/go-build" GOMODCACHE="$build_module_cache" GOSUMDB=off GOPROXY="$local_go_proxy" \
	go build -trimpath -ldflags='-s -w -X main.buildVersion=verify -X main.buildCommit=verify -X main.buildDate=verify' \
		-o "$temp_dir/kubeseer" ./cmd/kubeseer || fail_complete "manager executable build failed"
GOCACHE="$temp_dir/go-build" GOMODCACHE="$build_module_cache" GOSUMDB=off GOPROXY="$local_go_proxy" \
	go build -trimpath -ldflags='-s -w -X main.buildVersion=verify -X main.buildCommit=verify -X main.buildDate=verify' \
		-o "$temp_dir/kubeseer-purge" ./cmd/kubeseer-purge || fail_complete "purge executable build failed"
"$temp_dir/kubeseer" version | rg -q '^kubeseer version=verify commit=verify date=verify$' || fail_complete "manager version identity is not executable"
"$temp_dir/kubeseer-purge" --version | rg -q '^kubeseer-purge version=verify commit=verify date=verify$' || fail_complete "purge version identity is not executable"

printf 'PACKAGE_VERIFY=complete STATUS=passed\n'
