// Copyright 2017 Istio Authors
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

// Functions related to translation from the control policies to Envoy config
// Policies apply to Envoy upstream clusters but may appear in the route section.

package envoy

import "istio.io/manager/model/proxy/alphav1/config"

// TODO: apply fault filter by destination as a post-processing step

func insertMixerFilter(listeners []*Listener, mixer string) {
	for _, l := range listeners {
		for _, http := range l.Filters {
			if http.Name == HTTPConnectionManager {
				http.Config.Filters = append([]Filter{{
					Type:   "both",
					Name:   "mixer",
					Config: &FilterMixerConfig{MixerServer: mixer},
				}}, http.Config.Filters...)
			}
		}
	}
}

// buildFaultFilters builds a list of fault filters for the http route. If the route points to a single
// cluster, an array of size 1 is returned. If the route points to a weighted cluster, an array of fault
// filters (one per cluster entry in the weighted cluster) is returned.
func buildFaultFilters(route *Route, faultRule *config.HTTPFaultInjection) []Filter {
	if route == nil {
		return nil
	}
	faults := make([]Filter, 0)
	if route.WeightedClusters != nil {
		for _, cluster := range route.WeightedClusters.Clusters {
			faults = append(faults, buildFaultFilter(cluster.Name, faultRule))
		}
	} else {
		faults = append(faults, buildFaultFilter(route.Cluster, faultRule))
	}
	return faults
}

// buildFaultFilter builds a single fault filter for envoy cluster
func buildFaultFilter(cluster string, faultRule *config.HTTPFaultInjection) Filter {
	return Filter{
		Type: "decoder",
		Name: "fault",
		Config: FilterFaultConfig{
			UpstreamCluster: cluster,
			Headers:         buildHeaders(faultRule.Headers),
			Abort:           buildAbortConfig(faultRule.Abort),
			Delay:           buildDelayConfig(faultRule.Delay),
		},
	}
}

// buildAbortConfig builds the envoy config related to abort spec in a fault filter
func buildAbortConfig(abortRule *config.HTTPFaultInjection_Abort) *AbortFilter {
	if abortRule == nil || abortRule.GetHttpStatus() == 0 {
		return nil
	}

	return &AbortFilter{
		Percent:    int(abortRule.Percent),
		HTTPStatus: int(abortRule.GetHttpStatus()),
	}
}

// buildDelayConfig builds the envoy config related to delay spec in a fault filter
func buildDelayConfig(delayRule *config.HTTPFaultInjection_Delay) *DelayFilter {
	if delayRule == nil || delayRule.GetFixedDelay() == nil {
		return nil
	}

	return &DelayFilter{
		Type:     "fixed",
		Percent:  int(delayRule.GetFixedDelay().Percent),
		Duration: int(delayRule.GetFixedDelay().FixedDelaySeconds * 1000),
	}
}
