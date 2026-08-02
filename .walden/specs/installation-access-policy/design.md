---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-02T08:26:03Z
last_modified: 2026-08-02T08:26:03Z
approved_fingerprint: sha256:89800f88c9cfa51ad505ef2cd57fff2b7640cbec3fb5d51af7c6db79df594722
source_requirements_approved_at: 2026-08-02T08:25:14Z
source_requirements_fingerprint: sha256:f7a99b5d19399e9f98b7c6631911cbb448abbb53d361cc1be48e1c6dbae3de93
---

# Feature Design

## Overview

The feature introduces one cluster-scoped `KubeseerAccessPolicy` custom resource in
`kubeseer.io/v1alpha1` and a small internal `accesspolicy` package that validates,
compiles, and evaluates that policy. The only active object is named
`installation-access-ceiling`.
The policy remains separate from namespaced `Kubeseer` resources and represents an
installation-wide ceiling: later request-level authorization may narrow an allowed
request but cannot widen this policy.

The API server rejects structurally invalid objects. The compiler repeats the
security-critical validation before constructing an immutable, set-backed snapshot,
so objects created in memory or retained from an older schema also fail closed. A
loader converts missing, invalid, and unavailable policy states into deny-all
snapshots; it never returns a previously valid or permissive fallback. Evaluation is
then pure, deterministic, and free of Kubernetes API or resource-instance reads.

