# Kubeseer installation and lifecycle guide

This guide covers both the canonical chart in `charts/kubeseer` and its
official Helm OCI distribution. Kubeseer supports Kubernetes `1.35.6` and
`1.36.2`, Helm 3.12 or newer, and the `kubeseer.io/v1alpha1` API. The chart
declares the Kubernetes gate `>=1.35.0-0 <1.37.0-0`; a cluster outside that
range is rejected before resources are rendered or installed. The official
controller image release contract currently certifies `linux/amd64` only.

For policy, RBAC, certificate, limit, and process-setting decisions, read
[Configuration](configuration.md). For runtime checks after installation, see
[Operations](operations.md); for trust boundaries and data exposure, see
[Security model](security.md). The [README](../README.md) is the documentation
entry point.

## Prerequisites

The administrator needs:

- an explicitly selected Kubernetes context and permission to install CRDs,
  cluster-scoped RBAC, admission webhooks, and cert-manager resources;
- Helm 3.12 or newer and access from the cluster to the public GHCR controller
  image (or to the registry hosting a deliberately overridden image);
- a versioned, non-`latest` Kubeseer image;
- cert-manager exposing `cert-manager.io/v1` (the package compatibility profile
  is pinned to cert-manager `v1.18.2`) for the default certificate mode, or an
  externally managed TLS Secret and public PEM CA for `externalSecret` mode.

The package never uses an ambient cloud credential or ambient kubeconfig in a
compatibility run. Cluster commands below show the selected context explicitly.

## Installing an official release

Official releases use the public Helm OCI artifact and the matching public
controller image. They can be installed without cloning the source repository
or building either artifact. The GitHub Container Registry packages must be
public so Helm and Kubernetes can pull them anonymously. A registry login is
not needed for these public packages.

