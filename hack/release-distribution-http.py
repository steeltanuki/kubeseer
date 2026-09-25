#!/usr/bin/env python3
# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

"""Small, fail-closed HTTP helpers for release-distribution.sh."""

from __future__ import annotations

import argparse
import base64
import gzip
import hashlib
import json
import os
import pathlib
import re
import tarfile
import urllib.error
import urllib.parse
import urllib.request


IMAGE_ACCEPT = ", ".join(
    (
        "application/vnd.oci.image.index.v1+json",
        "application/vnd.oci.image.manifest.v1+json",
        "application/vnd.docker.distribution.manifest.list.v2+json",
        "application/vnd.docker.distribution.manifest.v2+json",
    )
)
CHART_ACCEPT = ", ".join(
    (
        "application/vnd.oci.image.manifest.v1+json",
        "application/vnd.cncf.helm.config.v1+json",
    )
)


def fail(message: str) -> "NoReturn":
    raise SystemExit(message)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file_pointer, code, message, headers, new_url):
        return None


def http_request(
    url: str, *, headers: dict[str, str] | None = None, data: bytes | None = None,
    follow_redirects: bool = True,
):
    request_headers = {"User-Agent": "kubeseer-release-distribution/1"}
    if headers:
        request_headers.update(headers)
    method = "POST" if data is not None else "GET"
    request = urllib.request.Request(url, data=data, headers=request_headers, method=method)
    try:
        opener = urllib.request.urlopen if follow_redirects else urllib.request.build_opener(NoRedirect()).open
        response = opener(request, timeout=25)
        return response.status, response.headers, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.headers, error.read()
    except (urllib.error.URLError, TimeoutError) as error:
        fail(f"HTTP request failed for {url}: {error}")


def bearer_parameters(value: str) -> dict[str, str]:
    parameters: dict[str, str] = {}
    for match in re.finditer(r'([A-Za-z][A-Za-z0-9_-]*)="([^"\\]*(?:\\.[^"\\]*)*)"', value):
        parameters[match.group(1).lower()] = match.group(2).replace('\\"', '"')
    return parameters


def registry_get(base: str, repository: str, path: str, accept: str, *, authenticated: bool = False):
    url = f"{base.rstrip('/')}/v2/{repository}/{path.lstrip('/')}"
    headers = {"Accept": accept}
    status, response_headers, body = http_request(url, headers=headers)
    if status != 401:
        if authenticated and status == 404 and urllib.parse.urlsplit(base).hostname == "ghcr.io":
            fail("GHCR inventory returned HTTP 404 without an authenticated bearer challenge")
        return status, response_headers, body

    challenge = response_headers.get("WWW-Authenticate", "")
    if not challenge.lower().startswith("bearer "):
        fail(f"registry denied anonymous pull without a usable bearer challenge (HTTP {status})")
    parameters = bearer_parameters(challenge[len("Bearer ") :])
    realm = parameters.get("realm")
    if not realm:
        fail("registry bearer challenge has no token realm")
    registry_origin = urllib.parse.urlsplit(base)
    token_origin = urllib.parse.urlsplit(realm)
    if (token_origin.scheme, token_origin.netloc) != (registry_origin.scheme, registry_origin.netloc):
        fail("registry bearer token realm is outside the configured registry origin")
    query = {key: value for key, value in parameters.items() if key in ("service", "scope")}
    if authenticated:
        actor = os.environ.get("GITHUB_ACTOR")
        secret = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
        if not actor or not secret:
            fail("GITHUB_ACTOR and GITHUB_TOKEN or GH_TOKEN are required for authenticated registry inventory")
        query["scope"] = f"repository:{repository}:pull,push"
        credentials = base64.b64encode(f"{actor}:{secret}".encode("utf-8")).decode("ascii")
        token_headers = {"Authorization": f"Basic {credentials}"}
    else:
        token_headers = {}
    if "scope" not in query:
        query["scope"] = f"repository:{repository}:pull"
    token_url = realm + ("&" if "?" in realm else "?") + urllib.parse.urlencode(query)
    token_status, _, token_body = http_request(token_url, headers=token_headers, follow_redirects=False)
    if token_status != 200:
        access = "authenticated inventory" if authenticated else "anonymous pull"
        fail(f"registry {access} token request failed (HTTP {token_status})")
    try:
        token_payload = json.loads(token_body)
        token = token_payload.get("token") or token_payload.get("access_token")
    except (ValueError, AttributeError):
        fail("registry returned malformed pull-token data")
    if not isinstance(token, str) or not token:
        fail("registry returned an empty pull token")
    return http_request(url, headers={**headers, "Authorization": f"Bearer {token}"})


