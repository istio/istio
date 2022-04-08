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
package xds_test

import (
	"fmt"
	"sort"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/types/known/structpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/network"
)

// Testing the Split Horizon EDS.

type expectedResults struct {
	weights map[string]uint32
}

func (r expectedResults) getAddrs() []string {
	var out []string
	for addr := range r.weights {
		out = append(out, addr)
	}
	sort.Strings(out)
	return out
}

// The test will setup 3 networks with various number of endpoints for the same service within
// each network. It creates an instance of memory registry for each cluster and populate it
// with Service, Instances and an ingress gateway service.
// It then conducts an EDS query from each network expecting results to match the design of
// the Split Horizon EDS - all local endpoints + endpoint per remote network that also has
// endpoints for the service.
func TestSplitHorizonEds(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{NetworksWatcher: mesh.NewFixedNetworksWatcher(nil)})

	// Set up a cluster registry for network 1 with 1 instance for the service 'service5'
	// Network has 1 gateway
	initRegistry(s, 1, []string{"159.122.219.1"}, 1)
	// Set up a cluster registry for network 2 with 2 instances for the service 'service5'
	// Network has 1 gateway
	initRegistry(s, 2, []string{"159.122.219.2"}, 2)
	// Set up a cluster registry for network 3 with 3 instances for the service 'service5'
	// Network has 2 gateways
	initRegistry(s, 3, []string{"159.122.219.3", "179.114.119.3"}, 3)
	// Set up a cluster registry for network 4 with 4 instances for the service 'service5'
	// but without any gateway, which is treated as accessible directly.
	initRegistry(s, 4, []string{}, 4)

	// Push contexts needs to be updated
	s.Discovery.ConfigUpdate(&model.PushRequest{Full: true})
	time.Sleep(time.Millisecond * 200) // give time for cache to clear

	fmt.Println("gateways", s.Env().NetworkManager.AllGateways())

	tests := []struct {
		network   string
		sidecarID string
		want      expectedResults
	}{
		{
			// Verify that EDS from network1 will return 1 local endpoint with local VIP + 2 remote
			// endpoints weighted accordingly with the IP of the ingress gateway.
			network:   "network1",
			sidecarID: sidecarID("10.1.0.1", "app3"),
			want: expectedResults{
				weights: map[string]uint32{
					// 1 local endpoint
					"10.1.0.1": 2,

					// 2 endopints on network 2, go through single gateway.
					"159.122.219.2": 4,

					// 3 endpoints on network 3, weights split across 2 gateways.
					"159.122.219.3": 3,
					"179.114.119.3": 3,

					// no gateway defined for network 4 - treat as directly reachable.
					"10.4.0.1": 2,
					"10.4.0.2": 2,
					"10.4.0.3": 2,
					"10.4.0.4": 2,
				},
			},
		},
		{
			// Verify that EDS from network2 will return 2 local endpoints with local VIPs + 2 remote
			// endpoints weighted accordingly with the IP of the ingress gateway.
			network:   "network2",
			sidecarID: sidecarID("10.2.0.1", "app3"),
			want: expectedResults{
				weights: map[string]uint32{
					// 2 local endpoints
					"10.2.0.1": 2,
					"10.2.0.2": 2,

					// 1 endpoint on network 1, accessed via gateway.
					"159.122.219.1": 2,

					// 3 endpoints on network 3, weights split across 2 gateways.
					"159.122.219.3": 3,
					"179.114.119.3": 3,

					// no gateway defined for network 4 - treat as directly reachable.
					"10.4.0.1": 2,
					"10.4.0.2": 2,
					"10.4.0.3": 2,
					"10.4.0.4": 2,
				},
			},
		},
		{
			// Verify that EDS from network3 will return 3 local endpoints with local VIPs + 2 remote
			// endpoints weighted accordingly with the IP of the ingress gateway.
			network:   "network3",
			sidecarID: sidecarID("10.3.0.1", "app3"),
			want: expectedResults{
				weights: map[string]uint32{
					// 3 local endpoints.
					"10.3.0.1": 2,
					"10.3.0.2": 2,
					"10.3.0.3": 2,

					// 1 endpoint on network 1, accessed via gateway.
					"159.122.219.1": 2,

					// 2 endpoint on network 2, accessed via gateway.
					"159.122.219.2": 4,

					// no gateway defined for network 4 - treat as directly reachable.
					"10.4.0.1": 2,
					"10.4.0.2": 2,
					"10.4.0.3": 2,
					"10.4.0.4": 2,
				},
			},
		},
		{
			// Verify that EDS from network4 will return 4 local endpoint with local VIP + 4 remote
			// endpoints weighted accordingly with the IP of the ingress gateway.
			network:   "network4",
			sidecarID: sidecarID("10.4.0.1", "app3"),
			want: expectedResults{
				weights: map[string]uint32{
					// 4 local endpoints.
					"10.4.0.1": 2,
					"10.4.0.2": 2,
					"10.4.0.3": 2,
					"10.4.0.4": 2,

					// 1 endpoint on network 1, accessed via gateway.
					"159.122.219.1": 2,

					// 2 endpoint on network 2, accessed via gateway.
					"159.122.219.2": 4,

					// 3 endpoints on network 3, weights split across 2 gateways.
					"159.122.219.3": 3,
					"179.114.119.3": 3,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run("from "+tt.network, func(t *testing.T) {
			verifySplitHorizonResponse(t, s, tt.network, tt.sidecarID, tt.want)
		})
	}
}

// Tests whether an EDS response from the provided network matches the expected results
func verifySplitHorizonResponse(t *testing.T, s *xds.FakeDiscoveryServer, network string, sidecarID string, expected expectedResults) {
	t.Helper()
	ads := s.ConnectADS().WithID(sidecarID)

	metadata := &structpb.Struct{Fields: map[string]*structpb.Value{
		"ISTIO_VERSION": {Kind: &structpb.Value_StringValue{StringValue: "1.3"}},
		"NETWORK":       {Kind: &structpb.Value_StringValue{StringValue: network}},
	}}

	ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		Node: &core.Node{
			Id:       ads.ID,
			Metadata: metadata,
		},
		TypeUrl: v3.ClusterType,
	})

	clusterName := "outbound|1080||service5.default.svc.cluster.local"
	res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		Node: &core.Node{
			Id:       ads.ID,
			Metadata: metadata,
		},
		TypeUrl:       v3.EndpointType,
		ResourceNames: []string{clusterName},
	})
	cla := xdstest.UnmarshalClusterLoadAssignment(t, res.Resources)[0]
	eps := cla.Endpoints

	if len(eps) != 1 {
		t.Fatalf("expecting 1 locality endpoint but got %d", len(eps))
	}

	lbEndpoints := eps[0].LbEndpoints
	if len(lbEndpoints) != len(expected.weights) {
		t.Fatalf("unexpected number of endpoints.\nWant:\n%v\nGot:\n%v", expected.getAddrs(), getLbEndpointAddrs(lbEndpoints))
	}

	for addr, weight := range expected.weights {
		var match *endpoint.LbEndpoint
		for _, ep := range lbEndpoints {
			if ep.GetEndpoint().Address.GetSocketAddress().Address == addr {
				match = ep
				break
			}
		}
		if match == nil {
			t.Fatalf("couldn't find endpoint with address %s: found %v", addr, getLbEndpointAddrs(lbEndpoints))
		}
		if match.LoadBalancingWeight.Value != weight {
			t.Errorf("weight for endpoint %s is expected to be %d but got %d", addr, weight, match.LoadBalancingWeight.Value)
		}
	}
}

