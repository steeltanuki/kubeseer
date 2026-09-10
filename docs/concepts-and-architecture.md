# Concepts and architecture

This guide explains how Kubeseer turns a declarative resource into a public,
typed status view. Start with the [README](../README.md) for an overview and
the [API reference](api-reference.md) for exact fields and enumerations.

## Mental model

A `Kubeseer` is a namespaced query with one or more independent sources. Each
source identifies one Kubernetes resource type, narrows the objects of that
type, extracts named values, optionally transforms or filters them, and may
reduce them into aggregates.

The output is a materialized view, not a proxy for arbitrary API requests. It
is recomputed when relevant inputs change and stored in the `Kubeseer` status.
The view is deliberately:

- declarative: no embedded program or executable JSONPath expression;
- typed: conversions are explicit and never silently truncate or stringify;
- deterministic: equivalent inputs produce equivalent ordered output and hash;
- bounded: admission budgets and runtime limits cap work and status size;
- fail-closed: no observed-resource read occurs without fresh authorization;
- partially resilient: an isolated source or aggregate failure can coexist
  with successful sibling results.

## Evaluation pipeline

```text
Kubeseer spec
  -> discovery and scope resolution
  -> fresh installation-policy authorization
  -> exact read/watch capability
  -> resource selection
  -> restricted JSONPath extraction
  -> typed conversion
  -> ordered predicates and transforms
  -> grouping and aggregation
  -> deterministic status projection
```

### 1. Discovery and scope resolution

Kubeseer resolves the requested `apiVersion` and `kind` through Kubernetes
discovery. The result establishes whether the type is namespaced or
cluster-scoped and supplies the exact resource identity used downstream.

For namespaced resources, omitting `namespaces` means the namespace containing
the `Kubeseer`. An explicit empty namespace list intentionally selects no
namespace and succeeds with zero matches. Cluster-scoped resources must omit
`namespaces`.

### 2. Authorization

The controller reads a fresh `KubeseerAccessPolicy` named
`installation-access-ceiling`. The requested resource type and every resolved
namespace must fit that policy. A successful decision creates an internal
capability bound to the exact subject, policy identity, generation, target,
and operation.

That capability is required at the actual `LIST` and watch boundaries.
Kubernetes RBAC is evaluated separately by the API server. Policy absence,
invalidity, drift, revocation, RBAC denial, or stale work prevents the read or
publication. See [Security model](security.md).

### 3. Selection

Selection can combine exact name, label map, label expressions, and field
selector. Populated mechanisms are joined with logical AND. An otherwise empty
valid selector means all objects in the authorized scope; zero matching
objects is a successful result.

Lists are paginated and a source spanning multiple namespaces is atomic at the
selection boundary: if any target or page fails, its partial matches are
discarded. Successful sibling sources remain available. Selected objects are
ordered by namespace, name, then UID before evaluation.

### 4. Extraction and typing

Each field expression is compiled from Kubeseer's restricted JSONPath subset.
It traverses native Kubernetes JSON values, so objects, lists, booleans, and
numbers are not flattened into strings. A missing terminal produces an absent
field; explicit JSON `null` remains distinct from absence; an invalid
container traversal is a field evaluation failure.

Every extracted value is converted to its declared type. Timestamp, duration,
and quantity values use Kubernetes-aware canonical representations. Failed
conversions are explicit and do not silently coerce data.

### Exact native scalar conversion

Quantity fields use the Kubernetes parser pinned by `go.mod`, while duration
fields use Go's `time.ParseDuration` grammar. Native parser success supplies
canonical text but is not the complete acceptance rule: Kubeseer independently
accumulates an exact rational magnitude, rejects values outside the existing
representation ceiling (or signed `int64` nanosecond range), and rejects any
value that the native parser would round. No alternate Unicode normalization or
implicit unit conversion is applied.

The documented compatibility cases are:

- quantity `100n` -> `baseUnits: "0.0000001"`;
- quantity `100u` -> `baseUnits: "0.0001"`;
- quantity `-100u` -> `baseUnits: "-0.0001"`;
- duration strings `"0"`, `"+0"`, and `"-0"` -> `canonical: "0s"`,
  `nanoseconds: 0`;
- ASCII `"1us"`, U+00B5 MICRO SIGN `"1µs"`, and U+03BC GREEK SMALL LETTER MU
  `"1μs"` -> `canonical: "1µs"`, `nanoseconds: 1000`.

