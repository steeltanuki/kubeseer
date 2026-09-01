---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-01T06:44:34Z
last_modified: 2026-09-01T06:44:34Z
approved_fingerprint: sha256:1164721abe8248bdb9b119aa7279bfbdb3d05d0e2166c3a55c2fc437dc28f7b0
source_requirements_approved_at: 2026-09-01T06:12:03Z
source_requirements_fingerprint: sha256:2c70a2672efedbd212d151c0b2dbaf1efaa069e0598ae5f216504e4962c98084
---

# Packaging And Installation Design

## Overview

Kubeseer is delivered as one OCI manager image and one Helm v2 application
chart under `charts/kubeseer`. The chart owns deployment topology, runtime RBAC,
webhook registration, certificate-lifecycle resources, and deterministic values
translation. Generated CRDs remain the repository source of truth and are copied
unchanged into `charts/kubeseer/crds`.

The manager executable is also the in-cluster lifecycle helper used by bounded
Helm hook Jobs. It exposes explicit `manager`, `version`, and `package` commands;
the production container starts `manager` directly and never invokes a shell.
A separate client-side `kubeseer-purge` executable owns irreversible custom
resource and CRD deletion and is never embedded in the chart or runtime image.

Certificate mode is selected only from values. `certManager` is the default and
creates namespaced cert-manager issuers and certificates with CA injection.
`externalSecret` renders no cert-manager custom resources, references a
pre-existing TLS Secret, and injects the administrator-supplied PEM CA bundle.
Both modes use controller-runtime's certificate watcher so replacement key pairs
become active without a Deployment edit.

<!-- decided: Helm is the sole initial package and certManager is the default with externalSecret as the explicit fallback (source: approved Requirements) -->
<!-- assumed: live cluster checks run in explicit lifecycle commands and Helm hooks, never through template lookup, so offline rendering remains deterministic -->
<!-- assumed: the manager image contains one statically linked Kubeseer executable; destructive purge remains a separate client-side executable -->
<!-- assumed: package-owned names are derived from one bounded fullname helper except the approved cluster singleton names -->

## Architecture

```text
committed Go + generated CRDs
        |                         values.yaml + values.schema.json
        v                                      |
cmd/kubeseer -> static manager image           v
        |                              charts/kubeseer
        |                                      |
        +--> manager Deployment <--------------+
        +--> lifecycle hook Jobs <--------------+
                                               |
                       +-----------------------+-------------------+
                       |                       |                   |
                    CRDs/                 runtime objects     certificate mode
                       |                       |              /              \
              API establishment       Deployment/RBAC/   cert-manager    external Secret
                       |               Services/webhook      CRs          reference + CA
                       +-----------------------+-------------------+
                                               |
                                  post-install readiness proof

cmd/kubeseer-purge --kubeconfig --context --confirm <token>
        -> validate exact target -> delete instances -> delete CRDs -> report
```

Helm processes CRDs before pre-install hooks. Hook weights then create temporary
least-privilege hook RBAC, run cluster preflight, and reconcile the optional
chart-managed deny-all policy. Normal resources are applied only after those
checks pass. A post-install or post-upgrade hook proves Deployment availability,
TLS trust, webhook reachability, CRD establishment, and required ServiceAccount
permissions. Failed hook Jobs are retained for diagnostics; successful hook
resources are removed.

The supported command order is:

1. render, lint, and run values/static verification offline;
2. apply compatible CRD updates explicitly when an upgrade contains them;
3. run `helm upgrade --install --wait --wait-for-jobs` with explicit inputs;
4. let package hooks prove live prerequisites and installed readiness;
5. use the documented pre-delete-aware Helm uninstall procedure for reversible
   removal, or invoke `kubeseer-purge` separately for destructive cleanup.

## Options Considered

### Option A: One Helm Chart, Lifecycle Hooks, And External Purge Tool

- Summary: use pure Helm templates for desired state, manager-image hook Jobs
  for live checks and safe ordering, cert-manager by default, and a dedicated
  client-side purge executable.
- Why chosen: it satisfies deterministic GitOps rendering while still proving
  cluster-dependent conditions and placing destructive deletion behind a
  separate, strongly confirmed boundary.

