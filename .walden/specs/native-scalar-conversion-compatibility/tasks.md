---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-10T06:22:22Z
last_modified: 2026-09-10T07:14:19Z
approved_fingerprint: sha256:3102d5ba1a4b95d0c4269f57cfcd70755d681c00ad45a5230a6b4b6836eb5359
source_design_approved_at: 2026-09-10T06:13:03Z
source_design_fingerprint: sha256:182a17eac071079fa41375e3acd7a714dd47a0a5c9ff9821a20b026f8e35f809
---

# Implementation Plan

## Execution Boundaries

Requirements and Design are approved; this Tasks draft is not implementation
approval. Execute only explicitly requested tasks after this plan is approved.

Before execution, inspect the typed-output-model Design conversion matrix and
Tasks scalar/persistence proofs for enumerated grammar assumptions. Reconcile
and review any approved document that must change; otherwise extend the current
higher-layer acceptance matrices without changing their contract or proof command.
See the approved Design's Baseline Contract Reconciliation.

<!-- assumed: quantity and duration stay separate incremental code tasks, followed by shared pipeline/API proof and documentation (source: approved Design, Components And Interfaces) -->

## Proof Conventions

Run tasks in numerical order. Each task creates and registers the named new
TestModuleIntegration or TestAPIContract scenario before executing its proof.
Tests start from real extraction and production conversion, not private-helper
unit tests. Assert native parsing separately from independently stated exact
expected magnitudes, including negative controls. Keep dependencies and public
schemas unchanged. Use writable caches and repository-pinned isolated envtest
assets; never use ambient kubeconfig. Proofs are read-only and require the named
PASS output, failing rather than skipping unavailable infrastructure.
No implementation test is executed or claimed as passing during planning.
Documentation fixture checks complement a recorded manual prose/link review.

## Tasks

- [x] 1. Extend exact native scalar conversion
  - [x] 1.1 Support exact nano/micro quantities without relaxing native grammar or precision
    - Extend quantityPattern and quantitySuffixFactor in internal/typedoutput/converters.go with n and u and exact rational factors 1/10^9 and 1/10^6. Retain native resource.ParseQuantity canonicalization, the existing magnitude ceiling, and comparison against the exact parsed value; do not upgrade dependencies.
    - Preserve distinct InvalidValue, ConversionOverflow, and ForbiddenConversion outcomes for invalid syntax, out-of-range exact magnitudes (including native rejection/saturation), and native rounding. Do not leak raw parser errors.
    - Extend assertTypedOutputConversionScenarios and register NativeQuantityCompatibility using shared fixture helpers. Feed actual extraction outcomes through conversion and public serialization. Cover 100n, 100u, signed variants, all accepted suffix families, decimal/exponent equivalents, binary controls, exact boundaries, malformed inputs, and quantity 0.0000000001 rejection.
    - Assert exact normalized base units and native canonical text independently; equivalent magnitudes need not have identical lexical canonical text. Retain every existing successful scalar matrix row.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `NFR1`, `NFR2`
    - Design: Components And Interfaces / Quantity conversion; Error Handling; Testing Strategy
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=1","-run","^TestModuleIntegration$/^NativeQuantityCompatibility$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/NativeQuantityCompatibility"
        covers: ["R1.AC1","R1.AC2","R1.AC3","R1.AC4","R1.AC5","R1.AC6","R1.AC7","R1.AC8","R1.AC9"]
        timeout: 10m
  - [x] 1.2 Support signed zero and both Unicode microsecond spellings with exact duration arithmetic
    - Recognize exactly 0, +0, and -0 as unitless zero in the exact duration helper. Add U+03BC GREEK SMALL LETTER MU plus s beside ASCII us and U+00B5 MICRO SIGN plus s; match complete UTF-8 units without broad Unicode normalization.
    - Keep exact rational compound accumulation, signed int64 bounds, and equality with time.ParseDuration. Preserve ConversionOverflow for syntactically valid out-of-range magnitudes, ForbiddenConversion for fractional nanoseconds, and InvalidValue for malformed native forms.
    - Extend the shared conversion matrix and register NativeDurationCompatibility using real extraction, conversion, and serialization. Explicitly encode both Unicode code points; cover signed zeros, positive/negative microseconds, equivalent compound forms, signed int64 boundaries, ordinary duration controls, and canonical duration.String output.
    - Retain rejection of 0.1ns, empty text, bare signs, nonzero unitless numbers, malformed compounds, and independent positive/negative overflow cases. Do not broaden other logical-type conversion.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `NFR1`, `NFR2`
    - Design: Components And Interfaces / Duration conversion; Data Models; Error Handling; Testing Strategy
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=1","-run","^TestModuleIntegration$/^NativeDurationCompatibility$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/NativeDurationCompatibility"
        covers: ["R2.AC1","R2.AC2","R2.AC3","R2.AC4","R2.AC5","R2.AC6","R2.AC7","R2.AC8","R2.AC9"]
        timeout: 10m

