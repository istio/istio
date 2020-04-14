// Copyright 2020 Istio Authors
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

package dns

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/miekg/dns"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"

	"istio.io/istio/pkg/test/env"

	_ "google.golang.org/grpc/xds/experimental" // To install the xds resolvers and balancers.
)

var (
	istiodDNSAddr = "127.0.0.1:14053"
	agentDNSAddr  = "127.0.0.1:14054"

	grpcAddr = "127.0.0.1:14056"

	// Address of the Istiod gRPC service, used in tests.
	istiodSvcAddr = "istiod.istio-system.svc.cluster.local:14056"

	initErr error
)

func init() {
	initErr = initDNS()
}

func initDNS() error {
	key := path.Join(env.IstioSrc, "tests/testdata/certs/pilot/key.pem")
	cert := path.Join(env.IstioSrc, "tests/testdata/certs/pilot/cert-chain.pem")
	root := path.Join(env.IstioSrc, "tests/testdata/certs/pilot/root-cert.pem")

	certP, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}

	cp := x509.NewCertPool()
	rootCertBytes, err := ioutil.ReadFile(root)
	if err != nil {
		return err
	}
	cp.AppendCertsFromPEM(rootCertBytes)

	cfg := &tls.Config{
		Certificates: []tls.Certificate{certP},
		ClientAuth:   tls.NoClientCert,
		ClientCAs:    cp,
	}
	// TODO: load certificates for TLS
	istiodDNS := InitDNS()
	l, err := net.Listen("tcp", istiodDNSAddr)
	if err != nil {
		return err
	}
	istiodDNS.StartDNS(istiodDNSAddr, tls.NewListener(l, cfg))

	agentDNS := InitDNSAgent(istiodDNSAddr, "", rootCertBytes, []string{".com."})
	agentDNS.StartDNS(agentDNSAddr, nil)
	return nil
}

// Creates an in-process discovery server, using the same code as Istiod, but
// backed by an in-memory config and endpoint store.
func initDS() (*v2.DiscoveryServer, error) {
	// Start an XDS server. Also see bench_test in envoy_v2
	env := &model.Environment{
		PushContext: model.NewPushContext(),
	}
	mc := mesh.DefaultMeshConfig()
	env.Watcher = mesh.NewFixedWatcher(&mc)
	env.PushContext.Mesh = env.Watcher.Mesh()

	ds := v2.NewDiscoveryServer(env, nil)

	// In-memory config store, controller and istioConfigStore
	store := memory.Make(collections.Pilot)
	initStore(store)
	configController := memory.NewController(store)
	istioConfigStore := model.MakeIstioStore(configController)

	// Endpoints/Clusters - using the config store for ServiceEntries
	serviceControllers := aggregate.NewController()

	serviceEntryStore := external.NewServiceDiscovery(configController, istioConfigStore, ds)
	serviceEntryRegistry := serviceregistry.Simple{
		ProviderID:       "External",
		Controller:       serviceEntryStore,
		ServiceDiscovery: serviceEntryStore,
	}
	serviceControllers.AddRegistry(serviceEntryRegistry)

	sd := v2.NewMemServiceDiscovery(map[host.Name]*model.Service{}, 0)
	sd.EDSUpdater = ds
	ds.MemRegistry = sd
	serviceControllers.AddRegistry(serviceregistry.Simple{
		ProviderID:       "Mem",
		ServiceDiscovery: sd,
		Controller:       sd.Controller,
	})
	sd.AddHTTPService("fortio1.fortio.svc.cluster.local", "10.10.10.1", 8081)
	//sd.AddEndpoint("fortio1.fortio.svc.cluster.local",
	//	"http-main", 8081, "127.0.0.1", 15019)

	sd.AddHTTPService("istiod.istio-system.svc.cluster.local", "10.10.10.2", 14056)
	sd.SetEndpoints("istiod.istio-system.svc.cluster.local", "", []*model.IstioEndpoint{
		{
			Address:         "127.0.0.1",
			EndpointPort:    uint32(14056),
			ServicePortName: "http-main",
		},
	})

	go configController.Run(make(chan struct{}))

	env.IstioConfigStore = istioConfigStore
	env.ServiceDiscovery = serviceControllers

	if err := env.PushContext.InitContext(env, env.PushContext, nil); err != nil {
		return nil, err
	}
	return ds, nil
}

