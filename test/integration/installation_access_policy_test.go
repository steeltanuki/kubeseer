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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/admission"
	"github.com/steeltanuki/kubeseer/internal/aggregation"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/extraction"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	crwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

// TestModuleIntegration composes the production discovery resolver with the
// policy compiler and evaluator. Only the narrow DiscoveryClient boundary is
// controlled; all policy behavior runs through its production contracts.
func TestModuleIntegration(t *testing.T) {
	ctx := context.Background()
	discoveryClient := newPolicyDiscoveryClient()
	resolver := discovery.NewResolver(discoveryClient)

	t.Run("compiler and evaluator use real discovery resolutions", func(t *testing.T) {
		assertEvaluationScenarios(t, ctx, resolver)
	})
	t.Run("compiler validation is defensive and deterministic", func(t *testing.T) {
		assertCompilerValidationScenarios(t)
	})
	t.Run("compiled snapshots are immutable and permutation stable", func(t *testing.T) {
		assertSnapshotStabilityScenarios(t, ctx, resolver)
	})
	t.Run("loader fail-closed transitions use real evaluation", func(t *testing.T) {
		assertLoaderScenarios(t, ctx, resolver)
	})
	t.Run("authorization enforcement preserves exact decisions and evidence", func(t *testing.T) {
		assertAuthorizationEnforcementCoreScenarios(t, ctx, resolver)
	})
	t.Run("resource selection planning uses real discovery", func(t *testing.T) {
		assertResourceSelectionPlanningScenarios(t, ctx, resolver)
	})
	t.Run("resource selection authorization is exact and complete", func(t *testing.T) {
		assertResourceSelectionAuthorizationScenarios(t, ctx, resolver)
	})
	t.Run("resource selection execution stays behind the authorization boundary", func(t *testing.T) {
		assertResourceSelectionExecutionBoundaryScenarios(t, ctx, resolver)
	})
	t.Run("authorization enforcement guards every LIST page and reports RBAC separately", func(t *testing.T) {
		assertAuthorizationEnforcementListScenarios(t, ctx, resolver)
	})
	t.Run("resource selection pagination is complete and deterministic", func(t *testing.T) {
		assertResourceSelectionPaginationScenarios(t, ctx, resolver)
	})
	t.Run("resource selection failures remain source-scoped and isolated", func(t *testing.T) {
		assertResourceSelectionFailureScenarios(t, ctx, resolver)
	})
	t.Run("field extraction planning accepts only the approved grammar", func(t *testing.T) {
		assertFieldExtractionPlanningScenarios(t)
	})
	t.Run("field extraction preserves native values and provenance", func(t *testing.T) {
		assertFieldExtractionNativeValueScenarios(t)
	})
	t.Run("field extraction batches preserve source atomicity and cancellation", func(t *testing.T) {
		assertFieldExtractionBatchScenarios(t)
	})
	t.Run("typed-output scalar planning and conversions remain explicit", func(t *testing.T) {
		assertTypedOutputConversionScenarios(t)
	})
	t.Run("typed-output outcomes preserve cardinality and composite values", func(t *testing.T) {
		assertTypedOutputCardinalityScenarios(t)
	})
	t.Run("typed-output results serialize through the structural status adapter", func(t *testing.T) {
		assertTypedOutputSerializationScenarios(t)
	})
	t.Run("typed-output batches isolate fields, sources, and cancellation", func(t *testing.T) {
		assertTypedOutputIsolationScenarios(t)
	})
	t.Run("value operators reuse typed conversion and plan complete sources", func(t *testing.T) {
		assertValueOperatorPlanningScenarios(t)
	})
	t.Run("value operators compare normalized scalar multi-match outcomes", func(t *testing.T) {
		assertValueOperatorComparisonScenarios(t)
	})
	t.Run("value operators apply string presence and membership predicates", func(t *testing.T) {
		assertValueOperatorPredicateScenarios(t)
	})
	t.Run("value operators apply ordered transformations and project outcomes", func(t *testing.T) {
		assertValueOperatorTransformationScenarios(t)
	})
	t.Run("value operators isolate sources and cancellation partitions", func(t *testing.T) {
		assertValueOperatorIsolationScenarios(t)
	})
	t.Run("cross-namespace aggregation planning reuses typed semantics", func(t *testing.T) {
		assertCrossNamespaceAggregationPlanningScenarios(t)
	})
	t.Run("cross-namespace aggregation forms deterministic typed groups", func(t *testing.T) {
		assertCrossNamespaceAggregationGroupingScenarios(t)
	})
	t.Run("cross-namespace aggregation applies closed reducers", func(t *testing.T) {
		assertCrossNamespaceAggregationReducerScenarios(t)
	})
	t.Run("cross-namespace aggregation applies exact arithmetic", func(t *testing.T) {
		assertCrossNamespaceAggregationArithmeticScenarios(t)
	})
	t.Run("cross-namespace aggregation isolates failures, limits, and cancellation", func(t *testing.T) {
		assertCrossNamespaceAggregationIsolationScenarios(t)
	})
	t.Run("cross-namespace aggregation publishes raw and aggregate status together", func(t *testing.T) {
		assertCrossNamespaceAggregationStatusScenarios(t)
	})
	t.Run("cross-namespace aggregation runs after operators in the production pipeline", func(t *testing.T) {
		assertCrossNamespaceAggregationPipelineScenarios(t, ctx, resolver)
	})
	t.Run("admission budgets stop before dynamic work", func(t *testing.T) {
		assertAdmissionValidationBudgetScenarios(t)
	})
	t.Run("admission semantic validation composes every pure planner", func(t *testing.T) {
		assertAdmissionValidationSemanticScenarios(t)
	})
	t.Run("admission dynamic validation uses current discovery and exact policy targets", func(t *testing.T) {
		assertAdmissionValidationDynamicScenarios(t)
	})
	t.Run("admission registration decodes and maps typed AdmissionReviews", func(t *testing.T) {
		assertAdmissionValidationRegistrationScenarios(t)
	})
	t.Run("reconciliation runtime scheduling preserves lifecycle and queue semantics", func(t *testing.T) {
		assertReconciliationRuntimeSchedulingScenarios(t)
	})
	t.Run("reconciliation runtime routes authorized metadata events", func(t *testing.T) {
		assertReconciliationRuntimeWatchRoutingScenarios(t, ctx, resolver)
	})
	t.Run("authorization enforcement retains current capability-backed watch permits", func(t *testing.T) {
		assertAuthorizationEnforcementWatchScenarios(t, ctx, resolver)
	})
	t.Run("reconciliation runtime composes ordered partial results", func(t *testing.T) {
		assertReconciliationRuntimePipelineScenarios(t, ctx, resolver)
	})
	t.Run("authorization enforcement composes fresh decisions through status", func(t *testing.T) {
		assertAuthorizationEnforcementPipelineScenarios(t, ctx, resolver)
	})
	t.Run("admission validation never substitutes runtime authority", func(t *testing.T) {
		assertAdmissionValidationRuntimeScenarios(t, ctx, resolver)
	})
	t.Run("reconciliation runtime publishes guarded semantic status", func(t *testing.T) {
		assertReconciliationRuntimeStatusScenarios(t, ctx)
	})
	t.Run("status result projection derives deterministic summary and hash", func(t *testing.T) {
		assertStatusAndConditionsResultScenarios(t)
	})
	t.Run("status conditions compose deterministic terminal outcomes", func(t *testing.T) {
		assertStatusAndConditionsConditionScenarios(t)
	})
	t.Run("status pipeline carries typed terminal assessments", func(t *testing.T) {
		assertStatusAndConditionsPipelineScenarios(t, ctx, resolver)
	})
	t.Run("status publisher writes only complete semantic snapshots", func(t *testing.T) {
		assertStatusAndConditionsPublisherScenarios(t, ctx)
	})
	t.Run("observability core keeps stable signals passive and bounded", func(t *testing.T) {
		assertObservabilityCoreScenarios(t)
	})
	t.Run("observability instruments authorization and successful LIST boundaries", func(t *testing.T) {
		assertObservabilityBoundaryScenarios(t, ctx, resolver)
	})
	t.Run("observability correlates production runtime outcomes and optional traces", func(t *testing.T) {
		assertObservabilityRuntimeScenarios(t, ctx, resolver)
	})
	t.Run("observability reports semantic status publication and Events", func(t *testing.T) {
		assertObservabilityStatusScenarios(t, ctx)
	})
	t.Run("observability reports unexpected WATCH stops and restarts", func(t *testing.T) {
		assertObservabilityWatchScenarios(t, ctx, resolver)
	})
	t.Run("observability composes one manager-wide observer through production", func(t *testing.T) {
		assertObservabilityManagerScenarios(t, ctx, resolver)
	})

	t.Log("MODULE_INTEGRATION=discovery-access-policy-evaluation STATUS=passed")
	t.Log("MODULE_INTEGRATION=discovery-access-policy-loader STATUS=passed")
	t.Log("MODULE_INTEGRATION=authorization-enforcement-core STATUS=passed")
	t.Log("MODULE_INTEGRATION=authorization-enforcement STATUS=passed")
	t.Log("MODULE_INTEGRATION=resource-selection-planning STATUS=passed")
	t.Log("MODULE_INTEGRATION=resource-selection-authorization STATUS=passed")
	t.Log("MODULE_INTEGRATION=resource-selection-execution-boundary STATUS=passed")
	t.Log("MODULE_INTEGRATION=authorization-enforcement-list STATUS=passed")
	t.Log("MODULE_INTEGRATION=resource-selection-pagination STATUS=passed")
	t.Log("MODULE_INTEGRATION=resource-selection-failures STATUS=passed")
	t.Log("MODULE_INTEGRATION=field-extraction-planning STATUS=passed")
	t.Log("MODULE_INTEGRATION=field-extraction-native-values STATUS=passed")
	t.Log("MODULE_INTEGRATION=field-extraction-batch STATUS=passed")
	t.Log("MODULE_INTEGRATION=typed-output-model-conversions STATUS=passed")
	t.Log("MODULE_INTEGRATION=typed-output-model-cardinality STATUS=passed")
	t.Log("MODULE_INTEGRATION=typed-output-model-serialization STATUS=passed")
	t.Log("MODULE_INTEGRATION=typed-output-model-isolation STATUS=passed")
	t.Log("MODULE_INTEGRATION=value-operators-planning STATUS=passed")
	t.Log("MODULE_INTEGRATION=value-operators-comparisons STATUS=passed")
	t.Log("MODULE_INTEGRATION=value-operators-predicates STATUS=passed")
	t.Log("MODULE_INTEGRATION=value-operators-transformations STATUS=passed")
	t.Log("MODULE_INTEGRATION=value-operators-isolation STATUS=passed")
	t.Log("MODULE_INTEGRATION=cross-namespace-aggregation-planning STATUS=passed")
	t.Log("MODULE_INTEGRATION=cross-namespace-aggregation-grouping STATUS=passed")
	t.Log("MODULE_INTEGRATION=cross-namespace-aggregation-reducers STATUS=passed")
	t.Log("MODULE_INTEGRATION=cross-namespace-aggregation-arithmetic STATUS=passed")
	t.Log("MODULE_INTEGRATION=cross-namespace-aggregation-isolation STATUS=passed")
	t.Log("MODULE_INTEGRATION=cross-namespace-aggregation-status STATUS=passed")
	t.Log("MODULE_INTEGRATION=cross-namespace-aggregation-pipeline STATUS=passed")
	t.Log("MODULE_INTEGRATION=admission-validation-budgets STATUS=passed")
	t.Log("MODULE_INTEGRATION=admission-validation-semantics STATUS=passed")
	t.Log("MODULE_INTEGRATION=admission-validation-dynamic STATUS=passed")
	t.Log("MODULE_INTEGRATION=admission-validation-registration STATUS=passed")
	t.Log("MODULE_INTEGRATION=reconciliation-runtime-scheduling STATUS=passed")
	t.Log("MODULE_INTEGRATION=reconciliation-runtime-watch-routing STATUS=passed")
	t.Log("MODULE_INTEGRATION=authorization-enforcement-watch STATUS=passed")
	t.Log("MODULE_INTEGRATION=reconciliation-runtime-pipeline STATUS=passed")
	t.Log("MODULE_INTEGRATION=authorization-enforcement-pipeline STATUS=passed")
	t.Log("MODULE_INTEGRATION=admission-validation-runtime STATUS=passed")
	t.Log("MODULE_INTEGRATION=reconciliation-runtime-status STATUS=passed")
	t.Log("MODULE_INTEGRATION=status-and-conditions-result STATUS=passed")
	t.Log("MODULE_INTEGRATION=status-and-conditions-conditions STATUS=passed")
	t.Log("MODULE_INTEGRATION=status-and-conditions-pipeline STATUS=passed")
	t.Log("MODULE_INTEGRATION=status-and-conditions-publisher STATUS=passed")
	t.Log("MODULE_INTEGRATION=admission-validation STATUS=passed")
	t.Log("MODULE_INTEGRATION=observability-core STATUS=passed")
	t.Log("MODULE_INTEGRATION=observability-boundaries STATUS=passed")
	t.Log("MODULE_INTEGRATION=observability-runtime STATUS=passed")
	t.Log("MODULE_INTEGRATION=observability-status STATUS=passed")
	t.Log("MODULE_INTEGRATION=observability-watch STATUS=passed")
	t.Log("MODULE_INTEGRATION=observability STATUS=passed")
}

func assertEvaluationScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	policy := basePolicy()
	snapshot := mustSnapshot(t, policy)

	tests := []struct {
		name       string
		descriptor discovery.SourceDescriptor
		namespace  string
		want       accesspolicy.Decision
	}{
		{
			name:       "core namespaced resource is allowed",
			descriptor: discovery.SourceDescriptor{SourceID: "pods", APIVersion: "v1", Kind: "Pod"},
			namespace:  "team-a",
			want:       allowedDecision(),
		},
		{
			name:       "grouped resource is matched exactly",
			descriptor: discovery.SourceDescriptor{SourceID: "deployments", APIVersion: "apps/v1", Kind: "Deployment"},
			namespace:  "team-a",
			want:       allowedDecision(),
		},
		{
			name:       "CRD-backed resource is matched exactly",
			descriptor: discovery.SourceDescriptor{SourceID: "widgets", APIVersion: "widgets.kubeseer.io/v1", Kind: "Widget"},
			namespace:  "team-a",
			want:       allowedDecision(),
		},
		{
			name:       "resource denial precedes namespace denial",
			descriptor: discovery.SourceDescriptor{SourceID: "jobs", APIVersion: "batch/v1", Kind: "Job"},
			namespace:  "team-b",
			want:       accesspolicy.Decision{Reason: accesspolicy.ReasonResourceDenied, Message: `resource type "batch"/"Job" is outside the installation policy`},
		},
		{
			name:       "excluded namespace wins over explicit inclusion",
			descriptor: discovery.SourceDescriptor{SourceID: "excluded-pods", APIVersion: "v1", Kind: "Pod"},
			namespace:  "team-b",
			want:       accesspolicy.Decision{Reason: accesspolicy.ReasonNamespaceDenied, Message: `namespace "team-b" is excluded by the installation policy`},
		},
		{
			name:       "default system namespace boundary denies",
			descriptor: discovery.SourceDescriptor{SourceID: "system-pods", APIVersion: "v1", Kind: "Pod"},
			namespace:  "kube-system",
			want:       accesspolicy.Decision{Reason: accesspolicy.ReasonNamespaceDenied, Message: `namespace "kube-system" is outside the installation policy`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := requestFromResolution(t, ctx, resolver, test.descriptor, test.namespace)
			if got := snapshot.Evaluate(request); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Evaluate(%#v) = %#v, want %#v", request, got, test.want)
			}
		})
	}

	t.Run("discovery preserves resource identity and scope", func(t *testing.T) {
		cases := []struct {
			descriptor discovery.SourceDescriptor
			group      string
			resource   string
			scope      discovery.Scope
		}{
			{descriptor: discovery.SourceDescriptor{SourceID: "pods", APIVersion: "v1", Kind: "Pod"}, resource: "pods", scope: discovery.ScopeNamespaced},
			{descriptor: discovery.SourceDescriptor{SourceID: "nodes", APIVersion: "v1", Kind: "Node"}, resource: "nodes", scope: discovery.ScopeCluster},
			{descriptor: discovery.SourceDescriptor{SourceID: "deployments", APIVersion: "apps/v1", Kind: "Deployment"}, group: "apps", resource: "deployments", scope: discovery.ScopeNamespaced},
			{descriptor: discovery.SourceDescriptor{SourceID: "widgets", APIVersion: "widgets.kubeseer.io/v1", Kind: "Widget"}, group: "widgets.kubeseer.io", resource: "widgets", scope: discovery.ScopeNamespaced},
		}
		for _, test := range cases {
			resolution, err := resolver.Resolve(ctx, test.descriptor)
			if err != nil {
				t.Fatalf("resolve %q: %v", test.descriptor.SourceID, err)
			}
			if resolution.Resource.Group != test.group || resolution.Resource.Resource != test.resource || resolution.Scope != test.scope {
				t.Fatalf("resolution for %q = %#v, want group=%q resource=%q scope=%q", test.descriptor.SourceID, resolution, test.group, test.resource, test.scope)
			}
		}
	})

	t.Run("namespace modes and exclusion precedence", func(t *testing.T) {
		all := basePolicy()
		all.Spec.Namespaces.Mode = v1alpha1.NamespaceModeAll
		all.Spec.Namespaces.Include = nil
		all.Spec.Namespaces.Exclude = nil
		allSnapshot := mustSnapshot(t, all)
		assertAllowed(t, allSnapshot, requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "all-system", APIVersion: "v1", Kind: "Pod"}, "kube-system"))

		explicit := basePolicy()
		explicit.Spec.Namespaces.Mode = v1alpha1.NamespaceModeExplicit
		explicit.Spec.Namespaces.Include = []string{"kube-system"}
		explicit.Spec.Namespaces.Exclude = nil
		explicitSnapshot := mustSnapshot(t, explicit)
		assertAllowed(t, explicitSnapshot, requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "explicit-system", APIVersion: "v1", Kind: "Pod"}, "kube-system"))
		assertDeniedReason(t, explicitSnapshot, requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "explicit-team", APIVersion: "v1", Kind: "Pod"}, "team-a"), accesspolicy.ReasonNamespaceDenied)

		excluded := basePolicy()
		excluded.Spec.Namespaces.Include = []string{"team-b"}
		excluded.Spec.Namespaces.Exclude = []string{"team-b"}
		excludedSnapshot := mustSnapshot(t, excluded)
		assertDeniedReason(t, excludedSnapshot, requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "excluded-explicit", APIVersion: "v1", Kind: "Pod"}, "team-b"), accesspolicy.ReasonNamespaceDenied)

		emptyOverride := basePolicy()
		emptyOverride.Spec.Namespaces.SystemNamespaces = []string{}
		emptySnapshot := mustSnapshot(t, emptyOverride)
		assertAllowed(t, emptySnapshot, requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "empty-system-override", APIVersion: "v1", Kind: "Pod"}, "kube-system"))
	})

	t.Run("cluster scope follows policy flag and ignores namespace state", func(t *testing.T) {
		nodeDescriptor := discovery.SourceDescriptor{SourceID: "nodes", APIVersion: "v1", Kind: "Node"}
		request := requestFromResolution(t, ctx, resolver, nodeDescriptor, "team-b")
		assertDeniedReason(t, snapshot, request, accesspolicy.ReasonClusterScopeDenied)

		clusterPolicy := basePolicy()
		clusterPolicy.Spec.AllowClusterScoped = true
		clusterSnapshot := mustSnapshot(t, clusterPolicy)
		assertAllowed(t, clusterSnapshot, request)
	})

	t.Run("invalid request is rejected before policy lookup", func(t *testing.T) {
		request := requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "invalid-request", APIVersion: "v1", Kind: "Pod"}, "team-a")
		request.Scope = discovery.Scope(99)
		assertDecision(t, snapshot, request, accesspolicy.Decision{Reason: accesspolicy.ReasonInvalidRequest, Message: "request scope must be namespaced or cluster"})

		request = requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "missing-kind", APIVersion: "v1", Kind: "Pod"}, "team-a")
		request.Kind = ""
		assertDecision(t, snapshot, request, accesspolicy.Decision{Reason: accesspolicy.ReasonInvalidRequest, Message: "request Kind is missing or not normalized"})

		request = requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "missing-namespace", APIVersion: "v1", Kind: "Pod"}, "team-a")
		request.Namespace = ""
		assertDecision(t, snapshot, request, accesspolicy.Decision{Reason: accesspolicy.ReasonInvalidRequest, Message: "request namespace is required for a namespaced resource"})

		request = requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "invalid-namespace", APIVersion: "v1", Kind: "Pod"}, "team-a")
		request.Namespace = "Invalid_Namespace"
		assertDeniedReason(t, snapshot, request, accesspolicy.ReasonInvalidRequest)
	})

	t.Run("logical policy decision is independent of source identity and RBAC", func(t *testing.T) {
		request := requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "secret-value-source", APIVersion: "v1", Kind: "Pod"}, "team-a")
		request.SourceID = "secret-value"
		decision := snapshot.Evaluate(request)
		if !decision.Allowed || decision.Reason != accesspolicy.ReasonAllowed {
			t.Fatalf("logical policy unexpectedly denied a valid request: %#v", decision)
		}
		if strings.Contains(decision.Message, request.SourceID) {
			t.Fatalf("decision leaked source identity: %#v", decision)
		}
	})
}

