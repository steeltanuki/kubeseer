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

package admission

import "sort"

// Class is the Kubernetes-facing category of an admission result.
type Class string

const (
	// Invalid identifies deterministic object or declaration defects.
	Invalid Class = "Invalid"
	// Forbidden identifies a current installation-policy denial.
	Forbidden Class = "Forbidden"
	// Unavailable identifies a retryable validation dependency failure.
	Unavailable Class = "ValidationUnavailable"
)

// Issue is one sanitized, submitted-path admission diagnostic.
type Issue struct {
	Path    string
	Reason  string
	Message string
	Class   Class
}

// Result is the complete deterministic outcome of one pure or dynamic
// admission validation request.
type Result struct {
	Issues []Issue
}

// Valid reports whether validation produced no issues.
func (r Result) Valid() bool { return len(r.Issues) == 0 }

// Class returns the highest-precedence class represented by the result.
func (r Result) Class() Class {
	class := Class("")
	for _, issue := range r.Issues {
		if issue.Class == Invalid {
			return Invalid
		}
		if issue.Class == Forbidden {
			class = Forbidden
			continue
		}
		if issue.Class == Unavailable && class == "" {
			class = Unavailable
		}
	}
	return class
}

// IssuesCopy returns a defensive copy in canonical order.
func (r Result) IssuesCopy() []Issue {
	issues := append([]Issue(nil), r.Issues...)
	SortIssues(issues)
	return issues
}

// SortIssues applies the approved path, reason, message, and class ordering.
func SortIssues(issues []Issue) {
	sort.SliceStable(issues, func(left, right int) bool {
		if issues[left].Path != issues[right].Path {
			return issues[left].Path < issues[right].Path
		}
		if issues[left].Reason != issues[right].Reason {
			return issues[left].Reason < issues[right].Reason
		}
		if issues[left].Message != issues[right].Message {
			return issues[left].Message < issues[right].Message
		}
		return issues[left].Class < issues[right].Class
	})
}

func result(issues []Issue) Result {
	SortIssues(issues)
	return Result{Issues: issues}
}

func invalidIssue(path, reason, message string) Issue {
	return Issue{Path: path, Reason: reason, Message: message, Class: Invalid}
}
