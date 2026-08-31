---
walden_schema_version: v1alpha1
status: in-review
approved_at:
last_modified: 2026-08-31T21:08:55Z
approved_fingerprint:
source_design_approved_at:
source_design_fingerprint:
---

# Implementation Plan

- [ ] 1. Establish the immutable manager-wide limit profile
  - [ ] 1.1 Add defaults, pointer overrides, validation, adapters, and canonical accounting
    - Create `internal/limits` with pointer-based overrides, private immutable
      effective profile values, every approved positive default, overflow-safe
      deterministic canonical JSON sizing, and reusable stage accountants.
      Distinguish omitted settings from explicit non-positive values and reject
      structural admission over-caps as `InvalidLimitConfiguration`.
    - Move the admission and aggregation default values behind narrow profile
      adapters without creating dependency cycles. Preserve existing direct
      constructor defaults, make defensive value copies, and add no API field,
      generated CRD change, packaging flag, environment key, or mutable global.
    - Extend `TestModuleIntegration` with the complete default table, partial
      overrides, omitted/zero/negative/over-cap cases, immutable-copy behavior,
      deterministic canonical accounting, integer overflow, compatibility with
      existing admission/aggregation defaults, and public-API absence. Emit
      `MODULE_INTEGRATION=performance-and-limits-profile STATUS=passed` only
      after the full matrix succeeds.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `R1.AC10`, `R1.AC11`, `R1.AC12`, `R1.AC13`, `R1.AC14`, `R1.AC15`, `R1.AC16`, `R1.AC17`, `R1.AC18`, `R1.AC19`, `R1.AC20`, `NFR1`, `NFR2`, `NFR6`, `C2`, `C3`, `C5`, `C11`
    - Design: Components And Interfaces / Effective limit profile; Data Models / Configuration state, Attempt-local accounting; Simplicity And Elegance Review; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits-profile STATUS=passed"
        timeout: 40m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R1.AC9", "R1.AC10", "R1.AC11", "R1.AC12", "R1.AC13", "R1.AC14", "R1.AC15", "R1.AC16", "R1.AC17", "R1.AC18", "R1.AC19", "R1.AC20", "NFR1", "NFR2", "NFR6", "C2", "C3", "C5", "C11"]

- [ ] 2. Centralize admission and runtime configuration budgets
  - [ ] 2.1 Reuse one budget validator at webhook and fail-closed runtime boundaries
    - Extract the existing pure Kubeseer and KubeseerAccessPolicy budget checks
      into a reusable validator driven by the effective profile. Apply it before
      semantic/discovery/policy work in both admission paths while retaining
      stable field-oriented `ConfigurationBudgetExceeded` responses.
    - Reapply the same Kubeseer budget after loading a current generation and
      the policy budget before authorization compilation. Perform no observed
      resource I/O for an over-budget Kubeseer and preserve the approved
      fail-closed invalid-policy behavior for a persisted over-budget policy.
    - Extend `TestModuleIntegration` through the real admission validator,
      policy loader, and runtime with tightened budgets, equivalent first-field
      failures, no-I/O recorders, invalid policy, and diagnostics containing
      only field coordinate and ceiling. Emit
      `MODULE_INTEGRATION=performance-and-limits-configuration STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R2.AC11`, `NFR2`, `NFR3`, `NFR4`, `NFR6`, `C1`, `C3`, `C4`, `C9`, `C10`, `C11`
    - Design: Components And Interfaces / Admission and runtime configuration budgets; Error Handling; Security Considerations; Verification Plan / Profile and admission proof
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits-configuration STATUS=passed"
        timeout: 40m
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R2.AC11", "NFR2", "NFR3", "NFR4", "NFR6", "C1", "C3", "C4", "C9", "C10", "C11"]

