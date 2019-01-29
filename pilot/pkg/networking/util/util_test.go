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

package util

import (
	"reflect"
	"testing"

	"gopkg.in/d4l3k/messagediff.v1"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

func TestConvertAddressToCidr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want *core.CidrRange
	}{
		{
			"return nil when the address is empty",
			"",
			nil,
		},
		{
			"success case with no PrefixLen",
			"1.2.3.4",
			&core.CidrRange{
				AddressPrefix: "1.2.3.4",
				PrefixLen: &types.UInt32Value{
					Value: 32,
				},
			},
		},
		{
			"success case with PrefixLen",
			"1.2.3.4/16",
			&core.CidrRange{
				AddressPrefix: "1.2.3.4",
				PrefixLen: &types.UInt32Value{
					Value: 16,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertAddressToCidr(tt.addr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertAddressToCidr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetNetworkEndpointAddress(t *testing.T) {
	neUnix := &model.NetworkEndpoint{
		Family:  model.AddressFamilyUnix,
		Address: "/var/run/test/test.sock",
	}
	aUnix := GetNetworkEndpointAddress(neUnix)
	if aUnix.GetPipe() == nil {
		t.Fatalf("GetAddress() => want Pipe, got %s", aUnix.String())
	}
	if aUnix.GetPipe().GetPath() != neUnix.Address {
		t.Fatalf("GetAddress() => want path %s, got %s", neUnix.Address, aUnix.GetPipe().GetPath())
	}

	neIP := &model.NetworkEndpoint{
		Family:  model.AddressFamilyTCP,
		Address: "192.168.10.45",
		Port:    4558,
	}
	aIP := GetNetworkEndpointAddress(neIP)
	sock := aIP.GetSocketAddress()
	if sock == nil {
		t.Fatalf("GetAddress() => want SocketAddress, got %s", aIP.String())
	}
	if sock.GetAddress() != neIP.Address {
		t.Fatalf("GetAddress() => want %s, got %s", neIP.Address, sock.GetAddress())
	}
	if int(sock.GetPortValue()) != neIP.Port {
		t.Fatalf("GetAddress() => want port %d, got port %d", neIP.Port, sock.GetPortValue())
	}
}

func TestIsProxyVersionGE11(t *testing.T) {
	tests := []struct {
		name string
		node *model.Proxy
		want bool
	}{
		{
			"the given Proxy version is 1.x",
			&model.Proxy{
				Metadata: map[string]string{
					"ISTIO_PROXY_VERSION": "1.0",
				},
			},
			false,
		},
		{
			"the given Proxy version is not 1.x",
			&model.Proxy{
				Metadata: map[string]string{
					"ISTIO_PROXY_VERSION": "0.8",
				},
			},
			false,
		},
		{
			"the given Proxy version is 1.1",
			&model.Proxy{
				Metadata: map[string]string{
					"ISTIO_PROXY_VERSION": "1.1",
				},
			},
			true,
		},
		{
			"the given Proxy version is 1.1.1",
			&model.Proxy{
				Metadata: map[string]string{
					"ISTIO_PROXY_VERSION": "1.1.1",
				},
			},
			true,
		},
		{
			"the given Proxy version is 2.0",
			&model.Proxy{
				Metadata: map[string]string{
					"ISTIO_PROXY_VERSION": "2.0",
				},
			},
			true,
		},
		{
			"the given Proxy version is 10.0",
			&model.Proxy{
				Metadata: map[string]string{
					"ISTIO_PROXY_VERSION": "2.0",
				},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsProxyVersionGE11(tt.node); got != tt.want {
				t.Errorf("IsProxyVersionGE11() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveHostsInNetworksConfig(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		modified bool
	}{
		{
			"Gateway with IP address",
			"9.142.3.1",
			false,
		},
		{
			"Gateway with localhost address",
			"localhost",
			true,
		},
		{
			"Gateway with empty address",
			"",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"network": {
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_Address{
									Address: tt.address,
								},
							},
						},
					},
				},
			}
			ResolveHostsInNetworksConfig(config)
			addrAfter := config.Networks["network"].Gateways[0].GetAddress()
			if addrAfter == tt.address && tt.modified {
				t.Fatalf("Expected network address to be modified but it's the same as before calling the function")
			}
			if addrAfter != tt.address && !tt.modified {
				t.Fatalf("Expected network address not to be modified after calling the function")
			}
		})
	}
}

func TestConvertLocality(t *testing.T) {
	tests := []struct {
		name     string
		locality string
		want     *core.Locality
	}{
		{
			"nil locality",
			"",
			nil,
		},
		{
			"locality with only region",
			"region",
			&core.Locality{
				Region: "region",
			},
		},
		{
			"locality with region and zone",
			"region/zone",
			&core.Locality{
				Region: "region",
				Zone:   "zone",
			},
		},
		{
			"locality with region zone and subzone",
			"region/zone/subzone",
			&core.Locality{
				Region:  "region",
				Zone:    "zone",
				SubZone: "subzone",
			},
		},
		{
			"locality with region zone subzone and rack",
			"region/zone/subzone/rack",
			&core.Locality{
				Region:  "region",
				Zone:    "zone",
				SubZone: "subzone",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertLocality(tt.locality)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expected locality %#v, but got %#v", tt.want, got)
			}
		})
	}
}

func TestBuildConfigInfoMetadata(t *testing.T) {
	cases := []struct {
		name string
		in   model.ConfigMeta
		want *core.Metadata
	}{
		{
			"route-rule",
			model.ConfigMeta{Name: "svcA", Namespace: "default", Domain: "svc.cluster.local", Type: "route-rule"},
			&core.Metadata{
				FilterMetadata: map[string]*types.Struct{
					istioMetadataKey: {
						Fields: map[string]*types.Value{
							"route-rule": {
								Kind: &types.Value_StringValue{
									StringValue: "svcA.default.svc.cluster.local",
								},
							},
						},
					},
				},
			},
		},
		{
			"missing namespace",
			model.ConfigMeta{Name: "svcA", Domain: "svc.cluster.local", Type: "route-rule"},
			&core.Metadata{
				FilterMetadata: map[string]*types.Struct{
					istioMetadataKey: {
						Fields: map[string]*types.Value{
							"route-rule": {
								Kind: &types.Value_StringValue{
									StringValue: "svcA..svc.cluster.local",
								},
							},
						},
					},
				},
			},
		},
		{
			"missing domain",
			model.ConfigMeta{Name: "svcA", Namespace: "istio-system", Type: "unknown-type"},
			&core.Metadata{
				FilterMetadata: map[string]*types.Struct{
					istioMetadataKey: {
						Fields: map[string]*types.Value{
							"unknown-type": {
								Kind: &types.Value_StringValue{
									StringValue: "svcA.istio-system.", // is this OK?
								},
							},
						},
					},
				},
			},
		},
		{
			"no-type",
			model.ConfigMeta{Name: "svcA", Namespace: "istio-system", Domain: "istio.io"},
			&core.Metadata{
				FilterMetadata: map[string]*types.Struct{
					istioMetadataKey: {
						Fields: map[string]*types.Value{
							"": { // is this OK?
								Kind: &types.Value_StringValue{
									StringValue: "svcA.istio-system.istio.io",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			got := BuildConfigInfoMetadata(v.in)
			if diff, equal := messagediff.PrettyDiff(got, v.want); !equal {
				tt.Errorf("BuildConfigInfoMetadata(%v) produced incorrect result:\ngot: %v\nwant: %v\nDiff: %s", v.in, got, v.want, diff)
			}
		})
	}
}
