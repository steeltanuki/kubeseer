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

package accesspolicy

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	defaultSystemNamespaceKubeSystem    = "kube-system"
	defaultSystemNamespaceKubePublic    = "kube-public"
	defaultSystemNamespaceKubeNodeLease = "kube-node-lease"
)

var kindPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

type resourceKey struct {
	apiGroup string
	kind     string
}

// CompiledPolicy is an immutable, in-memory representation of a valid policy.
// Its maps are private and are populated only from defensive copies during
// Compile, so evaluation cannot mutate the source policy or this snapshot.
type CompiledPolicy struct {
	namespaceMode      v1alpha1.NamespaceMode
	includedNamespaces map[string]struct{}
	excludedNamespaces map[string]struct{}
	systemNamespaces   map[string]struct{}
	allowedResources   map[resourceKey]struct{}
	allowClusterScoped bool
	policyIdentity     PolicyIdentity
	hasPolicyIdentity  bool
}

type validationIssue struct {
	field   string
	message string
}

// Compile validates a policy independently of API-server admission and returns
// an immutable lookup representation. Empty namespace and resource lists are
// valid deny-all boundaries; nil systemNamespaces receives the documented
// default while a non-nil empty slice remains empty.
func Compile(policy *v1alpha1.KubeseerAccessPolicy) (*CompiledPolicy, error) {
	issues := validatePolicy(policy)
	if len(issues) != 0 {
		return nil, &PolicyError{
			Reason:  ReasonPolicyInvalid,
			Field:   issues[0].field,
			Message: issues[0].message,
		}
	}

	namespaces := policy.Spec.Namespaces
	systemNamespaces := namespaces.SystemNamespaces
	if systemNamespaces == nil {
		systemNamespaces = []string{
			defaultSystemNamespaceKubeSystem,
			defaultSystemNamespaceKubePublic,
			defaultSystemNamespaceKubeNodeLease,
		}
	}

	compiled := &CompiledPolicy{
		namespaceMode:      namespaces.Mode,
		includedNamespaces: copyStringSet(namespaces.Include),
		excludedNamespaces: copyStringSet(namespaces.Exclude),
		systemNamespaces:   copyStringSet(systemNamespaces),
		allowedResources:   make(map[resourceKey]struct{}),
		allowClusterScoped: policy.Spec.AllowClusterScoped,
		policyIdentity: PolicyIdentity{
			Name:       policy.Name,
			UID:        policy.UID,
			Generation: policy.Generation,
		},
		hasPolicyIdentity: true,
	}
	for _, rule := range policy.Spec.Resources {
		for _, apiGroup := range rule.APIGroups {
			for _, kind := range rule.Kinds {
				compiled.allowedResources[resourceKey{apiGroup: apiGroup, kind: kind}] = struct{}{}
			}
		}
	}

	return compiled, nil
}

func validatePolicy(policy *v1alpha1.KubeseerAccessPolicy) []validationIssue {
	if policy == nil {
		return []validationIssue{{field: "policy", message: "must not be nil"}}
	}

	issues := make([]validationIssue, 0)
	if policy.Name != v1alpha1.InstallationAccessCeilingName {
		issues = append(issues, validationIssue{field: "metadata.name", message: "must be installation-access-ceiling"})
	}

	namespaces := policy.Spec.Namespaces
	switch namespaces.Mode {
	case v1alpha1.NamespaceModeExplicit, v1alpha1.NamespaceModeAll, v1alpha1.NamespaceModeAllNonSystem:
	default:
		issues = append(issues, validationIssue{field: "spec.namespaces.mode", message: "must be Explicit, All, or AllNonSystem"})
	}

	issues = append(issues, validateNamespaceSet("spec.namespaces.include", namespaces.Include)...)
	issues = append(issues, validateNamespaceSet("spec.namespaces.exclude", namespaces.Exclude)...)
	if namespaces.SystemNamespaces != nil {
		issues = append(issues, validateNamespaceSet("spec.namespaces.systemNamespaces", namespaces.SystemNamespaces)...)
	}

	for ruleIndex, rule := range policy.Spec.Resources {
		groupsField := fmt.Sprintf("spec.resources[%d].apiGroups", ruleIndex)
		kindsField := fmt.Sprintf("spec.resources[%d].kinds", ruleIndex)
		if len(rule.APIGroups) == 0 {
			issues = append(issues, validationIssue{field: groupsField, message: "must contain at least one API group"})
		} else {
			issues = append(issues, validateResourceGroups(groupsField, rule.APIGroups)...)
		}
		if len(rule.Kinds) == 0 {
			issues = append(issues, validationIssue{field: kindsField, message: "must contain at least one Kind"})
		} else {
			issues = append(issues, validateKinds(kindsField, rule.Kinds)...)
		}
	}

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].field != issues[j].field {
			return issues[i].field < issues[j].field
		}
		return issues[i].message < issues[j].message
	})
	return issues
}

func validateNamespaceSet(field string, values []string) []validationIssue {
	issues := make([]validationIssue, 0)
	seen := make(map[string]int, len(values))
	for index, value := range values {
		itemField := fmt.Sprintf("%s[%d]", field, index)
		if previous, exists := seen[value]; exists {
			issues = append(issues, validationIssue{field: itemField, message: fmt.Sprintf("duplicates entry at index %d", previous)})
		} else {
			seen[value] = index
		}
		if problems := validation.IsDNS1123Label(value); len(problems) != 0 {
			issues = append(issues, validationIssue{field: itemField, message: "must be a valid DNS-1123 namespace name"})
		}
	}
	return issues
}

func validateResourceGroups(field string, values []string) []validationIssue {
	issues := make([]validationIssue, 0)
	seen := make(map[string]int, len(values))
	for index, value := range values {
		itemField := fmt.Sprintf("%s[%d]", field, index)
		if previous, exists := seen[value]; exists {
			issues = append(issues, validationIssue{field: itemField, message: fmt.Sprintf("duplicates entry at index %d", previous)})
		} else {
			seen[value] = index
		}
		if value != "" && len(validation.IsDNS1123Subdomain(value)) != 0 {
			issues = append(issues, validationIssue{field: itemField, message: "must be empty for the core API group or a valid DNS-1123 subdomain"})
		}
	}
	return issues
}

func validateKinds(field string, values []string) []validationIssue {
	issues := make([]validationIssue, 0)
	seen := make(map[string]int, len(values))
	for index, value := range values {
		itemField := fmt.Sprintf("%s[%d]", field, index)
		if previous, exists := seen[value]; exists {
			issues = append(issues, validationIssue{field: itemField, message: fmt.Sprintf("duplicates entry at index %d", previous)})
		} else {
			seen[value] = index
		}
		if len(value) > 63 || !kindPattern.MatchString(value) {
			issues = append(issues, validationIssue{field: itemField, message: "must be a CamelCase Kubernetes Kind without wildcards"})
		}
	}
	return issues
}

func copyStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
