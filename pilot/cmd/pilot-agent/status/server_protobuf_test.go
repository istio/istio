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
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"
)

// nativeHistogramFamily builds a hand-rolled *dto.MetricFamily exercising every
// native-histogram field the merger must preserve unchanged from input to output.
// The fixture is deliberately rich (positive and negative spans, zero bucket,
// non-zero schema) so the round-trip assertion in the proto tests can detect
// any field that the merger drops or rewrites.
func nativeHistogramFamily() *dto.MetricFamily {
	return &dto.MetricFamily{
		Name: proto.String("test_native_histogram"),
		Help: proto.String("Synthetic native histogram for the merger round-trip test."),
		Type: dto.MetricType_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{{
			Histogram: &dto.Histogram{
				SampleCount:   proto.Uint64(10),
				SampleSum:     proto.Float64(42.0),
				Schema:        proto.Int32(3),
				ZeroThreshold: proto.Float64(0.001),
				ZeroCount:     proto.Uint64(1),
				PositiveSpan: []*dto.BucketSpan{
					{Offset: proto.Int32(0), Length: proto.Uint32(2)},
				},
				PositiveDelta: []int64{1, 2},
				NegativeSpan: []*dto.BucketSpan{
					{Offset: proto.Int32(-1), Length: proto.Uint32(1)},
				},
				NegativeDelta: []int64{1},
			},
		}},
	}
}

// classicCounterFamily is a non-histogram MetricFamily used to verify the merger
// preserves ordinary metrics on the proto path. We assert by name lookup rather
// than equality so the encoder's escaping pass (which mutates names that contain
// dots) does not break the assertion if anyone later adds dotted names.
func classicCounterFamily(name string) *dto.MetricFamily {
	return &dto.MetricFamily{
		Name: proto.String(name),
		Type: dto.MetricType_COUNTER.Enum(),
		Metric: []*dto.Metric{{
			Counter: &dto.Counter{Value: proto.Float64(7)},
		}},
	}
}

func encodeProtoFamilies(t *testing.T, mfs ...*dto.MetricFamily) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, FmtProtoDelim)
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	return buf.Bytes()
}

func decodeProtoFamilies(t *testing.T, body []byte) []*dto.MetricFamily {
	t.Helper()
	mfs, err := decodeAll(bytes.NewReader(body), FmtProtoDelim)
	if err != nil {
		t.Fatalf("decodeAll: %v", err)
	}
	return mfs
}

func findFamily(mfs []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

// TestNegotiateMetricsFormat_Protobuf covers the new protobuf branch in
// negotiateMetricsFormat. Each case feeds a Content-Type value that an upstream
// might plausibly return and asserts the recognized format. The "missing"
// cases must fall through to FmtText so a misbehaving upstream cannot trick the
// merger into delimit-decoding a text body.
func TestNegotiateMetricsFormat_Protobuf(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		want        expfmt.Format
	}{
		{
			name:        "canonical proto delimited",
			contentType: `application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`,
			want:        FmtProtoDelim,
		},
		{
			name:        "proto with charset suffix",
			contentType: `application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited; charset=utf-8`,
			want:        FmtProtoDelim,
		},
		{
			name:        "proto wrong subschema",
			contentType: `application/vnd.google.protobuf; proto=com.example.OtherMessage; encoding=delimited`,
			want:        FmtText,
		},
		{
			name:        "proto wrong encoding",
			contentType: `application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=compact-text`,
			want:        FmtText,
		},
		{
			name:        "proto missing encoding param",
			contentType: `application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily`,
			want:        FmtText,
		},
		{
			name:        "malformed content type",
			contentType: `application/vnd.google.protobuf;;;`,
			want:        FmtText,
		},
		{
			name:        "plain text default unchanged",
			contentType: "text/plain; version=0.0.4; charset=utf-8",
			want:        FmtText,
		},
		{
			name:        "openmetrics unchanged",
			contentType: `application/openmetrics-text; version=1.0.0; charset=utf-8`,
			want:        FmtOpenMetrics_1_0_0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := negotiateMetricsFormat(tc.contentType)
			if got != tc.want {
				t.Fatalf("negotiateMetricsFormat(%q) = %q; want %q", tc.contentType, got, tc.want)
			}
		})
	}
}

