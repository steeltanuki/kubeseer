# Kubeseer Project Constitution

This document records project-wide rules that apply to every Kubeseer feature. Feature-specific behavior belongs in `.walden/specs/<feature>/` and must pass the Walden requirements, design, tasks, and execution gates.

## Project Summary

Kubeseer is a Kubernetes operator for declaratively observing built-in resources and Custom Resources. A `Kubeseer` resource selects authorized objects, extracts values with JSONPath, converts those values to explicit logical types, optionally filters or aggregates them, and publishes a deterministic result in status.

The operator serves two roles:

- cluster administrators define the maximum observable scope at installation time;
- users define `Kubeseer` resources that may narrow, but never broaden, that scope.

The first certified vertical slice reads one typed field from one authorized Kubernetes resource and updates status only when the semantic result changes.

## Terminology

- **Kubeseer resource**: an instance of the `Kubeseer` Custom Resource configured by a user.
- **Source**: a declaration that identifies a Kubernetes type and selects concrete resource instances.
- **Installation access policy**: administrator-owned configuration defining the maximum namespaces and resource types the operator may observe.
- **Effective observation scope**: the intersection of the installation access policy and the narrower scope requested by a Kubeseer resource.
- **Extracted value**: a native Kubernetes value returned by a supported JSONPath expression before conversion.
- **Typed output**: a converted value whose logical type is preserved in Kubeseer status.
- **Provenance**: the API version, kind, namespace, name, and UID of a contributing resource.
- **Semantic result**: the normalized, deterministically ordered output used to decide whether status has meaningfully changed.
- **Partial or degraded result**: a result produced from successful sources while one or more other sources failed under an explicitly approved policy.

## Technology And Compatibility Baseline

- Implementation language: Go.
- Operator framework: Kubernetes controller-runtime and controller-tools conventions.
- Kubernetes API interaction: typed clients for owned APIs and dynamic/discovery clients for observed built-in and custom resource types.
- Initial extraction language: a declared and validated JSONPath subset. CEL and arbitrary scripting are out of scope for the first version.
- Local Kubernetes provider: kind using Podman as the container engine.
- API maturity: pre-stable APIs use Kubernetes versioning conventions and begin in an alpha version selected by the `kubeseer-api-foundation` feature.
- Minimum supported Kubernetes version: not yet selected. The `kubeseer-api-foundation` requirements must make this an explicit, reviewed compatibility decision before implementation begins.
- Go, controller-runtime, controller-tools, kind node image, kubectl, and Podman versions must be pinned or centrally declared when the project toolchain is bootstrapped.

No feature may claim compatibility with an unrecorded version. Version changes that alter generated APIs, supported Kubernetes releases, or controller behavior require an approved specification update.

## Licensing And Copyright

- Kubeseer-authored source code and generated code SHALL be distributed under the Apache License 2.0.
- Unless a file or an approved specification explicitly states another applicable copyright holder or licensing obligation, copyright notices for Kubeseer-authored code SHALL identify Alessandro Rontani.
- New source files and generated artifacts SHALL carry the Apache 2.0 notice where a copyright header or license metadata is customary.
- Third-party dependencies and copied material SHALL retain their upstream copyright notices and licenses; this constitution does not relicense them.

## Architecture Rules

- Features are externally verifiable capabilities, not individual functions and not the whole product.
- Resource discovery, selection, extraction, typing, operators, aggregation, authorization, reconciliation, and status remain explicit boundaries.
- The installation access policy is administrative configuration separate from each Kubeseer resource.
- A Kubeseer resource may narrow the installation policy but may never broaden it.
- JSONPath evaluation preserves native Kubernetes values until typed conversion.
- Operators are drawn from a controlled, validated set with explicit type compatibility.
- Cross-namespace results preserve provenance and use deterministic ordering.
- Status follows Kubernetes API conventions, including `observedGeneration`, conditions, stable reason codes, and human-readable messages.
- Status is updated only when the semantic result or observable condition state changes.
- Reconciliation is idempotent and must not loop because of the controller's own status writes.
- Admission validation handles structural and early validation; runtime authorization remains mandatory before every read.
- Examples exercise the normal policy and authorization paths rather than bypassing them.

## Go And Controller Conventions

- Follow standard Go package naming, formatting, error wrapping, and table-driven testing conventions.
- Keep API types, generated artifacts, controllers, policy evaluation, extraction, typing, and aggregation in separable packages with narrow interfaces.
- Treat generated code and manifests as reproducible build outputs; generation checks must detect committed drift.
- Use Kubernetes-native types and semantics where they are part of the public contract, including `metav1.Condition`, quantities, durations, selectors, and API errors.
- Use controller-runtime clients, caches, watches, predicates, and reconciliation results deliberately; direct API reads require a documented reason.
- Pass `context.Context` through Kubernetes and reconciliation operations.
- Use structured logging keyed by the relevant Kubeseer namespace/name and source identifier; do not log extracted sensitive values by default.
- Tests must not depend primarily on arbitrary sleeps. Use readiness checks, bounded polling, and deterministic assertions.
- Cross-module integration is the lowest required automated test layer once a real production collaboration path exists; local envtest is the bootstrap layer before that point.
- Do not maintain a dedicated unit-test layer. Existing package-local unit tests are transitional and may be removed only after their required observable behavior passes at a genuine cross-module, envtest, or end-to-end layer.

