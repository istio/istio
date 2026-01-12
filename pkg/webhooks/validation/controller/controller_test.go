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
	"errors"
	"fmt"
	"testing"
	"time"

	"go.uber.org/atomic"
	admission "k8s.io/api/admissionregistration/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	"istio.io/api/label"
	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/testcerts"
	"istio.io/istio/pkg/webhooks/util"
)

var (
	unpatchedWebhookConfig = &admission.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: admission.SchemeGroupVersion.String(),
			Kind:       "ValidatingWebhookConfiguration",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
			Labels: map[string]string{
				label.IoIstioRev.Name: revision,
			},
		},
		Webhooks: []admission.ValidatingWebhook{{
			Name: "hook0",
			ClientConfig: admission.WebhookClientConfig{Service: &admission.ServiceReference{
				Namespace: namespace,
				Name:      istiod,
				Path:      &[]string{"/hook0"}[0],
			}},
			Rules: []admission.RuleWithOperations{{
				Operations: []admission.OperationType{admission.Create, admission.Update},
				Rule: admission.Rule{
					APIGroups:   []string{"group0"},
					APIVersions: []string{"*"},
					Resources:   []string{"*"},
				},
			}},
			FailurePolicy: ptr.Of(admission.Ignore),
		}, {
			Name: "hook1",
			ClientConfig: admission.WebhookClientConfig{Service: &admission.ServiceReference{
				Namespace: namespace,
				Name:      istiod,
				Path:      &[]string{"/hook1"}[0],
			}},
			Rules: []admission.RuleWithOperations{{
				Operations: []admission.OperationType{admission.Create, admission.Update},
				Rule: admission.Rule{
					APIGroups:   []string{"group1"},
					APIVersions: []string{"*"},
					Resources:   []string{"*"},
				},
			}},
			FailurePolicy: ptr.Of(admission.Ignore),
		}},
	}

	webhookConfigWithCABundleFail   *admission.ValidatingWebhookConfiguration
	webhookConfigWithCABundleIgnore *admission.ValidatingWebhookConfiguration

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
	webhookConfigWithCABundleIgnore = unpatchedWebhookConfig.DeepCopyObject().(*admission.ValidatingWebhookConfiguration)
	webhookConfigWithCABundleIgnore.Webhooks[0].ClientConfig.CABundle = caBundle0
	webhookConfigWithCABundleIgnore.Webhooks[1].ClientConfig.CABundle = caBundle0

	webhookConfigWithCABundleFail = webhookConfigWithCABundleIgnore.DeepCopyObject().(*admission.ValidatingWebhookConfiguration)
	webhookConfigWithCABundleFail.Webhooks[0].FailurePolicy = ptr.Of(admission.Fail)
	webhookConfigWithCABundleFail.Webhooks[1].FailurePolicy = ptr.Of(admission.Fail)
}

const (
	istiod    = "istio-revision"
	namespace = "istio-system"
	revision  = "revision"
)

var webhookName = fmt.Sprintf("istio-validator-revision-%s", namespace)

func createTestController(t *testing.T) (*Controller, *atomic.Pointer[error]) {
	c := kube.NewFakeClient()
	revision := "default"
	ns := "default"
	watcher := keycertbundle.NewWatcher()
	watcher.SetAndNotify(nil, nil, caBundle0)
	control := newController(Options{
		WatchedNamespace: ns,
		CABundleWatcher:  watcher,
		Revision:         revision,
		ServiceName:      "istiod",
	}, c)
	stop := test.NewStop(t)
	c.RunAndWait(stop)
	go control.Run(stop)
	kube.WaitForCacheSync("test", stop, control.queue.HasSynced)

	gatewayError := setupGatewayError(c)

	return control, gatewayError
}

func unstartedTestController(c kube.Client) *Controller {
	revision := "default"
	ns := "default"
	watcher := keycertbundle.NewWatcher()
	watcher.SetAndNotify(nil, nil, caBundle0)
	control := newController(Options{
		WatchedNamespace: ns,
		CABundleWatcher:  watcher,
		Revision:         revision,
		ServiceName:      "istiod",
	}, c)
	return control
}

func setupGatewayError(c kube.Client) *atomic.Pointer[error] {
	gatewayError := atomic.NewPointer[error](nil)
	c.Istio().(*istiofake.Clientset).PrependReactor("*", "gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		e := gatewayError.Load()
		if e == nil {
			return true, &clientnetworking.Gateway{}, nil
		}
		return true, &clientnetworking.Gateway{}, *e
	})
	return gatewayError
}

