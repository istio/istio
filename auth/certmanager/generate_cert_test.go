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

package certmanager

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"istio.io/auth/pkg/pki"
)

// VerifyFields contains the certficate fields to verify in the test.
type VerifyFields struct {
	notBefore   time.Time
	notAfter    time.Time
	extKeyUsage []x509.ExtKeyUsage
	keyUsage    x509.KeyUsage
	isCA        bool
	org         string
}

var now = time.Now().Round(time.Second).UTC()

func TestGenCSR(t *testing.T) {
	// Options to generate a CSR.
	csrOptions := CertOptions{
		Host:       "test_ca.com",
		Org:        "MyOrg",
		RSAKeySize: 512,
	}

	csrPem, _, err := GenCSR(csrOptions)

	if err != nil {
		t.Errorf("failed to gen CSR")
	}

	pemBlock, _ := pem.Decode(csrPem)
	if pemBlock == nil {
		t.Errorf("failed to decode csr")
	}
	csr, err := x509.ParseCertificateRequest(pemBlock.Bytes)
	if err != nil {
		t.Errorf("failed to parse csr")
	}
	if err = csr.CheckSignature(); err != nil {
		t.Errorf("csr signature is invalid")
	}
	if csr.Subject.Organization[0] != "MyOrg" {
		t.Errorf("csr subject does not match")
	}
	if !strings.HasSuffix(string(csr.Extensions[0].Value[:]), "test_ca.com") {
		t.Errorf("csr host does not match")
	}
}

func TestGenCSRWithInvalidOption(t *testing.T) {
	// Options with invalid Key size.
	csrOptions := CertOptions{
		Host:       "test_ca.com",
		Org:        "MyOrg",
		RSAKeySize: -1,
	}

	csr, priv, err := GenCSR(csrOptions)

	if err == nil || csr != nil || priv != nil {
		t.Errorf("Should have failed")
	}
}

