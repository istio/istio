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
	"testing"
	"time"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schemas"
)

// newDiscoveryServer creates a DiscoveryServer with the provided configs using the mem registry
func newDiscoveryServer() (*DiscoveryServer, model.ConfigStoreCache, error) {
	m := mesh.DefaultMeshConfig()
	store := memory.Make(schemas.Istio)
	configController := memory.NewController(store)
	istioConfigStore := model.MakeIstioStore(configController)
	serviceControllers := aggregate.NewController()
	serviceEntryStore := external.NewServiceDiscovery(configController, istioConfigStore)
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

	s := NewDiscoveryServer(env, v1alpha3.NewConfigGenerator([]plugin.Plugin{}))
	return s, configController, nil
}

func TestEndpointShardsMemoryLeak(t *testing.T) {
	server, configStore, err := newDiscoveryServer()
	if err != nil {
		t.Errorf("Failed creating discovery server: %v", err)
		return
	}

	configStore.RegisterEventHandler(schemas.ServiceEntry.Type, func(config model.Config, event model.Event) {
		serviceEntry := config.Spec.(*networking.ServiceEntry)
		hostname := serviceEntry.Hosts[0]
		server.SvcUpdate("", hostname, config.Namespace, event)
	})

	go configStore.Run(make(chan struct{}))

	configs := createEndpoints(2, 5)
	for _, cfg := range configs {
		if _, err := configStore.Create(cfg); err != nil {
			t.Errorf("Failed create: %v", cfg)
		}
	}

	if err := server.Env.PushContext.InitContext(server.Env, nil, nil); err != nil {
		t.Errorf("Failed init context: %v", err)
	}

	if err := server.updateServiceShards(server.globalPushContext()); err != nil {
		t.Errorf("Failed updateServiceShards: %v", err)
	}

	for _, se := range configs {
		endpointShardsByNamespace, exist := server.EndpointShardsByService[se.Spec.(*networking.ServiceEntry).Hosts[0]]
		if !exist {
			t.Errorf("Service %s not exist", se.Spec.(*networking.ServiceEntry).Hosts[0])
		}
		endpointShards, exist := endpointShardsByNamespace[se.Namespace]
		if !exist {
			t.Errorf("Service %s not exist", se.Spec.(*networking.ServiceEntry).Hosts[0])
		}

		endpointShards.mutex.RLock()
		epNum := len(endpointShards.Shards[""])
		endpointShards.mutex.RUnlock()
		if epNum != 2 {
			t.Errorf("Service %s endpoint does not match", se.Spec.(*networking.ServiceEntry).Hosts[0])
		}

		configStore.Delete(se.Type, se.Name, se.Namespace)
		// TODO: figure out a way to notify
		time.Sleep(100 * time.Millisecond)

		server.mutex.RLock()
		_, exist = server.EndpointShardsByService[se.Spec.(*networking.ServiceEntry).Hosts[0]]
		server.mutex.RUnlock()
		if exist {
			t.Errorf("EndpointShards of service %s should be deleted", se.Spec.(*networking.ServiceEntry).Hosts[0])
		}
	}
}
