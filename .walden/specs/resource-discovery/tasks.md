---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-01T11:52:07Z
last_modified: 2026-08-01T12:01:35Z
approved_fingerprint: sha256:68f192b1c29b8d1f70f322837a1111a14850cc8f482012f34562e839d6bae188
source_design_approved_at: 2026-08-01T11:47:16Z
source_design_fingerprint: sha256:3758e9ed57d131cec58d72b1d2e659fabdf35d9552eb5f1f4c482f606ab5ea68
---

# Implementation Plan

- [ ] 1. Implement the discovery contracts and single-source resolver
  - [x] 1.1 Define source descriptors, scope/result values, and typed resolution errors
    - Requirements: `R1.AC2`, `R3.AC1`, `R3.AC2`, `R4.AC1`, `R4.AC2`, `R4.AC4`, `R6.AC4`, `NFR2`, `NFR3`
    - Design: Source descriptor; Resolution and error values; Data Models; Error Handling
    - Verification:
      - command: ["go", "test", "-v", "./internal/discovery/...", "-run", "^TestDiscoveryContracts$"]
        expect_output: "--- PASS: TestDiscoveryContracts"
        covers: ["R1.AC2", "R3.AC1", "R3.AC2", "R4.AC1", "R4.AC2", "R4.AC4", "R6.AC4", "NFR2", "NFR3"]

  - [x] 1.2 Implement single-source API identity resolution through the Kubernetes discovery client
    - Requirements: `R1.AC1`, `R2.AC1`, `R2.AC2`, `R3.AC3`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `NFR1`, `NFR2`, `NFR3`
    - Design: Discovery client adapter; Discovery resolver; Resolution and error values; Error Handling; Security Considerations
    - Verification:
      - command: ["go", "test", "-v", "./internal/discovery/...", "-run", "^TestResolve(Single|Core|Grouped|CRD|Scope|Ambiguous)$"]
        expect_output: "--- PASS: TestResolveSingle"
        covers: ["R1.AC1", "R2.AC1", "R2.AC2", "R3.AC3", "R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "NFR1", "NFR2", "NFR3"]

  - [ ] 1.3 Add batch resolution with independent source outcomes
    - Requirements: `R4.AC3`, `R4.AC4`, `NFR2`, `NFR3`
    - Design: Discovery resolver; Resolution and error values; Error Handling; Unit tests
    - Verification:
      - command: ["go", "test", "-v", "./internal/discovery/...", "-run", "^TestResolveBatch$"]
        expect_output: "--- PASS: TestResolveBatch"
        covers: ["R4.AC3", "R4.AC4", "NFR2", "NFR3"]

- [ ] 2. Implement the resolver-owned TTL cache and invalidation lifecycle
  - [ ] 2.1 Add a concurrency-safe TTL cache with an injectable clock and per-key refresh coordination
    - Requirements: `R5.AC1`, `R5.AC2`, `NFR1`, `NFR2`, `NFR3`
    - Design: TTL cache; Discovery resolver; Data Models; Failure Modes And Tradeoffs; Unit tests
    - Verification:
      - command: ["go", "test", "-v", "./internal/discovery/...", "-run", "^TestCache(FreshHit|ExpiryRefresh|ConcurrentRefresh)$"]
        expect_output: "--- PASS: TestCacheFreshHit"
        covers: ["R5.AC1", "R5.AC2", "NFR1", "NFR2", "NFR3"]

  - [ ] 2.2 Add explicit invalidation and one bounded stale-mapping rediscovery
    - Requirements: `R5.AC3`, `R5.AC4`, `R5.AC5`, `NFR1`, `NFR2`, `NFR3`
    - Design: TTL cache; Discovery resolver; Error Handling; Failure Modes And Tradeoffs; Cache proof
    - Verification:
      - command: ["go", "test", "-v", "./internal/discovery/...", "-run", "^TestCacheInvalidation$"]
        expect_output: "--- PASS: TestCacheInvalidation"
        covers: ["R5.AC3", "R5.AC4", "R5.AC5", "NFR1", "NFR2", "NFR3"]

- [ ] 3. Prove live Kubernetes discovery compatibility and quality boundaries
  - [ ] 3.1 Add an envtest discovery smoke test for built-in, cluster-scoped, and CRD-backed resources
    - Requirements: `R2.AC1`, `R2.AC2`, `R3.AC1`, `R3.AC2`, `R6.AC1`, `R6.AC2`, `NFR1`, `NFR3`
    - Design: Testing Strategy — Integration tests; Verification Plan — Compatibility proof; Security Considerations
    - Verification:
      - command: ["sh", "-c", "KUBEBUILDER_ASSETS=\"$(./hack/envtest-assets.sh 1.35.6)\" go test -v ./internal/discovery/... -run '^TestEnvtestDiscovery$'"]
        expect_output: "--- PASS: TestEnvtestDiscovery"
        timeout: 20m
        covers: ["R2.AC1", "R2.AC2", "R3.AC1", "R3.AC2", "R6.AC1", "R6.AC2", "NFR1", "NFR3"]

  - [ ] 3.2 Add deterministic race and static verification for the discovery package
    - Requirements: `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R6.AC3`, `R6.AC4`, `NFR1`, `NFR2`, `NFR3`
    - Design: Simplicity And Elegance Review; Testing Strategy — Static and quality checks; Verification Plan — Requirement proof and Security proof
    - Verification:
      - command: ["go", "test", "-race", "-v", "./internal/discovery/..."]
        expect_output: "PASS"
        covers: ["R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R6.AC3", "R6.AC4", "NFR1", "NFR2", "NFR3"]
