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

package certmanager

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io/ioutil"
	"log"
	"math/big"
	"net"
	"strings"
	"time"
)

// CertOptions contains options for generating a new certificate.
type CertOptions struct {
	// Comma-separated hostnames and IPs to generate a certificate for.
	// This can also be set to the identity running the workload,
	// like kubernetes service account.
	Host string

	// This certificate's validity start time formatted as
	// Jan 1 15:04:05 2011.
	// If empty string, the validity start time is set to time.Now()
	ValidFrom string

	// Duration that this certificate is valid for.
	ValidFor time.Duration

	// Signer certificate (PEM encoded).
	SignerCert *x509.Certificate

	// Signer private key (PEM encoded).
	SignerPriv *rsa.PrivateKey

	// Organization for this certificate.
	Org string

	// Whether this certificate should be a Cerificate Authority.
	IsCA bool

	// Whether this cerificate is self-signed.
	IsSelfSigned bool

	// Whether this certificate is for a client.
	IsClient bool
}

// Size of RSA key to generate.
const rsaBits = 2048

// GenCert generates a X.509 certificate with the given options.
func GenCert(options CertOptions) ([]byte, []byte) {
	// Generates a RSA private&public key pair.
	// The public key will be bound to the certficate generated below. The
	// private key will be used to sign this certificate in the self-signed
	// case, otherwise the certificate is signed by the signer private key
	// as specified in the CertOptions.
	priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		log.Fatalf("RSA key generation failed with error %s.", err)
	}
	template := genCertTemplate(options)
	signingCert, signingKey := &template, priv
	if !options.IsSelfSigned {
		signingCert, signingKey = options.SignerCert, options.SignerPriv
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, signingCert, &priv.PublicKey, signingKey)
	if err != nil {
		log.Fatalf("Could not create certificate (err = %s).", err)
	}

	// Returns the certificate that carries the RSA public key as well as
	// the corresponding private key.
	certPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})

	privDer := x509.MarshalPKCS1PrivateKey(priv)
	privPem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDer})
	return privPem, certPem
}

// LoadSigningCreds loads the signer cert&key from the given files.
//   signerCertFile: cert file name
//   signerPrivFile: private key file name
func LoadSigningCreds(signerCertFile string, signerPrivFile string) (*x509.Certificate, *rsa.PrivateKey) {
	fileBytes, err := ioutil.ReadFile(signerCertFile)
	if err != nil {
		log.Fatalf("Reading cert file failed with error %s.", err)
	}
	der, _ := pem.Decode(fileBytes)
	if der == nil {
		log.Fatalf("Invalid PEM encoding.")
	}
	signingCert, err := x509.ParseCertificate(der.Bytes)
	if err != nil {
		log.Fatalf("Certificate parsing failed with error %s.", err)
	}

	fileBytes, err = ioutil.ReadFile(signerPrivFile)
	if err != nil {
		log.Fatalf("Reading private key file failed with error %s.", err)
	}
	der, _ = pem.Decode(fileBytes)
	signingKey, err := x509.ParsePKCS1PrivateKey(der.Bytes)
	if err != nil {
		log.Fatalf("Private key parsing failed with error %s.", err)
	}
	return signingCert, signingKey
}

// toFromDates generates the certficiate validity period [notBefore, notAfter]
// from the given start time and expiration duration.
//   validFrom: certficate validity start time. If empty, the certificate
//              validity start time will be set to time.Now()
//   validFor: certficate validity duration
func toFromDates(validFrom string, validFor time.Duration) (time.Time, time.Time) {
	var notBefore time.Time
	if len(validFrom) == 0 {
		notBefore = time.Now()
	} else {
		var err error
		notBefore, err = time.Parse("Jan 2 15:04:05 2006", validFrom)
		if err != nil {
			log.Fatalf("Failed to parse creation date: %s\n", err)
		}
	}
	notAfter := notBefore.Add(validFor)
	return notBefore, notAfter
}

func genSerialNum() *big.Int {
	serialNumLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNum, err := rand.Int(rand.Reader, serialNumLimit)
	if err != nil {
		log.Fatalf("Failed to generate serial number: %s.", err)
	}
	return serialNum
}

// genCertTemplate generates a certificate template with the given options.
func genCertTemplate(options CertOptions) x509.Certificate {
	notBefore, notAfter := toFromDates(options.ValidFrom, options.ValidFor)
	extKeyUsage := x509.ExtKeyUsageServerAuth
	if options.IsClient {
		extKeyUsage = x509.ExtKeyUsageClientAuth
	}
	template := x509.Certificate{
		SerialNumber: genSerialNum(),
		Subject: pkix.Name{
			Organization: []string{options.Org},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{extKeyUsage},
		BasicConstraintsValid: true,
	}
	hosts := strings.Split(options.Host, ",")
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}
	if options.IsCA {
		template.IsCA = true
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	return template
}
