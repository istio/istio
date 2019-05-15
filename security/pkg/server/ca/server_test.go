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
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"istio.io/istio/security/pkg/pki/ca"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	mockutil "istio.io/istio/security/pkg/pki/util/mock"
	"istio.io/istio/security/pkg/server/ca/authenticate"
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

const badSanCsr = `
MIICdzCCAV8CAQAwCzEJMAcGA1UEChMAMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAr8uTt9MSXAHugljyfxCS1BE3X0U5YQnN8Cgj1qn5cnu43LDdwA/x
Zgsd7ZfkuA+fpxBW2x4yR4LOSEwZAav6z45f9dxoZea0/wTPUHXam2tHIuhz1F1F
LlZX0EbZErBcjiPs6Y/FUaVROZZftOkq+sfNExTiXR7q5fAyYP/9L57OHOEx6RA3
kNEFBaa190j4ITvuS8fqsMT3lsRqLQ7fTCd5Ygw8rGZWOT6GSpLm1YJvSXhDUdxL
hYvoMoDgJ+SRpXWvG/YlzP6nMvJN45flTcIGXSMFvqaFGs5HhYxIviX8dE1Vso+/
1GV5MNPksTuGh/QqCjjcKvzZ6cMRuUeziQIDAQABoCcwJQYJKoZIhvcNAQkOMRgw
FjAUBgNVHREEDTALgglsb2NhbGhvc3QwDQYJKoZIhvcNAQELBQADggEBAEHIduLz
5oei9NHapvYsDDe6A+Q2nUm9uvWn/mMBujbstY9ZmLc73gWS0A8maXFFCjtMf7+n
u8naR7rmw0MjbVJPL2gbbqWjlNqvfm/upiYT2o8UtXyi0ZIQwfxL/iLqHZOVfm//
GGpTOohc7joR0EUnBa5piK3XXc4U5aCWMwlnmENMBAtNlRBuAzYJsMydv0Be72ga
gCojNs0xyJ77JA80HLY7iR4J6BRYsZQ/5UB/pYR55e4TGFDbI+C/6NBqLkzEfyX0
5KLq/6IJesVZLnKoxOt07OYriZS+U4b+Lx3++vWVnI8z2iOdGPUuJj7ys57zKJZ3
1sT/u25qExkefck=
-----END CERTIFICATE REQUEST-----`

type mockAuthenticator struct {
	authSource authenticate.AuthSource
	identities []string
	errMsg     string
}

func (authn *mockAuthenticator) Authenticate(ctx context.Context) (*authenticate.Caller, error) {
	if len(authn.errMsg) > 0 {
		return nil, fmt.Errorf("%v", authn.errMsg)
	}

	return &authenticate.Caller{
		AuthSource: authn.authSource,
		Identities: authn.identities,
	}, nil
}

type mockAuthorizer struct {
	errMsg string
}

// nolint: unparam
func (authz *mockAuthorizer) authorize(requester *authenticate.Caller, requestedIds []string) error {
	if len(authz.errMsg) > 0 {
		return fmt.Errorf("%v", authz.errMsg)
	}
	return nil
}

func TestCreateCertificate(t *testing.T) {
	testCases := map[string]struct {
		authenticators []authenticator
		authorizer     *mockAuthorizer
		ca             ca.CertificateAuthority
		certChain      []string
		code           codes.Code
	}{
		"No authenticator": {
			authenticators: nil,
			code:           codes.Unauthenticated,
			authorizer:     &mockAuthorizer{},
			ca:             &mockca.FakeCA{},
		},
		"Unauthenticated request": {
			authenticators: []authenticator{&mockAuthenticator{
				errMsg: "Not authorized",
			}},
			code:       codes.Unauthenticated,
			authorizer: &mockAuthorizer{},
			ca:         &mockca.FakeCA{},
		},
		"CA not ready": {
			authorizer:     &mockAuthorizer{},
			authenticators: []authenticator{&mockAuthenticator{}},
			ca:             &mockca.FakeCA{SignErr: ca.NewError(ca.CANotReady, fmt.Errorf("cannot sign"))},
			code:           codes.Internal,
		},
		"Invalid CSR": {
			authorizer:     &mockAuthorizer{},
			authenticators: []authenticator{&mockAuthenticator{}},
			ca:             &mockca.FakeCA{SignErr: ca.NewError(ca.CSRError, fmt.Errorf("cannot sign"))},
			code:           codes.InvalidArgument,
		},
		"Invalid TTL": {
			authorizer:     &mockAuthorizer{},
			authenticators: []authenticator{&mockAuthenticator{}},
			ca:             &mockca.FakeCA{SignErr: ca.NewError(ca.TTLError, fmt.Errorf("cannot sign"))},
			code:           codes.InvalidArgument,
		},
		"Failed to sign": {
			authorizer:     &mockAuthorizer{},
			authenticators: []authenticator{&mockAuthenticator{}},
			ca:             &mockca.FakeCA{SignErr: ca.NewError(ca.CertGenError, fmt.Errorf("cannot sign"))},
			code:           codes.Internal,
		},
		"Successful signing": {
			authenticators: []authenticator{&mockAuthenticator{}},
			authorizer:     &mockAuthorizer{},
			ca: &mockca.FakeCA{
				SignedCert: []byte("cert"),
				KeyCertBundle: &mockutil.FakeKeyCertBundle{
					CertChainBytes: []byte("cert_chain"),
					RootCertBytes:  []byte("root_cert"),
				},
			},
			certChain: []string{"cert", "cert_chain", "root_cert"},
			code:      codes.OK,
		},
	}

	for id, c := range testCases {
		server := &Server{
			ca:             c.ca,
			hostnames:      []string{"hostname"},
			port:           8080,
			authorizer:     c.authorizer,
			authenticators: c.authenticators,
			monitoring:     newMonitoringMetrics(),
		}
		request := &pb.IstioCertificateRequest{Csr: "dumb CSR"}

		response, err := server.CreateCertificate(context.Background(), request)
		s, _ := status.FromError(err)
		code := s.Code()
		if c.code != code {
			t.Errorf("Case %s: expecting code to be (%d) but got (%d): %s", id, c.code, code, s.Message())
		} else if c.code == codes.OK {
			if len(response.CertChain) != len(c.certChain) {
				t.Errorf("Case %s: expecting cert chain length to be (%d) but got (%d)",
					id, len(c.certChain), len(response.CertChain))
			}
			for i, v := range response.CertChain {
				if v != c.certChain[i] {
					t.Errorf("Case %s: expecting cert to be (%s) but got (%s) at position [%d] of cert chain.",
						id, c.certChain, v, i)
				}
			}

		}
	}
}

