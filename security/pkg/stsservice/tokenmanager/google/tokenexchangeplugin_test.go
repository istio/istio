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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/stsservice"
	"istio.io/istio/security/pkg/stsservice/tokenmanager/google/mock"
)

// TestAccessToken verifies that token manager could successfully call server and get access token.
func TestTokenExchangePlugin(t *testing.T) {
	tmPlugin, ms, originalFederatedTokenEndpoint, originalAccessTokenEndpoint := setUpTest(t, testSetUp{})
	lastStatusDumpMap := make(map[string]stsservice.TokenInfo)
	defer func() {
		if err := ms.Stop(); err != nil {
			t.Logf("failed to stop mock server: %v", err)
		}
		federatedTokenEndpoint = originalFederatedTokenEndpoint
		accessTokenEndpoint = originalAccessTokenEndpoint
	}()

	testCases := map[string]struct {
		genFederatedTokenError   error
		genAccessTokenError      error
		expectedError            string
		expectedStatusDumpUpdate []string
	}{
		"token manager returns valid STS success response": {
			expectedStatusDumpUpdate: []string{federatedToken, accessToken},
		},
		"token manager failed to return federated token": {
			genFederatedTokenError:   errors.New("fake error in generating federated access token"),
			expectedError:            "failed to exchange federated token",
			expectedStatusDumpUpdate: []string{},
		},
		"token manager failed to return access token": {
			genAccessTokenError:      errors.New("fake error in generating access token"),
			expectedError:            "failed to exchange access token",
			expectedStatusDumpUpdate: []string{federatedToken},
		},
	}

	for k, tc := range testCases {
		if tc.genAccessTokenError != nil {
			ms.SetGenAcsTokenError(tc.genAccessTokenError)
		}
		if tc.genFederatedTokenError != nil {
			ms.SetGenFedTokenError(tc.genFederatedTokenError)
		}
		stsRespJSON, err := tmPlugin.ExchangeToken(defaultSTSRequest())
		verifyToken(t, k, stsRespJSON, err, tc.expectedError)
		stsDumpJSON, _ := tmPlugin.DumpPluginStatus()
		lastStatusDumpMap = verifyDumpStatus(t, k, stsDumpJSON, lastStatusDumpMap, tc.expectedStatusDumpUpdate)
		ms.SetGenAcsTokenError(nil)
		ms.SetGenFedTokenError(nil)
	}
}

func verifyDumpStatus(t *testing.T, tCase string, dumpJSON []byte, lastStatus map[string]stsservice.TokenInfo,
	expected []string,
) map[string]stsservice.TokenInfo {
	newStatus := &stsservice.TokensDump{}
	if err := json.Unmarshal(dumpJSON, newStatus); err != nil {
		t.Errorf("(Test case %s), failed to unmarshal status dump: %v", tCase, err)
	}
	newStatusMap := extractTokenDumpToMap(newStatus)
	t.Logf("Dump newStatusMap:\n%+v", newStatusMap)
	t.Logf("Dump lastStatus:\n%+v", lastStatus)
	for _, exp := range expected {
		if newVal, ok := newStatusMap[exp]; !ok {
			t.Errorf("(Test case %s), failed to find expected token %s in status dump", tCase, exp)
		} else if oldVal, ok := lastStatus[exp]; ok {
			if newVal.ExpireTime == oldVal.ExpireTime || newVal.IssueTime == oldVal.IssueTime {
				t.Errorf("(Test case %s), expected status update for %s (%v) in status dump", tCase, exp, newVal)
			}
		}
	}
	return newStatusMap
}

func extractTokenDumpToMap(newStatus *stsservice.TokensDump) map[string]stsservice.TokenInfo {
	newStatusMap := make(map[string]stsservice.TokenInfo)
	for _, info := range newStatus.Tokens {
		newStatusMap[info.TokenType] = info
	}
	return newStatusMap
}

