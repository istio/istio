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

package writer

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/generator"
)

const (
	// CAKeyName is the name of the CA private key
	CAKeyName = "ca-key.pem"
	// CACertName is the name of the CA certificate
	CACertName = "ca-cert.pem"
	// ServerKeyName is the name of the server private key
	ServerKeyName = "key.pem"
	// ServerCertName is the name of the serving certificate
	ServerCertName = "cert.pem"
)

// CertWriter provides method to handle webhooks.
type CertWriter interface {
	// EnsureCert provisions the cert for the webhookClientConfig.
	EnsureCert(dnsName string) (*generator.Artifacts, bool, error)
	// Inject injects the necessary information given the objects.
	// It supports MutatingWebhookConfiguration and ValidatingWebhookConfiguration.
	Inject(objs ...runtime.Object) error
}

// handleCommon ensures the given webhook has a proper certificate.
// It uses the given certReadWriter to read and (or) write the certificate.
func handleCommon(dnsName string, ch certReadWriter) (*generator.Artifacts, bool, error) {
	if len(dnsName) == 0 {
		return nil, false, errors.New("dnsName should not be empty")
	}
	if ch == nil {
		return nil, false, errors.New("certReaderWriter should not be nil")
	}

	certs, changed, err := createIfNotExists(ch)
	if err != nil {
		return nil, changed, err
	}

	// Recreate the cert if it's invalid.
	valid := validCert(certs, dnsName)
	if !valid {
		log.Info("cert is invalid or expiring, regenerating a new one")
		certs, err = ch.overwrite()
		if err != nil {
			return nil, false, err
		}
		changed = true
	}
	return certs, changed, nil
}

func createIfNotExists(ch certReadWriter) (*generator.Artifacts, bool, error) {
	// Try to read first
	certs, err := ch.read()
	if isNotFound(err) {
		// Create if not exists
		certs, err = ch.write()
		switch {
		// This may happen if there is another racer.
		case isAlreadyExists(err):
			certs, err = ch.read()
			return certs, true, err
		default:
			return certs, true, err
		}
	}
	return certs, false, err
}

// certReadWriter provides methods for reading and writing certificates.
type certReadWriter interface {
	// read reads a webhook name and returns the certs for it.
	read() (*generator.Artifacts, error)
	// write writes the certs and return the certs it wrote.
	write() (*generator.Artifacts, error)
	// overwrite overwrites the existing certs and return the certs it wrote.
	overwrite() (*generator.Artifacts, error)
}

func validCert(certs *generator.Artifacts, dnsName string) bool {
	if certs == nil {
		return false
	}

	// Verify key and cert are valid pair
	_, err := tls.X509KeyPair(certs.Cert, certs.Key)
	if err != nil {
		return false
	}

	// Verify cert is good for desired DNS name and signed by CA and will be valid for desired period of time.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certs.CACert) {
		return false
	}
	block, _ := pem.Decode([]byte(certs.Cert))
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	ops := x509.VerifyOptions{
		DNSName:     dnsName,
		Roots:       pool,
		CurrentTime: time.Now().AddDate(0, 6, 0),
	}
	_, err = cert.Verify(ops)
	return err == nil
}
