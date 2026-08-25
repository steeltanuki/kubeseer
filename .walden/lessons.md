# Walden Lessons

Review this file before non-trivial work when the current request matches past mistakes, rejections, or validation failures.

## Lessons

<!-- Append entries with: walden lesson log --feature <name> --phase <phase> --trigger "..." --lesson "..." --guardrail "..." -->
### 2026-08-01T09:14:17Z | kubeseer-api-foundation | tasks
- Trigger: Initial task validation after requirements and design approval reported missing coverage from the placeholder scaffold
- Lesson: A valid scaffold becomes incomplete as soon as approved upstream documents introduce the real requirement set
- Guardrail: Before opening task review, require both task_reference_coverage and proof_reference_coverage to be complete in walden validate --json
### 2026-08-01T09:31:49Z | kubeseer-api-foundation | execute
- Trigger: Initial task 1.1 proof exposed non-semantic test assumptions
- Lesson: A JSON round-trip test compared metav1.Time locations and a pointer to an empty result struct by identity, causing false failures despite semantic preservation
- Guardrail: Compare Kubernetes time values by instant and test deep-copy isolation only through mutable fields; do not use pointer identity for zero-size structs
### 2026-08-01T09:45:48Z | kubeseer-api-foundation | execute
- Trigger: Initial task 1.3 generation proof could not resolve the pinned controller tool in the sandbox
- Lesson: Read-only generation verification depends on Go module resolution and a writable build/module cache even though it writes no repository artifacts
- Guardrail: Run generation proofs with a writable cache and preserve the controller-gen version in the canonical Makefile used by both generation and verification
### 2026-08-01T10:21:39Z | kubeseer-api-foundation | execute
- Trigger: setup-envtest did not publish the approved Kubernetes 1.35.6 patch archive
- Lesson: Treat controller-tools envtest archive availability as separate from Kubernetes release availability; an exact API-server patch can exist without a matching bundled archive.
- Guardrail: Keep a versioned direct-asset resolver fallback that pins the Kubernetes API binaries and the etcd family when the setup-envtest index has no exact patch.
### 2026-08-01T11:55:20Z | resource-discovery | execute
- Trigger: Initial task 1.1 contract proof exposed Kubernetes schema parsing and core GVR formatting assumptions
- Lesson: schema.ParseGroupVersion accepts a group with an empty version, and core GroupVersionResource.String includes an empty group prefix.
- Guardrail: Validate both parse success and a non-empty version, and assert core GVRs from the Kubernetes type rather than hand-written display strings.
### 2026-08-01T12:01:26Z | resource-discovery | execute
- Trigger: The initial scope test reused the discovery error variable after a short declaration
- Lesson: A test assertion can accidentally inspect the outer nil error instead of the guard error, causing a panic rather than validating the contract.
- Guardrail: Name guard errors explicitly before asserting their reason and message; never reuse an outer error variable in follow-up assertions.

### 2026-08-01T13:47:19Z | resource-discovery | execute
- Trigger: The envtest discovery proof could not start its local control plane inside the sandbox
- Lesson: The Kubernetes API-server smoke test requires local sockets that the restricted sandbox denies even when all assets are available.
- Guardrail: Run envtest proofs with the required local-process permission and distinguish socket policy failures from discovery implementation failures.

### 2026-08-01T13:49:48Z | resource-discovery | execute
- Trigger: The task 3.2 race proof ran the package without envtest assets and exposed a refresh handoff race
- Lesson: Integration tests must skip explicitly when their required assets are absent, and refresh ownership must re-check a cache populated between the initial miss and lock acquisition to prevent a second discovery.
- Guardrail: Gate envtest tests on KUBEBUILDER_ASSETS and perform an atomic fresh-entry check while acquiring each per-key refresh slot.

### 2026-08-01T13:50:07Z | resource-discovery | execute
- Trigger: Walden task completion inherited a read-only default Go build cache
- Lesson: A proof can pass with explicit writable caches while the same command fails when Walden launches Go with the sandbox default cache.
- Guardrail: Use writable GOCACHE and GOMODCACHE paths for every Go-backed Walden completion or verification in restricted environments.

### 2026-08-25T13:11:23Z | integration-testing-foundation | design
- Trigger: The user removed the unit-test layer during design review because AI-generated code will be verified at higher test layers
- Lesson: The required test layers are a product-level maintenance and confidence decision that must be fixed in requirements before designing the harness
- Guardrail: Before opening design review for a testing foundation, confirm which test layers are mandatory and record the accepted diagnostic and coverage trade-off in requirements

### 2026-08-25T13:33:08Z | integration-testing-foundation | tasks
- Trigger: Task planning found that api/v1alpha1 and internal/discovery have no production collaboration path, so the required non-vacuous module suite would need artificial production code or a test-only composition
- Lesson: A testing foundation must define its bootstrap behavior for the period before the first real cross-module collaboration path exists
- Guardrail: Before design review, inventory actual production collaboration edges rather than package count and specify the default local test layer when that edge count is zero

