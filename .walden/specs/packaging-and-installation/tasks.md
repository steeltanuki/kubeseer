---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-01T07:02:59Z
last_modified: 2026-09-01T08:44:18Z
approved_fingerprint: sha256:43a7b4cb41f958e7e05a5cb5a411ca03866945bda7497ce19aedd1d9f1e7ce59
source_design_approved_at: 2026-09-01T06:44:34Z
source_design_fingerprint: sha256:1164721abe8248bdb9b119aa7279bfbdb3d05d0e2166c3a55c2fc437dc28f7b0
---

# Implementation Plan

- [x] 1. Add the production executable and immutable manager image
  - [x] 1.1 Compose the real manager, lifecycle subcommands, version metadata, and minimal container
    - Add `cmd/kubeseer` and a narrow internal composition package that parse
      validated configuration, register the v1alpha1 scheme, reconciliation,
      admission, health, readiness, metrics, leader election, limits, optional
      tracing, signal handling, `version`, and bounded `package` hook commands.
    - Add the reproducible multi-stage Dockerfile and build targets. Start the
      compiled `manager` command directly as a fixed numeric non-root user, keep
      the final image free of shells/toolchains/source, reject `latest`, and
      expose immutable OCI and executable version identities.
    - Extend `TestModuleIntegration` through the real composition root with
      successful registration, every startup failure, health/readiness,
      disabled tracing, termination, version output, image configuration, and
      forbidden runtime-content checks. Emit
      `MODULE_INTEGRATION=packaging-manager-image STATUS=passed`.
    - Requirements: `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R2.AC11`, `R2.AC12`, `R2.AC13`, `R2.AC14`, `R2.AC15`, `R2.AC16`, `NFR1`, `NFR3`, `NFR5`, `NFR6`, `NFR7`, `C1`, `C4`, `C6`, `C8`, `C12`
    - Design: Components And Interfaces / Production Command And Manager Composition Root; Workload, Services, And Runtime Security; Error Handling; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-packaging-go-build GOMODCACHE=/tmp/kubeseer-packaging-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=packaging-manager-image STATUS=passed"
        timeout: 50m
        covers: ["R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R2.AC11", "R2.AC12", "R2.AC13", "R2.AC14", "R2.AC15", "R2.AC16", "NFR1", "NFR3", "NFR5", "NFR6", "NFR7", "C1", "C4", "C6", "C8", "C12"]

- [x] 2. Establish the canonical deterministic Helm chart and values API
  - [x] 2.1 Add chart metadata, helpers, defaults, schema, ownership, and package verification
    - Create `charts/kubeseer` as a Helm v2 application chart with SemVer,
      `appVersion`, the approved Kubernetes range, canonical labels/names, CRD
      directory, documented defaults, and no bundled dependency or live
      `lookup`. Implement the finite values tree and conditional JSON schema,
      including namespace/image overrides, policy/RBAC input, two certificate
      modes, performance defaults, omitted-versus-empty semantics, and rejection
      of mutable or incoherent values.
    - Add pinned Helm tooling and read-only package verification for lint,
      deterministic repeated `--include-crds` rendering, every valid/invalid
      values fixture, public-value documentation coverage, ownership identity,
      single-release preflight metadata, and versioned archive inspection. Emit
      `PACKAGE_VERIFY=chart-core STATUS=passed` only after the matrix completes.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `R1.AC10`, `R1.AC11`, `R1.AC12`, `R1.AC13`, `R1.AC14`, `R1.AC15`, `R1.AC16`, `R1.AC17`, `R1.AC18`, `R1.AC19`, `R1.AC20`, `R1.AC21`, `R1.AC22`, `R1.AC23`, `R1.AC24`, `NFR4`, `NFR5`, `NFR6`, `NFR8`, `C2`, `C3`, `C8`, `C13`, `C14`
    - Design: Components And Interfaces / Canonical Helm Chart, Values Contract; Data Models / Render Identity; Options Considered; Simplicity And Elegance Review
    - Verification:
      - command: ["make", "verify-package"]
        expect_output: "PACKAGE_VERIFY=chart-core STATUS=passed"
        timeout: 30m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R1.AC9", "R1.AC10", "R1.AC11", "R1.AC12", "R1.AC13", "R1.AC14", "R1.AC15", "R1.AC16", "R1.AC17", "R1.AC18", "R1.AC19", "R1.AC20", "R1.AC21", "R1.AC22", "R1.AC23", "R1.AC24", "NFR4", "NFR5", "NFR6", "NFR8", "C2", "C3", "C8", "C13", "C14"]

