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
	"fmt"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

func (configgen *ConfigGeneratorImpl) buildInboundHBONEClusters(cb *ClusterBuilder, proxy *model.Proxy, instances []*model.ServiceInstance) []*cluster.Cluster {
	if !proxy.EnableHBONE() {
		return nil
	}
	clusters := make([]*cluster.Cluster, 0)
	for _, i := range instances {
		p := i.Endpoint.EndpointPort
		name := fmt.Sprintf("inbound-hbone|%d", p)
		clusters = append(clusters, cb.buildInternalListenerCluster(name, name).build())
	}
	return clusters
}

func (configgen *ConfigGeneratorImpl) buildWaypointInboundClusters(
	cb *ClusterBuilder,
	proxy *model.Proxy,
	push *model.PushContext,
	svcs map[host.Name]*model.Service,
) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	// Creates "internal" cluster to route to the main "internal" listener.
	// Creates "encap" listener to route to the encap listener.
	clusters = append(clusters, cb.buildWaypointInboundInternal()...)
	// Creates per-VIP load balancing upstreams.
	clusters = append(clusters, cb.buildWaypointInboundVIP(svcs)...)
	// Upstream of the "encap" listener.
	clusters = append(clusters, cb.buildWaypointInboundConnect(proxy, push))

	for _, c := range clusters {
		if c.TransportSocket != nil && c.TransportSocketMatches != nil {
			log.Errorf("invalid cluster, multiple matches: %v", c.Name)
		}
	}
	return clusters
}

// `inbound-vip||hostname|port`. EDS routing to the internal listener for each pod in the VIP.
func (cb *ClusterBuilder) buildWaypointInboundVIPCluster(svc *model.Service, port model.Port, subset string) *MutableCluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, subset, svc.Hostname, port.Port)

	clusterType := cluster.Cluster_EDS
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, nil,
		model.TrafficDirectionInbound, &port, nil, nil)

	// Ensure VIP cluster has services metadata for stats filter usage
	im := getOrCreateIstioMetadata(localCluster.cluster)
	im.Fields["services"] = &structpb.Value{
		Kind: &structpb.Value_ListValue{
			ListValue: &structpb.ListValue{
				Values: []*structpb.Value{},
			},
		},
	}
	svcMetaList := im.Fields["services"].GetListValue()
	svcMetaList.Values = append(svcMetaList.Values, buildServiceMetadata(svc))

	// no TLS, we are just going to internal address
	localCluster.cluster.TransportSocketMatches = nil
	localCluster.cluster.TransportSocket = util.InternalUpstreamTransportSocket(util.TunnelHostMetadata, util.IstioHostMetadata)
	maybeApplyEdsConfig(localCluster.cluster)
	return localCluster
}

var InternalUpstreamSocketMatch = []*cluster.Cluster_TransportSocketMatch{
	{
		Name: "internal_upstream",
		Match: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				model.TunnelLabelShortName: {Kind: &structpb.Value_StringValue{StringValue: model.TunnelHTTP}},
			},
		},
		TransportSocket: util.InternalUpstreamTransportSocket(util.TunnelHostMetadata, util.IstioHostMetadata),
	},
	defaultTransportSocketMatch(),
}

var BaggagePassthroughTransportSocket = util.InternalUpstreamTransportSocket(util.IstioClusterMetadata, util.IstioHostMetadata)

func (cb *ClusterBuilder) buildWaypointInboundInternal() []*cluster.Cluster {
	clusters := []*cluster.Cluster{}
	{
		// This TCP cluster routes to "internal" listener.
		clusterType := cluster.Cluster_STATIC
		llb := util.BuildInternalEndpoint("internal", nil)
		localCluster := cb.buildDefaultCluster("internal", clusterType, llb,
			model.TrafficDirectionInbound, &model.Port{Protocol: protocol.TCP}, nil, nil)
		localCluster.cluster.TransportSocketMatches = nil
		localCluster.cluster.TransportSocket = util.InternalUpstreamTransportSocket()
		clusters = append(clusters, localCluster.build())
	}
	{
		// This TCP/HTTP cluster routes from "internal" listener.
		clusterType := cluster.Cluster_STATIC
		llb := util.BuildInternalEndpoint("connect_originate", nil)
		localCluster := cb.buildDefaultCluster("encap", clusterType, llb,
			model.TrafficDirectionInbound, &model.Port{Protocol: protocol.TCP}, nil, nil)
		localCluster.cluster.TransportSocketMatches = nil
		localCluster.cluster.TransportSocket = util.InternalUpstreamTransportSocket()
		localCluster.cluster.TypedExtensionProtocolOptions = map[string]*anypb.Any{
			v3.HttpProtocolOptionsType: PassthroughHttpProtocolOptions,
		}
		clusters = append(clusters, localCluster.build())
	}
	return clusters
}

