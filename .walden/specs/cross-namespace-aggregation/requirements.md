---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T08:45:18Z
last_modified: 2026-08-31T08:45:18Z
approved_fingerprint: sha256:e186b059871840b5a4aad9bc0472967c52ee01bca4c087451be456b2ac84f58b
---

# Requirements Document

## Introduction

This feature defines deterministic grouping and typed aggregation over the
accepted resource outcomes produced by `value-operators`. Each source may
declare named aggregations that collect or reduce one typed field, optionally
partitioned by one or more other typed fields, across all accepted resources
and authorized namespaces selected for that source.

Aggregation is a pure processing boundary. It performs no discovery,
authorization, Kubernetes reads, extraction, operator evaluation, or status
writes. It preserves the approved source and resource failure boundaries:
resource-local failures may yield a degraded aggregate beside successful
contributions, while an upstream source failure remains atomic and contributes
no values. Every contribution retains complete Kubernetes provenance
internally; public contributor provenance is emitted only when requested.

<!-- assumed: aggregations are declared per source because the approved source boundary owns one resource type, namespace set, field declarations, and ordered operator outcome (source: approved resource-selection R1-R6, typed-output-model R7, and value-operators R7) -->
<!-- assumed: aggregate declarations are named list-map entries and are evaluated in lexicographic name order, matching the stable-identifier and deterministic-order conventions used by sources and fields (source: .walden/constitution.md and approved field-extraction R1, R5) -->
<!-- assumed: the initial feature aggregates one declared field per aggregation; joins, expressions, and cross-source reductions are deferred because they require a new data-composition language (source: SPECIFICATIONS.md aggregation objective and .walden/constitution.md explicit boundary rules) -->
<!-- assumed: resource-local aggregation failures degrade only affected aggregate outcomes, while an unavailable namespace remains an atomic upstream source failure because approved resource-selection R6 discards partial values from a failed multi-target source (source: approved resource-selection R6 and reconciliation-runtime R5) -->
<!-- assumed: implementation-owned aggregate cardinality ceilings fail closed without truncation; the later performance-and-limits feature may centralize or revise their numeric values without changing this observable behavior (source: SPECIFICATIONS.md cross-namespace maximum-cardinality scope and performance-and-limits dependency) -->
<!-- assumed: aggregate outcomes are additive beside existing per-resource results rather than replacing them because the approved public status summary and semantic-result contracts already treat resource results as authoritative inputs (source: approved status-and-conditions R6, R7 and reconciliation-runtime R6) -->
<!-- assumed: average precision means fractional decimal places, defaults to 6, and uses half-even rounding by default because this gives a bounded familiar representation while minimizing repeated midpoint bias (source: user decision during cross-namespace-aggregation Design checkpoint) -->

## Requirements

### R1 Per-Source Aggregate Declarations

**User Story:** As a Kubeseer user, I want to declare named aggregations on a source, so that values selected from multiple resources and namespaces can be summarized explicitly.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL expose `aggregations` as an optional list-map on every Kubeseer source declaration.
2. `R1.AC2` WHEN an aggregation is declared, the system SHALL require its unique lower-camel-case `name` property.
3. `R1.AC3` WHEN an aggregation is declared, the system SHALL require its `function` property.
4. `R1.AC4` WHEN an aggregation is declared, the system SHALL require its `field` property to reference one field of the containing source.
5. `R1.AC5` The system SHALL define the supported aggregation functions as `collect`, `count`, `sum`, `min`, `max`, `average`, `first`, `last`, and `distinct`.
6. `R1.AC6` The system SHALL allow an aggregation to declare an optional ordered `groupBy` list of field names from the containing source.
7. `R1.AC7` The system SHALL allow an aggregation to declare an optional `includeProvenance` boolean.
8. `R1.AC8` The system SHALL declare no API-server default for omitted aggregations or `includeProvenance`.
9. `R1.AC9` WHEN a source omits `aggregations`, the system SHALL preserve its accepted operator resource outcomes without aggregate outcomes.
10. `R1.AC10` WHEN a source declares an explicit empty `aggregations` list, the system SHALL preserve its accepted operator resource outcomes without aggregate outcomes.
11. `R1.AC11` IF a manifest supplies an aggregation function outside the supported set, THEN the system SHALL reject the manifest without persisting it.
12. `R1.AC12` WHEN valid aggregation declarations are serialized through the `v1alpha1` API, the system SHALL preserve every declaration property.
13. `R1.AC13` WHEN a Kubeseer resource containing aggregation declarations is deep-copied, the system SHALL prevent mutations to the copy from changing the original declarations.
14. `R1.AC14` The system SHALL represent aggregation declarations through a structural Kubernetes OpenAPI schema without an opaque preserve-unknown region.
15. `R1.AC15` The system SHALL allow an `average` aggregation to declare an optional integer `precision` measured in fractional decimal places.
16. `R1.AC16` The system SHALL allow an `average` aggregation to declare an optional `roundingMode`.
17. `R1.AC17` The system SHALL define the supported rounding modes as `halfEven`, `halfAwayFromZero`, `towardZero`, and `awayFromZero`.
18. `R1.AC18` WHEN an `average` aggregation omits `precision`, the system SHALL default `precision` to 6 fractional decimal places.
19. `R1.AC19` WHEN an `average` aggregation omits `roundingMode`, the system SHALL default `roundingMode` to `halfEven`.
20. `R1.AC20` IF a manifest supplies `precision` outside the inclusive range from 0 through 18, THEN the system SHALL reject the manifest without persisting it.

