# Kubeseer — Functional Specifications and Walden Feature Breakdown

## 1. Purpose

This document describes Kubeseer's functional architecture and divides the product into independent, verifiable, and approvable Walden features.

Each feature listed below must be implemented as a separate Walden directory:

```text
.walden/specs/<feature>/
  requirements.md
  design.md
  tasks.md
```

Every feature must use EARS acceptance criteria with stable identifiers, complete design coverage, and executable verification proofs attached to implementation tasks.

## 2. Product vision

Kubeseer is a Kubernetes operator that can:

- observe built-in and custom Kubernetes resources;
- select resources from one or more namespaces;
- extract values through JSONPath;
- convert extracted values into typed outputs;
- filter and aggregate results;
- publish the computed result in the status of a `Kubeseer` Custom Resource;
- restrict observable namespaces and resource types through an administrator-defined installation policy;
- provide a reproducible local environment for experimentation and examples without requiring an existing Kubernetes cluster.

A single `Kubeseer` resource may narrow the access scope granted by the installation, but it must never broaden it.

## 3. Initial architectural decisions

- JSONPath is the initial extraction mechanism.
- CEL is explicitly out of scope for the first version and may be introduced later.
- Available operators form a controlled and validated set.
- Results preserve the logical type of each value.
- Kubeseer supports aggregation across multiple namespaces.
- Administrators define observable namespaces and resource types during installation.
- Users creating `Kubeseer` resources do not need direct read permissions on observed resources.
- The operator must never read outside the scope allowed by the installation policy.
- Status updates must occur only when the computed result changes semantically.
- The supported local experimentation environment uses kind with Podman.
- Examples must be executable and suitable for both manual exploration and automated local verification.

## 4. Stable Walden project context

Cross-cutting and stable project information belongs in `.walden/constitution.md`.

The constitution should include at least:

- Kubeseer's purpose;
- project terminology;
- technology stack;
- minimum supported Kubernetes version;
- Go and controller-runtime conventions;
- Kubernetes API conventions;
- standard build, test, lint, and validation commands;
- the architectural decisions listed above;
- non-negotiable security rules.

## 5. Walden feature structure

```text
.walden/
  constitution.md
  environment.md
  specs/
    kubeseer-api-foundation/
    integration-testing-foundation/
    resource-discovery/
    installation-access-policy/
    resource-selection/
    field-extraction/
    typed-output-model/
    value-operators/
    cross-namespace-aggregation/
    reconciliation-runtime/
    status-and-conditions/
    authorization-enforcement/
    admission-validation/
    observability/
    performance-and-limits/
    packaging-and-installation/
    end-to-end-scenarios/
    local-development-environment/
```

---

# 6. Independent feature specifications

## 6.1 `kubeseer-api-foundation`

### Objective

Define the fundamental contract of the `Kubeseer` Custom Resource without introducing resource observation or aggregation logic.

### Includes

- API group, version, and kind;
- high-level `spec` structure;
- high-level `status` structure;
- naming rules and identifiers;
- API versioning strategy;
- required and optional fields;
- defaulting rules;
- future compatibility rules;
- `observedGeneration`;
- references to other configuration resources, where required.

### Excludes

- resource discovery;
- JSONPath evaluation;
- authorization;
- operators;
- aggregation;
- reconciler behavior.

### Verifiable outcome

- the CRD can be installed successfully;
- Kubernetes accepts valid `Kubeseer` manifests;
- Kubernetes rejects structurally invalid manifests;
- generated Go APIs compile and their API contracts pass against local envtest.

### Dependencies

None.

---

## 6.2 `integration-testing-foundation`

### Objective

Establish modular Go boundaries and a cloud-independent higher-layer testing
strategy before additional production collaboration paths are introduced.

### Includes

- cohesive, acyclic production package boundaries;
- narrow consumer-owned interfaces and explicit constructors;
- in-process integration between real Kubeseer modules once a production
  collaboration path exists;
