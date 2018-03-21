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

	"github.com/golang/protobuf/ptypes/duration"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	OutboundClusterPrefix = "out"
	InboudClusterPrefix   = "in"
)

// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
func BuildClusters(env model.Environment, proxy model.Proxy) []*v2.Cluster {
	clusters := make([]*v2.Cluster, 0)

	// TODO node type router

	// node type Sidecar
	services, err := env.Services()
	if err != nil {
		log.Errorf("Failed for retrieve services: %v", err)
		return nil
	}

	// build outbound clusters
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
								Address:  instance.Endpoint.Address,
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
					Name:  buildClusterName(OutboundClusterPrefix, service.Hostname, "", svcPort.Name),
					Type:  convertResolution(service.Resolution),
					Hosts: hosts,
				}
				applyTrafficPolicy(cluster, destinationRule.TrafficPolicy)
				clusters = append(clusters, cluster)

				for _, subset := range destinationRule.Subsets {
					cluster := &v2.Cluster{
						Name:  buildClusterName(OutboundClusterPrefix, service.Hostname, subset.Name, svcPort.Name),
						Type:  convertResolution(service.Resolution),
						Hosts: hosts,
					}
					applyTrafficPolicy(cluster, destinationRule.TrafficPolicy)
					applyTrafficPolicy(cluster, subset.TrafficPolicy)

					clusters = append(clusters, cluster)
				}

				// TODO HTTP2 feature

			}
		}
	}

	// TODO build inbound clusters
	instances, err := env.GetProxyServiceInstances(proxy)
	for _, instance := range instances {
		clusters = append(clusters, BuildInboundCluster(instance.Endpoint.Port, model.ProtocolTCP, 0))
	}

	// TODO add original dst cluster

	return clusters // TODO: guaranteed ordering?
}

// BuildInboundCluster builds an inbound cluster.
func BuildInboundCluster(port int, protocol model.Protocol, timeout *duration.Duration) *v2.Cluster {
	cluster := &v2.Cluster{
		// TODO currently using original inbound cluster naming convention
		Name:           fmt.Sprintf("%s|%s", InboudClusterPrefix, port),
		Type:           v2.Cluster_STATIC,
		ConnectTimeout: convertDurationGogo(timeout),
		LbPolicy:       v2.Cluster_ROUND_ROBIN,
		Hosts: []*core.Address{{Address: &core.SocketAddress{
			Address: "127.0.0.1",
			// TODO will this always be TCP?
			Protocol: core.TCP,
			PortSpecifier: &core.SocketAddress_PortValue{
				PortValue: uint32(port),
			},
			// TODO: HTTP2 feature
		}}}}
	return cluster
}

// TODO port name is not mandatory if only one port defined
// TODO subset name can be empty
func buildClusterName(prefix, hostname, subsetName, portName string) string {
	return fmt.Sprintf("%s|%s|%s|%s", prefix, subsetName, hostname, portName)
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

func applyTrafficPolicy(cluster *v2.Cluster, policy *networking.TrafficPolicy) {
	if policy == nil {
		return
	}
	applyConnectionPool(cluster, policy.ConnectionPool)
	cluster.OutlierDetection = buildOutlierDetection(policy.OutlierDetection)
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyConnectionPool(cluster *v2.Cluster, settings *networking.ConnectionPoolSettings) {
	if settings == nil {
		return
	}

	threshold := &v2_cluster.CircuitBreakers_Thresholds{}

	if settings.Http != nil {
		if settings.Http.Http2MaxRequests > 0 {
			// Envoy only applies MaxRequests in HTTP/2 clusters
			threshold.MaxRequests = types.UInt32Value{Value: uint32(settings.Http.Http2MaxRequests)}
		}
		if settings.Http.Http1MaxPendingRequests > 0 {
			// Envoy only applies MaxPendingRequests in HTTP/1.1 clusters
			threshold.MaxPendingRequests = types.UInt32Value{Value: uint32(settings.Http.Http1MaxPendingRequests)}
		}

		if settings.Http.MaxRequestsPerConnection > 0 {
			cluster.MaxRequestsPerConnection = types.UInt32Value{Value: uint32(settings.Http.MaxRequestsPerConnection)}
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default value of 3
		if settings.Http.MaxRetries > 0 {
			threshold.MaxRetries = types.UInt32Value{Value: uint32(settings.Http.MaxRetries)}
		}
	}

	if settings.Tcp != nil {
		if settings.Tcp.ConnectTimeout != nil {
			cluster.ConnectTimeout = convertDurationGogo(settings.Tcp.ConnectTimeout)
		}

		if settings.Tcp.MaxConnections > 0 {
			threshold.MaxConnections = types.UInt32Value{Value: uint32(settings.Tcp.MaxConnections)}
		}
	}

	cluster.CircuitBreakers = &v2_cluster.CircuitBreakers{
		Thresholds: []*v2_cluster.CircuitBreakers_Thresholds{threshold},
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func buildOutlierDetection(outlier *networking.OutlierDetection) *v2_cluster.OutlierDetection {
	if outlier == nil || outlier.Http == nil {
		return nil
	}

	out := &v2_cluster.OutlierDetection{}
	if outlier.Http.BaseEjectionTime != nil {
		out.BaseEjectionTime = convertDurationGogo(outlier.Http.BaseEjectionTime)
	}
	if outlier.Http.ConsecutiveErrors > 0 {
		out.Consecutive_5Xx = types.UInt32Value{Value: uint32(outlier.Http.ConsecutiveErrors)}
	}
	if outlier.Http.Interval != nil {
		out.Interval = convertDurationGogo(outlier.Http.Interval)
	}
	if outlier.Http.MaxEjectionPercent > 0 {
		out.MaxEjectionPercent = types.UInt32Value{Value: uint32(outlier.Http.MaxEjectionPercent)}
	}
	return out
}
