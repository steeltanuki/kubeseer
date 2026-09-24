---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-23T07:20:14Z
last_modified: 2026-09-23T07:20:14Z
approved_fingerprint: sha256:965c3a8050d9b6c21d26acf55674150bed5f6ba20ccf9e8fdab5d538810618e7
---

# Requirements Document

## Introduction

An owned kind cluster can remain in Podman storage after a host shutdown while
its control-plane container is exited. In that state, `make local-up` currently
stops at the Kubernetes API identity check. A contributor must start the
container manually before the normal local convergence can run.

This corrective feature lets an explicit `make local-up` resume the existing,
exactly owned control-plane container and then continue the approved local
workflow. It supplements `local-development-environment` without changing
cluster creation, cleanup, or other local entry points.

<!-- assumed: resumption happens only during an explicit `make local-up`, because that command is the approved mutation boundary (source: .walden/specs/local-development-environment/requirements.md R1.AC2 and design.md Command behavior) -->
<!-- assumed: this is a corrective follow-up to the completed baseline feature, so its approval and evidence are independent (source: .walden/constitution.md Specification Governance and SPECIFICATIONS.md section 11) -->

## Requirements

### R1 Resume the owned stopped node

**User Story:** As a contributor, I want `make local-up` to resume my existing
local cluster after a host shutdown, so that I can use the normal setup command
without manually operating Podman or losing cluster state.

#### Acceptance Criteria

1. `R1.AC1` WHEN a contributor runs `make local-up` for a previously owned cluster whose control-plane container is exited, the system SHALL start that same container through rootless Podman.
2. `R1.AC2` WHEN the resumed node passes the existing Kubernetes API and kind node identity checks, the system SHALL continue normal `make local-up` convergence.
3. `R1.AC3` WHEN a contributor runs `make local-up` against a healthy owned cluster, the system SHALL converge it without restarting its control-plane container.
4. `R1.AC4` WHEN a contributor runs `make local-up` without an existing owned cluster, the system SHALL follow the existing new-cluster creation path.
5. `R1.AC5` WHILE an exited owned node is being resumed before API identity validation completes, the system SHALL preserve existing in-cluster workloads.
6. `R1.AC6` IF stopped-node startup or API readiness fails, THEN the system SHALL retain the owned metadata and kubeconfig for a later retry.

### R2 Confine recovery to the exact owned target

**User Story:** As a contributor with other Podman and Kubernetes resources, I
want recovery limited to the cluster this project owns, so that a local setup
attempt cannot start or replace an unrelated target.

#### Acceptance Criteria

1. `R2.AC1` IF a named cluster exists without valid matching owned metadata, THEN the system SHALL refuse to start any Podman container for that cluster.
2. `R2.AC2` IF the candidate container's kind cluster name, node name, or control-plane role does not match the owned target, THEN the system SHALL refuse to start that container.
3. `R2.AC3` IF owned metadata claims a cluster whose kind node is absent, THEN the system SHALL report an ownership conflict.
4. `R2.AC4` IF the owned control-plane container is running while its API is unavailable, THEN the system SHALL leave the container running without an automatic restart.
5. `R2.AC5` WHILE stopped-node recovery executes, the system SHALL not delete or recreate the owned cluster.
6. `R2.AC6` WHILE `make local-check` or `make local-status` executes, the system SHALL leave exited kind nodes stopped.
7. `R2.AC7` IF the owned kubeconfig API endpoint does not match the candidate container's published Kubernetes API port, THEN the system SHALL refuse to start that container.

### R3 Bound and explain the recovery outcome

**User Story:** As a contributor, I want an attempted resume to finish
predictably and report what failed, so that I can diagnose remaining Podman or
Kubernetes problems without losing the existing cluster.

#### Acceptance Criteria

1. `R3.AC1` WHEN `make local-up` starts an exited owned node, the system SHALL wait for the recorded Kubernetes API endpoint within a bounded timeout.
2. `R3.AC2` IF Podman cannot start the owned node, THEN the system SHALL report the node name and Podman failure reason.
3. `R3.AC3` IF the resumed API remains unreachable when the timeout expires, THEN the system SHALL report the expired API readiness wait.
4. `R3.AC4` IF the resumed API identity conflicts with the owned kubeconfig or kind node identity, THEN the system SHALL stop before issuing a Kubernetes write.
5. `R3.AC5` WHEN `make local-up` completes after resuming an exited node, the system SHALL emit the existing `LOCAL_ENVIRONMENT=up STATUS=passed` marker.
6. `R3.AC6` The system SHALL document automatic stopped-node recovery and its failure guidance in the local development and troubleshooting guides.

## Non-Functional Requirements

- `NFR1` Recovery must have a finite readiness budget; `R3.AC1` and `R3.AC3` expose its behavior.
- `NFR2` Recovery must preserve ownership isolation and existing local state; `R1.AC5`, `R1.AC6`, and `R2.AC1` through `R2.AC7` expose its behavior.

## Constraints And Dependencies

- `C1` This feature depends on the approved `local-development-environment` lifecycle, including rootless Podman, kind, exact project ownership metadata, explicit kubeconfig and context, and preflight before mutation. Ownership or provider failure is handled by `R2.AC1`, `R2.AC2`, and `R3.AC2`.
- `C2` Resumption must use the existing configured local identity and a bounded readiness budget. Missing nodes, mismatched ports, and unavailable APIs are handled by `R2.AC3`, `R2.AC7`, and `R3.AC3`.
- `C3` Existing-cluster convergence retains the baseline failure-preservation and no-unrelated-cleanup contract. `R1.AC5`, `R1.AC6`, and `R2.AC5` enforce this boundary.

## Out Of Scope

- Changing Podman restart policies, `podman-restart.service`, systemd linger, or host boot behavior.
- Automatically resuming the cluster during read-only commands or without an explicit `make local-up`.
- Restarting a running node whose Kubernetes API is unhealthy.
- Deleting or recreating an owned cluster to recover a stopped node.
- Changing the independent disposable `make e2e` cluster lifecycle.
