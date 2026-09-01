---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-01T19:05:43Z
last_modified: 2026-09-01T19:43:38Z
approved_fingerprint: sha256:d2dadb778a12e8875379abc4081b38c35e0768044f51e3d74140545120ce45b9
source_design_approved_at: 2026-09-01T18:53:13Z
source_design_fingerprint: sha256:516862975fc6af08a2c5e1d84a03481870bcda2b8746939cee5348cd97bfb58d
---

# Implementation Plan

<!-- assumed: incremental leaf proofs use read-only static checks and traced fake-tool acceptance; task 9.1 owns the atomic genuine kind-on-Podman proof of runtime behavior because the approved Design requires the same public lifecycle to be exercised end to end -->

- [x] 1. Establish the public command, toolchain, and sanitized process contracts
  - [x] 1.1 Add central versions, Make routing, provider helpers, preflight, and acceptance foundations
    - Move the existing Kubernetes matrix, exact kind node-image mapping, and
      cert-manager pin into `hack/toolchain.mk`; add the approved minimum
      versions for Go, Git, Make, Podman, kind, kubectl, Helm, and curl without
      changing package or E2E compatibility. Wire the eight lifecycle targets,
      constrained `local-example` target, `verify-local-environment`, and
      `test-local-environment` through exact environment inputs.
    - Add `hack/kind-podman-common.sh` with sanitized child environments,
      explicit Podman-provider kind calls, explicit kubeconfig/context wrappers,
      bounded port-forward handling, and exact OCI import primitives. Refactor
      the E2E harness to consume only these stateless helpers while preserving
      its run-unique identity and existing acceptance contract.
    - Add the action router and preflight slice of
      `hack/local-environment.sh`. Validate arguments before state creation,
      enforce Linux/amd64, report every tool version, reject missing or old
      tools and unsupported Kubernetes selections, and prove rootless Podman
      plus explicit kind provider selection before mutation. Start
      `hack/local-environment-acceptance.sh` with traced stubs covering usage,
      phase markers, credentials, host/tool/provider failures, central mapping,
      first-download errors, no mutation, and worktree preservation.
    - Requirements: `R1.AC1`, `R1.AC2`, `R1.AC3`, `R1.AC4`, `R1.AC5`, `R1.AC6`, `R1.AC7`, `R1.AC8`, `R1.AC9`, `R1.AC10`, `R1.AC11`, `R2.AC1`, `R2.AC2`, `R2.AC3`, `R2.AC4`, `R2.AC5`, `R2.AC6`, `R2.AC7`, `R2.AC8`, `R2.AC9`, `R2.AC10`, `R2.AC11`, `R2.AC12`, `NFR3`, `NFR5`, `NFR7`, `NFR9`, `C2`, `C3`, `C4`, `C9`, `C11`, `C12`
    - Design: Architecture; Components And Interfaces / Make lifecycle API and central toolchain, Shared kind/Podman process primitives; Command behavior; Error Handling; Security Considerations
    - Verification:
      - command: ["./hack/local-environment-acceptance.sh", "preflight"]
        expect_output: "LOCAL_ENVIRONMENT_ACCEPTANCE=preflight STATUS=passed"
        timeout: 20m
        covers: ["R1.AC1", "R1.AC2", "R1.AC3", "R1.AC4", "R1.AC5", "R1.AC6", "R1.AC7", "R1.AC8", "R1.AC9", "R1.AC10", "R1.AC11", "R2.AC1", "R2.AC2", "R2.AC3", "R2.AC4", "R2.AC5", "R2.AC6", "R2.AC7", "R2.AC8", "R2.AC9", "R2.AC10", "R2.AC11", "R2.AC12", "NFR3", "NFR5", "NFR7", "NFR9", "C2", "C3", "C4", "C9", "C11", "C12"]
      - command: ["./hack/e2e-harness-acceptance.sh"]
        expect_output: "E2E_HARNESS_ACCEPTANCE=complete STATUS=passed"
        timeout: 25m

