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

package core

import (
	"strings"
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

const (
	rdsTestNS     = "default"
	rdsEchoHost   = "echo.default.svc.cluster.local"
	rdsEchoV1Host = "echo-v1.default.svc.cluster.local"
	rdsEchoV2Host = "echo-v2.default.svc.cluster.local"
	rdsEchoChain  = "inbound-vip|80|http|echo.default.svc.cluster.local"
	rdsEchoV1Cl   = "inbound-vip|80|http|echo-v1.default.svc.cluster.local"
	rdsEchoV2Cl   = "inbound-vip|80|http|echo-v2.default.svc.cluster.local"
)

func TestParseWaypointServiceChainName(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantHost host.Name
		wantPort int
		wantOK   bool
	}{
		{"service http chain", rdsEchoChain, rdsEchoHost, 80, true},
		{"other port", "inbound-vip|8443|http|echo.default.svc.cluster.local", rdsEchoHost, 8443, true},
		{"tcp chain is not a route table", "inbound-vip|80|tcp|echo.default.svc.cluster.local", "", 0, false},
		{"sidecar inbound cluster", "inbound|80||", "", 0, false},
		{"sidecar outbound cluster", "outbound|80||echo.default.svc.cluster.local", "", 0, false},
		{"direct-to-pod encap table", "encap", "", 0, false},
		{"hbone terminator table", "default", "", 0, false},
		{"port zero", "inbound-vip|0|http|echo.default.svc.cluster.local", "", 0, false},
		{"port not a number", "inbound-vip|http|http|echo.default.svc.cluster.local", "", 0, false},
		{"empty hostname", "inbound-vip|80|http|", "", 0, false},
		{"prefix only", "inbound-vip|", "", 0, false},
		{"empty", "", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotHost, gotPort, gotOK := parseWaypointServiceChainName(tc.in)
			if gotOK != tc.wantOK || gotHost != tc.wantHost || gotPort != tc.wantPort {
				t.Fatalf("parseWaypointServiceChainName(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tc.in, gotHost, gotPort, gotOK, tc.wantHost, tc.wantPort, tc.wantOK)
			}
		})
	}
}

func TestWaypointServiceChainUsesRDS(t *testing.T) {
	svc := rdsTestService(rdsEchoHost, "echo", "10.0.0.1")
	cases := []struct {
		name  string
		flag  bool
		svc   *model.Service
		chain string
		want  bool
	}{
		{"flag off", false, svc, rdsEchoChain, false},
		{"flag on, service http chain", true, svc, rdsEchoChain, true},
		{"flag on, no service (direct-to-pod chain)", true, nil, EncapClusterName, false},
		{"flag on, service but tcp chain", true, svc, "inbound-vip|80|tcp|echo.default.svc.cluster.local", false},
		{"flag on, service but not a chain name", true, svc, "bogus", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableWaypointRDS, tc.flag)
			if got := waypointServiceChainUsesRDS(tc.svc, tc.chain); got != tc.want {
				t.Fatalf("waypointServiceChainUsesRDS = %v, want %v", got, tc.want)
			}
		})
	}
}

// With the flag on, Service HTTP chains reference their route table by RDS name and the direct pod
// access chain stays inline. With the flag off everything is inline.
func TestWaypointListenerRouteSource(t *testing.T) {
	for _, flag := range []bool{false, true} {
		t.Run(map[bool]string{false: "inline", true: "rds"}[flag], func(t *testing.T) {
			test.SetForTest(t, &features.EnableWaypointRDS, flag)
			cg, proxy, svcs := setupWaypointRDSTest(t, rdsTestVirtualService(80, 20))
			lb := NewListenerBuilder(proxy, cg.PushContext())
			l := lb.buildWaypointInternal(nil, svcs)
			if l == nil {
				t.Fatal("expected main_internal listener")
			}
			seenServiceChains, seenDirect := 0, false
			for _, fc := range l.FilterChains {
				h := rdsTestHCM(t, fc)
				if h == nil {
					continue // tcp chains
				}
				switch {
				case fc.Name == "direct-http":
					seenDirect = true
					if h.GetRds() != nil || h.GetRouteConfig() == nil || h.GetRouteConfig().Name != EncapClusterName {
						t.Fatalf("direct-http must always keep its inline %q table, got rds=%v inline=%v",
							EncapClusterName, h.GetRds(), h.GetRouteConfig())
					}
				case strings.HasPrefix(fc.Name, "inbound-vip|"):
					seenServiceChains++
					if flag {
						if h.GetRds() == nil || h.GetRds().RouteConfigName != fc.Name || h.GetRouteConfig() != nil {
							t.Fatalf("chain %s: want rds name == chain name and no inline table, got rds=%v inline=%v",
								fc.Name, h.GetRds(), h.GetRouteConfig())
						}
					} else if h.GetRds() != nil || h.GetRouteConfig() == nil || h.GetRouteConfig().Name != fc.Name {
						t.Fatalf("chain %s: want inline table named after the chain, got rds=%v inline=%v",
							fc.Name, h.GetRds(), h.GetRouteConfig())
					}
				}
			}
			if seenServiceChains != 3 || !seenDirect {
				t.Fatalf("expected 3 service http chains and the direct-http chain, got %d / %v", seenServiceChains, seenDirect)
			}
			// The listener should subscribe to exactly the Service chain route names.
			want := sets.New[string]()
			if flag {
				want = sets.New(rdsEchoChain, rdsEchoV1Cl, rdsEchoV2Cl)
			}
			assert.Equal(t, sets.New(ExtractRoutesFromListeners([]*listener.Listener{l})...), want)
		})
	}
}

