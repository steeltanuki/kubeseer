---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-26T10:02:20Z
last_modified: 2026-09-26T10:07:25Z
approved_fingerprint: sha256:4675bb3f0e7273982936000861a8c5ecc61463fe73441311490bd3188491f4f4
source_design_approved_at: 2026-09-26T09:59:29Z
source_design_fingerprint: sha256:bd76f11a1a74a13ec633c3cdd3532d370b9705890b6254dc914a5e03632ecd90
---

# Implementation Plan

The leaves implement release automation and its fixture evidence. Actual GHCR
and GitHub Release publication remains an explicit maintainer tag action;
fixture proofs do not claim that a public release already exists.
Tasks 1–5 record the completed initial `develop`-lineage implementation.
Task 6 corrects the release lineage to protected `main`; its new assertions
must pass before the changed contract has current evidence. GitHub branch and
tag rulesets, promotion, and the actual release tag remain maintainer actions.

- [x] 1. Establish release intent and source identity
  - [x] 1.1 Add the strict tag/source/notes validator and no-public-write policy fixtures
    - Add `hack/release-distribution.sh validate` with exact stable SemVer parsing,
      tag peeling, protected `develop` ancestry, immutable source/version
      comparison, and tagged maintainer note preflight. It must reject source
      edits, unsupported refs, and every identity mismatch before publication.
    - Start `hack/test-release-distribution.sh` and
      `make test-release-distribution` with synthetic Git repositories and
      event contexts. Assert malformed tags, moved tags, untrusted lineage,
      version mismatch, missing notes, no tag creation, and no registry/Release
      write on PR, branch, or local contexts. Emit the policy marker only after
      every named case runs.
    - Requirements: `R1.AC5`, `R1.AC6`, `R1.AC7`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC8`, `R2.AC9`, `R9.AC5`, `R9.AC6`, `NFR1`, `NFR2`, `NFR4`, `C5`, `C6`
    - Design: Architecture / Release identity and staged candidates; Architecture / Permissions and public verification; Failure Modes And Tradeoffs
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=policy"]
        expect_output: "RELEASE_DISTRIBUTION=policy STATUS=passed"
        timeout: 20m
        covers: ["R1.AC5","R1.AC6","R1.AC7","R2.AC1","R2.AC2","R2.AC3","R2.AC4","R2.AC8","R2.AC9","R9.AC5","R9.AC6","NFR1","NFR2","NFR4","C5","C6"]

- [x] 2. Stage and inspect the approved release candidates
  - [x] 2.1 Build the tagged production image and package the one canonical chart
    - Add read-only `make verify-release-source TAG=vX.Y.Z` around the script's
      `stage` command. Use the existing Dockerfile, exact commit/version build
      inputs, approved amd64 platform, canonical Helm package command, and
      run-owned temporary output. Inspect executable output, OCI labels,
      platform, entrypoint/security, archive name/content, chart metadata, and
      default image; reject arm64 claims from the current Dockerfile.
    - Extend candidate fixtures to build an actual temporary image and chart
      from a synthetic tagged checkout, inject each mismatched identity, prove
      the worktree remains unchanged, and emit a candidate marker only after
      non-vacuous image/chart checks. Preserve the existing packaging contract
      rather than adding alternate templates or version rewrites.
    - Requirements: `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC10`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC7`, `NFR1`, `NFR3`, `NFR6`, `C1`, `C3`, `C7`, `C8`
    - Design: Architecture / Release identity and staged candidates; Architecture / Mandatory gates and environment; Verification Plan
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=candidate"]
        expect_output: "RELEASE_DISTRIBUTION=candidate STATUS=passed"
        timeout: 45m
        covers: ["R2.AC5","R2.AC6","R2.AC7","R2.AC10","R4.AC2","R4.AC3","R4.AC4","R4.AC5","R5.AC2","R5.AC3","R5.AC4","R5.AC7","NFR1","NFR3","NFR6","C1","C3","C7","C8"]

