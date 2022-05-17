//go:build !agent
// +build !agent

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

package uproxygen

import (
	"fmt"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/match"
	"net"
	"strconv"
	"strings"
	"time"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	routerfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	originaldst "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_dst/v3"
	originalsrc "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/original_src/v3"
	httpconn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	internalupstream "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/internal_upstream/v3"
	rawbuffer "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/metadata/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	any "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	istiolog "istio.io/pkg/log"
)

var uproxyLog = istiolog.RegisterScope("uproxygen", "xDS Generator for uProxy clients", 0)

var _ model.XdsResourceGenerator = &UProxyConfigGenerator{}

type UProxyConfigGenerator struct {
	EndpointIndex *model.EndpointIndex
	Workloads     ambient.Cache
}

func (g *UProxyConfigGenerator) Generate(
	proxy *model.Proxy,
	w *model.WatchedResource,
	req *model.PushRequest,
) (model.Resources, model.XdsLogDetails, error) {
	push := req.Push
	switch w.TypeUrl {
	case v3.ListenerType:
		return g.BuildListeners(proxy, push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	case v3.ClusterType:
		return g.BuildClusters(proxy, push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	case v3.EndpointType:
		return g.BuildEndpoints(proxy, push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	}

	return nil, model.DefaultXdsLogDetails, nil
}

const (
	UproxyOutboundCapturePort         uint32 = 15001
	UproxyInboundNodeLocalCapturePort uint32 = 15088
	UproxyInboundCapturePort          uint32 = 15008

	// TODO: this needs to match the mark in the iptables rules.
	// And also not clash with any other mark on the host level.
	// either figure out a way to not hardcode it, or a way to not use it.
	// i think the best solution is to have this mark configurable and run the
	// iptables rules from the code, so we are sure the mark matches.
	OriginalSrcMark uint32 = 1234
)

func (g *UProxyConfigGenerator) BuildListeners(proxy *model.Proxy, push *model.PushContext, names []string) (out model.Resources) {
	out = append(out,
		g.buildPodOutboundCaptureListener(proxy, push),
		g.buildInboundCaptureListener(proxy, push),
	)
	for sa := range push.SidecarlessIndex.Workloads.ByIdentity {
		// name is "" to infer from sa name
		out = append(out, outboundTunnelListener("", sa))
	}

	return out
}

func (g *UProxyConfigGenerator) BuildClusters(proxy *model.Proxy, push *model.PushContext, names []string) model.Resources {
	var out model.Resources
	// TODO node local SAs only?
	services := proxy.SidecarScope.Services()
	workloads := push.SidecarlessIndex.Workloads
	for sa := range workloads.ByIdentity {
		for _, svc := range services {
			c := remoteOutboundCluster(proxy, sa, svc)
			out = append(out, &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)})
		}
	}

	for sa, saWorkloads := range workloads.NodeLocalBySA(proxy.Metadata.NodeName) {
		c := outboundTunnelCluster(proxy, push, sa, &saWorkloads[0])
		out = append(out, &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)})
	}

	out = append(out, buildPepClusters(proxy, push)...)
	out = append(out, g.buildVirtualInboundCluster(), passthroughCluster(push), blackholeCluster(push))
	return out
}

func blackholeCluster(push *model.PushContext) *discovery.Resource {
	c := &cluster.Cluster{
		Name:                 util.BlackHoleCluster,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		ConnectTimeout:       push.Mesh.ConnectTimeout,
		LbPolicy:             cluster.Cluster_ROUND_ROBIN,
	}
	return &discovery.Resource{
		Name:     c.Name,
		Resource: util.MessageToAny(c),
	}
}

