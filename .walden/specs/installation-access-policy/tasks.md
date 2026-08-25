---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-25T21:15:19Z
last_modified: 2026-08-25T21:29:06Z
approved_fingerprint: sha256:1d7cab82a6c4c7efd515ba9b8a47e35c6f020588945f6787f473437adae1471f
source_design_approved_at: 2026-08-25T21:05:38Z
source_design_fingerprint: sha256:8a75e79f579c3d9fa19fce2f6ab6b4cbabc52058ae1d2ad3951d36a20f920d1a
---

# Implementation Plan

- [x] 1. Consolidate the policy API contract at the envtest layer
  - [x] 1.1 Migrate the typed policy contract into `TestAPIContract`
    - Extend the existing `api/v1alpha1` envtest suite with named policy subtests
      for scheme registration, typed controller-runtime client round trips, JSON
      representation, deep-copy isolation, the active singleton-name constant,
      nil-versus-explicit-empty `systemNamespaces`, and the disabled zero value for
      cluster-scoped access.
    - Preserve the policy as a cluster-scoped administrative API separate from
      namespaced `Kubeseer` resources. Emit the stable
      `API_CONTRACT=kubeseer-access-policy-types STATUS=passed` marker only after all
      migrated assertions pass, then remove
      `api/v1alpha1/kubeseer_access_policy_types_test.go`.
    - Requirements: `R1.AC1`, `R1.AC3`, `R2.AC5`, `R2.AC6`, `R3.AC2`, `R4.AC3`, `NFR4`
    - Design: Overview; Components And Interfaces / KubeseerAccessPolicy API; Data Models; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make test-api"]
        expect_output: "API_CONTRACT=kubeseer-access-policy-types STATUS=passed"
        timeout: 20m
        covers: ["R1.AC1", "R1.AC3", "R2.AC5", "R2.AC6", "R3.AC2", "R4.AC3"]

  - [x] 1.2 Prove generated CRD admission and the real policy client adapter
    - Extend `TestAPIContract` against the installed generated CRD to discover the
      cluster-scoped resource, persist the active singleton through a real
      controller-runtime client, observe omitted-field defaults, preserve an
      explicitly empty `systemNamespaces`, and exercise the controller-runtime-backed
      `PolicySource` adapter against the envtest API server.
    - Prove admission rejects non-singleton names, invalid modes and namespaces,
      duplicate set values, empty resource-rule members, malformed groups and Kinds,
      and wildcards while accepting both intentional empty deny-all boundaries.
      Retain static structural-schema assertions that admission cannot expose, emit
      `API_CONTRACT=kubeseer-access-policy-admission STATUS=passed`, then remove
      `api/v1alpha1/kubeseer_access_policy_crd_test.go`.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R2.AC5`, `R2.AC6`, `R3.AC2`, `R4.AC3`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `NFR1`, `NFR4`
    - Design: Architecture; Components And Interfaces / KubeseerAccessPolicy API; Components And Interfaces / PolicySource and loader; Data Models; Security Considerations; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make test-api"]
        expect_output: "API_CONTRACT=kubeseer-access-policy-admission STATUS=passed"
        timeout: 20m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R2.AC5", "R2.AC6", "R3.AC2", "R4.AC3", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8"]

- [x] 2. Prove the production discovery-to-policy path at the module layer
  - [x] 2.1 Add the first real `TestModuleIntegration` evaluation scenarios
    - Create `test/integration` with the single top-level
      `TestModuleIntegration` entry point and compose the production
      `discovery.Resolver`, policy compiler, immutable snapshot, and evaluator. Keep
      only `discovery.DiscoveryClient` controlled so scenarios exercise the real
      module contracts rather than test-only orchestration or feature flags.
    - Migrate the compiler and evaluator behavior tables through real discovery
      resolutions: all namespace modes and exclusions, system-namespace default and
      explicit-empty behavior, exact built-in and CRD-backed resource matches,
      resource-rule cross-products, namespaced and cluster-scoped decisions,
      defensive validation, immutable inputs, deterministic precedence, logical/RBAC
      separation, and sanitized stable diagnostics.
    - Emit `MODULE_INTEGRATION=discovery-access-policy-evaluation STATUS=passed` only
      after the migrated cases pass, then remove
      `internal/accesspolicy/compiler_test.go` and
      `internal/accesspolicy/evaluator_test.go`.
    - Requirements: `R1.AC2`, `R1.AC4`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`
    - Design: Architecture; Components And Interfaces / Compiler; Components And Interfaces / Snapshot evaluator; Data Models; Decision Algorithm; Error Handling; Security Considerations; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=discovery-access-policy-evaluation STATUS=passed"
        timeout: 20m
        covers: ["R1.AC2", "R1.AC4", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6"]

  - [x] 2.2 Extend `TestModuleIntegration` through the fail-closed loader
    - Extend the same production collaboration path with the real loader, compiler,
      immutable snapshot, evaluator, and discovery resolver. Control only the narrow
      `PolicySource` boundary for deterministic missing, invalid, and unavailable
      transitions; rely on Task 1.2 for the real controller-runtime adapter proof.
    - Prove every load constructs a fresh snapshot, never reuses a prior success,
      denies every resolved request on missing, invalid, or unavailable policy,
      performs no policy-source call during evaluation, keeps logical allows distinct
      from later RBAC outcomes, and excludes raw upstream errors and resource data
      from stable decisions.
    - Emit `MODULE_INTEGRATION=discovery-access-policy-loader STATUS=passed` only after
      the migrated cases pass, then remove
      `internal/accesspolicy/loader_test.go`.
    - Requirements: `R1.AC2`, `R1.AC4`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R8.AC1`, `R8.AC2`, `R8.AC5`, `R8.AC6`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`
    - Design: Architecture; Components And Interfaces / PolicySource and loader; Components And Interfaces / Compiler; Components And Interfaces / Snapshot evaluator; Error Handling; Security Considerations; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=discovery-access-policy-loader STATUS=passed"
        timeout: 20m
        covers: ["R1.AC2", "R1.AC4", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R8.AC1", "R8.AC2", "R8.AC5", "R8.AC6"]

- [x] 3. Seal generated, compatibility, and higher-layer verification
  - [x] 3.1 Run the complete installation access policy verification matrix
    - Regenerate and verify deep-copy code and CRD manifests, and update generated
      verification only where necessary to keep all generated policy artifacts under
      drift detection.
    - Run the default higher-layer suite, the envtest API contract on Kubernetes
      v1.35.6 and v1.36.2, compilation, and module tidiness. Confirm the test-layer
      policy reports no package-local unit-test files after the five migrated files
      are removed and that no ambient kubeconfig or cloud credentials are used.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R2.AC5`, `R2.AC6`, `R3.AC2`, `R4.AC3`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `NFR1`, `NFR2`, `NFR4`
    - Design: Testing Strategy; Verification Plan; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make verify"]
        expect_output: "Test layer policy passed"
        timeout: 20m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make test"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        timeout: 20m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 35m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R2.AC5", "R2.AC6", "R3.AC2", "R4.AC3", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod go build ./..."]
        timeout: 10m
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-access-policy-go-build GOMODCACHE=/tmp/kubeseer-access-policy-go-mod go mod tidy -diff"]
        timeout: 10m