// Add in-memory objects to the store, used by the tests
func initStore(store model.ConfigStore) {
	se := collections.IstioNetworkingV1Alpha3Serviceentries.Resource()
	store.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      se.Kind(),
			Group:     se.Group(),
			Version:   se.Version(),
			Name:      "istiod",
			Namespace: "istio-system",
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{
				"istiod.istio-system.svc",
				"istiod.istio-system.svc.cluster.local",
			},
			Addresses: []string{"1.2.3.4"},

			Ports: []*networking.Port{
				{Number: 14056, Name: "grpc-insecure", Protocol: "http"},
			},

			Endpoints: []*networking.WorkloadEntry{
				{
					Address: "127.0.0.1",
					Ports:   map[string]uint32{"grpc-insecure": 14056},
				},
			},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_STATIC,
		},
	})
	store.Create(model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      se.Kind(),
			Group:     se.Group(),
			Version:   se.Version(),
			Name:      "fortio",
			Namespace: "fortio",
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{
				"fortio.fortio.svc",
				"fortio.fortio.svc.cluster.local",
			},
			Addresses: []string{"1.2.3.4"},

			Ports: []*networking.Port{
				{Number: 14056, Name: "grpc-insecure", Protocol: "http"},
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
}

// Test using resolving DNS over GRPC. This uses XDS protocol, and Listener resources
// to represent the names. The protocol is based on GRPC resolution of XDS resources.
func TestDNSGRPC(t *testing.T) {

	ds, _ := initDS()

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	ds.Register(s)

	go s.Serve(lis)
	defer lis.Close()

	t.Run("adsc", func(t *testing.T) {
		adscConn, err := adsc.Dial(grpcAddr, "", &adsc.Config{
			IP: "1.2.3.4",
			Meta: model.NodeMetadata {
				InterceptionMode: model.InterceptionAPI,
			}.ToStruct(),
		})
		if err != nil {
			t.Fatal("Error connecting ", err)
		}

		adscConn.Send(&xdsapi.DiscoveryRequest{
			TypeUrl:       adsc.ListenerType,
		})

		got, err := adscConn.Wait(10*time.Second, adsc.ListenerType)
		if err != nil {
			t.Fatal("Failed to receive lds", err)
		}
		if len(got) == 0 {
			t.Fatal("No LDS response")
		}
		data := adscConn.Received[adsc.ListenerType]
		for _, rs := range data.Resources {
			l := &xdsapi.Listener{}
			err = proto.Unmarshal(rs.Value, l)
			if err != nil {
				t.Fatal("Unmarshall error ", err)
			}

			t.Log("LDS: ", l)
		}
	})
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
		//ch := make(chan resolver.State)
		b := rb.Build(&testLBClientConn{}, balancer.BuildOptions{})

		defer b.Close()

	})

	t.Run("gRPC-dial", func(t *testing.T) {
		conn, err := grpc.Dial("xds-experimental:///istiod.istio-system.svc.cluster.local:14056", grpc.WithInsecure())
		if err != nil {
			t.Fatal("XDS gRPC", err)
		}

		defer conn.Close()
		xds := ads.NewAggregatedDiscoveryServiceClient(conn)

		s, err := xds.StreamAggregatedResources(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		s.Send(&xdsapi.DiscoveryRequest{})

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
	log.Println("UPDATE STATE ", s)
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

func TestDNS(t *testing.T) {
	if initErr != nil {
		t.Fatal(initErr)
	}

	t.Run("simple", func(t *testing.T) {
		c := dns.Client{}
		m := new(dns.Msg)
		m.SetQuestion("www.google.com.", dns.TypeA)
		res, _, err := c.Exchange(m, agentDNSAddr)

		if err != nil {
			t.Error("Failed to resolve", err)
		} else if len(res.Answer) == 0 {
			t.Error("No results")
		}

	})
}

// Benchmark - the actual address will be cached by the local resolver.
// Baseline:
//  - initial impl in 1.6:
//      170 uS on agent
//      116 uS on istiod (UDP to machine resolver)
//      75 uS machine resolver
func BenchmarkDNS(t *testing.B) {
	if initErr != nil {
		t.Fatal(initErr)
	}

	t.Run("TLS", func(b *testing.B) {
		bench(b, agentDNSAddr)
	})

	t.Run("plain", func(b *testing.B) {
		bench(b, istiodDNSAddr)
	})

	t.Run("public", func(b *testing.B) {
		dnsConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			b.Fatal(err)
		}

		bench(b, dnsConfig.Servers[0]+":53")
	})
}

func bench(t *testing.B, dest string) {
	errs := 0
	nrs := 0
	for i := 0; i < t.N; i++ {
		c := dns.Client{
			Timeout: 1 * time.Second,
		}
		m := new(dns.Msg)
		m.SetQuestion("www.google.com.", dns.TypeA)
		res, _, err := c.Exchange(m, dest)

		if err != nil {
			errs++
		} else if len(res.Answer) == 0 {
			nrs++
		}
	}

	if errs+nrs > 0 {
		t.Log("Sent", t.N, "err", errs, "no response", nrs)
	}

}
