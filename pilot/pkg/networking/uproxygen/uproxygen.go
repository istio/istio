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

/*
Listener uproxy_outbound:
  Chain: <srcpodname>_<srcpodip>_to_<portname>_<hostname>_<vip>
      -> CDS: <identity>_to_<vip>_<portname>_<hostname>_outbound_internal
          transport: internal
					-> EDS:
							address: podIP
							tunnel: outbound_tunnel_lis_<identity>
  Chain: <srcpodname>_<srcpodip>_to_client_pep_<pep>
      tunneling_config: istio-uproxy-to-pep
      -> CDS: _to_client_pep_<source identity> (EDS)
           address: PEP_IP
           transport: TLS
  Chain: <srcpodname>_<srcpodip>_to_server_pep_<pep>
      tunneling_config: istio-uproxy-to-pep
      -> CDS: <source identity>_to_server_pep_<server identity> (EDS)
           address: PEP_IP
           transport: TLS
  Chain: passthrough
  Chain: blackhole

Internal listener: outbound_tunnel_lis_<identity>
      tunneling_config: host.com:443
      -> CDS: outbound_tunnel_clus_<identity> (ORIG_DST)
          transport: TLS

Listener uproxy_inbound:
  Chain: inbound_<podip>
      transport: terminate TLS
      match: CONNECT
      -> CDS: virtual_inbound (ORIG_DST)
  Chain: blackhole
*/

package uproxygen

import (
	"fmt"
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
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	any "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/match"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("uproxygen", "xDS Generator for uProxy clients", 0)

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
	UproxyInbound2CapturePort         uint32 = 15006
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
		g.buildInboundPlaintextCaptureListener(proxy, push),
	)
	for sa := range push.SidecarlessIndex.Workloads.ByIdentity {
		out = append(out, outboundTunnelListener(outboundTunnelListenerName(sa), sa))
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
			for _, port := range svc.Ports {
				c := remoteOutboundCluster(sa, svc, port.Name)
				out = append(out, &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)})
			}
		}
	}

	for sa, saWorkloads := range workloads.NodeLocalBySA(proxy.Metadata.NodeName) {
		c := outboundTunnelCluster(proxy, push, sa, &saWorkloads[0])
		out = append(out, &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)})
	}
	for sa, saWorkloads := range workloads.NodeLocalBySA(proxy.Metadata.NodeName) {
		c := outboundPodTunnelCluster(proxy, push, sa, &saWorkloads[0])
		out = append(out, &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)})
	}
	if features.SidecarlessCapture == model.VariantIptables {
		for sa, saWorkloads := range workloads.NodeLocalBySA(proxy.Metadata.NodeName) {
			c := outboundPodLocalTunnelCluster(proxy, push, sa, &saWorkloads[0])
			out = append(out, &discovery.Resource{Name: c.Name, Resource: util.MessageToAny(c)})
		}
	}

	out = append(out, buildPepClusters(proxy, push)...)
	out = append(out, g.buildVirtualInboundCluster(), passthroughCluster(push), tcpPassthroughCluster(push), blackholeCluster(push))
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

