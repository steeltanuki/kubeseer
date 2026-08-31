---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T08:58:06Z
last_modified: 2026-08-31T08:58:06Z
approved_fingerprint: sha256:286b57aa97ad1ab9f28376243902e39ecf007e430d46845e6cc074ab23646234
source_requirements_approved_at: 2026-08-31T08:45:18Z
source_requirements_fingerprint: sha256:e186b059871840b5a4aad9bc0472967c52ee01bca4c087451be456b2ac84f58b
---

# Feature Design

## Overview

`cross-namespace-aggregation` adds a pure aggregation stage after
`value-operators`. For each source, it compiles named aggregate declarations
against the source's declared typed fields, consumes the immutable operator
outcome, forms typed groups, executes a closed set of reducers, and projects
structural aggregate outcomes beside the existing resource results.

The stage performs no Kubernetes reads and never reconstructs values from the
public status representation. It consumes only already-authorized operator
outcomes, so the atomic source-failure boundary and the omission of rejected
resources remain unchanged. Resource-local errors degrade only the affected
aggregate; aggregate-local and source-local failures preserve independent
siblings.

<!-- assumed: `average` uses configurable fractional precision from 0 through 18, configurable closed rounding modes, and defaults to 6 places with half-even rounding (source: user decision during Design review) -->

Average evaluation retains an exact rational intermediate, then rounds once to
the configured number of fractional decimal places. Omitted options acquire
effective runtime defaults of precision `6` and mode `halfEven`. These are plan
defaults rather than unconditional API-server defaults: conditional CRD
defaulting cannot distinguish `average` safely from functions for which the
same fields must cause a planning failure. The persisted declaration therefore
retains omission while the immutable plan always contains explicit effective
average options.

## Architecture

The data flow is deliberately one-way:

```text
KubeseerSource declarations
          |
          v
internal/aggregation Planner ----> immutable PlanOutcome
                                          |
operator SourceOutcome ------------------+
                                          v
                              aggregation EvaluateBatch
                                          |
                                          v
                            immutable SourceOutcome
                                          |
                                          v
                              aggregation BuildResult
                                          |
                                          v
                       existing status normalization/hash/write
```

The new `internal/aggregation` package owns planning, grouping, reduction,
limits, failure isolation, and result projection. `internal/typedoutput` gains
only narrow reusable typed-value primitives for canonical equality/order keys,
exact arithmetic, and public match construction. `internal/operators` remains
the owner of operator execution and accepted/rejected/unsuccessful resource
outcomes. `internal/status` remains the sole owner of result normalization,
semantic hashing, summary construction, and status publication.

Evaluation is sequential in the first implementation. Determinism is achieved
by sorting plans, resources, contributions, groups, and public collections at
their explicit boundaries rather than depending on goroutine scheduling or map
iteration. A future concurrent implementation may replace the execution
strategy only if it produces the same immutable outcomes and cancellation
boundaries.

## Options Considered

### Dedicated Aggregation Package Over Operator Outcomes — Selected

This option keeps the new policy in `internal/aggregation`, accepts immutable
plans plus operator outcomes, and delegates typed primitives to
`internal/typedoutput`. It preserves package ownership, keeps Kubernetes I/O
out of aggregation, and allows aggregation failures to be expressed without
changing operator semantics.

### Extend `internal/operators` With Reducers — Rejected

Operators transform or filter a field within one resource. Cross-resource
grouping has different lifecycle, limits, ordering, and failure boundaries.
Combining them would make operator execution responsible for source-wide state
and would blur the accepted-resource contract.

### Aggregate The Projected Public Result — Rejected

Aggregating `KubeseerResult` would make status-shaped data an execution input,
discard private immutable typed representations, and couple reducers to API
serialization. It would also make rejected-resource and failure semantics
harder to preserve correctly.

### Concurrent Streaming Reducers — Deferred

Streaming could lower temporary memory use, but ordered `collect`, `first`,
`last`, `distinct`, deterministic limit failures, and cancellation would need
additional merge protocols. The bounded sequential pipeline is simpler and is
adequate until measured workloads justify concurrency.

