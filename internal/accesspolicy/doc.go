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

// Package accesspolicy compiles and evaluates the installation-wide ceiling
// that bounds which Kubernetes resources Kubeseer may observe.
//
// Responsibility: load the canonical access policy, compile immutable
// fail-closed snapshots, and evaluate resource and namespace requests.
//
// Boundary: Kubernetes I/O is confined to policy loading; compilation and
// evaluation are deterministic domain operations and never read resources.
package accesspolicy
