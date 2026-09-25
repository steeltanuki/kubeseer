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

### 2026-08-27T09:24:41Z | authorization-enforcement | requirements
- Trigger: The initial scaffold replacement repeated a rejected delete-and-add patch shape for one path
- Lesson: Reviewing a known patch guardrail is insufficient unless it becomes a pre-edit operation check
- Guardrail: Before replacing any Walden scaffold, use exactly one Update File operation and verify that no patch targets the same path twice

### 2026-08-31T08:04:24Z | cross-namespace-aggregation | requirements
- Trigger: Opening Requirements review inserted trailing whitespace in empty YAML approval fields, detected by git diff --check
- Lesson: Walden review transitions can leave whitespace-only values in empty frontmatter fields
- Guardrail: After every walden review open transition, run git diff --check and normalize empty frontmatter fields before handoff

### 2026-08-31T08:15:26Z | cross-namespace-aggregation | design
- Trigger: Designing exact average exposed that valid decimal inputs can have a non-terminating arithmetic mean
- Lesson: An exact decimal aggregation requirement is incomplete unless it defines behavior for rational results without a finite decimal representation
- Guardrail: Before approving numeric aggregation Requirements, test zero, overflow, and non-terminating division cases against the public result representation

### 2026-08-31T08:23:03Z | cross-namespace-aggregation | requirements
- Trigger: Reconcile inserted trailing whitespace while clearing empty approval fields, extending the earlier review-open occurrence
- Lesson: Any Walden transition that clears frontmatter values can leave whitespace-only YAML fields
- Guardrail: After every Walden state transition that rewrites frontmatter, run git diff --check and normalize empty fields before handoff

### 2026-09-01T05:40:47Z | packaging-and-installation | requirements
- Trigger: User rejected the inferred Kustomize package and selected a Helm chart
- Lesson: The installation package format is a bifurcation-significant operator UX decision and cannot be inferred solely from an existing generated-manifest directory
- Guardrail: Before drafting packaging requirements, surface Helm versus Kustomize as an explicit decision checkpoint unless the user or an approved upstream contract has already selected the format

### 2026-09-01T05:54:06Z | packaging-and-installation | design
- Trigger: Post-approval feature validation immediately evaluated the untouched design scaffold and reported missing requirement coverage
- Lesson: After Requirements approval, walden validate advances to Design and an expected empty scaffold failure must not be misreported as a Requirements regression
- Guardrail: Validate Requirements before approval; after approval use walden status to confirm the gate and defer full validation until design.md has been authored

### 2026-09-01T05:59:49Z | packaging-and-installation | requirements
- Trigger: User confirmed cert-manager as the default but required a fallback when cert-manager cannot be installed
- Lesson: Selecting a preferred certificate provider does not resolve installation portability when some clusters prohibit that dependency; fallback ownership and rotation semantics are a separate bifurcation-significant requirement
- Guardrail: When a packaging design selects an external certificate controller, explicitly decide whether unsupported clusters fail installation or use a documented fallback before approving the certificate lifecycle contract

### 2026-09-01T06:07:47Z | packaging-and-installation | requirements
- Trigger: User requested an explicit CRD purge path although the draft only promised a generic command or manifest path
- Lesson: A destructive lifecycle contract is still ambiguous when it does not name a dedicated versioned entrypoint and explicit cluster-target confirmation
- Guardrail: For destructive packaging operations, require a separate executable entrypoint, explicit kubeconfig and context, typed confirmation, ordered deletion, and a final retained-or-deleted resource report

### 2026-09-01T06:35:27Z | packaging-and-installation | design
- Trigger: Initial phase inspection re-ran validation against the known incomplete Design scaffold
- Lesson: A fresh approved Requirements gate plus a draft placeholder Design predicts a coverage failure and should be inspected without invoking full feature validation
- Guardrail: When status identifies Design as current and the document is still a placeholder, inspect and author it first; run walden validate only after the required Design sections exist

### 2026-09-01T18:47:18Z | local-development-environment | design
- Trigger: Initial Design replacement targeted the existing scaffold with separate delete and add operations
- Lesson: A known single-update scaffold guardrail must be enforced in patch construction, not only reviewed before editing
- Guardrail: For every existing Walden scaffold, construct exactly one Update File operation and reject the patch locally if the path appears in more than one operation

