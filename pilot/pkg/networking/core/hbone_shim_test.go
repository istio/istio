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
	"testing"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	internalupstream "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/internal_upstream/v3"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func shimTestProxy(cg *ConfigGenTest, kind model.NodeType, enabled bool) *model.Proxy {
	return cg.SetupProxy(&model.Proxy{
		Type:     kind,
		Metadata: &model.NodeMetadata{EnableHBONEOriginationShim: model.StringBool(enabled)},
		Labels:   map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayMeshControllerLabel},
	})
}

func TestHBONEOriginationShimListener(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, false)
	test.SetForTest(t, &features.EnableAmbientIngressMultiNetwork, false)
	for _, baggage := range []bool{false, true} {
		test.SetForTest(t, &features.EnableAmbientBaggage, baggage)
		cg := NewConfigGenTest(t, TestOptions{})
		for _, kind := range []model.NodeType{model.Waypoint, model.Router} {
			for _, enabled := range []bool{false, true} {
				proxy := shimTestProxy(cg, kind, enabled)
				class := istionetworking.ListenerClassSidecarInbound
				if kind == model.Router {
					class = istionetworking.ListenerClassGateway
				}
				legacy := buildConnectOriginateListener(cg.PushContext(), proxy, class)
				found, foundLegacy := false, false
				for _, l := range cg.Listeners(proxy) {
					if l.Name == ConnectOriginate {
						foundLegacy = true
					}
					if l.Name != util.HBONEOriginationShimListener {
						continue
					}
					found = true
					assert.Equal(t, l.GetInternalListener(), legacy.GetInternalListener())
					assert.Equal(t, l.ListenerFilters, legacy.ListenerFilters)
					filters := l.FilterChains[0].Filters
					original := legacy.FilterChains[0].Filters
					assert.Equal(t, filters[:len(filters)-1], original[:len(original)-1])
					terminal := filters[len(filters)-1]
					assert.Equal(t, terminal.Name, util.HBONEOriginationShimFilter)
					wrapped := &udpa.TypedStruct{}
					assert.NoError(t, terminal.GetTypedConfig().UnmarshalTo(wrapped))
					assert.Equal(t, wrapped.TypeUrl, util.HBONEOriginationShimConfigType)
					tcpProxy := &tcp.TcpProxy{}
					assert.NoError(t, protomarshal.StructToMessageSlow(wrapped.Value.Fields["tcp_proxy"].GetStructValue(), tcpProxy))
					wantTCP := &tcp.TcpProxy{}
					assert.NoError(t, original[len(original)-1].GetTypedConfig().UnmarshalTo(wantTCP))
					assert.Equal(t, tcpProxy, wantTCP)
					assert.Equal(t, tcpProxy.GetCluster(), ConnectOriginate)
					assert.Equal(t, tcpProxy.GetTunnelingConfig().Hostname, "%DOWNSTREAM_LOCAL_ADDRESS%")
				}
				assert.Equal(t, found, enabled)
				assert.Equal(t, foundLegacy, true)
			}
		}
	}
}

