---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T09:10:26Z
last_modified: 2026-08-31T10:10:11Z
approved_fingerprint: sha256:4165e99396de27d1c60cc14480a3759231233abc56963e8f6d36fa93c7a866e6
source_design_approved_at: 2026-08-31T08:58:06Z
source_design_fingerprint: sha256:286b57aa97ad1ab9f28376243902e39ecf007e430d46845e6cc074ab23646234
---

# Implementation Plan

- [x] 1. Add the structural aggregation API contract
  - [x] 1.1 Add declarations, public outcomes, generated artifacts, and API-server proof
    - Extend each `KubeseerSource` with the optional aggregation list-map keyed
      by lower-camel-case `name`. Add closed function and rounding-mode enums,
      ordered atomic `groupBy`, optional provenance, optional precision bounded
      from 0 through 18, and the direct-field representation needed to detect
      invalid options on non-average functions. Keep omitted and explicit-empty
      aggregations default-free and compatible with existing manifests.
    - Add the structural aggregate result, group, typed key, value-state,
      aggregate match, resource provenance, resource failure, and aggregate
      state types beside existing resource results. Reuse
      `KubeseerTypedMatch`; introduce no opaque maps, raw extensions, or second
      API version. Regenerate deep-copy code and the CRD.
    - Extend `TestAPIContract` to prove list-map uniqueness, enum and precision
      admission, declaration and aggregate-status round trips, omission versus
      explicit empty behavior, typed keys/values/failures/provenance,
      deep-copy isolation, exact generated item schema, and compatibility of
      pre-aggregation manifests. Emit
      `API_CONTRACT=cross-namespace-aggregation-types STATUS=passed` only after
      the real API-server contract succeeds.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `R1.AC10`, `R1.AC11`, `R1.AC12`, `R1.AC13`, `R1.AC14`, `R1.AC15`, `R1.AC16`, `R1.AC17`, `R1.AC20`, `R5.AC12`, `R5.AC13`, `R5.AC14`, `R9.AC7`, `NFR7`, `NFR8`, `C2`, `C4`
    - Design: Components And Interfaces / Public API Declarations; Data Models / Public Aggregate Outcome; Testing Strategy / API And Envtest; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-api"]
        expect_output: "API_CONTRACT=cross-namespace-aggregation-types STATUS=passed"
        timeout: 40m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R1.AC9", "R1.AC10", "R1.AC11", "R1.AC12", "R1.AC13", "R1.AC14", "R1.AC15", "R1.AC16", "R1.AC17", "R1.AC20", "R5.AC12", "R5.AC13", "R5.AC14", "R9.AC7", "NFR7", "NFR8", "C2", "C4"]

- [x] 2. Build typed-key primitives and the immutable aggregation planner
  - [x] 2.1 Reuse canonical typed semantics and compile independent ordered plans
    - Add narrow `internal/typedoutput` helpers for type-tagged canonical keys,
      approved typed ordering, exact numeric extraction, and safe construction
      of aggregate matches. Reuse existing number, timestamp, duration, and
      quantity normalization; expose no mutable private representation and add
      no coercion path.
    - Create Apache-licensed `internal/aggregation` contracts with immutable
      plans/outcomes, defensive accessors, sanitized errors, and centrally
      declared `DefaultLimits`: 1,000 groups and 10,000 each for contributions,
      collected values, distinct values, and emitted provenance entries.
    - Implement complete per-source planning: index declared fields; process
      aggregate declarations in lexical name order; preserve `groupBy` order;
      validate references, duplicates, supported group types, reducer/type
      compatibility, and average-only options; apply effective average defaults
      of precision 6 and `halfEven`; bind copied limits; and preserve independent
      declaration failures. Do not add a required cache.
    - Extend the real extraction/typed-output/operator collaboration in
      `TestModuleIntegration` with every valid compatibility branch, unsupported
      declarations, omitted and explicit average options, equivalent-plan
      determinism, immutable accessor mutation attempts, canonical typed keys,
      no-cache recompilation, stable reason codes, and value-free diagnostics.
      Emit `MODULE_INTEGRATION=cross-namespace-aggregation-planning STATUS=passed`.
    - Requirements: `R1.AC18`, `R1.AC19`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R2.AC11`, `R2.AC12`, `R2.AC13`, `R2.AC14`, `R2.AC15`, `R2.AC16`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R8.AC1`, `R8.AC2`, `R9.AC1`, `NFR1`, `NFR2`, `NFR6`, `C1`, `C3`, `C5`, `C7`, `C8`
    - Design: Architecture; Components And Interfaces / `internal/aggregation` Layout; Components And Interfaces / Planner; Components And Interfaces / Typed Key Service; Data Models / Internal Outcome; Error Handling
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=cross-namespace-aggregation-planning STATUS=passed"
        timeout: 30m
        covers: ["R1.AC18", "R1.AC19", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R2.AC11", "R2.AC12", "R2.AC13", "R2.AC14", "R2.AC15", "R2.AC16", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R8.AC1", "R8.AC2", "R9.AC1", "NFR1", "NFR2", "NFR6", "C1", "C3", "C5", "C7", "C8"]

