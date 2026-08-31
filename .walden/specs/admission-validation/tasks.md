---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-31T13:33:39Z
last_modified: 2026-08-31T13:33:39Z
approved_fingerprint: sha256:5e00abc086fe0bf48be82b6643f6bf9b3d50777226cab56fe631a4ba7b06de9e
source_design_approved_at: 2026-08-31T11:23:51Z
source_design_fingerprint: sha256:1de1e7d444390d4970bc924e74f5f3e34ba03c7c053712f1d03e72560bef69a6
---

# Implementation Plan

<!-- assumed: all automated proof remains in genuine cross-module integration and envtest layers, with no dedicated package-local unit suite (source: approved Design / Testing Strategy and .walden/constitution.md) -->

- [ ] 1. Enforce structural schemas and bounded admission input
  - [ ] 1.1 Add generated schema constraints, immutable limits, and pre-I/O budget guards
    - Add the approved required, enum, pattern, length, range, list-map/set,
      maximum-items, and maximum-properties markers across every public
      `Kubeseer` and `KubeseerAccessPolicy` configuration field. Keep the API
      structural, preserve existing omission semantics, and introduce no
      preserve-unknown region or second API version.
    - Add immutable admission limits for every approved cardinality and the
      262144-byte canonical JSON encoding of each typed `Spec`. Implement
      defensive budget validation before semantic planning, discovery, policy
      loading, or compilation; retain submitted indexes in every budget issue.
    - Regenerate CRD artifacts. Extend `TestAPIContract` to
      inspect generated item schemas and list extensions directly, exercise
      API-server rejection for every expressible constraint, and prove old
      v1alpha1 manifests retain their serialized meanings. Emit
      `API_CONTRACT=admission-validation-structure STATUS=passed` only after the
      real API-server contract succeeds.
    - Extend `TestModuleIntegration` with exact-boundary and over-boundary
      tables for every cardinality and byte limit. Use counting discovery and
      policy boundaries to prove all over-budget requests stop before dynamic
      I/O and policy compilation. Emit
      `MODULE_INTEGRATION=admission-validation-budgets STATUS=passed`.
    - Requirements: `R1.AC1`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R2.AC11`, `R2.AC12`, `R2.AC13`, `R2.AC14`, `R2.AC15`, `R2.AC16`, `R2.AC17`, `R2.AC18`, `R2.AC19`, `R2.AC20`, `R2.AC21`, `R2.AC22`, `R2.AC23`, `R2.AC24`, `NFR2`, `NFR4`, `NFR8`, `C2`, `C3`, `C8`
    - Design: Components And Interfaces / Structural API Schema And Budget Constants; Validation Flow; Data Models; Testing Strategy; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-api"]
        expect_output: "API_CONTRACT=admission-validation-structure STATUS=passed"
        timeout: 45m
        covers: ["R1.AC1", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R2.AC11", "R2.AC12", "R2.AC13", "R2.AC14", "R2.AC18", "R2.AC19", "R2.AC20", "R2.AC21", "R2.AC22", "NFR2", "NFR8", "C2", "C3"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=admission-validation-budgets STATUS=passed"
        timeout: 35m
        covers: ["R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R2.AC11", "R2.AC12", "R2.AC13", "R2.AC14", "R2.AC15", "R2.AC16", "R2.AC17", "R2.AC18", "R2.AC19", "R2.AC20", "R2.AC21", "R2.AC22", "R2.AC23", "R2.AC24", "NFR4", "C8"]

- [ ] 2. Expose complete shared declarative validation outcomes
  - [ ] 2.1 Extend approved planners and the policy compiler with sorted multi-error contracts
    - Extend selection/static parsing, extraction, typed output, operators,
      aggregation, and access-policy validation to expose every independent
      declaration failure with stable source, field, operator, aggregate, and
      submitted-index coordinates. Preserve existing planner behavior and keep
      current single-error APIs as compatibility wrappers over the first sorted
      failure.
    - Add the internal admission `Issue`, `Class`, and `Result` contracts plus
      deterministic path/reason/message sorting. Map only sanitized domain
      reasons and fixed corrective templates; never include operands, observed
      objects, Secret data, or wrapped downstream error text.
    - Compose pure semantic validation for source identity, Kubernetes
      group-version and Kind syntax, namespace/name/selectors, field identity,
      typed JSONPath, operator arity/operands/conversion/regex, aggregation
      references/types/options, and policy compilation. Skip only dependent
      checks after a prerequisite failure while continuing independent siblings.
      Do not mutate paths or omitted average options.
    - Extend the real planner collaboration in `TestModuleIntegration` with
      every semantic reason family, multiple sibling defects, duplicate index
      paths, deterministic permutations, old wrapper compatibility, unchanged
      submitted JSONPath text, empty field/namespace semantics, omitted average
      defaults, valid narrowing policy updates, and confidentiality sentinels.
      Emit `MODULE_INTEGRATION=admission-validation-semantics STATUS=passed`.
    - Requirements: `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R3.AC11`, `R3.AC12`, `R3.AC13`, `R3.AC14`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R4.AC10`, `R4.AC11`, `R4.AC12`, `R4.AC13`, `R4.AC14`, `R4.AC15`, `R4.AC16`, `R4.AC17`, `R4.AC18`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC12`, `R7.AC13`, `R8.AC8`, `NFR3`, `NFR5`, `NFR8`, `C1`, `C4`, `C9`
    - Design: Components And Interfaces / Shared Declarative Validation Results; Components And Interfaces / Admission Validator; Validation Flow; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=admission-validation-semantics STATUS=passed"
        timeout: 35m
        covers: ["R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R3.AC11", "R3.AC12", "R3.AC13", "R3.AC14", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R4.AC10", "R4.AC11", "R4.AC12", "R4.AC13", "R4.AC14", "R4.AC15", "R4.AC16", "R4.AC17", "R4.AC18", "R6.AC9", "R6.AC10", "R6.AC11", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC12", "R7.AC13", "R8.AC8", "NFR3", "NFR5", "NFR8", "C1", "C4", "C9"]

- [ ] 3. Orchestrate current discovery and exact-target installation policy validation
  - [ ] 3.1 Build the staged Kubeseer validator without resource-instance I/O or capabilities
    - Implement `internal/admission.Validator` so static/budget failures stop
      dynamic work, then every otherwise valid source is planned sequentially
      through current discovery and `selection.Planner`. Retain discovered GVR,
      Kind, scope, and exact namespace targets without reading resource
      instances or requiring namespace existence.
    - Classify unknown resources and scope conflicts as invalid, transient or
      permission discovery failures as `ValidationUnavailable`, and resolve
      mixed source outcomes with the approved deterministic precedence.
    - Load one fresh canonical `installation-access-ceiling` snapshot for every
      dynamically valid request, including zero-target explicit-empty namespace
      requests. Evaluate every exact namespaced or cluster target, collect all
      denials atomically, and classify policy denied/missing/invalid as
      forbidden and load failure as unavailable. Produce no authorization
      capability, cache authority, SubjectAccessReview, or caller-RBAC inference.
    - Extend `TestModuleIntegration` with served/unknown/unavailable resources,
      namespaced/cluster scopes, omitted/multiple/explicit-empty namespaces,
      deterministic equivalent discovery, all policy terminal states, exact
      target denials, fresh-load counts, sibling outcomes, cancellation, and
      zero resource-instance calls. Emit
      `MODULE_INTEGRATION=admission-validation-dynamic STATUS=passed`.
    - Requirements: `R1.AC11`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC13`, `R7.AC1`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R7.AC14`, `NFR1`, `NFR3`, `NFR5`, `NFR6`, `C5`, `C6`
    - Design: Architecture; Components And Interfaces / Admission Validator; Components And Interfaces / Discovery And Exact-Target Policy Adapters; Validation Flow / Kubeseer CREATE And UPDATE; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=admission-validation-dynamic STATUS=passed"
        timeout: 35m
        covers: ["R1.AC11", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC13", "R7.AC1", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "R7.AC14", "NFR1", "NFR3", "NFR5", "NFR6", "C5", "C6"]

- [ ] 4. Expose the AdmissionReview v1 handler and installable registration contract
  - [ ] 4.1 Add typed handlers, Kubernetes Status mapping, and endpoint-agnostic webhook configuration
    - Register generic controller-runtime validators for `Kubeseer` and
      `KubeseerAccessPolicy` at the two approved stable paths. Validate proposed
      objects on CREATE/UPDATE, defensively allow DELETE, preserve dry-run
      behavior through side-effect-free execution, and return BadRequest for
      decode failures without echoing object content.
    - Translate sorted issues into `metav1.Status` causes with submitted field
      path, concise message, and stable machine reason. Map invalid,
      policy-forbidden, and unavailable outcomes to exact 422, 403, and 503
      status classes with the approved precedence and sanitized top-level text.
    - Add the endpoint-agnostic `ValidatingWebhookConfiguration` builder with
      exact v1alpha1 main-resource CREATE/UPDATE rules, namespaced and cluster
      scopes, `matchPolicy: Exact`, `failurePolicy: Fail`, `sideEffects: None`,
      `timeoutSeconds: 10`, and `admissionReviewVersions: [v1]`. Accept injected
      client configuration and do not add production Service or certificate
      lifecycle ownership.
    - Extend `TestModuleIntegration` through real AdmissionReview decoding and
      response encoding. Cover both kinds and operations, malformed payloads,
      multi-cause ordering, status classes, dry-run equivalence, defensive
      DELETE, exact registration fields, immutable builder outputs, and absence
      of capabilities or mutations. Emit
      `MODULE_INTEGRATION=admission-validation-registration STATUS=passed`.
    - Requirements: `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `R1.AC10`, `R1.AC11`, `R1.AC12`, `R7.AC1`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC12`, `R7.AC13`, `R7.AC14`, `NFR1`, `NFR2`, `NFR3`, `NFR5`, `NFR6`, `NFR8`, `C7`, `C10`
    - Design: Components And Interfaces / AdmissionReview Adapter And Registration Contract; Data Models; Error Handling; Security Considerations; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=admission-validation-registration STATUS=passed"
        timeout: 35m
        covers: ["R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R1.AC9", "R1.AC10", "R1.AC11", "R1.AC12", "R7.AC1", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC12", "R7.AC13", "R7.AC14", "NFR1", "NFR2", "NFR3", "NFR5", "NFR6", "NFR8", "C7", "C10"]

- [ ] 5. Prove the complete TLS webhook boundary with a real API server
  - [ ] 5.1 Add the envtest webhook harness and covered-operation scenario matrix
    - Add `TestEnvtestAdmissionValidation` under the approved envtest layer.
      Start a controller-runtime TLS webhook server with envtest serving
      material, install the generated CRDs and endpoint-injected registration
      contract, and use bounded readiness checks rather than sleeps. Add the
      suite to `make test-api` and therefore the supported-minor compatibility
      matrix.
    - Through typed and dynamic clients, prove valid and invalid CREATE/UPDATE,
      dry-run equivalence, structural rejections, atomic non-persistence,
      current built-in and temporary-CR discovery, namespace/cluster scope,
      exact policy target checks, policy create/update compilation, valid
      narrowing, and exact 422/403/503 response causes.
    - Point the registration at an unavailable endpoint and prove covered
      writes fail closed. While unavailable, prove status updates and DELETE do
      not match the webhook and remain recoverable. Confirm the installed
      configuration's paths, rules, AdmissionReview version, failure policy,
      side effects, match policy, and timeout.
    - Keep URL/CA injection local to the harness; add no production namespace,
      Service, certificate issuance, rotation, deployment, metrics, or Events.
      Emit `API_CONTRACT=admission-validation STATUS=passed` only after the
      complete API-server-to-webhook matrix succeeds.
    - Requirements: `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `R1.AC10`, `R1.AC12`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R7.AC1`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC14`, `NFR2`, `NFR6`, `NFR7`, `NFR8`, `C7`, `C10`
    - Design: Components And Interfaces / Local Webhook Test Harness; Validation Flow; Failure Modes And Tradeoffs; Testing Strategy; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-api"]
        expect_output: "API_CONTRACT=admission-validation STATUS=passed"
        timeout: 50m
        covers: ["R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R1.AC9", "R1.AC10", "R1.AC12", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC9", "R6.AC10", "R6.AC11", "R7.AC1", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC14", "NFR2", "NFR6", "NFR7", "NFR8", "C7", "C10"]

