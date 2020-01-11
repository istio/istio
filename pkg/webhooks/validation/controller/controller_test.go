// Copyright 2019 Istio Authors
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
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	kubeApiAdmission "k8s.io/api/admissionregistration/v1beta1"
	kubeApiApp "k8s.io/api/apps/v1"
	kubeApiCore "k8s.io/api/core/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeApisMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kubeTypedAdmission "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	kubeTypedApp "k8s.io/client-go/kubernetes/typed/apps/v1"
	kubeTypedCore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/pkg/filewatcher"

	"istio.io/istio/pkg/mcp/testing/testcerts"
)

var (
	istiodEndpoint = &kubeApiCore.Endpoints{
		ObjectMeta: kubeApisMeta.ObjectMeta{
			Name:      istiod,
			Namespace: namespace,
		},
		Subsets: []kubeApiCore.EndpointSubset{{
			Addresses: []kubeApiCore.EndpointAddress{{
				IP: "192.168.1.1",
			}},
		}},
	}

	galleyDeployment = &kubeApiApp.Deployment{
		ObjectMeta: kubeApisMeta.ObjectMeta{
			Name:      galley,
			Namespace: namespace,
		},
		Spec: kubeApiApp.DeploymentSpec{
			Replicas: &[]int32{1}[0],
		},
	}

	unpatchedIstiodWebhookConfig = &kubeApiAdmission.ValidatingWebhookConfiguration{
		TypeMeta: kubeApisMeta.TypeMeta{
			APIVersion: kubeApiAdmission.SchemeGroupVersion.String(),
			Kind:       "ValidatingWebhookConfiguration",
		},
		ObjectMeta: kubeApisMeta.ObjectMeta{
			Name: galleyWebhookName,
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
			FailurePolicy:     &defaultFailurePolicy,
			NamespaceSelector: &kubeApiMeta.LabelSelector{},
			ObjectSelector:    &kubeApiMeta.LabelSelector{},
			SideEffects:       &defaultSideEffects,
			TimeoutSeconds:    &defaultTimeout,
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
			FailurePolicy:     &defaultFailurePolicy,
			NamespaceSelector: &kubeApiMeta.LabelSelector{},
			ObjectSelector:    &kubeApiMeta.LabelSelector{},
			SideEffects:       &defaultSideEffects,
			TimeoutSeconds:    &defaultTimeout,
		}},
	}

	istiodWebhookConfigEncoded       string
	webhookConfigWithCABundle0       *kubeApiAdmission.ValidatingWebhookConfiguration
	galleyWebhookConfigWithCABundle1 *kubeApiAdmission.ValidatingWebhookConfiguration

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
	istiodWebhookConfigEncoded = runtime.EncodeOrDie(codec, unpatchedIstiodWebhookConfig)

	webhookConfigWithCABundle0 = unpatchedIstiodWebhookConfig.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	webhookConfigWithCABundle0.Webhooks[0].ClientConfig.CABundle = caBundle0
	webhookConfigWithCABundle0.Webhooks[1].ClientConfig.CABundle = caBundle0

	galleyWebhookConfigWithCABundle1 = webhookConfigWithCABundle0.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	galleyWebhookConfigWithCABundle1.Webhooks[0].ClientConfig.Service.Name = galley
	galleyWebhookConfigWithCABundle1.Webhooks[1].ClientConfig.Service.Name = galley
	galleyWebhookConfigWithCABundle1.Webhooks[0].ClientConfig.CABundle = caBundle1
	galleyWebhookConfigWithCABundle1.Webhooks[1].ClientConfig.CABundle = caBundle1
}

type fakeController struct {
	*Controller

	endpointStore    cache.Store
	deploymentStore  cache.Store
	configStore      cache.Store
	clusterRoleStore cache.Store

	caChangedCh     chan bool
	configChangedCh chan bool

	injectedMu       sync.Mutex
	injectedCABundle []byte
	injectedConfig   []byte

	fakeWatcher *filewatcher.FakeWatcher
	*fake.Clientset
	reconcileDoneCh chan struct{}
}

const (
	namespace            = "istio-system"
	galley               = "istio-galley"
	galleyDeploymentName = "istio-galley"
	galleyWebhookName    = "istio-galley"
	istiod               = "istiod"
	caPath               = "fakeCAPath"
	configPath           = "fakeConfigPath"
	istiodClusterRole    = "istiod-istio-system"
)

