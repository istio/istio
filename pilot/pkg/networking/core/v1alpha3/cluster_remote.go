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
	"net"
	"strconv"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	internalupstream "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/internal_upstream/v3"
	rawbuffer "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/metadata/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/networking/util"
	security "istio.io/istio/pilot/pkg/security/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/pkg/log"
)

func (configgen *ConfigGeneratorImpl) buildRemoteInboundClusters(cb *ClusterBuilder, proxy *model.Proxy, push *model.PushContext) []*cluster.Cluster {
	clusters := make([]*cluster.Cluster, 0)
	wls, svcs := FindAssociatedResources(proxy, push)

	// We create 4 types of clusters:
	// 1. `inbound-vip|internal|hostname|port`. Will send to internal listener of the same name.
	// 2. `inbound-vip|protocol|hostname|port`. EDS routing to the internal listener for each pod in the VIP.
	// 3. `inbound-pod||podip|port`. Points to inbound_CONNECT_originate with tunnel metadata set to hit the pod
	// 4. inbound_CONNECT_originate. original dst with TLS added

	clusters = append(clusters, cb.buildRemoteInboundVIPInternal(svcs)...)
	clusters = append(clusters, cb.buildRemoteInboundVIP(svcs)...)
	clusters = append(clusters, cb.buildRemoteInboundPod(wls, configgen.Discovery)...)
	clusters = append(clusters, cb.buildRemoteInboundConnect(proxy, push))

	for _, c := range clusters {
		if c.TransportSocket != nil && c.TransportSocketMatches != nil {
			log.Errorf("invalid cluster, multiple matches: %v", c.Name)
		}
	}
	return clusters
}

func (cb *ClusterBuilder) buildRemoteInboundPodCluster(wl ambient.Workload, port model.Port) *MutableCluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundPod, "", host.Name(wl.Status.PodIP), port.Port)
	address := wl.Status.PodIP
	tunnelPort := 15008
	// We will connect to inbound_CONNECT_originate internal listener, telling it to tunnel to ip:15008,
	// and add some detunnel metadata that had the original port.
	llb := buildInternalEndpoint("inbound_CONNECT_originate", util.BuildTunnelMetadata(map[string]interface{}{
		"target":           "inbound_CONNECT_originate",
		"tunnel_address":   net.JoinHostPort(address, strconv.Itoa(tunnelPort)),
		"detunnel_address": net.JoinHostPort(address, strconv.Itoa(port.Port)),
		"detunnel_ip":      address,
		"detunnel_port":    strconv.Itoa(port.Port),
	}))
	if wl.Spec.NodeName == cb.proxy.Metadata.NodeName && features.SidecarlessCapture == model.VariantBpf {
		// TODO: On BPF mode, we currently do not get redirect for same node Remote -> Pod
		// Instead, just go direct
		llb = buildEndpoint(address, port.Port)
	}
	clusterType := cluster.Cluster_STATIC
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, llb,
		model.TrafficDirectionInbound, &port, nil, nil)
	//opts := buildClusterOpts{
	//	mesh:             cb.req.Push.Mesh,
	//	mutable:          localCluster,
	//	policy:           nil,
	//	port:             &port,
	//	serviceAccounts:  nil,
	//	serviceInstances: cb.serviceInstances,
	//	istioMtlsSni:     "",
	//	clusterMode:      DefaultClusterMode,
	//	direction:        model.TrafficDirectionInbound,
	//}
	// TODO get destination rule
	//cb.applyTrafficPolicy(opts)

	// Apply internal_upstream, since we need to pass our the pod dest address in the metadata
	localCluster.cluster.TransportSocketMatches = nil
	localCluster.cluster.TransportSocket = InternalUpstreamSocketMatch[0].TransportSocket
	if wl.Spec.NodeName == cb.proxy.Metadata.NodeName && features.SidecarlessCapture == model.VariantBpf {
		// TODO: On BPF mode, we currently do not get redirect for same node Remote -> Pod
		localCluster.cluster.TransportSocket = nil
	}
	return localCluster
}

// `inbound-vip|internal|hostname|port`. Will send to internal listener of the same name (without internal subset)
func (cb *ClusterBuilder) buildRemoteInboundVIPInternalCluster(svc *model.Service, port model.Port) *MutableCluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "internal", svc.Hostname, port.Port)
	destinationName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, "", svc.Hostname, port.Port)

	clusterType := cluster.Cluster_STATIC
	llb := buildInternalEndpoint(destinationName, nil)
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, llb,
		model.TrafficDirectionInbound, &port, nil, nil)
	// no TLS
	localCluster.cluster.TransportSocketMatches = nil
	localCluster.cluster.TransportSocket = nil
	return localCluster
}

