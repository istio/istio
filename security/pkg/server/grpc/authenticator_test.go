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

	"istio.io/istio/security/pkg/pki"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"golang.org/x/net/context"
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
		certChain [][]*x509.Certificate
		caller    *caller
	}{
		"no client certificate": {
			certChain: nil,
			caller:    nil,
		},
		"Empty cert chain": {
			certChain: [][]*x509.Certificate{},
			caller:    nil,
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
		result, _ := auth.authenticate(ctx)

		if !reflect.DeepEqual(tc.caller, result) {
			t.Errorf("Case %q: Unexpected authentication result: want %v but got %v", id, tc.caller, result)
		}
	}
}

// TODO: Test the error messages.
func TestExtractBearerToken(t *testing.T) {
	testCases := map[string]struct {
		metadata      metadata.MD
		expectedToken string
	}{
		"No metadata": {
			expectedToken: "",
		},
		"No auth header": {
			metadata: metadata.MD{
				"random": []string{},
			},
			expectedToken: "",
		},
		"No bearer token": {
			metadata: metadata.MD{
				"random": []string{},
				"authorization": []string{
					"Basic callername",
				},
			},
			expectedToken: "",
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
		if actual, _ := extractBearerToken(ctx); actual != tc.expectedToken {
			t.Errorf("Case %q: unexpected token: want %s but got %s", id, tc.expectedToken, actual)
		}
	}
}
