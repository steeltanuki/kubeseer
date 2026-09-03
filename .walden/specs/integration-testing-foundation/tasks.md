---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-02T17:05:23Z
last_modified: 2026-09-02T17:05:23Z
approved_fingerprint: sha256:f735290627b354da3e086c86b3cac60af12764edec98d7d07f957d3758c721b6
source_design_approved_at: 2026-08-25T13:42:19Z
source_design_fingerprint: sha256:5ed040de4077c66ff76591309afc4862013bfe4ae5c2d521fafa477231594fae
---

# Implementation Plan

## Coverage Migration Matrix

| Existing test | Required higher-layer replacement |
| --- | --- |
| `TestSchemeRegistration` | API envtest: typed scheme and client construction |
| `TestJSONRoundTrip` | API envtest: typed persistence and serialization |
| `TestDeepCopyIsolation` | API envtest: typed cache copy isolation |
| `TestGeneratedCRDContract` | API envtest: installed CRD schema and subresources |
| `TestDiscoveryContracts` | Discovery envtest: descriptor, scope, and stable-error contract |
| `TestResolveSingle`, `TestResolveCore`, `TestResolveGrouped`, `TestResolveCRD` | Discovery envtest: real built-in and CRD-backed resolution |
| `TestResolveScope`, `TestResolveAmbiguous`, `TestResolveErrors`, `TestResolveBatch` | Discovery envtest: scope guards, ambiguity, unavailable API, and batch isolation |
| `TestCacheFreshHit`, `TestCacheExpiryRefresh`, `TestCacheConcurrentRefresh`, `TestCacheInvalidation` | Discovery envtest: counting transport, injected clock, concurrent refresh, and CRD-driven invalidation |

