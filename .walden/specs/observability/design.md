---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T16:16:10Z
last_modified: 2026-08-31T16:16:10Z
approved_fingerprint: sha256:169d2a8089da98599481ba8514e9284dabbd58f66b9b7f034ff9d6463ad064d8
source_requirements_approved_at: 2026-08-31T15:45:49Z
source_requirements_fingerprint: sha256:fc0f60f66ba3a9dacab52b013b52a43324c9b2aa62de778e00446b32ccb9fca5
---

# Observability Design

## Overview

Observability is a passive, manager-wide instrumentation layer that receives
already-sanitized facts at the production boundaries where their meaning is
authoritative. It never inspects Kubernetes payloads, repeats domain decisions,
or performs reads for enrichment.

The feature adds `internal/observability` as the owner of signal vocabulary,
correlation, Prometheus collectors, structured log emission, optional
OpenTelemetry spans, and Kubernetes Event selection. Existing modules retain
their behavior and expose narrow observation points:

- `internal/authorization` continues to emit ordered `authorization.Record`
  values through its existing `Recorder` boundary;
- `internal/selection` reports only scope and item count for successful LIST
  pages;
- `internal/reconciliation` reports attempt, stage, source, result, status, and
  watch outcomes already computed by the pipeline; and
- the status publisher records an Event only after a successful semantic status
  write.

Runtime observation methods are best-effort and cannot return a replacement
domain result. Configuration and collector registration are validated during
manager setup. Tracing is disabled unless a manager-wide provider is supplied.
The implementation owns no asynchronous queue or durable history.

## Architecture

```text
authorization.Record -------- authorization adapter ----+
successful LIST page -------- selection observer -------+
reconcile/stage/source result runtime observer ----------+--> structured logs
watch lifecycle ------------ route observer ------------+--> Prometheus metrics
status publication --------- status observer -----------+--> optional spans
                                                           --> Kubernetes Event
                                                               after status write
```

`reconciliation.SetupWithManager` is the composition root. It constructs one
observability instance from the manager logger, controller-runtime Prometheus
registry, manager Event recorder, clock/ID defaults, and optional trace
provider. It injects adapters into the enforcer, executor, route registry,
status publisher, and runtime. Direct runtime construction defaults to a no-op
observer, preserving existing callers.

The dependency direction is:

```text
reconciliation --> observability --> authorization
          |              |
          +------------> selection
```

Observability adapts producer-owned authorization and selection contracts but
does not import reconciliation. Runtime/status/watch DTOs are defined in the
observability package and consumed by reconciliation, avoiding cycles.

## Decision Checkpoint

No user checkpoint is required. Approved requirements already decide the
signal families, Event priority, confidentiality boundary, manager-wide scope,
and tracing default. The pinned controller-runtime, Prometheus, and
OpenTelemetry APIs provide the required registry, logger, Event recorder, and
context-based tracing composition points.

The remaining local choices are reversible: use typed observers instead of a
generic bus; preserve constructors through optional arguments; reject duplicate
collector registration; and keep emission synchronous from Kubeseer's
perspective while an external trace provider may own its own batching.

## Options Considered

### Option A: Typed passive observer at authoritative boundaries

- Summary: One observability package plus small producer hooks carrying only
  sanitized, already-classified facts.
- Why chosen: It centralizes taxonomy and sink behavior while domain modules
  remain authoritative. Tests gain replaceable capture points without a broker,
  duplicate state, or API fields.
- Cost: The composition root gains explicit adapters, and the private pipeline
  must expose a terminal classification for exactly-once completion.

### Option B: Direct telemetry in every domain package

- Summary: Authorization, selection, status, and reconciliation each construct
  logs and collectors.
- Why rejected: Taxonomy, confidentiality, registration, and failure isolation
  would be duplicated; domain code would depend on telemetry libraries and
  equivalent outcomes could drift.

### Option C: Generic asynchronous event bus

