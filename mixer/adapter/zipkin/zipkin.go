// Copyright 2018 the Istio Authors.
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

//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/zipkin/config/config.proto -i mixer/adapter/zipkin/config

// Package zipkin contains a tracespan adapter for Zipkin (https://zipkin.io/).
package zipkin

import (
	"context"

	zgo "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/reporter"
	zhttp "github.com/openzipkin/zipkin-go/reporter/http"
	oczipkin "go.opencensus.io/exporter/zipkin"
	"go.opencensus.io/trace"

	"istio.io/istio/mixer/adapter/zipkin/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/opencensus"
	"istio.io/istio/mixer/template/tracespan"
)

type (
	builder struct {
		types map[string]*tracespan.Type
		cfg   *config.Params
	}
)

// compile-time assertion that we implement the interfaces we promise
var _ tracespan.HandlerBuilder = &builder{}

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "zipkin",
		Impl:        "istio.io/istio/mixer/adapter/zipkin",
		Description: "Publishes traces to Zipkin.",
		SupportedTemplates: []string{
			tracespan.TemplateName,
		},
		DefaultConfig: &config.Params{},
		NewBuilder: func() adapter.HandlerBuilder {
			return &builder{}
		}}
}

func (b *builder) SetTraceSpanTypes(types map[string]*tracespan.Type) {
	b.types = types
}
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.cfg = cfg.(*config.Params)
}
func (b *builder) Validate() (err *adapter.ConfigErrors) {
	if b.cfg.SampleProbability < 0 || b.cfg.SampleProbability > 1 {
		err = err.Appendf("sample_probability", "must be between 0 and 1 inclusive")
	}
	return err
}

var getReporterFunc = newReporter // indirection for testing

func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	rep := getReporterFunc(b.cfg)
	traceCfg := b.cfg
	var sampler trace.Sampler
	if sampleProbability := traceCfg.SampleProbability; sampleProbability > 0 {
		sampler = trace.ProbabilitySampler(traceCfg.SampleProbability)
	}
	newExporter := func(name, endpoint string) trace.Exporter {
		ep, err := zgo.NewEndpoint(name, endpoint)
		if err != nil {
			env.Logger().Warningf("Failed to construct endpoint: %s", err)
		}
		// Share the underlying Reporter, but with a new endpoint:
		return oczipkin.NewExporter(rep, ep)
	}
	handler := opencensus.NewTraceHandler(newExporter, sampler)
	handler.CloseFunc = rep.Close
	return handler, nil
}

func newReporter(cfg *config.Params) reporter.Reporter {
	return zhttp.NewReporter(cfg.Url)
}
