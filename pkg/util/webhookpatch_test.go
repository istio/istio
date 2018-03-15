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
	"testing"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestWebhookPatch(t *testing.T) {
	ts := []struct {
		configs     admissionregistrationv1beta1.MutatingWebhookConfigurationList
		configName  string
		webhookName string
		pemData     []byte
		expected    string
	}{
		{
			admissionregistrationv1beta1.MutatingWebhookConfigurationList{},
			"config1",
			"webhook1",
			[]byte("fake CA"),
			"mutatingwebhookconfigurations.admissionregistration.k8s.io \"config1\" not found",
		},
		{
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
			"webhook entry not found in config",
		},
		{
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
		client := fake.NewSimpleClientset(tc.configs.DeepCopyObject())
		err := PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
			tc.configName, tc.webhookName, tc.pemData)
		if err != nil {
			if err.Error() != tc.expected {
				t.Errorf("Expected %s, Got %s", tc.expected, err)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error: %s", err)
			} else {
				config := admissionregistrationv1beta1.MutatingWebhookConfiguration{}
				patch := client.Actions()[1].(k8stesting.PatchAction).GetPatch()
				err = json.Unmarshal(patch, &config)
				if err != nil {
					t.Errorf(err.Error())
				}
				if !bytes.Equal(config.Webhooks[0].ClientConfig.CABundle, tc.pemData) {
					t.Errorf("Incorrect CA bundle: expect %s got %s", tc.pemData, config.Webhooks[0].ClientConfig.CABundle)
				}
			}
		}
	}
}
