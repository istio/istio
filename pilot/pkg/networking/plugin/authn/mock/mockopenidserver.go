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
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"istio.io/istio/pkg/log"
)

// OpenID discovery content returned by mock server.
const cfgContent = `
{
	"issuer": "https://accounts.google.com",
	"authorization_endpoint": "https://accounts.google.com/o/oauth2/v2/auth",
	"token_endpoint": "https://www.googleapis.com/oauth2/v4/token",
	"userinfo_endpoint": "https://www.googleapis.com/oauth2/v3/userinfo",
	"revocation_endpoint": "https://accounts.google.com/o/oauth2/revoke",
	"jwks_uri": "https://www.googleapis.com/oauth2/v3/certs",
	"response_types_supported": [
	 "code",
	 "token",
	 "id_token",
	 "code token",
	 "code id_token",
	 "token id_token",
	 "code token id_token",
	 "none"
	],
	"subject_types_supported": [
	 "public"
	],
	"id_token_signing_alg_values_supported": [
	 "RS256"
	],
	"scopes_supported": [
	 "openid",
	 "email",
	 "profile"
	],
	"token_endpoint_auth_methods_supported": [
	 "client_secret_post",
	 "client_secret_basic"
	],
	"claims_supported": [
	 "aud",
	 "email",
	 "email_verified",
	 "exp",
	 "family_name",
	 "given_name",
	 "iat",
	 "iss",
	 "locale",
	 "name",
	 "picture",
	 "sub"
	],
	"code_challenge_methods_supported": [
	 "plain",
	 "S256"
	]
   }
`

// MockOpenIDDiscoveryServer is the in-memory openID discovery server.
type MockOpenIDDiscoveryServer struct {
	port   int
	url    string
	server *http.Server
}

// NewServer creates a mock openID discovery server.
func NewServer(port int) *MockOpenIDDiscoveryServer {
	return &MockOpenIDDiscoveryServer{
		port: port,
		url:  fmt.Sprintf("http://localhost:%d", port),
	}
}

// Start starts the mock server.
func (ms *MockOpenIDDiscoveryServer) Start() error {
	router := mux.NewRouter()
	router.HandleFunc("/.well-known/openid-configuration", ms.openIDCfg).Methods("GET")

	server := &http.Server{
		Addr:    ":" + strconv.Itoa(ms.port),
		Handler: router,
	}

	// Starts the HTTP and waits for it to begin receiving requests.
	// Returns an error if the server doesn't serve traffic within about 2 seconds.
	go func() {
		err := server.ListenAndServe()
		log.Errorf("Server exited with %v, ms is %+v, server is %+v", err, ms, server)
	}()

	wait := 300 * time.Millisecond
	for try := 0; try < 3; try++ {
		time.Sleep(wait)
		// Try to call the server
		if _, err := http.Get(fmt.Sprintf("%s/.well-known/openid-configuration", ms.url)); err != nil {
			log.Infof("Server not yet serving: %v", err)
			// Retry after some sleep.
			wait *= 2
			continue
		}

		log.Infof("Successfully serving on %s", ms.url)

		ms.server = server
		return nil
	}

	ms.Stop()
	return errors.New("server failed to start")
}

// Stop stops he mock server.
func (ms *MockOpenIDDiscoveryServer) Stop() error {
	return ms.server.Close()
}

func (ms *MockOpenIDDiscoveryServer) openIDCfg(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "%v", cfgContent)
}
