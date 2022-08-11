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
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	wasmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	google_rpc "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	extensions "istio.io/api/extensions/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/envoy"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/retry"
)

// TestXdsLeak is a regression test for https://github.com/istio/istio/issues/34097
func TestXdsLeak(t *testing.T) {
	proxy := setupXdsProxyWithDownstreamOptions(t, []grpc.ServerOption{grpc.StreamInterceptor(xdstest.SlowServerInterceptor(time.Second, time.Second))})
	f := xdstest.NewMockServer(t)
	setDialOptions(proxy, f.Listener)
	proxy.istiodDialOptions = append(proxy.istiodDialOptions, grpc.WithStreamInterceptor(xdstest.SlowClientInterceptor(0, time.Second*10)))
	conn := setupDownstreamConnection(t, proxy)
	downstream := stream(t, conn)
	sendDownstreamWithoutResponse(t, downstream)
	for i := 0; i < 15; i++ {
		// Send a bunch of responses from Istiod. These should not block, even though there are more sends than responseChan can hold
		f.SendResponse(&discovery.DiscoveryResponse{TypeUrl: v3.ClusterType})
	}
	// Exit test, closing the connections. We should not have any goroutine leaks (checked by leak.CheckMain)
}

// sendDownstreamWithoutResponse sends a response without waiting for a response
func sendDownstreamWithoutResponse(t *testing.T, downstream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
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
}

// Validates basic xds proxy flow by proxying one CDS requests end to end.
func TestXdsProxyBasicFlow(t *testing.T) {
	proxy := setupXdsProxy(t)
	f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	setDialOptions(proxy, f.BufListener)
	conn := setupDownstreamConnection(t, proxy)
	downstream := stream(t, conn)
	sendDownstreamWithNode(t, downstream, model.NodeMetadata{
		Namespace:   "default",
		InstanceIPs: []string{"1.1.1.1"},
	})
}

