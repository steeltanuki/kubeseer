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

package managerapp

import "strings"

// VersionInfo contains immutable build metadata supplied by linker flags.
type VersionInfo struct {
	Version string
	Commit  string
	Date    string
}

// VersionString returns a stable, log-friendly identity for the executable.
func VersionString(info VersionInfo) string {
	return "kubeseer version=" + known(info.Version) +
		" commit=" + known(info.Commit) +
		" date=" + known(info.Date)
}

func known(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "unknown"
}
