// Copyright 2019 Istio Authors
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

package v2

import (
	"fmt"
	"testing"
	"time"

	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/serviceregistry/mock"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"

	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/fakes"
	"istio.io/istio/pkg/config/schema/resource"

	envoy_api_v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
)

// SetupDiscoveryServer creates a DiscoveryServer with the provided configs using the mem registry
func SetupDiscoveryServer(t testing.TB, cfgs ...model.Config) *DiscoveryServer {
	m := mesh.DefaultMeshConfig()
	env := &model.Environment{
		Watcher:         mesh.NewFixedWatcher(&m),
		NetworksWatcher: mesh.NewFixedNetworksWatcher(nil),
		PushContext:     model.NewPushContext(),
	}
	s := NewDiscoveryServer(env, []string{})
	store := memory.Make(collections.Pilot)
	configController := memory.NewController(store)
	istioConfigStore := model.MakeIstioStore(configController)
	serviceControllers := aggregate.NewController()
	serviceEntryStore := external.NewServiceDiscovery(configController, istioConfigStore, s)
	go configController.Run(make(chan struct{}))
	serviceEntryRegistry := serviceregistry.Simple{
		ProviderID:       "External",
		Controller:       serviceEntryStore,
		ServiceDiscovery: serviceEntryStore,
	}
	serviceControllers.AddRegistry(serviceEntryRegistry)

	env.IstioConfigStore = istioConfigStore
	env.ServiceDiscovery = serviceControllers

	for _, cfg := range cfgs {
		if _, err := configController.Create(cfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.PushContext.InitContext(env, env.PushContext, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.updateServiceShards(s.globalPushContext()); err != nil {
		t.Fatalf("Failed to update service shards: %v", err)
	}
	return s
}

func createEndpoints(numEndpoints int, numServices int) []model.Config {
	result := make([]model.Config, 0, numServices)
	for s := 0; s < numServices; s++ {
		endpoints := make([]*networking.ServiceEntry_Endpoint, 0, numEndpoints)
		for e := 0; e < numEndpoints; e++ {
			endpoints = append(endpoints, &networking.ServiceEntry_Endpoint{Address: fmt.Sprintf("111.%d.%d.%d", e/(256*256), (e/256)%256, e%256)})
		}
		result = append(result, model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:              collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Kind(),
				Group:             collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Group(),
				Version:           collections.IstioNetworkingV1Alpha3Serviceentries.Resource().Version(),
				Name:              fmt.Sprintf("foo-%d", s),
				Namespace:         "default",
				CreationTimestamp: time.Now(),
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{fmt.Sprintf("foo-%d.com", s)},
				Ports: []*networking.Port{
					{Number: 80, Name: "http-port", Protocol: "http"},
				},
				Endpoints:  endpoints,
				Resolution: networking.ServiceEntry_STATIC,
			},
		})
	}
	return result
}

func buildTestEnv(cfg []*model.Config, services int) model.Environment {
	svcs := map[host.Name]*model.Service{}
	for i := 0; i < services; i++ {
		hn := host.Name(fmt.Sprintf("service-%d.namespace.svc.cluster.local", i))
		s := &model.Service{
			Attributes: model.ServiceAttributes{
				Name:      fmt.Sprintf("service-%d", i),
				Namespace: "namespace",
			},
			Ports: []*model.Port{{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			}, {
				Name:     "tcp",
				Port:     81,
				Protocol: protocol.TCP,
			}, {}, {
				Name:     "https",
				Port:     443,
				Protocol: protocol.HTTPS,
			}, {
				Name:     "auto",
				Port:     83,
				Protocol: protocol.Unsupported,
			}},
			Hostname: hn,
			Address:  fmt.Sprintf("1.1.1.%d", services),
		}
		svcs[hn] = s
	}
	serviceDiscovery := mock.NewDiscovery(svcs, 1)

	configStore := &fakes.IstioConfigStore{
		ListStub: func(typ resource.GroupVersionKind, namespace string) (configs []model.Config, e error) {
			res := []model.Config{}
			for _, c := range cfg {
				if c.Type == typ.Kind {
					res = append(res, *c)
				}
			}
			return res, nil
		},
	}

	m := mesh.DefaultMeshConfig()
	env := model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: configStore,
		Watcher:          mesh.NewFixedWatcher(&m),
	}

	return env
}

