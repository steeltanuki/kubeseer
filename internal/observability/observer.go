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
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/selection"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
)

// ErrObservabilitySetupInvalid is the only setup diagnostic exposed for
// invalid facilities or collector registration. It intentionally contains no
// registry, logger, or dependency details.
var ErrObservabilitySetupInvalid = errors.New("ObservabilitySetupInvalid")

// Clock is injected for deterministic duration proofs.
type Clock func() time.Time

// AttemptIDGenerator supplies opaque attempt identifiers.
type AttemptIDGenerator func() string

// LogSink receives already-sanitized structured records. It cannot influence
// reconciliation because the interface has no domain-result return value.
type LogSink interface {
	Emit(LogRecord) error
}

// LogSinkFunc adapts a function to LogSink.
type LogSinkFunc func(LogRecord) error

// Emit implements LogSink.
func (f LogSinkFunc) Emit(record LogRecord) error {
	if f == nil {
		return nil
	}
	return f(record)
}

// AuthorizationRecorder adapts the existing ordered authorization evidence
// boundary to this passive observer. It retains no record history and cannot
// return an error to the enforcer.
type AuthorizationRecorder struct {
	observer *Observer
}

// NewAuthorizationRecorder constructs an authorization evidence adapter.
func NewAuthorizationRecorder(observer *Observer) *AuthorizationRecorder {
	return &AuthorizationRecorder{observer: observer}
}

// Record emits each copied record in the order supplied by the enforcer.
func (r *AuthorizationRecorder) Record(ctx context.Context, records []authorization.Record) {
	if r == nil || r.observer == nil {
		return
	}
	for _, record := range records {
		r.observer.ObserveAuthorization(ctx, record)
	}
}

// Record lets Observer itself satisfy authorization.Recorder for the manager
// composition root while the named adapter remains available to callers that
// want an explicit boundary value.
func (o *Observer) Record(ctx context.Context, records []authorization.Record) {
	if o == nil {
		return
	}
	for _, record := range records {
		o.ObserveAuthorization(ctx, record)
	}
}

// PageObserver adapts successful selection pages to the bounded resources-read
// metric. The selection package remains independent of observability.
type PageObserver struct {
	observer *Observer
}

// NewPageObserver constructs a selection page observer.
func NewPageObserver(observer *Observer) *PageObserver {
	return &PageObserver{observer: observer}
}

// ObservePage records only scope and count from a successful page.
func (p *PageObserver) ObservePage(ctx context.Context, observation selection.PageObservation) {
	if p == nil || p.observer == nil {
		return
	}
	p.observer.ObserveResourcesRead(ctx, ScopeFromDiscovery(observation.Scope), observation.ItemCount)
}

// Options configures one manager-owned observer. A normal observer requires a
// Prometheus registerer; Event recording remains optional until a status
// boundary is composed. Direct runtime callers use NewNoop when unconfigured.
type Options struct {
	Logger         logr.Logger
	Registerer     prometheus.Registerer
	EventRecorder  record.EventRecorder
	TracerProvider trace.TracerProvider
	Clock          Clock
	AttemptID      AttemptIDGenerator
	LogSink        LogSink
}

// Observer is the synchronous passive observability implementation. It owns
// collectors and per-attempt handles only; it has no queue, goroutine, or
// retained authorization history.
type Observer struct {
	logger         logr.Logger
	logSink        LogSink
	eventRecorder  record.EventRecorder
	metrics        *Metrics
	clock          Clock
	attemptID      AttemptIDGenerator
	tracerProvider trace.TracerProvider
	disabled       bool
}

