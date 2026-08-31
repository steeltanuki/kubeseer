---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-30T13:12:34Z
last_modified: 2026-08-30T13:12:34Z
approved_fingerprint: sha256:802b5ac2d8148015b1cb6b675eac4f42ee7faca3a6c87895a5dbabdf5f95b192
source_requirements_approved_at: 2026-08-30T12:49:25Z
source_requirements_fingerprint: sha256:0117a96751f49bb51ed517d9864733ea39b7efdd00a68967c26e29f745bcd5ef
---

# Feature Design

## Overview

Implement `value-operators` as additive `api/v1alpha1` declarations plus a new
pure package, `internal/operators`, between `internal/typedoutput` and result
publication. The package compiles every field's ordered declarations before it
examines typed resources, converts configured operands through the existing
typed-output conversion rules, and evaluates immutable plans against immutable
typed outcomes.

The public operand is an explicit structural tagged union. Its payload branch
declares the logical type, so planning never infers a type from the target
field. Object and list operands use JSON text in dedicated branches, matching
the existing structural status boundary without introducing a
preserve-unknown subtree. Operator names are an API enum; compatibility,
arity, operand conversion, and regular-expression compilation remain source
planning checks because they depend on the field declaration.

Evaluation produces one immutable operator outcome for each source and each
input resource. Accepted resources expose transformed typed fields, rejected
resources retain an internal successful decision but contribute no downstream
values, and unsuccessful resources retain provenance plus a sanitized failure
while discarding temporary transformations. A pure result adapter publishes
accepted resources and unsuccessful resource diagnostics; reconciliation still
owns scheduling, conditions, retries, and status-write policy.

<!-- assumed: operand payload branches explicitly identify their logical type, rather than carrying a second independent type property, because a branch/type disagreement would add an invalid state without improving R3 validation (source: approved R1.AC10 and R3.AC11-R3.AC15) -->
<!-- assumed: explicit null is represented by an operand with state `null` and no payload, then rejected during planning; omission remains distinguishable at the enclosing pointer/list level (source: approved R3.AC7-R3.AC10 and R3.AC15) -->
<!-- assumed: unsuccessful resource outcomes are represented in the public result by provenance plus an optional resource-level error and no fields, while rejected resources are omitted; this preserves diagnostics without confusing predicate rejection with failure (source: approved R7.AC2-R7.AC7 and R7.AC12-R7.AC14) -->
<!-- assumed: exact equality and ordering belong to typed-output domain helpers because the private typed value representation, not the operator layer, owns numeric and quantity normalization (source: approved C3-C5 and R4.AC10-R4.AC16) -->
<!-- assumed: the first implementation has no operator-plan cache; deterministic recompilation is the complete cache-miss path required by R2.AC13 (source: approved R2.AC13 and C10) -->

## Architecture

```text
api/v1alpha1 KubeseerSource.Fields[].Operators
                         │
                         ▼
             operators.CompileBatch
             │ valid immutable plans
             │ or source-local failures
             ▼
typedoutput.SourceOutcome ──────── upstream source error ──────┐
             │                                                  │
             ▼                                                  │
             operators.EvaluateBatch                            │
             │                                                  │
             ├─ accepted: transformed typed fields              │
             ├─ rejected: successful decision, no values        │
             └─ unsuccessful: provenance + sanitized failure    │
                         │                                      │
                         ▼                                      │
                 operators.BuildResult ◄────────────────────────┘
                         │
                         ▼
              api/v1alpha1.KubeseerResult
                         │
                         ▼
          reconciliation/status publication policy
```

`CompileBatch` compiles every source before batch evaluation starts. Within a
source it inspects all declarations, orders field plans lexicographically, and
retains operator declaration order. Any failure makes that source plan
unusable, but does not affect other source plans. Regular expressions and
converted operands are stored in the immutable plan, so evaluation performs no
configuration parsing.

`EvaluateBatch` walks sources and resources in input order. Each resource uses
private working field outcomes; transformations replace only their field's
working outcome, while predicates accumulate an implicit AND decision. A false
predicate can stop further evaluation of that resource because later operators
cannot change rejection, but planning has already validated the complete source
and no partial values escape. Errors discard the entire resource's working
fields. Cancellation preserves completed source outcomes, marks the active and
unstarted sources interrupted, and performs no evaluation for those unstarted
sources.

