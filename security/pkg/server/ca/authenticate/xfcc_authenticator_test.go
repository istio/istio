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
			name: "Xfcc Header",
			// nolint lll
			xfccHeader: `Hash=hash;Subject="CN=hello,OU=hello,O=Acme\, Inc.";URI=;DNS=hello.west.example.com;DNS=hello.east.example.com,By=spiffe://mesh.example.com/ns/hellons/sa/hellosa;Hash=again;Subject="";URI=spiffe://mesh.example.com/ns/otherns/sa/othersa`,
			caller: &security.Caller{
				AuthSource: security.AuthSourceClientCertificate,
				Identities: []string{
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
			ctx := metadata.NewIncomingContext(context.Background(), md)
			result, err := auth.Authenticate(security.NewAuthContext(ctx))
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
