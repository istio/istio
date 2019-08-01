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

package chiron

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const (
	exampleCACert = `-----BEGIN CERTIFICATE-----
MIIDCzCCAfOgAwIBAgIQbfOzhcKTldFipQ1X2WXpHDANBgkqhkiG9w0BAQsFADAv
MS0wKwYDVQQDEyRhNzU5YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUw
HhcNMTkwNTE2MjIxMTI2WhcNMjQwNTE0MjMxMTI2WjAvMS0wKwYDVQQDEyRhNzU5
YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQC6sSAN80Ci0DYFpNDumGYoejMQai42g6nSKYS+ekvs
E7uT+eepO74wj8o6nFMNDu58+XgIsvPbWnn+3WtUjJfyiQXxmmTg8om4uY1C7R1H
gMsrL26pUaXZ/lTE8ZV5CnQJ9XilagY4iZKeptuZkxrWgkFBD7tr652EA3hmj+3h
4sTCQ+pBJKG8BJZDNRrCoiABYBMcFLJsaKuGZkJ6KtxhQEO9QxJVaDoSvlCRGa8R
fcVyYQyXOZ+0VHZJQgaLtqGpiQmlFttpCwDiLfMkk3UAd79ovkhN1MCq+O5N7YVt
eVQWaTUqUV2tKUFvVq21Zdl4dRaq+CF5U8uOqLY/4Kg9AgMBAAGjIzAhMA4GA1Ud
DwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQCg
oF71Ey2b1QY22C6BXcANF1+wPzxJovFeKYAnUqwh3rF7pIYCS/adZXOKlgDBsbcS
MxAGnCRi1s+A7hMYj3sQAbBXttc31557lRoJrx58IeN5DyshT53t7q4VwCzuCXFT
3zRHVRHQnO6LHgZx1FuKfwtkhfSXDyYU2fQYw2Hcb9krYU/alViVZdE0rENXCClq
xO7AQk5MJcGg6cfE5wWAKU1ATjpK4CN+RTn8v8ODLoI2SW3pfsnXxm93O+pp9HN4
+O+1PQtNUWhCfh+g6BN2mYo2OEZ8qGSxDlMZej4YOdVkW8PHmFZTK0w9iJKqM5o1
V6g5gZlqSoRhICK09tpc
-----END CERTIFICATE-----`
)

var (
	fakeCACert = []byte("fake-CA-cert")
)

type mockTLSServer struct {
	httpServer *httptest.Server
}

func TestReadCACert(t *testing.T) {
	testCases := map[string]struct {
		certPath     string
		shouldFail   bool
		expectedCert []byte
	}{
		"cert not exist": {
			certPath:   "./invalid-path/invalid-file",
			shouldFail: true,
		},
		"cert valid": {
			certPath:     "./test-data/example-ca-cert.pem",
			shouldFail:   false,
			expectedCert: []byte(exampleCACert),
		},
		"cert invalid": {
			certPath:   "./test-data/example-invalid-ca-cert.pem",
			shouldFail: true,
		},
	}

	for _, tc := range testCases {
		cert, err := readCACert(tc.certPath)
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed at readCACert()")
			} else {
				// Should fail, skip the current case.
				continue
			}
		} else if err != nil {
			t.Errorf("failed at readCACert(): %v", err)
		}

		if !bytes.Equal(tc.expectedCert, cert) {
			t.Error("the certificate read is unexpected")
		}
	}
}

func TestIsTCPReachable(t *testing.T) {
	server1 := newMockTLSServer(t)
	defer server1.httpServer.Close()
	server2 := newMockTLSServer(t)
	defer server2.httpServer.Close()

	host := "127.0.0.1"
	port1, err := getServerPort(server1.httpServer)
	if err != nil {
		t.Fatalf("error to get the server 1 port: %v", err)
	}
	port2, err := getServerPort(server2.httpServer)
	if err != nil {
		t.Fatalf("error to get the server 2 port: %v", err)
	}

	// Server 1 should be reachable, since it is not closed.
	if !isTCPReachable(host, port1) {
		t.Fatal("server 1 is unreachable")
	}

	// After closing server 2, server 2 should not be reachable
	server2.httpServer.Close()
	if isTCPReachable(host, port2) {
		t.Fatal("server 2 is reachable")
	}
}

func TestRebuildMutatingWebhookConfigHelper(t *testing.T) {
	configFile := "./test-data/example-mutating-webhook-config.yaml"
	configName := "proto-mutate"
	webhookConfig, err := rebuildMutatingWebhookConfigHelper(fakeCACert, configFile, configName)
	if err != nil {
		t.Fatalf("err rebuilding mutating webhook config: %v", err)
	}
	if webhookConfig.Name != configName {
		t.Fatalf("webhookConfig.Name (%v) is different from %v", webhookConfig.Name, configName)
	}
	for i := range webhookConfig.Webhooks {
		if !bytes.Equal(webhookConfig.Webhooks[i].ClientConfig.CABundle, fakeCACert) {
			t.Fatalf("webhookConfig CA bundle(%v) is different from %v",
				webhookConfig.Webhooks[i].ClientConfig.CABundle, fakeCACert)
		}
	}
}

// newMockTLSServer creates a mock TLS server for testing purpose.
func newMockTLSServer(t *testing.T) *mockTLSServer {
	server := &mockTLSServer{}

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		t.Logf("request: %+v", *req)
		switch req.URL.Path {
		default:
			t.Logf("The request contains path: %v", req.URL)
			resp.WriteHeader(http.StatusOK)
		}
	})

	server.httpServer = httptest.NewTLSServer(handler)

	t.Logf("Serving TLS at: %v", server.httpServer.URL)

	return server
}

// Get the server port from server.URL (e.g., https://127.0.0.1:36253)
func getServerPort(server *httptest.Server) (int, error) {
	strs := strings.Split(server.URL, ":")
	if len(strs) < 2 {
		return 0, fmt.Errorf("server.URL is invalid: %v", server.URL)
	}
	port, err := strconv.Atoi(strs[len(strs)-1])
	if err != nil {
		return 0, fmt.Errorf("error to extract port from URL: %v", server.URL)
	}
	return port, nil
}
