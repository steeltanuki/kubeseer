---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-27T10:44:01Z
last_modified: 2026-08-27T10:44:01Z
approved_fingerprint: sha256:3e8e73f7fcbc3691b15f488e263fd3cae5b50b80a2886bf3cfba5bc3b77cdaef
source_requirements_approved_at: 2026-08-27T10:06:29Z
source_requirements_fingerprint: sha256:938416c4cd20919d2a8a3bd1abd3379b056a0de06d5eb872fa47b042310bf23e
---

# Feature Design

## Overview

Implement `authorization-enforcement` as a narrow internal capability layer
between pure installation-policy evaluation and every observed-resource I/O
adapter. A new `internal/authorization` package owns the process-local subject,
sanitized decision evidence, opaque capability, and freshness interfaces. It
does not interpret policy rules: `internal/accesspolicy` remains the only
policy compiler and evaluator.

The existing selection and reconciliation modules become capability consumers:

- selection binds one capability to every exact target before constructing an
  executable source plan;
- the dynamic LIST adapter receives an opaque authorized target rather than a
  raw target and checks freshness immediately before every page request;
- the route registry converts current authorized route bindings into a private
  WATCH permit before every watch start or restart;
- policy, Kubeseer identity, generation, deletion, and cancellation events
  invalidate the same process-local subject used by LIST, WATCH, and status;
- the status pipeline keeps the existing public reasons and structural result
  contract unchanged.

`accesspolicy.Snapshot` is enriched only with the canonical policy identity
needed for decision evidence. No public API field, CRD schema, condition type,
reason code, controller, webhook, RBAC object, or durable audit store is added.

<!-- assumed: one new internal package is preferable to placing lifecycle and audit concerns in the pure policy evaluator or selection executor (source: approved architecture boundaries and current package graph) -->
<!-- assumed: capability freshness is checked immediately before each network call and the same cancellable reconciliation or supervisor context is passed to Kubernetes, so a concurrent invalidation cancels an already-issued request and blocks every later request (source: approved reconciliation-runtime freshness model) -->
<!-- assumed: the authorization evidence recorder is a non-failing in-process observer; persistence and delivery guarantees are deferred rather than allowed to influence an authorization decision (source: requirements C7 and observability boundary) -->
<!-- assumed: a shared metadata WATCH may continue while at least one exact current capability authorizes its address, because the transport retains no object contents and routes events only to current bound owners (source: approved reconciliation-runtime shared-route design) -->

## Architecture

```text
Kubeseer + FreshnessTracker lease
               |
               v
selection.Planner --------> exact ReadTarget values
               |                    |
               |                    v
               |          accesspolicy.Request
               |                    |
               v                    v
accesspolicy.Load ------> immutable Snapshot + PolicyIdentity
                                    |
                                    v
                    authorization.Enforcer.EvaluateBatch
                         | outcomes + ordered records
                         | allowed targets mint capabilities
                         v
                  selection.BindCapabilities
                         |
                         +----> AuthorizedPlan
                         |         |
                         |         v
                         |    AuthorizedRead --fresh check--> dynamic LIST
                         |
                         +----> AuthorizedRoute
                                   |
                                   v
                              WatchPermit --fresh check--> metadata WATCH

policy/identity change --> tracker invalidation + context cancellation
                       --> route removal + enqueue-all
                       --> stale LIST/WATCH/status paths stop

selection outcomes --> existing extraction/typing/status composition
authorization records --> AuthorizationRecorderPort (process-local only)
```

The dependency direction remains acyclic:

```text
accesspolicy <- authorization <- selection <- reconciliation
                       ^                         |
                       +-------------------------+
```

`authorization` imports the pure policy contract and Kubernetes identity
types. It does not import selection or reconciliation. Selection converts its
own `ReadTarget` to an `accesspolicy.Request`; reconciliation converts its
existing `Lease` to an authorization `Subject` and implements freshness
verification through `FreshnessTracker`.