The result adapter is pure and validates outcome invariants. It delegates typed
field serialization and pass-through source errors to narrow helpers in
`internal/typedoutput`; there is no reverse dependency from typed output to
operators. Rejected resources are absent from `KubeseerResult.Resources`.
Unsuccessful resources are present with full provenance, no fields, and one
resource-level error so existing result degradation can observe the failure.

## Options Considered

### Option A — Dedicated operator package with structural tagged operands

- Summary: Add a closed public declaration model and a pure planner/evaluator
  package over immutable typed outcomes.
- Why chosen: It keeps extraction and conversion semantics upstream, supports
  deterministic early validation, avoids API I/O, and gives aggregation a
  stable accepted/rejected/unsuccessful boundary.

### Option B — Add operators directly to `internal/typedoutput`

- Summary: Extend conversion so each field is filtered or transformed as soon
  as its native matches are converted.
- Why rejected: A later operator failure would be mixed with conversion, source
  planning could become data-dependent, and resource-wide implicit AND would
  couple otherwise field-local conversion to downstream filtering.

### Option C — Evaluate API operands dynamically for every resource

- Summary: Dispatch on operator strings and convert operands during each
  resource evaluation.
- Why rejected: Invalid declarations could produce partial data-dependent
  results, regular expressions would be repeatedly compiled, and reason codes
  could vary with observed cardinality.

### Option D — Use CEL or an opaque expression payload

- Summary: Replace the closed operator list with a general expression engine or
  arbitrary JSON arguments.
- Why rejected: It violates the approved scope, weakens the structural schema,
  introduces a second type/coercion language, and makes deterministic cost and
  confidentiality substantially harder.

## Simplicity And Elegance Review

- Simplest viable shape: one closed declaration union, one planner, one
  evaluator, one immutable outcome model, and one result adapter; no cache,
  registry plugins, expression AST, reflection dispatch, or concurrency.
- Coupling check: `internal/operators` imports `api/v1alpha1` and
  `internal/typedoutput`. Upstream packages never import operators, and
  operators performs no Kubernetes, discovery, selection, extraction, policy,
  or authorization work.
- Shared semantics: configured values use one narrow typed-output conversion
  entry point, while exact equality and ordering use typed-output comparison
  helpers. The operator package does not duplicate numeric normalization.
- Future-proofing: the closed operator enum and compatibility table can grow
  only through a reviewed API change. OR groups, cross-field references,
  aggregation, caching, limits, and parallel evaluation are intentionally
  deferred.
- Challenge result: folding predicates into typed conversion removes one
  package but breaks early whole-source planning and resource-wide decisions.
  The dedicated package is the smallest boundary that preserves the approved
  semantics.

## Components And Interfaces

### Public Operator Declaration Contract

- Purpose: add an ordered operator chain to every `KubeseerField`.
- Shape: `Operators []KubeseerOperator` with `json:"operators,omitempty"` and
  atomic-list semantics; nil and empty both mean identity and have no API
  default.
- Operator entry: required enum-valued `Operator`, optional pointer `Value`, and
  optional atomic `Values` list. Admission rejects names outside the approved
  lower-camel-case set.
- Operand: required `State` plus one optional typed payload branch. `state:
  value` requires exactly one branch during planning; `state: null` requires no
  branch and is then rejected as `invalid-operand`. Structural admission does
  not attempt the field-dependent arity or compatibility matrix.
- Text encodings: `numberValue` retains exact decimal text;
  `timestampValue`, `durationValue`, and `quantityValue` retain their approved
  textual inputs; `objectValue` and `listValue` contain JSON text whose
  top-level kind is checked before typed conversion.
- Generated artifacts: deep-copy code and the CRD OpenAPI schema are
  regenerated. Tests inspect the generated item schema, enum, atomic ordering,
  and absence of preserve-unknown regions.
- Requirements: `R1`, `R3`, `NFR1`, `NFR6`.

### Typed-Output Operator Bridge

- Purpose: reuse typed conversion and private value semantics without exposing
  mutable payloads.
- Configured conversion: a narrow pure function accepts a declared field name,
  field type, and one decoded native operand, then invokes the same conversion
  core used for observed matches. Operators map any conversion failure to a
  sanitized `invalid-operand` planning failure.
