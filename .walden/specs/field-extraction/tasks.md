---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T14:29:37Z
last_modified: 2026-08-26T14:49:54Z
approved_fingerprint: sha256:9a1261d5238d4d22e1da1dc2ba2a277ea2e8a58364cd82f10a57cd25b2af337e
source_design_approved_at: 2026-08-26T14:21:55Z
source_design_fingerprint: sha256:f0b276c47aee1be97d37cc9a0f3740aa2e9b62a7f70446c84c28abe39c80b999
---

# Implementation Plan

- [x] 1. Extend the public source API with field declarations
  - [x] 1.1 Add typed fields, structural validation, and API-server contract coverage
    - Add `KubeseerField` and the optional `Fields` list to
      `api/v1alpha1.KubeseerSource`. Preserve omitted and explicit-empty lists,
      require field names and paths, apply the approved name and path bounds, and
      declare field names as the list-map key so duplicate names are rejected by
      the installed structural schema.
    - Regenerate deep-copy code and the Kubeseer CRD, then extend
      `TestAPIContract` to prove typed-client persistence, JSON round trips,
      deep-copy isolation, installed-schema inspection, and real API-server
      rejection of missing names, missing paths, invalid names, overlong paths,
      and duplicate field names. Emit
      `API_CONTRACT=field-extraction-types STATUS=passed` only after the complete
      declaration contract passes.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC9`, `NFR5`
    - Design: Components And Interfaces / Public API Field Declaration; Data Models; Testing Strategy; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod make test-api"]
        expect_output: "API_CONTRACT=field-extraction-types STATUS=passed"
        timeout: 20m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC9", "NFR5"]

- [x] 2. Compile the restricted field language into immutable plans
  - [x] 2.1 Implement exact-subset parsing, stable planning failures, and source-isolated compilation
    - Add `internal/extraction` contracts, stable compile-error reasons, immutable
      tokens and plans, and the dedicated parser for the approved brace, root,
      property, bracket-key, non-negative index, and array-wildcard grammar. Reject
      malformed expressions separately from syntactically valid but unsupported
      JSONPath constructs, and reject surrounding text or multiple expressions.
    - Compile every declared field before resource evaluation, sort fields by name,
      retain source and field identity in failures, and keep plans source-local.
      Implement the approved no-cache initial strategy so unavailable, missing, or
      stale cache state falls back to equivalent compilation semantics. Extend the
      real `TestModuleIntegration` path across API and extraction packages and emit
      `MODULE_INTEGRATION=field-extraction-planning STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R5.AC3`, `NFR2`, `NFR3`, `NFR6`
    - Design: Architecture; Components And Interfaces / Restricted Parser; Components And Interfaces / Compiler And Immutable Plan; Data Models; Error Handling; Failure Modes And Tradeoffs; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=field-extraction-planning STATUS=passed"
        timeout: 20m
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC7", "R3.AC8", "R3.AC9", "R5.AC3", "NFR2", "NFR3", "NFR6"]

- [x] 3. Evaluate field plans with native JSON cardinality
  - [x] 3.1 Implement branch evaluation, provenance, native cloning, and sanitized resource failures
    - Implement `MatchSet` evaluation over selected unstructured objects. Preserve
      scalar, object, array, and explicit-null values as single native matches;
      drop only missing-property or out-of-range branches; expand wildcards in
      source-array order; and evaluate nested wildcards depth-first and left-to-right.
      Deep-copy every returned object or array so consumers cannot mutate selected
      resources through extracted values.
    - Associate every field outcome with source, field, and resource provenance,
      preserve selected-resource order, and provide stable invalid-resource and
      type-mismatch failures without resource contents or extracted values. Extend
      `TestModuleIntegration` through real selection outcomes and extraction plans,
      covering equivalent-input determinism and successful empty selections, then
      emit `MODULE_INTEGRATION=field-extraction-native-values STATUS=passed`.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R4.AC10`, `R4.AC11`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `NFR1`, `NFR2`, `NFR4`, `NFR6`
    - Design: Architecture; Components And Interfaces / Native Evaluator; Data Models; Error Handling; Security Considerations; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=field-extraction-native-values STATUS=passed"
        timeout: 20m
        covers: ["R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R4.AC10", "R4.AC11", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "NFR1", "NFR2", "NFR4", "NFR6"]

- [x] 4. Compose source-atomic extraction over selection outcomes
  - [x] 4.1 Add ordered batch outcomes, failure isolation, and cancellation semantics
    - Add `SourceInput`, `SourceOutcome`, and sequential `ExtractBatch` behavior that
      carries the original selection outcome and provenance unchanged. Return one
      extraction outcome per source in input order; treat omitted or explicit-empty
      field lists and empty successful selections as successful empty extraction;
      and skip field evaluation for unsuccessful selections.
    - Compile all fields for a source before evaluating its resources, discard every
      source-local extracted value when one field fails, preserve completed sibling
      sources, and report interrupted outcomes for the current and unstarted sources
      after cancellation or deadline expiry. Exercise stable failure reasons and
      confidentiality through the production selection-to-extraction boundary in
      `TestModuleIntegration`, then emit
      `MODULE_INTEGRATION=field-extraction-batch STATUS=passed`.
    - Requirements: `R1.AC7`, `R1.AC8`, `R3.AC6`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `NFR2`, `NFR3`, `NFR4`, `NFR6`
    - Design: Architecture; Components And Interfaces / Batch Engine; Data Models; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=field-extraction-batch STATUS=passed"
        timeout: 20m
        covers: ["R1.AC7", "R1.AC8", "R3.AC6", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R6.AC11", "NFR2", "NFR3", "NFR4", "NFR6"]

- [x] 5. Seal generated artifacts and the higher-layer verification matrix
  - [x] 5.1 Verify generation, compatibility, race safety, build, and module tidiness
    - Keep regenerated deep-copy and CRD artifacts under drift detection and confirm
      the field list-map, name pattern, required members, and path bounds remain
      structural on every supported Kubernetes version. Preserve the repository
      policy of proving this feature through API envtest and real cross-module
      integration rather than package-local unit tests.
    - Run repository verification, the default module-integration suite, the API
      compatibility matrix, race-enabled higher-layer tests, compilation, and
      read-only module tidiness with task-scoped writable Go caches. Confirm the
      final implementation preserves native-value isolation, unsupported-expression
      rejection, sanitized diagnostics, deterministic outcomes, and cancellation
      behavior under the complete suite.
    - Requirements: `R1.AC1`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC9`, `R2.AC8`, `R4.AC11`, `R6.AC5`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`
    - Design: Testing Strategy; Verification Plan; Security Considerations; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 20m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod make test GO_TEST_FLAGS=-race"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        timeout: 25m
        covers: ["R2.AC8", "R4.AC11", "R6.AC5", "R6.AC8", "R6.AC9", "R6.AC10", "NFR1", "NFR2", "NFR3", "NFR4", "NFR6"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 35m
        covers: ["R1.AC1", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC9", "NFR5", "NFR6"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod go build ./..."]
        timeout: 10m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-field-extraction-go-build GOMODCACHE=/tmp/kubeseer-field-extraction-go-mod go mod tidy -diff"]
        timeout: 10m
