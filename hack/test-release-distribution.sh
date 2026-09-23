#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

readonly ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly SCRIPT="$ROOT_DIR/hack/release-distribution.sh"
readonly SCENARIO_NAME="${SCENARIO:-policy}"
readonly temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-release-distribution.XXXXXX")"
fixture_api_pid=""
fixture_aux_pid=""
fixture_registry_name=""
fixture_registry_data=""
cleanup() {
	if [[ -n "$fixture_api_pid" ]]; then kill "$fixture_api_pid" 2>/dev/null || true; fi
	if [[ -n "$fixture_aux_pid" ]]; then kill "$fixture_aux_pid" 2>/dev/null || true; fi
	if [[ -n "$fixture_registry_name" ]] && command -v podman >/dev/null 2>&1; then podman rm --force --ignore "$fixture_registry_name" >/dev/null 2>&1 || true; fi
	chmod -R u+w "$temp_dir" 2>/dev/null || true
	rm -rf "$temp_dir"
}
trap cleanup EXIT

fail() {
	printf 'RELEASE_DISTRIBUTION=%s STATUS=failed: %s\n' "$SCENARIO_NAME" "$1" >&2
	exit 1
}

pass_case() {
	passed_cases=$((passed_cases + 1))
	printf 'PASS release-distribution/%s\n' "$1"
}

passed_cases=0
expected_cases=0

new_fixture() {
	local name="$1"
	FIXTURE="$temp_dir/$name"
	FIXTURE_REPO="$FIXTURE/repo"
	FIXTURE_REMOTE="$FIXTURE/remote.git"
	mkdir -p "$FIXTURE"
	git init -q --initial-branch=develop "$FIXTURE_REPO"
	git -C "$FIXTURE_REPO" config user.name "Release fixture"
	git -C "$FIXTURE_REPO" config user.email "release-fixture@example.invalid"
	mkdir -p "$FIXTURE_REPO/.github/workflows" "$FIXTURE_REPO/charts/kubeseer" "$FIXTURE_REPO/docs/releases"
	cat >"$FIXTURE_REPO/.github/workflows/release.yml" <<'EOF'
name: Official release distribution
on:
  push:
    tags: ["v*"]
EOF
	printf '%s\n' 'apiVersion: v2' 'name: kubeseer' 'type: application' 'version: 0.1.0' 'appVersion: "0.1.0"' >"$FIXTURE_REPO/charts/kubeseer/Chart.yaml"
	cat >"$FIXTURE_REPO/docs/releases/v0.1.0.md" <<'EOF'
# Kubeseer v0.1.0

## Highlights
- The fixture release publishes a verified Kubeseer controller and canonical chart.

## Upgrade considerations
- Follow the versioned installation and upgrade guidance before changing releases.
EOF
	git -C "$FIXTURE_REPO" add .
	git -C "$FIXTURE_REPO" commit -qm "fixture: release baseline"
	FIXTURE_BASE_SHA="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"
	git init -q --bare "$FIXTURE_REMOTE"
	git -C "$FIXTURE_REPO" remote add origin "$FIXTURE_REMOTE"
	git -C "$FIXTURE_REPO" push -q -u origin develop
	git -C "$FIXTURE_REPO" fetch -q origin '+refs/heads/develop:refs/remotes/origin/develop'
	git -C "$FIXTURE_REPO" tag -a v0.1.0 -m "Kubeseer v0.1.0"
	FIXTURE_SHA="$(git -C "$FIXTURE_REPO" rev-parse 'refs/tags/v0.1.0^{commit}')"
}

expect_rejected() {
	local label="$1"
	local expected_text="$2"
	shift 2
	local output
	if output="$("$@" 2>&1)"; then
		fail "$label was unexpectedly accepted"
	fi
	[[ "$output" == *"$expected_text"* ]] || fail "$label failed for the wrong reason: $output"
	pass_case "$label"
}

validate_fixture() {
	local tag="$1"
	local sha="$2"
	(
		cd "$FIXTURE_REPO"
		"$SCRIPT" validate --tag "$tag" --source-sha "$sha"
	)
}