### 2026-09-01T18:59:44Z | local-development-environment | tasks
- Trigger: Initial task sequence assigned catalog-to-values verification before the catalog task created its inputs
- Lesson: A proof can be structurally valid yet unrunnable at its execution point when it depends on artifacts introduced by a later task
- Guardrail: Before opening task review, walk leaf tasks in order and ensure every proof consumes only repository artifacts created by that task or an earlier one

### 2026-09-02T14:00:07Z | local-development-environment | execute
- Trigger: Full local proof exposed admission/example policy and cross-namespace cleanup conflicts
- Lesson: Runtime authorization-denial examples must be created under an admitted baseline, then exercise policy drift; multi-namespace manifests must be deleted without a forced single namespace.
- Guardrail: Keep local policy drift explicit and reversible around the probe, wait for initial readiness before fixture removal, and let manifest namespaces drive multi-namespace cleanup.

### 2026-09-02T17:03:51Z | integration-testing-foundation | tasks
- Trigger: The end-to-end-scenarios implementation made the foundation make e2e proof obsolete
- Lesson: When a later approved feature makes a previously unavailable test boundary executable, update the foundation proof to assert the real success contract.
- Guardrail: Keep foundation task proofs synchronized with the current executable entry point and re-open the Tasks approval gate before re-verification.

### 2026-09-02T17:17:08Z | integration-testing-foundation | execute
- Trigger: Initial post-approval verification ran without the recorded Podman runtime and network access
- Lesson: A valid proof can fail before reaching its assertion when the runner cannot access Podman runtime state or download uncached Go modules.
- Guardrail: Before full re-verification, compare environment probes and use the recorded writable caches, network access, and XDG_RUNTIME_DIR needed by the proof.

### 2026-09-03T08:45:54Z | local-development-environment | requirements
- Trigger: A contributor observed the Kubernetes 1.33+ core/v1 Endpoints deprecation warning during make local-up after the local workflow had been completed
- Lesson: An API-agnostic webhook reachability requirement allowed the implementation to depend on a deprecated Kubernetes resource across a compatibility matrix where the deprecation warning is expected
- Guardrail: Require local and E2E webhook readiness observers to use discovery.k8s.io/v1 EndpointSlice and prove that supported workflows do not request core/v1 Endpoints

### 2026-09-03T08:57:38Z | local-development-environment | design
- Trigger: A read-only memory keyword scan passed Markdown backticks inside a double-quoted shell pattern and attempted command substitution
- Lesson: A previously recorded shell-safety lesson was reviewed but not enforced while constructing a later search command
- Guardrail: Use single-quoted rg patterns whenever Markdown backticks are literal and reject double-quoted search patterns containing shell substitution syntax before execution

### 2026-09-03T09:08:04Z | local-development-environment | tasks
- Trigger: Task-plan non-vacuity review found that make test-local-environment can emit its generic success marker after taking the fake-tool fallback
- Lesson: A genuine-cluster proof is vacuous when its expected output is shared with a fallback that never contacts the Kubernetes API server
- Guardrail: Require a genuine-only terminal marker that the fake fallback cannot emit for every proof intended to certify live cluster behavior

### 2026-09-03T10:03:26Z | local-development-environment | execute
- Trigger: Genuine local acceptance encountered an existing persistently owned kubeseer-local cluster while using an isolated temporary state directory.
- Lesson: A temporary local acceptance state directory does not isolate the fixed cluster identity from a contributor-owned persistent cluster.
- Guardrail: Before genuine acceptance, inspect the fixed cluster identity; preserve an existing owned environment and run the proof with an explicit disposable cluster identity when cleanup is not authorized.

### 2026-09-03T10:28:20Z | local-development-environment | execute
- Trigger: Independent E2E observability scenario read the persistent local cluster because its fixed metrics and readiness port-forwards occupied 18080 and 18081.
- Lesson: Run-unique Kubernetes identity does not by itself isolate fixed host port-forwards from a persistent local workflow.
- Guardrail: Before E2E certification, reserve run-owned readiness and metrics ports or verify they are free; never accept a pre-existing listener as the E2E forward.

