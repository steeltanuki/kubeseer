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

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	admissionregistrationclient "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
)

const (
	kubeseerGVR = "kubeseers.kubeseer.io"
	policyGVR   = "kubeseeraccesspolicies.kubeseer.io"
	defaultWait = 5 * time.Minute
)

var (
	KubeseerResource = schema.GroupVersionResource{Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseers"}
	PolicyResource   = schema.GroupVersionResource{Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseeraccesspolicies"}
)

// RunMetadata is immutable input supplied by hack/e2e-harness.sh. It contains
// identities and paths only; no kubeconfig, token, or arbitrary environment
// data is copied into this value.
type RunMetadata struct {
	Kubeconfig         string
	ClusterName        string
	Context            string
	KubernetesVersion  string
	CertManagerVersion string
	Namespace          string
	Release            string
	ImageReference     string
	SourceRevision     string
	SourceIdentity     string
	Diagnostics        string
}

// LoadRunMetadata validates the explicit harness boundary.
func LoadRunMetadata() (RunMetadata, error) {
	metadata := RunMetadata{
		Kubeconfig:         os.Getenv("KUBESEER_E2E_KUBECONFIG"),
		ClusterName:        os.Getenv("KUBESEER_E2E_CLUSTER_NAME"),
		Context:            os.Getenv("KUBESEER_E2E_CONTEXT"),
		KubernetesVersion:  os.Getenv("KUBESEER_E2E_KUBERNETES_VERSION"),
		CertManagerVersion: os.Getenv("KUBESEER_E2E_CERT_MANAGER_VERSION"),
		Namespace:          os.Getenv("KUBESEER_E2E_NAMESPACE"),
		Release:            os.Getenv("KUBESEER_E2E_RELEASE"),
		ImageReference:     os.Getenv("KUBESEER_E2E_IMAGE_REF"),
		SourceRevision:     os.Getenv("KUBESEER_E2E_SOURCE_REVISION"),
		SourceIdentity:     os.Getenv("KUBESEER_E2E_SOURCE_IDENTITY"),
		Diagnostics:        os.Getenv("KUBESEER_E2E_DIAGNOSTICS"),
	}
	values := map[string]string{
		"KUBESEER_E2E_KUBECONFIG":         metadata.Kubeconfig,
		"KUBESEER_E2E_CLUSTER_NAME":       metadata.ClusterName,
		"KUBESEER_E2E_CONTEXT":            metadata.Context,
		"KUBESEER_E2E_KUBERNETES_VERSION": metadata.KubernetesVersion,
		"KUBESEER_E2E_NAMESPACE":          metadata.Namespace,
		"KUBESEER_E2E_RELEASE":            metadata.Release,
		"KUBESEER_E2E_IMAGE_REF":          metadata.ImageReference,
		"KUBESEER_E2E_SOURCE_REVISION":    metadata.SourceRevision,
		"KUBESEER_E2E_SOURCE_IDENTITY":    metadata.SourceIdentity,
		"KUBESEER_E2E_DIAGNOSTICS":        metadata.Diagnostics,
	}
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			return RunMetadata{}, fmt.Errorf("%s is required", name)
		}
	}
	if !filepath.IsAbs(metadata.Kubeconfig) || !filepath.IsAbs(metadata.Diagnostics) {
		return RunMetadata{}, errors.New("run-owned kubeconfig and diagnostics must be absolute paths")
	}
	if metadata.KubernetesVersion != "1.35.6" && metadata.KubernetesVersion != "1.36.2" {
		return RunMetadata{}, fmt.Errorf("unsupported Kubernetes version %q", metadata.KubernetesVersion)
	}
	if !strings.HasPrefix(metadata.ClusterName, "kubeseer-e2e-") || metadata.Context != "kind-"+metadata.ClusterName {
		return RunMetadata{}, errors.New("run cluster identity is not a kind-owned name/context pair")
	}
	return metadata, nil
}

// ClusterSession owns all public clients for one run. No in-memory substitute or
// production-domain package is reachable from this type.
type ClusterSession struct {
	Metadata      RunMetadata
	Config        *rest.Config
	Core          kubernetes.Interface
	Dynamic       dynamic.Interface
	APIExtensions apiextensionsclient.Interface
	Discovery     discovery.DiscoveryInterface
	REST          *rest.RESTClient
	Admission     admissionregistrationclient.AdmissionregistrationV1Interface
	HTTP          *http.Client
	Scheme        *runtime.Scheme
	FixtureLabels map[string]string
}