- [x] 3. Render the secure manager workload and operational endpoints
  - [x] 3.1 Add Deployment, Services, probes, resources, security, and rollout configuration
    - Template one Deployment, dedicated ServiceAccount, cluster-local metrics
      Service, and webhook Service. Wire the approved defaults and overrides for
      replicas, leader election, resources, limits, bind addresses, optional
      tracing, rolling update, termination, TLS/CA mounts, and manager arguments.
    - Apply non-root, no-privilege, dropped-capability, runtime-default seccomp,
      read-only-root, and no-host-access controls. Add distinct startup/readiness
      and liveness probes, and test that missing configuration or TLS remains
      unready while metrics are cluster-local.
    - Extend the integration and render fixtures through the real manager setup
      and chart workload. Emit
      `MODULE_INTEGRATION=packaging-workload STATUS=passed`.
    - Requirements: `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R3.AC11`, `R3.AC12`, `R3.AC13`, `R3.AC14`, `R3.AC15`, `R3.AC16`, `R3.AC17`, `R3.AC18`, `R3.AC19`, `R3.AC20`, `R3.AC21`, `R3.AC22`, `R3.AC23`, `R3.AC24`, `NFR1`, `NFR3`, `NFR6`, `NFR8`, `C6`, `C8`
    - Design: Components And Interfaces / Workload, Services, And Runtime Security; Production Command And Manager Composition Root; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-packaging-go-build GOMODCACHE=/tmp/kubeseer-packaging-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=packaging-workload STATUS=passed"
        timeout: 50m
        covers: ["R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R3.AC11", "R3.AC12", "R3.AC13", "R3.AC14", "R3.AC15", "R3.AC16", "R3.AC17", "R3.AC18", "R3.AC19", "R3.AC20", "R3.AC21", "R3.AC22", "R3.AC23", "R3.AC24", "NFR1", "NFR3", "NFR6", "NFR8", "C6", "C8"]

