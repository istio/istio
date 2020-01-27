// Copyright 2017 Istio Authors
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

	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
)

var validServiceKeys = map[string]struct {
	service *Service
	labels  labels.Collection
}{
	"example-service1.default|grpc,http|a=b,c=d;e=f": {
		service: &Service{
			Hostname: "example-service1.default",
			Ports:    []*Port{{Name: "http", Port: 80}, {Name: "grpc", Port: 90}}},
		labels: labels.Collection{{"e": "f"}, {"c": "d", "a": "b"}}},
	"my-service": {
		service: &Service{
			Hostname: "my-service",
			Ports:    []*Port{{Name: "", Port: 80}}}},
	"svc.ns": {
		service: &Service{
			Hostname: "svc.ns",
			Ports:    []*Port{{Name: "", Port: 80}}}},
	"svc||istio.io/my_tag-v1.test=my_value-v2.value": {
		service: &Service{
			Hostname: "svc",
			Ports:    []*Port{{Name: "", Port: 80}}},
		labels: labels.Collection{{"istio.io/my_tag-v1.test": "my_value-v2.value"}}},
	"svc|test|prod": {
		service: &Service{
			Hostname: "svc",
			Ports:    []*Port{{Name: "test", Port: 80}}},
		labels: labels.Collection{{"prod": ""}}},
	"svc.default.svc.cluster.local|http-test": {
		service: &Service{
			Hostname: "svc.default.svc.cluster.local",
			Ports:    []*Port{{Name: "http-test", Port: 80}}}},
}

func TestServiceString(t *testing.T) {
	for s, svc := range validServiceKeys {
		if err := svc.service.Validate(); err != nil {
			t.Errorf("Valid service failed validation: %v,  %#v", err, svc.service)
		}
		s1 := ServiceKey(svc.service.Hostname, svc.service.Ports, svc.labels)
		if s1 != s {
			t.Errorf("ServiceKey => Got %s, expected %s", s1, s)
		}
		hostname, ports, l := ParseServiceKey(s)
		if hostname != svc.service.Hostname {
			t.Errorf("ParseServiceKey => Got %s, expected %s for %s", hostname, svc.service.Hostname, s)
		}
		if !compareLabels(l, svc.labels) {
			t.Errorf("ParseServiceKey => Got %#v, expected %#v for %s", l, svc.labels, s)
		}
		if len(ports) != len(svc.service.Ports) {
			t.Errorf("ParseServiceKey => Got %#v, expected %#v for %s", ports, svc.service.Ports, s)
		}
	}
}

// compare two slices of strings as sets
func compare(a, b []string) bool {
	ma := make(map[string]bool)
	mb := make(map[string]bool)
	for _, i := range a {
		ma[i] = true
	}
	for _, i := range b {
		mb[i] = true
	}
	for key := range ma {
		if !mb[key] {
			return false
		}
	}
	for key := range mb {
		if !ma[key] {
			return false
		}
	}

	return true
}

// compareLabels compares sets of labels
func compareLabels(a, b []labels.Instance) bool {
	var as, bs []string
	for _, i := range a {
		as = append(as, i.String())
	}
	for _, j := range b {
		bs = append(bs, j.String())
	}
	return compare(as, bs)
}

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

func TestGetLocality(t *testing.T) {
	cases := []struct {
		name     string
		instance ServiceInstance
		expected string
	}{
		{
			name: "endpoint with locality is overridden by label",
			instance: ServiceInstance{
				Endpoint: &IstioEndpoint{
					Locality: "region/zone/subzone-1",
					Labels: labels.Instance{
						LocalityLabel: "region/zone/subzone-2",
					},
				},
			},
			expected: "region/zone/subzone-2",
		},
		{
			name: "endpoint without label, use registry locality",
			instance: ServiceInstance{
				Endpoint: &IstioEndpoint{
					Locality: "region/zone/subzone-1",
					Labels: labels.Instance{
						LocalityLabel: "",
					},
				},
			},
			expected: "region/zone/subzone-1",
		},
		{
			name: "istio-locality label with k8s label separator",
			instance: ServiceInstance{
				Endpoint: &IstioEndpoint{
					Locality: "",
					Labels: labels.Instance{
						LocalityLabel: "region" + k8sSeparator + "zone" + k8sSeparator + "subzone-2",
					},
				},
			},
			expected: "region/zone/subzone-2",
		},
		{
			name: "istio-locality label with both k8s label separators and slashes",
			instance: ServiceInstance{
				Endpoint: &IstioEndpoint{
					Locality: "",
					Labels: labels.Instance{
						LocalityLabel: "region/zone/subzone.2",
					},
				},
			},
			expected: "region/zone/subzone.2",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.instance.GetLocality()
			if got != testCase.expected {
				t.Errorf("expected locality %s, but got %s", testCase.expected, got)
			}
		})
	}
}

func BenchmarkBuildSubsetKey(b *testing.B) {
	for n := 0; n < b.N; n++ {
		_ = BuildSubsetKey(TrafficDirectionInbound, "v1", "someHost", 80)
	}
}