func passthroughCluster(push *model.PushContext) *discovery.Resource {
	c := &cluster.Cluster{
		Name:                 util.PassthroughCluster,
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		ConnectTimeout:       push.Mesh.ConnectTimeout,
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		// TODO protocol options are copy-paste from v1alpha3 package
		TypedExtensionProtocolOptions: map[string]*any.Any{
			v3.HttpProtocolOptionsType: util.MessageToAny(&http.HttpProtocolOptions{
				UpstreamProtocolOptions: &http.HttpProtocolOptions_UseDownstreamProtocolConfig{
					UseDownstreamProtocolConfig: &http.HttpProtocolOptions_UseDownstreamHttpConfig{
						HttpProtocolOptions: &core.Http1ProtocolOptions{},
						Http2ProtocolOptions: &core.Http2ProtocolOptions{
							// Envoy default value of 100 is too low for data path.
							MaxConcurrentStreams: &wrappers.UInt32Value{
								Value: 1073741824,
							},
						},
					},
				},
			}),
		},
	}
	return &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)}
}

// buildPodOutboundCaptureListener creates a single listener with a FilterChain for each combination
// of ServiceAccount from pods on the node and Service VIP in the cluster.
func (g *UProxyConfigGenerator) buildPodOutboundCaptureListener(proxy *model.Proxy, push *model.PushContext) *discovery.Resource {
	l := &listener.Listener{
		Name:           "uproxy_outbound",
		UseOriginalDst: wrappers.Bool(true),
		Transparent:    wrappers.Bool(true),
		AccessLog:      accessLogString("outbound capture listener"),
		ListenerFilters: []*listener.ListenerFilter{{
			Name: wellknown.OriginalDestination,
			ConfigType: &listener.ListenerFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&originaldst.OriginalDst{}),
			},
		}, {
			Name: wellknown.OriginalSource,
			ConfigType: &listener.ListenerFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{
					Mark: OriginalSrcMark,
				}),
			},
		}},
		Address: &core.Address{Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address: "127.0.0.1",
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: UproxyOutboundCapturePort,
				},
			},
		}},
	}

	// match logic:
	// dest port == 15001 -> blackhole
	// source unknown -> passthrough
	// source known, has pep -> client PEP
	// source known, no pep, dest is a VIP -> resolve VIP, use passthrough metadata from EDS for tunnel headers
	// source known, no pep, dest NOT a VIP -> use original src/dest for tunnel headers (headless)

	sourceMatch := match.NewSourceIP()
	sourceMatch.OnNoMatch = match.ToChain(util.PassthroughFilterChain)

	destPortMatch := match.NewDestinationPort()
	destPortMatch.OnNoMatch = match.ToMatcher(sourceMatch.Matcher)
	destPortMatch.Map[strconv.Itoa(int(l.GetAddress().GetSocketAddress().GetPortValue()))] = match.ToChain(util.BlackHoleCluster)

	services := proxy.SidecarScope.Services()
	for _, workload := range push.SidecarlessIndex.Workloads.NodeLocal(proxy.Metadata.NodeName) {
		sourceAndDestMatch := match.NewDestinationIP()
		sourceMatch.Map[workload.Status.PodIP] = match.ToMatcher(sourceAndDestMatch.Matcher)

		peps := push.SidecarlessIndex.PEPs.ByIdentity[workload.Identity()] // TODO need to use this instead of ServiceAccountName

		if len(peps) > 0 && peps[0].Status.PodIP != "" {
			pep, pepIP := peps[0], peps[0].Status.PodIP
			chainName := workload.Name + "_" + workload.Status.PodIP + "_to_client_pep_" + pepIP
			// we won't program a match for destinations, so we want to go to client PEP
			sourceAndDestMatch.OnNoMatch = match.ToChain(chainName)

			// Has client PEP, send traffic there
			pepChain := &listener.FilterChain{
				Name: chainName,
				Filters: []*listener.Filter{{
					Name: wellknown.TCPProxy,
					ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
						AccessLog:        accessLogString("capture outbound (to client pep " + pepIP + ")"),
						StatPrefix:       "uproxy_out_" + workload.Name + "_" + workload.Status.PodIP + "_to_client_pep",
						ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: pepClusterName(pep.Identity())},
						TunnelingConfig: &tcp.TcpProxy_TunnelingConfig{
							Hostname: "istio-uproxy-to-pep", // (unused, per extended connect)
							HeadersToAdd: []*core.HeaderValueOption{
								// This is for server uProxy - not really needed for PEP
								{Header: &core.HeaderValue{Key: "x-envoy-original-dst-host", Value: "%DOWNSTREAM_LOCAL_ADDRESS%"}},

								// These are the HBONE headers
								{Header: &core.HeaderValue{Key: "x-original-ip", Value: "%DOWNSTREAM_LOCAL_ADDRESS_WITHOUT_PORT%"}},
								{Header: &core.HeaderValue{Key: "x-original-port", Value: "%DOWNSTREAM_LOCAL_PORT%"}},
								{Header: &core.HeaderValue{Key: "x-original-src", Value: "%DOWNSTREAM_REMOTE_ADDRESS_WITHOUT_PORT%"}},
								{Header: &core.HeaderValue{Key: "x-direction", Value: "outbound"}},
							},
						},
					},
					)},
				}},
			}
			l.FilterChains = append(l.FilterChains, pepChain)
		} else {
			// No client PEP, we build a chain per destination VIP
			for _, svc := range services {
				vip := svc.GetAddressForProxy(proxy)
				tunnelChain := &listener.FilterChain{
					Name: workload.Name + "_" + workload.Status.PodIP + "_to_" + svc.Hostname.String() + "_" + vip,
					FilterChainMatch: &listener.FilterChainMatch{
						SourcePrefixRanges: matchIP(workload.Status.PodIP),
						PrefixRanges:       matchIP(vip),
					},
					Filters: []*listener.Filter{{
						Name: wellknown.TCPProxy,
						ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
							AccessLog:        accessLogString("capture outbound (no pep)"),
							StatPrefix:       "uproxy_out_" + workload.Name + "_" + workload.Status.PodIP + "_to_" + svc.Hostname.String() + "_" + vip,
							ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: remoteOutboundClusterName(workload.Identity(), vip, svc.Hostname.String())},
						},
						)},
					}},
				}
				sourceAndDestMatch.Map[vip] = match.ToChain(tunnelChain.Name)
				l.FilterChains = append(l.FilterChains, tunnelChain)
			}
			// TODO headless if we know the source, but the destination is not a known VIP (go straight to outbound tunnel cluster, add headers here)
		}
	}
	l.FilterChainMatcher = destPortMatch.BuildMatcher()
	l.FilterChains = append(l.FilterChains, passthroughFilterChain(), blackholeFilterChain())
	return &discovery.Resource{
		Name:     l.Name,
		Resource: util.MessageToAny(l),
	}
}

