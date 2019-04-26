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
	k8sCaCertA            = []byte("fake root cert A")
	k8sSecretNameA        = "test-scrtA"
	k8sTestGenericSecretA = &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainA,
			genericScrtKey:    k8sKeyA,
			genericScrtCaCert: k8sCaCertA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}
	k8sInvalidTestGenericSecretA = &v1.Secret{
		Data: map[string][]byte{
			genericScrtKey:    k8sKeyA,
			genericScrtCaCert: k8sCaCertA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	k8sKeyB               = []byte("k8sKeyB private fake")
	k8sCertChainB         = []byte("B chain cert fake")
	k8sCaCertB            = []byte("B cert root fake")
	k8sTestGenericSecretB = &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainB,
			genericScrtKey:    k8sKeyB,
			genericScrtCaCert: k8sCaCertB,
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
			tlsScrtCert: k8sCertChainC,
			tlsScrtKey:  k8sKeyC,
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
			tlsScrtCert: k8sCertChainD,
			tlsScrtKey:  k8sKeyD,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameC,
			Namespace: "test-namespace",
		},
		Type: "test-tls-secret",
	}
)

type expectedSecret struct {
	exist  bool
	secret *model.SecretItem
}

// TestSecretFetcher verifies that secret fetcher is able to add kubernetes secret into local store,
// find secret by name, and delete secret by name.
func TestSecretFetcher(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns model.SecretItem) {},
	}
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
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
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainA,
				PrivateKey:       k8sKeyA,
			},
		},
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName: k8sSecretNameA + IngressGatewaySdsCaSuffix,
				RootCert:     k8sCaCertA,
			},
		},
	}
	var secretVersionOne string
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedAddedSecrets, &secretVersionOne)

	// Delete test secret and verify that key/cert pair in secret is removed from local store.
	expectedDeletedSecrets := []expectedSecret{
		{
			exist:  false,
			secret: &model.SecretItem{ResourceName: k8sSecretNameA},
		},
		{
			exist:  false,
			secret: &model.SecretItem{ResourceName: k8sSecretNameA + IngressGatewaySdsCaSuffix},
		},
	}
	testDeleteSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedDeletedSecrets)

	// Add test secret again and verify that key/cert pair is stored and version number is different.
	var secretVersionTwo string
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretA, expectedAddedSecrets, &secretVersionTwo)
	if secretVersionTwo == secretVersionOne {
		t.Errorf("added secret should have different version")
	}

	// Update test secret and verify that key/cert pair is changed and version number is different.
	expectedUpdateSecrets := []expectedSecret{
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainB,
				PrivateKey:       k8sKeyB,
			},
		},
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName: k8sSecretNameA + IngressGatewaySdsCaSuffix,
				RootCert:     k8sCaCertB,
			},
		},
	}
	var secretVersionThree string
	testUpdateSecret(t, gSecretFetcher, k8sTestGenericSecretA, k8sTestGenericSecretB, expectedUpdateSecrets, &secretVersionThree)
	if secretVersionThree == secretVersionTwo || secretVersionThree == secretVersionOne {
		t.Errorf("updated secret should have different version")
	}
}

