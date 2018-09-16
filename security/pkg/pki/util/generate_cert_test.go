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

package util

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"strings"
	"testing"
	"time"
)

var now = time.Now().Round(time.Second).UTC()

func TestGenCertKeyFromOptions(t *testing.T) {
	// set "notBefore" to be one hour ago, this ensures the issued certifiate to
	// be valid as of now.
	caCertNotBefore := now.Add(-time.Hour)
	caCertTTL := 24 * time.Hour

	// Options to generate a CA cert.
	caCertOptions := CertOptions{
		Host:         "test_ca.com",
		NotBefore:    caCertNotBefore,
		TTL:          caCertTTL,
		SignerCert:   nil,
		SignerPriv:   nil,
		Org:          "MyOrg",
		IsCA:         true,
		IsSelfSigned: true,
		IsClient:     false,
		IsServer:     true,
		RSAKeySize:   512,
	}

	caCertPem, caPrivPem, err := GenCertKeyFromOptions(caCertOptions)
	if err != nil {
		t.Error(err)
	}

	fields := &VerifyFields{
		NotBefore:   caCertNotBefore,
		TTL:         caCertTTL,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageCertSign,
		IsCA:        true,
		Org:         "MyOrg",
		Host:        caCertOptions.Host,
	}
	if VerifyCertificate(caPrivPem, caCertPem, caCertPem, fields) != nil {
		t.Error(err)
	}

	caCert, err := ParsePemEncodedCertificate(caCertPem)
	if err != nil {
		t.Error(err)
	}

	caPriv, err := ParsePemEncodedKey(caPrivPem)
	if err != nil {
		t.Error(err)
	}

	notBefore := now.Add(-5 * time.Minute)
	ttl := time.Hour
	cases := []struct {
		name         string
		certOptions  CertOptions
		verifyFields *VerifyFields
	}{
		// These certs are signed by the CA cert
		{
			name: "Server cert with DNS SAN",
			certOptions: CertOptions{
				Host:         "test_server.com",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotBefore:   notBefore,
				TTL:         ttl,
				Org:         "MyOrg",
			},
		},
		{
			name: "Server and client cert with DNS SAN",
			certOptions: CertOptions{
				Host:         "test_client.com",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotBefore:   notBefore,
				TTL:         ttl,
				Org:         "MyOrg",
			},
		},
		{
			name: "Server cert with IP SAN",
			certOptions: CertOptions{
				Host:         "1.2.3.4",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotBefore:   notBefore,
				TTL:         ttl,
				Org:         "MyOrg",
			},
		},
		{
			name: "Client cert with URI SAN",
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   512,
			},
			verifyFields: &VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotBefore:   notBefore,
				TTL:         ttl,
				Org:         "MyOrg",
			},
		},
		{
			name: "Server cert with DNS for webhook",
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo,bar.foo.svcs",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				RSAKeySize:   2048,
			},
			verifyFields: &VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotBefore:   notBefore,
				TTL:         ttl,
				Org:         "MyOrg",
			},
		},
		{
			name: "Generate cert with multiple host names",
			certOptions: CertOptions{
				Host:       "a,b",
				NotBefore:  notBefore,
				TTL:        ttl,
				SignerCert: caCert,
				SignerPriv: caPriv,
				RSAKeySize: 2048,
			},
			verifyFields: &VerifyFields{
				IsCA:     false,
				KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			},
		},
		{
			name: "Generate dual-use cert",
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   512,
				IsDualUse:    true,
			},
			verifyFields: &VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotBefore:   notBefore,
				TTL:         ttl,
				Org:         "MyOrg",
				CommonName:  "spiffe://domain/ns/bar/sa/foo",
			},
		},
		{
			name: "Generate dual-use cert with multiple host names",
			certOptions: CertOptions{
				Host:         "a,b,c",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   512,
				IsDualUse:    true,
			},
			verifyFields: &VerifyFields{
				ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
				IsCA:        false,
				KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
				NotBefore:   notBefore,
				TTL:         ttl,
				Org:         "MyOrg",
				CommonName:  "a", // only first host used for CN
			},
		},
	}

	for _, c := range cases {
		certOptions := c.certOptions
		certPem, privPem, err := GenCertKeyFromOptions(certOptions)
		if err != nil {
			t.Errorf("[%s] cert/key generation error: %v", c.name, err)
		}

		for _, host := range strings.Split(certOptions.Host, ",") {
			c.verifyFields.Host = host
			if err := VerifyCertificate(privPem, certPem, caCertPem, c.verifyFields); err != nil {
				t.Errorf("[%s] cert verification error: %v", c.name, err)
			}
		}
	}
}

