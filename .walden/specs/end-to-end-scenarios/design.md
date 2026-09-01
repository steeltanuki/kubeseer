---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-01T12:58:17Z
last_modified: 2026-09-01T12:58:17Z
approved_fingerprint: sha256:71be9aa71abbdc5a888df5bd0606f2d36037d937a4dfe60d6059c56a09f52e78
source_requirements_approved_at: 2026-09-01T12:30:49Z
source_requirements_fingerprint: sha256:5be99c6777610b01231284cf5e9a7501d6f89baa38f744eef3b84bb4a787dad3
---

# End-To-End Scenarios Design

## Overview

The feature adds one release-certification path behind `make e2e`. The path
builds the production manager image from the current checkout, creates a
run-owned kind cluster through Podman, installs the canonical Helm chart with
the default `certManager` certificate profile, and executes an ordered Go
scenario suite only through Kubernetes and HTTP boundaries exposed by the
installed product.

The existing shell harness remains the owner of external process lifecycle,
credentials, installation, diagnostics, and cleanup. A new `test/e2e` Go
package owns Kubernetes fixtures, bounded observation, public-result
assertions, and scenario reporting. A version-controlled certification
manifest binds every required scenario and every in-scope feature to either a
stable executable scenario or a named approved prerequisite certification.
The Go suite validates this manifest before executing product scenarios, so a
missing mapping, duplicate identifier, skipped scenario, or zero-test
selection cannot produce a successful certification.

This design does not duplicate upstream domain logic in the test suite. Typed
clients manage Kubeseer-owned APIs, dynamic clients manage observed built-in
and custom resources, discovery uses the cluster API, and metrics, logs,
Events, status, readiness, and admission responses are observed at their
production endpoints. Direct calls into selection, extraction, typing,
authorization, reconciliation, status, or observability packages are
forbidden in `test/e2e`.

Two upstream interactions are made explicit:

- a Kubeseer already persisted while its targets are allowed is used for the
  runtime namespace- and kind-denial scenarios. The active policy is then
  narrowed. A newly submitted request that is already outside the policy is
  separately expected to be rejected by the installed validating webhook;
- the Kubernetes Event required after a status write is the upstream
  observability Event whose reason is selected from the canonical condition
  outcome. `StatusUpdateWritten` remains the correlated structured-log event
  code and is not treated as a Kubernetes Event reason. The `StatusUpdated`
  wording in `R9.AC5` denotes that successful status-transition Event rather
  than defining a new public reason.

No additional user decision checkpoint is required. The approved
Requirements and upstream contracts already select the owned kind-on-Podman
environment, canonical Helm chart, one centrally pinned supported Kubernetes
member, the default `certManager` profile, public-boundary assertions, and the
division between product scenarios and prerequisite certifications.

## Architecture

```text
make e2e
   |
   v
hack/e2e-harness.sh
   |-- preflight + sanitized environment + run-owned directory
   |-- kind create --provider podman --image <central node image>
   |-- podman build -> kind image load
   |-- pinned cert-manager -> canonical Helm chart -> readiness gates
   |-- explicit kubeconfig and run metadata
   v
existing non-vacuous test-layer runner
   |
   v
test/e2e: TestEndToEnd
   |-- validate certification manifest and scenario registry
   |-- create deterministic fixtures through Kubernetes APIs
   |-- bounded waits on status, metrics, logs, Events, and lifecycle
   |-- emit SCENARIO=<stable-id> STATUS=<outcome>
   v
sanitized diagnostics -> exact owned-cluster cleanup -> worktree comparison
```

The shell/Go boundary is environment based. The harness supplies only
run-owned and immutable metadata: kubeconfig path, cluster name, Kubernetes
version, package identity, source revision, installation namespace, release
name, manager image, and diagnostic directory. The Go process rejects absent,
empty, or inconsistent mandatory values and verifies that the active API
server identity is the run-owned kind cluster before creating fixtures.

The cluster is shared by the ordered scenarios because image and package
installation are part of the certified setup and are expensive to repeat.
Scenarios never run in parallel. Each scenario uses stable logical fixture
names beneath a run-scoped namespace, restores the canonical policy baseline
before and after any administrative mutation, and registers cleanup before
the first write. A failed cleanup is a scenario failure and prevents a false
pass for later scenarios.

