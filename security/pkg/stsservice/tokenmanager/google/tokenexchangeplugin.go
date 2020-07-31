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

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/stsservice"
	"istio.io/pkg/log"
)

const (
	httpTimeOutInSec = 5
	maxRequestRetry  = 5
	cacheHitDivisor  = 50
	contentType      = "application/json"
	scope            = "https://www.googleapis.com/auth/cloud-platform"
	tokenType        = "urn:ietf:params:oauth:token-type:access_token"
	federatedToken   = "federated token"
	accessToken      = "access token"
)

var (
	pluginLog              = log.RegisterScope("token", "token manager plugin debugging", 0)
	federatedTokenEndpoint = "https://securetoken.googleapis.com/v1/identitybindingtoken"
	accessTokenEndpoint    = "https://iamcredentials.googleapis.com/v1/projects/-/" +
		"serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken"
	// default grace period in seconds of an access token. If caching is enabled and token remaining life time is
	// within this period, refresh access token.
	defaultGracePeriod = 300
	GCEProvider        = "GoogleComputeEngine"
)

// Plugin supports token exchange with Google OAuth 2.0 authorization server.
type Plugin struct {
	httpClient  *http.Client
	credFetcher security.CredFetcher
	trustDomain string
	// tokens is the cache for fetched tokens.
	// map key is token type, map value is tokenInfo.
	tokens           sync.Map
	gcpProjectNumber string
	gkeClusterURL    string
	enableCache      bool

	// Counts numbers of access token cache hits.
	mutex               sync.RWMutex
	accessTokenCacheHit uint64
}

// CreateTokenManagerPlugin creates a plugin that fetches token from a Google OAuth 2.0 authorization server.
func CreateTokenManagerPlugin(credFetcher security.CredFetcher, trustDomain, gcpProjectNumber, gkeClusterURL string, enableCache bool) (*Plugin, error) {
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		pluginLog.Errorf("Failed to get SystemCertPool: %v", err)
		return nil, err
	}
	p := &Plugin{
		httpClient: &http.Client{
			Timeout: httpTimeOutInSec * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: caCertPool,
				},
			},
		},
		credFetcher:      credFetcher,
		trustDomain:      trustDomain,
		gcpProjectNumber: gcpProjectNumber,
		gkeClusterURL:    gkeClusterURL,
		enableCache:      enableCache,
	}
	return p, nil
}

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"` // Expiration time in seconds
}

// GenerateToken takes STS request parameters and fetches token, returns StsResponseParameters in JSON.
func (p *Plugin) ExchangeToken(parameters stsservice.StsRequestParameters) ([]byte, error) {
	if tokenSTS, ok := p.useCachedToken(); ok {
		return tokenSTS, nil
	}
	pluginLog.Debugf("Start to fetch token with STS request parameters: %v", parameters)
	ftResp, err := p.fetchFederatedToken(parameters)
	if err != nil {
		return nil, err
	}
	atResp, err := p.fetchAccessToken(ftResp)
	if err != nil {
		return nil, err
	}
	return p.generateSTSResp(atResp)
}

// useCachedToken checks if there is a cached access token which is not going to expire soon. Returns
// cached token in STS response or false if token is not available.
func (p *Plugin) useCachedToken() ([]byte, bool) {
	if !p.enableCache {
		return nil, false
	}
	v, ok := p.tokens.Load(accessToken)
	if !ok {
		return nil, false
	}

	var cacheHitCount uint64
	p.mutex.Lock()
	p.accessTokenCacheHit++
	cacheHitCount = p.accessTokenCacheHit
	p.mutex.Unlock()

	token := v.(stsservice.TokenInfo)
	remainingLife := time.Until(token.ExpireTime)
	if cacheHitCount%cacheHitDivisor == 0 {
		pluginLog.Debugf("find a cached access token with remaining lifetime: %s (number of cache hits: %d)",
			remainingLife.String(), cacheHitCount)
	}
	if remainingLife > time.Duration(defaultGracePeriod)*time.Second {
		expireInSec := int64(remainingLife.Seconds())
		if tokenSTS, err := p.generateSTSRespInner(token.Token, expireInSec); err == nil {
			if cacheHitCount%cacheHitDivisor == 0 {
				pluginLog.Debugf("generated an STS response using a cached access token")
			}
			return tokenSTS, true
		}
	}
	return nil, false
}

