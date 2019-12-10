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

package google

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

// TokenManager supports token exchange with Google OAuth 2.0 authorization server.
type TokenManager struct {
	hTTPClient  *http.Client
	trustDomain string
	// tokens is the cache for fetched tokens.
	// map key is timestamp of token, map value is tokenInfo.
	tokens        sync.Map
}

type tokenInfo struct {
	tokenType  string `json:"token_type"`
	issueTime  string `json:"issue_time"`
	expireTime string `json:"expire_time"`
}

type tokensDump struct {
	tokens []tokenInfo `json:"tokens"`
}

// CreateTokenManager creates a token manager that fetches token from a Google OAuth 2.0 authorization server.
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

// constructFederatedTokenRequest returns a query in JSON concatenating all request parameters.
func (tm *TokenManager) constructFederatedTokenRequest(parameters StsRequestParameters) []byte {
	reqScope := scope
	if len(parameters.scope) != 0 {
		reqScope = parameters.scope
	}
	query := map[string]string{
		"audience":           tm.trustDomain,
		"grantType":          parameters.grantType,
		"requestedTokenType": federatedTokenType,
		"subjectTokenType":   parameters.subjectTokenType,
		"subjectToken":       parameters.subjectToken,
		"scope":              reqScope,
	}
	jsonQuery, _ := json.Marshal(query)
	return jsonQuery
}

// GenerateToken takes STS request parameters and fetches token, returns StsResponseParameters in JSON.
func (tm *TokenManager) GenerateToken(parameters StsRequestParameters) ([]byte, error) {
	tokenManagerLog.Debugf("Start to fetch token with STS request parameters: %v", parameters)
	ftResp, err := tm.fetchFederatedToken(parameters)
	if err != nil {
		return nil, err
	}
	atResp, err := tm.fetchAccessToken(ftResp)
	if err != nil {
		return nil, err
	}
	return tm.generateSTSResp(atResp), nil
}

// fetchFederatedToken exchanges a third-party issued Json Web Token for an OAuth2.0 access token
// which asserts a third-party identity within an identity namespace.
func (tm *TokenManager) fetchFederatedToken(parameters StsRequestParameters) (federatedTokenResponse, error) {
	respData := federatedTokenResponse{}

	jsonQuery := tm.constructFederatedTokenRequest(parameters)
	req, _ := http.NewRequest("POST", secureTokenEndpoint, bytes.NewBuffer(jsonQuery))
	req.Header.Set("Content-Type", contentType)
	resp, err := tm.hTTPClient.Do(req)
	if err != nil {
		tokenManagerLog.Errorf("Failed to exchange federated token (HTTP status %d): %s", resp.Status,
			err.Error())
		return respData, fmt.Errorf("failed to exchange federated token (HTTP status %d): %s", resp.Status,
			err.Error())
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	if err := json.Unmarshal(body, respData); err != nil {
		tokenManagerLog.Errorf("Failed to unmarshal federated token response data: %s", err.Error())
		return respData, fmt.Errorf("failed to unmarshal federated token response data: %s", err.Error())
	}
	if respData.AccessToken == "" {
		tokenManagerLog.Errora("federated token response does not have access token", string(body))
		return respData, errors.New("federated token response does not have access token. " + string(body))
	}
	tokenManagerLog.Debug("successfully exchanged a federated token")
	tokenReceivedTime := time.Now()
	tm.tokens.Store(tokenReceivedTime.String(), tokenInfo{
		tokenType:  respData.IssuedTokenType,
		issueTime:  tokenReceivedTime.String(),
		expireTime: tokenReceivedTime.Add(time.Duration(respData.ExpiresIn) * time.Second).String()})
	return respData, nil
}

type accessTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"` // Expiration time in seconds
}

func (tm *TokenManager) fetchAccessToken(federatedToken federatedTokenResponse) (accessTokenResponse, error) {

}

// DumpTokenStatus dumps all token status in JSON
func (tm *TokenManager) DumpTokenStatus() ([]byte, error) {
	tokenStatus := make([]tokenInfo, 0)
	tm.tokens.Range(func(k interface{}, v interface{}) bool {
		token := v.(tokenInfo)
		tokenStatus = append(tokenStatus, token)
		return true
	})
	td := tokensDump{
		tokens: tokenStatus,
	}
	statusJSON, err := json.MarshalIndent(td, "", " ")
	return statusJSON, err
}


