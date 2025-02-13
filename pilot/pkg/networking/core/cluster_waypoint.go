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

package core

import (
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	proxyprotocol "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/metadata/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	sec_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

// buildInternalUpstreamCluster builds a single endpoint cluster to the internal listener.
func buildInternalUpstreamCluster(name string, internalListener string) *cluster.Cluster {
	c := &cluster.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		CircuitBreakers:      &cluster.CircuitBreakers{Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()}},
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints:   util.BuildInternalEndpoint(internalListener, nil),
		},
		TransportSocket: util.DefaultInternalUpstreamTransportSocket,
		TypedExtensionProtocolOptions: map[string]*anypb.Any{
			v3.HttpProtocolOptionsType: passthroughHttpProtocolOptions,
		},
	}

	c.AltStatName = util.DelimitedStatsPrefix(name)

	return c
}

var (
	GetMainInternalCluster = func() *cluster.Cluster {
		return buildInternalUpstreamCluster(MainInternalName, MainInternalName)
	}

	GetEncapCluster = func() *cluster.Cluster {
		return buildInternalUpstreamCluster(EncapClusterName, ConnectOriginate)
	}
)

func (configgen *ConfigGeneratorImpl) buildInboundHBONEClusters() *cluster.Cluster {
	return GetMainInternalCluster()
}

func (configgen *ConfigGeneratorImpl) buildWaypointInboundClusters(
	cb *ClusterBuilder,
	proxy *model.Proxy,
	push *model.PushContext,
	svcs map[host.Name]*model.Service,
) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	// Creates "main_internal" cluster to route to the main internal listener.
	// Creates "encap" cluster to route to the encap listener.
	clusters = append(clusters, GetMainInternalCluster(), GetEncapCluster())
	// Creates per-VIP load balancing upstreams.
	clusters = append(clusters, cb.buildWaypointInboundVIP(proxy, svcs, push.Mesh)...)
	// Upstream of the "encap" listener.
	clusters = append(clusters, cb.buildWaypointConnectOriginate(proxy, push))

	for _, c := range clusters {
		if c.TransportSocket != nil && c.TransportSocketMatches != nil {
			log.Errorf("invalid cluster, multiple matches: %v", c.Name)
		}
	}
	return clusters
}

