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

package v2

import (
	"bytes"
	"fmt"
	"html/template"
	"net"
	"path"
	"testing"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/test"

	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/serviceregistry/mock"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"

	"istio.io/istio/pilot/pkg/networking/core"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
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
	serviceEntryStore := serviceentry.NewServiceDiscovery(configController, istioConfigStore, s)
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
	if err := s.UpdateServiceShards(s.globalPushContext()); err != nil {
		t.Fatalf("Failed to update service shards: %v", err)
	}
	return s
}

func createEndpoints(numEndpoints int, numServices int) []model.Config {
	result := make([]model.Config, 0, numServices)
	for s := 0; s < numServices; s++ {
		endpoints := make([]*networking.WorkloadEntry, 0, numEndpoints)
		for e := 0; e < numEndpoints; e++ {
			endpoints = append(endpoints, &networking.WorkloadEntry{Address: fmt.Sprintf("111.%d.%d.%d", e/(256*256), (e/256)%256, e%256)})
		}
		result = append(result, model.Config{
			ConfigMeta: model.ConfigMeta{
				GroupVersionKind:  collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
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

func getNextIP(current string) string {
	i := net.ParseIP(current).To4()
	v := uint(i[0])<<24 + uint(i[1])<<16 + uint(i[2])<<8 + uint(i[3])
	v++
	v3 := byte(v & 0xFF)
	v2 := byte((v >> 8) & 0xFF)
	v1 := byte((v >> 16) & 0xFF)
	v0 := byte((v >> 24) & 0xFF)
	ret := net.IPv4(v0, v1, v2, v3)
	return ret.String()
}

func buildTestEnv(t test.Failer, cfg []model.Config, input ConfigInput) model.Environment {
	svcs := map[host.Name]*model.Service{}
	ip := "1.1.1.1"
	for _, svc := range input.Services {
		hn := host.Name(svc)
		s := &model.Service{
			Ports: []*model.Port{{
				Name:     "http",
				Port:     80,
				Protocol: protocol.HTTP,
			}, {
				Name:     "tcp",
				Port:     81,
				Protocol: protocol.TCP,
			}, {
				Name:     "https",
				Port:     443,
				Protocol: protocol.HTTPS,
			}, {
				Name:     "auto",
				Port:     83,
				Protocol: protocol.Unsupported,
			}},
			Hostname: hn,
			Address:  ip,
		}
		svcs[hn] = s
		ip = getNextIP(ip)
	}
	serviceDiscovery := mock.NewDiscovery(svcs, 1)

	configStore := memory.Make(collections.Pilot)
	for _, cfg := range cfg {
		if _, err := configStore.Create(cfg); err != nil {
			t.Fatalf("failed to create config %v: %v", cfg.Name, err)
		}

	}

	m := mesh.DefaultMeshConfig()
	env := model.Environment{
		PushContext:      model.NewPushContext(),
		ServiceDiscovery: serviceDiscovery,
		IstioConfigStore: model.MakeIstioStore(configStore),
		Watcher:          mesh.NewFixedWatcher(&m),
	}

	return env
}

// ConfigInput defines inputs passed to the test config templates
// This allows tests to do things like create a virtual service for each service, for example
type ConfigInput struct {
	// A list of hostname of allservices
	Services []string
}

// Setup test builds a mock test environment.
// TODO make this configurable with Istio config, different service setups, etc
func setupTest(t test.Failer, testName string) (model.Environment, core.ConfigGenerator, model.Proxy) {
	proxy := model.Proxy{
		Type:        model.SidecarProxy,
		IPAddresses: []string{"1.1.1.1"},
		ID:          "v0.default",
		DNSDomain:   "default.example.org",
		Metadata: &model.NodeMetadata{
			Namespace: "not-default",
		},
		IstioVersion:    &model.IstioVersion{Major: 1, Minor: 6},
		ConfigNamespace: "not-default",
	}

	numServices := 100
	services := []string{}
	for i := 0; i < numServices; i++ {
		hn := fmt.Sprintf("service-%d.namespace.svc.cluster.local", i)
		services = append(services, hn)
	}
	tmpl, err := template.ParseFiles(path.Join("testdata", "benchmarks", testName+".yaml"))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var buf bytes.Buffer
	input := ConfigInput{Services: services}
	if err := tmpl.Execute(&buf, input); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}
	configs, _, err := crd.ParseInputs(buf.String())
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	env := buildTestEnv(t, configs, input)
	env.PushContext.InitContext(&env, nil, nil)
	proxy.SetSidecarScope(env.PushContext)
	configgen := core.NewConfigGenerator([]string{plugin.Authn, plugin.Authz, plugin.Health, plugin.Mixer})
	return env, configgen, proxy
}

func routesFromListeners(ll []*listener.Listener) []string {
	routes := []string{}
	for _, l := range ll {
		for _, fc := range l.FilterChains {
			for _, filter := range fc.Filters {
				if filter.Name == "envoy.hcmection_manager" {
					filter.GetTypedConfig()
					hcon := &hcm.HttpConnectionManager{}
					if err := ptypes.UnmarshalAny(filter.GetTypedConfig(), hcon); err != nil {
						panic(err)
					}
					switch r := hcon.GetRouteSpecifier().(type) {
					case *hcm.HttpConnectionManager_Rds:
						routes = append(routes, r.Rds.RouteConfigName)
					}
				}
			}
		}
	}
	return routes
}

var testCases = []string{
	"empty",
	"telemetry",
	"virtualservice",
}

func BenchmarkRouteGeneration(b *testing.B) {
	for _, tt := range testCases {
		b.Run(tt, func(b *testing.B) {
			env, configgen, proxy := setupTest(b, tt)
			// To determine which routes to generate, first gen listeners once (not part of benchmark) and extract routes
			l := configgen.BuildListeners(&proxy, env.PushContext)
			routeNames := routesFromListeners(l)
			b.ResetTimer()
			var response interface{}
			for n := 0; n < b.N; n++ {
				r := configgen.BuildHTTPRoutes(&proxy, env.PushContext, routeNames)
				response = routeDiscoveryResponse(r, "", "", RouteType)
			}
			_ = response
		})
	}
}

func BenchmarkClusterGeneration(b *testing.B) {
	for _, tt := range testCases {
		b.Run(tt, func(b *testing.B) {
			env, configgen, proxy := setupTest(b, tt)
			b.ResetTimer()
			var response interface{}
			for n := 0; n < b.N; n++ {
				c := configgen.BuildClusters(&proxy, env.PushContext)
				response = cdsDiscoveryResponse(c, "", ClusterType)
			}
			_ = response
		})
	}
}

func BenchmarkListenerGeneration(b *testing.B) {
	for _, tt := range testCases {
		b.Run(tt, func(b *testing.B) {
			env, configgen, proxy := setupTest(b, tt)
			b.ResetTimer()
			var response interface{}
			for n := 0; n < b.N; n++ {
				l := configgen.BuildListeners(&proxy, env.PushContext)
				response = ldsDiscoveryResponse(l, "", "", ListenerType)
			}
			_ = response
		})
	}
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

				loadAssignments := make([]*endpoint.ClusterLoadAssignment, 0)
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
				response = endpointDiscoveryResponse(loadAssignments, version, push.Version, EndpointType)
			}
		})
	}
	_ = response
}
