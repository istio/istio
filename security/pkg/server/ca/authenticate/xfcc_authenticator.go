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
	"fmt"
	"net"
	"net/http"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/alecholmes/xfccparser"

	"istio.io/istio/pilot/pkg/features"
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
func (xff XfccAuthenticator) Authenticate(ctx *security.AuthContext) (*security.Caller, error) {
	authResult := security.AuthResult{}
	ctx.Authenticators[xff.AuthenticatorType()] = authResult
	peerInfo, _ := peer.FromContext(ctx.RequestContext)
	// First check if client is trusted client so that we can "trust" the Xfcc Header.
	if !isTrustedAddress(peerInfo.Addr.String()) {
		message := fmt.Sprintf("caller from %s is not in the trusted network. XfccAuthenticator can not be used", peerInfo.Addr.String())
		authResult.Messages = append(authResult.Messages, message)
		return nil, fmt.Errorf(message)
	}
	meta, ok := metadata.FromIncomingContext(ctx.RequestContext)

	if !ok || len(meta.Get(xfccparser.ForwardedClientCertHeader)) == 0 {
		return nil, nil
	}
	xfccHeader := meta.Get(xfccparser.ForwardedClientCertHeader)[0]
	return buildSecurityCaller(xfccHeader, &authResult)
}

// AuthenticateRequest validates Xfcc Header.
func (xff XfccAuthenticator) AuthenticateRequest(req *http.Request) (*security.Caller, error) {
	xfccHeader := req.Header.Get(xfccparser.ForwardedClientCertHeader)
	if len(xfccHeader) == 0 {
		return nil, nil
	}
	return buildSecurityCaller(xfccHeader, &security.AuthResult{})
}

func buildSecurityCaller(xfccHeader string, authResult *security.AuthResult) (*security.Caller, error) {
	clientCerts, err := xfccparser.ParseXFCCHeader(xfccHeader)
	if err != nil {
		message := fmt.Sprintf("error in parsing xfcc header: %v", err)
		authResult.Messages = append(authResult.Messages, message)
		return nil, fmt.Errorf(message)
	}
	if len(clientCerts) == 0 {
		message := "xfcc header does not have atleast one client certs"
		authResult.Messages = append(authResult.Messages, message)
		return nil, fmt.Errorf(message)
	}
	if len(clientCerts) > 1 {
		message := " Oxfcc header has more then one  client certs"
		authResult.Messages = append(authResult.Messages, message)
		return nil, fmt.Errorf(message)
	}
	ids := []string{}
	for _, cc := range clientCerts {
		ids = append(ids, cc.URI)
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

func isTrustedAddress(addr string) bool {
	if len(features.TrustedGatewayCIDR) > 0 {
		cidrs := strings.Split(features.TrustedGatewayCIDR, ",")
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