func tcpPassthroughCluster(push *model.PushContext) *discovery.Resource {
	c := &cluster.Cluster{
		Name:                 util.PassthroughCluster + "-tcp",
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		ConnectTimeout:       push.Mesh.ConnectTimeout,
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
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
		ListenerFilters: []*listener.ListenerFilter{
			{
				Name: wellknown.OriginalDestination,
				ConfigType: &listener.ListenerFilter_TypedConfig{
					TypedConfig: util.MessageToAny(&originaldst.OriginalDst{}),
				},
			},
		},
		Address: &core.Address{Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address: "0.0.0.0",
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: UproxyOutboundCapturePort,
				},
			},
		}},
	}
	if features.SidecarlessCapture == model.VariantIptables {
		l.ListenerFilters = append(l.ListenerFilters, &listener.ListenerFilter{
			Name: wellknown.OriginalSource,
			ConfigType: &listener.ListenerFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{
					Mark: OriginalSrcMark,
				}),
			},
		})
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
	seen := sets.New()
	for _, sourceWl := range push.SidecarlessIndex.Workloads.NodeLocal(proxy.Metadata.NodeName) {
		sourceAndDestMatch := match.NewDestinationIP()
		// TODO: handle host network better, which has a shared IP
		sourceMatch.Map[sourceWl.Status.PodIP] = match.ToMatcher(sourceAndDestMatch.Matcher)

		clientPeps := push.SidecarlessIndex.PEPs.ByIdentity[sourceWl.Identity()] // TODO need to use this instead of ServiceAccountName
		clientPepChain := buildPepChain(sourceWl, clientPeps, "client")

		for _, svc := range services {
			// No client PEP, we build a chain per destination VIP
			vip := svc.GetAddressForProxy(proxy)

			portMatch := match.NewDestinationPort()
			sourceAndDestMatch.Map[vip] = match.ToMatcher(portMatch.Matcher)
			for _, port := range svc.Ports {
				var chain *listener.FilterChain
				// Need to decide if there is a server PEP. This is somewhat problematic because a Service may span PEP and non-PEP.
				// If any workload behind the service has a PEP, we will use the PEP. In 99% of cases this is homogenous.
				var serverPepChain *listener.FilterChain
				for _, wl := range push.SidecarlessIndex.Workloads.All() {
					if wl.Namespace != svc.Attributes.Namespace {
						continue
					}
					if !labels.Instance(svc.Attributes.LabelSelectors).SubsetOf(wl.Labels) {
						continue
					}
					serverPepChain = buildPepChain(sourceWl, push.SidecarlessIndex.PEPs.ByIdentity[wl.Identity()], "server")
					break
				}
				if serverPepChain != nil {
					// Has server PEP, send traffic there
					chain = serverPepChain
				} else if clientPepChain != nil {
					// Has client PEP, send traffic there
					chain = clientPepChain
				} else {
					// No PEP
					name := remoteOutboundClusterName(sourceWl.Identity(), port.Name, svc.Hostname.String())
					chain = &listener.FilterChain{
						Name: name,
						Filters: []*listener.Filter{{
							Name: wellknown.TCPProxy,
							ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
								AccessLog:        accessLogString("capture outbound (no pep)"),
								StatPrefix:       name,
								ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: name},
							},
							)},
						}},
					}
				}

				if !seen.InsertContains(chain.Name) {
					l.FilterChains = append(l.FilterChains, chain)
				}
				portMatch.Map[fmt.Sprint(port.Port)] = match.ToChain(chain.Name)
			}
		}
		// Add chain for each pod IP
		wls := push.SidecarlessIndex.Workloads.All()
		wls = append(wls, push.SidecarlessIndex.None.All()...)
		for _, wl := range wls {
			var chain *listener.FilterChain
			// Need to decide if there is a server PEP. This is somewhat problematic because a Service may span PEP and non-PEP.
			// If any workload behind the service has a PEP, we will use the PEP. In 99% of cases this is homogenous.
			serverPepChain := buildPepChain(sourceWl, push.SidecarlessIndex.PEPs.ByIdentity[wl.Identity()], "server")
			if serverPepChain != nil {
				// Has server PEP, send traffic there
				chain = serverPepChain
			} else if clientPepChain != nil {
				// Has client PEP, send traffic there
				chain = clientPepChain
			} else {
				// No PEP
				// Naively, we could simply create a FC with tunnel_config here and point to an original_dst cluster.
				// This won't work for a few reasons:
				// * We need to override the port. `x-envoy-original-dst-host` cannot be used since it is an
				//   upstream header; the cluster looks for downstream headers
				// * We could add config to orig_dst cluster to override the port. This would almost work, but
				//   then we run into issues with the original_src filter. Currently, this filter is on the listener filter
				//   but it only applies for direct connections. When we go through another internal listener, the effect is lost.
				//   Ultimately that means for tunneling, we do not use the original_src but for direct calls we do. This means
				//   that we will need to go through an internal listener to "break" the original_src effect.
				// TODO: this is broken
				// If we use outboundTunnelClusterName, we get orig_dst, but x-envoy-original-dst-host is an upstream header
				// while the cluster looks for downstream headers.
				// if we make a dedicate cluster, we cannot pass the original port anymore since the context is lost.
				// We cannot create a cluster per port since it can be any port.
				// TODO2: this is still broken even with custom orig_dst. the listener sets the orig_src mark
				// If we

				name := sourceWl.Identity() + "_to_" + wl.Status.PodIP
				tunnel := &tcp.TcpProxy_TunnelingConfig{
					Hostname: "istio-uproxy-to-pep", // (unused, per extended connect)
					HeadersToAdd: []*core.HeaderValueOption{
						// This is for server uProxy - not really needed for PEP
						{Header: &core.HeaderValue{Key: "x-envoy-original-dst-host", Value: "%DOWNSTREAM_LOCAL_ADDRESS%"}},

						// These are the MTP headers
						{Header: &core.HeaderValue{Key: "x-original-ip", Value: "%DOWNSTREAM_LOCAL_ADDRESS_WITHOUT_PORT%"}},
						{Header: &core.HeaderValue{Key: "x-original-port", Value: "%DOWNSTREAM_LOCAL_PORT%"}},
						{Header: &core.HeaderValue{Key: "x-original-src", Value: "%DOWNSTREAM_REMOTE_ADDRESS_WITHOUT_PORT%"}},
						{Header: &core.HeaderValue{Key: "x-direction", Value: "outbound"}},
					},
				}
				// Case 1: tunnel cross node
				cluster := outboundPodTunnelClusterName(sourceWl.Identity())
				// Case 2: same node tunnel (iptables)
				if node := wl.Spec.NodeName; node != "" && node == proxy.Metadata.NodeName && features.SidecarlessCapture == model.VariantIptables {
					cluster = outboundPodLocalTunnelClusterName(sourceWl.Identity())
				}
				// Case 3: direct
				if wl.Labels[ambient.LabelType] != ambient.TypeWorkload {
					cluster = util.PassthroughCluster + "-tcp"
					tunnel = nil
				}
				// Case 4: same node tunnel (bpf)
				// Currently we don't get redirection from remote -> pod on same node
				if node := wl.Spec.NodeName; node != "" && node == proxy.Metadata.NodeName && features.SidecarlessCapture == model.VariantBpf {
					cluster = util.PassthroughCluster + "-tcp"
					tunnel = nil
				}

				chain = &listener.FilterChain{
					Name: name,
					Filters: []*listener.Filter{
						{
							Name: wellknown.TCPProxy,
							ConfigType: &listener.Filter_TypedConfig{
								TypedConfig: util.MessageToAny(&tcp.TcpProxy{
									AccessLog:        accessLogString("capture outbound pod (no pep)"),
									StatPrefix:       name,
									ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: cluster},
									TunnelingConfig:  tunnel,
								}),
							},
						},
					},
				}
			}

			if !seen.InsertContains(chain.Name) {
				l.FilterChains = append(l.FilterChains, chain)
			}
			sourceAndDestMatch.Map[wl.Status.PodIP] = match.ToChain(chain.Name)
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

