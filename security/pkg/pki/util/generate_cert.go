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
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"istio.io/istio/pkg/log"
)

// SupportedECSignatureAlgorithms are the types of EC Signature Algorithms
// to be used in key generation (e.g. ECDSA or ED2551)
type SupportedECSignatureAlgorithms string

// SupportedEllipticCurves are the types of curves
// to be used in key generation (e.g. P256, P384)
type SupportedEllipticCurves string

const (
	// only ECDSA is currently supported
	EcdsaSigAlg SupportedECSignatureAlgorithms = "ECDSA"

	// supported curves when using ECC
	P256Curve SupportedEllipticCurves = "P256"
	P384Curve SupportedEllipticCurves = "P384"
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

	// Signer certificate.
	SignerCert *x509.Certificate

	// Signer private key.
	SignerPriv crypto.PrivateKey

	// Signer private key (PEM encoded).
	SignerPrivPem []byte

	// Organization for this certificate.
	Org string

	// The size of RSA private key to be generated.
	RSAKeySize int

	// Whether this certificate is used as signing cert for CA.
	IsCA bool

	// Whether this certificate is self-signed.
	IsSelfSigned bool

	// Whether this certificate is for a client.
	IsClient bool

	// Whether this certificate is for a server.
	IsServer bool

	// Whether this certificate is for dual-use clients (SAN+CN).
	IsDualUse bool

	// If true, the private key is encoded with PKCS#8.
	PKCS8Key bool

	// The type of Elliptical Signature algorithm to use
	// when generating private keys. Currently only ECDSA is supported.
	// If empty, RSA is used, otherwise ECC is used.
	ECSigAlg SupportedECSignatureAlgorithms

	// The type of Elliptical Signature algorithm to use
	// when generating private keys. Currently only ECDSA is supported.
	// If empty, RSA is used, otherwise ECC is used.
	ECCCurve SupportedEllipticCurves

	// Subjective Alternative Name values.
	DNSNames string
}

// GenCertKeyFromOptions generates a X.509 certificate and a private key with the given options.
func GenCertKeyFromOptions(options CertOptions) (pemCert []byte, pemKey []byte, err error) {
	// Generate the appropriate private&public key pair based on options.
	// The public key will be bound to the certificate generated below. The
	// private key will be used to sign this certificate in the self-signed
	// case, otherwise the certificate is signed by the signer private key
	// as specified in the CertOptions.
	if options.ECSigAlg != "" {
		var ecPriv *ecdsa.PrivateKey

		switch options.ECSigAlg {
		case EcdsaSigAlg:
			var curve elliptic.Curve
			switch options.ECCCurve {
			case P384Curve:
				curve = elliptic.P384()
			default:
				curve = elliptic.P256()
			}

			ecPriv, err = ecdsa.GenerateKey(curve, rand.Reader)
			if err != nil {
				return nil, nil, fmt.Errorf("cert generation fails at EC key generation (%v)", err)
			}

		default:
			return nil, nil, errors.New("cert generation fails due to unsupported EC signature algorithm")
		}
		return genCert(options, ecPriv, &ecPriv.PublicKey)
	}

	if options.RSAKeySize < MinimumRsaKeySize {
		return nil, nil, fmt.Errorf("requested key size does not meet the minimum required size of %d (requested: %d)", MinimumRsaKeySize, options.RSAKeySize)
	}
	rsaPriv, err := rsa.GenerateKey(rand.Reader, options.RSAKeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("cert generation fails at RSA key generation (%v)", err)
	}
	return genCert(options, rsaPriv, &rsaPriv.PublicKey)
}

