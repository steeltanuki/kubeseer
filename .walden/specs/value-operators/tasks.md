---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-30T21:53:31Z
last_modified: 2026-08-30T22:43:20Z
approved_fingerprint: sha256:643b2eda8ed84325fb30cdfaa7baa39a65b9069f5cfb9e49a6d411d4d59185b4
source_design_approved_at: 2026-08-30T13:12:34Z
source_design_fingerprint: sha256:802b5ac2d8148015b1cb6b675eac4f42ee7faca3a6c87895a5dbabdf5f95b192
---

# Implementation Plan

- [x] 1. Add the structural operator API contract
  - [x] 1.1 Add declarations, operands, resource errors, generated artifacts, and API-server proof
    - Extend `KubeseerField` with the optional atomic ordered operator list and
      add the closed lower-camel-case `KubeseerOperatorName` enum, operator
      entry, and structural tagged operand union from the approved design. Keep
      omitted and explicit-empty lists default-free and semantically identical.
    - Add the optional resource-level result error used for unsuccessful
      operator resources. Regenerate deep-copy code and the Kubeseer CRD without
      preserve-unknown regions or a second API version.
    - Extend `TestAPIContract` to prove supported-name admission, unsupported
      name rejection, typed operand and declaration-order round trips, explicit
      null representation, omitted/empty compatibility, deep-copy isolation,
      resource-error persistence, exact generated list/item/enum schemas, and
      the absence of opaque schema regions. Emit
      `API_CONTRACT=value-operators-types STATUS=passed` only after the real API
      server contract succeeds.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `R1.AC10`, `R1.AC11`, `R1.AC12`, `NFR6`, `NFR7`, `C2`, `C7`
    - Design: Components And Interfaces / Public Operator Declaration Contract; Data Models; Components And Interfaces / Structural Result Adapter And Pipeline Composition; Testing Strategy; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test-api"]
        expect_output: "API_CONTRACT=value-operators-types STATUS=passed"
        timeout: 35m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R1.AC9", "R1.AC10", "R1.AC11", "R1.AC12", "NFR6", "NFR7", "C2", "C7"]

- [x] 2. Build the typed-output bridge and immutable operator planner
  - [x] 2.1 Reuse exact conversion/comparison semantics and compile complete source plans
    - Add the narrow immutable typed-output bridge for configured match
      conversion, equality, three-way ordering, field-match replacement, and
      pure result projection. Refactor only shared conversion/comparison cores;
      do not expose mutable private values or introduce a second coercion path.
    - Create Apache-licensed `internal/operators` contracts with the seven
      stable reason codes, sanitized `OperatorError`, immutable plans and
      defensive accessors. Implement deterministic `CompileSource` and
      `CompileBatch`: sort fields lexicographically, retain declaration indexes,
      validate the complete source before resource evaluation, convert operands
      in input order, precompile Go regular expressions, and use deterministic
      no-cache recompilation.
    - Extend the real extraction-to-typed-output-to-operators collaboration in
      `TestModuleIntegration`. Exercise every operator/type compatibility and
      arity branch; scalar/composite operand conversion; inferred, null,
      multi-branch, mismatched and forbidden operands; duplicate membership
      values; malformed regex; complete-source planning; sibling-source
      isolation; immutable accessors; equivalent-plan determinism; and
      value-free diagnostics. Emit
      `MODULE_INTEGRATION=value-operators-planning STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R2.AC11`, `R2.AC12`, `R2.AC13`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R3.AC11`, `R3.AC12`, `R3.AC13`, `R3.AC14`, `R3.AC15`, `R3.AC16`, `R8.AC1`, `R8.AC2`, `NFR1`, `NFR2`, `NFR5`, `C1`, `C3`, `C5`, `C6`, `C10`
    - Design: Architecture; Components And Interfaces / Typed-Output Operator Bridge; Components And Interfaces / Operator Planner; Data Models; Data Models / Compatibility And Arity Matrix; Evaluation Algorithms / Source Planning; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=value-operators-planning STATUS=passed"
        timeout: 25m
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R2.AC11", "R2.AC12", "R2.AC13", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R3.AC11", "R3.AC12", "R3.AC13", "R3.AC14", "R3.AC15", "R3.AC16", "R8.AC1", "R8.AC2", "NFR1", "NFR2", "NFR5", "C1", "C3", "C5", "C6", "C10"]

- [x] 3. Implement exact equality and ordered predicates
  - [x] 3.1 Evaluate scalar comparisons over typed multi-match outcomes
    - Implement `eq`, `ne`, `gt`, `gte`, `lt`, and `lte` over the immutable
      typed-output bridge. Use existential semantics for positive predicates and
      non-empty all-unequal semantics for `ne`; treat absent and all-null fields
      as false.
    - Compare exact signed integers, normalized decimals, represented timestamp
      instants, duration nanoseconds, quantity magnitudes, case-sensitive
      strings, and logical booleans without display-text or floating-point
      dependence. Preserve input fields and match order because predicates do
      not transform values.
    - Extend `TestModuleIntegration` with equal/unequal and every ordered
      boundary, equivalent number/quantity/timestamp encodings, wildcard
      multi-match positives and negatives, null/absent sets, boolean/string
      equality, deterministic repetition, and caller-mutation attempts. Emit
      `MODULE_INTEGRATION=value-operators-comparisons STATUS=passed`.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R4.AC10`, `R4.AC11`, `R4.AC12`, `R4.AC13`, `R4.AC14`, `R4.AC15`, `R4.AC16`, `R4.AC17`, `NFR2`, `NFR3`
    - Design: Components And Interfaces / Typed-Output Operator Bridge; Components And Interfaces / Predicate And Transformation Evaluator; Evaluation Algorithms / Resource Evaluation; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=value-operators-comparisons STATUS=passed"
        timeout: 25m
        covers: ["R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R4.AC10", "R4.AC11", "R4.AC12", "R4.AC13", "R4.AC14", "R4.AC15", "R4.AC16", "R4.AC17", "NFR2", "NFR3"]

