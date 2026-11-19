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
	"google.golang.org/protobuf/proto"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

func adapterTestProxy(cg *ConfigGenTest, kind model.NodeType, enabled bool) *model.Proxy {
	return cg.SetupProxy(&model.Proxy{
		Type:     kind,
		Metadata: &model.NodeMetadata{EnableHBONEConnectWait: model.StringBool(enabled)},
		Labels:   map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayMeshControllerLabel},
	})
}

func TestHBONEConnectWaitSharedListener(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, false)
	test.SetForTest(t, &features.EnableAmbientIngressMultiNetwork, false)
	for _, baggage := range []bool{false, true} {
		test.SetForTest(t, &features.EnableAmbientBaggage, baggage)
		cg := NewConfigGenTest(t, TestOptions{ConfigString: `
apiVersion: telemetry.istio.io/v1
kind: Telemetry
metadata:
  name: outbound-logs
  namespace: default
spec:
  accessLogging:
  - providers:
    - name: envoy
    match:
      mode: CLIENT
  - providers:
    - name: envoy
    match:
      mode: SERVER
    disabled: true
`})
		for _, kind := range []model.NodeType{model.Waypoint, model.Router, model.SidecarProxy} {
			stockListeners := cg.Listeners(adapterTestProxy(cg, kind, false))
			for _, enabled := range []bool{false, true} {
				proxy := adapterTestProxy(cg, kind, enabled)
				class := istionetworking.ListenerClassSidecarOutbound
				if kind == model.Waypoint {
					class = istionetworking.ListenerClassSidecarInbound
				}
				if kind == model.Router {
					class = istionetworking.ListenerClassGateway
				}
				want := buildConnectOriginateListener(cg.PushContext(), proxy, class)
				listeners := cg.Listeners(proxy)
				// CONNECT wait is configured on the clusters and transports, so enabling
				// it must not add listeners or change their filters, telemetry, or names.
				assert.Equal(t, listeners, stockListeners)
				found := 0
				for _, l := range listeners {
					if l.Name != ConnectOriginate {
						continue
					}
					found++
					assert.Equal(t, l, want)
					filters := l.FilterChains[0].Filters
					terminal := filters[len(filters)-1]
					assert.Equal(t, terminal.Name, wellknown.TCPProxy)
					tcpProxy := &tcp.TcpProxy{}
					assert.NoError(t, terminal.GetTypedConfig().UnmarshalTo(tcpProxy))
					if kind == model.SidecarProxy {
						assert.Equal(t, len(tcpProxy.AccessLog), 1)
					}
					assert.Equal(t, tcpProxy.GetCluster(), ConnectOriginate)
					assert.Equal(t, tcpProxy.GetTunnelingConfig().Hostname, "%DOWNSTREAM_LOCAL_ADDRESS%")
					assert.Equal(t, tcpProxy.GetUpstreamConnectMode(), tcp.UpstreamConnectMode_IMMEDIATE)
					assert.Equal(t, tcpProxy.GetTunnelingConfig().GetUsePost(), false)
				}
				assert.Equal(t, found, 1)
			}
		}
	}
}

// Verify EDS marks HBONE endpoints while CDS applies service, port, and subset policies.
func TestHBONEOriginationAdapterPairing(t *testing.T) {
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
		adapter bool
	}{
		{name: "raw", adapter: true},
		{name: "istio mutual", adapter: true, policy: &networking.TrafficPolicy{Tls: &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_ISTIO_MUTUAL}}},
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
			name: "subset disables application tls", adapter: true, subset: "v1",
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
			for _, kind := range []model.NodeType{model.Waypoint, model.Router, model.SidecarProxy} {
				var stockTransport *core.TransportSocket
				for _, enabled := range []bool{false, true} {
					proxy := adapterTestProxy(cg, kind, enabled)
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
					wantAdapter := enabled && tt.adapter
					assert.Equal(t, c.UpstreamConfig != nil, wantAdapter)
					clusterName := model.BuildSubsetKey(direction, subset, svc.Hostname, port.Port)
					eb := endpoints.NewEndpointBuilder(clusterName, proxy, cg.PushContext())
					localities := eb.FromServiceEndpoints()
					if len(localities) != 1 || len(localities[0].LbEndpoints) != 1 {
						t.Fatalf("unexpected endpoints: %v", localities)
					}
					assert.Equal(t, eb.WithSubset(subset).FromServiceEndpoints(), localities)
					metadata := localities[0].LbEndpoints[0].GetMetadata().GetFilterMetadata()[util.EnvoyTransportSocketMetadataKey].GetFields()
					// EDS marks HBONE endpoints regardless of policy; CDS controls which transport is used.
					assert.Equal(t, metadata[model.TunnelLabelShortName].GetStringValue(), model.TunnelHTTP)
					transport := c.TransportSocket
					for _, match := range c.TransportSocketMatches {
						matches := true
						for key, value := range match.Match.Fields {
							if !proto.Equal(metadata[key], value) {
								matches = false
								break
							}
						}
						if matches {
							transport = match.TransportSocket
							break
						}
					}
					if transport == nil {
						t.Fatal("missing HBONE transport")
					}
					assert.Equal(t, transport.Name == util.HBONEOriginationAdapterTransport, wantAdapter)
					if !enabled {
						stockTransport = transport
					}
					if !wantAdapter {
						assert.Equal(t, transport, stockTransport)
					}
					ep := localities[0].LbEndpoints[0].GetEndpoint()
					assert.Equal(t, ep.Address.GetEnvoyInternalAddress().GetServerListenerName(), ConnectOriginate)
					assert.Equal(t, xdstest.ExtractListener(ConnectOriginate, cg.Listeners(proxy)) != nil, true)
					assert.Equal(t, ep.Address.GetEnvoyInternalAddress().GetEndpointId(), "10.0.0.2:8080")
					// Ineligible services share the HBONE cluster, whose pool passes through
					// callers without CONNECT-wait state.
					outer := cb.buildWaypointConnectOriginate(proxy, cg.PushContext())
					assertHBONEConnectWaitPool(t, outer, enabled)
					assert.Equal(t, outer.TransportSocket.Name, "tls")
					assert.Equal(t, outer.TypedExtensionProtocolOptions != nil, true)
				}
			}
		})
	}
}

func TestHBONEOriginationAdapterMetadata(t *testing.T) {
	for _, baggage := range []bool{false, true} {
		test.SetForTest(t, &features.EnableAmbientBaggage, baggage)
		for _, pair := range [][2]*core.TransportSocket{
			{util.WaypointInternalUpstreamTransportSocket(util.RawBufferTransport()), util.WaypointHBONEOriginationAdapterTransportSocket()},
			{
				util.FullMetadataPassthroughInternalUpstreamTransportSocket(util.RawBufferTransport()),
				util.FullMetadataPassthroughHBONEOriginationAdapterTransportSocket(),
			},
		} {
			original := &internalupstream.InternalUpstreamTransport{}
			assert.NoError(t, pair[0].GetTypedConfig().UnmarshalTo(original))
			wrapped := &udpa.TypedStruct{}
			assert.NoError(t, pair[1].GetTypedConfig().UnmarshalTo(wrapped))
			assert.Equal(t, wrapped.TypeUrl, util.HBONEConnectWaitTransportSocketTypeURL)
			decoded := &internalupstream.InternalUpstreamTransport{}
			assert.NoError(t, protomarshal.StructToMessageSlow(wrapped.Value, decoded))
			assert.Equal(t, decoded.PassthroughMetadata, original.PassthroughMetadata)
		}
	}
}
