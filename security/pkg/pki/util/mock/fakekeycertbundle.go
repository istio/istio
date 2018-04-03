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

package mock

import (
	"crypto"
	"crypto/x509"
	"sync"
)

// FakeKeyCertBundle is a mocked KeyCertBundle for testing.
type FakeKeyCertBundle struct {
	CertBytes      []byte
	Cert           *x509.Certificate
	PrivKeyBytes   []byte
	PrivKey        *crypto.PrivateKey
	CertChainBytes []byte
	RootCertBytes  []byte
	ReturnErr      error
	mutex          sync.Mutex
}

// GetAllPem returns all key/cert PEMs in KeyCertBundle together. Getting all values together avoids inconsistancy.
func (b *FakeKeyCertBundle) GetAllPem() (certBytes, privKeyBytes, certChainBytes, rootCertBytes []byte) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.CertBytes, b.PrivKeyBytes, b.CertChainBytes, b.RootCertBytes
}

// GetAll returns all key/cert in KeyCertBundle together. Getting all values together avoids inconsistancy.
func (b *FakeKeyCertBundle) GetAll() (cert *x509.Certificate, privKey *crypto.PrivateKey, certChainBytes,
	rootCertBytes []byte) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.Cert, b.PrivKey, b.CertChainBytes, b.RootCertBytes
}

// VerifyAndSetAll verifies the key/certs, and sets all key/certs in KeyCertBundle together.
// Setting all values together avoids inconsistancy.
func (b *FakeKeyCertBundle) VerifyAndSetAll(certBytes, privKeyBytes, certChainBytes, rootCertBytes []byte) error {
	if b.ReturnErr != nil {
		return b.ReturnErr
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.CertBytes = certBytes
	b.PrivKeyBytes = privKeyBytes
	b.CertChainBytes = certChainBytes
	b.RootCertBytes = rootCertBytes
	return nil
}
