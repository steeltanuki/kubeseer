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
	"context"
	"errors"
	"reflect"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/observability"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatusProjection contains the complete semantic status projection authored
// by this feature. Canonical condition order and API-equivalent result
// collection representations are normalized before comparison.
type StatusProjection struct {
	ObservedGeneration int64
	Conditions         []metav1.Condition
	Summary            *v1alpha1.KubeseerSummary
	ResultHash         string
	Result             *v1alpha1.KubeseerResult
}

// ProjectStatus creates a complete normalized semantic projection. Nil and
// empty result collections compare equal, while a nil result pointer remains
// distinct from a present empty result.
func ProjectStatus(status v1alpha1.KubeseerStatus) StatusProjection {
	var summary *v1alpha1.KubeseerSummary
	if status.Summary != nil {
		summary = status.Summary.DeepCopy()
	}
	return StatusProjection{
		ObservedGeneration: status.ObservedGeneration,
		Conditions:         statuscontract.NormalizeConditions(status.Conditions),
		Summary:            summary,
		ResultHash:         status.ResultHash,
		Result:             statuscontract.NormalizeResult(status.Result),
	}
}

// SemanticallyEqualStatus compares the complete status projection using the
// runtime's recursive normalization rules.
func SemanticallyEqualStatus(left, right v1alpha1.KubeseerStatus) bool {
	return statusProjectionEqual(ProjectStatus(left), ProjectStatus(right))
}

// SemanticallyEqualResult compares two structural results while treating nil
// and empty collection representations as equivalent.
func SemanticallyEqualResult(left, right *v1alpha1.KubeseerResult) bool {
	return statuscontract.SemanticResultEqual(left, right)
}

func statusProjectionEqual(left, right StatusProjection) bool {
	return reflect.DeepEqual(left, right)
}

// StatusPublisher performs one guarded status-subresource update at most.
type StatusPublisher struct {
	reader         KubeseerReader
	writer         StatusWriter
	tracker        *FreshnessTracker
	observer       *observability.Observer
	maxStatusBytes int64
}

var _ StatusPublisherPort = (*StatusPublisher)(nil)

// StatusPublisherOption adds passive instrumentation without changing the
// status publication contract.
type StatusPublisherOption func(*StatusPublisher)

// WithStatusObserver reports one authoritative publication outcome and emits
// Events after successful semantic writes.
func WithStatusObserver(observer *observability.Observer) StatusPublisherOption {
	return func(publisher *StatusPublisher) {
		publisher.observer = observer
	}
}

// WithMaxStatusBytes applies the manager-wide canonical status ceiling. A
// non-positive value is retained so direct callers receive the same defensive
// StatusLimitInvalid result as any other invalid publisher configuration.
func WithMaxStatusBytes(maxBytes int64) StatusPublisherOption {
	return func(publisher *StatusPublisher) {
		publisher.maxStatusBytes = maxBytes
	}
}

// NewStatusPublisher constructs a direct-read, status-only publisher.
func NewStatusPublisher(reader KubeseerReader, writer StatusWriter, tracker *FreshnessTracker, options ...StatusPublisherOption) *StatusPublisher {
	publisher := &StatusPublisher{reader: reader, writer: writer, tracker: tracker, maxStatusBytes: limits.DefaultMaxStatusBytes}
	for _, option := range options {
		if option != nil {
			option(publisher)
		}
	}
	return publisher
}