// Exercise the actual CDS and EDS builders against the same effective DestinationRule.
// A partial opt-in would produce a connection failure for every request to that cluster.
func TestHBONEOriginationShimPairing(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.PreferHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, false)
	test.SetForTest(t, &features.EnableAmbientIngressMultiNetwork, false)
	port := &model.Port{Name: "http", Port: 80, Protocol: protocol.HTTP}
	svc := &model.Service{
		Hostname: "app.default.svc.cluster.local", DefaultAddress: "10.0.0.1",
		Ports: model.PortList{port}, Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{Name: "app", Namespace: "default"},
	}
	for _, tt := range []struct {
		name    string
		policy  *networking.TrafficPolicy
		subset  string
		subsets []*networking.Subset
		shim    bool
	}{
		{name: "raw", shim: true},
		{name: "istio mutual", shim: true, policy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL}}},
		{name: "application tls", policy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE}}},
		{name: "application mtls", policy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{
			Mode: networking.ClientTLSSettings_MUTUAL, ClientCertificate: "/cert", PrivateKey: "/key", CaCertificates: "/root",
		}}},
		{name: "proxy protocol", policy: &networking.TrafficPolicy{ProxyProtocol: &networking.TrafficPolicy_ProxyProtocol{}}},
		{name: "port application tls", policy: &networking.TrafficPolicy{PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{{
			Port: &networking.PortSelector{Number: 80}, Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE},
		}}}},
		{name: "subset application tls", subset: "v1", subsets: []*networking.Subset{{Name: "v1", TrafficPolicy: &networking.TrafficPolicy{
			Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE},
		}}}},
		{
			name: "subset disables application tls", shim: true, subset: "v1",
			policy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE}},
			subsets: []*networking.Subset{{Name: "v1", TrafficPolicy: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_DISABLE},
			}}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dr := config.Config{
				Meta: config.Meta{GroupVersionKind: gvk.DestinationRule, Name: "app", Namespace: "default"},
				Spec: &networking.DestinationRule{Host: string(svc.Hostname), TrafficPolicy: tt.policy, Subsets: tt.subsets},
			}
			cg := NewConfigGenTest(t, TestOptions{
				Services: []*model.Service{svc}, Configs: []config.Config{dr},
				Instances: []*model.ServiceInstance{{Service: svc, ServicePort: port, Endpoint: &model.IstioEndpoint{
					Addresses: []string{"10.0.0.2"}, EndpointPort: 8080, ServicePortName: "http",
					Namespace: "default", CapturedByZtunnel: true,
				}}},
			})
			for _, kind := range []model.NodeType{model.Waypoint, model.Router} {
				for _, enabled := range []bool{false, true} {
					proxy := shimTestProxy(cg, kind, enabled)
					cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, model.NewXdsCache())
					direction, subset := model.TrafficDirectionOutbound, tt.subset
					var subsetPolicy *networking.TrafficPolicy
					if len(tt.subsets) > 0 {
						subsetPolicy = tt.subsets[0].TrafficPolicy
					}
					policy := util.MergeSubsetTrafficPolicy(tt.policy, subsetPolicy, port)
					var c *cluster.Cluster
					if kind == model.Waypoint {
						direction, subset = model.TrafficDirectionInboundVIP, "http"
						if tt.subset != "" {
							subset += "/" + tt.subset
						}
						c = cb.buildWaypointInboundVIPCluster(proxy, svc, *port, subset, cg.PushContext().Mesh, policy, &dr)
					} else {
						c = &cluster.Cluster{ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}}
						cb.applyTrafficPolicy(svc, buildClusterOpts{
							mutable: &clusterWrapper{cluster: c}, policy: policy, port: port,
							mesh: cg.PushContext().Mesh, direction: direction,
						})
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
					clusterName := model.BuildSubsetKey(direction, subset, svc.Hostname, port.Port)
					eb := endpoints.NewEndpointBuilder(clusterName, proxy, cg.PushContext())
					localities := eb.FromServiceEndpoints()
					if len(localities) != 1 || len(localities[0].LbEndpoints) != 1 {
						t.Fatalf("unexpected endpoints: %v", localities)
					}
					ep := localities[0].LbEndpoints[0].GetEndpoint()
					wantListener := ConnectOriginate
					if wantShim {
						wantListener = util.HBONEOriginationShimListener
					}
					assert.Equal(t, ep.Address.GetEnvoyInternalAddress().GetServerListenerName(), wantListener)
					assert.Equal(t, ep.Address.GetEnvoyInternalAddress().GetEndpointId(), "10.0.0.2:8080")
					// The shim and legacy traffic continue to use the identical outer mTLS/H2 cluster.
					outer := cb.buildWaypointConnectOriginate(proxy, cg.PushContext())
					assert.Equal(t, outer.TransportSocket.Name, "tls")
					assert.Equal(t, outer.TypedExtensionProtocolOptions != nil, true)
				}
			}
		})
	}
}

func TestHBONEOriginationShimMetadata(t *testing.T) {
	for _, baggage := range []bool{false, true} {
		test.SetForTest(t, &features.EnableAmbientBaggage, baggage)
		for _, pair := range [][2]*core.TransportSocket{
			{util.WaypointInternalUpstreamTransportSocket(util.RawBufferTransport()), util.WaypointHBONEOriginationShimTransportSocket()},
			{util.FullMetadataPassthroughInternalUpstreamTransportSocket(util.RawBufferTransport()), util.FullMetadataPassthroughHBONEOriginationShimTransportSocket()},
		} {
			original := &internalupstream.InternalUpstreamTransport{}
			assert.NoError(t, pair[0].GetTypedConfig().UnmarshalTo(original))
			wrapped := &udpa.TypedStruct{}
			assert.NoError(t, pair[1].GetTypedConfig().UnmarshalTo(wrapped))
			assert.Equal(t, wrapped.TypeUrl, util.HBONEOriginationShimUpstreamConfigType)
			decoded := &internalupstream.InternalUpstreamTransport{}
			assert.NoError(t, protomarshal.StructToMessageSlow(wrapped.Value, decoded))
			assert.Equal(t, decoded.PassthroughMetadata, original.PassthroughMetadata)
		}
	}
}

func TestHBONEOriginationShimCacheIsolation(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientIngressMultiNetwork, false)
	svc := &model.Service{Hostname: "app.default.svc.cluster.local", Attributes: model.ServiceAttributes{Namespace: "default"}}
	port := &model.Port{Name: "http", Port: 80, Protocol: protocol.HTTP}
	cg := NewConfigGenTest(t, TestOptions{Services: []*model.Service{svc}})
	keys := make([][2]any, 0, 2)
	for _, enabled := range []bool{false, true} {
		proxy := shimTestProxy(cg, model.Router, enabled)
		cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, model.NewXdsCache())
		cds := buildClusterKey(svc, port, cb, proxy, nil)
		eds := endpoints.NewEndpointBuilder(model.BuildSubsetKey(model.TrafficDirectionOutbound, "", svc.Hostname, port.Port), proxy, cg.PushContext())
		keys = append(keys, [2]any{cds.Key(), eds.Key()})
	}
	if keys[0][0] == keys[1][0] || keys[0][1] == keys[1][1] {
		t.Fatal("shim and stock proxy share an xDS cache key")
	}
}
