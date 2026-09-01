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
	"context"
	"errors"
	"sort"

	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/limits"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const defaultPageLimit int64 = 500

const (
	defaultMaxMatchedResources   = 1000
	defaultMaxSelectedInputBytes = 8 * 1024 * 1024
)

// ResourceLister is the only resource-instance I/O boundary used by the
// executor. It exposes LIST and no operation that can mutate or watch objects.
type ResourceLister interface {
	List(context.Context, AuthorizedRead, metav1.ListOptions) (*unstructured.UnstructuredList, error)
}

// PageObservation contains only the scope and returned item count of one
// successful LIST page. It intentionally cannot carry selectors, targets, or
// resource instances across the selection boundary.
type PageObservation struct {
	Scope     discovery.Scope
	ItemCount int
}

// PageObserver observes successful LIST pages without participating in
// selection or authorization decisions.
type PageObserver interface {
	ObservePage(context.Context, PageObservation)
}

// PageObserverFunc adapts a function to PageObserver.
type PageObserverFunc func(context.Context, PageObservation)

// ObservePage implements PageObserver.
func (f PageObserverFunc) ObservePage(ctx context.Context, observation PageObservation) {
	if f != nil {
		f(ctx, observation)
	}
}

// DynamicResourceLister adapts client-go's dynamic client to ResourceLister.
type DynamicResourceLister struct {
	client   dynamic.Interface
	verifier authorization.Verifier
}

// NewDynamicResourceLister creates a list-only dynamic-client adapter.
func NewDynamicResourceLister(client dynamic.Interface, verifier authorization.Verifier) *DynamicResourceLister {
	return &DynamicResourceLister{client: client, verifier: verifier}
}