func blackholeFilterChain() *listener.FilterChain {
	return &listener.FilterChain{
		Name: model.VirtualOutboundBlackholeFilterChainName,
		Filters: []*listener.Filter{{
			Name: wellknown.TCPProxy,
			ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
				AccessLog:        accessLogString("blackhole"),
				StatPrefix:       util.BlackHoleCluster,
				ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: util.BlackHoleCluster},
			})},
		}},
	}
}

func passthroughFilterChain() *listener.FilterChain {
	return &listener.FilterChain{
		Name: util.PassthroughFilterChain,
		/// TODO no match – add one to make it so we only passthrough if strict mTLS to the destination is allowed
		Filters: []*listener.Filter{{
			Name: wellknown.TCPProxy,
			ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
				AccessLog:        accessLogString("passthrough"),
				StatPrefix:       util.PassthroughCluster,
				ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: util.PassthroughCluster},
			})},
		}},
	}
}

// remoteOutboundCluster points to outboundTunnelListener (internal listener) via EDS metadata.
func remoteOutboundCluster(proxy *model.Proxy, sa string, svc *model.Service) *cluster.Cluster {
	return &cluster.Cluster{
		Name:                 remoteOutboundClusterName(sa, svc.GetAddressForProxy(proxy), svc.Hostname.String()),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
		EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
			EdsConfig: &core.ConfigSource{
				ConfigSourceSpecifier: &core.ConfigSource_Ads{
					Ads: &core.AggregatedConfigSource{},
				},
				InitialFetchTimeout: durationpb.New(0),
				ResourceApiVersion:  core.ApiVersion_V3,
			},
		},
		TransportSocket: &core.TransportSocket{
			Name: "envoy.transport_sockets.internal_upstream",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&internalupstream.InternalUpstreamTransport{
				PassthroughMetadata: []*internalupstream.InternalUpstreamTransport_MetadataValueSource{{
					Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{}},
					Name: "tunnel",
				}},
				TransportSocket: &core.TransportSocket{
					Name:       "envoy.transport_sockets.raw_buffer",
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&rawbuffer.RawBuffer{})},
				},
			})},
		},
	}
}