### R2 Aggregate Planning And Validation

**User Story:** As a platform operator, I want aggregation configuration validated before resource outcomes are reduced, so that invalid declarations cannot produce data-dependent partial aggregates.

#### Acceptance Criteria

1. `R2.AC1` WHEN aggregation is prepared for a source, the system SHALL inspect every aggregate declaration before evaluating any resource outcome for that source.
2. `R2.AC2` WHEN every aggregate declaration for a source is valid, the system SHALL produce an immutable aggregate plan keyed by the source identifier.
3. `R2.AC3` WHEN one source declares multiple aggregations, the system SHALL order their plans lexicographically by aggregate name.
4. `R2.AC4` WHEN one aggregation declares multiple grouping fields, the system SHALL preserve their declaration order in its plan.
5. `R2.AC5` WHEN equivalent source declarations are prepared, the system SHALL produce semantically equivalent aggregate plans.
6. `R2.AC6` IF an aggregate name duplicates another aggregate name in the same source, THEN the system SHALL reject the manifest without persisting it.
7. `R2.AC7` IF an aggregate references an undeclared target field, THEN the system SHALL fail planning for that source.
8. `R2.AC8` IF an aggregate references an undeclared grouping field, THEN the system SHALL fail planning for that source.
9. `R2.AC9` IF an aggregate repeats a field in `groupBy`, THEN the system SHALL fail planning for that source.
10. `R2.AC10` IF a grouping field has an unsupported logical type, THEN the system SHALL fail planning for that aggregate.
11. `R2.AC11` IF an aggregation function is incompatible with its target field type, THEN the system SHALL fail planning for that aggregate.
12. `R2.AC12` WHEN one aggregate declaration fails planning, the system SHALL preserve independent plans or failures for every other aggregate in the source.
13. `R2.AC13` WHEN aggregate planning fails, the system SHALL identify the source identifier, aggregate name, function, and relevant field name.
14. `R2.AC14` WHEN aggregate planning fails, the system SHALL return a stable reason code.
15. `R2.AC15` IF an aggregate-plan cache is absent, stale, or unavailable, THEN the system SHALL prepare a valid plan from the current declarations without changing aggregation semantics.
16. `R2.AC16` IF a function other than `average` declares `precision` or `roundingMode`, THEN the system SHALL fail planning for that aggregate.

### R3 Group Formation And Typed Keys

**User Story:** As a Kubeseer user, I want resources grouped by explicit typed fields, so that equal-looking values with different logical meanings cannot collide.

#### Acceptance Criteria

