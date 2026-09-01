---
walden_schema_version: v1alpha1
status: approved
approved_at: 2026-09-01T06:12:03Z
last_modified: 2026-09-01T06:12:03Z
approved_fingerprint: sha256:2c70a2672efedbd212d151c0b2dbaf1efaa069e0598ae5f216504e4962c98084
---

# Requirements Document

## Introduction

Kubeseer has approved runtime, authorization, admission, observability, and
resource-limit contracts, but it does not yet have a production entrypoint or
a supported installation package. This feature defines the externally
verifiable contract for building the controller image, packaging one canonical
Helm chart, installing the operator with least-privilege permissions,
bootstrapping a fail-closed access policy, operating the validating webhook,
upgrading without losing user state, and uninstalling without accidental data
loss.

The installation package owns Kubernetes deployment topology and lifecycle. It
does not weaken the logical `KubeseerAccessPolicy` boundary, grant Kubeseer
authors direct access to observed resources, provision a Kubernetes cluster, or
certify the product's end-to-end observation scenarios.

<!-- decided: Helm is the canonical initial packaging format (source: explicit user decision during Requirements review) -->
<!-- assumed: the default Helm release name is kubeseer and the default installation namespace is kubeseer-system, while both remain configurable through documented Helm CLI inputs -->
<!-- assumed: only one active Kubeseer Helm release is supported per cluster because the installation policy, CRDs, and webhook registration are cluster-scoped singletons (source: approved API and admission contracts) -->
<!-- assumed: the bootstrap installation policy is deny-all until an administrator explicitly declares observable namespaces and resource types, preserving the constitution's fail-closed security boundary -->
<!-- decided: cert-manager is the default automated certificate lifecycle and externalSecret is the explicit fallback when cert-manager cannot be installed (source: explicit user decision during Requirements replanning) -->
<!-- assumed: certificate mode is selected only through Helm values; the chart does not use cluster lookup to change rendered output implicitly, preserving deterministic offline rendering -->
<!-- assumed: normal uninstall preserves CRDs and custom resources; irreversible CRD and instance deletion requires a separate explicit purge path -->

## Requirements

### R1 Canonical Helm Chart And Deterministic Rendering

**User Story:** As a cluster administrator, I want one canonical Helm chart, so
that installation values and rendered Kubernetes objects are
reviewable, reproducible, and suitable for GitOps workflows.

#### Acceptance Criteria

1. `R1.AC1` The system SHALL provide one canonical Helm application chart rooted at `charts/kubeseer`.
2. `R1.AC2` The system SHALL package verified generated CRDs under the chart's `crds/` directory.
3. `R1.AC3` WHEN an administrator runs the documented `helm template` command with CRDs included, the system SHALL emit a complete Kubernetes object stream without contacting a cluster.
4. `R1.AC4` WHEN equivalent chart version, release name, namespace, and values are rendered repeatedly, the system SHALL emit byte-for-byte equivalent object content in deterministic order.
5. `R1.AC5` WHEN an administrator supplies a non-default Helm release namespace, the system SHALL place every namespace-scoped chart object in that namespace.
6. `R1.AC6` WHEN an administrator supplies image repository and version values, the system SHALL render that exact image reference into the manager workload.
7. `R1.AC7` IF an image reference resolves to the mutable `latest` tag, THEN the system SHALL reject the production package verification.
8. `R1.AC8` The system SHALL apply one stable app identity label set to every package-owned object that supports labels.
9. `R1.AC9` The system SHALL annotate or label every package-owned object sufficiently to distinguish Kubeseer ownership from unrelated cluster resources.
10. `R1.AC10` IF a required Helm value is missing or malformed, THEN the system SHALL fail schema validation before applying any object.
11. `R1.AC11` WHEN an administrator repeats the documented `helm upgrade --install` command with equivalent inputs, the system SHALL converge without creating duplicate logical resources.
12. `R1.AC12` The system SHALL exclude environment-specific credentials and private keys from committed package content.
13. `R1.AC13` The system SHALL declare Helm chart API version `v2` in `Chart.yaml`.
14. `R1.AC14` The system SHALL declare a Semantic Versioning chart version in `Chart.yaml`.
15. `R1.AC15` The system SHALL declare the default manager application version as `appVersion` in `Chart.yaml`.
16. `R1.AC16` The system SHALL provide documented default configuration in `values.yaml`.
17. `R1.AC17` The system SHALL validate every supported public chart value through `values.schema.json`.
18. `R1.AC18` WHEN the documented chart lint command runs, the system SHALL complete `helm lint` with exit code zero.
19. `R1.AC19` WHEN the documented chart packaging command runs, the system SHALL produce a versioned `.tgz` chart archive.
20. `R1.AC20` IF another active Kubeseer Helm release already owns the cluster-scoped installation objects, THEN the system SHALL reject a second installation without adopting or overwriting that release.
21. `R1.AC21` The system SHALL use standard Helm and Kubernetes application labels on every chart-owned object that supports labels.
22. `R1.AC22` The system SHALL declare the approved Kubernetes compatibility range through `kubeVersion` in `Chart.yaml`.
23. `R1.AC23` IF the target cluster version falls outside the declared `kubeVersion` range, THEN the system SHALL reject installation or upgrade before applying chart resources.
24. `R1.AC24` The system SHALL select certificate lifecycle resources exclusively from validated Helm values without cluster-dependent template lookup.

