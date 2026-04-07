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
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type CdsGenerator struct {
	ConfigGenerator core.ConfigGenerator
}

var _ model.XdsDeltaResourceGenerator = &CdsGenerator{}

// cdsNeedsPush may return a new PushRequest with ConfigsUpdated filtered to only include configs that impact CDS,
// this is done because cluster generator checks if only some specific types of configs are present to enable delta generation.
func (c CdsGenerator) cdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) (*model.PushRequest, bool) {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return req, res
	}
	if proxy.Type == model.Waypoint && waypointNeedsPush(req) {
		return req, true
	}

	relevantUpdates := make(sets.Set[model.ConfigKey])
	filtered := false
	checkGateway := false
	gatewayNeedsPush := false
	for config := range req.ConfigsUpdated {
		if !c.ConfigGenerator.ClusterAffectingConfig(proxy.Type, config.Kind) {
			filtered = true
			continue
		}

		if proxy.Type == model.Router && config.Kind == kind.Gateway {
			if !checkGateway {
				checkGateway = true
				autoPassthroughModeChanged := proxy.MergedGateway.HasAutoPassthroughGateways() != proxy.PrevMergedGateway.HasAutoPassthroughGateway()
				autoPassthroughHostsChanged := !proxy.MergedGateway.GetAutoPassthroughGatewaySNIHosts().Equals(proxy.PrevMergedGateway.GetAutoPassthroughSNIHosts())
				gwNamesChanged := proxy.MergedGateway == nil || !slices.EqualUnordered(proxy.MergedGateway.GetGatewayNames(), proxy.PrevMergedGateway.GetGatewayNames())
				if autoPassthroughModeChanged || autoPassthroughHostsChanged || gwNamesChanged {
					gatewayNeedsPush = true
				}
			}

			if !gatewayNeedsPush {
				filtered = true
				continue
			}
		}
		relevantUpdates.Insert(config)
	}

	if filtered {
		newPushRequest := *req
		newPushRequest.ConfigsUpdated = relevantUpdates
		req = &newPushRequest
	}

	return req, len(req.ConfigsUpdated) > 0
}

func (c CdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	req, needsPush := c.cdsNeedsPush(req, proxy)
	if !needsPush {
		return nil, model.DefaultXdsLogDetails, nil
	}
	clusters, logs := c.ConfigGenerator.BuildClusters(req, proxy, proxy.SidecarScope, req.Push)
	return clusters, logs, nil
}

// GenerateDeltas for CDS currently only builds deltas when services change. todo implement changes for DestinationRule, etc
func (c CdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	req, needsPush := c.cdsNeedsPush(req, proxy)
	if !needsPush {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}
	updatedClusters, removedClusters, logs, usedDelta := c.ConfigGenerator.BuildDeltaClusters(req, proxy, proxy.SidecarScope, proxy.PrevSidecarScope, req.Push, w)
	return updatedClusters, removedClusters, logs, usedDelta, nil
}
