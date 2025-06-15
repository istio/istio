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

package model

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	fuzz "github.com/google/gofuzz"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestGetByPort(t *testing.T) {
	ports := PortList{{
		Name: "http",
		Port: 80,
	}}

	if port, exists := ports.GetByPort(80); !exists || port == nil || port.Name != "http" {
		t.Errorf("GetByPort(80) => want http but got %v, %t", port, exists)
	}
	if port, exists := ports.GetByPort(88); exists || port != nil {
		t.Errorf("GetByPort(88) => want none but got %v, %t", port, exists)
	}
}

func BenchmarkParseSubsetKey(b *testing.B) {
	for n := 0; n < b.N; n++ {
		ParseSubsetKey("outbound|80|v1|example.com")
		ParseSubsetKey("outbound_.8080_.v1_.foo.example.org")
	}
}

func TestParseSubsetKey(t *testing.T) {
	tests := []struct {
		input      string
		direction  TrafficDirection
		subsetName string
		hostname   host.Name
		port       int
	}{
		{"outbound|80|v1|example.com", TrafficDirectionOutbound, "v1", "example.com", 80},
		{"", "", "", "", 0},
		{"|||", "", "", "", 0},
		{"outbound_.8080_.v1_.foo.example.org", TrafficDirectionOutbound, "v1", "foo.example.org", 8080},
		{"inbound_.8080_.v1_.foo.example.org", TrafficDirectionInbound, "v1", "foo.example.org", 8080},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, s, h, p := ParseSubsetKey(tt.input)
			if d != tt.direction {
				t.Errorf("Expected direction %v got %v", tt.direction, d)
			}
			if s != tt.subsetName {
				t.Errorf("Expected subset %v got %v", tt.subsetName, s)
			}
			if h != tt.hostname {
				t.Errorf("Expected hostname %v got %v", tt.hostname, h)
			}
			if p != tt.port {
				t.Errorf("Expected direction %v got %v", tt.port, p)
			}
		})
	}
}

func TestIsValidSubsetKey(t *testing.T) {
	cases := []struct {
		subsetkey string
		expectErr bool
	}{
		{
			subsetkey: "outbound|80|subset|hostname",
			expectErr: false,
		},
		{
			subsetkey: "outbound|80||hostname",
			expectErr: false,
		},
		{
			subsetkey: "outbound|80|subset||hostname",
			expectErr: true,
		},
		{
			subsetkey: "",
			expectErr: true,
		},
	}

	for _, c := range cases {
		err := IsValidSubsetKey(c.subsetkey)
		if !err != c.expectErr {
			t.Errorf("got %v but want %v\n", err, c.expectErr)
		}
	}
}