### 2026-09-10T07:16:17Z | native-scalar-conversion-compatibility | execute
- Trigger: walden verify encountered Podman profile drift in the sandbox
- Lesson: API envtest proofs can fail before assertions when the sandbox cannot initialize Podman, even though the same exact proof passes with the recorded runtime.
- Guardrail: Before re-verifying envtest-backed tasks, compare the recorded environment profile and run the exact proof with the required writable runtime directory and authorized Podman access.

### 2026-09-10T08:45:39Z | configuration-budget-status-invalidation | tasks
- Trigger: Sandbox envtest could not bind its local control-plane socket, and the route promotion proof exposed a blocking initial Replace fixture.
- Lesson: Run the exact envtest proof with approved local-socket escalation; keep initial Replace readiness ordering while making RemoveOwner promotion nonblocking, and drive deliberately stalled initial replacements asynchronously in tests.
- Guardrail: Before completion, rerun every exact Walden proof on the final code, inspect all RouteRegistry promotion callsites, and run the route race proof to catch hidden waits.

### 2026-09-10T11:58:24Z | watch-startup-cancellation | tasks
- Trigger: envtest ha rilevato WATCH duplicati dopo una risposta di establishment riuscita
- Lesson: Non cancellare il contesto di trasporto quando la risposta WATCH ha vinto la gara del timer: il contesto figlio deve restare supervisor-owned per tutta la vita dello stream.
- Guardrail: Separare il timer di establishment dalla cancellazione del trasporto; annullare il trasporto solo su timeout, rimozione dell'ultimo owner o shutdown e mantenere una prova envtest con uno stream attivo.

### 2026-09-23T07:02:25Z | local-cluster-resume | requirements
- Trigger: EARS validation warned about two state conditions placed after SHALL
- Lesson: State-limited recovery invariants were phrased as ubiquitous criteria with DURING or WHILE in the response
- Guardrail: Put WHILE preconditions before SHALL and inspect per-criterion EARS warnings before opening review

### 2026-09-23T07:32:39Z | local-cluster-resume | tasks
- Trigger: walden validate reported missing R3 task coverage despite R3 acceptance IDs in wrapped Requirements lists
- Lesson: Task coverage parsing depends on IDs on the same physical Requirements line; wrapped continuation lines can hide valid references.
- Guardrail: Keep each leaf task Requirements field on one physical line and validate task coverage before review.

### 2026-09-23T08:13:39Z | local-cluster-resume | tasks
- Trigger: genuine Podman proof showed kind provider diagnostics on stderr were included in the kind node inventory during API identity comparison
- Lesson: Combining provider stderr with machine-readable node output can fabricate inventory entries and cause false identity conflicts.
- Guardrail: Capture kind node inventory from stdout only and preserve stderr as diagnostics; exercise identity comparison on a real provider.

### 2026-09-23T08:25:09Z | local-cluster-resume | design
- Trigger: The approved run-unique genuine resume proof was blocked because cmd/kubeseer-local also hard-coded kubeseer-local and kind-kubeseer-local in its read-only ownership metadata validator
- Lesson: A private probe can duplicate a fixed project identity constraint and invalidate a run-unique integration contract even when its kubeconfig and context inputs are explicit
- Guardrail: When an approved genuine harness uses configurable resource identity, include the probe's independently expected identity interface and default, unique, and mismatch assertions in Design before implementation

### 2026-09-23T08:42:46Z | local-cluster-resume | execute
- Trigger: The genuine Podman run stopped in the harness ownership proof because the inspect template requested HostIp, while Podman reports its host port field as HostIP
- Lesson: Podman inspect template fields are case-sensitive Go struct fields; Docker-style HostIp is not interchangeable with Podman's HostIP
- Guardrail: Before using Podman inspect fields in ownership proofs, compare the template with real formatted output from the supported Podman provider and cover that proof with the genuine harness

### 2026-09-25T07:35:49Z | release-distribution | release
- Trigger: Hosted publication failed on a Podman option accepted locally; repeated Helm packaging changed archive hashes while recovery fixtures reused one candidate.
- Lesson: Release proofs must exercise runner-compatible publication commands and independently rebuilt candidates, not only event policy and retries of the same staged files.
- Guardrail: Run CLI capability checks before builds, exercise real registry transactions in CI, and assert image/chart digest equality after rebuilding the same tagged source.