func remoteOutboundClusterName(sa, vip, hostname string) string {
	return fmt.Sprintf("%s_to_%s_%s_outbound_internal", sa, vip, hostname)
}

func parseRemoteOutboundClusterName(clusterName string) (sa, vip, hostname string, err error) {
	p := strings.Split(clusterName, "_")
	if !strings.HasSuffix(clusterName, "_outbound_internal") || len(p) < 3 {
		err = fmt.Errorf("parseRemoteOutboundClusterName: invalid cluster")
		return
	}
	return p[0], p[2], p[3], err
}

func pepClusterName(sa string) string {
	return sa + "_pep"
}

func buildPepClusters(proxy *model.Proxy, push *model.PushContext) model.Resources {
	var clusters []*cluster.Cluster
	for sa, peps := range push.SidecarlessIndex.PEPs.ByIdentity {
		saWorkloads := push.SidecarlessIndex.Workloads.NodeLocalBySA(proxy.Metadata.NodeName)[sa]
		if len(saWorkloads) == 0 || len(peps) == 0 {
			// no peps or no workloads that use this client PEP on the node
			continue
		}
		workload := saWorkloads[0] // we use this pod id for fetching cert

		clusters = append(clusters, &cluster.Cluster{
			Name:                          pepClusterName(sa),
			ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
			LbPolicy:                      cluster.Cluster_ROUND_ROBIN,
			ConnectTimeout:                durationpb.New(10 * time.Second),
			CleanupInterval:               durationpb.New(60 * time.Second),
			TypedExtensionProtocolOptions: h2connectUpgrade(),
			TransportSocket: &core.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.UpstreamTlsContext{
					CommonTlsContext: buildCommonTLSContext(proxy, &workload, push, false),
				})},
			},
			LoadAssignment: &endpoint.ClusterLoadAssignment{
				ClusterName: pepClusterName(sa),
				Endpoints: []*endpoint.LocalityLbEndpoints{{
					LbEndpoints: []*endpoint.LbEndpoint{{
						HostIdentifier: &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{
							Address: &core.Address{
								Address: &core.Address_SocketAddress{
									SocketAddress: &core.SocketAddress{
										Address:       peps[0].Status.PodIP, // TODO support multiple
										PortSpecifier: &core.SocketAddress_PortValue{PortValue: UproxyOutboundCapturePort},
									},
								},
							},
						}},
					}},
				}},
			},
		})
	}
	var out model.Resources
	for _, c := range clusters {
		out = append(out, &discovery.Resource{
			Name:     c.Name,
			Resource: util.MessageToAny(c),
		})
	}
	return out
}

func (g *UProxyConfigGenerator) BuildEndpoints(proxy *model.Proxy, push *model.PushContext, names []string) model.Resources {
	out := model.Resources{}
	for _, clusterName := range names {
		// sa here is already our "envoy friendly" one
		sa, _, hostname, err := parseRemoteOutboundClusterName(clusterName)
		if err != nil {
			continue
		}
		svc := push.ServiceForHostname(proxy, host.Name(hostname))
		out = append(out, &discovery.Resource{
			Name: clusterName,
			Resource: util.MessageToAny(&endpoint.ClusterLoadAssignment{
				ClusterName: clusterName,
				Endpoints:   g.llbEndpointsFromShards(proxy, sa, svc),
			}),
		})
	}
	return out
}

