---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T10:59:27Z
last_modified: 2026-08-31T10:59:27Z
approved_fingerprint: sha256:c75b59a2d2bdee9a4fb2c51cebae22a48df228d732b744457015ec3312ef2bf4
---

# Requirements Document

## Introduction

Kubeseer configuration currently has two validation boundaries. The structural
CRD schema rejects malformed public API shapes, while runtime planners,
discovery, and authorization reject semantic or cluster-dependent defects only
after persistence. This feature completes the admission boundary so invalid or
forbidden `Kubeseer` configurations are rejected before they can become stored
desired state, with deterministic and actionable Kubernetes API errors.

Static shape, enum, pattern, range, uniqueness, cardinality, and configuration
size constraints are enforced by the structural OpenAPI schema wherever that
schema can express them. A side-effect-free validating admission webhook owns
the checks that require parsing, cross-field references, type compatibility,
Kubernetes discovery, or the current installation access policy. The same
semantic planners and policy contracts remain mandatory at runtime: successful
admission is never an authorization capability, and later discovery or policy
changes cannot make a previously admitted decision authoritative.

This feature also rejects semantically invalid `KubeseerAccessPolicy` create
and update requests through the approved policy compiler. It never prevents an
administrator from narrowing or deleting the installation policy merely
because existing Kubeseer resources would become unauthorized.

<!-- assumed: validating admission covers CREATE and UPDATE of Kubeseer and KubeseerAccessPolicy main resources, while status updates and DELETE remain outside the webhook so invalid stored configuration cannot become undeletable -->
<!-- assumed: webhook unavailability fails closed for covered CREATE and UPDATE operations because early validation is part of the persisted configuration contract; runtime authorization still fails closed independently (source: .walden/constitution.md and approved authorization-enforcement requirements) -->
<!-- assumed: admission-time policy compliance uses the current canonical installation policy but produces no reusable authorization capability (source: approved installation-access-policy and authorization-enforcement requirements) -->
<!-- assumed: configuration-shape limits are implementation-owned defaults that may later be centralized or revised by performance-and-limits without changing fail-closed rejection semantics (source: SPECIFICATIONS.md admission-validation and performance-and-limits boundaries) -->
<!-- assumed: namespace admission validates syntax and policy compliance without requiring namespace existence because authorized namespaces may be provisioned after the Kubeseer configuration and runtime selection already owns unavailable-target behavior -->

## Requirements

### R1 Validation Layers And Admission Scope

**User Story:** As a cluster operator, I want each validation rule owned by the earliest reliable Kubernetes boundary, so that invalid configuration is rejected before persistence without weakening runtime safety.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL maintain a structural OpenAPI schema for every public `Kubeseer` and `KubeseerAccessPolicy` configuration field.
2. `R1.AC2` WHEN a CREATE request targets the main `Kubeseer` resource, the system SHALL run validating admission against the proposed spec.
3. `R1.AC3` WHEN an UPDATE request targets the main `Kubeseer` resource, the system SHALL run validating admission against the proposed spec.
4. `R1.AC4` WHEN a CREATE request targets the main `KubeseerAccessPolicy` resource, the system SHALL run validating admission against the proposed policy.
5. `R1.AC5` WHEN an UPDATE request targets the main `KubeseerAccessPolicy` resource, the system SHALL run validating admission against the proposed policy.
6. `R1.AC6` WHEN a request targets a status subresource, the system SHALL exclude that request from configuration admission validation.
7. `R1.AC7` WHEN a DELETE request targets a Kubeseer-owned resource, the system SHALL allow deletion independently of the stored configuration's current validity.
8. `R1.AC8` WHEN a covered request is marked as dry-run, the system SHALL apply the same validation decisions as the equivalent non-dry-run request.
9. `R1.AC9` The system SHALL declare the validating admission webhook as side-effect free.
10. `R1.AC10` IF the validating admission webhook is unavailable for a covered request, THEN the system SHALL fail that request without persisting the proposed object.
11. `R1.AC11` WHEN admission accepts a Kubeseer configuration, the system SHALL avoid representing that outcome as runtime authorization.
12. `R1.AC12` The system SHALL bound each validating admission webhook request to at most 10 seconds.