- [x] 3. Publish and audit immutable versioned artifacts
  - [x] 3.1 Implement remote inventory, ordered GHCR/Release publication, and public audit
    - Add `inventory`, `publish-image`, `publish-chart`,
      `publish-release`, and read-only `audit` modes to the same script.
      Distinguish absent references from auth failures; compare image manifest
      digest/revision and pulled chart archive bytes before writing. Serialize
      per-version writes, push only missing versions, verify each public pull,
      render the pulled OCI chart, preserve the `latest` baseline, and create
      the GitHub Release last from tagged notes and recorded digests using
      `--verify-tag` or an equivalent existing-tag-only API operation.
    - Extend the fixture harness with a disposable OCI registry and recorded
      GitHub responses. Assert initial success, identical rerun, conflicting
      image/chart/Release, partial image-only and chart-only recovery, moved
      tag, private package, missing post-publication artifact, no replacement
      writes, and release-page identity. Emit a transaction marker only after
      all cases prove exact write logs and read-after-write outcomes.
    - Requirements: `R4.AC1`, `R4.AC6`, `R4.AC7`, `R5.AC1`, `R5.AC5`, `R5.AC6`, `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R9.AC7`, `NFR1`, `NFR2`, `NFR3`, `NFR5`, `C4`, `C9`
    - Design: Architecture / Registry, Release, and user interfaces; Architecture / Publication transaction and retry rules; Architecture / Permissions and public verification; Failure Modes And Tradeoffs
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=transaction"]
        expect_output: "RELEASE_DISTRIBUTION=transaction STATUS=passed"
        timeout: 50m
        covers: ["R4.AC1","R4.AC6","R4.AC7","R5.AC1","R5.AC5","R5.AC6","R6.AC1","R6.AC2","R6.AC3","R6.AC4","R6.AC5","R6.AC6","R7.AC1","R7.AC2","R7.AC3","R7.AC4","R7.AC5","R7.AC6","R7.AC7","R7.AC8","R7.AC9","R7.AC10","R7.AC11","R8.AC1","R8.AC2","R8.AC3","R8.AC4","R8.AC5","R8.AC6","R8.AC7","R8.AC8","R9.AC7","NFR1","NFR2","NFR3","NFR5","C4","C9"]

