---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-27T07:28:04Z
last_modified: 2026-08-27T07:28:04Z
approved_fingerprint: sha256:894c7c2d96e506eb1ac95e83ff165ca8aa36242faa82b36d5a47a91d066876bf
---

# Requirements Document

## Introduction

Kubeseer already publishes a current-generation structural typed result, but
API consumers do not yet have a controller-owned condition contract that
explains whether that result is ready, partial, invalid, unauthorized, or
temporarily unavailable. This feature defines that public status contract for
the existing `kubeseer.io/v1alpha1` resource.

The feature owns the canonical Kubernetes-style conditions, their transition
rules and public reason taxonomy, a deterministic processed-result summary,
and a semantic result hash. It composes with the existing reconciliation
publisher: every current-generation status snapshot contains
`observedGeneration` and conditions; when a structural result exists, the same
snapshot also contains its summary and result hash. Existing source- and
field-scoped result errors remain the detailed partial-error contract.

<!-- assumed: the canonical public conditions are Accepted, Authorized, SourcesResolved, Ready, and Degraded because SPECIFICATIONS.md names that set and the existing API already exposes metav1.Condition -->
<!-- assumed: the overall state is represented by the canonical conditions and their reasons rather than by a second status.state field, avoiding two public sources of truth -->
<!-- assumed: status summary and resultHash are additive v1alpha1 fields because SPECIFICATIONS.md includes both and reconciliation-runtime explicitly assigns semantic-result hashes to this feature -->
<!-- assumed: timestamps are condition lastTransitionTime values only; a last-evaluated timestamp is excluded because changing it on every reconciliation would violate semantic no-op status suppression -->
<!-- assumed: source- and field-scoped errors remain inside status.result rather than being duplicated in a top-level error list because the approved typed-output contract already preserves partial failures there -->
<!-- assumed: a terminal evaluation of a Kubeseer with zero sources is ready with a present empty result because zero sources is a valid declaration and reconciliation-runtime already publishes that structural outcome -->

## Requirements

### R1 Atomic Current-Generation Status Snapshot

**User Story:** As a Kubernetes API consumer, I want one coherent status
snapshot for the current Kubeseer generation, so that I never have to combine
fields produced by different evaluations.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL expose the public status contract defined by this specification through the `KubeseerStatus` envelope.
2. `R1.AC2` WHEN a terminal status snapshot is publishable for the current Kubeseer generation, the system SHALL set `status.observedGeneration` to that generation.
3. `R1.AC3` WHEN a terminal evaluation produces a structural result, the system SHALL place that result in `status.result`.
4. `R1.AC4` WHEN a terminal evaluation produces a structural result, the system SHALL derive `status.summary` from that result.
5. `R1.AC5` WHEN a terminal evaluation produces a structural result, the system SHALL derive `status.resultHash` from that result.
6. `R1.AC6` WHEN a terminal status snapshot is publishable, the system SHALL derive `status.conditions` from its terminal outcomes.
7. `R1.AC7` WHEN the current-generation status snapshot is published, the system SHALL use one status-subresource write.
8. `R1.AC8` WHEN the current-generation status snapshot is published, the system SHALL preserve the persisted Kubeseer spec.
9. `R1.AC9` IF the Kubeseer UID differs from the identity captured for evaluation, THEN the system SHALL issue no write for that candidate status.
10. `R1.AC10` IF the Kubeseer generation changes before status publication, THEN the system SHALL issue no write for the stale candidate status.
11. `R1.AC11` IF the reconciliation context is canceled before status publication, THEN the system SHALL issue no write for that reconciliation.
12. `R1.AC12` IF the Kubeseer has a deletion timestamp before status publication, THEN the system SHALL issue no status write for that resource.

### R2 Canonical Kubernetes Conditions

**User Story:** As a Kubernetes API consumer, I want a small stable condition
set with native metadata, so that generic clients can interpret Kubeseer state
without parsing free-form text.

#### Acceptance Criteria

