---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-10T06:13:03Z
last_modified: 2026-09-10T06:13:03Z
approved_fingerprint: sha256:61e65111fe98c27a896b9bd61c24a7dc29037e1a31f461142ea11b8c2967a482
source_requirements_approved_at: 2026-09-06T08:34:51Z
source_requirements_fingerprint: sha256:b7fc29114013c073ed9305c55ae4ffcbac87f67ba01f194185bd016c8be9c01f
---

# Feature Design

## Overview

Give RouteManager.Replace an explicit caller context for readiness waiting.
Keep shared WATCH transports owned by registry supervisors. Bound the
establishment phase of each transport independently, and dispose of late
responses after cancellation. This corrects blocked workers without tying a
shared stream to one reconciliation's lifetime.

## Architecture

```text
Runtime evaluation context ──→ Replace(ctx, lease, routes)
                                      |
                              bounded readiness wait
                                      |
manager context ──→ registry ──→ supervisor ──→ WATCH transport
                                      |
                           establishment timer only
                                      |
                        established stream / retry backoff
```

No registry or supervisor lock is held across a network request or readiness
wait. The supervisor remains the only goroutine that opens and consumes its
target's transport.

## Options Considered

### Option A — Separate caller wait and shared transport lifetime (selected)

Change Replace to accept evaluationCtx; its waits select caller cancellation,
supervisor completion, and manager shutdown. A supervisor-owned startup timer
cancels a transport that has not established. Once established, stop that timer
and retain the supervisor cancellation context for the stream.

### Option B — Run WATCH directly under evaluationCtx

This releases the worker but normal end-of-reconciliation cancellation would
also close a stream needed by other owners. Sharing would become unstable.

### Option C — Spawn a goroutine around the existing blocking Replace

The caller could return but blocked work would accumulate outside controller
concurrency limits. It also leaves late results and stale owner state unmanaged.
Reject this unbounded detachment.

## Simplicity And Elegance Review

Reuse the current supervisor, exact-address indexes, capacity limits, trigger
ingress, and retry loop. Add one context parameter and explicit startup state,
not a second watcher pool. Generalize promotion launch once for Replace,
RemoveOwner, registry startup, and stale-binding cleanup.

<!-- assumed: use the existing profile EvaluationTimeout as the transport establishment bound; the caller still observes its own earlier deadline (source: approved C2 and R1.AC1) -->

## Components And Interfaces

### Runtime and RouteManager port

Change the internal signature to Replace(context.Context, Lease,
[]AuthorizedRoute) error and update all implementations and integration adapters.
Runtime supplies evaluationCtx. Empty replacements remove bindings without
waiting on other targets' transports.

On Replace error, check evaluationState before generic retry classification:
a deadline with a current lease enters finishTimedOutEvaluation; lease or external
cancellation returns the skipped outcome without publishing. A new queued
generation can then acquire its existing per-key execution gate.

Requirements: R1.AC1–R1.AC7.

### Registry launch and readiness wait

Register each supervisor with a single startup notification before launching
its run loop. Reuse that notification when another owner joins while establishment
is pending; do not allocate an untracked waiter goroutine. Notification closes
exactly once on first-attempt completion or supervisor stop. A successful or failed
first attempt is distinguishable from caller cancellation via the selected
context, with a final context check to handle simultaneous readiness.

Index updates and capacity promotion occur under the registry mutex. Launch
supervisors after unlock, with registration rechecked before start. Waiting is a
separate operation performed only by Replace for its requested active targets.
RemoveOwner, stale pruning, and lifecycle callbacks launch promotions without
waiting. They can therefore revoke ownership while an unrelated target stalls.

On a canceled Replace, prune only stale bindings for the caller's owner/targets.
A still-current owner's evaluation timeout does not revoke authorization.
Never delete another generation's bindings or another owner's current routes.

Requirements: R1.AC2/R1.AC3/R1.AC6, R2.AC1–R2.AC3, R2.AC9.

### Supervisor establishment bound and late responses

Add a registry option populated from the resolved profile EvaluationTimeout in
SetupWithManager. Direct composition defaults to that profile's default.
No new command flag or Helm setting is introduced.

For each serial attempt:
1. Resolve a current exact-target WatchPermit.
2. Create a transport cancellation context under the supervisor context.
3. Arm an establishment timer for the configured bound.
4. Call MetadataWatcher.Watch directly from the supervisor goroutine.
5. Atomically settle establishment as established, failed, canceled, or timed out.
6. Stop the establishment timer once established; keep transport cancellation
   active until stream termination or supervisor removal.
7. Before consuming a returned stream, recheck cancellation and current registry
   ownership. Stop any stream returned after removal or timer expiry.
8. Consume normally, then stop the stream and release the attempt context before
   retrying with existing bounded backoff.

Timer expiry and successful response arbitrate through explicit synchronized
attempt state; simply calling Timer.Stop is insufficient to rule out an already
running callback. If timeout won, even a successful late response is disposed.
The transport context must not be an uncancelable background context.

A context-aware production transport is required. A deliberately noncooperative
test watcher may return late to prove disposal, but the runtime cannot forcibly
terminate arbitrary third-party code that ignores context. Do not launch another
attempt while that supervisor's previous call is still running.

