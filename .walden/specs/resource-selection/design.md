---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-26T09:25:54Z
last_modified: 2026-08-26T09:25:54Z
approved_fingerprint: sha256:3057c75d4ae90cb3b446ca465c806c5cb611f8979e53c2537edb280538ceea6b
source_requirements_approved_at: 2026-08-26T09:11:16Z
source_requirements_fingerprint: sha256:ba075fe46141107f51ecf7ba48f1a49de2c0bbe7dec593e57326701bb5afdf33
---

# Feature Design

## Overview

Implement `resource-selection` as one internal Go package with three explicit phases: build an immutable selection plan from the public source plus discovery metadata, bind exact installation-policy decisions to every read target, then execute only a fully authorized plan through a narrow Kubernetes LIST boundary. This shape makes the authorization-before-read invariant structural without moving policy loading or evaluation into selection.

Extend `api/v1alpha1.KubeseerSource` with typed resource coordinates, an optional namespace block whose pointer preserves omitted versus explicit-empty state, and an optional selector block. Normalize label selectors with `metav1.LabelSelectorAsSelector`, field selectors with `fields.ParseSelector`, and exact names as the `metadata.name` field-selector term. The executor uses one LIST path for exact-name and broad selections, which keeps combination semantics uniform and makes zero matches naturally successful.

The production adapter uses `dynamic.Interface.Resource(gvr)`, applies `.Namespace(name)` only for discovery-reported namespaced resources, and sends canonical selectors through `metav1.ListOptions`. Continued pages reuse identical query parameters except for the opaque `Continue` token. A `410 ResourceExpired` discards the current target's partial pages and restarts that target once from an empty token to retain a consistent list snapshot. [client-go dynamic API](https://pkg.go.dev/k8s.io/client-go/dynamic), [Kubernetes API pagination](https://kubernetes.io/docs/reference/using-api/api-concepts/#retrieving-large-results-sets-in-chunks)

<!-- assumed: selection uses LIST for every selector shape, including selector.name, because a single server-side path preserves AND semantics and zero-match behavior without local field evaluation (source: approved R3 and client-go ResourceInterface) -->
<!-- assumed: the internal list page limit defaults to 500 and is constructor-configurable; product-wide cardinality and concurrency limits remain deferred to performance-and-limits (source: approved R7 and Kubernetes API pagination example) -->
<!-- assumed: one consistent-list restart is allowed after ResourceExpired; a second expiration fails the source so cancellation and deadline bounds remain meaningful (source: approved R6, R7, and Kubernetes continue-token semantics) -->
<!-- assumed: batch execution is sequential in this feature; concurrency policy belongs to performance-and-limits and is unnecessary for source isolation (source: approved out-of-scope boundary) -->

## Architecture

```text
KubeseerSource + owner namespace
              |
              v
      Selection Planner -----> discovery.Resolver
              |                 (GVR + exact scope)
              v
     immutable SelectionPlan
      | one ReadTarget per scope
      | canonical label/field selectors
      v
caller evaluates accesspolicy.Request for every target
              |
              v
   Authorization Binder
      | validates complete exact target coverage
      | produces opaque AuthorizedPlan or no plan
              v
   Selection Executor -----> ResourceLister adapter -----> dynamic.Interface
      | LIST pages only             | GVR / namespace
      | source-atomic               | ListOptions selectors
      v
ordered SelectionOutcome
  selected unstructured objects + provenance, or one typed source error
```

Planning performs no resource-instance I/O. Binding checks the complete authorization set before constructing an `AuthorizedPlan`, so a missing, denied, duplicated, or mismatched decision prevents every read for that source. The executor accepts only `AuthorizedPlan`; its unexported fields cannot be assembled directly by other packages. This is an in-process capability boundary, not a cryptographic trust mechanism: the runtime composition root remains responsible for obtaining decisions from the real installation-policy snapshot.