- [ ] 6. Preserve runtime authority across legacy objects and cluster drift
  - [ ] 6.1 Prove independent replanning, rediscovery, authorization, and policy invalidation
    - Extend the production module collaboration to show that an admitted
      object yields no retained plan, discovery proof, policy snapshot, or
      authorization capability. At reconciliation, repeat semantic planning,
      current discovery, and current exact-target capability binding before any
      LIST/WATCH request or status publication.
    - Add legacy-object fixtures persisted before webhook registration and
      previously admitted fixtures whose resource type or policy changes after
      admission. Prove source-scoped discovery failure, policy narrowing and
      deletion invalidation, stale-result removal, no post-revocation read,
      current runtime state precedence, and the same fail-closed behavior for
      legacy and newly admitted resources.
    - Extend both `TestModuleIntegration` and
      `TestEnvtestReconciliationRuntime` through the existing production
      reconciliation and authorization boundaries. Preserve current public
      status semantics and confidentiality. Emit
      `MODULE_INTEGRATION=admission-validation-runtime STATUS=passed` and
      `API_CONTRACT=admission-validation-runtime STATUS=passed` only after the
      respective higher-layer scenarios succeed.
    - Requirements: `R1.AC11`, `R6.AC8`, `R6.AC12`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `NFR1`, `NFR3`, `NFR5`, `NFR7`, `NFR8`, `C6`, `C9`
    - Design: Overview; Architecture; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy; Verification Plan
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=admission-validation-runtime STATUS=passed"
        timeout: 40m
        covers: ["R1.AC11", "R6.AC8", "R6.AC12", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "NFR1", "NFR3", "NFR5", "NFR7", "NFR8", "C6", "C9"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-api"]
        expect_output: "API_CONTRACT=admission-validation-runtime STATUS=passed"
        timeout: 50m
        covers: ["R6.AC12", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "NFR1", "NFR5", "NFR7", "NFR8", "C6", "C9"]

- [ ] 7. Complete higher-layer and repository verification
  - [ ] 7.1 Verify comprehensive coverage, boundaries, race safety, compatibility, and generated state
    - Complete the cross-module scenario matrix so every approved admission
      requirement is exercised through production collaborations, with only
      discovery and policy-reader infrastructure adapted at their existing
      consumer-owned boundaries. Emit exactly
      `MODULE_INTEGRATION=admission-validation STATUS=passed` after the complete
      matrix succeeds.
    - Register new integration/envtest files in the test-layer policy and add
      static boundary checks for no package-local unit suite, second API
      version, opaque schema, SubjectAccessReview/user impersonation,
      admission-created authorization capability, resource-instance I/O,
      production Service/certificate lifecycle, or generated drift.
    - Run generated/schema and test-layer verification, race-enabled
      higher-layer tests, the Kubernetes 1.35.6/1.36.2 compatibility matrix,
      complete Go compilation, and read-only module tidiness with task-scoped
      writable caches.
    - Requirements: `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`, `C10`
    - Design: Simplicity And Elegance Review; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy; Verification Plan; Requirement Coverage
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 35m
        covers: ["NFR3", "NFR6", "NFR7", "NFR8", "C2", "C3", "C5", "C6", "C7", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test GO_TEST_FLAGS=-race"]
        expect_output: "MODULE_INTEGRATION=admission-validation STATUS=passed"
        timeout: 50m
        covers: ["NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "NFR8", "C1", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 75m
        covers: ["NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "NFR8", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod go build ./..."]
        timeout: 25m
        covers: ["C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-admission-validation-go-build GOMODCACHE=/tmp/kubeseer-admission-validation-go-mod go mod tidy -diff"]
        timeout: 25m
        covers: ["C1", "C2"]
