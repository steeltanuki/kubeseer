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

package observability

import (
	"time"

	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"k8s.io/apimachinery/pkg/types"
)

// EventCode is the closed event vocabulary emitted by observability.
type EventCode string

const (
	EventReconciliationStarted        EventCode = "ReconciliationStarted"
	EventReconciliationSkipped        EventCode = "ReconciliationSkipped"
	EventReconciliationRetryScheduled EventCode = "ReconciliationRetryScheduled"
	EventReconciliationFailed         EventCode = "ReconciliationFailed"
	EventReconciliationCompleted      EventCode = "ReconciliationCompleted"
	EventSourceFailed                 EventCode = "SourceFailed"
	EventStatusUpdateWritten          EventCode = "StatusUpdateWritten"
	EventStatusUpdateSkipped          EventCode = "StatusUpdateSkipped"
	EventAuthorizationDecision        EventCode = "AuthorizationDecision"
	EventAuthorizationReadForbidden   EventCode = "AuthorizationReadForbidden"
	EventSourceWatchStopped           EventCode = "SourceWatchStopped"
	EventSourceWatchRestarted         EventCode = "SourceWatchRestarted"
	EventJSONPathFailed               EventCode = "JSONPathFailed"
)

// Outcome is the closed outcome vocabulary used by logs and metric labels.
type Outcome string

const (
	OutcomeStarted     Outcome = "started"
	OutcomeCompleted   Outcome = "completed"
	OutcomeSkipped     Outcome = "skipped"
	OutcomeRetry       Outcome = "retry"
	OutcomeFailed      Outcome = "failed"
	OutcomeDegraded    Outcome = "degraded"
	OutcomeSucceeded   Outcome = "succeeded"
	OutcomeWritten     Outcome = "written"
	OutcomeConflict    Outcome = "conflicted"
	OutcomeUnavailable Outcome = "unavailable"
	OutcomeAllowed     Outcome = "allowed"
	OutcomeDenied      Outcome = "denied"
	OutcomeForbidden   Outcome = "forbidden"
)

// Reason is the sanitized diagnostic vocabulary. Unknown values normalize to
// InternalError rather than becoming user-controlled labels or messages.
type Reason string

