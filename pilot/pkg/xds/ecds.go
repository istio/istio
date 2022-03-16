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
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/schema/gvk"
)

// EcdsGenerator generates ECDS configuration.
type EcdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &EcdsGenerator{}

func ecdsNeedsPush(req *model.PushRequest) bool {
	if req == nil {
		return true
	}
	if !req.Full {
		// ECDS only handles full push
		return false
	}
	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	// Only push if config updates is triggered by EnvoyFilter or WasmPlugin.
	for config := range req.ConfigsUpdated {
		switch config.Kind {
		case gvk.EnvoyFilter:
			return true
		case gvk.WasmPlugin:
			return true
		}
	}
	return false
}

// Generate returns ECDS resources for a given proxy.
func (e *EcdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !ecdsNeedsPush(req) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	ec := e.Server.ConfigGenerator.BuildExtensionConfiguration(proxy, req.Push, w.ResourceNames)
	if ec == nil {
		return nil, model.DefaultXdsLogDetails, nil
	}

	resources := make(model.Resources, 0, len(ec))
	for _, c := range ec {
		resources = append(resources, &discovery.Resource{
			Name:     c.Name,
			Resource: util.MessageToAny(c),
		})
	}
	return resources, model.DefaultXdsLogDetails, nil
}
