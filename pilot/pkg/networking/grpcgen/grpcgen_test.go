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
	"net/url"
	"path"
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	// To install the xds resolvers and balancers.
	grpcxdsresolver "google.golang.org/grpc/xds"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/istio-agent/grpcxds"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
)

var (
	grpcXdsAddr = "127.0.0.1:14057"

	// Address of the Istiod gRPC service, used in tests.
	istiodSvcHost = "istiod.istio-system.svc.cluster.local"
	istiodSvcAddr = "istiod.istio-system.svc.cluster.local:14057"
)

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
	xdsresolver := resolverForTest(t, "istio-system")
	ds := xds.NewXDS(make(chan struct{}))

	sd := ds.DiscoveryServer.MemRegistry
	sd.ClusterID = "Kubernetes"
	sd.AddHTTPService("fortio1.fortio.svc.cluster.local", "10.10.10.1", 8081)

	sd.AddHTTPService(istiodSvcHost, "10.10.10.2", 14057)
	sd.SetEndpoints(istiodSvcHost, "", []*model.IstioEndpoint{
		{
			Address:         "127.0.0.1",
			EndpointPort:    uint32(14057),
			ServicePortName: "http-main",
		},
	})
	se := collections.IstioNetworkingV1Alpha3Serviceentries.Resource()
	store := ds.MemoryConfigStore

	store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: se.GroupVersionKind(),
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

	t.Run("gRPC-cdslb", func(t *testing.T) {
		rb := balancer.Get("cluster_resolver_experimental")
		b := rb.Build(&testLBClientConn{}, balancer.BuildOptions{})
		defer b.Close()
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
				conn, err := grpc.DialContext(ctx, "xds:///"+host+":14057", grpc.WithInsecure(), grpc.WithBlock(),
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
