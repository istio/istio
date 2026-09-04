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

package xds

import (
	"fmt"
	"testing"

	envoycore "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/api/label"
	mesh "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestWorkloadSubscriberNeedsAddressPush(t *testing.T) {
	test.SetForTest(t, &features.ScopedAddressPushes, true)
	push := core.NewConfigGenTest(t, core.TestOptions{}).PushContext()
	const addr = "Kubernetes//Pod/default/x"
	for _, nodeType := range []model.NodeType{model.Router, model.SidecarProxy} {
		for _, subscribed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/subscribed=%v", nodeType, subscribed), func(t *testing.T) {
				proxy := &model.Proxy{
					Type:             nodeType,
					Metadata:         &model.NodeMetadata{},
					WatchedResources: map[string]*model.WatchedResource{},
				}
				if subscribed {
					proxy.NewWatchedResource(v3.WorkloadType, nil)
				}
				req := &model.PushRequest{
					Push:             push,
					AddressesUpdated: sets.New(addr),
					ConfigsUpdated:   sets.New(model.ConfigKey{Kind: kind.Address, Name: addr}),
				}
				filtered, needsPush := DefaultProxyNeedsPush(proxy, req)
				assert.Equal(t, needsPush, subscribed)
				assert.Equal(t, len(filtered.ConfigsUpdated), 0)
				assert.Equal(t, filtered.AddressesUpdated, sets.New(addr))
				assert.Equal(t, req.ConfigsUpdated, sets.New(model.ConfigKey{Kind: kind.Address, Name: addr}))

				req.AddressesUpdated = nil
				_, needsPush = DefaultProxyNeedsPush(proxy, req)
				assert.Equal(t, needsPush, false)
			})
		}
	}
}

func TestGatewayScopeDependencies(t *testing.T) {
	const gatewayNamespace = "gateway"
	const serviceNamespace = "backend"
	const rootNamespace = "root"
	const serviceHost = "backend.example.com"
	service := &model.Service{
		Hostname: serviceHost,
		Attributes: model.ServiceAttributes{
			Namespace: serviceNamespace,
			ExportTo:  sets.New(visibility.Public),
		},
	}
	destinationRule := config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.DestinationRule, Name: "dr", Namespace: serviceNamespace},
		Spec: &networking.DestinationRule{Host: serviceHost},
	}
	peerAuthentication := config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.PeerAuthentication, Name: "pa", Namespace: serviceNamespace},
		Spec: &security.PeerAuthentication{},
	}
	old := core.NewConfigGenTest(t, core.TestOptions{
		Services:   []*model.Service{service},
		Configs:    []config.Config{destinationRule, peerAuthentication},
		MeshConfig: &mesh.MeshConfig{RootNamespace: rootNamespace},
	})
	current := core.NewConfigGenTest(t, core.TestOptions{
		Services:   []*model.Service{service},
		MeshConfig: &mesh.MeshConfig{RootNamespace: rootNamespace},
	})
	proxy := &model.Proxy{Type: model.Router, ConfigNamespace: gatewayNamespace, Metadata: &model.NodeMetadata{}}
	proxy.SetSidecarScope(old.PushContext())
	proxy.SetSidecarScope(current.PushContext())

	t.Run("removed dependencies", func(t *testing.T) {
		for _, k := range []kind.Kind{kind.DestinationRule, kind.PeerAuthentication} {
			name := "dr"
			if k == kind.PeerAuthentication {
				name = "pa"
			}
			key := model.ConfigKey{Kind: k, Name: name, Namespace: serviceNamespace}
			assert.Equal(t, proxy.SidecarScope.DependsOnConfig(key, rootNamespace), false)
			assert.Equal(t, proxy.PrevSidecarScope.DependsOnConfig(key, rootNamespace), true)
			_, needsPush := DefaultProxyNeedsPush(proxy, &model.PushRequest{
				Push: current.PushContext(), ConfigsUpdated: sets.New(key),
			})
			assert.Equal(t, needsPush, true)
		}
	})

	t.Run("current dependencies", func(t *testing.T) {
		proxy := &model.Proxy{Type: model.Router, ConfigNamespace: gatewayNamespace, Metadata: &model.NodeMetadata{}}
		proxy.SetSidecarScope(old.PushContext())
		for _, cfg := range []config.Config{destinationRule, peerAuthentication} {
			_, needsPush := DefaultProxyNeedsPush(proxy, &model.PushRequest{
				Push: old.PushContext(), ConfigsUpdated: sets.New(model.ConfigKey{
					Kind: gvk.MustToKind(cfg.GroupVersionKind), Name: cfg.Name, Namespace: cfg.Namespace,
				}),
			})
			assert.Equal(t, needsPush, true)
		}
	})

	t.Run("service visibility", func(t *testing.T) {
		// Without filtered gateway clusters, ServiceEntry updates follow the scope's dependencies like
		// any other config: the visible service by hostname and namespace, not by hostname alone.
		test.SetForTest(t, &features.FilterGatewayClusterConfig, false)
		for _, tt := range []struct {
			key  model.ConfigKey
			want bool
		}{
			{model.ConfigKey{Kind: kind.ServiceEntry, Name: serviceHost, Namespace: serviceNamespace}, true},
			{model.ConfigKey{Kind: kind.ServiceEntry, Name: serviceHost, Namespace: "unrelated"}, false},
			{model.ConfigKey{Kind: kind.ServiceEntry, Name: "other.example.com", Namespace: serviceNamespace}, false},
		} {
			filtered, needsPush := DefaultProxyNeedsPush(proxy, &model.PushRequest{
				Push: current.PushContext(), ConfigsUpdated: sets.New(tt.key),
			})
			assert.Equal(t, needsPush, tt.want, tt.key.String())
			assert.Equal(t, filtered.ConfigsUpdated.Contains(tt.key), tt.want, tt.key.String())
		}
	})

	for _, k := range []kind.Kind{
		kind.EnvoyFilter, kind.RequestAuthentication, kind.AuthorizationPolicy,
		kind.Telemetry, kind.TrafficExtension, kind.WasmPlugin,
	} {
		for _, ns := range []string{rootNamespace, gatewayNamespace, "unrelated"} {
			t.Run(k.String()+"/"+ns, func(t *testing.T) {
				key := model.ConfigKey{Kind: k, Name: "policy", Namespace: ns}
				req := &model.PushRequest{Push: current.PushContext(), ConfigsUpdated: sets.New(key)}
				filtered, needsPush := DefaultProxyNeedsPush(proxy, req)
				want := ns != "unrelated"
				assert.Equal(t, needsPush, want)
				assert.Equal(t, filtered.ConfigsUpdated.Contains(key), want)
				assert.Equal(t, req.ConfigsUpdated.Contains(key), true)
			})
		}
	}

	for _, k := range []kind.Kind{kind.DestinationRule, kind.PeerAuthentication, kind.VirtualService, kind.Gateway} {
		t.Run(k.String(), func(t *testing.T) {
			_, needsPush := DefaultProxyNeedsPush(proxy, &model.PushRequest{
				Push: current.PushContext(), ConfigsUpdated: sets.New(model.ConfigKey{
					Kind: k, Name: "unrelated", Namespace: "unrelated",
				}),
			})
			assert.Equal(t, needsPush, k == kind.Gateway)
		})
	}

	t.Run("service update merged with unrelated filter", func(t *testing.T) {
		serviceKey := model.ConfigKey{Kind: kind.ServiceEntry, Name: serviceHost, Namespace: serviceNamespace}
		filterKey := model.ConfigKey{Kind: kind.EnvoyFilter, Name: "filter", Namespace: "unrelated"}
		for _, forced := range []bool{false, true} {
			req := &model.PushRequest{
				Push: current.PushContext(), ConfigsUpdated: sets.New(serviceKey, filterKey), Forced: forced,
			}
			filtered, needsPush := DefaultProxyNeedsPush(proxy, req)
			assert.Equal(t, needsPush, true)
			assert.Equal(t, filtered.ConfigsUpdated.Contains(serviceKey), true)
			assert.Equal(t, filtered.ConfigsUpdated.Contains(filterKey), forced)
			assert.Equal(t, req.ConfigsUpdated.Contains(filterKey), true)
		}
	})
}

