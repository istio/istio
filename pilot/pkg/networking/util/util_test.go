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

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	pilot_meta "istio.io/istio/pkg/metadata/pilot"
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

func Test_isProxyVersion(t *testing.T) {
	tests := []struct {
		name   string
		node   *model.Proxy
		prefix string
		want   bool
	}{
		{
			"the given Proxy version is 1.x",
			&model.Proxy{
				Metadata: map[string]string{
					pilot_meta.ProxyVersion: "1.0",
				},
			},
			"1.",
			true,
		},
		{
			"the given Proxy version is not 1.x",
			&model.Proxy{
				Metadata: map[string]string{
					pilot_meta.ProxyVersion: "0.8",
				},
			},
			"1.",
			false,
		},
		{
			"the given Proxy version is 1.1",
			&model.Proxy{
				Metadata: map[string]string{
					pilot_meta.ProxyVersion: "1.1",
				},
			},
			"1.1",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isProxyVersion(tt.node, tt.prefix); got != tt.want {
				t.Errorf("isProxyVersion() = %v, want %v", got, tt.want)
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
