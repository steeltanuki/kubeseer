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

# One source of truth for package, E2E, and persistent local development.
# Numeric minimums are deliberately conservative; changing a compatibility
# member requires an approved specification and a matching node image.
CONTROLLER_GEN_VERSION := v0.20.1
SETUP_ENVTEST_VERSION := v0.24.1
KUBERNETES_VERSION ?= 1.35.6
KUBERNETES_COMPATIBILITY_VERSIONS := 1.35.6 1.36.2
KIND_NODE_IMAGE_1_35_6 ?= kindest/node:v1.35.5
KIND_NODE_IMAGE_1_36_2 ?= kindest/node:v1.36.1
CERT_MANAGER_VERSION ?= v1.18.2

GO_MIN_VERSION ?= 1.26
GIT_MIN_VERSION ?= 2.30
MAKE_MIN_VERSION ?= 4.3
PODMAN_MIN_VERSION ?= 4.9
KIND_MIN_VERSION ?= 0.23
KUBECTL_MIN_VERSION ?= 1.27
HELM_MIN_VERSION ?= 3.12
CURL_MIN_VERSION ?= 7.81

LOCAL_CLUSTER_NAME ?= kubeseer-local
LOCAL_KUBE_CONTEXT ?= kind-kubeseer-local
LOCAL_NAMESPACE ?= kubeseer-system
LOCAL_RELEASE ?= kubeseer
LOCAL_PROVIDER ?= podman

GO_MODULE_CACHE := $(shell go env GOMODCACHE)
LOCAL_GO_PROXY := file://$(GO_MODULE_CACHE)/cache/download
GO_RUN_WITH_LOCAL_PROXY := GOPROXY=$(LOCAL_GO_PROXY),https://proxy.golang.org,direct go run
CONTROLLER_GEN := $(GO_RUN_WITH_LOCAL_PROXY) sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
SETUP_ENVTEST := $(GO_RUN_WITH_LOCAL_PROXY) sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION)
API_PACKAGE := ./api/v1alpha1
CRD_OUTPUT := config/crd/bases
DEEP_COPY_HEADER := hack/boilerplate.go.txt