run_policy() {
	expected_cases=15
new_fixture valid-tag
valid_output="$(validate_fixture v0.1.0 "$FIXTURE_SHA")"
[[ "$valid_output" == *"RELEASE_DISTRIBUTION=source STATUS=passed VERSION=0.1.0 SOURCE_SHA=$FIXTURE_SHA"* ]] || fail "valid annotated tag did not return its canonical identity"
pass_case valid-annotated-tag-and-canonical-version

tags_before="$(git -C "$FIXTURE_REPO" for-each-ref --format='%(refname):%(objectname)' refs/tags)"
expect_rejected prerelease-tag "stable vMAJOR.MINOR.PATCH SemVer" validate_fixture v0.2.0-rc.1 "$FIXTURE_SHA"
tags_after="$(git -C "$FIXTURE_REPO" for-each-ref --format='%(refname):%(objectname)' refs/tags)"
[[ "$tags_before" == "$tags_after" ]] || fail "validation created or moved a Git tag"
pass_case validation-does-not-create-tags

expect_rejected mismatched-source-sha "does not match resolved tag commit" validate_fixture v0.1.0 "$(printf '%040d' 7)"

new_fixture moved-tag
git -C "$FIXTURE_REPO" checkout -q -b follow-up
printf 'fixture follow-up\n' >"$FIXTURE_REPO/follow-up.txt"
git -C "$FIXTURE_REPO" add follow-up.txt
git -C "$FIXTURE_REPO" commit -qm "fixture: move version tag"
moved_sha="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"
git -C "$FIXTURE_REPO" tag -fa v0.1.0 -m "moved fixture tag" HEAD >/dev/null
expect_rejected moved-tag-identity "does not match resolved tag commit" validate_fixture v0.1.0 "$FIXTURE_BASE_SHA"
git -C "$FIXTURE_REPO" checkout -q --detach "$moved_sha"
expect_rejected moved-tag-lineage "not reachable from origin/develop" validate_fixture v0.1.0 "$moved_sha"

new_fixture untrusted-lineage
git -C "$FIXTURE_REPO" checkout -q --orphan untrusted
git -C "$FIXTURE_REPO" rm -q -rf .
mkdir -p "$FIXTURE_REPO/.github/workflows" "$FIXTURE_REPO/charts/kubeseer" "$FIXTURE_REPO/docs/releases"
printf '%s\n' 'name: Official release distribution' >"$FIXTURE_REPO/.github/workflows/release.yml"
printf '%s\n' 'apiVersion: v2' 'name: kubeseer' 'type: application' 'version: 0.1.0' 'appVersion: "0.1.0"' >"$FIXTURE_REPO/charts/kubeseer/Chart.yaml"
cat >"$FIXTURE_REPO/docs/releases/v0.1.0.md" <<'EOF'
# Kubeseer v0.1.0
## Highlights
- A substantive release note fixture for the protected tag validator.
## Upgrade considerations
- Review the installation and upgrade path before using this fixture release.
EOF
git -C "$FIXTURE_REPO" add .
git -C "$FIXTURE_REPO" commit -qm "fixture: untrusted root"
untrusted_sha="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"
git -C "$FIXTURE_REPO" tag -a -f v0.1.0 -m "untrusted fixture tag" >/dev/null
expect_rejected untrusted-release-lineage "not reachable from origin/develop" validate_fixture v0.1.0 "$untrusted_sha"

new_fixture chart-version-mismatch
sed -i 's/^version: 0.1.0$/version: 0.1.1/' "$FIXTURE_REPO/charts/kubeseer/Chart.yaml"
git -C "$FIXTURE_REPO" add charts/kubeseer/Chart.yaml
git -C "$FIXTURE_REPO" commit -qm "fixture: mismatched chart version"
git -C "$FIXTURE_REPO" push -q origin develop
git -C "$FIXTURE_REPO" fetch -q origin '+refs/heads/develop:refs/remotes/origin/develop'
git -C "$FIXTURE_REPO" tag -fa v0.1.0 -m "mismatched chart version" >/dev/null
FIXTURE_SHA="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"
expect_rejected chart-version-mismatch "Chart.yaml version does not match" validate_fixture v0.1.0 "$FIXTURE_SHA"

new_fixture app-version-mismatch
sed -i 's/^appVersion: "0.1.0"$/appVersion: "0.1.1"/' "$FIXTURE_REPO/charts/kubeseer/Chart.yaml"
git -C "$FIXTURE_REPO" add charts/kubeseer/Chart.yaml
git -C "$FIXTURE_REPO" commit -qm "fixture: mismatched appVersion"
git -C "$FIXTURE_REPO" push -q origin develop
git -C "$FIXTURE_REPO" fetch -q origin '+refs/heads/develop:refs/remotes/origin/develop'
git -C "$FIXTURE_REPO" tag -fa v0.1.0 -m "mismatched appVersion" >/dev/null
FIXTURE_SHA="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"
expect_rejected app-version-mismatch "Chart.yaml appVersion does not match" validate_fixture v0.1.0 "$FIXTURE_SHA"

new_fixture missing-notes
rm "$FIXTURE_REPO/docs/releases/v0.1.0.md"
git -C "$FIXTURE_REPO" add -u
git -C "$FIXTURE_REPO" commit -qm "fixture: missing release notes"
git -C "$FIXTURE_REPO" push -q origin develop
git -C "$FIXTURE_REPO" fetch -q origin '+refs/heads/develop:refs/remotes/origin/develop'
git -C "$FIXTURE_REPO" tag -fa v0.1.0 -m "missing release notes" >/dev/null
FIXTURE_SHA="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"
expect_rejected missing-tagged-release-notes "missing maintainer release notes" validate_fixture v0.1.0 "$FIXTURE_SHA"

new_fixture placeholder-notes
sed -i 's/The fixture release publishes a verified Kubeseer controller and canonical chart./TODO: write release notes./' "$FIXTURE_REPO/docs/releases/v0.1.0.md"
git -C "$FIXTURE_REPO" add docs/releases/v0.1.0.md
git -C "$FIXTURE_REPO" commit -qm "fixture: placeholder release notes"
git -C "$FIXTURE_REPO" push -q origin develop
git -C "$FIXTURE_REPO" fetch -q origin '+refs/heads/develop:refs/remotes/origin/develop'
git -C "$FIXTURE_REPO" tag -fa v0.1.0 -m "placeholder release notes" >/dev/null
FIXTURE_SHA="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"
expect_rejected placeholder-release-notes "unfinished placeholder text" validate_fixture v0.1.0 "$FIXTURE_SHA"

new_fixture dirty-source
printf 'local source change\n' >"$FIXTURE_REPO/untracked.txt"
expect_rejected dirty-tagged-source "worktree is not clean" validate_fixture v0.1.0 "$FIXTURE_SHA"

readonly fake_bin="$temp_dir/fake-bin"
readonly public_write_log="$temp_dir/public-writes.log"
mkdir -p "$fake_bin"
for tool in podman helm curl gh; do
	cat >"$fake_bin/$tool" <<'EOF'
#!/usr/bin/env bash
printf '%s %s\n' "$(basename "$0")" "$*" >>"$RELEASE_PUBLIC_WRITE_LOG"
exit 99
EOF
	chmod +x "$fake_bin/$tool"
done

assert_publication_denied() {
	local name="$1" event="$2" ref="$3" sha="$4"
	local output
	rm -f "$public_write_log"
	if output="$(
		cd "$FIXTURE_REPO"
		PATH="$fake_bin:$PATH" RELEASE_PUBLIC_WRITE_LOG="$public_write_log" \
		GITHUB_ACTIONS=true GITHUB_EVENT_NAME="$event" GITHUB_REPOSITORY=steeltanuki/kubeseer \
		GITHUB_REF="$ref" GITHUB_REF_PROTECTED=true \
		GITHUB_WORKFLOW_REF="steeltanuki/kubeseer/.github/workflows/release.yml@$ref" \
		GITHUB_JOB=publish GITHUB_SHA="$sha" RELEASE_GATE_SOURCE_SHA="$sha" \
		GITHUB_RUN_ID=123 GITHUB_RUN_ATTEMPT=1 GITHUB_TOKEN=fixture-token \
		"$SCRIPT" publish-image --tag v0.1.0 --source-sha "$sha" 2>&1
	)"; then
		fail "$name unexpectedly entered a publication command"
	fi
	[[ "$output" == *"official GitHub Actions tag workflow"* || "$output" == *"protected tag push event"* || "$output" == *"publication ref does not match"* ]] || fail "$name was denied for an unexpected reason: $output"
	[[ ! -s "$public_write_log" ]] || fail "$name invoked a public registry or release client"
	pass_case "$name"
}

new_fixture pr-event
assert_publication_denied pull-request-no-public-write pull_request refs/pull/17/merge "$FIXTURE_SHA"
assert_publication_denied ordinary-branch-no-public-write push refs/heads/develop "$FIXTURE_SHA"

new_fixture local-event
output="$(
	cd "$FIXTURE_REPO"
	PATH="$fake_bin:$PATH" RELEASE_PUBLIC_WRITE_LOG="$public_write_log" \
	GITHUB_EVENT_NAME=push GITHUB_REPOSITORY=steeltanuki/kubeseer \
	GITHUB_REF=refs/tags/v0.1.0 GITHUB_JOB=publish \
	env -u GITHUB_ACTIONS "$SCRIPT" publish-image --tag v0.1.0 --source-sha "$FIXTURE_SHA" 2>&1
)" && fail "local command unexpectedly entered publication"
[[ "$output" == *"official GitHub Actions tag workflow"* ]] || fail "local command was denied for an unexpected reason: $output"
[[ ! -s "$public_write_log" ]] || fail "local command invoked a public registry or release client"
pass_case local-command-no-public-write

[[ "$passed_cases" == "$expected_cases" ]] || fail "only $passed_cases of $expected_cases policy cases ran"
printf 'RELEASE_DISTRIBUTION=policy STATUS=passed CASES=%s\n' "$passed_cases"
}

copy_repository_fixture() {
	local name="$1"
	FIXTURE="$temp_dir/$name"
	FIXTURE_REPO="$FIXTURE/repo"
	FIXTURE_REMOTE="$FIXTURE/remote.git"
	mkdir -p "$FIXTURE_REPO"
	while IFS= read -r -d '' path; do
		mkdir -p "$FIXTURE_REPO/$(dirname -- "$path")"
		cp -a "$ROOT_DIR/$path" "$FIXTURE_REPO/$path"
	done < <(git -C "$ROOT_DIR" ls-files --cached --others --exclude-standard -z)
	mkdir -p "$FIXTURE_REPO/.github/workflows" "$FIXTURE_REPO/docs/releases"
	if [[ ! -f "$FIXTURE_REPO/.github/workflows/release.yml" ]]; then
		printf '%s\n' 'name: Official release distribution' 'on:' '  push:' '    tags: ["v*"]' >"$FIXTURE_REPO/.github/workflows/release.yml"
	fi
	cat >"$FIXTURE_REPO/docs/releases/v0.1.0.md" <<'EOF'
# Kubeseer v0.1.0

## Highlights
- Candidate fixtures build the production controller and canonical chart from this tagged source.

## Upgrade considerations
- Follow the installation guide and review this release's compatibility before upgrading.
EOF
	git -C "$FIXTURE_REPO" init -q --initial-branch=develop
	git -C "$FIXTURE_REPO" config user.name "Release candidate fixture"
	git -C "$FIXTURE_REPO" config user.email "release-candidate@example.invalid"
	git -C "$FIXTURE_REPO" add -A
	git -C "$FIXTURE_REPO" commit -qm "fixture: tagged source snapshot"
	git init -q --bare "$FIXTURE_REMOTE"
	git -C "$FIXTURE_REPO" remote add origin "$FIXTURE_REMOTE"
	git -C "$FIXTURE_REPO" push -q -u origin develop
	git -C "$FIXTURE_REPO" fetch -q origin '+refs/heads/develop:refs/remotes/origin/develop'
	git -C "$FIXTURE_REPO" tag -a v0.1.0 -m "Kubeseer v0.1.0"
	FIXTURE_SHA="$(git -C "$FIXTURE_REPO" rev-parse 'refs/tags/v0.1.0^{commit}')"
}

