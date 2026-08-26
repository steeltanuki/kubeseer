---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T15:55:57Z
last_modified: 2026-08-26T16:40:53Z
approved_fingerprint: sha256:da78b5fb25b1924f355ac098c0b783be4c714d548f7a351bc0a550bb83bb67df
source_design_approved_at: 2026-08-26T15:34:51Z
source_design_fingerprint: sha256:bb6b2efe295a6c76e8537d49363a7a8924a6f978e333b305f730d5cdf83469c0
---

# Implementation Plan

- [x] 1. Add the structural public typed-output API
  - [x] 1.1 Add field types, result unions, generated artifacts, and API-server coverage
    - Add the optional `KubeseerValueType` declaration to `KubeseerField` with
      the approved nine-value enum and no default. Replace the reserved empty
      `KubeseerResult` with the structural source, resource, field, match,
      duration, quantity, and sanitized-error types from the approved design.
      Use explicit state enums, pointer scalar payloads, and atomic result lists;
      keep `status.result` optional and avoid every preserve-unknown shortcut.
    - Regenerate deep-copy code and the Kubeseer CRD. Extend `TestAPIContract` to
      prove omitted-type compatibility, unsupported-type rejection, JSON/YAML
      round trips, deep-copy isolation, exact generated enum/list/schema markers,
      the absence of opaque schema regions, and status-subresource persistence
      for empty, value, null, and error result branches. Emit
      `API_CONTRACT=typed-output-model-types STATUS=passed` only after the real
      API-server contract passes.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC10`, `R5.AC7`, `R5.AC8`, `R5.AC13`, `R5.AC14`, `R5.AC15`, `NFR7`, `NFR8`
    - Design: Components And Interfaces / Public Field Type Contract; Components And Interfaces / Structural Status Adapter; Data Models; Testing Strategy; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod make test-api"]
        expect_output: "API_CONTRACT=typed-output-model-types STATUS=passed"
        timeout: 20m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC10", "R5.AC7", "R5.AC8", "R5.AC13", "R5.AC14", "R5.AC15", "NFR7", "NFR8"]

- [x] 2. Plan types and implement exact scalar conversions
  - [x] 2.1 Add immutable plans, stable errors, and the approved scalar conversion matrix
    - Create `internal/typedoutput` contracts with stable conversion reasons,
      sanitized errors, immutable source and field plans, field-local planning
      failures, and deterministic `CompileSource`/`CompileBatch` behavior. Sort
      valid field plans lexicographically, retain source and field identity, and
      implement the approved no-cache fallback.
    - Implement explicit converters for string, integer, number, boolean,
      timestamp, duration, and quantity. Enforce canonical integer and JSON
      number syntax, exact `int64` range checks, finite-number checks, UTC RFC
      3339 normalization, exact duration nanoseconds, and Kubernetes quantity
      canonicalization with a lexical-versus-`AsDec` guard against parser rounding
      or capping. Reject mismatches, invalid lexical forms, overflow, truncation,
      rounding, locale-sensitive parsing, and implicit scalar stringification.
    - Extend the real selection-to-extraction-to-typed-output path in
      `TestModuleIntegration`; cover supported native/string inputs, every
      forbidden conversion, missing and programmatic unsupported types,
      equivalent-plan determinism, stable reasons, and value-free diagnostics.
      Emit `MODULE_INTEGRATION=typed-output-model-conversions STATUS=passed`.
    - Requirements: `R1.AC8`, `R1.AC9`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R3.AC13`, `R3.AC14`, `R3.AC15`, `R3.AC16`, `R3.AC17`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC12`, `NFR1`, `NFR3`, `NFR4`, `NFR6`
    - Design: Architecture; Components And Interfaces / Conversion Planner; Components And Interfaces / Scalar And Semantic Converters; Data Models; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=typed-output-model-conversions STATUS=passed"
        timeout: 20m
        covers: ["R1.AC8", "R1.AC9", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R3.AC13", "R3.AC14", "R3.AC15", "R3.AC16", "R3.AC17", "R5.AC9", "R5.AC10", "R5.AC11", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC12", "NFR1", "NFR3", "NFR4", "NFR6"]

- [x] 3. Preserve native composite values and extraction cardinality
  - [x] 3.1 Add immutable typed outcomes, object/list conversion, and cardinality semantics
    - Implement the private tagged `Value`, typed match, field, resource, and
      source outcome models with defensive accessors. Preserve native object and
      list values as deep copies; model absence separately from present null;
      retain empty string, object, and list values; and keep one match position
      for every extraction match.
    - Join immutable plans to real extraction resource outcomes. Preserve
      provenance, resource order, lexicographic field order, direct-list-as-one
      match semantics, wildcard match cardinality, null type identity, and
      sibling fields after a field-local conversion failure. Discard every
      temporary match for the affected field when any match fails.
    - Extend `TestModuleIntegration` with scalar/null/empty/composite fixtures,
      direct arrays, wildcard sequences, multi-match failure atomicity,
      provenance, ordering, and caller-mutation attempts. Emit
      `MODULE_INTEGRATION=typed-output-model-cardinality STATUS=passed`.
    - Requirements: `R3.AC11`, `R3.AC12`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R4.AC10`, `R4.AC11`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `NFR2`, `NFR4`, `NFR5`
    - Design: Architecture; Components And Interfaces / Scalar And Semantic Converters; Components And Interfaces / Internal Typed Outcome Model; Data Models; Error Handling; Failure Modes And Tradeoffs; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=typed-output-model-cardinality STATUS=passed"
        timeout: 20m
        covers: ["R3.AC11", "R3.AC12", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R4.AC10", "R4.AC11", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R6.AC4", "R6.AC5", "R6.AC6", "NFR2", "NFR4", "NFR5"]

