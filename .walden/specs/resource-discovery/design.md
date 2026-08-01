---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-01T11:47:16Z
last_modified: 2026-08-01T11:47:16Z
approved_fingerprint: sha256:3758e9ed57d131cec58d72b1d2e659fabdf35d9552eb5f1f4c482f606ab5ea68
source_requirements_approved_at: 2026-08-01T11:33:36Z
source_requirements_fingerprint: sha256:3d227198f729edbadbff52f8885f0c9af6d35f19a2b2c761565dacc6ff3109c6
---

# Feature Design

## Overview

Implement a small, internal discovery package that resolves a normalized
source descriptor into a `GroupVersionResource` and an explicit namespaced or
cluster-scoped result. The package uses the Kubernetes `client-go/discovery`
client for one group/version at a time, wraps it with a resolver-owned
time-to-live cache, and exposes explicit invalidation for CRD changes. It
returns source-scoped typed errors and batch outcomes without reading resource
instances or writing status.

The public `Kubeseer` CRD is not changed. The later `resource-selection`
feature supplies source configuration and uses this boundary before constructing
dynamic resource clients.

<!-- assumed: a five-minute default cache TTL is sufficient for normal reconciliation and remains configurable for tests and deployments; the requirements intentionally specify refresh behavior rather than a public duration (source: R5 and NFR1) -->
<!-- assumed: APIResource entries whose name contains a slash are subresources and are rejected as ambiguous for this foundation; subresource support requires a later approved contract (source: resource-selection is the consumer of concrete resource reads) -->

## Architecture

```text
normalized SourceDescriptor
        │
        ▼
  DiscoveryResolver ── resolve GroupVersion ──> DiscoveryClient
        │                                      (one API request)
        ├── TTL cache keyed by group/version + kind
        ├── explicit invalidation
        └── source-scoped Resolution / ResolutionError
```

