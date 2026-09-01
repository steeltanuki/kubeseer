---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-01T18:25:07Z
last_modified: 2026-09-01T18:25:07Z
approved_fingerprint: sha256:2e3cf860c2525787b1c145fa464358465ef84b327e619823b833c7b7f6b0e732
---

# Requirements Document

## Introduction

Kubeseer needs one reproducible contributor workflow that starts from a host
without an existing Kubernetes cluster and leaves a ready, inspectable local
installation. The workflow owns a persistent kind cluster backed by Podman,
builds the current checkout, installs the canonical Helm package with its
normal authorization boundary, and exposes executable examples without using
an external image registry or an ambient kubeconfig.

This feature owns the local-environment user experience, the bundled example
catalog, interactive diagnostics, and safe teardown. It consumes the approved
packaging and end-to-end contracts without redefining product behavior. The
long-lived local cluster remains separate from the run-unique disposable
cluster created by `make e2e`.

<!-- assumed: the initial supported host profile is Linux/amd64 because the current end-to-end evidence is recorded on linux/amd64 and no approved host evidence exists for macOS Podman machine, Windows, WSL, or another architecture (source: .walden/evidence/end-to-end-scenarios.json and the current build defaults in Makefile) -->
<!-- assumed: the stable local cluster identity is `kubeseer-local`, with context `kind-kubeseer-local`, because an exact fixed project-owned identity makes repeated setup, inspection, and narrowly scoped cleanup externally verifiable (source: .walden/constitution.md and SPECIFICATIONS.md) -->
<!-- assumed: `make local-up` installs the environment but does not deploy the example catalog; examples remain an explicit follow-up step so contributors can distinguish platform readiness from demonstration state (source: the setup and example stages in SPECIFICATIONS.md) -->

## Requirements

### R1 Stable local workflow entry points

**User Story:** As a contributor, I want memorable repository commands for the
complete local lifecycle, so that I can use the environment without assembling
provider, image, Helm, and Kubernetes commands manually.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL expose `make local-check` as the prerequisite-validation entry point.
2. `R1.AC2` The system SHALL expose `make local-up` as the environment creation and convergence entry point.
3. `R1.AC3` The system SHALL expose `make local-status` as the read-only environment inspection entry point.
4. `R1.AC4` The system SHALL expose `make local-examples` as the bundled-example deployment entry point.
5. `R1.AC5` The system SHALL expose `make local-verify` as the installed-environment and example verification entry point.
6. `R1.AC6` The system SHALL expose `make local-diagnostics` as the sanitized diagnostic-collection entry point.
7. `R1.AC7` The system SHALL expose `make local-examples-down` as the bundled-example cleanup entry point.
8. `R1.AC8` The system SHALL expose `make local-down` as the project-owned environment deletion entry point.
9. `R1.AC9` WHEN a local workflow entry point completes successfully, the system SHALL print a stable `LOCAL_ENVIRONMENT=<phase> STATUS=passed` terminal marker.
10. `R1.AC10` IF a local workflow entry point receives an unsupported argument, THEN the system SHALL exit non-zero with actionable usage text before mutating cluster state.
11. `R1.AC11` WHEN a local workflow entry point invokes another repository capability, the system SHALL use its canonical public entry point rather than a duplicated implementation.

### R2 Prerequisite and compatibility validation

**User Story:** As a contributor, I want setup failures detected before cluster
creation, so that missing or incompatible tools do not leave ambiguous local
state.

#### Acceptance Criteria

