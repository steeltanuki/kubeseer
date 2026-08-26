---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T13:33:46Z
last_modified: 2026-08-26T13:33:46Z
approved_fingerprint: sha256:b8f6a3d692127affc45209de5bedd5c5a82a70eb4f0539b0214666af4430953b
---

# Requirements Document

## Introduction

This feature defines how each `Kubeseer` source declares fields and extracts native values from the resources returned by `resource-selection`. It introduces a deliberately limited JSONPath contract, validates every declared expression before evaluating resources, and produces deterministic source-, resource-, and field-scoped extraction outcomes for later typed conversion.

Extraction is a pure processing boundary: it consumes selected unstructured Kubernetes objects plus their provenance and performs no discovery, authorization, or Kubernetes API reads. Missing values, explicit `null`, one match, and multiple matches remain distinct. Native Kubernetes JSON values are preserved without string rendering or typed conversion.

<!-- assumed: fields belong to each source and remain optional in v1alpha1 so this additive feature preserves manifests accepted by the approved API and resource-selection contracts (source: SPECIFICATIONS.md field-extraction example and approved kubeseer-api-foundation R5.AC4) -->
<!-- assumed: the initial path language requires the documented `{.path}` envelope and supports member access, quoted map-key access, non-negative array indexes, and array wildcards; filters, unions, slices, recursive descent, templates, functions, and scripts remain excluded (source: .walden/constitution.md controlled-subset rule and SPECIFICATIONS.md examples) -->
<!-- assumed: a direct path to a native list is one match containing that list, while wildcard expansion produces an ordered sequence of matches (source: SPECIFICATIONS.md scalar, list, and multiple-result distinction) -->
<!-- assumed: missing keys and out-of-range indexes produce an absent outcome, explicit null produces a present null outcome, and other evaluation failures make the affected source outcome atomic and unsuccessful (source: .walden/constitution.md absent-versus-null and explicit degraded-result rules) -->

## Requirements

### R1 Per-Source Field Declarations

**User Story:** As a Kubeseer user, I want to declare named extraction fields on each source, so that downstream results remain attributable to stable configuration identifiers.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL expose `fields` as an optional list on every Kubeseer source.
2. `R1.AC2` WHEN a field entry is declared, the system SHALL require its `name` property.
3. `R1.AC3` WHEN a field entry is declared, the system SHALL require its `path` property.
4. `R1.AC4` WHEN a field name is provided, the system SHALL accept at most 63 characters matching `^[a-z][A-Za-z0-9]*(?:-[a-z0-9]+)*$`.
5. `R1.AC5` IF two field entries in one source use the same field name, THEN the system SHALL reject the Kubeseer manifest without persisting it.
6. `R1.AC6` IF a field path is empty or exceeds 1024 characters, THEN the system SHALL reject the Kubeseer manifest without persisting it.
7. `R1.AC7` WHEN a source omits `fields`, the system SHALL produce a successful empty extraction outcome for that source.
8. `R1.AC8` WHEN a source declares an explicit empty `fields` list, the system SHALL produce a successful empty extraction outcome for that source.
9. `R1.AC9` WHEN a valid Kubeseer source is serialized and deserialized through the `v1alpha1` API, the system SHALL preserve every declared field name and path unchanged.

### R2 Supported JSONPath Subset

**User Story:** As a Kubeseer user, I want a small documented JSONPath subset, so that extraction behavior is predictable across built-in resources and Custom Resources.

#### Acceptance Criteria

1. `R2.AC1` The system SHALL require each path expression to be enclosed by one opening `{` and one closing `}` delimiter.
2. `R2.AC2` The system SHALL require the expression body to address the selected resource root through a leading `.` token.
3. `R2.AC3` WHEN an expression uses `.property` syntax with a property matching `^[A-Za-z_][A-Za-z0-9_]*$`, the system SHALL access the named property of the current object.
4. `R2.AC4` WHEN an expression uses `['property-key']` syntax, the system SHALL access the exact quoted key of the current object.
5. `R2.AC5` WHEN an expression uses a non-negative `[index]` token, the system SHALL access that zero-based position of the current array.
6. `R2.AC6` WHEN an expression uses a `[*]` token, the system SHALL expand the current array from its first element to its last element.
7. `R2.AC7` WHEN a supported access token follows an array wildcard, the system SHALL evaluate that token independently against every expanded element.
8. `R2.AC8` IF an expression uses filters, unions, slices, negative indexes, recursive descent, map wildcards, template directives, functions, or script evaluation, THEN the system SHALL reject the expression as unsupported.
9. `R2.AC9` IF an expression contains text outside its single delimited path, THEN the system SHALL reject the expression as unsupported.

