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

package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"reflect"
	"testing"

	"istio.io/auth/pkg/pki"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"golang.org/x/net/context"
)

func TestAuthenticat(t *testing.T) {
	userID := "test.identity"
	ids := []pki.Identity{
		{Type: pki.TypeURI, Value: []byte(userID)},
	}
	sanExt, err := pki.BuildSANExtension(ids)
	if err != nil {
		t.Error(err)
	}

	testCases := map[string]struct {
		certChain [][]*x509.Certificate
		user      *user
	}{
		"no client certificate": {
			certChain: nil,
			user:      nil,
		},
		"Empty cert chain": {
			certChain: [][]*x509.Certificate{},
			user:      nil,
		},
		"With client certificate": {
			certChain: [][]*x509.Certificate{
				{
					{
						Extensions: []pkix.Extension{*sanExt},
					},
				},
			},
			user: &user{identities: []string{userID}},
		},
	}

	auth := &clientCertAuthenticator{}

	for id, tc := range testCases {
		ctx := context.Background()
		if tc.certChain != nil {
			tlsInfo := credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: tc.certChain},
			}
			p := &peer.Peer{AuthInfo: tlsInfo}
			ctx = peer.NewContext(ctx, p)
		}
		result := auth.authenticate(ctx)

		if !reflect.DeepEqual(tc.user, result) {
			t.Errorf("Case %q: Unexpected authentication result: want %v but got %v", id, tc.user, result)
		}
	}
}
