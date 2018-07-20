// Copyright 2018 Istio Authors
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

package validation

import (
	"reflect"
	"testing"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/crd/validation/testcerts"
)

var (
	failurePolicyFailVal = admissionregistrationv1beta1.Fail
	failurePolicyFail    = &failurePolicyFailVal
)

func TestValidatingWebhookConfig(t *testing.T) {
	want := &admissionregistrationv1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config1",
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(
					dummyDeployment,
					schema.GroupVersionKind{
						Group:   "extensions",
						Version: "v1beta1",
						Kind:    "Deployment",
					},
				),
			},
		},
		Webhooks: []admissionregistrationv1beta1.Webhook{
			{
				Name: "hook-foo",
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Name:      "hook1",
						Namespace: "default",
					},
					CABundle: testcerts.CACert,
				},
				Rules: []admissionregistrationv1beta1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
						},
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{"g1"},
							APIVersions: []string{"v1"},
							Resources:   []string{"r1"},
						},
					},
					{
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
						},
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{"g2"},
							APIVersions: []string{"v2"},
							Resources:   []string{"r2"},
						},
					},
				},
				FailurePolicy:     failurePolicyFail,
				NamespaceSelector: &metav1.LabelSelector{},
			},
			{
				Name: "hook-bar",
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Name:      "hook2",
						Namespace: "default",
					},
					CABundle: testcerts.CACert,
				},
				Rules: []admissionregistrationv1beta1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
						},
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{"g3"},
							APIVersions: []string{"v3"},
							Resources:   []string{"r3"},
						},
					},
					{
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
						},
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{"g4"},
							APIVersions: []string{"v4"},
							Resources:   []string{"r4"},
						},
					},
				},
				FailurePolicy:     failurePolicyFail,
				NamespaceSelector: &metav1.LabelSelector{},
			},
		},
	}

	ts := []struct {
		name    string
		configs admissionregistrationv1beta1.ValidatingWebhookConfigurationList
		desired *admissionregistrationv1beta1.ValidatingWebhookConfiguration
		updated bool
	}{
		{
			name:    "WebhookConfigNotFound",
			configs: admissionregistrationv1beta1.ValidatingWebhookConfigurationList{},
			updated: true,
		},
		{
			name: "WebhookEntryNotFound",
			configs: admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.ValidatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "config1",
						},
					},
				},
			},
			desired: want,
			updated: true,
		},
		{
			name: "SuccessfullyPatched",
			configs: admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.ValidatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "config1",
						},
						Webhooks: []admissionregistrationv1beta1.Webhook{
							{
								Name:         "webhook1",
								ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{},
							},
						},
					},
				},
			},
			desired: want,
			updated: true,
		},
		{
			name: "MultipleWebhookEntryNotFound",
			configs: admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.ValidatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "config1",
						},
						Webhooks: []admissionregistrationv1beta1.Webhook{
							{
								Name:         "webhook1",
								ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{},
							},
						},
					},
				},
			},
			desired: want,
			updated: true,
		},
		{
			name: "MultipleSuccessfullyPatched",
			configs: admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.ValidatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "config1",
						},
						Webhooks: []admissionregistrationv1beta1.Webhook{
							{
								Name:         "webhook1",
								ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{},
							},
							{
								Name:         "webhook2",
								ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{},
							},
						},
					},
				},
			},
			desired: want,
			updated: true,
		},
		{
			name: "No change",
			configs: admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.ValidatingWebhookConfiguration{*want},
			},
			desired: want,
			updated: false,
		},
		{
			name: "No change with missing defaults",
			configs: admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.ValidatingWebhookConfiguration{*want},
			},
			desired: missingDefaults,
			updated: false,
		},
	}

	for _, tc := range ts {
		t.Run(tc.name, func(t *testing.T) {
			wh, cancel := createTestWebhook(t, fake.NewSimpleClientset(dummyDeployment, tc.configs.DeepCopyObject()), want)
			defer cancel()

			client := fake.NewSimpleClientset(tc.configs.DeepCopyObject())
			config, _, err := rebuildWebhookConfigurationHelper(wh.caFile, wh.webhookConfigFile, wh.ownerRefs, wh.caCertPem)
			if err != nil {
				t.Fatalf("Got unexpected error: %v", err)
			}

			// not set by create/update
			config.Name = want.Name

			validateClient := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
			updated, err := reconcileWebhookConfigurationHelper(validateClient, config)
			if err != nil {
				t.Fatalf("reconcileWebhookConfigurationHelper failed: %v", err)
			}

			if tc.updated != updated {
				t.Fatalf("incorrect config update: got %v want %v", updated, tc.updated)
			}

			if tc.updated {
				wantActions := 2
				actions := client.Actions()
				if len(actions) != wantActions {
					t.Fatalf("unexpected number of k8s actions: got %v want %v", len(actions), wantActions)
				}

				switch action := actions[1].(type) {
				case k8stesting.UpdateActionImpl:
					got := action.GetObject().(*admissionregistrationv1beta1.ValidatingWebhookConfiguration)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("Got incorrect update webhook configuration: \ngot %#v \nwant %#v",
							got, want)
					}
				case k8stesting.CreateActionImpl:
					got := action.GetObject().(*admissionregistrationv1beta1.ValidatingWebhookConfiguration)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("Got incorrect create webhook configuration: \ngot %#v \nwant %#v",
							got, want)
					}
				}
			}
		})
	}
}
