---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T19:32:54Z
last_modified: 2026-08-26T19:32:54Z
approved_fingerprint: sha256:b9c28d2c7b5eb65532ab7c9ab7bf3c5060dd700b1b281f318dd16a643e4b20d9
source_requirements_approved_at: 2026-08-26T19:03:02Z
source_requirements_fingerprint: sha256:7894cd920ebd3bde99f6af6ce91d8d9ab00cd8adc61fe3b8309c937677860700
---

# Feature Design

## Overview

Implement `reconciliation-runtime` as the internal composition root that
registers one controller-runtime controller, joins the approved domain modules
in their required order, and publishes the existing structural typed result to
the Kubeseer status subresource. The controller uses namespace/name work-queue
keys, direct fresh reads for security- and publication-sensitive state, and the
manager cache only as an event source. It adds no public API field, condition,
finalizer, deployment manifest, or product-wide concurrency setting.

The controller has three event paths. A typed Kubeseer source handles create,
generation-change, deletion-timestamp, and delete events. A typed singleton
policy source invalidates active evaluations and requests an enqueue of every
Kubeseer. One custom trigger source owns periodic safety scheduling and
authorized metadata-only watches for arbitrary discovered GVRs. All paths add
ordinary `reconcile.Request` values to the same controller-runtime queue, which
supplies duplicate coalescing, dirty-during-processing follow-up, per-key rate
limiting, and same-key execution exclusion.

Source watches are conservative and selector-independent. One transport is
shared for each exact address `(GVR, discovered scope, namespace)` while route
bindings retain the source ID needed to prove an exact policy decision. Every
add, modify, or delete event for that address enqueues every distinct bound
Kubeseer key; selectors are evaluated only by the approved selection LIST path.
This catches objects entering or leaving a selector without storing old objects,
resource contents, extracted values, or field paths.

Reconciliation loads the latest Kubeseer and a fresh fail-closed installation
policy snapshot, plans and authorizes all source targets, atomically replaces
that owner's watch routes, and only then performs authorized LIST, extraction,
typed conversion, and `BuildResult`. The user-approved partial-result policy is
applied at source boundaries: successful sources remain visible, failed sources
become sanitized error outcomes, and cancellation before terminal outcomes
withholds status entirely. A publication guard re-reads the Kubeseer directly
from the API server and performs at most one optimistic `Status().Update` when
the normalized semantic projection changed.

<!-- assumed: security-sensitive Kubeseer and policy reads use manager.GetAPIReader rather than the cache because R4, R6, and R8 require latest persisted generation and a fresh policy snapshot (source: approved requirements and controller-runtime client boundaries) -->
<!-- assumed: dynamic source observation uses the metadata client and no informer-owned initial resource LIST, so selection remains the only resource-instance LIST boundary; periodic safety closes watch gaps (source: approved C3, C7, R2.AC10, and R3.AC2) -->
<!-- assumed: the controller does not override manager-level MaxConcurrentReconciles; state is concurrency-safe when a future composition root configures more workers (source: approved C10 and R8) -->
<!-- assumed: a transient source failure may be published as a sanitized partial result before Reconcile returns an error for rate-limited retry; semantic suppression prevents retry churn (source: approved Option A, R5, R6, and R7) -->

## Architecture

```text
manager cache                         direct API-server clients
     |                                          |
     | Kubeseer add/spec/delete                 | GET Kubeseer + policy
     v                                          v
Lifecycle Handler ---- cancel/observe ----> Freshness Tracker
     |                                          |
     +---------------- reconcile.Request -------+------+
                                                        |
policy cache                                            v
     | singleton change                         controller-runtime queue
     v                                          coalesce + rate limit
Policy Handler -- invalidate all --> Trigger Source     |
                                          ^             v
periodic ticker -- enqueue all ------------+       Reconciler
authorized metadata WATCH -- target event -+             |
                                                        v
                                             selection.Planner
                                             discovery.Resolver
                                                        |
                                             fresh accesspolicy.Snapshot
                                                        |
                                             selection.Bind
                                                        |
                                  Route Registry.Replace(owner, generation)
                                  (remove denied/obsolete routes before LIST)
                                                        |
                                             selection.Executor
                                                        |
                                             extraction.ExtractBatch
                                                        |
                                             typedoutput.ConvertBatch
                                                        |
                                             typedoutput.BuildResult
                                                        |
                                             Semantic Status Publisher
                                             direct re-GET + Status().Update
```