### R2 Controller Image And Production Entrypoint

**User Story:** As a platform operator, I want a minimal production image with
one explicit manager entrypoint, so that the deployed process starts all
approved Kubeseer runtime boundaries consistently.

#### Acceptance Criteria

1. `R2.AC1` WHEN the documented image build command completes, the system SHALL produce an OCI-compatible Linux controller image.
2. `R2.AC2` The system SHALL compile the manager executable from committed Go source in the repository.
3. `R2.AC3` The system SHALL start the image through the compiled manager executable without a shell entrypoint.
4. `R2.AC4` WHEN the manager starts, the system SHALL register the `kubeseer.io/v1alpha1` API types with its runtime scheme.
5. `R2.AC5` WHEN the manager starts, the system SHALL register the approved reconciliation runtime with the controller-runtime manager.
6. `R2.AC6` WHEN the manager starts, the system SHALL register both approved validating admission endpoints with the controller-runtime webhook server.
7. `R2.AC7` WHEN the manager starts, the system SHALL expose the controller-runtime health endpoint on its configured bind address.
8. `R2.AC8` WHEN the manager starts, the system SHALL expose the approved controller-runtime Prometheus registry on its configured metrics bind address.
9. `R2.AC9` WHEN the manager receives a termination signal, the system SHALL stop through controller-runtime signal handling within the configured termination grace period.
10. `R2.AC10` IF startup configuration is invalid, THEN the system SHALL exit non-zero before starting reconciliation workers.
11. `R2.AC11` IF manager construction or runtime registration fails, THEN the system SHALL exit non-zero before reporting readiness.
12. `R2.AC12` The system SHALL include an immutable version identifier in the final image metadata.
13. `R2.AC13` The system SHALL run the production executable as a non-root numeric user.
14. `R2.AC14` The system SHALL omit package managers, compilers, source code, and shell utilities from the final runtime image.
15. `R2.AC15` WHEN the manager starts, the system SHALL expose the controller-runtime readiness endpoint on its configured bind address.
16. `R2.AC16` WHEN an administrator runs the documented version command against the executable, the system SHALL report its immutable version identifier.

### R3 Manager Workload, Configuration, And Readiness

**User Story:** As a cluster administrator, I want a secure and configurable
manager workload, so that Kubeseer can be operated predictably without editing
generated resources.

#### Acceptance Criteria

1. `R3.AC1` The system SHALL deploy the manager as one Kubernetes `Deployment` in the selected installation namespace.
2. `R3.AC2` The system SHALL assign the manager a dedicated Kubernetes `ServiceAccount`.
3. `R3.AC3` The system SHALL enable leader election for controller ownership.
4. `R3.AC4` The system SHALL default to one manager replica.
5. `R3.AC5` WHEN an administrator changes the supported replica setting, the system SHALL preserve single-leader reconciliation semantics.
6. `R3.AC6` The system SHALL define non-zero default CPU and memory requests for the manager container.
7. `R3.AC7` The system SHALL define finite default CPU and memory limits for the manager container.
8. `R3.AC8` WHEN an administrator supplies supported resource overrides through Helm values, the system SHALL render those values without modifying chart templates.
9. `R3.AC9` The system SHALL configure a startup or readiness probe that remains unsuccessful until manager initialization is complete.
10. `R3.AC10` The system SHALL configure a liveness probe that detects a manager process unable to serve its health endpoint.
11. `R3.AC11` The system SHALL configure a bounded termination grace period for the manager Pod.
12. `R3.AC12` The system SHALL prevent privilege escalation in the manager container security context.
13. `R3.AC13` The system SHALL drop every Linux capability from the manager container.
14. `R3.AC14` The system SHALL use the runtime-default seccomp profile for the manager Pod.
15. `R3.AC15` The system SHALL mount the manager root filesystem read-only except for explicitly declared writable volumes.
16. `R3.AC16` The system SHALL avoid host networking for the manager Pod.
17. `R3.AC17` The system SHALL keep metrics reachable only through cluster-local networking by default.
18. `R3.AC18` WHEN tracing export is not configured, the system SHALL start without requiring an external tracing backend.
19. `R3.AC19` IF the manager Pod cannot load required configuration or TLS material, THEN the system SHALL remain unready.
20. `R3.AC20` WHEN the documented installation command returns success, the system SHALL have observed the manager Deployment become available within a bounded timeout.
21. `R3.AC21` The system SHALL avoid host PID and host IPC namespaces for the manager Pod.
22. `R3.AC22` The system SHALL avoid privileged mode for the manager container.
23. `R3.AC23` The system SHALL avoid host-path mounts for the manager Pod.
24. `R3.AC24` The system SHALL provide one cluster-local Service for scraping the approved manager metrics endpoint.

### R4 CRD Installation And API Lifecycle

