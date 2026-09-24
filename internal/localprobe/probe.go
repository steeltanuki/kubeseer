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

// Package localprobe contains the read-only observer used by the persistent
// local environment. It deliberately imports only Kubernetes clients and the
// public Kubeseer API; lifecycle and reconciliation remain in the shell and
// controller packages respectively.
//
// Responsibility: inspect the owned local environment and project bounded,
// sanitized public state, examples, and diagnostics for contributors.
//
// Boundary: the probe performs no lifecycle mutation and never exposes raw
// kubeconfig, credentials, resource bodies, or controller internals.
package localprobe

import (
	"bufio"
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
	"runtime"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	discoveryclient "k8s.io/client-go/kubernetes/typed/discovery/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
)

const (
	ClusterName     = "kubeseer-local"
	ContextName     = "kind-kubeseer-local"
	Namespace       = "kubeseer-system"
	Release         = "kubeseer"
	PolicyName      = v1alpha1.InstallationAccessCeilingName
	maxDiagLogLines = 200
	maxDiagLogBytes = 64 * 1024
)

// Metadata is the allowlisted, versioned ownership record written by the
// shell orchestrator. It is intentionally not a Kubernetes object.
type Metadata struct {
	SchemaVersion     string
	State             string
	ClusterName       string
	Context           string
	KubernetesVersion string
	KindNodeImage     string
	Kubeconfig        string
	Namespace         string
	Release           string
	SourceRevision    string
	SourceIdentity    string
	ImageReference    string
	ImageID           string
}

// Config configures one bounded probe invocation.
type Config struct {
	StateDir    string
	Metadata    string
	Kubeconfig  string
	ClusterName string
	Context     string
	Timeout     time.Duration
	Catalog     string
	Example     string
	Namespace   string
	Destination string
}

// IdentityReport is the stable public identity projection.
type IdentityReport struct {
	ClusterName       string   `json:"clusterName"`
	Context           string   `json:"context"`
	KubernetesVersion string   `json:"kubernetesVersion"`
	KindNodeImage     string   `json:"kindNodeImage"`
	Namespace         string   `json:"namespace"`
	Release           string   `json:"release"`
	SourceRevision    string   `json:"sourceRevision"`
	SourceIdentity    string   `json:"sourceIdentity"`
	ImageReference    string   `json:"imageReference"`
	ImageID           string   `json:"imageID"`
	ServerVersion     string   `json:"serverVersion,omitempty"`
	Nodes             []string `json:"nodes,omitempty"`
}

// LoadMetadata parses metadata as data, not executable shell input.
func LoadMetadata(path, expectedClusterName, expectedContext string) (Metadata, error) {
	var metadata Metadata
	if path == "" || !filepath.IsAbs(path) {
		return metadata, errors.New("metadata path must be absolute")
	}
	if len(validation.IsDNS1123Label(expectedClusterName)) != 0 || expectedContext != "kind-"+expectedClusterName {
		return metadata, errors.New("configured cluster/context identity is invalid")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return metadata, fmt.Errorf("read ownership metadata: %w", err)
	}
	allowed := map[string]bool{
		"schema_version": true, "state": true, "cluster_name": true, "context": true,
		"kubernetes_version": true, "kind_node_image": true, "kubeconfig": true,
		"namespace": true, "release": true, "source_revision": true, "source_identity": true,
		"image_reference": true, "image_id": true,
	}
	seen := map[string]bool{}
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, "=")
		if !ok || !allowed[key] || seen[key] || strings.ContainsAny(value, "\r\n") {
			return metadata, fmt.Errorf("malformed ownership metadata field %q", key)
		}
		seen[key] = true
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return metadata, fmt.Errorf("scan ownership metadata: %w", err)
	}
	for _, key := range []string{"schema_version", "state", "cluster_name", "context", "kubernetes_version", "kind_node_image", "kubeconfig", "namespace", "release", "source_revision", "source_identity", "image_reference", "image_id"} {
		if !seen[key] {
			return metadata, fmt.Errorf("ownership metadata is missing %s", key)
		}
	}
	metadata = Metadata{
		SchemaVersion: values["schema_version"], State: values["state"], ClusterName: values["cluster_name"],
		Context: values["context"], KubernetesVersion: values["kubernetes_version"], KindNodeImage: values["kind_node_image"],
		Kubeconfig: values["kubeconfig"], Namespace: values["namespace"], Release: values["release"],
		SourceRevision: values["source_revision"], SourceIdentity: values["source_identity"], ImageReference: values["image_reference"], ImageID: values["image_id"],
	}
	if metadata.SchemaVersion != "1" || (metadata.State != "creating" && metadata.State != "owned") {
		return Metadata{}, errors.New("unsupported ownership metadata schema or state")
	}
	if metadata.ClusterName != expectedClusterName || metadata.Context != expectedContext || metadata.Namespace != Namespace || metadata.Release != Release {
		return Metadata{}, errors.New("ownership metadata identity does not match expected cluster/context")
	}
	if !filepath.IsAbs(metadata.Kubeconfig) {
		return Metadata{}, errors.New("metadata kubeconfig must be absolute")
	}
	if !validHex(metadata.SourceRevision, 40, 64) || !validDigest(metadata.SourceIdentity, "sha256:", 64) {
		return Metadata{}, errors.New("ownership metadata source identity is malformed")
	}
	if metadata.ImageReference != "" && !validDigest(metadata.ImageReference, "localhost/kubeseer-local:", 32) {
		return Metadata{}, errors.New("ownership metadata image reference is malformed")
	}
	if metadata.ImageID != "" && metadata.ImageReference == "" {
		return Metadata{}, errors.New("ownership metadata image identity is incomplete")
	}
	return metadata, nil
}

