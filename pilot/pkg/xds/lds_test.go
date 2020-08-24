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
package xds_test

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	"istio.io/istio/pkg/config/labels"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

// TestLDS using isolated namespaces
func TestLDSIsolated(t *testing.T) {
	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	// Sidecar in 'none' mode
	t.Run("sidecar_none", func(t *testing.T) {
		ldsr, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
			Meta: model.NodeMetadata{
				InterceptionMode: model.InterceptionNone,
				HTTP10:           "1",
			}.ToStruct(),
			IP:        "10.11.0.1", // matches none.yaml s1tcp.none
			Namespace: "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer ldsr.Close()

		ldsr.Watch()

		_, err = ldsr.Wait(5*time.Second, "lds")
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}

		err = ldsr.Save(env.IstioOut + "/none")
		if err != nil {
			t.Fatal(err)
		}

		// 7071 (inbound), 2001 (service - also as http proxy), 18010 (fortio), 15006 (virtual inbound)
		// We do not get mixer on 9091 because there are no services defined in istio-system namespace
		// in the none.yaml setup
		if len(ldsr.GetHTTPListeners()) != 4 {
			t.Error("HTTP listeners, expecting 4 got ", len(ldsr.GetHTTPListeners()), ldsr.GetHTTPListeners())
		}

		// s1tcp:2000 outbound, bind=true (to reach other instances of the service)
		// s1:5005 outbound, bind=true
		// :443 - https external, bind=false
		// 10.11.0.1_7070, bind=true -> inbound|2000|s1 - on port 7070, fwd to 37070
		// virtual
		if len(ldsr.GetTCPListeners()) == 0 {
			t.Fatal("No response")
		}

		for _, s := range []string{"lds_tcp", "lds_http", "rds", "cds", "ecds"} {
			want, err := ioutil.ReadFile(env.IstioOut + "/none_" + s + ".json")
			if err != nil {
				t.Fatal(err)
			}
			got, err := ioutil.ReadFile("testdata/none_" + s + ".json")
			if err != nil {
				t.Fatal(err)
			}

			if err = util.Compare(got, want); err != nil {
				// Just log for now - golden changes every time there is a config generation update.
				// It is mostly intended as a reference for what is generated - we need to add explicit checks
				// for things we need, like the number of expected listeners.
				// This is mainly using for debugging what changed from the snapshot in the golden files.
				if os.Getenv("CONFIG_DIFF") == "1" {
					t.Logf("error in golden file %s %v", s, err)
				}
			}
		}
	})

	// Test for the examples in the ServiceEntry doc
	t.Run("se_example", func(t *testing.T) {
		// TODO: add a Service with EDS resolution in the none ns.
		// The ServiceEntry only allows STATIC - both STATIC and EDS should generated TCP listeners on :port
		// while DNS and NONE should generate old-style bind ports.
		// Right now 'STATIC' and 'EDS' result in ClientSideLB in the internal object, so listener test is valid.

		ldsr, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
			Meta:      nil,
			IP:        "10.12.0.1", // matches none.yaml s1tcp.none
			Namespace: "seexamples",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer ldsr.Close()

		ldsr.Watch()

		if _, err := ldsr.Wait(5*time.Second, "lds"); err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}
	})

	// Test for the examples in the ServiceEntry doc
	t.Run("se_examplegw", func(t *testing.T) {
		// TODO: add a Service with EDS resolution in the none ns.
		// The ServiceEntry only allows STATIC - both STATIC and EDS should generated TCP listeners on :port
		// while DNS and NONE should generate old-style bind ports.
		// Right now 'STATIC' and 'EDS' result in ClientSideLB in the internal object, so listener test is valid.

		ldsr, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
			Meta:      nil,
			IP:        "10.13.0.1",
			Namespace: "exampleegressgw",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer ldsr.Close()

		ldsr.Watch()

		if _, err = ldsr.Wait(5*time.Second, "lds"); err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}
	})

}

