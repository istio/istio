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
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/security/pkg/pki/util"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/security/pkg/pki/ca"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	caerror "istio.io/istio/security/pkg/pki/error"
	mockutil "istio.io/istio/security/pkg/pki/util/mock"
	"istio.io/istio/security/pkg/server/ca/authenticate"
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

type mockAuthenticator struct {
	authSource authenticate.AuthSource
	identities []string
	errMsg     string
}

func (authn *mockAuthenticator) AuthenticatorType() string {
	return "mockAuthenticator"
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

type mockAuthInfo struct {
	authType string
}

func (ai mockAuthInfo) AuthType() string {
	return ai.authType
}

/*This is a testing to send a request to the server using
the client cert authenticator instead of mock authenticator
*/
func TestCreateCertificateE2EUsingClientCertAuthenticator(t *testing.T) {
	callerID := "test.identity"
	ids := []util.Identity{
		{Type: util.TypeURI, Value: []byte(callerID)},
	}
	sanExt, err := util.BuildSANExtension(ids)
	if err != nil {
		t.Error(err)
	}
	auth := &authenticate.ClientCertAuthenticator{}

	server := &Server{
		ca: &mockca.FakeCA{
			SignedCert: []byte("cert"),
			KeyCertBundle: &mockutil.FakeKeyCertBundle{
				CertChainBytes: []byte("cert_chain"),
				RootCertBytes:  []byte("root_cert"),
			},
		},
		hostnames:      []string{"hostname"},
		port:           8080,
		Authenticators: []authenticate.Authenticator{auth},
		monitoring:     newMonitoringMetrics(),
	}
	mockCertChain := []string{"cert", "cert_chain", "root_cert"}
	mockIPAddr := &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)}
	testCerts := map[string]struct {
		certChain    [][]*x509.Certificate
		caller       *authenticate.Caller
		fakeAuthInfo *mockAuthInfo
		code         codes.Code
		ipAddr       *net.IPAddr
	}{
		//no client certificate is presented
		"No client certificate": {
			certChain: nil,
			caller:    nil,
			ipAddr:    mockIPAddr,
			code:      codes.Unauthenticated,
		},
		//"unsupported auth type: not-tls"
		"Unsupported auth type": {
			certChain:    nil,
			caller:       nil,
			fakeAuthInfo: &mockAuthInfo{"not-tls"},
			ipAddr:       mockIPAddr,
			code:         codes.Unauthenticated,
		},
		//no cert chain presented
		"Empty cert chain": {
			certChain: [][]*x509.Certificate{},
			caller:    nil,
			ipAddr:    mockIPAddr,
			code:      codes.Unauthenticated,
		},
		//certificate misses the the SAN field
		"Certificate has no SAN": {
			certChain: [][]*x509.Certificate{
				{
					{
						Version: 1,
					},
				},
			},
			ipAddr: mockIPAddr,
			code:   codes.Unauthenticated,
		},
		//successful testcase with valid client certificate
		"With client certificate": {
			certChain: [][]*x509.Certificate{
				{
					{
						Extensions: []pkix.Extension{*sanExt},
					},
				},
			},
			caller: &authenticate.Caller{Identities: []string{callerID}},
			ipAddr: mockIPAddr,
			code:   codes.OK,
		},
	}

	for id, c := range testCerts {
		request := &pb.IstioCertificateRequest{Csr: "dumb CSR"}
		ctx := context.Background()
		if c.certChain != nil {
			tlsInfo := credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: c.certChain},
			}
			p := &peer.Peer{Addr: c.ipAddr, AuthInfo: tlsInfo}
			ctx = peer.NewContext(ctx, p)
		}
		if c.fakeAuthInfo != nil {
			ctx = peer.NewContext(ctx, &peer.Peer{Addr: c.ipAddr, AuthInfo: c.fakeAuthInfo})
		}
		response, err := server.CreateCertificate(ctx, request)

		s, _ := status.FromError(err)
		code := s.Code()
		if code != c.code {
			t.Errorf("Case %s: expecting code to be (%d) but got (%d): %s", id, c.code, code, s.Message())
		} else if c.code == codes.OK {
			if len(response.CertChain) != len(mockCertChain) {
				t.Errorf("Case %s: expecting cert chain length to be (%d) but got (%d)",
					id, len(mockCertChain), len(response.CertChain))
			}
			for i, v := range response.CertChain {
				if v != mockCertChain[i] {
					t.Errorf("Case %s: expecting cert to be (%s) but got (%s) at position [%d] of cert chain.",
						id, mockCertChain, v, i)
				}
			}
		}
	}
}