## Simplicity And Elegance Review

The design uses one planner, one evaluator pipeline, one closed reducer
dispatch, and one projection adapter. It does not introduce a reducer plugin
registry, expression language, cross-source joins, cache requirement, or a
second status writer. All reducers share group formation, contribution order,
provenance, limit accounting, and error construction.

The main possible simplification—placing aggregation directly in
`internal/operators`—would remove a package but increase conceptual coupling
and expand an already approved boundary. The dedicated package is the smallest
design that preserves existing responsibilities. Runtime defaults avoid a
mutating admission webhook solely for conditional defaulting.

## Components And Interfaces

### Public API Declarations

`api/v1alpha1` adds the following structural declarations:

```go
type KubeseerAggregation struct {
    Name              string                    `json:"name"`
    Function          KubeseerAggregationFunction `json:"function"`
    Field             string                    `json:"field"`
    GroupBy           []string                  `json:"groupBy,omitempty"`
    IncludeProvenance bool                      `json:"includeProvenance,omitempty"`
    Precision         *int32                    `json:"precision,omitempty"`
    RoundingMode      KubeseerRoundingMode      `json:"roundingMode,omitempty"`
}
```

`KubeseerSource.Aggregations` is an optional list-map keyed by `name`.
`GroupBy` is an atomic list because declaration order is semantic. Function and
rounding-mode fields use closed kubebuilder enums. `Precision` has minimum `0`
and maximum `18`; a pointer preserves omission for conditional runtime
defaulting and non-average validation. `RoundingMode` uses the empty string for
omission and is not defaulted in OpenAPI.

Admission rejects unknown functions, unknown rounding modes, duplicate
aggregate names, and out-of-range precision. Semantic checks that depend on
the enclosing source—field references, duplicate grouping fields, function
compatibility, and options on non-average functions—belong to runtime planning.

### `internal/aggregation` Layout

- `contracts.go` defines immutable `Limits`, `PlanOutcome`, `AggregatePlan`,
  `SourceInput`, `SourceOutcome`, `AggregateOutcome`, `Group`, `Contribution`,
  and sanitized `AggregateError` values. Constructors defensively copy slices;
  accessors never expose mutable backing storage.
- `planner.go` indexes the source fields, validates every declaration, applies
  effective average defaults, binds limits, and returns one ordered plan or
  planning failure per declaration.
- `keys.go` creates canonical typed equality and lexical-order tokens for group
  tuples and distinct values.
- `reducers.go` implements the nine closed aggregation functions over ordered
  contributions.
- `evaluator.go` validates source-plan association, deduplicates resource UIDs,
  forms groups, accounts limits, isolates failures, and handles cancellation.
- `result.go` delegates existing resource projection to `operators.BuildResult`
  and appends aggregate projections to the corresponding source result.

The principal interfaces are concrete and intentionally small:

```go
type Limits struct {
    MaxGroups           int
    MaxContributions    int
    MaxCollectedValues  int
    MaxDistinctValues   int
    MaxProvenanceEntries int
}

func DefaultLimits() Limits
func PlanSource(source v1alpha1.KubeseerSource, limits Limits) PlanOutcome
func EvaluateBatch(ctx context.Context, inputs []SourceInput) []SourceOutcome
func BuildResult(outcomes []SourceOutcome) v1alpha1.KubeseerResult
```

`DefaultLimits` owns positive implementation defaults in one place: 1,000
groups, 10,000 accepted contributions, 10,000 collected values, 10,000
distinct values, and 10,000 emitted provenance entries per aggregate. Tests
inject smaller limits through `Limits`; no environment variable or global
mutable override changes semantics.

### Planner

The planner performs a complete declaration pass before any resource outcome
is evaluated. It builds a field-name/type index, walks declarations in
lexicographic aggregate-name order, preserves `groupBy` order, and records an
independent plan or failure for each aggregation. Equivalent declarations and
limits produce semantically equivalent plans; no cache is required.

