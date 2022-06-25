// Copyright 2017 Istio Authors
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

package authenticate

import (
	"net"
	"strings"

	"google.golang.org/grpc/peer"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/security"
)

// CidrAuthenticator is delegated authenticator that validates whether
// caller is in the trusted Cidr range and hence trust the information
// passed.
type CidrAuthenticator struct{}

var _ security.DelegatedAuthenticator = &CidrAuthenticator{}

// CanTrustedCaller determines if the caller is in trusted cidr range.
func (c *CidrAuthenticator) CanTrustCaller(ctx *security.AuthContext) bool {
	peerInfo, _ := peer.FromContext(ctx.RequestContext)
	return isTrustedAddress(peerInfo.Addr.String())
}

func isTrustedAddress(addr string) bool {
	if len(features.TrustedCIDRRanges) > 0 {
		cidrs := strings.Split(features.TrustedCIDRRanges, ",")
		for _, cidr := range cidrs {
			if isInRange(addr, cidr) {
				return true
			}
		}
	}
	// Always trust local host addresses.
	if net.ParseIP(addr).IsLoopback() {
		return true
	}
	return false
}

func isInRange(addr, cidr string) bool {
	if strings.Contains(cidr, "/") {
		ip, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return false
		}
		if ip.To4() == nil && ip.To16() == nil {
			return false
		}
		return ipnet.Contains(net.ParseIP(addr))
	}
	return false
}
