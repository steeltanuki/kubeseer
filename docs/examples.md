# Examples

Kubeseer ships seven runnable scenarios under [`examples`](../examples/).
They use the same discovery, policy, authorization, selection, evaluation, and
status paths as normal resources. The ordered
[`examples/catalog.txt`](../examples/catalog.txt) is the canonical catalog for
application, verification, documentation, and cleanup.

## Run the catalog

Bring up the supported local environment and run every example:

```sh
make local-check
make local-up
make local-examples
make local-verify
```

Run one constrained action for one catalog name:

```sh
make local-example EXAMPLE=builtin-resource ACTION=apply
make local-example EXAMPLE=builtin-resource ACTION=inspect
make local-example EXAMPLE=builtin-resource ACTION=verify
make local-example EXAMPLE=builtin-resource ACTION=down
```

Valid actions are `apply`, `inspect`, `verify`, and `down`. The helper always
uses the owned `kind-kubeseer-local` context and rejects names outside the
catalog. See [Local development](local-development.md) for prerequisites and
state ownership.

## Catalog

| Example | Capability | Expected public outcome |
| --- | --- | --- |
| [`builtin-resource`](../examples/builtin-resource/) | Exact Deployment selection and provenance | `Ready=True`, one Deployment, integer `replicas=1` |
| [`typed-extraction`](../examples/typed-extraction/) | Native scalar extraction and integer conversion | Successful integer replica value |
| [`value-operator`](../examples/value-operator/) | Typed `gte` predicate | Non-empty result whose replica value is at least 1 |
| [`cross-namespace-aggregation`](../examples/cross-namespace-aggregation/) | Authorized selection in two namespaces and `sum` | One deterministic aggregate with both namespaces contributing |
| [`custom-resource`](../examples/custom-resource/) | Discovery and selection of a structural `Widget` CRD | Typed integer `size` with Widget provenance |
| [`authorization-denial`](../examples/authorization-denial/) | Runtime policy revalidation after narrowing authority | Exact public reason `AuthorizationDenied` |
| [`partial-degradation`](../examples/partial-degradation/) | Isolation of an unavailable Custom Resource source | `Degraded=True`, successful Deployment sibling, unavailable-source error |

## What each directory contains

Every example includes:

- an Apache-2.0 README describing the intent and expected result;
- workload and namespace fixtures;
- a `Kubeseer` manifest labeled with `kubeseer.io/example`;
- deterministic application and reverse cleanup ordering;
- public-boundary assertions used by `ACTION=verify`.

Custom Resource examples establish the fixture CRD before its objects and
delete the objects before the CRD. Cross-namespace examples configure both the
logical installation policy and manager RBAC through the local profile.
Negative scenarios deliberately manipulate their fixture or policy only after
creation so they exercise runtime drift handling rather than merely admission
rejection.

## Inspect results

`ACTION=inspect` is the preferred path. For manual public inspection, use the
owned kubeconfig reported by `make local-status`:

```sh
STATE="${KUBESEER_LOCAL_STATE_DIR:-$HOME/.local/state/kubeseer/local}"
kubectl --kubeconfig "$STATE/kubeconfig" \
  --context kind-kubeseer-local \
  --namespace kubeseer-example-builtin \
  get kubeseer builtin -o yaml
```

Read `observedGeneration`, conditions, summary, source state, provenance,
typed fields, aggregates, and `resultHash`. Do not infer success from object
existence alone.

## Cleanup

Remove all example-owned resources while retaining the local cluster and Helm
release:

```sh
make local-examples-down
```

Then remove only the project-owned cluster:

```sh
make local-down
```

The workflow does not prune global Podman state, touch another kubeconfig, or
run the destructive CRD purge client.

## Adapt an example

Copy a manifest outside the canonical catalog and change one dimension at a
time:

1. select an API type already allowed by installation policy and manager RBAC;
2. start in the `Kubeseer` namespace before requesting multiple namespaces;
3. add a narrow selector;
4. add one explicit typed field;
5. inspect absence, null, error, and value outcomes;
6. add operators, then aggregations;
7. review where the result is stored and who can read it.

The complete declaration language is in [API reference](api-reference.md).
Do not add ad hoc entries to `catalog.txt` without also adding fixtures,
documentation, verification, cleanup, and the relevant approved specification
coverage.