An `AggregatePlan` contains source ID, aggregate name, function, target field
and logical type, ordered grouping fields and types, provenance policy,
effective average options, and a copied `Limits` value. For `average`, omitted
precision and mode become `6` and `halfEven`; explicit values are retained. For
all other functions, either option being present yields
`invalid-average-options`.

### Typed Key Service

`internal/typedoutput` exposes package-level helpers over its immutable
`Match` value:

```go
func CanonicalKey(valueType v1alpha1.KubeseerValueType, match Match) ([]byte, error)
func ExactRational(valueType v1alpha1.KubeseerValueType, match Match) (*big.Rat, error)
func MatchFromExactNumber(value *big.Rat, scale int32, mode RoundingMode) (Match, error)
```

The exact signatures may use private typed aliases, but ownership remains the
same: canonical number, timestamp, duration, and quantity semantics are
implemented once in `typedoutput`. Canonical keys include a type tag and a
length-delimited normalized payload, so tuples cannot collide by
concatenation. The sortable token uses the approved type comparison semantics;
it is not a locale-sensitive display string.

### Evaluator

`SourceInput` pairs one `PlanOutcome` with one `operators.SourceOutcome`.
Mismatched source IDs fail before resource traversal. An unsuccessful operator
source is carried through atomically and yields no aggregate values. Rejected
resources are ignored. Unsuccessful resources are omitted from contributions
and retained as sanitized source-level resource failures.

Accepted resources are deduplicated by UID before contribution processing.
Each resource must have complete API version, Kind, name, and UID provenance;
cluster-scoped resources use an empty namespace. Contributions are normalized
into deterministic order by namespace, name, UID, then typed match index.

For each aggregate and accepted resource, every grouping field must contain
exactly one non-null typed match. A missing, null, failed, or multi-match key
creates a resource-scoped failure for that aggregate only. An absent or
null-only target contributes nothing; a target field error creates a
resource-scoped failure; each non-null target match creates one contribution.

Groups are indexed by canonical typed tuple but never emitted from map order.
After reduction they are sorted lexicographically by the canonical tuple. An
ungrouped aggregate always has one global group, including when it has no
contributions, so empty reducer semantics remain observable.

### Reducers And Average Rounding

The closed reducer dispatch applies these rules:

- `collect` returns every contribution in deterministic order.
- `count` returns the exact non-null contribution count as `integer`.
- `sum` uses checked `int64` arithmetic for integer and duration, exact
  rational/decimal arithmetic for number, and exact normalized base-unit
  arithmetic for quantity.
- `min` and `max` use typed comparison and retain the selected typed value.
- `average` converts integer or number values to exact rationals, computes the
  exact quotient of sum and count, and rounds exactly once for projection.
- `first` and `last` select from deterministic contribution order.
- `distinct` keys by logical type plus canonical typed payload, retains values
  in first-occurrence order, and accumulates all contributing provenance for
  each retained value.

Average rounding multiplies the absolute rational by `10^precision`, divides
the numerator by the denominator, and decides whether to increment the integer
quotient from the exact remainder. `halfEven` increments above one half and at
an exact midpoint only when the retained integer is odd;
`halfAwayFromZero` increments at or above one half; `towardZero` never
increments; `awayFromZero` increments for any non-zero remainder. The original
sign is then restored. This symmetric absolute-value algorithm handles
negative midpoints without sign-specific branches. Projection inserts the
decimal point, canonicalizes negative zero to zero, and strips insignificant
trailing fractional zeros.

Empty `collect` and `distinct` results are value-state `values` with no
matches. Empty `count` is integer zero. Empty `sum` is the exact typed zero.
Empty `min`, `max`, `average`, `first`, and `last` use value-state `absent`.

### Cardinality Accounting

