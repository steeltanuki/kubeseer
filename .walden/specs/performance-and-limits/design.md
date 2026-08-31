---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T20:42:19Z
last_modified: 2026-08-31T20:42:19Z
approved_fingerprint: sha256:a08101017d657dde24b1eaab8cab28fd09fc10c9e6ead2f4934ab5bc8e344e90
source_requirements_approved_at: 2026-08-31T19:14:16Z
source_requirements_fingerprint: sha256:77f51845b1483c3f370320f4ac0f982d8bb21c187c4dc28869970adf6fef1bc7
---

# Performance And Limits Design

## Overview

Performance and limits is a manager-wide capacity-control layer. It resolves one
immutable effective profile during setup, injects domain-specific views into
the existing admission and reconciliation boundaries, and rejects invalid
configuration before the controller or admission handler starts.

The runtime keeps the current authorization, planning, freshness, aggregation,
and semantic-status contracts. Its evaluation body changes from stage-wide
batches to a source-at-a-time loop after shared preflight. That loop bounds
temporary state, makes source-level limit failures atomic, and permits a
deadline to retain only source outcomes completed before expiry. No observed
resource body or derived value survives an attempt.

The design also extends existing process-local sharing rather than introducing
new infrastructure: the manager-owned discovery resolver gains bounded LRU
state, exact authorized metadata watches gain a ceiling and deterministic
fallback, and source-trigger ingress coalesces identities before handing them
to controller-runtime's work queue.

## Architecture

```text
manager configuration
        |
        v
limits.Resolve(pointer overrides) --> immutable Profile
        |                                  |
        |                                  +--> controller worker count
        |                                  +--> shared discovery LRU/TTL
        |                                  +--> shared watches/trigger ingress
        |                                  +--> status publisher ceiling
        |
        +--> admission budget adapter --> webhook validation
        |
        +--> runtime budget adapter ----> post-load revalidation
                                           |
freshness lease context -------------------+
                                           v
                                 evaluation timeout context
                                           |
                                  shared source preflight
                         policy -> authorize -> plan -> routes
                                           |
                                   source-at-a-time loop
                    LIST -> extract -> convert -> operate -> aggregate
                      |          incremental canonical-byte accountants
                      +---------- source-atomic limit outcomes
                                           |
                                  result/status composition
                                           |
freshness lease context ------------------> bounded status publication
```

`internal/limits` owns defaults, pointer-based configuration overrides,
validation, immutable profile values, and canonical JSON accounting helpers.
It imports no domain package. Admission, aggregation, selection, discovery,
reconciliation, and status consume narrow profile values or adapters, which
keeps dependency direction acyclic.

`reconciliation.SetupWithManager` remains the runtime composition root. It
resolves the profile once, validates the compact-status floor, constructs one
resolver, one bounded trigger ingress, and one route registry, and passes the
configured `MaxConcurrentReconciles` to the pinned controller-runtime API.
Future flags, environment variables, or ConfigMaps only populate overrides;
they do not alter the profile or enforcement contracts.

## Decision Checkpoint

No user checkpoint is required. Approved requirements decide manager-wide
scope, safe defaults, hard structural ceilings, source-atomic failures,
deadline behavior, compact status, controller-runtime ownership, process-local
sharing, and confidentiality. Repository contracts decide the remaining
composition points.

The implementation choices below are reversible internal details: pointer
overrides distinguish omission from explicit invalid values; accounting is
incremental at producer boundaries; exact watches beyond capacity fall back to
periodic reconciliation; and no payload cache is introduced.

## Options Considered

### Option A: Immutable profile with boundary-local enforcement

- Summary: Resolve one profile at setup and inject only the values and
  accountants required by each existing domain boundary.
- Why chosen: Configuration has one source of truth while admission,
  authorization, selection, conversion, aggregation, status, and routing remain
  authoritative for their own data. The approach supports exact diagnostics
  and tests without a generic interception layer.
- Cost: Several constructors gain options, and the reconciliation pipeline is
  reorganized around one source at a time.

### Option B: Global mutable limiter singleton

- Summary: Let packages read shared mutable limits on demand.
- Why rejected: It would make attempts sensitive to concurrent configuration
  mutation, hide dependencies, complicate race proofs, and make equivalent
  inputs nondeterministic.

