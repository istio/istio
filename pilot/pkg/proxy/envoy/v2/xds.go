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
	"log"

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

	services model.ServiceDiscovery
	configs  model.ConfigStore

	snapshots cache.SnapshotCache
	server    server.Server
}

// NewConfigCache spins up a new config cache
func NewConfigCache(services model.ServiceDiscovery, configs model.ConfigStore, domainSuffix string) *ConfigCache {
	out := &ConfigCache{
		serviceSuffix: "svc." + domainSuffix,
		services:      services,
		configs:       configs,
	}
	out.snapshots = cache.NewSnapshotCache(false /* non-ADS mode */, out, nil)
	out.server = server.NewServer(out.snapshots, out)
	return out
}

// RegisterOutput registers with a gRPC server
func (cache *ConfigCache) RegisterOutput(grpcServer *grpc.Server) {
	v2.RegisterRouteDiscoveryServiceServer(grpcServer, cache.server)
}

// RegisterInput connects with the controllers
// The route generation code needs to subscribe to:
// - collection of services
// - collection of virtual services
func (cache *ConfigCache) RegisterInput(services model.Controller, configs model.ConfigStoreCache) {
	if err := services.AppendServiceHandler(cache.OnServiceEvent); err != nil {
		log.Fatal(err)
	}
	configs.RegisterEventHandler(model.VirtualService.Type, cache.OnConfigEvent)
}

// ID ...
func (cache *ConfigCache) ID(node *core.Node) string {
	return node.GetId()
}

// OnServiceEvent ...
func (cache *ConfigCache) OnServiceEvent(svc *model.Service, event model.Event) {
	cache.update()
}

// OnConfigEvent ...
func (cache *ConfigCache) OnConfigEvent(svc model.Config, event model.Event) {
	cache.update()
}

// non-incremental update
// TODO make this more incremental
func (cache *ConfigCache) update() {
	virtualServices, _ := cache.configs.List(model.VirtualService.Type, model.NamespaceAll)
	hosts := v1alpha3.TranslateVirtualHosts(virtualServices, nil, cache.serviceSuffix)
	_ = hosts
}

// OnStreamOpen ...
func (cache *ConfigCache) OnStreamOpen(int64, string) {}

// OnStreamClosed ...
func (cache *ConfigCache) OnStreamClosed(int64) {}

// OnStreamRequest ...
func (cache *ConfigCache) OnStreamRequest(int64, *v2.DiscoveryRequest) {
	// TODO
}

// OnStreamResponse ...
func (cache *ConfigCache) OnStreamResponse(int64, *v2.DiscoveryRequest, *v2.DiscoveryResponse) {}

// OnFetchRequest ...
func (cache *ConfigCache) OnFetchRequest(*v2.DiscoveryRequest) {}

// OnFetchResponse ...
func (cache *ConfigCache) OnFetchResponse(*v2.DiscoveryRequest, *v2.DiscoveryResponse) {}