// TestMergeFormat exhaustively covers the cross-product of the three formats plus
// the empty-and-failure cases.
func TestMergeFormat(t *testing.T) {
	cases := []struct {
		name string
		in   []expfmt.Format
		want expfmt.Format
	}{
		{"empty", nil, FmtText},
		{"all empty strings", []expfmt.Format{"", ""}, FmtText},
		{"only text", []expfmt.Format{FmtText}, FmtText},
		{"only openmetrics", []expfmt.Format{FmtOpenMetrics_1_0_0}, FmtOpenMetrics_1_0_0},
		{"only proto", []expfmt.Format{FmtProtoDelim}, FmtProtoDelim},
		{"text + text", []expfmt.Format{FmtText, FmtText}, FmtText},
		{"openmetrics + openmetrics same version", []expfmt.Format{FmtOpenMetrics_1_0_0, FmtOpenMetrics_1_0_0}, FmtOpenMetrics_1_0_0},
		{"proto + proto", []expfmt.Format{FmtProtoDelim, FmtProtoDelim}, FmtProtoDelim},
		{"text + proto downgrades", []expfmt.Format{FmtText, FmtProtoDelim}, FmtText},
		{"proto + text downgrades", []expfmt.Format{FmtProtoDelim, FmtText}, FmtText},
		{"openmetrics + proto downgrades", []expfmt.Format{FmtOpenMetrics_1_0_0, FmtProtoDelim}, FmtText},
		{"proto + openmetrics downgrades", []expfmt.Format{FmtProtoDelim, FmtOpenMetrics_1_0_0}, FmtText},
		{"text + openmetrics downgrades", []expfmt.Format{FmtText, FmtOpenMetrics_1_0_0}, FmtText},
		{"openmetrics v1 + v0 downgrades", []expfmt.Format{FmtOpenMetrics_1_0_0, FmtOpenMetrics_0_0_1}, FmtText},
		{"empty interleaved with proto wins", []expfmt.Format{"", FmtProtoDelim, ""}, FmtProtoDelim},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeFormat(tc.in...)
			if got != tc.want {
				t.Fatalf("mergeFormat(%v) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestScrapeAndWriteAgentMetrics_Protobuf round-trips agent self-metrics through
// the proto encoder and decoder. The recovered MetricFamily set must be non-empty,
// proving that the format parameter (rather than the historical hardcoded FmtText)
// is honored.
func TestScrapeAndWriteAgentMetrics_Protobuf(t *testing.T) {
	registry := TestingRegistry(t)

	var buf bytes.Buffer
	if err := scrapeAndWriteAgentMetrics(registry, &buf, FmtProtoDelim); err != nil {
		t.Fatalf("scrapeAndWriteAgentMetrics: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty proto output from agent metrics")
	}

	mfs := decodeProtoFamilies(t, buf.Bytes())
	if len(mfs) == 0 {
		t.Fatal("expected at least one MetricFamily decoded from agent metrics proto stream")
	}
}

// TestHandleStats_ProtobufEnvoyNoApp exercises the proto fast path with only an
// Envoy upstream. The fixture includes a native-histogram MetricFamily; the test
// asserts every native-histogram field survives the merger unchanged. This is
// the load-bearing test for the entire PR — if the merger drops native-histogram
// structure, native histograms are unusable end-to-end.
func TestHandleStats_ProtobufEnvoyNoApp(t *testing.T) {
	envoyHist := nativeHistogramFamily()
	envoyCounter := classicCounterFamily("envoy_test_counter")
	envoyBody := encodeProtoFamilies(t, envoyHist, envoyCounter)

	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		if _, err := w.Write(envoyBody); err != nil {
			t.Fatalf("envoy write: %v", err)
		}
	}))
	defer envoySrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept",
		`application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if negotiateMetricsFormat(ct) != FmtProtoDelim {
		t.Fatalf("Content-Type = %q; want proto-delimited", ct)
	}

	mfs := decodeProtoFamilies(t, rec.Body.Bytes())
	gotHist := findFamily(mfs, "test_native_histogram")
	if gotHist == nil {
		t.Fatalf("native histogram not present in merged output; got names: %v", familyNames(mfs))
	}

	// Load-bearing canonical equality: the full input Histogram (every span, every
	// delta, ZeroThreshold, NegativeDelta, SampleCount, SampleSum, schema) must
	// round-trip unchanged through the merger. proto.Equal compares every populated
	// field; if the merger drops or rewrites any subfield, this fails.
	wantHist := envoyHist.GetMetric()[0].GetHistogram()
	gotHistogram := gotHist.GetMetric()[0].GetHistogram()
	if !proto.Equal(wantHist, gotHistogram) {
		t.Fatalf("native histogram round-trip not byte-equal\n  want: %+v\n   got: %+v", wantHist, gotHistogram)
	}

	// Retain field-level assertions for human-readable diagnostics if proto.Equal
	// ever fires; they make the failure root cause obvious without re-running.
	if gotHistogram.GetSchema() != 3 {
		t.Errorf("Schema lost: got %v", gotHistogram.GetSchema())
	}
	if gotHistogram.GetZeroCount() != 1 {
		t.Errorf("ZeroCount lost: got %v", gotHistogram.GetZeroCount())
	}
	if len(gotHistogram.GetPositiveSpan()) == 0 {
		t.Error("PositiveSpan lost")
	}
	if len(gotHistogram.GetPositiveDelta()) == 0 {
		t.Error("PositiveDelta lost")
	}
	if len(gotHistogram.GetNegativeSpan()) == 0 {
		t.Error("NegativeSpan lost")
	}

	if findFamily(mfs, "envoy_test_counter") == nil {
		t.Errorf("classic counter not present in merged output; got names: %v", familyNames(mfs))
	}
}

func familyNames(mfs []*dto.MetricFamily) []string {
	out := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		out = append(out, mf.GetName())
	}
	return out
}

// TestHandleStats_ProtobufEnvoyProtoApp exercises the proto path when both
// upstreams are proto. The native histogram from Envoy and a counter from the
// app must both appear in the merged output.
func TestHandleStats_ProtobufEnvoyProtoApp(t *testing.T) {
	envoyBody := encodeProtoFamilies(t, nativeHistogramFamily())
	appBody := encodeProtoFamilies(t, classicCounterFamily("app_counter"))

	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(envoyBody)
	}))
	defer envoySrv.Close()
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(appBody)
	}))
	defer appSrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		prometheus: &PrometheusScrapeConfiguration{
			Port: strings.Split(appSrv.URL, ":")[2],
		},
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept",
		`application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if negotiateMetricsFormat(rec.Header().Get("Content-Type")) != FmtProtoDelim {
		t.Fatalf("Content-Type = %q; want proto", rec.Header().Get("Content-Type"))
	}

	mfs := decodeProtoFamilies(t, rec.Body.Bytes())
	if findFamily(mfs, "test_native_histogram") == nil {
		t.Errorf("native histogram missing; got %v", familyNames(mfs))
	}
	if findFamily(mfs, "app_counter") == nil {
		t.Errorf("app counter missing; got %v", familyNames(mfs))
	}
}

// TestHandleStats_ProtobufEnvoyTextApp covers the mixed-format downgrade: Envoy
// is proto, app is text, output must be text. The text body must parse as text;
// the binary native-histogram structure naturally collapses (text exposition is
// not defined for native histograms), but the classic envoy counter must
// survive and the app counter must survive.
func TestHandleStats_ProtobufEnvoyTextApp(t *testing.T) {
	envoyBody := encodeProtoFamilies(t,
		classicCounterFamily("envoy_classic_counter"),
		nativeHistogramFamily(),
	)
	appText := "# TYPE app_metric counter\napp_metric 1\n"

	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(envoyBody)
	}))
	defer envoySrv.Close()
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", FmtText)
		_, _ = io.WriteString(w, appText)
	}))
	defer appSrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		prometheus: &PrometheusScrapeConfiguration{
			Port: strings.Split(appSrv.URL, ":")[2],
		},
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept", `application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if got := negotiateMetricsFormat(ct); got != FmtText {
		t.Fatalf("Content-Type = %q (negotiated %q); want text downgrade", ct, got)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "envoy_classic_counter") {
		t.Errorf("envoy_classic_counter missing from downgraded body; body=%q", body)
	}
	if !strings.Contains(body, "app_metric") {
		t.Errorf("app_metric missing from downgraded body; body=%q", body)
	}
}

