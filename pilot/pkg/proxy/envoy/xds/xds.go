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

package xds

import (
	"net"

	"google.golang.org/grpc"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
)

type Server struct {
	// DiscoveryServer is the gRPC XDS implementation
	// Env and MemRegistry are available as fields, as well as the default PushContext.
	DiscoveryServer *v2.DiscoveryServer

	// MemoryStore is an in-memory config store, part of the aggregate store used by the discovery server.
	MemoryConfigStore model.IstioConfigStore

	AggregateConfigControoler *aggregate.Controller
	GRPCListener              net.Listener
}

// Creates an basic, functional discovery server, using the same code as Istiod, but
// backed by an in-memory config and endpoint stores.
//
// Can be used in tests, or as a minimal XDS discovery server with no dependency on K8S or
// the complex bootstrap used by Istiod. A memory registry and memory config store are used to
// generate the configs - they can be programmatically updated.
func NewXDS() *Server {
	// Prepare a working XDS server, with aggregate config and registry stores and a memory store for each.
	// TODO: refactor bootstrap code to use this server, and add more registries.

	env := &model.Environment{
		PushContext: model.NewPushContext(),
	}
	mc := mesh.DefaultMeshConfig()
	env.Watcher = mesh.NewFixedWatcher(&mc)
	env.PushContext.Mesh = env.Watcher.Mesh()

	ds := v2.NewDiscoveryServer(env, nil)

	// In-memory config store, controller and istioConfigStore
	store := memory.Make(collections.Pilot)

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

	go configController.Run(make(chan struct{}))

	env.IstioConfigStore = istioConfigStore
	env.ServiceDiscovery = serviceControllers

	return &Server{
		DiscoveryServer: ds,
		MemoryConfigStore: istioConfigStore,
		AggregateConfigControoler: serviceControllers,
	}
}

func (s *Server) StartGRPC(addr string) error {


	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	gs := grpc.NewServer()
	s.DiscoveryServer.Register(gs)
	s.GRPCListener = lis
	go gs.Serve(lis)

	return nil
}
