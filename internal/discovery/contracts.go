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

package discovery

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SourceDescriptor identifies the Kubernetes API type requested by one source.
// It is an internal boundary and does not define the public Kubeseer CRD.
type SourceDescriptor struct {
	SourceID   string
	APIVersion string
	Kind       string
}

// Validate checks the fields that must be valid before a discovery request.
func (d SourceDescriptor) Validate() error {
	if strings.TrimSpace(d.SourceID) == "" {
		return NewResolutionError(d.SourceID, ReasonInvalidDescriptor, "source ID must not be empty")
	}
	if strings.TrimSpace(d.APIVersion) == "" {
		return NewResolutionError(d.SourceID, ReasonInvalidDescriptor, "apiVersion must not be empty")
	}
	groupVersion, err := schema.ParseGroupVersion(d.APIVersion)
	if err != nil || groupVersion.Version == "" {
		return NewResolutionError(d.SourceID, ReasonInvalidDescriptor, fmt.Sprintf("apiVersion %q is invalid", d.APIVersion))
	}
	if strings.TrimSpace(d.Kind) == "" {
		return NewResolutionError(d.SourceID, ReasonInvalidDescriptor, "kind must not be empty")
	}

	return nil
}

// Scope describes whether a discovered resource requires a namespace.
type Scope int

const (
	// ScopeNamespaced identifies resources addressed within a namespace.
	ScopeNamespaced Scope = iota
	// ScopeCluster identifies resources addressed at cluster scope.
	ScopeCluster
)

// Valid reports whether the scope is one of the supported values.
func (s Scope) Valid() bool {
	return s == ScopeNamespaced || s == ScopeCluster
}

// String returns the stable serialized name used in diagnostics.
func (s Scope) String() string {
	switch s {
	case ScopeNamespaced:
		return "namespaced"
	case ScopeCluster:
		return "cluster"
	default:
		return "unknown"
	}
}

// Resolution is the metadata needed to address a Kubernetes resource type.
type Resolution struct {
	SourceID string
	Resource schema.GroupVersionResource
	Scope    Scope
}

// Outcome carries the independent result for one source in a batch.
// Exactly one of Resolution and Err is expected to be non-nil.
type Outcome struct {
	SourceID   string
	Resolution *Resolution
	Err        *ResolutionError
}

// ResolutionErrorReason is a stable category for source-scoped failures.
type ResolutionErrorReason string

const (
	ReasonUnknownType          ResolutionErrorReason = "UnknownType"
	ReasonDiscoveryUnavailable ResolutionErrorReason = "DiscoveryUnavailable"
	ReasonInvalidDescriptor    ResolutionErrorReason = "InvalidDescriptor"
	ReasonAmbiguousResource    ResolutionErrorReason = "AmbiguousResource"
	ReasonInvalidScope         ResolutionErrorReason = "InvalidScope"
)

// ResolutionError is safe to expose to status-producing callers. Message is
// human-readable and must not contain resource contents or arbitrary payloads.
type ResolutionError struct {
	SourceID string
	Reason   ResolutionErrorReason
	Message  string
	Cause    error
}

// NewResolutionError creates a source-scoped error with a stable reason.
func NewResolutionError(sourceID string, reason ResolutionErrorReason, message string) *ResolutionError {
	return &ResolutionError{SourceID: sourceID, Reason: reason, Message: message}
}

// Error implements error with a deterministic source/reason/message format.
func (e *ResolutionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("source %q: %s", e.SourceID, e.Reason)
	}
	return fmt.Sprintf("source %q: %s: %s", e.SourceID, e.Reason, e.Message)
}

// Unwrap exposes the underlying discovery error without changing the stable
// status-facing Error string.
func (e *ResolutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// HasReason reports whether err is a ResolutionError with the requested reason.
func HasReason(err error, reason ResolutionErrorReason) bool {
	var resolutionErr *ResolutionError
	return errors.As(err, &resolutionErr) && resolutionErr.Reason == reason
}
