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

package loadbalancer

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
)

func localityMatch(proxyLocality *core.Locality, ruleLocality string) bool {
	ruleRegion, ruleZone, ruleSubzone := model.SplitLocalityLabel(ruleLocality)
	regionMatch := ruleRegion == "*" || proxyLocality.GetRegion() == ruleRegion
	zoneMatch := ruleZone == "*" || ruleZone == "" || proxyLocality.GetZone() == ruleZone
	subzoneMatch := ruleSubzone == "*" || ruleSubzone == "" || proxyLocality.GetSubZone() == ruleSubzone

	if regionMatch && zoneMatch && subzoneMatch {
		return true
	}
	return false
}

func localityLbPriority(proxyLocality *core.Locality, endpointsLocality *core.Locality) int {
	if proxyLocality.GetRegion() == endpointsLocality.GetRegion() {
		if proxyLocality.GetZone() == endpointsLocality.GetZone() {
			if proxyLocality.GetSubZone() == endpointsLocality.GetSubZone() {
				return 0
			}
			return 1
		}
		return 2
	}
	return 3
}

func topologyLbPriority(proxyLocality *core.Locality, endpointsLocality *core.Locality,
	proxyNetwork, endpointNetwork string, topologyKeys []*networking.TopologyKeys) int {
	// If not matched, the priority equals length of topology keys list
	p := len(topologyKeys)
	for priority, keys := range topologyKeys {
		if topologyKeysMatched(proxyLocality, endpointsLocality, proxyNetwork, endpointNetwork, keys.GetKeys()) {
			p = priority
			break
		}
	}

	return p
}

func topologyKeysMatched(proxyLocality *core.Locality, endpointsLocality *core.Locality,
	proxyNetwork, endpointNetwork string, keys []networking.TopologyKeys_TopologyKey) bool {
	for _, key := range keys {
		switch key {
		case networking.TopologyKeys_NETWORK:
			if proxyNetwork != endpointNetwork {
				return false
			}
		case networking.TopologyKeys_REGION:
			if proxyLocality.GetRegion() != endpointsLocality.GetRegion() {
				return false
			}
		case networking.TopologyKeys_ZONE:
			if proxyLocality.GetZone() != endpointsLocality.GetZone() {
				return false
			}
		case networking.TopologyKeys_SUBZONE:
			if proxyLocality.GetSubZone() != endpointsLocality.GetSubZone() {
				return false
			}
		}
	}

	return true
}