func (g *UProxyConfigGenerator) llbEndpointsFromShards(
	proxy *model.Proxy,
	sa string,
	svc *model.Service,
) []*endpoint.LocalityLbEndpoints {
	if svc == nil {
		return nil
	}
	shards, ok := g.EndpointIndex.ShardsForService(svc.Hostname.String(), svc.Attributes.Namespace)
	if !ok || shards == nil {
		uproxyLog.Warnf("no endpoint shards for %s/%s", svc.Attributes.Namespace, svc.Attributes.Name)
		return nil
	}
	eps := &endpoint.LocalityLbEndpoints{
		LbEndpoints: nil,
	}
	for _, shard := range shards.Shards {
		for _, istioEndpoint := range shard {

			capturePort := UproxyInboundCapturePort
			// TODO passthrough for node-local upstreams without PEPs
			if node := istioEndpoint.NodeName; node != "" && node == proxy.Metadata.NodeName {
				capturePort = UproxyInboundNodeLocalCapturePort
			}
			// TODO re-use some eds code; stable eds ordering, support multi-cluster cluster local rules, and multi-network stuff
			metadata, err := structpb.NewStruct(map[string]interface{}{
				"target":           outboundTunnelListenerName(sa),
				"tunnel_address":   net.JoinHostPort(istioEndpoint.Address, strconv.Itoa(int(capturePort))), // TODO tunnel address changes if we have a Server PEP
				"detunnel_address": net.JoinHostPort(istioEndpoint.Address, strconv.Itoa(int(istioEndpoint.EndpointPort))),
				"detunnel_ip":      istioEndpoint.Address,
				"detunnel_port":    strconv.Itoa(int(istioEndpoint.EndpointPort)),
			})
			if err != nil {
				uproxyLog.Warnf("error building metadata for %s: %v", err)
			}
			eps.LbEndpoints = append(eps.LbEndpoints, &endpoint.LbEndpoint{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Address:       istioEndpoint.Address,
								PortSpecifier: &core.SocketAddress_PortValue{PortValue: istioEndpoint.EndpointPort},
							},
						},
					},
				}},
				Metadata: &core.Metadata{FilterMetadata: map[string]*structpb.Struct{
					"tunnel": metadata,
				}}, // TODO metadata
				LoadBalancingWeight: wrappers.UInt32(1),
			})
		}
	}
	return []*endpoint.LocalityLbEndpoints{eps}
}

func outboundTunnelListenerName(sa string) string {
	return "outbound_tunnel_lis_" + sa
}

// outboundTunnelListener is built for each ServiceAccount from pods on the node.
// This listener adds the original destination headers from the dynamic EDS metadata pass through.
// We build the listener per-service account so that it can point to the corresponding cluster that presents the correct cert.
func outboundTunnelListener(listenerNameOverride, sa string) *discovery.Resource {
	name := outboundTunnelListenerName(sa)
	if listenerNameOverride != "" {
		name = listenerNameOverride
	}

	l := &listener.Listener{
		Name:              name,
		UseOriginalDst:    wrappers.Bool(false),
		ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		Address: &core.Address{Address: &core.Address_EnvoyInternalAddress{
			EnvoyInternalAddress: &core.EnvoyInternalAddress{
				AddressNameSpecifier: &core.EnvoyInternalAddress_ServerListenerName{
					ServerListenerName: name,
				},
			},
		}},
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{
				Name: wellknown.TCPProxy,
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: util.MessageToAny(&tcp.TcpProxy{
						StatPrefix:       name,
						AccessLog:        accessLogString("outbound tunnel"),
						ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: outboundTunnelClusterName(sa)},
						TunnelingConfig: &tcp.TcpProxy_TunnelingConfig{
							Hostname: "host.com:443", // TODO not sure how to set host properly here without svc?
							HeadersToAdd: []*core.HeaderValueOption{
								{Header: &core.HeaderValue{Key: "x-envoy-original-dst-host", Value: "%DYNAMIC_METADATA([\"tunnel\", \"detunnel_address\"])%"}},
								// TODO the following are unused at this point
								{Header: &core.HeaderValue{Key: "x-original-ip", Value: "%DYNAMIC_METADATA([\"tunnel\", \"detunnel_ip\"])%"}},
								{Header: &core.HeaderValue{Key: "x-original-port", Value: "%DYNAMIC_METADATA([\"tunnel\", \"detunnel_port\"])%"}},
							},
						},
					}),
				},
			}},
		}},
	}
	return &discovery.Resource{
		Name:     name,
		Resource: util.MessageToAny(l),
	}
}

