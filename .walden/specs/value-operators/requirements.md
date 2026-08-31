---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-30T12:49:25Z
last_modified: 2026-08-30T12:49:25Z
approved_fingerprint: sha256:0117a96751f49bb51ed517d9864733ea39b7efdd00a68967c26e29f745bcd5ef
---

# Requirements Document

## Introduction

This feature defines a controlled, type-safe operator layer over the immutable
field outcomes produced by `typed-output-model`. A Kubeseer field may declare
an ordered operator chain containing comparisons, string predicates, presence
checks, collection membership checks, and the field-local `default` and
`coalesce` transformations. Operator declarations are planned before any
typed resource is evaluated, and invalid names, arity, operands, regular
expressions, or type combinations fail deterministically.

Predicate operators decide whether one typed resource contributes to the
downstream value collection. Every predicate declared across that resource's
fields must pass. Transformations update only the working outcome of their own
field and feed later operators in declaration order. Operator evaluation
preserves source and resource isolation, provenance, typed-value fidelity, and
stable ordering while performing no discovery, authorization, extraction, or
Kubernetes I/O.

<!-- assumed: `operators` is an optional ordered list on each field because fields are the stable declaration and typed-outcome boundary shared by the approved prerequisites (source: approved field-extraction R1 and typed-output-model R1, R5) -->
<!-- assumed: predicates across one resource use implicit AND, while OR, negation groups, nested expressions, and cross-field references are deferred because they would introduce a separate expression language (source: SPECIFICATIONS.md controlled operator set and CEL exclusion) -->
<!-- assumed: positive predicates use existential semantics over non-null matches, while `ne` and `notIn` are the corresponding negative predicates over a non-empty comparable match set (source: approved typed-output-model cardinality contract and deterministic filtering objective) -->
<!-- assumed: `default` and `coalesce` are field-local; `default` replaces absence or null with one configured value, while `coalesce` selects the first non-null match without referencing sibling fields (source: SPECIFICATIONS.md simple-transformations scope and .walden/constitution.md boundary rules) -->
<!-- assumed: configured operands are converted through the approved typed-output conversion matrix for the field's declared type, and the operator layer introduces no second coercion system (source: approved typed-output-model R3 and NFR1) -->

## Requirements

### R1 Per-Field Operator Declarations

**User Story:** As a Kubeseer user, I want to declare an ordered operator chain on a typed field, so that filtering and simple transformations remain explicit in the resource configuration.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL expose `operators` as an optional ordered list on every Kubeseer field declaration.
2. `R1.AC2` WHEN an operator entry is declared, the system SHALL require its lower-camel-case `operator` property.
3. `R1.AC3` The system SHALL define the supported operator names as `eq`, `ne`, `gt`, `gte`, `lt`, `lte`, `contains`, `startsWith`, `endsWith`, `matches`, `exists`, `notExists`, `in`, `notIn`, `default`, and `coalesce`.
4. `R1.AC4` The system SHALL allow an operator entry to carry either one typed `value` operand or one ordered typed `values` operand list according to its declared operator.
5. `R1.AC5` The system SHALL declare no API-server default for an omitted operator list.
6. `R1.AC6` WHEN a field omits `operators`, the system SHALL treat that field as having an identity operator chain.
7. `R1.AC7` WHEN a field declares an explicit empty `operators` list, the system SHALL treat that field as having an identity operator chain.
8. `R1.AC8` IF a manifest supplies an operator name outside the supported set, THEN the system SHALL reject the manifest without persisting it.
9. `R1.AC9` WHEN a valid operator chain is serialized and deserialized through the `v1alpha1` API, the system SHALL preserve operator declaration order.
10. `R1.AC10` WHEN a valid operator chain is serialized and deserialized through the `v1alpha1` API, the system SHALL preserve every operand's logical type and value.
11. `R1.AC11` WHEN a Kubeseer resource containing operators is deep-copied, the system SHALL prevent mutations to the copy from changing the original declarations.
12. `R1.AC12` The system SHALL represent operator operands through a structural Kubernetes OpenAPI schema without an opaque preserve-unknown region.

