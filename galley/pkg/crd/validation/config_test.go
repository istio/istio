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
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/onsi/gomega"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/mcp/testing/testcerts"
)

var (
	failurePolicyFailVal = admissionregistrationv1beta1.Fail
	failurePolicyFail    = &failurePolicyFailVal
)

func createTestWebhookConfig(
	t testing.TB,
	cl clientset.Interface,
	fakeWebhookSource cache.ListerWatcher,
	config *admissionregistrationv1beta1.ValidatingWebhookConfiguration) (*WebhookConfig, func()) {

	t.Helper()
	dir, err := ioutil.TempDir("", "galley_validation_webhook")
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	cleanup := func() {
		os.RemoveAll(dir) // nolint: errcheck
	}

	var (
		certFile   = filepath.Join(dir, "cert-file.yaml")
		keyFile    = filepath.Join(dir, "key-file.yaml")
		caFile     = filepath.Join(dir, "ca-file.yaml")
		configFile = filepath.Join(dir, "config-file.yaml")
		port       = uint(0)
	)

	// cert
	if err := ioutil.WriteFile(certFile, testcerts.ServerCert, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", certFile, err)
	}
	// key
	if err := ioutil.WriteFile(keyFile, testcerts.ServerKey, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", keyFile, err)
	}
	// ca
	if err := ioutil.WriteFile(caFile, testcerts.CACert, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", caFile, err)
	}

	configBytes, err := yaml.Marshal(&config)
	if err != nil {
		cleanup()
		t.Fatalf("could not create fake webhook configuration data: %v", err)
	}
	if err := ioutil.WriteFile(configFile, configBytes, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", configFile, err)
	}

	options := WebhookParameters{
		CertFile:                      certFile,
		KeyFile:                       keyFile,
		Port:                          port,
		DomainSuffix:                  testDomainSuffix,
		WebhookConfigFile:             configFile,
		CACertFile:                    caFile,
		Clientset:                     cl,
		WebhookName:                   config.Name,
		DeploymentName:                dummyDeployment.Name,
		ServiceName:                   dummyDeployment.Name,
		DeploymentAndServiceNamespace: dummyDeployment.Namespace,
	}
	whc, err := NewWebhookConfig(options)
	if err != nil {
		cleanup()
		t.Fatalf("NewWebhookConfig() failed: %v", err)
	}

	whc.createInformerWebhookSource = func(cl clientset.Interface, name string) cache.ListerWatcher {
		return fakeWebhookSource
	}

	return whc, func() {
		cleanup()
	}
}

func TestValidatingWebhookConfig(t *testing.T) {
	want := initValidatingWebhookConfiguration()

	missingDefaults := want.DeepCopyObject().(*admissionregistrationv1beta1.ValidatingWebhookConfiguration)
	missingDefaults.Webhooks[0].NamespaceSelector = nil
	missingDefaults.Webhooks[0].FailurePolicy = nil

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
			whc, cancel := createTestWebhookConfig(t,
				fake.NewSimpleClientset(dummyDeployment, tc.configs.DeepCopyObject()),
				createFakeWebhookSource(), want)
			defer cancel()

			client := fake.NewSimpleClientset(tc.configs.DeepCopyObject())
			config, err := rebuildWebhookConfigHelper(whc.caFile, whc.webhookConfigFile, whc.webhookName, whc.ownerRefs)
			if err != nil {
				t.Fatalf("Got unexpected error: %v", err)
			}

			// not set by create/update
			config.Name = want.Name

			validateClient := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
			updated, err := createOrUpdateWebhookConfigHelper(validateClient, config)
			if err != nil {
				t.Fatalf("createOrUpdateWebhookConfigHelper failed: %v", err)
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

func initValidatingWebhookConfiguration() *admissionregistrationv1beta1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config1",
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(
					dummyDeployment,
					appsv1.SchemeGroupVersion.WithKind("Deployment"),
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
}

func checkCert(t *testing.T, whc *WebhookConfig, cert, key []byte) bool {
	t.Helper()
	actual := whc.cert
	expected, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("fail to load test certs.")
	}
	return bytes.Equal(actual.Certificate[0], expected.Certificate[0])
}

func TestReloadCert(t *testing.T) {
	whc, cleanup := createTestWebhookConfig(t,
		fake.NewSimpleClientset(),
		createFakeWebhookSource(),
		dummyConfig)
	defer cleanup()
	stop := make(chan struct{})
	defer func() { close(stop) }()
	go whc.reconcile(stop)
	checkCert(t, whc, testcerts.ServerCert, testcerts.ServerKey)
	// Update cert/key files.
	if err := ioutil.WriteFile(whc.certFile, testcerts.RotatedCert, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", whc.certFile, err)
	}
	if err := ioutil.WriteFile(whc.keyFile, testcerts.RotatedKey, 0644); err != nil { // nolint: vetshadow
		cleanup()
		t.Fatalf("WriteFile(%v) failed: %v", whc.keyFile, err)
	}
	g := gomega.NewGomegaWithT(t)
	g.Eventually(func() bool {
		return checkCert(t, whc, testcerts.RotatedCert, testcerts.RotatedKey)
	}, "10s", "100ms").Should(gomega.BeTrue())
}

func TestLoadCaCertPem(t *testing.T) {
	cases := []struct {
		name      string
		want      []byte
		wantError bool
	}{
		{
			name:      "valid pem",
			want:      testcerts.CACert,
			wantError: false,
		},
		{
			name:      "pem decode error",
			want:      append([]byte("-----foo"), testcerts.CACert...),
			wantError: true,
		},
		{
			name:      "pem wrong type",
			want:      []byte(strings.Replace(string(testcerts.CACert), "CERTIFICATE", "MALFORMED", -1)),
			wantError: true,
		},
		{
			name:      "invalid x509",
			want:      testcerts.BadCert,
			wantError: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %s", i, c.name), func(tt *testing.T) {
			got, err := loadCaCertPem(bytes.NewReader(c.want))
			if err != nil {
				if !c.wantError {
					tt.Fatalf("unexpected error: got error %q", err)
				}
			} else {
				if c.wantError {
					tt.Fatal("expected error")
				}
				if !reflect.DeepEqual(got, c.want) {
					tt.Fatalf("got wrong ca pem: \ngot %v \nwant %s", string(got), string(c.want))
				}
			}
		})
	}
}

func TestInitialConfigLoadError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("configuration should not panic on invalid configuration: %v", r)
		}
	}()

	whc, cleanup := createTestWebhookConfig(t,
		fake.NewSimpleClientset(),
		createFakeWebhookSource(),
		dummyConfig)
	defer cleanup()

	whc.webhookConfigFile = ""
	whc.webhookConfiguration = nil
	if err := whc.rebuildWebhookConfig(); err == nil {
		t.Fatal("unexpected success: rebuildWebhookConfig() should have failed given invalid config files")
	}
}
