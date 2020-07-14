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

// Package trace contains a tracespan adapter for Stackdriver trace.
package trace

import (
	"context"

	"go.opencensus.io/trace"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/opencensus"
	"istio.io/istio/mixer/template/tracespan"
)

type builder struct {
	types map[string]*tracespan.Type
	mg    helper.MetadataGenerator
	cfg   *config.Params
}

var (
	// compile-time assertion that we implement the interfaces we promise
	_ tracespan.HandlerBuilder = &builder{}
)

// NewBuilder returns a builder implementing the tracespan.HandlerBuilder interface.
func NewBuilder(mg helper.MetadataGenerator) tracespan.HandlerBuilder {
	return &builder{mg: mg}
}

func (b *builder) SetTraceSpanTypes(types map[string]*tracespan.Type) {
	b.types = types
}

func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.cfg = cfg.(*config.Params)
}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if t := b.cfg.Trace; t != nil {
		if t.SampleProbability < 0 || t.SampleProbability > 1 {
			ce = ce.Appendf("trace.sampleProbability", "sampling probability must be between 0 and 1 (inclusive)")
		}
	}
	return ce
}

func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	cfg := b.cfg
	md := b.mg.GenerateMetadata()
	if cfg.ProjectId == "" {
		// Try to fill project ID if it is not provided with metadata.
		cfg.ProjectId = md.ProjectID
	}
	exporter, err := getExporterFunc(ctx, env, cfg)
	if err != nil {
		return nil, err
	}

	traceCfg := b.cfg.Trace
	var sampler trace.Sampler
	if traceCfg != nil {
		if sampleProbability := traceCfg.SampleProbability; sampleProbability > 0 {
			sampler = trace.ProbabilitySampler(traceCfg.SampleProbability)
		}
	}
	newExporter := func(name string, endpoint string) trace.Exporter {
		return exporter
	}
	return opencensus.NewTraceHandler(newExporter, sampler), nil
}
