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

import "net/netip"

// IsValidIPAddress Tell whether the given IP address is valid or not
func IsValidIPAddress(ip string) bool {
	ipa, _ := netip.ParseAddr(ip)
	return ipa.IsValid()
}

// IsIPv6Address returns if ip is IPv6.
func IsIPv6Address(ip string) bool {
	ipa, _ := netip.ParseAddr(ip)
	return ipa.Is6()
}

// IsIPv4Address returns if ip is IPv4.
func IsIPv4Address(ip string) bool {
	ipa, _ := netip.ParseAddr(ip)
	return ipa.Is4()
}