## Kubernetes API Conventions

- CRDs must be structural and installable through the supported packaging path.
- Public fields, condition types, reason codes, and serialization formats require compatibility review before change.
- Source and field identifiers are stable and unique within their declared scope.
- Namespaced and cluster-scoped resources are handled according to discovery data, never assumption.
- Absent, null, empty, invalid, and unavailable values remain distinguishable where the public contract requires it.
- Results that combine resources with equal names in different namespaces remain unambiguous.
- Output cardinality and status size are bounded; limit handling is deterministic and observable.

## Standard Repository Commands

The project toolchain must expose these stable entry points as implementation is introduced:

```bash
make build       # compile the controller
make test        # run the default local higher-layer suite
make test-integration # run real cross-module collaboration scenarios
make test-api    # run local Kubernetes API scenarios with pinned envtest assets
make test-compatibility # run API scenarios across supported Kubernetes versions
make lint        # run static analysis and formatting checks
make generate    # regenerate Go API code
make manifests   # regenerate CRDs and RBAC manifests
make verify      # assert generated files, formatting, and manifests are current
make e2e         # run cluster-level scenarios
make local-up    # create the project-owned kind-on-Podman environment
make local-down  # remove only the project-owned local environment
```

Until a command is implemented by an approved task, its absence is expected and must not be represented as passing evidence. Walden task proofs use direct, read-only commands wherever possible and must assert that the intended tests actually ran.

## Key Files And Directories

- `SPECIFICATIONS.md`: product vision, feature boundaries, dependencies, and delivery order.
- `README.md`: contributor-facing product overview and project status.
- `.walden/constitution.md`: stable cross-feature rules.
- `.walden/environment.md`: evidence environment probes.
- `.walden/lessons.md`: reusable workflow lessons.
- `.walden/specs/`: independently gated feature specifications.
- `api/`: Kubeseer API types and generated deep-copy code once introduced.
- `internal/`: non-public controller and domain packages once introduced.
- `config/`: CRDs, RBAC, manager, webhook, and installation manifests once introduced.
- `test/integration/`: in-process scenarios spanning real Kubeseer modules once a production collaboration path exists.
- `test/envtest/`: cross-module Kubernetes API scenarios using a disposable local control plane once introduced.
- `test/e2e/`: executable cluster-level scenarios once introduced.
- `examples/`: self-contained, documented examples once introduced.

## Feature Portfolio And Delivery Order

The canonical feature set is the 18 directories named in `SPECIFICATIONS.md`. The recommended order is:

1. Minimum vertical slice: `kubeseer-api-foundation`, `integration-testing-foundation`, `resource-discovery`, `installation-access-policy`, `resource-selection`, `field-extraction`, `typed-output-model`, `reconciliation-runtime`, `status-and-conditions`, `authorization-enforcement`.
2. Filtering and aggregation: `value-operators`, `cross-namespace-aggregation`, `admission-validation`.
3. Production readiness and experimentation: `observability`, `performance-and-limits`, `packaging-and-installation`, `end-to-end-scenarios`, `local-development-environment`.

Dependencies declared in `SPECIFICATIONS.md` constrain planning and execution. Scaffolding a dependent feature does not authorize drafting or implementation ahead of approved prerequisites.

## Non-Negotiable Security Rules

- The operator SHALL never read a namespace or resource type outside the installation access policy.
- Every source request SHALL be authorized before discovery results are used to read resource instances.
- Operator ServiceAccount permissions, logical policy permissions, and user permissions SHALL remain distinct in specifications and implementation.
- Broader ServiceAccount RBAC SHALL never substitute for logical policy enforcement.
- A policy restriction SHALL invalidate or remove results that are no longer authorized without exposing the protected data through status, events, logs, or errors.
- Users creating Kubeseer resources do not require direct read access to observed resources; this delegated observation model SHALL not enable privilege escalation.
- Diagnostics and logs SHALL exclude Secrets and sensitive extracted values by default.
- Local cleanup SHALL target only the Kubeseer-owned kind cluster and project-generated resources; it SHALL never remove unrelated Podman or Kubernetes state.
- Failures in one Kubeseer instance or source SHALL be isolated according to the approved error policy and SHALL not block unrelated instances.

## Specification Governance

- Every acceptance criterion uses EARS syntax and stable identifiers.
- Every design traces all requirement and non-functional requirement identifiers.
- Every leaf implementation task references acceptance criteria and design sections and carries executable proof.
- Requirements, design, and tasks require separate explicit approval; planning approval never authorizes implementation.
- Approved upstream changes make downstream documents stale and require reconciliation and renewed approval.
- Completed work is accepted only with current Walden evidence bound to the approved specification and code.
