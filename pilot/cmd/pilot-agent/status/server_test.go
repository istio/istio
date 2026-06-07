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
	"github.com/prometheus/common/model"
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
				`"/app-health/business/livez": {"httpGet": {"path": "/business/live", "port": 9090}}}`,
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

			parser := expfmt.NewTextParser(model.LegacyValidation)
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

func generateEnvoyMetrics(size int) string {
	envoy := strings.Builder{}
	envoy.Grow(size << 10 * 100)
	envoy.WriteString(`# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`)
	for i := 0; envoy.Len() < size<<10; i++ {
		envoy.WriteString("#TYPE my_other_metric_" + strconv.Itoa(i) + " counter\nmy_other_metric_" + strconv.Itoa(i) + " 0\n")
	}

	return envoy.String()
}

func TestHTTPCompressionOnStats(t *testing.T) {
	cases := []struct {
		name                  string
		acceptEncodingHeader  string
		expectContentEncoding string
	}{
		{
			name:                  "no accept encoding header",
			acceptEncodingHeader:  "",
			expectContentEncoding: "",
		},
		{
			name:                  "gzip",
			acceptEncodingHeader:  "gzip",
			expectContentEncoding: "gzip",
		},
		{
			name:                  "zstd",
			acceptEncodingHeader:  "zstd",
			expectContentEncoding: "zstd",
		},
		{
			name:                  "prefer zstd over gzip",
			acceptEncodingHeader:  "gzip, zstd",
			expectContentEncoding: "zstd",
		},
		{
			name:                  "client likes gzip more than zstd",
			acceptEncodingHeader:  "deflate, gzip;q=1.0, zstd;q=0.5",
			expectContentEncoding: "gzip",
		},
		{
			name:                  "ignore unsupported encodings",
			acceptEncodingHeader:  "br, lz4",
			expectContentEncoding: "",
		},
	}

	// Start an Envoy server to provide a big chunk of stats
	envoyMetricBytes := generateEnvoyMetrics(1 << 11) // 10 MiB
	envoyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte(envoyMetricBytes)); err != nil {
			t.Errorf("write failed: %v", err)
		}
	}))
	t.Cleanup(envoyServer.Close)
	envoyPort, err := strconv.Atoi(strings.Split(envoyServer.URL, ":")[2])
	if err != nil {
		t.Error(err)
	}

	// Starts the pilot agent status server.
	server := NewTestServer(t, Options{EnvoyPrometheusPort: envoyPort})

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client := http.Client{}
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%v/stats/prometheus", server.statusPort), nil)
			if err != nil {
				t.Fatalf("[%v] failed to create request", err)
			}

			req.Header = make(http.Header)
			req.Header.Set("Accept", "text/plain")
			if tt.acceptEncodingHeader != "" {
				req.Header.Add("Accept-Encoding", tt.acceptEncodingHeader)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal("request failed: ", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("[%v] unexpected status code, want = %v, got = %v", "/stats/prometheus", http.StatusOK, resp.StatusCode)
			}
			if resp.Header.Get("Content-Encoding") != tt.expectContentEncoding {
				t.Errorf("[%v] unexpected content encoding, want = %v, got = %v", "/stats/prometheus", tt.expectContentEncoding, resp.Header.Get("Content-Encoding"))
			}
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
				textParser := expfmt.NewTextParser(model.LegacyValidation)
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

// targetSpec parameterizes a single httptest.Server backing one ScrapeTarget in the
// multi-target Server tests. Defined at file scope so the helper newMultiTargetTestServer
// can take a []targetSpec.
type targetSpec struct {
	body        string
	contentType string // empty → default text/plain from httptest
	fail        bool   // when true, handler returns 500
}

// wantStats groups the expected outcomes for a TestStatsMultiTarget case, separating
// "what the request looked like" (envoy, targets) from "what the response should be."
type wantStats struct {
	ordered, contains, notContains []string
	eofCount                       *int // nil = don't check count
	parseOM, parseText, parseError bool
	appScrapeErrorsDelta           float64
	contentType                    expfmt.Format // empty = don't check
}

// intPtr is a one-line helper for building *int literals inside wantStats.
func intPtr(i int) *int { return &i }

// newMultiTargetTestServer spins up an envoy httptest.Server backed by envoyBody and one
// httptest.Server per targetSpec, then wires a *Server to scrape them. All servers are
// closed via t.Cleanup. The returned Server has maxAppBodyBytes defaulted; tests needing
// a tractable cap should set the field after the call.
//
// Note: this helper deliberately uses the process-global Prometheus registry via
// TestingRegistry(t). Tests calling handleStats from this Server therefore MUST NOT run
// under t.Parallel() — the registry is shared and AppScrapeErrors deltas would race.
func newMultiTargetTestServer(t *testing.T, envoyBody string, specs []targetSpec) (*Server, *httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	envoyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(envoyBody)); err != nil {
			t.Fatalf("envoy write failed: %v", err)
		}
	}))
	t.Cleanup(envoyServer.Close)

	targets := make([]ScrapeTarget, 0, len(specs))
	for _, spec := range specs {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if spec.fail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if spec.contentType != "" {
				w.Header().Set("Content-Type", spec.contentType)
			}
			if _, err := w.Write([]byte(spec.body)); err != nil {
				t.Fatalf("target write failed: %v", err)
			}
		}))
		t.Cleanup(srv.Close)
		_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatalf("split host port: %v", err)
		}
		targets = append(targets, ScrapeTarget{Port: port, Path: "/metrics"})
	}

	_, envoyPortStr, err := net.SplitHostPort(strings.TrimPrefix(envoyServer.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	envoyPort, err := strconv.Atoi(envoyPortStr)
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		prometheus: &PrometheusScrapeConfiguration{
			Port:    targets[0].Port,
			Path:    targets[0].Path,
			Targets: targets,
		},
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		registry:        TestingRegistry(t),
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}
	return server, httptest.NewRecorder(), &http.Request{}
}