- Summary: Publish internal events to a buffered broker with sink consumers.
- Why rejected: Queue limits, shutdown, ordering, drops, and lifecycle add
  complexity without a durable/deferred-delivery requirement. Asynchrony also
  weakens authorization audit ordering before resource I/O.

### Option D: Controller-runtime generic telemetry only

- Summary: Use built-in reconcile metrics and controller logs.
- Why rejected: Generic signals cannot express authorization evidence,
  semantic status transitions, source-stage and JSONPath failures, or the nine
  required metric families.

## Simplicity And Elegance Review

- Simplest viable shape: one package, one manager-owned instance, typed DTOs,
  and narrow options. No custom metrics server, durable store, broker, or API
  configuration is added.
- Coupling check: producers pass facts they already own. Observability cannot
  authorize, select, compose/publish status, or request retries.
- State check: only collector state and per-attempt handles exist. There is no
  retained audit history or global correlation map.
- Compatibility check: `NewExecutor` and `NewRouteRegistry` already accept
  options; `NewStatusPublisher` gains variadic options; runtime dependencies gain
  an optional observer. Existing call sites remain valid.
- Future-proofing: exporters, dashboards, ServiceMonitor resources, production
  logger settings, and performance budgets remain with their owning features.

## Components And Interfaces

### Observability setup

`internal/observability.New` accepts a `logr.Logger`, Prometheus `Registerer`,
manager `record.EventRecorder`, optional `trace.TracerProvider`, and injectable
clock/opaque attempt-ID generator. Higher-layer tests may also provide capture
adapters.

`reconciliation.Options` gains an internal manager-wide observability option
whose only runtime-specific setting is the optional trace provider; logger,
registry, and Event recorder always come from the owning manager. The default
attempt-ID generator is concurrency-safe and produces a fresh opaque UUID;
tests replace it deterministically.

It creates and registers all nine collectors with `Register`, never
`MustRegister`. Any error, including `AlreadyRegisteredError`, returns the
stable sanitized setup reason `ObservabilitySetupInvalid` and prevents manager
startup. A partially configured instance is never returned.

Production passes `controller-runtime/pkg/metrics.Registry`, the manager logger,
and `GetEventRecorderFor`. A nil trace provider means no tracing, no exporter,
and no collector requirement. Logs, metrics, and Events otherwise use manager
facilities; no second server is started.

### Correlated attempt and stage handles

`Observer.StartAttempt(ctx, AttemptStart)` creates an immutable handle, adds
namespace/name/opaque attempt ID to logger context, optionally starts the root
span, and emits `ReconciliationStarted`. After load, `BindObject` derives a new
context with Kubeseer UID/generation rather than mutating shared state.

The runtime wrapper calls `Complete` once from one deferred path. Private
`reconcileKey` returns an internal terminal classification alongside its
existing controller result/error. The classification contains only authoritative
outcome, reason, and retry state and cannot alter the controller result.

`StartStage` returns a child context and idempotent completion function. Fixed
stage names match current boundaries: `load`, `validate`, `authorize`, `plan`,
`read`, `extract`, `aggregate`, `compose`, and `publish`. Completion records only
stage, outcome, stable reason, and retry classification, then ends the span.

### Structured log emitter

Every runtime log uses a common envelope containing `event`, `outcome`,
`reason`, `severity`, and available correlation fields. A start uses the fixed
outcome `started` and reason `ReconciliationStarted`; terminal values come from
the authoritative runtime classification. Fixed signal-specific fields make
equivalent outcomes deterministic:

| Signal | Event code | Required safe fields |
| --- | --- | --- |
| Attempt accepted | `ReconciliationStarted` | namespace, name, attempt ID |
| Absent/deleting attempt | `ReconciliationSkipped` | correlation, outcome, reason, elapsed |
| Retry returned | `ReconciliationRetryScheduled` | correlation, reason, retry, elapsed |
| Terminal failure | `ReconciliationFailed` | correlation, reason, retry, elapsed |
| Other completion | `ReconciliationCompleted` | correlation, outcome, reason, elapsed |
| Source terminal failure | `SourceFailed` | correlation, source ID, stage, reason, retry |
| Status written/skipped | `StatusUpdateWritten` / `StatusUpdateSkipped` | correlation, outcome, reason |
| Unexpected watch stop | `SourceWatchStopped` | authorized target identity, reason, retry |
| Scheduled watch restart | `SourceWatchRestarted` | authorized target identity, reason, retry |
| Policy/read evidence | `AuthorizationDecision` / `AuthorizationReadForbidden` | allowlisted record fields |

Starts, successes, writes, and skips are informational; retryable/degraded
outcomes are warnings; terminal internal failures are errors. Raw errors and
wrapped causes never reach the emitter. Active trace/span IDs are copied from
context into correlated logs; absent tracing simply omits them.

### Authorization adapter

The adapter implements existing `authorization.Recorder`. For every record, in
slice order, it chooses the event code from record kind, copies only
`authorization.Record` fields, emits one audit record, and increments one
authorization metric. Policy identity appears only when the record carries it.
The adapter stores no history and returns no error. Existing enforcer placement
preserves audit exposure before authorized resource-instance I/O.

### Selection page observer

Selection gains a `PageObserver` option accepting only `{scope, itemCount}`.
The executor invokes it after every successful non-nil LIST response and before
object inspection. Re-read pages after an expired-resource-version restart are
counted again because the metric describes actual returned resources. No
object, selector, path, target name, or payload crosses the interface.

### Runtime outcome observers

The pipeline emits only finalized facts:

- one completion per attempt;
- one source failure per source retained as terminally failed;
- one JSONPath failure per compilation/evaluation failure retained in that
  terminal source outcome; and
- one result-produced observation per publishable complete/degraded snapshot.

Exhaustive conversion functions map existing enums to finite vocabularies.
Unknown values become `InternalError`; they are never derived from
`error.Error()`.

### Status observer and Kubernetes Event recorder

`NewStatusPublisher` gains a variadic no-error observer option. Each publication
reports exactly one of `written`, `skipped`, `conflicted`, or `failed`, driving
one metric update and eligible written/skipped log.

Only after a successful semantic write does the publisher pass the already
loaded Kubeseer and persisted candidate status to Event recording. The primary
reason is the first non-successful canonical condition in this order:

1. `Accepted`;
2. `Authorized`;
3. `SourcesResolved`;
4. `Ready`.

All-success selects `EvaluationSucceeded`. It emits `Normal`; every other
reason emits `Warning`. The Event message is the selected condition's sanitized
message. Missing/malformed canonical data falls back to Warning/InternalError.
Skipped, conflicted, or failed publication never invokes the recorder. The
fire-and-forget recorder cannot roll back a successful status write.

### Watch lifecycle observer

The route registry gains a watch observer. It emits `SourceWatchStopped` when a
watch terminates unexpectedly. Immediately before a replacement watch is
scheduled, it emits `SourceWatchRestarted` and increments one restart metric.
Expected shutdown caused by owning-context cancellation emits neither signal.
Logs may include authorized target API group, kind/resource, scope, and
namespace; these identify the configured target, not an observed object. Only
the fixed reason becomes a metric label. Existing forbidden/expired/unavailable
classifications are retained; an unknown failure becomes `InternalError`.

### Sink isolation

Eligible log, metric, trace, Event, and capture adapters are invoked
independently. Adapter methods cannot return domain outcomes; delivery errors
are consumed and later sinks still run. Replaceable capture adapters recover
their own panics so a diagnostic extension cannot unwind reconciliation.

Kubeseer owns no asynchronous queue. Therefore the conditional queue rules do
not activate. Any future owned queue must have finite capacity and deterministic
drop/coalesce behavior. An external OpenTelemetry provider owns any processor
buffering; Kubeseer only starts/ends spans through its interface.

## Data Models And Stable Vocabulary

