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

func LbPriority(proxyLocality *core.Locality, endpointsLocality *core.Locality,
	proxyNetwork, endpointNetwork string, enabledNetworkFailover bool) int {
	if !enabledNetworkFailover {
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

	if model.IsSameNetwork(proxyNetwork, endpointNetwork) {
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
	return 5
}