// Construct the audience field for GetFederatedToken request.
func (p *Plugin) constructAudience() string {
	provider := ""
	if p.credFetcher != nil {
		provider = p.credFetcher.GetIdentityProvider()
	}
	// For GKE, we do not register IdentityProvider explicitly. The provider name
	// is GKEClusterURL by default.
	if provider == "" {
		provider = p.gkeClusterURL
	}
	return fmt.Sprintf("identitynamespace:%s:%s", p.trustDomain, provider)
}

// constructFederatedTokenRequest returns an HTTP request for federated token.
// Example of a federated token request:
// POST https://securetoken.googleapis.com/v1/identitybindingtoken
// Content-Type: application/json
// {
//    audience: <trust domain>:<provider>
//    grantType: urn:ietf:params:oauth:grant-type:token-exchange
//    requestedTokenType: urn:ietf:params:oauth:token-type:access_token
//    subjectTokenType: urn:ietf:params:oauth:token-type:jwt
//    subjectToken: <jwt token>
//    Scope: https://www.googleapis.com/auth/cloud-platform
// }
func (p *Plugin) constructFederatedTokenRequest(parameters stsservice.StsRequestParameters) (*http.Request, error) {
	reqScope := scope
	if len(parameters.Scope) != 0 {
		reqScope = parameters.Scope
	}
	aud := p.constructAudience()
	query := map[string]string{
		"audience":           aud,
		"grantType":          parameters.GrantType,
		"requestedTokenType": tokenType,
		"subjectTokenType":   parameters.SubjectTokenType,
		"subjectToken":       parameters.SubjectToken,
		"scope":              reqScope,
	}
	jsonQuery, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query for get federated token request: %+v", err)
	}
	req, err := http.NewRequest("POST", federatedTokenEndpoint, bytes.NewBuffer(jsonQuery))
	if err != nil {
		return req, fmt.Errorf("failed to create get federated token request: %+v", err)
	}
	req.Header.Set("Content-Type", contentType)
	if pluginLog.DebugEnabled() {
		dQuery := map[string]string{
			"audience":           aud,
			"grantType":          parameters.GrantType,
			"requestedTokenType": tokenType,
			"subjectTokenType":   parameters.SubjectTokenType,
			"subjectToken":       "redacted",
			"scope":              reqScope,
		}
		dJSONQuery, _ := json.Marshal(dQuery)
		dReq, _ := http.NewRequest("POST", federatedTokenEndpoint, bytes.NewBuffer(dJSONQuery))
		dReq.Header.Set("Content-Type", contentType)
		reqDump, _ := httputil.DumpRequest(dReq, true)
		pluginLog.Debugf("Prepared federated token request: \n%s", string(reqDump))
	} else {
		pluginLog.Info("Prepared federated token request")
	}
	return req, nil
}

// fetchFederatedToken exchanges a third-party issued Json Web Token for an OAuth2.0 access token
// which asserts a third-party identity within an identity namespace.
func (p *Plugin) fetchFederatedToken(parameters stsservice.StsRequestParameters) (*federatedTokenResponse, error) {
	respData := &federatedTokenResponse{}

	req, err := p.constructFederatedTokenRequest(parameters)
	if err != nil {
		pluginLog.Errorf("failed to create get federated token request: %+v", err)
		return nil, err
	}
	resp, timeElapsed, err := p.sendRequestWithRetry(req)
	if err != nil {
		respCode := 0
		if resp != nil {
			respCode = resp.StatusCode
		}
		pluginLog.Errorf("Failed to exchange federated token (HTTP status %d, total time elapsed %s): %v",
			respCode, timeElapsed.String(), err)
		return nil, fmt.Errorf("failed to exchange federated token (HTTP status %d): %v", respCode,
			err)
	}
	// resp should not be nil.
	defer resp.Body.Close()

	if pluginLog.DebugEnabled() {
		respDump, _ := httputil.DumpResponse(resp, false)
		pluginLog.Debugf("Received federated token response after %s: \n%s",
			timeElapsed.String(), string(respDump))
	} else {
		pluginLog.Infof("Received federated token response after %s", timeElapsed.String())
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		pluginLog.Errorf("Failed to read federated token response body: %+v", err)
		return respData, fmt.Errorf("failed to read federated token response body: %+v", err)
	}
	if err := json.Unmarshal(body, respData); err != nil {
		pluginLog.Errorf("Failed to unmarshal federated token response data: %v", err)
		return respData, fmt.Errorf("failed to unmarshal federated token response data: %v", err)
	}
	if respData.AccessToken == "" {
		pluginLog.Errora("federated token response does not have access token", string(body))
		return respData, errors.New("federated token response does not have access token. " + string(body))
	}
	pluginLog.Infof("Federated token will expire in %d seconds", respData.ExpiresIn)
	tokenReceivedTime := time.Now()
	p.tokens.Store(federatedToken, stsservice.TokenInfo{
		TokenType:  federatedToken,
		IssueTime:  tokenReceivedTime,
		ExpireTime: tokenReceivedTime.Add(time.Duration(respData.ExpiresIn) * time.Second)})
	return respData, nil
}

