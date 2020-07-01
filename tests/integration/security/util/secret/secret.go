//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package secret

import (
	"crypto/x509"
	"fmt"

	"istio.io/istio/pkg/test"

	"istio.io/istio/security/pkg/k8s/chiron"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"

	v1 "k8s.io/api/core/v1"
)

// ExamineDNSSecretOrFail calls ExamineDNSSecret and fails t if an error occurs.
func ExamineDNSSecretOrFail(t test.Failer, secret *v1.Secret, expectedID string) {
	t.Helper()
	if err := ExamineDNSSecret(secret, expectedID); err != nil {
		t.Fatal(err)
	}
}

// ExamineDNSSecret examines the content of a secret containing DNS secret to make sure that
// * Secret type is correctly set;
// * Key, certificate and CA root are correctly saved in the data section;
func ExamineDNSSecret(secret *v1.Secret, expectedID string) error {
	if secret.Type != chiron.IstioDNSSecretType {
		return fmt.Errorf(`unexpected value for the "type" annotation: expecting %v but got %v`,
			chiron.IstioDNSSecretType, secret.Type)
	}

	for _, key := range []string{ca.CertChainID, ca.RootCertID, ca.PrivateKeyID} {
		if _, exists := secret.Data[key]; !exists {
			return fmt.Errorf("%v does not exist in the data section", key)
		}
	}

	verifyFields := &util.VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		IsCA:        false,
		Host:        expectedID,
	}

	if err := util.VerifyCertificate(secret.Data[controller.PrivateKeyID],
		secret.Data[controller.CertChainID], secret.Data[controller.RootCertID],
		verifyFields); err != nil {
		return fmt.Errorf("certificate verification failed: %v", err)
	}

	return nil
}
