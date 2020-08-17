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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/security"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var (
	// GKEClusterURL is the URL to send requests to the token exchange service.
	GKEClusterURL = env.RegisterStringVar("GKE_CLUSTER_URL", "", "The url of GKE cluster").Get()
	// SecureTokenEndpoint is the Endpoint the STS client calls to.
	SecureTokenEndpoint = "https://securetoken.googleapis.com/v1/identitybindingtoken"
	stsClientLog        = log.RegisterScope("stsclient", "STS client debugging", 0)
	GCEProvider         = "GoogleComputeEngine"
)

const (
	httpTimeOutInSec = 5
	contentType      = "application/json"
	scope            = "https://www.googleapis.com/auth/cloud-platform"
)

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"` // Expiration time in seconds
}

// TokenExchanger for google securetoken api interaction.
type Plugin struct {
	hTTPClient *http.Client
}

// NewPlugin returns an instance of secure token service client plugin
func NewPlugin() security.TokenExchanger {
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		stsClientLog.Errorf("Failed to get SystemCertPool: %v", err)
		return nil
	}
	return Plugin{
		hTTPClient: &http.Client{
			Timeout: httpTimeOutInSec * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: caCertPool,
				},
			},
		},
	}
}

// ExchangeToken exchange oauth access token from trusted domain and k8s sa jwt.
func (p Plugin) ExchangeToken(ctx context.Context, credFetcher security.CredFetcher, trustDomain, k8sSAjwt string) (
	string /*access token*/, time.Time /*expireTime*/, int /*httpRespCode*/, error) {
	aud := constructAudience(credFetcher, trustDomain)
	var jsonStr = constructFederatedTokenRequest(aud, k8sSAjwt)
	req, _ := http.NewRequest("POST", SecureTokenEndpoint, bytes.NewBuffer(jsonStr))
	req.Header.Set("Content-Type", contentType)

	resp, err := p.hTTPClient.Do(req)
	errMsg := "failed to call token exchange service. "
	if err != nil || resp == nil {
		statusCode := http.StatusServiceUnavailable
		// If resp is not null, return the actually status code returned from the token service.
		// If resp is null, return a service unavailable status and try again.
		if resp != nil {
			statusCode = resp.StatusCode
			errMsg += fmt.Sprintf("HTTP status: %s. Error: %v", resp.Status, err)
		} else {
			errMsg += fmt.Sprintf("HTTP response empty. Error: %v", err)
		}
		return "", time.Now(), statusCode, errors.New(errMsg)
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	respData := &federatedTokenResponse{}
	if err := json.Unmarshal(body, respData); err != nil {
		return "", time.Now(), resp.StatusCode, fmt.Errorf(
			"failed to unmarshal response data. HTTP status: %s. Error: %v. Body size: %d", resp.Status, err, len(body))
	}

	if respData.AccessToken == "" {
		return "", time.Now(), resp.StatusCode, fmt.Errorf(
			"exchanged empty token. HTTP status: %s. Response: %v", resp.Status, string(body))
	}

	return respData.AccessToken, time.Now().Add(time.Second * time.Duration(respData.ExpiresIn)), resp.StatusCode, nil
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
		"scope":              scope,
	}
	jsonValue, _ := json.Marshal(values)
	return jsonValue
}
