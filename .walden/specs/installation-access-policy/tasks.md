---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-02T08:52:52Z
last_modified: 2026-08-25T10:47:29Z
approved_fingerprint: sha256:427da64da46d7d23d62e316355c606a1371217053667284d34a11673f31cbe58
source_design_approved_at: 2026-08-02T08:26:03Z
source_design_fingerprint: sha256:89800f88c9cfa51ad505ef2cd57fff2b7640cbec3fb5d51af7c6db79df594722
---

# Implementation Plan

- [x] 1. Define and generate the installation access policy API
  - [x] 1.1 Add the typed `KubeseerAccessPolicy` API contract
    - Add the cluster-scoped policy and list types, namespace modes, namespace policy,
      resource rules, active singleton-name constant, scheme registration, JSON
      behavior, and generated deep-copy support without adding policy fields to
      namespaced `Kubeseer` resources.
    - Test scheme registration, JSON round trips, deep-copy isolation, the core API
      group representation, nil-versus-explicit-empty `systemNamespaces`, and the
      disabled zero value for cluster-scoped access.
    - Requirements: `R1.AC1`, `R1.AC3`, `R2.AC5`, `R2.AC6`, `R3.AC2`, `R4.AC3`, `NFR4`
    - Design: Overview; Components And Interfaces / KubeseerAccessPolicy API; Data Models
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod go test -v -run '^TestKubeseerAccessPolicy(SchemeRegistration|JSONRoundTrip|DeepCopyIsolation)$' ./api/v1alpha1"]
        expect_output: "--- PASS: TestKubeseerAccessPolicySchemeRegistration"
        covers: ["R1.AC1", "R1.AC3", "R2.AC5", "R2.AC6", "R3.AC2", "R4.AC3"]

  - [x] 1.2 Generate and assert the structural cluster-scoped CRD
    - Add kubebuilder markers for cluster scope, the
      `installation-access-ceiling` singleton CEL rule, enum/default/pattern/list
      constraints, required non-empty rule members, and valid empty deny-all policy
      surfaces; regenerate the CRD and deep-copy artifacts.
    - Extend generated-artifact verification to compare every generated CRD and add
      a static manifest contract test covering names, scope, defaults, set lists,
      exact API group/Kind syntax, and absence of a status subresource.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R2.AC5`, `R2.AC6`, `R3.AC2`, `R4.AC3`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `NFR1`, `NFR4`
    - Design: Architecture; Components And Interfaces / KubeseerAccessPolicy API; Data Models; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod go test -v -run '^TestKubeseerAccessPolicyGeneratedCRDContract$' ./api/v1alpha1"]
        expect_output: "--- PASS: TestKubeseerAccessPolicyGeneratedCRDContract"
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R2.AC5", "R2.AC6", "R3.AC2", "R4.AC3", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make verify"]
        expect_output: "generated artifacts are current"

- [x] 2. Compile and evaluate the logical installation ceiling
  - [x] 2.1 Implement defensive validation and immutable policy compilation
    - Create `internal/accesspolicy` with stable reason/diagnostic contracts and a
      compiler that validates the active name, namespace modes and names, duplicate
      set entries, resource rule cardinality, exact group/Kind syntax, and wildcard
      exclusion even when API admission is bypassed.
    - Compile defensive copies of namespace sets and every API-group/Kind
      cross-product; preserve nil-versus-empty system namespace behavior, accept the
      two intentional empty deny-all boundaries, and deterministically select a
      field-specific validation error.
    - Requirements: `R1.AC2`, `R2.AC5`, `R2.AC6`, `R3.AC2`, `R3.AC6`, `R4.AC3`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R8.AC1`, `R8.AC2`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`
    - Design: Components And Interfaces / Compiler; Data Models; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod go test -v -run '^TestCompilePolicy$' ./internal/accesspolicy"]
        expect_output: "--- PASS: TestCompilePolicy"
        covers: ["R1.AC2", "R2.AC5", "R2.AC6", "R3.AC2", "R3.AC6", "R4.AC3", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R8.AC1", "R8.AC2"]

  - [x] 2.2 Implement deterministic namespace, resource, and scope evaluation
    - Add normalized `Request` and stable `Decision` contracts plus pure snapshot
      evaluation with the documented precedence: snapshot state, invalid request,
      resource denial, cluster-scope denial, namespace denial, allow.
    - Cover all namespace modes, exclusion priority, explicit system-namespace
      inclusion, exact built-in and CRD-backed matches, cross-product matches,
      namespaced versus cluster-scoped behavior, and request narrowing without any
      Kubernetes API, resource-instance, ServiceAccount, or user-RBAC input.
    - Add permutation and combined-denial tests proving byte-for-byte stability,
      immutable compiled inputs, exactly one primary reason, logical/RBAC separation,
      and sanitized messages.
    - Requirements: `R1.AC4`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`
    - Design: Components And Interfaces / Snapshot evaluator; Data Models; Decision Algorithm; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod go test -v -run '^TestEvaluatePolicy$' ./internal/accesspolicy"]
        expect_output: "--- PASS: TestEvaluatePolicy"
        covers: ["R1.AC4", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6"]

- [x] 3. Load the active policy through a fail-closed boundary
  - [x] 3.1 Implement the narrow policy source, loader, and terminal snapshots
    - Add a `PolicySource` abstraction and controller-runtime client adapter that can
      retrieve only `KubeseerAccessPolicy/installation-access-ceiling`; classify
      not-found separately from other read failures and pass retrieved objects through
      the defensive compiler.
    - Return a newly constructed valid or deny-all snapshot for every load attempt,
      never reuse a previous success, and keep raw upstream errors and resource data
      outside stable status-facing decisions.
    - Test transitions from valid to missing, invalid, and unavailable policy;
      demonstrate that all failure snapshots deny every request, evaluation performs
      no source call, logical allows remain distinct from later RBAC failures, and
      diagnostics contain only approved request/configuration identities.
    - Requirements: `R1.AC2`, `R1.AC4`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R8.AC1`, `R8.AC2`, `R8.AC5`, `R8.AC6`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`
    - Design: Architecture; Components And Interfaces / PolicySource and loader; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod go test -v -run '^TestLoadPolicy$' ./internal/accesspolicy"]
        expect_output: "--- PASS: TestLoadPolicy"
        covers: ["R1.AC2", "R1.AC4", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R8.AC1", "R8.AC2", "R8.AC5", "R8.AC6"]

- [x] 4. Prove admission and defaulting on supported Kubernetes API servers
  - [x] 4.1 Extend the envtest contract and compatibility matrix for the policy CRD
    - Extend the existing API-server contract to discover the cluster-scoped policy
      resource, persist a valid `installation-access-ceiling`, observe omitted-field
      defaults, and preserve an explicitly empty `systemNamespaces` list.
    - Prove the API server rejects non-singleton names, invalid modes/namespaces,
      duplicate set values, empty rule members, malformed groups/Kinds, and wildcards
      while accepting empty deny-all namespace and resource configurations.
    - Run the contract through the existing Kubernetes v1.35.6 and v1.36.2
      compatibility matrix without starting a controller manager or reading any
      Kubernetes resource instance governed by the policy.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R2.AC5`, `R2.AC6`, `R3.AC2`, `R4.AC3`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `NFR1`, `NFR4`
    - Design: Testing Strategy; Verification Plan; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 30m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R2.AC5", "R2.AC6", "R3.AC2", "R4.AC3", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8"]
