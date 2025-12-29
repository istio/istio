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

package authenticate

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/alecholmes/xfccparser"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
)

const (
	XfccAuthenticatorType = "XfccAuthenticator"
)

// XfccAuthenticator extracts identities from Xfcc header.
type XfccAuthenticator struct{}

var _ security.Authenticator = &XfccAuthenticator{}

func (xff XfccAuthenticator) AuthenticatorType() string {
	return XfccAuthenticatorType
}

// Authenticate extracts identities from Xfcc Header.
func (xff XfccAuthenticator) Authenticate(ctx security.AuthContext) (*security.Caller, error) {
	remoteAddr := ctx.RemoteAddress()
	xfccHeader := ctx.Header(xfccparser.ForwardedClientCertHeader)
	if len(remoteAddr) == 0 || len(xfccHeader) == 0 {
		return nil, fmt.Errorf("caller from %s does not have Xfcc header", remoteAddr)
	}
	// First check if client is trusted client so that we can "trust" the Xfcc Header.
	if !isTrustedAddress(remoteAddr, features.TrustedGatewayCIDR) {
		return nil, fmt.Errorf("caller from %s is not in the trusted network. XfccAuthenticator can not be used", remoteAddr)
	}

	return buildSecurityCaller(xfccHeader[0])
}

func buildSecurityCaller(xfccHeader string) (*security.Caller, error) {
	clientCerts, err := xfccparser.ParseXFCCHeader(xfccHeader)
	if err != nil {
		return nil, fmt.Errorf("error in parsing xfcc header: %v", err)
	}
	if len(clientCerts) == 0 {
		return nil, fmt.Errorf("xfcc header does not have at least one client certs")
	}
	ids := []string{}
	for _, cc := range clientCerts {
		ids = append(ids, cc.URI...)
		ids = append(ids, cc.DNS...)
		if cc.Subject != nil {
			ids = append(ids, cc.Subject.CommonName)
		}
	}

	return &security.Caller{
		AuthSource: security.AuthSourceClientCertificate,
		Identities: ids,
	}, nil
}

func isTrustedAddress(addr string, trustedCidrs []string) bool {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Warnf("peer address %s can not be split in to proper host and port", addr)
		return false
	}
	for _, cidr := range trustedCidrs {
		if isInRange(ip, cidr) {
			return true
		}
	}
	// Always trust local host addresses.
	return netip.MustParseAddr(ip).IsLoopback()
}

func isInRange(addr, cidr string) bool {
	if strings.Contains(cidr, "/") {
		ipp, err := netip.ParsePrefix(cidr)
		if err != nil {
			return false
		}

		return ipp.Contains(netip.MustParseAddr(addr))
	}
	return false
}
