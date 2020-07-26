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
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/solarwinds/config/config.proto -x "-n solarwind -t logentry -t metric -d example"

// Package solarwinds publishes metric and log values collected by Mixer
// to appoptics and papertrail respectively.
package solarwinds

import (
	"context"
	"fmt"
	"regexp"

	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/adapter/solarwinds/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
)

type (
	builder struct {
		cfg           *config.Params
		metricTypes   map[string]*metric.Type
		logentryTypes map[string]*logentry.Type
	}

	handler struct {
		logger         adapter.Logger
		metricsHandler metricsHandlerInterface
		logHandler     logHandlerInterface
	}
)

const paperTrailURLPattern = `logs\d+.papertrailapp.com\:\d{3,5}`

var (
	_ metric.HandlerBuilder = &builder{}
	_ metric.Handler        = &handler{}
)

// GetInfo returns the Info associated with this adapter.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("solarwinds")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}

func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	// this is a common adapter config for both log and metric
	b.cfg = cfg.(*config.Params)
}

func (b *builder) SetMetricTypes(mts map[string]*metric.Type) {
	b.metricTypes = mts
}

func (b *builder) SetLogEntryTypes(entries map[string]*logentry.Type) {
	b.logentryTypes = entries
}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if b.cfg.AppopticsBatchSize < 0 || b.cfg.AppopticsBatchSize > 1000 {
		ce = ce.Append("appoptics_batch_size", fmt.Errorf("appoptics batch size provided is not in the range from 1 to 1000"))
	}
	if b.cfg.PapertrailUrl != "" {
		if re := regexp.MustCompile(paperTrailURLPattern); !re.MatchString(b.cfg.PapertrailUrl) {
			ce = ce.Append("paper_trail_url",
				fmt.Errorf("papertrail url provided is invalid: %v. It did not match the pattern: %s",
					b.cfg.PapertrailUrl, paperTrailURLPattern))
		}
	}

	for instName, inst := range b.cfg.Metrics {
		mInst, ok := b.metricTypes[instName]
		if !ok {
			ce = ce.Append("metrics", fmt.Errorf("%s is an invalid metric instance name", instName))
		} else {
			for _, label := range inst.LabelNames {
				if _, ok := mInst.Dimensions[label]; !ok {
					ce = ce.Append("metric label", fmt.Errorf("%s is an invalid metric label name", label))
				}
			}
		}
	}

	for inst := range b.cfg.Logs {
		if _, ok := b.logentryTypes[inst]; !ok {
			ce = ce.Append("metrics", fmt.Errorf("%s is an invalid logentry instance name", inst))
		}
	}
	return
}

func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	logger := env.Logger()

	m := newMetricsHandler(env, b.cfg)
	l, err := newLogHandler(env, b.cfg)
	if err != nil {
		_ = m.close()
		return nil, err
	}
	return &handler{
		metricsHandler: m,
		logHandler:     l,
		logger:         logger,
	}, nil
}

func (h *handler) HandleMetric(ctx context.Context, vals []*metric.Instance) error {
	return h.metricsHandler.handleMetric(ctx, vals)
}

func (h *handler) HandleLogEntry(ctx context.Context, values []*logentry.Instance) error {
	return h.logHandler.handleLogEntry(ctx, values)
}

func (h *handler) Close() error {
	if h.metricsHandler != nil {
		_ = h.metricsHandler.close()
	}
	if h.logHandler != nil {
		_ = h.logHandler.close()
	}
	return nil
}
