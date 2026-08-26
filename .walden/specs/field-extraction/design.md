---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T14:21:55Z
last_modified: 2026-08-26T14:21:55Z
approved_fingerprint: sha256:f0b276c47aee1be97d37cc9a0f3740aa2e9b62a7f70446c84c28abe39c80b999
source_requirements_approved_at: 2026-08-26T13:33:46Z
source_requirements_fingerprint: sha256:b8f6a3d692127affc45209de5bedd5c5a82a70eb4f0539b0214666af4430953b
---

# Feature Design

## Overview

Implement `field-extraction` as one internal Go package, `internal/extraction`, with three explicit stages: compile each source's public field declarations into an immutable plan, evaluate that plan against the successful `selection.SelectionOutcome`, and assemble source-atomic extraction outcomes in input order.

The package uses a small dedicated parser and evaluator for the exact grammar approved in `R2`. It does not wrap `k8s.io/client-go/util/jsonpath`: the pinned v0.36.0 parser accepts filters, unions, slices, recursive descent, template text, and identifiers that this feature excludes, while its evaluator drops explicit null branches and treats an out-of-range array index as an error. Those behaviors conflict with `R2`, `R4`, and `R6`; adapting around them would be more complex than implementing the three approved operation kinds directly. Source references: `go.mod`, `k8s.io/client-go@v0.36.0/util/jsonpath/parser.go`, and `k8s.io/client-go@v0.36.0/util/jsonpath/jsonpath.go`.

The initial implementation deliberately has no compiled-expression cache. Compilation is deterministic and local, and the approved requirements make caching optional. The immutable plan boundary leaves room for a cache later without exposing it in the API or changing outcomes.

<!-- assumed: a dedicated parser is preferable to validating and adapting client-go's broader AST because the approved grammar and null/missing/index semantics differ materially (source: approved R2, R4, R6 and pinned client-go v0.36.0 source) -->
<!-- assumed: the first implementation performs batch compilation and evaluation sequentially; concurrency and product-wide throughput limits remain owned by performance-and-limits (source: approved C7 and out-of-scope boundaries) -->
<!-- assumed: the parser accepts `\\'` and `\\\\` inside bracket-quoted keys so exact Kubernetes map keys containing a quote or backslash remain representable without adding general string templates (source: approved R2.AC4 exact-key contract) -->

## Architecture

```text
[]extraction.SourceInput
 | Source: v1alpha1.KubeseerSource
 | Selection: selection.SelectionOutcome
              |
              v
       extraction.Compiler
       | validates field IDs
       | parses exact subset
       | sorts fields by name
              v
       []CompileOutcome
       | immutable Plan or typed Error
              |
              | paired by input index and exact SourceID
              v
       extraction.Engine.ExtractBatch(ctx, ...)
                       |
                       | unsuccessful selection: pass through
                       | successful selection: evaluate resources
                       v
               temporary source-local results
                       |
            success ---+--- failure/cancellation
               |                    |
               v                    v
      ordered SourceOutcome   discard temporary values
```

`CompileBatch` completes planning for every source before `ExtractBatch` evaluates any selected object. This is stronger than the per-source minimum in `R3.AC1` and guarantees that expression validity never depends on selected-resource cardinality.

Evaluation starts with one branch containing `unstructured.Unstructured.Object`. Each compiled operation transforms the ordered branch sequence:

1. a property operation reads one `map[string]any` key and drops only branches where the key is absent;
2. an index operation reads one `[]any` element and drops only branches where the index is out of range;
3. a wildcard operation expands one `[]any` from index zero upward.

A non-null branch of the wrong container type fails the source immediately. A null is retained when it is the final match, but a later property or array operation against that null is a type mismatch. Looping over branches in order and appending wildcard elements in index order naturally produces depth-first, left-to-right results.

The engine accumulates all resource and field results in source-local temporary storage. It publishes them only after the entire source succeeds. Any extraction error or context interruption drops that storage while leaving completed outcomes for independent sources intact.

## Options Considered

### Option A — Dedicated restricted parser and native evaluator

- Summary: Parse the approved syntax directly into field, index, and array-wildcard operations; evaluate those operations over Kubernetes JSON values.
- Why chosen: It makes every accepted token explicit, distinguishes unsupported from malformed expressions, preserves null versus missing, implements branch-local absence, and avoids reflection or rendering.

