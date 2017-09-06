// Copyright 2017 the Istio Authors.
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

package stackdriver

import (
	"context"

	"github.com/hashicorp/go-multierror"

	"istio.io/mixer/adapter/stackdriver/config"
	"istio.io/mixer/adapter/stackdriver/log"
	sdmetric "istio.io/mixer/adapter/stackdriver/metric"
	"istio.io/mixer/pkg/adapter"
	handlers "istio.io/mixer/pkg/handler"
	"istio.io/mixer/template/logentry"
	"istio.io/mixer/template/metric"
)

type (
	builder struct {
		m metric.HandlerBuilder
		l logentry.HandlerBuilder
	}

	handler struct {
		m metric.Handler
		l logentry.Handler
	}
)

var (
	_ metric.HandlerBuilder = &builder{}
	_ metric.Handler        = &handler{}

	_ logentry.HandlerBuilder = &builder{}
	_ logentry.Handler        = &handler{}
)

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() handlers.Info {
	return handlers.Info{
		Name:        "stackdriver",
		Impl:        "istio.io/mixer/adapte/stackdriver",
		Description: "Publishes StackDriver metrics and logs.",
		SupportedTemplates: []string{
			metric.TemplateName,
			logentry.TemplateName,
		},
		CreateHandlerBuilder: func() adapter.HandlerBuilder { return &builder{m: sdmetric.NewBuilder(), l: log.NewBuilder()} },
		DefaultConfig:        &config.Params{},
		ValidateConfig:       func(msg adapter.Config) *adapter.ConfigErrors { return nil },
	}
}

func (b *builder) ConfigureMetricHandler(metrics map[string]*metric.Type) error {
	return b.m.ConfigureMetricHandler(metrics)
}

func (b *builder) ConfigureLogEntryHandler(entries map[string]*logentry.Type) error {
	return b.l.ConfigureLogEntryHandler(entries)
}

func (b *builder) Build(c adapter.Config, env adapter.Env) (adapter.Handler, error) {
	m, err := b.m.Build(c, env)
	if err != nil {
		return nil, err
	}
	mh, _ := m.(metric.Handler)

	l, err := b.l.Build(c, env)
	if err != nil {
		return nil, err
	}
	lh, _ := l.(logentry.Handler)

	return &handler{m: mh, l: lh}, nil
}

func (h *handler) Close() error {
	return multierror.Append(h.m.Close(), h.l.Close()).ErrorOrNil()
}

func (h *handler) HandleMetric(ctx context.Context, values []*metric.Instance) error {
	return h.m.HandleMetric(ctx, values)
}

func (h *handler) HandleLogEntry(ctx context.Context, values []*logentry.Instance) error {
	return h.l.HandleLogEntry(ctx, values)
}
