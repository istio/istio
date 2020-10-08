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
	"testing"
	"time"

	gomega "github.com/onsi/gomega"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes/fake"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/spiffe"
	ca "istio.io/istio/security/pkg/pki/ca"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	customca "istio.io/istio/security/pkg/pki/custom"
	caerror "istio.io/istio/security/pkg/pki/error"
	"istio.io/istio/security/pkg/pki/signingapi/mock"
	"istio.io/istio/security/pkg/pki/util"
	mockutil "istio.io/istio/security/pkg/pki/util/mock"
	"istio.io/istio/security/pkg/server/ca/authenticate"
)

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

func TestGetServerCertificateWhenCustomCAIsSetted(t *testing.T) {
	cases := map[string]struct {
		rootCert     string
		serverCert   string
		serverKey    string
		clientCert   string
		clientKey    string
		workloadCert string
	}{
		"RSA cert": {
			rootCert:     "../../pki/signingapi/testdata/custom-certs/root-cert.pem",
			serverCert:   "../../pki/signingapi/testdata/custom-certs/server-cert.pem",
			serverKey:    "../../pki/signingapi/testdata/custom-certs/server-key.pem",
			clientCert:   "../../pki/signingapi/testdata/custom-certs/client-cert.pem",
			clientKey:    "../../pki/signingapi/testdata/custom-certs/client-key.pem",
			workloadCert: "../../pki/signingapi/testdata/custom-ecc-certs/workload-cert-chain.pem",
		},
		"ECC cert": {
			rootCert:     "../../pki/signingapi/testdata/custom-ecc-certs/root-cert.pem",
			serverCert:   "../../pki/signingapi/testdata/custom-ecc-certs/server-cert.pem",
			serverKey:    "../../pki/signingapi/testdata/custom-ecc-certs/server-key.pem",
			clientCert:   "../../pki/signingapi/testdata/custom-ecc-certs/client-cert.pem",
			clientKey:    "../../pki/signingapi/testdata/custom-ecc-certs/client-key.pem",
			workloadCert: "../../pki/signingapi/testdata/custom-ecc-certs/workload-cert-chain.pem",
		},
	}

	caCertTTL := time.Hour
	defaultCertTTL := 30 * time.Minute
	maxCertTTL := time.Hour
	org := "custom.ca.Org"
	const caNamespace = "default"
	client := fake.NewSimpleClientset()
	rootCertCheckInverval := time.Hour
	rsaKeySize := 2048

	for id, tc := range cases {
		t.Run(id, func(tsub *testing.T) {
			g := gomega.NewWithT(tsub)

			fakeServer, err := mock.NewFakeExternalCA(true, tc.serverCert, tc.serverKey, tc.rootCert, tc.workloadCert)
			g.Expect(err).To(gomega.BeNil())

			addr, err := fakeServer.Serve()
			g.Expect(err).To(gomega.BeNil())
			defer fakeServer.Stop()

			caopts, err := ca.NewSelfSignedIstioCAOptions(context.Background(),
				0, caCertTTL, rootCertCheckInverval, defaultCertTTL,
				maxCertTTL, org, false, caNamespace, -1, client.CoreV1(),
				tc.rootCert, false, rsaKeySize)

			g.Expect(err).ShouldNot(gomega.HaveOccurred(), "should NewSelfSignedIstio successful")

			istioCA, err := ca.NewIstioCA(caopts)
			g.Expect(err).To(gomega.BeNil())

			rootCertPool := x509.NewCertPool()
			ok := rootCertPool.AppendCertsFromPEM(caopts.KeyCertBundle.GetRootCertPem())
			g.Expect(ok).To(gomega.Equal(true))
			// Expect KeyCertBundle should contains 2 rootCAs: one from Custom RootCert, one self generate
			g.Expect(rootCertPool.Subjects()).To(gomega.HaveLen(2), "expect KeyCertBundle should contains 2 rootCAs")
			spiffeURI, err := spiffe.GenSpiffeURI("test-namespace", "test-service-account")
			g.Expect(err).ShouldNot(gomega.HaveOccurred())

			signerOpts := util.CertOptions{
				Host:       spiffeURI,
				NotBefore:  time.Now(),
				IsCA:       false,
				RSAKeySize: 2048,
			}

			fakeCSR, _, err := util.GenCSR(signerOpts)
			g.Expect(err).ShouldNot(gomega.HaveOccurred())
			server := &Server{
				ca: istioCA,
				Authenticators: []authenticate.Authenticator{&mockAuthenticator{
					identities: []string{spiffeURI},
				}},
				monitoring: newMonitoringMetrics(),
			}

			g.Expect(err).To(gomega.BeNil())
			customCAOpts := &mesh.MeshConfig_CA{
				Address: addr.String(),
				TlsSettings: &v1alpha3.ClientTLSSettings{
					Mode:              v1alpha3.ClientTLSSettings_MUTUAL,
					CaCertificates:    tc.rootCert,
					ClientCertificate: tc.clientCert,
					PrivateKey:        tc.clientKey,
				},
			}
			c, err := customca.NewCAClient(customCAOpts, caopts.KeyCertBundle)
			g.Expect(err).To(gomega.BeNil())
			server.CustomCAClient = c

			// -------------------
			// CREATE CERTIFICATES
			// -------------------
			certChain, err := server.CreateCertificate(context.TODO(), &pb.IstioCertificateRequest{
				Csr:              string(fakeCSR),
				ValidityDuration: 10000,
			})
			g.Expect(err).To(gomega.BeNil(), "should create certificate successful")
			g.Expect(certChain.GetCertChain()).To(gomega.HaveLen(2), "create certificate should response 2 certs")

			rootCerts := string(caopts.KeyCertBundle.GetRootCertPem())
			g.Expect(certChain.GetCertChain()[len(certChain.GetCertChain())-1]).To(gomega.Equal(rootCerts),
				"expect root-certs response should contains 2 rootCAs")
		})

	}
}