### R2 Operator Planning And Early Validation

**User Story:** As a platform operator, I want operator configuration validated before typed resources are processed, so that configuration defects cannot produce data-dependent partial filtering.

#### Acceptance Criteria

1. `R2.AC1` WHEN operator evaluation is prepared for a source, the system SHALL inspect every declared operator before evaluating any typed resource for that source.
2. `R2.AC2` WHEN every operator declaration for a source is valid, the system SHALL produce an immutable operator plan keyed by the source identifier.
3. `R2.AC3` WHEN one field contains multiple operators, the system SHALL retain their declaration order in the operator plan.
4. `R2.AC4` WHEN one source contains multiple fields, the system SHALL order field operator plans lexicographically by field name.
5. `R2.AC5` WHEN equivalent field and operator declarations are prepared, the system SHALL produce semantically equivalent operator plans.
6. `R2.AC6` IF an operator has incompatible field-type semantics, THEN the system SHALL fail planning for that source before evaluating typed resources.
7. `R2.AC7` IF an operator has invalid operand arity, THEN the system SHALL fail planning for that source before evaluating typed resources.
8. `R2.AC8` IF an operator operand cannot be converted to the field's declared type, THEN the system SHALL fail planning for that source before evaluating typed resources.
9. `R2.AC9` IF a `matches` operand is not a valid supported regular expression, THEN the system SHALL fail planning for that source before evaluating typed resources.
10. `R2.AC10` WHEN operator planning fails for one source, the system SHALL preserve independent plans or failures for every other source.
11. `R2.AC11` WHEN an operator planning failure is reported, the system SHALL identify the source identifier, field name, operator index, and operator name.
12. `R2.AC12` WHEN an operator planning failure is reported, the system SHALL return a stable reason code.
13. `R2.AC13` IF an operator-plan cache is absent, stale, or unavailable, THEN the system SHALL prepare a valid plan from the current declarations without changing operator semantics.

### R3 Type Compatibility, Arity, And Operand Conversion

**User Story:** As a Kubeseer user, I want one explicit operator compatibility matrix, so that a declaration has the same meaning for every observed resource.

#### Acceptance Criteria

1. `R3.AC1` The system SHALL allow `eq` and `ne` only for `string`, `integer`, `number`, `boolean`, `timestamp`, `duration`, and `quantity` fields.
2. `R3.AC2` The system SHALL allow `gt`, `gte`, `lt`, and `lte` only for `integer`, `number`, `timestamp`, `duration`, and `quantity` fields.
3. `R3.AC3` The system SHALL allow `contains`, `startsWith`, `endsWith`, and `matches` only for `string` fields.
4. `R3.AC4` The system SHALL allow `exists` and `notExists` for every supported field type.
5. `R3.AC5` The system SHALL allow `in` and `notIn` only for `string`, `integer`, `number`, `boolean`, `timestamp`, `duration`, and `quantity` fields.
6. `R3.AC6` The system SHALL allow `default` and `coalesce` for every supported field type.
7. `R3.AC7` WHEN `eq`, `ne`, `gt`, `gte`, `lt`, `lte`, `contains`, `startsWith`, `endsWith`, `matches`, or `default` is declared, the system SHALL require exactly one non-null `value` operand.
8. `R3.AC8` WHEN `in` or `notIn` is declared, the system SHALL require a non-empty `values` operand list.
9. `R3.AC9` WHEN `exists`, `notExists`, or `coalesce` is declared, the system SHALL require the declaration to omit operands.
10. `R3.AC10` IF one operator declaration supplies both `value` and `values`, THEN the system SHALL fail operator planning with an invalid-arity reason.
11. `R3.AC11` WHEN a configured operand is prepared, the system SHALL convert it through the approved `typed-output-model` conversion matrix for the field's declared type.
12. `R3.AC12` WHEN every member of a `values` operand list is prepared, the system SHALL convert each member through the field's declared type in input order.
13. `R3.AC13` IF operand preparation would require an inferred field type, THEN the system SHALL fail operator planning with an invalid-operand reason.
14. `R3.AC14` IF operand preparation would require an implicit coercion forbidden by `typed-output-model`, THEN the system SHALL fail operator planning with an invalid-operand reason.
15. `R3.AC15` IF a null operand is supplied, THEN the system SHALL fail operator planning with an invalid-operand reason.
16. `R3.AC16` WHEN duplicate converted members occur in an `in` or `notIn` operand list, the system SHALL preserve the same predicate semantics as the equivalent list with duplicates removed.

