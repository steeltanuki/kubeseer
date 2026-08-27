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

### 2026-08-25T14:06:28Z | integration-testing-foundation | execute
- Trigger: A broad package test was run without the envtest asset contract
- Lesson: Package-local API tests include an envtest contract that intentionally fails when KUBEBUILDER_ASSETS is absent; direct go test is not the repository entry point for that package.
- Guardrail: Use the task proof and make test-api for envtest-backed packages; use go test -run '^$' only for compile-only checks when assets are unavailable.

### 2026-08-25T14:10:59Z | integration-testing-foundation | execute
- Trigger: The temporary runner acceptance fixture exposed a different Go diagnostic for a missing package
- Lesson: go test -list reports missing directories as stat <path>: directory not found rather than always using the pattern prefix
- Guardrail: Classify both pattern and stat directory-not-found diagnostics as not-applicable, while propagating other list failures.

### 2026-08-25T14:11:16Z | integration-testing-foundation | execute
- Trigger: The runner acceptance compile-error assertion assumed a compiler wording
- Lesson: Go parser failures may report expected tokens without the phrase syntax error
- Guardrail: Assert the stable failed marker and non-zero propagation plus a broad parser diagnostic, not one exact compiler sentence.

### 2026-08-25T14:12:51Z | integration-testing-foundation | execute
- Trigger: Public layer targets exposed the glob-directory missing diagnostic from go test
- Lesson: An absent wildcard package can be reported as lstat <path>: no such file or directory, which is a not-applicable state rather than a compile failure
- Guardrail: Classify lstat/stat missing-path diagnostics as not-applicable while continuing to propagate actual compiler and tooling errors.

### 2026-08-25T14:13:22Z | integration-testing-foundation | execute
- Trigger: The acceptance harness fixture overlapped the real module-integration glob during concurrent checks
- Lesson: Temporary Go packages under a selected test-layer directory can be mistaken for real suites and alter probe results
- Guardrail: Keep acceptance fixtures outside every production layer glob and clean only their exact temporary ownership path.

### 2026-08-25T15:24:20Z | integration-testing-foundation | execute
- Trigger: make verify reached controller-gen module verification but the restricted sandbox blocked proxy.golang.org DNS
- Lesson: A writable Go build cache is not sufficient for repository verification when controller-gen must verify a module through the checksum database; the proof can fail before the changed verifier runs.
- Guardrail: Preflight Go tool dependencies and rerun make verify with approved network access when the pinned controller tool is not already fully cached; distinguish dependency transport failures from repository verification failures.

### 2026-08-25T20:12:32Z | integration-testing-foundation | execute
- Trigger: task 4.2 final go mod tidy -diff attempted dependency downloads and exited before producing a module diff
- Lesson: The final matrix can reach a cold or non-writable module cache even after build and envtest proofs pass; Walden then reports dependency-resolution failure as the task failure.
- Guardrail: Run the final Go tidiness proof with an explicit writable GOMODCACHE and network access, and distinguish module-resolution failures from an actual go.mod/go.sum diff before changing dependencies.

### 2026-08-25T20:28:26Z | resource-discovery | execute
- Trigger: walden verify failed after integration-testing-foundation removed the package-local discovery tests
- Lesson: The discovery implementation still passes the consolidated envtest suite, but the approved resource-discovery proofs target deleted unit-test names and the race proof omits KUBEBUILDER_ASSETS.
- Guardrail: When a testing foundation changes test-layer ownership, update dependent Walden proof commands to the surviving higher-layer suite before re-verification.

### 2026-08-01T21:42:53Z | installation-access-policy | requirements
- Trigger: A manual acceptance-criterion scan passed Markdown backticks through an unsafe shell regex
- Lesson: Backticks inside a double-quoted shell pattern are command substitutions, so a read-only quality scan can execute unintended text and distort its pattern.
- Guardrail: Pass Markdown-aware rg patterns in single quotes and keep shell metacharacters literal during spec quality scans.

### 2026-08-02T08:21:08Z | installation-access-policy | requirements
- Trigger: User rejected the generic singleton name default during design review
- Lesson: A singleton resource name is part of the operator UX and should communicate the policy role, not merely its cardinality
- Guardrail: Name installation-wide singleton resources after their administrative function before approving requirements

