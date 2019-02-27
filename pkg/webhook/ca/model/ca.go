// Copyright 2019 Istio Authors
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

package model

import (
	"encoding/pem"
	"fmt"
	"time"

	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/webhook/ca/util"
)

// CertificateAuthority contains methods to be supported by a CA.
type CertificateAuthority interface {
	// Sign generates a certificate for a workload or CA, from the given CSR and TTL.
	// TODO(myidpt): simplify this interface and pass a struct with cert field values instead.
	Sign(csrPEM []byte, subjectIDs []string, ttl time.Duration, forCA bool) ([]byte, error)
	// GetCAKeyCertBundle returns the KeyCertBundle used by CA.
	GetCAKeyCertBundle() util.KeyCertBundle
}

// WebhookCA generates keys and certificates for webhook.
type WebhookCA struct {
	CertTTL    time.Duration
	MaxCertTTL time.Duration

	KeyCertBundle util.KeyCertBundle

	LivenessProbe *probe.Probe
}

// Sign takes a PEM-encoded CSR, subject IDs and lifetime, and returns a signed certificate. If forCA is true,
// the signed certificate is a CA certificate, otherwise, it is a workload certificate.
// TODO(myidpt): Add error code to identify the Sign error types.
func (ca *WebhookCA) Sign(csrPEM []byte, subjectIDs []string, requestedLifetime time.Duration, forCA bool) ([]byte, error) {
	signingCert, signingKey, _, _ := ca.KeyCertBundle.GetAll()
	if signingCert == nil {
		return nil, NewError(CANotReady, fmt.Errorf("Istio CA is not ready")) // nolint
	}

	csr, err := util.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return nil, NewError(CSRError, err)
	}

	lifetime := requestedLifetime
	// If the requested requestedLifetime is non-positive, apply the default TTL.
	if requestedLifetime.Seconds() <= 0 {
		lifetime = ca.CertTTL
	}
	// If the requested TTL is greater than maxCertTTL, return an error
	if requestedLifetime.Seconds() > ca.MaxCertTTL.Seconds() {
		return nil, NewError(TTLError, fmt.Errorf(
			"requested TTL %s is greater than the max allowed TTL %s", requestedLifetime, ca.MaxCertTTL))
	}

	certBytes, err := util.GenCertFromCSR(csr, signingCert, csr.PublicKey, *signingKey, subjectIDs, lifetime, forCA)
	if err != nil {
		return nil, NewError(CertGenError, err)
	}

	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}
	cert := pem.EncodeToMemory(block)

	return cert, nil
}

// GetCAKeyCertBundle returns the KeyCertBundle for the CA.
func (ca *WebhookCA) GetCAKeyCertBundle() util.KeyCertBundle {
	return ca.KeyCertBundle
}
