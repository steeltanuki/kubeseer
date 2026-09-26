---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-26T09:57:35Z
last_modified: 2026-09-26T09:57:35Z
approved_fingerprint: sha256:5d89ea48ec60f52a531da280d4bfd9bbae3e3d0b4d56dfe76be796f70145d861
---

# Requirements Document

## Introduction

The approved `packaging-and-installation` feature defines the production
controller image, one Helm v2 chart, and their local verification and lifecycle
contracts. It deliberately excludes publication. This feature makes an
explicitly chosen stable Kubeseer release installable from public GitHub-hosted
artifacts without a source checkout or a local build. It owns release intent,
identity, gates, publication, public verification, recovery, and the released
installation path. Packaging structure and Kubernetes lifecycle semantics stay
with `packaging-and-installation`.

The initial release platform is `linux/amd64`. The current production
`Dockerfile` hardcodes `GOARCH=amd64`; `linux/arm64` needs a separately approved
production-image correction and certification before it can be advertised or
published. This feature must not infer platform support from the release runner.

`develop` is the integration branch. The protected `main` branch holds the
latest promoted release source. A maintainer promotes reviewed changes to
`main` and creates each versioned release tag from that branch. The branch name
does not create an image tag or publish an artifact.

<!-- assumed: stable vMAJOR.MINOR.PATCH tags only, with no prerelease or build suffix, because no approved release contract requires prereleases -->
<!-- assumed: the initial official release carries no separate GitHub Release chart archive; the chart archive is a temporary input to the canonical Helm OCI publication -->

## Requirements

### R1 Explicit Release Intent And CI Boundary

**User Story:** As a contributor, I want routine validation to remain separate
from public publication, so that development activity cannot create an official
artifact.

#### Acceptance Criteria

1. `R1.AC1` WHEN a pull request is opened or updated, the system SHALL run applicable repository validation without publishing an official artifact.
   - Acceptance check: a PR run reports its validation result and creates no public image, chart, GitHub Release, or version tag.
2. `R1.AC2` WHEN a commit is pushed to `develop`, `main`, or another ordinary branch, the system SHALL run applicable validation without publishing an official artifact.
   - Acceptance check: a branch run leaves the public release registries, GitHub Releases, and official tags unchanged.
3. `R1.AC3` The system SHALL reserve public controller-image, Helm OCI, and GitHub Release publication for an explicit official release action.
   - Acceptance check: only a qualifying release action can reach publication operations.
4. `R1.AC4` WHEN validation needs a container image or chart archive, the system SHALL keep that temporary artifact outside the public release repositories.
   - Acceptance check: integration and E2E validation can build locally while public release references remain absent.
5. `R1.AC5` The system SHALL never publish `latest`, `stable`, `develop`, `main`, `edge`, `snapshot`, PR-number, or equivalent development or floating image tags under this feature.
   - Acceptance check: the publication plan contains only the canonical immutable version tag.
6. `R1.AC6` The system SHALL never create official version tags as a side effect of CI or release automation.
   - Acceptance check: tag creation remains an explicit maintainer action.
7. `R1.AC7` IF the requested release action is not a qualifying `vMAJOR.MINOR.PATCH` tag, THEN the system SHALL stop before publishing any artifact.
   - Acceptance check: a branch event, malformed tag, or unsupported prerelease leaves all official endpoints unchanged.
8. `R1.AC8` WHEN repository policy checks an ordinary CI workflow, the system SHALL determine conformance from its required validation commands, approved action dependencies, read-only permissions, and absence of publication operations without requiring an exact total number of shell steps.
   - Acceptance check: a workflow fixture with an additional non-publishing conditional shell step passes, while removing a required command or introducing a publication operation still fails.

### R2 Canonical Version And Source Revision

**User Story:** As a user, I want every artifact to identify one release and one
source commit, so that I can verify what I installed.

#### Acceptance Criteria

