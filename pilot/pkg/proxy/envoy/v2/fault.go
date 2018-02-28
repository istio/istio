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

package v2

import (
	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/api/filter"
	"github.com/envoyproxy/go-control-plane/api/filter/http"
	"github.com/envoyproxy/go-control-plane/api/filter/network"
	"github.com/golang/protobuf/ptypes"
	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
)

// buildHTTPFaultFilter builds a single fault filter for an Envoy cluster.
func buildHTTPFaultFilter(cluster string, faultRule *routing.HTTPFaultInjection, headers []*api.HeaderMatcher) *network.HttpFilter {
	abort := buildAbortConfig(faultRule.Abort)
	delay := buildDelayConfig(faultRule.Delay)
	if abort == nil && delay == nil {
		return nil
	}

	config := &envoy_api_v2_filter_http.HTTPFault{
		UpstreamCluster: cluster,
		Headers:         headers,
		Abort:           abort,
		Delay:           delay,
	}

	return &network.HttpFilter{
		Name:   "fault",
		Config: buildProtoStruct("fault", config.String()),
	}
}

// buildAbortConfig builds the envoy config related to abort spec in a fault filter.
func buildAbortConfig(abortRule *routing.HTTPFaultInjection_Abort) *envoy_api_v2_filter_http.FaultAbort {
	if abortRule == nil || abortRule.GetHttpStatus() == 0 || abortRule.Percent == 0.0 {
		return nil
	}

	return &envoy_api_v2_filter_http.FaultAbort{
		Percent:   uint32(abortRule.Percent),
		ErrorType: &envoy_api_v2_filter_http.FaultAbort_HttpStatus{HttpStatus: uint32(abortRule.GetHttpStatus())},
	}
}

// buildDelayConfig builds the envoy config related to delay spec in a fault filter.
func buildDelayConfig(delayRule *routing.HTTPFaultInjection_Delay) *filter.FaultDelay {
	dur, err := ptypes.Duration(delayRule.GetFixedDelay())
	if delayRule == nil || (err != nil && dur.Seconds() == 0 && dur.Nanoseconds() == 0) || delayRule.Percent == 0.0 {
		return nil
	}

	d := durationToTimeDuration(delayRule.GetFixedDelay())
	return &filter.FaultDelay{
		Type: filter.FaultDelay_FIXED,
		FaultDelayType: &filter.FaultDelay_FixedDelay{
			FixedDelay: &d,
		},
		Percent: uint32(delayRule.Percent),
	}
}

func buildHTTPFaultFilterV2(cluster string, faultRule *routingv2.HTTPFaultInjection, headers []*api.HeaderMatcher) *network.HttpFilter {
	abort := buildAbortConfigV2(faultRule.Abort)
	delay := buildDelayConfigV2(faultRule.Delay)
	if abort == nil && delay == nil {
		return nil
	}

	config := &envoy_api_v2_filter_http.HTTPFault{
		UpstreamCluster: cluster,
		Headers:         headers,
		Abort:           abort,
		Delay:           delay,
	}

	return &network.HttpFilter{
		Name:   "fault",
		Config: buildProtoStruct("fault", config.String()),
	}
}

func buildAbortConfigV2(abortRule *routingv2.HTTPFaultInjection_Abort) *envoy_api_v2_filter_http.FaultAbort {
	if abortRule == nil || abortRule.GetHttpStatus() == 0 {
		return nil
	}

	percent := uint32(abortRule.Percent)
	if percent == 0 {
		percent = 100 // default to 100 percent
	}

	return &envoy_api_v2_filter_http.FaultAbort{
		Percent:   percent,
		ErrorType: &envoy_api_v2_filter_http.FaultAbort_HttpStatus{HttpStatus: uint32(abortRule.GetHttpStatus())},
	}
}

func buildDelayConfigV2(delayRule *routingv2.HTTPFaultInjection_Delay) *filter.FaultDelay {
	if delayRule == nil {
		return nil
	}

	d := protoDurationToTimeDuration(nil) // delayRule.GetFixedDelay())
	if d.Nanoseconds() == 0 {
		return nil
	}

	percent := int(delayRule.Percent)
	if percent == 0 {
		percent = 100 // default to 100 percent
	}

	return &filter.FaultDelay{
		Type: filter.FaultDelay_FIXED,
		FaultDelayType: &filter.FaultDelay_FixedDelay{
			FixedDelay: &d,
		},
		Percent: uint32(delayRule.Percent),
	}
}
