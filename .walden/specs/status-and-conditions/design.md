---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-27T07:46:52Z
last_modified: 2026-08-27T07:46:52Z
approved_fingerprint: sha256:a5882173e77e74be0a78363659a0b7d004781de0951a98a1a3c7e13e8f142fcf
source_requirements_approved_at: 2026-08-27T07:28:04Z
source_requirements_fingerprint: sha256:894c7c2d96e506eb1ac95e83ff165ca8aa36242faa82b36d5a47a91d066876bf
---

# Status And Conditions Design

## Overview

The feature extends `KubeseerStatus` with a processed-result summary and a
semantic result hash, then makes the existing reconciliation publisher compose
and publish one complete current-generation status snapshot. A new pure
`internal/status` boundary owns result normalization, summary and hash
derivation, condition aggregation, transition handling, canonical ordering,
and semantic projection. The reconciliation pipeline remains the authority for
what happened and supplies typed terminal assessments without asking the
status boundary to repeat policy, discovery, selection, extraction, or
conversion work.

The existing publisher remains in `internal/reconciliation`. It already owns
the evaluation lease, fresh direct read, UID and generation checks, deletion
guard, context guard, semantic no-op suppression, status-subresource write,
and conflict-to-retry behavior. It will delegate status composition and full
semantic projection to `internal/status`, preserving those proven freshness
controls while replacing its result-only candidate with an atomic full-status
candidate.

<!-- assumed: the constitution's explicit status boundary is satisfied by a pure internal/status package while the lease-aware writer remains in reconciliation, because moving the writer would invert or duplicate the existing freshness contract -->
<!-- assumed: source ordinals rather than source descriptors are sufficient in condition messages and provide actionable ordering without risking selector, field-path, or observed-value disclosure -->

## Architecture

The implementation has four cooperating parts:

1. `api/v1alpha1` defines the additive public `summary` and `resultHash`
   fields while retaining `observedGeneration`, list-map conditions, and the
   authoritative structural result.
2. The reconciliation pipeline records one terminal status assessment per
   declared source as it performs existing work. It preserves original policy,
   resolution, and read outcomes before they are adapted into source result
   errors, so condition selection never parses public error messages.
3. `internal/status` accepts the terminal assessments and optional structural
   result, normalizes the result, derives summary and hash, aggregates the five
   canonical conditions, and merges them with persisted non-canonical
   conditions using native `metav1.Condition` transition semantics.
4. The existing reconciliation publisher obtains a fresh Kubeseer, verifies
   the evaluation lease, composes the candidate against persisted conditions,
   compares the complete semantic projection, and performs at most one status
   subresource update.

The terminal data flow is:

```text
policy/discovery/selection/extraction/conversion outcomes
                         |
                         v
             reconciliation assessments + result
                         |
                         v
       internal/status Composer (conditions/summary/hash)
                         |
                         v
         fresh-read lease guards + semantic comparison
                         |
                         v
              one status-subresource update
```

An evaluation that produced a result always composes conditions, summary,
hash, and result together. An evaluation that reached a terminal dependency
failure without a result composes conditions with `Ready=False` and
`Degraded=True`, and explicitly omits result, summary, and hash. Failures that
make the resource identity or current generation unknowable, cancellation, or
deletion do not produce a status write.

## Options Considered

### Option A: Pure Status Composer With Existing Lease-Aware Publisher

- Summary: add a dependency-light `internal/status` package for semantic
  composition and retain the fresh-read publisher in reconciliation.
- Why chosen: it gives status one explicit semantic owner, consumes rather
  than repeats terminal outcomes, and reuses the already proved stale-write,
  no-op, and conflict behavior. The package depends only on the public API and
  Kubernetes metadata helpers, so reconciliation can depend on it without a
  cycle.

### Option B: Infer Conditions From Published Result Errors

- Summary: leave the pipeline unchanged and derive all conditions by parsing
  `KubeseerResultError.Reason` and message text after result construction.
- Why rejected: planning currently adapts several discovery and policy
  outcomes into broader selection errors. Parsing would lose distinctions such
  as unavailable versus deterministic resolution failure, couple public
  messages to controller logic, and make first-source reason selection
  brittle.

### Option C: Move Composition, Freshness Tracking, And Writing Together

- Summary: relocate the whole status publisher into the new package.
- Why rejected: the writer consumes reconciliation-specific `Lease` and
  `FreshnessTracker` contracts. Moving it would either create an inverted
  dependency on reconciliation or duplicate proven guards. Keeping a pure
  semantic package and a thin orchestration adapter is the smaller boundary.

