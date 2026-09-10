---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-10T06:13:03Z
last_modified: 2026-09-10T06:13:03Z
approved_fingerprint: sha256:c7922cf02203430e85a046f14a7061b1796df730f3e1bbcfe95be6a936d7e763
source_requirements_approved_at: 2026-09-06T08:34:51Z
source_requirements_fingerprint: sha256:b245d66d600bd2d850e90738a2a638b0286770f20c01c40f2dea4b4823fa7490
---

# Feature Design

## Overview

Implement the approved configuration-budget rejection contract through the
existing status composer and guarded publisher. A rejected object gets one
constant-size terminal evaluation, independent of its source count. Its old
result, summary, and resultHash disappear only after a successful status write.

This Design covers requirements R1–R4 and NFR1–NFR4, including documentation.
It proposes no new API fields or installation settings.

## Architecture

```text
load Kubeseer → acquire lease → validate effective budget
                                    |
                              budget rejected
                                    |
                     remove this owner's route bindings
                                    |
                  compose configuration rejection evaluation
                                    |
                guarded publisher → Kubernetes status subresource
```

The rejection branch remains before policy loading, discovery, and source
execution. Owned-resource status I/O is permitted; observed-resource I/O is not.

## Options Considered

### Option A — Extend the existing evaluation and publisher (selected)

Add a distinct configuration-budget terminal state to internal/status.Evaluation.
Compose the five canonical conditions in internal/status and publish through
StatusPublisher. This preserves one place for freshness guards, size checks,
conflict classification, semantic comparison, and status Events.

### Option B — Clear status directly from the runtime

A direct update could remove old results but would duplicate publication guards,
condition merging, size enforcement, and observability. Reject this option
because rejection would acquire its own status-writing path.

## Simplicity And Elegance Review

A dedicated ConfigurationBudgetExceeded boolean follows the existing
ResultLimitExceeded pattern. Validate it as mutually exclusive with Result,
ResultUnavailable, ResultLimitExceeded, source assessments, and global
authorization. A broad evaluation-state redesign is unnecessary for this fix.

No per-source rejection results or retained payloads are allocated. The runtime
keeps its existing first-field diagnostic; public condition messages are fixed.
No controller, cache, API field, or policy query is added.

## Components And Interfaces

### Runtime rejection branch

In internal/reconciliation/pipeline.go, replace the immediate budget-error
return with a rejection completion helper. It checks the current lease, removes
the owner's routes, creates the terminal Evaluation, and invokes the existing
Publisher.Publish with the lease context. Using the lease context permits a
small terminal publication after evaluation timeout, while cancellation and
identity invalidation still suppress it.

Do not call Planner, Enforcer, Executor, or PolicySource on this branch.
On a successful write or semantic no-op, return the existing sanitized
ConfigurationBudgetExceeded runtime error and terminal outcome. Preserve current
controller retry behavior; this correction does not redesign permanent errors.

Requirements: R1.AC7–R1.AC8, R2.AC1–R2.AC6, R3.AC1–R3.AC4.

### Route removal

Reuse RouteRegistry.RemoveOwner and its exact-owner indexes. Last-owner removal
cancels a supervisor; other owners retain their bindings. RemoveOwner currently
can wait for promoted WATCH startup. Separate promotion launch from readiness
waiting so this cleanup and lifecycle callbacks perform no blocking network
work. Supervisors remain registry-owned and capacity-bounded.

This local prerequisite is also required by watch-startup-cancellation; both
implementations must converge on the same non-waiting promotion helper.
It is not a prerequisite to implement the entire watch correction first.

Requirements: R2.AC1–R2.AC3.

### Status composition and bounded publication

Extend validateEvaluation and Compose in internal/status/conditions.go with the
new terminal mode. Construct a fresh KubeseerStatus without any observation
payload. Retain the normal mergeConditions behavior, including transition times
and noncanonical conditions, and the normal semantic comparison.

| Condition | Status | Reason |
| --- | --- | --- |
| Accepted | False | ConfigurationBudgetExceeded |
| Authorized | Unknown | AuthorizationNotEvaluated |
| SourcesResolved | Unknown | ResolutionNotEvaluated |
| Ready | False | ConfigurationBudgetExceeded |
| Degraded | True | EvaluationUnavailable |

Degraded follows the baseline unavailable-result semantics. All conditions and
status.observedGeneration refer to the rejected generation. Messages contain
fixed explanations; no prior result values or source-indexed details enter them.

The publisher measures the complete merged status. For this already compact
terminal mode, exceeding the ceiling returns StatusLimitInvalid directly:
do not relabel it as ResultLimitExceeded or preserve old successful conditions.
Retained foreign conditions can make publication too large; report that failure
rather than silently deleting another condition owner's data.

Requirements: R1.AC1–R1.AC6, R2.AC4–R2.AC9, R3.AC1.

