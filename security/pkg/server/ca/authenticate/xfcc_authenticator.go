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
	"context"
	"fmt"
	"net"
	"net/http"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/alecholmes/xfccparser"

	"istio.io/istio/pkg/security"
)

const (
	XfccAuthenticatorType = "XfccAuthenticator"
)

// XfccAuthenticator extracts identities from Xfcc header.
type XfccAuthenticator struct{}

var _ security.Authenticator = &XfccAuthenticator{}

func (xff *XfccAuthenticator) AuthenticatorType() string {
	return XfccAuthenticatorType
}

// Authenticate extracts identities from Xfcc Header.
func (xff *XfccAuthenticator) Authenticate(ctx security.AuthContext) (*security.Caller, error) {
	// TODO: Extend this for other "trusted" IPs based on network policies.
	if !isLocalHost(ctx.RequestContext) {
		return nil, fmt.Errorf("call is not from trusted network, xfcc can not be used as authenticator")
	}
	meta, ok := metadata.FromIncomingContext(ctx.RequestContext)

	if !ok || len(meta.Get(xfccparser.ForwardedClientCertHeader)) == 0 {
		return nil, fmt.Errorf("xfcc header is not present")
	}

	xfccHeader := meta.Get(xfccparser.ForwardedClientCertHeader)[0]
	return buildSecurityCaller(xfccHeader)
}

func isLocalHost(ctx context.Context) bool {
	peerInfo, _ := peer.FromContext(ctx)
	if net.ParseIP(peerInfo.Addr.String()).IsLoopback() {
		return true
	}
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip.String() == peerInfo.Addr.String() {
				return true
			}
		}
	}
	return false
}

// AuthenticateRequest validates Xfcc Header.
func (xff *XfccAuthenticator) AuthenticateRequest(req *http.Request) (*security.Caller, error) {
	xfccHeader := req.Header.Get(xfccparser.ForwardedClientCertHeader)
	if len(xfccHeader) == 0 {
		return nil, fmt.Errorf("xfcc header is not present")
	}
	return buildSecurityCaller(xfccHeader)
}

func buildSecurityCaller(xfccHeader string) (*security.Caller, error) {
	clientCerts, err := xfccparser.ParseXFCCHeader(xfccHeader)
	if err != nil {
		return nil, fmt.Errorf("error in parsing xfcc header: %v", err)
	}
	ids := []string{}
	for _, cc := range clientCerts {
		ids = append(ids, cc.DNS...)
		if len(cc.URI) > 0 {
			ids = append(ids, cc.URI)
		}
	}

	return &security.Caller{
		AuthSource: security.AuthSourceClientCertificate,
		Identities: ids,
	}, nil
}