### Existing Boundaries Retained

| Boundary | Retained responsibility |
| --- | --- |
| `internal/accesspolicy` | Load, validate, compile, and evaluate the installation ceiling with stable reasons |
| `internal/discovery` | Supply canonical GVR and namespaced or cluster scope |
| `internal/selection` | Plan selectors, bind exact targets, LIST pages, and preserve source atomicity |
| `internal/reconciliation` | Own current identity, generation, policy epoch, event cancellation, routing, retries, and publication |
| `internal/status` | Compose existing authorization conditions and sanitized result errors |

### New Enforcement Responsibility

The new package converts a policy decision into a capability only when all of
these facts agree:

1. the authorization subject is complete;
2. the request is the exact normalized target supplied by selection;
3. the decision is `Allowed/Allowed` from the current snapshot;
4. one deterministic decision record has been emitted for the target;
5. later consumers can verify the subject against current runtime state.

Denied, unavailable, missing, mismatched, duplicate, or extra outcomes retain
sanitized evidence but never expose a capability.

## Options Considered

### Option A: Dedicated Capability And Evidence Boundary

- Summary: Add `internal/authorization` for subject identity, policy outcome
  evidence, opaque capabilities, freshness verification, and a non-failing
  recorder port. Refactor LIST and WATCH adapters to accept private permits.
- Why chosen: It makes the security property structural at the I/O boundary,
  keeps policy semantics pure, avoids package cycles, and gives later
  observability one sanitized record contract.

### Option B: Continue Passing Raw Decisions Through Existing Modules

- Summary: Extend the current `selection.Authorization` and
  `reconciliation.AuthorizedRoute` structs with owner and epoch fields while
  leaving raw `ReadTarget` and `WatchAddress` accepted by I/O adapters.
- Why rejected: This is a smaller diff but leaves bypassable raw adapter
  signatures, duplicates capability validation in selection and routing, and
  has no single evidence contract linking policy decisions to later RBAC
  failures.

### Option C: Reload And Re-evaluate Policy Before Every Network Request

- Summary: Call the policy loader before every LIST page and WATCH restart.
- Why rejected: It adds Kubernetes reads to pagination and watch backoff,
  creates multiple policy snapshots inside one source execution, complicates
  source atomicity, and still needs identity/generation guards before status.
  The existing policy epoch plus cancellable context provides a simpler
  currentness boundary.

## Simplicity And Elegance Review

- Simplest viable shape: One new internal package, one policy-identity
  enrichment, and private LIST/WATCH permit types. Existing policy rules,
  planner, tracker, route event handling, result building, and status rendering
  remain authoritative.
- Coupling check: `authorization` knows neither selection plans nor
  reconciliation state. Consumers depend on its small value and interface
  contracts; `FreshnessTracker` is adapted through `Verifier` without moving
  controller lifecycle concerns downward.
- First-draft challenge: A process-local evidence journal was considered, then
  removed. Requirements need deterministic evidence exposure, not retention;
  retaining latest or historical records would add cleanup, bounds, and query
  semantics that belong to observability.
- Future-proofing: New observed-resource verbs can accept the same capability
  type. A later observability adapter can consume the recorder port without
  changing authorization, while durable history and delivery guarantees remain
  deferred.

## Components And Interfaces

### Policy Snapshot Identity

- Purpose: Preserve the identity of the policy object that produced a snapshot
  without changing policy evaluation semantics.
- Inputs/Outputs: `accesspolicy.Load` returns a `Snapshot` that exposes
  `PolicyIdentity() (PolicyIdentity, bool)`. The identity contains canonical
  name, UID, and generation. Missing or unavailable snapshots have no identity;
  invalid present objects retain their identity.
- Dependencies: Existing `KubeseerAccessPolicy` loader/compiler and
  `metav1.Object` identity fields.
