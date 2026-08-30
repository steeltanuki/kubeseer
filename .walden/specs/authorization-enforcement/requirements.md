---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-27T10:06:29Z
last_modified: 2026-08-27T10:06:29Z
approved_fingerprint: sha256:938416c4cd20919d2a8a3bd1abd3379b056a0de06d5eb872fa47b042310bf23e
---

# Requirements Document

## Introduction

This feature completes Kubeseer's minimum vertical slice by enforcing the
administrator-owned installation access policy at every resource-instance I/O
boundary. It binds each discovery-normalized source target to a fresh logical
policy decision, permits LIST and metadata WATCH operations only through an
exact current authorization capability, revokes stale capabilities when the
policy changes, and prevents stale or newly unauthorized values from reaching
status.

The installation access policy remains the sole definition of the logical
observation ceiling. Kubernetes RBAC for the operator ServiceAccount remains an
independent execution constraint, while the permissions of a Kubeseer author
neither broaden nor replace the administrator-approved delegated observation
scope. This feature also defines sanitized decision evidence suitable for later
observability integration without introducing durable audit storage or new
public API fields.

<!-- assumed: authorization-enforcement composes the existing accesspolicy evaluator, opaque selection capability, reconciliation freshness lease, authorized route registry, and status outcomes instead of introducing a second policy engine (source: approved installation-access-policy, resource-selection, reconciliation-runtime, and status-and-conditions contracts) -->
<!-- assumed: Kubeseer uses administrator-approved delegated observation and does not impersonate the Kubeseer author or require that author to hold direct read RBAC on observed resources (source: .walden/constitution.md Non-Negotiable Security Rules) -->
<!-- assumed: authorization capabilities and decision evidence are process-local, non-serializable values; durable audit storage, retention, transport, and querying belong to observability (source: SPECIFICATIONS.md authorization-enforcement and observability boundaries) -->
<!-- assumed: a Kubeseer with zero sources has no authorization target and preserves the approved successful empty-result behavior without requiring the installation policy singleton (source: approved reconciliation-runtime and status-and-conditions contracts) -->

## Requirements

### R1 Logical Authority And Exact Target Identity

**User Story:** As a cluster administrator, I want every observed target bounded by the active installation policy, so that neither user configuration nor Kubernetes credentials can broaden the approved scope.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL treat the active `KubeseerAccessPolicy` named `installation-access-ceiling` as the sole logical authority for runtime observation.
2. `R1.AC2` WHEN discovery resolves a source, the system SHALL derive its authorization target from the exact source identifier, normalized API group, Kind, discovered scope, and namespace.
3. `R1.AC3` WHEN a source requests a namespaced target, the system SHALL require both its exact resource type and namespace to be allowed by the active policy.
4. `R1.AC4` WHEN a source requests a cluster-scoped target, the system SHALL require both its exact resource type and cluster-scoped access to be allowed by the active policy.
5. `R1.AC5` IF a Kubeseer source requests any target outside the active installation policy, THEN the system SHALL deny that exact target.
6. `R1.AC6` The system SHALL derive resource scope only from Kubernetes discovery metadata.
7. `R1.AC7` The system SHALL prevent selector, extraction, typing, or downstream processing configuration from changing an authorization target after its decision.

### R2 Fresh And Fail-Closed Policy Evaluation

**User Story:** As a security-conscious operator, I want each reconciliation to use current fail-closed policy state, so that missing or unhealthy policy state cannot reuse an older permission.

#### Acceptance Criteria

1. `R2.AC1` WHEN reconciliation begins for a live Kubeseer with at least one source, the system SHALL load a fresh installation-policy snapshot before evaluating any exact target.
2. `R2.AC2` IF the active installation policy is missing, THEN the system SHALL deny every exact target with reason `PolicyMissing`.
3. `R2.AC3` IF the active installation policy is invalid, THEN the system SHALL deny every exact target with reason `PolicyInvalid`.
4. `R2.AC4` IF the active installation policy is unavailable, THEN the system SHALL deny every exact target with reason `PolicyUnavailable`.
5. `R2.AC5` WHILE policy state is missing, invalid, or unavailable, the system SHALL produce no allow decision from cached, default, or previously loaded policy state.
6. `R2.AC6` IF policy state is temporarily unavailable, THEN the system SHALL request a rate-limited reconciliation retry for each affected Kubeseer key.
7. `R2.AC7` WHEN equivalent policy and target inputs are evaluated, the system SHALL produce the same logical decision and stable reason.
8. `R2.AC8` WHEN a Kubeseer declares zero sources, the system SHALL perform no installation-policy authorization evaluation.

### R3 Exact Authorization Capability

