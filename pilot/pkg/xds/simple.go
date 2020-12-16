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
	"sync"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	controllermemory "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/pkg/log"
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
	MemoryConfigStore model.IstioConfigStore

	// GRPCListener is the listener used for GRPC. For agent it is
	// an insecure port, bound to 127.0.0.1
	GRPCListener net.Listener

	// syncCh is used for detecting if the stores have synced,
	// which needs to happen before serving requests.
	syncCh chan string

	ConfigStoreCache model.ConfigStoreCache

	m sync.RWMutex
}

// Creates an basic, functional discovery server, using the same code as Istiod, but
// backed by an in-memory config and endpoint stores.
//
// Can be used in tests, or as a minimal XDS discovery server with no dependency on K8S or
// the complex bootstrap used by Istiod. A memory registry and memory config store are used to
// generate the configs - they can be programmatically updated.
func NewXDS() *SimpleServer {
	// Prepare a working XDS server, with aggregate config and registry stores and a memory store for each.
	// TODO: refactor bootstrap code to use this server, and add more registries.

	env := &model.Environment{
		PushContext: model.NewPushContext(),
	}
	mc := mesh.DefaultMeshConfig()
	env.Watcher = mesh.NewFixedWatcher(&mc)
	env.PushContext.Mesh = env.Watcher.Mesh()

	ds := NewDiscoveryServer(env, nil, "pilot-123")
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

	serviceEntryStore := serviceentry.NewServiceDiscovery(configController, s.MemoryConfigStore, ds)
	serviceEntryRegistry := serviceregistry.Simple{
		ProviderID:       "External",
		Controller:       serviceEntryStore,
		ServiceDiscovery: serviceEntryStore,
	}
	serviceControllers.AddRegistry(serviceEntryRegistry)

	sd := controllermemory.NewServiceDiscovery(nil)
	sd.EDSUpdater = ds
	ds.MemRegistry = sd
	serviceControllers.AddRegistry(serviceregistry.Simple{
		ProviderID:       "Mem",
		ServiceDiscovery: sd,
		Controller:       sd.Controller,
	})
	env.ServiceDiscovery = serviceControllers

	go configController.Run(make(chan struct{}))

	// configStoreCache - with HasSync interface
	aggregateConfigController, err := configaggregate.MakeCache([]model.ConfigStoreCache{
		configController,
	})
	if err != nil {
		log.Fatala("Creating aggregate config ", err)
	}

	// TODO: fix the mess of store interfaces - most are too generic for their own good.
	s.ConfigStoreCache = aggregateConfigController
	env.IstioConfigStore = model.MakeIstioStore(aggregateConfigController)

	return s
}

func (s *SimpleServer) StartGRPC(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	gs := grpc.NewServer()
	s.DiscoveryServer.Register(gs)
	reflection.Register(gs)
	s.GRPCListener = lis
	go func() {
		err = gs.Serve(lis)
		if err != nil {
			log.Info("Serve done ", err)
		}
	}()
	return nil
}

// ProxyGen implements a proxy generator - any request is forwarded using the agent ADSC connection.
// Responses are forwarded back on the connection that they are received.
type ProxyGen struct {
	adsc   *adsc.ADSC
	server *SimpleServer
}

// HandleResponse will dispatch a response from a federated
// XDS server to all connections listening for that type.
func (p *ProxyGen) HandleResponse(con *adsc.ADSC, res *discovery.DiscoveryResponse) {
	// TODO: filter the push to only connections that
	// match a filter.
	// Push config changes, iterating over connected envoys. This cover ADS and EDS(0.7), both share
	// the same connection table
	// Create a temp map to avoid locking the add/remove
	pending := []*Connection{}
	for _, v := range p.server.DiscoveryServer.Clients() {
		v.proxy.RLock()
		if v.proxy.WatchedResources[res.TypeUrl] != nil {
			pending = append(pending, v)
		}
		v.proxy.RUnlock()
	}

	// only marshal resources if there are connected clients
	if len(pending) == 0 {
		return
	}

	for _, p := range pending {
		// p.send() waits for an ACK - which is reasonable for normal push,
		// but in this case we want to sync fast and not bother with stuck connections.
		// This is expecting a relatively small number of watchers - each other istiod
		// plus few admin tools or bridges to real message brokers. The normal
		// push expects 1000s of envoy connections.
		con := p
		go func() {
			err := con.stream.Send(res)
			if err != nil {
				adsLog.Info("Failed to send internal event ", con.ConID, " ", err)
			}
		}()
	}
}

func (s *SimpleServer) NewProxy() *ProxyGen {
	return &ProxyGen{
		server: s,
	}
}

func (p *ProxyGen) AddClient(adsc *adsc.ADSC) {
	p.server.m.Lock()
	p.adsc = adsc // TODO: list
	p.server.m.Unlock()
}

func (p *ProxyGen) Close() {
	if p.adsc != nil {
		p.adsc.Close()
	}
}

// TODO: remove clients, multiple clients (agent has only one)

// Generate will forward the request to all remote XDS servers.
// Responses will be forwarded back to the client.
//
// TODO: allow clients to indicate which requests they handle ( similar with topic )
func (p *ProxyGen) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, req *model.PushRequest) model.Resources {
	if p.adsc == nil {
		return nil
	}

	// TODO: track requests to connections, so resonses from server are dispatched to the right con
	//
	// TODO: Envoy (or client) may send multiple requests without waiting for the ack.
	// Need to change the signature of Generator to take Request as parameter.
	err := p.adsc.Send(w.LastRequest)
	if err != nil {
		log.Debug("Failed to send, connection probably closed ", err)
	}

	return nil
}
