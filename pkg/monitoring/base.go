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
	"go.opentelemetry.io/otel/attribute"

	"istio.io/istio/pkg/slices"
)

type baseMetric struct {
	name string
	// attrs stores all attrs for the metrics
	attrs []attribute.KeyValue
	rest  Metric
}

func (f baseMetric) Name() string {
	return f.name
}

func (f baseMetric) Increment() {
	f.rest.Record(1)
}

func (f baseMetric) Decrement() {
	f.rest.Record(-1)
}

func (f baseMetric) runRecordHook(value float64) {
	recordHookMutex.RLock()
	if rh, ok := recordHooks[f.name]; ok {
		lv := slices.Map(f.attrs, func(e attribute.KeyValue) LabelValue {
			return LabelValue{e}
		})
		rh.OnRecord(f.name, lv, value)
	}
	recordHookMutex.RUnlock()
}

func (f baseMetric) Register() error {
	return nil
}

func (f baseMetric) RecordInt(value int64) {
	f.rest.Record(float64(value))
}

func rebuildAttributes(bm baseMetric, labelValues []LabelValue) ([]attribute.KeyValue, attribute.Set) {
	attrs := make([]attribute.KeyValue, 0, len(bm.attrs)+len(labelValues))
	attrs = append(attrs, bm.attrs...)
	for _, v := range labelValues {
		attrs = append(attrs, v.keyValue)
	}

	set := attribute.NewSet(attrs...)
	return attrs, set
}
