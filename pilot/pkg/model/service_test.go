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
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	fuzz "github.com/google/gofuzz"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/visibility"
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

func TestGetLocalityOrDefault(t *testing.T) {
	cases := []struct {
		name         string
		label        string
		defaultLabel string
		expected     string
	}{
		{
			name:         "with label",
			label:        "region/zone/subzone-1",
			defaultLabel: "region/zone/subzone-2",
			expected:     "region/zone/subzone-1",
		},
		{
			name:         "default",
			defaultLabel: "region/zone/subzone-1",
			expected:     "region/zone/subzone-1",
		},
		{
			name:     "label with k8s label separator",
			label:    "region" + k8sSeparator + "zone" + k8sSeparator + "subzone-2",
			expected: "region/zone/subzone-2",
		},
		{
			name:     "label with both k8s label separators and slashes",
			label:    "region/zone/subzone.2",
			expected: "region/zone/subzone.2",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := GetLocalityLabelOrDefault(testCase.label, testCase.defaultLabel)
			if got != testCase.expected {
				t.Errorf("expected locality %s, but got %s", testCase.expected, got)
			}
		})
	}
}

func TestWorkloadInstanceEqual(t *testing.T) {
	exampleInstance := &WorkloadInstance{
		Endpoint: &IstioEndpoint{
			Labels:          labels.Instance{"app": "prod-app"},
			Address:         "an-address",
			ServicePortName: "service-port-name",
			EnvoyEndpoint:   nil,
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
	differingAddr.Endpoint.Address = "another-address"
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
	if !reflect.DeepEqual(originalSvc, copied) {
		cmp.AllowUnexported()
		diff := cmp.Diff(originalSvc, copied, cmp.AllowUnexported(), cmpopts.IgnoreFields(AddressMap{}, "mutex"))
		t.Errorf("unexpected diff %v", diff)
	}
}
