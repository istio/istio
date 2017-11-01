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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"golang.org/x/net/context"

	"istio.io/istio/auth/pkg/pki/ca"
	pb "istio.io/istio/auth/proto"
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

func (ca *mockCA) Sign(csrPEM []byte) ([]byte, error) {
	if ca.errMsg != "" {
		return nil, fmt.Errorf(ca.errMsg)
	}
	return []byte(ca.cert), nil
}

func (ca *mockCA) GetRootCertificate() []byte {
	return nil
}

type mockAuthenticator struct {
	authenticated bool
}

func (authn *mockAuthenticator) authenticate(ctx context.Context) *user {
	if !authn.authenticated {
		return nil
	}
	return &user{}
}

type mockAuthorizer struct {
	authorized bool
}

func (authz *mockAuthorizer) authorize(*user, []string) bool {
	return authz.authorized
}

func TestSign(t *testing.T) {
	testCases := map[string]struct {
		authenticated bool
		authorized    bool
		ca            ca.CertificateAuthority
		csr           string
		cert          string
		code          codes.Code
	}{
		"Unauthenticated request": {
			authenticated: false,
			code:          codes.Unauthenticated,
		},
		"Unauthorized request": {
			authenticated: true,
			authorized:    false,
			csr:           csr,
			code:          codes.PermissionDenied,
		},
		"Failed to sign": {
			authenticated: true,
			authorized:    true,
			ca:            &mockCA{errMsg: "cannot sign"},
			csr:           csr,
			code:          codes.Internal,
		},
		"Successful signing": {
			authenticated: true,
			authorized:    true,
			ca:            &mockCA{cert: "generated cert"},
			csr:           csr,
			cert:          "generated cert",
			code:          codes.OK,
		},
	}

	for id, c := range testCases {
		server := &Server{
			authenticators: []authenticator{&mockAuthenticator{c.authenticated}},
			authorizer:     &mockAuthorizer{c.authorized},
			ca:             c.ca,
			hostname:       "hostname",
			port:           8080,
		}
		request := &pb.Request{CsrPem: []byte(c.csr)}

		response, err := server.HandleCSR(nil, request)
		if c.code != grpc.Code(err) {
			t.Errorf("Case %s: expecting code to be (%d) but got (%d)", id, c.code, grpc.Code(err))
		} else if c.code == codes.OK && !bytes.Equal(response.SignedCertChain, []byte(c.cert)) {
			t.Errorf("Case %s: expecting cert to be (%s) but got (%s)", id, c.cert, response.SignedCertChain)
		}
	}
}

func TestShouldRefresh(t *testing.T) {
	now := time.Now()
	testCases := map[string]struct {
		cert          *tls.Certificate
		shouldRefresh bool
	}{
		"No leaf cert": {
			cert:          &tls.Certificate{},
			shouldRefresh: true,
		},
		"Cert is expired": {
			cert: &tls.Certificate{
				Leaf: &x509.Certificate{NotAfter: now},
			},
			shouldRefresh: true,
		},
		"Cert is about to expire": {
			cert: &tls.Certificate{
				Leaf: &x509.Certificate{NotAfter: now.Add(5 * time.Second)},
			},
			shouldRefresh: true,
		},
		"Cert is valid": {
			cert: &tls.Certificate{
				Leaf: &x509.Certificate{NotAfter: now.Add(5 * time.Minute)},
			},
			shouldRefresh: false,
		},
	}

	for id, tc := range testCases {
		result := shouldRefresh(tc.cert)
		if tc.shouldRefresh != result {
			t.Errorf("%s: expected result is %t but got %t", id, tc.shouldRefresh, result)
		}
	}
}