The runtime registers the controller through low-level `controller.New` and
fixed `source.Kind` sources rather than the higher-level builder. This lets the
Kubeseer event handler cancel active stale or deleting work before enqueuing the
key. The trigger source implements
`source.TypedSource[reconcile.Request]`; `Start` captures the queue, starts
cancellable scheduler/watch supervisors, and returns immediately as required
by controller-runtime.

The manager starts sources before workers. Existing Kubeseers therefore arrive
through informer add events after cache synchronization, and periodic
enqueue-all independently provides restart and watch-failure recovery.
Reconciliation does not require an ambient kubeconfig, a production entrypoint,
or a finalizer.

## Options Considered

### Option A — One controller with a custom trigger source and shared metadata watches

- Summary: Use fixed typed sources for Kubeseer and policy events, plus one
  custom controller source that owns exact-target metadata watches, periodic
  enqueue-all scheduling, and a concurrency-safe route registry.
- Why chosen: It keeps one queue and one reconciliation path, supports arbitrary
  discovered GVRs without pre-registering schemes, shares equivalent watch
  transports, keeps selection as the only full-resource LIST boundary, and
  makes periodic fallback independent of watch health.

### Option B — Create one informer or controller-runtime source per target

- Summary: Construct a metadata informer for every authorized GVR/namespace and
  add it dynamically to the controller.
- Why rejected: A shared informer performs an initial metadata LIST and retains
  an object store, adding a second resource-list/cache boundary. Dynamic source
  removal and reference sharing are also harder than a small direct-WATCH
  supervisor.

### Option C — Use only periodic reconciliation

- Summary: Remove source watches and enqueue all Kubeseers on a fixed interval.
- Why rejected: It does not satisfy source create/update/delete event routing
  and increases convergence latency and repeated LIST load.

### Option D — Watch with each source's selectors

- Summary: Establish separate server-side watches for each source selector.
- Why rejected: Updates leaving a selector are difficult to route without old
  object retention, equivalent targets cannot share one transport reliably, and
  field-selector watch support varies by resource. A broad authorized target
  watch plus fresh selection LIST is smaller and safer.

## Simplicity And Elegance Review

- Simplest viable shape: one `internal/reconciliation` package, one controller,
  one trigger source, one route registry, one orchestrator, and one status
  publisher. Existing domain modules stay authoritative.
- Coupling check: reconciliation imports domain packages and controller
  libraries; no upstream package imports reconciliation. Kubernetes clients
  terminate at narrow reader, watcher, lister, and status-writer ports.
- State check: mutable runtime state contains only Kubeseer keys, UIDs,
  generations, exact target identities, metadata resource versions,
  cancellation functions, and retry timing. Result values are invocation-local.
- First-draft challenge: `GenerationChangedPredicate` alone is smaller, but it
  suppresses deletion-timestamp-only updates and cannot cancel active stale
  work. The lifecycle tracker closes both races without persistence.
- Watch challenge: controller-runtime accepts dynamically added sources, but
  one source per target spreads lifecycle and sharing across controller setup.
  One source with internal supervisors has clearer ownership.
- Future-proofing: condition rendering, authorization audit, hashes, metrics,
  limits, worker budgets, production RBAC, leader election, and packaging remain
  in their named later specifications.

## Components And Interfaces

### Runtime setup and controller registration

- Purpose: Validate internal options, construct concrete adapters, and register
  one controller plus its sources with a supplied manager.
- Boundary: `SetupWithManager(mgr manager.Manager, options Options) error` with
  a required positive `SafetyInterval`. Test construction uses `NewRuntime` with
  explicit narrow dependencies; no CRD option is introduced.
