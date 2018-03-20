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

package v1alpha3

import (
	"fmt"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	v2_cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	"github.com/gogo/protobuf/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
func BuildClusters(env model.Environment, node model.Proxy) []*v2.Cluster {
	var clusters []*v2.Cluster

	// TODO node type router

	// node type Sidecar
	services, err := env.Services()
	if err != nil {
		log.Errorf("Failed for retrieve services: %v", err)
		return clusters
	}


	// build Outbound Clusters
	for _, service := range services {
		config := env.IstioConfigStore.DestinationRule(service.Hostname, "")
		if config != nil {
			destinationRule := config.Spec.(*networking.DestinationRule)
			for _, svcPort := range service.Ports {

				var hosts []*core.Address
				if service.Resolution == model.DNSLB {
					// FIXME port name not required if only one port
					instances, err := env.ServiceDiscovery.Instances(service.Hostname, []string{svcPort.Name}, nil)
					if err != nil {
						log.Errorf("failed to retrieve instances for %s: %v", service.Hostname, err)
					} else {
						for _, instance := range instances {
							hosts = append(hosts, &core.Address{Address: &core.SocketAddress{
								Address: instance.Endpoint.Address,
								Protocol: core.TCP,
								PortSpecifier: &core.SocketAddress_PortValue{
									PortValue: uint32(instance.Endpoint.Port),
								},
							}})
						}
					}
				}

				// create default cluster
				cluster := &v2.Cluster{
					Name: buildClusterName(service.Hostname, "", svcPort.Name),
					Type: convertResolution(service.Resolution),
					Hosts: hosts,
				}
				applyTrafficPolicy(cluster, destinationRule.TrafficPolicy)
				clusters = append(clusters, cluster)

				for _, subset := range destinationRule.Subsets {
					cluster := &v2.Cluster{
						Name: buildClusterName(service.Hostname, subset.Name, svcPort.Name),
						Type: convertResolution(service.Resolution),
						Hosts: hosts,
					}
					applyTrafficPolicy(cluster, destinationRule.TrafficPolicy)
					applyTrafficPolicy(cluster, subset.TrafficPolicy)

					clusters = append(clusters, cluster)
				}

				// TODO HTTP2 cluster feature

			}
		}
	}

	// TODO build inbound clusters

	// TODO build TCP clusters in/out

	// TODO add one original dest cluster

	return clusters
}

// TODO port name is not mandatory if only one port defined
// TODO subset name can be empty
func buildClusterName(hostname, subsetName, portName string) string {
	return fmt.Sprintf("%s.%s|%s", subsetName, hostname, portName)
}

func convertResolution(resolution model.Resolution) v2.Cluster_DiscoveryType {
	switch resolution {
	case model.ClientSideLB:
		return v2.Cluster_EDS
	case model.DNSLB:
		return v2.Cluster_STRICT_DNS
	case model.Passthrough:
		return v2.Cluster_ORIGINAL_DST
	default:
		return v2.Cluster_EDS
	}
}

func buildOutlierDetection(outlier *networking.OutlierDetection) *v2_cluster.OutlierDetection {
	if outlier != nil && outlier.Http != nil {
		out := &v2_cluster.OutlierDetection{
			Consecutive_5Xx:    5,
			Interval:           types.Duration{Seconds: 10},
			BaseEjectionTime:   types.Duration{Seconds: 30},
			MaxEjectionPercent: 10,
		}

		if outlier.Http.BaseEjectionTime != nil {
			out.BaseEjectionTime = convertDurationGogo(outlier.Http.BaseEjectionTime)
		}
		if outlier.Http.ConsecutiveErrors > 0 {
			out.Consecutive_5Xx = int(outlier.Http.ConsecutiveErrors)
		}
		if outlier.Http.Interval != nil {
			out.Interval = convertDurationGogo(outlier.Http.Interval)
		}
		if outlier.Http.MaxEjectionPercent > 0 {
			out.MaxEjectionPercent = int(outlier.Http.MaxEjectionPercent)
		}

		return out
	}
	return nil
}

func applyConnectionPool(cluster *v2.Cluster, settings *networking.ConnectionPoolSettings) {
	cluster.MaxRequestsPerConnection = 1024 // TODO: set during cluster construction?

	if settings == nil {
		return
	}

	threshold := &v2_cluster.CircuitBreakers_Thresholds{
		MaxConnections:     1024, // TODO: what is the Istio default?
		MaxPendingRequests: 1024,
		MaxRequests:        1024,
		MaxRetries:         3,
	}

	if settings.Http != nil {
		if settings.Http.Http2MaxRequests > 0 {
			// Envoy only applies MaxRequests in HTTP/2 clusters
			threshold.MaxRequests = int(settings.Http.Http2MaxRequests)
		}
		if settings.Http.Http1MaxPendingRequests > 0 {
			// Envoy only applies MaxPendingRequests in HTTP/1.1 clusters
			threshold.MaxPendingRequests = int(settings.Http.Http1MaxPendingRequests)
		}

		if settings.Http.MaxRequestsPerConnection > 0 {
			cluster.MaxRequestsPerConnection = int(settings.Http.MaxRequestsPerConnection)
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default value of 3
		if settings.Http.MaxRetries > 0 {
			threshold.MaxRetries = int(settings.Http.MaxRetries)
		}
	}

	if settings.Tcp != nil {
		if settings.Tcp.ConnectTimeout != nil {
			cluster.ConnectTimeout = convertDurationGogo(settings.Tcp.ConnectTimeout)
		}

		if settings.Tcp.MaxConnections > 0 {
			threshold.MaxConnections = int(settings.Tcp.MaxConnections)
		}
	}

	// Envoy's circuit breaker is a combination of its circuit breaker (which is actually a bulk head) and
	// outlier detection (which is per pod circuit breaker)
	cluster.CircuitBreakers = &v2_cluster.CircuitBreakers{
		Thresholds: []*v2_cluster.CircuitBreakers_Thresholds{threshold},
	}
}

func applyTrafficPolicy(cluster *v2.Cluster, policy *networking.TrafficPolicy) {
	if policy == nil {
		return
	}
	applyConnectionPool(cluster, policy.ConnectionPool)
	cluster.OutlierDetection = buildOutlierDetection(policy.OutlierDetection)
}
