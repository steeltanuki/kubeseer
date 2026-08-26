---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T19:43:44Z
last_modified: 2026-08-26T21:08:53Z
approved_fingerprint: sha256:f8dae1f6ead8dfc1d392837f7dc7a997b538b41892dae65e768974591957a171
source_design_approved_at: 2026-08-26T19:32:54Z
source_design_fingerprint: sha256:b9c28d2c7b5eb65532ab7c9ab7bf3c5060dd700b1b281f318dd16a643e4b20d9
---

# Implementation Plan

- [x] 1. Establish lifecycle tracking, periodic scheduling, and queue semantics
  - [x] 1.1 Add freshness leases, lifecycle event handling, and the trigger source
    - Create `internal/reconciliation` contracts with Apache 2.0 headers,
      constructor validation, a positive internal safety interval, narrow
      Kubeseer reader/queue ports, and process-local freshness leases keyed by
      namespace/name, UID, generation, and policy epoch. Implement the custom
      Kubeseer predicate/handler so create, generation, deletion-timestamp, and
      delete events update the tracker before enqueue, while status-only updates
      are suppressed and deletion removes owner routing without a finalizer.
    - Implement the non-blocking typed trigger source with one cancellable
      periodic scheduler and coalesced enqueue-all intent. Use the
      controller-runtime work queue for duplicate-key coalescing,
      dirty-during-processing follow-up, same-key exclusion, and per-key rate
      reset after success; retry only transient enqueue-all reads with
      context-selectable capped backoff and no sleeps in reconciliation code.
    - Extend the real external `TestModuleIntegration` path with API types plus
      reconciliation runtime primitives and scripted outbound readers. Prove
      invalid setup fails before source start, existing/create/spec/delete
      lifecycle, status-only suppression, periodic all-key enqueue, event-burst
      coalescing, active-key follow-up, policy fan-out, cancellation, and
      independent keys. Emit
      `MODULE_INTEGRATION=reconciliation-runtime-scheduling STATUS=passed`.
    - Requirements: `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R2.AC8`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R7.AC5`, `R7.AC12`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `NFR2`, `NFR3`, `NFR5`, `NFR7`, `NFR8`, `NFR10`
    - Design: Architecture; Components And Interfaces / Kubeseer lifecycle handler and freshness tracker; Components And Interfaces / Trigger source and periodic scheduler; Data Models; Reconciliation And Event Flows; Error Handling; Testing Strategy / Cross-module integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=reconciliation-runtime-scheduling STATUS=passed"
        timeout: 20m
        covers: ["R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R2.AC8", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R7.AC5", "R7.AC12", "R8.AC1", "R8.AC2", "R8.AC3", "NFR2", "NFR3", "NFR5", "NFR7", "NFR8", "NFR10"]

- [x] 2. Route authorized exact-target events through shared metadata watches
  - [x] 2.1 Add the route registry, metadata adapter, and resilient watch supervisors
    - Add immutable `WatchAddress`, `RouteBinding`, and `AuthorizedRoute`
      contracts. Construct routes only from an exact
      `selection.ReadTarget`/`accesspolicy.Decision` allow pair; retain source ID
      in bindings while sharing one transport by GVR, discovered scope, and
      namespace. Maintain synchronized owner and target indexes, per-owner
      reference counts, lease-guarded atomic replacement, and immediate
      cancellation when the last binding disappears.
    - Add a narrow watch port and production `metadata.Interface` adapter that
      issues no resource LIST, addresses cluster or exact namespace scope from
      discovery, applies no selector, and retains only metadata resourceVersion.
      Route Added, Modified, and Deleted to each distinct current owner key;
      ignore bookmarks except for resumption; drop event objects immediately.
      Restart start errors, stream errors, closes, and expired resource versions
      with cancellable capped backoff while periodic safety remains active.
    - Extend `TestModuleIntegration` through real selection targets and policy
      decisions with a scripted metadata watcher. Prove deny-before-WATCH,
      exact address dimensions, shared transport/deduplicated keys, selector
      enter/leave conservatism, current-generation replacement, deletion
      teardown, no retained content/value/path sentinel, and restart behavior.
      Emit
      `MODULE_INTEGRATION=reconciliation-runtime-watch-routing STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC9`, `R2.AC10`, `R2.AC11`, `R7.AC3`, `R7.AC7`, `R7.AC8`, `R8.AC4`, `R8.AC5`, `NFR1`, `NFR2`, `NFR5`, `NFR6`, `NFR8`, `NFR10`
    - Design: Architecture; Components And Interfaces / Authorized route registry and watch supervisors; Data Models; Reconciliation And Event Flows / Source target event; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy / Cross-module integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=reconciliation-runtime-watch-routing STATUS=passed"
        timeout: 20m
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC9", "R2.AC10", "R2.AC11", "R7.AC3", "R7.AC7", "R7.AC8", "R8.AC4", "R8.AC5", "NFR1", "NFR2", "NFR5", "NFR6", "NFR8", "NFR10"]

- [x] 3. Compose fresh authorization and ordered partial-result execution
  - [x] 3.1 Implement the reconciliation pipeline and retry classifier
    - Add the production reconciler over narrow current-object reader,
      policy-source, route, planner, executor, and status-publisher ports. GET the
      latest Kubeseer directly, no-op on NotFound/deletion, begin a freshness
      lease, load one new fail-closed snapshot, plan every source in declaration
      order, evaluate every exact target, bind complete decisions, and replace
      the owner route set before any selection LIST.
    - Execute only bound plans and preserve one source slot for every
      declaration. Map discovery, planning, authorization, and read failures to
      stable sanitized selection outcomes; compose the real selection,
      `extraction.ExtractBatch`, `typedoutput.ConvertBatch`, and
      `typedoutput.BuildResult` boundaries. Preserve successful siblings,
      field/resource-local conversion results, all-failed outcomes, and the
      present empty result; discard source temporaries and all old values from
      denied/removed targets. Withhold candidates on incomplete cancellation.
    - Classify discovery/policy/read/status availability failures as transient
      and deterministic invalid, denied, unsupported, or Forbidden outcomes as
      event/periodic-only. Permit sanitized partial publication before returning
      one retryable error, propagate context to every boundary, and keep all
      result state invocation-local.
    - Extend `TestModuleIntegration` through all real approved modules. Record
      boundary order and prove no LIST before allow, mixed/all-failed/empty
      results, declaration order, policy fail-closed invalidation, selector and
      source removal, retry classification, sibling/instance isolation,
      cancellation withholding, deterministic convergence, and diagnostics
      without contents, values, selectors, or field paths. Emit
      `MODULE_INTEGRATION=reconciliation-runtime-pipeline STATUS=passed`.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R5.AC12`, `R5.AC13`, `R7.AC1`, `R7.AC2`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R7.AC12`, `R8.AC3`, `R8.AC5`, `R8.AC6`, `R8.AC8`, `NFR1`, `NFR2`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR9`, `NFR10`
    - Design: Architecture; Components And Interfaces / Reconciliation pipeline orchestrator; Components And Interfaces / Retry classifier; Reconciliation And Event Flows / Reconcile one key; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy / Cross-module integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=reconciliation-runtime-pipeline STATUS=passed"
        timeout: 20m
        covers: ["R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R5.AC12", "R5.AC13", "R7.AC1", "R7.AC2", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "R7.AC12", "R8.AC3", "R8.AC5", "R8.AC6", "R8.AC8", "NFR1", "NFR2", "NFR4", "NFR5", "NFR6", "NFR7", "NFR9", "NFR10"]

