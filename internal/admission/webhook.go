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

package admission

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	cradmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// KubeseerWebhookPath is the stable endpoint for Kubeseer main resources.
	KubeseerWebhookPath = "/validate-kubeseer-io-v1alpha1-kubeseer"

	// KubeseerAccessPolicyWebhookPath is the stable endpoint for the singleton
	// installation access policy main resource.
	KubeseerAccessPolicyWebhookPath = "/validate-kubeseer-io-v1alpha1-kubeseeraccesspolicy"

	validatingWebhookConfigurationName = "kubeseer-validating-webhook"
	kubeseerWebhookName                = "kubeseer.kubeseer.io"
	accessPolicyWebhookName            = "kubeseeraccesspolicy.kubeseer.io"
)

// Register installs both typed validating handlers on the supplied
// controller-runtime webhook server. The server owns TLS and lifecycle;
// this package only owns endpoint registration.
func Register(server webhook.Server, scheme *runtime.Scheme, validator *Validator) {
	if server == nil || scheme == nil {
		return
	}
	server.Register(KubeseerWebhookPath, NewKubeseerWebhook(scheme, validator))
	server.Register(KubeseerAccessPolicyWebhookPath, NewKubeseerAccessPolicyWebhook(scheme, validator))
}

// NewKubeseerWebhook creates the generic controller-runtime validator for
// Kubeseer objects.
func NewKubeseerWebhook(scheme *runtime.Scheme, validator *Validator) *cradmission.Webhook {
	return cradmission.WithValidator[*v1alpha1.Kubeseer](scheme, &kubeseerWebhookValidator{validator: validator})
}

// NewKubeseerAccessPolicyWebhook creates the generic controller-runtime
// validator for KubeseerAccessPolicy objects.
func NewKubeseerAccessPolicyWebhook(scheme *runtime.Scheme, validator *Validator) *cradmission.Webhook {
	return cradmission.WithValidator[*v1alpha1.KubeseerAccessPolicy](scheme, &accessPolicyWebhookValidator{validator: validator})
}

type kubeseerWebhookValidator struct {
	validator *Validator
}

func (v *kubeseerWebhookValidator) ValidateCreate(ctx context.Context, object *v1alpha1.Kubeseer) (cradmission.Warnings, error) {
	return nil, v.validate(ctx, object)
}

func (v *kubeseerWebhookValidator) ValidateUpdate(ctx context.Context, _, object *v1alpha1.Kubeseer) (cradmission.Warnings, error) {
	return nil, v.validate(ctx, object)
}

func (*kubeseerWebhookValidator) ValidateDelete(context.Context, *v1alpha1.Kubeseer) (cradmission.Warnings, error) {
	return nil, nil
}

func (v *kubeseerWebhookValidator) validate(ctx context.Context, object *v1alpha1.Kubeseer) error {
	if v == nil || v.validator == nil {
		return unavailableError("Kubeseer")
	}
	return resultError("Kubeseer", v.validator.ValidateKubeseer(ctx, object))
}

type accessPolicyWebhookValidator struct {
	validator *Validator
}

func (v *accessPolicyWebhookValidator) ValidateCreate(ctx context.Context, object *v1alpha1.KubeseerAccessPolicy) (cradmission.Warnings, error) {
	return nil, v.validate(ctx, object)
}

func (v *accessPolicyWebhookValidator) ValidateUpdate(ctx context.Context, _, object *v1alpha1.KubeseerAccessPolicy) (cradmission.Warnings, error) {
	return nil, v.validate(ctx, object)
}

func (*accessPolicyWebhookValidator) ValidateDelete(context.Context, *v1alpha1.KubeseerAccessPolicy) (cradmission.Warnings, error) {
	return nil, nil
}

func (v *accessPolicyWebhookValidator) validate(ctx context.Context, object *v1alpha1.KubeseerAccessPolicy) error {
	if v == nil || v.validator == nil {
		return unavailableError("KubeseerAccessPolicy")
	}
	return resultError("KubeseerAccessPolicy", v.validator.ValidateAccessPolicy(ctx, object))
}

func unavailableError(kind string) error {
	return resultError(kind, result([]Issue{{
		Path:    "spec",
		Reason:  "ValidationUnavailable",
		Message: "admission validation is unavailable",
		Class:   Unavailable,
	}}))
}