1. `R2.AC1` WHEN a contributor runs `make local-check`, the system SHALL verify that the host operating system and architecture match the supported local profile.
2. `R2.AC2` WHEN a contributor runs `make local-check`, the system SHALL verify the availability of Go, Git, Make, Podman, kind, kubectl, Helm, and the configured HTTP client.
3. `R2.AC3` WHEN a contributor runs `make local-check`, the system SHALL report the detected version of every required tool without reporting environment credentials.
4. `R2.AC4` WHEN a contributor runs `make local-check`, the system SHALL verify that rootless Podman is operational.
5. `R2.AC5` WHEN a contributor runs `make local-check`, the system SHALL verify that kind can select Podman explicitly as its provider.
6. `R2.AC6` WHEN a contributor selects a Kubernetes version, the system SHALL accept only a centrally declared member of the supported Kubernetes compatibility matrix.
7. `R2.AC7` WHEN a contributor selects a supported Kubernetes version, the system SHALL resolve it to one centrally declared kind node image.
8. `R2.AC8` IF a required command is absent, THEN the system SHALL identify the missing command before creating or changing the local cluster.
9. `R2.AC9` IF a required tool version is unsupported, THEN the system SHALL identify the detected and supported versions before creating or changing the local cluster.
10. `R2.AC10` IF Podman is unavailable or unusable, THEN the system SHALL preserve every existing Kubernetes and Podman resource.
11. `R2.AC11` IF the host profile is unsupported, THEN the system SHALL identify the supported host profile before creating or changing the local cluster.
12. `R2.AC12` IF a pinned first-time dependency cannot be downloaded or found in a local cache, THEN the system SHALL identify the dependency and its required version.

### R3 Owned cluster identity and persistent state

**User Story:** As a contributor, I want a predictable local cluster that is
isolated from my other Kubernetes environments, so that setup and cleanup are
safe to repeat.

#### Acceptance Criteria

1. `R3.AC1` WHEN a contributor runs `make local-up` without an existing owned cluster, the system SHALL create the exact kind cluster `kubeseer-local` with the Podman provider.
2. `R3.AC2` WHEN the owned cluster is created, the system SHALL select the centrally mapped kind node image for the configured Kubernetes version.
3. `R3.AC3` The system SHALL keep the local kubeconfig and ownership metadata in a project-owned state directory outside the repository worktree.
4. `R3.AC4` The system SHALL address the local API server only through the explicit context `kind-kubeseer-local` from the project-owned kubeconfig.
5. `R3.AC5` The system SHALL ignore ambient `KUBECONFIG`, `USE_EXISTING_CLUSTER`, and cloud-provider credential variables for every local cluster operation.
6. `R3.AC6` WHEN `make local-up` finds the matching owned cluster, the system SHALL converge that cluster instead of creating a second cluster.
7. `R3.AC7` WHEN `make local-up` finds an owned cluster created for another supported Kubernetes version, the system SHALL report the version mismatch and the explicit recovery command.
8. `R3.AC8` IF a cluster named `kubeseer-local` exists without matching ownership metadata, THEN the system SHALL refuse to mutate or delete that cluster.
9. `R3.AC9` IF new cluster creation fails before ownership is established, THEN the system SHALL remove only the partially created `kubeseer-local` cluster.
10. `R3.AC10` IF convergence of a previously owned cluster fails, THEN the system SHALL preserve the cluster for diagnosis.
11. `R3.AC11` WHEN a local cluster command terminates, the system SHALL leave the repository worktree unchanged.
12. `R3.AC12` WHEN local state is inspected, the system SHALL report the exact kubeconfig path, context, cluster name, Kubernetes version, and source identity without printing credential material.

### R4 Local image build and node loading

**User Story:** As a contributor, I want the current checkout running in the
local cluster without publishing an image, so that I can test local changes
quickly and reproducibly.

#### Acceptance Criteria

1. `R4.AC1` WHEN `make local-up` prepares Kubeseer, the system SHALL build the production Dockerfile with Podman.
2. `R4.AC2` WHEN the local image is built, the system SHALL assign an immutable tag bound to the current source revision and worktree identity.
3. `R4.AC3` WHEN the local image is built, the system SHALL record its immutable image identity in the project-owned local state.
4. `R4.AC4` WHEN the local image is ready, the system SHALL load that exact image into every node of the owned kind cluster without an external registry.
5. `R4.AC5` WHEN the Helm release is rendered, the system SHALL reference the exact locally loaded image tag.
6. `R4.AC6` WHEN the Helm release is rendered for the local image, the system SHALL use an image pull policy that does not require an external registry.
7. `R4.AC7` WHEN `make local-up` runs again with an equivalent source identity, the system SHALL avoid publishing a different logical image identity.
8. `R4.AC8` WHEN `make local-up` runs with a changed source identity, the system SHALL deploy an image built from the changed checkout.
9. `R4.AC9` IF the image build fails, THEN the system SHALL stop before changing the Kubeseer Helm release.
10. `R4.AC10` IF image loading fails for any kind node, THEN the system SHALL identify the affected node.

