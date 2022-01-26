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

package testserver

import (
	"net"
	"net/http"
	"net/http/httptest"
)

// CreateAndStartServer starts a server and returns the response passed.
func CreateAndStartServer(response string) *httptest.Server {
	return createHTTPServer(createDefaultFuncMap(response))
}

func createHTTPServer(handlers map[string]func(rw http.ResponseWriter, _ *http.Request)) *httptest.Server {
	mux := http.NewServeMux()
	for k, v := range handlers {
		mux.HandleFunc(k, http.HandlerFunc(v))
	}

	// Start a local HTTP server
	server := httptest.NewUnstartedServer(mux)

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		panic("Could not create listener for test: " + err.Error())
	}
	server.Listener = l
	server.Start()
	return server
}

func createDefaultFuncMap(statsToReturn string) map[string]func(rw http.ResponseWriter, _ *http.Request) {
	return map[string]func(rw http.ResponseWriter, _ *http.Request){
		"/stats": func(rw http.ResponseWriter, _ *http.Request) {
			// Send response to be tested
			_, err := rw.Write([]byte(statsToReturn))
			if err != nil {
				panic("Could not write response: " + err.Error())
			}
		},
	}
}
