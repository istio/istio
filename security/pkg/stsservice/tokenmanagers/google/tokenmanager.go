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
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	"gopkg.in/square/go-jose.v2/jwt"
	"io/ioutil"
	"istio.io/istio/security/pkg/stsservice"
	"istio.io/pkg/log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	httpTimeOutInSec       = 5
	maxRequestRetry        = 5
	contentType            = "application/json"
	scope                  = "https://www.googleapis.com/auth/cloud-platform"
    tokenType              = "urn:ietf:params:oauth:token-type:access_token"
	federatedTokenEndpoint = "https://securetoken.googleapis.com/v1/identitybindingtoken"
	// https://cloud.google.com/iam/docs/reference/credentials/rest/v1/projects.serviceAccounts/generateAccessToken
	accessTokenEndpoint    = "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/service-%d@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken"
	// In GenerateAccessTokenRequest, the `name` field should be in the format `projects/-/serviceAccounts/{account or project ID}`
	serviceAccountPrefix = "projects/-/serviceAccounts/"
)

var tokenManagerLog = log.RegisterScope("tokenManagerLog", "STS token manager debugging", 0)

// TokenManager supports token exchange with Google OAuth 2.0 authorization server.
type TokenManager struct {
	hTTPClient  *http.Client
	trustDomain string
	// tokens is the cache for fetched tokens.
	// map key is timestamp of token, map value is tokenInfo.
	tokens        sync.Map
	gCPProjectNumber int
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
func CreateTokenManager(trustDomain string, gCPProjectNumber int) (*TokenManager, error) {
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

// constructFederatedTokenRequest returns a query in JSON concatenating all request parameters.
func (tm *TokenManager) constructFederatedTokenRequest(parameters stsservice.StsRequestParameters) []byte {
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
		"scope":              reqScope,
	}
	jsonQuery, _ := json.Marshal(query)
	return jsonQuery
}

// GenerateToken takes STS request parameters and fetches token, returns StsResponseParameters in JSON.
func (tm *TokenManager) GenerateToken(parameters stsservice.StsRequestParameters) ([]byte, error) {
	tokenManagerLog.Debugf("Start to fetch token with STS request parameters: %v", parameters)
	ftResp, err := tm.fetchFederatedToken(parameters)
	if err != nil {
		return nil, err
	}
	serviceAccount, err := tm.extractServiceAccountFromJWT(parameters.SubjectToken)
	if err != nil {
		return nil, err
	}
	atResp, err := tm.fetchAccessToken(ftResp, serviceAccount)
	if err != nil {
		return nil, err
	}
	return tm.generateSTSResp(atResp)
}

// fetchFederatedToken exchanges a third-party issued Json Web Token for an OAuth2.0 access token
// which asserts a third-party identity within an identity namespace.
func (tm *TokenManager) fetchFederatedToken(parameters stsservice.StsRequestParameters) (federatedTokenResponse, error) {
	respData := federatedTokenResponse{}

	jsonQuery := tm.constructFederatedTokenRequest(parameters)
	req, _ := http.NewRequest("POST", federatedTokenEndpoint, bytes.NewBuffer(jsonQuery))
	req.Header.Set("Content-Type", contentType)
	resp, err := tm.sendRequestWithRetry(req)
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

// Send HTTP request every 0.01 seconds until successfully receive response or hit max retry numbers.
// If response code is 4xx, return immediately without retry.
func (tm *TokenManager) sendRequestWithRetry(req *http.Request) (resp *http.Response, err error) {
	for i := 0; i < maxRequestRetry; i++ {
		resp, err = tm.hTTPClient.Do(req)
		if err == nil {
			return resp, err
		}
		if resp != nil && resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
			return resp, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return resp, err
}

type accessTokenResponse struct {
	AccessToken     string `json:"access_token"`
	ExpireTime      duration.Duration `json:"expire_time"`
}

func (tm *TokenManager) extractServiceAccountFromJWT(tokenStr string) (string, error) {
	token, err := jwt.ParseSigned(tokenStr)
	if err != nil {
		tokenManagerLog.Errorf("failed to parse Kubernetes JWT: %v", err)
		return "", err
	}
	tokenClaims := jwt.Claims{}
	if err := token.UnsafeClaimsWithoutVerification(tokenClaims); err != nil {
		tokenManagerLog.Errorf("failed to deserialize the claims of a JWT: %v", err)
		return "", err
	}
	tokenManagerLog.Debugf("extracted service account from JWT: %s", tokenClaims.Subject)
	stringSlice := strings.Split(tokenClaims.Subject, ":")
	if len(stringSlice) > 0 {
		return stringSlice[len(stringSlice) - 1], nil
 	}
	return "", fmt.Errorf("failed to find service account from JWT claims, sub is %s", tokenClaims.Subject)
}

// constructFederatedTokenRequest returns a query in JSON concatenating all request parameters.
func (tm *TokenManager) constructGenerateAccessTokenRequest(_ federatedTokenResponse, sub string) []byte {
	reqScope := scope
	name := serviceAccountPrefix + sub
	query := map[string]string{
		"name":           name,
		"scope":          reqScope,
	}
	jsonQuery, _ := json.Marshal(query)
	return jsonQuery
}

func (tm *TokenManager) fetchAccessToken(federatedToken federatedTokenResponse, sub string) (accessTokenResponse, error) {
	respData := accessTokenResponse{}

	jsonQuery := tm.constructGenerateAccessTokenRequest(federatedToken, sub)
	endpoint := fmt.Sprintf(accessTokenEndpoint, tm.gCPProjectNumber)
	req, _ := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonQuery))
	req.Header.Set("Content-Type", contentType)
	req.Header.Add("Authorization", "Bearer " + federatedToken.AccessToken)
	resp, err := tm.sendRequestWithRetry(req)
	if err != nil {
		tokenManagerLog.Errorf("Failed to exchange access token (HTTP status %d): %s", resp.Status,
			err.Error())
		return respData, fmt.Errorf("failed to exchange access token (HTTP status %d): %s", resp.Status,
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
	tokenManagerLog.Debug("successfully exchanged an access token")
	tokenReceivedTime := time.Now()
	expireTimeInSec, _ := ptypes.Duration(&respData.ExpireTime)
	tm.tokens.Store(tokenReceivedTime.String(), tokenInfo{
		tokenType:  tokenType,
		issueTime:  tokenReceivedTime.String(),
		expireTime: tokenReceivedTime.Add(expireTimeInSec).String()})
	return respData, nil
}

// generateSTSResp takes accessTokenResponse and generates StsResponseParameters in JSON.
func (tm *TokenManager) generateSTSResp(atResp accessTokenResponse) ([]byte, error) {
	expireTimeInSec, _ := ptypes.Duration(&atResp.ExpireTime)
	stsRespParam := stsservice.StsResponseParameters{
		AccessToken: atResp.AccessToken,
		IssuedTokenType: tokenType,
		TokenType: "Bearer",
		ExpiresIn: int64(expireTimeInSec.Seconds()),
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


