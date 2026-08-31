---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T11:23:51Z
last_modified: 2026-08-31T11:23:51Z
approved_fingerprint: sha256:1de1e7d444390d4970bc924e74f5f3e34ba03c7c053712f1d03e72560bef69a6
source_requirements_approved_at: 2026-08-31T10:59:27Z
source_requirements_fingerprint: sha256:c75b59a2d2bdee9a4fb2c51cebae22a48df228d732b744457015ec3312ef2bf4
---

# Admission Validation Design

## Overview

Admission validation is implemented as one side-effect-free orchestration
boundary over the project's existing declarative planners, discovery resolver,
and installation-policy evaluator. Structural OpenAPI rejects malformed shape
and most cardinality violations in the API server. A validating webhook then
performs bounded serialized-size checks, cross-field semantic planning,
current discovery, and exact-target policy evaluation before persistence.

The webhook handles CREATE and UPDATE of the `Kubeseer` and
`KubeseerAccessPolicy` main resources. Its registration deliberately does not
match status subresources or DELETE. Dry-run requests traverse the same handler
because the handler performs no writes. The registration explicitly uses
`failurePolicy: Fail`, `sideEffects: None`, `timeoutSeconds: 10`, and
`admissionReviewVersions: [v1]`, following the Kubernetes
[dynamic admission contract](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
and the
[`admissionregistration.k8s.io/v1` API](https://kubernetes.io/docs/reference/kubernetes-api/admissionregistration/validating-webhook-configuration-v1/).

Admission returns no durable state, discovery proof, or authorization
capability. Reconciliation continues to compile the persisted generation,
resolve current discovery, and obtain current exact-target authorization before
resource-instance I/O.

## Architecture

The request path is intentionally staged so bounded deterministic defects are
reported before any cluster-dependent work:

```text
Kubernetes API request
        |
        v
structural CRD OpenAPI
        |
        v
typed AdmissionReview v1 adapter
        |
        v
configuration budgets ------------------------> 422 Invalid
        |
        v
shared pure semantic planners ----------------> 422 Invalid
        |
        v
selection planner + current discovery --------> 422 Invalid / 503 Unavailable
        |
        v
fresh installation policy snapshot
        |
        +---- exact-target evaluation --------> 403 Forbidden / 503 Unavailable
        |
        v
allow without mutation or reusable capability
```

`KubeseerAccessPolicy` requests use the same adapter and response model but a
shorter pipeline: structural schema, budgets, and the shared policy compiler.
They never inspect existing `Kubeseer` objects, so a valid narrowing update is
not blocked. DELETE is not registered and existing runtime fail-closed behavior
handles a subsequently missing policy.

Every stage accumulates independently detectable sibling defects and sorts
them by submitted field path and stable reason. A stage with deterministic
invalid defects stops later dynamic stages to avoid cascading errors and to
guarantee that over-budget input causes no discovery or policy I/O. Across
sources, deterministic invalidity takes precedence over a concurrent transient
discovery failure; both detected causes are retained in the response, but the
top-level class is `Invalid`. Policy evaluation starts only when every source
has a complete selection plan.

## Options Considered

### Option A: Staged Adapter Over Shared Planners

- Summary: Add one `internal/admission` package that sequences budget checks,
  existing planner contracts, discovery-backed selection, and the current
  policy snapshot. Minimally extend shared planners that currently expose only
  their first error so admission can retrieve all sorted declaration failures.
- Why chosen: It keeps parser, type, operator, aggregate, discovery, and policy
  semantics shared with runtime while giving admission one place to own field
  paths, response classification, ordering, and the no-I/O-before-budgets rule.
  Existing single-error APIs remain compatibility wrappers over the expanded
  validation results.

### Option B: Independent Webhook Validator

- Summary: Reimplement selector, JSONPath, operand, conversion, aggregation,
  and policy rules directly in webhook code.
- Why rejected: It is initially straightforward but creates a second semantic
  implementation that can drift from runtime. Every future declarative change
  would require synchronized fixes and duplicate proofs, directly weakening
  the runtime/admission equivalence requirement.

### Option C: OpenAPI Or CEL Only

- Summary: Express every rule in generated CRD schema or admission CEL and do
  not run a webhook.
- Why rejected: OpenAPI cannot resolve a GroupVersionKind through current
  discovery, derive exact namespace targets, load the installation policy, or
  reuse the approved Go planners. CEL would also duplicate the parser and
  compatibility contracts and is outside the approved expression boundary.

## Simplicity And Elegance Review

- Simplest viable shape: One orchestration package, two typed handlers, one
  response model, and one webhook-configuration builder are sufficient. There
  is no admission controller, cache, persistence layer, background worker,
  authorization token, or new public API.
- Coupling check: `internal/admission` consumes narrow planner, discovery, and
  policy interfaces. Domain packages do not import admission. API packages
  remain serialization-only and cannot form a dependency cycle with runtime
  packages.
- Multi-error challenge: A common issue type is justified by the requirement
  to report all defects with stable paths and classes. It remains internal and
  specific to these two resources rather than becoming a generic validation
  framework.
- Dynamic-work challenge: Sequential source planning is preferred over a new
  worker pool. At most 32 sources are admitted, the shared resolver owns its
  discovery behavior, ordering stays obvious, and request cancellation is
  propagated directly.
- Future-proofing: Production Service selection, CA injection, certificate
  rotation, deployment lifecycle, operational telemetry, runtime output
  limits, and additional API versions remain deferred to their named features.
  Adding another served version requires explicit review of `matchPolicy` and
  conversion behavior.

## Components And Interfaces

### Structural API Schema And Budget Constants

- Purpose: Make all public fields structural and reject required-field, enum,
  pattern, range, list-map/set uniqueness, and expressible cardinality defects
  in the API server.
- Inputs/Outputs: Kubebuilder markers on `api/v1alpha1` types generate the two
  CRD schemas. Maximum items/properties markers implement the approved source,
  namespace, field, operator, operand, aggregate, group, selector, and policy
  limits. A shared internal `Limits` value owns both 262144-byte spec ceilings
  and defensive cardinality checks used by the webhook.
- Dependencies: `controller-gen`; generated CRDs under `config/crd/bases`.
- Constraints: No `x-kubernetes-preserve-unknown-fields` region is introduced.
  Generated item schemas and list extensions are inspected, not inferred from
  source markers alone. The canonical size is computed by `encoding/json` over
  the typed `Spec` only; metadata, status, and AdmissionReview envelope bytes
  do not count.
- Requirements: `R1`, `R2`, `R3`, `NFR2`, `NFR4`, `NFR8`.

### Shared Declarative Validation Results

- Purpose: Expose all deterministic declaration failures from the same code
  that creates runtime plans.
- Inputs/Outputs: Selection/static parsing, extraction, typed output,
  operators, aggregation, and access-policy packages expose validation or plan
  outcomes containing stable domain reason, source/field/declaration identity,
  and sanitized message. Where an existing public internal function returns
  one error, it remains behavior-compatible and returns the first sorted issue.
- Dependencies: `internal/selection`, `internal/extraction`,
  `internal/typedoutput`, `internal/operators`, `internal/aggregation`, and
  `internal/accesspolicy`.
- Constraints: The adapters may add coordinates needed for field-path mapping,
  but do not define a second parser or compatibility table. Planning never
  mutates the proposed object; therefore omitted average precision and rounding
  remain omitted even though the aggregate plan computes approved defaults.
- Requirements: `R3`, `R4`, `R6`, `R7`, `R8`, `NFR3`, `NFR5`, `NFR8`.

### Admission Validator

- Purpose: Sequence validation stages and turn domain outcomes into one sorted,
  sanitized result.
- Proposed contract:

  ```go
  type Class string

  const (
      Invalid     Class = "Invalid"
      Forbidden   Class = "Forbidden"
      Unavailable Class = "ValidationUnavailable"
  )

  type Issue struct {
      Path    string
      Reason  string
      Message string
      Class   Class
  }

  type Result struct {
      Issues []Issue
  }

  type Validator struct { /* planner, policy source, and limits */ }

  func (v *Validator) ValidateKubeseer(
      context.Context, *v1alpha1.Kubeseer,
  ) Result
  func (v *Validator) ValidateAccessPolicy(
      context.Context, *v1alpha1.KubeseerAccessPolicy,
  ) Result
  ```

- Inputs/Outputs: Typed proposed objects in; an allowed empty result or sorted
  issues out. Paths use submitted indexes, for example
  `spec.sources[2].fields[1].operators[0].value`. Duplicate identities retain
  separate index paths. Sorting is lexical path, then stable reason, then
  message as a final deterministic tie-breaker.
- Dependencies: Pure budget/semantic validators, `selection.Planner`, and
  `accesspolicy.PolicySource`.
- Constraints: It performs no resource LIST/GET, namespace existence check,
  SubjectAccessReview, user impersonation, status write, or logging of submitted
  operands. It loads the canonical policy once per valid `Kubeseer` request,
  including a request whose explicit-empty namespaces produce no targets.
- Requirements: `R1`, `R2`, `R3`, `R4`, `R5`, `R6`, `R7`, `R8`, `NFR1`,
  `NFR3`, `NFR4`, `NFR5`, `NFR6`.

### Discovery And Exact-Target Policy Adapters

- Purpose: Resolve each declared type and evaluate the exact target set without
  reading resource instances.
- Inputs/Outputs: `selection.Planner.Plan` receives the proposed object's
  namespace and one source. Its `SelectionPlan.Targets()` supplies discovered
  GVR, Kind, scope, and exact namespace to policy requests. The validator loads
  one fresh `accesspolicy.Snapshot` from the canonical policy source and calls
  its pure evaluator for every target.
- Dependencies: Current discovery resolver and
  `accesspolicy.NewClientPolicySource(client.Reader)`.
- Classification: Unknown served identities and scope incompatibility are
  invalid; transient discovery failures are unavailable. Policy missing or
  invalid is forbidden, policy load failure is unavailable, and target denial
  is forbidden. Only sanitized stable domain messages cross the boundary.
- Constraints: No `authorization.Capability` is created. Explicit-empty
  namespaces produce zero target evaluations; omitted namespaces produce the
  owner namespace. Cluster-scoped sources produce one cluster target.
- Requirements: `R5`, `R6`, `R7`, `R8`, `NFR1`, `NFR3`, `NFR5`, `NFR6`.

### AdmissionReview Adapter And Registration Contract

- Purpose: Decode the two v1alpha1 types, invoke the validator, and emit a
  Kubernetes-native response.
- Inputs/Outputs: Two controller-runtime generic validating handlers are
  registered at stable paths:
  `/validate-kubeseer-io-v1alpha1-kubeseer` and
  `/validate-kubeseer-io-v1alpha1-kubeseeraccesspolicy`.
  CREATE validates the new object; UPDATE validates only the proposed object;
  defensive DELETE methods always allow.
- Registration: A builder returns one
  `admissionregistration.k8s.io/v1` `ValidatingWebhookConfiguration` with
  separate entries for namespaced `kubeseers` and cluster-scoped
  `kubeseeraccesspolicies`. Rules use only CREATE and UPDATE, exact main
  resources, `matchPolicy: Exact`, `failurePolicy: Fail`, `sideEffects: None`,
  `timeoutSeconds: 10`, and `admissionReviewVersions: [v1]`. The caller supplies
  the `WebhookClientConfig`; envtest uses a URL and CA bundle, while packaging
  later supplies Service wiring and certificate lifecycle.
- Response mapping: The adapter creates a `metav1.Status` with all issues in
  `details.causes`. Invalid uses `StatusReasonInvalid`/422, forbidden uses
  `StatusReasonForbidden`/403, and unavailable uses
  `StatusReasonServiceUnavailable`/503. Each cause carries path, concise
  message, and its stable reason. The top-level message summarizes counts and
  contains no raw wrapped error.
- Requirements: `R1`, `R6`, `R7`, `NFR1`, `NFR2`, `NFR3`, `NFR5`, `NFR6`,
  `NFR8`.

### Local Webhook Test Harness

- Purpose: Prove the complete API-server-to-TLS-webhook path without defining
  production installation ownership.
- Inputs/Outputs: Envtest starts a controller-runtime webhook server with
  envtest-issued serving material, installs CRDs, and installs the generated
  registration contract with its URL and CA bundle. Readiness is checked with
  bounded polling before API assertions.
- Dependencies: Existing pinned envtest assets and compatibility matrix.
- Requirements: `R1`, `R2`, `R5`, `R6`, `R7`, `R8`, `NFR2`, `NFR6`, `NFR7`,
  `NFR8`.

## Validation Flow

### Kubeseer CREATE And UPDATE

1. Decode the proposed typed object and retain original declaration indexes for
   field-path mapping. UPDATE intentionally does not validate the old spec.
2. Marshal `Spec` canonically and check every cardinality and byte budget. Run
   all cheap checks even though structural OpenAPI normally rejects the same
   cardinality defects. If any fail, return every budget issue and perform no
   discovery or policy operation.
3. Validate source identity uniqueness, Kubernetes group/version and Kind
   syntax, namespace/name/selector syntax, field identity/type/JSONPath,
   operators, and aggregations through shared validation/planning contracts.
   Collect independent sibling issues; skip a dependent planner when its input
   declaration is already invalid. If any issue exists, return 422.
4. Run discovery-backed selection for every otherwise valid source. Collect
   unknown-resource, scope, and unavailable outcomes. If a deterministic
   invalid issue exists, return 422; otherwise, if discovery was unavailable,
   return 503. No resource instances are read.
5. Load a fresh canonical installation policy. Missing and invalid snapshots
   return 403; unavailable returns 503.
6. Evaluate every exact selection target through the pure policy snapshot and
   collect all denials. Any denial returns 403 for the complete object.
7. Return allowed without changing the request or retaining any plan or policy
   result.

### KubeseerAccessPolicy CREATE And UPDATE

1. Decode the proposed policy, check its spec byte budget and all policy
   cardinality budgets, and stop before compilation if exceeded.
2. Run the shared policy validation contract and collect every semantic issue.
   The singleton name, namespace modes, namespace sets, API groups, Kinds, and
   cross-field rules remain owned by the policy package.
3. Return 422 for any issue or allow the request. Do not load the current policy
   and do not list or reconcile existing Kubeseer resources.

## Data Models

No API field or persistent object is added. `Issue`, `Result`, `Limits`, and
the registration builder are in-process contracts only.

The default admission limits are immutable implementation constants matching
`R2`: 32 sources; 64 namespaces, fields, label expressions, expression values,
and exact labels; 16 operators and group fields; 128 operator values; 32
aggregations; 256 policy namespaces; 128 policy resource rules; 64 API groups
and Kinds per rule; and 262144 canonical JSON bytes for either spec. Tests use
the same exported/read-only limits value; production callers do not configure
these values in this feature.

The registration builder accepts endpoint material rather than owning it:

```go
func WebhookConfiguration(
    clientConfig admissionregistrationv1.WebhookClientConfig,
) *admissionregistrationv1.ValidatingWebhookConfiguration

func Register(
    server webhook.Server,
    scheme *runtime.Scheme,
    validator *Validator,
)
```

This is the installable contract for local and future packaging composition.
It avoids committing a fictitious production namespace, Service, or CA bundle.

## Error Handling

- Malformed shape normally fails structural CRD validation before webhook
  invocation. Decode failures are returned as Kubernetes BadRequest without
  echoing object content.
- `Issue.Message` is selected from fixed corrective templates. Submitted field
  paths and non-sensitive identifiers may appear; operand payloads, Secret data,
  observed objects, URLs from downstream bodies, and wrapped errors do not.
- Context cancellation and deadline errors from discovery or policy loading map
  to `ValidationUnavailable`; the handler does not retry inside the admission
  request.
- The response classification precedence is `Invalid`, then `Forbidden`, then
  `ValidationUnavailable`. Staging prevents policy classes from mixing with
  invalid classes; the explicit precedence only resolves independent discovery
  outcomes across different sources.
- A panic or transport failure is not converted to an allow decision. The API
  server applies the registered fail-closed policy.
- DELETE and status remain recoverable even when the webhook endpoint is down
  because their requests do not match the registration rules.

## Security Considerations

- Admission policy checks use only the administrator-owned
  `installation-access-ceiling`; caller identity, ServiceAccount RBAC, and
  SubjectAccessReview are deliberately absent.
- Exact targets come from current discovery and the approved selection planner,
  preventing a namespace or scope assumption from broadening policy evaluation.
- The policy source performs one fresh read for each valid admission request.
  No previously loaded snapshot is reused after a read failure.
- Accepted requests yield no capability. Runtime authorization enforcement
  still binds a fresh opaque capability to every exact target immediately
  before LIST/WATCH resource-instance I/O.
- Side-effect-free validation makes dry-run equivalent and prevents admission
  from leaking protected data into status, Events, or external systems.
- Fixed limits, request context propagation, and the 10-second API-server
  timeout bound attacker-controlled work. Product-wide resource/output limits
  remain a separate runtime concern.

## Failure Modes And Tradeoffs

- Failure mode: The webhook endpoint, TLS configuration, or process is
  unavailable. Mitigation: `failurePolicy: Fail` denies covered writes; status
  and DELETE remain unmatched. Tradeoff: configuration writes depend on webhook
  availability because persisting unchecked desired state is not acceptable.
- Failure mode: Discovery is stale, unavailable, or permission-denied.
  Mitigation: the current resolver contract distinguishes unknown identity from
  unavailable validation; unavailable returns retryable 503. Tradeoff: valid
  writes can be temporarily blocked rather than admitted on incomplete state.
- Failure mode: The canonical policy is absent, malformed, or unreadable.
  Mitigation: missing/invalid return 403 and transient read failures return 503;
  none can allow a Kubeseer request. Tradeoff: even an explicit-empty target
  request requires a valid installation policy, keeping one uniform
  administrative boundary.
- Failure mode: Multiple planner errors cascade from one malformed declaration.
  Mitigation: dependent planners are skipped after prerequisite failure while
  independent siblings continue. Tradeoff: fixing one prerequisite may reveal
  a later dependent defect, but responses avoid misleading duplicates.
- Failure mode: Admission accepted an object before discovery or policy changed.
  Mitigation: runtime replans, rediscovers, and reauthorizes independently.
  Tradeoff: admission improves authoring feedback but is intentionally not a
  promise that future reconciliation will succeed.
- Failure mode: Shared planner APIs currently expose only their first failure.
  Mitigation: extend them with sorted multi-failure outcomes and preserve old
  wrappers. Tradeoff: small changes touch several domain packages, but this is
  preferable to cloning their semantics in admission.
- Failure mode: The ten-second timeout is exhausted by many distinct discovery
  lookups. Mitigation: strict source limits, resolver reuse, sequential
  cancellation-aware work, and a retryable response. Tradeoff: no speculative
  parallelism or admission-specific cache is introduced in this feature.

## Testing Strategy

No dedicated package-local unit suite is added. Proof starts at the repository's
required higher layers.

- Cross-module `TestModuleIntegration` scenarios construct the production
  validator with the real semantic planners. Only Kubernetes discovery and the
  policy reader use their existing consumer-owned boundaries. Tables cover all
  budgets, every planner reason family, duplicate/index path mapping, sibling
  error accumulation, deterministic ordering, skipped dependent work,
  sanitized diagnostics, and no retained capability.
- API contract tests regenerate and inspect both CRDs for structural fields,
  required markers, enums, patterns, numeric bounds, maximum items/properties,
  list-map/list-set keys, and the absence of preserve-unknown regions. Tests
  inspect generated item schemas directly.
- Envtest installs the CRDs and starts the TLS webhook. Typed and dynamic clients
  prove valid/invalid CREATE, UPDATE, dry-run equivalence, exact 422/403/503
  statuses and causes, discovery of built-in and temporary custom resources,
  namespaced/cluster scope, explicit-empty namespaces, current policy changes,
  policy semantic admission, and atomic non-persistence.
- Envtest points the registration at an unavailable endpoint to prove covered
  writes fail closed. With that endpoint unavailable, a status update and
  DELETE prove that the rules exclude those operations.
- Runtime integration scenarios persist legacy/previously valid fixtures before
  enabling or changing admission dependencies and prove reconciliation still
  replans, rediscovers, and obtains current exact-target authorization.
- `make test-api` includes the webhook envtest suite, and
  `make test-compatibility` executes it against every supported Kubernetes
  minor. Generated-file verification rejects CRD or webhook-contract drift.

## Verification Plan

- Requirement proof: Structural schema assertions prove earliest-boundary
  constraints; cross-module tests prove shared semantics and deterministic
  issue composition; envtest proves real AdmissionReview routing, dynamic
  dependencies, status codes, dry-run, fail-closed transport behavior, and
  operation exclusions; runtime integration proves admission is non-authoritative.
- Test evidence: `make verify`, `make test-integration`, `make test-api`, and
  `make test-compatibility` are the feature-level evidence commands. Leaf tasks
  must use focused test names beneath those entry points and assert that the
  intended suite actually ran.
- Operational evidence: Inspect the installed
  `ValidatingWebhookConfiguration` for exact rules, paths, v1 review support,
  side effects, failure policy, and timeout; inspect API responses and persisted
  object absence. Metrics, Events, dashboards, production certificates, and
  deployment health are intentionally outside this feature.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Structural API Schema; AdmissionReview Adapter And Registration Contract; Validation Flow; envtest operation and outage scenarios |
| `R2` | Structural API Schema And Budget Constants; validation stages 1-2; CRD and cross-module budget proofs |
| `R3` | Shared Declarative Validation Results; Kubeseer validation stage 3; field-path and preservation scenarios |
| `R4` | Shared operator, typed-output, and aggregation planning outcomes; Kubeseer validation stage 3; integration matrices |
| `R5` | Discovery And Exact-Target Policy Adapters; Kubeseer validation stage 4; envtest discovery/scope scenarios |
| `R6` | Fresh canonical policy snapshot and pure exact-target evaluation; policy admission flow; policy envtest scenarios |
| `R7` | Internal Issue/Result model; deterministic sorting and Status causes; response and confidentiality proofs |
| `R8` | Non-persistent admission outcome; unchanged runtime authorization boundary; legacy/drift runtime integration scenarios |
| `NFR1` | Fail-closed registration, fresh policy load, no capability, and independent runtime authorization proofs |
| `NFR2` | Structural CRDs, Kubernetes parsers/discovery, AdmissionReview v1, Kubernetes Status classes, and envtest |
| `NFR3` | Sorted validation outcomes, sequential orchestration, stable reasons/paths, and repeated-input tests |
| `NFR4` | Structural and defensive cardinality/byte limits before dynamic I/O; no-I/O assertions |
| `NFR5` | Sanitized domain errors and fixed response templates; negative leakage assertions |
| `NFR6` | Retryable dynamic failures plus unmatched status/DELETE; endpoint and dependency outage envtest |
| `NFR7` | Cross-module integration, API contract inspection, full TLS webhook envtest, and compatibility matrix |
| `NFR8` | Unchanged v1alpha1 serialization, exact version registration, generated compatibility checks, and legacy fixtures |
