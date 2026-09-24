---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-10T06:22:22Z
last_modified: 2026-09-10T11:56:49Z
approved_fingerprint: sha256:3cb0041c6ca8128eca7f2c4b6617260c2020be96f07d36cc71d21c3389b050d7
source_design_approved_at: 2026-09-10T06:13:03Z
source_design_fingerprint: sha256:61e65111fe98c27a896b9bd61c24a7dc29037e1a31f461142ea11b8c2967a482
---

# Implementation Plan

## Execution Boundaries

Requirements and Design are approved; Tasks remain a separate review gate.
Implementation requires an explicit task request after fresh approvals.

Before execution, reconcile and review affected reconciliation-runtime Design
startup/wait sections and performance-and-limits Design timeout/promotion
sections, including any changed task descriptions/proofs. Preserve their
existing interruption, capacity, and sharing requirements. See the approved
Design's Baseline Contract Reconciliation; do not edit approved baselines silently.

<!-- assumed: separate caller waiting first, then bound supervisor establishment and prove recovery; use existing profile settings (source: approved Design, Components And Interfaces) -->
<!-- assumed: share the non-waiting promotion helper with configuration-budget-status-invalidation, implementing it only if absent (source: approved Design, Baseline Contract Reconciliation) -->

## Proof Conventions

Execute tasks in numerical order. Each task creates and registers its new
TestModuleIntegration scenarios before running their exact-name proofs.
Use real Runtime, RouteRegistry, FreshnessTracker, Enforcer, and StatusPublisher;
test doubles control the MetadataWatcher or other infrastructure ports only.
Use started/canceled/release channels and at most one second after the relevant
cancellation signal to assert caller completion. Manager shutdown must not make
a lease-cancellation or deadline test pass. Race proofs repeat ten times.
Use writable caches and pinned isolated envtest assets; no ambient cluster.
Proofs are read-only, require the named PASS marker, and fail on missing
infrastructure. New proof commands are plans, not evidence from this phase.
Documentation assertions complement explicit manual content review.

## Tasks

