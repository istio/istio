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

package util

import (
	"bytes"
	"encoding/pem"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	mockutil "istio.io/istio/security/pkg/pki/util/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

const (
	secret    = "sidecar-injector-certs"
	namespace = "istio-system"
)

func TestGenWebhookSecrets(t *testing.T) {
	client := fake.NewSimpleClientset()
	cert := mockutil.FakeKeyCertBundle{
		CertBytes:      []byte("fake CA cert"),
		PrivKeyBytes:   []byte("fake private key"),
		CertChainBytes: []byte("fake cert chain"),
		RootCertBytes:  []byte("fake root cert"),
	}
	ca := &mockca.FakeCA{
		SignedCert:    []byte("fake signed cert"),
		SignErr:       nil,
		KeyCertBundle: &cert,
	}
	err := GenerateWebhookSecrets(client.CoreV1(), ca, []WebhookIdentity{
		{
			Service:   "istio-sidecar-injector",
			Secret:    secret,
			Namespace: namespace,
		}})
	if err != nil {
		t.Errorf("Failed to generate secrets: %v", err)
		return
	}
	expected, err := client.CoreV1().Secrets(namespace).Get(secret, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get secret: %v", err)
		return
	}
	data := expected.Data
	certBytes, ok := data["cert.pem"]
	if !ok || !bytes.Equal(certBytes, ca.SignedCert) {
		t.Errorf("Invalid cert.pem")
	}
	caBytes, ok := data["ca.pem"]
	if !ok || !bytes.Equal(caBytes, cert.CertBytes) {
		t.Errorf("Invalid ca.pem")
	}
	keyBytes, ok := data["key.pem"]
	if !ok || !validateKey(t, keyBytes) {
		t.Errorf("Invalid key.pem")
	}
}

func validateKey(t *testing.T, key []byte) bool {
	p, _ := pem.Decode(key)
	if p == nil {
		t.Error("Cannot decode the key.")
		return false
	}
	return true
}
