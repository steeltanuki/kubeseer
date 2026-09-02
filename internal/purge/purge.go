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

// Package purge contains the deliberately separate, destructive CRD cleanup
// path. It has no dependency on the Helm release and never selects Secrets or
// arbitrary objects by label.
//
// Responsibility: delete only the explicitly confirmed Kubeseer CRs and CRDs
// from the caller-selected Kubernetes target with bounded, sanitized results.
//
// Boundary: purge requires explicit kubeconfig, context, server, and token;
// it never consults ambient targets or manages the Helm release lifecycle.
package purge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	extensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	ConfirmationToken   = "purge-kubeseer-crds"
	KubeseerCRDName     = "kubeseers.kubeseer.io"
	AccessPolicyCRDName = "kubeseeraccesspolicies.kubeseer.io"
)

var purgeCollections = []purgeCollection{
	{
		Target: "kubeseers.kubeseer.io",
		GVR: schema.GroupVersionResource{
			Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseers",
		},
		CRDName:    KubeseerCRDName,
		Namespaced: true,
	},
	{
		Target: "kubeseeraccesspolicies.kubeseer.io",
		GVR: schema.GroupVersionResource{
			Group: "kubeseer.io", Version: "v1alpha1", Resource: "kubeseeraccesspolicies",
		},
		CRDName: AccessPolicyCRDName,
	},
}

// Options is the explicit target and confirmation supplied by the operator.
// No ambient kubeconfig, current context, or cloud credential is consulted.
type Options struct {
	KubeconfigPath string
	ContextName    string
	ConfirmContext string
	ConfirmServer  string
	Confirmation   string
	Timeout        time.Duration
}

// Target is the resolved cluster identity printed before any deletion.
type Target struct {
	Context string
	Server  string
}

// Result is a sanitized outcome for one exact target. It intentionally has no
// Kubernetes object payload or Secret field.
type Result struct {
	Target  string
	Action  string
	Outcome string
	Error   string
}

// Report is the complete ordered purge report.
type Report struct {
	Target  Target
	Results []Result
}

type purgeCollection struct {
	Target     string
	GVR        schema.GroupVersionResource
	CRDName    string
	Namespaced bool
}

// ResolveTarget reads only the explicitly selected kubeconfig and validates
// every destructive confirmation before returning a client configuration.
func ResolveTarget(options Options) (*rest.Config, Target, error) {
	if err := validateOptions(options); err != nil {
		return nil, Target{}, err
	}
	if !filepath.IsAbs(options.KubeconfigPath) {
		return nil, Target{}, errors.New("kubeconfig path must be absolute")
	}
	info, err := os.Stat(options.KubeconfigPath)
	if err != nil {
		return nil, Target{}, errors.New("explicit kubeconfig path is unavailable")
	}
	if !info.Mode().IsRegular() {
		return nil, Target{}, errors.New("explicit kubeconfig path is not a regular file")
	}
	rawConfig, err := clientcmd.LoadFromFile(options.KubeconfigPath)
	if err != nil {
		return nil, Target{}, errors.New("explicit kubeconfig could not be read")
	}
	contextConfig, ok := rawConfig.Contexts[options.ContextName]
	if !ok || contextConfig == nil || strings.TrimSpace(contextConfig.Cluster) == "" {
		return nil, Target{}, errors.New("explicit Kubernetes context is unavailable")
	}
	clusterConfig, ok := rawConfig.Clusters[contextConfig.Cluster]
	if !ok || clusterConfig == nil || strings.TrimSpace(clusterConfig.Server) == "" {
		return nil, Target{}, errors.New("explicit Kubernetes context has no server")
	}
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: options.KubeconfigPath}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: options.ContextName}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, Target{}, errors.New("explicit Kubernetes context could not be resolved")
	}
	server := normalizeServer(restConfig.Host)
	if server == "" || normalizeServer(options.ConfirmServer) != server {
		return nil, Target{}, errors.New("confirmed cluster server does not match the resolved target")
	}
	if options.ConfirmContext != options.ContextName {
		return nil, Target{}, errors.New("confirmed Kubernetes context does not match the resolved target")
	}
	return restConfig, Target{Context: options.ContextName, Server: server}, nil
}

// Execute deletes exact custom-resource instances first and the two exact CRD
// definitions second. It is idempotent and stops before CRD deletion when a
// collection remains non-empty or a finalizer blocks cleanup.
func Execute(ctx context.Context, restConfig *rest.Config, options Options, target Target) (Report, error) {
	report := Report{Target: target}
	if restConfig == nil {
		return report, errors.New("REST config is required")
	}
	if err := validateOptions(options); err != nil {
		return report, err
	}
	if target.Context != options.ContextName || normalizeServer(target.Server) != normalizeServer(options.ConfirmServer) {
		return report, errors.New("purge target does not match explicit confirmation")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	purgeContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return report, errors.New("create purge client failed")
	}
	crdClient, err := extensionsclient.NewForConfig(restConfig)
	if err != nil {
		return report, errors.New("create CRD purge client failed")
	}

	for _, collection := range purgeCollections {
		if err := purgeCollectionObjects(purgeContext, dynamicClient, collection, &report); err != nil {
			return report, fmt.Errorf("purge stopped at %s: %s", collection.Target, sanitizeError(err))
		}
	}
	for _, collection := range purgeCollections {
		if err := purgeCRD(purgeContext, crdClient, collection.CRDName, &report); err != nil {
			return report, fmt.Errorf("purge stopped at %s: %s", collection.CRDName, sanitizeError(err))
		}
	}
	return report, nil
}