// TestLDS using default sidecar in root namespace
func TestLDSWithDefaultSidecar(t *testing.T) {

	server, tearDown := util.EnsureTestServer(func(args *bootstrap.PilotArgs) {
		args.Plugins = bootstrap.DefaultPlugins
		args.RegistryOptions.FileDir = env.IstioSrc + "/tests/testdata/networking/sidecar-ns-scope"
		args.MeshConfigFile = env.IstioSrc + "/tests/testdata/networking/sidecar-ns-scope/mesh.yaml"
		args.RegistryOptions.Registries = []string{}
	})
	testEnv = env.NewTestSetup(env.SidecarTest, t)
	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
	testEnv.IstioSrc = env.IstioSrc
	testEnv.IstioOut = env.IstioOut

	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
	defer tearDown()

	adsResponse, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
		Meta: model.NodeMetadata{
			InstanceIPs:  []string{"100.1.1.2"},
			Namespace:    "ns1",
			IstioVersion: "1.3.0",
		}.ToStruct(),
		IP:        "100.1.1.2",
		Namespace: "ns1",
	})

	if err != nil {
		t.Fatal(err)
	}
	defer adsResponse.Close()

	adsResponse.Watch()

	upd, err := adsResponse.Wait(10*time.Second, "lds", "rds", "cds")
	if err != nil {
		t.Fatal("Failed to receive XDS response", err, upd)
		return
	}

	// Expect 6 listeners : 2 orig_dst, 4 outbound (http, tcp1, istio-policy and istio-telemetry)
	if (len(adsResponse.GetHTTPListeners()) + len(adsResponse.GetTCPListeners())) != 6 {
		t.Fatalf("Expected 7 listeners, got %d\n", len(adsResponse.GetHTTPListeners())+len(adsResponse.GetTCPListeners()))
	}

	// Expect 11 CDS clusters:
	// 2 inbound(http, inbound passthroughipv4) notes: no passthroughipv6
	// 9 outbound (2 http services, 1 tcp service, 2 istio-system services,
	//   and 2 subsets of http1, 1 blackhole, 1 passthrough)
	if (len(adsResponse.GetClusters()) + len(adsResponse.GetEdsClusters())) != 11 {
		t.Fatalf("Expected 12 clusters in CDS output. Got %d", len(adsResponse.GetClusters())+len(adsResponse.GetEdsClusters()))
	}

	// Expect two vhost blocks in RDS output for 8080 (one for http1, another for http2)
	// plus one extra due to mem registry
	if len(adsResponse.GetRoutes()["8080"].VirtualHosts) != 3 {
		t.Fatalf("Expected 3 VirtualHosts in RDS output. Got %d", len(adsResponse.GetRoutes()["8080"].VirtualHosts))
	}
}

// TestLDS using gateways
func TestLDSWithIngressGateway(t *testing.T) {
	server, tearDown := util.EnsureTestServer(func(args *bootstrap.PilotArgs) {
		args.Plugins = bootstrap.DefaultPlugins
		args.RegistryOptions.FileDir = env.IstioSrc + "/tests/testdata/networking/ingress-gateway"
		args.MeshConfigFile = env.IstioSrc + "/tests/testdata/networking/ingress-gateway/mesh.yaml"
		args.RegistryOptions.Registries = []string{}
	})
	testEnv = env.NewTestSetup(env.GatewayTest, t)
	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
	testEnv.IstioSrc = env.IstioSrc
	testEnv.IstioOut = env.IstioOut

	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
	defer tearDown()

	adsResponse, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
		Meta: model.NodeMetadata{
			InstanceIPs:  []string{"99.1.1.1"}, // as service instance of ingress gateway
			Namespace:    "istio-system",
			IstioVersion: "1.3.0",
		}.ToStruct(),
		IP:        "99.1.1.1",
		Namespace: "istio-system",
		NodeType:  "router",
	})

	if err != nil {
		t.Fatal(err)
	}
	defer adsResponse.Close()

	adsResponse.Watch()

	_, err = adsResponse.Wait(10*time.Second, "lds")
	if err != nil {
		t.Fatal("Failed to receive LDS response", err)
		return
	}

	// Expect 2 listeners : 1 for 80, 1 for 443
	// where 443 listener has 3 filter chains
	if (len(adsResponse.GetHTTPListeners()) + len(adsResponse.GetTCPListeners())) != 2 {
		t.Fatalf("Expected 2 listeners, got %d\n", len(adsResponse.GetHTTPListeners())+len(adsResponse.GetTCPListeners()))
	}

	// TODO: This is flimsy. The ADSC code treats any listener with http connection manager as a HTTP listener
	// instead of looking at it as a listener with multiple filter chains
	l := adsResponse.GetHTTPListeners()["0.0.0.0_443"]

	if l != nil {
		if len(l.FilterChains) != 3 {
			t.Fatalf("Expected 3 filter chains, got %d\n", len(l.FilterChains))
		}
	}
}

