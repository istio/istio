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
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

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

func ApplyLocalityLBSetting(
	loadAssignment *endpoint.ClusterLoadAssignment,
	wrappedLocalityLbEndpoints []*WrappedLocalityLbEndpoints,
	locality *core.Locality,
	proxyLabels map[string]string,
	localityLB *v1alpha3.LocalityLoadBalancerSetting,
	enableFailover bool,
) {
	if localityLB == nil || loadAssignment == nil {
		return
	}

	// one of Distribute or Failover settings can be applied.
	if localityLB.GetDistribute() != nil {
		applyLocalityWeight(locality, loadAssignment, localityLB.GetDistribute())
		// Failover needs outlier detection, otherwise Envoy will never drop down to a lower priority.
		// Do not apply default failover when locality LB is disabled.
	} else if enableFailover && (localityLB.Enabled == nil || localityLB.Enabled.Value) {
		if len(localityLB.FailoverPriority) > 0 {
			applyPriorityFailover(loadAssignment, wrappedLocalityLbEndpoints, proxyLabels, localityLB.FailoverPriority)
			return
		}
		applyLocalityFailover(locality, loadAssignment, localityLB.Failover)
	}
}

// set locality loadbalancing weight
func applyLocalityWeight(
	locality *core.Locality,
	loadAssignment *endpoint.ClusterLoadAssignment,
	distribute []*v1alpha3.LocalityLoadBalancerSetting_Distribute,
) {
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
			util.LocalityMatch(locality, localityWeightSetting.From) {
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
						if util.LocalityMatch(ep.Locality, locality) {
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
	failover []*v1alpha3.LocalityLoadBalancerSetting_Failover,
) {
	// key is priority, value is the index of the LocalityLbEndpoints in ClusterLoadAssignment
	priorityMap := map[int][]int{}

	// 1. calculate the LocalityLbEndpoints.Priority compared with proxy locality
	for i, localityEndpoint := range loadAssignment.Endpoints {
		// if region/zone/subZone all match, the priority is 0.
		// if region/zone match, the priority is 1.
		// if region matches, the priority is 2.
		// if locality not match, the priority is 3.
		priority := util.LbPriority(locality, localityEndpoint.Locality)
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

// WrappedLocalityLbEndpoints contain an envoy LocalityLbEndpoints
// and the original IstioEndpoints used to generate it.
// It is used to do failover priority label match with proxy labels.
type WrappedLocalityLbEndpoints struct {
	IstioEndpoints      []*model.IstioEndpoint
	LocalityLbEndpoints *endpoint.LocalityLbEndpoints
}

// set loadbalancing priority by failover priority label
func applyPriorityFailover(
	loadAssignment *endpoint.ClusterLoadAssignment,
	wrappedLocalityLbEndpoints []*WrappedLocalityLbEndpoints,
	proxyLabels map[string]string,
	failoverPriorities []string,
) {
	if len(proxyLabels) == 0 || len(wrappedLocalityLbEndpoints) == 0 {
		return
	}
	priorityMap := make(map[int][]int, len(failoverPriorities))
	localityLbEndpoints := []*endpoint.LocalityLbEndpoints{}
	for _, wrappedLbEndpoint := range wrappedLocalityLbEndpoints {
		localityLbEndpointsPerLocality := applyPriorityFailoverPerLocality(proxyLabels, wrappedLbEndpoint, failoverPriorities)
		localityLbEndpoints = append(localityLbEndpoints, localityLbEndpointsPerLocality...)
	}
	for i, ep := range localityLbEndpoints {
		priorityMap[int(ep.Priority)] = append(priorityMap[int(ep.Priority)], i)
	}
	// since Priorities should range from 0 (highest) to N (lowest) without skipping.
	// adjust the priorities in order
	// 1. sort all priorities in increasing order.
	priorities := []int{}
	for priority := range priorityMap {
		priorities = append(priorities, priority)
	}
	sort.Ints(priorities)
	// 2. adjust LocalityLbEndpoints priority
	// if the index and value of priorities array is not equal.
	for i, priority := range priorities {
		if i != priority {
			// the LocalityLbEndpoints index in ClusterLoadAssignment.Endpoints
			for _, index := range priorityMap[priority] {
				localityLbEndpoints[index].Priority = uint32(i)
			}
		}
	}
	loadAssignment.Endpoints = localityLbEndpoints
}

// set loadbalancing priority by failover priority label.
// split one LocalityLbEndpoints to multiple LocalityLbEndpoints based on failover priorities.
func applyPriorityFailoverPerLocality(
	proxyLabels map[string]string,
	ep *WrappedLocalityLbEndpoints,
	failoverPriorities []string,
) []*endpoint.LocalityLbEndpoints {
	lowestPriority := len(failoverPriorities)
	// key is priority, value is the index of LocalityLbEndpoints.LbEndpoints
	priorityMap := map[int][]int{}
	for i, istioEndpoint := range ep.IstioEndpoints {
		var priority int
		// failoverPriority labels match
		for j, label := range failoverPriorities {
			if proxyLabels[label] != istioEndpoint.Labels[label] {
				priority = lowestPriority - j
				break
			}
		}
		priorityMap[priority] = append(priorityMap[priority], i)
	}

	// sort all priorities in increasing order.
	priorities := []int{}
	for priority := range priorityMap {
		priorities = append(priorities, priority)
	}
	sort.Ints(priorities)

	out := make([]*endpoint.LocalityLbEndpoints, len(priorityMap))
	for i, priority := range priorities {
		out[i] = util.CloneLocalityLbEndpoint(ep.LocalityLbEndpoints)
		out[i].LbEndpoints = nil
		out[i].Priority = uint32(priority)
		var weight uint32
		for _, index := range priorityMap[priority] {
			out[i].LbEndpoints = append(out[i].LbEndpoints, ep.LocalityLbEndpoints.LbEndpoints[index])
			weight += ep.LocalityLbEndpoints.LbEndpoints[index].GetLoadBalancingWeight().GetValue()
		}
		// reset weight
		out[i].LoadBalancingWeight = &wrappers.UInt32Value{
			Value: weight,
		}
	}

	return out
}
