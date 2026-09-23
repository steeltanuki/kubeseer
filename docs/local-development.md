# Local development environment

This guide describes the persistent contributor environment implemented by the
`local-*` Make targets. It is certified only on Linux/amd64 with rootless
Podman and kind. macOS, Windows, WSL, Podman machine, Docker, minikube, k3d,
and non-amd64 hosts are uncertified and are rejected by `local-check`.

The [README](../README.md) is the documentation entry point. Use
[Examples](examples.md) for the scenario catalog,
[Development and verification](development.md) for test layers, and
[Troubleshooting](troubleshooting.md#local-environment-failure) for recovery.

## Fixed identities and versions

The environment owns exactly kind cluster `kubeseer-local`, kubeconfig context
`kind-kubeseer-local`, Helm release `kubeseer`, namespace `kubeseer-system`,
and policy singleton `installation-access-ceiling`. Mutable state is outside
the worktree at `${XDG_STATE_HOME:-$HOME/.local/state}/kubeseer/local`; image,
Helm, module, and pinned-manifest caches are at
`${XDG_CACHE_HOME:-$HOME/.cache}/kubeseer/local`. CI may set the absolute
`KUBESEER_LOCAL_STATE_DIR` and `KUBESEER_LOCAL_CACHE_DIR` overrides. Both roots
must be private, normalized, and outside this repository.

The compatibility matrix, kind node-image mapping, cert-manager pin, and tool
minimums are declared in [`hack/toolchain.mk`](../hack/toolchain.mk):

| Tool/input | Declared value or minimum |
| --- | --- |
| Kubernetes | 1.35.6 or 1.36.2 |
| kind node images | `kindest/node:v1.35.5`, `kindest/node:v1.36.1` |
| cert-manager | v1.18.2 |
| Go | 1.26 |
| Git | 2.30 |
| GNU Make | 4.3 |
| Podman | 4.9 |
| kind | 0.23 |
| kubectl | 1.27 |
| Helm | 3.12 |
| curl | 7.81 |

## First run and lifecycle

First-time setup needs network access for the pinned Go modules, cert-manager
manifest, kind node image, and any workload images used by an example. The
Kubeseer image itself is built by Podman and imported into every kind node; no
Kubeseer registry push or external registry credential is used. The supported
profile uses rootless Podman, which must be operational for the current user,
and kind must select it explicitly. Existing or cloud clusters are never
adopted.

Run the complete workflow in order:

```bash
make local-check
make local-up
make local-status
make local-examples
make local-verify
make local-diagnostics
make local-examples-down
make local-down                 # deletes only kind cluster kubeseer-local
```

Every successful phase prints one `LOCAL_ENVIRONMENT=<phase>
STATUS=passed` marker. Invalid arguments, unsupported hosts, version
mismatches, ownership conflicts, readiness timeouts, and failed deletion exit
non-zero with the earliest stable reason. Re-running `local-up` or
`local-examples` converges the same owned objects; it does not create a second
cluster, release, or policy.

### Recovering after host shutdown

Rootless Podman may leave the owned control-plane container exited after a host
shutdown. The next explicit `make local-up` checks its saved ownership metadata,
kind node, container labels, and kubeconfig API port. If the control-plane node
is stopped, `local-up` starts it and waits for the API before continuing the usual
image, package, and readiness checks. The API wait defaults to 120 seconds;
override it with `KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS` using an integer from 1
through 600, for example `KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS=240 make local-up`.

If Podman cannot start the node, the API wait expires, or identity checks
conflict, the command reports the failure and retains the owned metadata and
kubeconfig for diagnosis and retry. An already-running node with an unavailable
API is left running for investigation. `make local-check` and `make
local-status` remain read-only and never start an exited node. Recovery occurs
only when a contributor explicitly runs `make local-up`; host boot and Podman
restart settings are unchanged.

`local-status` is read-only and reports the exact state path, kubeconfig,
context, cluster, Kubernetes version, source identity, package, manager,
policy, and example readiness. For direct public inspection, always pass the
owned credentials explicitly:

```bash
STATE="${KUBESEER_LOCAL_STATE_DIR:-$HOME/.local/state/kubeseer/local}"
kubectl --kubeconfig "$STATE/kubeconfig" --context kind-kubeseer-local \
  -n kubeseer-system get kubeseer -o json
kubectl --kubeconfig "$STATE/kubeconfig" --context kind-kubeseer-local \
  -n kubeseer-system get events
kubectl --kubeconfig "$STATE/kubeconfig" --context kind-kubeseer-local \
  -n kubeseer-system get endpointslice \
  --selector kubernetes.io/service-name=kubeseer-webhook
kubectl --kubeconfig "$STATE/kubeconfig" --context kind-kubeseer-local \
  -n kubeseer-system logs deployment/kubeseer --all-containers --tail=200
```

The EndpointSlice command lists all ready and non-ready backends associated
with the webhook Service without relying on deprecated core `v1 Endpoints`.

The manager readiness endpoint is `/readyz`; the metrics endpoint is
`/metrics`. Conditions, Events, and status summaries are public observations.
`make local-diagnostics` writes a private, bounded, allowlisted projection to
an external run directory and reports its exact path. It never includes
kubeconfig bytes, tokens, Secret bodies, extracted values, result bodies,
selector operands, or Event messages.

If creation fails before ownership promotion, only a partial
`kubeseer-local` target is removed. If an owned upgrade or readiness check
fails, the cluster, metadata, kubeconfig, and prior Helm revision are retained
for diagnosis. If deletion fails, retry `make local-down`; the exact cluster
and metadata paths are reported. Caches and local image archives are retained
by design. No recovery command runs Podman prune, removes networks or volumes,
or invokes the destructive CRD purge client against an external cluster.

## Bundled examples

The ordered catalog in [`examples/catalog.txt`](../examples/catalog.txt) is the
single source for application, verification, documentation, and cleanup. Each
example has an Apache-2.0 README, workload, Kubeseer manifest, stable
`kubeseer.io/example` identity, expected public outcome, and constrained
commands:

```bash
make local-example EXAMPLE=<catalog-name> ACTION=apply
make local-example EXAMPLE=<catalog-name> ACTION=inspect
make local-example EXAMPLE=<catalog-name> ACTION=verify
make local-example EXAMPLE=<catalog-name> ACTION=down
```

The seven entries are:

- `builtin-resource`: Deployment selection and provenance.
- `typed-extraction`: native scalar value preserved as an integer.
- `value-operator`: supported `gte` predicate over a typed value.
- `cross-namespace-aggregation`: deterministic contributors from two namespaces.
- `custom-resource`: structural `Widget` CRD before Widget objects, and reverse cleanup.
- `authorization-denial`: exact logical `AuthorizationDenied` outside the active policy.
- `partial-degradation`: successful Deployment sibling plus an unavailable Widget source and `Degraded=True`.

`make local-examples` applies all seven in catalog order and is idempotent.
`make local-examples-down` removes only catalog-labeled namespaces/resources,
deleting fixture Custom Resources before their CRD; it preserves the Helm
release and access policy.

## Persistent exploration versus certification

`kubeseer-local` is a persistent development aid. `make e2e` remains the
authoritative full-product certification path: it creates a separate
run-unique kind cluster, kubeconfig, image tag, diagnostics directory, and
cleanup boundary. Local verification is not a replacement, alias, or shortcut
for E2E certification. Unsupported hosts, external clusters/providers,
external registries, `externalSecret` local mode, and global cleanup are out of
scope for this feature.

`make test-local-cluster-resume` runs a separate genuine recovery proof on
Linux/amd64 with working rootless Podman and kind. It creates a run-unique
cluster and workload, stops only that proof's verified control-plane container,
checks that the same container and workload return, then removes that exact
cluster. It does not use a fake-tool fallback as genuine evidence.