### R5 Canonical local installation and development policy

**User Story:** As a contributor, I want the local environment to install the
same package and authorization controls used in supported deployments, so that
examples do not rely on privileged shortcuts.

#### Acceptance Criteria

1. `R5.AC1` WHEN `make local-up` installs Kubeseer, the system SHALL use the canonical chart under `charts/kubeseer`.
2. `R5.AC2` WHEN `make local-up` installs Kubeseer, the system SHALL use the release name `kubeseer` in namespace `kubeseer-system`.
3. `R5.AC3` WHEN `make local-up` prepares certificate management, the system SHALL install the centrally pinned cert-manager release.
4. `R5.AC4` WHEN `make local-up` installs the chart, the system SHALL select the canonical `certManager` certificate mode.
5. `R5.AC5` The system SHALL keep the local Helm values profile under version control.
6. `R5.AC6` The system SHALL configure `installation-access-ceiling` with explicit namespaces required by the bundled examples.
7. `R5.AC7` The system SHALL configure `installation-access-ceiling` with explicit API groups and kinds required by the bundled examples.
8. `R5.AC8` The system SHALL keep the development policy deny-by-default for namespaces absent from the example allowlist.
9. `R5.AC9` The system SHALL keep the development policy deny-by-default for resource kinds absent from the example allowlist.
10. `R5.AC10` The system SHALL configure operator RBAC separately from the logical development policy.
11. `R5.AC11` The system SHALL exclude Secret reads from the development policy and observed-resource RBAC.
12. `R5.AC12` WHEN `make local-up` finds the owned Helm release, the system SHALL converge it through the canonical upgrade path.
13. `R5.AC13` IF cert-manager installation fails, THEN the system SHALL stop before installing the Kubeseer Helm release.
14. `R5.AC14` IF the canonical chart rejects the development values profile, THEN the system SHALL preserve the previously working release state.

### R6 Readiness, status, and repeatability

**User Story:** As a contributor, I want setup to finish only when Kubeseer is
usable and repeated commands to have predictable results, so that local state
is easy to trust.

#### Acceptance Criteria

1. `R6.AC1` WHEN `make local-up` waits for cluster readiness, the system SHALL use a bounded readiness condition rather than an arbitrary sleep.
2. `R6.AC2` WHEN `make local-up` waits for package readiness, the system SHALL verify that both Kubeseer CRDs are Established.
3. `R6.AC3` WHEN `make local-up` waits for manager readiness, the system SHALL verify that the Kubeseer Deployment is Available.
4. `R6.AC4` WHEN `make local-up` waits for admission readiness, the system SHALL verify that the serving certificate is Ready.
5. `R6.AC5` WHEN `make local-up` waits for admission readiness, the system SHALL verify that every validating-webhook CA bundle is non-empty.
6. `R6.AC6` WHEN `make local-up` waits for admission readiness, the system SHALL verify that the webhook Service has a reachable endpoint.
7. `R6.AC7` WHEN `make local-up` waits for runtime readiness, the system SHALL verify the manager readiness endpoint.
8. `R6.AC8` WHEN `make local-up` waits for observability readiness, the system SHALL verify the metrics endpoint.
9. `R6.AC9` WHEN `make local-up` waits for policy readiness, the system SHALL verify the canonical `installation-access-ceiling` identity.
10. `R6.AC10` WHEN a contributor runs `make local-status`, the system SHALL report one bounded allowlisted projection of cluster, package, manager, policy, and example readiness.
11. `R6.AC11` WHEN repeated `make local-up` invocations use equivalent inputs, the system SHALL converge to one cluster, one Helm release, and one access-policy singleton.
12. `R6.AC12` IF a bounded readiness wait expires, THEN the system SHALL identify the exact awaited condition.
13. `R6.AC13` IF the API server identity does not match the owned kubeconfig and context, THEN the system SHALL stop before issuing a Kubernetes write.

