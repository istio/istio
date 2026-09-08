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

package security

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

// BlockedIPDialContext returns a DialContext (suitable for http.Transport.DialContext) that
// rejects a connection when isBlocked returns a non-nil error for the address's resolved IP.
//
// The check is implemented via the dialer's Control callback, which fires after DNS resolution
// but before the socket connects. This means the block applies uniformly to the original request
// and to every redirect hop a client follows, and cannot be bypassed by redirecting to a hostname
// that resolves to a blocked address (a check against the pre-fetch URL or Location header string
// alone cannot catch this).
//
// dialer is copied and only its Control field is overridden, so callers keep their own
// Timeout/KeepAlive/etc settings.
func BlockedIPDialContext(dialer *net.Dialer, isBlocked func(net.IP) error) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := *dialer
	d.Control = func(network, address string, c syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("unable to parse IP from resolved address %s", address)
		}
		return isBlocked(ip)
	}
	return d.DialContext
}

// IsLinkLocal reports whether ip is in a link-local range (169.254.0.0/16, fe80::/10). This
// includes cloud provider instance metadata endpoints (e.g. 169.254.169.254) and has no
// legitimate use as a remote fetch target.
func IsLinkLocal(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// knownCloudMetadataRanges are cloud provider instance metadata addresses that fall outside
// the link-local range and so aren't caught by IsLinkLocal: Alibaba Cloud's metadata service
// lives in RFC 6598 carrier-grade-NAT space (not private, loopback, or link-local by any
// net.IP predicate), and AWS's IPv6 IMDS endpoint lives in ULA space reserved by AWS
// specifically for this. Each is reserved by its provider for this one purpose and has no
// other legitimate use, unlike the general private/ULA ranges these otherwise sit in.
var knownCloudMetadataRanges = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range []string{
		"100.100.100.200/32", // Alibaba Cloud metadata service
		"fd00:ec2::/32",      // AWS IMDS IPv6 endpoint
	} {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err) // static list; only fails on a programming error
		}
		nets = append(nets, n)
	}
	return nets
}()

// IsKnownCloudMetadataAddress reports whether ip is a well-known cloud provider instance
// metadata address not otherwise covered by IsLinkLocal.
func IsKnownCloudMetadataAddress(ip net.IP) bool {
	for _, n := range knownCloudMetadataRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
