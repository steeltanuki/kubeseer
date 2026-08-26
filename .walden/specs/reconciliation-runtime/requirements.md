---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T19:03:02Z
last_modified: 2026-08-26T19:03:02Z
approved_fingerprint: sha256:7894cd920ebd3bde99f6af6ce91d8d9ab00cd8adc61fe3b8309c937677860700
---

# Requirements Document

## Introduction

This feature turns the approved discovery, installation-policy, selection,
field-extraction, and typed-output capabilities into an operational
controller-runtime reconciliation loop for namespaced `Kubeseer` resources.
It defines the events that enqueue work, the authorized source-watch boundary,
periodic safety reconciliation, coalescing, retry behavior, orchestration of
the existing processing stages, stale-generation protection, deletion
behavior, and semantic status-write suppression.

The runtime owns coordination rather than the domain semantics of the modules
it invokes. Resource discovery remains authoritative for GVR and scope;
installation-policy evaluation remains the logical access ceiling; selection
remains the only resource-instance LIST boundary; extraction and typed output
retain their approved ordering, isolation, and confidentiality contracts.
Detailed public conditions and reason taxonomy belong to
`status-and-conditions`, while worker budgets, cardinality limits, and
throughput tuning belong to `performance-and-limits`.

<!-- assumed: reconciliation is registered with a controller-runtime manager and uses a namespaced-name work-queue key because controller-runtime is the approved operator framework (source: .walden/constitution.md Technology And Compatibility Baseline) -->
<!-- assumed: source events are routed from a conservative exact-target watch and selectors are re-evaluated during reconciliation, so updates that enter or leave a selector cannot be missed (source: SPECIFICATIONS.md source-watch and affected-instance mapping scope) -->
<!-- assumed: the periodic safety interval is internal, positive, and constructor-configurable rather than a new v1alpha1 field; public scheduling configuration is deferred (source: approved API compatibility rules and performance-and-limits boundary) -->
<!-- assumed: no finalizer is added because the current vertical slice owns no external resource that requires cleanup before Kubeseer deletion (source: implemented module boundaries and SPECIFICATIONS.md finalizers-only-where-necessary rule) -->
<!-- assumed: source-watch routing state retains only target and Kubeseer identities and does not persist observed resource contents or extracted values (source: .walden/constitution.md confidentiality rules) -->
<!-- assumed: a reconciliation with mixed successful and failed sources publishes a partial result containing both successful outcomes and sanitized failures; status-and-conditions will represent the public degraded state (source: user choice of Option A during requirements drafting) -->

## Requirements

### R1 Controller Registration And Kubeseer Lifecycle Events

**User Story:** As a platform operator, I want every Kubeseer instance to enter a managed reconciliation loop, so that creation, configuration changes, restart recovery, and deletion are handled predictably.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL expose a controller-runtime registration boundary for namespaced `Kubeseer` resources.
2. `R1.AC2` WHEN the controller observes an existing or newly created Kubeseer resource, the system SHALL enqueue its namespace and name for reconciliation.
3. `R1.AC3` WHEN a Kubeseer update changes `metadata.generation`, the system SHALL enqueue its namespace and name for reconciliation.
4. `R1.AC4` WHEN a Kubeseer update changes only status while `metadata.generation` remains unchanged, the system SHALL suppress an enqueue caused by that owned-resource update.
5. `R1.AC5` WHEN a Kubeseer resource receives a deletion timestamp, the system SHALL prevent new source-resource reads for that instance.
6. `R1.AC6` WHEN a Kubeseer resource is deleted, the system SHALL remove its source-event routing entries.
7. `R1.AC7` WHEN the controller restarts and its Kubeseer cache synchronizes, the system SHALL enqueue every existing Kubeseer instance without requiring a spec mutation.
8. `R1.AC8` The system SHALL reconcile the current vertical slice without adding a finalizer to Kubeseer resources.

### R2 Authorized Source Watch Routing

**User Story:** As a Kubeseer user, I want changes to an observed resource target to enqueue every potentially affected Kubeseer instance, so that status converges without waiting only for periodic polling.

#### Acceptance Criteria

