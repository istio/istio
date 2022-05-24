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
)

type CdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsDeltaResourceGenerator = &CdsGenerator{}

func cdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if req == nil {
		return true
	}
	if !req.Full {
		// CDS only handles full push
		return false
	}
	// EDS and CDS share the same logic. This is necessary due to Envoy's warming semantics:
	// https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/cluster_manager.html?highlight=warming#cluster-warming
	return edsNeedsPush(req, proxy)
}

func (c CdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !cdsNeedsPush(req, proxy) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	clusters, logs := c.Server.ConfigGenerator.BuildClusters(proxy, req)
	return clusters, logs, nil
}

// GenerateDeltas for CDS currently only builds deltas when services change. todo implement changes for DestinationRule, etc
func (c CdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	if !cdsNeedsPush(req, proxy) {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}
	updatedClusters, removedClusters, logs, usedDelta := c.Server.ConfigGenerator.BuildDeltaClusters(proxy, req, w)
	return updatedClusters, removedClusters, logs, usedDelta, nil
}
