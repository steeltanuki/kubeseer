---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-01T12:30:49Z
last_modified: 2026-09-01T12:30:49Z
approved_fingerprint: sha256:5be99c6777610b01231284cf5e9a7501d6f89baa38f744eef3b84bb4a787dad3
---

# Requirements Document

## Introduction

Kubeseer has approved contracts for its public API, installation package,
observation pipeline, security boundaries, lifecycle, diagnostics, and runtime
limits. Those contracts are proven at focused higher layers, but the project
does not yet certify that the packaged manager composes them correctly inside
a real Kubernetes cluster.

This feature establishes executable cluster-level certification for the
current release scope. The suite builds the production image from the tested
checkout, installs the canonical Helm chart into a disposable project-owned
kind cluster using Podman, creates real Kubernetes fixtures, and observes the
public API, status, admission, metrics, Events, and lifecycle outcomes. It
covers every minimum scenario listed for `end-to-end-scenarios` in
`SPECIFICATIONS.md`, plus the release-scope packaging, admission,
observability, operator, and limit boundaries needed to make the result a
non-vacuous product certification.

This feature verifies approved behavior without redefining it. Upstream
feature specifications remain authoritative for selectors, extraction,
typing, operators, aggregation, authorization, status, observability, limits,
and packaging semantics.

<!-- assumed: the certified release scope is the sixteen approved and fresh features from kubeseer-api-foundation through packaging-and-installation; local-development-environment remains downstream of this feature (source: SPECIFICATIONS.md, .walden/constitution.md, and current Walden status) -->
<!-- assumed: end-to-end execution uses the canonical Helm chart and a production image built from the current checkout rather than an alternate test-only manager deployment (source: packaging-and-installation Requirements) -->
<!-- assumed: the default end-to-end entry point creates its own kind cluster with Podman, uses an explicit owned kubeconfig, ignores ambient kubeconfig and cloud credentials, and never falls back to an external cluster (source: integration-testing-foundation Requirements and .walden/constitution.md) -->
<!-- assumed: the default end-to-end run targets one centrally pinned version from the supported Kubernetes matrix; full matrix certification remains with the approved API and packaging compatibility entry points -->
<!-- assumed: one supported webhook certificate profile is sufficient for product scenarios because packaging-and-installation independently certifies both certManager and externalSecret lifecycle profiles -->

## Requirements

### R1 Certified Release Scope And Scenario Traceability

**User Story:** As a release maintainer, I want every cluster-level claim tied
to an executable scenario, so that an end-to-end pass has an explicit and
reviewable meaning.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL declare the exact upstream features included in the certified release scope.
2. `R1.AC2` The system SHALL map every in-scope feature to at least one executable end-to-end scenario or one explicitly identified prerequisite certification.
3. `R1.AC3` The system SHALL map every minimum scenario from `SPECIFICATIONS.md` section `end-to-end-scenarios` to an executable end-to-end scenario.
4. `R1.AC4` The system SHALL assign a stable identifier to every required end-to-end scenario.
5. `R1.AC5` WHEN a maintainer runs `make e2e`, the system SHALL execute the complete required end-to-end scenario set.
6. `R1.AC6` IF any in-scope feature has no declared certification mapping, THEN the system SHALL fail end-to-end verification.
7. `R1.AC7` IF any required scenario is skipped, THEN the system SHALL fail end-to-end verification.
8. `R1.AC8` IF the selected end-to-end test pattern matches zero tests, THEN the system SHALL fail end-to-end verification.
9. `R1.AC9` WHEN an end-to-end run completes, the system SHALL report the outcome of every required scenario by stable identifier.
10. `R1.AC10` WHEN an end-to-end run starts, the system SHALL report the tested source revision.
11. `R1.AC11` WHEN an end-to-end run starts, the system SHALL report the selected Kubernetes version.
12. `R1.AC12` WHEN an end-to-end run starts, the system SHALL report the selected package identity.

### R2 Isolated Kind-On-Podman Execution

**User Story:** As a contributor, I want end-to-end tests to own an isolated
local cluster, so that they cannot accidentally inspect or damage unrelated
Kubernetes or Podman state.

