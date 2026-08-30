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

	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
)

// Enforcer evaluates exact policy requests and mints capabilities only for
// allowed outcomes from the supplied current snapshot.
type Enforcer struct {
	recorder Recorder
}

// NewEnforcer constructs an enforcer. A nil recorder is normalized to the
// explicit no-op implementation and can never affect a decision.
func NewEnforcer(recorder Recorder) *Enforcer {
	if recorder == nil {
		recorder = NoopRecorder{}
	}
	return &Enforcer{recorder: recorder}
}

// Recorder returns the normalized recorder configured for this enforcer.
func (e *Enforcer) Recorder() Recorder {
	if e == nil || e.recorder == nil {
		return NoopRecorder{}
	}
	return e.recorder
}

// EvaluateBatch evaluates requests in their supplied order. It records the
// complete ordered decision batch before returning; only allowed outcomes
// contain opaque capabilities.
func (e *Enforcer) EvaluateBatch(ctx context.Context, subject Subject, snapshot accesspolicy.Snapshot, requests []accesspolicy.Request) (Batch, error) {
	if err := subject.Validate(); err != nil {
		return Batch{}, &AuthorizationError{Reason: ReasonInvalidSubject, Message: "authorization subject is incomplete", Cause: err}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Batch{}, &AuthorizationError{Reason: ReasonContextCanceled, Message: "authorization evaluation was canceled", Cause: err}
	}

	recorder := e.Recorder()
	outcomes := make([]DecisionOutcome, len(requests))
	records := make([]Record, len(requests))
	for index, request := range requests {
		if err := ctx.Err(); err != nil {
			return Batch{}, &AuthorizationError{Reason: ReasonContextCanceled, Message: "authorization evaluation was canceled", Cause: err}
		}
		decision := snapshot.Evaluate(request)
		record := recordFor(subject, request, snapshot, decision)
		outcome := DecisionOutcome{request: request, decision: decision, record: record}
		if decision.Allowed && decision.Reason == accesspolicy.ReasonAllowed {
			capability := &Capability{
				subject:  subject,
				request:  request,
				record:   record.DeepCopy(),
				recorder: recorder,
			}
			outcome.capability = capability
		}
		outcomes[index] = outcome
		records[index] = record.DeepCopy()
	}
	recorder.Record(ctx, records)
	return Batch{outcomes: outcomes}, nil
}

func recordFor(subject Subject, request accesspolicy.Request, snapshot accesspolicy.Snapshot, decision accesspolicy.Decision) Record {
	state := PolicyReady
	switch snapshot.TerminalReason() {
	case accesspolicy.ReasonPolicyMissing:
		state = PolicyMissing
	case accesspolicy.ReasonPolicyInvalid:
		state = PolicyInvalid
	case accesspolicy.ReasonPolicyUnavailable:
		state = PolicyUnavailable
	}
	outcome := OutcomeDenied
	if decision.Allowed && decision.Reason == accesspolicy.ReasonAllowed {
		outcome = OutcomeAllowed
	} else if decision.Reason == accesspolicy.ReasonPolicyUnavailable {
		outcome = OutcomeUnavailable
	}
	var identity *accesspolicy.PolicyIdentity
	if value, ok := snapshot.PolicyIdentity(); ok {
		identity = &value
	}
	return Record{
		Kind:           RecordPolicyDecision,
		Subject:        subject,
		SourceID:       request.SourceID,
		APIGroup:       request.APIGroup,
		KindName:       request.Kind,
		Scope:          request.Scope,
		Namespace:      request.Namespace,
		PolicyState:    state,
		PolicyIdentity: identity,
		Outcome:        outcome,
		Reason:         string(decision.Reason),
	}
}
