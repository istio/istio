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

package util

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"reflect"
	"strings"
)

const (
	blockTypeECPrivateKey    = "EC PRIVATE KEY"
	blockTypeRSAPrivateKey   = "RSA PRIVATE KEY" // PKCS#1 private key
	blockTypePKCS8PrivateKey = "PRIVATE KEY"     // PKCS#8 plain private key
)

// ParsePemEncodedCertificate constructs a `x509.Certificate` object using the
// given a PEM-encoded certificate.
func ParsePemEncodedCertificate(certBytes []byte) (*x509.Certificate, error) {
	cb, _ := pem.Decode(certBytes)
	if cb == nil {
		return nil, fmt.Errorf("invalid PEM encoded certificate")
	}

	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse X.509 certificate")
	}

	return cert, nil
}

// ParsePemEncodedCertificateChain constructs a slice of `x509.Certificate`
// objects using the given a PEM-encoded certificate chain.
func ParsePemEncodedCertificateChain(certBytes []byte) ([]*x509.Certificate, error) {
	var (
		certs []*x509.Certificate
		cb    *pem.Block
	)
	for {
		cb, certBytes = pem.Decode(certBytes)
		if cb == nil {
			break
		}
		cert, err := x509.ParseCertificate(cb.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse X.509 certificate")
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no PEM encoded X.509 certificates parsed")
	}
	return certs, nil
}

// ParsePemEncodedCSR constructs a `x509.CertificateRequest` object using the
// given PEM-encoded certificate signing request.
func ParsePemEncodedCSR(csrBytes []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(csrBytes)
	if block == nil {
		return nil, fmt.Errorf("certificate signing request is not properly encoded")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse X.509 certificate signing request")
	}
	return csr, nil
}

// ParsePemEncodedKey takes a PEM-encoded key and parsed the bytes into a `crypto.PrivateKey`.
func ParsePemEncodedKey(keyBytes []byte) (crypto.PrivateKey, error) {
	kb, _ := pem.Decode(keyBytes)
	if kb == nil {
		return nil, fmt.Errorf("invalid PEM-encoded key")
	}

	switch kb.Type {
	case blockTypeECPrivateKey:
		key, err := x509.ParseECPrivateKey(kb.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse the ECDSA private key")
		}
		return key, nil
	case blockTypeRSAPrivateKey:
		key, err := x509.ParsePKCS1PrivateKey(kb.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse the RSA private key")
		}
		return key, nil
	case blockTypePKCS8PrivateKey:
		key, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse the PKCS8 private key")
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type for a private key: %s", kb.Type)
	}
}

// GetRSAKeySize returns the size if it is RSA key, otherwise it returns an error.
func GetRSAKeySize(privKey crypto.PrivateKey) (int, error) {
	if t := reflect.TypeOf(privKey); t != reflect.TypeOf(&rsa.PrivateKey{}) {
		return 0, fmt.Errorf("key type is not RSA: %v", t)
	}
	pkey := privKey.(*rsa.PrivateKey)
	return pkey.N.BitLen(), nil
}

// IsSupportedECPrivateKey is a predicate returning true if the private key is EC based
func IsSupportedECPrivateKey(privKey *crypto.PrivateKey) bool {
	switch (*privKey).(type) {
	// this should agree with var SupportedECSignatureAlgorithms
	case *ecdsa.PrivateKey:
		return true
	default:
		return false
	}
}

// PemCertBytestoString: takes an array of PEM certs in bytes and returns a string array in the same order with
// trailing newline characters removed
func PemCertBytestoString(caCerts []byte) []string {
	certs := []string{}
	var cert string
	pemBlock := caCerts
	for block, rest := pem.Decode(pemBlock); block != nil && len(block.Bytes) != 0; block, rest = pem.Decode(pemBlock) {
		if len(rest) == 0 {
			cert = strings.TrimPrefix(strings.TrimSuffix(string(pemBlock), "\n"), "\n")
			certs = append(certs, cert)
			break
		}
		cert = string(pemBlock[0 : len(pemBlock)-len(rest)])
		cert = strings.TrimPrefix(strings.TrimSuffix(cert, "\n"), "\n")
		certs = append(certs, cert)
		pemBlock = rest
	}
	return certs
}