func createTestController(t *testing.T, deferTo bool) *fakeController {
	fakeClient := fake.NewSimpleClientset()
	o := Options{
		WatchedNamespace:  namespace,
		ResyncPeriod:      time.Minute,
		CAPath:            caPath,
		WebhookConfigName: galleyWebhookName,
		WebhookConfigPath: configPath,
		ServiceName:       istiod,
		ClusterRoleName:   istiodClusterRole,
	}

	if deferTo {
		o.DeferToDeploymentName = galleyDeploymentName
	}

	caChanged := make(chan bool, 10)
	configChanged := make(chan bool, 10)
	changed := func(path string, added bool) {
		switch path {
		case o.CAPath:
			caChanged <- added
		case o.WebhookConfigPath:
			configChanged <- added
		}
	}

	newFileWatcher, fakeWatcher := filewatcher.NewFakeWatcher(changed)

	fc := &fakeController{
		caChangedCh:      caChanged,
		configChangedCh:  configChanged,
		injectedCABundle: caBundle0,
		injectedConfig:   []byte(istiodWebhookConfigEncoded),
		fakeWatcher:      fakeWatcher,
		Clientset:        fakeClient,
		reconcileDoneCh:  make(chan struct{}, 100),
	}

	readFile := func(filename string) ([]byte, error) {
		fc.injectedMu.Lock()
		defer fc.injectedMu.Unlock()

		switch filename {
		case o.CAPath:
			return fc.injectedCABundle, nil
		case o.WebhookConfigPath:
			return fc.injectedConfig, nil
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

	si := fc.Controller.sharedInformers
	fc.endpointStore = si.Core().V1().Endpoints().Informer().GetStore()
	fc.deploymentStore = si.Apps().V1().Deployments().Informer().GetStore()
	fc.configStore = si.Admissionregistration().V1beta1().ValidatingWebhookConfigurations().Informer().GetStore()
	fc.clusterRoleStore = si.Rbac().V1().ClusterRoles().Informer().GetStore()

	return fc
}

func (fc *fakeController) ValidatingWebhookConfigurations() kubeTypedAdmission.ValidatingWebhookConfigurationInterface {
	return fc.client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
}

func (fc *fakeController) Endpoints() kubeTypedCore.EndpointsInterface {
	return fc.client.CoreV1().Endpoints(fc.o.WatchedNamespace)
}

func (fc *fakeController) Deployments() kubeTypedApp.DeploymentInterface {
	return fc.client.AppsV1().Deployments(fc.o.WatchedNamespace)
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
	c := createTestController(t, false)

	g.Expect(c.Actions()[0].Matches("get", "clusterroles")).Should(BeTrue())

	reconcileHelper(t, c)
	_, err := c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})
	g.Expect(kubeErrors.ReasonForError(err)).Should(Equal(kubeApiMeta.StatusReasonNotFound),
		"no config when endpoint not present")

	_ = c.endpointStore.Add(istiodEndpoint)
	reconcileHelper(t, c)
	g.Expect(c.Actions()[0].Matches("create", "validatingwebhookconfigurations")).Should(BeTrue())
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundle0), "istiod config created when endpoint is ready")
}

func TestDeferDisabled(t *testing.T) {
	g := NewGomegaWithT(t)
	c := createTestController(t, false)

	// setup an existing deployment and config
	_ = c.deploymentStore.Add(galleyDeployment)
	_ = c.configStore.Add(galleyWebhookConfigWithCABundle1)
	_ = c.endpointStore.Add(istiodEndpoint)
	_, err := c.ValidatingWebhookConfigurations().Create(galleyWebhookConfigWithCABundle1)
	g.Expect(err).Should(Succeed())

	// verify we ignore any existing config when deferral is disabled.
	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundle0), "istiod should override galley config when galley is present")
}