- [x] 4. Publish only current semantic status
  - [x] 4.1 Add recursive normalization and guarded status-subresource publication
    - Implement a pure semantic projection that normalizes nil versus empty
      result collections without sorting or collapsing result-pointer presence.
      Compare observedGeneration plus every source/error state, provenance,
      field/type/state, match cardinality, and typed payload while treating
      API-normalized collection representation as equivalent.
    - Implement the status publisher with a direct pre-publication GET, lease,
      context, UID, generation, deletion, and policy-epoch guards. Copy the
      latest object, set observedGeneration/result, preserve current conditions
      and spec, suppress equal writes, and issue at most one
      `client.Status().Update` with the latest resourceVersion. Return conflict
      and transient API failures for rate-limited retry without overwriting newer
      state.
    - Extend `TestModuleIntegration` with a scripted direct reader and counting
      status writer. Prove nil/empty equivalence, every meaningful difference,
      generation-only publication, source-event no-op, condition/spec
      preservation, one-attempt maximum, conflict classification, UID
      replacement, deletion, stale generation/policy lease, and canceled
      no-write. Emit
      `MODULE_INTEGRATION=reconciliation-runtime-status STATUS=passed`.
    - Requirements: `R4.AC10`, `R4.AC11`, `R4.AC12`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R6.AC13`, `R6.AC14`, `R7.AC4`, `R8.AC7`, `NFR3`, `NFR5`, `NFR7`, `NFR9`, `NFR10`
    - Design: Components And Interfaces / Semantic status publisher; Semantic Status Normalization; Reconciliation And Event Flows; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy / Cross-module integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=reconciliation-runtime-status STATUS=passed"
        timeout: 20m
        covers: ["R4.AC10", "R4.AC11", "R4.AC12", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R6.AC11", "R6.AC12", "R6.AC13", "R6.AC14", "R7.AC4", "R8.AC7", "NFR3", "NFR5", "NFR7", "NFR9", "NFR10"]

- [x] 5. Prove the real manager, API server, watch, queue, and status behavior
  - [x] 5.1 Wire SetupWithManager and add lifecycle/scheduling envtest scenarios
    - Implement `SetupWithManager` with direct APIReader-backed Kubeseer and
      policy ports, manager client status writer, existing discovery resolver,
      dynamic selection client, metadata watcher, typed lifecycle/policy
      sources, custom trigger source, and one low-level controller. Validate
      setup before registration, do not override manager worker/leader settings,
      and add no public field, finalizer, RBAC, deployment, or entrypoint.
    - Create `TestEnvtestReconciliationRuntime` under `test/envtest/` using the
      shared pinned harness and real manager. Add it to `make test-api` as a
      required named suite. Prove manager startup/cache sync, existing and new
      Kubeseer enqueue, generation and deletion handling, status-only
      suppression, policy singleton fan-out, periodic all-instance safety,
      duplicate coalescing, active-work follow-up, same-key exclusion, and
      independent-key progress with bounded barriers rather than sleeps. Emit
      `API_CONTRACT=reconciliation-runtime-lifecycle STATUS=passed`.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R2.AC8`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R7.AC5`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `NFR2`, `NFR3`, `NFR5`, `NFR7`, `NFR8`, `NFR10`
    - Design: Components And Interfaces / Runtime setup and controller registration; Components And Interfaces / Kubeseer lifecycle handler and freshness tracker; Components And Interfaces / Trigger source and periodic scheduler; Reconciliation And Event Flows; Testing Strategy / Envtest manager and API behavior; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test-api"]
        expect_output: "API_CONTRACT=reconciliation-runtime-lifecycle STATUS=passed"
        timeout: 30m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R2.AC8", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R7.AC5", "R8.AC1", "R8.AC2", "R8.AC3", "NFR2", "NFR3", "NFR5", "NFR7", "NFR8", "NFR10"]

  - [x] 5.2 Exercise authorized source watches and fail-closed invalidation in envtest
    - Extend the envtest suite with a disposable namespaced observed-resource CRD
      and real discovery, dynamic LIST, and metadata WATCH clients. Use request
      recording/readiness barriers to prove an exact allowed route starts before
      LIST, multiple sources share one transport, create/update/delete and
      selector enter/leave events enqueue distinct owners, obsolete generation
      routes are replaced, and source/deletion cleanup stops watches.
    - Prove missing, invalid, unavailable, narrowed, and broadened policy
      transitions through the real singleton path. Assert denied targets issue no
      WATCH or LIST, stale values disappear, fresh reconciliation is required
      before broadened values appear, a forced watch close backs off/restarts,
      periodic safety converges during the gap, and routing/diagnostics retain no
      fixture body, extracted value, or field path. Emit
      `API_CONTRACT=reconciliation-runtime-watch-routing STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R2.AC11`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R7.AC3`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R8.AC4`, `R8.AC5`, `R8.AC8`, `NFR1`, `NFR2`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `NFR10`
    - Design: Components And Interfaces / Authorized route registry and watch supervisors; Components And Interfaces / Reconciliation pipeline orchestrator; Reconciliation And Event Flows / Source target event; Reconciliation And Event Flows / Policy event; Security Considerations; Testing Strategy / Envtest manager and API behavior; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test-api"]
        expect_output: "API_CONTRACT=reconciliation-runtime-watch-routing STATUS=passed"
        timeout: 30m
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R2.AC11", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R7.AC3", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "R8.AC4", "R8.AC5", "R8.AC8", "NFR1", "NFR2", "NFR5", "NFR6", "NFR7", "NFR8", "NFR10"]

  - [x] 5.3 Exercise status publication, retries, conflicts, and cancellation in envtest
    - Extend the real-manager suite with successful, mixed partial, all-failed,
      and zero-source objects. Assert complete ordered typed status,
      observedGeneration, condition/spec preservation, nil/empty semantic
      suppression, no write for unchanged source events, generation-only write,
      and invalidation after source/resource/policy changes.
    - Use narrow deterministic test adapters around real clients to inject
      discovery/LIST availability failures, controller cancellation, a watch
      close, and a concurrent real API write immediately before delegated status
      update. Prove transient per-key rate-limited recovery, deterministic
      failure no-hot-loop behavior, real resourceVersion conflict without
      overwrite, one attempt per reconciliation, stale/canceled no-write, one
      busy key not blocking another, and final deterministic convergence. Emit
      `API_CONTRACT=reconciliation-runtime-status STATUS=passed`.
    - Requirements: `R4.AC9`, `R4.AC10`, `R4.AC11`, `R4.AC12`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R5.AC12`, `R5.AC13`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R6.AC13`, `R6.AC14`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR9`, `NFR10`
    - Design: Components And Interfaces / Retry classifier; Components And Interfaces / Semantic status publisher; Semantic Status Normalization; Error Handling; Failure Modes And Tradeoffs; Testing Strategy / Envtest manager and API behavior; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test-api"]
        expect_output: "API_CONTRACT=reconciliation-runtime-status STATUS=passed"
        timeout: 30m
        covers: ["R4.AC9", "R4.AC10", "R4.AC11", "R4.AC12", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R5.AC12", "R5.AC13", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R6.AC11", "R6.AC12", "R6.AC13", "R6.AC14", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC6", "R8.AC7", "R8.AC8", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "NFR9", "NFR10"]

- [x] 6. Seal the higher-layer verification matrix
  - [x] 6.1 Verify formatting, race safety, API compatibility, build, and module tidiness
    - Keep all runtime code under `internal/reconciliation` and all automated
      proof in `test/integration` or `test/envtest`. Update canonical Make
      targets without adding package-local unit tests, API/generated drift,
      ambient kubeconfig fallback, public scheduling fields, controller
      manifests, RBAC, or packaging.
    - Run repository verification, race-enabled default integration, the complete
      envtest compatibility matrix, compilation, and read-only module tidiness
      with task-scoped writable caches. Confirm every named marker is non-vacuous
      and the complete runtime remains valid on Kubernetes 1.35.6 and 1.36.2.
    - Requirements: `R1.AC1`, `R2.AC2`, `R3.AC3`, `R4.AC4`, `R5.AC2`, `R6.AC5`, `R7.AC3`, `R8.AC4`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `NFR9`, `NFR10`
    - Design: Simplicity And Elegance Review; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 20m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test GO_TEST_FLAGS=-race"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        timeout: 30m
        covers: ["R3.AC3", "R4.AC4", "R5.AC2", "R6.AC5", "R8.AC4", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR8", "NFR9"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 45m
        covers: ["R1.AC1", "R2.AC2", "R7.AC3", "NFR7", "NFR10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod go build ./..."]
        timeout: 10m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-reconciliation-runtime-go-build GOMODCACHE=/tmp/kubeseer-reconciliation-runtime-go-mod go mod tidy -diff"]
        timeout: 10m