// TestLDS is running LDS tests.
func TestLDS(t *testing.T) {
	_, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()

	t.Run("sidecar", func(t *testing.T) {
		ldsr, cancel, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()
		err = sendLDSReq(sidecarID(app3Ip, "app3"), ldsr)
		if err != nil {
			t.Fatal(err)
		}

		res, err := ldsr.Recv()
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
			return
		}
		if len(res.Resources) == 0 {
			t.Fatal("No response")
		}
	})

	// 'router' or 'gateway' type of listener
	t.Run("gateway", func(t *testing.T) {
		ldsr, cancel, err := connectADS(util.MockPilotGrpcAddr)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()
		err = sendLDSReqWithLabels(gatewayID(gatewayIP), ldsr, map[string]string{"version": "v2", "app": "my-gateway-controller"})
		if err != nil {
			t.Fatal(err)
		}

		res, err := ldsr.Recv()
		if err != nil {
			t.Fatal("Failed to receive LDS", err)
		}
		if len(res.Resources) == 0 {
			t.Fatal("No response")
		}
	})
}

// TestLDS using sidecar scoped on workload without Service
func TestLDSWithSidecarForWorkloadWithoutService(t *testing.T) {
	server, tearDown := util.EnsureTestServer(func(args *bootstrap.PilotArgs) {
		args.Plugins = bootstrap.DefaultPlugins
		args.RegistryOptions.FileDir = env.IstioSrc + "/tests/testdata/networking/sidecar-without-service"
		args.MeshConfigFile = env.IstioSrc + "/tests/testdata/networking/sidecar-without-service/mesh.yaml"
		args.RegistryOptions.Registries = []string{}
	})
	registry := memServiceDiscovery(server, t)
	registry.AddWorkload("98.1.1.1", labels.Instance{"app": "consumeronly"}) // These labels must match the sidecars workload selector

	testEnv = env.NewTestSetup(env.SidecarConsumerOnlyTest, t)
	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
	testEnv.IstioSrc = env.IstioSrc
	testEnv.IstioOut = env.IstioOut

	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
	defer tearDown()

	adsResponse, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
		Meta: model.NodeMetadata{
			InstanceIPs:  []string{"98.1.1.1"}, // as service instance of ingress gateway
			Namespace:    "consumerns",
			IstioVersion: "1.3.0",
		}.ToStruct(),
		IP:        "98.1.1.1",
		Namespace: "consumerns", // namespace must match the namespace of the sidecar in the configs.yaml
		NodeType:  "sidecar",
	})

	if err != nil {
		t.Fatal(err)
	}
	defer adsResponse.Close()

	adsResponse.Watch()

	_, err = adsResponse.Wait(10*time.Second, "lds")
	if err != nil {
		t.Fatal("Failed to receive LDS response", err)
		return
	}

	// Expect 2 HTTP listeners for outbound 8081 and one virtualInbound which has the same inbound 9080
	// as a filter chain. Since the adsclient code treats any listener with a HTTP connection manager filter in ANY
	// filter chain,  as a HTTP listener, we end up getting both 9080 and virtualInbound.
	if len(adsResponse.GetHTTPListeners()) != 2 {
		t.Fatalf("Expected 2 http listeners, got %d", len(adsResponse.GetHTTPListeners()))
	}

	// TODO: This is flimsy. The ADSC code treats any listener with http connection manager as a HTTP listener
	// instead of looking at it as a listener with multiple filter chains
	if l := adsResponse.GetHTTPListeners()["0.0.0.0_8081"]; l != nil {
		expected := 2
		if len(l.FilterChains) != expected {
			t.Fatalf("Expected %d filter chains, got %d", expected, len(l.FilterChains))
		}
	} else {
		t.Fatal("Expected listener for 0.0.0.0_8081")
	}

	if l := adsResponse.GetHTTPListeners()["virtualInbound"]; l == nil {
		t.Fatal("Expected listener virtualInbound")
	}

	// Expect only one eds cluster for http1.ns1.svc.cluster.local
	if len(adsResponse.GetEdsClusters()) != 1 {
		t.Fatalf("Expected 1 eds cluster, got %d", len(adsResponse.GetEdsClusters()))
	}
	if cluster, ok := adsResponse.GetEdsClusters()["outbound|8081||http1.ns1.svc.cluster.local"]; !ok {
		t.Fatalf("Expected eds cluster outbound|8081||http1.ns1.svc.cluster.local, got %v", cluster.Name)
	}
}

