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

package circonus

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	cgm "github.com/circonus-labs/circonus-gometrics"
	"github.com/circonus-labs/circonus-gometrics/checkmgr"

	"istio.io/istio/mixer/adapter/circonus/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

type (
	builder struct {
		adpCfg      *config.Params
		metricTypes map[string]*metric.Type
	}

	handler struct {
		cm            cgm.CirconusMetrics
		env           adapter.Env
		metrics       map[string]config.Params_MetricInfo_Type
		lastFlushed   time.Time
		flushLock     sync.Mutex
		flushInterval time.Duration
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

// bridge stdlog to env.Logger()
type logToEnvLogger struct {
	env adapter.Env
}

// Build constructs a circonus-gometrics instance and sets up the handler
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {

	// register cgm.Flush() with env.ScheduleWork() to wrap goroutines
	var cm *cgm.CirconusMetrics
	ec := make(chan error, 1)

	env.ScheduleWork(
		func() {

			bridge := &logToEnvLogger{env: env}

			cmc := &cgm.Config{
				CheckManager: checkmgr.Config{
					Check: checkmgr.CheckConfig{
						SubmissionURL: b.adpCfg.SubmissionUrl,
					},
				},
				Log:      log.New(bridge, "", 0),
				Debug:    true, // enable [DEBUG] level logging for env.Logger
				Interval: "0s", // do not autoflush
			}

			var err error
			cm, err = cgm.NewCirconusMetrics(cmc)
			ec <- err
		})

	select {
	case err := <-ec:
		if err != nil {
			env.Logger().Errorf("Error instantiating cgm: %v", err)
			return nil, err
		}
	}

	// wait until cgm background tasks initialized under ScheduleWork have completed
	for !cm.Ready() {
		time.Sleep(1 * time.Second)
	}

	metrics := make(map[string]config.Params_MetricInfo_Type)
	ac := b.adpCfg
	for _, adpMetric := range ac.Metrics {
		metrics[adpMetric.Name] = adpMetric.Type
	}

	lastFlushed := time.Now()
	flushInterval := b.adpCfg.SubmissionInterval

	return &handler{cm: *cm, env: env, metrics: metrics, lastFlushed: lastFlushed, flushInterval: flushInterval}, nil
}

// SetAdapterConfig assigns operator configuration to the builder
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adpCfg = cfg.(*config.Params)
}

// Validate checks the SubmissionUrl parameter is well formed, and needed metrics are declared
func (b *builder) Validate() (ce *adapter.ConfigErrors) {

	// verify the submission url is well formed
	if _, err := url.ParseRequestURI(b.adpCfg.SubmissionUrl); err != nil {
		ce = ce.Append("submission_url", err)
	}

	// put the metric config into a map for use below
	configMetrics := make(map[string]config.Params_MetricInfo_Type)
	for _, configMetric := range b.adpCfg.Metrics {
		configMetrics[configMetric.Name] = configMetric.Type
	}

	// verify there are no metric types without a corresponding config item
	for metricName := range b.metricTypes {
		if _, ok := configMetrics[metricName]; !ok {
			err := fmt.Errorf("missing metric configuration %v", metricName)
			ce = ce.Append("metrics", err)
		}
	}

	// verify there are no config items without a corresponding metric type
	for configMetric := range configMetrics {
		if _, ok := b.metricTypes[configMetric]; !ok {
			err := fmt.Errorf("missing metric type for %v", configMetric)
			ce = ce.Append("metrics", err)
		}
	}

	// because SubmissionInterval is a duration type, validation errors will be caught before this point

	return
}

// SetMetricTypes sets the available metric types in the builder
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

// HandleMetric submits metrics to Circonus via circonus-gometrics
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {

	for _, inst := range insts {

		metricName := inst.Name
		metricType := h.metrics[metricName]

		switch metricType {

		case config.GAUGE:
			value, _ := inst.Value.(int64)
			h.cm.Gauge(metricName, value)

		case config.COUNTER:
			h.cm.Increment(metricName)

		case config.DISTRIBUTION:
			value, _ := inst.Value.(time.Duration)
			h.cm.Timing(metricName, float64(value))
		}

	}

	// see if we should flush
	h.flushLock.Lock()
	if time.Now().After(h.lastFlushed.Add(h.flushInterval)) {
		h.env.ScheduleWork(h.cm.Flush)
		h.lastFlushed = time.Now()
	}
	h.flushLock.Unlock()

	return nil
}

// Close flushes any buffered metricsl
func (h *handler) Close() error {
	h.cm.Flush()
	return nil
}

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "circonus",
		Description: "Emit metrics to Circonus.com monitoring endpoint",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{SubmissionUrl: "", SubmissionInterval: 10 * time.Second},
	}
}

// logToEnvLogger converts CGM log package writes to env.Logger()
func (b logToEnvLogger) Write(msg []byte) (int, error) {
	if bytes.HasPrefix(msg, []byte("[ERROR]")) {
		b.env.Logger().Infof(string(msg))
	} else if bytes.HasPrefix(msg, []byte("[WARN]")) {
		b.env.Logger().Warningf(string(msg))
	} else if bytes.HasPrefix(msg, []byte("[DEBUG]")) {
		b.env.Logger().Errorf(string(msg))
	} else {
		b.env.Logger().Infof(string(msg))
	}
	return len(msg), nil
}
