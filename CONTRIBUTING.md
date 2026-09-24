# Contributing to Kubeseer

Contributions are welcome through issues and pull requests. This guide covers
bug reports, proposals, code, documentation, and examples. For the development
toolchain and test commands, see [Development and verification](docs/development.md).

## Maintainer and decisions

Alessandro Rontani is currently the sole maintainer. He triages issues,
reviews and merges pull requests, maintains the project's Walden records, and
publishes releases. Anyone may offer feedback or review, but acceptance and
merging require the maintainer's decision; there is no requirement for a second
maintainer approval. Please allow time for review.

## Walden is optional for contributors

You may use [Walden](https://github.com/andrearaponi/walden) and include draft
requirements, design, or tasks with your proposal. The maintainer reviews them
through the project's approval process; a draft is not automatically approved.

You may also open an ordinary issue or pull request without using Walden. The
maintainer decides whether a new or updated specification is needed and
prepares or revises it when needed. For changes governed by Walden, the
maintainer records current verification evidence before merging. An existing
pull request can inform a specification written later. The maintainer may
request changes to align the code with the approved contract.
Approvals and evidence record the actual review and verification, not an
earlier date.

Documentation changes and fixes that preserve an approved contract do not by
themselves require a new feature specification. Contributors are never
required to install Walden or produce its records.

## Before you start

Search the existing [issues](https://github.com/steeltanuki/kubeseer/issues)
and [pull requests](https://github.com/steeltanuki/kubeseer/pulls) before
opening a new one. Small, focused fixes and documentation improvements can go
straight to a pull request. For a substantial change to behavior, the public
API, packaging, or architecture, open an issue first to discuss the problem and
proposed approach with the maintainer.

When reporting a bug, include:

- the Kubeseer version or commit and relevant Kubernetes, Helm, or local
  environment versions;
- steps to reproduce it, the expected result, and the actual result;
- a minimal manifest or example and relevant logs, with credentials and other
  sensitive data removed.

For a feature proposal, describe the use case, expected behavior, and any
compatibility or security impact. For a suspected vulnerability, use GitHub's
private vulnerability reporting if it is available for the repository. If it
is unavailable, open an issue asking for a private contact channel without
publishing the vulnerability details.

## Make a change

1. Fork the repository and create a focused branch from `develop`.
2. Follow the code conventions in [Development and verification](docs/development.md).
   Keep unrelated changes out of the pull request.
3. Format Go code with `gofmt`. If you change API types or manifests, run the
   relevant generation commands and include the generated files. Do not edit
   generated CRDs or deep-copy files by hand.
4. Add or update appropriate tests when changing behavior. Run the relevant
   checks described below before opening the pull request.

## Checks

For code changes, start with the relevant test layer and repository verification:

```sh
make test
make verify
```

Run additional checks from [Development and verification](docs/development.md)
when your change affects the Kubernetes API, packaging, compatibility, or
full-cluster behavior. The pull request CI runs `make test` and `make verify`.
If a check cannot be run locally, say so in the pull request and include the
reason. For documentation-only changes, check local links and whitespace.

## Open a pull request

Target `develop` and keep the pull request focused. In its description,
explain why the change is needed, summarize what changed, link the related
issue when there is one, and list the checks you ran. Call out changes to the
public API, compatibility, security boundaries, generated artifacts, or
documentation. Address review feedback in the same pull request.

The maintainer decides whether and when to merge after reviewing the change
and its applicable checks. Release tags and publication are maintainer tasks.

## Conduct and licensing

Be respectful and constructive in issues, reviews, and pull requests. Discuss
ideas and code without personal attacks, harassment, or spam.

Kubeseer is licensed under [Apache License 2.0](LICENSE). Submit only work you
have the right to license under its contribution terms, preserve existing
license and attribution notices, and identify any third-party material in the
pull request.
