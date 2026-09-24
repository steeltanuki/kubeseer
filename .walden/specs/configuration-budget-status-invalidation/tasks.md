---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-10T06:22:22Z
last_modified: 2026-09-10T08:32:35Z
approved_fingerprint: sha256:ec1cde41a6c782901ef7550367bd688ab2fd17f4e8f50246f943e1c8a2861f3d
source_design_approved_at: 2026-09-10T06:13:03Z
source_design_fingerprint: sha256:c7922cf02203430e85a046f14a7061b1796df730f3e1bbcfe95be6a936d7e763
---

# Implementation Plan

## Execution Boundaries

Requirements and Design are approved; this plan requires its own review.
Execution starts only on an explicit task request. No code, test, or public
documentation change is authorized by drafting this plan.

Before execution, reconcile and review the affected performance-and-limits
Design (configuration budgets, error handling, verification) and task 2.1
description/proof, plus any exhaustive status-and-conditions Design/proof tables,
as identified in the approved Design's Baseline Contract Reconciliation.
Do not silently rewrite approved baseline documents or weaken their requirements.

<!-- assumed: deliver the terminal status contract before connecting runtime rejection, then prove persisted recovery (source: approved Design, Components And Interfaces and Verification Plan) -->
<!-- assumed: implement or reuse the non-waiting promotion helper once, shared with watch-startup-cancellation; neither full correction must precede the other (source: approved Design, Route removal) -->

## Proof Conventions

Tasks run in numerical order. Every named new scenario below is created and
registered under TestModuleIntegration or TestEnvtestReconciliationRuntime by
its owning task before its proof runs. Tests compose production modules; doubles
control only I/O boundaries. Direct integration commands select only the named
scenario; make test-api also exercises the existing API wiring required by Design.
Use pinned, isolated envtest assets and writable caches with sufficient space;
never use ambient kubeconfig or a real cluster. Proofs are read-only and fail
rather than skip missing infrastructure. No proof is run during Tasks drafting.
Documentation assertions support, but do not replace, the stated prose review.

## Tasks

