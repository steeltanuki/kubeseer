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

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/admission"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/observability"
	"github.com/steeltanuki/kubeseer/internal/operators"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"github.com/steeltanuki/kubeseer/internal/selection"
	statuscontract "github.com/steeltanuki/kubeseer/internal/status"
	"github.com/steeltanuki/kubeseer/internal/typedoutput"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertPerformanceLimitsProfileScenarios(t *testing.T) {
	t.Helper()
	profile := limits.DefaultProfile()
	if profile.PageSize() != limits.DefaultPageSize || profile.MaxMatchedResources() != limits.DefaultMaxMatchedResources ||
		profile.MaxSelectedInputBytes() != limits.DefaultSelectedInputBytes || profile.MaxProducedValueBytes() != limits.DefaultProducedValueBytes ||
		profile.MaxStatusBytes() != limits.DefaultMaxStatusBytes || profile.EvaluationTimeout() != limits.DefaultEvaluationTimeout ||
		profile.MaxConcurrentReconciles() != limits.DefaultMaxConcurrentReconciles || profile.DiscoveryCacheEntries() != limits.DefaultDiscoveryCacheEntries ||
		profile.DiscoveryCacheTTL() != limits.DefaultDiscoveryCacheTTL || profile.MaxActiveWatches() != limits.DefaultMaxActiveWatches ||
		profile.MaxPendingTriggers() != limits.DefaultMaxPendingTriggers {
		t.Fatalf("default profile does not match the approved table: %#v", profile)
	}

	admissionDefaults := admission.DefaultLimits()
	admissionView := profile.Admission()
	if admissionDefaults.MaxKubeseerSources != admissionView.MaxKubeseerSources ||
		admissionDefaults.MaxSourceNamespaces != admissionView.MaxSourceNamespaces ||
		admissionDefaults.MaxSourceFields != admissionView.MaxSourceFields ||
		admissionDefaults.MaxFieldOperators != admissionView.MaxFieldOperators ||
		admissionDefaults.MaxOperatorValues != admissionView.MaxOperatorValues ||
		admissionDefaults.MaxSourceAggregations != admissionView.MaxSourceAggregations ||
		admissionDefaults.MaxAggregationGroupBy != admissionView.MaxAggregationGroupBy ||
		admissionDefaults.MaxSelectorMatchExpressions != admissionView.MaxSelectorMatchExpressions ||
		admissionDefaults.MaxSelectorExpressionValues != admissionView.MaxSelectorExpressionValues ||
		admissionDefaults.MaxSelectorMatchLabels != admissionView.MaxSelectorMatchLabels ||
		admissionDefaults.MaxPolicyNamespaceNames != admissionView.MaxPolicyNamespaceNames ||
		admissionDefaults.MaxPolicyResourceRules != admissionView.MaxPolicyResourceRules ||
		admissionDefaults.MaxPolicyAPIGroups != admissionView.MaxPolicyAPIGroups ||
		admissionDefaults.MaxPolicyKinds != admissionView.MaxPolicyKinds ||
		admissionDefaults.MaxKubeseerSpecBytes != admissionView.MaxKubeseerSpecBytes ||
		admissionDefaults.MaxAccessPolicySpecBytes != admissionView.MaxAccessPolicySpecBytes {
		t.Fatal("admission default adapter changed the existing budget contract")
	}
	aggregationDefaults := aggregation.DefaultLimits()
	aggregationView := profile.Aggregation()
	if aggregationDefaults.MaxGroups != aggregationView.MaxGroups ||
		aggregationDefaults.MaxContributions != aggregationView.MaxContributions ||
		aggregationDefaults.MaxCollectedValues != aggregationView.MaxCollectedValues ||
		aggregationDefaults.MaxDistinctValues != aggregationView.MaxDistinctValues ||
		aggregationDefaults.MaxProvenanceEntries != aggregationView.MaxProvenanceEntries {
		t.Fatal("aggregation default adapter changed the existing budget contract")
	}

	pageSize := int64(17)
	resources := 19
	admissionSources := 3
	overrides := limits.Overrides{
		PageSize:            &pageSize,
		MaxMatchedResources: &resources,
		Admission: limits.AdmissionOverrides{
			MaxKubeseerSources: &admissionSources,
		},
	}
	custom, err := limits.Resolve(overrides)
	if err != nil {
		t.Fatalf("partial overrides rejected: %v", err)
	}
	if custom.PageSize() != pageSize || custom.MaxMatchedResources() != resources || custom.Admission().MaxKubeseerSources != admissionSources ||
		custom.MaxSelectedInputBytes() != limits.DefaultSelectedInputBytes || custom.Aggregation().MaxGroups != limits.DefaultMaxAggregationGroups {
		t.Fatalf("partial overrides did not preserve omitted defaults: %#v", custom)
	}
	pageSize = 29
	resources = 31
	admissionSources = 5
	if custom.PageSize() != 17 || custom.MaxMatchedResources() != 19 || custom.Admission().MaxKubeseerSources != 3 {
		t.Fatal("resolved profile retained mutable override pointers")
	}

	zero := int64(0)
	negative := -1
	badCases := []limits.Overrides{
		{PageSize: &zero},
		{MaxMatchedResources: &negative},
		{EvaluationTimeout: durationPointer(0)},
		{Admission: limits.AdmissionOverrides{MaxKubeseerSources: intPointer(limits.DefaultMaxKubeseerSources + 1)}},
	}
	for index, bad := range badCases {
		if _, err := limits.Resolve(bad); err == nil {
			t.Fatalf("invalid override case %d was accepted", index)
		} else {
			var invalid *limits.InvalidConfigurationError
			if !errors.As(err, &invalid) || invalid.Error() != limits.ReasonInvalidLimitConfiguration+": "+invalid.Field {
				t.Fatalf("invalid override case %d returned unsanitized or unstable error: %v", index, err)
			}
		}
	}

	first, err := limits.CanonicalSize(map[string]any{"b": 2, "a": 1})
	if err != nil {
		t.Fatalf("canonical size failed: %v", err)
	}
	second, err := limits.CanonicalSize(map[string]any{"a": 1, "b": 2})
	if err != nil || first != second {
		t.Fatalf("canonical map sizing was not deterministic: %d/%d (%v)", first, second, err)
	}
	accountant, err := limits.NewAccountant(5, "selected-input-bytes")
	if err != nil {
		t.Fatalf("accountant setup failed: %v", err)
	}
	if err := accountant.AddBytes(5); err != nil || accountant.Used() != 5 {
		t.Fatalf("exact accountant boundary failed: used=%d err=%v", accountant.Used(), err)
	}
	if err := accountant.AddBytes(1); !errors.Is(err, limits.ErrLimitExceeded) || accountant.Used() != 5 {
		t.Fatalf("accountant committed beyond its ceiling: used=%d err=%v", accountant.Used(), err)
	}
	overflow, err := limits.NewAccountant(math.MaxInt64, "overflow")
	if err != nil {
		t.Fatalf("overflow accountant setup failed: %v", err)
	}
	if err := overflow.AddBytes(math.MaxInt64); err != nil {
		t.Fatalf("maximum integer accounting failed: %v", err)
	}
	if err := overflow.AddBytes(1); !errors.Is(err, limits.ErrAccountingOverflow) {
		t.Fatalf("integer overflow was not rejected: %v", err)
	}

	for _, typ := range []reflect.Type{reflect.TypeOf(v1alpha1.KubeseerSpec{}), reflect.TypeOf(v1alpha1.KubeseerAccessPolicySpec{})} {
		for index := 0; index < typ.NumField(); index++ {
			if typ.Field(index).Name == "Limits" || typ.Field(index).Name == "Performance" {
				t.Fatalf("public API unexpectedly exposes manager limit configuration: %s.%s", typ.Name(), typ.Field(index).Name)
			}
		}
	}
	t.Log("MODULE_INTEGRATION=performance-and-limits-profile STATUS=passed")
}

func intPointer(value int) *int { return &value }

func durationPointer(value int64) *time.Duration {
	duration := time.Duration(value)
	return &duration
}

