---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-25T13:42:19Z
last_modified: 2026-08-25T13:42:19Z
approved_fingerprint: sha256:5ed040de4077c66ff76591309afc4862013bfe4ae5c2d521fafa477231594fae
source_requirements_approved_at: 2026-08-25T13:39:25Z
source_requirements_fingerprint: sha256:05f841f8bc0756abee58cdf47801feae108b15da7207ddb5abdad3d586a845be
---

# Feature Design

## Overview

Kubeseer will use explicit module boundaries and a three-layer higher-fidelity
test model. Each production module owns one cohesive responsibility, exposes
narrow typed contracts, and receives infrastructure dependencies through
constructors or function adapters. The future runtime composition root wires
concrete Kubernetes clients; higher-layer tests use the same constructors as
alternate composition roots.

The required test layers are in-process cross-module integration, local
Kubernetes API integration, and full end-to-end. In-process tests compose at
least two real Kubeseer modules and replace only outbound infrastructure
boundaries. API integration uses controller-runtime `envtest` with pinned local
binaries. Full-cluster behavior uses the project-owned kind cluster with
Podman. Before the first real production collaboration path exists, API
integration is the default bootstrap proof; after that path exists,
cross-module integration becomes the default lowest layer. No dedicated
unit-test suite or entry point remains, and no default test path loads an
ambient kubeconfig or falls back to an external cluster.

