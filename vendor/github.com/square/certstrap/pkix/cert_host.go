/*-
 * Copyright 2015 Square Inc.
 * Copyright 2014 CoreOS
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package pkix

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"time"
)

var (
	// Build CA based on RFC5280
	hostTemplate = x509.Certificate{
		// **SHOULD** be filled in a unique number
		SerialNumber: big.NewInt(0),
		// **SHOULD** be filled in host info
		Subject: pkix.Name{},
		// NotBefore is set to be 10min earlier to fix gap on time difference in cluster
		NotBefore: time.Now().Add(-600).UTC(),
		// 10-year lease
		NotAfter: time.Time{},
		// Used for certificate signing only
		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement,

		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		UnknownExtKeyUsage: nil,

		BasicConstraintsValid: false,

		// 160-bit SHA-1 hash of the value of the BIT STRING subjectPublicKey
		// (excluding the tag, length, and number of unused bits)
		// **SHOULD** be filled in later
		SubjectKeyId: nil,

		// Subject Alternative Name
		DNSNames: nil,

		PermittedDNSDomainsCritical: false,
		PermittedDNSDomains:         nil,
	}
)

// CreateCertificateHost creates certificate for host.
// The arguments include CA certificate, CA key, certificate request.
func CreateCertificateHost(crtAuth *Certificate, keyAuth *Key, csr *CertificateSigningRequest, proposedExpiry time.Time) (*Certificate, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}
	hostTemplate.SerialNumber.Set(serialNumber)

	rawCsr, err := csr.GetRawCertificateSigningRequest()
	if err != nil {
		return nil, err
	}

	// pkix.Name{} doesn't take ordering into account.
	// RawSubject works because CreateCertificate() first checks if
	// RawSubject has a value.
	hostTemplate.RawSubject = rawCsr.RawSubject

	caExpiry := time.Now().Add(crtAuth.GetExpirationDuration())
	// ensure cert doesn't expire after issuer
	if caExpiry.Before(proposedExpiry) {
		hostTemplate.NotAfter = caExpiry
	} else {
		hostTemplate.NotAfter = proposedExpiry
	}

	hostTemplate.SubjectKeyId, err = GenerateSubjectKeyID(rawCsr.PublicKey)
	if err != nil {
		return nil, err
	}

	hostTemplate.IPAddresses = rawCsr.IPAddresses
	hostTemplate.DNSNames = rawCsr.DNSNames

	rawCrtAuth, err := crtAuth.GetRawCertificate()
	if err != nil {
		return nil, err
	}

	crtHostBytes, err := x509.CreateCertificate(rand.Reader, &hostTemplate, rawCrtAuth, rawCsr.PublicKey, keyAuth.Private)
	if err != nil {
		return nil, err
	}

	return NewCertificateFromDER(crtHostBytes), nil
}
