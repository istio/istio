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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/textparse"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpcHealth "google.golang.org/grpc/health/grpc_health_v1"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pilot/cmd/pilot-agent/status/testserver"
	"istio.io/istio/pkg/kube/apimirror"
	"istio.io/istio/pkg/lazy"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

type handler struct {
	// LastALPN stores the most recent ALPN requested. This is needed to determine info about a request,
	// since the appProber strips all headers/responses.
	lastAlpn *atomic.String
}

const (
	testHeader      = "Some-Header"
	testHeaderValue = "some-value"
	testHostValue   = "test.com:9999"
)

var liveServerStats = "cluster_manager.cds.update_success: 1\nlistener_manager.lds.update_success: 1\nserver.state: 0\nlistener_manager.workers_started: 1"

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastAlpn.Store(r.Proto)
	segments := strings.Split(r.URL.Path[1:], "/")
	switch segments[0] {
	case "header":
		if r.Host != testHostValue {
			log.Errorf("Missing expected host value %s, got %v", testHostValue, r.Host)
			w.WriteHeader(http.StatusBadRequest)
		}
		if r.Header.Get(testHeader) != testHeaderValue {
			log.Errorf("Missing expected Some-Header, got %v", r.Header)
			w.WriteHeader(http.StatusBadRequest)
		}
	case "redirect":
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	case "redirect-loop":
		http.Redirect(w, r, "/redirect-loop", http.StatusMovedPermanently)
	case "remote-redirect":
		http.Redirect(w, r, "http://example.com/foo", http.StatusMovedPermanently)
	case "", "hello/sunnyvale":
		w.Write([]byte("welcome, it works"))
	case "status":
		code, _ := strconv.Atoi(segments[1])
		w.Header().Set("Location", "/")
		w.WriteHeader(code)
	default:
		return
	}
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
			err:   "invalid path",
		},
		// invalid probe type
		{
			probe: `{"/app-health/hello-world/readyz": {"exec": {"command": [ "true" ]}}}`,
			err:   "invalid prober type",
		},
		// tcp probes are valid as well
		{
			probe: `{"/app-health/hello-world/readyz": {"tcpSocket": {"port": 8888}}}`,
		},
		// probes must be one of tcp, http or gRPC
		{
			probe: `{"/app-health/hello-world/readyz": {"tcpSocket": {"port": 8888}, "httpGet": {"path": "/", "port": 7777}}}`,
			err:   "must be one of type httpGet, tcpSocket or gRPC",
		},
		// probes must be one of tcp, http or gRPC
		{
			probe: `{"/app-health/hello-world/readyz": {"grpc": {"port": 8888}, "httpGet": {"path": "/", "port": 7777}}}`,
			err:   "must be one of type httpGet, tcpSocket or gRPC",
		},
		// Port is not Int typed (tcpSocket).
		{
			probe: `{"/app-health/hello-world/readyz": {"tcpSocket": {"port": "tcp"}}}`,
			err:   "must be int type",
		},
		// Port is not Int typed (httpGet).
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
		// A valid gRPC probe.
		{
			probe: `{"/app-health/hello-world/readyz": {"gRPC": {"port": 8080}}}`,
		},
		// A valid gRPC probe with null service.
		{
			probe: `{"/app-health/hello-world/readyz": {"gRPC": {"port": 8080, "service": null}}}`,
		},
		// A valid gRPC probe with service.
		{
			probe: `{"/app-health/hello-world/readyz": {"gRPC": {"port": 8080, "service": "foo"}}}`,
		},
		// A valid gRPC probe with service and timeout.
		{
			probe: `{"/app-health/hello-world/readyz": {"gRPC": {"port": 8080, "service": "foo"}, "timeoutSeconds": 10}}`,
		},
	}
	for _, tc := range testCases {
		_, err := NewServer(Options{
			KubeAppProbers:     tc.probe,
			PrometheusRegistry: TestingRegistry(t),
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

func NewTestServer(t test.Failer, o Options) *Server {
	if o.PrometheusRegistry == nil {
		o.PrometheusRegistry = TestingRegistry(t)
	}
	server, err := NewServer(o)
	if err != nil {
		t.Fatalf("failed to create status server %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go server.Run(ctx)

	if err := retry.UntilSuccess(func() error {
		server.mutex.RLock()
		statusPort := server.statusPort
		server.mutex.RUnlock()
		if statusPort == 0 {
			return fmt.Errorf("no port allocated")
		}
		return nil
	}, retry.Delay(time.Microsecond)); err != nil {
		t.Fatalf("failed to getport: %v", err)
	}

	return server
}

func TestPprof(t *testing.T) {
	pprofPath := "/debug/pprof/cmdline"
	// Starts the pilot agent status server.
	server := NewTestServer(t, Options{EnableProfiling: true})
	client := http.Client{}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%v/%s", server.statusPort, pprofPath), nil)
	if err != nil {
		t.Fatalf("[%v] failed to create request", pprofPath)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal("request failed: ", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("[%v] unexpected status code, want = %v, got = %v", pprofPath, http.StatusOK, resp.StatusCode)
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
				http:           &http.Client{},
				registry:       TestingRegistry(t),
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

func TestNegotiateMetricsFormat(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		expected    expfmt.Format
	}{
		{
			name:        "openmetrics minimal accept header",
			contentType: `application/openmetrics-text; version=0.0.1`,
			expected:    FmtOpenMetrics_0_0_1,
		},
		{
			name:        "openmetrics minimal v1 accept header",
			contentType: `application/openmetrics-text; version=1.0.0`,
			expected:    FmtOpenMetrics_1_0_0,
		},
		{
			name:        "openmetrics accept header",
			contentType: `application/openmetrics-text; version=0.0.1; charset=utf-8`,
			expected:    FmtOpenMetrics_0_0_1,
		},
		{
			name:        "openmetrics v1 accept header",
			contentType: `application/openmetrics-text; version=1.0.0; charset=utf-8`,
			expected:    FmtOpenMetrics_1_0_0,
		},
		{
			name:        "plaintext accept header",
			contentType: "text/plain; version=0.0.4; charset=utf-8",
			expected:    expfmt.NewFormat(expfmt.TypeTextPlain),
		},
		{
			name:        "empty accept header",
			contentType: "",
			expected:    expfmt.NewFormat(expfmt.TypeTextPlain),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, negotiateMetricsFormat(tt.contentType), tt.expected)
		})
	}
}

func TestStatsContentType(t *testing.T) {
	appOpenMetrics := `# TYPE jvm info
# HELP jvm VM version info
jvm_info{runtime="OpenJDK Runtime Environment",vendor="AdoptOpenJDK",version="16.0.1+9"} 1.0
# TYPE jmx_config_reload_success counter
# HELP jmx_config_reload_success Number of times configuration have successfully been reloaded.
jmx_config_reload_success_total 0.0
jmx_config_reload_success_created 1.623984612719E9
# EOF
`
	appText004 := `# HELP jvm_info VM version info
# TYPE jvm_info gauge
jvm_info{runtime="OpenJDK Runtime Environment",vendor="AdoptOpenJDK",version="16.0.1+9",} 1.0
# HELP jmx_config_reload_failure_created Number of times configuration have failed to be reloaded.
# TYPE jmx_config_reload_failure_created gauge
jmx_config_reload_failure_created 1.624025983489E9
`
	envoy := `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`
	cases := []struct {
		name             string
		acceptHeader     string
		expectParseError bool
	}{
		{
			name:         "openmetrics accept header",
			acceptHeader: `application/openmetrics-text; version=0.0.1,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`,
		},
		{
			name:         "openmetrics v1 accept header",
			acceptHeader: `application/openmetrics-text; version=1.0.0,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`,
		},
		{
			name:         "plaintext accept header",
			acceptHeader: string(FmtText),
		},
		{
			name:         "empty accept header",
			acceptHeader: "",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			envoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(envoy)); err != nil {
					t.Fatalf("write failed: %v", err)
				}
			}))
			defer envoy.Close()
			app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				format := expfmt.NegotiateIncludingOpenMetrics(r.Header)
				var negotiatedMetrics string
				if strings.Contains(string(format), "text/plain") {
					negotiatedMetrics = appText004
				} else {
					negotiatedMetrics = appOpenMetrics
				}
				w.Header().Set("Content-Type", string(format))
				if _, err := w.Write([]byte(negotiatedMetrics)); err != nil {
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
				registry:       TestingRegistry(t),
				envoyStatsPort: envoyPort,
				http:           &http.Client{},
			}
			req := &http.Request{}
			req.Header = make(http.Header)
			req.Header.Add("Accept", tt.acceptHeader)
			server.handleStats(rec, req)
			if rec.Code != 200 {
				t.Fatalf("handleStats() => %v; want 200", rec.Code)
			}

			if negotiateMetricsFormat(rec.Header().Get("Content-Type")) == FmtText {
				textParser := expfmt.TextParser{}
				_, err := textParser.TextToMetricFamilies(strings.NewReader(rec.Body.String()))
				if err != nil {
					t.Fatalf("failed to parse text metrics: %v", err)
				}
			} else {
				omParser := textparse.NewOpenMetricsParser(rec.Body.Bytes(), labels.NewSymbolTable())
				for {
					_, err := omParser.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("failed to parse openmetrics: %v", err)
					}
				}
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
				registry:       TestingRegistry(t),
				envoyStatsPort: tt.envoy,
				http:           &http.Client{},
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

// initServerWithSize size is kB
func initServerWithSize(t *testing.B, size int) *Server {
	appText := `# TYPE jvm info
# HELP jvm VM version info
jvm_info{runtime="OpenJDK Runtime Environment",vendor="AdoptOpenJDK",version="16.0.1+9"} 1.0
# TYPE jmx_config_reload_success counter
# HELP jmx_config_reload_success Number of times configuration have successfully been reloaded.
jmx_config_reload_success_total 0.0
jmx_config_reload_success_created 1.623984612719E9
`
	appOpenMetrics := appText + "# EOF"

	envoy := strings.Builder{}
	envoy.Grow(size << 10 * 100)
	envoy.WriteString(`# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`)
	for i := 0; envoy.Len()+len(appText) < size<<10; i++ {
		envoy.WriteString("#TYPE my_other_metric_" + strconv.Itoa(i) + " counter\nmy_other_metric_" + strconv.Itoa(i) + " 0\n")
	}
	eb := []byte(envoy.String())

	envoyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(eb); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}))
	t.Cleanup(envoyServer.Close)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		format := expfmt.NegotiateIncludingOpenMetrics(r.Header)
		var negotiatedMetrics string
		if format == FmtText {
			negotiatedMetrics = appText
		} else {
			negotiatedMetrics = appOpenMetrics
		}
		w.Header().Set("Content-Type", string(format))
		if _, err := w.Write([]byte(negotiatedMetrics)); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}))
	t.Cleanup(app.Close)
	envoyPort, err := strconv.Atoi(strings.Split(envoyServer.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	registry, err := initializeMonitoring()
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		registry: registry,
		prometheus: &PrometheusScrapeConfiguration{
			Port: strings.Split(app.URL, ":")[2],
		},
		envoyStatsPort: envoyPort,
		http:           &http.Client{},
	}
	t.ResetTimer()
	return server
}