commit_tagged_change() {
	local message="$1"
	shift
	git -C "$FIXTURE_REPO" add "$@"
	git -C "$FIXTURE_REPO" commit -qm "fixture: $message"
	git -C "$FIXTURE_REPO" push -q origin develop
	git -C "$FIXTURE_REPO" fetch -q origin '+refs/heads/develop:refs/remotes/origin/develop'
	git -C "$FIXTURE_REPO" tag -fa v0.1.0 -m "$message" >/dev/null
	FIXTURE_SHA="$(git -C "$FIXTURE_REPO" rev-parse HEAD)"
}

run_candidate() {
	expected_cases=8
	copy_repository_fixture valid-candidate
	local candidate_output="$temp_dir/candidate-output"
	mkdir -p "$candidate_output"
	local before_state after_state valid_output
	before_state="$(git -C "$FIXTURE_REPO" status --porcelain --untracked-files=all)"
	valid_output="$(cd "$FIXTURE_REPO" && make --no-print-directory verify-release-source TAG=v0.1.0 OUTPUT_DIR="$candidate_output" 2>&1)" || fail "production candidate staging failed: $valid_output"
	[[ "$valid_output" == *"RELEASE_DISTRIBUTION=candidate STATUS=passed VERSION=0.1.0 SOURCE_SHA=$FIXTURE_SHA PLATFORM=linux/amd64"* ]] || fail "staged candidate did not report the tagged amd64 release identity"
	[[ -f "$candidate_output/candidate.json" && -f "$candidate_output/kubeseer-controller.oci.tar" && -f "$candidate_output/kubeseer-0.1.0.tgz" && -s "$candidate_output/rendered-chart.yaml" ]] || fail "staging did not preserve the inspected image/chart candidate evidence"
python3 - "$candidate_output/candidate.json" "$FIXTURE_SHA" <<'PY'
import hashlib
import json
import pathlib
import sys

candidate = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
archive = pathlib.Path(sys.argv[1]).parent / "kubeseer-0.1.0.tgz"
assert candidate["source_commit"] == sys.argv[2]
assert candidate["version"] == "0.1.0"
assert candidate["platform"] == "linux/amd64"
assert candidate["chart_archive"] == archive.name
assert candidate["chart_archive_sha256"] == hashlib.sha256(archive.read_bytes()).hexdigest()
assert candidate["chart_content_sha256"]
assert candidate["image_config_digest"].startswith("sha256:")
assert candidate["image_manifest_digest"].startswith("sha256:")
assert candidate["image_archive"] == "kubeseer-controller.oci.tar"
assert candidate["image_reference"] == "ghcr.io/steeltanuki/kubeseer:0.1.0"
PY
	after_state="$(git -C "$FIXTURE_REPO" status --porcelain --untracked-files=all)"
	[[ "$before_state" == "$after_state" && -z "$after_state" ]] || fail "candidate staging changed the tagged source worktree"
	pass_case real-production-image-canonical-chart-and-clean-source

	local fake_bin="$temp_dir/fake-podman-bin"
	mkdir -p "$fake_bin"
	cat >"$fake_bin/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
	"build --pull=always"|"build --platform"|"build --format"|"build --timestamp"|"build --build-arg"|"build --tag"|"build --file") exit 0 ;;
	"save --format")
		python3 - "$5" <<'PY'
import hashlib
import io
import json
import pathlib
import tarfile
import sys
config = json.dumps({"architecture": "amd64", "os": "linux", "config": {}}, separators=(",", ":")).encode()
config_digest = "sha256:" + hashlib.sha256(config).hexdigest()
manifest = json.dumps({"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
                       "config": {"mediaType": "application/vnd.oci.image.config.v1+json", "digest": config_digest, "size": len(config)},
                       "layers": []}, separators=(",", ":")).encode()
manifest_digest = "sha256:" + hashlib.sha256(manifest).hexdigest()
index = json.dumps({"schemaVersion": 2, "manifests": [{"mediaType": "application/vnd.oci.image.manifest.v1+json",
                     "digest": manifest_digest, "size": len(manifest)}]}, separators=(",", ":")).encode()
with tarfile.open(pathlib.Path(sys.argv[1]), "w") as archive:
    for name, content in (("index.json", index), ("blobs/sha256/" + manifest_digest[7:], manifest)):
        item = tarfile.TarInfo(name)
        item.size = len(content)
        archive.addfile(item, io.BytesIO(content))
PY
		;;
	"image inspect")
		python3 - "$RELEASE_TEST_FAULT" "$RELEASE_TEST_VERSION" "$RELEASE_TEST_SHA" "$RELEASE_TEST_DATE" <<'PY'
import json
import sys

fault, version, revision, created = sys.argv[1:]
labels = {
    "org.opencontainers.image.title": "Kubeseer",
    "org.opencontainers.image.version": version,
    "org.opencontainers.image.revision": revision,
    "org.opencontainers.image.source": "https://github.com/steeltanuki/kubeseer",
    "org.opencontainers.image.created": created,
    "org.opencontainers.image.licenses": "Apache-2.0",
}
os_name, architecture = "linux", "amd64"
if fault == "version-label": labels["org.opencontainers.image.version"] = "9.9.9"
if fault == "revision-label": labels["org.opencontainers.image.revision"] = "0" * len(revision)
if fault == "platform": architecture = "arm64"
image = {
    "Id": "sha256:" + "a" * 64,
    "Os": os_name,
    "Architecture": architecture,
    "Config": {"Labels": labels, "User": "65532:65532", "Entrypoint": ["/kubeseer", "manager"]},
}
print(json.dumps([image]))
PY
		;;
	"run --rm")
		if [[ "$RELEASE_TEST_FAULT" == executable-version ]]; then
			printf 'kubeseer version=9.9.9 commit=%s date=%s\n' "$RELEASE_TEST_SHA" "$RELEASE_TEST_DATE"
		else
			printf 'kubeseer version=%s commit=%s date=%s\n' "$RELEASE_TEST_VERSION" "$RELEASE_TEST_SHA" "$RELEASE_TEST_DATE"
		fi
		;;
	"image rm") exit 0 ;;
	*) printf 'unexpected fake podman call: %s\n' "$*" >&2; exit 91 ;;
