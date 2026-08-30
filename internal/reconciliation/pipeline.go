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
	"fmt"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type plannedSource struct {
	source          v1alpha1.KubeseerSource
	plan            selection.SelectionPlan
	authorized      selection.AuthorizedPlan
	selectionErr    *selection.SelectionError
	canExecute      bool
	retryableErr    error
	authorizedSet   []AuthorizedRoute
	authorizations  []authorization.DecisionOutcome
	assessment      statuscontract.SourceAssessment
	originalPlanErr error
}

// Reconcile implements controller-runtime's namespaced request boundary.
func (r *Runtime) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if r == nil {
		return reconcile.Result{}, errors.New("reconciliation runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	release := r.gate.lock(request.NamespacedName)
	defer release()
	return r.reconcileKey(ctx, request.NamespacedName)
}

func (r *Runtime) reconcileKey(ctx context.Context, key types.NamespacedName) (reconcile.Result, error) {
	object := new(v1alpha1.Kubeseer)
	if err := r.deps.Reader.Get(ctx, key, object); err != nil {
		if apierrors.IsNotFound(err) {
			r.deps.Routes.RemoveOwner(key)
			r.deps.Tracker.MarkAbsent(key)
			return reconcile.Result{}, nil
		}
		if ctx.Err() != nil {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, transientRuntimeError("kubeseer-read", "", ReasonReadUnavailable, "Kubeseer is unavailable", err)
	}
	if object.DeletionTimestamp != nil {
		r.deps.Tracker.Observe(object)
		r.deps.Routes.RemoveOwner(key)
		r.deps.Tracker.MarkAbsent(key)
		return reconcile.Result{}, nil
	}

	lease, child, releaseLease, err := r.deps.Tracker.Acquire(ctx, key, object.UID, object.Generation)
	if err != nil {
		if errors.Is(err, ErrStaleLease) || ctx.Err() != nil {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	defer releaseLease()

	if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
		return reconcile.Result{}, nil
	}
	sources := object.Spec.Sources
	// A source-free object has no observation target. It receives the present
	// empty result without loading or evaluating the policy, but still follows
	// the same route replacement and publication guards.
	var snapshot accesspolicy.Snapshot
	if len(sources) > 0 {
		snapshot = accesspolicy.Load(child, r.deps.PolicySource)
		if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
			return reconcile.Result{}, nil
		}
	}
	planned := make([]plannedSource, len(sources))
	var retryable []error
	if len(sources) > 0 && snapshot.TerminalReason() == accesspolicy.ReasonPolicyUnavailable {
		retryable = append(retryable, transientRuntimeError("policy-load", "", ReasonPolicyUnavailable, "installation policy is unavailable", nil))
	}
	var subject authorization.Subject
	if len(sources) > 0 {
		var subjectErr error
		subject, subjectErr = authorization.NewSubject(key, lease.UID, lease.Generation, lease.PolicyEpoch)
		if subjectErr != nil {
			return reconcile.Result{}, nil
		}
	}

	for index, source := range sources {
		if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
			return reconcile.Result{}, nil
		}
		entry := plannedSource{
			source: source,
			assessment: statuscontract.SourceAssessment{
				Index:         index,
				Configuration: configurationOutcome(source),
				Authorization: statuscontract.AuthorizationNotEvaluatedOutcome,
				Resolution:    statuscontract.ResolutionNotEvaluatedOutcome,
			},
		}
		plan, planErr := r.deps.Planner.Plan(child, object.Namespace, source)
		if planErr != nil {
			entry.originalPlanErr = planErr
			entry.selectionErr = mapPlanningError(source.ID, planErr)
			entry.assessment = assessmentForPlanningError(entry.assessment, planErr)
			if isDiscoveryUnavailable(planErr) {
				entry.retryableErr = transientRuntimeError("discovery", source.ID, ReasonReadUnavailable, "source discovery is unavailable", nil)
				retryable = append(retryable, entry.retryableErr)
			}
			planned[index] = entry
			continue
		}
		entry.plan = plan
		entry.assessment.Resolution = statuscontract.ResolutionResolvedOutcome
		requests := make([]accesspolicy.Request, 0, len(plan.Targets()))
		for _, target := range plan.Targets() {
			requests = append(requests, selection.RequestForTarget(target))
		}
		batch, evaluateErr := r.deps.Enforcer.EvaluateBatch(child, subject, snapshot, requests)
		if evaluateErr != nil {
			if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
				return reconcile.Result{}, nil
			}
			entry.selectionErr = selection.NewSelectionError(source.ID, selection.ReasonAuthorizationMismatch, "source authorization could not be evaluated")
			planned[index] = entry
			continue
		}
		entry.authorizations = batch.Outcomes()
		for _, outcome := range entry.authorizations {
			decision := outcome.Decision()
			if decision.Allowed {
				continue
			}
			entry.assessment.Authorization = authorizationOutcomeForDecision(decision)
			if !decision.Allowed && entry.selectionErr == nil {
				entry.selectionErr = selection.NewSelectionError(source.ID, selection.ReasonAuthorizationDenied, policyDecisionMessage(decision))
			}
		}
		if entry.assessment.Authorization == statuscontract.AuthorizationNotEvaluatedOutcome {
			entry.assessment.Authorization = statuscontract.AuthorizationAllowedOutcome
		}
		bound, bindErr := selection.BindCapabilities(plan, entry.authorizations)
		if bindErr != nil {
			if entry.selectionErr == nil {
				entry.selectionErr = mapBindingError(source.ID, bindErr)
			}
			if entry.assessment.Authorization == statuscontract.AuthorizationAllowedOutcome || entry.assessment.Authorization == statuscontract.AuthorizationNotEvaluatedOutcome {
				entry.assessment.Authorization = statuscontract.AuthorizationUnavailableOutcome
			}
			planned[index] = entry
			continue
		}
		entry.authorized = bound
		entry.canExecute = true
		for targetIndex, target := range plan.Targets() {
			capability, ok := entry.authorizations[targetIndex].Capability()
			if !ok {
				entry.selectionErr = selection.NewSelectionError(source.ID, selection.ReasonAuthorizationMismatch, "authorized route has no matching capability")
				entry.canExecute = false
				break
			}
			route, routeErr := NewAuthorizedRoute(target, capability)
			if routeErr != nil {
				entry.selectionErr = selection.NewSelectionError(source.ID, selection.ReasonAuthorizationMismatch, "authorized route could not be constructed")
				entry.canExecute = false
				break
			}
			entry.authorizedSet = append(entry.authorizedSet, route)
		}
		planned[index] = entry
	}

	if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
		return reconcile.Result{}, nil
	}
	routes := make([]AuthorizedRoute, 0)
	for _, entry := range planned {
		if entry.canExecute && entry.selectionErr == nil {
			routes = append(routes, entry.authorizedSet...)
		}
	}
	if err := r.deps.Routes.Replace(lease, routes); err != nil {
		if errors.Is(err, ErrStaleLease) || IsRetryable(err) == false && !r.deps.Tracker.IsLeaseCurrent(lease) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
		return reconcile.Result{}, nil
	}

	selectionOutcomes := make([]selection.SelectionOutcome, len(planned))
	for index, entry := range planned {
		selectionOutcomes[index].SourceID = entry.source.ID
		if entry.selectionErr != nil || !entry.canExecute {
			selectionOutcomes[index].Err = entry.selectionErr
			continue
		}
		if child.Err() != nil {
			return reconcile.Result{}, nil
		}
		selectionOutcomes[index] = r.deps.Executor.Execute(child, entry.authorized)
		if selectionOutcomes[index].Err != nil {
			if selection.HasReason(selectionOutcomes[index].Err, selection.ReasonReadForbidden) {
				entry.assessment.Authorization = statuscontract.AuthorizationReadForbiddenOutcome
				planned[index] = entry
			}
			if retryableSelectionError(selectionOutcomes[index].Err) {
				retryable = append(retryable, transientRuntimeError("selection", entry.source.ID, ReasonReadUnavailable, "source selection is temporarily unavailable", selectionOutcomes[index].Err))
			}
		}
	}
	if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
		return reconcile.Result{}, nil
	}

	extractionInputs := make([]extraction.SourceInput, len(sources))
	for index, source := range sources {
		extractionInputs[index] = extraction.SourceInput{Source: source, Selection: selectionOutcomes[index]}
	}
	extracted := extraction.ExtractBatch(child, extractionInputs)
	if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
		return reconcile.Result{}, nil
	}
	typedInputs := make([]typedoutput.SourceInput, len(sources))
	for index, source := range sources {
		typedInputs[index] = typedoutput.SourceInput{Source: source, Extraction: extracted[index]}
	}
	converted := typedoutput.ConvertBatch(child, typedInputs)
	if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
		return reconcile.Result{}, nil
	}
	result, err := typedoutput.BuildResult(converted)
	if err != nil {
		buildErr := transientRuntimeError("result-build", "", ReasonBuildFailure, "candidate result construction failed", err)
		unavailable := statuscontract.Evaluation{
			Sources:             assessmentsForPlanned(planned),
			GlobalAuthorization: globalAuthorizationOutcome(snapshot, len(sources)),
			ResultUnavailable:   true,
		}
		if publishErr := r.deps.Publisher.Publish(child, lease, unavailable); publishErr != nil {
			if errors.Is(publishErr, ErrStaleLease) || errors.Is(publishErr, context.Canceled) || errors.Is(publishErr, context.DeadlineExceeded) {
				return reconcile.Result{}, buildErr
			}
			return reconcile.Result{}, errors.Join(buildErr, publishErr)
		}
		return reconcile.Result{}, buildErr
	}
	if child.Err() != nil || !r.deps.Tracker.IsLeaseCurrent(lease) {
		return reconcile.Result{}, nil
	}

	evaluation := statuscontract.Evaluation{
		Result:              &result,
		Sources:             assessmentsForPlanned(planned),
		GlobalAuthorization: globalAuthorizationOutcome(snapshot, len(sources)),
	}
	if err := r.deps.Publisher.Publish(child, lease, evaluation); err != nil {
		if errors.Is(err, ErrStaleLease) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return reconcile.Result{}, nil
		}
		if IsRetryable(err) {
			retryable = append(retryable, err)
		} else {
			return reconcile.Result{}, err
		}
	}
	if len(retryable) != 0 {
		return reconcile.Result{}, errors.Join(retryable...)
	}
	return reconcile.Result{}, nil
}