### R3 Expression Planning And Validation

**User Story:** As a platform operator, I want field expressions validated before object processing, so that configuration defects cannot yield partial or input-dependent execution.

#### Acceptance Criteria

1. `R3.AC1` WHEN extraction is prepared for a source, the system SHALL parse every declared field expression before evaluating any selected resource for that source.
2. `R3.AC2` WHEN all field expressions for a source are valid, the system SHALL produce an immutable extraction plan keyed by the source identifier.
3. `R3.AC3` WHEN equivalent field declarations are prepared, the system SHALL produce semantically equivalent extraction plans.
4. `R3.AC4` IF any field expression has invalid syntax, THEN the system SHALL fail planning for that source before evaluating any selected resource.
5. `R3.AC5` IF any field expression uses syntax outside the supported subset, THEN the system SHALL fail planning for that source before evaluating any selected resource.
6. `R3.AC6` WHEN planning fails for one source, the system SHALL preserve independent plans or failures for every other source.
7. `R3.AC7` WHEN a planning failure is reported, the system SHALL identify the affected source identifier and field name.
8. `R3.AC8` WHEN a planning failure is reported, the system SHALL return a stable reason code that distinguishes invalid syntax from unsupported syntax.
9. `R3.AC9` IF a compiled-expression cache is absent, stale, or unavailable, THEN the system SHALL prepare a valid extraction plan from the declared fields without changing its semantics.

### R4 Native Value And Cardinality Semantics

**User Story:** As a typed-output component, I want extraction to preserve native values and match cardinality, so that later conversion can distinguish absence, null, scalar values, lists, and wildcard results.

#### Acceptance Criteria

1. `R4.AC1` WHEN a path resolves to exactly one non-null JSON scalar, the system SHALL return that native scalar as one match.
2. `R4.AC2` WHEN a path resolves directly to one JSON object, the system SHALL return that native object as one match.
3. `R4.AC3` WHEN a path resolves directly to one JSON array, the system SHALL return that native array as one match.
4. `R4.AC4` WHEN a path resolves to an explicit JSON null, the system SHALL return a present null as one match.
5. `R4.AC5` WHEN an object property or quoted map key is absent on one traversal branch, the system SHALL discard only that branch from the field's match sequence.
6. `R4.AC6` WHEN a non-negative array index is outside the current array bounds on one traversal branch, the system SHALL discard only that branch from the field's match sequence.
7. `R4.AC7` WHEN no traversal branch produces a match, the system SHALL return an absent outcome for that resource and field.
8. `R4.AC8` WHEN an array wildcard produces multiple matches, the system SHALL return those matches in source-array traversal order.
9. `R4.AC9` WHEN nested array wildcards produce multiple matches, the system SHALL return those matches in depth-first left-to-right traversal order.
10. `R4.AC10` WHEN a value is extracted, the system SHALL preserve its native JSON value without textual rendering or type coercion.
11. `R4.AC11` WHEN an extracted object or array is returned to a caller, the system SHALL prevent caller mutation from changing the selected resource object.

### R5 Deterministic Extraction Outcomes

**User Story:** As a downstream processing component, I want extraction outcomes to retain stable identity and order, so that typed conversion and later aggregation can remain deterministic.

#### Acceptance Criteria

1. `R5.AC1` WHEN a field is evaluated for a selected resource, the system SHALL associate its outcome with the source identifier, field name, and complete selection provenance.
2. `R5.AC2` WHEN one source contains multiple selected resources, the system SHALL preserve the resource order supplied by the successful selection outcome.
3. `R5.AC3` WHEN one source declares multiple fields, the system SHALL order field outcomes lexicographically by field name within each resource.
4. `R5.AC4` WHEN equivalent extraction plans and selected resource contents are supplied, the system SHALL return semantically equivalent extraction outcomes.
5. `R5.AC5` WHEN a successful selection contains no resources, the system SHALL return a successful empty extraction outcome for that source.
6. `R5.AC6` IF a selection outcome for a source is unsuccessful, THEN the system SHALL perform no field evaluation for that source.
7. `R5.AC7` WHEN multiple source outcomes are processed, the system SHALL preserve one extraction outcome for each input source in input order.
8. `R5.AC8` WHEN an unsuccessful selection outcome is carried into extraction processing, the system SHALL preserve its source identifier and selection failure unchanged.

### R6 Evaluation Failures And Isolation

**User Story:** As a Kubeseer user, I want extraction failures isolated and diagnosable, so that invalid data in one source does not obscure independent sources or expose observed values.

#### Acceptance Criteria

