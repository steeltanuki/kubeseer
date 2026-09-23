---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-23T08:32:55Z
last_modified: 2026-09-23T08:46:12Z
approved_fingerprint: sha256:2329204aa9aa8e74872664a24db2d0c6c18bdf4c41264fbafded29dbde6f1fbe
source_design_approved_at: 2026-09-23T08:27:59Z
source_design_fingerprint: sha256:354b2133d491577537bbffc76d914e49b6aae739f4bb80cd9cad5010c439dd4a
---

# Implementation Plan

- [x] 1. Guard and resume the owned control plane in the local lifecycle
  - [x] 1.1 Add exact-target inspection and deterministic ownership cases
    - Extend the stateful Podman, kind, and kubectl doubles in
      `hack/local-environment-acceptance.sh` so they expose an immutable
      container ID, name, kind labels, lifecycle state, API port binding,
      kubeconfig endpoint, and node inventory. Exercise the real `local-up`
      router through a `resume-ownership` mode.
    - In `hack/local-environment.sh`, validate the existing owned metadata,
      kind node membership, exact control-plane name/cluster/role, HTTPS
      loopback kubeconfig endpoint, and matching published API port before any
      Podman start. Refuse absent, malformed, or ambiguous targets. Preserve the
      no-cluster creation path and existing owned files on failure.
    - Assert no start, create, delete, image load, Helm operation, or Kubernetes
      write in every negative case. Assert fresh creation remains a separate
      path. Verify the selected container ID is used for later operations.
    - Requirements: `R1.AC4`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC7`, `NFR2`, `C1`, `C2`
    - Design: Architecture; Components And Interfaces / Owned-target inspection;
      Security Considerations; Error Handling; Verification Plan
    - Verification:
      - command: ["bash", "-n", "hack/local-environment.sh"]
        covers: ["C1"]
      - command: ["bash", "-n", "hack/local-environment-acceptance.sh"]
        covers: ["C1"]
      - command: ["./hack/local-environment-acceptance.sh", "resume-ownership"]
        expect_output: "LOCAL_CLUSTER_RESUME_ACCEPTANCE=ownership STATUS=passed"
        timeout: 25m
        covers: ["R1.AC4", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC7", "NFR2", "C1", "C2"]

  - [x] 1.2 Start an exited owned node once and bound the API identity gate
    - In the owned branch of `local-up`, start only the inspected exited
      container ID. Keep healthy running nodes on the normal path; leave
      running but unreachable nodes untouched. Validate the configured timeout
      before mutation, cap each API request and poll by the remaining budget,
      then compare authenticated API and kind node identity before convergence.
    - Extend acceptance mode `resume` with delayed API success, idempotent
      repeat `local-up`, start error, timeout, unsupported state, endpoint or
      node conflict after start, retained metadata/kubeconfig, forbidden
      write/delete/create traces, and `local-check`/`local-status` read-only
      behavior. Include these cases in `complete` without emitting a genuine
      marker from fake tools.
    - Keep the existing success marker, source/image/Helm convergence, action
      lock, and partial-creation cleanup boundary intact.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC5`, `R1.AC6`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `NFR1`, `NFR2`, `C1`, `C2`, `C3`
    - Design: Architecture; Components And Interfaces / Resume and API gate;
      Data Models; Error Handling; Failure Modes And Tradeoffs; Verification Plan
    - Verification:
      - command: ["bash", "-n", "hack/local-environment.sh"]
        covers: ["C1"]
      - command: ["bash", "-n", "hack/local-environment-acceptance.sh"]
        covers: ["C1"]
      - command: ["./hack/local-environment-acceptance.sh", "resume"]
        expect_output: "LOCAL_CLUSTER_RESUME_ACCEPTANCE=deterministic STATUS=passed"
        timeout: 30m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC5", "R1.AC6", "R2.AC4", "R2.AC5", "R2.AC6", "R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "NFR1", "NFR2", "C2", "C3"]
      - command: ["./hack/local-environment-acceptance.sh", "complete"]
        expect_output: "LOCAL_ENVIRONMENT_ACCEPTANCE=complete STATUS=passed"
        timeout: 35m
        covers: ["R1.AC4", "R2.AC5", "C1", "C3"]

