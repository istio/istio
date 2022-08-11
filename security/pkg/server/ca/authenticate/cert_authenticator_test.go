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
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"reflect"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/pki/util"
)

type mockAuthInfo struct {
	authType string
}

func (ai mockAuthInfo) AuthType() string {
	return ai.authType
}

func TestAuthenticate_clientCertAuthenticator(t *testing.T) {
	callerID := "test.identity"
	ids := []util.Identity{
		{Type: util.TypeURI, Value: []byte(callerID)},
	}
	sanExt, err := util.BuildSANExtension(ids)
	if err != nil {
		t.Error(err)
	}

	testCases := map[string]struct {
		certChain          [][]*x509.Certificate
		caller             *security.Caller
		authenticateErrMsg string
		fakeAuthInfo       *mockAuthInfo
	}{
		"No client certificate": {
			certChain:          nil,
			caller:             nil,
			authenticateErrMsg: "no client certificate is presented",
		},
		"Unsupported auth type": {
			certChain:          nil,
			caller:             nil,
			authenticateErrMsg: "unsupported auth type: \"not-tls\"",
			fakeAuthInfo:       &mockAuthInfo{"not-tls"},
		},
		"Empty cert chain": {
			certChain:          [][]*x509.Certificate{},
			caller:             nil,
			authenticateErrMsg: "no verified chain is found",
		},
		"Certificate has no SAN": {
			certChain: [][]*x509.Certificate{
				{
					{
						Version: 1,
					},
				},
			},
			authenticateErrMsg: "the SAN extension does not exist",
		},
		"With client certificate": {
			certChain: [][]*x509.Certificate{
				{
					{
						Extensions: []pkix.Extension{*sanExt},
					},
				},
			},
			caller: &security.Caller{Identities: []string{callerID}},
		},
	}

	auth := &ClientCertAuthenticator{}

	for id, tc := range testCases {
		ctx := context.Background()
		if tc.certChain != nil {
			tlsInfo := credentials.TLSInfo{
				State: tls.ConnectionState{VerifiedChains: tc.certChain},
			}
			p := &peer.Peer{AuthInfo: tlsInfo}
			ctx = peer.NewContext(ctx, p)
		}
		if tc.fakeAuthInfo != nil {
			ctx = peer.NewContext(ctx, &peer.Peer{AuthInfo: tc.fakeAuthInfo})
		}

		result, err := auth.Authenticate(security.AuthContext{GrpcContext: ctx})
		if len(tc.authenticateErrMsg) > 0 {
			if err == nil {
				t.Errorf("Case %s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.authenticateErrMsg {
				t.Errorf("Case %s: Incorrect error message: want %s but got %s",
					id, tc.authenticateErrMsg, err.Error())
			}
			continue
		} else if err != nil {
			t.Fatalf("Case %s: Unexpected Error: %v", id, err)
		}

		if !reflect.DeepEqual(tc.caller, result) {
			t.Errorf("Case %q: Unexpected authentication result: want %v but got %v",
				id, tc.caller, result)
		}
	}
}