- [ ] 3. Bound paginated resource selection
  - [ ] 3.1 Enforce unique-resource and canonical-input ceilings before retention
    - Extend the selection executor options with effective page, unique resource,
      and canonical selected-input ceilings. During successful pagination,
      validate identity, coalesce duplicate UIDs, canonical-size each first
      occurrence before append, and retain the approved deterministic final
      ordering and expired-resource-version restart behavior.
    - Stop further LIST pages when accepting the next unique object would cross
      either source ceiling. Discard all objects retained for that source and
      return the source-scoped `SelectionLimitExceeded` outcome without
      presenting a truncated success or preventing an independent later source.
    - Extend `TestModuleIntegration` through the real planner/executor with
      page-size assertions, duplicate UIDs, exact-boundary and boundary-plus-one
      count/byte cases, pagination stop recorders, deterministic replay, later
      source continuation, source cleanup, and no partial output. Emit
      `MODULE_INTEGRATION=performance-and-limits-selection STATUS=passed`.
    - Requirements: `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC15`, `R3.AC16`, `R3.AC17`, `NFR1`, `NFR2`, `NFR3`, `C4`, `C5`, `C6`, `C9`, `C10`, `C11`
    - Design: Components And Interfaces / Source-local evaluation pipeline; Data Models / Attempt-local accounting; Error Handling; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits-selection STATUS=passed"
        timeout: 45m
        covers: ["R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC15", "R3.AC16", "R3.AC17", "NFR1", "NFR2", "NFR3", "C4", "C5", "C6", "C9", "C10", "C11"]

- [ ] 4. Bound source-local value processing
  - [ ] 4.1 Refactor to source-at-a-time evaluation with atomic stage accountants
    - Preserve shared policy, authorization, planning, and route preflight, then
      evaluate each approved source through selection, extraction, conversion,
      operators, aggregation, and terminal result assembly before starting the
      next source. Release all source bodies and temporary values after assembly.
    - Give extraction, typed conversion, operator evaluation, and aggregation a
      fresh produced-value accountant. Check each exact retained output before
      commit; on overflow discard the source execution and return
      `ValueLimitExceeded`. Continue later independent sources and preserve
      approved aggregate group/contribution/collection/distinct/provenance
      limits and atomic `cardinality-exceeded` outcomes.
    - Extend `TestModuleIntegration` through the real selection-to-aggregation
      pipeline with exact and plus-one byte boundaries at every stage,
      transformation expansion, all aggregate ceilings, deterministic replay,
      source cleanup, later-source success, and absence of truncated values,
      groups, contributors, or provenance. Emit
      `MODULE_INTEGRATION=performance-and-limits-values STATUS=passed`.
    - Requirements: `R3.AC9`, `R3.AC10`, `R3.AC11`, `R3.AC12`, `R3.AC13`, `R3.AC14`, `R3.AC15`, `R3.AC16`, `R3.AC17`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `C1`, `C4`, `C5`, `C9`, `C10`, `C11`
    - Design: Components And Interfaces / Source-local evaluation pipeline; Data Models / Attempt-local accounting; Failure Modes And Tradeoffs; Verification Plan / Resource and value proof
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits-values STATUS=passed"
        timeout: 50m
        covers: ["R3.AC9", "R3.AC10", "R3.AC11", "R3.AC12", "R3.AC13", "R3.AC14", "R3.AC15", "R3.AC16", "R3.AC17", "NFR1", "NFR2", "NFR3", "NFR4", "C1", "C4", "C5", "C9", "C10", "C11"]