esac
EOF
	chmod +x "$fake_bin/podman"
	local commit_epoch build_date fault_output fault_name fault_reason fault_dir
	commit_epoch="$(git -C "$FIXTURE_REPO" show -s --format=%ct "$FIXTURE_SHA")"
	build_date="$(date -u -d "@$commit_epoch" '+%Y-%m-%dT%H:%M:%SZ')"
	for fault_name in version-label revision-label platform executable-version; do
		case "$fault_name" in
			version-label) fault_reason='OCI label org.opencontainers.image.version' ;;
			revision-label) fault_reason='OCI label org.opencontainers.image.revision' ;;
			platform) fault_reason='candidate image platform must be linux/amd64' ;;
			executable-version) fault_reason='controller executable metadata does not match' ;;
		esac
		fault_dir="$temp_dir/candidate-fault-$fault_name"
		mkdir -p "$fault_dir"
		if fault_output="$(cd "$FIXTURE_REPO" && PATH="$fake_bin:$PATH" RELEASE_TEST_FAULT="$fault_name" RELEASE_TEST_VERSION=0.1.0 RELEASE_TEST_SHA="$FIXTURE_SHA" RELEASE_TEST_DATE="$build_date" \
			./hack/release-distribution.sh stage --tag v0.1.0 --source-sha "$FIXTURE_SHA" --output-dir "$fault_dir" 2>&1)"; then
			fail "candidate accepted injected image identity mismatch: $fault_name"
		fi
		[[ "$fault_output" == *"$fault_reason"* ]] || fail "candidate rejected $fault_name for the wrong reason: $fault_output"
		pass_case "reject-injected-$fault_name"
	done

	local chart_version_dir="$temp_dir/candidate-fault-chart-version"
	mkdir -p "$chart_version_dir"
	sed -i 's/^version: 0.1.0$/version: 0.1.1/' "$FIXTURE_REPO/charts/kubeseer/Chart.yaml"
	commit_tagged_change "chart version mismatch" charts/kubeseer/Chart.yaml
	if fault_output="$(cd "$FIXTURE_REPO" && ./hack/release-distribution.sh stage --tag v0.1.0 --source-sha "$FIXTURE_SHA" --output-dir "$chart_version_dir" 2>&1)"; then
		fail "candidate accepted a mismatched chart version"
	fi
	[[ "$fault_output" == *"Chart.yaml version does not match"* ]] || fail "chart version mismatch failed for the wrong reason: $fault_output"
	pass_case reject-chart-version-mismatch

	local chart_image_dir="$temp_dir/candidate-fault-chart-image"
	mkdir -p "$chart_image_dir"
	sed -i 's/^version: 0.1.1$/version: 0.1.0/' "$FIXTURE_REPO/charts/kubeseer/Chart.yaml"
	sed -i 's#repository: ghcr.io/steeltanuki/kubeseer#repository: ghcr.io/example/kubeseer#' "$FIXTURE_REPO/charts/kubeseer/values.yaml"
	commit_tagged_change "default image mismatch" charts/kubeseer/Chart.yaml charts/kubeseer/values.yaml
	local fault_sha="$FIXTURE_SHA"
	commit_epoch="$(git -C "$FIXTURE_REPO" show -s --format=%ct "$fault_sha")"
	build_date="$(date -u -d "@$commit_epoch" '+%Y-%m-%dT%H:%M:%SZ')"
	if fault_output="$(cd "$FIXTURE_REPO" && PATH="$fake_bin:$PATH" RELEASE_TEST_FAULT=none RELEASE_TEST_VERSION=0.1.0 RELEASE_TEST_SHA="$fault_sha" RELEASE_TEST_DATE="$build_date" \
		./hack/release-distribution.sh stage --tag v0.1.0 --source-sha "$fault_sha" --output-dir "$chart_image_dir" 2>&1)"; then
		fail "candidate accepted a chart that renders a non-canonical default image"
	fi
	[[ "$fault_output" == *"packaged chart default images must all resolve"* ]] || fail "default image mismatch failed for the wrong reason: $fault_output"
	pass_case reject-chart-default-image-mismatch

	[[ -z "$(git -C "$FIXTURE_REPO" status --porcelain --untracked-files=all)" ]] || fail "candidate mismatch fixtures changed the source checkout after committing their inputs"
	pass_case candidate-fixtures-preserve-tagged-source
	[[ "$passed_cases" == "$expected_cases" ]] || fail "only $passed_cases of $expected_cases candidate cases ran"
	printf 'RELEASE_DISTRIBUTION=candidate STATUS=passed CASES=%s\n' "$passed_cases"
}

run_transaction() {
	expected_cases=19
	copy_repository_fixture transaction
	local candidate="$temp_dir/transaction-candidate" stage_output write_log real_podman real_helm registry_port api_port api_state api_log fixture_bin
	mkdir -p "$candidate"
	stage_output="$(cd "$FIXTURE_REPO" && ./hack/release-distribution.sh stage --tag v0.1.0 --source-sha "$FIXTURE_SHA" --output-dir "$candidate" 2>&1)" || fail "transaction fixture could not stage production release inputs: $stage_output"
	[[ "$stage_output" == *"RELEASE_DISTRIBUTION=candidate STATUS=passed VERSION=0.1.0 SOURCE_SHA=$FIXTURE_SHA"* ]] || fail "transaction fixture candidate has no release identity"
	pass_case stage-candidate-for-transaction-fixtures

	api_state="$temp_dir/github-state.json"
	api_log="$temp_dir/github-server.log"
	cat >"$temp_dir/github-api-fixture.py" <<'PY'
import http.server
import json
import pathlib
import sys
from urllib.parse import urlsplit
state_path = pathlib.Path(sys.argv[1])
class Handler(http.server.BaseHTTPRequestHandler):
    def respond(self, status, value=None):
        body = b"" if value is None else json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        if body:
            self.send_header("Content-Type", "application/json")
        self.end_headers()
        if body:
            self.wfile.write(body)
    def read_state(self):
        return json.loads(state_path.read_text(encoding="utf-8"))
    def write_state(self, value):
        state_path.write_text(json.dumps(value), encoding="utf-8")
    def record(self, state, method):
        state.setdefault("requests", []).append({"method": method, "path": self.path})
        self.write_state(state)
    def authorized(self):
        return self.headers.get("Authorization") == "Bearer fixture-token"
    def do_GET(self):
        state = self.read_state()
        self.record(state, "GET")
        if not self.authorized():
            return self.respond(401, {"message": "unauthorized"})
        path = urlsplit(self.path).path
        if path == "/healthz":
            return self.respond(200, {"ok": True})
        if path.endswith("/git/ref/tags/v0.1.0"):
            return self.respond(200, {"ref": "refs/tags/v0.1.0",
                                      "object": {"type": "commit", "sha": state["tag_sha"]}})
        if path.endswith("/releases/tags/v0.1.0"):
            if state.get("release") is None:
                return self.respond(404, {"message": "Not Found"})
            return self.respond(200, state["release"])
        return self.respond(404, {"message": "Not Found"})
    def do_POST(self):
        state = self.read_state()
        self.record(state, "POST")
        if not self.authorized():
            return self.respond(401, {"message": "unauthorized"})
        if state.get("fail_create"):
            return self.respond(503, {"message": "injected release creation failure"})
        if state.get("release") is not None:
            return self.respond(422, {"message": "release already exists"})
        value = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
        if value.get("tag_name") != "v0.1.0":
            return self.respond(422, {"message": "unexpected release tag"})
        release = {"id": 1, "tag_name": value["tag_name"], "name": value.get("name", ""),
                   "body": value.get("body", ""), "draft": False, "prerelease": False,
                   "html_url": "https://github.com/steeltanuki/kubeseer/releases/tag/v0.1.0"}
        state["release"] = release
        self.write_state(state)
        return self.respond(201, release)
    def log_message(self, *_):
        pass
server = http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[2])), Handler)
server.serve_forever()
PY
	api_port="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
	python3 - "$api_state" "$api_port" <<'PY'
import json, pathlib, sys
pathlib.Path(sys.argv[1]).write_text(json.dumps({"tag_sha": "", "release": None, "fail_create": False, "requests": []}))
PY
	python3 "$temp_dir/github-api-fixture.py" "$api_state" "$api_port" >"$api_log" 2>&1 &
	fixture_api_pid="$!"
	for _ in $(seq 1 30); do
		if curl --noproxy '*' -fsS "http://127.0.0.1:$api_port/healthz" >/dev/null 2>&1; then break; fi
		sleep 0.2
	done
	fixture_bin="$temp_dir/transaction-bin"
	mkdir -p "$fixture_bin"
	real_podman="$(command -v podman)"
	real_helm="$(command -v helm)"
	cat >"$fixture_bin/gh" <<'PY'