- Comparison: typed-output equality and three-way ordered comparison helpers
  validate matching logical types and compare their normalized private values.
  They never compare status text or convert through `float64`.
- Transformation: an immutable replacement helper creates a new field outcome
  from an existing field identity/type and defensive copies of approved
  matches. It cannot mutate the upstream field or change its identity/type.
- Projection: pure typed-output helpers serialize transformed fields/resources
  and preserve existing upstream source-error mapping. They perform no status
  write.
- Requirements: `R3`, `R4`, `R6`, `R7`, `NFR1`, `NFR3`.

### Operator Planner

- Purpose: validate all declarations before any resource for that source is
  evaluated.
- Inputs/Outputs: `CompileSource(v1alpha1.KubeseerSource) PlanOutcome` and
  `CompileBatch([]v1alpha1.KubeseerSource) []PlanOutcome`.
- Source plan: immutable source ID plus field plans sorted by field name. Each
  field plan stores its field identity/type and an ordered operator slice.
- Operator plan: normalized enum kind, declaration index, converted immutable
  operand(s), and a compiled `regexp.Regexp` only for `matches`.
- Validation order: identity, supported name, compatibility, arity, operand
  state/branch, configured conversion, then regular-expression compilation.
  All declarations are inspected so deterministic failures can be retained,
  but any failure invalidates the entire source plan.
- Cache: none. Every call recompiles current declarations deterministically.
- Requirements: `R2`, `R3`, `R8`, `NFR1`, `NFR2`, `NFR4`, `NFR5`.

### Predicate And Transformation Evaluator

- Purpose: apply one valid field plan to one immutable typed field outcome.
- Predicates: return only true/false and leave the working field unchanged.
  Positive comparisons/string/membership use existential matching over
  non-null values. `ne` and `notIn` require a non-empty non-null set and return
  true only when no member equals the operand set. Presence counts all matches,
  including null and empty composite/scalar values.
- Transformations: `default` replaces absence with one configured match and
  replaces each null in place; `coalesce` selects the first non-null match,
  otherwise one null, while preserving absence.
- Errors: an operator-bearing typed field in the error state, any identity/type
  mismatch, or an impossible internal union produces an unsuccessful resource
  outcome rather than false. Temporary transformations are discarded.
- Requirements: `R4`, `R5`, `R6`, `R7`, `R8`, `NFR2`, `NFR3`, `NFR4`.

### Batch Operator Engine And Outcomes

- Purpose: preserve source/resource isolation, deterministic order, and
  cancellation behavior.
- Input: `SourceInput{Plan PlanOutcome, Typed typedoutput.SourceOutcome}` for
  each declared source, in declaration order.
- Output: one immutable `operators.SourceOutcome` per input. A successful source
  contains one resource outcome per typed resource; an upstream source error is
  retained unchanged; a planning or interruption failure is source-scoped.
- Resource state: exactly `accepted`, `rejected`, or `unsuccessful`. Accepted
  stores provenance and transformed fields; rejected stores provenance and the
  successful predicate decision but exposes no fields; unsuccessful stores
  provenance and failure but no fields.
- Ordering: sources and resources retain input order; field plans and returned
  fields remain lexicographic; matches retain input order except for the
  explicitly approved `default` and `coalesce` changes.
- Cancellation: context checks occur before each source, resource, field, and
  operator. The active source becomes interrupted; completed sources remain;
  later sources receive interrupted outcomes without evaluation.
- Requirements: `R2`, `R6`, `R7`, `R8`, `NFR2`, `NFR4`, `NFR5`.

### Structural Result Adapter And Pipeline Composition

- Purpose: project operator outcomes into the existing value result and compose
  the stage into reconciliation without taking over status policy.
- Input/Output: `BuildResult([]operators.SourceOutcome)
  (v1alpha1.KubeseerResult, error)`.
- Accepted resources: serialize full provenance and transformed fields.
- Rejected resources: omit them from the downstream resource collection; a
  source with only rejected resources remains a successful empty source.
- Unsuccessful resources: serialize provenance, no fields, and a sanitized
  optional `KubeseerResultError` on `KubeseerResourceResult`. Existing status
  derivation is extended to recognize that error as degradation; condition and
  publication decisions remain unchanged.
- Planning failures: serialize deterministic field errors and cause the
  existing reconciliation configuration assessment to be invalid by adding
  operator planning to `configurationOutcome`.
