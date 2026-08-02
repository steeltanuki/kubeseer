---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-02T08:25:14Z
last_modified: 2026-08-02T08:25:14Z
approved_fingerprint: sha256:f7a99b5d19399e9f98b7c6631911cbb448abbb53d361cc1be48e1c6dbae3de93
---

# Requirements Document

## Introduction

This feature defines the administrator-owned installation access policy that
sets the maximum Kubernetes observation scope available to Kubeseer. The policy
describes allowed namespaces, API groups, kinds, and cluster-scoped access. It
also defines deterministic validation and fail-closed decisions that later
authorization enforcement applies before any resource instance is read.

The policy is a logical security boundary independent of the operator
ServiceAccount's Kubernetes RBAC. Broader ServiceAccount permissions never
broaden the policy, while a logical allow decision does not grant RBAC that the
ServiceAccount does not possess.

<!-- assumed: KubeseerAccessPolicy is cluster-scoped because it defines one installation-wide maximum rather than per-namespace user configuration (source: .walden/constitution.md Project Summary and Architecture Rules) -->
<!-- assumed: the active installation policy is the singleton named installation-access-ceiling so its object name communicates its installation-wide maximum-permission role (source: user correction during design review) -->
<!-- assumed: namespace exclusion has highest precedence, explicit inclusion may add a system namespace in AllNonSystem mode, and the system namespace list defaults to kube-system, kube-public, and kube-node-lease (source: SPECIFICATIONS.md includes and example model) -->
<!-- assumed: resource authorization uses exact API-group and kind allowlists without wildcards in this feature (source: SPECIFICATIONS.md explicit resource lists and .walden/constitution.md Non-Negotiable Security Rules) -->

## Requirements

### R1 Installation-wide administrative policy

**User Story:** As a cluster administrator, I want one installation-level policy, so that every Kubeseer observation is bounded by the same administrator-controlled maximum scope.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL define `KubeseerAccessPolicy` as cluster-scoped administrative configuration.
2. `R1.AC2` The system SHALL recognize the cluster-scoped `KubeseerAccessPolicy` named `installation-access-ceiling` as the active installation policy.
3. `R1.AC3` The system SHALL keep installation access policy fields separate from every namespaced `Kubeseer` resource.
4. `R1.AC4` WHEN a Kubeseer request is evaluated, the system SHALL treat the active installation policy as the maximum scope that the request may narrow.

### R2 Namespace policy modes

**User Story:** As a cluster administrator, I want explicit namespace modes and overrides, so that I can express narrow, broad, or non-system observation boundaries predictably.

#### Acceptance Criteria

1. `R2.AC1` WHEN a namespace is evaluated in `Explicit` mode, the system SHALL allow it only when it appears in `include` and does not appear in `exclude`.
2. `R2.AC2` WHEN a namespace is evaluated in `All` mode, the system SHALL allow it only when it does not appear in `exclude`.
3. `R2.AC3` WHEN a namespace is evaluated in `AllNonSystem` mode, the system SHALL allow it only when it is not classified as a system namespace or it appears in `include`, unless it appears in `exclude`.
4. `R2.AC4` WHEN the same namespace appears in both `include` and `exclude`, the system SHALL treat that namespace as excluded.
5. `R2.AC5` WHEN `systemNamespaces` is omitted, the system SHALL classify `kube-system`, `kube-public`, and `kube-node-lease` as system namespaces.
6. `R2.AC6` WHEN `systemNamespaces` is specified, the system SHALL use that exact set to classify system namespaces.

### R3 Resource type allowlist

**User Story:** As a cluster administrator, I want to allow exact Kubernetes API groups and kinds, so that the operator cannot observe an unapproved resource type.

#### Acceptance Criteria