**User Story:** As a cluster administrator, I want CRDs managed safely across
installation and upgrades, so that existing Kubeseer resources are not lost or
silently reinterpreted.

#### Acceptance Criteria

1. `R4.AC1` The system SHALL package the generated `Kubeseer` CRD for `kubeseer.io/v1alpha1`.
2. `R4.AC2` The system SHALL package the generated cluster-scoped `KubeseerAccessPolicy` CRD for `kubeseer.io/v1alpha1`.
3. `R4.AC3` WHEN `helm install` begins on a cluster without Kubeseer CRDs, the system SHALL establish both CRDs before creating templated manager resources.
4. `R4.AC4` WHEN installation completes, the system SHALL expose both approved API resources through Kubernetes discovery.
5. `R4.AC5` The system SHALL install CRD schemas directly from the repository's verified generated artifacts.
6. `R4.AC6` The system SHALL preserve the approved CRD scopes, status subresources, defaults, list semantics, and validation rules.
7. `R4.AC7` WHEN a chart upgrade includes a compatible CRD schema change, the system SHALL require the documented CRD upgrade command before `helm upgrade` rolls out a dependent manager.
8. `R4.AC8` WHEN a compatible CRD upgrade is applied, the system SHALL preserve existing custom-resource instances.
9. `R4.AC9` IF a package contains generated CRDs that differ from a fresh repository generation, THEN the system SHALL fail package verification.
10. `R4.AC10` IF an upgrade requires an unapproved storage-version conversion or destructive schema change, THEN the system SHALL reject the supported upgrade path.
11. `R4.AC11` WHEN normal uninstall runs, the system SHALL preserve both CRDs.
12. `R4.AC12` WHEN normal uninstall runs, the system SHALL preserve every `Kubeseer` and `KubeseerAccessPolicy` instance.
13. `R4.AC13` The system SHALL keep both CRDs outside the chart's `templates/` directory.
14. `R4.AC14` The system SHALL document that Helm does not upgrade CRDs placed under the chart's `crds/` directory.
15. `R4.AC15` IF the CRD upgrade preflight detects an incompatible schema transition, THEN the system SHALL stop before invoking `helm upgrade`.

### R5 RBAC And Permission Separation

**User Story:** As a security administrator, I want installation RBAC to keep
operator identity, logical observation policy, and author permissions separate,
so that deployment convenience cannot broaden delegated data access.

#### Acceptance Criteria

1. `R5.AC1` The system SHALL represent operator ServiceAccount permissions separately from `KubeseerAccessPolicy` configuration in rendered resources.
2. `R5.AC2` The system SHALL represent Kubeseer-author permissions separately from operator ServiceAccount permissions in rendered RBAC.
3. `R5.AC3` The system SHALL grant the operator ServiceAccount only the verbs required to reconcile Kubeseer-owned APIs, publish status, emit Events, and hold leader-election leases.
4. `R5.AC4` The system SHALL omit wildcard write verbs from the operator's default RBAC.
5. `R5.AC5` The system SHALL omit wildcard observed-resource rules from the operator's default RBAC.
6. `R5.AC6` The system SHALL grant no permission to read Kubernetes `Secret` resources in the default installation.
7. `R5.AC7` WHEN an administrator configures namespaced observed-resource permissions, the system SHALL scope those permissions to the selected namespaces through namespace-scoped bindings.
8. `R5.AC8` WHEN an administrator configures observed-resource permissions, the system SHALL limit them to the API groups and resources explicitly declared for the ServiceAccount.
9. `R5.AC9` WHEN an administrator enables observation of cluster-scoped resources, the system SHALL require an explicit cluster-scoped RBAC grant.
10. `R5.AC10` WHEN an administrator enables observation of cluster-scoped resources, the system SHALL require `allowClusterScoped` in the logical installation policy independently of RBAC.
11. `R5.AC11` WHEN ServiceAccount RBAC is broader than the active installation policy, the system SHALL continue to enforce the narrower logical policy before every observed-resource read.
12. `R5.AC12` WHEN the active installation policy is broader than ServiceAccount RBAC, the system SHALL expose the approved sanitized runtime RBAC failure without granting additional permission.
13. `R5.AC13` The system SHALL provide a reusable namespaced Kubeseer-author Role for managing `Kubeseer` resources.
14. `R5.AC14` The system SHALL withhold permission to create, update, patch, or delete `KubeseerAccessPolicy` from the default Kubeseer-author role.
15. `R5.AC15` The system SHALL withhold permission to modify operator RBAC, webhook registration, or the manager Deployment from the default Kubeseer-author role.
16. `R5.AC16` IF required operator RBAC is absent, THEN the system SHALL fail the documented installation readiness check with the missing permission identified.
17. `R5.AC17` The system SHALL document operator ServiceAccount permissions separately from logical access-policy permissions.
18. `R5.AC18` The system SHALL document Kubeseer-author permissions separately from operator ServiceAccount permissions.
19. `R5.AC19` The system SHALL grant no observed-resource read permission through the default Kubeseer-author Role.

### R6 Installation Access Policy Bootstrap