// initRegistry creates and initializes a memory registry that holds a single
// service with the provided amount of endpoints. It also creates a service for
// the ingress with the provided external IP
func initRegistry(server *xds.FakeDiscoveryServer, networkNum int, gatewaysIP []string, numOfEndpoints int) {
	clusterID := cluster.ID(fmt.Sprintf("cluster%d", networkNum))
	networkID := network.ID(fmt.Sprintf("network%d", networkNum))
	memRegistry := memory.NewServiceDiscovery()
	memRegistry.EDSUpdater = server.Discovery

	server.Env().ServiceDiscovery.(*aggregate.Controller).AddRegistry(serviceregistry.Simple{
		ClusterID:        clusterID,
		ProviderID:       provider.Mock,
		ServiceDiscovery: memRegistry,
		Controller:       &memory.ServiceController{},
	})

	gws := make([]*meshconfig.Network_IstioNetworkGateway, 0)
	for _, gatewayIP := range gatewaysIP {
		if gatewayIP != "" {
			gw := &meshconfig.Network_IstioNetworkGateway{
				Gw: &meshconfig.Network_IstioNetworkGateway_Address{
					Address: gatewayIP,
				},
				Port: 80,
			}
			gws = append(gws, gw)
		}
	}

	if len(gws) != 0 {
		addNetwork(server, networkID, &meshconfig.Network{
			Gateways: gws,
		})
	}

	svcLabels := map[string]string{
		"version": "v1.1",
	}

	// Explicit test service, in the v2 memory registry. Similar with mock.MakeService,
	// but easier to read.
	memRegistry.AddService(&model.Service{
		Hostname:       "service5.default.svc.cluster.local",
		DefaultAddress: "10.10.0.1",
		Ports: []*model.Port{
			{
				Name:     "http-main",
				Port:     1080,
				Protocol: protocol.HTTP,
			},
		},
	})
	istioEndpoints := make([]*model.IstioEndpoint, numOfEndpoints)
	for i := 0; i < numOfEndpoints; i++ {
		addr := fmt.Sprintf("10.%d.0.%d", networkNum, i+1)
		istioEndpoints[i] = &model.IstioEndpoint{
			Address:         addr,
			EndpointPort:    2080,
			ServicePortName: "http-main",
			Network:         networkID,
			Locality: model.Locality{
				Label:     "az",
				ClusterID: clusterID,
			},
			Labels:  svcLabels,
			TLSMode: model.IstioMutualTLSModeLabel,
		}
	}
	memRegistry.SetEndpoints("service5.default.svc.cluster.local", "default", istioEndpoints)
}

func addNetwork(server *xds.FakeDiscoveryServer, id network.ID, network *meshconfig.Network) {
	meshNetworks := server.Env().NetworksWatcher.Networks()
	// copy old networks if they exist
	c := map[string]*meshconfig.Network{}
	if meshNetworks != nil {
		for k, v := range meshNetworks.Networks {
			c[k] = v
		}
	}
	// add the new one
	c[string(id)] = network
	server.Env().NetworksWatcher.SetNetworks(&meshconfig.MeshNetworks{Networks: c})
}

func getLbEndpointAddrs(eps []*endpoint.LbEndpoint) []string {
	addrs := make([]string, 0)
	for _, lbEp := range eps {
		addrs = append(addrs, lbEp.GetEndpoint().Address.GetSocketAddress().Address)
	}
	sort.Strings(addrs)
	return addrs
}
