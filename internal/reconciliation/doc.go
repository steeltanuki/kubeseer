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

// Package reconciliation composes the approved Kubeseer processing modules
// into one controller-runtime reconciliation boundary.
//
// The package owns lifecycle tracking, exact authorized watch routing,
// scheduling, retries, orchestration, and status-subresource publication. It
// deliberately keeps Kubernetes clients behind narrow ports so the same
// runtime can be exercised by cross-module and envtest suites.
//
// Responsibility: coordinate authorized selection, extraction, typing,
// operators, aggregation, retries, and guarded semantic status publication.
//
// Boundary: reconciliation owns orchestration and lifecycle state; concrete
// Kubernetes clients enter only through explicit adapters and ports.
package reconciliation