1. `R2.AC1` WHEN a source read target has a current successful installation-policy decision, the system SHALL establish or retain event observation for that exact GVR, discovered scope, and namespace.
2. `R2.AC2` IF a source read target lacks a current successful installation-policy decision, THEN the system SHALL issue no WATCH request for that target.
3. `R2.AC3` WHEN a create event is received from an authorized source target, the system SHALL enqueue every Kubeseer instance whose current source routing may be affected by that target.
4. `R2.AC4` WHEN an update event is received from an authorized source target, the system SHALL enqueue every Kubeseer instance whose current source routing may be affected by either the old or new object state.
5. `R2.AC5` WHEN a delete event is received from an authorized source target, the system SHALL enqueue every Kubeseer instance whose current source routing may be affected by that target.
6. `R2.AC6` WHEN a Kubeseer source changes its resource identity or namespace scope, the system SHALL replace that instance's obsolete routing entries with entries derived from the current generation.
7. `R2.AC7` WHEN multiple Kubeseer sources share one exact authorized target, the system SHALL route one target event to each distinct affected Kubeseer key at most once per pending work-queue cycle.
8. `R2.AC8` WHEN the active installation access policy changes, the system SHALL enqueue every existing Kubeseer instance for fresh policy evaluation.
9. `R2.AC9` WHEN a fresh installation-policy decision removes authorization for a watched target, the system SHALL remove that target from active source-event routing.
10. `R2.AC10` IF a source watch cannot be established or terminates unexpectedly, THEN the system SHALL preserve periodic safety reconciliation for the affected Kubeseer instances.
11. `R2.AC11` WHEN a source event is routed, the system SHALL avoid retaining observed resource contents or extracted values in routing state.

### R3 Periodic Safety Scheduling And Event Coalescing

**User Story:** As a platform operator, I want bounded periodic reconciliation and event coalescing, so that missed watches recover without creating hot loops during event bursts.

#### Acceptance Criteria

1. `R3.AC1` The system SHALL require a positive periodic safety interval before the reconciliation runtime starts.
2. `R3.AC2` WHEN the configured safety interval elapses, the system SHALL enqueue every existing Kubeseer instance.
3. `R3.AC3` WHEN more than one pending trigger addresses the same Kubeseer namespace and name, the system SHALL coalesce those triggers into one pending work-queue key.
4. `R3.AC4` WHEN a new trigger arrives while its Kubeseer key is being reconciled, the system SHALL preserve a follow-up reconciliation for that key.
5. `R3.AC5` IF the configured safety interval is zero or negative, THEN the system SHALL fail runtime setup before starting source watches.
6. `R3.AC6` IF a deterministic source configuration failure remains unchanged, THEN the system SHALL avoid an immediate unbounded retry loop for that failure.
7. `R3.AC7` IF a transient runtime dependency fails, THEN the system SHALL request a rate-limited retry for the affected Kubeseer key.
8. `R3.AC8` WHEN equivalent event bursts occur for one Kubeseer key, the system SHALL preserve equivalent eventual reconciliation behavior regardless of event arrival order.

### R4 Fresh And Ordered Reconciliation Execution

**User Story:** As a Kubeseer user, I want each reconciliation to evaluate my current generation through the approved processing boundaries, so that no stale configuration or unauthorized target is published.

#### Acceptance Criteria

1. `R4.AC1` WHEN a Kubeseer key is dequeued, the system SHALL read the latest persisted Kubeseer resource before evaluating any source.
2. `R4.AC2` IF the dequeued Kubeseer resource no longer exists, THEN the system SHALL complete that reconciliation without a source-resource read.
3. `R4.AC3` WHEN reconciliation begins for a live Kubeseer resource, the system SHALL load a fresh fail-closed installation-policy snapshot before authorizing any source target.
4. `R4.AC4` WHEN the current Kubeseer generation is evaluated, the system SHALL compose discovery, exact-target authorization, selection, extraction, typed conversion, and structural result construction in that order.
5. `R4.AC5` IF an exact source target is not authorized by the fresh policy snapshot, THEN the system SHALL perform no resource-instance LIST for that target.
6. `R4.AC6` WHEN multiple sources are evaluated, the system SHALL preserve one outcome for every declared source in declaration order.
7. `R4.AC7` WHEN reconciliation invokes a Kubernetes or processing boundary, the system SHALL propagate the reconciliation context to that boundary.
8. `R4.AC8` IF the reconciliation context is canceled or reaches its deadline, THEN the system SHALL stop starting new source-resource reads.
9. `R4.AC9` WHEN one Kubeseer reconciliation fails, the system SHALL preserve independent reconciliation progress for other Kubeseer instances.
10. `R4.AC10` IF the Kubeseer generation changes before a candidate status is published, THEN the system SHALL discard the candidate status from the older generation.
11. `R4.AC11` IF a status write conflicts with a newer persisted resource version, THEN the system SHALL avoid overwriting the newer status.
12. `R4.AC12` IF a status write conflicts with a newer persisted resource version, THEN the system SHALL request a rate-limited reconciliation retry.

