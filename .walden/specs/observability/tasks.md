---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T16:39:28Z
last_modified: 2026-08-31T17:57:04Z
approved_fingerprint: sha256:c61137591fb06e534bea5544e8c49a69ecd0e66bec22782758b4273b7e5516c0
source_design_approved_at: 2026-08-31T16:16:10Z
source_design_fingerprint: sha256:169d2a8089da98599481ba8514e9284dabbd58f66b9b7f034ff9d6463ad064d8
---

# Implementation Plan

<!-- assumed: all automated proof remains in the existing TestModuleIntegration and TestEnvtestReconciliationRuntime entry points, with feature-specific success markers and no dedicated package-local unit suite (source: approved Design / Testing Strategy and .walden/constitution.md) -->
<!-- assumed: Kubeseer introduces no owned asynchronous telemetry queue, so the conditional queue requirements are satisfied by the synchronous passive observer and a static absence check (source: approved Design / Sink isolation) -->

- [x] 1. Establish the passive observability core
  - [x] 1.1 Add stable signal contracts, collectors, logging, setup validation, and no-op behavior
    - Create `internal/observability` with immutable allowlisted DTOs, closed
      event/outcome/reason/stage/kind/scope vocabularies, `InternalError`
      fallback, observer/no-op contracts, immutable attempt context, injected
      clock and concurrency-safe opaque ID generation. Keep sink methods unable
      to replace domain outcomes and retain no authorization history.
    - Implement structured logging with the common event/outcome/reason/severity
      envelope and trace correlation extraction. Implement all nine exact
      Prometheus collectors against an injected registry, finite private label
      constructors, non-negative duration handling, independent sink delivery,
      and sanitized `ObservabilitySetupInvalid` rejection for nil/invalid or
      duplicate registration.
    - Introduce no goroutine, channel, queue, custom metrics server, persisted
      telemetry, CRD field, or packaging resource. Register higher-layer core
      scenarios in `TestModuleIntegration` covering deterministic envelopes,
      exact family/label exposition, duplicate registration, unknown-reason
      fallback, disabled tracing, concurrent accounting, forbidden label/data
      sentinels, and unavailable capture-sink isolation. Emit
      `MODULE_INTEGRATION=observability-core STATUS=passed` only after the matrix
      succeeds.
    - Requirements: `R1.AC1`, `R1.AC9`, `R3.AC1`, `R3.AC11`, `R3.AC12`, `R3.AC13`, `R3.AC14`, `R3.AC15`, `R5.AC5`, `R5.AC7`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R7.AC2`, `R7.AC3`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `NFR1`, `NFR2`, `NFR3`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `C1`, `C2`, `C3`, `C4`, `C6`, `C7`, `C8`, `C9`, `C10`
    - Design: Architecture; Components And Interfaces / Observability setup, Correlated attempt and stage handles, Structured log emitter, Sink isolation; Data Models And Stable Vocabulary; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=observability-core STATUS=passed"
        timeout: 35m
        covers: ["R1.AC1", "R1.AC9", "R3.AC1", "R3.AC11", "R3.AC12", "R3.AC13", "R3.AC14", "R3.AC15", "R5.AC5", "R5.AC7", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R7.AC2", "R7.AC3", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "NFR1", "NFR2", "NFR3", "NFR5", "NFR6", "NFR7", "NFR8", "C1", "C2", "C3", "C4", "C6", "C7", "C8", "C9", "C10"]

- [x] 2. Instrument authorization evidence and successful resource reads
  - [x] 2.1 Wire ordered authorization audit and bounded LIST-page observations
    - Implement the observability adapter for the existing
      `authorization.Recorder`. Copy only `authorization.Record` fields,
      preserve batch order, choose the exact decision/read-forbidden event,
      include policy identity only when present, update the authorization
      metric once, retain no history, and preserve the pre-I/O evidence barrier
      and allow/deny outcome under sink failure.
    - Add the producer-owned selection `PageObserver` option and invoke it only
      after each successful non-nil LIST page. Pass only scope and returned item
      count; count pages re-read after expiration while excluding objects,
      identities, selectors, field paths, and values.
    - Extend `TestModuleIntegration` through the real enforcer/capability and
      executor paths. Prove ordered active/missing/invalid/unavailable policy
      evidence, forbidden enforcement, audit-before-I/O, no retention, exact
      authorization labels, namespaced/cluster resource counts, pagination and
      expiration accounting, confidentiality, and passive failing sinks. Emit
      `MODULE_INTEGRATION=observability-boundaries STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R3.AC4`, `R3.AC9`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R8.AC3`, `R8.AC4`, `R8.AC7`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR8`, `C1`, `C2`, `C9`
    - Design: Components And Interfaces / Authorization adapter, Selection page observer, Sink isolation; Data Flow / Reconciliation; Security Considerations; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=observability-boundaries STATUS=passed"
        timeout: 35m
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R3.AC4", "R3.AC9", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R8.AC3", "R8.AC4", "R8.AC7", "NFR1", "NFR2", "NFR3", "NFR4", "NFR8", "C1", "C2", "C9"]

- [x] 3. Correlate runtime attempts, outcomes, metrics, and optional traces
  - [x] 3.1 Instrument the production reconciliation lifecycle with exactly-once completion
    - Add the optional observer dependency to the runtime and preserve no-op
      construction. Start one attempt before the first read, bind UID/generation
      after load, wrap fixed production stages, and route all private returns
      through one authoritative terminal classification and deferred completion
      without changing `reconcile.Result`, errors, retry policy, or status.
    - Emit exact start/skipped/retry/failed/completed and source-failure records;
      update reconciliation/duration, terminal source, retained JSONPath, and
      publishable result metrics once at their final production boundaries.
      Exclude stale/canceled candidate values and sanitize unknown failures.
    - When configured, create one root span and fixed child-stage spans with
      stable attributes and correlated log IDs. Keep tracing disabled by
      default and preserve outcomes under capture/export failure.
    - Extend `TestModuleIntegration` with success, absent, deleting, retryable,
      terminal, degraded, source failure, JSONPath failure, stale/canceled, no
      provider, active provider, exporter failure, and concurrent attempt
      scenarios. Assert exact record/metric counts and span hierarchy, then emit
      `MODULE_INTEGRATION=observability-runtime STATUS=passed`.
    - Requirements: `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC9`, `R1.AC10`, `R1.AC11`, `R1.AC12`, `R3.AC2`, `R3.AC3`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC8`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC8`, `R8.AC1`, `R8.AC2`, `R8.AC6`, `R8.AC7`, `NFR1`, `NFR2`, `NFR4`, `NFR5`, `NFR6`, `NFR8`, `C1`, `C2`, `C6`, `C9`
    - Design: Components And Interfaces / Correlated attempt and stage handles, Structured log emitter, Runtime outcome observers, Sink isolation; Data Flow / Reconciliation; Error Handling; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=observability-runtime STATUS=passed"
        timeout: 40m
        covers: ["R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC9", "R1.AC10", "R1.AC11", "R1.AC12", "R3.AC2", "R3.AC3", "R3.AC5", "R3.AC6", "R3.AC7", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC8", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC8", "R8.AC1", "R8.AC2", "R8.AC6", "R8.AC7", "NFR1", "NFR2", "NFR4", "NFR5", "NFR6", "NFR8", "C1", "C2", "C6", "C9"]

- [x] 4. Observe status publication and persist semantic Kubernetes Events
  - [x] 4.1 Add publication outcomes, deterministic Event selection, and real API-server proof
    - Add a variadic no-error observer option to `NewStatusPublisher` while
      preserving existing callers. Report exactly one written, skipped,
      conflicted, or failed publication outcome; emit the required written and
      skipped records and update the status metric only after authoritative
      classification.
    - After and only after a successful semantic status write, select the first
      non-success condition in Accepted, Authorized, SourcesResolved, Ready
      order, otherwise select `EvaluationSucceeded`. Record one manager Event
      using the canonical sanitized message, `Normal` only for success and
      `Warning` otherwise. Emit none for skips/conflicts/failures and never
      alter persisted status when Event delivery fails.
    - Extend `TestModuleIntegration` with all publication outcomes, condition
      priorities, success/warning types, malformed fallback, no per-target/data
      Event amplification, confidentiality, and failing recorder behavior. Emit
      `MODULE_INTEGRATION=observability-status STATUS=passed`.
    - Extend existing `TestEnvtestReconciliationRuntime` through the real
      manager/API server. Assert one persisted sanitized Event for a semantic
      transition and no duplicate after an unchanged reconcile or unpublished
      candidate. Emit `API_CONTRACT=observability-events STATUS=passed`.
    - Requirements: `R1.AC6`, `R1.AC7`, `R3.AC8`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R8.AC5`, `R8.AC7`, `NFR1`, `NFR2`, `NFR4`, `NFR6`, `NFR8`, `C1`, `C2`, `C5`, `C7`, `C9`
    - Design: Components And Interfaces / Status observer and Kubernetes Event recorder, Sink isolation; Data Flow / Status Event; Error Handling; Security Considerations; Testing Strategy / Envtest
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=observability-status STATUS=passed"
        timeout: 40m
        covers: ["R1.AC6", "R1.AC7", "R3.AC8", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R8.AC7", "NFR1", "NFR2", "NFR4", "NFR6", "NFR8", "C1", "C2", "C5", "C7", "C9"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-api"]
        expect_output: "API_CONTRACT=observability-events STATUS=passed"
        timeout: 50m
        covers: ["R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R6.AC1", "R6.AC2", "R6.AC5", "R7.AC1", "R7.AC4", "R8.AC5", "R8.AC7", "NFR1", "NFR2", "NFR4", "NFR6", "NFR8", "C5", "C9"]

