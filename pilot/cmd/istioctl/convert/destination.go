// Copyright 2017 Istio Authors.
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

package convert

import (
	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

func convertDestinationPolicies(configs []model.Config) []model.Config {
	policies := make([]*routing.DestinationPolicy, 0)
	for _, config := range configs {
		if config.Type == model.DestinationPolicy.Type {
			policies = append(policies, config.Spec.(*routing.DestinationPolicy))
		}
	}

	destinationRuleByHost := make(map[string]*routingv2.DestinationRule)

	for _, policy := range policies {
		host := convertIstioService(policy.Destination)
		if destinationRuleByHost[host] == nil {
			destinationRuleByHost[host] = &routingv2.DestinationRule{Name: host}
		}
		destinationRule := destinationRuleByHost[host]
		destinationRule.Subsets = append(destinationRule.Subsets, convertDestinationPolicy(policy))
	}

	// rewrite traffic policies where len(subsets) == 1
	for _, rule := range destinationRuleByHost {
		if len(rule.Subsets) == 1 {
			rule.TrafficPolicy = rule.Subsets[0].TrafficPolicy
			rule.Subsets = nil
		}
	}

	out := make([]model.Config, 0)
	for _, rule := range destinationRuleByHost {
		// TODO: ConfigMeta needs to be aggregated from source configs somehow
		out = append(out, model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:            model.V1alpha2RouteRule.Type,
				Name:            "FIXME",
				Namespace:       "FIXME",
				Domain:          "FIXME",
				Labels:          nil,
				Annotations:     nil,
				ResourceVersion: "FIXME",
			},
			Spec: rule,
		})
	}

	return out
}

func convertLoadBalancing(in *routing.LoadBalancing) *routingv2.LoadBalancerSettings {
	if in == nil {
		return nil
	}

	switch v := in.LbPolicy.(type) {
	case *routing.LoadBalancing_Custom:
		log.Warnf("Custom load balancing not supported")
	case *routing.LoadBalancing_Name:
		simple := &routingv2.LoadBalancerSettings_Simple{}
		switch v.Name {
		case routing.LoadBalancing_ROUND_ROBIN:
			simple.Simple = routingv2.LoadBalancerSettings_ROUND_ROBIN
		case routing.LoadBalancing_LEAST_CONN:
			simple.Simple = routingv2.LoadBalancerSettings_LEAST_CONN
		case routing.LoadBalancing_RANDOM:
			simple.Simple = routingv2.LoadBalancerSettings_RANDOM
		}
		return &routingv2.LoadBalancerSettings{
			LbPolicy: simple,
		}
	}
	return nil
}

func convertDestinationPolicy(in *routing.DestinationPolicy) *routingv2.Subset {
	if in == nil {
		return nil
	}

	out := &routingv2.Subset{
		Name:   labelsToSubsetName(in.Destination.Labels),
		Labels: in.Destination.Labels,
		TrafficPolicy: &routingv2.TrafficPolicy{
			LoadBalancer: convertLoadBalancing(in.LoadBalancing),
		},
	}

	// TODO: in.Source

	if in.CircuitBreaker != nil {
		if in.CircuitBreaker.GetCustom() != nil {
			log.Warnf("Custom circuit breaker policy not supported")
		}

		if cb := in.CircuitBreaker.GetSimpleCb(); cb != nil {
			out.TrafficPolicy.ConnectionPool = &routingv2.ConnectionPoolSettings{
				Http: &routingv2.ConnectionPoolSettings_HTTPSettings{
					Http2MaxRequests:         cb.HttpMaxRequests,
					Http1MaxPendingRequests:  cb.HttpMaxPendingRequests,
					MaxRequestsPerConnection: cb.HttpMaxRequestsPerConnection,
					MaxRetries:               cb.HttpMaxRetries,
				},
				Tcp: &routingv2.ConnectionPoolSettings_TCPSettings{
					MaxConnections: cb.MaxConnections,
				},
			}

			// TODO: out.TrafficPolicy.ConnectionPool.Tcp.MaxConnections =
			out.TrafficPolicy.OutlierDetection = &routingv2.OutlierDetection{
				Http: &routingv2.OutlierDetection_HTTPSettings{
					Interval:           cb.HttpDetectionInterval,
					ConsecutiveErrors:  cb.HttpConsecutiveErrors,
					BaseEjectionTime:   cb.SleepWindow,
					MaxEjectionPercent: cb.HttpMaxEjectionPercent,
				},
			}
		}
	}

	if in.Custom != nil {
		log.Warn("Custom destination policy not supported")
	}

	return out
}

// alpha1 route rules, alpha2 destination rules -> complete alpha2 destination rules (no broken refs)
//func createMissingDestinationRules(configs []model.Config) []model.Config {
//	subsets := make(map[string]map[string]bool)
//	destinationRuleByHost := make(map[string]*routingv2.DestinationRule)
//	routeConfigs := make([]model.Config, 0)
//
//	for _, config := range configs {
//		switch config.Type {
//		case model.V1alpha2RouteRule:
//		case model.RouteRule:
//		case model.DestinationRule:
//		}
//	}
//
//	// TODO ...
//
//	routeRules := make([]*routing.RouteRule, 0)
//	for _, rule := range routeRules {
//		defaultHost := convertIstioService(rule.Destination)
//		for _, route := range rule.Route {
//			host := defaultHost
//			if route.Destination != nil {
//				host = convertIstioService(route.Destination)
//			}
//			subsetName := labelsToSubsetName(route.Labels)
//			if !subsets[host][subsetName] {
//				if _, exists := destinationRuleByHost[host]; !exists {
//					destinationRuleByHost[host] = &routingv2.DestinationRule{
//						// TODO
//					}
//				}
//				destRule := destinationRuleByHost[host]
//				destRule.Subsets = append(destRule.Subsets, &routingv2.Subset{
//					// TODO
//				})
//
//				subsets[host][subsetName] = true
//			}
//		}
//	}
//}
