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
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	controllermemory "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
)

// Server represents the XDS serving feature of Istiod (pilot).
// Unlike bootstrap/, this packet has no dependencies on K8S, CA,
// and other features. It'll be used initially in the istio-agent,
// to provide a minimal proxy while reusing the same code as istiod.
// Portions of the code will also be used in istiod - after it becomes
// stable the plan is to refactor bootstrap to use this code instead
// of directly bootstrapping XDS.
//
// The server support proxy/federation of multiple sources - last part
// or parity with MCP/Galley and MCP-over-XDS.
type SimpleServer struct {
	// DiscoveryServer is the gRPC XDS implementation
	// Env and MemRegistry are available as fields, as well as the default
	// PushContext.
	DiscoveryServer *DiscoveryServer

	// MemoryStore is an in-memory config store, part of the aggregate store
	// used by the discovery server.
	MemoryConfigStore model.ConfigStore

	// GRPCListener is the listener used for GRPC. For agent it is
	// an insecure port, bound to 127.0.0.1
	GRPCListener net.Listener

	// syncCh is used for detecting if the stores have synced,
	// which needs to happen before serving requests.
	syncCh chan string

	ConfigStoreCache model.ConfigStoreController
}

// Creates an basic, functional discovery server, using the same code as Istiod, but
// backed by an in-memory config and endpoint stores.
//
// Can be used in tests, or as a minimal XDS discovery server with no dependency on K8S or
// the complex bootstrap used by Istiod. A memory registry and memory config store are used to
// generate the configs - they can be programmatically updated.
func NewXDS(stop chan struct{}) *SimpleServer {
	// Prepare a working XDS server, with aggregate config and registry stores and a memory store for each.
	// TODO: refactor bootstrap code to use this server, and add more registries.

	env := model.NewEnvironment()
	env.Watcher = mesh.NewFixedWatcher(mesh.DefaultMeshConfig())
	env.PushContext.Mesh = env.Watcher.Mesh()
	env.Init()

	ds := NewDiscoveryServer(env, "istiod", map[string]string{})
	ds.InitGenerators(env, "istio-system")
	ds.CachesSynced()

	// Config will have a fixed format:
	// - aggregate store
	// - one primary (local) memory config
	// Additional stores can be added dynamically - for example by push or reference from a server.
	// This is used to implement and test XDS federation (which is not yet final).

	// In-memory config store, controller and istioConfigStore
	schemas := collections.Pilot

	store := memory.Make(schemas)
	s := &SimpleServer{
		DiscoveryServer: ds,
	}
	s.syncCh = make(chan string, len(schemas.All()))
	configController := memory.NewController(store)
	s.MemoryConfigStore = model.MakeIstioStore(configController)

	// Endpoints/Clusters - using the config store for ServiceEntries
	serviceControllers := aggregate.NewController(aggregate.Options{})

	serviceEntryController := serviceentry.NewController(configController, s.MemoryConfigStore, ds)
	serviceControllers.AddRegistry(serviceEntryController)

	sd := controllermemory.NewServiceDiscovery()
	sd.XdsUpdater = ds
	ds.MemRegistry = sd
	serviceControllers.AddRegistry(serviceregistry.Simple{
		ProviderID:       "Mem",
		ServiceDiscovery: sd,
		Controller:       sd.Controller,
	})
	env.ServiceDiscovery = serviceControllers

	go configController.Run(stop)

	// configStoreCache - with HasSync interface
	aggregateConfigController, err := configaggregate.MakeCache([]model.ConfigStoreController{
		configController,
	})
	if err != nil {
		log.Fatalf("Creating aggregate config: %v", err)
	}

	// TODO: fix the mess of store interfaces - most are too generic for their own good.
	s.ConfigStoreCache = aggregateConfigController
	env.ConfigStore = model.MakeIstioStore(aggregateConfigController)

	return s
}

func (s *SimpleServer) StartGRPC(addr string) (string, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	gs := grpc.NewServer()
	s.DiscoveryServer.Register(gs)
	reflection.Register(gs)
	s.GRPCListener = lis
	go func() {
		err = gs.Serve(lis)
		if err != nil {
			log.Infof("Serve done with %v", err)
		}
	}()
	return lis.Addr().String(), nil
}
