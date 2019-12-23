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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"

	"istio.io/istio/security/pkg/stsservice"
	"istio.io/pkg/log"
)

const (
	httpTimeOutInSec = 5
	maxRequestRetry  = 5
	contentType      = "application/json"
	scope            = "https://www.googleapis.com/auth/cloud-platform"
	tokenType        = "urn:ietf:params:oauth:token-type:access_token"
	federatedToken   = "federated token"
	accessToken      = "access token"
)

var (
	tokenManagerLog        = log.RegisterScope("tokenManager", "STS token manager debugging", 0)

	federatedTokenEndpoint = "https://securetoken.googleapis.com/v1/identitybindingtoken"
	// https://cloud.google.com/iam/docs/reference/credentials/rest/v1/projects.serviceAccounts/generateAccessToken
	accessTokenEndpoint = "https://iamcredentials.googleapis.com/v1/projects/-/" +
		"serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken"
)

// TokenManager supports token exchange with Google OAuth 2.0 authorization server.
type TokenManager struct {
	hTTPClient  *http.Client
	trustDomain string
	// tokens is the cache for fetched tokens.
	// map key is timestamp of token, map value is tokenInfo.
	tokens           sync.Map
	gCPProjectNumber string
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
func CreateTokenManager(trustDomain string, gCPProjectNumber string) (*TokenManager, error) {
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
		trustDomain:      trustDomain,
		gCPProjectNumber: gCPProjectNumber,
	}
	return tm, nil
}

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"` // Expiration time in seconds
}

// GenerateToken takes STS request parameters and fetches token, returns StsResponseParameters in JSON.
func (tm *TokenManager) GenerateToken(parameters stsservice.StsRequestParameters) ([]byte, error) {
	tokenManagerLog.Debugf("Start to fetch token with STS request parameters: %v", parameters)
	ftResp, err := tm.fetchFederatedToken(parameters)
	if err != nil {
		return nil, err
	}
	atResp, err := tm.fetchAccessToken(ftResp)
	if err != nil {
		return nil, err
	}
	return tm.generateSTSResp(atResp)
}

// constructFederatedTokenRequest returns an HTTP request for federated token.
// Example of a federated token request:
// POST https://securetoken.googleapis.com/v1/identitybindingtoken
// Content-Type: application/json
// {
//    audience: <trust domain>
//    grantType: urn:ietf:params:oauth:grant-type:token-exchange
//    requestedTokenType: urn:ietf:params:oauth:token-type:access_token
//    subjectTokenType: urn:ietf:params:oauth:token-type:jwt
//    subjectToken: <jwt token>
//    Scope: https://www.googleapis.com/auth/cloud-platform
// }
func (tm *TokenManager) constructFederatedTokenRequest(parameters stsservice.StsRequestParameters) *http.Request {
	reqScope := scope
	if len(parameters.Scope) != 0 {
		reqScope = parameters.Scope
	}
	query := map[string]string{
		"audience":           tm.trustDomain,
		"grantType":          parameters.GrantType,
		"requestedTokenType": tokenType,
		"subjectTokenType":   parameters.SubjectTokenType,
		"subjectToken":       parameters.SubjectToken,
		"Scope":              reqScope,
	}
	jsonQuery, _ := json.Marshal(query)
	req, _ := http.NewRequest("POST", federatedTokenEndpoint, bytes.NewBuffer(jsonQuery))
	req.Header.Set("Content-Type", contentType)
	reqDump, _ := httputil.DumpRequest(req, true)
	tokenManagerLog.Debugf("Generated federated token request: %s", string(reqDump))
	return req
}

// fetchFederatedToken exchanges a third-party issued Json Web Token for an OAuth2.0 access token
// which asserts a third-party identity within an identity namespace.
func (tm *TokenManager) fetchFederatedToken(parameters stsservice.StsRequestParameters) (*federatedTokenResponse, error) {
	respData := &federatedTokenResponse{}

	req := tm.constructFederatedTokenRequest(parameters)
	resp, err := tm.sendRequestWithRetry(req)
	if err != nil {
		respCode := 0
		if resp != nil {
			respCode = resp.StatusCode
		}
		tokenManagerLog.Errorf("Failed to exchange federated token (HTTP status %d): %v", respCode,
			err)
		return nil, fmt.Errorf("failed to exchange federated token (HTTP status %d): %v", respCode,
			err)
	}
	// resp should not be nil.
	defer resp.Body.Close()

	respDump, _ := httputil.DumpResponse(resp, true)
	tokenManagerLog.Debugf("Received federated token response: \n%s", string(respDump))

	body, _ := ioutil.ReadAll(resp.Body)
	if err := json.Unmarshal(body, respData); err != nil {
		tokenManagerLog.Errorf("Failed to unmarshal federated token response data: %v", err)
		return respData, fmt.Errorf("failed to unmarshal federated token response data: %v", err)
	}
	if respData.AccessToken == "" {
		tokenManagerLog.Errora("federated token response does not have access token", string(body))
		return respData, errors.New("federated token response does not have access token. " + string(body))
	}
	tokenManagerLog.Debug("successfully exchanged a federated token")
	tokenReceivedTime := time.Now()
	tm.tokens.Store(tokenReceivedTime.String(), tokenInfo{
		tokenType:  federatedToken,
		issueTime:  tokenReceivedTime.String(),
		expireTime: tokenReceivedTime.Add(time.Duration(respData.ExpiresIn) * time.Second).String()})
	return respData, nil
}

