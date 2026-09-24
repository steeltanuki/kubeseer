---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-06T08:34:51Z
last_modified: 2026-09-06T08:34:51Z
approved_fingerprint: sha256:b245d66d600bd2d850e90738a2a638b0286770f20c01c40f2dea4b4823fa7490
---

# Requirements Document

## Introduction

This corrective feature addresses review finding 1 (P1): an existing Kubeseer
can retain previously authorized results indefinitely after a manager limit is
tightened, even if the installation policy is subsequently removed or restricted.
Runtime budget rejection currently returns before status publication.

The observable outcome is a bounded, current-generation configuration failure
status with no retained observation data. Rejection remains before discovery
and observed-resource I/O.

### Evidence And Baseline

- Reproduction: persist a two-source Kubeseer with a result, restart with
  maximum sources set to one, remove the installation policy, and reconcile.
  The current runtime returns ConfigurationBudgetExceeded without publishing.
- Code: `internal/reconciliation/pipeline.go`, budget check in `reconcileKey`.
- Existing coverage: `assertPerformanceLimitsConfigurationScenarios` in
  `test/integration/performance_limits_profile_test.go` explicitly expects no
  publication on budget rejection but starts without an old published result.
- Baseline contracts: `performance-and-limits` R2, `authorization-enforcement`
  R5/R8, and the freshness and semantic-write rules in `status-and-conditions`.