**User Story:** As a cluster administrator, I want a safe initial observation
ceiling and explicit customization points, so that a fresh installation cannot
observe workload data until I authorize it.

#### Acceptance Criteria

1. `R6.AC1` The system SHALL provide a bootstrap manifest for the canonical `KubeseerAccessPolicy` named `installation-access-ceiling`.
2. `R6.AC2` The system SHALL configure the unmodified bootstrap policy with `Explicit` namespace mode.
3. `R6.AC3` The system SHALL configure the unmodified bootstrap policy with no included namespaces.
4. `R6.AC4` The system SHALL configure the unmodified bootstrap policy with no allowed resource rules.
5. `R6.AC5` The system SHALL configure the unmodified bootstrap policy with cluster-scoped observation disabled.
6. `R6.AC6` WHEN an administrator supplies selected observable namespaces through Helm values, the system SHALL render those exact namespace names into the policy input.
7. `R6.AC7` WHEN an administrator supplies selected observable resource types through Helm values, the system SHALL render those exact API-group and Kind pairs into the policy input.
8. `R6.AC8` WHEN an administrator supplies an explicit system-namespace classification through Helm values, the system SHALL preserve omitted and explicitly empty semantics from the approved API contract.
9. `R6.AC9` WHEN the canonical policy is absent during runtime startup, the system SHALL remain fail closed according to the approved policy behavior.
10. `R6.AC10` IF a bootstrap policy fails structural or admission validation, THEN the system SHALL report installation failure without weakening runtime authorization.
11. `R6.AC11` IF `installation-access-ceiling` already exists before Helm installation, THEN the system SHALL require the administrator to select externally managed policy mode.
12. `R6.AC12` WHEN an upgrade does not include an explicit policy change, the system SHALL preserve the installed policy spec.
13. `R6.AC13` WHEN an administrator explicitly changes the installed policy, the system SHALL submit that change through the approved validating webhook.
14. `R6.AC14` The system SHALL keep logical policy configuration independent from the ServiceAccount RBAC manifest.
15. `R6.AC15` The system SHALL document that effective observation requires both logical policy permission and Kubernetes RBAC permission.
16. `R6.AC16` WHERE externally managed policy mode is selected through Helm values, the system SHALL omit `installation-access-ceiling` from the rendered release.
17. `R6.AC17` IF chart-managed policy mode is selected while `installation-access-ceiling` already exists outside the release, THEN the system SHALL fail preflight without modifying the existing policy.
18. `R6.AC18` WHEN `helm uninstall` removes a release that managed `installation-access-ceiling`, the system SHALL retain that policy object.

### R7 Validating Webhook Service And Certificate Lifecycle

**User Story:** As a cluster administrator, I want a trusted validating webhook
with an automated default and a least-privilege fallback, so that covered
writes fail closed even when cert-manager cannot be installed.

#### Acceptance Criteria

1. `R7.AC1` The system SHALL deploy one cluster-local Service for the manager's validating webhook endpoint.
2. `R7.AC2` The system SHALL register one `admissionregistration.k8s.io/v1` `ValidatingWebhookConfiguration` using the approved webhook name and rules.
3. `R7.AC3` The system SHALL route the Kubeseer webhook entry to `/validate-kubeseer-io-v1alpha1-kubeseer`.
4. `R7.AC4` The system SHALL route the access-policy webhook entry to `/validate-kubeseer-io-v1alpha1-kubeseeraccesspolicy`.
5. `R7.AC5` The system SHALL preserve `failurePolicy: Fail`, `matchPolicy: Exact`, `sideEffects: None`, `timeoutSeconds: 10`, and `admissionReviewVersions: [v1]`.
6. `R7.AC6` The system SHALL provision webhook serving certificates through cert-manager in the default certificate mode.
7. `R7.AC7` The system SHALL store the webhook private key only in a Kubernetes `Secret`.
8. `R7.AC8` The system SHALL mount webhook serving material read-only into the manager Pod.
9. `R7.AC9` The system SHALL require the active serving certificate to be valid for the exact in-cluster webhook Service DNS names.
10. `R7.AC10` The system SHALL configure the corresponding CA bundle in both webhook client configurations.
11. `R7.AC11` WHEN installation activates the webhook registration, the system SHALL have a ready TLS endpoint with matching trust material.
12. `R7.AC12` WHEN cert-manager-managed serving material approaches expiration, the system SHALL renew it before the current certificate expires.
13. `R7.AC13` WHEN cert-manager rotates serving material, the system SHALL make the renewed certificate active without requiring an administrator to edit the webhook configuration manually.
14. `R7.AC14` IF certificate issuance, mounting, or CA injection is incomplete, THEN the system SHALL keep installation readiness unsuccessful.
15. `R7.AC15` IF the webhook endpoint is unavailable after registration, THEN the system SHALL rely on the approved fail-closed admission behavior for covered CREATE and UPDATE operations.
16. `R7.AC16` WHEN normal uninstall begins, the system SHALL remove the webhook registration before stopping the final serving endpoint.
17. `R7.AC17` The system SHALL avoid embedding a reusable private key or static production certificate in source, image layers, rendered output, or documentation.
18. `R7.AC18` WHEN an administrator selects the supported certificate lifecycle through Helm values, the system SHALL render only the resources required by that lifecycle.
19. `R7.AC19` IF an unsupported certificate-lifecycle value is supplied, THEN the system SHALL fail Helm values schema validation.
20. `R7.AC20` The system SHALL default the certificate lifecycle Helm value to `certManager`.
21. `R7.AC21` IF `certManager` mode is selected while the required cert-manager APIs are unavailable, THEN the system SHALL fail installation preflight with `externalSecret` identified as the supported fallback.
22. `R7.AC22` WHERE `externalSecret` mode is selected, the system SHALL require the name of a pre-existing TLS Secret in the installation namespace.
23. `R7.AC23` WHERE `externalSecret` mode is selected, the system SHALL require an explicit non-empty PEM CA bundle through Helm values.
24. `R7.AC24` WHERE `externalSecret` mode is selected, the system SHALL omit all cert-manager custom resources from the rendered release.
25. `R7.AC25` WHERE `externalSecret` mode is selected, the system SHALL omit the external TLS Secret from the rendered release.
26. `R7.AC26` WHERE `externalSecret` mode is selected, the system SHALL mount the named Secret read-only into the manager Pod.
27. `R7.AC27` WHERE `externalSecret` mode is selected, the system SHALL place the supplied CA bundle in both webhook client configurations.
28. `R7.AC28` IF the external TLS Secret is absent or lacks a usable `tls.crt` or `tls.key`, THEN the system SHALL keep installation readiness unsuccessful.
29. `R7.AC29` IF the external serving certificate does not match the webhook Service DNS names or supplied CA bundle, THEN the system SHALL keep installation readiness unsuccessful.
30. `R7.AC30` WHEN externally managed serving material rotates under the currently configured CA, the system SHALL activate the renewed key pair without requiring an administrator to edit the manager Deployment.
31. `R7.AC31` WHEN the externally managed serving CA changes, the system SHALL require the release CA bundle to be updated before activating the corresponding serving certificate.
32. `R7.AC32` IF `externalSecret` mode omits its Secret name or CA bundle value, THEN the system SHALL fail Helm values schema validation.

