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
	"net/http/httptest"
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

func TestIsValidIPAddress(t *testing.T) {
	type args struct {
		ip string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "test is a valid ipv4 address",
			args: args{
				ip: "10.10.1.1",
			},
			want: true,
		},
		{
			name: "test not a valid ipv4 address",
			args: args{
				ip: "188.188.188.1888",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidIPAddress(tt.args.ip); got != tt.want {
				t.Errorf("IsValidIPAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsIPv6Address(t *testing.T) {
	type args struct {
		ip string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "test is a valid ipv6 address",
			args: args{
				ip: "::1",
			},
			want: true,
		},
		{
			name: "test not a valid ipv6 address",
			args: args{
				ip: "2001:db8:::1",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsIPv6Address(tt.args.ip); got != tt.want {
				t.Errorf("IsIPv6Address() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsIPv4Address(t *testing.T) {
	type args struct {
		ip string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "test is a ipv4 address",
			args: args{
				ip: "10.0.0.1",
			},
			want: true,
		},
		{
			name: "test not a ipv4 address",
			args: args{
				ip: "188.188.188.1888",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsIPv4Address(tt.args.ip); got != tt.want {
				t.Errorf("IsIPv4Address() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPsSplitV4V61(t *testing.T) {
	type args struct {
		ips []string
	}
	tests := []struct {
		name     string
		args     args
		wantIpv4 []string
		wantIpv6 []string
	}{
		{
			name: "test correctly split ipv4 and ipv6 addresses",
			args: args{
				ips: []string{
					"invalid addr",
					"10.1.0.1",
					"172.28.19.1",
					"100.0.0.0.0",
					"188.0.188.1888",
					"::1",
					":1:1:1",
					"2001:db8:::1",
					"2001:db8:::100",
				},
			},
			wantIpv4: []string{
				"10.1.0.1",
				"172.28.19.1",
			},
			wantIpv6: []string{
				"::1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIpv4, gotIpv6 := IPsSplitV4V6(tt.args.ips)
			if !reflect.DeepEqual(gotIpv4, tt.wantIpv4) {
				t.Errorf("IPsSplitV4V6() gotIpv4 = %v, want %v", gotIpv4, tt.wantIpv4)
			}
			if !reflect.DeepEqual(gotIpv6, tt.wantIpv6) {
				t.Errorf("IPsSplitV4V6() gotIpv6 = %v, want %v", gotIpv6, tt.wantIpv6)
			}
		})
	}
}

func TestParseIPsSplitToV4V61(t *testing.T) {
	type args struct {
		ips []string
	}
	tests := []struct {
		name     string
		args     args
		wantIpv4 []netip.Addr
		wantIpv6 []netip.Addr
	}{
		{
			name: "test correctly parse ipv4 and ipv6 addresses",
			args: args{
				ips: []string{
					"invalid addr",
					"10.1.0.1",
					"172.28.19.1",
					"100.0.0.0.0",
					"188.0.188.1888",
					"::1",
					":1:1:1",
					"2001:db8:::1",
					"2001:db8:::100",
				},
			},
			wantIpv4: []netip.Addr{
				netip.MustParseAddr("10.1.0.1"),
				netip.MustParseAddr("172.28.19.1"),
			},
			wantIpv6: []netip.Addr{
				netip.MustParseAddr("::1"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIpv4, gotIpv6 := ParseIPsSplitToV4V6(tt.args.ips)
			if !reflect.DeepEqual(gotIpv4, tt.wantIpv4) {
				t.Errorf("ParseIPsSplitToV4V6() gotIpv4 = %v, want %v", gotIpv4, tt.wantIpv4)
			}
			if !reflect.DeepEqual(gotIpv6, tt.wantIpv6) {
				t.Errorf("ParseIPsSplitToV4V6() gotIpv6 = %v, want %v", gotIpv6, tt.wantIpv6)
			}
		})
	}
}

func TestIsRequestFromLocalhost(t *testing.T) {
	testCases := []struct {
		name       string
		remoteAddr string
		expected   bool
	}{
		{
			name:       "Localhost IPv4",
			remoteAddr: "127.0.0.1:8080",
			expected:   true,
		},
		{
			name:       "Localhost IPv6",
			remoteAddr: "[::1]:8080",
			expected:   true,
		},
		{
			name:       "Private IPv4",
			remoteAddr: "192.168.1.100:8080",
			expected:   false,
		},
		{
			name:       "Public IPv4",
			remoteAddr: "8.8.8.8:8080",
			expected:   false,
		},
		{
			name:       "Invalid Remote Address",
			remoteAddr: "invalid",
			expected:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr

			result := IsRequestFromLocalhost(req)
			if result != tc.expected {
				t.Errorf("IsRequestFromLocalhost expected %t for %s, but got %t", tc.expected, tc.remoteAddr, result)
			}
		})
	}
}
