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

//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -a mixer/adapter/signalfx/config/config.proto -x "-n signalfx -t metric"

import (
	"context"
	"fmt"
	"net/url"
	"time"

	me "github.com/hashicorp/go-multierror"
	"github.com/signalfx/golib/sfxclient"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/signalfx/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/tracespan"
)

type (
	builder struct {
		metricTypes map[string]*metric.Type
		config      *config.Params
	}

	handler struct {
		*metricshandler
		*tracinghandler
		cancel func()
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

var _ tracespan.HandlerBuilder = &builder{}
var _ tracespan.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	confsByName := make(map[string]*config.Params_MetricConfig, len(b.config.Metrics))
	for i := range b.config.Metrics {
		confsByName[b.config.Metrics[i].Name] = b.config.Metrics[i]
	}

	ctx, cancel := context.WithCancel(ctx)

	sink := sfxclient.NewHTTPSink()
	sink.AuthToken = b.config.AccessToken

	if b.config.IngestUrl != "" {
		sink.DatapointEndpoint = fmt.Sprintf("%s/v2/datapoint", b.config.IngestUrl)
		sink.TraceEndpoint = fmt.Sprintf("%s/v1/trace", b.config.IngestUrl)
	}

	h := &handler{
		cancel: cancel,
	}

	var resErr *me.Error

	if b.config.EnableMetrics && len(b.metricTypes) > 0 {
		h.metricshandler = &metricshandler{
			env:                env,
			ctx:                ctx,
			metricTypes:        b.metricTypes,
			sink:               sink,
			intervalSeconds:    uint32(b.config.DatapointInterval.Round(time.Second).Seconds()),
			metricConfigByName: confsByName,
		}

		if err := h.metricshandler.InitMetrics(); err != nil {
			resErr = me.Append(resErr, err)
		}
	}

	if b.config.EnableTracing {
		h.tracinghandler = &tracinghandler{
			sink: sink,
			env:  env,
			ctx:  ctx,
		}

		if err := h.tracinghandler.InitTracing(int(b.config.TracingBufferSize), b.config.TracingSampleProbability); err != nil {
			resErr = me.Append(resErr, err)
		}
	}

	return h, resErr.ErrorOrNil()
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.config = cfg.(*config.Params)
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if b.config.AccessToken == "" {
		ce = ce.Appendf("access_token", "You must specify the SignalFx access token")
	}

	if b.config.EnableMetrics && len(b.config.Metrics) == 0 {
		ce = ce.Appendf("metrics", "There must be at least one metric definition for this to be useful")
	}

	if _, err := url.Parse(b.config.IngestUrl); b.config.IngestUrl != "" && err != nil {
		ce = ce.Appendf("ingest_url", "Unable to parse url: "+err.Error())
	}

	if b.config.DatapointInterval.Round(time.Second) < 1*time.Second {
		ce = ce.Appendf("datapoint_interval", "Interval must not be less than one second")
	}

	typesSeen := map[string]bool{}

	for i := range b.config.Metrics {
		if b.config.Metrics[i].Type == config.NONE {
			ce = ce.Appendf(fmt.Sprintf("metrics[%d].type", i), "type must be specified")
		}

		name := b.config.Metrics[i].Name
		if len(name) == 0 {
			ce = ce.Appendf(fmt.Sprintf("metrics[%d].name", i), "name must not be blank")
			continue
		}

		if typ := b.metricTypes[name]; typ != nil {
			typesSeen[name] = true
			switch typ.Value {
			case v1beta1.INT64, v1beta1.DOUBLE, v1beta1.BOOL, v1beta1.TIMESTAMP, v1beta1.DURATION:
				break
			default:
				ce = ce.Appendf(fmt.Sprintf("metrics[%d]", i),
					"istio metric's value should be numeric but is %s", typ.Value.String())
			}
		} else {
			ce = ce.Appendf(fmt.Sprintf("metrics[%d].name", i),
				"Name %s does not correspond to a metric type registered in Istio and sent to this adapter", name)
		}
	}

	for name := range b.metricTypes {
		if !typesSeen[name] {
			ce = ce.Appendf("metrics", "istio metric type %s must be configured", name)
		}
	}

	if b.config.TracingSampleProbability < 0 || b.config.TracingSampleProbability > 1.0 {
		ce = ce.Appendf("tracing_sample_probability", "must be between 0.0 and 1.0 inclusive")
	}

	if b.config.TracingBufferSize <= 0 {
		ce = ce.Appendf("tracing_buffer_size", "must be greater than 0")
	}

	return
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

// tracespan.HandlerBuilder#SetTraceSpanTypes
func (b *builder) SetTraceSpanTypes(entries map[string]*tracespan.Type) {
	// We don't really care what the spans look like since we just convert
	// everything to a string using adapter.Stringify, so we have no reason to
	// store tracespan.Types.
}

////////////////// Request-time Methods //////////////////////////

// adapter.Handler#Close
func (h *handler) Close() error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

////////////////// Bootstrap //////////////////////////

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "signalfx",
		Description: "Sends metrics and traces to SignalFx",
		SupportedTemplates: []string{
			metric.TemplateName,
			tracespan.TemplateName,
		},
		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{
			EnableMetrics:            true,
			EnableTracing:            true,
			DatapointInterval:        10 * time.Second,
			TracingBufferSize:        1000,
			TracingSampleProbability: 1.0,
		},
	}
}
