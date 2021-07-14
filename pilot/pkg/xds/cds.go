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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

type CdsGenerator struct {
	Server *DiscoveryServer
}


var _ model.XdsResourceGenerator = &CdsGenerator{}

// Map of all configs that do not impact CDS
var SkippedCdsConfigs = map[config.GroupVersionKind]struct{}{
	gvk.Gateway:               {},
	gvk.WorkloadEntry:         {},
	gvk.WorkloadGroup:         {},
	gvk.AuthorizationPolicy:   {},
	gvk.RequestAuthentication: {},
	gvk.Secret:                {},
}

// Map all configs that impacts CDS for gateways.
var PushCdsGatewayConfig = map[config.GroupVersionKind]struct{}{
	gvk.VirtualService: {},
	gvk.Gateway:        {},
}

func cdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if req == nil {
		return true
	}
	if !req.Full {
		// CDS only handles full push
		return false
	}
	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	for config := range req.ConfigsUpdated {
		if proxy.Type == model.Router {
			if _, f := PushCdsGatewayConfig[config.Kind]; f {
				return true
			}
		}

		if _, f := SkippedCdsConfigs[config.Kind]; !f {
			return true
		}
	}
	return false
}

func (c CdsGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates *model.PushRequest, delta bool) (model.Resources, model.XdsLogDetails, error) {
	// todo -- right now, we push some clusters (inbound for sidecars, passthrough and blackhole for sidecars and gateways) regardless.
	// 	for delta, therefore, it doesn't make sense to check if we need to push on a per-resource basis, for now, scoping to a per-type basis should
	// 	be a good first step.
	if !cdsNeedsPush(updates, proxy) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	clusters, logs := c.Server.ConfigGenerator.BuildClusters(proxy, push, delta, updates, w)
	return clusters, logs, nil
}