func buildPepChain(workload ambient.Workload, peps []ambient.Workload, t string) *listener.FilterChain {
	if len(peps) == 0 {
		return nil
	}

	// pep is just for identity (same across multiple PEPs)
	pep := peps[0]
	// For client PEP, we know the PEP and client are always the same identity which simplifies things; we can share a cluster for all
	cluster := pepClusterName(pep.Identity())
	if t == "server" {
		// For server, we need to create the product of source identity x PEP
		cluster = serverPepClusterName(pep.Identity(), workload.Identity())
	}
	return &listener.FilterChain{
		Name: cluster,
		Filters: []*listener.Filter{{
			Name: wellknown.TCPProxy,
			ConfigType: &listener.Filter_TypedConfig{TypedConfig: util.MessageToAny(&tcp.TcpProxy{
				AccessLog:        accessLogString(fmt.Sprintf("capture outbound (to %v pep)", t)),
				StatPrefix:       cluster,
				ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: cluster},
				TunnelingConfig: &tcp.TcpProxy_TunnelingConfig{
					Hostname: "istio-uproxy-to-pep", // (unused, per extended connect)
					HeadersToAdd: []*core.HeaderValueOption{
						// This is for server uProxy - not really needed for PEP
						{Header: &core.HeaderValue{Key: "x-envoy-original-dst-host", Value: "%DOWNSTREAM_LOCAL_ADDRESS%"}},

						// These are the MTP headers
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

func remoteOutboundCluster(sa string, svc *model.Service, port string) *cluster.Cluster {
	return &cluster.Cluster{
		Name:                 remoteOutboundClusterName(sa, port, svc.Hostname.String()),
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
		TransportSocketMatches: v1alpha3.InternalUpstreamSocketMatch,
	}
}

func remoteOutboundClusterName(sa, port string, hostname string) string {
	return fmt.Sprintf("%s_to_%s_%s_outbound_internal", sa, port, hostname)
}

func parseRemoteOutboundClusterName(clusterName string) (sa, port string, hostname string, ok bool) {
	p := strings.Split(clusterName, "_")
	if !strings.HasSuffix(clusterName, "_outbound_internal") || len(p) < 3 {
		return "", "", "", false
	}
	return p[0], p[2], p[3], true
}

func pepClusterName(pep string) string {
	return fmt.Sprintf("_to_client_pep_%s", pep)
}

func serverPepClusterName(pep, workload string) string {
	return fmt.Sprintf("%s_to_server_pep_%s", workload, pep)
}

// pep cluster names are in the format {src}_to_{t}_pep_{dst} where src/dst are identities
func parsePepClusterName(name string) (src, dst, t string, ok bool) {
	p := strings.Split(name, "_")
	if len(p) != 5 || p[1] != "to" || p[3] != "pep" {
		return "", "", "", false
	}
	return p[0], p[4], p[2], true
}

func buildPepClusters(proxy *model.Proxy, push *model.PushContext) model.Resources {
	var clusters []*cluster.Cluster
	// Client PEPs
	for sa, peps := range push.SidecarlessIndex.PEPs.ByIdentity {
		saWorkloads := push.SidecarlessIndex.Workloads.NodeLocalBySA(proxy.Metadata.NodeName)[sa]
		if len(saWorkloads) == 0 || len(peps) == 0 {
			// no peps or no workloads that use this client PEP on the node
			continue
		}
		workload := saWorkloads[0] // we use this pod id for fetching cert

		clusters = append(clusters, &cluster.Cluster{
			Name:                          pepClusterName(sa),
			ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
			LbPolicy:                      cluster.Cluster_ROUND_ROBIN,
			ConnectTimeout:                durationpb.New(2 * time.Second),
			TypedExtensionProtocolOptions: h2connectUpgrade(),
			TransportSocket: &core.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.UpstreamTlsContext{
					CommonTlsContext: buildCommonTLSContext(proxy, &workload, push, false),
				})},
			},
			EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
				EdsConfig: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
					InitialFetchTimeout: durationpb.New(0),
					ResourceApiVersion:  core.ApiVersion_V3,
				},
			},
		})
	}
	for pepSA, peps := range push.SidecarlessIndex.PEPs.ByIdentity {
		for workloadSA, workloads := range push.SidecarlessIndex.Workloads.NodeLocalBySA(proxy.Metadata.NodeName) {
			if len(workloads) == 0 || len(peps) == 0 {
				// no peps or no workloads that use this identity on the node
				continue
			}
			workload := workloads[0] // we use this pod id for fetching cert
			clusters = append(clusters, &cluster.Cluster{
				Name:                          serverPepClusterName(pepSA, workloadSA),
				ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
				LbPolicy:                      cluster.Cluster_ROUND_ROBIN,
				ConnectTimeout:                durationpb.New(2 * time.Second),
				TypedExtensionProtocolOptions: h2connectUpgrade(),
				TransportSocket: &core.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.UpstreamTlsContext{
						CommonTlsContext: buildCommonTLSContext(proxy, &workload, push, false),
					})},
				},
				EdsClusterConfig: &cluster.Cluster_EdsClusterConfig{
					EdsConfig: &core.ConfigSource{
						ConfigSourceSpecifier: &core.ConfigSource_Ads{
							Ads: &core.AggregatedConfigSource{},
						},
						InitialFetchTimeout: durationpb.New(0),
						ResourceApiVersion:  core.ApiVersion_V3,
					},
				},
			})
		}
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
	// uproxy outbound to upstream
	for _, clusterName := range names {
		// sa here is already our "envoy friendly" one
		sa, port, hostname, ok := parseRemoteOutboundClusterName(clusterName)
		if !ok {
			continue
		}
		svc := push.ServiceForHostname(proxy, host.Name(hostname))
		out = append(out, &discovery.Resource{
			Name: clusterName,
			Resource: util.MessageToAny(&endpoint.ClusterLoadAssignment{
				ClusterName: clusterName,
				Endpoints:   g.upstreamLbEndpointsFromShards(proxy, sa, svc, port),
			}),
		})
	}
	// uproxy to pep
	for _, clusterName := range names {
		_, dst, t, ok := parsePepClusterName(clusterName)
		if !ok {
			continue
		}
		out = append(out, &discovery.Resource{
			Name: clusterName,
			Resource: util.MessageToAny(&endpoint.ClusterLoadAssignment{
				ClusterName: clusterName,
				Endpoints:   buildPepLbEndpoints(dst, t, push),
			}),
		})
	}
	return out
}

func (g *UProxyConfigGenerator) upstreamLbEndpointsFromShards(proxy *model.Proxy, sa string, svc *model.Service, port string) []*endpoint.LocalityLbEndpoints {
	if svc == nil {
		return nil
	}
	shards, ok := g.EndpointIndex.ShardsForService(svc.Hostname.String(), svc.Attributes.Namespace)
	if !ok || shards == nil {
		log.Warnf("no endpoint shards for %s/%s", svc.Attributes.Namespace, svc.Attributes.Name)
		return nil
	}
	eps := &endpoint.LocalityLbEndpoints{
		LbEndpoints: nil,
	}
	for _, shard := range shards.Shards {
		for _, istioEndpoint := range shard {
			if port != istioEndpoint.ServicePortName {
				continue
			}
			lbe := &endpoint.LbEndpoint{
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
				LoadBalancingWeight: wrappers.UInt32(1),
			}

			capturePort := UproxyInboundCapturePort
			// TODO passthrough for node-local upstreams without PEPs
			if node := istioEndpoint.NodeName; node != "" && node == proxy.Metadata.NodeName && features.SidecarlessCapture == model.VariantIptables {
				capturePort = UproxyInboundNodeLocalCapturePort
			}
			supportsTunnel := false
			if al := istioEndpoint.Labels[ambient.LabelType]; al == ambient.TypePEP || al == ambient.TypeWorkload {
				supportsTunnel = true
			}
			// TODO: On BPF mode, we currently do not get redirect for same node Remote -> Pod
			// Instead, just go direct
			if node := istioEndpoint.NodeName; node != "" && node == proxy.Metadata.NodeName && features.SidecarlessCapture == model.VariantBpf {
				supportsTunnel = false
			}
			if supportsTunnel {
				// TODO re-use some eds code; stable eds ordering, support multi-cluster cluster local rules, and multi-network stuff
				metadata, err := structpb.NewStruct(map[string]interface{}{
					"target":           outboundTunnelListenerName(sa),
					"tunnel_address":   net.JoinHostPort(istioEndpoint.Address, strconv.Itoa(int(capturePort))), // TODO tunnel address changes if we have a Server PEP
					"detunnel_address": net.JoinHostPort(istioEndpoint.Address, strconv.Itoa(int(istioEndpoint.EndpointPort))),
					"detunnel_ip":      istioEndpoint.Address,
					"detunnel_port":    strconv.Itoa(int(istioEndpoint.EndpointPort)),
				})
				if err != nil {
					log.Warnf("error building metadata for %s: %v", err)
				}
				lbe.Metadata = &core.Metadata{FilterMetadata: map[string]*structpb.Struct{
					"tunnel": metadata,
				}}
				lbe.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey] = &structpb.Struct{
					Fields: map[string]*structpb.Value{
						ambient.TransportMatchKey: {Kind: &structpb.Value_StringValue{StringValue: ambient.TransportMatchValue}},
					},
				}
			}
			eps.LbEndpoints = append(eps.LbEndpoints, lbe)
		}
	}
	return []*endpoint.LocalityLbEndpoints{eps}
}