### R8 Upgrade And Rollback Safety

**User Story:** As a platform operator, I want a deterministic upgrade path, so
that controller and webhook changes do not erase policy, custom resources, or
availability unexpectedly.

#### Acceptance Criteria

1. `R8.AC1` WHEN an administrator runs the documented `helm upgrade` command with a supported chart version, the system SHALL apply resources in the documented dependency order.
2. `R8.AC2` WHEN an upgrade includes compatible CRD changes, the system SHALL complete CRD establishment before replacing manager Pods.
3. `R8.AC3` WHEN an upgrade changes the manager image, the system SHALL use a rolling Deployment update.
4. `R8.AC4` WHEN a single-replica manager is upgraded, the system SHALL permit a replacement Pod to become ready before the previous Pod is terminated.
5. `R8.AC5` WHEN an upgrade changes webhook serving configuration, the system SHALL preserve at least one trusted serving endpoint until the replacement is ready.
6. `R8.AC6` WHEN an upgrade changes no policy input, the system SHALL retain the existing `installation-access-ceiling` spec.
7. `R8.AC7` WHEN an upgrade changes no user custom resource, the system SHALL retain every existing `Kubeseer` spec.
8. `R8.AC8` WHEN an upgrade completes, the system SHALL report the installed manager version through the documented operational check.
9. `R8.AC9` IF the replacement manager never becomes ready, THEN the system SHALL leave the Deployment rollout visibly incomplete.
10. `R8.AC10` IF a requested upgrade crosses an unsupported API or storage-version boundary, THEN the system SHALL fail preflight with the unsupported transition identified.
11. `R8.AC11` WHEN an administrator runs `helm rollback` to a declared compatible release revision, the system SHALL preserve CRDs and custom-resource instances.
12. `R8.AC12` IF rollback would require a destructive CRD downgrade, THEN the system SHALL reject the supported rollback path.
13. `R8.AC13` The system SHALL document version-specific prerequisites and compatibility limits for every supported upgrade path.
14. `R8.AC14` WHEN a chart version changes, the system SHALL record the new version in Helm release history.
15. `R8.AC15` WHEN no explicit image version override is supplied, the system SHALL render the chart `appVersion` as the manager image version.
16. `R8.AC16` WHEN `helm upgrade` receives no explicit access-policy change, the system SHALL preserve the release's current access-policy values.
17. `R8.AC17` IF `helm rollback` targets a revision whose CRD contract is no longer compatible with installed storage, THEN the system SHALL reject the supported rollback procedure before changing the release.
18. `R8.AC18` WHEN `helm upgrade` receives no explicit certificate-mode change, the system SHALL preserve the release's current certificate mode and inputs.
19. `R8.AC19` WHEN an `externalSecret` release is upgraded or rolled back, the system SHALL leave the externally managed TLS Secret unchanged.

### R9 Uninstall And Explicit Purge