Each target is listed to completion before the next target. Items are collected in temporary source-local storage, deduplicated by UID, then ordered by namespace, name, and UID. A failure discards all temporary items for that source. `SelectBatch` preserves input source order and emits one outcome per source, including interrupted outcomes for sources not started after context cancellation.

## Options Considered

### Option A — Plan, bind, and execute through a list-only capability boundary

- Summary: Separate pure normalization from authorization binding and I/O. Require an opaque fully authorized plan for a uniform paginated LIST executor.
- Why chosen: It directly enforces `R4`, keeps policy evaluation outside selection, supports exact-name plus label and field constraints through one canonical query, makes empty/no-match behavior natural, and gives higher-layer tests narrow outbound boundaries.

### Option B — One service that resolves, evaluates policy, and reads objects

- Summary: Inject discovery, the active policy snapshot, and a dynamic client into one `SelectSource` service.
- Why rejected: It hides the authorization handoff, couples selection to policy lifecycle, makes target/decision mismatch impossible to observe explicitly, and would absorb responsibilities reserved for `authorization-enforcement` and reconciliation.

### Option C — GET exact names and LIST all other selector shapes

- Summary: Optimize name-only sources with GET while using LIST for label or field selection.
- Why rejected: A name combined with a label or field selector needs a second matching path or local field-selector evaluation. The branch duplicates error/no-match behavior and weakens the single query contract for little value before performance limits are measured.

### Option D — Use controller-runtime `client.Client` as the domain boundary

- Summary: Build unstructured objects and invoke controller-runtime Get/List methods directly from the selection service.
- Why rejected: The existing discovery boundary already returns a GVR, while `dynamic.Interface` addresses that identity directly and exposes native `ListOptions` pagination. A narrow local `ResourceLister` keeps either concrete client replaceable without leaking it into domain contracts.

## Simplicity And Elegance Review

- Simplest viable shape: one `internal/selection` package, three immutable phase values (`SelectionPlan`, `AuthorizedPlan`, `SelectionOutcome`), one narrow outbound `ResourceLister`, and one dynamic-client adapter.
- Coupling check: the planner consumes the existing discovery contract; the binder consumes existing access-policy request/decision values; only the adapter imports client-go dynamic APIs.
- First-draft challenge: an orchestration service, authorizer callback, GET fast path, local label filtering, concurrent workers, caches, and retry framework were removed because none is needed to satisfy the approved requirements.
- State check: no selected objects are cached or persisted by this feature; all temporary objects die with one source execution.
- Future-proofing: reconciliation can orchestrate plan/authorize/execute later, while performance limits can add concurrency and cardinality policy without changing the public selector contract.

## Components And Interfaces

### Public API extension

- Purpose: Make source target and selector intent structural and serializable in `kubeseer.io/v1alpha1`.
- Shape: add required `resource`, optional pointer `namespaces`, and optional pointer `selector` to `KubeseerSource`.
- Validation: require resource API version and Kind; keep `spec.sources` list-map uniqueness; validate namespace entries as a set of DNS labels; preserve `namespaces: {names: []}` by serializing `names` without `omitempty`.
- Selector representation: use `selector.name`, `selector.matchLabels`, `selector.matchExpressions` with Kubernetes `metav1.LabelSelectorRequirement`, and `selector.fieldSelector`.
- Generated outputs: deep-copy code and the structural CRD remain reproducible and are verified across the pinned Kubernetes compatibility matrix.
- Requirements: `R1`, `R2.AC7`, `R2.AC8`, `R3.AC1`, `R3.AC3`, `R3.AC4`, `NFR5`.

### Selection planner

- Purpose: Convert one public source and owner namespace into a deterministic plan without reading resource instances.
- Input: `context.Context`, owner namespace, `v1alpha1.KubeseerSource`, and the existing discovery resolver boundary.
- Behavior: build `discovery.SourceDescriptor`; preserve the source ID; resolve exact GVR/scope; validate source/resolution identity; canonicalize selectors; derive namespace or cluster targets; sort targets by namespace.
- Empty namespace behavior: a non-nil namespace block with an empty `names` list returns a valid zero-target plan.
- Scope behavior: cluster scope accepts only an omitted namespace block; namespaced scope defaults omission to the owner namespace.
- Requirements: `R1`, `R2`, `R3.AC6`, `R3.AC8`, `R4.AC1`, `R4.AC2`, `NFR2`, `NFR5`.

