---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-24T16:55:25Z
last_modified: 2026-09-24T16:55:25Z
approved_fingerprint: sha256:7ad6e28084b632df0352236871bea6dbd26d6d5f75334700c21f6001ff80a37f
source_requirements_approved_at: 2026-09-24T16:52:24Z
source_requirements_fingerprint: sha256:2c2ce54e55bb8387bf09071baf112aae52fb8699841e5884715a0c1b5e93b191
---

# Feature Design

## Architecture

Two GitHub Actions workflows orchestrate one existing product package and one
release-specific script. CI uses `pull_request` and `push.branches` events;
the release workflow uses tag pushes only:

```text
pull_request / branch push -> ci.yml (read-only token)
                         -> make verify, make test, release-policy fixtures
                         -> no public write

maintainer promotes reviewed changes from develop to protected main
  -> main push runs ci.yml; no public write

maintainer pushes protected vX.Y.Z tag
  -> release.yml / gates (contents: read)
       -> peel tag to full commit; check protected main ancestry
       -> validate source versions and maintainer release notes
       -> make verify; make test; make test-compatibility
       -> make test-package-compatibility
       -> ./hack/e2e-harness-acceptance.sh; make e2e
  -> release.yml / publish (contents: write, packages: write)
       -> checkout and confirm the same full commit
       -> build and inspect candidate image; package and inspect canonical chart
       -> inventory GHCR and GitHub Release; reject conflicts
       -> push missing image; verify public image
       -> push missing chart; verify public pull/render
       -> publish GitHub Release; verify Release and immutable digests
```

