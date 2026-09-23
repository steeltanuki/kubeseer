---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-23T08:27:59Z
last_modified: 2026-09-23T08:27:59Z
approved_fingerprint: sha256:354b2133d491577537bbffc76d914e49b6aae739f4bb80cd9cad5010c439dd4a
source_requirements_approved_at: 2026-09-23T07:20:14Z
source_requirements_fingerprint: sha256:965c3a8050d9b6c21d26acf55674150bed5f6ba20ccf9e8fdab5d538810618e7
---

# Feature Design

## Overview

Extend the existing shell-owned `local-up` lifecycle with an owned-container
resume gate. After validating metadata, inspect the exact control-plane container,
start it only when exited, wait for its recorded API endpoint, and validate node
identity before image loading or Kubernetes writes. Do not change host startup
policy, recreate clusters, or broaden recovery to other commands.

Keep the read-only local probe aligned with the configured target: pass its
expected cluster name explicitly while retaining the fixed default identity for
normal contributor use. This lets the genuine recovery proof use a run-unique
cluster without weakening metadata ownership checks.

## Architecture

Keep orchestration in `hack/local-environment.sh`, using the existing sanitized
Podman, kind, and explicit-kubeconfig kubectl wrappers. The action lock covers the
entire transition. No daemon, new package, or metadata migration is needed.

The existing owned-cluster branch becomes:

1. Validate metadata, configured identity, selected Kubernetes version, saved
   kubeconfig, and kind membership. Missing owned nodes remain an ownership error.
2. Resolve `${LOCAL_CLUSTER_NAME}-control-plane` to an immutable Podman container
   ID. Check its exact name, kind cluster label, control-plane role label, state,
   and published `6443/tcp` binding against the saved kubeconfig server.
3. For an exited container, start that ID once. For a running container, do not
   start or restart it. Reject other states with an actionable diagnostic.
4. After a start, poll the explicit API endpoint within a finite budget. Then
   validate API access and compare API node names with kind membership before
   returning to normal convergence. A running node follows the existing immediate
   identity check, with bounded requests, without automatic restart.
5. Continue the existing build/load/install/readiness sequence and success marker.
   The shell probe wrapper passes the configured cluster name and context to the
   read-only local CLI so its identity check follows the same exact target.

The no-cluster creation path and partial-creation cleanup remain unchanged.

## Options Considered

### Option A: Resume inside the existing shell lifecycle (selected)

- Summary: Add small inspection, resume, and readiness helpers to the owned branch.
- Why chosen: The shell already owns locking, Podman operations, metadata, and
  cleanup. This keeps the mutation decision beside its ownership checks.

### Option B: Add a Go recovery command

- Summary: Extend `cmd/kubeseer-local` with provider inspection, start, and polling.
- Why rejected: Although typed parsing is attractive, this introduces container
  mutation into the read-only probe and duplicates shell lifecycle ownership.
  The narrow inspection contract can use Podman's formatted output and kubectl
  JSONPath without introducing a new parser or dependency.

### Option C: Infer the probe target from ownership metadata (rejected)

- Summary: Accept any cluster/context pair that is internally consistent in
  `metadata.v1`.
- Why rejected: The metadata is the claim being checked, not an independent
  expectation. Inferring the target from it would weaken the probe's ownership
  boundary.

Host restart policies are an operational alternative, not this feature: they
change boot behavior and do not repair an explicitly requested `local-up` safely.

## Simplicity And Elegance Review

- Simplest viable shape: One guarded branch, one container start, one bounded API
  wait, and the existing convergence path. Reuse tools already in preflight.
- Coupling check: Keep lifecycle helpers local to the router; shared wrappers stay
  stateless and the Go probe remains read-only. Its expected identity is explicit,
  and its default remains the existing contributor cluster.
- Future-proofing: Defer multi-control-plane recovery, reboot automation, kubeconfig
  repair, restart policies, and automatic unhealthy-container restarts.

## Components And Interfaces

### Owned-target inspection

- Inputs: Valid owned metadata, explicit kubeconfig/context, kind node inventory,
  configured control-plane name.
- Output: Validated immutable container ID and state, or a nonzero ownership error.
- Read only the needed Podman fields: ID, name, state, `io.x-k8s.kind.cluster`,
  `io.x-k8s.kind.role`, and API port bindings. Require the expected node in kind's
  inventory; never select arbitrary containers using a broad name search.
