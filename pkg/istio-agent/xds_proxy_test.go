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
	"strings"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacv3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	httprbac "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	wasmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	google_rpc "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	dnsClient "istio.io/istio/pkg/dns/client"
	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/envoy"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
	wasmcache "istio.io/istio/pkg/wasm"
)

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
	// TODO: allow fake XDS to be "authenticated"
	test.SetForTest(t, &features.ValidateWorkloadEntryIdentity, false)
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
	return setupXdsProxyWithAgentOptions(t, &AgentOptions{DownstreamGrpcOptions: opts}, nil)
}

// setupXdsProxyWithAgentOptions runs beforeInit, if set, on the agent before the proxy is initialized.
func setupXdsProxyWithAgentOptions(t *testing.T, agentOpts *AgentOptions, beforeInit func(*Agent)) *XdsProxy {
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
	agentOpts.XdsUdsPath = filepath.Join(dir, "XDS")
	ia := NewAgent(proxyConfig, agentOpts, secOpts, envoy.ProxyConfig{TestOnly: true})
	t.Cleanup(func() {
		ia.Close()
	})
	if beforeInit != nil {
		beforeInit(ia)
	}
	proxy, err := initXdsProxy(ia)
	if err != nil {
		t.Fatalf("Failed to initialize xds proxy %v", err)
	}
	ia.xdsProxy = proxy

	return proxy
}

func setDialOptions(p *XdsProxy, l *bufconn.Listener) {
	// Override dialOptions so that the test can connect with plain text and with buffcon listener.
	p.dialOptions = []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return l.Dial()
		}),
	}
}

var ctx = metadata.AppendToOutgoingContext(context.Background(), "ClusterID", constants.DefaultClusterName)

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
		proxy.dialOptions = []grpc.DialOption{grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials())}

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

func (f *fakeAckCache) Get(string, wasmcache.GetOptions) (string, error) {
	return "test", nil
}
func (f *fakeAckCache) Cleanup() {}

type fakeNackCache struct{}

func (f *fakeNackCache) Get(string, wasmcache.GetOptions) (string, error) {
	return "", errors.New("error")
}
func (f *fakeNackCache) Cleanup() {}