### R7 Executable example catalog

**User Story:** As a user evaluating Kubeseer, I want small executable examples
with explicit expected outcomes, so that I can understand built-in resources,
typing, operators, aggregation, custom resources, denial, and degradation.

#### Acceptance Criteria

1. `R7.AC1` The system SHALL provide the stable examples `builtin-resource`, `typed-extraction`, `value-operator`, `cross-namespace-aggregation`, `custom-resource`, `authorization-denial`, and `partial-degradation` under the top-level `examples/` directory.
2. `R7.AC2` The system SHALL give every bundled example a concise purpose statement.
3. `R7.AC3` The system SHALL give every bundled example all required Kubernetes workload manifests.
4. `R7.AC4` The system SHALL give every bundled example one or more valid `Kubeseer` manifests.
5. `R7.AC5` The system SHALL give every bundled example an explicit expected public result or condition outcome.
6. `R7.AC6` The system SHALL give every bundled example commands to apply its resources with the project-owned kubeconfig and context.
7. `R7.AC7` The system SHALL give every bundled example commands to inspect its public Kubeseer status.
8. `R7.AC8` The system SHALL give every bundled example a non-vacuous automated verification command.
9. `R7.AC9` The system SHALL give every bundled example commands to remove only its owned resources.
10. `R7.AC10` The system SHALL label or namespace every example resource with a stable example identity.
11. `R7.AC11` WHEN a contributor runs `make local-examples`, the system SHALL apply every bundled example in a deterministic order.
12. `R7.AC12` WHEN a contributor runs `make local-examples` repeatedly, the system SHALL converge the example resources without creating duplicates.
13. `R7.AC13` WHEN the custom-resource example is applied, the system SHALL install its lightweight structural fixture CRD before its Custom Resources.
14. `R7.AC14` WHEN the authorization-denial example is verified, the system SHALL prove that its source remains outside the active logical policy.
15. `R7.AC15` WHEN the partial-degradation example is verified, the system SHALL prove that one successful source remains present in the public result.
16. `R7.AC16` WHEN the partial-degradation example is verified, the system SHALL prove that the unavailable source produces the approved degraded condition.
17. `R7.AC17` IF an example verification command matches no expected object, THEN the system SHALL exit non-zero.
18. `R7.AC18` IF an example assertion fails, THEN the system SHALL identify the stable example name and awaited public outcome.
19. `R7.AC19` WHEN a contributor runs `make local-examples-down`, the system SHALL remove only resources carrying a bundled-example identity.
20. `R7.AC20` WHEN example cleanup removes the custom-resource example, the system SHALL delete its fixture Custom Resources before its fixture CRD.

### R8 Local verification and end-to-end separation

**User Story:** As a maintainer, I want local verification to exercise the
installed environment while preserving the certified E2E boundary, so that
developer feedback is useful without weakening release evidence.

#### Acceptance Criteria

1. `R8.AC1` WHEN a contributor runs `make local-verify`, the system SHALL verify the owned cluster identity before any cluster assertion.
2. `R8.AC2` WHEN a contributor runs `make local-verify`, the system SHALL verify the canonical package readiness conditions.
3. `R8.AC3` WHEN a contributor runs `make local-verify`, the system SHALL execute every bundled example's automated assertion.
4. `R8.AC4` WHEN local verification reads Kubernetes state, the system SHALL use structured API output rather than presentation-oriented table text.
5. `R8.AC5` WHEN local verification waits for asynchronous state, the system SHALL use a bounded condition, watch, or polling predicate.
6. `R8.AC6` WHEN local verification succeeds, the system SHALL report every verified stable example name.
7. `R8.AC7` IF any local verification step fails, THEN the system SHALL exit non-zero.
8. `R8.AC8` WHEN continuous integration verifies the local workflow contract, the system SHALL use a reusable repository entry point with the same ownership and command semantics.
9. `R8.AC9` WHEN continuous integration runs the local workflow acceptance proof, the system SHALL fail if a required phase is not exercised.
10. `R8.AC10` WHEN a contributor runs `make e2e`, the system SHALL create its independent run-unique disposable cluster rather than reuse `kubeseer-local`.
11. `R8.AC11` WHEN local workflow verification terminates, the system SHALL leave the repository worktree unchanged.
12. `R8.AC12` The system SHALL preserve `make e2e` as the authoritative full-product cluster certification entry point.

