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
	"istio.io/api/networking/v1alpha3"
	"istio.io/api/routing/v1alpha1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

func convertDestinationPolicies(configs []model.Config) []model.Config {
	policies := make([]*v1alpha1.DestinationPolicy, 0)
	for _, config := range configs {
		if config.Type == model.DestinationPolicy.Type {
			policies = append(policies, config.Spec.(*v1alpha1.DestinationPolicy))
		}
	}

	destinationRules := make(map[string]*v1alpha3.DestinationRule) // host -> destination rule
	for _, policy := range policies {
		host := convertIstioService(policy.Destination)
		if destinationRules[host] == nil {
			destinationRules[host] = &v1alpha3.DestinationRule{Name: host}
		}
		destinationRule := destinationRules[host]
		destinationRule.Subsets = append(destinationRule.Subsets, convertDestinationPolicy(policy))
	}

	// rewrite traffic policies where len(subsets) == 1
	for _, rule := range destinationRules {
		if len(rule.Subsets) == 1 {
			rule.TrafficPolicy = rule.Subsets[0].TrafficPolicy
			rule.Subsets = nil
		}
	}

	out := make([]model.Config, 0)
	for host, rule := range destinationRules {
		out = append(out, model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.VirtualService.Type,
				Name:      host,
				Namespace: configs[0].Namespace,
				Domain:    configs[0].Domain,
			},
			Spec: rule,
		})
	}

	return out
}

func convertLoadBalancing(in *v1alpha1.LoadBalancing) *v1alpha3.LoadBalancerSettings {
	if in == nil {
		return nil
	}

	switch v := in.LbPolicy.(type) {
	case *v1alpha1.LoadBalancing_Custom:
		log.Warnf("Custom load balancing not supported")
	case *v1alpha1.LoadBalancing_Name:
		simple := &v1alpha3.LoadBalancerSettings_Simple{}
		switch v.Name {
		case v1alpha1.LoadBalancing_ROUND_ROBIN:
			simple.Simple = v1alpha3.LoadBalancerSettings_ROUND_ROBIN
		case v1alpha1.LoadBalancing_LEAST_CONN:
			simple.Simple = v1alpha3.LoadBalancerSettings_LEAST_CONN
		case v1alpha1.LoadBalancing_RANDOM:
			simple.Simple = v1alpha3.LoadBalancerSettings_RANDOM
		}
		return &v1alpha3.LoadBalancerSettings{
			LbPolicy: simple,
		}
	}
	return nil
}

func convertDestinationPolicy(in *v1alpha1.DestinationPolicy) *v1alpha3.Subset {
	if in == nil {
		return nil
	}

	out := &v1alpha3.Subset{
		Name:   labelsToSubsetName(in.Destination.Labels),
		Labels: in.Destination.Labels,
		TrafficPolicy: &v1alpha3.TrafficPolicy{
			LoadBalancer: convertLoadBalancing(in.LoadBalancing),
		},
	}

	// TODO: in.Source
	if in.Source != nil {
		log.Warnf("Destination policy source not supported")
	}

	if in.CircuitBreaker != nil {
		if in.CircuitBreaker.GetCustom() != nil {
			log.Warnf("Custom circuit breaker policy not supported")
		}

		if cb := in.CircuitBreaker.GetSimpleCb(); cb != nil {
			out.TrafficPolicy.ConnectionPool = &v1alpha3.ConnectionPoolSettings{
				Http: &v1alpha3.ConnectionPoolSettings_HTTPSettings{
					Http2MaxRequests:         cb.HttpMaxRequests,
					Http1MaxPendingRequests:  cb.HttpMaxPendingRequests,
					MaxRequestsPerConnection: cb.HttpMaxRequestsPerConnection,
					MaxRetries:               cb.HttpMaxRetries,
				},
				Tcp: &v1alpha3.ConnectionPoolSettings_TCPSettings{
					MaxConnections: cb.MaxConnections,
				},
			}

			// TODO: out.TrafficPolicy.ConnectionPool.Tcp.MaxConnections =
			out.TrafficPolicy.OutlierDetection = &v1alpha3.OutlierDetection{
				Http: &v1alpha3.OutlierDetection_HTTPSettings{
					Interval:           convertGogoDuration(cb.HttpDetectionInterval),
					ConsecutiveErrors:  cb.HttpConsecutiveErrors,
					BaseEjectionTime:   convertGogoDuration(cb.SleepWindow),
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