// New constructs and validates a fully registered observer. A failed setup
// never returns a partially configured observer.
func New(options Options) (*Observer, error) {
	metrics, err := newMetrics(options.Registerer)
	if err != nil {
		return nil, ErrObservabilitySetupInvalid
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	attemptID := options.AttemptID
	if attemptID == nil {
		attemptID = newOpaqueAttemptID
	}
	return &Observer{
		logger:         options.Logger,
		logSink:        options.LogSink,
		eventRecorder:  options.EventRecorder,
		metrics:        metrics,
		clock:          clock,
		attemptID:      attemptID,
		tracerProvider: options.TracerProvider,
	}, nil
}

// NewNoop constructs the no-op observer used by direct callers that do not
// opt into manager-wide observability. It performs no registration or I/O.
func NewNoop() *Observer {
	return &Observer{disabled: true, clock: time.Now, attemptID: newOpaqueAttemptID}
}

// Metrics returns the feature collectors for diagnostics and integration
// gathering. The returned value contains no user-controlled labels.
func (o *Observer) Metrics() *Metrics {
	if o == nil {
		return nil
	}
	return o.metrics
}

// StartAttempt creates an immutable-by-use attempt handle and emits the
// single start record before the first producer read.
func (o *Observer) StartAttempt(ctx context.Context, start AttemptStart) (context.Context, *Attempt) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o == nil {
		o = NewNoop()
	}
	identifier := ""
	if o.attemptID != nil {
		identifier = o.attemptID()
	}
	if identifier == "" {
		identifier = newOpaqueAttemptID()
	}
	correlation := correlation{Namespace: start.Namespace, Name: start.Name, AttemptID: identifier}
	attemptCtx := context.WithValue(ctx, correlationKey{}, correlation)
	attempt := &Attempt{observer: o, started: o.now(), context: attemptCtx}
	if o.tracerProvider != nil && !o.disabled {
		tracer := o.tracerProvider.Tracer("github.com/steeltanuki/kubeseer/internal/observability")
		attemptCtx, attempt.rootSpan = tracer.Start(attemptCtx, "kubeseer.reconciliation", trace.WithAttributes(
			attribute.String("kubeseer.namespace", start.Namespace),
			attribute.String("kubeseer.name", start.Name),
			attribute.String("kubeseer.attempt_id", identifier),
		))
		attempt.setContext(attemptCtx)
	}
	o.emit(attemptCtx, LogRecord{
		Event:     EventReconciliationStarted,
		Outcome:   OutcomeStarted,
		Reason:    ReasonReconciliationStarted,
		Severity:  SeverityInfo,
		Namespace: start.Namespace,
		Name:      start.Name,
		AttemptID: identifier,
	})
	return attemptCtx, attempt
}

// ObserveSourceFailure emits one source-scoped failure and updates its
// terminal-source metric.
func (o *Observer) ObserveSourceFailure(ctx context.Context, failure SourceFailure) {
	if o == nil || o.disabled {
		return
	}
	failure.Reason = normalizeReason(failure.Reason)
	failure.Stage = normalizeStage(failure.Stage)
	if failure.Stage == "" {
		failure.Stage = StageCompose
	}
	failure.Retry = normalizeRetry(failure.Retry)
	o.metrics.observeSourceFailure(failure.Stage, failure.Reason)
	o.emit(ctx, LogRecord{
		Event:    EventSourceFailed,
		Outcome:  OutcomeFailed,
		Reason:   failure.Reason,
		Severity: severityFor(failure.Retry, OutcomeFailed),
		Retry:    failure.Retry,
		SourceID: failure.SourceID,
		Stage:    failure.Stage,
	})
}

// ObserveJSONPathFailure records one retained JSONPath failure without
// accepting the path, value, or raw error.
func (o *Observer) ObserveJSONPathFailure(ctx context.Context, reason Reason) {
	if o == nil || o.disabled {
		return
	}
	reason = normalizeReason(reason)
	o.metrics.observeJSONPathFailure(reason)
	o.emit(ctx, LogRecord{Event: EventJSONPathFailed, Outcome: OutcomeFailed, Reason: reason, Severity: SeverityWarning, Stage: StageExtract})
}

// ObserveResult records a publishable complete or degraded snapshot.
func (o *Observer) ObserveResult(ctx context.Context, outcome Outcome) {
	if o == nil || o.disabled {
		return
	}
	switch outcome {
	case OutcomeCompleted, OutcomeSucceeded:
		outcome = OutcomeCompleted
	case OutcomeDegraded:
		// stable degraded outcome
	default:
		return
	}
	o.metrics.observeResult(outcome)
}

// ObserveResourcesRead adds the number of objects returned by one successful
// LIST page. Zero-item pages are intentionally observable only through a
// request, not as a counter increase.
func (o *Observer) ObserveResourcesRead(_ context.Context, scope Scope, itemCount int) {
	if o == nil || o.disabled {
		return
	}
	o.metrics.observeResourcesRead(scope, itemCount)
}

