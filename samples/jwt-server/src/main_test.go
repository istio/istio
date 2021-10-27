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

package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
)

func TestJwtHTTPServer(t *testing.T) {
	server := NewJwtServer("", "")
	// Start the test server on random port.
	go server.runHTTP("localhost:0")
	// Prepare the HTTP request.
	httpClient := &http.Client{}
	httpReq, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/jwtkeys", <-server.httpPort), nil)
	if err != nil {
		t.Fatalf(err.Error())
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		t.Fatalf(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected to get %d, got %d", http.StatusOK, resp.StatusCode)
	}
}

func TestJwtHTTPSServer(t *testing.T) {
	var (
		exampleCert = "../../../../../jwt/example.crt"
		exampleKey  = "../../../../../jwt/example.key"
	)
	server := NewJwtServer(exampleCert, exampleKey)

	// Start the test server on random port.
	go server.runHTTPS(":8443")

	// Prepare the HTTPS request.
	httpsClient := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	httpReq, err := http.NewRequest("GET", fmt.Sprintf("https://localhost:%d/jwtkeys", <-server.httpsPort), nil)
	if err != nil {
		t.Fatalf(err.Error())
	}
	resp, err := httpsClient.Do(httpReq)
	if err != nil {
		t.Fatalf(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected to get %d, got %d", http.StatusOK, resp.StatusCode)
	}
}