- Dependencies: manager cache for typed event sources;
  `mgr.GetAPIReader()` for fresh Kubeseer/policy reads; `mgr.GetClient()` for
  status writes; client-go discovery/dynamic clients for existing planning and
  selection; metadata client for source WATCH.
- Startup: validate dependencies and interval before creating the controller or
  starting a watcher. Leave max workers, leader election, deployment, and RBAC
  to manager/packaging configuration.
- Requirements: `R1.AC1`, `R1.AC7`, `R1.AC8`, `R3.AC1`, `R3.AC5`, `NFR7`,
  `NFR9`, `C1`-`C5`, `C9`-`C11`.

### Kubeseer lifecycle handler and freshness tracker

- Purpose: Convert owned-resource events into keys while canceling active work
  that became stale or unsafe.
- Predicate: accept create/delete/generic events; accept update only when UID or
  generation changes, or deletion timestamp transitions from absent to present.
  Suppress status-only updates.
- Observation: retain only key, UID, generation, and deleting/absent state. A
  newer generation, new UID, deletion marker, delete event, or policy epoch
  invalidation cancels the active per-instance child context.
- Deletion: remove owner routes immediately on a deletion marker or delete
  event, then enqueue. A later NotFound reconcile repeats removal idempotently.
- Guard: expose an immutable lease of key, UID, generation, and policy epoch;
  stale leases cannot replace routes or publish.
- Requirements: `R1.AC2`-`R1.AC7`, `R3.AC4`, `R4.AC1`, `R4.AC2`,
  `R4.AC8`, `R4.AC10`, `R6.AC13`, `R6.AC14`, `R8.AC1`, `R8.AC5`,
  `R8.AC7`, `NFR5`.

### Trigger source and periodic scheduler

- Purpose: Feed periodic, policy-wide, and observed-target triggers into the
  same typed queue.
- Source contract: `Start(ctx, queue)` binds the queue once, starts goroutines
  under `ctx`, and returns without blocking.
- Enqueue-all: list current Kubeseer metadata through a narrow reader and add
  every key. Periodic ticks and singleton policy events share this operation.
  Concurrent requests collapse into one pending intent; transient list failure
  retries it with cancellable capped backoff.
- Queue semantics: ordinary triggers use `queue.Add`. The controller-runtime
  queue coalesces pending duplicates and preserves a dirty follow-up when an
  event arrives during processing. Reconcile errors use per-item rate limiting;
  successful completion clears that key's rate-limit state.
- Requirements: `R2.AC8`, `R2.AC10`, `R3.AC2`-`R3.AC8`, `R7.AC5`,
  `R7.AC12`, `R8.AC2`, `R8.AC3`, `NFR2`, `NFR3`, `NFR7`, `NFR8`.

### Authorized route registry and watch supervisors

- Purpose: Maintain race-free owner-to-target routing and one live metadata
  WATCH transport for every exact address with an authorized binding.
- Inputs: `Replace(lease, []AuthorizedRoute)`, `RemoveOwner(key)`, and watch
  events. An `AuthorizedRoute` is constructed only from a successful policy
  decision paired with the exact `selection.ReadTarget`.
- Sharing: `WatchAddress` is GVR, discovered scope, and namespace.
  `RouteBinding` additionally carries source ID and owner generation so WATCH
  and LIST authorization dimensions remain exact while one transport is shared.
- Replacement: validate the lease, atomically remove obsolete bindings and add
  current ones, cancel zero-reference supervisors, then start newly referenced
  addresses. Reconciliation replaces routes before selection LIST. Failed,
  unresolved, or denied sources contribute no route.
- Watch adapter: use `metadata.Interface` against exactly the discovered GVR and
  namespace. Apply no object selector and retain only the last metadata
  resourceVersion needed for resumption.
- Events: Added, Modified, and Deleted enqueue every distinct currently bound
  owner. Target-wide routing makes Modified cover old and new selector
  membership without retaining either object. Bookmarks only advance metadata.
- Recovery: start error, error event, or unexpected close restarts that
  transport with cancellable capped exponential backoff. Expired resource
  version clears the resume token; periodic reconciliation covers any gap.
