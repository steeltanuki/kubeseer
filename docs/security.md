# Security model

Kubeseer is a delegated observer: administrators decide the maximum data scope,
authors declare narrower views, and the manager reads resources on their
behalf. This guide describes the trust boundaries and the controls that keep
an authored resource from expanding installation authority.

## Trust boundaries

| Actor or component | Authority |
| --- | --- |
| Cluster administrator | Installs the chart, owns policy, manager RBAC, limits, certificates, and lifecycle |
| Kubeseer author | Creates or changes namespaced `Kubeseer` declarations only where RBAC permits |
| Manager ServiceAccount | Performs exact reads granted by administrator-controlled Kubernetes RBAC |
| `installation-access-ceiling` | Defines the logical namespace and resource-type ceiling |
| Kubernetes API server | Enforces RBAC, admission registration, object persistence, and API semantics |
| Status reader | Can read all values materialized into any `Kubeseer` it is authorized to get |

The installation policy and ServiceAccount RBAC are intentionally independent.
Both must allow a target. Author permissions do not imply observed-resource
permissions, and manager RBAC alone does not imply logical authorization.

## Fail-closed authorization

Every source is resolved to an exact API resource and namespace scope. The
controller then evaluates a fresh policy snapshot and creates an internal
capability bound to:

- the exact `Kubeseer` subject and generation;
- the current policy identity;
- the resolved API group, version, resource, Kind, and scope;
- the permitted operation.

The network adapter requires that capability before `LIST`. Source routes and
watches require equivalent authorization. Missing or invalid policy,
discovery ambiguity, denied scope, policy drift, stale generation, and
revocation all stop observation or publication. Kubernetes RBAC can still deny
an otherwise policy-authorized request, producing `ReadForbidden`.

Policy and identity changes cancel in-flight work and invalidate existing
routes. An older result is not allowed to publish after authority changes.

## Admission and runtime revalidation

The validating webhook rejects structurally invalid declarations, unsupported
JSONPath, incompatible operators or aggregations, unresolved targets, budget
violations, and requests outside the current installation policy.

Admission is not treated as permanent authorization. Resource discovery,
policy, RBAC, and Kubernetes objects can change after storage, so the runtime
repeats the security-sensitive checks on every relevant reconciliation. A
request admitted earlier can later become denied or degraded without another
spec edit.

The packaged webhook uses `failurePolicy: Fail`, so an unavailable webhook
blocks affected CREATE and UPDATE requests. The manager still enforces runtime
authorization before reads.

## Data exposure

The `status` subresource is the product output. It can contain extracted values,
typed objects or lists, aggregate values, and resource provenance. Anyone who
can read a `Kubeseer` can read its current materialized result.

Therefore:

- authorize source types and fields with the same care as a read API;
- do not use Kubeseer to project credentials, tokens, private keys, or other
  secret material;
- restrict RBAC on Kubeseer objects when results contain sensitive operational
  data;
- remember that a cross-namespace view transfers selected data into the
  `Kubeseer` namespace;
- review provenance exposure as well as values.

The Helm chart deliberately rejects Secret resources in its observed-resource
RBAC interface and grants no Secret access by default. An administrator who
adds broader RBAC outside the chart and a matching logical policy assumes
responsibility for the resulting data flow. Kubeseer does not redact values
from its own status because those values are the declared result.

## Diagnostic confidentiality

Operational telemetry is intentionally less detailed than status. Logs,
metrics, Kubernetes Events, traces, admission errors, and diagnostic bundles
exclude:

- extracted, typed, and aggregate values;
- source payloads and result bodies;
- field paths and selector operand values;
- Secret contents, credentials, and tokens;
- raw upstream error strings that may reveal confidential input.

Instead they use bounded reason codes, counts, safe identities, and stage names.
This reduces accidental disclosure but does not make telemetry unrestricted:
namespace, resource type, condition, and outcome metadata may still be
sensitive and should follow the cluster's normal observability controls.

## JSONPath and expression safety

Kubeseer implements a small, native evaluator rather than executing arbitrary
kubectl templates. It accepts property lookup, quoted keys, non-negative array
indexes, and array wildcards only. Filters, scripts, recursive descent,
functions, template directives, and surrounding text are rejected. Regular
expression matching is bounded by the implementation and does not enable code
execution.

Object and list operator operands are JSON data, not executable expressions.
Typed conversion rejects ambiguous or lossy coercions.

## Resource exhaustion controls

The API and manager bound source count, namespaces, fields, operators,
operands, aggregations, selector complexity, spec size, selected resources,
input bytes, output bytes, status size, execution time, concurrent reconciles,
watch count, queued triggers, groups, contributions, and provenance entries.

Limits fail explicitly. Results are never silently truncated and presented as
complete. Administrators should increase a limit only with corresponding API
server, memory, CPU, and data-exposure analysis. See
[Configuration](configuration.md#runtime-limits).

## Package and runtime hardening

The Helm package:

- rejects the mutable `latest` image tag;
- uses an explicit, versioned image and a finite values schema;
- grants authors no observed-resource or administrative RBAC;
- grants the manager no observed-resource or Secret access by default;
- supports only exact `get`, `list`, and `watch` observed permissions;
- mounts serving certificates read-only;
- keeps readiness false for missing, expired, mismatched, untrusted, or
  incomplete webhook TLS material;
- uses leader election and bounded graceful shutdown;
- separates normal uninstall from destructive CRD purge.

Default installation creates a deny-all logical policy. Expand policy and RBAC
in the same reviewed change.

## Certificate ownership

In cert-manager mode, chart resources own the CA and serving-certificate
lifecycle. In `externalSecret` mode, the administrator owns issuance, renewal,
Secret retention, SAN correctness, and CA rollover. Only the public CA belongs
in Helm values. The private key remains in the named Secret.

Readiness verifies key/certificate match, validity time, exact Service SANs,
and trust against the mounted CA. Follow the staged old+new CA rollover in the
[installation guide](installation.md#fallback-externalsecret); activating a
new serving certificate before publishing its CA causes webhook failure.

## Lifecycle safeguards

Normal uninstall removes the runtime and owned webhook registration but
preserves both CRDs, all custom-resource instances, the access policy, and any
external TLS Secret. This makes uninstall reversible and avoids implicit data
destruction.

The separate `kubeseer-purge` client is destructive. It requires an absolute
kubeconfig, an explicit context, an exact confirmed API server, the same
confirmed context, and the literal final token `purge-kubeseer-crds`. It
deletes only Kubeseer's two collections and CRDs and stops if instance cleanup
is blocked. See [Safe uninstall and explicit purge](installation.md#safe-uninstall-and-explicit-purge).

## Administrative checklist

Before expanding an installation:

1. Identify the exact fields that a proposed `Kubeseer` could publish.
2. Confirm the destination namespace's readers are allowed to see that data.
3. Add the minimum resource Kind and namespace to the logical policy.
4. Add matching plural resource and namespace RBAC to the manager.
5. Keep cluster-scoped observation disabled unless it is explicitly required.
6. Validate chart rendering and run package checks.
7. Observe authorization metrics and conditions after rollout.
8. Revoke both policy and RBAC when the observation is no longer needed.

For a detailed declaration contract, see [API reference](api-reference.md).
For runtime signals and diagnosis, see [Operations](operations.md).
