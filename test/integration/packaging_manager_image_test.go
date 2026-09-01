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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/admission"
	"github.com/steeltanuki/kubeseer/internal/managerapp"
	"k8s.io/client-go/rest"
)

func assertPackagingManagerImageScenarios(t *testing.T) {
	t.Helper()
	config := managerapp.DefaultConfig()
	config.MetricsBindAddress = "0"
	config.HealthProbeBindAddress = "0"
	application, err := managerapp.Build(&rest.Config{Host: "https://127.0.0.1"}, config)
	if err != nil {
		t.Fatalf("build production manager: %v", err)
	}

	if !application.Manager.GetScheme().Recognizes(v1alpha1.GroupVersion.WithKind("Kubeseer")) {
		t.Fatal("manager scheme does not recognize Kubeseer")
	}
	if !application.Manager.GetScheme().Recognizes(v1alpha1.GroupVersion.WithKind("KubeseerAccessPolicy")) {
		t.Fatal("manager scheme does not recognize KubeseerAccessPolicy")
	}
	if application.Profile.MaxConcurrentReconciles() <= 0 {
		t.Fatal("manager profile has no reconciliation concurrency")
	}

	webhookMux := application.Manager.GetWebhookServer().WebhookMux()
	for _, path := range []string{admission.KubeseerWebhookPath, admission.KubeseerAccessPolicyWebhookPath} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, nil)
		webhookMux.ServeHTTP(recorder, request)
		if recorder.Code == http.StatusNotFound {
			t.Fatalf("webhook path %q is not registered", path)
		}
	}

	for _, value := range []string{"preflight", "policy", "verify", "pre-delete", "post-delete"} {
		if _, err := managerapp.ParsePackageAction(value); err != nil {
			t.Fatalf("parse package action %q: %v", value, err)
		}
	}
	if _, err := managerapp.ParsePackageAction("delete-everything"); err == nil {
		t.Fatal("unsafe package action was accepted")
	}
	if got := application.Manager.GetConfig().Host; got != "https://127.0.0.1" {
		t.Fatalf("manager REST host = %q, want test host", got)
	}

	t.Log("MODULE_INTEGRATION=packaging-manager-image STATUS=passed")
}