- [x] 2. Prove configured probe identity and real state preservation
  - [x] 2.1 Bind the read-only probe to the configured owned identity
    - Add a private `--cluster-name` option to `cmd/kubeseer-local` with the
      existing `kubeseer-local` default. Pass `LOCAL_CLUSTER_NAME` from the
      shell probe wrapper beside its explicit context. Keep the probe read-only
      and the `metadata.v1` schema unchanged.
    - Validate the expected cluster as a DNS-1123 label and require the expected
      context to equal `kind-<cluster-name>`. Require metadata to match both
      expected values and the fixed namespace/release before API client creation
      or diagnostic ownership output. Keep the absolute kubeconfig and HTTPS
      loopback checks.
    - Extend `hack/verify-local-environment.sh` to exercise its compiled probe
      against isolated metadata/kubeconfig fixtures. Assert the fixed default
      and an explicit run-unique identity reach API observation, while invalid
      names and cluster/context/metadata mismatches fail before API access.
      Verify the shell passes its configured name and emit
      `LOCAL_PROBE_IDENTITY=passed` only after these assertions.
    - Requirements: `R2.AC2`, `NFR2`, `C1`, `C2`
    - Design: Components And Interfaces / Read-only probe ownership identity;
      Security Considerations; Testing Strategy; Verification Plan
    - Verification:
      - command: ["bash", "-n", "hack/local-environment.sh"]
        covers: ["C1"]
      - command: ["bash", "-n", "hack/verify-local-environment.sh"]
        covers: ["C1"]
      - command: ["make", "verify-local-environment"]
        expect_output: "LOCAL_PROBE_IDENTITY=passed"
        timeout: 45m
        covers: ["NFR2", "C1", "C2"]
      - command: ["./hack/local-environment-acceptance.sh", "resume-ownership"]
        expect_output: "LOCAL_CLUSTER_RESUME_ACCEPTANCE=ownership STATUS=passed"
        timeout: 25m
        covers: ["R2.AC2", "NFR2", "C2"]

  - [x] 2.2 Add the isolated genuine resume proof and contributor guidance
    - Add `hack/test-local-cluster-resume.sh` and a
      `make test-local-cluster-resume` target. The harness shall use run-unique
      cluster/context/state/cache paths, create its own cluster with the public
      `local-up`, store its control-plane ID and a private workload fixture,
      stop only that proven-owned disposable container, and call the public
      `local-up` again. Confirm same container ID and workload identity/data,
      public readiness, and exactly one recovery start. Repeat `local-up` to
      prove it does not restart the healthy node.
    - On completion, remove only the proven fixture cluster. On cleanup
      failure, report its exact recovery path. Emit the genuine acceptance
      marker only after the real-cluster assertions; missing host capability
      or fake-tool fallback must not produce that marker.
    - Update `docs/local-development.md` and `docs/troubleshooting.md` with
      automatic stopped-node recovery, timeout control, and guidance for
      ownership conflicts, retained state, and a running unreachable API.
      Preserve the default contributor cluster and unrelated provider objects.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC5`, `R1.AC6`, `R2.AC5`, `R3.AC1`, `R3.AC5`, `R3.AC6`, `NFR1`, `NFR2`, `C1`, `C3`
    - Design: Components And Interfaces / Read-only probe ownership identity;
      Components And Interfaces / Command boundaries and contributor guidance;
      Testing Strategy; Verification Plan; Failure Modes And Tradeoffs
    - Verification:
      - command: ["bash", "-n", "hack/test-local-cluster-resume.sh"]
        covers: ["C1"]
      - command: ["make", "verify-local-environment"]
        expect_output: "LOCAL_ENVIRONMENT_VERIFY=complete STATUS=passed"
        timeout: 45m
        covers: ["C1", "C3"]
      - command: ["make", "test-local-cluster-resume"]
        expect_output: "LOCAL_CLUSTER_RESUME_ACCEPTANCE=genuine STATUS=passed"
        timeout: 120m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC5", "R1.AC6", "R2.AC5", "R3.AC1", "R3.AC5", "NFR1", "NFR2", "C1", "C3"]
      - command: ["rg", "-q", "stopped|exited|resume", "docs/local-development.md"]
        covers: ["R3.AC6"]
      - command: ["rg", "-q", "stopped|exited|resume", "docs/troubleshooting.md"]
        covers: ["R3.AC6"]
