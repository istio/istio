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
	"istio.io/istio/pkg/spiffe"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
	"time"

	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"istio.io/istio/security/pkg/pki/ca"
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
	istioTestSecret = ca.BuildSecret("test", "istio.test", "test-ns", certChain, caKey, rootCert, nil, nil, IstioSecretType)
)

func TestSecretController(t *testing.T) {
	spiffe.WithIdentityDomain("cluster.local", func() {
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
					ktesting.NewCreateAction(gvr, "new-ns", ca.BuildSecret("new-name", "istio.new-name", "new-ns", certChain, caKey, rootCert, nil, nil, IstioSecretType)),
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
						ca.BuildSecret("test", sidecarInjectorSvcAccount, "test-ns", certChain, caKey, rootCert, nil, nil, IstioSecretType)),
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

			webhooks := map[string]DNSNameEntry{
				sidecarInjectorSvcAccount: {
					ServiceName: sidecarInjectorSvc,
					Namespace:   "test-ns",
				},
			}
			controller, err := NewSecretController(createFakeCA(), defaultTTL,
				tc.gracePeriodRatio, defaultMinGracePeriod, false, client.CoreV1(), false,
				metav1.NamespaceAll, webhooks)
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
	})
}

func TestSecretContent(t *testing.T) {
	spiffe.WithIdentityDomain("cluster.local", func() {
		saName := "test-serviceaccount"
		saNamespace := "test-namespace"
		client := fake.NewSimpleClientset()
		controller, err := NewSecretController(createFakeCA(), defaultTTL,
			defaultGracePeriodRatio, defaultMinGracePeriod, false, client.CoreV1(), false,
			metav1.NamespaceAll, map[string]DNSNameEntry{})
		if err != nil {
			t.Errorf("Failed to create secret controller: %v", err)
		}
		controller.saAdded(createServiceAccount(saName, saNamespace))

		secret := ca.BuildSecret(saName, GetSecretName(saName), saNamespace, nil, nil, nil, nil, nil, IstioSecretType)
		secret, err = client.CoreV1().Secrets(saNamespace).Get(GetSecretName(saName), metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to retrieve secret: %v", err)
		}
		if !bytes.Equal(rootCert, secret.Data[RootCertID]) {
			t.Errorf("Root cert verification error: expected %v but got %v", rootCert, secret.Data[RootCertID])
		}
		if !bytes.Equal(append(signedCert, certChain...), secret.Data[CertChainID]) {
			t.Errorf("Cert chain verification error: expected %v but got %v", certChain, secret.Data[CertChainID])
		}
	})
}
func TestDeletedIstioSecret(t *testing.T) {
	spiffe.WithIdentityDomain("cluster.local", func() {
		client := fake.NewSimpleClientset()
		controller, err := NewSecretController(createFakeCA(), defaultTTL,
			defaultGracePeriodRatio, defaultMinGracePeriod, false, client.CoreV1(), false,
			metav1.NamespaceAll, nil)
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
				secret: ca.BuildSecret("test-sa", "istio.test-sa", "test-ns", nil, nil, nil, nil, nil, IstioSecretType),
				expectedActions: []ktesting.Action{
					ktesting.NewGetAction(saGvr, "test-ns", "test-sa"),
					ktesting.NewCreateAction(scrtGvr, "test-ns", ca.BuildSecret("test-sa", "istio.test-sa", "test-ns", nil, nil, nil, nil, nil, IstioSecretType)),
				},
			},
			"Do not recover secret for non-existing service account in the same namespace": {
				secret: ca.BuildSecret("test-sa2", "istio.test-sa2", "test-ns", nil, nil, nil, nil, nil, IstioSecretType),
				expectedActions: []ktesting.Action{
					ktesting.NewGetAction(saGvr, "test-ns", "test-sa2"),
				},
			},
			"Do not recover secret for service account in different namespace": {
				secret: ca.BuildSecret("test-sa", "istio.test-sa", "test-ns2", nil, nil, nil, nil, nil, IstioSecretType),
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

	})
}

func TestUpdateSecret(t *testing.T) {
	spiffe.WithIdentityDomain("cluster.local", func() {
		gvr := schema.GroupVersionResource{
			Resource: "secrets",
			Version:  "v1",
		}
		testCases := map[string]struct {
			expectedActions  []ktesting.Action
			ttl              time.Duration
			gracePeriodRatio float32
			minGracePeriod   time.Duration
			rootCert         []byte
		}{
			"Does not update non-expiring secret": {
				expectedActions:  []ktesting.Action{},
				ttl:              time.Hour,
				gracePeriodRatio: 0.5,
				minGracePeriod:   10 * time.Minute,
			},
			"Update secret in grace period": {
				expectedActions: []ktesting.Action{
					ktesting.NewUpdateAction(gvr, "test-ns", istioTestSecret),
				},
				ttl:              time.Hour,
				gracePeriodRatio: 1, // Always in grace period
				minGracePeriod:   10 * time.Minute,
			},
			"Update secret in min grace period": {
				expectedActions: []ktesting.Action{
					ktesting.NewUpdateAction(gvr, "test-ns", istioTestSecret),
				},
				ttl:              10 * time.Minute,
				gracePeriodRatio: 0.5,
				minGracePeriod:   time.Hour, // ttl is always in minGracePeriod
			},
			"Update expired secret": {
				expectedActions: []ktesting.Action{
					ktesting.NewUpdateAction(gvr, "test-ns", istioTestSecret),
				},
				ttl:              -time.Second,
				gracePeriodRatio: 0.5,
				minGracePeriod:   10 * time.Minute,
			},
			"Update secret with different root cert": {
				expectedActions: []ktesting.Action{
					ktesting.NewUpdateAction(gvr, "test-ns", istioTestSecret),
				},
				ttl:              time.Hour,
				gracePeriodRatio: 0.5,
				minGracePeriod:   10 * time.Minute,
				rootCert:         []byte("Outdated root cert"),
			},
		}

		for k, tc := range testCases {
			client := fake.NewSimpleClientset()
			controller, err := NewSecretController(createFakeCA(), time.Hour,
				tc.gracePeriodRatio, tc.minGracePeriod, false, client.CoreV1(), false, metav1.NamespaceAll, nil)
			if err != nil {
				t.Errorf("failed to create secret controller: %v", err)
			}

			scrt := istioTestSecret
			if rc := tc.rootCert; rc != nil {
				scrt.Data[RootCertID] = rc
			}

			opts := util.CertOptions{
				IsSelfSigned: true,
				TTL:          tc.ttl,
				RSAKeySize:   512,
			}
			bs, _, err := util.GenCertKeyFromOptions(opts)
			if err != nil {
				t.Error(err)
			}
			scrt.Data[CertChainID] = bs

			controller.scrtUpdated(nil, scrt)

			if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
				t.Errorf("Case %q: %s", k, err.Error())
			}
		}

	})
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
