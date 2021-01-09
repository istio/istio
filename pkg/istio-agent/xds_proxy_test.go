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

package istioagent

import (
	"context"
	"fmt"
	"net"
	"path"
	"path/filepath"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	google_rpc "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/retry"
)

// Validates basic xds proxy flow by proxying one CDS requests end to end.
func TestXdsProxyBasicFlow(t *testing.T) {
	proxy := setupXdsProxy(t)
	f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	setDialOptions(proxy, f.Listener)
	conn := setupDownstreamConnection(t, proxy)
	downstream := stream(t, conn)
	sendDownstream(t, downstream)
}

func init() {
	features.WorkloadEntryHealthChecks = true
	features.WorkloadEntryAutoRegistration = true
}

// Validates the proxy health checking updates
func TestXdsProxyHealthCheck(t *testing.T) {
	healthy := &discovery.DiscoveryRequest{TypeUrl: v3.HealthInfoType}
	unhealthy := &discovery.DiscoveryRequest{TypeUrl: v3.HealthInfoType,
		ErrorDetail: &google_rpc.Status{
			Code:    500,
			Message: "unhealthy",
		},
	}
	node := model.NodeMetadata{
		AutoRegisterGroup: "group",
		Namespace:         "default",
		InstanceIPs:       []string{"1.1.1.1"},
	}
	proxy := setupXdsProxy(t)

	f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	if _, err := f.Store().Create(config.Config{
		Meta: config.Meta{
			Name:             "group",
			Namespace:        "default",
			GroupVersionKind: gvk.WorkloadGroup,
		},
		Spec: &networking.WorkloadGroup{
			Template: &networking.WorkloadEntry{},
		},
	}); err != nil {
		t.Fatal(err)
	}
	setDialOptions(proxy, f.Listener)
	conn := setupDownstreamConnection(t, proxy)
	downstream := stream(t, conn)
	sendDownstreamWithNode(t, downstream, node)

	// Setup test helpers
	waitDisconnect := func() {
		retry.UntilSuccessOrFail(t, func() error {
			proxy.connectedMutex.Lock()
			defer proxy.connectedMutex.Unlock()
			if proxy.connected != nil {
				return fmt.Errorf("still connected")
			}
			return nil
		}, retry.Timeout(time.Second), retry.Delay(time.Millisecond))
	}
	expectCondition := func(expected string) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			cfg := f.Store().Get(gvk.WorkloadEntry, "group-1.1.1.1", "default")
			if cfg == nil {
				return fmt.Errorf("config not found")
			}
			con := status.GetConditionFromSpec(*cfg, status.ConditionHealthy)
			if con == nil {
				if expected == "" {
					return nil
				}
				return fmt.Errorf("found no conditions, expected %v", expected)
			}
			if con.Status != expected {
				return fmt.Errorf("expected status %q got %q", expected, con.Status)
			}
			return nil
		}, retry.Timeout(time.Second*2))
	}

	// On initial connect, status is unset.
	expectCondition("")

	// Flip status back and forth, ensure we update
	proxy.PersistRequest(healthy)
	expectCondition(status.StatusTrue)
	proxy.PersistRequest(unhealthy)
	expectCondition(status.StatusFalse)
	proxy.PersistRequest(healthy)
	expectCondition(status.StatusTrue)

	// Completely disconnect
	conn.Close()
	downstream.CloseSend()
	waitDisconnect()
	conn = setupDownstreamConnection(t, proxy)
	downstream = stream(t, conn)
	sendDownstreamWithNode(t, downstream, node)

	// Old status should remain
	expectCondition(status.StatusTrue)
	// And still update when we send new requests
	proxy.PersistRequest(unhealthy)
	expectCondition(status.StatusFalse)

	// Send a new update while we are disconnected
	conn.Close()
	downstream.CloseSend()
	waitDisconnect()
	proxy.PersistRequest(healthy)
	conn = setupDownstreamConnection(t, proxy)
	downstream = stream(t, conn)
	sendDownstreamWithNode(t, downstream, node)

	// When we reconnect, our status update should still persist
	expectCondition(status.StatusTrue)

	// Confirm more updates are honored
	proxy.PersistRequest(healthy)
	expectCondition(status.StatusTrue)
	proxy.PersistRequest(unhealthy)
	expectCondition(status.StatusFalse)

	// Disconnect and remove workload entry. This could happen if there is an outage and istiod cleans up
	// the workload entry.
	conn.Close()
	downstream.CloseSend()
	waitDisconnect()
	f.Store().Delete(gvk.WorkloadEntry, "group-1.1.1.1", "default", nil)
	proxy.PersistRequest(healthy)
	conn = setupDownstreamConnection(t, proxy)
	downstream = stream(t, conn)
	sendDownstreamWithNode(t, downstream, node)

	// When we reconnect, our status update should be re-applied
	expectCondition(status.StatusTrue)

	// Confirm more updates are honored
	proxy.PersistRequest(unhealthy)
	expectCondition(status.StatusFalse)
	proxy.PersistRequest(healthy)
	expectCondition(status.StatusTrue)
}

