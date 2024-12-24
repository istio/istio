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

	kind.KubernetesGateway,
)

// Map all configs that impact CDS for gateways when `PILOT_FILTER_GATEWAY_CLUSTER_CONFIG = true`.
var pushCdsGatewayConfig = func() sets.Set[kind.Kind] {
	s := sets.New(
		kind.VirtualService,
		kind.Gateway,

		kind.KubernetesGateway,
		kind.HTTPRoute,
		kind.TCPRoute,
		kind.TLSRoute,
		kind.GRPCRoute,
	)
	if features.JwksFetchMode != jwt.Istiod {
		s.Insert(kind.RequestAuthentication)
	}
	return s
}()

func cdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return res
	}
	if proxy.Type == model.Waypoint && waypointNeedsPush(req) {
		return true
	}
	if !req.Full {
		return false
	}
	checkGateway := false
	for config := range req.ConfigsUpdated {
		if proxy.Type == model.Router {
			if features.FilterGatewayClusterConfig {
				if _, f := pushCdsGatewayConfig[config.Kind]; f {
					return true
				}
			}
			if config.Kind == kind.Gateway {
				// Do the check outside of the loop since its slow; just trigger we need it
				checkGateway = true
			}
		}

		if _, f := skippedCdsConfigs[config.Kind]; !f {
			return true
		}
	}
	if checkGateway {
		autoPassthroughModeChanged := proxy.MergedGateway.HasAutoPassthroughGateways() != proxy.PrevMergedGateway.HasAutoPassthroughGateway()
		autoPassthroughHostsChanged := !proxy.MergedGateway.GetAutoPassthroughGatewaySNIHosts().Equals(proxy.PrevMergedGateway.GetAutoPassthroughSNIHosts())
		if autoPassthroughModeChanged || autoPassthroughHostsChanged {
			return true
		}
	}
	return false
}

func (c CdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !cdsNeedsPush(req, proxy) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	clusters, logs := c.ConfigGenerator.BuildClusters(proxy, req)
	return clusters, logs, nil
}

// GenerateDeltas for CDS currently only builds deltas when services change. todo implement changes for DestinationRule, etc
func (c CdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	if !cdsNeedsPush(req, proxy) {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}
	updatedClusters, removedClusters, logs, usedDelta := c.ConfigGenerator.BuildDeltaClusters(proxy, req, w)
	return updatedClusters, removedClusters, logs, usedDelta, nil
}
