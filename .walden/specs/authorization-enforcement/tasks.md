---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-30T11:26:53Z
last_modified: 2026-08-30T12:25:56Z
approved_fingerprint: sha256:0ae8ee71c83dc2b16413be772d41050d0da07fe2b8794b2ca2b452c5d34d8039
source_design_approved_at: 2026-08-27T10:44:01Z
source_design_fingerprint: sha256:3e8e73f7fcbc3691b15f488e263fd3cae5b50b80a2886bf3cfba5bc3b77cdaef
---

# Implementation Plan

- [x] 1. Establish the capability and evidence boundary
  - [x] 1.1 Add policy identity, authorization subjects, deterministic records, and opaque capabilities
    - Enrich `accesspolicy.Snapshot` with the canonical policy name, UID, and
      generation without changing evaluator precedence. Preserve identity for
      invalid present policies and omit it for missing or unavailable policy
      state.
    - Add the Apache-licensed `internal/authorization` package with immutable
      subjects, fail-closed ordered batch evaluation, defensive decision
      outcomes, private capability construction, freshness verification, and
      an explicit non-failing recorder normalized to `NoopRecorder`.
    - Extend the genuine module-integration suite with exact namespaced and
      cluster-scoped decisions; terminal policy states; deterministic record
      ordering; policy identity; subject completeness; capability privacy; and
      evidence allowlisting. Emit
      `MODULE_INTEGRATION=authorization-enforcement-core STATUS=passed` only
      after those production collaborations succeed.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC7`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `NFR3`, `NFR4`, `C2`, `C5`, `C6`, `C7`, `C9`
    - Design: Architecture; Components And Interfaces / Policy Snapshot Identity, Authorization Enforcer, Authorization Evidence Recorder; Data Models / Authorization Subject, Policy Identity, Decision And Enforcement Record, Outcome Batch And Capability, Freshness Verifier; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=authorization-enforcement-core STATUS=passed"
        timeout: 25m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC7", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC6", "R6.AC7", "R6.AC8", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "NFR3", "NFR4", "C2", "C5", "C6", "C7", "C9"]

- [x] 2. Make resource LIST capability-only
  - [x] 2.1 Bind exact capabilities into selection plans and guard every LIST request
    - Replace raw decision binding with `BindCapabilities`, remove the
      compatibility bypass, and keep one private `AuthorizedRead` for every
      exact planned target. Reject missing, denied, mismatched, duplicate, and
      extra outcomes before any target in that source can execute.
    - Change `ResourceLister` and `DynamicResourceLister` to accept only
      authorized reads. Check context, capability-target equality, and subject
      freshness before the first page, every continuation, and the single
      `ResourceExpired` restart; emit linked sanitized evidence on Forbidden
      while preserving source atomicity and independent sibling execution.
    - Extend `TestModuleIntegration` with zero-I/O mismatch cases, selector and
      target preservation, page barriers, cancellation and stale-subject
      barriers, RBAC Forbidden, transient non-RBAC errors, and sibling
      isolation. Emit
      `MODULE_INTEGRATION=authorization-enforcement-list STATUS=passed`.
    - Requirements: `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC6`, `R4.AC7`, `R4.AC9`, `R4.AC10`, `R6.AC4`, `R6.AC5`, `R6.AC9`, `R7.AC6`, `R8.AC1`, `R8.AC7`, `R8.AC8`, `R8.AC10`, `R8.AC11`, `R8.AC12`, `NFR1`, `NFR3`, `NFR5`, `C3`, `C8`, `C9`
    - Design: Components And Interfaces / Selection Capability Binding, Authorized LIST Adapter; Data Models / Private I/O Permits; Processing Flows / Allowed Source, Denied Or Terminal Policy Source, Kubernetes RBAC Forbidden; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=authorization-enforcement-list STATUS=passed"
        timeout: 25m
        covers: ["R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC6", "R4.AC7", "R4.AC9", "R4.AC10", "R6.AC4", "R6.AC5", "R6.AC9", "R7.AC6", "R8.AC1", "R8.AC7", "R8.AC8", "R8.AC10", "R8.AC11", "R8.AC12", "NFR1", "NFR3", "NFR5", "C3", "C8", "C9"]

- [x] 3. Make metadata WATCH capability-only
  - [x] 3.1 Retain exact route bindings and mint fresh private WATCH permits per attempt
    - Construct authorized routes only from exact target capabilities and keep
      the full subject in each route binding. Refactor the route index to prune
      stale bindings before every start or restart and to stop a shared
      supervisor when its last current binding disappears.
    - Change `MetadataWatcher` to accept a private `WatchPermit`; allow a
      shared metadata transport only while at least one current capability
      authorizes the exact address. Emit one linked sanitized Forbidden record
      per current binding and retain the existing bounded recovery semantics.
    - Extend `TestModuleIntegration` with watch start/restart barriers, mixed
      current and stale owners, last-owner removal, policy-epoch races, event
      routing only to current owners, and WATCH Forbidden evidence. Emit
      `MODULE_INTEGRATION=authorization-enforcement-watch STATUS=passed`.
    - Requirements: `R3.AC10`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC8`, `R5.AC3`, `R5.AC5`, `R5.AC9`, `R7.AC6`, `NFR1`, `NFR2`, `C3`, `C4`, `C8`, `C9`
    - Design: Components And Interfaces / Authorized Route And WATCH Permit; Data Models / Freshness Verifier, Private I/O Permits; Processing Flows / Policy Change, Kubernetes RBAC Forbidden; Failure Modes And Tradeoffs; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=authorization-enforcement-watch STATUS=passed"
        timeout: 25m
        covers: ["R3.AC10", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC8", "R5.AC3", "R5.AC5", "R5.AC9", "R7.AC6", "NFR1", "NFR2", "C3", "C4", "C8", "C9"]

- [x] 4. Compose enforcement into reconciliation and existing status
  - [x] 4.1 Evaluate fresh ordered batches, revoke stale work, and preserve public outcomes
    - Convert each live lease to one authorization subject, skip policy work for
      zero sources, load one fresh snapshot per reconciliation, evaluate targets
      in declaration order, bind capabilities, and derive LIST and route access
      from the same decisions. Normalize a nil recorder explicitly during
      setup and reject missing enforcement dependencies fail closed.
    - Keep policy invalidation ordered as tracker/context invalidation, route
      removal, then enqueue-all. Preserve retry classification, currentness
      publication guards, zero-source success, successful siblings, and the
      existing `AuthorizationDenied`, `PolicyMissing`, `PolicyInvalid`,
      `AuthorizationUnavailable`, `AuthorizationNotEvaluated`, and
      `ReadForbidden` status mappings without adding public API.
    - Extend `TestModuleIntegration` through the real policy, discovery,
      authorization, selection, reconciliation, status, and capturing-recorder
      boundaries. Cover policy narrowing/broadening, stale-value removal,
      restart reconstruction, unavailable retry, deterministic not-evaluated
      paths, stale no-write, and confidential diagnostics. Emit
      `MODULE_INTEGRATION=authorization-enforcement-pipeline STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC6`, `R2.AC8`, `R4.AC9`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `R8.AC11`, `R8.AC12`, `R8.AC13`, `NFR2`, `NFR3`, `NFR5`, `NFR6`, `C1`, `C4`, `C5`, `C6`, `C8`, `C9`
    - Design: Architecture; Components And Interfaces / Reconciliation Composition And Revocation, Existing Status Outcome Adapter; Processing Flows / Denied Or Terminal Policy Source, Policy Change, Zero Sources; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=authorization-enforcement-pipeline STATUS=passed"
        timeout: 30m
        covers: ["R2.AC1", "R2.AC6", "R2.AC8", "R4.AC9", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "R8.AC10", "R8.AC11", "R8.AC12", "R8.AC13", "NFR2", "NFR3", "NFR5", "NFR6", "C1", "C4", "C5", "C6", "C8", "C9"]

- [x] 5. Prove the complete cross-module security contract
  - [x] 5.1 Add a non-vacuous end-to-end module-integration scenario matrix
    - Complete `TestModuleIntegration` with production collaborations covering
      every exact-target decision, capability construction and rejection path,
      LIST/WATCH freshness checkpoint, revocation event, RBAC separation,
      evidence field, status mapping, sibling-isolation case, zero-source path,
      and confidentiality sentinel in the approved requirements.
    - Assert no forbidden API, RBAC, impersonation, durable audit, or public
      schema surface is introduced. Emit exactly
      `MODULE_INTEGRATION=authorization-enforcement STATUS=passed` only after
      the entire scenario matrix succeeds.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R4.AC10`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `R8.AC11`, `R8.AC12`, `R8.AC13`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`
    - Design: Testing Strategy / Module Integration; Verification Plan; Requirement Coverage; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=authorization-enforcement STATUS=passed"
        timeout: 30m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R4.AC10", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "R8.AC10", "R8.AC11", "R8.AC12", "R8.AC13", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9"]