// The RouteConfiguration served over RDS must match the one that is otherwise inlined.
func TestWaypointRDSRouteConfigMatchesInline(t *testing.T) {
	cg, proxy, svcs := setupWaypointRDSTest(t, rdsTestVirtualService(80, 20))
	echo := svcs[0]
	port, _ := echo.Ports.GetByPort(80)

	test.SetForTest(t, &features.EnableWaypointRDS, false)
	inline := buildWaypointInboundHTTPRouteConfig(NewListenerBuilder(proxy, cg.PushContext()), echo,
		newWaypointInboundChainConfig(echo, port, rdsEchoChain))

	test.SetForTest(t, &features.EnableWaypointRDS, true)
	viaRDS := rdsTestBuildRoute(t, cg, proxy, rdsEchoChain)

	assert.Equal(t, viaRDS, inline)
	assert.Equal(t, rdsTestEffectiveWeights(t, viaRDS), map[string]uint32{rdsEchoV1Host: 80, rdsEchoV2Host: 20})
}

// A VirtualService weight change is reflected in the RDS table.
func TestWaypointRDSRouteConfigFollowsVirtualService(t *testing.T) {
	test.SetForTest(t, &features.EnableWaypointRDS, true)
	cases := []struct {
		name     string
		v1, v2   int32
		wantHost string
	}{
		{"all to v1", 100, 0, rdsEchoV1Host},
		{"all to v2", 0, 100, rdsEchoV2Host},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cg, proxy, _ := setupWaypointRDSTest(t, rdsTestVirtualService(tc.v1, tc.v2))
			rc := rdsTestBuildRoute(t, cg, proxy, rdsEchoChain)
			assert.Equal(t, rdsTestEffectiveWeights(t, rc), map[string]uint32{tc.wantHost: 100})
		})
	}
}

// Names not handled by the waypoint builder fall through to the generic path, which still returns a
// (possibly empty) RouteConfiguration so the listener can warm.
func TestWaypointRDSFallbacks(t *testing.T) {
	test.SetForTest(t, &features.EnableWaypointRDS, true)
	cg, proxy, _ := setupWaypointRDSTest(t, rdsTestVirtualService(100, 0))
	push := cg.PushContext()

	t.Run("unknown service -> empty table", func(t *testing.T) {
		rc := rdsTestBuildRoute(t, cg, proxy, "inbound-vip|80|http|nope.default.svc.cluster.local")
		if len(rc.VirtualHosts) != 0 {
			t.Fatalf("expected empty table, got %v", rc.VirtualHosts)
		}
	})
	t.Run("known service, unknown port -> empty table", func(t *testing.T) {
		rc := rdsTestBuildRoute(t, cg, proxy, "inbound-vip|81|http|echo.default.svc.cluster.local")
		if len(rc.VirtualHosts) != 0 {
			t.Fatalf("expected empty table, got %v", rc.VirtualHosts)
		}
	})
	t.Run("tcp chain name is not handled", func(t *testing.T) {
		if r := cg.ConfigGen.buildWaypointRouteResource(proxy, push, "inbound-vip|80|tcp|echo.default.svc.cluster.local"); r != nil {
			t.Fatalf("expected nil, got %v", r)
		}
	})
	t.Run("sidecar proxy is not handled", func(t *testing.T) {
		sidecar := cg.SetupProxy(&model.Proxy{Type: model.SidecarProxy, ConfigNamespace: rdsTestNS, IPAddresses: []string{"10.0.0.200"}})
		if r := cg.ConfigGen.buildWaypointRouteResource(sidecar, push, rdsEchoChain); r != nil {
			t.Fatalf("expected nil for a sidecar, got %v", r)
		}
	})
	t.Run("flag off is not handled", func(t *testing.T) {
		test.SetForTest(t, &features.EnableWaypointRDS, false)
		if r := cg.ConfigGen.buildWaypointRouteResource(proxy, push, rdsEchoChain); r != nil {
			t.Fatalf("expected nil with the flag off, got %v", r)
		}
	})
}

