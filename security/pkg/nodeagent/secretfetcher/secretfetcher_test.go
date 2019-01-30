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
	"testing"

	"istio.io/istio/security/pkg/nodeagent/model"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	k8sKeyA               = []byte("fake private k8sKeyA")
	k8sCertChainA         = []byte("fake cert chain A")
	k8sSecretNameA        = "test-scrtA"
	k8sTestGenericSecretA = &v1.Secret{
		Data: map[string][]byte{
			GenericScrtCert: k8sCertChainA,
			GenericScrtKey:  k8sKeyA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	k8sKeyB               = []byte("k8sKeyB private fake")
	k8sCertChainB         = []byte("B chain cert fake")
	k8sTestGenericSecretB = &v1.Secret{
		Data: map[string][]byte{
			GenericScrtCert: k8sCertChainB,
			GenericScrtKey:  k8sKeyB,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	k8sKeyC           = []byte("fake private k8sKeyC")
	k8sCertChainC     = []byte("fake cert chain C")
	k8sSecretNameC    = "test-scrtC"
	k8sTestTLSSecretC = &v1.Secret{
		Data: map[string][]byte{
			TLSScrtCert: k8sCertChainC,
			TLSScrtKey:  k8sKeyC,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameC,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}

	k8sKeyD           = []byte("fake private k8sKeyD")
	k8sCertChainD     = []byte("fake cert chain D")
	k8sTestTLSSecretD = &v1.Secret{
		Data: map[string][]byte{
			TLSScrtCert: k8sCertChainD,
			TLSScrtKey:  k8sKeyD,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameC,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}
)

// TestSecretFetcher verifies that secret fetcher is able to add kubernetes secret into local store,
// find secret by name, and delete secret by name.
func TestSecretFetcher(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns model.SecretItem) {},
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
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedAddedSecret, &secretVersionOne)

	// Delete test secret and verify that key/cert pair in secret is removed from local store.
	expectedDeletedSecret := &model.SecretItem{
		ResourceName: k8sSecretNameA,
	}
	testDeleteSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedDeletedSecret)

	// Add test secret again and verify that key/cert pair is stored and version number is different.
	expectedSecret := &model.SecretItem{
		ResourceName:     k8sSecretNameA,
		CertificateChain: k8sCertChainA,
		PrivateKey:       k8sKeyA,
	}
	var secretVersionTwo string
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedSecret, &secretVersionTwo)
	if secretVersionTwo == secretVersionOne {
		t.Errorf("added secret should have different version")
	}

	// Update test secret and verify that key/cert pair is changed and version number is different.
	expectedUpdateSecret := &model.SecretItem{
		ResourceName:     k8sSecretNameA,
		CertificateChain: k8sCertChainB,
		PrivateKey:       k8sKeyB,
	}
	var secretVersionThree string
	testUpdateSecret(t, gSecretFetcher, k8sTestGenericSecretA, k8sTestGenericSecretB, expectedUpdateSecret, &secretVersionThree)
	if secretVersionThree == secretVersionTwo || secretVersionThree == secretVersionOne {
		t.Errorf("updated secret should have different version")
	}
}

// TestSecretFetcherTlsSecretFormat verifies that secret fetcher is able to extract key/cert
// from TLS secret format.
func TestSecretFetcherTlsSecretFormat(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns model.SecretItem) {},
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
		ResourceName:     k8sSecretNameC,
		CertificateChain: k8sCertChainC,
		PrivateKey:       k8sKeyC,
	}
	var secretVersion string
	testAddSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedAddedSecret, &secretVersion)

	// Delete test secret and verify that key/cert pair in secret is removed from local store.
	expectedDeletedSecret := &model.SecretItem{
		ResourceName: k8sSecretNameC,
	}
	testDeleteSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedDeletedSecret)

	// Add test secret again and verify that key/cert pair is stored and version number is different.
	expectedSecret := &model.SecretItem{
		ResourceName:     k8sSecretNameC,
		CertificateChain: k8sCertChainC,
		PrivateKey:       k8sKeyC,
	}
	testAddSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedSecret, &secretVersion)

	// Update test secret and verify that key/cert pair is changed and version number is different.
	expectedUpdateSecret := &model.SecretItem{
		ResourceName:     k8sSecretNameC,
		CertificateChain: k8sCertChainD,
		PrivateKey:       k8sKeyD,
	}
	var newSecretVersion string
	testUpdateSecret(t, gSecretFetcher, k8sTestTLSSecretC, k8sTestTLSSecretD, expectedUpdateSecret, &newSecretVersion)
	if secretVersion == newSecretVersion {
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

func testDeleteSecret(t *testing.T, sf *SecretFetcher, k8ssecret *v1.Secret, expectedSecret *model.SecretItem) {
	// Delete a test secret and find the secret.
	sf.scrtDeleted(k8ssecret)
	_, ok := sf.FindIngressGatewaySecret(expectedSecret.ResourceName)
	if ok {
		t.Errorf("secretFetcher found a deleted secret %v", expectedSecret.ResourceName)
	}
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