The package gate precedes all product scenarios. CRD establishment,
certificate readiness, non-empty webhook CA bundles, webhook traversal,
Deployment availability, direct readiness endpoint success, canonical policy
presence, and metrics endpoint reachability must all pass. Any failure leaves
the scenario registry unexecuted and is reported as an installation failure,
not as a product-scenario skip.

### Run lifecycle

1. Capture the initial repository state with
   `git status --porcelain=v1 --untracked-files=all`, source revision, and a
   content-derived checkout identity. Create one run-owned temporary directory
   containing all Go, Helm, kind, kubeconfig, and diagnostic state.
2. Preflight `go`, `git`, `kind`, `podman`, `kubectl`, `helm`, and the local HTTP
   client before cluster ownership is established. Validate the selected
   Kubernetes version against the centrally declared supported matrix and map
   it to one centrally declared kind node image.
3. Start from a sanitized environment. Unset ambient Kubernetes and cloud
   credential variables, set `KIND_EXPERIMENTAL_PROVIDER=podman`, and pass the
   run-owned kubeconfig explicitly to every Kubernetes, Helm, kind, and test
   operation. There is no existing-cluster branch.
4. Create a run-unique cluster name, such as
   `kubeseer-e2e-<run-identity>`. Once kind creation begins, arm an exact-name
   cleanup trap. The trap never invokes provider-wide prune, network cleanup,
   or wildcard deletion.
5. Build the repository `Dockerfile` with Podman under a run-specific immutable
   tag derived from the source identity and run identity. Capture the resulting
   image identity, load that exact image into every kind node, and configure the
   chart with the supported image values and `IfNotPresent` pull policy.
6. Install the pinned cert-manager prerequisite, wait for its API and workloads,
   then install the local `charts/kubeseer` chart in an explicit release
   namespace. Use the chart's default certificate profile and a committed E2E
   limit profile that is small enough to exercise limits but large enough to
   admit the upstream compact limit status and all ordinary scenarios.
7. Gate the suite on both CRDs becoming Established, manager Deployment
   availability, certificate readiness, non-empty webhook CA bundles, valid and
   invalid dry-run admission requests, the persisted
   `installation-access-ceiling`, the manager `/readyz` endpoint reached by a
   bounded explicit-kubeconfig port-forward, and the metrics endpoint.
8. Invoke the existing test-layer runner for `./test/e2e/...` and exactly
   `^TestEndToEnd$`. The runner retains its compile/list zero-match probe and
   terminal `TEST_LAYER=end-to-end STATUS=passed` contract.
9. On failure, collect and print sanitized diagnostics before teardown. Then
   request deletion of only the exact run-owned cluster. Remove the temporary
   directory only after confirmed deletion; otherwise retain and report the
   kubeconfig path. Finally compare the worktree snapshot byte-for-byte with
   the initial snapshot and fail on drift.

### Scenario execution flow

`TestEndToEnd` first loads the embedded certification manifest and compares it
with the compiled registry. It rejects unknown or duplicate feature names,
unknown prerequisite commands, duplicate scenario IDs, missing mandatory
scenario mappings, registry entries absent from the manifest, manifest entries
without executable functions, and any use of `testing.T.Skip` represented as a
non-run outcome.

Each registered scenario follows the same transaction:

1. establish its namespace, administrative-policy baseline, and fixtures;
2. record observable baselines such as resource versions, metric counters, and
   manager Pod identity;
3. create or mutate public Kubernetes objects;
4. wait with a context deadline for a named observable predicate;
5. assert exact public status, provenance, conditions, summary, hash,
   admission result, metrics, structured records, or Events as applicable;
6. report the stable identifier and outcome, collect sanitized diagnostics on
   failure, and restore any global state.

Watch-based observation is preferred when the API exposes a resource version.
Bounded polling is used for Prometheus exposition, logs, port-forwarded HTTP
endpoints, and conditions without a reliable watch. No readiness or semantic
assertion uses an arbitrary sleep as its primary mechanism. A deliberately
bounded quiet window is allowed only when proving absence of a write or Event;
it begins after the causative update has been observed by a correlated manager
record and compares resource version, metrics, and Events throughout the
window.

## Options Considered

### Option A: Shell lifecycle with a Go public-boundary suite

- Summary: Extend the existing shell harness for host and cluster lifecycle,
  then run one Go suite against the installed API.
