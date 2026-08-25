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

// Package envtest owns the local Kubernetes API integration lifecycle.
package envtest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	controllerenvtest "sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	// DefaultKubernetesVersion is the version pinned by the repository's
	// test-api Make target when no compatibility version is supplied.
	DefaultKubernetesVersion = "1.35.6"

	// DefaultStartTimeout and DefaultStopTimeout bound each control-plane
	// component. They intentionally match controller-runtime's defaults while
	// making the lifecycle contract explicit at this repository boundary.
	DefaultStartTimeout = 20 * time.Second
	DefaultStopTimeout  = 20 * time.Second

	// DefaultRequestTimeout bounds client calls made by the shared clients.
	DefaultRequestTimeout = 30 * time.Second

	// DefaultPollInterval is used by readiness and cleanup polling helpers.
	DefaultPollInterval = 100 * time.Millisecond

	defaultCRDWait = 20 * time.Second
)

var scopeCounter atomic.Uint64

// Options controls CRD installation and lifecycle deadlines for one local
// control plane. CRDs are installed only after the returned API endpoint has
// passed the loopback safety check.
type Options struct {
	CRDDirectoryPaths     []string
	CRDs                  []*apiextensionsv1.CustomResourceDefinition
	ErrorIfCRDPathMissing bool
	CRDMaxWait            time.Duration
	CRDPollInterval       time.Duration
	StartTimeout          time.Duration
	StopTimeout           time.Duration

	// SkipNamespace disables creation of the unique namespace in Scope. It is
	// useful for a suite that only exercises cluster-scoped APIs.
	SkipNamespace bool
}

// Scope identifies all state owned by one harness instance.
type Scope struct {
	Prefix    string
	Namespace string
	TempDir   string
}

// CleanupFunc removes one resource family owned by a Scope. Functions are
// called in reverse registration order with one shared bounded context.
type CleanupFunc func(context.Context) error

// Clients are all constructed from the rest.Config returned by the local
// envtest control plane. No client in this package loads kubeconfig or cloud
// credentials.
type Clients struct {
	Core          kubernetes.Interface
	APIExtensions apiextensionsclient.Interface
	Dynamic       dynamic.Interface
	Discovery     discovery.DiscoveryInterface
}

// Harness owns one disposable local Kubernetes control plane.
type Harness struct {
	t                  testing.TB
	scope              Scope
	assets             string
	version            string
	options            Options
	environment        *controllerenvtest.Environment
	controlPlaneOutput *diagnosticBuffer

	config        *rest.Config
	started       bool
	installedCRDs []*apiextensionsv1.CustomResourceDefinition

	cleanupMu sync.Mutex
	cleanups  []cleanupEntry
	closeOnce sync.Once
	closeErr  error
}

type cleanupEntry struct {
	description string
	function    CleanupFunc
}

// New creates a harness and registers teardown with the owning test. It does
// not start any process; callers must call Start and handle its error.
func New(t testing.TB, options Options) (*Harness, error) {
	t.Helper()
	if t == nil {
		return nil, errors.New("envtest harness requires a non-nil testing handle")
	}

	assets, err := validateAssets()
	if err != nil {
		return nil, err
	}

	startTimeout := options.StartTimeout
	if startTimeout <= 0 {
		startTimeout = DefaultStartTimeout
	}
	stopTimeout := options.StopTimeout
	if stopTimeout <= 0 {
		stopTimeout = DefaultStopTimeout
	}
	options.StartTimeout = startTimeout
	options.StopTimeout = stopTimeout

	version := configuredKubernetesVersion()
	scope := newScope(t)
	environment := &controllerenvtest.Environment{
		// CRDs are deliberately installed by Start after verifyLoopbackHost.
		// Passing paths to controller-runtime here would install them before
		// this package could inspect the returned rest.Config.
		BinaryAssetsDirectory:    assets,
		UseExistingCluster:       boolPointer(false),
		ControlPlaneStartTimeout: startTimeout,
		ControlPlaneStopTimeout:  stopTimeout,
		AttachControlPlaneOutput: false,
	}
	controlPlaneOutput := &diagnosticBuffer{}
	apiServer := environment.ControlPlane.GetAPIServer()
	apiServer.Out = controlPlaneOutput
	apiServer.Err = controlPlaneOutput

	harness := &Harness{
		t:                  t,
		scope:              scope,
		assets:             assets,
		version:            version,
		options:            options,
		environment:        environment,
		controlPlaneOutput: controlPlaneOutput,
	}
	t.Cleanup(func() {
		if err := harness.Close(); err != nil {
			t.Errorf("envtest scope %s cleanup failed: %v", scope.Prefix, err)
		}
	})

	return harness, nil
}

