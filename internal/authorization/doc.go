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

// Package authorization turns installation-policy decisions into immutable,
// process-local capabilities that observed-resource I/O adapters can verify.
//
// The package deliberately does not load or interpret policy rules. It owns
// subject identity, sanitized evidence, capability construction, and the
// freshness port shared by higher-level runtime components.
//
// Responsibility: bind complete policy decisions to opaque, freshness-checked
// capabilities and sanitized authorization evidence.
//
// Boundary: authorization never performs Kubernetes reads or evaluates policy;
// consuming selection and watch adapters must present its capabilities.
package authorization