**User Story:** As a cluster administrator, I want reversible normal uninstall
and a separate destructive purge, so that removing the operator cannot
silently delete declarative state or unrelated cluster resources.

#### Acceptance Criteria

1. `R9.AC1` WHEN an administrator runs the documented Helm uninstall procedure, the system SHALL remove the package-owned validating webhook registration first.
2. `R9.AC2` WHEN the Helm uninstall procedure runs, the system SHALL remove the package-owned manager Deployment.
3. `R9.AC3` WHEN the Helm uninstall procedure runs, the system SHALL remove the package-owned ServiceAccount.
4. `R9.AC4` WHEN the Helm uninstall procedure runs, the system SHALL remove the package-owned webhook Service.
5. `R9.AC5` WHEN the Helm uninstall procedure runs, the system SHALL preserve both Kubeseer CRDs.
6. `R9.AC6` WHEN the Helm uninstall procedure runs, the system SHALL preserve every Kubeseer custom-resource instance.
7. `R9.AC7` WHEN the Helm uninstall procedure runs, the system SHALL preserve `installation-access-ceiling`.
8. `R9.AC8` WHEN the documented Helm uninstall procedure runs repeatedly, the system SHALL converge successfully when package-owned runtime objects are already absent.
9. `R9.AC9` The system SHALL provide one versioned executable purge entrypoint separate from the Helm chart and normal uninstall procedure.
10. `R9.AC10` WHEN an administrator invokes purge without the documented explicit destructive opt-in, the system SHALL perform no custom-resource or CRD deletion.
11. `R9.AC11` WHEN an administrator confirms purge, the system SHALL delete Kubeseer custom-resource instances before deleting their CRDs.
12. `R9.AC12` WHEN uninstall or purge runs, the system SHALL target only objects carrying the canonical Kubeseer identity and documented stable names.
13. `R9.AC13` WHEN uninstall or purge runs, the system SHALL leave unrelated namespaces, workloads, ServiceAccounts, RBAC, webhooks, Secrets, and CRDs unchanged.
14. `R9.AC14` IF cleanup cannot remove a package-owned object, THEN the system SHALL exit non-zero with that remaining object identified.
15. `R9.AC15` IF custom resources remain when confirmed purge reaches CRD deletion, THEN the system SHALL stop before deleting the affected CRD.
16. `R9.AC16` WHEN the Helm uninstall procedure runs, the system SHALL remove the package-owned RBAC bindings.
17. `R9.AC17` WHEN the Helm uninstall procedure runs, the system SHALL remove the package-owned certificate-lifecycle objects.
18. `R9.AC18` WHEN the Helm uninstall procedure completes, the system SHALL remove the Helm release record from the selected namespace.
19. `R9.AC19` WHEN the Helm uninstall procedure completes, the system SHALL report the retained CRDs and access policy to the administrator.
20. `R9.AC20` WHEN an `externalSecret` release is uninstalled or purged, the system SHALL preserve the externally managed TLS Secret.
21. `R9.AC21` The system SHALL require an explicit kubeconfig path and Kubernetes context for every purge invocation.
22. `R9.AC22` IF a purge invocation omits or cannot resolve its explicit kubeconfig path or context, THEN the system SHALL perform no deletion.
23. `R9.AC23` IF a purge invocation does not include the exact documented destructive confirmation token, THEN the system SHALL perform no deletion.
24. `R9.AC24` IF the resolved cluster identity differs from the explicitly confirmed context, THEN the system SHALL perform no deletion.
25. `R9.AC25` WHEN confirmed purge completes, the system SHALL report whether each targeted custom-resource collection and CRD was deleted or already absent.
26. `R9.AC26` WHEN confirmed purge runs after every targeted Kubeseer custom resource and CRD is already absent, the system SHALL converge successfully.

### R10 Documentation And Installation Verification

**User Story:** As a platform operator, I want executable installation checks
and exact lifecycle documentation, so that package success is proven rather
than inferred from rendered YAML.

#### Acceptance Criteria

