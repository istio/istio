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

package grpcgen_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	statefulsession "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/stateful_session/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	cookiev3 "github.com/envoyproxy/go-control-plane/envoy/extensions/http/stateful_session/cookie/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	xdsgrpc "google.golang.org/grpc/xds" // To install the xds resolvers and balancers.
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/istio-agent/grpcxds"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common"
	echoproto "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/echo/server/endpoint"
	"istio.io/istio/pkg/test/env"
)

// Address of the test gRPC service, used in tests.
// Avoid using "istiod" as it is implicitly considered clusterLocal
var testSvcHost = "test.istio-system.svc.cluster.local"

// Local integration tests for proxyless gRPC.
// The tests will start an in-process Istiod, using the memory store, and use
// proxyless grpc servers and clients to validate the config generation.
// GRPC project has more extensive tests for each language, we mainly verify that Istiod
// generates the expected XDS, and gRPC tests verify that the XDS is correctly interpreted.
//
// To debug, set GRPC_GO_LOG_SEVERITY_LEVEL=info;GRPC_GO_LOG_VERBOSITY_LEVEL=99 for
// verbose logs from gRPC side.

// GRPCBootstrap creates the bootstrap bytes dynamically.
// This can be used with NewXDSResolverWithConfigForTesting, and used when creating clients.
//
// See pkg/istio-agent/testdata/grpc-bootstrap.json for a sample bootstrap as expected by Istio agent.
func GRPCBootstrap(app, namespace, ip string, xdsPort int) []byte {
	if ip == "" {
		ip = "127.0.0.1"
	}
	if namespace == "" {
		namespace = "default"
	}
	if app == "" {
		app = "app"
	}
	nodeID := "sidecar~" + ip + "~" + app + "." + namespace + "~" + namespace + ".svc.cluster.local"
	bootstrap, err := grpcxds.GenerateBootstrap(grpcxds.GenerateBootstrapOptions{
		Node: &model.Node{
			ID: nodeID,
			Metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace: namespace,
					Generator: "grpc",
					ClusterID: constants.DefaultClusterName,
				},
			},
		},
		DiscoveryAddress: fmt.Sprintf("127.0.0.1:%d", xdsPort),
		CertDir:          path.Join(env.IstioSrc, "tests/testdata/certs/default"),
	})
	if err != nil {
		return []byte{}
	}
	bootstrapBytes, err := json.Marshal(bootstrap)
	if err != nil {
		return []byte{}
	}
	return bootstrapBytes
}

// resolverForTest creates a resolver for xds:// names using dynamic bootstrap.
func resolverForTest(t test.Failer, xdsPort int, ns string) resolver.Builder {
	xdsresolver, err := xdsgrpc.NewXDSResolverWithConfigForTesting(
		GRPCBootstrap("foo", ns, "10.0.0.1", xdsPort))
	if err != nil {
		t.Fatal(err)
	}
	return xdsresolver
}

func init() {
	// Setup gRPC logging. Do it once in init to avoid races
	o := log.DefaultOptions()
	o.SetDefaultOutputLevel(log.GrpcScopeName, log.DebugLevel)
	log.Configure(o)
}