### Option C: Truncate collections and status at publication

- Summary: Allow processing to continue, then keep only values that fit.
- Why rejected: Late truncation does not bound intermediate memory, silently
  changes aggregate meaning, cannot identify the authoritative stop boundary,
  and violates source-atomic and explicit-failure requirements.

### Option D: Cache selected resources and derived values

- Summary: Reuse payloads or computed outcomes across attempts to reduce work.
- Why rejected: Payload lifetime would become detached from freshness leases,
  policy epochs, and exact-target authorization. It also creates a sensitive
  process-local data store that the requirements explicitly exclude.

### Option E: Replace controller-runtime scheduling

- Summary: Introduce a Kubeseer worker pool and custom queue with hard bounds.
- Why rejected: Controller-runtime already owns workers, retries, and key
  coalescing. Kubeseer only needs to configure supported concurrency and bound
  the source-event ingress it owns.

## Simplicity And Elegance Review

- Simplest viable shape: one small limits package, extensions to existing
  caches/routes/trigger ingress, and checks at boundaries already holding the
  measured data. No external cache, scheduler, persistence, or public API field
  is added.
- Coupling check: `internal/limits` has no Kubernetes or domain dependencies;
  package adapters convert profile values to existing contracts. Limit checks
  cannot authorize reads, reinterpret values, or publish status directly.
- State check: retained manager state is limited to immutable configuration,
  discovery metadata, exact route identities, pending Kubeseer keys, and
  controller workers. Attempt-local bodies and values are released source by
  source.
- Determinism check: source order, canonical JSON accounting, LRU tie-breaking,
  watch promotion, trigger draining, reasons, and compact status projections
  have explicit stable order.
- Compatibility check: defaults preserve existing page size, admission and
  aggregation budgets, discovery TTL, and successful below-limit behavior.
  The `kubeseer.io/v1alpha1` schema does not change.
- Future-proofing: deployment exposure, autoscaling, distributed caches,
  dynamic reconfiguration, and cluster-wide quotas remain with future packaging
  or scheduling features.

## Components And Interfaces

### Effective limit profile

`internal/limits.Overrides` contains a pointer for every configurable number or
duration. A nil pointer means omitted; a non-nil zero or negative value is an
explicit invalid configuration. Nested admission and aggregation overrides use
the same per-field representation so a partial override cannot accidentally
zero another budget.

`limits.Resolve(Overrides) (Profile, error)` starts from the following defaults,
applies non-nil overrides, rejects non-positive values, rejects admission values
above generated structural ceilings, and returns a profile whose fields are
private and exposed only by value-returning accessors:

| Dimension | Default |
| --- | ---: |
| LIST page size | 500 resources |
| Matched resources per source | 1,000 |
| Selected input per source | 8 MiB |
| Produced values per source and stage | 8 MiB |
| Status candidate | 512 KiB |
| Evaluation duration | 30 seconds |
| Concurrent reconciles | 4 |
| Discovery cache | 1,024 entries, 5-minute TTL |
| Shared exact watches | 1,024 |
| Pending distinct source triggers | 4,096 |
| Aggregation groups/contributions | 1,000 / 10,000 |
| Aggregate collection/distinct/provenance | approved aggregation defaults |
| Admission configuration | approved admission defaults and structural caps |

The package exposes `CanonicalSize` and a stage-local `Accountant`. They encode
the exact retained API or unstructured value shape with deterministic JSON and
check the next retained item before committing it to an output. They do not use
heap estimates or retain encoded bytes. Overflow-safe integer addition converts
arithmetic overflow into the same exceeded result.

Setup maps profile views into `admission.Limits`, `aggregation.Limits`, executor
options, resolver options, route/trigger options, status publisher options, and
controller options. Existing direct constructors continue to select defaults
when no profile option is supplied. Production setup never normalizes an
explicit invalid override back to a default.

Requirements: `R1`, `R2`, `NFR1`, `NFR2`, `NFR6`.

### Admission and runtime configuration budgets

Admission extracts its existing pure budget checks into a reusable
`BudgetValidator`. The webhook invokes it before semantic validation and any
discovery or policy read. The reconciliation runtime invokes the same validator
immediately after loading the current Kubeseer and before policy, discovery,
authorization, route replacement, or resource-instance I/O.