- [x] 4. Implement string, presence, and membership predicates
  - [x] 4.1 Apply case-sensitive string, cardinality, and set semantics
    - Implement `contains`, `startsWith`, `endsWith`, and precompiled `matches`
      using existential non-null string semantics. Implement `exists` and
      `notExists` from field cardinality so null, empty string, empty object,
      and empty list remain present.
    - Implement `in` and `notIn` with exact typed equality, preserving duplicate
      member equivalence and requiring a non-empty non-null observed set for the
      negative predicate. Keep absent and all-null membership decisions false.
    - Extend `TestModuleIntegration` with case-sensitive substring/prefix/suffix
      and Go-regex cases, multi-match outcomes, empty and null presence,
      scalar-type membership, duplicate members, absent/all-null negative cases,
      and deterministic field/match preservation. Emit
      `MODULE_INTEGRATION=value-operators-predicates STATUS=passed`.
    - Requirements: `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R5.AC12`, `R5.AC13`, `R5.AC14`, `R5.AC15`, `NFR2`, `NFR3`
    - Design: Components And Interfaces / Predicate And Transformation Evaluator; Data Models / Compatibility And Arity Matrix; Evaluation Algorithms / Resource Evaluation; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=value-operators-predicates STATUS=passed"
        timeout: 25m
        covers: ["R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R5.AC12", "R5.AC13", "R5.AC14", "R5.AC15", "NFR2", "NFR3"]