// Publish re-reads the current Kubeseer, preserves spec and conditions, and
// writes the status subresource only when the normalized projection changes.
func (p *StatusPublisher) Publish(ctx context.Context, lease Lease, evaluation statuscontract.Evaluation) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	publication := observability.StatusFailed
	reason := observability.ReasonStatusUnavailable
	if p != nil && p.observer != nil {
		defer func() {
			if err != nil {
				publication, reason = classifyStatusPublication(err)
				if observation, ok := statusLimitObservation(err, p.maxStatusBytes); ok {
					p.observer.ObserveLimit(ctx, observation)
				}
			}
			p.observer.ObserveStatus(ctx, publication, reason)
		}()
	}
	if p == nil || p.reader == nil || p.writer == nil {
		return errors.New("status publisher dependencies are not configured")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if p.tracker != nil && !p.tracker.IsLeaseCurrent(lease) {
		return staleRuntimeError("status-publish")
	}

	current := new(v1alpha1.Kubeseer)
	if err := p.reader.Get(ctx, lease.Key, current); err != nil {
		if apierrors.IsNotFound(err) || ctx.Err() != nil {
			return err
		}
		return transientRuntimeError("status-read", "", ReasonStatusUnavailable, "current Kubeseer status is unavailable", err)
	}
	if current.DeletionTimestamp != nil {
		publication = observability.StatusSkipped
		reason = observability.ReasonObjectDeleting
		return nil
	}
	if lease.UID != current.UID {
		return staleRuntimeError("status-publish")
	}
	if current.Generation != lease.Generation {
		return staleRuntimeError("status-publish")
	}
	if p.tracker != nil && !p.tracker.IsLeaseCurrent(lease) {
		return staleRuntimeError("status-publish")
	}

	composed, err := p.composeBoundedStatus(lease.Generation, current.Status.Conditions, evaluation)
	if err != nil {
		return err
	}
	if p.observer != nil && statusContainsReason(composed.Conditions, statuscontract.ReasonResultLimitExceeded) {
		p.observer.ObserveLimit(ctx, observability.LimitObservation{
			Stage:     observability.StageCompose,
			Reason:    observability.ReasonResultLimitExceeded,
			Dimension: observability.LimitDimensionStatusBytes,
			Ceiling:   normalizedStatusCeiling(p.maxStatusBytes),
		})
	}
	candidate := current.DeepCopy()
	candidate.Status = composed
	if SemanticallyEqualStatus(current.Status, candidate.Status) {
		publication = observability.StatusSkipped
		reason = statusReasonFromConditions(candidate.Status.Conditions)
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if p.tracker != nil && !p.tracker.IsLeaseCurrent(lease) {
		return staleRuntimeError("status-publish")
	}
	if err := p.writer.Update(ctx, candidate); err != nil {
		return classifyStatusWriteError(err)
	}
	publication = observability.StatusWritten
	reason = statusReasonFromConditions(candidate.Status.Conditions)
	if p.observer != nil {
		p.observer.RecordStatusEvent(ctx, candidate, candidate.Status.Conditions)
	}
	return nil
}

func statusLimitObservation(err error, ceiling int64) (observability.LimitObservation, bool) {
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		return observability.LimitObservation{}, false
	}
	var reason observability.Reason
	switch runtimeErr.Reason {
	case ReasonStatusLimitInvalid:
		reason = observability.ReasonStatusLimitInvalid
	default:
		return observability.LimitObservation{}, false
	}
	return observability.LimitObservation{
		Stage:     observability.StageCompose,
		Reason:    reason,
		Dimension: observability.LimitDimensionStatusBytes,
		Ceiling:   normalizedStatusCeiling(ceiling),
	}, true
}

func normalizedStatusCeiling(ceiling int64) int64 {
	if ceiling < 0 {
		return 0
	}
	return ceiling
}

func statusContainsReason(conditions []metav1.Condition, reason string) bool {
	for _, condition := range conditions {
		if condition.Reason == reason {
			return true
		}
	}
	return false
}

