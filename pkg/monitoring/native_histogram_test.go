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

package monitoring

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// nativeTestDistribution is defined at package level because metrics must be
// registered before any exporter starts.
var nativeTestDistribution = NewDistribution(
	"native_test_duration_seconds",
	"Distribution used to test native histogram export",
	[]float64{0.01, 0.1, 1},
)

// gatherTestHistogram records observations into a meter provider built with opts
// and returns the exported histogram for nativeTestDistribution. It uses its own
// provider so the global one other tests rely on is left alone.
func gatherTestHistogram(t *testing.T, opts ...ExporterOption) *dto.Histogram {
	t.Helper()
	reg := prometheus.NewRegistry()
	mp, err := newMeterProvider(reg, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	h, err := mp.Meter("istio").Float64Histogram(nativeTestDistribution.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []float64{0.003, 0.02, 0.4, 3} {
		h.Record(context.Background(), v)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() == nativeTestDistribution.Name() {
			return mf.GetMetric()[0].GetHistogram()
		}
	}
	t.Fatalf("%s not exported", nativeTestDistribution.Name())
	return nil
}

func TestNativeHistograms(t *testing.T) {
	classic := gatherTestHistogram(t)
	if len(classic.GetBucket()) != 3 || classic.Schema != nil {
		t.Errorf("default export should keep the explicit buckets: %v", classic)
	}

	native := gatherTestHistogram(t, WithNativeHistograms())
	if native.Schema == nil || native.GetSchema() > 8 {
		t.Errorf("native export schema = %v; want a Prometheus native schema (<= 8)", native.Schema)
	}
	if len(native.GetPositiveSpan()) == 0 || len(native.GetBucket()) != 0 {
		t.Errorf("native export should carry sparse buckets only: %v", native)
	}
	if native.GetSampleCount() != 4 {
		t.Errorf("sample count = %d; want 4", native.GetSampleCount())
	}
}