### R9 Sanitized local diagnostics

**User Story:** As a contributor diagnosing a failure, I want actionable local
state collected without credentials or observed sensitive values, so that I
can troubleshoot and share evidence safely.

#### Acceptance Criteria

1. `R9.AC1` WHEN a contributor runs `make local-diagnostics`, the system SHALL verify the owned cluster identity before reading cluster state.
2. `R9.AC2` WHEN local diagnostics run, the system SHALL record the allowlisted tool and dependency versions.
3. `R9.AC3` WHEN local diagnostics can reach the API server, the system SHALL record an allowlisted projection of node and control-plane readiness.
4. `R9.AC4` WHEN local diagnostics can reach the API server, the system SHALL record an allowlisted projection of Kubeseer Deployment and Pod readiness.
5. `R9.AC5` WHEN local diagnostics can reach the API server, the system SHALL record an allowlisted projection of CRD and webhook readiness.
6. `R9.AC6` WHEN local diagnostics can reach the API server, the system SHALL record the canonical access-policy identity without serializing its unrestricted body.
7. `R9.AC7` WHEN local diagnostics can reach the API server, the system SHALL record bounded manager logs with the approved observability vocabulary.
8. `R9.AC8` WHEN local diagnostics can reach the API server, the system SHALL record bounded Kubernetes Event identities without free-form messages.
9. `R9.AC9` WHEN local diagnostics include Kubeseer status, the system SHALL use the approved sanitized diagnostic projection.
10. `R9.AC10` The system SHALL exclude Kubernetes Secret bodies from local diagnostics.
11. `R9.AC11` The system SHALL exclude kubeconfig contents and authentication tokens from local diagnostics.
12. `R9.AC12` The system SHALL exclude ambient cloud credentials from local diagnostics.
13. `R9.AC13` The system SHALL exclude extracted source values from local diagnostics by default.
14. `R9.AC14` WHEN local diagnostics complete, the system SHALL report their exact repository-external destination path.
15. `R9.AC15` IF the local API server is unavailable, THEN the system SHALL retain the available local metadata and identify the failed cluster observation.
16. `R9.AC16` IF diagnostic collection cannot create its destination, THEN the system SHALL exit non-zero without changing cluster state.

### R10 Safe example and environment cleanup

**User Story:** As a contributor, I want teardown to affect only Kubeseer's
owned local state, so that unrelated Podman and Kubernetes environments remain
untouched.

#### Acceptance Criteria

1. `R10.AC1` WHEN a contributor runs `make local-down`, the system SHALL validate the exact `kubeseer-local` cluster name against the ownership metadata.
2. `R10.AC2` WHEN ownership validation succeeds, the system SHALL delete only the exact kind cluster `kubeseer-local`.
3. `R10.AC3` WHEN exact cluster deletion succeeds, the system SHALL remove only the corresponding project-owned local state.
4. `R10.AC4` WHEN exact cluster deletion succeeds, the system SHALL report the retained local image and tool-cache state.
5. `R10.AC5` WHEN `make local-down` finds no owned cluster, the system SHALL complete successfully with an explicit already-absent state.
6. `R10.AC6` WHEN `make local-examples-down` runs, the system SHALL preserve the Kubeseer Helm release and development access policy.
7. `R10.AC7` The system SHALL exclude global Podman container prune operations from local cleanup.
8. `R10.AC8` The system SHALL exclude global Podman image prune operations from local cleanup.
9. `R10.AC9` The system SHALL exclude unrelated Podman networks and volumes from local cleanup.
10. `R10.AC10` The system SHALL exclude every other kind or Kubernetes cluster from local cleanup.
11. `R10.AC11` The system SHALL exclude external clusters from every invocation of the destructive Kubeseer CRD purge client.
12. `R10.AC12` IF cluster ownership metadata is absent or inconsistent, THEN the system SHALL preserve the named cluster.
13. `R10.AC13` IF exact cluster deletion fails, THEN the system SHALL preserve the kubeconfig and ownership metadata required for retry.
14. `R10.AC14` IF exact cluster deletion fails, THEN the system SHALL report the remaining exact cluster identity.
15. `R10.AC15` WHEN local cleanup terminates, the system SHALL leave the repository worktree unchanged.