**User Story:** As an enforcement component, I want resource I/O reachable only through an exact opaque authorization capability, so that incomplete, denied, or mismatched decisions cannot be bypassed accidentally.

#### Acceptance Criteria

1. `R3.AC1` WHEN one source produces exact read targets, the system SHALL evaluate one logical policy decision for every target before constructing an executable source plan.
2. `R3.AC2` WHEN an exact target has one matching allowed decision from the current policy snapshot, the system SHALL permit construction of an opaque authorization capability for that target.
3. `R3.AC3` The system SHALL bind each authorization capability to the Kubeseer namespace, name, UID, generation, source identifier, exact target identity, and current policy epoch.
4. `R3.AC4` The system SHALL keep authorization capability state immutable and unavailable for direct construction outside the enforcement boundary.
5. `R3.AC5` IF an exact target has no matching decision, THEN the system SHALL construct no executable authorization capability for its source.
6. `R3.AC6` IF an exact target has a denied decision, THEN the system SHALL construct no executable authorization capability for its source.
7. `R3.AC7` IF a decision refers to a different source, resource identity, scope, or namespace, THEN the system SHALL reject that decision as an authorization mismatch.
8. `R3.AC8` IF more than one decision matches an exact target, THEN the system SHALL reject that decision set as an authorization mismatch.
9. `R3.AC9` IF a decision set contains a target absent from the source plan, THEN the system SHALL reject that decision set as an authorization mismatch.
10. `R3.AC10` The system SHALL require an opaque current authorization capability at every resource-instance I/O adapter.

### R4 Enforcement At LIST And WATCH Boundaries

**User Story:** As a cluster administrator, I want every LIST page and metadata WATCH lifecycle guarded by current authorization, so that later requests cannot outlive the decision that permitted them.

#### Acceptance Criteria

1. `R4.AC1` WHEN selection starts its first LIST for an exact target, the system SHALL verify that the target's authorization capability is current.
2. `R4.AC2` WHEN selection requests a continuation page for an exact target, the system SHALL verify that the target's authorization capability is current before issuing the request.
3. `R4.AC3` WHEN selection restarts a LIST after an expired continuation, the system SHALL verify that the target's authorization capability is current before issuing the request.
4. `R4.AC4` WHEN source routing starts a metadata WATCH for an exact target, the system SHALL verify that the target's authorization capability is current.
5. `R4.AC5` WHEN source routing restarts a metadata WATCH for an exact target, the system SHALL verify that the target's authorization capability is current before issuing the request.
6. `R4.AC6` IF a capability becomes stale or its reconciliation context is canceled, THEN the system SHALL issue no subsequent resource-instance request through that capability.
7. `R4.AC7` IF any exact target of one source lacks a current allowed capability, THEN the system SHALL issue no resource-instance LIST for any target of that source execution.
8. `R4.AC8` IF an exact target lacks a current allowed capability, THEN the system SHALL issue no metadata WATCH for that target.
9. `R4.AC9` WHEN one source is denied and an independent sibling source is authorized, the system SHALL preserve execution of the authorized sibling source.
10. `R4.AC10` WHEN selector constraints are applied through an authorized capability, the system SHALL keep the authorized GVR, discovered scope, and namespace unchanged.

### R5 Policy Change Revocation And Result Invalidation

**User Story:** As a cluster administrator, I want policy restrictions to revoke previous authorization promptly, so that old watches, in-flight work, and published values cannot survive a narrowed ceiling.

#### Acceptance Criteria

1. `R5.AC1` WHEN the active installation policy is created, replaced, changed to a new generation, marked for deletion, or deleted, the system SHALL invalidate every active authorization capability from the previous policy epoch.
2. `R5.AC2` WHEN the policy epoch is invalidated, the system SHALL cancel every active reconciliation context bound to the previous epoch.
3. `R5.AC3` WHEN the policy epoch is invalidated, the system SHALL remove every source route authorized under the previous epoch.
4. `R5.AC4` WHEN the policy epoch is invalidated, the system SHALL enqueue every existing Kubeseer for fresh policy evaluation.
5. `R5.AC5` IF an active metadata WATCH loses its last current authorized route, THEN the system SHALL stop that WATCH.
6. `R5.AC6` IF a policy change makes an in-flight candidate stale, THEN the system SHALL withhold that candidate from status publication.
7. `R5.AC7` WHEN a fresh policy decision denies a previously allowed target, the system SHALL omit values from that target in the next publishable result.
8. `R5.AC8` WHEN a policy change broadens the allowed scope, the system SHALL require a fresh exact-target decision before issuing a resource-instance request for the broadened scope.
9. `R5.AC9` WHEN a policy change races with a source event, the system SHALL use the current policy epoch before the next resource-instance request.
10. `R5.AC10` WHEN the controller restarts, the system SHALL reconstruct authorization only from freshly loaded policy state.