1. `R2.AC1` WHEN a terminal evaluation is published, the system SHALL expose exactly one condition of each type `Accepted`, `Authorized`, `SourcesResolved`, `Ready`, and `Degraded`.
2. `R2.AC2` The system SHALL represent every canonical condition with `metav1.Condition` semantics.
3. `R2.AC3` WHEN a canonical condition is published, the system SHALL set its `observedGeneration` to the status snapshot generation.
4. `R2.AC4` WHEN a canonical condition is published, the system SHALL set its status to `True`, `False`, or `Unknown`.
5. `R2.AC5` WHEN a canonical condition is published, the system SHALL set a non-empty stable reason code.
6. `R2.AC6` WHEN a canonical condition is published, the system SHALL set a non-empty human-readable message.
7. `R2.AC7` WHEN canonical conditions are serialized, the system SHALL order them as `Accepted`, `Authorized`, `SourcesResolved`, `Ready`, and `Degraded`.
8. `R2.AC8` WHEN a condition status changes, the system SHALL set `lastTransitionTime` to the time of that status transition.
9. `R2.AC9` WHEN a condition status remains unchanged, the system SHALL preserve its persisted `lastTransitionTime`.
10. `R2.AC10` WHILE no terminal evaluation has ever been published, the system SHALL allow the canonical conditions to be absent.
11. `R2.AC11` WHEN canonical conditions are published, the system SHALL preserve persisted non-canonical conditions unchanged.

### R3 Configuration Acceptance

**User Story:** As a Kubeseer author, I want status to distinguish an accepted
declaration from an invalid one, so that configuration defects are actionable
without being confused with cluster availability failures.

#### Acceptance Criteria

1. `R3.AC1` WHEN every declared source passes deterministic runtime configuration checks, the system SHALL set `Accepted=True` with reason `ConfigurationAccepted`.
2. `R3.AC2` IF any declared source has a deterministic configuration error, THEN the system SHALL set `Accepted=False` with reason `InvalidConfiguration`.
3. `R3.AC3` WHEN a Kubeseer declares zero sources, the system SHALL set `Accepted=True` with reason `ConfigurationAccepted`.
4. `R3.AC4` WHEN multiple configuration errors exist, the system SHALL derive the `Accepted` condition from the first failed source in declaration order.

### R4 Authorization And Source Resolution

**User Story:** As a platform operator, I want authorization and discovery
state reported independently, so that denial, invalid policy, missing resource
types, and transient control-plane failures remain distinguishable.

#### Acceptance Criteria

1. `R4.AC1` WHEN every resolved exact source target has a successful authorization outcome, the system SHALL set `Authorized=True` with reason `AuthorizationSucceeded`.
2. `R4.AC2` IF a valid installation access policy explicitly denies any exact source target, THEN the system SHALL set `Authorized=False` with reason `AuthorizationDenied`.
3. `R4.AC3` IF the installation access policy is missing, THEN the system SHALL set `Authorized=False` with reason `PolicyMissing`.
4. `R4.AC4` IF the installation access policy is invalid, THEN the system SHALL set `Authorized=False` with reason `PolicyInvalid`.
5. `R4.AC5` IF an authorized source read is forbidden by Kubernetes RBAC, THEN the system SHALL set `Authorized=False` with reason `ReadForbidden`.
6. `R4.AC6` IF policy state is temporarily unavailable, THEN the system SHALL set `Authorized=Unknown` with reason `AuthorizationUnavailable`.
7. `R4.AC7` IF source resolution is temporarily unavailable before an authorization decision, THEN the system SHALL set `Authorized=Unknown` with reason `AuthorizationUnavailable`.
8. `R4.AC8` WHEN every declared source resolves to one exact Kubernetes resource type and scope, the system SHALL set `SourcesResolved=True` with reason `ResolutionSucceeded`.
9. `R4.AC9` IF any declared source has a deterministic resource-resolution failure, THEN the system SHALL set `SourcesResolved=False` with reason `ResolutionFailed`.
10. `R4.AC10` IF Kubernetes discovery is temporarily unavailable for any declared source, THEN the system SHALL set `SourcesResolved=Unknown` with reason `ResolutionUnavailable`.
11. `R4.AC11` WHEN a Kubeseer declares zero sources, the system SHALL set `Authorized=True` with reason `AuthorizationSucceeded`.
12. `R4.AC12` WHEN a Kubeseer declares zero sources, the system SHALL set `SourcesResolved=True` with reason `ResolutionSucceeded`.
13. `R4.AC13` WHEN multiple source-scoped authorization failures have different public reasons, the system SHALL derive the `Authorized` condition from the first affected source in declaration order.
14. `R4.AC14` WHEN multiple source-resolution failures have different public reasons, the system SHALL derive the `SourcesResolved` condition from the first affected source in declaration order.
15. `R4.AC15` IF deterministic configuration or resolution failure prevents an authorization decision, THEN the system SHALL set `Authorized=Unknown` with reason `AuthorizationNotEvaluated`.
16. `R4.AC16` IF an earlier fail-closed outcome prevents source resolution from being attempted, THEN the system SHALL set `SourcesResolved=Unknown` with reason `ResolutionNotEvaluated`.