1. `R3.AC1` WHEN an aggregation omits `groupBy`, the system SHALL evaluate one global group for that aggregation.
2. `R3.AC2` WHEN an aggregation declares `groupBy`, the system SHALL derive one ordered key component from each declared grouping field for every accepted resource.
3. `R3.AC3` The system SHALL allow `string`, `integer`, `number`, `boolean`, `timestamp`, `duration`, and `quantity` fields as grouping keys.
4. `R3.AC4` WHEN grouping keys are compared, the system SHALL include each component's logical type in equality semantics.
5. `R3.AC5` WHEN number grouping keys are compared, the system SHALL use their exact normalized decimal values.
6. `R3.AC6` WHEN timestamp grouping keys are compared, the system SHALL use their represented instants independently of original offsets.
7. `R3.AC7` WHEN duration grouping keys are compared, the system SHALL use their exact normalized nanosecond magnitudes.
8. `R3.AC8` WHEN quantity grouping keys are compared, the system SHALL use their exact normalized Kubernetes quantity magnitudes independently of display units.
9. `R3.AC9` WHEN string grouping keys are compared, the system SHALL use case-sensitive exact string equality.
10. `R3.AC10` IF a grouping field is absent for an accepted resource, THEN the system SHALL produce a resource-scoped grouping failure for that aggregate.
11. `R3.AC11` IF a grouping field contains a null match for an accepted resource, THEN the system SHALL produce a resource-scoped grouping failure for that aggregate.
12. `R3.AC12` IF a grouping field contains a match count other than one for an accepted resource, THEN the system SHALL produce a resource-scoped grouping failure for that aggregate.
13. `R3.AC13` WHEN two resources have equal complete typed group keys, the system SHALL place their target-field contributions in the same group.
14. `R3.AC14` WHEN two resources differ in any typed group-key component, the system SHALL place their target-field contributions in different groups.

### R4 Function Compatibility And Contribution Semantics

**User Story:** As a Kubeseer user, I want each aggregation function to have explicit type and cardinality semantics, so that results remain predictable for every typed field.

#### Acceptance Criteria

1. `R4.AC1` The system SHALL allow `collect`, `count`, `first`, `last`, and `distinct` for every supported field type.
2. `R4.AC2` The system SHALL allow `sum` for `integer`, `number`, `duration`, and `quantity` fields.
3. `R4.AC3` The system SHALL allow `average` for `integer` and `number` fields.
4. `R4.AC4` The system SHALL allow `min` and `max` for `integer`, `number`, `timestamp`, `duration`, and `quantity` fields.
5. `R4.AC5` WHEN an accepted resource target field contains non-null matches, the system SHALL contribute every non-null match in typed match order.
6. `R4.AC6` WHEN an accepted resource target field is absent, the system SHALL contribute no value for that resource.
7. `R4.AC7` WHEN an accepted resource target field contains only null matches, the system SHALL contribute no value for that resource.
8. `R4.AC8` WHEN `collect` evaluates a group, the system SHALL return all contributions in deterministic contribution order.
9. `R4.AC9` WHEN `count` evaluates a group, the system SHALL return the exact number of non-null contributions as an integer.
10. `R4.AC10` WHEN `sum` evaluates integer contributions, the system SHALL return their exact signed 64-bit sum.
11. `R4.AC11` IF an integer sum overflows the signed 64-bit range, THEN the system SHALL produce an aggregate-scoped overflow failure.
12. `R4.AC12` WHEN `sum` evaluates number contributions, the system SHALL return their exact normalized decimal sum without floating-point rounding.
13. `R4.AC13` WHEN `sum` evaluates duration contributions, the system SHALL return their exact normalized nanosecond sum.
14. `R4.AC14` WHEN `sum` evaluates quantity contributions, the system SHALL return their exact normalized Kubernetes quantity sum.
15. `R4.AC15` WHEN `average` evaluates integer or number contributions, the system SHALL return their normalized decimal arithmetic mean rounded once to the configured fractional precision.
16. `R4.AC16` WHEN `min` evaluates a non-empty group, the system SHALL return its least contribution according to the target logical type.
17. `R4.AC17` WHEN `max` evaluates a non-empty group, the system SHALL return its greatest contribution according to the target logical type.
18. `R4.AC18` WHEN `first` evaluates a non-empty group, the system SHALL return its first contribution in deterministic contribution order.
19. `R4.AC19` WHEN `last` evaluates a non-empty group, the system SHALL return its last contribution in deterministic contribution order.
20. `R4.AC20` WHEN `distinct` evaluates a group, the system SHALL return the first occurrence of every distinct typed value in deterministic contribution order.
21. `R4.AC21` WHEN `collect` or `distinct` evaluates a group with no contributions, the system SHALL return a successful empty value collection.
22. `R4.AC22` WHEN `count` evaluates a group with no contributions, the system SHALL return integer zero.
23. `R4.AC23` WHEN `sum` evaluates a group with no contributions, the system SHALL return the exact zero value of its declared output type.
24. `R4.AC24` WHEN `min`, `max`, `average`, `first`, or `last` evaluates a group with no contributions, the system SHALL return a successful absent aggregate value.
25. `R4.AC25` WHEN `average` computes its pre-rounding result, the system SHALL use the exact rational quotient of the exact contribution sum and contribution count.
26. `R4.AC26` WHEN `halfEven` rounds an exact midpoint, the system SHALL choose the nearest result whose last retained decimal digit is even.
27. `R4.AC27` WHEN `halfAwayFromZero` rounds an exact midpoint, the system SHALL choose the nearest result with greater absolute magnitude.
28. `R4.AC28` WHEN `towardZero` rounds a result with discarded non-zero digits, the system SHALL choose the representable result with smaller absolute magnitude.
29. `R4.AC29` WHEN `awayFromZero` rounds a result with discarded non-zero digits, the system SHALL choose the representable result with greater absolute magnitude.
30. `R4.AC30` WHEN a rounded average is returned, the system SHALL remove insignificant trailing fractional zeros from its normalized decimal representation.

