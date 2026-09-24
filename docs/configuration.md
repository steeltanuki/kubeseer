# Configuration

Kubeseer is configured through the Helm chart in `charts/kubeseer`. The chart
has a finite values schema: unknown keys, wildcards in observed-resource RBAC,
mutable `latest` images, and invalid combinations are rejected during render.
See the [Helm values reference](../charts/kubeseer/README.md) for every value
and default; this guide focuses on decisions and safe combinations.

## Start with an explicit values file

Keep environment-specific settings outside the canonical chart:

```yaml
image:
  repository: ghcr.io/steeltanuki/kubeseer
  tag: 0.1.4

accessPolicy:
  mode: managed
  explicitNamespaces: [applications]
  allowedResources:
    - apiGroups: [apps]
      kinds: [Deployment, StatefulSet]
  allowClusterScoped: false

rbac:
  observed:
    namespaced:
      - namespace: applications
        apiGroups: [apps]
        resources: [deployments, statefulsets]
        verbs: [get, list, watch]
    clusterScoped: []
  authors:
    namespaces: [applications]
    subjects:
      - kind: Group
        name: platform-observers
```

Install it with an explicit namespace and immutable image tag:

```sh
helm upgrade --install kubeseer charts/kubeseer \
  --namespace kubeseer-system --create-namespace \
  --values kubeseer-values.yaml \
  --wait --timeout 10m
```

## Policy and RBAC are independent

Two gates must allow an observed read:

1. `accessPolicy` defines the logical ceiling Kubeseer authors cannot exceed;
2. `rbac.observed` grants the manager ServiceAccount Kubernetes API verbs.

Making either side broader does not broaden the other. A policy-allowed target
without RBAC reports `ReadForbidden`; RBAC alone never bypasses logical
authorization. Keep both sets exact and review them together.

### Managed policy

`accessPolicy.mode: managed` is the normal mode. Helm creates or reconciles the
singleton `installation-access-ceiling`. The default is deny-all:

```yaml
accessPolicy:
  mode: managed
  explicitNamespaces: []
  allowedResources: []
  allowClusterScoped: false
```

The managed chart interface uses `Explicit` namespace policy semantics. Add
only the namespaces and exact API group/Kind pairs the installation is meant
to expose. The core API group is `""`.

`systemNamespaces`, when omitted, preserves the policy API's default system
set. If supplied, it replaces that set exactly. It matters when an externally
managed policy uses `AllNonSystem` and should be changed only deliberately.

### External policy

Set `accessPolicy.mode: external` when a separate administrator owns the
singleton lifecycle. The object must exist, be named exactly
`installation-access-ceiling`, and remain valid. A missing or invalid policy
causes fail-closed runtime outcomes.

The full policy API, including `Explicit`, `All`, `AllNonSystem`, exclusions,
and cluster-scoped rules, is in the [API reference](api-reference.md).

### Observed-resource RBAC

Namespaced entries create exact Roles in their declared namespaces. Use
resource plural names in `resources`, not Kinds:

```yaml
rbac:
  observed:
    namespaced:
      - namespace: applications
        apiGroups: [""]
        resources: [pods]
        verbs: [get, list, watch]
    clusterScoped: []
```

Cluster-scoped entries create cluster-level read permissions and require
`accessPolicy.allowClusterScoped: true`. Only `get`, `list`, and `watch` are
accepted. Wildcards and Secret resources are rejected by the chart schema.

### Author RBAC

`rbac.authors` grants the named subjects namespaced permissions for Kubeseer
objects in the listed namespaces. It grants no access to observed resources,
Secrets, the cluster policy, webhooks, Deployments, or RBAC objects.

ServiceAccount subjects require their namespace:

```yaml
rbac:
  authors:
    namespaces: [applications]
    subjects:
      - kind: ServiceAccount
        name: view-author
        namespace: applications
```

## Certificates

The validating webhook and manager readiness depend on valid serving TLS.

### cert-manager mode

The default `certificate.mode: certManager` requires the approved cert-manager
API. With no external issuer name, the chart creates a private self-signed CA,
Issuer chain, and serving Certificate:

```yaml
certificate:
  mode: certManager
  certManager:
    issuer:
      group: cert-manager.io
      kind: Issuer
      name: ""
    certificate:
      duration: 2160h
      renewBefore: 360h
```

To use an existing compatible issuer, set its exact group, kind, and name.

### External Secret mode

Use `externalSecret` when another PKI owns issuance:

```yaml
certificate:
  mode: externalSecret
  externalSecret:
    secretName: administrator-webhook-tls
    caBundle: |
      -----BEGIN CERTIFICATE-----
      ... public CA only ...
      -----END CERTIFICATE-----
```

The Secret must already exist in the release namespace with `tls.crt` and
`tls.key`. Never put the private key in values. The certificate must cover all
four exact webhook Service DNS names; CA rollover ordering and validation are
documented in [Installation](installation.md#fallback-externalsecret).

## Runtime limits

Defaults bound resource count, input and output bytes, status size, execution
time, concurrency, discovery cache, watches, pending triggers, admission
complexity, and aggregate cardinality. Important defaults include:

| Limit | Default |
| --- | ---: |
| Selected resources | 1,000 |
| Selected input bytes | 8 MiB |
| Produced value bytes | 8 MiB |
| Status bytes | 512 KiB |
| Evaluation timeout | 30 seconds |
| Concurrent reconciles | 4 |
| Active watches | 1,024 |
| Aggregate groups | 1,000 |
| Aggregate contributions | 10,000 |

Tune limits only after measuring actual workloads. Larger values increase API
traffic, memory use, status write size, and the amount of data exposed through
the result. A reached result limit is reported as `ResultLimitExceeded`; the
controller does not silently truncate.

Admission limits should normally match or be tighter than the public CRD
budgets. The chart's complete `limits` tree is in the
[values reference](../charts/kubeseer/README.md#public-values-and-defaults).

## Process and service settings

Common manager settings are:

| Setting | Default | Notes |
| --- | --- | --- |
| `replicaCount` | 1 | Leader election remains enabled for replicas above one |
| `manager.safetyInterval` | `5m` | Bounded periodic reevaluation safety net |
| `manager.metricsBindAddress` | `:8080` | Prometheus endpoint bind |
| `manager.healthProbeBindAddress` | `:8081` | `/healthz` and `/readyz` |
| `manager.webhookPort` | `9443` | Admission webhook serving port |
| `metrics.service.enabled` | `true` | Exposes metrics through a Service |
| `tracing.enabled` | `false` | Enables the optional OTLP configuration |

The default resource request is intentionally small (`10m`, `32Mi`) and the
limit is bounded (`500m`, `256Mi`). Size these together with concurrency and
evaluation limits.

## Configuration validation

Validate before changing a cluster:

```sh
helm lint charts/kubeseer --values kubeseer-values.yaml
helm template kubeseer charts/kubeseer \
  --namespace kubeseer-system \
  --kube-version 1.35.6 \
  --include-crds \
  --values kubeseer-values.yaml
make verify-package
```

Use `helm diff` if it is part of your administrative toolchain. For upgrades,
run the explicit CRD compatibility gate before Helm; see
[Installation and lifecycle](installation.md#policy-rbac-and-upgrades).