- Pipeline: `typedoutput.ConvertBatch` feeds `operators.EvaluateBatch`, whose
  result feeds this adapter. Lease/cancellation checks remain before and after
  the new pure stage.
- Requirements: `R7`, `R8`, `NFR4`, `NFR5`, `NFR7`.

## Data Models

The public shape is fixed by the following pseudocode; controller-generation
comments and validation markers provide the structural schema.

```go
type KubeseerOperatorName string

const (
    OperatorEq         KubeseerOperatorName = "eq"
    OperatorNe         KubeseerOperatorName = "ne"
    OperatorGt         KubeseerOperatorName = "gt"
    OperatorGte        KubeseerOperatorName = "gte"
    OperatorLt         KubeseerOperatorName = "lt"
    OperatorLte        KubeseerOperatorName = "lte"
    OperatorContains   KubeseerOperatorName = "contains"
    OperatorStartsWith KubeseerOperatorName = "startsWith"
    OperatorEndsWith   KubeseerOperatorName = "endsWith"
    OperatorMatches    KubeseerOperatorName = "matches"
    OperatorExists     KubeseerOperatorName = "exists"
    OperatorNotExists  KubeseerOperatorName = "notExists"
    OperatorIn         KubeseerOperatorName = "in"
    OperatorNotIn      KubeseerOperatorName = "notIn"
    OperatorDefault    KubeseerOperatorName = "default"
    OperatorCoalesce   KubeseerOperatorName = "coalesce"
)

type KubeseerField struct {
    Name      string              `json:"name"`
    Path      string              `json:"path"`
    Type      KubeseerValueType   `json:"type,omitempty"`
    Operators []KubeseerOperator `json:"operators,omitempty"`
}

type KubeseerOperator struct {
    Operator KubeseerOperatorName      `json:"operator"`
    Value    *KubeseerOperatorOperand  `json:"value,omitempty"`
    Values   []KubeseerOperatorOperand `json:"values,omitempty"`
}

type KubeseerOperatorOperand struct {
    State          KubeseerMatchState `json:"state"`
    StringValue    *string            `json:"stringValue,omitempty"`
    IntegerValue   *int64             `json:"integerValue,omitempty"`
    NumberValue    *string            `json:"numberValue,omitempty"`
    BooleanValue   *bool              `json:"booleanValue,omitempty"`
    TimestampValue *string            `json:"timestampValue,omitempty"`
    DurationValue  *string            `json:"durationValue,omitempty"`
    QuantityValue  *string            `json:"quantityValue,omitempty"`
    ObjectValue    *string            `json:"objectValue,omitempty"`
    ListValue      *string            `json:"listValue,omitempty"`
}

type KubeseerResourceResult struct {
    APIVersion string                `json:"apiVersion"`
    Kind       string                `json:"kind"`
    Namespace  string                `json:"namespace,omitempty"`
    Name       string                `json:"name"`
    UID        types.UID             `json:"uid"`
    Fields     []KubeseerFieldResult `json:"fields,omitempty"`
    Error      *KubeseerResultError  `json:"error,omitempty"`
}
```

The internal model remains private and immutable:

```go
type PlanOutcome struct {
    sourceID string
    plan     *SourcePlan
    failures []*OperatorError
}

type SourcePlan struct {
    sourceID string
    fields   []FieldPlan // lexicographic by name
}

type FieldPlan struct {
    name      string
    typeName  v1alpha1.KubeseerValueType
    operators []OperatorPlan // declaration order
}

type OperatorPlan struct {
    index    int
    kind     Kind
    operand  *typedoutput.Match
    operands []typedoutput.Match
    pattern  *regexp.Regexp
}

type ResourceOutcome struct {
    state      ResourceState // accepted, rejected, unsuccessful
    provenance selection.Provenance
    fields     []typedoutput.FieldOutcome
    failure    *OperatorError
}
```

All constructors defensively copy slices, typed matches, errors, regular
expressions, and composite payloads. Accessors return defensive copies. Plans
never retain pointers into API declarations.

### Compatibility And Arity Matrix

