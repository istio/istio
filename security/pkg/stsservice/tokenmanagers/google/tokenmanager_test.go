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
	"istio.io/istio/security/pkg/stsservice/mock"
	"istio.io/istio/security/pkg/stsservice"
	"testing"
)

func TestAccessToken(t *testing.T) {
	tm, _ := CreateTokenManager(mock.FakeTrustDomain, mock.FakeProjectNum)

	ms, err := mock.StartNewServer(t)
	if err != nil {
		t.Fatalf("failed to start a mock server: %v", err)
	}
	originalFederatedTokenEndpoint := federatedTokenEndpoint
	federatedTokenEndpoint = ms.URL + "/v1/identitybindingtoken"
	originalAccessTokenEndpoint := accessTokenEndpoint
	accessTokenEndpoint = ms.URL + "/v1/projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken"
	defer func() {
		if err := ms.Stop(); err != nil {
			t.Logf("failed to stop mock server: %v", err)
		}
		federatedTokenEndpoint = originalFederatedTokenEndpoint
		accessTokenEndpoint = originalAccessTokenEndpoint
	}()

	stsReq := stsservice.StsRequestParameters{
		GrantType: "urn:ietf:params:oauth:grant-type:token-exchange",
		Audience: mock.FakeTrustDomain,
		Scope: scope,
		SubjectToken: mock.FakeSubjectToken,
		SubjectTokenType: "urn:ietf:params:oauth:token-type:jwt",

	}
	stsRespJSON, err := tm.GenerateToken(stsReq)
	if err != nil {
		t.Fatalf("failed to call exchange token: %v", err)
	}
	stsResp := stsservice.StsResponseParameters{}
	if err := json.Unmarshal(stsRespJSON, stsResp); err != nil {
		t.Errorf("failed to unmarshal STS response")
	}
	if stsResp.AccessToken != mock.FakeAccessToken {
		t.Errorf("Access token got %q, expected %q", stsResp.AccessToken, mock.FakeFederatedToken)
	}
}