func TestGatewayVirtualServiceDependencies(t *testing.T) {
	for _, change := range []string{"rebound", "deleted", "hidden"} {
		t.Run(change, func(t *testing.T) {
			route := config.Config{
				Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "route", Namespace: "routes"},
				Spec: &networking.VirtualService{Hosts: []string{"example.com"}, Gateways: []string{"gateways/a"}},
			}
			old := core.NewConfigGenTest(t, core.TestOptions{Configs: []config.Config{route}})
			var updated []config.Config
			if change != "deleted" {
				route = route.DeepCopy()
				if change == "rebound" {
					route.Spec.(*networking.VirtualService).Gateways = []string{"gateways/b"}
				} else {
					route.Spec.(*networking.VirtualService).ExportTo = []string{"."}
				}
				updated = []config.Config{route}
			}
			current := core.NewConfigGenTest(t, core.TestOptions{Configs: updated})
			key := model.ConfigKey{Kind: kind.VirtualService, Name: "route", Namespace: "routes"}
			for _, gateway := range []string{"a", "b", "unrelated"} {
				names := []string{"gateways/" + gateway}
				proxy := &model.Proxy{
					Type: model.Router, ConfigNamespace: "proxy", Metadata: &model.NodeMetadata{},
					MergedGateway: &model.MergedGateway{GatewayNames: names, GatewayScopeKey: model.NewGatewayScopeKey(model.Router, "proxy", names)},
				}
				proxy.SetSidecarScope(old.PushContext())
				proxy.SetSidecarScope(current.PushContext())
				rootNamespace := current.PushContext().Mesh.RootNamespace
				assert.Equal(t, proxy.SidecarScope.DependsOnConfig(key, rootNamespace), gateway == "b" && change == "rebound")
				assert.Equal(t, proxy.PrevSidecarScope.DependsOnConfig(key, rootNamespace), gateway == "a")
				filtered, needsPush := DefaultProxyNeedsPush(proxy, &model.PushRequest{
					Push: current.PushContext(), ConfigsUpdated: sets.New(key),
				})
				want := gateway == "a" || (gateway == "b" && change == "rebound")
				assert.Equal(t, needsPush, want)
				assert.Equal(t, filtered.ConfigsUpdated.Contains(key), want)
			}
		})
	}
}

