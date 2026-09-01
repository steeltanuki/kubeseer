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
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	extensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	admissionregistrationclient "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	CertificateModeCertManager    = "certManager"
	CertificateModeExternalSecret = "externalSecret"
	KubeseerCRDName               = "kubeseers.kubeseer.io"
	AccessPolicyCRDName           = "kubeseeraccesspolicies.kubeseer.io"
	ValidatingWebhookName         = "kubeseer-validating-webhook"
	DefaultWebhookServiceName     = "kubeseer-webhook"
	DefaultDeploymentName         = "kubeseer"
)

// LifecycleOptions is the immutable release identity passed by Helm hooks.
// The hooks deliberately pass expected values rather than discovering or
// mutating release state from the cluster.
type LifecycleOptions struct {
	ReleaseName                           string
	ReleaseNamespace                      string
	DeploymentName                        string
	WebhookServiceName                    string
	CertificateMode                       string
	ExpectedVersion                       string
	ExpectedImage                         string
	ExpectedKubeseerCRDStorageVersion     string
	ExpectedAccessPolicyCRDStorageVersion string
	WebhookPort                           int32
	WebhookCertPath                       string
	WebhookKeyPath                        string
	WebhookCAPath                         string
	WebhookDNSNames                       []string
	Timeout                               time.Duration
}

func (o LifecycleOptions) validate(requireReadiness bool) error {
	if strings.TrimSpace(o.ReleaseName) == "" || strings.TrimSpace(o.ReleaseNamespace) == "" {
		return errors.New("release name and namespace are required")
	}
	if strings.TrimSpace(o.DeploymentName) == "" {
		return errors.New("deployment name is required")
	}
	if strings.TrimSpace(o.WebhookServiceName) == "" {
		return errors.New("webhook Service name is required")
	}
	if o.CertificateMode != CertificateModeCertManager && o.CertificateMode != CertificateModeExternalSecret {
		return fmt.Errorf("unsupported certificate mode %q", o.CertificateMode)
	}
	if strings.TrimSpace(o.ExpectedVersion) == "" {
		return errors.New("expected manager version is required")
	}
	if o.WebhookPort < 1 || o.WebhookPort > 65535 {
		return fmt.Errorf("webhook port must be between 1 and 65535: %d", o.WebhookPort)
	}
	if o.Timeout <= 0 {
		return errors.New("lifecycle timeout must be positive")
	}
	if o.ExpectedKubeseerCRDStorageVersion != "" && o.ExpectedKubeseerCRDStorageVersion != "v1" {
		return fmt.Errorf("unsupported Kubeseer CRD storage version %q", o.ExpectedKubeseerCRDStorageVersion)
	}
	if o.ExpectedAccessPolicyCRDStorageVersion != "" && o.ExpectedAccessPolicyCRDStorageVersion != "v1" {
		return fmt.Errorf("unsupported access-policy CRD storage version %q", o.ExpectedAccessPolicyCRDStorageVersion)
	}
	if requireReadiness {
		if strings.TrimSpace(o.ExpectedImage) == "" {
			return errors.New("expected manager image is required")
		}
		if strings.TrimSpace(o.WebhookCertPath) == "" || strings.TrimSpace(o.WebhookKeyPath) == "" || strings.TrimSpace(o.WebhookCAPath) == "" {
			return errors.New("webhook certificate, key, and CA paths are required")
		}
		if len(o.WebhookDNSNames) == 0 {
			return errors.New("webhook DNS names are required")
		}
	}
	return nil
}

