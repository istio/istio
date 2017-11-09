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

import (
	"fmt"
	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/proxy"
)

// applyClusterPolicy assumes an outbound cluster and inserts custom configuration for the cluster
func applyClusterPolicy(cluster *Cluster,
	instances []*model.ServiceInstance,
	config model.IstioConfigStore,
	mesh *proxyconfig.MeshConfig,
	accounts model.ServiceAccounts) {
	duration := protoDurationToMS(mesh.ConnectTimeout)
	cluster.ConnectTimeoutMs = duration

	// skip remaining policies for non mesh-local outbound clusters
	if !cluster.outbound {
		return
	}

	// Original DST cluster are used to route to services outside the mesh
	// where Istio auth does not apply.
	if cluster.Type != ClusterTypeOriginalDST {
		authPolicy := cluster.port.AuthenticationPolicy
		if cluster.port.AuthMigrationPort != nil &&
			cluster.port.AuthMigrationPort.Active {
			fmt.Printf("AuthMigrationPort %d: %v\n", cluster.port.AuthMigrationPort.Port,
				cluster.port.AuthMigrationPort.AuthenticationPolicy)
			authPolicy = cluster.port.AuthMigrationPort.AuthenticationPolicy
		}

		if conslidateAuthPolicy(mesh, authPolicy) == proxyconfig.AuthenticationPolicy_MUTUAL_TLS {
			// apply auth policies
			ports := model.PortList{cluster.port}.GetNames()
			serviceAccounts := accounts.GetIstioServiceAccounts(cluster.hostname, ports)
			cluster.SSLContext = buildClusterSSLContext(proxy.AuthCertsPath, serviceAccounts)
		}
	}

	// apply destination policies
	policyConfig := config.Policy(instances, cluster.hostname, cluster.tags)

	if policyConfig == nil {
		return
	}

	policy := policyConfig.Spec.(*proxyconfig.DestinationPolicy)

	// Load balancing policies do not apply for Original DST clusters
	// as the intent is to go directly to the instance.
	if policy.LoadBalancing != nil && cluster.Type != ClusterTypeOriginalDST {
		switch policy.LoadBalancing.GetName() {
		case proxyconfig.LoadBalancing_ROUND_ROBIN:
			cluster.LbType = LbTypeRoundRobin
		case proxyconfig.LoadBalancing_LEAST_CONN:
			cluster.LbType = LbTypeLeastRequest
		case proxyconfig.LoadBalancing_RANDOM:
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
		//TODO: need to add max_retries as well. Currently it defaults to 3

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
