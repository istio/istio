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

package test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"istio.io/istio/pkg/log"
)

var fakeaccesstoken = "footoken"

// MockServer is the in-memory secure token service.
type MockServer struct {
	Port   int
	URL    string
	server *http.Server
}

// StartNewServer creates a mock server and starts it
func StartNewServer() (*MockServer, error) {
	server := &MockServer{}
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

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"` // Expiration time in seconds
}

func (ms *MockServer) getFederatedToken(w http.ResponseWriter, req *http.Request) {
	resp := federatedTokenResponse{
		AccessToken:     fakeaccesstoken,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       3600,
	}
	_ = json.NewEncoder(w).Encode(resp)

}