- local Kubernetes API integration through pinned envtest assets;
- full-cluster verification through project-owned kind on Podman;
- deterministic test ownership, cleanup, and non-vacuous layer selection;
- envtest as the default bootstrap while no production collaboration path
  exists;
- migration and retirement of dedicated unit-test suites after equivalent
  higher-layer behavior is covered.

### Excludes

- feature-specific discovery, selection, extraction, authorization,
  reconciliation, or aggregation behavior;
- the complete end-to-end scenario catalog;
- cloud-provider Kubernetes certification;
- performance, soak, or chaos workloads.

### Verifiable outcome

- every supported test layer has a stable repository entry point;
- the default test command selects envtest during bootstrap and cross-module
  integration after the first real collaboration path exists;
- no default path reads an ambient kubeconfig or targets an external cluster;
- selected empty suites fail explicitly;
- dedicated unit-test entry points and suites are absent after their required
  behavior is migrated.

### Dependencies

- `kubeseer-api-foundation`.

Every subsequent feature that introduces or changes production behavior
inherits `integration-testing-foundation` as a cross-cutting prerequisite.

---

## 6.3 `resource-discovery`

### Objective

Dynamically resolve the Kubernetes resource types requested by Kubeseer sources.

### Includes

- resolution of `apiVersion` and `kind` into a `GroupVersionResource`;
- use of the Kubernetes discovery client;
- support for built-in resources;
- support for Custom Resource Definitions;
- distinction between namespaced and cluster-scoped resources;
- handling unavailable APIs;
- discovery cache refresh and invalidation;
- handling removal or modification of a CRD.

### Error behavior

- an unknown type must produce an error associated with the affected source;
- failure of one source must not necessarily interrupt processing of other sources;
- a cluster-scoped resource must never be treated as namespaced;
- discovery errors must be exposed clearly in status.

### Dependencies

- `kubeseer-api-foundation`.
- `integration-testing-foundation`.

---

## 6.4 `installation-access-policy`

### Objective

Allow an administrator to define the maximum observation scope available to the operator at installation time.

### Includes

- allowed namespaces;
- explicit namespace lists;
- all-namespaces mode;
- all-non-system-namespaces mode;
- explicit inclusions and exclusions;
- configurable identification of system namespaces;
- allowed API groups and kinds;
- allowed built-in and custom resources;
- optional access to cluster-scoped resources;
- policy validation;
- behavior when the policy is missing or invalid;
- relationship between the logical policy and effective Kubernetes RBAC.

### Example model

```yaml
apiVersion: kubeseer.io/v1alpha1
kind: KubeseerAccessPolicy
metadata:
  name: default
spec:
  namespaces:
    mode: AllNonSystem
    include: []
    exclude:
      - kube-system
      - kube-public
      - kube-node-lease
  resources:
    - apiGroups: [""]
      kinds: ["Pod", "Service", "ConfigMap"]
    - apiGroups: ["apps"]
      kinds: ["Deployment", "StatefulSet"]
    - apiGroups: ["example.io"]
      kinds: ["MyCustomResource"]
```

### Core rules

- the policy defines the maximum installation scope;
- a `Kubeseer` resource may narrow this scope but must not broaden it;
- an out-of-policy request must be rejected before reading the resource;
- administrative configuration must remain separate from individual `Kubeseer` configurations.

### Dependencies

- `kubeseer-api-foundation`;
- `resource-discovery`.

---

## 6.5 `resource-selection`

### Objective

Define how a Kubeseer source selects concrete Kubernetes resource instances.

### Includes

- selection by resource name;
- selection from a single namespace;
- selection from a namespace list;
- label selectors;
- optional field selectors;
- selection of all matching resources;
- combination rules for selection mechanisms;
- deterministic result ordering;
- behavior when no resource matches;
- stable source identifiers.

### Example model