// TestHandleStats_ProtobufEnvoyOMApp_PreservesOMFields covers the regression
// flagged in PR review by @krinkinmu: when the proto path fires with an
// OpenMetrics app upstream, the expfmt text decoder (used to silently drop OM
// exemplars, _created lines, and # UNIT) is now replaced by the
// textparse.OpenMetricsParser-backed adapter (decodeOpenMetricsToFamilies).
// This test verifies the regression is fixed end-to-end: an OM body with
// exemplars + _created + # UNIT parses through the proto merger without
// dropping the counter (the prior expfmt path would parse-error the exemplar
// line and drop the entire family).
func TestHandleStats_ProtobufEnvoyOMApp_PreservesOMFields(t *testing.T) {
	// Envoy returns proto (forces the proto path).
	envoyBody := encodeProtoFamilies(t, classicCounterFamily("envoy_classic_counter"))

	// App returns OpenMetrics with all the features the legacy expfmt textDecoder
	// silently mangled: exemplar on a counter sample, _created line, # UNIT.
	appOM := strings.Join([]string{
		`# HELP app_requests Total app requests`,
		`# TYPE app_requests counter`,
		`# UNIT app_requests requests`,
		`app_requests_total{method="get"} 42 # {trace_id="abc"} 0.5 1700000001.5`,
		`app_requests_created{method="get"} 1700000000.0`,
		`# EOF`,
		``,
	}, "\n")

	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(envoyBody)
	}))
	defer envoySrv.Close()
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", FmtOpenMetrics_1_0_0)
		_, _ = io.WriteString(w, appOM)
	}))
	defer appSrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		prometheus: &PrometheusScrapeConfiguration{
			Port: strings.Split(appSrv.URL, ":")[2],
		},
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept", `application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	body := rec.Body.String()
	// The Envoy proto + app OM disagreement downgrades the output to text. The
	// VALUE of the counter must survive (legacy expfmt path would parse-error
	// the exemplar line and drop the entire family — that is exactly the
	// regression this test guards against). The text re-encoder may strip the
	// "_total" OM convention from the metric name, so we accept either spelling.
	if !strings.Contains(body, "app_requests{method=\"get\"} 42") && !strings.Contains(body, "app_requests_total{method=\"get\"} 42") {
		t.Errorf("app_requests value missing from merged body — exemplar parse may have dropped it; body=%q", body)
	}
	if !strings.Contains(body, "envoy_classic_counter") {
		t.Errorf("envoy_classic_counter missing from merged body; body=%q", body)
	}
}

