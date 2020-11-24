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
	"sync"
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"k8s.io/apimachinery/pkg/util/uuid"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/test/xdstest"
	ca2 "istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/cache"
)

const (
	// firstPartyJwt is generated in a testing K8s cluster. It is the default service account JWT.
	// No expiration time.
	FirstPartyJwt = "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9." +
		"eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9u" +
		"YW1lc3BhY2UiOiJmb28iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlY3JldC5uYW1lIjoiaHR0cGJp" +
		"bi10b2tlbi14cWRncCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUi" +
		"OiJodHRwYmluIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQudWlkIjoiOTlm" +
		"NjVmNTAtNzYwYS0xMWVhLTg5ZTctNDIwMTBhODAwMWMxIiwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmZv" +
		"bzpodHRwYmluIn0.4kIl9TRjXEw6DfhtR-LdpxsAlYjJgC6Ly1DY_rqYY4h0haxcXB3kYZ3b2He-3fqOBryz524W" +
		"KkZscZgvs5L-sApmvlqdUG61TMAl7josB0x4IMHm1NS995LNEaXiI4driffwfopvqc_z3lVKfbF9j-mBgnCepxz3" +
		"UyWo5irFa3qcwbOUB9kuuUNGBdtbFBN5yIYLpfa9E-MtTX_zJ9fQ9j2pi8Z4ljii0tEmPmRxokHkmG_xNJjUkxKU" +
		"WZf4bLDdCEjVFyshNae-FdxiUVyeyYorTYzwZZYQch9MJeedg4keKKUOvCCJUlKixd2qAe-H7r15RPmo4AU5O5YL" +
		"65xiNg"
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
		Version:          time.Now().Format("01-02 15:04:05.000"),
		Token:            fakeToken1,
	}
	fakeToken1        = "faketoken1"
	fakeToken2        = "faketoken2"
	emptyToken        = ""
	testResourceName  = "default"
	rootResourceName  = "ROOTCA"
	extraResourceName = "extra-resource-name"
)

type TestServer struct {
	t       *testing.T
	server  *Server
	udsPath string
	store   *mockSecretStore
}

func (s *TestServer) Connect() *xds.AdsTest {
	conn, err := setupConnection(s.udsPath)
	if err != nil {
		s.t.Fatal(err)
	}
	sdsClient := sds.NewSecretDiscoveryServiceClient(conn)
	header := metadata.Pairs("authorization", fakeToken1)
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	stream, err := sdsClient.StreamSecrets(ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	return xds.NewAdsTest(s.t, conn, stream).WithType(SecretTypeV3)
}

type Expectation struct {
	ResourceName string
	CertChain    []byte
	Key          []byte
	RootCert     []byte
}

func (s *TestServer) Verify(resp *discovery.DiscoveryResponse, expectations ...Expectation) {
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
}

func setupSDS(t *testing.T) *TestServer {
	// TODO setup to use JWT
	st := &mockSecretStore{
		checkToken:    false,
		//expectedToken: fakeToken1,
	}
	opts := &ca2.Options{
		WorkloadUDSPath:   fmt.Sprintf("/tmp/workload_gotest%s.sock", string(uuid.NewUUID())),
		EnableWorkloadSDS: true,
		RecycleInterval:   time.Second * 30,
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
	t.Run("simple", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		c.ExpectNoResponse()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}, ResponseNonce: "foo"}), expectRoot)
		c.ExpectNoResponse()
	})
	t.Run("root first", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		c.ExpectNoResponse()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		c.ExpectNoResponse()
	})
	t.Run("multiple single requests", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		c.ExpectNoResponse()
	})
	t.Run("multiple request", func(t *testing.T) {
		// This test is legal Envoy behavior, but in practice Envoy doesn't aggregate requests
		t.Skip()
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName, rootResourceName}}), expectCert, expectRoot)
		c.ExpectNoResponse()
	})
	t.Run("multiple parallel connections", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()
		root := s.Connect()

		s.Verify(cert.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		s.Verify(root.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		cert.ExpectNoResponse()
		root.ExpectNoResponse()
	})
	t.Run("multiple serial connections", func(t *testing.T) {
		s := setupSDS(t)
		cert := s.Connect()
		s.Verify(cert.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		cert.ExpectNoResponse()

		root := s.Connect()
		s.Verify(root.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{rootResourceName}}), expectRoot)
		root.ExpectNoResponse()
		cert.ExpectNoResponse()
	})
	t.Run("push", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		s.Verify(c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}}), expectCert)
		if err := s.server.UpdateCallback()(cache.ConnKey{ResourceName: testResourceName}, pushSecret); err != nil {
			t.Fatal(err)
		}
		s.Verify(c.ExpectResponse(), Expectation{
			ResourceName: testResourceName,
			CertChain:    fakePushCertificateChain,
			Key:          fakePushPrivateKey,
		})
	})
	t.Run("reconnect", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		res := c.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}})
		s.Verify(res, expectCert)
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
	t.Run("unknown resource", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		c.Request(&discovery.DiscoveryRequest{ResourceNames: []string{extraResourceName}})
		c.ExpectNoResponse()
	})
	t.Run("nack", func(t *testing.T) {
		s := setupSDS(t)
		c := s.Connect()
		c.RequestResponseNack(&discovery.DiscoveryRequest{ResourceNames: []string{testResourceName}})
		c.ExpectNoResponse()
	})
}

