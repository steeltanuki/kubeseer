# Development and verification

Kubeseer is a Go/controller-runtime project with generated Kubernetes APIs,
layered integration tests, a packaged kind E2E system, and Walden-controlled
feature evidence. This guide describes the repository's current executable
workflow.

## Toolchain

Pins and minimum versions live in [`hack/toolchain.mk`](../hack/toolchain.mk).
The current baseline is:

| Tool or input | Version |
| --- | --- |
| Go | 1.26 minimum |
| controller-gen | v0.20.1 |
| setup-envtest | v0.24.1 |
| Kubernetes API tests | 1.35.6 and 1.36.2 |
| kind node images | v1.35.5 and v1.36.1 |
| cert-manager | v1.18.2 |
| GNU Make | 4.3 minimum |
| Podman | 4.9 minimum |
| kind | 0.23 minimum |
| kubectl | 1.27 minimum |
| Helm | 3.12 minimum |

Use the declared variables instead of duplicating version strings in scripts.
The certified local and E2E provider is kind on rootless Podman.

## Repository map

| Path | Purpose |
| --- | --- |
| `api/v1alpha1` | Public API types and generated deep-copy code |
| `cmd/kubeseer` | Manager binary entry point |
| `cmd/kubeseer-local` | Private helper used by the constrained local workflow |
| `cmd/kubeseer-purge` | Separately built, confirmed destructive purge client |
| `internal/*` | Discovery, policy, authorization, selection, evaluation, status, admission, and runtime packages |
| `config/crd/bases` | Generated canonical CRDs |
| `charts/kubeseer` | Canonical Helm package and values schema |
| `examples` | Runnable public-boundary scenarios |
| `test/integration` | Cross-module in-process scenarios |
| `test/envtest` | Real Kubernetes API machinery through envtest |
| `test/e2e` | Public-boundary full-cluster certification suite |
| `hack` | Toolchain, verification, package, E2E, and local-environment scripts |
| `.walden/specs` | Approved feature requirements, designs, task plans, and task state |
| `.walden/evidence` | Verification evidence bound to spec and code identity |

See [Concepts and architecture](concepts-and-architecture.md#repository-architecture)
for runtime package responsibilities.

## Build and generation

```sh
make build
make generate
make manifests
```

`make generate` regenerates Go object code. `make manifests` regenerates the
CRDs from API markers. Generated artifacts are committed and must remain
reproducible with the pinned controller-tools version.

After changing public API types:

1. update the approved specification before implementation;
2. change types and validation markers;
3. run `make generate` and `make manifests`;
4. inspect the generated item schemas, defaults, validations, scope, and
   storage version rather than trusting generator success alone;
5. run API, compatibility, package, and E2E checks appropriate to the change.

Do not hand-edit generated CRDs or deep-copy files.

## Test layers

Kubeseer treats the lowest genuine collaboration boundary as the preferred
proof layer. Package-local unit tests may exist, but required behaviour is
proved through cross-module, envtest, or E2E scenarios.

### Default and cross-module integration

```sh
make test
make test-integration
```

`make test` probes for the cross-module `TestModuleIntegration` suite. If it is
available, it runs that layer; the fallback is the Kubernetes API suite. Use
`make test-integration` when the cross-module layer is explicitly required.

### Kubernetes API and compatibility

```sh
make test-api
make test-compatibility
```

`make test-api` obtains pinned envtest binaries unless `KUBEBUILDER_ASSETS` is
already set, then runs API schema, discovery, selection, reconciliation, and
admission scenarios against a real local API server. `make test-compatibility`
runs the API suite across both supported Kubernetes versions.

Use writable caches in restricted or ephemeral environments:

```sh
GOCACHE=/tmp/kubeseer-go-build \
GOMODCACHE=/tmp/kubeseer-go-mod \
  make test-api
```

The suite never uses an ambient kubeconfig or cloud credential.

### Static repository verification

```sh
make verify
make verify-package
make verify-local-environment
```

`make verify` checks generated drift, test-layer policy, admission,
observability, performance/limit boundaries, E2E structure, package structure,
and the local-environment contract. It does not replace the runtime E2E run.

`make verify-package` regenerates and compares package artifacts, validates
image and security rules, and renders both certificate modes. Run
`make test-package-compatibility` for the offline package matrix; an optional
live matrix requires explicit kubeconfig and context variables documented in
[Installation](installation.md#verification-matrix).

### Full-cluster E2E

```sh
make e2e
```

The E2E harness is the authoritative whole-product certification. It creates a
run-unique kind cluster, kubeconfig, image tag, diagnostic directory, and
cleanup boundary; builds the production image; installs the canonical Helm
package; and asserts public CRD, status, Event, metrics, authorization,
degradation, limit, reconciliation, and lifecycle outcomes.

It never adopts an existing cluster and never relies on ambient kubeconfig or
registry credentials. Failure diagnostics are bounded and sanitized. This is
separate from the persistent environment in
[Local development](local-development.md).

## Persistent local workflow

For iterative exploration:

```sh
make local-check
make local-up
make local-examples
make local-verify
make local-diagnostics
make local-examples-down
make local-down
```

Only the fixed `kubeseer-local` cluster is owned by these commands. Never use
global Podman cleanup as a project recovery step. See the
[local development guide](local-development.md) for state paths and supported
hosts.

## Walden workflow

The project constitution is `.walden/constitution.md`. Each feature under
`.walden/specs/<feature>/` passes separately reviewed gates:

1. EARS requirements;
2. architecture and detailed design;
3. leaf implementation tasks with requirement/design traceability and proof;
4. execution and verification evidence.

Approval of one phase does not authorize the next. Implement only approved
tasks, preserve task boundaries, and update checkbox state through Walden's
task workflow. If an approved upstream document changes, reconcile and
reapprove dependent artifacts before execution.

Useful read-only checks are:

```sh
walden validate --all --json
walden status <feature>
walden evidence status <feature>
```

Verification evidence must prove the behaviour at the declared boundary, not
only that matching source text exists. Evidence is identity-bound: changes to
proof targets can make an otherwise passing feature stale and require an
explicitly reviewed proof update and re-verification.

Do not edit approved requirements, design, or tasks merely to make an
implementation fit. Stop at the relevant gate and reconcile the specification.

## Change checklist

Before handing off a change:

1. keep the diff limited to the approved or requested scope;
2. preserve unrelated worktree changes;
3. run formatting and generation appropriate to changed files;
4. run the narrowest relevant higher-layer suite, then broader checks in
   proportion to risk;
5. run `make verify` for repository-boundary changes;
6. run `walden validate --all --json` and current task proofs for Walden work;
7. run `git diff --check` and inspect the final diff;
8. document any environment limitation without representing an unrun check as
   passing;
9. use Apache-2.0 notices where source or generated-file headers are customary.

Release publication additionally requires package compatibility and isolated
E2E certification for the intended commit.