### R5 Aggregate Outcomes And Provenance

**User Story:** As a Kubeseer consumer, I want aggregate values and their contributors represented structurally, so that results can be consumed without losing resource identity.

#### Acceptance Criteria

1. `R5.AC1` WHEN a successful operator source outcome is aggregated, the system SHALL produce one aggregate source outcome with the same source identifier.
2. `R5.AC2` WHEN an aggregate declaration is evaluated, the system SHALL produce exactly one successful, degraded, or unsuccessful aggregate outcome.
3. `R5.AC3` WHEN a successful aggregate contains groups, the system SHALL represent every group with its ordered typed key components.
4. `R5.AC4` WHEN a successful aggregate group is returned, the system SHALL represent its result with an explicit logical type and value state.
5. `R5.AC5` WHEN a resource value contributes to an aggregate, the system SHALL associate that contribution internally with API version, Kind, namespace, name, and UID provenance.
6. `R5.AC6` WHEN `includeProvenance` is omitted or false, the system SHALL omit public contributor provenance from that aggregate outcome.
7. `R5.AC7` WHEN `includeProvenance` is true, the system SHALL expose complete provenance for every resource contributing to that aggregate group.
8. `R5.AC8` WHEN `includeProvenance` is true for `collect`, the system SHALL associate each collected value with the provenance of its contributing resource.
9. `R5.AC9` WHEN `includeProvenance` is true for `distinct`, the system SHALL associate each distinct value with every resource provenance that contributed that value.
10. `R5.AC10` WHEN `includeProvenance` is true for a reducing function, the system SHALL expose each contributing resource provenance once per aggregate group.
11. `R5.AC11` WHEN equal resource names occur in different namespaces, the system SHALL keep their contributions unambiguous through complete provenance.
12. `R5.AC12` WHEN a public aggregate outcome is serialized and deserialized, the system SHALL preserve typed keys, typed values, value states, ordering, failures, and requested provenance.
13. `R5.AC13` WHEN a public aggregate outcome is deep-copied, the system SHALL prevent mutations to the copy from changing the original outcome.
14. `R5.AC14` The system SHALL represent public aggregate outcomes through a structural Kubernetes OpenAPI schema without an opaque preserve-unknown region.

### R6 Partial Results And Failure Isolation

**User Story:** As a Kubeseer consumer, I want aggregate failures isolated at the narrowest safe boundary, so that useful authorized values remain visible without publishing incomplete source reads as complete data.

#### Acceptance Criteria

