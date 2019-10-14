// Copyright 2017 Istio Authors
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
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	k8ssecret "istio.io/istio/security/pkg/k8s/secret"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	"istio.io/istio/security/pkg/pki/util"
	mockutil "istio.io/istio/security/pkg/pki/util/mock"
)

const (
	defaultTTL                = time.Hour
	defaultGracePeriodRatio   = 0.5
	defaultMinGracePeriod     = 10 * time.Minute
	sidecarInjectorSvcAccount = "istio-sidecar-injector-service-account"
	sidecarInjectorSvc        = "istio-sidecar-injector"
)

var (
	caCert          = []byte("fake CA cert")
	caKey           = []byte("fake private key")
	certChain       = []byte("fake cert chain")
	rootCert        = []byte("fake root cert")
	signedCert      = []byte("fake signed cert")
	istioTestSecret = k8ssecret.BuildSecret("test", "istio.test",
		"test-ns", certChain, caKey, rootCert, nil, nil, IstioSecretType)
	cert1Pem = `
-----BEGIN CERTIFICATE-----
MIIC3jCCAcagAwIBAgIJAMwyWk0iqlOoMA0GCSqGSIb3DQEBCwUAMBwxGjAYBgNV
BAoMEWs4cy5jbHVzdGVyLmxvY2FsMB4XDTE4MDkyMTAyMjAzNFoXDTI4MDkxODAy
MjAzNFowHDEaMBgGA1UECgwRazhzLmNsdXN0ZXIubG9jYWwwggEiMA0GCSqGSIb3
DQEBAQUAA4IBDwAwggEKAoIBAQC8TDtfy23OKCRnkSYrKZwuHG5lOmTZgLwoFR1h
3NDTkjR9406CjnAy6Gl73CRG3zRYVgY/2dGNqTzAKRCeKZlOzBlK6Kilb0NIJ6it
s6ooMAxwXlr7jOKiSn6xbaexVMrP0VPUbCgJxQtGs3++hQ14D6WnyfdzPBZJLKbI
tVdDnAcl/FJXKVV9gIg+MM0gETWOYj5Yd8Ye0FTvoFcgs8NKkxhEZe/LeYa7XYsk
S0PymwbHwNZcfC4znp2bzu28LUmUe6kL97YU8ubvhR0muRy6h5MnQNMQrRG5Q5j4
A2+tkO0vto8gOb6/lacEUVYuQdSkMZJiqWEjWgWKeAYdkTJDAgMBAAGjIzAhMA4G
A1UdDwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IB
AQAxWP3MT0IelJcb+e7fNTfMS0r3UhpiNkRU368Z7gJ4tDNOGRPzntW6CLnaE+3g
IjOMAE8jlXeEmNuXtDQqQoZwWc1D5ma3jyc83E5H9LJzjfmn5rAHafr29YH85Ms2
VlKdpP+teYg8Cag9u4ar/AUR4zMUEpGK5U+T9IH44lVqVH23T+DxAT+btsyuGiB0
DsM76XVDj4g3OKCUalu7a8FHvgTkBpUJBl7vwh9kqo9HwCaj4iC2CwveOm0WtSgy
K9PpVDxTGNSxqsxKn7DJQ15NTOP+gr29ABqFKwRr+S8ggw6evzHbABQTUMebaRSr
iH7cSgrzZBiUvJmZRi7/BrYU
-----END CERTIFICATE-----`

	key1Pem = `
-----BEGIN PRIVATE KEY-----
MIIEwAIBADANBgkqhkiG9w0BAQEFAASCBKowggSmAgEAAoIBAQC8TDtfy23OKCRn
kSYrKZwuHG5lOmTZgLwoFR1h3NDTkjR9406CjnAy6Gl73CRG3zRYVgY/2dGNqTzA
KRCeKZlOzBlK6Kilb0NIJ6its6ooMAxwXlr7jOKiSn6xbaexVMrP0VPUbCgJxQtG
s3++hQ14D6WnyfdzPBZJLKbItVdDnAcl/FJXKVV9gIg+MM0gETWOYj5Yd8Ye0FTv
oFcgs8NKkxhEZe/LeYa7XYskS0PymwbHwNZcfC4znp2bzu28LUmUe6kL97YU8ubv
hR0muRy6h5MnQNMQrRG5Q5j4A2+tkO0vto8gOb6/lacEUVYuQdSkMZJiqWEjWgWK
eAYdkTJDAgMBAAECggEBAJTemFqmVQwWxKF1Kn4ZibcTF1zFDBLCKwBtoStMD3YW
M5YL7nhd8OruwOcCJ1Q5CAOHD63PolOjp7otPUwui1y3FJAa3areCo2zfTLHxxG6
2zrD/p6+xjeVOhFBJsGWzjn7v5FEaWs/9ChTpf2U6A8yH8BGd3MN4Hi96qboaDO0
fFz3zOu7sgjkDNZiapZpUuqs7a6MCCr2T3FPwdWUiILZF2t5yWd/l8KabP+3QvvR
tDU6sNv4j8e+dsF2l9ZT81JLkN+f6HvWcLVAADvcBqMcd8lmMSPgxSbytzKanx7o
wtzIiGkNZBCVKGO7IK2ByCluiyHDpGul60Th7HUluDECgYEA9/Q1gT8LTHz1n6vM
2n2umQN9R+xOaEYN304D5DQqptN3S0BCJ4dihD0uqEB5osstRTf4QpP/qb2hMDP4
qWbWyrc7Z5Lyt6HI1ly6VpVnYKb3HDeJ9M+5Se1ttdwyRCzuT4ZBhT5bbqBatsOU
V7+dyrJKbk8r9K4qy29UFozz/38CgYEAwmhzPVak99rVmqTpe0gPERW//n+PdW3P
Ta6ongU8zkkw9LAFwgjGtNpd4nlk0iQigiM4jdJDFl6edrRXv2cisEfJ9+s53AOb
hXui4HAn2rusPK+Dq2InkHYTGjEGDpx94zC/bjYR1GBIsthIh0w2G9ql8yvLatxG
x6oXEsb7Lz0CgYEA7Oj+/mDYUNrMbSVfdBvF6Rl2aHQWbncQ5h3Khg55+i/uuY3K
J66pqKQ0ojoIfk0XEh3qLOLv0qUHD+F4Y5OJAuOT9OBo3J/OH1M2D2hs/+JIFUPT
on+fEE21F6AuvwkXIhCrJb5w6gB47Etuv3CsOXGkwEURQJXw+bODapB+yc0CgYEA
t7zoTay6NdcJ0yLR2MZ+FvOrhekhuSaTqyPMEa15jq32KwzCJGUPCJbp7MY217V3
N+/533A+H8JFmoNP+4KKcnknFb2n7Z0rO7licyUNRdniK2jm1O/r3Mj7vOFgjCaz
hCnqg0tvBn4Jt55aziTlbuXzuiRGGTUfYE4NiJ2vgTECgYEA8di9yqGhETYQkoT3
E70JpEmkCWiHl/h2ClLcDkj0gXKFxmhzmvs8G5On4S8toNiJ6efmz0KlHN1F7Ldi
2iVd9LZnFVP1YwG0mvTJxxc5P5Uy5q/EhCLBAetqoTkWYlPcpkcathmCbCpJG4/x
iOmuuOfQWnMfcVk8I0YDL5+G9Pg=
-----END PRIVATE KEY-----`
)

