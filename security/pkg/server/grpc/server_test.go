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
  "strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"golang.org/x/net/context"

	"istio.io/istio/security/pkg/pki/ca"
	pb "istio.io/istio/security/proto"
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
	err error
}

func (authn *mockAuthenticator) authenticate(ctx context.Context) (*caller, error) {
	if authn.err != nil {
		return nil, authn.err
	}
	return &caller{}, nil
}

type mockAuthorizer struct {
	err error
}

func (authz *mockAuthorizer) authorize(*caller, []string) error {
	return authz.err
}

func TestSign(t *testing.T) {
	testCases := map[string]struct {
		authnErrMsg string
		authzErrMsg string
		ca          ca.CertificateAuthority
		csr         string
		cert        string
		errCode        codes.Code
    errMsg      string
	}{
		"Unauthenticated request": {
			authnErrMsg: "authn error1",
			errCode:        codes.Unauthenticated,
      errMsg:       "request authenticate failure",
		},
		"Unauthorized request": {
			authnErrMsg: "",
			authzErrMsg: "authz error1",
			csr:         csr,
			errCode:        codes.PermissionDenied,
      errMsg:     "request is not authorized (authz error1)",
		},
		"Failed to sign": {
			authnErrMsg: "",
			authzErrMsg: "",
			ca:          &mockCA{errMsg: "sign error1"},
			csr:         csr,
			errCode:        codes.Internal,
      errMsg:      "CSR signing error (sign error1)",
		},
		"Successful signing": {
			authnErrMsg: "",
			authzErrMsg: "",
			ca:          &mockCA{cert: "generated cert1"},
			csr:         csr,
			cert:        "generated cert1",
			errCode:        codes.OK,
		},
	}

	for id, c := range testCases {
		var authnErr, authzErr error
		authnErr = nil
		authzErr = nil
		if len(c.authnErrMsg) > 0 {
			authnErr = fmt.Errorf(c.authnErrMsg)
		}
		if len(c.authzErrMsg) > 0 {
			authzErr = fmt.Errorf(c.authzErrMsg)
		}
		server := &Server{
			authenticators: []authenticator{&mockAuthenticator{authnErr}},
			authorizer:     &mockAuthorizer{authzErr},
			ca:             c.ca,
			hostname:       "hostname",
			port:           8080,
		}
		request := &pb.Request{CsrPem: []byte(c.csr)}

		response, err := server.HandleCSR(nil, request)
		if c.errCode != grpc.Code(err) {
      t.Errorf("Case %s: Error code mismatch: %d VS (expected) %d", id, grpc.Code(err), c.errCode)
		}
    if grpc.Code(err) != codes.OK {
      if strings.Compare(grpc.ErrorDesc(err), c.errMsg) != 0 {
        t.Errorf("Case %s: Error message mismatch: %s VS (expected) %s", id, grpc.ErrorDesc(err), c.errMsg)
      }
    } else if !bytes.Equal(response.SignedCertChain, []byte(c.cert)) {
      t.Errorf("Case %s: issued cert mismatch: %s VS (expected) %s", id, response.SignedCertChain, c.cert)
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