1. `R2.AC1` WHEN an eligible tag has the form `vMAJOR.MINOR.PATCH`, the system SHALL derive the canonical application version by removing its leading `v`.
   - Acceptance check: `v0.1.0` yields exactly `0.1.0` throughout the release record.
2. `R2.AC2` WHEN an official release begins, the system SHALL resolve the tag to exactly one Git commit and use that commit for all release inputs.
   - Acceptance check: the image, chart, executable, notes, and Release identify the same full commit SHA.
3. `R2.AC3` The system SHALL require `Chart.yaml` `version` to equal the canonical application version before publication.
   - Acceptance check: a version mismatch stops the release with no public write.
4. `R2.AC4` The system SHALL require `Chart.yaml` `appVersion` to equal the canonical application version before publication.
   - Acceptance check: an application-version mismatch stops the release with no public write.
5. `R2.AC5` The system SHALL require controller image tag, executable version output, and OCI image version metadata to equal the canonical application version before publication.
   - Acceptance check: each identity reads `0.1.0` for `v0.1.0`, or publication stops.
6. `R2.AC6` The system SHALL require the controller image revision metadata to equal the resolved tag commit before publication.
   - Acceptance check: the image revision is the full resolved commit SHA.
7. `R2.AC7` The system SHALL require the Helm archive used for OCI publication to be named `kubeseer-<version>.tgz` and contain that same chart and application version.
   - Acceptance check: the staged archive for `v0.1.0` is `kubeseer-0.1.0.tgz` with both fields `0.1.0`.
8. `R2.AC8` The system SHALL leave source-controlled version declarations unchanged throughout an official release.
   - Acceptance check: release execution produces no edit to tagged `Chart.yaml` or other committed version declarations.
9. `R2.AC9` IF any required version, source, or archive identity disagrees with the tag, THEN the system SHALL reject publication before its first public write.
   - Acceptance check: each seeded mismatch fails at prepublication validation.
10. `R2.AC10` The system SHALL expose project, version, source repository, and full source revision through release artifact metadata or its immutable release identity record.
   - Acceptance check: a user can follow published image labels and the release's recorded chart digest to the same tagged commit.

### R3 Mandatory Prepublication Gates

**User Story:** As a release maintainer, I want existing certification to gate
publication, so that a release cannot bypass approved product checks.

#### Acceptance Criteria

1. `R3.AC1` WHEN an official release is requested, the system SHALL execute the approved repository verification gate on the tagged source.
   - Acceptance check: the gate reports its non-vacuous success for that exact commit.
2. `R3.AC2` WHEN an official release is requested, the system SHALL execute the approved cross-module test gate on the tagged source.
   - Acceptance check: the named higher-layer suite ran and passed for that commit.
3. `R3.AC3` WHEN an official release is requested, the system SHALL execute the approved Kubernetes API compatibility gate on the tagged source.
   - Acceptance check: every centrally declared supported Kubernetes version is reported as passed.
4. `R3.AC4` WHEN an official release is requested, the system SHALL execute the approved package compatibility gate on the tagged source.
   - Acceptance check: every approved Kubernetes and certificate-mode combination is reported as passed.
5. `R3.AC5` WHEN an official release is requested, the system SHALL execute the approved E2E harness acceptance and cluster certification gates on the tagged source.
   - Acceptance check: the harness contract and all mandatory product scenarios report passed.
6. `R3.AC6` IF a mandatory gate fails, is skipped, or matches no required scenario, THEN the system SHALL stop before publishing any official artifact.
   - Acceptance check: a forced failure or zero-match result leaves all public release endpoints unchanged.
7. `R3.AC7` The system SHALL use the repository's existing verification entry points as the authority for their behavior.
   - Acceptance check: release automation invokes the same approved commands used locally rather than carrying separate copies of their assertions.

### R4 Public Controller Image

**User Story:** As a platform operator, I want one retrievable versioned
controller image, so that the released chart can deploy without a local build.

#### Acceptance Criteria

