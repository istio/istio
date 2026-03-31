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

package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jwt"

	"istio.io/istio/pkg/util/sets"
)

func TestDPoPValidator(t *testing.T) {
	config := DefaultDPoPConfig()
	validator := NewDPoPValidator(config)
	defer validator.Close()

	// Generate test key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	jwkKey, err := jwk.New(privateKey.PublicKey)
	if err != nil {
		t.Fatalf("Failed to create JWK: %v", err)
	}

	tests := []struct {
		name        string
		request     *http.Request
		expectError bool
		errorMsg    string
	}{
		{
			name:        "missing DPoP header",
			request:     &http.Request{Method: "GET", URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/api"}},
			expectError: true,
			errorMsg:    "missing DPoP header",
		},
		{
			name:        "invalid DPoP token",
			request:     createTestRequest(t, "invalid.jwt.token", "GET", "https://example.com/api"),
			expectError: true,
			errorMsg:    "failed to parse DPoP JWT",
		},
		{
			name:        "valid DPoP proof",
			request:     createValidDPoPRequest(t, jwkKey, privateKey, "GET", "https://example.com/api"),
			expectError: false,
		},
		{
			name:        "HTTP method mismatch",
			request:     createValidDPoPRequest(t, jwkKey, privateKey, "POST", "https://example.com/api"),
			expectError: true,
			errorMsg:    "HTTP method mismatch",
		},
		{
			name:        "HTTP URI mismatch",
			request:     createValidDPoPRequest(t, jwkKey, privateKey, "GET", "https://example.com/other"),
			expectError: true,
			errorMsg:    "HTTP URI mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proof, err := validator.ValidateDPoPProof(tt.request)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if proof == nil {
					t.Errorf("Expected proof but got nil")
				}
			}
		})
	}
}

func TestDPoPTokenBinding(t *testing.T) {
	config := DefaultDPoPConfig()
	validator := NewDPoPValidator(config)
	defer validator.Close()

	// Generate test key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate test key: %v", err)
	}

	jwkKey, err := jwk.New(privateKey.PublicKey)
	if err != nil {
		t.Fatalf("Failed to create JWK: %v", err)
	}

	// Calculate JWK thumbprint
	jkt, err := calculateJWKThumbprint(jwkKey)
	if err != nil {
		t.Fatalf("Failed to calculate JWK thumbprint: %v", err)
	}

	tests := []struct {
		name        string
		accessToken string
		proof       *DPoPProof
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid token binding",
			accessToken: createAccessTokenWithJKT(t, jkt),
			proof: &DPoPProof{
				JKT: jkt,
			},
			expectError: false,
		},
		{
			name:        "missing cnf claim",
			accessToken: createAccessTokenWithoutCNF(t),
			proof: &DPoPProof{
				JKT: jkt,
			},
			expectError: true,
			errorMsg:    "missing cnf claim",
		},
		{
			name:        "jkt mismatch",
			accessToken: createAccessTokenWithJKT(t, "different-jkt"),
			proof: &DPoPProof{
				JKT: jkt,
			},
			expectError: true,
			errorMsg:    "token binding failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateTokenBinding(tt.accessToken, tt.proof)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestReplayCache(t *testing.T) {
	cache := NewReplayCache(100, time.Minute)
	defer cache.Close()

	jti := "test-jti-123"
	iat := time.Now()

	// First use should succeed
	err := cache.CheckAndAdd(jti, iat)
	if err != nil {
		t.Errorf("First use should succeed: %v", err)
	}

	// Second use should fail (replay attack)
	err = cache.CheckAndAdd(jti, iat)
	if err == nil {
		t.Errorf("Second use should fail (replay attack)")
	}

	// Different JTI should succeed
	err = cache.CheckAndAdd("different-jti", iat)
	if err != nil {
		t.Errorf("Different JTI should succeed: %v", err)
	}
}

func TestDPoPSettings(t *testing.T) {
	tests := []struct {
		name        string
		settings    *DPoPSettings
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid settings",
			settings:    &DPoPSettings{Enabled: true, Required: false},
			expectError: false,
		},
		{
			name:        "negative max age",
			settings:    &DPoPSettings{Enabled: true, MaxAge: durationPtr(-time.Second)},
			expectError: true,
			errorMsg:    "max_age must be positive",
		},
		{
			name:        "max age too large",
			settings:    &DPoPSettings{Enabled: true, MaxAge: durationPtr(2 * time.Hour)},
			expectError: true,
			errorMsg:    "max_age cannot exceed 1 hour",
		},
		{
			name:        "negative cache size",
			settings:    &DPoPSettings{Enabled: true, ReplayCacheSize: -1},
			expectError: true,
			errorMsg:    "replay_cache_size cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDPoPManager(t *testing.T) {
	manager := GetDPoPManager()
	defer manager.Close()

	config := DefaultDPoPConfig()
	
	// Get validator
	validator1 := manager.GetValidator(config)
	if validator1 == nil {
		t.Errorf("Expected validator but got nil")
	}

	// Get validator again - should return the same instance
	validator2 := manager.GetValidator(config)
	if validator1 != validator2 {
		t.Errorf("Expected same validator instance")
	}
}

// Helper functions

func createTestRequest(t *testing.T, dpopToken, method, uri string) *http.Request {
	req, err := http.NewRequest(method, uri, nil)
	if err != nil {
		t.Fatalf("Failed to create test request: %v", err)
	}
	
	if dpopToken != "" {
		req.Header.Set(DPoPHeaderName, dpopToken)
	}
	
	return req
}

func createValidDPoPRequest(t *testing.T, jwkKey jwk.Key, privateKey *ecdsa.PrivateKey, method, uri string) *http.Request {
	// Create DPoP proof
	now := time.Now()
	token := jwt.NewBuilder().
		JwtID("test-jti-" + sets.NewElement().String()).
		IssuedAt(now).
		Claim("htu", uri).
		Claim("htm", method).
		Claim("ath", "test-access-token-hash").
		Build()

	// Add JWK header
	headers := jwt.NewHeaders()
	headers.Set("jwk", jwkKey)
	token.SetHeaders(headers)

	// Sign the token
	signedToken, err := jwt.Sign(token, jwt.WithKey(jwa.ES256, privateKey))
	if err != nil {
		t.Fatalf("Failed to sign DPoP token: %v", err)
	}

	return createTestRequest(t, string(signedToken), method, uri)
}

func createAccessTokenWithJKT(t *testing.T, jkt string) string {
	claims := map[string]interface{}{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"cnf": map[string]interface{}{
			"jkt": jkt,
		},
	}

	token := jwt.NewBuilder().
		Claims(claims).
		Build()

	signedToken, err := jwt.Sign(token, jwt.WithKey(jwa.ES256, generateTestPrivateKey(t)))
	if err != nil {
		t.Fatalf("Failed to sign access token: %v", err)
	}

	return string(signedToken)
}

func createAccessTokenWithoutCNF(t *testing.T) string {
	claims := map[string]interface{}{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewBuilder().
		Claims(claims).
		Build()

	signedToken, err := jwt.Sign(token, jwt.WithKey(jwa.ES256, generateTestPrivateKey(t)))
	if err != nil {
		t.Fatalf("Failed to sign access token: %v", err)
	}

	return string(signedToken)
}

func generateTestPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate test private key: %v", err)
	}
	return privateKey
}

func durationPtr(d time.Duration) *time.Duration {
	return &d
}