func buildPepLbEndpoints(pepIdentity, t string, push *model.PushContext) []*endpoint.LocalityLbEndpoints {
	port := UproxyOutboundCapturePort
	if t == "server" {
		port = UproxyInbound2CapturePort
	}
	peps := push.SidecarlessIndex.PEPs.ByIdentity[pepIdentity]

	lbEndpoints := &endpoint.LocalityLbEndpoints{
		LbEndpoints: []*endpoint.LbEndpoint{},
	}
	for _, pep := range peps {
		lbEndpoints.LbEndpoints = append(lbEndpoints.LbEndpoints, &endpoint.LbEndpoint{
			HostIdentifier: &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address:       pep.Status.PodIP,
							PortSpecifier: &core.SocketAddress_PortValue{PortValue: port},
						},
					},
				},
			}},
		})
	}
	return []*endpoint.LocalityLbEndpoints{lbEndpoints}
}

func outboundTunnelListenerName(sa string) string {
	return "outbound_tunnel_lis_" + sa
}

// outboundTunnelListener is built for each ServiceAccount from pods on the node.
// This listener adds the original destination headers from the dynamic EDS metadata pass through.
// We build the listener per-service account so that it can point to the corresponding cluster that presents the correct cert.
func outboundTunnelListener(name string, sa string) *discovery.Resource {
	l := &listener.Listener{
		Name:              name,
		UseOriginalDst:    wrappers.Bool(false),
		ListenerSpecifier: &listener.Listener_InternalListener{InternalListener: &listener.Listener_InternalListenerConfig{}},
		Address:           internalAddress(name),
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
	// TODO san match
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
		Name:                 outboundTunnelClusterName(sa),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:       durationpb.New(2 * time.Second),
		CleanupInterval:      durationpb.New(60 * time.Second),
		LbConfig: &cluster.Cluster_OriginalDstLbConfig_{
			OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{UseHttpHeader: true},
		},
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		TransportSocket: &core.TransportSocket{
			Name: "envoy.transport_sockets.tls",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: buildCommonTLSContext(proxy, workload, push, false),
			})},
		},
	}
}