func TestHandleCSR(t *testing.T) {
	testCases := map[string]struct {
		authenticators []authenticator
		authorizer     *mockAuthorizer
		ca             *mockca.FakeCA
		csr            string
		cert           string
		certChain      string
		expectedIDs    []string
		code           codes.Code
	}{
		"No authenticator": {
			authenticators: nil,
			authorizer:     &mockAuthorizer{},
			ca:             &mockca.FakeCA{SignErr: ca.NewError(ca.CANotReady, fmt.Errorf("cannot sign"))},
			code:           codes.Unauthenticated,
		},
		"Unauthenticated request": {
			authenticators: []authenticator{&mockAuthenticator{
				errMsg: "Not authorized",
			}},
			authorizer: &mockAuthorizer{},
			ca:         &mockca.FakeCA{SignErr: ca.NewError(ca.CANotReady, fmt.Errorf("cannot sign"))},
			code:       codes.Unauthenticated,
		},
		"No caller authenticated": {
			authorizer:     &mockAuthorizer{},
			authenticators: []authenticator{&mockAuthenticator{}},
			code:           codes.Unauthenticated,
		},
		"Corrupted CSR": {
			authorizer:     &mockAuthorizer{},
			authenticators: []authenticator{&mockAuthenticator{identities: []string{"test"}}},
			csr:            "deadbeef",
			code:           codes.InvalidArgument,
		},
		"Invalid SAN CSR": {
			authorizer:     &mockAuthorizer{},
			authenticators: []authenticator{&mockAuthenticator{identities: []string{"test"}}},
			csr:            badSanCsr,
			code:           codes.InvalidArgument,
		},
		"Failed to sign": {
			authorizer:     &mockAuthorizer{},
			authenticators: []authenticator{&mockAuthenticator{identities: []string{"test"}}},
			ca:             &mockca.FakeCA{SignErr: ca.NewError(ca.CANotReady, fmt.Errorf("cannot sign"))},
			csr:            csr,
			code:           codes.Internal,
		},
		"Successful signing": {
			authenticators: []authenticator{&mockAuthenticator{identities: []string{"test"}}},
			authorizer:     &mockAuthorizer{},
			ca: &mockca.FakeCA{
				SignedCert:    []byte("generated cert"),
				KeyCertBundle: &mockutil.FakeKeyCertBundle{CertChainBytes: []byte("cert chain")},
			},
			csr:         csr,
			cert:        "generated cert",
			certChain:   "cert chain",
			expectedIDs: []string{"test"},
			code:        codes.OK,
		},
		"Multiple identities received by CA signer": {
			authenticators: []authenticator{&mockAuthenticator{identities: []string{"test1", "test2"}}},
			authorizer:     &mockAuthorizer{},
			ca: &mockca.FakeCA{
				SignedCert:    []byte("generated cert"),
				KeyCertBundle: &mockutil.FakeKeyCertBundle{CertChainBytes: []byte("cert chain")},
			},
			csr:         csr,
			cert:        "generated cert",
			certChain:   "cert chain",
			expectedIDs: []string{"test1", "test2"},
			code:        codes.OK,
		},
	}

	for id, c := range testCases {
		server := &Server{
			ca:             c.ca,
			hostnames:      []string{"hostname"},
			port:           8080,
			authorizer:     c.authorizer,
			authenticators: c.authenticators,
			monitoring:     newMonitoringMetrics(),
		}
		request := &pb.CsrRequest{CsrPem: []byte(c.csr)}

		response, err := server.HandleCSR(context.Background(), request)
		s, _ := status.FromError(err)
		code := s.Code()
		if c.code != code {
			t.Errorf("Case %s: expecting code to be (%d) but got (%d: %s)", id, c.code, code, s.Message())
		} else if c.code == codes.OK {
			if !bytes.Equal(response.SignedCert, []byte(c.cert)) {
				t.Errorf("Case %s: expecting cert to be (%s) but got (%s)", id, c.cert, response.SignedCert)
			}
			if !bytes.Equal(response.CertChain, []byte(c.certChain)) {
				t.Errorf("Case %s: expecting cert chain to be (%s) but got (%s)", id, c.certChain, response.CertChain)
			}
		}
		if c.expectedIDs != nil {
			receivedIDs := c.ca.ReceivedIDs
			if len(receivedIDs) != len(c.expectedIDs) {
				t.Errorf("Case %s: CA received different IDs (%v) than the callers (%v)",
					id, receivedIDs, c.expectedIDs)
			}
			for i, v := range receivedIDs {
				if v != c.expectedIDs[i] {
					t.Errorf("Case %s: CA received different IDs (%v) than the callers (%v)",
						id, receivedIDs, c.expectedIDs)
				}
			}
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

func TestRun(t *testing.T) {
	k8sEnv := false
	if _, err := os.Stat(caCertPath); !os.IsNotExist(err) {
		if _, err := os.Stat(jwtPath); !os.IsNotExist(err) {
			k8sEnv = true
		}
	}
	testCases := map[string]struct {
		ca                          *mockca.FakeCA
		hostname                    []string
		port                        int
		expectedErr                 string
		applyServerCertificateError string
		expectedAuthenticatorsLen   int
	}{
		"Invalid listening port number": {
			ca:          &mockca.FakeCA{SignedCert: []byte(csr)},
			hostname:    []string{"localhost"},
			port:        -1,
			expectedErr: "cannot listen on port -1 (error: listen tcp: address -1: invalid port)",
		},
		"CA sign error": {
			ca:                          &mockca.FakeCA{SignErr: ca.NewError(ca.CANotReady, fmt.Errorf("cannot sign"))},
			hostname:                    []string{"localhost"},
			port:                        0,
			expectedErr:                 "",
			expectedAuthenticatorsLen:   1, // 2 when ID token authenticators are enabled.
			applyServerCertificateError: "cannot sign",
		},
		"Bad signed cert": {
			ca:                        &mockca.FakeCA{SignedCert: []byte(csr)},
			hostname:                  []string{"localhost"},
			port:                      0,
			expectedErr:               "",
			expectedAuthenticatorsLen: 1, // 2 when ID token authenticators are enabled.
			applyServerCertificateError: "tls: failed to find \"CERTIFICATE\" PEM block in certificate " +
				"input after skipping PEM blocks of the following types: [CERTIFICATE REQUEST]",
		},
		"Multiple hostname": {
			ca:                        &mockca.FakeCA{SignedCert: []byte(csr)},
			hostname:                  []string{"localhost", "fancyhost"},
			port:                      0,
			expectedAuthenticatorsLen: 1, // 3 when ID token authenticators are enabled.
			applyServerCertificateError: "tls: failed to find \"CERTIFICATE\" PEM block in certificate " +
				"input after skipping PEM blocks of the following types: [CERTIFICATE REQUEST]",
		},
		"Empty hostnames": {
			ca:          &mockca.FakeCA{SignedCert: []byte(csr)},
			hostname:    []string{},
			expectedErr: "failed to create grpc server hostlist empty",
		},
	}

	for id, tc := range testCases {
		if k8sEnv {
			// K8s JWT authenticator is added in k8s env.
			tc.expectedAuthenticatorsLen++
		}
		server, err := New(tc.ca, time.Hour, false, tc.hostname, tc.port, "testdomain.com")
		if err == nil {
			err = server.Run()
		}
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

		if len(server.authenticators) != tc.expectedAuthenticatorsLen {
			t.Fatalf("%s: Unexpected Authenticators Length. Expected: %v Actual: %v",
				id, tc.expectedAuthenticatorsLen, len(server.authenticators))
		}

		if len(tc.hostname) != len(server.hostnames) {
			t.Errorf("%s: unmatched number of hosts in CA server configuration. %d (expected) vs %d", id, len(tc.hostname), len(server.hostnames))
		}
		for i, hostname := range tc.hostname {
			if hostname != server.hostnames[i] {
				t.Errorf("%s: unmatched hosts in CA server configuration. %v (expected) vs %v", id, tc.hostname, server.hostnames)
			}
		}

		_, err = server.applyServerCertificate()
		if len(tc.applyServerCertificateError) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.applyServerCertificateError {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), tc.applyServerCertificateError)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}
	}
}
