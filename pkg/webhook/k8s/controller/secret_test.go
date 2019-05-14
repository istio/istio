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
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/webhook/model"
	"istio.io/istio/pkg/webhook/util"
)

func TestSecretController(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}
	testCases := map[string]struct {
		expectedActions []ktesting.Action
		secretToAdd     *v1.Secret
		secretToDelete  *v1.Secret
		shouldFail      bool
		namespace       string
		serviceName     string
	}{
		"creating new secret": {
			secretToAdd: createSerect("istio.test", "test-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "test-ns", createSerect("istio.test", "test-ns")),
			},
			shouldFail:  false,
			namespace:   "test-ns",
			serviceName: "test",
		},
		"deleting the existing secret": {
			secretToDelete: createSerect("istio.deleted", "deleted-ns"),
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "deleted-ns", createSerect("deleted", "deleted-ns")),
				ktesting.NewCreateAction(gvr, "deleted-ns", createSerect("deleted", "deleted-ns")),
			},
			shouldFail:  false,
			namespace:   "deleted-ns",
			serviceName: "deleted",
		},
	}

	for k, tc := range testCases {
		client := fake.NewSimpleClientset()

		controller, err := NewSecretController(createFakeCAArgs(), client.CoreV1(), tc.serviceName, tc.namespace)
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
		//create the secret with name istio.{tc.serviceName}
		controller.UpsertSecret()

		if tc.secretToAdd != nil {
			controller.scrtAdded(tc.secretToAdd)
		}

		if tc.secretToDelete != nil {
			controller.scrtDeleted(tc.secretToDelete)
		}

		if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
			t.Errorf("Case %q: %s", k, err.Error())
		}
	}
}

func TestSecretContent(t *testing.T) {
	srctName := "test"
	srctNamespace := "test-namespace"
	client := fake.NewSimpleClientset()
	controller, err := NewSecretController(createFakeCAArgs(), client.CoreV1(), srctName, srctNamespace)
	if err != nil {
		t.Errorf("Failed to create secret controller: %v", err)
	}
	controller.UpsertSecret()

	secret, err := client.CoreV1().Secrets(srctNamespace).Get(GetSecretName(srctName), metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to retrieve secret: %v", err)
	}
	_, err = util.ParsePemEncodedCertificate(secret.Data[RootCertID])
	if err != nil {
		t.Errorf("Root cert verification error: %v", secret.Data[RootCertID])
	}
	_, err = util.ParsePemEncodedCertificate(secret.Data[CertChainID])
	if err != nil {
		t.Errorf("Cert chain verification error: %v", secret.Data[CertChainID])
	}
}

func TestUpdateSecret(t *testing.T) {
	args := createFakeCAArgs()

	gvr := schema.GroupVersionResource{
		Resource: "secrets",
		Version:  "v1",
	}
	testCases := map[string]struct {
		expectedActions []ktesting.Action
		ttl             time.Duration
		serviceName     string
		namespace       string
	}{
		"Does not update non-expiring secret": {
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "test-ns1", createSerect("istio.test1", "test-ns1")),
				ktesting.NewGetAction(gvr, "test-ns1", "istio.test1"),
			},
			ttl:         time.Hour,
			serviceName: "test1",
			namespace:   "test-ns1",
		},
		"Update expired secret": {
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, "test-ns2", createSerect("istio.test2", "test-ns2")),
				ktesting.NewGetAction(gvr, "test-ns2", "istio.test2"),
				ktesting.NewUpdateAction(gvr, "test-ns2", createSerect("istio.test2", "test-ns2")),
			},
			ttl:         -time.Second,
			serviceName: "test2",
			namespace:   "test-ns2",
		},
	}

	for k, tc := range testCases {
		args.RequestedCertTTL = tc.ttl
		client := fake.NewSimpleClientset()
		controller, err := NewSecretController(args, client.CoreV1(), tc.serviceName, tc.namespace)
		if err != nil {
			t.Errorf("failed to create secret controller: %v", err)
		}

		controller.UpsertSecret()

		secret, err := client.CoreV1().Secrets(controller.namespace).Get(GetSecretName(controller.svcName), metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to retrieve secret: %v", err)
		}

		controller.scrtUpdated(nil, secret)

		if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
			t.Errorf("Case %q: %s", k, err.Error())
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

func createFakeCAArgs() *model.Args {
	return &model.Args{
		Org:              "MyOrg",
		RequestedCertTTL: 365 * 24 * time.Hour,
		RSAKeySize:       2048,
		IsSelfSigned:     true,
		CertFile:         "/etc/certs/cert-chain.pem",
		CertChainFile:    "/etc/certs/cert-chain.pem",
		KeyFile:          "/etc/certs/key.pem",
		RootCertFile:     "/etc/certs/root-cert.pem",
	}
}

func createSerect(srctName, namespace string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      srctName,
			Namespace: namespace,
		},
		Type: v1.SecretTypeOpaque,
	}
}
