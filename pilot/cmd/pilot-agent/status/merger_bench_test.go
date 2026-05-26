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
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"
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