1. `R6.AC1` IF an operator source outcome is unsuccessful, THEN the system SHALL perform no aggregation for that source.
2. `R6.AC2` WHEN an unsuccessful operator source outcome is carried into aggregation, the system SHALL preserve its source identifier and upstream failure unchanged.
3. `R6.AC3` IF an operator resource outcome is unsuccessful, THEN the system SHALL omit that resource from aggregate contributions.
4. `R6.AC4` WHEN an unsuccessful resource is omitted from aggregate contributions, the system SHALL preserve its sanitized upstream failure in the aggregate source outcome.
5. `R6.AC5` WHEN one resource has a grouping failure for an aggregate, the system SHALL preserve contributions from independent successful resources for that aggregate.
6. `R6.AC6` WHEN one resource has a target-field failure for an aggregate, the system SHALL preserve contributions from independent successful resources for that aggregate.
7. `R6.AC7` WHEN an aggregate contains any resource-scoped failure beside a computed result, the system SHALL mark that aggregate outcome as degraded.
8. `R6.AC8` WHEN one aggregate declaration or evaluation fails, the system SHALL preserve independent outcomes for every other aggregate in the source.
9. `R6.AC9` WHEN one source aggregation fails, the system SHALL preserve independent outcomes for every other source.
10. `R6.AC10` IF any namespace read for a multi-namespace source is temporarily unavailable, THEN the system SHALL preserve the atomic unsuccessful source outcome produced by resource selection.
11. `R6.AC11` IF any namespace read for a multi-namespace source is temporarily unavailable, THEN the system SHALL publish no aggregate value from temporary partial values of that source execution.
12. `R6.AC12` WHEN a source failure is preserved, the system SHALL allow successful sibling source aggregates to remain in the candidate result.
13. `R6.AC13` IF the aggregation context is canceled or reaches its deadline, THEN the system SHALL return an aggregation-interrupted failure for the affected source.
14. `R6.AC14` WHEN cancellation prevents aggregation of a later source, the system SHALL return an aggregation-interrupted outcome for that unstarted source.
15. `R6.AC15` WHEN cancellation occurs during batch aggregation, the system SHALL preserve every source outcome completed before cancellation.
16. `R6.AC16` WHEN an aggregation failure is reported, the system SHALL omit extracted values, operator operands, field paths, and resource contents from its diagnostic.
17. `R6.AC17` WHEN equivalent aggregation failures occur for equivalent inputs, the system SHALL return the same stable reason code.

### R7 Deduplication And Deterministic Ordering

**User Story:** As an API consumer, I want stable aggregate ordering and deduplication, so that equivalent cluster state produces equivalent status independently of processing timing.

#### Acceptance Criteria

1. `R7.AC1` WHEN multiple source outcomes are aggregated, the system SHALL preserve one aggregate source outcome for each input source in input order.
2. `R7.AC2` WHEN one source contains multiple aggregate outcomes, the system SHALL order them lexicographically by aggregate name.
3. `R7.AC3` WHEN one aggregate contains multiple groups, the system SHALL order them lexicographically by canonical typed key tuple.
4. `R7.AC4` WHEN contributions are ordered within a group, the system SHALL use resource namespace, name, UID, and typed match index as the ordering key.
5. `R7.AC5` WHEN a cluster-scoped resource contributes, the system SHALL use an empty namespace component in its deterministic ordering key.
6. `R7.AC6` WHEN `distinct` compares values, the system SHALL include logical type and normalized typed payload in equality semantics.
7. `R7.AC7` WHEN equivalent typed values use different timestamp offsets or quantity display units, the system SHALL treat them as one distinct value.
8. `R7.AC8` WHEN the same object UID is present more than once in one operator source outcome, the system SHALL count its contributions only once.
9. `R7.AC9` WHEN equal resource names occur in different namespaces, the system SHALL treat their distinct UIDs as independent contributors.
10. `R7.AC10` WHEN processing order varies for equivalent aggregate plans and operator outcomes, the system SHALL produce semantically equivalent aggregate outcomes.
11. `R7.AC11` WHEN equivalent aggregate outcomes are serialized repeatedly, the system SHALL produce stable collection ordering.

### R8 Aggregate Cardinality Limits

**User Story:** As a platform operator, I want bounded aggregate output, so that cross-namespace fan-out cannot create unbounded memory or status growth.

