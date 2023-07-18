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
	"sync"

	"go.opentelemetry.io/otel/attribute"
	api "go.opentelemetry.io/otel/metric"

	"istio.io/istio/pkg/log"
)

type gauge struct {
	baseMetric
	g api.Float64ObservableGauge

	// attributeSets stores a map of attributes -> values, for gauges.
	attributeSetsMutex *sync.RWMutex
	attributeSets      map[attribute.Set]*gaugeValues
	currentGaugeSet    *gaugeValues
}

var _ Metric = &gauge{}

func newGauge(o options) *gauge {
	r := &gauge{
		attributeSetsMutex: &sync.RWMutex{},
		currentGaugeSet:    &gaugeValues{},
	}
	r.attributeSets = map[attribute.Set]*gaugeValues{
		attribute.NewSet(): r.currentGaugeSet,
	}
	g, err := meter().Float64ObservableGauge(o.name,
		api.WithFloat64Callback(func(ctx context.Context, observer api.Float64Observer) error {
			r.attributeSetsMutex.Lock()
			defer r.attributeSetsMutex.Unlock()
			for _, gv := range r.attributeSets {
				observer.Observe(gv.val, gv.opt...)
			}
			return nil
		}),
		api.WithDescription(o.description),
		api.WithUnit(string(o.unit)))
	if err != nil {
		log.Fatalf("failed to create gauge: %v", err)
	}
	r.g = g
	r.baseMetric = baseMetric{
		name: o.name,
		rest: r,
	}
	return r
}

func (f *gauge) Record(value float64) {
	f.runRecordHook(value)
	// TODO: https://github.com/open-telemetry/opentelemetry-specification/issues/2318 use synchronous gauge so we don't need to deal with this
	f.attributeSetsMutex.Lock()
	f.currentGaugeSet.val = value
	f.attributeSetsMutex.Unlock()
}

func (f *gauge) With(labelValues ...LabelValue) Metric {
	attrs, set := rebuildAttributes(f.baseMetric, labelValues)
	nm := &gauge{
		g:                  f.g,
		attributeSetsMutex: f.attributeSetsMutex,
		attributeSets:      f.attributeSets,
	}
	if _, f := nm.attributeSets[set]; !f {
		nm.attributeSets[set] = &gaugeValues{
			opt: []api.ObserveOption{api.WithAttributeSet(set)},
		}
	}
	nm.currentGaugeSet = nm.attributeSets[set]
	nm.baseMetric = baseMetric{
		name:  f.name,
		attrs: attrs,
		rest:  nm,
	}
	return nm
}

type gaugeValues struct {
	val float64
	opt []api.ObserveOption
}