- Requirements: `R2.AC1`-`R2.AC11`, `R3.AC7`, `R7.AC3`, `R7.AC7`,
  `R7.AC8`, `R8.AC4`, `R8.AC5`, `NFR1`, `NFR2`, `NFR5`, `NFR6`,
  `NFR8`, `C4`-`C7`.

### Reconciliation pipeline orchestrator

- Purpose: Produce exactly one ordered terminal outcome per declared source by
  composing approved production modules.
- Preflight: for every source in declaration order call
  `selection.Planner.Plan`; evaluate `selection.RequestForTarget` for every
  exact target against one fresh snapshot; pass complete decisions to
  `selection.Bind`; derive routes only from bound plans.
- Execution: after route replacement, execute each valid plan in source order.
  Convert planning, discovery, authorization, and read failures into sanitized
  `selection.SelectionOutcome` slots. Feed every slot through
  `extraction.ExtractBatch`, `typedoutput.ConvertBatch`, and
  `typedoutput.BuildResult`.
- Isolation: selection discards partial LIST pages, extraction discards failed
  source temporaries, typed conversion preserves successful sibling
  fields/resources, and each invocation owns its result slices.
- Empty input: zero sources produces a present empty `KubeseerResult` and no
  routes.
- Cancellation: check the child context before route replacement, each module
  boundary, result construction, and publication. Cancellation before all
  sources are terminal withholds candidate status.
- Requirements: `R4.AC1`-`R4.AC9`, `R5.AC1`-`R5.AC13`,
  `R7.AC7`-`R7.AC11`, `R8.AC5`, `R8.AC6`, `R8.AC8`, `NFR1`, `NFR4`,
  `NFR6`, `NFR9`, `C1`, `C3`.

### Retry classifier

- Purpose: Separate publishable source failures from the decision to request an
  immediate rate-limited retry.
- Transient: discovery unavailable; policy unavailable; API timeout, temporary
  transport failure, rate limiting, LIST unavailability or repeated
  continuation expiry; status conflict or transient status failure. Publish a
  candidate when possible, then return one joined sanitized error.
- Deterministic: invalid source/selector/scope/type/path/value, policy
  missing/invalid/denied, authorization mismatch, and ServiceAccount Forbidden.
  Publish the outcome and return success so events or periodic safety drive the
  next evaluation.
- Cancellation: parent cancellation/deadline withholds publication and does not
  manufacture a retry.
- Confidentiality: retry errors contain stage, source ID, target identity, and
  stable reason only; never resource contents, selector values, field paths, or
  extracted values.
- Requirements: `R3.AC6`, `R3.AC7`, `R4.AC9`, `R7.AC1`-`R7.AC6`,
  `R7.AC12`, `R8.AC3`, `NFR4`, `NFR6`, `NFR7`.

### Semantic status publisher

- Purpose: Publish the current generation's result exactly once and only when
  its semantic projection differs.
- Guard: directly re-GET by key and reject NotFound, deletion, UID mismatch,
  generation mismatch, canceled context, or stale tracker lease without a
  write. Copy the freshly persisted object before mutation.
- Candidate: set `ObservedGeneration`, defensively copy `Result`, and preserve
  freshly persisted `Conditions` unchanged.
- Comparison: recursively normalize nil and empty result collections, preserve
  result pointer presence, and compare every source state, provenance field,
  field state/type, match cardinality/payload, and sanitized error. Do not add a
  semantic hash.
- Write: when different, call `client.Status().Update(ctx, candidate)` once.
  Latest resourceVersion gives optimistic concurrency; conflict/transient errors
  return to the retry classifier. Whole-object writes are forbidden.
- Requirements: `R4.AC10`-`R4.AC12`, `R6.AC1`-`R6.AC14`, `R7.AC4`,
  `R8.AC7`, `NFR3`, `NFR5`, `NFR7`, `NFR9`, `C8`, `C9`.

## Data Models