- Requirements: `R1`, `R2`, `R7.AC2`-`R7.AC4`, `NFR4`, `C2`.

Policy identity is diagnostic context only. It never participates in namespace,
resource, or scope allowlist evaluation and cannot turn a terminal snapshot
into an allowed snapshot.

### Authorization Enforcer

- Purpose: Evaluate exact requests in deterministic order, produce sanitized
  records, and mint capabilities only for current allowed outcomes.
- Inputs/Outputs:

  ```go
  type Enforcer struct {
      recorder Recorder
  }

  func (e *Enforcer) EvaluateBatch(
      ctx context.Context,
      subject Subject,
      snapshot accesspolicy.Snapshot,
      requests []accesspolicy.Request,
  ) (Batch, error)
  ```

  `Batch.Outcomes()` returns one ordered outcome per request. Every outcome
  contains a defensive record and policy decision; only an allowed outcome can
  return a `Capability`.
- Dependencies: Pure `Snapshot.Evaluate`, authorization `Recorder`, and
  Kubernetes identity value types.
- Requirements: `R1`, `R2`, `R3.AC1`-`R3.AC6`, `R6`, `R7`, `NFR1`, `NFR3`,
  `NFR4`.

The enforcer validates the subject before evaluation. An incomplete subject is
an internal fail-closed error, emits no capability, and is mapped by
reconciliation to a sanitized non-publishable or unavailable path rather than
to an allow.

### Authorization Evidence Recorder

- Purpose: Expose complete ordered decision records before I/O and enforcement
  records when Kubernetes RBAC rejects a permitted request.
- Interface:

  ```go
  type Recorder interface {
      Record(context.Context, []Record)
  }

  type RecorderFunc func(context.Context, []Record)
  ```

- Semantics: The method has no error return. Recorder availability cannot
  broaden access, change a deny into an allow, or make authorization depend on
  durable storage. Batches are defensively copied before observation.
- Production composition: Reconciliation setup accepts an optional
  `authorization.Recorder`; nil normalizes explicitly to
  `authorization.NoopRecorder`. `NewRuntime` receives a non-nil `Enforcer`,
  while integration composition injects a capturing recorder to prove order
  and sanitization. No recorder retains history in this feature.
- Dependencies: None beyond context and immutable record values.
- Requirements: `R7`, `NFR3`, `NFR4`, `C6`, `C7`.

The recorder contract deliberately excludes messages and arbitrary key-value
payloads. Later observability may translate records into logs, metrics, or a
durable sink under its own approved contract.

### Selection Capability Binding

- Purpose: Require complete exact authorization for one source before any of
  its targets can execute.
- Inputs/Outputs: `selection.BindCapabilities(plan, batch.Outcomes())` replaces
  raw decision binding. It verifies one outcome per `RequestForTarget`, rejects
  missing, denied, mismatched, duplicate, and extra outcomes, and returns an
  `AuthorizedPlan` whose private targets each pair one `ReadTarget` with one
  capability.
- Dependencies: Existing immutable `SelectionPlan`,
  `selection.RequestForTarget`, and authorization outcomes.
- Requirements: `R1.AC2`-`R1.AC7`, `R3`, `R4.AC7`, `R4.AC9`, `R4.AC10`,
  `NFR1`, `NFR5`.

The old raw-decision `Bind` path is removed rather than retained as a
compatibility bypass. `AuthorizedPlan.Plan()` may continue returning a
defensive diagnostic copy, but no executor accepts that raw plan.

### Authorized LIST Adapter

- Purpose: Make the capability requirement explicit at the nearest production
  resource-instance I/O boundary.
- Shape:

  ```go
  type AuthorizedRead struct {
      target     ReadTarget
      capability authorization.Capability
  }

  type ResourceLister interface {
      List(context.Context, AuthorizedRead, metav1.ListOptions) (
          *unstructured.UnstructuredList,
          error,
      )
  }
  ```

