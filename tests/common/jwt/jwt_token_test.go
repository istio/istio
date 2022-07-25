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

package jwt

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jws"
)

func getKey(jwksFile string, t *testing.T) any {
	t.Helper()

	data, err := os.ReadFile(jwksFile)
	if err != nil {
		t.Fatalf("failed to read jwks: %s", err)
	}
	jwks, err := jwk.Parse(data)
	if err != nil {
		t.Fatalf("failed to parse jwks: %s", err)
	}
	var key any
	k, _ := jwks.Get(0)
	if err := k.Raw(&key); err != nil {
		t.Fatalf("failed to materialize jwks: %s", err)
	}
	return key
}

func TestSampleJwtToken(t *testing.T) {
	testCases := []struct {
		name        string
		token       string
		wantClaims  map[string]any
		wantInvalid bool
	}{
		{
			name:  "TokenIssuer1",
			token: TokenIssuer1,
			wantClaims: map[string]any{
				"groups": []any{"group-1"},
				"iss":    "test-issuer-1@istio.io",
				"sub":    "sub-1",
				"exp":    4715782722.0,
			},
		},
		{
			name:  "TokenIssuer1NestedClaims1",
			token: TokenIssuer1WithNestedClaims1,
			wantClaims: map[string]any{
				"nested": map[string]any{
					"key1": []any{"valueA", "valueB"},
					"nested-2": map[string]any{
						"key1": []any{"valueA", "valueB"},
					},
				},
				"iss": "test-issuer-1@istio.io",
				"sub": "sub-1",
				"exp": 4757607896.0,
			},
		},
		{
			name:  "TokenIssuer1NestedClaims2",
			token: TokenIssuer1WithNestedClaims2,
			wantClaims: map[string]any{
				"nested": map[string]any{
					"key2": "valueC",
					"nested-2": map[string]any{
						"key2": "valueC",
					},
				},
				"iss": "test-issuer-1@istio.io",
				"sub": "sub-1",
				"exp": 4757608018.0,
			},
		},
		{
			name:  "TokenIssuer2",
			token: TokenIssuer2,
			wantClaims: map[string]any{
				"groups": []any{"group-2"},
				"iss":    "test-issuer-2@istio.io",
				"sub":    "sub-2",
				"exp":    4715782783.0,
			},
		},
		{
			name:  "TokenExpired",
			token: TokenExpired,
			wantClaims: map[string]any{
				"groups": []any{"group-1"},
				"iss":    "test-issuer-1@istio.io",
				"sub":    "sub-1",
				"exp":    1562182856.0,
			},
		},
		{
			name:        "TokenInvalid",
			token:       TokenInvalid,
			wantInvalid: true,
		},
	}

	key := getKey("jwks.json", t)
	for _, tc := range testCases {
		token, err := jws.Verify([]byte(tc.token), jwa.RS256, key)
		if tc.wantInvalid {
			if err == nil {
				t.Errorf("%s: got valid token but want invalid", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: failed to parse token: %v", tc.name, err)
		}

		claims := map[string]any{}
		err = json.Unmarshal(token, &claims)
		if err != nil {
			t.Fatalf("%s: failed to parse payload: %v", tc.name, err)
		}

		for k, v := range tc.wantClaims {
			got, ok := claims[k]
			if ok {
				if !reflect.DeepEqual(got, v) {
					t.Errorf("%s: claim %q got value %v but want %v", tc.name, k, got, v)
				}
			} else {
				t.Errorf("%s: want claim %s but not found", tc.name, k)
			}
		}
	}
}