func TestGatewaySidecarScopeReselection(t *testing.T) {
	test.SetForTest(t, &features.ScopeGatewayToNamespace, false)
	gateway := func(name string, ports ...uint32) config.Config {
		servers := make([]*networking.Server, 0, len(ports))
		for _, port := range ports {
			servers = append(servers, &networking.Server{
				Hosts: []string{"*"}, Port: &networking.Port{Number: port, Name: fmt.Sprint("http-", port), Protocol: "HTTP"},
			})
		}
		return config.Config{
			Meta: config.Meta{GroupVersionKind: gvk.Gateway, Name: name, Namespace: "gateways"},
			Spec: &networking.Gateway{Selector: map[string]string{"app": "gateway"}, Servers: servers},
		}
	}
	proxyTypes := []struct {
		name   string
		typ    model.NodeType
		labels map[string]string
	}{
		{"router", model.Router, map[string]string{"app": "gateway"}},
		{"east-west", model.Waypoint, map[string]string{
			"app": "gateway", label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel,
		}},
	}
	cases := []struct {
		name            string
		gateways        []config.Config
		wantNames       sets.Set[string]
		wantReselection bool
	}{
		{"server count changed", []config.Config{gateway("a", 80, 81)}, sets.New("gateways/a"), false},
		{"gateway added", []config.Config{gateway("a", 80), gateway("b", 80)}, sets.New("gateways/a", "gateways/b"), true},
		{"gateway replaced", []config.Config{gateway("b", 80)}, sets.New("gateways/b"), true},
		{"gateway removed", nil, sets.New[string](), true},
	}
	for _, pt := range proxyTypes {
		for _, tt := range cases {
			t.Run(pt.name+"/"+tt.name, func(t *testing.T) {
				routes := []config.Config{
					{
						Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "a", Namespace: "routes"},
						Spec: &networking.VirtualService{Hosts: []string{"a.example.com"}, Gateways: []string{"gateways/a"}},
					},
					{
						Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "b", Namespace: "routes"},
						Spec: &networking.VirtualService{Hosts: []string{"b.example.com"}, Gateways: []string{"gateways/b"}},
					},
				}
				old := core.NewConfigGenTest(t, core.TestOptions{Configs: append([]config.Config{gateway("a", 80)}, routes...)})
				proxy := &model.Proxy{
					Type: pt.typ, ConfigNamespace: "proxy",
					Metadata: &model.NodeMetadata{Labels: pt.labels}, XdsNode: &envoycore.Node{}, LastPushContext: old.PushContext(),
				}
				server := &DiscoveryServer{Env: old.Env()}
				server.computeProxyState(proxy, nil)
				oldScope := proxy.SidecarScope
				rootNamespace := old.PushContext().Mesh.RootNamespace
				assert.Equal(t, oldScope.DependsOnConfig(model.ConfigKey{Kind: kind.VirtualService, Name: "a", Namespace: "routes"}, rootNamespace), true)
				assert.Equal(t, len(proxy.SidecarScope.GatewayVirtualServices("gateways/a")), 1)
				current := core.NewConfigGenTest(t, core.TestOptions{Configs: append(tt.gateways, routes...)})
				server.Env = current.Env()
				server.computeProxyState(proxy, &model.PushRequest{
					Push: current.PushContext(), ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Gateway, Name: "a", Namespace: "gateways"}),
				})
				assert.Equal(t, proxy.PrevMergedGateway.GatewayNames, []string{"gateways/a"})
				gatewayNames := proxy.MergedGateway.GetGatewayNames()
				assert.Equal(t, sets.New(gatewayNames...), tt.wantNames)
				assert.Equal(t, proxy.SidecarScope != oldScope, tt.wantReselection)
				for _, name := range []string{"a", "b"} {
					bound := tt.wantNames.Contains("gateways/" + name)
					assert.Equal(t, proxy.SidecarScope.DependsOnConfig(model.ConfigKey{Kind: kind.VirtualService, Name: name, Namespace: "routes"},
						rootNamespace), bound)
					// Generation reads gateway VirtualServices from the scope, which must follow the merged gateways.
					virtualServices := proxy.SidecarScope.GatewayVirtualServices("gateways/" + name)
					assert.Equal(t, len(virtualServices) == 1 && virtualServices[0].Name == name, bound)
				}
				key := model.ConfigKey{Kind: kind.VirtualService, Name: "a", Namespace: "routes"}
				if tt.wantReselection {
					assert.Equal(t, proxy.PrevSidecarScope == oldScope, true)
					assert.Equal(t, proxy.PrevSidecarScope.DependsOnConfig(key, rootNamespace), true)
				}
				// Removing a gateway reselects the scope, but its previously imported VirtualServices
				// must still be classified as dependencies through the previous scope.
				assert.Equal(t, proxyDependentOnConfig(proxy, key, current.PushContext()), true)
				filtered, needsPush := DefaultProxyNeedsPush(proxy, &model.PushRequest{
					Push: current.PushContext(), ConfigsUpdated: sets.New(key),
				})
				assert.Equal(t, needsPush, true)
				assert.Equal(t, filtered.ConfigsUpdated.Contains(key), true)
				_, needsPush = DefaultProxyNeedsPush(proxy, &model.PushRequest{
					Push: current.PushContext(), ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Gateway, Name: "a", Namespace: "gateways"}),
				})
				assert.Equal(t, needsPush, true)
			})
		}
	}
}

func TestGatewaySidecarScopeFromNoGateways(t *testing.T) {
	test.SetForTest(t, &features.ScopeGatewayToNamespace, false)
	gateway := config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.Gateway, Name: "a", Namespace: "gateways"},
		Spec: &networking.Gateway{Selector: map[string]string{"app": "gateway"}, Servers: []*networking.Server{{
			Hosts: []string{"*"}, Port: &networking.Port{Number: 80, Name: "http", Protocol: "HTTP"},
		}}},
	}
	route := config.Config{
		Meta: config.Meta{GroupVersionKind: gvk.VirtualService, Name: "a", Namespace: "routes"},
		Spec: &networking.VirtualService{Hosts: []string{"a.example.com"}, Gateways: []string{"gateways/a"}},
	}
	key := model.ConfigKey{Kind: kind.VirtualService, Name: "a", Namespace: "routes"}
	for _, pt := range []struct {
		name   string
		typ    model.NodeType
		labels map[string]string
	}{
		{"router", model.Router, map[string]string{"app": "gateway"}},
		{"east-west", model.Waypoint, map[string]string{
			"app": "gateway", label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel,
		}},
	} {
		t.Run(pt.name, func(t *testing.T) {
			// The proxy connects before any Gateway selects it, so it has no merged gateway and
			// sits on the namespace scope, which neither depends on nor serves gateway routes.
			old := core.NewConfigGenTest(t, core.TestOptions{Configs: []config.Config{route}})
			proxy := &model.Proxy{
				Type: pt.typ, ConfigNamespace: "proxy",
				Metadata: &model.NodeMetadata{Labels: pt.labels}, XdsNode: &envoycore.Node{}, LastPushContext: old.PushContext(),
			}
			server := &DiscoveryServer{Env: old.Env()}
			server.computeProxyState(proxy, nil)
			assert.Equal(t, proxy.MergedGateway == nil, true)
			baseScope := proxy.SidecarScope
			assert.Equal(t, baseScope.DependsOnConfig(key, old.PushContext().Mesh.RootNamespace), false)
			assert.Equal(t, len(proxy.SidecarScope.GatewayVirtualServices("gateways/a")), 0)
			_, needsPush := DefaultProxyNeedsPush(proxy, &model.PushRequest{Push: old.PushContext(), ConfigsUpdated: sets.New(key)})
			assert.Equal(t, needsPush, pt.typ != model.Router)

			// Creating the Gateway moves the proxy from no gateways to {a}. That alone must reselect
			// the scope, or generation would read an empty gateway VirtualService list.
			current := core.NewConfigGenTest(t, core.TestOptions{Configs: []config.Config{gateway, route}})
			server.Env = current.Env()
			server.computeProxyState(proxy, &model.PushRequest{
				Push: current.PushContext(), ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Gateway, Name: "a", Namespace: "gateways"}),
			})
			assert.Equal(t, proxy.MergedGateway.GetGatewayNames(), []string{"gateways/a"})
			assert.Equal(t, len(proxy.PrevMergedGateway.GetGatewayNames()), 0)
			assert.Equal(t, proxy.SidecarScope != baseScope, true)
			assert.Equal(t, proxy.PrevSidecarScope == baseScope, true)
			assert.Equal(t, proxy.SidecarScope.DependsOnConfig(key, current.PushContext().Mesh.RootNamespace), true)
			virtualServices := proxy.SidecarScope.GatewayVirtualServices("gateways/a")
			assert.Equal(t, len(virtualServices), 1)
			assert.Equal(t, virtualServices[0].Name, "a")
			assert.Equal(t, proxyDependentOnConfig(proxy, key, current.PushContext()), true)
			_, needsPush = DefaultProxyNeedsPush(proxy, &model.PushRequest{Push: current.PushContext(), ConfigsUpdated: sets.New(key)})
			assert.Equal(t, needsPush, true)
		})
	}
}