1. `R6.AC1` IF a property-access token is evaluated against a non-object value, THEN the system SHALL fail the affected source with an evaluation-type-mismatch reason.
2. `R6.AC2` IF an array token is evaluated against a non-array value, THEN the system SHALL fail the affected source with an evaluation-type-mismatch reason.
3. `R6.AC3` IF a selected resource cannot provide a valid object for evaluation, THEN the system SHALL fail the affected source with an invalid-resource reason.
4. `R6.AC4` WHEN an evaluation failure is reported, the system SHALL identify the affected source identifier, field name, and resource provenance.
5. `R6.AC5` WHEN an evaluation failure is reported, the system SHALL omit observed resource contents and extracted values from its diagnostic.
6. `R6.AC6` IF any field evaluation fails for a source, THEN the system SHALL discard every extraction value produced for that source execution.
7. `R6.AC7` WHEN one source fails extraction, the system SHALL preserve independent outcomes for every other source.
8. `R6.AC8` IF the extraction context is canceled or reaches its deadline, THEN the system SHALL return a source-scoped interrupted-extraction failure.
9. `R6.AC9` WHEN cancellation prevents evaluation of a later source, the system SHALL return an interrupted-extraction outcome for that unstarted source.
10. `R6.AC10` WHEN cancellation occurs during batch extraction, the system SHALL preserve every source outcome completed before cancellation.
11. `R6.AC11` WHEN equivalent evaluation failures occur for equivalent inputs, the system SHALL return the same stable reason code.

## Non-Functional Requirements

- `NFR1` **Native-value fidelity:** Extraction SHALL preserve JSON-native null, boolean, string, number, object, and array values until typed conversion; `R4.AC1` through `R4.AC4`, `R4.AC10`, and `R4.AC11` provide behavioral coverage.
- `NFR2` **Determinism:** Equivalent field declarations and selected objects SHALL produce stable plans, traversal order, outcome order, and reason codes; `R3.AC3`, `R4.AC8`, `R4.AC9`, `R5.AC2` through `R5.AC4`, and `R6.AC11` provide behavioral coverage.
- `NFR3` **Reliability:** Invalid expressions SHALL be rejected before resource evaluation, while runtime failures remain source-atomic and isolated across sources; `R3.AC1`, `R3.AC4` through `R3.AC6`, and `R6.AC6` through `R6.AC10` provide behavioral coverage.
- `NFR4` **Confidentiality:** Extraction diagnostics SHALL identify configuration and provenance without exposing observed object contents or extracted values; `R3.AC7`, `R6.AC4`, and `R6.AC5` provide behavioral coverage.
- `NFR5` **Bounded input:** Public field identifiers and expressions SHALL have structural length limits, while unbounded product-wide result cardinality remains deferred to `performance-and-limits`; `R1.AC4` and `R1.AC6` provide behavioral coverage.
- `NFR6` **Testability:** Extraction behavior SHALL be verifiable through genuine collaboration with `resource-selection` at the repository's cross-module integration layer; `R3.AC1`, `R5.AC1`, `R5.AC6`, `R5.AC7`, and `R6.AC7` provide observable boundary coverage.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `kubeseer-api-foundation` and `resource-selection` contracts.
- `C2` The public API extends each `kubeseer.io/v1alpha1` source with an optional `fields` list; it must not introduce a second API version or change existing selection semantics.
- `C3` Extraction consumes only successful `resource-selection` outcomes containing unstructured Kubernetes JSON objects and complete provenance.
- `C4` The initial expression language is the exact subset declared by `R2`; compatibility with a broader JSONPath implementation is not implied.
- `C5` A compiled-expression cache is optional, but any cache must preserve the validation, isolation, native-value, and determinism contracts in this document; `R3.AC9` defines the unavailable-cache fallback.
- `C6` Automated proof follows the repository testing constitution: genuine cross-module integration is the minimum layer because a real selection-to-extraction collaboration path exists, and no dedicated unit-test layer is maintained.
- `C7` Product-wide limits for selected-resource count, extracted-value count, object size, concurrency, and status size belong to `performance-and-limits`.

## Out Of Scope

- Converting extracted values to declared logical types or defining serialized status values.
- Filtering, comparing, grouping, aggregating, truncating, or paginating extracted values.
- JSONPath filters, unions, slices, negative indexes, recursive descent, map wildcards, templates, functions, scripts, CEL, or arbitrary transformations.
- Resource discovery, resource selection, installation-policy evaluation, authorization decisions, or Kubernetes API reads.
- Admission webhooks or discovery-dependent admission checks for field expressions; runtime planning remains mandatory.
- Reconciliation watches, retries, scheduling, status conditions, degraded-result policy, or status publication.
- Logging observed object contents or extracted values.
- Product-wide cardinality, object-size, status-size, concurrency, or performance limits.
