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
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// NdsGenerator generates config for Nds i.e. Name Discovery Service. Istio agents
// send NDS requests to istiod and istiod responds with a list of services and their
// associated IPs (including service entries).
// The agent then updates its internal DNS based on this data. If DNS capture is enabled
// in the pod the agent will capture all DNS requests and attempt to resolve locally before
// forwarding to upstream dns servers.
type NdsGenerator struct {
	ConfigGenerator core.ConfigGenerator
}

var _ model.XdsResourceGenerator = &NdsGenerator{}

var _ model.XdsDeltaResourceGenerator = &NdsGenerator{}

// Map of all configs that do not impact NDS
var skippedNdsConfigs = func() sets.Set[kind.Kind] {
	s := sets.New(
		kind.Gateway,
		kind.VirtualService,
		kind.DestinationRule,
		kind.Secret,
		kind.Telemetry,
		kind.EnvoyFilter,
		kind.WorkloadEntry,
		kind.WorkloadGroup,
		kind.AuthorizationPolicy,
		kind.RequestAuthentication,
		kind.PeerAuthentication,
		kind.WasmPlugin,
		kind.TrafficExtension,
		kind.ProxyConfig,
		kind.MeshConfig,
		kind.Endpoints,
	)
	if features.ScopedAddressPushes {
		// The DNS name table is derived from services; ambient Address changes that matter
		// to it arrive as ServiceEntry updates.
		s.Insert(kind.Address)
	}
	return s
}()

// ndsNeedsPush returns a filtered PushRequest (including only NDS-relevant kinds) and whether NDS needs to be pushed.
func ndsNeedsPush(req *model.PushRequest, proxy *model.Proxy) (*model.PushRequest, bool) {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return req, res
	}
	relevantUpdates := make(sets.Set[model.ConfigKey])
	filtered := false
	for config := range req.ConfigsUpdated {
		if !skippedNdsConfigs.Contains(config.Kind) {
			relevantUpdates.Insert(config)
		} else {
			filtered = true
		}
	}
	if filtered {
		newReq := *req
		newReq.ConfigsUpdated = relevantUpdates
		req = &newReq
	}
	return req, len(req.ConfigsUpdated) > 0
}

func (n NdsGenerator) Generate(proxy *model.Proxy, _ *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	req, needsPush := ndsNeedsPush(req, proxy)
	if !needsPush {
		return nil, model.DefaultXdsLogDetails, nil
	}
	resources, logs := n.ConfigGenerator.BuildNameTable(proxy, req.Push)
	return resources, logs, nil
}

func (n NdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	req, needsPush := ndsNeedsPush(req, proxy)
	if !needsPush {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}
	res, removed, log, usedDelta := n.ConfigGenerator.BuildDeltaNameTable(proxy, req, w)
	return res, removed, log, usedDelta, nil
}