// TestHandleStats_ProtobufEnvoyOMApp_DecoderPreservesExemplars exercises the
// same regression but asserts at the decoder layer (independent of the output
// format) that the adapter populates Exemplar + CreatedTimestamp + Unit on the
// produced *dto.MetricFamily. This is the precise field-level guarantee the
// reviewer asked for: prior to this PR's openmetrics_decode.go, these would be
// silently dropped because expfmt routed OM bodies through its text parser.
func TestHandleStats_ProtobufEnvoyOMApp_DecoderPreservesExemplars(t *testing.T) {
	appOM := strings.Join([]string{
		`# HELP app_requests Total app requests`,
		`# TYPE app_requests counter`,
		`# UNIT app_requests requests`,
		`app_requests_total{method="get"} 42 # {trace_id="abc"} 0.5 1700000001.5`,
		`app_requests_created{method="get"} 1700000000.0`,
		`# EOF`,
		``,
	}, "\n")

	mfs, err := decodeAll(strings.NewReader(appOM), FmtOpenMetrics_1_0_0)
	if err != nil {
		t.Fatalf("decodeAll on OM body: %v", err)
	}
	if len(mfs) != 1 {
		t.Fatalf("expected 1 family, got %d", len(mfs))
	}
	fam := mfs[0]
	if fam.GetName() != "app_requests" {
		t.Errorf("family name = %q; want app_requests", fam.GetName())
	}
	if fam.GetUnit() != "requests" {
		t.Errorf("Unit = %q; want %q (was silently dropped by expfmt textDecoder before this PR)", fam.GetUnit(), "requests")
	}
	if len(fam.Metric) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(fam.Metric))
	}
	c := fam.Metric[0].GetCounter()
	if c == nil {
		t.Fatalf("metric has no Counter; was the entire family dropped due to exemplar parse error?")
	}
	if c.GetValue() != 42 {
		t.Errorf("counter value = %v; want 42", c.GetValue())
	}
	if c.GetExemplar() == nil {
		t.Fatalf("counter has no Exemplar (was silently dropped by expfmt textDecoder before this PR)")
	}
	if got := c.GetExemplar().GetLabel(); len(got) != 1 || got[0].GetName() != "trace_id" || got[0].GetValue() != "abc" {
		t.Errorf("exemplar labels = %v; want [trace_id=abc]", got)
	}
	if c.GetCreatedTimestamp() == nil {
		t.Fatalf("counter has no CreatedTimestamp (was silently dropped by expfmt textDecoder before this PR)")
	}
	if got := c.GetCreatedTimestamp().AsTime().UnixMilli(); got != 1700000000000 {
		t.Errorf("CreatedTimestamp = %d ms; want 1700000000000 ms", got)
	}
}