Configured admission budgets may tighten but never exceed generated OpenAPI
ceilings. A violation preserves the existing field-oriented admission response;
runtime revalidation produces sanitized `ConfigurationBudgetExceeded` and no
source read. The policy loader applies the policy view before compiling
authorization state and preserves the approved fail-closed invalid-policy path.
The effective aggregation view replaces direct calls to
`aggregation.DefaultLimits()` during runtime planning.

Requirements: `R2`, `R8`, `NFR3`, `NFR6`.

### Source-local evaluation pipeline

After runtime budget validation, the pipeline retains its shared preflight:
load installation policy, resolve and authorize exact targets, compile source
plans, and replace authorized routes under the current freshness lease. It then
evaluates sources in approved order through one source-local sequence:

1. paginated selection;
2. field extraction;
3. typed conversion;
4. value operators;
5. source aggregation; and
6. terminal source-result assembly.

The selection executor tracks unique UIDs while pages arrive. For every first
occurrence it canonical-encodes the retained unstructured object and checks both
resource count and selected-input bytes before appending it. Duplicate UIDs do
not consume either budget. Exceeding either ceiling stops further pages,
discards all selected objects for that source, and returns
`SelectionLimitExceeded`. Existing final identity ordering remains unchanged.

Extraction, conversion, operators, and aggregation each receive a fresh
stage-local produced-value accountant. A stage checks the exact output shape as
it builds it. Exceeding the ceiling discards that stage's partial output and
returns `ValueLimitExceeded`; downstream adapters preserve the failure without
processing values. Aggregation additionally applies the profile's existing
group, contribution, collection, distinct, and provenance ceilings, preserving
`cardinality-exceeded` where that approved contract owns the failure.

One source failure does not consume or truncate a later source. Once a terminal
source result is assembled, all selected bodies and intermediate values for
that source become unreachable before the next source begins. The final result
preserves approved source order and distinguishes complete, degraded, and
unavailable outcomes without a successful partial value set.

Requirements: `R3`, `R8`, `NFR1`, `NFR2`, `NFR3`.

### Evaluation deadline and freshness composition

Immediately after acquiring a freshness lease for the current object, runtime
creates `evaluationCtx` with the profile timeout as a child of the lease
context. Runtime Kubeseer and policy budget revalidation, policy loading,
discovery, authorization, planning, selection, extraction, conversion,
operators, and aggregation all receive that child context.

The lease context remains separate and is used only to compose and publish the
terminal timeout status. If `evaluationCtx` reaches its deadline while the lease
is current, runtime cancels in-flight work, discards the active source's
temporary state, preserves earlier terminal source results, and asks the result
assembler for `EvaluationTimedOut` failures for the active and unstarted
sources. It then publishes that current-generation degraded result through the
normal status path and returns a retryable, rate-limited runtime error.

If the lease context itself is canceled, runtime does not use a detached
context, does not publish, and returns the existing stale/canceled outcome. A
deadline detected after all source results are complete does not rewrite a
completed evaluation. The timeout helper and source-result assembler use only
stable reasons and source IDs already present in the specification.

Requirements: `R4`, `R6`, `NFR2`, `NFR3`.

### Bounded status publisher

`StatusPublisher` receives the effective maximum status bytes. It first composes
the ordinary current-generation candidate, canonical-encodes the exact status
subresource, and measures it before semantic comparison or API write.

If the candidate exceeds the ceiling, `internal/status` composes a dedicated
limit evaluation: it omits `result`, `summary`, and `resultHash`; preserves
`Accepted`, `Authorized`, and `SourcesResolved`; sets `Ready=False` and
`Degraded=True` with `ResultLimitExceeded`; and retains the evaluated
`observedGeneration`. The publisher measures that compact candidate again.
Only a fitting candidate enters the existing semantic suppression and conflict
safe write path.

`internal/status` exposes a setup validation helper that builds the largest
fixed-shape compact limit candidate accepted by the contract and reports its
canonical size. Composition rejects a configured status ceiling below this
floor before startup. The second runtime measurement remains a defensive guard;
failure is sanitized as `StatusLimitInvalid` and issues no write. Later fitting
results naturally replace compact status through the existing semantic path.