func TestSecretController(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}
	testCases := map[string]struct {
		existingSecret   *v1.Secret
		saToAdd          *v1.ServiceAccount
		saToDelete       *v1.ServiceAccount
		sasToUpdate      *updatedSas
		expectedActions  []ktesting.Action
		gracePeriodRatio float32
		injectFailure    bool
		shouldFail       bool
	}{
		"invalid gracePeriodRatio": {
			saToAdd: createServiceAccount("test", "test-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "test-ns", istioTestSecret),
			},
			gracePeriodRatio: 1.4,
			shouldFail:       true,
		},
		"adding service account creates new secret": {
			saToAdd: createServiceAccount("test", "test-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "test-ns", istioTestSecret),
			},
			gracePeriodRatio: defaultGracePeriodRatio,
			shouldFail:       false,
		},
		"removing service account deletes existing secret": {
			saToDelete: createServiceAccount("deleted", "deleted-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewDeleteAction(gvr, "deleted-ns", "istio.deleted"),
			},
			gracePeriodRatio: defaultGracePeriodRatio,
			shouldFail:       false,
		},
		"updating service accounts does nothing if name and namespace are not changed": {
			sasToUpdate: &updatedSas{
				curSa: createServiceAccount("name", "ns"),
				oldSa: createServiceAccount("name", "ns"),
			},
			gracePeriodRatio: defaultGracePeriodRatio,
			expectedActions:  []ktesting.Action{},
			shouldFail:       false,
		},
		"updating service accounts deletes old secret and creates a new one": {
			sasToUpdate: &updatedSas{
				curSa: createServiceAccount("new-name", "new-ns"),
				oldSa: createServiceAccount("old-name", "old-ns"),
			},
			expectedActions: []ktesting.Action{
				ktesting.NewDeleteAction(gvr, "old-ns", "istio.old-name"),
				ktesting.NewCreateAction(gvr, "new-ns", k8ssecret.BuildSecret(
					"new-name", "istio.new-name", "new-ns", certChain,
					caKey, rootCert, nil, nil, IstioSecretType)),
			},
			gracePeriodRatio: defaultGracePeriodRatio,
			shouldFail:       false,
		},
		"adding new service account does not overwrite existing secret": {
			existingSecret:   istioTestSecret,
			saToAdd:          createServiceAccount("test", "test-ns"),
			gracePeriodRatio: defaultGracePeriodRatio,
			expectedActions:  []ktesting.Action{},
			shouldFail:       false,
		},
		"adding service account retries when failed": {
			saToAdd: createServiceAccount("test", "test-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "test-ns", istioTestSecret),
				ktesting.NewCreateAction(gvr, "test-ns", istioTestSecret),
				ktesting.NewCreateAction(gvr, "test-ns", istioTestSecret),
			},
			gracePeriodRatio: defaultGracePeriodRatio,
			injectFailure:    true,
			shouldFail:       false,
		},
		"adding webhook service account": {
			saToAdd: createServiceAccount(sidecarInjectorSvcAccount, "test-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "test-ns",
					k8ssecret.BuildSecret("test", sidecarInjectorSvcAccount,
						"test-ns", certChain, caKey, rootCert, nil,
						nil, IstioSecretType)),
			},
			gracePeriodRatio: defaultGracePeriodRatio,
			shouldFail:       false,
		},
	}

	for k, tc := range testCases {
		client := fake.NewSimpleClientset()

		if tc.injectFailure {
			callCount := 0
			// PrependReactor to ensure action handled by our handler.
			client.Fake.PrependReactor("*", "*", func(a ktesting.Action) (bool, runtime.Object, error) {
				callCount++
				if callCount < secretCreationRetry {
					return true, nil, errors.New("failed to create secret deliberately")
				}
				return true, nil, nil
			})
		}

		webhooks := map[string]*DNSNameEntry{
			sidecarInjectorSvcAccount: &DNSNameEntry{
				ServiceName: sidecarInjectorSvc,
				Namespace:   "test-ns",
			},
		}
		controller, err := NewSecretController(createFakeCA(),
			defaultTTL, tc.gracePeriodRatio, defaultMinGracePeriod, false, client.CoreV1(),
			false, []string{metav1.NamespaceAll}, webhooks,
			"test-ns")

		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed to create secret controller")
			} else {
				// Should fail, skip the current case.
				continue
			}
		} else if err != nil {
			t.Errorf("failed to create secret controller: %v", err)
		}

		if tc.existingSecret != nil {
			err := controller.scrtStore.Add(tc.existingSecret)
			if err != nil {
				t.Errorf("Failed to add a secret (error %v)", err)
			}
		}

		if tc.saToAdd != nil {
			controller.saAdded(tc.saToAdd)
		}
		if tc.saToDelete != nil {
			controller.saDeleted(tc.saToDelete)
		}
		if tc.sasToUpdate != nil {
			controller.saUpdated(tc.sasToUpdate.oldSa, tc.sasToUpdate.curSa)
		}

		if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
			t.Errorf("Case %q: %s", k, err.Error())
		}
	}
}