- [x] 6. Prove policy changes and I/O enforcement against a real API server
  - [x] 6.1 Extend envtest with revocation, pagination, watch, RBAC, and status barriers
    - Extend `TestEnvtestReconciliationRuntime` through the generated CRDs,
      real manager/API server, production dynamic client, policy handler,
      tracker, route registry, pipeline, and status publisher. Exercise allowed,
      narrowed, deleted, invalid, unavailable, re-created, and broadened policy
      states; LIST continuation/expiration races; WATCH restarts; and real or
      adapter-level Forbidden responses.
    - Use deterministic barriers and bounded polling to prove active
      cancellation, no post-revocation request, no stale status write, stale
      value removal, fresh authorization before broader access, unchanged
      public API, and sanitized diagnostics. Emit
      `API_CONTRACT=authorization-enforcement STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC8`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC8`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R6.AC2`, `R6.AC4`, `R6.AC5`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `R8.AC11`, `R8.AC12`, `R8.AC13`, `NFR1`, `NFR2`, `NFR3`, `NFR5`, `NFR6`, `NFR7`, `C1`, `C3`, `C4`, `C5`, `C6`, `C8`, `C9`
    - Design: Testing Strategy / Envtest; Processing Flows / Policy Change, Kubernetes RBAC Forbidden; Failure Modes And Tradeoffs; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make test-api"]
        expect_output: "API_CONTRACT=authorization-enforcement STATUS=passed"
        timeout: 35m
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC8", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC8", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R6.AC2", "R6.AC4", "R6.AC5", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC7", "R8.AC8", "R8.AC9", "R8.AC10", "R8.AC11", "R8.AC12", "R8.AC13", "NFR1", "NFR2", "NFR3", "NFR5", "NFR6", "NFR7", "C1", "C3", "C4", "C5", "C6", "C8", "C9"]