Requirements: R2.AC3–R2.AC5, R2.AC7–R2.AC9.

### Observation-gap recovery

Retain first-attempt readiness before initial LIST on the normal success path.
For a failed or timed-out establishment followed by a later successful WATCH,
enqueue all current owners immediately after successful establishment.
Their fresh LIST closes the interval between an earlier LIST and the new stream.
Do this through the existing coalescing trigger ingress, including for promoted
targets. Metadata events subsequently enqueue only current authorized owners.

Periodic safety reconciliation remains available while startup/retry is pending.
Watch capacity remains unchanged; targets beyond capacity retain authorized
bindings and periodic coverage. No foreign-owner transport is created by a
canceled owner alone.

Requirements: R2.AC6/R2.AC8/R2.AC10, R3.AC5.

### Documentation

Update docs/concepts-and-architecture.md with the caller/transport lifetime
distinction and evaluation timeout behavior. Update docs/operations.md with
EvaluationTimedOut versus ReadForbidden/policy-denial diagnosis, existing watch
restart telemetry, and periodic recovery. Update docs/security.md with current
owner authority and last-owner cancellation. Examples use identities and bounded
reason-only diagnostics; never print payloads, selectors, or capabilities.

Requirements: R4.AC1–R4.AC4.

## Data Models

No public resource schema changes. Private supervisor state includes a startup
notification and per-attempt establishment state. Ownership remains keyed by
exact target and subject identity. A removed supervisor is never restarted in
place; registration identity checks distinguish it from any successor for the
same address. Existing watch-count limits bound concurrent supervisors.

## Error Handling

Caller deadline uses the existing evaluation-timeout publication path; external
cancellation and staleness suppress publication. Establishment timeout is a
transport interruption for existing restart telemetry and backoff, not logical
authorization denial. Forbidden WATCH remains ReadForbidden. A late stream is
stopped without event delivery. Manager shutdown cancels caller waits and
supervisor transports; readiness waits do not consume the graceful shutdown
budget waiting for a remote server.

## Security Considerations

Revalidate capabilities immediately before every WATCH request. Cleanup preserves
other authorized owners, and incoming events resolve current owners again.
Cancellation of one attempt never mints authority or revives a stale binding.

## Failure Modes And Tradeoffs

Bounding the initial request can retry a slow but eventually healthy API server;
reuse the existing evaluation bound rather than creating another setting.
A recovery enqueue adds one coalesced reconcile per current owner per successful
establishment, buying coverage of reconnect gaps. Context cancellation releases
workers even if transport cleanup is delayed; serial supervisor attempts prevent
unbounded detached work.

## Baseline Contract Reconciliation

Update internal RouteManager adapters and test fixtures together with the new
signature. Preserve the existing watch-routing and capacity assertions.
The reconciliation-runtime Design's route startup/waiting sections and
performance-and-limits Design's evaluation-timeout and promotion sections need
to describe the two lifetimes. Their Requirements already require interruption,
sharing, and recovery; no broader scope is proposed.

Any approved baseline Design/proof that needs editing is reconciled and reviewed
through Walden before execution. This draft records the affected surfaces
without changing their approvals. The non-waiting promotion helper overlaps
configuration-budget-status-invalidation; implement or reuse it once.

## Testing Strategy

Add integration scenarios composing real Runtime, RouteRegistry,
FreshnessTracker, Enforcer, and StatusPublisher. The MetadataWatcher port
provides controlled started/release/canceled channels. Keep manager context alive
in deadline, lease-invalidation, and newer-generation tests. Completion must
occur within the approved one-second post-signal allowance.

Exercise two owners, last-owner removal, blocked promoted targets, concurrent
readiness/cancellation, late stream return, established streams surviving caller
completion, and all-worker saturation. Use race detection for these scenarios.
A local HTTP transport fixture verifies cancellation while response headers are
blocked; no external cluster or ambient kubeconfig is needed.

## Verification Plan

- R1: prove evaluation timeout publication and lease-cancellation suppression
  independently, with the manager alive and real publication assertions.
- R2/R3: prove one transport for two owners, last-owner context cancellation,
  late stream Stop, generation recovery, and no binding resurrection.
- R2.AC10/R3.AC5: change a source during establishment/reconnect and verify the
  recovery LIST reflects it, even when the stream emits no historical event.
- R2.AC9: cancel manager while startup is blocked; prove callers return within
  shutdown allowance and cooperative transport goroutines terminate.
- R4: review the three named guides against runtime reasons, profile defaults,
  lifetime behavior, and non-payload examples; validate relative links.
- Later Tasks bind named non-vacuous scenarios to make test-integration and the
  appropriate race invocation. Run existing envtest reconciliation coverage
  for the changed port wiring. No dedicated unit-test layer is introduced.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Runtime and RouteManager port; Registry launch and readiness wait |
| `R2` | Supervisor bound; ownership cleanup; Observation-gap recovery |
| `R3` | Testing Strategy; Verification Plan production composition scenarios |
| `R4` | Documentation; guide verification |
| `NFR1` | Caller deadlines, startup timer, and shutdown cancellation |
| `NFR2` | Capability checks and shared-owner/late-response handling |
| `NFR3` | Serial bounded supervisors; recovery enqueue; periodic scheduling |
| `NFR4` | Operational reason mapping and documentation |
