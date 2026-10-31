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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/test"
)

// makeBenchProtoBody constructs a delimited Prometheus protobuf body containing
// roughly nFamilies metric families to size the per-scrape work the merger
// faces in production. Used by the byte-concat vs decode-reencode benchmarks
// committed to in the PR thread.
func makeBenchProtoBody(t testing.TB, nFamilies int) []byte {
	t.Helper()
	families := make([]*dto.MetricFamily, nFamilies)
	for i := 0; i < nFamilies; i++ {
		families[i] = &dto.MetricFamily{
			Name: proto.String(fmt.Sprintf("bench_counter_%d", i)),
			Help: proto.String(fmt.Sprintf("bench help %d", i)),
			Type: dto.MetricType_COUNTER.Enum(),
			Metric: []*dto.Metric{
				{
					Counter: &dto.Counter{Value: proto.Float64(float64(i))},
					Label: []*dto.LabelPair{
						{Name: proto.String("idx"), Value: proto.String(fmt.Sprintf("%d", i))},
					},
				},
			},
		}
	}
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, FmtProtoDelim)
	for _, fam := range families {
		if err := enc.Encode(fam); err != nil {
			t.Fatalf("encode family %q: %v", fam.GetName(), err)
		}
	}
	return buf.Bytes()
}

// BenchmarkMerger_ByteConcat measures the cost of the historical byte-concat
// fast path: read body, write body. The proto path (decode-reencode) must be
// compared against this baseline.
func BenchmarkMerger_ByteConcat(b *testing.B) {
	body := makeBenchProtoBody(b, 10_000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if _, err := io.Copy(&out, bytes.NewReader(body)); err != nil {
			b.Fatal(err)
		}
		_ = out.Bytes()
	}
}

// BenchmarkMerger_DecodeReencode_Proto measures the cost of decode-and-re-encode
// on a delimited protobuf body. This is the per-scrape overhead any
// always-decode/reencode merger design (the option @krinkinmu suggested in the
// PR review) would incur on every scrape.
func BenchmarkMerger_DecodeReencode_Proto(b *testing.B) {
	body := makeBenchProtoBody(b, 10_000)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mfs, err := decodeAll(bytes.NewReader(body), FmtProtoDelim)
		if err != nil {
			b.Fatal(err)
		}
		var out bytes.Buffer
		enc := expfmt.NewEncoder(&out, FmtProtoDelim)
		for _, mf := range mfs {
			if err := enc.Encode(mf); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkMerger_DecodeReencode_OM measures the cost of decode-and-re-encode
// on an OpenMetrics text body via the new openmetrics_decode.go adapter. This
// is the cost path the proto merger now pays when an upstream returns OM
// (Python prometheus_client default).
func BenchmarkMerger_DecodeReencode_OM(b *testing.B) {
	// Build an OM body roughly comparable in size to the proto benchmark.
	var buf bytes.Buffer
	buf.WriteString("# TYPE bench_counter counter\n")
	for i := 0; i < 10_000; i++ {
		fmt.Fprintf(&buf, "bench_counter_total{idx=\"%d\"} %d\n", i, i)
	}
	buf.WriteString("# EOF\n")
	body := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mfs, err := decodeAll(bytes.NewReader(body), FmtOpenMetrics_1_0_0)
		if err != nil {
			b.Fatal(err)
		}
		var out bytes.Buffer
		enc := expfmt.NewEncoder(&out, FmtOpenMetrics_1_0_0)
		for _, mf := range mfs {
			if err := enc.Encode(mf); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// makeBenchTextAppBody builds a text exposition body of roughly size bytes made of
// classic histograms, the shape a typical text-only application serves.
func makeBenchTextAppBody(size int) []byte {
	var buf bytes.Buffer
	for i := 0; buf.Len() < size; i++ {
		fmt.Fprintf(&buf, "# HELP app_latency_%d_seconds latency\n# TYPE app_latency_%d_seconds histogram\n", i, i)
		for _, le := range []string{"0.005", "0.01", "0.05", "0.1", "0.5", "1", "5", "+Inf"} {
			fmt.Fprintf(&buf, "app_latency_%d_seconds_bucket{route=\"/api/v1/items\",code=\"200\",le=\"%s\"} %d\n", i, le, i)
		}
		fmt.Fprintf(&buf, "app_latency_%d_seconds_sum{route=\"/api/v1/items\",code=\"200\"} 1.5\n", i)
		fmt.Fprintf(&buf, "app_latency_%d_seconds_count{route=\"/api/v1/items\",code=\"200\"} %d\n", i, i)
	}
	return buf.Bytes()
}

// BenchmarkHandleStats_TextApp is the cost comparison for protobuf upconversion. It
// scrapes a text application merged with a protobuf-capable Envoy end to end through
// handleStats, in three modes: a text-only scraper (byte-concat fast path), a
// protobuf-preferring scraper with upconversion disabled (decode, downgrade to text),
// and the same scraper with upconversion enabled (decode, re-encode as protobuf).
func BenchmarkHandleStats_TextApp(b *testing.B) {
	envoyProto := makeBenchProtoBody(b, 2_000)
	envoyMfs, err := decodeAll(bytes.NewReader(envoyProto), FmtProtoDelim)
	if err != nil {
		b.Fatal(err)
	}
	var envoyText bytes.Buffer
	textEnc := expfmt.NewEncoder(&envoyText, FmtText)
	for _, mf := range envoyMfs {
		if err := textEnc.Encode(mf); err != nil {
			b.Fatal(err)
		}
	}
	envoySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), expfmt.ProtoType) {
			w.Header().Set("Content-Type", string(FmtProtoDelim))
			_, _ = w.Write(envoyProto)
			return
		}
		w.Header().Set("Content-Type", FmtText)
		_, _ = w.Write(envoyText.Bytes())
	}))
	defer envoySrv.Close()
	envoyPort, err := strconv.Atoi(strings.Split(envoySrv.URL, ":")[2])
	if err != nil {
		b.Fatal(err)
	}

	modes := []struct {
		name      string
		accept    string
		upconvert bool
	}{
		{"fastpath", "text/plain;version=0.0.4", false},
		{"downgrade", acceptPrometheusNative, false},
		{"upconvert", acceptPrometheusNative, true},
	}
	for _, size := range []int{100 << 10, 1 << 20, 5 << 20} {
		appBody := makeBenchTextAppBody(size)
		appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", FmtText)
			_, _ = w.Write(appBody)
		}))
		for _, mode := range modes {
			b.Run(fmt.Sprintf("app=%dKiB/%s", size>>10, mode.name), func(b *testing.B) {
				test.SetForTest(b, &features.AgentMetricsProtobufUpconversion, mode.upconvert)
				server := &Server{
					prometheus:      &PrometheusScrapeConfiguration{Port: strings.Split(appSrv.URL, ":")[2]},
					registry:        TestingRegistry(b),
					envoyStatsPort:  envoyPort,
					http:            &http.Client{},
					maxAppBodyBytes: defaultMaxAppMetricsBodyBytes,
				}
				req := &http.Request{Header: make(http.Header)}
				req.Header.Set("Accept", mode.accept)
				b.SetBytes(int64(len(appBody)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					rec := httptest.NewRecorder()
					server.handleStats(rec, req)
					if rec.Code != http.StatusOK {
						b.Fatalf("status = %d", rec.Code)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(scrapeWithAccept(server, mode.accept).Body.Len()), "resp-bytes")
			})
		}
		appSrv.Close()
	}
}
