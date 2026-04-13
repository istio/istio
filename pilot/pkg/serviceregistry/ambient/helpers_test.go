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

package ambient

import (
	"net/netip"
	"testing"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestParseCidrOrIP(t *testing.T) {
	cases := []struct {
		input   string
		want    netip.Prefix
		wantErr bool
	}{
		{input: "10.0.0.1", want: netip.MustParsePrefix("10.0.0.1/32")},
		{input: "10.0.0.1/32", want: netip.MustParsePrefix("10.0.0.1/32")},
		{input: "10.0.0.0/24", want: netip.MustParsePrefix("10.0.0.0/24")},
		{input: "10.0.0.0/8", want: netip.MustParsePrefix("10.0.0.0/8")},
		// Non-canonical CIDR is masked
		{input: "10.0.0.5/24", want: netip.MustParsePrefix("10.0.0.0/24")},
		{input: "::1", want: netip.MustParsePrefix("::1/128")},
		{input: "::1/128", want: netip.MustParsePrefix("::1/128")},
		{input: "fe80::/10", want: netip.MustParsePrefix("fe80::/10")},
		{input: "invalid", wantErr: true},
		{input: "10.0.0.0/33", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseCidrOrIP(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			if got != tc.want {
				t.Fatalf("parseCidrOrIP(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestToNetworkAddressFromCidr(t *testing.T) {
	cases := []struct {
		name string
		vip  string
		want *workloadapi.NetworkAddress
	}{
		{
			name: "plain IP",
			vip:  "10.0.0.1",
			want: &workloadapi.NetworkAddress{
				Network: "test",
				Address: netip.MustParseAddr("10.0.0.1").AsSlice(),
			},
		},
		{
			name: "single-IP CIDR /32",
			vip:  "10.0.0.1/32",
			want: &workloadapi.NetworkAddress{
				Network: "test",
				Address: netip.MustParseAddr("10.0.0.1").AsSlice(),
			},
		},
		{
			name: "CIDR /24",
			vip:  "10.0.0.0/24",
			want: &workloadapi.NetworkAddress{
				Network: "test",
				Address: netip.MustParseAddr("10.0.0.0").AsSlice(),
				Length:  ptr.Of(uint32(24)),
			},
		},
		{
			name: "IPv6 CIDR",
			vip:  "fe80::/10",
			want: &workloadapi.NetworkAddress{
				Network: "test",
				Address: netip.MustParseAddr("fe80::").AsSlice(),
				Length:  ptr.Of(uint32(10)),
			},
		},
		{
			name: "IPv6 single-IP /128",
			vip:  "::1/128",
			want: &workloadapi.NetworkAddress{
				Network: "test",
				Address: netip.MustParseAddr("::1").AsSlice(),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toNetworkAddressFromCidr(tc.vip, "test")
			assert.NoError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}
