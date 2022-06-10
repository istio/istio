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
	"reflect"
	"testing"

	"github.com/alecholmes/xfccparser"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	"istio.io/istio/pkg/security"
)

func TestAuthenticate_XfccAuthenticator(t *testing.T) {
	testCases := map[string]struct {
		xfccHeader         string
		caller             *security.Caller
		authenticateErrMsg string
	}{
		"No xfcc header": {
			xfccHeader:         "",
			caller:             nil,
			authenticateErrMsg: "xfcc header is not present",
		},
		"Xfcc Header": {
			// nolint lll
			xfccHeader: `Hash=hash;Subject="CN=hello,OU=hello,O=Acme\, Inc.";URI=;DNS=hello.west.example.com;DNS=hello.east.example.com,By=spiffe://mesh.example.com/ns/hellons/sa/hellosa;Hash=again;Subject="";URI=spiffe://mesh.example.com/ns/otherns/sa/othersa`,
			caller: &security.Caller{
				AuthSource: security.AuthSourceClientCertificate,
				Identities: []string{
					"hello.west.example.com",
					"hello.east.example.com",
					"spiffe://mesh.example.com/ns/otherns/sa/othersa",
				},
			},
		},
	}

	auth := &XfccAuthenticator{}

	for id, tc := range testCases {
		ctx := context.Background()
		md := metadata.MD{}
		if len(tc.xfccHeader) > 0 {
			md.Append(xfccparser.ForwardedClientCertHeader, tc.xfccHeader)
		}

		ctx = metadata.NewIncomingContext(ctx, md)
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
