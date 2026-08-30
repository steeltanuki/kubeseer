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

package selection

import (
	"errors"
	"fmt"

	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// SelectionErrorReason is the stable source-scoped planning or execution
// failure category exposed to callers.
type SelectionErrorReason string

const (
	ReasonInvalidSource         SelectionErrorReason = "InvalidSource"
	ReasonInvalidNamespaceScope SelectionErrorReason = "InvalidNamespaceScope"
	ReasonInvalidSelector       SelectionErrorReason = "InvalidSelector"
	ReasonAuthorizationMissing  SelectionErrorReason = "AuthorizationMissing"
	ReasonAuthorizationDenied   SelectionErrorReason = "AuthorizationDenied"
	ReasonAuthorizationMismatch SelectionErrorReason = "AuthorizationMismatch"
	ReasonAuthorizationStale    SelectionErrorReason = "AuthorizationStale"
	ReasonReadForbidden         SelectionErrorReason = "ReadForbidden"
	ReasonUnsupportedSelector   SelectionErrorReason = "UnsupportedSelector"
	ReasonReadInterrupted       SelectionErrorReason = "ReadInterrupted"
	ReasonReadUnavailable       SelectionErrorReason = "ReadUnavailable"
	ReasonListExpired           SelectionErrorReason = "ListExpired"
	ReasonInvalidObject         SelectionErrorReason = "InvalidObject"
)

// SelectionError is safe to expose to status-producing callers. Cause is
// available through errors.Is/errors.As but is never included in the stable
// user-facing error string.
type SelectionError struct {
	SourceID string
	Reason   SelectionErrorReason
	Message  string
	Cause    error
}

// NewSelectionError creates a stable source-scoped selection error.
func NewSelectionError(sourceID string, reason SelectionErrorReason, message string) *SelectionError {
	return &SelectionError{SourceID: sourceID, Reason: reason, Message: message}
}

func selectionErrorWithCause(sourceID string, reason SelectionErrorReason, message string, cause error) *SelectionError {
	return &SelectionError{SourceID: sourceID, Reason: reason, Message: message, Cause: cause}
}

// Error implements error with deterministic source, reason, and message text.
func (e *SelectionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("source %q: %s", e.SourceID, e.Reason)
	}
	return fmt.Sprintf("source %q: %s: %s", e.SourceID, e.Reason, e.Message)
}

// Unwrap exposes an underlying cause without changing the stable diagnostic.
func (e *SelectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// HasReason reports whether err carries the requested selection reason.
func HasReason(err error, reason SelectionErrorReason) bool {
	var selectionErr *SelectionError
	return errors.As(err, &selectionErr) && selectionErr.Reason == reason
}

// ReadTarget is the exact resource identity and scope of one authorized read.
// A cluster-scoped target uses an empty Namespace.
type ReadTarget struct {
	SourceID  string
	GVR       schema.GroupVersionResource
	Kind      string
	Scope     discovery.Scope
	Namespace string
}

// SelectionPlan is an immutable, instance-I/O-free plan for one source.
// Values are exposed through copy-returning accessors so callers cannot mutate
// the planner's target or selector state.
type SelectionPlan struct {
	sourceID      string
	apiVersion    string
	kind          string
	targets       []ReadTarget
	labelSelector string
	fieldSelector string
}

// SourceID returns the unchanged public source identifier.
func (p SelectionPlan) SourceID() string { return p.sourceID }

// APIVersion returns the declared source API version.
func (p SelectionPlan) APIVersion() string { return p.apiVersion }

// Kind returns the declared source Kind.
func (p SelectionPlan) Kind() string { return p.kind }

// Targets returns a defensive copy of the exact read targets.
func (p SelectionPlan) Targets() []ReadTarget {
	return append([]ReadTarget(nil), p.targets...)
}

// LabelSelector returns the canonical Kubernetes label selector query.
func (p SelectionPlan) LabelSelector() string { return p.labelSelector }

// FieldSelector returns the canonical Kubernetes field selector query.
func (p SelectionPlan) FieldSelector() string { return p.fieldSelector }

// AuthorizedRead is the private I/O permit for one exact target. Its fields
// are private so callers can pass only a capability produced by the
// authorization enforcement boundary to a ResourceLister.
type AuthorizedRead struct {
	target     ReadTarget
	capability authorization.Capability
}

// Target returns a defensive copy of the exact target paired with the
// capability.
func (r AuthorizedRead) Target() ReadTarget { return r.target }

// Capability returns the opaque capability paired with the target.
func (r AuthorizedRead) Capability() authorization.Capability { return r.capability }

// Valid reports whether the read permit contains a current-independent exact
// target/capability pairing. Freshness is checked by the executor immediately
// before each network request.
func (r AuthorizedRead) Valid() bool {
	return r.target.Scope.Valid() && r.capability.Valid() && r.capability.Request() == RequestForTarget(r.target)
}

// AuthorizedPlan is the only plan type accepted by the resource executor. Its
// fields are private so an unbound plan cannot be assembled accidentally.
type AuthorizedPlan struct {
	plan  SelectionPlan
	reads []AuthorizedRead
}

// Plan returns the immutable source plan after authorization binding. It is
// intentionally exposed for batch orchestration and diagnostics, while the
// executor receives the capability itself.
func (p AuthorizedPlan) Plan() SelectionPlan { return clonePlan(p.plan) }

// Reads returns defensive value copies of the private target permits for
// adapters and integration fixtures that need to inspect execution shape.
func (p AuthorizedPlan) Reads() []AuthorizedRead {
	return append([]AuthorizedRead(nil), p.reads...)
}

// Provenance identifies the selected Kubernetes object without copying object
// contents into diagnostics.
type Provenance struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	UID        types.UID
}

// SelectedResource is one selected object and its stable provenance.
type SelectedResource struct {
	Object     *unstructured.Unstructured
	Provenance Provenance
}

// SelectionOutcome is one source-scoped result. Err is nil for successful
// outcomes, including valid empty selections.
type SelectionOutcome struct {
	SourceID  string
	Resources []SelectedResource
	Err       *SelectionError
}

func clonePlan(plan SelectionPlan) SelectionPlan {
	plan.targets = append([]ReadTarget(nil), plan.targets...)
	return plan
}