func assertCompilerValidationScenarios(t *testing.T) {
	t.Helper()
	tests := []struct {
		name      string
		policy    *v1alpha1.KubeseerAccessPolicy
		wantField string
		contains  string
	}{
		{name: "nil policy", policy: nil, wantField: "policy", contains: "must not be nil"},
		{name: "invalid active name", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) { policy.Name = "default" }), wantField: "metadata.name", contains: "installation-access-ceiling"},
		{name: "invalid namespace mode", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) { policy.Spec.Namespaces.Mode = "Unknown" }), wantField: "spec.namespaces.mode", contains: "Explicit, All, or AllNonSystem"},
		{name: "duplicate namespace entry", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
			policy.Spec.Namespaces.Include = []string{"team-a", "team-a"}
		}), wantField: "spec.namespaces.include[1]", contains: "duplicates entry"},
		{name: "invalid namespace entry", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
			policy.Spec.Namespaces.Include = []string{"Invalid_Namespace"}
		}), wantField: "spec.namespaces.include[0]", contains: "valid DNS-1123 namespace name"},
		{name: "empty resource rule members", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) { policy.Spec.Resources = []v1alpha1.ResourceRule{{}} }), wantField: "spec.resources[0].apiGroups", contains: "at least one API group"},
		{name: "duplicate resource entry", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
			policy.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{"apps", "apps"}, Kinds: []string{"Deployment"}}}
		}), wantField: "spec.resources[0].apiGroups[1]", contains: "duplicates entry"},
		{name: "malformed API group", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
			policy.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{"Apps"}, Kinds: []string{"Deployment"}}}
		}), wantField: "spec.resources[0].apiGroups[0]", contains: "valid DNS-1123 subdomain"},
		{name: "wildcard Kind", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
			policy.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{""}, Kinds: []string{"*"}}}
		}), wantField: "spec.resources[0].kinds[0]", contains: "CamelCase Kubernetes Kind"},
		{name: "malformed Kind", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
			policy.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{""}, Kinds: []string{"deployment"}}}
		}), wantField: "spec.resources[0].kinds[0]", contains: "CamelCase Kubernetes Kind"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := accesspolicy.Compile(test.policy)
			if err == nil {
				t.Fatal("Compile unexpectedly accepted invalid policy")
			}
			policyErr, ok := err.(*accesspolicy.PolicyError)
			if !ok || policyErr.Reason != accesspolicy.ReasonPolicyInvalid || policyErr.Field != test.wantField || !strings.Contains(policyErr.Message, test.contains) {
				t.Fatalf("Compile diagnostic = %#v, want reason=%q field=%q containing %q", err, accesspolicy.ReasonPolicyInvalid, test.wantField, test.contains)
			}
		})
	}

	first := mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
		policy.Name = "default"
		policy.Spec.Namespaces.Mode = "invalid"
		policy.Spec.Resources = []v1alpha1.ResourceRule{{}}
	})
	second := first.DeepCopy()
	_, firstErr := accesspolicy.Compile(first)
	_, secondErr := accesspolicy.Compile(second)
	if firstErr.Error() != secondErr.Error() || !accesspolicy.HasReason(firstErr, accesspolicy.ReasonPolicyInvalid) {
		t.Fatalf("diagnostic ordering is unstable: first=%v second=%v", firstErr, secondErr)
	}
}

func assertAdmissionValidationBudgetScenarios(t *testing.T) {
	t.Helper()

	defaults := admission.DefaultLimits()
	baseSource := func(id string) v1alpha1.KubeseerSource {
		return v1alpha1.KubeseerSource{
			ID:       id,
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		}
	}
	builders := []struct {
		name  string
		limit func(admission.Limits) int
		build func(int) *v1alpha1.Kubeseer
		path  string
	}{
		{
			name:  "sources",
			limit: func(limits admission.Limits) int { return limits.MaxKubeseerSources },
			path:  "spec.sources[32]",
			build: func(size int) *v1alpha1.Kubeseer {
				object := &v1alpha1.Kubeseer{}
				for index := 0; index < size; index++ {
					object.Spec.Sources = append(object.Spec.Sources, baseSource(fmt.Sprintf("source-%03d", index)))
				}
				return object
			},
		},
		{
			name:  "source namespaces",
			limit: func(limits admission.Limits) int { return limits.MaxSourceNamespaces },
			path:  "spec.sources[0].namespaces.names[64]",
			build: func(size int) *v1alpha1.Kubeseer {
				object := &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{baseSource("namespaces")}}}
				object.Spec.Sources[0].Namespaces = &v1alpha1.NamespaceSelection{}
				for index := 0; index < size; index++ {
					object.Spec.Sources[0].Namespaces.Names = append(object.Spec.Sources[0].Namespaces.Names, fmt.Sprintf("team-%03d", index))
				}
				return object
			},
		},
		{
			name:  "source fields",
			limit: func(limits admission.Limits) int { return limits.MaxSourceFields },
			path:  "spec.sources[0].fields[64]",
			build: func(size int) *v1alpha1.Kubeseer {
				object := &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{baseSource("fields")}}}
				for index := 0; index < size; index++ {
					object.Spec.Sources[0].Fields = append(object.Spec.Sources[0].Fields, v1alpha1.KubeseerField{Name: fmt.Sprintf("field-%03d", index), Path: "{.metadata.name}"})
				}
				return object
			},
		},
		{
			name:  "field operators",
			limit: func(limits admission.Limits) int { return limits.MaxFieldOperators },
			path:  "spec.sources[0].fields[0].operators[16]",
			build: func(size int) *v1alpha1.Kubeseer {
				source := baseSource("operators")
				source.Fields = []v1alpha1.KubeseerField{{Name: "value", Path: "{.metadata.name}"}}
				for index := 0; index < size; index++ {
					source.Fields[0].Operators = append(source.Fields[0].Operators, v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorExists})
				}
				return &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{source}}}
			},
		},
		{
			name:  "operator values",
			limit: func(limits admission.Limits) int { return limits.MaxOperatorValues },
			path:  "spec.sources[0].fields[0].operators[0].values[128]",
			build: func(size int) *v1alpha1.Kubeseer {
				source := baseSource("operator-values")
				operator := v1alpha1.KubeseerOperator{Operator: v1alpha1.OperatorIn}
				for index := 0; index < size; index++ {
					value := "value"
					operator.Values = append(operator.Values, v1alpha1.KubeseerOperatorOperand{State: v1alpha1.MatchStateValue, StringValue: &value})
				}
				source.Fields = []v1alpha1.KubeseerField{{Name: "value", Path: "{.metadata.name}", Operators: []v1alpha1.KubeseerOperator{operator}}}
				return &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{source}}}
			},
		},
		{
			name:  "source aggregations",
			limit: func(limits admission.Limits) int { return limits.MaxSourceAggregations },
			path:  "spec.sources[0].aggregations[32]",
			build: func(size int) *v1alpha1.Kubeseer {
				source := baseSource("aggregations")
				for index := 0; index < size; index++ {
					source.Aggregations = append(source.Aggregations, v1alpha1.KubeseerAggregation{Name: fmt.Sprintf("aggregate-%03d", index), Function: v1alpha1.AggregationCount, Field: "value"})
				}
				return &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{source}}}
			},
		},
		{
			name:  "aggregation groupBy",
			limit: func(limits admission.Limits) int { return limits.MaxAggregationGroupBy },
			path:  "spec.sources[0].aggregations[0].groupBy[16]",
			build: func(size int) *v1alpha1.Kubeseer {
				source := baseSource("group-by")
				aggregate := v1alpha1.KubeseerAggregation{Name: "aggregate", Function: v1alpha1.AggregationCount, Field: "value"}
				for index := 0; index < size; index++ {
					aggregate.GroupBy = append(aggregate.GroupBy, fmt.Sprintf("field-%03d", index))
				}
				source.Aggregations = []v1alpha1.KubeseerAggregation{aggregate}
				return &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{source}}}
			},
		},
		{
			name:  "selector match expressions",
			limit: func(limits admission.Limits) int { return limits.MaxSelectorMatchExpressions },
			path:  "spec.sources[0].selector.matchExpressions[64]",
			build: func(size int) *v1alpha1.Kubeseer {
				source := baseSource("expressions")
				source.Selector = &v1alpha1.ResourceSelector{}
				for index := 0; index < size; index++ {
					source.Selector.MatchExpressions = append(source.Selector.MatchExpressions, metav1.LabelSelectorRequirement{Key: fmt.Sprintf("label-%03d", index), Operator: metav1.LabelSelectorOpIn, Values: []string{"backend"}})
				}
				return &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{source}}}
			},
		},
		{
			name:  "selector expression values",
			limit: func(limits admission.Limits) int { return limits.MaxSelectorExpressionValues },
			path:  "spec.sources[0].selector.matchExpressions[0].values[64]",
			build: func(size int) *v1alpha1.Kubeseer {
				source := baseSource("expression-values")
				source.Selector = &v1alpha1.ResourceSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "tier", Operator: metav1.LabelSelectorOpIn}}}
				for index := 0; index < size; index++ {
					source.Selector.MatchExpressions[0].Values = append(source.Selector.MatchExpressions[0].Values, fmt.Sprintf("value-%03d", index))
				}
				return &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{source}}}
			},
		},
		{
			name:  "selector exact labels",
			limit: func(limits admission.Limits) int { return limits.MaxSelectorMatchLabels },
			path:  "spec.sources[0].selector.matchLabels[64]",
			build: func(size int) *v1alpha1.Kubeseer {
				source := baseSource("labels")
				source.Selector = &v1alpha1.ResourceSelector{MatchLabels: make(map[string]string, size)}
				for index := 0; index < size; index++ {
					source.Selector.MatchLabels[fmt.Sprintf("label-%03d", index)] = "backend"
				}
				return &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{source}}}
			},
		},
	}

	for _, test := range builders {
		t.Run(test.name, func(t *testing.T) {
			limit := test.limit(defaults)
			assertKubeseerBudgetBoundary(t, test.build(limit), defaults, false, "")
			assertKubeseerBudgetBoundary(t, test.build(limit+1), defaults, true, test.path)
		})
	}

	policyBuilders := []struct {
		name  string
		limit func(admission.Limits) int
		build func(int) *v1alpha1.KubeseerAccessPolicy
		path  string
	}{
		{
			name:  "policy namespace lists",
			limit: func(limits admission.Limits) int { return limits.MaxPolicyNamespaceNames },
			path:  "spec.namespaces.include[256]",
			build: func(size int) *v1alpha1.KubeseerAccessPolicy {
				policy := basePolicy()
				policy.Spec.Namespaces.Include = nil
				for index := 0; index < size; index++ {
					policy.Spec.Namespaces.Include = append(policy.Spec.Namespaces.Include, fmt.Sprintf("include-%03d", index))
				}
				return policy
			},
		},
		{
			name:  "policy resource rules",
			limit: func(limits admission.Limits) int { return limits.MaxPolicyResourceRules },
			path:  "spec.resources[128]",
			build: func(size int) *v1alpha1.KubeseerAccessPolicy {
				policy := basePolicy()
				policy.Spec.Resources = nil
				for index := 0; index < size; index++ {
					policy.Spec.Resources = append(policy.Spec.Resources, v1alpha1.ResourceRule{APIGroups: []string{""}, Kinds: []string{fmt.Sprintf("Kind%03d", index)}})
				}
				return policy
			},
		},
		{
			name:  "policy API groups",
			limit: func(limits admission.Limits) int { return limits.MaxPolicyAPIGroups },
			path:  "spec.resources[0].apiGroups[64]",
			build: func(size int) *v1alpha1.KubeseerAccessPolicy {
				policy := basePolicy()
				policy.Spec.Resources = []v1alpha1.ResourceRule{{Kinds: []string{"Pod"}}}
				for index := 0; index < size; index++ {
					policy.Spec.Resources[0].APIGroups = append(policy.Spec.Resources[0].APIGroups, fmt.Sprintf("group-%03d.example.com", index))
				}
				return policy
			},
		},
		{
			name:  "policy Kinds",
			limit: func(limits admission.Limits) int { return limits.MaxPolicyKinds },
			path:  "spec.resources[0].kinds[64]",
			build: func(size int) *v1alpha1.KubeseerAccessPolicy {
				policy := basePolicy()
				policy.Spec.Resources = []v1alpha1.ResourceRule{{APIGroups: []string{""}}}
				for index := 0; index < size; index++ {
					policy.Spec.Resources[0].Kinds = append(policy.Spec.Resources[0].Kinds, fmt.Sprintf("Kind%03d", index))
				}
				return policy
			},
		},
	}
	for _, test := range policyBuilders {
		t.Run(test.name, func(t *testing.T) {
			limit := test.limit(defaults)
			assertAccessPolicyBudgetBoundary(t, test.build(limit), defaults, false, "")
			assertAccessPolicyBudgetBoundary(t, test.build(limit+1), defaults, true, test.path)
		})
	}

	largeKubeseer := builders[2].build(defaults.MaxSourceFields)
	for index := range largeKubeseer.Spec.Sources[0].Fields {
		largeKubeseer.Spec.Sources[0].Fields[index].Path = strings.Repeat("x", 1024)
	}
	encodedKubeseer, err := json.Marshal(largeKubeseer.Spec)
	if err != nil {
		t.Fatalf("encode large Kubeseer budget fixture: %v", err)
	}
	if len(encodedKubeseer) >= defaults.MaxKubeseerSpecBytes {
		t.Fatalf("large Kubeseer fixture unexpectedly exceeds default budget before size scenario: %d", len(encodedKubeseer))
	}
	sizeLimits := defaults
	sizeLimits.MaxKubeseerSpecBytes = len(encodedKubeseer)
	assertKubeseerBudgetBoundary(t, largeKubeseer, sizeLimits, false, "")
	sizeLimits.MaxKubeseerSpecBytes--
	assertKubeseerBudgetBoundary(t, largeKubeseer, sizeLimits, true, "spec")

	largePolicy := policyBuilders[2].build(defaults.MaxPolicyAPIGroups)
	encodedPolicy, err := json.Marshal(largePolicy.Spec)
	if err != nil {
		t.Fatalf("encode large access policy budget fixture: %v", err)
	}
	if len(encodedPolicy) >= defaults.MaxAccessPolicySpecBytes {
		t.Fatalf("large access policy fixture unexpectedly exceeds default budget before size scenario: %d", len(encodedPolicy))
	}
	sizeLimits = defaults
	sizeLimits.MaxAccessPolicySpecBytes = len(encodedPolicy)
	assertAccessPolicyBudgetBoundary(t, largePolicy, sizeLimits, false, "")
	sizeLimits.MaxAccessPolicySpecBytes--
	assertAccessPolicyBudgetBoundary(t, largePolicy, sizeLimits, true, "spec")
}