### Option B: Helm Templates With Cluster `lookup`

- Summary: inspect cert-manager, existing releases, policy, and Secrets while
  rendering.
- Why rejected: output would depend on the rendering environment, `helm
  template` would not match installation, and fallback behavior could change
  implicitly instead of following reviewed values.

### Option C: Kubeseer-Owned PKI Controller Or Hook-Generated Certificates

- Summary: generate and rotate certificates in Kubeseer or one-shot Helm Jobs.
- Why rejected: a PKI controller expands security-sensitive runtime code and
  RBAC; one-shot Jobs do not provide continuous renewal. Both duplicate a
  responsibility already covered by cert-manager or an administrator PKI.

### Option D: Delete CRDs Through Helm Uninstall

- Summary: template CRDs or attach destructive delete hooks to ordinary
  uninstall.
- Why rejected: routine release removal would risk deleting all Kubeseer state.
  CRDs in `crds/` plus an explicit purge executable make the safe and destructive
  lifecycles unmistakably different.

## Simplicity And Elegance Review

- Simplest viable shape: one chart, one runtime image, one production executable,
  two certificate modes, and one separate destructive executable.
- Coupling check: templates translate values only; package lifecycle code owns
  cluster checks; the manager composition root owns runtime setup; cert-manager
  or the external Secret owner owns key issuance.
- Reduced moving parts: cert-manager is a prerequisite, not a subchart; there is
  no live template lookup, PKI controller, wildcard RBAC generator, backup
  subsystem, or second runtime sidecar.
- Challenge result: removing hook checks would make successful Helm release state
  weaker than the approved readiness contract, while embedding purge in uninstall
  would weaken data safety. The selected boundaries are the minimum additions
  needed for those observable guarantees.
- Future-proofing: signing, SBOM publication, OLM, GitOps integration, additional
  certificate providers, and storage conversion remain deferred.

## Components And Interfaces

### Production Command And Manager Composition Root

`cmd/kubeseer` parses immutable startup configuration and supports:

- `kubeseer manager`: constructs the controller-runtime manager, registers
  `kubeseer.io/v1alpha1`, reconciliation, admission endpoints, health/readiness,
  metrics, leader election, limit profile, and optional tracing;
- `kubeseer version`: prints the build-time version and commit identity;
- `kubeseer package <preflight|policy|verify|pre-delete|post-delete>`: executes
  bounded hook operations with structured, sanitized diagnostics.

Invalid bind addresses, ports, leader-election values, limit overrides,
certificate paths, or tracing configuration fail before manager start. The
runtime image is a multi-stage, reproducible Linux build whose final stage is
distroless/static or equivalent, runs as a fixed non-root numeric UID, and
contains only the executable and required trust roots. OCI labels carry version,
revision, source, license, and creation metadata; the image reference may never
default to `latest`.

Requirements: `R2`, `R3`, `R7`, `NFR1`, `NFR5`, `NFR7`.

### Canonical Helm Chart

`charts/kubeseer` contains `Chart.yaml`, `values.yaml`, `values.schema.json`, a
generated chart README, `crds/`, and templates grouped by manager, RBAC, webhook,
certificate, policy hooks, and lifecycle hooks. `_helpers.tpl` is the only source
for names, labels, selectors, release ownership, and Service DNS names.

`Chart.yaml` declares API v2, SemVer chart version, manager `appVersion`, the
approved Kubernetes `kubeVersion` range, and no bundled chart dependency.
`values.schema.json` rejects missing or malformed values, `latest`, unsupported
certificate modes, incomplete `externalSecret` inputs, unsafe replica/resource
settings, and incoherent RBAC or policy combinations. Templates use values and
chart metadata only; `lookup` is forbidden by package verification.

Cluster singleton names remain `kubeseer-validating-webhook`,
`installation-access-ceiling`, and the two generated CRD names. A preflight Job
rejects an existing singleton carrying another release identity. All other names
are bounded derivatives of release name and namespace.

Requirements: `R1`, `R4`, `R8`, `R10`, `NFR4`, `NFR5`, `NFR8`.

### Values Contract

The public values tree is deliberately finite:

