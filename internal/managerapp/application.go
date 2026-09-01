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

// Package managerapp owns the production composition root. Keeping this
// package separate from the command parser lets integration tests construct
// the same manager that the container starts without a live cluster.
package managerapp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/accesspolicy"
	"github.com/steeltanuki/kubeseer/internal/admission"
	discoveryruntime "github.com/steeltanuki/kubeseer/internal/discovery"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/reconciliation"
	"k8s.io/apimachinery/pkg/runtime"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	DefaultMetricsBindAddress         = ":8080"
	DefaultHealthProbeBindAddress     = ":8081"
	DefaultWebhookPort                = 9443
	DefaultWebhookCertDir             = "/var/run/secrets/kubeseer/webhook"
	DefaultWebhookCertName            = "tls.crt"
	DefaultWebhookKeyName             = "tls.key"
	DefaultWebhookCAFile              = "/var/run/secrets/kubeseer/ca/ca.crt"
	DefaultLeaderElectionNamespace    = "kubeseer-system"
	DefaultLeaderElectionID           = "kubeseer-controller"
	DefaultLeaderElectionResourceLock = "leases"
	DefaultSafetyInterval             = 5 * time.Minute
	DefaultGracefulShutdownTimeout    = 30 * time.Second
)

// Config is the immutable startup configuration shared by the command and
// the production composition root.
type Config struct {
	MetricsBindAddress         string
	HealthProbeBindAddress     string
	WebhookPort                int
	WebhookCertDir             string
	WebhookCertName            string
	WebhookKeyName             string
	WebhookCAFile              string
	WebhookDNSNames            []string
	LeaderElection             bool
	LeaderElectionNamespace    string
	LeaderElectionID           string
	LeaderElectionResourceLock string
	SafetyInterval             time.Duration
	GracefulShutdownTimeout    time.Duration
	LimitOverrides             limits.Overrides
	Logger                     logr.Logger
}

// DefaultConfig returns the reviewed production defaults. The returned value
// is independent and can be safely modified by a command-line parser.
func DefaultConfig() Config {
	return Config{
		MetricsBindAddress:     DefaultMetricsBindAddress,
		HealthProbeBindAddress: DefaultHealthProbeBindAddress,
		WebhookPort:            DefaultWebhookPort,
		WebhookCertDir:         DefaultWebhookCertDir,
		WebhookCertName:        DefaultWebhookCertName,
		WebhookKeyName:         DefaultWebhookKeyName,
		WebhookCAFile:          DefaultWebhookCAFile,
		WebhookDNSNames: []string{
			"kubeseer-webhook",
			"kubeseer-webhook.kubeseer-system",
			"kubeseer-webhook.kubeseer-system.svc",
			"kubeseer-webhook.kubeseer-system.svc.cluster.local",
		},
		LeaderElection:             true,
		LeaderElectionNamespace:    DefaultLeaderElectionNamespace,
		LeaderElectionID:           DefaultLeaderElectionID,
		LeaderElectionResourceLock: DefaultLeaderElectionResourceLock,
		SafetyInterval:             DefaultSafetyInterval,
		GracefulShutdownTimeout:    DefaultGracefulShutdownTimeout,
		Logger:                     logr.Discard(),
	}
}

// Validate checks all values that can make manager startup unsafe or
// ambiguous. Kubernetes-dependent checks remain in the lifecycle hooks.
func (c Config) Validate() error {
	if strings.TrimSpace(c.MetricsBindAddress) == "" {
		return errors.New("metrics bind address must not be empty")
	}
	if strings.TrimSpace(c.HealthProbeBindAddress) == "" {
		return errors.New("health probe bind address must not be empty")
	}
	if c.WebhookPort < 1 || c.WebhookPort > 65535 {
		return fmt.Errorf("webhook port must be between 1 and 65535: %d", c.WebhookPort)
	}
	if err := validateCertDir(c.WebhookCertDir); err != nil {
		return err
	}
	if err := validateCertFile(c.WebhookCertName, "webhook certificate name"); err != nil {
		return err
	}
	if err := validateCertFile(c.WebhookKeyName, "webhook key name"); err != nil {
		return err
	}
	if err := validateCertPath(c.WebhookCAFile, "webhook CA file"); err != nil {
		return err
	}
	if len(c.WebhookDNSNames) == 0 {
		return errors.New("webhook DNS names must not be empty")
	}
	for _, name := range c.WebhookDNSNames {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, " ,") {
			return fmt.Errorf("webhook DNS name is invalid: %q", name)
		}
	}
	if strings.TrimSpace(c.LeaderElectionNamespace) == "" {
		return errors.New("leader election namespace must not be empty")
	}
	if strings.TrimSpace(c.LeaderElectionID) == "" {
		return errors.New("leader election id must not be empty")
	}
	if strings.TrimSpace(c.LeaderElectionResourceLock) == "" {
		return errors.New("leader election resource lock must not be empty")
	}
	if c.SafetyInterval <= 0 {
		return errors.New("safety interval must be positive")
	}
	if c.GracefulShutdownTimeout <= 0 {
		return errors.New("graceful shutdown timeout must be positive")
	}
	return nil
}

func validateCertDir(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("webhook certificate directory must not be empty")
	}
	if !filepath.IsAbs(value) || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("webhook certificate directory must be an absolute path: %q", value)
	}
	return nil
}

