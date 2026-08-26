---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T10:13:23Z
last_modified: 2026-08-26T10:45:16Z
approved_fingerprint: sha256:da30de1809bf3061e2c608e13e75d231d32c17ef8c7d8d4e17a6a873d20e8a7a
source_design_approved_at: 2026-08-26T09:25:54Z
source_design_fingerprint: sha256:3057c75d4ae90cb3b446ca465c806c5cb611f8979e53c2537edb280538ceea6b
---

# Implementation Plan

- [x] 1. Extend the public source API and its generated contract
  - [x] 1.1 Add typed resource, namespace, and selector fields with envtest admission coverage
    - Extend `api/v1alpha1.KubeseerSource` with the approved resource
      coordinates, pointer-preserved namespace selection, and Kubernetes-native
      selector representation. Add structural markers for required coordinates,
      unique source IDs, namespace DNS-label validation, and namespace set
      semantics without collapsing omitted and explicit-empty values.
    - Regenerate deep-copy code and the Kubeseer CRD, then extend
      `TestAPIContract` to exercise typed client persistence, JSON round trips,
      deep-copy isolation, installed-schema inspection, and real API-server
      rejection of duplicate IDs, missing coordinates, invalid namespaces, and
      duplicate namespace entries. Emit
      `API_CONTRACT=resource-selection-types STATUS=passed` only after the full
      source-shape contract passes.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC5`, `R1.AC6`, `R2.AC7`, `R2.AC8`, `NFR5`
    - Design: Components And Interfaces / Public API extension; Data Models / Public API types; Testing Strategy / API contract through envtest; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-api"]
        expect_output: "API_CONTRACT=resource-selection-types STATUS=passed"
        timeout: 20m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC5", "R1.AC6", "R2.AC7", "R2.AC8", "NFR5"]

- [x] 2. Build deterministic plans and bind exact authorization decisions
  - [x] 2.1 Implement source planning through the real discovery contract
    - Add `internal/selection` contracts, stable planner errors, immutable read
      targets, and a planner that composes with the production
      `discovery.Resolver`. Preserve source identity, accept discovery as the only
      authority for GVR and scope, derive omitted, explicit-empty, single, multiple,
      and cluster-scoped namespace targets, and sort targets deterministically.
    - Normalize label selectors with `metav1.LabelSelectorAsSelector`, normalize
      field selectors with `fields.ParseSelector`, combine exact names as the
      `metadata.name` field term, and fail invalid source, namespace-scope, or
      selector inputs before resource-instance I/O. Extend the real
      `TestModuleIntegration` path with controlled discovery infrastructure and emit
      `MODULE_INTEGRATION=resource-selection-planning STATUS=passed`.
    - Requirements: `R1.AC4`, `R1.AC7`, `R1.AC8`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R3.AC6`, `R3.AC8`, `R4.AC1`, `R4.AC2`, `NFR2`, `NFR5`, `NFR6`
    - Design: Architecture; Components And Interfaces / Selection planner; Data Models / Internal selection values; Selector Normalization And Read Flow; Error Handling; Testing Strategy / Cross-module integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=resource-selection-planning STATUS=passed"
        timeout: 20m
        covers: ["R1.AC4", "R1.AC7", "R1.AC8", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R3.AC6", "R3.AC8", "R4.AC1", "R4.AC2", "NFR2", "NFR5", "NFR6"]

  - [x] 2.2 Add the complete authorization binder and opaque executable capability
    - Implement authorization pairs keyed by the exact source ID, API group, Kind,
      discovered scope, and namespace. Require exactly one matching
      `accesspolicy.Decision` with `Allowed=true` and `ReasonAllowed` for every
      target; reject denied, missing, extra, duplicate, or mismatched decisions
      before producing a plan the executor can accept.
    - Keep `AuthorizedPlan` internals inaccessible outside `internal/selection`,
      retain no policy loader or snapshot, and extend `TestModuleIntegration` with
      the real policy compiler/evaluator plus binder scenarios. Assert stable,
      sanitized missing, denied, and mismatch reasons and emit
      `MODULE_INTEGRATION=resource-selection-authorization STATUS=passed`.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC4`, `NFR1`, `NFR4`, `NFR6`
    - Design: Architecture; Components And Interfaces / Authorization binder; Data Models / Internal selection values; Error Handling; Security Considerations; Testing Strategy / Cross-module integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=resource-selection-authorization STATUS=passed"
        timeout: 20m
        covers: ["R4.AC1", "R4.AC2", "R4.AC4", "NFR1", "NFR4", "NFR6"]

- [x] 3. Execute authorized selections with Kubernetes semantics
  - [x] 3.1 Implement the list-only executor, dynamic adapter, and real selector scenarios
    - Add the narrow `ResourceLister` port, its `dynamic.Interface` adapter, and an
      executor whose only input is `AuthorizedPlan`. Address exactly the bound GVR
      and namespace, use LIST for every selector shape, deep-copy returned objects,
      construct complete provenance, and treat a completed zero-item response as a
      successful empty selection.
    - Add `TestEnvtestSelection` using the shared disposable control plane and real
      dynamic client for built-in, namespaced, cluster-scoped, and Secret-free
      CRD-backed fixtures. Prove exact name, matchLabels, matchExpressions,
      supported/unsupported field selectors, combined AND semantics, match-all,
      no-match, and cross-namespace reads. Extend `make test-api`, emit
      `API_CONTRACT=resource-selection STATUS=passed`, and add a counting-lister
      module scenario proving denied or mismatched plans cannot issue LIST calls;
      emit `MODULE_INTEGRATION=resource-selection-execution-boundary STATUS=passed`.
    - Requirements: `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC9`, `R4.AC3`, `R4.AC5`, `R4.AC6`, `R5.AC1`, `R5.AC5`, `NFR1`, `NFR5`, `NFR6`
    - Design: Components And Interfaces / Resource lister adapter; Components And Interfaces / Selection executor; Selector Normalization And Read Flow; Security Considerations; Testing Strategy / Kubernetes API selection through envtest
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=resource-selection-execution-boundary STATUS=passed"
        timeout: 20m
        covers: ["R4.AC3", "R4.AC5", "R4.AC6", "NFR1", "NFR6"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-api"]
        expect_output: "API_CONTRACT=resource-selection STATUS=passed"
        timeout: 20m
        covers: ["R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC9", "R5.AC1", "R5.AC5", "NFR5", "NFR6"]

  - [x] 3.2 Add bounded pagination, restart, deduplication, and deterministic ordering
    - Implement the constructor-configured positive page limit and continued LIST
      loop that preserves selectors and target identity while changing only the
      opaque continuation token. Accumulate through empty intermediate pages,
      discard target-local pages and restart once on `ResourceExpired`, and fail on
      a second expiration without merging inconsistent snapshots.
    - Extend `TestModuleIntegration` with a scripted `ResourceLister` to prove
      complete accumulation across different page boundaries, unchanged continued
      queries, context propagation, UID deduplication, deep-copy isolation, and
      lexicographic namespace/name/UID ordering. Extend `TestEnvtestSelection` with
      a small page limit to exercise real API-server continuation and emit
      `MODULE_INTEGRATION=resource-selection-pagination STATUS=passed` only after the
      scripted scenarios pass.
    - Requirements: `R3.AC5`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `NFR2`, `NFR3`, `NFR5`
    - Design: Components And Interfaces / Selection executor; Selector Normalization And Read Flow; Error Handling; Failure Modes And Tradeoffs; Testing Strategy / Cross-module integration; Testing Strategy / Kubernetes API selection through envtest
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=resource-selection-pagination STATUS=passed"
        timeout: 20m
        covers: ["R3.AC5", "R5.AC2", "R5.AC3", "R5.AC4", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "NFR2", "NFR3", "NFR5"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-api"]
        expect_output: "API_CONTRACT=resource-selection-pagination STATUS=passed"
        timeout: 20m
        covers: ["R7.AC1", "R7.AC2", "R7.AC3", "NFR5"]

  - [x] 3.3 Add source-atomic failures and independent batch outcomes
    - Implement stable `SelectionError` reasons and sanitized messages for forbidden
      reads, unsupported selectors, interrupted/unavailable reads, exhausted list
      restarts, and invalid object identity. Keep raw causes unwrap-only and discard
      every source-local object when any target or page fails.
    - Add sequential `SelectBatch` behavior that preserves input order and completed
      sibling outcomes, while producing interrupted outcomes for unstarted sources
      after context cancellation. Exercise equivalent failure stability, RBAC versus
      installation-policy distinction, atomic discard, sibling isolation, and
      confidentiality through the real planner/binder/executor path with only the
      outbound lister scripted. Emit
      `MODULE_INTEGRATION=resource-selection-failures STATUS=passed`.
    - Requirements: `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R7.AC4`, `R7.AC5`, `NFR2`, `NFR3`, `NFR4`
    - Design: Components And Interfaces / Selection executor; Components And Interfaces / Batch selector; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy / Cross-module integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=resource-selection-failures STATUS=passed"
        timeout: 20m
        covers: ["R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R7.AC4", "R7.AC5", "NFR2", "NFR3", "NFR4"]

- [x] 4. Seal generated artifacts and the higher-layer verification matrix
  - [x] 4.1 Verify generation, compatibility, race safety, build, and module tidiness
    - Regenerate deep-copy and CRD artifacts, update canonical Make targets for the
      named selection envtest suite, and keep all generated files under drift
      detection. Confirm the structural CRD remains valid on Kubernetes v1.35.6 and
      v1.36.2 and that omitted versus explicit-empty namespace selection survives
      both versions.
    - Run repository verification, the default module-integration suite, the full
      API compatibility matrix, race-enabled higher-layer tests, compilation, and
      read-only module tidiness with task-scoped writable Go caches. Confirm no
      package-local unit-test layer, ambient kubeconfig, or cloud credential fallback
      is introduced.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC5`, `R1.AC6`, `R2.AC4`, `R2.AC7`, `R2.AC8`, `R4.AC3`, `NFR1`, `NFR3`, `NFR5`, `NFR6`
    - Design: Testing Strategy / API contract through envtest; Testing Strategy / Static and generated checks; Verification Plan; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 20m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test GO_TEST_FLAGS=-race"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        timeout: 25m
        covers: ["R4.AC3", "NFR1", "NFR3", "NFR6"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 35m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC5", "R1.AC6", "R2.AC4", "R2.AC7", "R2.AC8", "NFR5", "NFR6"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod go build ./..."]
        timeout: 10m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-resource-selection-go-build GOMODCACHE=/tmp/kubeseer-resource-selection-go-mod go mod tidy -diff"]
        timeout: 10m
