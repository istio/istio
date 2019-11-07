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
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

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
	if !strings.HasSuffix(string(csr.Extensions[0].Value), "test_ca.com") {
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
