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

package authorization

import (
	"context"
	"errors"
	"fmt"

	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"k8s.io/apimachinery/pkg/types"
)

// Subject is the complete process-local identity of one live Kubeseer
// reconciliation. Values are passed by copy and no method mutates them.
type Subject struct {
	Key         types.NamespacedName
	UID         types.UID
	Generation  int64
	PolicyEpoch uint64
}

// NewSubject validates and constructs an immutable-by-value authorization
// subject. PolicyEpoch zero is valid because it is the initial runtime epoch.
func NewSubject(key types.NamespacedName, uid types.UID, generation int64, policyEpoch uint64) (Subject, error) {
	subject := Subject{Key: key, UID: uid, Generation: generation, PolicyEpoch: policyEpoch}
	if err := subject.Validate(); err != nil {
		return Subject{}, err
	}
	return subject, nil
}

// Validate checks the identity dimensions required before any authorization
// decision can produce a capability.
func (s Subject) Validate() error {
	if s.Key.Namespace == "" || s.Key.Name == "" {
		return errors.New("authorization subject namespace and name are required")
	}
	if s.UID == "" {
		return errors.New("authorization subject UID is required")
	}
	if s.Generation <= 0 {
		return errors.New("authorization subject generation must be positive")
	}
	return nil
}

// PolicyState describes the policy snapshot state represented by a record.
type PolicyState string

const (
	PolicyReady       PolicyState = "Ready"
	PolicyMissing     PolicyState = "Missing"
	PolicyInvalid     PolicyState = "Invalid"
	PolicyUnavailable PolicyState = "Unavailable"
)

// RecordKind identifies whether evidence describes logical policy evaluation
// or a later Kubernetes RBAC enforcement failure.
type RecordKind string

const (
	RecordPolicyDecision RecordKind = "PolicyDecision"
	RecordReadForbidden  RecordKind = "ReadForbidden"
)

// Outcome identifies the sanitized result of one enforcement boundary.
type Outcome string

const (
	OutcomeAllowed     Outcome = "Allowed"
	OutcomeDenied      Outcome = "Denied"
	OutcomeUnavailable Outcome = "Unavailable"
	OutcomeForbidden   Outcome = "ReadForbidden"
)

// Record is the allowlisted authorization evidence exposed to in-process
// observers. It intentionally contains no selector, object identity, value,
// field path, payload, message, or raw Kubernetes error.
type Record struct {
	Kind           RecordKind
	Subject        Subject
	SourceID       string
	APIGroup       string
	KindName       string
	Scope          discovery.Scope
	Namespace      string
	PolicyState    PolicyState
	PolicyIdentity *accesspolicy.PolicyIdentity
	Outcome        Outcome
	Reason         string
}

// DeepCopy returns a defensive record copy, including policy identity.
func (r Record) DeepCopy() Record {
	copy := r
	if r.PolicyIdentity != nil {
		identity := *r.PolicyIdentity
		copy.PolicyIdentity = &identity
	}
	return copy
}

// Recorder observes sanitized authorization evidence. It cannot influence a
// decision because the method has no error result.
type Recorder interface {
	Record(context.Context, []Record)
}

// RecorderFunc adapts a function to Recorder and defensively copies records
// before invoking it.
type RecorderFunc func(context.Context, []Record)

// Record implements Recorder.
func (f RecorderFunc) Record(ctx context.Context, records []Record) {
	if f == nil {
		return
	}
	f(ctx, cloneRecords(records))
}

// NoopRecorder is the explicit recorder used when no observer is configured.
// It retains no state and never affects authorization.
type NoopRecorder struct{}

// Record implements Recorder.
func (NoopRecorder) Record(context.Context, []Record) {}

// Verifier reports whether a subject remains current in the owning runtime.
// A nil verifier is never treated as current.
type Verifier interface {
	IsCurrent(Subject) bool
}

// VerifierFunc adapts a function to Verifier.
type VerifierFunc func(Subject) bool

// IsCurrent implements Verifier.
func (f VerifierFunc) IsCurrent(subject Subject) bool {
	return f != nil && f(subject)
}

// DecisionOutcome is one defensive result in an ordered evaluation batch.
// Only an allowed outcome contains a capability.
type DecisionOutcome struct {
	request    accesspolicy.Request
	decision   accesspolicy.Decision
	record     Record
	capability *Capability
}

// Request returns the exact request evaluated for this outcome.
func (o DecisionOutcome) Request() accesspolicy.Request { return o.request }

// Decision returns the logical installation-policy decision.
func (o DecisionOutcome) Decision() accesspolicy.Decision { return o.decision }

// Record returns a defensive copy of the sanitized evidence.
func (o DecisionOutcome) Record() Record { return o.record.DeepCopy() }