### R5 Ready, Degraded, And Overall Outcome

**User Story:** As a Kubeseer consumer, I want one unambiguous current outcome,
so that automation can distinguish complete success from partial, invalid,
unauthorized, or unavailable evaluation.

#### Acceptance Criteria

1. `R5.AC1` WHEN the current generation produces a terminal result without source-scoped or field-scoped errors, the system SHALL set `Ready=True` with reason `EvaluationSucceeded`.
2. `R5.AC2` WHEN the current generation produces a terminal result without source-scoped or field-scoped errors, the system SHALL set `Degraded=False` with reason `EvaluationSucceeded`.
3. `R5.AC3` WHEN the current generation produces a terminal result containing any source-scoped or field-scoped error, the system SHALL set `Ready=False` with reason `EvaluationDegraded`.
4. `R5.AC4` WHEN the current generation produces a terminal result containing any source-scoped or field-scoped error, the system SHALL set `Degraded=True` with reason `EvaluationDegraded`.
5. `R5.AC5` IF a terminal dependency failure prevents the current generation from producing a result snapshot, THEN the system SHALL set `Ready=False` with reason `EvaluationUnavailable`.
6. `R5.AC6` IF a terminal dependency failure prevents the current generation from producing a result snapshot, THEN the system SHALL set `Degraded=True` with reason `EvaluationUnavailable`.
7. `R5.AC7` WHEN a Kubeseer declares zero sources and reaches a terminal evaluation, the system SHALL publish a present empty `status.result`.
8. `R5.AC8` WHEN a Kubeseer declares zero sources and reaches a terminal evaluation, the system SHALL set `Ready=True` with reason `EvaluationSucceeded`.
9. `R5.AC9` WHEN a Kubeseer declares zero sources and reaches a terminal evaluation, the system SHALL set `Degraded=False` with reason `EvaluationSucceeded`.
10. `R5.AC10` The system SHALL prevent `Ready` and `Degraded` from both having status `True` for the same observed generation.

### R6 Processed-Result Summary

**User Story:** As a human or automated consumer, I want compact result counts,
so that I can understand evaluation scope without traversing the complete typed
result.

#### Acceptance Criteria

1. `R6.AC1` WHEN a terminal result is published, the system SHALL set `status.summary.successfulSources` to the count of source results in state `values`.
2. `R6.AC2` WHEN a terminal result is published, the system SHALL set `status.summary.failedSources` to the count of source results in state `error`.
3. `R6.AC3` WHEN a terminal result is published, the system SHALL set `status.summary.matchedResources` to the count of resource results across all source results.
4. `R6.AC4` The system SHALL expose every summary count as a non-negative integer.
5. `R6.AC5` WHEN a source contains field-scoped errors but remains in state `values`, the system SHALL count that source in `successfulSources`.
6. `R6.AC6` WHEN a terminal result is empty, the system SHALL publish zero for every summary count.
7. `R6.AC7` IF `status.result` is absent, THEN the system SHALL omit `status.summary`.

### R7 Semantic Result Hash

**User Story:** As an API consumer, I want a stable result fingerprint, so that
I can cheaply identify whether the observable result changed while retaining
the full result as the authoritative value.

#### Acceptance Criteria

1. `R7.AC1` WHEN a terminal result is published, the system SHALL set `status.resultHash` to a SHA-256 fingerprint of the normalized semantic result.
2. `R7.AC2` WHEN `status.resultHash` is published, the system SHALL encode it as `sha256:` followed by 64 lowercase hexadecimal characters.
3. `R7.AC3` WHEN equivalent normalized semantic results are produced in separate reconciliations, the system SHALL produce the same `resultHash`.
4. `R7.AC4` WHEN result collections differ only by omitted-versus-empty representation, the system SHALL produce the same `resultHash`.
5. `R7.AC5` WHEN result source or resource order changes, the system SHALL treat the ordered result as a different hash input.
6. `R7.AC6` WHEN `observedGeneration` changes without a semantic result change, the system SHALL preserve the existing `resultHash` value.
7. `R7.AC7` WHEN condition metadata changes without a semantic result change, the system SHALL preserve the existing `resultHash` value.
8. `R7.AC8` WHEN summary serialization changes without a semantic result change, the system SHALL preserve the existing `resultHash` value.
9. `R7.AC9` IF `status.result` is absent, THEN the system SHALL omit `status.resultHash`.
10. `R7.AC10` The system SHALL use `status.result` as the authoritative semantic value.

