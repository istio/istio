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

package statsd // import "istio.io/mixer/adapter/statsd"

import (
	"context"
	"fmt"
	"io/ioutil"
	"text/template"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/mixer/adapter/statsd2/config"
	"istio.io/mixer/pkg/adapter"
	pkgHndlr "istio.io/mixer/pkg/handler"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/template/metric"
)

const (
	defaultFlushBytes = 512
)

type (
	info struct {
		mtype config.Params_MetricInfo_Type
		tmpl  *template.Template
	}

	handler struct {
		rate      float32
		client    statsd.Statter
		templates map[string]info // metric name -> template
	}
)

func (h *handler) HandleMetric(_ context.Context, values []*metric.Instance) error {
	var result *multierror.Error
	for _, v := range values {
		if err := h.record(v); err != nil {
			result = multierror.Append(result, err)
		}
	}
	return result.ErrorOrNil()
}

func (h *handler) record(value *metric.Instance) error {
	mname := value.Name
	t, found := h.templates[mname]
	if !found {
		return fmt.Errorf("no info for metric named %s", value.Name)
	}
	if t.tmpl != nil {
		buf := pool.GetBuffer()
		// We don't check the error here because Execute should only fail when the template is invalid; since
		// we check that the templates are parsable in ValidateConfig and further check that they can be executed
		// with the metric's labels in NewMetricsAspect, this should never fail.
		_ = t.tmpl.Execute(buf, value.Dimensions)
		mname = buf.String()
		pool.PutBuffer(buf)
	}

	switch t.mtype {
	case config.GAUGE:
		v, ok := value.Value.(int64)
		if !ok {
			return fmt.Errorf("could not record counter '%s' expected int value, got %v", mname, value.Value)
		}
		return h.client.Gauge(mname, v, h.rate)
	case config.COUNTER:
		v, ok := value.Value.(int64)
		if !ok {
			return fmt.Errorf("could not record counter '%s' expected int value, got %v", mname, value.Value)
		}
		return h.client.Inc(mname, v, h.rate)
	case config.DISTRIBUTION:
		// TODO: figure out how to program histograms via config.*
		// updates
		v, ok := value.Value.(time.Duration)
		if ok {
			return h.client.TimingDuration(mname, v, h.rate)
		}
		// TODO: figure out support for non-duration distributions.
		vint, ok := value.Value.(int64)
		if ok {
			return h.client.Inc(mname, vint, h.rate)
		}
		return fmt.Errorf("could not record distribution '%s'; expected int or duration, got %v", mname, value.Value)
	default:
		return fmt.Errorf("unknown metric type '%v' for metric: %s", t.mtype, value.Name)
	}
}

func (h *handler) Close() error { return h.client.Close() }

////////////////// Config //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() pkgHndlr.Info {
	return pkgHndlr.Info{
		Name:        "statsd",
		Impl:        "istio.io/mixer/adapter/statsd",
		Description: "Produces statsd metrics",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		DefaultConfig: &config.Params{
			Address:       "localhost:8125",
			Prefix:        "",
			FlushDuration: 300 * time.Millisecond,
			FlushBytes:    512,
			SamplingRate:  1.0,
		},

		NewBuilder: func() adapter.Builder2 { return &builder{} },

		// TO BE DELETED
		CreateHandlerBuilder: func() adapter.HandlerBuilder { return &obuilder{&builder{}} },
		ValidateConfig:       func(cfg adapter.Config) *adapter.ConfigErrors { return nil },
	}
}

type builder struct {
	adapterConfig *config.Params
	metricTypes   map[string]*metric.Type
}

func (b *builder) SetMetricTypes(types map[string]*metric.Type) { b.metricTypes = types }
func (b *builder) SetAdapterConfig(cfg adapter.Config)          { b.adapterConfig = cfg.(*config.Params) }

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	ac := b.adapterConfig
	if ac.FlushDuration < 0 {
		ce = ce.Appendf("flushDuration", "flush duration must be >= 0")
	}
	if ac.FlushBytes < 0 {
		ce = ce.Appendf("flushBytes", "flush bytes must be >= 0")
	}
	if ac.SamplingRate < 0 {
		ce = ce.Appendf("samplingRate", "sampling rate must be >= 0")
	}
	for metricName, s := range ac.Metrics {
		if _, err := template.New(metricName).Parse(s.NameTemplate); err != nil {
			ce = ce.Appendf("metricNameTemplateStrings", "failed to parse template '%s' for metric '%s': %v", s, metricName, err)
		}
	}
	return
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	ac := b.adapterConfig

	flushBytes := int(ac.FlushBytes)
	if flushBytes <= 0 {
		env.Logger().Infof("Got FlushBytes of '%d', defaulting to '%d'", flushBytes, defaultFlushBytes)
		// the statsd impl we use defaults to 1432 byte UDP packets when flushBytes <= 0; we want to default to 512 so we check ourselves.
		flushBytes = defaultFlushBytes
	}

	client, _ := statsd.NewBufferedClient(ac.Address, ac.Prefix, ac.FlushDuration, flushBytes)

	templates := make(map[string]info)
	for metricName, s := range ac.Metrics {
		def, found := b.metricTypes[metricName]
		if !found {
			env.Logger().Infof("template registered for nonexistent metric '%s'", metricName)
			continue // we don't have a metric that corresponds to this template, skip processing it
		}

		var t *template.Template
		if s.NameTemplate != "" {
			t, _ = template.New(metricName).Parse(s.NameTemplate)
			if err := t.Execute(ioutil.Discard, def.Dimensions); err != nil {
				env.Logger().Warningf(
					"skipping custom statsd metric name for metric '%s', could not satisfy template '%s' with labels '%v': %v",
					metricName, s, def.Dimensions, err)
				continue
			}
		}
		templates[metricName] = info{mtype: s.Type, tmpl: t}
	}
	return &handler{ac.SamplingRate, client, templates}, nil
}

// EVERYTHING BELOW IS TO BE DELETED

type obuilder struct {
	b *builder
}

func (o *obuilder) Build(cfg adapter.Config, env adapter.Env) (adapter.Handler, error) {
	o.b.SetAdapterConfig(cfg)
	return o.b.Build(context.Background(), env)
}

// ConfigureMetricHandler is to be deleted
func (o *obuilder) ConfigureMetricHandler(types map[string]*metric.Type) error {
	o.b.SetMetricTypes(types)
	return nil
}
