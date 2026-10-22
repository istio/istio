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
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestHBONERejectionHandlingOptInAndCacheIsolation(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.PreferHBONESend, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, false)
	svc := &model.Service{Hostname: "app.default.svc.cluster.local", Resolution: model.ClientSideLB,
		Attributes: model.ServiceAttributes{Namespace: "default"}}
	cg := NewConfigGenTest(t, TestOptions{Services: []*model.Service{svc}})
	for _, kind := range []model.NodeType{model.Waypoint, model.Router, model.SidecarProxy} {
		for _, appProtocol := range []protocol.Instance{protocol.HTTP, protocol.HTTP2, protocol.TCP} {
			port := &model.Port{Name: "app", Port: 80, Protocol: appProtocol}
			var oldKey any
			for _, shim := range []bool{false, true} {
				for _, rejection := range []bool{false, true} {
					proxy := shimTestProxy(cg, kind, shim)
					proxy.Metadata.EnableHBONERejectionHandling = model.StringBool(rejection)
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
					want := shim && rejection
					assert.Equal(t, c.UpstreamConfig != nil, want)
					if want {
						assert.Equal(t, c.UpstreamConfig.Name, util.HBONERejectionPool)
						wrapped := &udpa.TypedStruct{}
						assert.NoError(t, c.UpstreamConfig.TypedConfig.UnmarshalTo(wrapped))
						assert.Equal(t, wrapped.TypeUrl, util.HBONERejectionPoolType)
					}
					cache := buildClusterKey(svc, port, cb, proxy, nil)
					if shim && !rejection {
						oldKey = cache.Key()
					}
					if want && oldKey == cache.Key() {
						t.Fatal("new and old shim share a CDS cache key")
					}
				}
			}
		}
	}
}

func TestHBONERejectionHandlingNestedPairing(t *testing.T) {
	enableDoubleShimTest(t)
	for _, kind := range []model.NodeType{model.Waypoint, model.Router} {
		cg := NewConfigGenTest(t, TestOptions{ConfigString: doubleShimGateway})
		for _, shim := range []bool{false, true} {
			for _, rejection := range []bool{false, true} {
				proxy := doubleShimProxy(cg, kind, shim)
				proxy.Metadata.EnableHBONERejectionHandling = model.StringBool(rejection)
				cb := NewClusterBuilder(proxy, &model.PushRequest{Push: cg.PushContext()}, model.NewXdsCache())
				c := cb.buildInnerConnectOriginateCluster(proxy, cg.PushContext())
				outer := xdstest.ExtractListener(util.HBONEOuterOriginationShimListener, cg.Listeners(proxy))
				want := shim && rejection
				assert.Equal(t, outer != nil, want)
				endpoint := c.LoadAssignment.Endpoints[0].LbEndpoints[0].GetEndpoint().Address.GetEnvoyInternalAddress()
				if !want {
					assert.Equal(t, c.TransportSocket.Name, "internal_upstream_with_tls")
					assert.Equal(t, endpoint.GetServerListenerName(), DoubleHBONEOuterConnectOriginate)
					continue
				}
				assert.Equal(t, c.TransportSocket.Name, util.HBONEOriginationShimTransport)
				assert.Equal(t, endpoint.GetServerListenerName(), outer.Name)
				assert.Equal(t, outer.FilterChains[0].Filters[0].Name, util.HBONEOriginationShimFilter)
				wrapped := &udpa.TypedStruct{}
				assert.NoError(t, c.TransportSocket.GetTypedConfig().UnmarshalTo(wrapped))
				socket := &core.TransportSocket{}
				assert.NoError(t, protomarshal.StructToMessageSlow(wrapped.Value.Fields["transport_socket"].GetStructValue(), socket))
				innerTLS := &tls.UpstreamTlsContext{}
				assert.NoError(t, socket.GetTypedConfig().UnmarshalTo(innerTLS))
				assert.Equal(t, innerTLS.CommonTlsContext, buildCommonConnectTLSContext(proxy, cg.PushContext()))
			}
		}
	}
}