### Diagnostics and documentation

Add ConfigurationBudgetExceeded to the finite observability reason vocabulary
and normalization where necessary. Only successful semantic writes emit status
Events. An unsuccessful status write must be reported as a publication failure;
it must not generate an Event claiming removal.

Update docs/api-reference.md with the condition table and omitted result fields.
Update docs/operations.md with generation checks, budget-versus-policy diagnosis,
and recovery by fixing the declaration or restarting with compatible limits.
Update docs/security.md with the successful-write boundary and API/RBAC
limitations. Examples show identity, generation, conditions, and field presence,
never observation payloads. These edits belong to a dedicated later leaf task.

Requirements: R2.AC9, R4.AC1–R4.AC4.

## Data Models

Only the internal Evaluation gains a field. The public v1alpha1 result schema,
spec, metadata, RBAC, and configured limits remain unchanged. A rejection status
omits result, summary, and resultHash rather than representing an empty successful
observation. The composer never derives a summary from prior persisted data.

## Error Handling

- Stale UID/generation/deletion/policy epoch or canceled lease: suppress the write.
- Conflict or transient status read/write error: propagate its existing retryable
  classification so a new attempt re-reads the current object.
- Forbidden status write: surface sanitized StatusUnavailable; no deletion claim.
- Impossible compact status: StatusLimitInvalid, no oversized write.
- Later compliant spec/profile: run the normal pipeline with fresh policy;
  denial remains denial, and success replaces rejection with a fresh result.

## Security Considerations

Publication does not depend on policy availability. No over-budget source is
read to decide what data to remove. Removing all observation fields closes the
retention case without reconstructing authorization for old results. Persisted
data cannot be promised erased during an API outage or denied status write.

## Failure Modes And Tradeoffs

The runtime still holds the loaded object until completion; this correction
bounds rejection output rather than introducing metadata-only object loading.
A policy event can invalidate cleanup publication, requiring a current retry.
Condition preservation can prevent publication under an unusually small ceiling;
the documented StatusLimitInvalid path retains ownership semantics.

## Baseline Contract Reconciliation

The performance-and-limits Requirements R2 already prohibit observed-resource
I/O, not status writes. Its Design sections Admission and runtime configuration
budgets, Error Handling, and Verification Plan need to describe terminal
publication and route cleanup. Its task 2.1 proof uses the integration suite;
replace the no-publication assertion in
assertPerformanceLimitsConfigurationScenarios with no-source-I/O plus persisted
rejection assertions. Review affected task descriptions and proof coverage.

The status-and-conditions condition contract must accommodate this new terminal
reason through the corrective feature. During baseline reconciliation, identify
any exhaustive baseline condition tables and update their Design/proof coverage.
Any changed approved baseline document follows walden reconcile and its own
review gate before execution. Existing approvals are not silently rewritten.
No baseline document is mutated by this Design draft.

## Testing Strategy

Cross-module tests compose the real budget validator, Runtime, RouteRegistry,
StatusPublisher, and status composer. Control failures only at I/O ports.
Start with an actual successful persisted result, then tighten the profile and
remove/restrict/fail the policy source. Assert zero policy/discovery/source calls,
route cleanup, preserved shared owners, and the full rejection status.

Use envtest for status persistence, resource-version conflicts, forbidden writes,
and successful/denied recovery. Repeated rejection must preserve transition times
and avoid another status write. Exercise huge source count, minimal status budget,
and oversized retained foreign conditions. No dedicated unit-test layer.

## Verification Plan

- R1/R2: initial result → profile tightening → rejection; assert field absence,
  five conditions, generation, no source I/O, and publication-failure diagnostics.
- R2: controlled freshness races and shared-route cleanup; a blocked promoted
  WATCH must not block invalidation. Assert no stale writes or other-owner loss.
- R3: repeat unchanged rejection, edit into compliance, then test denied and
  authorized recovery with no old-value restoration.
- R4: review all three named guides against the condition table and failure
  fixtures; validate relative links and check examples for payload disclosure.
- Later Tasks use make test-integration, make test-api with pinned local assets,
  and appropriate race coverage. Each proof asserts the named scenario ran.
  Documentation review is its own evidence item; tests alone do not prove prose.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Runtime rejection branch; Status composition and bounded publication |
| `R2` | Route removal; Error Handling; Security Considerations; Verification Plan |
| `R3` | Runtime rejection branch; Error Handling; recovery and idempotency tests |
| `R4` | Diagnostics and documentation; documentation verification |
| `NFR1` | Payload-free rejection; no policy/source reads; sanitized diagnostics |
| `NFR2` | Existing publication guards; bounded status; shared-route ownership |
| `NFR3` | Semantic comparison; retry and compliant-object recovery |
| `NFR4` | Condition table and API/operations/security guide updates |