func TestGRPC(t *testing.T) {
	// Init Istiod in-process server.
	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ListenerBuilder: func() (net.Listener, error) {
			return net.Listen("tcp", "127.0.0.1:0")
		},
	})
	sd := ds.MemRegistry

	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	_, ports, _ := net.SplitHostPort(lis.Addr().String())
	port, _ := strconv.Atoi(ports)

	// Echo service
	// initRBACTests(sd, store, "echo-rbac-plain", 14058, false)
	initRBACTests(sd, ds.Store(), "echo-rbac-mtls", port, true)
	initPersistent(sd)

	_, xdsPorts, _ := net.SplitHostPort(ds.Listener.Addr().String())
	xdsPort, _ := strconv.Atoi(xdsPorts)

	addIstiod(sd, xdsPort)

	// Client bootstrap - will show as "foo.clientns"
	xdsresolver := resolverForTest(t, xdsPort, "clientns")

	// Test the xdsresolver - query LDS and RDS for a specific service, wait for the update.
	// Should be very fast (~20ms) and validate bootstrap and basic XDS connection.
	// Unfortunately we have no way to look at the response except using the logs from XDS.
	// This does not attempt to resolve CDS or EDS.
	t.Run("gRPC-resolve", func(t *testing.T) {
		rb := xdsresolver
		stateCh := make(chan resolver.State, 1)
		errorCh := make(chan error, 1)
		_, err := rb.Build(resolver.Target{URL: url.URL{
			Scheme: "xds",
			Path:   "/" + net.JoinHostPort(testSvcHost, xdsPorts),
		}},
			&testClientConn{stateCh: stateCh, errorCh: errorCh}, resolver.BuildOptions{
				Authority: testSvcHost,
			})
		if err != nil {
			t.Fatal("Failed to resolve XDS ", err)
		}
		tm := time.After(10 * time.Second)
		select {
		case s := <-stateCh:
			t.Log("Got state ", s)
		case e := <-errorCh:
			t.Error("Error in resolve", e)
		case <-tm:
			t.Error("Didn't resolve in time")
		}
	})

	t.Run("gRPC-svc", func(t *testing.T) {
		t.Run("gRPC-svc-tls", func(t *testing.T) {
			// Replaces: insecure.NewCredentials
			creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{FallbackCreds: insecure.NewCredentials()})
			if err != nil {
				t.Fatal(err)
			}

			grpcOptions := []grpc.ServerOption{
				grpc.Creds(creds),
			}

			bootstrapB := GRPCBootstrap("echo-rbac-mtls", "test", "127.0.1.1", xdsPort)
			grpcOptions = append(grpcOptions, xdsgrpc.BootstrapContentsForTesting(bootstrapB))

			// Replaces: grpc NewServer
			grpcServer, err := xdsgrpc.NewGRPCServer(grpcOptions...)
			if err != nil {
				t.Fatal(err)
			}

			testRBAC(t, grpcServer, xdsresolver, "echo-rbac-mtls", port, lis)
		})
	})

	t.Run("persistent", func(t *testing.T) {
		test.SetAtomicBoolForTest(t, features.EnablePersistentSessionFilter, true)
		proxy := ds.SetupProxy(&model.Proxy{Metadata: &model.NodeMetadata{
			Generator: "grpc",
		}})
		adscConn := ds.Connect(proxy, []string{}, []string{})

		adscConn.Send(&discovery.DiscoveryRequest{
			TypeUrl: v3.ListenerType,
		})

		msg, err := adscConn.WaitVersion(5*time.Second, v3.ListenerType, "")
		if err != nil {
			t.Fatal("Failed to receive lds", err)
		}
		// Extract the cookie name from 4 layers of marshaling...
		hcm := &hcm.HttpConnectionManager{}
		ss := &statefulsession.StatefulSession{}
		sc := &cookiev3.CookieBasedSessionState{}
		filterIndex := -1
		for _, rsc := range msg.Resources {
			valBytes := rsc.Value
			ll := &listener.Listener{}
			_ = proto.Unmarshal(valBytes, ll)
			if strings.HasPrefix(ll.Name, "echo-persistent.test.svc.cluster.local:") {
				proto.Unmarshal(ll.ApiListener.ApiListener.Value, hcm)
				for index, f := range hcm.HttpFilters {
					if f.Name == util.StatefulSessionFilter {
						proto.Unmarshal(f.GetTypedConfig().Value, ss)
						filterIndex = index
						if ss.GetSessionState().Name == "envoy.http.stateful_session.cookie" {
							proto.Unmarshal(ss.GetSessionState().TypedConfig.Value, sc)
						}
					}
				}
			}
		}
		if sc.Cookie == nil {
			t.Fatal("Failed to find session cookie")
		}
		if filterIndex == (len(hcm.HttpFilters) - 1) {
			t.Fatal("session-cookie-filter cannot be the last filter!")
		}
		if sc.Cookie.Name != "test-cookie" {
			t.Fatal("Missing expected cookie name", sc.Cookie)
		}
		if sc.Cookie.Path != "/Service/Method" {
			t.Fatal("Missing expected cookie path", sc.Cookie)
		}
		clusterName := "outbound|9999||echo-persistent.test.svc.cluster.local"
		adscConn.Send(&discovery.DiscoveryRequest{
			TypeUrl:       v3.EndpointType,
			ResourceNames: []string{clusterName},
		})
		_, err = adscConn.Wait(5*time.Second, v3.EndpointType)
		if err != nil {
			t.Fatal("Failed to receive endpoint", err)
		}
		ep := adscConn.GetEndpoints()[clusterName]
		if ep == nil {
			t.Fatal("Endpoints not found for persistent session cluster")
		}
		if len(ep.GetEndpoints()) == 0 {
			t.Fatal("No endpoint not found for persistent session cluster")
		}
		lbep1 := ep.GetEndpoints()[0]
		if lbep1.LbEndpoints[0].HealthStatus.Number() != 3 {
			t.Fatal("Draining status not included")
		}
	})

	t.Run("gRPC-dial", func(t *testing.T) {
		for _, host := range []string{
			testSvcHost,
			//"istiod.istio-system.svc",
			//"istiod.istio-system",
			//"istiod",
		} {
			t.Run(host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
				defer cancel()
				conn, err := grpc.DialContext(ctx, "xds:///"+host+":"+xdsPorts, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(),
					grpc.WithResolvers(xdsresolver))
				if err != nil {
					t.Fatal("XDS gRPC", err)
				}
				defer conn.Close()
				s, err := discovery.NewAggregatedDiscoveryServiceClient(conn).StreamAggregatedResources(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_ = s.Send(&discovery.DiscoveryRequest{})
			})
		}
	})
}

