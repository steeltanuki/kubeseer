---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-08-01T09:10:17Z
last_modified: 2026-08-01T09:10:17Z
approved_fingerprint: sha256:9d7a86cb0435c9e87face9b1879020937311f8ad72543ac974be4a86bad87657
source_requirements_approved_at: 2026-08-01T08:45:29Z
source_requirements_fingerprint: sha256:8674f24413fb506b85abe9bebb31125b9f993b8fcead42824ed9b83add21b42a
---

# Kubeseer API Foundation Design

## Overview

The API foundation will use a minimal Kubebuilder-compatible Go layout in which versioned Go types are the source of truth and `controller-gen` produces the CRD and deep-copy code. The feature creates no controller, webhook, RBAC policy, packaging layer, discovery client, or business logic.

The public contract is a namespace-scoped `kubeseer.io/v1alpha1` resource with an explicitly required `spec`, an optional list of source envelopes containing only stable identifiers, and a status subresource containing `observedGeneration`, Kubernetes conditions, and an empty typed result envelope ready for later features.

The selected dependency baseline is Go 1.26, controller-runtime v0.24.1, its Kubernetes v0.36 dependency family, and controller-tools v0.20.1. Controller-runtime maps its v0.24 line to Kubernetes/client-go v0.36 and Go 1.26; compatibility with the approved Kubernetes 1.35 minimum is demonstrated separately by running the generated CRD against both Kubernetes 1.35 and 1.36 API servers. [controller-runtime compatibility](https://github.com/kubernetes-sigs/controller-runtime#compatibility), [controller-runtime v0.24.1](https://github.com/kubernetes-sigs/controller-runtime/releases/tag/v0.24.1), [controller-tools v0.20.1](https://github.com/kubernetes-sigs/controller-tools/releases/tag/v0.20.1)

<!-- assumed: the Go module path is github.com/steeltanuki/kubeseer (source: repository origin remote) -->
<!-- assumed: controller-runtime v0.24.1 is the dependency baseline because it matches the newest supported Kubernetes minor while API-server compatibility tests retain the approved 1.35 minimum (source: controller-runtime compatibility table and approved NFR1) -->
<!-- assumed: spec.sources uses Kubernetes list-map semantics keyed by id (source: .walden/constitution.md stable and unique source identifiers) -->
<!-- assumed: status.result begins as an empty typed object rather than arbitrary JSON (source: C4 and NFR5) -->

## Architecture

The feature has one authoritative input, two generated outputs, and a real-API-server verification boundary:

```text
api/v1alpha1 Go types and markers
          │
          ├── controller-gen object ──> zz_generated.deepcopy.go
          │
          └── controller-gen crd ─────> config/crd/bases/kubeseer.io_kubeseers.yaml
                                              │
                                              ├── envtest Kubernetes 1.35.6
                                              └── envtest Kubernetes 1.36.2
```

`api/v1alpha1` owns serialization names, validation markers, group/version registration, and runtime scheme registration. Generated artifacts are committed and reviewed, but never edited by hand. Verification regenerates them in a temporary copy outside the worktree and compares the result, keeping Walden proof execution read-only.

The CRD uses `apiextensions.k8s.io/v1`, a structural OpenAPI v3 schema, `scope: Namespaced`, one served and storage version (`v1alpha1`), and the status subresource. Kubernetes requires v1 CRDs to use structural schemas; unknown arbitrary payload preservation is therefore not used for `spec` or `status`. [Kubernetes CRD documentation](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/)

## Options Considered

### Option A — Minimal Kubebuilder-Compatible API Package

- Summary: Define versioned Go types and Kubebuilder markers, generate deep-copy code and the CRD with pinned controller-tools, and test the contract with envtest.
- Why chosen: One source of truth keeps Go clients and the installed OpenAPI schema aligned. Official markers directly express resource scope, status subresource, requiredness, patterns, lengths, and numeric minimums. [CRD generation markers](https://book.kubebuilder.io/reference/markers/crd), [validation markers](https://book.kubebuilder.io/reference/markers/crd-validation.html)
- Scope control: Create only the API package, generated CRD, generation tooling, and tests; defer the manager and reconciler.

### Option B — Hand-Written CRD Plus Independent Go Types

- Summary: Maintain the YAML schema and Go structs separately.
- Why rejected: It duplicates the public contract and makes drift between serialization, validation, and clients likely. Reproducibility checks become comparison logic between two authored definitions instead of a deterministic generation check.

### Option C — Opaque Extension Payloads

- Summary: Use `runtime.RawExtension` or `x-kubernetes-preserve-unknown-fields` for sources and results so later features can place arbitrary data there.
- Why rejected: It weakens structural validation, conflicts with `C4`, and delays public-contract errors until controller runtime. An empty typed result object can be extended compatibly without admitting arbitrary data.

### Dependency Alternative — controller-runtime v0.23

- Summary: Align client libraries with Kubernetes 1.35 and Go 1.25.
- Why rejected: It aligns with the minimum API server but not the newest supported minor. v0.24.1 provides the current v0.36 client family while compatibility tests independently prove that the CRD works on 1.35.

## Simplicity And Elegance Review

- Simplest viable shape: Five authored concerns are sufficient: module/tool pins, the API package, the generated CRD, focused tests, and read-only generation verification.
- Coupling check: API types depend only on Kubernetes API machinery and controller-runtime scheme helpers; there is no dependency on future discovery, policy, extraction, aggregation, or reconciler packages.
- Generator check: Go types remain the only authored schema. Generated files are committed evidence, not a second design surface.
- Runtime check: No manager binary is added because this feature proves an API, not a running controller.
- Future-proofing: Later features extend `KubeseerSource`, `KubeseerResult`, and status types with optional typed fields. Incompatible semantics require a new API version rather than a marker trick.
- Deferred complexity: Conversion webhooks, defaulting webhooks, CEL validation, printer columns, scale subresources, finalizers, and controller watches remain outside this feature.

## Components And Interfaces

### Toolchain And Module Boundary

- Purpose: Pin the Go and Kubernetes API generation toolchain used by all generated artifacts.
- Files: `go.mod`, `go.sum`, `Makefile`, and `hack/verify-generated.sh`.
- Contract: Go 1.26; controller-runtime v0.24.1; controller-tools v0.20.1; setup-envtest from the controller-runtime v0.24 line.
- Commands: `make build`, `make test`, `make generate`, `make manifests`, `make verify`, and `make test-api`.
- Reproducibility: Tool versions live in version-controlled configuration. `make verify` writes only to a temporary directory and fails on any generated diff.
- Requirements: `R6`, `R7`, `NFR1`, `NFR3`.

### Versioned API Package

- Purpose: Define the public Go and JSON contract for `kubeseer.io/v1alpha1`.
- Files: `api/v1alpha1/doc.go`, `groupversion_info.go`, `kubeseer_types.go`, and generated `zz_generated.deepcopy.go`.
- Inputs: Authored Go structs, JSON tags, package markers, resource markers, and validation markers.
- Outputs: Runtime objects, `GroupVersion`, `SchemeBuilder`, `AddToScheme`, and generated deep-copy methods.
- Dependencies: `k8s.io/apimachinery/pkg/apis/meta/v1`, `k8s.io/apimachinery/pkg/runtime/schema`, and `sigs.k8s.io/controller-runtime/pkg/scheme`.
- Requirements: `R1`, `R2`, `R3`, `R4`, `R5`, `R6`, `NFR2`, `NFR4`, `NFR5`.

### Generated CRD

- Purpose: Materialize the API contract for Kubernetes API servers.
- File: `config/crd/bases/kubeseer.io_kubeseers.yaml`.
- Inputs: The versioned API package and pinned `controller-gen`.
- Outputs: A namespace-scoped structural CRD with `v1alpha1` served/storage flags, OpenAPI validation, and `/status`.
- Ownership: Generated only; manual edits fail `make verify`.
- Requirements: `R1`, `R2`, `R3`, `R4`, `R5`, `R7`, `NFR3`, `NFR5`.

### API Contract Test Harness

- Purpose: Prove the generated schema and client contract without introducing a controller.
- Unit boundary: Scheme registration, JSON round trips, and deep-copy isolation.
- Static boundary: Parse the committed CRD as an `apiextensionsv1.CustomResourceDefinition` and inspect identity, scope, versions, schema, and subresources.
- Integration boundary: Start envtest with pinned Kubernetes assets, install the CRD, and exercise typed and unstructured API requests.
- Compatibility matrix: Kubernetes 1.35.6 and 1.36.2. These are centrally pinned initial patches; upgrading either pin is a reviewed dependency change. Kubernetes identifies 1.35 and 1.36 as supported release lines. [Kubernetes patch releases](https://kubernetes.io/releases/patch-releases/)
- Requirements: `R1`, `R2`, `R3`, `R4`, `R5`, `R6`, `R7`, `NFR1`, `NFR5`.

## Data Models

### API Registration

```go
var GroupVersion = schema.GroupVersion{
    Group:   "kubeseer.io",
    Version: "v1alpha1",
}

var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
var AddToScheme = SchemeBuilder.AddToScheme
```

Package initialization registers `Kubeseer` and `KubeseerList`. The CRD type carries explicit resource, storage-version, object-root, and status-subresource markers.

### Root Resources

```go
type Kubeseer struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec              KubeseerSpec   `json:"spec"`
    Status            KubeseerStatus `json:"status,omitempty"`
}

type KubeseerList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []Kubeseer `json:"items"`
}
```

`Spec` is a non-pointer, non-`omitempty` field and is explicitly marked required. `Status` is omitted when empty and is writable through the status subresource. No scale or additional-printer-column markers are added.

### Spec And Source Envelope

```go
type KubeseerSpec struct {
    Sources []KubeseerSource `json:"sources,omitempty"`
}

type KubeseerSource struct {
    ID string `json:"id"`
}
```

`Sources` is optional and has no default. It uses `+listType=map` and `+listMapKey=id` so stable identifiers define merge identity and duplicates are rejected by Kubernetes list-map semantics. `ID` is required with maximum length 63 and pattern `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`. No resource selector, namespace, field, policy, permission, or processing field exists yet.

### Status And Result Envelope

```go
type KubeseerStatus struct {
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    Result             *KubeseerResult    `json:"result,omitempty"`
}

type KubeseerResult struct{}
```

`ObservedGeneration` has a minimum of zero; zero means no generation has been observed. `Conditions` uses Kubernetes map-list semantics keyed by condition `type`. `Result` is a pointer so absence and an explicitly present empty envelope remain distinguishable. The empty result schema is structural and prunes unknown fields rather than preserving arbitrary JSON.

## Schema And Serialization Rules

- Package marker `+groupName=kubeseer.io` establishes the API group.
- Resource marker declares `path=kubeseers`, `singular=kubeseer`, and `scope=Namespaced`; no short name is introduced.
- `+kubebuilder:subresource:status` generates the status endpoint and spec/status write isolation.
- `+kubebuilder:storageversion` records `v1alpha1` as the initial storage version.
- Required and optional fields use explicit markers in addition to JSON tags, avoiding generator-default ambiguity.
- No `+kubebuilder:default` marker is present.
- No `PreserveUnknownFields`, embedded arbitrary resource, or raw JSON field is present.
- All public JSON names use lower camel case.
- Deep-copy code is generated for every runtime object and nested mutable slice.

## API Evolution Strategy

- Compatible `v1alpha1` additions are optional typed fields, or fields with an API-server default defined in an approved upstream specification.
- Existing serialized names and meanings are not repurposed.
- A change that cannot preserve meaning creates a new version package such as `api/v1beta1`; the old version remains served until a conversion and migration strategy is approved.
- Conversion infrastructure is introduced only with a second version. The single-version foundation needs no conversion webhook or internal hub type.
- Generated CRDs must always name exactly one storage version.

## Error Handling

- Invalid CRD generation fails the generator command and produces no accepted change.
- A CRD rejected as non-structural or not Established within the test context deadline fails compatibility verification with the API-server error and target version.
- Missing `spec`, invalid source identifiers, non-list `sources`, and negative `observedGeneration` are rejected by OpenAPI validation before persistence.
- An unserved Kubeseer version fails through Kubernetes API discovery/request handling; no custom error layer is added.
- Status tests use the status client path and assert that attempted spec changes do not persist.
- Tests report the Kubernetes asset version in failure output so version-specific drift is diagnosable.

## Security Considerations

- The foundation contains no policy reference, permission grant, bypass flag, impersonation field, or arbitrary read target.
- Source envelopes carry identity only; observed resource coordinates arrive in later reviewed features.
- Structural schemas and pruning prevent unreviewed payloads from becoming hidden API surface.
- Status remains a separate subresource so RBAC can distinguish user-authored configuration from controller-authored observations in later packaging work.
- No Secret data, resource contents, or extracted values exist in this feature or its test diagnostics.

## Failure Modes And Tradeoffs

- Failure mode: controller-runtime v0.24 uses Kubernetes v0.36 libraries while the minimum server is 1.35.
  - Mitigation: Run the same CRD contract suite against real 1.35.6 and 1.36.2 envtest API servers.
  - Tradeoff: One current client dependency set is maintained instead of parallel builds for each server minor.
- Failure mode: An empty typed `KubeseerResult` cannot carry future result fields today.
  - Mitigation: Later features add optional typed fields before they write results.
  - Tradeoff: Early arbitrary status payloads are deliberately rejected or pruned.
- Failure mode: Generated artifacts drift after marker or tool changes.
  - Mitigation: `make verify` regenerates in a temporary copy and compares all generated paths.
  - Tradeoff: Generated files remain committed, increasing diff size but making the installed contract reviewable.
- Failure mode: envtest assets are unavailable or a CRD never establishes.
  - Mitigation: Pin assets centrally, use a bounded context, and surface setup/API-server logs.
  - Tradeoff: Compatibility proof requires downloading two API-server asset sets.
- Failure mode: Source list-map semantics make duplicate IDs invalid earlier than later admission logic.
  - Mitigation: Treat uniqueness as a stable API invariant from the constitution.
  - Tradeoff: Duplicate IDs cannot be accepted temporarily for controller-side diagnostics.

## Testing Strategy

### Unit Tests

- Register `Kubeseer` and `KubeseerList` into a fresh scheme and resolve their GVKs.
- Serialize and deserialize representative objects, asserting API identity, lower-camel-case fields, omitted optional sources, zero/default behavior, conditions, and result presence.
- Deep-copy objects containing sources and conditions, mutate the copy, and prove the original does not alias mutable fields.

### Generated Contract Tests

- Parse the committed CRD into the typed apiextensions API.
- Assert group, plural, singular, kind, namespace scope, served/storage flags, structural schema, required `spec`, optional `sources`, source ID validation, status schema, and status subresource.
- Assert that neither `spec` nor `status` enables unknown-field preservation and that no default values are emitted.

### Envtest Compatibility Tests

Run one shared suite with Kubernetes 1.35.6 assets and again with 1.36.2 assets. controller-runtime recommends envtest with a real API server for API behavior rather than relying on a fake client. [controller-runtime testing guidance](https://github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md)

The suite will:

1. start envtest and install the generated CRD;
2. wait with a bounded context for `Established=True`;
3. persist a minimal `{spec: {}}` resource;
4. persist a resource containing a valid source ID;
5. reject a missing `spec`;
6. reject an invalid source ID;
7. reject a non-list `spec.sources` through an unstructured request;
8. reject a negative `status.observedGeneration`;
9. update status and prove the stored spec is unchanged;
10. reject a request to an unserved Kubeseer API version.

## Verification Plan

- Requirement proof: Focused Go tests prove scheme registration, round-trip serialization, deep-copy isolation, and marker-derived CRD structure for `R1`–`R6`.
- Compatibility proof: The same envtest contract suite runs with pinned Kubernetes 1.35.6 and 1.36.2 assets for `R1`, `R2`, `R3`, `R4`, and `R7`.
- Generation proof: `make verify` regenerates deep-copy and CRD artifacts outside the repository and fails on any diff for `R6` and `NFR3`.
- Build proof: `go build ./...` demonstrates that the public Go API compiles without a controller binary.
- Test evidence: Proof commands must assert named test output so a pattern matching zero tests cannot pass vacuously.
- Operational evidence: No runtime controller evidence is required; envtest API-server logs and the target Kubernetes version are retained in test output on failure.

## Requirement Coverage

| Requirement | Covered By |
| --- | --- |
| `R1` | Versioned API Package; Generated CRD; Envtest Compatibility Tests |
| `R2` | Spec And Source Envelope; Generated Contract Tests; Security Considerations |
| `R3` | Spec And Source Envelope; Schema And Serialization Rules; Envtest Compatibility Tests |
| `R4` | Status And Result Envelope; Generated CRD; status-subresource envtest cases |
| `R5` | API Registration; API Evolution Strategy; round-trip unit tests |
| `R6` | Toolchain And Module Boundary; Versioned API Package; Generation proof |
| `R7` | API Contract Test Harness; Envtest Compatibility Tests; Compatibility proof |
| `NFR1` | Dependency baseline; pinned Kubernetes 1.35.6 and 1.36.2 matrix |
| `NFR2` | API Evolution Strategy; typed extension points; serialization tests |
| `NFR3` | Pinned generators; temporary-copy `make verify`; generated artifact review |
| `NFR4` | Minimal source envelope; no permission or policy-override fields; Security Considerations |
| `NFR5` | Structural OpenAPI schema; `metav1.Condition`; generated and envtest contract tests |
