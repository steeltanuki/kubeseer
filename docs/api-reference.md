# API reference

Kubeseer exposes two `kubeseer.io/v1alpha1` resources:

- `Kubeseer`, a namespaced declaration whose evaluated view is published in
  its status;
- `KubeseerAccessPolicy`, a cluster-scoped singleton that places an
  installation-wide ceiling on observation.

The generated CRDs in [`config/crd/bases`](../config/crd/bases/) are the
canonical structural schema. This guide explains their runtime semantics.

## Kubeseer

```yaml
apiVersion: kubeseer.io/v1alpha1
kind: Kubeseer
metadata:
  name: workload-view
  namespace: applications
spec:
  sources: []
```

`spec.sources` is a map-list keyed by `id` with at most 32 entries. Sources
are evaluated independently and appear in deterministic ID order in the
public result.

### Source fields

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | yes | Unique lowercase DNS-style source ID, at most 63 characters |
| `resource.apiVersion` | yes | Exact API version, for example `apps/v1` |
| `resource.kind` | yes | Exact Kind, for example `Deployment` |
| `namespaces.names` | no | Set of up to 64 namespaces for a namespaced type |
| `selector` | no | Name, label, expression, and field constraints |
| `fields` | no | Up to 64 named extraction declarations |
| `aggregations` | no | Up to 32 source-wide aggregation declarations |

Namespace rules are significant:

- omitted `namespaces`: use the containing `Kubeseer` namespace for a
  namespaced resource;
- `namespaces: {names: []}`: intentionally select no namespace and succeed
  without an observed-resource read;
- one or more names: select exactly those authorized namespaces;
- cluster-scoped resource: omit `namespaces` entirely.

### Selectors

```yaml
selector:
  name: web
  matchLabels:
    app.kubernetes.io/component: frontend
  matchExpressions:
    - key: environment
      operator: In
      values: [production, staging]
  fieldSelector: metadata.namespace=applications
```

All populated mechanisms are combined with logical AND. `matchLabels` and
`matchExpressions` use Kubernetes label-selector semantics. `fieldSelector`
is sent to the API server. A valid empty selector selects every object in the
authorized scope; zero matches is successful.

Selection is paginated and deterministic. Objects are ordered by namespace,
name, then UID. If one page or namespace of a source fails, the source does not
publish partial selection data.

## Field extraction

```yaml
fields:
  - name: replicas
    path: "{.spec.replicas}"
    type: integer
```

Field names are unique within a source. `path` is 1–1024 characters. Although
`type` remains optional at the structural API level for compatibility, runtime
typed conversion reports a field-scoped error when it is omitted; new
resources should always declare it.

### Supported JSONPath subset

One expression is allowed and must start with `{.` and end with `}`. Supported
tokens are:

| Syntax | Meaning | Example |
| --- | --- | --- |
| `.property` | Identifier-style object key | `{.metadata.name}` |
| `['property-key']` | Exact quoted object key | `{.metadata.labels['app.kubernetes.io/name']}` |
| `[index]` | Non-negative array index | `{.spec.containers[0].image}` |
| `[*]` | Array wildcard | `{.status.conditions[*].type}` |

Chained tokens apply to each wildcard branch. Results preserve depth-first,
left-to-right array order. A missing key or out-of-range index drops that
branch; no remaining branches means `absent`. A terminal JSON `null` is an
explicit null. Traversing a scalar as an object or list is an evaluation
failure.

Filters, unions, slices, negative indexes, recursive descent, map wildcards,
template directives, functions, scripts, and text outside the single path are
rejected. This is intentionally not the complete kubectl JSONPath language.

### Value types

| Type | Accepted input and output semantics |
| --- | --- |
| `string` | Native JSON string |
| `integer` | Integral JSON number in `int64` range or canonical base-10 string |
| `number` | Finite JSON number or JSON-number string, preserving exact decimal text |
| `boolean` | Native boolean or exact string `true`/`false` |
| `timestamp` | RFC 3339 string with explicit offset, normalized to UTC |
| `duration` | Go duration string, published canonically with nanoseconds |
| `quantity` | Kubernetes quantity string, published canonically with base units |
| `object` | Native JSON object |
| `list` | Native JSON array |

Conversions never truncate, round, apply locale parsing, or implicitly turn a
scalar into a string. Non-finite numbers are rejected.

### Native duration and quantity spellings

`quantity` accepts the grammar of the repository-pinned Kubernetes quantity
parser. The `n` and `u` decimal suffixes are exact when they fit the existing
representation limit; canonical text comes from the native quantity value and
`baseUnits` is the independently checked normalized decimal.