- Why chosen: It preserves the approved test-layer runner and kind ownership
  boundary while giving scenario assertions typed Kubernetes objects,
  contexts, watches, deterministic comparison, and precise failure output.

### Option B: Shell-only kubectl scenarios

- Summary: Express fixtures, waits, and assertions entirely as shell and
  `kubectl` commands.
- Why rejected: JSON status, typed results, provenance, hashes, condition
  transitions, metric deltas, and no-op resource-version assertions become
  brittle text processing. Cleanup and scenario traceability also become harder
  to make composable and deterministic.

### Option C: Let Go own kind, Podman, Helm, and scenarios

- Summary: Put cluster and external-process lifecycle in `TestMain` or a Go
  helper package.
- Why rejected: It would duplicate the approved shell harness, obscure the
  exact cleanup boundary, and couple cluster provisioning failures to Go test
  discovery. The current harness already has acceptance coverage for
  preflight, ambient-state isolation, non-vacuity, and cleanup.

### Option D: Reuse envtest or an existing cluster

- Summary: Run scenarios against envtest, ambient kubeconfig, or an optional
  pre-existing Kubernetes environment.
- Why rejected: Envtest does not exercise the production image, Pod,
  certificate profile, networked webhook, or Helm package. An existing-cluster
  path violates owned-cluster isolation and creates an unsafe fallback.

## Simplicity And Elegance Review

- Simplest viable shape: One existing Make target, one lifecycle script, one Go
  test entry point, one declarative certification manifest, and a small fixture
  tree are sufficient. No E2E framework, custom report service, registry,
  operator test mode, or production test hook is introduced.
- Coupling check: The shell layer knows how to provision and install but not how
  Kubeseer results are computed. The Go layer knows public API schemas and
  observable contracts but not how kind or Helm are implemented. Product code
  remains unaware of the E2E suite.
- Reuse check: Existing test-layer non-vacuity, chart values, central version
  declarations, production Dockerfile, status types, stable reasons, and
  observability vocabulary are consumed rather than copied into new logic.
- Determinism check: The registry fixes execution order, fixtures use explicit
  identities, assertions normalize only API-defined nondeterministic metadata,
  and all asynchronous work names a deadline and predicate.
- Future-proofing: Full Kubernetes/certificate matrices, reusable local-up and
  local-down UX, external observability backends, upgrades, load, and published
  artifacts remain in their owning features. The manifest can add scenarios
  without changing the runner protocol.

## Components And Interfaces

### E2E lifecycle harness

- Purpose: Own host preflight, environment isolation, exact cluster lifecycle,
  image build/load, package installation, suite invocation, diagnostics, and
  teardown.
- Inputs: Go package pattern and exact test regex from `make e2e`; centrally
  declared Kubernetes, kind-node, and cert-manager versions; optional
  non-secret timeout overrides bounded by documented defaults.
- Outputs: Run metadata environment, stable phase diagnostics, exact process
  exit status, and the existing terminal test-layer marker.
- Dependencies: `hack/e2e-harness.sh`, the existing test-layer runner, kind,
  Podman, kubectl, Helm, Go, Git, and a local HTTP client.
- Invariants: No ambient kubeconfig or cloud credential use; no external
  cluster fallback; no scenario before readiness; exact owned-target cleanup;
  no repository writes.
- Requirements: `R1`, `R2`, `R3`, `R10`, `NFR1`, `NFR2`, `NFR4`, `NFR6`,
  `NFR8`, `NFR9`.

### Certification manifest and scenario registry

- Purpose: Make the meaning of a passing release certification reviewable and
  mechanically complete.
- Inputs/Outputs: A version-controlled `test/e2e/certification.json` declares
  release-scope feature names, mandatory scenario keys from
  `SPECIFICATIONS.md`, stable scenario IDs, and prerequisite certification
  commands. `test/e2e/scenarios.go` supplies exactly one function for every
  executable ID. Validation produces an ordered execution plan or a complete
  deterministic defect list.
- Dependencies: Go embedding and standard JSON decoding; no production
  package.
- Invariants: IDs are immutable once published; manifest order is report order;
  mappings are many-to-many; every registry function is selected; no optional
  or skipped required entry exists.
- Requirements: `R1`, `R10`, `NFR3`, `NFR5`, `NFR7`, `NFR9`.

### Cluster session

- Purpose: Construct Kubernetes clients from only the run-owned kubeconfig and
  carry immutable run metadata into scenarios.