- Freshness: `DynamicResourceLister` receives an authorization `Verifier`.
  Before each initial page, continuation page, or expiration restart it checks
  context cancellation, capability-to-target equality, and subject currentness.
  A missing verifier fails setup or returns a no-read authorization error.
- Forbidden handling: If Kubernetes returns Forbidden, the capability emits one
  `ReadForbidden` enforcement record before selection returns the existing
  sanitized source error.
- Dependencies: Dynamic client, existing selector and pagination behavior, and
  authorization verifier.
- Requirements: `R3.AC10`, `R4.AC1`-`R4.AC3`, `R4.AC6`, `R4.AC7`, `R6.AC3`-
  `R6.AC5`, `R6.AC9`, `R7.AC6`, `R8.AC7`, `NFR1`, `NFR5`.

Pagination remains source-atomic. A stale check returns before the next network
call; the reconciliation context and tracker prevent its failure from
publishing an obsolete candidate.

### Authorized Route And WATCH Permit

- Purpose: Preserve shared metadata-watch efficiency while requiring current
  owner capabilities for every transport start and restart.
- Inputs/Outputs: `NewAuthorizedRouteForLease` is replaced by construction from
  the exact target and its capability. `RouteBinding` retains the full
  authorization subject, source ID, and watch address. Raw allowed decisions
  cannot construct a route.
- Registry changes: The target index retains exact bindings rather than only
  owner counts. Before every supervisor call to `MetadataWatcher.Watch`, the
  registry removes stale bindings, requires at least one current binding for
  the address, and creates a private `WatchPermit`.
- Adapter change:

  ```go
  type MetadataWatcher interface {
      Watch(context.Context, WatchPermit, string) (watch.Interface, error)
  }
  ```

  `WatchPermit` exposes no public constructor and preserves only the exact GVR,
  discovered scope, namespace, and current capability set needed for
  enforcement evidence.
- Shared transport: One WATCH continues while at least one current binding
  exists. Events route only to current owners. Removing the last binding stops
  the supervisor and cancels the stream.
- Forbidden handling: A Forbidden WATCH start emits one sanitized enforcement
  record for every current bound capability, then follows existing bounded
  supervisor recovery. It does not overwrite a successful LIST-derived status
  because watch health has no public condition in the approved API.
- Dependencies: Existing route registry, watcher, backoff, and freshness
  tracker.
- Requirements: `R3.AC10`, `R4.AC4`-`R4.AC6`, `R4.AC8`, `R5.AC1`-`R5.AC5`,
  `R5.AC9`, `R7.AC6`, `NFR1`, `NFR2`, `C3`, `C4`.

### Reconciliation Composition And Revocation

- Purpose: Produce one shared authorization subject, coordinate ordered batch
  evaluation, and preserve existing result/status semantics.
- Flow:
  1. acquire the current `Lease` and child context;
  2. skip authorization evaluation for zero sources;
  3. load one fresh policy snapshot;
  4. plan every source in declaration order;
  5. convert each source target to requests in target order;
  6. evaluate one authorization batch per source;
  7. bind capabilities and derive routes from those same capabilities;
  8. replace routes before LIST execution;
  9. execute authorized sibling sources and preserve denied source outcomes;
  10. publish only while identity, generation, deletion state, context, and
      policy epoch remain current.
- Revocation: The existing policy handler keeps its order: invalidate tracker
  epoch and child contexts, remove all routes and supervisors, then enqueue all
  Kubeseers. Fresh reconciliation is required after both narrowing and
  broadening.
- Dependencies: Existing runtime, policy source, planner, tracker, route
  manager, executor, status publisher, and new enforcer.
- Requirements: `R2`, `R4.AC9`, `R5`, `R8`, `NFR2`, `NFR5`, `C1`, `C4`.

### Existing Status Outcome Adapter