// NewClusterSession constructs clients from only the run-owned kubeconfig and
// verifies that its selected context points at a loopback kind API server.
func NewClusterSession(ctx context.Context, metadata RunMetadata) (*ClusterSession, error) {
	if ctx == nil {
		return nil, errors.New("cluster session requires a context")
	}
	if metadata.Kubeconfig == "" || !filepath.IsAbs(metadata.Kubeconfig) {
		return nil, errors.New("cluster session requires an absolute kubeconfig")
	}
	raw, err := os.ReadFile(metadata.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("read owned kubeconfig: %w", err)
	}
	loaded, err := clientcmd.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("decode owned kubeconfig: %w", err)
	}
	if loaded.CurrentContext != metadata.Context {
		return nil, fmt.Errorf("kubeconfig current context %q does not match run context %q", loaded.CurrentContext, metadata.Context)
	}
	selected, ok := loaded.Contexts[metadata.Context]
	if !ok || selected.Cluster == "" {
		return nil, fmt.Errorf("run context %q is absent from kubeconfig", metadata.Context)
	}
	cluster, ok := loaded.Clusters[selected.Cluster]
	if !ok || cluster.Server == "" {
		return nil, fmt.Errorf("run context %q has no API server", metadata.Context)
	}
	server, err := url.Parse(cluster.Server)
	if err != nil || server.Hostname() == "" || !isLoopback(server.Hostname()) {
		return nil, errors.New("run API server is not a loopback kind endpoint")
	}
	config, err := clientcmd.BuildConfigFromFlags("", metadata.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("build explicit Kubernetes config: %w", err)
	}
	config.Timeout = 30 * time.Second
	// The suite deliberately creates many bounded fixtures (including the
	// resource-limit case). The observation client must not turn a short
	// quiet-window assertion into a client-side token-bucket deadline.
	config.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()
	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create typed Kubernetes client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create dynamic Kubernetes client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}
	admissionClient, err := admissionregistrationclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create admission client: %w", err)
	}
	apiExtensions, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create API extensions client: %w", err)
	}
	restConfig := rest.CopyConfig(config)
	restConfig.GroupVersion = &schema.GroupVersion{Version: "v1"}
	restConfig.APIPath = "/api"
	restConfig.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	restClient, err := rest.RESTClientFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create REST client: %w", err)
	}
	session := &ClusterSession{
		Metadata:      metadata,
		Config:        config,
		Core:          core,
		Dynamic:       dynamicClient,
		APIExtensions: apiExtensions,
		Discovery:     discoveryClient,
		REST:          restClient,
		Admission:     admissionClient,
		HTTP:          &http.Client{Timeout: 10 * time.Second},
		Scheme:        scheme.Scheme,
		FixtureLabels: map[string]string{"kubeseer.io/e2e-cluster": metadata.ClusterName},
	}
	if err := session.verifyServer(ctx); err != nil {
		return nil, err
	}
	return session, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *ClusterSession) verifyServer(ctx context.Context) error {
	version, err := s.Core.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("verify run API server identity: %w", err)
	}
	if version == nil || version.GitVersion == "" {
		return errors.New("run API server returned no version identity")
	}
	return nil
}

// WaitFor executes a bounded public observation predicate. It relists on each
// bounded client call and never uses an unbounded delay or retry.
func (s *ClusterSession) WaitFor(ctx context.Context, scenarioID, awaited string, predicate func(context.Context) (bool, error)) error {
	if ctx == nil {
		return fmt.Errorf("SCENARIO=%s awaited=%s: nil context", scenarioID, awaited)
	}
	if strings.TrimSpace(scenarioID) == "" || strings.TrimSpace(awaited) == "" {
		return errors.New("scenario ID and awaited predicate are required")
	}
	deadline, cancel := context.WithTimeout(ctx, defaultWait)
	defer cancel()
	return wait.PollUntilContextCancel(deadline, 100*time.Millisecond, true, predicate)
}

