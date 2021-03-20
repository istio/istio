// Copyright Istio Authors
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

package webhooks

import (
	"bytes"
	"context"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/api/label"
)

func TestMutatingWebhookPatch(t *testing.T) {
	testRevision := "test-revision"
	wrongRevision := "wrong-revision"
	testRevisionLabel := map[string]string{label.IoIstioRev.Name: testRevision}
	wrongRevisionLabel := map[string]string{label.IoIstioRev.Name: wrongRevision}
	ts := []struct {
		name        string
		configs     admissionregistrationv1.MutatingWebhookConfigurationList
		revision    string
		configName  string
		webhookName string
		pemData     []byte
		err         string
	}{
		{
			"WebhookConfigNotFound",
			admissionregistrationv1.MutatingWebhookConfigurationList{},
			testRevision,
			"config1",
			"webhook1",
			[]byte("fake CA"),
			"\"config1\" not found",
		},
		{
			"WebhookEntryNotFound",
			admissionregistrationv1.MutatingWebhookConfigurationList{
				Items: []admissionregistrationv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "config1",
							Labels: testRevisionLabel,
						},
					},
				},
			},
			testRevision,
			"config1",
			"webhook1",
			[]byte("fake CA"),
			errNoWebhookWithName.Error(),
		},
		{
			"SuccessfullyPatched",
			admissionregistrationv1.MutatingWebhookConfigurationList{
				Items: []admissionregistrationv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "config1",
							Labels: testRevisionLabel,
						},
						Webhooks: []admissionregistrationv1.MutatingWebhook{
							{
								Name:         "webhook1",
								ClientConfig: admissionregistrationv1.WebhookClientConfig{},
							},
						},
					},
				},
			},
			testRevision,
			"config1",
			"webhook1",
			[]byte("fake CA"),
			"",
		},
		{
			"Prefix",
			admissionregistrationv1.MutatingWebhookConfigurationList{
				Items: []admissionregistrationv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "config1",
							Labels: testRevisionLabel,
						},
						Webhooks: []admissionregistrationv1.MutatingWebhook{
							{
								Name:         "prefix.webhook1",
								ClientConfig: admissionregistrationv1.WebhookClientConfig{},
							},
						},
					},
				},
			},
			testRevision,
			"config1",
			"webhook1",
			[]byte("fake CA"),
			"",
		},
		{
			"NoRevisionWebhookNotUpdated",
			admissionregistrationv1.MutatingWebhookConfigurationList{
				Items: []admissionregistrationv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "config1",
						},
						Webhooks: []admissionregistrationv1.MutatingWebhook{
							{
								Name:         "webhook1",
								ClientConfig: admissionregistrationv1.WebhookClientConfig{},
							},
						},
					},
				},
			},
			testRevision,
			"config1",
			"webhook1",
			[]byte("fake CA"),
			errWrongRevision.Error(),
		},
		{
			"WrongRevisionWebhookNotUpdated",
			admissionregistrationv1.MutatingWebhookConfigurationList{
				Items: []admissionregistrationv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "config1",
							Labels: wrongRevisionLabel,
						},
						Webhooks: []admissionregistrationv1.MutatingWebhook{
							{
								Name:         "webhook1",
								ClientConfig: admissionregistrationv1.WebhookClientConfig{},
							},
						},
					},
				},
			},
			testRevision,
			"config1",
			"webhook1",
			[]byte("fake CA"),
			errWrongRevision.Error(),
		},
		{
			"MultipleWebhooks",
			admissionregistrationv1.MutatingWebhookConfigurationList{
				Items: []admissionregistrationv1.MutatingWebhookConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "config1",
							Labels: testRevisionLabel,
						},
						Webhooks: []admissionregistrationv1.MutatingWebhook{
							{
								Name:         "webhook1",
								ClientConfig: admissionregistrationv1.WebhookClientConfig{},
							},
							{
								Name:         "should not be changed",
								ClientConfig: admissionregistrationv1.WebhookClientConfig{},
							},
						},
					},
				},
			},
			testRevision,
			"config1",
			"webhook1",
			[]byte("fake CA"),
			"",
		},
	}
	for _, tc := range ts {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tc.configs.DeepCopyObject())
			whPatcher := WebhookCertPatcher{
				client:      client,
				revision:    tc.revision,
				webhookName: tc.webhookName,
				caCertPem:   tc.pemData,
			}

			err := whPatcher.patchMutatingWebhookConfig(client.AdmissionregistrationV1().MutatingWebhookConfigurations(),
				tc.configName)
			if (err != nil) != (tc.err != "") {
				t.Fatalf("Wrong error: got %v want %v", err, tc.err)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("Got %q, want %q", err, tc.err)
				}
			} else {
				obj, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), tc.configName, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				for _, w := range obj.Webhooks {
					if strings.HasSuffix(w.Name, tc.webhookName) {
						if !bytes.Equal(w.ClientConfig.CABundle, tc.pemData) {
							t.Fatalf("Incorrect CA bundle: expect %s got %s", tc.pemData, w.ClientConfig.CABundle)
						}
					}
					if !strings.HasSuffix(w.Name, tc.webhookName) {
						if bytes.Equal(w.ClientConfig.CABundle, tc.pemData) {
							t.Fatalf("Non-matching webhook \"%s\" CA bundle updated to %v", w.Name, w.ClientConfig.CABundle)
						}
					}
				}
			}
		})
	}
}
