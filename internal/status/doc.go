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

// Package status owns pure semantic composition primitives for the public
// Kubeseer status contract.
//
// Responsibility: normalize, hash, compare, and project deterministic status
// snapshots and condition assessments from completed runtime outcomes.
//
// Boundary: status performs no Kubernetes I/O and never decides authorization
// or reads resource data outside the values supplied by reconciliation.
package status
