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

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics owns the nine feature-specific collectors. It contains no labels
// derived from resource, source, namespace, or policy identities.
type Metrics struct {
	reconciliations        *prometheus.CounterVec
	reconciliationDuration *prometheus.HistogramVec
	resourcesRead          *prometheus.CounterVec
	sourceFailures         *prometheus.CounterVec
	resultsProduced        *prometheus.CounterVec
	statusUpdates          *prometheus.CounterVec
	authorization          *prometheus.CounterVec
	jsonPathFailures       *prometheus.CounterVec
	watchRestarts          *prometheus.CounterVec
}

const (
	metricReconciliations        = "kubeseer_reconciliations_total"
	metricReconciliationDuration = "kubeseer_reconciliation_duration_seconds"
	metricResourcesRead          = "kubeseer_resources_read_total"
	metricSourceFailures         = "kubeseer_source_failures_total"
	metricResultsProduced        = "kubeseer_results_produced_total"
	metricStatusUpdates          = "kubeseer_status_updates_total"
	metricAuthorization          = "kubeseer_authorization_decisions_total"
	metricJSONPathFailures       = "kubeseer_jsonpath_failures_total"
	metricWatchRestarts          = "kubeseer_source_watch_restarts_total"
)

func newMetrics(registerer prometheus.Registerer) (*Metrics, error) {
	if registerer == nil {
		return nil, ErrObservabilitySetupInvalid
	}
	metrics := &Metrics{
		reconciliations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricReconciliations,
			Help: "Total Kubeseer reconciliation attempts by terminal outcome and reason.",
		}, []string{"outcome", "reason"}),
		reconciliationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: metricReconciliationDuration,
			Help: "Kubeseer reconciliation duration in seconds by terminal outcome.",
		}, []string{"outcome"}),
		resourcesRead: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricResourcesRead,
			Help: "Resource instances returned by successful authorized LIST pages.",
		}, []string{"scope"}),
		sourceFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricSourceFailures,
			Help: "Terminal source failures by fixed runtime stage and reason.",
		}, []string{"stage", "reason"}),
		resultsProduced: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricResultsProduced,
			Help: "Publishable Kubeseer result snapshots by outcome.",
		}, []string{"outcome"}),
		statusUpdates: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricStatusUpdates,
			Help: "Kubeseer status publication outcomes by stable reason.",
		}, []string{"outcome", "reason"}),
		authorization: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricAuthorization,
			Help: "Authorization decisions emitted by fixed kind, outcome, and reason.",
		}, []string{"kind", "outcome", "reason"}),
		jsonPathFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricJSONPathFailures,
			Help: "Retained JSONPath failures by stable reason.",
		}, []string{"reason"}),
		watchRestarts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricWatchRestarts,
			Help: "Authorized source watch restarts by stable reason.",
		}, []string{"reason"}),
	}

	collectors := []prometheus.Collector{
		metrics.reconciliations,
		metrics.reconciliationDuration,
		metrics.resourcesRead,
		metrics.sourceFailures,
		metrics.resultsProduced,
		metrics.statusUpdates,
		metrics.authorization,
		metrics.jsonPathFailures,
		metrics.watchRestarts,
	}
	registered := make([]prometheus.Collector, 0, len(collectors))
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			for _, item := range registered {
				registerer.Unregister(item)
			}
			return nil, ErrObservabilitySetupInvalid
		}
		registered = append(registered, collector)
	}
	return metrics, nil
}

func (m *Metrics) observeReconciliation(outcome Outcome, reason Reason, elapsed time.Duration) {
	if m == nil {
		return
	}
	outcome = normalizeOutcome(outcome)
	reason = normalizeReason(reason)
	if elapsed < 0 {
		elapsed = 0
	}
	m.reconciliations.WithLabelValues(string(outcome), string(reason)).Inc()
	m.reconciliationDuration.WithLabelValues(string(outcome)).Observe(elapsed.Seconds())
}

func (m *Metrics) observeResourcesRead(scope Scope, itemCount int) {
	if m == nil || itemCount <= 0 {
		return
	}
	scope = normalizeScope(scope)
	if scope == "" {
		return
	}
	m.resourcesRead.WithLabelValues(string(scope)).Add(float64(itemCount))
}

func (m *Metrics) observeSourceFailure(stage Stage, reason Reason) {
	if m == nil {
		return
	}
	stage = normalizeStage(stage)
	if stage == "" {
		stage = StageCompose
	}
	m.sourceFailures.WithLabelValues(string(stage), string(normalizeReason(reason))).Inc()
}

func (m *Metrics) observeResult(outcome Outcome) {
	if m == nil {
		return
	}
	switch outcome {
	case OutcomeCompleted, OutcomeSucceeded:
		outcome = OutcomeCompleted
	case OutcomeDegraded:
		// retain the stable degraded label
	default:
		outcome = OutcomeFailed
	}
	m.resultsProduced.WithLabelValues(string(outcome)).Inc()
}

func (m *Metrics) observeStatus(outcome StatusOutcome, reason Reason) {
	if m == nil {
		return
	}
	m.statusUpdates.WithLabelValues(string(normalizeStatusOutcome(outcome)), string(normalizeReason(reason))).Inc()
}

func (m *Metrics) observeAuthorization(kind AuthorizationKind, outcome Outcome, reason Reason) {
	if m == nil {
		return
	}
	if kind != AuthorizationPolicyDecision && kind != AuthorizationReadForbidden {
		kind = AuthorizationPolicyDecision
	}
	switch outcome {
	case OutcomeAllowed, OutcomeDenied, OutcomeUnavailable, OutcomeForbidden:
		// approved finite values
	default:
		outcome = OutcomeDenied
	}
	m.authorization.WithLabelValues(string(kind), string(outcome), string(normalizeReason(reason))).Inc()
}

func (m *Metrics) observeJSONPathFailure(reason Reason) {
	if m == nil {
		return
	}
	m.jsonPathFailures.WithLabelValues(string(normalizeReason(reason))).Inc()
}

func (m *Metrics) observeWatchRestart(reason Reason) {
	if m == nil {
		return
	}
	m.watchRestarts.WithLabelValues(string(normalizeReason(reason))).Inc()
}