func genCert(options CertOptions, priv any, key any) ([]byte, []byte, error) {
	template, err := genCertTemplateFromOptions(options)
	if err != nil {
		return nil, nil, fmt.Errorf("cert generation fails at cert template creation (%v)", err)
	}
	signerCert, signerKey := template, crypto.PrivateKey(priv)
	if !options.IsSelfSigned {
		signerCert, signerKey = options.SignerCert, options.SignerPriv
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, template, signerCert, key, signerKey)
	if err != nil {
		return nil, nil, fmt.Errorf("cert generation fails at X509 cert creation (%v)", err)
	}

	pemCert, pemKey, err := encodePem(false, certBytes, priv, options.PKCS8Key)
	return pemCert, pemKey, err
}

func publicKey(priv any) any {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &k.PublicKey
	case *ecdsa.PrivateKey:
		return &k.PublicKey
	case ed25519.PrivateKey:
		return k.Public().(ed25519.PublicKey)
	default:
		return nil
	}
}

// GenRootCertFromExistingKey generates a X.509 certificate using existing
// CA private key. Only called by a self-signed Citadel.
func GenRootCertFromExistingKey(options CertOptions) (pemCert []byte, pemKey []byte, err error) {
	if !options.IsSelfSigned || len(options.SignerPrivPem) == 0 {
		return nil, nil, fmt.Errorf("skip cert " +
			"generation. Citadel is not in self-signed mode or CA private key is not " +
			"available")
	}

	template, err := genCertTemplateFromOptions(options)
	if err != nil {
		return nil, nil, fmt.Errorf("cert generation fails at cert template creation (%v)", err)
	}
	caPrivateKey, err := ParsePemEncodedKey(options.SignerPrivPem)
	if err != nil {
		return nil, nil, fmt.Errorf("unrecogniazed CA "+
			"private key, skip root cert rotation: %s", err.Error())
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, publicKey(caPrivateKey), caPrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("cert generation fails at X509 cert creation (%v)", err)
	}

	pemCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	return pemCert, options.SignerPrivPem, nil
}

// GetCertOptionsFromExistingCert parses cert and generates a CertOptions
// that contains information about the cert. This is the reverse operation of
// genCertTemplateFromOptions(), and only called by a self-signed Citadel.
func GetCertOptionsFromExistingCert(certBytes []byte) (opts CertOptions, err error) {
	cert, certErr := ParsePemEncodedCertificate(certBytes)
	if certErr != nil {
		return opts, certErr
	}

	orgs := cert.Subject.Organization
	if len(orgs) > 0 {
		opts.Org = orgs[0]
	}
	// TODO(JimmyCYJ): parse other fields from certificate, e.g. CommonName.
	return opts, nil
}

// MergeCertOptions merges deltaOpts into defaultOpts and returns the merged
// CertOptions. Only called by a self-signed Citadel.
func MergeCertOptions(defaultOpts, deltaOpts CertOptions) CertOptions {
	if len(deltaOpts.Org) > 0 {
		defaultOpts.Org = deltaOpts.Org
	}
	// TODO(JimmyCYJ): merge other fields, e.g. Host, IsDualUse, etc.
	return defaultOpts
}

// GenCertFromCSR generates a X.509 certificate with the given CSR.
func GenCertFromCSR(csr *x509.CertificateRequest, signingCert *x509.Certificate, publicKey any,
	signingKey crypto.PrivateKey, subjectIDs []string, ttl time.Duration, isCA bool,
) (cert []byte, err error) {
	tmpl, err := genCertTemplateFromCSR(csr, subjectIDs, ttl, isCA)
	if err != nil {
		return nil, err
	}
	return x509.CreateCertificate(rand.Reader, tmpl, signingCert, publicKey, signingKey)
}