- [x] 2. Implement external state, exact ownership, and safe lifecycle transitions
  - [x] 2.1 Add path validation, atomic metadata, locking, cluster creation, convergence, and deletion
    - Resolve private XDG state/cache defaults and explicit absolute overrides;
      reject repository, root, home, unresolved, and unsafe paths. Implement the
      versioned non-executable metadata parser/writer, atomic action lock,
      source/worktree snapshots, and the `absent`, `creating`, `owned`,
      `version-mismatch`, `conflict`, and `unreachable` classifications.
    - Create only `kubeseer-local` with context `kind-kubeseer-local`, explicit
      kubeconfig, selected node image, and Podman provider. Promote `creating`
      state only after kind, kubeconfig, loopback API, version, and node identity
      agree. Remove a partial current-action cluster before promotion, preserve
      an already owned cluster after convergence failure, and reject foreign or
      malformed ownership without reads or writes.
    - Implement read-only status and exact `local-down`. Validate metadata,
      kubeconfig, context, API identity, and kind membership before deletion;
      preserve state on failure, remove only matching state after confirmed
      deletion, report retained caches/images, and make an already absent target
      succeed. Extend fake acceptance with concurrency, malformed metadata,
      collisions, version mismatch, interrupted creation, repeated up/down,
      deletion failure, unrelated clusters/provider state, forbidden
      prune/purge commands, ambient credentials, and worktree drift.
    - Requirements: `R3.AC1`, `R3.AC2`, `R3.AC3`, `R3.AC4`, `R3.AC5`, `R3.AC6`, `R3.AC7`, `R3.AC8`, `R3.AC9`, `R3.AC10`, `R3.AC11`, `R3.AC12`, `R10.AC1`, `R10.AC2`, `R10.AC3`, `R10.AC4`, `R10.AC5`, `R10.AC6`, `R10.AC7`, `R10.AC8`, `R10.AC9`, `R10.AC10`, `R10.AC11`, `R10.AC12`, `R10.AC13`, `R10.AC14`, `R10.AC15`, `NFR1`, `NFR4`, `NFR6`, `C7`, `C10`
    - Design: Architecture / Lifecycle flow; Components And Interfaces / Local lifecycle orchestrator; Data Models / Local paths, Versioned ownership metadata, Ownership states; Failure Modes And Tradeoffs; Security Considerations
    - Verification:
      - command: ["./hack/local-environment-acceptance.sh", "ownership"]
        expect_output: "LOCAL_ENVIRONMENT_ACCEPTANCE=ownership STATUS=passed"
        timeout: 25m
        covers: ["R3.AC1", "R3.AC2", "R3.AC3", "R3.AC4", "R3.AC5", "R3.AC6", "R3.AC7", "R3.AC8", "R3.AC9", "R3.AC10", "R3.AC11", "R3.AC12", "R10.AC1", "R10.AC2", "R10.AC3", "R10.AC4", "R10.AC5", "R10.AC6", "R10.AC7", "R10.AC8", "R10.AC9", "R10.AC10", "R10.AC11", "R10.AC12", "R10.AC13", "R10.AC14", "R10.AC15", "NFR1", "NFR4", "NFR6", "C7", "C10"]