const (
	ReasonInternalError             Reason = "InternalError"
	ReasonAllowed                   Reason = "Allowed"
	ReasonReconciliationStarted     Reason = "ReconciliationStarted"
	ReasonObjectAbsent              Reason = "ObjectAbsent"
	ReasonObjectDeleting            Reason = "ObjectDeleting"
	ReasonReadUnavailable           Reason = "ReadUnavailable"
	ReasonPolicyUnavailable         Reason = "PolicyUnavailable"
	ReasonPolicyMissing             Reason = "PolicyMissing"
	ReasonPolicyInvalid             Reason = "PolicyInvalid"
	ReasonStatusConflict            Reason = "StatusConflict"
	ReasonStatusUnavailable         Reason = "StatusUnavailable"
	ReasonStaleLease                Reason = "StaleLease"
	ReasonInvalidSetup              Reason = "InvalidSetup"
	ReasonBuildFailure              Reason = "BuildFailure"
	ReasonInvalidSource             Reason = "InvalidSource"
	ReasonInvalidNamespaceScope     Reason = "InvalidNamespaceScope"
	ReasonInvalidScope              Reason = "InvalidScope"
	ReasonInvalidSelector           Reason = "InvalidSelector"
	ReasonUnsupportedSelector       Reason = "UnsupportedSelector"
	ReasonAuthorizationMissing      Reason = "AuthorizationMissing"
	ReasonAuthorizationDenied       Reason = "AuthorizationDenied"
	ReasonAuthorizationMismatch     Reason = "AuthorizationMismatch"
	ReasonAuthorizationStale        Reason = "AuthorizationStale"
	ReasonReadForbidden             Reason = "ReadForbidden"
	ReasonReadInterrupted           Reason = "ReadInterrupted"
	ReasonListExpired               Reason = "ListExpired"
	ReasonInvalidObject             Reason = "InvalidObject"
	ReasonDiscoveryUnavailable      Reason = "DiscoveryUnavailable"
	ReasonUnknownType               Reason = "UnknownType"
	ReasonAmbiguousResource         Reason = "AmbiguousResource"
	ReasonInvalidDescriptor         Reason = "InvalidDescriptor"
	ReasonConfigurationAccepted     Reason = "ConfigurationAccepted"
	ReasonInvalidConfiguration      Reason = "InvalidConfiguration"
	ReasonAuthorizationSucceeded    Reason = "AuthorizationSucceeded"
	ReasonAuthorizationUnavailable  Reason = "AuthorizationUnavailable"
	ReasonAuthorizationNotEvaluated Reason = "AuthorizationNotEvaluated"
	ReasonInvalidRequest            Reason = "InvalidRequest"
	ReasonResourceDenied            Reason = "ResourceDenied"
	ReasonClusterScopeDenied        Reason = "ClusterScopeDenied"
	ReasonNamespaceDenied           Reason = "NamespaceDenied"
	ReasonResolutionSucceeded       Reason = "ResolutionSucceeded"
	ReasonResolutionFailed          Reason = "ResolutionFailed"
	ReasonResolutionNotEvaluated    Reason = "ResolutionNotEvaluated"
	ReasonResolutionUnavailable     Reason = "ResolutionUnavailable"
	ReasonEvaluationSucceeded       Reason = "EvaluationSucceeded"
	ReasonEvaluationDegraded        Reason = "EvaluationDegraded"
	ReasonEvaluationUnavailable     Reason = "EvaluationUnavailable"
	ReasonInvalidExtraction         Reason = "InvalidExtraction"
	ReasonExtractionFailed          Reason = "ExtractionFailed"
	ReasonInvalidValue              Reason = "InvalidValue"
	ReasonOperatorFailed            Reason = "OperatorFailed"
	ReasonAggregationFailed         Reason = "AggregationFailed"
	ReasonAggregationLimit          Reason = "AggregationLimit"
	ReasonInvalidExpression         Reason = "InvalidExpression"
	ReasonUnsupportedExpression     Reason = "UnsupportedExpression"
	ReasonEvaluationTypeMismatch    Reason = "EvaluationTypeMismatch"
	ReasonInvalidResource           Reason = "InvalidResource"
	ReasonExtractionInvalidInput    Reason = "InvalidInput"
	ReasonExtractionInterrupted     Reason = "ExtractionInterrupted"
	ReasonMissingType               Reason = "MissingType"
	ReasonUnsupportedType           Reason = "UnsupportedType"
	ReasonConversionTypeMismatch    Reason = "ConversionTypeMismatch"
	ReasonConversionOverflow        Reason = "ConversionOverflow"
	ReasonForbiddenConversion       Reason = "ForbiddenConversion"
	ReasonConversionInterrupted     Reason = "ConversionInterrupted"
	ReasonUnsupportedOperator       Reason = "unsupported-operator"
	ReasonIncompatibleOperator      Reason = "incompatible-operator"
	ReasonInvalidArity              Reason = "invalid-arity"
	ReasonInvalidOperand            Reason = "invalid-operand"
	ReasonInvalidPattern            Reason = "invalid-pattern"
	ReasonOperatorInvalidInput      Reason = "invalid-input"
	ReasonOperatorInterrupted       Reason = "operator-interrupted"
	ReasonDuplicateAggregate        Reason = "duplicate-aggregate"
	ReasonUnknownField              Reason = "unknown-field"
	ReasonDuplicateGroupField       Reason = "duplicate-group-field"
	ReasonUnsupportedGroupType      Reason = "unsupported-group-type"
	ReasonIncompatibleFunction      Reason = "incompatible-function"
	ReasonInvalidAverageOptions     Reason = "invalid-average-options"
	ReasonAggregationInvalidInput   Reason = "invalid-input"
	ReasonInvalidGroupKey           Reason = "invalid-group-key"
	ReasonTargetFieldError          Reason = "target-field-error"
	ReasonOverflow                  Reason = "overflow"
	ReasonCardinalityExceeded       Reason = "cardinality-exceeded"
	ReasonAggregationInterrupted    Reason = "aggregation-interrupted"
	ReasonInvalidSubject            Reason = "InvalidSubject"
	ReasonContextCanceled           Reason = "ContextCanceled"
)

// Severity is the closed structured-log severity vocabulary.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// RetryClassification describes whether the authoritative runtime result will
// be retried. It is intentionally not a metric label.
type RetryClassification string

const (
	RetryNone      RetryClassification = "none"
	RetryScheduled RetryClassification = "scheduled"
)

// Stage is the fixed runtime stage vocabulary.
type Stage string