func validDigest(value, prefix string, hexLength int) bool {
	return strings.HasPrefix(value, prefix) && validHex(strings.TrimPrefix(value, prefix), hexLength, hexLength)
}

func validHex(value string, minLength, maxLength int) bool {
	if len(value) < minLength || len(value) > maxLength {
		return false
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

type clients struct {
	core             kubernetes.Interface
	dynamic          dynamic.Interface
	apiExt           apiextensionsclient.Interface
	admission        admissionregistrationv1.AdmissionregistrationV1Interface
	webhookEndpoints *EndpointSliceObserver
	config           *rest.Config
	metadata         Metadata
	identity         IdentityReport
}

// EndpointSliceObserver reads the ready backend count for one Service through
// the stable discovery/v1 API. The namespace is fixed when the observer is
// constructed so every invocation remains namespace-scoped and read-only.
type EndpointSliceObserver struct {
	endpointSlices discoveryclient.EndpointSliceInterface
	namespace      string
	serviceName    string
}

// NewEndpointSliceObserver constructs the production webhook backend
// observer. The caller provides the typed discovery client and the exact
// namespace/Service identity; no EndpointSlice name is inferred.
func NewEndpointSliceObserver(client discoveryclient.DiscoveryV1Interface, namespace, serviceName string) (*EndpointSliceObserver, error) {
	if client == nil {
		return nil, errors.New("EndpointSlice discovery client is required")
	}
	if strings.TrimSpace(namespace) == "" || namespace != strings.TrimSpace(namespace) {
		return nil, errors.New("EndpointSlice namespace must be non-empty")
	}
	if strings.TrimSpace(serviceName) == "" || serviceName != strings.TrimSpace(serviceName) {
		return nil, errors.New("EndpointSlice Service name must be non-empty")
	}
	if _, err := endpointSliceServiceSelector(serviceName); err != nil {
		return nil, err
	}
	return &EndpointSliceObserver{
		endpointSlices: client.EndpointSlices(namespace),
		namespace:      namespace,
		serviceName:    serviceName,
	}, nil
}

func endpointSliceServiceSelector(serviceName string) (string, error) {
	requirement, err := labels.NewRequirement(discoveryv1.LabelServiceName, selection.Equals, []string{serviceName})
	if err != nil {
		return "", fmt.Errorf("build EndpointSlice Service selector for %q: %w", serviceName, err)
	}
	return labels.NewSelector().Add(*requirement).String(), nil
}

// ReadyEndpointCount lists every EndpointSlice selected by the Service-name
// label and aggregates endpoint entries across all returned slices. An
// absent ready condition is treated as ready by the discovery/v1 contract;
// only an explicit false condition is excluded.
func (o *EndpointSliceObserver) ReadyEndpointCount(ctx context.Context) (int, error) {
	if o == nil || o.endpointSlices == nil {
		return 0, errors.New("EndpointSlice observer is not configured")
	}
	selector, err := endpointSliceServiceSelector(o.serviceName)
	if err != nil {
		return 0, err
	}
	slices, err := o.endpointSlices.List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, fmt.Errorf("webhook Service %s/%s EndpointSlice list unavailable: %w", o.namespace, o.serviceName, err)
	}
	ready := 0
	for _, endpointSlice := range slices.Items {
		for _, endpoint := range endpointSlice.Endpoints {
			if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
				ready++
			}
		}
	}
	if ready == 0 {
		return 0, fmt.Errorf("webhook Service %s/%s has no ready EndpointSlice endpoints", o.namespace, o.serviceName)
	}
	return ready, nil
}

func newClients(ctx context.Context, cfg Config) (*clients, error) {
	metadata, err := LoadMetadata(cfg.Metadata, cfg.ClusterName, cfg.Context)
	if err != nil {
		return nil, err
	}
	if metadata.Kubeconfig != cfg.Kubeconfig || cfg.Context != metadata.Context {
		return nil, errors.New("explicit kubeconfig/context do not match ownership metadata")
	}
	raw, err := os.ReadFile(cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("read owned kubeconfig: %w", err)
	}
	kubeconfig, err := clientcmd.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("parse owned kubeconfig: %w", err)
	}
	contextConfig, ok := kubeconfig.Contexts[cfg.Context]
	if !ok || contextConfig == nil || contextConfig.Cluster == "" {
		return nil, fmt.Errorf("owned kubeconfig lacks context %s", cfg.Context)
	}
	clusterConfig, ok := kubeconfig.Clusters[contextConfig.Cluster]
	if !ok || clusterConfig == nil || clusterConfig.Server == "" {
		return nil, errors.New("owned kubeconfig context has no API server")
	}
	parsed, err := url.Parse(clusterConfig.Server)
	if err != nil || parsed.Scheme != "https" || !isLoopback(parsed.Hostname()) {
		return nil, errors.New("owned API server must be an HTTPS loopback endpoint")
	}
	loader := clientcmd.NewNonInteractiveClientConfig(*kubeconfig, cfg.Context, &clientcmd.ConfigOverrides{}, nil)
	restConfig, err := loader.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("construct owned client: %w", err)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	restConfig.Timeout = cfg.Timeout
	core, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	apiExt, err := apiextensionsclient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	webhookEndpoints, err := NewEndpointSliceObserver(core.DiscoveryV1(), metadata.Namespace, metadata.Release+"-webhook")
	if err != nil {
		return nil, fmt.Errorf("configure webhook EndpointSlice observer: %w", err)
	}
	return &clients{core: core, dynamic: dynamicClient, apiExt: apiExt, admission: core.AdmissionregistrationV1(), webhookEndpoints: webhookEndpoints, config: restConfig, metadata: metadata}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *clients) checkIdentity(ctx context.Context) (IdentityReport, error) {
	serverVersion, err := c.core.Discovery().ServerVersion()
	if err != nil {
		return IdentityReport{}, fmt.Errorf("Kubernetes API identity unavailable: %w", err)
	}
	nodes, err := c.core.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 16})
	if err != nil {
		return IdentityReport{}, fmt.Errorf("kind node identity unavailable: %w", err)
	}
	if len(nodes.Items) == 0 {
		return IdentityReport{}, errors.New("kind returned no nodes")
	}
	names := make([]string, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		names = append(names, node.Name)
	}
	sort.Strings(names)
	identity := IdentityReport{
		ClusterName: c.metadata.ClusterName, Context: c.metadata.Context, KubernetesVersion: c.metadata.KubernetesVersion,
		KindNodeImage: c.metadata.KindNodeImage, Namespace: c.metadata.Namespace, Release: c.metadata.Release,
		SourceRevision: c.metadata.SourceRevision, SourceIdentity: c.metadata.SourceIdentity, ImageReference: c.metadata.ImageReference,
		ImageID: c.metadata.ImageID, ServerVersion: serverVersion.GitVersion, Nodes: names,
	}
	c.identity = identity
	return identity, nil
}