- [x] 1. Separate caller cancellation from shared transport lifetime
  - [x] 1.1 Make route readiness waits cancellation-aware throughout the real runtime composition
    - Change RouteManager.Replace to accept context.Context and update runtime plus every implementation/adapter. Pass evaluationCtx; on return inspect evaluationState before generic error handling so a current deadline reaches finishTimedOutEvaluation and stale/external cancellation suppresses publication.
    - Register one shared startup notification before launching each supervisor, close it once, and let only Replace wait for its requested active targets. Recheck context on simultaneous readiness. Never hold registry/supervisor locks across waits or transport calls and never detach an untracked waiter goroutine.
    - Implement or reuse non-waiting promotion launch for RemoveOwner, startup, and stale pruning. Empty replacements do not wait on unrelated targets. Cancellation prunes only stale caller bindings; a still-current evaluation timeout does not revoke authority or affect another owner/generation.
    - Create WatchStartupCancellation with real production composition. Cover evaluation deadline, external cancellation, UID/generation/deletion/policy-epoch lease invalidation, newer-generation progress, and all-worker saturation while the manager stays alive. Distinguish timeout status from zero publication on invalidation.
    - In the same scenario, join two owners during pending startup and prove one registered transport, surviving current bindings and event delivery after one wait ends, and established-stream survival after ordinary caller completion.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R2.AC1`, `R2.AC2`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `NFR1`, `NFR2`, `NFR3`
    - Design: Components And Interfaces / Runtime and RouteManager port; Registry launch and readiness wait; Architecture; Testing Strategy
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=10","-race","-run","^TestModuleIntegration$/^WatchStartupCancellation$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/WatchStartupCancellation"
        covers: ["R1.AC1","R1.AC2","R1.AC3","R1.AC4","R1.AC5","R1.AC6","R1.AC7","R2.AC1","R2.AC2","R3.AC1","R3.AC2","R3.AC3"]
        timeout: 20m
  - [x] 1.2 Bound serial WATCH establishment and dispose canceled or late streams
    - Add the registry establishment-bound option using the existing default EvaluationTimeout for direct composition. Keep each transport context supervisor-owned; atomically settle response versus startup-timer expiry and stop the timer only after establishment wins. Normal caller completion must not cancel an established shared transport.
    - Before every serial attempt, obtain a current exact-target WatchPermit. Preserve existing bounded-backoff intervals and capacity; do not start another attempt while a noncooperative previous call is still blocked.
    - Cancel transport contexts on last-current-owner loss, supervisor removal, and manager shutdown. Recheck registration identity and cancellation before consuming a response; Stop late streams without delivering events or resurrecting routes. Retain sanitized ReadForbidden versus timeout/restart classifications.
    - Create WatchSupervisorEstablishment with cooperative stalled and controlled late-response watchers plus a loopback HTTP fixture that blocks response headers through the production transport. Assert request cancellation, timer/response races, one transport per target, serial bounded retry, reauthorization, replacement-supervisor identity, late Stop, current-owner event routing, and shutdown within the configured interval. Explicitly release noncooperative fixtures during teardown.
    - Requirements: `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R3.AC4`, `NFR1`, `NFR2`, `NFR3`
    - Design: Components And Interfaces / Supervisor establishment bound and late responses; Data Models; Error Handling; Security Considerations
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=10","-race","-run","^TestModuleIntegration$/^WatchSupervisorEstablishment$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/WatchSupervisorEstablishment"
        covers: ["R2.AC3","R2.AC4","R2.AC5","R2.AC7","R2.AC8","R2.AC9","R3.AC4"]
        timeout: 20m

- [x] 2. Close observation gaps and verify manager wiring
  - [x] 2.1 Restore current-owner observation after startup failure, reconnect, or capacity promotion
    - Wire the resolved profile EvaluationTimeout into registry construction in SetupWithManager without adding flags or Helm settings. Preserve graceful shutdown and existing watch-capacity/periodic scheduling.
    - Retain WATCH readiness before normal initial LIST. On successful initial establishment, retry/reconnect, or promoted startup, enqueue current authorized owners through the existing coalescing ingress so a fresh LIST closes any unobserved interval.
    - Create WatchStartupRecovery. Change a source during stalled/failed startup and reconnect, emit no historical watch event, and assert that recovery LIST publishes the changed value. Include promoted targets and capacity-deferred targets; prove periodic reconciliation while no stream exists and no enqueue for stale/revoked owners.
    - Extend the existing envtest reconciliation scenarios for the context-aware RouteManager and real manager/profile wiring; create WatchStartupProfileWiring under TestEnvtestReconciliationRuntime. Prove configured startup cancellation and cooperative transport cleanup at shutdown, retaining the existing routing/API regressions and bounded fixture lifecycle.
    - Requirements: `R2.AC6`, `R2.AC10`, `R3.AC5`, `R1.AC1`, `R2.AC9`, `NFR1`, `NFR3`
    - Design: Components And Interfaces / Observation-gap recovery; Supervisor establishment bound and late responses; Baseline Contract Reconciliation; Verification Plan
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=10","-race","-run","^TestModuleIntegration$/^WatchStartupRecovery$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/WatchStartupRecovery"
        covers: ["R2.AC6","R2.AC10","R3.AC5"]
        timeout: 20m
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","make","test-api","GO_TEST_FLAGS=-count=1"]
        expect_output: "--- PASS: TestEnvtestReconciliationRuntime/WatchStartupProfileWiring"
        covers: ["R1.AC1","R2.AC9"]
        timeout: 20m

- [x] 3. Deliver public documentation
  - [x] 3.1 Explain bounded startup, shared authority, and safe stalled-WATCH diagnosis
    - Update docs/concepts-and-architecture.md with evaluation/freshness cancellation versus supervisor-owned stream lifetime. Update docs/operations.md with EvaluationTimedOut, WATCH restart telemetry, periodic safety reconciliation, and a diagnostic order distinguishing startup/retry, policy revocation, and ReadForbidden/RBAC denial.
    - Update docs/security.md with exact-target sharing, current-owner authority, per-attempt authorization, and last-owner cancellation. Describe serial retry and late-response disposal without promising forced termination of arbitrary context-ignoring transports.
    - Use identity/reason-only examples, excluding payloads, field values, selectors, and authorization capabilities. Create WatchStartupDocumentation in the integration suite to check guide reason/configuration references against production definitions and safe command projections, and validate local file/fragment links in the changed sections.
    - Manually compare all three guides with the cancellation, shared-owner, late-response, and recovery fixture outcomes; record content/link/security review during task hand-off. Automated references do not replace that review.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `NFR4`
    - Design: Components And Interfaces / Documentation; Error Handling; Security Considerations; Verification Plan
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=1","-run","^TestModuleIntegration$/^WatchStartupDocumentation$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/WatchStartupDocumentation"
        covers: ["R4.AC1","R4.AC2","R4.AC3","R4.AC4"]
        timeout: 10m