```go
type Options struct {
    SafetyInterval time.Duration
}

type Lease struct {
    Key         types.NamespacedName
    UID         types.UID
    Generation  int64
    PolicyEpoch uint64
}

type WatchAddress struct {
    GVR       schema.GroupVersionResource
    Scope     discovery.Scope
    Namespace string
}

type RouteBinding struct {
    Address    WatchAddress
    SourceID   string
    Owner      types.NamespacedName
    OwnerUID   types.UID
    Generation int64
}

type AuthorizedRoute struct {
    binding RouteBinding
}

type routeState struct {
    byOwner  map[types.NamespacedName]map[RouteBinding]struct{}
    byTarget map[WatchAddress]map[types.NamespacedName]int
    watches  map[WatchAddress]*watchSupervisor
}

type Candidate struct {
    Lease     Lease
    Result    v1alpha1.KubeseerResult
    Retryable error
}
```

`WatchAddress` omits source ID because it identifies a Kubernetes network
endpoint. `RouteBinding` retains source ID so every source sharing that endpoint
must independently pass exact authorization. The reverse index uses per-owner
reference counts but emits each owner key once.

`PolicyEpoch` is process-local invalidation metadata, not a policy cache. Every
reconciliation still calls `accesspolicy.Load` through a direct
APIReader-backed `PolicySource`; no old allowed snapshot survives a load
failure.

## Reconciliation And Event Flows

### Reconcile one key

1. Directly GET the current Kubeseer. On NotFound, remove routes and succeed.
2. If deletion is present, cancel the key, remove routes, and succeed without
   discovery, WATCH creation, or resource-instance LIST.
3. Begin a freshness lease for UID/generation and a cancellable child context.
4. Load and compile one fresh policy snapshot through `accesspolicy.Load`.
5. Plan each source, evaluate every exact target, and bind complete decisions.
6. Replace the owner's routes with only current authorized targets before LIST.
7. Execute valid plans and put sanitized failures in invalid/denied source slots.
8. Run extraction, typed conversion, and structural result construction.
9. Withhold status if cancellation occurred before terminal outcomes; otherwise
   invoke semantic publication.
10. Return success when no transient failure remains; otherwise return an error
    after publication so the queue applies per-key backoff.

### Source target event

1. A supervisor receives PartialObjectMetadata Added, Modified, or Deleted.
2. It snapshots and deduplicates owners for the exact address under a read lock.
3. It drops the event object and adds each key to the queue.
4. Reconciliation re-GETs spec/policy, re-lists with current selectors, and
   removes deleted or no-longer-matching values through normal result rebuild.

### Policy event

1. The typed handler accepts create/delete or generation change only for
   `installation-access-ceiling`.
2. It advances the policy epoch and cancels active reconciliations.
3. It asks the trigger source to enqueue all existing Kubeseers.
4. Each next run loads a fresh valid or deny-all snapshot, replaces routes
   before LIST, and publishes no newly denied values.

## Semantic Status Normalization

Normalization is pure and invocation-local. Nil and zero-length source,
resource, field-error, field, and match slices compare equal, while list order
and all scalar/pointer payloads are preserved. It distinguishes:

- absent `status.result` from a present empty result;
- old from current `observedGeneration`;
- successful empty selection from source error;
- absent, null, value, and error field states;
- empty string, numeric zero, false, and omitted payload pointers;
- every provenance coordinate including UID;
- sanitized error reason and message.

Conditions are copied from the latest persisted object and are not authored or
normalized by this feature. A future condition writer is therefore preserved,
and a concurrent change causes conflict/retry rather than overwrite.

## Error Handling

| Failure | Candidate behavior | Immediate retry | Route behavior |
| --- | --- | --- | --- |
| Kubeseer NotFound/deleting | No candidate | No | Remove owner |
| Policy missing/invalid | Sanitized errors, no values | No | Remove affected routes |
| Policy unavailable | Fail-closed errors, no values | Yes | Remove affected routes |
| Deterministic discovery/configuration error | Failed source plus siblings | No | Remove failed-source routes |
| Discovery unavailable | Failed source plus siblings | Yes | Remove failed-source routes |
| Logical policy denial | Failed source plus siblings | No | Remove denied routes |
| WATCH start/stream error | LIST result remains eligible | Supervisor backoff | Retain route and periodic fallback |
| LIST Forbidden/unsupported selector | Failed source plus siblings | No | Retain authorized route |
| LIST timeout/unavailable/expired twice | Failed source plus siblings | Yes | Retain authorized route |
| Extraction/conversion failure | Approved source/field-local outcome | Only for transient upstream/cancellation | Retain route |
| BuildResult invariant failure | No candidate | Yes, rate limited | Retain routes |
| Stale UID/generation/policy lease | No write | Queued follow-up | Newer run owns routes |
| Status conflict/transient API failure | Candidate not assumed persisted | Yes | Retain routes |
| Context cancellation | No candidate/write | No synthetic retry | Cancel operations |