func buildCommonTLSContext(proxy *model.Proxy, workload *ambient.Workload, push *model.PushContext, inbound bool) *tls.CommonTlsContext {
	ctx := &tls.CommonTlsContext{}
	security.ApplyToCommonTLSContext(ctx, proxy, nil, authn.TrustDomainsForValidation(push.Mesh), inbound)

	// TODO always use the below flow, always specify which workload
	if workload != nil {
		// present the workload cert if possible
		workloadSecret := kube.SecureNamingSAN(workload.Pod)
		if workload.UID != "" {
			workloadSecret += "~" + workload.Name + "~" + string(workload.UID)
		}
		ctx.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
			security.ConstructSdsSecretConfig(workloadSecret),
		}
	}

	return ctx
}

// outboundTunnelCluster is per-workload SA, but requires one workload that uses that SA so we can send the Pod UID
func outboundTunnelCluster(proxy *model.Proxy, push *model.PushContext, sa string, workload *ambient.Workload) *cluster.Cluster {
	return &cluster.Cluster{
		Name:                          outboundTunnelClusterName(sa),
		ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:                      cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:                durationpb.New(10 * time.Second),
		CleanupInterval:               durationpb.New(60 * time.Second),
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		TransportSocket: &core.TransportSocket{
			Name: "envoy.transport_sockets.tls",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: buildCommonTLSContext(proxy, workload, push, false),
			})},
		},
	}
}

func outboundTunnelClusterName(sa string) string {
	return "outbound_tunnel_clus_" + sa
}

// buildInboundCaptureListener creates a single listener with a FilterChain for each pod on the node.
func (g *UProxyConfigGenerator) buildInboundCaptureListener(proxy *model.Proxy, push *model.PushContext) *discovery.Resource {
	// TODO L7 stuff (deny at l4 for l7 auth if there is a remote proxy for the dest workload)

	l := &listener.Listener{
		Name:           "uproxy_inbound",
		UseOriginalDst: wrappers.Bool(true),
		Transparent:    wrappers.Bool(true),
		ListenerFilters: []*listener.ListenerFilter{
			{
				Name: wellknown.OriginalDestination,
				ConfigType: &listener.ListenerFilter_TypedConfig{
					TypedConfig: util.MessageToAny(&originaldst.OriginalDst{}),
				},
			},
			{
				Name: wellknown.OriginalSource,
				ConfigType: &listener.ListenerFilter_TypedConfig{
					TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{
						Mark: OriginalSrcMark,
					}),
				},
			},
		},
		AccessLog: accessLogString("capture inbound listener"),
		Address: &core.Address{Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				// TODO because of the port 15088 workaround, we need to use a redirect rule,
				// which means we can't bind to localhost. once we remove that workaround,
				// this can be changed back to 127.0.0.1
				Address: "0.0.0.0",
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: UproxyInboundCapturePort,
				},
			},
		}},
	}

	for _, workload := range push.SidecarlessIndex.Workloads.NodeLocal(proxy.Metadata.NodeName) {
		l.FilterChains = append(l.FilterChains, &listener.FilterChain{
			Name:             "inbound_" + workload.Status.PodIP,
			FilterChainMatch: &listener.FilterChainMatch{PrefixRanges: matchIP(workload.Status.PodIP)},
			TransportSocket: &core.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.DownstreamTlsContext{
					CommonTlsContext: buildCommonTLSContext(proxy, &workload, push, true),
				})},
			},
			Filters: []*listener.Filter{{
				Name: "envoy.filters.network.http_connection_manager",
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: util.MessageToAny(&httpconn.HttpConnectionManager{
						AccessLog:  accessLogString("inbound hcm"),
						CodecType:  0,
						StatPrefix: "inbound_hcm",
						RouteSpecifier: &httpconn.HttpConnectionManager_RouteConfig{
							RouteConfig: &route.RouteConfiguration{
								Name: "local_route",
								VirtualHosts: []*route.VirtualHost{{
									Name:    "local_service",
									Domains: []string{"*"},
									Routes: []*route.Route{{
										Match: &route.RouteMatch{PathSpecifier: &route.RouteMatch_ConnectMatcher_{
											ConnectMatcher: &route.RouteMatch_ConnectMatcher{},
										}},
										Action: &route.Route_Route{
											Route: &route.RouteAction{
												UpgradeConfigs: []*route.RouteAction_UpgradeConfig{{
													UpgradeType:   "CONNECT",
													ConnectConfig: &route.RouteAction_UpgradeConfig_ConnectConfig{},
												}},
												ClusterSpecifier: &route.RouteAction_Cluster{
													Cluster: "virtual_inbound",
												},
											},
										},
									}},
								}},
							},
						},
						// TODO rewrite destination port to original_dest port
						HttpFilters: []*httpconn.HttpFilter{{
							Name:       "envoy.filters.http.router",
							ConfigType: &httpconn.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(&routerfilter.Router{})},
						}},
						Http2ProtocolOptions: &core.Http2ProtocolOptions{
							AllowConnect: true,
						},
						UpgradeConfigs: []*httpconn.HttpConnectionManager_UpgradeConfig{{
							UpgradeType: "CONNECT",
						}},
					}),
				},
			}},
		})
	}
	//TODO cases where we passthrough
	l.FilterChains = append(l.FilterChains, blackholeFilterChain())

	return &discovery.Resource{
		Name:     l.Name,
		Resource: util.MessageToAny(l),
	}
}