- Interface: `newClusterSession(context.Context, RunMetadata)` returns typed,
  dynamic, discovery, REST, and port-forward clients plus bounded helper
  methods. It verifies current context, API server identity, installed API
  resources, package identity, and release namespace before returning.
- Dependencies: Kubernetes client-go APIs already used by the repository.
- Invariants: No `clientcmd` default loading rules, ambient context, fake
  clients, or calls into production domain packages.
- Requirements: `R2`, `R3`, `R4`, `R7`, `R8`, `R9`, `NFR1`, `NFR2`, `NFR7`.

### Fixture builder

- Purpose: Create deterministic, run-scoped built-in resources, a fixture CRD
  and Custom Resources, policies, and Kubeseer objects.
- Inputs/Outputs: Stable fixture declarations and a scenario namespace in;
  created object identities and registered idempotent cleanup callbacks out.
- Dependencies: Typed and dynamic Kubernetes clients. Static CRD manifests and
  schema-valid fixture fragments live under `test/e2e/fixtures`.
- Invariants: No Secret is required for value assertions. A sentinel Secret may
  be created solely to prove diagnostics exclude its payload, and its value is
  never selected by Kubeseer. Fixture names contain the run/scenario identity,
  labels are explicit, and creation order is deliberately non-canonical where
  order normalization is asserted.
- Requirements: `R4`, `R5`, `R6`, `R7`, `R8`, `R10`, `NFR2`, `NFR3`.

### Bounded observer

- Purpose: Wait for one named public condition and return the latest observed
  snapshot without sleeping blindly.
- Interface: Helpers accept a context deadline, stable scenario ID, awaited
  condition name, and predicate. Status helpers watch from a captured resource
  version and recover from expired watches by relisting. HTTP and log helpers
  poll with bounded backoff. Quiet-window helpers verify absence only after a
  positive causation signal.
- Outputs: A successful typed snapshot or an error containing scenario ID,
  awaited condition, deadline, last sanitized state, and last stable reason.
- Requirements: `R3`, `R4`, `R5`, `R6`, `R7`, `R8`, `R9`, `R10`, `NFR3`,
  `NFR4`, `NFR8`.

### Public assertion library

- Purpose: Compare API-visible semantic contracts without reproducing product
  algorithms.
- Inputs/Outputs: Expected literal fixture values and API responses in; precise
  diffs over conditions, summaries, hashes, typed results, provenance,
  admission status, metrics, records, and Events out.
- Dependencies: Public `api/v1alpha1` types and Kubernetes API types only.
- Invariants: Expected aggregates and transformed values are literal outcomes
  computed from small committed fixtures, not by importing production
  aggregation or operator code. Ordering is asserted, not normalized away.
- Requirements: `R4`, `R5`, `R6`, `R7`, `R8`, `R9`, `NFR3`, `NFR7`.

### Sanitized diagnostic collector

- Purpose: Make package, readiness, timeout, and product failures actionable
  without leaking observed values or credentials.
- Inputs: Stable scenario/phase identity, Kubeseer object names, manager Pod
  identity, metric endpoint, and Events API.
- Outputs: An allowlisted projection containing source revision, package and
  Kubernetes identity, object generation, canonical conditions, result
  summary, result hash, stable sanitized error reasons, selected metric
  families, structured event/outcome/reason/correlation fields, and sanitized
  Event type/reason.
- Sanitization: Result bodies, extracted/typed values, Secret bodies, kubeconfig
  content, tokens, raw admission bodies, arbitrary log messages, and unknown
  structured fields are omitted. Committed forbidden sentinels are scanned in
  the collected output; any match fails the run.
- Requirements: `R2`, `R3`, `R7`, `R9`, `R10`, `NFR1`, `NFR4`, `NFR8`.

## Certification Scenario Set

