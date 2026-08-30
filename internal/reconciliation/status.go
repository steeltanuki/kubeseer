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
	reader  KubeseerReader
	writer  StatusWriter
	tracker *FreshnessTracker
}

var _ StatusPublisherPort = (*StatusPublisher)(nil)

// NewStatusPublisher constructs a direct-read, status-only publisher.
func NewStatusPublisher(reader KubeseerReader, writer StatusWriter, tracker *FreshnessTracker) *StatusPublisher {
	return &StatusPublisher{reader: reader, writer: writer, tracker: tracker}
}

// Publish re-reads the current Kubeseer, preserves spec and conditions, and
// writes the status subresource only when the normalized projection changes.
func (p *StatusPublisher) Publish(ctx context.Context, lease Lease, evaluation statuscontract.Evaluation) error {
	if p == nil || p.reader == nil || p.writer == nil {
		return errors.New("status publisher dependencies are not configured")
	}
	if ctx == nil {
		ctx = context.Background()
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

	composed, err := statuscontract.Compose(lease.Generation, current.Status.Conditions, evaluation)
	if err != nil {
		return &RuntimeError{Stage: "status-compose", Reason: ReasonBuildFailure, Message: "status composition failed", Cause: err}
	}
	candidate := current.DeepCopy()
	candidate.Status = composed
	if SemanticallyEqualStatus(current.Status, candidate.Status) {
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
	return nil
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