Watch and enqueue-all backoff use context-selectable timers, never blocking
sleeps in `Reconcile`. Deterministic failures wait for relevant events or the
periodic interval.

## Security Considerations

- Kubeseer and policy snapshots are direct API-server reads. Missing, invalid,
  nil, or unavailable policy yields a new deny-all snapshot and never reuses an
  old allow.
- Discovery alone supplies GVR and scope. WATCH and LIST derive identical exact
  policy dimensions from `selection.ReadTarget`; Kind naming never determines
  scope.
- Route replacement follows policy binding and precedes LIST. Denied or
  unresolved targets cannot start a new watch or reach selection and lose stale
  routes from prior generations.
- Policy observation cancels active child contexts. Every Kubernetes/module
  boundary receives and checks that context before starting another read.
- Metadata WATCH is broad only inside one authorized exact target, requests
  PartialObjectMetadata, applies no selector, and drops event objects after
  deriving the trigger.
- Routing, retries, diagnostics, and logs omit resource contents, extracted
  values, typed payloads, selector values, and field paths.
- Logical allow and ServiceAccount RBAC remain distinct. WATCH/LIST Forbidden is
  never converted into an allow, and this feature creates no role.
- Status publication checks key, UID, generation, deletion state, policy epoch,
  context, and resourceVersion and uses only the status subresource.

## Failure Modes And Tradeoffs

- Failure mode: Direct WATCH can miss events during start, backoff, or expired
  resource-version recovery.
- Mitigation: Start supervision before LIST, resume metadata resourceVersion
  when valid, and periodically enqueue every Kubeseer.
- Tradeoff: Deterministic eventual convergence is guaranteed, not a globally
  atomic snapshot across independent endpoints.

- Failure mode: A broad target watch observes objects matching no selector.
- Mitigation: Queue coalescing deduplicates keys and semantic comparison
  suppresses unchanged status writes.
- Tradeoff: Extra reconciliations are accepted to avoid object retention and
  missed leave-selector transitions.

- Failure mode: A transient source failure is published and retried.
- Mitigation: Per-item exponential backoff, source isolation, and semantic no-op
  detection bound churn.
- Tradeoff: Current failure visibility and successful siblings are preferred to
  retaining stale values.

- Failure mode: Policy/generation update races with active evaluation.
- Mitigation: Event cancellation, freshness leases, boundary context checks,
  route generation guards, and direct pre-publication re-read reject stale work.
- Tradeoff: Completed temporary work may be discarded for policy freshness.

- Failure mode: Enqueue-all listing fails on policy event or periodic tick.
- Mitigation: Keep one pending intent with cancellable backoff; owned/source
  events and later ticks remain active.
- Tradeoff: Convergence may be delayed during control-plane failure without
  blocking a controller worker.

- Failure mode: Future manager configuration enables concurrent workers.
- Mitigation: controller-runtime excludes same-key overlap; registry/tracker
  state is synchronized; results are invocation-local; status uses optimistic
  guards; race tests exercise different keys.
- Tradeoff: Concurrency safety is provided without selecting production worker
  counts or watch limits in this feature.

## Testing Strategy

### Cross-module integration

Extend the external `TestModuleIntegration` suite under `test/integration/`
with the real orchestrator, discovery, policy, selection, extraction, typed
conversion, and result builder. Replace only outbound Kubernetes
reader/watcher/lister/status ports with deterministic scripted adapters.