func purgeCollectionObjects(ctx context.Context, dynamicClient dynamic.Interface, collection purgeCollection, report *Report) error {
	var resourceClient dynamic.ResourceInterface
	if collection.Namespaced {
		resourceClient = dynamicClient.Resource(collection.GVR).Namespace(metav1.NamespaceAll)
	} else {
		resourceClient = dynamicClient.Resource(collection.GVR)
	}
	list, err := resourceClient.List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) {
		report.Results = append(report.Results, Result{Target: collection.Target, Action: "delete", Outcome: "already absent"})
		return nil
	}
	if err != nil {
		report.Results = append(report.Results, Result{Target: collection.Target, Action: "list", Outcome: "failed", Error: sanitizeError(err)})
		return err
	}
	if len(list.Items) == 0 {
		report.Results = append(report.Results, Result{Target: collection.Target, Action: "delete", Outcome: "already absent"})
		return nil
	}
	for _, object := range list.Items {
		objectTarget := object.GetName()
		if object.GetNamespace() != "" {
			objectTarget = object.GetNamespace() + "/" + object.GetName()
		}
		deleteErr := resourceClient.Delete(ctx, object.GetName(), metav1.DeleteOptions{})
		if apierrors.IsNotFound(deleteErr) {
			report.Results = append(report.Results, Result{Target: objectTarget, Action: "delete", Outcome: "already absent"})
			continue
		}
		if deleteErr != nil {
			report.Results = append(report.Results, Result{Target: objectTarget, Action: "delete", Outcome: "failed", Error: sanitizeError(deleteErr)})
			return deleteErr
		}
		report.Results = append(report.Results, Result{Target: objectTarget, Action: "delete", Outcome: "deleted"})
	}
	if err := waitCollectionEmpty(ctx, resourceClient); err != nil {
		report.Results = append(report.Results, Result{Target: collection.Target, Action: "wait-empty", Outcome: "retained/blocked", Error: sanitizeError(err)})
		return err
	}
	return nil
}

func waitCollectionEmpty(ctx context.Context, resourceClient dynamic.ResourceInterface) error {
	return wait.PollUntilContextCancel(ctx, 500*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		list, err := resourceClient.List(ctx, metav1.ListOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return len(list.Items) == 0, nil
	})
}

func purgeCRD(ctx context.Context, crdClient *extensionsclient.Clientset, name string, report *Report) error {
	crds := crdClient.ApiextensionsV1().CustomResourceDefinitions()
	if _, err := crds.Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		report.Results = append(report.Results, Result{Target: name, Action: "delete", Outcome: "already absent"})
		return nil
	} else if err != nil {
		report.Results = append(report.Results, Result{Target: name, Action: "get", Outcome: "failed", Error: sanitizeError(err)})
		return err
	}
	if err := crds.Delete(ctx, name, metav1.DeleteOptions{}); apierrors.IsNotFound(err) {
		report.Results = append(report.Results, Result{Target: name, Action: "delete", Outcome: "already absent"})
		return nil
	} else if err != nil {
		report.Results = append(report.Results, Result{Target: name, Action: "delete", Outcome: "failed", Error: sanitizeError(err)})
		return err
	}
	if err := wait.PollUntilContextCancel(ctx, 500*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		_, err := crds.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}); err != nil {
		report.Results = append(report.Results, Result{Target: name, Action: "wait-absent", Outcome: "retained/blocked", Error: sanitizeError(err)})
		return err
	}
	report.Results = append(report.Results, Result{Target: name, Action: "delete", Outcome: "deleted"})
	return nil
}

func validateOptions(options Options) error {
	if strings.TrimSpace(options.KubeconfigPath) == "" {
		return errors.New("--kubeconfig is required")
	}
	if strings.TrimSpace(options.ContextName) == "" {
		return errors.New("--context is required")
	}
	if strings.TrimSpace(options.ConfirmContext) == "" {
		return errors.New("--confirm-context is required")
	}
	if strings.TrimSpace(options.ConfirmServer) == "" {
		return errors.New("--confirm-server is required")
	}
	if options.Confirmation != ConfirmationToken {
		return errors.New("exact destructive confirmation token is required")
	}
	if options.Timeout <= 0 {
		return errors.New("purge timeout must be positive")
	}
	return nil
}

func normalizeServer(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		parsed.RawPath = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return strings.TrimRight(parsed.String(), "/")
	}
	return strings.TrimRight(value, "/")
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "operation timed out"
	}
	if apierrors.IsForbidden(err) {
		return "operation forbidden"
	}
	if apierrors.IsConflict(err) {
		return "operation conflicted"
	}
	if apierrors.IsNotFound(err) {
		return "target already absent"
	}
	return "operation failed"
}