// TestHandleStats_EnvoyReturnsOM_RoutesThroughMerger covers the Envoy-side of
// PR review comment #4 (krinkinmu): if Envoy ever returns a non-text/plain
// body (e.g. OpenMetrics in some hypothetical future Envoy release), the
// byte-concat fast path would write an "# EOF" marker mid-stream. The
// dispatcher's needsParseMerge predicate forces the parse-and-re-encode
// merger whenever envoyFormat is non-text/plain, eliminating that hazard.
// Today Envoy never returns OM, but the test future-proofs the rule.
func TestHandleStats_EnvoyReturnsOM_RoutesThroughMerger(t *testing.T) {
	envoyOM := strings.Join([]string{
		`# TYPE envoy_metric counter`,
		`envoy_metric_total{src="envoy"} 5`,
		`# EOF`,
		``,
	}, "\n")
	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", FmtOpenMetrics_1_0_0)
		_, _ = io.WriteString(w, envoyOM)
	}))
	defer envoySrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}

	req := &http.Request{Header: make(http.Header)}
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	body := rec.Body.String()
	// The byte-concat fast path would have left envoy's "# EOF" mid-stream
	// before the agent metrics. The merger re-encodes, producing exactly one
	// trailing "# EOF" (or none if outFormat is text). Count occurrences:
	eofCount := strings.Count(body, "# EOF")
	if eofCount > 1 {
		t.Errorf("merged body contains %d # EOF markers; want ≤1 (byte-concat regression?); body tail: %q",
			eofCount, body[max(0, len(body)-200):])
	}
	if !strings.Contains(body, "envoy_metric") {
		t.Errorf("envoy_metric missing from merged body; body=%q", body)
	}
}