```yaml
image: {repository: ghcr.io/steeltanuki/kubeseer, tag: "", pullPolicy: IfNotPresent}
replicaCount: 1
resources: {requests: {}, limits: {}}
manager: {leaderElection: true, terminationGracePeriodSeconds: 30}
metrics: {service: {enabled: true}}
tracing: {enabled: false, endpoint: ""}
certificate:
  mode: certManager
  certManager: {issuer: {}, certificate: {}}
  externalSecret: {secretName: "", caBundle: ""}
accessPolicy:
  mode: managed
  explicitNamespaces: []
  allowedResources: []
  allowClusterScoped: false
rbac:
  observed: {namespaced: [], clusterScoped: []}
  authors: {namespaces: [], subjects: []}
limits: {}
```

An empty image tag resolves to `Chart.appVersion`. Resource and limit defaults
are copied from the approved runtime contracts and remain non-zero and finite.
`systemNamespaces` is modeled with schema/template logic that preserves omitted
versus explicitly empty input. Secret private keys are never accepted as values.
Every public leaf is documented with type, default, constraints, and security
effect.

Requirements: `R1`, `R3`, `R5`, `R6`, `R7`, `R8`, `R10`.

### CRD Packaging And Upgrade Gate

`make package-sync-crds` copies the two verified files from `config/crd/bases`
to chart `crds/` without transformation. Verification regenerates both sources,
compares source and chart copies byte-for-byte, and records a digest in chart
metadata consumed by preflight.

Helm installs missing CRDs from `crds/` but does not own CRD upgrades. The
documented upgrade procedure first runs an explicit CRD compatibility check,
applies only approved compatible schemas with a fixed field manager, and waits
for `Established`. It rejects storage-version removal, scope changes, destructive
schema transitions, or any transition requiring an unapproved conversion. The
pre-upgrade hook refuses to roll out a manager whose required CRD digest is not
established. Normal uninstall never targets CRDs.

Requirements: `R4`, `R8`, `R9`, `R10`, `NFR4`, `NFR9`.

### Workload, Services, And Runtime Security

The chart renders one rolling `Deployment`, dedicated ServiceAccount, webhook
Service, and cluster-local metrics Service. Defaults are one replica,
`maxUnavailable: 0`, `maxSurge: 1`, leader election enabled, non-zero resource
requests, finite limits, and bounded termination grace. Readiness and liveness
use separate controller-runtime endpoints; readiness includes manager setup and
certificate checks.

Pod and container security set non-root numeric identity, no privilege
escalation, all capabilities dropped, runtime-default seccomp, no host network,
PID, IPC, host paths, or privileged mode, and a read-only root filesystem.
Only projected ServiceAccount data and read-only TLS/CA mounts are present;
explicit empty-directory storage is added only if the executable needs a bounded
writable path. Metrics remain cluster-local. Tracing adds configuration only
when enabled.

Requirements: `R3`, `R7`, `NFR1`, `NFR3`, `NFR6`, `NFR8`.

### RBAC Synthesis And Permission Boundaries

RBAC is split into independently reviewable template groups:

- operator RBAC covers owned APIs, status, Events, leader-election leases,
  discovery, and only explicitly configured observed resources;
- namespaced observed rules render a Role and RoleBinding in each selected
  namespace; cluster-scoped reads require a separate explicit ClusterRole rule;
- the reusable author Role manages namespaced Kubeseer objects but cannot read
  observed resources or mutate policy, Deployment, RBAC, or webhooks;
- hook RBAC is command-specific, short-lived, and grants only the reads or named
  deletes required by its lifecycle phase.

Default observed-resource and Secret permissions are empty. Schema and static
verification reject wildcard resources, wildcard write verbs, Secret reads, or
cluster-scoped observed rules without both the RBAC opt-in and logical policy
opt-in. Runtime authorization continues to intersect these Kubernetes
permissions with `installation-access-ceiling` before every read.

Requirements: `R5`, `R6`, `R9`, `R10`, `NFR1`, `NFR2`.

### Access Policy Bootstrap

A pre-install/pre-upgrade hook Job uses the typed Kubeseer client to reconcile
the canonical policy after CRDs are established and before the manager rollout.
The unmodified managed profile is deny-all: `Explicit`, no namespaces, no
resource rules, and cluster-scoped observation disabled. Stable ownership labels
and `helm.sh/resource-policy: keep` semantics identify it as retained state.