// Identity performs the read-only ownership and API identity check.
func Identity(ctx context.Context, cfg Config) (IdentityReport, error) {
	c, err := newClients(ctx, cfg)
	if err != nil {
		return IdentityReport{}, err
	}
	return c.checkIdentity(ctx)
}

type readinessReport struct {
	Identity         IdentityReport `json:"identity"`
	CRDs             []string       `json:"crds"`
	Deployment       string         `json:"deployment"`
	Certificate      string         `json:"certificate"`
	WebhookCABundles int            `json:"webhookCABundles"`
	WebhookEndpoints int            `json:"webhookEndpoints"`
	Policy           string         `json:"policy"`
	Readyz           string         `json:"readyz"`
	Metrics          string         `json:"metrics"`
}

func (c *clients) readinessOnce(ctx context.Context, report *readinessReport) error {
	identity, err := c.checkIdentity(ctx)
	if err != nil {
		return err
	}
	report.Identity = identity
	for _, name := range []string{"kubeseers.kubeseer.io", "kubeseeraccesspolicies.kubeseer.io"} {
		crd, getErr := c.apiExt.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("awaited CRD %s: %w", name, getErr)
		}
		if !hasCondition(crd.Status.Conditions, "Established", "True") {
			return fmt.Errorf("awaited CRD condition Established=True for %s", name)
		}
		report.CRDs = append(report.CRDs, name)
	}
	deployment, err := c.core.AppsV1().Deployments(c.metadata.Namespace).Get(ctx, c.metadata.Release, metav1.GetOptions{})
	if err != nil || deployment.Status.AvailableReplicas < 1 {
		return fmt.Errorf("awaited Deployment/%s Available=True", c.metadata.Release)
	}
	report.Deployment = "Available=True"
	certificate, err := c.dynamic.Resource(schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}).Namespace(c.metadata.Namespace).Get(ctx, c.metadata.Release+"-webhook-serving", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("awaited serving Certificate Ready=True: %w", err)
	}
	if !unstructuredCondition(certificate, "Ready", "True") {
		return errors.New("awaited serving Certificate Ready=True")
	}
	report.Certificate = "Ready=True"
	webhook, err := c.admission.ValidatingWebhookConfigurations().Get(ctx, "kubeseer-validating-webhook", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("awaited validating webhook configuration: %w", err)
	}
	for _, item := range webhook.Webhooks {
		if len(strings.TrimSpace(string(item.ClientConfig.CABundle))) == 0 {
			return errors.New("awaited every validating-webhook CA bundle to be non-empty")
		}
		report.WebhookCABundles++
	}
	if report.WebhookCABundles == 0 {
		return errors.New("awaited at least one validating-webhook CA bundle")
	}
	readyEndpoints, err := c.webhookEndpoints.ReadyEndpointCount(ctx)
	if err != nil {
		return fmt.Errorf("awaited webhook Service EndpointSlice readiness: %w", err)
	}
	report.WebhookEndpoints = readyEndpoints
	policy, err := c.dynamic.Resource(schema.GroupVersionResource{Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseeraccesspolicies"}).Get(ctx, PolicyName, metav1.GetOptions{})
	if err != nil || policy.GetName() != PolicyName {
		return errors.New("awaited installation-access-ceiling policy")
	}
	report.Policy = PolicyName
	report.Readyz, report.Metrics = "object-ready", "service-endpoint-ready"
	// The shell lifecycle supplies loopback URLs after bounded port-forward
	// setup. Keeping the URLs optional preserves a read-only probe for callers
	// that can only observe Kubernetes objects (for example, status snapshots).
	if endpoint := os.Getenv("KUBESEER_LOCAL_READYZ_URL"); endpoint != "" {
		if err := HTTPReady(ctx, endpoint, "ok"); err != nil {
			return fmt.Errorf("awaited manager /readyz: %w", err)
		}
		report.Readyz = "HTTP 200"
	}
	if endpoint := os.Getenv("KUBESEER_LOCAL_METRICS_URL"); endpoint != "" {
		if err := HTTPReady(ctx, endpoint, "# HELP"); err != nil {
			return fmt.Errorf("awaited manager /metrics: %w", err)
		}
		report.Metrics = "HTTP 200"
	}
	return nil
}

// Readiness waits for all package predicates with one bounded deadline.
func Readiness(ctx context.Context, cfg Config) (map[string]any, error) {
	c, err := newClients(ctx, cfg)
	if err != nil {
		return nil, err
	}
	deadline := cfg.Timeout
	if deadline <= 0 {
		deadline = 2 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	report := &readinessReport{}
	var lastErr error
	err = wait.PollUntilContextTimeout(waitCtx, 500*time.Millisecond, deadline, true, func(pollCtx context.Context) (bool, error) {
		attempt := &readinessReport{}
		attemptErr := c.readinessOnce(pollCtx, attempt)
		if attemptErr != nil {
			lastErr = attemptErr
			return false, nil
		}
		*report = *attempt
		return true, nil
	})
	if err != nil {
		if lastErr == nil {
			lastErr = err
		}
		return nil, fmt.Errorf("readiness timeout; awaited package predicate: %w", lastErr)
	}
	return map[string]any{"phase": "readiness", "status": "passed", "report": report}, nil
}

// Status returns a bounded projection and does not serialize resource bodies.
func Status(ctx context.Context, cfg Config) (map[string]any, error) {
	c, err := newClients(ctx, cfg)
	if err != nil {
		return nil, err
	}
	identity, err := c.checkIdentity(ctx)
	if err != nil {
		return nil, err
	}
	packageStatus := map[string]any{"release": c.metadata.Release, "namespace": c.metadata.Namespace, "crds": map[string]string{}}
	crdStatus := packageStatus["crds"].(map[string]string)
	for _, name := range []string{"kubeseers.kubeseer.io", "kubeseeraccesspolicies.kubeseer.io"} {
		crd, getErr := c.apiExt.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			crdStatus[name] = "unavailable"
			continue
		}
		if hasCondition(crd.Status.Conditions, "Established", "True") {
			crdStatus[name] = "Established=True"
		} else {
			crdStatus[name] = "Established=False"
		}
	}
	managerStatus := map[string]any{"status": "unavailable"}
	if deployment, getErr := c.core.AppsV1().Deployments(c.metadata.Namespace).Get(ctx, c.metadata.Release, metav1.GetOptions{}); getErr == nil {
		managerStatus["status"] = "available"
		managerStatus["availableReplicas"] = deployment.Status.AvailableReplicas
	} else {
		managerStatus["reason"] = stableError(getErr)
	}
	policyStatus := map[string]any{"name": PolicyName, "status": "unavailable"}
	if policy, getErr := c.dynamic.Resource(schema.GroupVersionResource{Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseeraccesspolicies"}).Get(ctx, PolicyName, metav1.GetOptions{}); getErr == nil && policy.GetName() == PolicyName {
		policyStatus["status"] = "present"
	} else if getErr != nil {
		policyStatus["reason"] = stableError(getErr)
	}
	webhookStatus := map[string]any{"status": "unavailable", "endpoints": 0}
	if endpoints, endpointErr := c.webhookEndpoints.ReadyEndpointCount(ctx); endpointErr == nil {
		webhookStatus["status"] = "ready"
		webhookStatus["endpoints"] = endpoints
	} else {
		webhookStatus["reason"] = stableError(endpointErr)
	}
	exampleStatus := map[string]any{"catalog": cfg.Catalog, "observed": []string{}}
	if names, catalogErr := loadCatalog(cfg.Catalog); catalogErr == nil {
		observed := []string{}
		for _, name := range names {
			namespace := exampleNamespace(name)
			list, listErr := c.dynamic.Resource(schema.GroupVersionResource{Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseers"}).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: "kubeseer.io/example=" + name, Limit: 1})
			if listErr == nil && len(list.Items) > 0 {
				observed = append(observed, name)
			}
		}
		exampleStatus["catalog"] = names
		exampleStatus["observed"] = observed
	}
	result := map[string]any{"cluster": identity, "package": packageStatus, "manager": managerStatus, "webhook": webhookStatus, "policy": policyStatus, "examples": exampleStatus}
	return result, nil
}

var catalogNames = []string{"builtin-resource", "typed-extraction", "value-operator", "cross-namespace-aggregation", "custom-resource", "authorization-denial", "partial-degradation"}

func loadCatalog(path string) ([]string, error) {
	if path == "" {
		path = filepath.Join("examples", "catalog.txt")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		if seen[line] {
			return nil, fmt.Errorf("duplicate example %s", line)
		}
		seen[line] = true
		names = append(names, line)
	}
	if len(names) != len(catalogNames) {
		return nil, fmt.Errorf("catalog must contain exactly %d examples", len(catalogNames))
	}
	for i, expected := range catalogNames {
		if names[i] != expected {
			return nil, fmt.Errorf("catalog entry %d is %s, expected %s", i+1, names[i], expected)
		}
	}
	return names, nil
}

// Verify asserts public Kubeseer status for every registered example.
func Verify(ctx context.Context, cfg Config) error {
	c, err := newClients(ctx, cfg)
	if err != nil {
		return err
	}
	if _, err := c.checkIdentity(ctx); err != nil {
		return err
	}
	names, err := loadCatalog(cfg.Catalog)
	if err != nil {
		return err
	}
	if cfg.Example != "" {
		found := false
		for _, name := range names {
			if name == cfg.Example {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown example %s", cfg.Example)
		}
		names = []string{cfg.Example}
	}
	for _, name := range names {
		namespace := cfg.Namespace
		if namespace == "" {
			namespace = exampleNamespace(name)
		}
		pollErr := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, verificationTimeout(ctx), true, func(pollCtx context.Context) (bool, error) {
			list, listErr := c.dynamic.Resource(schema.GroupVersionResource{Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseers"}).Namespace(namespace).List(pollCtx, metav1.ListOptions{LabelSelector: "kubeseer.io/example=" + name, Limit: 16})
			if listErr != nil || len(list.Items) == 0 {
				return false, nil
			}
			for _, object := range list.Items {
				if !publicExampleOutcome(name, object) {
					return false, nil
				}
			}
			return true, nil
		})
		if pollErr != nil {
			return fmt.Errorf("example %s: awaited public outcome before timeout: %w", name, pollErr)
		}
		fmt.Printf("EXAMPLE=%s STATUS=passed\n", name)
	}
	return nil
}

func verificationTimeout(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			return remaining
		}
	}
	return 2 * time.Minute
}

func exampleNamespace(name string) string {
	return map[string]string{"builtin-resource": "kubeseer-example-builtin", "typed-extraction": "kubeseer-example-typed", "value-operator": "kubeseer-example-operator", "cross-namespace-aggregation": "kubeseer-example-aggregation-a", "custom-resource": "kubeseer-example-custom", "authorization-denial": "kubeseer-example-denial", "partial-degradation": "kubeseer-example-degraded"}[name]
}

func publicExampleOutcome(name string, object unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	if len(conditions) == 0 {
		return false
	}
	resultSources, foundSources, _ := unstructured.NestedSlice(object.Object, "status", "result", "sources")
	if !foundSources || len(resultSources) == 0 {
		return false
	}
	var sourceByID = map[string]map[string]any{}
	for _, raw := range resultSources {
		if source, ok := raw.(map[string]any); ok {
			if id, ok, _ := unstructured.NestedString(source, "id"); ok {
				sourceByID[id] = source
			}
		}
	}
	if len(sourceByID) == 0 {
		return false
	}
	// A denied source is intentionally a zero-match result. Verify its public
	// authorization condition and sanitized source error before applying the
	// positive-result checks used by the other examples.
	if name == "authorization-denial" {
		authorizedDenied := false
		for _, raw := range conditions {
			condition, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			typeName, _, _ := unstructured.NestedString(condition, "type")
			status, _, _ := unstructured.NestedString(condition, "status")
			reason, _, _ := unstructured.NestedString(condition, "reason")
			if typeName == "Authorized" && status == "False" && reason == "AuthorizationDenied" {
				authorizedDenied = true
				break
			}
		}
		denied, exists := sourceByID["outside-policy"]
		if !exists {
			return false
		}
		state, stateOK, _ := unstructured.NestedString(denied, "state")
		errorReason, reasonOK, _ := unstructured.NestedString(denied, "error", "reason")
		return authorizedDenied && stateOK && state == "error" && reasonOK && errorReason == "AuthorizationDenied"
	}
	matched, foundMatched, _ := unstructured.NestedInt64(object.Object, "status", "summary", "matchedResources")
	if !foundMatched || matched < 1 {
		return false
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typeName, _, _ := unstructured.NestedString(condition, "type")
		status, _, _ := unstructured.NestedString(condition, "status")
		if name == "partial-degradation" && typeName == "Degraded" && status == "True" {
			successful, ok := sourceByID["successful-deployment"]
			state, stateOK, _ := unstructured.NestedString(successful, "state")
			return ok && stateOK && state == "values"
		}
		if typeName == "Ready" && status == "True" {
			positive, ok := sourceByID[map[string]string{
				"builtin-resource":            "deployment",
				"typed-extraction":            "deployment",
				"value-operator":              "deployment",
				"cross-namespace-aggregation": "deployments",
				"custom-resource":             "widget",
			}[name]]
			if !ok {
				return false
			}
			state, stateOK, _ := unstructured.NestedString(positive, "state")
			if !stateOK || state != "values" {
				return false
			}
			resources, resourcesOK, _ := unstructured.NestedSlice(positive, "resources")
			if !resourcesOK || len(resources) == 0 {
				return false
			}
			if name == "custom-resource" {
				resource, ok := resources[0].(map[string]any)
				kind, _, _ := unstructured.NestedString(resource, "kind")
				if !ok || kind != "Widget" || !resourceHasIntegerField(resource, "size", 7) {
					return false
				}
			}
			if name == "typed-extraction" && !resourceHasTimestampField(resources[0], "createdAt") {
				return false
			}
			if name == "builtin-resource" && !resourceHasIntegerField(resources[0], "replicas", 1) {
				return false
			}
			if name == "value-operator" && !resourceHasIntegerField(resources[0], "replicas", 3) {
				return false
			}
			if name == "cross-namespace-aggregation" {
				if len(resources) != 2 {
					return false
				}
				aggregates, aggregateOK, _ := unstructured.NestedSlice(positive, "aggregates")
				if !aggregateOK || len(aggregates) == 0 {
					return false
				}
				aggregateMap, aggregateOK := aggregates[0].(map[string]any)
				aggregateState, stateOK, _ := unstructured.NestedString(aggregateMap, "state")
				if !aggregateOK || !stateOK || aggregateState != "values" || !aggregateHasIntegerValue(aggregateMap, 3) {
					return false
				}
			}
			return true
		}
	}
	return false
}

func resourceHasIntegerField(raw any, wantedName string, wantedValue int64) bool {
	resource, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	fields, ok, _ := unstructured.NestedSlice(resource, "fields")
	if !ok {
		return false
	}
	for _, rawField := range fields {
		field, ok := rawField.(map[string]any)
		if !ok {
			continue
		}
		name, nameOK, _ := unstructured.NestedString(field, "name")
		typeName, typeOK, _ := unstructured.NestedString(field, "type")
		matches, matchesOK, _ := unstructured.NestedSlice(field, "matches")
		if !nameOK || !typeOK || name != wantedName || typeName != "integer" || !matchesOK || len(matches) == 0 {
			continue
		}
		match, matchOK := matches[0].(map[string]any)
		if !matchOK {
			continue
		}
		value, valueOK, _ := unstructured.NestedInt64(match, "integerValue")
		state, stateOK, _ := unstructured.NestedString(match, "state")
		if valueOK && stateOK && state == "value" && value == wantedValue {
			return true
		}
	}
	return false
}

func resourceHasTimestampField(raw any, wantedName string) bool {
	resource, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	fields, ok, _ := unstructured.NestedSlice(resource, "fields")
	if !ok {
		return false
	}
	for _, rawField := range fields {
		field, ok := rawField.(map[string]any)
		if !ok {
			continue
		}
		name, nameOK, _ := unstructured.NestedString(field, "name")
		typeName, typeOK, _ := unstructured.NestedString(field, "type")
		matches, matchesOK, _ := unstructured.NestedSlice(field, "matches")
		if !nameOK || !typeOK || name != wantedName || typeName != "timestamp" || !matchesOK || len(matches) == 0 {
			continue
		}
		match, matchOK := matches[0].(map[string]any)
		if !matchOK {
			continue
		}
		value, valueOK, _ := unstructured.NestedString(match, "timestampValue")
		state, stateOK, _ := unstructured.NestedString(match, "state")
		if !valueOK || !stateOK || state != "value" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return true
		}
	}
	return false
}

func aggregateHasIntegerValue(aggregate map[string]any, wantedValue int64) bool {
	groups, ok, _ := unstructured.NestedSlice(aggregate, "groups")
	if !ok || len(groups) == 0 {
		return false
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		return false
	}
	matches, ok, _ := unstructured.NestedSlice(group, "value", "matches")
	if !ok || len(matches) == 0 {
		return false
	}
	match, ok := matches[0].(map[string]any)
	if !ok {
		return false
	}
	value, valueOK, _ := unstructured.NestedInt64(match, "value", "integerValue")
	state, stateOK, _ := unstructured.NestedString(match, "value", "state")
	return valueOK && stateOK && state == "value" && value == wantedValue
}

func hasCondition(conditions []apiextensionsv1.CustomResourceDefinitionCondition, wantedType, wantedStatus string) bool {
	for _, condition := range conditions {
		if string(condition.Type) == wantedType && string(condition.Status) == wantedStatus {
			return true
		}
	}
	return false
}

func unstructuredCondition(object *unstructured.Unstructured, wantedType, wantedStatus string) bool {
	conditions, found, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	if !found {
		return false
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		t, _, _ := unstructured.NestedString(condition, "type")
		s, _, _ := unstructured.NestedString(condition, "status")
		if t == wantedType && s == wantedStatus {
			return true
		}
	}
	return false
}

func stableError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "timeout"):
		return "timeout"
	case strings.Contains(text, "forbidden"):
		return "forbidden"
	case strings.Contains(text, "connection refused"):
		return "connection-refused"
	default:
		return "unavailable"
	}
}