func assertKubeseerBudgetBoundary(t *testing.T, object *v1alpha1.Kubeseer, limits admission.Limits, overBudget bool, wantPath string) {
	t.Helper()
	issues := admission.ValidateKubeseerBudgetsWithLimits(object, limits)
	if overBudget {
		if len(issues) == 0 {
			t.Fatal("over-budget Kubeseer was accepted")
		}
		if wantPath != "" && !hasBudgetPath(issues, wantPath) {
			t.Fatalf("over-budget Kubeseer paths = %#v, want %q", issues, wantPath)
		}
		client := newPolicyDiscoveryClient()
		resolver := discovery.NewResolver(client)
		dynamicCalls := 0
		policyCalls := 0
		validateThenRun := func() {
			if len(issues) != 0 {
				return
			}
			dynamicCalls++
			_, _ = resolver.Resolve(context.Background(), discovery.SourceDescriptor{SourceID: "budget", APIVersion: "v1", Kind: "Pod"})
			policyCalls++
		}
		validateThenRun()
		if dynamicCalls != 0 || client.calls["v1"] != 0 || policyCalls != 0 {
			t.Fatalf("over-budget Kubeseer reached dynamic work: planner=%d discovery=%d policy=%d", dynamicCalls, client.calls["v1"], policyCalls)
		}
		return
	}
	if len(issues) != 0 {
		t.Fatalf("boundary Kubeseer was rejected: %#v", issues)
	}
	client := newPolicyDiscoveryClient()
	resolver := discovery.NewResolver(client)
	if _, err := resolver.Resolve(context.Background(), discovery.SourceDescriptor{SourceID: "budget", APIVersion: "v1", Kind: "Pod"}); err != nil {
		t.Fatalf("boundary Kubeseer could not reach discovery: %v", err)
	}
	policyCalls := 0
	_ = accesspolicy.Load(context.Background(), accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
		policyCalls++
		return basePolicy(), nil
	}))
	if client.calls["v1"] == 0 || policyCalls != 1 {
		t.Fatalf("boundary Kubeseer did not reach dynamic work: discovery=%d policy=%d", client.calls["v1"], policyCalls)
	}
}

func assertAccessPolicyBudgetBoundary(t *testing.T, object *v1alpha1.KubeseerAccessPolicy, limits admission.Limits, overBudget bool, wantPath string) {
	t.Helper()
	issues := admission.ValidateAccessPolicyBudgetsWithLimits(object, limits)
	if overBudget {
		if len(issues) == 0 {
			t.Fatal("over-budget access policy was accepted")
		}
		if wantPath != "" && !hasBudgetPath(issues, wantPath) {
			t.Fatalf("over-budget access policy paths = %#v, want %q", issues, wantPath)
		}
		compileCalls := 0
		validateThenCompile := func() {
			if len(issues) != 0 {
				return
			}
			compileCalls++
			_, _ = accesspolicy.Compile(object)
		}
		validateThenCompile()
		if compileCalls != 0 {
			t.Fatalf("over-budget access policy reached policy compilation")
		}
		return
	}
	if len(issues) != 0 {
		t.Fatalf("boundary access policy was rejected: %#v", issues)
	}
	if _, err := accesspolicy.Compile(object); err != nil {
		t.Fatalf("boundary access policy could not reach policy compilation: %v", err)
	}
}

func hasBudgetPath(issues []admission.BudgetIssue, want string) bool {
	for _, issue := range issues {
		if issue.Path == want {
			return true
		}
	}
	return false
}

