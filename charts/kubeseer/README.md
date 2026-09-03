# Kubeseer Helm chart

This is the canonical Helm v2 application chart rooted at
`charts/kubeseer`. It is deterministic, contains no dependency chart, and
never uses Helm `lookup`. The Kubernetes compatibility gate is
`>=1.35.0-0 <1.37.0-0`; the verified matrix is Kubernetes `1.35.6` and
`1.36.2`.

Use the project [README](../../README.md) as the documentation entry point,
[Configuration](../../docs/configuration.md) for deployment decisions, and
[Installation](../../docs/installation.md) for lifecycle procedures.

## Prerequisites

- Helm 3.12 or newer.
- Kubernetes 1.35.6 or 1.36.2.
- A reachable, versioned Kubeseer image; `latest` is rejected.
- cert-manager `v1.18.2` exposing `cert-manager.io/v1` for the default
  `certificate.mode: certManager`, or an administrator-owned TLS Secret and
  public PEM CA for `certificate.mode: externalSecret`.

## Commands

```sh
helm lint charts/kubeseer
helm template kubeseer charts/kubeseer \
  --namespace kubeseer-system --kube-version 1.35.6 --include-crds
helm package charts/kubeseer --destination dist
make verify-package
make test-package-compatibility
```

Install, upgrade, and uninstall procedures are in
[`docs/installation.md`](../../docs/installation.md). The exact lifecycle is:

1. weighted read-only preflight;
2. managed policy reconciliation, when enabled;
3. normal release resources;
4. post-install/upgrade/rollback rollout, webhook, and trust verification;
5. pre-delete removal of only the owned validating webhook;
6. normal Helm deletion of runtime, RBAC, and cert-manager resources;
7. post-delete retained-state reporting.

## Public values and defaults

Every supported value is listed below with its default. Unknown keys are
rejected by `values.schema.json`; omitted values use these defaults.

| Value | Default | Meaning |
| --- | --- | --- |
| `image.repository` | `ghcr.io/steeltanuki/kubeseer` | OCI image repository |
| `image.tag` | `""` (Chart `appVersion`) | immutable image tag |
| `image.pullPolicy` | `IfNotPresent` | image pull policy |
| `replicaCount` | `1` | manager replicas |
| `resources.requests.cpu` | `10m` | CPU request |
| `resources.requests.memory` | `32Mi` | memory request |
| `resources.limits.cpu` | `500m` | CPU limit |
| `resources.limits.memory` | `256Mi` | memory limit |
| `manager.leaderElection` | `true` | leader election |
| `manager.terminationGracePeriodSeconds` | `30` | Pod termination grace |
| `manager.metricsBindAddress` | `:8080` | manager metrics bind |
| `manager.healthProbeBindAddress` | `:8081` | health/readiness bind |
| `manager.webhookPort` | `9443` | serving container port |
| `manager.webhookCertDir` | `/var/run/secrets/kubeseer/webhook` | serving mount |
| `manager.webhookCertName` | `tls.crt` | serving certificate file |
| `manager.webhookKeyName` | `tls.key` | serving key file |
| `manager.webhookCAFile` | `/var/run/secrets/kubeseer/ca/ca.crt` | CA bundle file |
| `manager.safetyInterval` | `5m` | reconciliation safety interval |
| `manager.gracefulShutdownTimeout` | `30s` | manager shutdown bound |
| `metrics.service.enabled` | `true` | render metrics Service |
| `metrics.service.port` | `8080` | metrics Service port |
| `tracing.enabled` | `false` | enable optional OTLP setting |
| `tracing.endpoint` | `""` | OTLP endpoint when enabled |
| `certificate.mode` | `certManager` | certificate provider |
| `certificate.certManager.issuer.group` | `cert-manager.io` | issuer API group |
| `certificate.certManager.issuer.kind` | `Issuer` | issuer kind |
| `certificate.certManager.issuer.name` | `""` | generated issuer when empty |
| `certificate.certManager.certificate.duration` | `2160h` | serving lifetime |
| `certificate.certManager.certificate.renewBefore` | `360h` | renewal window |
| `certificate.externalSecret.secretName` | `""` | required external TLS Secret |
| `certificate.externalSecret.caBundle` | `""` | required public CA PEM |
| `accessPolicy.mode` | `managed` | managed or external policy |
| `accessPolicy.explicitNamespaces` | `[]` | logical namespace allowlist |
| `accessPolicy.allowedResources` | `[]` | logical API group/kind allowlist |
| `accessPolicy.allowClusterScoped` | `false` | logical cluster-scope opt-in |
| `accessPolicy.systemNamespaces` | omitted | optional system namespace override |
| `rbac.observed.namespaced` | `[]` | explicit namespaced read rules |
| `rbac.observed.clusterScoped` | `[]` | explicit cluster read rules |
| `rbac.authors.namespaces` | `[]` | author Role namespaces |
| `rbac.authors.subjects` | `[]` | author Role subjects |