def expected_sha256(data: bytes, reported: str, what: str) -> str:
    computed = "sha256:" + hashlib.sha256(data).hexdigest()
    if reported and reported != computed:
        fail(f"{what} bytes do not match the registry digest header")
    return computed


def registry_manifest(args: argparse.Namespace) -> None:
    accept = IMAGE_ACCEPT if args.kind == "image" else CHART_ACCEPT
    status, headers, body = registry_get(
        args.base, args.repository, f"manifests/{args.reference}", accept,
        authenticated=args.authenticated,
    )
    if status == 404:
        print(json.dumps({"state": "absent"}, sort_keys=True))
        return
    if status != 200:
        fail(f"registry manifest query failed closed with HTTP {status}")
    try:
        manifest = json.loads(body)
    except ValueError:
        fail("registry returned a malformed OCI manifest")
    digest = expected_sha256(body, headers.get("Docker-Content-Digest", ""), "registry manifest")
    result: dict[str, object] = {"state": "present", "digest": digest, "manifest": manifest}
    if args.kind == "image":
        media_type = manifest.get("mediaType", "")
        if media_type in (
            "application/vnd.oci.image.index.v1+json",
            "application/vnd.docker.distribution.manifest.list.v2+json",
        ):
            fail("published controller image must be a single-platform linux/amd64 manifest")
        descriptor = (manifest.get("config") or {}).get("digest")
        if not isinstance(descriptor, str) or not descriptor.startswith("sha256:"):
            fail("published image manifest has no SHA-256 config descriptor")
        blob_status, blob_headers, config_body = registry_get(
            args.base, args.repository, f"blobs/{descriptor}", "application/octet-stream",
            authenticated=args.authenticated,
        )
        if blob_status != 200:
            fail(f"published image config blob is not retrievable (HTTP {blob_status})")
        expected_sha256(config_body, descriptor, "image config blob")
        try:
            config = json.loads(config_body)
        except ValueError:
            fail("published image config is malformed")
        labels = ((config.get("config") or {}).get("Labels") or {})
        result["labels"] = labels
        result["architecture"] = config.get("architecture")
        result["os"] = config.get("os")
    print(json.dumps(result, sort_keys=True))