// Start starts the local control plane, validates its endpoint, installs the
// requested CRDs, and creates the unique namespace unless SkipNamespace is
// set. Startup and readiness failures include the pinned asset context and a
// remediation command.
func (h *Harness) Start() (*rest.Config, error) {
	if h == nil {
		return nil, errors.New("envtest harness is nil")
	}
	if h.started {
		return h.Config(), nil
	}

	config, err := h.environment.Start()
	if err != nil {
		return nil, h.startupError("start local control plane", err)
	}
	h.config = config
	h.started = true

	if err := verifyLoopbackHost(config.Host); err != nil {
		return nil, h.abort(fmt.Errorf("local API endpoint safety check failed: %w", err))
	}

	if err := h.installCRDs(); err != nil {
		return nil, h.abort(h.startupError("install local CRDs", err))
	}
	if !h.options.SkipNamespace {
		if err := h.createNamespace(); err != nil {
			return nil, h.abort(h.startupError("create ownership namespace", err))
		}
	}

	return h.Config(), nil
}

// Config returns a client-safe copy of the local rest.Config. It fails closed
// by returning nil before Start and preserves the loopback-only boundary.
func (h *Harness) Config() *rest.Config {
	if h == nil || h.config == nil {
		return nil
	}
	config := rest.CopyConfig(h.config)
	if config.Timeout == 0 {
		config.Timeout = DefaultRequestTimeout
	}
	return config
}

// Clients constructs all standard client-go clients from Config.
func (h *Harness) Clients() (Clients, error) {
	config := h.Config()
	if config == nil {
		return Clients{}, errors.New("envtest clients requested before harness Start")
	}
	if err := verifyLoopbackHost(config.Host); err != nil {
		return Clients{}, fmt.Errorf("refusing non-loopback envtest client host: %w", err)
	}

	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return Clients{}, fmt.Errorf("create Kubernetes client for scope %s: %w", h.scope.Prefix, err)
	}
	apiExtensions, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return Clients{}, fmt.Errorf("create API extensions client for scope %s: %w", h.scope.Prefix, err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return Clients{}, fmt.Errorf("create dynamic client for scope %s: %w", h.scope.Prefix, err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return Clients{}, fmt.Errorf("create discovery client for scope %s: %w", h.scope.Prefix, err)
	}

	return Clients{
		Core:          core,
		APIExtensions: apiExtensions,
		Dynamic:       dynamicClient,
		Discovery:     discoveryClient,
	}, nil
}

// Scope returns the unique ownership scope for this harness.
func (h *Harness) Scope() Scope {
	if h == nil {
		return Scope{}
	}
	return h.scope
}

// AssetsDirectory returns the validated local asset directory.
func (h *Harness) AssetsDirectory() string {
	if h == nil {
		return ""
	}
	return h.assets
}

// AddCleanup registers an explicit, scope-owned cleanup operation. Cleanup
// callbacks should remove objects before the harness removes its namespace and
// CRDs. A callback registered after Close is ignored because the scope is
// already immutable.
func (h *Harness) AddCleanup(description string, cleanup CleanupFunc) {
	if h == nil || cleanup == nil {
		return
	}
	h.cleanupMu.Lock()
	defer h.cleanupMu.Unlock()
	if h.closeErr != nil {
		return
	}
	h.cleanups = append(h.cleanups, cleanupEntry{description: description, function: cleanup})
}