func assertAdmissionValidationSemanticScenarios(t *testing.T) {
	t.Helper()

	valid := &v1alpha1.Kubeseer{Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{{
		ID:       "valid-source",
		Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		Fields: []v1alpha1.KubeseerField{{
			Name: "value", Path: "{.metadata.name}", Type: v1alpha1.ValueTypeNumber,
		}},
		Aggregations: []v1alpha1.KubeseerAggregation{{
			Name: "average", Function: v1alpha1.AggregationAverage, Field: "value",
		}},
	}}}}
	original := valid.DeepCopy()
	if result := admission.ValidateKubeseerSemantics(valid); !result.Valid() {
		t.Fatalf("valid pure Kubeseer semantics produced issues: %#v", result.Issues)
	}
	if !reflect.DeepEqual(valid, original) {
		t.Fatal("semantic validation mutated the submitted Kubeseer")
	}

	empty := valid.DeepCopy()
	empty.Spec.Sources[0].Fields = nil
	empty.Spec.Sources[0].Aggregations = nil
	empty.Spec.Sources[0].Namespaces = &v1alpha1.NamespaceSelection{Names: []string{}}
	if result := admission.ValidateKubeseerSemantics(empty); !result.Valid() {
		t.Fatalf("omitted fields or explicit empty namespaces were rejected: %#v", result.Issues)
	}

	averagePlan := aggregation.PlanSource(valid.Spec.Sources[0], aggregation.DefaultLimits())
	if !averagePlan.Valid() || len(averagePlan.Plans()) != 1 {
		t.Fatalf("valid average declaration was not planned: %#v", averagePlan.Failures())
	}
	if plan := averagePlan.Plans()[0]; plan.Precision() != 6 || plan.RoundingMode() != v1alpha1.RoundingHalfEven {
		t.Fatalf("omitted average options did not resolve to defaults: precision=%d rounding=%q", plan.Precision(), plan.RoundingMode())
	}
	if valid.Spec.Sources[0].Aggregations[0].Precision != nil || valid.Spec.Sources[0].Aggregations[0].RoundingMode != "" {
		t.Fatal("average planning mutated omitted options")
	}

	secret := "secret-operand-that-must-not-escape"
	invalid := valid.DeepCopy()
	invalid.Spec.Sources = []v1alpha1.KubeseerSource{
		{
			ID:       "duplicate-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "malformed", Kind: "not-a-kind"},
			Namespaces: &v1alpha1.NamespaceSelection{Names: []string{
				"team-a", "team-a",
			}},
			Selector: &v1alpha1.ResourceSelector{Name: "bad/name", FieldSelector: "metadata.name=="},
			Fields: []v1alpha1.KubeseerField{
				{Name: "value", Path: "{.metadata.name}", Type: v1alpha1.ValueTypeString, Operators: []v1alpha1.KubeseerOperator{{
					Operator: v1alpha1.OperatorEq,
				}}},
				{Name: "value", Path: "{.metadata.name}", Type: v1alpha1.KubeseerValueType(secret)},
				{Name: "broken", Path: "{.metadata..name}", Type: v1alpha1.ValueTypeString},
			},
			Aggregations: []v1alpha1.KubeseerAggregation{
				{Name: "duplicate", Function: v1alpha1.AggregationCount, Field: "value"},
				{Name: "duplicate", Function: v1alpha1.AggregationCount, Field: "missing"},
			},
		},
		{
			ID:       "duplicate-source",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		},
	}
	result := admission.ValidateKubeseerSemantics(invalid)
	if result.Valid() || result.Class() != admission.Invalid || len(result.Issues) < 8 {
		t.Fatalf("multi-defect semantic result = %#v", result)
	}
	ordered := result.IssuesCopy()
	if !reflect.DeepEqual(ordered, result.IssuesCopy()) {
		t.Fatal("semantic issue ordering is unstable")
	}
	for index := 1; index < len(ordered); index++ {
		previous, current := ordered[index-1], ordered[index]
		if previous.Path > current.Path || previous.Path == current.Path && previous.Reason > current.Reason {
			t.Fatalf("semantic issues are not sorted: %#v", ordered)
		}
	}
	encoded, err := json.Marshal(result.Issues)
	if err != nil {
		t.Fatalf("encode semantic issues: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatal("semantic diagnostics leaked an operand value")
	}

	badPath := v1alpha1.KubeseerSource{ID: "wrapper", Fields: []v1alpha1.KubeseerField{{Name: "field", Path: "{.metadata..name}"}}}
	allExtraction := extraction.ValidateSource(badPath)
	_, firstExtraction := extraction.CompileSource(badPath)
	if len(allExtraction) == 0 || firstExtraction == nil || firstExtraction.FieldIndex != allExtraction[0].FieldIndex || firstExtraction.Reason != allExtraction[0].Reason {
		t.Fatalf("single-error extraction wrapper diverged from sorted multi-error result: all=%#v first=%#v", allExtraction, firstExtraction)
	}

	invalidPolicy := mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
		policy.Name = "default"
		policy.Spec.Namespaces.Mode = "invalid"
		policy.Spec.Resources = []v1alpha1.ResourceRule{{}}
	})
	policyIssues := accesspolicy.Validate(invalidPolicy)
	_, policyError := accesspolicy.Compile(invalidPolicy)
	if len(policyIssues) < 3 || policyError == nil {
		t.Fatalf("policy multi-error validation did not retain independent issues: issues=%#v error=%v", policyIssues, policyError)
	}
	firstPolicy, ok := policyError.(*accesspolicy.PolicyError)
	if !ok || firstPolicy.Field != policyIssues[0].Field || firstPolicy.Message != policyIssues[0].Message {
		t.Fatalf("policy compatibility wrapper did not return first sorted issue: issues=%#v error=%v", policyIssues, policyError)
	}

	narrowed := basePolicy()
	narrowed.Spec.Resources = nil
	if result := admission.ValidateAccessPolicySemantics(narrowed); !result.Valid() {
		t.Fatalf("valid narrowing policy was rejected: %#v", result.Issues)
	}
}