1. `R4.AC1` WHEN an official release passes prepublication checks, the system SHALL publish the production controller image at `ghcr.io/steeltanuki/kubeseer:<version>`.
   - Acceptance check: `v0.1.0` yields an anonymously retrievable `ghcr.io/steeltanuki/kubeseer:0.1.0`.
2. `R4.AC2` The system SHALL build the official image from the exact tagged commit using the approved production image contract.
   - Acceptance check: the published image matches the tagged Dockerfile and approved entrypoint/security inspection.
3. `R4.AC3` The system SHALL preserve the packaging feature's non-root, minimal-runtime, immutable-version, source, and revision image requirements.
   - Acceptance check: inspection of the published image satisfies the existing package checks and expected OCI labels.
4. `R4.AC4` The system SHALL publish the initial image only for the certified `linux/amd64` platform.
   - Acceptance check: the public version manifest identifies `linux/amd64` and the release page claims no unapproved `linux/arm64` support.
5. `R4.AC5` IF an image build or published platform does not match the approved platform set, THEN the system SHALL reject that release artifact.
   - Acceptance check: an arm64 claim backed only by the current amd64 Dockerfile fails validation.
6. `R4.AC6` The system SHALL record the immutable digest of the published versioned image.
   - Acceptance check: the release audit can resolve the public tag to its recorded digest.
7. `R4.AC7` WHEN the image has been published, the system SHALL verify that the versioned reference can be pulled from GHCR.
   - Acceptance check: a fresh pull resolves the expected digest, version, revision, and entrypoint.

### R5 Public Canonical Helm Chart

**User Story:** As a cluster administrator, I want to install the released chart
from GHCR, so that no repository checkout or chart build is needed.

#### Acceptance Criteria

1. `R5.AC1` WHEN an official release passes prepublication checks, the system SHALL publish `charts/kubeseer` as `oci://ghcr.io/steeltanuki/charts/kubeseer` at the canonical version.
   - Acceptance check: `helm pull` with `--version 0.1.0` retrieves the canonical chart for `v0.1.0`.
2. `R5.AC2` The system SHALL use the tagged canonical chart and its existing verification contract as the only source for the published OCI chart.
   - Acceptance check: published templates, CRDs, values, and schema match the verified tagged chart.
3. `R5.AC3` The system SHALL require the published chart's `version` and `appVersion` to match the canonical release version.
   - Acceptance check: pulled chart metadata shows `0.1.0` for both fields at `v0.1.0`.
4. `R5.AC4` The system SHALL require the published chart's default controller image to resolve to the matching public release image.
   - Acceptance check: rendering the pulled chart with default image values yields `ghcr.io/steeltanuki/kubeseer:0.1.0` for `v0.1.0`.
5. `R5.AC5` WHEN a user installs a published chart version with the documented command, the system SHALL require no source checkout or local chart build.
   - Acceptance check: a clean client can install from the OCI reference and the public image.
6. `R5.AC6` The system SHALL record the immutable digest of the published Helm OCI version.
   - Acceptance check: the release audit can resolve the public chart version to its recorded digest.
7. `R5.AC7` The system SHALL generate any temporary `.tgz` input from the same verified chart content that is published to OCI.
   - Acceptance check: a pulled OCI chart has the staged archive's chart content and version.

### R6 GitHub Release And Installation Guidance

**User Story:** As a user, I want one release page with an install command and
compatibility information, so that I can select and deploy an official version.

#### Acceptance Criteria

1. `R6.AC1` WHEN the public image and chart have passed publication verification, the system SHALL publish the GitHub Release for the exact `v<version>` tag.
   - Acceptance check: the Release for `v0.1.0` points to the same tag commit as both artifacts.
2. `R6.AC2` The system SHALL include the Kubeseer version, source tag, and source commit in the GitHub Release.
   - Acceptance check: those three identities are visible on the release page.
3. `R6.AC3` The system SHALL include the supported Kubernetes range, required Helm version, and certified image platform in the GitHub Release.
   - Acceptance check: a user can read the exact declared prerequisites without opening the source tree.