// Close performs explicit scope cleanup and stops the local control plane.
// It is idempotent so callers may use it in a defer in addition to the test's
// registered cleanup.
func (h *Harness) Close() error {
	if h == nil {
		return nil
	}
	h.closeOnce.Do(func() {
		if !h.started {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), h.options.StopTimeout)
		defer cancel()

		var cleanupErrors []error
		h.cleanupMu.Lock()
		cleanups := append([]cleanupEntry(nil), h.cleanups...)
		h.cleanups = nil
		h.cleanupMu.Unlock()
		for index := len(cleanups) - 1; index >= 0; index-- {
			entry := cleanups[index]
			if err := entry.function(ctx); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("%s: %w", entry.description, err))
			}
		}

		if err := h.deleteInstalledCRDs(ctx); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
		if err := h.environment.Stop(); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("stop control plane: %w", err))
		}
		if len(cleanupErrors) > 0 {
			h.closeErr = fmt.Errorf("envtest ownership scope %s cleanup failed: %w", h.scope.Prefix, errors.Join(cleanupErrors...))
		}
	})
	return h.closeErr
}

// WaitFor polls a readiness condition with an explicit bounded timeout.
func WaitFor(ctx context.Context, timeout time.Duration, condition func(context.Context) (bool, error)) error {
	if condition == nil {
		return errors.New("envtest readiness condition is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = defaultCRDWait
	}
	return wait.PollUntilContextTimeout(ctx, DefaultPollInterval, timeout, true, condition)
}

func (h *Harness) installCRDs() error {
	if len(h.options.CRDDirectoryPaths) == 0 && len(h.options.CRDs) == 0 {
		return nil
	}

	maxWait := h.options.CRDMaxWait
	if maxWait <= 0 {
		maxWait = defaultCRDWait
	}
	pollInterval := h.options.CRDPollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	crdOptions := controllerenvtest.CRDInstallOptions{
		Paths:              append([]string(nil), h.options.CRDDirectoryPaths...),
		CRDs:               deepCopyCRDs(h.options.CRDs),
		ErrorIfPathMissing: h.options.ErrorIfCRDPathMissing,
		MaxTime:            maxWait,
		PollInterval:       pollInterval,
		CleanUpAfterUse:    false,
	}
	installed, err := controllerenvtest.InstallCRDs(h.config, crdOptions)
	h.installedCRDs = installed
	if err != nil {
		return err
	}
	return nil
}

func (h *Harness) createNamespace() error {
	clients, err := h.Clients()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultRequestTimeout)
	defer cancel()
	_, err = clients.Core.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: h.scope.Namespace,
			Labels: map[string]string{
				"kubeseer.io/test-scope": h.scope.Prefix,
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create namespace %s: %w", h.scope.Namespace, err)
	}
	h.AddCleanup("delete namespace "+h.scope.Namespace, func(ctx context.Context) error {
		err := clients.Core.CoreV1().Namespaces().Delete(ctx, h.scope.Namespace, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("delete namespace %s: %w", h.scope.Namespace, err)
		}
		return nil
	})
	return nil
}

func (h *Harness) deleteInstalledCRDs(ctx context.Context) error {
	if len(h.installedCRDs) == 0 || h.config == nil {
		return nil
	}
	client, err := apiextensionsclient.NewForConfig(h.Config())
	if err != nil {
		return fmt.Errorf("create CRD cleanup client for scope %s: %w", h.scope.Prefix, err)
	}
	var cleanupErrors []error
	for index := len(h.installedCRDs) - 1; index >= 0; index-- {
		crd := h.installedCRDs[index]
		if err := client.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, crd.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("delete CRD %s: %w", crd.Name, err))
		}
	}
	if len(cleanupErrors) > 0 {
		return errors.Join(cleanupErrors...)
	}
	return nil
}

func (h *Harness) abort(err error) error {
	if cleanupErr := h.Close(); cleanupErr != nil {
		return errors.Join(err, cleanupErr)
	}
	return err
}