### R2 Structural Constraints And Configuration Budgets

**User Story:** As a platform operator, I want malformed or excessively large configuration rejected by the API server, so that admission work remains bounded and generated clients share one structural contract.

#### Acceptance Criteria

1. `R2.AC1` The system SHALL represent every configuration field through structural OpenAPI without an opaque preserve-unknown region.
2. `R2.AC2` IF a manifest omits a property required by an approved declarative-model contract, THEN the system SHALL reject the manifest without persisting it.
3. `R2.AC3` IF a manifest supplies a value outside a declared enum, THEN the system SHALL reject the manifest without persisting it.
4. `R2.AC4` IF a manifest violates a declared string pattern, length bound, or numeric range, THEN the system SHALL reject the manifest without persisting it.
5. `R2.AC5` IF a list-map repeats a declared map key, THEN the system SHALL reject the manifest without persisting it.
6. `R2.AC6` The system SHALL limit one Kubeseer spec to at most 32 source declarations.
7. `R2.AC7` The system SHALL limit one source to at most 64 explicit namespace names.
8. `R2.AC8` The system SHALL limit one source to at most 64 field declarations.
9. `R2.AC9` The system SHALL limit one field to at most 16 operator declarations.
10. `R2.AC10` The system SHALL limit one operator `values` list to at most 128 operands.
11. `R2.AC11` The system SHALL limit one source to at most 32 aggregation declarations.
12. `R2.AC12` The system SHALL limit one aggregation to at most 16 `groupBy` field references.
13. `R2.AC13` The system SHALL limit one selector to at most 64 label match expressions.
14. `R2.AC14` The system SHALL limit one label match expression to at most 64 values.
15. `R2.AC15` The system SHALL limit the canonical JSON encoding of one Kubeseer `spec` to at most 262144 bytes.
16. `R2.AC16` IF a configuration exceeds a declared cardinality budget, THEN the system SHALL reject the request before discovery or policy evaluation.
17. `R2.AC17` IF a Kubeseer spec exceeds its serialized-size budget, THEN the system SHALL reject the request before discovery or policy evaluation.
18. `R2.AC18` The system SHALL limit one selector to at most 64 exact label matches.
19. `R2.AC19` The system SHALL limit each installation-policy namespace list to at most 256 names.
20. `R2.AC20` The system SHALL limit one installation policy to at most 128 resource rules.
21. `R2.AC21` The system SHALL limit one resource rule to at most 64 API groups.
22. `R2.AC22` The system SHALL limit one resource rule to at most 64 Kinds.
23. `R2.AC23` The system SHALL limit the canonical JSON encoding of one KubeseerAccessPolicy `spec` to at most 262144 bytes.
24. `R2.AC24` IF a KubeseerAccessPolicy spec exceeds its serialized-size budget, THEN the system SHALL reject the request before policy compilation.

### R3 Source, Selector, Field, And JSONPath Validation

**User Story:** As a Kubeseer user, I want source and field mistakes reported at admission, so that selection and extraction do not begin from invalid desired state.

#### Acceptance Criteria

