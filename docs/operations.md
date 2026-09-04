# Operations

This guide covers the running controller: readiness, public outcomes,
telemetry, diagnosis, and routine lifecycle checks. Installation, upgrades,
rollback, uninstall, and purge commands are in
[Installation and lifecycle](installation.md).

## Health and readiness

The manager exposes:

| Endpoint | Default bind | Meaning |
| --- | --- | --- |
| `/healthz` | `:8081` | Process liveness |
| `/readyz` | `:8081` | Runtime and webhook-serving readiness |
| `/metrics` | `:8080` | Prometheus metrics |

Readiness remains false until webhook TLS is present and valid: the key must
match the certificate, the time window must be current, every exact Service
SAN must exist, and the certificate must verify against the configured CA.

Basic checks:

```sh
kubectl --context my-cluster -n kubeseer-system \
  rollout status deployment/kubeseer --timeout=5m
kubectl --context my-cluster -n kubeseer-system \
  get deployment kubeseer -o wide
kubectl --context my-cluster -n kubeseer-system \
  get pods,service
kubectl --context my-cluster -n kubeseer-system \
  get endpointslice --selector kubernetes.io/service-name=kubeseer-webhook
```

The last command inspects every controller-managed webhook backend through the
discovery/v1 EndpointSlice API and the standard Service-name selector.

For direct endpoint inspection without exposing the Service externally:

```sh
kubectl --context my-cluster -n kubeseer-system \
  port-forward deployment/kubeseer 8081:8081 8080:8080
curl --fail http://127.0.0.1:8081/healthz
curl --fail http://127.0.0.1:8081/readyz
curl --fail http://127.0.0.1:8080/metrics
```

## Read a Kubeseer outcome

Always compare `metadata.generation` with `status.observedGeneration`, then
read conditions before inspecting the potentially larger result:

```sh
kubectl --context my-cluster -n applications get kubeseer workload-view \
  -o jsonpath='generation={.metadata.generation}{" observed="}{.status.observedGeneration}{"\n"}{range .status.conditions[*]}{.type}={.status} reason={.reason}{"\n"}{end}'
kubectl --context my-cluster -n applications get kubeseer workload-view \
  -o jsonpath='{.status.summary}{"\n"}{.status.resultHash}{"\n"}'
```

Condition interpretation:

| Observation | Interpretation |
| --- | --- |
| `Ready=True`, `Degraded=False` | Current generation completed successfully |
| `Ready=False`, `Degraded=True`, result present | Useful partial result is available; inspect failed source or aggregate outcomes |
| `Accepted=False` | The stored declaration cannot currently be planned or validated |
| `Authorized=False` | Logical policy or Kubernetes RBAC blocks observation |
| `SourcesResolved=False` | API discovery or target resolution failed |
| `Ready=False` | No complete current result is available; use the reason and result presence to identify the stage |
| observed generation is older | Status belongs to an earlier spec; wait or diagnose reconciliation |

