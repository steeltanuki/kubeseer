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

package reconciliation

import (
	"errors"
	"fmt"
)

// FailureReason identifies a sanitized runtime failure without exposing
// Kubernetes response bodies or observed values.
type FailureReason string

const (
	ReasonReadUnavailable   FailureReason = "ReadUnavailable"
	ReasonPolicyUnavailable FailureReason = "PolicyUnavailable"
	ReasonStatusConflict    FailureReason = "StatusConflict"
	ReasonStatusUnavailable FailureReason = "StatusUnavailable"
	ReasonStaleLease        FailureReason = "StaleLease"
	ReasonInvalidSetup      FailureReason = "InvalidSetup"
	ReasonBuildFailure      FailureReason = "BuildFailure"
)

// RuntimeError carries a stage, stable reason, and retry classification. The
// cause remains available to programmatic callers but is never formatted into
// the sanitized message.
type RuntimeError struct {
	Stage     string
	SourceID  string
	Reason    FailureReason
	Message   string
	Retryable bool
	Cause     error
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return ""
	}
	identity := "reconciliation"
	if e.Stage != "" {
		identity += " stage " + e.Stage
	}
	if e.SourceID != "" {
		identity += fmt.Sprintf(" source %q", e.SourceID)
	}
	if e.Message == "" {
		return fmt.Sprintf("%s: %s", identity, e.Reason)
	}
	return fmt.Sprintf("%s: %s: %s", identity, e.Reason, e.Message)
}

func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsRetryable reports whether an error should be returned to the
// controller-runtime rate-limited queue.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) {
		return runtimeErr.Retryable
	}
	return false
}

// ErrStaleLease is returned when a route or publication operation no longer
// owns the current UID/generation/policy epoch.
var ErrStaleLease = errors.New("reconciliation lease is stale")

func staleRuntimeError(stage string) *RuntimeError {
	return &RuntimeError{
		Stage:   stage,
		Reason:  ReasonStaleLease,
		Message: "candidate is no longer current",
		Cause:   ErrStaleLease,
	}
}

func transientRuntimeError(stage, sourceID string, reason FailureReason, message string, cause error) *RuntimeError {
	return &RuntimeError{Stage: stage, SourceID: sourceID, Reason: reason, Message: message, Retryable: true, Cause: cause}
}