// TestHandleStats_ProtobufMalformedEnvoy verifies that a malformed protobuf
// upstream does not corrupt the merged response. The truncated bytes will
// cause decodeAll to error; the merger must log + drop Envoy, return the agent
// metrics it can, and produce a parseable body.
func TestHandleStats_ProtobufMalformedEnvoy(t *testing.T) {
	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		// Write the prefix of a real proto stream but truncate mid-frame. A
		// non-zero varint length followed by no payload guarantees the decoder
		// returns an error before EOF.
		_, _ = w.Write([]byte{0x05, 0x00, 0x01})
	}))
	defer envoySrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept", `application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	// We still expect a proto response (the merge path picked proto). The
	// merged body may contain just the agent metrics — the load-bearing
	// assertion is that the response is well-formed proto, not garbled bytes.
	if _, err := decodeAll(bytes.NewReader(rec.Body.Bytes()), FmtProtoDelim); err != nil {
		t.Fatalf("merged proto body is malformed after truncated envoy: %v; bytes=%x",
			err, rec.Body.Bytes())
	}
}

// TestHandleStats_NoAcceptHeader_DefaultsToText is the regression guard for the
// historical default: a scraper that omits the Accept header gets text/plain.
func TestHandleStats_NoAcceptHeader_DefaultsToText(t *testing.T) {
	envoyText := "# TYPE my_metric counter\nmy_metric 0\n"
	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", FmtText)
		_, _ = io.WriteString(w, envoyText)
	}))
	defer envoySrv.Close()
	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}
	req := &http.Request{Header: make(http.Header)}
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if negotiateMetricsFormat(rec.Header().Get("Content-Type")) != FmtText {
		t.Fatalf("Content-Type = %q; want text", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "my_metric") {
		t.Fatalf("envoy text body missing from merged response; body=%q", rec.Body.String())
	}
}

