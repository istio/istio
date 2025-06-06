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
	"istio.io/istio/security/pkg/pki/ca"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
)

// FakeCA is a mock of CertificateAuthority.
type FakeCA struct {
	SignedCert    []byte
	SignErr       *caerror.Error
	KeyCertBundle *util.KeyCertBundle
	ReceivedIDs   []string
}

// Sign returns the SignErr if SignErr is not nil, otherwise, it returns SignedCert.
func (ca *FakeCA) Sign(csr []byte, certOpts ca.CertOpts) ([]byte, error) {
	ca.ReceivedIDs = certOpts.SubjectIDs
	if ca.SignErr != nil {
		return nil, ca.SignErr
	}
	return ca.SignedCert, nil
}

// SignWithCertChain returns the SignErr if SignErr is not nil, otherwise, it returns SignedCert and the cert chain.
func (ca *FakeCA) SignWithCertChain(csr []byte, certOpts ca.CertOpts) ([]string, error) {
	if ca.SignErr != nil {
		return nil, ca.SignErr
	}
	cert := ca.SignedCert
	respCertChain := []string{string(cert)}
	if ca.KeyCertBundle != nil {
		respCertChain = append(respCertChain, string(ca.KeyCertBundle.GetCertChainPem()))
	}
	_, _, _, rootCertBytes := ca.GetCAKeyCertBundle().GetAll()
	if len(rootCertBytes) != 0 {
		respCertChain = append(respCertChain, string(rootCertBytes))
	}
	return respCertChain, nil
}

// GetCAKeyCertBundle returns KeyCertBundle if KeyCertBundle is not nil, otherwise, it returns an empty
// FakeKeyCertBundle.
func (ca *FakeCA) GetCAKeyCertBundle() *util.KeyCertBundle {
	if ca.KeyCertBundle == nil {
		return util.NewKeyCertBundleFromPem([]byte{}, []byte("foo"), []byte("fake"), []byte("fake"), nil)
	}
	return ca.KeyCertBundle
}