### R6 Kubernetes RBAC And Delegated Observation Separation

**User Story:** As a platform operator, I want logical authorization, operator credentials, and Kubeseer-author permissions kept distinct, so that delegated observation remains explicit and cannot become an accidental privilege grant.

#### Acceptance Criteria

1. `R6.AC1` The system SHALL compute logical authorization independently of the Kubernetes RBAC granted to the operator ServiceAccount.
2. `R6.AC2` IF the operator ServiceAccount has broader RBAC than the active installation policy, THEN the system SHALL keep every out-of-policy target denied.
3. `R6.AC3` WHEN logical policy allows an exact target, the system SHALL avoid representing that decision as proof that Kubernetes RBAC permits the request.
4. `R6.AC4` IF Kubernetes RBAC forbids a resource request made through a logically allowed capability, THEN the system SHALL report a source-scoped `ReadForbidden` outcome.
5. `R6.AC5` IF Kubernetes RBAC forbids a resource request, THEN the system SHALL publish no observed value returned by that failed request.
6. `R6.AC6` The system SHALL avoid impersonating the Kubeseer author for observed-resource reads.
7. `R6.AC7` The system SHALL avoid requiring direct observed-resource read RBAC from the Kubeseer author.
8. `R6.AC8` IF the Kubeseer author has direct RBAC for an out-of-policy target, THEN the system SHALL keep that target denied.
9. `R6.AC9` IF a logically allowed read fails for a transient reason other than RBAC denial, THEN the system SHALL preserve the logical allow outcome as distinct from the read failure.

### R7 Sanitized Authorization Decision Evidence

**User Story:** As a platform operator, I want each authorization outcome represented by stable sanitized evidence, so that later observability can audit enforcement without handling observed data.

#### Acceptance Criteria

1. `R7.AC1` WHEN an exact target is evaluated, the system SHALL produce one authorization decision record for that target.
2. `R7.AC2` The system SHALL include the Kubeseer namespace, name, UID, generation, source identifier, exact target identity, policy state, applicable policy identity, outcome, and stable reason in each decision record.
3. `R7.AC3` WHEN the active policy is present, the system SHALL identify it in decision evidence by canonical name, UID, and generation.
4. `R7.AC4` WHEN policy state is missing or unavailable, the system SHALL represent that terminal state without inventing a policy UID or generation.
5. `R7.AC5` WHEN an allowed decision is used to construct a capability, the system SHALL expose its decision record to the authorization evidence boundary before resource-instance I/O begins.
6. `R7.AC6` WHEN Kubernetes RBAC forbids a logically allowed request, the system SHALL produce a sanitized enforcement record linked to the original logical decision dimensions.
7. `R7.AC7` WHEN multiple source targets are evaluated, the system SHALL order decision records by source declaration order and exact target order.
8. `R7.AC8` WHEN equivalent policy, Kubeseer identity, generation, and target inputs recur, the system SHALL produce semantically equivalent decision evidence.
9. `R7.AC9` The system SHALL exclude selector values, observed object names, observed object UIDs, resource bodies, extracted values, typed values, field paths, Secret data, and raw Kubernetes error bodies from authorization evidence.
10. `R7.AC10` The system SHALL expose authorization evidence through a narrow in-process boundary without requiring durable audit storage in this feature.

### R8 Public Outcomes And Confidential Failure Handling

**User Story:** As a Kubeseer consumer, I want authorization failures reflected through the existing result and condition contracts, so that denied access is visible without retaining protected data.

#### Acceptance Criteria

1. `R8.AC1` WHEN an exact target is explicitly denied by a valid policy, the system SHALL report a sanitized source error with the stable upstream denial reason.
2. `R8.AC2` WHEN any exact source target is explicitly denied by a valid policy, the system SHALL supply the existing `AuthorizationDenied` outcome to status composition.
3. `R8.AC3` IF the active policy is missing, THEN the system SHALL supply the existing `PolicyMissing` outcome to status composition.
4. `R8.AC4` IF the active policy is invalid, THEN the system SHALL supply the existing `PolicyInvalid` outcome to status composition.
5. `R8.AC5` IF policy state is temporarily unavailable, THEN the system SHALL supply the existing `AuthorizationUnavailable` outcome to status composition.
6. `R8.AC6` IF deterministic configuration or resolution failure prevents authorization, THEN the system SHALL supply the existing `AuthorizationNotEvaluated` outcome to status composition.
7. `R8.AC7` IF Kubernetes RBAC forbids a logically allowed read, THEN the system SHALL supply the existing `ReadForbidden` outcome to status composition.
8. `R8.AC8` WHEN an authorized sibling source succeeds while another source is denied, the system SHALL retain the authorized sibling result in the candidate status.
9. `R8.AC9` WHEN authorization failure replaces a previously successful target, the system SHALL retain no value from the previous successful target in the new candidate result.
10. `R8.AC10` WHEN an authorization failure is reported, the system SHALL exclude observed resource contents and extracted values from its diagnostic.
11. `R8.AC11` WHEN an authorization failure is reported, the system SHALL exclude selector values and field paths from its diagnostic.
12. `R8.AC12` WHEN an authorization failure is reported, the system SHALL exclude Secret data and raw Kubernetes error bodies from its diagnostic.
13. `R8.AC13` IF the Kubeseer UID, generation, deletion state, or policy epoch becomes stale before publication, THEN the system SHALL issue no status write for that authorization evaluation.

