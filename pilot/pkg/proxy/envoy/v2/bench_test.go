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

	envoy_api_v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/pkg/log"
)

// SetupDiscoveryServer creates a DiscoveryServer with the provided configs using the mem registry
func SetupDiscoveryServer(t testing.TB, cfgs ...model.Config) *DiscoveryServer {
	m := mesh.DefaultMeshConfig()
	store := memory.Make(schemas.Istio)
	configController := memory.NewController(store)
	istioConfigStore := model.MakeIstioStore(configController)
	serviceControllers := aggregate.NewController()
	serviceEntryStore := external.NewServiceDiscovery(configController, istioConfigStore)
	go configController.Run(make(chan struct{}))
	serviceEntryRegistry := aggregate.Registry{
		Name:             "ServiceEntries",
		Controller:       serviceEntryStore,
		ServiceDiscovery: serviceEntryStore,
	}
	serviceControllers.AddRegistry(serviceEntryRegistry)

	env := &model.Environment{
		Mesh:             &m,
		MeshNetworks:     nil,
		IstioConfigStore: istioConfigStore,
		ServiceDiscovery: serviceControllers,
		PushContext:      model.NewPushContext(),
	}
	for _, cfg := range cfgs {
		if _, err := configController.Create(cfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.PushContext.InitContext(env, env.PushContext, nil); err != nil {
		t.Fatal(err)
	}
	s := NewDiscoveryServer(env, v1alpha3.NewConfigGenerator([]plugin.Plugin{}),
		serviceControllers, nil, configController)
	if err := s.updateServiceShards(s.globalPushContext()); err != nil {
		t.Fatalf("Failed to update service shards: %v", err)
	}
	return s
}

func createEndpoints(numEndpoints int) []model.Config {
	result := []model.Config{}
	endpoints := make([]*networking.ServiceEntry_Endpoint, 0, numEndpoints)
	for i := 0; i < numEndpoints; i++ {
		endpoints = append(endpoints, &networking.ServiceEntry_Endpoint{Address: fmt.Sprintf("111.%d.%d.%d", i/(256*256), (i/256)%256, i%256)})
	}
	result = append(result, model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schemas.ServiceEntry.Type,
			Name:              fmt.Sprintf("foo"),
			Namespace:         "default",
			CreationTimestamp: time.Now(),
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"foo.com"},
			Ports: []*networking.Port{
				{Number: 80, Name: "http-port", Protocol: "http"},
			},
			Endpoints:  endpoints,
			Resolution: networking.ServiceEntry_STATIC,
		},
	})
	return result
}

// BenchmarkEDS measures performance of EDS config generation
// TODO Add more variables, such as different services
func BenchmarkEDS(b *testing.B) {
	tests := []struct {
		endpoints int
	}{
		{100},
		{1000},
		{10000},
		{100000},
	}
	adsLog.SetOutputLevel(log.WarnLevel)
	var response interface{}
	for _, tt := range tests {
		b.Run(fmt.Sprintf("%d", tt.endpoints), func(b *testing.B) {
			s := SetupDiscoveryServer(b, createEndpoints(tt.endpoints)...)
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
				l := s.loadAssignmentsForClusterIsolated(proxy, push, "outbound|80||foo.com")

				clonedCLA := util.CloneClusterLoadAssignment(l)
				l = &clonedCLA

				loadbalancer.ApplyLocalityLBSetting(proxy.Locality, l, s.Env.Mesh.LocalityLbSetting, true)
				response = endpointDiscoveryResponse([]*envoy_api_v2.ClusterLoadAssignment{l}, version, push.Version)
			}
		})
	}
	_ = response
}