```yaml
sources:
  - id: frontend-pods
    resource:
      apiVersion: v1
      kind: Pod
    namespaces:
      names:
        - frontend
        - shared-services
    selector:
      matchLabels:
        app: frontend
```

### Excludes

- JSONPath extraction;
- typed conversion;
- aggregation;
- installation-policy enforcement.

### Dependencies

- `resource-discovery`;
- `installation-access-policy`.

---

## 6.6 `field-extraction`

### Objective

Extract values from selected resources through a declared and validated JSONPath subset.

### Includes

- supported JSONPath syntax;
- expression parsing and validation;
- scalar extraction;
- list extraction;
- array and map access;
- multiple results;
- missing fields;
- null values;
- syntax errors;
- evaluation errors;
- optional caching of compiled expressions;
- intentional limitations compared with a complete JSONPath implementation.

### Example model

```yaml
fields:
  - name: phase
    path: "{.status.phase}"
  - name: node
    path: "{.spec.nodeName}"
```

### Core rules

- the native Kubernetes value must be preserved until typed conversion;
- invalid JSONPath expressions must be detected before processing resources;
- errors must identify the affected field and resource;
- the semantics of multiple results must be defined explicitly.

### Dependencies

- `resource-selection`.

---

## 6.7 `typed-output-model`

### Objective

Define an explicitly typed output model that can be serialized safely into Kubernetes status.

### Initial types

- `string`;
- `integer`;
- `number`;
- `boolean`;
- `timestamp`;
- `duration`;
- `quantity`;
- `object`;
- `list`.

### Includes

- internal type representation;
- YAML and JSON representation;
- allowed conversions;
- forbidden conversions;
- overflow handling;
- conversion error handling;
- optional preservation of the original value;
- duration and quantity normalization;
- distinction between absent, null, empty, and non-convertible values;
- per-field error representation.

### Example model

```yaml
fields:
  - name: availableReplicas
    path: "{.status.availableReplicas}"
    type: integer
  - name: memoryLimit
    path: "{.spec.template.spec.containers[0].resources.limits.memory}"
    type: quantity
```

```yaml
status:
  result:
    fields:
      availableReplicas:
        type: integer
        value: 3
      memoryLimit:
        type: quantity
        value: 512Mi
        normalizedValue: 536870912
```

### Dependencies

- `field-extraction`.

---

## 6.8 `value-operators`

### Objective

Define a controlled set of operators applicable to extracted and typed values.

### Suggested initial operators

#### Comparison

- `eq`;
- `ne`;
- `gt`;
- `gte`;
- `lt`;
- `lte`.

#### String

- `contains`;
- `startsWith`;
- `endsWith`;
- `matches`.

#### Presence

- `exists`;
- `notExists`.

#### Collections

- `in`;
- `notIn`.

#### Simple transformations

- `default`;
- `coalesce`.

### Includes

- operator-to-type compatibility matrix;
- comparison semantics;
- quantity and duration comparisons;
- behavior for null or absent values;
- allowed and forbidden coercions;
- operator application order;
- deterministic error messages;
- early validation of invalid combinations.

### Excludes

- aggregation functions;
- CEL;
- arbitrary transformations;
- scripting.

### Dependencies

- `field-extraction`;
- `typed-output-model`.

---

## 6.9 `cross-namespace-aggregation`

### Objective

Group and aggregate values from multiple resources and namespaces while retaining their provenance.

### Includes

- cross-namespace collection;
- provenance metadata;
- grouping by one or more fields;
- typed aggregations;
- partial results;
- deduplication;
- collisions between equal names in different namespaces;
- deterministic ordering;
- maximum cardinality;
- temporarily unavailable namespaces;
- degraded source behavior.

### Minimum value provenance

```yaml
source:
  apiVersion: apps/v1
  kind: Deployment
  namespace: team-a
  name: frontend
  uid: 9d8...
```

### Suggested initial aggregations