func (g *UProxyConfigGenerator) buildVirtualInboundCluster() *discovery.Resource {
	c := &cluster.Cluster{
		Name:                 "virtual_inbound",
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		LbConfig: &cluster.Cluster_OriginalDstLbConfig_{
			OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{
				UseHttpHeader: true,
			},
		},
	}
	return &discovery.Resource{
		Name:     c.Name,
		Resource: util.MessageToAny(c),
	}
}

func matchIP(addr string) []*core.CidrRange {
	return []*core.CidrRange{{
		AddressPrefix: addr,
		PrefixLen:     wrappers.UInt32(32),
	}}
}

const accessLogStringFormat = " -- ds remote: %DOWNSTREAM_REMOTE_ADDRESS%; ds local: %DOWNSTREAM_LOCAL_ADDRESS% -> us local: %UPSTREAM_LOCAL_ADDRESS%\n"

func accessLogString(prefix string) []*accesslog.AccessLog {
	inlineString := prefix + accessLogStringFormat
	return []*accesslog.AccessLog{{
		Name: "envoy.access_loggers.file",
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(&fileaccesslog.FileAccessLog{
			Path: "/dev/stdout",
			AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{TextFormatSource: &core.DataSource{Specifier: &core.DataSource_InlineString{
					InlineString: inlineString,
				}}},
			}},
		})},
	}}
}

func h2connectUpgrade() map[string]*any.Any {
	return map[string]*any.Any{
		v3.HttpProtocolOptionsType: util.MessageToAny(&http.HttpProtocolOptions{
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

func ipPortAddress(ip string, port uint32) *core.Address {
	return &core.Address{Address: &core.Address_SocketAddress{
		SocketAddress: &core.SocketAddress{
			Address: ip,
			PortSpecifier: &core.SocketAddress_PortValue{
				PortValue: port,
			},
		},
	}}
}

func internalAddress(name string) *core.Address {
	return &core.Address{Address: &core.Address_EnvoyInternalAddress{EnvoyInternalAddress: &core.EnvoyInternalAddress{
		AddressNameSpecifier: &core.EnvoyInternalAddress_ServerListenerName{ServerListenerName: name},
	}}}
}
