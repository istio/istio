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

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

var caBundle0 = []byte(`-----BEGIN CERTIFICATE-----
MIIC9DCCAdygAwIBAgIJAIFe3lWPaalKMA0GCSqGSIb3DQEBCwUAMA4xDDAKBgNV
BAMMA19jYTAgFw0xNzEyMjIxODA0MjRaGA8yMjkxMTAwNzE4MDQyNFowDjEMMAoG
A1UEAwwDX2NhMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAuBdxj+Hi
8h0TkId1f64TprLydwgzzLwXAs3wpmXz+BfnW1oMQPNyN7vojW6VzqJGGYLsc1OB
MgwObU/VeFNc6YUCmu6mfFJwoPfXMPnhmGuSwf/kjXomlejAYjxClU3UFVWQht54
xNLjTi2M1ZOnwNbECOhXC3Tw3G8mCtfanMAO0UXM5yObbPa8yauUpJKkpoxWA7Ed
qiuUD9qRxluFPqqw/z86V8ikmvnyjQE9960j+8StlAbRs82ArtnrhRgkDO0Smtf7
4QZsb/hA1KNMm73bOGS6+SVU+eH8FgVOzcTQYFRpRT3Mhi6dKZe9twIO8mpZK4wk
uygRxBM32Ag9QQIDAQABo1MwUTAdBgNVHQ4EFgQUc8tvoNNBHyIkoVV8XCXy63Ya
BEQwHwYDVR0jBBgwFoAUc8tvoNNBHyIkoVV8XCXy63YaBEQwDwYDVR0TAQH/BAUw
AwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAVmaUkkYESfcfgnuPeZ4sTNs2nk2Y+Xpd
lxkMJhChb8YQtlCe4uiLvVe7er1sXcBLNCm/+2K9AT71gnxBSeS5mEOzWmCPErhy
RmYtSxeRyXAaUWVYLs/zMlBQ0Iz4dpY+FVVbMjIurelVwHF0NBk3VtU5U3lHyKdZ
j4C2rMjvTxmkyIcR1uBEeVvuGU8R70nZ1yfo3vDwmNGMcLwW+4QK+WcfwfjLXhLs
5550arfEYdTzYFMxY60HJT/LvbGrjxY0PQUWWDbPiRfsdRjOFduAbM0/EVRda/Oo
Fg72WnHeojDUhqEz4UyFZbnRJ4x6leQhnrIcVjWX4FFFktiO9rqqfw==
-----END CERTIFICATE-----`)

func TestMutatingWebhookPatch(t *testing.T) {
	testRevision := "test-revision"
	wrongRevision := "wrong-revision"
	testRevisionLabel := map[string]string{label.IoIstioRev.Name: testRevision}
	wrongRevisionLabel := map[string]string{label.IoIstioRev.Name: wrongRevision}
	watcher := &keycertbundle.Watcher{}
	watcher.SetAndNotify(nil, nil, caBundle0)
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
			caBundle0,
			errNotFound.Error(),
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
			caBundle0,
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
			caBundle0,
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
			caBundle0,
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
			caBundle0,
			errNotFound.Error(),
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
			caBundle0,
			errNotFound.Error(),
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
			caBundle0,
			"",
		},
	}
	for _, tc := range ts {
		t.Run(tc.name, func(t *testing.T) {
			client := kube.NewFakeClient()
			for _, wh := range tc.configs.Items {
				if _, err := client.Kube().AdmissionregistrationV1().
					MutatingWebhookConfigurations().Create(context.Background(), wh.DeepCopy(), metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}

			watcher := keycertbundle.NewWatcher()
			watcher.SetAndNotify(nil, nil, tc.pemData)
			whPatcher, err := NewWebhookCertPatcher(client, tc.revision, tc.webhookName, watcher)
			if err != nil {
				t.Fatal(err)
			}

			stop := test.NewStop(t)
			go whPatcher.informer.Run(stop)
			client.RunAndWait(stop)
			retry.UntilOrFail(t, whPatcher.informer.HasSynced)

			err = whPatcher.patchMutatingWebhookConfig(
				client.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations(),
				tc.configName)
			if (err != nil) != (tc.err != "") {
				t.Fatalf("Wrong error: got %v want %v", err, tc.err)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("Got %q, want %q", err, tc.err)
				}
			} else {
				obj, err := client.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), tc.configName, metav1.GetOptions{})
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