// ObserveAuthorization records one copied authorization boundary record.
func (o *Observer) ObserveAuthorization(ctx context.Context, record authorization.Record) {
	if o == nil || o.disabled {
		return
	}
	copy := record.DeepCopy()
	kind := AuthorizationPolicyDecision
	event := EventAuthorizationDecision
	if copy.Kind == "ReadForbidden" {
		kind = AuthorizationReadForbidden
		event = EventAuthorizationReadForbidden
	}
	outcome := OutcomeDenied
	switch copy.Outcome {
	case "Allowed":
		outcome = OutcomeAllowed
	case "Unavailable":
		outcome = OutcomeUnavailable
	case "ReadForbidden":
		outcome = OutcomeForbidden
	}
	reason := normalizeReason(Reason(copy.Reason))
	o.metrics.observeAuthorization(kind, outcome, reason)
	auth := copy
	o.emit(ctx, LogRecord{Event: event, Outcome: outcome, Reason: reason, Severity: severityFor(RetryNone, outcome), Authorization: &auth})
}

// ObserveStatus records the authoritative publication outcome. Event delivery
// is added by the status adapter without changing the no-error contract.
func (o *Observer) ObserveStatus(ctx context.Context, outcome StatusOutcome, reason Reason) {
	if o == nil || o.disabled {
		return
	}
	outcome = normalizeStatusOutcome(outcome)
	reason = normalizeReason(reason)
	o.metrics.observeStatus(outcome, reason)
	event := EventStatusUpdateSkipped
	logOutcome := OutcomeSkipped
	severity := SeverityInfo
	if outcome == StatusWritten {
		event = EventStatusUpdateWritten
		logOutcome = OutcomeWritten
	} else if outcome == StatusConflicted || outcome == StatusFailed {
		logOutcome = OutcomeFailed
		severity = SeverityWarning
	}
	o.emit(ctx, LogRecord{Event: event, Outcome: logOutcome, Reason: reason, Severity: severity})
}

// RecordStatusEvent records one semantic status transition after the caller
// has successfully persisted the status. The recorder is deliberately
// fire-and-forget: an Event failure cannot change the already-authoritative
// status write or create a reconciliation retry.
func (o *Observer) RecordStatusEvent(_ context.Context, owner runtime.Object, conditions []metav1.Condition) {
	if o == nil || o.disabled || o.eventRecorder == nil || owner == nil {
		return
	}
	eventType, reason, message := selectStatusEvent(conditions)
	defer func() { _ = recover() }()
	o.eventRecorder.Event(owner, eventType, reason, message)
}

// ObserveWatchRestart records one scheduled replacement watch.
func (o *Observer) ObserveWatchRestart(ctx context.Context, observation WatchObservation) {
	if o == nil || o.disabled {
		return
	}
	observation.Reason = normalizeReason(observation.Reason)
	observation.Retry = normalizeRetry(observation.Retry)
	o.metrics.observeWatchRestart(observation.Reason)
	o.emit(ctx, LogRecord{Event: EventSourceWatchRestarted, Outcome: OutcomeRetry, Reason: observation.Reason, Severity: SeverityWarning, Retry: observation.Retry, Target: observation.Target})
}

// ObserveWatchStopped records an unexpected watch termination.
func (o *Observer) ObserveWatchStopped(ctx context.Context, observation WatchObservation) {
	if o == nil || o.disabled {
		return
	}
	observation.Reason = normalizeReason(observation.Reason)
	observation.Retry = normalizeRetry(observation.Retry)
	o.emit(ctx, LogRecord{Event: EventSourceWatchStopped, Outcome: OutcomeFailed, Reason: observation.Reason, Severity: SeverityWarning, Retry: observation.Retry, Target: observation.Target})
}

// StartStage starts a fixed child span when tracing is enabled. Its completion
// function is idempotent and never returns an error to the producer.
func (a *Attempt) StartStage(ctx context.Context, stage Stage) (context.Context, *StageHandle) {
	if a == nil {
		return ctx, &StageHandle{}
	}
	if ctx == nil {
		ctx = a.Context()
	}
	stage = normalizeStage(stage)
	if stage == "" {
		stage = StageCompose
	}
	handle := &StageHandle{attempt: a, stage: stage, context: ctx}
	if a.rootSpan != nil && a.observer != nil && a.observer.tracerProvider != nil {
		tracer := a.observer.tracerProvider.Tracer("github.com/steeltanuki/kubeseer/internal/observability")
		stageCtx, span := tracer.Start(ctx, "kubeseer.reconciliation."+string(stage), trace.WithAttributes(attribute.String("kubeseer.stage", string(stage))))
		handle.context = stageCtx
		handle.span = span
	}
	return handle.context, handle
}