#!/usr/bin/env python3
import json, os, pathlib, sys, urllib.error, urllib.request
args = sys.argv[1:]
if len(args) < 3 or args[:2] != ["release", "create"] or "--verify-tag" not in args:
    raise SystemExit("fixture gh requires release create with --verify-tag")
tag = args[2]
def option(name):
    try: return args[args.index(name) + 1]
    except (ValueError, IndexError): raise SystemExit("missing gh option " + name)
notes = pathlib.Path(option("--notes-file")).read_text(encoding="utf-8")
base = os.environ["RELEASE_DISTRIBUTION_GITHUB_API_BASE"]
token = os.environ["GH_TOKEN"]
headers = {"Authorization": "Bearer " + token, "Accept": "application/vnd.github+json", "Content-Type": "application/json"}
tag_url = base + "/repos/steeltanuki/kubeseer/git/ref/tags/" + tag
with urllib.request.urlopen(urllib.request.Request(tag_url, headers=headers), timeout=10) as response:
    tag_data = json.load(response)
if tag_data["object"]["sha"] != os.environ["RELEASE_GATE_SOURCE_SHA"]:
    raise SystemExit("fixture gh refused a moved tag")
payload = {"tag_name": tag, "name": option("--title"), "body": notes, "draft": False, "prerelease": False}
url = base + "/repos/steeltanuki/kubeseer/releases"
request = urllib.request.Request(url, data=json.dumps(payload).encode(), headers=headers, method="POST")
try:
    with urllib.request.urlopen(request, timeout=10) as response:
        json.load(response)
except urllib.error.HTTPError as error:
    raise SystemExit("fixture gh release creation failed with HTTP " + str(error.code))
log = pathlib.Path(os.environ["RELEASE_TEST_WRITE_LOG"])
with log.open("a", encoding="utf-8") as output:
    output.write("github release create " + tag + "\n")
PY
	cat >"$fixture_bin/podman" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == push ]]; then
	printf 'podman push %s\n' "$*" >>"$RELEASE_TEST_WRITE_LOG"
	if [[ "${RELEASE_TEST_FAIL_IMAGE_PUSH:-}" == true ]]; then exit 87; fi
fi
exec "$RELEASE_REAL_PODMAN" "$@"
SH
	cat >"$fixture_bin/helm" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == push ]]; then
	printf 'helm push %s\n' "$*" >>"$RELEASE_TEST_WRITE_LOG"
	if [[ "${RELEASE_TEST_FAIL_CHART_PUSH:-}" == true ]]; then exit 88; fi
fi
exec "$RELEASE_REAL_HELM" "$@"
SH
	chmod +x "$fixture_bin/gh" "$fixture_bin/podman" "$fixture_bin/helm"
	write_log="$temp_dir/public-write-attempts.log"
	touch "$write_log"
	registry_start() {
		fixture_registry_name="kubeseer-release-transaction-$$"
		fixture_registry_data="$temp_dir/registry-data"
		podman rm --force --ignore "$fixture_registry_name" >/dev/null 2>&1 || true
		rm -rf "$fixture_registry_data"
		mkdir -p "$fixture_registry_data"
		registry_port="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
		podman run --detach --pull=never --name "$fixture_registry_name" --publish "127.0.0.1:$registry_port:5000" \
			--env REGISTRY_STORAGE_DELETE_ENABLED=true --volume "$fixture_registry_data:/var/lib/registry:Z" docker.io/library/registry:2.8.3 >/dev/null || fail "could not start disposable OCI registry"
		export RELEASE_DISTRIBUTION_REGISTRY_BASE="http://localhost:$registry_port"
		for _ in $(seq 1 50); do
			if curl --noproxy '*' -fsS "$RELEASE_DISTRIBUTION_REGISTRY_BASE/v2/" >/dev/null 2>&1; then return; fi
			sleep 0.2
		done
		fail "disposable OCI registry did not become ready"
	}
	reset_github() {
		python3 - "$api_state" "$FIXTURE_SHA" <<'PY'
import json, pathlib, sys
pathlib.Path(sys.argv[1]).write_text(json.dumps({"tag_sha": sys.argv[2], "release": None, "fail_create": False, "requests": []}))
PY
	}
	call_release() {
		local mode="$1"
		shift
		(cd "$FIXTURE_REPO" && env -u GITHUB_WORKSPACE PATH="$fixture_bin:$PATH" REALPODMAN="$real_podman" \
			RELEASE_REAL_PODMAN="$real_podman" RELEASE_REAL_HELM="$real_helm" RELEASE_TEST_WRITE_LOG="$write_log" \
			GITHUB_ACTIONS=true GITHUB_EVENT_NAME=push GITHUB_REPOSITORY=steeltanuki/kubeseer \
			GITHUB_REF=refs/tags/v0.1.0 GITHUB_REF_PROTECTED=true \
			GITHUB_WORKFLOW_REF=steeltanuki/kubeseer/.github/workflows/release.yml@refs/tags/v0.1.0 \
			GITHUB_JOB=publish GITHUB_SHA="$FIXTURE_SHA" RELEASE_GATE_SOURCE_SHA="$FIXTURE_SHA" \
			GITHUB_RUN_ID=78901 GITHUB_RUN_ATTEMPT=1 GITHUB_TOKEN=fixture-token GH_TOKEN=fixture-token GITHUB_ACTOR=fixture \
			RELEASE_DISTRIBUTION_TEST_ENDPOINTS=true RELEASE_DISTRIBUTION_REGISTRY_BASE="${TRANSACTION_REGISTRY_OVERRIDE:-$RELEASE_DISTRIBUTION_REGISTRY_BASE}" \
			RELEASE_DISTRIBUTION_GITHUB_API_BASE="http://127.0.0.1:$api_port" \
			"$SCRIPT" "$mode" --tag v0.1.0 --source-sha "$FIXTURE_SHA" --candidate-dir "$candidate" "$@")
	}
	registry_state() {
		local kind="$1" repo="$2" reference="$3"
		python3 "$ROOT_DIR/hack/release-distribution-http.py" registry-manifest --base "$RELEASE_DISTRIBUTION_REGISTRY_BASE" --repository "$repo" --reference "$reference" --kind "$kind"
	}
	registry_digest() {
		registry_state "$1" "$2" "$3" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("digest", ""))'
	}
	registry_status() {
		registry_state "$1" "$2" "$3" | python3 -c 'import json,sys;print(json.load(sys.stdin)["state"])'
	}
	delete_artifact() {
		local kind="$1" repository="$2" version="$3" digest
		digest="$(registry_digest "$kind" "$repository" "$version")"
		[[ -n "$digest" ]] || fail "cannot delete missing test artifact"
		curl --noproxy '*' -fsS -X DELETE -H 'Accept: application/vnd.oci.image.manifest.v1+json' \
			"$RELEASE_DISTRIBUTION_REGISTRY_BASE/v2/$repository/manifests/$digest" >/dev/null || fail "disposable registry refused test artifact deletion"
	}
	registry_start
	reset_github
	local inventory_output image_state chart_state release_state
	inventory_output="$(call_release inventory 2>&1)" || fail "read-only inventory failed against the local registry and recorded GitHub fixture: $inventory_output"
	python3 - "$inventory_output" <<'PY'
