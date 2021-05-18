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

// packages used for load balancer setting
package loadbalancer

import (
	"math"
	"sort"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

func GetLocalityLbSetting(
	mesh *v1alpha3.LocalityLoadBalancerSetting,
	destrule *v1alpha3.LocalityLoadBalancerSetting,
) *v1alpha3.LocalityLoadBalancerSetting {
	var enabled bool
	// Locality lb is enabled if its not explicitly disabled in mesh global config
	if mesh != nil && (mesh.Enabled == nil || mesh.Enabled.Value) {
		enabled = true
	}
	// Unless we explicitly override this in destination rule
	if destrule != nil {
		if destrule.Enabled != nil && !destrule.Enabled.Value {
			enabled = false
		} else {
			enabled = true
		}
	}
	if !enabled {
		return nil
	}

	// Destination Rule overrides mesh config. If its defined, use that
	if destrule != nil {
		return destrule
	}
	// Otherwise fall back to mesh default
	return mesh
}

func GetNetworkLbSetting(
	mesh *v1alpha3.NetworkLoadBalancerSetting,
	destrule *v1alpha3.NetworkLoadBalancerSetting,
) *v1alpha3.NetworkLoadBalancerSetting {
	var enabled bool
	// Locality lb is enabled if its not explicitly disabled in mesh global config
	if mesh != nil && (mesh.Enabled == nil || mesh.Enabled.Value) {
		enabled = true
	}
	// Unless we explicitly override this in destination rule
	if destrule != nil {
		if destrule.Enabled != nil && !destrule.Enabled.Value {
			enabled = false
		} else {
			enabled = true
		}
	}
	if !enabled {
		return nil
	}

	// Destination Rule overrides mesh config. If its defined, use that
	if destrule != nil {
		return destrule
	}
	// Otherwise fall back to mesh default
	return mesh
}

type TopologyFailOver struct {
	From string
	To   []string
}

func ApplyLBSetting(
	loadAssignment *endpoint.ClusterLoadAssignment,
	proxyLocality *core.Locality,
	localityLB *v1alpha3.LocalityLoadBalancerSetting,
	proxyNetwork string,
	networkLB *v1alpha3.NetworkLoadBalancerSetting,
	enableFailover bool,
) {
	if localityLB == nil && networkLB == nil {
		return
	}
	if networkLB == nil && proxyLocality == nil {
		return
	}

	// one of Distribute or Failover settings can be applied.
	if localityLB.GetDistribute() != nil {
		applyLocalityWeight(proxyLocality, loadAssignment, localityLB.GetDistribute())
		// Failover needs outlier detection, otherwise Envoy will never drop down to a lower priority.
		// Do not apply default failover when locality LB is disabled.
	} else if enableFailover {
		if networkLB == nil {
			applyLocalityFailover(proxyLocality, loadAssignment, localityLB.Failover)
		}
		if localityLB == nil {
			applyNetworkFailover(loadAssignment, proxyNetwork, networkLB.Failover)
		}
		if networkLB != nil && localityLB != nil {
			applyTopologyFailover(loadAssignment, proxyLocality, localityLB.Failover, proxyNetwork, networkLB.Failover)
		}
	}
}

// set locality loadbalancing weight
func applyLocalityWeight(
	locality *core.Locality,
	loadAssignment *endpoint.ClusterLoadAssignment,
	distribute []*v1alpha3.LocalityLoadBalancerSetting_Distribute) {
	if distribute == nil {
		return
	}

	// Support Locality weighted load balancing
	// (https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/load_balancing/locality_weight#locality-weighted-load-balancing)
	// by providing weights in LocalityLbEndpoints via load_balancing_weight.
	// By setting weights across different localities, it can allow
	// Envoy to weight assignments across different zones and geographical locations.
	for _, localityWeightSetting := range distribute {
		if localityWeightSetting != nil &&
			localityMatch(locality, localityWeightSetting.From) {
			misMatched := map[int]struct{}{}
			for i := range loadAssignment.Endpoints {
				misMatched[i] = struct{}{}
			}
			for locality, weight := range localityWeightSetting.To {
				// index -> original weight
				destLocMap := map[int]uint32{}
				totalWeight := uint32(0)
				for i, ep := range loadAssignment.Endpoints {
					if _, exist := misMatched[i]; exist {
						if localityMatch(ep.Locality, locality) {
							delete(misMatched, i)
							if ep.LoadBalancingWeight != nil {
								destLocMap[i] = ep.LoadBalancingWeight.Value
							} else {
								destLocMap[i] = 1
							}
							totalWeight += destLocMap[i]
						}
					}
				}
				// in case wildcard dest matching multi groups of endpoints
				// the load balancing weight for a locality is divided by the sum of the weights of all localities
				for index, originalWeight := range destLocMap {
					destWeight := float64(originalWeight*weight) / float64(totalWeight)
					if destWeight > 0 {
						loadAssignment.Endpoints[index].LoadBalancingWeight = &wrappers.UInt32Value{
							Value: uint32(math.Ceil(destWeight)),
						}
					}
				}
			}

			// remove groups of endpoints in a locality that miss matched
			for i := range misMatched {
				loadAssignment.Endpoints[i].LbEndpoints = nil
			}
			break
		}
	}
}

// set locality loadbalancing priority
func applyLocalityFailover(
	locality *core.Locality,
	loadAssignment *endpoint.ClusterLoadAssignment,
	failover []*v1alpha3.LocalityLoadBalancerSetting_Failover) {
	// key is priority, value is the index of the LocalityLbEndpoints in ClusterLoadAssignment
	priorityMap := map[int][]int{}

	// 1. calculate the LocalityLbEndpoints.Priority compared with proxy locality
	for i, localityEndpoint := range loadAssignment.Endpoints {
		// if region/zone/subZone all match, the priority is 0.
		// if region/zone match, the priority is 1.
		// if region matches, the priority is 2.
		// if locality not match, the priority is 3.
		priority := LbPriority(locality, localityEndpoint.Locality, "", "", false)
		// region not match, apply failover settings when specified
		// update localityLbEndpoints' priority to 4 if failover not match
		if priority == 3 {
			for _, failoverSetting := range failover {
				if failoverSetting.From == locality.Region {
					if localityEndpoint.Locality == nil || localityEndpoint.Locality.Region != failoverSetting.To {
						priority = 4
					}
					break
				}
			}
		}
		loadAssignment.Endpoints[i].Priority = uint32(priority)
		priorityMap[priority] = append(priorityMap[priority], i)
	}

	refreshPriority(loadAssignment, priorityMap)
}

// set network loadbalancing priority
func applyNetworkFailover(
	loadAssignment *endpoint.ClusterLoadAssignment,
	proxyNetwork string,
	networkFailover []*v1alpha3.NetworkLoadBalancerSetting_Failover) {
	// key is priority, value is the index of the LocalityLbEndpoints in ClusterLoadAssignment
	priorityMap := map[int][]int{}

	// 1. calculate the LocalityLbEndpoints.Priority compared with proxy network
	for i, localityEndpoint := range loadAssignment.Endpoints {
		// all the LbEndpoints belong to same network as we have filtered in ep_filter.go
		epNetwork := util.IstioMetadata(localityEndpoint.LbEndpoints[0], "network")
		// if network matches, the priority is 0.
		// if network matches the failover.To, the priority is 1.
		// for others, the priority is 2.
		var priority int
		if model.IsSameNetwork(proxyNetwork, epNetwork) {
			priority = 0
		} else {
			priority = 2
			for _, failover := range networkFailover {
				if model.IsSameNetwork(epNetwork, failover.To) {
					priority = 1
					break
				}
			}
		}
		loadAssignment.Endpoints[i].Priority = uint32(priority)
		priorityMap[priority] = append(priorityMap[priority], i)
	}

	refreshPriority(loadAssignment, priorityMap)
}

// set loadbalancing priority
func applyTopologyFailover(
	loadAssignment *endpoint.ClusterLoadAssignment,
	proxyLocality *core.Locality,
	localityFailover []*v1alpha3.LocalityLoadBalancerSetting_Failover,
	proxyNetwork string,
	networkFailover []*v1alpha3.NetworkLoadBalancerSetting_Failover,
) {
	// key is priority, value is the index of the LocalityLbEndpoints in ClusterLoadAssignment
	priorityMap := map[int][]int{}

	// 1. calculate the LocalityLbEndpoints.Priority compared with proxy locality
	for i, localityEndpoint := range loadAssignment.Endpoints {
		// all the LbEndpoints belong to same network as we have filtered in ep_filter.go
		epNetwork := util.IstioMetadata(localityEndpoint.LbEndpoints[0], "network")
		// 1. if network/region/zone/subZone all match, the priority is 0.
		// 2. if network/region/zone match, the priority is 1.
		// 3. if network/region matches, the priority is 2.
		// 4. if network matches
		//    4.1. network matches and region matches failover.To, the priority is 3.
		//    4.2. network matches and region do not match failover.To, the priority is 4.
		// 5. if network does not match
		//    5.1. network matches failover.To, the priority is 5.
		//    5.2. all others, the priority is 6.
		priority := LbPriority(proxyLocality, localityEndpoint.Locality, proxyNetwork, epNetwork, true)
		// process match condition 4
		if priority == 3 {
			for _, failoverSetting := range localityFailover {
				if failoverSetting.From == proxyLocality.Region {
					if localityEndpoint.Locality == nil || localityEndpoint.Locality.Region != failoverSetting.To {
						priority = 4
					}
					break
				}
			}
		} else if priority == 5 { // process match condition 5
			for _, failover := range networkFailover {
				if model.IsSameNetwork(failover.From, proxyNetwork) {
					if model.IsSameNetwork(epNetwork, failover.To) {
						priority = 6
					}
					break
				}
			}
		}
		loadAssignment.Endpoints[i].Priority = uint32(priority)
		priorityMap[priority] = append(priorityMap[priority], i)
	}

	refreshPriority(loadAssignment, priorityMap)
}

func refreshPriority(loadAssignment *endpoint.ClusterLoadAssignment, priorityMap map[int][]int) {
	// since Priorities should range from 0 (highest) to N (lowest) without skipping.
	// 2. adjust the priorities in order
	// 2.1 sort all priorities in increasing order.
	priorities := []int{}
	for priority := range priorityMap {
		priorities = append(priorities, priority)
	}
	sort.Ints(priorities)
	// 2.2 adjust LocalityLbEndpoints priority
	// if the index and value of priorities array is not equal.
	for i, priority := range priorities {
		if i != priority {
			// the LocalityLbEndpoints index in ClusterLoadAssignment.Endpoints
			for _, index := range priorityMap[priority] {
				loadAssignment.Endpoints[index].Priority = uint32(i)
			}
		}
	}
}
