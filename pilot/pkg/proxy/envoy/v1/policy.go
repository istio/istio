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
	meshconfig "istio.io/api/mesh/v1alpha1"
	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

func isDestinationExcludedForMTLS(serviceName string, mtlsExcludedServices []string) bool {
	hostname, _, _ := model.ParseServiceKey(serviceName)
	for _, serviceName := range mtlsExcludedServices {
		if hostname == serviceName {
			return true
		}
	}
	return false
}

// applyClusterPolicy assumes an outbound cluster and inserts custom configuration for the cluster
func applyClusterPolicy(cluster *Cluster,
	instances []*model.ServiceInstance,
	config model.IstioConfigStore,
	mesh *meshconfig.MeshConfig,
	accounts model.ServiceAccounts,
	domain string) {
	duration := protoDurationToMS(mesh.ConnectTimeout)
	cluster.ConnectTimeoutMs = duration

	// skip remaining policies for non mesh-local outbound clusters
	if !cluster.outbound {
		return
	}

	// Original DST cluster are used to route to services outside the mesh
	// where Istio auth does not apply.
	if cluster.Type != ClusterTypeOriginalDST {
		if !isDestinationExcludedForMTLS(cluster.ServiceName, mesh.MtlsExcludedServices) &&
			consolidateAuthPolicy(mesh, cluster.port.AuthenticationPolicy) == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
			// apply auth policies
			ports := model.PortList{cluster.port}.GetNames()
			serviceAccounts := accounts.GetIstioServiceAccounts(cluster.hostname, ports)
			cluster.SSLContext = buildClusterSSLContext(model.AuthCertsPath, serviceAccounts)
		}
	}

	// apply destination policies
	policyConfig := config.Policy(instances, cluster.hostname, cluster.labels)

	// if no policy is configured apply destination rule if one exists
	if policyConfig == nil {
		applyDestinationRule(config, cluster, domain)
		return
	}

	policy := policyConfig.Spec.(*routing.DestinationPolicy)

	// Load balancing policies do not apply for Original DST clusters
	// as the intent is to go directly to the instance.
	if policy.LoadBalancing != nil && cluster.Type != ClusterTypeOriginalDST {
		switch policy.LoadBalancing.GetName() {
		case routing.LoadBalancing_ROUND_ROBIN:
			cluster.LbType = LbTypeRoundRobin
		case routing.LoadBalancing_LEAST_CONN:
			cluster.LbType = LbTypeLeastRequest
		case routing.LoadBalancing_RANDOM:
			cluster.LbType = LbTypeRandom
		}
	}

	// Set up circuit breakers and outlier detection
	if policy.CircuitBreaker != nil && policy.CircuitBreaker.GetSimpleCb() != nil {
		cbconfig := policy.CircuitBreaker.GetSimpleCb()
		cluster.MaxRequestsPerConnection = int(cbconfig.HttpMaxRequestsPerConnection)

		// Envoy's circuit breaker is a combination of its circuit breaker (which is actually a bulk head)
		// outlier detection (which is per pod circuit breaker)
		cluster.CircuitBreaker = &CircuitBreaker{}
		if cbconfig.MaxConnections > 0 {
			cluster.CircuitBreaker.Default.MaxConnections = int(cbconfig.MaxConnections)
		}
		if cbconfig.HttpMaxRequests > 0 {
			cluster.CircuitBreaker.Default.MaxRequests = int(cbconfig.HttpMaxRequests)
		}
		if cbconfig.HttpMaxPendingRequests > 0 {
			cluster.CircuitBreaker.Default.MaxPendingRequests = int(cbconfig.HttpMaxPendingRequests)
		}
		if cbconfig.HttpMaxRetries > 0 {
			cluster.CircuitBreaker.Default.MaxRetries = int(cbconfig.HttpMaxRetries)
		}

		cluster.OutlierDetection = &OutlierDetection{}

		cluster.OutlierDetection.MaxEjectionPercent = 10
		if cbconfig.SleepWindow.Seconds > 0 {
			cluster.OutlierDetection.BaseEjectionTimeMS = protoDurationToMS(cbconfig.SleepWindow)
		}
		if cbconfig.HttpConsecutiveErrors > 0 {
			cluster.OutlierDetection.ConsecutiveErrors = int(cbconfig.HttpConsecutiveErrors)
		}
		if cbconfig.HttpDetectionInterval.Seconds > 0 {
			cluster.OutlierDetection.IntervalMS = protoDurationToMS(cbconfig.HttpDetectionInterval)
		}
		if cbconfig.HttpMaxEjectionPercent > 0 {
			cluster.OutlierDetection.MaxEjectionPercent = int(cbconfig.HttpMaxEjectionPercent)
		}
	}
}

