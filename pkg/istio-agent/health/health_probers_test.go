package health

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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
			expectedError:       errors.New("Get \"http://127.0.0.1:61023/test/health/check\": dial tcp 127.0.0.1:61023: connect: connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			httpProber := HTTPProber{
				Config: &v1alpha3.HTTPHealthCheckConfig{
					Path:   "/test/health/check",
					Port:   61023,
					Host:   "127.0.0.1",
					Scheme: "http",
				},
			}

			server := createHTTPServer(tt.statusCode)
			if tt.statusCode == -1 {
				server.Close()
			}

			if got, err := httpProber.Probe(time.Second); got != tt.expectedProbeResult || (err == nil && tt.expectedError != nil) || (err != nil && tt.expectedError == nil) {
				server.Close()
				t.Errorf("%s: got: %v, expected: %v, got error: %v, expected error %v", tt.desc, got, tt.expectedProbeResult, err, tt.expectedError)
			}

			server.Close()
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
			expectedError:       errors.New("dial tcp 127.0.0.1:61023: connect: connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tcpProber := TCPProber{
				Config: &v1alpha3.TCPHealthCheckConfig{
					Host: "127.0.0.1",
					Port: 61023,
				},
			}

			srv, _ := net.Listen("tcp", "127.0.0.1:61023")
			if tt.expectedProbeResult == Unhealthy {
				srv.Close()
			}

			if got, err := tcpProber.Probe(time.Second); got != tt.expectedProbeResult || (err == nil && tt.expectedError != nil) || (err != nil && tt.expectedError == nil) {
				srv.Close()
				t.Errorf("%s: got: %v, expected: %v, got error: %v, expected error %v", tt.desc, got, tt.expectedProbeResult, err, tt.expectedError)
			}

			srv.Close()
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
			command:             []string{"/usr/bin/whoami"},
			expectedProbeResult: Healthy,
			expectedError:       nil,
		},
		{
			desc:                "Unhealthy",
			command:             []string{"/usr/bin/foooobarrrrrr"},
			expectedProbeResult: Unhealthy,
			expectedError:       errors.New("fork/exec /usr/bin/foooobarrrrrr: no such file or directory"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			execProber := &ExecProber{
				Config: &v1alpha3.ExecHealthCheckConfig{
					Command: tt.command,
				},
			}

			if got, err := execProber.Probe(time.Second); got != tt.expectedProbeResult || (err == nil && tt.expectedError != nil) || (err != nil && tt.expectedError == nil) {
				t.Errorf("%s: got: %v, expected: %v, got error: %v, expected error %v", tt.desc, got, tt.expectedProbeResult, err, tt.expectedError)
			}
		})
	}
}

func createHTTPServer(statusCode int) *httptest.Server {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(statusCode)
	}))

	listener, err := net.Listen("tcp", "127.0.0.1:61023")
	if err != nil {
		panic("Could not create listener for test: " + err.Error())
	}

	server.Listener.Close()
	server.Listener = listener
	server.Start()
	return server
}