// End finalizes a stage exactly once, recording stable span attributes before
// ending the child span.
func (h *StageHandle) End(observation StageObservation) {
	if h == nil {
		return
	}
	h.once.Do(func() {
		observation.Stage = normalizeStage(observation.Stage)
		if observation.Stage == "" {
			observation.Stage = h.stage
		}
		observation.Outcome = normalizeOutcome(observation.Outcome)
		observation.Reason = normalizeReason(observation.Reason)
		observation.Retry = normalizeRetry(observation.Retry)
		if h.span != nil {
			h.span.SetAttributes(
				attribute.String("kubeseer.stage", string(observation.Stage)),
				attribute.String("kubeseer.outcome", string(observation.Outcome)),
				attribute.String("kubeseer.reason", string(observation.Reason)),
				attribute.String("kubeseer.retry", string(observation.Retry)),
			)
			h.span.End()
		}
	})
}

// BindObject derives a context containing the owning UID and generation after
// the first successful object load. It never adds observed-resource data.
func (a *Attempt) BindObject(ctx context.Context, identity ObjectIdentity) context.Context {
	if a == nil {
		return ctx
	}
	if ctx == nil {
		ctx = a.Context()
	}
	correlation := correlationFromContext(ctx)
	correlation.UID = identity.UID
	correlation.Generation = identity.Generation
	bound := context.WithValue(ctx, correlationKey{}, correlation)
	a.setContext(bound)
	return bound
}

