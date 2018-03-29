// Copyright 2018 Istio Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package v2

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/envoyproxy/go-control-plane/pkg/server"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/v1alpha3"
)

// ConfigCache for xDS resources
type ConfigCache struct {
	serviceSuffix string

	// inputs
	services model.ServiceDiscovery
	configs  model.ConfigStore

	// version counter
	// TODO: this should be derived from the inputs for load balancing between xDS instances to work correctly
	version int64

	// generated hosts for the version counter;
	// hosts need to be computed once per the entire mesh, reducing the load on specializing it to each proxy
	hosts []v1alpha3.GuardedHost

	// tracking nodes with the last request timestamp
	// TODO: garbage collection based on the expiration of the last request or stream closure
	nodes map[string]time.Time

	snapshots cache.SnapshotCache
	server    server.Server

	mu sync.Mutex
}

// NewConfigCache spins up a new config cache
func NewConfigCache(services model.ServiceDiscovery, configs model.ConfigStore, domainSuffix string) *ConfigCache {
	out := &ConfigCache{
		serviceSuffix: "svc." + domainSuffix,
		services:      services,
		configs:       configs,
		nodes:         make(map[string]time.Time),
	}
	out.snapshots = cache.NewSnapshotCache(false /* non-ADS mode */, out, nil /* add spam logger here */)
	out.server = server.NewServer(out.snapshots, out)
	return out
}

// RegisterOutput registers with a gRPC server
// TODO: make sure the inputs are initialized before serving
func (xds *ConfigCache) RegisterOutput(grpcServer *grpc.Server) {
	v2.RegisterRouteDiscoveryServiceServer(grpcServer, xds.server)
}

// RegisterInput connects with the controllers
// The route generation code needs to subscribe to:
// - collection of services
// - collection of virtual services
func (xds *ConfigCache) RegisterInput(services model.Controller, configs model.ConfigStoreCache) {
	if err := services.AppendServiceHandler(xds.OnServiceEvent); err != nil {
		log.Fatal(err)
	}
	configs.RegisterEventHandler(model.VirtualService.Type, xds.OnConfigEvent)
}

// ID uses string-encoded node ID
func (xds *ConfigCache) ID(node *core.Node) string {
	return node.GetId()
}

// OnServiceEvent ...
func (xds *ConfigCache) OnServiceEvent(svc *model.Service, event model.Event) {
	xds.Update()
}

// OnConfigEvent ...
func (xds *ConfigCache) OnConfigEvent(svc model.Config, event model.Event) {
	xds.Update()
}

// Update performs the global re-compute
// TODO make this more incremental
func (xds *ConfigCache) Update() {
	xds.mu.Lock()
	defer xds.mu.Unlock()

	xds.version++
	services, _ := xds.services.Services()
	virtualServices, _ := xds.configs.List(model.VirtualService.Type, model.NamespaceAll)
	xds.hosts = v1alpha3.TranslateVirtualHosts(virtualServices, v1alpha3.IndexServices(services), xds.serviceSuffix)
	for id := range xds.nodes {
		xds.updateID(id)
	}
}

func (xds *ConfigCache) updateID(id string) {
	proxy, _ := model.ParseServiceNode(id)
	switch proxy.Type {
	case model.Ingress: // TODO ingress nodes are deprecated?
		return
	case model.Router: // TODO router nodes are deprecated?
		return
	case model.Sidecar:
		// continue
	}
	labels := xds.services.GetProxyLabels(proxy)
	routes := v1alpha3.SpecializeGuardedHosts(xds.hosts, proxy.Domain, labels, v1alpha3.MeshGateway)
	_ = xds.snapshots.SetSnapshot(id, cache.Snapshot{
		Routes: cache.NewResources(fmt.Sprintf("%d", xds.version), routes),
	})
}

// OnStreamRequest ...
func (xds *ConfigCache) OnStreamRequest(_ int64, req *v2.DiscoveryRequest) {
	id := req.GetNode().GetId()

	xds.mu.Lock()
	if _, exists := xds.nodes[id]; !exists {
		xds.updateID(id)
	}
	xds.nodes[id] = time.Now()
	xds.mu.Unlock()
}

// OnStreamOpen ...
func (xds *ConfigCache) OnStreamOpen(int64, string) {}

// OnStreamClosed ...
func (xds *ConfigCache) OnStreamClosed(int64) {}

// OnStreamResponse ...
func (xds *ConfigCache) OnStreamResponse(int64, *v2.DiscoveryRequest, *v2.DiscoveryResponse) {}

// OnFetchRequest ...
func (xds *ConfigCache) OnFetchRequest(*v2.DiscoveryRequest) {}

// OnFetchResponse ...
func (xds *ConfigCache) OnFetchResponse(*v2.DiscoveryRequest, *v2.DiscoveryResponse) {}