Automation should branch on condition `type`, `status`, and `reason`, never on
message text. Public reason codes are listed in the
[API reference](api-reference.md#conditions).

`status.resultHash` identifies the semantic result. A stable hash with repeated
reconciliations is expected: Kubeseer suppresses status writes when only
volatile processing details changed.

## Kubernetes Events

Kubeseer emits one bounded Event after a semantic status write. A successful
evaluation emits a Normal Event; the first non-success condition in
`Accepted`, `Authorized`, `SourcesResolved`, then `Ready` order determines a
Warning Event.

```sh
kubectl --context my-cluster -n applications get events \
  --field-selector involvedObject.kind=Kubeseer,involvedObject.name=workload-view \
  --sort-by=.lastTimestamp
```

Event messages are sanitized and do not contain source payloads, extracted
values, selector operands, or raw errors.

## Metrics

The manager exports these stable metric families:

| Metric | Labels | Purpose |
| --- | --- | --- |
| `kubeseer_reconciliations_total` | `outcome`, `reason` | Reconciliation terminal outcomes |
| `kubeseer_reconciliation_duration_seconds` | `outcome` | Reconciliation latency |
| `kubeseer_resources_read_total` | `scope` | Observed-resource read volume |
| `kubeseer_source_failures_total` | `stage`, `reason` | Source failures by pipeline stage |
| `kubeseer_results_produced_total` | `outcome` | Produced result outcomes |
| `kubeseer_status_updates_total` | `outcome`, `reason` | Written versus suppressed status updates |
| `kubeseer_authorization_decisions_total` | `kind`, `outcome`, `reason` | Logical and API authorization decisions |
| `kubeseer_jsonpath_failures_total` | `reason` | Extraction failures |
| `kubeseer_source_watch_restarts_total` | `reason` | Dynamic source-watch restarts |

Labels are finite and must not be augmented with names, UIDs, field paths, or
user values. Useful alerts include sustained reconciliation failures,
authorization-unavailable outcomes, readiness loss, repeated watch restarts,
and a sharp increase in result-limit failures. Rate-based alerts are preferable
to individual transient failures.

## Structured logs and tracing

Logs use stable event codes including:

- `ReconciliationStarted`, `ReconciliationSkipped`,
  `ReconciliationRetryScheduled`, `ReconciliationFailed`,
  `ReconciliationCompleted`;
- `SourceFailed`, `JSONPathFailed`;
- `AuthorizationDecision`, `ReadForbidden`;
- `StatusUpdateWritten`, `StatusUpdateSkipped`;
- `SourceWatchStopped`, `SourceWatchRestarted`.

Inspect a bounded manager tail:

```sh
kubectl --context my-cluster -n kubeseer-system logs \
  deployment/kubeseer --all-containers --tail=200
```

Optional tracing is disabled by default. Enable it only with a reviewed OTLP
endpoint:

```yaml
tracing:
  enabled: true
  endpoint: otel-collector.observability.svc:4317
```

Logs and traces intentionally omit result values and confidential inputs. Use
the `Kubeseer` status, subject to its RBAC, when value-level inspection is
required.

## Authorization diagnosis

Logical authorization and Kubernetes API authorization have different public
outcomes:

- `AuthorizationDenied`: the requested scope is outside
  `installation-access-ceiling`;
- `PolicyMissing` or `PolicyInvalid`: the singleton is absent or unusable;
- `AuthorizationUnavailable`: a fresh decision could not be established;
- `ReadForbidden`: policy allowed the target, but the manager ServiceAccount
  was denied by Kubernetes RBAC.

Inspect both controls:

```sh
kubectl --context my-cluster get kubeseeraccesspolicy \
  installation-access-ceiling -o yaml
kubectl --context my-cluster auth can-i list deployments.apps \
  --as=system:serviceaccount:kubeseer-system:kubeseer \
  --namespace applications
```

The actual ServiceAccount name is release-dependent; confirm it from the
Deployment before running `auth can-i`.

## Routine lifecycle checks

Before an upgrade:

1. record `helm history` and the deployed image version;
2. render and validate new values;
3. run `make package-crd-check` with an explicit kubeconfig and context;
4. back up custom resources and the installation policy using normal cluster
   administration procedures;
5. confirm certificate ownership and renewal state;
6. apply compatible CRDs explicitly, then run the Helm upgrade;
7. wait for Deployment and webhook readiness;
8. compare conditions, Events, metrics, and representative result hashes.

Normal uninstall intentionally retains CRDs and data. Do not run the purge
client as routine cleanup. The exact safe procedures are in
[Installation](installation.md#safe-uninstall-and-explicit-purge).

## Diagnostic order

For most incidents, use this order:

1. Deployment availability and `/readyz`;
2. `metadata.generation` versus `status.observedGeneration`;
3. condition types and reasons;
4. source and aggregate states in `status.result`;
5. namespace Events;
6. bounded structured logs and relevant metric deltas;
7. policy and exact `auth can-i` checks;
8. certificate objects and Secret metadata, never private-key output.

See [Troubleshooting](troubleshooting.md) for symptom-specific remedies and
[Security model](security.md) for diagnostic confidentiality requirements.