- [x] 2. Prove public pipeline compatibility and persistence
  - [x] 2.1 Verify field isolation, shared consumers, and exact status round trips
    - Create NativeScalarPipeline under TestModuleIntegration using real extraction, conversion batches, public serialization, and mixed-resource/mixed-field fixtures. Exercise all four reported cases, absent versus null, and established decimal, exponent, binary-quantity, and compound-duration controls.
    - For a field with successful and failing matches, assert all of that field's typed matches are discarded while independent fields survive. Check stable public field names/type tags and redaction of sentinel values, JSONPath expressions, native parser messages, and containing resources from public diagnostics.
    - Trace the existing operator and aggregation bridges that consume shared converters; include corrected lexical values and current valid/invalid controls through affected production consumers without changing operator/reducer semantics.
    - Add NativeScalarPersistence under TestAPIContract using the existing isolated envtest status-subresource suite. Store production-serialized corrected quantity/duration payloads, re-read and decode them, and compare canonical strings, exact normalized values, type tags, and absence/null representation. No CRD or dependency change is expected; investigate any generated-schema diff rather than accepting it.
    - Requirements: `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `NFR3`, `NFR4`
    - Design: Components And Interfaces / Pipeline, public payload, and consumers; Security Considerations; Baseline Contract Reconciliation; Verification Plan
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=1","-run","^TestModuleIntegration$/^NativeScalarPipeline$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/NativeScalarPipeline"
        covers: ["R3.AC1","R3.AC3","R3.AC4","R3.AC5","R3.AC6","R3.AC7","R3.AC8"]
        timeout: 10m
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","make","test-api","GO_TEST_FLAGS=-count=1"]
        expect_output: "--- PASS: TestAPIContract/NativeScalarPersistence"
        covers: ["R3.AC1","R3.AC2"]
        timeout: 20m

- [x] 3. Deliver public documentation
  - [x] 3.1 Document native spellings, exactness limits, and canonical scalar payloads
    - Update docs/api-reference.md with exact n/u quantity examples, quoted 0/+0/-0 duration inputs, ASCII us and both explicitly named U+00B5/U+03BC spellings, and canonical text plus normalized public values.
    - Update docs/concepts-and-architecture.md to explain pinned native grammar plus independent exactness/range checks, signed limits, overflow, and no rounding. Keep quantity 0.0000000001 and duration 0.1ns as rejected precision examples.
    - Use synthetic scalar-only payloads with no unrelated resource contents or sensitive fields. Create NativeScalarDocumentation under TestModuleIntegration: extract the documented example table/snippets, compare them with actual extraction/conversion/serialization fixtures, distinguish both Unicode code points, verify negative examples, and validate local file/fragment links in changed sections.
    - Manually review both guides for parser-version accuracy, quoted string inputs, public field names, canonical versus normalized terminology, and confidentiality. Record that review with the task evidence; automated example checks alone do not certify the prose.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `NFR5`
    - Design: Components And Interfaces / Documentation delivery; Data Models; Error Handling; Verification Plan
    - Verification:
      - command: ["env","GOCACHE=/tmp/kubeseer-corrective-go-build","GOMODCACHE=/tmp/kubeseer-corrective-go-mod","rtk","proxy","go","test","-v","-count=1","-run","^TestModuleIntegration$/^NativeScalarDocumentation$","./test/integration"]
        expect_output: "--- PASS: TestModuleIntegration/NativeScalarDocumentation"
        covers: ["R4.AC1","R4.AC2","R4.AC3","R4.AC4"]
        timeout: 10m
