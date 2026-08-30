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
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/authorization"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	"k8s.io/apimachinery/pkg/types"
)

func assertAuthorizationEnforcementCoreScenarios(t *testing.T, ctx context.Context, resolver *discovery.Resolver) {
	t.Helper()

	policy := basePolicy()
	policy.UID = types.UID("authorization-policy-uid")
	policy.Generation = 7
	snapshot := mustSnapshot(t, policy)
	podRequest := requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "pods", APIVersion: "v1", Kind: "Pod"}, "team-a")
	nodeRequest := requestFromResolution(t, ctx, resolver, discovery.SourceDescriptor{SourceID: "nodes", APIVersion: "v1", Kind: "Node"}, "")
	subject := authorization.Subject{
		Key:         types.NamespacedName{Namespace: "team-a", Name: "authorization-owner"},
		UID:         types.UID("authorization-owner-uid"),
		Generation:  3,
		PolicyEpoch: 11,
	}

	t.Run("ordered batch records exact namespaced and cluster decisions", func(t *testing.T) {
		var observed []authorization.Record
		enforcer := authorization.NewEnforcer(authorization.RecorderFunc(func(_ context.Context, records []authorization.Record) {
			observed = records
		}))

		batch, err := enforcer.EvaluateBatch(ctx, subject, snapshot, []accesspolicy.Request{podRequest, nodeRequest})
		if err != nil {
			t.Fatalf("EvaluateBatch: %v", err)
		}
		outcomes := batch.Outcomes()
		if len(outcomes) != 2 || len(observed) != 2 {
			t.Fatalf("outcome/record lengths = %d/%d, want 2/2", len(outcomes), len(observed))
		}
		if outcomes[0].Decision().Reason != accesspolicy.ReasonAllowed {
			t.Fatalf("namespaced decision = %#v, want allowed", outcomes[0].Decision())
		}
		capability, ok := outcomes[0].Capability()
		if !ok || !capability.Valid() {
			t.Fatalf("allowed outcome capability = %#v, valid=%t", capability, capability.Valid())
		}
		if !capability.IsCurrent(authorization.VerifierFunc(func(candidate authorization.Subject) bool {
			return reflect.DeepEqual(candidate, subject)
		})) {
			t.Fatal("current capability was rejected")
		}
		if capability.IsCurrent(authorization.VerifierFunc(func(authorization.Subject) bool { return false })) {
			t.Fatal("stale capability was accepted")
		}
		if _, ok := outcomes[1].Capability(); ok {
			t.Fatal("cluster-denied outcome unexpectedly carried a capability")
		}
		if outcomes[1].Decision().Reason != accesspolicy.ReasonClusterScopeDenied {
			t.Fatalf("cluster decision = %#v, want cluster-scope denial", outcomes[1].Decision())
		}
		if observed[0].SourceID != "pods" || observed[1].SourceID != "nodes" {
			t.Fatalf("record order = %q, %q; want pods, nodes", observed[0].SourceID, observed[1].SourceID)
		}
		for index, record := range observed {
			if record.Kind != authorization.RecordPolicyDecision || record.Subject != subject || record.PolicyState != authorization.PolicyReady {
				t.Fatalf("record[%d] = %#v, missing ordered policy evidence", index, record)
			}
			if record.PolicyIdentity == nil || *record.PolicyIdentity != (accesspolicy.PolicyIdentity{
				Name:       v1alpha1.InstallationAccessCeilingName,
				UID:        policy.UID,
				Generation: policy.Generation,
			}) {
				t.Fatalf("record[%d] identity = %#v, want policy identity", index, record.PolicyIdentity)
			}
		}

		clusterPolicy := policy.DeepCopy()
		clusterPolicy.Spec.AllowClusterScoped = true
		clusterBatch, err := enforcer.EvaluateBatch(ctx, subject, mustSnapshot(t, clusterPolicy), []accesspolicy.Request{nodeRequest})
		if err != nil {
			t.Fatalf("cluster EvaluateBatch: %v", err)
		}
		clusterOutcome := clusterBatch.Outcomes()[0]
		if !clusterOutcome.Decision().Allowed {
			t.Fatalf("cluster-scoped decision = %#v, want allowed", clusterOutcome.Decision())
		}
		if capability, ok := clusterOutcome.Capability(); !ok || capability.Request() != nodeRequest {
			t.Fatalf("cluster capability = %#v, ok=%t, target mismatch", capability, ok)
		}
	})

	t.Run("subject completeness fails closed before recording", func(t *testing.T) {
		recordCalls := 0
		enforcer := authorization.NewEnforcer(authorization.RecorderFunc(func(context.Context, []authorization.Record) {
			recordCalls++
		}))
		batch, err := enforcer.EvaluateBatch(ctx, authorization.Subject{}, snapshot, []accesspolicy.Request{podRequest})
		if !authorization.HasReason(err, authorization.ReasonInvalidSubject) {
			t.Fatalf("invalid subject error = %v, want InvalidSubject", err)
		}
		if batch.Len() != 0 || recordCalls != 0 {
			t.Fatalf("invalid subject produced batch/record calls = %d/%d, want 0/0", batch.Len(), recordCalls)
		}
	})

	t.Run("terminal states deny without cached capability and preserve only present identity", func(t *testing.T) {
		invalidPolicy := policy.DeepCopy()
		invalidPolicy.Spec.Namespaces.Mode = v1alpha1.NamespaceMode("invalid")
		invalidSnapshot := accesspolicy.Load(ctx, accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			return invalidPolicy.DeepCopy(), nil
		}))
		unavailableSnapshot := accesspolicy.Load(ctx, accesspolicy.PolicySourceFunc(func(context.Context) (*v1alpha1.KubeseerAccessPolicy, error) {
			return nil, errors.New("raw-upstream-payload-sentinel")
		}))
		cases := []struct {
			name         string
			snapshot     accesspolicy.Snapshot
			state        authorization.PolicyState
			outcome      authorization.Outcome
			wantIdentity bool
			wantReason   accesspolicy.PolicyReason
		}{
			{name: "missing", snapshot: accesspolicy.NewDenyAllSnapshot(accesspolicy.ReasonPolicyMissing, "missing"), state: authorization.PolicyMissing, outcome: authorization.OutcomeDenied, wantReason: accesspolicy.ReasonPolicyMissing},
			{name: "invalid", snapshot: invalidSnapshot, state: authorization.PolicyInvalid, outcome: authorization.OutcomeDenied, wantIdentity: true, wantReason: accesspolicy.ReasonPolicyInvalid},
			{name: "unavailable", snapshot: unavailableSnapshot, state: authorization.PolicyUnavailable, outcome: authorization.OutcomeUnavailable, wantReason: accesspolicy.ReasonPolicyUnavailable},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				var observed []authorization.Record
				enforcer := authorization.NewEnforcer(authorization.RecorderFunc(func(_ context.Context, records []authorization.Record) {
					observed = records
				}))
				batch, err := enforcer.EvaluateBatch(ctx, subject, test.snapshot, []accesspolicy.Request{podRequest})
				if err != nil {
					t.Fatalf("EvaluateBatch: %v", err)
				}
				outcome := batch.Outcomes()[0]
				if outcome.Decision().Allowed || outcome.Decision().Reason != test.wantReason {
					t.Fatalf("decision = %#v, want terminal reason %q", outcome.Decision(), test.wantReason)
				}
				if _, ok := outcome.Capability(); ok {
					t.Fatal("terminal outcome unexpectedly carried a capability")
				}
				if len(observed) != 1 || observed[0].PolicyState != test.state || observed[0].Outcome != test.outcome {
					t.Fatalf("terminal record = %#v, want state=%q outcome=%q", observed, test.state, test.outcome)
				}
				if (observed[0].PolicyIdentity != nil) != test.wantIdentity {
					t.Fatalf("terminal identity presence = %t, want %t", observed[0].PolicyIdentity != nil, test.wantIdentity)
				}
				if strings.Contains(fmt.Sprintf("%#v", observed[0]), "raw-upstream-payload-sentinel") {
					t.Fatal("raw upstream error leaked into authorization evidence")
				}
			})
		}
	})

	t.Run("records and capabilities expose defensive values", func(t *testing.T) {
		batch, err := authorization.NewEnforcer(nil).EvaluateBatch(ctx, subject, snapshot, []accesspolicy.Request{podRequest})
		if err != nil {
			t.Fatalf("EvaluateBatch: %v", err)
		}
		records := batch.Records()
		if records[0].PolicyIdentity == nil {
			t.Fatal("allowed record has no policy identity")
		}
		records[0].PolicyIdentity.Name = "mutated"
		if got := batch.Records()[0].PolicyIdentity.Name; got != v1alpha1.InstallationAccessCeilingName {
			t.Fatalf("batch record identity changed through returned copy: %q", got)
		}
		outcomes := batch.Outcomes()
		capability, ok := outcomes[0].Capability()
		if !ok {
			t.Fatal("allowed outcome has no capability")
		}
		capabilityRecord := capability.Record()
		capabilityRecord.PolicyIdentity.Name = "mutated-capability"
		if got := batch.Outcomes()[0].CapabilityPtr().Record().PolicyIdentity.Name; got != v1alpha1.InstallationAccessCeilingName {
			t.Fatalf("capability record changed through returned copy: %q", got)
		}

		capabilityType := reflect.TypeOf(authorization.Capability{})
		for index := 0; index < capabilityType.NumField(); index++ {
			if capabilityType.Field(index).PkgPath == "" {
				t.Fatalf("capability field %q is exported", capabilityType.Field(index).Name)
			}
		}
		recordType := reflect.TypeOf(authorization.Record{})
		for _, forbidden := range []string{"Message", "Selector", "Object", "Field", "Payload", "RawError"} {
			if _, ok := recordType.FieldByName(forbidden); ok {
				t.Fatalf("forbidden evidence field %q exists", forbidden)
			}
		}
	})
}