// GetKubeseer reads only the public status object through the dynamic client.
func (s *ClusterSession) GetKubeseer(ctx context.Context, namespace, name string) (*v1alpha1.Kubeseer, error) {
	object, err := s.Dynamic.Resource(KubeseerResource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(object.Object)
	if err != nil {
		return nil, fmt.Errorf("encode public Kubeseer: %w", err)
	}
	decoded := &v1alpha1.Kubeseer{}
	if err := json.Unmarshal(encoded, decoded); err != nil {
		return nil, fmt.Errorf("decode public Kubeseer: %w", err)
	}
	return decoded, nil
}

// StatusDigest computes a stable digest of a public status snapshot for
// resource-version/no-op assertions without exposing its result body.
func StatusDigest(status v1alpha1.KubeseerStatus) string {
	encoded, _ := json.Marshal(status)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// ProbeHTTP calls an explicitly supplied local manager endpoint. It is used
// only for bounded readiness and metrics assertions; response bodies are not
// retained in diagnostics.
func (s *ClusterSession) ProbeHTTP(ctx context.Context, endpoint, path string) (int, error) {
	if endpoint == "" {
		return 0, errors.New("HTTP endpoint is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+path, nil)
	if err != nil {
		return 0, err
	}
	response, err := s.HTTP.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

// ValidatePackage performs the package/bootstrap public checks owned by
// E2E-001. The harness has already gated readiness, but these checks bind the
// suite to the installed objects rather than a test seam.
func (s *ClusterSession) ValidatePackage(ctx context.Context) error {
	resources, err := s.Discovery.ServerResourcesForGroupVersion("kubeseer.io/v1alpha1")
	if err != nil {
		return fmt.Errorf("discover Kubeseer API: %w", err)
	}
	seenKubeseer, seenPolicy := false, false
	for _, resource := range resources.APIResources {
		switch resource.Name {
		case "kubeseers", kubeseerGVR:
			seenKubeseer = true
		case "kubeseeraccesspolicies", policyGVR:
			seenPolicy = true
		}
	}
	if !seenKubeseer || !seenPolicy {
		return errors.New("installed discovery does not expose both Kubeseer resources")
	}
	if _, err := s.Dynamic.Resource(PolicyResource).Get(ctx, v1alpha1.InstallationAccessCeilingName, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("read canonical installation policy: %w", err)
	}
	deployment, err := s.Core.AppsV1().Deployments(s.Metadata.Namespace).Get(ctx, s.Metadata.Release, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read manager Deployment: %w", err)
	}
	if deployment.Status.AvailableReplicas < 1 {
		return fmt.Errorf("manager Deployment has no available replicas")
	}
	webhook, err := s.Admission.ValidatingWebhookConfigurations().Get(ctx, "kubeseer-validating-webhook", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read validating webhook: %w", err)
	}
	if len(webhook.Webhooks) != 2 {
		return fmt.Errorf("expected two validating webhooks, got %d", len(webhook.Webhooks))
	}
	for _, entry := range webhook.Webhooks {
		if len(entry.ClientConfig.CABundle) == 0 || entry.FailurePolicy == nil || string(*entry.FailurePolicy) != admissionregistrationFail {
			return errors.New("validating webhook is not fail-closed with a CA bundle")
		}
	}
	certificate, err := s.Dynamic.Resource(schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}).Namespace(s.Metadata.Namespace).Get(ctx, s.Metadata.Release+"-webhook-serving", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read serving Certificate: %w", err)
	}
	conditions, found, err := unstructuredConditions(certificate.Object)
	if err != nil || !found {
		return errors.New("serving Certificate has no status conditions")
	}
	if !conditionTrue(conditions, "Ready") {
		return errors.New("serving Certificate is not Ready")
	}
	return nil
}

const admissionregistrationFail = "Fail"

func unstructuredConditions(object map[string]interface{}) ([]metav1.Condition, bool, error) {
	items, found, err := unstructuredNestedSlice(object, "status", "conditions")
	if err != nil || !found {
		return nil, found, err
	}
	conditions := make([]metav1.Condition, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, false, err
		}
		var condition metav1.Condition
		if err := json.Unmarshal(encoded, &condition); err != nil {
			return nil, false, err
		}
		conditions = append(conditions, condition)
	}
	return conditions, true, nil
}

func unstructuredNestedSlice(object map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	current := interface{}(object)
	for _, field := range fields {
		values, ok := current.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		current, ok = values[field]
		if !ok {
			return nil, false, nil
		}
	}
	items, ok := current.([]interface{})
	return items, ok, nil
}

func conditionTrue(conditions []metav1.Condition, conditionType string) bool {
	for _, condition := range conditions {
		if string(condition.Type) == conditionType && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