#### Acceptance Criteria

1. `R8.AC1` The system SHALL enforce centrally declared positive ceilings for groups, contributions, collected values, distinct values, and emitted provenance entries.
2. `R8.AC2` WHEN aggregate planning succeeds, the system SHALL bind the current cardinality ceilings into the immutable aggregate plan.
3. `R8.AC3` IF forming another group would exceed the group ceiling, THEN the system SHALL fail that aggregate with a cardinality-exceeded reason.
4. `R8.AC4` IF accepting another contribution would exceed the contribution ceiling, THEN the system SHALL fail that aggregate with a cardinality-exceeded reason.
5. `R8.AC5` IF emitting another collected or distinct value would exceed its output ceiling, THEN the system SHALL fail that aggregate with a cardinality-exceeded reason.
6. `R8.AC6` IF emitting requested contributor provenance would exceed its ceiling, THEN the system SHALL fail that aggregate with a cardinality-exceeded reason.
7. `R8.AC7` WHEN an aggregate exceeds a cardinality ceiling, the system SHALL publish no truncated result for that aggregate.
8. `R8.AC8` WHEN one aggregate exceeds a cardinality ceiling, the system SHALL preserve independent aggregate and source outcomes.
9. `R8.AC9` WHEN equivalent inputs exceed a cardinality ceiling, the system SHALL report the same stable failure boundary independently of processing timing.

### R9 Processing Boundary And Result Integration

**User Story:** As a reconciliation component, I want aggregation to compose with existing operator and status contracts, so that enabling aggregation does not bypass security or alter unrelated source behavior.

#### Acceptance Criteria

1. `R9.AC1` The system SHALL consume only immutable aggregate plans and operator outcomes as aggregation input.
2. `R9.AC2` WHILE aggregate planning or evaluation is active, the system SHALL perform no Kubernetes API read.
3. `R9.AC3` WHEN a source has no aggregation declarations, the system SHALL preserve its existing public resource results unchanged.
4. `R9.AC4` WHEN a source has aggregation declarations, the system SHALL preserve its existing public resource outcomes beside its aggregate outcomes.
5. `R9.AC5` WHEN aggregate outcomes change semantically, the system SHALL make that change observable to the existing semantic result comparison boundary.
6. `R9.AC6` WHEN aggregate outcomes are semantically equivalent, the system SHALL provide an equivalent normalized input to status publication.
7. `R9.AC7` WHEN aggregation adds public result fields, the system SHALL preserve compatibility with valid `v1alpha1` manifests that omit aggregation declarations.
8. `R9.AC8` IF aggregate input associates a plan with a different source identifier, THEN the system SHALL return an invalid-input failure without evaluating that source.
9. `R9.AC9` IF aggregate input contains provenance missing API version, Kind, name, or UID, THEN the system SHALL return an invalid-input failure for that resource.

## Non-Functional Requirements

