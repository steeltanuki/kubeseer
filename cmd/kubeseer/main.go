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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	"github.com/steeltanuki/kubeseer/internal/limits"
	"github.com/steeltanuki/kubeseer/internal/managerapp"
	"k8s.io/klog/v2/klogr"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	ctrlruntimeLog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var (
	buildVersion = "unknown"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		args = []string{"manager"}
	}
	switch args[0] {
	case "manager":
		return runManager(args[1:], stdout, stderr)
	case "version":
		if len(args) != 1 {
			return errors.New("version does not accept arguments")
		}
		_, err := fmt.Fprintln(stdout, managerapp.VersionString(managerapp.VersionInfo{
			Version: buildVersion,
			Commit:  buildCommit,
			Date:    buildDate,
		}))
		return err
	case "package":
		return runPackage(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q (want manager, package, or version)", args[0])
	}
}

func runManager(args []string, stdout, stderr io.Writer) error {
	config := managerapp.DefaultConfig()
	webhookDNSNames := strings.Join(config.WebhookDNSNames, ",")
	maxMatchedResources := limits.DefaultMaxMatchedResources
	maxStatusBytes := limits.DefaultMaxStatusBytes
	flags := flag.NewFlagSet("manager", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&config.MetricsBindAddress, "metrics-bind-address", config.MetricsBindAddress, "address for the Prometheus metrics endpoint")
	flags.StringVar(&config.HealthProbeBindAddress, "health-probe-bind-address", config.HealthProbeBindAddress, "address for health and readiness probes")
	flags.IntVar(&config.WebhookPort, "webhook-port", config.WebhookPort, "webhook TLS port")
	flags.StringVar(&config.WebhookCertDir, "webhook-cert-dir", config.WebhookCertDir, "webhook certificate directory")
	flags.StringVar(&config.WebhookCertName, "webhook-cert-name", config.WebhookCertName, "webhook certificate file name")
	flags.StringVar(&config.WebhookKeyName, "webhook-key-name", config.WebhookKeyName, "webhook private key file name")
	flags.StringVar(&config.WebhookCAFile, "webhook-ca-file", config.WebhookCAFile, "webhook CA bundle file")
	flags.StringVar(&webhookDNSNames, "webhook-dns-names", webhookDNSNames, "comma-separated exact webhook Service DNS SANs")
	flags.BoolVar(&config.LeaderElection, "leader-election", config.LeaderElection, "enable leader election")
	flags.StringVar(&config.LeaderElectionNamespace, "leader-election-namespace", config.LeaderElectionNamespace, "leader election namespace")
	flags.StringVar(&config.LeaderElectionID, "leader-election-id", config.LeaderElectionID, "leader election identity")
	flags.StringVar(&config.LeaderElectionResourceLock, "leader-election-resource-lock", config.LeaderElectionResourceLock, "leader election resource lock")
	flags.DurationVar(&config.SafetyInterval, "safety-interval", config.SafetyInterval, "reconciliation safety interval")
	flags.DurationVar(&config.GracefulShutdownTimeout, "graceful-shutdown-timeout", config.GracefulShutdownTimeout, "manager graceful shutdown timeout")
	flags.IntVar(&maxMatchedResources, "max-matched-resources", maxMatchedResources, "maximum matched resources retained per source")
	flags.Int64Var(&maxStatusBytes, "max-status-bytes", maxStatusBytes, "maximum published status size in bytes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("manager does not accept positional arguments: %v", flags.Args())
	}
	config.WebhookDNSNames = splitDNSNames(webhookDNSNames)
	config.LimitOverrides.MaxMatchedResources = &maxMatchedResources
	config.LimitOverrides.MaxStatusBytes = &maxStatusBytes
	config.Logger = klogr.New()
	ctrlruntimeLog.SetLogger(config.Logger)
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid manager configuration: %w", err)
	}
	restConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	application, err := managerapp.Build(restConfig, config)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "starting %s\n", managerapp.VersionString(managerapp.VersionInfo{
		Version: buildVersion,
		Commit:  buildCommit,
		Date:    buildDate,
	})); err != nil {
		return err
	}
	return application.Start(signals.SetupSignalHandler())
}