// Send HTTP request every 0.01 seconds until successfully receive response or hit max retry numbers.
// If response code is 4xx, return immediately without retry.
func (p *Plugin) sendRequestWithRetry(req *http.Request) (resp *http.Response, elapsedTime time.Duration, err error) {
	start := time.Now()
	for i := 0; i < maxRequestRetry; i++ {
		resp, err = p.httpClient.Do(req)
		if err != nil {
			pluginLog.Errorf("failed to send out request: %v (response: %v)", err, resp)
		}
		if resp != nil && resp.StatusCode == http.StatusOK {
			return resp, time.Since(start), err
		}
		if resp != nil && resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
			return resp, time.Since(start), err
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		return resp, time.Since(start), fmt.Errorf("HTTP Status %d, body: %s", resp.StatusCode, string(bodyBytes))
	}
	return resp, time.Since(start), err
}

type Duration struct {
	// Signed seconds of the span of time. Must be from -315,576,000,000
	// to +315,576,000,000 inclusive. Note: these bounds are computed from:
	// 60 sec/min * 60 min/hr * 24 hr/day * 365.25 days/year * 10000 years
	Seconds int64 `json:"seconds"`
}

type accessTokenRequest struct {
	Name      string   `json:"name"` // nolint: structcheck, unused
	Delegates []string `json:"delegates"`
	Scope     []string `json:"scope"`
	LifeTime  Duration `json:"lifetime"` // nolint: structcheck, unused
}

type accessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpireTime  string `json:"expireTime"`
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
func (p *Plugin) constructGenerateAccessTokenRequest(fResp *federatedTokenResponse) (*http.Request, error) {
	// Request for access token with a lifetime of 3600 seconds.
	query := accessTokenRequest{
		LifeTime: Duration{Seconds: 3600},
	}
	query.Scope = append(query.Scope, scope)

	jsonQuery, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query for get access token request: %+v", err)
	}
	endpoint := fmt.Sprintf(accessTokenEndpoint, p.gcpProjectNumber)
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonQuery))
	if err != nil {
		return nil, fmt.Errorf("failed to create get access token request: %+v", err)
	}
	req.Header.Add("Content-Type", contentType)
	if pluginLog.DebugEnabled() {
		reqDump, _ := httputil.DumpRequest(req, true)
		pluginLog.Debugf("Prepared access token request: \n%s", string(reqDump))
	} else {
		pluginLog.Info("Prepared access token request")
	}
	req.Header.Add("Authorization", "Bearer "+fResp.AccessToken)
	return req, nil
}