// Send HTTP request every 0.01 seconds until successfully receive response or hit max retry numbers.
// If response code is 4xx, return immediately without retry.
func (tm *TokenManager) sendRequestWithRetry(req *http.Request) (resp *http.Response, err error) {
	for i := 0; i < maxRequestRetry; i++ {
		resp, err = tm.hTTPClient.Do(req)
		if resp != nil && resp.StatusCode == http.StatusOK {
			return resp, err
		}
		if resp != nil && resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
			return resp, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		return resp, fmt.Errorf("HTTP Status %d, body: %s", resp.StatusCode, string(bodyBytes))
	}
	return resp, err
}

type accessTokenRequest struct {
	Name      string            `json:"Name"` // nolint: structcheck, unused
	Delegates []string          `json:"Delegates"`
	Scope     []string          `json:"Scope"`
	LifeTime  duration.Duration `json:"lifetime"` // nolint: structcheck, unused
}

type accessTokenResponse struct {
	AccessToken string            `json:"accessToken"`
	ExpireTime  duration.Duration `json:"expireTime"`
}

// constructFederatedTokenRequest returns an HTTP request for access token.
// Example of an access token request:
// POST https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/
// service-<GCP project number>@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken
// Content-Type: application/json
// Authorization: Bearer <federated token>
// {
//  "Delegates": [],
//  "Scope": [
//      https://www.googleapis.com/auth/cloud-platform
//  ],
// }
func (tm *TokenManager) constructGenerateAccessTokenRequest(fResp *federatedTokenResponse) *http.Request {
	query := accessTokenRequest{}
	query.Scope = append(query.Scope, scope)

	jsonQuery, _ := json.Marshal(query)
	endpoint := fmt.Sprintf(accessTokenEndpoint, tm.gCPProjectNumber)
	req, _ := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonQuery))
	req.Header.Add("Content-Type", contentType)
	req.Header.Add("Authorization", "Bearer "+fResp.AccessToken)
	reqDump, _ := httputil.DumpRequest(req, true)
	tokenManagerLog.Debugf("Prepared access token request: \n%s", string(reqDump))
	return req
}

func (tm *TokenManager) fetchAccessToken(federatedToken *federatedTokenResponse) (*accessTokenResponse, error) {
	respData := &accessTokenResponse{}

	req := tm.constructGenerateAccessTokenRequest(federatedToken)
	resp, err := tm.sendRequestWithRetry(req)
	if err != nil {
		respCode := 0
		if resp != nil {
			respCode = resp.StatusCode
		}
		tokenManagerLog.Errorf("failed to exchange access token (HTTP status %d): %v", respCode, err)
		return respData, fmt.Errorf("failed to exchange access token (HTTP status %d): %v", respCode, err)
	}
	defer resp.Body.Close()
	respDump, _ := httputil.DumpResponse(resp, true)
	tokenManagerLog.Debugf("Received access token response: \n%s", string(respDump))

	body, _ := ioutil.ReadAll(resp.Body)
	if err := json.Unmarshal(body, respData); err != nil {
		tokenManagerLog.Errorf("Failed to unmarshal access token response data: %v", err)
		return respData, fmt.Errorf("failed to unmarshal access token response data: %v", err)
	}
	if respData.AccessToken == "" {
		tokenManagerLog.Errora("access token response does not have access token", string(body))
		return respData, errors.New("access token response does not have access token. " + string(body))
	}
	tokenManagerLog.Debug("successfully exchanged an access token")
	tokenReceivedTime := time.Now()
	expireTime, _ := ptypes.Duration(&respData.ExpireTime)
	tm.tokens.Store(tokenReceivedTime.String(), tokenInfo{
		tokenType:  accessToken,
		issueTime:  tokenReceivedTime.String(),
		expireTime: tokenReceivedTime.Add(expireTime).String()})
	return respData, nil
}

// generateSTSResp takes accessTokenResponse and generates StsResponseParameters in JSON.
func (tm *TokenManager) generateSTSResp(atResp *accessTokenResponse) ([]byte, error) {
	expireTime, _ := ptypes.Duration(&atResp.ExpireTime)
	stsRespParam := stsservice.StsResponseParameters{
		AccessToken:     atResp.AccessToken,
		IssuedTokenType: tokenType,
		TokenType:       "Bearer",
		ExpiresIn:       int64(expireTime.Seconds()),
	}
	statusJSON, err := json.MarshalIndent(stsRespParam, "", " ")
	return statusJSON, err
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