func addIstiod(sd *memory.ServiceDiscovery, xdsPort int) {
	sd.AddService(&model.Service{
		Attributes: model.ServiceAttributes{
			Name:      "istiod",
			Namespace: "istio-system",
		},
		Hostname:       host.Name(testSvcHost),
		DefaultAddress: "127.0.1.12",
		Ports: model.PortList{
			{
				Name:     "grpc-main",
				Port:     xdsPort,
				Protocol: protocol.GRPC, // SetEndpoints hardcodes this
			},
		},
	})
	sd.SetEndpoints(testSvcHost, "istio-system", []*model.IstioEndpoint{
		{
			Addresses:       []string{"127.0.0.1"},
			EndpointPort:    uint32(xdsPort),
			ServicePortName: "grpc-main",
		},
	})
}

// initPersistent creates echo-persistent.test:9999, type GRPC with one drained endpoint
func initPersistent(sd *memory.ServiceDiscovery) {
	ns := "test"
	svcname := "echo-persistent"
	hn := svcname + "." + ns + ".svc.cluster.local"
	sd.AddService(&model.Service{
		Attributes: model.ServiceAttributes{
			Name:      svcname,
			Namespace: ns,
			Labels:    map[string]string{features.PersistentSessionLabel: "test-cookie:/Service/Method"},
		},
		Hostname:       host.Name(hn),
		DefaultAddress: "127.0.5.2",
		Ports: model.PortList{
			{
				Name:     "grpc-main",
				Port:     9999,
				Protocol: protocol.GRPC,
			},
		},
	})
	sd.SetEndpoints(hn, ns, []*model.IstioEndpoint{
		{
			Addresses:       []string{"127.0.1.2"},
			EndpointPort:    uint32(9999),
			ServicePortName: "grpc-main",
			HealthStatus:    model.Draining,
		},
		{
			Addresses:       []string{"127.0.1.3", "2001:1::3"},
			EndpointPort:    uint32(9999),
			ServicePortName: "grpc-main",
			HealthStatus:    model.Draining,
		},
	})
}

