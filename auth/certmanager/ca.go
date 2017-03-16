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

package certmanager

import (
	"crypto"
	"crypto/x509"
	"fmt"
	"time"
)

const (
	// The time to live for issued certificate.
	certTTL = time.Hour
	// The time to live for self-signed root CA certificate.
	rootCertTTL = 24 * time.Hour
)

// CertificateAuthority contains methods to be supported by a CA.
type CertificateAuthority interface {
	Generate(name, namespace string) (cert, key []byte)
}

// IstioCA generates keys and certificates for Istio identities.
type IstioCA struct {
	signerCert *x509.Certificate
	signerKey  crypto.PrivateKey
}

// NewSelfSignedIstioCA returns a new IstioCA instance using self-signed certificate.
func NewSelfSignedIstioCA() *IstioCA {
	now := time.Now()
	options := CertOptions{
		NotBefore:    now,
		NotAfter:     now.Add(rootCertTTL),
		Org:          "istio.io",
		IsCA:         true,
		IsSelfSigned: true,
	}
	pemCert, pemKey := GenCert(options)
	cert, key := parsePemEncodedCertificateAndKey(pemCert, pemKey)

	return NewIstioCA(cert, key)
}

// NewIstioCA returns a new IstioCA instance.
func NewIstioCA(cert *x509.Certificate, key crypto.PrivateKey) *IstioCA {
	return &IstioCA{
		signerCert: cert,
		signerKey:  key,
	}
}

// Generate returns a key and a certificate of an Istio identity defined by
// the name and the namespace.
func (ca IstioCA) Generate(name, namepsace string) (cert, key []byte) {
	// Currently the domain is always set to "cluster.local" since we only
	// support in-cluster identities.
	id := fmt.Sprintf("%s:%s.%s.cluster.local", uriScheme, name, namepsace)
	now := time.Now()
	options := CertOptions{
		Host:         id,
		NotBefore:    now,
		NotAfter:     now.Add(certTTL),
		SignerCert:   ca.signerCert,
		SignerPriv:   ca.signerKey,
		IsCA:         false,
		IsSelfSigned: false,
	}
	return GenCert(options)
}
