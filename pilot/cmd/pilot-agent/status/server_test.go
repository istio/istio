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
	"testing"

	"istio.io/istio/pilot/cmd/pilot-agent/status/app"

	"k8s.io/apimachinery/pkg/util/intstr"
)

type handler struct{}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/hello/sunnyvale" && r.URL.Path != "/" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	_, _ = w.Write([]byte("welcome, it works"))
}

func TestAppProbe(t *testing.T) {
	// Starts the application first.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Errorf("failed to allocate unused port %v", err)
	}
	go func() { _ = http.Serve(listener, &handler{}) }()
	appPort := listener.Addr().(*net.TCPAddr).Port

	// Starts the pilot agent status server.
	server := NewServer(Config{
		StatusPort: 0, // Port will be assigned dynamically.
		AppProbeMap: app.ProbeMap{
			"/app-health/hello-world/readyz": {
				Path: "/hello/sunnyvale",
				Port: kubePort(appPort),
			},
			"/app-health/hello-world/livez": {
				Port: kubePort(appPort),
			},
		},
	})

	if err != nil {
		t.Errorf("failed to create status server %v", err)
		return
	}

	go server.Run(context.Background())

	// Extract the actual status port.
	server.WaitForReady()
	statusPort := server.GetConfig().StatusPort

	t.Logf("status server starts at port %v, app starts at port %v", statusPort, appPort)
	testCases := []struct {
		name       string
		probePath  string
		statusCode int
		err        string
	}{
		{
			name:       "BadPathShouldBeDisallowed",
			probePath:  fmt.Sprintf(":%v/bad-path-should-be-disallowed", statusPort),
			statusCode: http.StatusBadRequest,
		},
		{
			name:       "Readyz",
			probePath:  fmt.Sprintf(":%v/app-health/hello-world/readyz", statusPort),
			statusCode: http.StatusOK,
		},
		{
			name:       "Livez",
			probePath:  fmt.Sprintf(":%v/app-health/hello-world/livez", statusPort),
			statusCode: http.StatusOK,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := http.Client{}
			req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s", tc.probePath), nil)
			if err != nil {
				t.Errorf("[%v] failed to create request", tc.probePath)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal("request failed")
			}
			_ = resp.Body.Close()
			if resp.StatusCode != tc.statusCode {
				t.Errorf("[%v] unexpected status code, want = %v, got = %v", tc.probePath, tc.statusCode, resp.StatusCode)
			}
		})
	}
}

func kubePort(port int) intstr.IntOrString {
	return intstr.IntOrString{
		Type:   intstr.Int,
		IntVal: int32(port),
	}
}