- Purpose: Keep the approved public condition and result contracts while
  consuming the stronger enforcement outcomes.
- Mapping:

  | Internal outcome | Existing public authorization outcome |
  | --- | --- |
  | Allowed capability and successful read | `AuthorizationSucceeded` |
  | Explicit resource, namespace, or cluster-scope denial | `AuthorizationDenied` |
  | Missing policy | `PolicyMissing` |
  | Invalid policy | `PolicyInvalid` |
  | Unavailable policy | `AuthorizationUnavailable` |
  | Configuration or resolution prevented a decision | `AuthorizationNotEvaluated` |
  | Logically allowed LIST rejected by Kubernetes RBAC | `ReadForbidden` |

  A transient non-RBAC LIST failure preserves logical authorization as allowed
  and remains a separate retryable source failure.
- Dependencies: Existing status assessment enums, result errors, composer, and
  publisher.
- Requirements: `R6.AC9`, `R8`, `NFR3`, `NFR5`, `NFR6`, `C6`.

## Data Models

### Authorization Subject

```go
type Subject struct {
    Key         types.NamespacedName
    UID         types.UID
    Generation  int64
    PolicyEpoch uint64
}
```

`Subject` is the process-local identity shared by capabilities, LIST checks,
route bindings, and evidence. It is converted from `reconciliation.Lease`; it
is not serialized or exposed in the public API.

### Policy Identity

```go
// package accesspolicy
type PolicyIdentity struct {
    Name       string
    UID        types.UID
    Generation int64
}
```

The identity is present for valid and invalid loaded policy objects. Missing
and unavailable snapshots expose no identity. Name must be the canonical
`installation-access-ceiling`; identity never affects the pure decision.

### Decision And Enforcement Record

```go
type PolicyState string
const (
    PolicyReady       PolicyState = "Ready"
    PolicyMissing     PolicyState = "Missing"
    PolicyInvalid     PolicyState = "Invalid"
    PolicyUnavailable PolicyState = "Unavailable"
)

type RecordKind string
const (
    RecordPolicyDecision RecordKind = "PolicyDecision"
    RecordReadForbidden  RecordKind = "ReadForbidden"
)

type Outcome string
const (
    OutcomeAllowed     Outcome = "Allowed"
    OutcomeDenied      Outcome = "Denied"
    OutcomeUnavailable Outcome = "Unavailable"
    OutcomeForbidden   Outcome = "ReadForbidden"
)

type Record struct {
    Kind           RecordKind
    Subject        Subject
    SourceID       string
    APIGroup       string
    KindName       string
    Scope          discovery.Scope
    Namespace      string
    PolicyState    PolicyState
    PolicyIdentity *accesspolicy.PolicyIdentity
    Outcome        Outcome
    Reason         string
}
```

The record intentionally has no generic message, selector, object identity,
field path, payload, or raw error field. `DeepCopy` or equivalent defensive
copying protects pointer fields at observer boundaries.

### Outcome Batch And Capability

```go
type DecisionOutcome struct {
    request    accesspolicy.Request
    decision   accesspolicy.Decision
    record     Record
    capability *Capability
}

type Capability struct {
    subject  Subject
    request  accesspolicy.Request
    record   Record
    recorder Recorder
}
```

Capability fields and constructors are private. Accessors return defensive
values. The recorder reference permits the actual LIST/WATCH boundary to emit
a linked Forbidden record without carrying raw Kubernetes errors upward.

### Freshness Verifier

```go
type Verifier interface {
    IsCurrent(Subject) bool
}

type VerifierFunc func(Subject) bool
```

`FreshnessTracker` implements this contract by comparing key, UID, generation,
deletion/absence state, and policy epoch. A nil verifier never means current.

### Private I/O Permits

`selection.AuthorizedRead` pairs one exact `ReadTarget` with one capability.
`reconciliation.WatchPermit` pairs one exact `WatchAddress` with the current
capabilities sharing that transport. Both have private fields and constructors;
only the production adapters receive them.

