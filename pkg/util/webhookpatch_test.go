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

package util

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestMutatingWebhookPatch(t *testing.T) {
	ts := []struct {
		name        string
		configs     admissionregistrationv1beta1.MutatingWebhookConfigurationList
		configName  string
		webhookName string
		pemData     []byte
		err         string
	}{
		{
			"WebhookConfigNotFound",
			admissionregistrationv1beta1.MutatingWebhookConfigurationList{},
			"config1",
			"webhook1",
			[]byte("fake CA"),
			"\"config1\" not found",
		},
		{
			"WebhookEntryNotFound",
			admissionregistrationv1beta1.MutatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "config1",
						},
					},
				},
			},
			"config1",
			"webhook1",
			[]byte("fake CA"),
			"webhook entry \"webhook1\" not found in config \"config1\"",
		},
		{
			"SuccessfullyPatched",
			admissionregistrationv1beta1.MutatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.MutatingWebhookConfiguration{
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
			"config1",
			"webhook1",
			[]byte("fake CA"),
			"",
		},
	}
	for _, tc := range ts {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tc.configs.DeepCopyObject())
			err := PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
				tc.configName, tc.webhookName, tc.pemData)
			if (err != nil) != (tc.err != "") {
				t.Fatalf("Wrong error: got %v want %v", err, tc.err)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("Got %q, want %q", err, tc.err)
				}
			} else {
				config := admissionregistrationv1beta1.MutatingWebhookConfiguration{}
				patch := client.Actions()[1].(k8stesting.PatchAction).GetPatch()
				err = json.Unmarshal(patch, &config)
				if err != nil {
					t.Fatalf("Fail to parse the patch: %s", err.Error())
				}
				if !bytes.Equal(config.Webhooks[0].ClientConfig.CABundle, tc.pemData) {
					t.Fatalf("Incorrect CA bundle: expect %s got %s", tc.pemData, config.Webhooks[0].ClientConfig.CABundle)
				}
			}
		})
	}
}

var (
	failurePolicyFailVal = admissionregistrationv1beta1.Fail
	failurePolicyFail    = &failurePolicyFailVal
)

func TestValidatingWebhookPatch(t *testing.T) {
	want := &admissionregistrationv1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config1",
		},
		Webhooks: []admissionregistrationv1beta1.Webhook{
			{
				Name: "hook-foo",
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Name:      "hook1",
						Namespace: "default",
					},
					CABundle: []byte{0, 1, 2, 3},
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
				FailurePolicy: failurePolicyFail,
			},
			{
				Name: "hook-bar",
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Name:      "hook2",
						Namespace: "default",
					},
					CABundle: []byte{0, 1, 2, 3},
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
				FailurePolicy: failurePolicyFail,
			},
		},
	}

	ts := []struct {
		name    string
		configs admissionregistrationv1beta1.ValidatingWebhookConfigurationList
	}{
		{
			name:    "WebhookConfigNotFound",
			configs: admissionregistrationv1beta1.ValidatingWebhookConfigurationList{},
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
		},
	}
	for _, tc := range ts {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tc.configs.DeepCopyObject())
			err := PatchValidatingWebhookConfig(client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations(), want)
			if err != nil {
				t.Fatalf("Got unexpected error: %v", err)
			}

			wantActions := 2
			actions := client.Actions()
			if len(actions) != wantActions {
				t.Fatalf("unexpected number of k8s actions: got %v want %v", actions, wantActions)
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
		})
	}
}