- [x] 1. Establish module and test-layer boundaries
  - [x] 1.1 Document production package responsibilities and add a read-only architecture verifier for package docs, acyclic imports, consumer-owned infrastructure ports, and forbidden production imports from `test/`.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `NFR1`
    - Design: Architecture; Production module contract; Module-boundary proof
    - Verification:
      - command: ["./hack/verify-module-boundaries.sh"]
        expect_output: "Module boundary verification passed"
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "NFR1"]
      - command: ["go", "build", "./..."]
        covers: ["R1.AC2"]

  - [x] 1.2 Implement the probe/required test-layer runner and its temporary-package acceptance harness, including exact not-applicable handling, compile-error propagation, stable markers, and anchored non-vacuous selection.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R4.AC6`, `R6.AC2`, `R6.AC3`, `NFR2`, `NFR4`
    - Design: Module integration suite; Non-vacuous test-layer runner; Entry-point acceptance checks
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod ./hack/test-layer-runner-acceptance.sh"]
        expect_output: "Test-layer runner acceptance passed"
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R4.AC6", "R6.AC2", "R6.AC3", "NFR2", "NFR4"]

  - [x] 1.3 Add stable Make targets for default, module-integration, API-integration, compatibility, and end-to-end selection, with envtest bootstrap when no production collaboration suite exists.
    - Requirements: `R3.AC4`, `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R6.AC1`, `NFR2`, `NFR3`, `NFR4`
    - Design: Repository test command surface; Non-vacuous test-layer runner; Architecture command surface
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make test-integration"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        covers: ["R4.AC1", "R4.AC6", "R6.AC1"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make test"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        timeout: 20m
        covers: ["R3.AC4", "R4.AC2", "R4.AC3", "R4.AC5", "R4.AC6", "R4.AC7", "NFR2", "NFR3", "NFR4"]

- [x] 2. Consolidate local Kubernetes API integration and migrate behavior
  - [x] 2.1 Add the shared envtest lifecycle with pinned assets, `UseExistingCluster=false`, loopback-only clients, unique ownership scopes, bounded polling, explicit cleanup, and actionable startup diagnostics.
    - Requirements: `R1.AC4`, `R1.AC5`, `R1.AC6`, `R3.AC1`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `NFR2`, `NFR3`, `NFR4`, `NFR5`
    - Design: Local envtest harness; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "assets=\"$(./hack/envtest-assets.sh 1.35.6)\" && GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod KUBEBUILDER_ASSETS=\"$assets\" ./hack/envtest-harness-acceptance.sh"]
        expect_output: "Envtest harness acceptance passed"
        timeout: 20m
        covers: ["R1.AC4", "R1.AC5", "R1.AC6", "R3.AC1", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "NFR2", "NFR3", "NFR4", "NFR5"]

  - [x] 2.2 Migrate the API type, serialization, generated deep-copy, CRD schema, admission, persistence, and status behavior from package-local unit tests into the named API envtest suite without deleting the old tests yet.
    - Requirements: `R3.AC1`, `R5.AC6`, `R6.AC5`, `R7.AC2`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `NFR4`, `NFR6`
    - Design: Unit-test retirement and coverage migration; Envtest API integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make test-api"]
        expect_output: "API_CONTRACT=kubeseer-v1alpha1 STATUS=passed"
        timeout: 20m
        covers: ["R3.AC1", "R5.AC6", "R6.AC5", "R7.AC2", "R7.AC4", "R7.AC5", "R7.AC6", "NFR4", "NFR6"]

  - [x] 2.3 Migrate discovery descriptors, resolution, failures, batch isolation, cache freshness, concurrent refresh, and invalidation into the named discovery envtest suite using the real local discovery endpoint, a counting transport, and the injectable clock.
    - Requirements: `R1.AC5`, `R1.AC6`, `R3.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC6`, `R6.AC5`, `R7.AC2`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `NFR4`, `NFR6`
    - Design: Unit-test retirement and coverage migration; Local envtest harness; Envtest API integration
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make test-api GO_TEST_FLAGS=-race"]
        expect_output: "API_CONTRACT=resource-discovery STATUS=passed"
        timeout: 25m
        covers: ["R1.AC5", "R1.AC6", "R3.AC1", "R5.AC2", "R5.AC3", "R5.AC6", "R6.AC5", "R7.AC2", "R7.AC4", "R7.AC5", "R7.AC6", "NFR4", "NFR6"]

- [x] 3. Establish the local full-cluster boundary
  - [x] 3.1 Add the non-vacuous end-to-end entry point and kind-on-Podman lifecycle boundary with explicit provider, project-owned cluster name, temporary kubeconfig, prerequisite checks, and exact scoped cleanup; prove arguments through a temporary stubbed-tool acceptance harness without mutating a real cluster.
    - Requirements: `R3.AC2`, `R4.AC1`, `R4.AC4`, `R4.AC6`, `R5.AC1`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `NFR3`, `NFR4`, `NFR5`
    - Design: Kind end-to-end boundary; Repository test command surface; Kind safety proof
    - Verification:
      - command: ["./hack/e2e-harness-acceptance.sh"]
        expect_output: "E2E harness acceptance passed"
        covers: ["R3.AC2", "R4.AC1", "R4.AC4", "R4.AC6", "R5.AC1", "R5.AC3", "R5.AC4", "R5.AC5", "NFR3", "NFR4", "NFR5"]
      - command: ["make", "e2e"]
        expect_output: "E2E_CERTIFICATION=complete STATUS=passed"
        timeout: 90m
        covers: ["R4.AC4", "R4.AC6"]

- [x] 4. Retire unit tests and seal repository verification
  - [x] 4.1 After every migration-matrix replacement passes, remove the dedicated API and discovery unit-test files and fixtures, add the structural no-unit-test verifier, and integrate it into the canonical repository verification target.
    - Requirements: `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `NFR4`, `NFR5`, `NFR6`
    - Design: Test-layer classifier and retirement gate; Unit-retirement proof
    - Verification:
      - command: ["./hack/test-layer-policy-acceptance.sh"]
        expect_output: "Test-layer policy acceptance passed"
        covers: ["R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "NFR4", "NFR5", "NFR6"]
      - command: ["sh", "-c", "test ! -e api/v1alpha1/kubeseer_types_test.go && test ! -e internal/discovery/contracts_test.go && test ! -e internal/discovery/resolver_test.go"]
        covers: ["R7.AC1", "R7.AC2"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make verify"]
        expect_output: "Test layer policy passed"
        covers: ["R7.AC1", "R7.AC3", "NFR6"]

  - [x] 4.2 Run the final bootstrap, API compatibility, race, build, and module-tidiness matrix through the canonical entry points and keep local and CI command semantics identical.
    - Requirements: `R2.AC5`, `R2.AC6`, `R3.AC3`, `R3.AC5`, `R3.AC6`, `R4.AC3`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R5.AC6`, `R6.AC4`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`
    - Design: Repository test command surface; Verification Plan; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make test"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        timeout: 20m
        covers: ["R2.AC6", "R3.AC3", "R3.AC5", "R3.AC6", "R4.AC3", "R4.AC5", "R4.AC6", "R4.AC7", "R5.AC6", "R6.AC4", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make test-integration"]
        expect_output: "TEST_LAYER=module-integration STATUS=passed"
        covers: ["R2.AC5", "R2.AC6", "R4.AC6"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make test-api GO_TEST_FLAGS=-race"]
        expect_output: "TEST_LAYER=kubernetes-api STATUS=passed"
        timeout: 25m
        covers: ["R3.AC6", "R5.AC6", "NFR2"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod make test-compatibility"]
        expect_output: "API compatibility matrix passed"
        timeout: 35m
        covers: ["R4.AC3", "R4.AC5", "NFR2", "NFR3"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod go build ./..."]
        covers: ["R1.AC2"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-testing-foundation-go-build GOMODCACHE=/tmp/kubeseer-testing-foundation-go-mod go mod tidy -diff"]
        covers: ["NFR2"]
