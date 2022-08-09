// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package authenticate

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
	jose "gopkg.in/square/go-jose.v2"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pkg/security"
)

const (
	bearerTokenPrefix = "Bearer "
)

type jwksServer struct {
	key jose.JSONWebKeySet
	t   *testing.T
}

func (k *jwksServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(k.key); err != nil {
		k.t.Fatalf("failed to encode the jwks: %v", err)
	}
}

func TestNewJwtAuthenticator(t *testing.T) {
	tests := []struct {
		name      string
		expectErr bool
		jwtRule   string
	}{
		{
			name:      "jwt rule with jwks_uri",
			expectErr: false,
			jwtRule:   `{"issuer": "foo", "jwks_uri": "baz", "audiences": ["aud1", "aud2"]}`,
		},
		{
			name: "jwt rule with OIDC config expected to fail",
			// "foo/.well-known/openid-configuration" is expected to fail
			expectErr: true,
			jwtRule:   `{"issuer": "foo", "audiences": ["aud1", "aud2"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jwtRule := v1beta1.JWTRule{}
			err := json.Unmarshal([]byte(tt.jwtRule), &jwtRule)
			if err != nil {
				t.Fatalf("failed at unmarshal the jwt rule (%v), err: %v",
					tt.jwtRule, err)
			}
			_, err = NewJwtAuthenticator(&jwtRule, "domain-foo")
			gotErr := err != nil
			if gotErr != tt.expectErr {
				t.Errorf("expect error is %v while actual error is %v", tt.expectErr, gotErr)
			}
		})
	}
}

func TestCheckAudience(t *testing.T) {
	tests := []struct {
		name        string
		expectRet   bool
		audToCheck  []string
		audExpected []string
	}{
		{
			name:        "audience is in the expected set",
			expectRet:   true,
			audToCheck:  []string{"aud1"},
			audExpected: []string{"aud1", "aud2"},
		},
		{
			name:        "audience is NOT in the expected set",
			expectRet:   false,
			audToCheck:  []string{"aud3"},
			audExpected: []string{"aud1", "aud2"},
		},
		{
			name:        "one of the audiences is in the expected set",
			expectRet:   true,
			audToCheck:  []string{"aud1", "aud3"},
			audExpected: []string{"aud1", "aud2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := checkAudience(tt.audToCheck, tt.audExpected)
			if ret != tt.expectRet {
				t.Errorf("expected return is %v while actual return is %v", tt.expectRet, ret)
			}
		})
	}
}

func TestOIDCAuthenticate(t *testing.T) {
	// Create a JWKS server
	rsaKey, err := rsa.GenerateKey(rand.Reader, 512)
	if err != nil {
		t.Fatalf("failed to generate a private key: %v", err)
	}
	key := jose.JSONWebKey{Algorithm: string(jose.RS256), Key: rsaKey}
	keySet := jose.JSONWebKeySet{}
	keySet.Keys = append(keySet.Keys, key.Public())
	server := httptest.NewServer(&jwksServer{key: keySet})
	defer server.Close()

	// Create a JWT authenticator
	jwtRuleStr := `{"issuer": "` + server.URL + `", "jwks_uri": "` + server.URL + `", "audiences": ["baz.svc.id.goog"]}`
	jwtRule := v1beta1.JWTRule{}
	err = json.Unmarshal([]byte(jwtRuleStr), &jwtRule)
	if err != nil {
		t.Fatalf("failed at unmarshal jwt rule")
	}
	authenticator, err := NewJwtAuthenticator(&jwtRule, "baz.svc.id.goog")
	if err != nil {
		t.Fatalf("failed to create the JWT authenticator: %v", err)
	}

	// Create a valid JWT token
	expStr := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	claims := `{"iss": "` + server.URL + `", "aud": ["baz.svc.id.goog"], "sub": "system:serviceaccount:bar:foo", "exp": ` + expStr + `}`
	token, err := generateJWT(&key, []byte(claims))
	if err != nil {
		t.Fatalf("failed to generate JWT: %v", err)
	}
	// Create an expired JWT token
	expiredStr := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	expiredClaims := `{"iss": "` + server.URL + `", "aud": ["baz.svc.id.goog"], "sub": "system:serviceaccount:bar:foo", "exp": ` + expiredStr + `}`
	expiredToken, err := generateJWT(&key, []byte(expiredClaims))
	if err != nil {
		t.Fatalf("failed to generate an expired JWT: %v", err)
	}
	// Create a JWT token with wrong audience
	claimsWrongAudience := `{"iss": "` + server.URL + `", "aud": ["wrong-audience"], "sub": "system:serviceaccount:bar:foo", "exp": ` + expStr + `}`
	tokenWrongAudience, err := generateJWT(&key, []byte(claimsWrongAudience))
	if err != nil {
		t.Fatalf("failed to generate JWT: %v", err)
	}
	// Create a JWT token with invalid subject, which is not prefixed with "system:serviceaccount"
	claimsWrongSubject := `{"iss": "` + server.URL + `", "aud": ["baz.svc.id.goog"], "sub": "bar:foo", "exp": ` + expStr + `}`
	tokenInvalidSubject, err := generateJWT(&key, []byte(claimsWrongSubject))
	if err != nil {
		t.Fatalf("failed to generate JWT: %v", err)
	}

	tests := map[string]struct {
		token      string
		expectErr  bool
		expectedID string
	}{
		"No bearer token": {
			expectErr: true,
		},
		"Valid token": {
			token:      token,
			expectErr:  false,
			expectedID: fmt.Sprintf(IdentityTemplate, "baz.svc.id.goog", "bar", "foo"),
		},
		"Expired token": {
			token:     expiredToken,
			expectErr: true,
		},
		"Token with wrong audience": {
			token:     tokenWrongAudience,
			expectErr: true,
		},
		"Token with invalid subject": {
			token:     tokenInvalidSubject,
			expectErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			md := metadata.MD{}
			if tc.token != "" {
				token := bearerTokenPrefix + tc.token
				md.Append("authorization", token)
			}
			ctx = metadata.NewIncomingContext(ctx, md)

			actualCaller, err := authenticator.Authenticate(security.AuthContext{GrpcContext: ctx})
			gotErr := err != nil
			if gotErr != tc.expectErr {
				t.Errorf("gotErr (%v) whereas expectErr (%v)", gotErr, tc.expectErr)
			}
			if gotErr {
				return
			}
			expectedCaller := &security.Caller{
				AuthSource: security.AuthSourceIDToken,
				Identities: []string{tc.expectedID},
			}
			if !reflect.DeepEqual(actualCaller, expectedCaller) {
				t.Errorf("%v: unexpected caller (want %v but got %v)", name, expectedCaller, actualCaller)
			}
		})
	}
}

func generateJWT(key *jose.JSONWebKey, claims []byte) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.SignatureAlgorithm(key.Algorithm),
		Key:       key,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create a signer: %v", err)
	}
	signature, err := signer.Sign(claims)
	if err != nil {
		return "", fmt.Errorf("failed to sign claims: %v", err)
	}
	jwt, err := signature.CompactSerialize()
	if err != nil {
		return "", fmt.Errorf("failed to serialize the JWT: %v", err)
	}
	return jwt, nil
}