- [x] 5. Observe authorized source watch lifecycle
  - [x] 5.1 Add sanitized unexpected-stop and restart observations to the route supervisor
    - Add the optional route-registry watch observer. Emit
      `SourceWatchStopped` for unexpected termination and
      `SourceWatchRestarted` immediately before a replacement is scheduled;
      increment the restart metric exactly once using existing stable
      forbidden/expired/unavailable reasons or `InternalError` fallback.
    - Include only structured authorized target group/resource/scope/namespace,
      reason, and retry state in logs. Emit nothing for expected owning-context
      cancellation, expose no observed object identity/value, and preserve
      existing backoff, restart, cancellation, and authorization behavior under
      sink failure.
    - Extend `TestModuleIntegration` through production route/watch boundaries
      for forbidden, expiration, unavailable, unknown, repeated restart,
      expected cancellation, and failing capture scenarios. Emit
      `MODULE_INTEGRATION=observability-watch STATUS=passed`.
    - Requirements: `R1.AC8`, `R3.AC10`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR8`, `C1`, `C2`, `C9`
    - Design: Components And Interfaces / Watch lifecycle observer, Sink isolation; Data Flow / Watch restart; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=observability-watch STATUS=passed"
        timeout: 40m
        covers: ["R1.AC8", "R3.AC10", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "NFR1", "NFR2", "NFR3", "NFR4", "NFR8", "C1", "C2", "C9"]

- [x] 6. Compose observability through the real manager and production pipeline
  - [x] 6.1 Wire every adapter once and prove the complete passive signal contract
    - Extend manager-wide reconciliation options with only the optional trace
      provider. In `SetupWithManager`, create one observer from manager logger,
      controller-runtime registry, and manager Event recorder; inject its
      authorization, selection, runtime, status, and watch adapters exactly once.
      Reject setup before manager start on collector/configuration failure and
      preserve all existing direct-construction no-op defaults.
    - Complete `TestModuleIntegration` with the real production pipeline and
      replaceable logger, fresh registry, capture exporter, failing capture
      sink, counting readers, and concurrent Kubeseer attempts. Assert every
      required log/metric/audit/span signal, exact counts/order/correlation,
      remaining-sink delivery, no telemetry-only reads, no retained history,
      fixed label cardinality, no forbidden payload/error sentinel, unchanged
      domain/retry/status results, and no owned queue. Emit exactly
      `MODULE_INTEGRATION=observability STATUS=passed` after the full matrix.
    - Confirm manager facilities are reused, no second endpoint/exporter is
      started, and no public API, generated CRD, packaging manifest, durable
      store, dashboard, alert, or product-wide performance control is added.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `R1.AC10`, `R1.AC11`, `R1.AC12`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R3.AC11`, `R3.AC12`, `R3.AC13`, `R3.AC14`, `R3.AC15`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`, `C10`
    - Design: Overview; Architecture; Components And Interfaces; Data Models And Stable Vocabulary; Data Flow; Error Handling; Security Considerations; Testing Strategy; Requirement Coverage
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=observability STATUS=passed"
        timeout: 45m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R1.AC9", "R1.AC10", "R1.AC11", "R1.AC12", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R3.AC11", "R3.AC12", "R3.AC13", "R3.AC14", "R3.AC15", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC6", "R8.AC7", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "NFR8", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-api"]
        expect_output: "API_CONTRACT=observability-events STATUS=passed"
        timeout: 50m
        covers: ["R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R8.AC5", "NFR1", "NFR2", "NFR4", "NFR6", "NFR8", "C5", "C9"]

- [x] 7. Complete repository-wide verification
  - [x] 7.1 Verify boundaries, generated state, race safety, compatibility, build, and module tidiness
    - Register observability integration/envtest helpers in the existing
      test-layer policy and add a static observability boundary verifier. Reject
      package-local tests, free-form/high-cardinality labels, Kubeseer-owned
      async queues, telemetry enrichment reads, public API fields, generated
      CRD drift, custom metrics servers, durable audit storage, and packaging
      resources. Preserve approved Apache headers.
    - Run repository verification, race-enabled module integration, the full API
      compatibility matrix (including real Event persistence), complete build,
      and read-only module tidiness with task-scoped writable caches.
    - Requirements: `R3.AC11`, `R3.AC12`, `R3.AC13`, `R3.AC14`, `R6.AC1`, `R6.AC2`, `R6.AC6`, `R6.AC7`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`, `C10`
    - Design: Simplicity And Elegance Review; Sink isolation; Security Considerations; Testing Strategy; Verification Plan; Requirement Coverage
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make verify"]
        expect_output: "Observability boundary verification passed"
        timeout: 35m
        covers: ["R3.AC11", "R3.AC12", "R3.AC13", "R6.AC1", "R6.AC2", "R6.AC6", "R6.AC7", "R7.AC6", "R7.AC7", "R7.AC9", "NFR1", "NFR2", "NFR3", "NFR6", "NFR7", "NFR8", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-integration GO_TEST_FLAGS=-race"]
        expect_output: "MODULE_INTEGRATION=observability STATUS=passed"
        timeout: 50m
        covers: ["R3.AC14", "R7.AC8", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR8", "C1", "C2", "C9"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 75m
        covers: ["NFR1", "NFR2", "NFR4", "NFR6", "NFR7", "NFR8", "C1", "C3", "C5", "C9"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod go build ./..."]
        timeout: 25m
        covers: ["C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-observability-go-build GOMODCACHE=/tmp/kubeseer-observability-go-mod go mod tidy -diff"]
        timeout: 25m
        covers: ["C1", "C6", "C10"]