### R8 Stable Reasons, Messages, And Partial Errors

**User Story:** As a Kubeseer author, I want stable machine-readable reasons
and safe human-readable diagnostics, so that failures are actionable without
exposing observed data.

#### Public Condition Reason Codes

- `ConfigurationAccepted`
- `InvalidConfiguration`
- `AuthorizationSucceeded`
- `AuthorizationDenied`
- `AuthorizationNotEvaluated`
- `PolicyMissing`
- `PolicyInvalid`
- `ReadForbidden`
- `AuthorizationUnavailable`
- `ResolutionSucceeded`
- `ResolutionFailed`
- `ResolutionNotEvaluated`
- `ResolutionUnavailable`
- `EvaluationSucceeded`
- `EvaluationDegraded`
- `EvaluationUnavailable`

#### Acceptance Criteria

1. `R8.AC1` The system SHALL restrict canonical condition reasons to the public condition reason codes defined by this requirement.
2. `R8.AC2` WHEN the same terminal outcome recurs, the system SHALL select the same public condition reason.
3. `R8.AC3` WHEN a condition message reports evaluation outcome, the system SHALL describe the current observed generation.
4. `R8.AC4` WHEN a condition message reports counts, the system SHALL derive those counts from the published result snapshot.
5. `R8.AC5` The system SHALL exclude observed resource bodies from condition messages.
6. `R8.AC6` The system SHALL exclude extracted or typed values from condition messages.
7. `R8.AC7` The system SHALL exclude selector values from condition messages.
8. `R8.AC8` The system SHALL exclude field paths from condition messages.
9. `R8.AC9` The system SHALL exclude Secret data from every public status diagnostic.
10. `R8.AC10` WHEN a source fails while sibling sources succeed, the system SHALL retain the successful sibling results in `status.result`.
11. `R8.AC11` WHEN a source fails, the system SHALL retain its stable sanitized source error in that source result.
12. `R8.AC12` WHEN one field fails while sibling fields succeed, the system SHALL retain the successful sibling fields in that resource result.
13. `R8.AC13` WHEN a declared field fails before per-resource evaluation, the system SHALL retain its stable sanitized error in the source `fieldErrors` collection.
14. `R8.AC14` WHEN a per-resource field evaluation fails, the system SHALL retain its stable sanitized error in the owning field result.

### R9 Semantic Transitions And Write Suppression

**User Story:** As a platform operator, I want condition-aware semantic update
suppression, so that status remains current without self-triggered loops or
resource-version churn.

#### Acceptance Criteria

1. `R9.AC1` WHEN a candidate status is ready, the system SHALL compare its complete semantic projection with the persisted status before issuing a write.
2. `R9.AC2` WHEN the candidate and persisted semantic projections are equivalent, the system SHALL issue no status write.
3. `R9.AC3` WHEN top-level `observedGeneration` differs, the system SHALL treat the candidate status as semantically different.
4. `R9.AC4` WHEN the canonical condition set differs, the system SHALL treat the candidate status as semantically different.
5. `R9.AC5` WHEN a canonical condition `observedGeneration` differs, the system SHALL treat the candidate status as semantically different.
6. `R9.AC6` WHEN a canonical condition status differs, the system SHALL treat the candidate status as semantically different.
7. `R9.AC7` WHEN a canonical condition reason differs, the system SHALL treat the candidate status as semantically different.
8. `R9.AC8` WHEN a canonical condition message differs, the system SHALL treat the candidate status as semantically different.
9. `R9.AC9` WHEN processed-result summary presence differs, the system SHALL treat the candidate status as semantically different.
10. `R9.AC10` WHEN a processed-result summary count differs, the system SHALL treat the candidate status as semantically different.
11. `R9.AC11` WHEN the persisted `resultHash` differs from the candidate hash derived from `status.result`, the system SHALL treat the candidate status as semantically different.
12. `R9.AC12` WHEN the normalized structural result differs, the system SHALL treat the candidate status as semantically different.
13. `R9.AC13` WHEN candidate conditions differ only by canonical list ordering, the system SHALL treat those conditions as semantically equivalent.
14. `R9.AC14` WHEN candidate results differ only by API-normalized omitted-versus-empty collection representation, the system SHALL treat those results as semantically equivalent.
15. `R9.AC15` WHEN a candidate status is semantically different, the system SHALL attempt at most one status publication in that reconciliation.
16. `R9.AC16` IF a status write conflicts with a newer persisted resource version, THEN the system SHALL request a rate-limited reconciliation retry.