func TestWorkloadInstanceEqual(t *testing.T) {
	exampleInstance := &WorkloadInstance{
		Endpoint: &IstioEndpoint{
			Labels:          labels.Instance{"app": "prod-app"},
			Addresses:       []string{"an-address"},
			ServicePortName: "service-port-name",
			ServiceAccount:  "service-account",
			Network:         "Network",
			Locality: Locality{
				ClusterID: "cluster-id",
				Label:     "region1/zone1/subzone1",
			},
			EndpointPort: 22,
			LbWeight:     100,
			TLSMode:      "mutual",
		},
	}
	differingAddr := exampleInstance.DeepCopy()
	differingAddr.Endpoint.Addresses = []string{"another-address"}
	differingNetwork := exampleInstance.DeepCopy()
	differingNetwork.Endpoint.Network = "AnotherNetwork"
	differingTLSMode := exampleInstance.DeepCopy()
	differingTLSMode.Endpoint.TLSMode = "permitted"
	differingLabels := exampleInstance.DeepCopy()
	differingLabels.Endpoint.Labels = labels.Instance{
		"app":         "prod-app",
		"another-app": "blah",
	}
	differingServiceAccount := exampleInstance.DeepCopy()
	differingServiceAccount.Endpoint.ServiceAccount = "service-account-two"
	differingLocality := exampleInstance.DeepCopy()
	differingLocality.Endpoint.Locality = Locality{
		ClusterID: "cluster-id-two",
		Label:     "region2/zone2/subzone2",
	}
	differingLbWeight := exampleInstance.DeepCopy()
	differingLbWeight.Endpoint.LbWeight = 0

	cases := []struct {
		comparer *WorkloadInstance
		comparee *WorkloadInstance
		shouldEq bool
		name     string
	}{
		{
			comparer: &WorkloadInstance{},
			comparee: &WorkloadInstance{},
			shouldEq: true,
			name:     "two null endpoints",
		},
		{
			comparer: exampleInstance.DeepCopy(),
			comparee: exampleInstance.DeepCopy(),
			shouldEq: true,
			name:     "exact same endpoints",
		},
		{
			comparer: exampleInstance.DeepCopy(),
			comparee: differingAddr.DeepCopy(),
			shouldEq: false,
			name:     "different Addresses",
		},
		{
			comparer: exampleInstance.DeepCopy(),
			comparee: differingNetwork.DeepCopy(),
			shouldEq: false,
			name:     "different Network",
		},
		{
			comparer: exampleInstance.DeepCopy(),
			comparee: differingTLSMode.DeepCopy(),
			shouldEq: false,
			name:     "different TLS Mode",
		},
		{
			comparer: exampleInstance.DeepCopy(),
			comparee: differingLabels.DeepCopy(),
			shouldEq: false,
			name:     "different Labels",
		},
		{
			comparer: exampleInstance.DeepCopy(),
			comparee: differingServiceAccount.DeepCopy(),
			shouldEq: false,
			name:     "different Service Account",
		},
		{
			comparer: exampleInstance.DeepCopy(),
			comparee: differingLocality.DeepCopy(),
			shouldEq: false,
			name:     "different Locality",
		},
		{
			comparer: exampleInstance.DeepCopy(),
			comparee: differingLbWeight.DeepCopy(),
			shouldEq: false,
			name:     "different LbWeight",
		},
	}

	for _, testCase := range cases {
		t.Run("WorkloadInstancesEqual: "+testCase.name, func(t *testing.T) {
			isEq := WorkloadInstancesEqual(testCase.comparer, testCase.comparee)
			isEqReverse := WorkloadInstancesEqual(testCase.comparee, testCase.comparer)

			if isEq != isEqReverse {
				t.Errorf(
					"returned different for reversing arguments for structs: %v , and %v",
					testCase.comparer,
					testCase.comparee,
				)
			}
			if isEq != testCase.shouldEq {
				t.Errorf(
					"equality of %v , and %v do not equal expected %t",
					testCase.comparer,
					testCase.comparee,
					testCase.shouldEq,
				)
			}
		})
	}
}