// Validates the proxy health checking updates
func TestXdsProxyHealthCheck(t *testing.T) {
	healthy := &discovery.DiscoveryRequest{TypeUrl: v3.HealthInfoType}
	unhealthy := &discovery.DiscoveryRequest{
		TypeUrl: v3.HealthInfoType,
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
	setDialOptions(proxy, f.BufListener)
	conn := setupDownstreamConnection(t, proxy)
	downstream := stream(t, conn)

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

	// send cds before healthcheck, to make wle registered
	coreNode := &core.Node{
		Id:       "sidecar~1.1.1.1~debug~cluster.local",
		Metadata: node.ToStruct(),
	}
	err := downstream.Send(&discovery.DiscoveryRequest{TypeUrl: v3.ClusterType, Node: coreNode})
	if err != nil {
		t.Fatal(err)
	}
	_, err = downstream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	// healthcheck before lds will be not sent
	proxy.sendHealthCheckRequest(healthy)
	expectCondition("")

	// simulate envoy send xds requests
	sendDownstreamWithNode(t, downstream, node)

	// after lds sent, the caching healthcheck will be resent
	expectCondition(status.StatusTrue)

	// Flip status back and forth, ensure we update
	proxy.sendHealthCheckRequest(healthy)
	expectCondition(status.StatusTrue)
	proxy.sendHealthCheckRequest(unhealthy)
	expectCondition(status.StatusFalse)
	proxy.sendHealthCheckRequest(healthy)
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
	proxy.sendHealthCheckRequest(unhealthy)
	expectCondition(status.StatusFalse)

	// Send a new update while we are disconnected
	conn.Close()
	downstream.CloseSend()
	waitDisconnect()
	proxy.sendHealthCheckRequest(healthy)
	conn = setupDownstreamConnection(t, proxy)
	downstream = stream(t, conn)
	sendDownstreamWithNode(t, downstream, node)

	// When we reconnect, our status update should still persist
	expectCondition(status.StatusTrue)

	// Confirm more updates are honored
	proxy.sendHealthCheckRequest(healthy)
	expectCondition(status.StatusTrue)
	proxy.sendHealthCheckRequest(unhealthy)
	expectCondition(status.StatusFalse)

	// Disconnect and remove workload entry. This could happen if there is an outage and istiod cleans up
	// the workload entry.
	conn.Close()
	downstream.CloseSend()
	waitDisconnect()
	f.Store().Delete(gvk.WorkloadEntry, "group-1.1.1.1", "default", nil)
	proxy.sendHealthCheckRequest(healthy)
	conn = setupDownstreamConnection(t, proxy)
	downstream = stream(t, conn)
	sendDownstreamWithNode(t, downstream, node)

	// When we reconnect, our status update should be re-applied
	expectCondition(status.StatusTrue)

	// Confirm more updates are honored
	proxy.sendHealthCheckRequest(unhealthy)
	expectCondition(status.StatusFalse)
	proxy.sendHealthCheckRequest(healthy)
	expectCondition(status.StatusTrue)
}

func setupXdsProxy(t *testing.T) *XdsProxy {
	return setupXdsProxyWithDownstreamOptions(t, nil)
}

func setupXdsProxyWithDownstreamOptions(t *testing.T, opts []grpc.ServerOption) *XdsProxy {
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
	dir := t.TempDir()
	ia := NewAgent(proxyConfig, &AgentOptions{
		XdsUdsPath:            filepath.Join(dir, "XDS"),
		DownstreamGrpcOptions: opts,
	}, secOpts, envoy.ProxyConfig{TestOnly: true})
	t.Cleanup(func() {
		ia.close()
	})
	proxy, err := initXdsProxy(ia)
	if err != nil {
		t.Fatalf("Failed to initialize xds proxy %v", err)
	}
	ia.xdsProxy = proxy

	return proxy
}

func setDialOptions(p *XdsProxy, l *bufconn.Listener) {
	// Override istiodDialOptions so that the test can connect with plain text and with buffcon listener.
	p.istiodDialOptions = []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
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
		setDialOptions(proxy, f.BufListener)

		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})

		downstream.CloseSend()
		waitDisconnect(proxy)

		downstream = stream(t, conn)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})
	})
	t.Run("Envoy opens multiple stream", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.BufListener)

		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})
		downstream = stream(t, conn)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})
	})
	t.Run("Envoy closes connection", func(t *testing.T) {
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.BufListener)

		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})
		conn.Close()

		c := setupDownstreamConnection(t, proxy)
		downstream = stream(t, c)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})
	})
	t.Run("Envoy sends concurrent requests", func(t *testing.T) {
		// Envoy doesn't really do this, in reality it should only have a single connection. However,
		// this ensures we are robust against cases where envoy rapidly disconnects and reconnects
		proxy := setupXdsProxy(t)
		f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		setDialOptions(proxy, f.BufListener)

		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})

		c := setupDownstreamConnection(t, proxy)
		downstream = stream(t, c)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})
		conn.Close()

		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})
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
		proxy.istiodDialOptions = []grpc.DialOption{grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials())}

		// Setup gRPC server
		grpcServer := grpc.NewServer()
		t.Cleanup(grpcServer.Stop)
		f.Discovery.Register(grpcServer)
		go grpcServer.Serve(listener)

		// Send initial request
		conn := setupDownstreamConnection(t, proxy)
		downstream := stream(t, conn)
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})

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
		sendDownstreamWithNode(t, downstream, model.NodeMetadata{
			Namespace:   "default",
			InstanceIPs: []string{"1.1.1.1"},
		})
	})
}

type fakeAckCache struct{}

func (f *fakeAckCache) Get(string, string, string, string, time.Duration, []byte, extensions.PullPolicy) (string, error) {
	return "test", nil
}
func (f *fakeAckCache) Cleanup() {}

type fakeNackCache struct{}

func (f *fakeNackCache) Get(string, string, string, string, time.Duration, []byte, extensions.PullPolicy) (string, error) {
	return "", errors.New("errror")
}
func (f *fakeNackCache) Cleanup() {}

