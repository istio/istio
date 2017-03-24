// Copyright 2017 Istio Authors
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

package statsd

import (
	"fmt"
	"io/ioutil"
	"text/template"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/mixer/adapter/statsd/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/pool"
)

const (
	defaultFlushBytes = 512
)

type (
	builder struct {
		adapter.DefaultBuilder
	}

	aspect struct {
		rate      float32
		client    statsd.Statter
		templates map[string]*template.Template // metric name -> template
	}
)

var (
	name        = "statsd"
	desc        = "Pushes statsd metrics"
	defaultConf = &config.Params{
		Address:                   "localhost:8125",
		Prefix:                    "",
		FlushDuration:             &types.Duration{Nanos: int32(300 * time.Millisecond)},
		FlushBytes:                512,
		SamplingRate:              1.0,
		MetricNameTemplateStrings: make(map[string]string),
	}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterMetricsBuilder(newBuilder())
}

func newBuilder() *builder {
	return &builder{adapter.NewDefaultBuilder(name, desc, defaultConf)}
}

func (b *builder) ValidateConfig(c adapter.Config) (ce *adapter.ConfigErrors) {
	params := c.(*config.Params)
	flushDuration, err := types.DurationFromProto(params.FlushDuration)
	if err != nil {
		ce = ce.Append("FlushDuration", err)
	}
	if flushDuration < time.Duration(0) {
		ce = ce.Appendf("FlushDuration", "flush duration must be >= 0")
	}
	if params.FlushBytes < 0 {
		ce = ce.Appendf("FlushBytes", "flush bytes must be >= 0")
	}
	if params.SamplingRate < 0 {
		ce = ce.Appendf("SamplingRate", "sampling rate must be >= 0")
	}
	for metricName, s := range params.MetricNameTemplateStrings {
		if _, err := template.New(metricName).Parse(s); err != nil {
			ce = ce.Appendf("MetricNameTemplateStrings", "failed to parse template '%s' for metric '%s' with err: %s", s, metricName, err)
		}
	}
	return
}

func (*builder) NewMetricsAspect(env adapter.Env, cfg adapter.Config, metrics map[string]*adapter.MetricDefinition) (adapter.MetricsAspect, error) {
	params := cfg.(*config.Params)

	flushBytes := int(params.FlushBytes)
	if flushBytes <= 0 {
		env.Logger().Infof("Got FlushBytes of '%d', defaulting to '%d'", flushBytes, defaultFlushBytes)
		// the statsd impl we use defaults to 1432 byte UDP packets when flushBytes <= 0; we want to default to 512 so we check ourselves.
		flushBytes = defaultFlushBytes
	}

	flushDuration, _ := types.DurationFromProto(params.FlushDuration)
	client, _ := statsd.NewBufferedClient(params.Address, params.Prefix, flushDuration, flushBytes)

	templates := make(map[string]*template.Template)
	for metricName, s := range params.MetricNameTemplateStrings {
		def, found := metrics[metricName]
		if !found {
			env.Logger().Infof("template registered for nonexistent metric '%s'", metricName)
			continue // we don't have a metric that corresponds to this template, skip processing it
		}

		t, _ := template.New(metricName).Parse(s)
		if err := t.Execute(ioutil.Discard, def.Labels); err != nil {
			env.Logger().Warningf(
				"skipping custom statsd metric name for metric '%s', could not satisfy template '%s' with labels '%v' with err: %s",
				metricName, s, def.Labels, err)
			continue
		}
		templates[metricName] = t
	}
	return &aspect{params.SamplingRate, client, templates}, nil
}

func (a *aspect) Record(values []adapter.Value) error {
	var result *multierror.Error
	for _, v := range values {
		if err := a.record(v); err != nil {
			result = multierror.Append(result, err)
		}
	}
	return result.ErrorOrNil()
}

func (a *aspect) record(value adapter.Value) error {
	mname := value.Definition.Name
	if t, found := a.templates[mname]; found {
		buf := pool.GetBuffer()

		// We don't check the error here because Execute should only fail when the template is invalid; since
		// we check that the templates are parsable in ValidateConfig and further check that they can be executed
		// with the metric's labels in NewMetricsAspect, this should never fail.
		_ = t.Execute(buf, value.Labels)
		mname = buf.String()

		buf.Reset()
		pool.PutBuffer(buf)
	}

	var result error
	switch value.Definition.Kind {
	case adapter.Gauge:
		v, err := value.Int64()
		if err != nil {
			return fmt.Errorf("could not record gauge '%s' with err: %s", mname, err)
		}
		result = a.client.Gauge(mname, v, a.rate)

	case adapter.Counter:
		v, err := value.Int64()
		if err != nil {
			return fmt.Errorf("could not record counter '%s' with err: %s", mname, err)
		}
		result = a.client.Inc(mname, v, a.rate)
	}

	return result
}

func (a *aspect) Close() error { return a.client.Close() }