func (p *StatusPublisher) composeBoundedStatus(generation int64, persisted []metav1.Condition, evaluation statuscontract.Evaluation) (v1alpha1.KubeseerStatus, error) {
	if p == nil || p.maxStatusBytes <= 0 {
		return v1alpha1.KubeseerStatus{}, &RuntimeError{Stage: "status-compose", Reason: ReasonStatusLimitInvalid, Message: "configured status limit is invalid"}
	}
	composed, err := statuscontract.Compose(generation, persisted, evaluation)
	if err != nil {
		return v1alpha1.KubeseerStatus{}, &RuntimeError{Stage: "status-compose", Reason: ReasonBuildFailure, Message: "status composition failed", Cause: err}
	}
	size, err := limits.CanonicalSize(composed)
	if err != nil {
		return v1alpha1.KubeseerStatus{}, &RuntimeError{Stage: "status-compose", Reason: ReasonStatusLimitInvalid, Message: "status candidate could not be measured", Cause: err}
	}
	if int64(size) <= p.maxStatusBytes {
		return composed, nil
	}
	if evaluation.ConfigurationBudgetExceeded {
		return v1alpha1.KubeseerStatus{}, &RuntimeError{Stage: "status-compose", Reason: ReasonStatusLimitInvalid, Message: "configured status limit cannot contain configuration rejection"}
	}
	compact, err := statuscontract.ComposeResultLimitExceeded(generation, persisted, evaluation)
	if err != nil {
		return v1alpha1.KubeseerStatus{}, &RuntimeError{Stage: "status-compose", Reason: ReasonBuildFailure, Message: "compact status composition failed", Cause: err}
	}
	compactSize, err := limits.CanonicalSize(compact)
	if err != nil || int64(compactSize) > p.maxStatusBytes {
		return v1alpha1.KubeseerStatus{}, &RuntimeError{Stage: "status-compose", Reason: ReasonStatusLimitInvalid, Message: "configured status limit cannot contain compact status", Cause: err}
	}
	return compact, nil
}

func classifyStatusPublication(err error) (observability.StatusOutcome, observability.Reason) {
	if err == nil {
		return observability.StatusSkipped, observability.ReasonEvaluationSucceeded
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return observability.StatusSkipped, observability.ReasonReadInterrupted
	}
	if errors.Is(err, ErrStaleLease) {
		return observability.StatusSkipped, observability.ReasonStaleLease
	}
	if apierrors.IsConflict(err) {
		return observability.StatusConflicted, observability.ReasonStatusConflict
	}
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) {
		switch runtimeErr.Reason {
		case ReasonStatusConflict:
			return observability.StatusConflicted, observability.ReasonStatusConflict
		case ReasonBuildFailure:
			return observability.StatusFailed, observability.ReasonBuildFailure
		case ReasonStatusUnavailable:
			return observability.StatusFailed, observability.ReasonStatusUnavailable
		default:
			return observability.StatusFailed, observability.Reason(runtimeErr.Reason)
		}
	}
	return observability.StatusFailed, observability.ReasonStatusUnavailable
}

func statusReasonFromConditions(conditions []metav1.Condition) observability.Reason {
	for _, conditionType := range []string{
		statuscontract.ConditionAccepted,
		statuscontract.ConditionAuthorized,
		statuscontract.ConditionSourcesResolved,
		statuscontract.ConditionReady,
	} {
		found := false
		for _, condition := range conditions {
			if condition.Type != conditionType {
				continue
			}
			if found || condition.Reason == "" {
				return observability.ReasonInternalError
			}
			found = true
			if condition.Status != metav1.ConditionTrue {
				return observability.Reason(condition.Reason)
			}
		}
		if !found {
			return observability.ReasonInternalError
		}
	}
	return observability.ReasonEvaluationSucceeded
}

func classifyStatusWriteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if apierrors.IsConflict(err) {
		return transientRuntimeError("status-write", "", ReasonStatusConflict, "status update conflicted with a newer resource version", err)
	}
	if apierrors.IsForbidden(err) || apierrors.IsInvalid(err) {
		return &RuntimeError{Stage: "status-write", Reason: ReasonStatusUnavailable, Message: "status update was rejected", Cause: err}
	}
	return transientRuntimeError("status-write", "", ReasonStatusUnavailable, "status update is temporarily unavailable", err)
}