import json, sys
data = json.loads(sys.argv[1])
assert data["image"]["state"] == "absent"
assert data["chart"]["state"] == "absent"
assert data["github-release"]["state"] == "absent"
PY
	pass_case read-only-inventory-distinguishes-absent-release-artifacts
	[[ ! -s "$write_log" ]] || fail "read-only inventory attempted a public write"
	pass_case inventory-performs-no-public-writes
	local publish_output
	export RELEASE_TEST_FAIL_IMAGE_PUSH=true
	if publish_output="$(call_release publish-image 2>&1)"; then fail "injected image push failure was accepted"; fi
	unset RELEASE_TEST_FAIL_IMAGE_PUSH
	[[ "$publish_output" == *"controller image publication failed"* ]] || fail "image push failure was reported incorrectly: $publish_output"
	[[ "$(registry_status image steeltanuki/kubeseer 0.1.0)" == absent && "$(registry_status chart steeltanuki/charts/kubeseer 0.1.0)" == absent ]] || fail "failed image push left a public release artifact"
	pass_case failed-image-push-publishes-no-versioned-artifacts
	publish_output="$(call_release publish-image 2>&1)" || fail "controller image publication failed: $publish_output"
	[[ "$publish_output" == *"RELEASE_DISTRIBUTION=publish-image STATUS=passed VERSION=0.1.0"* ]] || fail "image publication returned no success identity"
	pass_case controller-image-published-to-disposable-registry
	export RELEASE_TEST_FAIL_CHART_PUSH=true
	if publish_output="$(call_release publish-chart 2>&1)"; then fail "injected Helm push failure was accepted"; fi
	unset RELEASE_TEST_FAIL_CHART_PUSH
	[[ "$publish_output" == *"Helm OCI chart publication failed"* ]] || fail "chart push failure was reported incorrectly: $publish_output"
	[[ "$(registry_status image steeltanuki/kubeseer 0.1.0)" == present && "$(registry_status chart steeltanuki/charts/kubeseer 0.1.0)" == absent ]] || fail "failed chart push did not leave the detectable image-only partial state"
	pass_case chart-push-failure-leaves-detectable-image-only-state
	publish_output="$(call_release publish-chart 2>&1)" || fail "Helm chart publication failed: $publish_output"
	[[ "$publish_output" == *"RELEASE_DISTRIBUTION=publish-chart STATUS=passed VERSION=0.1.0"* ]] || fail "chart publication returned no success identity"
	local release_before
	release_before="$(python3 - "$api_state" <<'PY'
import json, pathlib, sys
print("present" if json.loads(pathlib.Path(sys.argv[1]).read_text())["release"] else "absent")
PY
)"
	[[ "$release_before" == absent ]] || fail "GitHub Release was created before both verified OCI artifacts"
	pass_case chart-publication-precedes-github-release
	python3 - "$api_state" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
state = json.loads(path.read_text())
state["fail_create"] = True
path.write_text(json.dumps(state))
PY
	if publish_output="$(call_release publish-release 2>&1)"; then fail "injected GitHub Release creation failure was accepted"; fi
	[[ "$publish_output" == *"GitHub Release creation failed after GHCR publication"* ]] || fail "GitHub Release failure did not identify recoverable artifacts: $publish_output"
	[[ "$(registry_status image steeltanuki/kubeseer 0.1.0)" == present && "$(registry_status chart steeltanuki/charts/kubeseer 0.1.0)" == present ]] || fail "Release API failure removed an already verified registry artifact"
	[[ "$(python3 -c 'import json,sys;print("present" if json.load(open(sys.argv[1]))["release"] else "absent")' "$api_state")" == absent ]] || fail "failed Release creation left a partial GitHub Release"
	pass_case github-release-failure-leaves-verifiable-registry-artifacts
	python3 - "$api_state" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
state = json.loads(path.read_text())
state["fail_create"] = False
path.write_text(json.dumps(state))
PY
	publish_output="$(call_release publish-release 2>&1)" || fail "GitHub Release creation failed: $publish_output"
	[[ "$publish_output" == *"RELEASE_DISTRIBUTION=publish STATUS=passed VERSION=0.1.0"* ]] || fail "GitHub Release did not report its canonical identity"
	local write_count
	write_count="$(wc -l <"$write_log" | tr -d ' ')"
	[[ "$write_count" == 5 ]] || fail "release attempt log omitted image, chart, or GitHub Release writes"
	mapfile -t write_lines <"$write_log"
	[[ "${write_lines[0]}" == podman\ push* && "${write_lines[1]}" == podman\ push* && "${write_lines[2]}" == helm\ push* && "${write_lines[3]}" == helm\ push* && "${write_lines[4]}" == "github release create v0.1.0" ]] || fail "public release writes did not follow image, chart, Release order"
	pass_case github-release-created-last-with-tagged-identity
	for mode in publish-image publish-chart publish-release; do
		publish_output="$(call_release "$mode" 2>&1)" || fail "identical rerun failed in $mode: $publish_output"
	done
	[[ "$(wc -l <"$write_log" | tr -d ' ')" == "$write_count" ]] || fail "identical rerun attempted to overwrite an immutable public artifact"
	pass_case identical-rerun-reuses-verified-digests-without-writes
	python3 "$ROOT_DIR/hack/release-distribution-http.py" registry-manifest --base "$RELEASE_DISTRIBUTION_REGISTRY_BASE" --repository steeltanuki/kubeseer --reference latest --kind image >"$temp_dir/image-latest.json"
	python3 "$ROOT_DIR/hack/release-distribution-http.py" registry-manifest --base "$RELEASE_DISTRIBUTION_REGISTRY_BASE" --repository steeltanuki/charts/kubeseer --reference latest --kind chart >"$temp_dir/chart-latest.json"
	[[ "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["state"])' "$temp_dir/image-latest.json")" == absent ]] || fail "controller latest image tag was introduced"
	[[ "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["state"])' "$temp_dir/chart-latest.json")" == absent ]] || fail "Helm chart latest tag was introduced"
	pass_case no-latest-image-or-chart-artifact
	python3 - "$api_state" <<'PY'
import json, pathlib, sys
release = json.loads(pathlib.Path(sys.argv[1]).read_text())["release"]
body = release["body"]
required = ("# Kubeseer v0.1.0", "Source commit", "Certified Kubernetes versions", "1.35.6, 1.36.2",
           "Minimum Helm version", "3.12", "ghcr.io/steeltanuki/kubeseer:0.1.0",
           "oci://ghcr.io/steeltanuki/charts/kubeseer", "helm upgrade --install kubeseer",
           "## Release notes", "installation.md", "image_digest=sha256:", "chart_digest=sha256:")
missing = [item for item in required if item not in body]
assert not missing, "GitHub Release omits required user-facing identity or install details: " + repr(missing)
PY
	pass_case github-release-page-identifies-source-artifacts-and-installation
	delete_artifact chart steeltanuki/charts/kubeseer 0.1.0
	if publish_output="$(call_release audit 2>&1)"; then fail "audit accepted a missing published chart"; fi
	[[ "$publish_output" == *"release audit found one or more missing official artifacts"* ]] || fail "missing chart audit failed for the wrong reason: $publish_output"
	publish_output="$(call_release publish-chart 2>&1)" || fail "explicit chart-only recovery failed: $publish_output"
	publish_output="$(call_release audit 2>&1)" || fail "audit failed after chart recovery: $publish_output"
	[[ "$publish_output" == *"RELEASE_DISTRIBUTION=audit STATUS=passed VERSION=0.1.0"* ]] || fail "audit did not certify chart-only recovery"
	pass_case missing-chart-is-detected-and-restored-to-recorded-digest
	delete_artifact image steeltanuki/kubeseer 0.1.0
	if publish_output="$(call_release audit 2>&1)"; then fail "audit accepted a missing published image"; fi
	[[ "$publish_output" == *"release audit found one or more missing official artifacts"* ]] || fail "missing image audit failed for the wrong reason: $publish_output"
	publish_output="$(call_release publish-image 2>&1)" || fail "explicit image-only recovery failed: $publish_output"
	publish_output="$(call_release audit 2>&1)" || fail "audit failed after image recovery: $publish_output"
	[[ "$publish_output" == *"RELEASE_DISTRIBUTION=audit STATUS=passed VERSION=0.1.0"* ]] || fail "audit did not certify image-only recovery"
	pass_case missing-image-is-detected-and-restored-to-recorded-digest
	local stable_release_state="$temp_dir/github-state-stable.json" current_count
	cp "$api_state" "$stable_release_state"
	python3 - "$api_state" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