### R11 Documentation and contributor guidance

**User Story:** As a new contributor, I want one complete local guide with
expected outcomes and limitations, so that I can start without undocumented
cluster knowledge.

#### Acceptance Criteria

1. `R11.AC1` The system SHALL document Linux/amd64 as the initial supported local host profile.
2. `R11.AC2` The system SHALL document macOS, Windows, WSL, and non-amd64 hosts as uncertified profiles for this feature.
3. `R11.AC3` The system SHALL document every prerequisite with its centrally declared or minimum supported version.
4. `R11.AC4` The system SHALL document one ordered workflow from `make local-check` through `make local-down`.
5. `R11.AC5` The system SHALL document the exact local cluster, context, namespace, release, and state-directory identities.
6. `R11.AC6` The system SHALL document how to inspect Kubeseer status, conditions, Events, manager logs, readiness, and metrics.
7. `R11.AC7` The system SHALL document how to apply, verify, inspect, and remove each bundled example.
8. `R11.AC8` The system SHALL document first-time network requirements for pinned modules, manifests, and container images.
9. `R11.AC9` The system SHALL document known rootless Podman and kind limitations for the supported host profile.
10. `R11.AC10` The system SHALL document the separation between persistent `kubeseer-local` exploration and disposable `make e2e` certification.
11. `R11.AC11` The system SHALL link the local development guide from the repository README.
12. `R11.AC12` IF a documented recovery step is destructive to the owned local cluster, THEN the system SHALL name the exact target that will be deleted.

## Non-Functional Requirements