Every aggregate owns independent counters. The evaluator checks the next
group, contribution, collected/distinct output, or provenance entry before
accepting it. Crossing a ceiling discards all partial groups for that aggregate
and returns `cardinality-exceeded`; no truncated value is projected. The
counter and traversal order are deterministic, so equivalent input reaches the
same failure boundary. Sibling aggregates and sources continue.

### Cancellation

`EvaluateBatch` checks the context before every source, resource, aggregate,
and contribution loop boundary. Outcomes completed before cancellation are
preserved. The active source and every unstarted source receive an
`aggregation-interrupted` outcome with their source IDs; no partial aggregate
from the interrupted source is published.

### Result And Status Integration

`BuildResult` first reuses `operators.BuildResult` for the existing source and
resource projection. It then attaches ordered aggregate outcomes to each
matching `KubeseerSourceResult`. Sources without declarations therefore retain
their prior public shape, and source/resource outcomes remain authoritative.

`internal/status` extends its existing normalizer and degradation detector for
aggregate slices, group keys, values, failures, and provenance. Nil and empty
collections normalize consistently. Aggregate semantic changes naturally
participate in the existing full-result hash; equivalent aggregate outcomes
produce the same normalized hash input. An aggregate in `degraded` or `error`,
or carrying failures, marks the candidate result degraded. Existing summary
counts continue to derive successful/failed sources and matched resources from
source states and raw resource results; aggregates do not inflate resource
counts.

## Data Models

### Public Aggregate Outcome

`KubeseerSourceResult` gains an optional atomic `aggregates` list. The public
model is structural and uses existing `KubeseerTypedMatch` payload branches:

```go
type KubeseerAggregateResult struct {
    Name     string
    Function KubeseerAggregationFunction
    Field    string
    State    KubeseerAggregateState // values, degraded, error
    Groups   []KubeseerAggregateGroup
    Failures []KubeseerAggregateResourceFailure
    Error    *KubeseerResultError
}

type KubeseerAggregateGroup struct {
    Keys         []KubeseerAggregateKey
    Value        KubeseerAggregateValue
    Contributors []KubeseerResourceProvenance
}

type KubeseerAggregateValue struct {
    Type    KubeseerValueType
    State   KubeseerAggregateValueState // absent, values
    Matches []KubeseerAggregateMatch
}

type KubeseerAggregateMatch struct {
    Value        KubeseerTypedMatch
    Contributors []KubeseerResourceProvenance
}
```

An aggregate state of `values` represents successful evaluation, `degraded`
represents computed groups beside resource-scoped failures, and `error`
represents an unsuccessful aggregate with no groups. Keys carry field name,
logical type, and one non-null typed match. `KubeseerResourceProvenance`
contains API version, Kind, namespace, name, and UID. Resource failures contain
the same provenance plus a sanitized `KubeseerResultError`.

When provenance is disabled, all contributor slices are omitted. When enabled,
`collect` associates one contributor with each emitted match, `distinct`
associates all contributors for each distinct match, and other reducers place
deduplicated contributors in deterministic order on the group.

Generated deep-copy and CRD artifacts cover every new slice, pointer, enum,
minimum/maximum, list-map, and atomic-list marker. No arbitrary map,
`RawExtension`, or preserve-unknown region is introduced.

### Internal Outcome

Internal outcomes retain immutable typed matches and causes until the public
projection boundary. `AggregateOutcome` contains declaration identity, state,
ordered groups, ordered resource failures, and an optional aggregate error.
`SourceOutcome` contains the original operator source outcome plus ordered
aggregate outcomes, allowing the projection adapter to preserve upstream
failures without translating and reparsing them.

Provenance is mandatory internally even if omitted publicly. A contribution is
the target typed match, its match index, and complete immutable provenance.
Group keys likewise retain typed matches in declaration order rather than only
their serialized key token.

## Error Handling

`AggregateError` carries source ID, aggregate name, function, relevant field
name, optional resource provenance, stable reason, safe message, and an
internal wrapped cause. Projection exposes only reason and safe message.

Stable reasons include:

- `duplicate-aggregate`, `unknown-field`, `duplicate-group-field`,
  `unsupported-group-type`, `incompatible-function`, and
  `invalid-average-options` for planning;
