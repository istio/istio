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

package mock

import (
	"sync"
	"time"
	"encoding/json"
	"istio.io/istio/security/pkg/stsservice"
)

type fakeTokenManager struct {
	mutex sync.RWMutex
	generateTokenError error
	dumpTokenError     error
	tokens        		 sync.Map
}

type tokenInfo struct {
	tokenType  string `json:"token_type"`
	issueTime  string `json:"issue_time"`
	expireTime string `json:"expire_time"`
}

type tokensDump struct {
	tokens []tokenInfo `json:"tokens"`
}

func CreateFakeTokenManager() (*fakeTokenManager) {
	tm := &fakeTokenManager{
		generateTokenError: nil,
		dumpTokenError:     nil,
		tokens:             sync.Map{},
	}
	return tm
}

func (tm *fakeTokenManager) SetGenerateTokenError (err error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.generateTokenError = err
}

func (tm *fakeTokenManager) SetDumpTokenError (err error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.dumpTokenError = err
}

// GenerateToken returns a fake token, or error if generateTokenError is set.
func (tm *fakeTokenManager) GenerateToken(_ stsservice.StsRequestParameters) ([]byte, error) {
	var expErr error
	tm.mutex.Lock()
		expErr = tm.generateTokenError
	tm.mutex.Unlock()
	if expErr != nil {
		return nil, expErr
	}
	stsResp := stsservice.StsResponseParameters{
		AccessToken:     "fakeaccesstoken",
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       60,
		Scope:           "example.com",
	}
	t := time.Now()
	tm.tokens.Store(t.String(), tokenInfo{
		tokenType:  stsResp.TokenType,
		issueTime:  t.String(),
		expireTime: t.Add(time.Duration(stsResp.ExpiresIn) * time.Second).String(),
	})
	statusJSON, _ := json.MarshalIndent(stsResp, "", " ")
	return statusJSON, nil
}

// DumpTokenStatus returns fake token status, or error if dumpTokenError is set.
func (tm *fakeTokenManager) DumpTokenStatus() ([]byte, error) {
	var expErr error
	tm.mutex.Lock()
	expErr = tm.dumpTokenError
	tm.mutex.Unlock()
	if expErr != nil {
		return nil, expErr
	}

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