## Processing Flows

### Allowed Source

1. Planner returns exact targets from discovery.
2. Enforcer evaluates requests using one fresh snapshot and records outcomes.
3. Binding verifies complete matching allowed capabilities.
4. Route registry installs capability-backed bindings.
5. LIST adapter verifies each authorized read before every page.
6. Extraction, typing, result construction, and status composition proceed
   unchanged.

### Denied Or Terminal Policy Source

1. Enforcer records one denied or unavailable outcome per exact target.
2. Binding returns no executable source plan.
3. No route or WATCH permit is created for denied targets.
4. No LIST occurs for any target in that source execution.
5. Independent authorized sibling sources continue.
6. Existing source errors and status outcomes describe the current generation.

### Policy Change

1. Policy predicate accepts canonical create, replacement, generation,
   deletion-marker, or delete events.
2. Tracker increments policy epoch and cancels active child contexts.
3. Route registry removes all bindings and stops supervisors.
4. Enqueue-all requests fresh reconciliation.
5. Any in-flight network request receives the canceled context.
6. Stale capabilities fail future checks and stale candidates fail publication.

### Kubernetes RBAC Forbidden

1. Logical policy has already emitted an Allowed record and capability.
2. Production LIST or WATCH adapter receives Forbidden from Kubernetes.
3. Capability emits a linked sanitized `ReadForbidden` record.
4. LIST returns the existing source-scoped `ReadForbidden` error with no values.
5. WATCH follows route recovery while periodic reconciliation remains active.

### Zero Sources

The runtime creates no authorization subject batch, loads no policy for
authorization, installs no routes, performs no resource I/O, and preserves the
approved present empty successful result.

## Error Handling

| Failure | Enforcement behavior | Retry/public behavior |
| --- | --- | --- |
| Missing policy | Deny all targets; no capability | Deterministic `PolicyMissing` |
| Invalid policy | Deny all targets; preserve policy identity in evidence | Deterministic `PolicyInvalid` |
| Unavailable policy | Deny all targets; no cached allow | `AuthorizationUnavailable` plus rate-limited retry |
| Incomplete subject | No capability or I/O | Sanitized internal enforcement failure; no stale publication |
| Missing/denied/mismatched/duplicate/extra outcome | Source binding fails before I/O | Existing deterministic authorization error |
| Stale capability | Adapter returns before network request | Context/stale path withholds obsolete status |
| LIST Forbidden | Emit linked record; discard source values | `ReadForbidden`, no immediate transient retry |
| WATCH Forbidden | Emit linked records; no object retention | Existing bounded watch recovery and periodic safety |
| Other transient LIST failure | Keep logical allow distinct | Sanitized source error plus rate-limited retry |
| Policy change during I/O | Cancel context and invalidate epoch | Discard old work; fresh reconciliation |
| Evidence observer absent from direct construction | Normalize to explicit no-op recorder | Authorization result remains deterministic |

Authorization records never format underlying errors. Existing error wrappers
may retain causes for programmatic classification, but user-facing strings,
status, and evidence use only stable reasons and fixed sanitized text.

## Security Considerations

- The policy evaluator remains fail-closed and is never bypassed by the
  operator ServiceAccount or Kubeseer-author RBAC.
- Capability constructors and private I/O permits prevent normal production
  code from reaching LIST or WATCH with a raw target.
- Every network call performs a currentness check immediately before request
  construction and receives a context canceled by policy or identity changes.
- The small race after the currentness check is contained by passing that same
  context to client-go; already-completed bytes are invocation-local and stale
  publication guards prevent their release.
- A multi-target source receives no LIST capability unless every exact target
  is allowed, while denied siblings do not block independent sources.
- Shared WATCH transports retain metadata events only and continue solely for
  current exact bindings; selectors are re-evaluated by the guarded LIST path.