The resolver parses the descriptor's `apiVersion` with
`schema.ParseGroupVersion`. It asks `ServerResourcesForGroupVersion` for that
group/version, finds an exact `APIResource.Kind` match, and constructs the
resource GVR from the containing group/version and `APIResource.Name`. The
`APIResource.Namespaced` bit is copied into an explicit scope enum; kind or
group naming never infers scope. `APIResource` is the Kubernetes discovery
contract that carries the resource name, kind, and namespaced flag. [client-go
discovery](https://pkg.go.dev/k8s.io/client-go/discovery),
[APIResource](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#APIResource)

The cache stores only successful, non-subresource resolutions. A fresh hit
avoids discovery. An expired entry is replaced by one live request. Explicit
invalidation removes matching entries. A caller that observes a stale mapping
can invalidate the corresponding key and request one bounded retry; the
resolver does not loop indefinitely or read an object to verify the mapping.

## Options Considered

### Option A — Resolver-owned TTL cache over a narrow discovery interface

- Summary: Keep a narrow `ServerResourcesForGroupVersion` interface behind a
  resolver, with an in-memory TTL cache, explicit invalidation methods, a
  mutex, and an injectable clock.
- Why chosen: It makes the required freshness interval, request-count behavior,
  invalidation, and deterministic tests explicit while keeping client-go out of
  the domain model. The same boundary accepts the real discovery client and a
  scripted fake.

### Option B — Use `client-go/discovery/cached/memory` as the complete cache

- Summary: Delegate all caching to `CachedDiscoveryInterface`, using its
  `Fresh()` and `Invalidate()` methods.
- Why rejected: The client-go cache is invalidation-oriented and does not
  provide the resolver's per-entry refresh interval or source/key-level
  invalidation contract. It also makes request-count and expiry behavior depend
  on client-go internals rather than the feature boundary. The resolver may
  still be composed with a cached client later, but it cannot rely on that
  implementation for the public contract. [CachedDiscoveryInterface](https://pkg.go.dev/k8s.io/client-go/discovery#CachedDiscoveryInterface)

### Option C — Discover all groups and resources for every resolution

- Summary: Call `ServerGroupsAndResources` once and scan the complete response
  for each source.
- Why rejected: It increases startup cost and error surface, makes partial
  discovery responses harder to associate with one source, and provides no
  benefit when a descriptor already identifies one group/version.

## Simplicity And Elegance Review

- Simplest viable shape: one resolver, one narrow client interface, one mutex
  protected map, one clock abstraction, and typed result/error values.
- Coupling check: only the adapter knows client-go; selection, authorization,
  dynamic reads, and status writers consume plain resolver contracts.
- Cache check: cache keys and invalidation are resolver concepts, while the
  Kubernetes discovery client remains replaceable and testable.
- Future-proofing: later features can add discovery verbs, preferred-version
  lookup, or subresource support without changing the initial CRD; no watch or
  controller-runtime manager is introduced here.
- Rejected complexity: no background refresh goroutine, shared global cache,
  disk persistence, or status mutation is needed to satisfy the approved
  requirements.

## Components And Interfaces

### Source descriptor

- Purpose: Carry the stable source identity and requested API type across the
  discovery boundary.
- Inputs: `SourceID`, `APIVersion`, and `Kind` strings.
- Validation: reject empty source IDs, malformed API versions, and empty kinds
  before a discovery request.
- Requirements: `R1.AC1`, `R4.AC1`, `R4.AC2`, `R6.AC4`.

### Discovery client adapter

- Purpose: Hide the client-go concrete client and expose only the request needed
  by this feature.
- Interface: `ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error)`.
- Dependency: `k8s.io/client-go/discovery.DiscoveryInterface` or a compatible
  wrapper. The real REST configuration supplies a request timeout because the
  client-go discovery method is synchronous and does not accept a context.
- Requirements: `R2.AC1`, `R2.AC2`, `R6.AC1`, `R6.AC2`, `NFR1`, `NFR3`.

### Discovery resolver

- Purpose: Parse API identity, consult the cache, query discovery on a miss or
  expiry, and return a typed resolution or source-scoped error.
- Operations:
  - `Resolve(ctx, descriptor) (Resolution, error)`;
  - `ResolveBatch(ctx, descriptors) []Outcome`;
  - `InvalidateGroupVersion(groupVersion)`;
  - `InvalidateResource(groupVersion, kind)`;
  - `InvalidateAll()`.
- Behavior: `ResolveBatch` never converts one source error into a batch-wide
  failure. Every outcome retains its source ID.
- Requirements: `R1`, `R3`, `R4`, `R5`, `R6`, `NFR1`, `NFR2`, `NFR3`.

### Resolution and error values

- `Resolution`: source ID, GVR, and `ScopeNamespaced` or `ScopeCluster`.
- `Outcome`: source ID plus either a resolution or a `ResolutionError`.
- `ResolutionError`: stable reason (`UnknownType`, `DiscoveryUnavailable`,
  `InvalidDescriptor`, or `AmbiguousResource`) and a human-readable message
  that includes the source ID without including resource data.
- Requirements: `R1.AC2`, `R3.AC1`, `R3.AC2`, `R4`, `R6.AC3`, `R6.AC4`, `NFR2`.

### TTL cache

- Purpose: Avoid repeated discovery requests while allowing API and CRD changes
  to become visible.
- Key: canonical group/version string plus exact resource kind; source ID is
  not part of the key so two sources requesting the same type share metadata.
- Value: resolution, insertion/expiry time, and no resource content.
- Synchronization: a `sync.RWMutex` protects the map; the resolver performs one
  live request per expired key under a per-key refresh guard so concurrent
  reconciles do not stampede discovery.
- Requirements: `R5`, `NFR1`, `NFR2`, `NFR3`.

## Data Models

```go
type SourceDescriptor struct {
    SourceID   string
    APIVersion string
    Kind       string
}

type Scope int

const (
    ScopeNamespaced Scope = iota
    ScopeCluster
)

type Resolution struct {
    SourceID string
    Resource schema.GroupVersionResource
    Scope    Scope
}

type Outcome struct {
    SourceID  string
    Resolution *Resolution
    Err       *ResolutionError
}
```

`Resolution.Resource.Resource` is the plural discovery name. The resolver
accepts only one exact kind match whose resource name is not a subresource. Zero
matches produce `UnknownType`; more than one eligible match produces
`AmbiguousResource`. Scope is an enum rather than a boolean so callers cannot
mistake an omitted value for namespaced access.

The cache uses a canonical `schema.GroupVersion` string and kind as its key.
Invalidation by group/version removes every kind under that version;
invalidation by resource removes only the matching kind. All returned slices,
errors, and resolutions are newly allocated or immutable-by-convention values
so callers cannot mutate cache state.

## Error Handling

- Parse and descriptor validation errors return `InvalidDescriptor` without a
  discovery request.
- A discovery client error returns `DiscoveryUnavailable` with the source ID and
  wrapped diagnostic context. The underlying error is retained for logs/tests,
  but raw API responses and resource data are not copied into status messages.
- No matching resource returns `UnknownType`.
- Multiple eligible matches return `AmbiguousResource` in a stable order after
  sorting candidates by resource name.
- A cluster-scoped resolution cannot be converted to namespaced scope; the
  resolver returns a typed invalid-scope error before any dynamic client is
  constructed.
- `ResolveBatch` records each failure independently and continues with the next
  descriptor. A canceled context stops new work and marks unprocessed outcomes
  with the same bounded cancellation error.
- Expiry causes one live discovery call. Explicit invalidation or a caller's
  stale-mapping report removes the entry; one retry may repopulate it. No
  unbounded retry or background goroutine is used.

## Security Considerations

- The resolver calls discovery metadata endpoints only; it never calls a
  dynamic, typed, or metadata resource client.
- It does not evaluate selectors, namespaces, policy, RBAC, or permissions.
  Installation policy and authorization remain mandatory gates before later
  resource reads.
- Error/status-ready messages contain source IDs, API identity, and stable
  reasons only. They do not include Secret contents, extracted values, or
  arbitrary API payloads.
- Cache entries contain no object data and are process-local, so they cannot
  become a second persistence surface for observed resources.

## Failure Modes And Tradeoffs

- Failure mode: discovery is temporarily unavailable.
  - Mitigation: bounded client timeout, source-scoped `DiscoveryUnavailable`,
    and independent batch outcomes.
  - Tradeoff: an expired cache is not served indefinitely, so callers may see a
    transient error instead of stale metadata.
- Failure mode: a CRD is removed or its resource name/scope changes.
  - Mitigation: explicit invalidation and one bounded rediscovery after a stale
    mapping is reported.
  - Tradeoff: this boundary does not watch CRDs; later controller runtime work
    can connect watch events to invalidation.
- Failure mode: two eligible discovery entries match a kind.
  - Mitigation: stable candidate sorting and `AmbiguousResource` failure.
  - Tradeoff: the resolver refuses to guess, requiring a later contract for
    subresources or alternate identities.
- Failure mode: concurrent reconciles refresh the same key.
  - Mitigation: per-key refresh coordination under the cache mutex.
  - Tradeoff: one caller waits for the bounded discovery request while others
    reuse its result.
- Failure mode: a cluster-scoped resource is passed to namespaced selection.
  - Mitigation: explicit scope enum and typed invalid-scope rejection.
  - Tradeoff: callers must branch on scope before constructing a dynamic client.

## Testing Strategy

### Unit tests

- Use a scripted fake implementing the narrow discovery interface.
- Resolve core `v1` and grouped API versions, asserting exact GVR and scope.
- Resolve a CRD-shaped group/version and assert identical behavior to a built-in
  resource.
- Assert namespaced and cluster-scoped results and reject scope conversion.
- Assert unknown, unavailable, invalid, and ambiguous errors include source ID
  and stable reason.
- Resolve a batch with one failure and one success, asserting independent
  outcomes.
- Inject a fake clock and request counter to prove cache hits, expiry,
  group/version invalidation, resource invalidation, and one retry.
- Assert the fake has no resource-read method or calls, proving the boundary is
  metadata-only.

### Integration tests

- Use the existing envtest assets from the API foundation to install the
  Kubeseer CRD only when a later integration path needs it; discovery resolver
  tests do not depend on a controller manager.
- Add a Kubernetes API-server discovery smoke test for one built-in resource,
  one cluster-scoped resource, and one dynamically installed CRD when the
  implementation introduces that fixture.

### Static and quality checks

- `go test ./internal/discovery/...` with named test output.
- `go test -race ./internal/discovery/...` for cache synchronization.
- `go vet ./...` and `gofmt` on authored Go files.
- No test reads a resource instance or relies on arbitrary sleeps.

## Verification Plan

- Requirement proof: unit tests cover every resolution, scope, error, batch,
  cache, metadata-only, and determinism criterion (`R1`–`R6`).
- Cache proof: fake-clock/request-counter tests demonstrate fresh-hit,
  expiry-refresh, explicit invalidation, and one bounded stale retry (`R5`).
- Compatibility proof: an envtest discovery smoke test demonstrates built-in,
  cluster-scoped, and CRD-backed discovery against the pinned Kubernetes matrix
  from `kubeseer-api-foundation` (`R2`, `R3`, `NFR1`).
- Security proof: the fake discovery boundary and package dependency check show
  that no resource client is called by the resolver (`R6`, `C2`, `NFR3`).
- Operational evidence: test output names source IDs, stable reasons, and the
  discovery API version involved in failures; no observed data is emitted.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Source descriptor; Discovery resolver; Resolution values; identity unit tests |
| `R2` | Discovery client adapter; Discovery resolver; built-in and CRD-shaped tests |
| `R3` | Resolution scope enum; invalid-scope handling; scope unit tests |
| `R4` | ResolutionError; ResolveBatch; failure-isolation tests |
| `R5` | TTL cache; invalidation operations; fake-clock cache tests |
| `R6` | Metadata-only adapter; security boundary; fake call assertions |
| `NFR1` | REST timeout configuration; bounded refresh and retry behavior |
| `NFR2` | Canonical keys, stable sorting, deterministic errors, repeatability tests |
| `NFR3` | Narrow discovery interface, fake client, fake clock, request counters |
