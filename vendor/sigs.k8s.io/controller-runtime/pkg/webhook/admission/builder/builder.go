/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package builder

import (
	"errors"
	"fmt"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/types"
)

// WebhookBuilder builds a webhook based on the provided options.
type WebhookBuilder struct {
	// name specifies the name of the webhook. It must be unique among all webhooks.
	name string

	// path is the URL Path to register this webhook. e.g. "/mutate-pods".
	path string

	// handlers handle admission requests.
	// A WebhookBuilder may have multiple handlers.
	// For example, handlers[0] mutates a pod for feature foo.
	// handlers[1] mutates a pod for a different feature bar.
	handlers []admission.Handler

	// t specifies the type of the webhook.
	// Currently, Mutating and Validating are supported.
	t *types.WebhookType

	// operations define the operations this webhook cares.
	// only one of operations and Rules can be set.
	operations []admissionregistrationv1beta1.OperationType
	// apiType represents the resource that this webhook cares.
	// Only one of apiType and Rules can be set.
	apiType runtime.Object
	// rules contain a list of admissionregistrationv1beta1.RuleWithOperations
	// It overrides operations and apiType.
	rules []admissionregistrationv1beta1.RuleWithOperations

	// failurePolicy maps to the FailurePolicy in the admissionregistrationv1beta1.Webhook
	failurePolicy *admissionregistrationv1beta1.FailurePolicyType

	// namespaceSelector maps to the NamespaceSelector in the admissionregistrationv1beta1.Webhook
	namespaceSelector *metav1.LabelSelector

	// manager is the manager for the webhook.
	// It is used for provisioning various dependencies for the webhook. e.g. RESTMapper.
	manager manager.Manager
}

// NewWebhookBuilder creates an empty WebhookBuilder.
func NewWebhookBuilder() *WebhookBuilder {
	return &WebhookBuilder{}
}

// Name sets the name of the webhook.
// This is optional
func (b *WebhookBuilder) Name(name string) *WebhookBuilder {
	b.name = name
	return b
}

// Mutating sets the type to mutating admission webhook
// Only one of Mutating and Validating can be invoked.
func (b *WebhookBuilder) Mutating() *WebhookBuilder {
	m := types.WebhookTypeMutating
	b.t = &m
	return b
}

// Validating sets the type to validating admission webhook
// Only one of Mutating and Validating can be invoked.
func (b *WebhookBuilder) Validating() *WebhookBuilder {
	m := types.WebhookTypeValidating
	b.t = &m
	return b
}

// Path sets the path for the webhook.
// Path needs to be unique among different webhooks.
// This is optional. If not set, it will be built from the type and resource name.
// For example, a webhook that mutates pods has a default path of "/mutate-pods"
// If the defaulting logic can't find a unique path for it, user need to set it manually.
func (b *WebhookBuilder) Path(path string) *WebhookBuilder {
	b.path = path
	return b
}

// Operations sets the operations that this webhook cares.
// It will be overridden by Rules if Rules are not empty.
// This is optional
func (b *WebhookBuilder) Operations(ops ...admissionregistrationv1beta1.OperationType) *WebhookBuilder {
	b.operations = ops
	return b
}

// ForType sets the type of resources that the webhook will operate.
// It will be overridden by Rules if Rules are not empty.
func (b *WebhookBuilder) ForType(obj runtime.Object) *WebhookBuilder {
	b.apiType = obj
	return b
}

// Rules sets the RuleWithOperations for the webhook.
// It overrides ForType and Operations.
// This is optional and for advanced user.
func (b *WebhookBuilder) Rules(rules ...admissionregistrationv1beta1.RuleWithOperations) *WebhookBuilder {
	b.rules = rules
	return b
}

// FailurePolicy sets the FailurePolicy of the webhook.
// If not set, it will be defaulted by the server.
// This is optional
func (b *WebhookBuilder) FailurePolicy(policy admissionregistrationv1beta1.FailurePolicyType) *WebhookBuilder {
	b.failurePolicy = &policy
	return b
}

// NamespaceSelector sets the NamespaceSelector for the webhook.
// This is optional
func (b *WebhookBuilder) NamespaceSelector(namespaceSelector *metav1.LabelSelector) *WebhookBuilder {
	b.namespaceSelector = namespaceSelector
	return b
}

// WithManager set the manager for the webhook for provisioning various dependencies. e.g. client etc.
func (b *WebhookBuilder) WithManager(mgr manager.Manager) *WebhookBuilder {
	b.manager = mgr
	return b
}

// Handlers sets the handlers of the webhook.
func (b *WebhookBuilder) Handlers(handlers ...admission.Handler) *WebhookBuilder {
	b.handlers = handlers
	return b
}

func (b *WebhookBuilder) validate() error {
	if b.t == nil {
		return errors.New("webhook type cannot be nil")
	}
	if b.rules == nil && b.apiType == nil {
		return fmt.Errorf("ForType should be set")
	}
	if b.rules != nil && b.apiType != nil {
		return fmt.Errorf("at most one of ForType and Rules can be set")
	}
	return nil
}

// Build creates the Webhook based on the options provided.
func (b *WebhookBuilder) Build() (*admission.Webhook, error) {
	err := b.validate()
	if err != nil {
		return nil, err
	}

	w := &admission.Webhook{
		Name:              b.name,
		Type:              *b.t,
		Path:              b.path,
		FailurePolicy:     b.failurePolicy,
		NamespaceSelector: b.namespaceSelector,
		Handlers:          b.handlers,
	}

	if b.rules != nil {
		w.Rules = b.rules
	} else {
		if b.manager == nil {
			return nil, errors.New("manager should be set using WithManager")
		}
		gvk, err := apiutil.GVKForObject(b.apiType, b.manager.GetScheme())
		if err != nil {
			return nil, err
		}
		mapper := b.manager.GetRESTMapper()
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, err
		}

		if b.operations == nil {
			b.operations = []admissionregistrationv1beta1.OperationType{
				admissionregistrationv1beta1.Create,
				admissionregistrationv1beta1.Update,
			}
		}
		w.Rules = []admissionregistrationv1beta1.RuleWithOperations{
			{
				Operations: b.operations,
				Rule: admissionregistrationv1beta1.Rule{
					APIGroups:   []string{gvk.Group},
					APIVersions: []string{gvk.Version},
					Resources:   []string{mapping.Resource.Resource},
				},
			},
		}
	}

	return w, nil
}
