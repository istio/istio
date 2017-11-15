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

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"istio.io/istio/security/pkg/pki"
)

// TODO: Test the error messages.
func TestAuthenticate(t *testing.T) {
	callerID := "test.identity"
	ids := []pki.Identity{
		{Type: pki.TypeURI, Value: []byte(callerID)},
	}
	sanExt, err := pki.BuildSANExtension(ids)
	if err != nil {
		t.Error(err)
	}

	testCases := map[string]struct {
		certChain          [][]*x509.Certificate
		caller             *caller
		authenticateErrMsg string
	}{
		"no client certificate": {
			certChain:          nil,
			caller:             nil,
			authenticateErrMsg: "no client certificate is presented",
		},
		"Empty cert chain": {
			certChain:          [][]*x509.Certificate{},
			caller:             nil,
			authenticateErrMsg: "no verified chain is found",
		},
		"With client certificate": {
			certChain: [][]*x509.Certificate{
				{
					{
						Extensions: []pkix.Extension{*sanExt},
					},
				},
			},
			caller: &caller{identities: []string{callerID}},
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

		result, err := auth.authenticate(ctx)
		if len(tc.authenticateErrMsg) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.authenticateErrMsg {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), tc.authenticateErrMsg)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		if !reflect.DeepEqual(tc.caller, result) {
			t.Errorf("Case %q: Unexpected authentication result: want %v but got %v", id, tc.caller, result)
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
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != tc.extractBearerTokenErrMsg {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), tc.extractBearerTokenErrMsg)
			}
			continue
		} else if err != nil {
			t.Fatalf("%s: Unexpected Error: %v", id, err)
		}

		if actual != tc.expectedToken {
			t.Errorf("Case %q: unexpected token: want %s but got %s", id, tc.expectedToken, actual)
		}
	}
}
