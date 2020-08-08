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

package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	kubeApiAdmission "k8s.io/api/admissionregistration/v1beta1"
	kubeApiCore "k8s.io/api/core/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	kubeTypedAdmission "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/filewatcher"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/testcerts"
)

var (
	failurePolicyFail   = kubeApiAdmission.Fail
	failurePolicyIgnore = kubeApiAdmission.Ignore

	istiodEndpoint = &kubeApiCore.Endpoints{
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name:      istiod,
			Namespace: namespace,
		},
		Subsets: []kubeApiCore.EndpointSubset{{
			Addresses: []kubeApiCore.EndpointAddress{{
				IP: "192.168.1.1",
			}},
		}},
	}

	unpatchedWebhookConfig = &kubeApiAdmission.ValidatingWebhookConfiguration{
		TypeMeta: kubeApiMeta.TypeMeta{
			APIVersion: kubeApiAdmission.SchemeGroupVersion.String(),
			Kind:       "ValidatingWebhookConfiguration",
		},
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name: istiod,
		},
		Webhooks: []kubeApiAdmission.ValidatingWebhook{{
			Name: "hook0",
			ClientConfig: kubeApiAdmission.WebhookClientConfig{Service: &kubeApiAdmission.ServiceReference{
				Namespace: namespace,
				Name:      istiod,
				Path:      &[]string{"/hook0"}[0],
			}},
			Rules: []kubeApiAdmission.RuleWithOperations{{
				Operations: []kubeApiAdmission.OperationType{kubeApiAdmission.Create, kubeApiAdmission.Update},
				Rule: kubeApiAdmission.Rule{
					APIGroups:   []string{"group0"},
					APIVersions: []string{"*"},
					Resources:   []string{"*"},
				},
			}},
			FailurePolicy: &failurePolicyIgnore,
		}, {
			Name: "hook1",
			ClientConfig: kubeApiAdmission.WebhookClientConfig{Service: &kubeApiAdmission.ServiceReference{
				Namespace: namespace,
				Name:      istiod,
				Path:      &[]string{"/hook1"}[0],
			}},
			Rules: []kubeApiAdmission.RuleWithOperations{{
				Operations: []kubeApiAdmission.OperationType{kubeApiAdmission.Create, kubeApiAdmission.Update},
				Rule: kubeApiAdmission.Rule{
					APIGroups:   []string{"group1"},
					APIVersions: []string{"*"},
					Resources:   []string{"*"},
				},
			}},
			FailurePolicy: &failurePolicyIgnore,
		}},
	}

	webhookConfigEncoded            string
	webhookConfigWithCABundleFail   *kubeApiAdmission.ValidatingWebhookConfiguration
	webhookConfigWithCABundleIgnore *kubeApiAdmission.ValidatingWebhookConfiguration

	caBundle0 = []byte(`-----BEGIN CERTIFICATE-----
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

	caBundle1 = []byte(`-----BEGIN CERTIFICATE-----
MIIDCzCCAfOgAwIBAgIQbfOzhcKTldFipQ1X2WXpHDANBgkqhkiG9w0BAQsFADAv
MS0wKwYDVQQDEyRhNzU5YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUw
HhcNMTkwNTE2MjIxMTI2WhcNMjQwNTE0MjMxMTI2WjAvMS0wKwYDVQQDEyRhNzU5
YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQC6sSAN80Ci0DYFpNDumGYoejMQai42g6nSKYS+ekvs
E7uT+eepO74wj8o6nFMNDu58+XgIsvPbWnn+3WtUjJfyiQXxmmTg8om4uY1C7R1H
gMsrL26pUaXZ/lTE8ZV5CnQJ9XilagY4iZKeptuZkxrWgkFBD7tr652EA3hmj+3h
4sTCQ+pBJKG8BJZDNRrCoiABYBMcFLJsaKuGZkJ6KtxhQEO9QxJVaDoSvlCRGa8R
fcVyYQyXOZ+0VHZJQgaLtqGpiQmlFttpCwDiLfMkk3UAd79ovkhN1MCq+O5N7YVt
eVQWaTUqUV2tKUFvVq21Zdl4dRaq+CF5U8uOqLY/4Kg9AgMBAAGjIzAhMA4GA1Ud
DwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQCg
oF71Ey2b1QY22C6BXcANF1+wPzxJovFeKYAnUqwh3rF7pIYCS/adZXOKlgDBsbcS
MxAGnCRi1s+A7hMYj3sQAbBXttc31557lRoJrx58IeN5DyshT53t7q4VwCzuCXFT
3zRHVRHQnO6LHgZx1FuKfwtkhfSXDyYU2fQYw2Hcb9krYU/alViVZdE0rENXCClq
xO7AQk5MJcGg6cfE5wWAKU1ATjpK4CN+RTn8v8ODLoI2SW3pfsnXxm93O+pp9HN4
+O+1PQtNUWhCfh+g6BN2mYo2OEZ8qGSxDlMZej4YOdVkW8PHmFZTK0w9iJKqM5o1
V6g5gZlqSoRhICK09tpc
-----END CERTIFICATE-----`)
)

// patch the caBundle into the final istiod and galley configs.
func init() {
	webhookConfigEncoded = runtime.EncodeOrDie(codec, unpatchedWebhookConfig)

	webhookConfigWithCABundleIgnore = unpatchedWebhookConfig.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	webhookConfigWithCABundleIgnore.Webhooks[0].ClientConfig.CABundle = caBundle0
	webhookConfigWithCABundleIgnore.Webhooks[1].ClientConfig.CABundle = caBundle0

	webhookConfigWithCABundleFail = webhookConfigWithCABundleIgnore.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	webhookConfigWithCABundleFail.Webhooks[0].FailurePolicy = &failurePolicyFail
	webhookConfigWithCABundleFail.Webhooks[1].FailurePolicy = &failurePolicyFail
}

type fakeController struct {
	*Controller

	endpointStore cache.Store
	configStore   cache.Store

	caChangedCh chan bool

	injectedMu       sync.Mutex
	injectedCABundle []byte

	fakeWatcher *filewatcher.FakeWatcher
	*fake.Clientset
	dFakeClient     *dfake.FakeDynamicClient
	reconcileDoneCh chan struct{}
	client          kube.Client
}

const (
	namespace = "istio-system"
	istiod    = "istiod"
	caPath    = "fakeCAPath"
)

func createTestController(t *testing.T) *fakeController {
	fakeClient := kube.NewFakeClient()
	o := Options{
		WatchedNamespace:  namespace,
		ResyncPeriod:      time.Minute,
		CAPath:            caPath,
		WebhookConfigName: istiod,
		ServiceName:       istiod,
	}

	caChanged := make(chan bool, 10)
	changed := func(path string, added bool) {
		switch path {
		case o.CAPath:
			caChanged <- added
		}
	}

	newFileWatcher, fakeWatcher := filewatcher.NewFakeWatcher(changed)

	fc := &fakeController{
		caChangedCh:      caChanged,
		injectedCABundle: caBundle0,
		fakeWatcher:      fakeWatcher,
		client:           fakeClient,
		Clientset:        fakeClient.Kube().(*fake.Clientset),
		dFakeClient:      fakeClient.Dynamic().(*dfake.FakeDynamicClient),
		reconcileDoneCh:  make(chan struct{}, 100),
	}

	readFile := func(filename string) ([]byte, error) {
		fc.injectedMu.Lock()
		defer fc.injectedMu.Unlock()

		switch filename {
		case o.CAPath:
			return fc.injectedCABundle, nil
		}
		return nil, os.ErrNotExist
	}

	reconcileDone := func() {
		select {
		case fc.reconcileDoneCh <- struct{}{}:
		default:
			t.Fatal("reconcile completion channel is stuck")
		}
	}

	var err error
	fc.Controller, err = newController(o, fakeClient, newFileWatcher, readFile, reconcileDone)
	if err != nil {
		t.Fatalf("failed to create test controller: %v", err)
	}
	fakeClient.RunAndWait(make(chan struct{}))

	fc.endpointStore = fakeClient.KubeInformer().Core().V1().Endpoints().Informer().GetStore()
	fc.configStore = fakeClient.KubeInformer().Admissionregistration().V1beta1().ValidatingWebhookConfigurations().Informer().GetStore()

	return fc
}

func (fc *fakeController) ValidatingWebhookConfigurations() kubeTypedAdmission.ValidatingWebhookConfigurationInterface {
	return fc.client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
}

func reconcileHelper(t *testing.T, c *fakeController) {
	t.Helper()

	c.ClearActions()
	if err := c.reconcileRequest(&reconcileRequest{"test"}); err != nil {
		t.Fatalf("unexpected reconciliation error: %v", err)
	}
}

func TestGreenfield(t *testing.T) {
	g := NewGomegaWithT(t)
	c := createTestController(t)

	// install adds the webhook config with fail open policy
	_, _ = c.ValidatingWebhookConfigurations().Create(context.TODO(), unpatchedWebhookConfig, kubeApiMeta.CreateOptions{})
	_ = c.configStore.Add(unpatchedWebhookConfig)

	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(context.TODO(), istiod, kubeApiMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundleIgnore), "no config update when endpoint not present")

	_ = c.endpointStore.Add(istiodEndpoint)

	// verify the webhook isn't updated if invalid config is accepted.
	c.dFakeClient.PrependReactor("create", "gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{}, nil
	})
	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(context.TODO(), istiod, kubeApiMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundleIgnore), "no config update when endpoint invalid config is accepted")

	// verify the webhook is updated after the controller can confirm invalid config is rejected.
	c.dFakeClient.PrependReactor("create", "gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{}, kubeErrors.NewInternalError(errors.New("unknown error"))
	})
	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(context.TODO(), istiod, kubeApiMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundleIgnore),
			"no config update when endpoint invalid config is rejected for an unknown reason")

	// verify the webhook is updated after the controller can confirm invalid config is rejected.
	c.dFakeClient.PrependReactor("create", "gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{}, kubeErrors.NewInternalError(errors.New(deniedRequestMessageFragment))
	})
	reconcileHelper(t, c)
	g.Expect(c.Actions()[0].Matches("update", "validatingwebhookconfigurations")).Should(BeTrue())
	g.Expect(c.ValidatingWebhookConfigurations().Get(context.TODO(), istiod, kubeApiMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundleFail),
			"istiod config created when endpoint is ready and invalid config is denied")
}

func TestCABundleChange(t *testing.T) {
	g := NewGomegaWithT(t)
	c := createTestController(t)

	_, _ = c.ValidatingWebhookConfigurations().Create(context.TODO(), unpatchedWebhookConfig, kubeApiMeta.CreateOptions{})
	_ = c.configStore.Add(unpatchedWebhookConfig)
	_ = c.endpointStore.Add(istiodEndpoint)
	c.dFakeClient.PrependReactor("create", "gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.Unstructured{}, kubeErrors.NewInternalError(errors.New(deniedRequestMessageFragment))
	})
	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(context.TODO(), istiod, kubeApiMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundleFail), "istiod config created when endpoint is ready")
	// keep test store and tracker in-sync
	_ = c.configStore.Add(webhookConfigWithCABundleFail)

	// verify the config updates after injecting a cafile change
	c.injectedMu.Lock()
	c.injectedCABundle = caBundle1
	c.injectedMu.Unlock()

	webhookConfigAfterCAUpdate := webhookConfigWithCABundleFail.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	webhookConfigAfterCAUpdate.Webhooks[0].ClientConfig.CABundle = caBundle1
	webhookConfigAfterCAUpdate.Webhooks[1].ClientConfig.CABundle = caBundle1

	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(context.TODO(), istiod, kubeApiMeta.GetOptions{})).
		Should(Equal(webhookConfigAfterCAUpdate), "webhook should change after cert change")
	// keep test store and tracker in-sync
	_ = c.configStore.Update(webhookConfigAfterCAUpdate)
}

func TestLoadCaCertPem(t *testing.T) {
	cases := []struct {
		name      string
		cert      []byte
		wantError bool
	}{
		{
			name:      "valid pem",
			cert:      testcerts.CACert,
			wantError: false,
		},
		{
			name:      "pem decode error",
			cert:      append([]byte("-----codec"), testcerts.CACert...),
			wantError: true,
		},
		{
			name:      "pem wrong type",
			wantError: true,
		},
		{
			name:      "invalid x509",
			cert:      testcerts.BadCert,
			wantError: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %s", i, c.name), func(tt *testing.T) {
			err := verifyCABundle(c.cert)
			if err != nil {
				if !c.wantError {
					tt.Fatalf("unexpected error: got error %q", err)
				}
			} else {
				if c.wantError {
					tt.Fatal("expected error")
				}
			}
		})
	}
}
