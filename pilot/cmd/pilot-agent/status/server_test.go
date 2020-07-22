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

package status

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test/env"
)

type handler struct{}

const (
	testHeader      = "Some-Header"
	testHeaderValue = "some-value"
	testHostValue   = "host"
)

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/header" {
		if r.Host != testHostValue {
			log.Errorf("Missing expected host header, got %v", r.Host)
			w.WriteHeader(http.StatusBadRequest)
		}
		if r.Header.Get(testHeader) != testHeaderValue {
			log.Errorf("Missing expected Some-Header, got %v", r.Header)
			w.WriteHeader(http.StatusBadRequest)
		}
	}
	if r.URL.Path != "/hello/sunnyvale" && r.URL.Path != "/" {
		return
	}
	w.Write([]byte("welcome, it works"))
}

func TestNewServer(t *testing.T) {
	testCases := []struct {
		probe string
		err   string
	}{
		// Json can't be parsed.
		{
			probe: "invalid-prober-json-encoding",
			err:   "failed to decode",
		},
		// map key is not well formed.
		{
			probe: `{"abc": {"path": "/app-foo/health"}}`,
			err:   "invalid key",
		},
		// invalid probe type
		{
			probe: `{"/app-health/hello-world/readyz": {"tcpSocket": {"port": "8888"}}}`,
			err:   "invalid prober type",
		},
		// Port is not Int typed.
		{
			probe: `{"/app-health/hello-world/readyz": {"httpGet": {"path": "/hello/sunnyvale", "port": "container-port-dontknow"}}}`,
			err:   "must be int type",
		},
		// A valid input.
		{
			probe: `{"/app-health/hello-world/readyz": {"httpGet": {"path": "/hello/sunnyvale", "port": 8080}},` +
				`"/app-health/business/livez": {"httpGet": {"path": "/buisiness/live", "port": 9090}}}`,
		},
		// long request timeout
		{
			probe: `{"/app-health/hello-world/readyz": {"httpGet": {"path": "/hello/sunnyvale", "port": 8080},` +
				`"initialDelaySeconds": 120,"timeoutSeconds": 10,"periodSeconds": 20}}`,
		},
		// A valid input with empty probing path, which happens when HTTPGetAction.Path is not specified.
		{
			probe: `{"/app-health/hello-world/readyz": {"httpGet": {"path": "/hello/sunnyvale", "port": 8080}},
"/app-health/business/livez": {"httpGet": {"port": 9090}}}`,
		},
		// A valid input without any prober info.
		{
			probe: `{}`,
		},
		// A valid input with probing path not starting with /, which happens when HTTPGetAction.Path does not start with a /.
		{
			probe: `{"/app-health/hello-world/readyz": {"httpGet": {"path": "hello/sunnyvale", "port": 8080}},
"/app-health/business/livez": {"httpGet": {"port": 9090}}}`,
		},
	}
	for _, tc := range testCases {
		_, err := NewServer(Config{
			KubeAppProbers: tc.probe,
		})

		if err == nil {
			if tc.err != "" {
				t.Errorf("test case failed [%v], expect error %v", tc.probe, tc.err)
			}
			continue
		}
		if tc.err == "" {
			t.Errorf("test case failed [%v], expect no error, got %v", tc.probe, err)
		}
		// error case, error string should match.
		if !strings.Contains(err.Error(), tc.err) {
			t.Errorf("test case failed [%v], expect error %v, got %v", tc.probe, tc.err, err)
		}
	}
}

