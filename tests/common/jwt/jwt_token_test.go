// Copyright 2019 Istio Authors
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
	"io/ioutil"
	"testing"

	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jws"
)

func getKey(jwksFile string, t *testing.T) interface{} {
	t.Helper()

	data, err := ioutil.ReadFile(jwksFile)
	if err != nil {
		t.Fatalf("failed to read jwks: %s", err)
	}
	jwks, err := jwk.ParseBytes(data)
	if err != nil {
		t.Fatalf("failed to parse jwks: %s", err)
	}
	key, err := jwks.Keys[0].Materialize()
	if err != nil {
		t.Fatalf("failed to materialize jwks: %s", err)
	}
	return key
}

func TestSampleJwtToken(t *testing.T) {
	testCases := []struct {
		name        string
		token       string
		wantClaims  map[string]interface{}
		wantInvalid bool
	}{
		{
			name:  "TokenIssuer1",
			token: TokenIssuer1,
			wantClaims: map[string]interface{}{
				"group": "group-1",
				"iss":   "test-issuer-1@istio.io",
				"sub":   "sub-1",
				"exp":   4714747295,
			},
		},
		{
			name:  "TokenIssuer2",
			token: TokenIssuer2,
			wantClaims: map[string]interface{}{
				"group": "group-2",
				"iss":   "test-issuer-2@istio.io",
				"sub":   "sub-2",
				"exp":   4714747389,
			},
		},
		{
			name:  "TokenExpired",
			token: TokenExpired,
			wantClaims: map[string]interface{}{
				"group": "group-1",
				"iss":   "test-issuer-1@istio.io",
				"sub":   "sub-1",
				"exp":   1561146548,
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

		claims := map[string]interface{}{}
		err = json.Unmarshal(token, &claims)
		if err != nil {
			t.Fatalf("%s: failed to parse payload: %v", tc.name, err)
		}

		for k, v := range tc.wantClaims {
			switch v.(type) {
			case string:
				got, ok := claims[k].(string)
				if ok {
					if got != v {
						t.Errorf("%s: claim %q got value %q but want %q", tc.name, k, got, v)
					}
				} else {
					t.Errorf("%s: claim %q got type %T but want string", tc.name, k, claims[k])
				}
			case int:
				got, ok := claims[k].(float64)
				gotInt := int(got)
				if ok {
					if gotInt != v {
						t.Errorf("%s: claim %q got value %d but want %d", tc.name, k, gotInt, v)
					}
				} else {
					t.Errorf("%s: claim %q got type %T but want float64", tc.name, k, claims[k])
				}
			default:
				t.Fatalf("unknown claim %q of type %T", k, v)
			}
		}
	}
}
