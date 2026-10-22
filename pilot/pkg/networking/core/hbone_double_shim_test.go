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
	"fmt"
	"testing"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	sfs "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/set_filter_state/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	internal "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/internal_upstream/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

const doubleShimGateway = `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: ingress
  namespace: default
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts: ["*"]
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: app
  namespace: default
spec:
  hosts: ["*"]
  gateways: [ingress]
  http:
  - route:
    - destination:
        host: app.default.svc.cluster.local
        port:
          number: 80
`

func doubleShimProxy(cg *ConfigGenTest, kind model.NodeType, enabled bool) *model.Proxy {
	controller := constants.ManagedGatewayMeshControllerLabel
	if kind == model.Router {
		controller = constants.ManagedGatewayControllerLabel
	}
	return cg.SetupProxy(&model.Proxy{
		Type:   kind,
		Labels: map[string]string{label.GatewayManaged.Name: controller, "istio": "ingressgateway"},
		Metadata: &model.NodeMetadata{
			Network: "network1", EnableHBONEConnectWait: model.StringBool(enabled),
		},
	})
}

func enableDoubleShimTest(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbient, true)
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)
	test.SetForTest(t, &features.EnableAmbientWaypointMultiNetwork, true)
	test.SetForTest(t, &features.EnableAmbientIngressMultiNetwork, true)
	test.SetForTest(t, &features.FilterGatewayClusterConfig, false)
}

func TestHBONEDoubleShimListeners(t *testing.T) {
	enableDoubleShimTest(t)
	for _, kind := range []model.NodeType{model.Waypoint, model.Router} {
		for _, baggage := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/baggage=%t", kind, baggage), func(t *testing.T) {
				test.SetForTest(t, &features.EnableAmbientBaggage, baggage)
				cg := NewConfigGenTest(t, TestOptions{ConfigString: doubleShimGateway})
				for _, enabled := range []bool{false, true} {
					proxy := doubleShimProxy(cg, kind, enabled)
					listeners := cg.Listeners(proxy)
					inner := xdstest.ExtractListener(DoubleHBONEInnerConnectOriginate, listeners)
					outer := xdstest.ExtractListener(DoubleHBONEOuterConnectOriginate, listeners)
					shim := xdstest.ExtractListener(util.HBONEInnerOriginationShimListener, listeners)
					if inner == nil || outer == nil {
						t.Fatal("missing native nested listeners")
					}
					assert.Equal(t, shim != nil, enabled)
					assert.Equal(t, xdstest.ExtractListener(util.HBONEOuterOriginationShimListener, listeners) != nil, enabled)
					assert.Equal(t, xdstest.ExtractListener(util.HBONEOriginationShimListener, listeners) != nil, enabled)
					assert.Equal(t, outer.FilterChains[0].Filters[0].Name, wellknown.TCPProxy)
					if !enabled {
						continue
					}
					// Unwrapping the shim must reproduce the entire native listener.
					unwrapped := protomarshal.Clone(shim)
					unwrapped.Name = inner.Name
					terminal := unwrapped.FilterChains[0].Filters[len(unwrapped.FilterChains[0].Filters)-1]
					wrapped := &udpa.TypedStruct{}
					assert.Equal(t, terminal.Name, util.HBONEOriginationShimFilter)
					assert.NoError(t, terminal.GetTypedConfig().UnmarshalTo(wrapped))
					assert.Equal(t, wrapped.TypeUrl, util.HBONEOriginationShimConfigType)
					tcpProxy := &tcp.TcpProxy{}
					assert.NoError(t, protomarshal.StructToMessageSlow(wrapped.Value.Fields["tcp_proxy"].GetStructValue(), tcpProxy))
					terminal.Name = wellknown.TCPProxy
					terminal.ConfigType = &listener.Filter_TypedConfig{TypedConfig: protoconv.MessageToAny(tcpProxy)}
					assert.Equal(t, unwrapped, inner)
					assert.Equal(t, tcpProxy.GetCluster(), DoubleHBONEInnerConnectOriginate)
					assert.Equal(t, tcpProxy.TunnelingConfig.Hostname, "%FILTER_STATE(istio.double_hbone.hbone_target_address:PLAIN)%")
					assert.Equal(t, tcpProxy.TunnelingConfig.HeadersToAdd[0].Header.Value, "network1")
					assert.Equal(t, tcpProxy.TunnelingConfig.PropagateResponseHeaders, baggage)
					state := &sfs.Config{}
					assert.NoError(t, inner.FilterChains[0].Filters[0].GetTypedConfig().UnmarshalTo(state))
					assert.Equal(t, len(state.OnNewConnection), 2)
					assert.Equal(t, state.OnNewConnection[0].GetObjectKey(), "envoy.filters.listener.original_dst.local_ip")
					assert.Equal(t, state.OnNewConnection[1].GetObjectKey(), "istio.double_hbone.hbone_target_address")
				}
			})
		}
	}
}

