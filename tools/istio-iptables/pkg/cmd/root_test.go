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

package cmd

import (
	"net"
	"testing"
)

var tesrLocalIPAddrs = func(ips []net.IP) ([]net.Addr, error) {
	var IPAddrs []net.Addr
	for i := 0; i < len(ips); i++ {
		var ipAddr net.Addr
		ipNetAddr := &net.IPNet{IP: ips[i]}
		ipAddr = ipNetAddr
		IPAddrs = append(IPAddrs, ipAddr)
	}
	return IPAddrs, nil
}

func TestGetLocalIP(t *testing.T) {
	tests := []struct {
		name     string
		lipas    func() ([]net.Addr, error)
		expected bool
	}{
		{
			name: "ipv4 only local ip addresses",
			lipas: func() ([]net.Addr, error) {
				return tesrLocalIPAddrs([]net.IP{
					net.ParseIP("127.0.0.1"),
					net.ParseIP("1.2.3.5"),
				})
			},
			expected: false,
		},
		{
			name: "ipv6 only local ip addresses",
			lipas: func() ([]net.Addr, error) {
				return tesrLocalIPAddrs([]net.IP{
					net.ParseIP("::1"),
					net.ParseIP("2222:3333::1"),
				})
			},
			expected: true,
		},
		{
			name: "mixed ipv4 and ipv6 local ip addresses",
			lipas: func() ([]net.Addr, error) {
				return tesrLocalIPAddrs([]net.IP{
					net.ParseIP("::1"),
					net.ParseIP("127.0.0.1"),
					net.ParseIP("1.2.3.5"),
					net.ParseIP("2222:3333::1"),
				})
			},
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			LocalIPAddrs = tt.lipas
			result := constructConfig()
			if result.EnableInboundIPv6 != tt.expected {
				t.Errorf("unexpected EnableInboundIPv6 result, expected: %t got: %t", tt.expected, result.EnableInboundIPv6)
			}
		})
	}
}