### Option B — Wrap `client-go/util/jsonpath`

- Summary: Parse and evaluate with the Kubernetes JSONPath utility, then validate its AST and normalize results.
- Why rejected: The parser intentionally supports a much broader template language; the AST loses some source-syntax distinctions; `FindResults` skips nil branches; missing-key behavior is global; array bounds are errors. Correcting all differences would create a second semantic layer around the library and still couple the contract to reflection-heavy internals.

### Option C — JSON Pointer

- Summary: Translate field paths to RFC 6901-style pointers and evaluate with a pointer library.
- Why rejected: JSON Pointer has no array wildcard or multi-match semantics, so it cannot satisfy `R2.AC6`, `R2.AC7`, `R4.AC8`, or `R4.AC9` without inventing a parallel extension language.

## Simplicity And Elegance Review

- Simplest viable shape: one package, one immutable operation model, one sequential engine, and no cache or pluggable evaluator.
- Coupling check: the public API owns only declarations; `internal/extraction` depends on `api/v1alpha1` and `internal/selection`; selection does not import extraction, and typed output will consume extraction outcomes later.
- Future-proofing: operation kinds can be extended only through a future approved requirements change; caching can wrap deterministic compilation without changing `Plan`; output limits, concurrency, typed conversion, and status representation remain deferred.
- Challenge result: combining compilation and evaluation into one function would use fewer exported symbols but would hide the required immutable plan and make pre-resource validation harder to prove. The small explicit plan boundary is justified.

## Components And Interfaces

### Public API Field Declaration

- Purpose: extend `v1alpha1.KubeseerSource` with optional named extraction declarations.
- Shape: `Fields []KubeseerField` with `json:"fields,omitempty"`, Kubernetes list-map semantics, and `name` as the list-map key. `KubeseerField` contains required `Name` and `Path` strings.
- Validation markers: `Name` uses `MaxLength=63` plus the approved pattern; `Path` uses `MinLength=1` and `MaxLength=1024`.
- Generated artifacts: deep-copy code and the CRD schema are regenerated through existing repository targets. Envtest proves duplicate-key rejection, limits, omission/empty behavior, and round-trip preservation.
- Requirements: `R1`, `NFR5`.

### Restricted Parser

- Purpose: classify one field path as valid, invalid syntax, or recognized-but-unsupported syntax.
- Input/Output: a declared path string becomes an ordered `[]operation`, or an `ExtractionError` carrying source ID, field name, stable reason, and a token offset.
- Grammar:

  ```text
  path        = '{' root operation* '}'
  root        = '.'
  operation   = dot-property | quoted-key | array-index | array-wildcard
  dot-property = identifier ('.' identifier)*
  quoted-key  = '[' '\'' quoted-character* '\'' ']'
  array-index = '[' unsigned-decimal ']'
  array-wildcard = '[*]'
  ```

  The first identifier follows the root dot; subsequent dot properties consume their own dot. A quoted key supports only `\\'` and `\\\\` escapes. Unmatched delimiters, malformed escapes, invalid identifiers, and overflowed indexes return `InvalidExpression`. Whitespace or other text outside the single path plus tokens belonging to the explicitly excluded broader JSONPath language return `UnsupportedExpression`.
- Dependencies: Go string/rune processing only; no new module.
- Requirements: `R2`, `R3`, `NFR2`, `NFR3`.

### Compiler And Immutable Plan

- Purpose: validate all field declarations for a source before object evaluation.
- Input/Output: `CompileSource(v1alpha1.KubeseerSource) (Plan, *ExtractionError)` and `CompileBatch([]v1alpha1.KubeseerSource) []CompileOutcome`.
- Behavior: compile every field, sort compiled fields lexicographically by name, copy operation slices into private plan state, and return defensive copies from accessors.
- Cache policy: no cache in the initial implementation. Every call remains a valid cache-unavailable path; a future cache may store plans by the complete ordered `(name,path)` declaration set.
- Requirements: `R1`, `R3`, `R5.AC3`, `NFR2`, `NFR3`.

### Native Evaluator

