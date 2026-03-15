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
	"context"
	"fmt"
	"net/http"
	"sync"

	"istio.io/istio/pkg/log"
)

var (
	integrationLog = log.RegisterScope("dpop_integration", "DPoP integration debugging")
)

// DPoPManager manages DPoP validators across the system
type DPoPManager struct {
	validators map[string]*DPoPValidator
	mu         sync.RWMutex
}

// Global DPoP manager instance
var globalDPoPManager *DPoPManager
var managerOnce sync.Once

// GetDPoPManager returns the global DPoP manager
func GetDPoPManager() *DPoPManager {
	managerOnce.Do(func() {
		globalDPoPManager = &DPoPManager{
			validators: make(map[string]*DPoPValidator),
		}
	})
	return globalDPoPManager
}

// GetValidator returns a DPoP validator for the given configuration
func (m *DPoPManager) GetValidator(config *DPoPConfig) *DPoPValidator {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Create a simple config key (in practice, this could be more sophisticated)
	key := "default"
	
	if validator, exists := m.validators[key]; exists {
		return validator
	}
	
	// Create new validator
	validator := NewDPoPValidator(config)
	m.validators[key] = validator
	
	integrationLog.Debugf("Created new DPoP validator for config: %+v", config)
	return validator
}

// Close closes all validators
func (m *DPoPManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for _, validator := range m.validators {
		validator.Close()
	}
	m.validators = make(map[string]*DPoPValidator)
}

// DPoPValidationContext contains context for DPoP validation
type DPoPValidationContext struct {
	// Validator is the DPoP validator to use
	Validator *DPoPValidator
	
	// Required indicates whether DPoP is required for this request
	Required bool
	
	// AccessToken is the bearer token for token binding validation
	AccessToken string
}

// ValidateRequest performs complete DPoP validation for a request
func (ctx *DPoPValidationContext) ValidateRequest(req *http.Request) error {
	if ctx.Validator == nil {
		return nil // DPoP not configured
	}
	
	// Validate the DPoP proof
	proof, err := ctx.Validator.ValidateDPoPProof(req)
	if err != nil {
		if ctx.Required {
			return err
		}
		// DPoP is optional, log and continue
		integrationLog.Debugf("DPoP validation failed (optional): %v", err)
		return nil
	}
	
	// If we have an access token, validate token binding
	if ctx.AccessToken != "" {
		if err := ctx.Validator.ValidateTokenBinding(ctx.AccessToken, proof); err != nil {
			return err
		}
	}
	
	return nil
}

// HTTPRequestExtractor extracts HTTP request information for DPoP validation
type HTTPRequestExtractor interface {
	// ExtractRequest extracts the HTTP request from the context
	ExtractRequest(ctx context.Context) (*http.Request, error)
	
	// ExtractAccessToken extracts the access token from the request
	ExtractAccessToken(req *http.Request) (string, error)
}

// DefaultHTTPRequestExtractor implements HTTPRequestExtractor for standard HTTP requests
type DefaultHTTPRequestExtractor struct{}

// ExtractRequest extracts the HTTP request from the context
func (e *DefaultHTTPRequestExtractor) ExtractRequest(ctx context.Context) (*http.Request, error) {
	// In practice, this would extract from the specific context type
	// For now, this is a placeholder that would need to be implemented
	// based on the actual context structure used by Istio
	return nil, fmt.Errorf("not implemented: extract request from context")
}

// ExtractAccessToken extracts the access token from the request
func (e *DefaultHTTPRequestExtractor) ExtractAccessToken(req *http.Request) (string, error) {
	authHeader := req.Header.Get("Authorization")
	if authHeader == "" {
		return "", nil // No authorization header
	}
	
	// Extract Bearer token
	const bearerPrefix = "Bearer "
	if len(authHeader) < len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
		return "", fmt.Errorf("invalid authorization header format, expected Bearer token")
	}
	
	return authHeader[len(bearerPrefix):], nil
}

// ValidateRequestWithContext performs DPoP validation using the provided context
func ValidateRequestWithContext(
	ctx context.Context,
	validationCtx *DPoPValidationContext,
	extractor HTTPRequestExtractor,
) error {
	if validationCtx == nil || validationCtx.Validator == nil {
		return nil // DPoP not configured
	}
	
	// Extract the HTTP request
	req, err := extractor.ExtractRequest(ctx)
	if err != nil {
		return fmt.Errorf("failed to extract HTTP request: %w", err)
	}
	
	// Extract the access token if not already provided
	if validationCtx.AccessToken == "" {
		accessToken, err := extractor.ExtractAccessToken(req)
		if err != nil {
			return fmt.Errorf("failed to extract access token: %w", err)
		}
		validationCtx.AccessToken = accessToken
	}
	
	// Perform validation
	return validationCtx.ValidateRequest(req)
}