// `inbound-vip||hostname|port`. EDS routing to the internal listener for each pod in the VIP.
func (cb *ClusterBuilder) buildWaypointInboundVIPCluster(
	proxy *model.Proxy,
	svc *model.Service,
	port model.Port,
	subset string,
	mesh *meshconfig.MeshConfig,
	policy *networking.TrafficPolicy,
	drConfig *config.Config,
) *cluster.Cluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, subset, svc.Hostname, port.Port)

	discoveryType := convertResolution(cb.proxyType, svc)
	var lbEndpoints []*endpoint.LocalityLbEndpoints
	if discoveryType == cluster.Cluster_STRICT_DNS || discoveryType == cluster.Cluster_LOGICAL_DNS {
		lbEndpoints = endpoints.NewCDSEndpointBuilder(
			proxy,
			cb.req.Push,
			clusterName,
			model.TrafficDirectionInboundVIP, subset, svc.Hostname, port.Port,
			svc, nil,
		).FromServiceEndpoints()
	}
	localCluster := cb.buildCluster(clusterName, discoveryType, lbEndpoints,
		model.TrafficDirectionInboundVIP, &port, svc, nil, subset)

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

	// Apply DestinationRule configuration for the service
	connectionPool, outlierDetection, loadBalancer, tls, proxyProtocol := selectTrafficPolicyComponents(policy)
	// Add applicable metadata to the cluster to identify which config is applied for tooling
	if policy != nil {
		util.AddConfigInfoMetadata(localCluster.cluster.Metadata, drConfig.Meta)
	}
	if subset != "" {
		util.AddSubsetToMetadata(localCluster.cluster.Metadata, subset)
	}
	if connectionPool == nil {
		connectionPool = &networking.ConnectionPoolSettings{}
	}

	// For these policies, we have the standard logic apply
	cb.applyConnectionPool(mesh, localCluster, connectionPool)
	cb.applyH2Upgrade(localCluster, &port, mesh, connectionPool)
	applyOutlierDetection(nil, localCluster.cluster, outlierDetection)
	applyLoadBalancer(svc, localCluster.cluster, loadBalancer, &port, cb.locality, cb.proxyLabels, mesh)

	// Setup EDS config after apply LoadBalancer, since it can impact the result
	if localCluster.cluster.GetType() == cluster.Cluster_ORIGINAL_DST {
		localCluster.cluster.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
	}
	maybeApplyEdsConfig(localCluster.cluster)

	// TLS and PROXY are more involved, since these impact the transport socket which is customized for HBONE.
	opts := &buildClusterOpts{
		mesh:           cb.req.Push.Mesh,
		mutable:        localCluster,
		policy:         policy,
		port:           &port,
		serviceTargets: cb.serviceTargets,
		clusterMode:    DefaultClusterMode,
		direction:      model.TrafficDirectionInboundVIP,
	}
	transportSocket := util.RawBufferTransport()
	if tlsContext := buildWaypointTLSContext(opts, tls); tlsContext != nil {
		transportSocket = &core.TransportSocket{
			Name:       wellknown.TransportSocketTLS,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(tlsContext)},
		}
	}
	if proxyProtocol != nil {
		// Wrap the existing transport socket. Note this could be RawBuffer or TLS, depending on other config
		transportSocket = &core.TransportSocket{
			Name: wellknown.TransportSocketPROXY,
			ConfigType: &core.TransportSocket_TypedConfig{
				TypedConfig: protoconv.MessageToAny(&proxyprotocol.ProxyProtocolUpstreamTransport{
					Config:          &core.ProxyProtocolConfig{Version: core.ProxyProtocolConfig_Version(proxyProtocol.Version)},
					TransportSocket: transportSocket,
				},
				),
			},
		}
	}
	// no TLS, we are just going to internal address
	localCluster.cluster.TransportSocketMatches = nil
	// Wrap the transportSocket with internal listener upstream. Note this could be a raw buffer, PROXY, TLS, etc
	localCluster.cluster.TransportSocket = util.WaypointInternalUpstreamTransportSocket(transportSocket)

	return localCluster.build()
}

func buildWaypointTLSContext(opts *buildClusterOpts, tls *networking.ClientTLSSettings) *tlsv3.UpstreamTlsContext {
	if tls == nil {
		return nil
	}

	var tlsContext *tlsv3.UpstreamTlsContext
	var err error
	switch tls.Mode {
	case networking.ClientTLSSettings_DISABLE:
		// nothing needed
		return nil
	case networking.ClientTLSSettings_ISTIO_MUTUAL:
		// not supported (?)
		return nil
	case networking.ClientTLSSettings_SIMPLE:
		tlsContext, err = constructUpstreamTLS(opts, tls, opts.mutable, false)

	case networking.ClientTLSSettings_MUTUAL:
		tlsContext, err = constructUpstreamTLS(opts, tls, opts.mutable, true)
	}
	if err != nil {
		log.Errorf("failed to build Upstream TLSContext: %s", err.Error())
		return nil
	}
	// Compliance for Envoy TLS upstreams.
	if tlsContext != nil {
		sec_model.EnforceCompliance(tlsContext.CommonTlsContext)
	}
	return tlsContext
}