- `collect`;
- `count`;
- `sum`;
- `min`;
- `max`;
- `average`;
- `first`;
- `last`;
- `distinct`.

### Core rules

- numeric aggregations must accept only compatible types;
- processing order must not produce non-deterministic output;
- contributing resources must remain traceable when requested by configuration;
- source failure must produce either a degraded result or an error according to an explicit policy.

### Dependencies

- `resource-selection`;
- `field-extraction`;
- `typed-output-model`;
- `value-operators`.

---

## 6.10 `reconciliation-runtime`

### Objective

Define when and how the controller reconciles a `Kubeseer` resource.

### Includes

- watches on `Kubeseer` Custom Resources;
- watches on source resources;
- mapping changed resources to affected `Kubeseer` instances;
- periodic safety reconciliation;
- idempotency;
- retry and backoff;
- event debounce or coalescing;
- concurrent update handling;
- deletion behavior;
- finalizers only where necessary;
- caches and informers;
- result invalidation;
- semantic comparison before status updates.

### Core rules

- a change to an observed resource must reconcile affected `Kubeseer` instances;
- a change that does not alter the computed result must not trigger an unnecessary status update;
- the controller must not loop because of its own status updates;
- reconciliation must be repeatable and idempotent;
- failure of one `Kubeseer` instance must not block others.

### Dependencies

- all functional features required by the first implemented vertical slice.

---

## 6.11 `status-and-conditions`

### Objective

Define the public and observable status contract of a `Kubeseer` resource.

### Includes

- `observedGeneration`;
- Kubernetes-style conditions;
- overall state;
- computed result;
- processed-source summary;
- partial errors;
- timestamps;
- semantic result hash;
- ready, degraded, invalid, unauthorized, and unavailable states;
- stable reason codes and human-readable messages.

### Suggested conditions

- `Accepted`;
- `Authorized`;
- `SourcesResolved`;
- `Ready`;
- `Degraded`.

### Example model

```yaml
status:
  observedGeneration: 4
  conditions:
    - type: Ready
      status: "True"
      reason: EvaluationSucceeded
      message: Evaluated 12 resources from 3 namespaces
  summary:
    matchedResources: 12
    successfulSources: 3
    failedSources: 0
  result: {}
```

### Dependencies

- `typed-output-model`;
- `cross-namespace-aggregation` where aggregation is enabled;
- `reconciliation-runtime`.

---

## 6.12 `authorization-enforcement`

### Objective

Enforce the administrator-defined installation policy at runtime.

### Includes

- validation of every requested namespace;
- validation of every requested API group and kind;
- prevention of privilege escalation;
- behavior when the installation policy is restricted;
- invalidation or removal of previously authorized results;
- prevention of data exposure through error messages;
- auditable authorization decisions;
- separation between user permissions and operator permissions.

### Core rule

```text
IF a Kubeseer source requests a namespace or resource type that is not allowed
by the installation policy, THEN the system SHALL reject that source without
attempting to read the requested resource.
```

### Dependencies

- `installation-access-policy`;
- `resource-selection`.

---

## 6.13 `admission-validation`

### Objective

Reject invalid Kubeseer configurations as early and clearly as possible.

### Includes

- CRD OpenAPI validation;
- validating admission webhook where dynamic checks are required;
- JSONPath validation;
- type-and-operator compatibility validation;
- unique source and field identifiers;
- valid references;
- installation-policy compliance checks;
- cardinality and size limits;
- actionable validation messages.

### Validation layers

1. CRD/OpenAPI validation for structural constraints.
2. Admission webhook or controller validation for discovery-dependent, JSONPath, and authorization checks.

### Dependencies

- all declarative model features.

---

## 6.14 `observability`

### Objective

Make Kubeseer's runtime behavior measurable, diagnosable, and auditable.

### Includes

- structured logs;
- correlation with the relevant `Kubeseer` resource;
- Prometheus metrics;
- Kubernetes Events;
- optional tracing;
- protection of sensitive values in logs;
- stable error and reason codes.