func TestSecretContent(t *testing.T) {
	saName := "test-serviceaccount"
	saNamespace := "test-namespace"
	client := fake.NewSimpleClientset()
	controller, err := NewSecretController(createFakeCA(), defaultTTL,
		defaultGracePeriodRatio, defaultMinGracePeriod, false, client.CoreV1(),
		false, []string{metav1.NamespaceAll}, map[string]*DNSNameEntry{},
		"test-namespace")
	if err != nil {
		t.Errorf("Failed to create secret controller: %v", err)
	}
	controller.saAdded(createServiceAccount(saName, saNamespace))

	_ = k8ssecret.BuildSecret(saName, GetSecretName(saName), saNamespace,
		nil, nil, nil, nil, nil, IstioSecretType)
	secret, err := client.CoreV1().Secrets(saNamespace).Get(GetSecretName(saName), metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to retrieve secret: %v", err)
	}
	if !bytes.Equal(rootCert, secret.Data[RootCertID]) {
		t.Errorf("Root cert verification error: expected %v but got %v", rootCert, secret.Data[RootCertID])
	}
	if !bytes.Equal(append(signedCert, certChain...), secret.Data[CertChainID]) {
		t.Errorf("Cert chain verification error: expected %v but got %v", certChain, secret.Data[CertChainID])
	}
}
func TestDeletedIstioSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	controller, err := NewSecretController(createFakeCA(), defaultTTL,
		defaultGracePeriodRatio, defaultMinGracePeriod, false, client.CoreV1(),
		false, []string{metav1.NamespaceAll}, nil,
		"test-ns")
	if err != nil {
		t.Errorf("failed to create secret controller: %v", err)
	}
	sa := createServiceAccount("test-sa", "test-ns")
	if _, err := client.CoreV1().ServiceAccounts("test-ns").Create(sa); err != nil {
		t.Error(err)
	}

	saGvr := schema.GroupVersionResource{
		Resource: "serviceaccounts",
		Version:  "v1",
	}
	scrtGvr := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}

	testCases := map[string]struct {
		secret          *v1.Secret
		expectedActions []ktesting.Action
	}{
		"Recover secret for existing service account": {
			secret: k8ssecret.BuildSecret("test-sa", "istio.test-sa", "test-ns",
				nil, nil, nil, nil, nil, IstioSecretType),
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(saGvr, "test-ns", "test-sa"),
				ktesting.NewCreateAction(scrtGvr, "test-ns", k8ssecret.BuildSecret("test-sa",
					"istio.test-sa", "test-ns", nil, nil, nil,
					nil, nil, IstioSecretType)),
			},
		},
		"Do not recover secret for non-existing service account in the same namespace": {
			secret: k8ssecret.BuildSecret("test-sa2", "istio.test-sa2", "test-ns",
				nil, nil, nil, nil, nil, IstioSecretType),
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(saGvr, "test-ns", "test-sa2"),
			},
		},
		"Do not recover secret for service account in different namespace": {
			secret: k8ssecret.BuildSecret("test-sa", "istio.test-sa", "test-ns2",
				nil, nil, nil, nil, nil, IstioSecretType),
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(saGvr, "test-ns2", "test-sa"),
			},
		},
	}

	for k, tc := range testCases {
		client.ClearActions()
		controller.scrtDeleted(tc.secret)
		if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
			t.Errorf("Failure in test case %s: %v", k, err)
		}
	}
}

