---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-03T09:03:56Z
last_modified: 2026-09-03T09:03:56Z
approved_fingerprint: sha256:ed40a58d26c328ce42ae3da7d7d15c97505545095de0ce2419f9df81a119a9ea
source_requirements_approved_at: 2026-09-03T08:54:34Z
source_requirements_fingerprint: sha256:bbb9db7d26f122b209515c4b12663a4b4fda6c81a7a6c7fa9bddd75675c72e13
---

# Local Development Environment Design

## Overview

The feature adds one persistent contributor environment behind the stable
`make local-*` interface. A shell orchestrator owns host preflight, exact kind
and Podman lifecycle, image construction and loading, Helm convergence, example
application, and cleanup. A small Go probe owns read-only Kubernetes identity,
readiness, example assertions, and sanitized diagnostic projections. This split
keeps external-process ownership visible while avoiding presentation-text
parsing or unsafe JSON construction for cluster observations.

The environment has one fixed identity: kind cluster `kubeseer-local`, context
`kind-kubeseer-local`, Helm release `kubeseer`, namespace `kubeseer-system`, and
access-policy singleton `installation-access-ceiling`. Mutable state, the
kubeconfig, caches, image-transfer archives, locks, and diagnostics live outside
the repository. Every cluster command uses the stored absolute kubeconfig and
exact context after validating metadata and API-server identity. Ambient
kubeconfig, existing-cluster flags, and cloud credentials are removed from
child environments.

The current checkout is represented by a content-derived source identity. The
orchestrator tags the production Dockerfile build as
`localhost/kubeseer-local:<source-identity>`, records its Podman image ID, and
imports that exact OCI image into every kind node without a registry. It
installs pinned cert-manager and the canonical `charts/kubeseer` chart with a
committed local values profile. Repeated setup converges the owned cluster and
release; it never creates a second cluster or adopts an unowned one.

Seven committed examples exercise only public Kubernetes and Kubeseer
interfaces. One ordered catalog drives application, verification,
documentation coverage, and label-scoped cleanup. `make local-verify` proves
package readiness and every example, while `make e2e` remains an independent
run-unique release-certification path.

