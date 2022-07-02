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
	"istio.io/istio/pkg/config/schema/kind"
)

type LdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &LdsGenerator{}

// Map of all configs that do not impact LDS
var skippedLdsConfigs = map[model.NodeType]map[kind.Kind]struct{}{
	model.Router: {
		// for autopassthrough gateways, we build filterchains per-dr subset
		kind.WorkloadGroup: {},
		kind.WorkloadEntry: {},
		kind.Secret:        {},
		kind.ProxyConfig:   {},
	},
	model.SidecarProxy: {
		kind.Gateway:       {},
		kind.WorkloadGroup: {},
		kind.WorkloadEntry: {},
		kind.Secret:        {},
		kind.ProxyConfig:   {},
	},
}

func ldsNeedsPush(proxy *model.Proxy, req *model.PushRequest) bool {
	if req == nil {
		return true
	}
	if !req.Full {
		// LDS only handles full push
		return false
	}
	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	for config := range req.ConfigsUpdated {
		if _, f := skippedLdsConfigs[proxy.Type][config.Kind]; !f {
			return true
		}
	}
	return false
}

func (l LdsGenerator) Generate(proxy *model.Proxy, _ *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !ldsNeedsPush(proxy, req) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	listeners := l.Server.ConfigGenerator.BuildListeners(proxy, req.Push)
	resources := model.Resources{}
	for _, c := range listeners {
		resources = append(resources, &discovery.Resource{
			Name:     c.Name,
			Resource: util.MessageToAny(c),
		})
	}
	return resources, model.DefaultXdsLogDetails, nil
}