- `NFR1` **Type safety:** Aggregation SHALL use the approved logical types, exact intermediate arithmetic, explicit average rounding, and normalized result semantics without introducing implicit coercion; `R1.AC15` through `R1.AC20`, `R2.AC10`, `R2.AC11`, `R2.AC16`, `R3.AC3` through `R3.AC9`, and `R4.AC1` through `R4.AC30` provide behavioral coverage.
- `NFR2` **Determinism:** Equivalent declarations, operator outcomes, limits, and failure conditions SHALL produce stable grouping, reductions, rounding, ordering, deduplication, and reason codes; `R1.AC17` through `R1.AC20`, `R2.AC3` through `R2.AC5`, `R3.AC4` through `R3.AC14`, `R4.AC8` through `R4.AC30`, `R6.AC17`, `R7.AC1` through `R7.AC11`, and `R8.AC9` provide behavioral coverage.
- `NFR3` **Provenance fidelity:** Every contribution SHALL remain traceable to complete immutable Kubernetes provenance even when public provenance is omitted; `R5.AC5` through `R5.AC13` and `R7.AC4` through `R7.AC9` provide behavioral coverage.
- `NFR4` **Failure isolation:** Resource-local, aggregate-local, source-local, sibling-source, cancellation, and limit failures SHALL remain contained at their approved boundaries; `R2.AC12`, `R6.AC1` through `R6.AC17`, and `R8.AC3` through `R8.AC8` provide behavioral coverage.
- `NFR5` **Boundedness:** Grouping, contribution processing, collection, deduplication, and requested provenance SHALL remain within explicit implementation-owned cardinality ceilings; `R8.AC1` through `R8.AC9` provide behavioral coverage.
- `NFR6` **Confidentiality:** Diagnostics SHALL identify declaration and provenance boundaries without exposing field paths, extracted values, operator operands, or resource contents; `R2.AC13`, `R6.AC16`, and `R6.AC17` provide behavioral coverage.
- `NFR7` **API compatibility:** Aggregate declarations and outcomes SHALL be additive, structural, round-trip safe, and compatible with existing `v1alpha1` resources; `R1.AC8` through `R1.AC20`, `R5.AC12` through `R5.AC14`, and `R9.AC3` through `R9.AC7` provide behavioral coverage.
- `NFR8` **Testability:** Aggregation SHALL be verifiable through genuine collaboration with typed output and value operators at the cross-module integration layer plus envtest for public API admission, defaulting, serialization, and status persistence; `R1.AC11` through `R1.AC20`, `R4.AC1` through `R4.AC30`, `R5.AC12` through `R5.AC14`, `R6.AC1`, and `R9.AC3` through `R9.AC7` provide observable boundary coverage.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `resource-selection`, `field-extraction`, `typed-output-model`, and `value-operators` contracts.
- `C2` The public API remains `kubeseer.io/v1alpha1`; aggregate declarations and outcomes must be additive and must not change existing source, field, operator, or typed-result semantics.
- `C3` Aggregation consumes only accepted or unsuccessful operator outcomes with complete provenance; it performs no discovery, authorization, selection, extraction, operator evaluation, Kubernetes I/O, or status write.
- `C4` Public aggregation declarations and outcomes must produce a structural CRD schema and must not use `runtime.RawExtension`, an unbounded arbitrary map, or `x-kubernetes-preserve-unknown-fields` as a shortcut.
- `C5` Exact number, timestamp, duration, and quantity behavior must reuse the normalization contracts approved by `typed-output-model` and `value-operators`; only `average` may round, through the explicit precision and rounding-mode contract in `R1` and `R4`.
- `C6` A source-level selection failure, including one temporarily unavailable namespace, remains atomic according to approved `resource-selection` R6; aggregation may not recover or publish discarded temporary values.
- `C7` Aggregate cardinality ceilings must be centrally declared and testable; numeric capacity tuning across the whole product remains in `performance-and-limits`.
- `C8` The initial aggregator has no required cache; any future cache must preserve planning, typing, provenance, isolation, ordering, limit, and cancellation semantics, with `R2.AC15` defining the unavailable-cache path.
- `C9` Automated proof follows the repository testing constitution: genuine cross-module integration with `typed-output-model` and `value-operators` is the minimum layer, envtest proves public API behavior, and no dedicated unit-test layer is maintained.
- `C10` Reconciliation scheduling, retries, condition transitions, summary semantics, semantic hashing, and status-write suppression remain owned by `reconciliation-runtime` and `status-and-conditions`.

## Out Of Scope

- Cross-source aggregation, joins between resource types, lookups, window functions, nested aggregations, or aggregate expressions.
- User-defined functions, CEL, scripting, templates, arbitrary transformations, percentile, median, histogram, or standard-deviation functions.
- Grouping by absent, null, multi-match, `object`, or `list` fields.
- Locale-sensitive collation, case folding, Unicode normalization, approximate equality, or fuzzy deduplication.
- Significant-digit precision, stochastic rounding, locale-sensitive rounding, or rounding modes outside the closed initial set.
- Publishing stale partial values from a source whose namespace read, authorization, selection, or operator stage failed atomically.
- Configurable per-resource failure policies or best-effort authorization behavior.
- Discovery-dependent admission checks; runtime planning remains mandatory until `admission-validation` introduces approved earlier rejection paths.
- Product-wide source, resource, reconciliation, concurrency, execution-time, object-size, or total status-size budgets beyond aggregate-local cardinality ceilings.
