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
	"net"
	"path"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test/env"
)

// Validates basic xds proxy flow by proxying one CDS requests end to end.
func TestXdsProxyBasicFlow(t *testing.T) {
	proxy := setupXdsProxy(t)
	f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	setDialOptions(proxy, f.Listener)
	conn := setupDownstreamConnection(t)
	downstream := stream(t, conn)
	sendDownstream(t, downstream)
}

func setupXdsProxy(t *testing.T) *XdsProxy {
	secOpts := &security.Options{
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
	ia := NewAgent(&proxyConfig,
		&AgentConfig{}, secOpts)
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
	t.Run("Envoy close and open stream", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.Listener)

		conn := setupDownstreamConnection(t)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)

		downstream.CloseSend()
		downstream = stream(t, conn)
		sendDownstream(t, downstream)
	})
	t.Run("Envoy opens multiple stream", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.Listener)

		conn := setupDownstreamConnection(t)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)
		downstream = stream(t, conn)
		sendDownstream(t, downstream)
	})
	t.Run("Envoy closes connection", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.Listener)

		conn := setupDownstreamConnection(t)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)
		conn.Close()
		conn = setupDownstreamConnection(t)
		downstream = stream(t, conn)
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
		conn := setupDownstreamConnection(t)
		downstream := stream(t, conn)
		sendDownstream(t, downstream)

		// Stop server, setup a new one. This simulates an Istiod pod being torn down
		grpcServer.Stop()
		listener, err = net.Listen("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
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

func sendDownstream(t *testing.T, downstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	t.Helper()
	err := downstream.Send(&discovery.DiscoveryRequest{
		TypeUrl: v3.ClusterType,
		Node: &core.Node{
			Id: "sidecar~0.0.0.0~debug~cluster.local",
		},
	})
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
}

func setupDownstreamConnection(t *testing.T) *grpc.ClientConn {
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithInsecure(), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", xdsUdsPath)
	}))

	conn, err := grpc.Dial(xdsUdsPath, opts...)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		conn.Close()
	})
	return conn
}
