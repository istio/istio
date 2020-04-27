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

func BenchmarkBuildSubsetKey(b *testing.B) {
	for n := 0; n < b.N; n++ {
		_ = BuildSubsetKey(TrafficDirectionInbound, "v1", "someHost", 80)
	}
}
