---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T15:34:51Z
last_modified: 2026-08-26T15:34:51Z
approved_fingerprint: sha256:bb6b2efe295a6c76e8537d49363a7a8924a6f978e333b305f730d5cdf83469c0
source_requirements_approved_at: 2026-08-26T15:14:38Z
source_requirements_fingerprint: sha256:aac32579c9becf2a6b06322a0926da92d440b5e2a69dda95774986dcfcca6a22
---

# Feature Design

## Overview

Implement `typed-output-model` as one new internal package,
`internal/typedoutput`, plus additive `api/v1alpha1` declarations. The package
has four stages: compile each source's field type declarations, convert native
matches from `internal/extraction`, retain immutable internal typed outcomes,
and map those outcomes into the structural `KubeseerResult` API model.

The design deliberately separates the internal value model from the public
status representation. Internal object and list values remain native deep
copies. At the API boundary they become canonical JSON text in dedicated
`objectValue` and `listValue` fields. Exact non-integer numbers and normalized
quantity magnitudes also use canonical decimal text. This avoids floating-point
loss and arbitrary OpenAPI regions while keeping every public branch explicit.
Kubernetes requires structural schemas to declare a type for object fields and
array items; preserving arbitrary unknown subtrees is an explicit opt-out that
this feature forbids. [Kubernetes structural schema](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#specifying-a-structural-schema)

<!-- assumed: `internal/typedoutput` owns planning, conversion, outcomes, and API mapping because extraction must not import its downstream consumer and api/v1alpha1 must not contain conversion behavior (source: .walden/constitution.md architecture boundaries) -->
<!-- assumed: exact decimal strings are the public representation for `number` and normalized quantity magnitudes because JSON floating-point values cannot satisfy the approved no-silent-precision-loss contract (source: approved NFR4 and R3.AC4-R3.AC5) -->
<!-- assumed: object and list payloads use canonical JSON text only at the public API boundary; the internal model retains native deep-copied values for future operators (source: approved C4, R3.AC11-R3.AC12, and R5.AC8) -->
<!-- assumed: source-level planning errors are represented once in `fieldErrors`, while resource-dependent conversion errors remain on each affected field result (source: approved R2.AC7-R2.AC9 and R6.AC4-R6.AC7) -->
<!-- assumed: cross-field union consistency is enforced by constructors and the status adapter in this feature; admission CEL for arbitrary external status writers remains deferred with admission-validation (source: approved Out Of Scope and C9) -->

## Architecture

```text
api/v1alpha1 KubeseerSource.Fields
                 │
                 ▼
       CompileBatch / immutable Plan
          │ valid fields   │ field planning errors
          ▼                └──────────────┐
extraction.SourceOutcome                  │
          │                               │
          ▼                               ▼
       ConvertBatch ───────────────► typedoutput.SourceOutcome
          │                               │
          │ native typed values           │ sanitized failures
          └──────────────┬────────────────┘
                         ▼
                  StatusResult adapter
                         │
                         ▼
             api/v1alpha1.KubeseerResult
```

`CompileBatch` validates all field type declarations before conversion starts.
Each source plan contains immutable field entries sorted by field name plus
field-local planning failures. The first implementation has no cache; every
call is the valid cache-unavailable path.

`ConvertBatch` walks source inputs sequentially. An unsuccessful extraction
outcome is passed through unchanged. Successful extraction resources and fields
are converted in their existing deterministic order. A runtime conversion
failure discards only temporary matches for the affected field. Cancellation
discards the in-progress source, preserves completed sources, and marks later
sources interrupted.

The status adapter is pure. It verifies internal union invariants, copies
identity and errors, canonicalizes object/list JSON, and emits an atomic-list
snapshot. It performs no status write; reconciliation and semantic update
decisions remain downstream.

## Options Considered

### Option A — Internal tagged values plus explicit structural status branches

- Summary: Keep native values in a private tagged union and map them to explicit
  public payload fields such as `integerValue`, `numberValue`, `timestampValue`,
  `objectValue`, and `listValue`.
- Why chosen: It preserves conversion fidelity internally, produces a structural
  CRD, makes invalid payload combinations observable, and avoids coupling
  downstream operators to API serialization details.

### Option B — Use `runtime.RawExtension` or `apiextensionsv1.JSON`

- Summary: Store every converted value as arbitrary JSON under one `value`
  property.
- Why rejected: Object and list values become an opaque schema region, violating
  `C4` and weakening generated-client guarantees. Kubernetes documents unknown
  subtree preservation as an explicit structural-schema escape hatch rather
  than a typed model. [Kubernetes CRD validation](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation)

### Option C — Recursive public JSON-value union

- Summary: Represent every object property and list element as a recursively
  nested public tagged value.
- Why rejected: It keeps native-looking status but multiplies schema nodes,
  generated deep-copy code, validation combinations, and client complexity.
  Canonical JSON text for only the two composite branches satisfies the approved
  round-trip contract with fewer moving parts.

### Option D — Convert directly into API structs

- Summary: Eliminate internal typed outcomes and make conversion return
  `v1alpha1.KubeseerResult` directly.
- Why rejected: Conversion, operator compatibility, and tests would depend on
  status serialization. A private domain model keeps future `value-operators`
  and `cross-namespace-aggregation` independent from API encoding.

## Simplicity And Elegance Review

- Simplest viable shape: one package, one immutable planner, one converter, one
  status adapter, no cache, no reflection-driven coercion registry, and no
  recursive public value schema.
- Coupling check: `internal/typedoutput` imports `api/v1alpha1` and
  `internal/extraction`; neither upstream package imports typed output. Public API
  structs contain data only. Kubernetes I/O remains outside the package.
- Future-proofing: conversion functions are selected by the closed logical-type
  enum. A future approved type adds one branch and one converter. Operators can
  consume the internal tagged values without parsing status JSON.
- Challenge result: producing API structs directly is smaller by one mapper but
  couples domain behavior to serialization. Keeping the mapper is the minimum
  extra boundary that protects later operators and aggregation, so Option A
  remains preferred.

## Components And Interfaces

### Public Field Type Contract

- Purpose: add the optional explicit type declaration to `KubeseerField`.
- Shape: Go field `Type KubeseerValueType` with JSON tag
  `json:"type,omitempty"`, enum validation for the nine approved lower-case
  values, and no default.
- Compatibility: omission remains API-valid; typed planning reports
  `MissingType` when conversion is requested.
- Generated artifacts: deep-copy code and the Kubeseer CRD OpenAPI schema are
  regenerated and verified.
- Requirements: `R1`, `NFR7`.

### Conversion Planner

- Purpose: validate type declarations independently of observed values.
- Inputs/Outputs: `CompileSource(v1alpha1.KubeseerSource) PlanOutcome` and
  `CompileBatch([]v1alpha1.KubeseerSource) []PlanOutcome`.
- Plan: immutable source ID plus valid `FieldPlan` entries sorted by field name;
  each field plan stores only name and `KubeseerValueType`.
- Errors: missing and unsupported types become field-local planning outcomes.
- Cache policy: no cache initially; plan construction is deterministic and
  cheap.
- Requirements: `R1`, `R2`, `NFR1`, `NFR3`.

### Scalar And Semantic Converters

- Purpose: convert one non-null native extraction match according to one field
  plan without reflection or implicit stringification.
- Dispatch: one explicit switch on `KubeseerValueType`; each branch returns a
  private `Value` or a sanitized `ConversionError`.
- Native inputs: exactly the JSON-compatible set already accepted by extraction:
  `nil`, `bool`, `string`, `int64`, `float64`, `json.Number`, `map[string]any`,
  and `[]any`.
- Requirements: `R3`, `R4`, `NFR1`, `NFR2`, `NFR4`.

| Declared type | Accepted non-null input | Internal value | Public payload |
| --- | --- | --- | --- |
| `string` | `string` | copied string | `stringValue` |
| `integer` | `int64`, integral finite `float64`, canonical base-10 string, integral `json.Number` | `int64` | `integerValue` |
| `number` | finite `int64`, `float64`, JSON-number string, `json.Number` | canonical exact decimal text | `numberValue` |
| `boolean` | `bool`, exact `"true"` or `"false"` | `bool` | `booleanValue` |
| `timestamp` | RFC 3339 string with offset | UTC `time.Time` | `timestampValue` |
| `duration` | Go duration string | `time.Duration` | canonical text plus `nanoseconds` |
| `quantity` | Kubernetes quantity string | `resource.Quantity` plus exact decimal magnitude | canonical text plus `baseUnits` |
| `object` | `map[string]any` | defensive native copy | canonical JSON `objectValue` |
| `list` | `[]any` | defensive native copy | canonical JSON `listValue` |

Integer strings are parsed with `strconv.ParseInt` after the approved lexical
check. Float inputs are accepted as integers only when finite, mathematically
integral, and exactly representable as `int64`. Number strings are validated
against the JSON number grammar and normalized by an exact decimal helper that
removes redundant sign, exponent, and zero forms without converting through
`float64`. A native `float64` is normalized from its shortest exact round-trip
decimal representation.

Timestamps use `time.Parse(time.RFC3339Nano, value)`, require an explicit `Z` or
numeric offset, convert to UTC, and serialize with `time.RFC3339Nano`.
Durations use `time.ParseDuration`; the canonical value is `Duration.String()`
and the exact normalized magnitude is the signed `int64` nanosecond count.

Quantities use `resource.ParseQuantity`, `Quantity.String()`, and
`Quantity.AsDec()`. Kubernetes `Quantity` is fixed-point and canonicalizes its
serialized form without floating point; the converter additionally compares
the exact input magnitude with `AsDec()` and rejects any parse that capped or
rounded the input. [Kubernetes `resource.Quantity`](https://pkg.go.dev/k8s.io/apimachinery/pkg/api/resource#Quantity)

### Internal Typed Outcome Model

- Purpose: retain typed values for later operators without exposing mutable
  aliases or API encoding choices.
- `SourceOutcome`: source ID, ordered resources, source-level field planning
  errors, and an optional pass-through extraction or interruption error.
- `ResourceOutcome`: complete `selection.Provenance` plus ordered fields.
- `FieldOutcome`: field name, optional declared type, state, ordered matches, and
  optional `ConversionError`.
- `Value`: private tagged union with defensive accessors; null is a distinct
  match state rather than a payload value.
- Requirements: `R4`, `R5`, `R6`, `NFR2`, `NFR5`, `NFR6`.

### Batch Converter

- Purpose: join public source declarations with extraction outcomes while
  preserving isolation and ordering.
- Input: `SourceInput{Source v1alpha1.KubeseerSource, Extraction extraction.SourceOutcome}`.
- Output: one `typedoutput.SourceOutcome` per input source.
- Behavior: compile the full batch first, then process sources sequentially.
  Validate source and field identities before conversion. Preserve extraction
  failures as the exact `error` value. Convert valid fields into temporary
  match storage and discard that storage on field failure.
- Cancellation: check before each source, resource, field, match, and composite
  canonicalization step. Discard the in-progress source on interruption,
  preserve completed sources, and mark every unstarted source interrupted.
- Requirements: `R2`, `R6`, `R7`, `NFR3`, `NFR5`.

### Structural Status Adapter

- Purpose: map valid internal outcomes into public `KubeseerResult` values
  without Kubernetes I/O.
- Input/Output: `BuildResult([]typedoutput.SourceOutcome) (v1alpha1.KubeseerResult, error)`.
- Invariants: source, resource, field, and match lists use atomic list semantics
  because they are ordered snapshots. State enums distinguish successful,
  absent, values, null, and error branches. Pointer payloads preserve empty
  string, zero integer, and false boolean values.
- Composite encoding: canonical JSON uses deterministic object-key order and
  preserves list order. The adapter validates the encoded top-level kind so an
  object cannot populate `listValue` or vice versa.
- Requirements: `R5`, `R6`, `NFR2`, `NFR3`, `NFR7`.

## Data Models

The following pseudocode fixes the public shape; exact comments and controller
generation markers remain implementation details.

```go
type KubeseerValueType string

const (
    ValueTypeString    KubeseerValueType = "string"
    ValueTypeInteger   KubeseerValueType = "integer"
    ValueTypeNumber    KubeseerValueType = "number"
    ValueTypeBoolean   KubeseerValueType = "boolean"
    ValueTypeTimestamp KubeseerValueType = "timestamp"
    ValueTypeDuration  KubeseerValueType = "duration"
    ValueTypeQuantity  KubeseerValueType = "quantity"
    ValueTypeObject    KubeseerValueType = "object"
    ValueTypeList      KubeseerValueType = "list"
)

type KubeseerField struct {
    Name string             `json:"name"`
    Path string             `json:"path"`
    Type KubeseerValueType  `json:"type,omitempty"`
}

type KubeseerResult struct {
    Sources []KubeseerSourceResult `json:"sources,omitempty"`
}

type KubeseerSourceResult struct {
    ID          string                 `json:"id"`
    State       KubeseerSourceState    `json:"state"`
    FieldErrors []KubeseerFieldError   `json:"fieldErrors,omitempty"`
    Resources   []KubeseerResourceResult `json:"resources,omitempty"`
    Error       *KubeseerResultError   `json:"error,omitempty"`
}

type KubeseerResourceResult struct {
    APIVersion string                 `json:"apiVersion"`
    Kind       string                 `json:"kind"`
    Namespace  string                 `json:"namespace,omitempty"`
    Name       string                 `json:"name"`
    UID        types.UID              `json:"uid"`
    Fields     []KubeseerFieldResult  `json:"fields,omitempty"`
}

type KubeseerFieldResult struct {
    Name    string                `json:"name"`
    Type    KubeseerValueType     `json:"type,omitempty"`
    State   KubeseerFieldState    `json:"state"`
    Matches []KubeseerTypedMatch  `json:"matches,omitempty"`
    Error   *KubeseerResultError  `json:"error,omitempty"`
}

type KubeseerTypedMatch struct {
    State         KubeseerMatchState      `json:"state"`
    StringValue   *string                 `json:"stringValue,omitempty"`
    IntegerValue  *int64                  `json:"integerValue,omitempty"`
    NumberValue   *string                 `json:"numberValue,omitempty"`
    BooleanValue  *bool                   `json:"booleanValue,omitempty"`
    TimestampValue *metav1.Time           `json:"timestampValue,omitempty"`
    DurationValue *KubeseerDurationValue  `json:"durationValue,omitempty"`
    QuantityValue *KubeseerQuantityValue  `json:"quantityValue,omitempty"`
    ObjectValue   *string                 `json:"objectValue,omitempty"`
    ListValue     *string                 `json:"listValue,omitempty"`
}

type KubeseerDurationValue struct {
    Canonical   string `json:"canonical"`
    Nanoseconds int64  `json:"nanoseconds"`
}

type KubeseerQuantityValue struct {
    Canonical string `json:"canonical"`
    BaseUnits string `json:"baseUnits"`
}

type KubeseerFieldError struct {
    Name    string `json:"name"`
    Reason  string `json:"reason"`
    Message string `json:"message,omitempty"`
}

type KubeseerResultError struct {
    Reason  string `json:"reason"`
    Message string `json:"message,omitempty"`
}
```

`KubeseerSourceState` is `values` or `error`. `KubeseerFieldState` is `absent`,
`values`, or `error`. `KubeseerMatchState` is `value` or `null`. Constructors
enforce these invariants:

- source `error` has one error and no resources;
- field `absent` has no matches and no error;
- field `values` has at least one match and no error;
- field `error` has one error and no matches;
- null match has no payload;
- value match has exactly one payload matching the field type.

All result collections use `+listType=atomic`; they are complete ordered
snapshots rather than mergeable keyed configuration. `status.result` remains a
pointer, preserving omitted versus explicitly present empty result semantics.

The private `ConversionError` carries source ID, field name, optional copied
provenance, stable `ConversionErrorReason`, sanitized message, and a private
cause exposed only through `errors.Is` and `errors.As`. Planning errors have no
resource provenance. Runtime field errors inherit provenance from the resource
outcome.

## Error Handling

- Stable conversion reasons are `MissingType`, `UnsupportedType`,
  `ConversionTypeMismatch`, `InvalidValue`, `ConversionOverflow`,
  `ForbiddenConversion`, `InvalidInput`, and `ConversionInterrupted`.
- Missing or unsupported type fails planning for only that field. The source
  retains valid plans and records one source-level field error.
- Lexical parsing failures use `InvalidValue`; wrong native container/scalar
  kinds use `ConversionTypeMismatch`; exactness or range failures use
  `ConversionOverflow` or `ForbiddenConversion` according to the approved
  matrix.
- A failed match discards every temporary match for that resource-field pair.
  Sibling fields, resources, and sources continue.
- Source or field identity mismatch is `InvalidInput`; no best-effort positional
  pairing is attempted.
- Extraction errors remain the exact upstream `error` in the internal source
  outcome. The status adapter emits only their sanitized reason and message.
- Context cancellation is checked throughout batch and composite processing.
  The current source is discarded and returned as `ConversionInterrupted`;
  completed source outcomes remain intact.
- Error text includes configuration identity and provenance only. It never
  includes the path, native value, canonical object/list JSON, or containing
  resource.
- There are no retries or Kubernetes I/O in this package.

## Security Considerations

- Typed payloads may contain sensitive observed data; conversion does not log
  values and errors do not embed them.
- Canonical object/list JSON is result data, not diagnostic data. It is created
  only by the explicit status adapter and never included in errors.
- Numeric and semantic parsers are bounded by their input strings and perform no
  template, script, locale, network, or filesystem evaluation.
- The adapter constructs only known structural fields. It does not preserve
  unknown JSON members at the API-schema boundary.
- Authorization remains upstream. Typed conversion never performs a read and
  cannot broaden the effective observation scope.

## Failure Modes And Tradeoffs

- Failure mode: a field omits `type` because older manifests remain API-valid.
  Mitigation: deterministic `MissingType` planning error before conversion.
  Tradeoff: compatibility is preserved at admission while the first typed
  result requires an explicit user update.
- Failure mode: a float is mathematically integral but outside exact `int64`
  representation. Mitigation: range and exactness checks before conversion.
  Tradeoff: ambiguous or lossy coercions fail rather than approximate.
- Failure mode: `resource.ParseQuantity` accepts a value by rounding or capping.
  Mitigation: compare the exact lexical magnitude with `Quantity.AsDec()` and
  reject a mismatch. Tradeoff: Kubeseer is stricter than the parser's permissive
  acceptance to satisfy NFR4.
- Failure mode: canonical composite JSON is malformed or has the wrong top-level
  kind. Mitigation: encode from validated native values and decode-check the
  adapter result before returning it. Tradeoff: one extra validation allocation
  at the status boundary.
- Failure mode: consumers expect object/list payloads as native nested status
  fields. Mitigation: explicit `objectValue` and `listValue` names plus logical
  type preserve meaning. Tradeoff: consumers parse canonical JSON text, avoiding
  a recursive or opaque CRD schema.
- Failure mode: a client writes an invalid payload union directly to the status
  subresource. Mitigation: Kubeseer's constructors and adapter reject invalid
  unions; envtest proves controller-produced values. Tradeoff: comprehensive
  admission CEL remains in the later admission feature.
- Failure mode: cancellation occurs after some matches are converted. Mitigation:
  source-local temporary storage is discarded. Tradeoff: completed fields in
  the interrupted source are not returned, while completed sibling sources are.
- Failure mode: a future operator needs the unconverted original value.
  Mitigation: internal object/list values remain native, and scalar typed values
  retain complete approved semantics. Tradeoff: adding an explicit original
  payload requires a future approved API change instead of duplicating status
  data now.

## Testing Strategy

Testing follows the repository constitution; no package-local unit-test layer is
introduced.

- Extend `TestAPIContract` under `api/v1alpha1` with envtest scenarios for type
  enum admission, omitted-type compatibility, JSON/YAML round trips, deep-copy
  isolation, generated schema structure, status-subresource persistence, atomic
  result lists, and absence of preserve-unknown regions.
- Extend `TestModuleIntegration` with real
  `selection -> extraction -> typedoutput` scenarios covering every conversion
  branch, exactness guards, missing/unsupported types, absence/null/empty values,
  direct-list versus wildcard cardinality, canonical object/list JSON, field
  isolation, extraction-error pass-through, identity mismatch, deterministic
  ordering, and cancellation partitions.
- Exercise public status mapping in integration and envtest so internal values
  and API serialization are proven together rather than through a test-only
  seam.
- Run the existing Kubernetes 1.35.6 and 1.36.2 compatibility matrix after CRD
  changes.
- Keep fixtures value-aware but diagnostics assertions verify that sensitive
  sentinel values never appear in errors.

## Verification Plan

- API proof: `make test-api` emits
  `API_CONTRACT=typed-output-model-types STATUS=passed` only after admission,
  schema, round-trip, deep-copy, and status persistence scenarios pass.
- Conversion proof: `make test-integration` emits
  `MODULE_INTEGRATION=typed-output-model-conversions STATUS=passed` after the
  exact conversion matrix and normalization scenarios pass.
- Cardinality proof: the same suite emits
  `MODULE_INTEGRATION=typed-output-model-cardinality STATUS=passed` after
  absence, null, empty, direct-list, wildcard, and defensive-copy scenarios.
- Isolation proof: the same suite emits
  `MODULE_INTEGRATION=typed-output-model-isolation STATUS=passed` after planning,
  runtime failure, upstream pass-through, ordering, and cancellation scenarios.
- Serialization proof: the same suite emits
  `MODULE_INTEGRATION=typed-output-model-serialization STATUS=passed` after
  structural adapter and JSON/YAML round-trip scenarios.
- Generated-artifact proof: `make generate`, `make manifests`, and `make verify`
  establish current deep-copy code and CRD schema; generated OpenAPI assertions
  inspect type enum, result branches, atomic lists, and absence of
  `x-kubernetes-preserve-unknown-fields`.
- Compatibility proof: `make test-compatibility` installs and exercises the CRD
  against the centrally pinned Kubernetes versions.
- Static and race proof: `make test GO_TEST_FLAGS=-race`, `go build ./...`, and
  `go mod tidy -diff` prove race safety, compilation, and dependency exactness.
- Operational evidence: none in this phase because conversion performs no I/O,
  logging, reconciliation, or status update.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Public Field Type Contract; API proof |
| `R2` | Conversion Planner; Batch Converter; planning integration scenarios |
| `R3` | Scalar And Semantic Converters; conversion matrix; exactness scenarios |
| `R4` | Internal Typed Outcome Model; cardinality proof |
| `R5` | Structural Status Adapter; public data model; API and serialization proofs |
| `R6` | ConversionError model; field-local temporary storage; isolation proof |
| `R7` | Batch Converter; ordered temporary source storage; cancellation proof |
| `NFR1` | Closed type dispatch; lexical, range, and exactness checks |
| `NFR2` | Tagged internal values; explicit state enums; defensive copies |
| `NFR3` | Sorted plans; preserved input order; canonical normalization |
| `NFR4` | Exact decimal representation; UTC normalization; duration and quantity exactness guards |
| `NFR5` | Field-atomic conversion; source-atomic interruption; sibling preservation |
| `NFR6` | Sanitized ConversionError and error sentinel tests |
| `NFR7` | Optional API additions; structural explicit branches; envtest compatibility matrix |
| `NFR8` | Real extraction-to-typed-output integration and envtest API coverage |
