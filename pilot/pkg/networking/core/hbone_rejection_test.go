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
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestHBONEConnectWaitOptInAndCacheIsolation(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.PreferHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, false)
	svc := &model.Service{Hostname: "app.default.svc.cluster.local", Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{Namespace: "default"}}
	cg := NewConfigGenTest(t, TestOptions{Services: []*model.Service{svc}})
	for _, kind := range []model.NodeType{model.Waypoint, model.Router, model.SidecarProxy} {
		for _, appProtocol := range []protocol.Instance{protocol.HTTP, protocol.HTTP2, protocol.TCP} {
			port := &model.Port{Name: "app", Port: 80, Protocol: appProtocol}
			var stockCDS, stockEDS any
			for _, tt := range []struct {
				name     string
				metadata map[string]any
				want     bool
			}{
				{name: "unset", metadata: map[string]any{}},
				{name: "disabled", metadata: map[string]any{"ENABLE_HBONE_CONNECT_WAIT": "false"}},
				{name: "enabled", want: true, metadata: map[string]any{"ENABLE_HBONE_CONNECT_WAIT": "true"}},
			} {
				t.Run(fmt.Sprintf("%s/%s/%s", kind, appProtocol, tt.name), func(t *testing.T) {
					metadata, err := structpb.NewStruct(tt.metadata)
					assert.NoError(t, err)
					parsed, err := model.ParseMetadata(metadata)
					assert.NoError(t, err)
					proxy := cg.SetupProxy(&model.Proxy{
						Type: kind, Metadata: parsed,
						Labels: map[string]string{label.GatewayManaged.Name: constants.ManagedGatewayMeshControllerLabel},
					})
					cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, model.NewXdsCache())
					var c *cluster.Cluster
					if kind == model.Waypoint {
						subset := "http"
						if appProtocol == protocol.TCP {
							subset = "tcp"
						}
						c = cb.buildWaypointInboundVIPCluster(proxy, svc, *port, subset, cg.PushContext().Mesh, nil, nil)
					} else {
						c = &cluster.Cluster{ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS}}
						cb.applyTrafficPolicy(svc, buildClusterOpts{
							mutable: &clusterWrapper{cluster: c}, port: port,
							mesh: cg.PushContext().Mesh, direction: model.TrafficDirectionOutbound,
						})
					}
					assert.Equal(t, c.UpstreamConfig != nil, tt.want)
					if tt.want {
						assert.Equal(t, c.UpstreamConfig.Name, util.HBONERejectionPool)
						assert.Equal(t, c.UpstreamConfig.TypedConfig.GetTypeUrl(), util.HBONERejectionPoolType)
						assert.Equal(t, len(c.UpstreamConfig.TypedConfig.GetValue()), 0)
					}
					cache := buildClusterKey(svc, port, cb, proxy, nil)
					direction, subset := model.TrafficDirectionOutbound, ""
					if kind == model.Waypoint {
						direction, subset = model.TrafficDirectionInboundVIP, "http"
						if appProtocol == protocol.TCP {
							subset = "tcp"
						}
					}
					eb := endpoints.NewEndpointBuilder(model.BuildSubsetKey(direction, subset, svc.Hostname, port.Port), proxy, cg.PushContext())
					if tt.name == "unset" {
						stockCDS, stockEDS = cache.Key(), eb.Key()
					}
					assert.Equal(t, cache.Key() != stockCDS, tt.want)
					assert.Equal(t, eb.Key() != stockEDS, tt.want)
					listeners := cg.Listeners(proxy)
					assert.Equal(t, xdstest.ExtractListener(util.HBONEOriginationAdapterListener, listeners) != nil, tt.want)
				})
			}
		}
	}
}

func TestHBONEConnectWaitNestedPairing(t *testing.T) {
	enableDoubleAdapterTest(t)
	for _, kind := range []model.NodeType{model.Waypoint, model.Router} {
		cg := NewConfigGenTest(t, TestOptions{ConfigString: doubleAdapterGateway})
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", kind, enabled), func(t *testing.T) {
				proxy := doubleAdapterProxy(cg, kind, enabled)
				cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, model.NewXdsCache())
				c := cb.buildInnerConnectOriginateCluster(proxy, cg.PushContext())
				outer := xdstest.ExtractListener(util.HBONEOuterOriginationAdapterListener, cg.Listeners(proxy))
				assert.Equal(t, outer != nil, enabled)
				endpoint := c.LoadAssignment.Endpoints[0].LbEndpoints[0].GetEndpoint().Address.GetEnvoyInternalAddress()
				if !enabled {
					assert.Equal(t, c.TransportSocket.Name, "internal_upstream_with_tls")
					assert.Equal(t, endpoint.GetServerListenerName(), DoubleHBONEOuterConnectOriginate)
					return
				}
				assert.Equal(t, c.TransportSocket.Name, util.HBONEOriginationAdapterTransport)
				assert.Equal(t, endpoint.GetServerListenerName(), outer.Name)
				assert.Equal(t, outer.FilterChains[0].Filters[0].Name, util.HBONEOriginationAdapterFilter)
				wrapped := &udpa.TypedStruct{}
				assert.NoError(t, c.TransportSocket.GetTypedConfig().UnmarshalTo(wrapped))
				socket := &core.TransportSocket{}
				assert.NoError(t, protomarshal.StructToMessageSlow(wrapped.Value.Fields["transport_socket"].GetStructValue(), socket))
				innerTLS := &tls.UpstreamTlsContext{}
				assert.NoError(t, socket.GetTypedConfig().UnmarshalTo(innerTLS))
				assert.Equal(t, innerTLS.CommonTlsContext, buildCommonConnectTLSContext(proxy, cg.PushContext()))
			})
		}
	}
}
