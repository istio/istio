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
	"log"
	"os"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
	"istio.io/istio/pilot/pkg/xds"
	v2 "istio.io/istio/pilot/pkg/xds/v2"
	"istio.io/istio/pkg/config/schema/collections"

	_ "google.golang.org/grpc/xds/experimental" // To install the xds resolvers and balancers.
)

var (
	grpcAddr = "127.0.0.1:14057"

	// Address of the Istiod gRPC service, used in tests.
	istiodSvcAddr = "istiod.istio-system.svc.cluster.local:14057"
)

func TestGRPC(t *testing.T) {
	ds := xds.NewXDS()
	ds.DiscoveryServer.Generators["grpc"] = &grpcgen.GrpcConfigGenerator{}
	epGen := &xds.EdsGenerator{Server: ds.DiscoveryServer}
	ds.DiscoveryServer.Generators["grpc/"+v2.EndpointType] = epGen

	sd := ds.DiscoveryServer.MemRegistry
	sd.AddHTTPService("fortio1.fortio.svc.cluster.local", "10.10.10.1", 8081)

	sd.AddHTTPService("istiod.istio-system.svc.cluster.local", "10.10.10.2", 14057)
	sd.SetEndpoints("istiod.istio-system.svc.cluster.local", "", []*model.IstioEndpoint{
		{
			Address:         "127.0.0.1",
			EndpointPort:    uint32(14057),
			ServicePortName: "http-main",
		},
	})
	se := collections.IstioNetworkingV1Alpha3Serviceentries.Resource()
	store := ds.MemoryConfigStore

	store.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
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

	env := ds.DiscoveryServer.Env
	if err := env.PushContext.InitContext(env, env.PushContext, nil); err != nil {
		t.Fatal(err)
	}
	ds.DiscoveryServer.UpdateServiceShards(env.PushContext)

	err := ds.StartGRPC(grpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ds.GRPCListener.Close()

	os.Setenv("GRPC_XDS_BOOTSTRAP", "testdata/xds_bootstrap.json")

	t.Run("gRPC-resolve", func(t *testing.T) {
		rb := resolver.Get("xds-experimental")
		ch := make(chan resolver.State)
		_, err := rb.Build(resolver.Target{Endpoint: istiodSvcAddr},
			&testClientConn{ch: ch}, resolver.BuildOptions{})

		if err != nil {
			t.Fatal("Failed to resolve XDS ", err)
		}
		tm := time.After(10 * time.Second)
		select {
		case s := <-ch:
			log.Println("Got state ", s)
		// TODO: timeout
		case <-tm:
			t.Error("Didn't resolve")
		}
	})

	t.Run("gRPC-cdslb", func(t *testing.T) {
		rb := balancer.Get("eds_experimental")
		b := rb.Build(&testLBClientConn{}, balancer.BuildOptions{})
		defer b.Close()
	})

	t.Run("gRPC-dial", func(t *testing.T) {
		conn, err := grpc.Dial("xds-experimental:///istiod.istio-system.svc.cluster.local:14057", grpc.WithInsecure())
		if err != nil {
			t.Fatal("XDS gRPC", err)
		}

		defer conn.Close()
		xds := ads.NewAggregatedDiscoveryServiceClient(conn)

		s, err := xds.StreamAggregatedResources(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Log(s.Send(&xdsapi.DiscoveryRequest{}))

	})

}

type testLBClientConn struct {
	balancer.ClientConn
}

// From xds_resolver_test
// testClientConn is a fake implemetation of resolver.ClientConn. All is does
// is to store the state received from the resolver locally and signal that
// event through a channel.
type testClientConn struct {
	resolver.ClientConn
	ch chan resolver.State
}

func (t *testClientConn) UpdateState(s resolver.State) {
	t.ch <- s
}

func (t *testClientConn) ReportError(err error) {
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
