---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-06T08:34:52Z
last_modified: 2026-09-06T08:34:52Z
approved_fingerprint: sha256:61e5da029595b29d2872277b8536387a5eb4e5da23c746695f3864108a4a0f29
---

# Requirements Document

## Introduction

This corrective feature addresses review findings 3 and 4 (P2): valid native
quantity and duration strings can fail typed conversion because Kubeseer's
secondary exactness parsers recognize a narrower lexical grammar.

The quantity cases `100n` and `100u` pass the pinned Kubernetes parser but fail
Kubeseer conversion. The duration cases `0` and `1μs` pass the pinned Go parser
but fail Kubeseer conversion. Both failures were reproduced through real
field extraction followed by typed conversion.

### Evidence And Baseline

- Code: `internal/typedoutput/converters.go`, `quantityPattern`,
  `exactQuantityMagnitude`, `exactDurationNanoseconds`, and `durationUnit`.
- Baseline contract: `typed-output-model` R3.AC9/R3.AC10 require native duration
  and quantity compatibility; R3.AC15/R3.AC17 prohibit overflow and lossy conversion.
- Public representation: `docs/api-reference.md` documents canonical duration
  text with nanoseconds and canonical quantity text with base units.
- The baseline integration conversion matrix tests ordinary values and errors
  but omits the four reported lexical cases.

<!-- assumed: combine the quantity and duration findings into one externally verifiable native scalar compatibility feature, with independent requirement groups for each type (source: typed-output-model R3 and user request to specify the review findings) -->
<!-- assumed: native parser acceptance alone does not permit rounding or overflow; exactness and representation limits remain mandatory (source: typed-output-model R3.AC15/R3.AC17 and NFR4) -->

## Requirements

### R1 Native Quantity Grammar And Exact Magnitudes

**User Story:** As a Kubeseer user, I want valid Kubernetes quantities to retain
their exact values, so that small quantities do not fail merely due to a suffix.

#### Acceptance Criteria

1. `R1.AC1` WHEN a quantity field receives a string accepted by the pinned Kubernetes quantity parser within the existing exact representation limits, the system SHALL produce a successful quantity value.
2. `R1.AC2` WHEN a quantity field receives 100n, the system SHALL expose the exact base-unit decimal magnitude 0.0000001.
3. `R1.AC3` WHEN a quantity field receives 100u, the system SHALL expose the exact base-unit decimal magnitude 0.0001.
4. `R1.AC4` WHEN a quantity field receives a representable signed nanounit or microunit quantity, the system SHALL preserve its sign in the normalized magnitude.
5. `R1.AC5` WHEN a quantity field is serialized, the system SHALL use the canonical text supplied by the pinned Kubernetes quantity representation.
6. `R1.AC6` WHEN equivalent exact quantity strings use different accepted suffixes or exponent forms, the system SHALL expose the same normalized base-unit magnitude.
7. `R1.AC7` IF a quantity string has invalid native lexical form, THEN the system SHALL return InvalidValue.
8. `R1.AC8` IF a quantity's exact magnitude exceeds the existing supported range, THEN the system SHALL return ConversionOverflow.
9. `R1.AC9` IF native quantity parsing would round the exact input magnitude, THEN the system SHALL return ForbiddenConversion.

### R2 Native Duration Grammar And Exact Nanoseconds

**User Story:** As a Kubeseer user, I want Go duration forms to work consistently,
so that zero values and accepted microsecond spellings remain interchangeable.

#### Acceptance Criteria

1. `R2.AC1` WHEN a duration field receives a string accepted by the pinned Go duration parser with an exact signed 64-bit nanosecond magnitude, the system SHALL produce a successful duration value.
2. `R2.AC2` WHEN a duration field receives 0, +0, or -0 as text, the system SHALL expose zero normalized nanoseconds.
3. `R2.AC3` WHEN a duration field receives 1us, 1µs, or 1μs as text, the system SHALL expose exactly 1000 normalized nanoseconds.
4. `R2.AC4` WHEN a duration field receives an exact representable negative duration using an accepted microsecond spelling, the system SHALL preserve the negative nanosecond magnitude.
5. `R2.AC5` WHEN a duration field is serialized, the system SHALL expose the canonical text supplied by the pinned Go duration representation.
6. `R2.AC6` WHEN exact equivalent duration strings use different accepted unit spellings or compound forms, the system SHALL expose semantically equivalent duration payloads.
7. `R2.AC7` IF a duration string has invalid native lexical form, THEN the system SHALL return InvalidValue.
8. `R2.AC8` IF a syntactically valid duration exceeds the signed 64-bit nanosecond range, THEN the system SHALL return ConversionOverflow.
9. `R2.AC9` IF a duration requires truncation to represent its exact magnitude in nanoseconds, THEN the system SHALL return ForbiddenConversion.

