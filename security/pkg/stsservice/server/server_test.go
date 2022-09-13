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

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"istio.io/istio/security/pkg/stsservice"
	"istio.io/istio/security/pkg/stsservice/mock"
	"istio.io/pkg/log"
)

type stsReqType int

const (
	validStsReq               stsReqType = 0
	emptyGrantType            stsReqType = 1
	incorrectGrantType        stsReqType = 2
	emptySubjectToken         stsReqType = 3
	emptySubjectTokenType     stsReqType = 4
	incorrectSubjectTokenType stsReqType = 5
	incorrectRequestMethod    stsReqType = 6
	incorrectContentType      stsReqType = 7
	tokenStatusDump           stsReqType = 8
)

type stsRespType int

const (
	successStsResp         stsRespType = 0
	validationFailure      stsRespType = 1
	tokenGenerationFailure stsRespType = 2
	StatusDumpSuccess      stsRespType = 3
	StatusDumpFailure      stsRespType = 4
)

// TestStsService verifies that STS server handles STS request properly.
func TestStsService(t *testing.T) {
	tokenManager, hTTPClient, sTSAddr, sTSServer := setUpServerAndClient(t)
	emptyStsParam := stsservice.StsResponseParameters{}
	mockToken := &stsservice.TokenInfo{
		TokenType:  "type",
		IssueTime:  time.Now(),
		ExpireTime: time.Now().Add(1 * time.Hour),
	}
	testCases := map[string]struct {
		genTokenError        error
		dumpTokenError       error
		stsRequest           *http.Request
		stsRespParam         stsservice.StsResponseParameters
		expectedStsResponse  *http.Response
		expectedResponseType stsRespType
		expectedToken        *stsservice.TokenInfo
	}{
		"Send a valid STS request and get STS success response": {
			stsRequest:           genStsRequest(validStsReq, "http://"+sTSAddr.String()+TokenPath),
			stsRespParam:         genSuccessStsRespParam(),
			expectedStsResponse:  genStsResponse(successStsResp, genSuccessStsRespParam(), nil, nil),
			expectedResponseType: successStsResp,
		},
		"Send an invalid STS request (empty grant type) and get STS error response": {
			stsRequest:           genStsRequest(emptyGrantType, "http://"+sTSAddr.String()+TokenPath),
			expectedStsResponse:  genStsResponse(validationFailure, genSuccessStsRespParam(), errors.New("request query grant_type is invalid"), nil),
			expectedResponseType: validationFailure,
		},
		"Send an invalid STS request (incorrect grant type) and get STS error response": {
			stsRequest:           genStsRequest(incorrectGrantType, "http://"+sTSAddr.String()+TokenPath),
			expectedStsResponse:  genStsResponse(validationFailure, genSuccessStsRespParam(), errors.New("request query grant_type is invalid"), nil),
			expectedResponseType: validationFailure,
		},
		"Send an invalid STS request (empty subject token) and get STS error response": {
			stsRequest:           genStsRequest(emptySubjectToken, "http://"+sTSAddr.String()+TokenPath),
			expectedStsResponse:  genStsResponse(validationFailure, genSuccessStsRespParam(), errors.New("subject_token is empty"), nil),
			expectedResponseType: validationFailure,
		},
		"Send an invalid STS request (empty subject token type) and get STS error response": {
			stsRequest:           genStsRequest(emptySubjectTokenType, "http://"+sTSAddr.String()+TokenPath),
			expectedStsResponse:  genStsResponse(validationFailure, genSuccessStsRespParam(), errors.New("subject_token_type is invalid"), nil),
			expectedResponseType: validationFailure,
		},
		"Send an invalid STS request (incorrect subject token type) and get STS error response": {
			stsRequest:           genStsRequest(incorrectSubjectTokenType, "http://"+sTSAddr.String()+TokenPath),
			expectedStsResponse:  genStsResponse(validationFailure, genSuccessStsRespParam(), errors.New("subject_token_type is invalid"), nil),
			expectedResponseType: validationFailure,
		},
		"Send an invalid STS request (incorrect request method) and get STS error response": {
			stsRequest:           genStsRequest(incorrectRequestMethod, "http://"+sTSAddr.String()+TokenPath),
			expectedStsResponse:  genStsResponse(validationFailure, genSuccessStsRespParam(), errors.New("request method is invalid"), nil),
			expectedResponseType: validationFailure,
		},
		"Send an invalid STS request (incorrect content type) and get STS error response": {
			stsRequest:           genStsRequest(incorrectContentType, "http://"+sTSAddr.String()+TokenPath),
			expectedStsResponse:  genStsResponse(validationFailure, genSuccessStsRespParam(), errors.New("request content type is invalid"), nil),
			expectedResponseType: validationFailure,
		},
		"Send a valid STS request and get STS error response": {
			stsRequest:           genStsRequest(validStsReq, "http://"+sTSAddr.String()+TokenPath),
			genTokenError:        errors.New("failed to generate token"),
			expectedStsResponse:  genStsResponse(tokenGenerationFailure, emptyStsParam, errors.New("failed to generate token"), nil),
			expectedResponseType: tokenGenerationFailure,
		},
		"Send a dump request and get dump information in response": {
			stsRequest:           genStsRequest(tokenStatusDump, "http://"+sTSAddr.String()+StsStatusPath),
			expectedStsResponse:  genStsResponse(StatusDumpSuccess, emptyStsParam, nil, mockToken),
			expectedResponseType: StatusDumpSuccess,
			expectedToken:        mockToken,
		},
		"Send a dump request and get error response": {
			stsRequest:           genStsRequest(tokenStatusDump, "http://"+sTSAddr.String()+StsStatusPath),
			dumpTokenError:       errors.New("failed to dump token"),
			expectedStsResponse:  genStsResponse(StatusDumpFailure, emptyStsParam, errors.New("failed to dump token"), nil),
			expectedResponseType: StatusDumpFailure,
		},
	}
	for k, tc := range testCases {
		if tc.genTokenError != nil {
			tokenManager.SetGenerateTokenError(tc.genTokenError)
		}
		if tc.dumpTokenError != nil {
			tokenManager.SetDumpTokenError(tc.dumpTokenError)
		}
		if tc.expectedToken != nil {
			tokenManager.SetToken(*tc.expectedToken)
		}
		tokenManager.SetRespStsParam(tc.stsRespParam)
		resp, err := sendStsRequestWithRetry(hTTPClient, tc.stsRequest)
		if err != nil {
			t.Fatalf("(Test case %s), failure in sending STS request: %v", k, err)
		}
		verifyResponse(t, k, tc.expectedResponseType, resp, tc.expectedStsResponse)
		tokenManager.SetGenerateTokenError(nil)
		tokenManager.SetDumpTokenError(nil)
	}
	sTSServer.Stop()
}

