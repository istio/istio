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
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"istio.io/pkg/log"
)

var (
	FakeFederatedToken   = "FakeFederatedToken"
	FakeAccessToken      = "FakeAccessToken"
	FakeTrustDomain      = "FakeTrustDomain"
	FakeSubjectToken     = "FakeSubjectToken"
	FakeProjectNum       = "1234567"
	FakeGKEClusterURL    = "https://container.googleapis.com/v1/projects/fakeproject/locations/fakelocation/clusters/fakecluster"
	FakeExpiresInSeconds = 3600
)

type federatedTokenRequest struct {
	Audience           string `json:"audience"`
	GrantType          string `json:"grantType"`
	RequestedTokenType string `json:"requestedTokenType"`
	SubjectTokenType   string `json:"subjectTokenType"`
	SubjectToken       string `json:"subjectToken"`
	Scope              string `json:"scope"`
}

type federatedTokenResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int32  `json:"expires_in"` // Expiration time in seconds
}

type Duration struct {
	// Signed seconds of the span of time. Must be from -315,576,000,000
	// to +315,576,000,000 inclusive. Note: these bounds are computed from:
	// 60 sec/min * 60 min/hr * 24 hr/day * 365.25 days/year * 10000 years
	Seconds int64 `json:"seconds"`
}

type accessTokenRequest struct {
	Name      string   `json:"name"`
	Delegates []string `json:"delegates"` // nolint: structcheck, unused
	Scope     []string `json:"scope"`
	LifeTime  Duration `json:"lifetime"` // nolint: structcheck, unused
}

type accessTokenResponse struct {
	AccessToken string `json:"accessToken"`
	ExpireTime  string `json:"expireTime"`
}

// AuthorizationServer mocks google secure token server.
// nolint: maligned
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
	accessTokenLife             int // life of issued access token in seconds
	accessToken                 string
	enableDynamicAccessToken    bool // whether generates different token each time
	numGetFederatedTokenCalls   int
	numGetAccessTokenCalls      int
	blockFederatedTokenRequest  bool
	blockAccessTokenRequest     bool
}

type Config struct {
	Port         int
	SubjectToken string
	TrustDomain  string
	AccessToken  string
}

// StartNewServer creates a mock server and starts it. The server listens on
// port for requests. If port is 0, a randomly chosen port is in use.
func StartNewServer(t *testing.T, conf Config) (*AuthorizationServer, error) {
	st := FakeSubjectToken
	if conf.SubjectToken != "" {
		st = conf.SubjectToken
	}
	aud := fmt.Sprintf("identitynamespace:%s:%s", FakeTrustDomain, FakeGKEClusterURL)
	if conf.TrustDomain != "" {
		aud = fmt.Sprintf("identitynamespace:%s:%s", conf.TrustDomain, FakeGKEClusterURL)
	}
	token := FakeAccessToken
	if conf.AccessToken != "" {
		token = conf.AccessToken
	}
	server := &AuthorizationServer{
		t: t,
		expectedFederatedTokenRequest: federatedTokenRequest{
			Audience:           aud,
			GrantType:          "urn:ietf:params:oauth:grant-type:token-exchange",
			RequestedTokenType: "urn:ietf:params:oauth:token-type:access_token",
			SubjectTokenType:   "urn:ietf:params:oauth:token-type:jwt",
			SubjectToken:       st,
			Scope:              "https://www.googleapis.com/auth/cloud-platform",
		},
		expectedAccessTokenRequest: accessTokenRequest{
			Name:  fmt.Sprintf("projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken", FakeProjectNum),
			Scope: []string{"https://www.googleapis.com/auth/cloud-platform"},
		},
		accessTokenLife:            3600,
		accessToken:                token,
		enableDynamicAccessToken:   false,
		blockFederatedTokenRequest: false,
		blockAccessTokenRequest:    false,
	}
	return server, server.Start(conf.Port)
}

func (ms *AuthorizationServer) SetGenFedTokenError(err error) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.generateFederatedTokenError = err
}

func (ms *AuthorizationServer) BlockFederatedTokenRequest(block bool) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.blockFederatedTokenRequest = block
}

func (ms *AuthorizationServer) BlockAccessTokenRequest(block bool) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.blockAccessTokenRequest = block
}

func (ms *AuthorizationServer) SetGenAcsTokenError(err error) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.generateAccessTokenError = err
}

// SetTokenLifeTime sets life time of issued access token to d seconds
func (ms *AuthorizationServer) SetTokenLifeTime(d int) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.accessTokenLife = d
}

// SetAccessToken sets the issued access token to token
func (ms *AuthorizationServer) SetAccessToken(token string) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.accessToken = token
}

// SetAccessToken sets the issued access token to token
func (ms *AuthorizationServer) EnableDynamicAccessToken(enable bool) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.enableDynamicAccessToken = enable
}

func (ms *AuthorizationServer) NumGetAccessTokenCalls() int {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	return ms.numGetAccessTokenCalls
}

func (ms *AuthorizationServer) NumGetFederatedTokenCalls() int {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	return ms.numGetFederatedTokenCalls
}

// Start starts the mock server.
func (ms *AuthorizationServer) Start(port int) error {
	atEndpoint := fmt.Sprintf("/v1/projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken", FakeProjectNum)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/identitybindingtoken", ms.getFederatedToken)
	mux.HandleFunc(atEndpoint, ms.getAccessToken)
	ms.t.Logf("Registered handler for endpoints:\n%s\n%s", atEndpoint, "/v1/identitybindingtoken")
	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Errorf("Server failed to listen %v", err)
		return err
	}
	// If passed in port is 0, get the actual chosen port.
	port = ln.Addr().(*net.TCPAddr).Port

	ms.Port = port
	ms.URL = fmt.Sprintf("http://localhost:%d", port)

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
	return ms.server.Shutdown(context.TODO())
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

	ms.mutex.Lock()
	ms.numGetFederatedTokenCalls++
	want := ms.expectedFederatedTokenRequest
	fakeErr := ms.generateFederatedTokenError
	blockRequest := ms.blockFederatedTokenRequest
	ms.mutex.Unlock()

	if blockRequest {
		time.Sleep(1 * time.Hour)
	}

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
		ExpiresIn:       int32(FakeExpiresInSeconds),
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

	ms.mutex.Lock()
	ms.numGetAccessTokenCalls++
	want := ms.expectedAccessTokenRequest
	fakeErr := ms.generateAccessTokenError
	tokenLife := time.Now().Add(time.Duration(ms.accessTokenLife) * time.Second)
	token := ms.accessToken
	if ms.enableDynamicAccessToken {
		token += time.Now().String()
	}
	blockRequest := ms.blockAccessTokenRequest
	ms.mutex.Unlock()

	if blockRequest {
		time.Sleep(1 * time.Hour)
	}

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
		AccessToken: token,
		ExpireTime:  tokenLife.Format(time.RFC3339Nano),
	}

	_ = json.NewEncoder(w).Encode(resp)
}