// TestLDS using default sidecar in root namespace
func TestLDSEnvoyFilterWithWorkloadSelector(t *testing.T) {
	server, tearDown := util.EnsureTestServer(func(args *bootstrap.PilotArgs) {
		args.Plugins = bootstrap.DefaultPlugins
		args.RegistryOptions.FileDir = env.IstioSrc + "/tests/testdata/networking/envoyfilter-without-service"
		args.MeshConfigFile = env.IstioSrc + "/tests/testdata/networking/envoyfilter-without-service/mesh.yaml"
		args.RegistryOptions.Registries = []string{}
	})
	registry := memServiceDiscovery(server, t)
	// The labels of 98.1.1.1 must match the envoyfilter workload selector
	registry.AddWorkload("98.1.1.1", labels.Instance{"app": "envoyfilter-test-app", "some": "otherlabel"})
	registry.AddWorkload("98.1.1.2", labels.Instance{"app": "no-envoyfilter-test-app"})
	registry.AddWorkload("98.1.1.3", labels.Instance{})

	testEnv = env.NewTestSetup(env.SidecarConsumerOnlyTest, t)
	testEnv.Ports().PilotGrpcPort = uint16(util.MockPilotGrpcPort)
	testEnv.Ports().PilotHTTPPort = uint16(util.MockPilotHTTPPort)
	testEnv.IstioSrc = env.IstioSrc
	testEnv.IstioOut = env.IstioOut

	server.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
	defer tearDown()

	tests := []struct {
		name            string
		ip              string
		expectLuaFilter bool
	}{
		{
			name:            "Add filter with matching labels to sidecar",
			ip:              "98.1.1.1",
			expectLuaFilter: true,
		},
		{
			name:            "Ignore filter with not matching labels to sidecar",
			ip:              "98.1.1.2",
			expectLuaFilter: false,
		},
		{
			name:            "Ignore filter with empty labels to sidecar",
			ip:              "98.1.1.3",
			expectLuaFilter: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			adsResponse, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
				Meta: model.NodeMetadata{
					InstanceIPs:  []string{test.ip}, // as service instance of ingress gateway
					Namespace:    "istio-system",
					IstioVersion: "1.4.0",
				}.ToStruct(),
				IP:        test.ip,
				Namespace: "consumerns", // namespace must match the namespace of the sidecar in the configs.yaml
				NodeType:  "sidecar",
			})
			if err != nil {
				t.Fatal(err)
			}
			defer adsResponse.Close()

			adsResponse.Watch()
			_, err = adsResponse.Wait(10*time.Second, "lds")
			if err != nil {
				t.Fatal("Failed to receive LDS response", err)
				return
			}

			// Expect 1 HTTP listeners for 8081, 1 hybrid listeners for 15006 (virtual inbound)
			if len(adsResponse.GetHTTPListeners()) != 2 {
				t.Fatalf("Expected 1 http listeners, got %d", len(adsResponse.GetHTTPListeners()))
			}
			// TODO: This is flimsy. The ADSC code treats any listener with http connection manager as a HTTP listener
			// instead of looking at it as a listener with multiple filter chains
			l := adsResponse.GetHTTPListeners()["0.0.0.0_8081"]

			expectLuaFilter(t, l, test.expectLuaFilter)
		})
	}
}

func expectLuaFilter(t *testing.T, l *listener.Listener, expected bool) {
	if l != nil {
		var chain *listener.FilterChain
		for _, fc := range l.FilterChains {
			if len(fc.Filters) == 1 && fc.Filters[0].Name == wellknown.HTTPConnectionManager {
				chain = fc
			}
		}
		if chain == nil {
			t.Fatalf("Failed to find http_connection_manager")
		}
		if len(chain.Filters) != 1 {
			t.Fatalf("Expected 1 filter in first filter chain, got %d", len(l.FilterChains))
		}
		filter := chain.Filters[0]
		if filter.Name != wellknown.HTTPConnectionManager {
			t.Fatalf("Expected HTTP connection, found %v", chain.Filters[0].Name)
		}
		httpCfg, ok := filter.ConfigType.(*listener.Filter_TypedConfig)
		if !ok {
			t.Fatalf("Expected Http Connection Manager Config Filter_TypedConfig, found %T", filter.ConfigType)
		}
		connectionManagerCfg := hcm.HttpConnectionManager{}
		err := ptypes.UnmarshalAny(httpCfg.TypedConfig, &connectionManagerCfg)
		if err != nil {
			t.Fatalf("Could not deserialize http connection manager config: %v", err)
		}
		found := false
		for _, filter := range connectionManagerCfg.HttpFilters {
			if filter.Name == "envoy.lua" {
				found = true
			}
		}
		if expected != found {
			t.Fatalf("Expected Lua filter: %v, found: %v", expected, found)
		}
	}
}

func memServiceDiscovery(server *bootstrap.Server, t *testing.T) *memory.ServiceDiscovery {
	index, found := server.ServiceController().GetRegistryIndex("v2-debug")
	if !found {
		t.Fatal("Could not find Mock ServiceRegistry")
	}
	registry, ok := server.ServiceController().GetRegistries()[index].(serviceregistry.Simple).ServiceDiscovery.(*memory.ServiceDiscovery)
	if !ok {
		t.Fatal("Unexpected type of Mock ServiceRegistry")
	}
	return registry
}