| Stable ID | Executable scenario | Principal evidence |
| --- | --- | --- |
| `E2E-001` | Package bootstrap and admission | Production image, chart, default certificate profile, readiness, policy singleton, webhook traversal |
| `E2E-002` | Same-namespace Deployment | Exact-name discovery, selection, provenance, canonical status |
| `E2E-003` | Multi-namespace Pods | Authorized cross-namespace selection, equal-name provenance, deterministic order |
| `E2E-004` | Custom Resource and labels | Fixture CRD discovery and label filtering |
| `E2E-005` | Extraction, typing, and operators | Scalar JSONPath, list JSONPath, logical typing, predicate omission, transformation, sibling preservation |
| `E2E-006` | Numeric aggregation | Cross-namespace contributions, groups, provenance, partial degraded aggregate |
| `E2E-007` | Missing type and partial degradation | Stable discovery failure with successful sibling result |
| `E2E-008` | Authorization denial after policy drift | Namespace and kind revocation, protected-value removal, stable denied status; new forbidden writes remain admission-rejected |
| `E2E-009` | Resource and status limits | Source-atomic matched-resource failure and compact oversized-status outcome without truncation |
| `E2E-010` | Source update and semantic no-op | Changed result, unchanged-result write suppression, resource-version/Event/metric evidence |
| `E2E-011` | Source deletion and policy restriction | Contribution removal and current authorization condition within deadlines |
| `E2E-012` | Manager restart | Pod replacement, readiness recovery, existing-object result recovery |
| `E2E-013` | Kubeseer deletion | No finalizer dependency, observed-resource preservation, manager health; process-local route removal mapped to prerequisite proof |
| `E2E-014` | Overlapping instances | Independent results and failure isolation for shared observed resources |
| `E2E-015` | Status and observability correlation | Complete conditions, summary, hash, metric delta, status Event, authorization record, and attempt correlation |

The twenty minimum scenarios in `SPECIFICATIONS.md` map as follows:

| Minimum scenario | Stable ID |
| --- | --- |
| Deployment in the Kubeseer namespace | `E2E-002` |
| Pods across multiple namespaces | `E2E-003` |
| Custom Resource | `E2E-004` |
| Label selection | `E2E-004` |
| Scalar JSONPath | `E2E-005` |
| List JSONPath | `E2E-005` |
| Typed values | `E2E-005` |
| Numeric aggregation | `E2E-006` |
| Partial degraded result | `E2E-006`, `E2E-007` |
| Forbidden namespace | `E2E-008` |
| Forbidden kind | `E2E-008` |
| Missing resource type or CRD | `E2E-007` |
| Source update | `E2E-010` |
| Semantic no-op | `E2E-010` |
| Source deletion | `E2E-011` |
| Policy restriction | `E2E-011` |
| Operator restart | `E2E-012` |
| Oversized output | `E2E-009` |
| Kubeseer deletion | `E2E-013` |
| Overlapping instances | `E2E-014` |

The release-scope feature mapping stored in the manifest uses these executable
IDs and, only where the behavior is not safely inducible through a production
cluster, these prerequisite certifications:

| In-scope feature | Certification mapping |
| --- | --- |
| `kubeseer-api-foundation` | `E2E-001` through public CRDs and status; approved API compatibility certification for the full version matrix |
| `integration-testing-foundation` | `E2E-001` plus the harness acceptance certification |
| `resource-discovery` | `E2E-002`, `E2E-003`, `E2E-004`, `E2E-007` |
| `installation-access-policy` | `E2E-001`, `E2E-008`, `E2E-011` |
| `resource-selection` | `E2E-002`, `E2E-003`, `E2E-004`; approved integration certification for forced API-list permutations |
| `field-extraction` | `E2E-005` |
| `typed-output-model` | `E2E-005`, `E2E-015` |
| `reconciliation-runtime` | `E2E-010` through `E2E-014`; approved route-registry integration certification for process-local removal |
| `status-and-conditions` | `E2E-002`, `E2E-007` through `E2E-015` |
| `authorization-enforcement` | `E2E-008`, `E2E-011`, `E2E-015` |
| `value-operators` | `E2E-005` |
| `cross-namespace-aggregation` | `E2E-006` |
| `admission-validation` | `E2E-001`, `E2E-008` |
| `observability` | `E2E-010`, `E2E-015`; approved integration certification for injected sink failure |
| `performance-and-limits` | `E2E-009` |
| `packaging-and-installation` | `E2E-001`; approved package compatibility certification for every Kubernetes and certificate-profile matrix member |

Prerequisite entries contain a stable certification name and repository
command, not a prose assertion. Manifest validation rejects a prerequisite
whose named command is absent from the repository's approved entry points.

## Data Models

### Run metadata