### 2026-08-25T10:48:26Z | installation-access-policy | execute
- Trigger: Generated CRD validation markers were accepted syntactically but item constraints were absent from the manifest
- Lesson: controller-gen v0.20.1 uses the lowercase validation:items: marker prefix for array-item MaxLength and Pattern; the uppercase form can be ignored without producing item schema
- Guardrail: After adding controller-gen markers, inspect the generated OpenAPI item schema and assert the intended constraints before treating generation as complete
### 2026-08-25T20:38:20Z | installation-access-policy | execute
- Trigger: Execution preflight found that the approved task proofs add package-local unit tests rejected by the newer integration-testing foundation on develop
- Lesson: Feature execution plans can become operationally obsolete when a later approved repository-wide testing contract changes the permitted proof layer without changing the feature fingerprints
- Guardrail: Before starting an older approved feature, compare its proof commands and test locations with the current constitution and testing-foundation gates; reconcile from the earliest conflicting phase

### 2026-08-25T20:39:09Z | installation-access-policy | design
- Trigger: Post-reconciliation diff check found trailing spaces in blank approval frontmatter emitted by Walden v0.10.1
- Lesson: A deterministic workflow mutation can still introduce repository-formatting defects that validation does not report
- Guardrail: Run git diff --check after reconciliation and normalize blank frontmatter values before opening review
### 2026-08-26T07:59:06Z | resource-discovery | execute
- Trigger: Walden verify found approved proofs to be obsolete after the migration to higher-layer testing and sandbox environmental limitations
- Lesson: Re-verification cannot restore evidence when tasks point to removed package-local tests or when envtest and Go dependencies are blocked by the environment
- Guardrail: Before verifying an approved feature, compare every proof with the current constitution and testing foundation; run envtest and controller-tools in an environment with sockets, a writable cache, and network access available

### 2026-08-26T08:17:07Z | integration-testing-foundation | execute
- Trigger: The goal scope explicitly excluded implementing tasks added or modified during the consistency recovery
- Lesson: Walden reconciliation can update contracts and proofs without authorizing new code; planning approval and implementation remain separate boundaries
- Guardrail: During consistency recoveries, always distinguish re-verifying completed tasks from implementing new work and stop if a proof requires code that does not exist

### 2026-08-26T08:51:58Z | resource-selection | requirements
- Trigger: Initial requirements replacement used delete and add operations on the same path, which the patch mechanism rejected
- Lesson: A scaffold replacement must use one update operation when multiple operations cannot target the same file
- Guardrail: Replace an existing Walden scaffold with a single Update File patch
### 2026-08-26T14:51:32Z | field-extraction | execute
- Trigger: Initial integration proof exposed empty-match and cancellation-fixture assumptions
- Lesson: Extraction tests must distinguish absent nil matches from allocated empty slices, and synthetic cancellation contexts can interrupt a source when their checkpoint budget is calibrated to an internal call count.
- Guardrail: Assert the explicit zero-match contract and design cancellation fixtures at the observable batch boundary; rerun the targeted higher-layer suite before recording task evidence.
### 2026-08-26T16:06:07Z | typed-output-model | execute
- Trigger: Initial task 1.1 envtest proof failed while compiling the new API contract helper
- Lesson: A large envtest helper can hide declaration and shadowing errors until the full API proof starts
- Guardrail: Run gofmt and a compile-only package check immediately after adding each new envtest helper before starting the local control plane
### 2026-08-26T19:17:37Z | reconciliation-runtime | design
- Trigger: Design recovery found design.md missing after a scaffold replacement was split into destructive steps
- Lesson: After a combined replacement is rejected, deleting the existing Walden document alone can leave the approval chain structurally incomplete
- Guardrail: Use one Update File patch while a document exists; if it is already missing, recreate it with one Add File patch before any review transition
### 2026-08-27T08:34:26Z | status-and-conditions | tasks
- Trigger: Initial task proofs exposed a wrapper-only envtest socket restriction and a bind-error path that overwrote an explicit denial assessment.
- Lesson: Run the exact Walden proof wrapper and inspect unfiltered failures before retrying completion; preserve terminal assessments when later adaptation reports a related internal error.
- Guardrail: Keep task-scoped caches and authorized envtest execution available, and add regression assertions for each terminal assessment before marking the task complete.