- [x] 1. Implement bounded rejection and connect runtime cleanup
  - [x] 1.1 Add the compact terminal status through the existing guarded publisher
    - Extend internal/status.Evaluation, validation, composition, and publication with ConfigurationBudgetExceeded, mutually exclusive with results, other terminal modes, source assessments, and global authorization. Keep the public schema unchanged.
    - Produce the approved five-condition table with current observedGeneration and no result, summary, or resultHash. Preserve foreign conditions and stable transition times; compare semantics before writing.
    - Measure the complete merged candidate against the status ceiling. An oversized already-compact rejection returns StatusLimitInvalid without fallback to ResultLimitExceeded or discarding foreign conditions. Keep messages and diagnostic reasons payload-free; extend finite observability normalization only where needed.
    - Create BudgetRejectionStatus using the real composer and publisher with an instrumented status I/O port. Assert all five conditions, incompatible-input rejection, successful-result replacement, fixed-size output across rejected source counts, byte-limit boundaries, oversized foreign conditions, sentinel redaction, and a repeated semantic no-op.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R3.AC1`, `NFR1`, `NFR2`, `NFR3`
    - Design: Components And Interfaces / Status composition and bounded publication; Data Models; Error Handling; Testing Strategy
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=1","-run","^TestModuleIntegration$/^BudgetRejectionStatus$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/BudgetRejectionStatus"
        covers: ["R1.AC1","R1.AC2","R1.AC3","R1.AC4","R1.AC5","R1.AC6","R2.AC7","R2.AC8","R2.AC9","R3.AC1"]
        timeout: 10m
  - [x] 1.2 Publish runtime rejection without policy or source reads and release only the rejected owner's routes
    - Replace the early budget-error return in internal/reconciliation/pipeline.go with lease-guarded route cleanup and Publisher.Publish using the lease context. Keep the existing sanitized terminal error and retry behavior; do not invoke PolicySource, Planner, Enforcer, or Executor.
    - Separate supervisor promotion launch from readiness waiting in RouteRegistry, or reuse the identical helper if the WATCH correction has already supplied it. Launch outside locks; RemoveOwner and lifecycle cleanup must not wait on promoted WATCH responses. Preserve exact-owner identity, other owners, and watch capacity.
    - Create BudgetRejectionRuntime composing the real validator, Runtime, RouteRegistry, freshness tracking, and publisher. First publish a successful two-source result, then reconstruct runtime with maximum sources one and missing, invalid, restricted, or unavailable policy.
    - Assert zero policy/discovery/source calls during rejection, removal of old observation fields, exact-owner cleanup, surviving shared-owner routes, last-owner cancellation, and publication completion while a promoted target stalls. Synchronize transport start/release with channels; leave the manager active during the assertion.
    - Requirements: `R1.AC7`, `R1.AC8`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `NFR1`, `NFR2`
    - Design: Components And Interfaces / Runtime rejection branch; Route removal; Diagnostics and documentation; Security Considerations
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=10","-race","-run","^TestModuleIntegration$/^BudgetRejectionRuntime$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/BudgetRejectionRuntime"
        covers: ["R1.AC7","R1.AC8","R2.AC1","R2.AC2","R2.AC3"]
        timeout: 20m

- [x] 2. Prove persistence, guarded failures, and recovery
  - [x] 2.1 Exercise rejection races and recovery against the isolated Kubernetes status subresource
    - Add BudgetRejectionPersistence under TestEnvtestReconciliationRuntime with independently bounded fixtures/contexts, real runtime and guarded publisher. Begin each transition from an actually persisted successful result; cover profile tightening and policy removal/restriction.
    - Invalidate UID, generation, deletion state, and policy epoch immediately before publication and assert no stale write. Exercise real resource-version conflict, an infrastructure-injected transient failure, and forbidden status-subresource credentials; assert retry classification, sanitized StatusUnavailable, and no Event or report claiming deletion on failed writes.
    - Verify repeated rejection preserves transition times/resourceVersion. Recover by both compliant spec edit and compatible-profile runtime restart, freshly evaluate current policy, and prove denied recovery cannot restore the old result while authorized recovery publishes newly computed data.
    - After the separate baseline gates are approved, update assertPerformanceLimitsConfigurationScenarios and any affected exhaustive condition assertions to require no source I/O plus current rejection publication. Retain their existing capacity, authorization, and semantic-write checks.
    - Requirements: `R2.AC4`, `R2.AC5`, `R2.AC6`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R1.AC7`, `NFR1`, `NFR2`, `NFR3`
    - Design: Error Handling; Baseline Contract Reconciliation; Testing Strategy; Verification Plan
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","make","test-api","GO_TEST_FLAGS=-count=1"]
        expect_output: "--- PASS: TestEnvtestReconciliationRuntime/BudgetRejectionPersistence"
        covers: ["R2.AC4","R2.AC5","R2.AC6","R3.AC1","R3.AC2","R3.AC3","R3.AC4","R1.AC7"]
        timeout: 20m
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=1","-run","^TestModuleIntegration$/^(performance_limits_.*|status_.*)$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/performance_limits_reuse_one_budget_boundary_before_dynamic_work"
        timeout: 10m

- [x] 3. Deliver public documentation
  - [x] 3.1 Document budget-rejection status, diagnosis, and the successful-write security boundary
    - Update docs/api-reference.md with the exact five-condition table and absent observation fields; docs/operations.md with generation freshness checks, budget-versus-policy diagnosis, spec/profile recovery, and status-write failure checks; docs/security.md with delayed removal during API/RBAC failures and the successful guarded-write boundary.
    - Use status-only, synthetic examples. Do not print prior results, source payloads, selector operands, JSONPath expressions, or sensitive values; show presence checks instead of dumping the entire object.
    - Create BudgetRejectionDocumentation in the integration suite: read the three guides, check the documented condition/status example against real composer output, check reason names against production constants, assert safe field projections in diagnostic commands, and validate local file/fragment links in the changed sections without network or writes.
    - Manually review the three changed sections against successful, stale, forbidden, and transient publication fixtures. Record this review with the execution hand-off; a passing text/example check alone does not certify operational prose.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `NFR4`
    - Design: Components And Interfaces / Diagnostics and documentation; Security Considerations; Verification Plan
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=1","-run","^TestModuleIntegration$/^BudgetRejectionDocumentation$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/BudgetRejectionDocumentation"
        covers: ["R4.AC1","R4.AC2","R4.AC3","R4.AC4"]
        timeout: 10m