func TestServicesEqual(t *testing.T) {
	cases := []struct {
		first    *Service
		other    *Service
		shouldEq bool
		name     string
	}{
		{
			first:    nil,
			other:    &Service{},
			shouldEq: false,
			name:     "first nil services",
		},
		{
			first:    &Service{},
			other:    nil,
			shouldEq: false,
			name:     "other nil services",
		},
		{
			first:    nil,
			other:    nil,
			shouldEq: true,
			name:     "both nil services",
		},
		{
			first:    &Service{},
			other:    &Service{},
			shouldEq: true,
			name:     "two empty services",
		},
		{
			first: &Service{
				Ports: port7000,
			},
			other: &Service{
				Ports: port7442,
			},
			shouldEq: false,
			name:     "different ports",
		},
		{
			first: &Service{
				Ports: twoMatchingPorts,
			},
			other: &Service{
				Ports: twoMatchingPorts,
			},
			shouldEq: true,
			name:     "matching ports",
		},
		{
			first: &Service{
				ServiceAccounts: []string{"sa1"},
			},
			other: &Service{
				ServiceAccounts: []string{"sa1"},
			},
			shouldEq: true,
			name:     "matching service accounts",
		},
		{
			first: &Service{
				ServiceAccounts: []string{"sa1"},
			},
			other: &Service{
				ServiceAccounts: []string{"sa2"},
			},
			shouldEq: false,
			name:     "different service accounts",
		},
		{
			first: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cluster-1": {"c1-vip1,c1-vip2"},
						"cluster-2": {"c2-vip1,c2-vip2"},
					},
				},
			},
			other: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cluster-1": {"c1-vip1,c1-vip2"},
						"cluster-2": {"c2-vip1,c2-vip2"},
					},
				},
			},
			shouldEq: true,
			name:     "matching cluster VIPs",
		},
		{
			first: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cluster-1": {"c1-vip1,c1-vip2"},
					},
				},
			},
			other: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cluster-1": {"c1-vip1,c1-vip2"},
						"cluster-2": {"c2-vip1,c2-vip2"},
					},
				},
			},
			shouldEq: false,
			name:     "different cluster VIPs",
		},
		{
			first: &Service{
				Attributes: ServiceAttributes{
					Name:      "test",
					Namespace: "testns",
					Labels: map[string]string{
						"label-1": "value-1",
					},
				},
			},
			other: &Service{
				Attributes: ServiceAttributes{
					Name:      "test",
					Namespace: "testns",
					Labels: map[string]string{
						"label-1": "value-1",
					},
				},
			},
			shouldEq: true,
			name:     "same service attributes",
		},
		{
			first: &Service{
				Attributes: ServiceAttributes{
					Name:      "test",
					Namespace: "testns",
					Labels: map[string]string{
						"label-1": "value-1",
					},
				},
			},
			other: &Service{
				Attributes: ServiceAttributes{
					Name:      "test",
					Namespace: "testns",
					Labels: map[string]string{
						"label-1": "value-2",
					},
				},
			},
			shouldEq: false,
			name:     "different service attributes",
		},
		{
			first: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cluster-1": {"c1-vip1,c1-vip2"},
					},
				},
				ServiceAccounts: []string{"sa-1", "sa-2"},
				Ports:           twoMatchingPorts,
				Attributes: ServiceAttributes{
					Name:      "test",
					Namespace: "testns",
					Labels: map[string]string{
						"label-1": "value-1",
					},
				},
			},
			other: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cluster-1": {"c1-vip1,c1-vip2"},
						"cluster-2": {"c2-vip1,c2-vip2"},
					},
				},
				Ports:           twoMatchingPorts,
				ServiceAccounts: []string{"sa-1", "sa-2"},
				Attributes: ServiceAttributes{
					Name:      "test",
					Namespace: "testns",
					Labels: map[string]string{
						"label-1": "value-2",
					},
				},
			},
			shouldEq: false,
			name:     "service with just label change",
		},
		{
			first: &Service{
				Attributes: ServiceAttributes{
					K8sAttributes: K8sAttributes{
						Type: "ClusterIP",
					},
				},
			},
			other: &Service{
				Attributes: ServiceAttributes{
					K8sAttributes: K8sAttributes{
						Type: "NodePort",
					},
				},
			},
			shouldEq: false,
			name:     "different types",
		},
		{
			first: &Service{
				Attributes: ServiceAttributes{
					K8sAttributes: K8sAttributes{
						ExternalName: "foo.com",
					},
				},
			},
			other: &Service{
				Attributes: ServiceAttributes{
					K8sAttributes: K8sAttributes{
						ExternalName: "bar.com",
					},
				},
			},
			shouldEq: false,
			name:     "different external names",
		},
		{
			first: &Service{
				Attributes: ServiceAttributes{
					K8sAttributes: K8sAttributes{
						NodeLocal: false,
					},
				},
			},
			other: &Service{
				Attributes: ServiceAttributes{
					K8sAttributes: K8sAttributes{
						NodeLocal: true,
					},
				},
			},
			shouldEq: false,
			name:     "different internal traffic policies",
		},
		{
			first:    &Service{Hostname: host.Name("foo.com")},
			other:    &Service{Hostname: host.Name("foo1.com")},
			shouldEq: false,
			name:     "different hostname",
		},
		{
			first:    &Service{DefaultAddress: constants.UnspecifiedIPv6},
			other:    &Service{DefaultAddress: constants.UnspecifiedIP},
			shouldEq: false,
			name:     "different default address",
		},
		{
			first:    &Service{AutoAllocatedIPv4Address: "240.240.0.100"},
			other:    &Service{AutoAllocatedIPv4Address: "240.240.0.101"},
			shouldEq: false,
			name:     "different auto allocated IPv4 addresses",
		},
		{
			first:    &Service{AutoAllocatedIPv6Address: "2001:2::f0f0:e351"},
			other:    &Service{AutoAllocatedIPv6Address: "2001:2::f0f1:e351"},
			shouldEq: false,
			name:     "different auto allocated IPv6 addresses",
		},
		{
			first:    &Service{Resolution: ClientSideLB},
			other:    &Service{Resolution: Passthrough},
			shouldEq: false,
			name:     "different resolution",
		},
		{
			first:    &Service{MeshExternal: true},
			other:    &Service{MeshExternal: false},
			shouldEq: false,
			name:     "different mesh external setting",
		},
	}

	for _, testCase := range cases {
		t.Run("ServicesEqual: "+testCase.name, func(t *testing.T) {
			isEq := testCase.first.Equals(testCase.other)
			if isEq != testCase.shouldEq {
				t.Errorf(
					"equality of %v , and %v are not equal expected %t",
					testCase.first,
					testCase.other,
					testCase.shouldEq,
				)
			}
		})
	}
}