func TestGreenfield(t *testing.T) {
	controllerStop := make(chan struct{})
	clientStop := test.NewStop(t)
	kc := kube.NewFakeClient()
	gatewayError := setupGatewayError(kc)
	c := unstartedTestController(kc)
	kc.RunAndWait(clientStop)
	go c.Run(controllerStop)
	webhooks := clienttest.Wrap(t, c.webhooks)
	fetch := func(name string) func() *admission.ValidatingWebhookConfiguration {
		return func() *admission.ValidatingWebhookConfiguration {
			return webhooks.Get(name, "")
		}
	}
	// install adds the webhook config with fail open policy
	webhooks.Create(unpatchedWebhookConfig)
	// verify the webhook isn't updated if invalid config is accepted.
	assert.EventuallyEqual(
		t,
		fetch(unpatchedWebhookConfig.Name),
		webhookConfigWithCABundleIgnore,
		retry.Message("no config update when endpoint not present"),
		LongRetry,
	)
	webhooks.Delete(unpatchedWebhookConfig.Name, "")

	// verify the webhook is updated after the controller can confirm invalid config is rejected.
	gatewayError.Store(ptr.Of[error](kerrors.NewInternalError(errors.New("unknown error"))))
	webhooks.Create(unpatchedWebhookConfig)
	assert.EventuallyEqual(
		t,
		fetch(unpatchedWebhookConfig.Name),
		webhookConfigWithCABundleIgnore,
		retry.Message("no config update when endpoint invalid config is rejected for an unknown reason"),
		LongRetry,
	)

	// verify the webhook is updated after the controller can confirm invalid config is rejected.
	gatewayError.Store(ptr.Of[error](kerrors.NewInternalError(errors.New(deniedRequestMessageFragment))))
	c.syncAll()
	assert.EventuallyEqual(
		t,
		fetch(unpatchedWebhookConfig.Name),
		webhookConfigWithCABundleFail,
		retry.Message("istiod config created when endpoint is ready and invalid config is denied"),
		LongRetry,
	)

	// If we start having issues, we should not flip back to Ignore
	gatewayError.Store(ptr.Of[error](kerrors.NewInternalError(errors.New("unknown error"))))
	c.syncAll()
	assert.EventuallyEqual(
		t,
		fetch(unpatchedWebhookConfig.Name),
		webhookConfigWithCABundleFail,
		retry.Message("should not go from Fail -> Ignore"),
		LongRetry,
	)

	// We also should not flip back to Ignore in a new instance
	close(controllerStop)
	_ = c.queue.WaitForClose(time.Second)
	controllerStop2 := test.NewStop(t)
	c2 := unstartedTestController(kc)
	go c2.Run(controllerStop2)
	assert.EventuallyEqual(
		t,
		fetch(unpatchedWebhookConfig.Name),
		webhookConfigWithCABundleFail,
		retry.Message("should not go from Fail -> Ignore"),
		LongRetry,
	)
	_ = c2
}

// TestCABundleChange ensures that we create request to update all webhooks when CA bundle changes.
func TestCABundleChange(t *testing.T) {
	c, gatewayError := createTestController(t)
	gatewayError.Store(ptr.Of[error](kerrors.NewInternalError(errors.New(deniedRequestMessageFragment))))
	webhooks := clienttest.Wrap(t, c.webhooks)
	fetch := func(name string) func() *admission.ValidatingWebhookConfiguration {
		return func() *admission.ValidatingWebhookConfiguration {
			return webhooks.Get(name, "")
		}
	}
	webhooks.Create(unpatchedWebhookConfig)
	assert.EventuallyEqual(
		t,
		fetch(unpatchedWebhookConfig.Name),
		webhookConfigWithCABundleFail,
		retry.Message("istiod config created when endpoint is ready"),
		LongRetry,
	)

	c.o.CABundleWatcher.SetAndNotify(nil, nil, caBundle1)
	webhookConfigAfterCAUpdate := webhookConfigWithCABundleFail.DeepCopyObject().(*admission.ValidatingWebhookConfiguration)
	webhookConfigAfterCAUpdate.Webhooks[0].ClientConfig.CABundle = caBundle1
	webhookConfigAfterCAUpdate.Webhooks[1].ClientConfig.CABundle = caBundle1
	assert.EventuallyEqual(
		t,
		fetch(unpatchedWebhookConfig.Name),
		webhookConfigAfterCAUpdate,
		retry.Message("webhook should change after cert change"),
		LongRetry,
	)
}

// LongRetry is used when comparing webhook values. Apparently the values are so large that with -race
// on the comparison can take a few seconds, meaning we never retry with the default settings.
var LongRetry = retry.Timeout(time.Second * 20)

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
			err := util.VerifyCABundle(c.cert)
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