- `NFR1` **Isolation:** Local commands SHALL operate only on the explicitly owned kubeconfig, context, cluster, Helm release, state directory, and example identities; `R3`, `R8.AC1`, `R9.AC1`, and `R10` provide behavioral coverage.
- `NFR2` **Security:** The local environment SHALL preserve logical-policy and Kubernetes-RBAC separation while excluding credentials, Secrets, and extracted values from default diagnostics; `R3.AC5`, `R5.AC6` through `R5.AC11`, and `R9` provide behavioral coverage.
- `NFR3` **Reproducibility:** Equivalent source, version, toolchain, configuration, and example inputs SHALL converge to equivalent cluster, image, package, and public example outcomes; `R2`, `R3`, `R4`, `R5`, `R6.AC11`, and `R7` provide behavioral coverage.
- `NFR4` **Reliability:** Setup, convergence, verification, diagnostics, and cleanup SHALL use bounded operations with actionable failure states; `R2.AC8` through `R2.AC12`, `R3.AC7` through `R3.AC10`, `R6`, `R7.AC17`, `R7.AC18`, `R8`, `R9.AC15`, `R9.AC16`, and `R10` provide behavioral coverage.
- `NFR5` **Usability:** A contributor without an existing cluster SHALL be able to complete the documented lifecycle through stable repository entry points and explicit terminal markers; `R1`, `R2`, and `R11` provide behavioral coverage.
- `NFR6` **Diagnosability:** Failures SHALL identify the earliest failed prerequisite, phase, owned target, or awaited condition using only allowlisted context; `R2`, `R4.AC9`, `R4.AC10`, `R6.AC12`, `R7.AC18`, `R9`, and `R10.AC13` through `R10.AC14` provide behavioral coverage.
- `NFR7` **Portability:** The initial local workflow SHALL remain cloud-independent and registry-independent on the certified Linux/amd64 host profile; `R2`, `R3`, `R4`, and `R11.AC1` through `R11.AC3` provide behavioral coverage.
- `NFR8` **Non-vacuous testability:** Local and continuous-integration proofs SHALL fail on missing phases, zero-match assertions, wrong cluster identity, or incorrect public outcomes; `R7.AC8`, `R7.AC17`, `R8`, and `R10` provide behavioral coverage.
- `NFR9` **Maintainability:** Local orchestration SHALL compose the canonical build, packaging, observability, and end-to-end entry points instead of maintaining divergent product semantics; `R1.AC11`, `R4`, `R5`, `R8.AC12`, and `R9.AC7` through `R9.AC9` provide behavioral coverage.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `packaging-and-installation` and `end-to-end-scenarios` contracts plus every upstream capability exercised by the examples.
- `C2` The initial supported host profile is Linux/amd64; additional operating systems, Podman-machine hosts, WSL, and architectures require separate approved compatibility evidence.
- `C3` The only required local Kubernetes path is kind using rootless Podman; Docker, cloud clusters, ambient clusters, and automatic existing-cluster fallback are not supported.
- `C4` The supported Kubernetes compatibility matrix, kind node-image mapping, cert-manager version, Go tool versions, Helm baseline, and product API version remain centrally declared by their owning specifications and repository configuration.
- `C5` Local installation uses the canonical `charts/kubeseer` Helm package, the `certManager` certificate mode, release `kubeseer`, namespace `kubeseer-system`, and singleton policy `installation-access-ceiling`.
- `C6` The development access policy may grant only the explicit namespaces and resource kinds needed by the bundled examples; logical authorization remains mandatory even when local ServiceAccount RBAC is broader.
- `C7` The persistent local environment and the disposable E2E certification environment must never share a cluster identity or kubeconfig.
- `C8` `make e2e` remains responsible for full-product certification; this feature verifies contributor orchestration, example usability, local inspection, and safe persistent lifecycle.
- `C9` First-time setup may download pinned modules, manifests, tools, and container images; offline first-time bootstrap is not required.
- `C10` Local mutable state and diagnostics must remain outside the repository worktree unless a version-controlled example, configuration, script, or document is intentionally authored.
- `C11` Kubeseer-authored scripts, manifests, examples, tests, and documentation remain under Apache License 2.0 with Alessandro Rontani as the default copyright holder.
- `C12` Automated proof follows the repository testing constitution: genuine higher-layer or cluster behavior is required where applicable, zero-match execution fails, and no dedicated package-local unit-test layer is introduced.

## Out Of Scope

- Supporting macOS Podman machine, Windows, WSL, non-amd64 hosts, Docker, minikube, k3d, or another Kubernetes provider in the certified local profile.
- Connecting to, installing into, diagnosing, or deleting an externally managed or cloud-provider Kubernetes cluster.
- Maintaining multiple simultaneous Kubeseer local clusters, named profiles, multi-node topology variants, or high-availability local installations.
- Publishing controller images or charts to an external registry, signing images, distributing provenance attestations, or managing registry credentials.
- Supporting the `externalSecret` certificate profile in the bundled local workflow; packaging verification remains responsible for that supported production fallback.
- Redefining Kubeseer APIs, selectors, extraction, typing, operators, aggregation, status, authorization, admission, observability, limits, or package lifecycle semantics.
- Replacing `make e2e`, duplicating its full scenario catalog, or treating successful local examples as release certification.
- Load, soak, chaos, upgrade-skew, disaster-recovery, backup, restore, multi-cluster, or cloud-provider verification.
- Automatically installing IDE extensions, dev containers, virtual machines, Podman machine, system packages, or host networking configuration.
- Global Podman pruning, unrelated container-engine cleanup, destructive CRD purge on an external cluster, or collection of Secret and credential material.