func setUpServerAndClient(t *testing.T) (*mock.FakeTokenManager, *http.Client, *net.TCPAddr, *Server) {
	tokenManager := mock.CreateFakeTokenManager()
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:3333")
	if err != nil {
		t.Fatalf("failed to create address %v", err)
	}
	config := Config{LocalHostAddr: addr.IP.String(), LocalPort: addr.Port}
	ipPort := addr.String()
	server, _ := NewServer(config, tokenManager)
	hTTPClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				t.Logf("set up server address to dial %s", addr)
				addr = ipPort
				return net.Dial(network, addr)
			},
		},
	}
	return tokenManager, hTTPClient, addr, server
}

func sendStsRequestWithRetry(client *http.Client, req *http.Request) (resp *http.Response, err error) {
	for i := 0; i < 10; i++ {
		resp, err = client.Do(req)
		if err == nil {
			return resp, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return resp, err
}

func verifyResponse(t *testing.T, tCase string, respType stsRespType, resp, expectedResp *http.Response) {
	if resp.StatusCode != expectedResp.StatusCode {
		t.Errorf("(Test case %s): response HTTP status code does not match, get %d vs expected %d",
			tCase, resp.StatusCode, expectedResp.StatusCode)
	}
	if respType != StatusDumpFailure {
		if resp.Header.Get("Content-Type") != expectedResp.Header.Get("Content-Type") {
			t.Errorf("(Test case %s): response HTTP Header Content-Type does not match, get %s vs expected %s",
				tCase, resp.Header.Get("Content-Type"), expectedResp.Header.Get("Content-Type"))
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	defer expectedResp.Body.Close()
	expectedBody, _ := io.ReadAll(expectedResp.Body)
	if respType == successStsResp {
		verifyResponseBody(t, tCase, body, expectedBody)
	} else if respType == validationFailure || respType == tokenGenerationFailure {
		verifyErrorResponse(t, tCase, body, expectedBody)
	} else if respType == StatusDumpSuccess {
		verifyDumpResponse(t, tCase, body, expectedBody)
	} else if respType == StatusDumpFailure {
		bodyStr := string(body)
		eBodyStr := string(expectedBody)
		if bodyStr != eBodyStr {
			t.Errorf("Dump status failure response does not match, get %s but expect %s", bodyStr, eBodyStr)
		}
	}
}

func verifyResponseBody(t *testing.T, tCase string, body, expBody []byte) {
	respStsParam := &stsservice.StsResponseParameters{}
	expRespStsParam := &stsservice.StsResponseParameters{}
	if err := json.Unmarshal(body, respStsParam); err != nil {
		t.Errorf("failed to unmarshal STS success response: %v", err)
	}
	if err := json.Unmarshal(expBody, expRespStsParam); err != nil {
		t.Errorf("failed to unmarshal expected STS success response: %v", err)
	}
	if !reflect.DeepEqual(respStsParam, expRespStsParam) {
		t.Errorf("(Test case %s): STS response parameter does not match, get %v vs expected %v",
			tCase, respStsParam, expRespStsParam)
	}
}

func verifyErrorResponse(t *testing.T, tCase string, body, expBody []byte) {
	respErr := &stsservice.StsErrorResponse{}
	expRespErr := &stsservice.StsErrorResponse{}
	if err := json.Unmarshal(body, respErr); err != nil {
		t.Errorf("failed to unmarshal error response: %v", err)
	}
	if err := json.Unmarshal(expBody, expRespErr); err != nil {
		t.Errorf("failed to unmarshal expected error response: %v", err)
	}
	if respErr.Error != expRespErr.Error {
		t.Errorf("(Test case %s): STS response error code does not match, get %s vs expected %s",
			tCase, respErr.Error, expRespErr.Error)
	}
	if !strings.HasPrefix(respErr.ErrorDescription, expRespErr.ErrorDescription) {
		t.Errorf("(Test case %s): STS response error message does not match, get %s vs expected %s",
			tCase, respErr.ErrorDescription, expRespErr.ErrorDescription)
	}
}

func verifyDumpResponse(t *testing.T, tCase string, body, expBody []byte) {
	tokenDump := &stsservice.TokensDump{}
	expTokenDump := &stsservice.TokensDump{}
	if err := json.Unmarshal(body, tokenDump); err != nil {
		t.Errorf("failed to unmarshal token dump: %v", err)
	}
	if err := json.Unmarshal(expBody, expTokenDump); err != nil {
		t.Errorf("failed to unmarshal expected token dump: %v", err)
	}
	if !reflect.DeepEqual(tokenDump, expTokenDump) {
		t.Errorf("(Test case %s): token dump does not match, get %v vs expected %v",
			tCase, tokenDump, expTokenDump)
	}
}

func genStsRequest(reqType stsReqType, serverAddr string) (req *http.Request) {
	stsQuery := url.Values{}
	stsQuery.Set("grant_type", TokenExchangeGrantType)
	stsQuery.Set("resource", "https//:backend.example.com")
	stsQuery.Set("audience", "audience")
	stsQuery.Set("scope", "scope")
	stsQuery.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
	stsQuery.Set("subject_token", "subject token")
	stsQuery.Set("subject_token_type", SubjectTokenType)
	stsQuery.Set("actor_token", "")
	stsQuery.Set("actor_token_type", "")
	if reqType == emptyGrantType {
		stsQuery.Set("grant_type", "")
	} else if reqType == incorrectGrantType {
		stsQuery.Set("grant_type", "incorrect")
	} else if reqType == emptySubjectToken {
		stsQuery.Set("subject_token", "")
	} else if reqType == emptySubjectTokenType {
		stsQuery.Set("subject_token_type", "")
	} else if reqType == incorrectSubjectTokenType {
		stsQuery.Set("subject_token_type", "incorrect")
	}

	if reqType == incorrectRequestMethod {
		req, _ = http.NewRequest("GET", serverAddr, strings.NewReader(stsQuery.Encode()))
		req.Header.Set("Content-Type", URLEncodedForm)
	} else if reqType == incorrectContentType {
		req, _ = http.NewRequest("POST", serverAddr, strings.NewReader(stsQuery.Encode()))
		req.Header.Set("Content-Type", "application/json")
	} else if reqType == tokenStatusDump {
		req, _ = http.NewRequest("GET", serverAddr, nil)
	} else {
		req, _ = http.NewRequest("POST", serverAddr, strings.NewReader(stsQuery.Encode()))
		req.Header.Set("Content-Type", URLEncodedForm)
	}
	reqDump, _ := httputil.DumpRequest(req, true)
	log.Infof("STS request: %s", string(reqDump))
	return req
}

func genStsResponse(respType stsRespType, param stsservice.StsResponseParameters,
	serverErr error, tokenInfo *stsservice.TokenInfo,
) (resp *http.Response) {
	resp = &http.Response{
		Header: make(http.Header),
	}
	resp.Header.Add("Content-Type", "application/json")
	if respType == successStsResp {
		resp.StatusCode = http.StatusOK
		resp.Status = http.StatusText(http.StatusOK)
		stsJSON, _ := json.MarshalIndent(param, "", "  ")
		resp.Body = io.NopCloser(bytes.NewBuffer(stsJSON))
	} else if respType == tokenGenerationFailure {
		resp.StatusCode = http.StatusInternalServerError
		resp.Status = http.StatusText(http.StatusInternalServerError)
		errResp := stsservice.StsErrorResponse{
			Error:            invalidTarget,
			ErrorDescription: serverErr.Error(),
		}
		errRespJSON, _ := json.MarshalIndent(errResp, "", "  ")
		resp.Body = io.NopCloser(bytes.NewBuffer(errRespJSON))
	} else if respType == validationFailure {
		resp.StatusCode = http.StatusBadRequest
		resp.Status = http.StatusText(http.StatusBadRequest)
		errResp := stsservice.StsErrorResponse{
			Error:            invalidRequest,
			ErrorDescription: serverErr.Error(),
		}
		errRespJSON, _ := json.MarshalIndent(errResp, "", "  ")
		resp.Body = io.NopCloser(bytes.NewBuffer(errRespJSON))
	} else if respType == StatusDumpSuccess {
		resp.StatusCode = http.StatusOK
		resp.Status = http.StatusText(http.StatusOK)
		tokenStatus := make([]stsservice.TokenInfo, 0)
		tokenStatus = append(tokenStatus, *tokenInfo)
		td := stsservice.TokensDump{Tokens: tokenStatus}
		statusJSON, _ := json.MarshalIndent(td, "", " ")
		resp.Body = io.NopCloser(bytes.NewBuffer(statusJSON))
	} else if respType == StatusDumpFailure {
		resp.StatusCode = http.StatusInternalServerError
		resp.Status = http.StatusText(http.StatusInternalServerError)
		resp.Header.Set("Content-Type", "text/plain")
		resp.Body = io.NopCloser(bytes.NewBufferString("failure in dumping STS server status: " + serverErr.Error()))
	}
	respDump, _ := httputil.DumpResponse(resp, true)
	log.Infof("Dump response: %s", string(respDump))
	return resp
}

func genSuccessStsRespParam() (p stsservice.StsResponseParameters) {
	return stsservice.StsResponseParameters{
		AccessToken:     "accesstoken",
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       60,
		Scope:           "example.com",
	}
}