func validateCertPath(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if !filepath.IsAbs(value) || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s must be an absolute path: %q", field, value)
	}
	return nil
}

func validateCertFile(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s must be a simple file name: %q", field, value)
	}
	return nil
}

// Application contains the fully composed manager and the resolved immutable
// runtime profile used by reconciliation and admission.
type Application struct {
	Manager ctrlmanager.Manager
	Config  Config
	Profile limits.Profile
}

// Build constructs the same manager used by the production executable. It
// does not start listeners or contact the Kubernetes API.
func Build(restConfig *rest.Config, config Config) (*Application, error) {
	if restConfig == nil {
		return nil, errors.New("REST config is required")
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid manager configuration: %w", err)
	}
	profile, err := limits.Resolve(config.LimitOverrides)
	if err != nil {
		return nil, fmt.Errorf("resolve manager limits: %w", err)
	}

	managerScheme, err := NewScheme()
	if err != nil {
		return nil, err
	}

	webhookServer := webhook.NewServer(webhook.Options{
		Port:     config.WebhookPort,
		CertDir:  config.WebhookCertDir,
		CertName: config.WebhookCertName,
		KeyName:  config.WebhookKeyName,
		TLSOpts: []func(*tls.Config){func(tlsConfig *tls.Config) {
			tlsConfig.MinVersion = tls.VersionTLS12
		}},
	})
	managerOptions := ctrlmanager.Options{
		Scheme:                     managerScheme,
		Logger:                     config.Logger,
		LeaderElection:             config.LeaderElection,
		LeaderElectionResourceLock: config.LeaderElectionResourceLock,
		LeaderElectionNamespace:    config.LeaderElectionNamespace,
		LeaderElectionID:           config.LeaderElectionID,
		Metrics:                    server.Options{BindAddress: config.MetricsBindAddress},
		HealthProbeBindAddress:     config.HealthProbeBindAddress,
		WebhookServer:              webhookServer,
		GracefulShutdownTimeout:    &config.GracefulShutdownTimeout,
	}
	mgr, err := ctrlmanager.New(restConfig, managerOptions)
	if err != nil {
		return nil, fmt.Errorf("create controller manager: %w", err)
	}
	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		return nil, fmt.Errorf("register liveness check: %w", err)
	}
	if err := mgr.AddReadyzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		return nil, fmt.Errorf("register readiness check: %w", err)
	}
	if err := mgr.AddReadyzCheck("webhook-certificate", CertificateReadinessChecker(
		filepath.Join(config.WebhookCertDir, config.WebhookCertName),
		filepath.Join(config.WebhookCertDir, config.WebhookKeyName),
		config.WebhookCAFile,
		config.WebhookDNSNames,
	)); err != nil {
		return nil, fmt.Errorf("register certificate readiness check: %w", err)
	}

	discoveryClient, err := k8sdiscovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create admission discovery client: %w", err)
	}
	validator := admission.NewValidatorWithProfile(
		discoveryruntime.NewResolver(discoveryClient),
		accesspolicy.NewClientPolicySource(mgr.GetAPIReader()),
		profile,
	)
	admission.Register(mgr.GetWebhookServer(), managerScheme, validator)
	if err := reconciliation.SetupWithManager(mgr, reconciliation.Options{
		SafetyInterval: config.SafetyInterval,
		LimitProfile:   &profile,
	}); err != nil {
		return nil, fmt.Errorf("register reconciliation runtime: %w", err)
	}

	return &Application{Manager: mgr, Config: config, Profile: profile}, nil
}

// NewScheme returns the private scheme shared by manager and lifecycle
// clients. It deliberately does not mutate client-go's global scheme.
func NewScheme() (*runtime.Scheme, error) {
	managerScheme := runtime.NewScheme()
	if err := scheme.AddToScheme(managerScheme); err != nil {
		return nil, fmt.Errorf("register Kubernetes scheme: %w", err)
	}
	if err := v1alpha1.AddToScheme(managerScheme); err != nil {
		return nil, fmt.Errorf("register Kubeseer scheme: %w", err)
	}
	return managerScheme, nil
}

// Start runs all manager-owned servers and controllers until ctx is canceled.
func (a *Application) Start(ctx context.Context) error {
	if a == nil || a.Manager == nil {
		return errors.New("manager application is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return a.Manager.Start(ctx)
}

// PackageAction is the finite set of lifecycle operations accepted by the
// manager image. Implementations may add cluster work behind these stable
// names without changing Helm hook arguments.
type PackageAction string

const (
	PackagePreflight  PackageAction = "preflight"
	PackagePolicy     PackageAction = "policy"
	PackageVerify     PackageAction = "verify"
	PackagePreDelete  PackageAction = "pre-delete"
	PackagePostDelete PackageAction = "post-delete"
)

// ParsePackageAction rejects arbitrary package command strings before any
// Kubernetes client is constructed.
func ParsePackageAction(value string) (PackageAction, error) {
	action := PackageAction(strings.TrimSpace(value))
	switch action {
	case PackagePreflight, PackagePolicy, PackageVerify, PackagePreDelete, PackagePostDelete:
		return action, nil
	default:
		return "", fmt.Errorf("unsupported package action %q", value)
	}
}
