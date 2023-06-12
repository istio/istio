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

package net

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestIPsSplitV4V6(t *testing.T) {
	tests := []struct {
		name string
		ips  []string
		ipv4 []string
		ipv6 []string
	}{
		{
			name: "include valid and invalid ip addresses",
			ips:  []string{"1.0.0.0", "256.255.255.255", "localhost", "::1", "2001:db8:::1"},
			ipv4: []string{"1.0.0.0"},
			ipv6: []string{"::1"},
		},
		{
			name: "two ipv4 and one ipv6",
			ips:  []string{"1.0.0.0", "255.255.255.255", "::1"},
			ipv4: []string{"1.0.0.0", "255.255.255.255"},
			ipv6: []string{"::1"},
		},
		{
			name: "two ipv4",
			ips:  []string{"1.0.0.0", "255.255.255.255"},
			ipv4: []string{"1.0.0.0", "255.255.255.255"},
			ipv6: []string{},
		},
		{
			name: "one ipv6",
			ips:  []string{"::1"},
			ipv4: []string{},
			ipv6: []string{"::1"},
		},
		{
			name: "invalid ip addresses",
			ips:  []string{"localhost", "256.255.255.255", "2001:db8:::1"},
			ipv4: []string{},
			ipv6: []string{},
		},
		{
			name: "empty ip address",
			ips:  []string{},
			ipv4: []string{},
			ipv6: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipv4, ipv6 := IPsSplitV4V6(tt.ips)
			if ipv4 == nil {
				ipv4 = []string{}
			}
			if ipv6 == nil {
				ipv6 = []string{}
			}
			if !reflect.DeepEqual(ipv4, tt.ipv4) || !reflect.DeepEqual(ipv6, tt.ipv6) {
				t.Errorf("IPsSplitV4V6() got %v %v, want %v %v", ipv4, ipv6, tt.ipv4, tt.ipv6)
			}
		})
	}
}

func TestParseIPsSplitToV4V6(t *testing.T) {
	tests := []struct {
		name string
		ips  []string
		ipv4 func() []netip.Addr
		ipv6 func() []netip.Addr
	}{
		{
			name: "include valid and invalid ip addresses",
			ips:  []string{"1.0.0.0", "256.255.255.255", "localhost", "::1", "2001:db8:::1"},
			ipv4: func() []netip.Addr {
				ip, _ := netip.ParseAddr("1.0.0.0")
				return []netip.Addr{ip}
			},
			ipv6: func() []netip.Addr {
				ip, _ := netip.ParseAddr("::1")
				return []netip.Addr{ip}
			},
		},
		{
			name: "two ipv4 and one ipv6",
			ips:  []string{"1.0.0.0", "255.255.255.255", "::1"},
			ipv4: func() []netip.Addr {
				ip, _ := netip.ParseAddr("1.0.0.0")
				ip2, _ := netip.ParseAddr("255.255.255.255")
				return []netip.Addr{ip, ip2}
			},
			ipv6: func() []netip.Addr {
				ip, _ := netip.ParseAddr("::1")
				return []netip.Addr{ip}
			},
		},
		{
			name: "two ipv4",
			ips:  []string{"1.0.0.0", "255.255.255.255"},
			ipv4: func() []netip.Addr {
				ip, _ := netip.ParseAddr("1.0.0.0")
				ip2, _ := netip.ParseAddr("255.255.255.255")
				return []netip.Addr{ip, ip2}
			},
			ipv6: func() []netip.Addr {
				return []netip.Addr{}
			},
		},
		{
			name: "one ipv6",
			ips:  []string{"::1"},
			ipv4: func() []netip.Addr {
				return []netip.Addr{}
			},
			ipv6: func() []netip.Addr {
				ip, _ := netip.ParseAddr("::1")
				return []netip.Addr{ip}
			},
		},
		{
			name: "invalid ip addresses",
			ips:  []string{"localhost", "256.255.255.255", "2001:db8:::1"},
			ipv4: func() []netip.Addr {
				return []netip.Addr{}
			},
			ipv6: func() []netip.Addr {
				return []netip.Addr{}
			},
		},
		{
			name: "empty ip address",
			ips:  []string{},
			ipv4: func() []netip.Addr {
				return []netip.Addr{}
			},
			ipv6: func() []netip.Addr {
				return []netip.Addr{}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipv4, ipv6 := ParseIPsSplitToV4V6(tt.ips)
			if ipv4 == nil {
				ipv4 = []netip.Addr{}
			}
			if ipv6 == nil {
				ipv6 = []netip.Addr{}
			}
			if !reflect.DeepEqual(ipv4, tt.ipv4()) || !reflect.DeepEqual(ipv6, tt.ipv6()) {
				t.Errorf("ParseIPsSplitToV4V6() got %v %v, want %v %v", ipv4, ipv6, tt.ipv4(), tt.ipv6())
			}
		})
	}
}
