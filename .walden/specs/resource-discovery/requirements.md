---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-01T11:33:36Z
last_modified: 2026-08-01T11:33:36Z
approved_fingerprint: sha256:3d227198f729edbadbff52f8885f0c9af6d35f19a2b2c761565dacc6ff3109c6
---

# Requirements Document

## Introduction

This feature defines the discovery boundary that turns a Kubeseer source
descriptor into the Kubernetes resource identity needed by later selection and
observation features. The resolver consumes a source identifier, `apiVersion`,
and `kind`, uses Kubernetes discovery metadata, and returns a
`GroupVersionResource` together with the resource scope. It supports built-in
resources and CRD-backed resources, isolates failures by source, and maintains
a refreshable cache. It does not read resource instances or enforce the
installation access policy.

<!-- assumed: source descriptors are an internal boundary carrying the stable source ID and requested apiVersion/kind; the public Kubeseer source schema is extended by the later resource-selection feature (source: SPECIFICATIONS.md resource-selection example and dependency order) -->
<!-- assumed: cache invalidation is explicit and also occurs after a cached mapping is rejected by an API read; watch-based CRD invalidation is deferred until a controller runtime exists (source: SPECIFICATIONS.md resource-discovery includes) -->

## Requirements

### R1 Resource identity resolution

**User Story:** As a controller component, I want to resolve a source's API identity, so that later selection code can address the correct Kubernetes resource endpoint.

#### Acceptance Criteria

1. `R1.AC1` WHEN a source descriptor provides a valid `apiVersion` and `kind` and discovery returns a matching resource, the system SHALL return the resolved `GroupVersionResource` associated with that source.
2. `R1.AC2` WHEN a source descriptor resolves successfully, the system SHALL return the discovery-reported resource scope associated with that source.

### R2 Supported Kubernetes resource types

**User Story:** As a platform operator, I want discovery to cover built-in and CRD-backed resources, so that Kubeseer sources work across the Kubernetes API surface.

#### Acceptance Criteria

1. `R2.AC1` WHEN discovery lists a matching built-in resource for a source descriptor, the system SHALL resolve that resource into a `GroupVersionResource`.
2. `R2.AC2` WHEN discovery lists a matching CRD-backed resource for a source descriptor, the system SHALL resolve that resource into a `GroupVersionResource`.

### R3 Resource scope correctness

**User Story:** As a controller component, I want scope metadata preserved from discovery, so that cluster-scoped resources are never addressed through a namespace.

#### Acceptance Criteria

1. `R3.AC1` WHEN discovery reports a resource as namespaced, the system SHALL mark the resolution as namespaced.
2. `R3.AC2` WHEN discovery reports a resource as cluster-scoped, the system SHALL mark the resolution as cluster-scoped.
3. `R3.AC3` IF discovery reports a resource as cluster-scoped, THEN the system SHALL reject any attempt to represent that resolution as namespaced.

### R4 Source-scoped failures and isolation

**User Story:** As a controller component, I want failures to identify their source and remain isolated, so that one unavailable API does not hide useful outcomes from other sources.

#### Acceptance Criteria

1. `R4.AC1` IF no discovered resource matches a source descriptor's `apiVersion` and `kind`, THEN the system SHALL return an error containing that source identifier and a stable unknown-type reason.
2. `R4.AC2` IF a discovery request fails, THEN the system SHALL return an error containing the affected source identifier and a stable discovery-unavailable reason.
3. `R4.AC3` WHEN a batch resolves multiple source descriptors and one descriptor fails, the system SHALL return independent outcomes for the remaining descriptors.
4. `R4.AC4` WHEN a resolution error is passed to a status-producing caller, the system SHALL expose a stable reason and human-readable message for that source.

### R5 Discovery cache lifecycle

**User Story:** As a controller operator, I want discovery metadata cached and refreshable, so that normal reconciliation avoids unnecessary API discovery traffic without retaining removed CRDs indefinitely.

#### Acceptance Criteria

1. `R5.AC1` WHEN a fresh cached resolution matches a source descriptor, the system SHALL return the cached resolution without issuing a discovery request.
2. `R5.AC2` WHEN a cached resolution reaches its refresh interval, the system SHALL query discovery before returning a resolution for that descriptor.
3. `R5.AC3` WHEN an explicit invalidation is requested for a cached API group/version or resource, the system SHALL remove the corresponding cached entries before the next resolution.
4. `R5.AC4` IF a cached resolution is rejected because the resource was removed or modified, THEN the system SHALL invalidate that cache entry before returning the resolution outcome.
5. `R5.AC5` IF a cached resolution is rejected because the resource was removed or modified, THEN the system SHALL perform at most one bounded rediscovery before returning an updated resolution or an error.

### R6 Metadata-only and deterministic behavior

**User Story:** As a security-conscious operator, I want discovery separated from resource reads and deterministic, so that this boundary cannot expose observed data or produce unstable identities.

#### Acceptance Criteria

1. `R6.AC1` WHEN resolving a source descriptor, the system SHALL read Kubernetes discovery metadata only.
2. `R6.AC2` WHEN resolving a source descriptor, the system SHALL not read resource instances.
3. `R6.AC3` WHEN identical discovery metadata is supplied for a source descriptor, the system SHALL return the same resource identity and scope.
4. `R6.AC4` IF discovery returns ambiguous metadata for a source descriptor, THEN the system SHALL return a deterministic ambiguity error instead of selecting an arbitrary resource.

## Non-Functional Requirements

- `NFR1` **Bounded reliability:** Discovery calls and cache refreshes SHALL use bounded contexts or timeouts, and a failed refresh SHALL return a diagnosable source-scoped error.
- `NFR2` **Determinism:** Equivalent discovery responses SHALL produce byte-for-byte stable resource identities, scope values, reason codes, and error messages.
- `NFR3` **Testability:** The resolver SHALL support a deterministic discovery-client boundary so unit tests can assert request counts, cache hits, invalidation, and failure isolation without a live cluster.

## Constraints And Dependencies

- `C1` The feature depends on `kubeseer-api-foundation` and the Kubernetes discovery API; it must not introduce a second public API version or duplicate the CRD schema.
- `C2` Source selection fields and concrete resource reads belong to later `resource-selection`, `installation-access-policy`, and observation features; this boundary accepts normalized descriptors and returns metadata only.
- `C3` Discovery results can change when APIs or CRDs are installed, removed, or modified; stale cache entries must be recoverable through the refresh and invalidation rules above.
- `C4` The resolver must use Kubernetes discovery metadata and must not assume that a resource is namespaced from its kind or API group.

## Out Of Scope

- Extending the public `Kubeseer` CRD source schema with selectors, namespaces, or resource coordinates.
- Reading, listing, watching, or filtering resource instances.
- Enforcing installation access policy, Kubernetes RBAC, or user authorization.
- Selecting concrete objects, evaluating JSONPath, converting values, filtering, aggregating, or publishing final results.
- Implementing controller watches, reconciliation scheduling, status condition types, or retry policy beyond bounded discovery refresh behavior.
- Supporting arbitrary API proxies, aggregated-server-specific policy, or non-Kubernetes discovery protocols.