def github_request(base: str, path: str, *, method: str = "GET", payload: dict | None = None):
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if not token:
        fail("GITHUB_TOKEN is required for authenticated GitHub API inventory")
    headers = {
        "Accept": "application/vnd.github+json",
        "Authorization": f"Bearer {token}",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    data = None
    if payload is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    url = base.rstrip("/") + "/" + path.lstrip("/")
    status, _, body = http_request(url, headers=headers, data=data)
    if status == 404 and method == "GET":
        return {"state": "absent"}
    if status < 200 or status >= 300:
        fail(f"GitHub API {method} {path} failed closed with HTTP {status}")
    try:
        return json.loads(body) if body else {}
    except ValueError:
        fail(f"GitHub API {method} {path} returned malformed JSON")


def peel_tag(base: str, repository: str, tag: str) -> dict[str, object]:
    path = f"repos/{repository}/git/ref/tags/{urllib.parse.quote(tag, safe='')}"
    result = github_request(base, path)
    if result.get("state") == "absent":
        fail(f"official Git tag {tag} is not present on GitHub")
    obj = result.get("object") or {}
    for _ in range(8):
        kind, sha = obj.get("type"), obj.get("sha")
        if not isinstance(sha, str) or not re.fullmatch(r"[0-9a-f]{40,64}", sha):
            fail("GitHub returned an invalid Git tag object identity")
        if kind == "commit":
            return {"sha": sha, "ref": result.get("ref")}
        if kind != "tag":
            fail(f"GitHub tag resolves to unexpected Git object type {kind!r}")
        obj = github_request(base, f"repos/{repository}/git/tags/{sha}").get("object") or {}
    fail("GitHub annotated tag nesting exceeds the supported limit")


def github_tag(args: argparse.Namespace) -> None:
    print(json.dumps(peel_tag(args.base, args.repository, args.tag), sort_keys=True))


def github_release(args: argparse.Namespace) -> None:
    path = f"repos/{args.repository}/releases/tags/{urllib.parse.quote(args.tag, safe='')}"
    print(json.dumps(github_request(args.base, path), sort_keys=True))


def archive_manifest(args: argparse.Namespace) -> None:
    path = pathlib.Path(args.archive)
    try:
        with tarfile.open(path, "r:*") as archive:
            index = json.load(archive.extractfile("index.json"))
            descriptors = index.get("manifests") or []
            if len(descriptors) != 1:
                fail("controller OCI archive must contain exactly one manifest")
            descriptor = descriptors[0]
            digest = descriptor.get("digest", "")
            if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
                fail("controller OCI archive manifest has an invalid digest")
            manifest_path = "blobs/sha256/" + digest.removeprefix("sha256:")
            manifest_bytes = archive.extractfile(manifest_path).read()
    except (OSError, tarfile.TarError, KeyError, TypeError, AttributeError, ValueError) as error:
        fail(f"cannot inspect controller OCI archive {path}: {error}")
    if expected_sha256(manifest_bytes, digest, "controller OCI archive manifest") != digest:
        fail("controller OCI archive manifest digest is inconsistent")
    print(json.dumps({"manifest_digest": digest}, sort_keys=True))


def normalize_chart(args: argparse.Namespace) -> None:
    """Keep Helm's files, but bind tar/gzip metadata to the source commit.

    Helm 3 stamps archive members with packaging time, ignoring source mtimes.
    A fresh runner must produce the same chart bytes for same-tag recovery.
    """
    path = pathlib.Path(args.archive)
    normalized = path.with_suffix(path.suffix + ".normalized")
    try:
        with tarfile.open(path, "r:gz") as source, normalized.open("wb") as output:
            with gzip.GzipFile(filename="", mode="wb", fileobj=output, mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as target:
                    seen = set()
                    for member in sorted(source.getmembers(), key=lambda item: item.name):
                        name = pathlib.PurePosixPath(member.name)
                        if (name.is_absolute() or ".." in name.parts or
                                not member.name.startswith("kubeseer/") or
                                not member.isfile() or member.name in seen):
                            fail(f"unsafe or duplicate Helm archive member: {member.name}")
                        seen.add(member.name)
                        header = tarfile.TarInfo(member.name)
                        header.size = member.size
                        header.mode = 0o644
                        header.mtime = args.epoch
                        target.addfile(header, source.extractfile(member))
        normalized.replace(path)
        # Helm's OCI pusher derives its created annotation from the archive's
        # filesystem mtime. Stabilize that as well as the bytes inside it.
        os.utime(path, (args.epoch, args.epoch))
    finally:
        normalized.unlink(missing_ok=True)


def main() -> None:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    registry = commands.add_parser("registry-manifest")
    registry.add_argument("--base", required=True)
    registry.add_argument("--repository", required=True)
    registry.add_argument("--reference", required=True)
    registry.add_argument("--kind", choices=("image", "chart"), required=True)
    registry.add_argument("--authenticated", action="store_true")
    registry.set_defaults(run=registry_manifest)
    tag = commands.add_parser("github-tag")
    tag.add_argument("--base", required=True)
    tag.add_argument("--repository", required=True)
    tag.add_argument("--tag", required=True)
    tag.set_defaults(run=github_tag)
    release = commands.add_parser("github-release")
    release.add_argument("--base", required=True)
    release.add_argument("--repository", required=True)
    release.add_argument("--tag", required=True)
    release.set_defaults(run=github_release)
    archive = commands.add_parser("archive-manifest")
    archive.add_argument("--archive", required=True)
    archive.set_defaults(run=archive_manifest)
    normalize = commands.add_parser("normalize-chart")
    normalize.add_argument("--archive", required=True)
    normalize.add_argument("--epoch", type=int, required=True)
    normalize.set_defaults(run=normalize_chart)
    args = parser.parse_args()
    args.run(args)


if __name__ == "__main__":
    main()
