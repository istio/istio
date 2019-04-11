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

package status

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/env"
)

type handler struct{}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/hello/sunnyvale" && r.URL.Path != "/" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Write([]byte("welcome, it works"))
}

func TestNewServer(t *testing.T) {
	testCases := []struct {
		httpProbe string
		err       string
	}{
		// Json can't be parsed.
		{
			httpProbe: "invalid-prober-json-encoding",
			err:       "failed to decode",
		},
		// map key is not well formed.
		{
			httpProbe: `{"abc": {"path": "/app-foo/health"}}`,
			err:       "invalid key",
		},
		// Port is not Int typed.
		{
			httpProbe: `{"/app-health/hello-world/readyz": {"path": "/hello/sunnyvale", "port": "container-port-dontknow"}}`,
			err:       "must be int type",
		},
		// A valid input.
		{
			httpProbe: `{"/app-health/hello-world/readyz": {"path": "/hello/sunnyvale", "port": 8080},` +
				`"/app-health/business/livez": {"path": "/buisiness/live", "port": 9090}}`,
		},
		// A valid input with empty probing path, which happens when HTTPGetAction.Path is not specified.
		{
			httpProbe: `{"/app-health/hello-world/readyz": {"path": "/hello/sunnyvale", "port": 8080},
"/app-health/business/livez": {"port": 9090}}`,
		},
		// A valid input without any prober info.
		{
			httpProbe: `{}`,
		},
	}
	for _, tc := range testCases {
		_, err := NewServer(Config{
			KubeAppHTTPProbers: tc.httpProbe,
		})

		if err == nil {
			if tc.err != "" {
				t.Errorf("test case failed [%v], expect error %v", tc.httpProbe, tc.err)
			}
			continue
		}
		if tc.err == "" {
			t.Errorf("test case failed [%v], expect no error, got %v", tc.httpProbe, err)
		}
		// error case, error string should match.
		if !strings.Contains(err.Error(), tc.err) {
			t.Errorf("test case failed [%v], expect error %v, got %v", tc.httpProbe, tc.err, err)
		}
	}
}

func TestAppProbe(t *testing.T) {
	// Starts the application first.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Errorf("failed to allocate unused port %v", err)
	}
	go http.Serve(listener, &handler{})
	appPort := listener.Addr().(*net.TCPAddr).Port

	// Starts the pilot agent status server.
	server, err := NewServer(Config{
		StatusPort: 0,
		KubeAppHTTPProbers: fmt.Sprintf(`{"/app-health/hello-world/readyz": {"path": "/hello/sunnyvale", "port": %v},
"/app-health/hello-world/livez": {"port": %v}}`, appPort, appPort),
	})
	if err != nil {
		t.Errorf("failed to create status server %v", err)
		return
	}
	go server.Run(context.Background())

	// We wait a bit here to ensure server's statusPort is updated.
	time.Sleep(time.Second * 3)

	server.mutex.RLock()
	statusPort := server.statusPort
	server.mutex.RUnlock()
	t.Logf("status server starts at port %v, app starts at port %v", statusPort, appPort)
	testCases := []struct {
		probePath  string
		statusCode int
	}{
		{
			probePath:  fmt.Sprintf(":%v/bad-path-should-be-disallowed", statusPort),
			statusCode: http.StatusBadRequest,
		},
		{
			probePath:  fmt.Sprintf(":%v/app-health/hello-world/readyz", statusPort),
			statusCode: http.StatusOK,
		},
		{
			probePath:  fmt.Sprintf(":%v/app-health/hello-world/livez", statusPort),
			statusCode: http.StatusOK,
		},
	}
	for _, tc := range testCases {
		client := http.Client{}
		req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s", tc.probePath), nil)
		if err != nil {
			t.Errorf("[%v] failed to create request", tc.probePath)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("request failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != tc.statusCode {
			t.Errorf("[%v] unexpected status code, want = %v, got = %v", tc.probePath, tc.statusCode, resp.StatusCode)
		}
	}
}

func TestHttpsAppProbe(t *testing.T) {
	// Starts the application first.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Errorf("failed to allocate unused port %v", err)
	}
	keyFile := env.IstioSrc + "/pilot/cmd/pilot-agent/status/test-cert/cert.key"
	crtFile := env.IstioSrc + "/pilot/cmd/pilot-agent/status/test-cert/cert.crt"
	go http.ServeTLS(listener, &handler{}, crtFile, keyFile)
	appPort := listener.Addr().(*net.TCPAddr).Port

	// Starts the pilot agent status server.
	server, err := NewServer(Config{
		StatusPort: 0,
		KubeAppHTTPProbers: fmt.Sprintf(`{"/app-health/hello-world/readyz": {"path": "/hello/sunnyvale", "port": %v, "scheme": "HTTPS"},
"/app-health/hello-world/livez": {"port": %v, "scheme": "HTTPS"}}`, appPort, appPort),
	})
	if err != nil {
		t.Errorf("failed to create status server %v", err)
		return
	}
	go server.Run(context.Background())

	// We wait a bit here to ensure server's statusPort is updated.
	time.Sleep(time.Second * 3)

	server.mutex.RLock()
	statusPort := server.statusPort
	server.mutex.RUnlock()
	t.Logf("status server starts at port %v, app starts at port %v", statusPort, appPort)
	testCases := []struct {
		probePath  string
		statusCode int
	}{
		{
			probePath:  fmt.Sprintf(":%v/bad-path-should-be-disallowed", statusPort),
			statusCode: http.StatusBadRequest,
		},
		{
			probePath:  fmt.Sprintf(":%v/app-health/hello-world/readyz", statusPort),
			statusCode: http.StatusOK,
		},
		{
			probePath:  fmt.Sprintf(":%v/app-health/hello-world/livez", statusPort),
			statusCode: http.StatusOK,
		},
	}
	for _, tc := range testCases {
		client := http.Client{}
		req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s", tc.probePath), nil)
		if err != nil {
			t.Errorf("[%v] failed to create request", tc.probePath)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("request failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != tc.statusCode {
			t.Errorf("[%v] unexpected status code, want = %v, got = %v", tc.probePath, tc.statusCode, resp.StatusCode)
		}
	}
}