Preflight fails without mutation when managed mode finds a pre-existing policy
not owned by the same active release. External mode renders no policy payload or
write operation. On upgrade or rollback, Helm's effective release values produce
the same policy unless the administrator explicitly changes policy values; any
change on an existing installation traverses the active validating webhook.

Requirements: `R6`, `R8`, `R9`, `NFR1`, `NFR2`, `NFR9`.

### Certificate Modes And Reload

In `certManager` mode the chart renders a namespaced self-signed bootstrap
Issuer, CA Certificate and Issuer, serving Certificate, and CA-injection
annotation on the validating webhook. The serving certificate contains both
`<service>.<namespace>.svc` and
`<service>.<namespace>.svc.cluster.local`, stores key material in a Secret, and
uses cert-manager renewal with private-key rotation. The required cert-manager
API/version is checked before normal resources are applied.

In `externalSecret` mode schema requires `secretName` and a non-empty PEM
`caBundle`. The chart renders the public CA in a ConfigMap and webhook client
configurations, but never renders, owns, reads through API, or deletes the TLS
Secret. The kubelet mounts `tls.crt` and `tls.key` read-only from the named Secret.

Controller-runtime's certificate watcher reloads the mounted key pair. A
readiness checker parses the certificate, checks expiry and exact Service DNS
SANs, and verifies it against the mounted CA bundle. External CA rollover uses
an overlap sequence: first upgrade the release CA bundle to trust old and new
CAs, then rotate the Secret, verify the new endpoint, and finally remove the old
CA in a later upgrade. Post-install verification also checks both webhook
entries contain the expected CA and can reach the trusted endpoint.

Requirements: `R7`, `R8`, `R9`, `R10`, `NFR1`, `NFR3`, `NFR8`.

### Helm Lifecycle Hooks And Safe Uninstall

Hook weights impose this order:

1. pre-install/pre-upgrade/pre-rollback RBAC and preflight;
2. managed policy reconciliation;
3. normal release resource application;
4. post-install/post-upgrade readiness verification;
5. pre-delete deletion of the owned validating webhook configuration;
6. normal Helm deletion of runtime, RBAC, and cert-manager resources;
7. post-delete retained-resource report.

Hooks check canonical labels and Helm ownership before mutation. Successful hook
Jobs and temporary RBAC are deleted; failed Jobs remain for diagnosis. The
documented uninstall command uses `helm uninstall --ignore-not-found --wait`, so
repetition converges. CRDs, all custom resources, the retained access policy,
and any external TLS Secret remain. A versioned wrapper verifies the ordering
and prints retained resources even when the release was already absent.

Requirements: `R8`, `R9`, `R10`, `NFR3`, `NFR6`, `NFR9`.

### Explicit Purge Executable

`cmd/kubeseer-purge` builds one client-side executable with `--version`. Every
run requires an absolute kubeconfig path, explicit context, and the exact token
`purge-kubeseer-crds`. Before deletion it resolves the selected context and API
server, prints the target identity and deletion plan, and compares it with the
explicit confirmation inputs. Missing, ambiguous, or mismatched identity exits
without writes.

Confirmed execution lists Kubeseer and policy instances, deletes instances,
waits for their collections to become empty, and only then deletes the two exact
CRD names. A remaining instance or finalizer blocks its CRD deletion. The tool
never selects arbitrary resources by broad label, never deletes Secrets, and
reports `deleted`, `already absent`, or `retained/blocked` for every target.
Re-running against an already purged cluster succeeds.

Requirements: `R9`, `R10`, `NFR1`, `NFR3`, `NFR9`.

## Data Models

### Render Identity

Render identity is the tuple `(chart version, app version, release name,
namespace, normalized values)`. Package verification renders twice, normalizes
only nondeterministic archive metadata outside Kubernetes YAML, and requires
byte-identical object streams and ordering.

### Package State

Cluster state is divided into:

- release-owned runtime state: Deployment, Services, ServiceAccount, runtime
  RBAC, webhook configuration, and cert-manager resources;
- retained Kubeseer state: both CRDs, all instances, and
  `installation-access-ceiling`;