state = json.loads(path.read_text())
state["release"]["body"] += "\nConflicting release body fixture.\n"
path.write_text(json.dumps(state))
PY
	current_count="$(wc -l <"$write_log" | tr -d ' ')"
	if publish_output="$(call_release publish-release 2>&1)"; then fail "conflicting GitHub Release body was accepted"; fi
	[[ "$publish_output" == *"existing GitHub Release body conflicts with the immutable release candidate"* ]] || fail "conflicting Release failed for the wrong reason: $publish_output"
	[[ "$(wc -l <"$write_log" | tr -d ' ')" == "$current_count" ]] || fail "conflicting Release caused a public write"
	cp "$stable_release_state" "$api_state"
	pass_case conflicting-github-release-is-rejected-without-writes
	local original_chart_digest conflict_chart conflict_archive conflict_state
	original_chart_digest="$(registry_digest chart steeltanuki/charts/kubeseer 0.1.0)"
	conflict_chart="$temp_dir/conflicting-chart/kubeseer"
	mkdir -p "$temp_dir/conflicting-chart/package"
	cp -a "$FIXTURE_REPO/charts/kubeseer" "$conflict_chart"
	printf '# immutable conflict fixture\n' >"$conflict_chart/templates/release-conflict.yaml"
	"$real_helm" package "$conflict_chart" --destination "$temp_dir/conflicting-chart/package" >/dev/null
	"$real_helm" push "$temp_dir/conflicting-chart/package/kubeseer-0.1.0.tgz" "oci://$(python3 -c 'from urllib.parse import urlparse;import os;print(urlparse(os.environ["RELEASE_DISTRIBUTION_REGISTRY_BASE"]).netloc)')/steeltanuki/charts" --registry-config "$candidate/helm-anonymous.json" >/dev/null 2>&1 || fail "could not create conflicting local chart fixture"
	conflict_state="$(registry_digest chart steeltanuki/charts/kubeseer 0.1.0)"
	[[ "$conflict_state" != "$original_chart_digest" ]] || fail "chart conflict fixture did not change the immutable artifact digest"
	current_count="$(wc -l <"$write_log" | tr -d ' ')"
	if publish_output="$(call_release publish-chart 2>&1)"; then fail "different Helm archive was accepted for an immutable version"; fi
	[[ "$publish_output" == *"published Helm OCI chart bytes differ from the staged canonical chart"* ]] || fail "chart conflict failed for the wrong reason: $publish_output"
	[[ "$(wc -l <"$write_log" | tr -d ' ')" == "$current_count" ]] || fail "conflicting Helm chart caused a public replacement write"
	"$real_helm" push "$candidate/kubeseer-0.1.0.tgz" "oci://$(python3 -c 'from urllib.parse import urlparse;import os;print(urlparse(os.environ["RELEASE_DISTRIBUTION_REGISTRY_BASE"]).netloc)')/steeltanuki/charts" --registry-config "$candidate/helm-anonymous.json" >/dev/null 2>&1 || fail "could not restore the exact verified chart fixture"
	[[ "$(registry_digest chart steeltanuki/charts/kubeseer 0.1.0)" == "$original_chart_digest" ]] || fail "chart fixture recovery changed the recorded immutable digest"
	pass_case conflicting-chart-content-is-rejected-without-replacement
	local original_image_digest image_conflict_ref
	original_image_digest="$(registry_digest image steeltanuki/kubeseer 0.1.0)"
	image_conflict_ref="localhost:$(python3 -c 'from urllib.parse import urlparse;import os;print(urlparse(os.environ["RELEASE_DISTRIBUTION_REGISTRY_BASE"]).port)')/steeltanuki/kubeseer:0.1.0"
	"$real_podman" tag docker.io/library/alpine:3.20 "$image_conflict_ref" || fail "cannot prepare conflicting controller image fixture"
	"$real_podman" push --tls-verify=false "$image_conflict_ref" "docker://$image_conflict_ref" >/dev/null 2>&1 || fail "cannot publish conflicting controller image fixture to the disposable registry"
	[[ "$(registry_digest image steeltanuki/kubeseer 0.1.0)" != "$original_image_digest" ]] || fail "image conflict fixture did not change the immutable artifact digest"
	current_count="$(wc -l <"$write_log" | tr -d ' ')"
	if publish_output="$(call_release publish-image 2>&1)"; then fail "different controller image was accepted for an immutable version"; fi
	[[ "$publish_output" == *"controller image version is already published with a different manifest digest"* ]] || fail "image conflict failed for the wrong reason: $publish_output"
	[[ "$(wc -l <"$write_log" | tr -d ' ')" == "$current_count" ]] || fail "conflicting controller image caused a public replacement write"
	pass_case conflicting-image-digest-is-rejected-without-replacement
	python3 - "$api_state" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
state = json.loads(path.read_text())
state["tag_sha"] = "0" * 40
path.write_text(json.dumps(state))
PY
	current_count="$(wc -l <"$write_log" | tr -d ' ')"
	if publish_output="$(call_release publish-image 2>&1)"; then fail "tag pointing to a different commit was accepted"; fi
	[[ "$publish_output" == *"GitHub release tag resolves to a different source commit"* ]] || fail "moved tag failed for the wrong reason: $publish_output"
	[[ "$(wc -l <"$write_log" | tr -d ' ')" == "$current_count" ]] || fail "moved tag caused a public write"
	pass_case moved-tag-is-rejected-before-publication
	python3 - "$api_state" "$FIXTURE_SHA" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
state = json.loads(path.read_text())
state["tag_sha"] = sys.argv[2]
path.write_text(json.dumps(state))
PY
	local private_port private_pid
	private_port="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
	cat >"$temp_dir/private-registry.py" <<'PY'
import http.server, sys
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(401)
        self.send_header("WWW-Authenticate", 'Basic realm="private fixture"')
        self.send_header("Content-Length", "0")
        self.end_headers()
    def log_message(self, *_):
        pass
http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
	python3 "$temp_dir/private-registry.py" "$private_port" >/dev/null 2>&1 &
	private_pid="$!"
	fixture_aux_pid="$private_pid"
	TRANSACTION_REGISTRY_OVERRIDE="http://localhost:$private_port"
	export TRANSACTION_REGISTRY_OVERRIDE
	current_count="$(wc -l <"$write_log" | tr -d ' ')"
	if publish_output="$(call_release publish-image 2>&1)"; then fail "private GHCR package visibility was accepted"; fi
	[[ "$publish_output" == *"without a usable bearer challenge"* ]] || fail "private package failed for the wrong reason: $publish_output"
	[[ "$(wc -l <"$write_log" | tr -d ' ')" == "$current_count" ]] || fail "private package visibility caused a public write"
	unset TRANSACTION_REGISTRY_OVERRIDE
	kill "$private_pid" 2>/dev/null || true
	fixture_aux_pid=""
	pass_case private-package-is-not-misclassified-as-absent
	[[ "$passed_cases" == "$expected_cases" ]] || fail "only $passed_cases of $expected_cases transaction cases ran"
	printf 'RELEASE_DISTRIBUTION=transaction STATUS=passed CASES=%s\n' "$passed_cases"
}