func setupXdsProxy(t *testing.T) *XdsProxy {
	secOpts := security.Options{
		FileMountedCerts: true,
	}
	proxyConfig := mesh.DefaultProxyConfig()
	proxyConfig.DiscoveryAddress = "buffcon"

	// While the actual test runs on plain text, these are setup to build the default dial options
	// with out these it looks for default cert location and fails.
	proxyConfig.ProxyMetadata = map[string]string{
		MetadataClientCertChain: path.Join(env.IstioSrc, "tests/testdata/certs/pilot/cert-chain.pem"),
		MetadataClientCertKey:   path.Join(env.IstioSrc, "tests/testdata/certs/pilot/key.pem"),
		MetadataClientRootCert:  path.Join(env.IstioSrc, "tests/testdata/certs/pilot/root-cert.pem"),
	}
	dir := t.TempDir()
	ia := NewAgent(&proxyConfig, &AgentConfig{
		XdsUdsPath: filepath.Join(dir, "XDS"),
	}, secOpts)
	t.Cleanup(func() {
		ia.Close()
	})
	proxy, err := initXdsProxy(ia)
	if err != nil {
		t.Fatalf("Failed to initialize xds proxy %v", err)
	}

	return proxy
}

func setDialOptions(p *XdsProxy, l *bufconn.Listener) {
	// Override istiodDialOptions so that the test can connect with plain text and with buffcon listener.
	p.istiodDialOptions = []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return l.Dial()
		}),
	}

}

var ctx = metadata.AppendToOutgoingContext(context.Background(), "ClusterID", "Kubernetes")

// Validates basic xds proxy flow by proxying one CDS requests end to end.
func TestXdsProxyReconnects(t *testing.T) {
	waitDisconnect := func(proxy *XdsProxy) {
		retry.UntilSuccessOrFail(t, func() error {
			proxy.connectedMutex.Lock()
			defer proxy.connectedMutex.Unlock()
			if proxy.connected != nil {
				return fmt.Errorf("still connected")
			}
			return nil
		}, retry.Timeout(time.Second), retry.Delay(time.Millisecond))
	}
	t.Run("Envoy close and open stream", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.Listener)

		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)

		downstream.CloseSend()
		waitDisconnect(proxy)

		downstream = stream(t, conn)
		sendDownstream(t, downstream)
	})
	t.Run("Envoy opens multiple stream", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.Listener)

		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)
		downstream = stream(t, conn)
		sendDownstream(t, downstream)
	})
	t.Run("Envoy closes connection", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.Listener)

		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)
		conn.Close()

		c := setupDownstreamConnection(t, proxy)
		downstream = stream(t, c)
		sendDownstream(t, downstream)
	})
	t.Run("Envoy sends concurrent requests", func(t *testing.T) {
		// Envoy doesn't really do this, in reality it should only have a single connection. However,
		// this ensures we are robust against cases where envoy rapidly disconnects and reconnects
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.Listener)

		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)

		c := setupDownstreamConnection(t, proxy)
		downstream = stream(t, c)
		sendDownstream(t, downstream)
		conn.Close()

		sendDownstream(t, downstream)
	})
	t.Run("Istiod closes connection", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})

		// Here we set up a real listener (instead of in memory) since we need to close and re-open
		// a new listener on the same port, which we cannot do with the in memory listener.
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatal(err)
		}
		proxy.istiodAddress = listener.Addr().String()
		proxy.istiodDialOptions = []grpc.DialOption{grpc.WithBlock(), grpc.WithInsecure()}

		// Setup gRPC server
		grpcServer := grpc.NewServer()
		t.Cleanup(grpcServer.Stop)
		f.Discovery.Register(grpcServer)
		go grpcServer.Serve(listener)

		// Send initial request
		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)

		// Stop server, setup a new one. This simulates an Istiod pod being torn down
		grpcServer.Stop()
		listener, err = net.Listen("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		waitDisconnect(proxy)

		grpcServer = grpc.NewServer()
		t.Cleanup(grpcServer.Stop)
		f.Discovery.Register(grpcServer)
		go grpcServer.Serve(listener)

		// Send downstream again
		downstream = stream(t, conn)
		sendDownstream(t, downstream)
	})
}

func stream(t *testing.T, conn *grpc.ClientConn) discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient {
	t.Helper()
	adsClient := discovery.NewAggregatedDiscoveryServiceClient(conn)
	downstream, err := adsClient.StreamAggregatedResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return downstream
}

func sendDownstreamWithNode(t *testing.T, downstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient, meta model.NodeMetadata) {
	t.Helper()
	node := &core.Node{
		Id:       "sidecar~1.1.1.1~debug~cluster.local",
		Metadata: meta.ToStruct(),
	}
	err := downstream.Send(&discovery.DiscoveryRequest{TypeUrl: v3.ClusterType, Node: node})
	if err != nil {
		t.Fatal(err)
	}
	res, err := downstream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.TypeUrl != v3.ClusterType {
		t.Fatalf("Expected to get cluster response but got %v", res)
	}
	err = downstream.Send(&discovery.DiscoveryRequest{TypeUrl: v3.ListenerType, Node: node})
	if err != nil {
		t.Fatal(err)
	}
	res, err = downstream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.TypeUrl != v3.ListenerType {
		t.Fatalf("Expected to get listener response but got %v", res)
	}
}

func sendDownstream(t *testing.T, downstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	t.Helper()
	sendDownstreamWithNode(t, downstream, model.NodeMetadata{
		Namespace:   "default",
		InstanceIPs: []string{"1.1.1.1"},
	})
}

func setupDownstreamConnectionUDS(t test.Failer, path string) *grpc.ClientConn {
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithInsecure(), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", path)
	}))

	conn, err := grpc.Dial(path, opts...)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		conn.Close()
	})
	return conn
}

func setupDownstreamConnection(t *testing.T, proxy *XdsProxy) *grpc.ClientConn {
	return setupDownstreamConnectionUDS(t, proxy.xdsUdsPath)
}
