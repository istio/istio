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

// Provides utility methods to generate X.509 certificates with different
// options. This implementation is Largely inspired from
// https://golang.org/src/crypto/tls/generate_cert.go.

package util

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// KeyCertBundle stores the cert, private key, cert chain and root cert for an entity. It is thread safe.
// The cert and privKey should be a public/private key pair.
// The cert should be verifiable from the rootCert through the certChain.
// cert and priveKey are pointers to the cert/key parsed from certBytes/privKeyBytes.
type KeyCertBundle struct {
	certBytes      []byte
	cert           *x509.Certificate
	privKeyBytes   []byte
	privKey        *crypto.PrivateKey
	certChainBytes []byte
	rootCertBytes  []byte
	crlBytes       []byte
	// mutex protects the R/W to all keys and certs.
	mutex sync.RWMutex
}

// NewKeyCertBundleFromPem returns a new KeyCertBundle, regardless of whether or not the key can be correctly parsed.
func NewKeyCertBundleFromPem(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes []byte) *KeyCertBundle {
	bundle := &KeyCertBundle{}
	bundle.setAllFromPem(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes)
	return bundle
}

// NewVerifiedKeyCertBundleFromPem returns a new KeyCertBundle, or error if the provided certs failed the
// verification.
func NewVerifiedKeyCertBundleFromPem(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes []byte) (
	*KeyCertBundle, error,
) {
	bundle := &KeyCertBundle{}
	if err := bundle.VerifyAndSetAll(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes); err != nil {
		return nil, err
	}
	return bundle, nil
}

// NewVerifiedKeyCertBundleFromFile returns a new KeyCertBundle, or error if the provided certs failed the
// verification.
func NewVerifiedKeyCertBundleFromFile(
	certFile, privKeyFile string,
	certChainFiles []string,
	rootCertFile, crlFile string,
) (
	*KeyCertBundle, error,
) {
	certBytes, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	privKeyBytes, err := os.ReadFile(privKeyFile)
	if err != nil {
		return nil, err
	}
	var certChainBytes []byte
	if len(certChainFiles) > 0 {
		for _, f := range certChainFiles {
			var b []byte

			if b, err = os.ReadFile(f); err != nil {
				return nil, err
			}

			certChainBytes = append(certChainBytes, b...)
		}
	}
	rootCertBytes, err := os.ReadFile(rootCertFile)
	if err != nil {
		return nil, err
	}

	// Read CRL file if provided
	crlBytes, crlErr := gerCRLBytesFromFile(crlFile)
	if crlErr != nil {
		return nil, crlErr
	}

	return NewVerifiedKeyCertBundleFromPem(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes)
}

// NewKeyCertBundleWithRootCertFromFile returns a new KeyCertBundle with the root cert without verification.
func NewKeyCertBundleWithRootCertFromFile(rootCertFile string) (*KeyCertBundle, error) {
	var rootCertBytes []byte
	var err error
	if rootCertFile == "" {
		rootCertBytes = []byte{}
	} else {
		rootCertBytes, err = os.ReadFile(rootCertFile)
		if err != nil {
			return nil, err
		}
	}
	return &KeyCertBundle{
		certBytes:      []byte{},
		cert:           nil,
		privKeyBytes:   []byte{},
		privKey:        nil,
		certChainBytes: []byte{},
		rootCertBytes:  rootCertBytes,
	}, nil
}

// GetAllPem returns all key/cert PEMs in KeyCertBundle together. Getting all values together avoids inconsistency.
func (b *KeyCertBundle) GetAllPem() (certBytes, privKeyBytes, certChainBytes, rootCertBytes []byte) {
	b.mutex.RLock()
	certBytes = copyBytes(b.certBytes)
	privKeyBytes = copyBytes(b.privKeyBytes)
	certChainBytes = copyBytes(b.certChainBytes)
	rootCertBytes = copyBytes(b.rootCertBytes)
	b.mutex.RUnlock()
	return certBytes, privKeyBytes, certChainBytes, rootCertBytes
}

// GetAll returns all key/cert in KeyCertBundle together. Getting all values together avoids inconsistency.
// NOTE: Callers should not modify the content of cert and privKey.
func (b *KeyCertBundle) GetAll() (cert *x509.Certificate, privKey *crypto.PrivateKey, certChainBytes,
	rootCertBytes []byte,
) {
	b.mutex.RLock()
	cert = b.cert
	privKey = b.privKey
	certChainBytes = copyBytes(b.certChainBytes)
	rootCertBytes = copyBytes(b.rootCertBytes)
	b.mutex.RUnlock()
	return cert, privKey, certChainBytes, rootCertBytes
}

