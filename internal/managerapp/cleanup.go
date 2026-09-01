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

package managerapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	extensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	admissionregistrationclient "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RemoveOwnedWebhook deletes only the package-owned validating webhook. It is
// the pre-delete boundary that prevents admission from pointing at a removed
// serving endpoint while the rest of the release is being deleted.
func RemoveOwnedWebhook(ctx context.Context, restConfig *rest.Config, releaseName, releaseNamespace string, timeout time.Duration) error {
	if restConfig == nil {
		return errors.New("REST config is required")
	}
	if releaseName == "" || releaseNamespace == "" {
		return errors.New("release name and namespace are required")
	}
	if timeout <= 0 {
		return errors.New("cleanup timeout must be positive")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	registrationClient, err := admissionregistrationclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create cleanup admission client: %w", err)
	}
	webhook, err := registrationClient.ValidatingWebhookConfigurations().Get(cleanupContext, ValidatingWebhookName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read validating webhook: %w", err)
	}
	if !ownedByRelease(webhook.GetAnnotations(), releaseName, releaseNamespace) {
		return errors.New("validating webhook is owned by another release")
	}
	if err := registrationClient.ValidatingWebhookConfigurations().Delete(cleanupContext, ValidatingWebhookName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete validating webhook: %w", err)
	}
	for {
		_, err := registrationClient.ValidatingWebhookConfigurations().Get(cleanupContext, ValidatingWebhookName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("verify validating webhook deletion: %w", err)
		}
		select {
		case <-cleanupContext.Done():
			return errors.New("validating webhook remains after cleanup timeout")
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// RetainedResource is a sanitized post-delete observation of state that Helm
// intentionally leaves in the cluster.
type RetainedResource struct {
	Target  string
	Outcome string
}

// ReportRetainedState reports only the two CRDs and the singleton policy. It
// never reads or prints custom-resource or Secret payloads.
func ReportRetainedState(ctx context.Context, restConfig *rest.Config) ([]RetainedResource, error) {
	if restConfig == nil {
		return nil, errors.New("REST config is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	crdClient, err := extensionsclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create retained-state CRD client: %w", err)
	}
	policyScheme, err := NewScheme()
	if err != nil {
		return nil, err
	}
	policyClient, err := client.New(restConfig, client.Options{Scheme: policyScheme})
	if err != nil {
		return nil, fmt.Errorf("create retained-state policy client: %w", err)
	}
	results := make([]RetainedResource, 0, 3)
	for _, name := range []string{KubeseerCRDName, AccessPolicyCRDName} {
		_, getErr := crdClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		outcome := "retained"
		if apierrors.IsNotFound(getErr) {
			outcome = "already absent"
		} else if getErr != nil {
			return nil, fmt.Errorf("read retained CRD %q: %w", name, getErr)
		}
		results = append(results, RetainedResource{Target: name, Outcome: outcome})
	}
	var policy v1alpha1.KubeseerAccessPolicy
	getErr := policyClient.Get(ctx, types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}, &policy)
	outcome := "retained"
	if apierrors.IsNotFound(getErr) {
		outcome = "already absent"
	} else if getErr != nil {
		return nil, fmt.Errorf("read retained access policy: %w", getErr)
	}
	results = append(results, RetainedResource{Target: v1alpha1.InstallationAccessCeilingName, Outcome: outcome})
	return results, nil
}