- The feature never impersonates users, performs SubjectAccessReview, grants
  RBAC, or interprets a policy allow as a Kubernetes RBAC allow.
- Evidence is allowlisted by type. It excludes object names and UIDs, selectors,
  paths, values, payloads, Secret data, and raw API errors.
- No authorization state or capability is serialized to status, annotations,
  caches, or durable storage.

## Failure Modes And Tradeoffs

- Failure mode: Policy changes between a freshness check and the client-go
  call.
  - Mitigation: Invalidation cancels the exact context passed to the call;
    pagination/restart checks block later requests and publication rejects the
    stale epoch.
  - Tradeoff: An already-issued API request cannot be retroactively unissued,
    but its returned values cannot become public status.

- Failure mode: One stale owner shares a watch address with current owners.
  - Mitigation: The registry prunes stale exact bindings before each WATCH
    attempt and creates a permit only when at least one current binding remains.
  - Tradeoff: Transport sharing is retained instead of allocating one WATCH per
    source, reducing load while preserving per-owner authorization evidence.

- Failure mode: A recorder implementation drops process-local records.
  - Mitigation: Records remain deterministic values in enforcement outcomes;
    the recorder is an explicit replaceable port and cannot alter capability
    decisions.
  - Tradeoff: Durable delivery is intentionally not guaranteed until
    `observability` defines storage and operations.

- Failure mode: An invalid present policy cannot compile.
  - Mitigation: The deny-all snapshot preserves only canonical object identity
    and stable invalid reason; no invalid policy fields enter evidence.
  - Tradeoff: Audit context identifies which generation failed without exposing
    malformed configuration values.

- Failure mode: Direct test or future code constructs an executor without a
  verifier.
  - Mitigation: Constructors validate dependencies and production adapters fail
    closed before I/O.
  - Tradeoff: Existing tests must use explicit current-verifier fixtures,
    making the security dependency visible.

- Failure mode: WATCH succeeds but LIST is Forbidden, or vice versa.
  - Mitigation: Each verb emits its own enforcement record; only LIST affects
    result status, while periodic reconciliation remains available when WATCH
    is unhealthy.
  - Tradeoff: No new public watch-health condition is added in this feature.

## Testing Strategy

Testing follows the repository constitution: no dedicated package-local unit
suite is introduced. Real production modules are composed at module-integration
and envtest layers; narrow fakes replace only Kubernetes I/O or the evidence
observer.

### Module Integration

Add authorization-enforcement scenarios to `TestModuleIntegration`, using the
real discovery resolver, policy loader/compiler/evaluator, enforcer, selection
planner/binder/executor, reconciliation tracker/routes/pipeline, typed result,
and status composer.

Scenarios cover:

- exact namespaced and cluster-scoped capability construction;
- missing, invalid, unavailable, resource, namespace, and cluster-scope denial;
- missing, denied, mismatched, duplicate, and extra outcomes with zero I/O;
- all-or-nothing multi-target source binding and authorized sibling isolation;
- freshness checks before first page, continuation, expiration restart, watch
  start, and watch restart;
- policy-epoch, UID, generation, deletion, and context invalidation;
- deterministic ordered policy and ReadForbidden records;
- evidence exclusion of selectors, object identity, values, paths, Secrets, and
  raw errors;
- logical allow remaining separate from transient LIST failure;
- zero-source policy-independent success;
- existing status reason mapping and stale-value removal.

Each selected scenario logs a stable non-vacuous marker such as
`MODULE_INTEGRATION=authorization-enforcement STATUS=passed`.

### Envtest

Extend `TestEnvtestReconciliationRuntime` with real API-server behavior:

- create an allowed Kubeseer and observe LIST/WATCH plus successful status;
- narrow, delete, invalidate, and re-create `installation-access-ceiling`;
- prove active watch cancellation and no newly denied LIST call;
- prove stale values disappear from the next status snapshot;
- broaden policy and prove no read occurs before fresh reconciliation;
- inject real or adapter-level Forbidden responses without exposing API bodies;
- exercise policy races against pagination and watch restart barriers;
- preserve UID/generation/policy-epoch no-write guards;
- repeat through the supported Kubernetes 1.35.6 and 1.36.2 compatibility
  matrix.

The envtest scenario emits
`API_CONTRACT=authorization-enforcement STATUS=passed`. Tests use bounded
polling and barriers rather than arbitrary sleeps and never use ambient
kubeconfig credentials.

### Static And Race Verification

- `make verify` proves formatting, generated artifacts, and test-layer policy;
- `make test GO_TEST_FLAGS=-race` proves cross-module concurrency safety;
- `make test-api` and `make test-compatibility` prove real Kubernetes behavior;
- `go build ./...` proves the package graph remains acyclic;
- `go mod tidy -diff` proves no module drift.

## Verification Plan

- Requirement proof: Capability construction and private adapter signatures
  prove exact authorization structurally; integration call counters and
  freshness barriers prove no forbidden or stale I/O; envtest status snapshots
  prove revocation and public outcome behavior.
- Test evidence: Stable module-integration and envtest markers prevent vacuous
  proof selection. Race-enabled tests cover tracker, route, recorder, and policy
  event concurrency. Compatibility repeats API behavior on both supported
  Kubernetes versions.
- Operational evidence: Capturing recorder assertions prove ordered sanitized
  decision records. Durable sinks, metrics, alerts, and dashboards remain
  explicitly deferred to `observability`.
- Regression evidence: Existing installation-policy, resource-selection,
  reconciliation-runtime, and status-and-conditions tests continue to pass,
  demonstrating that policy semantics and public status remain unchanged.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Policy Snapshot Identity; Authorization Enforcer; Selection Capability Binding |
| `R2` | Policy Snapshot Identity; Authorization Enforcer; Reconciliation Composition And Revocation |
| `R3` | Authorization Enforcer; Selection Capability Binding; private I/O permits |
| `R4` | Authorized LIST Adapter; Authorized Route And WATCH Permit |
| `R5` | Authorized Route And WATCH Permit; Reconciliation Composition And Revocation; Policy Change flow |
| `R6` | Authorization Enforcer; Authorized LIST Adapter; Kubernetes RBAC Forbidden flow |
| `R7` | Authorization Evidence Recorder; Decision And Enforcement Record |
| `R8` | Existing Status Outcome Adapter; Reconciliation Composition And Revocation |
| `NFR1` | Opaque capabilities, verifier checks, private LIST/WATCH permits, no-raw-target integration proofs |
| `NFR2` | Policy epoch invalidation, context cancellation, route removal, stale publication guard, envtest policy races |
| `NFR3` | Allowlisted record schema, sanitized existing errors, confidentiality assertions |
| `NFR4` | Ordered batch evaluator, stable enums/reasons, capturing-recorder proof |
| `NFR5` | Source-atomic capability binding, sibling execution, per-key reconciliation isolation |
| `NFR6` | No public API or CRD changes; existing status adapter and compatibility suite |
| `NFR7` | Module integration, envtest, race, compatibility, build, and tidy verification |
| `C1` | Composition reuses every approved prerequisite module and regression suite |
| `C2` | `internal/accesspolicy` remains the only policy compiler/evaluator |
| `C3` | Authorized LIST Adapter and Authorized Route And WATCH Permit |
| `C4` | Freshness verifier, policy handler ordering, route registry, guarded publisher |
| `C5` | Kubernetes RBAC separation; no RBAC API or manifest changes |
| `C6` | Process-local data models; no public API changes |
| `C7` | Non-failing recorder port; no journal or durable adapter |
| `C8` | Higher-layer Testing Strategy and Verification Plan |
| `C9` | Existing repository license headers and verification rules |
