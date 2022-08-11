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
package sds

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	cryptomb "github.com/envoyproxy/go-control-plane/contrib/envoy/extensions/private_key_providers/cryptomb/v3alpha"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/xdstest"
	ca2 "istio.io/istio/pkg/security"
	"istio.io/pkg/log"
)

var (
	fakeRootCert         = []byte{0o0}
	fakeCertificateChain = []byte{0o1}
	fakePrivateKey       = []byte{0o2}

	fakePushCertificateChain = []byte{0o3}
	fakePushPrivateKey       = []byte{0o4}
	pushSecret               = &ca2.SecretItem{
		CertificateChain: fakePushCertificateChain,
		PrivateKey:       fakePushPrivateKey,
		ResourceName:     testResourceName,
	}
	testResourceName           = "default"
	rootResourceName           = "ROOTCA"
	fakePrivateKeyProviderConf = &meshconfig.PrivateKeyProvider{
		Provider: &meshconfig.PrivateKeyProvider_Cryptomb{
			Cryptomb: &meshconfig.PrivateKeyProvider_CryptoMb{
				PollDelay: &durationpb.Duration{
					Seconds: 0,
					Nanos:   10000,
				},
			},
		},
	}
	usefakePrivateKeyProviderConf = false
)

type TestServer struct {
	t       *testing.T
	server  *Server
	udsPath string
	store   *ca2.DirectSecretManager
}

func (s *TestServer) Connect() *xds.AdsTest {
	conn, err := setupConnection(s.udsPath)
	if err != nil {
		s.t.Fatal(err)
	}
	return xds.NewSdsTest(s.t, conn)
}

func (s *TestServer) UpdateSecret(name string, secret *ca2.SecretItem) {
	s.t.Helper()
	s.store.Set(name, secret)
	s.server.OnSecretUpdate(name)
}

type Expectation struct {
	ResourceName string
	CertChain    []byte
	Key          []byte
	RootCert     []byte
}

func (s *TestServer) extractPrivateKeyProvider(provider *tlsv3.PrivateKeyProvider) []byte {
	var cmb cryptomb.CryptoMbPrivateKeyMethodConfig
	provider.GetTypedConfig().UnmarshalTo(&cmb)
	return cmb.GetPrivateKey().GetInlineBytes()
}

func (s *TestServer) Verify(resp *discovery.DiscoveryResponse, expectations ...Expectation) *discovery.DiscoveryResponse {
	s.t.Helper()
	if len(resp.Resources) != len(expectations) {
		s.t.Fatalf("expected %d resources, got %d", len(expectations), len(resp.Resources))
	}
	got := xdstest.ExtractTLSSecrets(s.t, resp.Resources)
	for _, e := range expectations {
		scrt := got[e.ResourceName]
		var expectationKey []byte
		if provider := scrt.GetTlsCertificate().GetPrivateKeyProvider(); provider != nil {
			expectationKey = s.extractPrivateKeyProvider(provider)
		} else {
			expectationKey = scrt.GetTlsCertificate().GetPrivateKey().GetInlineBytes()
		}
		r := Expectation{
			ResourceName: e.ResourceName,
			Key:          expectationKey,
			CertChain:    scrt.GetTlsCertificate().GetCertificateChain().GetInlineBytes(),
			RootCert:     scrt.GetValidationContext().GetTrustedCa().GetInlineBytes(),
		}
		if diff := cmp.Diff(e, r); diff != "" {
			s.t.Fatalf("got diff: %v", diff)
		}
	}
	return resp
}

func setupSDS(t *testing.T) *TestServer {
	var server *Server
	st := ca2.NewDirectSecretManager()
	st.Set(testResourceName, &ca2.SecretItem{
		CertificateChain: fakeCertificateChain,
		PrivateKey:       fakePrivateKey,
		ResourceName:     testResourceName,
	})
	st.Set(ca2.RootCertReqResourceName, &ca2.SecretItem{
		RootCert:     fakeRootCert,
		ResourceName: ca2.RootCertReqResourceName,
	})

	opts := &ca2.Options{}

	// SDS uses a hardcoded UDS path relative to current dir, so switch to a new one for the test.
	os.Chdir(t.TempDir())
	if usefakePrivateKeyProviderConf {
		server = NewServer(opts, st, fakePrivateKeyProviderConf)
	} else {
		server = NewServer(opts, st, nil)
	}
	t.Cleanup(func() {
		server.Stop()
	})
	return &TestServer{
		t:       t,
		server:  server,
		store:   st,
		udsPath: ca2.WorkloadIdentitySocketPath,
	}
}