// `inbound-vip|protocol|hostname|port`. EDS routing to the internal listener for each pod in the VIP.
func (cb *ClusterBuilder) buildWaypointInboundVIP(svcs map[host.Name]*model.Service) []*cluster.Cluster {
	clusters := []*cluster.Cluster{}

	for _, svc := range svcs {
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			if port.Protocol.IsUnsupported() || port.Protocol.IsTCP() {
				clusters = append(clusters, cb.buildWaypointInboundVIPCluster(svc, *port, "tcp").build())
			}
			if port.Protocol.IsUnsupported() || port.Protocol.IsHTTP() {
				clusters = append(clusters, cb.buildWaypointInboundVIPCluster(svc, *port, "http").build())
			}
			cfg := cb.proxy.SidecarScope.DestinationRule(model.TrafficDirectionInbound, cb.proxy, svc.Hostname).GetRule()
			if cfg != nil {
				destinationRule := cfg.Spec.(*networking.DestinationRule)
				for _, ss := range destinationRule.Subsets {
					if port.Protocol.IsUnsupported() || port.Protocol.IsTCP() {
						clusters = append(clusters, cb.buildWaypointInboundVIPCluster(svc, *port, "tcp/"+ss.Name).build())
					}
					if port.Protocol.IsUnsupported() || port.Protocol.IsHTTP() {
						clusters = append(clusters, cb.buildWaypointInboundVIPCluster(svc, *port, "http/"+ss.Name).build())
					}
				}
			}
		}
	}
	return clusters
}

// CONNECT origination cluster
func (cb *ClusterBuilder) buildWaypointInboundConnect(proxy *model.Proxy, push *model.PushContext) *cluster.Cluster {
	ctx := &tls.CommonTlsContext{}
	security.ApplyToCommonTLSContext(ctx, proxy, nil, nil, true)

	// Restrict upstream SAN to waypoint scope.
	scope := proxy.WaypointScope()
	m := &matcher.StringMatcher{}
	if scope.ServiceAccount != "" {
		m.MatchPattern = &matcher.StringMatcher_Exact{
			Exact: spiffe.MustGenSpiffeURI(scope.Namespace, scope.ServiceAccount),
		}
	} else {
		m.MatchPattern = &matcher.StringMatcher_Prefix{
			Prefix: spiffe.URIPrefix + spiffe.GetTrustDomain() + "/ns/" + scope.Namespace + "/sa/",
		}
	}
	validationCtx := ctx.GetCombinedValidationContext().DefaultValidationContext

	// NB: Un-typed SAN validation is ignored when typed is used, so only typed version must be used.
	validationCtx.MatchTypedSubjectAltNames = append(validationCtx.MatchTypedSubjectAltNames, &tls.SubjectAltNameMatcher{
		SanType: tls.SubjectAltNameMatcher_URI,
		Matcher: m,
	})
	aliases := authn.TrustDomainsForValidation(push.Mesh)
	if len(aliases) > 0 {
		matchers := util.StringToPrefixMatch(security.AppendURIPrefixToTrustDomain(aliases))
		for _, matcher := range matchers {
			validationCtx.MatchTypedSubjectAltNames = append(validationCtx.MatchTypedSubjectAltNames, &tls.SubjectAltNameMatcher{
				SanType: tls.SubjectAltNameMatcher_URI,
				Matcher: matcher,
			})
		}
	}

	ctx.AlpnProtocols = []string{"h2"}
	ctx.TlsParams = &tls.TlsParameters{
		// Ensure TLS 1.3 is used everywhere
		TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
		TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_3,
	}
	return &cluster.Cluster{
		Name:                          "connect_originate",
		ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:                      cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:                durationpb.New(2 * time.Second),
		CleanupInterval:               durationpb.New(60 * time.Second),
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		TransportSocket: &core.TransportSocket{
			Name: "tls",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: ctx,
			})},
		},
	}
}

func h2connectUpgrade() map[string]*anypb.Any {
	return map[string]*anypb.Any{
		v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
			UpstreamProtocolOptions: &http.HttpProtocolOptions_ExplicitHttpConfig_{ExplicitHttpConfig: &http.HttpProtocolOptions_ExplicitHttpConfig{
				ProtocolConfig: &http.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
					Http2ProtocolOptions: &core.Http2ProtocolOptions{
						AllowConnect: true,
					},
				},
			}},
		}),
	}
}
