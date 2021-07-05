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

package health

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"istio.io/api/networking/v1alpha3"
)

func TestHttpProber(t *testing.T) {
	tests := []struct {
		desc                string
		statusCode          int
		expectedProbeResult ProbeResult
		expectedError       error
	}{
		{
			desc:                "Healthy - 200 status code",
			statusCode:          200,
			expectedProbeResult: Healthy,
			expectedError:       nil,
		},
		{
			desc:                "Unhealthy - 500 status code",
			statusCode:          500,
			expectedProbeResult: Unhealthy,
			expectedError:       errors.New("status code was not from [200,400)"),
		},
		{
			desc:                "Unhealthy - Could not connect to server",
			statusCode:          -1,
			expectedProbeResult: Unhealthy,
			expectedError:       errors.New("dial tcp 127.0.0.1:<port>: connect: connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			server, port := createHTTPServer(tt.statusCode)
			defer server.Close()
			httpProber := NewHTTPProber(
				&v1alpha3.HTTPHealthCheckConfig{
					Path:   "/test/health/check",
					Port:   port,
					Host:   "127.0.0.1",
					Scheme: "http",
				}, false)

			if tt.statusCode == -1 {
				server.Close()
			}

			got, err := httpProber.Probe(time.Second)
			if got != tt.expectedProbeResult || (err == nil && tt.expectedError != nil) || (err != nil && tt.expectedError == nil) {
				t.Errorf("%s: got: %v, expected: %v, got error: %v, expected error %v", tt.desc, got, tt.expectedProbeResult, err, tt.expectedError)
			}
		})
	}
}

func TestTcpProber(t *testing.T) {
	tests := []struct {
		desc                string
		expectedProbeResult ProbeResult
		expectedError       error
	}{
		{
			desc:                "Healthy",
			expectedProbeResult: Healthy,
			expectedError:       nil,
		},
		{
			desc:                "Unhealthy",
			expectedProbeResult: Unhealthy,
			expectedError:       errors.New("dial tcp 127.0.0.1:<port>: connect: connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			server, port := createHTTPServer(200)
			defer server.Close()
			tcpProber := TCPProber{
				Config: &v1alpha3.TCPHealthCheckConfig{
					Host: "127.0.0.1",
					Port: port,
				},
			}

			if tt.expectedProbeResult == Unhealthy {
				server.Close()
			}

			got, err := tcpProber.Probe(time.Second)
			if got != tt.expectedProbeResult || (err == nil && tt.expectedError != nil) || (err != nil && tt.expectedError == nil) {
				t.Errorf("%s: got: %v, expected: %v, got error: %v, expected error %v", tt.desc, got, tt.expectedProbeResult, err, tt.expectedError)
			}
		})
	}
}

func TestExecProber(t *testing.T) {
	tests := []struct {
		desc                string
		command             []string
		expectedProbeResult ProbeResult
		expectedError       error
	}{
		{
			desc:                "Healthy",
			command:             []string{"true"},
			expectedProbeResult: Healthy,
			expectedError:       nil,
		},
		{
			desc:                "Unhealthy",
			command:             []string{"false"},
			expectedProbeResult: Unhealthy,
			expectedError:       errors.New("exit status 1"),
		},
		{
			desc:                "Timeout",
			command:             []string{"sleep", "10"},
			expectedProbeResult: Unhealthy,
			expectedError:       errors.New("command timeout exceeded: signal: killed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			execProber := &ExecProber{
				Config: &v1alpha3.ExecHealthCheckConfig{
					Command: tt.command,
				},
			}

			got, err := execProber.Probe(time.Millisecond * 200)
			if got != tt.expectedProbeResult {
				t.Errorf("got: %v, expected: %v", got, tt.expectedProbeResult)
			}
			errorOrEmpty := func(e error) string {
				if e != nil {
					return e.Error()
				}
				return ""
			}
			if errorOrEmpty(err) != errorOrEmpty(tt.expectedError) {
				t.Errorf("got err: %v, expected err: %v", err, tt.expectedError)
			}
		})
	}
}

func createHTTPServer(statusCode int) (*httptest.Server, uint32) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(statusCode)
	}))

	u, _ := url.Parse(server.URL)
	_, p, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.ParseUint(p, 10, 32)

	return server, uint32(port)
}