func BenchmarkBuildSubsetKey(b *testing.B) {
	for n := 0; n < b.N; n++ {
		_ = BuildSubsetKey(TrafficDirectionInbound, "v1", "someHost", 80)
	}
}

func BenchmarkServiceDeepCopy(b *testing.B) {
	svc1 := buildHTTPService("test.com", visibility.Public, "10.10.0.1", "default", 80, 8080, 9090, 9999)
	svc1.ServiceAccounts = []string{"sa1"}
	svc1.ClusterVIPs = AddressMap{
		Addresses: map[cluster.ID][]string{
			"cluster1": {"10.10.0.1"},
			"cluster2": {"10.10.0.2"},
		},
	}
	for n := 0; n < b.N; n++ {
		_ = svc1.DeepCopy()
	}
}

func TestFuzzServiceDeepCopy(t *testing.T) {
	fuzzer := fuzz.New()
	originalSvc := &Service{}
	fuzzer.Fuzz(originalSvc)
	copied := originalSvc.DeepCopy()
	opts := []cmp.Option{cmp.AllowUnexported(), cmpopts.IgnoreFields(AddressMap{}, "mutex")}
	if !cmp.Equal(originalSvc, copied, opts...) {
		diff := cmp.Diff(originalSvc, copied, opts...)
		t.Errorf("unexpected diff %v", diff)
	}
}

func TestParseSubsetKeyHostname(t *testing.T) {
	tests := []struct {
		in, out string
	}{
		{"outbound|80|subset|host.com", "host.com"},
		{"outbound|80|subset|", ""},
		{"|||", ""},
		{"||||||", ""},
		{"", ""},
		{"outbound_.80_._.test.local", "test.local"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, ParseSubsetKeyHostname(tt.in), tt.out)
		})
	}
}

