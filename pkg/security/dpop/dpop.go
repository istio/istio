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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jwt"

	"istio.io/istio/pkg/log"
)

const (
	// DPoPHeaderName is the HTTP header name for DPoP proof tokens
	DPoPHeaderName = "DPoP"
	
	// DefaultDPoPMaxAge is the default maximum age for DPoP proofs (5 minutes)
	DefaultDPoPMaxAge = 5 * time.Minute
	
	// DefaultCacheCleanupInterval is how often to clean the replay cache
	DefaultCacheCleanupInterval = 10 * time.Minute
)

var (
	dpopLog = log.RegisterScope("dpop", "DPoP authentication debugging")
)

// DPoPConfig contains configuration for DPoP validation
type DPoPConfig struct {
	// MaxAge defines the maximum age for DPoP proofs
	MaxAge time.Duration
	
	// CacheCleanupInterval defines how often to clean the replay cache
	CacheCleanupInterval time.Duration
	
	// ReplayCacheSize defines the maximum size of the replay cache
	ReplayCacheSize int
}

// DefaultDPoPConfig returns a default DPoP configuration
func DefaultDPoPConfig() *DPoPConfig {
	return &DPoPConfig{
		MaxAge:                DefaultDPoPMaxAge,
		CacheCleanupInterval:  DefaultCacheCleanupInterval,
		ReplayCacheSize:       10000,
	}
}

// DPoPValidator handles DPoP proof validation
type DPoPValidator struct {
	config      *DPoPConfig
	replayCache *ReplayCache
}

// NewDPoPValidator creates a new DPoP validator
func NewDPoPValidator(config *DPoPConfig) *DPoPValidator {
	if config == nil {
		config = DefaultDPoPConfig()
	}
	
	validator := &DPoPValidator{
		config:      config,
		replayCache: NewReplayCache(config.ReplayCacheSize, config.CacheCleanupInterval),
	}
	
	return validator
}

// DPoPProof represents a validated DPoP proof
type DPoPProof struct {
	// JWK is the public key used to verify the proof
	JWK jwk.Key
	
	// JTI is the JWT ID for replay protection
	JTI string
	
	// HTU is the HTTP URI claim
	HTU string
	
	// HTM is the HTTP method claim
	HTM string
	
	// IAT is the issued at time
	IAT time.Time
	
	// JKT is the JWK thumbprint for token binding
	JKT string
}

// ValidateDPoPProof validates a DPoP proof from HTTP headers
func (v *DPoPValidator) ValidateDPoPProof(req *http.Request) (*DPoPProof, error) {
	dpopHeader := req.Header.Get(DPoPHeaderName)
	if dpopHeader == "" {
		return nil, fmt.Errorf("missing DPoP header")
	}
	
	// Parse and validate the DPoP JWT
	proof, err := v.parseDPoPProof(dpopHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DPoP proof: %w", err)
	}
	
	// Validate the proof claims against the request
	if err := v.validateProofClaims(proof, req); err != nil {
		return nil, fmt.Errorf("DPoP proof validation failed: %w", err)
	}
	
	// Check for replay attacks
	if err := v.replayCache.CheckAndAdd(proof.JTI, proof.IAT); err != nil {
		return nil, fmt.Errorf("replay protection check failed: %w", err)
	}
	
	dpopLog.Debugf("DPoP proof validated successfully for JTI: %s", proof.JTI)
	return proof, nil
}

// parseDPoPProof parses and validates a DPoP proof JWT
func (v *DPoPValidator) parseDPoPProof(dpopToken string) (*DPoPProof, error) {
	// Parse the JWT without verification first to extract claims
	parsedJwt, err := jwt.Parse([]byte(dpopToken), jwt.WithVerify(false))
	if err != nil {
		return nil, fmt.Errorf("failed to parse DPoP JWT: %w", err)
	}
	
	// Extract required claims
	jti, ok := parsedJwt.Get("jti")
	if !ok {
		return nil, fmt.Errorf("missing jti claim")
	}
	
	htu, ok := parsedJwt.Get("htu")
	if !ok {
		return nil, fmt.Errorf("missing htu claim")
	}
	
	htm, ok := parsedJwt.Get("htm")
	if !ok {
		return nil, fmt.Errorf("missing htm claim")
	}
	
	iat, ok := parsedJwt.Get("iat")
	if !ok {
		return nil, fmt.Errorf("missing iat claim")
	}
	
	// Extract the JWK from the header
	headers := parsedJwt.Headers()
	jwkHeader, ok := headers.Get("jwk")
	if !ok {
		return nil, fmt.Errorf("missing jwk header")
	}
	
	jwkKey, ok := jwkHeader.(jwk.Key)
	if !ok {
		return nil, fmt.Errorf("invalid jwk header format")
	}
	
	// Verify the JWT signature using the embedded JWK
	if err := jwt.Verify([]byte(dpopToken), jwt.WithKey(jwkKey.Algorithm(), jwkKey)); err != nil {
		return nil, fmt.Errorf("DPoP proof signature verification failed: %w", err)
	}
	
	// Calculate JWK thumbprint for token binding
	jkt, err := calculateJWKThumbprint(jwkKey)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate JWK thumbprint: %w", err)
	}
	
	// Convert claims to proper types
	jtiStr, ok := jti.(string)
	if !ok {
		return nil, fmt.Errorf("invalid jti claim type")
	}
	
	htuStr, ok := htu.(string)
	if !ok {
		return nil, fmt.Errorf("invalid htu claim type")
	}
	
	htmStr, ok := htm.(string)
	if !ok {
		return nil, fmt.Errorf("invalid htm claim type")
	}
	
	iatFloat, ok := iat.(float64)
	if !ok {
		return nil, fmt.Errorf("invalid iat claim type")
	}
	
	return &DPoPProof{
		JWK: jwkKey,
		JTI: jtiStr,
		HTU: htuStr,
		HTM: htmStr,
		IAT: time.Unix(int64(iatFloat), 0),
		JKT: jkt,
	}, nil
}