- [x] 7. Complete repository-wide verification
  - [x] 7.1 Verify boundaries, generated state, race safety, compatibility, build, and module tidiness
    - Register any new integration files in the approved test-layer policy and
      update only repository boundary checks required by the new internal
      package. Confirm that no package-local unit suite, API field, RBAC
      manifest, durable evidence store, or generated drift was introduced.
    - Run the race-enabled higher-layer suite, the API compatibility matrix on
      Kubernetes 1.35.6 and 1.36.2, a complete build, and read-only module
      tidiness after all task-scoped proofs pass.
    - Requirements: `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`
    - Design: Architecture / Existing Boundaries Retained; Simplicity And Elegance Review; Testing Strategy / Static And Race Verification; Verification Plan; Requirement Coverage
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 30m
        covers: ["NFR3", "NFR6", "C2", "C5", "C6", "C7", "C8", "C9"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make test GO_TEST_FLAGS=-race"]
        expect_output: "MODULE_INTEGRATION=authorization-enforcement STATUS=passed"
        timeout: 35m
        covers: ["NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR7", "C1", "C3", "C4", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 60m
        covers: ["NFR1", "NFR2", "NFR3", "NFR5", "NFR6", "NFR7", "C1", "C3", "C4", "C5", "C6", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod go build ./..."]
        timeout: 20m
        covers: ["C1", "C2", "C3", "C4", "C5", "C6", "C9"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-authorization-enforcement-go-build GOMODCACHE=/tmp/kubeseer-authorization-enforcement-go-mod go mod tidy -diff"]
        timeout: 20m
        covers: ["C1", "C9"]