1. `R3.AC1` IF two sources use the same source identifier, THEN the system SHALL reject the Kubeseer request.
2. `R3.AC2` IF a source API version cannot be parsed into a non-empty Kubernetes group-version identity, THEN the system SHALL reject the Kubeseer request.
3. `R3.AC3` IF a source Kind is not a valid Kubernetes Kind identifier, THEN the system SHALL reject the Kubeseer request.
4. `R3.AC4` IF one explicit namespace list contains a duplicate name, THEN the system SHALL reject the Kubeseer request.
5. `R3.AC5` IF an exact-name selector is not a valid Kubernetes resource-name path segment, THEN the system SHALL reject the Kubeseer request.
6. `R3.AC6` IF a label selector is syntactically or semantically invalid under Kubernetes label-selector rules, THEN the system SHALL reject the Kubeseer request.
7. `R3.AC7` IF a field selector is syntactically invalid under Kubernetes field-selector rules, THEN the system SHALL reject the Kubeseer request.
8. `R3.AC8` IF two fields in one source use the same field name, THEN the system SHALL reject the Kubeseer request.
9. `R3.AC9` IF a field omits its logical type, THEN the system SHALL reject the Kubeseer request.
10. `R3.AC10` IF a field path has invalid syntax, THEN the system SHALL reject the Kubeseer request.
11. `R3.AC11` IF a field path uses JSONPath syntax outside the approved subset, THEN the system SHALL reject the Kubeseer request.
12. `R3.AC12` WHEN every field path is valid, the system SHALL preserve its submitted text unchanged in the persisted object.
13. `R3.AC13` WHEN one source omits fields or declares an explicit empty field list, the system SHALL treat that source shape as valid.
14. `R3.AC14` WHEN one source declares an explicit empty namespace list, the system SHALL treat that source as requesting no namespaced target.

### R4 Operator And Aggregation Validation

**User Story:** As a Kubeseer user, I want operator and aggregation declarations checked against their referenced typed fields, so that configuration errors cannot become data-dependent runtime failures.

#### Acceptance Criteria

1. `R4.AC1` IF an operator supplies an operand arity that differs from its approved operator contract, THEN the system SHALL reject the Kubeseer request.
2. `R4.AC2` IF an operator operand contains zero typed payload branches for a value state, THEN the system SHALL reject the Kubeseer request.
3. `R4.AC3` IF an operator operand contains more than one typed payload branch, THEN the system SHALL reject the Kubeseer request.
4. `R4.AC4` IF an operator operand uses the null state, THEN the system SHALL reject the Kubeseer request.
5. `R4.AC5` IF an operator operand payload is incompatible with the containing field type, THEN the system SHALL reject the Kubeseer request.
6. `R4.AC6` IF an operator is incompatible with the containing field type, THEN the system SHALL reject the Kubeseer request.
7. `R4.AC7` IF a `matches` operator contains an invalid regular expression, THEN the system SHALL reject the Kubeseer request.
8. `R4.AC8` IF an operator operand cannot be converted through the approved typed-output conversion matrix, THEN the system SHALL reject the Kubeseer request.
9. `R4.AC9` IF two aggregations in one source use the same aggregate name, THEN the system SHALL reject the Kubeseer request.
10. `R4.AC10` IF an aggregation references an undeclared target field, THEN the system SHALL reject the Kubeseer request.
11. `R4.AC11` IF an aggregation references an undeclared grouping field, THEN the system SHALL reject the Kubeseer request.
12. `R4.AC12` IF an aggregation repeats a field in `groupBy`, THEN the system SHALL reject the Kubeseer request.
13. `R4.AC13` IF a grouping field has a logical type outside the approved grouping-key set, THEN the system SHALL reject the Kubeseer request.
14. `R4.AC14` IF an aggregation function is incompatible with its target field type, THEN the system SHALL reject the Kubeseer request.
15. `R4.AC15` IF a function other than `average` declares `precision`, THEN the system SHALL reject the Kubeseer request.
16. `R4.AC16` IF a function other than `average` declares `roundingMode`, THEN the system SHALL reject the Kubeseer request.
17. `R4.AC17` WHEN an `average` aggregation omits precision or rounding mode, the system SHALL preserve omission for the approved runtime defaults.
18. `R4.AC18` WHEN all operator and aggregation declarations are valid, the system SHALL accept them under the approved runtime planners' semantic compatibility rules.

### R5 Discovery-Dependent Validation

**User Story:** As a Kubeseer user, I want resource identity and scope checked against the current cluster, so that unresolved or scope-incompatible sources are rejected before persistence.

#### Acceptance Criteria