// readinessProjection observes package objects without copying their bodies.
// Every field is a bounded identity, count, or finite readiness state.
func (c *clients) readinessProjection(ctx context.Context) map[string]any {
	result := map[string]any{
		"crds":        map[string]string{},
		"deployment":  "unavailable",
		"certificate": "unavailable",
		"webhook":     map[string]any{"caBundles": 0, "endpoints": 0},
	}
	crds := result["crds"].(map[string]string)
	for _, name := range []string{"kubeseers.kubeseer.io", "kubeseeraccesspolicies.kubeseer.io"} {
		crd, err := c.apiExt.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			crds[name] = "unavailable"
		} else if hasCondition(crd.Status.Conditions, "Established", "True") {
			crds[name] = "Established=True"
		} else {
			crds[name] = "Established=False"
		}
	}
	if deployment, err := c.core.AppsV1().Deployments(c.metadata.Namespace).Get(ctx, c.metadata.Release, metav1.GetOptions{}); err == nil {
		result["deployment"] = map[string]any{"name": deployment.Name, "availableReplicas": deployment.Status.AvailableReplicas}
	}
	if certificate, err := c.dynamic.Resource(schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}).Namespace(c.metadata.Namespace).Get(ctx, c.metadata.Release+"-webhook-serving", metav1.GetOptions{}); err == nil {
		state := "Ready=False"
		if unstructuredCondition(certificate, "Ready", "True") {
			state = "Ready=True"
		}
		result["certificate"] = state
	}
	webhookStatus := result["webhook"].(map[string]any)
	if webhook, err := c.admission.ValidatingWebhookConfigurations().Get(ctx, "kubeseer-validating-webhook", metav1.GetOptions{}); err == nil {
		caBundles := 0
		for _, item := range webhook.Webhooks {
			if len(strings.TrimSpace(string(item.ClientConfig.CABundle))) > 0 {
				caBundles++
			}
		}
		webhookStatus["caBundles"] = caBundles
	}
	if endpoints, err := c.webhookEndpoints.ReadyEndpointCount(ctx); err == nil {
		webhookStatus["endpoints"] = endpoints
	}
	return result
}