// Exercise remote gateway synthesis after endpoint generation, together with the
// service transport and native nested resources. Local and remote endpoints must
// share the same transport but enter different listeners.
func TestHBONEDoubleShimPairing(t *testing.T) {
	enableDoubleShimTest(t)
	for _, kind := range []model.NodeType{model.Waypoint, model.Router} {
		for _, appProtocol := range []protocol.Instance{protocol.HTTP, protocol.HTTP2, protocol.TCP} {
			for _, tt := range []struct {
				name   string
				policy *networking.TrafficPolicy
				shim   bool
			}{
				{name: "raw", shim: true},
				{name: "mesh mtls", shim: true, policy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL}}},
				{name: "application tls", policy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE}}},
				{name: "proxy protocol", policy: &networking.TrafficPolicy{ProxyProtocol: &networking.TrafficPolicy_ProxyProtocol{}}},
				{name: "port application tls", policy: &networking.TrafficPolicy{PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{{
					Port: &networking.PortSelector{Number: 80}, Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE},
				}}}},
			} {
				t.Run(fmt.Sprintf("%s/%s/%s", kind, appProtocol, tt.name), func(t *testing.T) {
					port := &model.Port{Name: "app", Port: 80, Protocol: appProtocol}
					svc := &model.Service{
						Hostname: "app.default.svc.cluster.local", DefaultAddress: "10.0.0.1",
						Ports: model.PortList{port}, Resolution: model.ClientSideLB,
						Attributes: model.ServiceAttributes{Name: "app", Namespace: "default"},
					}
					dr := config.Config{
						Meta: config.Meta{GroupVersionKind: gvk.DestinationRule, Name: "app", Namespace: "default"},
						Spec: &networking.DestinationRule{Host: string(svc.Hostname), TrafficPolicy: tt.policy},
					}
					var instances []*model.ServiceInstance
					for i, nw := range []network.ID{"network1", "network2", "network2"} {
						ep := &model.IstioEndpoint{
							Addresses: []string{fmt.Sprintf("10.0.0.%d", i+2)}, EndpointPort: 8080,
							ServicePortName: port.Name, Namespace: "default", CapturedByZtunnel: true,
						}
						ep.Network = nw
						instances = append(instances, &model.ServiceInstance{Service: svc, ServicePort: port, Endpoint: ep})
					}
					cg := NewConfigGenTest(t, TestOptions{
						Services: []*model.Service{svc}, Instances: instances,
						ConfigString: doubleShimGateway, Configs: []config.Config{dr}, Gateways: []model.NetworkGateway{
							{Network: "network2", Addr: "192.0.2.1", HBONEPort: 15008},
							{Network: "network2", Addr: "192.0.2.2", HBONEPort: 16008},
						},
					})
					var stockCDS, stockEDS any
					var stockService, stockOuter *cluster.Cluster
					for _, enabled := range []bool{false, true} {
						proxy := doubleShimProxy(cg, kind, enabled)
						cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, model.NewXdsCache())
						direction, subset := model.TrafficDirectionOutbound, ""
						var c *cluster.Cluster
						if kind == model.Waypoint {
							direction, subset = model.TrafficDirectionInboundVIP, "http"
							if appProtocol == protocol.TCP {
								subset = "tcp"
							}
							policy := util.MergeSubsetTrafficPolicy(tt.policy, nil, port)
							c = cb.buildWaypointInboundVIPCluster(proxy, svc, *port, subset, cg.PushContext().Mesh, policy, &dr)
						} else {
							c = xdstest.ExtractClusters(cg.Clusters(proxy))[model.BuildSubsetKey(direction, subset, svc.Hostname, port.Port)]
						}
						if c == nil {
							t.Fatal("missing service cluster")
						}
						var transport *core.TransportSocket
						if kind == model.Waypoint {
							transport = c.TransportSocket
						} else {
							for _, match := range c.TransportSocketMatches {
								if match.Name == "hbone" {
									transport = match.TransportSocket
								}
							}
						}
						if transport == nil {
							t.Fatal("missing HBONE transport")
						}
						wantShim := enabled && tt.shim
						assert.Equal(t, transport.Name == util.HBONEOriginationShimTransport, wantShim)
						assert.Equal(t, c.GetUpstreamConfig().GetName() == util.HBONERejectionPool, wantShim)
						if !enabled {
							stockService = c
						}
						assert.Equal(t, c.LoadBalancingPolicy, stockService.LoadBalancingPolicy)
						assert.Equal(t, c.LbPolicy, stockService.LbPolicy)
						name := model.BuildSubsetKey(direction, subset, svc.Hostname, port.Port)
						eb := endpoints.NewEndpointBuilder(name, proxy, cg.PushContext())
						cache := buildClusterKey(svc, port, cb, proxy, nil)
						cdsKey := cache.Key()
						if !enabled {
							stockCDS, stockEDS = cdsKey, eb.Key()
						}
						if wantShim && (cdsKey == stockCDS || eb.Key() == stockEDS) {
							t.Fatal("shim and stock share a cache key")
						}
						localities := eb.FromServiceEndpoints()
						assert.Equal(t, len(localities), 1)
						if len(localities) != 1 {
							return
						}
						assert.Equal(t, len(localities[0].LbEndpoints), 3)
						// Two remote pods are aggregated into two gateways. Scale=2 gives the
						// local pod and each gateway weight 2, preserving total weight 6.
						assert.Equal(t, localities[0].GetLoadBalancingWeight().GetValue(), uint32(6))
						for _, ep := range localities[0].LbEndpoints {
							addr := ep.GetEndpoint().Address.GetEnvoyInternalAddress()
							if addr == nil {
								t.Fatal("expected HBONE internal endpoint")
							}
							local := addr.EndpointId == "10.0.0.2:8080"
							wantListener := DoubleHBONEInnerConnectOriginate
							if local {
								wantListener = ConnectOriginate
							}
							if wantShim {
								wantListener = util.HBONEInnerOriginationShimListener
								if local {
									wantListener = util.HBONEOriginationShimListener
								}
							}
							assert.Equal(t, addr.GetServerListenerName(), wantListener)
							assert.Equal(t, ep.GetLoadBalancingWeight().GetValue(), uint32(2))
							if !local {
								assert.Equal(t, ep.Metadata.FilterMetadata[util.OriginalDstMetadataKey].Fields["local"].GetStringValue(), addr.EndpointId)
								target := ep.Metadata.FilterMetadata[util.IstioMetadataKey].Fields["double_hbone"].GetStructValue().Fields["hbone_target_address"].GetStringValue()
								assert.Equal(t, target, "app.default.svc.cluster.local:80")
							}
							if kind == model.Router {
								assert.Equal(t, ep.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey].Fields[model.TunnelLabelShortName].GetStringValue(), model.TunnelHTTP)
							}
						}
						clusters := xdstest.ExtractClusters(cg.Clusters(proxy))
						inner, outer := clusters[DoubleHBONEInnerConnectOriginate], clusters[DoubleHBONEOuterConnectOriginate]
						if inner == nil || outer == nil {
							t.Fatal("missing nested H2 clusters")
						}
						innerEndpoint := inner.LoadAssignment.Endpoints[0].LbEndpoints[0].GetEndpoint().Address.GetEnvoyInternalAddress()
						var innerTLS *core.TransportSocket
						if enabled {
							assert.Equal(t, innerEndpoint.GetServerListenerName(), util.HBONEOuterOriginationShimListener)
							assert.Equal(t, inner.TransportSocket.Name, util.HBONEOriginationShimTransport)
							wrapped := &udpa.TypedStruct{}
							assert.NoError(t, inner.TransportSocket.GetTypedConfig().UnmarshalTo(wrapped))
							innerTLS = &core.TransportSocket{}
							assert.NoError(t, protomarshal.StructToMessageSlow(wrapped.Value.Fields["transport_socket"].GetStructValue(), innerTLS))
						} else {
							assert.Equal(t, innerEndpoint.GetServerListenerName(), DoubleHBONEOuterConnectOriginate)
							socket := &internal.InternalUpstreamTransport{}
							assert.NoError(t, inner.TransportSocket.GetTypedConfig().UnmarshalTo(socket))
							innerTLS = socket.TransportSocket
						}
						assert.Equal(t, innerTLS.Name, "tls")
						assert.Equal(t, outer.TransportSocket.Name, "tls")
						assert.Equal(t, outer.GetOriginalDstLbConfig().GetUpstreamPortOverride().GetValue(), uint32(model.HBoneInboundListenPort))
						if !enabled {
							stockOuter = outer
						}
						assert.Equal(t, outer, stockOuter)
						for _, nested := range []*cluster.Cluster{inner, outer} {
							assert.Equal(t, nested.UpstreamConfig == nil, true)
							assert.Equal(t, len(nested.TypedExtensionProtocolOptions), 1)
							options := &http.HttpProtocolOptions{}
							assert.NoError(t, nested.TypedExtensionProtocolOptions["envoy.extensions.upstreams.http.v3.HttpProtocolOptions"].UnmarshalTo(options))
							assert.Equal(t, options.CommonHttpProtocolOptions.GetMaxRequestsPerConnection().GetValue(), uint32(1))
						}
					}
				})
			}
		}
	}
}
