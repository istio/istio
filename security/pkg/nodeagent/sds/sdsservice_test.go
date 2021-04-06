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
package sds

import (
	"fmt"
	"net"
	"strings"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/util/uuid"

	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/xdstest"
	ca2 "istio.io/istio/pkg/security"
	"istio.io/istio/tests/util/leak"
	"istio.io/pkg/log"
)

var (
	fakeRootCert         = []byte{00}
	fakeCertificateChain = []byte{01}
	fakePrivateKey       = []byte{02}

	fakePushCertificateChain = []byte{03}
	fakePushPrivateKey       = []byte{04}
	pushSecret               = &ca2.SecretItem{
		CertificateChain: fakePushCertificateChain,
		PrivateKey:       fakePushPrivateKey,
		ResourceName:     testResourceName,
	}
	testResourceName = "default"
	rootResourceName = "ROOTCA"
)

func TestMain(m *testing.M) {
	leak.CheckMain(m)
}

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
	s.server.UpdateCallback(name)
}

type Expectation struct {
	ResourceName string
	CertChain    []byte
	Key          []byte
	RootCert     []byte
}

func (s *TestServer) Verify(resp *discovery.DiscoveryResponse, expectations ...Expectation) *discovery.DiscoveryResponse {
	s.t.Helper()
	if len(resp.Resources) != len(expectations) {
		s.t.Fatalf("expected %d resources, got %d", len(expectations), len(resp.Resources))
	}
	got := xdstest.ExtractTLSSecrets(s.t, resp.Resources)
	for _, e := range expectations {
		scrt := got[e.ResourceName]
		r := Expectation{
			ResourceName: e.ResourceName,
			Key:          scrt.GetTlsCertificate().GetPrivateKey().GetInlineBytes(),
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

	opts := &ca2.Options{
		WorkloadUDSPath: fmt.Sprintf("/tmp/workload_gotest%s.sock", string(uuid.NewUUID())),
	}
	server, err := NewServer(opts, st)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		server.Stop()
	})
	return &TestServer{
		t:       t,
		server:  server,
		store:   st,
		udsPath: opts.WorkloadUDSPath,
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
		resp := s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		c.ExpectNoResponse()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{
			ResourceNames: []string{testResourceName, rootResourceName},
			ResponseNonce: resp.Nonce,
		}), expectCert, expectRoot)
		c.ExpectNoResponse()
	})
	t.Run("multiplexed root first", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		c.ExpectNoResponse()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		c.ExpectNoResponse()
	})
	t.Run("multiplexed multiple single", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		c.ExpectNoResponse()
	})
	t.Run("parallel", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()
		root := s.Connect()

		s.Verify(cert.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		s.Verify(root.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		cert.ExpectNoResponse()
		root.ExpectNoResponse()
	})
	t.Run("unknown", func(t *testing.T) {
		s := setupSDS(t)
		s.UpdateSecret(testResourceName, nil)
		cert := s.Connect()

		// When we connect, we get get an error
		cert.Request(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}})
		if err := cert.ExpectError(); !strings.Contains(fmt.Sprint(err), "failed to generate secret") {
			t.Fatalf("didn't get expected error; got %v", err)
		}
		cert.Cleanup()

		s.UpdateSecret(testResourceName, pushSecret)
		// If the secret is added later, new connections will succeed
		cert = s.Connect()
		s.Verify(cert.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), Expectation{
			ResourceName: testResourceName,
			CertChain:    fakePushCertificateChain,
			Key:          fakePushPrivateKey,
		})
	})
	t.Run("update empty", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()

		s.Verify(cert.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)

		// Remove secret and trigger push. This simulates CA outage. We should get an error, which
		// would force the client to retry
		s.UpdateSecret(testResourceName, nil)
		if err := cert.ExpectError(); !strings.Contains(fmt.Sprint(err), "failed to generate secret") {
			t.Fatalf("didn't get expected error; got %v", err)
		}
	})
	t.Run("serial", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()
		s.Verify(cert.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		cert.ExpectNoResponse()

		root := s.Connect()
		s.Verify(root.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		root.ExpectNoResponse()
		cert.ExpectNoResponse()
	})
	t.Run("push cert", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()
		root := s.Connect()
		s.Verify(cert.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		s.Verify(root.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		cert.ExpectNoResponse()
		root.ExpectNoResponse()

		s.UpdateSecret(testResourceName, pushSecret)
		s.Verify(cert.ExpectResponse(), Expectation{
			ResourceName: testResourceName,
			CertChain:    fakePushCertificateChain,
			Key:          fakePushPrivateKey,
		})
		// No need to push a new root if just the cert changes
		root.ExpectNoResponse()
	})
	t.Run("reconnect", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		res := s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		// Close out the connection
		c.Cleanup()

		// Reconnect with the same resources
		c = s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{
			ResourceNames: []string{testResourceName},
			ResponseNonce: res.Nonce,
			VersionInfo:   res.VersionInfo,
		}), expectCert)
	})
	t.Run("concurrent reconnect", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		res := s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		// Reconnect with the same resources, without closing the original connection
		c = s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{
			ResourceNames: []string{testResourceName},
			ResponseNonce: res.Nonce,
			VersionInfo:   res.VersionInfo,
		}), expectCert)
	})
	t.Run("concurrent connections", func(t *testing.T) {
		s := setupSDS(t)
		s.Verify(s.Connect().RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		s.Verify(s.Connect().RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
	})
	t.Run("unsubscribe", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		res := c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}})
		s.Verify(res, expectCert)
		c.Request(&discovery.DiscoveryRequest{
			ResourceNames: nil,
			ResponseNonce: res.Nonce,
			VersionInfo:   res.VersionInfo,
		})
		c.ExpectNoResponse()
	})
	t.Run("nack", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		c.RequestResponseNack(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}})
		c.ExpectNoResponse()
	})
}

func setupConnection(socket string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithInsecure(), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}))

	conn, err := grpc.Dial(socket, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}