// initRBACTests creates a service with RBAC configs, to be associated with the inbound listeners.
func initRBACTests(sd *memory.ServiceDiscovery, store model.ConfigStore, svcname string, port int, mtls bool) {
	ns := "test"
	hn := svcname + "." + ns + ".svc.cluster.local"
	// The 'memory' store GetProxyServiceTargets uses the IP address of the node and endpoints to
	// identify the service. In k8s store, labels are matched instead.
	// For server configs to work, the server XDS bootstrap must match the IP.
	sd.AddService(&model.Service{
		// Required: namespace (otherwise DR matching fails)
		Attributes: model.ServiceAttributes{
			Name:      svcname,
			Namespace: ns,
		},
		Hostname:       host.Name(hn),
		DefaultAddress: "127.0.5.1",
		Ports: model.PortList{
			{
				Name:     "grpc-main",
				Port:     port,
				Protocol: protocol.GRPC,
			},
		},
	})
	// The addresses will be matched against the INSTANCE_IPS and id in the node id. If they match, the service is returned.
	sd.SetEndpoints(hn, ns, []*model.IstioEndpoint{
		{
			Addresses:       []string{"127.0.1.1"},
			EndpointPort:    uint32(port),
			ServicePortName: "grpc-main",
		},
		{
			Addresses:       []string{"127.0.1.2", "2001:1::2"},
			EndpointPort:    uint32(port),
			ServicePortName: "grpc-main",
		},
	})

	store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.AuthorizationPolicy,
			Name:             svcname,
			Namespace:        ns,
		},
		Spec: &security.AuthorizationPolicy{
			Rules: []*security.Rule{
				{
					When: []*security.Condition{
						{
							Key: "request.headers[echo]",
							Values: []string{
								"block",
							},
						},
					},
				},
			},
			Action: security.AuthorizationPolicy_DENY,
		},
	})

	store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.AuthorizationPolicy,
			Name:             svcname + "-allow",
			Namespace:        ns,
		},
		Spec: &security.AuthorizationPolicy{
			Rules: []*security.Rule{
				{
					When: []*security.Condition{
						{
							Key: "request.headers[echo]",
							Values: []string{
								"allow",
							},
						},
					},
				},
			},
			Action: security.AuthorizationPolicy_ALLOW,
		},
	})
	if mtls {
		// Client side.
		_, _ = store.Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.DestinationRule,
				Name:             svcname,
				Namespace:        "test",
			},
			Spec: &networking.DestinationRule{
				Host: svcname + ".test.svc.cluster.local",
				TrafficPolicy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				}},
			},
		})

		// Server side.
		_, _ = store.Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.PeerAuthentication,
				Name:             svcname,
				Namespace:        "test",
			},
			Spec: &security.PeerAuthentication{
				Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_STRICT},
			},
		})

		_, _ = store.Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.AuthorizationPolicy,
				Name:             svcname,
				Namespace:        "test",
			},
			Spec: &security.AuthorizationPolicy{
				Rules: []*security.Rule{
					{
						From: []*security.Rule_From{
							{
								Source: &security.Source{
									Principals: []string{"evie"},
								},
							},
						},
					},
				},
				Action: security.AuthorizationPolicy_DENY,
			},
		})
	}
}

