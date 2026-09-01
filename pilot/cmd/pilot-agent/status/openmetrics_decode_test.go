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
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

// findFamilyByName is a test helper.
func findFamilyByName(t *testing.T, mfs []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	t.Fatalf("metric family %q not found in %d families", name, len(mfs))
	return nil
}

func TestDecodeOpenMetrics_CounterWithCreatedAndUnit(t *testing.T) {
	body := strings.Join([]string{
		`# HELP http_requests Total HTTP requests`,
		`# TYPE http_requests counter`,
		`# UNIT http_requests requests`,
		`http_requests_total{method="get"} 42`,
		`http_requests_created{method="get"} 1700000000.5`,
		`# EOF`,
		``,
	}, "\n")

	mfs, err := decodeOpenMetricsToFamilies(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(mfs) != 1 {
		t.Fatalf("expected 1 family, got %d", len(mfs))
	}
	fam := mfs[0]
	if fam.GetName() != "http_requests" {
		t.Errorf("name = %q; want http_requests", fam.GetName())
	}
	if fam.GetType() != dto.MetricType_COUNTER {
		t.Errorf("type = %v; want COUNTER", fam.GetType())
	}
	if fam.GetUnit() != "requests" {
		t.Errorf("unit = %q; want %q", fam.GetUnit(), "requests")
	}
	if fam.GetHelp() != "Total HTTP requests" {
		t.Errorf("help = %q; want %q", fam.GetHelp(), "Total HTTP requests")
	}
	if len(fam.Metric) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(fam.Metric))
	}
	c := fam.Metric[0].GetCounter()
	if c == nil {
		t.Fatalf("metric has no Counter")
	}
	if c.GetValue() != 42 {
		t.Errorf("counter value = %v; want 42", c.GetValue())
	}
	if c.GetCreatedTimestamp() == nil {
		t.Fatalf("counter has no CreatedTimestamp (expected 1700000000500ms)")
	}
	wantMs := int64(1700000000500)
	gotMs := c.GetCreatedTimestamp().AsTime().UnixMilli()
	if gotMs != wantMs {
		t.Errorf("CreatedTimestamp = %d ms; want %d ms", gotMs, wantMs)
	}
}

func TestDecodeOpenMetrics_CounterWithExemplar(t *testing.T) {
	body := strings.Join([]string{
		`# HELP requests_total total requests`,
		`# TYPE requests_total counter`,
		`requests_total{method="get"} 7 # {trace_id="abc123"} 0.5 1700000001.5`,
		`# EOF`,
		``,
	}, "\n")

	mfs, err := decodeOpenMetricsToFamilies(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	fam := findFamilyByName(t, mfs, "requests")
	if fam.GetType() != dto.MetricType_COUNTER {
		t.Errorf("type = %v; want COUNTER", fam.GetType())
	}
	if len(fam.Metric) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(fam.Metric))
	}
	c := fam.Metric[0].GetCounter()
	if c == nil {
		t.Fatalf("metric has no Counter")
	}
	if c.GetValue() != 7 {
		t.Errorf("counter value = %v; want 7", c.GetValue())
	}
	ex := c.GetExemplar()
	if ex == nil {
		t.Fatalf("counter has no Exemplar (regression: expfmt textDecoder drops these)")
	}
	if ex.GetValue() != 0.5 {
		t.Errorf("exemplar value = %v; want 0.5", ex.GetValue())
	}
	if ex.GetTimestamp() == nil {
		t.Fatalf("exemplar has no Timestamp")
	}
	if got := ex.GetTimestamp().AsTime().UnixMilli(); got != 1700000001500 {
		t.Errorf("exemplar ts = %d ms; want 1700000001500 ms", got)
	}
	if len(ex.GetLabel()) != 1 || ex.GetLabel()[0].GetName() != "trace_id" || ex.GetLabel()[0].GetValue() != "abc123" {
		t.Errorf("exemplar labels = %v; want [trace_id=abc123]", ex.GetLabel())
	}
}

