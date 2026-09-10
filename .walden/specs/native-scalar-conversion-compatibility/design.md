---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-10T06:13:03Z
last_modified: 2026-09-10T06:13:03Z
approved_fingerprint: sha256:182a17eac071079fa41375e3acd7a714dd47a0a5c9ff9821a20b026f8e35f809
source_requirements_approved_at: 2026-09-06T08:34:52Z
source_requirements_fingerprint: sha256:61e5da029595b29d2872277b8536387a5eb4e5da23c746695f3864108a4a0f29
---

# Feature Design

## Overview

Extend the existing exact quantity and duration parsing helpers to cover the
native lexical forms named in the approved Requirements. Continue using the
pinned native parsers for canonical values and compare their results with exact
magnitudes before conversion succeeds.

This is a local correction in typedoutput, followed by cross-module and API
persistence proof. The public payload model and dependency versions stay fixed.

## Architecture

```text
selected native string → field extraction → typed conversion
                                           |
                              native parsing + exact magnitude
                                           |
                               range and equality validation
                                           |
                           existing Match → public status payload
```

The converter owns lexical and exactness checks. Extraction retains its native
values and provenance; operators and aggregations consume the same typed model.

## Options Considered

### Option A — Extend existing exactness helpers (selected)

Add nano/micro suffix factors, signed unitless zero recognition, and the second
Unicode microsecond spelling. Preserve native canonical parsing and exact
rational comparisons. This is small and retains the existing no-rounding policy.

### Option B — Trust native parser success alone

Native parsers can round or truncate inputs that exceed their precision.
Removing the independent exactness comparison would accept the explicitly
forbidden subnanosecond cases, so this cannot satisfy the approved contract.

### Option C — Replace both parsers with a new shared grammar engine

A shared scanner could centralize grammar handling, but quantity and duration
have different syntax, ranges, units, and error classification. A new abstraction
would expand this correction beyond the demonstrated compatibility gap.

## Simplicity And Elegance Review

Keep helpers private in internal/typedoutput/converters.go. Extend their current
tables and narrow special cases; avoid text replacement on whole inputs.
The native parser remains the acceptance/canonicalization authority, while
exact arithmetic protects against loss of information. Add no cache or package.

<!-- assumed: retain the existing exact magnitude range and arithmetic helpers; only lexical coverage and error classification needed for the approved cases change (source: approved C2/C4 and R1/R2) -->

## Components And Interfaces

### Quantity conversion

Extend quantityPattern with the native decimal suffixes n and u.
In quantitySuffixFactor, represent them as exact rational factors 1/10^9 and
1/10^6. Preserve m, decimal exponents, and binary suffix handling. Verify the
complete supported suffix set against the repository-pinned parser during
implementation; do not add spellings the native parser rejects.

Keep convertQuantity's staged behavior:
1. Parse with resource.ParseQuantity.
2. Obtain exactQuantityMagnitude without rounding.
3. Enforce the existing absolute magnitude ceiling.
4. Compare exact magnitude with the native parsed decimal value.
5. Use the existing canonical rational decimal and quantity.String payloads.

Invalid lexical input maps to InvalidValue. An exact magnitude outside the
supported representation maps to ConversionOverflow, including when native
parsing itself rejects or saturates it. A native rounded result maps to
ForbiddenConversion. Successful output retains the original canonical quantity
format chosen by the native parser and the exact normalized base-unit decimal.

Requirements: R1.AC1–R1.AC9.

### Duration conversion

Recognize exactly 0, +0, and -0 as unitless zero in the exact magnitude helper.
Do not generalize this special case to empty strings, bare signs, or arbitrary
unitless numeric strings. Preserve the native parser's rejection of those forms.

Add U+03BC GREEK SMALL LETTER MU followed by s to durationUnit with factor 1000,
alongside us and U+00B5 MICRO SIGN followed by s. Keep the existing sign and
compound-term arithmetic. Match Unicode unit strings as complete UTF-8 strings;
do not normalize arbitrary Unicode text or confuse bytes with runes.

Retain exact rational accumulation before the signed int64 nanosecond range
check. Reject a non-integral final nanosecond magnitude as ForbiddenConversion.
For a syntactically valid duration outside int64, report ConversionOverflow
before treating the native range rejection as invalid lexical input. For a
representable exact magnitude, time.ParseDuration must succeed and return that
same magnitude. Existing canonical duration.String serialization remains in use.

Requirements: R2.AC1–R2.AC9.

### Pipeline, public payload, and consumers

CompileSource, ConvertMatch, ConvertBatchWithLimit, and the existing Match/value
representations keep their signatures. Field-level failures remain atomic;
successful sibling fields survive. Absent and explicit null values bypass scalar
parsing as before. No source I/O or discovery is added to conversion.

Reuse existing public quantity and duration serializers and CRD schemas. Check
the operator bridge and aggregation bridge for shared conversion consumers;
extend integration assertions where corrected spellings pass through those
existing paths without changing operator or reducer semantics.

Requirements: R3.AC1–R3.AC8.

### Documentation delivery

