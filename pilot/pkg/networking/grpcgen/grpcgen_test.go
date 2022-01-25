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
	"io/ioutil"
	"net"
	"net/url"
	"path"
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/grpclog"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	xdsgrpc "google.golang.org/grpc/xds"
	security "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/server/endpoint"

	// To install the xds resolvers and balancers.
	grpcxdsresolver "google.golang.org/grpc/xds"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/istio-agent/grpcxds"
	"istio.io/istio/pkg/test"
	echoproto "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/env"
)

var (
	grpcXdsAddr = "127.0.0.1:14057"

	// Address of the Istiod gRPC service, used in tests.
	istiodSvcHost = "istiod.istio-system.svc.cluster.local"
	istiodSvcAddr = "istiod.istio-system.svc.cluster.local:14057"
)

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
func GRPCBootstrap(app, namespace, ip string) []byte {
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
					Namespace:   namespace,
					Generator:   "grpc",
					ClusterID:   "Kubernetes",
					InstanceIPs: []string{""},
				},
			},
		},
		DiscoveryAddress: grpcXdsAddr,
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
func resolverForTest(t test.Failer, ns string) resolver.Builder {
	xdsresolver, err := grpcxdsresolver.NewXDSResolverWithConfigForTesting(
		GRPCBootstrap("foo", ns, "10.0.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	return xdsresolver
}

func TestGRPC(t *testing.T) {
	// Istio already has a dependency on Zap and istio-ecosystem - set the logger
	// so we can explicitly enable level/verbosity. Alternative is to use env variables.
	// '4' skips the adapters, shows the actual code logging the info.
	// 3  works for most core - but fails for 'prefixLogger' - used by XDS
	zl, _ := zap.NewDevelopment(zap.AddCallerSkip(4))
	// TODO: use zap.observer, we can use the collected info for assertions.
	grpc_zap.ReplaceGrpcLoggerV2WithVerbosity(zl, 99)

	// Init Istiod in-process server.
	ds := xds.NewXDS(make(chan struct{}))
	sd := ds.DiscoveryServer.MemRegistry
	sd.ClusterID = "Kubernetes"
	store := ds.MemoryConfigStore

	// Echo service
	//initRBACTests(sd, store, "echo-rbac-plain", 14058, false)
	initRBACTests(sd, store, "echo-rbac-mtls", 14059, true)

	env := ds.DiscoveryServer.Env
	env.Init()
	if err := env.PushContext.InitContext(env, env.PushContext, nil); err != nil {
		t.Fatal(err)
	}
	ds.DiscoveryServer.UpdateServiceShards(env.PushContext)

	if err := ds.StartGRPC(grpcXdsAddr); err != nil {
		t.Fatal(err)
	}
	defer ds.GRPCListener.Close()

	// Client bootstrap - will show as "foo.clientns"
	xdsresolver := resolverForTest(t, "clientns")

	// grpc defaults to grpclog.loggerv2, which uses golang log
	xdslog := grpclog.Component("xds")
	xdslog.Info("XDS logging")

	// Test the xdsresolver - query LDS and RDS for a specific service, wait for the update.
	// Should be very fast (~20ms) and validate bootstrap and basic XDS connection.
	// Unfortunately we have no way to look at the response.
	t.Run("gRPC-resolve", func(t *testing.T) {
		stateCh := &Channel{ch: make(chan interface{}, 1)}
		errorCh := &Channel{ch: make(chan interface{}, 1)}
		cc := &testClientConn{stateCh: stateCh, errorCh: errorCh}

		_, err := xdsresolver.Build(resolver.Target{
			URL: url.URL{Scheme: "xds", Path: "/" + istiodSvcAddr}},
			cc,
			resolver.BuildOptions{})
		if err != nil {
			t.Fatal("Failed to resolve XDS ", err)
		}
		tm := time.After(10 * time.Second)
		select {
		case s := <-stateCh.ch:
			t.Logf("Got state %V", s)
		case e := <-errorCh.ch:
			t.Error("Error in resolve", e)
		case <-tm:
			t.Error("Didn't resolve")
		}
	})

	t.Run("gRPC-svc", func(t *testing.T) {

		t.Run("gRPC-svc-tls", func(t *testing.T) {
			// Replaces: creds := insecure.NewCredentials()
			creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{FallbackCreds: insecure.NewCredentials()})
			if err != nil {
				t.Fatal(err)
			}

			grpcOptions := []grpc.ServerOption{
				grpc.Creds(creds),
			}

			//bootstrapB, _ := bootstrapForTest("echo-rbac-mtls", "test")
			bootstrapB, err := ioutil.ReadFile("testdata/xds_bootstrap.json")
			if err != nil {
				t.Fatal(err)
			}
			grpcOptions = append(grpcOptions, xdsgrpc.BootstrapContentsForTesting(bootstrapB))

			// Replaces: grpc.NewServer(grpcOptions...)
			grpcServer := xdsgrpc.NewGRPCServer(grpcOptions...)

			testRBAC(t, grpcServer, 14059, xdsresolver, "echo-rbac-mtls")
		})

		//t.Run("gRPC-svc-rbac-plain", func(t *testing.T) {
		//	grpcServer := xdsgrpc.NewGRPCServer()
		//
		//	testRBAC(t, grpcServer, 14058, xdsresolver, "echo-rbac-plain")
		//})
	})

	t.Run("gRPC-dial", func(t *testing.T) {
		for _, host := range []string{
			"istiod.istio-system.svc.cluster.local",
			"istiod.istio-system.svc",
			"istiod.istio-system",
			"istiod",
		} {
			t.Run(host, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
				defer cancel()
				conn, err := grpc.DialContext(ctx, "xds:///"+host+":14057", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(),
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

func initRBACTests(sd *memory.ServiceDiscovery, store model.IstioConfigStore, svcname string, port int, mtls bool) {
	ns := "test"
	hn := svcname + "." + ns + ".svc.cluster.local"
	// The 'memory' store GetProxyServiceInstances uses the IP address of the node and endpoints to
	// identify the service. In k8s store, labels are matched instead.
	// For server configs to work, the server XDS bootstrap must match the IP.
	sd.AddService(host.Name(hn), &model.Service{
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
	// The address will be matched against the INSTANCE_IPS and id in the node id. If they match, the service is returned.
	sd.SetEndpoints(hn, ns, []*model.IstioEndpoint{
		{
			Address:         "127.0.1.1",
			EndpointPort:    uint32(port),
			ServicePortName: "grpc-main",
		},
	})

	store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
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
	if false {
		// currently gRPC has a bug, doesn't allow both allow and deny
		store.Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
				Name:             svcname + "-allow",
				Namespace:        ns,
			},
			Spec: &security.AuthorizationPolicy{
				Rules: []*security.Rule{
					{When: []*security.Condition{
						{Key: "request.headers[echo]",
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
	}

	if mtls {
		// Client side.
		_, _ = store.Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
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
				GroupVersionKind: collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind(),
				Name:             svcname,
				Namespace:        "test",
			},
			Spec: &security.PeerAuthentication{
				Mtls: &security.PeerAuthentication_MutualTLS{Mode: security.PeerAuthentication_MutualTLS_STRICT},
			},
		})

		_, _ = store.Create(config.Config{
			Meta: config.Meta{
				GroupVersionKind: collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().GroupVersionKind(),
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

func testRBAC(t *testing.T, grpcServer *grpcxdsresolver.GRPCServer, port int, xdsresolver resolver.Builder, svcname string) {
	echos := &endpoint.EchoGrpcHandler{Config: endpoint.Config{Port: &common.Port{Port: port}}}
	echoproto.RegisterEchoTestServiceServer(grpcServer, echos)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("net.Listen(tcp, %q) failed: %v", port, err)
	}

	go func() {
		err := grpcServer.Serve(lis)
		if err != nil {
			t.Fatal(err)
		}
	}()
	time.Sleep(3 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{
		FallbackCreds: insecure.NewCredentials()})

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

type Channel struct {
	ch chan interface{}
}

// Send sends value on the underlying channel.
func (c *Channel) Send(value interface{}) {
	c.ch <- value
}

// From xds_resolver_test
// testClientConn is a fake implemetation of resolver.ClientConn. All is does
// is to store the state received from the resolver locally and signal that
// event through a channel.
type testClientConn struct {
	resolver.ClientConn
	stateCh *Channel
	errorCh *Channel
}

func (t *testClientConn) UpdateState(s resolver.State) error {
	t.stateCh.Send(s)
	return nil
}

func (t *testClientConn) ReportError(err error) {
	t.errorCh.Send(err)
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