func TestUpdateSecret(t *testing.T) {
	secretSchema := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}
	gvr := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}
	testCases := map[string]struct {
		expectedActions     []ktesting.Action
		ttl                 time.Duration
		minGracePeriod      time.Duration
		rootCert            []byte
		gracePeriodRatio    float32
		certIsInvalid       bool
		createIstioCASecret bool
		rootCertMatchBundle bool
		originalKCBSyncTime time.Time
		expectedKCBSyncTime bool
	}{
		"Does not update non-expiring secret": {
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(secretSchema, "", CASecret),
			},
			ttl:                 time.Hour,
			gracePeriodRatio:    0.5,
			minGracePeriod:      10 * time.Minute,
			rootCertMatchBundle: true,
			originalKCBSyncTime: time.Time{},
			expectedKCBSyncTime: false,
		},
		"Update secret in grace period": {
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(gvr, "test-ns", istioTestSecret),
			},
			ttl:              time.Hour,
			gracePeriodRatio: 1, // Always in grace period
			minGracePeriod:   10 * time.Minute,
			originalKCBSyncTime: time.Now(),
			expectedKCBSyncTime: false,
		},
		"Update secret in min grace period": {
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(gvr, "test-ns", istioTestSecret),
			},
			ttl:              10 * time.Minute,
			gracePeriodRatio: 0.5,
			minGracePeriod:   time.Hour, // ttl is always in minGracePeriod
			originalKCBSyncTime: time.Now(),
			expectedKCBSyncTime: false,
		},
		"Update expired secret": {
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(gvr, "test-ns", istioTestSecret),
			},
			ttl:              -time.Second,
			gracePeriodRatio: 0.5,
			minGracePeriod:   10 * time.Minute,
			originalKCBSyncTime: time.Now(),
			expectedKCBSyncTime: false,
		},
		"Update secret with different root cert": {
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(secretSchema, "", k8ssecret.BuildSecret("",
					CASecret, "", nil, nil, []byte(cert1Pem),
					[]byte(cert1Pem), []byte(key1Pem), IstioSecretType)),
				ktesting.NewGetAction(secretSchema, "", CASecret),
				ktesting.NewUpdateAction(secretSchema, "test-ns", istioTestSecret),
			},
			ttl:                 time.Hour,
			gracePeriodRatio:    0.5,
			minGracePeriod:      10 * time.Minute,
			rootCert:            []byte("Outdated root cert"),
			createIstioCASecret: true,
			originalKCBSyncTime: time.Time{},
			expectedKCBSyncTime: false,
		},
		"Update secret with invalid certificate": {
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(secretSchema, "test-ns", istioTestSecret),
			},
			ttl:              time.Hour,
			gracePeriodRatio: 0.5,
			minGracePeriod:   10 * time.Minute,
			certIsInvalid:    true,
			originalKCBSyncTime: time.Now(),
			expectedKCBSyncTime: false,
		},
		"Reload key cert bundle": {
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(secretSchema, "", k8ssecret.BuildSecret("",
					CASecret, "", nil, nil, []byte(cert1Pem),
					[]byte(cert1Pem), []byte(key1Pem), IstioSecretType)),
				ktesting.NewGetAction(secretSchema, "", CASecret),
			},
			ttl:                 time.Hour,
			gracePeriodRatio:    0.5,
			minGracePeriod:      10 * time.Minute,
			createIstioCASecret: true,
			rootCert:            []byte(cert1Pem),
			originalKCBSyncTime: time.Time{},
			expectedKCBSyncTime: true,
		},
		"Skip reloading key cert bundle": {
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(secretSchema, "", k8ssecret.BuildSecret("",
					CASecret, "", nil, nil, []byte(cert1Pem),
					[]byte(cert1Pem), []byte(key1Pem), IstioSecretType)),
				ktesting.NewUpdateAction(secretSchema, "test-ns", istioTestSecret),
			},
			ttl:                 time.Hour,
			gracePeriodRatio:    0.5,
			minGracePeriod:      10 * time.Minute,
			createIstioCASecret: true,
			rootCert:            []byte(cert1Pem),
			originalKCBSyncTime: time.Now(),
			expectedKCBSyncTime: false,
		},
	}

	for k, tc := range testCases {
		client := fake.NewSimpleClientset()
		ca := createFakeCA()
		controller, err := NewSecretController(ca, time.Hour, tc.gracePeriodRatio,
			tc.minGracePeriod, false, client.CoreV1(), false,
			[]string{metav1.NamespaceAll}, nil, "")
		if err != nil {
			t.Errorf("failed to create secret controller: %v", err)
		}
		controller.lastKCBSyncTime = tc.originalKCBSyncTime
		scrt := istioTestSecret
		if rc := tc.rootCert; rc != nil {
			scrt.Data[RootCertID] = rc
		}
		if tc.createIstioCASecret {
			caScrt := k8ssecret.BuildSecret("", CASecret, "", nil, nil, []byte(cert1Pem),
				[]byte(cert1Pem), []byte(key1Pem), IstioSecretType)
			client.CoreV1().Secrets("").Create(caScrt)
			ca.KeyCertBundle = &mockutil.FakeKeyCertBundle{
				CertBytes:      caCert,
				PrivKeyBytes:   caKey,
				CertChainBytes: certChain,
				RootCertBytes:  rootCert,
			}
		}
		opts := util.CertOptions{
			IsSelfSigned: true,
			TTL:          tc.ttl,
			RSAKeySize:   512,
		}
		if !tc.certIsInvalid {
			bs, _, err := util.GenCertKeyFromOptions(opts)
			if err != nil {
				t.Error(err)
			}
			scrt.Data[CertChainID] = bs
			if tc.rootCertMatchBundle {
				ca.KeyCertBundle = &mockutil.FakeKeyCertBundle{
					CertBytes:      bs,
					PrivKeyBytes:   []byte(key1Pem),
					CertChainBytes: bs,
					RootCertBytes:  bs,
				}
			}
		}

		controller.scrtUpdated(nil, scrt)

		if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
			t.Errorf("Case %q: %s", k, err.Error())
		}
		if tc.originalKCBSyncTime.IsZero() {
			if tc.expectedKCBSyncTime && controller.lastKCBSyncTime.IsZero() {
				t.Errorf("Case %q: controller's lastKCBSyncTime should be set", k)
			}
		}
	}
}

func checkActions(actual, expected []ktesting.Action) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("unexpected number of actions, want %d but got %d", len(expected), len(actual))
	}

	for i, action := range actual {
		expectedAction := expected[i]
		verb := expectedAction.GetVerb()
		resource := expectedAction.GetResource().Resource
		if !action.Matches(verb, resource) {
			return fmt.Errorf("unexpected %dth action, want %q but got %q", i, expectedAction, action)
		}
	}

	return nil
}

func createFakeCA() *mockca.FakeCA {
	return &mockca.FakeCA{
		SignedCert: signedCert,
		SignErr:    nil,
		KeyCertBundle: &mockutil.FakeKeyCertBundle{
			CertBytes:      caCert,
			PrivKeyBytes:   caKey,
			CertChainBytes: certChain,
			RootCertBytes:  rootCert,
		},
	}
}

func createServiceAccount(name, namespace string) *v1.ServiceAccount {
	return &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

type updatedSas struct {
	curSa *v1.ServiceAccount
	oldSa *v1.ServiceAccount
}