func assertAdmissionValidationDynamicScenarios(t *testing.T) {
	t.Helper()

	newObject := func(apiVersion, kind, sourceID string) *v1alpha1.Kubeseer {
		return &v1alpha1.Kubeseer{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
			Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{{
				ID:       sourceID,
				Resource: v1alpha1.ResourceReference{APIVersion: apiVersion, Kind: kind},
			}}},
		}
	}
	newPolicySource := func(calls *int, policy *v1alpha1.KubeseerAccessPolicy, sourceErr error) accesspolicy.PolicySource {
		return accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			*calls++
			if sourceErr != nil {
				return nil, sourceErr
			}
			return policy.DeepCopy(), nil
		})
	}
	assertResult := func(t *testing.T, got admission.Result, class admission.Class, reason string, wantIssues int) {
		t.Helper()
		if got.Class() != class || len(got.Issues) != wantIssues {
			t.Fatalf("admission result = %#v class=%q, want class=%q issues=%d", got.Issues, got.Class(), class, wantIssues)
		}
		if reason != "" {
			found := false
			for _, issue := range got.Issues {
				if issue.Reason == reason {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("admission result = %#v, want reason %q", got.Issues, reason)
			}
		}
	}

	t.Run("served namespaced resources use the owner namespace and one fresh policy", func(t *testing.T) {
		client := newPolicyDiscoveryClient()
		policyCalls := 0
		validator := admission.NewValidator(
			discovery.NewResolver(client),
			newPolicySource(&policyCalls, basePolicy(), nil),
		)
		got := validator.ValidateKubeseer(context.Background(), newObject("v1", "Pod", "served-pods"))
		if !got.Valid() {
			t.Fatalf("served namespaced object was rejected: %#v", got.Issues)
		}
		if client.calls["v1"] != 1 || policyCalls != 1 {
			t.Fatalf("dynamic calls = discovery %d policy %d, want one each", client.calls["v1"], policyCalls)
		}
	})

	t.Run("explicit namespaces evaluate every exact target", func(t *testing.T) {
		client := newPolicyDiscoveryClient()
		policy := basePolicy()
		policy.Spec.Namespaces.Mode = v1alpha1.NamespaceModeExplicit
		policy.Spec.Namespaces.Include = []string{"team-a"}
		policy.Spec.Namespaces.Exclude = nil
		policyCalls := 0
		object := newObject("v1", "Pod", "explicit-pods")
		object.Spec.Sources[0].Namespaces = &v1alpha1.NamespaceSelection{Names: []string{"team-a", "team-b"}}
		got := admission.NewValidator(discovery.NewResolver(client), newPolicySource(&policyCalls, policy, nil)).ValidateKubeseer(context.Background(), object)
		assertResult(t, got, admission.Forbidden, "NamespaceDenied", 1)
		if got.Issues[0].Path != "spec.sources[0].namespaces.names[1]" {
			t.Fatalf("target denial path = %q, want submitted namespace index", got.Issues[0].Path)
		}
		if policyCalls != 1 {
			t.Fatalf("policy calls = %d, want one fresh snapshot", policyCalls)
		}
	})

	t.Run("explicit empty namespaces still load policy without target evaluation", func(t *testing.T) {
		client := newPolicyDiscoveryClient()
		policyCalls := 0
		object := newObject("v1", "Pod", "empty-pods")
		object.Spec.Sources[0].Namespaces = &v1alpha1.NamespaceSelection{Names: []string{}}
		got := admission.NewValidator(discovery.NewResolver(client), newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(context.Background(), object)
		if !got.Valid() {
			t.Fatalf("explicit empty namespaces were rejected: %#v", got.Issues)
		}
		if client.calls["v1"] != 1 || policyCalls != 1 {
			t.Fatalf("dynamic calls = discovery %d policy %d, want discovery and one policy load", client.calls["v1"], policyCalls)
		}
	})

	t.Run("cluster scope is evaluated as one exact cluster target", func(t *testing.T) {
		client := newPolicyDiscoveryClient()
		policyCalls := 0
		object := newObject("v1", "Node", "cluster-nodes")
		got := admission.NewValidator(discovery.NewResolver(client), newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(context.Background(), object)
		assertResult(t, got, admission.Forbidden, "ClusterScopeDenied", 1)
		if policyCalls != 1 {
			t.Fatalf("policy calls after cluster denial = %d, want one", policyCalls)
		}

		policyCalls = 0
		allowedPolicy := mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) {
			policy.Spec.AllowClusterScoped = true
		})
		got = admission.NewValidator(discovery.NewResolver(newPolicyDiscoveryClient()), newPolicySource(&policyCalls, allowedPolicy, nil)).ValidateKubeseer(context.Background(), object)
		if !got.Valid() {
			t.Fatalf("cluster-scoped object was rejected after enabling cluster scope: %#v", got.Issues)
		}
		if policyCalls != 1 {
			t.Fatalf("policy calls after cluster allow = %d, want one", policyCalls)
		}
	})

	t.Run("cluster scope rejects a namespace declaration before policy load", func(t *testing.T) {
		policyCalls := 0
		object := newObject("v1", "Node", "invalid-cluster-nodes")
		object.Spec.Sources[0].Namespaces = &v1alpha1.NamespaceSelection{Names: []string{"team-a"}}
		got := admission.NewValidator(discovery.NewResolver(newPolicyDiscoveryClient()), newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(context.Background(), object)
		assertResult(t, got, admission.Invalid, "InvalidNamespaceScope", 1)
		if policyCalls != 0 {
			t.Fatalf("policy calls after scope conflict = %d, want zero", policyCalls)
		}
	})

	t.Run("unknown resources are invalid and do not load policy", func(t *testing.T) {
		client := newPolicyDiscoveryClient()
		policyCalls := 0
		object := newObject("v1", "Ghost", "unknown-resource")
		got := admission.NewValidator(discovery.NewResolver(client), newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(context.Background(), object)
		assertResult(t, got, admission.Invalid, "UnknownResource", 1)
		if policyCalls != 0 {
			t.Fatalf("policy calls after unknown resource = %d, want zero", policyCalls)
		}
	})

	t.Run("discovery unavailability is retryable and sanitized", func(t *testing.T) {
		policyCalls := 0
		resolver := admissionResolverFunc(func(context.Context, discovery.SourceDescriptor) (discovery.Resolution, error) {
			return discovery.Resolution{}, &discovery.ResolutionError{
				Reason:  discovery.ReasonDiscoveryUnavailable,
				Message: "raw-discovery-secret",
			}
		})
		got := admission.NewValidator(resolver, newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(context.Background(), newObject("v1", "Pod", "unavailable-resource"))
		assertResult(t, got, admission.Unavailable, "ValidationUnavailable", 1)
		encoded, err := json.Marshal(got.Issues)
		if err != nil {
			t.Fatalf("encode discovery result: %v", err)
		}
		if strings.Contains(string(encoded), "raw-discovery-secret") || policyCalls != 0 {
			t.Fatalf("discovery failure was not sanitized or policy was loaded: result=%s policyCalls=%d", encoded, policyCalls)
		}
	})

	t.Run("mixed source outcomes retain invalid precedence and skip policy", func(t *testing.T) {
		policyCalls := 0
		resolver := admissionResolverFunc(func(_ context.Context, descriptor discovery.SourceDescriptor) (discovery.Resolution, error) {
			if descriptor.SourceID == "unknown" {
				return discovery.Resolution{}, discovery.NewResolutionError(descriptor.SourceID, discovery.ReasonUnknownType, "unknown")
			}
			return discovery.Resolution{}, discovery.NewResolutionError(descriptor.SourceID, discovery.ReasonDiscoveryUnavailable, "unavailable")
		})
		object := newObject("v1", "Pod", "unknown")
		object.Spec.Sources = append(object.Spec.Sources, v1alpha1.KubeseerSource{
			ID:       "unavailable",
			Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
		})
		got := admission.NewValidator(resolver, newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(context.Background(), object)
		assertResult(t, got, admission.Invalid, "UnknownResource", 2)
		if got.Issues[0].Path != "spec.sources[0]" || got.Issues[1].Path != "spec.sources[1]" {
			t.Fatalf("mixed source paths = %#v, want submitted source order", got.Issues)
		}
		if policyCalls != 0 {
			t.Fatalf("policy calls after mixed discovery outcomes = %d, want zero", policyCalls)
		}
	})

	t.Run("all policy terminal states are fresh and classified", func(t *testing.T) {
		missing := apierrors.NewNotFound(schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: "kubeseeraccesspolicies"}, v1alpha1.InstallationAccessCeilingName)
		cases := []struct {
			name      string
			policy    *v1alpha1.KubeseerAccessPolicy
			err       error
			class     admission.Class
			reason    string
			confident string
		}{
			{name: "missing", err: missing, class: admission.Forbidden, reason: "PolicyMissing"},
			{name: "invalid", policy: mutatePolicy(basePolicy(), func(policy *v1alpha1.KubeseerAccessPolicy) { policy.Name = "default" }), class: admission.Forbidden, reason: "PolicyInvalid"},
			{name: "unavailable", err: errors.New("raw-policy-secret"), class: admission.Unavailable, reason: "ValidationUnavailable", confident: "raw-policy-secret"},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				policyCalls := 0
				got := admission.NewValidator(discovery.NewResolver(newPolicyDiscoveryClient()), newPolicySource(&policyCalls, test.policy, test.err)).ValidateKubeseer(context.Background(), newObject("v1", "Pod", "policy-"+test.name))
				assertResult(t, got, test.class, test.reason, 1)
				if policyCalls != 1 {
					t.Fatalf("policy calls = %d, want one fresh load", policyCalls)
				}
				encoded, err := json.Marshal(got.Issues)
				if err != nil {
					t.Fatalf("encode policy result: %v", err)
				}
				if test.confident != "" && strings.Contains(string(encoded), test.confident) {
					t.Fatalf("policy diagnostic leaked upstream error: %s", encoded)
				}
			})
		}
	})

	t.Run("equivalent discovery state produces equivalent outcomes", func(t *testing.T) {
		object := newObject("v1", "Pod", "equivalent")
		first := admission.NewValidator(discovery.NewResolver(newPolicyDiscoveryClient()), newPolicySource(new(int), basePolicy(), nil)).ValidateKubeseer(context.Background(), object)
		second := admission.NewValidator(discovery.NewResolver(newPolicyDiscoveryClient()), newPolicySource(new(int), basePolicy(), nil)).ValidateKubeseer(context.Background(), object.DeepCopy())
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("equivalent dynamic outcomes differ: first=%#v second=%#v", first, second)
		}
	})

	t.Run("cancellation is unavailable and does not load policy", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		policyCalls := 0
		got := admission.NewValidator(discovery.NewResolver(newPolicyDiscoveryClient()), newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(ctx, newObject("v1", "Pod", "cancelled"))
		assertResult(t, got, admission.Unavailable, "ValidationUnavailable", 1)
		if policyCalls != 0 {
			t.Fatalf("policy calls after cancellation = %d, want zero", policyCalls)
		}
	})

	t.Run("static defects stop discovery and policy work", func(t *testing.T) {
		resolverCalls := 0
		resolver := admissionResolverFunc(func(context.Context, discovery.SourceDescriptor) (discovery.Resolution, error) {
			resolverCalls++
			return discovery.Resolution{Resource: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Scope: discovery.ScopeNamespaced}, nil
		})
		policyCalls := 0
		object := newObject("bad/version/extra", "Pod", "static-invalid")
		got := admission.NewValidator(resolver, newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(context.Background(), object)
		assertResult(t, got, admission.Invalid, "InvalidAPIVersion", 1)
		if resolverCalls != 0 || policyCalls != 0 {
			t.Fatalf("static semantic failure reached dynamic work: resolver=%d policy=%d", resolverCalls, policyCalls)
		}

		resolverCalls = 0
		policyCalls = 0
		budgetObject := newObject("v1", "Pod", "budget-invalid")
		for index := 0; index < admission.DefaultMaxSourceFields+1; index++ {
			budgetObject.Spec.Sources[0].Fields = append(budgetObject.Spec.Sources[0].Fields, v1alpha1.KubeseerField{Name: fmt.Sprintf("field-%03d", index), Path: "{.metadata.name}"})
		}
		got = admission.NewValidator(resolver, newPolicySource(&policyCalls, basePolicy(), nil)).ValidateKubeseer(context.Background(), budgetObject)
		assertResult(t, got, admission.Invalid, "ConfigurationBudgetExceeded", 1)
		if resolverCalls != 0 || policyCalls != 0 {
			t.Fatalf("static budget failure reached dynamic work: resolver=%d policy=%d", resolverCalls, policyCalls)
		}
	})

	t.Run("one validator call loads one fresh policy snapshot", func(t *testing.T) {
		policyCalls := 0
		validator := admission.NewValidator(discovery.NewResolver(newPolicyDiscoveryClient()), newPolicySource(&policyCalls, basePolicy(), nil))
		object := newObject("v1", "Pod", "fresh-load")
		if got := validator.ValidateKubeseer(context.Background(), object); !got.Valid() {
			t.Fatalf("first fresh validation failed: %#v", got.Issues)
		}
		if got := validator.ValidateKubeseer(context.Background(), object.DeepCopy()); !got.Valid() {
			t.Fatalf("second fresh validation failed: %#v", got.Issues)
		}
		if policyCalls != 2 {
			t.Fatalf("policy calls across independent validations = %d, want two", policyCalls)
		}
	})
}

type admissionResolverFunc func(context.Context, discovery.SourceDescriptor) (discovery.Resolution, error)

func (f admissionResolverFunc) Resolve(ctx context.Context, descriptor discovery.SourceDescriptor) (discovery.Resolution, error) {
	return f(ctx, descriptor)
}

func assertAdmissionValidationRegistrationScenarios(t *testing.T) {
	t.Helper()

	newKubeseer := func(namespace, sourceID, apiVersion, kind string) *v1alpha1.Kubeseer {
		return &v1alpha1.Kubeseer{
			TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "Kubeseer"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example",
				Namespace: namespace,
			},
			Spec: v1alpha1.KubeseerSpec{Sources: []v1alpha1.KubeseerSource{{
				ID:       sourceID,
				Resource: v1alpha1.ResourceReference{APIVersion: apiVersion, Kind: kind},
			}}},
		}
	}
	newPolicy := func(name string) *v1alpha1.KubeseerAccessPolicy {
		policy := basePolicy()
		policy.TypeMeta = metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "KubeseerAccessPolicy"}
		policy.Name = name
		return policy
	}
	newServer := func(validator *admission.Validator) *httptest.Server {
		scheme := runtime.NewScheme()
		if err := v1alpha1.AddToScheme(scheme); err != nil {
			t.Fatalf("add Kubeseer scheme: %v", err)
		}
		server := crwebhook.NewServer(crwebhook.Options{WebhookMux: http.NewServeMux()})
		admission.Register(server, scheme, validator)
		return httptest.NewServer(server.WebhookMux())
	}
	newReview := func(t *testing.T, object runtime.Object, resource string, operation admissionv1.Operation, old runtime.Object, dryRun *bool) admissionv1.AdmissionReview {
		t.Helper()
		objectRaw, err := json.Marshal(object)
		if err != nil {
			t.Fatalf("encode admission object: %v", err)
		}
		request := &admissionv1.AdmissionRequest{
			UID: types.UID("registration-" + resource + "-" + strings.ToLower(string(operation))),
			Kind: metav1.GroupVersionKind{
				Group:   v1alpha1.GroupVersion.Group,
				Version: v1alpha1.GroupVersion.Version,
				Kind:    object.GetObjectKind().GroupVersionKind().Kind,
			},
			Resource: metav1.GroupVersionResource{
				Group:    v1alpha1.GroupVersion.Group,
				Version:  v1alpha1.GroupVersion.Version,
				Resource: resource,
			},
			Namespace: objectNamespace(object),
			Operation: operation,
			Object:    runtime.RawExtension{Raw: objectRaw},
			DryRun:    dryRun,
		}
		if old != nil {
			oldRaw, err := json.Marshal(old)
			if err != nil {
				t.Fatalf("encode old admission object: %v", err)
			}
			request.OldObject = runtime.RawExtension{Raw: oldRaw}
		}
		return admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
			Request:  request,
		}
	}
	sendPayload := func(t *testing.T, server *httptest.Server, path string, payload []byte) admissionv1.AdmissionReview {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("create AdmissionReview request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("send AdmissionReview: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("AdmissionReview HTTP status = %d, want 200", response.StatusCode)
		}
		var decoded admissionv1.AdmissionReview
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode AdmissionReview response: %v", err)
		}
		if decoded.Response == nil {
			t.Fatal("AdmissionReview response is missing response object")
		}
		return decoded
	}
	send := func(t *testing.T, server *httptest.Server, path string, review admissionv1.AdmissionReview) admissionv1.AdmissionReview {
		t.Helper()
		payload, err := json.Marshal(review)
		if err != nil {
			t.Fatalf("encode AdmissionReview: %v", err)
		}
		return sendPayload(t, server, path, payload)
	}
	assertResponse := func(t *testing.T, response admissionv1.AdmissionReview, allowed bool, code int32, reason metav1.StatusReason) {
		t.Helper()
		if response.Response == nil || response.Response.Allowed != allowed || response.Response.Result == nil {
			t.Fatalf("AdmissionReview response = %#v, want allowed=%t code=%d reason=%q", response.Response, allowed, code, reason)
		}
		if response.Response.Result.Code != code || response.Response.Result.Reason != reason {
			t.Fatalf("AdmissionReview status = %#v, want code=%d reason=%q", response.Response.Result, code, reason)
		}
	}
	assertCauses := func(t *testing.T, response admissionv1.AdmissionReview, wantPaths []string) {
		t.Helper()
		if response.Response == nil || response.Response.Result == nil || response.Response.Result.Details == nil {
			t.Fatalf("AdmissionReview response has no causes: %#v", response.Response)
		}
		causes := response.Response.Result.Details.Causes
		if len(causes) != len(wantPaths) {
			t.Fatalf("AdmissionReview causes = %#v, want %d causes", causes, len(wantPaths))
		}
		for index, path := range wantPaths {
			if causes[index].Field != path || causes[index].Type == "" || causes[index].Message == "" {
				t.Fatalf("AdmissionReview cause[%d] = %#v, want field %q and stable type/message", index, causes[index], path)
			}
		}
	}

	valid := newKubeseer("team-a", "pods", "v1", "Pod")
	validPolicy := newPolicy(v1alpha1.InstallationAccessCeilingName)
	policyCalls := 0
	validator := admission.NewValidator(
		discovery.NewResolver(newPolicyDiscoveryClient()),
		accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			policyCalls++
			return validPolicy.DeepCopy(), nil
		}),
	)
	server := newServer(validator)
	defer server.Close()

	create := newReview(t, valid, "kubeseers", admissionv1.Create, nil, nil)
	allowed := send(t, server, admission.KubeseerWebhookPath, create)
	assertResponse(t, allowed, true, http.StatusOK, "")
	if allowed.Response.UID != create.Request.UID {
		t.Fatalf("allowed response UID = %q, want %q", allowed.Response.UID, create.Request.UID)
	}
	if policyCalls != 1 {
		t.Fatalf("policy calls after AdmissionReview = %d, want one", policyCalls)
	}

	dryRun := true
	dryRunResponse := send(t, server, admission.KubeseerWebhookPath, newReview(t, valid, "kubeseers", admissionv1.Create, nil, &dryRun))
	if dryRunResponse.Response.Allowed != allowed.Response.Allowed || dryRunResponse.Response.Result.Code != allowed.Response.Result.Code || dryRunResponse.Response.Result.Reason != allowed.Response.Result.Reason {
		t.Fatalf("dry-run response = %#v, want same decision as non-dry-run %#v", dryRunResponse.Response, allowed.Response)
	}
	if policyCalls != 2 {
		t.Fatalf("policy calls after dry-run AdmissionReview = %d, want two independent loads", policyCalls)
	}

	invalid := newKubeseer("team-a", "same", "bad/version/extra", "Pod")
	invalid.Spec.Sources = append(invalid.Spec.Sources, v1alpha1.KubeseerSource{
		ID:       "same",
		Resource: v1alpha1.ResourceReference{APIVersion: "v1", Kind: "Pod"},
	})
	invalidResponse := send(t, server, admission.KubeseerWebhookPath, newReview(t, invalid, "kubeseers", admissionv1.Create, nil, nil))
	assertResponse(t, invalidResponse, false, http.StatusUnprocessableEntity, metav1.StatusReasonInvalid)
	assertCauses(t, invalidResponse, []string{"spec.sources[0].resource.apiVersion", "spec.sources[1].id"})
	if strings.Contains(invalidResponse.Response.Result.Message, "bad/version/extra") {
		t.Fatal("invalid top-level message exposed submitted API version")
	}

	oldInvalid := invalid.DeepCopy()
	updateAllowed := send(t, server, admission.KubeseerWebhookPath, newReview(t, valid, "kubeseers", admissionv1.Update, oldInvalid, nil))
	assertResponse(t, updateAllowed, true, http.StatusOK, "")
	updateDenied := send(t, server, admission.KubeseerWebhookPath, newReview(t, invalid, "kubeseers", admissionv1.Update, valid, nil))
	assertResponse(t, updateDenied, false, http.StatusUnprocessableEntity, metav1.StatusReasonInvalid)

	deleteAllowed := send(t, server, admission.KubeseerWebhookPath, newReview(t, valid, "kubeseers", admissionv1.Delete, valid, nil))
	assertResponse(t, deleteAllowed, true, http.StatusOK, "")

	malformedSecret := "registration-malformed-secret"
	malformedPayload := []byte("{\"apiVersion\":\"admission.k8s.io/v1\",\"kind\":\"AdmissionReview\",\"request\":{\"uid\":\"malformed\",\"object\":{\"secret\":\"" + malformedSecret)
	malformedResponse := sendPayload(t, server, admission.KubeseerWebhookPath, malformedPayload)
	assertResponse(t, malformedResponse, false, http.StatusBadRequest, "")
	if strings.Contains(malformedResponse.Response.Result.Message, malformedSecret) || malformedResponse.Response.Result.Details != nil {
		t.Fatalf("malformed response leaked payload or causes: %#v", malformedResponse.Response.Result)
	}

	forbiddenObject := newKubeseer("team-b", "forbidden-pods", "v1", "Pod")
	forbiddenResponse := send(t, server, admission.KubeseerWebhookPath, newReview(t, forbiddenObject, "kubeseers", admissionv1.Create, nil, nil))
	assertResponse(t, forbiddenResponse, false, http.StatusForbidden, metav1.StatusReasonForbidden)
	assertCauses(t, forbiddenResponse, []string{"spec.sources[0].resource"})

	policyObject := newPolicy(v1alpha1.InstallationAccessCeilingName)
	policyResponse := send(t, server, admission.KubeseerAccessPolicyWebhookPath, newReview(t, policyObject, "kubeseeraccesspolicies", admissionv1.Create, nil, nil))
	assertResponse(t, policyResponse, true, http.StatusOK, "")
	policyInvalidResponse := send(t, server, admission.KubeseerAccessPolicyWebhookPath, newReview(t, newPolicy("default"), "kubeseeraccesspolicies", admissionv1.Update, policyObject, nil))
	assertResponse(t, policyInvalidResponse, false, http.StatusUnprocessableEntity, metav1.StatusReasonInvalid)
	assertCauses(t, policyInvalidResponse, []string{"metadata.name"})
	policyDeleteResponse := send(t, server, admission.KubeseerAccessPolicyWebhookPath, newReview(t, policyObject, "kubeseeraccesspolicies", admissionv1.Delete, policyObject, nil))
	assertResponse(t, policyDeleteResponse, true, http.StatusOK, "")

	unavailableServer := newServer(admission.NewValidator(
		admissionResolverFunc(func(context.Context, discovery.SourceDescriptor) (discovery.Resolution, error) {
			return discovery.Resolution{}, discovery.NewResolutionError("unavailable", discovery.ReasonDiscoveryUnavailable, "registration-raw-secret")
		}),
		accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			return validPolicy.DeepCopy(), nil
		}),
	))
	defer unavailableServer.Close()
	unavailableResponse := send(t, unavailableServer, admission.KubeseerWebhookPath, newReview(t, valid, "kubeseers", admissionv1.Create, nil, nil))
	assertResponse(t, unavailableResponse, false, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable)
	if strings.Contains(unavailableResponse.Response.Result.Message, "registration-raw-secret") || unavailableResponse.Response.Result.Details == nil {
		t.Fatalf("unavailable response was not sanitized or lacked a cause: %#v", unavailableResponse.Response.Result)
	}
	assertCauses(t, unavailableResponse, []string{"spec.sources[0]"})

	urlValue := "https://webhook.example.test/base"
	caBundle := []byte("test-ca")
	clientConfig := admissionregistrationv1.WebhookClientConfig{URL: &urlValue, CABundle: caBundle}
	originalClientConfig := clientConfig.DeepCopy()
	configuration := admission.WebhookConfiguration(clientConfig)
	if configuration.Name != "kubeseer-validating-webhook" || len(configuration.Webhooks) != 2 {
		t.Fatalf("webhook configuration identity = %#v, want stable configuration with two webhooks", configuration)
	}
	wantPaths := []string{admission.KubeseerWebhookPath, admission.KubeseerAccessPolicyWebhookPath}
	wantResources := []string{"kubeseers", "kubeseeraccesspolicies"}
	wantScopes := []admissionregistrationv1.ScopeType{admissionregistrationv1.NamespacedScope, admissionregistrationv1.ClusterScope}
	for index, webhook := range configuration.Webhooks {
		if webhook.FailurePolicy == nil || *webhook.FailurePolicy != admissionregistrationv1.Fail || webhook.MatchPolicy == nil || *webhook.MatchPolicy != admissionregistrationv1.Exact || webhook.SideEffects == nil || *webhook.SideEffects != admissionregistrationv1.SideEffectClassNone || webhook.TimeoutSeconds == nil || *webhook.TimeoutSeconds != 10 || !reflect.DeepEqual(webhook.AdmissionReviewVersions, []string{"v1"}) {
			t.Fatalf("webhook[%d] policy fields = %#v, want exact fail-closed contract", index, webhook)
		}
		if len(webhook.Rules) != 1 || !reflect.DeepEqual(webhook.Rules[0].Operations, []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update}) || !reflect.DeepEqual(webhook.Rules[0].APIGroups, []string{v1alpha1.GroupVersion.Group}) || !reflect.DeepEqual(webhook.Rules[0].APIVersions, []string{v1alpha1.GroupVersion.Version}) || !reflect.DeepEqual(webhook.Rules[0].Resources, []string{wantResources[index]}) || webhook.Rules[0].Scope == nil || *webhook.Rules[0].Scope != wantScopes[index] {
			t.Fatalf("webhook[%d] rules = %#v, want exact v1alpha1 main-resource rule", index, webhook.Rules)
		}
		if webhook.ClientConfig.URL == nil || !strings.HasSuffix(*webhook.ClientConfig.URL, wantPaths[index]) || !reflect.DeepEqual(webhook.ClientConfig.CABundle, caBundle) {
			t.Fatalf("webhook[%d] client config = %#v, want injected URL path and CA", index, webhook.ClientConfig)
		}
	}
	configuration.Webhooks[0].ClientConfig.CABundle[0] = 'x'
	configuration.Webhooks[0].Rules[0].Resources[0] = "mutated"
	mutatedURL := "https://mutated.invalid"
	configuration.Webhooks[0].ClientConfig.URL = &mutatedURL
	if !reflect.DeepEqual(clientConfig, *originalClientConfig) {
		t.Fatalf("WebhookConfiguration mutated input client config: got=%#v want=%#v", clientConfig, *originalClientConfig)
	}
	secondConfiguration := admission.WebhookConfiguration(clientConfig)
	if secondConfiguration.Webhooks[0].Rules[0].Resources[0] != "kubeseers" || !strings.HasSuffix(*secondConfiguration.Webhooks[0].ClientConfig.URL, admission.KubeseerWebhookPath) {
		t.Fatal("WebhookConfiguration output was not independent across calls")
	}

	serviceConfig := admission.WebhookConfiguration(admissionregistrationv1.WebhookClientConfig{Service: &admissionregistrationv1.ServiceReference{Namespace: "system", Name: "webhook"}})
	for index, webhook := range serviceConfig.Webhooks {
		if webhook.ClientConfig.Service == nil || webhook.ClientConfig.Service.Path == nil || *webhook.ClientConfig.Service.Path != wantPaths[index] {
			t.Fatalf("service webhook[%d] path = %#v, want %q", index, webhook.ClientConfig.Service, wantPaths[index])
		}
	}
}

