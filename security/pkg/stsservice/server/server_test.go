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

package server

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"istio.io/istio/security/pkg/stsservice"
	"istio.io/istio/security/pkg/stsservice/mock"
)

type stsReqType int
const (
	validStsReq 						  stsReqType = 0
	emptyGrantType					  stsReqType = 1
	incorrectGrantType			  stsReqType = 2
	emptySubjectToken			  	stsReqType = 3
	emptySubjectTokenType 		stsReqType = 4
	incorrectSubjectTokenType stsReqType = 5
	incorrectRequestMethod    stsReqType = 6
	incorrectContentType      stsReqType = 7
)

type stsRespType int
const (
	successStsResp					stsRespType = 0
	invalidGrantType        stsRespType = 1
	invalidSubjectToken     stsRespType = 2
	invalidRequestMethod    stsRespType = 3
	invalidContentType      stsRespType = 4
)

// TestStsService verifies that STS server handles STS request properly.
func TestStsService(t *testing.T) {
	tokenManager := mock.CreateFakeTokenManager()
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	config := Config{LocalHostAddr:addr.IP.String(), LocalPort:addr.Port}
	stsServer := NewServer(config, tokenManager)
	testCases := map[string]struct {
		genTokenError  				error
		dumpTokenError 				error
		stsRequest     				*http.Request
		expectedStsResponse   *http.Response
	}{
		"Send a valid STS request and get STS success response": {
			stsRequest: genStsRequest(validStsReq, addr.String() + tokenPath),
			expectedStsResponse: genStsResponse(successStsResp),
		},
		"Send an invalid STS request and get STS error response": {},
		"Send a valid STS request and get STS error response": {},
		"Send a dump request and get dump information in response": {},
		"Send a dump request and get error response": {},
	}
	for k, tc := range testCases {

	}

}

func genStsRequest(reqType stsReqType, serverAddr string) (req *http.Request) {
	stsReqParam := stsservice.StsRequestParameters{
		GrantType: tokenExchangeGrantType,
		Resource: "https//:backend.example.com",
		Audience: "audience",
		Scope:    "scope",
		RequestedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		SubjectToken: "subject token",
		SubjectTokenType: subjectTokenType,
		ActorToken: "",
		ActorTokenType: "",
	}
	if reqType == emptyGrantType {
		stsReqParam.GrantType = ""
	} else if reqType == incorrectGrantType {
		stsReqParam.GrantType = "incorrect"
	} else if reqType == emptySubjectToken {
		stsReqParam.SubjectToken = ""
	} else if reqType == emptySubjectTokenType {
		stsReqParam.SubjectTokenType = ""
	} else if reqType == incorrectSubjectTokenType {
		stsReqParam.SubjectTokenType = "incorrect"
	}
	stsQueryJSON, _ := json.Marshal(stsReqParam)

	if reqType == validStsReq {
		req, _ := http.NewRequest("POST", serverAddr, bytes.NewBuffer(stsQueryJSON))
		req.Header.Set("Content-Type", urlEncodedForm)
		return req
	} else if reqType == incorrectRequestMethod {
		req, _ := http.NewRequest("GET", serverAddr, bytes.NewBuffer(stsQueryJSON))
		req.Header.Set("Content-Type", urlEncodedForm)
		return req
	} else if reqType == incorrectContentType {
		req, _ := http.NewRequest("POST", serverAddr, bytes.NewBuffer(stsQueryJSON))
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	return nil
}

func genStsResponse(respType stsRespType) (resp *http.Response) {
	if respType == successStsResp {

	}
	return resp
}