Named scenarios prove ordered success, mixed partial and all-failed results,
zero sources, fail-closed operation order, absence of WATCH/LIST before allow,
shared-route deduplication, selector-transition routing, deletion cleanup,
watch backoff, deterministic/transient retry classification, semantic
nil/empty equality, generation-only publication, condition preservation,
one-write maximum, cancellation/conflict races, and concurrent key isolation.
Run the same suite with `GO_TEST_FLAGS=-race`. No package-local unit layer is
added.

### Envtest manager and API behavior

Add `TestEnvtestReconciliationRuntime` under `test/envtest/` and select it from
`make test-api`. Start a real controller-runtime manager against pinned envtest,
install Kubeseer/policy plus a disposable observed-resource CRD, and use real
typed, discovery, dynamic, metadata, and status clients. Bounded eventually
assertions and readiness barriers replace arbitrary sleeps.

Scenarios prove existing-object restart enqueue, generation/deletion handling,
status-only suppression, policy fan-out, authorized source create/update/delete,
periodic recovery, queue follow-up, real status persistence, no-op suppression,
observedGeneration changes, selector/source invalidation, and stale rejection.
Narrow test adapters may close a real watch or inject a concurrent real API
write before delegated status update so recovery and resourceVersion conflicts
are deterministic. `make test-compatibility` repeats the API proof on Kubernetes
1.35.6 and 1.36.2; no ambient kubeconfig is used.

## Verification Plan

- Lifecycle proof: envtest counters and bounded status assertions cover
  add/restart/generation/delete, status-only suppression, route cleanup, no
  finalizer, and no reads after observed deletion.
- Authorization proof: operation traces show fresh policy GET, exact
  discovery-derived decision, WATCH setup, and LIST ordering; denied/unavailable
  policy emits zero observed WATCH/LIST requests and no values.
- Event proof: one shared watch routes create/modify/delete to each distinct
  owner; selector enter/leave and source removal converge; forced close recovers
  with backoff and periodic fallback.
- Pipeline proof: real modules produce complete ordered results for success,
  partial failure, all failure, and empty sources.
- Publication proof: counting writer plus envtest status subresource prove
  recursive semantic equality, generation-only change, condition preservation,
  one attempt, conflict retry, and stale/canceled no-write.
- Queue/concurrency proof: barriers inject duplicate triggers during pending and
  active work; assertions show one pending key plus one follow-up, independent
  keys, per-key retry isolation, and race-clean state.
- Confidentiality proof: routes, errors, logs, and failed public outcomes are
  inspected for absence of bodies, values, selector values, and field paths.
- Evidence: `make verify`, `make test-integration`,
  `make test-integration GO_TEST_FLAGS=-race`, `make test-api`, and
  `make test-compatibility` must emit non-vacuous suite markers and pass.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Runtime setup; lifecycle handler; restart/deletion envtest |
| `R2` | Authorized registry; metadata supervisors; policy and target event flows |
| `R3` | Trigger source; queue semantics; retry classifier; concurrency proof |
| `R4` | Direct reads; freshness lease; ordered pipeline; publication guard |
| `R5` | Ordered source slots; partial policy; typed result construction |
| `R6` | Semantic normalization and status publisher; envtest status proof |
| `R7` | Retry classifier; watch backoff; route/result invalidation |
| `R8` | Same-key queue guarantee; synchronized state; leases; race proof |
| `NFR1` | Fresh fail-closed exact-target authorization before WATCH/LIST |
| `NFR2` | Owned/policy/source events, retries, restart, and periodic convergence |
| `NFR3` | Queue coalescing, deterministic ordering, semantic no-op comparison |
| `NFR4` | Per-source outcomes, invocation-local results, per-key failure isolation |
| `NFR5` | Leases, synchronized routing, same-key exclusion, optimistic conflicts |
| `NFR6` | Metadata-only routing and sanitized status/retry/log contracts |
| `NFR7` | Manager, work queue, contexts, API errors, status subresource, resourceVersion |
| `NFR8` | Exact-address watch sharing, key deduplication, periodic fallback |
| `NFR9` | Existing `kubeseer.io/v1alpha1` status/result only; no API changes |
| `NFR10` | Genuine cross-module suite and real-manager envtest compatibility matrix |