func assertPerformanceLimitsConfigurationScenarios(t *testing.T) {
	t.Helper()
	maxSources := 1
	profile, err := limits.Resolve(limits.Overrides{
		Admission: limits.AdmissionOverrides{MaxKubeseerSources: &maxSources},
	})
	if err != nil {
		t.Fatalf("tight profile rejected: %v", err)
	}
	object := runtimePipelineKubeseer(
		types.NamespacedName{Namespace: "team-a", Name: "budgeted"},
		"budgeted-uid", 1,
		runtimePipelineValuesSource("first"),
		runtimePipelineValuesSource("second"),
	)
	policySource := &authorizationPipelinePolicySource{policy: basePolicy()}
	validator := admission.NewValidatorWithProfile(discovery.NewResolver(newPolicyDiscoveryClient()), policySource, profile)
	result := validator.ValidateKubeseer(context.Background(), object)
	if result.Valid() || len(result.IssuesCopy()) != 1 || result.IssuesCopy()[0].Reason != "ConfigurationBudgetExceeded" || result.IssuesCopy()[0].Path != "spec.sources[1]" {
		t.Fatalf("tight admission budget result = %#v", result.IssuesCopy())
	}
	if policySource.Calls() != 0 {
		t.Fatalf("budget rejection loaded policy %d times", policySource.Calls())
	}

	tooManyRules := basePolicy()
	tooManyRules.Spec.Resources = make([]v1alpha1.ResourceRule, 0, admission.DefaultMaxPolicyResourceRules+1)
	for index := 0; index < admission.DefaultMaxPolicyResourceRules+1; index++ {
		tooManyRules.Spec.Resources = append(tooManyRules.Spec.Resources, v1alpha1.ResourceRule{APIGroups: []string{""}, Kinds: []string{"Pod"}})
	}
	policyBudget := admission.NewBudgetValidatorFromProfile(profile)
	loaded := accesspolicy.LoadWithValidation(context.Background(), &authorizationPipelinePolicySource{policy: tooManyRules}, func(policy *v1alpha1.KubeseerAccessPolicy) error {
		if issues := policyBudget.ValidateAccessPolicy(policy); len(issues) != 0 {
			return errors.New(issues[0].Path)
		}
		return nil
	})
	if loaded.TerminalReason() != accesspolicy.ReasonPolicyInvalid {
		t.Fatalf("over-budget persisted policy did not fail closed: %q", loaded.TerminalReason())
	}

	reader := newRuntimePipelineReader(object)
	routes := &runtimePipelineRoutes{}
	publisher := &runtimePipelinePublisher{}
	lister := newRuntimePipelineLister()
	runtime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour, LimitProfile: &profile}, reconciliation.Dependencies{
		Reader:       reader,
		Lister:       reader,
		PolicySource: &authorizationPipelinePolicySource{policy: basePolicy()},
		Enforcer:     authorization.NewEnforcer(nil),
		Planner:      selection.NewPlanner(discovery.NewResolver(newPolicyDiscoveryClient())),
		Executor:     selection.NewExecutor(lister, selection.WithVerifier(authorization.VerifierFunc(func(subject authorization.Subject) bool { return subject.Validate() == nil }))),
		Routes:       routes,
		Publisher:    publisher,
		Tracker:      reconciliation.NewFreshnessTracker(),
	})
	if err != nil {
		t.Fatalf("construct tight runtime: %v", err)
	}
	if _, err := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "budgeted"}}); err == nil || !strings.Contains(err.Error(), "ConfigurationBudgetExceeded") {
		t.Fatalf("runtime did not reject persisted over-budget object: %v", err)
	}
	publications := publisher.Publications()
	key := types.NamespacedName{Namespace: object.Namespace, Name: object.Name}
	if len(lister.Calls()) != 0 || len(publications) != 1 || !publications[0].Evaluation.ConfigurationBudgetExceeded || publications[0].Evaluation.Result != nil || !routes.Removed(key) {
		t.Fatalf("runtime budget rejection reached dynamic work or missed cleanup: lists=%#v publications=%#v removed=%t", lister.Calls(), publications, routes.Removed(key))
	}
	t.Log("MODULE_INTEGRATION=performance-and-limits-configuration STATUS=passed")
}

func assertPerformanceLimitsSelectionScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	source := v1alpha1.KubeseerSource{
		ID:         "bounded-selection",
		Resource:   v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
	}
	authorized := planAndAuthorizeSelection(t, ctx, resolver, "team-a", source)
	alpha := paginationObject("team-a", "alpha", "uid-alpha")
	beta := paginationObject("team-a", "beta", "uid-beta")

	countLimited := &scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(alpha, beta, "next")}, {list: listWithContinue(beta, "")}}}
	countOutcome := newSelectionExecutor(countLimited, selection.WithPageLimit(2), selection.WithMaxMatchedResources(1)).Execute(ctx, authorized)
	if !selection.HasReason(countOutcome.Err, selection.ReasonSelectionLimitExceeded) || len(countOutcome.Resources) != 0 || len(countLimited.calls) != 1 {
		t.Fatalf("count boundary outcome=%#v calls=%d", countOutcome, len(countLimited.calls))
	}

	exactCount := &scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(alpha, beta, "")}}}
	exactOutcome := newSelectionExecutor(exactCount, selection.WithMaxMatchedResources(2)).Execute(ctx, authorized)
	if exactOutcome.Err != nil || len(exactOutcome.Resources) != 2 {
		t.Fatalf("exact count boundary outcome=%#v", exactOutcome)
	}

	alphaBytes, err := limits.CanonicalSize(alpha.Object)
	if err != nil {
		t.Fatalf("size alpha: %v", err)
	}
	exactBytes := &scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(alpha, "")}}}
	exactBytesOutcome := newSelectionExecutor(exactBytes, selection.WithMaxSelectedInputBytes(int64(alphaBytes))).Execute(ctx, authorized)
	if exactBytesOutcome.Err != nil || len(exactBytesOutcome.Resources) != 1 {
		t.Fatalf("exact byte boundary outcome=%#v", exactBytesOutcome)
	}
	plusOneBytes := &scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(alpha, beta, "")}}}
	plusOneOutcome := newSelectionExecutor(plusOneBytes, selection.WithMaxSelectedInputBytes(int64(alphaBytes))).Execute(ctx, authorized)
	if !selection.HasReason(plusOneOutcome.Err, selection.ReasonSelectionLimitExceeded) || len(plusOneOutcome.Resources) != 0 {
		t.Fatalf("plus-one byte boundary outcome=%#v", plusOneOutcome)
	}

	duplicate := &scriptedResourceLister{responses: []scriptedListResponse{{list: listWithContinue(alpha, "next")}, {list: listWithContinue(alpha, "")}}}
	duplicateOutcome := newSelectionExecutor(duplicate, selection.WithMaxMatchedResources(1)).Execute(ctx, authorized)
	if duplicateOutcome.Err != nil || len(duplicateOutcome.Resources) != 1 || len(duplicate.calls) != 2 {
		t.Fatalf("duplicate UID boundary outcome=%#v calls=%d", duplicateOutcome, len(duplicate.calls))
	}
	t.Log("MODULE_INTEGRATION=performance-and-limits-selection STATUS=passed")
}

func assertPerformanceLimitsValueScenarios(t *testing.T) {
	t.Helper()
	source := v1alpha1.KubeseerSource{
		ID:           "bounded-values",
		Resource:     v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Fields:       []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeString}},
		Aggregations: []v1alpha1.KubeseerAggregation{{Name: "count-values", Function: v1alpha1.AggregationCount, Field: "value"}},
	}
	selected := selection.SelectionOutcome{SourceID: source.ID, Resources: []selection.SelectedResource{{
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1", "kind": "Pod",
			"metadata": map[string]interface{}{"name": "value", "namespace": "team-a", "uid": "value-uid"},
			"data":     map[string]interface{}{"value": "a value larger than the strict test ceiling"},
		}},
		Provenance: selection.Provenance{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "value", UID: "value-uid"},
	}}}
	extracted := extraction.ExtractBatchWithLimit(context.Background(), []extraction.SourceInput{{Source: source, Selection: selected}}, 1)[0]
	if !extraction.HasReason(extracted.Err, extraction.ReasonValueLimitExceeded) || len(extracted.Resources) != 0 {
		t.Fatalf("extraction value ceiling outcome = %#v", extracted)
	}

	unlimitedExtraction := extraction.ExtractBatch(context.Background(), []extraction.SourceInput{{Source: source, Selection: selected}})[0]
	converted := typedoutput.ConvertBatchWithLimit(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: unlimitedExtraction}}, 1)[0]
	if !typedoutput.HasReason(converted.Err(), typedoutput.ReasonValueLimitExceeded) || len(converted.Resources()) != 0 {
		t.Fatalf("typed value ceiling outcome = %#v", converted)
	}

	fullConverted := typedoutput.ConvertBatch(context.Background(), []typedoutput.SourceInput{{Source: source, Extraction: unlimitedExtraction}})[0]
	operatorPlan := operators.CompileSource(source)
	operated := operators.EvaluateBatchWithLimit(context.Background(), []operators.SourceInput{{Plan: operatorPlan, Typed: fullConverted}}, 1)[0]
	if !operators.HasReason(operated.Err(), operators.ReasonValueLimitExceeded) || len(operated.Resources()) != 0 {
		t.Fatalf("operator value ceiling outcome = %#v", operated)
	}

	fullOperated := operators.EvaluateBatch(context.Background(), []operators.SourceInput{{Plan: operatorPlan, Typed: fullConverted}})[0]
	aggregationPlan := aggregation.PlanSource(source, aggregation.DefaultLimits())
	aggregated := aggregation.EvaluateBatchWithLimit(context.Background(), []aggregation.SourceInput{{Plan: aggregationPlan, Operators: fullOperated}}, 1)[0]
	if !aggregation.HasReason(aggregated.Err(), aggregation.ReasonValueLimitExceeded) || len(aggregated.Aggregates()) != 0 {
		t.Fatalf("aggregation value ceiling outcome = %#v", aggregated)
	}
	t.Log("MODULE_INTEGRATION=performance-and-limits-values STATUS=passed")
}

func assertPerformanceLimitsObservabilityScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	t.Run("limit records are fixed, passive, and metric-label bounded", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		capture := &observabilityCapture{err: errors.New("telemetry sink unavailable")}
		observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture})
		if err != nil {
			t.Fatalf("construct limit observer: %v", err)
		}
		cases := []observability.LimitObservation{
			{SourceID: "count-source", Stage: observability.StageRead, Reason: observability.ReasonSelectionLimitExceeded, Retry: observability.RetryNone, Dimension: observability.LimitDimensionMatchedResources, Ceiling: 2},
			{SourceID: "bytes-source", Stage: observability.StageRead, Reason: observability.ReasonSelectionLimitExceeded, Retry: observability.RetryNone, Dimension: observability.LimitDimensionSelectedInputBytes, Ceiling: 64},
			{SourceID: "value-source", Stage: observability.StageExtract, Reason: observability.ReasonValueLimitExceeded, Retry: observability.RetryNone, Dimension: observability.LimitDimensionProducedValueBytes, Ceiling: 128},
			{SourceID: "aggregate-source", Stage: observability.StageAggregate, Reason: observability.ReasonCardinalityExceeded, Retry: observability.RetryNone, Dimension: observability.LimitDimensionAggregationContributions, Ceiling: 4},
			{Stage: observability.StageCompose, Reason: observability.ReasonEvaluationTimedOut, Retry: observability.RetryScheduled, Dimension: observability.LimitDimensionEvaluationTimeout, Ceiling: 30000},
			{Stage: observability.StageCompose, Reason: observability.ReasonResultLimitExceeded, Retry: observability.RetryNone, Dimension: observability.LimitDimensionStatusBytes, Ceiling: 512},
			{Stage: observability.StageCompose, Reason: observability.ReasonStatusLimitInvalid, Retry: observability.RetryNone, Dimension: observability.LimitDimensionStatusBytes, Ceiling: 0},
		}
		for _, observation := range cases {
			observer.ObserveLimit(ctx, observation)
		}
		observer.ObserveLimit(ctx, observability.LimitObservation{Reason: observability.Reason("untrusted-limit-reason"), Dimension: observability.LimitDimensionStatusBytes, Ceiling: 1})
		records := capture.snapshot()
		if len(records) != len(cases) {
			t.Fatalf("limit records = %d, want %d", len(records), len(cases))
		}
		for index, record := range records {
			want := cases[index]
			if record.Reason != want.Reason || record.Stage != want.Stage || record.SourceID != want.SourceID || record.Dimension != want.Dimension || record.Ceiling != want.Ceiling || record.Retry != want.Retry {
				t.Fatalf("limit record %d = %#v, want observation %#v", index, record, want)
			}
			encoded, marshalErr := json.Marshal(record)
			if marshalErr != nil {
				t.Fatalf("marshal limit record %d: %v", index, marshalErr)
			}
			for _, forbidden := range []string{"observed-object", "selector", "field-path", "extracted-value", "typed-value", "aggregate-value", "secret-data", "wrapped-cause", "raw-error"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("forbidden limit diagnostic sentinel %q crossed the boundary: %s", forbidden, encoded)
				}
			}
		}
		families, err := registry.Gather()
		if err != nil {
			t.Fatalf("gather limit metrics: %v", err)
		}
		if len(families) != 1 || families[0].GetName() != "kubeseer_source_failures_total" {
			t.Fatalf("limit metric families = %#v", families)
		}
		for _, metric := range families[0].Metric {
			if len(metric.Label) != 2 || formatLabels(metric.Label) == "" {
				t.Fatalf("limit metric labels are not bounded to stage/reason: %#v", metric.Label)
			}
		}
		assertObservabilityMetricValue(t, registry, "kubeseer_source_failures_total", `reason="SelectionLimitExceeded",stage="read"`, 2)
		assertObservabilityMetricValue(t, registry, "kubeseer_source_failures_total", `reason="ValueLimitExceeded",stage="extract"`, 1)
		assertObservabilityMetricValue(t, registry, "kubeseer_source_failures_total", `reason="cardinality-exceeded",stage="aggregate"`, 1)
		assertObservabilityMetricValue(t, registry, "kubeseer_source_failures_total", `reason="EvaluationTimedOut",stage="compose"`, 1)
		assertObservabilityMetricValue(t, registry, "kubeseer_source_failures_total", `reason="ResultLimitExceeded",stage="compose"`, 1)
		assertObservabilityMetricValue(t, registry, "kubeseer_source_failures_total", `reason="StatusLimitInvalid",stage="compose"`, 1)
	})

	t.Run("real runtime value overflow emits one source limit observation", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		capture := &observabilityCapture{}
		observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture})
		if err != nil {
			t.Fatalf("construct runtime limit observer: %v", err)
		}
		valueCeiling := int64(1)
		profile, err := limits.Resolve(limits.Overrides{MaxProducedValueBytes: &valueCeiling})
		if err != nil {
			t.Fatalf("resolve runtime value profile: %v", err)
		}
		key := types.NamespacedName{Namespace: "team-a", Name: "observability-limit-owner"}
		source := runtimePipelineValuesSource("observability-limit-source")
		reader := newRuntimePipelineReader(runtimePipelineKubeseer(key, "observability-limit-uid", 1, source))
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, unstructuredListForPipeline(runtimePipelineResource("observability-limit-resource", "observability-limit-resource-uid", "value", "not-an-integer")))
		publisher := &runtimePipelinePublisher{}
		runtime := mustPerformanceLimitsRuntime(t, resolver, reader, lister, &runtimePipelinePolicySource{policy: basePolicy()}, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker(), profile, observer)
		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("runtime value overflow returned error: %v", err)
		}
		var limitRecords []observability.LogRecord
		for _, record := range capture.snapshot() {
			if record.Reason == observability.ReasonValueLimitExceeded && record.Dimension != "" {
				limitRecords = append(limitRecords, record)
			}
		}
		if len(limitRecords) != 1 {
			t.Fatalf("runtime value limit records = %#v, want one", limitRecords)
		}
		if limitRecords[0].SourceID != source.ID || limitRecords[0].Stage != observability.StageExtract || limitRecords[0].Dimension != observability.LimitDimensionProducedValueBytes || limitRecords[0].Ceiling != valueCeiling {
			t.Fatalf("runtime value limit record = %#v", limitRecords[0])
		}
		publications := publisher.Publications()
		if len(publications) != 1 || publications[0].Result.Sources[0].Error == nil || publications[0].Result.Sources[0].Error.Reason != string(observability.ReasonValueLimitExceeded) {
			t.Fatalf("runtime value limit outcome = %#v", publications)
		}
	})

	t.Run("real runtime selection overflow emits one source limit observation", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		capture := &observabilityCapture{}
		observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture})
		if err != nil {
			t.Fatalf("construct runtime selection observer: %v", err)
		}
		resourceCeiling := 1
		profile, err := limits.Resolve(limits.Overrides{MaxMatchedResources: &resourceCeiling})
		if err != nil {
			t.Fatalf("resolve runtime selection profile: %v", err)
		}
		source := runtimePipelineValuesSource("observability-selection-source")
		key := types.NamespacedName{Namespace: "team-a", Name: "observability-selection-owner"}
		reader := newRuntimePipelineReader(runtimePipelineKubeseer(key, "observability-selection-uid", 1, source))
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			*runtimePipelineResource("observability-selection-a", "observability-selection-a-uid", "value-a", "1"),
			*runtimePipelineResource("observability-selection-b", "observability-selection-b-uid", "value-b", "2"),
		}})
		publisher := &runtimePipelinePublisher{}
		runtime := mustPerformanceLimitsRuntime(t, resolver, reader, lister, &runtimePipelinePolicySource{policy: basePolicy()}, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker(), profile, observer)
		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("runtime selection overflow returned error: %v", err)
		}
		limitRecords := recordsForReason(capture.snapshot(), observability.ReasonSelectionLimitExceeded)
		if len(limitRecords) != 1 || limitRecords[0].SourceID != source.ID || limitRecords[0].Stage != observability.StageRead || limitRecords[0].Dimension != observability.LimitDimensionMatchedResources || limitRecords[0].Ceiling != int64(resourceCeiling) {
			t.Fatalf("runtime selection limit records = %#v", limitRecords)
		}
		publications := publisher.Publications()
		if len(publications) != 1 || publications[0].Result.Sources[0].Error == nil || publications[0].Result.Sources[0].Error.Reason != string(observability.ReasonSelectionLimitExceeded) || len(publications[0].Result.Sources[0].Resources) != 0 {
			t.Fatalf("runtime selection limit outcome = %#v", publications)
		}
	})

	t.Run("real runtime aggregation overflow emits one aggregate limit observation", func(t *testing.T) {
		registry := prometheus.NewRegistry()
		capture := &observabilityCapture{}
		observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture})
		if err != nil {
			t.Fatalf("construct runtime aggregation observer: %v", err)
		}
		contributionCeiling := 1
		profile, err := limits.Resolve(limits.Overrides{Aggregation: limits.AggregationOverrides{MaxContributions: &contributionCeiling}})
		if err != nil {
			t.Fatalf("resolve runtime aggregation profile: %v", err)
		}
		source := v1alpha1.KubeseerSource{
			ID:           "observability-aggregation-source",
			Resource:     v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
			Namespaces:   &v1alpha1.NamespaceSelection{Names: []string{"team-a"}},
			Fields:       []v1alpha1.KubeseerField{{Name: "value", Path: "{.data.value}", Type: v1alpha1.ValueTypeInteger}},
			Aggregations: []v1alpha1.KubeseerAggregation{{Name: "count-values", Function: v1alpha1.AggregationCount, Field: "value"}},
		}
		key := types.NamespacedName{Namespace: "team-a", Name: "observability-aggregation-owner"}
		reader := newRuntimePipelineReader(runtimePipelineKubeseer(key, "observability-aggregation-uid", 1, source))
		lister := newRuntimePipelineLister()
		lister.SetResponse(source.ID, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			*runtimePipelineAggregationResource("team-a", "observability-aggregation-a", "observability-aggregation-a-uid", "blue", "1"),
			*runtimePipelineAggregationResource("team-a", "observability-aggregation-b", "observability-aggregation-b-uid", "red", "2"),
		}})
		publisher := &runtimePipelinePublisher{}
		runtime := mustPerformanceLimitsRuntime(t, resolver, reader, lister, &runtimePipelinePolicySource{policy: basePolicy()}, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker(), profile, observer)
		if _, err := runtime.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
			t.Fatalf("runtime aggregation overflow returned error: %v", err)
		}
		limitRecords := recordsForReason(capture.snapshot(), observability.ReasonCardinalityExceeded)
		if len(limitRecords) != 1 || limitRecords[0].SourceID != source.ID || limitRecords[0].Stage != observability.StageAggregate || limitRecords[0].Dimension != observability.LimitDimensionAggregationContributions || limitRecords[0].Ceiling != int64(contributionCeiling) {
			t.Fatalf("runtime aggregation limit records = %#v", limitRecords)
		}
		publications := publisher.Publications()
		if len(publications) != 1 || len(publications[0].Result.Sources[0].Aggregates) != 1 || publications[0].Result.Sources[0].Aggregates[0].Error == nil || publications[0].Result.Sources[0].Aggregates[0].Error.Reason != string(observability.ReasonCardinalityExceeded) {
			t.Fatalf("runtime aggregation limit outcome = %#v", publications)
		}
	})

	t.Run("status compaction and impossible compact status stay observable without writes changing", func(t *testing.T) {
		assessment := statuscontract.SourceAssessment{Index: 0, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationAllowedOutcome, Resolution: statuscontract.ResolutionResolvedOutcome}
		value := strings.Repeat("observable-status-value-", 512)
		evaluation := statuscontract.Evaluation{Result: &v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{{ID: "status-observable-source", State: v1alpha1.SourceStateValues, Resources: []v1alpha1.KubeseerResourceResult{{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "status-observable-resource", UID: "status-observable-uid", Fields: []v1alpha1.KubeseerFieldResult{{Name: "value", Type: v1alpha1.ValueTypeString, State: v1alpha1.FieldStateValues, Matches: []v1alpha1.KubeseerTypedMatch{{State: v1alpha1.MatchStateValue, StringValue: &value}}}}}}}}}, Sources: []statuscontract.SourceAssessment{assessment}}
		compact, err := statuscontract.ComposeResultLimitExceeded(1, nil, evaluation)
		if err != nil {
			t.Fatalf("compose observable compact status: %v", err)
		}
		compactSize, err := limits.CanonicalSize(compact)
		if err != nil {
			t.Fatalf("measure observable compact status: %v", err)
		}
		current := &v1alpha1.Kubeseer{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "observable-status", UID: "observable-status-uid", Generation: 1}}
		registry := prometheus.NewRegistry()
		capture := &observabilityCapture{}
		observer, err := observability.New(observability.Options{Registerer: registry, LogSink: capture})
		if err != nil {
			t.Fatalf("construct status limit observer: %v", err)
		}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		writer := &runtimeStatusWriter{}
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker, reconciliation.WithStatusObserver(observer), reconciliation.WithMaxStatusBytes(int64(compactSize))).Publish(ctx, lease, evaluation); err != nil {
			t.Fatalf("publish observable compact status: %v", err)
		}
		limitRecords := recordsForReason(capture.snapshot(), observability.ReasonResultLimitExceeded)
		if len(limitRecords) != 1 || limitRecords[0].Dimension != observability.LimitDimensionStatusBytes || limitRecords[0].Ceiling != int64(compactSize) || writer.Calls() != 1 {
			t.Fatalf("compact status observations/writes = %#v/%d", limitRecords, writer.Calls())
		}

		invalidRegistry := prometheus.NewRegistry()
		invalidCapture := &observabilityCapture{}
		invalidObserver, err := observability.New(observability.Options{Registerer: invalidRegistry, LogSink: invalidCapture})
		if err != nil {
			t.Fatalf("construct invalid-status observer: %v", err)
		}
		invalidTracker := reconciliation.NewFreshnessTracker()
		invalidLease, invalidRelease := runtimeStatusLease(t, invalidTracker, current)
		defer invalidRelease()
		invalidWriter := &runtimeStatusWriter{}
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), invalidWriter, invalidTracker, reconciliation.WithStatusObserver(invalidObserver), reconciliation.WithMaxStatusBytes(int64(compactSize-1))).Publish(ctx, invalidLease, evaluation); err == nil {
			t.Fatal("impossible compact status unexpectedly succeeded")
		}
		invalidRecords := recordsForReason(invalidCapture.snapshot(), observability.ReasonStatusLimitInvalid)
		if len(invalidRecords) != 1 || invalidRecords[0].Dimension != observability.LimitDimensionStatusBytes || invalidRecords[0].Ceiling != int64(compactSize-1) || invalidWriter.Calls() != 0 {
			t.Fatalf("invalid compact status observations/writes = %#v/%d", invalidRecords, invalidWriter.Calls())
		}
	})
	t.Log("MODULE_INTEGRATION=performance-and-limits STATUS=passed")
}