`RunMetadata` is immutable after setup and contains the run identity, source
revision and checkout identity, cluster name, explicit kubeconfig path and
context, Kubernetes version and node image, package release name/namespace,
manager image reference and image identity, diagnostic directory, and bounded
deadlines. It contains paths and identities but never kubeconfig bytes,
credentials, tokens, or environment dumps.

### Certification manifest

The JSON document has three ordered collections:

- `features`: exact approved feature names with executable scenario IDs and/or
  prerequisite certification names;
- `minimumScenarios`: exact stable keys from the product specification with one
  or more executable IDs;
- `scenarios`: stable ID, human-readable name, and registry key.

The schema is decoded with unknown-field rejection. Set equality checks bind
the manifest to compile-time expected release feature names and mandatory
minimum-scenario keys. Adding, renaming, or removing a required feature or
scenario therefore requires an intentional manifest and test change.

### Scenario state

`Scenario` contains only stable metadata and a function accepting a context,
`testing.T`, and `ClusterSession`. Mutable resources are represented by
`FixtureSet`, which records created GroupVersionResources, namespaced names,
UIDs, and cleanup order. It is never serialized and carries no extracted
values after the scenario returns.

### Observable snapshot

`Snapshot` holds the public Kubeseer generation, resource version, canonical
conditions, summary, result hash, optional result for in-memory assertion,
selected metric samples, Event identities, and allowlisted structured records.
The diagnostic projection deliberately drops `Snapshot.Result`; successful
assertions may inspect it in memory but never print it wholesale.

No E2E state, report, generated manifest, cache, or golden result is written to
the repository. All transient state lives in the run-owned temporary
directory.

## Error Handling

- Preflight reports every missing or incompatible prerequisite in a stable
  list and exits before kind creation. It does not partially create a cluster.
- After cluster creation begins, one trap owns teardown for success, ordinary
  failure, signal termination, and partial installation. The original failure
  remains primary; cleanup failure is appended and forces a non-zero result.
- Installation commands have phase-specific deadlines. Diagnostics identify
  image build, image load, cert-manager, Helm rendering/install, CRD,
  Deployment, webhook, policy, readiness, or metrics as the failed phase.
- Scenario setup and cleanup are idempotent. Kubernetes NotFound is accepted
  only during deletion; AlreadyExists is accepted only after UID and fixture
  labels prove ownership by the current run.
- Watches start from captured resource versions, handle closure and expired
  resource versions by bounded relist, and never retry past the parent
  deadline. API throttling uses bounded client-go backoff.
- Each failure includes `SCENARIO=<id>`, the named awaited condition or failed
  assertion, the last sanitized observation, and the remaining cleanup result.
  Raw `error.Error()` text from downstream Kubernetes bodies is not copied into
  the final diagnostic unless it passes an explicit safe classifier.
- A subtest failure does not silently skip later registry entries. The parent
  records every entry as passed, failed, or blocked-by-installation. Any outcome
  other than passed for an executable required scenario fails the suite; a
  blocked installation cannot produce scenario-level success markers.
- Policy-mutating scenarios restore the baseline with a bounded confirmation.
  If restoration fails, the suite stops product execution because later
  authorization assertions would no longer have a valid baseline.

## Security Considerations

- Every Kubernetes and Helm operation receives the owned kubeconfig explicitly.
  Default client loading and ambient context selection are prohibited.
- The harness removes ambient Kubernetes and common cloud credential variables
  from child process environments. No command targets a cloud API or external
  Kubernetes endpoint.
- The manager ServiceAccount and logical `installation-access-ceiling` remain
  separate. Denial scenarios deliberately leave RBAC broad enough that a
  denied result proves logical fail-closed enforcement rather than an
  incidental RBAC error.
- Runtime denial fixtures are admitted while allowed and then revoked by a
  valid policy narrowing. This respects the webhook contract while proving
  that stale admission cannot authorize a later read or retain a protected
  value.
- New Kubeseer requests already outside the active policy are submitted with
  server-side dry-run and must receive the approved forbidden admission class;
  they are never persisted.
- Test fixtures avoid Secrets for functional outcomes. A random committed-name
  sentinel payload may be placed in an unrelated Secret to verify that logs,
  Events, and diagnostics never contain it. Kubeconfig and ServiceAccount token
  data are never collected.
- Diagnostics are allowlist projections. Full Kubeseer results, observed
  resource bodies, extracted values, typed values, selector operands, Secret
  content, credentials, and raw downstream bodies are excluded even when they
  would make a failure easier to inspect.