1. `R5.AC1` WHEN a source resource identity is submitted, the system SHALL resolve its API version and Kind through current Kubernetes discovery.
2. `R5.AC2` IF discovery reports that a submitted API version and Kind do not identify a served resource, THEN the system SHALL reject the Kubeseer request with an unknown-resource reason.
3. `R5.AC3` WHEN discovery resolves a source, the system SHALL use the discovered API group, resource, version, Kind, and scope for admission checks.
4. `R5.AC4` IF a source declares namespaces for a cluster-scoped resource, THEN the system SHALL reject the Kubeseer request.
5. `R5.AC5` WHEN a namespaced source omits namespaces, the system SHALL validate the containing Kubeseer namespace as its requested target.
6. `R5.AC6` WHEN a namespaced source declares multiple namespaces, the system SHALL validate each distinct namespace as an exact requested target.
7. `R5.AC7` WHEN a namespaced source declares an explicit empty namespace list, the system SHALL perform no namespace policy check for that source.
8. `R5.AC8` IF discovery cannot be consulted because of a transient service or permission failure, THEN the system SHALL reject the request with a retryable validation-unavailable reason.
9. `R5.AC9` WHEN equivalent discovery information is available, the system SHALL produce the same resource-scope validation outcome.

### R6 Installation-Policy Compliance And Policy Admission

**User Story:** As a cluster administrator, I want admission to reject observation requests beyond the current installation ceiling while preserving my ability to tighten that ceiling, so that invalid desired state is prevented without weakening administrative control.

#### Acceptance Criteria

1. `R6.AC1` WHEN a Kubeseer request reaches policy validation, the system SHALL load the canonical `installation-access-ceiling` policy.
2. `R6.AC2` WHEN a source targets a namespaced resource, the system SHALL evaluate each exact namespaced target against the current compiled policy.
3. `R6.AC3` WHEN a source targets a cluster-scoped resource, the system SHALL evaluate its exact cluster-scoped target against the current compiled policy.
4. `R6.AC4` IF any exact target is outside the current installation policy, THEN the system SHALL reject the complete Kubeseer request with a policy-denied reason.
5. `R6.AC5` IF the canonical installation policy is missing, THEN the system SHALL reject the Kubeseer request with a policy-missing reason.
6. `R6.AC6` IF the canonical installation policy is invalid, THEN the system SHALL reject the Kubeseer request with a policy-invalid reason.
7. `R6.AC7` IF the canonical installation policy cannot be loaded because of a transient service or permission failure, THEN the system SHALL reject the request with a retryable validation-unavailable reason.
8. `R6.AC8` WHEN admission evaluates policy compliance, the system SHALL produce no authorization capability reusable by runtime resource access.
9. `R6.AC9` WHEN a KubeseerAccessPolicy is created or updated, the system SHALL validate it through the approved policy compilation semantics.
10. `R6.AC10` IF a proposed KubeseerAccessPolicy is semantically invalid, THEN the system SHALL reject the policy request without persisting it.
11. `R6.AC11` WHEN a valid policy update narrows the installation ceiling, the system SHALL avoid rejecting that update because of existing Kubeseer resources.
12. `R6.AC12` WHEN the canonical policy is deleted, the system SHALL rely on the approved runtime policy-missing behavior for existing Kubeseer resources.
13. `R6.AC13` WHEN equivalent policy and source inputs are submitted, the system SHALL produce the same policy-compliance admission outcome.

### R7 Actionable And Sanitized Admission Responses

**User Story:** As a Kubeseer author, I want precise Kubernetes-native admission errors, so that I can correct every detectable defect without seeing unrelated or sensitive cluster data.

#### Acceptance Criteria