1. `R10.AC1` The system SHALL document prerequisites for Kubernetes versions, image availability, cluster-admin installation actions, and the selected certificate lifecycle.
2. `R10.AC2` The system SHALL document exact Helm commands for rendering, linting, packaging, installing, checking readiness, upgrading, rolling back, and uninstalling.
3. `R10.AC3` The system SHALL document how to choose the installation namespace without changing canonical base files.
4. `R10.AC4` The system SHALL document how to configure the access policy independently from ServiceAccount RBAC.
5. `R10.AC5` The system SHALL document how to grant namespaced observed-resource RBAC without granting cluster-wide observation.
6. `R10.AC6` The system SHALL document how Kubeseer authors receive namespaced CR permissions without observed-resource permissions.
7. `R10.AC7` WHEN package verification runs, the system SHALL build the manager executable from committed sources.
8. `R10.AC8` WHEN package verification runs, the system SHALL render the canonical chart with production values through `helm template --include-crds`.
9. `R10.AC9` WHEN package verification runs, the system SHALL reject generated-artifact drift, mutable image references, missing security settings, or over-broad default RBAC.
10. `R10.AC10` WHEN installation smoke verification runs against a supported Kubernetes cluster, the system SHALL observe both CRDs as established.
11. `R10.AC11` WHEN installation smoke verification runs against a supported Kubernetes cluster, the system SHALL observe the manager Deployment as available.
12. `R10.AC12` WHEN installation smoke verification runs against a supported Kubernetes cluster, the system SHALL observe the validating webhook certificate as trusted.
13. `R10.AC13` WHEN installation smoke verification submits a structurally valid deny-all bootstrap policy, the system SHALL observe the canonical singleton persisted.
14. `R10.AC14` WHEN installation smoke verification submits a covered invalid object, the system SHALL observe rejection from the installed validating webhook.
15. `R10.AC15` WHEN installation smoke verification runs with default Kubeseer-author permissions, the system SHALL prove that the author cannot modify the installation policy.
16. `R10.AC16` WHEN installation smoke verification runs with default Kubeseer-author permissions, the system SHALL prove that the author has no direct observed-resource permission granted by Kubeseer packaging.
17. `R10.AC17` WHEN compatibility verification runs, the system SHALL exercise installation and removal against every Kubernetes minor in the approved compatibility matrix.
18. `R10.AC18` IF a required installation scenario is skipped or matches zero tests, THEN the system SHALL fail package verification.
19. `R10.AC19` The system SHALL run installation verification without using ambient cloud credentials or an ambient kubeconfig outside the explicitly supplied test cluster.
20. `R10.AC20` WHEN package verification runs, the system SHALL build the production image from the verified manager executable.
21. `R10.AC21` WHEN installation smoke verification runs against a supported Kubernetes cluster, the system SHALL observe the validating webhook endpoint as reachable.
22. `R10.AC22` WHEN chart verification runs, the system SHALL execute `helm lint` against the canonical chart.
23. `R10.AC23` WHEN chart verification runs with every documented values profile, the system SHALL validate each profile against `values.schema.json`.
24. `R10.AC24` WHEN chart verification supplies one invalid value for each constrained value class, the system SHALL observe Helm reject every invalid profile.
25. `R10.AC25` WHEN chart verification runs the documented packaging command, the system SHALL inspect the resulting chart archive for the declared chart version.
26. `R10.AC26` The system SHALL document every supported public chart value and its default in the chart README.
27. `R10.AC27` The system SHALL document cert-manager as the default certificate prerequisite and `externalSecret` as the supported fallback.
28. `R10.AC28` The system SHALL document external certificate issuance, renewal, CA-change ordering, Secret format, and Service DNS requirements as administrator responsibilities in `externalSecret` mode.
29. `R10.AC29` WHEN installation smoke verification runs, the system SHALL exercise both `certManager` and `externalSecret` certificate modes.
30. `R10.AC30` WHEN `externalSecret` smoke verification rotates a serving key pair under the same CA, the system SHALL observe the webhook remain or become trusted without a manager Deployment edit.
31. `R10.AC31` The system SHALL document the exact purge entrypoint, explicit cluster-target arguments, destructive confirmation token, deletion order, and retained external resources.
32. `R10.AC32` WHEN purge smoke verification runs without the exact destructive confirmation token, the system SHALL observe every Kubeseer custom resource and CRD remain present.

## Non-Functional Requirements

- `NFR1` **Security:** The installation SHALL default to deny-all logical policy, non-root execution, protected webhook keys, and no observed-resource RBAC; `R2.AC13`, `R3.AC12` through `R3.AC16`, `R3.AC21` through `R3.AC23`, `R5`, `R6`, and `R7` provide behavioral coverage.
- `NFR2` **Least privilege:** Operator, administrator, and Kubeseer-author permissions SHALL remain explicit and independently reviewable; `R5`, `R6.AC14`, `R6.AC15`, `R10.AC4` through `R10.AC6`, `R10.AC15`, and `R10.AC16` provide behavioral coverage.
- `NFR3` **Reliability:** Installation, readiness, certificate rotation, upgrades, rollback, and cleanup SHALL expose deterministic success or actionable failure; `R3.AC9` through `R3.AC20`, `R7.AC11` through `R7.AC16`, `R8`, and `R9` provide behavioral coverage.
- `NFR4` **Compatibility:** Package APIs and lifecycle behavior SHALL remain compatible with the approved Kubernetes matrix and `kubeseer.io/v1alpha1`; `R4`, `R7.AC2` through `R7.AC5`, `R8`, and `R10.AC17` provide behavioral coverage.
- `NFR5` **Reproducibility:** Equivalent source, toolchain, chart, release, namespace, and values inputs SHALL produce equivalent executable, image, and rendered-manifest identities; `R1`, `R2.AC1` through `R2.AC3`, `R2.AC12`, `R2.AC16`, `R10.AC7` through `R10.AC9`, and `R10.AC20` through `R10.AC25` provide behavioral coverage.
- `NFR6` **Operability:** Administrators SHALL have bounded readiness checks, version visibility, explicit configuration points, and complete lifecycle instructions; `R3`, `R8.AC8`, `R8.AC13`, and `R10` provide behavioral coverage.
- `NFR7` **Supply-chain minimization:** The runtime image SHALL contain only the manager and its runtime prerequisites, use an immutable version, and carry no embedded credentials; `R1.AC6`, `R1.AC7`, `R1.AC12`, `R2.AC12` through `R2.AC14`, and `R2.AC16` provide behavioral coverage.
- `NFR8` **Portability:** The canonical Helm chart SHALL depend only on documented Kubernetes APIs, cert-manager in the default mode, an externally managed Secret in fallback mode, and a configurable OCI image reference rather than cloud-provider-specific services; `R1`, `R2.AC1`, `R3.AC17`, `R7`, and `R10.AC1` provide behavioral coverage.
- `NFR9` **Data safety:** Routine upgrades and uninstall SHALL preserve access policy, CRDs, and user custom resources unless the administrator takes an explicit destructive action; `R4.AC8` through `R4.AC12`, `R6.AC11` through `R6.AC13`, `R8`, and `R9` provide behavioral coverage.
- `NFR10` **Testability:** Packaging behavior SHALL be proven through deterministic rendering, static policy checks, image inspection, and genuine supported-cluster smoke scenarios; `R10` provides behavioral coverage.

