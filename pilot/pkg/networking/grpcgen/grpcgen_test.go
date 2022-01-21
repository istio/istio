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
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
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

// bootstrapForTest creates the bootstrap bytes dynamically.
// This can be used with NewXDSResolverWithConfigForTesting, and used when creating clients.
func bootstrapForTest(nodeID, namespace string) ([]byte, error) {
	bootstrap, err := grpcxds.GenerateBootstrap(grpcxds.GenerateBootstrapOptions{
		Node: &model.Node{
			ID: nodeID,
			Metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace: namespace,
					Generator: "grpc",
					ClusterID: "Kubernetes",
				},
			},
		},
		DiscoveryAddress: grpcXdsAddr,
		CertDir:          path.Join(env.IstioSrc, "tests/testdata/certs/default"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed generating bootstrap: %v", err)
	}
	bootstrapBytes, err := json.Marshal(bootstrap)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling bootstrap: %v", err)
	}
	return bootstrapBytes, nil
}

// resolverForTest creates a resolver for xds:// names using dynamic bootstrap.
func resolverForTest(t test.Failer, ns string) resolver.Builder {
	bootstrap, err := bootstrapForTest("sidecar~10.0.0.1~foo."+ns+"~"+ns+".svc.cluster.local", ns)
	if err != nil {
		t.Fatal(err)
	}
	xdsresolver, err := grpcxdsresolver.NewXDSResolverWithConfigForTesting(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	return xdsresolver
}



func TestGRPC(t *testing.T) {
	// Init Istiod in-process server.
	ds := xds.NewXDS(make(chan struct{}))
	sd := ds.DiscoveryServer.MemRegistry
	sd.ClusterID = "Kubernetes"
	store := ds.MemoryConfigStore

	// Echo service
	//initRBACTests(sd, store, "echo-rbac-plain", 14058, false)
	initRBACTests(sd, store, "echo-rbac-mtls", 14059, true)

	sd.AddHTTPService("fortio1.fortio.svc.cluster.local", "10.10.10.1", 8081)
	sd.AddHTTPService(istiodSvcHost, "10.10.10.2", 14057)
	sd.SetEndpoints(istiodSvcHost, "", []*model.IstioEndpoint{
		{
			Address:         "127.0.0.1",
			EndpointPort:    uint32(14057),
			ServicePortName: "http-main",
		},
	})

	store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
			Name:             "fortio",
			Namespace:        "fortio",
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{
				"fortio.fortio.svc",
				"fortio.fortio.svc.cluster.local",
			},
			Addresses: []string{"1.2.3.4"},

			Ports: []*networking.Port{
				{Number: 14057, Name: "grpc-insecure", Protocol: "http"},
			},

			Endpoints: []*networking.WorkloadEntry{
				{
					Address: "127.0.0.1",
					Ports:   map[string]uint32{"grpc-insecure": 8080},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_STATIC,
		},
	})

	_, _ = store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: collections.IstioNetworkingV1Alpha3Destinationrules.Resource().GroupVersionKind(),
			Name:             "mtls",
			Namespace:        "istio-system",
		},
		Spec: &networking.DestinationRule{
			Host: istiodSvcHost,
			TrafficPolicy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{
				Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
			}},
		},
	})

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

	xdsresolver := resolverForTest(t, "istio-system")

	t.Run("gRPC-resolve", func(t *testing.T) {
		rb := xdsresolver
		stateCh := &Channel{ch: make(chan interface{}, 1)}
		errorCh := &Channel{ch: make(chan interface{}, 1)}
		_, err := rb.Build(resolver.Target{URL: url.URL{Scheme: "xds", Path: "/" + istiodSvcAddr}},
			&testClientConn{stateCh: stateCh, errorCh: errorCh}, resolver.BuildOptions{})
		if err != nil {
			t.Fatal("Failed to resolve XDS ", err)
		}
		tm := time.After(10 * time.Second)
		select {
		case s := <-stateCh.ch:
			t.Log("Got state ", s)
		case e := <-errorCh.ch:
			t.Error("Error in resolve", e)
		case <-tm:
			t.Error("Didn't resolve")
		}
	})

	xdslog := grpclog.Component("xds")
	xdslog.Info("XDS logging")

	t.Run("gRPC-cdslb", func(t *testing.T) {
		rb := balancer.Get("cluster_resolver_experimental")
		b := rb.Build(&testLBClientConn{}, balancer.BuildOptions{})
		defer b.Close()
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
			Name: svcname,
			Namespace: ns,
		},
		Hostname: host.Name(hn),
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
				&security.Rule{
					When: []*security.Condition {
						&security.Condition {
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
					{ When: []*security.Condition{
							{ Key: "request.headers[echo]",
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
	md := metadata.New(map[string]string{"echo":"block"})
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

type testLBClientConn struct {
	balancer.ClientConn
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
	//
	//	"loadBalancingConfig":[
	//	{
	//		"cds_experimental":{
	//			"Cluster": "istiod.istio-system.svc.cluster.local:14056"
	//		}
	//	}
	//]
	//}
	return &serviceconfig.ParseResult{}
}