#### Acceptance Criteria

1. `R2.AC1` WHEN a contributor runs the default end-to-end entry point, the system SHALL target a project-owned kind cluster.
2. `R2.AC2` WHEN the end-to-end cluster is created, the system SHALL use Podman as the kind provider.
3. `R2.AC3` WHEN the end-to-end cluster is created, the system SHALL select a centrally declared kind node image for a supported Kubernetes version.
4. `R2.AC4` WHEN the end-to-end cluster is created, the system SHALL write its kubeconfig under a run-owned temporary directory.
5. `R2.AC5` WHEN any end-to-end Kubernetes command runs, the system SHALL pass the run-owned kubeconfig explicitly.
6. `R2.AC6` IF an ambient kubeconfig is present, THEN the system SHALL ignore it for end-to-end execution.
7. `R2.AC7` IF ambient cloud credentials are present, THEN the system SHALL ignore them for end-to-end execution.
8. `R2.AC8` The system SHALL never select an externally managed Kubernetes cluster as an automatic end-to-end fallback.
9. `R2.AC9` IF a required executable or local provider is unavailable, THEN the system SHALL fail before creating the end-to-end cluster.
10. `R2.AC10` IF a prerequisite check fails, THEN the system SHALL identify the failed prerequisite in the diagnostic.
11. `R2.AC11` WHEN an end-to-end run terminates after cluster ownership is established, the system SHALL request deletion of only the owned kind cluster.
12. `R2.AC12` WHEN owned cluster deletion succeeds, the system SHALL remove the run-owned temporary directory.
13. `R2.AC13` IF owned cluster deletion fails, THEN the system SHALL retain the owned kubeconfig path in the diagnostic.
14. `R2.AC14` The system SHALL NOT include unrelated Podman resources in end-to-end cleanup.
15. `R2.AC15` The system SHALL NOT include unrelated Podman networks in end-to-end cleanup.

### R3 Production Image, Package, And Readiness

**User Story:** As a release maintainer, I want scenarios to exercise the
actual distributable manager, so that test-only deployment paths cannot mask a
packaging or startup defect.

#### Acceptance Criteria

1. `R3.AC1` WHEN the end-to-end run prepares Kubeseer, the system SHALL build the production manager image from the tested checkout.
2. `R3.AC2` WHEN the production image is built, the system SHALL assign it an immutable run-specific tag.
3. `R3.AC3` WHEN the production image is available, the system SHALL load it into every end-to-end kind node without requiring an external registry.
4. `R3.AC4` WHEN Kubeseer is installed, the system SHALL use the canonical Helm chart.
5. `R3.AC5` WHEN Kubeseer is installed, the system SHALL use an explicit release namespace.
6. `R3.AC6` WHEN Kubeseer is installed, the system SHALL select the run-built image through supported chart values.
7. `R3.AC7` WHEN Kubeseer is installed, the system SHALL provision one supported validating-webhook certificate profile.
8. `R3.AC8` WHEN installation applies the Kubeseer CRDs, the system SHALL wait for both CRDs to become Established within a bounded deadline.
9. `R3.AC9` WHEN installation creates the manager Deployment, the system SHALL wait for it to become Available within a bounded deadline.
10. `R3.AC10` WHEN installation registers the validating webhooks, the system SHALL verify that their CA bundles are non-empty.
11. `R3.AC11` WHEN installation registers the validating webhooks, the system SHALL verify that their endpoints are reachable.
12. `R3.AC12` WHEN installation bootstraps the canonical access policy, the system SHALL observe the `installation-access-ceiling` singleton persisted.
13. `R3.AC13` WHEN installation completes, the system SHALL observe the manager readiness endpoint as ready.
14. `R3.AC14` IF image build, image loading, installation, or readiness fails, THEN the system SHALL execute no product scenario.
15. `R3.AC15` WHEN an invalid covered Kubeseer manifest is submitted to the installed API, the system SHALL observe rejection by the installed validating webhook.

### R4 Resource Discovery And Selection Scenarios