func TestGetAllAddresses(t *testing.T) {
	tests := []struct {
		name                   string
		service                *Service
		ipMode                 IPMode
		dualStackEnabled       bool
		ambientEnabled         bool
		autoAllocationEnabled  bool
		expectedAddresses      []string
		expectedExtraAddresses []string
	}{
		{
			name: "IPv4 mode, IPv4 and IPv6 CIDR addresses, expected to return only IPv4 addresses",
			service: &Service{
				DefaultAddress: "10.0.0.0/28",
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"id": {"10.0.0.0/28", "10.0.0.16/28", "::ffff:10.0.0.32/96", "::ffff:10.0.0.48/96"},
					},
				},
			},
			ipMode:                 IPv4,
			expectedAddresses:      []string{"10.0.0.0/28", "10.0.0.16/28"},
			expectedExtraAddresses: []string{"10.0.0.16/28"},
		},
		{
			name: "IPv6 mode, IPv4 and IPv6 CIDR addresses, expected to return only IPv6 addresses",
			service: &Service{
				DefaultAddress: "10.0.0.0/28",
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"id": {"10.0.0.0/28", "10.0.0.16/28", "::ffff:10.0.0.32/96", "::ffff:10.0.0.48/96"},
					},
				},
			},
			ipMode:                 IPv6,
			expectedAddresses:      []string{"::ffff:10.0.0.32/96", "::ffff:10.0.0.48/96"},
			expectedExtraAddresses: []string{"::ffff:10.0.0.48/96"},
		},
		{
			name: "dual mode, ISTIO_DUAL_STACK disabled, IPv4 and IPv6 addresses, expected to return only IPv4 addresses",
			service: &Service{
				DefaultAddress: "10.0.0.0",
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"id": {"10.0.0.0", "10.0.0.16", "::ffff:10.0.0.32", "::ffff:10.0.0.48"},
					},
				},
			},
			ipMode:                 Dual,
			expectedAddresses:      []string{"10.0.0.0", "10.0.0.16"},
			expectedExtraAddresses: []string{"10.0.0.16"},
		},
		{
			name: "dual mode, ISTIO_DUAL_STACK enabled, IPv4 and IPv6 addresses, expected to return all addresses",
			service: &Service{
				DefaultAddress: "10.0.0.0",
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"id": {"10.0.0.0", "10.0.0.16", "::ffff:10.0.0.32", "::ffff:10.0.0.48"},
					},
				},
			},
			ipMode:                 Dual,
			dualStackEnabled:       true,
			expectedAddresses:      []string{"10.0.0.0", "10.0.0.16", "::ffff:10.0.0.32", "::ffff:10.0.0.48"},
			expectedExtraAddresses: []string{"10.0.0.16", "::ffff:10.0.0.32", "::ffff:10.0.0.48"},
		},
		{
			name: "IPv4 mode, ISTIO_DUAL_STACK disabled, ambient enabled, IPv4 and IPv6 addresses, expected to return only IPv4 addresses",
			service: &Service{
				DefaultAddress: "10.0.0.0/28",
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"id": {"10.0.0.0/28", "10.0.0.16/28", "::ffff:10.0.0.32", "::ffff:10.0.0.48"},
					},
				},
			},
			ipMode:                 IPv4,
			ambientEnabled:         true,
			expectedAddresses:      []string{"10.0.0.0/28", "10.0.0.16/28"},
			expectedExtraAddresses: []string{"10.0.0.16/28"},
		},
		{
			name: "IPv6 mode, ISTIO_DUAL_STACK disabled, ambient enabled, IPv4 and IPv6 addresses, expected to return only IPv6 addresses",
			service: &Service{
				DefaultAddress: "10.0.0.0/28",
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"id": {"10.0.0.0/28", "10.0.0.16/28", "::ffff:10.0.0.32", "::ffff:10.0.0.48"},
					},
				},
			},
			ipMode:                 IPv6,
			ambientEnabled:         true,
			expectedAddresses:      []string{"::ffff:10.0.0.32", "::ffff:10.0.0.48"},
			expectedExtraAddresses: []string{"::ffff:10.0.0.48"},
		},
		{
			name: "IPv4 mode, auto-allocation enabled, expected auto-allocated address",
			service: &Service{
				DefaultAddress:           "0.0.0.0",
				AutoAllocatedIPv4Address: "240.240.0.1",
			},
			ipMode:                 IPv4,
			autoAllocationEnabled:  true,
			expectedAddresses:      []string{"240.240.0.1"},
			expectedExtraAddresses: []string{},
		},
		{
			name: "IPv6 mode, auto-allocation enabled, expected auto-allocated address",
			service: &Service{
				DefaultAddress:           "0.0.0.0",
				AutoAllocatedIPv6Address: "2001:2::f0f0:e351",
			},
			ipMode:                 IPv6,
			autoAllocationEnabled:  true,
			expectedAddresses:      []string{"2001:2::f0f0:e351"},
			expectedExtraAddresses: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dualStackEnabled {
				test.SetForTest(t, &features.EnableDualStack, true)
			}
			if tc.ambientEnabled {
				test.SetForTest(t, &features.EnableAmbient, true)
			}
			proxy := &Proxy{Metadata: &NodeMetadata{ClusterID: "id"}, ipMode: tc.ipMode}
			if tc.autoAllocationEnabled {
				proxy.Metadata.DNSCapture = true
				proxy.Metadata.DNSAutoAllocate = true
			}
			addresses := tc.service.GetAllAddressesForProxy(proxy)
			assert.Equal(t, addresses, tc.expectedAddresses)
			extraAddresses := tc.service.GetExtraAddressesForProxy(proxy)
			assert.Equal(t, extraAddresses, tc.expectedExtraAddresses)
		})
	}
}

