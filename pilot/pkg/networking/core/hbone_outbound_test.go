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

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

// Generate complete outbound CDS/LDS and EDS together. A mixed service must keep
// its native mTLS/plaintext matches while only HBONE endpoints enter the shim.
func TestHBONEOriginationShimOutbound(t *testing.T) {
	test.SetForTest(t, &features.EnableHBONESend, true)
	test.SetForTest(t, &features.PreferHBONESend, false)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, false)
	test.SetForTest(t, &features.FilterGatewayClusterConfig, false)
	for _, kind := range []model.NodeType{model.SidecarProxy, model.Router} {
		for _, proto := range []protocol.Instance{protocol.HTTP, protocol.HTTP2, protocol.TCP} {
			t.Run(fmt.Sprintf("%s/%s", kind, proto), func(t *testing.T) {
				port := &model.Port{Name: "app", Port: 80, Protocol: proto}
				svc := &model.Service{
					Hostname: "app.default.svc.cluster.local", DefaultAddress: "10.0.0.1",
					Ports: model.PortList{port}, Resolution: model.ClientSideLB,
					Attributes: model.ServiceAttributes{Name: "app", Namespace: "default"},
				}
				var instances []*model.ServiceInstance
				for _, ep := range []*model.IstioEndpoint{
					{Addresses: []string{"10.0.0.2"}, CapturedByZtunnel: true},
					{Addresses: []string{"10.0.0.3"}, TLSMode: model.IstioMutualTLSModeLabel},
					{Addresses: []string{"10.0.0.4"}},
				} {
					ep.EndpointPort, ep.ServicePortName, ep.Namespace = 8080, port.Name, "default"
					instances = append(instances, &model.ServiceInstance{Service: svc, ServicePort: port, Endpoint: ep})
				}
				cg := NewConfigGenTest(t, TestOptions{Services: []*model.Service{svc}, Instances: instances})
				name := model.BuildSubsetKey(model.TrafficDirectionOutbound, "", svc.Hostname, port.Port)
				stockProxy := cg.SetupProxy(&model.Proxy{Type: kind})
				stockClusters := xdstest.ExtractClusters(cg.Clusters(stockProxy))
				stock := stockClusters[name]
				if stock == nil {
					t.Fatal("missing stock service cluster")
				}
				stockBuilder := endpoints.NewEndpointBuilder(name, stockProxy, cg.PushContext())
				stockEndpoints := stockBuilder.FromServiceEndpoints()
				assert.Equal(t, len(stockEndpoints), 1)
				assert.Equal(t, len(stockEndpoints[0].LbEndpoints), 3)
				proxy := cg.SetupProxy(&model.Proxy{Type: kind, Metadata: &model.NodeMetadata{
					EnableHBONEConnectWait: true,
				}})
				clusters := xdstest.ExtractClusters(cg.Clusters(proxy))
				c := clusters[name]
				if c == nil || clusters[ConnectOriginate] == nil {
					t.Fatal("missing service or outer HBONE cluster")
				}
				assert.Equal(t, c.GetType(), cluster.Cluster_EDS)
				assert.Equal(t, c.GetUpstreamConfig().GetName(), util.HBONERejectionPool)
				assert.Equal(t, stock.UpstreamConfig == nil, true)
				assert.Equal(t, len(c.TransportSocketMatches), len(stock.TransportSocketMatches))
				found := false
				for i, match := range c.TransportSocketMatches {
					if match.Name == "hbone" {
						found = true
						assert.Equal(t, match.Match, stock.TransportSocketMatches[i].Match)
						assert.Equal(t, match.TransportSocket.Name, util.HBONEOriginationShimTransport)
					} else {
						assert.Equal(t, match, stock.TransportSocketMatches[i])
					}
				}
				assert.Equal(t, found, true)
				assert.Equal(t, c.LoadBalancingPolicy, stock.LoadBalancingPolicy)
				assert.Equal(t, c.LbPolicy, stock.LbPolicy)
				assert.Equal(t, clusters[ConnectOriginate], stockClusters[ConnectOriginate])
				listeners := cg.Listeners(proxy)
				assert.Equal(t, xdstest.ExtractListener(util.HBONEOriginationShimListener, listeners) != nil, true)
				assert.Equal(t, xdstest.ExtractListener(ConnectOriginate, listeners) != nil, true)
				builder := endpoints.NewEndpointBuilder(name, proxy, cg.PushContext())
				localities := builder.FromServiceEndpoints()
				assert.Equal(t, len(localities), 1)
				assert.Equal(t, len(localities[0].LbEndpoints), 3)
				hboneEndpoints := 0
				for i, ep := range localities[0].LbEndpoints {
					original := stockEndpoints[0].LbEndpoints[i]
					if addr := ep.GetEndpoint().Address.GetEnvoyInternalAddress(); addr != nil {
						hboneEndpoints++
						assert.Equal(t, addr.GetServerListenerName(), util.HBONEOriginationShimListener)
						assert.Equal(t, addr.GetEndpointId(), "10.0.0.2:8080")
						assert.Equal(t, ep.Metadata, original.Metadata)
						assert.Equal(t, ep.Metadata.FilterMetadata[util.EnvoyTransportSocketMetadataKey].Fields[model.TunnelLabelShortName].GetStringValue(), model.TunnelHTTP)
					} else {
						assert.Equal(t, ep, original)
					}
				}
				assert.Equal(t, hboneEndpoints, 1)
			})
		}
	}
}
