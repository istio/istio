// Copyright 2018 Istio Authors
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

package v2

import (
	"sort"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

type LbEpInfo struct {
	network string
	weight  uint32
	address string
}

type LocLbEpInfo struct {
	port  uint32
	lbEps []LbEpInfo
}

func TestEndpointsByNetworkFilter(t *testing.T) {

	// Environment defines the networks with:
	//  - 1 gateway for network1
	//  - 1 gateway for network2
	//  - 1 gateway for network3
	//  - 0 gateways for network4
	env := environment()

	// Test endpoints creates:
	//  - 2 endpoints in network1
	//  - 1 endpoints in network2
	//  - 0 endpoints in network3
	//  - 1 endpoints in network4
	testEndpoints := testEndpoints()

	// The tests below are calling the endpoints filter from each one of the
	// networks and examines the returned filtered endpoints
	tests := []struct {
		name      string
		endpoints []endpoint.LocalityLbEndpoints
		conn      *XdsConnection
		env       *model.Environment
		want      []LocLbEpInfo
	}{
		{
			name:      "from_network1",
			conn:      xdsConnection("network1"),
			env:       env,
			endpoints: testEndpoints,
			want: []LocLbEpInfo{
				{ // 2 local endpoints
					lbEps: []LbEpInfo{
						{address: "10.0.0.1"},
						{address: "10.0.0.2"},
					},
				},
				{ // 1 endpoint to gateway of network2 with weight 1 because it has 1 endpoint
					lbEps: []LbEpInfo{
						{address: "2.2.2.2", weight: 1},
					},
				},
			},
		},
		{
			name:      "from_network2",
			conn:      xdsConnection("network2"),
			env:       env,
			endpoints: testEndpoints,
			want: []LocLbEpInfo{
				{ // 1 endpoint to gateway of network1 with weight 2 because it has 2 endpoints
					lbEps: []LbEpInfo{
						{address: "1.1.1.1", weight: 2},
					},
				},
				{ // 1 local endpoint
					lbEps: []LbEpInfo{
						{address: "20.0.0.1"},
					},
				},
			},
		},
		{
			name:      "from_network3",
			conn:      xdsConnection("network3"),
			env:       env,
			endpoints: testEndpoints,
			want: []LocLbEpInfo{
				{ // 1 endpoint to gateway of network1 with weight 2 because it has 2 endpoints
					lbEps: []LbEpInfo{
						{address: "1.1.1.1", weight: 2},
					},
				},
				{ // 1 endpoint to gateway of network2 with weight 1 because it has 1 endpoint
					lbEps: []LbEpInfo{
						{address: "2.2.2.2", weight: 1},
					},
				},
			},
		},
		{
			name:      "from_network4",
			conn:      xdsConnection("network4"),
			env:       env,
			endpoints: testEndpoints,
			want: []LocLbEpInfo{
				{ // 1 endpoint to gateway of network1 with weight 2 because it has 2 endpoints
					lbEps: []LbEpInfo{
						{address: "1.1.1.1", weight: 2},
					},
				},
				{ // 1 endpoint to gateway of network2 with weight 1 because it has 1 endpoint
					lbEps: []LbEpInfo{
						{address: "2.2.2.2", weight: 1},
					},
				},
				{ // 1 local endpoint
					lbEps: []LbEpInfo{
						{address: "40.0.0.1"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := EndpointsByNetworkFilter(tt.endpoints, tt.conn, tt.env)
			if len(filtered) != len(tt.want) {
				t.Errorf("Unexpected number of filtered endpoints: got %v, want %v", len(filtered), len(tt.want))
				return
			}

			sort.Slice(filtered, func(i, j int) bool {
				addrI := filtered[i].LbEndpoints[0].Endpoint.Address.GetSocketAddress().Address
				addrJ := filtered[j].LbEndpoints[0].Endpoint.Address.GetSocketAddress().Address
				return addrI < addrJ
			})

			for i, ep := range filtered {
				if len(ep.LbEndpoints) != len(tt.want[i].lbEps) {
					t.Errorf("Unexpected number of LB endpoints within endpoint %d: %v, want %v", i, len(ep.LbEndpoints), len(tt.want[i].lbEps))
				}

				if ep.LoadBalancingWeight.GetValue() != tt.want[i].lbEps[0].weight {
					t.Errorf("Unexpected weight for endpoint %d: got %v, want %v", i, ep.LoadBalancingWeight.GetValue(), tt.want[i].lbEps[0].weight)
				}

				addr := ep.LbEndpoints[0].Endpoint.Address.GetSocketAddress().Address
				if addr != tt.want[i].lbEps[0].address {
					t.Errorf("Unexpected address for endpoint %d: got %v, want %v", i, addr, tt.want[i].lbEps[0].address)
				}
			}
		})
	}
}

func xdsConnection(network string) *XdsConnection {
	var metadata map[string]string
	if network != "" {
		metadata = map[string]string{"ISTIO_NETWORK": network}
	}
	return &XdsConnection{
		modelNode: &model.Proxy{
			Metadata: metadata,
		},
	}
}

// environment creates an Environment object with the following MeshNetworks configuraition:
//  - 1 gateway for network1
//  - 1 gateway for network2
//  - 1 gateway for network3
//  - 0 gateways for network4
func environment() *model.Environment {
	return &model.Environment{
		MeshNetworks: &meshconfig.MeshNetworks{
			Networks: map[string]*meshconfig.Network{
				"network1": &meshconfig.Network{
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "1.1.1.1",
							},
							Port: 80,
						},
					},
				},
				"network2": &meshconfig.Network{
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "2.2.2.2",
							},
							Port: 80,
						},
					},
				},
				"network3": &meshconfig.Network{
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "3.3.3.3",
							},
							Port: 443,
						},
					},
				},
				"network4": &meshconfig.Network{
					Gateways: []*meshconfig.Network_IstioNetworkGateway{},
				},
			},
		},
	}
}

// testEndpoints creates endpoints to be handed to the filter. It creates
// 2 endpoints on network1, 1 endpoint on network2 and 1 endpoint on network4.
func testEndpoints() []endpoint.LocalityLbEndpoints {
	return createEndpoints(
		[]LocLbEpInfo{
			{
				lbEps: []LbEpInfo{
					{network: "network1", address: "10.0.0.1"},
					{network: "network1", address: "10.0.0.2"},
					{network: "network2", address: "20.0.0.1"},
					{network: "network4", address: "40.0.0.1"},
				},
			},
		},
	)
}

func createEndpoints(locLbEpsInfo []LocLbEpInfo) []endpoint.LocalityLbEndpoints {
	locLbEps := make([]endpoint.LocalityLbEndpoints, len(locLbEpsInfo))
	for i, locLbEpInfo := range locLbEpsInfo {
		locLbEp := endpoint.LocalityLbEndpoints{}
		locLbEp.LbEndpoints = make([]endpoint.LbEndpoint, len(locLbEpInfo.lbEps))
		for j, lbEpInfo := range locLbEpInfo.lbEps {
			lbEp := endpoint.LbEndpoint{
				Endpoint: &endpoint.Endpoint{
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Address: lbEpInfo.address,
							},
						},
					},
				},
				Metadata: &core.Metadata{
					FilterMetadata: map[string]*types.Struct{
						"istio": &types.Struct{
							Fields: map[string]*types.Value{
								"network": &types.Value{
									Kind: &types.Value_StringValue{
										StringValue: lbEpInfo.network,
									},
								},
							},
						},
					},
				},
			}
			locLbEp.LbEndpoints[j] = lbEp
		}
		locLbEps[i] = locLbEp
	}
	return locLbEps
}