// List verifies the private permit immediately before addressing exactly
// target.GVR and applying a namespace only for a discovery-reported
// namespaced target.
func (l *DynamicResourceLister) List(ctx context.Context, read AuthorizedRead, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	target := read.Target()
	if l == nil || l.client == nil {
		return nil, errors.New("dynamic resource client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, interruptedError(target.SourceID, err)
	}
	if !read.Valid() {
		return nil, NewSelectionError(target.SourceID, ReasonAuthorizationMismatch, "authorized read does not match its exact capability")
	}
	if l.verifier == nil {
		return nil, NewSelectionError(target.SourceID, ReasonAuthorizationMissing, "authorization freshness verifier is not configured")
	}
	if !read.Capability().IsCurrent(l.verifier) {
		return nil, NewSelectionError(target.SourceID, ReasonAuthorizationStale, "authorization capability is no longer current")
	}
	resource := l.client.Resource(target.GVR)
	switch target.Scope {
	case discovery.ScopeNamespaced:
		if target.Namespace == "" {
			return nil, errors.New("namespaced target has no namespace")
		}
		return resource.Namespace(target.Namespace).List(ctx, options)
	case discovery.ScopeCluster:
		if target.Namespace != "" {
			return nil, errors.New("cluster target has a namespace")
		}
		return resource.List(ctx, options)
	default:
		return nil, errors.New("target scope is invalid")
	}
}

// Executor retrieves and normalizes all objects in an authorized plan.
type Executor struct {
	lister        ResourceLister
	pageLimit     int64
	maxResources  int
	maxInputBytes int64
	verifier      authorization.Verifier
	pageObserver  PageObserver
}

// ExecutorOption customizes executor behavior.
type ExecutorOption func(*Executor)

// WithVerifier configures the freshness verifier required before every LIST
// page and restart.
func WithVerifier(verifier authorization.Verifier) ExecutorOption {
	return func(e *Executor) {
		e.verifier = verifier
	}
}

// WithPageLimit sets the positive LIST page limit used for every target.
func WithPageLimit(limit int64) ExecutorOption {
	return func(e *Executor) {
		if limit > 0 {
			e.pageLimit = limit
		}
	}
}

// WithMaxMatchedResources sets the unique-resource ceiling for one source.
func WithMaxMatchedResources(limit int) ExecutorOption {
	return func(e *Executor) {
		if limit > 0 {
			e.maxResources = limit
		}
	}
}

// WithMaxSelectedInputBytes sets the canonical selected-input ceiling for one
// source. The next first-seen object is checked before it is retained.
func WithMaxSelectedInputBytes(limit int64) ExecutorOption {
	return func(e *Executor) {
		if limit > 0 {
			e.maxInputBytes = limit
		}
	}
}

// WithLimits applies the selection-related views of a resolved profile.
func WithLimits(profile limits.Profile) ExecutorOption {
	return func(e *Executor) {
		if profile.PageSize() > 0 {
			e.pageLimit = profile.PageSize()
		}
		if profile.MaxMatchedResources() > 0 {
			e.maxResources = profile.MaxMatchedResources()
		}
		if profile.MaxSelectedInputBytes() > 0 {
			e.maxInputBytes = profile.MaxSelectedInputBytes()
		}
	}
}

// WithPageObserver reports successful LIST page counts to a passive observer.
func WithPageObserver(observer PageObserver) ExecutorOption {
	return func(e *Executor) {
		e.pageObserver = observer
	}
}

// NewExecutor creates a source selection executor with a bounded default page
// size. It does not load policy or perform discovery.
func NewExecutor(lister ResourceLister, options ...ExecutorOption) *Executor {
	executor := &Executor{lister: lister, pageLimit: defaultPageLimit, maxResources: defaultMaxMatchedResources, maxInputBytes: defaultMaxSelectedInputBytes}
	for _, option := range options {
		if option != nil {
			option(executor)
		}
	}
	return executor
}

// Execute lists every target in order and returns one source-atomic outcome.
// The caller must supply an AuthorizedPlan produced by BindCapabilities.
func (e *Executor) Execute(ctx context.Context, authorized AuthorizedPlan) SelectionOutcome {
	plan := authorized.plan
	outcome := SelectionOutcome{SourceID: plan.SourceID(), Resources: []SelectedResource{}}
	if ctx == nil {
		ctx = context.Background()
	}
	if e == nil || e.lister == nil {
		outcome.Resources = nil
		outcome.Err = NewSelectionError(plan.SourceID(), ReasonReadUnavailable, "resource list is unavailable")
		return outcome
	}

	collected := make([]SelectedResource, 0)
	budget, err := newSelectionBudget(e.maxResources, e.maxInputBytes)
	if err != nil {
		outcome.Resources = nil
		outcome.Err = NewSelectionError(plan.SourceID(), ReasonSelectionLimitExceeded, "selection limits are invalid")
		return outcome
	}
	for _, read := range authorized.reads {
		resources, err := e.listTarget(ctx, plan, read, budget)
		if err != nil {
			outcome.Resources = nil
			outcome.Err = err
			return outcome
		}
		collected = append(collected, resources...)
	}

	outcome.Resources = deduplicateAndSort(collected)
	return outcome
}

type selectionBudget struct {
	seen  map[types.UID]struct{}
	bytes *limits.Accountant
}

func newSelectionBudget(maxResources int, maxInputBytes int64) (*selectionBudget, error) {
	if maxResources <= 0 || maxInputBytes <= 0 {
		return nil, errors.New("selection limits must be positive")
	}
	accountant, err := limits.NewAccountant(maxInputBytes, "selected-input-bytes")
	if err != nil {
		return nil, err
	}
	return &selectionBudget{seen: make(map[types.UID]struct{}, maxResources), bytes: accountant}, nil
}

func (e *Executor) listTarget(ctx context.Context, plan SelectionPlan, read AuthorizedRead, budget *selectionBudget) ([]SelectedResource, *SelectionError) {
	items := make([]SelectedResource, 0)
	targetAccepted := make([]selectedInput, 0)
	continueToken := ""
	restarted := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, interruptedError(plan.SourceID(), err)
		}
		if !read.Valid() {
			return nil, NewSelectionError(plan.SourceID(), ReasonAuthorizationMismatch, "authorized read does not match its exact capability")
		}
		if e.verifier == nil {
			return nil, NewSelectionError(plan.SourceID(), ReasonAuthorizationMissing, "authorization freshness verifier is not configured")
		}
		if !read.Capability().IsCurrent(e.verifier) {
			return nil, NewSelectionError(plan.SourceID(), ReasonAuthorizationStale, "authorization capability is no longer current")
		}
		response, err := e.lister.List(ctx, read, metav1.ListOptions{
			LabelSelector: plan.LabelSelector(),
			FieldSelector: plan.FieldSelector(),
			Limit:         e.pageLimit,
			Continue:      continueToken,
		})
		if err != nil {
			if cause := ctx.Err(); cause != nil {
				return nil, interruptedError(plan.SourceID(), cause)
			}
			if apierrors.IsResourceExpired(err) {
				if restarted {
					return nil, selectionErrorWithCause(plan.SourceID(), ReasonListExpired, "resource list continuation expired", err)
				}
				for _, accepted := range targetAccepted {
					delete(budget.seen, accepted.uid)
					_ = budget.bytes.ReleaseBytes(accepted.size)
				}
				targetAccepted = targetAccepted[:0]
				items = items[:0]
				continueToken = ""
				restarted = true
				continue
			}
			if apierrors.IsForbidden(err) {
				recordReadForbidden(ctx, read)
			}
			return nil, mapListError(ctx, plan.SourceID(), plan.FieldSelector(), err)
		}
		if response == nil {
			return nil, NewSelectionError(plan.SourceID(), ReasonReadUnavailable, "resource list returned no response")
		}
		notifyPageObserver(e.pageObserver, ctx, PageObservation{Scope: read.target.Scope, ItemCount: len(response.Items)})
		for index := range response.Items {
			item := response.Items[index]
			name := item.GetName()
			uid := item.GetUID()
			if name == "" || uid == "" {
				return nil, NewSelectionError(plan.SourceID(), ReasonInvalidObject, "selected object is missing required identity metadata")
			}
			if _, duplicate := budget.seen[uid]; duplicate {
				continue
			}
			copy := item.DeepCopy()
			if len(budget.seen) >= e.maxResources {
				return nil, NewSelectionError(plan.SourceID(), ReasonSelectionLimitExceeded, "selected resource ceiling exceeded")
			}
			size, sizeErr := limits.CanonicalSize(copy.Object)
			if sizeErr != nil {
				return nil, NewSelectionError(plan.SourceID(), ReasonSelectionLimitExceeded, "selected input cannot be canonically sized")
			}
			if err := budget.bytes.AddBytes(int64(size)); err != nil {
				return nil, NewSelectionError(plan.SourceID(), ReasonSelectionLimitExceeded, "selected input byte ceiling exceeded")
			}
			budget.seen[uid] = struct{}{}
			targetAccepted = append(targetAccepted, selectedInput{uid: uid, size: int64(size)})
			items = append(items, SelectedResource{
				Object: copy,
				Provenance: Provenance{
					APIVersion: plan.APIVersion(),
					Kind:       plan.Kind(),
					Namespace:  item.GetNamespace(),
					Name:       name,
					UID:        uid,
				},
			})
		}
		continueToken = response.GetContinue()
		if continueToken == "" {
			return items, nil
		}
	}
}