4. `R6.AC4` The system SHALL include the controller image reference, Helm OCI reference, and immutable artifact digests in the GitHub Release.
   - Acceptance check: each public artifact can be located and matched to the recorded digest.
5. `R6.AC5` The system SHALL include a copy-pastable version-pinned Helm installation command in the GitHub Release.
   - Acceptance check: the command names the public OCI chart, version, release name, and namespace.
6. `R6.AC6` The system SHALL include maintainer-authored release notes and version-specific upgrade considerations or links in the GitHub Release.
   - Acceptance check: the page contains substantive notes from the tagged source and a relevant upgrade pointer.
7. `R6.AC7` The system SHALL document separate paths for official-release installation, building/installing from source, and project-owned kind-and-Podman local development.
   - Acceptance check: user-facing documentation presents the OCI install path first and labels the two source/local paths distinctly.
8. `R6.AC8` The system SHALL keep the Helm OCI artifact as the canonical chart distribution and omit a second GitHub Release chart archive in the initial feature.
   - Acceptance check: the Release points to the OCI chart and has no separately maintained chart `.tgz` asset.
9. `R6.AC9` The system SHALL document `develop` as the integration branch and protected `main` as the branch from which maintainers create versioned release tags after promotion.
   - Acceptance check: the maintainer guide describes promotion to `main` before tag creation and confirms that branch updates alone do not publish artifacts.

### R7 Immutability, Ordering, And Recovery

**User Story:** As a release maintainer, I want retries to be safe and partial
publication visible, so that a failed multi-artifact release cannot silently
change a version.

#### Acceptance Criteria

1. `R7.AC1` WHEN publication starts, the system SHALL inventory the existing versioned image, chart, and GitHub Release before writing any of them.
   - Acceptance check: the audit distinguishes absent, matching, and conflicting artifacts.
2. `R7.AC2` IF a versioned image already exists with a different digest or source revision, THEN the system SHALL reject an overwrite.
   - Acceptance check: the existing image remains unchanged and the release fails with the conflict identified.
3. `R7.AC3` IF a versioned chart already exists with different verified archive content or version metadata, THEN the system SHALL reject an overwrite.
   - Acceptance check: the existing OCI chart remains unchanged and the release fails with the conflict identified.
4. `R7.AC4` WHEN a same-tag rerun finds an existing identical image or chart, the system SHALL reuse its existing digest without republishing it.
   - Acceptance check: the rerun records the same digest and performs no registry write for that artifact.
5. `R7.AC5` IF the GitHub Release already exists for a different source commit or artifact digest, THEN the system SHALL reject automatic modification.
   - Acceptance check: the existing Release remains unchanged and the mismatch is reported.
6. `R7.AC6` WHEN a same-tag rerun finds a complete matching GitHub Release, the system SHALL finish without creating a duplicate Release.
   - Acceptance check: the rerun reports a verified no-op and the release count remains one.
7. `R7.AC7` IF one registry artifact is published but another publication step fails, THEN the system SHALL leave the GitHub Release unpublished and report the partial artifact identities.
   - Acceptance check: the failed run reports existing and missing digests with a failed status and no public Release page.
8. `R7.AC8` WHEN a maintainer explicitly reruns a failed release for the same immutable tag, the system SHALL publish only missing artifacts after revalidating all existing identities and required gates.
   - Acceptance check: a partial same-tag rerun completes without replacing an existing digest.
9. `R7.AC9` IF the tag points to a different commit than a previously published artifact or Release claims, THEN the system SHALL stop without altering any published artifact.
   - Acceptance check: a moved-tag fixture reports a revision conflict before any write.
10. `R7.AC10` IF a published GitHub Release is found with a missing or altered registry artifact, THEN the system SHALL report the release as inconsistent rather than accepting it as complete.
   - Acceptance check: audit fails and identifies the absent or conflicting artifact and the recorded expected digest.
11. `R7.AC11` The system SHALL serialize publication attempts for one release version.
   - Acceptance check: two concurrent attempts cannot both publish or replace the same version.