func BenchmarkStats(t *testing.B) {
	tests := map[int]string{
		1:        "1kb",
		1 << 10:  "1mb",
		10 << 10: "10mb",
	}
	for size, v := range tests {
		server := initServerWithSize(t, size)
		t.Run("stats-fmttext-"+v, func(t *testing.B) {
			for i := 0; i < t.N; i++ {
				req := &http.Request{}
				req.Header = make(http.Header)
				req.Header.Add("Accept", string(FmtText))
				rec := httptest.NewRecorder()
				server.handleStats(rec, req)
			}
		})
		t.Run("stats-fmtopenmetrics-"+v, func(t *testing.B) {
			for i := 0; i < t.N; i++ {
				req := &http.Request{}
				req.Header = make(http.Header)
				req.Header.Add("Accept", string(FmtOpenMetrics_1_0_0))
				rec := httptest.NewRecorder()
				server.handleStats(rec, req)
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
	go http.Serve(listener, &handler{lastAlpn: atomic.NewString("")})
	appPort := listener.Addr().(*net.TCPAddr).Port

	simpleHTTPConfig := KubeAppProbers{
		"/app-health/hello-world/readyz": &Prober{
			HTTPGet: &apimirror.HTTPGetAction{
				Path: "/hello/sunnyvale",
				Port: apimirror.IntOrString{IntVal: int32(appPort)},
			},
		},
		"/app-health/hello-world/livez": &Prober{
			HTTPGet: &apimirror.HTTPGetAction{
				Port: apimirror.IntOrString{IntVal: int32(appPort)},
			},
		},
	}
	simpleTCPConfig := KubeAppProbers{
		"/app-health/hello-world/readyz": &Prober{
			TCPSocket: &apimirror.TCPSocketAction{
				Port: apimirror.IntOrString{IntVal: int32(appPort)},
			},
		},
		"/app-health/hello-world/livez": &Prober{
			TCPSocket: &apimirror.TCPSocketAction{
				Port: apimirror.IntOrString{IntVal: int32(appPort)},
			},
		},
	}

	type test struct {
		name       string
		probePath  string
		config     KubeAppProbers
		podIP      string
		ipv6       bool
		statusCode int
	}
	testCases := []test{
		{
			name:       "http-bad-path",
			probePath:  "bad-path-should-be-404",
			config:     simpleHTTPConfig,
			statusCode: http.StatusNotFound,
		},
		{
			name:       "http-readyz",
			probePath:  "app-health/hello-world/readyz",
			config:     simpleHTTPConfig,
			statusCode: http.StatusOK,
		},
		{
			name:       "http-livez",
			probePath:  "app-health/hello-world/livez",
			config:     simpleHTTPConfig,
			statusCode: http.StatusOK,
		},
		{
			name:       "http-livez-localhost",
			probePath:  "app-health/hello-world/livez",
			config:     simpleHTTPConfig,
			statusCode: http.StatusOK,
			podIP:      "localhost",
		},
		{
			name:      "http-readyz-header",
			probePath: "app-health/header/readyz",
			config: KubeAppProbers{
				"/app-health/header/readyz": &Prober{
					HTTPGet: &apimirror.HTTPGetAction{
						Port: apimirror.IntOrString{IntVal: int32(appPort)},
						Path: "/header",
						HTTPHeaders: []apimirror.HTTPHeader{
							{Name: testHeader, Value: testHeaderValue},
							{Name: "Host", Value: testHostValue},
						},
					},
				},
			},
			statusCode: http.StatusOK,
		},
		{
			name:      "http-readyz-path",
			probePath: "app-health/hello-world/readyz",
			config: KubeAppProbers{
				"/app-health/hello-world/readyz": &Prober{
					HTTPGet: &apimirror.HTTPGetAction{
						Path: "hello/texas",
						Port: apimirror.IntOrString{IntVal: int32(appPort)},
					},
				},
			},
			statusCode: http.StatusOK,
		},
		{
			name:      "http-prestopz-path",
			probePath: "app-lifecycle/hello-world/prestopz",
			config: KubeAppProbers{
				"/app-lifecycle/hello-world/prestopz": &Prober{
					HTTPGet: &apimirror.HTTPGetAction{
						Path: "hello/texas",
						Port: apimirror.IntOrString{IntVal: int32(appPort)},
					},
				},
			},
			statusCode: http.StatusOK,
		},
		{
			name:      "http-livez-path",
			probePath: "app-health/hello-world/livez",
			config: KubeAppProbers{
				"/app-health/hello-world/livez": &Prober{
					HTTPGet: &apimirror.HTTPGetAction{
						Path: "hello/texas",
						Port: apimirror.IntOrString{IntVal: int32(appPort)},
					},
				},
			},
			statusCode: http.StatusOK,
		},
		{
			name:       "tcp-readyz",
			probePath:  "app-health/hello-world/readyz",
			config:     simpleTCPConfig,
			statusCode: http.StatusOK,
		},
		{
			name:       "tcp-livez",
			probePath:  "app-health/hello-world/livez",
			config:     simpleTCPConfig,
			statusCode: http.StatusOK,
		},
		{
			name:       "tcp-livez-ipv4",
			probePath:  "app-health/hello-world/livez",
			config:     simpleTCPConfig,
			statusCode: http.StatusOK,
			podIP:      "127.0.0.1",
		},
		{
			name:       "tcp-livez-ipv6",
			probePath:  "app-health/hello-world/livez",
			config:     simpleTCPConfig,
			statusCode: http.StatusOK,
			podIP:      "::1",
			ipv6:       true,
		},
		{
			name:       "tcp-livez-wrapped-ipv6",
			probePath:  "app-health/hello-world/livez",
			config:     simpleTCPConfig,
			statusCode: http.StatusInternalServerError,
			podIP:      "[::1]",
			ipv6:       true,
		},
		{
			name:       "tcp-livez-localhost",
			probePath:  "app-health/hello-world/livez",
			config:     simpleTCPConfig,
			statusCode: http.StatusOK,
			podIP:      "localhost",
		},
		{
			name:      "redirect",
			probePath: "app-health/redirect/livez",
			config: KubeAppProbers{
				"/app-health/redirect/livez": &Prober{
					HTTPGet: &apimirror.HTTPGetAction{
						Path: "redirect",
						Port: apimirror.IntOrString{IntVal: int32(appPort)},
					},
				},
			},
			statusCode: http.StatusOK,
		},
		{
			name:      "redirect loop",
			probePath: "app-health/redirect-loop/livez",
			config: KubeAppProbers{
				"/app-health/redirect-loop/livez": &Prober{
					HTTPGet: &apimirror.HTTPGetAction{
						Path: "redirect-loop",
						Port: apimirror.IntOrString{IntVal: int32(appPort)},
					},
				},
			},
			statusCode: http.StatusInternalServerError,
		},
		{
			name:      "remote redirect",
			probePath: "app-health/remote-redirect/livez",
			config: KubeAppProbers{
				"/app-health/remote-redirect/livez": &Prober{
					HTTPGet: &apimirror.HTTPGetAction{
						Path: "remote-redirect",
						Port: apimirror.IntOrString{IntVal: int32(appPort)},
					},
				},
			},
			statusCode: http.StatusOK,
		},
	}
	testFn := func(t *testing.T, tc test) {
		appProber, err := json.Marshal(tc.config)
		if err != nil {
			t.Fatal("invalid app probers")
		}
		config := Options{
			KubeAppProbers: string(appProber),
			PodIP:          tc.podIP,
			IPv6:           tc.ipv6,
		}
		server := NewTestServer(t, config)
		// Starts the pilot agent status server.
		if tc.ipv6 {
			server.upstreamLocalAddress = &net.TCPAddr{IP: net.ParseIP("::1")} // required because ::6 is NOT a loopback address (IPv6 only has ::1)
		}

		client := http.Client{}
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%v/%s", server.statusPort, tc.probePath), nil)
		if err != nil {
			t.Fatalf("[%v] failed to create request", tc.probePath)
		}
		if c := tc.config["/"+tc.probePath]; c != nil {
			if hc := c.HTTPGet; hc != nil {
				for _, h := range hc.HTTPHeaders {
					req.Header[h.Name] = append(req.Header[h.Name], h.Value)
				}
			}
		}
		// This is simulating the kubelet behavior of setting the Host to Header["Host"].
		// https://github.com/kubernetes/kubernetes/blob/d3b7391dc2f1040083ee2a8bfcb02edf7b0ded4b/pkg/probe/http/request.go#L84C1-L84C1
		req.Host = req.Header.Get("Host")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("request failed: ", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != tc.statusCode {
			t.Errorf("[%v] unexpected status code, want = %v, got = %v", tc.probePath, tc.statusCode, resp.StatusCode)
		}
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) { testFn(t, tc) })
	}
	// Next we check ever
	t.Run("status codes", func(t *testing.T) {
		for code := http.StatusOK; code <= http.StatusNetworkAuthenticationRequired; code++ {
			if http.StatusText(code) == "" { // Not a valid HTTP code
				continue
			}
			expect := code
			if isRedirect(code) {
				expect = 200
			}
			t.Run(fmt.Sprint(code), func(t *testing.T) {
				testFn(t, test{
					probePath: "app-health/code/livez",
					config: KubeAppProbers{
						"/app-health/code/livez": &Prober{
							TimeoutSeconds: 1,
							HTTPGet: &apimirror.HTTPGetAction{
								Path: fmt.Sprintf("status/%d", code),
								Port: apimirror.IntOrString{IntVal: int32(appPort)},
							},
						},
					},
					statusCode: expect,
				})
			})
		}
	})
}

func TestHttpsAppProbe(t *testing.T) {
	setupServer := func(t *testing.T, alpn []string) (uint16, func() string) {
		// Starts the application first.
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Errorf("failed to allocate unused port %v", err)
		}
		t.Cleanup(func() { listener.Close() })
		keyFile := env.IstioSrc + "/pilot/cmd/pilot-agent/status/test-cert/cert.key"
		crtFile := env.IstioSrc + "/pilot/cmd/pilot-agent/status/test-cert/cert.crt"
		cert, err := tls.LoadX509KeyPair(crtFile, keyFile)
		if err != nil {
			t.Fatalf("could not load TLS keys: %v", err)
		}
		serverTLSConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   alpn,
			MinVersion:   tls.VersionTLS12,
		}
		tlsListener := tls.NewListener(listener, serverTLSConfig)
		h := &handler{lastAlpn: atomic.NewString("")}
		srv := http.Server{Handler: h}
		go srv.Serve(tlsListener)
		appPort := listener.Addr().(*net.TCPAddr).Port

		// Starts the pilot agent status server.
		server := NewTestServer(t, Options{
			KubeAppProbers: fmt.Sprintf(`{"/app-health/hello-world/readyz": {"httpGet": {"path": "/hello/sunnyvale", "port": %v, "scheme": "HTTPS"}},
"/app-health/hello-world/livez": {"httpGet": {"port": %v, "scheme": "HTTPS"}}}`, appPort, appPort),
		})
		return server.statusPort, h.lastAlpn.Load
	}
	testCases := []struct {
		name             string
		probePath        string
		expectedProtocol string
		statusCode       int
		alpns            []string
	}{
		{
			name:       "bad-path-should-be-disallowed",
			probePath:  "bad-path-should-be-disallowed",
			statusCode: http.StatusNotFound,
		},
		{
			name:             "readyz",
			probePath:        "app-health/hello-world/readyz",
			statusCode:       http.StatusOK,
			expectedProtocol: "HTTP/1.1",
			alpns:            nil,
		},
		{
			name:             "livez",
			probePath:        "app-health/hello-world/livez",
			statusCode:       http.StatusOK,
			expectedProtocol: "HTTP/1.1",
		},
		{
			name:             "h1 only",
			probePath:        "app-health/hello-world/readyz",
			statusCode:       http.StatusOK,
			expectedProtocol: "HTTP/1.1",
			alpns:            []string{"http/1.1"},
		},
		{
			name:             "h2 only",
			probePath:        "app-health/hello-world/readyz",
			statusCode:       http.StatusOK,
			expectedProtocol: "HTTP/2.0",
			alpns:            []string{"h2"},
		},
		{
			name:             "prefer h2",
			probePath:        "app-health/hello-world/readyz",
			statusCode:       http.StatusOK,
			expectedProtocol: "HTTP/2.0",
			alpns:            []string{"h2", "http/1.1"},
		},
		{
			name:             "prefer h1",
			probePath:        "app-health/hello-world/readyz",
			statusCode:       http.StatusOK,
			expectedProtocol: "HTTP/2.0",
			alpns:            []string{"h2", "http/1.1"},
		},
		{
			name:       "unknown alpn",
			probePath:  "app-health/hello-world/readyz",
			statusCode: http.StatusInternalServerError,
			alpns:      []string{"foo"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			statusPort, getAlpn := setupServer(t, tc.alpns)
			client := http.Client{}
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d/%s", statusPort, tc.probePath), nil)
			if err != nil {
				t.Fatal("failed to create request")
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal("request failed")
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.statusCode {
				t.Errorf("unexpected status code, want = %v, got = %v", tc.statusCode, resp.StatusCode)
			}
			if got := getAlpn(); got != tc.expectedProtocol {
				t.Errorf("unexpected protocol, want = %v, got = %v", tc.expectedProtocol, got)
			}
		})
	}
}

func TestGRPCAppProbe(t *testing.T) {
	appServer := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("serving-svc", grpcHealth.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("unknown-svc", grpcHealth.HealthCheckResponse_UNKNOWN)
	healthServer.SetServingStatus("not-serving-svc", grpcHealth.HealthCheckResponse_NOT_SERVING)
	grpcHealth.RegisterHealthServer(appServer, healthServer)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Errorf("failed to allocate unused port %v", err)
	}
	go appServer.Serve(listener)
	defer appServer.GracefulStop()

	appPort := listener.Addr().(*net.TCPAddr).Port
	// Starts the pilot agent status server.
	server := NewTestServer(t, Options{
		KubeAppProbers: fmt.Sprintf(`
{
    "/app-health/foo/livez": {
        "grpc": {
            "port": %v, 
            "service": null
        }, 
        "timeoutSeconds": 1
    }, 
    "/app-health/foo/readyz": {
        "grpc": {
            "port": %v, 
            "service": "not-serving-svc"
        }, 
        "timeoutSeconds": 1
    }, 
    "/app-health/bar/livez": {
        "grpc": {
            "port": %v, 
            "service": "serving-svc"
        }, 
        "timeoutSeconds": 10
    }, 
    "/app-health/bar/readyz": {
        "grpc": {
            "port": %v, 
            "service": "unknown-svc"
        }, 
        "timeoutSeconds": 10
    }
}`, appPort, appPort, appPort, appPort),
	})
	statusPort := server.statusPort
	t.Logf("status server starts at port %v, app starts at port %v", statusPort, appPort)

	testCases := []struct {
		name       string
		probePath  string
		statusCode int
	}{
		{
			name:       "bad-path-should-be-disallowed",
			probePath:  fmt.Sprintf(":%v/bad-path-should-be-disallowed", statusPort),
			statusCode: http.StatusNotFound,
		},
		{
			name:       "foo-livez",
			probePath:  fmt.Sprintf(":%v/app-health/foo/livez", statusPort),
			statusCode: http.StatusOK,
		},
		{
			name:       "foo-readyz",
			probePath:  fmt.Sprintf(":%v/app-health/foo/readyz", statusPort),
			statusCode: http.StatusInternalServerError,
		},
		{
			name:       "bar-livez",
			probePath:  fmt.Sprintf(":%v/app-health/bar/livez", statusPort),
			statusCode: http.StatusOK,
		},
		{
			name:       "bar-readyz",
			probePath:  fmt.Sprintf(":%v/app-health/bar/readyz", statusPort),
			statusCode: http.StatusInternalServerError,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := http.Client{}
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost%s", tc.probePath), nil)
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
		})
	}
}

func TestGRPCAppProbeWithIPV6(t *testing.T) {
	appServer := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("serving-svc", grpcHealth.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("unknown-svc", grpcHealth.HealthCheckResponse_UNKNOWN)
	healthServer.SetServingStatus("not-serving-svc", grpcHealth.HealthCheckResponse_NOT_SERVING)
	grpcHealth.RegisterHealthServer(appServer, healthServer)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Errorf("failed to allocate unused port %v", err)
	}
	go appServer.Serve(listener)
	defer appServer.GracefulStop()

	appPort := listener.Addr().(*net.TCPAddr).Port
	// Starts the pilot agent status server.
	server := NewTestServer(t, Options{
		IPv6:  true,
		PodIP: "::1",
		KubeAppProbers: fmt.Sprintf(`
{
    "/app-health/foo/livez": {
        "grpc": {
            "port": %v, 
            "service": null
        }, 
        "timeoutSeconds": 1
    }, 
    "/app-health/foo/readyz": {
        "grpc": {
            "port": %v, 
            "service": "not-serving-svc"
        }, 
        "timeoutSeconds": 1
    }, 
    "/app-health/bar/livez": {
        "grpc": {
            "port": %v, 
            "service": "serving-svc"
        }, 
        "timeoutSeconds": 10
    }, 
    "/app-health/bar/readyz": {
        "grpc": {
            "port": %v, 
            "service": "unknown-svc"
        }, 
        "timeoutSeconds": 10
    }
}`, appPort, appPort, appPort, appPort),
	})

	server.upstreamLocalAddress = &net.TCPAddr{IP: net.ParseIP("::1")} // required because ::6 is NOT a loopback address (IPv6 only has ::1)

	testCases := []struct {
		name       string
		probePath  string
		statusCode int
	}{
		{
			name:       "bad-path-should-be-disallowed",
			probePath:  fmt.Sprintf(":%v/bad-path-should-be-disallowed", server.statusPort),
			statusCode: http.StatusNotFound,
		},
		{
			name:       "foo-livez",
			probePath:  fmt.Sprintf(":%v/app-health/foo/livez", server.statusPort),
			statusCode: http.StatusOK,
		},
		{
			name:       "foo-readyz",
			probePath:  fmt.Sprintf(":%v/app-health/foo/readyz", server.statusPort),
			statusCode: http.StatusInternalServerError,
		},
		{
			name:       "bar-livez",
			probePath:  fmt.Sprintf(":%v/app-health/bar/livez", server.statusPort),
			statusCode: http.StatusOK,
		},
		{
			name:       "bar-readyz",
			probePath:  fmt.Sprintf(":%v/app-health/bar/readyz", server.statusPort),
			statusCode: http.StatusInternalServerError,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := http.Client{}
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost%s", tc.probePath), nil)
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
		})
	}
}