func TestECDSWasmConversion(t *testing.T) {
	node := model.NodeMetadata{
		Namespace:   "default",
		InstanceIPs: []string{"1.1.1.1"},
		ClusterID:   constants.DefaultClusterName,
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

	gotResp, err = downstream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if len(gotResp.Resources) != 1 {
		t.Errorf("xds proxy ecds wasm conversion number of received resource got %v want 1", len(gotResp.Resources))
	}
	if err := gotResp.Resources[0].UnmarshalTo(gotEcdsConfig); err != nil {
		t.Fatalf("wasm config conversion output %v failed to unmarshal", gotResp.Resources[0])
	}
	httpDenyAll := &httprbac.RBAC{
		Rules:           &rbacv3.RBAC{},
		RulesStatPrefix: wasmcache.DefaultDenyStatPrefix,
	}
	wantEcdsConfig = &core.TypedExtensionConfig{
		Name:        "extension-config",
		TypedConfig: protoconv.MessageToAny(httpDenyAll),
	}
	if !proto.Equal(gotEcdsConfig, wantEcdsConfig) {
		t.Errorf("xds proxy wasm config conversion got %v want %v", gotEcdsConfig, wantEcdsConfig)
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

func TestNDSDeltaHandler(t *testing.T) {
	info := func(ip, registry string) *dnsProto.NameTable_NameInfo {
		return &dnsProto.NameTable_NameInfo{Ips: []string{ip}, Registry: registry}
	}
	newHandler := func() (*ndsDeltaHandler, *dnsClient.LocalDNSServer) {
		dnsServer := &dnsClient.LocalDNSServer{}
		return &ndsDeltaHandler{dnsServer: dnsServer, resources: sets.New[string]()}, dnsServer
	}
	handle := func(t *testing.T, h *ndsDeltaHandler, resources []*discovery.Resource, removed []string) {
		t.Helper()
		if _, err := h.Handle(nil, resources, removed); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("applies additions, updates and removals", func(t *testing.T) {
		h, dnsServer := newHandler()
		handle(t, h, []*discovery.Resource{ndsHost("a.default.svc.cluster.local", "10.0.0.1"), ndsHost("b.default.svc.cluster.local", "10.0.0.2")}, nil)
		handle(t, h, []*discovery.Resource{ndsHost("b.default.svc.cluster.local", "10.0.0.3")}, []string{"a.default.svc.cluster.local"})
		assert.Equal(t, dnsIPs(dnsServer), map[string][]string{"b.default.svc.cluster.local": {"10.0.0.3"}})
	})

	t.Run("replacing a headless resource drops its removed pods", func(t *testing.T) {
		h, dnsServer := newHandler()
		headless := "mysql.db.svc.cluster.local"
		handle(t, h, []*discovery.Resource{ndsTable(headless, map[string]*dnsProto.NameTable_NameInfo{
			headless:                             {Ips: []string{"10.0.0.1", "10.0.0.2"}, Registry: "Kubernetes"},
			"mysql-0.mysql.db.svc.cluster.local": info("10.0.0.1", "Kubernetes"),
			"mysql-1.mysql.db.svc.cluster.local": info("10.0.0.2", "Kubernetes"),
		})}, nil)
		handle(t, h, []*discovery.Resource{ndsTable(headless, map[string]*dnsProto.NameTable_NameInfo{
			headless:                             info("10.0.0.1", "Kubernetes"),
			"mysql-0.mysql.db.svc.cluster.local": info("10.0.0.1", "Kubernetes"),
		})}, nil)
		assert.Equal(t, dnsIPs(dnsServer), map[string][]string{
			headless:                             {"10.0.0.1"},
			"mysql-0.mysql.db.svc.cluster.local": {"10.0.0.1"},
		})
	})

	t.Run("exact names win over aliases", func(t *testing.T) {
		h, dnsServer := newHandler()
		svc := "reviews.default.svc.cluster.local"
		kube := info("10.0.0.1", "Kubernetes")
		kube.Aliases = []string{"reviews", "reviews.default", "reviews.default.svc"}
		handle(t, h, []*discovery.Resource{
			ndsTable(svc, map[string]*dnsProto.NameTable_NameInfo{svc: kube}),
			ndsHost("reviews", "192.0.2.1"),
		}, nil)
		got := dnsIPs(dnsServer)
		assert.Equal(t, got["reviews"], []string{"192.0.2.1"})
		assert.Equal(t, got["reviews.default"], []string{"10.0.0.1"})

		// Removing the exact owner reveals the alias it shadowed.
		handle(t, h, nil, []string{"reviews"})
		assert.Equal(t, dnsIPs(dnsServer)["reviews"], []string{"10.0.0.1"})
	})

	t.Run("prefers Kubernetes entries, then resource name order", func(t *testing.T) {
		h, dnsServer := newHandler()
		pod := "x.headless.ns.svc.cluster.local"
		handle(t, h, []*discovery.Resource{
			ndsTable(pod, map[string]*dnsProto.NameTable_NameInfo{pod: info("192.0.2.1", "External")}),
			ndsTable("headless.ns.svc.cluster.local", map[string]*dnsProto.NameTable_NameInfo{pod: info("10.0.0.1", "Kubernetes")}),
			ndsTable("a.example.com", map[string]*dnsProto.NameTable_NameInfo{"shared.example.com": info("192.0.2.2", "External")}),
			ndsTable("b.example.com", map[string]*dnsProto.NameTable_NameInfo{"shared.example.com": info("192.0.2.3", "External")}),
		}, nil)
		got := dnsIPs(dnsServer)
		assert.Equal(t, got[pod], []string{"10.0.0.1"})
		assert.Equal(t, got["shared.example.com"], []string{"192.0.2.2"})

		// Removing the headless service exposes the ServiceEntry it shadowed.
		handle(t, h, nil, []string{"headless.ns.svc.cluster.local"})
		assert.Equal(t, dnsIPs(dnsServer)[pod], []string{"192.0.2.1"})
	})

	t.Run("legacy unnamed table replaces all resources", func(t *testing.T) {
		h, dnsServer := newHandler()
		handle(t, h, []*discovery.Resource{ndsHost("a.default.svc.cluster.local", "10.0.0.1")}, nil)
		handle(t, h, []*discovery.Resource{ndsTable("", map[string]*dnsProto.NameTable_NameInfo{
			"b.default.svc.cluster.local": info("10.0.0.2", "Kubernetes"),
		})}, []string{"a.default.svc.cluster.local"})
		assert.Equal(t, dnsIPs(dnsServer), map[string][]string{"b.default.svc.cluster.local": {"10.0.0.2"}})
		assert.Equal(t, len(h.initialResourceVersions(nil)), 0)
		if _, resolved := dnsServer.NameTableSnapshot(); resolved {
			t.Fatal("legacy table must replace the Delta NDS index")
		}

		// Later resources start from an empty index, so names from before the legacy table do not return.
		handle(t, h, []*discovery.Resource{ndsHost("c.default.svc.cluster.local", "10.0.0.3")}, nil)
		assert.Equal(t, dnsIPs(dnsServer), map[string][]string{"c.default.svc.cluster.local": {"10.0.0.3"}})
	})

	t.Run("partially rejected response applies valid resources and removals", func(t *testing.T) {
		h, dnsServer := newHandler()
		handle(t, h, []*discovery.Resource{
			ndsHost("a.default.svc.cluster.local", "10.0.0.1"),
			ndsHost("b.default.svc.cluster.local", "10.0.0.2"),
		}, nil)
		malformed := func(hostname string) *discovery.Resource {
			return &discovery.Resource{Name: hostname, Resource: protoconv.MessageToAny(&discovery.Resource{})}
		}
		resources := []*discovery.Resource{
			ndsHost("c.default.svc.cluster.local", "10.0.0.3"),
			malformed("b.default.svc.cluster.local"),
			malformed("d.default.svc.cluster.local"),
			{Name: "e.default.svc.cluster.local"},
		}
		_, err := h.Handle(nil, resources, []string{"a.default.svc.cluster.local"})
		if err == nil {
			t.Fatal("expected malformed resources to be rejected")
		}
		for _, hostname := range []string{"b.default.svc.cluster.local", "d.default.svc.cluster.local", "e.default.svc.cluster.local"} {
			if !strings.Contains(err.Error(), hostname) {
				t.Fatalf("NACK %q does not name rejected resource %s", err, hostname)
			}
		}
		// The removal and valid addition apply, and b keeps its previously accepted table.
		assert.Equal(t, dnsIPs(dnsServer), map[string][]string{
			"b.default.svc.cluster.local": {"10.0.0.2"},
			"c.default.svc.cluster.local": {"10.0.0.3"},
		})
		// Rejected additions are not reported as held on reconnect, so istiod sends them again.
		assert.Equal(t, h.initialResourceVersions(nil), map[string]string{"b.default.svc.cluster.local": "", "c.default.svc.cluster.local": ""})

		handle(t, h, []*discovery.Resource{ndsHost("d.default.svc.cluster.local", "10.0.0.4")}, nil)
		assert.Equal(t, dnsIPs(dnsServer)["d.default.svc.cluster.local"], []string{"10.0.0.4"})
	})

	t.Run("initial resource versions list accepted hostnames", func(t *testing.T) {
		h, _ := newHandler()
		handle(t, h, []*discovery.Resource{ndsHost("a.default.svc.cluster.local", "10.0.0.1"), ndsHost("b.default.svc.cluster.local", "10.0.0.2")}, nil)
		assert.Equal(t, h.initialResourceVersions(nil), map[string]string{"a.default.svc.cluster.local": "", "b.default.svc.cluster.local": ""})
	})

	t.Run("retired stream cannot publish after reconnect", func(t *testing.T) {
		h, dnsServer := newHandler()
		oldConnection := &ProxyConnection{}
		newConnection := &ProxyConnection{}
		h.initialResourceVersions(oldConnection)
		oldResources := []*discovery.Resource{ndsHost("old.default.svc.cluster.local", "10.0.0.1")}
		if active, err := h.Handle(oldConnection, oldResources, nil); err != nil || !active {
			t.Fatalf("initial response failed: active=%v err=%v", active, err)
		}

		assert.Equal(t, h.initialResourceVersions(newConnection), map[string]string{"old.default.svc.cluster.local": ""})
		newResources := []*discovery.Resource{ndsHost("new.default.svc.cluster.local", "10.0.0.2")}
		if active, err := h.Handle(newConnection, newResources, []string{"old.default.svc.cluster.local"}); err != nil || !active {
			t.Fatalf("new response failed: active=%v err=%v", active, err)
		}
		if active, err := h.Handle(oldConnection, oldResources, nil); err != nil || active {
			t.Fatalf("retired response was not discarded: active=%v err=%v", active, err)
		}
		assert.Equal(t, dnsIPs(dnsServer), map[string][]string{"new.default.svc.cluster.local": {"10.0.0.2"}})
	})
}

// fakeDeltaNDSServer is a fake istiod that records every Delta ADS stream opened by the proxy.
type fakeDeltaNDSServer struct {
	discovery.UnimplementedAggregatedDiscoveryServiceServer
	streams chan *fakeDeltaNDSStream
}

type fakeDeltaNDSStream struct {
	srv  discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer
	reqs chan *discovery.DeltaDiscoveryRequest
	done chan struct{}
}

func (f *fakeDeltaNDSServer) DeltaAggregatedResources(
	srv discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer,
) error {
	s := &fakeDeltaNDSStream{
		srv:  srv,
		reqs: make(chan *discovery.DeltaDiscoveryRequest, 100),
		done: make(chan struct{}),
	}
	f.streams <- s
	recvErr := make(chan error, 1)
	go func() {
		for {
			req, err := srv.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			s.reqs <- req
		}
	}()
	select {
	case <-s.done:
		return nil
	case err := <-recvErr:
		return err
	}
}

// request returns the next request of the given type, skipping other types.
func (s *fakeDeltaNDSStream) request(t *testing.T, typeURL string) *discovery.DeltaDiscoveryRequest {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case req := <-s.reqs:
			if req.TypeUrl == typeURL {
				return req
			}
		case <-timeout:
			t.Fatalf("timed out waiting for a %s request", typeURL)
		}
	}
}

// sendNDS sends a response and returns the error detail of the proxy's reply to it, which is nil for an ACK.
func (s *fakeDeltaNDSStream) sendNDS(t *testing.T, nonce string, resources []*discovery.Resource,
	removed ...string,
) *google_rpc.Status {
	t.Helper()
	err := s.srv.Send(&discovery.DeltaDiscoveryResponse{
		TypeUrl:          v3.NameTableType,
		Nonce:            nonce,
		Resources:        resources,
		RemovedResources: removed,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := s.request(t, v3.NameTableType)
	assert.Equal(t, req.ResponseNonce, nonce)
	return req.ErrorDetail
}

func (s *fakeDeltaNDSStream) sendAcked(t *testing.T, nonce string, resources []*discovery.Resource, removed ...string) {
	t.Helper()
	if nack := s.sendNDS(t, nonce, resources, removed...); nack != nil {
		t.Fatalf("unexpected NACK of %s: %v", nonce, nack)
	}
}

// disconnect ends the upstream stream, as if istiod went away.
func (s *fakeDeltaNDSStream) disconnect() {
	close(s.done)
}

type deltaNDSEnv struct {
	dns    *dnsClient.LocalDNSServer
	server *fakeDeltaNDSServer
	conn   *grpc.ClientConn
}

func setupDeltaNDS(t *testing.T) *deltaNDSEnv {
	proxy := setupXdsProxyWithAgentOptions(t, &AgentOptions{DeltaNDS: true}, func(ia *Agent) {
		dnsServer, err := dnsClient.NewLocalDNSServer("default", "default.svc.cluster.local", "localhost:0")
		if err != nil {
			t.Fatal(err)
		}
		ia.localDNSServer = dnsServer
	})
	server := &fakeDeltaNDSServer{streams: make(chan *fakeDeltaNDSStream, 10)}
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	go grpcServer.Serve(listener)
	t.Cleanup(grpcServer.Stop)
	setDialOptions(proxy, listener)
	return &deltaNDSEnv{dns: proxy.ia.localDNSServer, server: server, conn: setupDownstreamConnection(t, proxy)}
}

// connect opens a downstream stream the way Envoy does and returns it with the upstream stream the proxy opened.
func (e *deltaNDSEnv) connect(t *testing.T) (
	downstream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient,
	upstream *fakeDeltaNDSStream,
) {
	t.Helper()
	downstream = deltaStream(t, e.conn)
	node := model.NodeMetadata{
		Namespace:    "default",
		InstanceIPs:  []string{"1.1.1.1"},
		IstioVersion: "1.33.0",
		DeltaNDS:     true,
	}
	err := downstream.Send(&discovery.DeltaDiscoveryRequest{
		TypeUrl: v3.ListenerType,
		Node:    &core.Node{Id: "sidecar~1.1.1.1~debug~cluster.local", Metadata: node.ToStruct()},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case upstream = <-e.server.streams:
	case <-time.After(10 * time.Second):
		t.Fatal("proxy did not open an upstream stream")
	}
	upstream.request(t, v3.ListenerType)
	return downstream, upstream
}

// dnsIPs returns the addresses the local DNS server answers with.
func dnsIPs(dnsServer *dnsClient.LocalDNSServer) map[string][]string {
	out := map[string][]string{}
	for name, info := range dnsServer.NameTable().GetTable() {
		out[name] = info.Ips
	}
	return out
}

func ndsTable(hostname string, table map[string]*dnsProto.NameTable_NameInfo) *discovery.Resource {
	return &discovery.Resource{Name: hostname, Resource: protoconv.MessageToAny(&dnsProto.NameTable{Table: table})}
}

func ndsHost(hostname, ip string) *discovery.Resource {
	info := &dnsProto.NameTable_NameInfo{Ips: []string{ip}, Registry: "Kubernetes"}
	return ndsTable(hostname, map[string]*dnsProto.NameTable_NameInfo{hostname: info})
}

func TestDeltaNDSAckNack(t *testing.T) {
	env := setupDeltaNDS(t)
	_, upstream := env.connect(t)

	initial := upstream.request(t, v3.NameTableType)
	assert.Equal(t, len(initial.InitialResourceVersions), 0)
	assert.Equal(t, initial.ResponseNonce, "")

	upstream.sendAcked(t, "n1", []*discovery.Resource{
		ndsHost("a.default.svc.cluster.local", "10.0.0.1"),
		ndsHost("b.default.svc.cluster.local", "10.0.0.2"),
	})
	assert.Equal(t, dnsIPs(env.dns), map[string][]string{
		"a.default.svc.cluster.local": {"10.0.0.1"},
		"b.default.svc.cluster.local": {"10.0.0.2"},
	})

	// TestNDSDeltaHandler covers what a partially rejected response applies.
	malformed := &discovery.Resource{
		Name:     "b.default.svc.cluster.local",
		Resource: protoconv.MessageToAny(&discovery.Resource{}),
	}
	nack := upstream.sendNDS(t, "n2", []*discovery.Resource{malformed})
	if nack == nil || !strings.Contains(nack.Message, "b.default.svc.cluster.local") {
		t.Fatalf("expected a NACK naming the rejected resource, got %v", nack)
	}

	// The NACK does not stall the stream.
	upstream.sendAcked(t, "n3", []*discovery.Resource{ndsHost("b.default.svc.cluster.local", "10.0.0.4")})
	assert.Equal(t, dnsIPs(env.dns)["b.default.svc.cluster.local"], []string{"10.0.0.4"})
}

func TestDeltaNDSReconnect(t *testing.T) {
	env := setupDeltaNDS(t)
	downstream, upstream := env.connect(t)
	upstream.request(t, v3.NameTableType)

	upstream.sendAcked(t, "n1", []*discovery.Resource{
		ndsHost("a.default.svc.cluster.local", "10.0.0.1"),
		ndsHost("b.default.svc.cluster.local", "10.0.0.2"),
	})
	upstream.sendAcked(t, "n2", []*discovery.Resource{ndsHost("c.default.svc.cluster.local", "10.0.0.3")},
		"b.default.svc.cluster.local")

	// Tear down the upstream stream. The proxy ends the downstream stream, so Envoy reconnects.
	upstream.disconnect()
	if _, err := downstream.Recv(); err == nil {
		t.Fatal("expected the downstream stream to end with the upstream")
	}
	// The held DNS state outlives the stream.
	assert.Equal(t, len(dnsIPs(env.dns)), 2)

	_, upstream = env.connect(t)
	reconnect := upstream.request(t, v3.NameTableType)
	assert.Equal(t, reconnect.InitialResourceVersions, map[string]string{
		"a.default.svc.cluster.local": "",
		"c.default.svc.cluster.local": "",
	})
	assert.Equal(t, reconnect.ResponseNonce, "")

	// The new stream owns the state: istiod removes what went away while the proxy was disconnected.
	upstream.sendAcked(t, "n1", []*discovery.Resource{ndsHost("d.default.svc.cluster.local", "10.0.0.4")},
		"a.default.svc.cluster.local")
	assert.Equal(t, dnsIPs(env.dns), map[string][]string{
		"c.default.svc.cluster.local": {"10.0.0.3"},
		"d.default.svc.cluster.local": {"10.0.0.4"},
	})
}

func TestDeltaNDSLegacySwitch(t *testing.T) {
	env := setupDeltaNDS(t)
	_, upstream := env.connect(t)
	upstream.request(t, v3.NameTableType)

	upstream.sendAcked(t, "n1", []*discovery.Resource{
		ndsHost("a.default.svc.cluster.local", "10.0.0.1"),
		ndsHost("b.default.svc.cluster.local", "10.0.0.2"),
	})
	assert.Equal(t, len(dnsIPs(env.dns)), 2)

	// An unnamed full table replaces every named resource.
	legacy := ndsTable("", map[string]*dnsProto.NameTable_NameInfo{
		"x.default.svc.cluster.local": {Ips: []string{"10.0.1.1"}, Registry: "Kubernetes"},
	})
	upstream.sendAcked(t, "n2", []*discovery.Resource{legacy})
	assert.Equal(t, dnsIPs(env.dns), map[string][]string{"x.default.svc.cluster.local": {"10.0.1.1"}})

	// Named resources after a legacy table start from an empty table.
	upstream.sendAcked(t, "n3", []*discovery.Resource{ndsHost("c.default.svc.cluster.local", "10.0.0.3")})
	assert.Equal(t, dnsIPs(env.dns), map[string][]string{"c.default.svc.cluster.local": {"10.0.0.3"}})
}