The release workflow uses `on.push.tags: ['v*']` as a coarse filter, followed by
an exact stable-version parser accepting only
`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`. It has no branch,
`release`, `workflow_run`, or PR publication trigger. `concurrency` uses the tag
name with `cancel-in-progress: false`. A maintainer creates the tag; neither
workflow calculates or pushes a tag. The tagged commit must be an ancestor of
the protected `main` branch. `develop` remains the integration branch;
reviewed changes are promoted to `main` before a release tag is created. A
branch push, including a promotion to `main`, runs ordinary CI and never
publishes. A `v*` tag ruleset restricts creation to
maintainers and blocks update/deletion; the workflow checks ancestry and
resolves annotated or lightweight tags to one full commit. The workflow file
and release script are read from that trusted tagged commit. No privileged job
checks out forked PR code. GitHub [tag push triggers](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows),
[tag rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/creating-rulesets-for-a-repository),
and [Actions security guidance](https://docs.github.com/en/actions/reference/security/secure-use)
inform these boundaries.

`main` remains the repository's default, latest promoted release-source branch.
Repository administrators protect it with a branch ruleset requiring the
ordinary `validate` CI check and a pull request for promotion, with zero
mandatory second-person approvals while there is one maintainer. The ruleset
blocks force pushes and deletion. `develop` retains its integration-branch
protection. The tag rulesets restrict version-tag creation to the maintainer
and block updates and deletions. The creation rule grants the repository admin
bypass, while a separate update/deletion rule grants no bypass, so the
maintainer cannot move or remove a published version tag. These GitHub
settings are prerequisites, not
changes performed by a release run. The source validator checks the peeled
tag commit against `refs/remotes/origin/main` fetched with full history;
commits reachable only from `develop` fail before any public write. An older
tag remains valid after `main` advances, so an exact same-tag recovery can
still run. No `stable`, `latest`, or branch-named image/chart tag is published.

`hack/release-distribution.sh` is the sole release-specific implementation
entrypoint. It provides read-only `validate`, `stage`, `inventory`, and `audit`
operations plus explicit `publish-image`, `publish-chart`, and
`publish-release` operations. Each command accepts an explicit tag and source
commit; no command derives publication authority from the current branch or
ambient checkout. Mutating modes require the verified tag workflow context
and its gate-job source SHA; local invocations remain read-only. `stage` writes
candidate files only to a run-owned temporary directory and never edits
committed chart/version files. A small
`make test-release-distribution` fixture harness exercises the script's
decision table with local fake registry/GitHub endpoints and proves the
no-public-write paths. The script remains callable locally with suitable
GitHub identity; the workflow is an orchestrator, not a second package
implementation.

### Release identity and staged candidates

The tag is the only version source. For `v0.1.0`, version `0.1.0` must match
`charts/kubeseer/Chart.yaml` `version` and `appVersion`, candidate image tag,
`kubeseer version` output, OCI image version label, and the staged
`kubeseer-0.1.0.tgz` name/content. The script rejects any mismatch before the
first public write; it never rewrites `Chart.yaml`. `COMMIT` is the peeled full
tag commit and `BUILD_DATE` is derived from that commit's timestamp so the
existing Dockerfile embeds the same version/revision/date in binary and OCI
labels. It checks `org.opencontainers.image.title`, `version`, `source`, and
`revision`, the non-root UID, direct entrypoint, and runtime content through
the approved package verification contract and candidate inspection.

`docker buildx` (or an equivalent runner-provided OCI builder) uses the
production `Dockerfile` for `linux/amd64`, then stages a local OCI image and
verifies its platform and executable output. The Dockerfile currently sets
`GOARCH=amd64`; the initial release deliberately advertises only
`linux/amd64`. Adding `linux/arm64` or a multi-platform manifest requires a
separate approved `multi-platform-controller-image` corrective feature (or an
equivalent approved packaging correction) to change and certify the production
image contract first. A later release-distribution review can then enable a
single versioned manifest list. The release workflow cannot claim arm64 by
passing `--platform linux/arm64` to the current Dockerfile.

The chart candidate comes only from `helm package charts/kubeseer --destination
<run-temp>` after `make verify` has run the existing package checks. It is
unpacked and inspected for `name`, `version`, `appVersion`, default image
repository/tag resolution, CRDs, and a content hash. The archive exists for
`helm push` and is not attached to the initial GitHub Release. The canonical
user reference is `oci://ghcr.io/steeltanuki/charts/kubeseer --version 0.1.0`;
the upload destination is its parent, `oci://ghcr.io/steeltanuki/charts`,
because Helm derives the final basename and version from `Chart.yaml`.
This follows [Helm's OCI push and pull contract](https://helm.sh/docs/v3/topics/registries/).

The chart's source-revision binding is the protected `v<version>` tag plus
the verified chart OCI digest recorded in the GitHub Release. The image also
contains the full revision as an OCI label. The Release body records the exact
tag commit and both digests, allowing a consumer to trace both OCI artifacts
to one source revision without injecting a commit-dependent field into the
source-controlled chart. A chart digest and tag lookup must agree with the
release record during every audit.

### Mandatory gates and environment

The release gate set is selected from the approved package and E2E contracts:

| Gate | Why it is mandatory |
| --- | --- |
| `make verify` | Generated artifacts, chart/package checks, static policies, and repository drift; already includes `verify-package`. |
| `make test` | Current default higher-layer module integration; currently delegates to `test-integration`. |
| `make test-compatibility` | Full centrally declared Kubernetes API matrix, named by the E2E certification manifest. |
| `make test-package-compatibility` | Installation/removal on every supported Kubernetes and certificate profile, named by the E2E certification manifest. |
| `./hack/e2e-harness-acceptance.sh` | Proves ownership, no ambient cluster, zero-match failure, and cleanup; named by the E2E certification manifest. |
| `make e2e` | Complete real-cluster release-scope product scenarios on the approved kind/Podman path. |

The validation job pins or centrally declares the Go, Helm, kind, Podman,
kubectl, Kubernetes asset, and cert-manager inputs already required by
`hack/toolchain.mk`. It preflights Podman availability on a GitHub-hosted Linux
runner before running cluster gates. A missing runtime or dependency is a
failed gate, not a skip. `make build`, `make verify-package`, and
`make test-integration` are not repeated as separate release commands because
the selected gates and staged official build already exercise their approved
contracts. No `make lint` gate is claimed until that repository entrypoint
exists and is approved. The release script records command, source SHA,
toolchain identity, and non-vacuous success markers for audit without changing
the Walden evidence ledger.

### Registry, Release, and user interfaces

The image repository is `ghcr.io/steeltanuki/kubeseer` and the chart
repository is `ghcr.io/steeltanuki/charts/kubeseer`. GHCR packages must allow
anonymous pulls. A first package is [private by default](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility),
so package visibility and Actions access are a one-time GitHub administration
prerequisite. If a first push creates a private package, the workflow stops
before publishing the GitHub Release, reports the partial state, and a
maintainer makes the package public before rerunning. `GITHUB_TOKEN` is used
for publication; no PAT or registry password is committed or required for the
default repository-linked package path.

`docs/releases/v<version>.md` is the maintainer-authored, committed release
note input. The preflight requires the file to contain substantive notes and
upgrade considerations or a link to version-specific upgrade guidance. The
release body composes that file with the tag, full source SHA, version,
Kubernetes chart range and certified patch versions, Helm minimum from the
central toolchain, certified image platform, image/chart references and
digests, and a version-pinned install command. It references the existing
installation/CRD-upgrade documentation. The Release is published only after
the image and chart public checks pass. User documentation makes the public
OCI install command the first released-version path, with separate source
build and existing local kind/Podman paths. The chart's install/upgrade/
rollback/purge semantics remain owned by `packaging-and-installation`.

The published command is:

```bash
helm upgrade --install kubeseer oci://ghcr.io/steeltanuki/charts/kubeseer \
  --version 0.1.0 --namespace kubeseer-system --create-namespace
```

The normal package certificate prerequisite and any chart values required
for that environment remain documented alongside this command. A clean
client must be able to pull the chart and controller image anonymously.
Maintainer documentation describes the `develop` to `main` promotion, the
ordinary CI result on the promoted commit, and the subsequent version-tag
push from `main`. It states that neither branch push publishes artifacts.

### Publication transaction and retry rules

All mandatory gates, source/version checks, candidate builds, candidate
inspection, release-note checks, and remote inventory finish before the first
write. Registry `404` means absent only after a successful authenticated
repository query; permission or transport errors fail closed. Before each
write, inventory is repeated so an external conflicting writer is detected.
The script never issues a replacement push for an existing version:

1. An existing image with the same manifest digest and full source revision is
   reused. A different digest or revision is a hard conflict.
2. An existing chart is pulled. If its archive bytes, chart metadata, and
   content hash match the staged archive, its OCI digest is reused. Otherwise
   the version is a hard conflict. Incidental OCI manifest timestamps do not
   justify rewriting an existing chart version.
3. An existing published GitHub Release is accepted only if its tag resolves
   to the same SHA and its complete body records the expected references and
   digests. A complete match is an audited no-op. Different metadata fails;
   no automatic edit or asset replacement occurs.
4. Missing image and chart artifacts are published in that order. Each is
   immediately pulled/read back and checked before the next write. A public
   GitHub Release is the final write, followed by a fresh Release read. Release
   creation uses `gh release create --verify-tag` (or an existing-tag-only API
   equivalent), because the CLI otherwise may create a missing tag; see the
   [GitHub CLI release contract](https://cli.github.com/manual/gh_release_create).

A failed image push leaves no chart or GitHub Release. A failed chart push
leaves a versioned image and no public GitHub Release; the run reports the
image digest and missing chart. A failed GitHub Release creation leaves two
verified registry artifacts and no public Release. A maintainer's explicit
same-tag workflow rerun repeats gates and candidate/remote validation, reuses
matching digests, and fills only missing artifacts. If a published Release
later has a missing or altered registry artifact, audit reports an incident;
a maintainer reviews the expected digest and explicitly reruns the tagged
workflow for missing-only recovery. A different digest is never accepted.
No automatic deletion or tag movement is used as rollback.

GitHub's [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)
can protect the published Release and tag, but GHCR tag replacement prevention
still relies on restricted package write access, serialized workflow runs,
pre-write comparison, and post-write digest checks. An out-of-band package
administrator can still replace a GHCR tag; the audit detects that drift, and
the version is treated as compromised rather than silently repaired.

### Permissions and public verification

`ci.yml` and the release `gates` job explicitly grant only `contents: read`.
The release `publish` job grants `contents: write` for GitHub Release creation
and `packages: write` for GHCR, with unspecified scopes denied. The source
checkout action and any other external action are pinned to full commit SHAs;
first-party tooling and shell commands are preferred. No
`pull_request_target`, `workflow_run`, untrusted downloaded artifact, or
long-lived publication secret crosses the privileged job. GitHub documents
[job-scoped token permissions](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
and [GITHUB_TOKEN for linked GHCR packages](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).

After image/chart pushes, a clean unauthenticated client resolves the image
tag and digest, pulls the Helm OCI chart, inspects `Chart.yaml`, runs
`helm template --include-crds` from the OCI reference at a supported
Kubernetes version, checks its default manager image, and
compares version and source identities. The script checks the `latest` tag's
absence or unchanged prepublication digest. After Release publication, a
fresh GitHub read verifies the tag commit, body identities, and both digests.
Any failure produces an incomplete audit, never a success marker.

## Options Considered

| Option | Decision |
| --- | --- |
| Protected versioned tag push, GHCR image/chart, GitHub Release | Selected: explicit maintainer intent, one GitHub identity, and direct Helm OCI install. |
| Protected `main` as the tag-source branch | Selected: a reviewed promotion identifies the latest release source before the maintainer creates a version tag. |
| Keep `develop` as the required tag lineage and merge `main` back before each tag | Rejected: it makes the integration branch the release authority and adds a reverse promotion solely to satisfy ancestry. |
| Branch pushes or PR merges publish `develop`/`main`/snapshot images | Rejected: ordinary branch updates would create public executable artifacts and mutable channels. |
| GitHub Release event triggers artifact builds | Rejected: it can expose a release page before mandatory gates and registry publication complete. |
| A separate chart repository, ChartMuseum, or second chart tree | Rejected: duplicate package ownership and external infrastructure without a release need. |
| Attach an additional `.tgz` to GitHub Releases | Deferred: Helm OCI is sufficient; an extra asset adds another consistency and recovery state. |
| Multi-platform build from the current Dockerfile | Rejected for the initial release: the production build hardcodes `GOARCH=amd64`; arm64 needs a separately approved production-image correction. |

## Simplicity And Elegance Review

There are exactly two workflows and one release script. Existing Make targets
remain the authority for package and product verification. The release script
adds only identity, remote inventory, publication, and audit behavior. It does
not render alternative Kubernetes manifests, change chart metadata in place,
or maintain a second Helm package. The Release `.tgz` attachment is omitted.
The smallest trusted release unit is one protected tag commit; separate
version-generation or promotion services would add state without satisfying
an initial requirement. Promotion is an ordinary reviewed pull request from
`develop` to `main`; the release script changes one ancestry reference and
keeps the existing tagged-source and artifact checks.

## Failure Modes And Tradeoffs

| Failure | Containment and recovery |
| --- | --- |
| Malformed tag, version mismatch, untrusted lineage, missing notes | Stop before gates or public writes, with exact failing identity. |
| Tag commit exists only on `develop`, or `origin/main` is unavailable | Reject the tag before public writes; promote the reviewed source to protected `main` and create a new version tag there. |
| `main` advances after an earlier release tag | Keep the earlier tag eligible by ancestry so an exact same-tag audit or missing-only rerun remains possible. |
| Required `main` or `v*` ruleset is not active | Treat repository protection as an unmet administration prerequisite; do not create the release tag. |
| Verification gate fails or skips | Stop before candidate publication; no partial public state. |
| Runner cannot execute Podman/kind gate | Stop as an environment failure; use a compatible GitHub-hosted runner setup rather than weakening certification. |
| Existing registry version conflicts | Preserve remote bytes; report digest and revision conflict for maintainer investigation. |
| Image succeeds, chart fails | Record image digest and absent chart; same-tag rerun reuses image and publishes only matching missing chart. |
| Chart succeeds, GitHub Release fails | Record both registry digests; same-tag rerun verifies and creates the missing Release. |
| First GHCR packages remain private | Withhold GitHub Release; maintainer makes both packages public and reruns public verification. |
| Published Release exists but an artifact is absent or changed | Audit fails; explicit maintainer incident review and same-tag missing-only recovery, never silent replacement. |
| External actor races a GHCR tag | Workflow serialization does not control outside writers; restricted package access plus read-after-write detects conflict, but registry-level immutability is not claimed. |
| `latest` tag predates the release | Preserve its prior digest and prove this workflow did not create or change it; do not use it in released documentation. |

## Verification Plan

- `make test-release-distribution` uses fixture registries/GitHub responses and
  event contexts to assert PR/`develop`/`main` no-public-write behavior, strict tag
  parsing, protected `main` ancestry, rejection of a commit reachable only
  from `develop`, same-tag validity after `main` advances, version mismatch
  rejection before writes,
  candidate metadata, remote `404`/auth distinctions, conflict/no-op/partial
  reruns, existing Release handling, and `latest` preservation. Every case
  asserts the exact write log and prints a stable, non-vacuous result marker.
- `make verify-release-source TAG=vX.Y.Z` is a read-only tagged-checkout
  command that validates tag/source/chart/note identity and staged candidate
  image/chart without GHCR or Release writes. It checks no tracked file was
  changed by staging and prints the resolved full commit and candidate hashes.
- The official `release.yml` gates job records successful output of the six
  selected repository commands for the exact tag commit. A seeded failing or
  zero-match gate must prevent the publish job from starting.
- A disposable fixture registry exercises authenticated inventory and OCI
  push/pull behavior where feasible; official acceptance additionally uses a
  versioned release tag and GHCR/GitHub read-after-write audit. The latter
  cannot be claimed complete before an authorized first publication.
- Public audit reads the image by digest and tag, invokes its `version`
  subcommand, pulls and renders the Helm chart from GHCR, compares chart/image
  version and source bindings, checks anonymous access and `latest` state,
  then checks the published GitHub Release body and tag commit. It emits one
  success marker only after all reads pass. A published-artifact audit is
  read-only and can be rerun independently of publication.
- Documentation checks assert the OCI installation command, public reference,
  Kubernetes/Helm prerequisites, distinct source/local-development paths,
  and the `develop` to protected `main` promotion before version-tag creation.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | CI event/permission isolation for `develop` and `main`; exact tag trigger; no-public-write and no-floating-tag fixtures |
| `R2` | Tag parser, peeled commit, source preflight, candidate identity and archive inspection |
| `R3` | Read-only gates job and approved command matrix with non-vacuous markers |
| `R4` | Existing Dockerfile build, amd64/platform inspection, GHCR image digest and pull audit |
| `R5` | Canonical chart package, GHCR Helm OCI push/pull/render, default image inspection |
| `R6` | Tagged release notes, composed Release body, published OCI install and branch-promotion documentation |
| `R7` | Remote inventory, ordered writes, digest comparison, concurrency, partial-run audit and rerun cases |
| `R8` | Anonymous digest-based image/chart reads, Helm render, latest baseline, fresh Release read |
| `R9` | Per-job permissions, protected `main` ancestry and tag ruleset, pinned actions, GITHUB_TOKEN, public visibility gate |
| `NFR1` | Full commit, version, and digest binding in staged and public audits |
| `NFR2` | Prepublication aborts, conflict preservation, no false success marker |
| `NFR3` | Tagged inputs, fixed build date, pinned/declared tools, existing package checks |
| `NFR4` | CI read-only token and release job-specific write scopes |
| `NFR5` | Public OCI install path and explicit partial publication report |
| `NFR6` | Exact certified Kubernetes matrix and amd64-only image inspection |
| `C1` | Existing production Dockerfile/chart and `make verify` remain authoritative |
| `C2` | E2E manifest determines prerequisite release gates |
| `C3` | Initial amd64 release; separate corrective prerequisite for arm64 |
| `C4` | GitHub Actions, GHCR, and GitHub Releases only |
| `C5` | Strict stable SemVer tag parser and maintainer-created tag |
| `C6` | Protected `main` ancestry after reviewed promotion and `v*` tag ruleset |
| `C7` | Chart `kubeVersion`, central toolchain, and declared compatibility matrix |
| `C8` | One chart directory, parent Helm OCI push target, no duplicate package |
| `C9` | GHCR public visibility and anonymous pull audit |
| `C10` | Local-only CI test artifacts and no development publication |

<!-- Only these six headings are required by the kernel. Add optional detail
     when it records a relevant decision, contract, or verification need.
     Coverage and verification need substantive content, not non-applicability. -->