// TestStatsMultiTarget exercises the concurrent fan-out path in handleStats when
// s.prometheus.Targets contains more than one entry. The single-target path is covered
// by TestStats above and must remain byte-for-byte unchanged.
//
// This test reads and asserts on a process-global Prometheus counter via the want.appScrapeErrorsDelta
// field; do not add t.Parallel() to any subtest without first moving the counter off the
// package-level global (see appScrapeErrorCount).
func TestStatsMultiTarget(t *testing.T) {
	cases := []struct {
		name    string
		envoy   string
		targets []targetSpec
		want    wantStats
	}{
		{
			name: "two text targets with disjoint metrics merge in target order",
			targets: []targetSpec{
				{body: "# TYPE metric_a counter\nmetric_a{} 1\n"},
				{body: "# TYPE metric_b counter\nmetric_b{} 2\n"},
			},
			want: wantStats{
				ordered:     []string{"metric_a{} 1", "metric_b{} 2"},
				eofCount:    intPtr(0),
				parseText:   true,
				contentType: FmtText,
			},
		},
		{
			name: "three text targets preserve Targets-slice order even when goroutines race",
			targets: []targetSpec{
				{body: "# TYPE metric_zero counter\nmetric_zero{} 0\n"},
				{body: "# TYPE metric_one counter\nmetric_one{} 1\n"},
				{body: "# TYPE metric_two counter\nmetric_two{} 2\n"},
			},
			want: wantStats{
				ordered:     []string{"metric_zero", "metric_one", "metric_two"},
				eofCount:    intPtr(0),
				parseText:   true,
				contentType: FmtText,
			},
		},
		{
			name: "two OpenMetrics targets emit exactly one # EOF at the end",
			targets: []targetSpec{
				{
					body:        "# TYPE metric_a counter\n# HELP metric_a desc\nmetric_a_total 1\n# EOF\n",
					contentType: string(FmtOpenMetrics_1_0_0),
				},
				{
					body:        "# TYPE metric_b counter\n# HELP metric_b desc\nmetric_b_total 2\n# EOF\n",
					contentType: string(FmtOpenMetrics_1_0_0),
				},
			},
			want: wantStats{
				ordered:     []string{"metric_a", "metric_b"},
				contains:    []string{"# EOF"},
				eofCount:    intPtr(1),
				parseOM:     true,
				contentType: FmtOpenMetrics_1_0_0,
			},
		},
		{
			name: "one failing target does not prevent successful targets from appearing",
			targets: []targetSpec{
				{body: "# TYPE metric_ok counter\nmetric_ok{} 7\n"},
				{fail: true},
			},
			want: wantStats{
				contains:             []string{"metric_ok{} 7"},
				eofCount:             intPtr(0),
				parseText:            true,
				appScrapeErrorsDelta: 1,
				contentType:          FmtText,
			},
		},
		{
			name: "all failing targets still return 200 with envoy + agent metrics",
			envoy: `# TYPE envoy_metric counter
envoy_metric{} 9
`,
			targets: []targetSpec{
				{fail: true},
				{fail: true},
			},
			want: wantStats{
				contains:             []string{"envoy_metric{} 9"},
				eofCount:             intPtr(0),
				parseText:            true,
				appScrapeErrorsDelta: 2,
				contentType:          FmtText,
			},
		},
		{
			name: "Targets[0] failure falls back to first successful target's format",
			targets: []targetSpec{
				{fail: true},
				{
					body:        "# TYPE metric_fallback counter\nmetric_fallback_total 3\n# EOF\n",
					contentType: string(FmtOpenMetrics_1_0_0),
				},
			},
			want: wantStats{
				contains:             []string{"metric_fallback_total 3", "# EOF"},
				eofCount:             intPtr(1),
				parseOM:              true,
				appScrapeErrorsDelta: 1,
				contentType:          FmtOpenMetrics_1_0_0,
			},
		},
		{
			name: "mixed formats — Targets[0] is text so response is text and OM target's EOF is stripped",
			targets: []targetSpec{
				{body: "# TYPE metric_text counter\nmetric_text{} 4\n"},
				{
					body:        "# TYPE metric_om counter\n# HELP metric_om desc\nmetric_om_total 5\n# EOF\n",
					contentType: string(FmtOpenMetrics_1_0_0),
				},
			},
			want: wantStats{
				contains:    []string{"metric_text{} 4", "metric_om_total 5"},
				notContains: []string{"# EOF"},
				eofCount:    intPtr(0),
				parseText:   true,
				contentType: FmtText,
			},
		},
		{
			// D3 — the OM-first counterpart of the case above. Without the format-mismatch
			// downgrade, this response would claim OpenMetrics but contain a text body
			// without "# EOF", producing invalid OpenMetrics. Fix B coerces to text.
			name: "mixed formats — Targets[0] is OpenMetrics and Targets[1] is text; response downgrades to text and emits no # EOF",
			targets: []targetSpec{
				{
					body:        "# TYPE metric_om counter\n# HELP metric_om desc\nmetric_om_total 1\n# EOF\n",
					contentType: string(FmtOpenMetrics_1_0_0),
				},
				{
					body:        "# TYPE metric_text counter\nmetric_text{} 2\n",
					contentType: string(FmtText),
				},
			},
			want: wantStats{
				contains:    []string{"metric_om_total 1", "metric_text{} 2"},
				notContains: []string{"# EOF"},
				eofCount:    intPtr(0),
				parseText:   true,
				contentType: FmtText,
			},
		},
		{
			// D1 — two targets advertising the same metric family. The agent does not
			// dedupe across targets (out of scope per design); both blocks appear in the
			// merged body, and the consumer's parser fails on the duplicate # TYPE.
			name: "two targets emit the same metric family name; merged body contains both blocks (consumer parses-errors per design contract)",
			targets: []targetSpec{
				{body: "# TYPE metric_x counter\nmetric_x 1\n"},
				{body: "# TYPE metric_x counter\nmetric_x 2\n"},
			},
			want: wantStats{
				contains:    []string{"metric_x 1", "metric_x 2"},
				eofCount:    intPtr(0),
				parseText:   true,
				parseError:  true,
				contentType: FmtText,
			},
		},
		{
			// D-CRLF — verifies stripOpenMetricsEOF tolerates "\r\n# EOF\r\n" trailers as
			// documented. The merged OpenMetrics response should contain one terminator
			// at the very end (FinalizeOpenMetrics writes "# EOF\n", no CR in output).
			name: "OpenMetrics targets with CRLF EOF trailers — strip + reappend exactly once",
			targets: []targetSpec{
				{
					body:        "# TYPE metric_crlf_a counter\nmetric_crlf_a_total 1\n# EOF\r\n",
					contentType: string(FmtOpenMetrics_1_0_0),
				},
				{
					body:        "# TYPE metric_crlf_b counter\nmetric_crlf_b_total 2\n# EOF\r\n",
					contentType: string(FmtOpenMetrics_1_0_0),
				},
			},
			want: wantStats{
				ordered:     []string{"metric_crlf_a", "metric_crlf_b"},
				contains:    []string{"# EOF"},
				eofCount:    intPtr(1),
				parseOM:     true,
				contentType: FmtOpenMetrics_1_0_0,
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			server, rec, req := newMultiTargetTestServer(t, tt.envoy, tt.targets)

			beforeErrors := appScrapeErrorCount(t, server.registry)
			server.handleStats(rec, req)
			if rec.Code != 200 {
				t.Fatalf("handleStats() => %v; want 200", rec.Code)
			}
			if got := appScrapeErrorCount(t, server.registry) - beforeErrors; got != tt.want.appScrapeErrorsDelta {
				t.Errorf("AppScrapeErrors delta = %v, want %v", got, tt.want.appScrapeErrorsDelta)
			}

			body := rec.Body.String()

			for _, s := range tt.want.contains {
				if !strings.Contains(body, s) {
					t.Errorf("body missing %q; body:\n%s", s, body)
				}
			}
			for _, s := range tt.want.notContains {
				if strings.Contains(body, s) {
					t.Errorf("body must not contain %q; body:\n%s", s, body)
				}
			}
			// Assert Targets-order preservation via ascending substring indices.
			lastIdx := -1
			for _, s := range tt.want.ordered {
				idx := strings.Index(body, s)
				if idx < 0 {
					t.Errorf("body missing ordered substring %q; body:\n%s", s, body)
					continue
				}
				if idx < lastIdx {
					t.Errorf("substring %q at index %d appeared before earlier target (last index %d); body:\n%s", s, idx, lastIdx, body)
				}
				lastIdx = idx
			}
			if tt.want.eofCount != nil {
				if got := strings.Count(body, "# EOF"); got != *tt.want.eofCount {
					t.Errorf("# EOF count = %d, want %d; body:\n%s", got, *tt.want.eofCount, body)
				}
				// When an OpenMetrics response carries exactly one EOF, the terminator
				// must land at the very end of the response (design contract). This
				// catches regressions that emit "# EOF" mid-response while still
				// satisfying the count.
				if *tt.want.eofCount == 1 && tt.want.parseOM && !strings.HasSuffix(body, "# EOF\n") {
					tail := body
					if len(body) > 32 {
						tail = body[len(body)-32:]
					}
					t.Errorf("OpenMetrics response must end with %q; body tail: %q", "# EOF\n", tail)
				}
			}
			if tt.want.contentType != "" {
				if got := rec.Header().Get("Content-Type"); got != string(tt.want.contentType) {
					t.Errorf("Content-Type = %q, want %q", got, string(tt.want.contentType))
				}
			}
			if tt.want.parseText {
				parser := expfmt.NewTextParser(model.LegacyValidation)
				_, err := parser.TextToMetricFamilies(strings.NewReader(body))
				switch {
				case err != nil && !tt.want.parseError:
					t.Fatalf("text parse failed: %v; body:\n%s", err, body)
				case err == nil && tt.want.parseError:
					t.Fatalf("expected text parse error, got none; body:\n%s", body)
				}
			}
			if tt.want.parseOM {
				omp := textparse.NewOpenMetricsParser(rec.Body.Bytes(), labels.NewSymbolTable())
				var parseErr error
				for {
					_, perr := omp.Next()
					if perr == io.EOF {
						break
					}
					if perr != nil {
						parseErr = perr
						break
					}
				}
				switch {
				case parseErr != nil && !tt.want.parseError:
					t.Fatalf("openmetrics parse failed: %v; body:\n%s", parseErr, body)
				case parseErr == nil && tt.want.parseError:
					t.Fatalf("expected openmetrics parse error, got none; body:\n%s", body)
				}
			}
		})
	}
}

