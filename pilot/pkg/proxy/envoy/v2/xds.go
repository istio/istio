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
)

// ConfigCache for xDS resources
type ConfigCache struct {
	services model.ServiceDiscovery
	configs  model.ConfigStore

	snapshots cache.SnapshotCache
	server    server.Server
}

// NewConfigCache spins up a new config cache
func NewConfigCache(services model.ServiceDiscovery, configs model.ConfigStore) *ConfigCache {
	out := &ConfigCache{
		services: services,
		configs:  configs,
	}
	out.snapshots = cache.NewSnapshotCache(false, out, nil)
	out.server = server.NewServer(out.snapshots, out)
	return out
}

// Register with gRPC server
func (cache *ConfigCache) Register(grpcServer *grpc.Server) {
	v2.RegisterRouteDiscoveryServiceServer(grpcServer, cache.server)
	// v2.RegisterClusterDiscoveryServiceServer(grpcServer, cache.server)
	// v2.RegisterListenerDiscoveryServiceServer(grpcServer, cache.server)
}

// RegisterInput connects with the controllers
func (cache *ConfigCache) RegisterInput(services model.Controller, configs model.ConfigStoreCache) {
	if err := services.AppendServiceHandler(cache.OnServiceEvent); err != nil {
		log.Fatal(err)
	}
	configs.RegisterEventHandler(model.VirtualService.Type, cache.OnConfigEvent)
	configs.RegisterEventHandler(model.DestinationRule.Type, cache.OnConfigEvent)
}

// ID ...
func (cache *ConfigCache) ID(node *core.Node) string {
	return node.GetId()
}

// OnServiceEvent ...
func (cache *ConfigCache) OnServiceEvent(svc *model.Service, event model.Event) {
	// TODO
}

// OnConfigEvent ...
func (cache *ConfigCache) OnConfigEvent(svc model.Config, event model.Event) {
	// TODO
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