const (
	StageLoad      Stage = "load"
	StageValidate  Stage = "validate"
	StageAuthorize Stage = "authorize"
	StagePlan      Stage = "plan"
	StageRead      Stage = "read"
	StageExtract   Stage = "extract"
	StageAggregate Stage = "aggregate"
	StageCompose   Stage = "compose"
	StagePublish   Stage = "publish"
)

// Scope is the finite resource scope vocabulary used by metrics and safe
// target records.
type Scope string

const (
	ScopeNamespaced Scope = "namespaced"
	ScopeCluster    Scope = "cluster"
)

// AuthorizationKind is the finite authorization metric vocabulary.
type AuthorizationKind string

const (
	AuthorizationPolicyDecision AuthorizationKind = "PolicyDecision"
	AuthorizationReadForbidden  AuthorizationKind = "ReadForbidden"
)

// AttemptStart identifies the owning request. Namespace and name are kept as
// structured fields and never become metric labels.
type AttemptStart struct {
	Namespace string
	Name      string
}

// ObjectIdentity binds the loaded owning object to an attempt. It contains no
// observed-resource identity or object payload.
type ObjectIdentity struct {
	UID        types.UID
	Generation int64
}

// WatchTarget identifies an authorized configured target without any observed
// object data, selector, or field path.
type WatchTarget struct {
	APIGroup  string
	Resource  string
	Scope     Scope
	Namespace string
}

// Terminal describes one authoritative attempt completion.
type Terminal struct {
	Outcome Outcome
	Reason  Reason
	Retry   RetryClassification
}

// StageObservation describes one finalized runtime stage.
type StageObservation struct {
	Stage   Stage
	Outcome Outcome
	Reason  Reason
	Retry   RetryClassification
}

// SourceFailure describes one retained source-scoped terminal failure.
type SourceFailure struct {
	SourceID string
	Stage    Stage
	Reason   Reason
	Retry    RetryClassification
}

// WatchObservation describes an unexpected watch lifecycle transition.
type WatchObservation struct {
	Target WatchTarget
	Reason Reason
	Retry  RetryClassification
}

// StatusOutcome is the closed status publication outcome vocabulary.
type StatusOutcome string

const (
	StatusWritten    StatusOutcome = "written"
	StatusSkipped    StatusOutcome = "skipped"
	StatusConflicted StatusOutcome = "conflicted"
	StatusFailed     StatusOutcome = "failed"
)

// StatusObservation describes an authoritative publication boundary. Owner
// and conditions are consumed immediately by the Event adapter and are never
// retained by observability.
type StatusObservation struct {
	Outcome    StatusOutcome
	Reason     Reason
	Owner      interface{}
	Conditions []Condition
}

// Condition is the allowlisted subset needed for deterministic Event choice.
// It deliberately omits all status result payloads.
type Condition struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

// LogRecord is the structured, sanitized capture representation. Its fields
// are all allowlisted; in particular it has no raw error, body, selector,
// observed object identity, extracted value, or free-form metric label.
type LogRecord struct {
	Event         EventCode
	Outcome       Outcome
	Reason        Reason
	Severity      Severity
	Retry         RetryClassification
	Duration      time.Duration
	Namespace     string
	Name          string
	AttemptID     string
	UID           types.UID
	Generation    int64
	SourceID      string
	Stage         Stage
	Scope         Scope
	ItemCount     int
	Target        WatchTarget
	TraceID       string
	SpanID        string
	Authorization *authorization.Record
}

// ScopeFromDiscovery converts the discovery-owned scope into this package's
// finite vocabulary. Invalid values become an empty scope and are rejected by
// metric helpers rather than becoming labels.
func ScopeFromDiscovery(scope discovery.Scope) Scope {
	switch scope {
	case discovery.ScopeNamespaced:
		return ScopeNamespaced
	case discovery.ScopeCluster:
		return ScopeCluster
	default:
		return ""
	}
}

func normalizeOutcome(outcome Outcome) Outcome {
	switch outcome {
	case OutcomeStarted, OutcomeCompleted, OutcomeSkipped, OutcomeRetry,
		OutcomeFailed, OutcomeDegraded, OutcomeSucceeded, OutcomeWritten,
		OutcomeConflict, OutcomeUnavailable, OutcomeAllowed, OutcomeDenied,
		OutcomeForbidden:
		return outcome
	default:
		return OutcomeFailed
	}
}

func normalizeStatusOutcome(outcome StatusOutcome) StatusOutcome {
	switch outcome {
	case StatusWritten, StatusSkipped, StatusConflicted, StatusFailed:
		return outcome
	default:
		return StatusFailed
	}
}

