# Troubleshooting

Start with the current generation, public conditions, and bounded operator
signals. Do not begin by dumping all cluster objects or Secret contents.

```sh
kubectl --context my-cluster -n applications get kubeseer workload-view -o yaml
kubectl --context my-cluster -n applications get events \
  --field-selector involvedObject.kind=Kubeseer,involvedObject.name=workload-view
kubectl --context my-cluster -n kubeseer-system logs \
  deployment/kubeseer --all-containers --tail=200
```

The [operations guide](operations.md#diagnostic-order) gives the general
diagnostic sequence. The cases below map public symptoms to focused checks.

## The Deployment is not ready

Check rollout, Pod status, and probes:

```sh
kubectl --context my-cluster -n kubeseer-system get deployment,pods
kubectl --context my-cluster -n kubeseer-system describe deployment kubeseer
kubectl --context my-cluster -n kubeseer-system logs \
  deployment/kubeseer --all-containers --tail=200
```

Common causes are an unreachable image, invalid manager arguments, or invalid
webhook TLS. If Pods run but readiness fails, inspect certificate resource
conditions and only Secret metadata/key names:

```sh
kubectl --context my-cluster -n kubeseer-system get certificate,issuer
kubectl --context my-cluster -n kubeseer-system get secret
```

Do not print `tls.key` or other Secret values. In `externalSecret` mode,
confirm that the key matches the serving certificate, the certificate is
current, its chain matches the configured public CA, and it contains all four
required webhook Service DNS names. Follow the staged CA rollover in the
[installation guide](installation.md#fallback-externalsecret).

## Admission webhook failures

If `kubectl apply` reports a connection, TLS, or webhook timeout error:

1. confirm the Deployment is ready;
2. confirm the `kubeseer-webhook` Service has endpoints;
3. inspect the `ValidatingWebhookConfiguration` Service namespace/name and CA
   bundle presence;
4. inspect certificate readiness and manager logs;
5. confirm network policy allows the API server to reach port 9443 through the
   webhook Service.

If admission returns a normal denial, read its sanitized field path and reason.
Typical causes are unsupported JSONPath, duplicate IDs, wrong operator arity,
type incompatibility, unresolved API type, a cluster/namespaced scope mismatch,
an installation-policy violation, or an admission budget.

Do not bypass the webhook to force an object into storage. Correct the
declaration or administrative policy; runtime validation would still fail
closed.

## `PolicyMissing` or `PolicyInvalid`

The only active policy is the cluster-scoped object named exactly
`installation-access-ceiling`:

```sh
kubectl --context my-cluster get kubeseeraccesspolicy \
  installation-access-ceiling -o yaml
```

In managed mode, review Helm release status and values, then reconcile the
release. In external mode, the administrator must create or repair the policy.
Check namespace mode, include/exclude sets, exact API group/Kind rules, system
namespace semantics, and cluster-scope opt-in.

A missing or invalid policy is intentionally not replaced by broad manager
RBAC.

## `AuthorizationDenied`

The source resolves correctly but exceeds the logical policy. Compare:

- requested `resource.apiVersion` and `kind`;
- resolved namespaced versus cluster-scoped type;
- every requested namespace;
- policy namespace mode, includes, excludes, and system namespace set;
- exact policy API group/Kind rules;
- `allowClusterScoped` for cluster resources.

Either narrow the `Kubeseer` or deliberately expand the administrator-owned
policy after reviewing the data destination. Do not grant broader RBAC as a
workaround; it cannot change this decision.

## `ReadForbidden`

Logical policy allowed the target, but the API server denied the manager
ServiceAccount. Resolve the actual ServiceAccount from the Deployment and
check its exact permission:

```sh
kubectl --context my-cluster -n kubeseer-system get deployment kubeseer \
  -o jsonpath='{.spec.template.spec.serviceAccountName}{"\n"}'
kubectl --context my-cluster auth can-i list deployments.apps \
  --as=system:serviceaccount:kubeseer-system:SERVICE_ACCOUNT \
  --namespace applications
kubectl --context my-cluster auth can-i watch deployments.apps \
  --as=system:serviceaccount:kubeseer-system:SERVICE_ACCOUNT \
  --namespace applications
```

Add the smallest matching `rbac.observed` rule through reviewed chart values.
Policy and RBAC should name the same intended observation scope, using Kind in
policy and plural API resource in RBAC.

## `ResolutionFailed` or `ResolutionUnavailable`

Confirm the requested API resource exists and discovery is healthy:

```sh
kubectl --context my-cluster api-resources --api-group=apps
kubectl --context my-cluster get crd
```

For a Custom Resource, wait until its CRD is Established before creating the
`Kubeseer`. Check the exact `apiVersion`, Kind capitalization, served version,
and scope. `ResolutionUnavailable` can indicate a transient discovery/API
failure; if it persists, inspect API server availability and bounded manager
logs.

## `InvalidConfiguration`

Inspect the condition message and the affected source/field. Common runtime
causes after an object was admitted are:

- API discovery or policy drift made the stored declaration invalid;
- a field omitted its explicit `type`;
- JSONPath uses unsupported full-kubectl syntax;
- an operator has the wrong operand branch or arity;
- an operator or aggregate does not support the field type;
- an aggregation references a missing field;
- average options are used on another function.

Use [API reference](api-reference.md) as the supported language contract.

## `EvaluationDegraded`

`Degraded=True` can be expected and useful. Inspect
`status.result.sources[*]` and each source's `resources`, `fieldErrors`,
`aggregates`, `failures`, and sanitized `error`.

A successful sibling result remains valid; do not assume the entire status is
unusable. Correct the failing resource shape, conversion, operator,
aggregation contribution, or unavailable API type. The result hash will
change when the semantic outcome changes.

## `ResultLimitExceeded`

Kubeseer reached a configured resource, byte, status, group, contribution,
value, or provenance limit. It does not truncate.

Prefer narrowing the declaration:

- restrict namespaces;
- add exact name, label, or field selectors;
- extract fewer or smaller fields;
- avoid publishing large objects and lists;
- reduce group cardinality;
- disable provenance where it is unnecessary.

Increase an administrator limit only after measuring memory, API traffic, and
the resulting status/data exposure. Defaults and tuning considerations are in
[Configuration](configuration.md#runtime-limits).

## Status does not change after an observed object changes

First check whether the semantic result should change. Kubeseer deliberately
suppresses no-op status writes.

If the result should change:

1. compare generation and observed generation;
2. check conditions for policy or discovery changes;
3. inspect `kubeseer_source_watch_restarts_total` and structured watch events;
4. confirm the manager can `list` and `watch` the exact resource;
5. wait for the bounded safety reconciliation interval;
6. inspect source routing/retry logs without dumping values.

A policy restriction can remove formerly authorized results. Stale in-flight
work is intentionally discarded rather than published.

## Local environment failure

Run the preflight and status entry points:

```sh
make local-check
make local-status
```

The supported profile is Linux/amd64, rootless Podman, and kind. Docker,
minikube, k3d, Podman machine, WSL, and non-amd64 hosts are rejected rather
than treated as certified.

State lives outside the repository under the path reported by `local-status`.
On failure, run `make local-diagnostics`; it writes a private, bounded bundle
and prints its location. Retry `make local-down` only for the fixed owned
`kubeseer-local` cluster. Never use Podman prune or broad filesystem deletion
as recovery. See [Local development](local-development.md).

## Envtest or Go dependency download failure

Use writable build/module caches and verify network access to the pinned tool
and module sources:

```sh
GOCACHE=/tmp/kubeseer-go-build \
GOMODCACHE=/tmp/kubeseer-go-mod \
  make test-api
```

If the environment is intentionally offline, pre-provision the pinned envtest
assets and set `KUBEBUILDER_ASSETS` to their directory. Do not report a suite
as passing if dependency or binary acquisition prevented it from running.

## Helm upgrade or rollback failure

Check `helm history`, release status, hook Jobs, Deployment conditions, and
webhook readiness. Helm does not upgrade CRDs under `crds/`; run the explicit
compatibility check and apply path before a CRD-bearing upgrade or rollback.

Do not cross a storage-version or scope boundary with rollback. A failed
rolling update is left visible for diagnosis; it is not destructively
auto-rolled back. Follow the canonical
[upgrade procedure](installation.md#policy-rbac-and-upgrades).

## Uninstall left resources behind

This is expected. Normal uninstall retains:

- both Kubeseer CRDs;
- every `Kubeseer` instance;
- `installation-access-ceiling`;
- every externally managed TLS Secret.

Use the uninstall wrapper to report retained state. Run the separate purge
client only when permanent deletion is intended and all exact confirmations
are available. See [Safe uninstall and explicit purge](installation.md#safe-uninstall-and-explicit-purge).
