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
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/duration"

	"istio.io/pkg/log"
)

var (
	FakeFederatedToken = "FakeFederatedToken"
	FakeAccessToken    = "FakeAccessToken"
	FakeTrustDomain    = "FakeTrustDomain"
	FakeSubjectToken   = "FakeSubjectToken"
	FakeProjectNum     = "1234567"
)

type federatedTokenRequest struct {
	Audience           string `json:"audience"`
	GrantType          string `json:"grantType"`
	RequestedTokenType string `json:"requestedTokenType"`
	SubjectTokenType   string `json:"subjectTokenType"`
	SubjectToken       string `json:"subjectToken"`
	Scope              string `json:"Scope"`
}

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int32  `json:"expires_in"` // Expiration time in seconds
}

type accessTokenRequest struct {
	Name      string            `json:"Name"`
	Delegates []string          `json:"Delegates"` // nolint: structcheck, unused
	Scope     []string          `json:"Scope"`
	LifeTime  duration.Duration `json:"lifetime"` // nolint: structcheck, unused
}

type accessTokenResponse struct {
	AccessToken string            `json:"accessToken"`
	ExpireTime  duration.Duration `json:"expireTime"`
}

// AuthorizationServer is the in-memory secure token service.
type AuthorizationServer struct {
	Port   int
	URL    string
	server *http.Server
	t      *testing.T

	// These fields are needed to handle accessTokenRequest
	expectedFederatedTokenRequest federatedTokenRequest
	expectedAccessTokenRequest    accessTokenRequest

	mutex                       sync.RWMutex
	generateFederatedTokenError error
	generateAccessTokenError    error
}

// StartNewServer creates a mock server and starts it
func StartNewServer(t *testing.T) (*AuthorizationServer, error) {
	server := &AuthorizationServer{
		t: t,
		expectedFederatedTokenRequest: federatedTokenRequest{
			Audience:           FakeTrustDomain,
			GrantType:          "urn:ietf:params:oauth:grant-type:token-exchange",
			RequestedTokenType: "urn:ietf:params:oauth:token-type:access_token",
			SubjectTokenType:   "urn:ietf:params:oauth:token-type:jwt",
			SubjectToken:       FakeSubjectToken,
			Scope:              "https://www.googleapis.com/auth/cloud-platform",
		},
		expectedAccessTokenRequest: accessTokenRequest{
			Name:  fmt.Sprintf("projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken", FakeProjectNum),
			Scope: []string{"https://www.googleapis.com/auth/cloud-platform"},
		},
	}
	return server, server.Start()
}

func (ms *AuthorizationServer) SetGenFedTokenError(err error) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.generateFederatedTokenError = err
}

func (ms *AuthorizationServer) SetGenAcsTokenError(err error) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.generateAccessTokenError = err
}

// Start starts the mock server.
func (ms *AuthorizationServer) Start() error {
	atEndpoint := fmt.Sprintf("/v1/projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken", FakeProjectNum)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/identitybindingtoken", ms.getFederatedToken)
	mux.HandleFunc(atEndpoint, ms.getAccessToken)
	ms.t.Logf("Registered handler for endpoints:\n%s\n%s", atEndpoint, "/v1/identitybindingtoken")
	server := &http.Server{
		Addr:    ":",
		Handler: mux,
	}
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Errorf("Server failed to listen %v", err)
		return err
	}

	port := ln.Addr().(*net.TCPAddr).Port
	ms.Port = port
	ms.URL = fmt.Sprintf("http://localhost:%d", port)
	server.Addr = ":" + strconv.Itoa(port)

	go func() {
		if err := server.Serve(ln); err != nil {
			log.Errorf("Server failed to serve in %q: %v", ms.URL, err)
		}
	}()

	// sleep a while for mock server to start.
	time.Sleep(time.Second)

	return nil
}

// Stop stops he mock server.
func (ms *AuthorizationServer) Stop() error {
	if ms.server == nil {
		return nil
	}

	return ms.server.Close()
}

func (ms *AuthorizationServer) getFederatedToken(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	var request federatedTokenRequest
	err := decoder.Decode(&request)
	if err != nil {
		ms.t.Errorf("invalid federatedTokenRequest: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var fakeErr error
	var want federatedTokenRequest
	ms.mutex.Lock()
	want = ms.expectedFederatedTokenRequest
	fakeErr = ms.generateFederatedTokenError
	ms.mutex.Unlock()

	if req.Header.Get("Content-Type") != "application/json" {
		ms.t.Errorf("Content-Type header does not match\nwant %s\n got %s",
			"application/json", req.Header.Get("Content-Type"))
	}
	if !reflect.DeepEqual(want, request) {
		ms.t.Errorf("wrong federatedTokenRequest\nwant %+v\n got %+v", want, request)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if fakeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	resp := federatedTokenResponse{
		AccessToken:     FakeFederatedToken,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       3600,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (ms *AuthorizationServer) getAccessToken(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	var request accessTokenRequest
	err := decoder.Decode(&request)
	if err != nil {
		ms.t.Errorf("invalid accessTokenRequest: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var fakeErr error
	want := accessTokenRequest{}
	ms.mutex.Lock()
	want = ms.expectedAccessTokenRequest
	fakeErr = ms.generateAccessTokenError
	ms.mutex.Unlock()

	if req.Header.Get("Authorization") != "" {
		auth := req.Header.Get("Authorization")
		if strings.TrimPrefix(auth, "Bearer ") != FakeFederatedToken {
			ms.t.Errorf("Authorization header does not match\nwant %s\ngot %s",
				FakeFederatedToken, auth)
		}
	} else {
		ms.t.Error("missing Authorization header")
	}
	if req.Header.Get("Content-Type") != "application/json" {
		ms.t.Errorf("Content-Type header does not match\nwant %s\n got %s",
			"application/json", req.Header.Get("Content-Type"))
	}
	if !reflect.DeepEqual(want.Scope, request.Scope) {
		ms.t.Errorf("wrong federatedTokenRequest\nwant %+v\n got %+v", want, request)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if fakeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	resp := accessTokenResponse{
		AccessToken: FakeAccessToken,
		ExpireTime: duration.Duration{
			Seconds: 3600,
		},
	}

	_ = json.NewEncoder(w).Encode(resp)
}
