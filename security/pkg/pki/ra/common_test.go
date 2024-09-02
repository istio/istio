// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package ra

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"net"
	"net/url"
	"testing"

	"istio.io/istio/security/pkg/pki/util"
)

func TestCompareCSRs(t *testing.T) {
	basicGenCSR, err := util.GenCSRTemplate(util.CertOptions{Host: testCsrHostName})
	if err != nil {
		t.Fatalf("Failed to generate basic CSR template: %v", err)
	}
	basicGenDualHostCSR, err := util.GenCSRTemplate(util.CertOptions{Host: testCsrHostName, IsDualUse: true})
	if err != nil {
		t.Fatalf("Failed to generate dual host CSR template: %v", err)
	}
	cases := []struct {
		name           string
		orgCSR         *x509.CertificateRequest
		genCSR         *x509.CertificateRequest
		expectedResult bool
	}{
		{
			name:           "orgCSR is nil",
			orgCSR:         nil,
			genCSR:         &x509.CertificateRequest{},
			expectedResult: false,
		},
		{
			name:           "genCSR is nil",
			orgCSR:         &x509.CertificateRequest{},
			genCSR:         nil,
			expectedResult: false,
		},
		{
			name:           "orgCSR CN is invalid (not empty)",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}, CommonName: "invalid"}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR CN set to host (valid with isDualHost is true)",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}, CommonName: testCsrHostName}},
			genCSR:         basicGenDualHostCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR CN empty string",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}, CommonName: ""}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR Org is unset",
			orgCSR:         &x509.CertificateRequest{},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR Org is set to empty string",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR Org is invalid (not empty or empt string)",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{"invalid"}}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR URI is empty",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, URIs: []*url.URL{}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR URI set to host",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, URIs: []*url.URL{{Host: testCsrHostName}}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR with multiple URIs",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, URIs: []*url.URL{{Host: testCsrHostName}, {Host: "another"}}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR with unset emailAddresses",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, EmailAddresses: []string{}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR with emailAddresses set to empty string",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, EmailAddresses: []string{""}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR with invalid emailAddresses",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, EmailAddresses: []string{"invalid", "another-invalid"}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR with invalid IPAddresses",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, IPAddresses: []net.IP{{1, 2, 3, 4}}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR with empty IPAddresses",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, IPAddresses: []net.IP{}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR with empty DNSNames",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, DNSNames: []string{}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR with empty string DNSNames",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, DNSNames: []string{""}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR with invalid DNSNames",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, DNSNames: []string{"invalid", "antoher-invalid"}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR with SANs extensions only",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, Extensions: []pkix.Extension{{Id: util.OidSubjectAlternativeName}}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name: "orgCSR with SANs and non SANs extensions",
			orgCSR: &x509.CertificateRequest{
				Subject:    pkix.Name{Organization: []string{""}},
				Extensions: []pkix.Extension{{Id: util.OidSubjectAlternativeName}, {Id: asn1.ObjectIdentifier{1, 2, 3}}},
			},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR with non SANs extensions",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, Extensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3}}}},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
		{
			name:           "orgCSR with empty extensions",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, Extensions: []pkix.Extension{}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name:           "orgCSR with empty extra extensions",
			orgCSR:         &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{""}}, ExtraExtensions: []pkix.Extension{}},
			genCSR:         basicGenCSR,
			expectedResult: true,
		},
		{
			name: "orgCSR with non empty extra extensions",
			orgCSR: &x509.CertificateRequest{
				Subject:         pkix.Name{Organization: []string{""}},
				ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3}}},
			},
			genCSR:         basicGenCSR,
			expectedResult: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := compareCSRs(tc.orgCSR, tc.genCSR)
			if result != tc.expectedResult {
				t.Errorf("Expected %t, but got %t", tc.expectedResult, result)
			}
		})
	}
}