// `inbound-vip||hostname|port`. EDS routing to the internal listener for each pod in the VIP.
func (cb *ClusterBuilder) buildRemoteInboundVIPCluster(svc *model.Service, port model.Port, subset string) *MutableCluster {
	clusterName := model.BuildSubsetKey(model.TrafficDirectionInboundVIP, subset, svc.Hostname, port.Port)

	clusterType := cluster.Cluster_EDS
	localCluster := cb.buildDefaultCluster(clusterName, clusterType, nil,
		model.TrafficDirectionInbound, &port, nil, nil)
	//opts := buildClusterOpts{
	//	mesh:             cb.req.Push.Mesh,
	//	mutable:          localCluster,
	//	policy:           nil,
	//	port:             &port,
	//	serviceAccounts:  nil,
	//	serviceInstances: cb.serviceInstances,
	//	istioMtlsSni:     "",
	//	clusterMode:      DefaultClusterMode,
	//	direction:        model.TrafficDirectionInbound,
	//}
	// TODO should we apply connection pool here or at pod level?
	//cb.applyTrafficPolicy(opts)

	// no transport, we are just going to internal address
	localCluster.cluster.TransportSocket = nil
	localCluster.cluster.TransportSocketMatches = nil
	maybeApplyEdsConfig(localCluster.cluster)
	return localCluster
}

var InternalUpstreamSocketMatch = []*cluster.Cluster_TransportSocketMatch{
	{
		Name: "internal_upstream",
		Match: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				ambient.TransportMatchKey: {Kind: &structpb.Value_StringValue{StringValue: ambient.TransportMatchValue}},
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
	},
	defaultTransportSocketMatch(),
}

// `inbound-vip|internal|hostname|port`. Will send to internal listener of the same name.
func (cb *ClusterBuilder) buildRemoteInboundVIPInternal(svcs map[host.Name]*model.Service) []*cluster.Cluster {
	clusters := []*cluster.Cluster{}
	for _, svc := range svcs {
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			clusters = append(clusters, cb.buildRemoteInboundVIPInternalCluster(svc, *port).build())
		}
	}
	return clusters
}

// `inbound-vip|protocol|hostname|port`. EDS routing to the internal listener for each pod in the VIP.
func (cb *ClusterBuilder) buildRemoteInboundVIP(svcs map[host.Name]*model.Service) []*cluster.Cluster {
	clusters := []*cluster.Cluster{}

	for _, svc := range svcs {
		for _, port := range svc.Ports {
			if port.Protocol == protocol.UDP {
				continue
			}
			if port.Protocol.IsUnsupported() || port.Protocol.IsTCP() {
				clusters = append(clusters, cb.buildRemoteInboundVIPCluster(svc, *port, "tcp").build())
			}
			if port.Protocol.IsUnsupported() || port.Protocol.IsHTTP() {
				clusters = append(clusters, cb.buildRemoteInboundVIPCluster(svc, *port, "http").build())
			}
		}
	}
	return clusters
}

// `inbound-pod||podip|port`. Points to inbound_CONNECT_originate with tunnel metadata set to hit the pod
func (cb *ClusterBuilder) buildRemoteInboundPod(wls []WorkloadAndServices, discovery model.ServiceDiscovery) []*cluster.Cluster {
	clusters := []*cluster.Cluster{}
	for _, wlx := range wls {
		wl := wlx.WorkloadInfo
		instances := discovery.GetProxyServiceInstances(&model.Proxy{
			Type:            model.SidecarProxy,
			IPAddresses:     []string{wl.Status.PodIP},
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
			clusters = append(clusters, cb.buildRemoteInboundPodCluster(wl, port).build())
		}
	}
	return clusters
}

// inbound_CONNECT_originate. original dst with TLS added
func (cb *ClusterBuilder) buildRemoteInboundConnect(proxy *model.Proxy, push *model.PushContext) *cluster.Cluster {
	ctx := &tls.CommonTlsContext{}
	security.ApplyToCommonTLSContext(ctx, proxy, nil, authn.TrustDomainsForValidation(push.Mesh), true)

	return &cluster.Cluster{
		Name:                          "inbound_CONNECT_originate",
		ClusterDiscoveryType:          &cluster.Cluster_Type{Type: cluster.Cluster_ORIGINAL_DST},
		LbPolicy:                      cluster.Cluster_CLUSTER_PROVIDED,
		ConnectTimeout:                durationpb.New(2 * time.Second),
		CleanupInterval:               durationpb.New(60 * time.Second),
		TypedExtensionProtocolOptions: h2connectUpgrade(),
		TransportSocket: &core.TransportSocket{
			Name: "envoy.transport_sockets.tls",
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: util.MessageToAny(&tls.UpstreamTlsContext{
				CommonTlsContext: ctx,
			})},
		},
	}
}

func h2connectUpgrade() map[string]*anypb.Any {
	return map[string]*anypb.Any{
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

func buildInternalEndpoint(dest string, meta *core.Metadata) []*endpoint.LocalityLbEndpoints {
	lbEndpoints := []*endpoint.LbEndpoint{}
	address := util.BuildInternalAddress(dest)

	lb := &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: address,
			},
		},
		Metadata: meta,
	}

	lbEndpoints = append(lbEndpoints, lb)
	llb := []*endpoint.LocalityLbEndpoints{{
		LbEndpoints: lbEndpoints,
	}}
	return llb
}

func buildEndpoint(dest string, port int) []*endpoint.LocalityLbEndpoints {
	lbEndpoints := []*endpoint.LbEndpoint{}
	address := util.BuildAddress(dest, uint32(port))

	lb := &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: address,
			},
		},
	}

	lbEndpoints = append(lbEndpoints, lb)
	llb := []*endpoint.LocalityLbEndpoints{{
		LbEndpoints: lbEndpoints,
	}}
	return llb
}