// Capability returns a defensive value copy only when this outcome is
// allowed. The boolean prevents callers from treating a zero capability as
// executable authorization.
func (o DecisionOutcome) Capability() (Capability, bool) {
	if o.capability == nil {
		return Capability{}, false
	}
	return *o.capability, true
}

// CapabilityPtr returns a defensive pointer copy for adapters that prefer a
// nullable capability representation.
func (o DecisionOutcome) CapabilityPtr() *Capability {
	if o.capability == nil {
		return nil
	}
	copy := *o.capability
	copy.record = copy.record.DeepCopy()
	return &copy
}

// HasCapability reports whether this outcome can authorize I/O.
func (o DecisionOutcome) HasCapability() bool { return o.capability != nil }

// Batch is an immutable-by-convention ordered set of decision outcomes.
type Batch struct {
	outcomes []DecisionOutcome
}

// Outcomes returns a defensive copy of the ordered outcomes. Each outcome's
// record and capability remain immutable through their value APIs.
func (b Batch) Outcomes() []DecisionOutcome {
	copy := make([]DecisionOutcome, len(b.outcomes))
	for index := range b.outcomes {
		copy[index] = b.outcomes[index]
		copy[index].record = b.outcomes[index].record.DeepCopy()
		if b.outcomes[index].capability != nil {
			capability := *b.outcomes[index].capability
			capability.record = capability.record.DeepCopy()
			copy[index].capability = &capability
		}
	}
	return copy
}

// Records returns the ordered defensive evidence records in this batch.
func (b Batch) Records() []Record {
	records := make([]Record, len(b.outcomes))
	for index := range b.outcomes {
		records[index] = b.outcomes[index].record.DeepCopy()
	}
	return records
}

// Len reports the number of evaluated requests.
func (b Batch) Len() int { return len(b.outcomes) }

// Capability is an opaque exact-target authorization value. Its fields are
// private so only Enforcer can mint one; zero values always fail validation.
type Capability struct {
	subject  Subject
	request  accesspolicy.Request
	record   Record
	recorder Recorder
}

// Subject returns the identity bound to this capability.
func (c Capability) Subject() Subject { return c.subject }

// Request returns the exact target bound to this capability.
func (c Capability) Request() accesspolicy.Request { return c.request }

// Record returns the logical decision evidence linked to this capability.
func (c Capability) Record() Record { return c.record.DeepCopy() }

// Valid reports whether the capability has the complete immutable dimensions
// required for an exact resource request.
func (c Capability) Valid() bool {
	return c.subject.Validate() == nil && validRequest(c.request) && c.record.Kind == RecordPolicyDecision && c.record.Outcome == OutcomeAllowed && c.record.Reason == string(accesspolicy.ReasonAllowed)
}

// IsCurrent checks the capability against the runtime freshness verifier. A
// nil verifier, invalid capability, or subject mismatch fails closed.
func (c Capability) IsCurrent(verifier Verifier) bool {
	return c.Valid() && verifier != nil && verifier.IsCurrent(c.subject)
}

// Verify is an alias for IsCurrent used by I/O adapters.
func (c Capability) Verify(verifier Verifier) bool { return c.IsCurrent(verifier) }

// Recorder returns the observer associated with the capability. A nil result
// is never used to grant access; it only means no enforcement evidence sink
// was configured.
func (c Capability) Recorder() Recorder { return c.recorder }

// AuthorizationError is a sanitized internal authorization failure.
type AuthorizationError struct {
	Reason  string
	Message string
	Cause   error
}

// Error implements error without formatting an underlying cause.
func (e *AuthorizationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("authorization: %s", e.Reason)
	}
	return fmt.Sprintf("authorization: %s: %s", e.Reason, e.Message)
}

// Unwrap exposes only programmatic cause matching; the stable message stays
// sanitized.
func (e *AuthorizationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// HasReason reports whether err carries the requested internal reason.
func HasReason(err error, reason string) bool {
	var authorizationErr *AuthorizationError
	return errors.As(err, &authorizationErr) && authorizationErr.Reason == reason
}

const (
	ReasonInvalidSubject  = "InvalidSubject"
	ReasonContextCanceled = "ContextCanceled"
)

func validRequest(request accesspolicy.Request) bool {
	if request.SourceID == "" || request.Kind == "" || !request.Scope.Valid() {
		return false
	}
	if request.Scope == discovery.ScopeNamespaced {
		return request.Namespace != ""
	}
	return request.Namespace == ""
}

func cloneRecords(records []Record) []Record {
	if records == nil {
		return nil
	}
	copy := make([]Record, len(records))
	for index := range records {
		copy[index] = records[index].DeepCopy()
	}
	return copy
}