type selectedInput struct {
	uid  types.UID
	size int64
}

func notifyPageObserver(observer PageObserver, ctx context.Context, observation PageObservation) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.ObservePage(ctx, observation)
}

func recordReadForbidden(ctx context.Context, read AuthorizedRead) {
	capability := read.Capability()
	record := capability.Record()
	record.Kind = authorization.RecordReadForbidden
	record.Outcome = authorization.OutcomeForbidden
	record.Reason = string(ReasonReadForbidden)
	if recorder := capability.Recorder(); recorder != nil {
		recorder.Record(ctx, []authorization.Record{record})
	}
}

func mapListError(ctx context.Context, sourceID, fieldSelector string, err error) *SelectionError {
	var selectionErr *SelectionError
	if errors.As(err, &selectionErr) {
		return selectionErr
	}
	if ctx != nil && ctx.Err() != nil {
		return interruptedError(sourceID, ctx.Err())
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || apierrors.IsTimeout(err) {
		return selectionErrorWithCause(sourceID, ReasonReadInterrupted, "resource read was interrupted", err)
	}
	if apierrors.IsForbidden(err) {
		return selectionErrorWithCause(sourceID, ReasonReadForbidden, "resource read is forbidden", err)
	}
	if fieldSelector != "" && (apierrors.IsBadRequest(err) || apierrors.IsInvalid(err)) {
		return selectionErrorWithCause(sourceID, ReasonUnsupportedSelector, "field selector is unsupported for the resolved resource", err)
	}
	return selectionErrorWithCause(sourceID, ReasonReadUnavailable, "resource list is unavailable", err)
}

func interruptedError(sourceID string, cause error) *SelectionError {
	return &SelectionError{SourceID: sourceID, Reason: ReasonReadInterrupted, Message: "resource read was interrupted", Cause: cause}
}

func deduplicateAndSort(resources []SelectedResource) []SelectedResource {
	sort.SliceStable(resources, func(i, j int) bool {
		left, right := resources[i].Provenance, resources[j].Provenance
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return string(left.UID) < string(right.UID)
	})
	seen := make(map[types.UID]struct{}, len(resources))
	unique := make([]SelectedResource, 0, len(resources))
	for _, resource := range resources {
		if _, exists := seen[resource.Provenance.UID]; exists {
			continue
		}
		seen[resource.Provenance.UID] = struct{}{}
		unique = append(unique, resource)
	}
	return unique
}
