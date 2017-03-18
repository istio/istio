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
	"reflect"
	"strings"
	"testing"
	"time"
)

// VerifyFields contains the certficate fields to verify in the test.
type VerifyFields struct {
	notBefore   time.Time
	notAfter    time.Time
	extKeyUsage x509.ExtKeyUsage
	keyUsage    x509.KeyUsage
	isCA        bool
	org         string
}

var now = time.Now().Round(time.Second).UTC()

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
	}

	caCertPem, caPrivPem := GenCert(caCertOptions)
	verifyCert(t, caPrivPem, caCertPem, nil, caCertOptions.Host, VerifyFields{
		notBefore:   caCertNotBefore,
		notAfter:    caCertNotAfter,
		extKeyUsage: x509.ExtKeyUsageServerAuth,
		keyUsage:    x509.KeyUsageCertSign,
		isCA:        true,
		org:         "MyOrg",
	})

	caCert := parsePemEncodedCertificate(caCertPem)
	caPriv := parsePemEncodedKey(caCert.PublicKeyAlgorithm, caPrivPem)
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
			},
			verifyFields: VerifyFields{
				extKeyUsage: x509.ExtKeyUsageServerAuth,
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
			},
			verifyFields: VerifyFields{
				extKeyUsage: x509.ExtKeyUsageClientAuth,
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
			},
			verifyFields: VerifyFields{
				extKeyUsage: x509.ExtKeyUsageServerAuth,
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
				Host:         "istio:foo.serviceaccount.com",
				NotBefore:    now,
				NotAfter:     now.Add(time.Hour * 100),
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     true,
			},
			verifyFields: VerifyFields{
				extKeyUsage: x509.ExtKeyUsageClientAuth,
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
				Host:         "istio:bar.serviceaccount.com",
				NotBefore:    now,
				NotAfter:     now.Add(time.Hour * 50),
				SignerCert:   caCert,
				SignerPriv:   caPriv,
				Org:          "",
				IsCA:         false,
				IsSelfSigned: false,
				IsClient:     false,
			},
			verifyFields: VerifyFields{
				extKeyUsage: x509.ExtKeyUsageServerAuth,
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
		verifyCert(t, privPem, certPem, caCertPem, certOptions.Host, c.verifyFields)
	}
}

func verifyCert(
	t *testing.T, privPem []byte, certPem []byte, rootCertPem []byte,
	host string, expectedFields VerifyFields) {

	roots := x509.NewCertPool()
	var ok bool
	if rootCertPem == nil {
		ok = roots.AppendCertsFromPEM(certPem)
	} else {
		ok = roots.AppendCertsFromPEM(rootCertPem)
	}

	if !ok {
		t.Errorf("failed to parse root certificate")
	}

	block, _ := pem.Decode(certPem)
	if block == nil {
		t.Errorf("failed to parse certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Errorf("failed to parse certificate: " + err.Error())
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
		t.Errorf("failed to verify certificate: " + err.Error())
	}

	block, _ = pem.Decode(privPem)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Errorf("failed to decode PEM block containing private key")
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Errorf("failed to parse private key: " + err.Error())
	}
	if !reflect.DeepEqual(priv.PublicKey, *cert.PublicKey.(*rsa.PublicKey)) {
		t.Errorf("the generated private key and cert doesn't match.")
	}

	certFields := VerifyFields{
		notBefore:   cert.NotBefore,
		notAfter:    cert.NotAfter,
		extKeyUsage: cert.ExtKeyUsage[0],
		keyUsage:    cert.KeyUsage,
		isCA:        cert.IsCA,
		org:         cert.Issuer.Organization[0],
	}
	if !reflect.DeepEqual(expectedFields, certFields) {
		t.Errorf("{notBefore, notAfter, extKeyUsage, isCA, org}:")
		t.Errorf("expected: %+v", expectedFields)
		t.Errorf("actual: %+v", certFields)
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
			t.Errorf("the certificate doesn't have the expected SAN for: %s", host)
		}
	}
}
