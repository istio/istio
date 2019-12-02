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

package tokenmanager

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"time"
	"encoding/json"
	"istio.io/pkg/log"
)

const (
	httpTimeOutInSec    = 5
	contentType         = "application/json"
	scope               = "https://www.googleapis.com/auth/cloud-platform"
    federatedTokenType  = "urn:ietf:params:oauth:token-type:access_token"
	secureTokenEndpoint = "https://securetoken.googleapis.com/v1/identitybindingtoken"
)

var tokenManagerLog = log.RegisterScope("tokenManagerLog", "STS token manager debugging", 0)

// StsResponseParameters stores all attributes sent as JSON in a successful STS
// response. These attributes are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.1
type StsResponseParameters struct {
	accessToken 		string `json:"access_token"`      // Required
	issuedTokenType     string `json:"issued_token_type"` // Required
	tokenType           string `json:"token_type"`        // Required
	expiresIn           string `json:"expires_in"`
	scope               string `json:"scope"`
	refreshToken        string `json:"refresh_token"`
}

// StsRequestParameters stores all STS request attributes defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.1
type StsRequestParameters struct {
	grantType            string  // Required
	resource             string
	audience             string
	scope                string
	requestedTokenType   string
	subjectToken         string  // Required
	subjectTokenType     string  // Required
	actorToken           string
	actorTokenType       string
}

// TokenManager contains methods for fetching token. It implements the interface declared at
//
type TokenManager struct {
	hTTPClient  *http.Client
	trustDomain string
}

func CreateTokenManager(trustDomain string) (*TokenManager, error) {
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		tokenManagerLog.Errorf("Failed to get SystemCertPool: %v", err)
		return nil, err
	}
	tm := &TokenManager{
		hTTPClient: &http.Client{
			Timeout: httpTimeOutInSec * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: caCertPool,
				},
			},
		},
		trustDomain: trustDomain,
	}
	return tm, nil
}

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"` // Expiration time in seconds
}

func (tm *TokenManager) constructFederatedTokenRequest(attributes StsRequestParameters) []byte {
	values := map[string]string{
		"audience":           tm.trustDomain,
		"grantType":          attributes.grantType,
		"requestedTokenType": federatedTokenType,
		"subjectTokenType":   attributes.subjectTokenType,
		"subjectToken":       attributes.subjectToken,
		"scope":              attributes.scope,
	}
	jsonValue, _ := json.Marshal(values)
	return jsonValue
}

// GenerateToken takes STS request parameters and fetches token, returns StsResponseParameters in JSON.
func (tm *TokenManager) GenerateToken(attributes StsRequestParameters) ([]byte, error) {
	tokenManagerLog.Debugf("Start to fetch token with STS request parameters: %v", attributes)
	ftResp, err := tm.fetchFederatedToken(attributes)
	if err != nil {
		return nil, err
	}
	atResp, err := tm.fetchAccessToken(ftResp)
	if err != nil {
		return nil, err
	}
	return tm.generateSTSResp(atResp), nil
}

func (tm *TokenManager) fetchFederatedToken(attributes StsRequestParameters) (federatedTokenResponse, error) {
	respData := federatedTokenResponse{}

	federatedTokenReqJSON := tm.constructFederatedTokenRequest(attributes)
	req, _ := http.NewRequest("POST", secureTokenEndpoint, bytes.NewBuffer(federatedTokenReqJSON))
	req.Header.Set("Content-Type", contentType)
	resp, err := tm.hTTPClient.Do(req)
	if err != nil {
		tokenManagerLog.Errorf("Failed to get federated token (HTTP status %d): %s", resp.Status,
			err.Error())
		return respData, fmt.Errorf("failed to get federated token (HTTP status %d): %s", resp.Status,
			err.Error())
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	if err := json.Unmarshal(body, respData); err != nil {
		tokenManagerLog.Errorf("Failed to unmarshal federated token response data: %s", err.Error())
		return respData, fmt.Errorf("failed to unmarshal federated token response data: %s", err.Error())
	}
	if respData.AccessToken == "" {
		tokenManagerLog.Errora("Failed to exchange federated token", string(body))
		return respData, errors.New("failed to exchange federated token " + string(body))
	}
	return respData, nil
}

// DumpTokenStatus dumps all token status in JSON
func (tm *TokenManager) DumpTokenStatus() ([]byte, error) {
}