| Operators | Allowed field types | Operand contract |
| --- | --- | --- |
| `eq`, `ne` | string, integer, number, boolean, timestamp, duration, quantity | exactly one non-null `value` |
| `gt`, `gte`, `lt`, `lte` | integer, number, timestamp, duration, quantity | exactly one non-null `value` |
| `contains`, `startsWith`, `endsWith`, `matches` | string | exactly one non-null `value` |
| `exists`, `notExists` | all supported types | no operand |
| `in`, `notIn` | string, integer, number, boolean, timestamp, duration, quantity | non-empty non-null `values` |
| `default` | all supported types | exactly one non-null `value` |
| `coalesce` | all supported types | no operand |

For a value-state operand, the single populated payload branch must match the
field's declared logical type. Object/list text is decoded with JSON number
preservation, checked for the required top-level kind, and then passed to the
typed-output configured-value converter. Duplicate `in`/`notIn` operands may
remain in the immutable plan because linear membership preserves the required
semantics; deduplication is neither required nor observable.

## Evaluation Algorithms

### Source Planning

1. Validate source and field identity using the same declaration identities as
   typed output.
2. Sort field declarations by field name without mutating the API object.
3. For every operator, validate name, field compatibility, and exact arity.
4. Validate operand state and exactly one type branch; reject null and branch
   mismatches without including payloads in diagnostics.
5. Decode the configured branch and convert through the shared typed-output
   conversion core. Preserve input order for `values`.
6. Compile a `matches` operand with `regexp.Compile` and retain the compiled
   expression.
7. Return an immutable source plan only when no declaration failed. Otherwise
   return deterministic failures ordered by field name then operator index.

### Resource Evaluation

1. Validate source ID, resource provenance, field name, and declared type before
   applying operators.
2. Copy the typed fields into private working outcomes in lexicographic order.
3. For each field plan and operator in order, check context, transform the
   working field or evaluate its predicate, and retain the first false decision.
4. If a planned field's typed outcome is unsuccessful, or any invariant fails,
   discard all working fields and return one unsuccessful resource outcome.
5. If every predicate is true, return accepted with transformed fields. If any
   predicate is false, return rejected with no downstream fields.

Exact predicates use typed-output equality/comparison. String predicates use
the non-null string accessor and the precompiled regular expression. Presence
uses field cardinality rather than payload truthiness. Transformations reuse
the immutable configured match captured in the plan.

## Error Handling

`OperatorError` contains only stable identity and sanitized metadata:

```go
type Reason string

const (
    ReasonUnsupportedOperator  Reason = "unsupported-operator"
    ReasonIncompatibleOperator Reason = "incompatible-operator"
    ReasonInvalidArity         Reason = "invalid-arity"
    ReasonInvalidOperand       Reason = "invalid-operand"
    ReasonInvalidPattern       Reason = "invalid-pattern"
    ReasonInvalidInput         Reason = "invalid-input"
    ReasonOperatorInterrupted  Reason = "operator-interrupted"
)

type OperatorError struct {
    SourceID       string
    FieldName     string
    OperatorIndex int
    OperatorName  string
    Provenance    *selection.Provenance
    Reason        Reason
    Message       string
    cause         error
}
```

- Planning failures identify source, field, zero-based declaration index, name,
  and reason, but never include an operand or path.
- Evaluation failures additionally retain complete provenance when available,
  but never include typed/extracted values, field paths, or resource contents.
- Public messages are selected from fixed templates. Wrapped causes are
  available only for `errors.Is`/`errors.As` and are not serialized or logged by
  the package.
- A typed source error is preserved unchanged and bypasses all operator work.
- A source-plan identity mismatch fails that source. A field identity/type
  mismatch fails only the affected resource and preserves sibling resources.
- Context cancellation/deadline uses `operator-interrupted`; completed sources
  remain unchanged and unstarted sources receive deterministic interruption
  outcomes.

## Security Considerations

- Operator names and operand branches are closed structural schemas. No
  executable code, template, unknown subtree, or dynamic plugin is accepted.
- Evaluation consumes only authorized, selected, extracted, and converted
  outcomes; it cannot perform reads or bypass upstream capability checks.
- Regex uses Go's standard library and is compiled once per plan. Product-wide
  pattern and cardinality limits remain deferred to `performance-and-limits`.
- Diagnostics use stable templates and identities only. Operands, paths,
  observed values, and resource bodies are prohibited from messages.
- Defensive copies prevent API declaration, operand, match, or composite-value
  mutation from changing a prepared plan or completed outcome.

