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
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"reflect"
	"testing"

	oidc "github.com/coreos/go-oidc"
	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

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
		caller             *Caller
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
			caller: &Caller{Identities: []string{callerID}},
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

		result, err := auth.Authenticate(ctx)
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

type mockKeySet struct {
	err     error
	payload []byte
}

func (ks *mockKeySet) VerifySignature(ctx context.Context, jwt string) (payload []byte, err error) {
	return ks.payload, ks.err
}

func TestAuthenticate_IDTokenAuthenticator(t *testing.T) {
	payload := `{"iss":"https://foo", "email":"test@foo"}`
	signedPayload := "Bearer eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJodHRwczovL2ZvbyIsICJlbWFpbCI" +
		"6InRlc3RAZm9vIn0.BzP-QWpGleJ9CmsIOahyuw1A-O5qmv5yKrfehHh2ubcT_Ug3RHXDyt3sunU14_NAmdtIhE7EO5ywS" +
		"v1j8BEa1dZjJf3U8CICKj_MIe3EV_Ks2yzkgJr0UQ5xIa1IGaSITtARQUas7qzhArcECobDeINkMUaCup4nmQkWxg0rkjH3"

	testCases := map[string]struct {
		metadata       metadata.MD
		verifyError    error
		expectedErrMsg string
		expectedCaller *Caller
	}{
		"No ID token": {
			expectedErrMsg: "ID token extraction error: no metadata is attached",
		},
		"ID token with invalid signature": {
			metadata:       metadata.MD{"authorization": []string{signedPayload}},
			verifyError:    errors.New("invalid signature"),
			expectedErrMsg: "failed to verify the ID token (error failed to verify signature: invalid signature)",
		},
		"ID token with correct signature": {
			metadata: metadata.MD{"authorization": []string{signedPayload}},
			expectedCaller: &Caller{
				AuthSource: AuthSourceIDToken,
				Identities: []string{"test@foo"},
			},
		},
	}

	for id, tc := range testCases {
		ctx := context.Background()
		if tc.metadata != nil {
			ctx = metadata.NewIncomingContext(ctx, tc.metadata)
		}

		auth := &IDTokenAuthenticator{
			verifier: oidc.NewVerifier(
				"https://foo",
				&mockKeySet{
					err:     tc.verifyError,
					payload: []byte(payload)},
				&oidc.Config{
					SkipClientIDCheck: true,
					SkipExpiryCheck:   true,
				})}
		actual, err := auth.Authenticate(ctx)
		if len(tc.expectedErrMsg) > 0 {
			if err == nil {
				t.Errorf("Case %s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.expectedErrMsg {
				t.Errorf("Case %s: Incorrect error message: want %s but got %s", id, tc.expectedErrMsg, err.Error())
			}
		} else if err != nil {
			t.Fatalf("Case %s: Unexpected Error: %v", id, err)
		}
		if tc.expectedCaller != nil {
			if !reflect.DeepEqual(tc.expectedCaller, actual) {
				t.Errorf("Case %s: Unexpected caller: want %v but got %v", id, tc.expectedCaller, actual)
			}
		}
	}
}

func TestExtractBearerToken(t *testing.T) {
	testCases := map[string]struct {
		metadata                 metadata.MD
		expectedToken            string
		extractBearerTokenErrMsg string
	}{
		"No metadata": {
			expectedToken:            "",
			extractBearerTokenErrMsg: "no metadata is attached",
		},
		"No auth header": {
			metadata: metadata.MD{
				"random": []string{},
			},
			expectedToken:            "",
			extractBearerTokenErrMsg: "no HTTP authorization header exists",
		},
		"No bearer token": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
				},
			},
			expectedToken:            "",
			extractBearerTokenErrMsg: "no bearer token exists in HTTP authorization header",
		},
		"With bearer token": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
					"Bearer bearer-token",
				},
			},
			expectedToken: "bearer-token",
		},
	}

	for id, tc := range testCases {
		ctx := context.Background()
		if tc.metadata != nil {
			ctx = metadata.NewIncomingContext(ctx, tc.metadata)
		}

		actual, err := extractBearerToken(ctx)
		if len(tc.extractBearerTokenErrMsg) > 0 {
			if err == nil {
				t.Errorf("Case %s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.extractBearerTokenErrMsg {
				t.Errorf("Case %s: Incorrect error message: %s VS %s",
					id, err.Error(), tc.extractBearerTokenErrMsg)
			}
			continue
		} else if err != nil {
			t.Fatalf("Case %s: Unexpected Error: %v", id, err)
		}

		if actual != tc.expectedToken {
			t.Errorf("Case %q: Unexpected token: want %s but got %s", id, tc.expectedToken, actual)
		}
	}
}