- Cleanup uses the exact run-owned kind name and kubeconfig. It never runs
  `podman system prune`, deletes Podman networks, enumerates arbitrary
  containers for removal, or deletes any external Kubernetes resource.

## Failure Modes And Tradeoffs

| Failure mode | Mitigation | Tradeoff |
| --- | --- | --- |
| A prerequisite is missing or a selected version has no node-image mapping | Complete preflight before cluster creation | First-time setup can fail early and requires the contributor to install the named tool |
| Image or prerequisite pulls are unavailable | Pinned identities, phase deadline, actionable build/install phase | The first local run may require network access as allowed by the Requirements |
| A concurrent E2E run collides with this run | Run-unique cluster, namespaces, image tag, release identity, and temporary paths | Diagnostics are less predictable than one fixed cluster name but cleanup is safer |
| Helm reports success while product endpoints are not ready | Independent CRD, Deployment, CA, admission, `/readyz`, policy, and metrics gates | Setup is longer but package success is non-vacuous |
| A policy-denied object cannot be created because admission is fail-closed | Persist while allowed, narrow policy, then assert runtime invalidation; separately assert forbidden admission | The negative scenario has two phases but matches the approved admission/runtime composition |
| Kubernetes does not expose controllable API list order | Create fixtures non-canonically and assert canonical output; bind forced permutations to approved integration certification | The cluster suite proves the product boundary while the focused proof controls the otherwise inaccessible permutation |
| Process-local route removal cannot be directly inspected | Assert public deletion, no finalizer dependency, source preservation, and manager health; bind exact registry removal to approved integration certification | Internal memory shape remains outside the E2E API contract |
| Observability sink failure cannot be injected into the installed production manager | Bind passive-sink failure isolation to approved integration certification and verify all real production signals in cluster | No production test hook or special image is introduced |
| Status limit is configured below its compact failure shape | Commit one known-valid E2E profile and gate manager readiness before scenarios | The test uses a fixed small ceiling rather than dynamically discovering an internal minimum |
| A semantic no-op is mistaken for inactivity | Wait for the correlated reconciliation completion, then verify unchanged status resource version, status-write metric, and Event count through a bounded quiet window | Absence proof takes a small bounded interval |
| Diagnostics themselves contain protected values | Allowlist fields, omit result bodies, scan forbidden sentinels, and fail closed | Some low-level failure detail is intentionally unavailable |
| Cluster deletion fails | Retain and print the exact kubeconfig path; never broaden cleanup | Manual exact-target cleanup may be required |
| A scenario contaminates global policy or fixtures | Serial execution, pre-scenario baseline verification, registered cleanup, and stop on baseline-restoration failure | The suite does not parallelize scenarios |
| Worktree state changes during execution | Snapshot before setup, redirect caches and artifacts, compare after cleanup | Concurrent intentional repository edits cause the E2E run to fail rather than being ignored |

## Testing Strategy

No dedicated package-local unit-test layer is added.

### Harness acceptance

Extend `hack/e2e-harness-acceptance.sh` with stubbed external executables to
prove complete preflight, sanitized environment, central node-image selection,
run-unique exact-name lifecycle, image build/load wiring, explicit kubeconfig
on Kubernetes and Helm commands, installation gating, diagnostic ordering,
cleanup success/failure, retained kubeconfig reporting, non-vacuous test
selection, terminal marker propagation, and byte-for-byte worktree comparison.
The acceptance harness must prove that no Podman prune/network command and no
existing-cluster fallback can be reached.

### End-to-end suite

`TestEndToEnd` is the only top-level E2E test and contains the fifteen ordered
stable subtests. It runs against the real API server, installed validating
webhook, production manager Pod, status subresource, metrics endpoint, manager
logs, and Kubernetes Events. It covers all minimum scenarios and the
release-scope composition listed above.

Assertions include:

- exact selection, labels, scope, provenance, and deterministic order for
  built-in and custom resources;
- scalar and list extraction, logical types, predicate and transformation
  behavior, sanitized field failures, and sibling preservation;
- exact grouped numeric aggregates, contributor provenance, and degraded
  contribution behavior;
- admission denial plus runtime policy invalidation for namespace and kind,
  with no protected values in status or diagnostics;
- source-atomic resource-limit failure and compact status-limit publication
  without truncation or oversized candidate retention;