- external state: fallback TLS Secret, cert-manager installation, cluster,
  registry, and administrator RBAC.

This classification drives uninstall, purge, ownership checks, and reports.

### Certificate State

The manager consumes `(certPath, keyPath, caPath, serviceDNSNames)`. Readiness is
true only when files exist, the key matches the leaf certificate, validity covers
the current time, every required DNS name is present, and chain verification
succeeds against the current CA bundle. The watcher atomically replaces the
in-memory certificate only after a valid pair loads.

### Purge Plan And Result

The purge plan contains resolved kubeconfig path, context, API server identity,
confirmation token, exact CRD names, and ordered custom-resource collections.
Each result contains target, action, outcome, and sanitized error. No Kubernetes
payload or Secret content is included.

## Error Handling

- values and chart metadata errors fail offline schema/lint verification;
- cluster-version mismatch fails Helm's `kubeVersion` gate;
- missing cert-manager APIs, singleton ownership conflicts, incompatible CRDs,
  existing managed policy conflicts, and missing required permissions fail the
  preflight hook before manager rollout;
- unavailable or invalid TLS material keeps manager readiness false and causes
  post-install verification to fail within a bounded timeout;
- a failed rolling update remains visible through Deployment conditions and a
  failed Helm command; automatic destructive rollback is not attempted;
- webhook CREATE/UPDATE remains fail closed while registration exists and the
  endpoint is unavailable;
- uninstall removes webhook registration before the final endpoint and reports
  any owned object that remains;
- purge exits before CRD deletion when instances remain and never converts a
  partial failure into success.

Diagnostics use stable object identities and reason codes, omit private keys,
Secret bodies, extracted values, cloud credentials, and ambient kubeconfig
paths, and return non-zero for every incomplete operation.

## Security Considerations

- no private key enters values, rendered YAML, source, image layers, logs, or
  documentation;
- manager and hook Pods run under restricted security contexts and mount TLS
  material read-only;
- default logical policy and observed-resource RBAC are both deny-all;
- author, operator, hook, and observed-resource permissions remain separate;
- templates cannot discover or silently adopt live resources;
- all cluster singleton mutation verifies exact name and release ownership;
- purge requires explicit target resolution and typed confirmation, then deletes
  only the two approved APIs in instance-before-definition order;
- tests use an explicitly supplied disposable kubeconfig and never ambient cloud
  credentials.

## Failure Modes And Tradeoffs

| Failure mode | Mitigation | Accepted tradeoff |
| --- | --- | --- |
| cert-manager absent in default mode | Preflight fails and names `externalSecret` | Fallback selection is explicit, never automatic |
| External Secret invalid or wrong DNS | Readiness and installation verification fail | External PKI ownership requires administrator remediation |
| CA changes without overlap | Preflight/readiness reject the new chain | CA rollover needs a multi-step release operation |
| CRD package differs from generated source | Verification and digest gate fail | CRD upgrades are a separate explicit command |
| Existing singleton belongs to another release | Preflight refuses adoption | Only one active release is supported |
| Hook Job fails | Helm fails and retains the failed Job | Hooks add short-lived RBAC and operational objects |
| Manager rollout never becomes ready | `--wait` times out with rollout incomplete | No destructive automatic rollback |
| Uninstall is interrupted | Idempotent documented procedure can resume | Retained CRDs/policy require explicit later handling |
| Instance finalizer blocks purge | Purge stops before deleting its CRD | Administrator must resolve the finalizer safely |
| Chart and image versions diverge | Default tag follows `appVersion`; verification checks metadata | Explicit image overrides remain supported and visible |

## Testing Strategy

The feature adds no package-local unit-test layer. Proof is split by observable
boundary:

- static/package integration: build binaries, verify licenses and immutable
  metadata, regenerate CRDs, compare chart copies, reject `lookup`, inspect image
  user/entrypoint/content, lint/package the chart, and render every values profile
  twice;
- schema matrix: valid defaults, namespace override, policy modes, namespaced and
  cluster RBAC, both certificate modes, plus one invalid profile per constrained
  value class;
- manager integration: start the real composition root with fake or envtest
  infrastructure and prove registration, startup failure, health/readiness,
  metrics, version, TLS reload, and signal shutdown;