// Preflight performs bounded, read-only checks before a release is changed.
// It verifies prerequisites, singleton ownership, and the non-negotiable CRD
// storage contract without reading Secret payloads or applying anything.
func Preflight(ctx context.Context, restConfig *rest.Config, options LifecycleOptions) error {
	if restConfig == nil {
		return errors.New("REST config is required")
	}
	if err := options.validate(false); err != nil {
		return fmt.Errorf("invalid lifecycle preflight: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create lifecycle discovery client: %w", err)
	}
	if options.CertificateMode == CertificateModeCertManager {
		if _, err := discoveryClient.ServerResourcesForGroupVersion("cert-manager.io/v1"); err != nil {
			return errors.New("cert-manager.io/v1 is unavailable; install cert-manager or choose externalSecret mode")
		}
	}

	crdClient, err := extensionsclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create CRD client: %w", err)
	}
	for _, check := range []struct {
		name            string
		expectedStorage string
	}{
		{name: KubeseerCRDName, expectedStorage: options.ExpectedKubeseerCRDStorageVersion},
		{name: AccessPolicyCRDName, expectedStorage: options.ExpectedAccessPolicyCRDStorageVersion},
	} {
		crd, err := crdClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, check.name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read required CRD %q: %w", check.name, err)
		}
		if err := requireEstablishedCRD(crd); err != nil {
			return err
		}
		if check.expectedStorage != "" {
			storageVersion := crdStorageVersion(crd)
			if storageVersion != check.expectedStorage {
				return fmt.Errorf("CRD %q uses unsupported storage version %q; expected %q", check.name, storageVersion, check.expectedStorage)
			}
		}
	}

	policyScheme, err := NewScheme()
	if err != nil {
		return err
	}
	policyClient, err := client.New(restConfig, client.Options{Scheme: policyScheme})
	if err != nil {
		return fmt.Errorf("create policy preflight client: %w", err)
	}
	var policy v1alpha1.KubeseerAccessPolicy
	if err := policyClient.Get(ctx, types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}, &policy); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("read installation access policy: %w", err)
	} else if err == nil {
		if !ownedByRelease(policy.GetAnnotations(), options.ReleaseName, options.ReleaseNamespace) {
			return errors.New("installation access policy is owned by another release")
		}
	}

	coreClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create lifecycle Kubernetes client: %w", err)
	}
	if deployment, err := coreClient.AppsV1().Deployments(options.ReleaseNamespace).Get(ctx, options.DeploymentName, metav1.GetOptions{}); err == nil {
		if err := requireOwnedObject(deployment, options.ReleaseName, options.ReleaseNamespace); err != nil {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read existing manager Deployment: %w", err)
	}

	admissionClient, err := admissionregistrationclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create admission registration client: %w", err)
	}
	if webhook, err := admissionClient.ValidatingWebhookConfigurations().Get(ctx, ValidatingWebhookName, metav1.GetOptions{}); err == nil {
		if err := requireOwnedObject(webhook, options.ReleaseName, options.ReleaseNamespace); err != nil {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read existing validating webhook: %w", err)
	}
	return nil
}

// VerifyRelease waits for a complete rolling rollout, a versioned image, the
// exact fail-closed webhook registration, and a trusted reachable endpoint.
// All checks are read-only; a failed rollout remains visible in Deployment
// status for the administrator to diagnose.
func VerifyRelease(ctx context.Context, restConfig *rest.Config, options LifecycleOptions) error {
	if restConfig == nil {
		return errors.New("REST config is required")
	}
	if err := options.validate(true); err != nil {
		return fmt.Errorf("invalid release verification: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	checkContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create release verification client: %w", err)
	}
	var lastErr error
	for {
		lastErr = verifyReleaseOnce(checkContext, clientset, options)
		if lastErr == nil {
			return nil
		}
		select {
		case <-checkContext.Done():
			return fmt.Errorf("release verification timed out: %w", lastErr)
		case <-time.After(2 * time.Second):
		}
	}
}

func verifyReleaseOnce(ctx context.Context, clientset kubernetes.Interface, options LifecycleOptions) error {
	deployment, err := clientset.AppsV1().Deployments(options.ReleaseNamespace).Get(ctx, options.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return errors.New("manager Deployment is not available")
	}
	if err := requireOwnedObject(deployment, options.ReleaseName, options.ReleaseNamespace); err != nil {
		return err
	}
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}
	if deployment.Status.ObservedGeneration < deployment.Generation || deployment.Status.UpdatedReplicas < desiredReplicas || deployment.Status.AvailableReplicas < desiredReplicas || deployment.Status.UnavailableReplicas != 0 {
		for _, condition := range deployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentProgressing && condition.Reason == "ProgressDeadlineExceeded" {
				return errors.New("manager Deployment rollout exceeded its progress deadline")
			}
		}
		return errors.New("manager Deployment rollout is not available")
	}
	if options.ExpectedImage != "" {
		foundImage := false
		foundVersion := false
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name == "manager" {
				foundImage = true
				if container.Image != options.ExpectedImage {
					return fmt.Errorf("manager image %q does not match expected release image", container.Image)
				}
				for _, env := range container.Env {
					if env.Name == "KUBESEER_VERSION" && env.Value == options.ExpectedVersion {
						foundVersion = true
					}
				}
			}
		}
		if !foundImage {
			return errors.New("manager container is missing")
		}
		if !foundVersion {
			return errors.New("manager version does not match expected release version")
		}
	}

	service, err := clientset.CoreV1().Services(options.ReleaseNamespace).Get(ctx, options.WebhookServiceName, metav1.GetOptions{})
	if err != nil {
		return errors.New("webhook Service is not available")
	}
	if service.Spec.ClusterIP == "" || service.Spec.ClusterIP == "None" {
		return errors.New("webhook Service is not cluster-local")
	}
	if !hasWebhookPort(service, deployment, options.WebhookPort) {
		return errors.New("webhook Service does not expose the configured TLS port")
	}

	webhook, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, ValidatingWebhookName, metav1.GetOptions{})
	if err != nil {
		return errors.New("validating webhook configuration is not available")
	}
	if err := validateWebhookConfiguration(webhook, options); err != nil {
		return err
	}
	if err := validateCertificateFiles(options.WebhookCertPath, options.WebhookKeyPath, options.WebhookCAPath, options.WebhookDNSNames); err != nil {
		return err
	}
	if err := verifyWebhookReachability(ctx, options); err != nil {
		return err
	}
	return nil
}