### R4 Equality And Ordered Comparison Semantics

**User Story:** As a Kubeseer user, I want comparisons to use each field's logical type, so that filtering does not depend on display formatting or input encoding.

#### Acceptance Criteria

1. `R4.AC1` WHEN `eq` evaluates a field containing at least one equal non-null match, the system SHALL return a true predicate decision.
2. `R4.AC2` WHEN `eq` evaluates a field containing no equal non-null match, the system SHALL return a false predicate decision.
3. `R4.AC3` WHEN `ne` evaluates a field containing at least one non-null match and no equal match, the system SHALL return a true predicate decision.
4. `R4.AC4` WHEN `ne` evaluates a field containing an equal non-null match, the system SHALL return a false predicate decision.
5. `R4.AC5` WHEN `gt` evaluates a field containing at least one non-null match greater than its operand, the system SHALL return a true predicate decision.
6. `R4.AC6` WHEN `gte` evaluates a field containing at least one non-null match greater than or equal to its operand, the system SHALL return a true predicate decision.
7. `R4.AC7` WHEN `lt` evaluates a field containing at least one non-null match less than its operand, the system SHALL return a true predicate decision.
8. `R4.AC8` WHEN `lte` evaluates a field containing at least one non-null match less than or equal to its operand, the system SHALL return a true predicate decision.
9. `R4.AC9` WHEN an ordered comparison has no non-null match satisfying its relation, the system SHALL return a false predicate decision.
10. `R4.AC10` WHEN integer values are compared, the system SHALL compare their exact signed 64-bit values.
11. `R4.AC11` WHEN number values are compared, the system SHALL compare their exact normalized decimal values without floating-point rounding.
12. `R4.AC12` WHEN timestamp values are compared, the system SHALL compare their represented instants independently of original offsets.
13. `R4.AC13` WHEN duration values are compared, the system SHALL compare their exact normalized nanosecond magnitudes.
14. `R4.AC14` WHEN quantity values are compared, the system SHALL compare their exact normalized Kubernetes quantity magnitudes independently of display units.
15. `R4.AC15` WHEN string values are tested by `eq` or `ne`, the system SHALL use case-sensitive exact string equality.
16. `R4.AC16` WHEN boolean values are tested by `eq` or `ne`, the system SHALL compare their logical boolean values.
17. `R4.AC17` WHEN a comparison operator evaluates an absent field or a field containing only null matches, the system SHALL return a false predicate decision.

### R5 String, Presence, And Membership Semantics

**User Story:** As a Kubeseer user, I want string, presence, and membership predicates with explicit multi-match behavior, so that filters remain predictable for wildcard extraction results.

#### Acceptance Criteria

