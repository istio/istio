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

func TestValidatingWebhookPatch(t *testing.T) {
	ts := []struct {
		name         string
		configs      admissionregistrationv1beta1.ValidatingWebhookConfigurationList
		configName   string
		webhookNames []string
		pemData      []byte
		err          string
	}{
		{
			"WebhookConfigNotFound",
			admissionregistrationv1beta1.ValidatingWebhookConfigurationList{},
			"config1",
			[]string{"webhook1"},
			[]byte("fake CA"),
			`"config1" not found`,
		},
		{
			"WebhookEntryNotFound",
			admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
				Items: []admissionregistrationv1beta1.ValidatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "config1",
						},
					},
				},
			},
			"config1",
			[]string{"webhook1"},
			[]byte("fake CA"),
			`webhook entries ["webhook1"] not found in config "config1"`,
		},
		{
			"SuccessfullyPatched",
			admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
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
			"config1",
			[]string{"webhook1"},
			[]byte("fake CA"),
			"",
		},
		{
			"MultipleWebhookEntryNotFound",
			admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
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
			"config1",
			[]string{"webhook1", "webhook2"},
			[]byte("fake CA"),
			`webhook entries ["webhook1" "webhook2"] not found in config "config1"`,
		},
		{
			"MultipleSuccessfullyPatched",
			admissionregistrationv1beta1.ValidatingWebhookConfigurationList{
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
			"config1",
			[]string{"webhook1", "webhook2"},
			[]byte("fake CA"),
			"",
		},
	}
	for _, tc := range ts {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tc.configs.DeepCopyObject())
			err := PatchValidatingWebhookConfig(client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations(),
				tc.configName, tc.webhookNames, tc.pemData)
			if (err != nil) != (tc.err != "") {
				t.Fatalf("Wrong error: got %v want %v", err, tc.err)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("Got %q, want %q", err, tc.err)
				}
			} else {
				config := admissionregistrationv1beta1.ValidatingWebhookConfiguration{}
				patch := client.Actions()[1].(k8stesting.PatchAction).GetPatch()
				err = json.Unmarshal(patch, &config)
				if err != nil {
					t.Fatalf("Fail to parse the patch: %s", err.Error())
				}
				for i := 0; i < len(config.Webhooks); i++ {
					if !bytes.Equal(config.Webhooks[i].ClientConfig.CABundle, tc.pemData) {
						t.Fatalf("Incorrect CA bundle for client %v: expect %s got %s",
							i, tc.pemData, config.Webhooks[i].ClientConfig.CABundle)
					}
				}
			}
		})
	}
}