- [x] 3. Converge the current checkout through the canonical package
  - [x] 3.1 Add content-bound image loading, local values, cert-manager setup, and atomic Helm upgrade
    - Compute a stable source identity from revision, index, worktree, untracked
      content, Dockerfile, and build inputs. Build the production Dockerfile with
      Podman under the immutable local tag, record the exact Podman image ID,
      reuse equivalent logical identity, export/cache one OCI archive, and
      import it into every exact kind node without a registry. Stop before Helm
      on build or import failure and name every affected node.
    - Add `config/local/values.yaml` with `certManager`, exact release/namespace,
      `IfNotPresent`, managed `installation-access-ceiling`, explicit catalog
      namespaces/types, separate least-privilege observed RBAC, and no Secret or
      wildcard reads. Extend package verification to cross-check the profile
      against chart schema and the approved authorization boundaries.
    - Download or reuse only the pinned cert-manager manifest, apply it
      server-side, and wait for Established APIs and Available Deployments.
      Then run canonical `helm upgrade --install --atomic --cleanup-on-fail
      --wait` against `charts/kubeseer` with the exact local image. Extend fake
      acceptance with unchanged/changed source, build/import failures, cache,
      cert-manager failure, first install, repeated upgrade, invalid values,
      prior-revision preservation, exact ordering, and no external registry.
    - Requirements: `R4.AC1`, `R4.AC2`, `R4.AC3`, `R4.AC4`, `R4.AC5`, `R4.AC6`, `R4.AC7`, `R4.AC8`, `R4.AC9`, `R4.AC10`, `R5.AC1`, `R5.AC2`, `R5.AC3`, `R5.AC4`, `R5.AC5`, `R5.AC6`, `R5.AC7`, `R5.AC8`, `R5.AC9`, `R5.AC10`, `R5.AC11`, `R5.AC12`, `R5.AC13`, `R5.AC14`, `NFR2`, `NFR3`, `NFR9`, `C1`, `C4`, `C5`, `C6`, `C9`
    - Design: Architecture / Lifecycle flow; Components And Interfaces / Local lifecycle orchestrator, Local values profile; Data Models / Versioned ownership metadata; Error Handling; Failure Modes And Tradeoffs
    - Verification:
      - command: ["./hack/local-environment-acceptance.sh", "package"]
        expect_output: "LOCAL_ENVIRONMENT_ACCEPTANCE=package STATUS=passed"
        timeout: 30m
        covers: ["R4.AC1", "R4.AC2", "R4.AC3", "R4.AC4", "R4.AC5", "R4.AC6", "R4.AC7", "R4.AC8", "R4.AC9", "R4.AC10", "R5.AC1", "R5.AC2", "R5.AC3", "R5.AC4", "R5.AC5", "R5.AC6", "R5.AC7", "R5.AC8", "R5.AC9", "R5.AC10", "R5.AC11", "R5.AC12", "R5.AC13", "R5.AC14", "NFR2", "NFR3", "NFR9", "C1", "C4", "C5", "C6", "C9"]
      - command: ["make", "verify-package"]
        expect_output: "PACKAGE_VERIFY=complete STATUS=passed"
        timeout: 40m

- [x] 4. Build the typed local identity, readiness, and status probe
  - [x] 4.1 Add explicit clients, bounded package checks, structured status, and static boundaries
    - Add `cmd/kubeseer-local` and a narrow supporting package for
      `identity`, `readiness`, `status`, `verify`, and `diagnostics` subcommands.
      Parse only versioned metadata, load only its absolute kubeconfig, require
      the exact context, loopback server, Kubernetes version, and node identity,
      and pass context deadlines through every API or HTTP observation.
    - Implement typed/unstructured readiness for both CRDs, Deployment,
      Certificate, all webhook CA bundles, webhook endpoints, manager
      `/readyz`, metrics, and `installation-access-ceiling`. Emit a bounded
      allowlisted status projection for cluster, package, manager, policy, and
      examples; identify the exact awaited predicate on timeout and refuse any
      cluster write after identity mismatch.
    - Add `hack/verify-local-environment.sh` with read-only build, licensing,
      command-schema, import-boundary, context/deadline, structured-output,
      allowlist, and no-production-algorithm checks. Route `local-up` readiness
      and `local-status` through the probe without adding a package-local unit
      layer or a fake production behavior seam.
    - Requirements: `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R6.AC13`, `NFR4`, `NFR6`, `NFR8`, `C5`, `C11`, `C12`
    - Design: Components And Interfaces / Typed local probe; Architecture / Lifecycle flow; Data Models / Ownership states; Error Handling; Testing Strategy
    - Verification:
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-local-go-build GOMODCACHE=/tmp/kubeseer-local-go-mod ./hack/verify-local-environment.sh probe"]
        expect_output: "LOCAL_ENVIRONMENT_VERIFY=probe STATUS=passed"
        timeout: 35m
        covers: ["NFR4", "NFR6", "NFR8", "C5", "C11", "C12"]