### Option D: Reimplement Condition Transition Timestamps

- Summary: inject a clock and manually preserve or replace
  `lastTransitionTime`.
- Why rejected: the pinned Kubernetes `meta.SetStatusCondition` helper already
  implements the required rule: it preserves the timestamp while status is
  unchanged and sets it when status changes. Reimplementation adds clock and
  transition logic without adding contract value.

## Simplicity And Elegance Review

- Simplest viable shape: one additive API struct, one pure status package, one
  typed assessment adapter in the existing pipeline, and a narrow extension
  of the existing publisher. No second phase/state field, persistence model,
  event stream, or independent evaluator is introduced.
- Coupling check: domain packages remain unaware of status. Reconciliation
  translates domain outcomes once; `internal/status` imports only
  `api/v1alpha1`, standard-library hashing/JSON packages, and Kubernetes
  condition helpers. The writer keeps its existing store and tracker
  dependencies.
- Native-semantics check: `meta.SetStatusCondition` is reused for transition
  timestamps, and the CRD continues to use list-map conditions keyed by type.
  Custom code only enforces the feature's canonical order and preservation
  rules.
- Single-source-of-truth check: `status.result` remains authoritative. Summary
  and hash are always recomputed from that result, while Ready/Degraded inspect
  that same snapshot for partial errors.
- Future-proofing: typed assessments include explicit unavailable and
  not-evaluated outcomes so future fail-closed paths can report them without
  changing the public reason taxonomy. Aggregation, events, metrics, limits,
  and authorization audit behavior remain deferred.

## Components And Interfaces

### Public API Status Envelope

- Purpose: expose the complete additive `kubeseer.io/v1alpha1` status contract.
- Shape:

  ```go
  type KubeseerStatus struct {
      ObservedGeneration int64              `json:"observedGeneration,omitempty"`
      Conditions         []metav1.Condition `json:"conditions,omitempty"`
      Summary            *KubeseerSummary   `json:"summary,omitempty"`
      ResultHash         string             `json:"resultHash,omitempty"`
      Result             *KubeseerResult    `json:"result,omitempty"`
  }

  type KubeseerSummary struct {
      SuccessfulSources int64 `json:"successfulSources"`
      FailedSources     int64 `json:"failedSources"`
      MatchedResources  int64 `json:"matchedResources"`
  }
  ```

- Schema controls: all counts have a minimum of zero; `resultHash` is optional
  and constrained to `^sha256:[0-9a-f]{64}$`; conditions retain list-map/type
  validation. Generated deepcopy and CRD artifacts are refreshed.
- Presence: `Summary` is a pointer so absence is represented independently
  from a present all-zero summary. `ResultHash` uses empty-string omission and
  is never set without `Result`.
- Requirements: `R1`, `R2`, `R6`, `R7`, `NFR1`, `NFR6`.

### Terminal Assessment Adapter

- Purpose: preserve condition-relevant meaning at the point where existing
  domain outcomes are available, before broad result-error adaptation.
- Inputs: policy snapshot state, discovery resolution outcomes, policy
  decisions, selection/read outcomes, extraction/typed configuration checks,
  and declared source order already processed by reconciliation.
- Output: one `status.SourceAssessment` per declared source plus an optional
  policy-wide authorization outcome. The assessment contains only internal
  enums and a zero-based declaration index:

  ```go
  type SourceAssessment struct {
      Index         int
      Configuration ConfigurationOutcome
      Authorization AuthorizationOutcome
      Resolution    ResolutionOutcome
  }
  ```

- Configuration outcomes are `accepted` or `invalid`. Authorization outcomes
  are `allowed`, `denied`, `policy-missing`, `policy-invalid`,
  `read-forbidden`, `unavailable`, or `not-evaluated`. Resolution outcomes
  are `resolved`, `failed`, `unavailable`, or `not-evaluated`. These are
  internal closed enums, not new API fields.
- The optional policy-wide outcome carries missing, invalid, or temporarily
  unavailable policy state. It takes precedence because it applies to every
  exact target; otherwise aggregation selects the first non-successful source
  in declaration order.
- Original discovery outcomes are recorded before `mapPlanningError` broadens
  them for source result construction. Original policy decisions are retained
  separately from authorization selection errors. A Kubernetes read-forbidden
  outcome replaces that source's prior allowed assessment; other read
  availability failures leave authorization successful and degrade the result.