Requirements: `R5`, `R8`, `NFR1`, `NFR2`, `NFR3`.

### Bounded controller workers and trigger ingress

Setup copies the effective worker ceiling into
`controller.Options.MaxConcurrentReconciles`; controller-runtime continues to
own worker creation, queue coalescing, retries, and shutdown.

`TriggerSource` gains an identity-only ingress between route watches and the
controller queue. Under one mutex it retains a set of pending
`types.NamespacedName` values, a single buffered wake signal, and one overload
flag. Duplicate keys coalesce. A distinct key is accepted only below capacity;
otherwise a non-blocking operation sets one enqueue-all recovery intent and
returns to the producer. The drain removes keys in namespace/name order before
adding them to controller-runtime, then accepts new distinct keys immediately.

The recovery intent reuses the existing enqueue-all path and periodic safety
tick. It contains no observed-object identity or payload. Existing freshness
leases serialize competing attempts for the same Kubeseer, while different
workers keep contexts and temporary values local.

Requirements: `R6`, `R7`, `NFR1`, `NFR3`, `NFR5`.

### Bounded discovery cache

The one resolver built by manager setup remains shared across reconciliations.
Each cache entry gains an access sequence in addition to its existing expiry;
the resolver updates that sequence under the cache mutex on a valid hit or
insert. Concurrent misses continue to share the existing in-flight refresh.

Before inserting at capacity, the resolver evicts the non-refreshing entry with
the smallest access sequence. Canonical group/version/resource key order breaks
ties. In-flight refresh state is separate and is never selected for eviction.
Existing explicit discovery invalidation removes matching entries before reuse.
If cache insertion or retention is unavailable, the caller uses the fresh
discovery response for the current plan without making correctness depend on
cache state.

Cache keys and values contain discovery metadata only. Authorization remains
after policy loading and before resource-instance I/O; cache freshness never
extends a capability or route epoch.

Requirements: `R7`, `NFR1`, `NFR4`, `NFR5`.

### Bounded shared watches

`RouteRegistry` continues to share one metadata watch for each exact authorized
target address and to retain owner bindings separately from supervisors. On
route replacement it sorts newly required addresses canonically and creates
supervisors only while below the profile ceiling. Bindings beyond capacity stay
registered without a watch, so their owners remain covered by periodic safety
reconciliation.

When an address loses its last authorized owner, its watch is canceled and all
state for that address is removed. Available capacity is then offered to the
lowest canonical bound address that currently lacks a supervisor. Policy-epoch
replacement and owner removal use the same path, so stale authorization cannot
retain a watch. Watch identities contain only API group/version/resource,
scope, and configured namespace, never observed object identities or selectors.

Requirements: `R7`, `R6`, `NFR3`, `NFR4`, `NFR5`.

### Limit diagnostics and observability

Approved reason vocabularies gain `InvalidLimitConfiguration`,
`SelectionLimitExceeded`, `ValueLimitExceeded`, `EvaluationTimedOut`,
`ResultLimitExceeded`, and `StatusLimitInvalid` only at stages where those
reasons are authoritative.

Every exceeded runtime ceiling emits one source- or reconciliation-scoped
observation through the existing typed observer. The record includes fixed
`dimension`, configured numeric `ceiling`, approved stage, outcome, and reason.
It excludes the observed count when that could reveal cardinality and excludes
all bodies, object identities, selectors, paths, values, causes, and Secret
data. Existing bounded-cardinality failure counters use only approved stage and
reason labels; dimension and ceiling remain structured log/span fields, never
metric labels.

Limit observation is best-effort and cannot change a domain outcome. Equivalent
profiles and ordered inputs select the same boundary and reason regardless of
whether a telemetry sink is enabled.

Requirements: `R8`, `NFR2`, `NFR4`, `NFR7`.

## Data Models

### Configuration state

```text
Overrides                         Profile
---------                         -------
*int / *int64 / *duration  -->    positive concrete values
nil = omitted                     private fields
non-nil <= 0 = invalid            value-copy accessors
structural over-cap = invalid     no runtime mutation
```

There is no persisted configuration state and no CRD field. A profile belongs
to one manager process and is copied by value into long-lived components.

### Attempt-local accounting