// TestSecretFetcherInvalidSecret verifies that if a secret does not have key or cert, secret fetcher
// will skip adding or updating with the invalid secret.
func TestSecretFetcherInvalidSecret(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns model.SecretItem) {},
	}
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	gSecretFetcher.scrtAdded(k8sInvalidTestGenericSecretA)
	if _, ok := gSecretFetcher.FindIngressGatewaySecret(k8sInvalidTestGenericSecretA.GetName()); ok {
		t.Errorf("invalid secret should not be added into secret fetcher.")
	}

	var secretVersionOne string
	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainB,
				PrivateKey:       k8sKeyB,
			},
		},
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName: k8sSecretNameA + IngressGatewaySdsCaSuffix,
				RootCert:     k8sCaCertB,
			},
		},
	}
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretB, expectedAddedSecrets, &secretVersionOne)
	// Try to update with an invalid secret, and verify that the invalid secret is not added.
	// Secret fetcher still owns old secret k8sTestGenericSecretB.
	gSecretFetcher.scrtUpdated(k8sTestGenericSecretB, k8sInvalidTestGenericSecretA)
	secret, ok := gSecretFetcher.FindIngressGatewaySecret(k8sSecretNameA)
	if !ok {
		t.Errorf("secretFetcher failed to find secret %s", k8sSecretNameA)
	}
	if secretVersionOne != secret.Version {
		t.Errorf("version number does not match.")
	}
	if !bytes.Equal(k8sCertChainB, secret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v", k8sCertChainB, secret.CertificateChain)
	}
	if !bytes.Equal(k8sKeyB, secret.PrivateKey) {
		t.Errorf("private key verification error: expected %v but got %v", k8sKeyB, secret.PrivateKey)
	}
	casecret, ok := gSecretFetcher.FindIngressGatewaySecret(k8sSecretNameA + IngressGatewaySdsCaSuffix)
	if !ok || !bytes.Equal(k8sCaCertB, casecret.RootCert) {
		t.Errorf("root cert verification error: expected %v but got %v", k8sCaCertB, secret.RootCert)
	}
}

// TestSecretFetcherSkipSecret verifies that secret fetcher skips adding secrets if that secret
// is not a gateway secret.
func TestSecretFetcherSkipSecret(t *testing.T) {
	gSecretFetcher := &SecretFetcher{
		UseCaClient: false,
		DeleteCache: func(secretName string) {},
		UpdateCache: func(secretName string, ns model.SecretItem) {},
	}
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	istioPrefixSecret := &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainA,
			genericScrtKey:    k8sKeyA,
			genericScrtCaCert: k8sCaCertA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istioSecret",
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	gSecretFetcher.scrtAdded(istioPrefixSecret)
	if _, ok := gSecretFetcher.FindIngressGatewaySecret(istioPrefixSecret.GetName()); ok {
		t.Errorf("istio secret should not be added into secret fetcher.")
	}

	prometheusPrefixSecret := &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainA,
			genericScrtKey:    k8sKeyA,
			genericScrtCaCert: k8sCaCertA,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheusSecret",
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}

	gSecretFetcher.scrtAdded(prometheusPrefixSecret)
	if _, ok := gSecretFetcher.FindIngressGatewaySecret(prometheusPrefixSecret.GetName()); ok {
		t.Errorf("prometheus secret should not be added into secret fetcher.")
	}

	var secretVersionOne string
	// Add test secret and verify that key/cert pair is stored.
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName:     k8sSecretNameA,
				CertificateChain: k8sCertChainB,
				PrivateKey:       k8sKeyB,
			},
		},
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName: k8sSecretNameA + IngressGatewaySdsCaSuffix,
				RootCert:     k8sCaCertB,
			},
		},
	}
	testAddSecret(t, gSecretFetcher, k8sTestGenericSecretB, expectedAddedSecrets, &secretVersionOne)
	// Try to update with an invalid secret, and verify that the invalid secret is not added.
	// Secret fetcher still owns old secret k8sTestGenericSecretB.
	tokenSecretB := &v1.Secret{
		Data: map[string][]byte{
			genericScrtCert:   k8sCertChainB,
			genericScrtKey:    k8sKeyB,
			genericScrtCaCert: k8sCaCertB,
			scrtTokenField:    []byte("fake token"),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretNameA,
			Namespace: "test-namespace",
		},
		Type: "test-secret",
	}
	gSecretFetcher.scrtUpdated(k8sTestGenericSecretB, tokenSecretB)
	secret, ok := gSecretFetcher.FindIngressGatewaySecret(k8sSecretNameA)
	if !ok {
		t.Errorf("secretFetcher failed to find secret %s", k8sSecretNameA)
	}
	if secretVersionOne != secret.Version {
		t.Errorf("version number does not match.")
	}
	if !bytes.Equal(k8sCertChainB, secret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v", k8sCertChainB, secret.CertificateChain)
	}
	if !bytes.Equal(k8sKeyB, secret.PrivateKey) {
		t.Errorf("private key verification error: expected %v but got %v", k8sKeyB, secret.PrivateKey)
	}
	casecret, ok := gSecretFetcher.FindIngressGatewaySecret(k8sSecretNameA + IngressGatewaySdsCaSuffix)
	if !ok || !bytes.Equal(k8sCaCertB, casecret.RootCert) {
		t.Errorf("root cert verification error: expected %v but got %v", k8sCaCertB, secret.RootCert)
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
	gSecretFetcher.InitWithKubeClient(fake.NewSimpleClientset().CoreV1())
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
	expectedAddedSecrets := []expectedSecret{
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName:     k8sSecretNameC,
				CertificateChain: k8sCertChainC,
				PrivateKey:       k8sKeyC,
			},
		},
		{
			exist: false,
			secret: &model.SecretItem{
				ResourceName: k8sSecretNameC + IngressGatewaySdsCaSuffix,
			},
		},
	}
	var secretVersion string
	testAddSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedAddedSecrets, &secretVersion)

	// Delete test secret and verify that key/cert pair in secret is removed from local store.
	expectedDeletedSecret := []expectedSecret{
		{
			exist:  false,
			secret: &model.SecretItem{ResourceName: k8sSecretNameC},
		},
		{
			exist:  false,
			secret: &model.SecretItem{ResourceName: k8sSecretNameC + IngressGatewaySdsCaSuffix},
		},
	}
	testDeleteSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedDeletedSecret)

	// Add test secret again and verify that key/cert pair is stored and version number is different.
	testAddSecret(t, gSecretFetcher, k8sTestTLSSecretC, expectedAddedSecrets, &secretVersion)

	// Update test secret and verify that key/cert pair is changed and version number is different.
	expectedUpdateSecret := []expectedSecret{
		{
			exist: true,
			secret: &model.SecretItem{
				ResourceName:     k8sSecretNameC,
				CertificateChain: k8sCertChainD,
				PrivateKey:       k8sKeyD,
			},
		},
		{
			exist:  false,
			secret: &model.SecretItem{ResourceName: k8sSecretNameC + IngressGatewaySdsCaSuffix},
		},
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
	if !bytes.Equal(expectedSecret.RootCert, secret.RootCert) {
		t.Errorf("root cert verification error: expected %v but got %v", expectedSecret.RootCert, secret.RootCert)
	}
}