### Authorization binder

- Purpose: Turn a plan into the only value accepted by the executor after proving complete exact authorization coverage.
- Input: one `SelectionPlan` and a set of pairs containing `accesspolicy.Request` plus `accesspolicy.Decision`.
- Match key: source ID, API group, Kind, discovery scope, and namespace; cluster targets use an empty namespace.
- Validation: require exactly one matching allowed decision with `ReasonAllowed` per target; reject missing, extra, duplicate, denied, or mismatched pairs before returning any executable value.
- Output: `AuthorizedPlan` with unexported plan and target fields; no policy snapshot, loader, or evaluator is retained.
- Requirements: `R4`, `NFR1`, `NFR4`, `NFR6`.

### Resource lister adapter

- Purpose: Isolate Kubernetes resource-instance I/O behind the only operation required by this feature.
- Interface: `List(ctx context.Context, target ReadTarget, options metav1.ListOptions) (*unstructured.UnstructuredList, error)`.
- Production adapter: use `dynamic.Interface.Resource(target.GVR)` directly for cluster scope or `.Namespace(target.Namespace)` for namespaced scope, then call `List`.
- Boundary: expose no Get, Watch, create, update, delete, status, discovery, or policy methods.
- Requirements: `R2`, `R3`, `R4.AC3` through `R4.AC6`, `R7`, `NFR1`, `NFR5`, `NFR6`.

### Selection executor

- Purpose: Retrieve all pages for every target of one authorized plan and construct one atomic source outcome.
- Input: `AuthorizedPlan`, constructor-configured positive page limit, and `ResourceLister`.
- Query construction: reuse the plan's canonical label and field selector strings; set only `Limit` and the current opaque `Continue` token per page.
- Continuation behavior: continue while the token is non-empty; on one `ResourceExpired`, discard that target's pages and restart it with identical selectors; fail on a second expiration.
- Result behavior: validate identity metadata, deep-copy selected unstructured items, deduplicate by UID, derive provenance, and sort after every target succeeds.
- Requirements: `R3`, `R5`, `R6`, `R7`, `NFR2`, `NFR3`, `NFR4`, `NFR5`.

### Batch selector

- Purpose: Preserve independent source outcomes without introducing concurrency policy.
- Interface: `SelectBatch(ctx context.Context, plans []AuthorizedPlan) []SelectionOutcome`.
- Behavior: execute in input order; never discard completed sibling outcomes because one source fails; fill unstarted entries with interrupted outcomes after cancellation.
- Requirements: `R6.AC3`, `R6.AC5`, `R6.AC7`, `NFR2`, `NFR3`.

## Data Models

### Public API types

```go
type KubeseerSource struct {
    ID         string              `json:"id"`
    Resource   ResourceReference   `json:"resource"`
    Namespaces *NamespaceSelection `json:"namespaces,omitempty"`
    Selector   *ResourceSelector   `json:"selector,omitempty"`
}

type ResourceReference struct {
    APIVersion string `json:"apiVersion"`
    Kind       string `json:"kind"`
}

type NamespaceSelection struct {
    Names []string `json:"names"`
}

type ResourceSelector struct {
    Name             string                              `json:"name,omitempty"`
    MatchLabels      map[string]string                   `json:"matchLabels,omitempty"`
    MatchExpressions []metav1.LabelSelectorRequirement  `json:"matchExpressions,omitempty"`
    FieldSelector    string                              `json:"fieldSelector,omitempty"`
}
```