// statusProjection serializes only public condition, summary, and result-hash
// fields. It intentionally omits result sources, selectors, and extracted
// values even though they are present on the Kubernetes object.
func (c *clients) statusProjection(ctx context.Context) []map[string]any {
	list, err := c.dynamic.Resource(schema.GroupVersionResource{Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseers"}).Namespace(c.metadata.Namespace).List(ctx, metav1.ListOptions{Limit: 64})
	if err != nil {
		return []map[string]any{{"observation": "unavailable", "error": stableError(err)}}
	}
	projection := make([]map[string]any, 0, len(list.Items))
	for _, item := range list.Items {
		entry := map[string]any{"name": item.GetName(), "namespace": item.GetNamespace(), "generation": item.GetGeneration(), "conditions": []map[string]string{}}
		conditions, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		conditionProjection := entry["conditions"].([]map[string]string)
		for _, raw := range conditions {
			condition, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			typeName, _, _ := unstructured.NestedString(condition, "type")
			status, _, _ := unstructured.NestedString(condition, "status")
			reason, _, _ := unstructured.NestedString(condition, "reason")
			if typeName != "" && status != "" && reason != "" {
				conditionProjection = append(conditionProjection, map[string]string{"type": typeName, "status": status, "reason": reason})
			}
		}
		entry["conditions"] = conditionProjection
		if _, found, _ := unstructured.NestedMap(item.Object, "status", "summary"); found {
			compact := map[string]int64{}
			for _, key := range []string{"successfulSources", "failedSources", "matchedResources"} {
				if value, ok, _ := unstructured.NestedInt64(item.Object, "status", "summary", key); ok {
					compact[key] = value
				}
			}
			entry["summary"] = compact
		}
		if hash, found, _ := unstructured.NestedString(item.Object, "status", "resultHash"); found {
			entry["resultHash"] = hash
		}
		projection = append(projection, entry)
	}
	return projection
}

func writePrivate(path string, value any) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".diagnostic-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(value); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

// Diagnostics writes only allowlisted projections to a private external
// destination. API failures produce a stable unavailable section instead of
// exposing raw downstream responses.
func Diagnostics(ctx context.Context, cfg Config) (string, error) {
	destination := cfg.Destination
	if destination == "" {
		return "", errors.New("diagnostic destination is required")
	}
	if !filepath.IsAbs(destination) || destination == "/" || filepath.Clean(destination) == filepath.Clean(cfg.StateDir) {
		return "", errors.New("diagnostic destination must be a distinct absolute directory")
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return "", fmt.Errorf("create diagnostic destination: %w", err)
	}
	if err := os.Chmod(destination, 0700); err != nil {
		return "", err
	}
	manifest := map[string]any{"schemaVersion": 1, "destination": destination, "completedSections": []string{}, "failedSections": []string{}}
	completed := []string{}
	failed := []string{}
	add := func(name string, value any) error {
		if err := writePrivate(filepath.Join(destination, name+".json"), value); err != nil {
			failed = append(failed, name)
			return err
		}
		completed = append(completed, name)
		return nil
	}
	metadata, metadataErr := LoadMetadata(cfg.Metadata, cfg.ClusterName, cfg.Context)
	if metadataErr != nil {
		return "", metadataErr
	}
	versions := map[string]string{
		"go": runtime.Version(), "kubernetes": metadata.KubernetesVersion, "kindNodeImage": metadata.KindNodeImage,
		"certManager": "pinned", "schemaVersion": metadata.SchemaVersion,
		"kind": os.Getenv("KUBESEER_LOCAL_KIND_VERSION"), "podman": os.Getenv("KUBESEER_LOCAL_PODMAN_VERSION"),
		"kubectl": os.Getenv("KUBESEER_LOCAL_KUBECTL_VERSION"), "helm": os.Getenv("KUBESEER_LOCAL_HELM_VERSION"), "curl": os.Getenv("KUBESEER_LOCAL_CURL_VERSION"),
	}
	for key, value := range versions {
		if value == "" {
			versions[key] = "unknown"
		}
	}
	if err := add("versions", versions); err != nil {
		return "", err
	}
	ownership := map[string]string{"clusterName": metadata.ClusterName, "context": metadata.Context, "namespace": metadata.Namespace, "release": metadata.Release, "sourceRevision": metadata.SourceRevision, "sourceIdentity": metadata.SourceIdentity, "imageReference": metadata.ImageReference, "imageID": metadata.ImageID}
	if err := add("ownership", ownership); err != nil {
		return "", err
	}
	c, clientErr := newClients(ctx, cfg)
	if clientErr != nil {
		_ = add("unavailable", map[string]string{"observation": "unavailable", "error": stableError(clientErr)})
	} else if identity, identityErr := c.checkIdentity(ctx); identityErr != nil {
		_ = add("unavailable", map[string]string{"observation": "unavailable", "error": stableError(identityErr)})
	} else {
		_ = add("cluster", identity)
		_ = add("readiness", c.readinessProjection(ctx))
		_ = add("package", map[string]string{"namespace": metadata.Namespace, "release": metadata.Release})
		_ = add("policy", map[string]string{"name": PolicyName})
		_ = add("status", c.statusProjection(ctx))
		_ = add("metrics", map[string]any{"families": []string{
			"kubeseer_reconciliations_total",
			"kubeseer_reconciliation_duration_seconds",
			"kubeseer_resources_read_total",
			"kubeseer_source_failures_total",
			"kubeseer_results_produced_total",
			"kubeseer_status_updates_total",
			"kubeseer_authorization_decisions_total",
			"kubeseer_jsonpath_failures_total",
			"kubeseer_source_watch_restarts_total",
		}})
		_ = add("events", c.eventsProjection(ctx))
		_ = add("logs", c.logsProjection(ctx))
	}
	manifest["completedSections"] = completed
	manifest["failedSections"] = failed
	if err := writePrivate(filepath.Join(destination, "manifest.json"), manifest); err != nil {
		return "", err
	}
	return destination, nil
}

func (c *clients) eventsProjection(ctx context.Context) []map[string]string {
	events, err := c.core.CoreV1().Events("").List(ctx, metav1.ListOptions{Limit: 200})
	if err != nil {
		return []map[string]string{{"type": "unavailable", "reason": stableError(err)}}
	}
	result := make([]map[string]string, 0, len(events.Items))
	for _, event := range events.Items {
		item := map[string]string{"namespace": event.Namespace, "name": event.Name, "type": event.Type, "action": event.Action, "regardingKind": event.InvolvedObject.Kind, "regardingName": event.InvolvedObject.Name}
		if diagnosticFieldAllowed("reason", event.Reason) {
			item["reason"] = event.Reason
		}
		result = append(result, item)
	}
	return result
}

func (c *clients) logsProjection(ctx context.Context) []map[string]string {
	pods, err := c.core.CoreV1().Pods(c.metadata.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=kubeseer"})
	if err != nil {
		return []map[string]string{{"level": "warning", "reason": stableError(err)}}
	}
	result := []map[string]string{}
	bytesRead := 0
	for _, pod := range pods.Items {
		if len(result) >= maxDiagLogLines || bytesRead >= maxDiagLogBytes {
			break
		}
		tail := int64(maxDiagLogLines - len(result))
		limitBytes := maxDiagLogBytes - int64(bytesRead)
		stream, streamErr := c.core.CoreV1().Pods(c.metadata.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "manager", TailLines: &tail, LimitBytes: &limitBytes}).Stream(ctx)
		if streamErr != nil {
			continue
		}
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			line := scanner.Bytes()
			bytesRead += len(line)
			if bytesRead > maxDiagLogBytes {
				break
			}
			var fields map[string]any
			if json.Unmarshal(line, &fields) != nil {
				continue
			}
			item := map[string]string{"pod": pod.Name}
			for _, key := range []string{"timestamp", "level", "eventCode", "reason", "outcome", "stage"} {
				if value, ok := fields[key].(string); ok && diagnosticFieldAllowed(key, value) {
					item[key] = value
				}
			}
			if len(item) > 1 {
				result = append(result, item)
			}
		}
		_ = stream.Close()
	}
	return result
}

// diagnosticFieldAllowed keeps log values inside the closed observability
// vocabulary. Unknown user-controlled labels are dropped rather than echoed.
func diagnosticFieldAllowed(field, value string) bool {
	allowed := map[string]map[string]bool{
		"level":     {"info": true, "warning": true, "error": true},
		"outcome":   {"started": true, "completed": true, "skipped": true, "retry": true, "failed": true, "degraded": true, "succeeded": true, "written": true, "conflicted": true, "unavailable": true, "allowed": true, "denied": true, "forbidden": true},
		"stage":     {"load": true, "validate": true, "authorize": true, "plan": true, "read": true, "extract": true, "aggregate": true, "compose": true, "publish": true},
		"eventCode": {"ReconciliationStarted": true, "ReconciliationCompleted": true, "ReconciliationFailed": true, "SourceFailed": true, "AuthorizationDecision": true, "AuthorizationReadForbidden": true, "JSONPathFailed": true},
	}
	if field == "timestamp" {
		return len(value) <= 64
	}
	if field == "reason" {
		return map[string]bool{
			"Allowed": true, "AuthorizationDenied": true, "AuthorizationSucceeded": true,
			"AuthorizationUnavailable": true, "AuthorizationMissing": true, "ReadForbidden": true,
			"ReadUnavailable": true, "PolicyMissing": true, "PolicyInvalid": true,
			"ResolutionSucceeded": true, "ResolutionFailed": true, "ResolutionUnavailable": true,
			"EvaluationSucceeded": true, "EvaluationDegraded": true, "EvaluationUnavailable": true,
			"InvalidExtraction": true, "ExtractionFailed": true, "InvalidValue": true,
			"OperatorFailed": true, "AggregationFailed": true, "AggregationLimit": true,
			"ReconciliationStarted": true, "StatusConflict": true, "StatusUnavailable": true,
			"ResultLimitExceeded": true, "EvaluationTimedOut": true,
		}[value]
	}
	return allowed[field][value]
}

// HTTPReady probes a loopback endpoint with a deadline and bounded body.
func HTTPReady(ctx context.Context, endpoint string, expected string) error {
	if endpoint == "" {
		return errors.New("endpoint is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	buffer := make([]byte, 256)
	n, _ := response.Body.Read(buffer)
	if expected != "" && !strings.Contains(string(buffer[:n]), expected) {
		return errors.New("HTTP body did not contain expected marker")
	}
	return nil
}

// DiagnosticDigest is useful to report a stable digest without exposing the
// contents of a private diagnostic file.
func DiagnosticDigest(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
