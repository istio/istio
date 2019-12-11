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

package stsclient

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"istio.io/pkg/log"
)

var (
	fakeAccessToken  = "FakeAccessToken"
	fakeTrustDomain  = "FakeTrustDomain"
	fakeSubjectToken = "FakeSubjectToken"
)

// MockServer is the in-memory secure token service.
type MockServer struct {
	Port   int
	URL    string
	server *http.Server
	t      *testing.T
}

// StartNewServer creates a mock server and starts it
func StartNewServer(t *testing.T) (*MockServer, error) {
	server := &MockServer{
		t: t,
	}
	return server, server.Start()
}

// Start starts the mock server.
func (ms *MockServer) Start() error {
	router := mux.NewRouter()
	router.HandleFunc("/v1/identitybindingtoken", ms.getFederatedToken).Methods("POST")

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

type federatedTokenRequest struct {
	Audience           string `json:"audience"`
	GrantType          string `json:"grantType"`
	RequestedTokenType string `json:"requestedTokenType"`
	SubjectTokenType   string `json:"subjectTokenType"`
	SubjectToken       string `json:"subjectToken"`
	Scope              string `json:"scope"`
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
	want := federatedTokenRequest{
		Audience:           fakeTrustDomain,
		GrantType:          "urn:ietf:params:oauth:grant-type:token-exchange",
		RequestedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		SubjectTokenType:   "urn:ietf:params:oauth:token-type:jwt",
		SubjectToken:       fakeSubjectToken,
		Scope:              "https://www.googleapis.com/auth/cloud-platform",
	}
	if !reflect.DeepEqual(want, request) {
		ms.t.Errorf("wrong federatedTokenRequest\nwant %+v\n got %+v", want, request)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	resp := federatedTokenResponse{
		AccessToken:     fakeAccessToken,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       3600,
	}
	_ = json.NewEncoder(w).Encode(resp)
}