- `invalid-input` for source-ID mismatch or incomplete provenance;
- `invalid-group-key` and `target-field-error` for resource-scoped failures;
- `overflow` and `cardinality-exceeded` for aggregate-scoped evaluation;
- `aggregation-interrupted` for cancellation or deadline expiry.

Planning continues across declarations and evaluation continues across
independent aggregates and sources. A degraded aggregate contains complete
results from successful resources plus ordered failures. An aggregate-scoped
error contains no partial groups. An upstream unsuccessful source is preserved
unchanged and does not run aggregation.

Messages identify only safe boundaries and never include paths, operands,
typed values, raw objects, or API response bodies. Wrapped causes are available
to internal control flow but are not serialized or logged by this component.

## Security Considerations

- Aggregation cannot broaden access because it receives no Kubernetes client,
  discovery client, REST mapper, credentials, or namespace-selection input.
- Only accepted operator resources contribute. Rejected resources remain
  invisible, and an atomic failed source cannot leak temporary partial values.
- Complete provenance prevents same-name resources in different namespaces
  from being conflated; UID deduplication prevents duplicate observations from
  amplifying a result.
- Structural schemas and closed enums bound the accepted shape. Central limits
  bound fan-out, retained values, distinct sets, and optional provenance.
- Diagnostics and logs must contain identifiers and reason codes only. Values,
  paths, operands, object bodies, and credentials are forbidden.
- Exact integer/rational arithmetic avoids floating-point instability and
  explicit checked conversions prevent silent overflow.

## Failure Modes And Tradeoffs

- Runtime average defaults are not written back into the manifest. This keeps
  conditional validation correct, but API readers see omission rather than the
  effective values; the resulting aggregate is nevertheless deterministic and
  tests expose the effective behavior.
- Sequential evaluation favors auditable ordering and failure isolation over
  maximum throughput. Cardinality limits cap worst-case work, and concurrency
  can be added later behind the same outcomes.
- Aggregate-level failure on a limit or overflow discards partial groups. This
  sacrifices partial visibility to avoid presenting truncation as complete.
- All accepted typed matches are ordered before reduction. This requires
  bounded temporary memory, but gives one clear order for every reducer and
  deterministic cancellation/limit behavior.
- Public provenance can materially enlarge status. It is opt-in and separately
  limited; internal provenance remains mandatory for correctness.
- Quantity arithmetic uses normalized base-unit magnitude. Projection uses the
  canonical quantity contract and does not promise preservation of original
  display units.

## Testing Strategy

Tests follow the constitution's higher-layer policy; no dedicated isolated
unit-test suite is added for this package.

### API And Envtest

API integration tests generate and install the CRD, then verify aggregation
list-map behavior, function/rounding enums, precision range, structural schema,
round trips, and deep copies. They prove omitted average options persist as
omitted while runtime planning applies precision `6` and `halfEven`, and prove
that the same options fail planning on non-average functions. Status envtest
persists and retrieves typed keys, value states, failures, ordering, and
requested provenance.

### Cross-Module Integration

Integration fixtures exercise real field extraction, typed conversion,
operator execution, and aggregation together. The matrix covers every reducer
and compatible type, no-group and multi-field grouping, absent/null/error
targets, invalid group keys, accepted/rejected/unsuccessful resources, atomic
source failures, duplicate UIDs, and equal names across namespaces.

Numeric cases include exact decimal sums, integer/duration overflow, quantity
unit normalization, `1 / 3`, precision `0` and `18`, positive and negative
midpoints, all four rounding modes, negative zero, and trailing-zero removal.
Ordering fixtures randomize input processing while asserting identical plans,
groups, values, distinct retention, provenance, and errors.

Limit tests inject small ceilings and cross each boundary by one, proving no
truncated aggregate and preservation of siblings. Cancellation tests cover
before-batch, active-source, and later-source interruption while preserving
already completed outcomes.

### Reconciliation And Status Integration