// GetCertChainPem returns the certificate chain PEM.
func (b *KeyCertBundle) GetCertChainPem() []byte {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return copyBytes(b.certChainBytes)
}

// GetRootCertPem returns the root certificate PEM.
func (b *KeyCertBundle) GetRootCertPem() []byte {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return copyBytes(b.rootCertBytes)
}

// GetCRLPem returns the CRL PEM.
func (b *KeyCertBundle) GetCRLPem() []byte {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return copyBytes(b.crlBytes)
}

// VerifyAndSetAll verifies the key/certs, and sets all key/certs in KeyCertBundle together.
// Setting all values together avoids inconsistency.
func (b *KeyCertBundle) VerifyAndSetAll(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes []byte) error {
	if err := Verify(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes); err != nil {
		return err
	}
	b.setAllFromPem(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes)
	return nil
}

// Setting all values together avoids inconsistency.
func (b *KeyCertBundle) setAllFromPem(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes []byte) {
	b.mutex.Lock()
	b.certBytes = copyBytes(certBytes)
	b.privKeyBytes = copyBytes(privKeyBytes)
	b.certChainBytes = copyBytes(certChainBytes)
	b.rootCertBytes = copyBytes(rootCertBytes)

	// CRL is optional and used with plugged in CA, if not provided, it will be nil
	if len(crlBytes) != 0 {
		b.crlBytes = copyBytes(crlBytes)
	}

	// cert and privKey are always reset to point to new addresses. This avoids modifying the pointed structs that
	// could be still used outside of the class.
	b.cert, _ = ParsePemEncodedCertificate(certBytes)
	privKey, _ := ParsePemEncodedKey(privKeyBytes)
	b.privKey = &privKey
	b.mutex.Unlock()
}

// CertOptions returns the certificate config based on currently stored cert.
func (b *KeyCertBundle) CertOptions() (*CertOptions, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	ids, err := ExtractIDs(b.cert.Extensions)
	if err != nil {
		return nil, fmt.Errorf("failed to extract id %v", err)
	}
	if len(ids) != 1 {
		return nil, fmt.Errorf("expect single id from the cert, found %v", ids)
	}

	opts := &CertOptions{
		Host:      ids[0],
		Org:       b.cert.Issuer.Organization[0],
		IsCA:      b.cert.IsCA,
		TTL:       b.cert.NotAfter.Sub(b.cert.NotBefore),
		IsDualUse: ids[0] == b.cert.Subject.CommonName,
	}

	switch (*b.privKey).(type) {
	case *rsa.PrivateKey:
		size, err := GetRSAKeySize(*b.privKey)
		if err != nil {
			return nil, fmt.Errorf("failed to get RSA key size: %v", err)
		}
		opts.RSAKeySize = size
	case *ecdsa.PrivateKey:
		opts.ECSigAlg = EcdsaSigAlg
	default:
		return nil, errors.New("unknown private key type")
	}

	return opts, nil
}

// UpdateVerifiedKeyCertBundleFromFile Verifies and updates KeyCertBundle with new certs
func (b *KeyCertBundle) UpdateVerifiedKeyCertBundleFromFile(
	certFile, privKeyFile string,
	certChainFiles []string,
	rootCertFile, crlFile string,
) error {
	certBytes, err := os.ReadFile(certFile)
	if err != nil {
		return err
	}
	privKeyBytes, err := os.ReadFile(privKeyFile)
	if err != nil {
		return err
	}
	certChainBytes := []byte{}
	if len(certChainFiles) != 0 {
		for _, f := range certChainFiles {
			var b []byte
			if b, err = os.ReadFile(f); err != nil {
				return err
			}

			certChainBytes = append(certChainBytes, b...)
		}
	}
	rootCertBytes, err := os.ReadFile(rootCertFile)
	if err != nil {
		return err
	}

	// Read CRL file if provided
	crlBytes, crlErr := gerCRLBytesFromFile(crlFile)
	if crlErr != nil {
		return crlErr
	}

	err = b.VerifyAndSetAll(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crlBytes)
	if err != nil {
		return err
	}

	return nil
}

// gerCRLBytesFromFile reads the CRL file and returns the content if it exists.
// Providing CRL file is optional, if not provided, it returns nil without an error.
func gerCRLBytesFromFile(crlFile string) ([]byte, error) {
	if crlFile == "" {
		return nil, nil
	}

	return os.ReadFile(crlFile)
}

// ExtractRootCertExpiryTimestamp returns the expiration of the first root cert
func (b *KeyCertBundle) ExtractRootCertExpiryTimestamp() (time.Time, error) {
	return extractCertExpiryTimestamp("root cert", b.GetRootCertPem())
}

