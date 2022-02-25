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
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestGenCSR(t *testing.T) {
	// Options to generate a CSR.
	cases := map[string]struct {
		csrOptions CertOptions
		err        error
	}{
		"GenCSR with RSA": {
			csrOptions: CertOptions{
				Host:       "test_ca.com",
				Org:        "MyOrg",
				RSAKeySize: 2048,
			},
		},
		"GenCSR with EC": {
			csrOptions: CertOptions{
				Host:     "test_ca.com",
				Org:      "MyOrg",
				ECSigAlg: EcdsaSigAlg,
			},
		},
		"GenCSR with EC errors due to invalid signature algorithm": {
			csrOptions: CertOptions{
				Host:     "test_ca.com",
				Org:      "MyOrg",
				ECSigAlg: "ED25519",
			},
			err: errors.New("csr cert generation fails due to unsupported EC signature algorithm"),
		},
	}

	for id, tc := range cases {
		csrPem, _, err := GenCSR(tc.csrOptions)
		if err != nil {
			if tc.err != nil {
				if err.Error() == tc.err.Error() {
					continue
				}
				t.Fatalf("%s: expected error to match expected error: %v", id, err)
			} else {
				t.Errorf("%s: failed to gen CSR", id)
			}
		}

		pemBlock, _ := pem.Decode(csrPem)
		if pemBlock == nil {
			t.Fatalf("%s: failed to decode csr", id)
		}
		csr, err := x509.ParseCertificateRequest(pemBlock.Bytes)
		if err != nil {
			t.Fatalf("%s: failed to parse csr", id)
		}
		if err = csr.CheckSignature(); err != nil {
			t.Errorf("%s: csr signature is invalid", id)
		}
		if csr.Subject.Organization[0] != "MyOrg" {
			t.Errorf("%s: csr subject does not match", id)
		}
		if !strings.HasSuffix(string(csr.Extensions[0].Value), "test_ca.com") {
			t.Errorf("%s: csr host does not match", id)
		}
		if tc.csrOptions.ECSigAlg != "" {
			if tc.csrOptions.ECSigAlg != EcdsaSigAlg {
				t.Errorf("%s: Only ECDSA signature algorithms are currently supported", id)
			}
			if reflect.TypeOf(csr.PublicKey) != reflect.TypeOf(&ecdsa.PublicKey{}) {
				t.Errorf("%s: decoded PKCS#8 returned unexpected key type: %T", id, csr.PublicKey)
			}
		} else if reflect.TypeOf(csr.PublicKey) != reflect.TypeOf(&rsa.PublicKey{}) {
			t.Errorf("%s: decoded PKCS#8 returned unexpected key type: %T", id, csr.PublicKey)
		}
	}
}

func TestGenCSRPKCS8Key(t *testing.T) {
	// Options to generate a CSR.
	cases := map[string]struct {
		csrOptions CertOptions
	}{
		"PKCS8Key with RSA": {
			csrOptions: CertOptions{
				Host:       "test_ca.com",
				Org:        "MyOrg",
				RSAKeySize: 2048,
				PKCS8Key:   true,
			},
		},
		"PKCS8Key with EC": {
			csrOptions: CertOptions{
				Host:     "test_ca.com",
				Org:      "MyOrg",
				ECSigAlg: EcdsaSigAlg,
				PKCS8Key: true,
			},
		},
	}

	for id, tc := range cases {
		csrPem, keyPem, err := GenCSR(tc.csrOptions)
		if err != nil {
			t.Errorf("%s: failed to gen CSR", id)
		}

		pemBlock, _ := pem.Decode(csrPem)
		if pemBlock == nil {
			t.Fatalf("%s: failed to decode csr", id)
		}
		csr, err := x509.ParseCertificateRequest(pemBlock.Bytes)
		if err != nil {
			t.Fatalf("%s: failed to parse csr", id)
		}
		if err = csr.CheckSignature(); err != nil {
			t.Errorf("%s: csr signature is invalid", id)
		}
		if csr.Subject.Organization[0] != "MyOrg" {
			t.Errorf("%s: csr subject does not match", id)
		}
		if !strings.HasSuffix(string(csr.Extensions[0].Value), "test_ca.com") {
			t.Errorf("%s: csr host does not match", id)
		}

		keyPemBlock, _ := pem.Decode(keyPem)
		if keyPemBlock == nil {
			t.Fatalf("%s: failed to decode private key PEM", id)
		}
		key, err := x509.ParsePKCS8PrivateKey(keyPemBlock.Bytes)
		if err != nil {
			t.Errorf("%s: failed to parse PKCS#8 private key", id)
		}
		if tc.csrOptions.ECSigAlg != "" {
			if tc.csrOptions.ECSigAlg != EcdsaSigAlg {
				t.Errorf("%s: Only ECDSA signature algorithms are currently supported", id)
			}
			if reflect.TypeOf(key) != reflect.TypeOf(&ecdsa.PrivateKey{}) {
				t.Errorf("%s: decoded PKCS#8 returned unexpected key type: %T", id, key)
			}
		} else if reflect.TypeOf(key) != reflect.TypeOf(&rsa.PrivateKey{}) {
			t.Errorf("%s: decoded PKCS#8 returned unexpected key type: %T", id, key)
		}
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

func TestGenCSRTemplateForDualUse(t *testing.T) {
	tt := map[string]struct {
		host       string
		expectedCN string
	}{
		"Single host": {
			host:       "bla.com",
			expectedCN: "bla.com",
		},
		"Multiple hosts": {
			host:       "a.org,b.net,c.groups",
			expectedCN: "a.org",
		},
	}

	for _, tc := range tt {
		opts := CertOptions{
			Host:       tc.host,
			Org:        "MyOrg",
			RSAKeySize: 512,
			IsDualUse:  true,
		}

		csr, err := GenCSRTemplate(opts)
		if err != nil {
			t.Error(err)
		}

		if csr.Subject.CommonName != tc.expectedCN {
			t.Errorf("unexpected value for 'CommonName' field: want %v but got %v", tc.expectedCN, csr.Subject.CommonName)
		}
	}
}