`Namespaces` is a pointer because nil means “use the containing Kubeseer namespace” for a namespaced resource, while non-nil with `Names: []` means “select nothing.” `Selector` may be nil or structurally empty; both mean “match everything in each target.” Resource and selector types contain no policy reference, permission, impersonation, or bypass field.

### Internal selection values

```go
type ReadTarget struct {
    SourceID string
    GVR      schema.GroupVersionResource
    Kind     string
    Scope    discovery.Scope
    Namespace string
}

type SelectionPlan struct {
    SourceID      string
    APIVersion    string
    Kind          string
    Targets       []ReadTarget
    LabelSelector string
    FieldSelector string
}

type Authorization struct {
    Request  accesspolicy.Request
    Decision accesspolicy.Decision
}

type AuthorizedPlan struct {
    plan SelectionPlan
}

type Provenance struct {
    APIVersion string
    Kind       string
    Namespace  string
    Name       string
    UID        types.UID
}

type SelectedResource struct {
    Object     *unstructured.Unstructured
    Provenance Provenance
}

type SelectionOutcome struct {
    SourceID  string
    Resources []SelectedResource
    Err       *SelectionError
}
```

The actual implementation may keep target slices and selector strings private behind constructors. Returned objects are deep copies so callers cannot mutate the executor's temporary collection. The plan retains declared API version and Kind for stable provenance; the GVR supplies the dynamic endpoint and API group used for authorization.

## Selector Normalization And Read Flow