// ExtractCACertExpiryTimestamp returns the expiration of the leaf certificate
func (b *KeyCertBundle) ExtractCACertExpiryTimestamp() (time.Time, error) {
	return extractCertExpiryTimestamp("CA cert", b.GetCertChainPem())
}

// TimeBeforeCertExpires returns the time duration before the cert gets expired.
// It returns an error if it failed to extract the cert expiration timestamp.
// The returned time duration could be a negative value indicating the cert has already expired.
func TimeBeforeCertExpires(certBytes []byte, now time.Time) (time.Duration, error) {
	if len(certBytes) == 0 {
		return 0, fmt.Errorf("no certificate found")
	}

	certExpiryTimestamp, err := extractCertExpiryTimestamp("cert", certBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to extract cert expiration timestamp: %v", err)
	}

	certExpiry := certExpiryTimestamp.Sub(now)
	return certExpiry, nil
}

// Verify that the cert chain, root cert and key/cert match.
func Verify(certBytes, privKeyBytes, certChainBytes, rootCertBytes, crl []byte) error {
	// Verify the cert can be verified from the root cert through the cert chain.
	rcp := x509.NewCertPool()
	rcp.AppendCertsFromPEM(rootCertBytes)

	icp := x509.NewCertPool()
	icp.AppendCertsFromPEM(certChainBytes)

	opts := x509.VerifyOptions{
		Intermediates: icp,
		Roots:         rcp,
	}
	cert, err := ParsePemEncodedCertificate(certBytes)
	if err != nil {
		return fmt.Errorf("failed to parse cert PEM: %v", err)
	}
	chains, err := cert.Verify(opts)

	if len(chains) == 0 || err != nil {
		return fmt.Errorf(
			"cannot verify the cert with the provided root chain and cert "+
				"pool with error: %v", err)
	}

	// Verify that the key can be correctly parsed.
	if _, err = ParsePemEncodedKey(privKeyBytes); err != nil {
		return fmt.Errorf("failed to parse private key PEM: %v", err)
	}

	// Verify the cert and key match.
	if _, err := tls.X509KeyPair(certBytes, privKeyBytes); err != nil {
		return fmt.Errorf("the cert does not match the key: %v", err)
	}

	// verify only if the CRL is provided
	if len(crl) != 0 {
		// envoy expects that if a CRL is provided for any certificate authority in a trust chain,
		// a CRL must be provided for all certificate authorities in that chain.
		// Failure to do so will result in verification failure for both revoked and unrevoked certificates
		// from that chain.
		var parsedCRLs []*x509.RevocationList
		var signers []*x509.Certificate

		// gather all the certs in the chain
		for rest := certChainBytes; ; {
			block, remaining := pem.Decode(rest)
			if block == nil {
				break
			}
			c, parseErr := x509.ParseCertificate(block.Bytes)
			if parseErr != nil {
				return fmt.Errorf("failed to parse cert chain: %w", parseErr)
			}
			signers = append(signers, c)
			rest = remaining
		}

		// parse the CRL
		for rest := crl; ; {
			block, remaining := pem.Decode(rest)
			if block == nil {
				break
			}
			parsedCRL, parseErr := x509.ParseRevocationList(block.Bytes)
			if parseErr != nil {
				return fmt.Errorf("failed to parse CRL PEM: %w", parseErr)
			}
			parsedCRLs = append(parsedCRLs, parsedCRL)
			rest = remaining
		}

		// validate that the CRL is signed by the cert in the chain
		missing := make(map[string]*x509.Certificate, len(signers))
		for _, s := range signers {
			missing[s.Subject.String()] = s
		}

		for _, parsedCRL := range parsedCRLs {
			for subj, s := range missing {
				if parsedCRL.CheckSignatureFrom(s) == nil {
					delete(missing, subj)
				}
			}
			if len(missing) == 0 {
				break
			}
		}

		// if there are any missing CRLs, return an error
		if len(missing) != 0 {
			names := make([]string, 0, len(missing))
			for subj := range missing {
				names = append(names, subj)
			}
			return fmt.Errorf("missing CRL signed by certificates: %v", names)
		}
	}

	return nil
}

func extractCertExpiryTimestamp(certType string, certPem []byte) (time.Time, error) {
	cert, err := ParsePemEncodedCertificate(certPem)
	if err != nil {
		return time.Unix(0, 0), fmt.Errorf("failed to parse the %s: %v", certType, err)
	}
	return cert.NotAfter, nil
}

func copyBytes(src []byte) []byte {
	bs := make([]byte, len(src))
	copy(bs, src)
	return bs
}