1. `R7.AC1` IF any covered validation check fails, THEN the system SHALL deny persistence of the complete proposed object.
2. `R7.AC2` WHEN a request contains multiple independently detectable configuration defects, the system SHALL report every such defect in one admission response.
3. `R7.AC3` WHEN multiple defects are reported, the system SHALL order them deterministically by field path and stable reason.
4. `R7.AC4` WHEN a configuration defect is reported, the system SHALL identify the submitted field path responsible for the defect.
5. `R7.AC5` WHEN a configuration defect is reported, the system SHALL include a concise corrective message.
6. `R7.AC6` WHEN admission rejects malformed configuration, the system SHALL classify the response as a Kubernetes invalid-object error.
7. `R7.AC7` WHEN admission rejects a target outside the installation policy, the system SHALL classify the response as a Kubernetes forbidden error.
8. `R7.AC8` WHEN admission cannot complete a dynamic check because a dependency is transiently unavailable, the system SHALL classify the response as retryable.
9. `R7.AC9` WHEN admission reports a discovery or policy failure, the system SHALL exclude observed resource bodies from the response.
10. `R7.AC10` WHEN admission reports a discovery or policy failure, the system SHALL exclude Secret data from the response.
11. `R7.AC11` WHEN admission reports a discovery or policy failure, the system SHALL exclude raw downstream error bodies from the response.
12. `R7.AC12` WHEN equivalent invalid requests are evaluated against equivalent cluster state, the system SHALL return semantically equivalent reasons and field paths.
13. `R7.AC13` WHEN any admission defect is reported, the system SHALL include one stable machine-readable reason for that defect.
14. `R7.AC14` WHEN admission rejects a request because the canonical policy is missing or invalid, the system SHALL classify the response as a Kubernetes forbidden error.

### R8 Runtime Revalidation And Drift Safety

**User Story:** As a security-conscious operator, I want admission and runtime checks to compose safely, so that cluster drift or a stale admission result can never authorize a read.

#### Acceptance Criteria

1. `R8.AC1` WHEN runtime processing begins for an admitted Kubeseer, the system SHALL repeat the approved semantic planning checks against the persisted generation.
2. `R8.AC2` WHEN runtime resource access is prepared, the system SHALL resolve current discovery information independently of the admission result.
3. `R8.AC3` WHEN runtime resource access is prepared, the system SHALL require a current exact-target authorization capability independently of the admission result.
4. `R8.AC4` IF a resource type becomes unavailable after admission, THEN the system SHALL apply the approved source-scoped discovery failure behavior.
5. `R8.AC5` IF a policy change denies a previously admitted target, THEN the system SHALL apply the approved policy invalidation behavior before another resource-instance read.
6. `R8.AC6` IF a persisted Kubeseer predates the admission webhook, THEN the system SHALL subject it to the same runtime planning checks as a newly admitted resource.
7. `R8.AC7` IF a persisted Kubeseer predates the admission webhook, THEN the system SHALL require the same exact-target runtime authorization as a newly admitted resource.
8. `R8.AC8` WHEN admission implementation reuses a runtime parser, planner, or policy evaluator, the system SHALL preserve that component's approved semantics.
9. `R8.AC9` IF admission and runtime observe different cluster state, THEN the system SHALL treat the runtime decision as authoritative for resource access.

## Non-Functional Requirements