### R3 Pipeline Compatibility And Failure Isolation

**User Story:** As an API consumer, I want corrected lexical acceptance to retain
the existing typed-output contract throughout the observation pipeline.

#### Acceptance Criteria

1. `R3.AC1` The system SHALL preserve the existing public quantity and duration payload field names and logical type tags.
2. `R3.AC2` WHEN a corrected quantity or duration value is stored through the status subresource, the system SHALL preserve its canonical payload through API round-trip decoding.
3. `R3.AC3` IF any match in a quantity or duration field fails conversion, THEN the system SHALL discard all typed matches for that failed field outcome.
4. `R3.AC4` WHEN one quantity or duration field fails conversion, the system SHALL preserve successful independent field outcomes.
5. `R3.AC5` WHEN a conversion error is reported, the system SHALL exclude extracted values, JSONPath expressions, and containing resource contents from diagnostics.
6. `R3.AC6` WHEN extraction produces an absent or explicit-null quantity or duration field, the system SHALL preserve the existing absence or null semantics.
7. `R3.AC7` The system SHALL demonstrate the four reported lexical cases through real field extraction followed by typed conversion.
8. `R3.AC8` The system SHALL retain successful conversion of existing decimal, exponent, binary quantity, and compound-duration inputs within the exact representation limits.

### R4 Documentation And Compatibility Guidance

**User Story:** As a Kubeseer user, I want the API documentation to describe
accepted quantity and duration spellings, so that valid values are not rejected
because the examples use an incomplete grammar.

#### Acceptance Criteria

1. `R4.AC1` WHEN this feature is released, the system SHALL document that quantity conversion follows the pinned Kubernetes parser, including exact `n` and `u` suffix examples, in `docs/api-reference.md`.
2. `R4.AC2` WHEN this feature is released, the system SHALL document zero durations and both `µs` and `μs` microsecond spellings, including canonical serialization, in `docs/api-reference.md`.
3. `R4.AC3` WHEN this feature is released, the system SHALL document that exactness, signed-range, overflow, and no-rounding rules still apply to accepted native spellings in `docs/concepts-and-architecture.md`.
4. `R4.AC4` WHEN documentation shows quantity or duration examples, the system SHALL show normalized public values without exposing unrelated resource contents or sensitive fields.

## Non-Functional Requirements

- `NFR1` Native compatibility: accepted exact values follow the repository-pinned
  parser grammars; verified by R1.AC1 through R1.AC6 and R2.AC1 through R2.AC6.
- `NFR2` Precision: correction must not introduce silent rounding or overflow;
  verified by R1.AC8/R1.AC9 and R2.AC8/R2.AC9.
- `NFR3` API stability and confidentiality: conversion preserves public payloads,
  isolation, and sanitized failures; verified by R3.AC1 through R3.AC6.
- `NFR4` Regression confidence: production collaboration covers newly accepted
  inputs and established successful forms; verified by R3.AC7/R3.AC8.
- `NFR5` Documentation accuracy: user-facing examples match the pinned Go and
  Kubernetes parser contracts and the public normalized representation; verified
  by R4.AC1 through R4.AC4.

## Constraints And Dependencies

- `C1` Depends on typed-output-model, field-extraction, kubeseer-api-foundation,
  and integration-testing-foundation.
- `C2` The grammar baseline is the Go and k8s.io/apimachinery versions pinned
  by go.mod and the project toolchain; this feature does not upgrade dependencies.
- `C3` Microsecond spellings distinguish U+00B5 MICRO SIGN in µs from U+03BC
  GREEK SMALL LETTER MU in μs; regression fixtures must cover both code points.
- `C4` Native parser success is necessary but not sufficient: the existing
  magnitude range and exactness constraints take precedence over rounded native
  results. Inputs such as quantity 0.0000000001 and duration 0.1ns remain rejected.
- `C5` Verification uses cross-module extraction/conversion coverage and envtest
  for persisted public payloads. Any shared parser consumers affected by the
  correction must retain their existing contracts; no dedicated unit-test layer.
- `C6` This feature supplements typed-output-model's existing acceptance matrix.
  Design must identify affected baseline proof coverage before task planning.
- `C7` The documentation update is part of this feature's release scope and
  must be planned as a leaf task after Requirements and Design approval; this
  draft does not authorize documentation implementation.

## Out Of Scope

- New logical types, inferred types, or implicit scalar stringification.
- Changing the signed duration range, quantity range, or precision policy.
- Timestamp, integer, number, object, or list conversion changes.
- New operators, reducers, source-selection rules, or admission policies.
- Watch startup, configuration-budget rejection status, or dependency upgrades.
