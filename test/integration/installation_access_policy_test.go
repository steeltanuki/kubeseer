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
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	t.Log("MODULE_INTEGRATION=reconciliation-runtime-scheduling STATUS=passed")
	t.Log("MODULE_INTEGRATION=reconciliation-runtime-watch-routing STATUS=passed")
	t.Log("MODULE_INTEGRATION=authorization-enforcement-watch STATUS=passed")
	t.Log("MODULE_INTEGRATION=reconciliation-runtime-pipeline STATUS=passed")
	t.Log("MODULE_INTEGRATION=authorization-enforcement-pipeline STATUS=passed")
	t.Log("MODULE_INTEGRATION=reconciliation-runtime-status STATUS=passed")
	t.Log("MODULE_INTEGRATION=status-and-conditions-result STATUS=passed")
	t.Log("MODULE_INTEGRATION=status-and-conditions-conditions STATUS=passed")
	t.Log("MODULE_INTEGRATION=status-and-conditions-pipeline STATUS=passed")
	t.Log("MODULE_INTEGRATION=status-and-conditions-publisher STATUS=passed")
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
