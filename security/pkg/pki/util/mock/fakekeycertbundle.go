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
	"sync"

	"istio.io/istio/security/pkg/pki/util"
)

// FakeKeyCertBundle is a mocked KeyCertBundle for testing.
type FakeKeyCertBundle struct {
	CertBytes               []byte
	Cert                    *x509.Certificate
	PrivKeyBytes            []byte
	PrivKey                 *crypto.PrivateKey
	CertChainBytes          []byte
	RootCertBytes           []byte
	RootCertExpiryTimestamp float64
	CACertExpiryTimestamp   float64
	VerificationErr         error
	CertOptionsErr          error
	mutex                   sync.Mutex
}

// GetAllPem returns all key/cert PEMs in KeyCertBundle together. Getting all values together avoids inconsistency.
func (b *FakeKeyCertBundle) GetAllPem() (certBytes, privKeyBytes, certChainBytes, rootCertBytes []byte) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.CertBytes, b.PrivKeyBytes, b.CertChainBytes, b.RootCertBytes
}

// GetAll returns all key/cert in KeyCertBundle together. Getting all values together avoids inconsistency.
func (b *FakeKeyCertBundle) GetAll() (cert *x509.Certificate, privKey *crypto.PrivateKey, certChainBytes,
	rootCertBytes []byte) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.Cert, b.PrivKey, b.CertChainBytes, b.RootCertBytes
}

// VerifyAndSetAll returns VerificationErr if it is not nil. Otherwise, it returns all the key/certs in the bundle.
func (b *FakeKeyCertBundle) VerifyAndSetAll(certBytes, privKeyBytes, certChainBytes, rootCertBytes []byte) error {
	if b.VerificationErr != nil {
		return b.VerificationErr
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.CertBytes = certBytes
	b.PrivKeyBytes = privKeyBytes
	b.CertChainBytes = certChainBytes
	b.RootCertBytes = rootCertBytes
	return nil
}

// GetCertChainPem returns CertChainBytes.
func (b *FakeKeyCertBundle) GetCertChainPem() []byte {
	return b.CertChainBytes
}

// GetRootCertPem returns RootCertBytes.
func (b *FakeKeyCertBundle) GetRootCertPem() []byte {
	return b.RootCertBytes
}

// CertOptions returns CertOptionsErr if it is not nil. Otherwise it returns an empty CertOptions.
func (b *FakeKeyCertBundle) CertOptions() (*util.CertOptions, error) {
	if b.CertOptionsErr != nil {
		return nil, b.CertOptionsErr
	}
	return &util.CertOptions{}, nil
}

// ExtractRootCertExpiryTimestamp returns the unix timestamp when the root becomes expires.
func (b *FakeKeyCertBundle) ExtractRootCertExpiryTimestamp() (float64, error) {
	return b.RootCertExpiryTimestamp, nil
}

// ExtractCACertExpiryTimestamp returns the unix timestamp when the CA cert becomes expires.
func (b *FakeKeyCertBundle) ExtractCACertExpiryTimestamp() (float64, error) {
	return b.CACertExpiryTimestamp, nil
}
