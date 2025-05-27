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

package util

import (
	"fmt"
	"reflect"
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	statefulsession "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/stateful_session/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	cookiev3 "github.com/envoyproxy/go-control-plane/envoy/extensions/http/stateful_session/cookie/v3"
	httpv3 "github.com/envoyproxy/go-control-plane/envoy/type/http/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	structpb "google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	xdsutil "istio.io/istio/pkg/wellknown"
)

var testCla = &endpoint.ClusterLoadAssignment{
	ClusterName: "cluster",
	Endpoints: []*endpoint.LocalityLbEndpoints{{
		Locality: &core.Locality{Region: "foo", Zone: "bar"},
		LbEndpoints: []*endpoint.LbEndpoint{
			{
				HostIdentifier:      &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{Hostname: "foo", Address: BuildAddress("1.1.1.1", 80)}},
				LoadBalancingWeight: &wrappers.UInt32Value{Value: 100},
			},
			{
				HostIdentifier:      &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{Hostname: "foo", Address: BuildAddress("1.1.1.1", 80)}},
				LoadBalancingWeight: &wrappers.UInt32Value{Value: 100},
			},
		},
		LoadBalancingWeight: &wrappers.UInt32Value{Value: 50},
		Priority:            2,
	}},
}

func BenchmarkCloneClusterLoadAssignment(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cpy := CloneClusterLoadAssignment(testCla)
		_ = cpy
	}
}

func TestCloneClusterLoadAssignment(t *testing.T) {
	cloned := CloneClusterLoadAssignment(testCla)
	cloned2 := CloneClusterLoadAssignment(testCla)
	if !cmp.Equal(testCla, cloned, protocmp.Transform()) {
		t.Fatalf("expected %v to be the same as %v", testCla, cloned)
	}
	cloned.ClusterName = "foo"
	cloned.Endpoints[0].LbEndpoints[0].LoadBalancingWeight.Value = 5
	if cmp.Equal(testCla, cloned, protocmp.Transform()) {
		t.Fatalf("expected %v to be the different from %v", testCla, cloned)
	}
	if !cmp.Equal(testCla, cloned2, protocmp.Transform()) {
		t.Fatalf("expected %v to be the same as %v", testCla, cloned)
	}
}

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
			"cidr with two /",
			"192.168.0.0//16",
			nil,
		},
		{
			"cidr with invalid prefix length",
			"192.168.0.0/ab",
			nil,
		},
		{
			"cidr with negative prefix length",
			"192.168.0.0/-16",
			nil,
		},
		{
			"invalid ip address",
			"19216800",
			nil,
		},
		{
			"valid ipv6 address",
			"2001:abcd:85a3::8a2e:370:1234",
			&core.CidrRange{
				AddressPrefix: "2001:abcd:85a3::8a2e:370:1234",
				PrefixLen:     &wrappers.UInt32Value{Value: 128},
			},
		},
		{
			"success case with no PrefixLen",
			"1.2.3.4",
			&core.CidrRange{
				AddressPrefix: "1.2.3.4",
				PrefixLen: &wrappers.UInt32Value{
					Value: 32,
				},
			},
		},
		{
			"success case with PrefixLen",
			"1.2.3.4/16",
			&core.CidrRange{
				AddressPrefix: "1.2.3.4",
				PrefixLen: &wrappers.UInt32Value{
					Value: 16,
				},
			},
		},
		{
			"ipv6",
			"2001:db8::",
			&core.CidrRange{
				AddressPrefix: "2001:db8::",
				PrefixLen: &wrappers.UInt32Value{
					Value: 128,
				},
			},
		},
		{
			"ipv6 with prefix",
			"2001:db8::/64",
			&core.CidrRange{
				AddressPrefix: "2001:db8::",
				PrefixLen: &wrappers.UInt32Value{
					Value: 64,
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

func TestConvertLocality(t *testing.T) {
	tests := []struct {
		name     string
		locality string
		want     *core.Locality
		reverse  string
	}{
		{
			name:     "nil locality",
			locality: "",
			want:     &core.Locality{},
		},
		{
			name:     "locality with only region",
			locality: "region",
			want: &core.Locality{
				Region: "region",
			},
		},
		{
			name:     "locality with region and zone",
			locality: "region/zone",
			want: &core.Locality{
				Region: "region",
				Zone:   "zone",
			},
		},
		{
			name:     "locality with region zone and subzone",
			locality: "region/zone/subzone",
			want: &core.Locality{
				Region:  "region",
				Zone:    "zone",
				SubZone: "subzone",
			},
		},
		{
			name:     "locality with region zone subzone and rack",
			locality: "region/zone/subzone/rack",
			want: &core.Locality{
				Region:  "region",
				Zone:    "zone",
				SubZone: "subzone",
			},
			reverse: "region/zone/subzone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertLocality(tt.locality)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expected locality %#v, but got %#v", tt.want, got)
			}
			// Verify we can reverse the conversion back to the original input
			reverse := LocalityToString(got)
			if tt.reverse != "" {
				// Special case, reverse lookup is different than original input
				if tt.reverse != reverse {
					t.Errorf("Expected locality string %s, got %v", tt.reverse, reverse)
				}
			} else if tt.locality != reverse {
				t.Errorf("Expected locality string %s, got %v", tt.locality, reverse)
			}
		})
	}
}