// TestScrapeFormatLabel is a small unit test of the label mapper used by the
// scrape_format_total counter. Exhaustive over the FormatType enum we recognize.
func TestScrapeFormatLabel(t *testing.T) {
	cases := []struct {
		name string
		in   expfmt.Format
		want string
	}{
		{"text", FmtText, "text"},
		{"openmetrics", FmtOpenMetrics_1_0_0, "openmetrics"},
		{"proto delim", FmtProtoDelim, "proto"},
		{"proto text", expfmt.NewFormat(expfmt.TypeProtoText), "proto"},
		{"proto compact", expfmt.NewFormat(expfmt.TypeProtoCompact), "proto"},
		{"unknown defaults to text", expfmt.Format(""), "text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scrapeFormatLabel(tc.in)
			if got != tc.want {
				t.Fatalf("scrapeFormatLabel(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

// repeatedCounterBody returns a proto-delimited body with `n` distinct counter
// families. Each MetricFamily is small (~50 bytes); n controls the overall body
// size so tests can probe just-under and just-over the s.maxAppBodyBytes cap.
func repeatedCounterBody(t *testing.T, n int) []byte {
	t.Helper()
	mfs := make([]*dto.MetricFamily, 0, n)
	for i := 0; i < n; i++ {
		mfs = append(mfs, classicCounterFamily("cap_counter_"+strconv.Itoa(i)))
	}
	return encodeProtoFamilies(t, mfs...)
}

// TestProtoPathBodyCap_EnvoyUnderCap verifies the proto path's body cap admits
// a body just below s.maxAppBodyBytes. The Envoy body is sized so that the
// LimitReader fully consumes it without firing the overflow check; all
// MetricFamilies must appear in the merged output.
func TestProtoPathBodyCap_EnvoyUnderCap(t *testing.T) {
	body := repeatedCounterBody(t, 10) // small body, well under any tractable cap
	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(body)
	}))
	defer envoySrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}

	// Cap larger than the body. The merger should decode all 10 families.
	server := &Server{
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: len(body) + 1024,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept",
		`application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	mfs := decodeProtoFamilies(t, rec.Body.Bytes())
	for i := 0; i < 10; i++ {
		name := "cap_counter_" + strconv.Itoa(i)
		if findFamily(mfs, name) == nil {
			t.Errorf("expected family %q in under-cap merged output; got names: %v", name, familyNames(mfs))
		}
	}
}

// TestProtoPathBodyCap_EnvoyOverCap verifies the proto path's body cap drops an
// Envoy body that exceeds s.maxAppBodyBytes. The merger must:
//   - log + increment metrics.EnvoyScrapeErrors (we don't assert the counter
//     directly here because the registry is shared across parallel tests; the
//     load-bearing assertion is that the over-cap families do not appear),
//   - still return a well-formed proto response containing the agent metrics.
//
// This guarantees a misbehaving Envoy cannot OOM the agent through the proto
// path — the same guarantee the multi-target text path already provides.
func TestProtoPathBodyCap_EnvoyOverCap(t *testing.T) {
	// Body large enough that the LimitReader saturates at cap+1.
	body := repeatedCounterBody(t, 200)
	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(body)
	}))
	defer envoySrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}

	// Cap well below the body so the LimitReader saturates.
	bodyCap := len(body) / 4
	if bodyCap < 64 {
		bodyCap = 64
	}
	server := &Server{
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: bodyCap,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept",
		`application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	// Response must still be well-formed proto — the cap drop must not corrupt
	// the wire format.
	mfs, err := decodeAll(bytes.NewReader(rec.Body.Bytes()), FmtProtoDelim)
	if err != nil {
		t.Fatalf("over-cap response is malformed proto: %v", err)
	}
	// Whatever families were partially decoded before the cap fired may appear
	// (matching the existing partial-success behavior), but the highest-index
	// counter from the back of the body must NOT appear — the cap dropped the
	// tail of the stream.
	tail := "cap_counter_199"
	if findFamily(mfs, tail) != nil {
		t.Errorf("over-cap test admitted tail family %q; cap was not enforced (cap=%d, body=%d)",
			tail, bodyCap, len(body))
	}
}

// TestProtoPathBodyCap_SingleAppOverCap verifies the body cap on the
// single-target application path. Symmetric to the Envoy case: an over-cap app
// body must be dropped without corrupting the response.
func TestProtoPathBodyCap_SingleAppOverCap(t *testing.T) {
	envoyBody := encodeProtoFamilies(t, classicCounterFamily("envoy_small_counter"))
	appBody := repeatedCounterBody(t, 200) // large

	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(envoyBody)
	}))
	defer envoySrv.Close()
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(appBody)
	}))
	defer appSrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	bodyCap := len(appBody) / 4
	if bodyCap < 128 {
		bodyCap = 128
	}
	server := &Server{
		prometheus: &PrometheusScrapeConfiguration{
			Port: strings.Split(appSrv.URL, ":")[2],
		},
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: bodyCap,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept",
		`application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	mfs, err := decodeAll(bytes.NewReader(rec.Body.Bytes()), FmtProtoDelim)
	if err != nil {
		t.Fatalf("over-cap response is malformed proto: %v", err)
	}
	// Envoy must survive (under cap on its own scrape).
	if findFamily(mfs, "envoy_small_counter") == nil {
		t.Errorf("envoy small counter missing after app over-cap; got %v", familyNames(mfs))
	}
	// App tail must NOT survive.
	if findFamily(mfs, "cap_counter_199") != nil {
		t.Errorf("app over-cap admitted tail family; cap not enforced (cap=%d, body=%d)",
			bodyCap, len(appBody))
	}
}

// TestScrapeMultipleAppsBodyCap_PerBodyFormats is a focused unit test on
// scrapeMultipleApps's pre-existing per-target cap PLUS the new per-body formats
// return value. An over-cap target must be dropped and represented as nil in the
// returned bodies slice, while under-cap targets pass through; the new third
// return value (per-body formats) must parallel bodies.
func TestScrapeMultipleAppsBodyCap_PerBodyFormats(t *testing.T) {
	smallBody := strings.Repeat("a", 256)
	bigBody := strings.Repeat("b", 4*1024)

	smallSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", FmtText)
		_, _ = io.WriteString(w, smallBody)
	}))
	defer smallSrv.Close()
	bigSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", FmtText)
		_, _ = io.WriteString(w, bigBody)
	}))
	defer bigSrv.Close()

	smallPort := strings.Split(smallSrv.URL, ":")[2]
	bigPort := strings.Split(bigSrv.URL, ":")[2]
	server := &Server{
		prometheus: &PrometheusScrapeConfiguration{
			Targets: []ScrapeTarget{
				{Port: smallPort, Path: "/"},
				{Port: bigPort, Path: "/"},
			},
		},
		registry:        TestingRegistry(t),
		http:            &http.Client{},
		maxAppBodyBytes: 1024, // smaller than bigBody (4KiB), larger than smallBody (256B)
	}

	req := &http.Request{Header: make(http.Header)}
	format, bodies, bodyFormats := server.scrapeMultipleApps(req)

	if len(bodies) != 2 || len(bodyFormats) != 2 {
		t.Fatalf("scrapeMultipleApps returned %d bodies / %d formats; want 2/2", len(bodies), len(bodyFormats))
	}
	if bodies[0] == nil {
		t.Errorf("small (under-cap) target was dropped; expected non-nil body")
	}
	if bodies[1] != nil {
		t.Errorf("big (over-cap) target was admitted; expected nil body, got %d bytes", len(bodies[1]))
	}
	if format != FmtText {
		t.Errorf("aggregated format = %q; want FmtText", format)
	}
}

// TestProtoPath_MixedMultiTarget covers M2: a multi-target app config where one
// target returns proto and another returns text. scrapeMultipleApps aggregates
// the format to FmtText (mixed-format downgrade), but the proto body bytes in
// appBodies[0] are still binary. Before the fix, writeMergedProtoPath would
// decode every body with appFormat=FmtText, mis-parsing the proto target and
// silently dropping its metrics. After the fix, per-body formats are threaded
// through and each body decodes in its true format. Both targets' metrics must
// appear in the final (text) output.
func TestProtoPath_MixedMultiTarget(t *testing.T) {
	envoyBody := encodeProtoFamilies(t, classicCounterFamily("envoy_mixed_counter"))
	protoTargetBody := encodeProtoFamilies(t, classicCounterFamily("proto_target_counter"))
	textTargetBody := "# TYPE text_target_counter counter\ntext_target_counter 5\n"

	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(envoyBody)
	}))
	defer envoySrv.Close()
	protoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", string(FmtProtoDelim))
		_, _ = w.Write(protoTargetBody)
	}))
	defer protoSrv.Close()
	textSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", FmtText)
		_, _ = io.WriteString(w, textTargetBody)
	}))
	defer textSrv.Close()

	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		prometheus: &PrometheusScrapeConfiguration{
			Targets: []ScrapeTarget{
				{Port: strings.Split(protoSrv.URL, ":")[2], Path: "/"},
				{Port: strings.Split(textSrv.URL, ":")[2], Path: "/"},
			},
		},
		registry:        TestingRegistry(t),
		envoyStatsPort:  envoyPort,
		http:            &http.Client{},
		maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
	}

	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Accept",
		`application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited`)
	rec := httptest.NewRecorder()
	server.handleStats(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	// The output must be text — mixed proto+text downgrades. Envoy is proto so
	// anyProto=true; the merge format is text (since aggregated app format is
	// text from the mixed downgrade and Envoy says proto → mergeFormat → text).
	ct := rec.Header().Get("Content-Type")
	if got := negotiateMetricsFormat(ct); got != FmtText {
		t.Fatalf("Content-Type = %q (negotiated %q); want text downgrade", ct, got)
	}
	body := rec.Body.String()
	// Envoy's counter (proto-decoded → text-encoded) must survive.
	if !strings.Contains(body, "envoy_mixed_counter") {
		t.Errorf("envoy_mixed_counter missing from mixed-multi-target output; body=%q", body)
	}
	// The proto-app target's counter MUST survive (the M2 fix).
	if !strings.Contains(body, "proto_target_counter") {
		t.Errorf("proto_target_counter missing — M2 regression: per-body format not honored; body=%q", body)
	}
	// The text-app target's counter must survive.
	if !strings.Contains(body, "text_target_counter") {
		t.Errorf("text_target_counter missing from mixed-multi-target output; body=%q", body)
	}
}
