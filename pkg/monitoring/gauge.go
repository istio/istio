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

	api "go.opentelemetry.io/otel/metric"

	"istio.io/istio/pkg/log"
)

type gauge struct {
	baseMetric
	g api.Float64Gauge
	// precomputedRecordOption is just a precomputation to avoid allocations on each record call
	precomputedRecordOption []api.RecordOption
}

var _ Metric = &gauge{}

func newGauge(o options) *gauge {
	g, err := meter().Float64Gauge(o.name,
		api.WithDescription(o.description),
		api.WithUnit(string(o.unit)))
	if err != nil {
		log.Fatalf("failed to create gauge: %v", err)
	}
	r := &gauge{g: g}
	r.baseMetric = baseMetric{
		name: o.name,
		rest: r,
	}
	return r
}

func (f *gauge) Record(value float64) {
	f.runRecordHook(value)
	f.g.Record(context.Background(), value, f.precomputedRecordOption...)
}

func (f *gauge) With(labelValues ...LabelValue) Metric {
	attrs, set := rebuildAttributes(f.baseMetric, labelValues)
	nm := &gauge{
		g:                       f.g,
		precomputedRecordOption: []api.RecordOption{api.WithAttributeSet(set)},
	}
	nm.baseMetric = baseMetric{
		name:  f.name,
		attrs: attrs,
		rest:  nm,
	}
	return nm
}