func TestGenCert(t *testing.T) {
	caCertNotBefore := now
	caCertNotAfter := now.Add(24 * time.Hour)

	t.Logf("now: %+v\n", now)

	// Options to generate a CA cert.
	caCertOptions := CertOptions{
		Host:         "test_ca.com",
		NotBefore:    caCertNotBefore,
		NotAfter:     caCertNotAfter,
		SignerCert:   nil,
		SignerPriv:   nil,
		Org:          "MyOrg",
		IsCA:         true,
		IsSelfSigned: true,
		IsClient:     false,
		IsServer:     true,
		RSAKeySize:   512,
	}

	caCertPem, caPrivPem := GenCert(caCertOptions)
	fields := VerifyFields{
		notBefore:   caCertNotBefore,
		notAfter:    caCertNotAfter,
		extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		keyUsage:    x509.KeyUsageCertSign,
		isCA:        true,
		org:         "MyOrg",
	}
	if err := verifyCert(caPrivPem, caCertPem, nil, caCertOptions.Host, fields); err != nil {
		t.Error(err)
	}

	caCert, err := pki.ParsePemEncodedCertificate(caCertPem)
	if err != nil {
		t.Error(err)
	}

	caPriv, err := pki.ParsePemEncodedKey(caPrivPem)
	if err != nil {
		t.Error(err)
	}

	cases := []struct {
		certOptions  CertOptions
		verifyFields VerifyFields
	}{
		// These certs are signed by the CA cert
		{
			// server cert with DNS as SAN
			certOptions: CertOptions{
				Host:         "test_server.com",
				NotBefore:    now,
				NotAfter:     now.Add(time.Hour * 24),
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: VerifyFields{
				extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				isCA:        false,
				keyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				notAfter:    now.Add(time.Hour * 24),
				notBefore:   now,
				org:         "MyOrg",
			},
		},
		{
			// client cert with DNS as SAN
			certOptions: CertOptions{
				Host:         "test_client.com",
				NotBefore:    now,
				NotAfter:     now.Add(time.Hour * 36),
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: VerifyFields{
				extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
				isCA:        false,
				keyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				notAfter:    now.Add(time.Hour * 36),
				notBefore:   now,
				org:         "MyOrg",
			},
		},
		{
			// server cert with IP as SAN
			certOptions: CertOptions{
				Host:         "1.2.3.4",
				NotBefore:    now,
				NotAfter:     now.Add(time.Hour * 24),
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: VerifyFields{
				extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				isCA:        false,
				keyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				notAfter:    now.Add(time.Hour * 24),
				notBefore:   now,
				org:         "MyOrg",
			},
		},
		{
			// client cert with service account as SAN
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    now,
				NotAfter:     now.Add(time.Hour * 100),
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: VerifyFields{
				extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
				isCA:        false,
				keyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				notAfter:    now.Add(time.Hour * 100),
				notBefore:   now,
				org:         "MyOrg",
			},
		},
		{
			// server cert with service account as SAN
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    now,
				NotAfter:     now.Add(time.Hour * 50),
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: VerifyFields{
				extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				isCA:        false,
				keyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				notAfter:    now.Add(time.Hour * 50),
				notBefore:   now,
				org:         "MyOrg",
			},
		},
		{
			// a cert that can only be used as client-side cert
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    now,
				NotAfter:     now.Add(time.Hour * 50),
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     false,
				RSAKeySize:   512,
			},
			verifyFields: VerifyFields{
				extKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
				isCA:        false,
				keyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				notAfter:    now.Add(time.Hour * 50),
				notBefore:   now,
				org:         "MyOrg",
			},
		},
	}

	for _, c := range cases {
		certOptions := c.certOptions
		certPem, privPem := GenCert(certOptions)
		if err := verifyCert(privPem, certPem, caCertPem, certOptions.Host, c.verifyFields); err != nil {
			t.Error(err)
		}
	}
}

func verifyCert(privPem []byte, certPem []byte, rootCertPem []byte,
	host string, expectedFields VerifyFields) error {

	roots := x509.NewCertPool()
	var ok bool
	if rootCertPem == nil {
		ok = roots.AppendCertsFromPEM(certPem)
	} else {
		ok = roots.AppendCertsFromPEM(rootCertPem)
	}

	if !ok {
		return fmt.Errorf("failed to parse root certificate")
	}

	cert, err := pki.ParsePemEncodedCertificate(certPem)
	if err != nil {
		return err
	}

	san := host
	// uri scheme is currently not supported in go VerifyOptions. We verify
	// this uri at the end as a special case.
	if strings.HasPrefix(host, uriScheme) {
		san = ""
	}
	opts := x509.VerifyOptions{
		CurrentTime: now.Add(5 * time.Minute),
		DNSName:     san,
		Roots:       roots,
	}
	opts.KeyUsages = append(opts.KeyUsages, x509.ExtKeyUsageAny)

	if _, err = cert.Verify(opts); err != nil {
		return fmt.Errorf("failed to verify certificate: " + err.Error())
	}

	priv, err := pki.ParsePemEncodedKey(privPem)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(priv.(*rsa.PrivateKey).PublicKey, *cert.PublicKey.(*rsa.PublicKey)) {
		return fmt.Errorf("the generated private key and cert doesn't match")
	}

	certFields := VerifyFields{
		notBefore:   cert.NotBefore,
		notAfter:    cert.NotAfter,
		extKeyUsage: cert.ExtKeyUsage,
		keyUsage:    cert.KeyUsage,
		isCA:        cert.IsCA,
		org:         cert.Issuer.Organization[0],
	}
	if !reflect.DeepEqual(expectedFields, certFields) {
		return fmt.Errorf("{notBefore, notAfter, extKeyUsage, isCA, org}:\nexpected: %+v\nactual: %+v",
			expectedFields, certFields)
	}

	if strings.HasPrefix(host, uriScheme) {
		matchHost := false
		for _, e := range cert.Extensions {
			if strings.HasSuffix(string(e.Value[:]), host) {
				matchHost = true
				break
			}
		}
		if !matchHost {
			return fmt.Errorf("the certificate doesn't have the expected SAN for: %s", host)
		}
	}

	return nil
}
