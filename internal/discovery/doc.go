// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package discovery resolves source descriptors to Kubernetes resource
// identities and their discovery-reported scope.
//
// Responsibility: own discovery validation, resolution, stable source-scoped
// failures, and the resolution cache without owning controller orchestration.
//
// Boundary: the package owns the consumer-owned DiscoveryClient port and the
// Clock function adapter. Concrete Kubernetes clients and clocks are supplied
// by the composition root or by a higher-layer test through NewResolver.
package discovery