func TestProxyNeedsPush(t *testing.T) {
	const (
		svcName        = "svc1.com"
		privateSvcName = "private.com"
		drName         = "dr1"
		vsName         = "vs1"
		scName         = "sc1"
		nsName         = "ns1"
		nsRoot         = "rootns"
		generalName    = "name1"

		invalidNameSuffix = "invalid"
	)

	type Case struct {
		name        string
		proxy       *model.Proxy
		configs     sets.Set[model.ConfigKey]
		forced      bool
		want        bool
		wantConfigs sets.Set[model.ConfigKey]
	}

	sidecar := &model.Proxy{
		Type: model.SidecarProxy, IPAddresses: []string{"127.0.0.1"}, Metadata: &model.NodeMetadata{},
		SidecarScope: &model.SidecarScope{Name: generalName, Namespace: nsName},
	}
	gateway := &model.Proxy{
		Type:            model.Router,
		ConfigNamespace: nsName,
		Metadata:        &model.NodeMetadata{Namespace: nsName},
		Labels:          map[string]string{"gateway": "gateway"},
	}
	// A sidecar-type proxy subscribed to Workload Address resources (e.g. WDS on-demand clients)
	// must keep receiving Address pushes.
	workloadClient := &model.Proxy{
		Type: model.SidecarProxy, IPAddresses: []string{"127.0.0.2"}, Metadata: &model.NodeMetadata{},
		SidecarScope:     &model.SidecarScope{Name: generalName, Namespace: nsName},
		WatchedResources: map[string]*model.WatchedResource{},
	}
	workloadClient.NewWatchedResource(v3.AddressType, nil)

	sidecarScopeKindNames := map[kind.Kind]string{
		kind.ServiceEntry: svcName, kind.VirtualService: vsName, kind.DestinationRule: drName, kind.Sidecar: scName,
	}
	for kind, name := range sidecarScopeKindNames {
		sidecar.SidecarScope.AddConfigDependencies(model.ConfigKey{Kind: kind, Name: name, Namespace: nsName}.HashCode())
	}
	for kind := range UnAffectedConfigKinds[model.SidecarProxy] {
		sidecar.SidecarScope.AddConfigDependencies(model.ConfigKey{
			Kind:      kind,
			Name:      generalName,
			Namespace: nsName,
		}.HashCode())
	}

	cases := []Case{
		{"no namespace or configs", sidecar, nil, false, false, nil},
		{"forced push with no namespace or configs", sidecar, nil, true, true, nil},
		{
			"gateway config for sidecar", sidecar,
			sets.New(model.ConfigKey{Kind: kind.Gateway, Name: generalName, Namespace: nsName}),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"gateway config for gateway", gateway,
			sets.New(model.ConfigKey{Kind: kind.Gateway, Name: generalName, Namespace: nsName}),
			false,
			true,
			sets.New(model.ConfigKey{Kind: kind.Gateway, Name: generalName, Namespace: nsName}),
		},
		{
			"sidecar config for gateway", gateway, sets.New(model.ConfigKey{Kind: kind.Sidecar, Name: scName, Namespace: nsName}),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"invalid config for sidecar", sidecar,
			sets.New(model.ConfigKey{Kind: kind.Kind(255), Name: generalName, Namespace: nsName}),
			false,
			true,
			sets.New(model.ConfigKey{Kind: kind.Kind(255), Name: generalName, Namespace: nsName}),
		},
		{
			"mixture matched and unmatched config for sidecar",
			sidecar,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName, Namespace: nsName},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName + invalidNameSuffix, Namespace: nsName},
			),
			false,
			true,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName, Namespace: nsName},
			),
		},
		{
			"mixture unmatched and unmatched config for sidecar",
			sidecar,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName + invalidNameSuffix, Namespace: nsName},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName + invalidNameSuffix, Namespace: nsName},
			),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"forced push with mixture unmatched and unmatched config for sidecar",
			sidecar,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName + invalidNameSuffix, Namespace: nsName},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName + invalidNameSuffix, Namespace: nsName},
			),
			true,
			true,
			sets.New(
				model.ConfigKey{Kind: kind.DestinationRule, Name: drName + invalidNameSuffix, Namespace: nsName},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName + invalidNameSuffix, Namespace: nsName},
			),
		},
		{
			"address config for sidecar", sidecar,
			sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"address config for workload-subscribed sidecar", workloadClient,
			sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
			false,
			true,
			sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
		},
		{
			"address config for gateway", gateway,
			sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
			false,
			false,
			sets.New[model.ConfigKey](),
		},
		{
			"empty configsUpdated for sidecar",
			sidecar,
			nil,
			false,
			false,
			nil,
		},
		{
			"forced push with empty configsUpdated for sidecar",
			sidecar,
			nil,
			true,
			true,
			nil,
		},
	}

	for k, name := range sidecarScopeKindNames {
		cases = append(cases, Case{ // valid name
			name:        fmt.Sprintf("%s config for sidecar", k.String()),
			proxy:       sidecar,
			configs:     sets.New(model.ConfigKey{Kind: k, Name: name, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: k, Name: name, Namespace: nsName}),
		}, Case{ // invalid name
			name:        fmt.Sprintf("%s unmatched config for sidecar", k.String()),
			proxy:       sidecar,
			configs:     sets.New(model.ConfigKey{Kind: k, Name: name + invalidNameSuffix, Namespace: nsName}),
			want:        false,
			wantConfigs: sets.New[model.ConfigKey](),
		})
	}

	sidecarNamespaceScopeTypes := []kind.Kind{
		kind.EnvoyFilter, kind.AuthorizationPolicy, kind.RequestAuthentication, kind.WasmPlugin, kind.TrafficExtension,
	}
	for _, k := range sidecarNamespaceScopeTypes {
		cases = append(
			cases,
			Case{
				name:        fmt.Sprintf("%s config for sidecar in same namespace", k.String()),
				proxy:       sidecar,
				configs:     sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: nsName}),
				want:        true,
				wantConfigs: sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: nsName}),
			},
			Case{
				name:        fmt.Sprintf("%s config for sidecar in different namespace", k.String()),
				proxy:       sidecar,
				configs:     sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: "invalid-namespace"}),
				want:        false,
				wantConfigs: sets.New[model.ConfigKey](),
			},
			Case{
				name:        fmt.Sprintf("%s config in the root namespace", k.String()),
				proxy:       sidecar,
				configs:     sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: nsRoot}),
				want:        true,
				wantConfigs: sets.New(model.ConfigKey{Kind: k, Name: generalName, Namespace: nsRoot}),
			},
		)
	}

	// tests for kind-affect-proxy.
	for _, nodeType := range []model.NodeType{model.Router, model.SidecarProxy} {
		proxy := gateway
		if nodeType == model.SidecarProxy {
			proxy = sidecar
		}
		for k := range UnAffectedConfigKinds[proxy.Type] {
			cases = append(cases, Case{
				name:        fmt.Sprintf("kind %s not affect %s", k.String(), nodeType),
				proxy:       proxy,
				configs:     sets.New(model.ConfigKey{Kind: k, Name: generalName + invalidNameSuffix, Namespace: nsName}),
				want:        false,
				wantConfigs: sets.New[model.ConfigKey](),
			})
		}
	}

	// test for gateway proxy dependencies.
	cg := core.NewConfigGenTest(t, core.TestOptions{
		Services: []*model.Service{
			{
				Hostname: svcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: privateSvcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.None),
					Namespace: nsName,
				},
			},
			{
				Hostname: "foo",
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
		},
	})
	gateway.SetSidecarScope(cg.PushContext())

	// service visibility updated
	cg = core.NewConfigGenTest(t, core.TestOptions{
		Services: []*model.Service{
			{
				Hostname: svcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: privateSvcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.None),
					Namespace: nsName,
				},
			},
			{
				Hostname: "foo",
				Attributes: model.ServiceAttributes{
					// service visibility changed from public to none
					ExportTo:  sets.New(visibility.None),
					Namespace: nsName,
				},
			},
		},
	})
	gateway.SetSidecarScope(cg.PushContext())

	cases = append(
		cases,
		Case{
			name:        "service with public visibility for gateway",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName, Namespace: nsName}),
		},
		Case{
			name:        "service with none visibility for gateway",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: privateSvcName, Namespace: nsName}),
			want:        false,
			wantConfigs: sets.New[model.ConfigKey](),
		},
		Case{
			name:        "service visibility changed from public to none",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "foo", Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "foo", Namespace: nsName}),
		},
	)

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cg.PushContext().Mesh.RootNamespace = nsRoot
			newReq, got := DefaultProxyNeedsPush(tt.proxy, &model.PushRequest{ConfigsUpdated: tt.configs, Push: cg.PushContext(), Forced: tt.forced})
			if got != tt.want {
				t.Fatalf("Got needs push = %v, expected %v", got, tt.want)
			}
			if tt.wantConfigs == nil && newReq.ConfigsUpdated != nil {
				t.Fatalf("Got configs updated = %v, expected none", newReq.ConfigsUpdated)
			}
			if tt.wantConfigs != nil && !tt.wantConfigs.Equals(newReq.ConfigsUpdated) {
				t.Fatalf("Got configs updated = %v, expected %v", newReq.ConfigsUpdated, tt.wantConfigs)
			}
		})
	}

	// test for gateway proxy dependencies with PILOT_FILTER_GATEWAY_CLUSTER_CONFIG enabled.
	test.SetForTest(t, &features.FilterGatewayClusterConfig, true)
	test.SetForTest(t, &features.JwksFetchMode, jwt.Envoy)

	const (
		fooSvc       = "foo"
		extensionSvc = "extension"
		jwksSvc      = "jwks"
	)

	cg = core.NewConfigGenTest(t, core.TestOptions{
		Services: []*model.Service{
			{
				Hostname: fooSvc,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: svcName,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: extensionSvc,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
			{
				Hostname: jwksSvc,
				Attributes: model.ServiceAttributes{
					ExportTo:  sets.New(visibility.Public),
					Namespace: nsName,
				},
			},
		},
		Configs: []config.Config{
			{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             svcName,
					Namespace:        nsName,
				},
				Spec: &networking.VirtualService{
					Hosts:    []string{"*"},
					Gateways: []string{generalName},
					Http: []*networking.HTTPRoute{
						{
							Route: []*networking.HTTPRouteDestination{
								{
									Destination: &networking.Destination{
										Host: svcName,
									},
								},
							},
						},
					},
				},
			},
			{
				Meta: config.Meta{
					GroupVersionKind: gvk.RequestAuthentication,
					Name:             jwksSvc,
					Namespace:        nsName,
				},
				Spec: &security.RequestAuthentication{
					Selector: &v1beta1.WorkloadSelector{MatchLabels: gateway.Labels},
					JwtRules: []*security.JWTRule{{JwksUri: "https://" + jwksSvc}},
				},
			},
			{
				Meta: config.Meta{
					GroupVersionKind: gvk.RequestAuthentication,
					Name:             fooSvc,
					Namespace:        nsName,
				},
				Spec: &security.RequestAuthentication{
					// not matching the gateway
					Selector: &v1beta1.WorkloadSelector{MatchLabels: map[string]string{"foo": "bar"}},
					JwtRules: []*security.JWTRule{{JwksUri: "https://" + fooSvc}},
				},
			},
		},
		MeshConfig: &mesh.MeshConfig{
			ExtensionProviders: []*mesh.MeshConfig_ExtensionProvider{
				{
					Provider: &mesh.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp{
						EnvoyExtAuthzHttp: &mesh.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationHttpProvider{
							Service: extensionSvc,
						},
					},
				},
			},
		},
	})

	mergedGatewayNames := []string{nsName + "/" + generalName}
	gateway.MergedGateway = &model.MergedGateway{
		GatewayNames:    mergedGatewayNames,
		GatewayScopeKey: model.NewGatewayScopeKey(gateway.Type, gateway.ConfigNamespace, mergedGatewayNames),
	}
	gateway.SetSidecarScope(cg.PushContext())

	cases = []Case{
		{
			name:        "service without vs attached to gateway",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: fooSvc, Namespace: nsName}),
			want:        false,
			wantConfigs: sets.New[model.ConfigKey](),
		},
		{
			name:        "service with vs attached to gateway",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: svcName, Namespace: nsName}),
		},
		{
			name:        "mesh config extensions",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: extensionSvc, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: extensionSvc, Namespace: nsName}),
		},
		{
			name:        "jwks servers",
			proxy:       gateway,
			configs:     sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: jwksSvc, Namespace: nsName}),
			want:        true,
			wantConfigs: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: jwksSvc, Namespace: nsName}),
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			newReq, got := DefaultProxyNeedsPush(tt.proxy, &model.PushRequest{ConfigsUpdated: tt.configs, Push: cg.PushContext()})
			if got != tt.want {
				t.Fatalf("Got needs push = %v, expected %v", got, tt.want)
			}
			if tt.wantConfigs == nil && newReq.ConfigsUpdated != nil {
				t.Fatalf("Got configs updated = %v, expected none", newReq.ConfigsUpdated)
			}
			if tt.wantConfigs != nil && !tt.wantConfigs.Equals(newReq.ConfigsUpdated) {
				t.Fatalf("Got configs updated = %v, expected %v", newReq.ConfigsUpdated, tt.wantConfigs)
			}
		})
	}

	gateway.MergedGateway.ContainsAutoPassthroughGateways = true
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			newReq, push := DefaultProxyNeedsPush(tt.proxy, &model.PushRequest{ConfigsUpdated: tt.configs, Push: cg.PushContext()})
			if !push {
				t.Fatalf("Got needs push = %v, expected %v", push, true)
			}
			if !tt.configs.Equals(newReq.ConfigsUpdated) {
				t.Fatalf("Got configs updated = %v, expected %v", newReq.ConfigsUpdated, tt.configs)
			}
		})
	}
}

