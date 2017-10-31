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

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/mixer/adapter/stackdriver/config"
	"istio.io/mixer/adapter/stackdriver/log"
	sdmetric "istio.io/mixer/adapter/stackdriver/metric"
	"istio.io/mixer/pkg/adapter"
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
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "stackdriver",
		Impl:        "istio.io/mixer/adapte/stackdriver",
		Description: "Publishes StackDriver metrics and logs.",
		SupportedTemplates: []string{
			metric.TemplateName,
			logentry.TemplateName,
		},
		DefaultConfig: &config.Params{},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{m: sdmetric.NewBuilder(), l: log.NewBuilder()} },
	}
}

func (b *builder) SetMetricTypes(metrics map[string]*metric.Type) {
	b.m.SetMetricTypes(metrics)
}

func (b *builder) SetLogEntryTypes(entries map[string]*logentry.Type) {
	b.l.SetLogEntryTypes(entries)
}
func (b *builder) SetAdapterConfig(c adapter.Config) {
	b.m.SetAdapterConfig(c)
	b.l.SetAdapterConfig(c)
}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	return ce.Extend(b.m.Validate()).Extend(b.l.Validate())
}

// Build creates a stack driver handler object.
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	m, err := b.m.Build(ctx, env)
	if err != nil {
		return nil, err
	}
	mh, _ := m.(metric.Handler)

	l, err := b.l.Build(ctx, env)
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