Existing decimal (`1.5`), exponent (`1e3`), binary quantity (`1Gi`), and
compound duration (`1h2m3.004s`) forms retain their exact semantics. A
quantity such as `0.0000000001` or a duration such as `0.1ns` is syntactically
close to a valid value but is rejected with a no-rounding error. Conversion
errors are field-scoped and public diagnostics contain the reason and safe
message only, never the extracted value, JSONPath, or resource body. See the
[API examples](api-reference.md#native-duration-and-quantity-spellings) for
the serialized payload shape.

### 5. Operators

Operators run in declaration order. Predicates decide whether the current
resource contributes to later stages; all predicates for one resource are
implicitly ANDed. Transforms such as `default` and `coalesce` affect only their
declared field. Operator arity and type compatibility are checked before
evaluation.

### 6. Aggregation

Aggregations run per source after operators. They can reduce one field across
all accepted resources or partition contributions by typed `groupBy` values.
Grouping, contributors, values, and aggregate names have stable ordering.
Provenance is included only when requested.

A resource-local failure may degrade one aggregate without erasing valid
aggregate siblings. Cardinality and output limits fail explicitly; Kubeseer
does not truncate a result and present it as complete.

### 7. Status publication

The controller projects internal outcomes into a sanitized public status with
conditions, summary counts, source results, aggregates, provenance, and a
semantic SHA-256 result hash. A status write occurs only when the semantic
projection changes. Volatile reconciliation timing does not cause status
churn.

## Reconciliation model

The controller-runtime cache supplies event notifications, but it is not the
authority for policy, authorization, or publication-sensitive reads. The
controller uses direct API reads at those boundaries.

Reconciliation can be triggered by:

- changes to a `Kubeseer`;
- changes to the singleton installation policy;
- metadata changes on exactly routed observed resource types;
- a bounded periodic safety interval.

Observed-resource watches are conservative and selector-independent. They
signal that a source may need reevaluation; the authorized `LIST` performs the
actual selection. Policy or generation changes cancel stale work, invalidate
routes, and require new authorization before more observation.

## Watch lifecycle and observation gaps

An evaluation context and a source-watch supervisor have different lifetimes.
The evaluation context covers one reconciliation and its `EvaluationTimeout`;
the supervisor owns the transport for one exact `WatchAddress` (resource,
scope, and namespace) and can be shared by several current owners. A caller
deadline or ordinary completion therefore ends only that evaluation. It does
not cancel an established shared stream that still has a current owner.

`RouteRegistry` waits for readiness only for the targets requested by the
current reconciliation. It obtains a fresh exact permit before every serial
WATCH attempt, bounds establishment with the resolved `EvaluationTimeout`,
and never starts a second attempt while the previous transport call is still
blocked. A successful WATCH is ready before the initial `LIST` begins.

Startup failure, a reconnect after a stream gap, or a capacity promotion marks
the target for recovery. The supervisor then sends current authorized owners
through the coalescing ingress so a fresh `LIST` covers the interval in which
no event could have been observed. The periodic safety interval remains a
second, bounded backstop. If UID, generation, deletion, or policy epoch makes
an owner stale, it is removed before recovery is queued; a fresh reconciliation
must establish authority again.

The supervisor cancels its transport when its last current owner disappears or
when the manager shuts down. Late responses are checked against supervisor
identity and cancellation and are stopped without routing an event. The
transport must honor its context for prompt shutdown; a context-ignoring
implementation cannot be forcibly terminated by the registry.

See [Operations](operations.md#diagnose-stalled-watch-startup-and-recovery) for
safe startup/retry diagnosis and the [Security model](security.md#watch-authority-and-transport-lifetime)
for the authority boundary.

The resource has no controller finalizer. Deleting a `Kubeseer` removes its
routes through normal reconciliation and garbage-free in-memory cleanup.

## Failure isolation

Failures are isolated at the narrowest boundary that can still be truthful:

| Boundary | Behaviour |
| --- | --- |
| Admission | Rejects invalid or currently unauthorized declarations before storage |
| Discovery or authorization | Fails the affected source without attempting its read |
| Multi-target selection | Discards partial matches for that source |
| Field/operator evaluation | Records a sanitized local error; valid siblings may survive |
| Aggregation | Degrades or fails the affected aggregate; valid aggregates may survive |
| Global result limit | Rejects publication as complete; never silently truncates |
| Stale reconciliation | Cancels or discards the result before publication |

`Ready`, `Degraded`, and the stage-specific conditions make the distinction
visible. The [operations guide](operations.md) explains how to diagnose them.

## Repository architecture

The code follows explicit package boundaries:

| Area | Responsibility |
| --- | --- |
| `api/v1alpha1` | Public CRD types and schema markers |
| `internal/discovery` | Exact API resource resolution and cache |
| `internal/accesspolicy` | Installation policy interpretation |
| `internal/authorization` | Fresh decisions, capabilities, and revocation |
| `internal/selection` | Authorized paginated resource selection |
| `internal/extraction` | Restricted JSONPath planning and native evaluation |
| `internal/typedoutput` | Conversion and canonical typed representation |
| `internal/operators` | Typed predicates and transforms |
| `internal/aggregation` | Grouping, reducers, limits, and provenance |
| `internal/status` | Conditions, public projection, hashing, and write suppression |
| `internal/reconciliation` | Pipeline composition, cancellation, and routes |
| `internal/admission` | Structural and discovery-dependent validation |
| `internal/observability` | Sanitized metrics, logs, Events, and tracing hooks |
| `internal/limits` | Runtime limit configuration and validation |
| `internal/managerapp` | Controller-runtime manager and dependency wiring |
| `internal/purge` | Narrow destructive lifecycle operations for the separate client |
| `cmd/kubeseer` | Manager process entry point |

The architecture keeps public schema, pure evaluation logic, network adapters,
and runtime composition separate. Unit tests cover pure contracts; envtest
covers real API machinery; the kind-on-Podman E2E suite covers the packaged
system. See [Development and verification](development.md).

## Related documentation

- [API reference](api-reference.md)
- [Security model](security.md)
- [Operations](operations.md)
- [Examples](examples.md)
- [Feature specifications](../SPECIFICATIONS.md)
