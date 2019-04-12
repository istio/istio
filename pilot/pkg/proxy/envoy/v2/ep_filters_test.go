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
	address string
	// nolint: structcheck
	weight uint32
}

type LocLbEpInfo struct {
	lbEps  []LbEpInfo
	weight uint32
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
				{
					lbEps: []LbEpInfo{
						// 2 local endpoints
						{address: "10.0.0.1", weight: 2},
						{address: "10.0.0.2", weight: 2},
						// 1 endpoint to gateway of network2 with weight 1 because it has 1 endpoint
						{address: "2.2.2.2", weight: 1},
						{address: "2.2.2.20", weight: 1},
					},
					weight: 6,
				},
			},
		},
		{
			name:      "from_network2",
			conn:      xdsConnection("network2"),
			env:       env,
			endpoints: testEndpoints,
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 1 local endpoint
						{address: "20.0.0.1", weight: 2},
						// 1 endpoint to gateway of network1 with weight 4 because it has 2 endpoints
						{address: "1.1.1.1", weight: 4},
					},
					weight: 6,
				},
			},
		},
		{
			name:      "from_network3",
			conn:      xdsConnection("network3"),
			env:       env,
			endpoints: testEndpoints,
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 1 endpoint to gateway of network1 with weight 4 because it has 2 endpoints
						{address: "1.1.1.1", weight: 4},
						// 1 endpoint to gateway of network2 with weight 2 because it has 1 endpoint
						{address: "2.2.2.2", weight: 1},
						{address: "2.2.2.20", weight: 1},
					},
					weight: 6,
				},
			},
		},
		{
			name:      "from_network4",
			conn:      xdsConnection("network4"),
			env:       env,
			endpoints: testEndpoints,
			want: []LocLbEpInfo{
				{
					lbEps: []LbEpInfo{
						// 1 local endpoint
						{address: "40.0.0.1", weight: 2},
						// 1 endpoint to gateway of network1 with weight 2 because it has 2 endpoints
						{address: "1.1.1.1", weight: 4},
						// 1 endpoint to gateway of network2 with weight 1 because it has 1 endpoint
						{address: "2.2.2.2", weight: 1},
						{address: "2.2.2.20", weight: 1},
					},
					weight: 8,
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
				addrI := filtered[i].LbEndpoints[0].GetEndpoint().Address.GetSocketAddress().Address
				addrJ := filtered[j].LbEndpoints[0].GetEndpoint().Address.GetSocketAddress().Address
				return addrI < addrJ
			})

			for i, ep := range filtered {
				if len(ep.LbEndpoints) != len(tt.want[i].lbEps) {
					t.Errorf("Unexpected number of LB endpoints within endpoint %d: %v, want %v", i, len(ep.LbEndpoints), len(tt.want[i].lbEps))
				}

				if ep.LoadBalancingWeight.GetValue() != tt.want[i].weight {
					t.Errorf("Unexpected weight for endpoint %d: got %v, want %v", i, ep.LoadBalancingWeight.GetValue(), tt.want[i].weight)
				}

				for _, lbEp := range ep.LbEndpoints {
					addr := lbEp.GetEndpoint().Address.GetSocketAddress().Address
					found := false
					for _, wantLbEp := range tt.want[i].lbEps {
						if addr == wantLbEp.address {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Unexpected address for endpoint %d: %v", i, addr)
					}
				}
			}
		})
	}
}

func xdsConnection(network string) *XdsConnection {
	var metadata map[string]string
	if network != "" {
		metadata = map[string]string{"NETWORK": network}
	}
	return &XdsConnection{
		modelNode: &model.Proxy{
			Metadata: metadata,
		},
	}
}

// environment creates an Environment object with the following MeshNetworks configurations:
//  - 1 gateway for network1
//  - 1 gateway for network2
//  - 1 gateway for network3
//  - 0 gateways for network4
func environment() *model.Environment {
	return &model.Environment{
		MeshNetworks: &meshconfig.MeshNetworks{
			Networks: map[string]*meshconfig.Network{
				"network1": {
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "1.1.1.1",
							},
							Port: 80,
						},
					},
				},
				"network2": {
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "2.2.2.2",
							},
							Port: 80,
						},
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "2.2.2.20",
							},
							Port: 80,
						},
					},
				},
				"network3": {
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "3.3.3.3",
							},
							Port: 443,
						},
					},
				},
				"network4": {
					Gateways: []*meshconfig.Network_IstioNetworkGateway{},
				},
			},
		},
	}
}

// testEndpoints creates endpoints to be handed to the filter. It creates
// 2 endpoints on network1, 1 endpoint on network2 and 1 endpoint on network4.
func testEndpoints() []endpoint.LocalityLbEndpoints {
	lbEndpoints := createLbEndpoints(
		[]LbEpInfo{
			{network: "network1", address: "10.0.0.1"},
			{network: "network1", address: "10.0.0.2"},
			{network: "network2", address: "20.0.0.1"},
			{network: "network4", address: "40.0.0.1"},
		},
	)

	return []endpoint.LocalityLbEndpoints{
		{
			LbEndpoints: lbEndpoints,
			LoadBalancingWeight: &types.UInt32Value{
				Value: uint32(len(lbEndpoints)),
			},
		},
	}

}

func createLbEndpoints(lbEpsInfo []LbEpInfo) []endpoint.LbEndpoint {
	lbEndpoints := make([]endpoint.LbEndpoint, len(lbEpsInfo))
	for j, lbEpInfo := range lbEpsInfo {
		lbEp := endpoint.LbEndpoint{
			HostIdentifier: &endpoint.LbEndpoint_Endpoint{
				Endpoint: &endpoint.Endpoint{
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Address: lbEpInfo.address,
							},
						},
					},
				},
			},
			Metadata: &core.Metadata{
				FilterMetadata: map[string]*types.Struct{
					"istio": {
						Fields: map[string]*types.Value{
							"network": {
								Kind: &types.Value_StringValue{
									StringValue: lbEpInfo.network,
								},
							},
							"uid": {
								Kind: &types.Value_StringValue{
									StringValue: "kubernetes://dummy",
								},
							},
						},
					},
				},
			},
		}
		lbEndpoints[j] = lbEp
	}

	return lbEndpoints
}
