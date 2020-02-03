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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"istio.io/istio/security/pkg/stsservice"
	"istio.io/istio/security/pkg/stsservice/tokenmanager/google/mock"
)

// TestAccessToken verifies that token manager could successfully call server and get access token.
func TestTokenExchangePlugin(t *testing.T) {
	tmPlugin, ms, originalFederatedTokenEndpoint, originalAccessTokenEndpoint := setUpTest(t)
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
	expected []string) map[string]stsservice.TokenInfo {
	newStatus := &stsservice.TokensDump{}
	if err := json.Unmarshal(dumpJSON, newStatus); err != nil {
		t.Errorf("(Test case %s), failed to unmarshal status dump: %v", tCase, err)
	}
	newStatusMap := extractTokenDumpToMap(newStatus)
	t.Logf("Dump newStatusMap:\n%v", newStatusMap)
	t.Logf("Dump lastStatus:\n%v", lastStatus)
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

func defaultSTSRequest() stsservice.StsRequestParameters {
	return stsservice.StsRequestParameters{
		GrantType:        "urn:ietf:params:oauth:grant-type:token-exchange",
		Audience:         mock.FakeTrustDomain,
		Scope:            scope,
		SubjectToken:     mock.FakeSubjectToken,
		SubjectTokenType: "urn:ietf:params:oauth:token-type:jwt",
	}
}

// setUpTest sets up token manager, authorization server.
func setUpTest(t *testing.T) (*Plugin, *mock.AuthorizationServer, string, string) {
	tm, _ := CreateTokenManagerPlugin(mock.FakeTrustDomain, mock.FakeProjectNum, mock.FakeGKEClusterURL)
	ms, err := mock.StartNewServer(t, mock.Config{Port: 0})
	if err != nil {
		t.Fatalf("failed to start a mock server: %v", err)
	}
	originalFederatedTokenEndpoint := federatedTokenEndpoint
	federatedTokenEndpoint = ms.URL + "/v1/identitybindingtoken"
	originalAccessTokenEndpoint := accessTokenEndpoint
	accessTokenEndpoint = ms.URL + "/v1/projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken"
	return tm, ms, originalFederatedTokenEndpoint, originalAccessTokenEndpoint
}