func testRBAC(t *testing.T, grpcServer *xdsgrpc.GRPCServer, xdsresolver resolver.Builder, svcname string, port int, lis net.Listener) {
	echos := &endpoint.EchoGrpcHandler{Config: endpoint.Config{Port: &common.Port{Port: port}}}
	echoproto.RegisterEchoTestServiceServer(grpcServer, echos)

	go func() {
		err := grpcServer.Serve(lis)
		if err != nil {
			log.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	creds, _ := xdscreds.NewClientCredentials(xdscreds.ClientOptions{
		FallbackCreds: insecure.NewCredentials(),
	})

	conn, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s.test.svc.cluster.local:%d", svcname, port),
		grpc.WithTransportCredentials(creds),
		grpc.WithBlock(),
		grpc.WithResolvers(xdsresolver))
	if err != nil {
		t.Fatal("XDS gRPC", err)
	}
	defer conn.Close()
	echoc := echoproto.NewEchoTestServiceClient(conn)
	md := metadata.New(map[string]string{"echo": "block"})
	outctx := metadata.NewOutgoingContext(context.Background(), md)
	_, err = echoc.Echo(outctx, &echoproto.EchoRequest{})
	if err == nil {
		t.Fatal("RBAC rule not enforced")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatal("Unexpected error", err)
	}
	t.Log(err)
}

// From xds_resolver_test
// testClientConn is a fake implementation of resolver.ClientConn. All is does
// is to store the state received from the resolver locally and signal that
// event through a channel.
type testClientConn struct {
	resolver.ClientConn
	stateCh chan resolver.State
	errorCh chan error
}

func (t *testClientConn) UpdateState(s resolver.State) error {
	t.stateCh <- s
	return nil
}

func (t *testClientConn) ReportError(err error) {
	t.errorCh <- err
}

func (t *testClientConn) ParseServiceConfig(jsonSC string) *serviceconfig.ParseResult {
	// Will be called with something like:
	// {"loadBalancingConfig":[
	//  {"xds_cluster_manager_experimental":{
	//     "children":{
	//        "cluster:outbound|14057||istiod.istio-system.svc.cluster.local":{
	//           "childPolicy":[
	//          {"cds_experimental":
	//         		{"cluster":"outbound|14057||istiod.istio-system.svc.cluster.local"}}]}}}}]}
	return &serviceconfig.ParseResult{}
}

// TestGRPCRDSVirtualHostFiltering verifies that the gRPC generator's RDS
// response contains only the virtual host matching the requested route name,
// not all services sharing the same port.
func TestGRPCRDSVirtualHostFiltering(t *testing.T) {
	t.Parallel()

	var (
		sharedPort = 8080

		// Create three services on the same port.
		svcA = "svc-a.test.svc.cluster.local"
		svcB = "svc-b.test.svc.cluster.local"
		svcC = "svc-c.test.svc.cluster.local"

		ds = xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		sd = ds.MemRegistry
	)

	for _, svc := range []struct {
		name     string
		hostname string
		ip       string
	}{
		{"svc-a", svcA, "10.0.0.1"},
		{"svc-b", svcB, "10.0.0.2"},
		{"svc-c", svcC, "10.0.0.3"},
	} {
		sd.AddService(&model.Service{
			Attributes: model.ServiceAttributes{
				Name:      svc.name,
				Namespace: "test",
			},
			Hostname:       host.Name(svc.hostname),
			DefaultAddress: svc.ip,
			Ports: model.PortList{
				{
					Name:     "grpc-main",
					Port:     sharedPort,
					Protocol: protocol.GRPC,
				},
			},
		})
		sd.SetEndpoints(svc.hostname, "test", []*model.IstioEndpoint{
			{
				Addresses:       []string{"127.0.0.1"},
				EndpointPort:    uint32(sharedPort),
				ServicePortName: "grpc-main",
			},
		})
	}

	routeName := fmt.Sprintf("outbound|%d||%s", sharedPort, svcA)
	proxy := ds.SetupProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			Generator: "grpc",
			Namespace: "test",
		},
	})
	adscConn := ds.Connect(proxy, []string{}, []string{})
	defer adscConn.Close()

	adscConn.Send(&discovery.DiscoveryRequest{
		TypeUrl:       v3.RouteType,
		ResourceNames: []string{routeName},
	})
	msg, err := adscConn.Wait(5*time.Second, v3.RouteType)
	if err != nil {
		t.Fatal("Failed to receive RDS response", err)
	}

	routes := adscConn.GetRoutes()
	rc, ok := routes[routeName]
	if !ok {
		t.Fatalf("route config %q not found in response, got routes: %v", routeName, msg)
	}

	if len(rc.GetVirtualHosts()) != 1 {
		t.Fatalf("expected 1 virtual host, got %d: %v", len(rc.GetVirtualHosts()), virtualHostNames(rc))
	}

	vh := rc.GetVirtualHosts()[0]
	if !slices.Contains(vh.Domains, svcA) {
		t.Errorf("expected virtual host domains %v to contain %s", vh.Domains, svcA)
	}
}

