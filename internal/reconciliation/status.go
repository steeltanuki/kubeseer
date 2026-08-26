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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// StatusProjection contains exactly the status fields authored by this
// feature. Conditions are deliberately excluded because this runtime preserves
// them byte-for-byte from the latest persisted object.
type StatusProjection struct {
	ObservedGeneration int64
	Result             *v1alpha1.KubeseerResult
}

// ProjectStatus creates a pure normalized semantic projection. Nil and empty
// result collections compare equal, while a nil result pointer remains
// distinct from a present empty result.
func ProjectStatus(status v1alpha1.KubeseerStatus) StatusProjection {
	return StatusProjection{
		ObservedGeneration: status.ObservedGeneration,
		Result:             normalizeResult(status.Result),
	}
}

// SemanticallyEqualStatus compares the result and observed generation using
// the runtime's recursive normalization rules.
func SemanticallyEqualStatus(left, right v1alpha1.KubeseerStatus) bool {
	return statusProjectionEqual(ProjectStatus(left), ProjectStatus(right))
}

// SemanticallyEqualResult compares two structural results while treating nil
// and empty collection representations as equivalent.
func SemanticallyEqualResult(left, right *v1alpha1.KubeseerResult) bool {
	return statusProjectionEqual(
		StatusProjection{Result: normalizeResult(left)},
		StatusProjection{Result: normalizeResult(right)},
	)
}

func statusProjectionEqual(left, right StatusProjection) bool {
	if left.ObservedGeneration != right.ObservedGeneration {
		return false
	}
	if left.Result == nil || right.Result == nil {
		return left.Result == nil && right.Result == nil
	}
	return equalResult(*left.Result, *right.Result)
}

func equalResult(left, right v1alpha1.KubeseerResult) bool {
	// DeepEqual is sufficient after every optional collection has been
	// normalized; it preserves declaration order and all scalar/pointer states.
	return deepEqualNormalized(left, right)
}

func deepEqualNormalized(left, right v1alpha1.KubeseerResult) bool {
	leftCopy := normalizeResult(&left)
	rightCopy := normalizeResult(&right)
	return resultsEqual(leftCopy, rightCopy)
}

func resultsEqual(left, right *v1alpha1.KubeseerResult) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return reflect.DeepEqual(*left, *right)
}

func normalizeResult(result *v1alpha1.KubeseerResult) *v1alpha1.KubeseerResult {
	if result == nil {
		return nil
	}
	copy := result.DeepCopy()
	if len(copy.Sources) == 0 {
		copy.Sources = nil
	}
	for sourceIndex := range copy.Sources {
		source := &copy.Sources[sourceIndex]
		if len(source.FieldErrors) == 0 {
			source.FieldErrors = nil
		}
		if len(source.Resources) == 0 {
			source.Resources = nil
		}
		for resourceIndex := range source.Resources {
			resource := &source.Resources[resourceIndex]
			if len(resource.Fields) == 0 {
				resource.Fields = nil
			}
			for fieldIndex := range resource.Fields {
				if len(resource.Fields[fieldIndex].Matches) == 0 {
					resource.Fields[fieldIndex].Matches = nil
				}
			}
		}
	}
	return copy
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
func (p *StatusPublisher) Publish(ctx context.Context, lease Lease, result v1alpha1.KubeseerResult) error {
	if p == nil || p.reader == nil || p.writer == nil {
		return errors.New("status publisher dependencies are not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if p.tracker != nil && !p.tracker.IsCurrent(lease) {
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
	if p.tracker != nil && !p.tracker.IsCurrent(lease) {
		return staleRuntimeError("status-publish")
	}

	candidate := current.DeepCopy()
	candidate.Status.ObservedGeneration = lease.Generation
	candidate.Status.Result = result.DeepCopy()
	if SemanticallyEqualStatus(current.Status, candidate.Status) {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if p.tracker != nil && !p.tracker.IsCurrent(lease) {
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