### Suggested metrics

- total reconciliations;
- failed reconciliations;
- reconciliation duration;
- resources read;
- failed sources;
- results produced;
- skipped status updates;
- authorization failures;
- JSONPath failures.

### Dependencies

- `reconciliation-runtime`;
- `status-and-conditions`.

---

## 6.15 `performance-and-limits`

### Objective

Define operational limits and predictable behavior on large clusters or expensive configurations.

### Includes

- maximum sources per `Kubeseer`;
- maximum namespaces per source;
- maximum matched resources;
- maximum status output size;
- caching strategy;
- controller concurrency;
- evaluation timeout;
- memory constraints;
- behavior on large clusters;
- protection against excessively expensive configurations.

### Core rules

```text
IF the number of matching resources exceeds the configured limit,
THEN the system SHALL stop evaluation deterministically and report the limit.

WHILE multiple Kubeseer instances share the same source, the system SHALL avoid
duplicate operations where caching does not affect correctness or isolation.
```

### Dependencies

- `reconciliation-runtime`;
- `cross-namespace-aggregation`.

---

## 6.16 `packaging-and-installation`

### Objective

Define how Kubeseer is packaged, installed, upgraded, configured, and removed.

### Includes

- operator manifests;
- Helm or Kustomize packaging;
- CRDs;
- ServiceAccount;
- ClusterRoles and Roles;
- RoleBindings and ClusterRoleBindings;
- initial access-policy configuration;
- installation for selected namespaces;
- upgrade strategy;
- uninstall behavior;
- container images;
- webhook certificates and cert-manager integration where applicable.

### Required permission model

The specification must distinguish clearly between:

- permissions granted to the operator ServiceAccount;
- resources allowed by `KubeseerAccessPolicy`;
- permissions required by users who create `Kubeseer` resources.

Effective RBAC and the logical access policy should be aligned as closely as practical. The logical policy remains mandatory even when the ServiceAccount has broader permissions for operational reasons.

### Dependencies

- `installation-access-policy`;
- `authorization-enforcement`;
- `admission-validation`.

---

## 6.17 `end-to-end-scenarios`

### Objective

Certify the integration of all implemented features through executable cluster-level scenarios.

### Minimum scenarios

1. read a Deployment in the same namespace;
2. read Pods from multiple namespaces;
3. read a Custom Resource;
4. select resources through labels;
5. extract a scalar JSONPath value;
6. extract a list;
7. convert values into declared types;
8. perform a numeric aggregation;
9. produce a partially degraded result;
10. reject a forbidden namespace;
11. reject a forbidden kind;
12. handle a missing resource type or CRD;
13. react to a source-resource update;
14. avoid a status update when the semantic result is unchanged;
15. handle source-resource deletion;
16. react to a restriction of the access policy;
17. recover after an operator restart;
18. reject or truncate oversized output according to policy;
19. delete a `Kubeseer` resource cleanly;
20. process overlapping `Kubeseer` instances correctly.

### Suggested verification environment

```text
kind create cluster
make install
make deploy
kubectl apply -f test/e2e/fixtures
go test ./test/e2e/...
```

### Dependencies

- every feature included in the certified release scope.

---

## 6.18 `local-development-environment`

### Objective

Provide a reproducible local environment that allows contributors and users to install, explore, demonstrate, and test Kubeseer without access to an existing Kubernetes cluster.

The initial supported environment uses kind as the Kubernetes provider and Podman as the container engine.

### Includes

- a version-controlled kind cluster configuration;
- explicit configuration of kind to use Podman rather than Docker;
- prerequisite checks for Podman, kind, kubectl, and the project build tools;
- creation and safe deletion of a project-owned local cluster;
- local building of the Kubeseer controller image with Podman;
- loading the locally built image into the kind nodes without requiring an external registry;
- installation of CRDs, the controller, RBAC resources, and a development access policy;
- readiness checks and bounded waits;
- commands for setup, deployment, testing, diagnostics, and cleanup;
- local smoke and end-to-end tests;
- reusable test entry points suitable for continuous integration;
- clear documentation of supported host platforms and known Podman or kind limitations.