### R8 Postpublication Verification

**User Story:** As a consumer, I want released artifacts checked from their
public endpoints, so that a successful workflow means the install path works.

#### Acceptance Criteria

1. `R8.AC1` WHEN image publication completes, the system SHALL verify the public image's immutable digest, version, revision, source, platform, and executable version.
   - Acceptance check: independently pulled bytes and metadata match the staged release identity.
2. `R8.AC2` WHEN chart publication completes, the system SHALL pull the published OCI chart with standard Helm OCI functionality.
   - Acceptance check: a clean Helm pull for the version succeeds without repository files.
3. `R8.AC3` WHEN the published chart is pulled, the system SHALL render it using standard Helm functionality.
   - Acceptance check: Helm emits the expected installable Kubernetes objects from the public chart.
4. `R8.AC4` WHEN the published chart is rendered with default image values, the system SHALL verify its manager image reference matches the released controller image.
   - Acceptance check: the rendered image repository and version equal the published image reference.
5. `R8.AC5` WHEN publication is checked, the system SHALL verify chart and image version metadata against the canonical tag version.
   - Acceptance check: image labels, executable output, chart metadata, and Release record agree.
6. `R8.AC6` WHEN publication is checked, the system SHALL verify that the release did not create or change a `latest` image tag.
   - Acceptance check: the `latest` reference is absent or has exactly its prepublication digest.
7. `R8.AC7` WHEN the GitHub Release is published, the system SHALL verify that it references the expected tag, full source commit, and artifact digests.
   - Acceptance check: a fresh GitHub read returns the expected release identity.
8. `R8.AC8` IF any public endpoint fails verification, THEN the system SHALL report an incomplete release with the failing artifact and expected identity.
   - Acceptance check: the run cannot report success while an image, chart, or Release check fails.

### R9 Publication Security And Permissions

**User Story:** As a repository maintainer, I want release credentials and
workflow authority tightly scoped, so that untrusted development code cannot
publish executable artifacts.

#### Acceptance Criteria

1. `R9.AC1` The system SHALL grant ordinary CI only the GitHub permissions needed to read source and run validation.
   - Acceptance check: CI has no package-write or release-write capability.
2. `R9.AC2` The system SHALL grant publication permissions only to the official release job that needs them.
   - Acceptance check: the privileged job has no unrelated write scopes.
3. `R9.AC3` The system SHALL use repository-provided workflow identity for GitHub and GHCR publication in the default repository-linked path.
   - Acceptance check: no long-lived publication credential is needed for the default GitHub-based path.
4. `R9.AC4` The system SHALL keep publication tokens, passwords, private keys, and registry credentials out of committed source and release artifacts.
   - Acceptance check: tracked files and generated artifacts contain no publication secret.
5. `R9.AC5` The system SHALL restrict official release execution to repository-controlled workflow definitions and authorized release tags.
   - Acceptance check: an untrusted PR commit or unprotected branch ref cannot enter a privileged publication job.
6. `R9.AC6` IF the release tag is malformed or its commit is not reachable from protected `main`, THEN the system SHALL stop before publication.
   - Acceptance check: invalid tags and commits reachable only from `develop` fail without public writes, while a tag on promoted `main` passes lineage validation.
7. `R9.AC7` The system SHALL make public image and chart pull access a prerequisite for declaring release success.
   - Acceptance check: anonymous registry pulls succeed, including when a newly created GHCR package initially defaults to private.


## Non-Functional Requirements

