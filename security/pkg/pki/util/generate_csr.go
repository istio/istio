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
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"istio.io/pkg/log"
)

// minimumRsaKeySize is the minimum RSA key size to generate certificates
// to ensure proper security
const minimumRsaKeySize = 2048

// GenCSR generates a X.509 certificate sign request and private key with the given options.
func GenCSR(options CertOptions) ([]byte, []byte, error) {
	var priv interface{}
	var err error
	if options.ECSigAlg != "" {
		switch options.ECSigAlg {
		case EcdsaSigAlg:
			priv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				return nil, nil, fmt.Errorf("EC key generation failed (%v)", err)
			}
		default:
			return nil, nil, errors.New("csr cert generation fails due to unsupported EC signature algorithm")
		}
	} else {
		if options.RSAKeySize < minimumRsaKeySize {
			return nil, nil, fmt.Errorf("requested key size does not meet the minimum requied size of %d (requested: %d)", minimumRsaKeySize, options.RSAKeySize)
		}

		priv, err = rsa.GenerateKey(rand.Reader, options.RSAKeySize)
		if err != nil {
			return nil, nil, fmt.Errorf("RSA key generation failed (%v)", err)
		}
	}
	template, err := GenCSRTemplate(options)
	if err != nil {
		return nil, nil, fmt.Errorf("CSR template creation failed (%v)", err)
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, template, crypto.PrivateKey(priv))
	if err != nil {
		return nil, nil, fmt.Errorf("CSR creation failed (%v)", err)
	}

	csr, privKey, err := encodePem(true, csrBytes, priv, options.PKCS8Key)
	return csr, privKey, err
}

// GenCSRTemplate generates a certificateRequest template with the given options.
func GenCSRTemplate(options CertOptions) (*x509.CertificateRequest, error) {
	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{options.Org},
		},
	}

	if h := options.Host; len(h) > 0 {
		s, err := BuildSubjectAltNameExtension(h)
		if err != nil {
			return nil, err
		}
		if options.IsDualUse {
			cn, err := DualUseCommonName(h)
			if err != nil {
				// log and continue
				log.Errorf("dual-use failed for CSR template - omitting CN (%v)", err)
			} else {
				template.Subject.CommonName = cn
			}
		}
		template.ExtraExtensions = []pkix.Extension{*s}
	}

	return template, nil
}

// AppendRootCerts appends root certificates in RootCertFile to the input certificate.
func AppendRootCerts(pemCert []byte, rootCertFile string) ([]byte, error) {
	var rootCerts []byte
	if len(pemCert) > 0 {
		// Copy the input certificate
		rootCerts = make([]byte, len(pemCert))
		copy(rootCerts, pemCert)
	}
	if len(rootCertFile) > 0 {
		log.Debugf("append root certificates from %v", rootCertFile)
		certBytes, err := ioutil.ReadFile(rootCertFile)
		if err != nil {
			return rootCerts, fmt.Errorf("failed to read root certificates (%v)", err)
		}
		log.Debugf("The root certificates to be appended is: %v", rootCertFile)
		if len(rootCerts) > 0 {
			// Append a newline after the last cert
			rootCerts = []byte(strings.TrimSuffix(string(rootCerts), "\n") + "\n")
		}
		rootCerts = append(rootCerts, certBytes...)
	}
	return rootCerts, nil
}
