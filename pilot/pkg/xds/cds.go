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
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/util/sets"
)

type CdsGenerator struct {
	ConfigGenerator core.ConfigGenerator
}

var _ model.XdsDeltaResourceGenerator = &CdsGenerator{}

// Map of all configs that do not impact CDS
var skippedCdsConfigs = sets.New(
	kind.Gateway,
	kind.WorkloadEntry,
	kind.WorkloadGroup,
	kind.AuthorizationPolicy,
	kind.RequestAuthentication,
	kind.Secret,
	kind.Telemetry,
	kind.WasmPlugin,
	kind.ProxyConfig,
	kind.DNSName,
)

// Map all configs that impact CDS for gateways when `PILOT_FILTER_GATEWAY_CLUSTER_CONFIG = true`.
var pushCdsGatewayConfig = func() sets.Set[kind.Kind] {
	s := sets.New(
		kind.VirtualService,
		kind.Gateway,
	)
	if features.JwksFetchMode != jwt.Istiod {
		s.Insert(kind.RequestAuthentication)
	}
	return s
}()

// cdsNeedsPush may return a new PushRequest with ConfigsUpdated filtered to only include configs that impact CDS,
// this is done because cluster generator checks if only some specific types of configs are present to enable delta generation.
func cdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) (*model.PushRequest, bool) {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return req, res
	}
	if proxy.Type == model.Waypoint && waypointNeedsPush(req) {
		return req, true
	}
	if !req.Full {
		return req, false
	}

	relevantUpdates := make(sets.Set[model.ConfigKey])
	filtered := false
	checkGateway := false
	for config := range req.ConfigsUpdated {
		if proxy.Type == model.Router {
			if config.Kind == kind.Gateway {
				// Do the check outside of the loop since its slow; just trigger we need it
				checkGateway = true
			}
			if features.FilterGatewayClusterConfig {
				if _, f := pushCdsGatewayConfig[config.Kind]; f {
					relevantUpdates.Insert(config)
					continue
				}
			}
			if config.Kind == kind.VirtualService {
				// We largely don't use VirtualService for CDS building. However, we do use it as part of Sidecar scoping, which
				// implicitly includes VS destinations.
				// Since Routers do not use Sidecar, though, we can skip for Router.
				filtered = true
				continue
			}
		}

		if _, f := skippedCdsConfigs[config.Kind]; !f {
			relevantUpdates.Insert(config)
		} else {
			// we filtered a config
			filtered = true
		}
	}

	needsPush := false

	if checkGateway {
		autoPassthroughModeChanged := proxy.MergedGateway.HasAutoPassthroughGateways() != proxy.PrevMergedGateway.HasAutoPassthroughGateway()
		autoPassthroughHostsChanged := !proxy.MergedGateway.GetAutoPassthroughGatewaySNIHosts().Equals(proxy.PrevMergedGateway.GetAutoPassthroughSNIHosts())
		if autoPassthroughModeChanged || autoPassthroughHostsChanged {
			needsPush = true
		}
	}

	if filtered {
		newPushRequest := *req
		newPushRequest.ConfigsUpdated = relevantUpdates
		req = &newPushRequest
	}

	return req, needsPush || len(req.ConfigsUpdated) > 0
}

func (c CdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	req, needsPush := cdsNeedsPush(req, proxy)
	if !needsPush {
		return nil, model.DefaultXdsLogDetails, nil
	}
	clusters, logs := c.ConfigGenerator.BuildClusters(proxy, req)
	return clusters, logs, nil
}

// GenerateDeltas for CDS currently only builds deltas when services change. todo implement changes for DestinationRule, etc
func (c CdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	req, needsPush := cdsNeedsPush(req, proxy)
	if !needsPush {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}
	updatedClusters, removedClusters, logs, usedDelta := c.ConfigGenerator.BuildDeltaClusters(proxy, req, w)
	return updatedClusters, removedClusters, logs, usedDelta, nil
}