1. `R5.AC1` WHEN `contains` evaluates a string field containing at least one non-null match with the operand as a case-sensitive substring, the system SHALL return a true predicate decision.
2. `R5.AC2` WHEN `startsWith` evaluates a string field containing at least one non-null match with the operand as a case-sensitive prefix, the system SHALL return a true predicate decision.
3. `R5.AC3` WHEN `endsWith` evaluates a string field containing at least one non-null match with the operand as a case-sensitive suffix, the system SHALL return a true predicate decision.
4. `R5.AC4` WHEN `matches` evaluates a string field containing at least one non-null match accepted by the configured Go regular expression, the system SHALL return a true predicate decision.
5. `R5.AC5` WHEN a string predicate has no non-null match satisfying its relation, the system SHALL return a false predicate decision.
6. `R5.AC6` WHEN `exists` evaluates a field containing at least one match, the system SHALL return a true predicate decision.
7. `R5.AC7` WHEN `exists` evaluates an absent field, the system SHALL return a false predicate decision.
8. `R5.AC8` WHEN `notExists` evaluates an absent field, the system SHALL return a true predicate decision.
9. `R5.AC9` WHEN `notExists` evaluates a field containing at least one match, the system SHALL return a false predicate decision.
10. `R5.AC10` WHEN presence is evaluated, the system SHALL treat explicit null, empty string, empty object, and empty list matches as existing.
11. `R5.AC11` WHEN `in` evaluates a field containing at least one non-null match equal to a configured member, the system SHALL return a true predicate decision.
12. `R5.AC12` WHEN `in` evaluates a field containing no non-null match equal to a configured member, the system SHALL return a false predicate decision.
13. `R5.AC13` WHEN `notIn` evaluates a field containing at least one non-null match and no match equal to a configured member, the system SHALL return a true predicate decision.
14. `R5.AC14` WHEN `notIn` evaluates a field containing a non-null match equal to a configured member, the system SHALL return a false predicate decision.
15. `R5.AC15` WHEN `in` or `notIn` evaluates an absent field or a field containing only null matches, the system SHALL return a false predicate decision.

### R6 Transformations And Application Order

**User Story:** As a Kubeseer user, I want operator chains applied in declaration order, so that defaults, coalescing, and predicates compose without hidden reordering.

#### Acceptance Criteria

1. `R6.AC1` WHEN a field declares multiple operators, the system SHALL apply them from the first declared entry to the last declared entry.
2. `R6.AC2` WHEN `default` evaluates an absent field, the system SHALL produce one non-null match containing the converted fallback value.
3. `R6.AC3` WHEN `default` evaluates a null match, the system SHALL replace that match with the converted fallback value at the same position.
4. `R6.AC4` WHEN `default` evaluates a non-null match, the system SHALL preserve that match unchanged.
5. `R6.AC5` WHEN `coalesce` evaluates multiple matches containing a non-null value, the system SHALL retain only the first non-null match in input order.
6. `R6.AC6` WHEN `coalesce` evaluates one or more null matches without a non-null match, the system SHALL retain one null match.
7. `R6.AC7` WHEN `coalesce` evaluates an absent field, the system SHALL preserve the absent field state.
8. `R6.AC8` WHEN a transformation completes, the system SHALL supply its transformed field outcome to the next declared operator.
9. `R6.AC9` WHEN a predicate operator returns false, the system SHALL mark the containing resource as rejected by its operator plan.
10. `R6.AC10` WHEN multiple predicate operators apply to one resource, the system SHALL accept the resource only when every predicate returns true.
11. `R6.AC11` WHEN an operator chain contains only transformations, the system SHALL accept the resource with the transformed field outcome.
12. `R6.AC12` WHEN a field has an identity operator chain, the system SHALL preserve its typed outcome unchanged.
13. `R6.AC13` WHEN two equivalent operator chains receive equivalent typed outcomes, the system SHALL produce semantically equivalent operator outcomes.

### R7 Resource Filtering, Outcomes, And Ordering

**User Story:** As a downstream aggregation component, I want operator outcomes to preserve accepted values, rejected decisions, failures, and provenance, so that later grouping remains deterministic and diagnosable.

#### Acceptance Criteria