func TestGenCertFromCSR(t *testing.T) {
	keyFile := "../testdata/key.pem"
	certFile := "../testdata/cert.pem"
	keycert, err := NewVerifiedKeyCertBundleFromFile(certFile, keyFile, "", certFile)
	if err != nil {
		t.Errorf("Failed to load CA key and cert from files: %s, %s", keyFile, certFile)
	}
	signingCert, signingKey, _, _ := keycert.GetAll()

	// Then generates signee's key pairs.
	signeeKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Errorf("failed to generate signee key pair %v", err)
	}

	cases := []struct {
		name        string
		subjectIDs  []string
		csrTemplate *x509.CertificateRequest
	}{
		{
			name:       "Single subject ID",
			subjectIDs: []string{"spiffe://test.com/abc/def"},
			csrTemplate: &x509.CertificateRequest{
				SignatureAlgorithm: x509.SHA256WithRSA,
				DNSNames:           []string{"name_in_csr"},
				Version:            3,
			},
		},
		{
			name:       "Two subject IDs",
			subjectIDs: []string{"spiffe://test.com/abc/def", "test.com"},
			csrTemplate: &x509.CertificateRequest{
				SignatureAlgorithm: x509.SHA256WithRSA,
				DNSNames:           []string{"name_in_csr"},
				Version:            3,
			},
		},
		{
			name:       "Common name in CSR",
			subjectIDs: []string{"test.com"},
			csrTemplate: &x509.CertificateRequest{
				Subject:            pkix.Name{CommonName: "common_name"},
				SignatureAlgorithm: x509.SHA256WithRSA,
				DNSNames:           []string{"name_in_csr"},
				Version:            3,
			},
		},
	}

	for _, c := range cases {
		derBytes, err := x509.CreateCertificateRequest(rand.Reader, c.csrTemplate, signeeKey)
		if err != nil {
			t.Error("failed to create certificate request")
		}
		csr, err := x509.ParseCertificateRequest(derBytes)
		if err != nil {
			t.Errorf("failed to parse certificate request %v", err)
		}
		derBytes, err = GenCertFromCSR(csr, signingCert, &signeeKey.PublicKey, *signingKey, c.subjectIDs, time.Hour, false)
		if err != nil {
			t.Errorf("failed to GenCertFromCSR, error %v", err)
		}

		// Verify the certificate.
		out, err := x509.ParseCertificate(derBytes)
		if err != nil {
			t.Errorf("failed to parse generated certificate %v", err)
		}
		if len(c.csrTemplate.Subject.CommonName) == 0 {
			if len(out.Subject.CommonName) > 0 {
				t.Errorf("Common name should be empty, but got %s", out.Subject.CommonName)
			}
		} else if out.Subject.CommonName != c.subjectIDs[0] {
			t.Errorf("Unmatched common name, expected %s, got %s", c.subjectIDs[0], out.Subject.CommonName)
		}
		if len(out.Subject.Organization) > 0 {
			t.Errorf("Organization should be empty, but got %s", out.Subject.Organization)
		}

		ids, err := ExtractIDs(out.Extensions)
		if err != nil {
			t.Errorf("failed to extract IDs from cert extension: %v", err)
		}
		if len(c.subjectIDs) != len(ids) {
			t.Errorf("Wrong number of IDs encoded. Expected %d, but got %d.", len(c.subjectIDs), len(ids))
		}
		if len(c.subjectIDs) == 1 && c.subjectIDs[0] != ids[0] {
			t.Errorf("incorrect ID encoded: %v VS (expected) %v", ids[0], c.subjectIDs[0])
		}
		if len(c.subjectIDs) == 2 {
			if !(c.subjectIDs[0] == ids[0] && c.subjectIDs[1] == ids[1] || c.subjectIDs[0] == ids[1] && c.subjectIDs[1] == ids[0]) {
				t.Errorf("incorrect IDs encoded: %v, %v VS (expected) %v, %v", ids[0], ids[1], c.subjectIDs[0], c.subjectIDs[1])
			}
		}
		pool := x509.NewCertPool()
		pool.AddCert(signingCert)
		vo := x509.VerifyOptions{
			Roots: pool,
		}
		if _, err := out.Verify(vo); err != nil {
			t.Errorf("verification of the signed certificate failed %v", err)
		}
	}
}

func TestLoadSignerCredsFromFiles(t *testing.T) {
	testCases := map[string]struct {
		certFile    string
		keyFile     string
		expectedErr string
	}{
		"Good certificates": {
			certFile:    "../testdata/cert.pem",
			keyFile:     "../testdata/key.pem",
			expectedErr: "",
		},
		"Missing cert files": {
			certFile:    "../testdata/cert-not-exist.pem",
			keyFile:     "../testdata/key.pem",
			expectedErr: "certificate file reading failure (open ../testdata/cert-not-exist.pem: no such file or directory)",
		},
		"Missing key files": {
			certFile:    "../testdata/cert.pem",
			keyFile:     "../testdata/key-not-exist.pem",
			expectedErr: "private key file reading failure (open ../testdata/key-not-exist.pem: no such file or directory)",
		},
		"Bad cert files": {
			certFile:    "../testdata/cert-parse-fail.pem",
			keyFile:     "../testdata/key.pem",
			expectedErr: "pem encoded cert parsing failure (invalid PEM encoded certificate)",
		},
		"Bad key files": {
			certFile:    "../testdata/cert.pem",
			keyFile:     "../testdata/key-parse-fail.pem",
			expectedErr: "pem encoded key parsing failure (invalid PEM-encoded key)",
		},
	}

	for id, tc := range testCases {
		cert, key, err := LoadSignerCredsFromFiles(tc.certFile, tc.keyFile)
		if len(tc.expectedErr) > 0 {
			if err == nil {
				t.Errorf("[%s] Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.expectedErr {
				t.Errorf("[%s] incorrect error message: %s VS (expected) %s",
					id, err.Error(), tc.expectedErr)
			}
			continue
		} else if err != nil {
			t.Fatalf("[%s] Unexpected Error: %v", id, err)
		}

		if cert == nil || key == nil {
			t.Errorf("[%s] Faild to load signer credeitials from files: %v, %v", id, tc.certFile, tc.keyFile)
		}
	}
}