### Examples directory

The project must provide a top-level `examples/` directory containing executable, documented scenarios.

Each example must include:

- a concise purpose statement;
- all required Kubernetes workload manifests;
- one or more `Kubeseer` manifests;
- the expected result or status conditions;
- commands to apply, inspect, verify, and remove the example;
- an automated assertion or verification command where practical.

The initial example set should cover at least:

1. observation of a built-in Kubernetes resource;
2. typed JSONPath field extraction;
3. filtering or value-operator behavior;
4. aggregation across two namespaces;
5. observation of a lightweight example Custom Resource;
6. rejection of a source outside the installation access policy;
7. degraded or partial-result behavior caused by an invalid or unavailable source.

### Core rules

- the local workflow must not require the user to already have a Kubernetes cluster;
- the local environment must exercise the normal Kubeseer authorization and policy-enforcement paths rather than bypassing them;
- repeated setup and cleanup operations must be idempotent or produce actionable state messages;
- tests must inspect structured Kubernetes API output and must return a non-zero status when verification fails;
- synchronization must use readiness checks and bounded waits rather than relying primarily on arbitrary sleep durations;
- cleanup must target only the Kubeseer kind cluster and project-generated resources;
- cleanup must never remove unrelated Podman containers, images, networks, volumes, or Kubernetes clusters;
- diagnostics must avoid collecting Secrets or sensitive values by default;
- versions of environment-critical dependencies, including the kind node image, must be pinned or centrally defined.

### Verifiable outcome

A user on a supported host can follow one documented workflow to:

1. validate prerequisites;
2. create a kind cluster using Podman;
3. build and load Kubeseer locally;
4. install the operator and its development access policy;
5. deploy and verify one or more bundled examples;
6. run local smoke and end-to-end tests;
7. collect actionable diagnostics after a failure;
8. delete the environment safely.

### Dependencies

- `packaging-and-installation`;
- `end-to-end-scenarios`;
- all Kubeseer capabilities exercised by the bundled examples.

---

# 7. Recommended implementation order

## Phase 1 — Minimum vertical slice

```text
1. kubeseer-api-foundation
2. integration-testing-foundation
3. resource-discovery
4. installation-access-policy
5. resource-selection
6. field-extraction
7. typed-output-model
8. reconciliation-runtime
9. status-and-conditions
10. authorization-enforcement
```

Expected result: Kubeseer can read a typed field from an authorized Kubernetes resource and publish a stable result in status.

## Phase 2 — Filtering and aggregation

```text
11. value-operators
12. cross-namespace-aggregation
13. admission-validation
```

Expected result: Kubeseer can filter, group, and aggregate typed values across multiple namespaces.

## Phase 3 — Production readiness and experimentation

```text
14. observability
15. performance-and-limits
16. packaging-and-installation
17. end-to-end-scenarios
18. local-development-environment
```

Expected result: Kubeseer is deployable, measurable, bounded, certifiable, and easy to explore locally without an existing Kubernetes cluster.

---

# 8. Dependency overview

```text
kubeseer-api-foundation
└── integration-testing-foundation
    └── required by every later feature that changes production behavior

kubeseer-api-foundation
├── resource-discovery
│   ├── installation-access-policy
│   │   └── authorization-enforcement
│   └── resource-selection
│       ├── field-extraction
│       │   └── typed-output-model
│       │       ├── value-operators
│       │       └── cross-namespace-aggregation
│       └── authorization-enforcement
│
├── reconciliation-runtime
│   └── status-and-conditions
│
├── admission-validation
├── observability
├── performance-and-limits
└── packaging-and-installation

end-to-end-scenarios
└── depends on every feature in the certified release scope

local-development-environment
├── packaging-and-installation
├── end-to-end-scenarios
└── depends on the capabilities demonstrated by its examples
```