- supported-cluster smoke: install on explicitly supplied Kubernetes 1.35.6 and
  1.36.2 clusters, prove APIs, Deployment, webhook trust/rejection, policy,
  permissions, both certificate modes, serving-key rotation, upgrade, compatible
  rollback, uninstall retention, and failed-confirmation purge;
- destructive purge smoke: use only a disposable explicit cluster, prove target
  mismatch safety, instance-before-CRD order, external Secret retention, and
  idempotency.

Every test entrypoint uses a named exact test pattern and fails when no scenario
runs. Cluster creation remains outside this feature.

## Verification Plan

- Requirement proof: chart rendering and schema fixtures prove `R1`, workload
  and image inspection prove `R2`-`R3`, generated-artifact checks prove `R4`,
  RBAC fixtures prove `R5`, policy hooks prove `R6`, dual-mode TLS scenarios
  prove `R7`, lifecycle scenarios prove `R8`-`R9`, and executable documentation
  checks prove `R10`.
- Test evidence: targeted integration commands assert non-vacuous manager and
  package suites; cluster smoke commands run against each centrally declared
  Kubernetes version with an explicit kubeconfig and context.
- Operational evidence: Helm hook status, Deployment conditions, ready/health
  endpoints, webhook TLS probes, `kubeseer version`, Helm history, uninstall
  retained-resource output, and purge per-target results provide the handoff
  evidence.
- Drift evidence: `make verify` checks generated CRDs, chart copies, chart README,
  values schema coverage, deterministic renders, and immutable image inputs.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Canonical Helm Chart; Values Contract; render verification |
| `R2` | Production Command And Manager Composition Root; image inspection |
| `R3` | Workload, Services, And Runtime Security; readiness smoke |
| `R4` | CRD Packaging And Upgrade Gate; lifecycle smoke |
| `R5` | RBAC Synthesis And Permission Boundaries; RBAC fixtures |
| `R6` | Access Policy Bootstrap; policy hook and smoke proof |
| `R7` | Certificate Modes And Reload; dual-mode TLS smoke |
| `R8` | Helm Lifecycle Hooks And Safe Uninstall; upgrade and rollback smoke |
| `R9` | Safe Uninstall; Explicit Purge Executable; destructive smoke |
| `R10` | Testing Strategy; Verification Plan; generated documentation |
| `NFR1` | Restricted Pod security, deny-all defaults, TLS and purge controls |
| `NFR2` | Independent RBAC and logical policy template groups |
| `NFR3` | Bounded hooks, readiness, rollout, uninstall, and purge outcomes |
| `NFR4` | Declared Kubernetes range, generated CRDs, compatibility matrix |
| `NFR5` | Pinned inputs, deterministic rendering, immutable version metadata |
| `NFR6` | Stable commands, probes, hooks, reports, and chart README |
| `NFR7` | One minimal runtime executable and no embedded credentials |
| `NFR8` | Provider-neutral chart with two explicit certificate modes |
| `NFR9` | Retained routine state and separate confirmed purge |
| `NFR10` | Static, integration, and genuine cluster-level proof layers |
| `C1` | Approved upstream contracts compose at the manager and package boundaries |
| `C2` | Canonical Helm Chart excludes Kustomize overlays |
| `C3` | Supported-cluster matrix uses Kubernetes 1.35.6 and 1.36.2 |
| `C4` | CRD copies and manager registration preserve `v1alpha1` |
| `C5` | Certificate Modes And Reload implements only the two approved modes |
| `C6` | Production composition reuses controller-runtime facilities |
| `C7` | RBAC never replaces runtime logical authorization |
| `C8` | Values Contract imports approved performance defaults and validation |
| `C9` | Smoke tests consume, but never create, an explicit cluster |
| `C10` | Smoke scope stops at installation lifecycle and permission boundaries |
| `C11` | Testing Strategy uses higher-layer non-vacuous proofs |
| `C12` | Build, chart, image metadata, scripts, and docs retain Apache-2.0 notices |
| `C13` | Package toolchain pins the Helm CLI version |
| `C14` | Singleton ownership preflight enforces one active release |
| `C15` | Preflight and compatibility proof use a pinned cert-manager range |
