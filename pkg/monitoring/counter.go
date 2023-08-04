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

type counter struct {
	baseMetric
	c api.Float64Counter
	// precomputedAddOption is just a precomputation to avoid allocations on each record call
	precomputedAddOption []api.AddOption
}

var _ Metric = &counter{}

func newCounter(o options) *counter {
	c, err := meter().Float64Counter(o.name,
		api.WithDescription(o.description),
		api.WithUnit(string(o.unit)))
	if err != nil {
		log.Fatalf("failed to create counter: %v", err)
	}
	r := &counter{c: c}
	r.baseMetric = baseMetric{
		name: o.name,
		rest: r,
	}
	return r
}

func (f *counter) Record(value float64) {
	f.runRecordHook(value)
	if f.precomputedAddOption != nil {
		f.c.Add(context.Background(), value, f.precomputedAddOption...)
	} else {
		f.c.Add(context.Background(), value)
	}
}

func (f *counter) With(labelValues ...LabelValue) Metric {
	attrs, set := rebuildAttributes(f.baseMetric, labelValues)
	nm := &counter{
		c:                    f.c,
		precomputedAddOption: []api.AddOption{api.WithAttributeSet(set)},
	}
	nm.baseMetric = baseMetric{
		name:  f.name,
		attrs: attrs,
		rest:  nm,
	}
	return nm
}