// TestGRPCRDSWithVirtualService verifies that virtual host filtering works
// correctly when a VirtualService is present. The VS changes routing behavior
// (e.g., routing svc-a traffic to svc-b) but should not affect which virtual
// hosts are returned — only the VH matching the requested hostname.
func TestGRPCRDSWithVirtualService(t *testing.T) {
	t.Parallel()

	var (
		sharedPort = 8080

		svcA = "svc-a.test.svc.cluster.local"
		svcB = "svc-b.test.svc.cluster.local"

		ds = xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
			Configs: []config.Config{
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.VirtualService,
						Name:             "route-a-to-b",
						Namespace:        "test",
					},
					Spec: &networking.VirtualService{
						Hosts: []string{svcA},
						Http: []*networking.HTTPRoute{
							{
								Route: []*networking.HTTPRouteDestination{
									{
										Destination: &networking.Destination{
											Host: svcB,
											Port: &networking.PortSelector{Number: uint32(sharedPort)},
										},
									},
								},
							},
						},
					},
				},
			},
		})
		sd = ds.MemRegistry
	)

	for _, svc := range []struct {
		name     string
		hostname string
		ip       string
	}{
		{"svc-a", svcA, "10.0.0.1"},
		{"svc-b", svcB, "10.0.0.2"},
	} {
		sd.AddService(&model.Service{
			Attributes: model.ServiceAttributes{
				Name:      svc.name,
				Namespace: "test",
			},
			Hostname:       host.Name(svc.hostname),
			DefaultAddress: svc.ip,
			Ports: model.PortList{
				{
					Name:     "grpc-main",
					Port:     sharedPort,
					Protocol: protocol.GRPC,
				},
			},
		})
		sd.SetEndpoints(svc.hostname, "test", []*model.IstioEndpoint{
			{
				Addresses:       []string{"127.0.0.1"},
				EndpointPort:    uint32(sharedPort),
				ServicePortName: "grpc-main",
			},
		})
	}

	proxy := ds.SetupProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			Generator: "grpc",
			Namespace: "test",
		},
	})
	adscConn := ds.Connect(proxy, []string{}, []string{})
	defer adscConn.Close()

	routeName := fmt.Sprintf("outbound|%d||%s", sharedPort, svcA)
	adscConn.Send(&discovery.DiscoveryRequest{
		TypeUrl:       v3.RouteType,
		ResourceNames: []string{routeName},
	})
	_, err := adscConn.Wait(5*time.Second, v3.RouteType)
	if err != nil {
		t.Fatal("Failed to receive RDS response", err)
	}

	routes := adscConn.GetRoutes()
	rc, ok := routes[routeName]
	if !ok {
		t.Fatalf("route config %q not found", routeName)
	}

	// Verify only svc-a's virtual host is returned.
	if len(rc.GetVirtualHosts()) != 1 {
		t.Fatalf("expected 1 virtual host, got %d: %v",
			len(rc.GetVirtualHosts()), virtualHostNames(rc))
	}

	vh := rc.GetVirtualHosts()[0]
	hasSvcA := false
	for _, d := range vh.Domains {
		if d == svcA {
			hasSvcA = true
		}
	}
	if !hasSvcA {
		t.Errorf("virtual host domains %v do not contain %s", vh.Domains, svcA)
	}

	// Verify the routing goes to svc-b (VirtualService effect).
	if len(vh.Routes) == 0 {
		t.Fatal("expected at least one route in the virtual host")
	}
	cluster := vh.Routes[0].GetRoute().GetCluster()
	wantCluster := fmt.Sprintf("outbound|%d||%s", sharedPort, svcB)
	if cluster != wantCluster {
		t.Errorf("expected route cluster %q, got %q", wantCluster, cluster)
	}
}