**User Story:** As a Kubeseer author, I want certification against built-in and
custom Kubernetes resources, so that discovery and selection are proven across
real API-server boundaries.

#### Acceptance Criteria

1. `R4.AC1` WHEN a Kubeseer selects one Deployment in its own namespace by exact name, the system SHALL publish that Deployment as the matched resource.
2. `R4.AC2` WHEN a Kubeseer selects Pods from two authorized namespaces, the system SHALL publish matches from both namespaces.
3. `R4.AC3` WHEN equal resource names exist in different selected namespaces, the system SHALL preserve unambiguous provenance for each match.
4. `R4.AC4` WHEN an installed fixture CRD exposes one matching Custom Resource, the system SHALL publish that Custom Resource as the matched resource.
5. `R4.AC5` WHEN a source declares a label selector, the system SHALL publish only resources satisfying that selector.
6. `R4.AC6` WHEN equivalent selected resources are returned in a different API list order, the system SHALL preserve deterministic result ordering.
7. `R4.AC7` IF a declared resource type is absent from discovery, THEN the system SHALL publish the approved stable source failure for that missing type.
8. `R4.AC8` WHEN one source has a missing resource type, the system SHALL preserve successful sibling-source results.

### R5 Extraction, Typing, And Operator Scenarios

**User Story:** As a Kubeseer author, I want values processed through the
complete declared pipeline, so that status proves native extraction, logical
typing, and operators as one product behavior.

#### Acceptance Criteria

1. `R5.AC1` WHEN a field declares a supported scalar JSONPath, the system SHALL publish the extracted scalar value.
2. `R5.AC2` WHEN a field declares a supported list-producing JSONPath, the system SHALL publish the complete ordered list outcome.
3. `R5.AC3` WHEN an extracted scalar declares a supported logical type, the system SHALL publish the value using that declared type.
4. `R5.AC4` WHEN extracted list elements declare a supported logical type, the system SHALL publish every accepted element using that declared type.
5. `R5.AC5` WHEN a compatible predicate operator rejects one resource, the system SHALL omit that resource from the accepted result set.
6. `R5.AC6` WHEN a compatible transformation operator changes one value, the system SHALL publish the transformed typed value.
7. `R5.AC7` IF one field value cannot be converted to its declared type, THEN the system SHALL publish the approved sanitized field failure.
8. `R5.AC8` WHEN one field conversion fails, the system SHALL preserve successful sibling fields for that resource.
9. `R5.AC9` WHEN equivalent source objects and declarations are processed in separate reconciliations, the system SHALL publish semantically equivalent typed results.

### R6 Numeric Aggregation Across Namespaces

**User Story:** As a Kubeseer author, I want a real cross-namespace numeric
aggregation scenario, so that selection, extraction, typing, contribution
processing, and aggregation are certified together.

#### Acceptance Criteria

1. `R6.AC1` WHEN authorized resources in multiple namespaces contribute numeric values to a declared aggregation, the system SHALL publish the approved exact aggregate value.
2. `R6.AC2` WHEN the aggregate declares grouping, the system SHALL publish one outcome per expected typed group key.
3. `R6.AC3` WHEN aggregate inputs arrive in a different resource order, the system SHALL preserve deterministic aggregate ordering.
4. `R6.AC4` WHEN aggregate contributor provenance is requested, the system SHALL publish provenance for every accepted contribution.
5. `R6.AC5` IF one resource contribution fails while another contribution remains valid, THEN the system SHALL publish the approved degraded aggregate outcome.
6. `R6.AC6` WHEN one resource contribution fails, the system SHALL preserve every valid sibling contribution allowed by the aggregate contract.

### R7 Authorization, Degradation, And Limit Scenarios

**User Story:** As a platform administrator, I want cluster-level negative
scenarios to remain fail-closed, so that successful demonstrations cannot hide
authorization or boundedness regressions.

#### Acceptance Criteria