1. Convert `matchLabels` and `matchExpressions` through `metav1.LabelSelectorAsSelector`; conversion failure is `InvalidSelector`. [LabelSelectorAsSelector](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#LabelSelectorAsSelector)
2. Parse `fieldSelector` through `fields.ParseSelector`; parse failure is `InvalidSelector`.
3. If `selector.name` is present, combine `fields.OneTermEqualSelector("metadata.name", name)` with the parsed field selector using `fields.AndSelectors`.
4. Serialize both selectors once into canonical strings stored in the immutable plan.
5. Bind every target to an exact allowed policy request before the executor receives the plan.
6. For each target, issue LIST with the same label selector, field selector, and limit on every page; change only `Continue`.
7. Continue until the response token is empty, even when an intermediate page contains zero items.
8. Validate object name and UID, accumulate target-local items, and restart target-local accumulation once if the continuation token expires.
9. After all targets succeed, deduplicate by UID and sort by namespace, name, then UID.

The Kubernetes API documents label and field selectors as list query parameters and requires a continued request to retain the original query parameters. It also documents a consistent snapshot across valid continued requests; this design deliberately restarts after an expired token rather than accepting the server's optional inconsistent continuation. [Kubernetes field selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/), [Kubernetes API concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/)

## Error Handling

- Public structural failures are rejected by the API server; the planner repeats security- and I/O-relevant checks defensively for typed values constructed directly in Go.
- Discovery errors retain the existing source-scoped `discovery.ResolutionError` and stop before authorization or resource-instance access.
- Planner errors use stable reasons: `InvalidSource`, `InvalidNamespaceScope`, and `InvalidSelector`.
- Binder errors use `AuthorizationMissing`, `AuthorizationDenied`, and `AuthorizationMismatch`; no partially authorized plan is executable.
- `apierrors.IsForbidden` maps to `ReadForbidden`, distinct from installation-policy denial.
- `apierrors.IsBadRequest` or `apierrors.IsInvalid` for a syntactically valid field-selector request maps to `UnsupportedSelector`.
- Context cancellation, deadline expiration, API timeout, or server timeout maps to `ReadInterrupted` when the caller context ended, otherwise `ReadUnavailable`.
- `apierrors.IsResourceExpired` restarts the current target once; a second expiration maps to `ListExpired`.
- Endpoint not found, service unavailable, rate limiting, and other API failures map to stable source-scoped read reasons without copying raw response bodies into user-facing messages.
- An object missing required name or UID metadata maps to `InvalidObject`; the source outcome is discarded rather than emitting ambiguous provenance.
- A zero-item completed list is a successful empty outcome, including an exact name that does not exist.

`SelectionError` carries `SourceID`, stable `Reason`, sanitized `Message`, and an unwrap-only `Cause`. Error strings expose no unstructured object, Secret data, selector result values, credentials, or raw upstream response bodies.

## Security Considerations

- Planning and selector parsing cannot read Kubernetes resource instances.
- Binding validates all targets before any target is passed to the executor, avoiding partial reads that could reveal which allowed namespace was processed before a denial.
- `AuthorizedPlan` is the executor's only input type and has no public field constructor; callers cannot accidentally pass an unbound plan.
- Authorization matching uses the existing exact `accesspolicy.Request` dimensions and never infers namespace or scope from Kind naming.
- The dynamic adapter addresses exactly the bound GVR and namespace. Label, field, and name constraints may narrow the result but cannot change the authorized target.
- Logical policy decisions remain distinct from ServiceAccount RBAC. A Forbidden response is an operational read failure, not a policy decision.
- Selected objects remain in-memory values for downstream extraction and are never logged, persisted, or copied into error messages by this feature.
- No user RBAC, impersonation, policy loading, policy watching, or stale-result invalidation is introduced here.

## Failure Modes And Tradeoffs

- Failure mode: one namespace is denied or lacks an authorization result. Mitigation: binding rejects the complete source before I/O. Tradeoff: a multi-namespace source cannot return partial data, matching the approved source-atomic policy.
- Failure mode: a source combines exact name with label or field constraints. Mitigation: encode exact name as a server-side field term and use one LIST query. Tradeoff: name-only reads do not receive a GET optimization in this feature.
- Failure mode: a resource endpoint supports field-selector syntax but not the requested field. Mitigation: preserve API-server evaluation and map its rejection to `UnsupportedSelector`. Tradeoff: supported fields remain resource-specific and are not simulated locally.
- Failure mode: a continuation token expires. Mitigation: discard target-local pages and restart once from an empty token. Tradeoff: a very slow list may repeat work once, then fail rather than loop or merge inconsistent snapshots.
- Failure mode: an API read fails after earlier targets succeeded. Mitigation: keep items source-local until every target completes, then discard on failure. Tradeoff: memory usage is proportional to the selected source result until product-wide limits are introduced.
- Failure mode: the same UID appears through overlapping targets. Mitigation: deterministic UID deduplication after all targets complete. Tradeoff: the first occurrence in deterministic target/page order supplies the retained object copy.
- Failure mode: a discovery mapping becomes stale before LIST. Mitigation: return a typed resource-unavailable outcome with the wrapped cause so later reconciliation can invalidate and rebuild the plan. Tradeoff: this feature does not perform an unapproved reauthorization loop internally.
- Failure mode: sequential execution is slower for many sources. Mitigation: keep source outcomes independent and expose a future-safe batch boundary. Tradeoff: concurrency is deferred until `performance-and-limits` defines budgets and cancellation behavior.

## Testing Strategy

### API contract through envtest

- Extend `TestAPIContract` to round-trip the new typed source fields and inspect the installed CRD schema.
- Prove required `resource.apiVersion`/`resource.kind`, unique source IDs, namespace-name set semantics, and selector serialization.
- Prove omitted `namespaces` remains nil while `namespaces: {names: []}` remains explicitly non-nil and empty after API persistence.
- Reject invalid namespaces, duplicate namespaces, missing resource coordinates, and structurally invalid selector shapes.
- Regenerate deep-copy and CRD artifacts, then run the existing Kubernetes 1.35.6 and 1.36.2 compatibility matrix.

### Cross-module integration

- Extend the real `TestModuleIntegration` production path from discovery and access policy through selection planning, binding, and execution.
- Compose the real discovery resolver, policy compiler/evaluator, planner, binder, and executor; control only outbound discovery and resource-list infrastructure.
- Prove namespaced default/list/empty behavior, cluster scope, exact authorization matching, denial-before-read, mismatched decision rejection, sibling-source isolation, atomic discard, deterministic errors, UID deduplication, and ordering.
- Script list pages and stable API errors at the narrow `ResourceLister` boundary to prove continuation, one ResourceExpired restart, second-expiration failure, Forbidden distinction, unsupported field selector, cancellation, and confidentiality without package-local unit tests.

### Kubernetes API selection through envtest

- Add a named selection envtest suite using the real dynamic client and disposable ConfigMap, Secret-free CRD-backed, and cluster-scoped fixtures.
- Exercise exact name, matchLabels, matchExpressions, supported and unsupported field selectors, match-all, zero match, cross-namespace targets, and server pagination with a small constructor page limit.
- Assert every listed object carries stable provenance and that diagnostics never include fixture values.
- Run via `make test-api`; never fall back to ambient kubeconfig or external credentials.

### Static and generated checks

- `make verify` proves generated deep-copy/CRD artifacts, formatting, and the no-unit-test policy.
- `go build ./...` proves package composition.
- `go mod tidy -diff` proves dependency declarations remain exact without rewriting files.
- No test relies on arbitrary sleeps; envtest uses the shared harness and bounded API calls.

## Verification Plan

- API proof: `TestAPIContract` demonstrates `R1`, structural portions of `R2`/`R3`, omitted-versus-empty persistence, `NFR5`, and compatibility.
- Planning proof: cross-module scenarios demonstrate discovery handoff, scope-derived targets, selector normalization, empty plans, and deterministic target order for `R1`–`R3`.
- Security proof: the real access-policy evaluator plus binder and counting lister demonstrate no read before complete exact authorization for `R4` and `NFR1`.
- Selection proof: real envtest dynamic-client scenarios demonstrate match, no-match, namespace, cluster-scope, provenance, field/label server evaluation, and pagination behavior for `R2`, `R3`, `R5`, and `R7`.
- Failure proof: scripted outbound failures at the cross-module layer demonstrate source atomicity, sibling isolation, stable reason mapping, cancellation, pagination expiry, RBAC distinction, and sanitized diagnostics for `R6`, `NFR3`, and `NFR4`.
- Determinism proof: repeated and permuted target/page inputs demonstrate canonical selectors, UID deduplication, stable sort order, and stable errors for `NFR2`.
- Toolchain proof: `make test-integration`, `make test-api`, `make test-compatibility`, `make verify`, `go build ./...`, and `go mod tidy -diff` provide the executable evidence expected by later tasks.
- Operational evidence: named test markers report which module/API contract ran and its stable reason on failure; no runtime metrics or status schema is added by this feature.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Public API extension; Selection planner; discovery handoff; API and module-integration tests |
| `R2` | Pointer-preserved NamespaceSelection; scope-derived ReadTargets; planner and envtest namespace scenarios |
| `R3` | ResourceSelector; selector normalization; list-only executor; real and scripted selector tests |
| `R4` | exact ReadTarget tuple; Authorization binder; opaque AuthorizedPlan; counting-lister security proof |
| `R5` | Provenance; UID deduplication; stable sort; no-match and repeatability scenarios |
| `R6` | SelectionError; source-local accumulation; SelectBatch; API error classification and isolation tests |
| `R7` | constructor page limit; unchanged continued queries; one consistent restart; pagination tests |
| `NFR1` | complete pre-I/O binding; opaque capability input; exact dynamic-client addressing |
| `NFR2` | canonical selectors; sorted targets/results; UID deduplication; stable reasons |
| `NFR3` | source-local atomic buffer; bounded restart; context propagation; independent outcomes |
| `NFR4` | sanitized SelectionError; unwrap-only cause; confidentiality assertions |
| `NFR5` | Kubernetes metav1 selectors, fields selectors, dynamic client, ListOptions, API errors, and compatibility matrix |
| `NFR6` | narrow resolver/lister boundaries; real cross-module composition; envtest dynamic-client coverage |