// TestGRPCRDSWithWildcardVirtualServiceHost verifies wildcard VirtualService
// hosts are matched for the requested route hostname and returned as the only
// virtual host for gRPC RDS.
func TestGRPCRDSWithWildcardVirtualServiceHost(t *testing.T) {
	t.Parallel()

	var (
		sharedPort = 80

		requestedHost   = "foo.example.com"
		wildcardHost    = "*.example.com"
		destinationHost = "api.example.net"

		routeName = fmt.Sprintf("outbound|%d||%s", sharedPort, requestedHost)

		ds = xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
			Configs: []config.Config{
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.VirtualService,
						Name:             "wildcard-host-route",
						Namespace:        "test",
					},
					Spec: &networking.VirtualService{
						Hosts: []string{wildcardHost},
						Http: []*networking.HTTPRoute{
							{
								Route: []*networking.HTTPRouteDestination{
									{
										Destination: &networking.Destination{
											Host: destinationHost,
											Port: &networking.PortSelector{Number: uint32(sharedPort)},
										},
									},
								},
							},
						},
					},
				},
			},
		})

		proxy = ds.SetupProxy(&model.Proxy{
			Metadata: &model.NodeMetadata{
				Generator: "grpc",
				Namespace: "test",
			},
		})
	)

	adscConn := ds.Connect(proxy, []string{}, []string{})
	defer adscConn.Close()

	adscConn.Send(&discovery.DiscoveryRequest{
		TypeUrl:       v3.RouteType,
		ResourceNames: []string{routeName},
	})
	_, err := adscConn.Wait(5*time.Second, v3.RouteType)
	if err != nil {
		t.Fatal("Failed to receive RDS response", err)
	}

	routes := adscConn.GetRoutes()
	rc, ok := routes[routeName]
	if !ok {
		t.Fatalf("route config %q not found", routeName)
	}

	if len(rc.GetVirtualHosts()) != 1 {
		t.Fatalf("expected 1 virtual host, got %d: %v", len(rc.GetVirtualHosts()), virtualHostNames(rc))
	}

	vh := rc.GetVirtualHosts()[0]
	hasWildcard := false
	hasRequestedHost := false
	for _, d := range vh.Domains {
		if d == wildcardHost || d == wildcardHost+":80" {
			hasWildcard = true
		}
		if d == requestedHost || d == requestedHost+":80" {
			hasRequestedHost = true
		}
	}
	if !hasWildcard {
		t.Errorf("virtual host domains %v do not contain wildcard host %q", vh.Domains, wildcardHost)
	}
	if hasRequestedHost {
		t.Errorf(
			"virtual host domains %v unexpectedly contain requested host %q", vh.Domains, requestedHost,
		)
	}

	if len(vh.Routes) == 0 {
		t.Fatal("expected at least one route in the virtual host")
	}
	wantCluster := fmt.Sprintf("outbound|%d||%s", sharedPort, destinationHost)
	cluster := vh.Routes[0].GetRoute().GetCluster()
	if cluster != wantCluster {
		t.Errorf("expected route cluster %q, got %q", wantCluster, cluster)
	}
}

// TestGRPCRDSSubsetRouteFiltering verifies that requesting a subset route name
// (e.g. "outbound|8080|v1|svc.ns.svc.cluster.local") still returns the correct
// virtual host for the hostname.
func TestGRPCRDSSubsetRouteFiltering(t *testing.T) {
	t.Parallel()

	var (
		sharedPort = 8080

		svcA = "svc-a.test.svc.cluster.local"
		svcB = "svc-b.test.svc.cluster.local"

		ds = xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
			Configs: []config.Config{
				{
					Meta: config.Meta{
						GroupVersionKind: gvk.DestinationRule,
						Name:             "svc-a-subsets",
						Namespace:        "test",
					},
					Spec: &networking.DestinationRule{
						Host: svcA,
						Subsets: []*networking.Subset{
							{
								Name:   "v1",
								Labels: map[string]string{"version": "v1"},
							},
						},
					},
				},
			},
		})
		sd = ds.MemRegistry
	)

	for _, svc := range []struct {
		name     string
		hostname string
		ip       string
	}{
		{"svc-a", svcA, "10.0.0.1"},
		{"svc-b", svcB, "10.0.0.2"},
	} {
		sd.AddService(&model.Service{
			Attributes: model.ServiceAttributes{
				Name:      svc.name,
				Namespace: "test",
			},
			Hostname:       host.Name(svc.hostname),
			DefaultAddress: svc.ip,
			Ports: model.PortList{
				{
					Name:     "grpc-main",
					Port:     sharedPort,
					Protocol: protocol.GRPC,
				},
			},
		})
		sd.SetEndpoints(svc.hostname, "test", []*model.IstioEndpoint{
			{
				Addresses:       []string{"127.0.0.1"},
				EndpointPort:    uint32(sharedPort),
				ServicePortName: "grpc-main",
			},
		})
	}

	proxy := ds.SetupProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			Generator: "grpc",
			Namespace: "test",
		},
	})
	adscConn := ds.Connect(proxy, []string{}, []string{})
	defer adscConn.Close()

	// Request with subset in the route name.
	routeName := fmt.Sprintf("outbound|%d|v1|%s", sharedPort, svcA)
	adscConn.Send(&discovery.DiscoveryRequest{
		TypeUrl:       v3.RouteType,
		ResourceNames: []string{routeName},
	})
	_, err := adscConn.Wait(5*time.Second, v3.RouteType)
	if err != nil {
		t.Fatal("Failed to receive RDS response", err)
	}

	routes := adscConn.GetRoutes()
	rc, ok := routes[routeName]
	if !ok {
		t.Fatalf("route config %q not found", routeName)
	}

	// Should still return only svc-a's virtual host.
	if len(rc.GetVirtualHosts()) != 1 {
		t.Fatalf("expected 1 virtual host, got %d: %v",
			len(rc.GetVirtualHosts()), virtualHostNames(rc))
	}
	vh := rc.GetVirtualHosts()[0]
	hasSvcA := false
	for _, d := range vh.Domains {
		if d == svcA {
			hasSvcA = true
		}
	}
	if !hasSvcA {
		t.Errorf("virtual host domains %v do not contain %s", vh.Domains, svcA)
	}
}

