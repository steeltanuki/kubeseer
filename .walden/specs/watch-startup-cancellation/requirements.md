---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-06T08:34:51Z
last_modified: 2026-09-06T08:34:51Z
approved_fingerprint: sha256:b7fc29114013c073ed9305c55ae4ffcbac87f67ba01f194185bd016c8be9c01f
---

# Requirements Document

## Introduction

This corrective feature addresses review finding 2 (P1): first-WATCH startup
can retain a reconciliation worker after the evaluation deadline or freshness
lease has been canceled. Route replacement currently waits on the manager
lifecycle context, while watch readiness is signaled only after the WATCH call
returns. A newer generation can therefore remain queued behind obsolete work.

The observable outcome is bounded reconciliation waiting with safe shared
watch ownership and eventual recovery.

### Evidence And Baseline

- Reproduction: start a RouteRegistry with a watcher whose establishment waits
  for context cancellation, begin an authorized route replacement, then observe
  a newer owner generation. The lease is canceled but Replace stays blocked
  until the manager context is canceled.
- Code: `internal/reconciliation/routes.go`, `Replace`,
  `startSupervisorAndWait`, and `watchSupervisor.run`.
- Baseline contracts: `reconciliation-runtime` R2/R4/R7/R8,
  `performance-and-limits` R4/R6/R7, and `authorization-enforcement` R4/R5.

<!-- assumed: bound the caller's wait without making a shared watch's entire lifetime depend on one owner's evaluation context (source: authorization-enforcement R5.AC5 and the exact-target sharing contract in performance-and-limits) -->

## Requirements

### R1 Interruptible Watch Startup Waiting

**User Story:** As an operator, I want watch establishment to respect
reconciliation cancellation, so that a stalled transport cannot retain workers.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL bound any reconciliation wait for initial WATCH establishment by that attempt's effective evaluation deadline.
2. `R1.AC2` IF the reconciliation context is canceled during WATCH establishment, THEN the system SHALL release the reconciliation's startup wait without waiting for the WATCH response.
3. `R1.AC3` WHEN a UID, generation, deletion-state, or policy-epoch change cancels the waiting attempt's freshness lease, the system SHALL release that attempt's startup wait without waiting for manager shutdown.
4. `R1.AC4` IF the evaluation deadline expires while a current attempt waits for WATCH establishment, THEN the system SHALL enter the existing EvaluationTimedOut completion path.
5. `R1.AC5` IF lease invalidation or external cancellation interrupts WATCH startup, THEN the system SHALL suppress status publication by the interrupted attempt.
6. `R1.AC6` WHEN a newer generation is queued behind an interrupted startup wait, the system SHALL permit that generation to reconcile without requiring the obsolete WATCH response.
7. `R1.AC7` IF all available workers encounter stalled WATCH establishment, THEN the system SHALL release their startup waits by their respective evaluation deadlines.

### R2 Shared Watch Ownership And Recovery

**User Story:** As a user sharing observed resources with other Kubeseers, I want
one canceled attempt to leave other authorized owners operational.

#### Acceptance Criteria

1. `R2.AC1` WHILE multiple current authorized owners share an exact watch target, the system SHALL retain at most one registered shared WATCH transport for that target.
2. `R2.AC2` WHEN one owner's startup wait is canceled, the system SHALL preserve the route bindings of other current authorized owners.
3. `R2.AC3` IF an establishing or active WATCH loses its last current authorized owner, THEN the system SHALL cancel that WATCH transport's context.
4. `R2.AC4` WHEN a WATCH establishment attempt starts or restarts, the system SHALL verify a current exact-target authorization capability before issuing the request.
5. `R2.AC5` IF WATCH establishment fails for a target with a current authorized owner, THEN the system SHALL retain the existing bounded-backoff retry behavior.
6. `R2.AC6` WHILE an authorized target has no established WATCH, the system SHALL retain periodic safety reconciliation for its current owners.
7. `R2.AC7` IF a WATCH response arrives after its supervisor has been removed, THEN the system SHALL dispose of the returned stream without restoring the removed routing.
8. `R2.AC8` WHEN a shared WATCH establishes successfully after an owner's wait has ended, the system SHALL route subsequent events only to current authorized owners.
9. `R2.AC9` WHEN the manager shuts down, the system SHALL terminate pending watch-startup waits within the configured graceful shutdown interval.
10. `R2.AC10` The system SHALL preserve the existing gap-prevention behavior between initial observation and source-event monitoring.

