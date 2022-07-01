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

package util

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"strings"
	"testing"
	"time"
)

var (
	now     = time.Now().Round(time.Second).UTC()
	certPem = `-----BEGIN CERTIFICATE-----
MIIC5jCCAc6gAwIBAgIRAIDngVC9z3HRR4DdOvnKO38wDQYJKoZIhvcNAQELBQAw
HDEaMBgGA1UEChMRazhzLmNsdXN0ZXIubG9jYWwwHhcNMTcxMTE1MDAzMzUyWhcN
MjcxMTEzMDAzMzUyWjAcMRowGAYDVQQKExFrOHMuY2x1c3Rlci5sb2NhbDCCASIw
DQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAOLNvPT59LqfuJFZEkHNg5BABXqX
Yy0yu/t60lsd+Z43eTjEctnhyk45/4KE909wSVdzrq6jvlWCki/iHLkbnZ9Bfk0E
mGwP2TOjihOPWH9F6i8yO6GI5wqeQki7yiT/NozMo/vSNrso0Xa8WoQSN6svziP8
b9OeSIIMWIa8F1vD1EOvyHYlZHPMw/IJCqAxQef50FpVu2sB8t4FKeswyv0+Twh+
J75hB9OiDnM1G8Ex3An4G6KeUX8ptuJS6aLemuZrqOG6dsaG4HrC6OuIuxfyRbe2
zJyyHeOnGhozGVXS9TpCp3Mkr54NyKl4+p3XfeVtuBeG7UUvHS7EvS+2Bl0CAwEA
AaMjMCEwDgYDVR0PAQH/BAQDAgIEMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcN
AQELBQADggEBAEe3XmOAod4CoLkOWNFP6RbtSO3jDO6bzV0qOioS8Yj55eQ78hR9
R14TG5+QCHXz4W3FQMsgEg1OQodmw6lhupkvQn1ZP/zf3a/kfTuK0VOIzqeKe4TI
IgsccmELXGdxojN23311/tAcq3d8pSTKusH7KNwAQWmerkxB6wzSHTiJWICFJzs4
RWeVWm0l72yZcYFaZ/LBkn+gRyV88r51BR+IR7sMDB7k6hsdMWdxvNESr1h9JU+Q
NbOwbkIREzozcpaJ2eSiksLkPIxh8/zaULUpPbVMOeOIybUK4iW+K2FyibCc5r9d
vbw9mUuRBuYCROUaNv2/TAkauxVPCYPq7Ow=
-----END CERTIFICATE-----`
)