This design follows Kubernetes' structural OpenAPI schema and defaulting model for
CRDs ([Kubernetes CRD documentation](https://github.com/kubernetes/website/blob/main/content/en/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions.md))
and uses set-list semantics plus CEL only where OpenAPI markers cannot express a
cross-field or metadata invariant ([Kubernetes CEL validation guidance](https://github.com/kubernetes/website/blob/main/content/en/blog/_posts/2022/crd-validation-rules-graduate-to-beta.md)).

<!-- assumed: every resource rule present in spec.resources is enabled; no per-rule switch is introduced (source: approved requirements and simplest viable shape) -->

## Architecture

```text
KubeseerAccessPolicy/installation-access-ceiling
            |
            v
   PolicySource adapter  -- Get only the active object
            |
            v
   Validate + compile    -- schema-independent defensive checks
            |
            v
   immutable Snapshot    -- valid policy or one deny-all state
            |
            v
   Evaluate(Request)     -- Decision{Allowed, Reason, Message}
```

The public API and internal evaluator have intentionally different shapes:

- `api/v1alpha1` owns the Kubernetes representation, markers, generated deep-copy
  code, and generated CRD manifest.
- `internal/accesspolicy` owns a narrow loading boundary, defensive compilation,
  immutable lookup sets, reason precedence, and decisions.
- `internal/discovery` remains the source of normalized resource scope and resolved
  API group. The caller combines `SourceDescriptor.Kind`, `Resolution.Resource.Group`,
  `Resolution.Scope`, and the requested namespace into an access-policy request.
- Future authorization enforcement owns watching/reloading the object and applying
  the resulting snapshot before discovery reads. This feature supplies the policy
  primitive but performs no resource read, extraction, or reconciliation.

Loading and evaluation are separate operations. A load always produces a complete
snapshot: either a compiled valid policy or exactly one terminal deny-all state.
Consequently, evaluation never performs an API call and cannot accidentally fall
back to the last valid policy after a load failure.

## Options Considered

### Option A: Typed CRD plus compiled immutable snapshot

- Summary: Define a cluster-scoped, structurally validated CRD and compile its lists
  into private lookup sets before evaluation.
- Why chosen: It gives administrators a Kubernetes-native, inspectable contract;
  makes Helm templating straightforward later; keeps runtime decisions pure; and
  provides defense in depth without adding a webhook or controller.

### Option B: ConfigMap or process flags

- Summary: Store namespace and resource rules in untyped configuration consumed by
  the operator process.
- Why rejected: It loses CRD schema/defaulting, admits more parse-time ambiguity,
  makes field-specific rejection weaker, and couples policy evolution to process
  configuration rather than a stable Kubernetes API.

### Option C: Embed policy overrides in each Kubeseer resource

- Summary: Add namespace and resource access fields to `KubeseerSpec`.
- Why rejected: It violates the approved installation-admin boundary and creates a
  path by which a namespaced user could attempt to broaden installation policy.

## Simplicity And Elegance Review

- Simplest viable shape: one public CRD, one pure internal package, one generated CRD
  artifact, and focused tests. There is no status subresource, webhook, policy
  controller, cache, generic expression language, wildcard matcher, or RBAC writer.
- Coupling check: the evaluator accepts normalized scalar identity plus
  `discovery.Scope`; it does not depend on a Kubernetes client or on resource
  instances. Only the loader adapter knows how the active object is retrieved.
- Deliberate duplication: API-server validation improves administrator feedback;
  compiler validation is the mandatory fail-closed boundary. Both implement the same
  constraints, with tests ensuring they do not drift.
- Future-proofing: Helm can install the CRD and template
  `KubeseerAccessPolicy/installation-access-ceiling` without changing the Go API.
  Watching, atomic
  snapshot replacement, user authorization, and ServiceAccount RBAC remain in their
  already identified later features.

The design was challenged against a direct, uncompiled traversal of the CR object.
That alternative removes one type but would repeat list scans on every decision,
blur validation with authorization, and make deterministic behavior harder to prove.
The immutable snapshot is the smallest extra abstraction that satisfies the security,
performance, and testability requirements together.

## Components And Interfaces

### KubeseerAccessPolicy API

- Purpose: Express the administrator-owned maximum namespace, resource-kind, and
  cluster-scope access granted to Kubeseer.
- Inputs/Outputs: A cluster-scoped `KubeseerAccessPolicy` and list type registered in
  the existing `kubeseer.io/v1alpha1` scheme; generated CRD scope is `Cluster`, and
  the only valid metadata name is `installation-access-ceiling`.
- Dependencies: Kubernetes `apiextensions.k8s.io/v1`, controller-gen, existing API
  registration and generation workflow.
- Requirements: `R1`, `R2`, `R3`, `R4`, `R5`, `C1`, `C2`, `C3`, `C6`

### PolicySource and loader

- Purpose: Read only `KubeseerAccessPolicy/installation-access-ceiling`, classify
  retrieval outcomes, and return a newly constructed snapshot for every load attempt.
- Inputs/Outputs: A narrow `PolicySource` interface returns the active typed object or
  an error. `Load(ctx)` returns a valid compiled snapshot, `PolicyMissing`,
  `PolicyInvalid`, or `PolicyUnavailable`. It never accepts a caller-supplied name and
  never returns a previous snapshot on failure.
- Dependencies: the typed API and a thin controller-runtime client adapter; Kubernetes
  error classification is confined to the adapter/loader boundary.
- Requirements: `R1`, `R5`, `R6`, `NFR1`, `NFR4`, `C2`

### Compiler

- Purpose: Defensively validate a typed policy and create immutable lookup structures.
- Inputs/Outputs: `Compile(*v1alpha1.KubeseerAccessPolicy)` returns either a
  `CompiledPolicy` or a field-addressed `PolicyInvalid` diagnostic. It canonicalizes
  namespace sets and the cross-product of each rule's API groups and kinds.
- Dependencies: Kubernetes namespace validation utilities and standard Go maps; no
  Kubernetes client.
- Requirements: `R2`, `R3`, `R4`, `R5`, `R8`, `NFR1`, `NFR3`, `NFR4`

### Snapshot evaluator

- Purpose: Make the logical installation-policy decision without resource reads or
  ServiceAccount/user-RBAC inference.
- Inputs/Outputs: `Evaluate(Request) Decision`, where `Request` contains source ID,
  normalized API group, Kind, namespace, and `discovery.Scope`; `Decision` contains
  `Allowed`, a stable reason code, and a stable sanitized message.
- Dependencies: immutable compiled state and `internal/discovery.Scope` only.
- Requirements: `R2`, `R3`, `R4`, `R6`, `R7`, `R8`, `NFR1`, `NFR2`, `NFR3`, `NFR4`,
  `NFR5`, `C4`, `C5`

## Data Models

The public representation is equivalent to:

```go
type KubeseerAccessPolicySpec struct {
    Namespaces         NamespacePolicy `json:"namespaces"`
    Resources          []ResourceRule  `json:"resources,omitempty"`
    AllowClusterScoped bool            `json:"allowClusterScoped,omitempty"`
}

type NamespacePolicy struct {
    Mode             NamespaceMode `json:"mode"`
    Include          []string      `json:"include,omitempty"`
    Exclude          []string      `json:"exclude,omitempty"`
    SystemNamespaces []string      `json:"systemNamespaces"`
}

type ResourceRule struct {
    APIGroups []string `json:"apiGroups"`
    Kinds     []string `json:"kinds"`
}
```

`NamespaceMode` permits exactly `Explicit`, `All`, and `AllNonSystem`.
`systemNamespaces` defaults, when omitted, to `kube-system`, `kube-public`, and
`kube-node-lease`; an explicitly present empty list remains empty. Its Go JSON tag
deliberately omits `omitempty`: a nil slice serializes as `null` and receives the API
default, while a non-nil empty slice serializes as `[]` and remains the administrator's
exact empty override. The compiler makes the same nil-versus-empty distinction for
objects constructed directly in Go. `allowClusterScoped` defaults to `false`.

Namespace lists and each rule's API-group and Kind lists use Kubernetes set-list
semantics so duplicates are rejected by schema validation. Include/exclude overlap is
valid because exclusion deliberately has highest precedence. Resource rules remain an
atomic list; duplicate cross-product entries have no semantic effect because the
compiler stores exact `(apiGroup, kind)` keys in a set.

OpenAPI constraints require non-empty `apiGroups` and `kinds` in every rule, permit
the empty string only as the core API group, reject wildcard syntax, validate
namespace names, and constrain Kind and non-core API-group syntax. A root CEL rule
constrains `metadata.name` to `installation-access-ceiling`. The compiler repeats every security-relevant
constraint and produces field paths such as `spec.namespaces.mode` or
`spec.resources[1].kinds[0]`.

The private model is not serialized:

```go
type Request struct {
    SourceID  string
    APIGroup  string
    Kind      string
    Namespace string
    Scope     discovery.Scope
}

type Decision struct {
    Allowed bool
    Reason  Reason
    Message string
}
```

A compiled policy holds namespace sets, the exact resource-key set, the namespace
mode, and the cluster-scope flag. A snapshot holds either that compiled policy or one
terminal state: missing, invalid, or unavailable. Its fields remain private and are
constructed defensively, preventing callers from mutating policy maps after load.

## Decision Algorithm

The evaluator applies one stable precedence and stops at the first applicable result:

1. Return the snapshot state: `PolicyMissing`, `PolicyInvalid`, or
   `PolicyUnavailable`.
2. Reject malformed or non-normalized input as `InvalidRequest`.
3. Reject a missing exact `(apiGroup, kind)` key as `ResourceDenied`.
4. For cluster-scoped resources, reject a disabled cluster flag as
   `ClusterScopeDenied`; otherwise return `Allowed` without consulting namespace
   fields.
5. For namespaced resources, compute namespace access and return `NamespaceDenied`
   or `Allowed`.

Namespace access first checks `exclude`. It then evaluates the configured mode:

- `Explicit`: allow only membership in `include`.
- `All`: allow every namespace not excluded.
- `AllNonSystem`: allow membership in `include`, otherwise allow only namespaces not
  in `systemNamespaces`.

This ordering gives exclusions highest precedence, lets explicit inclusion add a
system namespace, ignores namespace fields for cluster-scoped requests, and yields a
single reason even when several constraints would deny the same request.

## Error Handling

- A not-found result for `installation-access-ceiling` becomes a deny-all snapshot with
  `PolicyMissing`.
- An object rejected by defensive compilation becomes a deny-all snapshot with
  `PolicyInvalid` and one deterministic field-specific message. Errors are ordered by
  field path and rule/list index before selecting the primary diagnostic.
- Any other read or decoding failure becomes a deny-all snapshot with
  `PolicyUnavailable`; no retry or stale-success fallback occurs inside this package.
- An unknown scope, missing Kind, malformed group, or missing namespace for a
  namespaced request becomes `InvalidRequest`.
- Logical decisions never translate Kubernetes RBAC errors. A later caller that is
  logically allowed but receives an API `Forbidden` retains that distinct operational
  result.

## Security Considerations

- The policy object's scope and singleton name prevent namespaced Kubeseer resources
  from supplying local overrides. Kubernetes RBAC remains responsible for deciding
  which administrators may write the cluster-scoped object.
- API validation is not the trust boundary: compiler validation ensures malformed
  objects cannot become permissive even if admission was bypassed or the object came
  from an older stored version.
- Missing, malformed, and unreadable policy states all deny access, and load failure
  cannot reuse a formerly permissive snapshot.
- Decisions depend only on policy fields and normalized request metadata. The
  evaluator neither reads instances nor treats ServiceAccount or user RBAC as an
  authorization grant.
- Messages may include stable identifiers already present in the request (source ID,
  namespace, API group, Kind) but never resource data, Secret contents, field values,
  credentials, or raw upstream error bodies.
- The CRD and generated source remain under Apache-2.0 with Alessandro Rontani as the
  default copyright holder, following the constitution.

## Failure Modes And Tradeoffs

- Failure mode: the active policy is absent, malformed, or temporarily unreadable.
  Mitigation: construct the corresponding deny-all snapshot and discard any previous
  success. Tradeoff: availability is sacrificed to preserve the installation boundary.
- Failure mode: admission and compiler validation drift. Mitigation: table-driven
  cases exercise the same accepted/rejected corpus through both layers. Tradeoff:
  validation logic is intentionally represented twice at different trust boundaries.
- Failure mode: direct Go construction omits API-server defaults. Mitigation: the
  compiler applies only the documented restrictive defaults while preserving explicit
  empty lists. Tradeoff: default behavior must have dedicated nil-versus-empty tests.
- Failure mode: overlapping or repeated rules produce ambiguous output. Mitigation:
  exclusion precedence and canonical exact-match sets make the result independent of
  ordering. Tradeoff: repeated resource rules are accepted but semantically deduplicated.
- Failure mode: ServiceAccount RBAC is broader than policy. Mitigation: the evaluator
  ignores RBAC and still denies outside-policy requests. Tradeoff: a logical allow may
  subsequently fail with Kubernetes `Forbidden`, which is intentionally reported by a
  later enforcement layer.
- Failure mode: a policy changes while a consumer uses an older snapshot. Mitigation:
  future authorization enforcement must atomically replace snapshots and invalidate
  results before reads. Tradeoff: watch/reload lifecycle is explicitly deferred and
  is not falsely simulated in this API-foundation feature.
- Failure mode: an object not named `installation-access-ceiling` is submitted.
  Mitigation: CEL rejects it at the API server and compilation rejects it defensively.
  Tradeoff: multiple named policies are intentionally unsupported.

## Testing Strategy

- API unit tests register both new kinds, verify deep-copy isolation, and assert JSON
  round trips preserve omitted versus explicit-empty `systemNamespaces`.
- Compiler table tests cover every namespace mode, default and overridden system
  namespace sets, exclusion precedence, exact core/non-core group-kind cross-products,
  cluster-scope defaults, duplicate and malformed values, empty deny-all scopes, and
  stable field-specific diagnostics.
- Evaluator table tests permute list/rule order and combined denial conditions to prove
  byte-for-byte stable decisions and precedence without any client or resource read.
- Loader tests use a narrow fake source to prove missing, invalid, and unavailable
  outcomes replace prior success with deny-all snapshots and never invoke evaluation-
  time I/O.
- Generated-manifest tests parse the CRD and assert group/version, cluster scope,
  singleton-name CEL, enums, defaults, set lists, required rule members, and wildcard
  rejection.
- `envtest` installs the generated CRD into a real API server, creates valid policies,
  observes persisted defaults, and proves invalid name/mode/list/rule shapes are
  rejected. `envtest` is appropriate because it installs CRDs and starts etcd plus an
  API server without requiring a controller manager
  ([controller-runtime envtest](https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/envtest/server.go)).
- Existing generation and compatibility checks run against the project's supported
  Kubernetes API-server matrix so the new CRD does not regress the v1.35 floor.

## Verification Plan

- Requirement proof: use table-driven compiler/evaluator tests mapped to each
  namespace, resource, scope, failure-state, RBAC-separation, and determinism
  acceptance criterion.
- Test evidence: run focused `go test` packages for `api/v1alpha1` and
  `internal/accesspolicy`, the CRD manifest assertions, and focused `envtest` cases;
  run the existing generation verification to prove checked-in generated artifacts
  match source markers.
- Operational evidence: no new metrics or status are required at this layer. Stable
  reason codes and sanitized messages are the observable contract consumed by later
  enforcement and diagnostics.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Cluster-scoped singleton API, loader, and separation from `KubeseerSpec` |
| `R2` | Namespace policy model, compiler sets, and namespace decision algorithm |
| `R3` | Resource-rule schema and exact `(apiGroup, kind)` compiled set |
| `R4` | Cluster flag default and scope-specific evaluator branches |
| `R5` | OpenAPI/CEL validation, defensive compiler, and field diagnostics |
| `R6` | Terminal deny-all snapshot states and sanitized failure handling |
| `R7` | Pure logical evaluator and explicit separation from Kubernetes RBAC |
| `R8` | Immutable snapshot, fixed precedence, exact sets, and no evaluation I/O |
| `NFR1` | Fail-closed loader/compiler/evaluator construction and tests |
| `NFR2` | Canonical sets, ordered diagnostics, fixed reasons, and permutation tests |
| `NFR3` | Precompiled in-memory lookup structures and allocation-light evaluation |
| `NFR4` | Narrow source interface and pure compiler/evaluator unit boundaries |
| `NFR5` | Sanitized decision/message contract and confidentiality assertions |