// TestProxyNeedsPushServiceTargets verifies how updates for the proxy's own service
// (LocalService / ServiceTargets) and its previous local service (PrevLocalService) are
// filtered when those services are not part of the proxy's egress (sidecar) scope.
//
// ServiceEntry updates for the current ServiceTargets are always kept (pre-existing
// inbound behavior). Endpoints updates for the local/previous-local service, and any
// PrevLocalService update, are only kept when the proxy has self-discovery enabled — this
// is what keeps the zone-aware local_cluster in sync as the proxy's own endpoints change.
func TestProxyNeedsPushServiceTargets(t *testing.T) {
	const (
		ownSvc   = "own.ns1.svc.cluster.local"
		prevSvc  = "prev.ns1.svc.cluster.local"
		otherSvc = "other.ns1.svc.cluster.local"
		ns       = "ns1"
		otherNs  = "ns2"
		nsRoot   = "rootns"
	)

	cg := core.NewConfigGenTest(t, core.TestOptions{})
	cg.PushContext().Mesh.RootNamespace = nsRoot

	// A sidecar whose egress scope does NOT include its own service, so DependsOnConfig
	// alone would filter out ServiceEntry/Endpoints updates for it. Only the
	// ServiceTargets / LocalService / PrevLocalService handling in filterRelevantUpdates
	// re-adds them. LocalService mirrors ServiceTargets[0], as SetServiceTargets populates it.
	newSidecar := func(selfDiscovery bool) *model.Proxy {
		return &model.Proxy{
			Type:         model.SidecarProxy,
			IPAddresses:  []string{"127.0.0.1"},
			Metadata:     &model.NodeMetadata{EnableSelfDiscovery: model.StringBool(selfDiscovery)},
			SidecarScope: &model.SidecarScope{Name: "sc1", Namespace: ns},
			ServiceTargets: []model.ServiceTarget{
				{Service: &model.Service{
					Hostname:   ownSvc,
					Attributes: model.ServiceAttributes{Namespace: ns},
				}},
			},
			LocalService:     model.LocalServiceInfo{Name: ownSvc, Namespace: ns},
			PrevLocalService: model.LocalServiceInfo{Name: prevSvc, Namespace: ns},
		}
	}

	key := func(k kind.Kind, name, namespace string) model.ConfigKey {
		return model.ConfigKey{Kind: k, Name: name, Namespace: namespace}
	}

	cases := []struct {
		name          string
		selfDiscovery bool
		configs       sets.Set[model.ConfigKey]
		want          bool
		wantConfigs   sets.Set[model.ConfigKey]
	}{
		// ServiceEntry for the current ServiceTargets is kept regardless of self-discovery.
		{
			name:          "service entry for own service kept without self-discovery",
			selfDiscovery: false,
			configs:       sets.New(key(kind.ServiceEntry, ownSvc, ns)),
			want:          true,
			wantConfigs:   sets.New(key(kind.ServiceEntry, ownSvc, ns)),
		},
		// Endpoints for the own service are dropped without self-discovery (out of scope).
		{
			name:          "endpoints for own service filtered without self-discovery",
			selfDiscovery: false,
			configs:       sets.New(key(kind.Endpoints, ownSvc, ns)),
			want:          false,
			wantConfigs:   sets.New[model.ConfigKey](),
		},
		// PrevLocalService is only relevant to self-discovery, so it is dropped otherwise.
		{
			name:          "previous local service filtered without self-discovery",
			selfDiscovery: false,
			configs:       sets.New(key(kind.ServiceEntry, prevSvc, ns), key(kind.Endpoints, prevSvc, ns)),
			want:          false,
			wantConfigs:   sets.New[model.ConfigKey](),
		},
		// With self-discovery, both ServiceEntry and Endpoints for the local service are kept.
		{
			name:          "endpoints for own service kept with self-discovery",
			selfDiscovery: true,
			configs:       sets.New(key(kind.Endpoints, ownSvc, ns)),
			want:          true,
			wantConfigs:   sets.New(key(kind.Endpoints, ownSvc, ns)),
		},
		{
			name:          "endpoints for previous local service kept with self-discovery",
			selfDiscovery: true,
			configs:       sets.New(key(kind.Endpoints, prevSvc, ns)),
			want:          true,
			wantConfigs:   sets.New(key(kind.Endpoints, prevSvc, ns)),
		},
		{
			name:          "service entry for previous local service kept with self-discovery",
			selfDiscovery: true,
			configs:       sets.New(key(kind.ServiceEntry, prevSvc, ns)),
			want:          true,
			wantConfigs:   sets.New(key(kind.ServiceEntry, prevSvc, ns)),
		},
		// Unrelated out-of-scope services are filtered even with self-discovery enabled.
		{
			name:          "endpoints for unrelated out-of-scope service filtered with self-discovery",
			selfDiscovery: true,
			configs:       sets.New(key(kind.Endpoints, otherSvc, ns)),
			want:          false,
			wantConfigs:   sets.New[model.ConfigKey](),
		},
		// Namespace must match: same name in a different namespace does not match the local service.
		{
			name:          "previous local service name but wrong namespace filtered with self-discovery",
			selfDiscovery: true,
			configs:       sets.New(key(kind.Endpoints, prevSvc, otherNs)),
			want:          false,
			wantConfigs:   sets.New[model.ConfigKey](),
		},
		{
			name:          "mixed: own-service endpoints kept, unrelated endpoints filtered with self-discovery",
			selfDiscovery: true,
			configs: sets.New(
				key(kind.Endpoints, ownSvc, ns),
				key(kind.Endpoints, otherSvc, ns),
			),
			want:        true,
			wantConfigs: sets.New(key(kind.Endpoints, ownSvc, ns)),
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			newReq, got := DefaultProxyNeedsPush(newSidecar(tt.selfDiscovery), &model.PushRequest{ConfigsUpdated: tt.configs, Push: cg.PushContext()})
			if got != tt.want {
				t.Fatalf("Got needs push = %v, expected %v", got, tt.want)
			}
			if !tt.wantConfigs.Equals(newReq.ConfigsUpdated) {
				t.Fatalf("Got configs updated = %v, expected %v", newReq.ConfigsUpdated, tt.wantConfigs)
			}
		})
	}
}

