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

package nodeagent

import (
	"errors"
	"net"
	"net/netip"
	"reflect"
	"testing"

	"istio.io/istio/cni/pkg/config"
)

func secondaryTestAddr(ip string) net.Addr {
	parsed := net.ParseIP(ip)
	if parsed.To4() != nil {
		return &net.IPNet{IP: parsed, Mask: net.CIDRMask(32, 32)}
	}
	return &net.IPNet{IP: parsed, Mask: net.CIDRMask(128, 128)}
}

func TestFilterSecondaryAddresses(t *testing.T) {
	primary := []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("fd00::1")}
	addrs := []net.Addr{
		secondaryTestAddr("10.0.0.3"),
		secondaryTestAddr("fd00::2"),
		secondaryTestAddr("10.0.0.1"),
		secondaryTestAddr("10.0.0.2"),
		secondaryTestAddr("10.0.0.3"), // duplicate
		secondaryTestAddr("fd00::1"),
		secondaryTestAddr("127.0.0.1"),
		secondaryTestAddr("169.254.1.1"),
		secondaryTestAddr("fe80::1"),
		secondaryTestAddr("224.0.0.1"),
		secondaryTestAddr("0.0.0.0"),
		&net.IPAddr{IP: net.ParseIP("10.0.0.9")}, // not an interface network
	}
	want := []netip.Addr{
		netip.MustParseAddr("10.0.0.2"),
		netip.MustParseAddr("10.0.0.3"),
		netip.MustParseAddr("fd00::2"),
	}
	if got := filterSecondaryAddresses(primary, addrs); !reflect.DeepEqual(got, want) {
		t.Fatalf("filterSecondaryAddresses() = %v, want %v", got, want)
	}
	if got := filterSecondaryAddresses(nil, addrs); got != nil {
		t.Fatalf("without primary pod IPs must not bypass capture, got %v", got)
	}
}

func TestAddSecondaryAddressOverrides(t *testing.T) {
	server := &NetServer{
		podInterfaceAddrs: func() ([]net.Addr, error) {
			return []net.Addr{secondaryTestAddr("10.0.0.1"), secondaryTestAddr("10.0.0.7")}, nil
		},
	}
	cfg := &config.PodLevelOverrides{}
	server.addSecondaryAddressOverrides(cfg, []netip.Addr{netip.MustParseAddr("10.0.0.1")})
	want := []netip.Addr{netip.MustParseAddr("10.0.0.7")}
	if !reflect.DeepEqual(cfg.SecondaryAddresses, want) {
		t.Fatalf("secondary addresses = %v, want %v", cfg.SecondaryAddresses, want)
	}

	server.podInterfaceAddrs = func() ([]net.Addr, error) {
		return nil, errors.New("interfaces unavailable")
	}
	cfg = &config.PodLevelOverrides{}
	server.addSecondaryAddressOverrides(cfg, []netip.Addr{netip.MustParseAddr("10.0.0.1")})
	if len(cfg.SecondaryAddresses) != 0 {
		t.Fatalf("interface lookup failure should not disable inbound capture: %v", cfg.SecondaryAddresses)
	}
}