func objectNamespace(object runtime.Object) string {
	accessor, ok := object.(metav1.Object)
	if !ok {
		return ""
	}
	return accessor.GetNamespace()
}

func assertSnapshotStabilityScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	policy := basePolicy()
	snapshot := mustSnapshot(t, policy)
	request := requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "immutable-pod", APIVersion: "v1", Kind: "Pod"}, "team-a")
	want := snapshot.Evaluate(request)
	policy.Spec.Namespaces.Include[0] = "changed"
	policy.Spec.Namespaces.SystemNamespaces = []string{"kube-system"}
	policy.Spec.Resources[0].APIGroups[0] = "changed.example.io"
	if got := snapshot.Evaluate(request); !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled snapshot changed after source mutation: got=%#v want=%#v", got, want)
	}

	first := basePolicy()
	second := first.DeepCopy()
	second.Spec.Namespaces.Include = []string{"team-a", "team-c"}
	second.Spec.Namespaces.Exclude = []string{"team-b", "team-d"}
	second.Spec.Resources[0].APIGroups = []string{"widgets.kubeseer.io", "apps", ""}
	second.Spec.Resources[0].Kinds = []string{"Widget", "Deployment", "Pod"}
	firstSnapshot := mustSnapshot(t, first)
	secondSnapshot := mustSnapshot(t, second)
	for _, test := range []struct {
		descriptor discovery.SourceDescriptor
		namespace  string
	}{
		{descriptor: discovery.SourceDescriptor{SourceID: "stable-pod", APIVersion: "v1", Kind: "Pod"}, namespace: "team-a"},
		{descriptor: discovery.SourceDescriptor{SourceID: "stable-deployment", APIVersion: "apps/v1", Kind: "Deployment"}, namespace: "team-a"},
		{descriptor: discovery.SourceDescriptor{SourceID: "stable-widget", APIVersion: "widgets.kubeseer.io/v1", Kind: "Widget"}, namespace: "team-b"},
		{descriptor: discovery.SourceDescriptor{SourceID: "stable-job", APIVersion: "batch/v1", Kind: "Job"}, namespace: "team-a"},
	} {
		request := requestFromResolution(t, ctx, resolver, test.descriptor, test.namespace)
		if firstDecision, secondDecision := firstSnapshot.Evaluate(request), secondSnapshot.Evaluate(request); firstDecision != secondDecision {
			t.Fatalf("equivalent rule order changed decision for %#v: %#v != %#v", request, firstDecision, secondDecision)
		}
	}
}

func assertLoaderScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()
	request := requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "loader-pod", APIVersion: "v1", Kind: "Pod"}, "team-a")
	valid := basePolicy()
	missingError := apierrors.NewNotFound(schema.GroupResource{Group: v1alpha1.GroupVersion.Group, Resource: "kubeseeraccesspolicies"}, v1alpha1.InstallationAccessCeilingName)

	tests := []struct {
		name       string
		source     accesspolicy.PolicySource
		wantReason accesspolicy.PolicyReason
		wantAllow  bool
		wantText   string
	}{
		{
			name: "valid policy produces a logical allow",
			source: accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
				return valid.DeepCopy(), nil
			}),
			wantReason: accesspolicy.ReasonAllowed,
			wantAllow:  true,
			wantText:   "request is allowed by the installation policy",
		},
		{
			name: "missing policy produces deny all",
			source: accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
				return nil, missingError
			}),
			wantReason: accesspolicy.ReasonPolicyMissing,
			wantText:   "policy is missing; observation denied",
		},
		{
			name: "invalid policy produces deny all",
			source: accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
				invalid := valid.DeepCopy()
				invalid.Name = "default"
				return invalid, nil
			}),
			wantReason: accesspolicy.ReasonPolicyInvalid,
			wantText:   "policy PolicyInvalid at metadata.name: must be installation-access-ceiling",
		},
		{
			name: "unavailable policy produces sanitized deny all",
			source: accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
				return nil, errors.New("raw-secret-upstream-payload")
			}),
			wantReason: accesspolicy.ReasonPolicyUnavailable,
			wantText:   "policy is unavailable; observation denied",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := accesspolicy.Load(ctx, test.source).Evaluate(request)
			if decision.Allowed != test.wantAllow || decision.Reason != test.wantReason || decision.Message != test.wantText {
				t.Fatalf("Load().Evaluate() = %#v, want allowed=%t reason=%q message=%q", decision, test.wantAllow, test.wantReason, test.wantText)
			}
			if strings.Contains(decision.Message, "raw-secret-upstream-payload") {
				t.Fatalf("loader diagnostic leaked the upstream error: %#v", decision)
			}
		})
	}

	t.Run("a later failure cannot reuse a prior valid snapshot", func(t *testing.T) {
		calls := 0
		source := accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			calls++
			if calls == 1 {
				return valid.DeepCopy(), nil
			}
			return nil, missingError
		})
		first := accesspolicy.Load(ctx, source).Evaluate(request)
		second := accesspolicy.Load(ctx, source).Evaluate(request)
		if !first.Allowed || first.Reason != accesspolicy.ReasonAllowed {
			t.Fatalf("first valid load was not allowed: %#v", first)
		}
		if second.Allowed || second.Reason != accesspolicy.ReasonPolicyMissing {
			t.Fatalf("second missing load reused valid state: %#v", second)
		}
		if calls != 2 {
			t.Fatalf("source calls = %d, want two independent loads", calls)
		}
	})

	t.Run("evaluation never calls the policy source", func(t *testing.T) {
		calls := 0
		source := accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			calls++
			return valid.DeepCopy(), nil
		})
		snapshot := accesspolicy.Load(ctx, source)
		before := calls
		_ = snapshot.Evaluate(request)
		job := requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "loader-job", APIVersion: "batch/v1", Kind: "Job"}, "team-a")
		_ = snapshot.Evaluate(job)
		if calls != before {
			t.Fatalf("evaluation called source: before=%d after=%d", before, calls)
		}
	})

	t.Run("nil source fails closed", func(t *testing.T) {
		decision := accesspolicy.Load(ctx, nil).Evaluate(request)
		if decision.Allowed || decision.Reason != accesspolicy.ReasonPolicyUnavailable || decision.Message == "" {
			t.Fatalf("nil source decision = %#v", decision)
		}
	})
}

func requestFromResolution(t *testing.T, ctx context.Context, resolver *discovery.Resolver, descriptor discovery.SourceDescriptor, namespace string) accesspolicy.Request {
	t.Helper()
	resolution, err := resolver.Resolve(ctx, descriptor)
	if err != nil {
		t.Fatalf("resolve %q: %v", descriptor.SourceID, err)
	}
	return accesspolicy.Request{
		SourceID:  descriptor.SourceID,
		APIGroup:  resolution.Resource.Group,
		Kind:      descriptor.Kind,
		Namespace: namespace,
		Scope:     resolution.Scope,
	}
}

func mustSnapshot(t *testing.T, policy *v1alpha1.KubeseerAccessPolicy) accesspolicy.Snapshot {
	t.Helper()
	compiled, err := accesspolicy.Compile(policy)
	if err != nil {
		t.Fatalf("compile policy: %v", err)
	}
	return compiled.Snapshot()
}

func mutatePolicy(policy *v1alpha1.KubeseerAccessPolicy, mutate func(*v1alpha1.KubeseerAccessPolicy)) *v1alpha1.KubeseerAccessPolicy {
	copy := policy.DeepCopy()
	mutate(copy)
	return copy
}

func basePolicy() *v1alpha1.KubeseerAccessPolicy {
	return &v1alpha1.KubeseerAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: v1alpha1.InstallationAccessCeilingName},
		Spec: v1alpha1.KubeseerAccessPolicySpec{
			Namespaces: v1alpha1.NamespacePolicy{
				Mode:    v1alpha1.NamespaceModeAllNonSystem,
				Include: []string{"team-a"},
				Exclude: []string{"team-b"},
			},
			Resources: []v1alpha1.ResourceRule{
				{APIGroups: []string{"", "apps", "widgets.kubeseer.io"}, Kinds: []string{"Pod", "Deployment", "Widget"}},
				{APIGroups: []string{""}, Kinds: []string{"Node"}},
			},
		},
	}
}

func allowedDecision() accesspolicy.Decision {
	return accesspolicy.Decision{Allowed: true, Reason: accesspolicy.ReasonAllowed, Message: "request is allowed by the installation policy"}
}

func assertAllowed(t *testing.T, snapshot accesspolicy.Snapshot, request accesspolicy.Request) {
	t.Helper()
	assertDecision(t, snapshot, request, allowedDecision())
}

func assertDeniedReason(t *testing.T, snapshot accesspolicy.Snapshot, request accesspolicy.Request, reason accesspolicy.PolicyReason) {
	t.Helper()
	decision := snapshot.Evaluate(request)
	if decision.Allowed || decision.Reason != reason || decision.Message == "" {
		t.Fatalf("decision = %#v, want denied with reason %q", decision, reason)
	}
}

func assertDecision(t *testing.T, snapshot accesspolicy.Snapshot, request accesspolicy.Request, want accesspolicy.Decision) {
	t.Helper()
	if got := snapshot.Evaluate(request); !reflect.DeepEqual(got, want) {
		t.Fatalf("Evaluate(%#v) = %#v, want %#v", request, got, want)
	}
}

type policyDiscoveryClient struct {
	mu        sync.Mutex
	resources map[string]*metav1.APIResourceList
	calls     map[string]int
}

func newPolicyDiscoveryClient() *policyDiscoveryClient {
	return &policyDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"v1": {GroupVersion: "v1", APIResources: []metav1.APIResource{
				{Name: "pods", Kind: "Pod", Namespaced: true},
				{Name: "nodes", Kind: "Node", Namespaced: false},
			}},
			"apps/v1": {GroupVersion: "apps/v1", APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment", Namespaced: true},
			}},
			"batch/v1": {GroupVersion: "batch/v1", APIResources: []metav1.APIResource{
				{Name: "jobs", Kind: "Job", Namespaced: true},
			}},
			"widgets.kubeseer.io/v1": {GroupVersion: "widgets.kubeseer.io/v1", APIResources: []metav1.APIResource{
				{Name: "widgets", Kind: "Widget", Namespaced: true},
			}},
		},
		calls: make(map[string]int),
	}
}

func (c *policyDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[groupVersion]++
	resources, ok := c.resources[groupVersion]
	if !ok {
		return nil, errors.New("discovery group version unavailable")
	}
	copy := *resources
	copy.APIResources = append([]metav1.APIResource(nil), resources.APIResources...)
	return &copy, nil
}