func TestDecodeOpenMetrics_HistogramWithBucketExemplarsAndCreated(t *testing.T) {
	body := strings.Join([]string{
		`# HELP latency_seconds request latency`,
		`# TYPE latency_seconds histogram`,
		`# UNIT latency_seconds seconds`,
		`latency_seconds_bucket{le="0.1"} 3 # {trace_id="t1"} 0.05`,
		`latency_seconds_bucket{le="1.0"} 5 # {trace_id="t2"} 0.42`,
		`latency_seconds_bucket{le="+Inf"} 6`,
		`latency_seconds_sum 2.7`,
		`latency_seconds_count 6`,
		`latency_seconds_created 1700000000.0`,
		`# EOF`,
		``,
	}, "\n")

	mfs, err := decodeOpenMetricsToFamilies(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	fam := findFamilyByName(t, mfs, "latency_seconds")
	if fam.GetType() != dto.MetricType_HISTOGRAM {
		t.Fatalf("type = %v; want HISTOGRAM", fam.GetType())
	}
	if fam.GetUnit() != "seconds" {
		t.Errorf("unit = %q; want seconds", fam.GetUnit())
	}
	if len(fam.Metric) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(fam.Metric))
	}
	h := fam.Metric[0].GetHistogram()
	if h == nil {
		t.Fatalf("metric has no Histogram")
	}
	if h.GetSampleCount() != 6 {
		t.Errorf("sample count = %d; want 6", h.GetSampleCount())
	}
	if h.GetSampleSum() != 2.7 {
		t.Errorf("sample sum = %v; want 2.7", h.GetSampleSum())
	}
	if len(h.Bucket) != 3 {
		t.Fatalf("expected 3 buckets, got %d", len(h.Bucket))
	}
	// Bucket with le=0.1 has exemplar t1; bucket with le=1.0 has exemplar t2; le=+Inf has none.
	wantBuckets := map[float64]struct {
		count    uint64
		exemplar string
	}{
		0.1: {3, "t1"},
		1.0: {5, "t2"},
		// +Inf bucket inferred from math.Inf — match by GetUpperBound() being +Inf.
	}
	for _, b := range h.Bucket {
		ub := b.GetUpperBound()
		if want, ok := wantBuckets[ub]; ok {
			if b.GetCumulativeCount() != want.count {
				t.Errorf("bucket le=%v count = %d; want %d", ub, b.GetCumulativeCount(), want.count)
			}
			if b.GetExemplar() == nil {
				t.Errorf("bucket le=%v has no exemplar; want trace_id=%s", ub, want.exemplar)
			} else if b.GetExemplar().GetLabel()[0].GetValue() != want.exemplar {
				t.Errorf("bucket le=%v exemplar trace_id = %q; want %q",
					ub, b.GetExemplar().GetLabel()[0].GetValue(), want.exemplar)
			}
		}
	}
	if h.GetCreatedTimestamp() == nil {
		t.Fatalf("histogram has no CreatedTimestamp")
	}
	if got := h.GetCreatedTimestamp().AsTime().UnixMilli(); got != 1700000000000 {
		t.Errorf("CreatedTimestamp = %d ms; want 1700000000000 ms", got)
	}
}

func TestDecodeOpenMetrics_Gauge(t *testing.T) {
	body := strings.Join([]string{
		`# HELP cpu_temp cpu temperature`,
		`# TYPE cpu_temp gauge`,
		`cpu_temp{cpu="0"} 47.3`,
		`cpu_temp{cpu="1"} 51.2`,
		`# EOF`,
		``,
	}, "\n")
	mfs, err := decodeOpenMetricsToFamilies(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	fam := findFamilyByName(t, mfs, "cpu_temp")
	if fam.GetType() != dto.MetricType_GAUGE {
		t.Errorf("type = %v; want GAUGE", fam.GetType())
	}
	if len(fam.Metric) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(fam.Metric))
	}
}