- Requirements: `R3`, `R4`, `R8`, `NFR2`, `NFR3`.

### Outcome Classification

The adapter uses stable category rules rather than text matching:

| Existing terminal outcome | Configuration | Authorization | Resolution |
| --- | --- | --- | --- |
| Valid exact source with allowed policy decision | accepted | allowed | resolved |
| Invalid descriptor, namespace/scope, selector, expression, or declared output type | invalid | not-evaluated if no exact decision exists | failed only when resolution itself failed |
| Deterministic unknown, ambiguous, or invalid resource resolution | accepted unless the descriptor itself is invalid | not-evaluated | failed |
| Temporary discovery unavailability before a decision | accepted | unavailable | unavailable |
| Missing installation policy | unchanged per source checks | policy-missing globally | independently preserved if attempted |
| Invalid installation policy | unchanged per source checks | policy-invalid globally | independently preserved if attempted |
| Temporarily unavailable policy state | unchanged per source checks | unavailable globally | independently preserved if attempted |
| Valid policy explicitly denies an exact target | accepted | denied | resolved |
| Kubernetes read returns forbidden after allowance | accepted | read-forbidden | resolved |
| Other read interruption/unavailability after allowance | accepted | allowed | resolved |
| Per-resource extraction or conversion failure | accepted | unchanged | resolved |

`not-evaluated` is used only when an earlier deterministic or fail-closed
outcome actually prevented that dimension from running. It is not substituted
for an available global policy result or an attempted discovery result.

### Status Evaluation Contract

- Purpose: pass all information needed for deterministic composition without
  passing clients or domain services into `internal/status`.
- Shape:

  ```go
  type Evaluation struct {
      Result              *v1alpha1.KubeseerResult
      Sources             []SourceAssessment
      GlobalAuthorization *AuthorizationOutcome
      ResultUnavailable   bool
  }
  ```

- Invariants: `Result` and `ResultUnavailable` are mutually exclusive; a
  terminal zero-source evaluation supplies a non-nil empty result and no source
  assessments; source indices are unique and contiguous; assessment enums are
  valid. Violations are internal build failures and are returned to
  reconciliation rather than published as malformed status.
- A terminal result-construction/dependency failure sets
  `ResultUnavailable=true`. The available configuration, authorization, and
  resolution assessments are retained, while Ready and Degraded report
  `EvaluationUnavailable`.
- Requirements: `R1`, `R3`, `R4`, `R5`, `NFR4`.

### Result Normalizer, Summary Builder, And Hasher

- Purpose: establish one semantic result projection used by storage,
  comparison, summary, and hashing.
- Normalization: deep-copy the result and canonicalize API-equivalent nil and
  empty slices at every result collection boundary. Ordered source, resource,
  field, and field-error slices are never sorted. Existing typed object/list
  payload strings are already canonical JSON from typed-output construction
  and are not decoded or rewritten.
- Summary: count source state `values` as successful, source state `error` as
  failed, and every resource entry across all sources as matched. A values
  source with field errors remains successful. Checked integer conversion
  prevents overflow into the public `int64` fields.
- Hash: JSON-encode only the normalized `KubeseerResult`, compute SHA-256 over
  those bytes, and encode `sha256:` plus lowercase hexadecimal. Generation,
  conditions, summary serialization, and all other status fields are excluded.
- Absence: nil result returns nil summary and empty hash. The result remains
  authoritative; persisted derived-field drift is repaired from the candidate
  result.
- Requirements: `R5`, `R6`, `R7`, `R9`, `NFR2`, `NFR5`.

### Condition Composer

- Purpose: aggregate typed outcomes into exactly one canonical condition of
  each type and apply Kubernetes transition semantics.
- Inputs: current generation, terminal `Evaluation`, derived summary, and a
  deep copy of persisted conditions from the fresh read.
- Aggregation:

  | Condition | Success | Deterministic failure | Unknown/unavailable |
  | --- | --- | --- | --- |
  | Accepted | `True/ConfigurationAccepted` | `False/InvalidConfiguration` from first invalid source | not applicable |
  | Authorized | `True/AuthorizationSucceeded` | `False/AuthorizationDenied`, `PolicyMissing`, `PolicyInvalid`, or `ReadForbidden` | `Unknown/AuthorizationUnavailable` or `AuthorizationNotEvaluated` |
  | SourcesResolved | `True/ResolutionSucceeded` | `False/ResolutionFailed` | `Unknown/ResolutionUnavailable` or `ResolutionNotEvaluated` |
  | Ready | `True/EvaluationSucceeded` | `False/EvaluationDegraded` when result has errors | `False/EvaluationUnavailable` without a result |
  | Degraded | `False/EvaluationSucceeded` | `True/EvaluationDegraded` when result has errors | `True/EvaluationUnavailable` without a result |

