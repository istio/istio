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

package driver

import (
	"context"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	extensionservice "github.com/envoyproxy/go-control-plane/envoy/service/extension/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/server/v3"
)

const (
	// APIType for extension configs.
	APIType = "type.googleapis.com/envoy.config.core.v3.TypedExtensionConfig"
)

// ExtensionServer is the main server instance.
type ExtensionServer struct {
	server.Server
	server.CallbackFuncs
	cache *cache.LinearCache
}

var _ extensionservice.ExtensionConfigDiscoveryServiceServer = &ExtensionServer{}

func NewExtensionServer(ctx context.Context) *ExtensionServer {
	out := &ExtensionServer{}
	out.cache = cache.NewLinearCache(APIType)
	out.Server = server.NewServer(ctx, out.cache, out)
	return out
}

func (es *ExtensionServer) StreamExtensionConfigs(stream extensionservice.ExtensionConfigDiscoveryService_StreamExtensionConfigsServer) error {
	return es.Server.StreamHandler(stream, APIType)
}

func (es *ExtensionServer) DeltaExtensionConfigs(stream extensionservice.ExtensionConfigDiscoveryService_DeltaExtensionConfigsServer) error {
	return es.Server.DeltaStreamHandler(stream, APIType)
}

func (es *ExtensionServer) FetchExtensionConfigs(ctx context.Context, req *discovery.DiscoveryRequest) (*discovery.DiscoveryResponse, error) {
	req.TypeUrl = APIType
	return es.Server.Fetch(ctx, req)
}

func (es *ExtensionServer) Update(config *core.TypedExtensionConfig) error {
	return es.cache.UpdateResource(config.Name, config)
}

func (es *ExtensionServer) Delete(name string) error {
	return es.cache.DeleteResource(name)
}
