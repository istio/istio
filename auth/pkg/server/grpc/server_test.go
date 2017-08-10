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

package grpc

import (
	"bytes"
	"crypto/x509"
	"fmt"
	"testing"

	"istio.io/auth/pkg/pki/ca"
	pb "istio.io/auth/proto"
)

const csr = `
-----BEGIN CERTIFICATE REQUEST-----
MIIBoTCCAQoCAQAwEzERMA8GA1UEChMISnVqdSBvcmcwgZ8wDQYJKoZIhvcNAQEB
BQADgY0AMIGJAoGBANFf06eqiDx0+qD/xBAR5aMwwgaBOn6TPfSy96vOxLTsfkTg
ir/vb8UG+F5hO6yxF+z2BgzD8LwcbKnxahoPq/aWGLw3Umcqm4wxgWKHxvtYSQDG
w4zpmKOqgkagxbx32JXDlMpi6adUVHNvB838CiUys6IkVB0obGHnre8zmCLdAgMB
AAGgTjBMBgkqhkiG9w0BCQ4xPzA9MDsGA1UdEQQ0MDKGMHNwaWZmZTovL3Rlc3Qu
Y29tL25hbWVzcGFjZS9ucy9zZXJ2aWNlYWNjb3VudC9zYTANBgkqhkiG9w0BAQsF
AAOBgQCw9dL6xRQSjdYKt7exqlTJliuNEhw/xDVGlNUbDZnT0uL3zXI//Z8tsejn
8IFzrDtm0Z2j4BmBzNMvYBKL/4JPZ8DFywOyQqTYnGtHIkt41CNjGfqJRk8pIqVC
hKldzzeCKNgztEvsUKVqltFZ3ZYnkj/8/Cg8zUtTkOhHOjvuig==
-----END CERTIFICATE REQUEST-----`

type mockCA struct {
	cert   string
	errMsg string
}

func (ca *mockCA) Sign(csr *x509.CertificateRequest) ([]byte, error) {
	if ca.errMsg != "" {
		return nil, fmt.Errorf(ca.errMsg)
	}
	return []byte(ca.cert), nil
}

func (ca *mockCA) Generate(name, ns string) (chain, key []byte) {
	return nil, nil
}

func (ca *mockCA) GetRootCertificate() []byte {
	return nil
}

func TestSign(t *testing.T) {
	testCases := map[string]struct {
		ca     ca.CertificateAuthority
		csr    string
		cert   string
		errMsg string
	}{
		"Bad CSR string": {
			csr:    "bad CSR string",
			errMsg: "Certificate signing request is not properly encoded",
		},
		"Failed to sign": {
			ca:     &mockCA{errMsg: "cannot sign"},
			csr:    csr,
			errMsg: "cannot sign",
		},
		"Successful signing": {
			ca:   &mockCA{cert: "generated cert"},
			csr:  csr,
			cert: "generated cert",
		},
	}

	for id, c := range testCases {
		server := New(c.ca, 8080)
		request := &pb.Request{CsrPem: []byte(c.csr)}

		response, err := server.HandleCSR(nil, request)
		if c.errMsg != "" && c.errMsg != err.Error() {
			t.Errorf("Case %s: expecting error message (%s) but got (%s)", id, c.errMsg, err.Error())
		} else if c.errMsg == "" && !bytes.Equal(response.SignedCertChain, []byte(c.cert)) {
			t.Errorf("Case %s: expecting cert to be (%s) but got (%s)", id, c.cert, response.SignedCertChain)
		}
	}
}