func TestGetAddressForProxy(t *testing.T) {
	tests := []struct {
		name            string
		service         *Service
		proxy           *Proxy
		expectedAddress string
	}{
		{
			name: "IPv4 mode with IPv4 addresses, expected to return the first IPv4 address",
			service: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cl1": {"10.0.0.1", "10.0.0.2"},
					},
				},
			},
			proxy: &Proxy{
				Metadata: &NodeMetadata{
					ClusterID: "cl1",
				},
				ipMode: IPv4,
			},
			expectedAddress: "10.0.0.1",
		},
		{
			name: "IPv6 mode with IPv6 addresses, expected to return the first IPv6 address",
			service: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cl1": {"2001:db8:abcd::1", "2001:db8:abcd::2"},
					},
				},
			},
			proxy: &Proxy{
				Metadata: &NodeMetadata{
					ClusterID: "cl1",
				},
				ipMode: IPv6,
			},
			expectedAddress: "2001:db8:abcd::1",
		},
		{
			name: "Dual mode with both IPv6 and IPv4 addresses, expected to return the IPv6 address",
			service: &Service{
				ClusterVIPs: AddressMap{
					Addresses: map[cluster.ID][]string{
						"cl1": {"2001:db8:abcd::1", "10.0.0.1"},
					},
				},
			},
			proxy: &Proxy{
				Metadata: &NodeMetadata{
					ClusterID: "cl1",
				},
				ipMode: Dual,
			},
			expectedAddress: "2001:db8:abcd::1",
		},
		{
			name: "IPv4 mode with Auto-allocated IPv4 address",
			service: &Service{
				DefaultAddress:           constants.UnspecifiedIP,
				AutoAllocatedIPv4Address: "240.240.0.1",
			},
			proxy: &Proxy{
				Metadata: &NodeMetadata{
					DNSAutoAllocate: true,
					DNSCapture:      true,
				},
				ipMode: IPv4,
			},
			expectedAddress: "240.240.0.1",
		},
		{
			name: "IPv6 mode with Auto-allocated IPv6 address",
			service: &Service{
				DefaultAddress:           constants.UnspecifiedIP,
				AutoAllocatedIPv6Address: "2001:2::f0f0:e351",
			},
			proxy: &Proxy{
				Metadata: &NodeMetadata{
					DNSAutoAllocate: true,
					DNSCapture:      true,
				},
				ipMode: IPv6,
			},
			expectedAddress: "2001:2::f0f0:e351",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.service.GetAddressForProxy(tt.proxy)
			assert.Equal(t, result, tt.expectedAddress)
		})
	}
}

