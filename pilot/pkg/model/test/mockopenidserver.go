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

package test

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"istio.io/pkg/log"
)

var (
	cfgContent  = "{\"jwks_uri\": \"%s\"}"
	serverMutex = &sync.Mutex{}
)

const (
	// JwtPubKey1 is the response to 1st call for JWT public key returned by mock server.
	JwtPubKey1 = `{ "keys": [ { "kid": "fakeKey1_1", "alg": "RS256", "kty": "RSA", "n": "abc", "e": "def" },
			{ "kid": "fakeKey1_2", "alg": "RS256", "kty": "RSA", "n": "123", "e": "456" } ] }`

	// JwtPubKey1Reordered is the response to 1st call for JWT public key returned by mock server, but in a modified order of json elements.
	JwtPubKey1Reordered = `{ "keys": [ { "alg": "RS256", "kid": "fakeKey1_2", "n": "123", "kty": "RSA", "e": "456" },
			{ "n": "abc", "alg": "RS256", "kty": "RSA", "kid": "fakeKey1_1", "e": "def" } ] }`

	// JwtPubKey2 is the response to later calls for JWT public key returned by mock server.
	JwtPubKey2 = `{ "keys": [ { "kid": "fakeKey2_1", "alg": "RS256", "kty": "RSA", "n": "ghi", "e": "lmn" },
			{ "kid": "fakeKey2_2", "alg": "RS256", "kty": "RSA", "n": "789", "e": "1234" } ] }`

	JwtPubKeyNoKid = `{ "keys": [ { "alg": "RS256", "kty": "RSA", "n": "abc", "e": "def" },
			{ "alg": "RS256", "kty": "RSA", "n": "123", "e": "456" } ] }`

	JwtPubKeyNoKid2 = `{ "keys": [ { "alg": "RS256", "kty": "RSA", "n": "ghi", "e": "lmn" },
			{ "alg": "RS256", "kty": "RSA", "n": "789", "e": "123" } ] }`

	JwtPubKeyNoKeys = `{ "pub": [ { "kid": "fakeKey1_1", "alg": "RS256", "kty": "RSA", "n": "abc", "e": "def" },
			{ "kid": "fakeKey1_2", "alg": "RS256", "kty": "RSA", "n": "123", "e": "456" } ] }`

	JwtPubKeyNoKeys2 = `{ "pub": [ { "kid": "fakeKey1_3", "alg": "RS256", "kty": "RSA", "n": "abc", "e": "def" },
			{ "kid": "fakeKey1_4", "alg": "RS256", "kty": "RSA", "n": "123", "e": "456" } ] }`

	JwtPubKeyExtraElements = `{ "keys": [ { "kid": "fakeKey1_1", "alg": "RS256", "kty": "RSA", "n": "abc", "e": "def", "bla": "blah" },
			{ "kid": "fakeKey1_2", "alg": "RS256", "kty": "RSA", "n": "123", "e": "456", "bla": "blah" } ] }`
)

// MockOpenIDDiscoveryServer is the in-memory openID discovery server.
type MockOpenIDDiscoveryServer struct {
	Port   int
	URL    string
	server *http.Server

	// How many times openIDCfg is called, use this number to verify cache takes effect.
	OpenIDHitNum uint64

	// How many times jwtPubKey is called, use this number to verify cache takes effect.
	PubKeyHitNum uint64

	// The mock server will return an error for the first number of hits for public key, this is used
	// to simulate network errors and test the retry logic in jwks resolver for public key fetch.
	ReturnErrorForFirstNumHits uint64

	// The mock server will start to return an error after the first number of hits for public key,
	// this is used to simulate network errors and test the refresh logic in jwks resolver.
	ReturnErrorAfterFirstNumHits uint64

	// The mock server will start to return an error after the first number of hits for public key,
	// this is used to simulate network errors and test the refresh logic in jwks resolver.
	ReturnReorderedKeyAfterFirstNumHits uint64

	// If both TLSKeyFile and TLSCertFile are set, Start() will attempt to start a HTTPS server.
	TLSKeyFile  string
	TLSCertFile string
}

// StartNewServer creates a mock openID discovery server and starts it
func StartNewServer() (*MockOpenIDDiscoveryServer, error) {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	server := &MockOpenIDDiscoveryServer{
		// 0 means the mock server always return the success result.
		ReturnErrorForFirstNumHits:   0,
		ReturnErrorAfterFirstNumHits: 0,
	}

	return server, server.Start()
}

// StartNewTLSServer creates a mock openID discovery server that serves HTTPS and starts it
func StartNewTLSServer(tlsCert, tlsKey string) (*MockOpenIDDiscoveryServer, error) {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	server := &MockOpenIDDiscoveryServer{
		// 0 means the mock server always return the success result.
		ReturnErrorForFirstNumHits:   0,
		ReturnErrorAfterFirstNumHits: 0,

		TLSCertFile: tlsCert,
		TLSKeyFile:  tlsKey,
	}

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

	scheme := "http"
	if ms.TLSCertFile != "" && ms.TLSKeyFile != "" {
		scheme = "https"
	}

	port := ln.Addr().(*net.TCPAddr).Port
	ms.URL = fmt.Sprintf("%s://localhost:%d", scheme, port)
	server.Addr = ":" + strconv.Itoa(port)

	// Starts the HTTP and waits for it to begin receiving requests.
	// Returns an error if the server doesn't serve traffic within about 2 seconds.
	go func() {
		if scheme == "https" {
			if err := server.ServeTLS(ln, ms.TLSCertFile, ms.TLSKeyFile); err != nil {
				log.Errorf("Server failed to serve TLS in %q: %v", ms.URL, err)
			}
			return
		}
		if err := server.Serve(ln); err != nil {
			log.Errorf("Server failed to serve in %q: %v", ms.URL, err)
		}
	}()

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	wait := 300 * time.Millisecond
	for try := 0; try < 5; try++ {
		time.Sleep(wait)
		// Try to call the server
		if _, err := httpClient.Get(fmt.Sprintf("%s/.well-known/openid-configuration", ms.URL)); err != nil {
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

	_ = ms.Stop()
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
	if ms.ReturnErrorAfterFirstNumHits != 0 && atomic.LoadUint64(&ms.PubKeyHitNum) > ms.ReturnErrorAfterFirstNumHits {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, "Mock server configured to return error after %d hits", ms.ReturnErrorAfterFirstNumHits)
		return
	}

	if atomic.LoadUint64(&ms.PubKeyHitNum) <= ms.ReturnErrorForFirstNumHits {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, "Mock server configured to return error until %d retries", ms.ReturnErrorForFirstNumHits)
		return
	}

	if atomic.LoadUint64(&ms.PubKeyHitNum) == ms.ReturnErrorForFirstNumHits+1 {
		fmt.Fprintf(w, "%v", JwtPubKey1)
		return
	}

	if ms.ReturnReorderedKeyAfterFirstNumHits != 0 && atomic.LoadUint64(&ms.PubKeyHitNum) >= ms.ReturnReorderedKeyAfterFirstNumHits+1 {
		fmt.Fprintf(w, "%v", JwtPubKey1Reordered)
		return
	}

	fmt.Fprintf(w, "%v", JwtPubKey2)
}