Update docs/api-reference.md with quantity nano/micro examples, quoted signed
zero duration strings, all three microsecond spellings, and canonical text plus
normalized values. Explicitly label U+00B5 versus U+03BC so a visually identical
example cannot hide missing coverage.

Update docs/concepts-and-architecture.md with the native grammar plus exactness
model: parser acceptance alone does not permit rounding or range overflow.
Examples contain synthetic scalar strings and their public normalized payloads.
A dedicated later leaf task checks these two guides against the same regression
fixtures and validates links.

Requirements: R4.AC1–R4.AC4.

## Data Models

No new internal or public fields. Representative normalized values are:

| Input string | Type | Normalized magnitude |
| --- | --- | --- |
| 100n | quantity | 0.0000001 base units |
| 100u | quantity | 0.0001 base units |
| -100u | quantity | -0.0001 base units |
| 0, +0, -0 | duration | 0 nanoseconds |
| 1us, 1µs, 1μs | duration | 1000 nanoseconds |
| -1μs | duration | -1000 nanoseconds |

Quantity canonical text is obtained from the pinned native quantity value, not
from a handwritten display rule. Canonical duration zero is 0s. Equivalent
quantity forms must agree in normalized magnitude; a native format choice need
not make their lexical quantity text identical.

## Error Handling

Keep InvalidValue, ConversionOverflow, and ForbiddenConversion distinct.
Retain failures for quantity 0.0000000001 and duration 0.1ns. Errors remain
field-scoped and sanitize native parser causes through the existing error
representation. Raw parser error strings must not reach logs, Events, or status,
because they may contain the supplied input.

The combined exactness and native checks also preserve rejection of malformed
compound forms. Regression fixtures cover positive/negative boundaries and
lexically malformed inputs independently of magnitude overflow.

## Security Considerations

The correction accepts native scalar forms within existing limits; it does not
broaden observable resource scope or retain unconverted copies. Existing input
and produced-value budgets remain enforced. Documentation uses synthetic values;
failure diagnostics exclude original strings, field paths, and resource bodies.

## Failure Modes And Tradeoffs

Secondary grammar tables still require maintenance when dependency versions
change. A differential fixture matrix against pinned native parsers detects
omissions while explicitly excluding native rounding from expected successes.
Visually similar Unicode units are distinct test entries expressed with explicit
code points. Exact arithmetic remains necessary even though it duplicates some
native parsing work.

## Baseline Contract Reconciliation

The typed-output-model Requirements already require native parsing conventions
and exactness; this correction expands concrete coverage without changing that
contract. Extend assertTypedOutputConversionScenarios in
test/integration/typed_output_conversions_test.go and the existing API contract
serialization/status coverage. Retain current successful forms and negative cases.

Review typed-output-model Design's conversion matrix and Tasks' scalar
conversion/persistence proofs for any enumerated grammar assumptions. Changed
approved descriptions or proofs must be reconciled and reviewed before relying
on them; unchanged proof commands can gain regression coverage in their existing
higher-layer suites. No approved baseline document is silently edited here.

## Testing Strategy

Use real extraction outcomes as input to the production converter, then assert
public serialization. Cover n/u signed quantities, decimal/exponent equivalents,
binary controls, signed zero, all microsecond code points, negative and compound
durations, int64 boundaries, quantity range, and fractional precision failures.
Native parser success is checked separately from the exact expected magnitude.

A mixed-resource/mixed-field fixture proves field atomicity and successful sibling
preservation. Include absent/null values and sentinel diagnostics checks.
Envtest stores corrected payloads through Kubeseer's status subresource and
verifies round-trip type tags and exact normalized values. Use the established
API contract suite; introduce no dedicated unit-test layer or external cluster.

## Verification Plan

- R1: native acceptance plus exact conversion and serialization for new suffixes;
  retain lexical, range, rounding, decimal, exponent, and binary controls.
- R2: native duration acceptance for explicit code-point fixtures; assert exact
  nanoseconds and native canonical text, plus malformed and precision failures.
- R3: extraction/conversion collaboration, field atomicity, sibling isolation,
  null/absence, diagnostics, and persisted API round-trip.
- R4: compare both documentation pages with regression fixture expectations,
  inspect quoted string examples, and validate relative links.
- Later Tasks use named, non-vacuous scenarios within make test-integration and
  make test-api with pinned envtest assets. Check generated schema stability
  through the repository's existing verification entry points when relevant.
- No tests are run or claimed as implementation evidence during this Design phase.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Quantity conversion; Data Models; quantity regression matrix |
| `R2` | Duration conversion; Unicode and exactness fixtures |
| `R3` | Pipeline, public payload, and consumers; envtest persistence |
| `R4` | Documentation delivery; fixture-aligned documentation review |
| `NFR1` | Pinned native parsers and lexical compatibility matrix |
| `NFR2` | Rational exactness comparison and range rejection |
| `NFR3` | Stable payloads; field atomicity; sanitized errors |
| `NFR4` | Cross-module and API tests with existing-form controls |
| `NFR5` | API and architecture examples checked against fixtures |
