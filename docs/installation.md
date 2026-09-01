# Kubeseer installation and lifecycle guide

This guide covers the canonical Helm package in `charts/kubeseer`. Kubeseer
supports Kubernetes `1.35.6` and `1.36.2`, Helm 3.12 or newer, and the
`kubeseer.io/v1alpha1` API. The chart declares the Kubernetes gate
`>=1.35.0-0 <1.37.0-0`; a cluster outside that range is rejected before
resources are rendered or installed.

## Prerequisites

The administrator needs:

- an explicitly selected Kubernetes context and permission to install CRDs,
  cluster-scoped RBAC, admission webhooks, and cert-manager resources;
- Helm 3.12 or newer and an image registry reachable by the cluster;
- a versioned, non-`latest` Kubeseer image;
- cert-manager exposing `cert-manager.io/v1` (the package compatibility profile
  is pinned to cert-manager `v1.18.2`) for the default certificate mode, or an
  externally managed TLS Secret and public PEM CA for `externalSecret` mode.

The package never uses an ambient cloud credential or ambient kubeconfig in a
compatibility run. Cluster commands below show the selected context explicitly.

## Render, lint, and package

Run these checks before installation:

```sh
helm lint charts/kubeseer
helm template kubeseer charts/kubeseer \
  --namespace kubeseer-system --kube-version 1.35.6 --include-crds
helm package charts/kubeseer --destination dist
make verify-package
```

`image.tag: ""` resolves to `Chart.appVersion` (`0.1.0` in this package).
Every explicit image tag must be immutable and `latest` is rejected. `make
verify-package` also regenerates the CRDs with the pinned controller-tools
version, checks the image and security contract, and renders both certificate
modes.

## Install

Choose the namespace with Helm; do not edit the chart's canonical files. The
default chart mode is `certManager` and uses cert-manager:

```sh
helm upgrade --install kubeseer charts/kubeseer \
  --namespace kubeseer-system --create-namespace \
  --set image.repository=ghcr.io/steeltanuki/kubeseer \
  --set image.tag=0.1.0 \
  --wait --timeout 10m
```

The lifecycle is ordered by hook weight: read-only preflight, policy bootstrap,
normal release resources, then bounded post-install readiness verification.
Preflight checks the cert-manager API, both Established CRDs, singleton
ownership, and the storage-version contract without writing cluster state.

Check availability and the installed version:

```sh
kubectl --context my-cluster -n kubeseer-system \
  wait --for=condition=Available deployment/kubeseer --timeout=5m
kubectl --context my-cluster -n kubeseer-system \
  get deployment kubeseer -o jsonpath='{.spec.template.spec.containers[?(@.name=="manager")].env[?(@.name=="KUBESEER_VERSION")].value}{"\n"}'
```

The default access policy is a deny-all `Explicit` policy named
`installation-access-ceiling`. Policy scope and Kubernetes RBAC are separate:
changing `accessPolicy.*` does not grant a ServiceAccount permission, and
granting observed-resource RBAC does not broaden logical authorization.

## Certificate modes

### Default: cert-manager

Install the approved cert-manager release before installing Kubeseer. The chart
creates a namespaced self-signed bootstrap Issuer, CA Certificate, CA Issuer,
serving Certificate, and CA-injection annotation. The serving Secret is owned
by the chart's cert-manager Certificate resource, is mounted read-only, and
contains `tls.crt`, `tls.key`, and `ca.crt`.

The webhook certificate must cover these exact Service names in the selected
namespace:

```text
kubeseer-webhook
kubeseer-webhook.<namespace>
kubeseer-webhook.<namespace>.svc
kubeseer-webhook.<namespace>.svc.cluster.local
```

The manager keeps readiness false until the current key matches the serving
certificate, the certificate is valid in time, every exact SAN is present, and
the chain verifies against the CA. Controller-runtime watches the mounted
certificate files and reloads a valid same-CA key pair without a Deployment
edit.

### Fallback: externalSecret

Use this mode only when cert-manager cannot be installed or another PKI owns
issuance. The named Secret must already exist in the release namespace and must
contain exactly the administrator-managed serving material under `tls.crt` and
`tls.key`. Do not put a private key in Helm values, source, or a ConfigMap.

Provide only the Secret name and public CA PEM. The chart renders a CA ConfigMap,
mounts the named Secret read-only, and renders no cert-manager object and no
TLS Secret:

```sh
helm upgrade --install kubeseer charts/kubeseer \
  --namespace kubeseer-system --create-namespace \
  --set image.repository=ghcr.io/steeltanuki/kubeseer \
  --set image.tag=0.1.0 \
  --set certificate.mode=externalSecret \
  --set certificate.externalSecret.secretName=administrator-webhook-tls \
  --set-file certificate.externalSecret.caBundle=public-ca.pem \
  --wait --timeout 10m
```

