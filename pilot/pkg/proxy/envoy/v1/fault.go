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

package v1

import (
	"github.com/golang/protobuf/ptypes"

	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
)

// buildFaultFilters builds a list of fault filters for the http route
func buildFaultFilters(routeConfig *HTTPRouteConfig) []HTTPFilter {
	if routeConfig == nil {
		return nil
	}

	rcFaults := routeConfig.faults()
	faults := make([]HTTPFilter, 0, len(rcFaults))

	for _, f := range rcFaults {
		faults = append(faults, *f)
	}

	return faults
}

// buildHTTPFaultFilter builds a single fault filter for an Envoy cluster
func buildHTTPFaultFilter(cluster string, faultRule *routing.HTTPFaultInjection, headers Headers) *HTTPFilter {
	abort := buildAbortConfig(faultRule.Abort)
	delay := buildDelayConfig(faultRule.Delay)
	if abort == nil && delay == nil {
		return nil
	}

	return &HTTPFilter{
		Type: decoder,
		Name: "fault",
		Config: FilterFaultConfig{
			UpstreamCluster: cluster,
			Headers:         headers,
			Abort:           abort,
			Delay:           delay,
		},
	}
}

// buildAbortConfig builds the envoy config related to abort spec in a fault filter
func buildAbortConfig(abortRule *routing.HTTPFaultInjection_Abort) *AbortFilter {
	if abortRule == nil || abortRule.GetHttpStatus() == 0 || abortRule.Percent == 0.0 {
		return nil
	}

	return &AbortFilter{
		Percent:    int(abortRule.Percent),
		HTTPStatus: int(abortRule.GetHttpStatus()),
	}
}

// buildDelayConfig builds the envoy config related to delay spec in a fault filter
func buildDelayConfig(delayRule *routing.HTTPFaultInjection_Delay) *DelayFilter {
	dur, err := ptypes.Duration(delayRule.GetFixedDelay())
	if delayRule == nil || (err != nil && dur.Seconds() == 0 && dur.Nanoseconds() == 0) || delayRule.Percent == 0.0 {
		return nil
	}

	return &DelayFilter{
		Type:     "fixed",
		Percent:  int(delayRule.Percent),
		Duration: protoDurationToMS(delayRule.GetFixedDelay()),
	}
}

func buildHTTPFaultFilterV2(cluster string, faultRule *routingv2.HTTPFaultInjection, headers Headers) *HTTPFilter {
	abort := buildAbortConfigV2(faultRule.Abort)
	delay := buildDelayConfigV2(faultRule.Delay)
	if abort == nil && delay == nil {
		return nil
	}

	return &HTTPFilter{
		Type: decoder,
		Name: "fault",
		Config: FilterFaultConfig{
			UpstreamCluster: cluster,
			Headers:         headers,
			Abort:           abort,
			Delay:           delay,
		},
	}
}

func buildAbortConfigV2(abortRule *routingv2.HTTPFaultInjection_Abort) *AbortFilter {
	if abortRule == nil || abortRule.GetHttpStatus() == 0 {
		return nil
	}

	percent := int(abortRule.Percent)
	if percent == 0 {
		percent = 100 // default to 100 percent
	}

	return &AbortFilter{
		Percent:    percent,
		HTTPStatus: int(abortRule.GetHttpStatus()),
	}
}

func buildDelayConfigV2(delayRule *routingv2.HTTPFaultInjection_Delay) *DelayFilter {
	if delayRule == nil {
		return nil
	}

	ms := protoDurationToMS(delayRule.GetFixedDelay())
	if ms == 0 {
		return nil
	}

	percent := int(delayRule.Percent)
	if percent == 0 {
		percent = 100 // default to 100 percent
	}

	return &DelayFilter{
		Type:     "fixed",
		Percent:  percent,
		Duration: ms,
	}
}
