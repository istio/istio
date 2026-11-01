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

// Downstream patch for https://github.com/istio/istio/issues/54921.
//
// Sidecars reach headless services through ORIGINAL_DST clusters, which can only send plaintext or legacy Istio
// mTLS. When a headless service has ambient pods, that traffic is rejected under STRICT. With
// PILOT_ENABLE_HEADLESS_HBONE_SEND, such a cluster becomes a single-endpoint cluster pointing at the
// connect_originate internal listener, and every headless TCP filter chain records the dialed pod address in the
// envoy.filters.listener.original_dst.local_ip filter state. The original_dst listener filter on connect_originate
// restores that address, so the tunnel is CONNECT <podIP:port> to <podIP>:15008, the same as the EDS HBONE path.

import (
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	sfsvalue "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/common/set_filter_state/v3"
	sfsnetwork "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/set_filter_state/v3"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/util/protomarshal"
)

// headlessHBONEOriginalDstFilter records the address the application dialed so that connect_originate can tunnel to
// it. It is harmless on chains whose cluster is still ORIGINAL_DST, since nothing reads the filter state there.
var headlessHBONEOriginalDstFilter = &listener.Filter{
	Name: "headless_hbone_original_dst",
	ConfigType: &listener.Filter_TypedConfig{
		TypedConfig: protoconv.MessageToAny(&sfsnetwork.Config{
			OnNewConnection: []*sfsvalue.FilterStateValue{{
				Key: &sfsvalue.FilterStateValue_ObjectKey{
					ObjectKey: "envoy.filters.listener.original_dst.local_ip",
				},
				Value: &sfsvalue.FilterStateValue_FormatString{
					FormatString: &core.SubstitutionFormatString{
						Format: &core.SubstitutionFormatString_TextFormatSource{
							TextFormatSource: &core.DataSource{
								Specifier: &core.DataSource_InlineString{
									InlineString: "%DOWNSTREAM_LOCAL_ADDRESS%",
								},
							},
						},
					},
				},
				SharedWithUpstream: sfsvalue.FilterStateValue_TRANSITIVE,
			}},
		}),
	},
}

func headlessHBONESendEnabled(proxy *model.Proxy) bool {
	return features.EnableHeadlessHBONESend && features.EnableHBONESend &&
		proxy != nil && proxy.Type == model.SidecarProxy &&
		(proxy.Metadata == nil || !bool(proxy.Metadata.DisableHBONESend))
}

func isHeadlessKubernetesService(service *model.Service) bool {
	return service != nil && service.Resolution == model.Passthrough &&
		service.Attributes.ServiceRegistry == provider.Kubernetes
}

// maybeAddHeadlessHBONEFilterState prepends headlessHBONEOriginalDstFilter to the TCP filter chains built for a
// headless Kubernetes service. It is added whether or not the cluster currently sends HBONE, so that CDS alone
// decides the transport and CDS/LDS ordering can never leave the tunnel without a destination.
func maybeAddHeadlessHBONEFilterState(opts outboundListenerOpts, chains []*filterChainOpts) {
	if !headlessHBONESendEnabled(opts.proxy) || !isHeadlessKubernetesService(opts.service) {
		return
	}
	for _, c := range chains {
		// Clone, as EnvoyFilter NETWORK_FILTER patches may modify filters in place.
		f := protomarshal.Clone(headlessHBONEOriginalDstFilter)
		c.networkFilters = append([]*listener.Filter{f}, c.networkFilters...)
	}
}

// shouldSendHeadlessHBONE reports whether the outbound cluster being built should be converted by
// applyHeadlessHBONE. userTLS is the DestinationRule TLS setting before auto mTLS is applied.
func (cb *ClusterBuilder) shouldSendHeadlessHBONE(service *model.Service, opts *buildClusterOpts,
	userTLS *networking.ClientTLSSettings, proxyProtocol *networking.TrafficPolicy_ProxyProtocol,
) bool {
	if !features.EnableHeadlessHBONESend || !features.EnableHBONESend || !cb.sidecarProxy() {
		return false
	}
	if cb.proxyMetadata != nil && bool(cb.proxyMetadata.DisableHBONESend) {
		return false
	}
	if !isHeadlessKubernetesService(service) || opts.clusterMode != DefaultClusterMode ||
		opts.direction != model.TrafficDirectionOutbound || opts.mutable.cluster.GetType() != cluster.Cluster_ORIGINAL_DST {
		return false
	}
	// Only TCP ports carry headlessHBONEOriginalDstFilter. HTTP and auto-detected ports are not handled.
	if opts.port == nil || !opts.port.Protocol.IsTCP() {
		return false
	}
	// The tunnel targets the dialed address, so a port override cannot be honored.
	if opts.mutable.cluster.GetOriginalDstLbConfig().GetUpstreamPortOverride() != nil {
		return false
	}
	// Respect an explicit user choice of DISABLE, SIMPLE or MUTUAL, and PROXY protocol.
	if (userTLS != nil && userTLS.Mode != networking.ClientTLSSettings_ISTIO_MUTUAL) || proxyProtocol != nil {
		return false
	}
	var proxyNetwork network.ID
	if cb.proxyMetadata != nil {
		proxyNetwork = cb.proxyMetadata.Network
	}
	return cb.req.Push.HeadlessServiceNeedsHBONE(service, opts.port, proxyNetwork)
}

// applyHeadlessHBONE turns an ORIGINAL_DST headless cluster into a single-endpoint cluster that tunnels through
// the connect_originate internal listener.
func applyHeadlessHBONE(c *cluster.Cluster) {
	c.ClusterDiscoveryType = &cluster.Cluster_Type{Type: cluster.Cluster_STATIC}
	c.LbPolicy = cluster.Cluster_ROUND_ROBIN
	c.LbConfig = nil
	c.LoadAssignment = &endpoint.ClusterLoadAssignment{
		ClusterName: c.Name,
		Endpoints:   util.BuildInternalEndpoint(ConnectOriginate, nil),
	}
	c.TransportSocket = hboneTransportSocket(xdsfilters.RawBufferTransportSocket).TransportSocket
	c.TransportSocketMatches = nil
	// There is a single internal endpoint; ejecting it would black-hole the whole service.
	c.OutlierDetection = nil
	im := getOrCreateIstioMetadata(c)
	im.Fields["disable_mx"] = &structpb.Value{Kind: &structpb.Value_BoolValue{BoolValue: true}}
}

// isHeadlessHBONECluster reports whether c was converted by applyHeadlessHBONE.
func isHeadlessHBONECluster(c *cluster.Cluster) bool {
	if c.GetType() != cluster.Cluster_STATIC || len(c.GetLoadAssignment().GetEndpoints()) != 1 {
		return false
	}
	lbe := c.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()
	return len(lbe) == 1 &&
		lbe[0].GetEndpoint().GetAddress().GetEnvoyInternalAddress().GetServerListenerName() == ConnectOriginate
}