func TestUpgradeDowngrade(t *testing.T) {
	g := NewGomegaWithT(t)
	c := createTestController(t, true)

	_ = c.deploymentStore.Add(galleyDeployment)
	_ = c.configStore.Add(galleyWebhookConfigWithCABundle1)
	_ = c.endpointStore.Add(istiodEndpoint)
	_, err := c.ValidatingWebhookConfigurations().Create(galleyWebhookConfigWithCABundle1)
	g.Expect(err).Should(Succeed())
	reconcileHelper(t, c)
	g.Expect(c.Actions()).Should(BeEmpty())
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(galleyWebhookConfigWithCABundle1), "galley webhook should exist when istiod is deployed")

	_ = c.deploymentStore.Delete(galleyDeployment)
	reconcileHelper(t, c)
	g.Expect(c.Actions()[0].Matches("update", "validatingwebhookconfigurations")).Should(BeTrue())
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundle0), "istiod webhook should exist when galley is removed")

	_ = c.deploymentStore.Add(galleyDeployment)
	_ = c.configStore.Add(galleyWebhookConfigWithCABundle1)
	_, err = c.ValidatingWebhookConfigurations().Update(galleyWebhookConfigWithCABundle1)
	g.Expect(err).Should(Succeed())
	reconcileHelper(t, c)
	g.Expect(c.Actions()).Should(BeEmpty())
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(galleyWebhookConfigWithCABundle1), "istiod webhook should not exist when galley is reployed")

	galleyDeploymentWithZeroReplicas := galleyDeployment.DeepCopyObject().(*kubeApiApp.Deployment)
	galleyDeploymentWithZeroReplicas.Spec.Replicas = &[]int32{0}[0]
	_ = c.deploymentStore.Update(galleyDeploymentWithZeroReplicas)
	reconcileHelper(t, c)
	g.Expect(c.Actions()[0].Matches("update", "validatingwebhookconfigurations")).Should(BeTrue())
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundle0), "istiod webhook should exist when galley has zero replicas")

	_ = c.deploymentStore.Delete(galleyDeployment)
	reconcileHelper(t, c)
	g.Expect(c.Actions()[0].Matches("update", "validatingwebhookconfigurations")).Should(BeTrue())
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundle0), "istiod webhook should exist when galley with zero replicas is removed")
}

func TestUnregisterValidationWebhook(t *testing.T) {
	g := NewGomegaWithT(t)
	c := createTestController(t, true)

	_ = c.endpointStore.Add(istiodEndpoint)
	reconcileHelper(t, c)
	_, err := c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApiMeta.GetOptions{})
	g.Expect(err).Should(Succeed())

	c.o.UnregisterValidationWebhook = true
	reconcileHelper(t, c)

	_, err = c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApiMeta.GetOptions{})
	g.Expect(err).ShouldNot(Succeed())
	g.Expect(kubeErrors.ReasonForError(err)).Should(Equal(kubeApiMeta.StatusReasonNotFound))
}

func TestCertAndConfigFileChange(t *testing.T) {
	g := NewGomegaWithT(t)
	c := createTestController(t, true)

	_ = c.endpointStore.Add(istiodEndpoint)
	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(webhookConfigWithCABundle0), "istiod config created when endpoint is ready")
	// keep test store and tracker in-sync
	_ = c.configStore.Add(webhookConfigWithCABundle0)

	// verify the config updates after injecting a cafile change
	c.injectedMu.Lock()
	c.injectedCABundle = caBundle1
	c.injectedMu.Unlock()

	webhookConfigAfterCAUpdate := webhookConfigWithCABundle0.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	webhookConfigAfterCAUpdate.Webhooks[0].ClientConfig.CABundle = caBundle1
	webhookConfigAfterCAUpdate.Webhooks[1].ClientConfig.CABundle = caBundle1

	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(webhookConfigAfterCAUpdate), "webhook should change after cert change")
	// keep test store and tracker in-sync
	_ = c.configStore.Update(webhookConfigAfterCAUpdate)

	// verify the config updates after injecting a config file change.
	webhookConfigAfterConfigUpdate := webhookConfigAfterCAUpdate.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	webhookConfigAfterConfigUpdate.Webhooks[0].TimeoutSeconds = &[]int32{1}[0]
	c.injectedMu.Lock()
	c.injectedConfig = []byte(runtime.EncodeOrDie(codec, webhookConfigAfterConfigUpdate))
	c.injectedMu.Unlock()

	reconcileHelper(t, c)
	g.Expect(c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApisMeta.GetOptions{})).
		Should(Equal(webhookConfigAfterConfigUpdate), "webhook should change after cert change")
	// keep test store and tracker in-sync
	_ = c.configStore.Update(webhookConfigAfterConfigUpdate)

	// verify config is not updated if the config file is bad
	c.injectedMu.Lock()
	c.injectedConfig = []byte("bad configfile")
	c.injectedMu.Unlock()
	g.Expect(c.ValidatingWebhookConfigurations().Delete(galleyWebhookName, &kubeApiMeta.DeleteOptions{})).Should(Succeed())
	reconcileHelper(t, c)
	_, err := c.ValidatingWebhookConfigurations().Get(galleyWebhookName, kubeApiMeta.GetOptions{})
	g.Expect(err).ShouldNot(Succeed())
	g.Expect(kubeErrors.ReasonForError(err)).Should(Equal(kubeApiMeta.StatusReasonNotFound))
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