// LoadSignerCredsFromFiles loads the signer cert&key from the given files.
//
//	signerCertFile: cert file name
//	signerPrivFile: private key file name
func LoadSignerCredsFromFiles(signerCertFile string, signerPrivFile string) (*x509.Certificate, crypto.PrivateKey, error) {
	signerCertBytes, err := os.ReadFile(signerCertFile)
	if err != nil {
		return nil, nil, fmt.Errorf("certificate file reading failure (%v)", err)
	}

	signerPrivBytes, err := os.ReadFile(signerPrivFile)
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

// ClockSkewGracePeriod defines the period of time a certificate will be valid before its creation.
// This is meant to handle cases where we have clock skew between the CA and workloads.
const ClockSkewGracePeriod = time.Minute * 2

// genCertTemplateFromCSR generates a certificate template with the given CSR.
// The NotBefore value of the cert is set to current time.
func genCertTemplateFromCSR(csr *x509.CertificateRequest, subjectIDs []string, ttl time.Duration, isCA bool) (
	*x509.Certificate, error,
) {
	subjectIDsInString := strings.Join(subjectIDs, ",")
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

	// Build cert extensions with the subjectIDs.
	ext, err := BuildSubjectAltNameExtension(subjectIDsInString)
	if err != nil {
		return nil, err
	}
	exts := []pkix.Extension{*ext}

	subject := pkix.Name{}
	// Dual use mode if common name in CSR is not empty.
	// In this case, set CN as determined by DualUseCommonName(subjectIDsInString).
	if len(csr.Subject.CommonName) != 0 {
		if cn, err := DualUseCommonName(subjectIDsInString); err != nil {
			// log and continue
			log.Errorf("dual-use failed for cert template - omitting CN (%v)", err)
		} else {
			subject.CommonName = cn
		}
	}

	now := time.Now()

	serialNum, err := genSerialNum()
	if err != nil {
		return nil, err
	}
	// SignatureAlgorithm will use the default algorithm.
	// See https://golang.org/src/crypto/x509/x509.go?s=5131:5158#L1965 .
	return &x509.Certificate{
		SerialNumber:          serialNum,
		Subject:               subject,
		NotBefore:             now.Add(-ClockSkewGracePeriod),
		NotAfter:              now.Add(ttl),
		KeyUsage:              keyUsage,
		ExtKeyUsage:           extKeyUsages,
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		ExtraExtensions:       exts,
	}, nil
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

	dnsNames := strings.Split(options.DNSNames, ",")
	if len(dnsNames[0]) == 0 {
		dnsNames = nil
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
		ExtraExtensions:       exts,
		DNSNames:              dnsNames,
	}, nil
}

func genSerialNum() (*big.Int, error) {
	serialNumLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNum, err := rand.Int(rand.Reader, serialNumLimit)
	if err != nil {
		return nil, fmt.Errorf("serial number generation failure (%v)", err)
	}
	return serialNum, nil
}

func encodePem(isCSR bool, csrOrCert []byte, priv any, pkcs8 bool) (
	csrOrCertPem []byte, privPem []byte, err error,
) {
	encodeMsg := "CERTIFICATE"
	if isCSR {
		encodeMsg = "CERTIFICATE REQUEST"
	}
	csrOrCertPem = pem.EncodeToMemory(&pem.Block{Type: encodeMsg, Bytes: csrOrCert})

	var encodedKey []byte
	if pkcs8 {
		if encodedKey, err = x509.MarshalPKCS8PrivateKey(priv); err != nil {
			return nil, nil, err
		}
		privPem = pem.EncodeToMemory(&pem.Block{Type: blockTypePKCS8PrivateKey, Bytes: encodedKey})
	} else {
		switch k := priv.(type) {
		case *rsa.PrivateKey:
			encodedKey = x509.MarshalPKCS1PrivateKey(k)
			privPem = pem.EncodeToMemory(&pem.Block{Type: blockTypeRSAPrivateKey, Bytes: encodedKey})
		case *ecdsa.PrivateKey:
			encodedKey, err = x509.MarshalECPrivateKey(k)
			if err != nil {
				return nil, nil, err
			}
			privPem = pem.EncodeToMemory(&pem.Block{Type: blockTypeECPrivateKey, Bytes: encodedKey})
		}
	}
	err = nil
	return csrOrCertPem, privPem, err
}
