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
	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	v2_cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	"github.com/gogo/protobuf/types"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// DefaultLbType set to round robin
	DefaultLbType = v2.Cluster_ROUND_ROBIN
)

// TODO: Need to do inheritance of DestRules based on domain suffix match

// BuildClusters returns the list of clusters for the given proxy. This is the CDS output
// For outbound: Cluster for each service/subset hostname or cidr with SNI set to service hostname
// Cluster type based on resolution
// For inbound (sidecar only): Cluster for each inbound endpoint port and for each service port
func BuildClusters(env model.Environment, proxy model.Proxy) []*v2.Cluster {
	clusters := make([]*v2.Cluster, 0)

	services, err := env.Services()
	if err != nil {
		log.Errorf("Failed for retrieve services: %v", err)
		return nil
	}

	clusters = append(clusters, buildOutboundClusters(env, services)...)
	if proxy.Type == model.Sidecar {
		instances, err := env.GetProxyServiceInstances(proxy)
		if err != nil {
			log.Errorf("failed to get service proxy service instances: %v", err)
			return nil
		}

		clusters = append(clusters, buildInboundClusters(env, instances)...)

		// TODO: Bug? why only for sidecars?
		// append cluster for JwksUri (for Jwt authentication) if necessary.
		clusters = append(clusters, buildJwksURIClustersForProxyInstances(
			env.Mesh, env.IstioConfigStore, instances)...)
	}

	// TODO add original dst cluster or workaround for routers

	return clusters // TODO: normalize/dedup/order
}

func buildOutboundClusters(env model.Environment, services []*model.Service) []*v2.Cluster {
	clusters := make([]*v2.Cluster, 0)
	for _, service := range services {
		config := env.DestinationRule(service.Hostname, "")
		for _, port := range service.Ports {
			hosts := buildClusterHosts(env, service, port)

			// create default cluster
			defaultCluster := &v2.Cluster{
				Name:  model.BuildSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port),
				Type:  convertResolution(service.Resolution),
				Hosts: hosts,
			}
			defaultCluster.ProtocolSelection = v2.Cluster_USE_DOWNSTREAM_PROTOCOL
			clusters = append(clusters, defaultCluster)

			if config != nil {
				destinationRule := config.Spec.(*networking.DestinationRule)

				applyTrafficPolicy(defaultCluster, destinationRule.TrafficPolicy)

				for _, subset := range destinationRule.Subsets {
					subsetCluster := &v2.Cluster{
						Name:  model.BuildSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port),
						Type:  convertResolution(service.Resolution),
						Hosts: hosts,
					}
					applyTrafficPolicy(subsetCluster, destinationRule.TrafficPolicy)
					applyTrafficPolicy(subsetCluster, subset.TrafficPolicy)
					subsetCluster.ProtocolSelection = v2.Cluster_USE_DOWNSTREAM_PROTOCOL
					clusters = append(clusters, subsetCluster)
				}
			}
		}
	}

	return clusters
}

func buildClusterHosts(env model.Environment, service *model.Service, port *model.Port) []*core.Address {
	if service.Resolution != model.DNSLB {
		return nil
	}

	// FIXME port name not required if only one port
	instances, err := env.Instances(service.Hostname, []string{port.Name}, nil)
	if err != nil {
		log.Errorf("failed to retrieve instances for %s: %v", service.Hostname, err)
		return nil
	}

	hosts := make([]*core.Address, 0)
	for _, instance := range instances {
		host := buildAddress(instance.Endpoint.Address, uint32(instance.Endpoint.Port))
		hosts = append(hosts, &host)
	}

	return hosts
}

func buildInboundClusters(env model.Environment, instances []*model.ServiceInstance) []*v2.Cluster {
	clusters := make([]*v2.Cluster, 0)
	for _, instance := range instances {
		// This cluster name is mainly for stats.
		clusterName := model.BuildSubsetKey(model.TrafficDirectionInbound, "", instance.Service.Hostname, instance.Endpoint.ServicePort)
		address := buildAddress("127.0.0.1", uint32(instance.Endpoint.Port))
		clusters = append(clusters, &v2.Cluster{
			Name:              clusterName,
			Type:              v2.Cluster_STATIC,
			LbPolicy:          DefaultLbType,
			ProtocolSelection: v2.Cluster_USE_DOWNSTREAM_PROTOCOL,
			ConnectTimeout:    convertProtoDurationToDuration(env.Mesh.ConnectTimeout),
			Hosts:             []*core.Address{&address},
		})
	}
	return clusters
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
	applyOutlierDetection(cluster, policy.OutlierDetection)
	applyLoadBalancer(cluster, policy.LoadBalancer)
	applyUpstreamTLSSettings(cluster, policy.Tls)
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
			threshold.MaxRequests = &types.UInt32Value{Value: uint32(settings.Http.Http2MaxRequests)}
		}
		if settings.Http.Http1MaxPendingRequests > 0 {
			// Envoy only applies MaxPendingRequests in HTTP/1.1 clusters
			threshold.MaxPendingRequests = &types.UInt32Value{Value: uint32(settings.Http.Http1MaxPendingRequests)}
		}

		if settings.Http.MaxRequestsPerConnection > 0 {
			cluster.MaxRequestsPerConnection = &types.UInt32Value{Value: uint32(settings.Http.MaxRequestsPerConnection)}
		}

		// FIXME: zero is a valid value if explicitly set, otherwise we want to use the default value of 3
		if settings.Http.MaxRetries > 0 {
			threshold.MaxRetries = &types.UInt32Value{Value: uint32(settings.Http.MaxRetries)}
		}
	}

	if settings.Tcp != nil {
		if settings.Tcp.ConnectTimeout != nil {
			cluster.ConnectTimeout = convertGogoDurationToDuration(settings.Tcp.ConnectTimeout)
		}

		if settings.Tcp.MaxConnections > 0 {
			threshold.MaxConnections = &types.UInt32Value{Value: uint32(settings.Tcp.MaxConnections)}
		}
	}

	cluster.CircuitBreakers = &v2_cluster.CircuitBreakers{
		Thresholds: []*v2_cluster.CircuitBreakers_Thresholds{threshold},
	}
}

