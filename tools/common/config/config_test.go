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

package config

import (
	"net"
	"net/netip"
	"reflect"
	"testing"
)

var tesrLocalIPAddrs = func(ips []netip.Addr) ([]net.Addr, error) {
	var IPAddrs []net.Addr
	for i := 0; i < len(ips); i++ {
		var ipAddr net.Addr
		ipNetAddr := &net.IPNet{IP: net.ParseIP(ips[i].String())}
		ipAddr = ipNetAddr
		IPAddrs = append(IPAddrs, ipAddr)
	}
	return IPAddrs, nil
}

func TestGetLocalIP(t *testing.T) {
	tests := []struct {
		name     string
		lipas    func() ([]net.Addr, error)
		isDS     bool
		expected bool
	}{
		{
			name: "ipv4 only local ip addresses",
			lipas: func() ([]net.Addr, error) {
				return tesrLocalIPAddrs([]netip.Addr{
					netip.MustParseAddr("127.0.0.1"),
					netip.MustParseAddr("1.2.3.5"),
				})
			},
			isDS:     false,
			expected: false,
		},
		{
			name: "ipv6 only local ip addresses",
			lipas: func() ([]net.Addr, error) {
				return tesrLocalIPAddrs([]netip.Addr{
					netip.MustParseAddr("::1"),
					netip.MustParseAddr("2222:3333::1"),
				})
			},
			isDS:     false,
			expected: true,
		},
		{
			name: "mixed ipv4 and ipv6 local ip addresses",
			lipas: func() ([]net.Addr, error) {
				return tesrLocalIPAddrs([]netip.Addr{
					netip.MustParseAddr("::1"),
					netip.MustParseAddr("127.0.0.1"),
					netip.MustParseAddr("1.2.3.5"),
					netip.MustParseAddr("2222:3333::1"),
				})
			},
			isDS:     false,
			expected: false,
		},

		{
			name: "mixed ipv4 and ipv6 local ip addresses with dual stack enable",
			lipas: func() ([]net.Addr, error) {
				return tesrLocalIPAddrs([]netip.Addr{
					netip.MustParseAddr("::1"),
					netip.MustParseAddr("127.0.0.1"),
					netip.MustParseAddr("1.2.3.5"),
					netip.MustParseAddr("2222:3333::1"),
				})
			},
			isDS:     true,
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			LocalIPAddrs = tt.lipas
			_, isV6, err := getLocalIP(tt.isDS)
			if err != nil {
				t.Errorf("getLocalIP err: %s", err)
			}
			if isV6 != tt.expected {
				t.Errorf("unexpected EnableIPv6 result, expected: %t got: %t", tt.expected, isV6)
			}
		})
	}
}

func TestSeparateV4V6(t *testing.T) {
	mkIPList := func(ips ...string) []netip.Prefix {
		ret := []netip.Prefix{}
		for _, ip := range ips {
			ipp, err := netip.ParsePrefix(ip)
			if err != nil {
				panic(err.Error())
			}
			ret = append(ret, ipp)
		}
		return ret
	}
	cases := []struct {
		name string
		cidr string
		v4   NetworkRange
		v6   NetworkRange
	}{
		{
			name: "wildcard",
			cidr: "*",
			v4:   NetworkRange{IsWildcard: true},
			v6:   NetworkRange{IsWildcard: true},
		},
		{
			name: "v4 only",
			cidr: "10.0.0.0/8,172.16.0.0/16",
			v4:   NetworkRange{CIDRs: mkIPList("10.0.0.0/8", "172.16.0.0/16")},
		},
		{
			name: "v6 only",
			cidr: "fd04:3e42:4a4e:3381::/64,ffff:ffff:ac10:ac10::/64",
			v6:   NetworkRange{CIDRs: mkIPList("fd04:3e42:4a4e:3381::/64", "ffff:ffff:ac10:ac10::/64")},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			v4Range, v6Range, err := SeparateV4V6(tt.cidr)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(v4Range, tt.v4) {
				t.Fatalf("expected %v, got %v", tt.v4, v4Range)
			}
			if !reflect.DeepEqual(v6Range, tt.v6) {
				t.Fatalf("expected %v, got %v", tt.v6, v6Range)
			}
		})
	}
}