- [x] 4. Map internal outcomes into the structural status result
  - [x] 4.1 Implement canonical status serialization and union validation
    - Add the pure `BuildResult` adapter from internal typed outcomes to
      `v1alpha1.KubeseerResult`. Enforce source, field, and match state unions;
      emit exactly one payload for non-null matches; preserve zero and false via
      pointers; and retain source, provenance, field, type, cardinality, and
      ordering semantics.
    - Canonicalize object and list payloads to deterministic JSON text with
      sorted object keys and preserved list order. Validate the top-level JSON
      kind, emit exact decimal numbers, normalized UTC timestamps, canonical
      duration plus nanoseconds, canonical quantity plus exact base units, and
      sanitized structural failures without paths or values.
    - Exercise the adapter through real extraction and conversion outcomes in
      `TestModuleIntegration`. Prove JSON/YAML round trips, absent/null/value/error
      branches, invalid internal union rejection, deterministic bytes and
      semantics, canonical composite payloads, and no original-value duplicate.
      Emit `MODULE_INTEGRATION=typed-output-model-serialization STATUS=passed`.
    - Requirements: `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R5.AC12`, `R5.AC15`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `NFR2`, `NFR3`, `NFR4`, `NFR6`, `NFR7`, `NFR8`
    - Design: Architecture; Components And Interfaces / Structural Status Adapter; Data Models; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=typed-output-model-serialization STATUS=passed"
        timeout: 20m
        covers: ["R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R5.AC12", "R5.AC15", "R6.AC1", "R6.AC2", "R6.AC3", "NFR2", "NFR3", "NFR4", "NFR6", "NFR7", "NFR8"]

- [x] 5. Compose ordered conversion over extraction outcomes
  - [x] 5.1 Add field-isolated batch outcomes, upstream pass-through, and cancellation
    - Add `SourceInput`, `SourceOutcome`, and sequential `ConvertBatch` behavior
      joining each public source with the matching extraction outcome. Compile
      the complete source batch before conversion, record planning failures once
      per field, validate source and field identities, preserve unsuccessful
      extraction errors unchanged, and produce successful empty source/resource
      outcomes where required.
    - Preserve completed fields after sibling conversion failures, resources
      after sibling failures, and sources after independent failures. Check
      cancellation before each source, resource, field, match, and composite
      operation; discard the in-progress source and return stable interrupted
      outcomes for it and every unstarted source.
    - Extend the real module integration suite with mixed valid/invalid fields,
      multiple resources and sources, extraction-error pass-through, identity
      mismatch, empty outcomes, deterministic repeated inputs, confidentiality
      sentinels, and cancellation partitions. Emit
      `MODULE_INTEGRATION=typed-output-model-isolation STATUS=passed`.
    - Requirements: `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `NFR3`, `NFR5`, `NFR6`, `NFR8`
    - Design: Architecture; Components And Interfaces / Batch Converter; Components And Interfaces / Internal Typed Outcome Model; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=typed-output-model-isolation STATUS=passed"
        timeout: 20m
        covers: ["R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R6.AC11", "R6.AC12", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "NFR3", "NFR5", "NFR6", "NFR8"]

- [x] 6. Seal generated artifacts and the higher-layer verification matrix
  - [x] 6.1 Verify generation, compatibility, race safety, build, and module tidiness
    - Keep regenerated deep-copy and CRD artifacts under drift detection. Inspect
      the generated schema for the type enum, explicit typed-result branches,
      atomic ordered lists, pointer-compatible scalar fields, status subresource,
      and absence of `x-kubernetes-preserve-unknown-fields`.
    - Run repository verification, race-enabled higher-layer tests, the API
      compatibility matrix, compilation, and read-only module tidiness with
      task-scoped writable Go caches. Confirm the final suite exercises API
      compatibility, exact conversion, native cardinality, structural
      serialization, sanitized failures, deterministic ordering, upstream error
      pass-through, and cancellation without package-local unit tests.
    - Requirements: `R1.AC4`, `R1.AC5`, `R1.AC6`, `R3.AC15`, `R3.AC17`, `R4.AC11`, `R5.AC6`, `R5.AC8`, `R5.AC12`, `R5.AC14`, `R6.AC3`, `R6.AC12`, `R7.AC8`, `R7.AC9`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR8`
    - Design: Testing Strategy; Verification Plan; Security Considerations; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 20m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod make test GO_TEST_FLAGS=-race"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        timeout: 25m
        covers: ["R3.AC15", "R3.AC17", "R4.AC11", "R5.AC6", "R5.AC12", "R6.AC3", "R6.AC12", "R7.AC8", "R7.AC9", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 35m
        covers: ["R1.AC4", "R1.AC5", "R1.AC6", "R5.AC8", "R5.AC14", "NFR7", "NFR8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod go build ./..."]
        timeout: 10m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-typed-output-go-build GOMODCACHE=/tmp/kubeseer-typed-output-go-mod go mod tidy -diff"]
        timeout: 10m
