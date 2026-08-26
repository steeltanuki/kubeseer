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
	"sort"
	"strings"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/discovery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
)

// DiscoveryResolver is the consumer-owned discovery boundary required by the
// planner. It deliberately exposes no resource-instance operations.
type DiscoveryResolver interface {
	Resolve(context.Context, discovery.SourceDescriptor) (discovery.Resolution, error)
}

// Planner converts a public source and containing namespace into a pure plan.
type Planner struct {
	resolver DiscoveryResolver
}

// NewPlanner creates a planner backed by the supplied discovery boundary.
func NewPlanner(resolver DiscoveryResolver) *Planner {
	return &Planner{resolver: resolver}
}

// Plan resolves identity and scope, canonicalizes selectors, and derives exact
// namespace targets without reading any resource instances.
func (p *Planner) Plan(ctx context.Context, ownerNamespace string, source v1alpha1.KubeseerSource) (SelectionPlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateSource(source); err != nil {
		return SelectionPlan{}, err
	}
	if p == nil || p.resolver == nil {
		return SelectionPlan{}, NewSelectionError(source.ID, ReasonInvalidSource, "discovery resolver is not configured")
	}

	descriptor := discovery.SourceDescriptor{
		SourceID:   source.ID,
		APIVersion: source.Resource.APIVersion,
		Kind:       source.Resource.Kind,
	}
	resolution, err := p.resolver.Resolve(ctx, descriptor)
	if err != nil {
		return SelectionPlan{}, err
	}
	if !resolution.Scope.Valid() || resolution.Resource.Resource == "" || resolution.Resource.Version == "" {
		return SelectionPlan{}, NewSelectionError(source.ID, ReasonInvalidSource, "discovery returned an incomplete resource resolution")
	}

	labelSelector, fieldSelector, err := canonicalSelectors(source)
	if err != nil {
		return SelectionPlan{}, err
	}
	targets, err := deriveTargets(source.ID, source.Resource.Kind, resolution, ownerNamespace, source.Namespaces)
	if err != nil {
		return SelectionPlan{}, err
	}

	return SelectionPlan{
		sourceID:      source.ID,
		apiVersion:    source.Resource.APIVersion,
		kind:          source.Resource.Kind,
		targets:       targets,
		labelSelector: labelSelector,
		fieldSelector: fieldSelector,
	}, nil
}

func validateSource(source v1alpha1.KubeseerSource) error {
	if source.ID == "" || len(source.ID) > 63 || len(validation.IsDNS1123Label(source.ID)) != 0 {
		return NewSelectionError(source.ID, ReasonInvalidSource, "source ID must be a valid DNS-1123 label")
	}
	if strings.TrimSpace(source.Resource.APIVersion) == "" {
		return NewSelectionError(source.ID, ReasonInvalidSource, "resource apiVersion must not be empty")
	}
	if strings.TrimSpace(source.Resource.Kind) == "" {
		return NewSelectionError(source.ID, ReasonInvalidSource, "resource Kind must not be empty")
	}
	return nil
}

func canonicalSelectors(source v1alpha1.KubeseerSource) (string, string, error) {
	selector := source.Selector
	if selector == nil {
		return labels.Everything().String(), fields.Everything().String(), nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels:      copyStringMap(selector.MatchLabels),
		MatchExpressions: cloneLabelRequirements(selector.MatchExpressions),
	})
	if err != nil {
		return "", "", NewSelectionError(source.ID, ReasonInvalidSelector, "label selector is invalid")
	}
	parsedFields, err := fields.ParseSelector(selector.FieldSelector)
	if err != nil {
		return "", "", NewSelectionError(source.ID, ReasonInvalidSelector, "field selector is invalid")
	}
	if selector.Name != "" {
		nameSelector := fields.OneTermEqualSelector("metadata.name", selector.Name)
		if parsedFields.String() == "" {
			parsedFields = nameSelector
		} else {
			parsedFields = fields.AndSelectors(nameSelector, parsedFields)
		}
	}
	return labelSelector.String(), parsedFields.String(), nil
}

func deriveTargets(sourceID, kind string, resolution discovery.Resolution, ownerNamespace string, selection *v1alpha1.NamespaceSelection) ([]ReadTarget, error) {
	if resolution.Scope == discovery.ScopeCluster {
		if selection != nil {
			return nil, NewSelectionError(sourceID, ReasonInvalidNamespaceScope, "cluster-scoped resources cannot declare namespaces")
		}
		return []ReadTarget{{
			SourceID: sourceID,
			GVR:      resolution.Resource,
			Kind:     kind,
			Scope:    resolution.Scope,
		}}, nil
	}

	if selection != nil && len(selection.Names) == 0 {
		return []ReadTarget{}, nil
	}
	namespaces := []string{ownerNamespace}
	if selection != nil {
		namespaces = append([]string(nil), selection.Names...)
	}
	if len(namespaces) == 0 || ownerNamespace == "" && selection == nil {
		return nil, NewSelectionError(sourceID, ReasonInvalidNamespaceScope, "namespaced resources require a containing namespace")
	}
	seen := make(map[string]struct{}, len(namespaces))
	for _, namespace := range namespaces {
		if len(validation.IsDNS1123Label(namespace)) != 0 {
			return nil, NewSelectionError(sourceID, ReasonInvalidNamespaceScope, "namespace must be a valid DNS-1123 label")
		}
		if _, exists := seen[namespace]; exists {
			return nil, NewSelectionError(sourceID, ReasonInvalidNamespaceScope, "namespace names must be distinct")
		}
		seen[namespace] = struct{}{}
	}
	sort.Strings(namespaces)
	targets := make([]ReadTarget, 0, len(namespaces))
	for _, namespace := range namespaces {
		targets = append(targets, ReadTarget{
			SourceID:  sourceID,
			GVR:       resolution.Resource,
			Kind:      kind,
			Scope:     resolution.Scope,
			Namespace: namespace,
		})
	}
	return targets, nil
}

func copyStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func cloneLabelRequirements(values []metav1.LabelSelectorRequirement) []metav1.LabelSelectorRequirement {
	if values == nil {
		return nil
	}
	copy := make([]metav1.LabelSelectorRequirement, len(values))
	for index := range values {
		values[index].DeepCopyInto(&copy[index])
	}
	return copy
}
