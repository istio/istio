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
	"github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/pkg/log"
)

// BuildClusters handles a gRPC CDS request, used with the 'ApiListener' style of requests.
// The main difference is that the request includes Resources to filter.
func (g *GrpcConfigGenerator) BuildClusters(node *model.Proxy, push *model.PushContext, names []string) model.Resources {
	resp := model.Resources{}
	// gRPC doesn't currently support any of the APIs - returning just the expected EDS result.
	// Since the code is relatively strict - we'll add info as needed.
	for _, n := range names {
		_, _, hn, porti := model.ParseSubsetKey(n)
		if hn == "" || porti == 0 {
			log.Warn("failed to parse subset key ", n)
			continue
		}

		// SANS associated with this host name.
		// TODO: apply DestinationRules, etc
		sans := push.ServiceAccounts[hn][porti]

		tls := buildTLSContext(sans)
		rc := &envoy_config_cluster_v3.Cluster{
			Name:                 n,
			ClusterDiscoveryType: &envoy_config_cluster_v3.Cluster_Type{Type: envoy_config_cluster_v3.Cluster_EDS},
			EdsClusterConfig: &envoy_config_cluster_v3.Cluster_EdsClusterConfig{
				ServiceName: n,
				EdsConfig: &envoy_config_core_v3.ConfigSource{
					ConfigSourceSpecifier: &envoy_config_core_v3.ConfigSource_Ads{
						Ads: &envoy_config_core_v3.AggregatedConfigSource{},
					},
				},
			},
			TransportSocket: &envoy_config_core_v3.TransportSocket{
				Name:       transportSocketName,
				ConfigType: &envoy_config_core_v3.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(tls)},
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

// buildTLSContext creates a TLS context that assumes 'default' name, and credentials/tls/certprovider/pemfile
func buildTLSContext(sans []string) *envoy_extensions_transport_sockets_tls_v3.UpstreamTlsContext {
	return &envoy_extensions_transport_sockets_tls_v3.UpstreamTlsContext{
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
}
