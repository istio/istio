// Copyright Istio Authors
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

package grpcgen

import (
	"net"
	"strconv"

	"github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

// BuildClusters handles a gRPC CDS request, used with the 'ApiListener' style of requests.
// The main difference is that the request includes Resources to filter.
func (g *GrpcConfigGenerator) BuildClusters(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	resp := model.Resources{}
	// gRPC doesn't currently support any of the APIs - returning just the expected EDS result.
	// Since the code is relatively strict - we'll add info as needed.
	for _, n := range names {
		hn, portn, err := net.SplitHostPort(n)
		if err != nil {
			log.Warn("Failed to parse ", n, " ", err)
			continue
		}

		porti, err := strconv.Atoi(portn)
		if err != nil {
			log.Warn("Failed to parse ", n, " ", err)
			continue
		}

		// SANS associated with this host name.
		// TODO: apply DestinationRules, etc
		sans := push.ServiceAccounts[host.Name(hn)][porti]

		// Assumes 'default' name, and credentials/tls/certprovider/pemfile

		tlsC := &envoy_extensions_transport_sockets_tls_v3.UpstreamTlsContext{
			CommonTlsContext: &envoy_extensions_transport_sockets_tls_v3.CommonTlsContext{
				TlsCertificateCertificateProviderInstance: &envoy_extensions_transport_sockets_tls_v3.CommonTlsContext_CertificateProviderInstance{
					InstanceName:    "default",
					CertificateName: "default",
				},

				ValidationContextType: &envoy_extensions_transport_sockets_tls_v3.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &envoy_extensions_transport_sockets_tls_v3.CommonTlsContext_CombinedCertificateValidationContext{
						ValidationContextCertificateProviderInstance: &envoy_extensions_transport_sockets_tls_v3.CommonTlsContext_CertificateProviderInstance{
							InstanceName:    "default",
							CertificateName: "ROOTCA",
						},
						DefaultValidationContext: &envoy_extensions_transport_sockets_tls_v3.CertificateValidationContext{
							MatchSubjectAltNames: util.StringToExactMatch(sans),
						},
					},
				},
			},
		}

		rc := &envoy_config_cluster_v3.Cluster{
			Name:                 clusterKey(hn, porti),
			ClusterDiscoveryType: &envoy_config_cluster_v3.Cluster_Type{Type: envoy_config_cluster_v3.Cluster_EDS},
			EdsClusterConfig: &envoy_config_cluster_v3.Cluster_EdsClusterConfig{
				ServiceName: "outbound|" + portn + "||" + hn,
				EdsConfig: &envoy_config_core_v3.ConfigSource{
					ConfigSourceSpecifier: &envoy_config_core_v3.ConfigSource_Ads{
						Ads: &envoy_config_core_v3.AggregatedConfigSource{},
					},
				},
			},
			TransportSocket: &envoy_config_core_v3.TransportSocket{
				Name:       transportSocketName,
				ConfigType: &envoy_config_core_v3.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tlsC)},
			},
		}
		// see grpc/xds/internal/client/xds.go securityConfigFromCluster
		resp = append(resp, &envoy_service_discovery_v3.Resource{
			Name:     n,
			Resource: util.MessageToAny(rc),
		})
	}
	return resp
}

