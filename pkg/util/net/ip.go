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
	"net"
	"net/http"
	"net/netip"

	"istio.io/istio/pkg/log"
)

// IsValidIPAddress Tell whether the given IP address is valid or not
func IsValidIPAddress(ip string) bool {
	ipa, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return ipa.IsValid()
}

// IsIPv6Address returns if ip is IPv6.
func IsIPv6Address(ip string) bool {
	ipa, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return ipa.Is6()
}

// IsIPv4Address returns if ip is IPv4.
func IsIPv4Address(ip string) bool {
	ipa, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return ipa.Is4()
}

// IPsSplitV4V6 returns two slice of ipv4 and ipv6 string slice.
func IPsSplitV4V6(ips []string) (ipv4 []string, ipv6 []string) {
	for _, i := range ips {
		ip, err := netip.ParseAddr(i)
		if err != nil {
			log.Debugf("ignoring un-parsable IP address: %v", err)
			continue
		}
		if ip.Is4() {
			ipv4 = append(ipv4, ip.String())
		} else if ip.Is6() {
			ipv6 = append(ipv6, ip.String())
		} else {
			log.Debugf("ignoring un-parsable IP address: %v", ip)
		}
	}
	return ipv4, ipv6
}

// ParseIPsSplitToV4V6 returns two slice of ipv4 and ipv6 netip.Addr.
func ParseIPsSplitToV4V6(ips []string) (ipv4 []netip.Addr, ipv6 []netip.Addr) {
	for _, i := range ips {
		ip, err := netip.ParseAddr(i)
		if err != nil {
			log.Debugf("ignoring un-parsable IP address: %v", err)
			continue
		}
		if ip.Is4() {
			ipv4 = append(ipv4, ip)
		} else if ip.Is6() {
			ipv6 = append(ipv6, ip)
		} else {
			log.Debugf("ignoring un-parsable IP address: %v", ip)
		}
	}
	return ipv4, ipv6
}

// IsRequestFromLocalhost returns true if request is from localhost address.
func IsRequestFromLocalhost(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}

	userIP := net.ParseIP(ip)
	return userIP.IsLoopback()
}