- `NFR1` **Security:** Admission SHALL fail closed for covered writes without replacing current exact-target runtime authorization; `R1.AC10`, `R1.AC11`, `R6.AC4` through `R6.AC8`, and `R8` provide behavioral coverage.
- `NFR2` **Kubernetes conformance:** Validation SHALL use structural CRD schemas, Kubernetes discovery, native selector syntax, dry-run semantics, and Kubernetes-native error classes; `R1.AC1` through `R1.AC9`, `R3.AC5` through `R3.AC7`, `R5`, and `R7.AC6` through `R7.AC8` provide behavioral coverage.
- `NFR3` **Determinism:** Equivalent configuration and cluster-validation state SHALL produce stable acceptance decisions, error ordering, reasons, and field paths; `R5.AC9`, `R6.AC13`, `R7.AC3`, and `R7.AC12` provide behavioral coverage.
- `NFR4` **Boundedness:** Admission work SHALL remain bounded by explicit configuration cardinality and serialized-size budgets before dynamic checks begin; `R2.AC6` through `R2.AC24` provide behavioral coverage.
- `NFR5` **Confidentiality:** Admission diagnostics SHALL expose submitted configuration locations without exposing observed resource contents, Secret data, or raw downstream error bodies; `R7.AC4`, `R7.AC9`, `R7.AC10`, and `R7.AC11` provide behavioral coverage.
- `NFR6` **Availability:** Transient discovery or policy dependencies SHALL fail covered writes with a retryable response while status updates and deletion remain available; `R1.AC6`, `R1.AC7`, `R5.AC8`, `R6.AC7`, and `R7.AC8` provide behavioral coverage.
- `NFR7` **Testability:** Structural, semantic, discovery-dependent, policy-dependent, dry-run, failure-policy, and runtime-revalidation behavior SHALL be verifiable through genuine cross-module integration and envtest layers; all acceptance criteria expose observable boundaries for those proofs.
- `NFR8` **Compatibility:** Admission SHALL extend `kubeseer.io/v1alpha1` without changing approved serialized field meanings or introducing a second API version; `R1`, `R2.AC1` through `R2.AC5`, and `R8.AC6` provide behavioral coverage.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `kubeseer-api-foundation`, `integration-testing-foundation`, `resource-discovery`, `installation-access-policy`, `resource-selection`, `field-extraction`, `typed-output-model`, `value-operators`, `cross-namespace-aggregation`, and `authorization-enforcement` contracts.
- `C2` The public APIs remain `kubeseer.io/v1alpha1`; this feature must not introduce a second API version or duplicate public configuration fields.
- `C3` Static validation must be expressed in generated structural OpenAPI wherever the constraint does not require parsing, sibling references, discovery, or current policy state.
- `C4` Semantic admission must reuse or faithfully adapt the approved selector parsers, JSONPath planner, typed conversion planner, operator planner, aggregate planner, policy compiler, and discovery contracts rather than define competing semantics.
- `C5` Admission policy checks use only the canonical administrator-owned installation policy and never infer permission from the operator ServiceAccount or Kubeseer author's RBAC.
- `C6` Runtime authorization and freshness checks remain mandatory before resource-instance I/O regardless of admission outcome.
- `C7` Production certificate issuance, certificate rotation, Service wiring, and installation lifecycle belong to `packaging-and-installation`; this feature must still expose an installable webhook contract and local test setup.
- `C8` Product-wide matched-resource, output-size, controller-concurrency, evaluation-time, cache, and memory limits belong to `performance-and-limits`; the configuration-shape budgets in `R2` only bound admission input.
- `C9` Automated proof follows the repository testing constitution: genuine cross-module integration is the minimum layer, envtest proves API-server and webhook behavior, and no dedicated package-local unit-test layer is added.
- `C10` The webhook must support the Kubernetes AdmissionReview `v1` contract used by every project-supported Kubernetes minor.

## Out Of Scope

- Mutating admission, defaulting webhooks, or rewriting submitted Kubeseer configuration.
- User impersonation, SubjectAccessReview, creator-RBAC checks, tenant-specific policy, or grants derived from admission.
- Treating successful admission as durable authorization, discovery cache authority, or proof that runtime Kubernetes RBAC permits a read.
- Rejecting a valid policy restriction because already persisted Kubeseer resources would become unauthorized.
- Validating the schemas or contents of observed built-in resources and Custom Resources beyond resolving their served identity and scope.
- Requiring an explicitly referenced namespace to exist at admission time.
- Proving that a JSONPath resolves against every future resource instance or that a syntactically valid field selector is supported by the resolved resource endpoint.
- Runtime matched-resource limits, total result or status-size limits, controller concurrency, evaluation timeouts, cache strategy, memory budgets, or large-cluster behavior.
- Production webhook certificate provisioning, rotation, packaging, upgrades, or uninstall behavior.
- Metrics, tracing, Kubernetes Events, durable audit history, external audit sinks, dashboards, or alerts.
- CEL as an extraction or operator expression language, arbitrary scripts, joins, cross-source references, or computed fields.