func TestConfigOverrideAndDefaulting(t *testing.T) {
	g := NewGomegaWithT(t)

	missingDefaults := unpatchedIstiodWebhookConfig.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)

	missingDefaults.Name = "wrong-name"
	for i := 0; i < len(missingDefaults.Webhooks); i++ {
		missingDefaults.Webhooks[i].FailurePolicy = nil
		missingDefaults.Webhooks[i].NamespaceSelector = nil
		missingDefaults.Webhooks[i].ObjectSelector = nil
		missingDefaults.Webhooks[i].SideEffects = nil
		missingDefaults.Webhooks[i].TimeoutSeconds = nil
	}

	applyDefaultsAndOverrides(missingDefaults, galleyWebhookName)
	g.Expect(missingDefaults.Name).Should(Equal(galleyWebhookName))
	for i := 0; i < len(missingDefaults.Webhooks); i++ {
		g.Expect(missingDefaults.Webhooks[i].FailurePolicy).Should(Equal(&defaultFailurePolicy))
		g.Expect(missingDefaults.Webhooks[i].NamespaceSelector).Should(Equal(defaultNamespaceSelector))
		g.Expect(missingDefaults.Webhooks[i].ObjectSelector).Should(Equal(defaultObjectSelector))
		g.Expect(missingDefaults.Webhooks[i].SideEffects).Should(Equal(&defaultSideEffects))
		g.Expect(missingDefaults.Webhooks[i].TimeoutSeconds).Should(Equal(&defaultTimeout))
	}

	var (
		nonDefaultFailurePolicy     = kubeApiAdmission.Ignore
		nonDefaultSideEffects       = kubeApiAdmission.SideEffectClassNone
		nonDefaultTimeout           = int32(3)
		nonDefaultNamespaceSelector = &kubeApiMeta.LabelSelector{MatchLabels: map[string]string{"k": "n"}}
		nonDefaultObjectSelector    = &kubeApiMeta.LabelSelector{MatchLabels: map[string]string{"k": "o"}}
	)

	nonDefaultFields := unpatchedIstiodWebhookConfig.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	for i := 0; i < len(missingDefaults.Webhooks); i++ {
		missingDefaults.Webhooks[i].FailurePolicy = &nonDefaultFailurePolicy
		missingDefaults.Webhooks[i].NamespaceSelector = nonDefaultNamespaceSelector
		missingDefaults.Webhooks[i].ObjectSelector = nonDefaultObjectSelector
		missingDefaults.Webhooks[i].SideEffects = &nonDefaultSideEffects
		missingDefaults.Webhooks[i].TimeoutSeconds = &nonDefaultTimeout
	}

	applyDefaultsAndOverrides(nonDefaultFields, galleyWebhookName)
	g.Expect(missingDefaults.Name).Should(Equal(galleyWebhookName))
	for i := 0; i < len(missingDefaults.Webhooks); i++ {
		g.Expect(missingDefaults.Webhooks[i].FailurePolicy).Should(Equal(&nonDefaultFailurePolicy))
		g.Expect(missingDefaults.Webhooks[i].NamespaceSelector).Should(Equal(nonDefaultNamespaceSelector))
		g.Expect(missingDefaults.Webhooks[i].ObjectSelector).Should(Equal(nonDefaultObjectSelector))
		g.Expect(missingDefaults.Webhooks[i].SideEffects).Should(Equal(&nonDefaultSideEffects))
		g.Expect(missingDefaults.Webhooks[i].TimeoutSeconds).Should(Equal(&nonDefaultTimeout))
	}
}