- [x] 3. Form deterministic groups with complete provenance
  - [x] 3.1 Consume accepted operator outcomes, deduplicate resources, and order contributions
    - Implement source-input validation and accepted-resource traversal without
      Kubernetes I/O. Preserve unsuccessful sources atomically, ignore rejected
      resources, omit unsuccessful resources from contributions while retaining
      their sanitized failures, and deduplicate repeated objects by UID before
      grouping.
    - Form one global group when `groupBy` is omitted. Otherwise require exactly
      one non-null match per grouping field, isolate missing/null/error/multiple
      key failures to that resource and aggregate, and compare complete typed
      tuples with canonical logical semantics.
    - Retain complete API version, Kind, namespace, name, and UID provenance on
      every internal contribution. Sort contributions by namespace, name, UID,
      and match index; sort final groups by canonical typed tuple; keep same-name
      resources from different namespaces unambiguous; and preserve only the
      first occurrence of a duplicate UID.
    - Extend `TestModuleIntegration` with global and multi-field groups, all
      supported key types and equivalent encodings, key cardinality failures,
      absent/null/error targets, multi-match targets, reordered inputs, duplicate
      UIDs, cluster-scoped resources, and equal names across namespaces. Emit
      `MODULE_INTEGRATION=cross-namespace-aggregation-grouping STATUS=passed`.
    - Requirements: `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R3.AC11`, `R3.AC12`, `R3.AC13`, `R3.AC14`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R5.AC1`, `R5.AC3`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `NFR2`, `NFR3`, `C3`, `C6`
    - Design: Components And Interfaces / Evaluator; Components And Interfaces / Typed Key Service; Data Models / Internal Outcome; Security Considerations; Testing Strategy / Cross-Module Integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=cross-namespace-aggregation-grouping STATUS=passed"
        timeout: 30m
        covers: ["R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R3.AC11", "R3.AC12", "R3.AC13", "R3.AC14", "R4.AC5", "R4.AC6", "R4.AC7", "R5.AC1", "R5.AC3", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "NFR2", "NFR3", "C3", "C6"]

- [x] 4. Implement the closed reducer set and exact average rounding
  - [x] 4.1 Implement collection, cardinality, selection, extrema, and distinct reducers
    - Implement `collect`, `count`, `min`, `max`, `first`, `last`, and
      `distinct` over the shared deterministic contribution order. Use approved
      typed comparison/equality, retain first-occurrence order for distinct
      values, and gather all contributors for each distinct value.
    - Implement empty-group semantics exactly: empty collections for `collect`
      and `distinct`, integer zero for `count`, and an explicit absent value for
      `min`, `max`, `first`, and `last`. Project value-level provenance for
      `collect`/`distinct`, group-level deduplicated provenance for reducers,
      and no public provenance when disabled.
    - Extend `TestModuleIntegration` across every compatible logical type,
      non-empty and empty groups, multiple matches, extrema ties, deterministic
      first/last, equivalent normalized distinct values, provenance enabled and
      disabled, and repeated input permutations. Emit
      `MODULE_INTEGRATION=cross-namespace-aggregation-reducers STATUS=passed`.
    - Requirements: `R4.AC1`, `R4.AC4`, `R4.AC8`, `R4.AC9`, `R4.AC16`, `R4.AC17`, `R4.AC18`, `R4.AC19`, `R4.AC20`, `R4.AC21`, `R4.AC22`, `R4.AC24`, `R5.AC4`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R7.AC4`, `R7.AC6`, `R7.AC7`, `R7.AC10`, `R7.AC11`, `NFR2`, `NFR3`
    - Design: Components And Interfaces / Reducers And Average Rounding; Data Models / Public Aggregate Outcome; Testing Strategy / Cross-Module Integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=cross-namespace-aggregation-reducers STATUS=passed"
        timeout: 30m
        covers: ["R4.AC1", "R4.AC4", "R4.AC8", "R4.AC9", "R4.AC16", "R4.AC17", "R4.AC18", "R4.AC19", "R4.AC20", "R4.AC21", "R4.AC22", "R4.AC24", "R5.AC4", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R7.AC4", "R7.AC6", "R7.AC7", "R7.AC10", "R7.AC11", "NFR2", "NFR3"]

  - [x] 4.2 Implement exact sums and single-rounding averages
    - Implement checked signed-64-bit integer and duration sums, exact decimal
      number sums, and exact normalized base-unit quantity sums. Return exact
      typed zero for empty sums and fail the aggregate without partial groups on
      overflow.
    - Implement integer/number averages as exact rational sum divided by exact
      count, followed by one final rounding at precision 0 through 18. Apply
      `halfEven`, `halfAwayFromZero`, `towardZero`, and `awayFromZero`
      symmetrically for positive and negative values; canonicalize negative zero
      and remove insignificant fractional zeros.
    - Extend `TestModuleIntegration` with exact decimals and quantities,
      integer/duration boundaries and overflow, typed empty zeros, terminating
      and non-terminating averages including `1 / 3`, precision 0/6/18,
      positive/negative midpoints, every rounding mode, negative zero, and
      trailing-zero normalization. Emit
      `MODULE_INTEGRATION=cross-namespace-aggregation-arithmetic STATUS=passed`.
    - Requirements: `R4.AC2`, `R4.AC3`, `R4.AC10`, `R4.AC11`, `R4.AC12`, `R4.AC13`, `R4.AC14`, `R4.AC15`, `R4.AC23`, `R4.AC24`, `R4.AC25`, `R4.AC26`, `R4.AC27`, `R4.AC28`, `R4.AC29`, `R4.AC30`, `NFR1`, `NFR2`, `C5`
    - Design: Components And Interfaces / Reducers And Average Rounding; Error Handling; Failure Modes And Tradeoffs; Testing Strategy / Cross-Module Integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=cross-namespace-aggregation-arithmetic STATUS=passed"
        timeout: 30m
        covers: ["R4.AC2", "R4.AC3", "R4.AC10", "R4.AC11", "R4.AC12", "R4.AC13", "R4.AC14", "R4.AC15", "R4.AC23", "R4.AC24", "R4.AC25", "R4.AC26", "R4.AC27", "R4.AC28", "R4.AC29", "R4.AC30", "NFR1", "NFR2", "C5"]

- [x] 5. Enforce failure isolation, limits, and cancellation
  - [x] 5.1 Produce bounded immutable batch outcomes without partial aggregate publication
    - Complete sequential `EvaluateBatch` with one aggregate outcome per
      declaration and one source outcome per input. Preserve upstream atomic
      source failures unchanged, retain unsuccessful-resource diagnostics,
      degrade computed aggregates with resource-scoped failures, isolate
      aggregate/source siblings, and reject mismatched source IDs or incomplete
      provenance before evaluation.
    - Enforce plan-bound counters before accepting each group, contribution,
      collected value, distinct value, or provenance entry. On a crossing,
      discard that aggregate's partial groups and return deterministic
      `cardinality-exceeded`; preserve all independent outcomes.
    - Check cancellation before every source/resource/aggregate/contribution
      boundary. Preserve completed sources and return sanitized
      `aggregation-interrupted` outcomes for the active and every unstarted
      source. Ensure diagnostics never contain paths, operands, values, or
      resource contents.
    - Extend `TestModuleIntegration` with planning, group, target, overflow,
      invalid-input, upstream source/resource, and sibling failures; inject
      one-below/at/one-above limits for every counter; randomize equivalent
      inputs; and cover before-batch, active-source, and later-source
      cancellation. Emit
      `MODULE_INTEGRATION=cross-namespace-aggregation-isolation STATUS=passed`.
    - Requirements: `R5.AC2`, `R5.AC4`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R6.AC13`, `R6.AC14`, `R6.AC15`, `R6.AC16`, `R6.AC17`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R9.AC8`, `R9.AC9`, `NFR4`, `NFR5`, `NFR6`, `C6`, `C7`
    - Design: Components And Interfaces / Evaluator; Components And Interfaces / Cardinality Accounting; Components And Interfaces / Cancellation; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=cross-namespace-aggregation-isolation STATUS=passed"
        timeout: 35m
        covers: ["R5.AC2", "R5.AC4", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R6.AC11", "R6.AC12", "R6.AC13", "R6.AC14", "R6.AC15", "R6.AC16", "R6.AC17", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "R9.AC8", "R9.AC9", "NFR4", "NFR5", "NFR6", "C6", "C7"]

- [x] 6. Integrate aggregate projection with status and reconciliation
  - [x] 6.1 Preserve raw results while making aggregate semantics publishable
    - Implement the pure aggregate result adapter by reusing existing operator
      projection, then append ordered aggregates beside unchanged raw resource
      outcomes. Omit aggregate fields for omitted/empty declarations and retain
      unsuccessful source/resource boundaries without reparsing public values.
    - Extend `internal/status` normalization and error detection across
      aggregate groups, keys, matches, failures, and provenance. Include
      aggregate semantics in the existing result hash, treat degraded/error
      aggregates as degradation, preserve nil/empty equivalence, and keep
      summary resource counts based only on raw resource results.
    - Insert planning/evaluation after operators in the production reconciliation
      pipeline while preserving authorization leases, cancellation guards,
      retry policy, and status ownership. Pass no client or credentials into
      aggregation and perform no additional resource reads.
    - Extend `TestModuleIntegration` with raw-result preservation, omitted and
      explicit-empty declarations, successful/degraded/error aggregate
      projection, stable source/aggregate/group ordering, semantic hash changes
      and no-ops, summary stability, and confidentiality sentinels. Emit
      `MODULE_INTEGRATION=cross-namespace-aggregation-status STATUS=passed`.
    - Extend `TestEnvtestReconciliationRuntime` through the generated CRD, real
      manager/API server, authorized multi-namespace resources, the production
      extraction/typing/operator/aggregation pipeline, status persistence, and
      repeated reconciliation. Prove runtime average defaults, requested
      provenance, invalid-plan status, sibling preservation, atomic unavailable
      namespace behavior, semantic no-op publication, and sanitized failures.
      Emit `API_CONTRACT=cross-namespace-aggregation-pipeline STATUS=passed`.
    - Requirements: `R1.AC9`, `R1.AC10`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC12`, `R6.AC1`, `R6.AC2`, `R6.AC4`, `R6.AC7`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R9.AC1`, `R9.AC2`, `R9.AC3`, `R9.AC4`, `R9.AC5`, `R9.AC6`, `R9.AC7`, `R9.AC8`, `R9.AC9`, `NFR4`, `NFR7`, `NFR8`, `C1`, `C2`, `C3`, `C4`, `C6`
    - Design: Architecture; Components And Interfaces / Result And Status Integration; Data Models / Public Aggregate Outcome; Testing Strategy / Reconciliation And Status Integration; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=cross-namespace-aggregation-status STATUS=passed"
        timeout: 35m
        covers: ["R1.AC9", "R1.AC10", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC12", "R6.AC1", "R6.AC2", "R6.AC4", "R6.AC7", "R6.AC10", "R6.AC11", "R6.AC12", "R9.AC1", "R9.AC3", "R9.AC4", "R9.AC5", "R9.AC6", "R9.AC7", "R9.AC8", "R9.AC9", "NFR4", "NFR7", "NFR8", "C1", "C2", "C3", "C4", "C6"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-api"]
        expect_output: "API_CONTRACT=cross-namespace-aggregation-pipeline STATUS=passed"
        timeout: 45m
        covers: ["R1.AC18", "R1.AC19", "R5.AC7", "R5.AC11", "R5.AC12", "R6.AC10", "R6.AC11", "R6.AC12", "R9.AC2", "R9.AC3", "R9.AC4", "R9.AC5", "R9.AC6", "R9.AC7", "NFR4", "NFR7", "NFR8", "C1", "C2", "C3", "C4", "C6"]

- [x] 7. Complete repository-wide verification
  - [x] 7.1 Verify boundaries, generated state, races, compatibility, build, and module tidiness
    - Register only the approved API, module-integration, and envtest scenarios
      in the test-layer policy. Add no dedicated unit-test layer, reducer plugin
      registry, expression language, cross-source joins, required cache,
      Kubernetes I/O in aggregation, second API version, or alternate status
      writer.
    - Run generated/schema and test-layer verification, race-enabled
      higher-layer tests, the Kubernetes 1.35.6/1.36.2 API compatibility matrix,
      full Go compilation, and read-only module tidiness with task-scoped
      writable caches. Statically confirm aggregation imports no dynamic,
      discovery, controller-runtime client, or status-writer dependency and
      that diagnostics contain none of the test value/path/body sentinels.
    - Requirements: `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`
    - Design: Simplicity And Elegance Review; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy; Verification Plan; Requirement Coverage
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 35m
        covers: ["NFR5", "NFR6", "NFR7", "NFR8", "C2", "C3", "C4", "C7", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test GO_TEST_FLAGS=-race"]
        expect_output: "MODULE_INTEGRATION=cross-namespace-aggregation-isolation STATUS=passed"
        timeout: 40m
        covers: ["NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "NFR8", "C1", "C3", "C5", "C6", "C7", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 65m
        covers: ["NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "NFR8", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod go build ./..."]
        timeout: 25m
        covers: ["C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-build GOMODCACHE=/tmp/kubeseer-cross-namespace-aggregation-go-mod go mod tidy -diff"]
        timeout: 25m
        covers: ["C1", "C2"]