- [x] 5. Compose transformations and immutable resource outcomes
  - [x] 5.1 Apply ordered chains and project accepted, rejected, and unsuccessful resources
    - Implement ordered field chains, resource-wide implicit AND, identity
      chains, `default` absent/null replacement, and `coalesce` first-non-null
      selection. Feed every transformation into the next declared operator and
      keep all temporary state resource-local.
    - Add immutable source/resource outcomes with exactly accepted, rejected, or
      unsuccessful resource state. Accepted resources retain provenance,
      lexicographic transformed fields, and approved match ordering; rejected
      resources retain a successful decision but expose no downstream fields;
      unsuccessful resources retain provenance and a sanitized failure while
      discarding all temporary fields.
    - Implement the pure operator result adapter using typed-output projection
      helpers. Omit rejected resources, preserve successful empty sources, and
      serialize unsuccessful resources with provenance, no fields, and the new
      resource error. Extend existing status result normalization/degradation to
      recognize resource errors without changing condition policy.
    - Extend `TestModuleIntegration` with transformation-before-predicate and
      predicate-before-transformation chains, multiple fields, identity and
      transformation-only chains, accepted/rejected/unsuccessful projection,
      empty accepted collections, provenance/order, immutable accessors, partial
      failure atomicity, and semantically equivalent repetitions. Emit
      `MODULE_INTEGRATION=value-operators-transformations STATUS=passed`.
    - Requirements: `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R6.AC13`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R7.AC12`, `R7.AC13`, `R7.AC14`, `NFR2`, `NFR3`, `NFR4`
    - Design: Components And Interfaces / Predicate And Transformation Evaluator; Components And Interfaces / Batch Operator Engine And Outcomes; Components And Interfaces / Structural Result Adapter And Pipeline Composition; Data Models; Evaluation Algorithms / Resource Evaluation; Error Handling
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=value-operators-transformations STATUS=passed"
        timeout: 30m
        covers: ["R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R6.AC11", "R6.AC12", "R6.AC13", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "R7.AC12", "R7.AC13", "R7.AC14", "NFR2", "NFR3", "NFR4"]

- [x] 6. Integrate isolated batch evaluation into reconciliation
  - [x] 6.1 Preserve failures, cancellation partitions, and status publication boundaries
    - Implement sequential `EvaluateBatch` over matching plans and typed source
      outcomes. Preserve upstream source errors unchanged, validate source and
      field identity/type, isolate planning/evaluation failures, retain one
      outcome per source/resource, and preserve completed sources when context
      cancellation marks the active and later sources interrupted.
    - Insert operator evaluation between `typedoutput.ConvertBatch` and result
      construction in the production reconciliation pipeline. Add operator
      planning to the existing configuration assessment, retain lease and
      cancellation guards around the stage, preserve retry/publication policy,
      and publish accepted values plus sanitized unsuccessful resource errors
      without exposing rejected values.
    - Extend `TestModuleIntegration` with mixed sources/resources, upstream
      pass-through identity, plan/source and field/type mismatches, operator-field
      typed failures, sibling isolation, stable diagnostics and confidentiality
      sentinels, completed/active/unstarted cancellation partitions, and
      deterministic outcome ordering. Emit
      `MODULE_INTEGRATION=value-operators-isolation STATUS=passed`.
    - Extend `TestEnvtestReconciliationRuntime` through the generated CRD, real
      manager/API server, approved production pipeline, authorized selected
      resources, extraction, typed conversion, operator planning/evaluation,
      status derivation, and publication. Prove valid filtering/transformation,
      invalid-plan configuration status, rejected empty results, partial
      resource degradation, semantic no-op behavior, preserved sibling sources,
      and sanitized status. Emit
      `API_CONTRACT=value-operators-pipeline STATUS=passed`.
    - Requirements: `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R7.AC12`, `R7.AC13`, `R7.AC14`, `R7.AC15`, `R7.AC16`, `R7.AC17`, `R7.AC18`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `R8.AC11`, `NFR2`, `NFR4`, `NFR5`, `NFR7`, `C4`, `C8`
    - Design: Architecture; Components And Interfaces / Batch Operator Engine And Outcomes; Components And Interfaces / Structural Result Adapter And Pipeline Composition; Evaluation Algorithms; Error Handling; Security Considerations; Testing Strategy; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=value-operators-isolation STATUS=passed"
        timeout: 30m
        covers: ["R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "R7.AC12", "R7.AC13", "R7.AC14", "R7.AC15", "R7.AC16", "R7.AC17", "R7.AC18", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "R8.AC10", "R8.AC11", "NFR2", "NFR4", "NFR5", "NFR7", "C4", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test-api"]
        expect_output: "API_CONTRACT=value-operators-pipeline STATUS=passed"
        timeout: 40m
        covers: ["R2.AC1", "R2.AC6", "R7.AC1", "R7.AC4", "R7.AC5", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC12", "R7.AC13", "R7.AC14", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC11", "NFR4", "NFR5", "NFR7", "C4", "C8"]

- [x] 7. Complete repository-wide verification
  - [x] 7.1 Verify boundaries, generated state, race safety, compatibility, build, and module tidiness
    - Register only the approved API, module-integration, and envtest scenarios
      in the test-layer policy. Add no package-local unit tests, arbitrary
      expression engine, cross-field references, aggregation, operator cache,
      product-wide limits, Kubernetes I/O in operators, second API version, or
      status/publication policy outside its existing owner.
    - Run generated and test-layer verification, race-enabled higher-layer
      tests, the Kubernetes 1.35.6/1.36.2 API compatibility matrix, complete Go
      compilation, and read-only module tidiness with task-scoped writable
      caches. Confirm every named marker is non-vacuous and diagnostics contain
      no operand, path, typed-value, extracted-value, or resource-body sentinel.
    - Requirements: `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`, `C10`
    - Design: Simplicity And Elegance Review; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy; Verification Plan; Requirement Coverage
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 30m
        covers: ["NFR5", "NFR6", "NFR7", "C2", "C4", "C6", "C7", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test GO_TEST_FLAGS=-race"]
        expect_output: "MODULE_INTEGRATION=value-operators-isolation STATUS=passed"
        timeout: 35m
        covers: ["NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR7", "C1", "C3", "C4", "C5", "C6", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 60m
        covers: ["NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod go build ./..."]
        timeout: 20m
        covers: ["C1", "C2", "C3", "C4", "C5", "C6", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-value-operators-go-build GOMODCACHE=/tmp/kubeseer-value-operators-go-mod go mod tidy -diff"]
        timeout: 20m
        covers: ["C1", "C9"]
