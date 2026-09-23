#!/usr/bin/env bash
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

set -euo pipefail

readonly repository="steeltanuki/kubeseer"
readonly workflow_path=".github/workflows/release.yml"
readonly http_helper="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/release-distribution-http.py"
stage_output_dir=""
stage_output_owned=false
staged_image_ref=""

cleanup_release() {
	if [[ -n "$staged_image_ref" ]] && command -v podman >/dev/null 2>&1; then
		podman image rm --force "$staged_image_ref" >/dev/null 2>&1 || true
	fi
	if [[ "$stage_output_owned" == true && -n "$stage_output_dir" ]]; then
		chmod -R u+w "$stage_output_dir" 2>/dev/null || true
		rm -rf "$stage_output_dir"
	fi
}
trap cleanup_release EXIT

fail() {
	local message="$1"
	printf 'RELEASE_DISTRIBUTION=source STATUS=failed: %s\n' "$message" >&2
	exit 1
}

usage() {
	cat >&2 <<'EOF'
usage: hack/release-distribution.sh validate --tag vMAJOR.MINOR.PATCH --source-sha FULL_SHA
       hack/release-distribution.sh stage --tag vMAJOR.MINOR.PATCH --source-sha FULL_SHA [--output-dir RUN_TEMP_DIR]
       hack/release-distribution.sh inventory|audit --tag vMAJOR.MINOR.PATCH --source-sha FULL_SHA
       hack/release-distribution.sh publish --tag vMAJOR.MINOR.PATCH --source-sha FULL_SHA [--output-dir RUN_TEMP_DIR]
       hack/release-distribution.sh publish-image|publish-chart|publish-release --tag vMAJOR.MINOR.PATCH --source-sha FULL_SHA --candidate-dir RUN_TEMP_DIR
EOF
	exit 2
}

