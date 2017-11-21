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

package ca

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"istio.io/istio/security/pkg/pki"
	tu "istio.io/istio/security/pkg/pki/testutil"
)

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

func TestLoadSignerCredsFromFiles(t *testing.T) {
	testCases := map[string]struct {
		certFile    string
		keyFile     string
		expectedErr string
	}{
		"Good certificates": {
			certFile:    "testdata/cert.pem",
			keyFile:     "testdata/key.pem",
			expectedErr: "",
		},
		"Missing cert files": {
			certFile:    "testdata/cert-not-exist.pem",
			keyFile:     "testdata/key.pem",
			expectedErr: "certificate file reading failure (open testdata/cert-not-exist.pem: no such file or directory)",
		},
		"Missing key files": {
			certFile:    "testdata/cert.pem",
			keyFile:     "testdata/key-not-exist.pem",
			expectedErr: "private key file reading failure (open testdata/key-not-exist.pem: no such file or directory)",
		},
		"Bad cert files": {
			certFile:    "testdata/cert-bad.pem",
			keyFile:     "testdata/key.pem",
			expectedErr: "invalid PEM encoded certificate",
		},
		"Bad key files": {
			certFile:    "testdata/cert.pem",
			keyFile:     "testdata/key-bad.pem",
			expectedErr: "invalid PEM-encoded key",
		},
	}

	for id, tc := range testCases {
		cert, key, err := LoadSignerCredsFromFiles(tc.certFile, tc.keyFile)
		if len(tc.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), tc.expectedErr)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		if cert == nil || key == nil {
			t.Errorf("%v: Faild to load signer credeitials from files: %v, %v", id, tc.certFile, tc.keyFile)
		}
	}
}

func TestGenCert(t *testing.T) {
	// set "notBefore" to be 5 minutes ago, this ensures the issued certifiate to
	// be valid as of now.
	caCertNotBefore := now.Add(-5 * time.Minute)
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
	fields := &tu.VerifyFields{
		NotBefore:   caCertNotBefore,
		NotAfter:    caCertNotAfter,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageCertSign,
		IsCA:        true,
		Org:         "MyOrg",
	}
	if err := tu.VerifyCertificate(caPrivPem, caCertPem, caCertPem, caCertOptions.Host, fields); err != nil {
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

	notAfter := now.Add(time.Hour)
	notBefore := now.Add(-5 * time.Minute)
	cases := []struct {
		certOptions  CertOptions
		verifyFields *tu.VerifyFields
	}{
		// These certs are signed by the CA cert
		{
			// server cert with DNS as SAN
			certOptions: CertOptions{
				Host:         "test_server.com",
				NotBefore:    notBefore,
				NotAfter:     notAfter,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &tu.VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotAfter:    notAfter,
				NotBefore:   notBefore,
				Org:         "MyOrg",
			},
		},
		{
			// client cert with DNS as SAN
			certOptions: CertOptions{
				Host:         "test_client.com",
				NotBefore:    notBefore,
				NotAfter:     notAfter,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &tu.VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotAfter:    notAfter,
				NotBefore:   notBefore,
				Org:         "MyOrg",
			},
		},
		{
			// server cert with IP as SAN
			certOptions: CertOptions{
				Host:         "1.2.3.4",
				NotBefore:    notBefore,
				NotAfter:     notAfter,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &tu.VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotAfter:    notAfter,
				NotBefore:   notBefore,
				Org:         "MyOrg",
			},
		},
		{
			// client cert with service account as SAN
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				NotAfter:     notAfter,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &tu.VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotAfter:    notAfter,
				NotBefore:   notBefore,
				Org:         "MyOrg",
			},
		},
		{
			// server cert with service account as SAN
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				NotAfter:     notAfter,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &tu.VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotAfter:    notAfter,
				NotBefore:   notBefore,
				Org:         "MyOrg",
			},
		},
		{
			// a cert that can only be used as client-side cert
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				NotAfter:     notAfter,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     false,
				RSAKeySize:   512,
			},
			verifyFields: &tu.VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotAfter:    notAfter,
				NotBefore:   notBefore,
				Org:         "MyOrg",
			},
		},
	}

	for _, c := range cases {
		certOptions := c.certOptions
		certPem, privPem := GenCert(certOptions)
		if e := tu.VerifyCertificate(privPem, certPem, caCertPem, certOptions.Host, c.verifyFields); e != nil {
			t.Error(e)
		}
	}
}