<!-- assumed: model the review findings as separately reviewable corrective features supplementing the existing approved baseline (source: user request to convert the review into Walden specs) -->
<!-- assumed: reject the whole over-budget object and remove all prior observation data without evaluating its sources or requiring a policy read (source: performance-and-limits R2.AC3/R2.AC6 and the constitution's result invalidation security rule) -->

This proposal explicitly replaces the existing no-publication expectation for
runtime budget rejection. It does not relax the no-observed-resource-I/O rule.
Related baseline design and proof changes must be identified during Design
and reconciled through their own gates before implementation relies on them.

## Requirements

### R1 Observable Configuration Rejection

**User Story:** As an administrator, I want rejected persisted configurations to
lose their previous results, so that old observations do not remain available.

#### Acceptance Criteria

1. `R1.AC1` WHEN runtime detects that a persisted Kubeseer exceeds an effective configuration budget, the system SHALL compose a current-generation rejection status without result, summary, or resultHash.
2. `R1.AC2` WHEN runtime composes a configuration-budget rejection status, the system SHALL set Accepted=False with reason ConfigurationBudgetExceeded.
3. `R1.AC3` WHEN runtime composes a configuration-budget rejection status, the system SHALL set Ready=False with reason ConfigurationBudgetExceeded.
4. `R1.AC4` WHEN runtime composes a configuration-budget rejection status before policy evaluation, the system SHALL represent Authorized as Unknown with reason AuthorizationNotEvaluated.
5. `R1.AC5` WHEN runtime composes a configuration-budget rejection status before discovery, the system SHALL represent SourcesResolved as Unknown with reason ResolutionNotEvaluated.
6. `R1.AC6` WHEN runtime composes a configuration-budget rejection status, the system SHALL set observedGeneration to the rejected generation.
7. `R1.AC7` WHEN runtime rejects an over-budget Kubeseer with previously published observation data, the system SHALL replace that data through the guarded status-subresource publication path.
8. `R1.AC8` IF the installation policy is missing, invalid, restricted, or unavailable during configuration-budget rejection, THEN the system SHALL perform rejection-status publication without depending on a successful policy read.

### R2 Bounded And Safe Failure Handling

**User Story:** As an operator, I want rejection to remain safe under concurrent
changes and API failures, so that cleanup cannot publish stale data.

#### Acceptance Criteria

1. `R2.AC1` The system SHALL perform no discovery or observed-resource I/O for a generation rejected by configuration-budget validation.
2. `R2.AC2` WHEN runtime rejects an over-budget Kubeseer, the system SHALL remove that owner's source-route bindings.
3. `R2.AC3` WHEN rejection removes bindings from a shared target, the system SHALL preserve bindings belonging to other current authorized owners.
4. `R2.AC4` IF the Kubeseer UID, generation, deletion state, or policy epoch invalidates a rejection candidate before publication, THEN the system SHALL suppress that stale status write.
5. `R2.AC5` IF a rejection-status write conflicts or fails transiently, THEN the system SHALL schedule a retry through the existing reconciliation error path.
6. `R2.AC6` IF a rejection-status write is forbidden, THEN the system SHALL report the sanitized status-publication failure without claiming that persisted data was removed.
7. `R2.AC7` The system SHALL keep rejection status within the effective status-byte ceiling independently of the rejected source count.
8. `R2.AC8` IF the effective status ceiling cannot contain a rejection status, THEN the system SHALL report StatusLimitInvalid without issuing an oversized write.
9. `R2.AC9` WHEN configuration-budget diagnostics are emitted, the system SHALL exclude submitted values, selector contents, JSONPath expressions, and prior observation payloads.

### R3 Idempotency And Recovery

**User Story:** As a user, I want a corrected configuration to recover through
normal evaluation, so that budget failures do not permanently disable my object.

#### Acceptance Criteria

1. `R3.AC1` WHEN the persisted rejection status is semantically equivalent to the new rejection candidate, the system SHALL suppress the status write.
2. `R3.AC2` WHEN an object becomes compliant through a spec edit or a manager restart with a compatible profile, the system SHALL evaluate it using the current installation policy.
3. `R3.AC3` IF authorization denies a formerly over-budget object after it becomes compliant, THEN the system SHALL publish the existing authorization-denial outcome without restoring old observation data.
4. `R3.AC4` WHEN a formerly over-budget object completes a successful authorized evaluation, the system SHALL replace rejection status with the newly computed result.

### R4 Documentation And Operational Guidance

**User Story:** As a cluster administrator, I want the public guides to explain
budget rejection and result invalidation, so that I can diagnose stale-looking
status safely.

#### Acceptance Criteria

1. `R4.AC1` WHEN this feature is released, the system SHALL document configuration-budget rejection, its condition reasons, and the absence of an observation result in `docs/api-reference.md`.
2. `R4.AC2` WHEN this feature is released, the system SHALL document how to inspect an over-budget Kubeseer, distinguish it from policy denial, and verify status freshness in `docs/operations.md`.
3. `R4.AC3` WHEN this feature is released, the system SHALL document that prior result data is removed only after a successful guarded status write and that API or RBAC failures can delay removal in `docs/security.md`.
4. `R4.AC4` WHEN documentation examples show a budget rejection, the system SHALL omit source payloads, selector operands, JSONPath expressions, and sensitive values.

## Non-Functional Requirements

- `NFR1` Confidentiality: prior observations are removed without requiring renewed source access; verified by R1.AC1, R1.AC7, R1.AC8, R2.AC1, and R2.AC9.
- `NFR2` Concurrency safety and boundedness: rejection obeys identity guards and output ceilings; verified by R2.AC3 through R2.AC8.
- `NFR3` Recoverability and determinism: repeated failures do not cause redundant writes or prevent recovery; verified by R3.AC1 through R3.AC4.
- `NFR4` Operability: administrators can interpret budget rejection and verify whether old data was removed through the documented status and diagnostic paths; verified by R4.AC1 through R4.AC4.

## Constraints And Dependencies

- `C1` Depends on the existing performance-and-limits, reconciliation-runtime,
  status-and-conditions, authorization-enforcement, and integration-testing-foundation contracts.
- `C2` The public API remains kubeseer.io/v1alpha1; this proposal adds the
  ConfigurationBudgetExceeded condition outcome without changing CRD fields.
  Other condition semantics follow status-and-conditions.
- `C3` Status removal is successful only after the Kubernetes API accepts the
  write. Unavailable or forbidden status writes cannot guarantee immediate
  deletion of persisted data; R2.AC5/R2.AC6 define the observable failure.
- `C4` Regression coverage must start with a real previously published result,
  then change limits and policy. Cross-module tests cover routing and publication;
  envtest covers persistence, conflicts, and recovery. No dedicated unit-test layer.
- `C5` Implementation planning must replace the baseline test's unconditional
  no-publication assertion with a no-observed-resource-I/O assertion plus
  current rejection-status proof.
- `C6` The documentation update is part of this feature's release scope and
  must be planned as a leaf task after Requirements and Design approval; it
  must not be represented as completed by this Requirements draft alone.

## Out Of Scope

- Changing configured or structural budget ceilings.
- Reading or partially evaluating sources from a rejected object.
- Changing the delegated authorization model, RBAC, or admission acceptance rules.
- Guaranteeing erasure from historical API revisions or external consumers.
- Implementing watch startup or scalar conversion corrections.