Webhook backend observation uses `discovery.k8s.io/v1 EndpointSlice`
throughout the local probe, disposable E2E readiness, and contributor
documentation. Consumers list every slice in the Service namespace with the
standard `kubernetes.io/service-name` label, aggregate ready endpoint entries,
and never fall back to deprecated core `v1 Endpoints`. Kubernetes documents
both the one-Service-to-many-slices relationship and label-based lookup in the
[EndpointSlice API reference](https://kubernetes.io/docs/reference/kubernetes-api/discovery/endpoint-slice-v1/).

<!-- assumed: repository-external defaults follow XDG state and cache conventions, with explicit absolute `KUBESEER_LOCAL_STATE_DIR` and `KUBESEER_LOCAL_CACHE_DIR` overrides allowed for CI (source: R3.AC3, C9, C10, and the existing E2E run-owned directory model) -->
<!-- assumed: a read-only Go probe is the smallest safe typed observer because Go and client-go are existing dependencies while jq is not a prerequisite (source: .walden/constitution.md, go.mod, and R8.AC4) -->
<!-- assumed: local lifecycle and E2E share only stateless provider helpers; identities, state machines, kubeconfigs, diagnostics, and cleanup remain separate (source: R8.AC10, R8.AC12, C7, and C8) -->
<!-- assumed: an EndpointSlice endpoint with `conditions.ready` absent is ready because the discovery/v1 API defines nil as true (source: Kubernetes EndpointSlice API reference and R6.AC15) -->

## Architecture

```text
                         committed inputs
              +-----------------------------------+
              | toolchain.mk | chart | local values |
              | examples/catalog + example manifests |
              +------------------+----------------+
                                 |
make local-<action>              |
        |                        |
        v                        v
hack/local-environment.sh -> hack/kind-podman-common.sh
        |       |                    |
        |       +-> hack/local-examples.sh
        |                            |
        +-> external state/cache     +-> sanitized exact CLI calls
        |   metadata, kubeconfig,
        |   lock, diagnostics
        v
kind `kubeseer-local` via rootless Podman
        |-- cert-manager + canonical Helm release
        |-- installation-access-ceiling
        +-- labeled example resources
                    |
                    v
          go run ./cmd/kubeseer-local
          identity | readiness | verify | diagnostics
                    |
                    +-> discovery/v1 EndpointSlice LIST

make e2e -> hack/e2e-harness.sh -> independent run-unique cluster
                                      +-> discovery/v1 EndpointSlice LIST
```

The Makefile is the supported user-facing router. Each target passes central
compatibility values and one exact action to `hack/local-environment.sh`. The
script rejects extra positional arguments and unknown actions before creating
state or invoking a mutating tool. Internal scripts remain directly executable
for acceptance testing, but are not an alternative public workflow.

### Lifecycle flow

1. Resolve the repository root without changing the caller's directory.
   Resolve state and cache roots, require absolute normalized paths, reject a
   root at or inside the repository, and snapshot the worktree.
2. Verify Linux/amd64, every required command and supported version, rootless
   Podman operation, explicit Podman selection by kind, and the selected
   Kubernetes-to-node-image mapping. No mutation occurs before this succeeds.
3. Acquire an atomic action lock below the state root for mutations. Concurrent
   mutation fails with recovery guidance; read-only status never steals a lock.
4. Classify ownership from exact kind name, `metadata.v1`, and kubeconfig. A
   collision without matching metadata fails closed. An owned cluster at
   another supported Kubernetes version reports both versions and the explicit
   `make local-down` recovery.
5. If absent, atomically write a `creating` intent and create exactly
   `kubeseer-local` with `KIND_EXPERIMENTAL_PROVIDER=podman`, the selected node
   image, and explicit kubeconfig. After context, loopback API server, server
   version, and kind node identity match, promote metadata to `owned`. Failure
   before promotion deletes only that partial cluster; later failure retains it.
6. Hash `HEAD`, index and worktree diffs, untracked-file identities, Dockerfile,
   and build inputs. Reuse an image only when source identity, tag, and Podman
   ID match; otherwise build a new content-bound tag. Export one OCI archive and
   import it into every named node's `k8s.io` containerd namespace.
7. Apply pinned cert-manager server-side and wait for its CRDs and Deployments.
   Run `helm upgrade --install --atomic --cleanup-on-fail --wait` against
   `charts/kubeseer` with the local profile, exact image tag, and
   `IfNotPresent`. A rejected upgrade leaves the prior successful revision.
8. Run the typed readiness probe. Success requires both Kubeseer CRDs,
   Deployment availability, serving Certificate readiness, every non-empty
   webhook CA bundle, at least one ready endpoint aggregated from all webhook
   Service EndpointSlices, `/readyz`, `/metrics`, and the exact policy
   singleton. Then record the converged source and image identities.
9. Compare the worktree snapshot. Drift fails the action and reports paths
   without resetting, deleting, or stashing contributor work.

### Command behavior

| Public target | Action | Mutation boundary |
| --- | --- | --- |
| `make local-check` | `check` | No state, Podman, or cluster mutation |
| `make local-up` | `up` | Exact owned cluster, prerequisite, and Helm release |
| `make local-status` | `status` | Read-only metadata and allowlisted cluster projection |
| `make local-examples` | `examples` | Catalog-owned examples and required policy inputs |
| `make local-verify` | `verify` | Read-only assertions and bounded port-forwards |
| `make local-diagnostics` | `diagnostics` | External diagnostic directory only |
| `make local-examples-down` | `examples-down` | Catalog-labeled resources and fixture definitions |
| `make local-down` | `down` | Exact owned cluster and corresponding state only |

The documented per-example interface is `make local-example
EXAMPLE=<catalog-name> ACTION=<apply|inspect|verify|down>`. It routes through
the same identity gate and catalog executor; it cannot accept an arbitrary
path, namespace, resource selector, or command. The eight lifecycle targets
remain the complete-environment shortcuts required by `R1`.

Every successful action prints exactly one
`LOCAL_ENVIRONMENT=<phase> STATUS=passed` marker after the worktree check.
Failures identify the phase, owned target where relevant, and earliest stable
reason, return non-zero, and print no success marker.

## Options Considered

### Option A: Shell lifecycle plus typed read-only Go probe

- Summary: Keep provider/process lifecycle in one shell orchestrator, share
  stateless kind/Podman helpers with E2E, and use Go for typed identity,
  readiness, verification, and diagnostic projections.
- Why chosen: It matches the existing ownership model, makes destructive calls
  auditable, and avoids brittle table parsing without adding a runtime service
  or external scripting dependency.

### Option B: Add a persistent mode to the E2E harness

- Summary: Parameterize `hack/e2e-harness.sh` to retain and reuse its cluster.
- Why rejected: Disposable certification and persistent development have
  opposite recovery semantics. Sharing their state machine could make an E2E
  failure retain state or a local failure delete diagnostic state.

### Option C: Implement every observation in shell

- Summary: Use kubectl, JSONPath, text filters, and shell-generated JSON.
- Why rejected: Conditions, status values, event identity, log vocabulary, and
  redaction would acquire fragile parsing and quoting dependencies.

### Option D: Implement an all-Go environment manager

- Summary: Wrap kind, Podman, Helm, kubectl, observations, and cleanup in Go.
- Why rejected: It duplicates mature CLI behavior and the existing shell
  lifecycle pattern while hiding exact destructive commands behind more code.

### Endpoint backend observation: aggregate EndpointSlices

- Selected: List every `discovery.k8s.io/v1 EndpointSlice` in the Service
  namespace with `kubernetes.io/service-name=<release>-webhook`, then aggregate
  endpoint entries whose `conditions.ready` value is true or absent.
- Why selected: EndpointSlices are stable, support one-to-many and dual-stack
  Service backends, and are the Kubernetes replacement for Endpoints. The
  Kubernetes migration guidance requires label-based listing because slice
  names are not predictable: [Endpoints deprecation guidance](https://kubernetes.io/blog/2025/04/24/endpoints-deprecation/).
- Rejected alternative: Continue reading core `v1 Endpoints` and suppress the
  server warning. Suppression would hide a deprecated dependency, retain the
  single-object and primary-address-family limitations, and fail `NFR10`.

## Simplicity And Elegance Review

- Simplest viable shape: Eight Make targets route to one state machine; one
  shared provider library has stateless wrappers; one read-only Go probe owns
  structured observations; one catalog drives all examples.
- Coupling check: The orchestrator depends only on central versions, Dockerfile,
  chart, public Kubernetes APIs, and probe commands. It neither imports
  controller internals nor invokes E2E scenarios.
- First-draft challenge: Separate scripts or binaries per action would let
  argument and ownership checks diverge. The final shape has one identity gate
  before every cluster read or write.
- Endpoint-migration challenge: A generic cross-harness readiness binary would
  share more code, but it would also couple the fixed local metadata contract to
  the run-unique E2E lifecycle. The design keeps each existing observer boundary
  and aligns only its stable EndpointSlice query/readiness semantics and proofs.
- Future-proofing: Named profiles, multiple clusters, Docker, Podman machine,
  offline bootstrap, additional hosts, registries, and scenario reuse remain
  deferred. Versioned metadata permits a future approved migration; stable
  EndpointSlice consumption removes the known Kubernetes 1.33+ API deprecation
  without adding compatibility branches.

## Components And Interfaces

### Make lifecycle API and central toolchain

- Purpose: Expose stable commands and centralize Kubernetes versions, node
  images, cert-manager, and minimum tool versions in `hack/toolchain.mk` for
  package, E2E, and local targets.
- Inputs/Outputs: Validated Make overrides in; terminal marker and exit status
  out.
- Dependencies: `Makefile`, `hack/toolchain.mk`, canonical build and package.
- Requirements: `R1`, `R2`, `R8`, `R11`, `NFR3`, `NFR5`, `NFR7`, `NFR9`.

The contract retains the two current Kubernetes members and exact kind image
mapping, existing cert-manager pin, Go baseline from `go.mod`, and explicit
minimums for Git, Make, Podman, kind, kubectl, Helm, and curl. Version parsing
accepts vendor suffixes but compares numeric components. Overrides must remain
members of the declared matrix and cannot expand compatibility claims.

### Shared kind/Podman process primitives

- Purpose: Provide sanitized process execution, explicit kubeconfig/context
  wrappers, Podman-provider kind calls, exact OCI import, and bounded
  port-forward handling without lifecycle decisions.
- Inputs/Outputs: Absolute paths and exact identities in; bounded command result
  out.
- Dependencies: `hack/kind-podman-common.sh`, pinned external CLIs.
- Requirements: `R2`, `R3`, `R4`, `R6`, `R8`, `NFR1`, `NFR4`, `NFR7`, `NFR9`, `NFR10`.

The helper never reads ambient kubeconfig and exposes no generic prune or
wildcard delete. Cloud and provider-discovery variables are unset for every
child. E2E retains its run-unique identity and local retains its fixed identity.

### Local lifecycle orchestrator

- Purpose: Implement preflight, state classification, convergence, routing, and
  exact cleanup under one fail-closed state machine.
- Inputs/Outputs: One action and central configuration; atomic metadata, phase
  output, and terminal status.
- Dependencies: provider helpers, external CLIs, Dockerfile, chart, Go probe.
- Requirements: `R1` through `R6`, `R8` through `R10`, `NFR1` through `NFR7`,
  `NFR9`, `NFR10`.

Mutating traps cover only resources created by the current action. No trap
deletes a previously owned cluster. `down` validates metadata, kubeconfig,
exact kind membership, and context before deleting `kubeseer-local`; state is
removed only after confirmed deletion. Caches and images are reported and kept.

### Local values profile

- Purpose: Configure the chart for examples without bypassing authorization or
  broadening observed-resource RBAC beyond the catalog.
- Inputs/Outputs: `config/local/values.yaml` in; deterministic Helm values out.
- Dependencies: chart schema, `certManager`, example catalog.
- Requirements: `R5`, `R6`, `R7`, `NFR2`, `NFR3`, `NFR9`.

Managed policy mode lists only example namespaces, API groups, and kinds.
Observed RBAC is separate and uses only `get`, `list`, and `watch`; Secrets and
wildcards are absent. Namespace/kind denials therefore exercise real logical
policy rather than missing ServiceAccount permission.

### Ordered example catalog and executor

- Purpose: Define stable identity, application order, public outcomes,
  per-example commands, and label-scoped cleanup.
- Inputs/Outputs: `examples/catalog.txt` plus `examples/<name>/`; applied objects
  and `EXAMPLE=<name> STATUS=<outcome>` records.
- Dependencies: owned cluster, local values, kubectl, Go probe.
- Requirements: `R7`, `R8`, `R10`, `R11`, `NFR3`, `NFR5`, `NFR8`, `NFR9`.

The catalog contains exactly these validated names in order:
`builtin-resource`, `typed-extraction`, `value-operator`,
`cross-namespace-aggregation`, `custom-resource`, `authorization-denial`, and
`partial-degradation`. Duplicate, unknown, missing-directory, blank, or
unregistered verifier entries fail before application. Each directory contains
an Apache-2.0 README, versioned workload and Kubeseer manifests, and an expected
public outcome. Resources carry `kubeseer.io/example=<name>` and use exact
`kubeseer-example-*` namespaces.

| Example | Public behavior | Special lifecycle |
| --- | --- | --- |
| `builtin-resource` | Built-in Deployment selection and provenance | None |
| `typed-extraction` | Native scalar extraction and typed status | None |
| `value-operator` | Supported value transformation or predicate | None |
| `cross-namespace-aggregation` | Deterministic aggregation across two namespaces | Cleans both exact namespaces |
| `custom-resource` | Structural `Widget` discovery and extraction | CRD before CRs; CRs removed before CRD |
| `authorization-denial` | Structurally valid request outside policy is denied | Exact negative server-side request is the expected pass |
| `partial-degradation` | Successful source plus unavailable type and degraded condition | Establishes then removes a fixture type |

Application uses server-side apply with a fixed field manager. Cleanup
enumerates catalog identities, requires expected ownership labels, preserves
the Helm release and policy, and performs the custom-resource order explicitly.
Each example README uses the constrained `make local-example` interface for
apply, structured status inspection, its non-vacuous assertion, and deletion of
only that example's catalog-owned resources.

### Typed local probe

- Purpose: Verify kubeconfig/context/API identity, report readiness, assert
  example outcomes, and serialize sanitized diagnostics.
- Inputs/Outputs: `go run ./cmd/kubeseer-local
  <identity|readiness|verify|diagnostics>` with absolute paths; structured JSON
  and stable phase/example records.
- Dependencies: client-go discovery/v1 and core clients, Kubeseer API types,
  approved condition and observability vocabularies; no reconciliation
  packages.
- Requirements: `R3`, `R6` through `R9`, `NFR1`, `NFR2`, `NFR4`, `NFR6`,
  `NFR8`, `NFR9`, `NFR10`, `C13`.

Clients come only from stored kubeconfig. The selected context, loopback API
endpoint, server version, metadata, and node labels must agree before any
observation. All waits have deadlines and identify their predicate. Assertions
use literal public outcomes and do not reproduce product algorithms.

One internal read-only helper receives namespace and Service name, lists
EndpointSlices with an escaped equality selector built from
`discoveryv1.LabelServiceName`, and returns the number of ready endpoint
entries across every matching slice. `conditions.ready == nil` is treated as
ready according to the discovery/v1 contract; explicit false is excluded. The
readiness command fails on a list error or zero ready entries. Status and
diagnostics reuse the same helper, preserve their bounded numeric projection,
and represent an unavailable list without exposing raw API response bodies.
No consumer predicts slice names, creates slices, or reads core `v1 Endpoints`.

The E2E package gate performs the equivalent label-selected EndpointSlice
list through its explicit kubeconfig and context, and non-vacuously asserts at
least one ready endpoint before admission probes. This changes only the
backend-readiness observer; E2E identity, lifecycle, scenarios, and authority
remain unchanged.

### Documentation and acceptance harness

- Purpose: Document the complete lifecycle and prove each public phase.
- Inputs/Outputs: `docs/local-development.md`, example READMEs, README link, and
  `make test-local-environment`; documentation and acceptance evidence.
- Dependencies: public targets, fake tools, explicit Linux/amd64 rootless
  Podman runner for genuine proof.
- Requirements: `R2`, `R8`, `R11`, `NFR5`, `NFR7`, `NFR8`, `NFR10`.

Fake-tool acceptance proves argument rejection, ordering, ownership conflicts,
worktree preservation, and exact cleanup. Genuine acceptance invokes the same
Make targets against temporary absolute state/cache roots, requires every
phase marker, and proves the cluster absent at completion.

Contributor documentation uses `kubectl get endpointslice --selector
kubernetes.io/service-name=<service>` for direct backend inspection. Static
documentation checks reject commands that direct contributors to core
`v1 Endpoints`.

## Data Models

### Local paths

```text
state: ${XDG_STATE_HOME:-$HOME/.local/state}/kubeseer/local
cache: ${XDG_CACHE_HOME:-$HOME/.cache}/kubeseer/local

state/{metadata.v1,kubeconfig,action.lock/,diagnostics/<run-id>/}
cache/{images/<source-identity>.oci.tar,helm/,go/,downloads/}
```

Directories use `0700`; private files use at most `0600`. Overrides must be
absolute, outside the repository, and not `/`, a home directory, or unresolved.
Normal teardown never removes caches.

### Versioned ownership metadata

`metadata.v1` is parsed as non-executable allowlisted data:

```text
schema_version=1
state=creating|owned
cluster_name=kubeseer-local
context=kind-kubeseer-local
kubernetes_version=<supported-member>
kind_node_image=<central-image>
kubeconfig=<absolute-state-path>
namespace=kubeseer-system
release=kubeseer
source_revision=<git-revision>
source_identity=sha256:<checkout-digest>
image_reference=localhost/kubeseer-local:<digest-tag>
image_id=<podman-content-id>
```

Unknown, duplicate, missing, malformed, or mismatched fields mean conflict.
Updates use a private same-directory temporary file and atomic rename. The lock
records action and PID for diagnosis, but PID alone never authorizes removal.

### Ownership states

| State | Evidence | Behavior |
| --- | --- | --- |
| `absent` | No cluster and no active ownership | `up` may create; `down` reports absent |
| `creating` | Current intent and optional partial exact cluster | Current action may finish or remove its partial cluster |
| `owned` | Cluster, metadata, kubeconfig, and API agree | Converge, inspect, verify, diagnose, examples, or delete |
| `version-mismatch` | Owned cluster has another supported version | Read-only inspection; mutations name exact recovery |
| `conflict` | Exact name/state exists without full agreement | Preserve all resources and fail closed |
| `unreachable` | Owned cluster exists but API cannot respond | Preserve state; allow local diagnostics or explicit down |

### Webhook backend readiness

The observer input is `(namespace, serviceName)`. Its Kubernetes query is one
namespace-scoped EndpointSlice LIST with the standard Service-name label. The
result joins zero or more slices and counts endpoint entries using these
semantics:

| `conditions.ready` | Readiness contribution |
| --- | --- |
| absent | ready, as required by discovery/v1 |
| `true` | ready |
| `false` | not ready |

Only the aggregate count enters readiness/status/diagnostic output. Endpoint
addresses, target references, node names, zones, and topology are neither
required nor serialized. A zero count is not success even when matching slices
exist.

### Diagnostic projection

Each unique diagnostic directory contains a manifest of successfully written
files. Allowed data is limited to tool versions; ownership identities; node,
Deployment, Pod, CRD, Certificate, webhook, endpoint, and policy identity or
readiness; bounded manager log timestamp/level/event-code/reason/outcome/stage
and owning Kubeseer identity; Event type/reason/action/regarding identity; and
Kubeseer generation, conditions, summary, result hash, metric family names, and
stable errors.

There is no representation for kubeconfig bytes, tokens, Secret bodies,
arbitrary environment values, free-form Event messages, extracted values,
result bodies, selector operands, or unapproved log fields. Logs are bounded by
count and bytes. When the API is unavailable, local metadata is retained with a
stable unavailable outcome and diagnostics perform no cluster write.

## Error Handling

- Invalid actions, extra arguments, unsupported hosts, missing/incompatible
  tools, unusable Podman, and unmapped versions fail before mutation.
- First-time download failure names only dependency and pinned version.
- A cluster-name collision without full ownership is never adopted or deleted.
- Failure before `owned` promotion removes only the partial exact cluster;
  later failure preserves cluster, kubeconfig, and metadata.
- Build failure precedes Helm mutation; import failure names each affected node.
- cert-manager failure precedes Helm. Atomic Helm failure retains the prior
  successful revision.
- Readiness timeout names the exact condition, endpoint, or object.
- EndpointSlice listing errors fail readiness without falling back to core
  `v1 Endpoints`; status and diagnostics retain their sanitized unavailable
  projection.
- Zero matching EndpointSlices and matching slices with zero ready endpoints
  both report webhook Service reachability as unavailable.
- Example failures name the stable example; cleanup success cannot mask them.
- Empty catalogs, zero selected objects, missing verifiers, or omitted phases
  are non-zero failures.
- Diagnostic destination failure performs no cluster access. Partial collection
  records failed sections without raw downstream responses.
- Failed cluster deletion retains state and kubeconfig; confirmed deletion
  removes only owned state and reports retained cache/images.
- Worktree drift is reported and never repaired automatically.

## Security Considerations

- Kubernetes and Helm always receive an explicit absolute kubeconfig/context
  after the identity gate; no ambient or existing-cluster path exists.
- Kubernetes and cloud credentials are removed before every child process.
- Metadata is parsed as data, paths are normalized, files are private, and no
  metadata value becomes an unconstrained deletion target.
- Cleanup exposes no global prune, network/volume removal, wildcard cluster
  deletion, or CRD purge path.
- Policy and RBAC remain separate allowlists; Secrets and wildcards are absent.
- Diagnostics are bounded, allowlist-generated, external, private, and scanned
  for protected sentinels before being reported shareable.
- The Go probe uses API and vocabulary contracts only and cannot invoke
  reconciliation internals or mutate policy during verification.

## Failure Modes And Tradeoffs

| Failure mode | Mitigation | Accepted tradeoff |
| --- | --- | --- |
| Fixed name collides with unowned cluster | Metadata/context gate refuses mutation | User resolves the collision manually |
| Selected version differs from owned cluster | Report both versions and exact down command | No in-place control-plane upgrades |
| Initial creation is interrupted | Intent trap deletes only current partial cluster | Narrow cleanup is allowed before ownership |
| Later convergence is interrupted | Preserve owned state for diagnosis | Recovery may require rerunning local-up |
| Recorded image ID differs | Rebuild or fail before Helm | Images/cache remain after teardown |
| OCI import partially fails | Name failed nodes and stop convergence | Import repeats on next setup |
| cert-manager is unavailable | Stop before Kubeseer release | Offline first bootstrap is unsupported |
| Helm rollout fails | Atomic upgrade preserves prior revision | Setup fails although old release may work |
| EndpointSlice list is forbidden or unavailable | Fail the readiness predicate with a sanitized Service-level error | Deprecated Endpoints is never used as fallback |
| EndpointSlices exist without a ready endpoint | Keep waiting until the bounded deadline | Slice existence alone does not prove reachability |
| A Service spans multiple EndpointSlices | Aggregate every label-selected slice | Observation performs one namespaced LIST rather than one object GET |
| Catalog and policy diverge | Static cross-check of identities and scopes | New examples need coordinated changes |
| Negative example fails differently | Require exact approved denial | Generic failure never proves authorization |
| API is unavailable for diagnostics | Emit local metadata and stable outcome | Cluster projection is incomplete |
| Deletion fails | Keep metadata/kubeconfig for retry | Teardown cannot report success |
| Workflow happens to work elsewhere | Reject uncertified hosts | Expansion needs separate evidence |

## Testing Strategy

No dedicated package-local unit-test layer is added. Proof uses observable
boundaries:

- static verification checks licenses, target wiring, version mapping, shell
  safety, forbidden ambient kubeconfig/prune/purge paths, metadata schema,
  values/schema compatibility, RBAC/policy least privilege, catalog uniqueness,
  manifests, documentation coverage, E2E identity separation, EndpointSlice
  label selection, and absence of supported-workflow core `v1 Endpoints`
  consumers;
- a fake-tool shell harness proves preflight-before-mutation, argument rejection,
  exact commands, state transitions, interrupted creation cleanup, owned-state
  preservation, collision refusal, markers, and worktree invariance;
- `make test-local-environment` runs the genuine ordered lifecycle on a
  Linux/amd64 rootless-Podman runner using temporary external roots: check, up,
  repeated up, status, examples, repeated examples, verify, diagnostics,
  examples-down, and down. It checks markers, singleton cardinality, diagnostic
  sentinels, unrelated kind/Podman sentinels, worktree invariance, ready
  EndpointSlice aggregation, and absence of the Kubernetes Endpoints
  deprecation warning;
- the Go probe runs only against that genuine installed cluster. It registers
  exactly seven catalog names, rejects zero selection, uses bounded observation,
  and asserts public status, provenance, typed values, aggregation, admission
  denial, and degradation;
- existing E2E acceptance proves `make e2e` remains run-unique and never
  resolves or reuses `kubeseer-local`; its package gate asserts label-selected
  EndpointSlice readiness and emits no core Endpoints deprecation warning.

The genuine local suite is contributor-workflow evidence, not a replacement or
alias for release certification.

## Verification Plan

- Lifecycle proof: fake-tool acceptance and genuine repeated lifecycle prove
  `R1`-`R6`, ownership, image identity, package convergence, and teardown.
- EndpointSlice proof: static checks, fake command traces, local readiness,
  status, diagnostics, and the E2E package gate prove label-selected multi-slice
  aggregation, ready/nil-ready semantics, zero-ready failure, and absence of
  core `v1 Endpoints` requests for `R6`, `R8`, `R9`, `NFR10`, and `C13`.
- Example proof: catalog validation, apply, typed assertions, exact denial,
  degradation, repeated convergence, and ordered cleanup prove `R7`.
- Isolation proof: ambient credential sentinels, unrelated kind/Podman
  resources, external paths, E2E checks, and worktree hashes prove `R8`/`R10`.
- Diagnostic proof: reachable and unreachable runs validate schema, bounds,
  destination, forbidden-sentinel absence, and zero cluster writes for `R9`.
- Documentation proof: executable extraction checks versions, identities,
  limitations, network needs, EndpointSlice inspection, examples, destructive
  targets, and README linkage for `R11`.
- Operational evidence: markers, metadata, node/image identity, Helm history,
  readiness JSON, per-example outcomes, diagnostic manifest, cleanup report,
  and worktree hashes form the handoff.
- Non-vacuity proof: runners require named phases/tests, catalog/verifier counts
  must match, positive assertions select objects, and denial reason is exact.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Make API; command table; lifecycle acceptance |
| `R2` | Central toolchain; preflight; fake and genuine acceptance |
| `R3` | Orchestrator; ownership metadata/states; identity proof |
| `R4` | Source identity; OCI import; repeated-source proof |
| `R5` | Local values; canonical Helm convergence; policy/RBAC checks |
| `R6` | Typed EndpointSlice readiness; bounded flow; repeatability proof |
| `R7` | Ordered catalog/executor; typed example verifier |
| `R8` | Local and E2E EndpointSlice gates; independent identities; genuine CI target |
| `R9` | EndpointSlice-backed diagnostic projection and sanitized proof |
| `R10` | Ownership-gated down; scoped example cleanup; sentinels |
| `R11` | EndpointSlice inspection guidance and executable documentation checks |
| `NFR1` | Exact identities, sanitized wrappers, ownership states |
| `NFR2` | Separate policy/RBAC and allowlisted diagnostics |
| `NFR3` | Content identity, central pins, idempotent convergence |
| `NFR4` | Bounded operations and actionable atomic failures |
| `NFR5` | Stable Make API, markers, and ordered guide |
| `NFR6` | Earliest-phase errors and typed diagnostic projection |
| `NFR7` | Linux/amd64, rootless Podman, no cloud/registry dependency |
| `NFR8` | Cardinality gates, exact denial, typed public assertions |
| `NFR9` | Shared helpers and canonical build/package/E2E contracts |
| `NFR10` | Stable discovery/v1 consumers and deprecation-warning regression gates |
| `C1` | Approved packaging and E2E inputs are composed, not redefined |
| `C2` | Linux/amd64 is the only accepted host profile |
| `C3` | kind always selects rootless Podman explicitly |
| `C4` | `hack/toolchain.mk` owns versions and mappings |
| `C5` | Values fix chart, certificate, release, namespace, and policy |
| `C6` | Catalog-to-policy checks limit namespaces and kinds |
| `C7` | Separate identities, kubeconfigs, states, and cleanup paths |
| `C8` | Local verification remains distinct from `make e2e` |
| `C9` | Pinned downloads and retained cache are documented |
| `C10` | Path validation and worktree snapshots enforce external state |
| `C11` | New artifacts retain Apache-2.0 metadata |
| `C12` | Static, acceptance, and genuine cluster layers; no unit layer |
| `C13` | Label-selected EndpointSlice LIST and multi-slice ready aggregation |
