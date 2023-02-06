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
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
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

func (configgen *ConfigGeneratorImpl) buildWaypointInboundClusters(cb *ClusterBuilder, proxy *model.Proxy, push *model.PushContext) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	wls, svcs := FindAssociatedResources(proxy, push)

	// We create 4 types of clusters:
	// 1. `inbound-vip|internal|hostname|port`. Will send to internal listener of the same name.
	// 2. `inbound-vip|protocol|hostname|port`. EDS routing to the internal listener for each pod in the VIP.
	// 3. `inbound-pod||podip|port`. Points to inbound_CONNECT_originate with tunnel metadata set to hit the pod
	// 4. inbound_CONNECT_originate. original dst with TLS added

	clusters = append(clusters, cb.buildWaypointInboundVIPInternal(svcs)...)
	clusters = append(clusters, cb.buildWaypointInboundVIP(svcs)...)
	clusters = append(clusters, cb.buildWaypointInboundPod(wls, configgen.Discovery)...)
	clusters = append(clusters, cb.buildWaypointInboundConnect(proxy, push))

	for _, c := range clusters {
		if c.TransportSocket != nil && c.TransportSocketMatches != nil {
			log.Errorf("invalid cluster, multiple matches: %v", c.Name)
		}
	}
	return clusters
}

func (cb *ClusterBuilder) buildWaypointInboundPodCluster(wl ambient.Workload, port model.Port) *MutableCluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundPod, "", host.Name(wl.PodIP), port.Port)
	address := wl.PodIP
	tunnelPort := 15008
	// We will connect to inbound_CONNECT_originate internal listener, telling it to tunnel to ip:15008,
	// and add some detunnel metadata that had the original port.
	tunnelOrigLis := "inbound_CONNECT_originate"
	llb := util.BuildInternalEndpoint(tunnelOrigLis, util.BuildTunnelMetadata(address, port.Port, tunnelPort))
	clusterType := cluster.Cluster_STATIC
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, llb,
		model.TrafficDirectionInbound, &port, nil, nil)

	// Apply internal_upstream, since we need to pass our the pod dest address in the metadata
	localCluster.cluster.TransportSocketMatches = nil
	localCluster.cluster.TransportSocket = InternalUpstreamSocketMatch[0].TransportSocket
	return localCluster
}

// Cluster to forward to the inbound-pod listener. This is similar to the inbound-vip internal cluster, but has a single endpoint.
// TODO: in the future maybe we could share the VIP cluster and just pre-select the IP.
func (cb *ClusterBuilder) buildWaypointInboundInternalPodCluster(wl ambient.Workload, port model.Port) *MutableCluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundPod, "internal", host.Name(wl.PodIP), port.Port)
	destName := model.BuildSubsetKey(model.TrafficDirectionInboundPod, "", host.Name(wl.PodIP), port.Port)
	// We will connect to inbound_CONNECT_originate internal listener, telling it to tunnel to ip:15008,
	// and add some detunnel metadata that had the original port.
	llb := util.BuildInternalEndpoint(destName, nil)
	clusterType := cluster.Cluster_STATIC
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, llb,
		model.TrafficDirectionInbound, &port, nil, nil)
	// Apply internal_upstream, since we need to pass our the pod dest address in the metadata
	localCluster.cluster.TransportSocketMatches = nil
	localCluster.cluster.TransportSocket = InternalUpstreamSocketMatch[0].TransportSocket
	return localCluster
}

// `inbound-vip|internal|hostname|port`. Will send to internal listener of the same name (without internal subset)
func (cb *ClusterBuilder) buildWaypointInboundVIPInternalCluster(svc *model.Service, port model.Port) *MutableCluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "internal", svc.Hostname, port.Port)
	destinationName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "", svc.Hostname, port.Port)

	clusterType := cluster.Cluster_STATIC
	llb := util.BuildInternalEndpoint(destinationName, nil)
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, llb,
		model.TrafficDirectionInbound, &port, nil, nil)
	// no TLS
	localCluster.cluster.TransportSocketMatches = nil
	localCluster.cluster.TransportSocket = BaggagePassthroughTransportSocket
	return localCluster
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

// `inbound-vip|internal|hostname|port`. Will send to internal listener of the same name.
func (cb *ClusterBuilder) buildWaypointInboundVIPInternal(svcs map[host.Name]*model.Service) []*cluster.Cluster {
	clusters := []*cluster.Cluster{}
	for _, svc := range svcs {
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			clusters = append(clusters, cb.buildWaypointInboundVIPInternalCluster(svc, *port).build())
		}
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

// `inbound-pod||podip|port`. Points to inbound_CONNECT_originate with tunnel metadata set to hit the pod
func (cb *ClusterBuilder) buildWaypointInboundPod(wls []WorkloadAndServices, discovery model.ServiceDiscovery) []*cluster.Cluster {
	clusters := []*cluster.Cluster{}
	for _, wlx := range wls {
		wl := wlx.WorkloadInfo
		instances := discovery.GetProxyServiceInstances(&model.Proxy{
			Type:            model.SidecarProxy,
			IPAddresses:     []string{wl.PodIP},
			ConfigNamespace: wl.Namespace,
			Metadata: &model.NodeMetadata{
				Namespace: wl.Namespace,
				Labels:    wl.Labels,
			},
		})
		for _, port := range getPorts(instances) {
			if port.Protocol == protocol.UDP {
				continue
			}
			clusters = append(clusters,
				cb.buildWaypointInboundPodCluster(wl, port).build(),
				cb.buildWaypointInboundInternalPodCluster(wl, port).build())
		}
	}
	return clusters
}

// inbound_CONNECT_originate. original dst with TLS added
func (cb *ClusterBuilder) buildWaypointInboundConnect(proxy *model.Proxy, push *model.PushContext) *cluster.Cluster {
	ctx := &tls.CommonTlsContext{}
	security.ApplyToCommonTLSContext(ctx, proxy, nil, authn.TrustDomainsForValidation(push.Mesh), true)

	ctx.AlpnProtocols = []string{"h2"}

	ctx.TlsParams = &tls.TlsParameters{
		// Ensure TLS 1.3 is used everywhere
		TlsMaximumProtocolVersion: tls.TlsParameters_TLSv1_3,
		TlsMinimumProtocolVersion: tls.TlsParameters_TLSv1_3,
	}
	return &cluster.Cluster{
		Name:                          "inbound_CONNECT_originate",
		ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:                      cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:                durationpb.New(2 * time.Second),
		CleanupInterval:               durationpb.New(60 * time.Second),
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		TransportSocket: &core.TransportSocket{
			Name: "envoy.transport_sockets.tls",
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
