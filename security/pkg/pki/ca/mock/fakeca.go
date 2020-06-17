// Copyright Istio Authors
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

package mock

import (
	"crypto"
	"crypto/x509"
	"time"

	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/pki/util/mock"
)

// FakeCA is a mock of CertificateAuthority.
type FakeCA struct {
	SignedCert    []byte
	SignErr       *caerror.Error
	KeyCertBundle util.KeyCertBundle
	ReceivedIDs   []string
}

// Sign returns the SignErr if SignErr is not nil, otherwise, it returns SignedCert.
func (ca *FakeCA) Sign(csr []byte, identities []string, lifetime time.Duration, forCA bool) ([]byte, error) {
	ca.ReceivedIDs = identities
	if ca.SignErr != nil {
		return nil, ca.SignErr
	}
	return ca.SignedCert, nil
}

// SignWithCertChain returns the SignErr if SignErr is not nil, otherwise, it returns SignedCert and the cert chain.
func (ca *FakeCA) SignWithCertChain(csr []byte, identities []string, lifetime time.Duration, forCA bool) ([]byte, error) {
	if ca.SignErr != nil {
		return nil, ca.SignErr
	}
	cert := ca.SignedCert
	if ca.KeyCertBundle != nil {
		cert = append(cert, ca.KeyCertBundle.GetCertChainPem()...)
	}
	return cert, nil
}

// GetCAKeyCertBundle returns KeyCertBundle if KeyCertBundle is not nil, otherwise, it returns an empty
// FakeKeyCertBundle.
func (ca *FakeCA) GetCAKeyCertBundle() util.KeyCertBundle {
	if ca.KeyCertBundle == nil {
		priv := crypto.PrivateKey("foo")
		return &mock.FakeKeyCertBundle{
			Cert:           &x509.Certificate{},
			PrivKey:        &priv,
			CertChainBytes: []byte("fake"),
			RootCertBytes:  []byte("fake"),
		}
	}
	return ca.KeyCertBundle
}
