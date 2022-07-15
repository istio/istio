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
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/alecholmes/xfccparser"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pkg/security"
)

func TestIsTrustedAddress(t *testing.T) {
	cases := []struct {
		name    string
		cidr    string
		peer    string
		trusted bool
	}{
		{
			name:    "localhost client",
			cidr:    "",
			peer:    "127.0.0.1",
			trusted: true,
		},
		{
			name:    "external client without trusted cidr",
			cidr:    "",
			peer:    "172.0.0.1",
			trusted: false,
		},
		{
			name:    "cidr in range",
			cidr:    "172.17.0.0/16,192.17.0.0/16",
			peer:    "172.17.0.2",
			trusted: true,
		},
		{
			name:    "cidr in range with both ipv6 and ipv4",
			cidr:    "172.17.0.0/16,2001:db8:1234:1a00::/56",
			peer:    "2001:0db8:1234:1aff:ffff:ffff:ffff:ffff",
			trusted: true,
		},
		{
			name:    "cidr outside range",
			cidr:    "172.17.0.0/16,172.17.0.0/16",
			peer:    "110.17.0.2",
			trusted: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if result := isTrustedAddress(tt.peer, strings.Split(tt.cidr, ",")); result != tt.trusted {
				t.Errorf("Unexpected authentication result: want %v but got %v",
					tt.trusted, result)
			}
		})
	}
}

func TestXfccAuthenticator(t *testing.T) {
	cases := []struct {
		name               string
		xfccHeader         string
		caller             *security.Caller
		authenticateErrMsg string
	}{
		{
			name:       "No xfcc header",
			xfccHeader: "",
			caller:     nil,
		},
		{
			name:               "junk xfcc header",
			xfccHeader:         `junk xfcc header`,
			authenticateErrMsg: `error in parsing xfcc header: invalid header format: unexpected token "junk xfcc header"`,
		},
		{
			name: "Xfcc Header single hop",
			// nolint lll
			xfccHeader: `Hash=meshclient;Subject="";URI=spiffe://mesh.example.com/ns/otherns/sa/othersa`,
			caller: &security.Caller{
				AuthSource: security.AuthSourceClientCertificate,
				Identities: []string{
					"spiffe://mesh.example.com/ns/otherns/sa/othersa",
				},
			},
		},
		{
			name: "Xfcc Header multiple hops",
			// nolint lll
			xfccHeader: `Hash=hash;Cert="-----BEGIN%20CERTIFICATE-----%0cert%0A-----END%20CERTIFICATE-----%0A";Subject="CN=hello,OU=hello,O=Acme\, Inc.";URI=spiffe://mesh.example.com/ns/firstns/sa/firstsa;DNS=hello.west.example.com;DNS=hello.east.example.com,By=spiffe://mesh.example.com/ns/hellons/sa/hellosa;Hash=again;Subject="";URI=spiffe://mesh.example.com/ns/otherns/sa/othersa`,
			caller: &security.Caller{
				AuthSource: security.AuthSourceClientCertificate,
				Identities: []string{
					"spiffe://mesh.example.com/ns/firstns/sa/firstsa",
					"hello.west.example.com",
					"hello.east.example.com",
					"hello",
					"spiffe://mesh.example.com/ns/otherns/sa/othersa",
				},
			},
		},
	}

	auth := &XfccAuthenticator{}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			md := metadata.MD{}
			if len(tt.xfccHeader) > 0 {
				md.Append(xfccparser.ForwardedClientCertHeader, tt.xfccHeader)
			}
			ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.IPAddr{IP: net.ParseIP("127.0.0.1").To4()}})
			ctx = metadata.NewIncomingContext(ctx, md)
			result, err := auth.Authenticate(security.AuthContext{GrpcContext: ctx})
			if len(tt.authenticateErrMsg) > 0 {
				if err == nil {
					t.Errorf("Succeeded. Error expected: %v", err)
				} else if err.Error() != tt.authenticateErrMsg {
					t.Errorf("Incorrect error message: want %s but got %s",
						tt.authenticateErrMsg, err.Error())
				}
			} else if err != nil {
				t.Fatalf("Unexpected Error: %v", err)
			}

			if !reflect.DeepEqual(tt.caller, result) {
				t.Errorf("Unexpected authentication result: want %v but got %v",
					tt.caller, result)
			}
		})
	}
}