Observation DTOs are immutable allowlisted structs containing only:

- Kubeseer namespace/name, opaque attempt ID, and after load UID/generation;
- source ID and fixed stage;
- authorized watch target API group/resource/scope/namespace;
- fixed event, outcome, reason, retry, and severity classifications;
- non-negative duration and LIST item count; and
- fields already exposed by `authorization.Record`.

Metric label constructors use private types and exhaustive switches, not
arbitrary strings. Values are approved domain outcomes/reasons plus
`InternalError`; scope is `namespaced` or `cluster`; stage and kind are closed
constants. No user identifier/message can enter a label constructor.

| Metric | Observation point | Labels |
| --- | --- | --- |
| `kubeseer_reconciliations_total` | attempt completion | outcome, reason |
| `kubeseer_reconciliation_duration_seconds` | attempt completion | outcome |
| `kubeseer_resources_read_total` | successful LIST page | scope |
| `kubeseer_source_failures_total` | terminal source failure | stage, reason |
| `kubeseer_results_produced_total` | publishable result | outcome |
| `kubeseer_status_updates_total` | publication completion | outcome, reason |
| `kubeseer_authorization_decisions_total` | each authorization record | kind, outcome, reason |
| `kubeseer_jsonpath_failures_total` | retained terminal JSONPath failure | reason |
| `kubeseer_source_watch_restarts_total` | restart scheduling | reason |

Prometheus collectors provide concurrency-safe updates. Attempt duration is
computed once from the injected monotonic clock, clamped to zero if a test clock
moves backwards, and observed with the same outcome as its counter. No CRD,
persisted audit model, or telemetry history store is introduced.

## Data Flow

### Reconciliation

1. Accept a namespaced key and start an attempt before the first read.
2. Emit `ReconciliationStarted` and establish logger/root-span context.
3. After load, derive context with Kubeseer UID/generation.
4. Execute existing stages; observation wraps but cannot replace outcomes.
5. Emit authorization records before capability-backed resource I/O.
6. Report only scope/count for successful LIST pages.
7. Report authoritative status outcome; after a semantic write, record one
   Event.
8. Map final result/error plus terminal classification to exactly one terminal
   log, counter increment, duration observation, and root-span end.

### Watch restart

The supervisor classifies an unexpected stop and emits its sanitized lifecycle
record. If it schedules a replacement, it emits the restart observation before
waiting/restarting, then follows existing backoff regardless of sink success.

### Status Event

Semantic comparison runs first. A skipped candidate reports skip and stops. A
changed status is written, then and only then the primary canonical condition
selects one Event.

## Error Handling

- Invalid manager facilities/configuration or collector registration return
  `ObservabilitySetupInvalid` before manager startup.
- Runtime observer APIs do not return errors to domain callers. Delivery loss
  never creates retry, authorization, or status changes.
- Unknown classifications use `InternalError`; raw/wrapped errors are dropped.
- Absent/deleting objects complete as `ReconciliationSkipped` without invented
  UID/generation.
- Retryable failures use `ReconciliationRetryScheduled`; non-retryable failures
  use `ReconciliationFailed`; every attempt has one terminal code.
- Status conflict/failure emits no Event. Event and trace export cannot roll
  back authoritative work.
- Stale/canceled telemetry carries only classification, never candidate values.

## Security Considerations

DTOs are allowlists, never free-form maps. They cannot carry observed bodies or
object identities, selectors, paths, extracted/typed/aggregate values, Secret
data, or Kubernetes errors. Authorization logs copy only
`authorization.Record`.

Kubeseer/source/policy/authorized-target identity is permitted only as
structured log/trace/Event correlation where approved and never as metric
labels. Events use canonical sanitized status messages and only the owning
Kubeseer. Instrumentation makes no discovery, GET, LIST, WATCH, or status reads,
so it cannot expand authorization through enrichment.

## Failure Modes And Tradeoffs