func mapPlanningError(sourceID string, err error) *selection.SelectionError {
	if err == nil {
		return nil
	}
	var selectionErr *selection.SelectionError
	if errors.As(err, &selectionErr) {
		return selection.NewSelectionError(sourceID, selectionErr.Reason, selectionErr.Message)
	}
	var resolutionErr *discovery.ResolutionError
	if errors.As(err, &resolutionErr) {
		return selection.NewSelectionError(sourceID, selection.ReasonInvalidSource, "source discovery failed: "+string(resolutionErr.Reason))
	}
	return selection.NewSelectionError(sourceID, selection.ReasonInvalidSource, "source planning failed")
}

func isDiscoveryUnavailable(err error) bool {
	return discovery.HasReason(err, discovery.ReasonDiscoveryUnavailable)
}

func mapBindingError(sourceID string, err error) *selection.SelectionError {
	var selectionErr *selection.SelectionError
	if errors.As(err, &selectionErr) {
		return selection.NewSelectionError(sourceID, selectionErr.Reason, selectionErr.Message)
	}
	return selection.NewSelectionError(sourceID, selection.ReasonAuthorizationMismatch, "source authorization could not be bound")
}

func policyDecisionMessage(decision accesspolicy.Decision) string {
	if decision.Reason == "" {
		return "source target is denied by the installation policy"
	}
	return fmt.Sprintf("source target is denied by installation policy: %s", decision.Reason)
}