func TestDecodeOpenMetrics_Summary(t *testing.T) {
	body := strings.Join([]string{
		`# HELP rpc_dur rpc duration`,
		`# TYPE rpc_dur summary`,
		`rpc_dur{quantile="0.5"} 0.012`,
		`rpc_dur{quantile="0.99"} 0.087`,
		`rpc_dur_sum 12.4`,
		`rpc_dur_count 1000`,
		`rpc_dur_created 1700000000.0`,
		`# EOF`,
		``,
	}, "\n")
	mfs, err := decodeOpenMetricsToFamilies(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	fam := findFamilyByName(t, mfs, "rpc_dur")
	if fam.GetType() != dto.MetricType_SUMMARY {
		t.Fatalf("type = %v; want SUMMARY", fam.GetType())
	}
	if len(fam.Metric) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(fam.Metric))
	}
	s := fam.Metric[0].GetSummary()
	if s == nil {
		t.Fatalf("metric has no Summary")
	}
	if s.GetSampleSum() != 12.4 {
		t.Errorf("sample sum = %v; want 12.4", s.GetSampleSum())
	}
	if s.GetSampleCount() != 1000 {
		t.Errorf("sample count = %v; want 1000", s.GetSampleCount())
	}
	if len(s.Quantile) != 2 {
		t.Errorf("expected 2 quantiles, got %d", len(s.Quantile))
	}
	if s.GetCreatedTimestamp() == nil {
		t.Errorf("summary has no CreatedTimestamp")
	}
}

func TestDecodeOpenMetrics_InfoStateset(t *testing.T) {
	// INFO and STATESET have no native dto enum; they map to UNTYPED with the
	// value verbatim. The point of the test is that they do NOT cause parse
	// errors (the expfmt text decoder errors out on "unknown metric type").
	body := strings.Join([]string{
		`# HELP target_info target info`,
		`# TYPE target_info info`,
		`target_info{instance="i1",job="j1"} 1`,
		`# HELP feature_enabled feature flags`,
		`# TYPE feature_enabled stateset`,
		`feature_enabled{feature="A"} 1`,
		`feature_enabled{feature="B"} 0`,
		`# EOF`,
		``,
	}, "\n")
	mfs, err := decodeOpenMetricsToFamilies(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	info := findFamilyByName(t, mfs, "target_info")
	if info.GetType() != dto.MetricType_UNTYPED {
		t.Errorf("info family type = %v; want UNTYPED", info.GetType())
	}
	if len(info.Metric) != 1 {
		t.Fatalf("expected 1 info metric, got %d", len(info.Metric))
	}
	if info.Metric[0].GetUntyped().GetValue() != 1 {
		t.Errorf("info value = %v; want 1", info.Metric[0].GetUntyped().GetValue())
	}
	stateset := findFamilyByName(t, mfs, "feature_enabled")
	if stateset.GetType() != dto.MetricType_UNTYPED {
		t.Errorf("stateset family type = %v; want UNTYPED", stateset.GetType())
	}
	if len(stateset.Metric) != 2 {
		t.Errorf("expected 2 stateset metrics, got %d", len(stateset.Metric))
	}
}

func TestDecodeOpenMetrics_EmptyBody(t *testing.T) {
	mfs, err := decodeOpenMetricsToFamilies(bytes.NewReader(nil))
	if err != nil {
		t.Errorf("empty body returned error: %v", err)
	}
	if len(mfs) != 0 {
		t.Errorf("empty body returned %d families; want 0", len(mfs))
	}
}

func TestDecodeOpenMetrics_MissingEOF(t *testing.T) {
	// OM spec requires a trailing "# EOF"; a missing trailer must be reported as
	// an error so the caller can decide whether to drop the upstream or fall
	// back. We still return any families parsed before the failure.
	body := "# TYPE foo counter\nfoo_total 1\n"
	mfs, err := decodeOpenMetricsToFamilies(bytes.NewReader([]byte(body)))
	if err == nil {
		t.Fatalf("expected error on missing # EOF, got nil")
	}
	if len(mfs) == 0 {
		t.Errorf("expected partial families to be returned on error")
	}
}

func TestDecodeOpenMetrics_MalformedSeriesError(t *testing.T) {
	body := "# TYPE foo gauge\nfoo not_a_number\n# EOF\n"
	_, err := decodeOpenMetricsToFamilies(bytes.NewReader([]byte(body)))
	if err == nil {
		t.Fatalf("expected parse error on non-numeric value, got nil")
	}
}
