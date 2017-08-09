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
	"crypto/x509"
	"reflect"
	"testing"
	"time"

	"istio.io/auth/pkg/pki/ca"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/pkg/api/v1"
	ktesting "k8s.io/client-go/testing"
)

type fakeCa struct{}

func (ca fakeCa) Sign(*x509.CertificateRequest) ([]byte, error) {
	return nil, nil
}

func (ca fakeCa) Generate(name, namespace string) (chain, key []byte) {
	chain = []byte("fake cert chain")
	key = []byte("fake key")
	return
}

func (ca fakeCa) GetRootCertificate() []byte {
	return []byte("fake root cert")
}

func createSecret(saName, scrtName, namespace string) *v1.Secret {
	return &v1.Secret{
		Data: map[string][]byte{
			CertChainID:  []byte("fake cert chain"),
			PrivateKeyID: []byte("fake key"),
			RootCertID:   []byte("fake root cert"),
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"istio.io/service-account.name": saName},
			Name:        scrtName,
			Namespace:   namespace,
		},
		Type: IstioSecretType,
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

func TestSecretController(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}
	testCases := map[string]struct {
		existingSecret  *v1.Secret
		saToAdd         *v1.ServiceAccount
		saToDelete      *v1.ServiceAccount
		sasToUpdate     *updatedSas
		expectedActions []ktesting.Action
	}{
		"adding service account creates new secret": {
			saToAdd: createServiceAccount("test", "test-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "test-ns", createSecret("test", "istio.test", "test-ns")),
			},
		},
		"removing service account deletes existing secret": {
			saToDelete: createServiceAccount("deleted", "deleted-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewDeleteAction(gvr, "deleted-ns", "istio.deleted"),
			},
		},
		"updating service accounts does nothing if name and namespace are not changed": {
			sasToUpdate: &updatedSas{
				curSa: createServiceAccount("name", "ns"),
				oldSa: createServiceAccount("name", "ns"),
			},
			expectedActions: []ktesting.Action{},
		},
		"updating service accounts deletes old secret and creates a new one": {
			sasToUpdate: &updatedSas{
				curSa: createServiceAccount("new-name", "new-ns"),
				oldSa: createServiceAccount("old-name", "old-ns"),
			},
			expectedActions: []ktesting.Action{
				ktesting.NewDeleteAction(gvr, "old-ns", "istio.old-name"),
				ktesting.NewCreateAction(gvr, "new-ns", createSecret("new-name", "istio.new-name", "new-ns")),
			},
		},
		"adding new service account does not overwrite existing secret": {
			existingSecret:  createSecret("test", "istio.test", "test-ns"),
			saToAdd:         createServiceAccount("test", "test-ns"),
			expectedActions: []ktesting.Action{},
		},
	}

	for k, tc := range testCases {
		client := fake.NewSimpleClientset()
		controller := NewSecretController(fakeCa{}, client.CoreV1(), metav1.NamespaceAll)

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

		actions := client.Actions()
		if !reflect.DeepEqual(actions, tc.expectedActions) {
			t.Errorf("%s: expect actions to be \n\t%v\n but actual actions are \n\t%v", k, tc.expectedActions, actions)
		}
	}
}

func TestRecoverFromDeletedIstioSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	controller := NewSecretController(fakeCa{}, client.CoreV1(), metav1.NamespaceAll)
	scrt := createSecret("test", "istio.test", "test-ns")
	controller.scrtDeleted(scrt)

	gvr := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}
	expectedActions := []ktesting.Action{ktesting.NewCreateAction(gvr, "test-ns", scrt)}
	actions := client.Actions()
	if !reflect.DeepEqual(actions, expectedActions) {
		t.Errorf("expect actions to be \n\t%v\n but actual actions are \n\t%v", expectedActions, actions)
	}
}

func TestUpdateSecret(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}
	testCases := map[string]struct {
		expectedActions []ktesting.Action
		notAfter        time.Time
		rootCert        []byte
	}{
		"Does not update non-expiring secret": {
			expectedActions: []ktesting.Action{},
			notAfter:        time.Now().Add(time.Hour),
		},
		"Update expiring secret": {
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(gvr, "test-ns", createSecret("test", "istio.test", "test-ns")),
			},
			notAfter: time.Now().Add(-time.Second),
		},
		"Update secret with different root cert": {
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(gvr, "test-ns", createSecret("test", "istio.test", "test-ns")),
			},
			notAfter: time.Now().Add(time.Hour),
			rootCert: []byte("Outdated root cert"),
		},
	}

	for k, tc := range testCases {
		client := fake.NewSimpleClientset()
		controller := NewSecretController(fakeCa{}, client.CoreV1(), metav1.NamespaceAll)

		scrt := createSecret("test", "istio.test", "test-ns")
		if rc := tc.rootCert; rc != nil {
			scrt.Data[RootCertID] = rc
		}

		opts := ca.CertOptions{
			IsSelfSigned: true,
			NotAfter:     tc.notAfter,
			RSAKeySize:   512,
		}
		bs, _ := ca.GenCert(opts)
		scrt.Data[CertChainID] = bs

		controller.scrtUpdated(nil, scrt)

		actions := client.Actions()
		if !reflect.DeepEqual(actions, tc.expectedActions) {
			t.Errorf("%s: expect actions to be \n\t%v\n but actual actions are \n\t%v", k, tc.expectedActions, actions)
		}
	}
}