func (s *TestServer) GeneratePushSecret(conID, token string) *ca2.SecretItem {
	pushSecret := &ca2.SecretItem{
		CertificateChain: fakePushCertificateChain,
		PrivateKey:       fakePushPrivateKey,
		ResourceName:     testResourceName,
		Version:          time.Now().Format("01-02 15:04:05.000"),
		Token:            token,
	}
	s.store.secrets.Store(cache.ConnKey{ConnectionID: conID, ResourceName: testResourceName}, pushSecret)
	return pushSecret
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

type mockSecretStore struct {
	checkToken      bool
	secrets         sync.Map
	secretCacheHit  int
	secretCacheMiss int
	mutex           sync.RWMutex
	expectedToken   string
}

func (ms *mockSecretStore) SecretCacheHit() int {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()
	return ms.secretCacheHit
}

func (ms *mockSecretStore) SecretCacheMiss() int {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()
	return ms.secretCacheMiss
}

func (ms *mockSecretStore) GenerateSecret(ctx context.Context, conID, resourceName, token string) (*ca2.SecretItem, error) {
	if ms.checkToken && ms.expectedToken != token {
		return nil, fmt.Errorf("unexpected token %q", token)
	}

	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: resourceName,
	}
	if resourceName == testResourceName {
		s := &ca2.SecretItem{
			CertificateChain: fakeCertificateChain,
			PrivateKey:       fakePrivateKey,
			ResourceName:     testResourceName,
			Version:          time.Now().Format("01-02 15:04:05.000"),
			Token:            token,
		}
		log.Info("Store secret for key: ", key, ". token: ", token)
		ms.secrets.Store(key, s)
		return s, nil
	}

	if resourceName == cache.RootCertReqResourceName || strings.HasPrefix(resourceName, "file-root:") {
		s := &ca2.SecretItem{
			RootCert:     fakeRootCert,
			ResourceName: cache.RootCertReqResourceName,
			Version:      time.Now().Format("01-02 15:04:05.000"),
			Token:        token,
		}
		log.Info("Store root cert for key: ", key, ". token: ", token)
		ms.secrets.Store(key, s)
		return s, nil
	}

	return nil, fmt.Errorf("unexpected resourceName %q", resourceName)
}

func (ms *mockSecretStore) SecretExist(conID, spiffeID, token, version string) bool {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: spiffeID,
	}
	val, found := ms.secrets.Load(key)
	if !found {
		log.Infof("cannot find secret %v in cache", key)
		ms.secretCacheMiss++
		return false
	}
	cs := val.(*ca2.SecretItem)
	log.Infof("key is: %v. Token: %v", key, cs.Token)
	if spiffeID != cs.ResourceName {
		log.Infof("resource name not match: %s vs %s", spiffeID, cs.ResourceName)
		ms.secretCacheMiss++
		return false
	}
	if token != cs.Token {
		log.Infof("token does not match %+v vs %+v", token, cs.Token)
		ms.secretCacheMiss++
		return false
	}
	if version != cs.Version {
		log.Infof("version does not match %s vs %s", version, cs.Version)
		ms.secretCacheMiss++
		return false
	}
	log.Infof("requested secret matches cache")
	ms.secretCacheHit++
	return true
}

func (ms *mockSecretStore) DeleteSecret(conID, resourceName string) {
	key := cache.ConnKey{
		ConnectionID: conID,
		ResourceName: resourceName,
	}
	ms.secrets.Delete(key)
}

func (ms *mockSecretStore) ShouldWaitForGatewaySecret(connectionID, resourceName, token string, fileMountedCertsOnly bool) bool {
	return false
}