// TestScrapeMultipleAppsBodyCap verifies that scrapeMultipleApps drops target responses
// that exceed Server.maxAppBodyBytes. The cap is shrunk to a tractable size via the Server
// struct literal (no global mutation); one target returns exactly cap bytes (accepted,
// present in the merged response) and another returns cap+1 bytes (dropped, absent from
// the response, AppScrapeErrors incremented).
func TestScrapeMultipleAppsBodyCap(t *testing.T) {
	const bodyCap = 100
	atCap := strings.Repeat("a", bodyCap)
	overCap := strings.Repeat("b", bodyCap+1)

	server, rec, req := newMultiTargetTestServer(t, "", []targetSpec{
		{body: atCap},
		{body: overCap},
	})
	server.maxAppBodyBytes = bodyCap

	before := appScrapeErrorCount(t, server.registry)
	server.handleStats(rec, req)
	after := appScrapeErrorCount(t, server.registry)

	body := rec.Body.String()
	if !strings.Contains(body, atCap) {
		t.Errorf("at-cap body (%d bytes of 'a') should be present in merged response; len(body)=%d", bodyCap, len(body))
	}
	if strings.Contains(body, overCap) {
		t.Errorf("over-cap body (%d bytes of 'b') should NOT be present; len(body)=%d", bodyCap+1, len(body))
	}
	if got := after - before; got != 1 {
		t.Errorf("AppScrapeErrors delta = %v, want 1 (only the over-cap target should fail)", got)
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
			drainDisabled := false
			s := NewTestServer(t, Options{
				Shutdown: func(err error) {
					close(shutdown)
				},
				DisableDrain: func() {
					drainDisabled = true
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
				if !drainDisabled {
					t.Fatalf("Expected drain to be disabled")
				}
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

// appScrapeErrorCount reads istio_agent_scrape_failures_total{type="application"} from the
// test registry. The agent wraps the registry with the "istio_agent_" prefix at startup
// (server.go: WrapRegistererWithPrefix), so the bare "scrape_failures_total" name will not
// match. Tests should capture this value before and after handleStats, then assert on the
// delta — the registry is process-global and accumulates across tests in the package.
func appScrapeErrorCount(t test.Failer, g prometheus.Gatherer) float64 {
	families, err := g.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range families {
		if mf.GetName() != "istio_agent_scrape_failures_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "type" && lp.GetValue() == "application" {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestParseScrapeTargets(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    []ScrapeTarget
		wantErr bool
	}{
		{
			name:  "single target",
			input: "8080:/metrics",
			want:  []ScrapeTarget{{Port: "8080", Path: "/metrics"}},
		},
		{
			name:  "two targets",
			input: "8080:/metrics,9100:/custom",
			want:  []ScrapeTarget{{Port: "8080", Path: "/metrics"}, {Port: "9100", Path: "/custom"}},
		},
		{
			name:  "no path defaults to /metrics",
			input: "8080",
			want:  []ScrapeTarget{{Port: "8080", Path: "/metrics"}},
		},
		{
			name:  "empty path component defaults to /metrics",
			input: "8080:,9100:/custom",
			want:  []ScrapeTarget{{Port: "8080", Path: "/metrics"}, {Port: "9100", Path: "/custom"}},
		},
		{
			name:  "whitespace trimmed",
			input: " 8080:/metrics , 9100:/custom ",
			want:  []ScrapeTarget{{Port: "8080", Path: "/metrics"}, {Port: "9100", Path: "/custom"}},
		},
		{
			name:  "empty string returns nil",
			input: "",
			want:  nil,
		},
		{
			name:  "only commas returns nil",
			input: ",,,",
			want:  nil,
		},
		{
			name:    "empty port is an error",
			input:   ":/metrics",
			wantErr: true,
		},
		{
			name:    "mixed valid and invalid stops at first empty port",
			input:   "8080:/metrics,:/bad,9100:/custom",
			wantErr: true,
		},
		{
			name:  "duplicate ports accepted (dedup is not this layer's job)",
			input: "8080:/metrics,8080:/other",
			want:  []ScrapeTarget{{Port: "8080", Path: "/metrics"}, {Port: "8080", Path: "/other"}},
		},
		{
			name:  "path with query string preserved verbatim",
			input: "8080:/metrics?foo=bar",
			want:  []ScrapeTarget{{Port: "8080", Path: "/metrics?foo=bar"}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseScrapeTargets(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseScrapeTargets(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseScrapeTargets(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIstioReservedPortReason(t *testing.T) {
	reserved := []string{"15000", "15001", "15004", "15006", "15008", "15020", "15021", "15053", "15090"}
	for _, p := range reserved {
		t.Run("reserved/"+p, func(t *testing.T) {
			reason, ok := IstioReservedPortReason(p)
			if !ok {
				t.Errorf("IstioReservedPortReason(%q) = _, false; want reserved", p)
			}
			if reason == "" {
				t.Errorf("IstioReservedPortReason(%q) returned empty reason", p)
			}
		})
	}
	nonReserved := []string{"80", "8080", "9090", "15002", "15007", "15091", "15099", ""}
	for _, p := range nonReserved {
		t.Run("non-reserved/"+p, func(t *testing.T) {
			if _, ok := IstioReservedPortReason(p); ok {
				t.Errorf("IstioReservedPortReason(%q) returned reserved=true; want false", p)
			}
		})
	}
	// Ports are compared as opaque strings so annotation-layer values round-trip
	// verbatim; canonical integer equality (15020 == 015020) is intentionally NOT
	// applied here.
	t.Run("boundary/leading-zero not matched", func(t *testing.T) {
		if _, ok := IstioReservedPortReason("015020"); ok {
			t.Errorf("IstioReservedPortReason(%q) matched; want string-equality only", "015020")
		}
	})
}

func TestNewServerPrometheusNormalization(t *testing.T) {
	const statusPort = 15020
	cases := []struct {
		name        string
		envJSON     string
		wantTargets []ScrapeTarget
		wantPort    string
		wantErr     bool
	}{
		{
			name:        "legacy single-port JSON synthesizes one target",
			envJSON:     `{"scrape":"true","port":"8080","path":"/metrics"}`,
			wantPort:    "8080",
			wantTargets: []ScrapeTarget{{Port: "8080", Path: "/metrics"}},
		},
		{
			name:        "new-format JSON with targets field is preserved",
			envJSON:     `{"scrape":"true","port":"8080","path":"/metrics","targets":[{"port":"8080","path":"/metrics"},{"port":"9100","path":"/custom"}]}`,
			wantPort:    "8080",
			wantTargets: []ScrapeTarget{{Port: "8080", Path: "/metrics"}, {Port: "9100", Path: "/custom"}},
		},
		{
			name:    "target port equal to status port is an error",
			envJSON: `{"scrape":"true","targets":[{"port":"15020","path":"/metrics"}]}`,
			wantErr: true,
		},
		{
			name:        "empty port defaults to 80 and synthesizes one target",
			envJSON:     `{"scrape":"true"}`,
			wantPort:    "80",
			wantTargets: []ScrapeTarget{{Port: "80", Path: "/metrics"}},
		},
		{
			name:        "empty path in target defaults to /metrics",
			envJSON:     `{"scrape":"true","targets":[{"port":"8080","path":""}]}`,
			wantTargets: []ScrapeTarget{{Port: "8080", Path: "/metrics"}},
		},
		{
			name:    "target on reserved Envoy admin port is rejected",
			envJSON: `{"scrape":"true","targets":[{"port":"15000","path":"/metrics"}]}`,
			wantErr: true,
		},
		{
			name:    "target on reserved Envoy Prometheus port is rejected",
			envJSON: `{"scrape":"true","targets":[{"port":"15090","path":"/metrics"}]}`,
			wantErr: true,
		},
		{
			name:    "target on reserved inbound redirect port is rejected",
			envJSON: `{"scrape":"true","targets":[{"port":"15006","path":"/metrics"}]}`,
			wantErr: true,
		},
		{
			name:    "one good target plus one reserved target is rejected",
			envJSON: `{"scrape":"true","targets":[{"port":"8080","path":"/metrics"},{"port":"15000","path":"/metrics"}]}`,
			wantErr: true,
		},
		{
			name:        "both legacy Port and Targets present — Targets are preserved verbatim, legacy Port is not overwritten",
			envJSON:     `{"scrape":"true","port":"8080","path":"/metrics","targets":[{"port":"9100","path":"/custom"}]}`,
			wantPort:    "8080",
			wantTargets: []ScrapeTarget{{Port: "9100", Path: "/custom"}},
		},
		{
			name:        "new-format JSON with targets but no top-level port syncs Port from Targets[0]",
			envJSON:     `{"scrape":"true","targets":[{"port":"8080","path":"/metrics"}]}`,
			wantPort:    "8080",
			wantTargets: []ScrapeTarget{{Port: "8080", Path: "/metrics"}},
		},
		{
			name:    "leading-zero port equal to status port is rejected by numeric comparison",
			envJSON: `{"scrape":"true","targets":[{"port":"015020","path":"/metrics"}]}`,
			wantErr: true,
		},
		{
			name:    "leading-zero reserved port is rejected by numeric comparison",
			envJSON: `{"scrape":"true","targets":[{"port":"015000","path":"/metrics"}]}`,
			wantErr: true,
		},
		{
			name:    "non-numeric port is rejected",
			envJSON: `{"scrape":"true","targets":[{"port":"abc","path":"/metrics"}]}`,
			wantErr: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(PrometheusScrapingConfig.Name, tt.envJSON)
			s, err := NewServer(Options{
				NodeType:           "sidecar",
				StatusPort:         statusPort,
				PrometheusRegistry: TestingRegistry(t),
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewServer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if s.prometheus == nil {
				t.Fatal("expected s.prometheus to be set")
			}
			if tt.wantPort != "" && s.prometheus.Port != tt.wantPort {
				t.Errorf("Port = %q, want %q", s.prometheus.Port, tt.wantPort)
			}
			if !reflect.DeepEqual(s.prometheus.Targets, tt.wantTargets) {
				t.Errorf("Targets = %v, want %v", s.prometheus.Targets, tt.wantTargets)
			}
		})
	}
}
