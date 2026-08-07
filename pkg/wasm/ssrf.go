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

package wasm

import (
	"context"
	"fmt"
	"net"
	"time"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/security"
)

// wasmBlockedIP always blocks link-local addresses (which includes cloud instance metadata
// endpoints such as 169.254.169.254, and never has a legitimate use as a Wasm module host) and
// a small list of well-known cloud metadata addresses that fall outside the link-local range
// (e.g. Alibaba Cloud's and AWS's IPv6 metadata services), plus any additional
// operator-configured ranges from BlockedCIDRsInWasmFetch.
//
// Private (RFC1918/ULA) and loopback ranges are intentionally NOT blocked by default: unlike
// cloud metadata endpoints, they are commonly used to legitimately host Wasm modules from an
// in-cluster registry or HTTP server. Operators who don't need that can lock it down further
// via BLOCKED_CIDRS_IN_WASM_FETCH.
func wasmBlockedIP(ip net.IP) error {
	if security.IsLinkLocal(ip) {
		return fmt.Errorf("connection blocked: resolved IP %s is link-local", ip.String())
	}
	if security.IsKnownCloudMetadataAddress(ip) {
		return fmt.Errorf("connection blocked: resolved IP %s is a known cloud metadata address", ip.String())
	}
	for _, cidr := range features.BlockedCIDRsInWasmFetch {
		if cidr.Contains(ip) {
			return fmt.Errorf("connection blocked: resolved IP %s is in blocked CIDR range %s", ip.String(), cidr.String())
		}
	}
	return nil
}

// wasmDialContext returns a DialContext that applies wasmBlockedIP to every connection made
// while fetching a Wasm module, including redirect hops (see security.BlockedIPDialContext).
// Timeout/KeepAlive match http.DefaultTransport's and go-containerregistry's
// remote.DefaultTransport's dialer settings, so this does not change existing dial behavior
// beyond adding the IP check.
func wasmDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return security.BlockedIPDialContext(&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}, wasmBlockedIP)
}
