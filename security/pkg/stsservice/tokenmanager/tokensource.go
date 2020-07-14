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

package tokenmanager

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"istio.io/istio/security/pkg/stsservice"
	"istio.io/istio/security/pkg/stsservice/server"
)

// TokenSource specifies an oauth token source based on STS token exchange.
// https://godoc.org/golang.org/x/oauth2#TokenSource
type TokenSource struct {
	tm                stsservice.TokenManager
	subjectToken      string
	subjectTokenMutex sync.Mutex
	authScope         string
}

var _ oauth2.TokenSource = &TokenSource{}

// NewTokenSource creates a token source based on STS token exchange.
func NewTokenSource(trustDomain, subjectToken, authScope string) *TokenSource {
	return &TokenSource{
		tm:           CreateTokenManager(GoogleTokenExchange, Config{TrustDomain: trustDomain}),
		subjectToken: subjectToken,
		authScope:    authScope,
	}
}

// RefreshSubjectToken sets subject token with new expiry.
func (ts *TokenSource) RefreshSubjectToken(subjectToken string) {
	ts.subjectTokenMutex.Lock()
	defer ts.subjectTokenMutex.Unlock()
	ts.subjectToken = subjectToken
}

// Token returns Oauth token received from sts token exchange.
func (ts *TokenSource) Token() (*oauth2.Token, error) {
	ts.subjectTokenMutex.Lock()
	params := stsservice.StsRequestParameters{
		GrantType:        server.TokenExchangeGrantType,
		Scope:            ts.authScope,
		SubjectToken:     ts.subjectToken,
		SubjectTokenType: server.SubjectTokenType,
	}
	ts.subjectTokenMutex.Unlock()
	body, err := ts.tm.GenerateToken(params)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange access token: %v", err)
	}
	respData := &stsservice.StsResponseParameters{}
	if err := json.Unmarshal(body, respData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal access token response data: %v", err)
	}

	return &oauth2.Token{
		AccessToken:  respData.AccessToken,
		TokenType:    respData.TokenType,
		RefreshToken: respData.RefreshToken,
		Expiry:       time.Now().Add(time.Second * time.Duration(respData.ExpiresIn)),
	}, nil
}