func resultError(kind string, validation Result) error {
	if validation.Valid() {
		return nil
	}

	issues := validation.IssuesCopy()
	causes := make([]metav1.StatusCause, 0, len(issues))
	for _, issue := range issues {
		causes = append(causes, metav1.StatusCause{
			Type:    metav1.CauseType(issue.Reason),
			Field:   issue.Path,
			Message: issue.Message,
		})
	}

	status := metav1.Status{
		Status:  metav1.StatusFailure,
		Message: fmt.Sprintf("%s admission validation failed with %d issue(s)", kind, len(issues)),
		Details: &metav1.StatusDetails{Causes: causes},
	}
	switch validation.Class() {
	case Forbidden:
		status.Code = http.StatusForbidden
		status.Reason = metav1.StatusReasonForbidden
	case Unavailable:
		status.Code = http.StatusServiceUnavailable
		status.Reason = metav1.StatusReasonServiceUnavailable
	default:
		status.Code = http.StatusUnprocessableEntity
		status.Reason = metav1.StatusReasonInvalid
	}
	return &apierrors.StatusError{ErrStatus: status}
}

// WebhookConfiguration builds the endpoint-agnostic registration contract.
// The supplied client configuration is copied and receives the two stable
// endpoint paths; TLS material and Service/certificate lifecycle remain the
// caller's responsibility.
func WebhookConfiguration(clientConfig admissionregistrationv1.WebhookClientConfig) *admissionregistrationv1.ValidatingWebhookConfiguration {
	failurePolicy := admissionregistrationv1.Fail
	matchPolicy := admissionregistrationv1.Exact
	sideEffects := admissionregistrationv1.SideEffectClassNone
	timeoutSeconds := int32(10)

	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       "ValidatingWebhookConfiguration",
		},
		ObjectMeta: metav1.ObjectMeta{Name: validatingWebhookConfigurationName},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name:                    kubeseerWebhookName,
				ClientConfig:            clientConfigForPath(clientConfig, KubeseerWebhookPath),
				Rules:                   []admissionregistrationv1.RuleWithOperations{resourceRule(admissionregistrationv1.NamespacedScope, "kubeseers")},
				FailurePolicy:           &failurePolicy,
				MatchPolicy:             &matchPolicy,
				SideEffects:             &sideEffects,
				TimeoutSeconds:          &timeoutSeconds,
				AdmissionReviewVersions: []string{"v1"},
			},
			{
				Name:                    accessPolicyWebhookName,
				ClientConfig:            clientConfigForPath(clientConfig, KubeseerAccessPolicyWebhookPath),
				Rules:                   []admissionregistrationv1.RuleWithOperations{resourceRule(admissionregistrationv1.ClusterScope, "kubeseeraccesspolicies")},
				FailurePolicy:           &failurePolicy,
				MatchPolicy:             &matchPolicy,
				SideEffects:             &sideEffects,
				TimeoutSeconds:          &timeoutSeconds,
				AdmissionReviewVersions: []string{"v1"},
			},
		},
	}
}

func resourceRule(scope admissionregistrationv1.ScopeType, resource string) admissionregistrationv1.RuleWithOperations {
	return admissionregistrationv1.RuleWithOperations{
		Operations: []admissionregistrationv1.OperationType{
			admissionregistrationv1.Create,
			admissionregistrationv1.Update,
		},
		Rule: admissionregistrationv1.Rule{
			APIGroups:   []string{v1alpha1.GroupVersion.Group},
			APIVersions: []string{v1alpha1.GroupVersion.Version},
			Resources:   []string{resource},
			Scope:       &scope,
		},
	}
}

func clientConfigForPath(input admissionregistrationv1.WebhookClientConfig, path string) admissionregistrationv1.WebhookClientConfig {
	copy := input.DeepCopy()
	if copy == nil {
		copy = &admissionregistrationv1.WebhookClientConfig{}
	}
	if copy.URL != nil {
		value := *copy.URL
		if parsed, err := url.Parse(value); err == nil {
			parsed.Path = path
			parsed.RawPath = ""
			parsed.RawQuery = ""
			parsed.Fragment = ""
			value = parsed.String()
		} else {
			value = strings.TrimRight(value, "/") + path
		}
		copy.URL = &value
	}
	if copy.Service != nil {
		copy.Service.Path = stringPointer(path)
	}
	return *copy
}

func stringPointer(value string) *string {
	return &value
}

var _ cradmission.Validator[*v1alpha1.Kubeseer] = (*kubeseerWebhookValidator)(nil)
var _ cradmission.Validator[*v1alpha1.KubeseerAccessPolicy] = (*accessPolicyWebhookValidator)(nil)
