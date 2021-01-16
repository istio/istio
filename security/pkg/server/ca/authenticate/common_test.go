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
	"context"
	"testing"

	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/security"
)

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

		actual, err := security.ExtractBearerToken(ctx)
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
