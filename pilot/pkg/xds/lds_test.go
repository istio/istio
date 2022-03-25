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
	"os"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

// TestLDS using isolated namespaces
func TestLDSIsolated(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{ConfigString: mustReadfolder(t, "tests/testdata/config")})

	// Sidecar in 'none' mode
	t.Run("sidecar_none", func(t *testing.T) {
		adscon := s.Connect(&model.Proxy{
			Metadata: &model.NodeMetadata{
				InterceptionMode: model.InterceptionNone,
				HTTP10:           "1",
			},
			IPAddresses:     []string{"10.11.0.1"}, // matches none.yaml s1tcp.none
			ConfigNamespace: "none",
		}, nil, watchAll)

		err := adscon.Save(env.IstioOut + "/none")
		if err != nil {
			t.Fatal(err)
		}

		// 7071 (inbound), 2001 (service - also as http proxy), 18010 (fortio)
		if len(adscon.GetHTTPListeners()) != 3 {
			t.Error("HTTP listeners, expecting 3 got", len(adscon.GetHTTPListeners()), xdstest.MapKeys(adscon.GetHTTPListeners()))
		}

		// s1tcp:2000 outbound, bind=true (to reach other instances of the service)
		// s1:5005 outbound, bind=true
		// :443 - https external, bind=false
		// 10.11.0.1_7070, bind=true -> inbound|2000|s1 - on port 7070, fwd to 37070
		// virtual
		if len(adscon.GetTCPListeners()) == 0 {
			t.Fatal("No response")
		}

		for _, s := range []string{"lds_tcp", "lds_http", "rds", "cds", "ecds"} {
			want, err := os.ReadFile(env.IstioOut + "/none_" + s + ".json")
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile("testdata/none_" + s + ".json")
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

		s.Connect(&model.Proxy{
			IPAddresses:     []string{"10.12.0.1"}, // matches none.yaml s1tcp.none
			ConfigNamespace: "seexamples",
		}, nil, watchAll)
	})

	// Test for the examples in the ServiceEntry doc
	t.Run("se_examplegw", func(t *testing.T) {
		// TODO: add a Service with EDS resolution in the none ns.
		// The ServiceEntry only allows STATIC - both STATIC and EDS should generated TCP listeners on :port
		// while DNS and NONE should generate old-style bind ports.
		// Right now 'STATIC' and 'EDS' result in ClientSideLB in the internal object, so listener test is valid.

		s.Connect(&model.Proxy{
			IPAddresses:     []string{"10.13.0.1"}, // matches none.yaml s1tcp.none
			ConfigNamespace: "exampleegressgw",
		}, nil, watchAll)
	})
}

// TestLDS using default sidecar in root namespace
func TestLDSWithDefaultSidecar(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadfolder(t, "tests/testdata/networking/sidecar-ns-scope"),
		MeshConfig: func() *meshconfig.MeshConfig {
			m := mesh.DefaultMeshConfig()
			m.RootNamespace = "istio-config"
			return m
		}(),
	})
	adsc := s.Connect(&model.Proxy{ConfigNamespace: "ns1", IPAddresses: []string{"100.1.1.2"}}, nil, watchAll)

	// Expect 2 listeners : 2 orig_dst, 2 outbound (http, tcp1)
	if (len(adsc.GetHTTPListeners()) + len(adsc.GetTCPListeners())) != 4 {
		t.Fatalf("Expected 4 listeners, got %d\n", len(adsc.GetHTTPListeners())+len(adsc.GetTCPListeners()))
	}

	// Expect 9 CDS clusters:
	// 2 inbound(http, inbound passthroughipv4) notes: no passthroughipv6
	// 9 outbound (2 http services, 1 tcp service,
	//   and 2 subsets of http1, 1 blackhole, 1 passthrough)
	if (len(adsc.GetClusters()) + len(adsc.GetEdsClusters())) != 9 {
		t.Fatalf("Expected 9 clusters in CDS output. Got %d", len(adsc.GetClusters())+len(adsc.GetEdsClusters()))
	}

	// Expect two vhost blocks in RDS output for 8080 (one for http1, another for http2)
	// plus one extra due to mem registry
	if len(adsc.GetRoutes()["8080"].VirtualHosts) != 3 {
		t.Fatalf("Expected 3 VirtualHosts in RDS output. Got %d", len(adsc.GetRoutes()["8080"].VirtualHosts))
	}
}

// TestLDS using gateways
func TestLDSWithIngressGateway(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadfolder(t, "tests/testdata/networking/ingress-gateway"),
		MeshConfig: func() *meshconfig.MeshConfig {
			m := mesh.DefaultMeshConfig()
			m.RootNamespace = "istio-config"
			return m
		}(),
	})
	labels := labels.Instance{"istio": "ingressgateway"}
	adsc := s.Connect(&model.Proxy{
		ConfigNamespace: "istio-system",
		Metadata:        &model.NodeMetadata{Labels: labels},
		IPAddresses:     []string{"99.1.1.1"},
		Type:            model.Router,
	}, nil, watchAll)

	// Expect 2 listeners : 1 for 80, 1 for 443
	// where 443 listener has 3 filter chains
	if (len(adsc.GetHTTPListeners()) + len(adsc.GetTCPListeners())) != 2 {
		t.Fatalf("Expected 2 listeners, got %d\n", len(adsc.GetHTTPListeners())+len(adsc.GetTCPListeners()))
	}

	// TODO: This is flimsy. The ADSC code treats any listener with http connection manager as a HTTP listener
	// instead of looking at it as a listener with multiple filter chains
	l := adsc.GetHTTPListeners()["0.0.0.0_443"]

	if l != nil {
		if len(l.FilterChains) != 3 {
			t.Fatalf("Expected 3 filter chains, got %d\n", len(l.FilterChains))
		}
	}
}

