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
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/test"
)

// acceptPrometheusNative is the Accept header Prometheus sends when native
// histograms are enabled: protobuf first, then OpenMetrics and text.
const acceptPrometheusNative = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;` +
	`encoding=delimited;q=0.6,application/openmetrics-text;version=1.0.0;q=0.5,text/plain;version=1.0.0;q=0.4,*/*;q=0.1`

// upconversionServer starts a protobuf Envoy upstream carrying a native histogram and a
// text application upstream, and returns a Server merging both.
func upconversionServer(t *testing.T, noEnvoy bool) *Server {
	t.Helper()
	envoyBody := encodeProtoFamilies(t,
		classicCounterFamily("envoy_classic_counter"),
		nativeHistogramFamily(),
	)
	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(envoyBody)
	}))
	t.Cleanup(envoySrv.Close)
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", FmtText)
		_, _ = io.WriteString(w, "# TYPE app_requests_total counter\napp_requests_total 3\n"+
			"# TYPE app_latency_seconds histogram\napp_latency_seconds_bucket{le=\"0.1\"} 1\n"+
			"app_latency_seconds_bucket{le=\"+Inf\"} 2\napp_latency_seconds_sum 0.3\napp_latency_seconds_count 2\n")
	}))
	t.Cleanup(appSrv.Close)

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		config: Options{NoEnvoy: noEnvoy},
		prometheus: &PrometheusScrapeConfiguration{
			Port: strings.Split(appSrv.URL, ":")[2],
		},
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}
}

func scrapeWithAccept(server *Server, accept string) *httptest.ResponseRecorder {
	req := &http.Request{Header: make(http.Header)}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)
	return rec
}

// TestHandleStats_UpconvertTextApp verifies that with upconversion enabled, a
// protobuf-preferring scrape of Envoy (protobuf) plus a text app is answered in
// protobuf: Envoy's native histogram survives unchanged instead of collapsing to
// +Inf/_sum/_count, and the app's text families are converted.
func TestHandleStats_UpconvertTextApp(t *testing.T) {
	test.SetForTest(t, &features.AgentMetricsProtobufUpconversion, true)
	rec := scrapeWithAccept(upconversionServer(t, false), acceptPrometheusNative)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if got := negotiateMetricsFormat(rec.Header().Get("Content-Type")); got != FmtProtoDelim {
		t.Fatalf("Content-Type negotiated %q; want protobuf", got)
	}
	mfs := decodeProtoFamilies(t, rec.Body.Bytes())
	want := nativeHistogramFamily()
	if got := findFamily(mfs, want.GetName()); !proto.Equal(got, want) {
		t.Errorf("native histogram not preserved:\n got  %v\n want %v", got, want)
	}
	if findFamily(mfs, "envoy_classic_counter") == nil {
		t.Errorf("envoy_classic_counter missing; got %v", familyNames(mfs))
	}
	if got := findFamily(mfs, "app_requests_total"); got.GetMetric()[0].GetCounter().GetValue() != 3 {
		t.Errorf("app_requests_total not converted: %v", got)
	}
	if got := findFamily(mfs, "app_latency_seconds"); len(got.GetMetric()[0].GetHistogram().GetBucket()) != 2 {
		t.Errorf("app_latency_seconds classic buckets not converted: %v", got)
	}
	if findFamily(mfs, "istio_agent_scrapes_total") == nil {
		t.Errorf("agent self-metrics missing; got %v", familyNames(mfs))
	}
}

// TestHandleStats_UpconvertNegotiation verifies that upconversion only changes the
// response for scrapers that prefer protobuf, and that it stays off by default.
func TestHandleStats_UpconvertNegotiation(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		accept  string
		want    expfmt.Format
	}{
		{"disabled downgrades to text", false, acceptPrometheusNative, FmtText},
		{"enabled, scraper prefers protobuf", true, acceptPrometheusNative, FmtProtoDelim},
		{"enabled, scraper prefers OpenMetrics", true, `application/openmetrics-text;version=1.0.0;q=0.9,` +
			`application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited;q=0.5`, FmtText},
		{"enabled, text-only scraper", true, "text/plain;version=0.0.4", FmtText},
		{"enabled, no Accept header", true, "", FmtText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			test.SetForTest(t, &features.AgentMetricsProtobufUpconversion, tc.enabled)
			rec := scrapeWithAccept(upconversionServer(t, false), tc.accept)
			if rec.Code != 200 {
				t.Fatalf("status = %d; want 200", rec.Code)
			}
			if got := negotiateMetricsFormat(rec.Header().Get("Content-Type")); got != tc.want {
				t.Fatalf("Content-Type negotiated %q; want %q", got, tc.want)
			}
		})
	}
}

// TestHandleStats_UpconvertWithoutEnvoy verifies the all-text case: with Envoy stats
// not merged, a protobuf-preferring scrape of a text app is still answered in protobuf.
func TestHandleStats_UpconvertWithoutEnvoy(t *testing.T) {
	test.SetForTest(t, &features.AgentMetricsProtobufUpconversion, true)
	rec := scrapeWithAccept(upconversionServer(t, true), acceptPrometheusNative)

	if got := negotiateMetricsFormat(rec.Header().Get("Content-Type")); got != FmtProtoDelim {
		t.Fatalf("Content-Type negotiated %q; want protobuf", got)
	}
	mfs := decodeProtoFamilies(t, rec.Body.Bytes())
	if findFamily(mfs, "envoy_classic_counter") != nil {
		t.Errorf("Envoy families present with NoEnvoy set")
	}
	if findFamily(mfs, "app_requests_total") == nil {
		t.Errorf("app_requests_total missing; got %v", familyNames(mfs))
	}
}