- Extract only the selected kubeconfig server through the explicit context. Require
  HTTPS and an unambiguous matching loopback host/port binding. Reject absent,
  wildcard, malformed, or conflicting bindings rather than rewrite kubeconfig.
- Use the validated ID for start and subsequent inspection, so name reuse cannot
  redirect the operation to a replacement container. Recheck identity/binding after
  start before any Kubernetes write.
- Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC7`.

### Read-only probe ownership identity

- Keep `kubeseer-local` as the private CLI's default expected cluster name. Add
  an explicit `--cluster-name` input and have the shell probe wrapper pass
  `LOCAL_CLUSTER_NAME` together with the already explicit context.
- Before constructing an API client, require the expected cluster name to be a
  valid DNS-1123 label, the expected context to equal `kind-<cluster-name>`,
  and both values to match `metadata.v1`. Continue requiring the fixed local
  namespace and Helm release, absolute owned kubeconfig, and HTTPS loopback
  endpoint. Apply the same expected-identity check before diagnostics records
  ownership data.
- Never derive the expected cluster or context from metadata. Keep metadata
  schema unchanged and the probe read-only.
- Requirements: `R2.AC2`, `C2`.

### Resume and API gate

- Start only `exited`; leave `running` alone. Other states fail with state and node
  name. Provider failures retain the Podman reason and exact node name.
- Default API readiness budget: 120 seconds. A narrowly scoped
  `KUBESEER_LOCAL_RESUME_TIMEOUT_SECONDS` accepts integers from 1 through 600;
  validate it before mutation. This supports slower hosts and short timeout proofs
  without changing existing package readiness settings.
- Use a Bash elapsed-time deadline. Each kubectl request receives a request timeout
  capped at five seconds and the remaining budget; polling pauses are capped by
  the remaining budget. No retry restarts the container. The budget governs API
  observation after successful Podman start, not package installation.
- Poll authenticated API access with the saved kubeconfig and normal TLS checks.
  On expiry, report the node, context, and expired API wait. Never print kubeconfig
  contents or credentials. After reachability, compare the API's sorted node names
  with kind's inventory; conflicts fail before image loading or Kubernetes writes.
- Print a concise resume diagnostic and retain the existing
  `LOCAL_ENVIRONMENT=up STATUS=passed` marker only on complete convergence.
- Requirements: `R1`, `R2.AC4`, `R2.AC5`, `R3.AC1`, `R3.AC2`, `R3.AC3`,
  `R3.AC4`, `R3.AC5`, `NFR1`.

### Command boundaries and contributor guidance

- Do not call resume from `local-check`, `local-status`, diagnostics, or cleanup.
  Check still verifies prerequisites rather than asserting a running cluster.
- Update `docs/local-development.md` and `docs/troubleshooting.md` with stopped-node
  recovery, the timeout setting, retained-state behavior, and explicit guidance
  for ownership conflicts and running-but-unreachable nodes.
- Requirements: `R2.AC6`, `R3.AC6`.

## Data Models

Keep `metadata.v1` unchanged. Inspection fields and the resume decision are
transient shell values. An owned cluster remains `owned` throughout recovery;
never mark it `creating` or set the new-cluster cleanup flags. Failed start, API
timeout, and identity conflict preserve metadata and kubeconfig byte-for-byte.
Normal successful convergence may update existing image/source metadata as before.

## Error Handling

Distinguish metadata/ownership conflict, inspection failure, unsupported container
state, Podman start failure, API wait expiry, and post-start identity mismatch.
Fail closed on missing or ambiguous inspection output. Preserve existing lock
release and failure traps; never route resume failure through partial-cluster
deletion. Read-only inspection failure is not evidence that a cluster is absent.
Do not automatically rewrite endpoints, export a replacement kubeconfig, or offer
destructive recreation as an implicit retry.

## Security Considerations

Use existing sanitized wrappers and explicit state paths/context, ignoring ambient
cloud credentials and kubeconfig. Validate ownership before container mutation and
API identity before Kubernetes mutation. Do not disable TLS verification, dump
container environment, or include credential-bearing kubeconfig data in output.
The lock coordinates this tool's actions, not arbitrary external Podman activity;
immutable-ID targeting and post-start checks contain that race by failing closed.

The Go probe's expected cluster name comes from the CLI default or an explicit
argument supplied by the shell, never from the metadata being validated. Reject
invalid configured names, context/name disagreement, and metadata mismatch
before connecting to Kubernetes.

## Failure Modes And Tradeoffs

- Host shutdown leaves the owned node exited: start the same container, preserving
  its storage; explicit `local-up` remains necessary after reboot.
- Wrong labels, node, or endpoint: refuse recovery. Strict matching can require
  manual diagnosis of unusual bindings, but avoids starting an unrelated target.
- API never becomes reachable: expire the wait and retain state. Leave the started
  node running for diagnosis; do not roll back by stopping or deleting it.
- Running node is unhealthy: fail the identity gate without restart. Recovery of
  that distinct fault remains an explicit operator decision.
- External removal/replacement: fail inspection/start or post-start validation;
  no cluster adoption or recreation fallback is allowed.
- Invalid or inconsistent probe identity: fail before API access; do not fall
  back to the metadata's claimed cluster/context.
- Control plane returns before workloads are ready: API recovery is only the
  first gate; existing package/readiness checks still determine final success.

## Testing Strategy

Use cross-command integration through the actual shell router, not a new isolated
unit-test layer. Extend `hack/local-environment-acceptance.sh` with a `resume` mode
and include it in `complete`. Stateful tool doubles must model container state,
labels, port binding, valid kubeconfig projection, API availability, and API node
inventory; parse wrapper-prefixed global flags correctly. Assert trace ordering
and forbidden operations, not only final marker text.

Exercise the compiled read-only CLI's identity boundary without a live cluster:
prove the default identity and an explicitly configured run-unique identity
pass metadata validation and reach the API observation stage, while invalid
names, mismatched contexts, and metadata/expectation mismatches fail before API
access. Keep these assertions in the existing local-environment verification
layer rather than adding package-local unit tests.

Add a dedicated genuine recovery harness with run-unique cluster name/context,
isolated state/cache/kubeconfig, and a private fixture resource. It creates its own
cluster through the public lifecycle, records container ID and fixture UID/data,
stops only that proven-owned disposable node, runs `local-up`, and verifies the
same ID and fixture survive with successful readiness. Never stop the contributor's
default cluster. Cleanup targets only the proven fixture; retain recovery paths
if cleanup fails. Do not modify the unrelated E2E lifecycle or depend on the existing
genuine harness's default cluster identity.

## Verification Plan

- Deterministic resume integration: exited owned success with delayed API; healthy
  running no start; fresh creation unchanged; start failure; bounded API timeout;
  missing/malformed/unowned metadata; absent kind node; wrong name/cluster/role;
  missing or mismatched port mapping; unsupported state; post-start node conflict;
  running-but-unreachable no restart; check/status leave exited nodes stopped;
  concurrent action lock; invalid timeout before mutation.
- Compiled probe identity checks: fixed default and explicit run-unique
  cluster/context accepted only when metadata agrees; invalid names and all
  mismatches refused before Kubernetes access.
- For failure cases, assert unchanged metadata/kubeconfig and no delete/create,
  image load, Helm mutation, or Kubernetes writes after the failed gate. For success,
  assert exactly one start, identity-before-convergence ordering, and final marker.
- Genuine proof: same container ID and fixture UID/data before/after stop/resume,
  public readiness and success marker, then repeat `local-up` without another
  container start. Emit `LOCAL_CLUSTER_RESUME_ACCEPTANCE=genuine STATUS=passed`
  only after real-cluster assertions; missing host capability must not produce that
  marker or count a fake fallback as genuine evidence.
- Regression: shell syntax checks, complete deterministic local acceptance, existing
  local-environment static/probe verification, documentation checks, and
  `git diff --check`. Record genuine host-access limitations separately from test
  failures. Exact proof commands and budgets belong in the Tasks phase.
- Operational evidence: bounded resume/failure messages and existing lifecycle
  success output; no new telemetry subsystem.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Owned branch, resume gate, unchanged creation/convergence; deterministic and genuine preservation proofs |
| `R2` | Owned-target inspection, immutable-ID start, configured probe identity, read-only command boundaries, negative trace assertions |
| `R3` | Deadline and diagnostics, post-start API/node checks, existing success marker, both contributor guides |
| `NFR1` | Finite observation deadline, per-request timeout, timeout integration proof |
| `NFR2` | Exact-target checks, unchanged metadata schema and cleanup flags, failure preservation and isolated genuine fixture |
| `C1` | Existing preflight, sanitized wrappers, explicit kubeconfig/context and action lock |
| `C2` | Configured local identity in shell and read-only probe, bounded resume timeout and failure coverage |
| `C3` | No resume cleanup or adoption; retained state and no unrelated cluster operations |