func TestWaypointKeyForProxy(t *testing.T) {
	tests := []struct {
		name              string
		proxy             *Proxy
		externalAddresses bool
		expectedKey       WaypointKey
	}{
		{
			name: "single service target with internal addresses",
			proxy: &Proxy{
				ConfigNamespace: "default",
				Metadata: &NodeMetadata{
					Network:   network.ID("network1"),
					ClusterID: "cluster1",
				},
				ServiceTargets: []ServiceTarget{
					{
						Service: &Service{
							Hostname: host.Name("service1.default.svc.cluster.local"),
							ClusterVIPs: AddressMap{
								Addresses: map[cluster.ID][]string{
									"cluster1": {"10.0.0.1"},
								},
							},
						},
					},
				},
			},
			expectedKey: WaypointKey{
				Namespace: "default",
				Network:   "network1",
				Hostnames: []string{"service1.default.svc.cluster.local"},
				Addresses: []string{"10.0.0.1"},
			},
		},
		{
			name: "single service target with external addresses",
			proxy: &Proxy{
				ConfigNamespace: "default",
				Metadata: &NodeMetadata{
					Network:   network.ID("network1"),
					ClusterID: "cluster1",
				},
				ServiceTargets: []ServiceTarget{
					{
						Service: &Service{
							Hostname: host.Name("service1.default.svc.cluster.local"),
							Attributes: ServiceAttributes{
								ClusterExternalAddresses: &AddressMap{
									Addresses: map[cluster.ID][]string{
										"cluster1": {"192.168.0.1"},
									},
								},
								Labels: map[string]string{
									"istio.io/global": "true",
								},
							},
						},
					},
				},
			},
			externalAddresses: true,
			expectedKey: WaypointKey{
				Namespace: "default",
				Network:   "network1",
				Hostnames: []string{"service1.default.svc.cluster.local"},
				Addresses: []string{"192.168.0.1"},
				IsGateway: true,
			},
		},
		{
			name: "service target with auto-allocated addresses",
			proxy: &Proxy{
				ConfigNamespace: "default",
				Metadata: &NodeMetadata{
					Network:   network.ID("network1"),
					ClusterID: "cluster1",
				},
				ServiceTargets: []ServiceTarget{
					{
						Service: &Service{
							Hostname:                 host.Name("service1.default.svc.cluster.local"),
							AutoAllocatedIPv4Address: "240.240.0.1",
							AutoAllocatedIPv6Address: "2001:2::f0f0:e351",
						},
					},
				},
			},
			externalAddresses: false,
			expectedKey: WaypointKey{
				Namespace: "default",
				Network:   "network1",
				Hostnames: []string{"service1.default.svc.cluster.local"},
				Addresses: []string{"240.240.0.1", "2001:2::f0f0:e351"},
			},
		},
		{
			name: "multiple service targets",
			proxy: &Proxy{
				ConfigNamespace: "default",
				Metadata: &NodeMetadata{
					Network:   network.ID("network1"),
					ClusterID: "cluster1",
				},
				ServiceTargets: []ServiceTarget{
					{
						Service: &Service{
							Hostname: host.Name("service1.default.svc.cluster.local"),
							ClusterVIPs: AddressMap{
								Addresses: map[cluster.ID][]string{
									"cluster1": {"10.0.0.1"},
								},
							},
						},
					},
					{
						Service: &Service{
							Hostname: host.Name("service2.default.svc.cluster.local"),
							ClusterVIPs: AddressMap{
								Addresses: map[cluster.ID][]string{
									"cluster1": {"10.0.0.2"},
								},
							},
						},
					},
				},
			},
			externalAddresses: false,
			expectedKey: WaypointKey{
				Namespace: "default",
				Network:   "network1",
				Hostnames: []string{"service1.default.svc.cluster.local", "service2.default.svc.cluster.local"},
				Addresses: []string{"10.0.0.1", "10.0.0.2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := waypointKeyForProxy(tt.proxy, tt.externalAddresses)
			if !cmp.Equal(key, tt.expectedKey) {
				t.Errorf("waypointKeyForProxy() = %v, want %v", key, tt.expectedKey)
			}
		})
	}
}

func BenchmarkEndpointDeepCopy(b *testing.B) {
	ep := &IstioEndpoint{
		Labels:          labels.Instance{"label-foo": "aaa", "label-bar": "bbb"},
		Addresses:       []string{"address-foo", "address-bar"},
		ServicePortName: "service-port-name",
		ServiceAccount:  "service-account",
		Network:         "Network",
		Locality: Locality{
			ClusterID: "cluster-id",
			Label:     "region1/zone1/subzone1",
		},
		EndpointPort: 22,
		LbWeight:     100,
		TLSMode:      "mutual",
		Namespace:    "namespace",
		WorkloadName: "workload-name",
		HostName:     "foo-pod.svc",
		SubDomain:    "subdomain",
		HealthStatus: Healthy,
		NodeName:     "node1",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ep.DeepCopy()
	}
}