// Setup test builds a mock test environment.
// TODO make this configurable with Istio config, different service setups, etc
func setupTest() (model.Environment, core.ConfigGenerator, model.Proxy) {
	proxy := model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			ConfigNamespace: "not-default",
		},
		ConfigNamespace: "not-default",
	}
	env := buildTestEnv(nil, 100)
	env.PushContext.InitContext(&env, nil, nil)
	proxy.SetSidecarScope(env.PushContext)
	configgen := core.NewConfigGenerator([]string{plugin.Authn, plugin.Authz, plugin.Health, plugin.Mixer})
	return env, configgen, proxy
}

func routesFromListeners(ll []*envoy_api_v2.Listener) []string {
	routes := []string{}
	for _, l := range ll {
		for _, fc := range l.FilterChains {
			for _, filter := range fc.Filters {
				if filter.Name == "envoy.http_connection_manager" {
					filter.GetTypedConfig()
					hcm := &http_conn.HttpConnectionManager{}
					if err := ptypes.UnmarshalAny(filter.GetTypedConfig(), hcm); err != nil {
						panic(err)
					}
					switch r := hcm.GetRouteSpecifier().(type) {
					case *http_conn.HttpConnectionManager_Rds:
						routes = append(routes, r.Rds.RouteConfigName)
					}
				}
			}
		}
	}
	return routes
}

func BenchmarkRouteGeneration(b *testing.B) {
	env, configgen, proxy := setupTest()
	// To determine which routes to generate, first gen listeners once (not part of benchmark) and extract routes
	l := configgen.BuildListeners(&proxy, env.PushContext)
	routeNames := routesFromListeners(l)
	b.ResetTimer()
	var response interface{}
	for n := 0; n < b.N; n++ {
		r := configgen.BuildHTTPRoutes(&proxy, env.PushContext, routeNames)
		response = routeDiscoveryResponse(r, "", "")
	}
	_ = response
}

func BenchmarkClusterGeneration(b *testing.B) {
	env, configgen, proxy := setupTest()
	b.ResetTimer()
	var response interface{}
	for n := 0; n < b.N; n++ {
		c := configgen.BuildClusters(&proxy, env.PushContext)
		response = cdsDiscoveryResponse(c, "")
	}
	_ = response
}

func BenchmarkListenerGeneration(b *testing.B) {
	env, configgen, proxy := setupTest()
	b.ResetTimer()
	var response interface{}
	for n := 0; n < b.N; n++ {
		l := configgen.BuildListeners(&proxy, env.PushContext)
		response = ldsDiscoveryResponse(l, "", "")
	}
	_ = response
}

// BenchmarkEDS measures performance of EDS config generation
// TODO Add more variables, such as different services
func BenchmarkEndpointGeneration(b *testing.B) {
	tests := []struct {
		endpoints int
		services  int
	}{
		{100, 1},
		{1000, 1},
		{10000, 1},
		{100, 100},
		{1000, 100},
		{10000, 100},
	}
	adsLog.SetOutputLevel(log.WarnLevel)
	var response interface{}
	for _, tt := range tests {
		b.Run(fmt.Sprintf("%d/%d", tt.endpoints, tt.services), func(b *testing.B) {
			s := SetupDiscoveryServer(b, createEndpoints(tt.endpoints, tt.services)...)
			proxy := &model.Proxy{
				Type:            model.SidecarProxy,
				IPAddresses:     []string{"10.3.3.3"},
				ID:              "random",
				ConfigNamespace: "default",
				Metadata:        &model.NodeMetadata{},
			}
			push := s.globalPushContext()
			proxy.SetSidecarScope(push)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				// This should correlate to pushEds()
				// TODO directly call pushEeds, but mock/skip the grpc send

				loadAssignments := make([]*envoy_api_v2.ClusterLoadAssignment, 0)
				for svc := 0; svc < tt.services; svc++ {
					l := s.loadAssignmentsForClusterIsolated(proxy, push, fmt.Sprintf("outbound|80||foo-%d.com", svc))

					if l == nil {
						continue
					}

					clonedCLA := util.CloneClusterLoadAssignment(l)
					l = &clonedCLA

					loadbalancer.ApplyLocalityLBSetting(proxy.Locality, l, s.Env.Mesh().LocalityLbSetting, true)
					loadAssignments = append(loadAssignments, l)
				}
				response = endpointDiscoveryResponse(loadAssignments, version, push.Version)
			}
		})
	}
	_ = response
}
