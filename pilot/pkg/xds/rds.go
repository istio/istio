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

type RdsGenerator struct {
	ConfigGenerator core.ConfigGenerator
}

var _ model.XdsDeltaResourceGenerator = &RdsGenerator{}

// Map of all configs that do not impact RDS
var skippedRdsConfigs = func() sets.Set[kind.Kind] {
	s := sets.New(
		kind.WorkloadEntry,
		kind.WorkloadGroup,
		kind.AuthorizationPolicy,
		kind.RequestAuthentication,
		kind.PeerAuthentication,
		kind.Secret,
		kind.WasmPlugin,
		kind.TrafficExtension,
		kind.Telemetry,
		kind.ProxyConfig,
		kind.DNSName,
		kind.Endpoints,
	)
	if features.ScopedAddressPushes {
		// Sidecar and gateway routes don't depend on ambient Address data; service changes that
		// affect them arrive as ServiceEntry/Endpoints updates. Waypoints are handled in rdsNeedsPush.
		s.Insert(kind.Address)
	}
	return s
}()

// rdsNeedsPush may return a new PushRequest with ConfigsUpdated filtered to only include configs that impact RDS,
// this is done because route generation checks if only some specific types of configs are present to enable delta generation.
func rdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) (*model.PushRequest, bool) {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return req, res
	}
	if proxy.Type == model.Waypoint && waypointNeedsPush(req, proxy) {
		return req, true
	}

	// Optimization: Skip RDS for headless endpoint updates. A route's cluster name is built from
	// the service hostname/port/subset (a static string), so it does not change when only
	// endpoints change. However, if ServiceUpdate is also present, the service definition changed
	// (ports, labels, etc.) and we need to push RDS.
	headlessOnly := req.Reason.Has(model.HeadlessEndpointUpdate) && !req.Reason.Has(model.ServiceUpdate)
	sawServiceEntry := false

	relevantUpdates := make(sets.Set[model.ConfigKey])
	filtered := false
	needsPush := false
	for config := range req.ConfigsUpdated {
		if headlessOnly {
			if config.Kind == kind.ServiceEntry {
				// Defer the decision on ServiceEntry until we know whether all updates are ServiceEntry.
				sawServiceEntry = true
				continue
			}
			// Not exclusively the headless endpoint marker; fall through to the normal check below.
			headlessOnly = false
		}

		if config.Kind == kind.Gateway {
			if proxy.Type == model.Router || proxy.IsAmbientEastWestGateway() {
				relevantUpdates.Insert(config)
				needsPush = true
			} else {
				filtered = true
			}
			continue
		}

		if !skippedRdsConfigs.Contains(config.Kind) {
			relevantUpdates.Insert(config)
			needsPush = true
		} else {
			filtered = true
		}
	}

	if headlessOnly {
		return req, false
	}

	if filtered {
		newPushRequest := *req
		newPushRequest.ConfigsUpdated = relevantUpdates
		req = &newPushRequest
	}

	// ServiceEntry updates only trigger a push here if they weren't exclusively headless endpoint markers.
	return req, needsPush || sawServiceEntry
}

func (c RdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	req, needsPush := rdsNeedsPush(req, proxy)
	if !needsPush {
		return nil, model.DefaultXdsLogDetails, nil
	}
	resources, logDetails := c.ConfigGenerator.BuildHTTPRoutes(proxy, req, w.ResourceNames.UnsortedList())
	return resources, logDetails, nil
}

// GenerateDeltas for RDS builds a true delta (only the route configurations affected by the
// current push's ConfigsUpdated) when features.EnableDeltaRDS is set and the update is
// precisely mappable; otherwise it falls back to rebuilding every watched route.
func (c RdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	req, needsPush := rdsNeedsPush(req, proxy)
	if !needsPush {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}
	updatedRoutes, removedRoutes, logs, usedDelta := c.ConfigGenerator.BuildDeltaHTTPRoutes(proxy, req, w)
	return updatedRoutes, removedRoutes, logs, usedDelta, nil
}