func TestECDSWasmConversion(t *testing.T) {
	node := model.NodeMetadata{
		Namespace:   "default",
		InstanceIPs: []string{"1.1.1.1"},
		ClusterID:   "Kubernetes",
	}
	proxy := setupXdsProxy(t)

	// Reset wasm cache to a fake ACK cache.
	proxy.wasmCache.Cleanup()
	proxy.wasmCache = &fakeAckCache{}

	// Initialize discovery server with an ECDS resource.
	ef, err := os.ReadFile(path.Join(env.IstioSrc, "pilot/pkg/xds/testdata/ecds.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	f := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: string(ef),
	})
	setDialOptions(proxy, f.BufListener)
	conn := setupDownstreamConnection(t, proxy)
	downstream := stream(t, conn)

	// Send first request, wasm module async fetch should work fine and
	// downstream should receive a local file based wasm extension config.
	err = downstream.Send(&discovery.DiscoveryRequest{
		TypeUrl:       v3.ExtensionConfigurationType,
		ResourceNames: []string{"extension-config"},
		Node: &core.Node{
			Id:       "sidecar~1.1.1.1~debug~cluster.local",
			Metadata: node.ToStruct(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the request received has been rewritten to local file.
	gotResp, err := downstream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if len(gotResp.Resources) != 1 {
		t.Errorf("xds proxy ecds wasm conversion number of received resource got %v want 1", len(gotResp.Resources))
	}
	gotEcdsConfig := &core.TypedExtensionConfig{}
	if err := gotResp.Resources[0].UnmarshalTo(gotEcdsConfig); err != nil {
		t.Fatalf("wasm config conversion output %v failed to unmarshal", gotResp.Resources[0])
	}
	wasm := &wasm.Wasm{
		Config: &wasmv3.PluginConfig{
			Vm: &wasmv3.PluginConfig_VmConfig{
				VmConfig: &wasmv3.VmConfig{
					Code: &core.AsyncDataSource{Specifier: &core.AsyncDataSource_Local{
						Local: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "test",
							},
						},
					}},
				},
			},
		},
	}
	wantEcdsConfig := &core.TypedExtensionConfig{
		Name:        "extension-config",
		TypedConfig: protoconv.MessageToAny(wasm),
	}

	if !proto.Equal(gotEcdsConfig, wantEcdsConfig) {
		t.Errorf("xds proxy wasm config conversion got %v want %v", gotEcdsConfig, wantEcdsConfig)
	}
	v1 := proxy.ecdsLastAckVersion
	n1 := proxy.ecdsLastNonce

	// reset wasm cache to a NACK cache, and recreate xds server as well to simulate a version bump
	proxy.wasmCache = &fakeNackCache{}
	f = xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: string(ef),
	})
	setDialOptions(proxy, f.BufListener)
	conn = setupDownstreamConnection(t, proxy)
	downstream = stream(t, conn)

	// send request again, this time a nack should be returned from the xds proxy due to NACK wasm cache.
	err = downstream.Send(&discovery.DiscoveryRequest{
		VersionInfo:   gotResp.VersionInfo,
		TypeUrl:       v3.ExtensionConfigurationType,
		ResourceNames: []string{"extension-config"},
		Node: &core.Node{
			Id:       "sidecar~1.1.1.1~debug~cluster.local",
			Metadata: node.ToStruct(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait until nonce was updated, which represents an ACK/NACK has been received.
	retry.UntilSuccessOrFail(t, func() error {
		if proxy.ecdsLastNonce.Load() == n1.Load() {
			return errors.New("last process nonce has not been updated. no ecds ack/nack is received yet")
		}
		return nil
	}, retry.Timeout(time.Second), retry.Delay(time.Millisecond))

	// Verify that the last ack version remains the same, which represents the latest DiscoveryRequest is a NACK.
	v2 := proxy.ecdsLastAckVersion
	if v1.Load() == v2.Load() {
		t.Errorf("last ack ecds request was updated. expect it to remain the same which represents a nack for ecds update")
	}
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

func setupDownstreamConnectionUDS(t test.Failer, path string) *grpc.ClientConn {
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
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
