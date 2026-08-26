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
CONTROLLER_GEN := go run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
SETUP_ENVTEST_VERSION := v0.24.1
SETUP_ENVTEST := go run sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION)
API_PACKAGE := ./api/v1alpha1
CRD_OUTPUT := config/crd/bases
DEEP_COPY_HEADER := hack/boilerplate.go.txt
KUBERNETES_VERSION ?= 1.35.6
KUBERNETES_COMPATIBILITY_VERSIONS := 1.35.6 1.36.2
GO_TEST_FLAGS ?=
KUBEBUILDER_ASSETS ?=

.PHONY: generate manifests verify test test-integration test-api test-compatibility e2e

generate:
	$(CONTROLLER_GEN) object:headerFile=$(DEEP_COPY_HEADER) paths=$(API_PACKAGE)

manifests:
	$(CONTROLLER_GEN) crd:crdVersions=v1 paths=$(API_PACKAGE) output:crd:artifacts:config=$(CRD_OUTPUT)

verify:
	./hack/verify-generated.sh
	./hack/verify-test-layer-policy.sh

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
	GO_TEST_FLAGS="$(GO_TEST_FLAGS)" KUBEBUILDER_ASSETS="$$assets" ./hack/test-layer-runner.sh kubernetes-api ./internal/selection '^TestEnvtestSelection$$' required

test-compatibility:
	@set -eu; \
	for version in $(KUBERNETES_COMPATIBILITY_VERSIONS); do \
		printf 'Running API contract against Kubernetes %s\n' "$$version"; \
		KUBEBUILDER_ASSETS= $(MAKE) --no-print-directory test-api KUBERNETES_VERSION="$$version"; \
	done; \
	printf '%s\n' 'API compatibility matrix passed'

e2e:
	./hack/e2e-harness.sh ./test/e2e/... '^TestEndToEnd$$'