func (h *Harness) startupError(operation string, err error) error {
	diagnostics := h.controlPlaneOutput.String()
	if len(diagnostics) > 16*1024 {
		diagnostics = diagnostics[len(diagnostics)-16*1024:]
	}
	if diagnostics != "" {
		err = fmt.Errorf("%w; retained control-plane output:\n%s", err, diagnostics)
	}
	return fmt.Errorf(
		"%s for envtest scope %s using Kubernetes %s assets %s on %s/%s: %w; verify local binaries with `KUBEBUILDER_ASSETS=$(./hack/envtest-assets.sh %s) make test-api` and ensure no existing-cluster mode is configured",
		operation,
		h.scope.Prefix,
		h.version,
		h.assets,
		runtime.GOOS,
		runtime.GOARCH,
		err,
		h.version,
	)
}

type diagnosticBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *diagnosticBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(value)
}

func (b *diagnosticBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

func validateAssets() (string, error) {
	assets := strings.TrimSpace(os.Getenv("KUBEBUILDER_ASSETS"))
	version := configuredKubernetesVersion()
	if assets == "" {
		return "", fmt.Errorf(
			"KUBEBUILDER_ASSETS is required for local envtest on %s/%s (Kubernetes %s); run `KUBEBUILDER_ASSETS=$(./hack/envtest-assets.sh %s) make test-api`, never use an existing cluster",
			runtime.GOOS,
			runtime.GOARCH,
			version,
			version,
		)
	}
	absAssets, err := filepath.Abs(assets)
	if err != nil {
		return "", fmt.Errorf("resolve KUBEBUILDER_ASSETS %q: %w", assets, err)
	}
	info, err := os.Stat(absAssets)
	if err != nil {
		return "", fmt.Errorf("KUBEBUILDER_ASSETS=%s is not readable: %w; run `./hack/envtest-assets.sh %s`", absAssets, err, version)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("KUBEBUILDER_ASSETS=%s is not a directory", absAssets)
	}

	for _, binary := range []string{"etcd", "kube-apiserver", "kubectl"} {
		path := filepath.Join(absAssets, binary)
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("KUBEBUILDER_ASSETS=%s is missing %s: %w; run `./hack/envtest-assets.sh %s`", absAssets, binary, err, version)
		}
		if info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("envtest asset %s is not executable: %s", binary, path)
		}
	}

	for _, variable := range []string{"TEST_ASSET_ETCD", "TEST_ASSET_KUBE_APISERVER", "TEST_ASSET_KUBECTL"} {
		if value, ok := os.LookupEnv(variable); ok {
			return "", fmt.Errorf("%s is set to %q and would override pinned KUBEBUILDER_ASSETS; unset it before running envtest", variable, value)
		}
	}
	return absAssets, nil
}

func verifyLoopbackHost(host string) error {
	parsed, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("parse API server host %q: %w", host, err)
	}
	if parsed.Scheme == "" || parsed.Hostname() == "" || parsed.Port() == "" {
		return fmt.Errorf("API server host %q is not an absolute host URL", host)
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("API server host %q contains unexpected URL components", host)
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("API server host %q is not a loopback IP", host)
	}
	return nil
}

func configuredKubernetesVersion() string {
	if version := strings.TrimSpace(os.Getenv("KUBERNETES_VERSION")); version != "" {
		return version
	}
	return DefaultKubernetesVersion
}

func newScope(t testing.TB) Scope {
	seed := fmt.Sprintf("%s-%d-%d", t.Name(), time.Now().UnixNano(), scopeCounter.Add(1))
	digest := sha256.Sum256([]byte(seed))
	token := hex.EncodeToString(digest[:])[:12]
	prefix := "kubeseer-" + token
	return Scope{
		Prefix:    prefix,
		Namespace: prefix + "-ns",
		TempDir:   t.TempDir(),
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func deepCopyCRDs(crds []*apiextensionsv1.CustomResourceDefinition) []*apiextensionsv1.CustomResourceDefinition {
	if len(crds) == 0 {
		return nil
	}
	copyCRDs := make([]*apiextensionsv1.CustomResourceDefinition, 0, len(crds))
	for _, crd := range crds {
		if crd != nil {
			copyCRDs = append(copyCRDs, crd.DeepCopy())
		}
	}
	return copyCRDs
}