- [x] 5. Publish the ordered executable example catalog and scoped executor
  - [x] 5.1 Add all manifests, outcomes, per-example commands, deterministic apply, and cleanup
    - Add `examples/catalog.txt` with exactly the seven approved names and one
      Apache-2.0 directory per entry. Provide purpose, workload and Kubeseer
      manifests, expected public outcome, and constrained apply/inspect/verify/
      down commands. Label or namespace every object with its stable identity
      and reject blank, duplicate, unknown, unregistered, or missing entries.
    - Implement built-in selection, typed extraction, value operator,
      cross-namespace aggregation, structural `Widget`, authorization denial,
      and partial degradation fixtures. Cross-check catalog namespaces, groups,
      and kinds against the committed local values profile. Ensure CustomResourceDefinitions precede
      Custom Resources; establish the degradable source before removing its
      fixture type; represent denial as an exact expected server-side failure
      outside active policy rather than an RBAC shortcut.
    - Add `hack/local-examples.sh` for one exact catalog name or the complete
      deterministic order. Use the identity gate, fixed field manager, and
      server-side convergence. Cleanup selects only catalog-owned resources,
      preserves release/policy, deletes fixture CRs before CRDs, and fails on
      foreign ownership. Extend static and fake acceptance for order,
      repetition, zero selection, exact denial, failure naming, and cleanup.
    - Requirements: `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R7.AC12`, `R7.AC13`, `R7.AC14`, `R7.AC15`, `R7.AC16`, `R7.AC17`, `R7.AC18`, `R7.AC19`, `R7.AC20`, `NFR2`, `NFR3`, `NFR5`, `NFR8`, `NFR9`, `C1`, `C5`, `C6`, `C11`, `C12`
    - Design: Components And Interfaces / Ordered example catalog and executor, Local values profile; Data Models; Security Considerations; Failure Modes And Tradeoffs
    - Verification:
      - command: ["./hack/local-environment-acceptance.sh", "examples"]
        expect_output: "LOCAL_ENVIRONMENT_ACCEPTANCE=examples STATUS=passed"
        timeout: 25m
        covers: ["R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "R7.AC12", "R7.AC13", "R7.AC14", "R7.AC15", "R7.AC16", "R7.AC17", "R7.AC18", "R7.AC19", "R7.AC20", "NFR2", "NFR3", "NFR5", "NFR8", "NFR9", "C1", "C5", "C6", "C11", "C12"]
      - command: ["./hack/verify-local-environment.sh", "examples"]
        expect_output: "LOCAL_ENVIRONMENT_VERIFY=examples STATUS=passed"
        timeout: 25m

- [x] 6. Complete installed-environment and example verification without weakening E2E
  - [x] 6.1 Add literal public assertions, non-vacuous reporting, and certification separation gates
    - Implement the probe's `verify` registry with exactly the catalog names.
      Use structured API clients, watches or bounded polling, and literal public
      expectations for readiness, provenance, typed values, operator output,
      deterministic aggregation, exact authorization denial, successful sibling
      preservation, and the approved degraded condition. Never import or
      reproduce discovery, selection, extraction, operator, aggregation,
      authorization, reconciliation, or status algorithms.
    - Route `local-verify` through identity, package readiness, and all example
      assertions. Emit every `EXAMPLE=<name> STATUS=passed` record and the local
      terminal marker only after all seven pass. Make missing registration,
      zero selected objects, timeout, wrong outcome, or any failed assertion
      non-zero and name the exact example plus awaited public outcome.
    - Extend boundary and fake acceptance checks for structured reads, bounded
      waits, catalog/registry equality, no skip/sleep, worktree preservation,
      required phase reporting, and public-only imports. Assert `make e2e`
      retains its run-unique disposable identity, independent kubeconfig, full
      scenario registry, and authoritative certification marker.
    - Requirements: `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `R8.AC11`, `R8.AC12`, `NFR1`, `NFR4`, `NFR8`, `NFR9`, `C7`, `C8`, `C10`, `C12`
    - Design: Components And Interfaces / Typed local probe, Ordered example catalog and executor; Testing Strategy; Verification Plan; Security Considerations
    - Verification:
      - command: ["./hack/local-environment-acceptance.sh", "verification"]
        expect_output: "LOCAL_ENVIRONMENT_ACCEPTANCE=verification STATUS=passed"
        timeout: 25m
        covers: ["R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "R8.AC10", "R8.AC11", "R8.AC12", "NFR1", "NFR4", "NFR8", "NFR9", "C7", "C8", "C10", "C12"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-local-go-build GOMODCACHE=/tmp/kubeseer-local-go-mod ./hack/verify-local-environment.sh verification"]
        expect_output: "LOCAL_ENVIRONMENT_VERIFY=verification STATUS=passed"
        timeout: 35m

- [x] 7. Add bounded sanitized diagnostics for reachable and unavailable state
  - [x] 7.1 Implement the allowlisted projection, log/event filtering, private output, and failure acceptance
    - Implement the probe's diagnostic schema and unique private destination.
      Include only approved tool/dependency versions, ownership identity,
      node/control-plane, Deployment/Pod, CRD, Certificate, webhook/endpoint,
      policy, condition/summary/hash, metric-family, stable-error, and bounded
      structured log/Event identity fields. Use the approved observability
      vocabulary and the existing sanitized Kubeseer status projection.
    - Exclude kubeconfig bytes, tokens, Secret bodies, cloud credentials,
      extracted values, result bodies, selectors, raw downstream bodies,
      arbitrary log fields, and Event messages. Bound manager records by count
      and bytes, write a manifest of completed sections, scan protected
      sentinels, report the exact external path, and perform no cluster mutation.
    - Route `local-diagnostics` through ownership before cluster reads. Preserve
      local versions/metadata with a stable unavailable observation when the API
      cannot respond; fail before cluster access when destination creation
      fails. Extend fake acceptance for reachable, unreachable, partial,
      destination, permissions, bounds, redaction, sentinel, and zero-write
      cases without serializing traced credentials.
    - Requirements: `R9.AC1`, `R9.AC2`, `R9.AC3`, `R9.AC4`, `R9.AC5`, `R9.AC6`, `R9.AC7`, `R9.AC8`, `R9.AC9`, `R9.AC10`, `R9.AC11`, `R9.AC12`, `R9.AC13`, `R9.AC14`, `R9.AC15`, `R9.AC16`, `NFR1`, `NFR2`, `NFR4`, `NFR6`, `C10`
    - Design: Components And Interfaces / Typed local probe; Data Models / Diagnostic projection; Error Handling; Security Considerations; Failure Modes And Tradeoffs
    - Verification:
      - command: ["./hack/local-environment-acceptance.sh", "diagnostics"]
        expect_output: "LOCAL_ENVIRONMENT_ACCEPTANCE=diagnostics STATUS=passed"
        timeout: 25m
        covers: ["R9.AC1", "R9.AC2", "R9.AC3", "R9.AC4", "R9.AC5", "R9.AC6", "R9.AC7", "R9.AC8", "R9.AC9", "R9.AC10", "R9.AC11", "R9.AC12", "R9.AC13", "R9.AC14", "R9.AC15", "R9.AC16", "NFR1", "NFR2", "NFR4", "NFR6", "C10"]
      - command: ["sh", "-c", "GOCACHE=/tmp/kubeseer-local-go-build GOMODCACHE=/tmp/kubeseer-local-go-mod ./hack/verify-local-environment.sh diagnostics"]
        expect_output: "LOCAL_ENVIRONMENT_VERIFY=diagnostics STATUS=passed"
        timeout: 35m

- [x] 8. Publish the contributor guide and complete read-only repository gates
  - [x] 8.1 Document the lifecycle and examples, link the README, and integrate complete static verification
    - Add `docs/local-development.md` with Linux/amd64 support and uncertified
      profiles, central/minimum versions, exact state/cluster/context/release/
      namespace identities, first-run network needs, rootless Podman/kind
      limitations, the ordered check-to-down workflow, and the separation from
      disposable `make e2e`. Document readiness, status, conditions, Events,
      logs, metrics, diagnostics, failure recovery, and exact destructive targets.
    - Complete every example README with purpose, manifests, expected public
      outcome, and constrained apply/inspect/verify/down commands using the
      project kubeconfig/context. Link the local guide from the repository README
      and keep unsupported hosts, providers, registries, external clusters,
      externalSecret mode, and global cleanup explicitly out of scope.
    - Complete `verify-local-environment` with command extraction, documentation
      coverage, licenses, shell/static safety, metadata schema, version mapping,
      chart/profile/catalog consistency, public-only probe imports, diagnostics
      allowlist, non-vacuity, and E2E separation. Add it to `make verify` and emit
      `LOCAL_ENVIRONMENT_VERIFY=complete STATUS=passed` only after all phases.
    - Requirements: `R11.AC1`, `R11.AC2`, `R11.AC3`, `R11.AC4`, `R11.AC5`, `R11.AC6`, `R11.AC7`, `R11.AC8`, `R11.AC9`, `R11.AC10`, `R11.AC11`, `R11.AC12`, `NFR5`, `NFR7`, `NFR9`, `C2`, `C9`, `C11`
    - Design: Components And Interfaces / Documentation and acceptance harness; Testing Strategy; Verification Plan; Failure Modes And Tradeoffs
    - Verification:
      - command: ["make", "verify-local-environment"]
        expect_output: "LOCAL_ENVIRONMENT_VERIFY=complete STATUS=passed"
        timeout: 45m
        covers: ["R11.AC1", "R11.AC2", "R11.AC3", "R11.AC4", "R11.AC5", "R11.AC6", "R11.AC7", "R11.AC8", "R11.AC9", "R11.AC10", "R11.AC11", "R11.AC12", "NFR5", "NFR7", "NFR9", "C2", "C9", "C11"]

- [x] 9. Certify the complete persistent local workflow on kind with rootless Podman
  - [x] 9.1 Add the genuine reusable lifecycle harness and prove every public phase atomically
    - Implement `make test-local-environment` as one Linux/amd64 genuine-cluster
      proof using temporary absolute state/cache roots and the same public Make
      semantics. Establish unrelated kind and Podman sentinels, then execute
      local-check, local-up twice, status, all examples twice, verification,
      reachable diagnostics, example cleanup, and local-down; separately prove
      unavailable diagnostics and ownership/deletion failure recovery without
      adopting an external cluster.
    - Assert every phase and example marker, exact tool/source/image/package
      identities, one cluster/release/policy, package readiness, all seven public
      outcomes, structured reads, bounded waits, diagnostics bounds and sentinel
      absence, ordered example cleanup, retained image/cache state, removal of
      only `kubeseer-local`, preservation of unrelated provider resources, and
      byte-identical repository state. Fail zero matches, skips, missing phases,
      leaks, retained owned cluster, or any non-pass outcome.
    - Run the complete read-only repository gate before the cluster proof and
      retain `make e2e` unchanged as the separate full-product certification.
      Emit `LOCAL_ENVIRONMENT_ACCEPTANCE=complete STATUS=passed` only after final
      cleanup and worktree comparison. Document the explicit socket/network and
      time requirements of this proof without introducing a cloud fallback.
    - Requirements: `R6.AC1`, `R6.AC2`, `R6.AC3`, `R6.AC4`, `R6.AC5`, `R6.AC6`, `R6.AC7`, `R6.AC8`, `R6.AC9`, `R6.AC10`, `R6.AC11`, `R6.AC12`, `R6.AC13`, `R7.AC1`, `R7.AC2`, `R7.AC3`, `R7.AC4`, `R7.AC5`, `R7.AC6`, `R7.AC7`, `R7.AC8`, `R7.AC9`, `R7.AC10`, `R7.AC11`, `R7.AC12`, `R7.AC13`, `R7.AC14`, `R7.AC15`, `R7.AC16`, `R7.AC17`, `R7.AC18`, `R7.AC19`, `R7.AC20`, `R8.AC1`, `R8.AC2`, `R8.AC3`, `R8.AC4`, `R8.AC5`, `R8.AC6`, `R8.AC7`, `R8.AC8`, `R8.AC9`, `R8.AC10`, `R8.AC11`, `R8.AC12`, `R9.AC1`, `R9.AC2`, `R9.AC3`, `R9.AC4`, `R9.AC5`, `R9.AC6`, `R9.AC7`, `R9.AC8`, `R9.AC9`, `R9.AC10`, `R9.AC11`, `R9.AC12`, `R9.AC13`, `R9.AC14`, `R9.AC15`, `R9.AC16`, `NFR1`, `NFR2`, `NFR3`, `NFR4`, `NFR5`, `NFR6`, `NFR7`, `NFR8`, `NFR9`, `C1`, `C2`, `C3`, `C4`, `C5`, `C6`, `C7`, `C8`, `C9`, `C10`, `C11`, `C12`
    - Design: Overview; Architecture; Components And Interfaces; Data Models; Error Handling; Security Considerations; Failure Modes And Tradeoffs; Testing Strategy; Verification Plan
    - Verification:
      - command: ["make", "verify-local-environment"]
        expect_output: "LOCAL_ENVIRONMENT_VERIFY=complete STATUS=passed"
        timeout: 45m
      - command: ["make", "test-local-environment"]
        expect_output: "LOCAL_ENVIRONMENT_ACCEPTANCE=complete STATUS=passed"
        timeout: 120m
        covers: ["R6.AC1", "R6.AC2", "R6.AC3", "R6.AC4", "R6.AC5", "R6.AC6", "R6.AC7", "R6.AC8", "R6.AC9", "R6.AC10", "R6.AC11", "R6.AC12", "R6.AC13", "R7.AC1", "R7.AC2", "R7.AC3", "R7.AC4", "R7.AC5", "R7.AC6", "R7.AC7", "R7.AC8", "R7.AC9", "R7.AC10", "R7.AC11", "R7.AC12", "R7.AC13", "R7.AC14", "R7.AC15", "R7.AC16", "R7.AC17", "R7.AC18", "R7.AC19", "R7.AC20", "R8.AC1", "R8.AC2", "R8.AC3", "R8.AC4", "R8.AC5", "R8.AC6", "R8.AC7", "R8.AC8", "R8.AC9", "R8.AC10", "R8.AC11", "R8.AC12", "R9.AC1", "R9.AC2", "R9.AC3", "R9.AC4", "R9.AC5", "R9.AC6", "R9.AC7", "R9.AC8", "R9.AC9", "R9.AC10", "R9.AC11", "R9.AC12", "R9.AC13", "R9.AC14", "R9.AC15", "R9.AC16", "NFR1", "NFR2", "NFR3", "NFR4", "NFR5", "NFR6", "NFR7", "NFR8", "NFR9", "C1", "C2", "C3", "C4", "C5", "C6", "C7", "C8", "C9", "C10", "C11", "C12"]