- [x] 4. Wire CI and the official tag workflow with separate permissions
  - [x] 4.1 Add the two GitHub Actions workflows and gate-policy assertions
    - Add `.github/workflows/ci.yml` for PR/branch validation with
      `contents: read` and no publication step. Add
      `.github/workflows/release.yml` for protected stable tag pushes only:
      a read-only gate job checks out the exact tag commit and runs the
      selected repository commands; a dependent, per-version-serialized
      publish job checks out the same SHA and receives only
      `contents: write` and `packages: write`. Pin action dependencies to
      full commits; do not use privileged PR triggers or persistent secrets.
    - Add workflow fixtures that parse both workflow files and simulate PR,
      branch, malformed tag, failed/zero-match gate, and valid tag events.
      Assert job dependencies, exact command set, permissions, source SHA
      handoff, no automatic tag creation, and no publication path for
      development events. Emit the workflow marker only after all cases run.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R9.AC1`, `R9.AC2`, `R9.AC3`, `R9.AC4`, `NFR2`, `NFR4`, `NFR6`, `C2`, `C4`, `C10`
    - Design: Architecture; Architecture / Mandatory gates and environment; Architecture / Permissions and public verification; Options Considered
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=workflows"]
        expect_output: "RELEASE_DISTRIBUTION=workflows STATUS=passed"
        timeout: 25m
        covers: ["R1.AC1","R1.AC2","R1.AC3","R1.AC4","R3.AC1","R3.AC2","R3.AC3","R3.AC4","R3.AC5","R3.AC6","R3.AC7","R9.AC1","R9.AC2","R9.AC3","R9.AC4","NFR2","NFR4","NFR6","C2","C4","C10"]

- [x] 5. Publish the released-version installation path
  - [x] 5.1 Document the OCI install flow and close the source/local path checks
    - Update `README.md` and installation documentation so the public
      version-pinned Helm OCI command is the preferred released-version path.
      Keep the source-build procedure and existing kind/Podman local workflow
      distinct. Document GHCR package visibility, stable tag preparation,
      source-controlled `docs/releases/v<version>.md` notes, supported
      Kubernetes/Helm/platform claims, partial-release diagnostics, explicit
      rerun recovery, and the absence of a separate Release chart archive.
    - Add documentation and complete-flow fixture assertions for the public
      references, commands, metadata/upgrade pointers, and no attached
      `.tgz` path. The final fixture must exercise the resulting
      release-script audit independently of publication. Record that live
      GHCR/GitHub acceptance becomes available only after a maintainer
      explicitly creates a qualifying tag.
    - Requirements: `R6.AC7`, `R6.AC8`, `NFR5`, `C1`, `C7`, `C8`
    - Design: Architecture / Registry, Release, and user interfaces; Simplicity And Elegance Review; Verification Plan
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=docs"]
        expect_output: "RELEASE_DISTRIBUTION=docs STATUS=passed"
        timeout: 15m
        covers: ["R6.AC7","R6.AC8","NFR5","C1","C7","C8"]

- [x] 6. Make protected `main` the release source
  - [x] 6.1 Rebind source validation and release fixtures to `main`
    - Change `hack/release-distribution.sh validate_source` to require the
      peeled tag commit in fetched `origin/main`, while retaining exact SHA,
      clean checkout, protected-tag, and version checks. An older tag remains
      valid after `main` advances; a commit present only on `develop` fails.
    - Update the synthetic repositories used by policy, candidate, and
      transaction fixtures to provide `origin/main`. Add policy cases for a
      promoted `main` tag, a `develop`-only tag, missing `origin/main`, an
      earlier tag after `main` advances, and the unsupported `stable` alias.
      Emit `PASS release-distribution/main-lineage-policy` only after those
      new assertions have all run.
    - Requirements: `R1.AC5`, `R2.AC2`, `R9.AC6`, `NFR1`, `NFR2`, `C6`
    - Design: Architecture; Architecture / Release identity and staged candidates; Failure Modes And Tradeoffs; Verification Plan
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=policy"]
        expect_output: "PASS release-distribution/main-lineage-policy"
        timeout: 20m
        covers: ["R1.AC5","R2.AC2","R9.AC6","NFR1","NFR2","C6"]
      - command: ["make", "test-release-distribution", "SCENARIO=candidate"]
        expect_output: "RELEASE_DISTRIBUTION=candidate STATUS=passed"
        timeout: 45m
        covers: ["R2.AC2","C6"]
      - command: ["make", "test-release-distribution", "SCENARIO=transaction"]
        expect_output: "RELEASE_DISTRIBUTION=transaction STATUS=passed"
        timeout: 50m
        covers: ["R9.AC6","C6"]
  - [x] 6.2 Prove `main` branch and floating tags cannot publish
    - Add `main` push and non-versioned `stable` tag events to
      `hack/verify-release-workflows.go`. Assert ordinary CI runs for the
      branch, neither event enters publication, and the versioned-tag gates
      and publish-job permissions remain unchanged. Update the harness's
      exact event-case count so the new cases cannot be skipped silently.
    - Requirements: `R1.AC2`, `R1.AC5`, `R1.AC7`, `C10`
    - Design: Architecture; Architecture / Permissions and public verification; Verification Plan
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=workflows"]
        expect_output: "RELEASE_WORKFLOW_POLICY=passed CASES=17"
        timeout: 25m
        covers: ["R1.AC2","R1.AC5","R1.AC7","C10"]
  - [x] 6.3 Document promotion and version-tag creation from `main`
    - Update `docs/installation.md` and `CONTRIBUTING.md` to identify
      `develop` as integration, protected `main` as the latest promoted
      release-source branch, and the maintainer's reviewed promotion, CI
      result, confirmation of active `main` and `v*` rulesets, then version-tag
      push from `main`. Preserve the rule that branch pushes and floating
      `stable`/`latest` aliases publish nothing.
    - Extend the documentation fixture to assert the branch roles, promotion
      order, main ancestry, ruleset prerequisite, and no-floating-alias guidance. Emit
      `PASS release-distribution/docs-main-promotion-contract` only after
      those assertions pass.
    - Requirements: `R1.AC2`, `R1.AC5`, `R6.AC9`, `C6`
    - Design: Architecture / Registry, Release, and user interfaces; Verification Plan
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=docs"]
        expect_output: "PASS release-distribution/docs-main-promotion-contract"
        timeout: 15m
        covers: ["R1.AC2","R1.AC5","R6.AC9","C6"]

- [x] 7. Keep ordinary CI policy checks semantic
  - [x] 7.1 Verify required CI properties without structural step counts
    - Apply the changed-file-aware ordinary CI workflow and update
      `hack/verify-release-workflows.go` to retain checks for required
      validation commands, trigger coverage, approved pinned actions,
      read-only permissions, and forbidden publication operations without
      requiring an exact total number of shell steps.
    - Extend the workflow harness with temporary workflow fixtures. Prove that
      an additional non-publishing conditional shell step passes, while a
      missing required validation command and an injected publication command
      fail. Emit a dedicated semantic-policy marker only after all outcomes
      are observed.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC8`, `R9.AC1`
    - Design: Architecture; Options Considered; Failure Modes And Tradeoffs; Verification Plan
    - Verification:
      - command: ["make", "test-release-distribution", "SCENARIO=workflows"]
        expect_output: "PASS release-distribution/semantic-ci-workflow-policy"
        timeout: 25m
        covers: ["R1.AC1","R1.AC2","R1.AC3","R1.AC8","R9.AC1"]
