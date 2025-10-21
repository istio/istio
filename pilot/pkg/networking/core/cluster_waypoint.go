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
	"istio.io/istio/pilot/pkg/features"
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
func buildInternalUpstreamCluster(name string, internalListener string, h2 bool) *cluster.Cluster {
	c := &cluster.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		CircuitBreakers:      &cluster.CircuitBreakers{Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()}},
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints:   util.BuildInternalEndpoint(internalListener, nil),
		},
		TransportSocket: util.DefaultInternalUpstreamTransportSocket,
	}

	if h2 {
		c.TypedExtensionProtocolOptions = map[string]*anypb.Any{
			v3.HttpProtocolOptionsType: passthroughHttpProtocolOptions,
		}
	}

	c.AltStatName = util.DelimitedStatsPrefix(name)

	return c
}

var (
	GetMainInternalCluster = func() *cluster.Cluster {
		return buildInternalUpstreamCluster(MainInternalName, MainInternalName, true)
	}

	GetEncapCluster = func(p *model.Proxy) *cluster.Cluster {
		name := ConnectOriginate
		h2 := true
		if isEastWestGateway(p) {
			name = ForwardInnerConnect
			h2 = false
		}
		return buildInternalUpstreamCluster(EncapClusterName, name, h2)
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
	clusters = append(clusters, GetMainInternalCluster(), GetEncapCluster(proxy))
	// Creates per-VIP load balancing upstreams.
	clusters = append(clusters, cb.buildWaypointInboundVIP(proxy, svcs, push.Mesh)...)

	// Upstream of the "encap" listener.
	if features.EnableAmbientMultiNetwork && isEastWestGateway(proxy) {
		// Creates "blackhole" cluster to avoid failures if no globally scoped services exist
		clusters = append(clusters, cb.buildWaypointForwardInnerConnect(), cb.buildBlackHoleCluster())
	} else {
		clusters = append(clusters, cb.buildWaypointConnectOriginate(proxy, push))
	}

	// This bit creates clusters needed to handle requests going to a remote network.
	// In ambient, requests going to a remote network have to be wrapped in a double-HBONE tunnel (e.g.,
	// HBONE inside another HBONE).
	//
	// For context, ambient E/W gateway will terminiate the outer HBONE tunnel and will redirect the inner
	// HBONE tunnel to either a waypoint or service backend as an opaque stream of bytes - E/W gateway does
	// not know that the data is actually an HBONE tunnel.
	if features.EnableAmbientMultiNetwork && features.EnableAmbientWaypointMultiNetwork && isWaypointProxy(proxy) {
		clusters = append(
			clusters,
			cb.buildWaypointInnerConnectOriginate(proxy, push),
			cb.buildWaypointOuterConnectOriginate(proxy, push),
		)
	}

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
	// TODO: is this enough? Probably since we validate no extra listeners are present in the conversion layer
	terminate := isEastWestGateway(proxy)
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
	connectionPool, outlierDetection, loadBalancer, tls, proxyProtocol, retryBudget := selectTrafficPolicyComponents(policy)
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

	if terminate {
		// We're tunneling double HBONE as raw TCP, so no need for HTTP settings
		// or h2 upgrade
		connectionPool.Http = nil
		cb.applyConnectionPool(mesh, localCluster, connectionPool, retryBudget)
		applyOutlierDetection(nil, localCluster.cluster, outlierDetection)
		applyLoadBalancer(svc, localCluster.cluster, loadBalancer, &port, cb.locality, cb.proxyLabels, mesh)
		// TODO: Decide if we want to support this
		if localCluster.cluster.GetType() == cluster.Cluster_ORIGINAL_DST {
			log.Warnf("Passthrough on the east/west gateway isn't expected")
			localCluster.cluster.LbPolicy = cluster.Cluster_CLUSTER_PROVIDED
		}
		maybeApplyEdsConfig(localCluster.cluster)
		// Set a transport socket since we're going to an internal listener
		transportSocket := util.RawBufferTransport()
		localCluster.cluster.TransportSocket = util.FullMetadataPassthroughInternalUpstreamTransportSocket(transportSocket)
		return localCluster.build()
	}

	// For these policies, we have the standard logic apply
	cb.applyConnectionPool(mesh, localCluster, connectionPool, retryBudget)
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
			if isEastWestGateway(proxy) {
				// East-west gateways don't respect DestinationRule, so don't read it here
				// TODO: Confirm this decision
				clusters = append(clusters, cb.buildWaypointInboundVIPCluster(proxy, svc, *port, "tcp", mesh, nil, nil))
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

// buildWaypointInnerConnectOriginate creates a cluster that innitiate inner HBONE tunnel of double-HBONE.
// InnerConnectOrigiante cluster is responsible for creating the inner CONNECT tunnel and forwarding to an internal listener that
// will wrap the traffic into outer HBONE tunnel.
func (cb *ClusterBuilder) buildWaypointInnerConnectOriginate(proxy *model.Proxy, push *model.PushContext) *cluster.Cluster {
	// Normally for clusters that just redirect to internal listeners we would use util.DefaultInternalUpstreamTransportSocket.
	// util.DefaultInternalUpstreamTransportSocket is a InternalUpstreamTransport socket wrapping a RawBufferTransport socket.
	// For double HBONE we want something slightly different - we still need to use InternalUpstreamTransport socket because
	// we redirect to an internal listener, but instead of keeping data as-is we want to encrypt it using TLS, so instead of the
	// RawBufferTransport we want to wrap a TLS transport socket.
	tlsCtx := buildCommonConnectTLSContext(proxy, push)
	sec_model.EnforceCompliance(tlsCtx)
	transportSocket := util.InternalUpstreamTransportSocket("internal_upstream_with_tls", &core.TransportSocket{
		Name: "tls",
		ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&tlsv3.UpstreamTlsContext{
			CommonTlsContext: tlsCtx,
		})},
	})

	c := &cluster.Cluster{
		Name:                 DoubleHBONEInnerConnectOriginate,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		CircuitBreakers: &cluster.CircuitBreakers{
			Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()},
		},
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: DoubleHBONEInnerConnectOriginate,
			Endpoints:   util.BuildInternalEndpoint(DoubleHBONEOuterConnectOriginate, nil),
		},
		TypedExtensionProtocolOptions: h2connectUpgradeWithNoPooling(),
		TransportSocket:               transportSocket,
	}

	c.AltStatName = util.DelimitedStatsPrefix(DoubleHBONEInnerConnectOriginate)

	return c
}