// FIXME: there isn't a way to distinguish between unset values and zero values
func applyOutlierDetection(cluster *v2.Cluster, outlier *networking.OutlierDetection) {
	if outlier == nil || outlier.Http == nil {
		return
	}

	out := &v2_cluster.OutlierDetection{}
	if outlier.Http.BaseEjectionTime != nil {
		out.BaseEjectionTime = outlier.Http.BaseEjectionTime
	}
	if outlier.Http.ConsecutiveErrors > 0 {
		out.Consecutive_5Xx = &types.UInt32Value{Value: uint32(outlier.Http.ConsecutiveErrors)}
	}
	if outlier.Http.Interval != nil {
		out.Interval = outlier.Http.Interval
	}
	if outlier.Http.MaxEjectionPercent > 0 {
		out.MaxEjectionPercent = &types.UInt32Value{Value: uint32(outlier.Http.MaxEjectionPercent)}
	}
	cluster.OutlierDetection = out
}

func applyLoadBalancer(cluster *v2.Cluster, lb *networking.LoadBalancerSettings) {
	if lb == nil {
		return
	}
	// TODO: RING_HASH and MAGLEV
	switch lb.GetSimple() {
	case networking.LoadBalancerSettings_LEAST_CONN:
		cluster.LbPolicy = v2.Cluster_LEAST_REQUEST
	case networking.LoadBalancerSettings_RANDOM:
		cluster.LbPolicy = v2.Cluster_RANDOM
	case networking.LoadBalancerSettings_ROUND_ROBIN:
		cluster.LbPolicy = v2.Cluster_ROUND_ROBIN
	case networking.LoadBalancerSettings_PASSTHROUGH:
		cluster.LbPolicy = v2.Cluster_ORIGINAL_DST_LB
		cluster.Type = v2.Cluster_ORIGINAL_DST
	}

	// DO not do if else here. since lb.GetSimple returns a enum value (not pointer).
}

func applyUpstreamTLSSettings(cluster *v2.Cluster, tls *networking.TLSSettings) {
	if tls == nil {
		return
	}

	switch tls.Mode {
	case networking.TLSSettings_DISABLE:
		// TODO: Need to make sure that authN does not override this setting
	case networking.TLSSettings_SIMPLE:
		cluster.TlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				TlsParams: &auth.TlsParameters{
					TlsMinimumProtocolVersion: 0,
					TlsMaximumProtocolVersion: 0,
					CipherSuites:              nil,
					EcdhCurves:                nil,
				},
				TlsCertificates:                nil,
				TlsCertificateSdsSecretConfigs: nil,
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: tls.CaCertificates,
						},
					},
					VerifyCertificateHash: nil,
					VerifySpkiSha256:      nil,
					VerifySubjectAltName:  tls.SubjectAltNames,
					RequireOcspStaple: &types.BoolValue{
						Value: false,
					},
					RequireSignedCertificateTimestamp: &types.BoolValue{
						Value: false,
					},
					Crl: &core.DataSource{
						Specifier: nil,
					},
				},
				AlpnProtocols: nil, // TODO: need to set for H2
				DeprecatedV1: &auth.CommonTlsContext_DeprecatedV1{
					AltAlpnProtocols: "",
				},
			},
			Sni: tls.Sni,
		}
	case networking.TLSSettings_MUTUAL:
		cluster.TlsContext = &auth.UpstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				TlsParams: &auth.TlsParameters{
					TlsMinimumProtocolVersion: 0,
					TlsMaximumProtocolVersion: 0,
					CipherSuites:              nil,
					EcdhCurves:                nil,
				},
				TlsCertificates: []*auth.TlsCertificate{
					{
						CertificateChain: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: tls.ClientCertificate,
							},
						},
						PrivateKey: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: tls.PrivateKey,
							},
						},
						Password: &core.DataSource{
							Specifier: nil,
						},
						OcspStaple: &core.DataSource{
							Specifier: nil,
						},
						SignedCertificateTimestamp: nil,
					},
				},
				TlsCertificateSdsSecretConfigs: nil,
				ValidationContext: &auth.CertificateValidationContext{
					TrustedCa: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: tls.CaCertificates,
						},
					},
					VerifyCertificateHash: nil,
					VerifySpkiSha256:      nil,
					VerifySubjectAltName:  tls.SubjectAltNames,
					RequireOcspStaple: &types.BoolValue{
						Value: false,
					},
					RequireSignedCertificateTimestamp: &types.BoolValue{
						Value: false,
					},
					Crl: &core.DataSource{
						Specifier: nil,
					},
				},
				AlpnProtocols: nil,
				DeprecatedV1: &auth.CommonTlsContext_DeprecatedV1{
					AltAlpnProtocols: "",
				},
			},
			Sni: tls.Sni,
		}
	}
}
