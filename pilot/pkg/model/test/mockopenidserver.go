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
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"istio.io/istio/pkg/log"
)

var (
	cfgContent  = "{\"jwks_uri\": \"%s\"}"
	serverMutex = &sync.Mutex{}
)

const (
	// JwtPubKey1 is the response to 1st call for JWT public key returned by mock server.
	JwtPubKey1 = "fakeKey1"

	// JwtPubKey2 is the response to later calls for JWT public key returned by mock server.
	JwtPubKey2 = "fakeKey2"
)

// MockOpenIDDiscoveryServer is the in-memory openID discovery server.
type MockOpenIDDiscoveryServer struct {
	Port   int
	URL    string
	server *http.Server

	// How many times openIDCfg is called, use this number to verfiy cache takes effect.
	OpenIDHitNum uint64

	// How many times jwtPubKey is called, use this number to verfiy cache takes effect.
	PubKeyHitNum uint64
}

// StartNewServer creates a mock openID discovery server and starts it
func StartNewServer() (*MockOpenIDDiscoveryServer, error) {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	server := &MockOpenIDDiscoveryServer{}

	return server, server.Start()
}

// Start starts the mock server.
func (ms *MockOpenIDDiscoveryServer) Start() error {
	router := mux.NewRouter()
	router.HandleFunc("/.well-known/openid-configuration", ms.openIDCfg).Methods("GET")
	router.HandleFunc("/oauth2/v3/certs", ms.jwtPubKey).Methods("GET")

	server := &http.Server{
		Addr:    ":" + strconv.Itoa(ms.Port),
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

	// Starts the HTTP and waits for it to begin receiving requests.
	// Returns an error if the server doesn't serve traffic within about 2 seconds.
	go func() {
		if err := server.Serve(ln); err != nil {
			log.Errorf("Server failed to serve in %q: %v", ms.URL, err)
		}
	}()

	wait := 300 * time.Millisecond
	for try := 0; try < 5; try++ {
		time.Sleep(wait)
		// Try to call the server
		if _, err := http.Get(fmt.Sprintf("%s/.well-known/openid-configuration", ms.URL)); err != nil {
			log.Infof("Server not yet serving: %v", err)
			// Retry after some sleep.
			wait *= 2
			continue
		}

		log.Infof("Successfully serving on %s", ms.URL)
		atomic.StoreUint64(&ms.OpenIDHitNum, 0)
		atomic.StoreUint64(&ms.PubKeyHitNum, 0)
		ms.server = server
		return nil
	}

	ms.Stop()
	return errors.New("server failed to start")
}

// Stop stops he mock server.
func (ms *MockOpenIDDiscoveryServer) Stop() error {
	atomic.StoreUint64(&ms.OpenIDHitNum, 0)
	atomic.StoreUint64(&ms.PubKeyHitNum, 0)
	if ms.server == nil {
		return nil
	}

	return ms.server.Close()
}

func (ms *MockOpenIDDiscoveryServer) openIDCfg(w http.ResponseWriter, req *http.Request) {
	atomic.AddUint64(&ms.OpenIDHitNum, 1)
	fmt.Fprintf(w, "%v", fmt.Sprintf(cfgContent, ms.URL+"/oauth2/v3/certs"))
}

func (ms *MockOpenIDDiscoveryServer) jwtPubKey(w http.ResponseWriter, req *http.Request) {
	atomic.AddUint64(&ms.PubKeyHitNum, 1)

	if atomic.LoadUint64(&ms.PubKeyHitNum) == 1 {
		fmt.Fprintf(w, "%v", JwtPubKey1)
		return
	}

	fmt.Fprintf(w, "%v", JwtPubKey2)
}
