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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	k8sKey        = []byte("fake private k8sKey")
	k8sCertChain  = []byte("fake cert chain")
	k8sSecretName = "test-scrt"
	k8sTestSecret = &v1.Secret{
		Data: map[string][]byte{
			ScrtCert: k8sCertChain,
			ScrtKey:  k8sKey,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sSecretName,
			Namespace: "test-namespace",
		},
		Type: IngressSecretType,
	}
)

// TestSecretFetcher verifies that secret fetcher is able to add kubernetes secret into local store,
// find secret by name, and delete secret by name.
func TestSecretFetcher(t *testing.T) {
	gSecretFetcher := SecretFetcher{
		UseCaClient: false,
	}
	gSecretFetcher.Init(fake.NewSimpleClientset().CoreV1())
	if gSecretFetcher.UseCaClient {
		t.Error("secretFetcher should not use ca client")
	}
	ch := make(chan struct{})
	gSecretFetcher.Run(ch)

	// Add a test secret and find the secret.
	gSecretFetcher.scrtAdded(k8sTestSecret)
	secret, ok := gSecretFetcher.FindIngressGatewaySecret(k8sSecretName)
	if !ok {
		t.Errorf("secretFetcher failed to find secret %v", k8sSecretName)
	}
	if k8sSecretName != secret.ResourceName {
		t.Errorf("resource name verification error: expected %s but got %s", k8sSecretName, secret.ResourceName)
	}
	if !bytes.Equal(k8sCertChain, secret.CertificateChain) {
		t.Errorf("cert chain verification error: expected %v but got %v", k8sCertChain, secret.CertificateChain)
	}
	if !bytes.Equal(k8sKey, secret.PrivateKey) {
		t.Errorf("k8sKey verification error: expected %v but got %v", k8sKey, secret.PrivateKey)
	}

	// Searching a non-existing secret should return false.
	if _, ok := gSecretFetcher.FindIngressGatewaySecret("non-existing-secret"); ok {
		t.Error("secretFetcher returns a secret non-existing-secret that should not exist")
	}

	// Delete test secret and verify that secret is removed from local store.
	gSecretFetcher.scrtDeleted(k8sTestSecret)
	if _, ok := gSecretFetcher.FindIngressGatewaySecret(k8sSecretName); ok {
		t.Errorf("secretFetcher fail to remove a deleted secret %s from local store", k8sSecretName)
	}
}
