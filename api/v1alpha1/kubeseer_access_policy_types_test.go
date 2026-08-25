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

package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestKubeseerAccessPolicySchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("add Kubeseer API types to scheme: %v", err)
	}

	for _, object := range []runtime.Object{&KubeseerAccessPolicy{}, &KubeseerAccessPolicyList{}} {
		gvks, _, err := scheme.ObjectKinds(object)
		if err != nil {
			t.Fatalf("resolve GVK for %T: %v", object, err)
		}
		if len(gvks) != 1 || gvks[0].GroupVersion() != GroupVersion {
			t.Fatalf("unexpected GVKs for %T: %v", object, gvks)
		}
	}

	for _, kind := range []string{"KubeseerAccessPolicy", "KubeseerAccessPolicyList"} {
		if _, err := scheme.New(GroupVersion.WithKind(kind)); err != nil {
			t.Fatalf("construct %s from scheme: %v", kind, err)
		}
	}

	if InstallationAccessCeilingName != "installation-access-ceiling" {
		t.Fatalf("unexpected active policy name: %q", InstallationAccessCeilingName)
	}
}

func TestKubeseerAccessPolicyJSONRoundTrip(t *testing.T) {
	original := KubeseerAccessPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "KubeseerAccessPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name: InstallationAccessCeilingName,
		},
		Spec: KubeseerAccessPolicySpec{
			Namespaces: NamespacePolicy{
				Mode:             NamespaceModeAllNonSystem,
				Include:          []string{"observability"},
				Exclude:          []string{"restricted"},
				SystemNamespaces: []string{},
			},
			Resources: []ResourceRule{{
				APIGroups: []string{""},
				Kinds:     []string{"Pod", "Service"},
			}},
		},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal access policy: %v", err)
	}

	var decoded KubeseerAccessPolicy
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal access policy: %v", err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("round-trip changed access policy: original=%#v decoded=%#v", original, decoded)
	}
	if decoded.Name != InstallationAccessCeilingName {
		t.Fatalf("round-trip changed active policy name: %q", decoded.Name)
	}
	if len(decoded.Spec.Resources) != 1 || len(decoded.Spec.Resources[0].APIGroups) != 1 || decoded.Spec.Resources[0].APIGroups[0] != "" {
		t.Fatalf("core API group was not preserved as the empty string: %#v", decoded.Spec.Resources)
	}
	if decoded.Spec.AllowClusterScoped {
		t.Fatal("cluster-scoped access is enabled by the zero value")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("inspect access policy JSON: %v", err)
	}
	var specFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["spec"], &specFields); err != nil {
		t.Fatalf("inspect access policy spec JSON: %v", err)
	}
	var namespaceFields map[string]json.RawMessage
	if err := json.Unmarshal(specFields["namespaces"], &namespaceFields); err != nil {
		t.Fatalf("inspect namespace policy JSON: %v", err)
	}
	if string(namespaceFields["systemNamespaces"]) != "[]" {
		t.Fatalf("explicit empty systemNamespaces was not preserved as []: %s", namespaceFields["systemNamespaces"])
	}

	withoutOverride := KubeseerAccessPolicy{Spec: KubeseerAccessPolicySpec{Namespaces: NamespacePolicy{Mode: NamespaceModeAllNonSystem}}}
	withoutOverrideJSON, err := json.Marshal(withoutOverride)
	if err != nil {
		t.Fatalf("marshal access policy without system namespace override: %v", err)
	}
	var withoutOverrideFields map[string]json.RawMessage
	if err := json.Unmarshal(withoutOverrideJSON, &withoutOverrideFields); err != nil {
		t.Fatalf("inspect access policy without system namespace override: %v", err)
	}
	var withoutOverrideSpec map[string]json.RawMessage
	if err := json.Unmarshal(withoutOverrideFields["spec"], &withoutOverrideSpec); err != nil {
		t.Fatalf("inspect spec without system namespace override: %v", err)
	}
	var withoutOverrideNamespaces map[string]json.RawMessage
	if err := json.Unmarshal(withoutOverrideSpec["namespaces"], &withoutOverrideNamespaces); err != nil {
		t.Fatalf("inspect namespaces without system namespace override: %v", err)
	}
	if string(withoutOverrideNamespaces["systemNamespaces"]) != "null" {
		t.Fatalf("omitted systemNamespaces was not preserved as null: %s", withoutOverrideNamespaces["systemNamespaces"])
	}
}

func TestKubeseerAccessPolicyDeepCopyIsolation(t *testing.T) {
	original := &KubeseerAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"environment": "test"},
		},
		Spec: KubeseerAccessPolicySpec{
			Namespaces: NamespacePolicy{
				Mode:             NamespaceModeExplicit,
				Include:          []string{"team-a"},
				Exclude:          []string{"team-b"},
				SystemNamespaces: []string{"kube-system"},
			},
			Resources: []ResourceRule{{
				APIGroups: []string{"", "apps"},
				Kinds:     []string{"Pod", "Deployment"},
			}},
		},
	}

	copy := original.DeepCopy()
	if copy == original {
		t.Fatal("DeepCopy returned the original pointer")
	}

	copy.Labels["environment"] = "production"
	copy.Spec.Namespaces.Include[0] = "team-c"
	copy.Spec.Namespaces.Exclude = append(copy.Spec.Namespaces.Exclude, "team-d")
	copy.Spec.Namespaces.SystemNamespaces[0] = "kube-public"
	copy.Spec.Resources[0].APIGroups[0] = "core"
	copy.Spec.Resources[0].Kinds[0] = "Service"

	if original.Labels["environment"] != "test" {
		t.Fatalf("metadata labels alias original: %#v", original.Labels)
	}
	if original.Spec.Namespaces.Include[0] != "team-a" || original.Spec.Namespaces.Exclude[0] != "team-b" || original.Spec.Namespaces.SystemNamespaces[0] != "kube-system" {
		t.Fatalf("namespace policy aliases original: %#v", original.Spec.Namespaces)
	}
	if original.Spec.Resources[0].APIGroups[0] != "" || original.Spec.Resources[0].Kinds[0] != "Pod" {
		t.Fatalf("resource rules alias original: %#v", original.Spec.Resources)
	}
}