// `inbound-vip|protocol|hostname|port`. EDS routing to the internal listener for each pod in the VIP.
func (cb *ClusterBuilder) buildWaypointInboundVIP(proxy *model.Proxy, svcs map[host.Name]*model.Service, mesh *meshconfig.MeshConfig) []*cluster.Cluster {
	clusters := []*cluster.Cluster{}

	for _, svc := range svcs {
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			cfg := cb.sidecarScope.DestinationRule(model.TrafficDirectionInbound, proxy, svc.Hostname).GetRule()
			dr := CastDestinationRule(cfg)
			policy, _ := util.GetPortLevelTrafficPolicy(dr.GetTrafficPolicy(), port)
			if port.Protocol.IsUnsupported() || port.Protocol.IsTCP() {
				clusters = append(clusters, cb.buildWaypointInboundVIPCluster(proxy, svc, *port, "tcp", mesh, policy, cfg))
			}
			if port.Protocol.IsUnsupported() || port.Protocol.IsHTTP() {
				clusters = append(clusters, cb.buildWaypointInboundVIPCluster(proxy, svc, *port, "http", mesh, policy, cfg))
			}
			for _, ss := range dr.GetSubsets() {
				policy = util.MergeSubsetTrafficPolicy(dr.GetTrafficPolicy(), ss.GetTrafficPolicy(), port)
				if port.Protocol.IsUnsupported() || port.Protocol.IsTCP() {
					clusters = append(clusters, cb.buildWaypointInboundVIPCluster(proxy, svc, *port, "tcp/"+ss.Name, mesh, policy, cfg))
				}
				if port.Protocol.IsUnsupported() || port.Protocol.IsHTTP() {
					clusters = append(clusters, cb.buildWaypointInboundVIPCluster(proxy, svc, *port, "http/"+ss.Name, mesh, policy, cfg))
				}
			}
		}
	}
	return clusters
}

func (cb *ClusterBuilder) buildWaypointConnectOriginate(proxy *model.Proxy, push *model.PushContext) *cluster.Cluster {
	m := &matcher.StringMatcher{}

	m.MatchPattern = &matcher.StringMatcher_Prefix{
		Prefix: spiffe.URIPrefix + push.Mesh.GetTrustDomain() + "/ns/" + proxy.Metadata.Namespace + "/sa/",
	}
	return cb.buildConnectOriginate(proxy, push, m)
}

func (cb *ClusterBuilder) buildConnectOriginate(proxy *model.Proxy, push *model.PushContext, uriSanMatchers ...*matcher.StringMatcher) *cluster.Cluster {
	ctx := buildCommonConnectTLSContext(proxy, push)
	validationCtx := ctx.GetCombinedValidationContext().DefaultValidationContext
	for _, uriSanMatcher := range uriSanMatchers {
		if uriSanMatcher != nil {
			validationCtx.MatchTypedSubjectAltNames = append(validationCtx.MatchTypedSubjectAltNames, &tlsv3.SubjectAltNameMatcher{
				SanType: tlsv3.SubjectAltNameMatcher_URI,
				Matcher: uriSanMatcher,
			})
		}
	}
	// Compliance for Envoy tunnel upstreams.
	sec_model.EnforceCompliance(ctx)
	c := &cluster.Cluster{
		Name:                          ConnectOriginate,
		ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:                      cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:                protomarshal.Clone(cb.req.Push.Mesh.ConnectTimeout),
		CleanupInterval:               durationpb.New(60 * time.Second),
		CircuitBreakers:               &cluster.CircuitBreakers{Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()}},
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		LbConfig: &cluster.Cluster_OriginalDstLbConfig_{
			OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{
				UpstreamPortOverride: &wrappers.UInt32Value{
					Value: model.HBoneInboundListenPort,
				},
				// Used to override destination pods with waypoints.
				MetadataKey: &metadata.MetadataKey{
					Key: util.OriginalDstMetadataKey,
					Path: []*metadata.MetadataKey_PathSegment{{
						Segment: &metadata.MetadataKey_PathSegment_Key{
							Key: "waypoint",
						},
					}},
				},
			},
		},
		TransportSocket: &core.TransportSocket{
			Name: "tls",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tlsv3.UpstreamTlsContext{
				CommonTlsContext: ctx,
			})},
		},
	}

	c.AltStatName = util.DelimitedStatsPrefix(ConnectOriginate)

	return c
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
