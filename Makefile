# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

CONTROLLER_GEN_VERSION := v0.20.1
SETUP_ENVTEST_VERSION := v0.24.1
GO_MODULE_CACHE := $(shell go env GOMODCACHE)
LOCAL_GO_PROXY := file://$(GO_MODULE_CACHE)/cache/download
GO_RUN_WITH_LOCAL_PROXY := GOPROXY=$(LOCAL_GO_PROXY),https://proxy.golang.org,direct go run
CONTROLLER_GEN := $(GO_RUN_WITH_LOCAL_PROXY) sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
SETUP_ENVTEST := $(GO_RUN_WITH_LOCAL_PROXY) sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION)
API_PACKAGE := ./api/v1alpha1
CRD_OUTPUT := config/crd/bases
DEEP_COPY_HEADER := hack/boilerplate.go.txt
KUBERNETES_VERSION ?= 1.35.6
KUBERNETES_COMPATIBILITY_VERSIONS := 1.35.6 1.36.2
# kind publishes a stable image for the latest available patch in each
# supported minor line; keep the API-compatibility version above independent
# from the image tag so the mapping remains explicit and reproducible.
KIND_NODE_IMAGE_1_35_6 ?= kindest/node:v1.35.5
KIND_NODE_IMAGE_1_36_2 ?= kindest/node:v1.36.1
CERT_MANAGER_VERSION ?= v1.18.2
GO_TEST_FLAGS ?=
KUBEBUILDER_ASSETS ?=

.PHONY: generate manifests package-sync-crds package-crd-check package-apply-crds build build-purge verify verify-package test test-integration test-api test-compatibility test-package-compatibility e2e

generate:
	$(CONTROLLER_GEN) object:headerFile=$(DEEP_COPY_HEADER) paths=$(API_PACKAGE)

manifests:
	$(CONTROLLER_GEN) crd:crdVersions=v1 paths=$(API_PACKAGE) output:crd:artifacts:config=$(CRD_OUTPUT)

verify:
	./hack/verify-generated.sh
	./hack/verify-test-layer-policy.sh
	./hack/verify-admission-boundaries.sh
	./hack/verify-observability-boundaries.sh
	./hack/verify-performance-and-limits-boundaries.sh
	GOCACHE=$${GOCACHE:-/tmp/kubeseer-e2e-go-build} GOMODCACHE=$${GOMODCACHE:-/tmp/kubeseer-e2e-go-mod} ./hack/verify-e2e-boundaries.sh complete
	./hack/verify-package.sh

verify-package:
	./hack/verify-package.sh

test-package-compatibility:
	./hack/test-package-compatibility.sh

package-sync-crds:
	./hack/package-sync-crds.sh

package-crd-check:
	./hack/check-crd-compatibility.sh --kubeconfig "$(KUBECONFIG)" --context "$(KUBE_CONTEXT)"

package-apply-crds:
	./hack/apply-compatible-crds.sh --kubeconfig "$(KUBECONFIG)" --context "$(KUBE_CONTEXT)"

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=$(or $(GOOS),linux) GOARCH=$(or $(GOARCH),amd64) go build -trimpath -ldflags="-s -w -X main.buildVersion=$(or $(VERSION),unknown) -X main.buildCommit=$(or $(COMMIT),unknown) -X main.buildDate=$(or $(BUILD_DATE),unknown)" -o bin/kubeseer ./cmd/kubeseer

build-purge:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=$(or $(GOOS),linux) GOARCH=$(or $(GOARCH),amd64) go build -trimpath -ldflags="-s -w -X main.buildVersion=$(or $(VERSION),unknown) -X main.buildCommit=$(or $(COMMIT),unknown) -X main.buildDate=$(or $(BUILD_DATE),unknown)" -o bin/kubeseer-purge ./cmd/kubeseer-purge

test:
	@set -eu; \
	if GO_TEST_FLAGS="$(GO_TEST_FLAGS)" ./hack/test-layer-runner.sh module-integration ./test/integration/... '^TestModuleIntegration$$' probe; then \
		$(MAKE) --no-print-directory test-integration; \
	else \
		status=$$?; \
		if [ "$$status" -ne 2 ]; then exit "$$status"; fi; \
		$(MAKE) --no-print-directory test-api; \
	fi

test-integration:
	GO_TEST_FLAGS="$(GO_TEST_FLAGS)" ./hack/test-layer-runner.sh module-integration ./test/integration/... '^TestModuleIntegration$$' required

test-api:
	@set -eu; \
	if [ -n "$(KUBEBUILDER_ASSETS)" ]; then \
		assets="$(KUBEBUILDER_ASSETS)"; \
	else \
		assets="$$( $(SETUP_ENVTEST) use -p path $(KUBERNETES_VERSION)! 2>/dev/null || ./hack/envtest-assets.sh $(KUBERNETES_VERSION) )"; \
	fi; \
	test -n "$$assets"; \
	GO_TEST_FLAGS="$(GO_TEST_FLAGS)" KUBEBUILDER_ASSETS="$$assets" ./hack/test-layer-runner.sh kubernetes-api $(API_PACKAGE) '^TestAPIContract$$' required; \
	GO_TEST_FLAGS="$(GO_TEST_FLAGS)" KUBEBUILDER_ASSETS="$$assets" ./hack/test-layer-runner.sh kubernetes-api ./internal/discovery '^TestEnvtestDiscovery$$' required; \
	GO_TEST_FLAGS="$(GO_TEST_FLAGS)" KUBEBUILDER_ASSETS="$$assets" ./hack/test-layer-runner.sh kubernetes-api ./internal/selection '^TestEnvtestSelection$$' required; \
	GO_TEST_FLAGS="$(GO_TEST_FLAGS)" KUBEBUILDER_ASSETS="$$assets" ./hack/test-layer-runner.sh kubernetes-api ./test/envtest '^TestEnvtestReconciliationRuntime$$' required; \
	GO_TEST_FLAGS="$(GO_TEST_FLAGS)" KUBEBUILDER_ASSETS="$$assets" ./hack/test-layer-runner.sh kubernetes-api ./test/envtest '^TestEnvtestAdmissionValidation$$' required

test-compatibility:
	@set -eu; \
	for version in $(KUBERNETES_COMPATIBILITY_VERSIONS); do \
		printf 'Running API contract against Kubernetes %s\n' "$$version"; \
		KUBEBUILDER_ASSETS= $(MAKE) --no-print-directory test-api KUBERNETES_VERSION="$$version"; \
	done; \
	printf '%s\n' 'API compatibility matrix passed'

e2e:
	KUBERNETES_VERSION="$(KUBERNETES_VERSION)" \
	CERT_MANAGER_VERSION="$(CERT_MANAGER_VERSION)" \
	KIND_NODE_IMAGE_1_35_6="$(KIND_NODE_IMAGE_1_35_6)" \
	KIND_NODE_IMAGE_1_36_2="$(KIND_NODE_IMAGE_1_36_2)" \
	./hack/e2e-harness.sh ./test/e2e/... '^TestEndToEnd$$'