func TestSDS(t *testing.T) {
	expectCert := Expectation{
		ResourceName: testResourceName,
		CertChain:    fakeCertificateChain,
		Key:          fakePrivateKey,
	}
	expectRoot := Expectation{
		ResourceName: rootResourceName,
		RootCert:     fakeRootCert,
	}
	log.FindScope("ads").SetOutputLevel(log.DebugLevel)
	t.Run("multiplexed", func(t *testing.T) {
		// In reality Envoy doesn't do this, but it *could* per XDS spec
		s := setupSDS(t)
		c := s.Connect()
		resp := s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		c.ExpectNoResponse(t)
		s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{
			ResourceNames: []string{testResourceName, rootResourceName},
			ResponseNonce: resp.Nonce,
		}), expectRoot)
		c.ExpectNoResponse(t)
	})
	t.Run("multiplexed root first", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		c.ExpectNoResponse(t)
		s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		c.ExpectNoResponse(t)
	})
	t.Run("multiplexed multiple single", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		c.ExpectNoResponse(t)
	})
	t.Run("parallel", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()
		root := s.Connect()

		s.Verify(cert.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		s.Verify(root.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		cert.ExpectNoResponse(t)
		root.ExpectNoResponse(t)
	})
	t.Run("unknown", func(t *testing.T) {
		s := setupSDS(t)
		s.UpdateSecret(testResourceName, nil)
		cert := s.Connect()

		// When we connect, we get get an error
		cert.Request(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}})
		if err := cert.ExpectError(t); !strings.Contains(fmt.Sprint(err), "failed to generate secret") {
			t.Fatalf("didn't get expected error; got %v", err)
		}
		cert.Cleanup()

		s.UpdateSecret(testResourceName, pushSecret)
		// If the secret is added later, new connections will succeed
		cert = s.Connect()
		s.Verify(cert.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), Expectation{
			ResourceName: testResourceName,
			CertChain:    fakePushCertificateChain,
			Key:          fakePushPrivateKey,
		})
	})
	t.Run("update empty", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()

		s.Verify(cert.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)

		// Remove secret and trigger push. This simulates CA outage. We should get an error, which
		// would force the client to retry
		s.UpdateSecret(testResourceName, nil)
		if err := cert.ExpectError(t); !strings.Contains(fmt.Sprint(err), "failed to generate secret") {
			t.Fatalf("didn't get expected error; got %v", err)
		}
	})
	t.Run("serial", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()
		s.Verify(cert.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		cert.ExpectNoResponse(t)

		root := s.Connect()
		s.Verify(root.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		root.ExpectNoResponse(t)
		cert.ExpectNoResponse(t)
	})
	t.Run("push cert", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()
		root := s.Connect()
		s.Verify(cert.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		s.Verify(root.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		cert.ExpectNoResponse(t)
		root.ExpectNoResponse(t)

		s.UpdateSecret(testResourceName, pushSecret)
		s.Verify(cert.ExpectResponse(t), Expectation{
			ResourceName: testResourceName,
			CertChain:    fakePushCertificateChain,
			Key:          fakePushPrivateKey,
		})
		// No need to push a new root if just the cert changes
		root.ExpectNoResponse(t)
	})
	t.Run("reconnect", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		res := s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		// Close out the connection
		c.Cleanup()

		// Reconnect with the same resources
		c = s.Connect()
		s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{
			ResourceNames: []string{testResourceName},
			ResponseNonce: res.Nonce,
			VersionInfo:   res.VersionInfo,
		}), expectCert)
	})
	t.Run("concurrent reconnect", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		res := s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		// Reconnect with the same resources, without closing the original connection
		c = s.Connect()
		s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{
			ResourceNames: []string{testResourceName},
			ResponseNonce: res.Nonce,
			VersionInfo:   res.VersionInfo,
		}), expectCert)
	})
	t.Run("concurrent connections", func(t *testing.T) {
		s := setupSDS(t)
		s.Verify(s.Connect().RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		s.Verify(s.Connect().RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
	})
	t.Run("unsubscribe", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		res := c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}})
		s.Verify(res, expectCert)
		c.Request(t, &discovery.DiscoveryRequest{
			ResourceNames: nil,
			ResponseNonce: res.Nonce,
			VersionInfo:   res.VersionInfo,
		})
		c.ExpectNoResponse(t)
	})
	t.Run("nack", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		c.RequestResponseNack(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}})
		c.ExpectNoResponse(t)
	})
	t.Run("connect_with_cryptomb", func(t *testing.T) {
		usefakePrivateKeyProviderConf = true
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		// Close out the connection
		c.Cleanup()
		usefakePrivateKeyProviderConf = false
	})
}

func setupConnection(socket string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}))

	conn, err := grpc.Dial(socket, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}
