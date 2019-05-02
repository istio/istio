/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package generator

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"time"

	"k8s.io/client-go/util/cert"
)

// ServiceToCommonName generates the CommonName for the certificate when using a k8s service.
func ServiceToCommonName(serviceNamespace, serviceName string) string {
	return fmt.Sprintf("%s.%s.svc", serviceName, serviceNamespace)
}

// SelfSignedCertGenerator implements the certGenerator interface.
// It provisions self-signed certificates.
type SelfSignedCertGenerator struct {
	caKey  []byte
	caCert []byte
}

var _ CertGenerator = &SelfSignedCertGenerator{}

// SetCA sets the PEM-encoded CA private key and CA cert for signing the generated serving cert.
func (cp *SelfSignedCertGenerator) SetCA(caKey, caCert []byte) {
	cp.caKey = caKey
	cp.caCert = caCert
}

// Generate creates and returns a CA certificate, certificate and
// key for the server. serverKey and serverCert are used by the server
// to establish trust for clients, CA certificate is used by the
// client to verify the server authentication chain.
// The cert will be valid for 365 days.
func (cp *SelfSignedCertGenerator) Generate(commonName string) (*Artifacts, error) {
	var signingKey *rsa.PrivateKey
	var signingCert *x509.Certificate
	var valid bool
	var err error

	valid, signingKey, signingCert = cp.validCACert()
	if !valid {
		signingKey, err = cert.NewPrivateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to create the CA private key: %v", err)
		}
		signingCert, err = cert.NewSelfSignedCACert(cert.Config{CommonName: "webhook-cert-ca"}, signingKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create the CA cert: %v", err)
		}
	}

	key, err := cert.NewPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to create the private key: %v", err)
	}
	signedCert, err := cert.NewSignedCert(
		cert.Config{
			CommonName: commonName,
			Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		},
		key, signingCert, signingKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create the cert: %v", err)
	}
	return &Artifacts{
		Key:    cert.EncodePrivateKeyPEM(key),
		Cert:   cert.EncodeCertPEM(signedCert),
		CAKey:  cert.EncodePrivateKeyPEM(signingKey),
		CACert: cert.EncodeCertPEM(signingCert),
	}, nil
}

func (cp *SelfSignedCertGenerator) validCACert() (bool, *rsa.PrivateKey, *x509.Certificate) {
	if !ValidCACert(cp.caKey, cp.caCert, cp.caCert, "",
		time.Now().AddDate(1, 0, 0)) {
		return false, nil, nil
	}

	var ok bool
	key, err := cert.ParsePrivateKeyPEM(cp.caKey)
	if err != nil {
		return false, nil, nil
	}
	privateKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return false, nil, nil
	}

	certs, err := cert.ParseCertsPEM(cp.caCert)
	if err != nil {
		return false, nil, nil
	}
	if len(certs) != 1 {
		return false, nil, nil
	}
	return true, privateKey, certs[0]
}