// validateProofClaims validates DPoP proof claims against the HTTP request
func (v *DPoPValidator) validateProofClaims(proof *DPoPProof, req *http.Request) error {
	// Check if the proof is too old
	if time.Since(proof.IAT) > v.config.MaxAge {
		return fmt.Errorf("DPoP proof is too old: issued at %v, current time %v", proof.IAT, time.Now())
	}
	
	// Validate HTTP method
	if strings.ToUpper(proof.HTM) != req.Method {
		return fmt.Errorf("HTTP method mismatch: proof has %s, request has %s", proof.HTM, req.Method)
	}
	
	// Validate HTTP URI
	requestURI := getRequestURI(req)
	if proof.HTU != requestURI {
		return fmt.Errorf("HTTP URI mismatch: proof has %s, request has %s", proof.HTU, requestURI)
	}
	
	return nil
}

// ValidateTokenBinding validates that the access token is bound to the DPoP proof
func (v *DPoPValidator) ValidateTokenBinding(accessToken string, proof *DPoPProof) error {
	// Parse the access token to extract the jkt claim
	claims, err := extractJWTClaims(accessToken)
	if err != nil {
		return fmt.Errorf("failed to parse access token: %w", err)
	}
	
	// Get the jkt claim from the access token
	jktClaim, ok := claims["cnf"]
	if !ok {
		return fmt.Errorf("access token missing cnf claim for token binding")
	}
	
	// The cnf claim should be an object with a jkt field
	cnfObj, ok := jktClaim.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid cnf claim format in access token")
	}
	
	jktFromToken, ok := cnfObj["jkt"]
	if !ok {
		return fmt.Errorf("access token missing jkt in cnf claim")
	}
	
	jktFromTokenStr, ok := jktFromToken.(string)
	if !ok {
		return fmt.Errorf("invalid jkt claim type in access token")
	}
	
	// Compare the JKT from the token with the JKT from the DPoP proof
	if jktFromTokenStr != proof.JKT {
		return fmt.Errorf("token binding failed: access token jkt (%s) does not match DPoP proof jkt (%s)",
			jktFromTokenStr, proof.JKT)
	}
	
	dpopLog.Debugf("Token binding validated successfully for JKT: %s", proof.JKT)
	return nil
}

// getRequestURI constructs the full URI for the request
func getRequestURI(req *http.Request) string {
	scheme := "https"
	if req.TLS == nil && req.URL.Scheme != "https" {
		scheme = "http"
	}
	
	// Build the full URL
	u := url.URL{
		Scheme: scheme,
		Host:   req.Host,
		Path:   req.URL.Path,
	}
	
	// Include query parameters if present
	if req.URL.RawQuery != "" {
		u.RawQuery = req.URL.RawQuery
	}
	
	return u.String()
}

// calculateJWKThumbprint calculates the SHA-256 JWK thumbprint
func calculateJWKThumbprint(jwkKey jwk.Key) (string, error) {
	// Serialize the JWK without private keys
	jwkBytes, err := json.Marshal(jwkKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JWK: %w", err)
	}
	
	// Calculate SHA-256 hash
	hash := sha256.Sum256(jwkBytes)
	
	// Base64url encode the hash
	thumbprint := base64.URLEncoding.EncodeToString(hash[:])
	
	return thumbprint, nil
}

// extractJWTClaims extracts claims from a JWT token without verification
func extractJWTClaims(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}
	
	// Decode the payload
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}
	
	// Parse the claims
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}
	
	return claims, nil
}

// Close cleans up the validator resources
func (v *DPoPValidator) Close() {
	if v.replayCache != nil {
		v.replayCache.Close()
	}
}