- Purpose: evaluate one immutable plan against one selected resource without rendering or coercion.
- Input/Output: `EvaluateResource(ctx, Plan, selection.SelectedResource) (ResourceOutcome, *ExtractionError)`.
- Behavior: preserve explicit null branches, drop missing branches, fail on container-type mismatches, and deep-copy every final native match through an error-returning JSON clone helper.
- Accepted native values: `nil`, `bool`, `string`, `int64`, `float64`, `encoding/json.Number`, `map[string]any`, and `[]any`. Any other nested Go value produces `InvalidResource`; the helper never panics.
- Requirements: `R4`, `R5.AC1` through `R5.AC5`, `R6.AC1` through `R6.AC5`, `NFR1`, `NFR2`, `NFR4`.

### Batch Engine

- Purpose: compose field declarations with selection outcomes while preserving source order, selection failures, atomicity, and cancellation semantics.
- Input/Output: `ExtractBatch(context.Context, []SourceInput) []SourceOutcome`, where each `SourceInput` owns one `v1alpha1.KubeseerSource` and its corresponding `selection.SelectionOutcome`.
- Preconditions: the source declaration ID must equal the selection outcome ID in each input. A mismatch becomes a source-scoped `InvalidInput` extraction error without evaluating that source.
- Behavior: compile every input source first; then walk inputs sequentially. Preserve an unsuccessful `selection.SelectionOutcome.Err` as the exact returned error. For a successful selection, evaluate resources in selection order into temporary storage. On failure, discard temporary values. When cancellation is observed, preserve completed outcomes and mark each unstarted source interrupted.
- Requirements: `R3.AC6`, `R5`, `R6`, `NFR2`, `NFR3`, `NFR4`, `NFR6`.

## Data Models

The internal model keeps cardinality structural rather than adding a public enum:

```go
type MatchSet struct {
    values []any
}

type FieldOutcome struct {
    FieldName string
    Matches   MatchSet
}

type ResourceOutcome struct {
    Provenance selection.Provenance
    Fields     []FieldOutcome
}

type SourceOutcome struct {
    SourceID  string
    Resources []ResourceOutcome
    Err       error
}

type SourceInput struct {
    Source    v1alpha1.KubeseerSource
    Selection selection.SelectionOutcome
}
```

- zero matches means absent;
- one match containing `nil` means explicit null;
- one match containing `[]any` means one native list value;
- more than one match means wildcard-produced multiplicity.

`MatchSet.Values`, `ResourceOutcome` accessors, and plan accessors return defensive copies. The output retains the full `selection.Provenance` value and does not duplicate resource contents.

Compiled plans use private state:

```go
type operationKind uint8

const (
    operationField operationKind = iota
    operationIndex
    operationWildcard
)

type operation struct {
    kind  operationKind
    key   string
    index int
}
```

`ExtractionErrorReason` contains `InvalidExpression`, `UnsupportedExpression`, `EvaluationTypeMismatch`, `InvalidResource`, `InvalidInput`, and `ExtractionInterrupted`. `ExtractionError` carries `SourceID`, optional `FieldName`, optional `Provenance`, stable `Reason`, sanitized `Message`, and an unexported or non-rendered `Cause`.

## Error Handling

- Structural API failures are rejected by the API server through generated CRD validation.
- Parser failures are source-scoped and occur before any selected object is evaluated. Error offsets identify configuration location without embedding observed values.
- Missing properties, missing map keys, out-of-range non-negative indexes, and empty wildcard expansions are not errors; they remove branches and can produce an absent field.
- A property operation on a non-map value or an array operation on a non-array value returns `EvaluationTypeMismatch`.
- Invalid native Go values return `InvalidResource` through explicit type checks; the clone path does not use a panic-based API.
- Selection errors pass through unchanged. Extraction does not reinterpret policy, discovery, selector, or read failures.
- A source error discards every temporary extraction value for that source. No retries occur in this pure processing feature.
- The engine checks `ctx.Err()` before each source, resource, field, and wildcard expansion. Cancellation stops new work and produces stable interrupted outcomes.

## Security Considerations

- Extraction receives only objects already read through the authorized selection capability and performs no Kubernetes I/O.
- `ExtractionError.Error()` contains identifiers, provenance, reason, and sanitized context only; it never formats selected objects or extracted matches.
- Structured logs, if added by a later composition layer, use source and field identifiers but not field paths, object contents, or values by default.
- Defensive copies prevent downstream typed conversion from mutating the selected `unstructured.Unstructured` object or another field result.
- Rejecting unsupported syntax eliminates filters, templates, functions, and scripts as expression-driven execution surfaces.

