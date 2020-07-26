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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/dogstatsd/config/config.proto -x "-n dogstatsd -t metric -d example"

package dogstatsd

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	multierror "github.com/hashicorp/go-multierror"

	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/dogstatsd/config"
	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

type builder struct {
	adapterConfig *config.Params
	metricTypes   map[string]*metric.Type
}
type handler struct {
	rate    float64
	client  *statsd.Client
	metrics map[string]*config.Params_MetricInfo
}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(_ context.Context, env adapter.Env) (adapter.Handler, error) {
	ac := b.adapterConfig

	var client = &statsd.Client{}
	var err error
	if ac.BufferLength > 0 {
		client, err = statsd.NewBuffered(ac.Address, int(ac.BufferLength))
	} else {
		client, err = statsd.New(ac.Address)
	}
	if err != nil {
		return nil, env.Logger().Errorf("Unable to create dogstatsd client: %v", err)
	}

	if !strings.HasSuffix(ac.Prefix, ".") {
		client.Namespace = ac.Prefix + "."
	} else {
		client.Namespace = ac.Prefix
	}
	client.Tags = flattenTags(ac.GlobalTags)

	// Create an empty map if tags aren't provided
	for _, m := range ac.Metrics {
		if m.Tags == nil {
			m.Tags = map[string]string{}
		}
	}
	return &handler{ac.SampleRate, client, ac.Metrics}, nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) { b.adapterConfig = cfg.(*config.Params) }

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	ac := b.adapterConfig

	// Validate the buffer is a valid size
	if ac.BufferLength < 0 {
		ce = ce.Appendf("bufferLength", "buffer length must be >= 0")
	}

	// Validate the sample rate is a valid percentage value
	if ac.SampleRate < 0 || ac.SampleRate > 1 {
		ce = ce.Appendf("sampleRate", "sampling rate must be between 0 and 1")
	}

	// Validate the address of the agent is valid
	if ac.Address == "" {
		ce = ce.Appendf("address", "Address is empty")
	}
	if _, _, err := net.SplitHostPort(ac.Address); err != nil {
		ce = ce.Appendf("address", "Address is malformed: %v", err)
	}

	// Validate the adapter handles all metrics it is being sent
	for mname := range b.metricTypes {
		if _, found := ac.Metrics[mname]; !found {
			ce = ce.Appendf("metricName", "%s is a valid metric but is not configured to be handled by the datadog adapter", mname)
		}
	}

	// Validate metrics that will be handled are of the right type and that they exist
	for mname, metric := range ac.Metrics {
		m, found := b.metricTypes[mname]
		if !found {
			ce = ce.Appendf("metricType", "%s is not a valid metric that can be emitted", mname)
		} else {
			val := m.Value
			switch metric.Type {
			case config.COUNTER, config.GAUGE:
				if val != descriptor.INT64 {
					ce = ce.Appendf("metricType", "Counters and Gauges must be an Int64 but metric %s is a %s", mname, val.String())
				}
			case config.DISTRIBUTION:
				if val != descriptor.DURATION && val != descriptor.INT64 {
					ce = ce.Appendf("metricType", "Histograms must be either a Duration or an Int64 but metric %s is a %s", mname, val.String())
				}
			default:
				ce = ce.Appendf("metricType", "%s is not a supported metric type for this adapter.", val.String())
			}
		}
	}
	return ce
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

////////////////// Request-time Methods //////////////////////////
// metric.Handler#HandleMetric
func (h *handler) HandleMetric(_ context.Context, insts []*metric.Instance) error {
	var result *multierror.Error
	for _, inst := range insts {
		if err := h.record(inst); err != nil {
			result = multierror.Append(result, err)
		}
	}
	return result.ErrorOrNil()
}

func (h *handler) record(value *metric.Instance) error {
	mname := value.Name

	// Shouldn't fail since validation checks metric creation
	info := h.metrics[mname]

	tagMap := make(map[string]string)
	for k, v := range h.metrics[mname].Tags {
		tagMap[k] = v
	}
	// Add dimensions to tags
	for k, v := range value.Dimensions {
		tagMap[k] = adapter.Stringify(v)
	}

	// Take the tag map and return a tag string that dogstatsd expects
	tags := flattenTags(tagMap)

	// Emit the metric based on its config type
	switch info.Type {
	case config.COUNTER:
		return h.client.Count(info.Name, value.Value.(int64), tags, h.rate)
	case config.GAUGE:
		return h.client.Gauge(info.Name, float64(value.Value.(int64)), tags, h.rate)
	case config.DISTRIBUTION:
		v, ok := value.Value.(time.Duration)
		if ok {
			return h.client.Timing(info.Name, v, tags, h.rate)
		}
		vint, ok := value.Value.(int64)
		if ok {
			return h.client.Histogram(info.Name, float64(vint), tags, h.rate)
		}
	}
	return fmt.Errorf("unknown metric type '%v' for metric: %s", info.Type, mname)
}

func flattenTags(tagMap map[string]string) []string {
	tags := []string{}
	for key, val := range tagMap {
		tags = append(tags, strings.Join([]string{key, val}, ":"))
	}
	return tags
}

// adapter.Handler#Close
func (h *handler) Close() error { return h.client.Close() }

////////////////// Bootstrap //////////////////////////

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("dogstatsd")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}