1. `R7.AC1` WHEN a successful typed source outcome is processed, the system SHALL produce one operator source outcome with the same source identifier.
2. `R7.AC2` WHEN one typed resource is processed, the system SHALL produce exactly one accepted, rejected, or unsuccessful operator resource outcome.
3. `R7.AC3` WHEN a resource has no predicate operators, the system SHALL produce an accepted operator resource outcome.
4. `R7.AC4` WHEN a resource is accepted, the system SHALL expose its transformed typed fields to downstream value collection.
5. `R7.AC5` WHEN a resource is rejected, the system SHALL omit its typed fields from downstream value collection.
6. `R7.AC6` WHEN a resource is rejected, the system SHALL treat the rejection as a successful predicate result rather than an error.
7. `R7.AC7` WHEN a successful source contains no accepted resources, the system SHALL produce a successful empty downstream value collection for that source.
8. `R7.AC8` WHEN accepted resources are returned, the system SHALL preserve their input resource order.
9. `R7.AC9` WHEN an accepted resource is returned, the system SHALL preserve its complete selection provenance.
10. `R7.AC10` WHEN transformed fields are returned, the system SHALL preserve lexicographic field-name order within each resource.
11. `R7.AC11` WHEN a field is not transformed by its operator chain, the system SHALL preserve its typed match order.
12. `R7.AC12` IF an operator-bearing field has an unsuccessful typed outcome, THEN the system SHALL produce an unsuccessful operator outcome for that resource rather than a false predicate decision.
13. `R7.AC13` WHEN an unsuccessful operator resource outcome is produced, the system SHALL discard temporary transformed values for that resource.
14. `R7.AC14` WHEN one resource has an unsuccessful operator outcome, the system SHALL preserve independent outcomes for every other resource in the source.
15. `R7.AC15` IF a typed source outcome is unsuccessful, THEN the system SHALL perform no operator evaluation for that source.
16. `R7.AC16` WHEN an unsuccessful typed source outcome is carried into operator processing, the system SHALL preserve its source identifier and upstream failure unchanged.
17. `R7.AC17` WHEN multiple source outcomes are processed, the system SHALL preserve one operator source outcome for each input source in input order.
18. `R7.AC18` WHEN equivalent operator plans and typed outcomes are processed, the system SHALL return semantically equivalent operator outcomes.

### R8 Failures, Interruption, And Confidentiality

**User Story:** As a Kubeseer user, I want operator failures isolated and sanitized, so that invalid configuration or data cannot expose observed values or hide independent outcomes.

#### Acceptance Criteria

1. `R8.AC1` WHEN an operator failure is reported, the system SHALL return one stable reason from unsupported-operator, incompatible-operator, invalid-arity, invalid-operand, invalid-pattern, invalid-input, or operator-interrupted.
2. `R8.AC2` WHEN an operator planning failure is reported, the system SHALL omit configured operand values from its diagnostic.
3. `R8.AC3` WHEN an operator evaluation failure is reported, the system SHALL identify the affected source identifier, field name, operator index, operator name, and resource provenance when available.
4. `R8.AC4` WHEN an operator evaluation failure is reported, the system SHALL omit field paths, typed values, extracted values, and containing resource contents from its diagnostic.
5. `R8.AC5` IF an operator plan is paired with a source of a different identifier, THEN the system SHALL return an invalid-input failure for that source outcome.
6. `R8.AC6` IF an operator field plan is paired with a typed field of a different name or declared type, THEN the system SHALL return an invalid-input failure for that resource outcome.
7. `R8.AC7` IF the operator context is canceled or reaches its deadline, THEN the system SHALL return an operator-interrupted failure for the affected source.
8. `R8.AC8` WHEN cancellation prevents evaluation of a later source, the system SHALL return an operator-interrupted outcome for that unstarted source.
9. `R8.AC9` WHEN cancellation occurs during batch operator evaluation, the system SHALL preserve every source outcome completed before cancellation.
10. `R8.AC10` WHEN equivalent operator failures occur for equivalent inputs, the system SHALL return the same stable reason code.
11. `R8.AC11` WHEN one source fails operator planning or evaluation, the system SHALL preserve independent outcomes for every other source.

## Non-Functional Requirements