1. `R3.AC1` WHEN a resolved resource type is evaluated, the system SHALL allow it only when a resource rule contains its exact API group and kind.
2. `R3.AC2` WHEN a core Kubernetes resource type is configured, the system SHALL represent its API group as the empty string.
3. `R3.AC3` WHEN a built-in resource type matches an enabled resource rule, the system SHALL evaluate it with the same allowlist semantics as any other resource type.
4. `R3.AC4` WHEN a CRD-backed resource type matches an enabled resource rule, the system SHALL evaluate it with the same allowlist semantics as any other resource type.
5. `R3.AC5` IF no resource rule contains the resolved API group and kind, THEN the system SHALL deny the resource type with a stable resource-denied reason.
6. `R3.AC6` WHEN one resource rule lists multiple API groups and kinds, the system SHALL allow each exact API-group and kind combination represented by that rule.

### R4 Namespaced and cluster-scoped decisions

**User Story:** As a cluster administrator, I want namespace and resource-scope controls evaluated explicitly, so that cluster-scoped access cannot be inferred or accidentally enabled.

#### Acceptance Criteria

1. `R4.AC1` WHEN discovery identifies a requested resource as namespaced, the system SHALL allow it only when both its namespace and resource type are allowed by the active policy.
2. `R4.AC2` WHEN discovery identifies a requested resource as cluster-scoped, the system SHALL allow it only when cluster-scoped access is enabled and its resource type is allowed by the active policy.
3. `R4.AC3` The system SHALL use disabled as the default value for cluster-scoped access.
4. `R4.AC4` IF cluster-scoped access is disabled and a cluster-scoped resource is requested, THEN the system SHALL deny the request with a stable cluster-scope-denied reason.
5. `R4.AC5` WHEN a cluster-scoped resource is evaluated, the system SHALL ignore namespace policy fields for that resource.

### R5 Policy validation

**User Story:** As a cluster administrator, I want invalid policies rejected clearly, so that ambiguous configuration cannot become an authorization boundary.

#### Acceptance Criteria

1. `R5.AC1` IF a policy uses a namespace mode other than `Explicit`, `All`, or `AllNonSystem`, THEN the system SHALL mark the policy invalid.
2. `R5.AC2` IF a namespace entry is not a valid Kubernetes namespace name, THEN the system SHALL mark the policy invalid.
3. `R5.AC3` IF a namespace list contains duplicate entries, THEN the system SHALL mark the policy invalid.
4. `R5.AC4` IF a resource rule has no API groups or no kinds, THEN the system SHALL mark the policy invalid.
5. `R5.AC5` IF a resource rule contains an invalid API group, invalid kind, or wildcard, THEN the system SHALL mark the policy invalid.
6. `R5.AC6` IF a `KubeseerAccessPolicy` has a name other than `installation-access-ceiling`, THEN the system SHALL mark the policy invalid.
7. `R5.AC7` WHEN a policy intentionally contains an empty namespace scope, the system SHALL treat it as a valid deny-all namespace boundary.
8. `R5.AC8` WHEN a policy intentionally contains an empty resource allowlist, the system SHALL treat it as a valid deny-all resource boundary.
9. `R5.AC9` WHEN policy validation fails, the system SHALL report a stable policy-invalid reason with a field-specific human-readable message.

### R6 Missing, invalid, and unavailable policy behavior

**User Story:** As a security-conscious operator, I want policy failures to deny observation, so that configuration problems cannot expand access.

#### Acceptance Criteria

1. `R6.AC1` IF the active `KubeseerAccessPolicy` is missing, THEN the system SHALL deny every observation request with a stable policy-missing reason.
2. `R6.AC2` IF the active `KubeseerAccessPolicy` is invalid, THEN the system SHALL deny every observation request with a stable policy-invalid reason.
3. `R6.AC3` IF the active `KubeseerAccessPolicy` cannot be loaded, THEN the system SHALL deny every observation request with a stable policy-unavailable reason.
4. `R6.AC4` WHILE the active policy is missing, invalid, or unavailable, the system SHALL not produce an allow decision from cached or default permissive configuration.
5. `R6.AC5` WHEN a policy failure is reported, the system SHALL omit observed resource contents and sensitive extracted values from the diagnostic.

### R7 Logical policy and Kubernetes RBAC separation

**User Story:** As a platform operator, I want logical policy decisions distinct from Kubernetes RBAC, so that operational permissions cannot bypass Kubeseer's security model.