- [x] 4. Package CRDs and enforce explicit compatible upgrades
  - [x] 4.1 Synchronize generated CRDs and add compatibility, apply, establishment, and retention gates
    - Copy both verified generated CRDs byte-for-byte into chart `crds/`, add
      stable digest metadata, and extend generation verification to reject drift
      in scopes, schemas, defaults, list semantics, status subresources, or
      admission markers.
    - Implement the explicit CRD compatibility/apply command and pre-upgrade
      digest gate. Reject storage-version removal, scope changes, destructive
      schema transitions, and conversion requirements; apply approved schemas
      under a fixed field manager and wait for both CRDs to become Established
      before manager rollout. Keep CRDs and instances outside normal uninstall.
    - Add higher-layer fixtures for first install, compatible upgrade, drift,
      incompatible transition, retained instances, and Helm's no-automatic-CRD-
      upgrade behavior. Emit `PACKAGE_VERIFY=crd-lifecycle STATUS=passed`.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R4.AC10`, `R4.AC11`, `R4.AC12`, `R4.AC13`, `R4.AC14`, `R4.AC15`, `NFR3`, `NFR4`, `NFR5`, `NFR9`, `C3`, `C4`, `C10`, `C12`
    - Design: Components And Interfaces / CRD Packaging And Upgrade Gate; Data Models / Package State; Error Handling
    - Verification:
      - command: ["make", "verify-package"]
        expect_output: "PACKAGE_VERIFY=crd-lifecycle STATUS=passed"
        timeout: 30m
        covers: ["R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R4.AC10", "R4.AC11", "R4.AC12", "R4.AC13", "R4.AC14", "R4.AC15", "NFR3", "NFR4", "NFR5", "NFR9", "C3", "C4", "C10", "C12"]

- [x] 5. Synthesize least-privilege RBAC without weakening logical authorization
  - [x] 5.1 Render operator, observed-resource, author, and hook permissions as separate groups
    - Template the operator's owned-API/status/Event/lease/discovery rules,
      namespaced observed-resource Roles and bindings, explicit cluster-scoped
      opt-in, reusable author Roles, and command-specific temporary hook RBAC.
      Keep default observed-resource and Secret reads empty and prevent authors
      from policy, RBAC, webhook, Deployment, or observed-data access.
    - Add schema and static rejection of wildcard resources, wildcard writes,
      Secret reads, namespace leakage, and cluster-scoped RBAC without the
      independent logical opt-in. Prove broader RBAC remains narrowed by runtime
      authorization and narrower RBAC produces the approved sanitized failure.
      Emit `MODULE_INTEGRATION=packaging-rbac STATUS=passed`.
    - Requirements: `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R5.AC12`, `R5.AC13`, `R5.AC14`, `R5.AC15`, `R5.AC16`, `R5.AC17`, `R5.AC18`, `R5.AC19`, `NFR1`, `NFR2`, `C1`, `C7`, `C14`
    - Design: Components And Interfaces / RBAC Synthesis And Permission Boundaries; Security Considerations; Data Models / Package State
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-packaging-go-build GOMODCACHE=/tmp/kubeseer-packaging-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=packaging-rbac STATUS=passed"
        timeout: 50m
        covers: ["R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R5.AC12", "R5.AC13", "R5.AC14", "R5.AC15", "R5.AC16", "R5.AC17", "R5.AC18", "R5.AC19", "NFR1", "NFR2", "C1", "C7", "C14"]

- [x] 6. Bootstrap and preserve the installation access ceiling
  - [x] 6.1 Implement managed and external policy modes through ordered lifecycle hooks
    - Add weighted pre-install/pre-upgrade/pre-rollback policy hooks using the
      typed client. Render the canonical deny-all managed payload, exact
      namespace/resource/system-namespace inputs, stable ownership, and retention
      metadata. External mode must render no policy payload or write operation.
    - Fail preflight without mutation for foreign existing policy ownership,
      preserve effective release values when no policy change is supplied, and
      send explicit upgrades through the active validating webhook. Keep policy
      data independent from ServiceAccount RBAC and retained on uninstall.
    - Extend integration fixtures through real policy compilation, admission,
      hook planning, ownership conflict, reinstall, upgrade, and fail-closed
      absence. Emit `MODULE_INTEGRATION=packaging-policy-bootstrap STATUS=passed`.
    - Requirements: `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R6.AC13`, `R6.AC14`, `R6.AC15`, `R6.AC16`, `R6.AC17`, `R6.AC18`, `NFR1`, `NFR2`, `NFR9`, `C1`, `C4`, `C7`, `C14`
    - Design: Components And Interfaces / Access Policy Bootstrap, Helm Lifecycle Hooks And Safe Uninstall; Data Models / Package State; Error Handling
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-packaging-go-build GOMODCACHE=/tmp/kubeseer-packaging-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=packaging-policy-bootstrap STATUS=passed"
        timeout: 50m
        covers: ["R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R6.AC11", "R6.AC12", "R6.AC13", "R6.AC14", "R6.AC15", "R6.AC16", "R6.AC17", "R6.AC18", "NFR1", "NFR2", "NFR9", "C1", "C4", "C7", "C14"]

- [x] 7. Implement trusted webhook certificate lifecycle in both supported modes
  - [x] 7.1 Render cert-manager and external-Secret resources with reload and trust readiness
    - Template the stable webhook Service/configuration, exact two paths and
      fail-closed admission fields. In default `certManager` mode render the
      pinned issuer/CA/serving Certificate chain and CA injection; fail preflight
      with the documented fallback when required APIs are absent.
    - In `externalSecret` mode require only Secret name and public PEM CA, render
      no cert-manager object or TLS Secret, and mount the named Secret read-only.
      Add controller-runtime certificate watching plus readiness validation for
      key match, time validity, exact Service DNS SANs, and CA chain. Implement
      overlap-safe CA transition checks and same-CA key-pair reload.
    - Extend integration fixtures with deterministic renders, unsupported and
      incomplete modes, issuance/injection delays, invalid/absent Secret data,
      DNS/CA mismatch, serving rotation, CA rollover order, trust probes, and
      external Secret non-ownership. Emit
      `MODULE_INTEGRATION=packaging-webhook-tls STATUS=passed`.
    - Requirements: `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R7.AC12`, `R7.AC13`, `R7.AC14`, `R7.AC15`, `R7.AC16`, `R7.AC17`, `R7.AC18`, `R7.AC19`, `R7.AC20`, `R7.AC21`, `R7.AC22`, `R7.AC23`, `R7.AC24`, `R7.AC25`, `R7.AC26`, `R7.AC27`, `R7.AC28`, `R7.AC29`, `R7.AC30`, `R7.AC31`, `R7.AC32`, `NFR1`, `NFR3`, `NFR4`, `NFR8`, `C5`, `C6`, `C15`
    - Design: Components And Interfaces / Certificate Modes And Reload; Data Models / Certificate State; Security Considerations; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-packaging-go-build GOMODCACHE=/tmp/kubeseer-packaging-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=packaging-webhook-tls STATUS=passed"
        timeout: 55m
        covers: ["R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "R7.AC12", "R7.AC13", "R7.AC14", "R7.AC15", "R7.AC16", "R7.AC17", "R7.AC18", "R7.AC19", "R7.AC20", "R7.AC21", "R7.AC22", "R7.AC23", "R7.AC24", "R7.AC25", "R7.AC26", "R7.AC27", "R7.AC28", "R7.AC29", "R7.AC30", "R7.AC31", "R7.AC32", "NFR1", "NFR3", "NFR4", "NFR8", "C5", "C6", "C15"]

- [x] 8. Enforce deterministic upgrade and compatible rollback
  - [x] 8.1 Add lifecycle preflight, ordered hooks, rolling availability, and release checks
    - Implement weighted preflight/policy/readiness hooks and temporary
      least-privilege RBAC for install, upgrade, and rollback. Check singleton
      ownership, cert-manager prerequisites, CRD digest/storage compatibility,
      policy ownership, required permissions, Deployment availability, webhook
      reachability/trust, and installed version within bounded timeouts.
    - Preserve policy and certificate inputs through effective release values,
      external Secrets through non-ownership, CRs/CRDs through compatible
      rollback, and at least one trusted endpoint through `maxUnavailable: 0`
      rolling replacement. Reject unsupported CRD downgrade before changing the
      release and expose failed rollout state without destructive auto-rollback.
    - Extend higher-layer lifecycle fixtures with no-op upgrade, image change,
      policy/certificate changes, failed readiness, release history, compatible
      rollback, and every rejected boundary. Emit
      `MODULE_INTEGRATION=packaging-upgrade-rollback STATUS=passed`.
    - Requirements: `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `R8.AC11`, `R8.AC12`, `R8.AC13`, `R8.AC14`, `R8.AC15`, `R8.AC16`, `R8.AC17`, `R8.AC18`, `R8.AC19`, `NFR3`, `NFR4`, `NFR6`, `NFR9`, `C3`, `C4`, `C5`, `C14`, `C15`
    - Design: Components And Interfaces / Helm Lifecycle Hooks And Safe Uninstall, CRD Packaging And Upgrade Gate, Certificate Modes And Reload; Error Handling; Failure Modes And Tradeoffs
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-packaging-go-build GOMODCACHE=/tmp/kubeseer-packaging-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=packaging-upgrade-rollback STATUS=passed"
        timeout: 55m
        covers: ["R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "R8.AC10", "R8.AC11", "R8.AC12", "R8.AC13", "R8.AC14", "R8.AC15", "R8.AC16", "R8.AC17", "R8.AC18", "R8.AC19", "NFR3", "NFR4", "NFR6", "NFR9", "C3", "C4", "C5", "C14", "C15"]

- [x] 9. Separate reversible uninstall from destructive CRD purge
  - [x] 9.1 Add ordered delete hooks, retained-state reporting, and the confirmed purge executable
    - Add pre-delete removal of only the owned validating webhook before the
      final serving endpoint, normal Helm deletion of runtime/RBAC/cert-manager
      resources, post-delete reporting, and an idempotent documented wrapper
      using `--ignore-not-found --wait`. Preserve CRDs, all instances, the access
      policy, and every external TLS Secret.
    - Add `cmd/kubeseer-purge` as a separately built versioned client executable.
      Require an absolute kubeconfig, explicit context, resolved target match,
      and exact `purge-kubeseer-crds` token before writes. Delete exact custom
      resource collections first, wait empty, then delete only the two CRDs;
      block on remaining instances/finalizers and report every deleted, absent,
      retained, or failed target without Secret content.
    - Extend integration fixtures with missing/mismatched target and token,
      foreign ownership, interrupted/repeated uninstall, finalizer blockage,
      ordered purge, unrelated-object preservation, external Secret retention,
      and already-purged convergence. Emit
      `MODULE_INTEGRATION=packaging-uninstall-purge STATUS=passed`.
    - Requirements: `R9.AC1`, `R9.AC2`, `R9.AC3`, `R9.AC4`, `R9.AC5`, `R9.AC6`, `R9.AC7`, `R9.AC8`, `R9.AC9`, `R9.AC10`, `R9.AC11`, `R9.AC12`, `R9.AC13`, `R9.AC14`, `R9.AC15`, `R9.AC16`, `R9.AC17`, `R9.AC18`, `R9.AC19`, `R9.AC20`, `R9.AC21`, `R9.AC22`, `R9.AC23`, `R9.AC24`, `R9.AC25`, `R9.AC26`, `NFR1`, `NFR3`, `NFR6`, `NFR9`, `C9`, `C10`, `C12`, `C14`
    - Design: Components And Interfaces / Helm Lifecycle Hooks And Safe Uninstall, Explicit Purge Executable; Data Models / Package State, Purge Plan And Result; Security Considerations
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-packaging-go-build GOMODCACHE=/tmp/kubeseer-packaging-go-mod make test-integration"]
        expect_output: "MODULE_INTEGRATION=packaging-uninstall-purge STATUS=passed"
        timeout: 55m
        covers: ["R9.AC1", "R9.AC2", "R9.AC3", "R9.AC4", "R9.AC5", "R9.AC6", "R9.AC7", "R9.AC8", "R9.AC9", "R9.AC10", "R9.AC11", "R9.AC12", "R9.AC13", "R9.AC14", "R9.AC15", "R9.AC16", "R9.AC17", "R9.AC18", "R9.AC19", "R9.AC20", "R9.AC21", "R9.AC22", "R9.AC23", "R9.AC24", "R9.AC25", "R9.AC26", "NFR1", "NFR3", "NFR6", "NFR9", "C9", "C10", "C12", "C14"]

- [x] 10. Publish executable lifecycle documentation and prove supported-cluster installation
  - [x] 10.1 Complete chart documentation, static package gates, and dual-mode compatibility smoke tests
    - Generate the chart README and operator installation guide with exact pinned
      prerequisites and commands for render, lint, package, install, readiness,
      explicit CRD upgrade, policy/RBAC setup, both certificate modes and
      rotation, compatible rollback, safe uninstall, and confirmed purge. Keep
      defaults, permission boundaries, retained resources, CA ordering, Service
      DNS, Secret format, and version limitations explicit.
    - Extend `make verify` with build/image/chart/schema/generated-artifact/RBAC/
      security/determinism checks and non-vacuous markers. Add a cluster smoke
      harness that requires explicit kubeconfig/context, never ambient cloud
      credentials, and runs install/removal against Kubernetes 1.35.6 and 1.36.2
      with both certificate modes. Prove CRD establishment, manager availability,
      webhook reachability/trust/rejection, deny-all policy, author/operator
      permission separation, same-CA key rotation, upgrade/rollback retention,
      safe uninstall, failed-confirmation purge, and external Secret retention.
    - Emit `PACKAGE_VERIFY=complete STATUS=passed` and
      `PACKAGE_COMPATIBILITY=complete STATUS=passed` only after every required
      profile/version/scenario runs; skips and zero matches fail.
    - Requirements: `R10.AC1`, `R10.AC2`, `R10.AC3`, `R10.AC4`, `R10.AC5`, `R10.AC6`, `R10.AC7`, `R10.AC8`, `R10.AC9`, `R10.AC10`, `R10.AC11`, `R10.AC12`, `R10.AC13`, `R10.AC14`, `R10.AC15`, `R10.AC16`, `R10.AC17`, `R10.AC18`, `R10.AC19`, `R10.AC20`, `R10.AC21`, `R10.AC22`, `R10.AC23`, `R10.AC24`, `R10.AC25`, `R10.AC26`, `R10.AC27`, `R10.AC28`, `R10.AC29`, `R10.AC30`, `R10.AC31`, `R10.AC32`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `NFR9`, `NFR10`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`, `C10`, `C11`, `C12`, `C13`, `C14`, `C15`
    - Design: Testing Strategy; Verification Plan; Components And Interfaces / Canonical Helm Chart, Certificate Modes And Reload, Helm Lifecycle Hooks And Safe Uninstall, Explicit Purge Executable; Failure Modes And Tradeoffs
    - Verification:
      - command: ["make", "verify-package"]
        expect_output: "PACKAGE_VERIFY=complete STATUS=passed"
        timeout: 40m
        covers: ["R10.AC1", "R10.AC2", "R10.AC3", "R10.AC4", "R10.AC5", "R10.AC6", "R10.AC7", "R10.AC8", "R10.AC9", "R10.AC20", "R10.AC22", "R10.AC23", "R10.AC24", "R10.AC25", "R10.AC26", "R10.AC27", "R10.AC28", "R10.AC31", "NFR4", "NFR5", "NFR6", "NFR7", "NFR8", "NFR10", "C2", "C3", "C4", "C5", "C8", "C11", "C12", "C13", "C15"]
      - command: ["make", "test-package-compatibility"]
        expect_output: "PACKAGE_COMPATIBILITY=complete STATUS=passed"
        timeout: 90m
        covers: ["R10.AC10", "R10.AC11", "R10.AC12", "R10.AC13", "R10.AC14", "R10.AC15", "R10.AC16", "R10.AC17", "R10.AC18", "R10.AC19", "R10.AC21", "R10.AC29", "R10.AC30", "R10.AC32", "NFR1", "NFR2", "NFR3", "NFR4", "NFR6", "NFR8", "NFR9", "NFR10", "C1", "C3", "C4", "C5", "C6", "C7", "C9", "C10", "C11", "C14", "C15"]