### R5 Partial And Fail-Closed Result Policy

**User Story:** As a Kubeseer user, I want successful sources to remain visible when independent sources fail, so that one failure does not hide useful current data or retain unauthorized stale data.

#### Acceptance Criteria

1. `R5.AC1` WHEN every declared source completes successfully, the system SHALL construct a candidate result containing one successful outcome for every source.
2. `R5.AC2` WHEN one or more sources fail while other sources succeed, the system SHALL include every successful source outcome in the candidate result.
3. `R5.AC3` WHEN one or more sources fail while other sources succeed, the system SHALL include one sanitized unsuccessful outcome for every failed source in the candidate result.
4. `R5.AC4` WHEN every declared source fails, the system SHALL construct a candidate result containing one unsuccessful outcome for every source.
5. `R5.AC5` IF a source execution fails after producing temporary values, THEN the system SHALL omit those temporary values from that source's candidate outcome.
6. `R5.AC6` WHEN a resource outcome contains a field-local typed-conversion failure, the system SHALL preserve independent successful fields in that resource outcome.
7. `R5.AC7` WHEN a source outcome contains a resource with field-local typed-conversion failures, the system SHALL preserve independent successful resources in that source outcome.
8. `R5.AC8` WHEN a source failure is included in a candidate result, the system SHALL preserve its stable upstream or runtime reason.
9. `R5.AC9` WHEN a source failure is included in a candidate result, the system SHALL omit resource contents, extracted values, and field paths from its diagnostic.
10. `R5.AC10` IF the active installation policy is missing, invalid, or unavailable, THEN the system SHALL construct no successful observed-resource value for the affected reconciliation.
11. `R5.AC11` IF a fresh installation-policy decision denies a previously successful source target, THEN the system SHALL omit every previously published value from that target in the new candidate result.
12. `R5.AC12` WHEN a Kubeseer resource declares no sources, the system SHALL construct a successful empty candidate result.
13. `R5.AC13` IF controller cancellation occurs before all declared sources reach terminal outcomes, THEN the system SHALL withhold a candidate status for that interrupted reconciliation.

### R6 Semantic Status Publication

**User Story:** As a Kubernetes API consumer, I want status writes only for meaningful changes, so that results remain current without reconciliation loops or resource-version churn.

#### Acceptance Criteria

1. `R6.AC1` WHEN a candidate status is publishable for the current Kubeseer generation, the system SHALL set `status.observedGeneration` to that generation.
2. `R6.AC2` WHEN a candidate status is publishable, the system SHALL place its structural typed result in `status.result`.
3. `R6.AC3` WHEN this feature supplies no condition candidate, the system SHALL preserve the persisted `status.conditions` collection unchanged.
4. `R6.AC4` WHEN a candidate status is ready, the system SHALL compare its normalized semantic projection with the persisted status before issuing a write.
5. `R6.AC5` WHEN the candidate and persisted semantic projections are equivalent, the system SHALL issue no status write.
6. `R6.AC6` WHEN `metadata.generation` changes while the candidate result remains equivalent, the system SHALL publish the new `observedGeneration`.
7. `R6.AC7` WHEN source state, resource provenance, field state, field type, match cardinality, typed payload, or sanitized failure content changes, the system SHALL treat the candidate result as semantically different.
8. `R6.AC8` WHEN candidate and persisted results differ only by API-normalized omitted-versus-empty collection representation, the system SHALL treat those results as semantically equivalent.
9. `R6.AC9` WHEN a source-resource event produces no semantic result change, the system SHALL issue no status write.
10. `R6.AC10` WHEN a candidate semantic projection differs from persisted status, the system SHALL attempt one status publication for that reconciliation.
11. `R6.AC11` WHEN status is published, the system SHALL use the Kubeseer status subresource.
12. `R6.AC12` WHEN status is published, the system SHALL preserve the persisted Kubeseer spec.
13. `R6.AC13` IF the Kubeseer generation becomes stale before publication, THEN the system SHALL issue no write for the stale candidate.
14. `R6.AC14` IF the reconciliation context is canceled before publication, THEN the system SHALL issue no status write for that reconciliation.

### R7 Retry, Recovery, And Result Invalidation

**User Story:** As a platform operator, I want transient failures retried with backoff and stale results invalidated by relevant changes, so that the controller converges without hot loops.

#### Acceptance Criteria

