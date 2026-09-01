// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package managerapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	helmReleaseNameAnnotation      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"
)

// ApplyManagedPolicy reads one chart-rendered policy payload and applies it as
// the singleton installation ceiling. It only updates a policy already owned
// by the same Helm release; a foreign singleton fails closed without mutation.
func ApplyManagedPolicy(ctx context.Context, restConfig *rest.Config, policyFile, releaseName, releaseNamespace string) error {
	if restConfig == nil {
		return errors.New("REST config is required")
	}
	if policyFile == "" || !filepath.IsAbs(policyFile) {
		return errors.New("policy file must be an absolute path")
	}
	if releaseName == "" || releaseNamespace == "" {
		return errors.New("release name and namespace are required")
	}
	payload, err := os.ReadFile(policyFile)
	if err != nil {
		return fmt.Errorf("read policy payload: %w", err)
	}
	var desired v1alpha1.KubeseerAccessPolicy
	if err := json.Unmarshal(payload, &desired); err != nil {
		return fmt.Errorf("decode policy payload: %w", err)
	}
	if desired.Name != v1alpha1.InstallationAccessCeilingName {
		return fmt.Errorf("policy payload must target %q", v1alpha1.InstallationAccessCeilingName)
	}
	if desired.Spec.Namespaces.Mode == "" {
		return errors.New("policy payload must define a namespace mode")
	}

	policyScheme, err := NewScheme()
	if err != nil {
		return err
	}
	policyClient, err := client.New(restConfig, client.Options{Scheme: policyScheme})
	if err != nil {
		return fmt.Errorf("create policy client: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := types.NamespacedName{Name: v1alpha1.InstallationAccessCeilingName}
	var existing v1alpha1.KubeseerAccessPolicy
	if err := policyClient.Get(ctx, key, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("read installation access policy: %w", err)
		}
		desired.Annotations = ensureReleaseAnnotations(desired.Annotations, releaseName, releaseNamespace)
		if err := policyClient.Create(ctx, &desired); err != nil {
			return fmt.Errorf("create installation access policy: %w", err)
		}
		return nil
	}

	if existing.Annotations[helmReleaseNameAnnotation] != releaseName || existing.Annotations[helmReleaseNamespaceAnnotation] != releaseNamespace {
		return errors.New("installation access policy is owned by another release")
	}
	if apiequality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		return nil
	}
	existing.Spec = *desired.Spec.DeepCopy()
	if err := policyClient.Update(ctx, &existing); err != nil {
		return fmt.Errorf("update installation access policy: %w", err)
	}
	return nil
}

func ensureReleaseAnnotations(annotations map[string]string, releaseName, releaseNamespace string) map[string]string {
	if annotations == nil {
		annotations = make(map[string]string, 2)
	}
	annotations[helmReleaseNameAnnotation] = releaseName
	annotations[helmReleaseNamespaceAnnotation] = releaseNamespace
	return annotations
}