func TestCanSendPartialFullPushesIgnoresSkippedConfigs(t *testing.T) {
	endpoint := model.ConfigKey{Kind: kind.Endpoints, Name: "service.example"}
	proxy := &model.Proxy{Type: model.Router}
	for skippedKind := range skippedEdsConfigs {
		if skippedKind == kind.Address {
			continue
		}
		t.Run(skippedKind.String(), func(t *testing.T) {
			assert.Equal(t, canSendPartialFullPushes(&model.PushRequest{
				ConfigsUpdated: sets.New(
					endpoint,
					model.ConfigKey{Kind: skippedKind, Name: "unrelated"},
				),
			}, proxy), true)
		})
	}
}

func TestCanSendPartialFullPushesConservativeFallbacks(t *testing.T) {
	endpoint := model.ConfigKey{Kind: kind.Endpoints, Name: "service.example"}
	proxy := &model.Proxy{Type: model.Router}
	tests := []struct {
		name string
		req  *model.PushRequest
	}{
		{
			name: "Address",
			req: &model.PushRequest{ConfigsUpdated: sets.New(
				endpoint,
				model.ConfigKey{Kind: kind.Address, Name: "address"},
			)},
		},
		{
			name: "unclassified kind",
			req: &model.PushRequest{ConfigsUpdated: sets.New(
				endpoint,
				model.ConfigKey{Kind: kind.Kind(255), Name: "unknown"},
			)},
		},
		{
			name: "root PeerAuthentication",
			req: &model.PushRequest{
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.PeerAuthentication, Name: "default", Namespace: "istio-system"}),
				Push:           &model.PushContext{Mesh: &mesh.MeshConfig{RootNamespace: "istio-system"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, canSendPartialFullPushes(tt.req, proxy), false)
		})
	}
}

