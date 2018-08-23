// Copyright 2018 Istio Authors
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

package signalfx

import (
	"errors"
	"time"

	"istio.io/istio/mixer/adapter/signalfx/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

func (h *handler) processMetric(conf *config.Params_MetricConfig, inst *metric.Instance) error {
	name := inst.Name
	dims := sfxDimsForInstance(inst)

	val, err := valueToFloat(inst.Value)
	if err != nil {
		return err
	}

	switch conf.Type {
	case config.COUNTER:
		cu := h.registry.RegisterOrGetCumulative(name, dims)
		cu.Add(int64(val))
	case config.HISTOGRAM:
		rb := h.registry.RegisterOrGetRollingBucket(name, dims, h.intervalSeconds)
		rb.Add(val)
	}
	return nil
}

func valueToFloat(val interface{}) (float64, error) {
	if val == nil {
		return 0.0, errors.New("nil value received")
	}

	switch v := val.(type) {
	case int64:
		return float64(v), nil
	case float64:
		return v, nil
	case bool:
		if v {
			return float64(1), nil
		}
		return float64(0), nil
	case time.Time:
		return float64(v.Unix()), nil
	case time.Duration:
		return float64(v), nil
	default:
		return 0.0, errors.New("unsupported value type")
	}
}

func sfxDimsForInstance(inst *metric.Instance) map[string]string {
	dims := map[string]string{}

	for key, val := range inst.Dimensions {
		dims[key] = adapter.Stringify(val)
	}

	if inst.MonitoredResourceType != "" {
		dims["monitored_resource_type"] = inst.MonitoredResourceType
	}

	for key, val := range inst.MonitoredResourceDimensions {
		dims["monitored_resource_"+key] = adapter.Stringify(val)
	}

	return dims
}