- [ ] 5. Enforce evaluation deadlines and bounded status publication
  - [ ] 5.1 Compose timeout outcomes and compact oversized status under freshness leases
    - Derive one evaluation timeout context immediately after the freshness lease
      and propagate it through runtime configuration checks, policy, discovery,
      authorization, planning, selection, extraction, conversion, operators,
      and aggregation. On deadline, cancel in-flight work, discard the active
      source, retain completed sources, and synthesize `EvaluationTimedOut` for
      active/unstarted sources.
    - Publish a terminal timeout result only through the still-current lease
      context, preserve parent-cancellation no-write behavior, and return the
      approved rate-limited retry. Preserve successful pre-deadline semantics.
    - Extend status composition/publication with canonical pre-write sizing,
      compact `ResultLimitExceeded` status, preserved Accepted/Authorized/
      SourcesResolved conditions and generation, no result/summary/hash,
      semantic suppression, later recovery, compact-ceiling setup validation,
      and defensive `StatusLimitInvalid` no-write behavior.
    - Extend `TestModuleIntegration` with deterministic stage cancellation,
      completed/active/unstarted source matrices, stale leases, retry timing,
      exact status boundaries, repeated compact status, and recovery. Extend the
      existing reconciliation envtest suite through the real status subresource
      and emit `MODULE_INTEGRATION=performance-and-limits-deadline-status STATUS=passed`
      and `API_CONTRACT=performance-and-limits-status STATUS=passed`.
    - Requirements: `R1.AC21`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R4.AC10`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R5.AC12`, `NFR1`, `NFR2`, `NFR3`, `C1`, `C4`, `C5`, `C9`, `C10`, `C11`
    - Design: Components And Interfaces / Evaluation deadline and freshness composition, Bounded status publisher; Error Handling; Failure Modes And Tradeoffs; Verification Plan / Deadline proof, Status proof
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits-deadline-status STATUS=passed"
        timeout: 55m
        covers: ["R1.AC21", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R4.AC10", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R5.AC12", "NFR1", "NFR2", "NFR3", "C1", "C4", "C5", "C9", "C10", "C11"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-api"]
        expect_output: "API_CONTRACT=performance-and-limits-status STATUS=passed"
        timeout: 55m
        covers: ["R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R5.AC12", "NFR1", "NFR2", "NFR3", "C1", "C9", "C10"]

- [ ] 6. Bound and share discovery metadata
  - [ ] 6.1 Add capacity-aware deterministic LRU behavior to the manager resolver
    - Extend the manager-owned resolver with effective capacity and TTL options,
      mutex-protected access sequencing, canonical-key LRU tie-breaking, and
      eviction of only non-refreshing entries. Preserve collapsed concurrent
      misses and remove explicitly invalidated entries before their next use.
    - Keep discovery cache values metadata-only and process-local. If insertion
      or retention is unavailable, use the fresh result for the current plan;
      never allow cache freshness to extend policy epochs, capabilities, routes,
      or authorization before resource-instance I/O.
    - Extend `TestModuleIntegration` with shared manager instances, concurrent
      equivalent misses, TTL expiry, hit promotion, capacity eviction,
      refreshing-entry exclusion, deterministic ties, invalidation, uncached
      fallback, policy-epoch changes, and forbidden payload/key sentinels. Emit
      `MODULE_INTEGRATION=performance-and-limits-discovery-cache STATUS=passed`.
    - Requirements: `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R7.AC12`, `R7.AC13`, `NFR1`, `NFR2`, `NFR4`, `NFR5`, `C1`, `C4`, `C8`, `C9`, `C10`, `C11`
    - Design: Components And Interfaces / Bounded discovery cache; Data Models / Shared bounded state; Security Considerations; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits-discovery-cache STATUS=passed"
        timeout: 45m
        covers: ["R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC9", "R7.AC10", "R7.AC11", "R7.AC12", "R7.AC13", "NFR1", "NFR2", "NFR4", "NFR5", "C1", "C4", "C8", "C9", "C10", "C11"]

- [ ] 7. Bound controller workers, trigger ingress, and shared watches
  - [ ] 7.1 Wire concurrency and deterministic non-blocking overload fallback
    - Pass the effective worker limit to
      `controller.Options.MaxConcurrentReconciles` while retaining
      controller-runtime queue, retry, and shutdown ownership.
    - Add an identity-only trigger ingress that coalesces duplicate Kubeseer
      keys, counts distinct pending identities, drains them in canonical order,
      resumes below capacity, and collapses excess into one non-blocking
      enqueue-all intent backed by periodic safety reconciliation.
    - Enforce the active exact-watch ceiling in `RouteRegistry`. Continue
      sharing authorized exact targets, release ownerless supervisors, omit
      excess watches with safety fallback, and deterministically promote an
      eligible bound target when capacity becomes available without weakening
      policy-epoch invalidation or freshness leases.
    - Extend `TestModuleIntegration` with worker counters, different/same-key
      concurrency, duplicate and distinct trigger bursts, overflow latency,
      single recovery intent, post-drain acceptance, shared/ownerless/excess
      watches, promotion, fallback correctness, policy changes, and prohibited
      identity/value retention. Extend envtest manager coverage and emit
      `MODULE_INTEGRATION=performance-and-limits-backpressure STATUS=passed` and
      `API_CONTRACT=performance-and-limits-manager STATUS=passed`.
    - Requirements: `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC11`, `R7.AC12`, `R7.AC13`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `C1`, `C4`, `C7`, `C8`, `C9`, `C10`, `C11`
    - Design: Components And Interfaces / Bounded controller workers and trigger ingress, Bounded shared watches; Data Models / Shared bounded state; Security Considerations; Verification Plan / Concurrency and backpressure proof, Sharing proof
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits-backpressure STATUS=passed"
        timeout: 55m
        covers: ["R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC11", "R7.AC12", "R7.AC13", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "C1", "C4", "C7", "C8", "C9", "C10", "C11"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-api"]
        expect_output: "API_CONTRACT=performance-and-limits-manager STATUS=passed"
        timeout: 60m
        covers: ["R6.AC1", "R6.AC2", "R6.AC7", "R6.AC9", "R6.AC10", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC11", "R7.AC12", "NFR1", "NFR3", "NFR4", "NFR5", "C1", "C7", "C8", "C9", "C10"]

- [ ] 8. Integrate sanitized limit diagnostics and production-boundary proofs
  - [ ] 8.1 Wire stable reasons, passive observations, complete integration, and envtest contracts
    - Extend approved status, selection, reconciliation, and observability
      vocabularies with only the authoritative limit reasons. Emit exactly one
      source- or reconciliation-scoped observation per exceeded runtime ceiling
      with fixed dimension and configured ceiling, and update an existing
      bounded-cardinality failure metric using approved stage/reason labels.
    - Exclude observed counts where sensitive and exclude every body, observed
      identity, selector, field path, extracted/typed/aggregate value, Secret,
      and wrapped cause. Keep observation best-effort and unable to alter limit,
      retry, status, authorization, or cache outcomes.
    - Complete `TestModuleIntegration` through the real admission-to-status
      pipeline with resource, byte, aggregation, timeout, status, cache, watch,
      trigger, concurrency, equivalent-replay, confidentiality, and default
      below-limit regression matrices. Assert stable outcomes, exact records,
      bounded metric labels, no cached payloads, and unchanged pre-limit
      semantics; emit `MODULE_INTEGRATION=performance-and-limits STATUS=passed`.
    - Complete the existing reconciliation envtest manager/status scenarios for
      effective concurrency, watch fallback, timeout and compact status
      publication/suppression/recovery, then emit
      `API_CONTRACT=performance-and-limits STATUS=passed`.
    - Requirements: `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`, `C10`, `C11`
    - Design: Architecture; Components And Interfaces / Limit diagnostics and observability; Error Handling; Security Considerations; Testing Strategy; Verification Plan; Requirement Coverage
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits STATUS=passed"
        timeout: 60m
        covers: ["R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC9", "R8.AC10", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10", "C11"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-api"]
        expect_output: "API_CONTRACT=performance-and-limits STATUS=passed"
        timeout: 65m
        covers: ["R8.AC5", "R8.AC8", "R8.AC10", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "C1", "C3", "C4", "C7", "C8", "C9", "C10"]

- [ ] 9. Complete repository-wide boundary and race verification
  - [ ] 9.1 Verify generated/API stability, race safety, compatibility, build, and module tidiness
    - Register performance-and-limits integration/envtest helpers in the
      existing test-layer policy and add a static boundary verifier. Reject
      public limit fields, raised structural ceilings, package-local test
      substitutions, mutable global profiles, payload/result caches, custom
      worker queues, unbounded limit labels, forbidden diagnostic fields,
      generated drift, and missing Apache headers.
    - Run repository verification, race-enabled module integration, API
      compatibility, complete build, and read-only module tidiness with
      task-scoped writable caches. Require the final production markers so an
      empty test selection cannot pass.
    - Requirements: `R1.AC14`, `R1.AC18`, `R1.AC20`, `R3.AC16`, `R3.AC17`, `R6.AC2`, `R6.AC6`, `R7.AC10`, `R7.AC13`, `R8.AC5`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`, `C10`, `C11`
    - Design: Simplicity And Elegance Review; Security Considerations; Testing Strategy; Verification Plan; Requirement Coverage
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make verify"]
        expect_output: "Performance and limits boundary verification passed"
        timeout: 40m
        covers: ["R1.AC14", "R1.AC18", "R1.AC20", "R3.AC17", "R7.AC10", "R7.AC13", "R8.AC5", "NFR1", "NFR3", "NFR4", "NFR6", "NFR7", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10", "C11"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-integration GO_TEST_FLAGS=-race"]
        expect_output: "MODULE_INTEGRATION=performance-and-limits STATUS=passed"
        timeout: 70m
        covers: ["R3.AC16", "R6.AC2", "R6.AC6", "R8.AC7", "R8.AC9", "R8.AC10", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR7", "C1", "C4", "C7", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 80m
        covers: ["R1.AC20", "R8.AC8", "R8.AC10", "NFR3", "NFR4", "NFR6", "NFR7", "C1", "C3", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod go build ./..."]
        timeout: 30m
        covers: ["NFR1", "NFR5", "NFR6", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C11"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-performance-limits-go-build GOMODCACHE=/tmp/kubeseer-performance-limits-go-mod go mod tidy -diff"]
        timeout: 30m
        covers: ["C1", "C6", "C10", "C11"]