# 9. Feature granularity rule

A Walden feature should represent an externally verifiable capability.

Good feature examples:

- `field-extraction`;
- `typed-output-model`;
- `authorization-enforcement`.

Too small:

- `implement-jsonpath-parser-function`;
- `add-status-struct-field`.

These belong in `tasks.md` as implementation tasks.

Too large:

- `implement-kubeseer`;
- `build-the-operator`.

These prevent focused requirements, design review, traceability, and reliable proof coverage.

# 10. Recommended first certified slice

The first implementation milestone should prove the following end-to-end behavior:

1. an administrator installs Kubeseer with one allowed namespace and one allowed resource type;
2. a user creates a `Kubeseer` resource without direct access to the observed resource;
3. the operator validates the request against the installation policy;
4. the operator resolves the requested Kubernetes type;
5. the operator reads one matching resource;
6. the operator extracts one JSONPath field;
7. the operator converts the value to the declared type;
8. the operator publishes the typed value in status;
9. the operator does not update status when the semantic result is unchanged.

Selectors spanning many resources, advanced operators, and advanced aggregations should be introduced only after this slice has been certified.

# 11. Corrective features from project review

The September 5, 2026 review identified four actionable issues. These three
follow-up features supplement the original 18-feature baseline. Each follows
its own Requirements → Design → Tasks approval chain. Existing baseline
approvals do not approve these corrections.

| Priority / finding | Corrective feature | Observable outcome | Baseline dependencies |
| --- | --- | --- | --- |
| P1 / 1 | [configuration-budget-status-invalidation](.walden/specs/configuration-budget-status-invalidation/requirements.md) | Rejected persisted configurations lose old results through guarded status publication, including after policy removal or restriction. | performance-and-limits, reconciliation-runtime, status-and-conditions, authorization-enforcement |
| P1 / 2 | [watch-startup-cancellation](.walden/specs/watch-startup-cancellation/requirements.md) | Stalled WATCH establishment respects evaluation deadlines and lease cancellation while preserving other authorized owners. | reconciliation-runtime, performance-and-limits, authorization-enforcement |
| P2 / 3 and 4 | [native-scalar-conversion-compatibility](.walden/specs/native-scalar-conversion-compatibility/requirements.md) | Exact native quantity and duration inputs, including nano/micro quantities and zero/microsecond durations, convert successfully. | typed-output-model, field-extraction, kubeseer-api-foundation |

All three also depend on integration-testing-foundation. Recommended priority
is the table order; the corrective features do not depend on each other.
Their Requirements documents record reproduction cases, failure handling,
compatibility constraints, and verification expectations. Requirements and
Designs are approved. Each Tasks plan contains four implementation/test or
documentation leaf tasks and is prepared for review; implementation requires
approved Tasks and an explicit execution request.

The configuration-budget correction deliberately changes the old runtime
test expectation of no status publication on budget failure. The corrective
Designs and Tasks identify the affected baseline design/proof reconciliation
and review gates required before execution. The two runtime corrections share
one non-waiting route-promotion helper, which is implemented or reused once.

# 12. Local cluster resume follow-up

A September 23, 2026 local-development incident showed that an owned kind
control-plane container can remain exited after host shutdown. The next
`make local-up` then stops at API identity validation before it can converge
the existing cluster.

| Corrective feature | Observable outcome | Baseline dependency |
| --- | --- | --- |
| [local-cluster-resume](.walden/specs/local-cluster-resume/requirements.md) | An explicit `make local-up` resumes only the exited, owned kind node, waits for the API, and continues normal convergence without replacing the cluster. | local-development-environment |

This follow-up has its own Requirements, Design, Tasks, and execution gates.
Host-level Podman restart policy and systemd changes remain outside its scope.