## Failure Modes And Tradeoffs

- Failure mode: an expression resembles valid Kubernetes JSONPath but uses an excluded construct.
  - Mitigation: return `UnsupportedExpression` before resource evaluation and document the exact accepted grammar.
  - Tradeoff: Kubeseer intentionally supports less syntax than `kubectl` in exchange for stable, auditable semantics.
- Failure mode: one wildcard branch has a missing key or index.
  - Mitigation: discard only that branch and preserve remaining matches in traversal order.
  - Tradeoff: absence is branch-local, while a wrong container type remains a source-failing configuration/data mismatch.
- Failure mode: one field fails after earlier values were extracted.
  - Mitigation: retain results only in source-local temporary storage and discard them on failure.
  - Tradeoff: successful values from that source are unavailable until a degraded-result policy is approved.
- Failure mode: a selected object contains a non-JSON Go value.
  - Mitigation: validate and clone with an error-returning type switch.
  - Tradeoff: the package duplicates a small JSON clone routine instead of calling `runtime.DeepCopyJSONValue`, whose pinned implementation panics for unsupported types.
- Failure mode: repeated compilation adds CPU work.
  - Mitigation: keep compilation linear in path length and plans immutable.
  - Tradeoff: avoid cache invalidation, synchronization, and memory ownership until measurements justify them.
- Failure mode: context cancellation occurs between completed sources.
  - Mitigation: sequential batching gives deterministic completed and interrupted partitions.
  - Tradeoff: source-level parallelism is deferred to the performance feature.

## Testing Strategy

Testing follows the constitution's higher-layer-only policy:

- extend `api/v1alpha1/kubeseer_envtest_test.go` to prove the generated API and CRD accept valid field declarations, preserve omitted and empty lists, round-trip name/path values, and reject duplicates or structural limit violations;
- add cross-module tests under `test/integration/` that construct real `selection.SelectionOutcome` values and pass them through `extraction.ExtractBatch`;
- cover every supported operation in scalar, native-list, object, explicit-null, absent, single-wildcard, and nested-wildcard scenarios;
- cover every excluded syntax family plus malformed delimiters, escapes, identifiers, and overflowing indexes;
- cover branch-local absence, type mismatch, invalid native values, source atomicity, selection-error pass-through, source-order preservation, and cancellation partitions;
- assert defensive-copy behavior by mutating returned maps/lists and checking the selected object plus sibling outcomes remain unchanged;
- do not add package-local unit tests under `internal/extraction`.

## Verification Plan

- Requirement proof: envtest proves `R1`; cross-module integration tables prove `R2` and `R3`; selection-to-extraction scenarios prove `R4` through `R6`.
- Test evidence: named integration tests expose parser-subset, native-cardinality, ordering, source-atomicity, selection-pass-through, and cancellation scenarios so task proofs can use non-vacuous `expect_output` assertions.
- Generated evidence: `make generate`, `make manifests`, and `make verify` establish that Go deep-copy code and CRD schema match the API markers.
- Repository evidence: `make test-integration` is the primary behavior proof; `make test-api` covers API-server validation; `go build ./...` and `go mod tidy -diff` remain final read-only consistency checks.
- Operational evidence: none in this feature because extraction is pure and emits no logs, metrics, events, or status updates.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Public API Field Declaration; envtest schema and round-trip proof |
| `R2` | Restricted Parser; parser-subset integration scenarios |
| `R3` | Restricted Parser; Compiler And Immutable Plan; batch precompilation |
| `R4` | Native Evaluator; MatchSet cardinality model; native-value scenarios |
| `R5` | Native Evaluator; Batch Engine; ordered outcome model |
| `R6` | Native Evaluator; Batch Engine; Error Handling |
| `NFR1` | Native evaluator type switch; error-returning JSON clone; defensive-copy tests |
| `NFR2` | Immutable sorted plans; ordered branch traversal; stable error reasons |
| `NFR3` | Batch precompilation; source-local temporary storage; cancellation checks |
| `NFR4` | Sanitized `ExtractionError`; no-I/O package boundary; confidentiality tests |
| `NFR5` | CRD name/path limits; envtest rejection scenarios |
| `NFR6` | Real `selection` to `extraction` collaboration under `test/integration/` |