func recordsForReason(records []observability.LogRecord, reason observability.Reason) []observability.LogRecord {
	filtered := make([]observability.LogRecord, 0)
	for _, record := range records {
		if record.Reason == reason && record.Dimension != "" {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func assertPerformanceLimitsDeadlineStatusScenarios(t *testing.T) {
	t.Helper()

	t.Run("deadline retains completed sources and marks active and unstarted sources", func(t *testing.T) {
		first := runtimePipelineValuesSource("timeout-completed")
		active := runtimePipelineValuesSource("timeout-active")
		unstarted := runtimePipelineValuesSource("timeout-unstarted")
		key := types.NamespacedName{Namespace: "team-a", Name: "timeout-owner"}
		object := runtimePipelineKubeseer(key, "timeout-owner-uid", 1, first, active, unstarted)
		reader := newRuntimePipelineReader(object)
		lister := newRuntimePipelineLister()
		started := make(chan struct{})
		var startedOnce sync.Once
		lister.SetHook(func(ctx context.Context, target selection.ReadTarget, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			switch target.SourceID {
			case first.ID:
				return unstructuredListForPipeline(runtimePipelineResource("completed-resource", "completed-uid", "completed-value", "1")), nil
			case active.ID:
				startedOnce.Do(func() { close(started) })
				<-ctx.Done()
				return nil, ctx.Err()
			default:
				return &unstructured.UnstructuredList{}, nil
			}
		})
		timeout := 25 * time.Millisecond
		profile, err := limits.Resolve(limits.Overrides{EvaluationTimeout: &timeout})
		if err != nil {
			t.Fatalf("resolve timeout profile: %v", err)
		}
		timeoutCapture := &observabilityCapture{}
		observer, err := observability.New(observability.Options{Registerer: prometheus.NewRegistry(), LogSink: timeoutCapture})
		if err != nil {
			t.Fatalf("construct timeout observer: %v", err)
		}
		publisher := &runtimePipelinePublisher{}
		runtime := mustPerformanceLimitsRuntime(t, discovery.NewResolver(newPolicyDiscoveryClient()), reader, lister, &runtimePipelinePolicySource{policy: basePolicy()}, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker(), profile, observer)
		done := make(chan error, 1)
		go func() {
			_, reconcileErr := runtime.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			done <- reconcileErr
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timeout fixture did not reach the active source LIST")
		}
		var reconcileErr error
		select {
		case reconcileErr = <-done:
		case <-time.After(time.Second):
			t.Fatal("deadline did not cancel the active source")
		}
		if reconcileErr == nil || !reconciliation.IsRetryable(reconcileErr) || !strings.Contains(reconcileErr.Error(), string(reconciliation.ReasonEvaluationTimedOut)) {
			t.Fatalf("deadline reconciliation error = %v", reconcileErr)
		}
		publications := publisher.Publications()
		if len(publications) != 1 || publications[0].Evaluation.Result == nil || len(publications[0].Result.Sources) != 3 {
			t.Fatalf("deadline publication = %#v", publications)
		}
		result := publications[0].Result
		if result.Sources[0].State != v1alpha1.SourceStateValues || len(result.Sources[0].Resources) != 1 {
			t.Fatalf("completed source was not retained: %#v", result.Sources[0])
		}
		for _, index := range []int{1, 2} {
			if result.Sources[index].State != v1alpha1.SourceStateError || result.Sources[index].Error == nil || result.Sources[index].Error.Reason != string(reconciliation.ReasonEvaluationTimedOut) {
				t.Fatalf("timeout source %d = %#v", index, result.Sources[index])
			}
		}
		calls := lister.Calls()
		if len(calls) != 2 || calls[0].Target.SourceID != first.ID || calls[1].Target.SourceID != active.ID {
			t.Fatalf("deadline LIST calls = %#v, want completed and active sources only", calls)
		}
		limitRecords := recordsForReason(timeoutCapture.snapshot(), observability.ReasonEvaluationTimedOut)
		if len(limitRecords) != 1 || limitRecords[0].SourceID != "" || limitRecords[0].Stage != observability.StageCompose || limitRecords[0].Dimension != observability.LimitDimensionEvaluationTimeout || limitRecords[0].Ceiling != timeout.Milliseconds() {
			t.Fatalf("timeout limit records = %#v", limitRecords)
		}
	})

	t.Run("parent cancellation suppresses timeout publication", func(t *testing.T) {
		source := runtimePipelineValuesSource("cancel-parent")
		key := types.NamespacedName{Namespace: "team-a", Name: "cancel-parent-owner"}
		reader := newRuntimePipelineReader(runtimePipelineKubeseer(key, "cancel-parent-uid", 1, source))
		lister := newRuntimePipelineLister()
		started := make(chan struct{})
		var startedOnce sync.Once
		lister.SetHook(func(ctx context.Context, _ selection.ReadTarget, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			startedOnce.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		})
		timeout := time.Second
		profile, err := limits.Resolve(limits.Overrides{EvaluationTimeout: &timeout})
		if err != nil {
			t.Fatalf("resolve parent-cancellation profile: %v", err)
		}
		publisher := &runtimePipelinePublisher{}
		runtime := mustPerformanceLimitsRuntime(t, discovery.NewResolver(newPolicyDiscoveryClient()), reader, lister, &runtimePipelinePolicySource{policy: basePolicy()}, &runtimePipelineRoutes{}, publisher, reconciliation.NewFreshnessTracker(), profile)
		parent, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, reconcileErr := runtime.Reconcile(parent, reconcile.Request{NamespacedName: key})
			done <- reconcileErr
		}()
		select {
		case <-started:
			cancel()
		case <-time.After(time.Second):
			cancel()
			t.Fatal("parent-cancellation fixture did not reach the source LIST")
		}
		select {
		case reconcileErr := <-done:
			if reconcileErr != nil {
				t.Fatalf("parent cancellation returned an error: %v", reconcileErr)
			}
		case <-time.After(time.Second):
			t.Fatal("parent cancellation did not stop the active source")
		}
		if len(publisher.Publications()) != 0 {
			t.Fatalf("parent cancellation published %d status candidates", len(publisher.Publications()))
		}
	})

	t.Run("oversized status compacts at the exact boundary and recovers", func(t *testing.T) {
		key := types.NamespacedName{Namespace: "team-a", Name: "bounded-status"}
		assessment := statuscontract.SourceAssessment{Index: 0, Configuration: statuscontract.ConfigurationAcceptedOutcome, Authorization: statuscontract.AuthorizationAllowedOutcome, Resolution: statuscontract.ResolutionResolvedOutcome}
		hugeValue := strings.Repeat("status-value-", 2048)
		hugeResult := v1alpha1.KubeseerResult{Sources: []v1alpha1.KubeseerSourceResult{{ID: "bounded-status-source", State: v1alpha1.SourceStateValues, Resources: []v1alpha1.KubeseerResourceResult{{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "bounded-status-resource", UID: "bounded-status-uid", Fields: []v1alpha1.KubeseerFieldResult{{Name: "value", Type: v1alpha1.ValueTypeString, State: v1alpha1.FieldStateValues, Matches: []v1alpha1.KubeseerTypedMatch{{State: v1alpha1.MatchStateValue, StringValue: &hugeValue}}}}}}}}}
		evaluation := statuscontract.Evaluation{Result: &hugeResult, Sources: []statuscontract.SourceAssessment{assessment}}
		compact, err := statuscontract.ComposeResultLimitExceeded(1, nil, evaluation)
		if err != nil {
			t.Fatalf("compose compact status: %v", err)
		}
		compactSize, err := limits.CanonicalSize(compact)
		if err != nil {
			t.Fatalf("measure compact status: %v", err)
		}
		normal, err := statuscontract.Compose(1, nil, evaluation)
		if err != nil {
			t.Fatalf("compose oversized status: %v", err)
		}
		normalSize, err := limits.CanonicalSize(normal)
		if err != nil || normalSize <= compactSize {
			t.Fatalf("normal status size = %d, compact size = %d, err=%v", normalSize, compactSize, err)
		}
		current := &v1alpha1.Kubeseer{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name, UID: "bounded-status-uid", Generation: 1}}
		tracker := reconciliation.NewFreshnessTracker()
		lease, release := runtimeStatusLease(t, tracker, current)
		defer release()
		writer := &runtimeStatusWriter{}
		publisher := reconciliation.NewStatusPublisher(newRuntimeStatusReader(current), writer, tracker, reconciliation.WithMaxStatusBytes(int64(compactSize)))
		if err := publisher.Publish(context.Background(), lease, evaluation); err != nil {
			t.Fatalf("compact status publication: %v", err)
		}
		written := writer.Last()
		if writer.Calls() != 1 || written.Status.Result != nil || written.Status.Summary != nil || written.Status.ResultHash != "" || written.Status.ObservedGeneration != 1 {
			t.Fatalf("compact status write = calls=%d status=%#v", writer.Calls(), written.Status)
		}
		for _, check := range []struct{ typ, reason string }{{statuscontract.ConditionAccepted, statuscontract.ReasonConfigurationAccepted}, {statuscontract.ConditionAuthorized, statuscontract.ReasonAuthorizationSucceeded}, {statuscontract.ConditionSourcesResolved, statuscontract.ReasonResolutionSucceeded}, {statuscontract.ConditionReady, statuscontract.ReasonResultLimitExceeded}, {statuscontract.ConditionDegraded, statuscontract.ReasonResultLimitExceeded}} {
			condition := performanceStatusCondition(written.Status, check.typ)
			if condition.Reason != check.reason {
				t.Fatalf("compact condition %s = %#v, want reason %s", check.typ, condition, check.reason)
			}
		}

		repeatCurrent := written
		repeatTracker := reconciliation.NewFreshnessTracker()
		repeatLease, repeatRelease := runtimeStatusLease(t, repeatTracker, repeatCurrent)
		defer repeatRelease()
		repeatWriter := &runtimeStatusWriter{}
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(repeatCurrent), repeatWriter, repeatTracker, reconciliation.WithMaxStatusBytes(int64(compactSize))).Publish(context.Background(), repeatLease, evaluation); err != nil {
			t.Fatalf("repeated compact status publication: %v", err)
		}
		if repeatWriter.Calls() != 0 {
			t.Fatalf("repeated compact status issued %d writes", repeatWriter.Calls())
		}

		invalidTracker := reconciliation.NewFreshnessTracker()
		invalidLease, invalidRelease := runtimeStatusLease(t, invalidTracker, repeatCurrent)
		defer invalidRelease()
		invalidWriter := &runtimeStatusWriter{}
		invalidErr := reconciliation.NewStatusPublisher(newRuntimeStatusReader(repeatCurrent), invalidWriter, invalidTracker, reconciliation.WithMaxStatusBytes(int64(compactSize-1))).Publish(context.Background(), invalidLease, evaluation)
		var runtimeErr *reconciliation.RuntimeError
		if !errors.As(invalidErr, &runtimeErr) || runtimeErr.Reason != reconciliation.ReasonStatusLimitInvalid || invalidWriter.Calls() != 0 {
			t.Fatalf("invalid compact status = err=%v calls=%d", invalidErr, invalidWriter.Calls())
		}

		small := statuscontract.Evaluation{Result: &v1alpha1.KubeseerResult{}, Sources: []statuscontract.SourceAssessment{assessment}}
		smallStatus, err := statuscontract.Compose(1, repeatCurrent.Status.Conditions, small)
		if err != nil {
			t.Fatalf("compose recovery status: %v", err)
		}
		smallSize, err := limits.CanonicalSize(smallStatus)
		if err != nil {
			t.Fatalf("measure recovery status: %v", err)
		}
		recoveryTracker := reconciliation.NewFreshnessTracker()
		recoveryLease, recoveryRelease := runtimeStatusLease(t, recoveryTracker, repeatCurrent)
		defer recoveryRelease()
		recoveryWriter := &runtimeStatusWriter{}
		if err := reconciliation.NewStatusPublisher(newRuntimeStatusReader(repeatCurrent), recoveryWriter, recoveryTracker, reconciliation.WithMaxStatusBytes(int64(smallSize))).Publish(context.Background(), recoveryLease, small); err != nil {
			t.Fatalf("compact status recovery: %v", err)
		}
		if recoveryWriter.Calls() != 1 || recoveryWriter.Last().Status.Result == nil {
			t.Fatalf("compact status recovery write = calls=%d status=%#v", recoveryWriter.Calls(), recoveryWriter.Last().Status)
		}
	})

	floor, err := statuscontract.CompactStatusSize()
	if err != nil || floor <= 0 {
		t.Fatalf("compact status floor = %d, err=%v", floor, err)
	}
	t.Log("MODULE_INTEGRATION=performance-and-limits-deadline-status STATUS=passed")
}