// TestLDS is running LDS tests.
func TestLDS(t *testing.T) {
	t.Run("sidecar", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
		ads := s.ConnectADS().WithType(v3.ListenerType)
		ads.RequestResponseAck(t, nil)
	})

	// 'router' or 'gateway' type of listener
	t.Run("gateway", func(t *testing.T) {
		s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{ConfigString: mustReadfolder(t, "tests/testdata/config")})
		// Matches Gateway config in test data
		labels := map[string]string{"version": "v2", "app": "my-gateway-controller"}
		ads := s.ConnectADS().WithType(v3.ListenerType).WithID(gatewayID(gatewayIP))
		ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
			Node: &core.Node{
				Id:       ads.ID,
				Metadata: model.NodeMetadata{Labels: labels}.ToStruct(),
			},
		})
	})
}

// TestLDS using sidecar scoped on workload without Service
func TestLDSWithSidecarForWorkloadWithoutService(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadfolder(t, "tests/testdata/networking/sidecar-without-service"),
		MeshConfig: func() *meshconfig.MeshConfig {
			m := mesh.DefaultMeshConfig()
			m.RootNamespace = "istio-config"
			return m
		}(),
	})
	labels := labels.Instance{"app": "consumeronly"}
	s.MemRegistry.AddWorkload("98.1.1.1", labels) // These labels must match the sidecars workload selector
	adsc := s.Connect(&model.Proxy{
		ConfigNamespace: "consumerns",
		Metadata:        &model.NodeMetadata{Labels: labels},
		IPAddresses:     []string{"98.1.1.1"},
	}, nil, watchAll)

	// Expect 2 HTTP listeners for outbound 8081 and one virtualInbound which has the same inbound 9080
	// as a filter chain. Since the adsclient code treats any listener with a HTTP connection manager filter in ANY
	// filter chain,  as a HTTP listener, we end up getting both 9080 and virtualInbound.
	if len(adsc.GetHTTPListeners()) != 2 {
		t.Fatalf("Expected 2 http listeners, got %d", len(adsc.GetHTTPListeners()))
	}

	// TODO: This is flimsy. The ADSC code treats any listener with http connection manager as a HTTP listener
	// instead of looking at it as a listener with multiple filter chains
	if l := adsc.GetHTTPListeners()["0.0.0.0_8081"]; l != nil {
		expected := 1
		if len(l.FilterChains) != expected {
			t.Fatalf("Expected %d filter chains, got %d", expected, len(l.FilterChains))
		}
	} else {
		t.Fatal("Expected listener for 0.0.0.0_8081")
	}

	if l := adsc.GetHTTPListeners()["virtualInbound"]; l == nil {
		t.Fatal("Expected listener virtualInbound")
	}

	// Expect only one eds cluster for http1.ns1.svc.cluster.local
	if len(adsc.GetEdsClusters()) != 1 {
		t.Fatalf("Expected 1 eds cluster, got %d", len(adsc.GetEdsClusters()))
	}
	if cluster, ok := adsc.GetEdsClusters()["outbound|8081||http1.ns1.svc.cluster.local"]; !ok {
		t.Fatalf("Expected eds cluster outbound|8081||http1.ns1.svc.cluster.local, got %v", cluster.Name)
	}
}

// TestLDS using default sidecar in root namespace
func TestLDSEnvoyFilterWithWorkloadSelector(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadfolder(t, "tests/testdata/networking/envoyfilter-without-service"),
	})
	// The labels of 98.1.1.1 must match the envoyfilter workload selector
	s.MemRegistry.AddWorkload("98.1.1.1", labels.Instance{"app": "envoyfilter-test-app", "some": "otherlabel"})
	s.MemRegistry.AddWorkload("98.1.1.2", labels.Instance{"app": "no-envoyfilter-test-app"})
	s.MemRegistry.AddWorkload("98.1.1.3", labels.Instance{})

	tests := []struct {
		name            string
		ip              string
		labels          labels.Instance
		expectLuaFilter bool
	}{
		{
			name:            "Add filter with matching labels to sidecar",
			ip:              "98.1.1.1",
			labels:          labels.Instance{"app": "envoyfilter-test-app", "some": "otherlabel"},
			expectLuaFilter: true,
		},
		{
			name:            "Ignore filter with not matching labels to sidecar",
			ip:              "98.1.1.2",
			labels:          labels.Instance{"app": "no-envoyfilter-test-app"},
			expectLuaFilter: false,
		},
		{
			name:            "Ignore filter with empty labels to sidecar",
			ip:              "98.1.1.3",
			labels:          labels.Instance{},
			expectLuaFilter: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			adsc := s.Connect(&model.Proxy{
				ConfigNamespace: "consumerns",
				Metadata:        &model.NodeMetadata{Labels: test.labels},
				IPAddresses:     []string{test.ip},
			}, nil, watchAll)

			// Expect 1 HTTP listeners for 8081
			if len(adsc.GetHTTPListeners()) != 1 {
				t.Fatalf("Expected 2 http listeners, got %v", xdstest.MapKeys(adsc.GetHTTPListeners()))
			}
			// TODO: This is flimsy. The ADSC code treats any listener with http connection manager as a HTTP listener
			// instead of looking at it as a listener with multiple filter chains
			l := adsc.GetHTTPListeners()["0.0.0.0_8081"]

			expectLuaFilter(t, l, test.expectLuaFilter)
		})
	}
}

func expectLuaFilter(t *testing.T, l *listener.Listener, expected bool) {
	t.Helper()
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
		err := httpCfg.TypedConfig.UnmarshalTo(&connectionManagerCfg)
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