1. `R7.AC1` IF discovery is temporarily unavailable for a source, THEN the system SHALL request a rate-limited retry for the affected Kubeseer key.
2. `R7.AC2` IF an authorized source LIST fails with a transient availability or continuation error, THEN the system SHALL request a rate-limited retry for the affected Kubeseer key.
3. `R7.AC3` IF an authorized source watch fails to start or terminates unexpectedly, THEN the system SHALL retry watch establishment with backoff.
4. `R7.AC4` IF a status write fails because of a conflict or transient API error, THEN the system SHALL request a rate-limited retry for the affected Kubeseer key.
5. `R7.AC5` WHEN a reconciliation completes without a retryable failure, the system SHALL clear accumulated rate-limit state for that Kubeseer key.
6. `R7.AC6` IF a source has a deterministic invalid configuration, THEN the system SHALL rely on configuration events or periodic safety reconciliation rather than an immediate retry loop.
7. `R7.AC7` WHEN an observed resource is deleted, the system SHALL remove that resource's values from the next successfully published result.
8. `R7.AC8` WHEN an observed resource no longer satisfies a source selector, the system SHALL remove that resource's values from the next successfully published result.
9. `R7.AC9` WHEN a source is removed from the current Kubeseer generation, the system SHALL remove that source outcome from the next successfully published result.
10. `R7.AC10` WHEN an installation-policy change narrows an authorized target, the system SHALL publish a fresh fail-closed result without values from the newly denied target.
11. `R7.AC11` WHEN an installation-policy change broadens an allowed target, the system SHALL require a fresh reconciliation before publishing values from that target.
12. `R7.AC12` The system SHALL avoid arbitrary blocking sleeps in reconciliation code.

### R8 Concurrency And Instance Isolation

**User Story:** As a platform operator, I want concurrent events and reconciliations contained by Kubeseer identity, so that one busy or failing instance cannot corrupt or block another.

#### Acceptance Criteria

1. `R8.AC1` WHILE a Kubeseer namespace-and-name key is being reconciled, the system SHALL prevent a second active execution for that same key.
2. `R8.AC2` WHEN different Kubeseer keys are ready, the system SHALL permit their scheduling to progress independently.
3. `R8.AC3` WHEN one Kubeseer reconciliation returns a retryable failure, the system SHALL preserve pending work for every other Kubeseer key.
4. `R8.AC4` WHEN source routing is read or updated concurrently, the system SHALL preserve a race-free mapping between exact targets and Kubeseer keys.
5. `R8.AC5` WHEN a policy change races with a source event, the system SHALL re-evaluate authorization from a fresh policy snapshot before the next resource-instance read.
6. `R8.AC6` WHEN candidate results are assembled concurrently for different Kubeseer instances, the system SHALL keep their mutable processing state isolated.
7. `R8.AC7` IF a stale reconciliation attempts status publication after a newer generation has been observed, THEN the system SHALL reject the stale publication path.
8. `R8.AC8` WHEN equivalent concurrent event sequences settle, the system SHALL converge to the same semantic status for the same final Kubernetes state.

## Non-Functional Requirements