- `NFR1` **Release integrity:** One tag, one source commit, and one canonical version SHALL bind every artifact; `R2`, `R4.AC6`, `R5.AC6`, and `R6.AC1` through `R6.AC4` provide observable coverage.
- `NFR2` **Fail-closed safety:** Malformed input, failed certification, conflicting content, and failed public checks SHALL never be reported as a complete release; `R1.AC7`, `R2.AC9`, `R3.AC6`, `R7`, and `R8.AC8` provide observable coverage.
- `NFR3` **Reproducibility:** Equivalent tagged source and centrally declared build inputs SHALL produce inspectable, repeatable release identities; `R2`, `R3.AC7`, `R4.AC2`, `R5.AC2`, `R7.AC4`, and `R8` provide observable coverage.
- `NFR4` **Least privilege:** CI and publication SHALL use the smallest GitHub permission set that supports their respective duties; `R1` and `R9` provide observable coverage.
- `NFR5` **Operability:** Users SHALL be able to install a versioned release from public artifacts and operators SHALL be able to diagnose partial publication; `R5`, `R6`, `R7.AC7` through `R7.AC10`, and `R8` provide observable coverage.
- `NFR6` **Compatibility honesty:** Published platform and Kubernetes/Helm claims SHALL follow approved, executed certification rather than assumptions; `R3.AC3` through `R3.AC5`, `R4.AC4`, `R4.AC5`, `R6.AC3`, and `R8.AC1` provide observable coverage.

## Constraints And Dependencies

- `C1` `packaging-and-installation` is the approved prerequisite and remains authoritative for image structure, Dockerfile, Helm chart, values, CRDs, RBAC, certificates, install, upgrade, rollback, uninstall, purge, and package verification.
- `C2` `end-to-end-scenarios` is a prerequisite for official release certification; its manifest names the API compatibility, package compatibility, harness acceptance, and cross-module prerequisites that release gating must honor.
- `C3` The initial published platform is `linux/amd64` because the current production Dockerfile explicitly builds `GOARCH=amd64`. `linux/arm64` and a single multi-platform manifest require an approved production-image correction under a separate feature before release-distribution may claim them.
- `C4` GitHub hosts the source repository, GitHub Actions is the automation mechanism, GHCR hosts image and Helm OCI artifacts, and GitHub Releases hosts the release page. No additional persistent distribution infrastructure is required.
- `C5` Official tags use stable `vMAJOR.MINOR.PATCH` Semantic Versioning with no leading zeroes in numeric components and no prerelease or build suffix. Maintainers choose and create tags explicitly; the automation does not calculate or create them.
- `C6` The tagged commit must be reachable from the protected `main` branch and carry the reviewed release workflow definition. Maintainers promote reviewed changes from `develop` to `main` before tagging. Protected release-tag creation and update/deletion restrictions are repository prerequisites.
- `C7` The centrally declared Kubernetes matrix remains `1.35.6` and `1.36.2`, the chart's `kubeVersion` remains authoritative for its supported range, and the pinned Helm CLI in the packaging toolchain remains authoritative for the required Helm version.
- `C8` The canonical Helm OCI repository is `oci://ghcr.io/steeltanuki/charts/kubeseer`; the parent upload target is `oci://ghcr.io/steeltanuki/charts`. No second chart tree, template set, or packaging path is introduced.
- `C9` GHCR package visibility and workflow access must permit anonymous pulls for the release image and chart. Initial package visibility may require one-time GitHub package administration; no long-lived registry secret is introduced by this feature.
- `C10` Normal CI and local kind/Podman tests may produce temporary private/local images, but those are never official release artifacts and never use the public versioned repositories as a development channel.

## Out Of Scope

- Changing the production image layout, manager entrypoint, chart templates or values, Kubernetes manifests, CRD/RBAC/certificate behavior, or installation and lifecycle semantics owned by `packaging-and-installation`.
- `linux/arm64` certification and multi-platform image construction until a separate approved production-image correction removes the current amd64-only build contract.
- Prerelease and build-metadata versions, automatic version calculation, automatic tag creation, nightly/development/PR images, branch image tags, and floating `latest` or `stable` tags.
- Image or chart signing, key management, SLSA attestations, SBOM publication, vulnerability-scanning policy, Artifact Hub registration, Docker Hub mirroring, or additional registries.
- A separately attached GitHub Release Helm archive, autogenerated changelog, deployment-environment promotion, and a second chart packaging implementation.