func splitDNSNames(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func runPackage(args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 {
		return errors.New("usage: kubeseer package <preflight|policy|verify|pre-delete|post-delete>")
	}
	action, err := managerapp.ParsePackageAction(args[0])
	if err != nil {
		return err
	}
	if action == managerapp.PackagePolicy {
		return runPolicy(args[1:], stdout, stderr)
	}
	if action == managerapp.PackagePreflight || action == managerapp.PackageVerify {
		return runLifecycle(action, args[1:], stdout, stderr)
	}
	if action == managerapp.PackagePreDelete || action == managerapp.PackagePostDelete {
		return runCleanup(action, args[1:], stdout, stderr)
	}
	if len(args) != 1 {
		return fmt.Errorf("package action %s does not accept arguments", action)
	}
	_, err = fmt.Fprintf(stdout, "package action %s accepted\n", action)
	return err
}

func runCleanup(action managerapp.PackageAction, args []string, stdout, stderr io.Writer) error {
	releaseName := os.Getenv("KUBESEER_RELEASE_NAME")
	releaseNamespace := os.Getenv("KUBESEER_RELEASE_NAMESPACE")
	timeout := 2 * time.Minute
	flags := flag.NewFlagSet("package "+string(action), flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&releaseName, "release-name", releaseName, "Helm release name")
	flags.StringVar(&releaseNamespace, "release-namespace", releaseNamespace, "Helm release namespace")
	flags.DurationVar(&timeout, "timeout", timeout, "bounded cleanup timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("package action %s does not accept positional arguments: %v", action, flags.Args())
	}
	if timeout <= 0 {
		return errors.New("cleanup timeout must be positive")
	}
	restConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	switch action {
	case managerapp.PackagePreDelete:
		err = managerapp.RemoveOwnedWebhook(ctx, restConfig, releaseName, releaseNamespace, timeout)
	case managerapp.PackagePostDelete:
		var retained []managerapp.RetainedResource
		retained, err = managerapp.ReportRetainedState(ctx, restConfig)
		if err == nil {
			for _, item := range retained {
				if _, printErr := fmt.Fprintf(stdout, "PACKAGE_RETAINED target=%s outcome=%s\n", item.Target, item.Outcome); printErr != nil {
					return printErr
				}
			}
		}
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "PACKAGE_ACTION=%s STATUS=passed\n", action)
	return err
}

func runLifecycle(action managerapp.PackageAction, args []string, stdout, stderr io.Writer) error {
	managerConfig := managerapp.DefaultConfig()
	options := managerapp.LifecycleOptions{
		ReleaseName:                           os.Getenv("KUBESEER_RELEASE_NAME"),
		ReleaseNamespace:                      os.Getenv("KUBESEER_RELEASE_NAMESPACE"),
		DeploymentName:                        managerapp.DefaultDeploymentName,
		WebhookServiceName:                    managerapp.DefaultWebhookServiceName,
		CertificateMode:                       managerapp.CertificateModeCertManager,
		ExpectedKubeseerCRDStorageVersion:     v1alpha1.GroupVersion.Version,
		ExpectedAccessPolicyCRDStorageVersion: v1alpha1.GroupVersion.Version,
		WebhookPort:                           int32(managerConfig.WebhookPort),
		WebhookCertPath:                       filepath.Join(managerConfig.WebhookCertDir, managerConfig.WebhookCertName),
		WebhookKeyPath:                        filepath.Join(managerConfig.WebhookCertDir, managerConfig.WebhookKeyName),
		WebhookCAPath:                         managerConfig.WebhookCAFile,
		WebhookDNSNames:                       append([]string(nil), managerConfig.WebhookDNSNames...),
		Timeout:                               2 * time.Minute,
	}
	webhookDNSNames := strings.Join(options.WebhookDNSNames, ",")
	webhookPort := int(options.WebhookPort)
	flags := flag.NewFlagSet("package "+string(action), flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.ReleaseName, "release-name", options.ReleaseName, "Helm release name")
	flags.StringVar(&options.ReleaseNamespace, "release-namespace", options.ReleaseNamespace, "Helm release namespace")
	flags.StringVar(&options.DeploymentName, "deployment-name", options.DeploymentName, "manager Deployment name")
	flags.StringVar(&options.WebhookServiceName, "webhook-service", options.WebhookServiceName, "webhook Service name")
	flags.StringVar(&options.CertificateMode, "certificate-mode", options.CertificateMode, "certificate lifecycle mode")
	flags.StringVar(&options.ExpectedVersion, "expected-version", options.ExpectedVersion, "expected manager version")
	flags.StringVar(&options.ExpectedImage, "expected-image", options.ExpectedImage, "expected manager image")
	flags.StringVar(&options.ExpectedKubeseerCRDStorageVersion, "expected-kubeseer-crd-storage-version", options.ExpectedKubeseerCRDStorageVersion, "expected Kubeseer CRD storage version")
	flags.StringVar(&options.ExpectedAccessPolicyCRDStorageVersion, "expected-access-policy-crd-storage-version", options.ExpectedAccessPolicyCRDStorageVersion, "expected access-policy CRD storage version")
	flags.IntVar(&webhookPort, "webhook-port", webhookPort, "webhook TLS port")
	flags.StringVar(&options.WebhookCertPath, "webhook-cert-path", options.WebhookCertPath, "webhook serving certificate path")
	flags.StringVar(&options.WebhookKeyPath, "webhook-key-path", options.WebhookKeyPath, "webhook serving key path")
	flags.StringVar(&options.WebhookCAPath, "webhook-ca-path", options.WebhookCAPath, "webhook CA bundle path")
	flags.StringVar(&webhookDNSNames, "webhook-dns-names", webhookDNSNames, "comma-separated exact webhook Service DNS SANs")
	flags.DurationVar(&options.Timeout, "timeout", options.Timeout, "bounded lifecycle timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("package action %s does not accept positional arguments: %v", action, flags.Args())
	}
	options.WebhookDNSNames = splitDNSNames(webhookDNSNames)
	options.WebhookPort = int32(webhookPort)
	restConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()
	switch action {
	case managerapp.PackagePreflight:
		err = managerapp.Preflight(ctx, restConfig, options)
	case managerapp.PackageVerify:
		err = managerapp.VerifyRelease(ctx, restConfig, options)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "PACKAGE_ACTION=%s STATUS=passed\n", action)
	return err
}

func runPolicy(args []string, stdout, stderr io.Writer) error {
	policyFile := ""
	releaseName := os.Getenv("KUBESEER_RELEASE_NAME")
	releaseNamespace := os.Getenv("KUBESEER_RELEASE_NAMESPACE")
	flags := flag.NewFlagSet("package policy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&policyFile, "policy-file", policyFile, "absolute path to the rendered policy JSON")
	flags.StringVar(&releaseName, "release-name", releaseName, "Helm release name")
	flags.StringVar(&releaseNamespace, "release-namespace", releaseNamespace, "Helm release namespace")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("package policy does not accept positional arguments: %v", flags.Args())
	}
	if policyFile == "" {
		return errors.New("package policy requires --policy-file")
	}
	restConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := managerapp.ApplyManagedPolicy(ctx, restConfig, policyFile, releaseName, releaseNamespace); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "PACKAGE_ACTION=policy STATUS=passed")
	return err
}