The controller-runtime documentation describes envtest as a local API server
and etcd without kubelet, controller-manager, or other built-in components, so
the design routes behavior that depends on those components to kind instead.
[Kubebuilder envtest guidance](https://master.book.kubebuilder.io/reference/envtest.html)

<!-- assumed: infrastructure interfaces are owned by the consuming module and concrete adapters are wired at the composition root, matching the existing internal/discovery.DiscoveryClient boundary (source: internal/discovery/resolver.go) -->
<!-- assumed: new cross-module suites are separated by package location and all layers use explicit Make targets rather than build tags; approved package-local envtest contracts remain in place and are selected by the API target so existing proof paths are preserved (source: Makefile and existing Walden evidence) -->
<!-- assumed: one envtest control plane is shared within one test package execution, while every scenario receives unique object identities and temporary paths; the whole control plane is discarded after the package completes -->
<!-- assumed: the canonical feature portfolio and dependency order are synchronized after design approval and before tasks are drafted, as required by C6 -->
<!-- assumed: suite discovery, not a manually maintained feature flag, determines whether a production collaboration path has a qualifying TestModuleIntegration scenario; this prevents configuration drift while preserving the bootstrap required by R2.AC6 and R4.AC7 -->

## Architecture

```text
                         future runtime composition root
                         (manager/controller wiring only)
                                      │
                     explicit constructors and typed values
                                      │
             ┌────────────────────────┼────────────────────────┐
             ▼                        ▼                        ▼
      internal/discovery       internal/<domain>        api/v1alpha1
      outbound client port     pure domain behavior      public API types
             │                        │
             └────────────── no imports from test/ ─────┘

test/integration/              real modules + scripted outbound ports
test/envtest/                  real modules/adapters + local API server/etcd
test/e2e/                      built operator + project-owned kind/Podman
```

Production dependencies point from composition toward domain modules and from
domain modules toward stable value contracts. A domain module never imports a
controller package or a test package. Interfaces live with the code that
consumes them; adapters may remain in the owning package until a second adapter
or a dependency cycle justifies a subpackage. Go's compiler rejects import
cycles, while focused construction and collaboration tests prove that modules
remain independently usable.

The test layer is selected by repository entry point rather than by ambient
machine state. The resulting command surface is:

| Layer | Canonical location | Entry point | Infrastructure |
| --- | --- | --- | --- |
| Module integration | `test/integration/...` | `make test-integration` | Two or more real modules; no external process |
| Kubernetes API integration | Package-local API contracts plus `test/envtest/...` cross-module scenarios | `make test-api` | Local `kube-apiserver` and etcd from pinned envtest assets |
| End-to-end | `test/e2e/...` | `make e2e` | Project-owned kind cluster with Podman |

`make test` first probes for the anchored `TestModuleIntegration` suite. When
the suite exists, it delegates to `test-integration`; while it does not exist,
it delegates to `test-api` and reports the API bootstrap layer. An explicit
`make test-integration` with no matching suite reports `not-applicable` and
exits non-zero. API integration and end-to-end otherwise remain explicit
because they start local processes or containers.
`make test-compatibility` reuses the API integration suite across the approved
Kubernetes version matrix.

## Options Considered

### Option A — Explicit ports with layered local test environments

- Summary: Keep modules as ordinary Go packages with consumer-owned interfaces,
  use envtest as the bootstrap proof, compose real modules in-process as soon as
  a production collaboration path exists, and use kind only for
  complete-cluster behavior.
- Why chosen: It gives fast feedback for most integration defects, preserves
  real Kubernetes validation where required, and makes cloud independence a
  property of the harness rather than a contributor convention.

### Option B — Run every integration scenario on kind

- Summary: Build and deploy the complete operator into kind for all integration
  tests.
- Why rejected: It couples module verification to image builds, container
  startup, RBAC, and controller readiness. Failures become harder to attribute,
  local feedback becomes slower, and module boundaries receive little direct
  exercise.

### Option C — Use controller-runtime's fake client for all Kubernetes tests

- Summary: Keep every suite in-process by replacing Kubernetes with the generic
  fake client.
- Why rejected: The fake client does not provide OpenAPI validation and differs
  in metadata, resource-version, patch, and status behavior. Its own
  documentation recommends envtest when real API-server behavior matters.
  [controller-runtime fake client limitations](https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/client/fake)

### Option D — Introduce a dependency-injection or mocking framework

- Summary: Register modules and generated mocks in a shared runtime container.
- Why rejected: Kubeseer's current dependency graph is small enough for
  constructors and function adapters. A framework would obscure ownership,
  enlarge the test-only abstraction surface, and make compile-time wiring less
  obvious.

### Option E — Retain package-local unit tests as a fast inner layer

- Summary: Keep pure-value and scripted-port tests beside each production
  package, then add integration, envtest, and end-to-end coverage above them.
- Why rejected: It conflicts with the approved maintenance policy for this
  AI-generated codebase and duplicates contract assertions across layers. The
  accepted cost is slower and less granular failure localization; the design
  compensates with stable scenario names and boundary-level diagnostics.

### Option F — Create a test-only collaboration or split a module for bootstrap

- Summary: Introduce an otherwise unnecessary production seam, or connect API
  and discovery only inside a test, so `TestModuleIntegration` exists
  immediately.
- Why rejected: Neither shape proves a real production collaboration contract.
  It would make the package graph serve the harness instead of cohesive domain
  responsibilities and would violate the approved classifier in `R7.AC3`.

## Simplicity And Elegance Review

- Simplest viable shape: Explicit constructors, consumer-owned interfaces,
  handwritten scripted ports, three test directories, and Make targets over
  ordinary `go test` commands. No generic scenario DSL or DI container is
  introduced.
- Coupling check: Test code imports production packages, but production code
  never imports harness code. Kubernetes clients stop at outbound ports; plain
  values cross module boundaries.
- Future-proofing: New modules join the same layers without registering in a
  central framework. A shared helper package is extracted only after at least
  two suites require the same lifecycle behavior.
- First-draft challenge: A single kind-based suite would have fewer named test
  layers, but it would add more runtime machinery to every proof and would not
  demonstrate independent module construction. The layered shape is the
  smaller architecture for the stated testability goal.
- Policy challenge: Retaining unit tests would shorten diagnosis for pure
  helpers, but it would preserve the maintenance layer explicitly rejected by
  the approved requirements. Three higher layers are therefore the simplest
  compliant shape.
- Bootstrap challenge: An artificial second module would make the in-process
  target green sooner, but envtest already provides meaningful local evidence
  for the current API and discovery behavior without distorting production
  boundaries.
- Deferred complexity: Coverage aggregation, test sharding, remote conformance,
  performance, chaos, and reusable cluster pools remain outside this feature.

## Components And Interfaces

### Production module contract

- Purpose: Keep each API, discovery, policy, selection, extraction, typing,
  aggregation, reconciliation, and status responsibility independently
  constructible.
- Inputs/Outputs: Constructors receive narrow interfaces or function types;
  operations receive `context.Context` and typed values; results and stable
  errors leave the package without exposing a concrete client.
- Dependencies: Standard library and domain-relevant Kubernetes value types.
  Concrete controller-runtime or client-go behavior remains behind the module's
  outbound port.
- Rules: No service locator, mutable package singleton, test hook in production
  code, or reverse import from a domain module into the runtime composition
  root.
- Requirements: `R1`, `R6`, `R7`, `NFR1`

### Module integration suite

- Purpose: Prove collaboration contracts before starting Kubernetes processes.
- Location: `test/integration/` as an external test package that imports the
  production modules under test.
- Inputs/Outputs: Table-driven scenarios construct at least two real modules;
  scripted ports supply deterministic external outcomes; assertions inspect the
  final production result or stable failure.
- Scenario contract: The first feature that introduces a real production
  collaboration path creates the named top-level `TestModuleIntegration` with
  successful data-flow and upstream-failure subtests. Later features append
  scenarios when they add collaboration paths.
- Bootstrap state: Until that path exists, no placeholder suite is created.
  Direct selection reports `TEST_LAYER=module-integration
  STATUS=not-applicable` and fails, while the default entry point selects the
  API bootstrap layer.
- Dependencies: Production packages plus small test-local scripted adapters.
  The suite does not import envtest, controller-runtime's fake client, or a
  kubeconfig loader.
- Requirements: `R2`, `R5`, `R6`, `R7`, `NFR1`, `NFR2`, `NFR4`, `NFR6`

### Test-layer classifier and retirement gate

- Purpose: Make removal of existing unit tests reviewable and prevent isolated
  tests from being relabeled as integration coverage.
- Classification rule: A module-integration scenario imports and composes at
  least two production modules. An API-integration scenario starts local
  envtest and reaches a real API-server boundary. An end-to-end scenario runs
  the built operator in the project-owned kind cluster. A test meeting none of
  these definitions is not retained as required automated coverage.
- Migration rule: Every existing unit-test behavior is classified as still
  required, duplicated by a higher layer, or obsolete. Required behavior is
  mapped to a named higher-layer scenario before the original test is deleted;
  the implementation tasks carry this finite migration matrix.
- Structural gate: Package-local `*_test.go` files are permitted only for
  explicitly named envtest suites. In-process collaboration tests live under
  `test/integration/`, cross-module API scenarios under `test/envtest/`, and
  cluster scenarios under `test/e2e/`. The repository verification command
  rejects remaining package-local non-envtest suites and a `test-unit` target.
- Completion rule: The migration gate runs only after all replacement
  scenarios pass locally, then removes the retired tests, obsolete fixtures,
  and documentation in the same change.
- Requirements: `R6`, `R7`, `NFR4`, `NFR6`

### Non-vacuous test-layer runner

- Purpose: Make a selected layer fail when its named suite contains no matching
  tests and identify the layer in local or CI output.
- Location: One small repository script under `hack/`, invoked only through
  Make targets.
- Inputs/Outputs: The runner receives a layer name, package path, anchored
  top-level test expression, and `probe` or `required` mode. Probe mode returns
  a distinct not-applicable result when no matching package or test exists.
  Required mode prints a stable layer/status marker and fails on the same
  condition. A match prints the layer marker and runs `go test -v` with the
  anchored expression.
- Selection contract: `make test` consumes probe mode only to choose between
  the module suite and API bootstrap. Direct layer targets always use required
  mode, so a selected empty suite cannot pass.
- Dependencies: Go tooling and portable shell already used by repository
  verification scripts.
- Requirements: `R2`, `R4`, `R6`, `NFR2`, `NFR4`

### Local envtest harness

- Purpose: Exercise API admission, defaulting, discovery, persistence, status,
  and real-client behavior without an external cluster.
- Location: Existing package-specific envtest contracts remain beside their
  owning packages. New API scenarios that compose multiple modules live in
  `test/envtest/`. `make test-api` invokes both sets without changing existing
  proof package paths.
- Inputs/Outputs: The Make target resolves centrally pinned assets and passes an
  explicit assets directory. The harness sets `UseExistingCluster` to false,
  starts `envtest.Environment`, installs committed CRDs, creates clients only
  from the returned `rest.Config`, and stops the environment in teardown.
- Isolation: One control plane is shared per package execution. Every scenario
  uses a unique prefix, namespace, and `t.TempDir`; cluster-scoped mutations are
  serialized. Objects are removed explicitly before teardown because envtest
  does not run namespace or garbage-collection controllers.
- Safety check: The returned API-server host must resolve to loopback. An
  ambient `KUBECONFIG`, cloud credential, or `USE_EXISTING_CLUSTER` value never
  supplies the test client configuration.
- Dependencies: controller-runtime v0.24.1 envtest and the repository's pinned
  Kubernetes asset resolver. The versioned envtest API supports an explicit
  `UseExistingCluster` setting and local binary directory.
  [envtest v0.24.1 API](https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/envtest)
- Requirements: `R3`, `R4`, `R5`, `R7`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`

### Kind end-to-end boundary

- Purpose: Reserve complete-cluster verification for behavior that envtest
  cannot represent, such as built-in controllers, workload lifecycle, and the
  deployed operator image.
- Inputs/Outputs: The entry point sets `KIND_EXPERIMENTAL_PROVIDER=podman`, uses
  one centrally declared Kubeseer-owned cluster name, writes kubeconfig to a
  test-owned temporary path, and passes that path explicitly to every client.
- Lifecycle: Setup and teardown target only the named cluster. No Podman-wide
  prune, image deletion, network deletion, or ambient kubectl context mutation
  is permitted.
- Dependencies: The later `end-to-end-scenarios` and
  `local-development-environment` features populate scenarios and full cluster
  lifecycle. The foundation defines the entry-point and safety contract first.
  kind documents explicit Podman selection through
  `KIND_EXPERIMENTAL_PROVIDER=podman` and lists rootless host constraints.
  [kind rootless Podman guidance](https://kind.sigs.k8s.io/docs/user/rootless/)
- Requirements: `R3`, `R4`, `R5`, `NFR3`, `NFR4`, `NFR5`

### Repository test command surface

- Purpose: Give contributors, IDE wrappers, Walden proofs, and CI one stable
  vocabulary for selecting test cost and fidelity.
- Entry points: `make test-integration`, `make test`, `make test-api`,
  `make test-compatibility`, and `make e2e`.
- Contract: Local and CI executions call the same Make targets. Targets may set
  writable caches and resolve assets, but do not change which scenarios belong
  to a layer.
- Bootstrap selection: `make test` uses runner probe mode. A matching
  `TestModuleIntegration` selects `make test-integration`; an explicit
  not-applicable result selects `make test-api`; any other probe failure stops
  immediately. No manually maintained feature flag controls the switch.
- Compatibility: Existing `make test-api` and `make test-compatibility` names
  remain stable while their package selection is consolidated.
- Selection: Package-local envtest tests skip explicitly when no assets are
  supplied. `make test-api` resolves assets first and invokes the non-vacuous
  runner for every required named suite, so a selected API layer cannot pass by
  skipping all tests.
- Requirements: `R2`, `R4`, `R6`, `R7`, `NFR2`, `NFR3`, `NFR4`, `NFR6`

### Portfolio synchronization gate

- Purpose: Make the foundation part of the canonical architecture before task
  planning.
- Inputs/Outputs: Update `SPECIFICATIONS.md` and `.walden/constitution.md` to
  list `integration-testing-foundation`, place it before features that add new
  cross-module flows, change the standard `make test` description from unit
  plus integration to cross-module integration, and preserve the existing
  ownership of envtest contracts, end-to-end scenarios, and the local
  development environment.
- Gate: Tasks are not drafted until this synchronization is complete and the
  approved requirements remain fresh.
- Requirements: `R6`, `R7`, `NFR1`, `NFR2`

## Data Models

This feature introduces no production persistence or public API fields.

Test code uses three deliberately small data shapes:

- **Scenario:** A table entry with a stable name, constructor/setup callback,
  invocation callback, and assertion callback. It remains local to the suite
  that owns the behavior rather than becoming a generic framework.
- **Scripted port:** A test implementation of one outbound interface with
  declared responses and recorded calls. An undeclared call fails the owning
  test immediately with the interface and operation name.
- **Ownership scope:** A run-specific prefix, namespace where applicable,
  temporary directory, and bounded cleanup callback. It is never serialized or
  shared between independent test processes.

Tool and Kubernetes versions remain in the existing central Makefile/module
configuration. Envtest assets may be cached outside the repository; cache
contents are build inputs, not committed state.

## Error Handling

- Constructor failures identify the module and outbound boundary without
  embedding arbitrary upstream payloads.
- Scripted ports reject unexpected calls instead of returning permissive zero
  values.
- The test-layer runner fails before execution when no named tests match.
- Missing or unsupported envtest assets fail `make test-api` with the requested
  Kubernetes version, host platform, and remediation command. They never enable
  `UseExistingCluster`.
- Envtest startup, CRD establishment, API operations, and shutdown use bounded
  contexts. Control-plane output is attached or retained on failure.
- Teardown errors fail the scenario and name only the ownership scope that
  remains.
- Missing kind or Podman prerequisites fail `make e2e` before cluster mutation.
  No external kubeconfig fallback is attempted.
- CI and local targets propagate the first failing command's non-zero exit
  status.

## Security Considerations

- Test entry points never call controller-runtime configuration helpers that
  discover an ambient kubeconfig. Envtest clients use only the configuration
  returned by the locally started environment.
- The envtest harness explicitly disables existing-cluster mode and verifies a
  loopback API endpoint before installing CRDs or creating objects.
- Kind uses an explicit Podman provider, project-owned cluster name, and
  temporary kubeconfig; cleanup names exactly that cluster.
- Fixtures use synthetic values. Logs, diagnostics, and failure messages do not
  include Secret data or extracted sensitive values.
- Test helpers receive only the permissions and clients required by their
  scenario. A broad fake or broad ServiceAccount is not treated as proof of the
  logical authorization boundary.

## Failure Modes And Tradeoffs

- Failure mode: Every dependency is abstracted behind an interface, producing
  interface explosion.
  - Mitigation: Introduce interfaces only at outbound infrastructure boundaries
    or independently replaceable module seams, and keep them consumer-owned.
  - Tradeoff: Pure value helpers remain concrete and receive coverage only
    through a consuming module collaboration or API path.
- Failure mode: Removing unit tests hides the exact function that introduced a
  regression.
  - Mitigation: Name scenarios by observable contract, emit the earliest
    failing module boundary, and keep scripted outbound calls strict and
    attributable.
  - Tradeoff: Diagnosis is slower and coverage is coarser than with focused
    unit tests; this is the explicit maintenance trade-off approved in `R7`.
- Failure mode: Unit tests are deleted before their still-required behavior is
  represented at a higher layer.
  - Mitigation: Complete the behavior-by-behavior migration matrix, run each
    replacement scenario, and apply the no-unit structural gate before removal.
  - Tradeoff: Retirement may require additional envtest scenarios and cannot be
    completed as a mechanical file deletion.
- Failure mode: In-process integration passes while Kubernetes rejects the same
  data.
  - Mitigation: Route schema, admission, status, discovery, and persistence
    semantics through the envtest layer.
  - Tradeoff: API integration is slower than pure Go tests and needs local
    binaries.
- Failure mode: Envtest gives false confidence for garbage collection, kubelet,
  scheduler, or built-in-controller behavior.
  - Mitigation: Keep those assertions out of envtest and place them in the kind
    end-to-end layer.
  - Tradeoff: A smaller set of scenarios pays the container and image-build
    cost.
- Failure mode: A shared envtest control plane leaks state between scenarios.
  - Mitigation: Use unique ownership scopes, explicit object cleanup, serialized
    cluster-scoped mutations, and final process teardown.
  - Tradeoff: Namespace objects may remain until the disposable control plane
    exits because envtest has no namespace controller.
- Failure mode: Rootless Podman or kind host prerequisites are incomplete.
  - Mitigation: Run read-only prerequisite checks before creation and return
    distribution-specific diagnostics without touching unrelated state.
  - Tradeoff: Full end-to-end support remains limited to declared host profiles.
- Failure mode: A test selector matches zero tests and reports success.
  - Mitigation: List and count anchored top-level tests before running the
    selected layer, then assert named pass output in Walden proofs.
  - Tradeoff: Every layer maintains one stable top-level suite name.
- Failure mode: A compile, package-listing, or runner error is mistaken for the
  bootstrap state and silently redirects `make test` to envtest.
  - Mitigation: Only the exact no-package or zero-match probe result means
    not-applicable; syntax, compilation, and tooling failures propagate without
    fallback.
  - Tradeoff: Default execution performs one bounded discovery probe before the
    selected suite.
- Failure mode: The bootstrap default is materially slower than a future
  in-process integration suite.
  - Mitigation: Reuse pinned envtest assets and switch automatically as soon as
    the first qualifying production collaboration scenario lands.
  - Tradeoff: Early contributors pay local control-plane startup cost instead
    of maintaining unit tests or artificial module seams.
- Failure mode: Downloaded binaries or tool versions drift between local and CI.
  - Mitigation: Reuse central version pins, record Walden environment probes,
    and resolve assets through the same Make targets.
  - Tradeoff: First execution may require downloads; offline execution requires
    prefetched assets.
- Failure mode: The feature remains outside the canonical portfolio.
  - Mitigation: Make portfolio synchronization a pre-task gate.
  - Tradeoff: Design approval does not by itself authorize implementation
    planning.

## Testing Strategy

### Unit-test retirement and coverage migration

- Inventory every top-level test currently in `api/` and `internal/` and map
  each still-required behavior to one named module-integration or API-integration
  scenario.
- Move API serialization, schema, status-subresource, generated deep-copy, and
  typed-client behavior into envtest scenarios that observe the contract
  through real API or cache boundaries.
- Move discovery resolution, scope, error propagation, cache refresh, and
  invalidation behavior into envtest scenarios. A counting transport and the
  existing injectable clock may expose cache decisions while the resolver still
  reaches the real local discovery endpoint.
- Preserve concurrency and failure assertions only where they remain observable
  at a qualifying higher layer; obsolete or duplicate implementation-detail
  assertions are removed rather than copied.
- Remove `kubeseer_types_test.go`, `contracts_test.go`, `resolver_test.go`, their
  retired fixtures, and any dedicated unit-test command only after all mapped
  replacement scenarios pass.
- Keep package-local tests only when their top-level suite starts envtest; this
  preserves existing API proof ownership without creating a unit-test exception.

### In-process module integration

- `TestModuleIntegration` composes at least two production packages through
  their real contracts.
- The first feature with a production collaboration path introduces successful
  data-flow and typed upstream-failure subtests; later features append their own
  paths.
- Only outbound infrastructure is scripted. No controller-runtime fake client
  stands in for API-server behavior.
- Once present, the suite runs in the default `make test` path and remains
  suitable for parallel execution after ownership isolation is proven.
- A scenario that constructs only one production package with scripted
  collaborators is rejected by the test-layer classifier.
- While the suite is absent, direct selection fails as not-applicable and the
  default entry point selects envtest instead.

### Envtest API integration

- `TestKubernetesAPIIntegration` starts one local control plane, installs the
  committed CRDs, and exercises real clients and discovery.
- Existing API and discovery envtest cases retain their package paths and
  approved assertions while `make test-api` consolidates their execution with
  new cross-module API scenarios.
- While no production collaboration path exists, this suite is also the
  default `make test` bootstrap and prints that selection explicitly.
- A safety subtest sets an unusable ambient kubeconfig and proves the client
  still reaches only the loopback envtest endpoint.
- The same suite runs under every centrally pinned compatibility version.
- Missing assets, zero matching tests, startup timeout, and cleanup failure are
  explicit failures.

### Kind end-to-end contract

- `TestEndToEnd` is the stable top-level suite populated by the dedicated
  end-to-end feature.
- Foundation verification proves provider, cluster-name, kubeconfig, and
  cleanup scoping. Product scenarios remain owned elsewhere.
- The runner refuses to report success while no matching end-to-end scenario is
  present.

### Entry-point acceptance checks

- Exercise the non-vacuous runner through each real layer entry point and one
  temporary zero-match package, proving command behavior rather than an
  isolated helper implementation.
- Prove that the current zero-path repository makes direct
  `test-integration` selection fail as not-applicable while `make test`
  executes the API layer successfully.
- Prove with a temporary qualifying `TestModuleIntegration` package that probe
  mode selects the in-process layer and does not start envtest.
- Verify layer markers and failure diagnostics through the public Make targets.
- Verify ownership prefixes and cleanup through the scenarios that create real
  temporary files or envtest objects; no dedicated helper-test suite is kept.

## Verification Plan

- Module-boundary proof: `go build ./...` and import-graph checks prove acyclic
  packages and independently constructible modules for `R1` and `R6` without
  manufacturing a collaboration seam.
- Bootstrap proof: In the current zero-path checkout, `make test-integration`
  fails with the stable not-applicable marker and `make test` executes the
  local API suite for `R2`, `R4`, and `R6`.
- Collaboration proof: Once a real path exists, `make test-integration`
  executes named success and upstream-failure subtests with at least two real
  modules for `R2`, `R5`, and `NFR1`.
- Non-vacuous proof: real layer executions, one temporary zero-match acceptance
  fixture, and Walden `expect_output` assertions prove zero-match failure and
  layer reporting for `R2`, `R4`, `R6`, and `NFR4`.
- API proof: `make test-api` starts envtest with pinned assets, installs CRDs,
  exercises real clients, verifies loopback configuration, and tears down for
  `R3`, `R4`, `R5`, `NFR2`, `NFR3`, and `NFR5`.
- Compatibility proof: `make test-compatibility` runs the same API suite against
  every approved Kubernetes version and reports each version explicitly.
- External-cluster safety proof: tests provide an ambient kubeconfig and
  existing-cluster environment value pointing at an unreachable endpoint;
  successful local envtest execution demonstrates that neither is selected.
- Kind safety proof: harness tests capture command arguments and prove explicit
  Podman provider, project cluster name, temporary kubeconfig, and exact delete
  target without deleting a real cluster.
- Unit-retirement proof: repository verification rejects `test-unit`, rejects
  package-local non-envtest suites, confirms each behavior in the finite
  migration matrix has a passing higher-layer scenario, and confirms the
  retired test files are absent for `R7` and `NFR6`.
- Portfolio proof: before tasks are generated, the canonical portfolio and
  dependency order name this feature and feature-specific Walden validation
  remains clean.
- Operational evidence: Failure output records the layer, named scenario,
  Kubernetes version, owned scope, and bounded control-plane diagnostics without
  fixture secrets.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Production module contract; Architecture dependency rules; module-boundary proof |
| `R2` | Module integration suite; bootstrap state; Non-vacuous test-layer runner; collaboration proof |
| `R3` | Local envtest harness; Kind end-to-end boundary; external-cluster safety proof |
| `R4` | Test-layer table; bootstrap selector; Repository test command surface; layer markers |
| `R5` | Ownership scope; envtest isolation; bounded teardown; kind cleanup contract |
| `R6` | Production module contract; bootstrap proof; collaboration gate; Portfolio synchronization gate |
| `R7` | Test-layer classifier; migration matrix; structural retirement gate; unit-retirement proof |
| `NFR1` | Consumer-owned interfaces; explicit constructors; no DI framework |
| `NFR2` | Shared Make targets; pinned assets; compatibility matrix; environment probes |
| `NFR3` | Local envtest control plane; explicit kind-on-Podman boundary |
| `NFR4` | Layer runner; stable suite names; earliest-boundary and prerequisite diagnostics |
| `NFR5` | Existing-cluster disabled; loopback assertion; scoped kubeconfig and cleanup |
| `NFR6` | Contract-level assertions; migration deduplication; no-unit structural gate |