func normalizeReason(reason Reason) Reason {
	switch reason {
	case ReasonInternalError, ReasonAllowed, ReasonReconciliationStarted, ReasonObjectAbsent,
		ReasonObjectDeleting, ReasonReadUnavailable, ReasonPolicyUnavailable,
		ReasonPolicyMissing, ReasonPolicyInvalid, ReasonStatusConflict,
		ReasonStatusUnavailable, ReasonStaleLease, ReasonInvalidSetup,
		ReasonBuildFailure, ReasonInvalidSource, ReasonInvalidNamespaceScope,
		ReasonInvalidScope,
		ReasonInvalidSelector, ReasonUnsupportedSelector, ReasonAuthorizationMissing,
		ReasonAuthorizationDenied, ReasonAuthorizationMismatch, ReasonAuthorizationStale,
		ReasonReadForbidden, ReasonReadInterrupted, ReasonListExpired,
		ReasonInvalidObject, ReasonDiscoveryUnavailable, ReasonUnknownType,
		ReasonAmbiguousResource, ReasonInvalidDescriptor, ReasonConfigurationAccepted,
		ReasonInvalidConfiguration, ReasonAuthorizationSucceeded,
		ReasonAuthorizationUnavailable, ReasonAuthorizationNotEvaluated, ReasonInvalidRequest, ReasonResourceDenied,
		ReasonClusterScopeDenied, ReasonNamespaceDenied, ReasonResolutionSucceeded,
		ReasonResolutionFailed, ReasonResolutionNotEvaluated,
		ReasonResolutionUnavailable, ReasonEvaluationSucceeded,
		ReasonEvaluationDegraded, ReasonEvaluationUnavailable, ReasonInvalidExtraction,
		ReasonExtractionFailed, ReasonInvalidValue, ReasonOperatorFailed,
		ReasonAggregationFailed, ReasonAggregationLimit, ReasonInvalidExpression,
		ReasonUnsupportedExpression, ReasonEvaluationTypeMismatch, ReasonInvalidResource,
		ReasonExtractionInvalidInput, ReasonExtractionInterrupted, ReasonMissingType,
		ReasonUnsupportedType, ReasonConversionTypeMismatch, ReasonConversionOverflow,
		ReasonForbiddenConversion, ReasonConversionInterrupted, ReasonUnsupportedOperator,
		ReasonIncompatibleOperator, ReasonInvalidArity, ReasonInvalidOperand,
		ReasonInvalidPattern, ReasonOperatorInvalidInput, ReasonOperatorInterrupted,
		ReasonDuplicateAggregate, ReasonUnknownField, ReasonDuplicateGroupField,
		ReasonUnsupportedGroupType, ReasonIncompatibleFunction, ReasonInvalidAverageOptions,
		ReasonInvalidGroupKey, ReasonTargetFieldError,
		ReasonOverflow, ReasonCardinalityExceeded, ReasonAggregationInterrupted,
		ReasonInvalidSubject, ReasonContextCanceled:
		return reason
	default:
		return ReasonInternalError
	}
}

func normalizeRetry(retry RetryClassification) RetryClassification {
	if retry == RetryScheduled {
		return RetryScheduled
	}
	return RetryNone
}

func normalizeSeverity(severity Severity) Severity {
	switch severity {
	case SeverityInfo, SeverityWarning, SeverityError:
		return severity
	default:
		return SeverityError
	}
}

func normalizeStage(stage Stage) Stage {
	switch stage {
	case StageLoad, StageValidate, StageAuthorize, StagePlan, StageRead,
		StageExtract, StageAggregate, StageCompose, StagePublish:
		return stage
	default:
		return ""
	}
}

func normalizeEvent(event EventCode) EventCode {
	switch event {
	case EventReconciliationStarted, EventReconciliationSkipped,
		EventReconciliationRetryScheduled, EventReconciliationFailed,
		EventReconciliationCompleted, EventSourceFailed, EventStatusUpdateWritten,
		EventStatusUpdateSkipped, EventAuthorizationDecision,
		EventAuthorizationReadForbidden, EventSourceWatchStopped,
		EventSourceWatchRestarted, EventJSONPathFailed:
		return event
	default:
		return EventReconciliationFailed
	}
}

func normalizeScope(scope Scope) Scope {
	if scope == ScopeNamespaced || scope == ScopeCluster {
		return scope
	}
	return ""
}