- Result error detection examines source state `error`, source field errors,
  and resource field state `error`. It does not infer degradation from free-form
  messages. Ready and Degraded are emitted by one branch so they cannot both be
  true.
- Zero sources aggregate to accepted, authorized, and resolved success. Their
  present empty result has a zero summary and produces Ready true/Degraded
  false.
- Messages use fixed templates containing the current generation, outcome,
  safe counts derived from the same result, and at most the first affected
  source ordinal. They never interpolate descriptors, selectors, namespaces,
  field paths, observed objects, extracted values, typed values, or Secret
  data.
- Merge algorithm: start from persisted conditions, update each canonical type
  with `meta.SetStatusCondition`, then emit canonical conditions in the fixed
  order Accepted, Authorized, SourcesResolved, Ready, Degraded. Persisted
  non-canonical conditions are deep-copied afterward with field values and
  relative order unchanged. Existing duplicate canonical entries, if any, are
  repaired to one entry.
- `meta.SetStatusCondition` supplies a new transition time only for a new
  condition or condition-status transition. Generation, reason, or message
  changes with stable status preserve the persisted transition time.
- Requirements: `R2`, `R3`, `R4`, `R5`, `R8`, `NFR1`, `NFR2`, `NFR3`.

### Complete Semantic Projection

- Purpose: decide whether a status-subresource write is observable and needed.
- Projection fields: top-level observed generation; all condition fields;
  summary presence and counts; result hash presence and value; normalized
  structural result presence and value.
- Before comparison, canonical condition types are placed in fixed order and
  result collections are normalized. Canonical condition list-order-only
  differences and nil-versus-empty result collection differences therefore
  compare equal. Duplicate or missing canonical conditions compare different
  from the valid candidate and are repaired.
- Non-canonical conditions participate in the projection because they are
  preserved by composition; the feature does not silently erase or overwrite
  another controller's public status metadata.
- Transition times participate in comparison after composition. Stable-status
  recurrence reuses the persisted time, while a real status transition gets a
  new time and therefore remains an intentional semantic change.
- Requirements: `R2`, `R7`, `R9`, `NFR5`.

### Lease-Aware Status Publisher

- Purpose: publish the composed snapshot through the genuine status
  subresource without weakening current-generation guarantees.
- Interface change: `StatusPublisherPort.Publish` accepts the evaluation lease
  and `status.Evaluation` instead of only `KubeseerResult`.
- Flow:
  1. honor context cancellation;
  2. direct-read the current Kubeseer;
  3. verify UID, generation, freshness tracker, and deletion timestamp;
  4. compose the candidate using the freshly persisted conditions;
  5. compare complete semantic projections;
  6. if changed, assign the complete candidate status and call one status
     subresource update.
- The fresh object's spec is never changed. A no-op candidate issues no write.
  A conflict or publication failure returns the existing retryable runtime
  outcome, and the current reconciliation makes no second publication attempt.
- If terminal result construction fails after a valid lease and assessments
  exist, reconciliation first attempts the result-absent unavailable snapshot,
  then returns the retryable evaluation error. If status publication also
  fails, the publication failure remains retryable and no malformed partial
  candidate is issued.
- Requirements: `R1`, `R5`, `R9`, `NFR4`, `NFR5`.

## Data Models

`KubeseerSummary` is an inline status value behind an optional pointer. It has
no independent lifecycle and is always replaced atomically with the result.
All three counts use `int64`, match Kubernetes JSON integer conventions, and
are schema-constrained to non-negative values.

`ResultHash` is a derived opaque string. Its algorithm prefix allows consumers
to identify SHA-256 without making the raw digest ambiguous. The implementation
does not accept persisted hash as input to candidate derivation and never uses
it as a substitute for comparing or publishing `Result`.

The internal assessment enums are deliberately not aliases for public reason
strings. A single mapping table in `internal/status` owns public condition
status/reason selection. This keeps domain classification exhaustive at
compile time and prevents accidental publication of internal module reason
codes.

No durable model, cache, history object, timestamp field, or secondary API
resource is introduced.

## Error Handling

- Invalid internal assessment combinations, non-contiguous source indices,
  summary overflow, or result serialization failure abort composition and
  return the runtime's retryable build-failure classification. They are not
  turned into an invalid public snapshot.