func TestCheckConnectionIdentity(t *testing.T) {
	cases := []struct {
		name      string
		identity  []string
		sa        string
		namespace string
		success   bool
	}{
		{
			name:      "single match",
			identity:  []string{spiffe.Identity{TrustDomain: "cluster.local", Namespace: "namespace", ServiceAccount: "serviceaccount"}.String()},
			sa:        "serviceaccount",
			namespace: "namespace",
			success:   true,
		},
		{
			name: "second match",
			identity: []string{
				spiffe.Identity{TrustDomain: "cluster.local", Namespace: "bad", ServiceAccount: "serviceaccount"}.String(),
				spiffe.Identity{TrustDomain: "cluster.local", Namespace: "namespace", ServiceAccount: "serviceaccount"}.String(),
			},
			sa:        "serviceaccount",
			namespace: "namespace",
			success:   true,
		},
		{
			name: "no match namespace",
			identity: []string{
				spiffe.Identity{TrustDomain: "cluster.local", Namespace: "bad", ServiceAccount: "serviceaccount"}.String(),
			},
			sa:        "serviceaccount",
			namespace: "namespace",
			success:   false,
		},
		{
			name: "no match service account",
			identity: []string{
				spiffe.Identity{TrustDomain: "cluster.local", Namespace: "namespace", ServiceAccount: "bad"}.String(),
			},
			sa:        "serviceaccount",
			namespace: "namespace",
			success:   false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{ConfigNamespace: tt.namespace, Metadata: &model.NodeMetadata{ServiceAccount: tt.sa}}
			if _, err := checkConnectionIdentity(proxy, tt.identity); (err == nil) != tt.success {
				t.Fatalf("expected success=%v, got err=%v", tt.success, err)
			}
		})
	}
}