## Non-Functional Requirements

- `NFR1` **Kubernetes API compatibility:** Conditions SHALL follow native `metav1.Condition` semantics and list-map behavior; `R2` and `R9` provide the observable contract.
- `NFR2` **Determinism:** Summary derivation, reason selection, condition ordering, result normalization, and hashing SHALL produce stable outputs for the same semantic input; `R2.AC7`, `R3.AC4`, `R4.AC13`, `R4.AC14`, `R6`, `R7`, and `R8.AC2` provide behavioral coverage.
- `NFR3` **Confidentiality:** Public status SHALL expose only approved structural results and sanitized diagnostics; `R8.AC5` through `R8.AC14` provide behavioral coverage.
- `NFR4` **Reliability:** Status SHALL describe only one current Kubeseer identity and generation; `R1.AC7` through `R1.AC12` and `R9.AC16` provide behavioral coverage.
- `NFR5` **Idempotency:** Equivalent evaluations SHALL not produce status writes or transition-time churn; `R2.AC9` and `R9` provide behavioral coverage.
- `NFR6` **API evolution:** This feature SHALL extend the existing `kubeseer.io/v1alpha1` status envelope additively without redefining approved typed-result semantics; `R1`, `R6`, `R7`, and `R8.AC10` through `R8.AC14` provide behavioral coverage.
- `NFR7` **Testability:** The status contract SHALL be verifiable at genuine cross-module integration and envtest layers using deterministic assertions and a real status subresource; all acceptance criteria are observable through those boundaries.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `kubeseer-api-foundation`, `integration-testing-foundation`, `installation-access-policy`, `resource-discovery`, `resource-selection`, `field-extraction`, `typed-output-model`, and `reconciliation-runtime` contracts.
- `C2` The public API remains `kubeseer.io/v1alpha1`; `summary` and `resultHash` are additive status fields and no second API version is introduced.
- `C3` The existing `KubeseerResult`, source-state, field-state, provenance, typed payload, and result-error representations remain authoritative and are not redefined by this feature.
- `C4` Canonical conditions use `metav1.Condition` and remain a list map keyed by condition type.
- `C5` Status publication uses the status subresource with optimistic resource-version semantics; whole-object updates are not an allowed substitute.
- `C6` The condition and summary composer must consume terminal outcomes from the implemented reconciliation pipeline rather than re-read observed resources or duplicate discovery, policy, selection, extraction, or conversion logic.
- `C7` Cross-namespace aggregation is not required for the first vertical slice; when aggregation is later enabled, summary and condition semantics continue to derive from the resulting structural snapshot.
- `C8` Authorization condition rendering consumes existing policy and Kubernetes read outcomes; impersonation, SubjectAccessReview, and the complete enforcement audit belong to `authorization-enforcement`.
- `C9` Admission-time rejection and webhook UX belong to `admission-validation`; this feature reports deterministic invalid configurations that reach runtime evaluation.
- `C10` Automated proof follows the repository testing constitution: genuine cross-module integration is the minimum layer, envtest proves real API and status-subresource behavior, and no dedicated package-local unit-test layer is added.

## Out Of Scope

- A second Kubeseer API version or changes to approved spec, selection, extraction, typed-value, provenance, or result-error semantics.
- A separate public `status.state` or `status.phase` field that duplicates canonical conditions.
- A wall-clock `lastEvaluatedTime`, reconciliation history, durable status history, or per-reconciliation timestamp.
- New source-selection, field-extraction, typed-conversion, value-operator, grouping, or aggregation behavior.
- Installation-policy semantics, user impersonation, SubjectAccessReview, ServiceAccount RBAC creation, or authorization audit history.
- Admission webhooks, schema-validation UX, or pre-persistence policy enforcement.
- Metrics, Kubernetes Events, tracing, dashboards, alerts, or log-contract implementation.
- Product-wide cardinality limits, status-size limits, evaluation deadlines, worker budgets, hash-performance targets, or throughput tuning.
- Deployment manifests, production RBAC, leader election, packaging, installation, upgrades, or local cluster lifecycle.