| Quoted input | Public type | Canonical payload | Normalized value |
| --- | --- | --- | --- |
| `100n` | `quantity` | `quantityValue.canonical: "100n"` | `quantityValue.baseUnits: "0.0000001"` |
| `100u` | `quantity` | `quantityValue.canonical: "100u"` | `quantityValue.baseUnits: "0.0001"` |
| `-100u` | `quantity` | `quantityValue.canonical: "-100u"` | `quantityValue.baseUnits: "-0.0001"` |

`duration` follows Go's native duration grammar. Unitless `"0"`, `"+0"`, and
`"-0"` all serialize as `canonical: "0s"` with `nanoseconds: 0`. The ASCII
`"1us"`, U+00B5 MICRO SIGN `"1µs"`, and U+03BC GREEK SMALL LETTER MU
`"1μs"` spellings all represent 1,000 nanoseconds; canonical duration text
uses Go's `µs` form.

```yaml
quantityValue:
  canonical: "100n"
  baseUnits: "0.0000001"
durationValue:
  canonical: "1µs"
  nanoseconds: 1000
```

The parser's acceptance does not permit loss of information: quantity
`0.0000000001` and duration `0.1ns` are rejected rather than rounded. See the
[exact conversion model](concepts-and-architecture.md#exact-native-scalar-conversion)
for range and diagnostic rules.

## Operators

Operators execute in declaration order. Predicates decide whether the current
resource contributes downstream; predicates on the same resource are joined
with logical AND. `default` and `coalesce` are field-local transforms.

```yaml
fields:
  - name: replicas
    path: "{.spec.replicas}"
    type: integer
    operators:
      - operator: gte
        value:
          state: value
          integerValue: 2
```

| Operators | Compatible types | Operand form |
| --- | --- | --- |
| `eq`, `ne` | string, integer, number, boolean, timestamp, duration, quantity | one `value` |
| `gt`, `gte`, `lt`, `lte` | integer, number, timestamp, duration, quantity | one `value` |
| `contains`, `startsWith`, `endsWith`, `matches` | string | one `value` |
| `in`, `notIn` | string, integer, number, boolean, timestamp, duration, quantity | `values` list |
| `exists`, `notExists` | every field type | no operand |
| `default` | every field type | one `value` |
| `coalesce` | every field type | no operand |

An operand is a structural union. Set `state: value` and exactly one branch
matching the field type:

```yaml
value:
  state: value
  stringValue: production
```

The branch names are `stringValue`, `integerValue`, `numberValue`,
`booleanValue`, `timestampValue`, `durationValue`, `quantityValue`,
`objectValue`, and `listValue`. Number operands use exact decimal strings;
object and list operands use JSON text. A `null` operand is structurally
representable but invalid for operator planning.

`matches` uses the controller's bounded regular-expression implementation; it
does not enable script evaluation.

## Aggregations

Aggregations consume values that survived extraction, typing, and operators in
the containing source:

```yaml
aggregations:
  - name: replicas-by-team
    function: sum
    field: replicas
    groupBy: [team]
    includeProvenance: true
```

| Field | Meaning |
| --- | --- |
| `name` | Unique aggregate name in the source |
| `function` | `collect`, `count`, `sum`, `min`, `max`, `average`, `first`, `last`, or `distinct` |
| `field` | Name of the extracted field to reduce |
| `groupBy` | Optional ordered list of up to 16 extracted fields |
| `includeProvenance` | Include contributing resource identities when true |
| `precision` | `average` decimal precision from 0 through 18; default 6 |
| `roundingMode` | `halfEven` (default), `halfAwayFromZero`, `towardZero`, or `awayFromZero` |

The planner validates that a function is compatible with its field type.
Groups use typed canonical keys, so values that merely look alike as text do
not collapse incorrectly. Empty, absent, null, and failed contributions follow
function-specific semantics and remain distinguishable in the public result.

Output is deterministic by aggregate name, group key, value, and provenance.
Limit exhaustion produces a visible failure rather than truncated output.

## Status

`status.observedGeneration` identifies the spec generation represented by the
current status. Consumers should not treat older status as current.

### Conditions

Conditions are a map-list keyed by type:

| Condition | Meaning |
| --- | --- |
| `Accepted` | The declaration is structurally and semantically valid |
| `Authorized` | Requested targets fit fresh logical policy and runtime authority |
| `SourcesResolved` | Discovery and target resolution completed |
| `Ready` | The current generation produced its intended result |
| `Degraded` | Evaluation is not fully successful; a partial result may exist, except for terminal failures such as a result-size limit |

Common public reasons are:

- configuration: `ConfigurationAccepted`, `InvalidConfiguration`;
- authorization: `AuthorizationSucceeded`, `AuthorizationDenied`,
  `AuthorizationNotEvaluated`, `PolicyMissing`, `PolicyInvalid`,
  `ReadForbidden`, `AuthorizationUnavailable`;
- resolution: `ResolutionSucceeded`, `ResolutionFailed`,
  `ResolutionNotEvaluated`, `ResolutionUnavailable`;
- evaluation: `EvaluationSucceeded`, `EvaluationDegraded`,
  `EvaluationUnavailable`, `ResultLimitExceeded`.

Messages are sanitized and bounded. Automation should branch on condition
type, status, reason, and `observedGeneration`, not parse human messages.

### Configuration-budget rejection

The manager applies its effective admission budget again at reconciliation
time. If a declaration no longer fits (for example after a profile change), it
publishes a terminal, current-generation status with these conditions:

| Condition | Status | Reason |
| --- | --- | --- |
| `Accepted` | `False` | `ConfigurationBudgetExceeded` |
| `Authorized` | `Unknown` | `AuthorizationNotEvaluated` |
| `SourcesResolved` | `Unknown` | `ResolutionNotEvaluated` |
| `Ready` | `False` | `ConfigurationBudgetExceeded` |
| `Degraded` | `True` | `EvaluationUnavailable` |

This status is deliberately status-only. `status.result`, `status.summary`,
and `status.resultHash` are absent rather than empty, and the manager does not
read observed resources to remove the old values. A synthetic status-only
projection looks like this:

```yaml
apiVersion: kubeseer.io/v1alpha1
kind: Kubeseer
metadata:
  generation: 8
status:
  observedGeneration: 8
  conditions:
  - type: Accepted
    status: "False"
    reason: ConfigurationBudgetExceeded
  - type: Authorized
    status: Unknown
    reason: AuthorizationNotEvaluated
  - type: SourcesResolved
    status: Unknown
    reason: ResolutionNotEvaluated
  - type: Ready
    status: "False"
    reason: ConfigurationBudgetExceeded
  - type: Degraded
    status: "True"
    reason: EvaluationUnavailable
```

See [operations diagnosis](operations.md#diagnose-configuration-budget-rejection)
for generation and field-presence checks, and [the security boundary](security.md#budget-rejection-and-data-removal)
for the successful-write rule.

### Summary and result

`status.summary` contains `successfulSources`, `failedSources`, and
`matchedResources`. `status.resultHash` is a `sha256:` digest of the semantic
projection and changes only when the meaningful result changes.

`status.result.sources` contains per-source outcomes. A source has either
`values` or `error` state. Resource matches carry exact provenance
(`apiVersion`, `kind`, namespace when applicable, name, and UID) and named
fields. A field is `absent`, `values`, or `error`; each match is `value` or
explicit `null`. Aggregates are `values`, `degraded`, or `error`, and their
value component is `absent` or `values`.

Use the installed CRD or `kubectl explain` for the complete nested structural
shape:

```sh
kubectl explain kubeseer.spec.sources --recursive
kubectl explain kubeseer.status --recursive
```

## KubeseerAccessPolicy

Exactly one cluster-scoped policy is active and its name must be
`installation-access-ceiling`:

```yaml
apiVersion: kubeseer.io/v1alpha1
kind: KubeseerAccessPolicy
metadata:
  name: installation-access-ceiling
spec:
  namespaces:
    mode: Explicit
    include: [applications, platform]
    exclude: [platform-private]
  resources:
    - apiGroups: [apps]
      kinds: [Deployment, StatefulSet]
    - apiGroups: [""]
      kinds: [Pod]
  allowClusterScoped: false
```

Namespace modes are:

- `Explicit`: only names in `include` are eligible;
- `All`: all namespaces except `exclude`;
- `AllNonSystem`: all namespaces except the effective system set and
  `exclude`.

`exclude` wins over `include`. If `systemNamespaces` is omitted, it defaults
to `kube-system`, `kube-public`, and `kube-node-lease`; an explicitly supplied
list replaces that set exactly. Resource rules match exact API groups and
Kinds. The core API group is the empty string. Cluster-scoped targets also
require `allowClusterScoped: true`.

This policy is only a logical ceiling. The manager ServiceAccount must
separately receive matching observed-resource RBAC. See
[Configuration](configuration.md) and [Security model](security.md).
