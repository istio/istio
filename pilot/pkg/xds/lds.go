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

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

type LdsGenerator struct {
	ConfigGenerator core.ConfigGenerator
}

var _ model.XdsResourceGenerator = &LdsGenerator{}

// Map of all configs that do not impact LDS
var skippedLdsConfigs = map[model.NodeType]sets.Set[kind.Kind]{
	model.Router: sets.New(
		// for autopassthrough gateways, we build filterchains per-dr subset
		kind.WorkloadGroup,
		kind.WorkloadEntry,
		kind.Secret,
		kind.ProxyConfig,
		kind.DNSName,
		kind.Endpoints,
		kind.Address,
	),
	model.SidecarProxy: sets.New(
		kind.Gateway,
		kind.WorkloadGroup,
		kind.WorkloadEntry,
		kind.Secret,
		kind.ProxyConfig,
		kind.DNSName,
		kind.Endpoints,
		kind.Address,
	),
	model.Waypoint: func() sets.Set[kind.Kind] {
		s := sets.New(
			kind.Gateway,
			kind.WorkloadGroup,
			kind.WorkloadEntry,
			kind.Secret,
			kind.ProxyConfig,
			kind.DNSName,
			kind.Endpoints,
		)
		if features.ScopedAddressPushes {
			// Address changes are handled by waypointNeedsPush, scoped to the affected waypoints
			s.Insert(kind.Address)
		}
		return s
	}(),
}

func ldsNeedsPush(proxy *model.Proxy, req *model.PushRequest) bool {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return res
	}
	if proxy.Type == model.Waypoint && waypointNeedsPush(req, proxy) {
		return true
	}

	// Optimization: Routers don't need LDS updates for headless endpoint changes.
	// However, if ServiceUpdate is also present, the service definition changed
	// (ports, labels, etc.) and we need to push LDS.
	headlessOnly := proxy.Type == model.Router && req.Reason.Has(model.HeadlessEndpointUpdate) && !req.Reason.Has(model.ServiceUpdate)
	sawServiceEntry := false

	for config := range req.ConfigsUpdated {
		if headlessOnly {
			if config.Kind == kind.ServiceEntry {
				// Defer the decision on ServiceEntry until we know whether all updates are ServiceEntry.
				sawServiceEntry = true
				continue
			}
			// Check if all updates are ServiceEntry (headless endpoint marker); if so, and this is a
			// headless-only update, none of them need to trigger a push on their own.
			headlessOnly = false
		}
		if !skippedLdsConfigs[proxy.Type].Contains(config.Kind) {
			if config.Kind == kind.PeerAuthentication && config.Namespace != proxy.ConfigNamespace &&
				config.Namespace != req.Push.Mesh.RootNamespace {
				// PeerAuthentication of the configNamespace or rootNamespace can only impact lds
				continue
			}
			return true
		}
	}
	// ServiceEntry updates only trigger a push here if they weren't exclusively headless endpoint markers.
	return sawServiceEntry && !headlessOnly
}

func (l LdsGenerator) Generate(proxy *model.Proxy, _ *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !ldsNeedsPush(proxy, req) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	listeners := l.ConfigGenerator.BuildListeners(proxy, req.Push)
	resources := model.Resources{}
	for _, c := range listeners {
		resources = append(resources, &discovery.Resource{
			Name:     c.Name,
			Resource: protoconv.MessageToAny(c),
		})
	}
	return resources, model.DefaultXdsLogDetails, nil
}