func performanceStatusCondition(status v1alpha1.KubeseerStatus, conditionType string) metav1.Condition {
	for _, condition := range status.Conditions {
		if condition.Type == conditionType {
			return condition
		}
	}
	return metav1.Condition{}
}

func mustPerformanceLimitsRuntime(t *testing.T, resolver *discovery.Resolver, reader *runtimePipelineReader, lister *runtimePipelineLister, policy accesspolicy.PolicySource, routes *runtimePipelineRoutes, publisher *runtimePipelinePublisher, tracker *reconciliation.FreshnessTracker, profile limits.Profile, observers ...*observability.Observer) *reconciliation.Runtime {
	t.Helper()
	var observer *observability.Observer
	if len(observers) > 0 {
		observer = observers[0]
	}
	runtime, err := reconciliation.NewRuntime(reconciliation.Options{SafetyInterval: time.Hour, LimitProfile: &profile}, reconciliation.Dependencies{
		Reader:       reader,
		Lister:       reader,
		PolicySource: policy,
		Enforcer:     authorization.NewEnforcer(observability.NewAuthorizationRecorder(observer)),
		Planner:      selection.NewPlanner(resolver),
		Executor: selection.NewExecutor(lister,
			selection.WithVerifier(authorization.VerifierFunc(func(subject authorization.Subject) bool {
				return subject.Validate() == nil
			})),
			selection.WithLimits(profile),
			selection.WithPageObserver(observability.NewPageObserver(observer)),
		),
		Routes:    routes,
		Publisher: publisher,
		Tracker:   tracker,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("construct performance-limits runtime: %v", err)
	}
	return runtime
}

func assertPerformanceLimitsDiscoveryCacheScenarios(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	t.Run("one resolver shares metadata and promotes recent hits", func(t *testing.T) {
		client := newPerformanceLimitsDiscoveryClient()
		resolver := discovery.NewResolver(client, discovery.WithCacheCapacity(2), discovery.WithCacheTTL(time.Hour))
		first := resolvePerformanceDiscovery(t, ctx, resolver, "shared-first", "v1", "Pod")
		second := resolvePerformanceDiscovery(t, ctx, resolver, "shared-second", "v1", "Pod")
		if first.Resource != second.Resource || client.CallCount("v1") != 1 {
			t.Fatalf("equivalent manager resolutions = %#v/%#v calls=%d, want one shared metadata miss", first, second, client.CallCount("v1"))
		}
		resolvePerformanceDiscovery(t, ctx, resolver, "shared-deployment", "apps/v1", "Deployment")
		resolvePerformanceDiscovery(t, ctx, resolver, "shared-pod-hit", "v1", "Pod")
		resolvePerformanceDiscovery(t, ctx, resolver, "shared-job", "batch/v1", "Job")
		if client.CallCount("apps/v1") != 1 || client.CallCount("batch/v1") != 1 {
			t.Fatalf("initial bounded cache calls = apps/%d batch/%d", client.CallCount("apps/v1"), client.CallCount("batch/v1"))
		}
		resolvePerformanceDiscovery(t, ctx, resolver, "evicted-deployment", "apps/v1", "Deployment")
		if client.CallCount("apps/v1") != 2 {
			t.Fatalf("LRU hit promotion did not evict the least-recent deployment: apps/v1 calls=%d", client.CallCount("apps/v1"))
		}
	})

	t.Run("expired entries refresh at the configured TTL", func(t *testing.T) {
		client := newPerformanceLimitsDiscoveryClient()
		now := time.Unix(100, 0)
		resolver := discovery.NewResolver(client, discovery.WithCacheTTL(10*time.Second), discovery.WithClock(func() time.Time { return now }))
		resolvePerformanceDiscovery(t, ctx, resolver, "ttl-first", "v1", "Pod")
		now = now.Add(11 * time.Second)
		resolvePerformanceDiscovery(t, ctx, resolver, "ttl-expired", "v1", "Pod")
		if client.CallCount("v1") != 2 {
			t.Fatalf("TTL expiry calls=%d, want two discovery requests", client.CallCount("v1"))
		}
	})

	t.Run("concurrent equivalent misses collapse to one request", func(t *testing.T) {
		client := newPerformanceLimitsDiscoveryClient()
		entered, release := client.BlockOnce("v1")
		resolver := discovery.NewResolver(client, discovery.WithCacheCapacity(2))
		descriptor := discovery.SourceDescriptor{SourceID: "concurrent-miss", APIVersion: "v1", Kind: "Pod"}
		results := make(chan error, 2)
		go func() {
			_, err := resolver.Resolve(ctx, descriptor)
			results <- err
		}()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("first discovery miss did not reach the client")
		}
		go func() {
			_, err := resolver.Resolve(ctx, descriptor)
			results <- err
		}()
		if got := client.CallCount("v1"); got != 1 {
			t.Fatalf("collapsed miss issued %d discovery requests before release", got)
		}
		close(release)
		for index := 0; index < 2; index++ {
			select {
			case err := <-results:
				if err != nil {
					t.Fatalf("collapsed miss result %d: %v", index, err)
				}
			case <-time.After(time.Second):
				t.Fatal("collapsed miss did not complete")
			}
		}
	})

	t.Run("refreshing entries are not eviction victims", func(t *testing.T) {
		client := newPerformanceLimitsDiscoveryClient()
		now := time.Unix(200, 0)
		resolver := discovery.NewResolver(client, discovery.WithCacheCapacity(2), discovery.WithCacheTTL(time.Second), discovery.WithClock(func() time.Time { return now }))
		resolvePerformanceDiscovery(t, ctx, resolver, "refresh-pod", "v1", "Pod")
		resolvePerformanceDiscovery(t, ctx, resolver, "refresh-deployment", "apps/v1", "Deployment")
		now = now.Add(2 * time.Second)
		entered, release := client.BlockOnce("v1")
		refreshResult := make(chan error, 1)
		go func() {
			_, err := resolver.Resolve(ctx, discovery.SourceDescriptor{SourceID: "refresh-pod-again", APIVersion: "v1", Kind: "Pod"})
			refreshResult <- err
		}()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("refresh did not reach the discovery client")
		}
		resolvePerformanceDiscovery(t, ctx, resolver, "refresh-job", "batch/v1", "Job")
		close(release)
		select {
		case err := <-refreshResult:
			if err != nil {
				t.Fatalf("refresh result: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("refresh did not complete")
		}
		resolvePerformanceDiscovery(t, ctx, resolver, "evicted-deployment-after-refresh", "apps/v1", "Deployment")
		if client.CallCount("apps/v1") != 2 {
			t.Fatalf("refreshing cache entry was selected as victim: apps/v1 calls=%d", client.CallCount("apps/v1"))
		}
	})

	t.Run("invalidation advances the cache epoch before reuse", func(t *testing.T) {
		client := newPerformanceLimitsDiscoveryClient()
		resolver := discovery.NewResolver(client)
		resolvePerformanceDiscovery(t, ctx, resolver, "invalidate-resource", "v1", "Pod")
		resolver.InvalidateResource(" /v1 ", "Pod")
		resolvePerformanceDiscovery(t, ctx, resolver, "invalidate-resource-again", "v1", "Pod")
		if client.CallCount("v1") != 2 {
			t.Fatalf("resource invalidation calls=%d, want two", client.CallCount("v1"))
		}
		resolvePerformanceDiscovery(t, ctx, resolver, "invalidate-group", "apps/v1", "Deployment")
		resolver.InvalidateGroupVersion(" apps/v1 ")
		resolvePerformanceDiscovery(t, ctx, resolver, "invalidate-group-again", "apps/v1", "Deployment")
		if client.CallCount("apps/v1") != 2 {
			t.Fatalf("group invalidation calls=%d, want two", client.CallCount("apps/v1"))
		}
		resolvePerformanceDiscovery(t, ctx, resolver, "invalidate-all", "batch/v1", "Job")
		resolver.InvalidateAll()
		resolvePerformanceDiscovery(t, ctx, resolver, "invalidate-all-again", "batch/v1", "Job")
		if client.CallCount("batch/v1") != 2 {
			t.Fatalf("global invalidation calls=%d, want two", client.CallCount("batch/v1"))
		}
	})

	t.Run("invalidating an in-flight refresh prevents stale retention", func(t *testing.T) {
		client := newPerformanceLimitsDiscoveryClient()
		now := time.Unix(250, 0)
		resolver := discovery.NewResolver(client, discovery.WithCacheTTL(time.Second), discovery.WithClock(func() time.Time { return now }))
		resolvePerformanceDiscovery(t, ctx, resolver, "invalidation-refresh-first", "apps/v1", "Deployment")
		now = now.Add(2 * time.Second)
		clientEntered, release := client.BlockOnce("apps/v1")
		result := make(chan error, 1)
		go func() {
			_, err := resolver.Resolve(ctx, discovery.SourceDescriptor{SourceID: "invalidation-refresh", APIVersion: "apps/v1", Kind: "Deployment"})
			result <- err
		}()
		select {
		case <-clientEntered:
		case <-time.After(time.Second):
			t.Fatal("invalidation refresh did not reach the discovery client")
		}
		resolver.InvalidateResource("apps/v1", "Deployment")
		close(release)
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("invalidation refresh result: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("invalidation refresh did not converge")
		}
		if client.CallCount("apps/v1") != 3 {
			t.Fatalf("invalidated refresh was reused: apps/v1 calls=%d, want original plus stale and post-invalidation refresh", client.CallCount("apps/v1"))
		}
	})

	t.Run("uncached fresh results remain usable when every entry refreshes", func(t *testing.T) {
		client := newPerformanceLimitsDiscoveryClient()
		now := time.Unix(300, 0)
		resolver := discovery.NewResolver(client, discovery.WithCacheCapacity(1), discovery.WithCacheTTL(time.Second), discovery.WithClock(func() time.Time { return now }))
		resolvePerformanceDiscovery(t, ctx, resolver, "fallback-pod", "v1", "Pod")
		now = now.Add(2 * time.Second)
		entered, release := client.BlockOnce("v1")
		refreshResult := make(chan error, 1)
		go func() {
			_, err := resolver.Resolve(ctx, discovery.SourceDescriptor{SourceID: "fallback-pod-refresh", APIVersion: "v1", Kind: "Pod"})
			refreshResult <- err
		}()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("fallback refresh did not reach the discovery client")
		}
		resolvePerformanceDiscovery(t, ctx, resolver, "uncached-job", "batch/v1", "Job")
		if client.CallCount("batch/v1") != 1 {
			t.Fatalf("uncached fresh result did not complete exactly once: calls=%d", client.CallCount("batch/v1"))
		}
		close(release)
		if err := <-refreshResult; err != nil {
			t.Fatalf("fallback refresh result: %v", err)
		}
		resolvePerformanceDiscovery(t, ctx, resolver, "uncached-job-again", "batch/v1", "Job")
		if client.CallCount("batch/v1") != 2 {
			t.Fatalf("fallback result was retained despite no eviction victim: calls=%d", client.CallCount("batch/v1"))
		}
	})

	t.Run("policy epochs and payload sentinels stay outside metadata cache", func(t *testing.T) {
		client := newPerformanceLimitsDiscoveryClient()
		resolver := discovery.NewResolver(client)
		resolvePerformanceDiscovery(t, ctx, resolver, "policy-secret-payload-sentinel", "v1", "Pod")
		before := client.CallCount("v1")
		resolver.InvalidateAll()
		resolvePerformanceDiscovery(t, ctx, resolver, "policy-epoch-after-invalidation", "v1", "Pod")
		if client.CallCount("v1") != before+1 {
			t.Fatalf("policy epoch invalidation did not force fresh metadata: calls=%d", client.CallCount("v1"))
		}
		for _, key := range client.SeenKeys() {
			if strings.Contains(key, "secret") || strings.Contains(key, "payload") {
				t.Fatalf("discovery request key retained a source payload sentinel: %q", key)
			}
		}
	})

	t.Log("MODULE_INTEGRATION=performance-and-limits-discovery-cache STATUS=passed")
}