1. `R7.AC1` IF a Kubeseer requests a namespace outside the active installation policy, THEN the system SHALL publish the approved namespace-denied outcome.
2. `R7.AC2` WHILE a namespace request is denied, the system SHALL publish no observed value from that namespace.
3. `R7.AC3` IF a Kubeseer requests a kind outside the active installation policy, THEN the system SHALL publish the approved resource-denied outcome.
4. `R7.AC4` WHILE a kind request is denied, the system SHALL publish no observed value for that kind.
5. `R7.AC5` WHEN one source succeeds while one sibling source fails, the system SHALL publish a partially degraded result.
6. `R7.AC6` WHEN a partially degraded result is published, the system SHALL set the canonical `Degraded` condition to `True`.
7. `R7.AC7` WHEN a partially degraded result is published, the system SHALL preserve the successful sibling source result.
8. `R7.AC8` IF one source exceeds the configured matched-resource ceiling, THEN the system SHALL publish the approved stable limit failure for that source.
9. `R7.AC9` WHILE a source exceeds its matched-resource ceiling, the system SHALL publish no truncated value set for that source.
10. `R7.AC10` IF one candidate status exceeds the configured publication ceiling, THEN the system SHALL publish the approved stable status-limit outcome.
11. `R7.AC11` WHILE a candidate status exceeds its publication ceiling, the system SHALL publish no oversized candidate status.
12. `R7.AC12` WHEN an authorization or limit failure is published, the system SHALL exclude observed values from its diagnostic.

### R8 Reconciliation And Lifecycle Scenarios

**User Story:** As a platform operator, I want the installed controller to
react correctly over time, so that certification covers reconciliation rather
than only one initial snapshot.

#### Acceptance Criteria

1. `R8.AC1` WHEN an observed source field changes, the system SHALL publish the corresponding new semantic result within a bounded deadline.
2. `R8.AC2` WHEN an observed source update leaves the semantic result unchanged, the system SHALL issue no Kubeseer status write for that update.
3. `R8.AC3` WHEN a matched source resource is deleted, the system SHALL remove that resource contribution from the published result within a bounded deadline.
4. `R8.AC4` WHEN the active access policy is narrowed to revoke an existing source target, the system SHALL remove the previously published protected value within a bounded deadline.
5. `R8.AC5` WHEN the active access policy is narrowed, the system SHALL publish the approved authorization condition for the revoked target.
6. `R8.AC6` WHEN the manager Pod is replaced, the system SHALL become ready again within a bounded deadline.
7. `R8.AC7` WHEN the restarted manager reconciles an existing Kubeseer, the system SHALL reproduce its current semantic result.
8. `R8.AC8` WHEN a Kubeseer resource is deleted, the system SHALL remove its process-local routing state without requiring a finalizer.
9. `R8.AC9` WHEN a Kubeseer resource is deleted, the system SHALL leave every observed source resource unchanged.
10. `R8.AC10` WHILE two Kubeseer instances select overlapping resources, the system SHALL publish an independent result for each instance.
11. `R8.AC11` IF one of two overlapping Kubeseer instances fails, THEN the system SHALL continue reconciling the unaffected instance.

### R9 Public Status And Observability Evidence

**User Story:** As an operator diagnosing a certified run, I want status and
signals to agree on the observed outcome, so that end-to-end failures are
actionable without exposing observed data.

The complete canonical condition set is `Accepted`, `Authorized`,
`SourcesResolved`, `Ready`, and `Degraded`.

#### Acceptance Criteria

1. `R9.AC1` WHEN a successful scenario reaches its terminal state, the system SHALL publish the complete canonical condition set for the current generation.
2. `R9.AC2` WHEN a terminal result is published, the system SHALL publish the matching deterministic result summary.
3. `R9.AC3` WHEN a terminal result is published, the system SHALL publish the matching semantic result hash.
4. `R9.AC4` WHEN a scenario reaches a semantic outcome, the system SHALL observe the corresponding bounded-cardinality Prometheus metric change.
5. `R9.AC5` WHEN a status write succeeds, the system SHALL observe the approved `StatusUpdated` Kubernetes Event.
6. `R9.AC6` WHEN authorization is evaluated, the system SHALL observe a sanitized authorization audit record in manager logs.
7. `R9.AC7` WHEN one reconciliation begins and terminates, the system SHALL correlate its structured log records by the approved attempt identity.
8. `R9.AC8` The system SHALL exclude Secret payloads from collected end-to-end diagnostics.
9. `R9.AC9` The system SHALL exclude extracted values from collected manager logs.
10. `R9.AC10` The system SHALL exclude typed values from collected manager logs.
11. `R9.AC11` IF observability emission fails internally, THEN the system SHALL preserve the reconciliation outcome required by the corresponding product scenario.