- `NFR1` **Security:** WATCH and LIST operations SHALL remain inside the fresh installation-policy ceiling for the exact discovered target; `R2.AC1`, `R2.AC2`, `R2.AC8`, `R2.AC9`, `R4.AC3`, `R4.AC5`, `R5.AC10`, `R5.AC11`, `R7.AC10`, `R7.AC11`, and `R8.AC5` provide behavioral coverage.
- `NFR2` **Eventual consistency:** Owned-resource events, authorized source events, policy events, retries, restart recovery, and periodic safety scheduling SHALL converge every existing Kubeseer toward current cluster state; `R1.AC2`, `R1.AC3`, `R1.AC7`, `R2.AC3` through `R2.AC10`, `R3.AC2` through `R3.AC8`, and `R7.AC1` through `R7.AC11` provide behavioral coverage.
- `NFR3` **Idempotency:** Equivalent current inputs SHALL produce equivalent candidate status and no repeated status write; `R3.AC3`, `R3.AC8`, `R6.AC4` through `R6.AC10`, and `R8.AC8` provide behavioral coverage.
- `NFR4` **Failure isolation:** Source failures SHALL produce the approved partial-result policy without hiding successful siblings, while one Kubeseer failure SHALL not block another; `R4.AC6`, `R4.AC9`, `R5.AC1` through `R5.AC9`, and `R8.AC2`, `R8.AC3`, `R8.AC6` provide behavioral coverage.
- `NFR5` **Concurrency safety:** Work-queue, routing, generation, status-conflict, and policy-event races SHALL not publish stale or cross-instance state; `R3.AC3`, `R3.AC4`, `R4.AC10` through `R4.AC12`, `R6.AC13`, and `R8.AC1` through `R8.AC8` provide behavioral coverage.
- `NFR6` **Confidentiality:** Routing, diagnostics, and retry state SHALL omit observed resource contents, extracted values, and field paths; `R2.AC11`, `R5.AC8`, and `R5.AC9` provide behavioral coverage.
- `NFR7` **Kubernetes conformance:** The runtime SHALL use controller-runtime manager, work-queue, context, status-subresource, optimistic-concurrency, and API-error semantics; `R1.AC1` through `R1.AC7`, `R3.AC3` through `R3.AC7`, `R4.AC1`, `R4.AC2`, `R4.AC7`, `R4.AC8`, `R4.AC11`, `R4.AC12`, and `R6.AC10` through `R6.AC14` provide behavioral coverage.
- `NFR8` **Efficiency:** Event routing SHALL share equivalent exact-target observation and coalesce duplicate Kubeseer keys without weakening periodic recovery; `R2.AC1`, `R2.AC7`, `R2.AC10`, and `R3.AC2` through `R3.AC4` provide behavioral coverage.
- `NFR9` **API compatibility:** Reconciliation SHALL use the existing additive `kubeseer.io/v1alpha1` spec and status envelopes without introducing a second version or redefining typed-result semantics; `R4.AC4`, `R5`, and `R6` provide behavioral coverage.
- `NFR10` **Testability:** Runtime orchestration SHALL be verifiable through the genuine cross-module pipeline and through envtest with a real manager, API server, source watches, status subresource, retries, conflicts, and cancellation; `R1.AC1` through `R1.AC7`, `R2`, `R3`, `R4`, `R6`, `R7`, and `R8` provide observable boundary coverage.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `kubeseer-api-foundation`, `integration-testing-foundation`, `resource-discovery`, `installation-access-policy`, `resource-selection`, `field-extraction`, and `typed-output-model` contracts.
- `C2` The public API remains `kubeseer.io/v1alpha1`; this feature must not add public scheduling, retry, watch, or concurrency configuration fields.
- `C3` Runtime composition must reuse the implemented discovery, access-policy, selection, extraction, and typed-output boundaries rather than duplicate their domain logic.
- `C4` Resource scope and GVR come only from fresh Kubernetes discovery; source-watch or LIST targets must not infer scope from Kind naming.
- `C5` A logical policy allow decision does not grant Kubernetes RBAC; WATCH, LIST, and status operations remain subject to the operator ServiceAccount permissions.
- `C6` Source WATCH authorization and selection LIST authorization use the same exact target identity dimensions: source identifier, API group, Kind, discovered scope, and namespace.
- `C7` The source-event routing boundary may consume object identity metadata but must not become a second field-extraction or result cache.
- `C8` Status publication uses the existing status subresource and optimistic resource-version semantics; whole-object updates are not an allowed substitute.
- `C9` Detailed condition types, transition timestamps, reason codes, human-readable condition messages, and semantic-result hashes belong to `status-and-conditions`.
- `C10` Worker counts, queue-depth budgets, watch-count limits, source cardinality, status-size limits, and performance objectives belong to `performance-and-limits`.
- `C11` Controller deployment manifests, production RBAC, leader election configuration, container entrypoints, and installation lifecycle belong to `packaging-and-installation`.
- `C12` Automated proof follows the repository testing constitution: genuine cross-module integration is the minimum orchestration layer, envtest proves manager and Kubernetes API behavior, and no dedicated package-local unit-test layer is maintained.

## Out Of Scope

- New public `Kubeseer` spec or status fields, a second API version, or changes to the approved typed-result representation.
- Public condition taxonomy, condition transition rules, timestamps, semantic-result hashes, or ready/degraded/invalid/unauthorized condition rendering.
- New discovery, policy-evaluation, selection, JSONPath, typed-conversion, value-operator, grouping, or aggregation semantics.
- User impersonation, SubjectAccessReview, admission authorization, ServiceAccount RBAC creation, or the complete `authorization-enforcement` audit contract.
- Product-wide worker concurrency, rate limits, cardinality limits, queue limits, watch limits, memory budgets, evaluation deadlines, or status-size enforcement.
- Creating, updating, patching, or deleting observed source resources.
- Owned child resources, external cleanup obligations, or Kubeseer finalizers.
- Durable result history, Kubernetes Events, metrics, tracing, dashboards, or alerting.
- Controller deployment, leader-election configuration, release packaging, installation, upgrade, or uninstall behavior.
- Full end-to-end clusters, local kind-on-Podman lifecycle, examples, or user-facing tutorials.
- A globally atomic snapshot across independent Kubernetes resource endpoints; reconciliation provides deterministic eventual convergence instead.