// TestGRPCRDSSameShortNameDifferentNamespaces verifies that when two services
// share the same short name but are in different namespaces, requesting a route
// by FQDN returns only the correct service's virtual host.
func TestGRPCRDSSameShortNameDifferentNamespaces(t *testing.T) {
	t.Parallel()

	var (
		sharedPort = 8080

		svcNS1 = "svc.ns1.svc.cluster.local"
		svcNS2 = "svc.ns2.svc.cluster.local"

		ds = xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		sd = ds.MemRegistry
	)

	for _, svc := range []struct {
		name      string
		namespace string
		hostname  string
		ip        string
	}{
		{"svc", "ns1", svcNS1, "10.0.0.1"},
		{"svc", "ns2", svcNS2, "10.0.0.2"},
	} {
		sd.AddService(&model.Service{
			Attributes: model.ServiceAttributes{
				Name:      svc.name,
				Namespace: svc.namespace,
			},
			Hostname:       host.Name(svc.hostname),
			DefaultAddress: svc.ip,
			Ports: model.PortList{
				{
					Name:     "grpc-main",
					Port:     sharedPort,
					Protocol: protocol.GRPC,
				},
			},
		})
		sd.SetEndpoints(svc.hostname, svc.namespace, []*model.IstioEndpoint{
			{
				Addresses:       []string{"127.0.0.1"},
				EndpointPort:    uint32(sharedPort),
				ServicePortName: "grpc-main",
			},
		})
	}

	proxy := ds.SetupProxy(&model.Proxy{
		Metadata: &model.NodeMetadata{
			Generator: "grpc",
			Namespace: "ns1",
		},
	})
	adscConn := ds.Connect(proxy, []string{}, []string{})
	defer adscConn.Close()

	routeName := fmt.Sprintf("outbound|%d||%s", sharedPort, svcNS1)
	adscConn.Send(&discovery.DiscoveryRequest{
		TypeUrl:       v3.RouteType,
		ResourceNames: []string{routeName},
	})
	_, err := adscConn.Wait(5*time.Second, v3.RouteType)
	if err != nil {
		t.Fatal("Failed to receive RDS response", err)
	}

	routes := adscConn.GetRoutes()
	rc, ok := routes[routeName]
	if !ok {
		t.Fatalf("route config %q not found", routeName)
	}

	// Should return only ns1's virtual host, not ns2's.
	if len(rc.GetVirtualHosts()) != 1 {
		t.Fatalf("expected 1 virtual host, got %d: %v",
			len(rc.GetVirtualHosts()), virtualHostNames(rc))
	}

	vh := rc.GetVirtualHosts()[0]
	hasNS1 := false
	hasNS2 := false
	for _, d := range vh.Domains {
		if d == svcNS1 {
			hasNS1 = true
		}
		if d == svcNS2 {
			hasNS2 = true
		}
	}
	if !hasNS1 {
		t.Errorf("virtual host domains %v do not contain %s", vh.Domains, svcNS1)
	}
	if hasNS2 {
		t.Errorf("virtual host domains %v unexpectedly contain %s", vh.Domains, svcNS2)
	}
}

func virtualHostNames(rc *route.RouteConfiguration) []string {
	var names []string
	for _, vh := range rc.GetVirtualHosts() {
		names = append(names, vh.Name)
	}
	return names
}
