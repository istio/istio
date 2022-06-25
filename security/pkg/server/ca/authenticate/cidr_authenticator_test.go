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
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test"
)

func TestCidrAuthenticator(t *testing.T) {
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
			name:    "cidr outside range",
			cidr:    "172.17.0.0/16,172.17.0.0/16",
			peer:    "110.17.0.2",
			trusted: false,
		},
	}

	auth := &CidrAuthenticator{}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.cidr) > 0 {
				test.SetStringForTest(t, &features.TrustedCIDRRanges, tt.cidr)
			}
			ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.IPAddr{IP: net.ParseIP(tt.peer).To4()}})
			if result := auth.CanTrustCaller(security.NewAuthContext(ctx)); result != tt.trusted {
				t.Errorf("Unexpected authentication result: want %v but got %v",
					tt.trusted, result)
			}
		})
	}
}