## Constraints And Dependencies

- `C1` This feature depends on the approved and fresh `kubeseer-api-foundation`, `integration-testing-foundation`, `installation-access-policy`, `authorization-enforcement`, `admission-validation`, `observability`, and `performance-and-limits` contracts.
- `C2` The canonical initial package is a Helm application chart under `charts/kubeseer`; Kustomize installation overlays are not required by this feature.
- `C3` The supported Kubernetes compatibility matrix remains the centrally declared versions `1.35.6` and `1.36.2` until an approved compatibility change replaces them.
- `C4` The public APIs remain `kubeseer.io/v1alpha1`; packaging must not redefine API fields, defaults, validation, condition meanings, or runtime failure semantics.
- `C5` The default certificate mode is `certManager`; the only supported fallback is `externalSecret`, which delegates issuance, renewal, and CA ownership to the administrator or another approved PKI system.
- `C6` The manager uses controller-runtime's manager, metrics registry, health endpoints, webhook server, signal handling, and leader-election semantics rather than parallel process infrastructure.
- `C7` Logical policy authorization remains mandatory even when operator ServiceAccount RBAC is broader; packaging cannot convert Kubernetes RBAC into a logical allow decision.
- `C8` The package may expose supported deployment and limit configuration, but it must preserve the default values and invalid-configuration behavior approved by `performance-and-limits`.
- `C9` The installation smoke proof may use an explicitly supplied disposable cluster, but Kubernetes cluster creation and container-engine lifecycle belong to `local-development-environment`.
- `C10` Full product behavior certification belongs to `end-to-end-scenarios`; this feature proves installation lifecycle, API establishment, manager readiness, webhook operation, and permission boundaries.
- `C11` Automated proof follows the repository testing constitution: genuine cross-module or cluster-level behavior is required, zero-match tests fail, and no dedicated package-local unit-test layer is added.
- `C12` Kubeseer-authored source, manifests, container metadata, tests, and documentation remain under Apache License 2.0 with Alessandro Rontani as the default copyright holder.
- `C13` The Helm CLI version used for chart linting, rendering, packaging, installation, upgrade, rollback, and uninstall proof must be pinned or centrally declared.
- `C14` Only one active Kubeseer Helm release is supported in a cluster because the CRDs, access-policy singleton, and validating webhook configuration are cluster-scoped installation resources.
- `C15` The supported cert-manager version range used by package verification must be pinned or centrally declared.

## Out Of Scope

- Kustomize installation overlays, an Operator Lifecycle Manager bundle, OperatorHub metadata, or distribution through a vendor marketplace.
- Provisioning or deleting Kubernetes clusters, Podman resources, cloud infrastructure, DNS zones, load balancers, or container registries.
- Publishing release images to a public registry, image signing, provenance attestations, vulnerability-scanner policy, or SBOM distribution.
- Supporting certificate modes beyond `certManager` and `externalSecret`, user-supplied private keys in committed configuration or Helm values, or a Kubeseer-owned general-purpose PKI service.
- Cloud-provider-specific identity, workload identity, secret manager, ingress, service mesh, monitoring operator, or policy-engine integration.
- GitOps-controller installation, reconciliation policy, drift remediation, or promotion between environments.
- Automatic generation of ServiceAccount RBAC from live discovery or from the active `KubeseerAccessPolicy`.
- User impersonation, SubjectAccessReview-based creator authorization, tenant-specific ServiceAccounts, or per-Kubeseer runtime identities.
- Changing the singleton access-policy schema, admission semantics, authorization semantics, observability vocabulary, or performance-limit defaults.
- Conversion webhooks, a second API version, destructive storage migration, backup, restore, disaster recovery, or cross-cluster migration.
- Certifying resource selection, extraction, operators, aggregation, degraded results, update propagation, or semantic no-op behavior end to end.
- Bundled examples, local kind-on-Podman workflow, developer diagnostics collection, or project-owned local environment cleanup.
