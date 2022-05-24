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

package v1alpha3

import (
	"net"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
)

const (
	// WildcardAddress binds to all IP addresses
	WildcardAddress = "0.0.0.0"

	// WildcardIPv6Address binds to all IPv6 addresses
	WildcardIPv6Address = "::"

	// LocalhostAddress for local binding
	LocalhostAddress = "127.0.0.1"

	// LocalhostIPv6Address for local binding
	LocalhostIPv6Address = "::1"

	// 6 is the magical number for inbound: 15006, 127.0.0.6, ::6
	InboundPassthroughBindIpv4 = "127.0.0.6"
	InboundPassthroughBindIpv6 = "::6"
)

// getActualWildcardAndLocalHostWithDualStack will return corresponding Wildcard and LocalHost
// depending on value of proxy's IPAddresses. This function checks each element
// and if there is at least one ipv4 address other than 127.0.0.1, it will use ipv4 address,
// if all addresses are ipv6  addresses then ipv6 address will be used to get wildcard and local host address.
func getActualWildcardAndLocalHostWithDualStack(node *model.Proxy) [][2]string {
	result := make([][2]string, 0, 2)
	if node.SupportsIPv4() {
		result = append(result, [2]string{WildcardAddress, LocalhostAddress})
	}

	if node.SupportsIPv6() {
		result = append(result, [2]string{WildcardIPv6Address, LocalhostIPv6Address})
	}

	if features.EnableDualStack && len(result) != 2 {
		log.Errorf("dual stack mode enabled, but proxy cannot bind to both ipv4 and ipv6 addresses")
		return [][2]string{}
	}
	return result
}

func getActualWildcardAndLocalHostFromIP(addr string) (string, string) {
	if net.ParseIP(addr) == nil || net.ParseIP(addr).To4() != nil {
		return WildcardAddress, LocalhostAddress
	}
	return WildcardIPv6Address, LocalhostIPv6Address
}

func getPassthroughBindIP(node *model.Proxy) string {
	if node.SupportsIPv4() {
		return InboundPassthroughBindIpv4
	}
	return InboundPassthroughBindIpv6
}

// getSidecarInboundBindIPWithDualStack returns the IP that the proxy can bind to along with the sidecar specified port.
// It looks for an unicast address, if none found, then the default wildcard address is used.
// This will make the inbound listener bind to instance_ip:port instead of 0.0.0.0:port where applicable.
func getSidecarInboundBindIPWithDualStack(node *model.Proxy) []string {
	// Return the IP if its a global unicast address.
	result := make([]string, 0, 2)
	if len(node.GlobalUnicastIP) > 0 {
		result = append(result, node.GlobalUnicastIP)
		return result
	}
	if node.SupportsIPv4() {
		result = append(result, WildcardAddress)
	}
	if node.SupportsIPv6() {
		result = append(result, WildcardIPv6Address)
	}
	return result
}
