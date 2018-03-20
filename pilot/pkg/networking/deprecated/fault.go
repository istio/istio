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

package deprecated

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	fault "github.com/envoyproxy/go-control-plane/envoy/config/filter/fault/v2"
	http_fault "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/fault/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/golang/protobuf/ptypes"

	routing "istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

// buildFaultFilters builds fault filters in v2 Envoy format for the given config, env and node.
func buildFaultFilters(config model.Config, env model.Environment, node model.Proxy) []*http_conn.HttpFilter {
	if config.Spec == nil {
		return []*http_conn.HttpFilter{}
	}
	rule := config.Spec.(*routing.RouteRule)
	// TODO(mostrowski): need a lightweight function to get list of cluster names.
	clusters, err := BuildClusters(env, node)
	if err != nil {
		panic(err)
	}
	headerMatchers := buildHTTPHeaderMatcher(rule.Match)
	var out []*http_conn.HttpFilter
	// Add the fault filters, one per cluster defined in weighted cluster or cluster
	if rule.HttpFault != nil {
		out = make([]*http_conn.HttpFilter, 0, len(clusters))
		for _, c := range clusters {
			if fault := buildHTTPFaultFilter(c.Name, rule.HttpFault, headerMatchers); fault != nil {
				out = append(out, fault)
			}
		}
	}

	return out
}

// buildHTTPFaultFilter builds a single fault filter for an Envoy cluster.
func buildHTTPFaultFilter(cluster string, faultRule *routing.HTTPFaultInjection, headers []*route.HeaderMatcher) *http_conn.HttpFilter {
	abort := buildAbortConfig(faultRule.Abort)
	delay := buildDelayConfig(faultRule.Delay)
	if abort == nil && delay == nil {
		return nil
	}

	config := &http_fault.HTTPFault{
		UpstreamCluster: cluster,
		Headers:         headers,
		Abort:           abort,
		Delay:           delay,
	}

	return &http_conn.HttpFilter{
		Name:   "fault",
		Config: buildProtoStruct("fault", config.String()),
	}
}

// buildAbortConfig builds the envoy config related to abort spec in a fault filter.
func buildAbortConfig(abortRule *routing.HTTPFaultInjection_Abort) *http_fault.FaultAbort {
	if abortRule == nil || abortRule.GetHttpStatus() == 0 || abortRule.Percent == 0.0 {
		return nil
	}

	return &http_fault.FaultAbort{
		Percent:   uint32(abortRule.Percent),
		ErrorType: &http_fault.FaultAbort_HttpStatus{HttpStatus: uint32(abortRule.GetHttpStatus())},
	}
}

// buildDelayConfig builds the envoy config related to delay spec in a fault filter.
func buildDelayConfig(delayRule *routing.HTTPFaultInjection_Delay) *fault.FaultDelay {
	dur, err := ptypes.Duration(delayRule.GetFixedDelay())
	if delayRule == nil || (err != nil && dur.Seconds() == 0 && dur.Nanoseconds() == 0) || delayRule.Percent == 0.0 {
		return nil
	}

	d := durationToTimeDuration(delayRule.GetFixedDelay())
	return &fault.FaultDelay{
		Type: fault.FaultDelay_FIXED,
		FaultDelayType: &fault.FaultDelay_FixedDelay{
			FixedDelay: &d,
		},
		Percent: uint32(delayRule.Percent),
	}
}
