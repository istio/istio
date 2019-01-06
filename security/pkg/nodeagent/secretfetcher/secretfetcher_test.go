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

package secretfetcher

import (
	"bytes"
	"istio.io/istio/security/pkg/nodeagent/model"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	k8sKeyA        = []byte("fake private k8sKeyA")
	k8sCertChainA  = []byte("fake cert chain A")
	k8sSecretNameA = "test-scrtA"
	k8sTestSecretA = &v1.Secret{
		Data: map[string][]byte{
			ScrtCert: k8sCertChainA,
			ScrtKey:  k8sKeyA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: IngressSecretType,
	}

	k8sKeyB        = []byte("k8sKeyB private fake")
	k8sCertChainB  = []byte("B chain cert fake")
	k8sTestSecretB = &v1.Secret{
		Data: map[string][]byte{
			ScrtCert: k8sCertChainB,
			ScrtKey:  k8sKeyB,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: IngressSecretType,
	}
)

// TestSecretFetcher verifies that secret fetcher is able to add kubernetes secret into local store,
// find secret by name, and delete secret by name.
func TestSecretFetcher(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
	}
	gSecretFetcher.Init(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	// Searching a non-existing secret should return false.
	if _, ok := gSecretFetcher.FindIngressGatewaySecret("non-existing-secret"); ok {
		t.Error("secretFetcher returns a secret non-existing-secret that should not exist")
	}

	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecret := &model.SecretItem{
		ResourceName:     k8sSecretNameA,
		CertificateChain: k8sCertChainA,
		PrivateKey:       k8sKeyA,
	}
	var secretVersionOne string
	testAddSecret(t, gSecretFetcher, k8sTestSecretA, expectedAddedSecret, &secretVersionOne)

	// Delete test secret and verify that key/cert pair in secret is removed from local store.
	expectedDeletedSecret := &model.SecretItem{
		ResourceName:     k8sSecretNameA,
	}
	var secretVersionTwo string
	testDeleteSecret(t, gSecretFetcher, k8sTestSecretA, expectedDeletedSecret, &secretVersionTwo)
	if secretVersionOne == secretVersionTwo {
		t.Errorf("added secret and deleted secret should have different version")
	}

	// Add test secret again and verify that key/cert pair is stored and version number is different.
	expectedSecret := &model.SecretItem{
		ResourceName:     k8sSecretNameA,
		CertificateChain: k8sCertChainA,
		PrivateKey:       k8sKeyA,
	}
	var secretVersionThree string
	testAddSecret(t, gSecretFetcher, k8sTestSecretA, expectedSecret, &secretVersionThree)
	if secretVersionThree == secretVersionTwo || secretVersionThree == secretVersionOne {
		t.Errorf("added secret should have different version")
	}

	// Update test secret and verify that key/cert pair is changed and version number is different.
	expectedUpdateSecret := &model.SecretItem{
		ResourceName:     k8sSecretNameA,
		CertificateChain: k8sCertChainB,
		PrivateKey:       k8sKeyB,
	}
	var secretVersionFour string
	testUpdateSecret(t, gSecretFetcher, k8sTestSecretA, k8sTestSecretB, expectedUpdateSecret, &secretVersionFour)
	if secretVersionFour == secretVersionTwo || secretVersionFour == secretVersionOne || secretVersionFour == secretVersionThree {
		t.Errorf("updated secret should have different version")
	}
}

func compareSecret(t *testing.T, secret, expectedSecret *model.SecretItem) {
	if expectedSecret.ResourceName != secret.ResourceName {
		t.Errorf("resource name verification error: expected %s but got %s", expectedSecret.ResourceName, secret.ResourceName)
	}
	if !bytes.Equal(expectedSecret.CertificateChain, secret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v", expectedSecret.CertificateChain, secret.CertificateChain)
	}
	if !bytes.Equal(expectedSecret.PrivateKey, secret.PrivateKey) {
		t.Errorf("private key verification error: expected %v but got %v", expectedSecret.PrivateKey, secret.PrivateKey)
	}
}

func testAddSecret(t *testing.T, sf *SecretFetcher, k8ssecret *v1.Secret, expectedSecret *model.SecretItem, version *string) {
	// Add a test secret and find the secret.
	sf.scrtAdded(k8ssecret)
	secret, ok := sf.FindIngressGatewaySecret(expectedSecret.ResourceName)
	if !ok {
		t.Errorf("secretFetcher failed to find secret %v", expectedSecret.ResourceName)
	}
	*version = secret.Version
	compareSecret(t, &secret, expectedSecret)
}

func testDeleteSecret(t *testing.T, sf *SecretFetcher, k8ssecret *v1.Secret, expectedSecret *model.SecretItem, version *string) {
	// Delete a test secret and find the secret.
	sf.scrtDeleted(k8ssecret)
	secret, ok := sf.FindIngressGatewaySecret(expectedSecret.ResourceName)
	if !ok {
		t.Errorf("secretFetcher failed to find a deleted secret %v", expectedSecret.ResourceName)
	}
	*version = secret.Version
	compareSecret(t, &secret, expectedSecret)
}

func testUpdateSecret(t *testing.T, sf *SecretFetcher, k8sOldsecret, k8sNewsecret *v1.Secret, expectedSecret *model.SecretItem, version *string) {
	// Add a test secret and find the secret.
	sf.scrtUpdated(k8sOldsecret, k8sNewsecret)
	secret, ok := sf.FindIngressGatewaySecret(expectedSecret.ResourceName)
	if !ok {
		t.Errorf("secretFetcher failed to find secret %v", expectedSecret.ResourceName)
	}
	*version = secret.Version
	compareSecret(t, &secret, expectedSecret)
}