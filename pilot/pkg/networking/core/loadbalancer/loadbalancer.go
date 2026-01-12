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
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/api/label"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	registrylabel "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/util/sets"
)

const (
	FailoverPriorityLabelDefaultSeparator = '='
)

func GetLocalityLbSetting(
	mesh *v1alpha3.LocalityLoadBalancerSetting,
	destrule *v1alpha3.LocalityLoadBalancerSetting,
	service *model.Service,
) (*v1alpha3.LocalityLoadBalancerSetting, bool) {
	if destrule != nil {
		if destrule.Enabled != nil && !destrule.Enabled.Value {
			return nil, false
		}
		return destrule, false
	}
	if service != nil && service.Attributes.TrafficDistribution != model.TrafficDistributionAny {
		switch service.Attributes.TrafficDistribution {
		case model.TrafficDistributionPreferSameZone:
			return &v1alpha3.LocalityLoadBalancerSetting{
				Enabled: wrappers.Bool(true),
				// Prefer same zone, region, network
				FailoverPriority: []string{
					label.TopologyNetwork.Name,
					registrylabel.LabelTopologyRegion,
					registrylabel.LabelTopologyZone,
				},
			}, true
		case model.TrafficDistributionPreferSameNode:
			return &v1alpha3.LocalityLoadBalancerSetting{
				Enabled: wrappers.Bool(true),
				// Prefer same node, subzone, zone, region, network
				FailoverPriority: []string{
					label.TopologyNetwork.Name,
					registrylabel.LabelTopologyRegion,
					registrylabel.LabelTopologyZone,
					label.TopologySubzone.Name,
					registrylabel.LabelHostname,
				},
			}, true
		case model.TrafficDistributionAny:
			// fallthrough
		}
	}
	msh := mesh.GetEnabled()
	if msh != nil && !msh.Value {
		return nil, false
	}
	return mesh, false
}

func ApplyLocalityLoadBalancer(
	loadAssignment *endpoint.ClusterLoadAssignment,
	wrappedLocalityLbEndpoints []*WrappedLocalityLbEndpoints,
	locality *core.Locality,
	proxyLabels map[string]string,
	localityLB *v1alpha3.LocalityLoadBalancerSetting,
	enableFailover bool,
) {
	// before calling this function localityLB.enabled field has been checked.
	if localityLB == nil || loadAssignment == nil {
		return
	}

	// one of Distribute or Failover settings can be applied.
	if localityLB.GetDistribute() != nil {
		applyLocalityWeights(locality, loadAssignment, localityLB.GetDistribute())
		// Failover needs outlier detection, otherwise Envoy will never drop down to a lower priority.
		// Do not apply default failover when locality LB is disabled.
	} else if enableFailover {
		if len(localityLB.FailoverPriority) > 0 {
			// Apply user defined priority failover settings.
			applyFailoverPriorities(loadAssignment, wrappedLocalityLbEndpoints, proxyLabels, localityLB.FailoverPriority)
			// If failover is explicitly configured with failover priority, apply failover settings also.
			if len(localityLB.Failover) != 0 {
				applyLocalityFailover(locality, loadAssignment, localityLB.Failover)
			}
		} else {
			// Apply default failover settings or user defined region failover settings.
			applyLocalityFailover(locality, loadAssignment, localityLB.Failover)
		}
	}
}

// set locality loadbalancing weight based on user defined weights.
func applyLocalityWeights(
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
	// Envoy to do weighted load balancing across different zones and geographical locations.
	for _, localityWeightSetting := range distribute {
		if localityWeightSetting != nil &&
			util.LocalityMatch(locality, localityWeightSetting.From) {
			misMatched := sets.Set[int]{}
			for i := range loadAssignment.Endpoints {
				misMatched.Insert(i)
			}
			for locality, weight := range localityWeightSetting.To {
				// index -> original weight
				destLocMap := map[int]uint32{}
				totalWeight := uint32(0)
				for i, ep := range loadAssignment.Endpoints {
					if misMatched.Contains(i) {
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
				if loadAssignment.Endpoints[i] != nil {
					loadAssignment.Endpoints[i].LbEndpoints = nil
				}
			}
			break
		}
	}
}

// set locality loadbalancing priority - This is based on Region/Zone/SubZone matching.
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
		// priority is calculated using the already assigned priority using failoverPriority.
		// Since there are at most 5 priorities can be assigned using locality failover(0-4),
		// we multiply the priority by 5 for maintaining the priorities already assigned.
		// Afterwards the final priorities can be calculated from 0 (highest) to N (lowest) without skipping.
		priorityInt := int(loadAssignment.Endpoints[i].Priority*5) + priority
		loadAssignment.Endpoints[i].Priority = uint32(priorityInt)
		priorityMap[priorityInt] = append(priorityMap[priorityInt], i)
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

// set loadbalancing priority by failover priority label.
func applyFailoverPriorities(
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
		localityLbEndpointsPerLocality := applyFailoverPriorityPerLocality(proxyLabels, wrappedLbEndpoint, failoverPriorities)
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

// Returning the label names in a separate array as the iteration of map is not ordered.
func priorityLabelOverrides(labels []string) ([]string, map[string]string) {
	priorityLabels := make([]string, 0, len(labels))
	overriddenValueByLabel := make(map[string]string, len(labels))
	var tempStrings []string
	for _, labelWithValue := range labels {
		tempStrings = strings.Split(labelWithValue, string(FailoverPriorityLabelDefaultSeparator))
		priorityLabels = append(priorityLabels, tempStrings[0])
		if len(tempStrings) == 2 {
			overriddenValueByLabel[tempStrings[0]] = tempStrings[1]
			continue
		}
	}
	return priorityLabels, overriddenValueByLabel
}

// set loadbalancing priority by failover priority label.
// split one LocalityLbEndpoints to multiple LocalityLbEndpoints based on failover priorities.
func applyFailoverPriorityPerLocality(
	proxyLabels map[string]string,
	ep *WrappedLocalityLbEndpoints,
	failoverPriorities []string,
) []*endpoint.LocalityLbEndpoints {
	lowestPriority := len(failoverPriorities)
	// key is priority, value is the index of LocalityLbEndpoints.LbEndpoints
	priorityMap := map[int][]int{}
	priorityLabels, priorityLabelOverrides := priorityLabelOverrides(failoverPriorities)
	for i, istioEndpoint := range ep.IstioEndpoints {
		var priority int
		// failoverPriority labels match
		for j, label := range priorityLabels {
			valueForProxy, ok := priorityLabelOverrides[label]
			if !ok {
				valueForProxy = proxyLabels[label]
			}
			if valueForProxy != istioEndpoint.Labels[label] {
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