func TestGenCertKeyFromOptions(t *testing.T) {
	// set "notBefore" to be one hour ago, this ensures the issued certificate to
	// be valid as of now.
	caCertNotBefore := now.Add(-time.Hour)
	caCertTTL := 24 * time.Hour
	host := "test_ca.com"

	// Options to generate a CA cert with RSA.
	rsaCaCertOptions := CertOptions{
		Host:         host,
		NotBefore:    caCertNotBefore,
		TTL:          caCertTTL,
		SignerCert:   nil,
		SignerPriv:   nil,
		Org:          "MyOrg",
		IsCA:         true,
		IsSelfSigned: true,
		IsClient:     false,
		IsServer:     true,
		RSAKeySize:   2048,
	}

	rsaCaCertPem, rsaCaPrivPem, err := GenCertKeyFromOptions(rsaCaCertOptions)
	if err != nil {
		t.Fatal(err)
	}

	// Options to generate a CA cert with EC.
	ecCaCertOptions := CertOptions{
		Host:         host,
		NotBefore:    caCertNotBefore,
		TTL:          caCertTTL,
		SignerCert:   nil,
		SignerPriv:   nil,
		Org:          "MyOrg",
		IsCA:         true,
		IsSelfSigned: true,
		IsClient:     false,
		IsServer:     true,
		ECSigAlg:     EcdsaSigAlg,
	}

	ecCaCertPem, ecCaPrivPem, err := GenCertKeyFromOptions(ecCaCertOptions)
	if err != nil {
		t.Fatal(err)
	}

	fields := &VerifyFields{
		NotBefore:   caCertNotBefore,
		TTL:         caCertTTL,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageCertSign,
		IsCA:        true,
		Org:         "MyOrg",
		Host:        host,
	}
	if VerifyCertificate(rsaCaPrivPem, rsaCaCertPem, rsaCaCertPem, fields) != nil {
		t.Fatal(err)
	}

	if VerifyCertificate(ecCaPrivPem, ecCaCertPem, ecCaCertPem, fields) != nil {
		t.Fatal(err)
	}

	rsaCaCert, err := ParsePemEncodedCertificate(rsaCaCertPem)
	if err != nil {
		t.Fatal(err)
	}

	ecCaCert, err := ParsePemEncodedCertificate(ecCaCertPem)
	if err != nil {
		t.Fatal(err)
	}

	rsaCaPriv, err := ParsePemEncodedKey(rsaCaPrivPem)
	if err != nil {
		t.Fatal(err)
	}

	ecCaPriv, err := ParsePemEncodedKey(ecCaPrivPem)
	if err != nil {
		t.Fatal(err)
	}

	notBefore := now.Add(-5 * time.Minute)
	ttl := time.Hour
	cases := map[string]struct {
		certOptions  CertOptions
		verifyFields *VerifyFields
	}{
		// These certs are signed by the CA cert
		"RSA: Server cert with DNS SAN": {
			certOptions: CertOptions{
				Host:         "test_server.com",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   rsaCaCert,
				SignerPriv:   rsaCaPriv,
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
		"RSA: Server and client cert with DNS SAN": {
			certOptions: CertOptions{
				Host:         "test_client.com",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   rsaCaCert,
				SignerPriv:   rsaCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   2048,
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
		"RSA: Server cert with IP SAN": {
			certOptions: CertOptions{
				Host:         "1.2.3.4",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   rsaCaCert,
				SignerPriv:   rsaCaPriv,
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
		"RSA: Client cert with URI SAN": {
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   rsaCaCert,
				SignerPriv:   rsaCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   2048,
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
		"RSA: Server cert with DNS for webhook": {
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo,bar.foo.svcs",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   rsaCaCert,
				SignerPriv:   rsaCaPriv,
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
		"RSA: Generate cert with multiple host names": {
			certOptions: CertOptions{
				Host:       "a,b",
				NotBefore:  notBefore,
				TTL:        ttl,
				SignerCert: rsaCaCert,
				SignerPriv: rsaCaPriv,
				RSAKeySize: 2048,
			},
			verifyFields: &VerifyFields{
				IsCA:     false,
				KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			},
		},
		"RSA: Generate dual-use cert": {
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   rsaCaCert,
				SignerPriv:   rsaCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   2048,
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
		"RSA: Generate dual-use cert with multiple host names": {
			certOptions: CertOptions{
				Host:         "a,b,c",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   rsaCaCert,
				SignerPriv:   rsaCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   2048,
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
		"RSA: Generate PKCS8 private key": {
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   rsaCaCert,
				SignerPriv:   rsaCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				RSAKeySize:   2048,
				PKCS8Key:     true,
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
		"EC: Server cert with DNS SAN": {
			certOptions: CertOptions{
				Host:         "test_server.com",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   ecCaCert,
				SignerPriv:   ecCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				ECSigAlg:     EcdsaSigAlg,
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
		"EC: Server and client cert with DNS SAN": {
			certOptions: CertOptions{
				Host:         "test_client.com",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   ecCaCert,
				SignerPriv:   ecCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				ECSigAlg:     EcdsaSigAlg,
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
		"EC: Server cert with IP SAN": {
			certOptions: CertOptions{
				Host:         "1.2.3.4",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   ecCaCert,
				SignerPriv:   ecCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				ECSigAlg:     EcdsaSigAlg,
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
		"EC: Client cert with URI SAN": {
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   ecCaCert,
				SignerPriv:   ecCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				ECSigAlg:     EcdsaSigAlg,
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
		"EC: Server cert with DNS for webhook": {
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo,bar.foo.svcs",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   ecCaCert,
				SignerPriv:   ecCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
				IsServer:     true,
				ECSigAlg:     EcdsaSigAlg,
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
		"EC: Generate cert with multiple host names": {
			certOptions: CertOptions{
				Host:       "a,b",
				NotBefore:  notBefore,
				TTL:        ttl,
				SignerCert: ecCaCert,
				SignerPriv: ecCaPriv,
				ECSigAlg:   EcdsaSigAlg,
			},
			verifyFields: &VerifyFields{
				IsCA:     false,
				KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			},
		},
		"EC: Generate dual-use cert": {
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   ecCaCert,
				SignerPriv:   ecCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				ECSigAlg:     EcdsaSigAlg,
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
		"EC: Generate dual-use cert with multiple host names": {
			certOptions: CertOptions{
				Host:         "a,b,c",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   ecCaCert,
				SignerPriv:   ecCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				ECSigAlg:     EcdsaSigAlg,
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
		"EC: Generate PKCS8 private key": {
			certOptions: CertOptions{
				Host:         "spiffe://domain/ns/bar/sa/foo",
				NotBefore:    notBefore,
				TTL:          ttl,
				SignerCert:   ecCaCert,
				SignerPriv:   ecCaPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
				IsServer:     true,
				ECSigAlg:     EcdsaSigAlg,
				PKCS8Key:     true,
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
	}

	for id, c := range cases {
		t.Run(id, func(t *testing.T) {
			certOptions := c.certOptions
			certPem, privPem, err := GenCertKeyFromOptions(certOptions)
			if err != nil {
				t.Errorf("[%s] cert/key generation error: %v", id, err)
			}

			for _, host := range strings.Split(certOptions.Host, ",") {
				c.verifyFields.Host = host
				root := rsaCaCertPem
				if c.certOptions.ECSigAlg != "" {
					root = ecCaCertPem
				}
				if err := VerifyCertificate(privPem, certPem, root, c.verifyFields); err != nil {
					t.Errorf("[%s] cert verification error: %v", id, err)
				}
			}
		})
	}
}

func TestGenCertFromCSR(t *testing.T) {
	keyFile := "../testdata/key.pem"
	certFile := "../testdata/cert.pem"
	keycert, err := NewVerifiedKeyCertBundleFromFile(certFile, keyFile, nil, certFile)
	if err != nil {
		t.Errorf("Failed to load CA key and cert from files: %s, %s", keyFile, certFile)
	}
	signingCert, signingKey, _, _ := keycert.GetAll()

	// Then generates signee's key pairs.
	rsaSigneeKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Errorf("failed to generate signee key pair %v", err)
	}
	ecdsaSigneeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Errorf("failed to generate signee key pair %v", err)
	}
	_, ed25519SigneeKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Errorf("failed to generate signee key pair %v", err)
	}

	cases := []struct {
		name        string
		subjectIDs  []string
		signeeKey   crypto.Signer
		csrTemplate *x509.CertificateRequest
	}{
		{
			name:       "Single subject ID",
			subjectIDs: []string{"spiffe://test.com/abc/def"},
			signeeKey:  rsaSigneeKey,
			csrTemplate: &x509.CertificateRequest{
				SignatureAlgorithm: x509.SHA256WithRSA,
				DNSNames:           []string{"name_in_csr"},
				Version:            3,
			},
		},
		{
			name:       "Two subject IDs",
			subjectIDs: []string{"spiffe://test.com/abc/def", "test.com"},
			signeeKey:  rsaSigneeKey,
			csrTemplate: &x509.CertificateRequest{
				SignatureAlgorithm: x509.SHA256WithRSA,
				DNSNames:           []string{"name_in_csr"},
				Version:            3,
			},
		},
		{
			name:       "Common name in CSR",
			subjectIDs: []string{"test.com"},
			signeeKey:  rsaSigneeKey,
			csrTemplate: &x509.CertificateRequest{
				Subject:            pkix.Name{CommonName: "common_name"},
				SignatureAlgorithm: x509.SHA256WithRSA,
				DNSNames:           []string{"name_in_csr"},
				Version:            3,
			},
		},
		{
			name:       "Use ECDSA Signee Key",
			subjectIDs: []string{"test.com"},
			signeeKey:  ecdsaSigneeKey,
			csrTemplate: &x509.CertificateRequest{
				Subject:            pkix.Name{CommonName: "common_name"},
				SignatureAlgorithm: x509.ECDSAWithSHA256,
				DNSNames:           []string{"name_in_csr"},
				Version:            3,
			},
		},
		{
			name:       "Use ED25519 Signee Key",
			subjectIDs: []string{"test.com"},
			signeeKey:  ed25519SigneeKey,
			csrTemplate: &x509.CertificateRequest{
				Subject:            pkix.Name{CommonName: "common_name"},
				SignatureAlgorithm: x509.PureEd25519,
				DNSNames:           []string{"name_in_csr"},
				Version:            3,
			},
		},
	}

	for _, c := range cases {
		derBytes, err := x509.CreateCertificateRequest(rand.Reader, c.csrTemplate, c.signeeKey)
		if err != nil {
			t.Errorf("failed to create certificate request %v", err)
		}
		csr, err := x509.ParseCertificateRequest(derBytes)
		if err != nil {
			t.Errorf("failed to parse certificate request %v", err)
		}
		derBytes, err = GenCertFromCSR(csr, signingCert, c.signeeKey.Public(), *signingKey, c.subjectIDs, time.Hour, false)
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
			t.Errorf("[%s] Failed to load signer credentials from files: %v, %v", id, tc.certFile, tc.keyFile)
		}
	}
}

// TestAppendRootCerts verifies that AppendRootCerts works properly.
func TestAppendRootCerts(t *testing.T) {
	testCases := map[string]struct {
		pemCert          []byte
		rootFile         string
		expectedErr      string
		expectedRootCert []byte
	}{
		"Empty pem cert and root file": {
			pemCert:          []byte{},
			rootFile:         "",
			expectedErr:      "",
			expectedRootCert: []byte{},
		},
		"Non empty root file": {
			pemCert:          []byte{},
			rootFile:         "../testdata/cert.pem",
			expectedErr:      "",
			expectedRootCert: []byte(certPem + "\n"),
		},
		"Non empty pem cert": {
			pemCert:          []byte(certPem),
			rootFile:         "",
			expectedErr:      "",
			expectedRootCert: []byte(certPem),
		},
		"Non empty pem cert and non empty root file": {
			pemCert:          []byte(certPem),
			rootFile:         "../testdata/cert.pem",
			expectedErr:      "",
			expectedRootCert: append([]byte(certPem+"\n"), []byte(certPem+"\n")...),
		},
		"Not existing root file": {
			pemCert:  []byte{},
			rootFile: "../testdata/notexistcert.pem",
			expectedErr: "failed to read root certificates (open ../testdata/notexistcert.pem: " +
				"no such file or directory)",
			expectedRootCert: []byte{},
		},
	}

	for id, tc := range testCases {
		rc, err := AppendRootCerts(tc.pemCert, tc.rootFile)
		if len(tc.expectedErr) > 0 {
			if err == nil {
				t.Errorf("[%s] Succeeded. Error expected: %s", id, tc.expectedErr)
			} else if err.Error() != tc.expectedErr {
				t.Errorf("[%s] incorrect error message: %s VS (expected) %s",
					id, err.Error(), tc.expectedErr)
			}
		} else if err != nil {
			t.Errorf("[%s] Unexpected error: %s", id, err.Error())
		}
		if !bytes.Equal(rc, tc.expectedRootCert) {
			t.Errorf("[%s] root cert does not match. %v VS (expected) %v", id, rc, tc.expectedRootCert)
		}
	}
}

// TestGenRootCertFromExistingKey creates original root certificate and private key, and then
// uses the private key to generate a new root certificate. Verifies that the new root certificate
// matches old root certificate except lifetime changes.
func TestGenRootCertFromExistingKey(t *testing.T) {
	// Generate root certificate and private key
	caCertTTL := 24 * time.Hour
	oldOrg := "old org"
	caKeySize := 2048
	caCertOptions := CertOptions{
		TTL:          caCertTTL,
		Org:          oldOrg,
		IsCA:         true,
		IsSelfSigned: true,
		RSAKeySize:   caKeySize,
		IsDualUse:    false,
	}
	oldRootCertPem, oldRootKeyPem, err := GenCertKeyFromOptions(caCertOptions)
	if err != nil {
		t.Errorf("failed to generate root certificate from options: %v", err)
	}

	// Rotate root certificate using the old private key.
	// 1. get cert option from old root certificate.
	oldCertOptions, err := GetCertOptionsFromExistingCert(oldRootCertPem)
	if err != nil {
		t.Errorf("failed to generate cert options from existing root certificate: %v", err)
	}
	// 2. create cert option for new root certificate.
	defaultOrg := "default org"
	// Verify that changing RSA key size does not change private key, as the key is reused.
	defaultRSAKeySize := 4096
	// Create a default cert options
	newCertOptions := CertOptions{
		TTL:           caCertTTL,
		SignerPrivPem: oldRootKeyPem,
		Org:           defaultOrg,
		IsCA:          true,
		IsSelfSigned:  true,
		RSAKeySize:    defaultRSAKeySize,
		IsDualUse:     false,
	}
	// Merge cert options.
	newCertOptions = MergeCertOptions(newCertOptions, oldCertOptions)
	if newCertOptions.Org != oldOrg && newCertOptions.Org == defaultOrg {
		t.Error("Org in cert options should be overwritten")
	}
	// 3. create new root certificate.
	newRootCertPem, newRootKeyPem, err := GenRootCertFromExistingKey(newCertOptions)
	if err != nil {
		t.Errorf("failed to generate root certificate from existing key: %v", err)
	}

	// Verifies that private key does not change, and certificates match.
	if !bytes.Equal(oldRootKeyPem, newRootKeyPem) {
		t.Errorf("private key should not change")
	}
	keyLen, err := getPublicKeySizeInBits(newRootKeyPem)
	if err != nil {
		t.Errorf("failed to parse private key: %v", err)
	}
	if keyLen != caKeySize {
		t.Errorf("Public key size should not change, (got %d) vs (expected %d)",
			keyLen, caKeySize)
	}

	oldRootCert, _ := ParsePemEncodedCertificate(oldRootCertPem)
	newRootCert, _ := ParsePemEncodedCertificate(newRootCertPem)
	if oldRootCert.Subject.String() != newRootCert.Subject.String() {
		t.Errorf("certificate Subject does not match (old: %s) vs (new: %s)",
			oldRootCert.Subject.String(), newRootCert.Subject.String())
	}
	if oldRootCert.Issuer.String() != newRootCert.Issuer.String() {
		t.Errorf("certificate Issuer does not match (old: %s) vs (new: %s)",
			oldRootCert.Issuer.String(), newRootCert.Issuer.String())
	}
	if oldRootCert.IsCA != newRootCert.IsCA {
		t.Errorf("certificate IsCA does not match (old: %t) vs (new: %t)",
			oldRootCert.IsCA, newRootCert.IsCA)
	}
	if oldRootCert.Version != newRootCert.Version {
		t.Errorf("certificate Version does not match (old: %d) vs (new: %d)",
			oldRootCert.Version, newRootCert.Version)
	}
	if oldRootCert.PublicKeyAlgorithm != newRootCert.PublicKeyAlgorithm {
		t.Errorf("public key algorithm does not match (old: %s) vs (new: %s)",
			oldRootCert.PublicKeyAlgorithm.String(), newRootCert.PublicKeyAlgorithm.String())
	}
}

func getPublicKeySizeInBits(keyPem []byte) (int, error) {
	privateKey, err := ParsePemEncodedKey(keyPem)
	if err != nil {
		return 0, err
	}
	k := privateKey.(*rsa.PrivateKey)
	return k.PublicKey.Size() * 8, nil
}

// TestMergeCertOptions verifies that cert option fields are overwritten.
func TestMergeCertOptions(t *testing.T) {
	certTTL := 240 * time.Hour
	org := "old org"
	keySize := 512
	defaultCertOptions := CertOptions{
		TTL:          certTTL,
		Org:          org,
		IsCA:         true,
		IsSelfSigned: true,
		RSAKeySize:   keySize,
		IsDualUse:    false,
	}

	deltaCertTTL := 1 * time.Hour
	deltaOrg := "delta org"
	deltaKeySize := 1024
	deltaCertOptions := CertOptions{
		TTL:          deltaCertTTL,
		Org:          deltaOrg,
		IsCA:         true,
		IsSelfSigned: true,
		RSAKeySize:   deltaKeySize,
		IsDualUse:    true,
	}

	mergedCertOptions := MergeCertOptions(defaultCertOptions, deltaCertOptions)
	if mergedCertOptions.Org != deltaCertOptions.Org {
		t.Errorf("Org does not match, (get %s) vs (expected %s)",
			mergedCertOptions.Org, deltaCertOptions.Org)
	}
	if mergedCertOptions.TTL != defaultCertOptions.TTL {
		t.Errorf("TTL does not match, (get %s) vs (expected %s)",
			mergedCertOptions.TTL.String(), deltaCertOptions.TTL.String())
	}
	if mergedCertOptions.IsCA != defaultCertOptions.IsCA {
		t.Errorf("IsCA does not match, (get %t) vs (expected %t)",
			mergedCertOptions.IsCA, deltaCertOptions.IsCA)
	}
	if mergedCertOptions.RSAKeySize != defaultCertOptions.RSAKeySize {
		t.Errorf("TTL does not match, (get %d) vs (expected %d)",
			mergedCertOptions.RSAKeySize, deltaCertOptions.RSAKeySize)
	}
	if mergedCertOptions.IsDualUse != defaultCertOptions.IsDualUse {
		t.Errorf("IsDualUse does not match, (get %t) vs (expected %t)",
			mergedCertOptions.IsDualUse, deltaCertOptions.IsDualUse)
	}
}