- `NFR1` **Type safety:** Operator planning and evaluation SHALL use only the compatibility and conversion matrix in `R3`, with observable rejection of incompatible, malformed, inferred, or forbidden operands through `R2.AC6` through `R2.AC9` and `R3.AC1` through `R3.AC16`.
- `NFR2` **Determinism:** Equivalent declarations and typed inputs SHALL produce stable plans, comparison decisions, transformation results, ordering, and reason codes through `R2.AC3` through `R2.AC5`, `R4.AC1` through `R4.AC17`, `R6.AC1`, `R6.AC13`, `R7.AC8` through `R7.AC11`, `R7.AC17`, `R7.AC18`, and `R8.AC10`.
- `NFR3` **Typed-value fidelity:** Operators SHALL compare normalized logical values and preserve unchanged or transformed match states without display-format dependence through `R4.AC10` through `R4.AC16`, `R5.AC10`, `R6.AC2` through `R6.AC8`, and `R7.AC11`.
- `NFR4` **Failure isolation:** Invalid plans, failed resources, source failures, and cancellation SHALL not discard independent resource or source outcomes through `R2.AC10`, `R7.AC12` through `R7.AC17`, and `R8.AC7` through `R8.AC11`.
- `NFR5` **Confidentiality:** Operator diagnostics SHALL expose stable configuration identity and provenance without exposing configured operands or observed values through `R2.AC11`, `R8.AC2`, `R8.AC3`, and `R8.AC4`.
- `NFR6` **API compatibility:** Operator declarations SHALL be additive, structural, round-trip safe, and compatible with existing `v1alpha1` manifests through `R1.AC1`, `R1.AC4` through `R1.AC12`.
- `NFR7` **Testability:** Operator behavior SHALL be verifiable through genuine collaboration with `typed-output-model` at the repository's cross-module integration layer and through envtest for public API admission, serialization, and deep-copy behavior; `R1.AC8` through `R1.AC12`, `R2.AC1`, `R6.AC1`, `R7.AC1`, `R7.AC17`, and `R8.AC11` provide observable boundary coverage.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `field-extraction` and `typed-output-model` contracts.
- `C2` The public API remains `kubeseer.io/v1alpha1`; operator declarations must be additive and must not change existing selection, extraction, or typed-conversion semantics.
- `C3` Operator evaluation consumes only typed-output plans and outcomes containing explicit logical types, ordered matches, field state, source identity, and complete provenance.
- `C4` Operator evaluation performs no Kubernetes API reads, discovery, installation-policy evaluation, authorization decisions, JSONPath evaluation, or typed conversion of observed values.
- `C5` Configured operand conversion must reuse the exact parsing, normalization, precision, and forbidden-coercion semantics approved for `typed-output-model`.
- `C6` Regular-expression matching must use the Go standard-library regular-expression syntax and execution semantics selected by the approved project toolchain.
- `C7` Automated proof follows the repository testing constitution: genuine cross-module integration with `typed-output-model` is the minimum layer, envtest proves the additive public API contract, and no dedicated unit-test layer is maintained.
- `C8` Reconciliation scheduling, status-update timing, conditions, and degraded-result publication policy remain owned by `reconciliation-runtime` and `status-and-conditions`.
- `C9` Grouping, aggregation, cross-namespace reduction, aggregate provenance, and aggregate cardinality belong to `cross-namespace-aggregation`.
- `C10` Product-wide limits for operator count, operand count, regular-expression size, value cardinality, concurrency, execution cost, and status size belong to `performance-and-limits`.

## Out Of Scope

- Aggregation functions, grouping, cross-namespace reduction, deduplication, or aggregate cardinality policy.
- CEL, scripting, templates, user-defined functions, arbitrary transformations, or dynamically loaded operators.
- Boolean expression trees, OR groups, explicit NOT nodes, nested operator groups, or precedence syntax beyond declaration order and implicit AND.
- Cross-field references, sibling-field fallback, cross-resource coalescing, joins, lookups, or computed fields.
- Locale-sensitive string comparison, case folding, Unicode normalization, fuzzy matching, or natural-language collation.
- Comparing or ordering `object` or `list` values beyond presence checks and the field-local `default` or `coalesce` transformations.
- Reading raw extraction matches as a bypass around the approved typed-output conversion contract.
- Discovery-dependent admission webhooks or semantic admission checks beyond structural operator-name validation; runtime planning remains mandatory.
- Resource discovery, selection, authorization, Kubernetes reads, reconciliation triggers, status condition transitions, or publication policy.
- Product-wide resource, operator, operand, regular-expression, concurrency, execution-time, or status-size limits.