func retryableSelectionError(err error) bool {
	var selectionErr *selection.SelectionError
	if !errors.As(err, &selectionErr) {
		return false
	}
	switch selectionErr.Reason {
	case selection.ReasonReadUnavailable, selection.ReasonListExpired:
		return true
	default:
		return false
	}
}

func configurationOutcome(source v1alpha1.KubeseerSource) statuscontract.ConfigurationOutcome {
	if _, err := extraction.CompileSource(source); err != nil {
		return statuscontract.ConfigurationInvalidOutcome
	}
	if len(typedoutput.CompileSource(source).Failures()) != 0 {
		return statuscontract.ConfigurationInvalidOutcome
	}
	return statuscontract.ConfigurationAcceptedOutcome
}

func assessmentForPlanningError(assessment statuscontract.SourceAssessment, err error) statuscontract.SourceAssessment {
	var resolutionErr *discovery.ResolutionError
	if errors.As(err, &resolutionErr) {
		switch resolutionErr.Reason {
		case discovery.ReasonDiscoveryUnavailable:
			assessment.Authorization = statuscontract.AuthorizationUnavailableOutcome
			assessment.Resolution = statuscontract.ResolutionUnavailableOutcome
		case discovery.ReasonUnknownType, discovery.ReasonAmbiguousResource:
			assessment.Authorization = statuscontract.AuthorizationNotEvaluatedOutcome
			assessment.Resolution = statuscontract.ResolutionFailedOutcome
		case discovery.ReasonInvalidDescriptor:
			assessment.Configuration = statuscontract.ConfigurationInvalidOutcome
			assessment.Authorization = statuscontract.AuthorizationNotEvaluatedOutcome
			assessment.Resolution = statuscontract.ResolutionNotEvaluatedOutcome
		default:
			assessment.Configuration = statuscontract.ConfigurationInvalidOutcome
			assessment.Authorization = statuscontract.AuthorizationNotEvaluatedOutcome
			assessment.Resolution = statuscontract.ResolutionNotEvaluatedOutcome
		}
		return assessment
	}

	var selectionErr *selection.SelectionError
	if errors.As(err, &selectionErr) {
		switch selectionErr.Reason {
		case selection.ReasonInvalidSelector, selection.ReasonInvalidNamespaceScope:
			assessment.Configuration = statuscontract.ConfigurationInvalidOutcome
			assessment.Authorization = statuscontract.AuthorizationNotEvaluatedOutcome
			assessment.Resolution = statuscontract.ResolutionResolvedOutcome
		default:
			assessment.Configuration = statuscontract.ConfigurationInvalidOutcome
			assessment.Authorization = statuscontract.AuthorizationNotEvaluatedOutcome
			assessment.Resolution = statuscontract.ResolutionNotEvaluatedOutcome
		}
		return assessment
	}

	assessment.Configuration = statuscontract.ConfigurationInvalidOutcome
	assessment.Authorization = statuscontract.AuthorizationNotEvaluatedOutcome
	assessment.Resolution = statuscontract.ResolutionNotEvaluatedOutcome
	return assessment
}