func resolvePerformanceDiscovery(t *testing.T, ctx context.Context, resolver *discovery.Resolver, sourceID, apiVersion, kind string) discovery.Resolution {
	t.Helper()
	resolution, err := resolver.Resolve(ctx, discovery.SourceDescriptor{SourceID: sourceID, APIVersion: apiVersion, Kind: kind})
	if err != nil {
		t.Fatalf("resolve %s %s/%s: %v", sourceID, apiVersion, kind, err)
	}
	return resolution
}

type performanceLimitsDiscoveryClient struct {
	mu           sync.Mutex
	resources    map[string]*metav1.APIResourceList
	calls        map[string]int
	keys         []string
	blockGroup   string
	blockEntered chan struct{}
	blockRelease chan struct{}
}

func newPerformanceLimitsDiscoveryClient() *performanceLimitsDiscoveryClient {
	return &performanceLimitsDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"v1":       {GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true}, {Name: "nodes", Kind: "Node"}}},
			"apps/v1":  {GroupVersion: "apps/v1", APIResources: []metav1.APIResource{{Name: "deployments", Kind: "Deployment", Namespaced: true}}},
			"batch/v1": {GroupVersion: "batch/v1", APIResources: []metav1.APIResource{{Name: "jobs", Kind: "Job", Namespaced: true}}},
		},
		calls: make(map[string]int),
	}
}

