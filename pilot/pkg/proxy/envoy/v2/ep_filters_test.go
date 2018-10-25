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
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

func TestEndpointsByNetworkFilter(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []endpoint.LocalityLbEndpoints
		conn      *XdsConnection
		env       *model.Environment
		want      []endpoint.LocalityLbEndpoints
	}{
		{
			name:      "simple",
			conn:      xdsConnection("network1"),
			env:       environment(),
			endpoints: endpoints([]string{"network1"}),
			want:      endpoints([]string{"network1"}),
		},
		{
			name:      "simple",
			conn:      xdsConnection("network1"),
			env:       environment(),
			endpoints: endpoints([]string{"network1", "network2"}),
			want:      endpoints([]string{"network1"}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := EndpointsByNetworkFilter(tt.endpoints, tt.conn, tt.env)
			if len(filtered) != len(tt.want) {
				t.Errorf("Unexpected number of filtered endpoints: %v, want %v", filtered, len(tt.want))
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

func environment() *model.Environment {
	return &model.Environment{
		MeshNetworks: &meshconfig.MeshNetworks{
			Networks: map[string]*meshconfig.Network{
				"network1": &meshconfig.Network{
					Endpoints: []*meshconfig.Network_NetworkEndpoints{
						{
							Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
								FromCidr: "10.10.1.1/24",
							},
						},
					},
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
					Endpoints: []*meshconfig.Network_NetworkEndpoints{
						{
							Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
								FromCidr: "10.11.1.1/24",
							},
						},
					},
					Gateways: []*meshconfig.Network_IstioNetworkGateway{
						{
							Gw: &meshconfig.Network_IstioNetworkGateway_Address{
								Address: "2.2.2.2",
							},
							Port: 80,
						},
					},
				},
			},
		},
	}
}

func endpoints(networks []string) []endpoint.LocalityLbEndpoints {
	eps := make([]endpoint.LocalityLbEndpoints, len(networks))
	for i, n := range networks {
		ep := endpoint.LocalityLbEndpoints{
			LbEndpoints: []endpoint.LbEndpoint{
				{
					Metadata: &core.Metadata{
						FilterMetadata: map[string]*types.Struct{
							"istio": &types.Struct{
								Fields: map[string]*types.Value{
									"network": &types.Value{
										Kind: &types.Value_StringValue{
											StringValue: n,
										},
									},
								},
							},
						},
					},
				},
			},
		}
		eps[i] = ep
	}
	return eps
}