func TestWaypointNeedsPush(t *testing.T) {
	const (
		waypointHost = "waypoint.default.svc.cluster.local"
		waypointVIP  = "3.0.0.0"
	)
	waypoint := &model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: "default",
		Metadata:        &model.NodeMetadata{ClusterID: "c1", Network: "net1"},
		ServiceTargets: []model.ServiceTarget{{
			Service: &model.Service{
				Hostname:    waypointHost,
				ClusterVIPs: model.AddressMap{Addresses: map[cluster.ID][]string{"c1": {waypointVIP}}},
			},
		}},
	}
	eastwest := &model.Proxy{
		Type:            model.Waypoint,
		ConfigNamespace: "default",
		Metadata:        &model.NodeMetadata{ClusterID: "c1", Network: "net1"},
		Labels: map[string]string{
			label.GatewayManaged.Name: constants.ManagedGatewayEastWestControllerLabel,
		},
	}

	addressUpdate := func(refs ...model.WaypointReference) *model.PushRequest {
		return &model.PushRequest{
			ConfigsUpdated:   sets.New(model.ConfigKey{Kind: kind.Address, Name: "Kubernetes//Pod/default/app"}),
			WaypointsUpdated: sets.New(refs...),
		}
	}

	cases := []struct {
		name  string
		proxy *model.Proxy
		req   *model.PushRequest
		want  bool
	}{
		{
			name:  "no address updates",
			proxy: waypoint,
			req:   &model.PushRequest{ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: "svc1.com", Namespace: "default"})},
			want:  false,
		},
		{
			// Ordinary pod churn: addresses changed but none of them attached to a waypoint
			name:  "address update without waypoint references",
			proxy: waypoint,
			req:   addressUpdate(),
			want:  false,
		},
		{
			name:  "address update attached to this waypoint by hostname",
			proxy: waypoint,
			req:   addressUpdate(model.WaypointReference{Namespace: "default", Hostname: waypointHost}),
			want:  true,
		},
		{
			name:  "address update attached to another waypoint",
			proxy: waypoint,
			req:   addressUpdate(model.WaypointReference{Namespace: "other", Hostname: "waypoint.other.svc.cluster.local"}),
			want:  false,
		},
		{
			name:  "address update attached to this waypoint by address",
			proxy: waypoint,
			req:   addressUpdate(model.WaypointReference{Network: "net1", Address: waypointVIP}),
			want:  true,
		},
		{
			name:  "address update attached to the same address on another network",
			proxy: waypoint,
			req:   addressUpdate(model.WaypointReference{Network: "net2", Address: waypointVIP}),
			want:  false,
		},
		{
			// East-west gateways serve global services rather than attached ones, so they
			// cannot be scoped by attachment
			name:  "east-west gateway",
			proxy: eastwest,
			req:   addressUpdate(),
			want:  true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, waypointNeedsPush(tt.req, tt.proxy), tt.want)
		})
	}

	t.Run("scoping disabled", func(t *testing.T) {
		test.SetForTest(t, &features.ScopedAddressPushes, false)
		assert.Equal(t, waypointNeedsPush(addressUpdate(), waypoint), true)
	})
}

// TestSidecarWaypointPushes ensures that sidecar proxies correctly receive xDS pushes for waypoint
// updates based on the feature flag and attachment.
func TestSidecarWaypointPushes(t *testing.T) {
	sidecar := &model.Proxy{Type: model.SidecarProxy}
	addressUpdate := &model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Address}),
		WaypointsUpdated: sets.New(model.WaypointReference{
			Namespace: "default",
			Hostname:  "waypoint.default.svc.cluster.local",
		}),
	}
	unrelatedAddressUpdate := &model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Address}),
	}
	endpointUpdate := &model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Endpoints}),
	}

	test.SetForTest(t, &features.EnableSidecarWaypointRouting, true)
	assert.Equal(t, waypointNeedsPush(addressUpdate, sidecar), true)
	assert.Equal(t, edsNeedsPush(addressUpdate, sidecar), true)
	assert.Equal(t, ldsNeedsPush(sidecar, addressUpdate), true)
	assert.Equal(t, canSendPartialFullPushes(endpointUpdate, sidecar), false)
	assert.Equal(t, waypointNeedsPush(unrelatedAddressUpdate, sidecar), false)
	assert.Equal(t, edsNeedsPush(unrelatedAddressUpdate, sidecar), false)
	assert.Equal(t, ldsNeedsPush(sidecar, unrelatedAddressUpdate), false)

	test.SetForTest(t, &features.EnableSidecarWaypointRouting, false)
	assert.Equal(t, waypointNeedsPush(addressUpdate, sidecar), false)
	assert.Equal(t, edsNeedsPush(addressUpdate, sidecar), false)
	assert.Equal(t, ldsNeedsPush(sidecar, addressUpdate), false)
	assert.Equal(t, canSendPartialFullPushes(endpointUpdate, sidecar), true)
}