func TestProbeHeader(t *testing.T) {
	headerChecker := func(t *testing.T, header http.Header) net.Listener {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatalf("failed to allocate unused port %v", err)
		}
		go http.Serve(listener, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			r.Header.Del("User-Agent")
			r.Header.Del("Accept-Encoding")
			if !reflect.DeepEqual(r.Header, header) {
				t.Errorf("unexpected header, want = %v, got = %v", header, r.Header)
				http.Error(rw, "", http.StatusBadRequest)
				return
			}
			http.Error(rw, "", http.StatusOK)
		}))
		return listener
	}

	testCases := []struct {
		name          string
		originHeaders http.Header
		proxyHeaders  []apimirror.HTTPHeader
		want          http.Header
	}{
		{
			name: "Only Origin",
			originHeaders: http.Header{
				testHeader: []string{testHeaderValue},
			},
			proxyHeaders: []apimirror.HTTPHeader{},
			want: http.Header{
				testHeader:   []string{testHeaderValue},
				"Connection": []string{"close"},
			},
		},
		{
			name: "Only Origin, has multiple values",
			originHeaders: http.Header{
				testHeader: []string{testHeaderValue, testHeaderValue},
			},
			proxyHeaders: []apimirror.HTTPHeader{},
			want: http.Header{
				testHeader:   []string{testHeaderValue, testHeaderValue},
				"Connection": []string{"close"},
			},
		},
		{
			name:          "Only Proxy",
			originHeaders: http.Header{},
			proxyHeaders: []apimirror.HTTPHeader{
				{
					Name:  testHeader,
					Value: testHeaderValue,
				},
			},
			want: http.Header{
				"Connection": []string{"close"},
			},
		},
		{
			name:          "Only Proxy, has multiple values",
			originHeaders: http.Header{},
			proxyHeaders: []apimirror.HTTPHeader{
				{
					Name:  testHeader,
					Value: testHeaderValue,
				},
				{
					Name:  testHeader,
					Value: testHeaderValue,
				},
			},
			want: http.Header{
				"Connection": []string{"close"},
			},
		},
		{
			name: "Proxy overwrites Origin",
			originHeaders: http.Header{
				testHeader: []string{testHeaderValue},
			},
			proxyHeaders: []apimirror.HTTPHeader{
				{
					Name:  testHeader,
					Value: testHeaderValue + "over",
				},
			},
			want: http.Header{
				testHeader:   []string{testHeaderValue},
				"Connection": []string{"close"},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := headerChecker(t, tc.want)
			defer svc.Close()
			probePath := "/app-health/hello-world/livez"
			appAddress := svc.Addr().(*net.TCPAddr)
			appProber, err := json.Marshal(KubeAppProbers{
				probePath: &Prober{
					HTTPGet: &apimirror.HTTPGetAction{
						Port:        apimirror.IntOrString{IntVal: int32(appAddress.Port)},
						Host:        appAddress.IP.String(),
						Path:        "/header",
						HTTPHeaders: tc.proxyHeaders,
					},
				},
			})
			if err != nil {
				t.Fatal("invalid app probers")
			}
			config := Options{
				KubeAppProbers: string(appProber),
			}
			// Starts the pilot agent status server.
			server := NewTestServer(t, config)
			client := http.Client{}
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%v%s", server.statusPort, probePath), nil)
			if err != nil {
				t.Fatal("failed to create request: ", err)
			}
			req.Header = tc.originHeaders
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal("request failed: ", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("unexpected status code, want = %v, got = %v", http.StatusOK, resp.StatusCode)
			}
		})
	}
}