### R10 Deterministic Fixtures, Waiting, And Failure Reporting

**User Story:** As a contributor, I want repeatable scenarios with bounded
waits and useful diagnostics, so that failures can be reproduced locally and
in continuous integration.

#### Acceptance Criteria

1. `R10.AC1` The system SHALL keep all end-to-end fixtures under version control.
2. `R10.AC2` The system SHALL assign a run-isolated name to every mutable fixture.
3. `R10.AC3` WHEN a scenario waits for asynchronous Kubernetes behavior, the system SHALL use bounded polling of an observable condition.
4. `R10.AC4` The system SHALL NOT use an arbitrary sleep as the primary readiness mechanism.
5. `R10.AC5` WHEN equivalent end-to-end inputs run against an equivalent clean cluster, the system SHALL produce equivalent semantic assertions.
6. `R10.AC6` IF a bounded wait expires, THEN the system SHALL identify the stable scenario identifier in the failure diagnostic.
7. `R10.AC7` IF a product assertion fails, THEN the system SHALL identify the stable scenario identifier in the failure diagnostic.
8. `R10.AC8` IF a product assertion fails, THEN the system SHALL collect the relevant sanitized Kubeseer status.
9. `R10.AC9` IF a product assertion fails, THEN the system SHALL collect the relevant sanitized manager logs.
10. `R10.AC10` IF a product assertion fails, THEN the system SHALL collect the relevant sanitized Kubernetes Events.
11. `R10.AC11` WHEN continuous integration invokes `make e2e`, the system SHALL preserve the same cluster ownership semantics as local execution.
12. `R10.AC12` WHEN the end-to-end entry point completes successfully, the system SHALL report `TEST_LAYER=end-to-end STATUS=passed`.
13. `R10.AC13` IF a bounded wait expires, THEN the system SHALL identify the awaited condition in the failure diagnostic.
14. `R10.AC14` WHEN continuous integration invokes `make e2e`, the system SHALL preserve the same scenario execution semantics as local execution.
15. `R10.AC15` WHEN an end-to-end run terminates, the system SHALL leave the repository worktree unchanged.

## Non-Functional Requirements