func authorizationOutcomeForDecision(decision accesspolicy.Decision) statuscontract.AuthorizationOutcome {
	if decision.Allowed && decision.Reason == accesspolicy.ReasonAllowed {
		return statuscontract.AuthorizationAllowedOutcome
	}
	switch decision.Reason {
	case accesspolicy.ReasonPolicyMissing:
		return statuscontract.AuthorizationPolicyMissingOutcome
	case accesspolicy.ReasonPolicyInvalid:
		return statuscontract.AuthorizationPolicyInvalidOutcome
	case accesspolicy.ReasonPolicyUnavailable:
		return statuscontract.AuthorizationUnavailableOutcome
	default:
		return statuscontract.AuthorizationDeniedOutcome
	}
}

func globalAuthorizationOutcome(snapshot accesspolicy.Snapshot, sourceCount int) *statuscontract.AuthorizationOutcome {
	if sourceCount == 0 {
		return nil
	}
	var outcome statuscontract.AuthorizationOutcome
	switch snapshot.TerminalReason() {
	case accesspolicy.ReasonPolicyMissing:
		outcome = statuscontract.AuthorizationPolicyMissingOutcome
	case accesspolicy.ReasonPolicyInvalid:
		outcome = statuscontract.AuthorizationPolicyInvalidOutcome
	case accesspolicy.ReasonPolicyUnavailable:
		outcome = statuscontract.AuthorizationUnavailableOutcome
	default:
		return nil
	}
	return &outcome
}

func assessmentsForPlanned(planned []plannedSource) []statuscontract.SourceAssessment {
	assessments := make([]statuscontract.SourceAssessment, len(planned))
	for index, entry := range planned {
		assessments[index] = entry.assessment
	}
	return assessments
}