Controller-level integration proves that aggregate changes alter the semantic
result hash, equivalent outcomes are no-ops, aggregate failures degrade status,
raw resource results remain present, and summary resource counts do not count
aggregate groups or values. No test uses ambient kubeconfig or cloud
credentials.

## Verification Plan

1. Run generation and repository policy checks, including CRD/deep-copy drift,
   formatting, linting, and static boundary checks through `make verify`.
2. Run the existing Go and cross-module integration suites with writable local
   Go caches through the repository test targets.
3. Run API/envtest suites with explicit `KUBEBUILDER_ASSETS` and no ambient
   kubeconfig.
4. Run compatibility checks proving manifests without aggregation remain valid
   and preserve prior result shape.
5. Inspect generated CRD item schemas for aggregation list-map keys, atomic
   group lists, enum values, and precision bounds; verify no preserve-unknown
   region appears.
6. Run race-enabled aggregation integration coverage where supported and prove
   deterministic results across repeated randomized input ordering.
7. Use static import/search checks to confirm `internal/aggregation` has no
   Kubernetes client, discovery, dynamic-client, or status-writer dependency.

## Requirement Coverage

| Requirement | Design coverage |
| --- | --- |
| `R1` | Public API Declarations defines the per-source list-map, closed functions, grouping/provenance fields, conditional runtime defaults, precision bounds, serialization, deep-copy, and structural schema behavior. |
| `R2` | Planner defines complete pre-evaluation validation, immutable ordered plans, independent declaration failures, compatibility checks, stable errors, and the no-cache path. |
| `R3` | Typed Key Service and Evaluator define exact typed equality, group-key cardinality, canonical tuples, global grouping, and resource-local key failures. |
| `R4` | Reducers And Average Rounding defines the compatibility matrix, contribution rules, exact sums, all reducers, empty states, exact rational mean, four rounding modes, and decimal normalization. |
| `R5` | Public Aggregate Outcome and Internal Outcome define structural typed groups, values, states, complete internal provenance, opt-in public provenance, round trips, and deep copies. |
| `R6` | Evaluator, Cancellation, and Error Handling define upstream atomicity, unsuccessful-resource preservation, degraded partial results, sibling isolation, interruption, and sanitized diagnostics. |
| `R7` | Evaluator, Typed Key Service, and Reducers define UID deduplication and deterministic source, aggregate, group, contribution, distinct, and provenance ordering. |
| `R8` | Cardinality Accounting defines central positive ceilings, plan binding, pre-acceptance counters, fail-without-truncation semantics, deterministic boundaries, and sibling preservation. |
| `R9` | Architecture and Result And Status Integration define immutable inputs, no Kubernetes I/O, preserved raw results, additive aggregate projection, hash normalization, compatibility, and invalid-input checks. |
| `NFR1` | Typed Key Service and Reducers use explicit logical types, exact arithmetic, checked overflow, and explicit one-time average rounding without coercion. |
| `NFR2` | Planner, Evaluator, and Cardinality Accounting define stable ordering, equality, rounding, deduplication, limits, and reason codes independently of map or processing order. |
| `NFR3` | Internal Outcome mandates complete immutable provenance; Public Aggregate Outcome defines exact opt-in projection for collections, distinct values, and reducers. |
| `NFR4` | Planner, Evaluator, Cancellation, and Error Handling isolate declaration, resource, aggregate, source, sibling-source, limit, and interruption failures. |
| `NFR5` | DefaultLimits and Cardinality Accounting bound every output-amplifying dimension and prohibit truncation. |
| `NFR6` | Error Handling and Security Considerations constrain diagnostics to safe identifiers and reason codes. |
| `NFR7` | Public API Declarations, Public Aggregate Outcome, and Result Integration preserve additive structural `v1alpha1` compatibility and generated artifacts. |
| `NFR8` | Testing Strategy and Verification Plan cover real cross-module collaboration, envtest admission/status, deterministic reducers, failure boundaries, and compatibility. |
