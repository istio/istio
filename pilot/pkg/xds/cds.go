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
	if req == nil {
		return true
	}
	switch proxy.Type {
	case model.Waypoint:
		if model.HasConfigsOfKind(req.ConfigsUpdated, kind.Address) {
			// TODO: this logic is identical to that used in LDS, consider refactor into a common function
			// taken directly from LDS... waypoints need CDS updates on kind.Address changes
			// after implementing use-waypoint which decouples waypoint creation, wl pod creation
			// user specifying waypoint use. Without this we're not getting correct waypoint config
			// in a timely manner
			return true
		}
		// Otherwise, only handle full pushes (skip endpoint-only updates)
		if !req.Full {
			return false
		}
	default:
		if !req.Full {
			// CDS only handles full push
			return false
		}
	}
	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	for config := range req.ConfigsUpdated {
		if proxy.Type == model.Router {
			if features.FilterGatewayClusterConfig {
				if _, f := pushCdsGatewayConfig[config.Kind]; f {
					return true
				}
			}
			if proxy.MergedGateway.ContainsAutoPassthroughGateways && config.Kind == kind.Gateway {
				return true
			}
		}

		if _, f := skippedCdsConfigs[config.Kind]; !f {
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
