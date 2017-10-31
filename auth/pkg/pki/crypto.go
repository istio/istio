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

package pki

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

const (
	blockTypeECParameters  = "EC PARAMETERS"
	blockTypeRSAPrivateKey = "RSA PRIVATE KEY"
)

// ParsePemEncodedCertificate constructs a `x509.Certificate` object using the
// given a PEM-encoded certificate.
func ParsePemEncodedCertificate(certBytes []byte) (*x509.Certificate, error) {
	cb, _ := pem.Decode(certBytes)
	if cb == nil {
		return nil, fmt.Errorf("Invalid PEM encoded certificate")
	}

	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse X.509 certificate")
	}

	return cert, nil
}

// ParsePemEncodedCSR constructs a `x509.CertificateRequest` object using the
// given PEM-encoded certificate signing request.
func ParsePemEncodedCSR(csrBytes []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(csrBytes)
	if block == nil {
		return nil, fmt.Errorf("Certificate signing request is not properly encoded")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse X.509 certificate signing request")
	}
	return csr, nil
}

// ParsePemEncodedKey takes a PEM-encoded key and parsed the bytes into a `crypto.PrivateKey`.
func ParsePemEncodedKey(keyBytes []byte) (crypto.PrivateKey, error) {
	kb, _ := pem.Decode(keyBytes)
	if kb == nil {
		return nil, fmt.Errorf("Invalid PEM-encoded key")
	}

	switch kb.Type {
	case blockTypeECParameters:
		key, err := x509.ParseECPrivateKey(kb.Bytes)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse the ECDSA private key")
		}
		return key, nil
	case blockTypeRSAPrivateKey:
		key, err := x509.ParsePKCS1PrivateKey(kb.Bytes)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse the RSA private key")
		}
		return key, nil
	default:
		return nil, fmt.Errorf("Unsupported PEM block type for a private key: %s", kb.Type)
	}
}