func TestHandleQuit(t *testing.T) {
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
			shutdown := make(chan struct{})
			s := NewTestServer(t, Options{
				Shutdown: func(err error) {
					close(shutdown)
				},
			})
			req, err := http.NewRequest(tt.method, "/quitquitquit", nil)
			if err != nil {
				t.Fatal(err)
			}

			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr + ":" + fmt.Sprint(s.statusPort)
			}

			resp := httptest.NewRecorder()
			s.handleQuit(resp, req)
			if resp.Code != tt.expected {
				t.Fatalf("Expected response code %v got %v", tt.expected, resp.Code)
			}

			if tt.expected == http.StatusOK {
				select {
				case <-shutdown:
				case <-time.After(time.Second):
					t.Fatal("Failed to receive expected shutdown")
				}
			} else {
				select {
				case <-shutdown:
					t.Fatal("unexpected shutdown")
				default:
				}
			}
		})
	}
}

func TestAdditionalProbes(t *testing.T) {
	rp := readyProbe{}
	urp := unreadyProbe{}
	testCases := []struct {
		name   string
		probes []ready.Prober
		err    error
	}{
		{
			name:   "success probe",
			probes: []ready.Prober{rp},
			err:    nil,
		},
		{
			name:   "not ready probe",
			probes: []ready.Prober{urp},
			err:    errors.New("not ready"),
		},
		{
			name:   "both probes",
			probes: []ready.Prober{rp, urp},
			err:    errors.New("not ready"),
		},
	}
	testServer := testserver.CreateAndStartServer(liveServerStats)
	defer testServer.Close()
	for _, tc := range testCases {
		server, err := NewServer(Options{
			Probes:    tc.probes,
			AdminPort: uint16(testServer.Listener.Addr().(*net.TCPAddr).Port),
		})
		if err != nil {
			t.Errorf("failed to construct server")
		}
		err = server.isReady()
		if tc.err == nil {
			if err != nil {
				t.Errorf("Unexpected result, expected: %v got: %v", tc.err, err)
			}
		} else {
			if err.Error() != tc.err.Error() {
				t.Errorf("Unexpected result, expected: %v got: %v", tc.err, err)
			}
		}

	}
}

type readyProbe struct{}

func (s readyProbe) Check() error {
	return nil
}

type unreadyProbe struct{}

func (u unreadyProbe) Check() error {
	return errors.New("not ready")
}

var reg = lazy.New(initializeMonitoring)

func TestingRegistry(t test.Failer) prometheus.Gatherer {
	r, err := reg.Get()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
