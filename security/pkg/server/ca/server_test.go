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

package ca

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/security"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/server/ca/authenticate"
)

type mockAuthenticator struct {
	authSource security.AuthSource
	identities []string
	errMsg     string
}

func (authn *mockAuthenticator) AuthenticatorType() string {
	return "mockAuthenticator"
}

func (authn *mockAuthenticator) Authenticate(_ security.AuthContext) (*security.Caller, error) {
	if len(authn.errMsg) > 0 {
		return nil, fmt.Errorf("%v", authn.errMsg)
	}

	return &security.Caller{
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
			SignedCert:    []byte("cert"),
			KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte("cert_chain"), []byte("root_cert")),
		},
		Authenticators: []security.Authenticator{auth},
		monitoring:     newMonitoringMetrics(),
	}
	mockCertChain := []string{"cert", "cert_chain", "root_cert"}
	mockIPAddr := &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)}
	testCerts := map[string]struct {
		certChain    [][]*x509.Certificate
		caller       *security.Caller
		fakeAuthInfo *mockAuthInfo
		code         codes.Code
		ipAddr       *net.IPAddr
	}{
		// no client certificate is presented
		"No client certificate": {
			certChain: nil,
			caller:    nil,
			ipAddr:    mockIPAddr,
			code:      codes.Unauthenticated,
		},
		// "unsupported auth type: not-tls"
		"Unsupported auth type": {
			certChain:    nil,
			caller:       nil,
			fakeAuthInfo: &mockAuthInfo{"not-tls"},
			ipAddr:       mockIPAddr,
			code:         codes.Unauthenticated,
		},
		// no cert chain presented
		"Empty cert chain": {
			certChain: [][]*x509.Certificate{},
			caller:    nil,
			ipAddr:    mockIPAddr,
			code:      codes.Unauthenticated,
		},
		// certificate misses the SAN field
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
		// successful testcase with valid client certificate
		"With client certificate": {
			certChain: [][]*x509.Certificate{
				{
					{
						Extensions: []pkix.Extension{*sanExt},
					},
				},
			},
			caller: &security.Caller{Identities: []string{callerID}},
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
		authenticators []security.Authenticator
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
			authenticators: []security.Authenticator{&mockAuthenticator{
				errMsg: "Not authorized",
			}},
			code: codes.Unauthenticated,
			ca:   &mockca.FakeCA{},
		},
		"CA not ready": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CANotReady, fmt.Errorf("cannot sign"))},
			code:           codes.Internal,
		},
		"Invalid CSR": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CSRError, fmt.Errorf("cannot sign"))},
			code:           codes.InvalidArgument,
		},
		"Invalid TTL": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.TTLError, fmt.Errorf("cannot sign"))},
			code:           codes.InvalidArgument,
		},
		"Failed to sign": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca:             &mockca.FakeCA{SignErr: caerror.NewError(caerror.CertGenError, fmt.Errorf("cannot sign"))},
			code:           codes.Internal,
		},
		"Successful signing": {
			authenticators: []security.Authenticator{&mockAuthenticator{identities: []string{"test-identity"}}},
			ca: &mockca.FakeCA{
				SignedCert:    []byte("cert"),
				KeyCertBundle: util.NewKeyCertBundleFromPem(nil, nil, []byte("cert_chain"), []byte("root_cert")),
			},
			certChain: []string{"cert", "cert_chain", "root_cert"},
			code:      codes.OK,
		},
	}

	for id, c := range testCases {
		server := &Server{
			ca:             c.ca,
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