## Failure Modes And Tradeoffs

- Failure mode: one invalid declaration could otherwise filter earlier
  resources before the defect is discovered.
  Mitigation: compile every operator for the source before resource evaluation;
  any failure invalidates the source plan.
- Failure mode: exact number or quantity comparison could drift through display
  text or floating point.
  Mitigation: compare normalized private typed values through typed-output
  helpers and never parse via `float64` in operators.
- Failure mode: a late field failure could expose partially transformed values.
  Mitigation: transformations live in resource-local working outcomes and the
  unsuccessful branch retains no fields.
- Failure mode: predicate rejection could be mistaken for degraded execution.
  Mitigation: rejection is a successful resource state and is omitted from the
  result without an error; only unsuccessful outcomes carry errors.
- Failure mode: cancellation could alter already completed outcomes.
  Mitigation: outcomes become immutable at source completion; interruption only
  replaces the active source and marks later sources.
- Tradeoff: object/list operands are JSON text rather than recursive public
  value trees. This keeps OpenAPI structural and compact at the cost of a
  planning decode step.
- Tradeoff: evaluation is sequential. It gives deterministic interruption and
  ordering; concurrency belongs with approved performance limits.
- Tradeoff: recompiling every reconciliation does repeated bounded planning
  work, but avoids cache invalidation semantics and satisfies the required
  cache-unavailable path.

## Testing Strategy

The repository constitution does not maintain a dedicated unit-test layer.
Proof therefore uses genuine cross-module integration and public API envtest.

- Envtest/API integration proves operator-name admission rejection, structural
  operand branches, omitted versus empty identity chains, order/value
  round-trip, deep-copy isolation, generated CRD item validation, and the
  additive resource-error branch.
- Cross-module integration uses real `field-extraction` and
  `typed-output-model` outcomes to prove whole-source planning, the compatibility
  and arity matrix, configured conversion, exact comparisons, string/presence/
  membership predicates, transformations, ordering, source/resource isolation,
  provenance, sanitization, and cancellation.
- Reconciliation integration proves the pipeline order, operator planning in
  configuration assessment, accepted/rejected/unsuccessful result projection,
  empty successful sources, degraded result derivation, and preservation of
  existing condition/publication ownership.
- Existing repository suites prove identity chains do not change manifests or
  results that omit operators.

## Verification Plan

- Requirement proof: map every acceptance criterion to integration/envtest
  assertions and record evidence with `walden prove task` after Tasks approval
  and implementation.
- Static evidence: run formatting, generated-code/CRD regeneration checks,
  `go vet ./...`, and repository boundary searches for forbidden Kubernetes I/O
  and confidential diagnostic interpolation in `internal/operators`.
- Test evidence: run the repository's cross-module integration suite, API
  envtest suite with local control-plane assets, reconciliation integration
  suite, and then `go test ./...` with writable Go caches.
- Walden evidence: run `walden validate value-operators --json` and
  `walden status value-operators --json`; the Design may advance only after
  explicit review approval.
- Operational evidence: no new logs, metrics, alerts, or dashboards are needed;
  deterministic result errors and existing status degradation are the approved
  observable surfaces.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Public Operator Declaration Contract; envtest/API integration |
| `R2` | Operator Planner; Source Planning algorithm |
| `R3` | Public Operand union; Typed-Output Operator Bridge; compatibility/arity matrix |
| `R4` | Typed-Output comparison helpers; Predicate Evaluator |
| `R5` | Predicate Evaluator; Resource Evaluation algorithm |
| `R6` | Predicate And Transformation Evaluator; immutable field replacement |
| `R7` | Batch Operator Engine And Outcomes; Structural Result Adapter |
| `R8` | OperatorError model; cancellation and confidentiality controls |
| `NFR1` | Closed compatibility matrix and shared typed conversion/comparison helpers |
| `NFR2` | Immutable plans/outcomes, sequential evaluation, and ordering assertions |
| `NFR3` | Private typed values, exact comparison, and immutable transformations |
| `NFR4` | Source/resource outcome isolation and interruption algorithm |
| `NFR5` | Stable reason enum, fixed messages, provenance-only diagnostics |
| `NFR6` | Additive structural API, generated CRD, round-trip and deep-copy envtest |
| `NFR7` | Cross-module integration, envtest, and reconciliation verification |