func testAddSecret(t *testing.T, sf *SecretFetcher, k8ssecret *v1.Secret, expectedSecrets []expectedSecret, version *string) {
	// Add a test secret and find the secret.
	sf.scrtAdded(k8ssecret)
	for _, es := range expectedSecrets {
		secret, ok := sf.FindIngressGatewaySecret(es.secret.ResourceName)
		if es.exist != ok {
			t.Errorf("Unexpected secret %s, expected to exist: %v but got: %v", es.secret.ResourceName, es.exist, ok)
		}
		if es.exist {
			*version = secret.Version
			compareSecret(t, &secret, es.secret)
		}
	}
}

func testDeleteSecret(t *testing.T, sf *SecretFetcher, k8ssecret *v1.Secret, expectedSecrets []expectedSecret) {
	// Delete a test secret and find the secret.
	sf.scrtDeleted(k8ssecret)
	for _, es := range expectedSecrets {
		_, ok := sf.FindIngressGatewaySecret(es.secret.ResourceName)
		if ok {
			t.Errorf("secretFetcher found a deleted secret %v", es.secret.ResourceName)
		}
	}
}

func testUpdateSecret(t *testing.T, sf *SecretFetcher, k8sOldsecret, k8sNewsecret *v1.Secret, expectedSecrets []expectedSecret, version *string) {
	// Add a test secret and find the secret.
	sf.scrtUpdated(k8sOldsecret, k8sNewsecret)
	for _, es := range expectedSecrets {
		secret, ok := sf.FindIngressGatewaySecret(es.secret.ResourceName)
		if es.exist != ok {
			t.Errorf("secretFetcher failed to find secret %s, expected to exist: %v but got: %v", es.secret.ResourceName, es.exist, ok)
		}
		if es.exist {
			*version = secret.Version
			compareSecret(t, &secret, es.secret)
		}
	}
}