- changed updates, semantic no-op write suppression, source deletion, policy
  restriction, manager restart, Kubeseer deletion, and overlapping-instance
  isolation;
- complete current-generation conditions, deterministic summary/hash,
  bounded-cardinality metric deltas, canonical status Events, sanitized
  authorization records, and correlated reconciliation start/terminal logs.

### Prerequisite certification

The manifest names, and verification confirms, the current approved commands
for full API compatibility, package Kubernetes/certificate matrices, harness
acceptance, forced list-order permutations, process-local route cleanup, and
observability sink-failure isolation. E2E does not rerun every prerequisite
command on each local invocation; it makes those boundaries explicit so a
release process can require both current prerequisite evidence and the cluster
suite without pretending an unobservable internal branch was induced in the
installed binary.

## Verification Plan

- Specification integrity: `walden validate end-to-end-scenarios --json` must
  report a valid draft/review, complete requirement coverage, fresh approved
  Requirements, and no schema defect.
- Harness contract: `./hack/e2e-harness-acceptance.sh` must exercise success and
  failure fixtures and emit its stable acceptance marker while proving exact
  cluster ownership, preflight-before-create, explicit kubeconfig, cleanup, and
  worktree preservation.
- Repository verification: `make verify` must confirm generated artifacts,
  formatting, package synchronization, and checked-in manifests remain
  current. `git diff --check` must report no whitespace error.
- Non-vacuity: the existing layer runner's compile/list probe must find exactly
  `TestEndToEnd` for `./test/e2e/...`; an intentionally unmatched pattern in
  harness acceptance must fail.
- Cluster certification: `make e2e` must build/load the current production
  image, pass every `E2E-001` through `E2E-015` subtest, print the source,
  Kubernetes, and package identities, report each stable scenario result, and
  terminate with `TEST_LAYER=end-to-end STATUS=passed`.
- Security evidence: denial scenarios compare public status and diagnostics
  against forbidden fixture sentinels; harness acceptance proves ambient
  credentials and unrelated Podman cleanup are unreachable.
- Lifecycle evidence: captured resource versions, Pod UIDs, status-write metric
  deltas, Events, and correlated records prove update, no-op, delete, policy,
  restart, and overlap outcomes within named deadlines.
- Cleanup evidence: success and failure paths compare the final repository
  status snapshot with the initial snapshot. Successful kind deletion removes
  the run directory; simulated deletion failure reports and retains the exact
  kubeconfig.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Certification manifest, scenario registry, stable report protocol, prerequisite mappings |
| `R2` | Lifecycle harness, sanitized environment, exact owned-cluster cleanup, harness acceptance |
| `R3` | Image build/load, canonical Helm installation, certificate/admission/readiness package gate, `E2E-001` |
| `R4` | Fixture builder and `E2E-002` through `E2E-004`, `E2E-007` |
| `R5` | Public assertion library and `E2E-005` |
| `R6` | Cross-namespace fixtures and `E2E-006` |
| `R7` | Policy-drift denial and bounded-output assertions in `E2E-007` through `E2E-009` |
| `R8` | Bounded lifecycle observers and `E2E-010` through `E2E-014`, route-cleanup prerequisite |
| `R9` | Status/metrics/log/Event snapshots in `E2E-010`, `E2E-015`, sink-failure prerequisite |
| `R10` | Version-controlled fixtures, named bounded waits, diagnostics, common local/CI entry point, worktree comparison |
| `NFR1` | Explicit credentials, logical-policy/RBAC separation, fail-closed drift scenario, allowlist diagnostics |
| `NFR2` | Run-owned cluster, namespaces, temporary state, exact cleanup |
| `NFR3` | Ordered manifest/registry, literal fixtures, semantic comparisons, stable scenario reports |
| `NFR4` | Phase gates, deadlines, idempotent cleanup, restart and policy recovery evidence |
| `NFR5` | Mechanical feature/minimum-scenario coverage and immutable stable identifiers |
| `NFR6` | Central supported-version/node-image selection and explicit tested-version report |
| `NFR7` | Real API server, installed production image, webhook, manager, and status subresource; no doubles or domain calls |
| `NFR8` | Context deadlines, watch/poll helpers, named timeout diagnostics |
| `NFR9` | Same `make e2e` entry point and declared identities for local and CI execution |