The `limits` tree defaults are:

| Value | Default |
| --- | ---: |
| `limits.pageSize` | `500` |
| `limits.maxMatchedResources` | `1000` |
| `limits.maxSelectedInputBytes` | `8388608` |
| `limits.maxProducedValueBytes` | `8388608` |
| `limits.maxStatusBytes` | `524288` |
| `limits.evaluationTimeout` | `30s` |
| `limits.maxConcurrentReconciles` | `4` |
| `limits.discoveryCacheEntries` | `1024` |
| `limits.discoveryCacheTTL` | `5m` |
| `limits.maxActiveWatches` | `1024` |
| `limits.maxPendingTriggers` | `4096` |
| `limits.admission.maxKubeseerSources` | `32` |
| `limits.admission.maxSourceNamespaces` | `64` |
| `limits.admission.maxSourceFields` | `64` |
| `limits.admission.maxFieldOperators` | `16` |
| `limits.admission.maxOperatorValues` | `128` |
| `limits.admission.maxSourceAggregations` | `32` |
| `limits.admission.maxAggregationGroupBy` | `16` |
| `limits.admission.maxSelectorMatchExpressions` | `64` |
| `limits.admission.maxSelectorExpressionValues` | `64` |
| `limits.admission.maxSelectorMatchLabels` | `64` |
| `limits.admission.maxPolicyNamespaceNames` | `256` |
| `limits.admission.maxPolicyResourceRules` | `128` |
| `limits.admission.maxPolicyAPIGroups` | `64` |
| `limits.admission.maxPolicyKinds` | `64` |
| `limits.admission.maxKubeseerSpecBytes` | `262144` |
| `limits.admission.maxAccessPolicySpecBytes` | `262144` |
| `limits.aggregation.maxGroups` | `1000` |
| `limits.aggregation.maxContributions` | `10000` |
| `limits.aggregation.maxCollectedValues` | `10000` |
| `limits.aggregation.maxDistinctValues` | `10000` |
| `limits.aggregation.maxProvenanceEntries` | `10000` |

## Certificate and retention rules

`certManager` renders a self-signed CA chain, serving Certificate, and
CA-injection annotation. `externalSecret` renders only the public CA ConfigMap
and a read-only mount of the named external Secret; it renders no cert-manager
resource and no TLS Secret. The Secret must contain `tls.crt` and `tls.key` and
the serving certificate must contain these exact names:

```text
kubeseer-webhook
kubeseer-webhook.<namespace>
kubeseer-webhook.<namespace>.svc
kubeseer-webhook.<namespace>.svc.cluster.local
```

For a CA rollover, release old and new CA together, rotate the serving Secret,
verify trust, and remove the old CA only in a later release. The manager stays
unready for missing, invalid, expired, mismatched, or untrusted material.

The package-owned policy, Deployment, Services, RBAC, webhook registration, and
cert-manager objects are removed by normal uninstall. Both CRDs, all
Kubeseer instances, `installation-access-ceiling`, and all external TLS
Secrets are retained. Helm does not upgrade CRDs under `crds/`; run
`make package-sync-crds`, `make package-crd-check`, and
`make package-apply-crds` for an explicit compatible CRD change.