func (c *performanceLimitsDiscoveryClient) BlockOnce(groupVersion string) (<-chan struct{}, chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entered := make(chan struct{})
	release := make(chan struct{})
	c.blockGroup = groupVersion
	c.blockEntered = entered
	c.blockRelease = release
	return entered, release
}

func (c *performanceLimitsDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	c.mu.Lock()
	c.calls[groupVersion]++
	c.keys = append(c.keys, groupVersion)
	resources, ok := c.resources[groupVersion]
	var entered chan struct{}
	var release chan struct{}
	if c.blockGroup == groupVersion {
		entered = c.blockEntered
		release = c.blockRelease
		c.blockGroup = ""
		c.blockEntered = nil
		c.blockRelease = nil
	}
	var copyList *metav1.APIResourceList
	if ok {
		copyValue := *resources
		copyValue.APIResources = append([]metav1.APIResource(nil), resources.APIResources...)
		copyList = &copyValue
	}
	c.mu.Unlock()
	if entered != nil {
		close(entered)
		<-release
	}
	if !ok {
		return nil, errors.New("performance discovery group version unavailable")
	}
	return copyList, nil
}

func (c *performanceLimitsDiscoveryClient) CallCount(groupVersion string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[groupVersion]
}

func (c *performanceLimitsDiscoveryClient) SeenKeys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.keys...)
}

func assertPerformanceLimitsBackpressureScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	t.Run("trigger ingress coalesces identities and recovers after overflow", func(t *testing.T) {
		pending := 2
		profile, err := limits.Resolve(limits.Overrides{MaxPendingTriggers: &pending})
		if err != nil {
			t.Fatalf("resolve trigger profile: %v", err)
		}
		store := newRuntimeTestStore()
		routes := &runtimeTestRoutes{}
		source := reconciliation.NewTriggerSource(store, routes, reconciliation.Options{SafetyInterval: time.Hour, LimitProfile: &profile})
		first := types.NamespacedName{Namespace: "team-b", Name: "first"}
		second := types.NamespacedName{Namespace: "team-a", Name: "second"}
		overflow := types.NamespacedName{Namespace: "team-c", Name: "overflow"}
		source.Enqueue(first)
		source.Enqueue(first)
		source.Enqueue(second)
		source.Enqueue(overflow)
		if source.PendingCount() != pending || !source.RecoveryPending() {
			t.Fatalf("pre-start trigger ingress state = pending/%d recovery/%t, want bounded identities plus one recovery intent", source.PendingCount(), source.RecoveryPending())
		}

		queue := newRuntimeQueue()
		defer queue.ShutDown()
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := source.Start(watchContext, queue); err != nil {
			t.Fatalf("start bounded trigger source: %v", err)
		}
		waitForRuntimeQueue(t, queue, pending)
		items := make([]reconcile.Request, 0, pending)
		for index := 0; index < pending; index++ {
			item, shutdown := queue.Get()
			if shutdown {
				t.Fatal("trigger queue shut down while draining bounded identities")
			}
			items = append(items, item)
			queue.Done(item)
			queue.Forget(item)
		}
		want := []types.NamespacedName{second, first}
		for index, item := range items {
			if item.NamespacedName != want[index] {
				t.Fatalf("trigger drain order = %#v, want namespace/name order %#v", items, want)
			}
		}
		waitForRuntimeWatchCondition(t, func() bool { return source.PendingCount() == 0 && !source.RecoveryPending() })

		source.Enqueue(overflow)
		waitForRuntimeQueue(t, queue, 1)
		item, shutdown := queue.Get()
		if shutdown || item.NamespacedName != overflow {
			t.Fatalf("post-drain trigger = item=%#v shutdown=%t, want %s/%s", item, shutdown, overflow.Namespace, overflow.Name)
		}
		queue.Done(item)
		queue.Forget(item)
		if store.ListCalls() == 0 {
			t.Fatal("overflow recovery never used the enqueue-all safety path")
		}
	})

	t.Run("shared watch capacity retains excess bindings and promotes canonically", func(t *testing.T) {
		watcher := newRuntimeScriptedMetadataWatcher()
		tracker := reconciliation.NewFreshnessTracker()
		registry := reconciliation.NewRouteRegistry(watcher, tracker, reconciliation.WithMaxActiveWatches(1), reconciliation.WithRouteWatchBackoff(time.Millisecond, 4*time.Millisecond))
		pending := 2
		profile, err := limits.Resolve(limits.Overrides{MaxPendingTriggers: &pending})
		if err != nil {
			t.Fatalf("resolve route trigger profile: %v", err)
		}
		source := reconciliation.NewTriggerSource(newRuntimeTestStore(), registry, reconciliation.Options{SafetyInterval: time.Hour, LimitProfile: &profile})
		queue := newRuntimeQueue()
		defer queue.ShutDown()
		watchContext, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := source.Start(watchContext, queue); err != nil {
			t.Fatalf("start shared-watch trigger source: %v", err)
		}

		policy := basePolicy()
		policy.Spec.Resources = append(policy.Spec.Resources, v1alpha1.ResourceRule{APIGroups: []string{"batch"}, Kinds: []string{"Job"}})
		snapshot := mustSnapshot(t, policy)
		planner := selection.NewPlanner(resolver)
		appTarget := mustRuntimeTarget(t, ctx, planner, "team-a", v1alpha1.KubeseerSource{ID: "watch-capacity-app", Resource: v1alpha1.ResourceReference{APIVersion: "apps/v1", Kind: "Deployment"}, Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}}})
		podTarget := mustRuntimeTarget(t, ctx, planner, "team-a", v1alpha1.KubeseerSource{ID: "watch-capacity-pod", Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"}, Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}}})
		jobTarget := mustRuntimeTarget(t, ctx, planner, "team-a", v1alpha1.KubeseerSource{ID: "watch-capacity-job", Resource: v1alpha1.ResourceReference{APIVersion: "batch/v1", Kind: "Job"}, Namespaces: &v1alpha1.NamespaceSelection{Names: []string{"team-a"}}})
		appPeerTarget := appTarget
		appPeerTarget.SourceID = "watch-capacity-app-peer"

		ownerA := types.NamespacedName{Namespace: "team-a", Name: "capacity-a"}
		ownerPeer := types.NamespacedName{Namespace: "team-a", Name: "capacity-peer"}
		ownerB := types.NamespacedName{Namespace: "team-a", Name: "capacity-b"}
		ownerC := types.NamespacedName{Namespace: "team-a", Name: "capacity-c"}
		acquire := func(owner types.NamespacedName, uid types.UID) (reconciliation.Lease, func()) {
			object := newRuntimeKubeseer(owner, uid, 1)
			tracker.Observe(object)
			lease, _, release, acquireErr := tracker.Acquire(ctx, owner, object.UID, object.Generation)
			if acquireErr != nil {
				t.Fatalf("acquire route lease %s/%s: %v", owner.Namespace, owner.Name, acquireErr)
			}
			return lease, release
		}
		leaseA, releaseA := acquire(ownerA, "capacity-uid-a")
		defer releaseA()
		leasePeer, releasePeer := acquire(ownerPeer, "capacity-uid-peer")
		defer releasePeer()
		leaseB, releaseB := acquire(ownerB, "capacity-uid-b")
		defer releaseB()
		leaseC, releaseC := acquire(ownerC, "capacity-uid-c")
		defer releaseC()
		makeRoute := func(lease reconciliation.Lease, target selection.ReadTarget) reconciliation.AuthorizedRoute {
			route, routeErr := mustRuntimeAuthorizedRoute(t, lease, target, snapshot)
			if routeErr != nil {
				t.Fatalf("authorize bounded route: %v", routeErr)
			}
			return route
		}
		appAddress, err := reconciliation.AddressForTarget(appTarget)
		if err != nil {
			t.Fatalf("app watch address: %v", err)
		}
		podAddress, err := reconciliation.AddressForTarget(podTarget)
		if err != nil {
			t.Fatalf("pod watch address: %v", err)
		}
		jobAddress, err := reconciliation.AddressForTarget(jobTarget)
		if err != nil {
			t.Fatalf("job watch address: %v", err)
		}

		if err := registry.Replace(context.Background(), leaseA, []reconciliation.AuthorizedRoute{makeRoute(leaseA, appTarget)}); err != nil {
			t.Fatalf("install first bounded route: %v", err)
		}
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 1 })
		if registry.WatchCount() != 1 || watcher.Calls()[0].Address != appAddress {
			t.Fatalf("first bounded watch = count/%d calls=%#v", registry.WatchCount(), watcher.Calls())
		}
		if err := registry.Replace(context.Background(), leasePeer, []reconciliation.AuthorizedRoute{makeRoute(leasePeer, appPeerTarget)}); err != nil {
			t.Fatalf("install shared bounded route: %v", err)
		}
		if registry.WatchCount() != 1 || len(watcher.Calls()) != 1 || !reflect.DeepEqual(registry.Owners(appAddress), []types.NamespacedName{ownerA, ownerPeer}) {
			t.Fatalf("shared exact watch state = watches/%d calls/%d owners=%#v", registry.WatchCount(), len(watcher.Calls()), registry.Owners(appAddress))
		}
		if err := registry.Replace(context.Background(), leaseB, []reconciliation.AuthorizedRoute{makeRoute(leaseB, podTarget)}); err != nil {
			t.Fatalf("install excess pod route: %v", err)
		}
		if err := registry.Replace(context.Background(), leaseC, []reconciliation.AuthorizedRoute{makeRoute(leaseC, jobTarget)}); err != nil {
			t.Fatalf("install excess job route: %v", err)
		}
		if registry.WatchCount() != 1 || registry.BindingCount(ownerB) != 1 || registry.BindingCount(ownerC) != 1 {
			t.Fatalf("excess route fallback state = watches/%d bindings B/%d C/%d", registry.WatchCount(), registry.BindingCount(ownerB), registry.BindingCount(ownerC))
		}

		stream := watcher.Stream(0)
		if stream == nil {
			t.Fatal("shared watch has no stream")
		}
		stream.Action(watch.Added, runtimeMetadataObject("rv-capacity", "secret-route-value-sentinel"))
		got := collectRuntimeQueueKeys(t, queue, 2)
		if !got[ownerA] || !got[ownerPeer] || len(got) != 2 {
			t.Fatalf("shared watch trigger identities = %#v, want both owners", got)
		}
		if strings.Contains(fmt.Sprintf("%#v", registry), "secret-route-value-sentinel") {
			t.Fatal("shared route registry retained observed event payload")
		}

		registry.RemoveOwner(ownerA)
		if registry.WatchCount() != 1 || len(watcher.Calls()) != 1 || !reflect.DeepEqual(registry.Owners(appAddress), []types.NamespacedName{ownerPeer}) {
			t.Fatalf("ownerless shared-watch transition removed the peer: watches/%d calls=%d owners=%#v", registry.WatchCount(), len(watcher.Calls()), registry.Owners(appAddress))
		}
		registry.RemoveOwner(ownerPeer)
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 2 })
		if registry.WatchCount() != 1 || watcher.Calls()[1].Address != podAddress {
			t.Fatalf("canonical watch promotion = count/%d second=%#v, want pod address %#v", registry.WatchCount(), watcher.Calls()[1], podAddress)
		}
		registry.RemoveOwner(ownerB)
		waitForRuntimeWatchCondition(t, func() bool { return len(watcher.Calls()) >= 3 })
		if registry.WatchCount() != 1 || watcher.Calls()[2].Address != jobAddress {
			t.Fatalf("second watch promotion = count/%d third=%#v, want job address %#v", registry.WatchCount(), watcher.Calls()[2], jobAddress)
		}
		if len(registry.Owners(jobAddress)) != 1 || registry.Owners(jobAddress)[0] != ownerC {
			t.Fatalf("promoted excess route owners = %#v", registry.Owners(jobAddress))
		}

		tracker.InvalidateAll()
		if owners := registry.Owners(jobAddress); len(owners) != 0 || registry.WatchCount() != 0 || registry.BindingCount(ownerC) != 0 {
			t.Fatalf("policy epoch invalidation retained route state: owners=%#v watches=%d bindings=%d", owners, registry.WatchCount(), registry.BindingCount(ownerC))
		}
	})

	t.Log("MODULE_INTEGRATION=performance-and-limits-backpressure STATUS=passed")
}