### R3 Observable Regression Coverage

**User Story:** As a maintainer, I want deterministic cancellation scenarios to
exercise the production composition, so that isolated substitutes cannot hide
the same unbounded wait.

#### Acceptance Criteria

1. `R3.AC1` The system SHALL demonstrate deadline release with the real reconciliation runtime connected to the real route registry and a deliberately stalled WATCH transport.
2. `R3.AC2` The system SHALL demonstrate lease-cancellation release while the manager context remains active.
3. `R3.AC3` The system SHALL demonstrate that canceling one of two shared owners preserves event delivery to the remaining current owner.
4. `R3.AC4` The system SHALL demonstrate late-response disposal after last-owner removal.
5. `R3.AC5` The system SHALL demonstrate that a source change during watch establishment is eventually reflected by reconciliation.

### R4 Documentation And Operational Guidance

**User Story:** As a cluster operator, I want the operational guides to explain
watch startup cancellation and recovery, so that a stalled source watch can be
diagnosed without treating it as an authorization failure.

#### Acceptance Criteria

1. `R4.AC1` WHEN this feature is released, the system SHALL document bounded WATCH startup, evaluation-deadline cancellation, and freshness-lease cancellation in `docs/concepts-and-architecture.md`.
2. `R4.AC2` WHEN this feature is released, the system SHALL document watch restart reasons, periodic safety reconciliation, and the diagnostic order for stalled watches in `docs/operations.md`.
3. `R4.AC3` WHEN this feature is released, the system SHALL document that shared exact-target watches remain active only for current authorized owners in `docs/security.md`.
4. `R4.AC4` WHEN documentation describes a stalled or failed WATCH, the system SHALL avoid exposing resource payloads, field values, selectors, or authorization capabilities.

## Non-Functional Requirements

- `NFR1` Responsiveness: watch-startup waiting follows evaluation and shutdown
  bounds; verified by R1.AC1 through R1.AC7 and R2.AC9.
- `NFR2` Authorization isolation: caller cancellation does not transfer or
  resurrect authority; verified by R2.AC2 through R2.AC4 and R2.AC7/R2.AC8.
- `NFR3` Recovery and determinism: shared watches retain retry and source-event
  coverage; verified by R2.AC1, R2.AC5, R2.AC6, R2.AC10, and R3.
- `NFR4` Operability: operators can distinguish stalled startup, retrying watch,
  policy revocation, and RBAC denial using the documented reasons and checks;
  verified by R4.AC1 through R4.AC4.

## Constraints And Dependencies

- `C1` Depends on reconciliation-runtime, performance-and-limits,
  authorization-enforcement, and integration-testing-foundation.
- `C2` Evaluation timeout and graceful shutdown values remain the existing
  manager configuration. This feature introduces no mandatory new public flag.
- `C3` Metadata watches remain shared by exact authorized target; their steady
  lifetime is distinct from the lifetime of one reconciliation attempt.
- `C4` Tests use explicit transport-start and cancellation synchronization.
  A bounded completion assertion may allow at most one second after the
  cancellation signal under the local integration environment; it must not
  cancel the manager to make a lease-cancellation test pass.
- `C5` Cross-module coverage must compose Runtime, RouteRegistry, freshness
  tracking, authorization, and publication. Transport failures may be controlled
  at the infrastructure port; no dedicated unit-test layer is introduced.
- `C6` Design must explicitly account for blocked request headers, late stream
  creation, shared-owner cancellation, and the LIST/WATCH observation gap.
  A detached goroutine that merely hides an unbounded worker wait is insufficient.
- `C7` The documentation update is part of this feature's release scope and
  must be planned as a leaf task after Requirements and Design approval; this
  draft does not authorize documentation implementation.

## Out Of Scope

- Replacing the controller work queue or metadata watcher architecture.
- Changing authorization scope, watch capacity, retry intervals, or LIST semantics.
- Result invalidation for configuration-budget rejection.
- Quantity or duration conversion changes.