// verifyToken verifies the received STS response parameters and error match expectation.
func verifyToken(t *testing.T, tCase string, stsRespJSON []byte, actualErr error, expErr string) {
	if len(expErr) != 0 && actualErr != nil {
		if !strings.Contains(actualErr.Error(), expErr) {
			t.Errorf("(Test case %s), error does not match, want: %v vs get: %v",
				tCase, expErr, actualErr)
		}
		return
	} else if len(expErr) == 0 && actualErr == nil {
		stsResp := &stsservice.StsResponseParameters{}
		if err := json.Unmarshal(stsRespJSON, stsResp); err != nil {
			t.Errorf("(Test case %s), failed to unmarshal STS response: %v", tCase, err)
		}
		if stsResp.AccessToken != mock.FakeAccessToken {
			t.Errorf("(Test case %s), access token got: %q, expected: %q",
				tCase, stsResp.AccessToken, mock.FakeAccessToken)
		}
	} else {
		t.Errorf("(Test case %s), error does not match: want %s vs get: %v",
			tCase, expErr, actualErr)
	}
}

func defaultSTSRequest() security.StsRequestParameters {
	return security.StsRequestParameters{
		GrantType:        "urn:ietf:params:oauth:grant-type:token-exchange",
		Audience:         mock.FakeTrustDomain,
		Scope:            scope,
		SubjectToken:     mock.FakeSubjectToken,
		SubjectTokenType: "urn:ietf:params:oauth:token-type:jwt",
	}
}

type testSetUp struct {
	enableCache        bool
	enableDynamicToken bool
}

// setUpTest sets up token manager, authorization server.
func setUpTest(t *testing.T, setup testSetUp) (*Plugin, *mock.AuthorizationServer, string, string) {
	tm, _ := CreateTokenManagerPlugin(nil, mock.FakeTrustDomain, mock.FakeProjectNum, mock.FakeGKEClusterURL, setup.enableCache)
	ms, err := mock.StartNewServer(t, mock.Config{Port: 0})
	ms.EnableDynamicAccessToken(setup.enableDynamicToken)
	if err != nil {
		t.Fatalf("failed to start a mock server: %v", err)
	}
	originalFederatedTokenEndpoint := federatedTokenEndpoint
	federatedTokenEndpoint = ms.URL + "/v1/token"
	originalAccessTokenEndpoint := accessTokenEndpoint
	accessTokenEndpoint = ms.URL + "/v1/projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken"
	return tm, ms, originalFederatedTokenEndpoint, originalAccessTokenEndpoint
}