// buildWaypointOuterConnectOriginate creates a cluster that finishes wrapping traffic in double HBONE by finalizing outer HBONE tunnel.
// It's basically equivalent to the regular waypoint ConnectOriginate cluster and does the same thing, the only real difference is that
// it wraps the data already wrapped into a CONNECT once.
func (cb *ClusterBuilder) buildWaypointOuterConnectOriginate(proxy *model.Proxy, push *model.PushContext) *cluster.Cluster {
	ctx := buildCommonConnectTLSContext(proxy, push)
	sec_model.EnforceCompliance(ctx)
	c := &cluster.Cluster{
		Name:                          DoubleHBONEOuterConnectOriginate,
		ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:                      cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:                protomarshal.Clone(cb.req.Push.Mesh.ConnectTimeout),
		CleanupInterval:               durationpb.New(60 * time.Second),
		CircuitBreakers:               &cluster.CircuitBreakers{Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()}},
		TypedExtensionProtocolOptions: h2connectUpgradeWithNoPooling(),
		LbConfig: &cluster.Cluster_OriginalDstLbConfig_{
			OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{
				UpstreamPortOverride: &wrappers.UInt32Value{
					Value: model.HBoneInboundListenPort,
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

	c.AltStatName = util.DelimitedStatsPrefix(DoubleHBONEOuterConnectOriginate)

	return c
}

func (cb *ClusterBuilder) buildWaypointConnectOriginate(proxy *model.Proxy, push *model.PushContext) *cluster.Cluster {
	// needed to enable cross-namespace waypoints when SkipValidateTrustDomain is set
	// this ensures the match_typed_subject_alt_names list for the envoy config cluster is always empty
	if features.SkipValidateTrustDomain {
		return cb.buildConnectOriginate(ConnectOriginate, proxy, push, nil)
	}
	m := &matcher.StringMatcher{}

	m.MatchPattern = &matcher.StringMatcher_Prefix{
		Prefix: spiffe.URIPrefix + push.Mesh.GetTrustDomain() + "/ns/" + proxy.Metadata.Namespace + "/sa/",
	}
	return cb.buildConnectOriginate(ConnectOriginate, proxy, push, m)
}

func (cb *ClusterBuilder) buildWaypointForwardInnerConnect() *cluster.Cluster {
	return cb.buildForwardInnerConnect()
}

// Create a cluster to replace connect_originate when we're forwarding a double-hbone request.
func (cb *ClusterBuilder) buildForwardInnerConnect() *cluster.Cluster {
	c := &cluster.Cluster{
		Name:                 ForwardInnerConnect,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:       protomarshal.Clone(cb.req.Push.Mesh.ConnectTimeout),
		CleanupInterval:      durationpb.New(60 * time.Second),
		CircuitBreakers:      &cluster.CircuitBreakers{Thresholds: []*cluster.CircuitBreakers_Thresholds{getDefaultCircuitBreakerThresholds()}},
		LbConfig: &cluster.Cluster_OriginalDstLbConfig_{
			OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{
				UpstreamPortOverride: &wrappers.UInt32Value{
					Value: model.HBoneInboundListenPort, // Should be fine whether we're sending to destination or waypoint
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
	}

	c.AltStatName = util.DelimitedStatsPrefix(ForwardInnerConnect)

	return c
}

func (cb *ClusterBuilder) buildConnectOriginate(
	name string,
	proxy *model.Proxy,
	push *model.PushContext,
	uriSanMatchers ...*matcher.StringMatcher,
) *cluster.Cluster {
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
		Name:                          name,
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

	c.AltStatName = util.DelimitedStatsPrefix(name)

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

func h2connectUpgradeWithNoPooling() map[string]*anypb.Any {
	return map[string]*anypb.Any{
		v3.HttpProtocolOptionsType: protoconv.MessageToAny(&http.HttpProtocolOptions{
			CommonHttpProtocolOptions: &core.HttpProtocolOptions{
				// This has very little effect as there is connection pooling at the level of the service
				// cluster that already multiplexes multiple HTTP requests over the same connection before
				// this option takes effect. However doing this is better than nothing.
				//
				// In the future though a better solution is needed to achieve sensible connection pooling
				// without lasering a particular backend in the remote network.
				//
				// TODO(https://github.com/istio/istio/issues/58039): remove it after deploying a sensible
				// connection pooling fix for ambient multi-network.
				MaxRequestsPerConnection: &wrappers.UInt32Value{Value: 1},
			},
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
