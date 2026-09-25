# Release pipeline audit and automation proposal

Audit date: 2026-09-25. Source inspected: `cef971dc999266ec25f9dd5677bfc924b1d946d1`
(`main`, tagged `v0.1.5`). The corrections described below are local changes on
`fix/release-pipeline-preflight`; the proposed release automation is not implemented.

## Findings and corrections

| Finding | Evidence | Resolution |
| --- | --- | --- |
| Version mistakes survive ordinary CI. | The `v0.1.2` and `v0.1.5` runs failed on chart version mismatches. `v0.1.5` contains chart/app version `0.1.4` and has no corresponding release notes. | Tag/source/notes validation now runs immediately after checkout, before Go setup, module downloads, or apt. Preparation automation below addresses mistakes before creating a tag. |
| Publisher CLI compatibility was discovered too late. | `v0.1.4` ran product gates for about 13 minutes, then failed on `podman push --retry` with runner Podman 4.9.3. Local Podman is 5.8.4. | The unsupported flag was already removed in PR #11. New `check-tools` parses the actual shared push options and other publication command options before expensive work, in CI and both release jobs. |
| CI omitted the actual publication path. | `ci.yml` ran only the `policy` scenario. Candidate building, actual Podman/Helm pushes, and recovery were absent from CI. | CI now runs all five release scenarios against disposable local endpoints, with read-only GitHub permissions. Required fixture images are pulled if missing on a fresh runner. |
| Rebuilding the same chart changed its digest. | Two Helm 3.12.1 packages of unchanged source had different member timestamps and SHA-256 hashes. Helm 3.22.0 also derives the OCI creation annotation from the archive's filesystem mtime, causing a second recovery failure even with identical archive bytes. | Normalize tar/gzip metadata and archive mtime after Helm packaging, preserving canonical chart contents. The transaction test rebuilds candidates, compares both image and chart digests, and exercises both no-op retries and missing-artifact recovery. |
| Tests depended on a specific release version. | Fixture rewriting only recognized `0.1.4`; documentation assertions hardcoded that version. A later bump could break the tests before testing the intended behavior. | Synthetic fixtures always set their own `0.1.3` identity; documentation checks derive the example version from the canonical chart. |
| Newer Helm rejected the fixture registry's HTTP transport. | Repeating the transaction with runner Helm 3.22.0 failed on an HTTPS-to-HTTP downgrade; local Helm 3.12.1 had accepted localhost fallback. | Select `--plain-http` for local fixture endpoints when the installed Helm command supports it. Official GHCR operations retain HTTPS. Help probes are excluded from fixture write/failure injection. |
| Tag immutability is not active. | Ruleset `release-tag-immutable` (23950282) is disabled. `release-tag-create` is active but has an administrator bypass. | Repository setting still needs correction: activate the separate update/deletion ruleset with no bypass. A true `GITHUB_REF_PROTECTED` does not establish that all required protections are enabled. |
| Public GHCR readiness is unresolved. | Anonymous token requests for both package paths returned HTTP 403. The local GitHub credential cannot list packages: missing `read:packages`. The repository has no GitHub Releases. | Do not classify the packages as absent from this evidence. Confirm package existence, visibility, repository linkage, and Actions access with a credential permitted to inspect packages. |

Historical runs:

- [v0.1.0](https://github.com/steeltanuki/kubeseer/actions/runs/36041503979): failed release; the reported log identifies E2E HTTP readiness.
- [v0.1.1](https://github.com/steeltanuki/kubeseer/actions/runs/36053262816): publisher could not resolve the tag in its checkout; full-history checkout is now present.
- [v0.1.2](https://github.com/steeltanuki/kubeseer/actions/runs/36055651740): chart/version mismatch.
- [v0.1.3](https://github.com/steeltanuki/kubeseer/actions/runs/36060050188): the reported log identifies anonymous GHCR inventory failure; authenticated inventory is now present.
- [v0.1.4](https://github.com/steeltanuki/kubeseer/actions/runs/36067410561): all product gates passed; publication failed on unsupported `--retry`.
- [v0.1.5](https://github.com/steeltanuki/kubeseer/actions/runs/36075243389): chart/version mismatch; publication was skipped.

## Verification and its limits

The existing five release scenarios passed before the repairs. This establishes
that the previous green tests did not cover candidate rebuilding. The repaired
transaction suite passed 20 cases with real Podman and Helm against a disposable
OCI registry and a fixture GitHub API. It covers image/chart publication, rebuilt
candidate identity, repeated execution, partial failures, conflicting artifacts,
and audit. It never publishes official artifacts.

The Ubuntu 24.04 Podman package was also checked in a disposable container:
Podman **4.9.3** accepts the current build/push options and rejects the previous
`--retry` option. This is a CLI compatibility check, not an entire hosted-runner
emulation. The local transaction uses Podman **5.8.4** and Helm **3.12.1**.
`actionlint` **1.7.7** passed for both workflows with its optional ShellCheck
integration disabled; the repository's workflow policy assertions also passed.
`make verify` and the default `make test` integration suite passed locally.
The aggregate release suite runs source-policy, workflow and documentation
checks before its image-build and transaction scenarios.

The complete 20-case transaction suite also passed with **Helm 3.22.0**, the
exact version recorded by the hosted runner, downloaded with its official
archive checksum verified. This additional run exposed and then verified fixes
for explicit HTTP transport and OCI creation timestamps. Helm's [OCI
pusher implementation](https://github.com/helm/helm/blob/v3.22.0/pkg/pusher/ocipusher.go)
binds that timestamp to the chart archive's modification time.

The last successful hosted product gate run is `v0.1.4`, including all 15 E2E
scenarios. Between that commit and the audited `main`, only the Podman option
and Walden release evidence changed. These historical results support the product
baseline; they do not certify a future tag. The release workflow continues to run
every mandatory product gate on the actual tagged commit.

The new workflow has not yet run on GitHub. Local checks cannot establish the
future runner's package permissions, GHCR visibility, or network availability.
No current result proves a complete public release. Review the refreshed Walden
evidence for this maintenance scope separately from that external readiness.

Chart normalization changes archive bytes from the previous publisher. Never
replace an already published version with these new bytes. Deliver this repair
under a new version; retain existing tags as historical records.

## Proposed maintainer workflow

Use a **Prepare release** GitHub Actions workflow with an explicit version input
and maintainer-authored highlights and upgrade considerations. Keep `develop` as
the integration branch and `main` as the reviewed release source. The intended
manual work becomes choosing the version/notes once and reviewing two PRs.

1. The maintainer starts **Prepare release**. Cheap checks reject malformed or
   already-used versions, missing notes, inappropriate source branches, and
   unavailable prerequisites before any build or branch write.
2. Automation creates or updates one `release/vX.Y.Z` branch from a recorded
   `develop` SHA. It updates chart `version` and `appVersion`, the versioned
   release notes, and maintained installation examples in one commit. It opens
   a PR to `develop` and reports the selected source/version. A repeated request
   resumes that preparation instead of creating duplicate branches or PRs.
3. After the maintainer merges the preparation PR and CI passes, automation opens
   the promotion PR `develop` to `main`. Its body identifies the version and
   source, release note changes, and preflight results. Use a merge commit for
   branch promotion to preserve ancestry; concurrent releases are serialized.
4. After the maintainer merges that specific promotion PR, the release request
   resolves the resulting `main` commit again and validates its complete content.
   If unrelated changes entered the promotion, or the requested version/source
   changed, it stops for renewed review. The authorized release request creates
   `vX.Y.Z` at that reviewed commit. Ordinary `main` pushes have no release intent.
5. The protected tag triggers the existing gated publisher. It publishes immutable
   versioned image/chart artifacts and creates the GitHub Release last. Failed
   attempts display whether to fix source with a new version or retry the same
   tag after an external failure; they never move tags or overwrite artifacts.

Do not create a GitHub Release as a way to start publication: the publisher owns
that final record, after verification of both public OCI artifacts.

This proposal changes the current approved contract: `release-distribution`
`R1.AC6` explicitly forbids automatic version tags. Before implementation, revise
that requirement to allow tag creation only for an explicit maintainer release
request bound to the reviewed promotion and resulting SHA, then review the
affected design/tasks. Preserve the existing prohibition on publication from
ordinary PR/branch events and on floating artifact tags.

## Fail-first ordering for preparation and release

| Order | Checks | Cost and failure behavior |
| --- | --- | --- |
| 1 | Version syntax; chart/app alignment; note title, content and placeholders; source SHA/ancestry; expected release intent | Local, normally seconds after checkout; no module download or image build. Report the expected and observed values. |
| 2 | Effective branch/tag rulesets; version already used; GHCR package visibility and workflow access; tool versions and exact supported options | Bounded read-only API/tool probes; stop before long gates. An authentication error is not evidence that an artifact is absent. Report the required GitHub setting directly. |
| 3 | Workflow syntax/policy, preparation fixtures, real local-registry publication and rebuilt-candidate recovery | CI on the preparation/promotion changes catches publisher failures before an official tag. Upload diagnostics even on failure. |
| 4 | Repository verification, module integration, API/package compatibility, E2E | Required checks on the release commit; publication remains blocked on any failure or skipped gate. Independent compatible checks can later run in parallel without sharing a cluster. |
| 5 | Build candidate once; inventory; publish missing artifacts; anonymous pull/render and digest verification; GitHub Release | Use immutable candidate identities and recorded toolchain/base-image inputs. Retry transient reads with bounded waits; fail immediately on invalid inputs, denied access or identity conflicts. |

The first local change implements early tag and command checks and adds missing
CI transaction coverage. The preparation workflow, external configuration
preflight, candidate handoff, diagnostic uploads and further parallelization
remain proposed work. Performance should be measured from checkout completion;
runner queue/startup time is outside the validator's control.

## GitHub setup and first-publication boundary

For unattended PR/tag creation, prefer a repository-scoped GitHub App with only
the required contents/PR permissions and an installation token. Give that App
creation permission in the version-tag creation ruleset, but no update/delete
bypass. Keep the publication job's existing `GITHUB_TOKEN` and package permissions.

A tag pushed using `GITHUB_TOKEN` does not automatically start a `push` workflow.
GitHub documents special handling for PRs opened with that token, including
approval-required workflow runs. An App token avoids relying on that recursive
trigger behavior. An alternative is an explicit `workflow_dispatch`/reusable
workflow design, which would require reviewing the publisher's existing
push-only authorization checks. See [GitHub workflow triggering](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow).

GHCR makes a new package private by default; making the source repository public
does not establish anonymous package access. This needs a one-time bootstrap
procedure for **both** `steeltanuki/kubeseer` and
`steeltanuki/charts/kubeseer`, as described in [GitHub's package visibility
documentation](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility).
The preparation preflight must distinguish ready public packages, inaccessible
existing packages, and first publication. For a missing package, report the
explicit bootstrap path before running expensive gates. After the first approved
publication creates a package, the maintainer may need to change its visibility
in GitHub and resume the same immutable release. The existing publisher verifies
the image before creating the chart, so these can require two visibility changes
and resumptions. Do not promise a completely unattended first GHCR publication.

Before the next tag: integrate and run the repaired CI, prepare a new coherent
version and notes, activate tag immutability, and resolve the GHCR prerequisite.
`v0.1.5` cannot be repaired by rerunning it because its committed chart version is
wrong. Changing chart or scripts requires a new commit and a new version tag.