// TestAccessToken verifies that token manager could return a cached token to client.
func TestTokenExchangePluginWithCache(t *testing.T) {
	tmPlugin, ms, originalFederatedTokenEndpoint, originalAccessTokenEndpoint := setUpTest(t, testSetUp{enableCache: true, enableDynamicToken: true})
	defer func() {
		if err := ms.Stop(); err != nil {
			t.Logf("failed to stop mock server: %v", err)
		}
		federatedTokenEndpoint = originalFederatedTokenEndpoint
		accessTokenEndpoint = originalAccessTokenEndpoint
	}()

	// Make the first token exchange call to plugin. Plugin should call backend.
	stsRespJSON, _ := tmPlugin.ExchangeToken(defaultSTSRequest())
	stsResp := &stsservice.StsResponseParameters{}
	if err := json.Unmarshal(stsRespJSON, stsResp); err != nil {
		t.Errorf("failed to unmarshal STS response: %v", err)
	}
	firstToken := stsResp.AccessToken
	numFTCalls := ms.NumGetFederatedTokenCalls()
	numATCalls := ms.NumGetAccessTokenCalls()
	if numFTCalls != 1 {
		t.Errorf("number of get federated token API calls does not match, expected 1 but got %d", numFTCalls)
	}
	if numATCalls != 1 {
		t.Errorf("number of get access token API calls does not match, expected 1 but got %d", numATCalls)
	}
	// Make the second token exchange call to plugin. Plugin should return cached token.
	stsRespJSON, _ = tmPlugin.ExchangeToken(defaultSTSRequest())
	stsResp = &stsservice.StsResponseParameters{}
	if err := json.Unmarshal(stsRespJSON, stsResp); err != nil {
		t.Errorf("failed to unmarshal STS response: %v", err)
	}
	secondToken := stsResp.AccessToken
	numFTCalls = ms.NumGetFederatedTokenCalls()
	numATCalls = ms.NumGetAccessTokenCalls()
	if numFTCalls != 1 {
		t.Errorf("number of get federated token API calls does not match, expected 1 got %d", numFTCalls)
	}
	if numATCalls != 1 {
		t.Errorf("number of get access token API calls does not match, expected 1 got %d", numATCalls)
	}
	if firstToken != secondToken {
		t.Errorf("cached token is not used")
	}

	// Delete cached token
	tmPlugin.ClearCache()
	// Set token life time to 4 min, which is shorter than token grace period.
	ms.SetTokenLifeTime(4 * 60)
	// Make the third token exchange call to plugin. Cache is deleted, plugin should call backend.
	stsRespJSON, _ = tmPlugin.ExchangeToken(defaultSTSRequest())
	stsResp = &stsservice.StsResponseParameters{}
	if err := json.Unmarshal(stsRespJSON, stsResp); err != nil {
		t.Errorf("failed to unmarshal STS response: %v", err)
	}
	thirdToken := stsResp.AccessToken
	numFTCalls = ms.NumGetFederatedTokenCalls()
	numATCalls = ms.NumGetAccessTokenCalls()
	if numFTCalls != 2 {
		t.Errorf("number of get federated token API calls does not match, expected 2 got %d", numFTCalls)
	}
	if numATCalls != 2 {
		t.Errorf("number of get access token API calls does not match, expected 2 got %d", numATCalls)
	}
	if secondToken == thirdToken {
		t.Errorf("should not return cached token")
	}

	// Make the fourth token exchange call to plugin. Cached token is going to expire, plugin should call backend.
	stsRespJSON, _ = tmPlugin.ExchangeToken(defaultSTSRequest())
	stsResp = &stsservice.StsResponseParameters{}
	if err := json.Unmarshal(stsRespJSON, stsResp); err != nil {
		t.Errorf("failed to unmarshal STS response: %v", err)
	}
	fourthToken := stsResp.AccessToken
	numFTCalls = ms.NumGetFederatedTokenCalls()
	numATCalls = ms.NumGetAccessTokenCalls()
	if numFTCalls != 3 {
		t.Errorf("number of get federated token API calls does not match, expected 3 got %d", numFTCalls)
	}
	if numATCalls != 3 {
		t.Errorf("number of get access token API calls does not match, expected 3 got %d", numATCalls)
	}
	if thirdToken == fourthToken {
		t.Errorf("should not return cached token")
	}
}

// TestAccessTokenRequestToJson verifies the result of AccessTokenRequest-to-Json conversion.
func TestAccessTokenRequestToJson(t *testing.T) {
	tests := []struct {
		name      string
		delegates []string
		scope     []string
		lifetime  Duration
		want      string
	}{
		{
			name:      "OneHourInSecondLifetime",
			delegates: []string{},
			scope:     []string{"https://www.googleapis.com/auth/cloud-platform"},
			lifetime:  Duration{Seconds: 3600},
			want:      `{"name":"OneHourInSecondLifetime","delegates":[],"scope":["https://www.googleapis.com/auth/cloud-platform"],"lifetime":{"seconds":3600}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := accessTokenRequest{
				Name:      tt.name,
				Delegates: tt.delegates,
				Scope:     tt.scope,
				LifeTime:  tt.lifetime,
			}
			jsonQuery, err := json.Marshal(query)
			if err != nil {
				t.Errorf("%s: query: %v, err in json.Marshal: %v", tt.name, query, err)
			}
			got := string(jsonQuery)
			if !(got == tt.want) {
				t.Errorf("%s: got: %v, want: %v", tt.name, got, tt.want)
			}
		})
	}
}