func applyLoadBalancePolicy(cluster *Cluster, policy *routingv2.LoadBalancerSettings) {
	if policy == nil || cluster.Type == ClusterTypeOriginalDST {
		return
	}

	if consistent := policy.GetConsistentHash(); consistent != nil {
		// cluster.LbType = LbTypeRingHash
		// TODO: need to set special values in the route config
		// see: https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/load_balancing.html#ring-hash
		log.Warn("Consistent hash load balancing type is not currently supported")
	} else {
		switch policy.GetSimple() {
		case routingv2.LoadBalancerSettings_LEAST_CONN:
			cluster.LbType = LbTypeLeastRequest
		case routingv2.LoadBalancerSettings_RANDOM:
			cluster.LbType = LbTypeRandom
		case routingv2.LoadBalancerSettings_PASSTHROUGH:
			cluster.LbType = LbTypeOriginalDST // FIXME: double-check
		default:
			cluster.LbType = LbTypeRoundRobin
		}
	}
}

func buildOutlierDetection(outlier *routingv2.OutlierDetection) *OutlierDetection {
	if outlier != nil && outlier.Http != nil {
		out := &OutlierDetection{
			ConsecutiveErrors:  5,
			IntervalMS:         10000, // 10s
			BaseEjectionTimeMS: 30000, // 30s
			MaxEjectionPercent: 10,
		}

		if outlier.Http.BaseEjectionTime != nil {
			out.BaseEjectionTimeMS = protoDurationToMS(outlier.Http.BaseEjectionTime)
		}
		if outlier.Http.ConsecutiveErrors > 0 {
			out.ConsecutiveErrors = int(outlier.Http.ConsecutiveErrors)
		}
		if outlier.Http.Interval != nil {
			out.IntervalMS = protoDurationToMS(outlier.Http.Interval)
		}
		if outlier.Http.MaxEjectionPercent > 0 {
			out.MaxEjectionPercent = int(outlier.Http.MaxEjectionPercent)
		}

		return out
	}
	return nil
}

func applyConnectionPool(cluster *Cluster, settings *routingv2.ConnectionPoolSettings) {
	cluster.MaxRequestsPerConnection = 1024 // TODO: set during cluster construction?

	if settings == nil {
		return
	}

	// Envoy's circuit breaker is a combination of its circuit breaker (which is actually a bulk head) and
	// outlier detection (which is per pod circuit breaker)
	cluster.CircuitBreaker = &CircuitBreaker{
		Default: DefaultCBPriority{
			MaxConnections:     1024, // TODO: what is the Istio default?
			MaxPendingRequests: 1024,
			MaxRequests:        1024,
			MaxRetries:         3,
		},
	}

	if settings.Http != nil {
		if settings.Http.Http2MaxRequests > 0 {
			// Envoy only applies MaxRequests in HTTP/2 clusters
			cluster.CircuitBreaker.Default.MaxRequests = int(settings.Http.Http2MaxRequests)
		}
		if settings.Http.Http1MaxPendingRequests > 0 {
			// Envoy only applies MaxPendingRequests in HTTP/1.1 clusters
			cluster.CircuitBreaker.Default.MaxPendingRequests = int(settings.Http.Http1MaxPendingRequests)
		}

		if settings.Http.MaxRequestsPerConnection > 0 {
			cluster.MaxRequestsPerConnection = int(settings.Http.MaxRequestsPerConnection)
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default value of 3
		if settings.Http.MaxRetries > 0 {
			cluster.CircuitBreaker.Default.MaxRetries = int(settings.Http.MaxRetries)
		}
	}

	if settings.Tcp != nil {
		if settings.Tcp.ConnectTimeout != nil {
			cluster.ConnectTimeoutMs = protoDurationToMS(settings.Tcp.ConnectTimeout)
		}

		if settings.Tcp.MaxConnections > 0 {
			cluster.CircuitBreaker.Default.MaxConnections = int(settings.Tcp.MaxConnections)
		}
	}
}

// TODO: write unit tests for sub-functions
func applyTrafficPolicy(cluster *Cluster, policy *routingv2.TrafficPolicy) {
	if policy == nil {
		return
	}
	applyLoadBalancePolicy(cluster, policy.LoadBalancer)
	applyConnectionPool(cluster, policy.ConnectionPool)
	cluster.OutlierDetection = buildOutlierDetection(policy.OutlierDetection)
}

func applyDestinationRule(config model.IstioConfigStore, cluster *Cluster, domain string) {
	destinationRuleConfig := config.DestinationRule(cluster.hostname, domain)
	if destinationRuleConfig != nil {
		destinationRule := destinationRuleConfig.Spec.(*routingv2.DestinationRule)

		applyTrafficPolicy(cluster, destinationRule.TrafficPolicy)
		for _, subset := range destinationRule.Subsets {
			if cluster.labels.Equals(subset.Labels) {
				applyTrafficPolicy(cluster, subset.TrafficPolicy)
				break
			}
		}
	}
}