- Policy missing/invalid, explicit denial, deterministic resolution failure,
  configuration failure, read forbidden, and per-source/per-field failures are
  terminal public outcomes. They produce stable conditions and retain existing
  sanitized structural result errors.
- Temporary policy/discovery/read failures remain represented through existing
  retryability and source errors. Their condition dimension is Unknown only
  where the decision itself is unavailable; an authorized read that later
  fails for availability remains Authorized true.
- A dependency failure that prevents result construction produces an
  unavailable result-absent snapshot when identity and generation remain
  publishable, then requests a retry.
- Fresh-read failure, context cancellation, UID mismatch, generation mismatch,
  stale tracker identity, or deletion suppresses publication exactly as in the
  existing runtime.
- Status conflicts and API write failures request a rate-limited retry. There
  is no in-reconciliation write retry and therefore no second publication.

## Security Considerations

Condition rendering uses closed reason constants and fixed message templates.
The only dynamic inputs allowed in messages are generation, result-derived
counts, and a source ordinal. Raw API objects, source descriptor details,
selector values, namespaces, field paths, extraction/conversion values, and
Secret data never enter the composer.

The structural result continues to use the approved typed-output and sanitized
error contracts. This feature neither expands which observed values may be
published nor adds logs/events containing those values. Hash input is the
already approved structural result, and only the digest is added to status.

The writer continues to use the status subresource, fresh identity checks, and
the controller's existing credentials. It does not introduce whole-object
writes, impersonation, SubjectAccessReview, or new RBAC behavior.

## Failure Modes And Tradeoffs

- Failure mode: a domain error is adapted before its condition meaning is
  recorded. Mitigation: capture typed resolution, policy, and read assessments
  at their current module boundaries and test every mapping category. Tradeoff:
  `plannedSource` carries additional status metadata.
- Failure mode: independently derived summary/hash diverge from result.
  Mitigation: derive all three in one composer from one normalized result and
  include derived fields in semantic comparison. Tradeoff: hashing repeats on
  each terminal reconciliation, accepted because performance targets are out
  of scope and correctness is the current priority.
- Failure mode: condition updates churn transition time. Mitigation: merge
  against freshly persisted conditions with `meta.SetStatusCondition` and
  prove stable-status/no-op behavior through envtest. Tradeoff: generation,
  reason, or message can change while transition time remains stable, matching
  Kubernetes status-transition semantics.
- Failure mode: another actor's condition is removed. Mitigation: retain every
  non-canonical condition unchanged and preserve its relative order. Tradeoff:
  canonical conditions are always serialized before non-canonical conditions,
  which is safe for list-map semantics.
- Failure mode: a stale result remains visible after result construction becomes
  unavailable. Mitigation: the unavailable candidate explicitly clears result,
  summary, and hash in the same status write. Tradeoff: consumers lose the old
  snapshot but never mistake it for the current observed generation.
- Failure mode: API conflict races a newer reconciliation. Mitigation: make one
  status attempt and rate-limit retry through the manager. Tradeoff: status may
  be temporarily delayed rather than overwritten from stale state.
- Failure mode: source order changes condition precedence or result hash.
  Mitigation: declaration order is intentionally authoritative and tested.
  Tradeoff: reordering sources is observable even when their unordered set is
  unchanged.

## Testing Strategy

The repository constitution excludes a new package-local unit-test layer. All
automated behavior is added to the existing genuine API, cross-module
integration, and envtest suites.

### API Contract Suite

Extend `api/v1alpha1/TestAPIContract` to prove JSON round trips, deepcopy
independence, list-map condition markers, optional summary/hash presence,
non-negative summary schema constraints, hash pattern validation, and generated
CRD item schemas. The generated CRD is inspected for the nested summary fields,
condition list-map key, and result hash pattern.

### Cross-Module Integration Suite

Extend `test/integration/TestModuleIntegration` and its real module fixtures to
exercise the pipeline-to-assessment-to-composer boundary. Scenarios cover:

- zero sources and complete success;
- partial source and field failures with successful sibling retention;
- invalid configuration, deterministic resolution failure, policy missing,
  policy invalid, explicit denial, read forbidden, and unavailable/not-evaluated
  outcomes;
- first-source declaration-order precedence for differing failures;
- exact summary counts, values-with-field-errors counting, and absent result;
- stable hash, ordered-result hash changes, and nil-versus-empty normalization;
- exactly five canonical conditions, fixed order, reason allowlist, safe
  messages, Ready/Degraded exclusion, non-canonical preservation, and no
  duplicate canonical type;
