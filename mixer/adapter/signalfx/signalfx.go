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

//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/signalfx/config/config.proto

import (
	"context"
	"fmt"
	"time"

	"github.com/signalfx/golib/sfxclient"

	"istio.io/istio/mixer/adapter/signalfx/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

type (
	builder struct {
		metricTypes map[string]*metric.Type
		config      *config.Params
	}

	handler struct {
		env                adapter.Env
		ctx                context.Context
		cancel             func()
		scheduler          *sfxclient.Scheduler
		registry           *registry
		metricTypes        map[string]*metric.Type
		ingestURL          string
		accessToken        string
		intervalSeconds    uint32
		metricConfigByName map[string]*config.Params_MetricConfig
		logger             adapter.Logger
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	confsByName := make(map[string]*config.Params_MetricConfig, len(b.config.Metrics))
	for i := range b.config.Metrics {
		confsByName[b.config.Metrics[i].Name] = b.config.Metrics[i]
	}

	env.Logger().Infof("Building SignalFx adapter")

	h := &handler{
		env:                env,
		metricTypes:        b.metricTypes,
		ingestURL:          b.config.IngestUrl,
		accessToken:        b.config.AccessToken,
		intervalSeconds:    b.config.IntervalSeconds,
		metricConfigByName: confsByName,
		logger:             env.Logger(),
	}

	return h, h.Init()
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.config = cfg.(*config.Params)

	if b.config.IntervalSeconds == 0 {
		b.config.IntervalSeconds = 10
	}
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if b.config.AccessToken == "" {
		ce = ce.Appendf("access_token", "You must specify the SignalFx access token")
	}
	if len(b.config.Metrics) == 0 {
		ce = ce.Appendf("metrics", "There must be at least one metric definition for this to be useful")
	}

	for i := range b.config.Metrics {
		if b.config.Metrics[i].Type == config.NONE {
			ce = ce.Appendf(fmt.Sprintf("metrics[%d].type", i), "type must be specified")
		}

		name := b.config.Metrics[i].Name
		if len(name) == 0 {
			ce = ce.Appendf(fmt.Sprintf("metrics[%d].name", i), "name must not be blank")
			continue
		}

		if typ := b.metricTypes[name]; typ == nil {
			ce = ce.Appendf(fmt.Sprintf("metrics[%d].name", i),
				"Name %s does not correspond to a metric type registered in Istio", name)
		}
	}

	return
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

////////////////// Request-time Methods //////////////////////////

func (h *handler) Init() error {
	h.scheduler = sfxclient.NewScheduler()

	h.scheduler.ReportingDelay(time.Duration(h.intervalSeconds) * time.Second)
	h.scheduler.Sink.(*sfxclient.HTTPSink).AuthToken = h.accessToken

	h.scheduler.ErrorHandler = func(err error) error {
		return h.logger.Errorf("Error sending datapoints to SignalFx: %s", err.Error())
	}

	if h.ingestURL != "" {
		h.scheduler.Sink.(*sfxclient.HTTPSink).DatapointEndpoint = fmt.Sprintf("%s/v2/datapoint", h.ingestURL)
	}

	h.ctx, h.cancel = context.WithCancel(context.Background())

	h.registry = newRegistry(5 * time.Minute)
	h.scheduler.AddCallback(h.registry)

	h.env.ScheduleDaemon(func() {
		err := h.scheduler.Schedule(h.ctx)
		if err != nil {
			h.logger.Infof("SignalFx scheduler shutdown with result %s", err.Error())
		}
	})
	return nil
}

// metric.Handler#HandleMetric
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	for i := range insts {
		name := insts[i].Name
		if conf := h.metricConfigByName[name]; conf != nil {
			if err := h.processMetric(conf, insts[i]); err != nil {
				return err
			}
		}
	}

	return nil
}

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
		Description: "Sends metrics to SignalFx",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{},
	}
}