| Failure mode | Containment | Accepted tradeoff |
| --- | --- | --- |
| Duplicate/failed collector registration | Reject setup with sanitized reason | Startup fails instead of binding an incompatible collector |
| Logger/capture adapter unavailable | Suppress failure; continue sinks/domain | Some evidence may be lost |
| Event delivery fails after status write | Keep persisted status | Transition may be absent from Events |
| Trace provider/exporter unavailable | Disabled no-op or passive provider failure | Traces are best-effort |
| Unknown classification | Emit `InternalError` without cause | Less detail prevents leakage/cardinality drift |
| Repeated watch restarts | One signal per actual restart, fixed labels | Log volume can rise; cardinality stays bounded |
| Crash between action and emission | No replay/history | Durable audit storage is out of scope |
| Capture adapter panic | Recover at replaceable adapter boundary | Core programmer faults remain setup/test failures |
| Concurrent attempts | Immutable contexts and safe collectors | No global queryable attempt registry |

## Testing Strategy

Tests follow the repository's higher-layer-only policy; no package-local unit
suite is introduced.

Extend existing `TestModuleIntegration` with the real runtime and replaceable
logger, fresh registry, authorization adapter, Event capture stub, and in-memory
trace exporter. Scenarios prove exact start/terminal records; correlation;
source/JSONPath/result/status/LIST/watch/authorization metrics; ordered audit
before I/O; fixed vocabularies; duplicate-registration rejection; optional span
hierarchy; forbidden-field absence; and unchanged outcomes under sink failure.

Synchronized concurrent scenarios assert exact gathered metrics, disjoint
attempt/trace contexts, and race-detector cleanliness.

Extend the existing reconciliation-runtime envtest suite, rather than adding a
test layer. Cause a real semantic status transition and assert exactly one
sanitized Event with expected involved object/type/reason/message. Reconcile an
unchanged object and exercise conflict/failure to prove no extra Event.

Existing API/CRD compatibility checks prove no schema field was added.

## Verification Plan

- Run `walden validate observability --json` for complete design coverage.
- Run `make verify` for formatting, generation, test-layer, and API checks.
- Run `make test-integration` for production runtime, authorization, registry,
  log, metric, trace, concurrency, and failure-injection evidence.
- Run the existing reconciliation-runtime envtest suite with local
  `KUBEBUILDER_ASSETS` for Event persistence and suppression.
- Run cross-module integration with `-race` for contexts/metric accounting.
- Gather the fresh registry and assert exact nine names, label names, finite
  values, counts, and forbidden-label absence.
- Scan every captured signal for sentinel bodies, object identity, selector,
  path, value, Secret, and raw/wrapped error text.
- Verify no trace provider and failing capture/export adapters preserve domain
  outcomes.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Attempt/stage handles, fixed log table, completion/status/watch flows |
| `R2` | Authorization adapter, sink isolation, ordered production-boundary tests |
| `R3` | Setup registration, exact collector table, finite constructors, concurrency tests |
| `R4` | Status observer, Event selection, envtest persistence/suppression |
| `R5` | Optional root/stage spans, log correlation, capture exporter tests |
| `R6` | Allowlisted DTOs, exhaustive taxonomy, forbidden-sentinel scans |
| `R7` | No-error observers, independent sinks, immutable contexts, no queue/API fields |
| `R8` | Existing cross-module integration and reconciliation envtest extensions |
| `NFR1` | Allowlisted evidence and captured-signal confidentiality assertions |
| `NFR2` | Fixed event/field/label/span vocabulary and deterministic Event priority |
| `NFR3` | Exact labels, private finite constructors, forbidden-label assertions |
| `NFR4` | Sink isolation, failure injection, post-write Event semantics |
| `NFR5` | Immutable contexts, Prometheus-safe collectors, race scenarios |
| `NFR6` | Manager registry/Event recorder and optional context tracing |
| `NFR7` | Compatible options, no CRD changes, upstream reason reuse |
| `NFR8` | Production-path capture, fresh registry, trace exporter, envtest Events |
