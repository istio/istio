// Copyright 2019 Istio Authors
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
	"go.opencensus.io/metric"
	"go.opencensus.io/metric/metricdata"

	"istio.io/istio/pkg/log"
)

// NewDerivedGauge creates a new Metric with an aggregation type of LastValue that generates the value
// dynamically according to the provided function. This can be used for values based on querying some
// state within a system (when event-driven recording is not appropriate).
func NewDerivedGauge(name, description string, opts ...DerivedOptions) DerivedMetric {
	options := createDerivedOptions(opts...)
	m, err := derivedRegistry.AddFloat64DerivedGauge(name,
		metric.WithDescription(description),
		metric.WithLabelKeys(options.labelKeys...),
		metric.WithUnit(metricdata.UnitDimensionless)) // TODO: allow unit in options
	if err != nil {
		log.Warnf("failed to add metric %q: %v", name, err)
	}
	derived := &derivedFloat64Metric{
		base: m,
		name: name,
	}
	if options.valueFn != nil {
		derived.ValueFrom(options.valueFn)
	}
	return derived
}

var _ DerivedMetric = &derivedFloat64Metric{}

type derivedFloat64Metric struct {
	base *metric.Float64DerivedGauge

	name string
}

func (d *derivedFloat64Metric) Name() string {
	return d.name
}

// no-op
func (d *derivedFloat64Metric) Register() error {
	return nil
}

func (d *derivedFloat64Metric) ValueFrom(valueFn func() float64, labelValues ...string) {
	if len(labelValues) == 0 {
		if err := d.base.UpsertEntry(valueFn); err != nil {
			log.Errorf("failed to add value for derived metric %q: %v", d.name, err)
		}
		return
	}
	lv := make([]metricdata.LabelValue, 0, len(labelValues))
	for _, l := range labelValues {
		lv = append(lv, metricdata.NewLabelValue(l))
	}
	if err := d.base.UpsertEntry(valueFn, lv...); err != nil {
		log.Errorf("failed to add value for derived metric %q: %v", d.name, err)
	}
}