#### Acceptance Criteria

1. `R7.AC1` The system SHALL compute logical policy decisions independently of the permissions granted to the operator ServiceAccount.
2. `R7.AC2` IF the operator ServiceAccount has broader RBAC than the active policy, THEN the system SHALL keep out-of-policy requests denied.
3. `R7.AC3` WHEN the active policy allows a request, the system SHALL represent that decision without implying that Kubernetes RBAC also permits the read.
4. `R7.AC4` IF Kubernetes RBAC denies a logically allowed request, THEN the system SHALL preserve the logical allow decision as distinct from the RBAC failure.

### R8 Deterministic policy decisions

**User Story:** As an authorization-enforcement component, I want stable policy outcomes, so that reconciliation and status handling can react consistently without exposing protected data.

#### Acceptance Criteria

1. `R8.AC1` WHEN equivalent policy and request inputs are evaluated, the system SHALL return the same allow or deny decision.
2. `R8.AC2` WHEN a request is denied, the system SHALL return exactly one stable primary reason associated with that request.
3. `R8.AC3` WHEN a request satisfies every applicable namespace, resource-type, and scope constraint, the system SHALL return an allow decision.
4. `R8.AC4` IF more than one policy restriction denies a request, THEN the system SHALL select the primary denial reason using a documented deterministic precedence.
5. `R8.AC5` WHILE a compiled valid policy is active, the system SHALL evaluate requests without querying the Kubernetes API for each decision.
6. `R8.AC6` WHEN the policy evaluator receives a policy and normalized request inputs, the system SHALL return a decision without reading Kubernetes resource instances.

## Non-Functional Requirements

- `NFR1` **Security:** Policy loading, validation, and evaluation SHALL fail closed without broadening access because of missing state, invalid state, cache state, or ServiceAccount RBAC.
- `NFR2` **Determinism:** Equivalent policies and requests SHALL produce byte-for-byte stable decisions, reason codes, and human-readable messages.
- `NFR3` **Performance:** A compiled valid policy SHALL support in-memory request evaluation without a Kubernetes API call per decision.
- `NFR4` **Testability:** Namespace, resource-type, scope, validation, and failure behavior SHALL be testable through pure or narrow interfaces without reading Kubernetes resource instances.
- `NFR5` **Confidentiality:** Policy diagnostics SHALL identify configuration fields and requested API identities without including observed resource contents or extracted values.

## Constraints And Dependencies

- `C1` The feature depends on the approved `kubeseer-api-foundation` API conventions and the `resource-discovery` scope result; it must reuse `kubeseer.io/v1alpha1` and must not duplicate discovery logic.
- `C2` `KubeseerAccessPolicy` is administrator-owned installation configuration; users of namespaced `Kubeseer` resources cannot modify or override it through a `Kubeseer` spec.
- `C3` The policy is a logical maximum and does not create Kubernetes RBAC permissions; packaging remains responsible for aligning ServiceAccount permissions with the configured policy as closely as practical.
- `C4` Later `authorization-enforcement` must apply these decisions before every resource-instance read and must react to policy restriction changes.
- `C5` API groups and kinds are matched against discovery-normalized identity and scope, never inferred from user-supplied naming conventions.
- `C6` Kubeseer-authored API types, generated code, manifests, and tests remain under Apache License 2.0 with Alessandro Rontani as the default copyright holder.

## Out Of Scope

- Reading, listing, watching, filtering, or extracting values from Kubernetes resource instances.
- Runtime enforcement before resource reads, invalidation of previously published results, or reconciliation triggered by policy changes; these belong to `authorization-enforcement` and `reconciliation-runtime`.
- Creating or broadening operator ServiceAccount Roles, ClusterRoles, or bindings; these belong to `packaging-and-installation`.
- Evaluating permissions of users who create `Kubeseer` resources.
- Namespace label selectors, API-group wildcards, kind wildcards, regular expressions, or arbitrary policy expressions.
- Admission webhooks for discovery-dependent policy checks; those belong to `admission-validation`.
- JSONPath extraction, typed conversion, filtering, aggregation, status publication, and observability pipelines.