An accountant contains only a positive ceiling, current byte total, and fixed
dimension. It is reset for every source/stage boundary and discarded with the
attempt. Selection additionally holds a source-local UID set and retained
resource slice. Neither structure crosses a source boundary.

### Shared bounded state

| Owner | Retained state | Bound |
| --- | --- | --- |
| Discovery resolver | metadata key, mapping, expiry, access sequence | cache entries |
| Route registry | authorized exact owner/address bindings, supervisors | watches bounded; bindings bounded by admitted spec population |
| Trigger source | pending Kubeseer keys, wake, overload intent | pending identities plus one intent |
| Controller-runtime | reconciliation keys and workers | workers bounded by profile; queue semantics owned upstream |

Route bindings are configuration identities already bounded by admission and
Kubeseer population; observed source-event identities never enter retained
route or trigger state.

## Error Handling

- Invalid or non-positive overrides fail manager setup with
  `InvalidLimitConfiguration`; no controller, webhook, cache, or watch starts.
- Runtime budget drift fails before discovery, policy, authorization, route, or
  source I/O and preserves field-safe diagnostics.
- Selection count/input overflow stops pagination and produces
  `SelectionLimitExceeded` for only that source.
- Produced-value overflow discards the active stage output and produces
  `ValueLimitExceeded`; approved aggregation cardinality failures keep their
  existing reason.
- Evaluation deadline publishes a current timeout result only while the
  freshness lease remains valid, then returns through the existing rate-limited
  retry path.
- Oversized ordinary status is replaced by compact limit status; an impossible
  compact write fails with `StatusLimitInvalid` and no API write.
- Trigger or watch overload degrades to one enqueue-all intent or periodic
  safety reconciliation; producers do not block indefinitely.
- Discovery eviction or a cache miss changes efficiency only. Failed refreshes
  retain existing discovery error semantics and never authorize a read.

All wrapped causes stay internal. Status, logs, metrics, and Events contain only
stable approved reasons and sanitized fixed fields.

## Security Considerations

- Limits are enforced after current policy/capability checks where data access
  requires them; caches and watch sharing cannot bypass exact-target
  authorization.
- The discovery cache stores API metadata only. Resource bodies and every
  derived value remain attempt-local and are released source by source.
- Trigger state stores Kubeseer owner identities, not observed source-object
  identities. Route state stores only configured authorized target addresses.
- Diagnostics never expose payloads, selectors, field paths, values, Secret
  data, wrapped causes, or sensitive observed cardinalities.
- Freshness lease cancellation takes precedence over timeout publication, so a
  stale worker cannot write even when a limit is exceeded.
- Manager-wide limits are not user-controlled CR fields and cannot raise fixed
  structural API ceilings.

## Failure Modes And Tradeoffs

- Failure mode: Canonical encoding of the next value is itself expensive.
  Mitigation: source input is bounded first, stages account before retention,
  and no stage output survives failure. Tradeoff: canonical API-size accounting
  is deterministic but is not an exact heap allocation limit.
- Failure mode: A deadline occurs between evaluation completion and status
  publication. Mitigation: completion is recorded before checking the deadline,
  and publication uses the still-current lease context. Tradeoff: status API
  latency is governed by the parent reconcile context, not the evaluation
  budget.
- Failure mode: Source-at-a-time evaluation performs less batch sharing.
  Mitigation: compiled plans and manager metadata remain shared; source results
  are independent by contract. Tradeoff: bounded temporary memory and precise
  timeout retention are preferred over batch throughput.
- Failure mode: Watch capacity omits event-driven refresh for some authorized
  routes. Mitigation: deterministic promotion and periodic safety
  reconciliation preserve eventual correctness. Tradeoff: affected owners may
  observe up to one safety interval of additional latency.
- Failure mode: Trigger overload collapses distinct events into enqueue-all.
  Mitigation: one non-blocking recovery intent and the safety tick preserve
  progress. Tradeoff: overload may perform broader reconciliation work.
- Failure mode: LRU eviction causes repeated discovery calls under churn.
  Mitigation: concurrent miss collapse and TTL remain. Tradeoff: strict memory
  bounds take precedence over maximum cache hit rate.