func (p *Plugin) fetchAccessToken(federatedToken *federatedTokenResponse) (*accessTokenResponse, error) {
	respData := &accessTokenResponse{}

	req, err := p.constructGenerateAccessTokenRequest(federatedToken)
	if err != nil {
		pluginLog.Errorf("failed to create get access token request: %+v", err)
		return nil, err
	}
	resp, timeElapsed, err := p.sendRequestWithRetry(req)
	if err != nil {
		respCode := 0
		if resp != nil {
			respCode = resp.StatusCode
		}
		pluginLog.Errorf("failed to exchange access token (HTTP status %d, total time elapsed %s): %v",
			respCode, timeElapsed.String(), err)
		return respData, fmt.Errorf("failed to exchange access token (HTTP status %d): %v", respCode, err)
	}
	defer resp.Body.Close()

	if pluginLog.DebugEnabled() {
		respDump, _ := httputil.DumpResponse(resp, false)
		pluginLog.Debugf("Received access token response after %s: \n%s",
			timeElapsed.String(), string(respDump))
	} else {
		pluginLog.Infof("Received access token response after %s", timeElapsed.String())
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		pluginLog.Errorf("Failed to read access token response body: %+v", err)
		return respData, fmt.Errorf("failed to read access token response body: %+v", err)
	}
	if err := json.Unmarshal(body, respData); err != nil {
		pluginLog.Errorf("Failed to unmarshal access token response data: %v", err)
		return respData, fmt.Errorf("failed to unmarshal access token response data: %v", err)
	}
	if respData.AccessToken == "" {
		pluginLog.Errora("access token response does not have access token", string(body))
		return respData, errors.New("access token response does not have access token. " + string(body))
	}
	pluginLog.Debug("successfully exchanged an access token")
	// Store access token
	// Default token life time is 3600 seconds.
	tokenExp := time.Now().Add(3600 * time.Second)
	exp, err := time.Parse(time.RFC3339Nano, respData.ExpireTime)
	if err != nil {
		pluginLog.Errorf("Failed to unmarshal timestamp %s from access token response, "+
			"fall back to use default lifetime (3600 seconds): %v", respData.ExpireTime, err)
	} else {
		tokenExp = exp
	}
	// Update cache and reset cache hit counter.
	p.tokens.Store(accessToken, stsservice.TokenInfo{
		TokenType:  accessToken,
		IssueTime:  time.Now(),
		ExpireTime: tokenExp,
		Token:      respData.AccessToken})
	p.mutex.Lock()
	p.accessTokenCacheHit = 0
	p.mutex.Unlock()
	return respData, nil
}

// generateSTSResp takes accessTokenResponse and generates StsResponseParameters in JSON.
func (p *Plugin) generateSTSResp(atResp *accessTokenResponse) ([]byte, error) {
	exp, err := time.Parse(time.RFC3339Nano, atResp.ExpireTime)
	// Default token life time is 3600 seconds
	var expireInSec int64 = 3600
	if err != nil {
		pluginLog.Errorf("Failed to unmarshal timestamp %s from access token response, "+
			"fall back to use default lifetime (3600 seconds): %v", atResp.ExpireTime, err)
	} else {
		expireInSec = int64(time.Until(exp).Seconds())
	}
	return p.generateSTSRespInner(atResp.AccessToken, expireInSec)
}

func (p *Plugin) generateSTSRespInner(token string, expire int64) ([]byte, error) {
	stsRespParam := stsservice.StsResponseParameters{
		AccessToken:     token,
		IssuedTokenType: tokenType,
		TokenType:       "Bearer",
		ExpiresIn:       expire,
	}
	statusJSON, err := json.MarshalIndent(stsRespParam, "", " ")
	if pluginLog.DebugEnabled() {
		stsRespParam.AccessToken = "redacted"
		pluginLog.Infof("Populated STS response parameters: %+v", stsRespParam)
	}
	return statusJSON, err
}

// DumpTokenStatus dumps all token status in JSON
func (p *Plugin) DumpPluginStatus() ([]byte, error) {
	tokenStatus := make([]stsservice.TokenInfo, 0)
	p.tokens.Range(func(k interface{}, v interface{}) bool {
		token := v.(stsservice.TokenInfo)
		tokenStatus = append(tokenStatus, stsservice.TokenInfo{
			TokenType: token.TokenType, IssueTime: token.IssueTime, ExpireTime: token.ExpireTime})
		return true
	})
	td := stsservice.TokensDump{
		Tokens: tokenStatus,
	}
	statusJSON, err := json.MarshalIndent(td, "", " ")
	return statusJSON, err
}

// SetEndpoints changes the endpoints for testing purposes only.
func (p *Plugin) SetEndpoints(fTokenEndpoint, aTokenEndpoint string) {
	federatedTokenEndpoint = fTokenEndpoint
	accessTokenEndpoint = aTokenEndpoint
}

// ClearCache is only used for testing purposes.
func (p *Plugin) ClearCache() {
	p.tokens.Delete(federatedToken)
	p.tokens.Delete(accessToken)
}