## Non-Functional Requirements

- `NFR1` **Security:** Every resource-instance LIST and metadata WATCH operation SHALL require a current exact-target authorization capability; `R1`, `R2`, `R3`, `R4`, `R5`, and `R6` provide the observable behavior.
- `NFR2` **Revocation safety:** Policy restrictions SHALL cancel stale work, remove obsolete routes, and prevent stale or newly denied values from being published; `R5` and `R8.AC9`, `R8.AC13` provide the observable behavior.
- `NFR3` **Confidentiality:** Authorization decisions, evidence, status, and diagnostics SHALL exclude observed data and sensitive configuration values; `R7.AC9` and `R8.AC10` through `R8.AC12` provide the observable behavior.
- `NFR4` **Determinism and auditability:** Equivalent authorization inputs SHALL produce stable ordered decisions, reasons, and sanitized evidence; `R2.AC7` and `R7` provide the observable behavior.
- `NFR5` **Failure isolation:** Denial or runtime RBAC failure for one source SHALL not prevent independent authorized sibling sources from completing; `R4.AC7` through `R4.AC9`, `R6.AC4`, `R6.AC5`, and `R8.AC8` provide the observable behavior.
- `NFR6` **API compatibility:** Enforcement SHALL reuse the existing `kubeseer.io/v1alpha1` source, result, error, and condition contracts without adding a second API version or authorization-history field; `R8` provides the observable behavior.
- `NFR7` **Testability:** Enforcement SHALL be verifiable through genuine cross-module integration and envtest scenarios using real policy, discovery, selection, reconciliation, status, and Kubernetes API boundaries; all acceptance criteria are observable through those layers.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `kubeseer-api-foundation`, `integration-testing-foundation`, `resource-discovery`, `installation-access-policy`, `resource-selection`, `reconciliation-runtime`, and `status-and-conditions` contracts.
- `C2` Installation-policy compilation and evaluation remain authoritative in `internal/accesspolicy`; this feature must not duplicate or reinterpret namespace, resource-type, or cluster-scope policy semantics.
- `C3` Resource-instance selection remains LIST-only through the dynamic-client boundary, while source routing remains metadata-only WATCH; any future observed-resource I/O operation must use the same exact authorization capability rule before execution.
- `C4` Policy invalidation composes with the reconciliation freshness tracker, current policy epoch, route registry, child-context cancellation, periodic safety reconciliation, and guarded status publication.
- `C5` The operator ServiceAccount performs Kubernetes requests under its configured RBAC; this feature creates no Role, ClusterRole, RoleBinding, or ClusterRoleBinding.
- `C6` The public API remains `kubeseer.io/v1alpha1`; authorization evidence is internal and process-local.
- `C7` Durable audit storage, retention, delivery guarantees, querying, metrics, Kubernetes Events, and production log formatting belong to `observability`.
- `C8` Automated proof follows the repository testing constitution: genuine cross-module integration is the minimum layer, envtest proves Kubernetes policy-change and I/O behavior, and no dedicated package-local unit-test layer is added.
- `C9` Kubeseer-authored source, tests, and generated artifacts remain under Apache License 2.0 with Alessandro Rontani as the default copyright holder.

## Out Of Scope

- Changing `KubeseerAccessPolicy` schema, singleton naming, namespace modes, resource allowlist semantics, or denial precedence.
- User impersonation, SubjectAccessReview, per-user policy rules, tenant-specific authorization, or admission-time creator permission checks.
- Creating, widening, or reconciling operator ServiceAccount RBAC.
- Durable authorization audit history, external audit sinks, retention policies, search APIs, dashboards, alerts, metrics, tracing, or Kubernetes Events.
- New public status fields, condition types, reason codes, history records, or API versions.
- Resource discovery, selector semantics, field extraction, typed conversion, value operators, grouping, aggregation, or cardinality limits.
- Admission webhooks or pre-persistence discovery-dependent authorization checks.
- Packaging, leader election, production deployment topology, or local cluster lifecycle.