func TestCreateCertificate(t *testing.T) {
	testCases := map[string]struct {
		authenticators []authenticate.Authenticator
		ca             CertificateAuthority
		certChain      []string
		code           codes.Code
	}{
		"No authenticator": {
			authenticators: nil,
			code:           codes.Unauthenticated,
			ca:             &mockca.FakeCA{},
		},
		"Unauthenticated request": {
			authenticators: []authenticate.Authenticator{&mockAuthenticator{
				errMsg: "Not authorized",
			}},
			code: codes.Unauthenticated,
			ca:   &mockca.FakeCA{},
		},
		"CA not ready": {
			authenticators: []authenticate.Authenticator{&mockAuthenticator{}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CANotReady, fmt.Errorf("cannot sign"))},
			code:           codes.Internal,
		},
		"Invalid CSR": {
			authenticators: []authenticate.Authenticator{&mockAuthenticator{}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CSRError, fmt.Errorf("cannot sign"))},
			code:           codes.InvalidArgument,
		},
		"Invalid TTL": {
			authenticators: []authenticate.Authenticator{&mockAuthenticator{}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.TTLError, fmt.Errorf("cannot sign"))},
			code:           codes.InvalidArgument,
		},
		"Failed to sign": {
			authenticators: []authenticate.Authenticator{&mockAuthenticator{}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CertGenError, fmt.Errorf("cannot sign"))},
			code:           codes.Internal,
		},
		"Successful signing": {
			authenticators: []authenticate.Authenticator{&mockAuthenticator{}},
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
			Authenticators: c.authenticators,
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
		ca                        *mockca.FakeCA
		hostname                  []string
		port                      int
		expectedErr               string
		getServerCertificateError string
		expectedAuthenticatorsLen int
	}{
		"Invalid listening port number": {
			ca:          &mockca.FakeCA{SignedCert: []byte(csr)},
			hostname:    []string{"localhost"},
			port:        -1,
			expectedErr: "cannot listen on port -1 (error: listen tcp: address -1: invalid port)",
		},
		"CA sign error": {
			ca:                        &mockca.FakeCA{SignErr: caerror.NewError(caerror.CANotReady, fmt.Errorf("cannot sign"))},
			hostname:                  []string{"localhost"},
			port:                      0,
			expectedErr:               "",
			expectedAuthenticatorsLen: 2, // 2 when ID token authenticators are enabled.
			getServerCertificateError: "cannot sign",
		},
		"Bad signed cert": {
			ca:                        &mockca.FakeCA{SignedCert: []byte(csr)},
			hostname:                  []string{"localhost"},
			port:                      0,
			expectedErr:               "",
			expectedAuthenticatorsLen: 2, // 2 when ID token authenticators are enabled.
			getServerCertificateError: "tls: failed to find \"CERTIFICATE\" PEM block in certificate " +
				"input after skipping PEM blocks of the following types: [CERTIFICATE REQUEST]",
		},
		"Multiple hostname": {
			ca:                        &mockca.FakeCA{SignedCert: []byte(csr)},
			hostname:                  []string{"localhost", "fancyhost"},
			port:                      0,
			expectedAuthenticatorsLen: 2, // 3 when ID token authenticators are enabled.
			getServerCertificateError: "tls: failed to find \"CERTIFICATE\" PEM block in certificate " +
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
		server, err := New(tc.ca, time.Hour, false, tc.hostname, tc.port, "testdomain.com", true,
			jwt.PolicyThirdParty, "kubernetes")
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

		if len(server.Authenticators) != tc.expectedAuthenticatorsLen {
			t.Fatalf("%s: Unexpected Authenticators Length. Expected: %v Actual: %v",
				id, tc.expectedAuthenticatorsLen, len(server.Authenticators))
		}

		if len(tc.hostname) != len(server.hostnames) {
			t.Errorf("%s: unmatched number of hosts in CA server configuration. %d (expected) vs %d", id, len(tc.hostname), len(server.hostnames))
		}
		for i, hostname := range tc.hostname {
			if hostname != server.hostnames[i] {
				t.Errorf("%s: unmatched hosts in CA server configuration. %v (expected) vs %v", id, tc.hostname, server.hostnames)
			}
		}

		_, err = server.getServerCertificate()
		if len(tc.getServerCertificateError) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.getServerCertificateError {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), tc.getServerCertificateError)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}
	}
}

func TestGetServerCertificate(t *testing.T) {
	cases := map[string]struct {
		rootCertFile    string
		certChainFile   string
		signingCertFile string
		signingKeyFile  string
	}{
		"RSA server cert": {
			rootCertFile:    "../../pki/testdata/multilevelpki/root-cert.pem",
			certChainFile:   "../../pki/testdata/multilevelpki/int2-cert-chain.pem",
			signingCertFile: "../../pki/testdata/multilevelpki/int2-cert.pem",
			signingKeyFile:  "../../pki/testdata/multilevelpki/int2-key.pem",
		},
		"ECC server cert": {
			rootCertFile:    "../../pki/testdata/multilevelpki/ecc-root-cert.pem",
			certChainFile:   "../../pki/testdata/multilevelpki/ecc-int2-cert-chain.pem",
			signingCertFile: "../../pki/testdata/multilevelpki/ecc-int2-cert.pem",
			signingKeyFile:  "../../pki/testdata/multilevelpki/ecc-int2-key.pem",
		},
	}

	defaultWorkloadCertTTL := 30 * time.Minute
	maxWorkloadCertTTL := time.Hour
	rsaKeySize := 2048

	for id, tc := range cases {
		caopts, err := ca.NewPluggedCertIstioCAOptions(tc.certChainFile, tc.signingCertFile, tc.signingKeyFile, tc.rootCertFile,
			defaultWorkloadCertTTL, maxWorkloadCertTTL, rsaKeySize)
		if err != nil {
			t.Fatalf("%s: Failed to create a plugged-cert CA Options: %v", id, err)
		}

		ca, err := ca.NewIstioCA(caopts)
		if err != nil {
			t.Errorf("%s: Got error while creating plugged-cert CA: %v", id, err)
		}
		if ca == nil {
			t.Fatalf("Failed to create a plugged-cert CA.")
		}

		server, err := New(ca, time.Hour, false, []string{"localhost"}, 0,
			"testdomain.com", true, jwt.PolicyThirdParty, "kubernetes")
		if err != nil {
			t.Errorf("%s: Cannot crete server: %v", id, err)
		}
		cert, err := server.getServerCertificate()
		if err != nil {
			t.Errorf("%s: getServerCertificate error: %v", id, err)
		}
		if len(cert.Certificate) != 4 {
			t.Errorf("Unexpected number of certificates returned: %d (expected 4)", len(cert.Certificate))
		}
	}
}