The administrator is responsible for issuance, renewal, the four exact SANs,
and preserving the Secret across upgrades and uninstall. A CA rollover is
ordered: first release a bundle containing old and new CA, then rotate the
serving Secret, verify trust and reachability, and finally release a bundle
containing only the new CA. The new serving certificate must not be activated
before its CA is present in the released bundle.

## Policy, RBAC, and upgrades

Policy values and RBAC values are independent. A namespaced observed read is an
explicit `rbac.observed.namespaced` entry. Cluster-scoped observed reads require
both that entry and `accessPolicy.allowClusterScoped=true`. Author subjects get
only namespaced Kubeseer permissions; they receive no observed-resource,
Secret, policy, webhook, Deployment, or RBAC permission from this chart.

Before a CRD-bearing upgrade, run the explicit compatibility gate and apply:

```sh
export KUBECONFIG=/absolute/path/to/kubeconfig
export KUBE_CONTEXT=my-cluster
make package-crd-check
make package-apply-crds
helm upgrade kubeseer charts/kubeseer \
  --namespace kubeseer-system --reuse-values \
  --wait --timeout 10m
```

`--reuse-values` preserves effective policy and certificate inputs when no
explicit change is intended. An explicit policy or certificate change is a
normal Helm upgrade and passes through the active validating webhook. A
compatible image change is a rolling update with `maxUnavailable: 0` and
`maxSurge: 1`; a failed rollout stays visible in Deployment conditions and is
not destructively auto-rolled back. Helm release history records every chart
revision.

Rollback is supported only after the same CRD compatibility gate accepts the
target revision:

```sh
helm history kubeseer --namespace kubeseer-system
make package-crd-check
helm rollback kubeseer REVISION --namespace kubeseer-system \
  --wait --timeout 10m
```

Never use rollback to cross a storage-version or scope boundary. CRDs and
custom-resource instances remain in place during compatible rollback.

## Safe uninstall and explicit purge

Normal uninstall is reversible. The pre-delete hook removes only the owned
`kubeseer-validating-webhook` registration before the manager endpoint is
removed. Helm then deletes the release-owned Deployment, Services, RBAC, and
cert-manager resources. The post-delete hook reports retained state. Repeat it
with the same command if the release is already absent:

```sh
./hack/uninstall-kubeseer.sh kubeseer kubeseer-system
# Equivalent Helm operation (the wrapper also reports retained state):
helm uninstall kubeseer --namespace kubeseer-system --ignore-not-found --wait
```

Normal uninstall preserves both CRDs, every `Kubeseer` instance,
`installation-access-ceiling`, and every externally managed TLS Secret. It does
not delete unrelated namespaces, workloads, RBAC, webhooks, Secrets, or CRDs.

CRD deletion is a separate destructive operation. Build the separately
versioned client and confirm the exact resolved cluster target twice:

```sh
GOCACHE=/tmp/kubeseer-packaging-go-build \
GOMODCACHE=/tmp/kubeseer-packaging-go-mod \
  make build-purge
./bin/kubeseer-purge \
  --kubeconfig /absolute/path/to/kubeconfig \
  --context my-cluster \
  --confirm-context my-cluster \
  --confirm-server https://api.example.invalid:6443 \
  purge-kubeseer-crds
```

The final token must be exactly `purge-kubeseer-crds`. The kubeconfig path must
be absolute, the context must exist in that file, and `--confirm-server` must
match its resolved API server. Any missing, mismatched, or incorrect
confirmation performs no write. A confirmed purge lists and deletes only the
`kubeseers.kubeseer.io` and `kubeseeraccesspolicies.kubeseer.io` collections,
waits for them to become empty, and then deletes only those two CRDs. Finalizer
blockage stops before CRD deletion. The client never reads or deletes Secrets
and prints only sanitized target outcomes.

## Verification matrix

The offline matrix is always runnable and renders both supported Kubernetes
versions in both certificate modes:

```sh
make test-package-compatibility
```

For a live smoke run, provide an explicit kubeconfig and context for every
matrix entry; ambient `KUBECONFIG` is ignored:

```sh
KUBESEER_PACKAGE_RUN_CLUSTER=1 \
KUBESEER_PACKAGE_KUBECONFIG_1_35_6=/absolute/kubeconfig-135 \
KUBESEER_PACKAGE_CONTEXT_1_35_6=my-135-cluster \
KUBESEER_PACKAGE_KUBECONFIG_1_36_2=/absolute/kubeconfig-136 \
KUBESEER_PACKAGE_CONTEXT_1_36_2=my-136-cluster \
KUBESEER_PACKAGE_IMAGE_REPOSITORY=ghcr.io/steeltanuki/kubeseer \
KUBESEER_PACKAGE_IMAGE_TAG=0.1.0 \
  make test-package-compatibility
```

The live profile requires cert-manager for the default mode and a pre-created
administrator-owned TLS Secret plus `KUBESEER_PACKAGE_EXTERNAL_CA_BUNDLE` for
the fallback mode. It installs, waits, verifies, uninstalls, checks retained
state, exercises the no-token purge path, and reports a non-zero result for any
missing or zero-match scenario.