Choose a stable version shown on the
[GitHub Releases page](https://github.com/steeltanuki/kubeseer/releases). For
example, this installs `0.1.6` (source tag `v0.1.6`):

```sh
helm upgrade --install kubeseer oci://ghcr.io/steeltanuki/charts/kubeseer \
  --version 0.1.6 \
  --namespace kubeseer-system --create-namespace \
  --wait --timeout 10m
```

`Chart.yaml` `version` and `appVersion` match the selected release. The
default image repository is `ghcr.io/steeltanuki/kubeseer`; its empty chart
`image.tag` resolves to `appVersion`, so this command installs
`ghcr.io/steeltanuki/kubeseer:0.1.6`. The `.tgz` chart archive is a temporary
release-verification input and is not attached to the GitHub Release; the OCI
chart is the canonical distribution artifact.

To inspect or render an official chart without installing it:

```sh
helm show chart oci://ghcr.io/steeltanuki/charts/kubeseer --version 0.1.6
helm template kubeseer oci://ghcr.io/steeltanuki/charts/kubeseer \
  --version 0.1.6 --namespace kubeseer-system \
  --kube-version 1.35.6 --include-crds
```

Before upgrading, review the release notes for compatibility and CRD changes.
The [policy, RBAC, and upgrade guidance](#policy-rbac-and-upgrades) describes
the required CRD gate and retained-state behavior.

## Working from a source checkout: render, lint, and package

The following commands are for contributors validating or packaging the
source-controlled canonical chart. Users installing an official release do
not need to clone the repository or run these commands. A local package does
not become an official release artifact.

Run these checks before installation:

```sh
helm lint charts/kubeseer
helm template kubeseer charts/kubeseer \
  --namespace kubeseer-system --kube-version 1.35.6 --include-crds
helm package charts/kubeseer --destination dist
make verify-package
```

`image.tag: ""` resolves to `Chart.appVersion` (`0.1.6` in this package).
Every explicit image tag must be immutable and `latest` is rejected. `make
verify-package` also regenerates the CRDs with the pinned controller-tools
version, checks the image and security contract, and renders both certificate
modes.

## Install a locally packaged source chart

This path is for contributor testing against a locally modified chart. For a
released version, use the OCI command above.

Choose the namespace with Helm; do not edit the chart's canonical files. The
default chart mode is `certManager` and uses cert-manager:

```sh
helm upgrade --install kubeseer charts/kubeseer \
  --namespace kubeseer-system --create-namespace \
  --set image.repository=ghcr.io/steeltanuki/kubeseer \
  --set image.tag=0.1.6 \
  --wait --timeout 10m
```

This source-chart example uses the public versioned controller image by
default. Contributors testing a locally built controller should follow the
local environment procedure below so the temporary image is loaded into kind.

For the contributor-owned local kind/Podman workflow, use the
[local development guide](local-development.md). It builds and loads a
temporary local image and does not publish an image or chart.

## Release publication and recovery

`develop` is the integration branch. Protected `main` is the latest promoted
release-source branch: it holds the newest release source selected by the
maintainer, rather than a floating `stable` tag or artifact alias. Contributor
pull requests continue to target `develop`; a release reaches `main` through
the maintainer's reviewed promotion.

Official releases use stable Semantic Versioning tags of the form
`vMAJOR.MINOR.PATCH`, such as `v0.1.6`. Prerelease tags are not supported by
the initial release-distribution workflow. For a release, the maintainer
prepares the chart's `version` and `appVersion` in
`charts/kubeseer/Chart.yaml` and substantive notes at
`docs/releases/v<version>.md`, including `Highlights` and
`Upgrade considerations`, then promotes that commit from `develop` to
protected `main` in a reviewed pull request. Before creating the tag, confirm
that the required CI checks for the promotion have passed and that the GitHub
rulesets for the `main` branch and `v*` tags are active. Check out the exact
reviewed promotion commit from `origin/main`, verify its SHA, then create and
push the annotated version tag. For example:

```sh
TAG=v0.1.6
git fetch origin main
git switch --detach origin/main
git rev-parse HEAD  # Confirm this is the reviewed promotion commit.
git tag -a "$TAG" -m "Kubeseer $TAG"
git push origin "$TAG"
```

The tagged commit must be reachable from fetched `origin/main`; the validator
also checks the exact tag SHA, chart versions, and release notes. An earlier
version tag remains valid after `main` advances. The workflow does not rewrite
source files or create tags. Branch pushes, including pushes to `develop` or
`main`, and floating `stable` or `latest` Git tags or image/chart aliases do
not publish artifacts; publication is triggered only by a versioned tag.

The tagged source currently certifies controller images for `linux/amd64`,
Kubernetes `1.35.6` and `1.36.2`, and Helm 3.12 or newer. The production image
contract is amd64-only; an arm64 image or multi-platform manifest is not
claimed. See the exact version and upgrade notes on each GitHub Release.

If a release workflow fails after some registry writes, first inspect the
failed job and ensure the GHCR image and chart packages allow anonymous pulls.
GHCR creates each new package as private. The first image or chart push can
therefore succeed while its immediate anonymous pull check fails. In the
maintainer's GitHub Packages settings, change the visibility of
`steeltanuki/kubeseer` and `steeltanuki/charts/kubeseer` to public after each
package first appears, then rerun the failed workflow for the same protected
tag. A new GitHub Release is created only after both packages pass anonymous
pull verification. An HTTP 403 from the anonymous registry token endpoint
does not by itself prove that a package already exists; the publisher uses its
package credentials to inventory missing versions before writing.

For read-only inventory from a clean checkout of the exact tag, a maintainer
can compare the public artifacts and their recorded digests:

```sh
TAG=v0.1.6
git checkout --detach "$TAG"
SOURCE_SHA="$(git rev-parse "refs/tags/${TAG}^{commit}")"
GITHUB_ACTOR=steeltanuki GH_TOKEN="$PACKAGE_TOKEN" \
  ./hack/release-distribution.sh inventory --tag "$TAG" --source-sha "$SOURCE_SHA"
GITHUB_ACTOR=steeltanuki GH_TOKEN="$PACKAGE_TOKEN" \
  ./hack/release-distribution.sh audit --tag "$TAG" --source-sha "$SOURCE_SHA"
```

`inventory` shows which image, chart, and GitHub Release references are
present; `audit` succeeds only when all public artifacts match the tagged
version, source commit, and recorded digests. Set `PACKAGE_TOKEN` to a classic
personal access token with `write:packages` and access to the maintainer's
packages. GHCR uses that permission to distinguish a missing package from one
the maintainer cannot access; these commands perform no writes.
After checking a missing-only partial release and confirming package
visibility, a maintainer may explicitly
rerun the failed workflow for the same protected tag. The workflow reuses
matching immutable artifacts and publishes only missing ones. If any digest,
revision, or release metadata conflicts, stop and investigate; do not move the
tag or overwrite the published version. A GitHub Release is created only
after both OCI artifacts pass public verification.

The complete maintainer-authored release notes are included in the GitHub
Release. The chart `.tgz` is not attached as a second download format.

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
helm upgrade --install kubeseer oci://ghcr.io/steeltanuki/charts/kubeseer \
  --version 0.1.6 \
  --namespace kubeseer-system --create-namespace \
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
helm upgrade kubeseer oci://ghcr.io/steeltanuki/charts/kubeseer \
  --version 0.1.6 \
  --namespace kubeseer-system --reuse-values \
  --wait --timeout 10m
```

Use the target release's version in `--version`. If you are testing a modified
source chart, replace the OCI reference with `charts/kubeseer` and follow the
source-checkout verification steps above.

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
KUBESEER_PACKAGE_IMAGE_TAG=0.1.6 \
  make test-package-compatibility
```

The live profile requires cert-manager for the default mode and a pre-created
administrator-owned TLS Secret plus `KUBESEER_PACKAGE_EXTERNAL_CA_BUNDLE` for
the fallback mode. It installs, waits, verifies, uninstalls, checks retained
state, exercises the no-token purge path, and reports a non-zero result for any
missing or zero-match scenario.
