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
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/stackdriver/config/config.proto -i mixer/adapter/stackdriver/config -x "-n stackdriver -t logentry -t tracespan -t metric -t edges -d example"

// Package stackdriver provides an adapter that implements the logEntry and metrics
// templates to serialize generated values to Stackdriver.
package stackdriver

import (
	"context"

	md "cloud.google.com/go/compute/metadata"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/adapter/stackdriver/contextgraph"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/adapter/stackdriver/log"
	sdmetric "istio.io/istio/mixer/adapter/stackdriver/metric"
	"istio.io/istio/mixer/adapter/stackdriver/trace"
	"istio.io/istio/mixer/pkg/adapter"
	edgepb "istio.io/istio/mixer/template/edge"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/tracespan"
)

type (
	builder struct {
		m metric.HandlerBuilder
		l logentry.HandlerBuilder
		t tracespan.HandlerBuilder
		c edgepb.HandlerBuilder
	}

	handler struct {
		m metric.Handler
		l logentry.Handler
		t tracespan.Handler
		c edgepb.Handler
	}
)

var (
	_ metric.HandlerBuilder = &builder{}
	_ metric.Handler        = &handler{}

	_ logentry.HandlerBuilder = &builder{}
	_ logentry.Handler        = &handler{}

	_ tracespan.HandlerBuilder = &builder{}
	_ tracespan.Handler        = &handler{}

	_ edgepb.HandlerBuilder = &builder{}
	_ edgepb.Handler        = &handler{}
)

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	clusterNameFn := func() (string, error) {
		cn, err := md.InstanceAttributeValue("cluster-name")
		if err != nil {
			return "", err
		}
		return cn, nil
	}
	clusterLocationFn := func() (string, error) {
		cl, err := md.InstanceAttributeValue("cluster-location")
		if err == nil {
			return cl, nil
		}
		return md.Zone()
	}
	mg := helper.NewMetadataGenerator(md.OnGCE, md.ProjectID, clusterLocationFn, clusterNameFn)
	info := metadata.GetInfo("stackdriver")
	info.NewBuilder = func() adapter.HandlerBuilder {
		return &builder{
			m: sdmetric.NewBuilder(mg),
			l: log.NewBuilder(mg),
			t: trace.NewBuilder(mg),
			c: contextgraph.NewBuilder(mg),
		}
	}
	return info
}

func (b *builder) SetMetricTypes(metrics map[string]*metric.Type) {
	b.m.SetMetricTypes(metrics)
}

func (b *builder) SetLogEntryTypes(entries map[string]*logentry.Type) {
	b.l.SetLogEntryTypes(entries)
}

func (b *builder) SetTraceSpanTypes(types map[string]*tracespan.Type) {
	b.t.SetTraceSpanTypes(types)
}

func (b *builder) SetEdgeTypes(types map[string]*edgepb.Type) {
	b.c.SetEdgeTypes(types)
}

func (b *builder) SetAdapterConfig(c adapter.Config) {
	b.m.SetAdapterConfig(c)
	b.l.SetAdapterConfig(c)
	b.t.SetAdapterConfig(c)
	b.c.SetAdapterConfig(c)
}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	return ce.Extend(b.m.Validate()).
		Extend(b.l.Validate()).
		Extend(b.t.Validate()).
		Extend(b.c.Validate())
}

// Build creates a stack driver handler object.
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	m, err := b.m.Build(ctx, env)
	if err != nil {
		return nil, err
	}
	mh := m.(metric.Handler)

	l, err := b.l.Build(ctx, env)
	if err != nil {
		return nil, err
	}
	lh := l.(logentry.Handler)

	t, err := b.t.Build(ctx, env)
	if err != nil {
		return nil, err
	}
	th := t.(tracespan.Handler)

	c, err := b.c.Build(ctx, env)
	if err != nil {
		return nil, err
	}
	ch := c.(edgepb.Handler)

	return &handler{m: mh, l: lh, t: th, c: ch}, nil
}

func (h *handler) Close() error {
	return multierror.Append(h.m.Close(), h.l.Close(), h.t.Close(), h.c.Close()).ErrorOrNil()
}

func (h *handler) HandleMetric(ctx context.Context, values []*metric.Instance) error {
	return h.m.HandleMetric(ctx, values)
}

func (h *handler) HandleLogEntry(ctx context.Context, values []*logentry.Instance) error {
	return h.l.HandleLogEntry(ctx, values)
}

func (h *handler) HandleTraceSpan(ctx context.Context, values []*tracespan.Instance) error {
	return h.t.HandleTraceSpan(ctx, values)
}

func (h *handler) HandleEdge(ctx context.Context, values []*edgepb.Instance) error {
	return h.c.HandleEdge(ctx, values)
}