run_docs() {
	expected_cases=2
	python3 - "$ROOT_DIR/README.md" "$ROOT_DIR/docs/installation.md" <<'PY'
import pathlib, sys
readme = " ".join(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").split())
install = " ".join(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8").split())
chart = "oci://ghcr.io/steeltanuki/charts/kubeseer"
image = "ghcr.io/steeltanuki/kubeseer"
required_readme = [
    "## Install an official release", chart, "--version 0.1.0",
    "## Install or build from source", "## Local development quick start",
    "without publishing them", "docs/local-development.md", "GitHub Releases page",
    "Kubernetes 1.35.6 and 1.36.2", "Helm 3.12 or newer",
]
missing = [item for item in required_readme if item not in readme]
assert not missing, "README release/source/local paths omit: " + repr(missing)
assert readme.index("## Install an official release") < readme.index("## Install or build from source") < readme.index("## Local development quick start"), "README install paths are not ordered release, source, local"
required_install = [
    "## Installing an official release", chart, "--version 0.1.0",
    "helm show chart", "helm template kubeseer", "Chart.yaml` `version` and `appVersion`",
    image, "## Working from a source checkout", "helm package charts/kubeseer",
    "local development guide", "not publish an image or chart",
    "vMAJOR.MINOR.PATCH", "Prerelease tags are not supported",
    "docs/releases/v<version>.md", "Upgrade considerations", "protected `develop`",
    "linux/amd64", "Kubernetes `1.35.6` and `1.36.2`", "Helm 3.12 or newer",
    "--version 0.1.0", "## Policy, RBAC, and upgrades", "CRD-bearing upgrade",
    "inventory --tag", "audit --tag", "same protected tag", "do not move the",
    "not attached to the GitHub Release", "not attached as a second download format",
    "GitHub Releases page",
]
missing = [item for item in required_install if item not in install]
assert not missing, "installation docs omit release path, metadata, upgrade, or recovery guidance: " + repr(missing)
assert install.index("## Installing an official release") < install.index("## Working from a source checkout"), "official OCI installation is not preferred over source packaging"
assert "releases/download/" not in install and "releases/download/" not in readme, "documentation exposes an attached release archive path"
print("PASS release-distribution/docs-release-source-local-contract")
PY
	pass_case official-source-local-and-upgrade-documentation

	copy_repository_fixture docs-audit
	local api_state="$temp_dir/docs-audit-api.json" api_port audit_output fixture_sha
	fixture_sha="$FIXTURE_SHA"
	api_port="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')"
	python3 - "$api_state" "$api_port" "$fixture_sha" <<'PY' >"$temp_dir/docs-audit-server.log" 2>&1 &
import http.server, json, pathlib, sys
state_path = pathlib.Path(sys.argv[1])
port, tag_sha = int(sys.argv[2]), sys.argv[3]
state_path.write_text(json.dumps({"requests": []}), encoding="utf-8")
class Handler(http.server.BaseHTTPRequestHandler):
    def respond(self, status, value=None):
        body = b"" if value is None else json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        if body:
            self.send_header("Content-Type", "application/json")
        self.end_headers()
        if body:
            self.wfile.write(body)
    def do_GET(self):
        state = json.loads(state_path.read_text(encoding="utf-8"))
        state["requests"].append({"method": "GET", "path": self.path})
        state_path.write_text(json.dumps(state), encoding="utf-8")
        if self.path.startswith("/v2/"):
            return self.respond(404, {"errors": [{"code": "MANIFEST_UNKNOWN"}]})
        if self.path.endswith("/git/ref/tags/v0.1.0"):
            return self.respond(200, {"ref": "refs/tags/v0.1.0", "object": {"type": "commit", "sha": tag_sha}})
        if self.path.endswith("/releases/tags/v0.1.0"):
            return self.respond(404, {"message": "Not Found"})
        return self.respond(404, {"message": "Not Found"})
    def do_POST(self):
        state = json.loads(state_path.read_text(encoding="utf-8"))
        state["requests"].append({"method": "POST", "path": self.path})
        state_path.write_text(json.dumps(state), encoding="utf-8")
        return self.respond(405, {"message": "read-only fixture"})
    def log_message(self, *_):
        pass
http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
	fixture_api_pid="$!"
	for _ in $(seq 1 30); do
		if curl --noproxy '*' -fsS "http://127.0.0.1:$api_port/healthz" >/dev/null 2>&1; then break; fi
		# The fixture deliberately has no health route; confirm its socket directly.
		if python3 - "$api_port" <<'PY' >/dev/null 2>&1
import socket, sys
s = socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=.2)
s.close()
PY
		then break; fi
		sleep 0.1
	done
	if audit_output="$(cd "$FIXTURE_REPO" && env -u GITHUB_ACTIONS GH_TOKEN=fixture-token \
		RELEASE_DISTRIBUTION_TEST_ENDPOINTS=true \
		RELEASE_DISTRIBUTION_REGISTRY_BASE="http://localhost:$api_port" \
		RELEASE_DISTRIBUTION_GITHUB_API_BASE="http://127.0.0.1:$api_port" \
		no_proxy="127.0.0.1,localhost" NO_PROXY="127.0.0.1,localhost" \
		./hack/release-distribution.sh audit --tag v0.1.0 --source-sha "$fixture_sha" 2>&1)"; then
		fail "audit accepted a fixture with no published artifacts"
	fi
	[[ "$audit_output" == *"release audit found one or more missing official artifacts"* ]] || fail "read-only audit fixture failed for the wrong reason: $audit_output"
	python3 - "$api_state" <<'PY'
import json, pathlib, sys
requests = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))["requests"]
assert requests, "audit made no public inventory requests"
assert all(request["method"] == "GET" for request in requests), "audit attempted a public write"
paths = [request["path"] for request in requests]
assert any("/v2/steeltanuki/kubeseer/manifests/0.1.0" in path for path in paths), "audit omitted controller image inventory"
assert any("/v2/steeltanuki/charts/kubeseer/manifests/0.1.0" in path for path in paths), "audit omitted Helm OCI chart inventory"
assert any(path.endswith("/releases/tags/v0.1.0") for path in paths), "audit omitted GitHub Release inventory"
PY
	pass_case release-audit-detects-absent-artifacts-with-read-only-requests
	[[ "$passed_cases" == "$expected_cases" ]] || fail "only $passed_cases of $expected_cases documentation cases ran"
	printf 'RELEASE_DISTRIBUTION=docs STATUS=passed CASES=%s\n' "$passed_cases"
}

run_workflows() {
	expected_cases=1
	local output fixture_count
	output="$(cd "$ROOT_DIR" && go run ./hack/verify-release-workflows.go "$ROOT_DIR" 2>&1)" || fail "workflow policy fixture failed: $output"
	fixture_count="$(printf '%s\n' "$output" | awk '/^PASS release-distribution\// { count++ } END { print count + 0 }')"
	[[ "$fixture_count" == 15 ]] || fail "workflow fixtures ran $fixture_count cases instead of 15: $output"
	[[ "$output" == *"RELEASE_WORKFLOW_POLICY=passed CASES=15"* ]] || fail "workflow verifier omitted its complete fixture marker: $output"
	printf '%s\n' "$output"
	pass_case parsed-workflows-and-event-policy-fixtures
	[[ "$passed_cases" == "$expected_cases" ]] || fail "only $passed_cases of $expected_cases workflow harness cases ran"
	printf 'RELEASE_DISTRIBUTION=workflows STATUS=passed CASES=%s\n' "$passed_cases"
}

if [[ "$SCENARIO_NAME" == all ]]; then
	for scenario in policy candidate transaction workflows docs; do
		SCENARIO="$scenario" "$0"
	done
	exit 0
fi

case "$SCENARIO_NAME" in
	policy) run_policy ;;
	candidate) run_candidate ;;
	transaction) run_transaction ;;
	workflows) run_workflows ;;
	docs) run_docs ;;
	*) fail "unknown or not yet implemented scenario: $SCENARIO_NAME" ;;
esac