- Failure mode: A status ceiling is below the compact contract's fixed shape.
  Mitigation: setup validates the compact sentinel and the publisher rechecks.
  Tradeoff: such a profile is rejected instead of attempting an ambiguous
  smaller status.

## Testing Strategy

Per the repository constitution, proof begins at genuine cross-module
integration rather than a new package-local unit-test layer.

Cross-module integration extends the real admission-to-status pipeline with
small profiles and deterministic dependency doubles. It proves omitted versus
explicit-invalid configuration, structural tightening, page/count/input-byte
selection boundaries, per-stage value accounting, aggregation limits,
source-atomic continuation, deadline retention, compact status, diagnostics,
default-profile compatibility, LRU/miss collapse, watch promotion, trigger
coalescing, overload recovery, and multi-worker isolation.

Envtest uses the existing manager and status suite against a local API server.
It proves controller concurrency wiring, runtime revalidation before reads,
shared-watch fallback with periodic recovery, timeout publication, compact
status write/suppression/recovery, and stale-lease protection through real
status-subresource behavior.

Race-enabled higher-layer tests stress discovery hits/refresh/eviction, route
replace/remove/promotion, trigger accept/drain/overflow, and concurrent
reconciles. Existing below-limit feature proofs remain green under the default
profile. Generated CRD verification confirms that no public schema change or
raised structural ceiling appears.

## Verification Plan

- Profile and admission proof: integration exercises every default, partial
  override, explicit non-positive value, structural over-cap, and runtime drift;
  no read recorder fires for rejected configuration.
- Resource/value proof: paginated integration inputs cross count and byte
  boundaries by one canonical item at selection, extraction, conversion,
  operator, and aggregation stages; later sources still complete and no partial
  successful value set appears.
- Deadline proof: a context-aware stage double expires during an active source;
  prior sources remain, active/unstarted sources report `EvaluationTimedOut`,
  the lease-current status is written, and stale cancellation suppresses it.
- Status proof: envtest writes compact `ResultLimitExceeded`, verifies omitted
  result/summary/hash and preserved conditions/generation, suppresses an
  equivalent repeat, then restores a normal fitting result.
- Concurrency/backpressure proof: manager setup observes no more than configured
  parallel reconciles; duplicate triggers coalesce; overflow is non-blocking and
  produces one enqueue-all recovery; acceptance resumes after drain.
- Sharing proof: concurrent discovery misses invoke one refresh, LRU eviction is
  deterministic, exact targets share one watch, excess targets use safety
  reconciliation, and released capacity promotes the canonical waiting target.
- Confidentiality proof: capture observers assert the stable stage/reason,
  dimension, and ceiling and reject prohibited payload/identity/value fields;
  failure metrics use only bounded approved labels.
- Regression proof: `make verify`, `make test`, `make test-integration`, and
  `make test-api` retain all existing feature markers and add
  `MODULE_INTEGRATION=performance-and-limits STATUS=passed` plus
  `API_CONTRACT=performance-and-limits STATUS=passed`.
- Race proof: `go test -race ./test/integration` exercises the higher-layer
  cache, route, trigger, and worker scenarios with writable isolated Go caches.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Effective profile, setup adapters, defaults, immutable configuration |
| `R2` | Shared admission/runtime budget validator and structural-cap checks |
| `R3` | Source-local pipeline, incremental accounting, atomic failure outcomes |
| `R4` | Layered evaluation deadline, terminal source retention, retry behavior |
| `R5` | Pre-write measurement, compact status composer, semantic recovery |
| `R6` | Controller worker option, bounded trigger ingress, freshness leases |
| `R7` | Manager discovery LRU, exact-watch sharing/fallback, no payload cache |
| `R8` | Stable outcomes, typed observations, bounded metrics, production proofs |
| `NFR1` | Positive finite profile and bounded attempt/shared state |
| `NFR2` | Canonical accounting, stable order, reasons, and compact projections |
| `NFR3` | Explicit atomic failures, freshness-safe publication, fallback retries |
| `NFR4` | Authorization-preserving metadata sharing and sanitized diagnostics |
| `NFR5` | Bounded workers, cache, watches, triggers, and duplicate work |
| `NFR6` | No CRD shape change; configurable budgets only tighten hard ceilings |
| `NFR7` | Cross-module, envtest, race, default-regression, and schema proofs |