// outboundTunnelCluster is per-workload SA, but requires one workload that uses that SA so we can send the Pod UID
func outboundPodTunnelCluster(proxy *model.Proxy, push *model.PushContext, sa string, workload *ambient.Workload) *cluster.Cluster {
	return &cluster.Cluster{
		Name:                 outboundPodTunnelClusterName(sa),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:       durationpb.New(2 * time.Second),
		CleanupInterval:      durationpb.New(60 * time.Second),
		LbConfig: &cluster.Cluster_OriginalDstLbConfig_{
			OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{
				UpstreamPortOverride: UproxyInboundCapturePort,
			},
		},
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		TransportSocket: &core.TransportSocket{
			Name: "envoy.transport_sockets.tls",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: buildCommonTLSContext(proxy, workload, push, false),
			})},
		},
	}
}

// outboundTunnelCluster is per-workload SA, but requires one workload that uses that SA so we can send the Pod UID
func outboundPodLocalTunnelCluster(proxy *model.Proxy, push *model.PushContext, sa string, workload *ambient.Workload) *cluster.Cluster {
	return &cluster.Cluster{
		Name:                 outboundPodLocalTunnelClusterName(sa),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:             cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:       durationpb.New(2 * time.Second),
		CleanupInterval:      durationpb.New(60 * time.Second),
		LbConfig: &cluster.Cluster_OriginalDstLbConfig_{
			OriginalDstLbConfig: &cluster.Cluster_OriginalDstLbConfig{
				UseHttpHeader:        true,
				UpstreamPortOverride: UproxyInboundNodeLocalCapturePort,
			},
		},
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

func outboundPodTunnelClusterName(sa string) string {
	return "outbound_pod_tunnel_clus_" + sa
}

func outboundPodLocalTunnelClusterName(sa string) string {
	return "outbound_pod_local_tunnel_clus_" + sa
}

// buildInboundCaptureListener creates a single listener with a FilterChain for each pod on the node.
func (g *UProxyConfigGenerator) buildInboundCaptureListener(proxy *model.Proxy, push *model.PushContext) *discovery.Resource {
	// TODO L7 stuff (deny at l4 for l7 auth if there is a remote proxy for the dest workload)

	l := &listener.Listener{
		Name:           "uproxy_inbound",
		UseOriginalDst: wrappers.Bool(true),
		ListenerFilters: []*listener.ListenerFilter{{
			Name: wellknown.OriginalDestination,
			ConfigType: &listener.ListenerFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&originaldst.OriginalDst{}),
			},
		}},
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
	if features.SidecarlessCapture == model.VariantIptables {
		l.ListenerFilters = append(l.ListenerFilters, &listener.ListenerFilter{
			Name: wellknown.OriginalSource,
			ConfigType: &listener.ListenerFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{
					Mark: OriginalSrcMark,
				}),
			},
		})
		l.Transparent = wrappers.Bool(true)
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
													// TODO this cluster passes through arbitrary requests; including unauthenticated destinations.
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
	// TODO cases where we passthrough
	l.FilterChains = append(l.FilterChains, blackholeFilterChain())

	return &discovery.Resource{
		Name:     l.Name,
		Resource: util.MessageToAny(l),
	}
}

