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

package mock

import (
	"encoding/json"
	"sync"
	"time"

	"istio.io/istio/security/pkg/stsservice"
)

type FakeTokenManager struct {
	mutex              sync.RWMutex
	generateTokenError error
	dumpTokenError     error
	tokens             sync.Map
	stsRespParam       stsservice.StsResponseParameters
}

func CreateFakeTokenManager() *FakeTokenManager {
	tm := &FakeTokenManager{
		generateTokenError: nil,
		dumpTokenError:     nil,
		tokens:             sync.Map{},
		stsRespParam:       stsservice.StsResponseParameters{},
	}
	return tm
}

func (tm *FakeTokenManager) SetGenerateTokenError(err error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.generateTokenError = err
}

func (tm *FakeTokenManager) SetDumpTokenError(err error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.dumpTokenError = err
}

func (tm *FakeTokenManager) SetRespStsParam(p stsservice.StsResponseParameters) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.stsRespParam = p
}

func (tm *FakeTokenManager) SetToken(t stsservice.TokenInfo) {
	//erase map
	tm.tokens.Range(func(key interface{}, value interface{}) bool {
		tm.tokens.Delete(key)
		return true
	})
	tm.tokens.Store(t.IssueTime, t)
}

// GenerateToken returns a fake token, or error if generateTokenError is set.
func (tm *FakeTokenManager) GenerateToken(_ stsservice.StsRequestParameters) ([]byte, error) {
	var expErr error
	tm.mutex.Lock()
	expErr = tm.generateTokenError
	tm.mutex.Unlock()
	if expErr != nil {
		return nil, expErr
	}
	var stsResp stsservice.StsResponseParameters
	tm.mutex.Lock()
	stsResp = tm.stsRespParam
	tm.mutex.Unlock()
	t := time.Now()
	tm.tokens.Store(t.String(), stsservice.TokenInfo{
		TokenType:  stsResp.TokenType,
		IssueTime:  t,
		ExpireTime: t.Add(time.Duration(stsResp.ExpiresIn) * time.Second),
	})
	statusJSON, _ := json.MarshalIndent(stsResp, "", " ")
	return statusJSON, nil
}

// DumpTokenStatus returns fake token status, or error if dumpTokenError is set.
func (tm *FakeTokenManager) DumpTokenStatus() ([]byte, error) {
	var expErr error
	tm.mutex.Lock()
	expErr = tm.dumpTokenError
	tm.mutex.Unlock()
	if expErr != nil {
		return nil, expErr
	}

	tokenStatus := make([]stsservice.TokenInfo, 0)
	tm.tokens.Range(func(k interface{}, v interface{}) bool {
		token := v.(stsservice.TokenInfo)
		tokenStatus = append(tokenStatus, token)
		return true
	})
	td := stsservice.TokensDump{
		Tokens: tokenStatus,
	}
	statusJSON, err := json.MarshalIndent(td, "", " ")
	return statusJSON, err
}
