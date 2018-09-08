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

// Provides utility methods to generate X.509 certificates with different
// options. This implementation is Largely inspired from
// https://golang.org/src/crypto/tls/generate_cert.go.

package util

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"time"

	"istio.io/istio/pkg/log"
)

// CertOptions contains options for generating a new certificate.
type CertOptions struct {
	// Comma-separated hostnames and IPs to generate a certificate for.
	// This can also be set to the identity running the workload,
	// like kubernetes service account.
	Host string

	// The NotBefore field of the issued certificate.
	NotBefore time.Time

	// TTL of the certificate. NotAfter - NotBefore.
	TTL time.Duration

	// Signer certificate (PEM encoded).
	SignerCert *x509.Certificate

	// Signer private key (PEM encoded).
	SignerPriv crypto.PrivateKey

	// Organization for this certificate.
	Org string

	// Whether this certificate is used as signing cert for CA.
	IsCA bool

	// Whether this certificate is self-signed.
	IsSelfSigned bool

	// Whether this certificate is for a client.
	IsClient bool

	// Whether this certificate is for a server.
	IsServer bool

	// The size of RSA private key to be generated.
	RSAKeySize int

	// Whether this certificate is for dual-use clients (SAN+CN).
	IsDualUse bool
}

// GenCertKeyFromOptions generates a X.509 certificate and a private key with the given options.
func GenCertKeyFromOptions(options CertOptions) (pemCert []byte, pemKey []byte, err error) {
	// Generate a RSA private&public key pair.
	// The public key will be bound to the certificate generated below. The
	// private key will be used to sign this certificate in the self-signed
	// case, otherwise the certificate is signed by the signer private key
	// as specified in the CertOptions.
	priv, err := rsa.GenerateKey(rand.Reader, options.RSAKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("cert generation fails at RSA key generation (%v)", err)
	}
	template, err := genCertTemplateFromOptions(options)
	if err != nil {
		return nil, nil, fmt.Errorf("cert generation fails at cert template creation (%v)", err)
	}
	signerCert, signerKey := template, crypto.PrivateKey(priv)
	if !options.IsSelfSigned {
		signerCert, signerKey = options.SignerCert, options.SignerPriv
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, template, signerCert, &priv.PublicKey, signerKey)
	if err != nil {
		return nil, nil, fmt.Errorf("cert generation fails at X509 cert creation (%v)", err)
	}

	pemCert, pemKey = encodePem(false, certBytes, priv)
	err = nil
	return
}

// GenCertFromCSR generates a X.509 certificate with the given CSR.
func GenCertFromCSR(csr *x509.CertificateRequest, signingCert *x509.Certificate, publicKey interface{},
	signingKey crypto.PrivateKey, ttl time.Duration, isCA bool) (cert []byte, err error) {
	tmpl, err := genCertTemplateFromCSR(csr, ttl, isCA)
	if err != nil {
		return nil, err
	}
	return x509.CreateCertificate(rand.Reader, tmpl, signingCert, publicKey, signingKey)
}

// LoadSignerCredsFromFiles loads the signer cert&key from the given files.
//   signerCertFile: cert file name
//   signerPrivFile: private key file name
func LoadSignerCredsFromFiles(signerCertFile string, signerPrivFile string) (*x509.Certificate, crypto.PrivateKey, error) {
	signerCertBytes, err := ioutil.ReadFile(signerCertFile)
	if err != nil {
		return nil, nil, fmt.Errorf("certificate file reading failure (%v)", err)
	}

	signerPrivBytes, err := ioutil.ReadFile(signerPrivFile)
	if err != nil {
		return nil, nil, fmt.Errorf("private key file reading failure (%v)", err)
	}

	cert, err := ParsePemEncodedCertificate(signerCertBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("pem encoded cert parsing failure (%v)", err)
	}

	key, err := ParsePemEncodedKey(signerPrivBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("pem encoded key parsing failure (%v)", err)
	}

	return cert, key, nil
}