// buildInboundCaptureListener creates a single listener with a FilterChain for each pod on the node.
func (g *UProxyConfigGenerator) buildInboundPlaintextCaptureListener(proxy *model.Proxy, push *model.PushContext) *discovery.Resource {
	// TODO L7 stuff (deny at l4 for l7 auth if there is a remote proxy for the dest workload)
	l := &listener.Listener{
		Name:           "uproxy_inbound_plaintext",
		UseOriginalDst: wrappers.Bool(true),
		ListenerFilters: []*listener.ListenerFilter{{
			Name: wellknown.OriginalDestination,
			ConfigType: &listener.ListenerFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&originaldst.OriginalDst{}),
			},
		}},
		AccessLog: accessLogString("capture inbound listener plaintext"),
		Address: &core.Address{Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				// TODO because of the port 15088 workaround, we need to use a redirect rule,
				// which means we can't bind to localhost. once we remove that workaround,
				// this can be changed back to 127.0.0.1
				Address: "0.0.0.0",
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: UproxyInbound2CapturePort,
				},
			},
		}},
	}
	if features.SidecarlessCapture == model.VariantIptables {
		l.ListenerFilters = append(l.ListenerFilters, &listener.ListenerFilter{
			Name: wellknown.OriginalSource,
			ConfigType: &listener.ListenerFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&originalsrc.OriginalSrc{
					Mark: OriginalSrcMark,
				}),
			},
		})
		l.Transparent = wrappers.Bool(true)
	}

	for _, workload := range push.SidecarlessIndex.Workloads.NodeLocal(proxy.Metadata.NodeName) {
		// TODO apply RBAC, etc
		l.FilterChains = append(l.FilterChains, &listener.FilterChain{
			Name:             "inbound_" + workload.Status.PodIP,
			FilterChainMatch: &listener.FilterChainMatch{PrefixRanges: matchIP(workload.Status.PodIP)},
			Filters: []*listener.Filter{{
				Name: wellknown.TCPProxy,
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: util.MessageToAny(&tcp.TcpProxy{
						StatPrefix:       util.BlackHoleCluster,
						ClusterSpecifier: &tcp.TcpProxy_Cluster{Cluster: "virtual_inbound"},
					}),
				},
			}},
		})
	}
	// TODO cases where we passthrough
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

const EnvoyTextLogFormat = "[%START_TIME%] \"%REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% " +
	"%PROTOCOL%\" %RESPONSE_CODE% %RESPONSE_FLAGS% " +
	"%RESPONSE_CODE_DETAILS% %CONNECTION_TERMINATION_DETAILS% " +
	"\"%UPSTREAM_TRANSPORT_FAILURE_REASON%\" %BYTES_RECEIVED% %BYTES_SENT% " +
	"%DURATION% %RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)% \"%REQ(X-FORWARDED-FOR)%\" " +
	"\"%REQ(USER-AGENT)%\" \"%REQ(X-REQUEST-ID)%\" \"%REQ(:AUTHORITY)%\" \"%UPSTREAM_HOST%\" " +
	"%UPSTREAM_CLUSTER% %UPSTREAM_LOCAL_ADDRESS% %DOWNSTREAM_LOCAL_ADDRESS% " +
	"%DOWNSTREAM_REMOTE_ADDRESS% %REQUESTED_SERVER_NAME% %ROUTE_NAME% "

func accessLogString(prefix string) []*accesslog.AccessLog {
	inlineString := EnvoyTextLogFormat + prefix + "\n"
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
