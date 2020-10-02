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
	"context"
	"sync"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	extensionservice "github.com/envoyproxy/go-control-plane/envoy/service/extension/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

const (
	// ExtensionConfigTypeURL for extension configs.
	ExtensionConfigTypeURL = "type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"
)

// ExtensionServer is the main server instance.
type ExtensionServer struct {
	sync.Mutex
	server.Server
	server.CallbackFuncs
	cache   *cache.LinearCache
	configs map[string]*core.TypedExtensionConfig
}

var _ extensionservice.ExtensionConfigDiscoveryServiceServer = &ExtensionServer{}

func NewExtensionServer() *ExtensionServer {
	out := &ExtensionServer{}
	out.cache = cache.NewLinearCache(ExtensionConfigTypeURL, cache.WithVersionPrefix(time.Now().Format(time.RFC3339)+"/"))
	out.Server = server.NewServer(context.Background(), out.cache, out)
	out.configs = make(map[string]*core.TypedExtensionConfig)
	return out
}

func (es *ExtensionServer) StreamExtensionConfigs(stream extensionservice.ExtensionConfigDiscoveryService_StreamExtensionConfigsServer) error {
	return es.Server.StreamHandler(stream, ExtensionConfigTypeURL)
}
func (es *ExtensionServer) DeltaExtensionConfigs(_ extensionservice.ExtensionConfigDiscoveryService_DeltaExtensionConfigsServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}
func (es *ExtensionServer) FetchExtensionConfigs(ctx context.Context, req *discovery.DiscoveryRequest) (*discovery.DiscoveryResponse, error) {
	req.TypeUrl = ExtensionConfigTypeURL
	return es.Server.Fetch(ctx, req)
}

// OnStreamRequest qualifies the requested resource name by the namespace
func (es *ExtensionServer) OnStreamRequest(streamID int64, req *discovery.DiscoveryRequest) error {
	ns := ""
	if req.Node.Metadata != nil {
		if field, ok := req.Node.Metadata.Fields["NAMESPACE"]; ok {
			ns = field.GetStringValue()
		}
	}
	names := make([]string, 0, len(req.ResourceNames))
	for _, name := range req.ResourceNames {
		names = append(names, ns+"/"+name)
	}
	req.ResourceNames = names
	return nil
}

// Update scans through all EnvoyFilters, identifies ECDS, and reconciles the serving cache.
// This can be invoked from multiple go-routines executing AdsPushAll.
func (es *ExtensionServer) Update(req *model.PushRequest) {
	es.Lock()
	defer es.Unlock()

	staged := make(map[string]*core.TypedExtensionConfig)
	allEnvoyFilters := req.Push.AllEnvoyFilters()
	for ns, configs := range allEnvoyFilters {
		if ns == req.Push.Mesh.RootNamespace {
			continue
		}
		for _, config := range configs {
			if config.Patches != nil {
				for _, patch := range config.Patches[networking.EnvoyFilter_EXTENSION_CONFIG] {
					value := patch.Value.(*core.TypedExtensionConfig)
					staged[ns+"/"+value.Name] = value
				}
			}
		}
	}
	for _, config := range allEnvoyFilters[req.Push.Mesh.RootNamespace] {
		if config.Patches != nil {
			for _, patch := range config.Patches[networking.EnvoyFilter_EXTENSION_CONFIG] {
				value := patch.Value.(*core.TypedExtensionConfig)
				for ns := range allEnvoyFilters {
					index := ns + "/" + value.Name
					if _, exists := staged[index]; !exists {
						staged[index] = value
					}
				}
			}
		}
	}
	for qualifier, config := range staged {
		prev := es.configs[qualifier]
		if !proto.Equal(prev, config) {
			_ = es.cache.UpdateResource(qualifier, config) // nolint: errcheck
		}
		delete(es.configs, qualifier)
	}
	for qualifier := range es.configs {
		_ = es.cache.DeleteResource(qualifier) // nolint: errcheck
	}
	es.configs = staged
}