// genCertTemplateFromCSR generates a certificate template with the given CSR.
// The NotBefore value of the cert is set to current time.
func genCertTemplateFromCSR(csr *x509.CertificateRequest, ttl time.Duration, isCA bool) (*x509.Certificate, error) {
	var keyUsage x509.KeyUsage
	extKeyUsages := []x509.ExtKeyUsage{}
	if isCA {
		// If the cert is a CA cert, the private key is allowed to sign other certificates.
		keyUsage = x509.KeyUsageCertSign
	} else {
		// Otherwise the private key is allowed for digital signature and key encipherment.
		keyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		// For now, we do not differentiate non-CA certs to be used on client auth or server auth.
		extKeyUsages = append(extKeyUsages, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	}

	exts := append(csr.Extensions, csr.ExtraExtensions...)

	now := time.Now()

	serialNum, err := genSerialNum()
	if err != nil {
		return nil, err
	}

	return &x509.Certificate{
		SerialNumber:          serialNum,
		Subject:               csr.Subject,
		NotBefore:             now,
		NotAfter:              now.Add(ttl),
		KeyUsage:              keyUsage,
		ExtKeyUsage:           extKeyUsages,
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		ExtraExtensions:       exts,
		DNSNames:              csr.DNSNames,
		EmailAddresses:        csr.EmailAddresses,
		IPAddresses:           csr.IPAddresses,
		SignatureAlgorithm:    csr.SignatureAlgorithm}, nil
}

// genCertTemplateFromoptions generates a certificate template with the given options.
func genCertTemplateFromOptions(options CertOptions) (*x509.Certificate, error) {
	var keyUsage x509.KeyUsage
	if options.IsCA {
		// If the cert is a CA cert, the private key is allowed to sign other certificates.
		keyUsage = x509.KeyUsageCertSign
	} else {
		// Otherwise the private key is allowed for digital signature and key encipherment.
		keyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	}

	extKeyUsages := []x509.ExtKeyUsage{}
	if options.IsServer {
		extKeyUsages = append(extKeyUsages, x509.ExtKeyUsageServerAuth)
	}
	if options.IsClient {
		extKeyUsages = append(extKeyUsages, x509.ExtKeyUsageClientAuth)
	}

	notBefore := time.Now()
	if !options.NotBefore.IsZero() {
		notBefore = options.NotBefore
	}

	serialNum, err := genSerialNum()
	if err != nil {
		return nil, err
	}

	subject := pkix.Name{
		Organization: []string{options.Org},
	}

	exts := []pkix.Extension{}
	if h := options.Host; len(h) > 0 {
		s, err := BuildSubjectAltNameExtension(h)
		if err != nil {
			return nil, err
		}
		if options.IsDualUse {
			cn, err := DualUseCommonName(h)
			if err != nil {
				// log and continue
				log.Errorf("dual-use failed for cert template - omitting CN (%v)", err)
			} else {
				subject.CommonName = cn
			}
		}
		exts = []pkix.Extension{*s}
	}

	return &x509.Certificate{
		SerialNumber:          serialNum,
		Subject:               subject,
		NotBefore:             notBefore,
		NotAfter:              notBefore.Add(options.TTL),
		KeyUsage:              keyUsage,
		ExtKeyUsage:           extKeyUsages,
		IsCA:                  options.IsCA,
		BasicConstraintsValid: true,
		ExtraExtensions:       exts}, nil
}

func genSerialNum() (*big.Int, error) {
	serialNumLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNum, err := rand.Int(rand.Reader, serialNumLimit)
	if err != nil {
		return nil, fmt.Errorf("serial number generation failure (%v)", err)
	}
	return serialNum, nil
}

func encodePem(isCSR bool, csrOrCert []byte, priv *rsa.PrivateKey) ([]byte, []byte) {
	encodeMsg := "CERTIFICATE"
	if isCSR {
		encodeMsg = "CERTIFICATE REQUEST"
	}
	csrOrCertPem := pem.EncodeToMemory(&pem.Block{Type: encodeMsg, Bytes: csrOrCert})

	privDer := x509.MarshalPKCS1PrivateKey(priv)
	privPem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDer})
	return csrOrCertPem, privPem
}
