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
API_PACKAGE := ./api/v1alpha1
CRD_OUTPUT := config/crd/bases
DEEP_COPY_HEADER := hack/boilerplate.go.txt

.PHONY: generate manifests verify

generate:
	$(CONTROLLER_GEN) object:headerFile=$(DEEP_COPY_HEADER) paths=$(API_PACKAGE)

manifests:
	$(CONTROLLER_GEN) crd:crdVersions=v1 paths=$(API_PACKAGE) output:crd:artifacts:config=$(CRD_OUTPUT)

verify:
	./hack/verify-generated.sh