- `NFR1` **Security:** End-to-end execution SHALL preserve logical-policy and Kubernetes-RBAC separation, use only explicit owned cluster credentials, and prove fail-closed denial without exposing protected values; `R2`, `R7`, and `R9.AC8` through `R9.AC10` provide behavioral coverage.
- `NFR2` **Isolation:** End-to-end execution SHALL affect only its project-owned kind cluster, run-owned temporary files, and namespaced fixtures; `R2` and `R10.AC2` provide behavioral coverage.
- `NFR3` **Determinism:** Equivalent source, package, cluster-version, policy, fixture, and scenario inputs SHALL produce equivalent semantic assertions and stable scenario reporting; `R1`, `R4.AC6`, `R5.AC9`, `R6.AC3`, and `R10.AC5` provide behavioral coverage.
- `NFR4` **Reliability:** Installation, reconciliation, restart, policy invalidation, deletion, bounded waiting, and cleanup SHALL expose deterministic success or actionable failure; `R2`, `R3`, `R8`, and `R10` provide behavioral coverage.
- `NFR5` **Traceability:** Every certified feature and every mandatory product scenario SHALL map to executable evidence with stable identifiers; `R1` provides behavioral coverage.
- `NFR6` **Compatibility:** The suite SHALL exercise the production package on a centrally selected version from the approved Kubernetes compatibility matrix without claiming untested versions; `R1.AC11`, `R2.AC3`, and `R3` provide behavioral coverage.
- `NFR7` **Non-vacuous testability:** Required scenarios SHALL run through the real API server, installed manager, webhook, and status subresource rather than substituting in-process doubles; `R1.AC5` through `R1.AC8`, `R3`, and `R4` through `R9` provide behavioral coverage.
- `NFR8` **Bounded execution:** Every asynchronous operation SHALL have an explicit deadline and produce enough sanitized context to diagnose expiration; `R3.AC8`, `R3.AC9`, `R8`, and `R10.AC3` through `R10.AC10` provide behavioral coverage.
- `NFR9` **Reproducibility:** The same repository entry point SHALL be usable locally and in continuous integration with centrally declared tool and image versions; `R1.AC10` through `R1.AC12`, `R2.AC3`, `R10.AC5`, and `R10.AC11` provide behavioral coverage.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `kubeseer-api-foundation`, `integration-testing-foundation`, `resource-discovery`, `installation-access-policy`, `resource-selection`, `field-extraction`, `typed-output-model`, `reconciliation-runtime`, `status-and-conditions`, `authorization-enforcement`, `value-operators`, `cross-namespace-aggregation`, `admission-validation`, `observability`, `performance-and-limits`, and `packaging-and-installation` contracts.
- `C2` Every upstream specification remains authoritative for public fields, defaults, selectors, JSONPath, logical types, operator semantics, aggregation semantics, conditions, reason codes, diagnostics, limits, and lifecycle behavior.
- `C3` The default end-to-end environment is a project-owned kind cluster using Podman; no Docker, cloud Kubernetes service, ambient cluster, or existing-cluster mode is required.
- `C4` The end-to-end deployment uses the canonical Helm chart under `charts/kubeseer` and a production manager image built from the tested checkout.
- `C5` The supported Kubernetes matrix remains centrally declared as `1.35.6` and `1.36.2` until an approved compatibility change replaces it; the default end-to-end run selects one pinned member of that matrix.
- `C6` Package compatibility verification remains responsible for certifying every supported Kubernetes minor and both supported webhook certificate profiles.
- `C7` The end-to-end suite may download or consume pinned Go modules, tools, Kubernetes images, and package prerequisites during first-time setup.
- `C8` The suite must not replace cluster-level behavior with envtest, fake clients, mock API servers, in-process webhook servers, or direct calls into production packages.
- `C9` The approved performance profile never silently truncates results; oversized scenarios must assert the corresponding fail-closed upstream limit outcome.
- `C10` Only one active Kubeseer Helm release is supported in the disposable end-to-end cluster.
- `C11` Automated proof follows the repository testing constitution: the stable entry point is `make e2e`, zero-match execution fails, and no dedicated package-local unit-test layer is added.
- `C12` End-to-end execution must not leave generated artifacts or mutable test state in the repository worktree.
- `C13` `local-development-environment` owns the reusable contributor setup workflow, bundled examples, interactive diagnostics, and explicit local-up/local-down commands.
- `C14` Kubeseer-authored scripts, fixtures, tests, and documentation remain under Apache License 2.0 with Alessandro Rontani as the default copyright holder.

## Out Of Scope

- Redefining any upstream API, authorization, extraction, typing, operator, aggregation, condition, observability, performance, admission, or packaging semantic.
- Certifying every acceptance criterion from every upstream feature at cluster level when focused approved evidence already proves behavior that does not require a full cluster.
- Running the product scenario suite across every supported Kubernetes version or every Helm values combination; those matrices remain with API and packaging compatibility verification.
- Supporting an externally managed cluster, ambient kubeconfig, cloud provider, Docker provider, or automatic existing-cluster fallback.
- Provisioning a persistent local development environment, tutorial workflow, demo application, or bundled example catalog.
- Publishing images, charts, test reports, or artifacts to an external registry or service.
- Load, soak, chaos, disaster-recovery, backup, restore, multi-cluster, upgrade-skew, or high-availability certification.
- Destructive CRD purge or validation of administrator confirmation UX for purge; packaging verification owns that lifecycle boundary.
- Enabling or requiring an external tracing backend, metrics collector, log store, dashboard, or alerting system.