- stable `lastTransitionTime` when status is unchanged and changed time only
  when condition status transitions.

Tests assert complete typed outputs and status projections, not only absence of
errors. Fixtures use synthesized non-sensitive values and explicit checks that
forbidden descriptors, selectors, paths, values, and Secret payloads do not
occur in condition messages.

### Envtest Status-Subresource Suite

Extend `test/envtest/TestEnvtestReconciliationRuntime` so the real manager,
cache, API server, generated CRD, and status subresource prove:

- atomic publication of observed generation, conditions, summary, hash, and
  result while spec remains unchanged;
- no status write/resource-version churn for an equivalent evaluation;
- condition transition-time preservation and real status-transition updates;
- generation/UID/deletion/cancellation stale-write suppression;
- preservation of an injected non-canonical condition;
- clearing stale result/summary/hash for a current unavailable evaluation;
- conflict-to-rate-limited-retry behavior and at most one status update attempt.

The status suite runs against the repository's minimum and previous-supported
Kubernetes minors, currently 1.35.6 and 1.36.2, using isolated envtest assets
and no ambient kubeconfig.

## Verification Plan

- Requirement proof: trace every requirement row below to API assertions,
  cross-module terminal scenarios, or genuine status-subresource behavior.
- Generated artifacts: run `make generate`, `make manifests`, and inspect the
  generated deepcopy and CRD schema for the additive status fields and nested
  validation.
- Static/project checks: run `make verify`, including formatting, generation
  freshness, import boundaries, contract checks, and Walden validation.
- Cross-module evidence: run `make test` and confirm the expanded
  `TestModuleIntegration` scenarios pass without package-local test seams.
- API evidence: run `make test-api K8S_VERSION=1.35.6` and
  `make test-api K8S_VERSION=1.36.2` with local envtest assets; record the
  manager/status-subresource scenario names and outcomes.
- No-op evidence: in envtest, capture resourceVersion and canonical transition
  times after first publication, reconcile an equivalent evaluation, and prove
  both remain unchanged.
- Confidentiality evidence: assert condition reasons are allowlisted and
  messages exclude every supplied sentinel descriptor, selector, field path,
  observed value, typed value, and Secret payload.
- Operational evidence: no new metrics, events, dashboards, or alert contracts
  are introduced; reconciliation's existing retry result is the operational
  signal for publication conflict/unavailability.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Public API envelope, evaluation contract, and lease-aware publisher atomically compose and publish one guarded current-generation snapshot. |
| `R2` | Condition composer, native transition helper, canonical ordering, non-canonical preservation, API contract tests, and envtest transition proofs. |
| `R3` | Typed configuration assessments and declaration-order aggregation in the condition composer. |
| `R4` | Preserved policy/discovery/read outcomes, outcome classification table, and deterministic authorization/resolution aggregation. |
| `R5` | Result error inspection, unavailable evaluation mode, mutually exclusive Ready/Degraded branch, and zero-source scenarios. |
| `R6` | Optional `KubeseerSummary`, normalized-result summary builder, schema constraints, and exact-count integration tests. |
| `R7` | Normalized-result SHA-256 hasher, authoritative result rule, schema pattern, and hash stability/order tests. |
| `R8` | Closed reason mappings, fixed safe message templates, existing partial result contract, and confidentiality sentinel assertions. |
| `R9` | Complete semantic projection, canonical condition/result normalization, one-attempt publisher, no-op envtest, and conflict retry proof. |
| `NFR1` | `metav1.Condition`, list-map schema, `meta.SetStatusCondition`, generated CRD checks, and real API-server evidence. |
| `NFR2` | Closed enums, first-source ordering, fixed condition ordering/templates, result normalization, and deterministic hash/count tests. |
| `NFR3` | Safe message input allowlist, approved structural result boundary, and sentinel-based confidentiality assertions. |
| `NFR4` | Existing lease, direct-read, UID/generation/tracker/deletion/context guards plus status-subresource conflict handling. |
| `NFR5` | Persisted-condition merge, complete semantic no-op comparison, and resourceVersion/transition-time envtest evidence. |
| `NFR6` | Additive v1alpha1 fields only; approved result and partial-error models remain unchanged and authoritative. |
| `NFR7` | Existing API contract, `TestModuleIntegration`, and `TestEnvtestReconciliationRuntime` suites provide deterministic genuine-boundary proof. |
