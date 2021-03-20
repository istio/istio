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

// Package stsclient is for oauth token exchange integration.
package stsclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/monitoring"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var (
	// GKEClusterURL is the URL to send requests to the token exchange service.
	GKEClusterURL = env.RegisterStringVar("GKE_CLUSTER_URL", "", "The url of GKE cluster").Get()
	// SecureTokenEndpoint is the Endpoint the STS client calls to.
	SecureTokenEndpoint = "https://securetoken.googleapis.com/v1/identitybindingtoken"
	stsClientLog        = log.RegisterScope("stsclient", "STS client debugging", 0)
)

const (
	httpTimeout = time.Second * 5
	contentType = "application/json"
	Scope       = "https://www.googleapis.com/auth/cloud-platform"
)

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"` // Expiration time in seconds
}

// TokenExchanger for google securetoken api interaction.
type SecureTokenServiceExchanger struct {
	httpClient  *http.Client
	credFetcher security.CredFetcher
	trustDomain string
	backoff     time.Duration
}

// NewSecureTokenServiceExchanger returns an instance of secure token service client plugin
func NewSecureTokenServiceExchanger(credFetcher security.CredFetcher, trustDomain string) *SecureTokenServiceExchanger {
	return &SecureTokenServiceExchanger{
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		backoff:     time.Millisecond * 50,
		credFetcher: credFetcher,
		trustDomain: trustDomain,
	}
}

func retryable(code int) bool {
	return code >= 500 && !(code == 501 || code == 505 || code == 511)
}

func (p *SecureTokenServiceExchanger) requestWithRetry(reqBytes []byte) ([]byte, error) {
	attempts := 0
	var lastError error
	for attempts < 5 {
		attempts++
		req, err := http.NewRequest("POST", SecureTokenEndpoint, bytes.NewBuffer(reqBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastError = err
			stsClientLog.Debugf("token exchange request failed: %v", err)
			time.Sleep(p.backoff)
			monitoring.NumOutgoingRetries.With(monitoring.RequestType.Value(monitoring.TokenExchange)).Increment()
			continue
		}
		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			return body, err
		}
		if retryable(resp.StatusCode) {
			body, _ := ioutil.ReadAll(resp.Body)
			lastError = fmt.Errorf("token exchange request failed: status code %v", resp.StatusCode)
			stsClientLog.Debugf("token exchange request failed: status code %v, body %v", resp.StatusCode, string(body))
			resp.Body.Close()
			time.Sleep(p.backoff)
			monitoring.NumOutgoingRetries.With(monitoring.RequestType.Value(monitoring.TokenExchange)).Increment()
			continue
		}
	}
	return nil, fmt.Errorf("exchange failed all retries, last error: %v", lastError)
}

// ExchangeToken exchange oauth access token from trusted domain and k8s sa jwt.
func (p *SecureTokenServiceExchanger) ExchangeToken(k8sSAjwt string) (string, error) {
	aud := constructAudience(p.credFetcher, p.trustDomain)
	jsonStr := constructFederatedTokenRequest(aud, k8sSAjwt)

	body, err := p.requestWithRetry(jsonStr)
	if err != nil {
		return "", fmt.Errorf("token exchange failed (aud: %s): %v", aud, err)
	}
	respData := &federatedTokenResponse{}
	if err := json.Unmarshal(body, respData); err != nil {
		// Normally the request should json - extremely hard to debug otherwise, not enough info in status/err
		stsClientLog.Debugf("Unexpected unmarshal error, response was %s", string(body))
		return "", fmt.Errorf("(aud: %s), failed to unmarshal response data of size %v: %v",
			aud, len(body), err)
	}

	if respData.AccessToken == "" {
		return "", fmt.Errorf(
			"exchanged empty token (aud: %s), response: %v", aud, string(body))
	}

	return respData.AccessToken, nil
}

func constructAudience(credFetcher security.CredFetcher, trustDomain string) string {
	provider := ""
	if credFetcher != nil {
		provider = credFetcher.GetIdentityProvider()
	}
	// For GKE, we do not register IdentityProvider explicitly. The provider name
	// is GKEClusterURL by default.
	if provider == "" {
		if GKEClusterURL != "" {
			provider = GKEClusterURL
		} else if platform.IsGCP() {
			stsClientLog.Warn("GKE_CLUSTER_URL is not set, fallback to call metadata server to get identity provider")
			provider = platform.NewGCP().Metadata()[platform.GCPClusterURL]
		}
	}
	return fmt.Sprintf("identitynamespace:%s:%s", trustDomain, provider)
}

func constructFederatedTokenRequest(aud, jwt string) []byte {
	values := map[string]string{
		"audience":           aud,
		"grantType":          "urn:ietf:params:oauth:grant-type:token-exchange",
		"requestedTokenType": "urn:ietf:params:oauth:token-type:access_token",
		"subjectTokenType":   "urn:ietf:params:oauth:token-type:jwt",
		"subjectToken":       jwt,
		"scope":              Scope,
	}
	jsonValue, _ := json.Marshal(values)
	return jsonValue
}