func TestStats(t *testing.T) {
	cases := []struct {
		name             string
		envoy            string
		app              string
		output           string
		expectParseError bool
	}{
		{
			name: "envoy metric only",
			envoy: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
			output: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
		},
		{
			name: "app metric only",
			app: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
			output: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
		},
		{
			name: "multiple metric",
			envoy: `# TYPE my_metric counter
my_metric{} 0
`,
			app: `# TYPE my_other_metric counter
my_other_metric{} 0
`,
			output: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
		},
		{
			name:  "agent metric",
			envoy: ``,
			app:   ``,
			// Agent metric is dynamic, so we just check a substring of it not the actual metric
			output: `
# TYPE istio_agent_scrapes_total counter
istio_agent_scrapes_total`,
		},
		// When the application and envoy share a metric, Prometheus will fail. This negative check validates this
		// assumption.
		{
			name: "conflict metric",
			envoy: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
			app: `# TYPE my_metric counter
my_metric{} 0
`,
			output: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
# TYPE my_metric counter
my_metric{} 0
`,
			expectParseError: true,
		},
		{
			name: "conflict metric labeled",
			envoy: `# TYPE my_metric counter
my_metric{app="foo"} 0
`,
			app: `# TYPE my_metric counter
my_metric{app="bar"} 0
`,
			output: `# TYPE my_metric counter
my_metric{app="foo"} 0
# TYPE my_metric counter
my_metric{app="bar"} 0
`,
			expectParseError: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			envoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(tt.envoy)); err != nil {
					t.Fatalf("write failed: %v", err)
				}
			}))
			defer envoy.Close()
			app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(tt.app)); err != nil {
					t.Fatalf("write failed: %v", err)
				}
			}))
			defer app.Close()
			envoyPort, err := strconv.Atoi(strings.Split(envoy.URL, ":")[2])
			if err != nil {
				t.Fatal(err)
			}
			server := &Server{
				prometheus: &PrometheusScrapeConfiguration{
					Port: strings.Split(app.URL, ":")[2],
				},
				envoyStatsPort: envoyPort,
			}
			req := &http.Request{}
			server.handleStats(rec, req)
			if rec.Code != 200 {
				t.Fatalf("handleStats() => %v; want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.output) {
				t.Fatalf("handleStats() => %v; want %v", rec.Body.String(), tt.output)
			}

			parser := expfmt.TextParser{}
			mfMap, err := parser.TextToMetricFamilies(strings.NewReader(rec.Body.String()))
			if err != nil && !tt.expectParseError {
				t.Fatalf("failed to parse metrics: %v", err)
			} else if err == nil && tt.expectParseError {
				t.Fatalf("expected a prse error, got %+v", mfMap)
			}
		})
	}
}

func TestStatsError(t *testing.T) {
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fail.Close()
	pass := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer pass.Close()
	failPort, err := strconv.Atoi(strings.Split(fail.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	passPort, err := strconv.Atoi(strings.Split(pass.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		envoy int
		app   int
	}{
		{"both pass", passPort, passPort},
		{"envoy pass", passPort, failPort},
		{"app pass", failPort, passPort},
		{"both fail", failPort, failPort},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {

			server := &Server{
				prometheus: &PrometheusScrapeConfiguration{
					Port: strconv.Itoa(tt.app),
				},
				envoyStatsPort: tt.envoy,
			}
			req := &http.Request{}
			rec := httptest.NewRecorder()
			server.handleStats(rec, req)
			if rec.Code != 200 {
				t.Fatalf("handleStats() => %v; want 200", rec.Code)
			}
		})
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

	simpleConfig := KubeAppProbers{
		"/app-health/hello-world/readyz": &Prober{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/hello/sunnyvale",
				Port: intstr.IntOrString{IntVal: int32(appPort)},
			},
		},
		"/app-health/hello-world/livez": &Prober{
			HTTPGet: &v1.HTTPGetAction{
				Port: intstr.IntOrString{IntVal: int32(appPort)},
			},
		},
	}
	_ = simpleConfig
	testCases := []struct {
		probePath  string
		config     KubeAppProbers
		statusCode int
	}{
		{
			probePath:  "bad-path-should-be-404",
			config:     simpleConfig,
			statusCode: http.StatusNotFound,
		},
		{
			probePath:  "app-health/hello-world/readyz",
			config:     simpleConfig,
			statusCode: http.StatusOK,
		},
		{
			probePath:  "app-health/hello-world/livez",
			config:     simpleConfig,
			statusCode: http.StatusOK,
		},
		{
			probePath: "app-health/header/readyz",
			config: KubeAppProbers{
				"/app-health/header/readyz": &Prober{
					HTTPGet: &v1.HTTPGetAction{
						Port: intstr.IntOrString{IntVal: int32(appPort)},
						Path: "/header",
						HTTPHeaders: []v1.HTTPHeader{
							{"Host", testHostValue},
							{testHeader, testHeaderValue},
						},
					},
				},
			},
			statusCode: http.StatusOK,
		},
		{
			probePath: "app-health/hello-world/readyz",
			config: KubeAppProbers{
				"/app-health/hello-world/readyz": &Prober{
					HTTPGet: &v1.HTTPGetAction{
						Path: "hello/texas",
						Port: intstr.IntOrString{IntVal: int32(appPort)},
					},
				},
			},
			statusCode: http.StatusOK,
		},
		{
			probePath: "app-health/hello-world/livez",
			config: KubeAppProbers{
				"/app-health/hello-world/livez": &Prober{
					HTTPGet: &v1.HTTPGetAction{
						Path: "hello/texas",
						Port: intstr.IntOrString{IntVal: int32(appPort)},
					},
				},
			},
			statusCode: http.StatusOK,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.probePath, func(t *testing.T) {
			// Starts the pilot agent status server.
			server, err := NewServer(Config{
				StatusPort: 0,
			})
			if err != nil {
				t.Fatalf("failed to create status server %v", err)
			}
			server.appKubeProbers = tc.config
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go server.Run(ctx)

			var statusPort uint16
			for statusPort == 0 {
				server.mutex.RLock()
				statusPort = server.statusPort
				server.mutex.RUnlock()
			}

			client := http.Client{}
			req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%v/%s", statusPort, tc.probePath), nil)
			if err != nil {
				t.Fatalf("[%v] failed to create request", tc.probePath)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal("request failed")
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.statusCode {
				t.Errorf("[%v] unexpected status code, want = %v, got = %v", tc.probePath, tc.statusCode, resp.StatusCode)
			}
		})
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
		KubeAppProbers: fmt.Sprintf(`{"/app-health/hello-world/readyz": {"httpGet": {"path": "/hello/sunnyvale", "port": %v, "scheme": "HTTPS"}},
"/app-health/hello-world/livez": {"httpGet": {"port": %v, "scheme": "HTTPS"}}}`, appPort, appPort),
	})
	if err != nil {
		t.Errorf("failed to create status server %v", err)
		return
	}
	go server.Run(context.Background())

	var statusPort uint16
	if err := retry.UntilSuccess(func() error {
		server.mutex.RLock()
		statusPort = server.statusPort
		server.mutex.RUnlock()
		if statusPort == 0 {
			return fmt.Errorf("no port allocated")
		}
		return nil
	}); err != nil {
		t.Fatalf("failed to getport: %v", err)
	}
	t.Logf("status server starts at port %v, app starts at port %v", statusPort, appPort)
	testCases := []struct {
		probePath  string
		statusCode int
	}{
		{
			probePath:  fmt.Sprintf(":%v/bad-path-should-be-disallowed", statusPort),
			statusCode: http.StatusNotFound,
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

func TestHandleQuit(t *testing.T) {
	statusPort := 15020
	s, err := NewServer(Config{StatusPort: uint16(statusPort)})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		method     string
		remoteAddr string
		expected   int
	}{
		{
			name:       "should send a sigterm for valid requests",
			method:     "POST",
			remoteAddr: "127.0.0.1",
			expected:   http.StatusOK,
		},
		{
			name:       "should send a sigterm for valid ipv6 requests",
			method:     "POST",
			remoteAddr: "[::1]",
			expected:   http.StatusOK,
		},
		{
			name:       "should require POST method",
			method:     "GET",
			remoteAddr: "127.0.0.1",
			expected:   http.StatusMethodNotAllowed,
		},
		{
			name:     "should require localhost",
			method:   "POST",
			expected: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Need to stop SIGTERM from killing the whole test run
			termChannel := make(chan os.Signal, 1)
			signal.Notify(termChannel, syscall.SIGTERM)
			defer signal.Reset(syscall.SIGTERM)

			req, err := http.NewRequest(tt.method, "/quitquitquit", nil)
			if err != nil {
				t.Fatal(err)
			}

			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr + ":15020"
			}

			resp := httptest.NewRecorder()
			s.handleQuit(resp, req)
			if resp.Code != tt.expected {
				t.Fatalf("Expected response code %v got %v", tt.expected, resp.Code)
			}

			if tt.expected == http.StatusOK {
				select {
				case <-termChannel:
				case <-time.After(time.Second):
					t.Fatalf("Failed to receive expected SIGTERM")
				}
			} else if len(termChannel) != 0 {
				t.Fatalf("A SIGTERM was sent when it should not have been")
			}
		})
	}
}