func requireEstablishedCRD(crd *apiextensionsv1.CustomResourceDefinition) error {
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
			return nil
		}
		if condition.Type == apiextensionsv1.NamesAccepted && condition.Status == apiextensionsv1.ConditionFalse {
			return fmt.Errorf("CRD %q rejected its name: %s", crd.Name, condition.Reason)
		}
	}
	return fmt.Errorf("CRD %q is not Established", crd.Name)
}

func crdStorageVersion(crd *apiextensionsv1.CustomResourceDefinition) string {
	for _, version := range crd.Spec.Versions {
		if version.Storage {
			return version.Name
		}
	}
	return ""
}

func ownedByRelease(annotations map[string]string, releaseName, releaseNamespace string) bool {
	return annotations[helmReleaseNameAnnotation] == releaseName && annotations[helmReleaseNamespaceAnnotation] == releaseNamespace
}

func requireOwnedObject(object metav1.Object, releaseName, releaseNamespace string) error {
	if !ownedByRelease(object.GetAnnotations(), releaseName, releaseNamespace) {
		return fmt.Errorf("object %q is owned by another release", object.GetName())
	}
	return nil
}

func hasWebhookPort(service *corev1.Service, deployment *appsv1.Deployment, webhookPort int32) bool {
	for _, port := range service.Spec.Ports {
		if port.Port != 443 {
			continue
		}
		if port.TargetPort.IntVal == webhookPort {
			return true
		}
		if port.TargetPort.StrVal == "webhook" {
			for _, container := range deployment.Spec.Template.Spec.Containers {
				for _, containerPort := range container.Ports {
					if containerPort.Name == "webhook" && containerPort.ContainerPort == webhookPort {
						return true
					}
				}
			}
		}
	}
	return false
}

func validateWebhookConfiguration(configuration *admissionregistrationv1.ValidatingWebhookConfiguration, options LifecycleOptions) error {
	if err := requireOwnedObject(configuration, options.ReleaseName, options.ReleaseNamespace); err != nil {
		return err
	}
	if len(configuration.Webhooks) != 2 {
		return errors.New("validating webhook configuration must contain exactly two entries")
	}
	wantedPaths := map[string]bool{
		"/validate-kubeseer-io-v1alpha1-kubeseer":             false,
		"/validate-kubeseer-io-v1alpha1-kubeseeraccesspolicy": false,
	}
	for _, webhook := range configuration.Webhooks {
		if webhook.FailurePolicy == nil || *webhook.FailurePolicy != admissionregistrationv1.Fail {
			return fmt.Errorf("validating webhook %q is not fail-closed", webhook.Name)
		}
		if webhook.ClientConfig.Service == nil || webhook.ClientConfig.Service.Name != options.WebhookServiceName || webhook.ClientConfig.Service.Namespace != options.ReleaseNamespace || webhook.ClientConfig.Service.Port == nil || *webhook.ClientConfig.Service.Port != 443 || webhook.ClientConfig.Service.Path == nil {
			return fmt.Errorf("validating webhook %q has an unexpected Service target", webhook.Name)
		}
		if _, ok := wantedPaths[*webhook.ClientConfig.Service.Path]; !ok {
			return fmt.Errorf("validating webhook %q has an unexpected path", webhook.Name)
		}
		wantedPaths[*webhook.ClientConfig.Service.Path] = true
		if len(webhook.ClientConfig.CABundle) == 0 {
			return fmt.Errorf("validating webhook %q has no CA bundle", webhook.Name)
		}
	}
	for path, present := range wantedPaths {
		if !present {
			return fmt.Errorf("validating webhook path %q is missing", path)
		}
	}
	return nil
}

func verifyWebhookReachability(ctx context.Context, options LifecycleOptions) error {
	roots, err := loadCertificateAuthorityPool(options.WebhookCAPath)
	if err != nil {
		return errors.New("webhook CA bundle is unavailable for trust probe")
	}
	address := net.JoinHostPort(fmt.Sprintf("%s.%s.svc", options.WebhookServiceName, options.ReleaseNamespace), "443")
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	connection, err := (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: options.WebhookServiceName,
	}}).DialContext(ctx, "tcp", address)
	if err != nil {
		return errors.New("webhook endpoint is not reachable or trusted")
	}
	if err := connection.Close(); err != nil {
		return errors.New("webhook trust probe could not close cleanly")
	}
	return nil
}