// ---- helpers ----

func rdsTestService(hostname, name, vip string) *model.Service {
	svc := &model.Service{
		Hostname:       host.Name(hostname),
		DefaultAddress: vip,
		Attributes:     model.ServiceAttributes{Name: name, Namespace: rdsTestNS},
		Ports:          model.PortList{{Name: "http", Port: 80, Protocol: protocol.HTTP}},
	}
	svc.ClusterVIPs.SetAddressesFor("cluster-1", []string{vip})
	return svc
}

func rdsTestVirtualService(v1Weight, v2Weight int32) config.Config {
	return config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "echo", Namespace: rdsTestNS},
		Spec: &networking.VirtualService{
			Hosts: []string{rdsEchoHost},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{
					{Destination: &networking.Destination{Host: rdsEchoV1Host}, Weight: v1Weight},
					{Destination: &networking.Destination{Host: rdsEchoV2Host}, Weight: v2Weight},
				},
			}},
		},
	}
}

func setupWaypointRDSTest(t *testing.T, configs ...config.Config) (*ConfigGenTest, *model.Proxy, []*model.Service) {
	t.Helper()
	svcs := []*model.Service{
		rdsTestService(rdsEchoHost, "echo", "10.0.0.1"),
		rdsTestService(rdsEchoV1Host, "echo-v1", "10.0.0.2"),
		rdsTestService(rdsEchoV2Host, "echo-v2", "10.0.0.3"),
	}
	cg := NewConfigGenTest(t, TestOptions{Services: svcs, Configs: configs})
	proxy := cg.SetupProxy(&model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: rdsTestNS,
		Metadata:        &model.NodeMetadata{ClusterID: "cluster-1"},
		IPAddresses:     []string{"10.0.0.100"},
	})
	return cg, proxy, svcs
}

func rdsTestHCM(t *testing.T, fc *listener.FilterChain) *hcm.HttpConnectionManager {
	t.Helper()
	for _, f := range fc.Filters {
		if f.Name != "envoy.filters.network.http_connection_manager" {
			continue
		}
		h := &hcm.HttpConnectionManager{}
		if err := f.GetTypedConfig().UnmarshalTo(h); err != nil {
			t.Fatal(err)
		}
		return h
	}
	return nil
}

func rdsTestBuildRoute(t *testing.T, cg *ConfigGenTest, proxy *model.Proxy, name string) *route.RouteConfiguration {
	t.Helper()
	resources, _ := cg.ConfigGen.BuildHTTPRoutes(proxy, &model.PushRequest{Push: cg.PushContext()}, []string{name})
	if len(resources) != 1 || resources[0].Name != name {
		t.Fatalf("expected exactly one RouteConfiguration named %q, got %v", name, resources)
	}
	rc := &route.RouteConfiguration{}
	if err := resources[0].Resource.UnmarshalTo(rc); err != nil {
		t.Fatal(err)
	}
	return rc
}

// rdsTestEffectiveWeights returns destination hostname -> weight for the first route, treating a single
// cluster as weight 100 and dropping zero-weight entries.
func rdsTestEffectiveWeights(t *testing.T, rc *route.RouteConfiguration) map[string]uint32 {
	t.Helper()
	if len(rc.VirtualHosts) != 1 || len(rc.VirtualHosts[0].Routes) == 0 {
		t.Fatalf("expected one virtual host with routes, got %v", rc)
	}
	ra := rc.VirtualHosts[0].Routes[0].GetRoute()
	out := map[string]uint32{}
	if c := ra.GetCluster(); c != "" {
		out[model.ParseSubsetKeyHostname(c)] = 100
		return out
	}
	for _, c := range ra.GetWeightedClusters().GetClusters() {
		if w := c.Weight.GetValue(); w > 0 {
			out[model.ParseSubsetKeyHostname(c.Name)] = w
		}
	}
	return out
}
