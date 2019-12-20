// Copyright 2018 Istio Authors
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
	"github.com/golang/protobuf/ptypes/duration"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

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
	GrantType          string `json:"grant_type"`
	RequestedTokenType string `json:"requested_token_type"`
	SubjectTokenType   string `json:"subject_token_type"`
	SubjectToken       string `json:"subject_token"`
	Scope              string `json:"scope"`
}

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int32  `json:"expires_in"` // Expiration time in seconds
}

type accessTokenRequest struct {
	name      string `json:"name"`
	delegates []string `json:"delegates"`
	scope     []string `json:"scope"`
	lifeTime  duration.Duration `json:"life_time"`
}

type accessTokenResponse struct {
	AccessToken     string `json:"access_token"`
	ExpireTime      duration.Duration `json:"expire_time"`
}

// MockServer is the in-memory secure token service.
type MockServer struct {
	Port   int
	URL    string
	server *http.Server
	t      *testing.T

	// These fields are needed to handle accessTokenRequest
	expectedFederatedTokenRequest federatedTokenRequest
	expectedAccessTokenRequest accessTokenRequest

	mutex              sync.RWMutex
	generateFederatedTokenError error
	generateAccessTokenError error
}

// StartNewServer creates a mock server and starts it
func StartNewServer(t *testing.T) (*MockServer, error) {
	server := &MockServer{
		t:            t,
		expectedFederatedTokenRequest: federatedTokenRequest{
			Audience:           FakeTrustDomain,
			GrantType:          "urn:ietf:params:oauth:grant-type:token-exchange",
			RequestedTokenType: "urn:ietf:params:oauth:token-type:access_token",
			SubjectTokenType:   "urn:ietf:params:oauth:token-type:jwt",
			SubjectToken:       FakeSubjectToken,
			Scope:              "https://www.googleapis.com/auth/cloud-platform",
		},
		expectedAccessTokenRequest: accessTokenRequest{
			name:           fmt.Sprintf("projects/-/serviceAccounts/service-%d@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken", FakeProjectNum),
			scope:          []string{"https://www.googleapis.com/auth/cloud-platform"},
		},
	}
	return server, server.Start()
}

// Start starts the mock server.
func (ms *MockServer) Start() error {
	router := mux.NewRouter()
	router.HandleFunc("/v1/identitybindingtoken", ms.getFederatedToken).Methods("POST")
	atEndpoint := fmt.Sprintf("/v1/projects/-/serviceAccounts/service-%d@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken", FakeProjectNum)
	router.HandleFunc(atEndpoint, ms.getAccessToken).Methods("POST")
	server := &http.Server{
		Addr:    ":",
		Handler: router,
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
func (ms *MockServer) Stop() error {
	if ms.server == nil {
		return nil
	}

	return ms.server.Close()
}

func (ms *MockServer) getFederatedToken(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	var request federatedTokenRequest
	err := decoder.Decode(&request)
	if err != nil {
		ms.t.Errorf("invalid federatedTokenRequest: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	want := federatedTokenRequest{}
	ms.mutex.Lock()
	want = ms.expectedFederatedTokenRequest
	ms.mutex.Unlock()

	if !reflect.DeepEqual(want, request) {
		ms.t.Errorf("wrong federatedTokenRequest\nwant %+v\n got %+v", want, request)
		w.WriteHeader(http.StatusBadRequest)
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

func (ms *MockServer) getAccessToken(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	var request accessTokenRequest
	err := decoder.Decode(&request)
	if err != nil {
		ms.t.Errorf("invalid accessTokenRequest: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	want := accessTokenRequest{}
	ms.mutex.Lock()
	want = ms.expectedAccessTokenRequest
	ms.mutex.Unlock()

	if !reflect.DeepEqual(want, request) {
		ms.t.Errorf("wrong federatedTokenRequest\nwant %+v\n got %+v", want, request)
		w.WriteHeader(http.StatusBadRequest)
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