// Context returns the latest context for this attempt.
func (a *Attempt) Context() context.Context {
	if a == nil {
		return context.Background()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.context == nil {
		return context.Background()
	}
	return a.context
}

// Complete records exactly one terminal attempt outcome and closes its root
// span. Calling it repeatedly is harmless and cannot duplicate metrics.
func (a *Attempt) Complete(ctx context.Context, terminal Terminal) {
	if a == nil {
		return
	}
	a.once.Do(func() {
		if ctx == nil {
			ctx = a.Context()
		}
		terminal.Outcome = normalizeOutcome(terminal.Outcome)
		terminal.Reason = normalizeReason(terminal.Reason)
		terminal.Retry = normalizeRetry(terminal.Retry)
		if terminal.Retry == RetryScheduled {
			terminal.Outcome = OutcomeRetry
		}
		elapsed := a.observer.elapsed(a.started)
		event, severity := terminalEvent(terminal)
		o := a.observer
		if o != nil && !o.disabled {
			o.metrics.observeReconciliation(terminal.Outcome, terminal.Reason, elapsed)
			o.emit(ctx, LogRecord{Event: event, Outcome: terminal.Outcome, Reason: terminal.Reason, Severity: severity, Retry: terminal.Retry, Duration: elapsed})
		}
		if a.rootSpan != nil {
			a.rootSpan.SetAttributes(
				attribute.String("kubeseer.outcome", string(terminal.Outcome)),
				attribute.String("kubeseer.reason", string(terminal.Reason)),
				attribute.String("kubeseer.retry", string(terminal.Retry)),
				attribute.Int64("kubeseer.duration_ms", elapsed.Milliseconds()),
			)
			a.rootSpan.End()
		}
	})
}

type correlation struct {
	Namespace  string
	Name       string
	AttemptID  string
	UID        types.UID
	Generation int64
}

type correlationKey struct{}

type Attempt struct {
	observer *Observer
	started  time.Time
	rootSpan trace.Span

	mu      sync.Mutex
	context context.Context
	once    sync.Once
}

// StageHandle is an idempotent child-stage completion handle.
type StageHandle struct {
	attempt *Attempt
	stage   Stage
	context context.Context
	span    trace.Span
	once    sync.Once
}

func (a *Attempt) setContext(ctx context.Context) {
	if a == nil || ctx == nil {
		return
	}
	a.mu.Lock()
	a.context = ctx
	a.mu.Unlock()
}

func (o *Observer) now() time.Time {
	if o == nil || o.clock == nil {
		return time.Now()
	}
	return o.clock()
}

func (o *Observer) elapsed(start time.Time) time.Duration {
	if o == nil {
		return 0
	}
	elapsed := o.now().Sub(start)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func correlationFromContext(ctx context.Context) correlation {
	if ctx == nil {
		return correlation{}
	}
	value, _ := ctx.Value(correlationKey{}).(correlation)
	return value
}

func (o *Observer) emit(ctx context.Context, record LogRecord) {
	if o == nil || o.disabled {
		return
	}
	record.Event = normalizeEvent(record.Event)
	record.Outcome = normalizeOutcome(record.Outcome)
	record.Reason = normalizeReason(record.Reason)
	record.Severity = normalizeSeverity(record.Severity)
	record.Retry = normalizeRetry(record.Retry)
	correlation := correlationFromContext(ctx)
	if record.Namespace == "" {
		record.Namespace = correlation.Namespace
	}
	if record.Name == "" {
		record.Name = correlation.Name
	}
	if record.AttemptID == "" {
		record.AttemptID = correlation.AttemptID
	}
	if record.UID == "" {
		record.UID = correlation.UID
	}
	if record.Generation == 0 {
		record.Generation = correlation.Generation
	}
	if record.Duration < 0 {
		record.Duration = 0
	}
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record.TraceID = spanContext.TraceID().String()
		record.SpanID = spanContext.SpanID().String()
	}
	// Each replaceable sink is isolated. The logger is deliberately called
	// independently so a capture adapter cannot suppress production logs.
	if o.logSink != nil {
		callLogSink(o.logSink, record)
	}
	values := []interface{}{
		"event", string(record.Event),
		"outcome", string(record.Outcome),
		"reason", string(record.Reason),
		"severity", string(record.Severity),
		"retry", string(record.Retry),
		"namespace", record.Namespace,
		"name", record.Name,
		"attempt_id", record.AttemptID,
	}
	if record.SourceID != "" {
		values = append(values, "source_id", record.SourceID)
	}
	if record.Stage != "" {
		values = append(values, "stage", string(record.Stage))
	}
	if record.TraceID != "" {
		values = append(values, "trace_id", record.TraceID, "span_id", record.SpanID)
	}
	if record.Severity == SeverityError {
		o.logger.Error(nil, "kubeseer observability", values...)
	} else {
		o.logger.Info("kubeseer observability", values...)
	}
}

func callLogSink(sink LogSink, record LogRecord) {
	defer func() { _ = recover() }()
	_ = sink.Emit(record)
}

const malformedStatusEventMessage = "Kubeseer status transition could not be classified"

var statusEventConditionOrder = []string{"Accepted", "Authorized", "SourcesResolved", "Ready"}

func selectStatusEvent(conditions []metav1.Condition) (string, string, string) {
	byType := make(map[string][]metav1.Condition, len(statusEventConditionOrder))
	for _, condition := range conditions {
		for _, conditionType := range statusEventConditionOrder {
			if condition.Type == conditionType {
				byType[conditionType] = append(byType[conditionType], condition)
				break
			}
		}
	}
	for _, conditionType := range statusEventConditionOrder {
		entries := byType[conditionType]
		if len(entries) != 1 || !validStatusEventCondition(entries[0]) {
			return corev1.EventTypeWarning, string(ReasonInternalError), malformedStatusEventMessage
		}
		if entries[0].Status != metav1.ConditionTrue {
			return corev1.EventTypeWarning, entries[0].Reason, entries[0].Message
		}
	}
	ready := byType["Ready"][0]
	if ready.Reason != string(ReasonEvaluationSucceeded) {
		return corev1.EventTypeWarning, string(ReasonInternalError), malformedStatusEventMessage
	}
	return corev1.EventTypeNormal, string(ReasonEvaluationSucceeded), ready.Message
}

func validStatusEventCondition(condition metav1.Condition) bool {
	if condition.Reason == "" || condition.Message == "" || condition.Status == "" {
		return false
	}
	switch condition.Status {
	case metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown:
	default:
		return false
	}
	if normalizeReason(Reason(condition.Reason)) != Reason(condition.Reason) {
		return false
	}
	return !strings.ContainsAny(condition.Message, "\r\n")
}

func severityFor(retry RetryClassification, outcome Outcome) Severity {
	if retry == RetryScheduled || outcome == OutcomeDenied || outcome == OutcomeForbidden || outcome == OutcomeFailed {
		return SeverityWarning
	}
	return SeverityInfo
}

func terminalEvent(terminal Terminal) (EventCode, Severity) {
	if terminal.Retry == RetryScheduled || terminal.Outcome == OutcomeRetry {
		return EventReconciliationRetryScheduled, SeverityWarning
	}
	if terminal.Outcome == OutcomeSkipped {
		return EventReconciliationSkipped, SeverityInfo
	}
	if terminal.Outcome == OutcomeFailed {
		return EventReconciliationFailed, SeverityError
	}
	return EventReconciliationCompleted, severityFor(terminal.Retry, terminal.Outcome)
}

func newOpaqueAttemptID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	// crypto/rand failure is exceptionally unlikely. A timestamp is still
	// opaque to callers and avoids turning telemetry setup into a domain error.
	return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
}