registry_base() {
	local value="${RELEASE_DISTRIBUTION_REGISTRY_BASE:-https://ghcr.io}"
	[[ "$value" != *" "* && "$value" != *$'\n'* ]] || fail "registry base URL is malformed"
	if [[ "${RELEASE_DISTRIBUTION_TEST_ENDPOINTS:-}" != true && "$value" != https://ghcr.io ]]; then
		fail "registry endpoint overrides are available only to local release-distribution fixtures"
	fi
	if [[ "${GITHUB_ACTIONS:-}" == true && "${RELEASE_DISTRIBUTION_TEST_ENDPOINTS:-}" != true && "$value" != https://ghcr.io ]]; then
		fail "official release publication is pinned to GitHub Container Registry"
	fi
	if [[ "${GITHUB_ACTIONS:-}" == true && "${RELEASE_DISTRIBUTION_TEST_ENDPOINTS:-}" == true && -n "${GITHUB_WORKSPACE:-}" ]]; then
		fail "local fixture endpoints are forbidden in a GitHub Actions workspace"
	fi
	printf '%s' "${value%/}"
}

github_api_base() {
	local value="${RELEASE_DISTRIBUTION_GITHUB_API_BASE:-https://api.github.com}"
	[[ "$value" != *" "* && "$value" != *$'\n'* ]] || fail "GitHub API base URL is malformed"
	if [[ "${RELEASE_DISTRIBUTION_TEST_ENDPOINTS:-}" != true && "$value" != https://api.github.com ]]; then
		fail "GitHub API endpoint overrides are available only to local release-distribution fixtures"
	fi
	if [[ "${GITHUB_ACTIONS:-}" == true && "${RELEASE_DISTRIBUTION_TEST_ENDPOINTS:-}" != true && "$value" != https://api.github.com ]]; then
		fail "official release publication is pinned to GitHub's public API"
	fi
	if [[ "${GITHUB_ACTIONS:-}" == true && "${RELEASE_DISTRIBUTION_TEST_ENDPOINTS:-}" == true && -n "${GITHUB_WORKSPACE:-}" ]]; then
		fail "local fixture endpoints are forbidden in a GitHub Actions workspace"
	fi
	printf '%s' "${value%/}"
}

is_plain_http_registry() {
	[[ "$(registry_base)" == http://* ]]
}

registry_host() {
	python3 - "$(registry_base)" <<'PY'
import sys
from urllib.parse import urlparse
parsed = urlparse(sys.argv[1])
if parsed.scheme not in ("https", "http") or not parsed.netloc or parsed.path not in ("", "/"):
    raise SystemExit("registry base must be an HTTP(S) origin without a path")
print(parsed.netloc)
PY
}

registry_probe() {
	local kind="$1" repo="$2" reference="$3" output="$4"
	python3 "$http_helper" registry-manifest --base "$(registry_base)" --repository "$repo" --reference "$reference" --kind "$kind" >"$output" || fail "cannot inventory public $kind artifact $repo:$reference"
}

github_tag_identity() {
	local tag="$1" output="$2"
	python3 "$http_helper" github-tag --base "$(github_api_base)" --repository "$repository" --tag "$tag" >"$output" || fail "cannot resolve official GitHub release tag"
}

github_release_identity() {
	local tag="$1" output="$2"
	python3 "$http_helper" github-release --base "$(github_api_base)" --repository "$repository" --tag "$tag" >"$output" || fail "cannot inventory the GitHub Release"
}

stable_version() {
	local tag="$1"
	local pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
	[[ "$tag" =~ $pattern ]] || fail "release tag must use stable vMAJOR.MINOR.PATCH SemVer"
	printf '%s' "${tag#v}"
}

chart_field() {
	local chart="$1"
	local field="$2"
	python3 - "$chart" "$field" <<'PY'
import pathlib
import re
import sys

path = pathlib.Path(sys.argv[1])
field = sys.argv[2]
matches = []
for line in path.read_text(encoding="utf-8").splitlines():
    match = re.fullmatch(rf"{re.escape(field)}:\s*(.*?)\s*(?:#.*)?", line)
    if match:
        value = match.group(1).strip().strip("\"'")
        matches.append(value)
if len(matches) != 1 or not matches[0]:
    raise SystemExit(f"expected exactly one non-empty root {field} in {path}")
print(matches[0])
PY
}

has_release_note_sections() {
	local notes="$1"
	awk '
		/^##[[:space:]]+/ {
			section = $0
			sub(/^##[[:space:]]+/, "", section)
			next
		}
		section == "Highlights" && /^[[:space:]]*[-*][[:space:]]+/ && length($0) > 22 { highlights = 1 }
		section ~ /^(Upgrade considerations|Upgrade guidance|Upgrade guide)$/ && /^[[:space:]]*[-*][[:space:]]+/ && length($0) > 22 { upgrade = 1 }
		END { exit !(highlights && upgrade) }
	' "$notes"
}

tree_digest() {
	local directory="$1"
	python3 - "$directory" <<'PY'
import hashlib
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
digest = hashlib.sha256()
for path in sorted(p for p in root.rglob("*") if p.is_file()):
    if path.name == "Chart.yaml":
        continue
    if path.is_symlink():
        raise SystemExit(f"chart package must not contain symlinks: {path}")
    relative = path.relative_to(root).as_posix().encode("utf-8")
    digest.update(relative + b"\0")
    digest.update(path.read_bytes())
    digest.update(b"\0")
print(digest.hexdigest())
PY
}

verify_image_candidate() {
	local inspect_file="$1"
	local expected_version="$2"
	local expected_sha="$3"
	local expected_date="$4"
	python3 - "$inspect_file" "$expected_version" "$expected_sha" "$expected_date" <<'PY'
import json
import pathlib
import re
import sys

images = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
if not isinstance(images, list) or len(images) != 1:
    raise SystemExit("expected exactly one inspected candidate image")
image = images[0]
config = image.get("Config") or {}
labels = config.get("Labels") or image.get("Labels") or {}
version, revision, created = sys.argv[2:]
required_labels = {
    "org.opencontainers.image.title": "Kubeseer",
    "org.opencontainers.image.version": version,
    "org.opencontainers.image.revision": revision,
    "org.opencontainers.image.source": "https://github.com/steeltanuki/kubeseer",
    "org.opencontainers.image.created": created,
    "org.opencontainers.image.licenses": "Apache-2.0",
}
for key, expected in required_labels.items():
    if labels.get(key) != expected:
        raise SystemExit(f"OCI label {key} does not match {expected!r}")
if image.get("Os", "").lower() != "linux" or image.get("Architecture", "").lower() != "amd64":
    raise SystemExit("candidate image platform must be linux/amd64")
if config.get("User") not in ("65532:65532", "65532"):
    raise SystemExit("candidate image must run as UID 65532")
if config.get("Entrypoint") != ["/kubeseer", "manager"]:
    raise SystemExit("candidate image must use the production manager entrypoint")
image_id = str(image.get("Id", ""))
if not re.fullmatch(r"(?:sha256:)?[0-9a-f]{64}", image_id):
    raise SystemExit("candidate image inspection did not expose its immutable config digest")
print(image_id if image_id.startswith("sha256:") else "sha256:" + image_id)
PY
}

set_stage_output_dir() {
	local requested="$1"
	local root="$2"
	if [[ -n "$requested" ]]; then
		mkdir -p "$requested"
		stage_output_dir="$(cd -- "$requested" && pwd)"
		case "$stage_output_dir/" in
			"$root/"*) fail "candidate output must be outside the tagged source checkout" ;;
		esac
		[[ -z "$(find "$stage_output_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]] || fail "candidate output directory must be empty"
		stage_output_owned=false
	else
		stage_output_dir="$(mktemp -d "${TMPDIR:-/tmp}/kubeseer-release-candidate.XXXXXX")" || fail "cannot create run-owned candidate directory"
		stage_output_owned=true
	fi
}

stage_candidate() {
	local tag="$1"
	local source_sha="$2"
	local requested_output_dir="$3"
	local root version commit_epoch build_date nonce expected_image image_id chart_version chart_app_version chart_source_digest chart_archive_digest chart_metadata_digest chart_content_digest
	validate_source "$tag" "$source_sha" >/dev/null
	root="$(git rev-parse --show-toplevel)"
	version="$(stable_version "$tag")"
	set_stage_output_dir "$requested_output_dir" "$root"
	commit_epoch="$(git -C "$root" show -s --format=%ct "$source_sha")"
	[[ "$commit_epoch" =~ ^[0-9]+$ ]] || fail "tagged commit has no valid timestamp"
	command -v date >/dev/null 2>&1 || fail "date is required to derive the reproducible build timestamp"
	build_date="$(date -u -d "@$commit_epoch" '+%Y-%m-%dT%H:%M:%SZ')" || fail "cannot convert tagged commit timestamp"
	command -v podman >/dev/null 2>&1 || fail "podman is required to build the production OCI image"
	command -v helm >/dev/null 2>&1 || fail "helm is required to package the canonical chart"
	command -v tar >/dev/null 2>&1 || fail "tar is required to inspect the packaged chart"
	command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required to identify the candidate chart"
	command -v rg >/dev/null 2>&1 || fail "ripgrep is required for candidate verification"
	command -v python3 >/dev/null 2>&1 || fail "python3 is required for candidate metadata inspection"

	nonce="${GITHUB_RUN_ID:-local}-$RANDOM-$RANDOM-$$"
	nonce="$(printf '%s' "$nonce" | tr -cd '[:alnum:]._-')"
	staged_image_ref="localhost/kubeseer-release-candidate:${version}-${nonce}"
	podman build --pull=always --platform linux/amd64 --format oci --timestamp "$commit_epoch" \
		--build-arg "VERSION=$version" --build-arg "COMMIT=$source_sha" --build-arg "BUILD_DATE=$build_date" \
		--tag "$staged_image_ref" --file "$root/Dockerfile" "$root" || fail "production Dockerfile image build failed"
	podman image inspect "$staged_image_ref" >"$stage_output_dir/image-inspect.json" || fail "cannot inspect built controller image"
	image_id="$(verify_image_candidate "$stage_output_dir/image-inspect.json" "$version" "$source_sha" "$build_date")" || fail "controller image metadata or security contract failed"
	expected_image="ghcr.io/steeltanuki/kubeseer:$version"
	local version_output
	version_output="$(podman run --rm --network=none --pull=never --entrypoint /kubeseer "$staged_image_ref" version 2>&1)" || fail "built controller executable could not report its release identity"
	[[ "$version_output" == "kubeseer version=$version commit=$source_sha date=$build_date" ]] || fail "controller executable metadata does not match the release identity"
	local image_archive="$stage_output_dir/kubeseer-controller.oci.tar"
	podman save --format oci-archive --output "$image_archive" "$staged_image_ref" >/dev/null || fail "cannot preserve the verified OCI image for release publication"
	local image_manifest_digest
	image_manifest_digest="$(python3 "$http_helper" archive-manifest --archive "$image_archive" | python3 -c 'import json,sys; print(json.load(sys.stdin)["manifest_digest"])')" || fail "cannot identify the staged OCI image manifest"

	local chart_stage="$stage_output_dir/chart-source"
	mkdir "$chart_stage"
	cp -a "$root/charts/kubeseer/." "$chart_stage/"
	find "$chart_stage" -exec touch -h -d "@$commit_epoch" {} + || fail "cannot normalize canonical chart package timestamps"
	helm package "$chart_stage" --destination "$stage_output_dir" >/dev/null || fail "canonical Helm chart packaging failed"
	local archive="$stage_output_dir/kubeseer-$version.tgz"
	[[ -f "$archive" ]] || fail "canonical Helm package did not produce kubeseer-$version.tgz"
	[[ "$(find "$stage_output_dir" -maxdepth 1 -type f -name 'kubeseer-*.tgz' | wc -l | tr -d ' ')" == 1 ]] || fail "candidate directory must contain exactly one Kubeseer chart archive"
	tar -tzf "$archive" >"$stage_output_dir/archive-files.txt" || fail "cannot list packaged Helm chart"
python3 - "$stage_output_dir/archive-files.txt" <<'PY'
import pathlib
import sys

for item in pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").splitlines():
    path = pathlib.PurePosixPath(item)
    if path.is_absolute() or ".." in path.parts or not item.startswith("kubeseer/"):
        raise SystemExit(f"unsafe or unexpected Helm archive path: {item}")
PY
	mkdir "$stage_output_dir/unpacked"
	tar -xzf "$archive" --no-same-owner --no-same-permissions -C "$stage_output_dir/unpacked"
	local unpacked_chart="$stage_output_dir/unpacked/kubeseer"
	[[ -f "$unpacked_chart/Chart.yaml" ]] || fail "Helm archive lacks the canonical Chart.yaml"
	helm show chart "$archive" >/dev/null || fail "Helm cannot read the packaged chart metadata"
	chart_version="$(chart_field "$unpacked_chart/Chart.yaml" version)" || fail "cannot read packaged chart version"
	chart_app_version="$(chart_field "$unpacked_chart/Chart.yaml" appVersion)" || fail "cannot read packaged chart appVersion"
	[[ "$chart_version" == "$version" ]] || fail "packaged Helm chart version does not match release version"
	[[ "$chart_app_version" == "$version" ]] || fail "packaged Helm chart appVersion does not match release version"
	helm show chart "$root/charts/kubeseer" >"$stage_output_dir/source-chart-metadata.yaml" || fail "Helm cannot read tagged canonical chart metadata"
	helm show chart "$archive" >"$stage_output_dir/packaged-chart-metadata.yaml" || fail "Helm cannot read packaged chart metadata"
	cmp -s "$stage_output_dir/source-chart-metadata.yaml" "$stage_output_dir/packaged-chart-metadata.yaml" || fail "packaged chart metadata differs from the tagged canonical chart"
	chart_source_digest="$(tree_digest "$root/charts/kubeseer")" || fail "cannot hash the tagged canonical chart"
	local chart_packaged_digest
	chart_packaged_digest="$(tree_digest "$unpacked_chart")" || fail "cannot hash the packaged canonical chart"
	[[ "$chart_source_digest" == "$chart_packaged_digest" ]] || fail "packaged chart content differs from the tagged canonical chart"
	chart_metadata_digest="$(sha256sum "$stage_output_dir/source-chart-metadata.yaml" | awk '{print $1}')"
	chart_content_digest="$(printf '%s\n%s\n' "$chart_metadata_digest" "$chart_source_digest" | sha256sum | awk '{print $1}')"
	helm template kubeseer "$archive" --namespace kubeseer-system --kube-version 1.35.6 --include-crds >"$stage_output_dir/rendered-chart.yaml" || fail "packaged chart cannot be rendered with the certified Kubernetes version"
	python3 - "$stage_output_dir/rendered-chart.yaml" "$expected_image" <<'PY'
import pathlib
import re
import sys

rendered = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
expected = sys.argv[2]
images = []
for line in rendered.splitlines():
    match = re.match(r"^\s*image:\s*[\"']?([^\"'\s]+)", line)
    if match:
        images.append(match.group(1))
if not images or any(image != expected for image in images):
    raise SystemExit(f"packaged chart default images must all resolve to {expected}; got {images}")
if any(image.endswith(":latest") for image in images):
    raise SystemExit("packaged chart must not render the latest image tag")
PY
	chart_archive_digest="$(sha256sum "$archive" | awk '{print $1}')"
	python3 - "$stage_output_dir/candidate.json" "$tag" "$version" "$source_sha" "$build_date" "$staged_image_ref" "$image_id" "$image_manifest_digest" "$expected_image" "$chart_archive_digest" "$chart_content_digest" <<'PY'
import json
import pathlib
import sys

path, tag, version, revision, build_date, image_ref, image_id, image_manifest_digest, default_image, chart_digest, chart_content_digest = sys.argv[1:]
data = {
    "tag": tag,
    "version": version,
    "source_commit": revision,
    "build_date": build_date,
    "platform": "linux/amd64",
    "image_candidate": image_ref,
    "image_config_digest": image_id,
    "image_manifest_digest": image_manifest_digest,
    "image_archive": "kubeseer-controller.oci.tar",
    "image_reference": default_image,
    "chart_archive": f"kubeseer-{version}.tgz",
    "chart_archive_sha256": chart_digest,
    "chart_content_sha256": chart_content_digest,
}
pathlib.Path(path).write_text(json.dumps(data, sort_keys=True, indent=2) + "\n", encoding="utf-8")
PY
	[[ -z "$(git -C "$root" status --porcelain --untracked-files=all)" ]] || fail "candidate staging changed the tagged source worktree"
	printf 'RELEASE_DISTRIBUTION=candidate STATUS=passed VERSION=%s SOURCE_SHA=%s PLATFORM=linux/amd64 IMAGE_ID=%s CHART_SHA256=%s\n' \
		"$version" "$source_sha" "$image_id" "$chart_archive_digest"
}

validate_source() {
	local tag="$1"
	local requested_sha="$2"
	local version root resolved_sha head_sha object_format notes_path note_file chart_version chart_app_version
	version="$(stable_version "$tag")"
	[[ -n "$requested_sha" ]] || fail "a full tagged source SHA is required"
	command -v git >/dev/null 2>&1 || fail "git is required"
	command -v python3 >/dev/null 2>&1 || fail "python3 is required to inspect Chart.yaml"
	root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail "current directory is not a Git checkout"
	object_format="$(git -C "$root" rev-parse --show-object-format 2>/dev/null)" || fail "cannot determine Git object format"
	case "$object_format" in
		sha1) [[ "$requested_sha" =~ ^[0-9a-f]{40}$ ]] || fail "source SHA must be a full lowercase sha1 commit" ;;
		sha256) [[ "$requested_sha" =~ ^[0-9a-f]{64}$ ]] || fail "source SHA must be a full lowercase sha256 commit" ;;
		*) fail "unsupported Git object format: $object_format" ;;
	esac
	resolved_sha="$(git -C "$root" rev-parse --verify "refs/tags/$tag^{commit}" 2>/dev/null)" || fail "release tag does not resolve to a commit: $tag"
	[[ "$resolved_sha" == "$requested_sha" ]] || fail "requested source SHA does not match resolved tag commit"
	head_sha="$(git -C "$root" rev-parse --verify 'HEAD^{commit}' 2>/dev/null)" || fail "HEAD does not resolve to a commit"
	[[ "$head_sha" == "$resolved_sha" ]] || fail "checkout HEAD is not the exact tagged source commit"
	[[ -z "$(git -C "$root" status --porcelain --untracked-files=all)" ]] || fail "tagged source worktree is not clean"
	git -C "$root" show-ref --verify --quiet refs/remotes/origin/develop || fail "origin/develop is required to validate protected release lineage"
	git -C "$root" merge-base --is-ancestor "$resolved_sha" refs/remotes/origin/develop || fail "tagged commit is not reachable from origin/develop"
	git -C "$root" cat-file -e "$resolved_sha:$workflow_path" 2>/dev/null || fail "tagged source does not contain the official release workflow"

	local chart="$root/charts/kubeseer/Chart.yaml"
	[[ -f "$chart" ]] || fail "tagged source is missing charts/kubeseer/Chart.yaml"
	chart_version="$(chart_field "$chart" version)" || fail "cannot read Chart.yaml version"
	chart_app_version="$(chart_field "$chart" appVersion)" || fail "cannot read Chart.yaml appVersion"
	[[ "$chart_version" == "$version" ]] || fail "Chart.yaml version does not match release version $version"
	[[ "$chart_app_version" == "$version" ]] || fail "Chart.yaml appVersion does not match release version $version"

	notes_path="docs/releases/$tag.md"
	git -C "$root" cat-file -e "$resolved_sha:$notes_path" 2>/dev/null || fail "tagged source is missing maintainer release notes: $notes_path"
	note_file="$(mktemp "${TMPDIR:-/tmp}/kubeseer-release-notes.XXXXXX")" || fail "cannot create temporary release-note inspection file"
	if ! git -C "$root" show "$resolved_sha:$notes_path" >"$note_file"; then
		rm -f "$note_file"
		fail "cannot read tagged maintainer release notes: $notes_path"
	fi
	if ! rg -Fqx "# Kubeseer $tag" "$note_file"; then
		rm -f "$note_file"
		fail "release notes must identify $tag in the title"
	fi
	if rg -qi '(^|[^[:alpha:]])(TODO|TBD|PLACEHOLDER|FILL[[:space:]]+IN)([^[:alpha:]]|$)' "$note_file"; then
		rm -f "$note_file"
		fail "release notes contain unfinished placeholder text"
	fi
	if ! has_release_note_sections "$note_file"; then
		rm -f "$note_file"
		fail "release notes need substantive Highlights and Upgrade considerations sections"
	fi
	rm -f "$note_file"

	printf 'RELEASE_SOURCE_VERSION=%s\n' "$version"
	printf 'RELEASE_SOURCE_SHA=%s\n' "$resolved_sha"
}

require_official_publish_context() {
	local tag="$1"
	local source_sha="$2"
	[[ "${GITHUB_ACTIONS:-}" == true ]] || fail "publication requires the official GitHub Actions tag workflow"
	[[ "${GITHUB_EVENT_NAME:-}" == push ]] || fail "publication requires a protected tag push event"
	[[ "${GITHUB_REPOSITORY:-}" == "$repository" ]] || fail "publication is restricted to $repository"
	[[ "${GITHUB_REF:-}" == "refs/tags/$tag" ]] || fail "publication ref does not match the requested release tag"
	[[ "${GITHUB_REF_PROTECTED:-}" == true ]] || fail "release tag is not protected by a GitHub ruleset"
	[[ "${GITHUB_WORKFLOW_REF:-}" == "$repository/$workflow_path@refs/tags/$tag" ]] || fail "publication workflow is not loaded from the tagged repository workflow"
	[[ "${GITHUB_JOB:-}" == publish ]] || fail "publication is restricted to the release publish job"
	[[ "${RELEASE_DISTRIBUTION_TEST_ENDPOINTS:-}" != true || -z "${GITHUB_WORKSPACE:-}" ]] || fail "local fixture endpoints cannot be used by an official GitHub Actions workspace"
	[[ "${GITHUB_SHA:-}" == "$source_sha" ]] || fail "GitHub event SHA does not match the requested source commit"
	[[ "${RELEASE_GATE_SOURCE_SHA:-}" == "$source_sha" ]] || fail "release gates did not hand off the same source commit"
	[[ "${GITHUB_RUN_ID:-}" =~ ^[0-9]+$ && "${GITHUB_RUN_ATTEMPT:-}" =~ ^[0-9]+$ ]] || fail "official workflow run identity is missing"
	[[ -n "${GITHUB_TOKEN:-}" ]] || fail "GITHUB_TOKEN is required in the official publish job"
	validate_source "$tag" "$source_sha" >/dev/null
}

json_value() {
	local file="$1" path="$2"
	python3 - "$file" "$path" <<'PY'
import json
import sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
for key in sys.argv[2].split("."):
    value = value.get(key) if isinstance(value, dict) else None
    if value is None:
        print("")
        raise SystemExit(0)
if isinstance(value, bool):
    print(json.dumps(value))
elif isinstance(value, (str, int, float)):
    print(value)
else:
    print(json.dumps(value, sort_keys=True))
PY
}

candidate_value() {
	json_value "$1/candidate.json" "$2"
}

validate_candidate() {
	local directory="$1" tag="$2" source_sha="$3" version="$4"
	[[ -d "$directory" && -f "$directory/candidate.json" ]] || fail "candidate directory lacks candidate.json"
	[[ -f "$directory/kubeseer-controller.oci.tar" && -f "$directory/kubeseer-$version.tgz" ]] || fail "candidate directory lacks the staged OCI image or Helm archive"
	python3 - "$directory/candidate.json" "$tag" "$version" "$source_sha" <<'PY'
import json
import pathlib
import re
import sys
data = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
tag, version, source = sys.argv[2:]
expected = {"tag": tag, "version": version, "source_commit": source,
            "platform": "linux/amd64", "image_reference": f"ghcr.io/steeltanuki/kubeseer:{version}",
            "image_archive": "kubeseer-controller.oci.tar", "chart_archive": f"kubeseer-{version}.tgz"}
for key, value in expected.items():
    if data.get(key) != value:
        raise SystemExit(f"candidate {key} does not match the verified release source")
for key in ("image_config_digest", "image_manifest_digest"):
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", str(data.get(key, ""))):
        raise SystemExit(f"candidate {key} is not a SHA-256 digest")
PY
	local archive_digest manifest_digest
	archive_digest="$(sha256sum "$directory/kubeseer-$version.tgz" | awk '{print $1}')"
	[[ "$archive_digest" == "$(candidate_value "$directory" chart_archive_sha256)" ]] || fail "candidate Helm archive changed after staging"
	manifest_digest="$(python3 "$http_helper" archive-manifest --archive "$directory/kubeseer-controller.oci.tar" | python3 -c 'import json,sys; print(json.load(sys.stdin)["manifest_digest"])')" || fail "cannot verify staged OCI image manifest"
	[[ "$manifest_digest" == "$(candidate_value "$directory" image_manifest_digest)" ]] || fail "candidate OCI image manifest changed after staging"
}

assert_public_image() {
	local file="$1" version="$2" source_sha="$3" expected_digest="$4" state digest
	state="$(json_value "$file" state)"
	[[ "$state" == present ]] || return 1
	digest="$(json_value "$file" digest)"
	[[ -z "$expected_digest" || "$digest" == "$expected_digest" ]] || fail "controller image version is already published with a different manifest digest"
	python3 - "$file" "$version" "$source_sha" <<'PY'
import json
import pathlib
import sys
data = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
labels = data.get("labels") or {}
version, revision = sys.argv[2:]
expected = {
    "org.opencontainers.image.title": "Kubeseer",
    "org.opencontainers.image.version": version,
    "org.opencontainers.image.revision": revision,
    "org.opencontainers.image.source": "https://github.com/steeltanuki/kubeseer",
    "org.opencontainers.image.licenses": "Apache-2.0",
}
for key, value in expected.items():
    if labels.get(key) != value:
        raise SystemExit(f"published image OCI label {key} does not match release metadata")
if data.get("os") != "linux" or data.get("architecture") != "amd64":
    raise SystemExit("published controller image does not support the certified linux/amd64 platform")
PY
}

verify_public_chart() {
	local version="$1" candidate_dir="$2" scratch="$3" expected_digest="$4"
	local pull_dir="$scratch/chart-pull" archive="$scratch/chart-pull/kubeseer-$version.tgz" chart_ref="oci://$(registry_host)/steeltanuki/charts/kubeseer"
	local -a flags=()
	mkdir -p "$pull_dir"
	helm pull "$chart_ref" --version "$version" --destination "$pull_dir" --registry-config "$candidate_dir/helm-anonymous.json" "${flags[@]}" >/dev/null || fail "published Helm OCI chart is not publicly retrievable"
	[[ -f "$archive" ]] || fail "Helm OCI pull did not produce the expected versioned chart archive"
	cmp -s "$candidate_dir/kubeseer-$version.tgz" "$archive" || fail "published Helm OCI chart bytes differ from the staged canonical chart"
	local metadata_version metadata_app_version archive_digest
	metadata_version="$(chart_field "$candidate_dir/unpacked/kubeseer/Chart.yaml" version)" || fail "staged chart metadata cannot be read"
	metadata_app_version="$(chart_field "$candidate_dir/unpacked/kubeseer/Chart.yaml" appVersion)" || fail "staged chart appVersion cannot be read"
	[[ "$metadata_version" == "$version" && "$metadata_app_version" == "$version" ]] || fail "published chart metadata does not match release version"
	archive_digest="$(sha256sum "$archive" | awk '{print $1}')"
	[[ "$archive_digest" == "$(candidate_value "$candidate_dir" chart_archive_sha256)" ]] || fail "pulled Helm chart archive digest differs from the candidate"
	local rendered="$scratch/published-chart.yaml"
	helm template kubeseer "$chart_ref" --version "$version" --namespace kubeseer-system --kube-version 1.35.6 --include-crds --registry-config "$candidate_dir/helm-anonymous.json" "${flags[@]}" >"$rendered" || fail "published Helm OCI chart cannot be rendered"
	python3 - "$rendered" "ghcr.io/steeltanuki/kubeseer:$version" <<'PY'
import pathlib
import re
import sys
images = [match.group(1) for line in pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").splitlines()
          if (match := re.match(r"^\s*image:\s*[\"']?([^\"'\s]+)", line))]
expected = sys.argv[2]
if not images or any(image != expected for image in images):
    raise SystemExit(f"published chart default image must resolve to {expected}; got {images}")
if any(image.endswith(":latest") for image in images):
    raise SystemExit("published Helm chart renders a forbidden latest image tag")
PY
	local manifest_file="$scratch/chart-manifest.json"
	registry_probe chart steeltanuki/charts/kubeseer "$version" "$manifest_file"
	[[ "$(json_value "$manifest_file" state)" == present ]] || fail "published Helm OCI chart version disappeared after pull"
	local digest
	digest="$(json_value "$manifest_file" digest)"
	[[ -z "$expected_digest" || "$digest" == "$expected_digest" ]] || fail "published Helm OCI chart digest conflicts with the recorded release digest"
	printf '%s' "$digest"
}

release_sentinel() {
	local body="$1"
	python3 - "$body" <<'PY'
import pathlib, re, sys
matches = re.findall(r"^<!-- kubeseer-release-distribution ([^>]+) -->$", pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"), re.M)
print(matches[0] if len(matches) == 1 else "")
PY
}

write_release_body() {
	local destination="$1" tag="$2" version="$3" source_sha="$4" image_digest="$5" chart_digest="$6" archive_sha="$7" latest_image="$8" latest_chart="$9"
	local root notes chart_url image_ref k8s helm_min
	root="$(git rev-parse --show-toplevel)"
	notes="$root/docs/releases/$tag.md"
	readarray -t release_toolchain < <(python3 - "$root/hack/toolchain.mk" <<'PY'
import pathlib, re, sys
text = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
k8s = re.search(r"^KUBERNETES_COMPATIBILITY_VERSIONS\s*:=\s*(.*?)\s*$", text, re.M)
helm = re.search(r"^HELM_MIN_VERSION\s*\?=\s*(\S+)", text, re.M)
if not k8s or not helm:
    raise SystemExit("release toolchain lacks compatibility or Helm minimum declarations")
print(", ".join(k8s.group(1).split()))
print(helm.group(1))
PY
)
	k8s="${release_toolchain[0]:-}"
	helm_min="${release_toolchain[1]:-}"
	[[ -n "$k8s" && -n "$helm_min" && -f "$notes" ]] || fail "cannot derive supported versions or tagged release notes"
	chart_url="oci://ghcr.io/steeltanuki/charts/kubeseer"
	image_ref="ghcr.io/steeltanuki/kubeseer:$version"
	{
		printf '<!-- kubeseer-release-distribution version=%s source_sha=%s image_digest=%s chart_digest=%s chart_archive_sha256=%s latest_image=%s latest_chart=%s -->\n\n' \
			"$version" "$source_sha" "$image_digest" "$chart_digest" "$archive_sha" "$latest_image" "$latest_chart"
		printf '# Kubeseer %s\n\n' "$tag"
		printf '**Version:** %s  \n**Source tag:** %s  \n**Source commit:** %s  \n' "$version" "$tag" "$source_sha"
		printf '**Certified Kubernetes versions:** %s  \n**Minimum Helm version:** %s  \n**Controller platform:** linux/amd64\n\n' "$k8s" "$helm_min"
		printf '**Controller image:** %s  \n**Controller image digest:** %s  \n' "$image_ref" "$image_digest"
		printf '**Helm OCI chart:** %s (version %s)  \n**Helm OCI chart digest:** %s  \n**Chart archive SHA-256:** %s\n\n' "$chart_url" "$version" "$chart_digest" "$archive_sha"
		printf 'Install this official release:\n\n<pre><code>helm upgrade --install kubeseer %s --version %s --namespace kubeseer-system --create-namespace</code></pre>\n\n' "$chart_url" "$version"
		printf 'Review [installation and upgrade documentation](https://github.com/steeltanuki/kubeseer/blob/%s/docs/installation.md) and [CRD upgrade guidance](https://github.com/steeltanuki/kubeseer/blob/%s/docs/installation.md#policy-rbac-and-upgrades) before upgrading.\n\n' "$tag" "$tag"
		printf '## Release notes\n\n'
		cat "$notes"
	} >"$destination"
}

github_release_body_to_file() {
	local release_json="$1" destination="$2"
	python3 - "$release_json" "$destination" <<'PY'
import json, pathlib, sys
release = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
if release.get("state") == "absent":
    pathlib.Path(sys.argv[2]).write_text("", encoding="utf-8")
else:
    body = release.get("body")
    if not isinstance(body, str):
        raise SystemExit("GitHub Release has no text body")
    pathlib.Path(sys.argv[2]).write_text(body, encoding="utf-8")
PY
}

remote_inventory() {
	local tag="$1" version="$2" source_sha="$3" directory="$4"
	registry_probe image steeltanuki/kubeseer "$version" "$directory/image.json"
	registry_probe chart steeltanuki/charts/kubeseer "$version" "$directory/chart.json"
	registry_probe image steeltanuki/kubeseer latest "$directory/image-latest.json"
	registry_probe chart steeltanuki/charts/kubeseer latest "$directory/chart-latest.json"
	github_tag_identity "$tag" "$directory/github-tag.json"
	[[ "$(json_value "$directory/github-tag.json" sha)" == "$source_sha" ]] || fail "GitHub release tag resolves to a different source commit"
	github_release_identity "$tag" "$directory/github-release.json"
	[[ "$(json_value "$directory/github-release.json" state)" == absent || "$(json_value "$directory/github-release.json" tag_name)" == "$tag" ]] || fail "GitHub Release tag identity is inconsistent"
}

latest_baseline() {
	local kind="$1" repo="$2" output="$3" state
	registry_probe "$kind" "$repo" latest "$output"
	state="$(json_value "$output" state)"
	if [[ "$state" == present ]]; then json_value "$output" digest; else printf absent; fi
}

check_latest_unchanged() {
	local expected_image="$1" expected_chart="$2" directory="$3" actual_image actual_chart
	actual_image="$(latest_baseline image steeltanuki/kubeseer "$directory/image-latest-after.json")"
	actual_chart="$(latest_baseline chart steeltanuki/charts/kubeseer "$directory/chart-latest-after.json")"
	[[ "$actual_image" == "$expected_image" ]] || fail "release workflow created or changed the controller latest tag"
	[[ "$actual_chart" == "$expected_chart" ]] || fail "release workflow created or changed a latest Helm OCI artifact"
}

acquire_release_lock() {
	local version="$1" lock_root="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
	command -v flock >/dev/null 2>&1 || fail "flock is required to serialize same-version release publication"
	mkdir -p "$lock_root"
	exec 9>"$lock_root/kubeseer-release-distribution-$version.lock"
	flock 9 || fail "cannot acquire the per-version release publication lock"
}

prepare_registry_credentials() {
	local directory="$1" host
	host="$(registry_host)"
	printf '{"auths":{}}\n' >"$directory/podman-auth.json"
	printf '{"auths":{}}\n' >"$directory/helm-anonymous.json"
	printf '{"auths":{}}\n' >"$directory/helm-publish.json"
	if [[ "$host" == ghcr.io ]]; then
		[[ -n "${GITHUB_ACTOR:-}" && -n "${GITHUB_TOKEN:-}" ]] || fail "GITHUB_ACTOR and GITHUB_TOKEN are required to publish to GHCR"
		printf '%s' "$GITHUB_TOKEN" | podman login --authfile "$directory/podman-auth.json" --username "$GITHUB_ACTOR" --password-stdin ghcr.io >/dev/null || fail "GITHUB_TOKEN cannot authenticate to GHCR for package publication"
		printf '%s' "$GITHUB_TOKEN" | helm registry login ghcr.io --registry-config "$directory/helm-publish.json" --username "$GITHUB_ACTOR" --password-stdin >/dev/null || fail "GITHUB_TOKEN cannot authenticate Helm publication to GHCR"
	fi
	chmod 600 "$directory/podman-auth.json" "$directory/helm-publish.json"
}

push_candidate_image() {
	local directory="$1" destination="$2"
	local -a flags=(--format oci --authfile "$directory/podman-auth.json" --digestfile "$directory/pushed-image-digest.txt" --retry 1)
	if is_plain_http_registry; then flags+=(--tls-verify=false); fi
	staged_image_ref="$(candidate_value "$directory" image_candidate)"
	podman load --input "$directory/kubeseer-controller.oci.tar" >/dev/null || fail "cannot restore the staged production OCI image into Podman storage"
	podman image exists "$staged_image_ref" || fail "staged OCI image archive did not preserve its unique candidate reference"
	podman push "${flags[@]}" "$staged_image_ref" "docker://$destination" >/dev/null || fail "controller image publication failed"
}

publish_chart_archive() {
	local version="$1" directory="$2" destination
	destination="oci://$(registry_host)/steeltanuki/charts"
	local -a flags=(--registry-config "$directory/helm-publish.json")
	helm push "$directory/kubeseer-$version.tgz" "$destination" "${flags[@]}" >/dev/null || fail "Helm OCI chart publication failed"
}

validate_existing_release_body() {
	local release_json="$1" body_file="$2" tag="$3" version="$4" source_sha="$5" image_digest="$6" archive_sha="$7" latest_image="$8" latest_chart="$9"
	github_release_body_to_file "$release_json" "$body_file"
	local sentinel field value found_chart_digest=""
	sentinel="$(release_sentinel "$body_file")"
	[[ -n "$sentinel" ]] || fail "existing GitHub Release lacks release-distribution identity metadata"
	for field in version source_sha image_digest chart_digest chart_archive_sha256 latest_image latest_chart; do
		value="$(printf '%s\n' "$sentinel" | tr ' ' '\n' | sed -n "s/^$field=//p")"
		[[ -n "$value" ]] || fail "existing GitHub Release has incomplete $field metadata"
		case "$field" in
			version) [[ "$value" == "$version" ]] || fail "existing GitHub Release claims a different version" ;;
			source_sha) [[ "$value" == "$source_sha" ]] || fail "existing GitHub Release claims a different source commit" ;;
			image_digest) [[ "$value" == "$image_digest" ]] || fail "existing GitHub Release image digest conflicts with the immutable candidate" ;;
			chart_archive_sha256) [[ "$value" == "$archive_sha" ]] || fail "existing GitHub Release chart archive conflicts with the immutable candidate" ;;
			latest_image) [[ "$value" == "$latest_image" ]] || fail "controller latest changed since the existing GitHub Release" ;;
			latest_chart) [[ "$value" == "$latest_chart" ]] || fail "chart latest changed since the existing GitHub Release" ;;
			chart_digest) found_chart_digest="$value" ;;
		esac
	done
	[[ "$found_chart_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || fail "existing GitHub Release chart digest is malformed"
	[[ "$(json_value "$release_json" tag_name)" == "$tag" ]] || fail "existing GitHub Release is associated with another tag"
	[[ "$(json_value "$release_json" draft)" == false && "$(json_value "$release_json" prerelease)" == false ]] || fail "existing GitHub Release is not a published stable release"
	printf '%s' "$found_chart_digest"
}

publish_transaction() {
	local mode="$1" tag="$2" source_sha="$3" directory="$4"
	local version image_digest archive_sha image_latest chart_latest image_state chart_state release_state chart_digest image_destination
	version="$(stable_version "$tag")"
	validate_source "$tag" "$source_sha" >/dev/null
	validate_candidate "$directory" "$tag" "$source_sha" "$version"
	prepare_registry_credentials "$directory"
	image_digest="$(candidate_value "$directory" image_manifest_digest)"
	archive_sha="$(candidate_value "$directory" chart_archive_sha256)"
	image_destination="$(registry_host)/steeltanuki/kubeseer:$version"
	remote_inventory "$tag" "$version" "$source_sha" "$directory"
	image_latest="$(json_value "$directory/image-latest.json" state)"
	if [[ "$image_latest" == present ]]; then image_latest="$(json_value "$directory/image-latest.json" digest)"; else image_latest=absent; fi
	chart_latest="$(json_value "$directory/chart-latest.json" state)"
	if [[ "$chart_latest" == present ]]; then chart_latest="$(json_value "$directory/chart-latest.json" digest)"; else chart_latest=absent; fi
	image_state="$(json_value "$directory/image.json" state)"
	chart_state="$(json_value "$directory/chart.json" state)"
	release_state="$(json_value "$directory/github-release.json" state)"
	if [[ "$image_state" == present ]]; then assert_public_image "$directory/image.json" "$version" "$source_sha" "$image_digest"; fi
	chart_digest=""
	if [[ "$chart_state" == present ]]; then chart_digest="$(verify_public_chart "$version" "$directory" "$directory/chart-existing" "")"; fi
	if [[ "$release_state" != absent ]]; then
		local existing_chart_digest
		existing_chart_digest="$(validate_existing_release_body "$directory/github-release.json" "$directory/existing-release.md" "$tag" "$version" "$source_sha" "$image_digest" "$archive_sha" "$image_latest" "$chart_latest")"
		if [[ -n "$chart_digest" && "$existing_chart_digest" != "$chart_digest" ]]; then fail "existing GitHub Release chart digest does not match GHCR"; fi
		chart_digest="$existing_chart_digest"
		write_release_body "$directory/expected-release.md" "$tag" "$version" "$source_sha" "$image_digest" "$chart_digest" "$archive_sha" "$image_latest" "$chart_latest"
		cmp -s "$directory/existing-release.md" "$directory/expected-release.md" || fail "existing GitHub Release body conflicts with the immutable release candidate"
	fi
	if [[ "$image_state" == absent && "$mode" != publish-chart && "$mode" != publish-release ]]; then
		registry_probe image steeltanuki/kubeseer "$version" "$directory/image-immediate.json"
		if [[ "$(json_value "$directory/image-immediate.json" state)" == present ]]; then
			assert_public_image "$directory/image-immediate.json" "$version" "$source_sha" "$image_digest"
		else
			push_candidate_image "$directory" "$image_destination"
			registry_probe image steeltanuki/kubeseer "$version" "$directory/image-after-push.json"
			assert_public_image "$directory/image-after-push.json" "$version" "$source_sha" "$image_digest"
			[[ "$(cat "$directory/pushed-image-digest.txt")" == "$image_digest" ]] || fail "image push digest differs from the staged OCI manifest"
		fi
		image_state=present
	fi
	if [[ "$chart_state" == absent && "$mode" != publish-image && "$mode" != publish-release ]]; then
		registry_probe chart steeltanuki/charts/kubeseer "$version" "$directory/chart-immediate.json"
		if [[ "$(json_value "$directory/chart-immediate.json" state)" == present ]]; then
			chart_digest="$(verify_public_chart "$version" "$directory" "$directory/chart-raced" "$chart_digest")"
		else
			publish_chart_archive "$version" "$directory"
			chart_digest="$(verify_public_chart "$version" "$directory" "$directory/chart-after-push" "$chart_digest")"
		fi
		chart_state=present
	fi
	if [[ "$mode" == publish-image || "$mode" == publish-chart ]]; then
		printf 'RELEASE_DISTRIBUTION=%s STATUS=passed VERSION=%s SOURCE_SHA=%s\n' "$mode" "$version" "$source_sha"
		return
	fi
	[[ "$image_state" == present && "$chart_state" == present ]] || fail "GitHub Release cannot be created until both public OCI artifacts are verified"
	if [[ -z "$chart_digest" ]]; then chart_digest="$(verify_public_chart "$version" "$directory" "$directory/chart-verify" "")"; fi
	check_latest_unchanged "$image_latest" "$chart_latest" "$directory"
	write_release_body "$directory/expected-release.md" "$tag" "$version" "$source_sha" "$image_digest" "$chart_digest" "$archive_sha" "$image_latest" "$chart_latest"
	if [[ "$release_state" == absent ]]; then
		GH_TOKEN="$GITHUB_TOKEN" gh release create "$tag" --verify-tag --repo "$repository" --title "Kubeseer $tag" --notes-file "$directory/expected-release.md" --latest=false >/dev/null || fail "GitHub Release creation failed after GHCR publication; rerun the same protected tag to recover missing release metadata"
	fi
	github_release_identity "$tag" "$directory/github-release-after.json"
	[[ "$(json_value "$directory/github-release-after.json" state)" != absent ]] || fail "GitHub Release is not visible after creation"
	github_release_body_to_file "$directory/github-release-after.json" "$directory/published-release.md"
	cmp -s "$directory/expected-release.md" "$directory/published-release.md" || fail "published GitHub Release failed read-after-write identity verification"
	[[ "$(json_value "$directory/github-release-after.json" tag_name)" == "$tag" ]] || fail "published GitHub Release is associated with a different source tag"
	printf 'RELEASE_DISTRIBUTION=publish STATUS=passed VERSION=%s SOURCE_SHA=%s IMAGE_DIGEST=%s CHART_DIGEST=%s\n' "$version" "$source_sha" "$image_digest" "$chart_digest"
}

run_inventory() {
	local tag="$1" source_sha="$2" version scratch temp_root="${TMPDIR:-/tmp}"
	version="$(stable_version "$tag")"
	validate_source "$tag" "$source_sha" >/dev/null
	scratch="$(mktemp -d "$temp_root/kubeseer-release-inventory.XXXXXX")"
	remote_inventory "$tag" "$version" "$source_sha" "$scratch"
	python3 - "$scratch" "$version" "$source_sha" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
summary = {"version": sys.argv[2], "source_commit": sys.argv[3]}
for name in ("image", "chart", "image-latest", "chart-latest", "github-release"):
    data = json.loads((root / f"{name}.json").read_text(encoding="utf-8"))
    summary[name] = {key: data.get(key) for key in ("state", "digest", "tag_name") if key in data}
print(json.dumps(summary, sort_keys=True))
PY
	rm -rf "$scratch"
}

release_metadata_field() {
	local body="$1" key="$2" sentinel
	sentinel="$(release_sentinel "$body")"
	printf '%s\n' "$sentinel" | tr ' ' '\n' | sed -n "s/^$key=//p"
}

run_audit() {
	local tag="$1" source_sha="$2" version temp_root scratch image_state chart_state release_state image_digest chart_digest archive_sha latest_image latest_chart
	version="$(stable_version "$tag")"
	validate_source "$tag" "$source_sha" >/dev/null
	temp_root="${TMPDIR:-/tmp}"
	scratch="$(mktemp -d "$temp_root/kubeseer-release-audit.XXXXXX")"
	stage_output_dir="$scratch"
	stage_output_owned=true
	printf '{"auths":{}}\n' >"$scratch/helm-anonymous.json"
	remote_inventory "$tag" "$version" "$source_sha" "$scratch"
	image_state="$(json_value "$scratch/image.json" state)"
	chart_state="$(json_value "$scratch/chart.json" state)"
	release_state="$(json_value "$scratch/github-release.json" state)"
	[[ "$image_state" == present && "$chart_state" == present && "$release_state" != absent ]] || fail "release audit found one or more missing official artifacts"
	image_digest="$(json_value "$scratch/image.json" digest)"
	assert_public_image "$scratch/image.json" "$version" "$source_sha" "$image_digest"
	github_release_body_to_file "$scratch/github-release.json" "$scratch/release.md"
	[[ "$(json_value "$scratch/github-release.json" tag_name)" == "$tag" ]] || fail "GitHub Release does not identify the audited source tag"
	[[ "$(json_value "$scratch/github-release.json" draft)" == false && "$(json_value "$scratch/github-release.json" prerelease)" == false ]] || fail "GitHub Release is not published as a stable release"
	[[ "$(release_metadata_field "$scratch/release.md" version)" == "$version" ]] || fail "GitHub Release version metadata is inconsistent"
	[[ "$(release_metadata_field "$scratch/release.md" source_sha)" == "$source_sha" ]] || fail "GitHub Release source revision metadata is inconsistent"
	[[ "$(release_metadata_field "$scratch/release.md" image_digest)" == "$image_digest" ]] || fail "GitHub Release controller image digest is inconsistent"
	chart_digest="$(json_value "$scratch/chart.json" digest)"
	[[ "$(release_metadata_field "$scratch/release.md" chart_digest)" == "$chart_digest" ]] || fail "GitHub Release chart digest is inconsistent"
	archive_sha="$(release_metadata_field "$scratch/release.md" chart_archive_sha256)"
	[[ "$archive_sha" =~ ^[0-9a-f]{64}$ ]] || fail "GitHub Release chart archive checksum is malformed"
	latest_image="$(release_metadata_field "$scratch/release.md" latest_image)"
	latest_chart="$(release_metadata_field "$scratch/release.md" latest_chart)"
	[[ -n "$latest_image" && -n "$latest_chart" ]] || fail "GitHub Release latest-tag baseline metadata is missing"
	check_latest_unchanged "$latest_image" "$latest_chart" "$scratch"
	local chart_pull="$scratch/chart-audit"
	mkdir -p "$chart_pull"
	local -a helm_flags=()
	helm pull "oci://$(registry_host)/steeltanuki/charts/kubeseer" --version "$version" --destination "$chart_pull" --registry-config "$scratch/helm-anonymous.json" "${helm_flags[@]}" >/dev/null || fail "audited public chart cannot be pulled"
	local chart_archive="$chart_pull/kubeseer-$version.tgz" chart_metadata="$scratch/Chart.yaml"
	[[ -f "$chart_archive" ]] || fail "audited Helm pull produced no versioned archive"
	[[ "$(sha256sum "$chart_archive" | awk '{print $1}')" == "$archive_sha" ]] || fail "audited Helm chart archive content differs from the GitHub Release"
	helm show chart "$chart_archive" >"$chart_metadata" || fail "audited chart metadata cannot be read"
	cp "$chart_archive" "$scratch/kubeseer-$version.tgz"
	mkdir -p "$scratch/unpacked/kubeseer"
	cp "$chart_metadata" "$scratch/unpacked/kubeseer/Chart.yaml"
	python3 - "$scratch/candidate.json" "$archive_sha" <<'PY'
import json, pathlib, sys
pathlib.Path(sys.argv[1]).write_text(json.dumps({"chart_archive_sha256": sys.argv[2]}), encoding="utf-8")
PY
	local audited_chart_digest
	audited_chart_digest="$(verify_public_chart "$version" "$scratch" "$scratch/chart-pull-audit" "$chart_digest")"
	[[ "$audited_chart_digest" == "$chart_digest" ]] || fail "audited Helm chart manifest digest changed during pull"
	write_release_body "$scratch/expected-release.md" "$tag" "$version" "$source_sha" "$image_digest" "$chart_digest" "$archive_sha" "$latest_image" "$latest_chart"
	cmp -s "$scratch/release.md" "$scratch/expected-release.md" || fail "GitHub Release user-facing metadata differs from the audited artifacts"
	printf 'RELEASE_DISTRIBUTION=audit STATUS=passed VERSION=%s SOURCE_SHA=%s IMAGE_DIGEST=%s CHART_DIGEST=%s\n' "$version" "$source_sha" "$image_digest" "$chart_digest"
}

main() {
	local mode="${1:-}"
	[[ -n "$mode" ]] || usage
	shift
	local tag="" source_sha="" output_dir="" candidate_dir=""
	while (($#)); do
		case "$1" in
			--tag)
				(($# >= 2)) || usage
				tag="$2"
				shift 2
				;;
			--source-sha)
				(($# >= 2)) || usage
				source_sha="$2"
				shift 2
				;;
			--output-dir)
				(($# >= 2)) || usage
				output_dir="$2"
				shift 2
				;;
			--candidate-dir)
				(($# >= 2)) || usage
				candidate_dir="$2"
				shift 2
				;;
			*) usage ;;
		esac
	done
	[[ -n "$tag" && -n "$source_sha" ]] || usage

	case "$mode" in
		validate)
			local version
			version="$(stable_version "$tag")"
			validate_source "$tag" "$source_sha"
			printf 'RELEASE_DISTRIBUTION=source STATUS=passed VERSION=%s SOURCE_SHA=%s\n' "$version" "$source_sha"
			;;
		stage)
			stage_candidate "$tag" "$source_sha" "$output_dir"
			;;
		inventory)
			run_inventory "$tag" "$source_sha"
			;;
		audit)
			run_audit "$tag" "$source_sha"
			;;
		publish)
			require_official_publish_context "$tag" "$source_sha"
			acquire_release_lock "$(stable_version "$tag")"
			stage_candidate "$tag" "$source_sha" "$output_dir"
			publish_transaction publish "$tag" "$source_sha" "$stage_output_dir"
			;;
		publish-image|publish-chart|publish-release)
			require_official_publish_context "$tag" "$source_sha"
			[[ -n "$candidate_dir" ]] || usage
			acquire_release_lock "$(stable_version "$tag")"
			publish_transaction "$mode" "$tag" "$source_sha" "$candidate_dir"
			;;
		*) usage ;;
	 esac
}

main "$@"
