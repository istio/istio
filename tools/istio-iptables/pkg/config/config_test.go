package config

import (
	"net"
	"net/netip"
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
				t.Errorf("unexpected EnableInboundIPv6 result, expected: %t got: %t", tt.expected, isV6)
			}
		})
	}
}
