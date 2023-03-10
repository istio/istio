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

package v1alpha3

import (
	"strconv"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/extensionproviders"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	networkutil "istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pilot/pkg/util/protoconv"
	sec "istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/util/sets"
)

func CreateMCPClusterForJwksURI(proxy *model.Proxy, req *model.PushRequest, cb *ClusterBuilder) []*cluster.Cluster {
	if !features.AutoCreateClusterEntry {
		return nil
	}
	reqAuthPolicies := req.Push.AuthnPolicies.GetJwtPoliciesForWorkload(proxy.Metadata.Namespace, proxy.Labels)
	jwksStore := sets.String{}
	clusters := make([]*cluster.Cluster, 0)
	if len(reqAuthPolicies) > 0 {
		processedJwtRules := []*v1beta1.JWTRule{}
		for _, config := range reqAuthPolicies {
			spec := config.Spec.(*v1beta1.RequestAuthentication)
			processedJwtRules = append(processedJwtRules, spec.JwtRules...)
		}
		for _, jwtRule := range processedJwtRules {
			jwksURIExists := jwksStore.Contains(jwtRule.JwksUri)
			if jwksURIExists {
				continue
			}
			jwksStore.Insert(jwtRule.JwksUri)
			jwksInfo, err := sec.ParseJwksURI(jwtRule.JwksUri)
			if err != nil {
				continue
			}
			_, cluster, err := extensionproviders.LookupCluster(req.Push, jwksInfo.Hostname.String(), jwksInfo.Port)
			if err != nil && cluster == "" {
				jwksCluster := buildClusterForJwksURI(jwksInfo, cb)
				clusters = append(clusters, jwksCluster)
			}
		}
	}
	return clusters
}

func buildClusterForJwksURI(jwksInfo sec.JwksInfo, cb *ClusterBuilder) *cluster.Cluster {
	ep := &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: jwksInfo.Hostname.String(),
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: uint32(jwksInfo.Port),
							},
						},
					},
				},
			},
		},
	}
	c := &cluster.Cluster{
		Name:                 "jwksuri-autogen-" + jwksInfo.Hostname.String() + "-" + strconv.Itoa(jwksInfo.Port),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STRICT_DNS},
		ConnectTimeout:       proto.Clone(cb.req.Push.Mesh.ConnectTimeout).(*durationpb.Duration),
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: "jwksuri-autogen-" + jwksInfo.Hostname.String() + "-" + strconv.Itoa(jwksInfo.Port),
			Endpoints: []*endpoint.LocalityLbEndpoints{
				{
					LbEndpoints: []*endpoint.LbEndpoint{ep},
				},
			},
		},
	}
	if jwksInfo.UseSSL {
		tlsContext := &auth.UpstreamTlsContext{
			CommonTlsContext: defaultUpstreamCommonTLSContext(),
			Sni:              jwksInfo.Hostname.String(),
		}
		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig("file-root:system"),
			},
		}
		c.TransportSocket = &core.TransportSocket{
			Name:       wellknown.TransportSocketTls,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(tlsContext)},
		}
	}

	dnsRate := cb.req.Push.Mesh.DnsRefreshRate
	c.DnsRefreshRate = dnsRate
	c.RespectDnsTtl = true
	if networkutil.AllIPv4(cb.proxyIPAddresses) {
		// IPv4 only
		c.DnsLookupFamily = cluster.Cluster_V4_ONLY
	} else if networkutil.AllIPv6(cb.proxyIPAddresses) {
		// IPv6 only
		c.DnsLookupFamily = cluster.Cluster_V6_ONLY
	} else {
		// Dual Stack
		if features.EnableDualStack {
			// If dual-stack, it may be [IPv4, IPv6] or [IPv6, IPv4]
			// using Cluster_ALL to enable Happy Eyeballsfor upstream connections
			c.DnsLookupFamily = cluster.Cluster_ALL
		} else {
			// keep the original logic if Dual Stack is disable
			c.DnsLookupFamily = cluster.Cluster_V4_ONLY
		}
	}
	return c
}