func TestLocalityMatch(t *testing.T) {
	tests := []struct {
		name     string
		locality *core.Locality
		rule     string
		match    bool
	}{
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "*",
			match: true,
		},
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/*",
			match: true,
		},
		{
			name: "wildcard matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone1/*",
			match: true,
		},
		{
			name: "wildcard not matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone2/*",
			match: false,
		},
		{
			name: "region matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1",
			match: true,
		},
		{
			name: "region and zone matching",
			locality: &core.Locality{
				Region:  "region1",
				Zone:    "zone1",
				SubZone: "subzone1",
			},
			rule:  "region1/zone1",
			match: true,
		},
		{
			name: "zubzone wildcard matching",
			locality: &core.Locality{
				Region: "region1",
				Zone:   "zone1",
			},
			rule:  "region1/zone1",
			match: true,
		},
		{
			name: "subzone mismatching",
			locality: &core.Locality{
				Region: "region1",
				Zone:   "zone1",
			},
			rule:  "region1/zone1/subzone2",
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := LocalityMatch(tt.locality, tt.rule)
			if match != tt.match {
				t.Errorf("Expected matching result %v, but got %v", tt.match, match)
			}
		})
	}
}

func TestIsLocalityEmpty(t *testing.T) {
	tests := []struct {
		name     string
		locality *core.Locality
		want     bool
	}{
		{
			"non empty locality",
			&core.Locality{
				Region: "region",
			},
			false,
		},
		{
			"empty locality",
			&core.Locality{
				Region: "",
			},
			true,
		},
		{
			"nil locality",
			nil,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsLocalityEmpty(tt.locality)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expected locality empty result %#v, but got %#v", tt.want, got)
			}
		})
	}
}

func TestBuildConfigInfoMetadata(t *testing.T) {
	cases := []struct {
		name string
		in   config.Meta
		want *core.Metadata
	}{
		{
			"destination-rule",
			config.Meta{
				Name:             "svcA",
				Namespace:        "default",
				Domain:           "svc.cluster.local",
				GroupVersionKind: gvk.DestinationRule,
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1/namespaces/default/destination-rule/svcA",
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
			if diff := cmp.Diff(got, v.want, protocmp.Transform()); diff != "" {
				tt.Errorf("BuildConfigInfoMetadata(%v) produced incorrect result:\ngot: %v\nwant: %v\nDiff: %s", v.in, got, v.want, diff)
			}
		})
	}
}

func TestAddConfigInfoMetadata(t *testing.T) {
	cases := []struct {
		name string
		in   config.Meta
		meta *core.Metadata
		want *core.Metadata
	}{
		{
			"nil metadata",
			config.Meta{
				Name:             "svcA",
				Namespace:        "default",
				Domain:           "svc.cluster.local",
				GroupVersionKind: gvk.DestinationRule,
			},
			nil,
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1/namespaces/default/destination-rule/svcA",
								},
							},
						},
					},
				},
			},
		},
		{
			"empty metadata",
			config.Meta{
				Name:             "svcA",
				Namespace:        "default",
				Domain:           "svc.cluster.local",
				GroupVersionKind: gvk.DestinationRule,
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{},
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1/namespaces/default/destination-rule/svcA",
								},
							},
						},
					},
				},
			},
		},
		{
			"existing istio metadata",
			config.Meta{
				Name:             "svcA",
				Namespace:        "default",
				Domain:           "svc.cluster.local",
				GroupVersionKind: gvk.DestinationRule,
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"other-config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "other-config",
								},
							},
						},
					},
				},
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"other-config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "other-config",
								},
							},
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1/namespaces/default/destination-rule/svcA",
								},
							},
						},
					},
				},
			},
		},
		{
			"existing non-istio metadata",
			config.Meta{
				Name:             "svcA",
				Namespace:        "default",
				Domain:           "svc.cluster.local",
				GroupVersionKind: gvk.DestinationRule,
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					"other-metadata": {
						Fields: map[string]*structpb.Value{
							"other-config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "other-config",
								},
							},
						},
					},
				},
			},
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					"other-metadata": {
						Fields: map[string]*structpb.Value{
							"other-config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "other-config",
								},
							},
						},
					},
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1/namespaces/default/destination-rule/svcA",
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
			got := AddConfigInfoMetadata(v.meta, v.in)
			if diff := cmp.Diff(got, v.want, protocmp.Transform()); diff != "" {
				tt.Errorf("AddConfigInfoMetadata(%v) produced incorrect result:\ngot: %v\nwant: %v\nDiff: %s", v.in, got, v.want, diff)
			}
		})
	}
}

func TestAddSubsetToMetadata(t *testing.T) {
	cases := []struct {
		name   string
		in     *core.Metadata
		subset string
		want   *core.Metadata
	}{
		{
			"simple subset",
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1/namespaces/default/destination-rule/svcA",
								},
							},
						},
					},
				},
			},
			"test-subset",
			&core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "/apis/networking.istio.io/v1/namespaces/default/destination-rule/svcA",
								},
							},
							"subset": {
								Kind: &structpb.Value_StringValue{
									StringValue: "test-subset",
								},
							},
						},
					},
				},
			},
		},
		{
			"no metadata",
			&core.Metadata{},
			"test-subset",
			&core.Metadata{},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			AddSubsetToMetadata(v.in, v.subset)
			got := v.in
			if diff := cmp.Diff(got, v.want, protocmp.Transform()); diff != "" {
				tt.Errorf("AddSubsetToMetadata(%v, %s) produced incorrect result:\ngot: %v\nwant: %v\nDiff: %s", v.in, v.subset, got, v.want, diff)
			}
		})
	}
}

func TestAddALPNOverrideToMetadata(t *testing.T) {
	alpnOverrideFalse := &core.Metadata{
		FilterMetadata: map[string]*structpb.Struct{
			IstioMetadataKey: {
				Fields: map[string]*structpb.Value{
					AlpnOverrideMetadataKey: {
						Kind: &structpb.Value_StringValue{
							StringValue: "false",
						},
					},
				},
			},
		},
	}

	cases := []struct {
		name    string
		tlsMode networking.ClientTLSSettings_TLSmode
		meta    *core.Metadata
		want    *core.Metadata
	}{
		{
			name:    "ISTIO_MUTUAL TLS",
			tlsMode: networking.ClientTLSSettings_ISTIO_MUTUAL,
			meta:    nil,
			want:    nil,
		},
		{
			name:    "DISABLED TLS",
			tlsMode: networking.ClientTLSSettings_DISABLE,
			meta:    nil,
			want:    nil,
		},
		{
			name:    "SIMPLE TLS and nil metadata",
			tlsMode: networking.ClientTLSSettings_SIMPLE,
			meta:    nil,
			want:    alpnOverrideFalse,
		},
		{
			name:    "MUTUAL TLS and nil metadata",
			tlsMode: networking.ClientTLSSettings_SIMPLE,
			meta:    nil,
			want:    alpnOverrideFalse,
		},
		{
			name:    "SIMPLE TLS and empty metadata",
			tlsMode: networking.ClientTLSSettings_SIMPLE,
			meta: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{},
			},
			want: alpnOverrideFalse,
		},
		{
			name:    "SIMPLE TLS and existing istio metadata",
			tlsMode: networking.ClientTLSSettings_SIMPLE,
			meta: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"other-config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "other-config",
								},
							},
						},
					},
				},
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"other-config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "other-config",
								},
							},
							AlpnOverrideMetadataKey: {
								Kind: &structpb.Value_StringValue{
									StringValue: "false",
								},
							},
						},
					},
				},
			},
		},
		{
			name:    "SIMPLE TLS and existing non-istio metadata",
			tlsMode: networking.ClientTLSSettings_SIMPLE,
			meta: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					"other-metadata": {
						Fields: map[string]*structpb.Value{
							"other-config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "other-config",
								},
							},
						},
					},
				},
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					"other-metadata": {
						Fields: map[string]*structpb.Value{
							"other-config": {
								Kind: &structpb.Value_StringValue{
									StringValue: "other-config",
								},
							},
						},
					},
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							AlpnOverrideMetadataKey: {
								Kind: &structpb.Value_StringValue{
									StringValue: "false",
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
			got := AddALPNOverrideToMetadata(v.meta, v.tlsMode)
			if diff := cmp.Diff(got, v.want, protocmp.Transform()); diff != "" {
				tt.Errorf("AddALPNOverrideToMetadata produced incorrect result:\ngot: %v\nwant: %v\nDiff: %s", got, v.want, diff)
			}
		})
	}
}

func TestIsHTTPFilterChain(t *testing.T) {
	httpFilterChain := &listener.FilterChain{
		Filters: []*listener.Filter{
			{
				Name: xdsutil.HTTPConnectionManager,
			},
		},
	}

	tcpFilterChain := &listener.FilterChain{
		Filters: []*listener.Filter{
			{
				Name: xdsutil.TCPProxy,
			},
		},
	}

	if !IsHTTPFilterChain(httpFilterChain) {
		t.Errorf("http Filter chain not detected properly")
	}

	if IsHTTPFilterChain(tcpFilterChain) {
		t.Errorf("tcp filter chain detected as http filter chain")
	}
}

func TestIsAllowAnyOutbound(t *testing.T) {
	tests := []struct {
		name   string
		node   *model.Proxy
		result bool
	}{
		{
			name:   "NilSidecarScope",
			node:   &model.Proxy{},
			result: false,
		},
		{
			name: "NilOutboundTrafficPolicy",
			node: &model.Proxy{
				SidecarScope: &model.SidecarScope{},
			},
			result: false,
		},
		{
			name: "OutboundTrafficPolicyRegistryOnly",
			node: &model.Proxy{
				SidecarScope: &model.SidecarScope{
					OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
						Mode: networking.OutboundTrafficPolicy_REGISTRY_ONLY,
					},
				},
			},
			result: false,
		},
		{
			name: "OutboundTrafficPolicyAllowAny",
			node: &model.Proxy{
				SidecarScope: &model.SidecarScope{
					OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
						Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
					},
				},
			},
			result: true,
		},
	}
	for i := range tests {
		t.Run(tests[i].name, func(t *testing.T) {
			out := IsAllowAnyOutbound(tests[i].node)
			if out != tests[i].result {
				t.Errorf("Expected %t but got %t for test case: %v\n", tests[i].result, out, tests[i].node)
			}
		})
	}
}

func TestBuildAddress(t *testing.T) {
	testCases := []struct {
		name     string
		addr     string
		port     uint32
		expected *core.Address
	}{
		{
			name: "ipv4",
			addr: "172.10.10.1",
			port: 8080,
			expected: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: "172.10.10.1",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 8080,
						},
					},
				},
			},
		},
		{
			name: "ipv6",
			addr: "fe80::10e7:52ff:fecd:198b",
			port: 8080,
			expected: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: "fe80::10e7:52ff:fecd:198b",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 8080,
						},
					},
				},
			},
		},
		{
			name: "uds",
			addr: "/var/run/test/socket",
			port: 0,
			expected: &core.Address{
				Address: &core.Address_Pipe{
					Pipe: &core.Pipe{
						Path: "/var/run/test/socket",
					},
				},
			},
		},
		{
			name: "uds with unix prefix",
			addr: "unix:///var/run/test/socket",
			port: 0,
			expected: &core.Address{
				Address: &core.Address_Pipe{
					Pipe: &core.Pipe{
						Path: "/var/run/test/socket",
					},
				},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			addr := BuildAddress(test.addr, test.port)
			if !reflect.DeepEqual(addr, test.expected) {
				t.Errorf("expected add %v, but got %v", test.expected, addr)
			}
		})
	}
}

func TestGetEndpointHost(t *testing.T) {
	tests := []struct {
		name     string
		endpoint *endpoint.LbEndpoint
		want     string
	}{
		{
			name: "socket address",
			endpoint: &endpoint.LbEndpoint{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &core.Address{
							Address: &core.Address_SocketAddress{
								SocketAddress: &core.SocketAddress{
									Address: "10.0.0.1",
								},
							},
						},
					},
				},
			},
			want: "10.0.0.1",
		},
		{
			name: "internal address",
			endpoint: &endpoint.LbEndpoint{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &core.Address{
							Address: &core.Address_EnvoyInternalAddress{
								EnvoyInternalAddress: &core.EnvoyInternalAddress{
									AddressNameSpecifier: &core.EnvoyInternalAddress_ServerListenerName{
										ServerListenerName: "connect_originate",
									},
									EndpointId: "10.0.0.1:80",
								},
							},
						},
					},
				},
			},
			want: "10.0.0.1",
		},
		{
			name: "internal address(ipv6)",
			endpoint: &endpoint.LbEndpoint{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &core.Address{
							Address: &core.Address_EnvoyInternalAddress{
								EnvoyInternalAddress: &core.EnvoyInternalAddress{
									AddressNameSpecifier: &core.EnvoyInternalAddress_ServerListenerName{
										ServerListenerName: "connect_originate",
									},
									EndpointId: "[fd00:10:96::7fc7]:80",
								},
							},
						},
					},
				},
			},
			want: "fd00:10:96::7fc7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetEndpointHost(tt.endpoint); got != tt.want {
				t.Errorf("GetEndpointHost got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCidrRangeSliceEqual(t *testing.T) {
	tests := []struct {
		name   string
		first  []*core.CidrRange
		second []*core.CidrRange
		want   bool
	}{
		{
			"both nil",
			nil,
			nil,
			true,
		},
		{
			"unequal length",
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.4",
					PrefixLen: &wrappers.UInt32Value{
						Value: 32,
					},
				},
				{
					AddressPrefix: "1.2.3.5",
					PrefixLen: &wrappers.UInt32Value{
						Value: 32,
					},
				},
			},
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.4",
					PrefixLen: &wrappers.UInt32Value{
						Value: 32,
					},
				},
			},
			false,
		},
		{
			"equal cidr",
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.4",
					PrefixLen: &wrappers.UInt32Value{
						Value: 32,
					},
				},
			},
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.4",
					PrefixLen: &wrappers.UInt32Value{
						Value: 32,
					},
				},
			},
			true,
		},
		{
			"equal cidr with different insignificant bits",
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.4",
					PrefixLen: &wrappers.UInt32Value{
						Value: 24,
					},
				},
			},
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.5",
					PrefixLen: &wrappers.UInt32Value{
						Value: 24,
					},
				},
			},
			true,
		},
		{
			"unequal cidr address prefix mismatch",
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.4",
					PrefixLen: &wrappers.UInt32Value{
						Value: 32,
					},
				},
			},
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.5",
					PrefixLen: &wrappers.UInt32Value{
						Value: 32,
					},
				},
			},
			false,
		},
		{
			"unequal cidr prefixlen mismatch",
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.4",
					PrefixLen: &wrappers.UInt32Value{
						Value: 32,
					},
				},
			},
			[]*core.CidrRange{
				{
					AddressPrefix: "1.2.3.4",
					PrefixLen: &wrappers.UInt32Value{
						Value: 16,
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CidrRangeSliceEqual(tt.first, tt.second); got != tt.want {
				t.Errorf("Unexpected CidrRangeSliceEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEndpointMetadata(t *testing.T) {
	cases := []struct {
		name                   string
		enableTelemetryLabel   bool
		endpointTelemetryLabel bool
		metadata               *model.EndpointMetadata
		want                   *core.Metadata
	}{
		{
			name:                   "all empty",
			enableTelemetryLabel:   true,
			endpointTelemetryLabel: true,
			metadata: &model.EndpointMetadata{
				TLSMode:      model.DisabledTLSModeLabel,
				Network:      "",
				WorkloadName: "",
				ClusterID:    "",
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"workload": {
								Kind: &structpb.Value_StringValue{
									StringValue: ";;;;",
								},
							},
						},
					},
				},
			},
		},
		{
			name:                   "tls mode",
			enableTelemetryLabel:   true,
			endpointTelemetryLabel: true,
			metadata: &model.EndpointMetadata{
				TLSMode:      model.IstioMutualTLSModeLabel,
				Network:      "",
				WorkloadName: "",
				ClusterID:    "",
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					EnvoyTransportSocketMetadataKey: {
						Fields: map[string]*structpb.Value{
							model.TLSModeLabelShortname: {
								Kind: &structpb.Value_StringValue{
									StringValue: model.IstioMutualTLSModeLabel,
								},
							},
						},
					},
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"workload": {
								Kind: &structpb.Value_StringValue{
									StringValue: ";;;;",
								},
							},
						},
					},
				},
			},
		},
		{
			name:                   "network and tls mode",
			enableTelemetryLabel:   true,
			endpointTelemetryLabel: true,
			metadata: &model.EndpointMetadata{
				TLSMode:      model.IstioMutualTLSModeLabel,
				Network:      "network",
				WorkloadName: "",
				ClusterID:    "",
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					EnvoyTransportSocketMetadataKey: {
						Fields: map[string]*structpb.Value{
							model.TLSModeLabelShortname: {
								Kind: &structpb.Value_StringValue{
									StringValue: model.IstioMutualTLSModeLabel,
								},
							},
						},
					},
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"workload": {
								Kind: &structpb.Value_StringValue{
									StringValue: ";;;;",
								},
							},
						},
					},
				},
			},
		},
		{
			name:                   "all label",
			enableTelemetryLabel:   true,
			endpointTelemetryLabel: true,
			metadata: &model.EndpointMetadata{
				TLSMode:      model.IstioMutualTLSModeLabel,
				Network:      "network",
				WorkloadName: "workload",
				ClusterID:    "cluster",
				Namespace:    "default",
				Labels: labels.Instance{
					model.IstioCanonicalServiceLabelName:         "service",
					model.IstioCanonicalServiceRevisionLabelName: "v1",
				},
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					EnvoyTransportSocketMetadataKey: {
						Fields: map[string]*structpb.Value{
							model.TLSModeLabelShortname: {
								Kind: &structpb.Value_StringValue{
									StringValue: model.IstioMutualTLSModeLabel,
								},
							},
						},
					},
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"workload": {
								Kind: &structpb.Value_StringValue{
									StringValue: "workload;default;service;v1;cluster",
								},
							},
						},
					},
				},
			},
		},
		{
			name:                   "miss pod label",
			enableTelemetryLabel:   true,
			endpointTelemetryLabel: true,
			metadata: &model.EndpointMetadata{
				TLSMode:      model.IstioMutualTLSModeLabel,
				Network:      "network",
				WorkloadName: "workload",
				ClusterID:    "cluster",
				Namespace:    "default",
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					EnvoyTransportSocketMetadataKey: {
						Fields: map[string]*structpb.Value{
							model.TLSModeLabelShortname: {
								Kind: &structpb.Value_StringValue{
									StringValue: model.IstioMutualTLSModeLabel,
								},
							},
						},
					},
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"workload": {
								Kind: &structpb.Value_StringValue{
									StringValue: "workload;default;workload;;cluster",
								},
							},
						},
					},
				},
			},
		},
		{
			name:                   "miss workload name",
			enableTelemetryLabel:   true,
			endpointTelemetryLabel: true,
			metadata: &model.EndpointMetadata{
				TLSMode:      model.IstioMutualTLSModeLabel,
				Network:      "network",
				WorkloadName: "",
				ClusterID:    "cluster",
				Namespace:    "",
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					EnvoyTransportSocketMetadataKey: {
						Fields: map[string]*structpb.Value{
							model.TLSModeLabelShortname: {
								Kind: &structpb.Value_StringValue{
									StringValue: model.IstioMutualTLSModeLabel,
								},
							},
						},
					},
					IstioMetadataKey: {
						Fields: map[string]*structpb.Value{
							"workload": {
								Kind: &structpb.Value_StringValue{
									StringValue: ";;;;cluster",
								},
							},
						},
					},
				},
			},
		},
		{
			name:                   "all empty, telemetry label disabled",
			enableTelemetryLabel:   false,
			endpointTelemetryLabel: false,
			metadata: &model.EndpointMetadata{
				TLSMode:      model.DisabledTLSModeLabel,
				Network:      "",
				WorkloadName: "",
				ClusterID:    "",
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{},
			},
		},
		{
			name:                   "tls mode, telemetry label disabled",
			enableTelemetryLabel:   false,
			endpointTelemetryLabel: false,
			metadata: &model.EndpointMetadata{
				TLSMode:      model.IstioMutualTLSModeLabel,
				Network:      "",
				WorkloadName: "",
				ClusterID:    "",
			},
			want: &core.Metadata{
				FilterMetadata: map[string]*structpb.Struct{
					EnvoyTransportSocketMetadataKey: {
						Fields: map[string]*structpb.Value{
							model.TLSModeLabelShortname: {
								Kind: &structpb.Value_StringValue{
									StringValue: model.IstioMutualTLSModeLabel,
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableTelemetryLabel, tt.enableTelemetryLabel)
			test.SetForTest(t, &features.EndpointTelemetryLabel, tt.endpointTelemetryLabel)
			input := &core.Metadata{}
			AppendLbEndpointMetadata(tt.metadata, input)
			if !reflect.DeepEqual(input, tt.want) {
				t.Errorf("Unexpected Endpoint metadata got %v, want %v", input, tt.want)
			}
		})
	}
}

func TestByteCount(t *testing.T) {
	cases := []struct {
		in  int
		out string
	}{
		{1, "1B"},
		{1000, "1.0kB"},
		{1_000_000, "1.0MB"},
		{1_500_000, "1.5MB"},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprint(tt.in), func(t *testing.T) {
			if got := ByteCount(tt.in); got != tt.out {
				t.Fatalf("got %v wanted %v", got, tt.out)
			}
		})
	}
}

func TestIPv6Compliant(t *testing.T) {
	tests := []struct {
		host  string
		match string
	}{
		{"localhost", "localhost"},
		{"127.0.0.1", "127.0.0.1"},
		{"::1", "[::1]"},
		{"2001:4860:0:2001::68", "[2001:4860:0:2001::68]"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.host), func(t *testing.T) {
			if got := IPv6Compliant(tt.host); got != tt.match {
				t.Fatalf("got %v wanted %v", got, tt.match)
			}
		})
	}
}

func TestDomainName(t *testing.T) {
	tests := []struct {
		host  string
		port  int
		match string
	}{
		{"localhost", 3000, "localhost:3000"},
		{"127.0.0.1", 3000, "127.0.0.1:3000"},
		{"::1", 3000, "[::1]:3000"},
		{"2001:4860:0:2001::68", 3000, "[2001:4860:0:2001::68]:3000"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.host), func(t *testing.T) {
			if got := DomainName(tt.host, tt.port); got != tt.match {
				t.Fatalf("got %v wanted %v", got, tt.match)
			}
		})
	}
}

func TestStatefulSessionFilterConfig(t *testing.T) {
	test.SetAtomicBoolForTest(t, features.EnablePersistentSessionFilter, true)
	cases := []struct {
		name           string
		service        *model.Service
		expectedconfig *statefulsession.StatefulSession
	}{
		{
			name:           "nil service",
			expectedconfig: nil,
		},
		{
			name: "service without cookie path",
			service: &model.Service{
				Attributes: model.ServiceAttributes{
					Labels: map[string]string{features.PersistentSessionLabel: "test-cookie"},
				},
			},
			expectedconfig: &statefulsession.StatefulSession{
				SessionState: &core.TypedExtensionConfig{
					Name: "envoy.http.stateful_session.cookie",
					TypedConfig: protoconv.MessageToAny(&cookiev3.CookieBasedSessionState{
						Cookie: &httpv3.Cookie{
							Path: "/",
							Name: "test-cookie",
						},
					}),
				},
			},
		},
		{
			name: "service with cookie path",
			service: &model.Service{
				Attributes: model.ServiceAttributes{
					Labels: map[string]string{features.PersistentSessionLabel: "test-cookie:/path"},
				},
			},
			expectedconfig: &statefulsession.StatefulSession{
				SessionState: &core.TypedExtensionConfig{
					Name: "envoy.http.stateful_session.cookie",
					TypedConfig: protoconv.MessageToAny(&cookiev3.CookieBasedSessionState{
						Cookie: &httpv3.Cookie{
							Path: "/path",
							Name: "test-cookie",
						},
					}),
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sessionConfig := MaybeBuildStatefulSessionFilterConfig(tt.service)
			if !reflect.DeepEqual(tt.expectedconfig, sessionConfig) {
				t.Errorf("unexpected stateful session filter config, expected: %v, got :%v", tt.expectedconfig, sessionConfig)
			}
		})
	}
}

func TestMergeSubsetTrafficPolicy(t *testing.T) {
	cases := []struct {
		name     string
		original *networking.TrafficPolicy
		subset   *networking.TrafficPolicy
		port     *model.Port
		expected *networking.TrafficPolicy
	}{
		{
			name:     "all nil policies",
			original: nil,
			subset:   nil,
			port:     nil,
			expected: nil,
		},
		{
			name: "no subset policy",
			original: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
			subset: nil,
			port:   nil,
			expected: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
		},
		{
			name:     "no parent policy",
			original: nil,
			subset: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
			port: nil,
			expected: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
		},
		{
			name: "merge non-conflicting fields",
			original: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				},
			},
			subset: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				Tunnel: &networking.TrafficPolicy_TunnelSettings{
					TargetHost: "example.com",
					TargetPort: 8443,
				},
			},
			port: nil,
			expected: &networking.TrafficPolicy{
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				},
				Tunnel: &networking.TrafficPolicy_TunnelSettings{
					TargetHost: "example.com",
					TargetPort: 8443,
				},
			},
		},
		{
			name: "subset overwrite top-level fields",
			original: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_ISTIO_MUTUAL,
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
			subset: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
			},
			port: nil,
			expected: &networking.TrafficPolicy{
				Tls: &networking.ClientTLSSettings{
					Mode: networking.ClientTLSSettings_SIMPLE,
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
			},
		},
		{
			name:     "merge port level policy, and do not inherit top-level fields",
			original: nil,
			subset: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_LEAST_REQUEST,
							},
						},
					},
				},
			},
			port: &model.Port{Port: 8080},
			expected: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_LEAST_REQUEST,
					},
				},
			},
		},
		{
			name: "merge port level policy, and do not inherit top-level fields",
			original: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 20,
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 15,
						},
					},
				},
			},
			subset: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 13,
						},
					},
				},
			},
			port: &model.Port{Port: 8080},
			expected: &networking.TrafficPolicy{
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 13,
				},
			},
		},
		{
			name: "default cluster, non-matching port selector",
			original: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 20,
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 15,
						},
					},
				},
			},
			subset: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
					{
						Port: &networking.PortSelector{
							Number: 8080,
						},
						OutlierDetection: &networking.OutlierDetection{
							ConsecutiveErrors: 13,
						},
					},
				},
			},
			port: &model.Port{Port: 9090},
			expected: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Http: &networking.ConnectionPoolSettings_HTTPSettings{
						MaxRetries: 10,
					},
				},
				OutlierDetection: &networking.OutlierDetection{
					ConsecutiveErrors: 20,
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			policy := MergeSubsetTrafficPolicy(tt.original, tt.subset, tt.port)
			assert.Equal(t, policy, tt.expected)
		})
	}
}

func TestMeshNetworksToEnvoyInternalAddressConfig(t *testing.T) {
	cases := []struct {
		name           string
		networks       *meshconfig.MeshNetworks
		expectedconfig *hcm.HttpConnectionManager_InternalAddressConfig
	}{
		{
			name:           "nil networks",
			expectedconfig: nil,
		},
		{
			name:           "empty networks",
			networks:       &meshconfig.MeshNetworks{},
			expectedconfig: nil,
		},
		{
			name: "networks populated",
			networks: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"default": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
									FromCidr: "192.168.0.0/16",
								},
							},
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
									FromCidr: "172.16.0.0/12",
								},
							},
						},
					},
				},
			},
			expectedconfig: &hcm.HttpConnectionManager_InternalAddressConfig{
				CidrRanges: []*core.CidrRange{
					{
						AddressPrefix: "172.16.0.0",
						PrefixLen:     &wrappers.UInt32Value{Value: 12},
					},
					{
						AddressPrefix: "192.168.0.0",
						PrefixLen:     &wrappers.UInt32Value{Value: 16},
					},
				},
			},
		},
		{
			name: "multi v6",
			networks: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"default": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
									FromCidr: "2001:db8:abcd::/48",
								},
							},
						},
					},
					"other": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
									FromCidr: "2001:db8:def0:1234::/56",
								},
							},
						},
					},
				},
			},
			expectedconfig: &hcm.HttpConnectionManager_InternalAddressConfig{
				CidrRanges: []*core.CidrRange{
					{
						AddressPrefix: "2001:db8:abcd::",
						PrefixLen:     &wrappers.UInt32Value{Value: 48},
					},
					{
						AddressPrefix: "2001:db8:def0:1234::",
						PrefixLen:     &wrappers.UInt32Value{Value: 56},
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := MeshNetworksToEnvoyInternalAddressConfig(tt.networks)
			if !reflect.DeepEqual(tt.expectedconfig, got) {
				t.Errorf("unexpected internal address config, expected: %v, got :%v", tt.expectedconfig, got)
			}
		})
	}
}
