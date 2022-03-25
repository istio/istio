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
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

// Nds stands for Name Discovery Service. Istio agents send NDS requests to istiod
// istiod responds with a list of service entries and their associated IPs (including k8s services)
// The agent then updates its internal DNS based on this data. If DNS capture is enabled in the pod
// the agent will capture all DNS requests and attempt to resolve locally before forwarding to upstream
// dns servers/
type NdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &NdsGenerator{}

// Map of all configs that do not impact NDS
var skippedNdsConfigs = map[config.GroupVersionKind]struct{}{
	gvk.Gateway:               {},
	gvk.VirtualService:        {},
	gvk.DestinationRule:       {},
	gvk.EnvoyFilter:           {},
	gvk.WorkloadEntry:         {},
	gvk.WorkloadGroup:         {},
	gvk.AuthorizationPolicy:   {},
	gvk.RequestAuthentication: {},
	gvk.PeerAuthentication:    {},
	gvk.WasmPlugin:            {},
}

func ndsNeedsPush(req *model.PushRequest) bool {
	if req == nil {
		return true
	}
	if !req.Full {
		// NDS only handles full push
		return false
	}
	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	for config := range req.ConfigsUpdated {
		if _, f := skippedNdsConfigs[config.Kind]; !f {
			return true
		}
	}
	return false
}

func (n NdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !ndsNeedsPush(req) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	nt := n.Server.ConfigGenerator.BuildNameTable(proxy, req.Push)
	if nt == nil {
		return nil, model.DefaultXdsLogDetails, nil
	}
	resources := model.Resources{&discovery.Resource{Resource: util.MessageToAny(nt)}}
	return resources, model.DefaultXdsLogDetails, nil
}
