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
	"istio.io/istio/pkg/slices"
)

type derivedGauge struct {
	mu    sync.RWMutex
	attrs map[attribute.Set]func() float64

	name string
}

var _ DerivedMetric = &derivedGauge{}

func newDerivedGauge(name, description string) DerivedMetric {
	dm := &derivedGauge{
		name:  name,
		attrs: map[attribute.Set]func() float64{},
	}
	_, err := meter().Float64ObservableGauge(name,
		api.WithDescription(description),
		api.WithFloat64Callback(func(ctx context.Context, observer api.Float64Observer) error {
			dm.mu.RLock()
			defer dm.mu.RUnlock()
			for kv, compute := range dm.attrs {
				observer.Observe(compute(), api.WithAttributeSet(kv))
			}
			return nil
		}))
	if err != nil {
		log.Fatalf("failed to create derived gauge: %v", err)
	}
	return dm
}

func (d *derivedGauge) Name() string {
	return d.name
}

func (d *derivedGauge) Register() error {
	return nil
}

func (d *derivedGauge) ValueFrom(valueFn func() float64, labelValues ...LabelValue) DerivedMetric {
	d.mu.Lock()
	defer d.mu.Unlock()
	lv := slices.Map(labelValues, func(e LabelValue) attribute.KeyValue {
		return e.keyValue
	})
	as := attribute.NewSet(lv...)
	d.attrs[as] = valueFn
	return d
}
